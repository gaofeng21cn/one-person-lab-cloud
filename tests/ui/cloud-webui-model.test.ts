import assert from "node:assert/strict";
import test from "node:test";
import { readRuntimeAgents, agentMessageRequest, readAgentReply } from "../../webui/cloud-agent/runtime-api.js";
import { admitWebuiSelection, isImmutableDigest, resolveEffectiveBuildDefaults } from "../../apps/console-ui/src/app/cloud-webui-model.ts";
import { readEffectiveBuildDefaults } from "../../apps/console-ui/src/api/publisher-api.ts";

const digest = "sha256:" + "a".repeat(64);
const webui = { id: "webui-1", status: "approved", artifactDigest: digest, port: 3000, healthPath: "/healthz", runtimeAbiVersions: ["opl-runtime-abi/v1"], packageFormatVersions: ["opl-agent-package/v1"] };
const runtime = { id: "runtime-1", status: "approved", artifactDigest: digest, runtimeAbiVersion: "opl-runtime-abi/v1", packageFormatVersions: ["opl-agent-package/v1"] };

test("Cloud WebUI admission accepts exact immutable compatible inputs", () => {
  assert.deepEqual(admitWebuiSelection(webui, runtime), { accepted: true, reasons: [] });
  assert.equal(isImmutableDigest(digest), true);
});

test("Cloud WebUI admission refuses mutable or incompatible inputs", () => {
  const decision = admitWebuiSelection({ ...webui, artifactDigest: "ghcr.io/example/webui:latest", port: 8080 }, { ...runtime, runtimeAbiVersion: "opl-runtime-abi/v2" });
  assert.deepEqual(decision.reasons, ["webui_artifact_digest_not_immutable", "webui_port_not_3000", "runtime_abi_not_supported_by_webui"]);
});

test("Cloud publisher resolves the new Build's Runtime from the owner's effective policy projection", () => {
  const approved = { id: "runtime-approved", name: "OPL App Runtime", versionLabel: "26.9.26", status: "approved", artifactDigest: digest };
  const resolution = resolveEffectiveBuildDefaults([
    approved,
    { id: "runtime-other", status: "approved", artifactDigest: digest },
    { id: "runtime-mutable", status: "approved", artifactDigest: "ghcr.io/example/runtime:latest" }
  ].map((row) => ({ ...row, defaultForNewBuilds: row.id === "runtime-approved" })), null);
  assert.deepEqual(resolution.runtime, { id: "runtime-approved", name: "OPL App Runtime", versionLabel: "26.9.26", artifactDigest: digest });
  assert.equal(resolution.webui, undefined);
  // An unreadable default remains unavailable instead of guessing a catalog row.
  assert.deepEqual(resolution.reasons, ["effective_default_webui_not_readable"]);
});

test("Cloud publisher refuses to guess when the owner policy names no usable default", () => {
  const approved = { id: "runtime-approved", status: "approved", artifactDigest: digest, defaultForNewBuilds: true };
  assert.deepEqual(resolveEffectiveBuildDefaults([{ ...approved, defaultForNewBuilds: false }], null).reasons, ["effective_default_runtime_missing", "effective_default_webui_not_readable"]);
  assert.deepEqual(resolveEffectiveBuildDefaults([approved, { ...approved, id: "runtime-second" }], null).reasons, ["effective_default_runtime_ambiguous", "effective_default_webui_not_readable"]);
  assert.deepEqual(resolveEffectiveBuildDefaults([{ ...approved, status: "revoked" }], null).reasons, ["effective_default_runtime_not_approved", "effective_default_webui_not_readable"]);
  assert.deepEqual(resolveEffectiveBuildDefaults([{ ...approved, artifactDigest: "ghcr.io/example/runtime:latest" }], null).reasons, ["effective_default_runtime_digest_not_immutable", "effective_default_webui_not_readable"]);
});

test("Cloud publisher carries an owner-served effective default WebUI into the frozen inputs", () => {
  const runtime = { id: "runtime-approved", status: "approved", artifactDigest: digest, defaultForNewBuilds: true };
  const resolution = resolveEffectiveBuildDefaults([runtime], { id: "webui-default", name: "OPL WebUI", versionLabel: "1.4.0", artifactDigest: digest });
  assert.deepEqual(resolution.reasons, []);
  assert.equal(resolution.runtime?.id, "runtime-approved");
  assert.deepEqual(resolution.webui, { id: "webui-default", name: "OPL WebUI", versionLabel: "1.4.0", artifactDigest: digest });
});

