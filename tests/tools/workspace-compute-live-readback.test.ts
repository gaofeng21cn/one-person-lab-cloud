import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, realpath, rm, stat, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { candidateReceipt } from "../helpers/protected-qualification-fixture.ts";
import { main as importComputeEvidence, validateWorkspaceComputeReadbackHandoff, validateWorkspaceComputeProviderAuditHandoff } from "../../tools/workspace-compute-readback-handoff.ts";
import {
  COMPUTE_READBACK_ERROR as ERROR, COMPUTE_READBACK_STATUS as STATUS,
  runWorkspaceComputeLiveReadback, validateWorkspaceComputeLiveReadbackReceipt,
  type ComputeReadbackAccess, type ComputeReadbackProvenance,
} from "../../tools/workspace-compute-live-readback.ts";

const workspaceId = "workspace_1234567890abcdef", workspaceOperationId = "op_original_delete";
const tenantId = "tenant_original", resourceSetId = "set_original", fabricOperationId = "fabric_op_original", computeId = "compute_original";
const KIND = { WORKSPACE: "delete_workspace", FABRIC: "resource_delete", COMPUTE: "compute", ACTION: "delete" } as const;
const PROVIDER = { NAME: "tencent-tke", MACHINE: "NativeCVM", NATIVE: "Native", CXM: "CXM", DELETED: "external_deleted", RETAINED: "active", ABSENT: "NOT_FOUND", SHUTDOWN: "SHUTDOWN", RUNNING: "RUNNING", MANMADE: "MANMADE" } as const;
const HTTP = { OK: 200, ERROR: 500 } as const;
const ERROR_TOKEN = { MISMATCH: "compute_destroy_status_readback_mismatch", CVM: "tencent_read_compute_destroy_cvm_failed" } as const;
const provenance: ComputeReadbackProvenance = {
  diagnosticSourceSha: "d".repeat(40), instanceRepository: "gaofeng21cn/opl-instance-medopl", instanceSha: "b".repeat(40),
  runId: "1000", runAttempt: "2", workflowPath: ".github/workflows/cloud-workspace-compute-readback.yml",
};
function input() { return { candidate: candidateReceipt(), workspaceId, workspaceOperationId, provenance: { ...provenance } }; }
function workspaceRow() {
  return { operationId: workspaceOperationId, tenantId, resourceId: workspaceId, kind: KIND.WORKSPACE, workspaceTenantId: tenantId,
    frozenSetId: resourceSetId, resourceReadback: { workspaceId, resourceSetId }, deleteOperation: { operationId: fabricOperationId, resourceId: resourceSetId } };
}
function fabricRow() {
  return { resourceSetId, workspaceId, tenantId,
    operation: { operationId: fabricOperationId, resourceId: resourceSetId, tenantId, kind: KIND.FABRIC },
    resources: [{ id: computeId, resourceSetId, kind: KIND.COMPUTE }],
    deletionActions: [{ resourceSetId, resourceId: computeId, commandId: `${fabricOperationId}:compute`, action: KIND.ACTION,
      approvedInput: { deletionKind: KIND.COMPUTE, resourceSetId, resourceId: computeId } }] };
}
function observation() {
  return { id: computeId, accountId: "private-account", workspaceId, packageId: "private-package", provider: PROVIDER.NAME,
    status: PROVIDER.DELETED, machinePresent: false, tkeStatus: PROVIDER.ABSENT, cvmStatus: PROVIDER.ABSENT,
    observedAt: "2026-11-09T09:53:01.123456789+08:00", readbackId: "private-readback", providerRequestId: "private-provider-request",
    instanceId: "ins-private", machineName: "private-machine", privateIp: "10.8.9.10", costTags: { private: "do-not-export" },
    providerData: { machineType: PROVIDER.MACHINE, cvmApplicable: "true", describeClusterMachinesReq: "private-tke-request", describeCvmRequestId: "private-cvm-request", isolatedSource: "", private: "token=never-export" } };
}
function journalRow() {
  return { id: "fop_destroy_1", operationId: "op_destroy_original", resourceId: computeId, workspaceId, accountId: "private-account",
    action: "destroy_compute_allocation", resourceKind: "compute_allocation", status: "failed",
    errorCode: "compute_destroy_recovery_unconfirmed: private-provider-detail", retryable: true,
    providerRequestId: "private-delete-request", requestHash: "private-request-hash", startedAt: "2026-11-09T09:50:00Z",
    finishedAt: "2026-11-09T09:51:00Z", createdAt: "2026-11-09T09:51:00Z", phase: "dispatch_authorized_uncertain", mutationCount: null,
    resource: { id: computeId, workspaceId, accountId: "private-account", provider: PROVIDER.NAME }, originalDestroyOperationId: null,
    deleteRequests: { deleteMethod: null, scaleDown: null, deleteMode: null, describeClusterMachinesReq: null, describeNodePoolRequestId: null, modifySelfProvisioningReqId: null, verifyMachineDeletedReqId: null } };
}
function fixture(response: { status: number; body: unknown } = { status: HTTP.OK, body: observation() }) {
  const calls: string[] = [];
  const access: ComputeReadbackAccess = {
    async select(owner, sql) {
      calls.push(`select:${owner}`); assert.match(sql, /^SELECT /); assert.doesNotMatch(sql, /\b(?:UPDATE|INSERT|DELETE|DROP|ALTER)\b/);
      return [JSON.stringify(owner === "workspace" ? workspaceRow() : owner === "fabric" ? fabricRow() : journalRow())];
    },
    async withFabric(image, consume) {
      calls.push("open");
      const value = await consume({ async get(path) { calls.push(path); assert.equal(path, `/fabric/compute-allocations/${computeId}/destroy-status`); return response; } });
      calls.push("repin-and-close");
      return { pin: { podUid: "private-pod-uid", deploymentUid: "private-deploy-uid", replicaSetUid: "private-rs-uid",
        imageIndexDigest: image.indexDigest, platformDigest: image.platforms.find(p => p.platform === "linux/amd64")!.digest, runtimeDigest: image.indexDigest }, value };
    },
  };
  return { access, calls };
}

