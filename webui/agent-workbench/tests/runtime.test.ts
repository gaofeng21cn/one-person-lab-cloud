import assert from "node:assert/strict";
import { test } from "node:test";
import {
  ApiResultError,
  messagesFromRuns,
  readAgentReply,
  readRuntimeAgents,
  readWorkbenchConfig,
  validateResult,
  type AgentSelection,
  type ApiEvidence,
  type ApiRun,
  type WorkbenchConfig,
} from "../state/workbench/remoteModelAdapter.ts";
import {
  evidenceDocumentTitle,
  groupEvidenceDocuments,
  sourceParagraphs,
} from "../state/workbench/evidencePresentation.ts";

function agentEntry(name: string, selection: AgentSelection) {
  return {
    package_id: selection.package_id,
    display_name: name,
    package_role: "standard_agent",
    readiness: { operational_ready: true },
    capability_metadata: { required_skill_ids: selection.required_skill_ids },
    home_shortcuts: [{
      shortcut_id: selection.shortcut_id,
      route: {
        route_kind: "agent_package_shortcut",
        executor: "codex_cli",
        codex_visible_entry: selection.codex_visible_entry,
      },
    }],
  };
}

function runtimeDirectory(entries: unknown[], modelReady: unknown = true) {
  return {
    app_state: {
      version: "g2",
      app_state: {
        schema_version: "opl_app_state.v1",
        core: { codex: { model_access_ready: modelReady } },
        agent_packages: { directory: { entries } },
      },
    },
  };
}

function completedReply() {
  return {
    executor: "codex_app_server",
    threadId: "thread-current",
    turnId: "turn-current",
    finalMessage: "The experiment plan is ready.",
    canonicalThread: {
      id: "thread-current",
      turns: [{ id: "turn-current", status: "completed" }],
    },
  };
}

function evidence(overrides: Partial<ApiEvidence> = {}): ApiEvidence {
  return {
    id: "source-1",
    citationId: 1,
    title: "Experiment notes",
    detail: "Recorded observations",
    quote: "The measured value increased.",
    sourceKind: "notebook",
    ...overrides,
  };
}

function answeredRun(items: ApiEvidence[] = []): ApiRun {
  return {
    runId: "turn-current",
    threadId: "thread-current",
    status: "succeeded",
    question: "What did the experiment show?",
    progress: { phase: "completed" },
    result: { runId: "turn-current", reply: "The measured value increased.", evidence: items },
  };
}

const researchSelection: AgentSelection = {
  package_id: "research-notebook",
  shortcut_id: "inspect",
  codex_visible_entry: "$research-notebook inspect",
  required_skill_ids: ["experiment-log", "data-analysis"],
};
const designSelection: AgentSelection = {
  package_id: "design-workshop",
  shortcut_id: "inspect",
  codex_visible_entry: "$design-workshop review",
  required_skill_ids: ["visual-review"],
};

test("independent Agent catalogs preserve their own executable selections", async (t) => {
  for (const [name, selection] of [
    ["Research Notebook", researchSelection],
    ["Design Workshop", designSelection],
  ] as const) {
    await t.test(name, () => {
      const directory = readRuntimeAgents(runtimeDirectory([agentEntry(name, selection)]));
      assert.equal(directory.modelReady, true);
      assert.equal(directory.packages.length, 1);
      assert.equal(directory.packages[0].name, name);
      assert.deepEqual(directory.packages[0].selection, selection);
      assert.equal(directory.packages[0].packageId, selection.package_id);
      assert.equal(typeof directory.packages[0].choiceId, "string");
      assert.ok(directory.packages[0].choiceId.length > 0);
    });
  }
});

test("choiceId distinguishes shortcuts while packageId preserves the real package identity", () => {
  const research = agentEntry("Research Notebook", researchSelection);
  const summarize = { ...researchSelection, shortcut_id: "summarize", codex_visible_entry: "$research-notebook summarize" };
  research.home_shortcuts.push(agentEntry("Research Notebook", summarize).home_shortcuts[0]);
  const { packages } = readRuntimeAgents(runtimeDirectory([
    research,
    agentEntry("Design Workshop", designSelection),
  ]));
  assert.equal(packages.length, 3);
  assert.equal(new Set(packages.map((item) => item.choiceId)).size, 3);
  assert.deepEqual(packages.filter((item) => item.packageId === researchSelection.package_id)
    .map((item) => item.selection.shortcut_id).sort(), ["inspect", "summarize"]);
  assert.equal(packages.filter((item) => item.packageId === designSelection.package_id).length, 1);
  for (const selection of [researchSelection, summarize, designSelection]) {
    const selected = packages.find((item) => item.selection.package_id === selection.package_id
      && item.selection.shortcut_id === selection.shortcut_id);
    assert.ok(selected);
    assert.equal(selected.packageId, selection.package_id);
    assert.equal(typeof selected.choiceId, "string");
    assert.ok(selected.choiceId.length > 0);
    const uiChoice = packages.find((item) => item.choiceId === selected.choiceId);
    assert.ok(uiChoice);
    assert.deepEqual(uiChoice.selection, selection);
  }
});

