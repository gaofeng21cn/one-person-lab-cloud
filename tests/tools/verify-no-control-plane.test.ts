import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  assertions,
  currentSlice,
  sliceOrder,
  stageTable,
  verifyNoControlPlane
} from "../../tools/verify-no-control-plane.ts";

const slicePosition = (slice: string) => sliceOrder.indexOf(slice as (typeof sliceOrder)[number]);

// The sample path is assembled from parts so this file does not itself carry a
// literal the gate hunts for: the gate scans the whole repository, and its own
// test must not look like residue.
const retiredPath = ["services", "control-plane"].join("/");

const stageIds = Object.entries(stageTable)
  .filter(([slice]) => slicePosition(slice) <= slicePosition(currentSlice))
  .flatMap(([, ids]) => ids)
  .sort();

test("the retirement residue gate runs green over the assertions that have arrived", () => {
  const result = verifyNoControlPlane();
  assert.deepEqual(result.metaFailures, []);
  assert.equal(result.passed, true);
  assert.deepEqual(result.failures, []);
  assert.deepEqual([...result.active].sort(), stageIds);
  assert.deepEqual(
    result.pending.map((pending) => pending.id).sort(),
    assertions.map((assertion) => assertion.id).filter((id) => !stageIds.includes(id)).sort()
  );
  for (const pending of result.pending) {
    const assertion = assertions.find((candidate) => candidate.id === pending.id);
    assert.equal(assertion?.fromSlice, pending.fromSlice);
  }
});

test("the stage table and the fromSlice markers declare the same plan", () => {
  const listed = Object.entries(stageTable).flatMap(([slice, ids]) => ids.map((id) => ({ id, slice })));
  assert.equal(listed.length, assertions.length);
  for (const assertion of assertions) {
    const stages = listed.filter((entry) => entry.id === assertion.id).map((entry) => entry.slice);
    assert.deepEqual(stages, [assertion.fromSlice], assertion.id);
  }
});

test("every declared assertion executes and reports a well-formed outcome", () => {
  for (const assertion of assertions) {
    const outcome = assertion.check(process.cwd());
    assert.equal(typeof outcome.detail, "string", assertion.id);
    assert.ok(outcome.detail.length > 0, assertion.id);
    assert.ok(Array.isArray(outcome.findings), assertion.id);
    for (const finding of outcome.findings) {
      assert.equal(typeof finding.file, "string", assertion.id);
      assert.equal(typeof finding.line, "number", assertion.id);
      assert.equal(typeof finding.text, "string", assertion.id);
    }
  }
});

test("a bridge scan reports the residue it finds and nothing else", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-residue-gate-"));
  try {
    const bridge = assertions.find((assertion) => assertion.id === "legacy-billing-bridges-absent");
    assert.ok(bridge);
    assert.deepEqual(bridge.check(root).findings, []);

    // The sample path is assembled from parts so this file does not itself
    // carry a literal the gate hunts for: the gate scans the whole repository,
    // and its own test must not look like residue.
    const bridgePath = ["/internal", "/cloud/legacy-billing/receipts"].join("");
    const client = join(root, "apps/console-bff/internal/clients/legacy_billing.go");
    await mkdir(join(root, "apps/console-bff/internal/clients"), { recursive: true });
    await writeFile(client, `const path = "${bridgePath}"\n`, "utf8");
    const findings = bridge.check(root).findings;
    assert.equal(findings.length, 1);
    assert.equal(findings[0].file, "apps/console-bff/internal/clients/legacy_billing.go");
    assert.equal(findings[0].line, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the directory scan reports an existing Control Plane directory", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-residue-gate-"));
  try {
    const directory = assertions.find((assertion) => assertion.id === "control-plane-directory-absent");
    assert.ok(directory);
    assert.deepEqual(directory.check(root).findings, []);

    await mkdir(join(root, retiredPath), { recursive: true });
    const findings = directory.check(root).findings;
    assert.equal(findings.length, 1);
    assert.equal(findings[0].file, retiredPath);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the repository path scan reports every file that still names the retired directory", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-residue-gate-"));
  try {
    const paths = assertions.find((assertion) => assertion.id === "repo-paths-no-control-plane");
    assert.ok(paths);
    assert.deepEqual(paths.check(root).findings, []);

    const target = join(root, "sqlc.yaml");
    await writeFile(target, `schema: ${retiredPath}/migrations/x.sql\n`, "utf8");
    const findings = paths.check(root).findings;
    assert.equal(findings.length, 1);
    assert.equal(findings[0].file, "sqlc.yaml");
    assert.equal(findings[0].line, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