test("binds original deletion to one live GET and exports only owner facts and redacted installation identity", async () => {
  const f = fixture(), r = await runWorkspaceComputeLiveReadback(input(), f.access);
  assert.equal(r.status, STATUS.OBSERVED); assert.equal(r.errorCode, ERROR.NONE);
  assert.equal(r.observation?.machinePresent, false); assert.equal(r.observation?.cvmStatus.value, PROVIDER.ABSENT);
  assert.equal(r.observation?.allocationStatus.value, PROVIDER.DELETED); assert.ok(r.installation);
  assert.equal(r.schemaVersion, 2); assert.equal(r.journal?.records[0].phase, "dispatch_authorized_uncertain");
  assert.equal(r.journal?.records[0].mutationCount, null);
  assert.deepEqual(f.calls, ["select:workspace", "select:fabric", "open", "select:fabric_domain", `/fabric/compute-allocations/${computeId}/destroy-status`, "repin-and-close"]);
  const text = JSON.stringify(r);
  for (const secret of [workspaceId, workspaceOperationId, tenantId, computeId, fabricOperationId, "ins-private", "10.8.9.10", "private-account", "private-provider-request", "private-delete-request", "private-request-hash", "private-provider-detail", "never-export", "private-pod-uid"]) assert.equal(text.includes(secret), false, secret);
  assert.deepEqual(validateWorkspaceComputeLiveReadbackReceipt(JSON.parse(text)), r);
  assert.equal(r.provenance.diagnosticSourceSha, provenance.diagnosticSourceSha);
  assert.notEqual(r.provenance.diagnosticSourceSha, r.candidate.productSha);
});