test("only operational standard Agents with a supported executable shortcut are selectable", () => {
  const ready = agentEntry("Research Notebook", researchSelection);
  const unready = { ...ready, package_id: "not-ready", readiness: { operational_ready: false } };
  const helper = { ...ready, package_id: "internal-helper", package_role: "support_agent" };
  const unsupported = agentEntry("Unsupported executor", { ...researchSelection, package_id: "unsupported" });
  unsupported.home_shortcuts[0].route.executor = "other_executor";
  const unrelated = agentEntry("Unrelated route", { ...researchSelection, package_id: "unrelated" });
  unrelated.home_shortcuts[0].route.route_kind = "other_route";
  const { packages } = readRuntimeAgents(runtimeDirectory([unready, helper, unsupported, unrelated, ready]));
  assert.equal(packages.length, 1);
  assert.deepEqual(packages[0].selection, researchSelection);
});

test("a valid Agent directory does not imply model readiness", async (t) => {
  for (const modelReady of [false, undefined, null, "true"]) {
    await t.test(`model_access_ready=${String(modelReady)}`, () => {
      const payload = runtimeDirectory([agentEntry("Research Notebook", researchSelection)]);
      // A missing JSON member is distinct from the explicit boolean true.
      if (modelReady === undefined) Reflect.deleteProperty(payload.app_state.app_state.core.codex, "model_access_ready");
      else payload.app_state.app_state.core.codex.model_access_ready = modelReady;
      const directory = readRuntimeAgents(payload);
      assert.equal(directory.modelReady, false);
      assert.equal(directory.packages.length, 1);
    });
  }
});

test("unsupported or incomplete Agent directory protocols are rejected", async (t) => {
  const current = runtimeDirectory([]);
  for (const [name, payload] of [
    ["missing envelope", {}],
    ["unsupported generation", { app_state: { ...current.app_state, version: "g1" } }],
    ["unsupported state schema", { app_state: { ...current.app_state, app_state: { ...current.app_state.app_state, schema_version: "opl_app_state.v2" } } }],
    ["missing directory", { app_state: { ...current.app_state, app_state: { schema_version: "opl_app_state.v1" } } }],
  ] as const) {
    await t.test(name, () => assert.throws(() => readRuntimeAgents(payload), ApiResultError));
  }
});

test("an advertised executable shortcut cannot omit its required skill declaration", () => {
  const entry = agentEntry("Research Notebook", researchSelection);
  const { capability_metadata: _metadata, ...undeclared } = entry;
  assert.throws(() => readRuntimeAgents(runtimeDirectory([undeclared])), ApiResultError);
});

test("canonical completed confirmation becomes a validated answer and conversation message", () => {
  const response = completedReply();
  const run = readAgentReply(response, response.threadId);
  assert.equal(run.status, "succeeded");
  assert.equal(run.runId, response.turnId);
  assert.equal(run.threadId, response.threadId);
  const result = validateResult(run);
  assert.equal(result.reply, response.finalMessage);
  assert.equal(result.runId, run.runId);
  assert.deepEqual(result.evidence, [], "the UI must not invent citations for a plain Runtime answer");
  const messages = messagesFromRuns([run]);
  assert.equal(messages.length, 1);
  assert.equal(messages[0].role, "assistant");
  assert.equal(messages[0].content, response.finalMessage);
  assert.deepEqual(messages[0].metadata?.custom?.result, result);
});

test("canonical failure rejects a stale top-level answer and surfaces the execution error", () => {
  const response = completedReply();
  const failed = {
    ...response,
    canonicalThread: {
      ...response.canonicalThread,
      turns: [
        { id: response.turnId, status: "failed", error: { message: "Agent execution failed" } },
        { id: "another-turn", status: "completed" },
      ],
    },
  };
  assert.throws(() => readAgentReply(failed, response.threadId),
    (error: unknown) => error instanceof ApiResultError && error.message === "Agent execution failed");
});

test("canonical confirmation rejects wrong thread, turn, executor, and unfinished execution", async (t) => {
  const response = completedReply();
  for (const [name, payload, expectedThreadId] of [
    ["unexpected response thread", response, "thread-other"],
    ["canonical thread mismatch", { ...response, canonicalThread: { ...response.canonicalThread, id: "thread-other" } }, response.threadId],
    ["turn missing from canonical thread", { ...response, turnId: "turn-other" }, response.threadId],
    ["unfinished turn with premature answer", { ...response, canonicalThread: { ...response.canonicalThread, turns: [{ id: response.turnId, status: "inProgress" }] } }, response.threadId],
    ["unsupported executor", { ...response, executor: "codex_cli" }, response.threadId],
  ] as const) {
    await t.test(name, () => assert.throws(() => readAgentReply(payload, expectedThreadId), ApiResultError));
  }
});

