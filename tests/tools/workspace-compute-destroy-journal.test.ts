import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import {
  computeDestroyJournalSql,
  decodeComputeDestroyJournal,
  validateComputeDestroyJournal,
} from "../../tools/workspace-compute-destroy-journal.ts";

const workspaceId = "workspace_original";
const computeId = "compute_original";
const accountId = "account_original";
const destroyOperationId = "op_destroy_original";
const finalReleaseOperationId = "op_final_release";
const requestKeys = [
  "deleteMethod", "scaleDown", "deleteMode",
  "describeClusterMachinesReq",
  "describeNodePoolRequestId",
  "modifySelfProvisioningReqId",
  "verifyMachineDeletedReqId",
] as const;

function digest(value: string): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function deleteRequests(values: Partial<Record<(typeof requestKeys)[number], string | null>> = {}) {
  return Object.fromEntries(requestKeys.map(key => [key, values[key] ?? null]));
}

function destroyRow(overrides: Record<string, unknown> = {}) {
  return {
    id: "fop_destroy_1",
    operationId: destroyOperationId,
    resourceId: computeId,
    workspaceId,
    accountId,
    action: "destroy_compute_allocation",
    resourceKind: "compute_allocation",
    status: "failed",
    errorCode: "compute_destroy_recovery_unconfirmed: provider detail must not escape",
    retryable: true,
    providerRequestId: "provider-request-secret",
    requestHash: "request-hash-secret",
    startedAt: "2026-11-10T01:00:00Z",
    finishedAt: "2026-11-10T01:01:00Z",
    createdAt: "2026-11-10T01:01:00Z",
    originalDestroyOperationId: null,
    resource: { id: computeId, workspaceId, accountId, provider: "tencent-tke" },
    deleteRequests: deleteRequests({ describeNodePoolRequestId: "node-pool-request-secret" }),
    phase: "delete_attempted_uncertain",
    mutationCount: "1",
    ...overrides,
  };
}

function finalReleaseRow(overrides: Record<string, unknown> = {}) {
  return {
    id: "fop_final_release_1",
    operationId: finalReleaseOperationId,
    resourceId: computeId,
    workspaceId,
    accountId,
    action: "release_recycled_compute_allocation",
    resourceKind: "compute_final_release",
    status: "failed",
    errorCode: "compute_final_release_unconfirmed",
    retryable: true,
    providerRequestId: "final-release-request-secret",
    requestHash: "final-release-hash-secret",
    startedAt: "2026-11-10T01:02:00Z",
    finishedAt: "2026-11-10T01:03:00Z",
    createdAt: "2026-11-10T01:03:00Z",
    originalDestroyOperationId: destroyOperationId,
    resource: null,
    deleteRequests: deleteRequests(),
    phase: null,
    mutationCount: null,
    ...overrides,
  };
}

function rows(...values: Record<string, unknown>[]): unknown[] {
  return values.map(value => JSON.stringify(value));
}

function decode(...values: Record<string, unknown>[]) {
  return decodeComputeDestroyJournal(rows(...values), { computeId, workspaceId }) as unknown as Record<string, unknown>;
}

function recordOf(journal: Record<string, unknown>, index = 0): Record<string, unknown> {
  return (journal.records as Record<string, unknown>[])[index];
}

test("SQL is read-only and projects all journal binding and request evidence", () => {
  const sql = computeDestroyJournalSql(computeId);
  assert.match(sql, /^SELECT /u);
  assert.match(sql, /originalDestroyOperationId/u);
  assert.match(sql, /resource/u);
  assert.match(sql, /deleteRequests/u);
  assert.match(sql, /computeDestroyPhase/u);
  assert.match(sql, /deleteMutationCount/u);
  assert.match(sql, /LIMIT 101/u);
  assert.doesNotMatch(sql, /\b(?:INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE)\b/u);
});

test("empty journal remains unknown and does not fabricate an account or destroy operation", () => {
  const journal = decodeComputeDestroyJournal([], { computeId, workspaceId }) as unknown as Record<string, unknown>;
  assert.deepEqual(journal, {
    status: "unknown",
    records: [],
    destroyOperationIdDigest: null,
    accountIdDigest: null,
    workspaceIdDigest: digest(`console-customer-workspace-readback:workspace:v1\0${workspaceId}`),
    computeIdDigest: digest(`compute-readback:compute:v1\0${computeId}`),
  });
});

test("started rows with an empty error code are accepted without treating missing count as zero", () => {
  const journal = decode(
    destroyRow({
      status: "started",
      errorCode: "",
      retryable: false,
      finishedAt: null,
      phase: "dispatch_authorized_uncertain",
      mutationCount: null,
      deleteRequests: deleteRequests({ describeNodePoolRequestId: "request-secret" }),
    }),
  );
  const record = recordOf(journal);
  assert.equal(journal.status, "observed");
  assert.equal(record.status, "started");
  assert.equal(record.errorToken, null);
  assert.equal(record.errorDigest, null);
  assert.equal(record.mutationCount, null);
  assert.equal(record.providerRequestPresent, true);
  assert.equal((journal.accountIdDigest as string).startsWith("sha256:"), true);
});

test("missing mutation count stays unknown even when provider request evidence exists", () => {
  const journal = decode(destroyRow({ mutationCount: null, phase: "delete_attempted_uncertain" }));
  const record = recordOf(journal);
  assert.equal(record.mutationCount, null);
  assert.equal(record.providerRequestPresent, true);
  assert.equal(Object.hasOwn(record, "sent"), false);
  assert.equal(Object.hasOwn(record, "dispatchConfirmed"), false);
});