test("v2 custody preserves exact journal bytes and rejects detached journal identity or missing evidence", async () => {
  const receipt = await runWorkspaceComputeLiveReadback(input(), fixture().access);
  const handoff = (value: unknown) => validateWorkspaceComputeReadbackHandoff({
    candidate: input().candidate, receiptText: `${JSON.stringify(value)}\n`,
    run: { id: Number(provenance.runId), run_attempt: Number(provenance.runAttempt),
      repository: { full_name: provenance.instanceRepository }, path: provenance.workflowPath,
      event: "workflow_dispatch", head_branch: "main", head_sha: provenance.instanceSha, status: "completed", conclusion: "success",
      actor: { login: "RenDeHuang" }, triggering_actor: { login: "RenDeHuang" } },
    expectedWorkspaceId: workspaceId, expectedWorkspaceOperationId: workspaceOperationId, expectedDiagnosticSourceSha: provenance.diagnosticSourceSha,
  });
  assert.equal(handoff(receipt).receiptStatus, STATUS.OBSERVED);
  for (const change of [
    (r: Record<string, unknown>) => { r.journal = null; },
    (r: Record<string, unknown>) => { delete r.journal; },
    (r: Record<string, unknown>) => { (r.journal as Record<string, unknown>).computeIdDigest = `sha256:${"f".repeat(64)}`; },
    (r: Record<string, unknown>) => { (r.journal as Record<string, unknown>).workspaceIdDigest = `sha256:${"f".repeat(64)}`; },
    (r: Record<string, unknown>) => { (r.journal as Record<string, unknown>).accountIdDigest = `sha256:${"f".repeat(64)}`; },
    (r: Record<string, unknown>) => { r.schemaVersion = 1; },
    (r: Record<string, unknown>) => { r.status = STATUS.BLOCKED; r.errorCode = ERROR.JOURNAL_BINDING; r.observation = null; r.installation = null; r.httpStatus = null; },
  ]) {
    const altered = structuredClone(receipt); change(altered as unknown as Record<string, unknown>);
    assert.throws(() => handoff(altered), /workspace_compute_readback_handoff_invalid/);
  }
});

test("domain database and journal failures stay distinct and never fall through to a provider request", async () => {
  for (const [result, expected] of [
    ["transport", "compute_live_fabric_domain_database_permission_denied"],
    ["malformed", ERROR.JOURNAL_BINDING], ["conflict", ERROR.JOURNAL_BINDING], ["overflow", ERROR.JOURNAL_BINDING],
  ] as const) {
    const f = fixture(), select = f.access.select;
    f.access.select = async (owner, sql) => {
      if (owner !== "fabric_domain") return select(owner, sql);
      f.calls.push(`select:${owner}`);
      if (result === "transport") throw new Error("private-error", { cause: {
        kind: "opl_compute_database_transport_failure", phase: "psql", clientCode: null,
        exitCode: 1, signal: null, stderr: "ERROR: 42501: private-database-query",
      } });
      if (result === "malformed") return ['{"private":"never-export"}'];
      if (result === "conflict") return [JSON.stringify({ ...journalRow(), workspaceId: "workspace_foreign" })];
      return Array.from({ length: 101 }, (_, i) => JSON.stringify({ ...journalRow(), id: `fop_${i}` }));
    };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, expected); assert.equal(r.journal, null);
    assert.ok(r.computeIdDigest); assert.equal(r.observation, null);
    assert.deepEqual(f.calls, ["select:workspace", "select:fabric", "open", "select:fabric_domain"]);
    assert.doesNotMatch(JSON.stringify(r), /private-error|never-export|private-database-query|workspace_foreign/);
  }
});

test("unknown journal is retained independently and account drift cannot attach journal evidence", async () => {
  const f = fixture(), select = f.access.select;
  f.access.select = async (owner, sql) => owner === "fabric_domain" ? [] : select(owner, sql);
  const empty = await runWorkspaceComputeLiveReadback(input(), f.access);
  assert.equal(empty.status, STATUS.OBSERVED); assert.equal(empty.journal?.status, "unknown");
  const body = observation(); body.accountId = "foreign-account";
  const drift = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.OK, body }).access);
  assert.equal(drift.errorCode, ERROR.JOURNAL_BINDING); assert.equal(drift.journal, null); assert.equal(drift.observation, null);
  const error = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.ERROR, body: { error: ERROR_TOKEN.CVM } }).access);
  assert.equal(error.status, STATUS.BLOCKED); assert.equal(error.journal?.status, "observed"); assert.equal(error.observation, null);
});

test("CVM SHUTDOWN/MANMADE remains a present provider fact and is never changed into absence", async () => {
  const body = observation(); body.status = PROVIDER.RETAINED as typeof body.status; body.cvmStatus = PROVIDER.SHUTDOWN as typeof body.cvmStatus; body.providerData.isolatedSource = PROVIDER.MANMADE;
  const r = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.OK, body }).access);
  assert.equal(r.status, STATUS.OBSERVED); assert.equal(r.observation?.cvmStatus.value, PROVIDER.SHUTDOWN);
  assert.equal(r.observation?.isolatedSource.value, PROVIDER.MANMADE); assert.equal(r.observation?.allocationStatus.value, null);
  assert.ok(r.observation?.allocationStatus.digest); assert.equal(Object.hasOwn(r, "deletionComplete"), false);
});

