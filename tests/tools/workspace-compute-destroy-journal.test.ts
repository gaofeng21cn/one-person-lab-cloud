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
  assert.match(sql, /status IN \('started','failed','succeeded'\)/u);
  assert.match(sql, /FROM fabric_operations WHERE status <> 'rejected' AND operation_id IN/u);
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

test("rejected journal rows stay outside the decoder contract", () => {
  const rejected = destroyRow({
    status: "rejected", errorCode: "compute_allocation_not_found", retryable: false,
    workspaceId: "", accountId: "", resource: null, finishedAt: "2026-11-10T01:01:00Z",
  });
  assert.throws(() => decode(rejected), /invalid/u);
  assert.throws(() => decode(destroyRow({ status: "rejected" })), /invalid/u);
  assert.doesNotThrow(() => decode(destroyRow()));
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

test("original provider deletion failures retain only fixed codes and full error digests", () => {
  for (const code of ["compute_machine_delete_unverified", "compute_cvm_delete_unverified", "compute_cvm_delete_identity_mismatch",
    "tencent_verify_compute_machine_delete_failed", "tencent_verify_cvm_delete_failed"]) {
    const raw = `${code}:provider detail token=never-export ins-private`;
    const journal = decode(destroyRow({ errorCode: raw }));
    assert.equal(recordOf(journal).errorToken, code);
    assert.equal(recordOf(journal).errorDigest, digest(raw));
    assert.doesNotMatch(JSON.stringify(journal), /never-export|ins-private|provider detail/);
    validateComputeDestroyJournal(journal);
  }
});

test("Tencent deletion rejection classes follow the SDK envelope without exposing message or request", () => {
  for (const [sdkCode, token] of [
    ["InvalidParameterValue", "tencent_compute_delete_invalid_parameter_value"],
    ["InvalidParameterValue.PrivateSubcode", "tencent_compute_delete_invalid_parameter_value"],
    ["UnsupportedOperation", "tencent_compute_delete_unsupported_operation"],
    ["UnsupportedOperation.PrivateSubcode", "tencent_compute_delete_unsupported_operation"],
    ["InternalError", "compute_machine_delete_unverified"],
  ]) {
    const raw = `compute_machine_delete_unverified:[TencentCloudSDKError] Code=${sdkCode}, Message=ins-private token=never-export, RequestId=private-request`;
    const journal = decode(destroyRow({ errorCode: raw }));
    assert.equal(recordOf(journal).errorToken, token);
    assert.equal(recordOf(journal).errorDigest, digest(raw));
    assert.doesNotMatch(JSON.stringify(journal), /PrivateSubcode|ins-private|never-export|private-request/);
    validateComputeDestroyJournal(journal);
  }
  for (const message of ["InvalidParameterValue", "UnsupportedOperation"]) {
    const journal = decode(destroyRow({ errorCode: `compute_machine_delete_unverified:${message}` }));
    assert.equal(recordOf(journal).errorToken, "compute_machine_delete_unverified");
  }
});

test("prepaid detach and retire journal safe Tencent rejection classes, never provider messages", () => {
  for (const action of ["detach_prepaid_compute_allocation", "retire_prepaid_compute_allocation"]) {
    for (const [providerCode, token] of [["AuthFailure", "tencent_prepaid_compute_auth_failure"],
      ["InvalidParameterValue", "tencent_prepaid_compute_invalid_parameter_value"],
      ["UnsupportedOperation", "tencent_prepaid_compute_unsupported_operation"],
      ["RequestLimitExceeded", "tencent_prepaid_compute_request_limit_exceeded"],
      ["InternalError", "tencent_prepaid_compute_internal_error"], ["ResourceNotFound", "tencent_prepaid_compute_resource_not_found"]]) {
      const raw = `${action === "detach_prepaid_compute_allocation" ? "compute_prepaid_detach" : "compute_prepaid_retire"}_dispatch_unconfirmed:${providerCode}`;
      const row = finalReleaseRow({ action, resourceKind: "compute_prepaid_retirement", errorCode: raw });
      const journal = decode(destroyRow(), row);
      assert.equal(recordOf(journal, 1).errorToken, token);
      assert.equal(recordOf(journal, 1).errorDigest, digest(raw));
      assert.doesNotMatch(JSON.stringify(journal), /UnauthorizedOperation|InstanceDeleteMode/u);
      validateComputeDestroyJournal(journal);
    }
  }
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

test("prepaid detach and retirement fences bind the same original destroy without claiming resource absence", () => {
  for (const action of ["detach_prepaid_compute_allocation", "retire_prepaid_compute_allocation"]) {
    const stage = finalReleaseRow({ action, resourceKind: "compute_prepaid_retirement" });
    const journal = decode(destroyRow(), stage);
    assert.equal(recordOf(journal, 1).action, action);
    assert.equal(recordOf(journal, 1).originalDestroyOperationIdDigest, journal.destroyOperationIdDigest);
    assert.equal(Object.hasOwn(recordOf(journal, 1), "absenceConfirmed"), false);
    assert.throws(() => decode(destroyRow(), { ...stage, originalDestroyOperationId: "op_foreign" }), /invalid/u);
    assert.throws(() => decode(destroyRow(), { ...stage, resourceKind: "compute_allocation" }), /invalid/u);
  }
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

const reconciliationAction = "reconcile_prepaid_compute_detachment";
function reconciliationRow(overrides: Record<string, unknown> = {}) {
  return finalReleaseRow({ id: "fop_reconciliation_1", operationId: "op_reconciliation_original",
    action: reconciliationAction, resourceKind: "compute_prepaid_reconciliation",
    errorCode: "compute_prepaid_detach_reconciliation_unconfirmed: SecretKey=never-export",
    retryable: false, providerRequestId: "reconciliation-provider-request-secret", requestHash: "reconciliation-request-hash-secret",
    ...overrides });
}

test("SQL includes independent reconciliation history through the same original resource binding", () => {
  const sql = computeDestroyJournalSql(computeId);
  assert.match(sql, /'reconcile_prepaid_compute_detachment'/u);
  assert.match(sql, /'originalDestroyOperationId',redacted_provider_payload::jsonb->'originalDestroyOperationId'/u);
  assert.ok(sql.includes(`resource_id='${computeId}'`));
  assert.doesNotMatch(sql, /SELECT \*/u);
  assert.doesNotMatch(sql, /SecretKey|authorizationReference|originalDetachRequestHash/u);
});

test("reconciliation retains original destroy/fence records unchanged and exports bound digests only", () => {
  const original = destroyRow();
  const detach = finalReleaseRow({ id: "fop_detach_original", operationId: "op_detach_original",
    action: "detach_prepaid_compute_allocation", resourceKind: "compute_prepaid_retirement",
    errorCode: "compute_prepaid_detach_dispatch_unconfirmed", retryable: false });
  const previous = decodeComputeDestroyJournal(rows(original, detach), { computeId, workspaceId });
  const journal = decodeComputeDestroyJournal(rows(reconciliationRow(), detach, original), { computeId, workspaceId });
  assert.equal(journal.status, "observed");
  for (const record of previous.records) assert.deepEqual(journal.records.find(value => value.recordIdDigest === record.recordIdDigest), record);
  const reconciliation = journal.records.find(value => value.action === reconciliationAction)!;
  assert.ok(reconciliation);
  assert.equal(reconciliation.originalDestroyOperationIdDigest, journal.destroyOperationIdDigest);
  assert.equal(reconciliation.operationIdDigest, digest("cloud-launch-readback:operation:v1\0op_reconciliation_original"));
  assert.notEqual(reconciliation.operationIdDigest, journal.destroyOperationIdDigest);
  assert.equal(reconciliation.errorToken, "compute_prepaid_detach_reconciliation_unconfirmed");
  assert.equal(reconciliation.errorDigest, digest(String(reconciliationRow().errorCode)));
  assert.equal(reconciliation.providerRequestDigest, digest("compute-readback:request:v1\0reconciliation-provider-request-secret"));
  assert.equal(reconciliation.requestHashDigest, digest("reconciliation-request-hash-secret"));
  assert.equal(reconciliation.mutationCount, null);
  assert.equal(Object.hasOwn(reconciliation, "dispatchConfirmed"), false);
  assert.equal(Object.hasOwn(reconciliation, "machineAbsent"), false);
  const text = JSON.stringify(journal);
  for (const secret of [accountId, workspaceId, computeId, destroyOperationId, "op_detach_original", "op_reconciliation_original",
    "reconciliation-provider-request-secret", "reconciliation-request-hash-secret", "SecretKey", "never-export"]) {
    assert.equal(text.includes(secret), false, secret);
  }
  assert.deepEqual(validateComputeDestroyJournal(JSON.parse(text)), journal);
});

test("reconciliation journal accepts actual nullable resource projection for started/failed/succeeded without inferring completion", () => {
  for (const status of ["started", "failed", "succeeded"]) {
    const raw = reconciliationRow({ status, errorCode: status === "failed" ? "compute_prepaid_detach_reconciliation_unconfirmed" : "",
      finishedAt: status === "started" ? null : "2026-11-10T01:03:00Z", providerRequestId: "", resource: null });
    const journal = decodeComputeDestroyJournal(rows(destroyRow(), raw), { computeId, workspaceId });
    const record = journal.records.find(value => value.action === reconciliationAction)!;
    assert.equal(record.status, status);
    assert.equal(record.providerRequestPresent, false);
    assert.equal(record.mutationCount, null);
    assert.equal(record.phase, null);
    assert.equal(record.errorToken, status === "failed" ? "compute_prepaid_detach_reconciliation_unconfirmed" : null);
  }
});

test("reconciliation cannot carry a foreign original destroy, owner, resource kind or unprojected secrets", () => {
  for (const change of [
    { originalDestroyOperationId: "op_foreign" }, { originalDestroyOperationId: null },
    { resourceId: "compute_foreign" }, { workspaceId: "workspace_foreign" }, { accountId: "account_foreign" },
    { resourceKind: "compute_prepaid_retirement" }, { resourceKind: "compute_allocation" },
    { originalDetachRequestHash: "raw-secret" }, { authorizationReference: "private-authorization" },
    { redactedProviderPayload: { originalDetachOperationId: "private-detach", secret: "never-export" } },
  ]) assert.throws(() => decode(destroyRow(), reconciliationRow(change)), /workspace_compute_destroy_journal_invalid/u);
});

test("strict reconciliation receipt rejects detached original binding and leaked identifiers", () => {
  const journal = decode(destroyRow(), reconciliationRow());
  for (const change of [
    { originalDestroyOperationIdDigest: digest("foreign") }, { originalDestroyOperationIdDigest: null },
    { accountId }, { authorizationReference: "private-authorization" }, { originalDetachRequestHash: "raw-secret" },
    { providerRequestDigest: "reconciliation-provider-request-secret" },
  ]) {
    const altered = structuredClone(journal);
    const record = (altered.records as Record<string, unknown>[]).find(value => value.action === reconciliationAction)!;
    Object.assign(record, change);
    assert.throws(() => validateComputeDestroyJournal(altered), /workspace_compute_destroy_journal_invalid/u);
  }
});
