/**
 * Negative controls for the L0 evidence-layer gate.
 *
 * Every test here exists to prove the gate can FAIL. A check that cannot fail
 * is not a check — that is the defect this repository already shipped 253 times
 * in `docs/evidence/source-checks/`, where nothing read the receipts and
 * anything with `"result": "pass"` was admissible forever.
 *
 * The convention for this file: for each rule there is a positive case that
 * passes and a negative case that must not. If a test starts failing because
 * the gate got weaker, that is the intended alarm.
 */

import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { classifyReceipt, evidenceLayers, verifyEvidence } from "../../tools/verify-evidence.ts";

const SHA = "d94e541b00000000000000000000000000000000";

/** A receipt shaped exactly like the one `tools/dev-session.ts` exports. */
function sourceReceipt(overrides: Record<string, unknown> = {}) {
  return {
    schemaVersion: 1,
    receiptType: "opl_development_source_check",
    evidenceLayer: "source",
    result: "pass",
    sourceBaseSha: SHA,
    recordedAt: "2026-10-13T09:47:27.026785Z",
    writeSet: ["tools/verify-evidence.ts"],
    verifiedSource: { baseSha: SHA, changedFilesSha256: { "tools/verify-evidence.ts": "abc123" } },
    execution: { command: "npm run verify:local", exitCode: 0, tests: 122, failed: 0, skipped: 0, todo: 0, outputSha256: "def456" },
    sourceStage: { runId: "run-1", gateId: "gate-1", attempt: 1, receiptHash: "aaa" },
    ...overrides
  };
}

function realEnvReceipt(overrides: Record<string, unknown> = {}) {
  return sourceReceipt({
    evidenceLayer: "real_env",
    // Layer-appropriate proof: what was run, and who outside the test saw it.
    steps: [{ name: "owner read", command: "GET /api/gateway/usage-summary", exitCode: 0, httpStatus: 200 }],
    realEnvReadback: [{ kind: "owner_api", target: "Gateway usage for tenant t-1", observedAt: "2026-10-13T10:00:00Z" }],
    ...overrides
  });
}

async function repository(receipts: Record<string, Record<string, unknown>> | null, legacy?: string[]) {
  const root = await mkdtemp(join(tmpdir(), "opl-evidence-"));
  await mkdir(join(root, "docs/evidence/source-checks"), { recursive: true });
  for (const [name, body] of Object.entries(receipts ?? {})) {
    await writeFile(join(root, "docs/evidence/source-checks", name), JSON.stringify(body, null, 2));
  }
  if (legacy) {
    await mkdir(join(root, "docs/evidence"), { recursive: true });
    await writeFile(join(root, "docs/evidence/source-checks-legacy.json"), JSON.stringify({ receipts: legacy }, null, 2));
  }
  return root;
}

