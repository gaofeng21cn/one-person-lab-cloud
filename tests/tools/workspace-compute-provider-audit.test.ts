import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import { candidateReceipt } from "../helpers/protected-qualification-fixture.ts";
import { canonicalJson } from "../../tools/cloud-candidate-receipt.ts";
import { decodeComputeDestroyJournal } from "../../tools/workspace-compute-destroy-journal.ts";
import { computeProviderRequestAuditSql, runComputeProviderRequestAudit, validateComputeProviderRequestAuditReceipt,
  COMPUTE_AUDIT_RESPONSE_ERROR as RESPONSE_ERROR, type ComputeProviderRequestAuditInput } from "../../tools/workspace-compute-provider-audit.ts";

const action = "detach_prepaid_compute_allocation";
const binding = { workspaceId: "workspace_original", workspaceOperationId: "op_workspace_original", tenantId: "tenant_original",
  resourceSetId: "set_original", fabricOperationId: "fabric_original", computeId: "compute_original" };
const account = "business_account_original"; // Deliberately unrelated to Tencent UIN.
const start = "2026-11-11T01:32:31.000000123Z", finish = "2026-11-11T01:32:31.883456789Z";
const frozen = { id: binding.computeId, workspaceId: binding.workspaceId, accountId: account, provider: "tencent-tke",
  machineName: "machine-private-original", providerData: { clusterId: "cls-private-original", machineName: "machine-private-original", region: "ap-guangzhou", machineType: "NativeCVM" } };
function sqlRows() {
  const base = { resourceId: binding.computeId, workspaceId: binding.workspaceId, accountId: account, providerRequestId: "", requestHash: "private-stage-hash", errorCode: "" };
  return [
    { ...base, id: "fop_destroy", operationId: "op_destroy_original", action: "destroy_compute_allocation", resourceKind: "compute_allocation", status: "started",
      startedAt: "2026-11-11T01:30:00Z", finishedAt: null, createdAt: "2026-11-11T01:30:00Z", originalDestroyOperationId: null, resource: structuredClone(frozen) },
    { ...base, id: "fop_detach_fence", operationId: "op_detach_original", action, resourceKind: "compute_prepaid_retirement", status: "started",
      startedAt: start, finishedAt: null, createdAt: start, originalDestroyOperationId: "op_destroy_original", resource: null },
    { ...base, id: "fop_detach_failed", operationId: "op_detach_original", action, resourceKind: "compute_prepaid_retirement", status: "failed",
      startedAt: start, finishedAt: finish, createdAt: finish, originalDestroyOperationId: "op_destroy_original", resource: null, errorCode: "compute_prepaid_detach_dispatch_unconfirmed" },
  ];
}
function journal(rows = sqlRows()) {
  return decodeComputeDestroyJournal(rows.map(row => JSON.stringify({ ...row, retryable: false, phase: null, mutationCount: null,
    resource: row.resource === null ? null : { id: binding.computeId, workspaceId: binding.workspaceId, accountId: account, provider: "tencent-tke" },
    deleteRequests: { deleteMethod: null, scaleDown: null, deleteMode: null, describeClusterMachinesReq: null, describeNodePoolRequestId: null, modifySelfProvisioningReqId: null, verifyMachineDeletedReqId: null } })), binding);
}
function event() {
  return { EventId: "private-event-original", RequestID: "private-tke-request-original", EventName: "DeleteClusterMachines", ErrorCode: 0,
    AccountID: 100000000001, EventSource: "tke.ap-guangzhou.api.tencentyun.com", ResourceRegion: "ap-guangzhou", SecretId: "private-secret-id",
    CloudAuditEvent: JSON.stringify({ eventVersion: 2, eventName: "DeleteClusterMachines", resourceType: "tke",
      eventSource: "tke.ap-guangzhou.api.tencentyun.com", eventRegion: "ap-guangzhou", requestID: "private-tke-request-original",
      errorCode: 0, errorMessage: "permission verify", apiErrorCode: "UnsupportedOperation.PrivateSubcode", apiErrorMessage: "private failure 10.7.8.9 actor-secret",
      userIdentity: { accountId: "100000000001", principalId: "100000000002", secretId: "********", userName: "private-actor-name",
        sessionContext: { ownerUin: "100000000001", uin: "100000000002" } },
      requestParameters: { ClusterId: frozen.providerData.clusterId, MachineNames: [frozen.machineName], InstanceDeleteMode: "retain", EnableScaleDown: true },
      requestElements: { Response: { Error: { Code: "UnsupportedOperation.PrivateSubcode", Message: "never-export" } } } }) };
}
function page(events: unknown[] = [event()], next: string | null = "", over = true) {
  return { Response: { NextToken: next, Events: events, ListOver: over, TotalCount: events.length, RequestId: "private-audit-query-request" } };
}
function fixture() {
  const calls: Array<{ body: Record<string, unknown>; region: string }> = [];
  const rows = sqlRows();
  const input: ComputeProviderRequestAuditInput = { candidate: candidateReceipt(), binding: { ...binding }, journal: journal(),
    provenance: { diagnosticSourceSha: "d".repeat(40), instanceRepository: "gaofeng21cn/opl-instance-medopl", instanceSha: "b".repeat(40),
      runId: "42866553995", runAttempt: "1", workflowPath: ".github/workflows/cloud-workspace-compute-readback.yml" },
    async select(sql) {
      assert.equal(sql, computeProviderRequestAuditSql(binding.computeId));
      assert.match(sql, /^SELECT json_build_object/);
      assert.match(sql, /FROM fabric_operations WHERE status <> 'rejected' AND operation_id IN/);
      assert.match(sql, /AND status IN \('started','failed','succeeded'\)/);
      assert.match(sql, /'resource',redacted_provider_payload::jsonb->'resource'/);
      assert.match(sql, /ORDER BY created_at,id LIMIT 101$/);
      assert.doesNotMatch(sql, /\b(?:UPDATE|INSERT|DELETE|DROP|ALTER|TRUNCATE)\b|create_compute_allocation|ensure_compute_allocation/);
      return rows.map(r => JSON.stringify(r));
    },
    async lookUpEvents(body, region) { calls.push({ body, region }); return page(); },
  };
  return { input, rows, calls };
}
function editEvent(edit: (inner: Record<string, any>, outer: ReturnType<typeof event>) => void) {
  const outer = event(), inner = JSON.parse(outer.CloudAuditEvent); edit(inner, outer); outer.CloudAuditEvent = JSON.stringify(inner); return outer;
}

