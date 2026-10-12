import assert from "node:assert/strict";
import test from "node:test";

import { readEffectiveBuildDefaults } from "../../apps/console-ui/src/api/publisher-api.ts";

const digest = "sha256:" + "a".repeat(64);

interface OwnerFacts {
  policy?: { defaultWebuiVersionId?: string };
  webui?: { id: string; name?: string; versionLabel?: string; artifactDigest: string; status: string }[];
  runtime?: { id: string; name?: string; versionLabel?: string; status: string; artifactDigest: string; defaultForNewBuilds?: boolean }[];
}

function ownerFetch(facts: OwnerFacts) {
  const requests: string[] = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const path = new URL(String(input), "https://opl-cloud.invalid").pathname;
    requests.push(path);
    const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
    if (path === "/api/v2/catalog/runtime-versions") {
      // The member catalog marks the default the way Runtime Control's own
      // projection does; a row may state it, and otherwise only runtime-1 is.
      const items = (facts.runtime ?? []).map((row) => ({
        ...row,
        defaultForNewBuilds: row.defaultForNewBuilds ?? row.id === "runtime-1"
      }));
      return json({ items, nextCursor: "" });
    }
    if (path === "/api/v2/catalog/build-policy") return json(facts.policy ?? {});
    if (path === "/api/v2/catalog/webui-versions") return json({ items: facts.webui ?? [], nextCursor: "" });
    throw new Error(`unexpected owner read ${path}`);
  }) as typeof globalThis.fetch;
  return { requests, restore: () => { globalThis.fetch = original; } };
}

const runtime = { id: "runtime-1", name: "OPL App Runtime", versionLabel: "26.9.26", status: "approved", artifactDigest: digest };
const webui = { id: "webui-1", name: "OPL WebUI", versionLabel: "1.4.0", status: "approved", artifactDigest: digest };

// A Publisher can start a Build only when BOTH halves of the Runtime Control
// owner's effective policy resolve to exact immutable inputs. The Runtime half
// comes from the member catalog projection; the WebUI half is the same policy's
// other field, which the Console reads from the owner's own member-readable
// effective policy and then looks up in the member WebUI catalog. Before this,
// the reader passed a literal null for the WebUI half, so every Build was
// blocked with effective_default_webui_not_readable no matter what the platform
// had published.
test("Publisher resolves both frozen Build inputs from owner-served facts", async () => {
  const owner = ownerFetch({ runtime: [runtime], policy: { defaultWebuiVersionId: "webui-1" }, webui: [webui] });
  try {
    const defaults = await readEffectiveBuildDefaults(new AbortController().signal);
    assert.deepEqual(defaults.reasons, []);
    assert.equal(defaults.runtime?.id, "runtime-1");
    assert.equal(defaults.webui?.id, "webui-1");
    assert.equal(defaults.webui?.artifactDigest, digest);
    // The WebUI half is read from the owner policy, never guessed from the catalog.
    assert.ok(owner.requests.includes("/api/v2/catalog/build-policy"), `requests=${owner.requests.join(",")}`);
    assert.ok(owner.requests.includes("/api/v2/catalog/webui-versions"), `requests=${owner.requests.join(",")}`);
  } finally {
    owner.restore();
  }
});

// The owner's policy is the only thing allowed to name the default WebUI. If it
// names one the Console cannot read back, the Build stays blocked: substituting
// some other approved catalog row would silently freeze an input the platform
// never chose.
test("Publisher refuses to substitute a WebUI the owner policy does not name", async () => {
  const owner = ownerFetch({ runtime: [runtime], policy: { defaultWebuiVersionId: "webui-absent" }, webui: [webui] });
  try {
    const defaults = await readEffectiveBuildDefaults(new AbortController().signal);
    assert.equal(defaults.webui, undefined);
    assert.deepEqual(defaults.reasons, ["effective_default_webui_not_readable"]);
    assert.equal(defaults.runtime?.id, "runtime-1");
  } finally {
    owner.restore();
  }
});

// A policy that names no WebUI at all is the same missing owner fact, not an
// instruction to pick one.
test("Publisher reports a missing default WebUI when the owner policy names none", async () => {
  const owner = ownerFetch({ runtime: [runtime], policy: {}, webui: [webui] });
  try {
    const defaults = await readEffectiveBuildDefaults(new AbortController().signal);
    assert.equal(defaults.webui, undefined);
    assert.deepEqual(defaults.reasons, ["effective_default_webui_not_readable"]);
  } finally {
    owner.restore();
  }
});

// A Build freezes exactly three inputs: the uploaded PackageVersion, the
// effective Runtime, and the effective WebUI. Both catalog rows can contain
// several approved entries, so this pins that the frozen ids are the ones the
// Runtime Control owner named — the runtime its own projection flags, and the
// WebUI its policy names — and never a substitute approved row.
test("Publisher freezes the owner-named inputs when the catalogs hold other approved rows", async () => {
  const owner = ownerFetch({
    runtime: [runtime, { ...runtime, id: "runtime-other", name: "Other Runtime", defaultForNewBuilds: false }],
    policy: { defaultWebuiVersionId: "webui-1" },
    webui: [webui, { ...webui, id: "webui-other", name: "Other WebUI" }]
  });
  try {
    const defaults = await readEffectiveBuildDefaults(new AbortController().signal);
    assert.deepEqual(defaults.reasons, []);
    assert.equal(defaults.runtime?.id, "runtime-1");
    assert.equal(defaults.webui?.id, "webui-1");
  } finally {
    owner.restore();
  }
});

// The Runtime half comes from the owner's own defaultForNewBuilds projection. If
// the platform has flagged more than one, neither is authoritative and the Build
// stays blocked rather than taking the first row.
test("Publisher refuses to choose between two runtimes the owner flagged as default", async () => {
  const owner = ownerFetch({
    runtime: [runtime, { ...runtime, id: "runtime-other", name: "Other Runtime", defaultForNewBuilds: true }],
    policy: { defaultWebuiVersionId: "webui-1" },
    webui: [webui]
  });
  try {
    const defaults = await readEffectiveBuildDefaults(new AbortController().signal);
    assert.equal(defaults.runtime, undefined);
    assert.ok(defaults.reasons.includes("effective_default_runtime_ambiguous"), `reasons=${defaults.reasons}`);
  } finally {
    owner.restore();
  }
});