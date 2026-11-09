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

test("binding failure prevents all transport and keeps unknown evidence", async () => {
  const f = fixture(); f.access.select = async () => [JSON.stringify({ error: "private-customer" })];
  const r = await runWorkspaceComputeLiveReadback(input(), f.access);
  assert.equal(r.status, STATUS.BLOCKED); assert.equal(r.errorCode, ERROR.BINDING); assert.equal(r.computeIdDigest, null);
  assert.equal(r.installation, null); assert.equal(r.observation, null); assert.deepEqual(f.calls, []);
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