test("SQL frozen detach and original destroy are bound to journal; unique target reports safe cause but never fence/caller attribution", async () => {
  const f = fixture(), result = await runComputeProviderRequestAudit(f.input);
  assert.equal(result.status, "unknown"); assert.equal(result.errorCode, "caller_identity_unbound");
  assert.equal(result.event?.apiErrorClass, "UnsupportedOperation"); assert.equal(result.event?.identityBinding, "event_internal_only");
  assert.equal(result.query.complete, true); assert.equal(result.query.pages, 1);
  assert.deepEqual(f.calls, [{ region: "ap-guangzhou", body: { StartTime: 1794360691, EndTime: 1794360812, MaxResults: 20, Mode: "standard",
    LookupAttributes: [{ AttributeKey: "EventName", AttributeValue: "DeleteClusterMachines" }] } }]);
  assert.deepEqual(result.window, { startTime: 1794360691, endTime: 1794360812, startUtc: "2026-11-11T01:31:31.000Z", endUtc: "2026-11-11T01:33:32.000Z" });
  assert.equal(result.event?.apiMessageDigest, `sha256:${createHash("sha256").update("private failure 10.7.8.9 actor-secret").digest("hex")}`);
  const encoded = JSON.stringify(result);
  for (const raw of [...Object.values(binding), account, frozen.machineName, frozen.providerData.clusterId, "private-actor-name", "private-secret-id", "100000000001", "10.7.8.9", "never-export", "PrivateSubcode"]) assert.equal(encoded.includes(raw), false, raw);
  assert.deepEqual(validateComputeProviderRequestAuditReceipt(JSON.parse(encoded)), result);
  assert.equal(Object.hasOwn(result, "retryAuthorized"), false);
});