test("final release carries a digest of its exact original destroy operation", () => {
  const journal = decode(destroyRow(), finalReleaseRow());
  const release = recordOf(journal, 1);
  assert.equal(release.action, "release_recycled_compute_allocation");
  assert.equal(release.originalDestroyOperationIdDigest, digest(`cloud-launch-readback:operation:v1\0${destroyOperationId}`));
  assert.equal(journal.destroyOperationIdDigest, digest(`cloud-launch-readback:operation:v1\0${destroyOperationId}`));
  assert.equal(release.originalDestroyOperationIdDigest, journal.destroyOperationIdDigest);
});

test("final release with a foreign original destroy operation is rejected", () => {
  assert.throws(() => decode(destroyRow(), finalReleaseRow({ originalDestroyOperationId: "op_foreign_destroy" })), /invalid/u);
});

test("multiple destroy operation IDs are an identity conflict", () => {
  assert.throws(() => decode(
    destroyRow(),
    destroyRow({ id: "fop_destroy_2", operationId: "op_other_destroy" }),
  ), /invalid/u);
});

test("resource identity conflicts are rejected instead of being filtered away", () => {
  for (const resource of [
    { id: computeId, workspaceId: "workspace_other", accountId, provider: "tencent-tke" },
    { id: computeId, workspaceId, accountId: "account_other", provider: "tencent-tke" },
    { id: computeId, workspaceId, accountId, provider: [] },
    { id: "compute_other", workspaceId, accountId, provider: "tencent-tke" },
  ]) {
    assert.throws(() => decode(destroyRow({ resource })), /invalid/u);
  }
});

test("duplicate record IDs are rejected rather than deduplicated", () => {
  assert.throws(() => decode(destroyRow(), destroyRow({ createdAt: "2026-11-10T01:02:00Z" })), /invalid/u);
});

test("101 rows are rejected instead of being truncated", () => {
  const many = Array.from({ length: 101 }, (_, index) => destroyRow({ id: `fop_destroy_${index}` }));
  assert.throws(() => decode(...many), /invalid/u);
});

test("phase and mutation count accept only their projected JSON string forms", () => {
  for (const [field, value] of [
    ["phase", 1], ["phase", []], ["mutationCount", 1], ["mutationCount", []],
  ] as const) {
    assert.throws(() => decode(destroyRow({ [field]: value })), /invalid/u);
  }
});

test("fixed delete-request dictionary exports digests only", () => {
  const journal = decode(destroyRow({
    deleteRequests: deleteRequests({
      describeClusterMachinesReq: "cluster-request-secret",
      describeNodePoolRequestId: "node-pool-request-secret",
    }),
  }));
  const record = recordOf(journal);
  const requests = record.requestEvidenceDigests as Record<string, unknown>;
  assert.deepEqual(Object.keys(requests).sort(), [...requestKeys].sort());
  assert.match(String(requests.describeClusterMachinesReq), /^sha256:[0-9a-f]{64}$/u);
  assert.match(String(requests.describeNodePoolRequestId), /^sha256:[0-9a-f]{64}$/u);
  assert.equal(requests.modifySelfProvisioningReqId, null);
  assert.equal(requests.deleteMethod, null);
  assert.equal(requests.scaleDown, null);
  assert.equal(requests.deleteMode, null);
  assert.doesNotMatch(JSON.stringify(journal), /cluster-request-secret|node-pool-request-secret/u);
});

test("raw resource, account, provider, request and hash values never enter the decoded receipt", () => {
  const journal = decode(destroyRow());
  const text = JSON.stringify(journal);
  for (const secret of [
    computeId, workspaceId, accountId, "tencent-tke", "provider-request-secret", "request-hash-secret",
    "provider detail must not escape", "node-pool-request-secret",
  ]) assert.equal(text.includes(secret), false, secret);
  assert.match(String(journal.accountIdDigest), /^sha256:[0-9a-f]{64}$/u);
});

test("strict receipt validation rejects leaked keys, forged digests and contradictory aggregates", () => {
  const journal = decode(destroyRow(), finalReleaseRow());
  validateComputeDestroyJournal(journal);
  const changes: Array<(value: Record<string, unknown>) => void> = [
    value => { value.rawAccountId = accountId; },
    value => { delete value.accountIdDigest; },
    value => { value.accountIdDigest = accountId; },
    value => { value.workspaceIdDigest = "not-a-digest"; },
    value => { value.status = "unknown"; },
    value => { value.destroyOperationIdDigest = null; },
    value => { (value.records as Record<string, unknown>[])[0].phaseDigest = "raw-phase"; },
    value => { (value.records as Record<string, unknown>[])[0].requestEvidenceDigests = { secret: "raw-request" }; },
    value => { (value.records as Record<string, unknown>[])[1].originalDestroyOperationIdDigest = digest("foreign"); },
    value => { (value.records as Record<string, unknown>[])[0].errorToken = "private-error"; },
    value => { (value.records as Record<string, unknown>[])[0].errorDigest = null; },
  ];
  for (const change of changes) {
    const altered = structuredClone(journal);
    change(altered);
    assert.throws(() => validateComputeDestroyJournal(altered), /invalid/u);
  }
});
