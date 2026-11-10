import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { candidateReceipt } from "../helpers/protected-qualification-fixture.ts";
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
function fixture(response: { status: number; body: unknown } = { status: HTTP.OK, body: observation() }) {
  const calls: string[] = [];
  const access: ComputeReadbackAccess = {
    async select(owner, sql) {
      calls.push(`select:${owner}`); assert.match(sql, /^SELECT /); assert.doesNotMatch(sql, /\b(?:UPDATE|INSERT|DELETE|DROP|ALTER)\b/);
      return [JSON.stringify(owner === "workspace" ? workspaceRow() : fabricRow())];
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
  assert.deepEqual(f.calls, ["select:workspace", "select:fabric", "open", `/fabric/compute-allocations/${computeId}/destroy-status`, "repin-and-close"]);
  const text = JSON.stringify(r);
  for (const secret of [workspaceId, workspaceOperationId, tenantId, computeId, fabricOperationId, "ins-private", "10.8.9.10", "private-account", "private-provider-request", "never-export", "private-pod-uid"]) assert.equal(text.includes(secret), false, secret);
  assert.deepEqual(validateWorkspaceComputeLiveReadbackReceipt(JSON.parse(text)), r);
  assert.equal(r.provenance.diagnosticSourceSha, provenance.diagnosticSourceSha);
  assert.notEqual(r.provenance.diagnosticSourceSha, r.candidate.productSha);
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
  assert.equal(r.observation?.tkeStatus.value, null); assert.ok(r.observation?.tkeStatus.digest);
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
    assert.equal(r.installation, null); assert.equal(r.httpStatus, null); assert.doesNotMatch(JSON.stringify(r), /never-export|kubeconfig/);
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