test("present Machine does not fabricate a CVM query or absence", async () => {
  const body: Record<string, unknown> = observation(); body.status = PROVIDER.RETAINED; body.machinePresent = true; body.tkeStatus = PROVIDER.RUNNING;
  delete body.cvmStatus; delete (body.providerData as Record<string, string>).describeCvmRequestId;
  const r = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.OK, body }).access);
  assert.equal(r.status, STATUS.OBSERVED); assert.equal(r.observation?.machinePresent, true);
  assert.deepEqual(r.observation?.cvmStatus, { value: null, digest: null }); assert.equal(r.observation?.cvmRequestIdDigest, null);
  assert.equal(r.observation?.tkeStatus.value, PROVIDER.RUNNING); assert.ok(r.observation?.tkeStatus.digest);
});

test("TKE-only persisted types retain their own scope without claiming a CVM read", async () => {
  for (const machineType of [PROVIDER.NATIVE, PROVIDER.CXM]) {
    const body: Record<string, unknown> = observation(); const data = body.providerData as Record<string, string>;
    data.machineType = machineType; data.cvmApplicable = "false"; delete data.describeCvmRequestId; delete body.cvmStatus;
    const r = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.OK, body }).access);
    assert.equal(r.status, STATUS.OBSERVED); assert.equal(r.observation?.machineType, machineType); assert.equal(r.observation?.cvmRequestIdDigest, null);
  }
});

test("non-200 owner failures preserve safe phase tokens and digest unknown messages without exporting raw errors", async () => {
  for (const [raw, token] of [
    [ERROR_TOKEN.MISMATCH, ERROR_TOKEN.MISMATCH],
    [`${ERROR_TOKEN.CVM}:credential=never-export ins-private`, ERROR_TOKEN.CVM],
    ["unknown-provider-error token=never-export", null],
  ] as const) {
    const r = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.ERROR, body: { error: raw } }).access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, ERROR.HTTP); assert.equal(r.httpStatus, HTTP.ERROR);
    assert.equal(r.observation, null); assert.equal(r.failure?.errorToken, token); assert.match(r.failure!.errorDigest, /^sha256:[a-f0-9]{64}$/);
    assert.doesNotMatch(JSON.stringify(r), /never-export|ins-private|unknown-provider-error/); assert.ok(r.installation);
  }
});

