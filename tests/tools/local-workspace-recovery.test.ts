import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  createLocalJ1RecoveryReadbackPendingArtifact,
  localJ1CleanupPlan,
  localJ1FailureDisposition,
  localJ1RecoveryArtifactPath,
  noLocalJ1ExternalWrites,
  recoveryIdentityDigest,
  writeLocalJ1RecoveryArtifact
} from "../../tools/local-workspace-recovery.ts";

const source = { sha: "a".repeat(40), tree: "b".repeat(40) };
const operationId = "workspace-launch-sensitive";
const workspaceId = "ws-sensitive";

const noWrite = Object.freeze({ attempted: 0, confirmed: 0, unknown: 0 });
const confirmedWrite = Object.freeze({ attempted: 1, confirmed: 1, unknown: 0 });
const unknownWrite = Object.freeze({ attempted: 1, confirmed: 0, unknown: 1 });
const zeroWrites = Object.freeze({ workspaceCreates: noWrite, keyCreates: noWrite, debits: noWrite, providerMutations: noWrite, purchaseReceipts: noWrite });

test("local J1 cleanup is permitted only before Create acceptance or after READY", () => {
  assert.deepEqual(localJ1FailureDisposition({ launchSubmitted: false, externalWrites: zeroWrites }), {
    cleanup: true, preserveRecoveryAuthority: false, reason: "pre_create_failure"
  });
  assert.deepEqual(localJ1FailureDisposition({ ready: true, launchSubmitted: true, externalWrites: { ...zeroWrites, workspaceCreates: confirmedWrite, debits: confirmedWrite } }), {
    cleanup: true, preserveRecoveryAuthority: false, reason: "ready"
  });
  for (const scenario of [
    { name: "Create response unknown", writes: zeroWrites },
    { name: "post Create", writes: { ...zeroWrites, workspaceCreates: confirmedWrite } },
    { name: "post Key", writes: { ...zeroWrites, workspaceCreates: confirmedWrite, keyCreates: confirmedWrite } },
    { name: "post Debit", writes: { ...zeroWrites, workspaceCreates: confirmedWrite, keyCreates: confirmedWrite, debits: unknownWrite } },
    { name: "provider pending", writes: { ...zeroWrites, workspaceCreates: confirmedWrite, keyCreates: confirmedWrite, debits: confirmedWrite, providerMutations: unknownWrite } },
    { name: "receipt failure", writes: { ...zeroWrites, workspaceCreates: confirmedWrite, keyCreates: confirmedWrite, debits: confirmedWrite, providerMutations: { attempted: 5, confirmed: 5, unknown: 0 }, purchaseReceipts: unknownWrite } }
  ]) {
    assert.deepEqual(localJ1FailureDisposition({ launchSubmitted: true, externalWrites: scenario.writes }), {
      cleanup: false, preserveRecoveryAuthority: true, reason: "durable_launch_or_external_write"
    }, scenario.name);
  }
});

test("cleanup plan preserves running service and volume authority after Create submission", () => {
  assert.deepEqual(localJ1CleanupPlan({ launchSubmitted: false, authority: { externalWrites: noLocalJ1ExternalWrites() } }), {
    cleanup: true,
    preserveRecoveryAuthority: false,
    reason: "pre_create_failure",
    stopServices: ["control-plane", "fabric", "ledger", "postgres"],
    removeComposeVolumes: true,
    removeRecoveryRoot: true
  });
  assert.deepEqual(localJ1CleanupPlan({ launchSubmitted: true, authority: { externalWrites: { ...zeroWrites, workspaceCreates: unknownWrite } } }), {
    cleanup: false,
    preserveRecoveryAuthority: true,
    reason: "durable_launch_or_external_write",
    stopServices: [],
    removeComposeVolumes: false,
    removeRecoveryRoot: false
  });
});

test("recovery artifact path is deterministic beside the qualification receipt", () => {
  assert.equal(localJ1RecoveryArtifactPath("/tmp/opl-local-ready.json"), "/tmp/opl-local-ready.recovery.json");
  assert.throws(() => localJ1RecoveryArtifactPath("relative.json"), /absolute JSON path/);
});