test("overseas target keeps its frozen region while every CloudAudit query uses fixed Guangzhou API region", async () => {
  const f = fixture();
  f.rows[0].resource!.providerData.region = "na-siliconvalley";
  const overseasEvent = editEvent((inner, outer) => {
    outer.ResourceRegion = "na-siliconvalley";
    inner.eventRegion = "na-siliconvalley";
  });
  f.input.lookUpEvents = async (body, region) => {
    f.calls.push({ body, region });
    return f.calls.length === 1 ? page([], "overseas-next", false) : page([overseasEvent]);
  };
  const result = await runComputeProviderRequestAudit(f.input);
  assert.equal(result.status, "unknown"); assert.equal(result.errorCode, "caller_identity_unbound");
  assert.equal(result.event?.apiErrorClass, "UnsupportedOperation");
  assert.equal(result.query.apiRegion, "ap-guangzhou");
  assert.equal(result.query.complete, true); assert.equal(result.query.pages, 2);
  const hash = (value: string) => `sha256:${createHash("sha256").update(value).digest("hex")}`;
  assert.equal(result.target?.regionDigest, hash("compute-provider-audit:region:v1\0na-siliconvalley"));
  assert.equal(f.calls.length, 2);
  for (const [index, call] of f.calls.entries()) {
    assert.equal(call.region, "ap-guangzhou");
    assert.equal(result.query.requestDigests[index], hash(canonicalJson({ body: call.body, region: "ap-guangzhou" })));
    assert.notEqual(result.query.requestDigests[index], hash(canonicalJson({ body: call.body, region: "na-siliconvalley" })));
  }
  assert.deepEqual(validateComputeProviderRequestAuditReceipt(structuredClone(result)), result);
  for (const apiRegion of ["na-siliconvalley", "ap-beijing", "ap-shanghai", null, undefined]) {
    const changed = structuredClone(result) as any;
    if (apiRegion === undefined) delete changed.query.apiRegion;
    else changed.query.apiRegion = apiRegion;
    assert.throws(() => validateComputeProviderRequestAuditReceipt(changed));
  }
  f.input.lookUpEvents = async (_body, region) => {
    assert.equal(region, "ap-guangzhou");
    return page(); // Guangzhou resource event must not match the overseas target.
  };
  const foreign = await runComputeProviderRequestAudit(f.input);
  assert.equal(foreign.errorCode, "no_matching_event"); assert.equal(foreign.event, null);
});

