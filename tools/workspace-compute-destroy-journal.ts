import { createHash } from "node:crypto";

const ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$(?![\s\S])/;
const DIGEST = /^sha256:[0-9a-f]{64}$/;
const PHASES = ["dispatch_authorized_uncertain", "delete_attempted_uncertain", "cloud_absence_confirmed"] as const;
const STATUSES = ["started", "failed", "succeeded"] as const;
const ACTIONS = {
  destroy_compute_allocation: "compute_allocation",
  release_recycled_compute_allocation: "compute_final_release",
  detach_prepaid_compute_allocation: "compute_prepaid_retirement",
  retire_prepaid_compute_allocation: "compute_prepaid_retirement",
} as const;
const SAFE_ERRORS = new Set([
  "compute_allocation_not_found",
  "compute_destroy_recovery_unconfirmed", "compute_destroy_recovery_identity_mismatch",
  "compute_destroy_recovery_phase_invalid", "compute_allocation_destroy_readback_mismatch",
  "compute_destroy_status_readback_unavailable", "compute_destroy_status_readback_mismatch",
  "compute_destroy_finalization_identity_mismatch", "compute_final_release_not_eligible", "compute_final_release_unconfirmed",
  "tencent_read_compute_destroy_machine_failed", "tencent_read_compute_destroy_cvm_failed",
  "compute_machine_delete_unverified", "compute_cvm_delete_unverified", "compute_cvm_delete_identity_mismatch",
  "tencent_verify_compute_machine_delete_failed", "tencent_verify_cvm_delete_failed",
  "tencent_compute_delete_invalid_parameter_value", "tencent_compute_delete_unsupported_operation",
  "compute_prepaid_retirement_required", "compute_prepaid_retirement_unconfirmed",
  "compute_destroy_charge_type_unverified", "compute_prepaid_retirement_unavailable",
  "compute_prepaid_detach_unavailable", "compute_prepaid_retire_unavailable",
  "compute_prepaid_retirement_identity_mismatch", "compute_prepaid_retirement_readback_mismatch",
  "compute_prepaid_retirement_dispatch_claim_invalid", "compute_prepaid_retirement_original_destroy_mismatch",
  "compute_prepaid_detach_identity_or_state_mismatch", "compute_prepaid_detach_dispatch_unconfirmed",
  "compute_prepaid_retire_identity_or_state_mismatch", "compute_prepaid_retire_dispatch_unconfirmed",
]);

export type ComputeDestroyJournalRecord = {
  recordIdDigest: string; operationIdDigest: string; action: keyof typeof ACTIONS;
  originalDestroyOperationIdDigest: string | null; phaseDigest: string | null;
  requestEvidenceDigests: {
    deleteMethod: string | null; scaleDown: string | null; deleteMode: string | null;
    describeClusterMachinesReq: string | null; describeNodePoolRequestId: string | null;
    modifySelfProvisioningReqId: string | null; verifyMachineDeletedReqId: string | null;
  };
  status: typeof STATUSES[number]; errorToken: string | null; errorDigest: string | null; retryable: boolean;
  phase: typeof PHASES[number] | null; mutationCount: number | null; providerRequestPresent: boolean;
  providerRequestDigest: string | null; requestHashDigest: string | null;
  startedAt: string; finishedAt: string | null; createdAt: string;
};
export type ComputeDestroyJournal = {
  status: "observed" | "unknown"; records: ComputeDestroyJournalRecord[]; destroyOperationIdDigest: string | null;
  accountIdDigest: string | null; workspaceIdDigest: string; computeIdDigest: string;
};

function fail(): never { throw new Error("workspace_compute_destroy_journal_invalid"); }
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail();
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: readonly string[]) {
  const object = record(value);
  if (Object.keys(object).length !== keys.length || keys.some(key => !Object.hasOwn(object, key))) fail();
  return object;
}
function hash(value: string) { return `sha256:${createHash("sha256").update(value).digest("hex")}`; }
function identity(domain: string, value: string) { return hash(`${domain}\0${value}`); }
function instant(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const match = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)$/.exec(value);
  if (!match || !Number.isFinite(Date.parse(value))) return false;
  const local = `${match[1]}.${(match[2] || "").padEnd(3, "0").slice(0, 3)}Z`;
  return new Date(local).toISOString() === local && (match[3] === "Z" ||
    Number(match[3].slice(1, 3)) <= 23 && Number(match[3].slice(4, 6)) <= 59);
}
function nullableText(value: unknown): value is string | null { return value === null || typeof value === "string" && value.length <= 4096; }