test("0600 recovery artifact binds exact source, recovery refs, and only identity digests", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-j1-recovery-test-"));
  const path = join(root, "recovery.json");
  try {
    const artifact = createLocalJ1RecoveryReadbackPendingArtifact({
      source,
      failure: { stage: "workspace_launch", errorCode: "local_workspace_qualification_failed" },
      operationId,
      workspaceId,
      compose: {
        project: "opl-local-qualification-123",
        recoveryRoot: "/tmp/opl-local-qualification-123",
        fabricSecretRoot: "/tmp/opl-local-qualification-123/fabric-secrets",
        postgresVolumeRefs: ["opl-local-qualification-123_postgres-data"],
        providerVolumeRefs: ["opl-workspace-volume-123"]
      }
    });
    await writeLocalJ1RecoveryArtifact(path, artifact);
    assert.equal((await stat(path)).mode & 0o777, 0o600);
    const serialized = await readFile(path, "utf8");
    const parsed = JSON.parse(serialized);
    assert.equal(parsed.status, "RECOVERY_READBACK_REQUIRED");
    assert.equal(parsed.cleanup.performed, false);
    assert.deepEqual(parsed.source, source);
    for (const forbidden of [operationId, workspaceId, "password", "token"]) {
      assert.equal(serialized.includes(forbidden), false, forbidden);
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("readback-unavailable recovery artifact still preserves exact refs and only identity digests", () => {
  const artifact = createLocalJ1RecoveryReadbackPendingArtifact({
    source,
    failure: { stage: "workspace_launch", errorCode: "local_workspace_qualification_failed" },
    operationId,
    workspaceId,
    compose: {
      project: "opl-local-qualification-123",
      recoveryRoot: "/tmp/opl-local-qualification-123",
      fabricSecretRoot: "/tmp/opl-local-qualification-123/fabric-secrets",
      postgresVolumeRefs: ["opl-local-qualification-123_opl-cloud-postgres"],
      providerVolumeRefs: []
    }
  });
  assert.equal(artifact.status, "RECOVERY_READBACK_REQUIRED");
  assert.deepEqual(artifact.authority.externalWrites, {
    workspaceCreates: { attempted: null, confirmed: null, unknown: null },
    keyCreates: { attempted: null, confirmed: null, unknown: null },
    debits: { attempted: null, confirmed: null, unknown: null },
    providerMutations: { attempted: null, confirmed: null, unknown: null },
    purchaseReceipts: { attempted: null, confirmed: null, unknown: null }
  });
  assert.equal(artifact.cleanup.performed, false);
  assert.equal(JSON.stringify(artifact).includes(operationId), false);
  assert.equal(JSON.stringify(artifact).includes(workspaceId), false);
});

test("a lost owner response preserves the exact replay request in the private recovery artifact", async () => {
  const declaration = {
    applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime-original" },
    computePlanId: "compute-original", storagePlanId: "storage-original", periodMonths: 1, modelSelections: [],
  };
  const artifact = createLocalJ1RecoveryReadbackPendingArtifact({
    source, failure: { stage: "workspace_order", errorCode: "local_workspace_qualification_failed" },
    idempotencyKey: "original-lost-request", quoteId: "quote-original", workspaceName: "Original qualification", declaration,
    compose: { project: "opl-isolated", recoveryRoot: "/tmp/opl-isolated", fabricSecretRoot: "/tmp/opl-isolated/secrets",
      postgresVolumeRefs: ["opl-isolated_postgres"], providerVolumeRefs: [] },
  });
  assert.equal(artifact.authority.operationIdDigest, null);
  assert.equal(artifact.authority.workspaceIdDigest, null);
  assert.equal(artifact.cleanup.performed, false);
  assert.deepEqual(artifact.replay, {
    owner: "workspace", idempotencyKey: "original-lost-request", quoteId: "quote-original", operationId: null,
    workspaceId: null, name: "Original qualification", declaration,
  });
});