test("SQL duplicate, missing, foreign or altered rows block before any provider call", async () => {
  const edits = [
    (r: ReturnType<typeof sqlRows>) => { r.push(structuredClone(r[1])); },
    (r: ReturnType<typeof sqlRows>) => { r.pop(); },
    (r: ReturnType<typeof sqlRows>) => { r[1].accountId = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { r[1].operationId = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { r[2].originalDestroyOperationId = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { r[1].requestHash = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { r[2].finishedAt = "2026-11-11T01:32:32Z"; },
    (r: ReturnType<typeof sqlRows>) => { r[2].errorCode = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { r[0].resource!.provider = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { delete (r[0].resource!.providerData as Record<string, unknown>).region; },
    (r: ReturnType<typeof sqlRows>) => { r[0].resource!.providerData.machineName = "foreign"; },
    (r: ReturnType<typeof sqlRows>) => { r[1].resourceKind = "workspace_launch_stage"; },
  ];
  for (const edit of edits) {
    const f = fixture(); edit(f.rows);
    const result = await runComputeProviderRequestAudit(f.input);
    assert.equal(result.status, "blocked"); assert.equal(result.errorCode, "journal_binding_invalid"); assert.equal(f.calls.length, 0); assert.equal(result.event, null);
  }
});

test("foreign binding/journal, malformed inputs and SQL expressions are refused", async () => {
  for (const key of ["workspaceIdDigest", "computeIdDigest"] as const) {
    const f = fixture(); f.input.journal[key] = `sha256:${"f".repeat(64)}`;
    await assert.rejects(runComputeProviderRequestAudit(f.input)); assert.equal(f.calls.length, 0);
  }
  assert.throws(() => computeProviderRequestAuditSql("compute' OR true --"));
  assert.throws(() => computeProviderRequestAuditSql("compute\n"));
});

test("exact parameters and region filter foreign target events without accepting multi-machine or different mode calls", async () => {
  for (const edit of [
    (i: any, e: any) => { e.ResourceRegion = "ap-shanghai"; },
    (i: any) => { i.requestParameters.ClusterId = "cls-foreign"; },
    (i: any) => { i.requestParameters.MachineNames = ["machine-foreign"]; },
    (i: any) => { i.requestParameters.MachineNames.push("machine-foreign"); },
    (i: any) => { i.requestParameters.InstanceDeleteMode = "terminate"; },
    (i: any) => { i.requestParameters.EnableScaleDown = false; },
  ]) {
    const f = fixture(); f.input.lookUpEvents = async () => page([editEvent(edit)]);
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.status, "unknown"); assert.equal(r.errorCode, "no_matching_event"); assert.equal(r.event, null);
  }
});

test("unknown/missing official schema or contradictory provider identities never publishes partial target evidence", async () => {
  const edits = [
    (i: any) => { delete i.requestParameters; }, (i: any) => { i.requestParameters = {}; },
    (i: any) => { i.requestParameters.EnableScaleDown = "true"; }, (i: any) => { i.eventVersion = 3; },
    (i: any) => { i.requestID = "foreign"; }, (i: any) => { i.eventSource = "foreign"; },
    (i: any) => { i.userIdentity.accountId = "100000000003"; }, (i: any) => { i.userIdentity.sessionContext.ownerUin = "100000000003"; },
    (i: any) => { delete i.userIdentity.principalId; }, (i: any) => { i.apiErrorCode = {}; },
    (i: any) => { delete i.apiErrorMessage; }, (i: any) => { i.apiErrorMessage = {}; },
  ];
  for (const edit of edits) {
    const f = fixture(); f.input.lookUpEvents = async () => page([editEvent(edit)]);
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.status, "blocked"); assert.equal(r.errorCode, "audit_schema_invalid"); assert.equal(r.event, null);
  }
});

test("official empty/string/numeric apiErrorCode forms preserve diagnostics without treating errors as no-side-effect proof", async () => {
  for (const [code, klass] of [["", null], [0, "success_code"], ["0", "success_code"], [42, "numeric_error"], ["InternalError.Private", "InternalError"], ["private-code", null]] as const) {
    const f = fixture(); f.input.lookUpEvents = async () => page([editEvent(i => { i.apiErrorCode = code; })]);
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.errorCode, "caller_identity_unbound"); assert.equal(r.event?.apiErrorClass, klass);
    assert.equal(JSON.stringify(r).includes("private-code"), false);
  }
});

test("all pages must finish; token loops and the five-page bound never certify an event", async () => {
  const f = fixture(); let calls = 0;
  f.input.lookUpEvents = async (body, region) => {
    assert.equal(region, "ap-guangzhou"); calls++;
    if (calls === 1) { assert.equal(Object.hasOwn(body, "NextToken"), false); return page([event()], "private-next", false); }
    assert.equal(body.NextToken, "private-next"); return page([], "", true);
  };
  const r = await runComputeProviderRequestAudit(f.input);
  assert.equal(r.query.pages, 2); assert.equal(r.errorCode, "caller_identity_unbound");
  for (const looping of [true, false]) {
    const g = fixture(); let n = 0;
    g.input.lookUpEvents = async () => page(n++ === 0 ? [event()] : [], looping ? "same-private-token" : `private-token-${n}`, false);
    const r = await runComputeProviderRequestAudit(g.input);
    assert.equal(r.errorCode, "incomplete_pagination"); assert.equal(r.status, "unknown"); assert.equal(r.event, null);
    assert.equal(n, looping ? 2 : 5); assert.equal(JSON.stringify(r).includes("private-token"), false);
  }
});

test("multiple matching events, duplicate events, empty results and transport failures cannot attribute a fence", async () => {
  const second = editEvent((i, e) => { e.EventId = "private-second-event"; e.RequestID = "private-second-request"; i.requestID = e.RequestID; });
  for (const [reply, expected] of [[page([event(), second]), "ambiguous_events"], [page([event(), event()]), "audit_schema_invalid"], [page([]), "no_matching_event"],
    [{ Response: { Error: { Code: "AuthFailure", Message: "private-secret" }, RequestId: "private-request" } }, "audit_unavailable"]] as const) {
    const f = fixture(); f.input.lookUpEvents = async () => reply;
    const r = await runComputeProviderRequestAudit(f.input); assert.equal(r.errorCode, expected); assert.equal(r.event, null);
  }
  for (const transport of ["sql", "audit"]) {
    const f = fixture();
    if (transport === "sql") f.input.select = async () => { throw new Error("secret-db-host"); };
    else f.input.lookUpEvents = async () => { throw new Error("secret-credential"); };
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.errorCode, transport === "sql" ? "sql_unavailable" : "audit_unavailable");
    assert.equal(r.status, "blocked"); assert.doesNotMatch(JSON.stringify(r), /secret-db-host|secret-credential/);
  }
});

test("strict receipt validation rejects leakage, attribution, malformed digests and contradictory paging/window state", async () => {
  const r = await runComputeProviderRequestAudit(fixture().input);
  for (const edit of [
    (v: any) => { v.retryAuthorized = true; }, (v: any) => { v.status = "matched"; }, (v: any) => { v.event.actor = "private"; },
    (v: any) => { v.event.apiErrorClass = "private-message"; }, (v: any) => { v.event.apiMessageDigest = "raw message"; },
    (v: any) => { v.event.identityBinding = "fence_verified"; }, (v: any) => { v.query.complete = false; },
    (v: any) => { v.query.pages++; }, (v: any) => { v.window.endTime++; }, (v: any) => { v.window.startUtc = "invalid"; },
    (v: any) => { v.stage.startedAt = "2026-02-30T00:00:00Z"; }, (v: any) => { v.owner.accountIdDigest = "business-account"; },
    (v: any) => { v.provenance.workflowPath = "foreign"; }, (v: any) => { v.candidate.cloudImageRevision = "f".repeat(40); },
  ]) { const changed = structuredClone(r); edit(changed); assert.throws(() => validateComputeProviderRequestAuditReceipt(changed)); }
});

// Model the exact outer/inner operation-id selection; the main thread also
// executes this projection against real owner migrations and PostgreSQL.
test("sparse same-operation rejected admission rows stay outside SQL lifecycle selection; foreign non-rejected rows do not", async () => {
  const f = fixture();
  const rejected = { ...f.rows[1], id: "fop_rejected_sparse", status: "rejected", workspaceId: "", accountId: "", resourceId: "", resource: null };
  const history = [...f.rows, rejected];
  f.input.select = async sql => {
    assert.match(sql, /FROM fabric_operations WHERE status <> 'rejected' AND operation_id IN/);
    assert.match(sql, /AND status IN \('started','failed','succeeded'\)/);
    const ids = new Set(history.filter(row => row.resourceId === binding.computeId &&
      ["destroy_compute_allocation", action].includes(row.action) && ["started", "failed", "succeeded"].includes(row.status)).map(row => row.operationId));
    return history.filter(row => row.status !== "rejected" && ids.has(row.operationId)).map(row => JSON.stringify(row));
  };
  assert.equal((await runComputeProviderRequestAudit(f.input)).errorCode, "caller_identity_unbound");
  history.push({ ...rejected, id: "fop_foreign_nonrejected", status: "failed" });
  const result = await runComputeProviderRequestAudit(f.input);
  assert.equal(result.status, "blocked"); assert.equal(result.errorCode, "journal_binding_invalid");
  assert.equal(f.calls.length, 1); // No additional audit query after the conflict.
});

function sha(value: string) { return `sha256:${createHash("sha256").update(value).digest("hex")}`; }
function queryError(code = "UnauthorizedOperation.PrivateDetail", message = "private query denial Secret=never-export") {
  return { Response: { Error: { Code: code, Message: message }, RequestId: "private-query-error-request" } };
}

test("CloudAudit query authorization refusals retain only fixed families and digests", async () => {
  for (const family of ["UnauthorizedOperation", "AuthFailure"]) {
    const f = fixture(), code = `${family}.PrivateDetail`, reply = queryError(code);
    f.input.lookUpEvents = async () => reply;
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.status, "blocked"); assert.equal(r.errorCode, "audit_unavailable");
    assert.equal(r.query.pages, 0); assert.equal(r.query.complete, false); assert.equal(r.event, null);
    assert.deepEqual(r.query.failure, { providerErrorClass: family, providerErrorCodeDigest: sha(code),
      providerMessageDigest: sha(reply.Response.Error.Message), responseRequestIdDigest: sha(`compute-readback:request:v1\0${reply.Response.RequestId}`) });
    assert.deepEqual(validateComputeProviderRequestAuditReceipt(structuredClone(r)), r);
    assert.doesNotMatch(JSON.stringify(r), /PrivateDetail|private query denial|never-export|private-query-error-request/);
  }
  const f = fixture(); f.input.lookUpEvents = async () => queryError("Unrecognized.PrivateDetail");
  const r = await runComputeProviderRequestAudit(f.input);
  assert.equal(r.query.failure?.providerErrorClass, null); assert.equal(r.query.failure?.providerErrorCodeDigest, sha("Unrecognized.PrivateDetail"));
});

test("malformed CloudAudit error envelopes preserve independently valid safe fields and remain blocked", async () => {
  for (const change of [
    (r: any) => { delete r.Response.RequestId; },
    (r: any) => { r.Response.RequestId = ""; },
    (r: any) => { r.Response.RequestId = "private invalid\nrequest"; },
    (r: any) => { r.Response.RequestId = 12; },
    (r: any) => { r.Response.Error.Message = { Secret: "never-export" }; },
    (r: any) => { delete r.Response.Error.Message; },
    (r: any) => { r.Response.Error.extraSecret = "never-export"; },
    (r: any) => { r.Response.Events = []; },
    (r: any) => { r.extraSecret = "never-export"; },
  ]) {
    const f = fixture(), reply = queryError("AuthFailure.PrivateDetail"); change(reply);
    f.input.lookUpEvents = async () => reply;
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.status, "blocked"); assert.equal(r.errorCode, "audit_schema_invalid"); assert.equal(r.event, null);
    assert.equal(r.query.failure?.providerErrorClass, "AuthFailure");
    assert.equal(r.query.failure?.providerErrorCodeDigest, sha("AuthFailure.PrivateDetail"));
    assert.deepEqual(validateComputeProviderRequestAuditReceipt(structuredClone(r)), r);
    assert.doesNotMatch(JSON.stringify(r), /PrivateDetail|private invalid|never-export|private-query-error-request/);
  }
  for (const error of [null, [], { Code: 123, Message: "private-message" }, { Message: "private-message" }]) {
    const f = fixture(); f.input.lookUpEvents = async () => ({ Response: { Error: error, RequestId: "private-query-error-request" } });
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.status, "blocked"); assert.equal(r.errorCode, "audit_schema_invalid"); assert.equal(r.query.failure?.providerErrorClass, null);
    assert.equal(r.query.failure?.providerErrorCodeDigest, null);
    assert.equal(r.query.failure?.providerMessageDigest, error !== null && !Array.isArray(error) ? sha("private-message") : null);
  }
});

