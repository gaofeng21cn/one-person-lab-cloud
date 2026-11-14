import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test, { type TestContext } from "node:test";

import { candidateReceipt } from "../helpers/protected-qualification-fixture.ts";
import {
  produceWorkspaceFabricDispatchErrorReadback,
  validateWorkspaceFabricDispatchErrorReadbackReceipt,
} from "../../tools/workspace-fabric-dispatch-error-readback.ts";

type DispatchReceipt = ReturnType<typeof produceWorkspaceFabricDispatchErrorReadback>;
const STATUS = Object.freeze({
  MATCHED: "matched", BLOCKED: "blocked", UNKNOWN: "unknown",
} satisfies Record<string, DispatchReceipt["status"]>);
const ERROR = Object.freeze({ INVALID: "workspace_fabric_dispatch_error_readback_invalid" } as const);
const ERROR_TOKEN = Object.freeze({
  STATUS_MISMATCH: "compute_destroy_status_readback_mismatch",
  FABRIC_IDENTITY: "compute_destroy_status_fabric_identity_mismatch",
  RETRY_EVIDENCE: "compute_destroy_status_retry_evidence_invalid",
  PROVIDER_IDENTITY: "compute_destroy_status_provider_identity_mismatch",
  PROVIDER_ABSENCE: "compute_destroy_status_provider_absence_mismatch",
  FABRIC_ABSENCE_EVIDENCE: "compute_destroy_status_fabric_absence_evidence_invalid",
  ALLOCATION_MISSING: "compute_allocation_not_found",
  OWNERSHIP_MISSING: "compute_readback_ownership_missing",
} as const);
const EXIT = Object.freeze({ SUCCESS: 0, FAILURE: 1 } as const);
const toolPath = fileURLToPath(new URL("../../tools/workspace-fabric-dispatch-error-readback.ts", import.meta.url));

const workspaceId = "workspace_bfa77aedf1dd7d2c572d73efe80abea3";
const workspaceOperationId = "op_d39e87df694c4ff159c6dcb93b654046";
const fabricOperationId = "fabric-op-compute-1042";
const otherFabricOperationId = "fabric-op-other-1042";
const startedAt = "2026-11-09T01:50:00Z";
const completedAt = "2026-11-09T01:52:00Z";
const line = (operationId: string, error: string) =>
  `2026-11-09T01:51:56Z 2026/11/09 09:51:56 fabric resource deletion unavailable operation=${operationId} kind=compute: ${error}`;

function excerpt(text: string) {
  return {
    text,
    sha256: `sha256:${createHash("sha256").update(text).digest("hex")}`,
    startedAt,
    completedAt,
  };
}

function input(text: string) {
  return {
    candidate: candidateReceipt(),
    binding: { workspaceId, workspaceOperationId, fabricOperationId },
    fabricOperationUpdatedAt: "2026-11-09T01:51:56Z",
    excerpt: excerpt(text),
  };
}

test("emits a matched Cloud receipt without provider or operation identifiers", () => {
  const raw = line(fabricOperationId, "compute_destroy_status_readback_mismatch: provider response omitted required identity");
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(raw));
  assert.equal(receipt.status, STATUS.MATCHED);
  assert.equal(receipt.matchedCount, 1);
  assert.deepEqual(receipt.matches, [{
    observedAt: "2026-11-09T01:51:56Z",
    errorToken: ERROR_TOKEN.STATUS_MISMATCH,
    errorDigest: `sha256:${createHash("sha256").update("compute_destroy_status_readback_mismatch: provider response omitted required identity").digest("hex")}`,
  }]);
  const output = JSON.stringify(receipt);
  assert.doesNotMatch(output, new RegExp(`${workspaceId}|${workspaceOperationId}|${fabricOperationId}`));
  assert.doesNotMatch(output, /fabric resource deletion unavailable|compute_destroy_status_readback_mismatch.*\s/);
  assert.match(receipt.workspaceIdDigest, /^sha256:[0-9a-f]{64}$/);
  assert.equal(validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify(receipt))).status, STATUS.MATCHED);
});

test("isolates the exact Fabric operation in mixed excerpts", () => {
  const raw = [
    line(otherFabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING),
    line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING),
    "2026/11/09 09:51:57 fabric resource deletion unavailable operation=" + fabricOperationId + " kind=storage: private-storage-error",
  ].join("\n");
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(raw));
  assert.equal(receipt.status, STATUS.MATCHED);
  assert.equal(receipt.matchedCount, 1);
  assert.equal(receipt.matches[0].errorToken, ERROR_TOKEN.ALLOCATION_MISSING);
  assert.doesNotMatch(JSON.stringify(receipt), /private-storage-error|fabric-op-other-1042/);
});

