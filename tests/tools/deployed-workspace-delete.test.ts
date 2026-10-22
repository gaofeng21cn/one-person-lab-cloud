import assert from "node:assert/strict";
import test from "node:test";
import type { WorkspaceOwnerDeletionDTO, WorkspaceOwnerOperationDTO, WorkspaceOwnerDTO } from "../../apps/console-ui/src/api/dtos.ts";
import {
  assertDeployedDeleteRequest, assertDeployedDeleteTarget, deployedCloudIdentityDigest,
  deployedDeleteIdempotencyKey, runDeployedWorkspaceDelete, validateDeployedDeleteReceipt
} from "../../tools/deployed-workspace-delete.ts";

const CANDIDATE = `sha256:${"a".repeat(64)}`;
const WORKSPACE_ID = "workspace_298869134c959a28727aec8d5b4c3c44";
const WORKSPACE_NAME = "customer workspace";
const BASE_URL = "https://workspace.example.test";
const DATE = "2026-10-23T00:00:00Z";
const identityDigest = deployedCloudIdentityDigest("actor-1", "tenant-1");
const request = (overrides = {}) => ({
  baseURL: BASE_URL, authorityClass: "production", candidateReceiptDigest: CANDIDATE,
  cloudIdentityDigest: identityDigest, workspaceId: WORKSPACE_ID, workspaceName: WORKSPACE_NAME,
  idempotencyIdentity: "run-1", ...overrides
});
const workspace = {
  id: WORKSPACE_ID, name: WORKSPACE_NAME, status: "active", computePlanId: "compute", storagePlanId: "storage",
  deliveryModel: "agent_saas", resourceReadiness: "ready", applicationAvailability: "ready", version: "1", createdAt: DATE, updatedAt: DATE
} satisfies WorkspaceOwnerDTO;
const deleted = { ...workspace, status: "deleted" } satisfies WorkspaceOwnerDTO;
const deletion = (overrides: Partial<WorkspaceOwnerDeletionDTO> = {}): WorkspaceOwnerDeletionDTO => ({
  workspaceId: WORKSPACE_ID, operationId: "op-delete-1", resourceDeletionStatus: "confirmed", dataDeletionStatus: "confirmed",
  refundStatus: "confirmed", refundOperationId: "wallet-refund-original", updatedAt: DATE, ...overrides
});
const operation = (overrides: Partial<WorkspaceOwnerOperationDTO> = {}): WorkspaceOwnerOperationDTO => ({
  operationId: "op-delete-1", owner: "workspace", kind: "delete_workspace", resourceId: WORKSPACE_ID,
  status: "succeeded", stage: "succeeded", observationResult: "confirmed", requestId: "request-original", createdAt: DATE, updatedAt: DATE, ...overrides
});

function transport({ deletions = [null, deletion()], operations = [operation()], workspaces = [workspace, deleted], deleteAnswer = undefined } = {}) {
  const calls = [];
  const waits = [];
  const dq = [...deletions], oq = [...operations], wq = [...workspaces];
  const next = q => q.length > 1 ? q.shift() : q[0];
  const http = {
    async request(path) {
      calls.push({ method: "GET", path });
      let payload;
      if (path === "/api/v2/auth/session") payload = { actorId: "actor-1", tenantId: "tenant-1", displayName: "A display name, not an email", role: "owner", csrfToken: "csrf-current", permissions: [], expiresAt: "2099-01-01T00:00:00Z" };
      else if (path.endsWith("/deletion")) payload = next(dq);
      else if (path.startsWith("/api/v2/operations/workspace/")) payload = next(oq);
      else if (path === `/api/v2/workspaces/${WORKSPACE_ID}`) payload = next(wq);
      else throw new Error(`unexpected route ${path}`);
      return { payload, response: { status: payload === undefined ? 404 : 200 } };
    },
    async json(path, init) {
      calls.push({ method: init.method, path, body: init.body, headers: init.headers });
      if (deleteAnswer instanceof Error) throw deleteAnswer;
      return { payload: deleteAnswer || operation({ status: "accepted", stage: "admission", pollAfterSeconds: 30 }), response: { status: 202 } };
    }
  };
  return { http, calls, waits, caller: { origin: BASE_URL, http, auth: { cookie: "opaque" }, wait: async ms => waits.push(ms) } };
}