function errorToken(error: string): string | null {
  // Tencent's SDK Error() has this fixed envelope. Export only the two known
  // rejection classes needed to diagnose the original delete, never its
  // subcode, Message, RequestId or arbitrary provider text.
  const sdk = /^compute_machine_delete_unverified:\[TencentCloudSDKError\] Code=(InvalidParameterValue|UnsupportedOperation)(?:\.[A-Za-z0-9_]+)*, Message=/.exec(error);
  if (sdk) return sdk[1] === "InvalidParameterValue" ? "tencent_compute_delete_invalid_parameter_value" : "tencent_compute_delete_unsupported_operation";
  return [...SAFE_ERRORS].find(token => error === token || error.startsWith(`${token}:`)) ?? null;
}

export function computeDestroyJournalSql(computeId: string): string {
  if (typeof computeId !== "string" || !ID.test(computeId)) fail();
  const id = `'${computeId}'`;
  return `SELECT json_build_object(
    'id',id,'operationId',operation_id,'resourceId',resource_id,'workspaceId',workspace_id,
    'accountId',account_id,'action',action,'resourceKind',resource_kind,'status',status,
    'errorCode',error_code,'retryable',retryable,'providerRequestId',provider_request_id,
    'requestHash',request_hash,'startedAt',started_at,'finishedAt',finished_at,'createdAt',created_at,
    'phase',redacted_provider_payload::jsonb->'resource'->'providerData'->'computeDestroyPhase',
    'mutationCount',redacted_provider_payload::jsonb->'resource'->'providerData'->'deleteMutationCount',
    'resource',CASE WHEN redacted_provider_payload::jsonb->'resource' IS NULL THEN NULL ELSE jsonb_build_object('id',redacted_provider_payload::jsonb->'resource'->'id',
      'workspaceId',redacted_provider_payload::jsonb->'resource'->'workspaceId',
      'accountId',redacted_provider_payload::jsonb->'resource'->'accountId',
      'provider',redacted_provider_payload::jsonb->'resource'->'provider') END,
    'originalDestroyOperationId',redacted_provider_payload::jsonb->'originalDestroyOperationId',
    'deleteRequests',jsonb_build_object(
      'deleteMethod',redacted_provider_payload::jsonb->'resource'->'providerData'->'deleteMethod',
      'scaleDown',redacted_provider_payload::jsonb->'resource'->'providerData'->'scaleDown',
      'deleteMode',redacted_provider_payload::jsonb->'resource'->'providerData'->'deleteMode',
      'describeClusterMachinesReq',redacted_provider_payload::jsonb->'resource'->'providerData'->'describeClusterMachinesReq',
      'describeNodePoolRequestId',redacted_provider_payload::jsonb->'resource'->'providerData'->'describeNodePoolRequestId',
      'modifySelfProvisioningReqId',redacted_provider_payload::jsonb->'resource'->'providerData'->'modifySelfProvisioningReqId',
      'verifyMachineDeletedReqId',redacted_provider_payload::jsonb->'resource'->'providerData'->'verifyMachineDeletedReqId')
  )::text FROM fabric_operations WHERE status <> 'rejected' AND operation_id IN (
    SELECT operation_id FROM fabric_operations WHERE
      (resource_id=${id} OR redacted_provider_payload::jsonb->'resource'->'id'=to_jsonb(${id}::text))
      AND action IN ('destroy_compute_allocation','release_recycled_compute_allocation','detach_prepaid_compute_allocation','retire_prepaid_compute_allocation')
      AND status IN ('started','failed','succeeded')
  ) ORDER BY created_at,id LIMIT 101`;
}