test("publisher reads the exact member policy and refuses a withdrawn WebUI or mismatched Runtime", async () => {
  const original = globalThis.fetch;
  const requests: string[] = [];
  let webuiStatus = "approved";
  let runtimeId = "runtime-1";
  globalThis.fetch = async (input) => {
    const path = String(input);
    requests.push(path);
    const data = path.startsWith("/api/v2/catalog/runtime-versions") ? { items: [{ ...runtime, defaultForNewBuilds: true }] }
      : path.startsWith("/api/v2/catalog/webui-versions") ? { items: [{ ...webui, status: webuiStatus }, { ...webui, id: "first-catalog-row" }] }
      : { runtimeVersionId: runtimeId, defaultWebuiVersionId: "webui-1" };
    return new Response(JSON.stringify(data), { headers: { "content-type": "application/json" } });
  };
  try {
    const signal = new AbortController().signal;
    const ready = await readEffectiveBuildDefaults(signal);
    assert.equal(ready.webui?.id, "webui-1");
    assert.deepEqual(ready.reasons, []);
    assert.ok(requests.includes("/api/v2/catalog/build-policy"));
    assert.equal(requests.some(path => path.includes("/admin/")), false);
    webuiStatus = "revoked";
    const revoked = await readEffectiveBuildDefaults(signal);
    assert.equal(revoked.webui, undefined);
    assert.deepEqual(revoked.reasons, ["effective_default_webui_not_readable"]);
    runtimeId = "other-runtime";
    assert.ok((await readEffectiveBuildDefaults(signal)).reasons.includes("effective_default_runtime_policy_mismatch"));
  } finally {
    globalThis.fetch = original;
  }
});

function agentEntry() {
  return {
    package_id: "ibd-agent", display_name: "IBD Agent", package_role: "standard_agent",
    readiness: { operational_ready: true },
    capability_metadata: { required_skill_ids: ["ibd-evidence", "scientific-search"] },
    home_shortcuts: [{ shortcut_id: "ask", route: {
      route_kind: "agent_package_shortcut", executor: "codex_cli", codex_visible_entry: "$ibd-agent"
    } }]
  };
}

function runtimeState(entries: unknown[] = [agentEntry()], modelReady: unknown = true) {
  return { app_state: { version: "g2", app_state: {
    schema_version: "opl_app_state.v1", agent_packages: { directory: { entries } },
    core: { codex: { model_access_ready: modelReady } }
  } } };
}

function completedReply() {
  return {
    executor: "codex_app_server", threadId: "thread-ibd", turnId: "turn-ibd", finalMessage: "IBD answer",
    canonicalThread: { id: "thread-ibd", turns: [
      { id: "unrelated-turn", status: "failed" },
      { id: "turn-ibd", status: "completed" }
    ] }
  };
}

test("Runtime Agent decoder reads the g2 envelope and exact ready shortcut selection", () => {
  const entry = agentEntry();
  entry.home_shortcuts.push({ shortcut_id: "review", route: {
    route_kind: "agent_package_shortcut", executor: "codex_cli", codex_visible_entry: "$ibd-review"
  } });
  const result = readRuntimeAgents(runtimeState([entry]));
  assert.deepEqual(result, {
    modelReady: true,
    choices: [
      { id: JSON.stringify(["ibd-agent", "ask"]), label: "IBD Agent", selection: {
        package_id: "ibd-agent", shortcut_id: "ask", codex_visible_entry: "$ibd-agent",
        required_skill_ids: ["ibd-evidence", "scientific-search"]
      } },
      { id: JSON.stringify(["ibd-agent", "review"]), label: "IBD Agent", selection: {
        package_id: "ibd-agent", shortcut_id: "review", codex_visible_entry: "$ibd-review",
        required_skill_ids: ["ibd-evidence", "scientific-search"]
      } }
    ]
  });
});

test("Runtime Agent decoder rejects unsupported versions and incomplete directory envelopes", () => {
  const state = runtimeState().app_state.app_state;
  for (const value of [
    undefined, null, {},
    { app_state: state },
    { app_state: { version: "g1", app_state: state } },
    { app_state: { version: "g2", app_state: { ...state, schema_version: "opl_app_state.v2" } } },
    { app_state: { version: "g2", app_state: { ...state, agent_packages: {} } } },
    { app_state: { version: "g2", app_state: { ...state, agent_packages: { directory: { entries: {} } } } } }
  ]) assert.throws(() => readRuntimeAgents(value), /unsupported Agent directory response/);
  assert.deepEqual(readRuntimeAgents(runtimeState([])), { choices: [], modelReady: true });
});