test("the deployed delete refuses any target that is not one declared production origin", () => {
  assert.equal(assertDeployedDeleteTarget(request()), BASE_URL);
  for (const baseURL of ["http://workspace.example.test", "https://localhost", "https://127.0.0.1", "https://10.1.2.3", "https://192.168.1.5", "https://172.16.0.9", "https://[::1]", `${BASE_URL}/path`, `${BASE_URL}/?q=1`, "https://user:pass@workspace.example.test"]) {
    assert.throws(() => assertDeployedDeleteTarget(request({ baseURL })), /production origin/);
  }
  assert.throws(() => assertDeployedDeleteTarget(request({ authorityClass: "fixture" })), /production origin/);
});

test("the deployed delete accepts a current owner Workspace identity and rejects malformed declarations", () => {
  const input = request();
  assert.equal(assertDeployedDeleteRequest(input), input);
  const { workspaceName: _omitted, ...missing } = input;
  assert.throws(() => assertDeployedDeleteRequest(missing), /production origin/);
  for (const invalid of [{ workspaceName: " padded " }, { workspaceId: "" }, { candidateReceiptDigest: "sha256:short" }, { extra: true }]) {
    assert.throws(() => assertDeployedDeleteRequest(request(invalid)), /production origin/);
  }
});

test("Cloud declaration binds the actor and Tenant instead of a display name or legacy account", () => {
  assert.notEqual(identityDigest, deployedCloudIdentityDigest("actor-1", "tenant-2"));
  assert.notEqual(identityDigest, deployedCloudIdentityDigest("actor-2", "tenant-1"));
  assert.throws(() => deployedCloudIdentityDigest("actor-1", ""));
  assert.notEqual(deployedDeleteIdempotencyKey(CANDIDATE, "run-1"), deployedDeleteIdempotencyKey(`sha256:${"b".repeat(64)}`, "run-1"));
});

test("the deployed delete follows the current Cloud owner protocol and tombstone", async () => {
  const t = transport({
    deletions: [null, deletion({ resourceDeletionStatus: "pending", dataDeletionStatus: "pending", refundStatus: "requested" }), deletion()],
    operations: [operation({ status: "running", stage: "compute_deletion", pollAfterSeconds: 30 }), operation()]
  });
  const receipt = await runDeployedWorkspaceDelete(request(), t.caller);
  assert.equal(receipt.operation.status, "succeeded");
  assert.equal(receipt.deletion.workspaceAbsent, true);
  assert.equal(receipt.deletion.refundOperationId, "wallet-refund-original");
  assert.deepEqual(t.waits, [30000]);
  const mutations = t.calls.filter(c => c.method === "DELETE");
  assert.equal(mutations.length, 1);
  assert.deepEqual(mutations[0].body, { confirmationName: WORKSPACE_NAME, acknowledgeDataDestruction: true });
  assert.equal(mutations[0].headers["x-csrf-token"], "csrf-current");
  assert.equal(t.calls.some(c => c.path === "/api/auth/me"), false);
  assert.equal(JSON.stringify(receipt).includes(WORKSPACE_NAME), false);
  assert.equal(receipt.mutations.observed.deletions, 1);
});

test("the deployed delete resumes the original accepted operation without another destruction", async () => {
  const t = transport({ deletions: [deletion()], workspaces: [deleted] });
  const receipt = await runDeployedWorkspaceDelete(request(), t.caller);
  assert.equal(receipt.operation.resumed, true);
  assert.equal(receipt.mutations.observed.deletions, 0);
  assert.equal(receipt.deletion.workspaceAbsent, true);
  assert.equal(t.calls.some(c => c.method === "DELETE"), false);
  assert.deepEqual(t.waits, []);
});