test("a later-page query refusal discards candidate events but retains the failing query diagnostics", async () => {
  const f = fixture(); let n = 0;
  f.input.lookUpEvents = async () => n++ === 0 ? page([event()], "private-next", false) : queryError();
  const r = await runComputeProviderRequestAudit(f.input);
  assert.equal(n, 2); assert.equal(r.query.pages, 1); assert.equal(r.query.requestDigests.length, 2);
  assert.equal(r.query.responseRequestDigests.length, 1); assert.equal(r.query.failure?.providerErrorClass, "UnauthorizedOperation");
  assert.equal(r.event, null); assert.equal(r.status, "blocked"); assert.equal(r.query.complete, false);
});

test("query failure decoder rejects leaked fields, unsupported families and contradictory receipt states", async () => {
  const f = fixture(); f.input.lookUpEvents = async () => queryError();
  const r = await runComputeProviderRequestAudit(f.input);
  for (const edit of [
    (v: any) => { v.query.failure.Code = "private raw code"; },
    (v: any) => { v.query.failure.providerErrorClass = "success_code"; },
    (v: any) => { v.query.failure.providerErrorClass = "UnauthorizedOperation.PrivateDetail"; },
    (v: any) => { v.query.failure.providerErrorCodeDigest = null; },
    (v: any) => { v.query.failure.providerMessageDigest = "private raw message"; },
    (v: any) => { v.query.failure.responseRequestIdDigest = null; },
    (v: any) => { v.query.failure.responseRequestIdDigest = "private request id"; },
    (v: any) => { delete v.query.failure.providerMessageDigest; },
    (v: any) => { v.query.complete = true; },
    (v: any) => { v.query.requestDigests = []; },
    (v: any) => { v.status = "unknown"; },
    (v: any) => { v.errorCode = "no_matching_event"; },
  ]) { const altered = structuredClone(r); edit(altered); assert.throws(() => validateComputeProviderRequestAuditReceipt(altered)); }
  const normal = await runComputeProviderRequestAudit(fixture().input);
  assert.equal(normal.query.failure, null);
  normal.query.failure = r.query.failure;
  assert.throws(() => validateComputeProviderRequestAuditReceipt(normal));
});