function decodeRow(value: unknown, expected: { computeId: string; workspaceId: string }): ComputeDestroyJournalRecord & { accountIdDigest: string } {
  const row = exact(value, ["id", "operationId", "resourceId", "workspaceId", "accountId", "action", "resourceKind", "status", "errorCode", "retryable", "providerRequestId", "requestHash", "startedAt", "finishedAt", "createdAt", "phase", "mutationCount", "resource", "originalDestroyOperationId", "deleteRequests"]);
  if (typeof row.id !== "string" || !ID.test(row.id) || typeof row.operationId !== "string" || !ID.test(row.operationId) ||
      row.resourceId !== expected.computeId || row.workspaceId !== expected.workspaceId || typeof row.accountId !== "string" || !ID.test(row.accountId) ||
      typeof row.action !== "string" || !Object.hasOwn(ACTIONS, row.action) ||
      row.resourceKind !== ACTIONS[row.action as keyof typeof ACTIONS] ||
      !STATUSES.includes(row.status as never) || !(row.errorCode === "" || nullableText(row.errorCode)) || typeof row.retryable !== "boolean" ||
      !nullableText(row.providerRequestId) || !nullableText(row.requestHash) || !instant(row.startedAt) ||
      !(row.finishedAt === null || instant(row.finishedAt)) || !instant(row.createdAt) ||
      !(row.phase === null || typeof row.phase === "string" && PHASES.includes(row.phase as never)) ||
      !(row.mutationCount === null || typeof row.mutationCount === "string" && /^(?:0|[1-9][0-9]*)$/.test(row.mutationCount) && Number.isSafeInteger(Number(row.mutationCount)))) fail();
  const resource = row.resource === null ? null : exact(row.resource, ["id", "workspaceId", "accountId", "provider"]);
  if (resource !== null && (resource.id !== row.resourceId || resource.workspaceId !== row.workspaceId || resource.accountId !== row.accountId || typeof resource.provider !== "string")) fail();
  if (row.action === "destroy_compute_allocation" && resource === null || row.action !== "destroy_compute_allocation" && (typeof row.originalDestroyOperationId !== "string" || !ID.test(row.originalDestroyOperationId)) ||
      row.action === "destroy_compute_allocation" && row.originalDestroyOperationId !== null) fail();
  const requests = exact(row.deleteRequests, ["deleteMethod", "scaleDown", "deleteMode", "describeClusterMachinesReq", "describeNodePoolRequestId", "modifySelfProvisioningReqId", "verifyMachineDeletedReqId"]);
  if (Object.values(requests).some(value => !(value === null || typeof value === "string" && value.length <= 4096))) fail();
  const errorCode = row.errorCode as string | null;
  const providerRequestId = row.providerRequestId as string | null;
  const requestHash = row.requestHash as string | null;
  return {
    recordIdDigest: identity("compute-destroy-journal:record:v1", row.id),
    operationIdDigest: identity("cloud-launch-readback:operation:v1", row.operationId),
    originalDestroyOperationIdDigest: typeof row.originalDestroyOperationId === "string" ? identity("cloud-launch-readback:operation:v1", row.originalDestroyOperationId) : null,
    phaseDigest: typeof row.phase === "string" ? hash(row.phase) : null,
    requestEvidenceDigests: Object.fromEntries(Object.entries(requests).map(([key, value]) => [key,
      typeof value === "string" && value !== "" ? identity("compute-readback:request:v1", value) : null])) as ComputeDestroyJournalRecord["requestEvidenceDigests"],
    action: row.action as ComputeDestroyJournalRecord["action"], status: row.status as ComputeDestroyJournalRecord["status"],
    errorToken: errorCode ? errorToken(errorCode) : null,
    errorDigest: errorCode ? hash(errorCode) : null, retryable: row.retryable,
    phase: row.phase as ComputeDestroyJournalRecord["phase"],
    mutationCount: row.mutationCount === null ? null : Number(row.mutationCount),
    providerRequestPresent: Boolean(providerRequestId),
    providerRequestDigest: providerRequestId ? identity("compute-readback:request:v1", providerRequestId) : null,
    requestHashDigest: requestHash ? hash(requestHash) : null,
    startedAt: row.startedAt as string, finishedAt: row.finishedAt as string | null, createdAt: row.createdAt as string,
    accountIdDigest: identity("compute-destroy-journal:account:v1", row.accountId as string),
  };
}

export function decodeComputeDestroyJournal(rows: unknown, expected: { computeId: string; workspaceId: string }): ComputeDestroyJournal {
  if (typeof expected.computeId !== "string" || !ID.test(expected.computeId) || typeof expected.workspaceId !== "string" || !ID.test(expected.workspaceId) || !Array.isArray(rows) || rows.length > 100) fail();
  const decoded = rows.map(value => {
    if (typeof value !== "string") fail();
    try { return decodeRow(JSON.parse(value), expected); } catch { fail(); }
  });
  if (new Set(decoded.map(item => item.accountIdDigest)).size > 1) fail();
  const destroys = decoded.filter(item => item.action === "destroy_compute_allocation");
  const operationIds = new Set(destroys.map(item => item.operationIdDigest));
  if (operationIds.size > 1) fail();
  if (destroys.length > 0 && decoded.some(item => item.action !== "destroy_compute_allocation" && item.originalDestroyOperationIdDigest !== destroys[0].operationIdDigest)) fail();
  const sorted = decoded.sort((a, b) => a.createdAt.localeCompare(b.createdAt) || a.recordIdDigest.localeCompare(b.recordIdDigest))
    .map(({ accountIdDigest: _accountIdDigest, ...item }) => item);
  return validateComputeDestroyJournal({ status: destroys.length ? "observed" : "unknown", records: sorted,
    destroyOperationIdDigest: destroys[0]?.operationIdDigest ?? null, accountIdDigest: decoded[0]?.accountIdDigest ?? null,
    workspaceIdDigest: identity("console-customer-workspace-readback:workspace:v1", expected.workspaceId),
    computeIdDigest: identity("compute-readback:compute:v1", expected.computeId) });
}