test("a lost command response follows the recorded intent without issuing another command", async () => {
  const t = transport({ deleteAnswer: new Error("response lost"), deletions: [null, deletion()] });
  const receipt = await runDeployedWorkspaceDelete(request(), t.caller);
  assert.equal(receipt.operation.operationId, "op-delete-1");
  assert.equal(t.calls.filter(c => c.method === "DELETE").length, 1);
});

test("owner attention preserves confirmed resource absence and independent unknown refund", async () => {
  const t = transport({ deletions: [deletion({ refundStatus: "unknown" })], operations: [operation({ status: "needs_attention", stage: "refund", pollAfterSeconds: 5, errorCode: "DEPENDENCY_UNAVAILABLE" })], workspaces: [deleted] });
  const receipt = await runDeployedWorkspaceDelete(request(), t.caller);
  assert.equal(receipt.operation.status, "needs_attention");
  assert.equal(receipt.deletion.workspaceAbsent, true);
  assert.equal(receipt.deletion.refundStatus, "unknown");
  assert.deepEqual(t.waits, []);
});

test("success requires both the owner tombstone and resource and data absence", async () => {
  for (const options of [{ workspaces: [workspace] }, { deletions: [null, deletion({ dataDeletionStatus: "unknown" })] }]) {
    const t = transport(options);
    await assert.rejects(() => runDeployedWorkspaceDelete(request(), t.caller), /without owner-confirmed/);
  }
});

test("the deployed delete refuses a different declared Cloud scope or Workspace name", async () => {
  for (const overrides of [{ cloudIdentityDigest: `sha256:${"c".repeat(64)}` }, { workspaceName: "another workspace" }]) {
    const t = transport();
    await assert.rejects(() => runDeployedWorkspaceDelete(request(overrides), t.caller), /does not match/);
    assert.equal(t.calls.some(c => c.method === "DELETE"), false);
  }
});

test("deletion readback cannot switch to another operation or consume the legacy deletion shape", async () => {
  for (const invalid of [deletion({ operationId: "another-op" }), { workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "deleted" }]) {
    const t = transport({ deletions: [null, invalid] });
    await assert.rejects(() => runDeployedWorkspaceDelete(request(), t.caller), /invalid owner deletion|accepted operation/);
  }
});

test("the deployed delete rejects unrelated operations and invalid nonterminal schedules", async () => {
  for (const overrides of [{ owner: "fabric" }, { resourceId: "another-workspace" }, { kind: "create_workspace" }, { status: "running", pollAfterSeconds: 0 }, { status: "running", pollAfterSeconds: undefined }]) {
    const t = transport({ operations: [operation(overrides)] });
    await assert.rejects(() => runDeployedWorkspaceDelete(request(), t.caller), /invalid or unrelated owner operation/);
  }
});

test("a missing owner tombstone or inaccessible deletion cannot become confirmed absence", async () => {
  for (const options of [{ workspaces: [workspace, undefined] }, { deletions: [undefined] }]) {
    const t = transport(options);
    await assert.rejects(() => runDeployedWorkspaceDelete(request(), t.caller), /could not read/);
  }
});

test("the deployed delete receipt is closed and binds its candidate, result and mutation count", async () => {
  const t = transport();
  const base = await runDeployedWorkspaceDelete(request(), t.caller);
  assert.equal(validateDeployedDeleteReceipt(base), base);
  for (const altered of [{ ...base, deletion: { ...base.deletion, workspaceAbsent: false } }, { ...base, operation: { ...base.operation, resumed: true } }, { ...base, extra: true }, { ...base, operation: { ...base.operation, status: "deleted" } }]) {
    assert.throws(() => validateDeployedDeleteReceipt(altered), /receipt is invalid/);
  }
});

test("delete declarations preserve opaque ids and Unicode confirmation names from the contract", () => {
  const input = request({ workspaceId: "opaque/resource identity", workspaceName: "我的工作环境" });
  assert.equal(assertDeployedDeleteRequest(input), input);
});

test("succeeded operation readback must carry the owner confirmed observation", async () => {
  const t = transport({ operations: [operation({ observationResult: "unknown" })] });
  await assert.rejects(() => runDeployedWorkspaceDelete(request(), t.caller), /invalid or unrelated owner operation/);
});