test("unknown raw errors are digested and block the receipt", () => {
  const secretError = "provider returned credential=top-secret token=never-persist-this";
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(line(fabricOperationId, secretError)));
  assert.equal(receipt.status, STATUS.BLOCKED);
  assert.equal(receipt.matchedCount, 1);
  assert.equal(receipt.matches[0].errorToken, null);
  assert.equal(receipt.matches[0].errorDigest, `sha256:${createHash("sha256").update(secretError).digest("hex")}`);
  assert.doesNotMatch(JSON.stringify(receipt), /top-secret|never-persist-this|credential|provider returned/);
  assert.throws(() => validateWorkspaceFabricDispatchErrorReadbackReceipt({ ...receipt, status: STATUS.MATCHED }));
});

test("projects fixed read-only compute mismatch stages without raw provider facts", () => {
  for (const errorToken of [ERROR_TOKEN.FABRIC_IDENTITY, ERROR_TOKEN.RETRY_EVIDENCE, ERROR_TOKEN.PROVIDER_IDENTITY,
    ERROR_TOKEN.PROVIDER_ABSENCE, ERROR_TOKEN.FABRIC_ABSENCE_EVIDENCE]) {
    const receipt = produceWorkspaceFabricDispatchErrorReadback(input(line(fabricOperationId, errorToken)));
    assert.equal(receipt.status, STATUS.MATCHED);
    assert.equal(receipt.matches[0].errorToken, errorToken);
    assert.doesNotMatch(JSON.stringify(receipt), /workspace_bfa|op_d39e|fabric-op-compute/);
  }
});

test("no exact operation match remains unknown without inventing an error", () => {
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(line(otherFabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)));
  assert.equal(receipt.status, STATUS.UNKNOWN);
  assert.equal(receipt.matchedCount, 0);
  assert.deepEqual(receipt.matches, []);
});

test("an exact operation line outside its update window is excluded", () => {
  const stale = `2026-11-09T01:40:00Z 2026/11/09 09:40:00 fabric resource deletion unavailable operation=${fabricOperationId} kind=compute: compute_allocation_not_found`;
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(stale));
  assert.equal(receipt.status, STATUS.UNKNOWN);
  assert.equal(receipt.matchedCount, 0);
});

test("rejects malformed JSON-boundary input and receipt shape drift", () => {
  const valid = produceWorkspaceFabricDispatchErrorReadback(input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)));
  const decoded = JSON.parse(JSON.stringify(valid));
  decoded.matches[0].rawError = "must-not-be-accepted";
  assert.throws(() => validateWorkspaceFabricDispatchErrorReadbackReceipt(decoded));
  assert.throws(() => produceWorkspaceFabricDispatchErrorReadback({
    ...input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)),
    excerpt: { ...excerpt(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)), sha256: "sha256:" + "0".repeat(64) },
  }));
  assert.throws(() => produceWorkspaceFabricDispatchErrorReadback({
    ...input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)),
    binding: { workspaceId, workspaceOperationId, fabricOperationId, providerAccountId: "acct-secret" },
  }));
  assert.throws(() => produceWorkspaceFabricDispatchErrorReadback({
    ...input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)),
    excerpt: { ...excerpt(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)), text: "not a dispatcher line" },
  }));
});

function matchedReceipt(): DispatchReceipt {
  return produceWorkspaceFabricDispatchErrorReadback(input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)));
}

function timestampedLine(observedAt: string, error: string = ERROR_TOKEN.ALLOCATION_MISSING): string {
  return `${observedAt} 2026/11/09 09:51:56 fabric resource deletion unavailable operation=${fabricOperationId} kind=compute: ${error}`;
}

for (const observedAt of [
  "2026-11-09T01:50:55Z",
  "2026-11-09T01:50:55.999999999Z",
  "2026-11-09T01:52:56.000000001Z",
  "2026-11-09T01:52:57Z",
]) {
  test(`receipt validator rejects a match outside the operation update window: ${observedAt}`, () => {
    const receipt = matchedReceipt();
    // Leave completedAt beyond both bounds to isolate the operation window.
    receipt.completedAt = "2026-11-09T01:54:00Z";
    receipt.matches[0].observedAt = observedAt;
    assert.throws(() => validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify(receipt))),
      { message: ERROR.INVALID });
  });
}