test("malformed SQL projection rows return a validated blocked journal receipt without querying CloudAudit", async () => {
  const original = sqlRows().map(row => JSON.stringify(row));
  const extra = JSON.parse(original[0]); extra.extraPrivateField = "never-export";
  const missing = JSON.parse(original[0]); delete missing.resourceKind;
  const malformedResource = JSON.parse(original[0]); malformedResource.resource = "private malformed resource";
  for (const first of [JSON.stringify(extra), JSON.stringify(missing), "{invalid-private-json", "null", "[]", JSON.stringify(malformedResource)]) {
    const f = fixture(); f.input.select = async () => [first, ...original.slice(1)];
    const r = await runComputeProviderRequestAudit(f.input);
    assert.equal(r.status, "blocked"); assert.equal(r.errorCode, "journal_binding_invalid");
    assert.equal(r.stage, null); assert.equal(r.target, null); assert.equal(r.window, null); assert.equal(r.event, null);
    assert.equal(r.query.pages, 0); assert.deepEqual(r.query.requestDigests, []); assert.equal(r.query.failure, null);
    assert.equal(f.calls.length, 0);
    assert.deepEqual(validateComputeProviderRequestAuditReceipt(structuredClone(r)), r);
    assert.doesNotMatch(JSON.stringify(r), /never-export|invalid-private-json|private malformed resource/);
  }
});