test("owner database access, SQLSTATE and decoder failures remain distinguishable without exporting diagnostics", async () => {
  for (const [owner, errorCode] of [
    ["workspace", ERROR.WORKSPACE_ACCESS], ["fabric", ERROR.FABRIC_ACCESS],
  ] as const) {
    const f = fixture();
    f.access.select = async selected => {
      f.calls.push(`select:${selected}`);
      if (selected === owner) throw new Error("private-database-error token=never-export");
      return [JSON.stringify(selected === "workspace" ? workspaceRow() : fabricRow())];
    };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, errorCode); assert.equal(r.computeIdDigest, null);
    assert.equal(r.installation, null); assert.equal(r.observation, null);
    assert.deepEqual(f.calls, owner === "workspace" ? ["select:workspace"] : ["select:workspace", "select:fabric"]);
    assert.doesNotMatch(JSON.stringify(r), /private-database-error|never-export/);
  }
  for (const [owner, stderr, errorCode] of [
    ["workspace", "ERROR: 42501: permission denied for relation private_table", "compute_live_workspace_database_permission_denied"],
    ["workspace", "ERROR: 42P01: relation private_table does not exist", "compute_live_workspace_database_schema_unavailable"],
    ["fabric", "ERROR: 25006: cannot execute DELETE in a read-only transaction", "compute_live_fabric_database_sqlstate_25006"],
    ["fabric", 'psql: error: could not translate host name "private-db" to address: Name or service not known', "compute_live_fabric_database_dns_unavailable"],
    ["workspace", 'ERROR: 42501: permission denied for relation "ssl_timeout"', "compute_live_workspace_database_permission_denied"],
    ["workspace", 'psql: error: invalid target_session_attrs value: "read-only"', "compute_live_workspace_database_target_session_attrs_invalid"],
    ["workspace", "unrecognized error containing timeout SSL private-db", "compute_live_workspace_database_psql_exit_1"],
  ] as const) {
    const f = fixture();
    f.access.select = async selected => {
      f.calls.push(`select:${selected}`);
      if (selected === owner) {
        const error = new Error("compute_access_database_failed");
        error.cause = { kind: "opl_compute_database_transport_failure", phase: "psql", clientCode: null,
          exitCode: 1, signal: null, stderr };
        throw error;
      }
      return [JSON.stringify(selected === "workspace" ? workspaceRow() : fabricRow())];
    };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, errorCode);
    assert.equal(r.computeIdDigest, null); assert.equal(r.observation, null);
    assert.deepEqual(f.calls, owner === "workspace" ? ["select:workspace"] : ["select:workspace", "select:fabric"]);
    assert.doesNotMatch(JSON.stringify(r), /private-db|permission denied|private_table|DELETE/);
  }
  for (const [owner, errorCode] of [
    ["workspace", ERROR.WORKSPACE_BINDING], ["fabric", ERROR.FABRIC_BINDING],
  ] as const) {
    const f = fixture();
    f.access.select = async selected => {
      f.calls.push(`select:${selected}`);
      const value = selected === owner ? { malformed: "private-customer" } : selected === "workspace" ? workspaceRow() : fabricRow();
      return [JSON.stringify(value)];
    };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, errorCode); assert.equal(r.computeIdDigest, null);
    assert.equal(r.installation, null); assert.equal(r.observation, null);
    assert.deepEqual(f.calls, owner === "workspace" ? ["select:workspace"] : ["select:workspace", "select:fabric"]);
    assert.doesNotMatch(JSON.stringify(r), /private-customer/);
  }
});

test("database phase and client diagnostics stay bounded, owner-scoped and non-authoritative", async () => {
  const base = { kind: "opl_compute_database_transport_failure", phase: "psql", clientCode: null, exitCode: null, signal: null, stderr: null };
  for (const [cause, errorCode] of [
    [{ ...base, phase: "private_file", clientCode: "ENOENT" }, "compute_live_workspace_database_private_file_missing"],
    [{ ...base, phase: "private_file", clientCode: "EACCES" }, "compute_live_workspace_database_private_file_unreadable"],
    [{ ...base, phase: "connection_url", clientCode: "database_url_parameter_invalid" }, "compute_live_workspace_database_url_parameter_invalid"],
    [{ ...base, phase: "connection_url", clientCode: "database_url_invalid" }, "compute_live_workspace_database_url_invalid"],
    [{ ...base, clientCode: "postgresql_client_required" }, "compute_live_workspace_database_client_required"],
    [{ ...base, clientCode: "EACCES" }, "compute_live_workspace_database_client_spawn_failed"],
    [{ ...base, signal: "SIGTERM" }, "compute_live_workspace_database_client_interrupted"],
    [{ ...base, exitCode: 2, stderr: "unknown private-database-error" }, "compute_live_workspace_database_psql_exit_2"],
    [{ ...base, clientCode: "unknown private-database-error" }, "compute_live_workspace_database_unclassified"],
    [{ ...base, kind: "unknown" }, ERROR.WORKSPACE_ACCESS],
    [{ ...base, phase: ["psql"] }, ERROR.WORKSPACE_ACCESS],
    [{ ...base, extra: "private-database-error" }, ERROR.WORKSPACE_ACCESS],
    [{ ...base, stderr: "x".repeat(1024 * 1024 + 1) }, ERROR.WORKSPACE_ACCESS],
    [{ ...base, stderr: "中".repeat(400000) }, ERROR.WORKSPACE_ACCESS],
    [{ ...base, exitCode: "1" }, ERROR.WORKSPACE_ACCESS],
  ] as const) {
    const f = fixture();
    f.access.select = async () => { throw new Error("compute_access_database_failed", { cause }); };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.errorCode, errorCode); assert.equal(r.status, STATUS.BLOCKED);
    assert.equal(r.observation, null); assert.equal(r.installation, null); assert.equal(r.computeIdDigest, null);
    assert.doesNotMatch(JSON.stringify(r), /private-database-error|ENOENT|EACCES|SIGTERM/);
    assert.deepEqual(f.calls, []);
  }
});