for (const observedAt of ["2026-11-09T01:50:56Z", "2026-11-09T01:52:56Z"]) {
  test(`receipt validator admits the inclusive operation update boundary: ${observedAt}`, () => {
    const receipt = matchedReceipt();
    receipt.completedAt = "2026-11-09T01:54:00Z";
    receipt.matches[0].observedAt = observedAt;
    assert.equal(validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify(receipt))).status, STATUS.MATCHED);
  });
}

for (const observedAt of ["2026-11-09T01:52:01Z", "2026-11-09T01:52:00.000000001Z"]) {
  test(`receipt validator rejects observations after excerpt completion: ${observedAt}`, () => {
    const receipt = matchedReceipt();
    receipt.matches[0].observedAt = observedAt;
    assert.throws(() => validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify(receipt))),
      { message: ERROR.INVALID });
  });
}

test("producer rejects a target observation later than excerpt completion", () => {
  assert.throws(() => produceWorkspaceFabricDispatchErrorReadback(input(timestampedLine("2026-11-09T01:52:01Z"))),
    { message: ERROR.INVALID });
});

for (const field of ["fabricOperationUpdatedAt", "startedAt", "completedAt", "observedAt"] as const) {
  test(`receipt validator rejects an impossible calendar date in ${field}`, () => {
    const receipt = matchedReceipt();
    // Keep all times mutually consistent so Date.parse rollover alone cannot pass.
    receipt.fabricOperationUpdatedAt = "2026-02-30T01:51:56Z";
    receipt.startedAt = "2026-03-02T01:50:00Z";
    receipt.completedAt = "2026-03-02T01:52:00Z";
    receipt.matches[0].observedAt = "2026-03-02T01:51:56Z";
    if (field !== "fabricOperationUpdatedAt") {
      receipt.fabricOperationUpdatedAt = "2026-03-02T01:51:56Z";
      if (field === "observedAt") receipt.matches[0].observedAt = "2026-02-30T01:51:56Z";
      else receipt[field] = `2026-02-30T${field === "startedAt" ? "01:50:00" : "01:52:00"}Z`;
    }
    assert.throws(() => validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify(receipt))),
      { message: ERROR.INVALID });
  });
}

for (const field of ["fabricOperationUpdatedAt", "startedAt", "completedAt", "observedAt"] as const) {
  test(`producer rejects an impossible calendar date in ${field}`, () => {
    const value = input(timestampedLine("2026-03-02T01:51:56Z"));
    value.fabricOperationUpdatedAt = "2026-03-02T01:51:56Z";
    value.excerpt.startedAt = "2026-03-02T01:50:00Z";
    value.excerpt.completedAt = "2026-03-02T01:52:00Z";
    if (field === "fabricOperationUpdatedAt") value.fabricOperationUpdatedAt = "2026-02-30T01:51:56Z";
    else if (field === "observedAt") Object.assign(value.excerpt, excerpt(timestampedLine("2026-02-30T01:51:56Z")), {
      startedAt: "2026-03-02T01:50:00Z", completedAt: "2026-03-02T01:52:00Z",
    });
    else value.excerpt[field] = `2026-02-30T${field === "startedAt" ? "01:50:00" : "01:52:00"}Z`;
    assert.throws(() => produceWorkspaceFabricDispatchErrorReadback(value), { message: ERROR.INVALID });
  });
}

for (const status of [STATUS.BLOCKED, STATUS.MATCHED]) {
  test(`receipt validator rejects zero matches with status ${status}`, () => {
    const receipt = produceWorkspaceFabricDispatchErrorReadback(input(line(otherFabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)));
    assert.equal(receipt.status, STATUS.UNKNOWN);
    assert.throws(() => validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify({ ...receipt, status }))),
      { message: ERROR.INVALID });
  });
}

test("target compute line with only a Go timestamp is rejected", () => {
  const raw = `2026/11/09 09:51:56 fabric resource deletion unavailable operation=${fabricOperationId} kind=compute: ${ERROR_TOKEN.ALLOCATION_MISSING}`;
  assert.throws(() => produceWorkspaceFabricDispatchErrorReadback(input(raw)), { message: ERROR.INVALID });
});