export function validateComputeDestroyJournal(value: unknown): ComputeDestroyJournal {
  const journal = exact(value, ["status", "records", "destroyOperationIdDigest", "accountIdDigest", "workspaceIdDigest", "computeIdDigest"]);
  if (typeof journal.status !== "string" || !["observed", "unknown"].includes(journal.status) || !Array.isArray(journal.records) || journal.records.length > 100 ||
      !(journal.destroyOperationIdDigest === null || typeof journal.destroyOperationIdDigest === "string" && DIGEST.test(journal.destroyOperationIdDigest)) ||
      !(journal.accountIdDigest === null || typeof journal.accountIdDigest === "string" && DIGEST.test(journal.accountIdDigest)) ||
      typeof journal.workspaceIdDigest !== "string" || !DIGEST.test(journal.workspaceIdDigest) ||
      typeof journal.computeIdDigest !== "string" || !DIGEST.test(journal.computeIdDigest)) fail();
  const records = journal.records.map(item => {
    const row = exact(item, ["recordIdDigest", "operationIdDigest", "action", "originalDestroyOperationIdDigest", "phaseDigest", "requestEvidenceDigests", "status", "errorToken", "errorDigest", "retryable", "phase", "mutationCount", "providerRequestPresent", "providerRequestDigest", "requestHashDigest", "startedAt", "finishedAt", "createdAt"]);
    if (![row.recordIdDigest, row.operationIdDigest].every(item => typeof item === "string" && DIGEST.test(item)) ||
        typeof row.action !== "string" || !Object.hasOwn(ACTIONS, row.action) ||
        !(row.originalDestroyOperationIdDigest === null || typeof row.originalDestroyOperationIdDigest === "string" && DIGEST.test(row.originalDestroyOperationIdDigest)) ||
        (row.action === "destroy_compute_allocation" ? row.originalDestroyOperationIdDigest !== null : row.originalDestroyOperationIdDigest === null) ||
        !(row.phaseDigest === null || typeof row.phaseDigest === "string" && DIGEST.test(row.phaseDigest)) || !STATUSES.includes(row.status as never) ||
        !(row.errorToken === null || typeof row.errorToken === "string" && SAFE_ERRORS.has(row.errorToken)) ||
        !(row.errorDigest === null || typeof row.errorDigest === "string" && DIGEST.test(row.errorDigest)) ||
        typeof row.retryable !== "boolean" || !(row.phase === null || PHASES.includes(row.phase as never)) || (row.phase === null) !== (row.phaseDigest === null) ||
        !(row.mutationCount === null || Number.isSafeInteger(row.mutationCount) && Number(row.mutationCount) >= 0) ||
        typeof row.providerRequestPresent !== "boolean" || !(row.providerRequestDigest === null || typeof row.providerRequestDigest === "string" && DIGEST.test(row.providerRequestDigest)) ||
        row.providerRequestPresent !== (row.providerRequestDigest !== null) ||
        !(row.requestHashDigest === null || typeof row.requestHashDigest === "string" && DIGEST.test(row.requestHashDigest)) ||
        !instant(row.startedAt) || !(row.finishedAt === null || instant(row.finishedAt)) || !instant(row.createdAt)) fail();
    if (row.phase !== null && row.phaseDigest !== hash(row.phase as string) ||
        row.status === "started" && row.finishedAt !== null || row.status !== "started" && row.finishedAt === null ||
        row.finishedAt !== null && Date.parse(row.finishedAt as string) < Date.parse(row.startedAt as string) ||
        Date.parse(row.createdAt as string) < Date.parse(row.startedAt as string)) fail();
    const requestDigests = exact(row.requestEvidenceDigests, ["deleteMethod", "scaleDown", "deleteMode", "describeClusterMachinesReq", "describeNodePoolRequestId", "modifySelfProvisioningReqId", "verifyMachineDeletedReqId"]);
    if (Object.values(requestDigests).some(value => !(value === null || typeof value === "string" && DIGEST.test(value))) || row.errorToken !== null && row.errorDigest === null) fail();
    return row as unknown as ComputeDestroyJournalRecord;
  });
  const destroys = records.filter(item => item.action === "destroy_compute_allocation");
  const operations = new Set(destroys.map(item => item.operationIdDigest));
  if (new Set(records.map(item => item.recordIdDigest)).size !== records.length || operations.size > 1 || (destroys[0]?.operationIdDigest ?? null) !== journal.destroyOperationIdDigest ||
      (journal.status === "observed") !== (destroys.length > 0) || (records.length > 0) !== (journal.accountIdDigest !== null) ||
      records.some(item => item.action !== "destroy_compute_allocation" && item.originalDestroyOperationIdDigest !== journal.destroyOperationIdDigest)) fail();
  return journal as unknown as ComputeDestroyJournal;
}