test("a host-shaped source receipt is accepted", async () => {
  const root = await repository({ "ok.json": sourceReceipt() });
  try {
    const report = verifyEvidence(root);
    assert.deepEqual(report.errors, []);
    assert.equal(report.passed, true);
    assert.equal(report.counts.conformant, 1);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("a real_env receipt with an external readback is accepted", async () => {
  const root = await repository({ "ok.json": realEnvReceipt() });
  try {
    const report = verifyEvidence(root);
    assert.deepEqual(report.errors, []);
    assert.equal(report.passed, true);
  } finally { await rm(root, { recursive: true, force: true }); }
});

// --- the prose defect that actually occurred -------------------------------

test("a prose result is rejected and the length is reported", () => {
  const errors = classifyReceipt("x.json", sourceReceipt({
    result: "existing_contract_adapter_implemented_real_tke_not_run"
  }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /is not one of pass \| fail \| partial/u);
  assert.match(errors[0], /prose/u);
});

test("every prose result found in the 2026-10-13 audit is rejected", () => {
  // Verbatim from docs/evidence/source-checks/*.json. These passed every gate
  // the repository had, because nothing read the field.
  const observed = [
    "existing_contract_adapter_implemented_real_tke_not_run",
    "source compiled; hosted Linux execution pending",
    "npm_run_verify_local_and_verify_local_full_are_green_on_the_serial_branch_head_with_zero_postgres_skips",
    "created_and_observed_active",
    "passed"
  ];
  for (const result of observed) {
    assert.ok(classifyReceipt("x.json", sourceReceipt({ result })).length > 0, `expected rejection: ${result}`);
  }
});

// --- the layer is load-bearing ---------------------------------------------

test("a real_env receipt without an external readback is rejected", () => {
  const receipt = realEnvReceipt();
  delete (receipt as Record<string, unknown>).realEnvReadback;
  const errors = classifyReceipt("x.json", receipt);
  // Reported twice on purpose: once as a missing required field, once as the
  // layer-specific rule. Both must fire so neither can regress silently.
  assert.ok(errors.some(error => /missing required field realEnvReadback/u.test(error)));
  assert.ok(errors.some(error => /requires a non-empty realEnvReadback/u.test(error)));
});

test("a real_env receipt with an empty readback array is rejected", () => {
  const errors = classifyReceipt("x.json", realEnvReceipt({ realEnvReadback: [] }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /non-empty realEnvReadback/u);
});

test("a real_env readback that names no target is rejected", () => {
  const errors = classifyReceipt("x.json", realEnvReceipt({
    realEnvReadback: [{ kind: "owner_api", observedAt: "2026-10-13T10:00:00Z" }]
  }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /must name what was observed/u);
});

test("a real_env readback with an unknown kind is rejected", () => {
  const errors = classifyReceipt("x.json", realEnvReceipt({
    realEnvReadback: [{ kind: "vibes", target: "t-1", observedAt: "2026-10-13T10:00:00Z" }]
  }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /realEnvReadback\[0\]\.kind must be one of/u);
});

test("an unknown evidence layer is rejected rather than silently kept", () => {
  const errors = classifyReceipt("x.json", sourceReceipt({ evidenceLayer: "contract_and_source_check_only" }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /is not one of/u);
});

test("the layer enum stays closed", () => {
  assert.deepEqual([...evidenceLayers], ["source", "source_and_browser_fixture", "real_env"]);
});

// --- receipts must record that something ran -------------------------------

test("a receipt whose execution records no command is rejected", () => {
  const errors = classifyReceipt("x.json", sourceReceipt({ execution: { exitCode: 0 } }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /execution\.command must record the command that ran/u);
});

test("a receipt with no execution block at all is rejected", () => {
  const receipt = sourceReceipt();
  delete (receipt as Record<string, unknown>).execution;
  const errors = classifyReceipt("x.json", receipt);
  assert.ok(errors.some(error => /missing required field execution/u.test(error)));
});

test("a sourceBaseSha that is not a 40-character SHA is rejected", () => {
  const errors = classifyReceipt("x.json", sourceReceipt({ sourceBaseSha: "d94e541b" }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /not a 40-character Git SHA/u);
});

test("a non-timestamp recordedAt is rejected", () => {
  const errors = classifyReceipt("x.json", sourceReceipt({ recordedAt: "yesterday" }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /not an ISO-8601 timestamp/u);
});

test("a non-object receipt is rejected without throwing", () => {
  assert.deepEqual(classifyReceipt("x.json", ["not", "an", "object"]), ["x.json: not a JSON object"]);
});

// --- the vacuous-pass guard -------------------------------------------------
//
// `pullRequestChangedPaths` returns [] for a stale base SHA, and an empty step
// list exits 0. If this gate had the same shape, deleting every receipt would
// turn it green, and "no evidence exists" would read as "no evidence fails".

test("an empty evidence directory FAILS instead of passing vacuously", async () => {
  const root = await repository(null);
  try {
    const report = verifyEvidence(root);
    assert.equal(report.passed, false);
    assert.equal(report.errors.length, 1);
    assert.match(report.errors[0], /is empty/u);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("a missing evidence directory fails closed", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-evidence-missing-"));
  try {
    const report = verifyEvidence(root);
    assert.equal(report.passed, false);
    assert.match(report.errors[0], /missing evidence directory/u);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("invalid JSON fails rather than being skipped", async () => {
  const root = await repository({ "ok.json": sourceReceipt() });
  await writeFile(join(root, "docs/evidence/source-checks/broken.json"), "{ not json");
  try {
    const report = verifyEvidence(root);
    assert.equal(report.passed, false);
    assert.ok(report.errors.some(error => /invalid JSON/u.test(error)));
  } finally { await rm(root, { recursive: true, force: true }); }
});

// --- the grandfather manifest ratchets ------------------------------------

test("a non-conformant receipt listed in the manifest is grandfathered", async () => {
  const root = await repository({ "legacy.json": { result: "old prose" } }, ["legacy.json"]);
  try {
    const report = verifyEvidence(root);
    assert.equal(report.passed, true);
    assert.equal(report.counts.grandfathered, 1);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("removing a receipt from the manifest makes the gate red again", async () => {
  // The ratchet: shrinking the manifest is how debt is retired, and a stale
  // entry cannot keep hiding a receipt forever.
  const root = await repository({ "legacy.json": { result: "old prose" } }, []);
  try {
    const report = verifyEvidence(root);
    assert.equal(report.passed, false);
    assert.equal(report.counts.failing, 1);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("a conformant receipt does not need a manifest entry", async () => {
  const root = await repository({ "ok.json": sourceReceipt() }, []);
  try {
    const report = verifyEvidence(root);
    assert.equal(report.passed, true);
    assert.equal(report.counts.conformant, 1);
  } finally { await rm(root, { recursive: true, force: true }); }
});

// --- the layer-appropriate field split -------------------------------------
//
// The real_env branch was widened once, when it rejected the repository's first
// real-environment receipt for lacking writeSet/verifiedSource/execution/
// sourceStage. Those fields describe a dev-session run against a diff; an
// environment observation has no such run behind it. The guarantee was carried
// over by `steps` instead. These controls exist so that widening cannot quietly
// become loosening: the source branch must stay exactly as strict as before.

test("a source receipt still requires the dev-session fields", () => {
  // The exact shape the first real_env receipt tripped over must NOT be
  // acceptable at the source layer. If this ever passes, the split leaked.
  for (const field of ["writeSet", "verifiedSource", "execution", "sourceStage"]) {
    const receipt = sourceReceipt();
    delete (receipt as Record<string, unknown>)[field];
    const errors = classifyReceipt("x.json", receipt);
    assert.ok(
      errors.some(error => error.includes(`missing required field ${field}`)),
      `a source receipt must still require ${field}`
    );
  }
});

test("a real_env receipt is accepted without the dev-session fields", () => {
  const receipt = realEnvReceipt();
  for (const field of ["writeSet", "verifiedSource", "execution", "sourceStage"]) {
    delete (receipt as Record<string, unknown>)[field];
  }
  assert.deepEqual(classifyReceipt("x.json", receipt), []);
});

test("a real_env receipt with no steps is rejected", () => {
  const receipt = realEnvReceipt({ steps: [] });
  const errors = classifyReceipt("x.json", receipt);
  assert.ok(errors.some(error => /requires a non-empty steps array/u.test(error)));
});

test("a real_env step that records no command is rejected", () => {
  const errors = classifyReceipt("x.json", realEnvReceipt({
    steps: [{ name: "owner read", exitCode: 0 }]
  }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /steps\[0\]\.command must record what was run/u);
});

test("a real_env step without an exit code is rejected", () => {
  const errors = classifyReceipt("x.json", realEnvReceipt({
    steps: [{ name: "owner read", command: "GET /api/gateway/usage-summary" }]
  }));
  assert.equal(errors.length, 1);
  assert.match(errors[0], /steps\[0\]\.exitCode must be a number/u);
});

test("a source receipt is not excused from having steps by the real_env rules", () => {
  const errors = classifyReceipt("x.json", sourceReceipt({ steps: [] }));
  assert.deepEqual(errors, []);
});

// --- the no-bypass guarantee -----------------------------------------------
// Nine `ci(candidate): admit the <sha> bridge ref` workflows were opened during
// the 2026-10-10 parallel run, each pinning repository + ref + product SHA into
// a dispatch escape hatch. This gate has no equivalent.

test("the gate exposes no force, skip or allow flag", async () => {
  const source = await readFile(new URL("../../tools/verify-evidence.ts", import.meta.url), "utf8");
  // Strip comments first. This file documents *why* there is no escape hatch,
  // so a naive substring scan would trip over its own rationale — which is
  // exactly what the first version of this test did.
  const code = source.replace(/\/\*[\s\S]*?\*\//gu, "").replace(/^\s*\/\/.*$/gmu, "");
  for (const hatch of ["--force", "--skip", "--allow", "SKIP_EVIDENCE", "BYPASS", "process.env.EVIDENCE"]) {
    assert.ok(!code.includes(hatch), `gate must not grow a bypass hatch: ${hatch}`);
  }
  assert.ok(/process\.argv\.length !== 2/u.test(code), "verify:evidence accepts no arguments");
});