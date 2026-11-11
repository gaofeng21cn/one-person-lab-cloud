import { createHash } from "node:crypto";
import { candidateInstallationIdentity, canonicalJson } from "./cloud-candidate-receipt.ts";
import type { ComputeReadbackBinding } from "./workspace-compute-readback-binding.ts";
import type { ComputeReadbackProvenance } from "./workspace-compute-live-readback.ts";
import { validateComputeDestroyJournal, type ComputeDestroyJournal } from "./workspace-compute-destroy-journal.ts";

// Official schemas: cloud.tencent.com/document/api/629/35344 and
// cloud.tencent.com/document/product/629/56259. No mutation or credential access.
const ACTION = "detach_prepaid_compute_allocation";
const EVENT = "DeleteClusterMachines";
// CloudAudit API region contract: cloud.tencent.com/document/api/629/35335.
// This query region is independent of the frozen resource's region.
const API_REGION = "ap-guangzhou" as const;
const DIGEST = /^sha256:[a-f0-9]{64}$/;
const SHA = /^[a-f0-9]{40}$/;
const ID = /^[A-Za-z0-9][A-Za-z0-9_-]{0,255}$/;
const MAX_PAGES = 5;
export const COMPUTE_AUDIT_RESPONSE_ERROR = {
  ENVELOPE: "audit_response_envelope_invalid", FIELDS: "audit_response_fields_invalid",
  NEXT_TOKEN: "audit_response_next_token_invalid", EVENTS: "audit_response_events_invalid",
  COMPLETION: "audit_response_completion_invalid", COUNT: "audit_response_count_invalid",
  REQUEST_ID: "audit_response_request_id_invalid",
} as const;
const ERRORS = [...Object.values(COMPUTE_AUDIT_RESPONSE_ERROR), "sql_unavailable", "journal_binding_invalid", "audit_unavailable", "audit_schema_invalid",
  "incomplete_pagination", "ambiguous_events", "no_matching_event", "caller_identity_unbound"] as const;
type Reason = typeof ERRORS[number];
const CLASSES = ["success_code", "numeric_error", "AuthFailure", "UnauthorizedOperation", "OperationDenied", "InvalidParameter",
  "InvalidParameterValue", "MissingParameter", "UnsupportedOperation", "InternalError", "FailedOperation", "RequestLimitExceeded",
  "ResourceNotFound", "ResourceUnavailable", "LimitExceeded", "InvalidAction"] as const;