test("response envelope and pagination schema failures retain safe field stages without raw data", async () => {
  const cases: Array<[string, (reply: any) => unknown]> = [
    [RESPONSE_ERROR.ENVELOPE, () => null],
    [RESPONSE_ERROR.ENVELOPE, () => ({ Response: null })],
    [RESPONSE_ERROR.FIELDS, reply => { delete reply.Response.Events; return reply; }],
    [RESPONSE_ERROR.FIELDS, reply => { reply.Response.privateExtra = "never-export"; return reply; }],
    [RESPONSE_ERROR.NEXT_TOKEN, reply => { reply.Response.NextToken = {}; return reply; }],
    [RESPONSE_ERROR.EVENTS, reply => { reply.Response.Events = null; return reply; }],
    [RESPONSE_ERROR.COMPLETION, reply => { reply.Response.ListOver = 1; return reply; }],
    [RESPONSE_ERROR.COUNT, reply => { reply.Response.TotalCount = null; return reply; }],
    [RESPONSE_ERROR.REQUEST_ID, reply => { reply.Response.RequestId = null; return reply; }],
  ];
  for (const [expected, edit] of cases) {
    const f = fixture(); f.input.lookUpEvents = async () => edit(page());
    const result = await runComputeProviderRequestAudit(f.input);
    assert.equal(result.status, "blocked"); assert.equal(result.errorCode, expected);
    assert.equal(result.query.pages, 0); assert.equal(result.query.complete, false); assert.equal(result.event, null);
    assert.doesNotMatch(JSON.stringify(result), /privateExtra|never-export|private-audit-query-request/);
    assert.deepEqual(validateComputeProviderRequestAuditReceipt(result), result);
  }
});


test("provider count metadata does not override explicit event pages or completion", async () => {
  const f = fixture();
  f.input.lookUpEvents = async () => ({ Response: { ...page().Response, TotalCount: 0, NextToken: "terminal-nonempty-token" } });
  const result = await runComputeProviderRequestAudit(f.input);
  assert.equal(result.errorCode, "caller_identity_unbound"); assert.equal(result.status, "unknown");
  assert.equal(result.query.pages, 1); assert.equal(result.query.complete, true);
  assert.equal(result.event?.apiErrorClass, "UnsupportedOperation");
  assert.deepEqual(validateComputeProviderRequestAuditReceipt(result), result);
});