test("Runtime Agent decoder admits only operational standard Agents with complete codex_cli shortcuts", () => {
  const entry = agentEntry();
  const shortcut = entry.home_shortcuts[0];
  const invalidEntries = [
    { ...entry, package_role: "tool" },
    { ...entry, readiness: { operational_ready: false } },
    { ...entry, readiness: { operational_ready: "true" } },
    { ...entry, readiness: undefined },
    { ...entry, package_id: "" },
    { ...entry, home_shortcuts: undefined },
    { ...entry, capability_metadata: undefined },
    { ...entry, capability_metadata: { required_skill_ids: "ibd-evidence" } },
    ...[
      { ...shortcut, shortcut_id: "" },
      { ...shortcut, route: undefined },
      { ...shortcut, route: { ...shortcut.route, route_kind: "action" } },
      { ...shortcut, route: { ...shortcut.route, executor: "codex_app_server" } },
      { ...shortcut, route: { ...shortcut.route, codex_visible_entry: "" } }
    ].map((invalidShortcut) => ({ ...entry, home_shortcuts: [invalidShortcut] }))
  ];
  assert.deepEqual(readRuntimeAgents(runtimeState(invalidEntries)), { choices: [], modelReady: true });
  const valid = readRuntimeAgents(runtimeState([...invalidEntries, {
    ...entry, display_name: "", capability_metadata: { required_skill_ids: [] }
  }]));
  assert.equal(valid.choices.length, 1);
  assert.equal(valid.choices[0].label, "ibd-agent");
  assert.deepEqual(valid.choices[0].selection.required_skill_ids, []);
});

test("Runtime Agent decoder requires explicit model access readiness", () => {
  for (const modelReady of [null, false, 0, 1, "true", "false", {}]) {
    const result = readRuntimeAgents(runtimeState([agentEntry()], modelReady));
    assert.equal(result.modelReady, false);
    assert.equal(result.choices.length, 1, "An installed Agent does not prove model access");
  }
  const state = runtimeState();
  for (const core of [undefined, {}, { codex: {} }, { codex: { model_access_ready: undefined } }]) {
    assert.equal(readRuntimeAgents({ app_state: { ...state.app_state, app_state: {
      ...state.app_state.app_state, core
    } } }).modelReady, false);
  }
});

test("Runtime message requests select the exact shortcut first and only the existing thread on continuation", () => {
  const choice = readRuntimeAgents(runtimeState()).choices[0];
  assert.deepEqual(agentMessageRequest(choice, "First task", null), {
    prompt: "First task", agentSelection: {
      package_id: "ibd-agent", shortcut_id: "ask", codex_visible_entry: "$ibd-agent",
      required_skill_ids: ["ibd-evidence", "scientific-search"]
    }
  });
  assert.deepEqual(agentMessageRequest(choice, "First task", undefined), { prompt: "First task", agentSelection: choice.selection });
  assert.deepEqual(agentMessageRequest(undefined, "Continue task", "thread-ibd"), {
    prompt: "Continue task", threadId: "thread-ibd"
  });
});

test("Runtime reply decoder accepts only the matching completed canonical turn", () => {
  const reply = completedReply();
  assert.deepEqual(readAgentReply(reply), { threadId: "thread-ibd", answer: "IBD answer" });
  assert.deepEqual(readAgentReply(reply, "thread-ibd"), { threadId: "thread-ibd", answer: "IBD answer" });
});

test("Runtime reply decoder rejects unconfirmed executor, thread and turn identities", () => {
  const reply = completedReply();
  for (const value of [
    undefined, null, {},
    { ...reply, executor: "codex_cli" },
    { ...reply, threadId: "" },
    { ...reply, turnId: "" },
    { ...reply, canonicalThread: undefined },
    { ...reply, canonicalThread: { ...reply.canonicalThread, id: "another-thread" } },
    { ...reply, canonicalThread: { id: "thread-ibd", turns: [] } },
    { ...reply, turnId: "missing-turn" }
  ]) assert.throws(() => readAgentReply(value), /did not confirm the Agent turn/);
  assert.throws(() => readAgentReply(reply, "another-thread"), /did not confirm the Agent turn/);
});

test("Runtime reply decoder rejects failed or unfinished canonical turns despite a final message", () => {
  const reply = completedReply();
  for (const status of ["failed", "in_progress", "interrupted", "cancelled", ""]) {
    const value = { ...reply, canonicalThread: { id: "thread-ibd", turns: [
      { id: "unrelated-turn", status: "completed" }, { id: "turn-ibd", status }
    ] } };
    assert.throws(() => readAgentReply(value), /Agent turn/);
  }
  assert.throws(() => readAgentReply({ ...reply, canonicalThread: { id: "thread-ibd", turns: [
    { id: "turn-ibd", status: "failed", error: { message: "Model execution failed" } }
  ] } }), /Model execution failed/);
});

test("Runtime reply decoder requires a nonempty final Agent answer after canonical completion", () => {
  for (const finalMessage of [undefined, null, "", " \n\t", 123, {}]) {
    assert.throws(() => readAgentReply({ ...completedReply(), finalMessage }), /completed the turn without an Agent answer/);
  }
});