test("transport, repin or cleanup failures discard the response and never expose environment diagnostics", async () => {
  for (const stage of ["before", "after"]) {
    const f = fixture(); const original = f.access.withFabric;
    f.access.withFabric = async (image, consume) => { if (stage === "after") await original(image, consume); throw new Error("private-kubeconfig token=never-export"); };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, ERROR.ACCESS); assert.equal(r.observation, null);
    assert.equal(r.installation, null); assert.equal(r.journal, null); assert.equal(r.httpStatus, null); assert.doesNotMatch(JSON.stringify(r), /never-export|kubeconfig/);
  }
});

test("wrong Candidate image or platform cannot produce a successful receipt", async () => {
  for (const field of ["imageIndexDigest", "platformDigest", "runtimeDigest"] as const) {
    const f = fixture(), original = f.access.withFabric;
    f.access.withFabric = async (image, consume) => { const result = await original(image, consume); result.pin[field] = `sha256:${"f".repeat(64)}`; return result; };
    const r = await runWorkspaceComputeLiveReadback(input(), f.access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, ERROR.IMAGE); assert.equal(r.observation, null);
  }
});

test("malformed, wrong-identity and incomplete owner responses cannot certify readback", async () => {
  const mutations: Array<(body: Record<string, unknown>) => void> = [
    b => { b.id = "other_compute"; }, b => { b.workspaceId = "other_workspace"; }, b => { b.provider = "other"; },
    b => { b.machinePresent = "false"; }, b => { delete b.readbackId; }, b => { b.observedAt = "2026-02-30T01:00:00Z"; },
    b => { b.observedAt = "2026-11-09T01:00:00+99:99"; }, b => { delete b.providerRequestId; },
    b => { delete (b.providerData as Record<string, string>).describeCvmRequestId; },
    b => { delete (b.providerData as Record<string, string>).describeClusterMachinesReq; },
    b => { b.cvmStatus = PROVIDER.SHUTDOWN; }, b => { b.machinePresent = true; },
    b => { (b.providerData as Record<string, unknown>).untrusted = {}; },
  ];
  for (const mutation of mutations) {
    const body: Record<string, unknown> = observation(); mutation(body);
    const r = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.OK, body }).access);
    assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.observation, null);
  }
  const r = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.ERROR, body: { error: ERROR_TOKEN.CVM, secret: "never-export" } }).access);
  assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.failure, null);
});

test("strict receipt decoder rejects contradictory state, leaked keys and forged tokens", async () => {
  const receipt = await runWorkspaceComputeLiveReadback(input(), fixture().access);
  const changes: Array<(r: Record<string, unknown>) => void> = [
    r => { r.rawError = "secret"; }, r => { r.status = STATUS.BLOCKED; }, r => { r.observation = null; },
    r => { r.httpStatus = HTTP.ERROR; }, r => { r.installation = null; }, r => { r.completedAt = "2026-02-30T00:00:00Z"; },
    r => { (r.candidate as Record<string, unknown>).cloudImageRevision = "f".repeat(40); },
    r => { (r.provenance as Record<string, unknown>).instanceRepository = "attacker/repo"; },
    r => { (r.provenance as Record<string, unknown>).runAttempt = "0"; },
    r => { const o = r.observation as Record<string, unknown>; o.rawInstanceId = "ins-secret"; },
    r => { (r.observation as Record<string, unknown>).cvmStatus = { value: PROVIDER.SHUTDOWN, digest: `sha256:${"a".repeat(64)}` }; },
  ];
  for (const change of changes) { const altered = structuredClone(receipt); change(altered as unknown as Record<string, unknown>); assert.throws(() => validateWorkspaceComputeLiveReadbackReceipt(altered)); }
  const error = await runWorkspaceComputeLiveReadback(input(), fixture({ status: HTTP.ERROR, body: { error: ERROR_TOKEN.CVM } }).access);
  error.failure!.errorToken = "private-secret"; assert.throws(() => validateWorkspaceComputeLiveReadbackReceipt(error));
});