test("completed metadata without an actual answer is rejected", async (t) => {
  for (const answer of [undefined, null, "", " \n\t"]) {
    await t.test(`finalMessage=${JSON.stringify(answer)}`, () => {
      const response = { ...completedReply(), finalMessage: answer };
      assert.throws(() => readAgentReply(response, "thread-current"), ApiResultError);
    });
  }
});

test("generic evidence kinds retain source titles without synthetic PDF suffixes", () => {
  const items: ApiEvidence[] = [
    evidence({ id: "notes", title: "Experiment notes", sourceKind: "notebook" }),
    evidence({ id: "code", title: "README.md", sourceKind: "repository" }),
    evidence({ id: "data", title: "measurements.csv", sourceKind: "dataset" }),
    evidence({ id: "scan", title: "Original.PDF", sourceKind: "archive" }),
  ];
  const result = validateResult(answeredRun(items));
  assert.deepEqual(result.evidence.map((item) => item.sourceKind), ["notebook", "repository", "dataset", "archive"]);
  for (const item of result.evidence) assert.equal(evidenceDocumentTitle(item), item.title);
  assert.deepEqual(groupEvidenceDocuments(result.evidence).map((group) => group.title), items.map((item) => item.title));
});

test("only a shared source identity groups passages; matching titles alone do not", () => {
  const source: NonNullable<ApiEvidence["source"]> = {
    documentId: "document-a",
    fileName: "notes.md",
    url: "/documents/document-a",
    locations: [{ page: 1, rect: [0, 0, 1, 1] }],
  };
  const first = evidence({ id: "passage-a", source });
  const second = evidence({ id: "passage-b", source, quote: "A second observation." });
  const distinct = evidence({ id: "passage-c", source: { ...source, documentId: "document-b", url: "/documents/document-b" } });
  const withoutSource = evidence({ id: "passage-d" });
  const groups = groupEvidenceDocuments([first, second, distinct, withoutSource]);
  assert.deepEqual(groups.map((group) => group.items.map((item) => item.id)), [
    ["passage-a", "passage-b"], ["passage-c"], ["passage-d"],
  ]);
  assert.ok(groups.every((group) => group.title === first.title));
});

test("quotation paragraphs preserve source line breaks and do not split prose sentences", () => {
  assert.deepEqual(sourceParagraphs("First sentence. Second sentence.\r\n\r\n  Indented source line.\rLast line.\n"), [
    "First sentence. Second sentence.", "  Indented source line.", "Last line.",
  ]);
});

test("claims must resolve to evidence in the same answer", () => {
  const run = answeredRun([evidence()]);
  run.result!.claims = [{ text: "The measured value increased.", evidence_ids: ["source-1"] }];
  assert.doesNotThrow(() => validateResult(run));
  run.result!.claims[0].evidence_ids = ["source-from-another-answer"];
  assert.throws(() => validateResult(run), ApiResultError);
});

test("answer identity, nonblank content, and unique evidence ids are required", async (t) => {
  const valid = answeredRun([evidence()]);
  for (const [name, run] of [
    ["result belongs to another turn", { ...valid, result: { ...valid.result!, runId: "turn-other" } }],
    ["blank reply", { ...valid, result: { ...valid.result!, reply: " \n" } }],
    ["duplicate evidence identity", answeredRun([evidence(), evidence({ quote: "Conflicting passage." })])],
    ["unconfirmed run status", { ...valid, status: "running" as const }],
  ] as const) {
    await t.test(name, () => assert.throws(() => validateResult(run), ApiResultError));
  }
});

const workbenchConfig: WorkbenchConfig = {
  schemaVersion: 1,
  runtimeProtocol: "opl-native-g2",
  sidebarTitle: "Research sessions",
  assistantTitle: null,
  notice: null,
};

test("explicit supported configuration preserves optional presentation choices", () => {
  assert.deepEqual(readWorkbenchConfig(workbenchConfig), workbenchConfig);
  const named: WorkbenchConfig = {
    ...workbenchConfig,
    assistantTitle: "Design Workshop",
    notice: { title: "Workspace note", body: "Review generated changes before applying them." },
  };
  assert.deepEqual(readWorkbenchConfig(named), named);
});

test("configuration requires an explicit supported protocol without automatic downgrade", async (t) => {
  for (const protocol of [undefined, null, "", "auto", "legacy-bridge", "opl-native-g1", "opl-native-g3"]) {
    await t.test(`runtimeProtocol=${String(protocol)}`, () => {
      const { runtimeProtocol: _protocol, ...rest } = workbenchConfig;
      const config = protocol === undefined ? rest : { ...rest, runtimeProtocol: protocol };
      assert.throws(() => readWorkbenchConfig(config), /WebUI 配置无效或 Runtime 协议不受支持/);
    });
  }
});