test("a foreign operation with only a Go timestamp cannot contaminate a valid target match", () => {
  const raw = [
    `2026/11/09 09:51:56 fabric resource deletion unavailable operation=${otherFabricOperationId} kind=compute: private-foreign-error`,
    line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING),
  ].join("\n");
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(raw));
  assert.equal(receipt.status, STATUS.MATCHED);
  assert.equal(receipt.matchedCount, 1);
  assert.doesNotMatch(JSON.stringify(receipt), /private-foreign-error/);
});

test("kubectl RFC3339 nanoseconds survive production and JSON-boundary validation", () => {
  const observedAt = "2026-11-09T01:51:56.123456789Z";
  const value = input(timestampedLine(observedAt));
  value.fabricOperationUpdatedAt = "2026-11-09T01:51:56.000000001Z";
  value.excerpt.startedAt = "2026-11-09T01:50:00.123456789Z";
  value.excerpt.completedAt = "2026-11-09T01:52:00.987654321Z";
  const receipt = produceWorkspaceFabricDispatchErrorReadback(value);
  assert.equal(receipt.status, STATUS.MATCHED);
  assert.equal(receipt.matches[0].observedAt, observedAt);
  assert.deepEqual(validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(JSON.stringify(receipt))), receipt);
});

test("recognizes the exact production ownership-missing error while hashing private details", () => {
  const rawError = `${ERROR_TOKEN.OWNERSHIP_MISSING}: private-account=must-stay-private`;
  const receipt = produceWorkspaceFabricDispatchErrorReadback(input(line(fabricOperationId, rawError)));
  assert.equal(receipt.status, STATUS.MATCHED);
  assert.equal(receipt.matches[0].errorToken, ERROR_TOKEN.OWNERSHIP_MISSING);
  assert.equal(receipt.matches[0].errorDigest, `sha256:${createHash("sha256").update(rawError).digest("hex")}`);
  assert.doesNotMatch(JSON.stringify(receipt), /private-account|must-stay-private/);
});

for (const rawError of [
  `prefix_${ERROR_TOKEN.OWNERSHIP_MISSING}`,
  `${ERROR_TOKEN.OWNERSHIP_MISSING}_suffix`,
  `private diagnostic mentions ${ERROR_TOKEN.OWNERSHIP_MISSING}`,
]) {
  test(`ownership allowlist refuses non-exact error text: ${rawError}`, () => {
    const receipt = produceWorkspaceFabricDispatchErrorReadback(input(line(fabricOperationId, rawError)));
    assert.equal(receipt.status, STATUS.BLOCKED);
    assert.equal(receipt.matches[0].errorToken, null);
    assert.doesNotMatch(JSON.stringify(receipt), /prefix_|_suffix|private diagnostic/);
  });
}

for (const key of ["candidate", "binding", "fabricOperationUpdatedAt", "excerpt"] as const) {
  test(`run input requires the ${key} top-level key`, () => {
    const value: Record<string, unknown> = input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING));
    delete value[key];
    assert.throws(() => produceWorkspaceFabricDispatchErrorReadback(value as Parameters<typeof produceWorkspaceFabricDispatchErrorReadback>[0]),
      { message: ERROR.INVALID });
  });
}

test("run input rejects additional top-level keys at the real JSON boundary", () => {
  const value = JSON.parse(JSON.stringify({
    ...input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)), rawError: "private-extra-field",
  }));
  assert.throws(() => produceWorkspaceFabricDispatchErrorReadback(value), { message: ERROR.INVALID });
});