type ErrorClass = typeof CLASSES[number] | null;
const QUERY_ERROR_CLASSES = CLASSES.filter(value => value !== "success_code" && value !== "numeric_error");
type QueryFailure = {
  providerErrorClass: Exclude<ErrorClass, "success_code" | "numeric_error">;
  providerErrorCodeDigest: string | null; providerMessageDigest: string | null; responseRequestIdDigest: string | null;
};
type Candidate = ReturnType<typeof candidateInstallationIdentity>;
type Stage = { operationIdDigest: string; startedRecordIdDigest: string; failedRecordIdDigest: string; requestHashDigest: string; startedAt: string; failedAt: string };
type Target = { clusterIdDigest: string; machineNameDigest: string; regionDigest: string };
type Window = { startTime: number; endTime: number; startUtc: string; endUtc: string };
type AuditEvent = {
  eventIdDigest: string; requestIdDigest: string; eventDigest: string; parametersDigest: string;
  providerAccountIdDigest: string; principalIdDigest: string; identityBinding: "event_internal_only";
  camErrorCode: number; camMessageDigest: string | null; apiErrorClass: ErrorClass;
  apiErrorCodeDigest: string | null; apiMessageDigest: string | null;
};
export type ComputeProviderRequestAuditReceipt = {
  schemaVersion: 1; kind: "opl_cloud_compute_provider_request_audit"; scope: typeof ACTION;
  status: "unknown" | "blocked"; errorCode: Reason; candidate: Candidate; provenance: ComputeReadbackProvenance;
  owner: { workspaceIdDigest: string; workspaceOperationIdDigest: string; tenantIdDigest: string; resourceSetIdDigest: string;
    fabricOperationIdDigest: string; computeIdDigest: string; accountIdDigest: string; destroyOperationIdDigest: string };
  stage: Stage | null; target: Target | null; window: Window | null;
  query: { apiRegion: typeof API_REGION; sqlDigest: string; requestDigests: string[]; responseRequestDigests: string[]; pages: number; complete: boolean; failure: QueryFailure | null };
  event: AuditEvent | null;
};
export type ComputeProviderRequestAuditInput = {
  candidate: unknown; provenance: ComputeReadbackProvenance; binding: ComputeReadbackBinding; journal: ComputeDestroyJournal;
  select(sql: string): Promise<string[]>; lookUpEvents(body: Record<string, unknown>, region: string): Promise<unknown>;
};
class AuditFailure extends Error {}
function fail(reason: Reason = "audit_schema_invalid"): never { throw new AuditFailure(reason); }
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail();
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: readonly string[]) {
  const row = object(value);
  if (Object.keys(row).length !== keys.length || keys.some(key => !Object.hasOwn(row, key))) fail();
  return row;
}
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0 && value.length <= 4096; }
function digest(value: unknown): value is string { return typeof value === "string" && DIGEST.test(value); }
function hash(value: string) { return `sha256:${createHash("sha256").update(value).digest("hex")}`; }
function identity(domain: string, value: string) { return hash(`${domain}\0${value}`); }
function instant(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const m = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)$/.exec(value);
  if (!m || !Number.isFinite(Date.parse(value))) return false;
  const local = `${m[1]}.${(m[2] ?? "").padEnd(3, "0").slice(0, 3)}Z`;
  return new Date(local).toISOString() === local && (m[3] === "Z" || Number(m[3].slice(1, 3)) <= 23 && Number(m[3].slice(4, 6)) <= 59);
}
function timestamp(value: unknown): bigint {
  if (!instant(value)) fail("journal_binding_invalid");
  const fraction = /\.(\d{1,9})/.exec(value)?.[1] ?? "";
  return BigInt(Math.floor(Date.parse(value) / 1000)) * 1_000_000_000n + BigInt(fraction.padEnd(9, "0"));
}
function provenance(value: unknown): ComputeReadbackProvenance {
  const p = exact(value, ["diagnosticSourceSha", "instanceRepository", "instanceSha", "runId", "runAttempt", "workflowPath"]);
  if (typeof p.diagnosticSourceSha !== "string" || !SHA.test(p.diagnosticSourceSha) || typeof p.instanceSha !== "string" || !SHA.test(p.instanceSha) ||
      p.instanceRepository !== "gaofeng21cn/opl-instance-medopl" || typeof p.runId !== "string" || !/^[1-9][0-9]*$/.test(p.runId) ||
      typeof p.runAttempt !== "string" || !/^[1-9][0-9]*$/.test(p.runAttempt) || p.workflowPath !== ".github/workflows/cloud-workspace-compute-readback.yml") fail();
  return p as ComputeReadbackProvenance;
}
function binding(value: unknown): ComputeReadbackBinding {
  const b = exact(value, ["workspaceId", "workspaceOperationId", "tenantId", "resourceSetId", "fabricOperationId", "computeId"]);
  if (Object.values(b).some(v => typeof v !== "string" || !ID.test(v))) fail("journal_binding_invalid");
  return b as ComputeReadbackBinding;
}
// Rejected admission records never entered the lifecycle (same rule as v2
// journal). Keep every non-rejected row of both operation IDs, including foreign
// actions and owners. Only the original destroy supplies resources.
export function computeProviderRequestAuditSql(computeId: string): string {
  if (!ID.test(computeId)) fail("journal_binding_invalid");
  return `SELECT json_build_object(
    'id',id,'operationId',operation_id,'resourceId',resource_id,'workspaceId',workspace_id,
    'accountId',account_id,'action',action,'resourceKind',resource_kind,'status',status,
    'startedAt',started_at,'finishedAt',finished_at,'createdAt',created_at,
    'requestHash',request_hash,'providerRequestId',provider_request_id,'errorCode',error_code,
    'originalDestroyOperationId',redacted_provider_payload::jsonb->'originalDestroyOperationId',
    'resource',redacted_provider_payload::jsonb->'resource'
  )::text FROM fabric_operations WHERE status <> 'rejected' AND operation_id IN (
    SELECT operation_id FROM fabric_operations WHERE
      (resource_id='${computeId}' OR redacted_provider_payload::jsonb->'resource'->'id'=to_jsonb('${computeId}'::text))
      AND action IN ('destroy_compute_allocation','${ACTION}')
      AND status IN ('started','failed','succeeded')
  ) ORDER BY created_at,id LIMIT 101`;
}
const ROW_KEYS = ["id", "operationId", "resourceId", "workspaceId", "accountId", "action", "resourceKind", "status", "startedAt", "finishedAt", "createdAt", "requestHash", "providerRequestId", "errorCode", "originalDestroyOperationId", "resource"];
function frozenInput(rows: unknown, b: ComputeReadbackBinding, journal: ComputeDestroyJournal) {
  if (!Array.isArray(rows) || rows.length === 0 || rows.length > 100 || rows.some(v => typeof v !== "string" || v.length > 1024 * 1024)) fail("journal_binding_invalid");
  const decoded = rows.map(v => exact(JSON.parse(v), ROW_KEYS));
  const selected = journal.records.filter(r => r.action === ACTION || r.action === "destroy_compute_allocation");
  if (selected.length !== decoded.length || new Set(decoded.map(r => r.id)).size !== decoded.length) fail("journal_binding_invalid");
  for (const row of decoded) {
    const record = selected.find(r => r.recordIdDigest === identity("compute-destroy-journal:record:v1", String(row.id)));
    if (!record || !text(row.id) || !text(row.operationId) || !text(row.accountId) || row.resourceId !== b.computeId || row.workspaceId !== b.workspaceId ||
        identity("compute-destroy-journal:account:v1", row.accountId) !== journal.accountIdDigest || record.action !== row.action || record.status !== row.status ||
        record.operationIdDigest !== identity("cloud-launch-readback:operation:v1", row.operationId) ||
        record.requestHashDigest !== (text(row.requestHash) ? hash(row.requestHash) : null) ||
        record.providerRequestDigest !== (text(row.providerRequestId) ? identity("compute-readback:request:v1", row.providerRequestId) : null) ||
        record.errorDigest !== (text(row.errorCode) ? hash(row.errorCode) : null) ||
        timestamp(record.startedAt) !== timestamp(row.startedAt) || timestamp(record.createdAt) !== timestamp(row.createdAt) ||
        (record.finishedAt === null ? row.finishedAt !== null : row.finishedAt === null || timestamp(record.finishedAt) !== timestamp(row.finishedAt))) fail("journal_binding_invalid");
  }
  const destroys = decoded.filter(r => r.action === "destroy_compute_allocation");
  const stages = decoded.filter(r => r.action === ACTION);
  const start = stages.find(r => r.status === "started"), failed = stages.find(r => r.status === "failed");
  if (!destroys.length || stages.length !== 2 || !start || !failed || start.resourceKind !== "compute_prepaid_retirement" || failed.resourceKind !== start.resourceKind ||
      start.operationId !== failed.operationId || start.requestHash !== failed.requestHash || !text(start.requestHash) ||
      start.finishedAt !== null || !instant(failed.finishedAt) || timestamp(start.startedAt) !== timestamp(failed.startedAt) ||
      timestamp(failed.finishedAt) < timestamp(start.startedAt) || timestamp(failed.finishedAt) - timestamp(start.startedAt) > 300_000_000_000n ||
      failed.errorCode !== "compute_prepaid_detach_dispatch_unconfirmed" || start.resource !== null || failed.resource !== null) fail("journal_binding_invalid");
  let frozen: { clusterId: string; machineName: string; region: string } | undefined;
  for (const row of destroys) {
    if (row.resourceKind !== "compute_allocation" || row.originalDestroyOperationId !== null ||
        identity("cloud-launch-readback:operation:v1", String(row.operationId)) !== journal.destroyOperationIdDigest ||
        start.originalDestroyOperationId !== row.operationId || failed.originalDestroyOperationId !== row.operationId) fail("journal_binding_invalid");
    const resource = object(row.resource), data = object(resource.providerData);
    if (resource.id !== b.computeId || resource.workspaceId !== b.workspaceId || resource.accountId !== row.accountId || resource.provider !== "tencent-tke" ||
        data.machineType !== "NativeCVM" || !text(data.clusterId) || !text(data.region) || !text(resource.machineName) ||
        data.machineName !== undefined && data.machineName !== resource.machineName || !/^[a-z][a-z0-9-]{0,63}$/.test(data.region)) fail("journal_binding_invalid");
    const tuple = { clusterId: data.clusterId, machineName: resource.machineName, region: data.region };
    if (frozen && canonicalJson(frozen) !== canonicalJson(tuple)) fail("journal_binding_invalid");
    frozen = tuple;
  }
  const startTime = Number(timestamp(start.startedAt) / 1_000_000_000n) - 60;
  const endTime = Number((timestamp(failed.finishedAt) + 999_999_999n) / 1_000_000_000n) + 60;
  const stage: Stage = { operationIdDigest: identity("cloud-launch-readback:operation:v1", start.operationId as string),
    startedRecordIdDigest: identity("compute-destroy-journal:record:v1", start.id as string), failedRecordIdDigest: identity("compute-destroy-journal:record:v1", failed.id as string),
    requestHashDigest: hash(start.requestHash), startedAt: start.startedAt as string, failedAt: failed.finishedAt };
  return { frozen: frozen!, stage, window: { startTime, endTime, startUtc: new Date(startTime * 1000).toISOString(), endUtc: new Date(endTime * 1000).toISOString() } };
}
function errorClass(code: unknown): ErrorClass {
  if (code === "" || code === null) return null;
  if (code === 0 || code === "0") return "success_code";
  if (typeof code === "number") return "numeric_error";
  const root = (code as string).split(".")[0];
  return CLASSES.includes(root as never) ? root as ErrorClass : null;
}
function optionalMessage(value: unknown): string | null {
  if (value === undefined || value === null || value === "") return null;
  if (!text(value)) fail();
  return hash(value);
}
function queryFailure(result: Record<string, unknown>, envelopeValid: boolean): { failure: QueryFailure; valid: boolean } {
  const raw = result.Error;
  const error = raw !== null && typeof raw === "object" && !Array.isArray(raw) ? raw as Record<string, unknown> : null;
  const code = error?.Code, message = error?.Message;
  const classified = text(code) ? errorClass(code) : null;
  const requestIdValid = text(result.RequestId) && !/[\s\p{Cc}]/u.test(result.RequestId);
  const codeValid = text(code) && !/[\s\p{Cc}]/u.test(code);
  return {
    failure: {
      providerErrorClass: classified !== null && QUERY_ERROR_CLASSES.includes(classified as never) ? classified as QueryFailure["providerErrorClass"] : null,
      providerErrorCodeDigest: text(code) ? hash(code) : null,
      providerMessageDigest: typeof message === "string" && message.length <= 4096 ? hash(message) : null,
      responseRequestIdDigest: requestIdValid ? identity("compute-readback:request:v1", result.RequestId as string) : null,
    },
    valid: envelopeValid && Object.keys(result).length === 2 && Object.hasOwn(result, "RequestId") && requestIdValid &&
      error !== null && Object.keys(error).length === 2 && Object.hasOwn(error, "Code") && Object.hasOwn(error, "Message") &&
      codeValid && typeof message === "string" && message.length <= 4096,
  };
}
// The event field table uses strings; the official LookUpEvents example uses
// numeric UINs. Admit exact safe integers only, with the same decimal identity.
function providerIdentity(value: unknown): string {
  if (text(value)) return value;
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return String(value);
  fail();
}
function jsonObject(value: unknown) { return object(typeof value === "string" ? JSON.parse(value) : value); }
function decodeEvent(raw: unknown, target: { clusterId: string; machineName: string; region: string }): AuditEvent | null {
  const e = object(raw);
  // CloudAuditEvent is an official JSON-encoded string, not an alternate DTO.
  if (!text(e.EventId) || !text(e.RequestID) || (typeof e.CloudAuditEvent !== "string" || e.CloudAuditEvent.length === 0 || e.CloudAuditEvent.length > 256 * 1024) || e.EventName !== EVENT ||
      !Number.isSafeInteger(e.ErrorCode) || typeof e.AccountID !== "number" || !Number.isSafeInteger(e.AccountID) || e.AccountID <= 0 ||
      !text(e.EventSource) || !text(e.ResourceRegion)) fail();
  const inner = jsonObject(e.CloudAuditEvent);
  if (inner.eventName !== EVENT || inner.resourceType !== "tke" || inner.eventSource !== e.EventSource || inner.requestID !== e.RequestID ||
      inner.eventVersion !== 2 || inner.errorCode !== e.ErrorCode || !text(inner.eventRegion) ||
      !(inner.apiErrorCode === null || typeof inner.apiErrorCode === "string" && inner.apiErrorCode.length <= 4096 || Number.isSafeInteger(inner.apiErrorCode)) ||
      !Object.hasOwn(inner, "apiErrorMessage") || !Object.hasOwn(inner, "requestParameters")) fail();
  const caller = object(inner.userIdentity);
  // These are Tencent identities only. They are NEVER compared with Fabric accountId.
  const accountId = providerIdentity(caller.accountId), principalId = providerIdentity(caller.principalId);
  if (accountId !== String(e.AccountID)) fail();
  if (caller.secretId !== undefined && typeof caller.secretId !== "string") fail();
  if (caller.sessionContext !== undefined) {
    const session = object(caller.sessionContext);
    if (session.ownerUin !== undefined && providerIdentity(session.ownerUin) !== accountId ||
        session.uin !== undefined && providerIdentity(session.uin) !== principalId) fail();
  }
  const params = jsonObject(inner.requestParameters);
  if (!["ClusterId", "MachineNames", "InstanceDeleteMode", "EnableScaleDown"].every(k => Object.hasOwn(params, k)) ||
      !text(params.ClusterId) || !Array.isArray(params.MachineNames) || params.MachineNames.some(v => !text(v)) ||
      typeof params.EnableScaleDown !== "boolean" || typeof params.InstanceDeleteMode !== "string") fail();
  if (e.ResourceRegion !== target.region || params.ClusterId !== target.clusterId || params.MachineNames.length !== 1 ||
      params.MachineNames[0] !== target.machineName || params.InstanceDeleteMode !== "retain" || params.EnableScaleDown !== true) return null;
  return { eventIdDigest: identity("compute-provider-audit:event:v1", e.EventId), requestIdDigest: identity("compute-readback:request:v1", e.RequestID),
    eventDigest: hash(canonicalJson(e)), parametersDigest: hash(canonicalJson(params)), providerAccountIdDigest: identity("compute-provider-audit:account:v1", accountId),
    principalIdDigest: identity("compute-provider-audit:principal:v1", principalId), identityBinding: "event_internal_only", camErrorCode: e.ErrorCode as number,
    camMessageDigest: optionalMessage(inner.errorMessage), apiErrorClass: errorClass(inner.apiErrorCode),
    apiErrorCodeDigest: inner.apiErrorCode === "" || inner.apiErrorCode === null ? null : hash(String(inner.apiErrorCode)), apiMessageDigest: optionalMessage(inner.apiErrorMessage) };
}
export async function runComputeProviderRequestAudit(input: ComputeProviderRequestAuditInput): Promise<ComputeProviderRequestAuditReceipt> {
  const b = binding(input.binding), j = validateComputeDestroyJournal(input.journal), p = provenance(input.provenance);
  if (j.status !== "observed" || !j.accountIdDigest || !j.destroyOperationIdDigest || j.workspaceIdDigest !== identity("console-customer-workspace-readback:workspace:v1", b.workspaceId) ||
      j.computeIdDigest !== identity("compute-readback:compute:v1", b.computeId)) fail("journal_binding_invalid");
  const sql = computeProviderRequestAuditSql(b.computeId);
  const receipt: ComputeProviderRequestAuditReceipt = { schemaVersion: 1, kind: "opl_cloud_compute_provider_request_audit", scope: ACTION, status: "blocked", errorCode: "journal_binding_invalid",
    candidate: candidateInstallationIdentity(input.candidate), provenance: p,
    owner: { workspaceIdDigest: j.workspaceIdDigest, workspaceOperationIdDigest: identity("cloud-launch-readback:operation:v1", b.workspaceOperationId),
      tenantIdDigest: identity("compute-provider-audit:tenant:v1", b.tenantId), resourceSetIdDigest: identity("compute-provider-audit:set:v1", b.resourceSetId),
      fabricOperationIdDigest: identity("cloud-launch-readback:operation:v1", b.fabricOperationId), computeIdDigest: j.computeIdDigest,
      accountIdDigest: j.accountIdDigest, destroyOperationIdDigest: j.destroyOperationIdDigest }, stage: null, target: null, window: null,
    query: { apiRegion: API_REGION, sqlDigest: hash(sql), requestDigests: [], responseRequestDigests: [], pages: 0, complete: false, failure: null }, event: null };
  let phase: Reason = "sql_unavailable";
  try {
    const rows = await input.select(sql);
    phase = "journal_binding_invalid";
    let decoded: ReturnType<typeof frozenInput>;
    try {
      decoded = frozenInput(rows, b, j);
    } catch {
      // SQL projection/JSON/resource decoding belongs to the journal boundary,
      // including shared exact()/object() helpers' default schema failures.
      fail("journal_binding_invalid");
    }
    const { frozen, stage, window } = decoded;
    receipt.stage = stage; receipt.window = window;
    receipt.target = { clusterIdDigest: identity("compute-provider-audit:cluster:v1", frozen.clusterId), machineNameDigest: identity("compute-provider-audit:machine:v1", frozen.machineName), regionDigest: identity("compute-provider-audit:region:v1", frozen.region) };
    const candidates: AuditEvent[] = [], tokens = new Set<string>(), eventIds = new Set<string>(), requests = new Set<string>();
    let token: string | undefined;
    for (let page = 0; page < MAX_PAGES; page++) {
      const body = { StartTime: window.startTime, EndTime: window.endTime, MaxResults: 20, Mode: "standard",
        LookupAttributes: [{ AttributeKey: "EventName", AttributeValue: EVENT }], ...(token === undefined ? {} : { NextToken: token }) };
      receipt.query.requestDigests.push(hash(canonicalJson({ body, region: API_REGION })));
      phase = "audit_unavailable";
      const response = await input.lookUpEvents(body, API_REGION);
      phase = COMPUTE_AUDIT_RESPONSE_ERROR.ENVELOPE;
      let envelope: Record<string, unknown>, result: Record<string, unknown>;
      try { envelope = object(response); result = object(envelope.Response); }
      catch { fail(COMPUTE_AUDIT_RESPONSE_ERROR.ENVELOPE); }
      if (Object.hasOwn(result, "Error")) {
        const decoded = queryFailure(result, Object.keys(envelope).length === 1);
        receipt.query.failure = decoded.failure;
        if (!decoded.valid) fail("audit_schema_invalid");
        fail("audit_unavailable");
      }
      try { exact(envelope, ["Response"]); }
      catch { fail(COMPUTE_AUDIT_RESPONSE_ERROR.ENVELOPE); }
      try { exact(result, ["NextToken", "Events", "ListOver", "TotalCount", "RequestId"]); }
      catch { fail(COMPUTE_AUDIT_RESPONSE_ERROR.FIELDS); }
      if (!(result.NextToken === null || typeof result.NextToken === "string" && result.NextToken.length <= 4096)) fail(COMPUTE_AUDIT_RESPONSE_ERROR.NEXT_TOKEN);
      if (!Array.isArray(result.Events) || result.Events.length > 20) fail(COMPUTE_AUDIT_RESPONSE_ERROR.EVENTS);
      if (typeof result.ListOver !== "boolean") fail(COMPUTE_AUDIT_RESPONSE_ERROR.COMPLETION);
      // The API documents TotalCount as a count, but does not bind it to
      // this page. Pagination authority is ListOver and NextToken only.
      if (!Number.isSafeInteger(result.TotalCount) || Number(result.TotalCount) < 0) fail(COMPUTE_AUDIT_RESPONSE_ERROR.COUNT);
      if (!text(result.RequestId)) fail(COMPUTE_AUDIT_RESPONSE_ERROR.REQUEST_ID);
      phase = "audit_schema_invalid";
      receipt.query.responseRequestDigests.push(identity("compute-readback:request:v1", result.RequestId)); receipt.query.pages++;
      for (const raw of result.Events) {
        const e = object(raw);
        if (!text(e.EventId) || !text(e.RequestID) || eventIds.has(e.EventId) || requests.has(e.RequestID)) fail();
        eventIds.add(e.EventId); requests.add(e.RequestID);
        const event = decodeEvent(raw, frozen);
        if (event) candidates.push(event);
      }
      if (result.ListOver) {
        receipt.query.complete = true; break;
      }
      if (!text(result.NextToken) || tokens.has(result.NextToken)) fail("incomplete_pagination");
      tokens.add(result.NextToken); token = result.NextToken;
    }
    receipt.status = "unknown";
    if (!receipt.query.complete) receipt.errorCode = "incomplete_pagination";
    else if (candidates.length > 1) receipt.errorCode = "ambiguous_events";
    else if (candidates.length === 0) receipt.errorCode = "no_matching_event";
    else { receipt.errorCode = "caller_identity_unbound"; receipt.event = candidates[0]; }
  } catch (error) {
    receipt.errorCode = error instanceof AuditFailure && ERRORS.includes(error.message as Reason) ? error.message as Reason : phase;
    receipt.status = receipt.errorCode === "incomplete_pagination" ? "unknown" : "blocked";
    receipt.event = null;
  }
  return validateComputeProviderRequestAuditReceipt(receipt);
}
export function validateComputeProviderRequestAuditReceipt(value: unknown): ComputeProviderRequestAuditReceipt {
  const r = exact(value, ["schemaVersion", "kind", "scope", "status", "errorCode", "candidate", "provenance", "owner", "stage", "target", "window", "query", "event"]);
  if (r.schemaVersion !== 1 || r.kind !== "opl_cloud_compute_provider_request_audit" || r.scope !== ACTION ||
      !["unknown", "blocked"].includes(String(r.status)) || !ERRORS.includes(r.errorCode as Reason)) fail();
  const c = exact(r.candidate, ["candidateReceiptDigest", "productSha", "productTree", "cloudImageRef", "cloudImageDigest", "cloudImageRevision", "cloudRuntimeChildDigest"]);
  if (![c.candidateReceiptDigest, c.cloudImageDigest, c.cloudRuntimeChildDigest].every(digest) || typeof c.productSha !== "string" || !SHA.test(c.productSha) ||
      typeof c.productTree !== "string" || !SHA.test(c.productTree) || c.cloudImageRevision !== c.productSha || c.cloudRuntimeChildDigest === c.cloudImageDigest ||
      c.cloudImageRef !== `ghcr.io/gaofeng21cn/one-person-lab-cloud@${c.cloudImageDigest}`) fail();
  provenance(r.provenance);
  const o = exact(r.owner, ["workspaceIdDigest", "workspaceOperationIdDigest", "tenantIdDigest", "resourceSetIdDigest", "fabricOperationIdDigest", "computeIdDigest", "accountIdDigest", "destroyOperationIdDigest"]);
  if (!Object.values(o).every(digest)) fail();
  if ((r.stage === null) !== (r.target === null) || (r.stage === null) !== (r.window === null)) fail();
  if (r.stage !== null) {
    const s = exact(r.stage, ["operationIdDigest", "startedRecordIdDigest", "failedRecordIdDigest", "requestHashDigest", "startedAt", "failedAt"]);
    if (![s.operationIdDigest, s.startedRecordIdDigest, s.failedRecordIdDigest, s.requestHashDigest].every(digest) || s.startedRecordIdDigest === s.failedRecordIdDigest ||
        !instant(s.startedAt) || !instant(s.failedAt) || timestamp(s.failedAt) < timestamp(s.startedAt) || timestamp(s.failedAt) - timestamp(s.startedAt) > 300_000_000_000n) fail();
    const t = exact(r.target, ["clusterIdDigest", "machineNameDigest", "regionDigest"]);
    if (!Object.values(t).every(digest)) fail();
    const w = exact(r.window, ["startTime", "endTime", "startUtc", "endUtc"]);
    if (w.startTime !== Number(timestamp(s.startedAt) / 1_000_000_000n) - 60 || w.endTime !== Number((timestamp(s.failedAt) + 999_999_999n) / 1_000_000_000n) + 60 ||
        w.startUtc !== new Date(Number(w.startTime) * 1000).toISOString() || w.endUtc !== new Date(Number(w.endTime) * 1000).toISOString()) fail();
  }
  const q = exact(r.query, ["apiRegion", "sqlDigest", "requestDigests", "responseRequestDigests", "pages", "complete", "failure"]);
  if (q.apiRegion !== API_REGION || !digest(q.sqlDigest) || !Array.isArray(q.requestDigests) || !q.requestDigests.every(digest) || !Array.isArray(q.responseRequestDigests) || !q.responseRequestDigests.every(digest) ||
      !Number.isInteger(q.pages) || Number(q.pages) < 0 || Number(q.pages) > MAX_PAGES || typeof q.complete !== "boolean" ||
      q.responseRequestDigests.length !== q.pages || q.requestDigests.length < Number(q.pages) || q.requestDigests.length > Math.min(MAX_PAGES, Number(q.pages) + 1) ||
      r.stage === null && (q.requestDigests.length !== 0 || q.pages !== 0 || q.complete) || q.complete && q.pages === 0) fail();
  if (Object.values(COMPUTE_AUDIT_RESPONSE_ERROR).includes(r.errorCode as never) &&
      (r.stage === null || r.status !== "blocked" || q.complete || q.failure !== null ||
        q.requestDigests.length !== Number(q.pages) + 1 || r.event !== null)) fail();
  if (q.failure !== null) {
    const f = exact(q.failure, ["providerErrorClass", "providerErrorCodeDigest", "providerMessageDigest", "responseRequestIdDigest"]);
    if (!(f.providerErrorClass === null || QUERY_ERROR_CLASSES.includes(f.providerErrorClass as never)) ||
        [f.providerErrorCodeDigest, f.providerMessageDigest, f.responseRequestIdDigest].some(v => v !== null && !digest(v)) ||
        f.providerErrorClass !== null && f.providerErrorCodeDigest === null ||
        r.status !== "blocked" || !["audit_unavailable", "audit_schema_invalid"].includes(String(r.errorCode)) ||
        r.stage === null || q.complete || r.event !== null || q.requestDigests.length !== Number(q.pages) + 1 ||
        r.errorCode === "audit_unavailable" && [f.providerErrorCodeDigest, f.providerMessageDigest, f.responseRequestIdDigest].some(v => v === null)) fail();
  }
  const unknown = ["incomplete_pagination", "ambiguous_events", "no_matching_event", "caller_identity_unbound"].includes(String(r.errorCode));
  if ((r.status === "unknown") !== unknown || unknown && r.stage === null ||
      ["ambiguous_events", "no_matching_event", "caller_identity_unbound"].includes(String(r.errorCode)) && !q.complete ||
      r.errorCode === "incomplete_pagination" && q.complete || (r.event !== null) !== (r.errorCode === "caller_identity_unbound") ||
      ["sql_unavailable", "journal_binding_invalid"].includes(String(r.errorCode)) !== (r.stage === null)) fail();
  if (r.event !== null) {
    const e = exact(r.event, ["eventIdDigest", "requestIdDigest", "eventDigest", "parametersDigest", "providerAccountIdDigest", "principalIdDigest", "identityBinding", "camErrorCode", "camMessageDigest", "apiErrorClass", "apiErrorCodeDigest", "apiMessageDigest"]);
    if (![e.eventIdDigest, e.requestIdDigest, e.eventDigest, e.parametersDigest, e.providerAccountIdDigest, e.principalIdDigest].every(digest) ||
        e.identityBinding !== "event_internal_only" || !Number.isSafeInteger(e.camErrorCode) ||
        !(e.apiErrorClass === null || CLASSES.includes(e.apiErrorClass as never)) ||
        [e.camMessageDigest, e.apiErrorCodeDigest, e.apiMessageDigest].some(v => v !== null && !digest(v)) || e.apiErrorClass !== null && e.apiErrorCodeDigest === null) fail();
  }
  return r as ComputeProviderRequestAuditReceipt;
}