test("CLI validates downloaded receipt bytes and rejects symlink and oversized input with a fixed error", async () => {
  const directory = await mkdtemp(join(tmpdir(), "compute-live-readback-"));
  const tool = fileURLToPath(new URL("../../tools/workspace-compute-live-readback.ts", import.meta.url));
  try {
    const receipt = await runWorkspaceComputeLiveReadback(input(), fixture().access), path = join(directory, "receipt.json");
    await writeFile(path, JSON.stringify(receipt));
    const run = (target: string) => spawnSync(process.execPath, [tool, "validate", "--receipt", target], { encoding: "utf8" });
    assert.equal(run(path).status, 0);
    await symlink(path, join(directory, "link.json")); const link = run(join(directory, "link.json"));
    assert.equal(link.status, 1); assert.equal(link.stderr.trim(), ERROR.INVALID);
    await writeFile(path, "never-export".repeat(100000)); const large = run(path);
    assert.equal(large.status, 1); assert.equal(large.stderr.trim(), ERROR.INVALID);
    assert.ok((await readFile(path)).length > 1024 * 1024);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test("optional provider audit publishes only after the pinned owner transport closes and leaves parent v2 unchanged", async () => {
  const f=fixture(), published: unknown[]=[];
  const r=await runWorkspaceComputeLiveReadback(input(),f.access,{
    async lookUpEvents(){throw new Error("missing phase must not query provider");},
    async publish(value){assert.equal(f.calls.at(-1),"repin-and-close");published.push(value);},
  });
  assert.equal(r.status,STATUS.OBSERVED,JSON.stringify({errorCode:r.errorCode,calls:f.calls,published}));assert.equal(r.schemaVersion,2);
  assert.equal(Object.hasOwn(r,"providerAudit"),false);assert.equal(published.length,1);
  assert.equal((published[0] as {status:string}).status,"blocked");
  const args={candidate:input().candidate,receiptText:JSON.stringify(r),auditReceiptText:JSON.stringify(published[0]),
    run:{id:Number(provenance.runId),run_attempt:Number(provenance.runAttempt),repository:{full_name:provenance.instanceRepository},
      path:provenance.workflowPath,event:"workflow_dispatch",head_branch:"main",head_sha:provenance.instanceSha,status:"completed",conclusion:"success"},
    expectedWorkspaceId:workspaceId,expectedWorkspaceOperationId:workspaceOperationId,expectedDiagnosticSourceSha:provenance.diagnosticSourceSha};
  const bound=validateWorkspaceComputeProviderAuditHandoff(args);
  assert.equal(bound.receiptStatus,"blocked");
  const directory=await realpath(await mkdtemp(join(tmpdir(),"compute-audit-custody-")));
  try{
    const {receiptText,auditReceiptText,...metadata}=args;
    const inputPath=join(directory,"input.json"),parentPath=join(directory,"receipt.json"),childPath=join(directory,"provider-audit.json"),outputPath=join(directory,"binding.json");
    await writeFile(inputPath,JSON.stringify(metadata));await writeFile(parentPath,receiptText);await writeFile(childPath,auditReceiptText);
    const imported=await importComputeEvidence(["import-provider-audit","--input",inputPath,"--parent-receipt",parentPath,"--receipt",childPath,"--output",outputPath]);
    assert.deepEqual(imported,bound);assert.equal((await stat(outputPath)).mode&0o777,0o600);
    assert.equal(await readFile(childPath,"utf8"),auditReceiptText);
  }finally{await rm(directory,{recursive:true,force:true});}

  for(const path of ["workspaceIdDigest","accountIdDigest","destroyOperationIdDigest"]){
    const foreign=structuredClone(published[0]) as {owner:Record<string,string>};foreign.owner[path]=`sha256:${"f".repeat(64)}`;
    assert.throws(()=>validateWorkspaceComputeProviderAuditHandoff({...args,auditReceiptText:JSON.stringify(foreign)}),/handoff_invalid/);
  }
});

test("provider audit cannot publish on a changed installation or unbound original operation",async()=>{
  for(const mode of ["pin_changed","binding_missing"]){
    const f=fixture();let published=0;
    if(mode==="pin_changed"){
      const original=f.access.withFabric;
      f.access.withFabric=async(image,consume)=>{const result=await original(image,consume);result.pin.imageIndexDigest=`sha256:${"f".repeat(64)}`;return result;};
    }else{const select=f.access.select;f.access.select=async(owner,sql)=>owner==="workspace"?[]:select(owner,sql);}
    const r=await runWorkspaceComputeLiveReadback(input(),f.access,{async lookUpEvents(){throw new Error("must not query");},async publish(){published++;}});
    assert.equal(r.status,STATUS.BLOCKED);assert.equal(published,0);
  }
});


test("audit opt-in preserves live evidence when the original journal has no lifecycle", async () => {
  const f = fixture(), select = f.access.select;
  let domainReads = 0, providerReads = 0, publishes = 0;
  f.access.select = async (owner, sql) => {
    if (owner === "fabric_domain") { domainReads++; return []; }
    return select(owner, sql);
  };
  const r = await runWorkspaceComputeLiveReadback(input(), f.access, {
    async lookUpEvents() { providerReads++; throw new Error("unbound lifecycle must not query provider"); },
    async publish() { publishes++; },
  });
  assert.equal(r.status, STATUS.OBSERVED); assert.equal(r.errorCode, ERROR.NONE);
  assert.equal(r.journal?.status, "unknown"); assert.ok(r.observation); assert.ok(r.installation);
  assert.equal(domainReads, 1); assert.equal(providerReads, 0); assert.equal(publishes, 0);
  assert.equal(f.calls.at(-1), "repin-and-close");
});

test("audit custody binds complete submillisecond journal timestamps", async () => {
  const f = fixture(), select = f.access.select, base = journalRow();
  const action = "detach_prepaid_compute_allocation";
  const startedAt = "2026-11-11T01:32:31.000000123Z", finishedAt = "2026-11-11T01:32:31.883456789Z";
  const resource = { ...base.resource, machineName: "machine_fixture", providerData: { machineType: PROVIDER.MACHINE, region: "na-siliconvalley", clusterId: "cls_fixture" } };
  const rows = [
    { ...base, resource },
    { ...base, id: "fop_detach_start", operationId: "op_detach_original", action, resourceKind: "compute_prepaid_retirement", status: "started", errorCode: "", providerRequestId: "", startedAt, finishedAt: null, createdAt: startedAt, phase: null, resource: null, originalDestroyOperationId: base.operationId },
    { ...base, id: "fop_detach_failed", operationId: "op_detach_original", action, resourceKind: "compute_prepaid_retirement", status: "failed", errorCode: "compute_prepaid_detach_dispatch_unconfirmed", providerRequestId: "", startedAt, finishedAt, createdAt: finishedAt, phase: null, resource: null, originalDestroyOperationId: base.operationId },
  ];
  f.access.select = async (owner, sql) => {
    if (owner !== "fabric_domain") return select(owner, sql);
    return rows.map(row => {
      if (sql.includes("'retryable'")) return JSON.stringify({ ...row, resource: row.resource === null ? null : base.resource });
      const { retryable, phase, mutationCount, deleteRequests, ...raw } = row;
      return JSON.stringify(raw);
    });
  };
  let child: unknown;
  const r = await runWorkspaceComputeLiveReadback(input(), f.access, {
    async lookUpEvents() { return { Response: { NextToken: "", Events: [], ListOver: true, TotalCount: 0, RequestId: "query_fixture" } }; },
    async publish(value) { child = value; },
  });
  assert.equal(r.status, STATUS.OBSERVED); assert.ok(child);
  const args = { candidate: input().candidate, receiptText: JSON.stringify(r), auditReceiptText: JSON.stringify(child),
    run: { id: Number(provenance.runId), run_attempt: Number(provenance.runAttempt), repository: { full_name: provenance.instanceRepository },
      path: provenance.workflowPath, event: "workflow_dispatch", head_branch: "main", head_sha: provenance.instanceSha, status: "completed", conclusion: "success" },
    expectedWorkspaceId: workspaceId, expectedWorkspaceOperationId: workspaceOperationId, expectedDiagnosticSourceSha: provenance.diagnosticSourceSha };
  assert.equal(validateWorkspaceComputeProviderAuditHandoff(args).errorCode, "no_matching_event");
  for (const [key, value] of [["startedAt", startedAt.replace("000000123", "000000999")], ["failedAt", finishedAt.replace("883456789", "883456999")]]) {
    const altered = structuredClone(child) as { stage: Record<string, string> };
    altered.stage[key] = value;
    assert.throws(() => validateWorkspaceComputeProviderAuditHandoff({ ...args, auditReceiptText: JSON.stringify(altered) }), /handoff_invalid/);
  }
});