async function cliFixture(t: TestContext, value: unknown) {
  const directory = await mkdtemp(join(tmpdir(), "opl-fabric-dispatch-error-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const inputPath = join(directory, "input.json");
  const outputPath = join(directory, "receipt.json");
  await writeFile(inputPath, JSON.stringify(value) + "\n", { mode: 0o600 });
  const run = (...args: string[]) => {
    const result = spawnSync(process.execPath, [toolPath, ...args], {
      cwd: directory, encoding: "utf8", shell: false, timeout: 15_000, maxBuffer: 1024 * 1024,
    });
    assert.equal(result.error, undefined);
    assert.equal(result.signal, null);
    return result;
  };
  return { directory, inputPath, outputPath, run };
}

function fixedFailure(result: ReturnType<typeof spawnSync>) {
  assert.equal(result.status, EXIT.FAILURE);
  assert.equal(result.stdout, "");
  assert.equal(result.stderr, `${ERROR.INVALID}\n`);
}

test("CLI run writes a private matched receipt, validate reads it, and wx prevents overwriting", async t => {
  const value = input(line(fabricOperationId, `${ERROR_TOKEN.OWNERSHIP_MISSING}: private-cli-error`));
  const fixture = await cliFixture(t, value);
  const first = fixture.run("run", "--input", fixture.inputPath, "--output", fixture.outputPath);
  assert.equal(first.status, EXIT.SUCCESS);
  const bytes = await readFile(fixture.outputPath, "utf8");
  const receipt = validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(bytes));
  assert.equal(receipt.status, STATUS.MATCHED);
  assert.equal(receipt.matches[0].errorToken, ERROR_TOKEN.OWNERSHIP_MISSING);
  assert.equal((await stat(fixture.outputPath)).mode & 0o777, 0o600);
  for (const forbidden of [workspaceId, workspaceOperationId, fabricOperationId, "private-cli-error"]) {
    assert.equal(bytes.includes(forbidden), false);
    assert.equal(`${first.stdout}${first.stderr}`.includes(forbidden), false);
  }
  const validation = fixture.run("validate", "--receipt", fixture.outputPath);
  assert.equal(validation.status, EXIT.SUCCESS);
  assert.equal(await readFile(fixture.outputPath, "utf8"), bytes);
  fixedFailure(fixture.run("run", "--input", fixture.inputPath, "--output", fixture.outputPath));
  assert.equal(await readFile(fixture.outputPath, "utf8"), bytes);
});

for (const [status, raw] of [
  [STATUS.UNKNOWN, line(otherFabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)],
  [STATUS.BLOCKED, line(fabricOperationId, "credential=private-cli-secret provider-response=private-cli-body")],
] as const) {
  test(`CLI run persists a redacted ${status} receipt and exits with failure`, async t => {
    const fixture = await cliFixture(t, input(raw));
    const result = fixture.run("run", "--input", fixture.inputPath, "--output", fixture.outputPath);
    assert.equal(result.status, EXIT.FAILURE);
    // Non-success diagnostics may be silent, but any text must be the fixed code.
    assert.equal(result.stdout, "");
    assert.ok(result.stderr === "" || result.stderr === `${ERROR.INVALID}\n`);
    const bytes = await readFile(fixture.outputPath, "utf8");
    const receipt = validateWorkspaceFabricDispatchErrorReadbackReceipt(JSON.parse(bytes));
    assert.equal(receipt.status, status);
    assert.equal(receipt.matchedCount, status === STATUS.UNKNOWN ? 0 : 1);
    assert.equal((await stat(fixture.outputPath)).mode & 0o777, 0o600);
    for (const forbidden of [workspaceId, workspaceOperationId, fabricOperationId, "private-cli-secret", "private-cli-body"]) {
      assert.equal(bytes.includes(forbidden), false);
      assert.equal(`${result.stdout}${result.stderr}`.includes(forbidden), false);
    }
  });
}

for (const invalid of [
  { ...input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)), rawError: "private-extra-input" },
  { ...input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)), binding: { workspaceId, workspaceOperationId, fabricOperationId, secret: "private-binding-secret" } },
]) {
  test("CLI rejects non-exact input without echoing raw values", async t => {
    const fixture = await cliFixture(t, invalid);
    fixedFailure(fixture.run("run", "--input", fixture.inputPath, "--output", fixture.outputPath));
    await assert.rejects(stat(fixture.outputPath), { code: "ENOENT" });
  });
}

test("CLI parse, file and argv errors expose only the fixed diagnostic", async t => {
  const fixture = await cliFixture(t, input(line(fabricOperationId, ERROR_TOKEN.ALLOCATION_MISSING)));
  await writeFile(fixture.inputPath, '{"private-malformed-json-secret":', { mode: 0o600 });
  for (const args of [
    ["run", "--input", fixture.inputPath, "--output", fixture.outputPath],
    ["run", "--input", join(fixture.directory, "private-missing-input"), "--output", fixture.outputPath],
    ["run", "--input", fixture.inputPath],
    ["run", "--input", fixture.inputPath, "--input", fixture.inputPath, "--output", fixture.outputPath],
    ["validate", "--receipt", fixture.inputPath],
    ["validate", "--receipt", join(fixture.directory, "private-missing-receipt")],
    ["private-unknown-command"],
  ]) fixedFailure(fixture.run(...args));
});

test("CLI validate rejects a forged receipt instead of accepting its matched status", async t => {
  const receipt = matchedReceipt();
  receipt.matches[0].observedAt = "2026-11-09T01:52:01Z";
  const fixture = await cliFixture(t, receipt);
  fixedFailure(fixture.run("validate", "--receipt", fixture.inputPath));
});
