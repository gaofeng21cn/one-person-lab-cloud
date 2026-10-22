import assert from "node:assert/strict";
import test from "node:test";
import { deployedAccountIdentityDigest } from "../../tools/deployed-workspace-purchase.ts";
import {
  assertDeployedDeleteRequest,
  assertDeployedDeleteTarget,
  deployedDeleteIdempotencyKey,
  runDeployedWorkspaceDelete,
  validateDeployedDeleteReceipt
} from "../../tools/deployed-workspace-delete.ts";

const EMAIL = "owner@example.test";
const CANDIDATE = `sha256:${"a".repeat(64)}`;
const WORKSPACE_ID = "ws-75e18d83e13db76332";
const WORKSPACE_NAME = "customer workspace";
const BASE_URL = "https://cloud.medopl.com";

const request = (overrides = {}) => ({
  baseURL: BASE_URL,
  authorityClass: "production",
  candidateReceiptDigest: CANDIDATE,
  accountEmailIdentityDigest: deployedAccountIdentityDigest(EMAIL),
  workspaceId: WORKSPACE_ID,
  workspaceName: WORKSPACE_NAME,
  idempotencyIdentity: "run-1",
  ...overrides
});

const account = { accountId: "acct-1", role: "owner", status: "active", email: EMAIL };
const workspace = { workspaceId: WORKSPACE_ID, name: WORKSPACE_NAME, accountId: "acct-1" };
const envelope = (data) => ({ source: "sub2api", available: true, status: "available", data });

/*
 * A transport that answers the four routes this entry uses. `deletions` is the
 * sequence the owner publishes on each read, so a test can drive pending ->
 * deleted without a timer.
 */
function transport({ deletions = [], workspaces = null, deleteAnswer = null } = {}) {
  const calls = [];
  const deletionQueue = [...deletions];
  const workspaceQueue = workspaces ? [...workspaces] : null;
  let workspaceState = workspace;
  const http = {
    async request(path) {
      calls.push({ method: "GET", path });
      if (path === "/api/auth/me") return { payload: envelope(account), response: { status: 200 } };
      if (path.endsWith("/deletion")) {
        if (deletionQueue.length > 0) {
          const next = deletionQueue.length > 1 ? deletionQueue.shift() : deletionQueue[0];
          return { payload: next, response: { status: 200 } };
        }
        return { payload: null, response: { status: 200 } };
      }
      if (path.startsWith("/api/v2/workspaces/")) {
        if (workspaceQueue && workspaceQueue.length > 0) {
          workspaceState = workspaceQueue.length > 1 ? workspaceQueue.shift() : workspaceQueue[0];
        }
        return { payload: workspaceState, response: { status: workspaceState ? 200 : 404 } };
      }
      throw new Error(`unexpected request ${path}`);
    },
    async json(path, init) {
      calls.push({ method: init.method, path, body: init.body, headers: init.headers });
      if (deleteAnswer) return { payload: deleteAnswer, response: { status: 202 } };
      return {
        payload: { kind: "delete_workspace", operationId: "op-delete-1", resourceId: WORKSPACE_ID, status: "pending" },
        response: { status: 202 }
      };
    }
  };
  return { http, calls };
}

const caller = (http, overrides = {}) => ({
  origin: BASE_URL,
  http,
  auth: { session: "opaque" },
  wait: async () => {},
  ...overrides
});

test("the deployed delete refuses any target that is not one production origin", () => {
  assert.equal(assertDeployedDeleteTarget(request()), BASE_URL);
  for (const baseURL of [
    "http://cloud.medopl.com",
    "https://localhost",
    "https://127.0.0.1",
    "https://10.1.2.3",
    "https://192.168.1.5",
    "https://172.16.0.9",
    "https://[::1]",
    "https://cloud.medopl.com/path",
    "https://cloud.medopl.com/?q=1",
    "https://user:pass@cloud.medopl.com"
  ]) {
    assert.throws(() => assertDeployedDeleteTarget(request({ baseURL })), /production origin/, baseURL);
  }
  assert.throws(() => assertDeployedDeleteTarget(request({ authorityClass: "fixture" })), /production origin/);
});

test("the deployed delete requires every declared input by name", () => {
  assert.deepEqual(assertDeployedDeleteRequest(request()), request());
  const { workspaceName: _omitted, ...missingName } = request();
  assert.throws(() => assertDeployedDeleteRequest(missingName), /production origin/);
  assert.throws(() => assertDeployedDeleteRequest(request({ workspaceName: " padded " })), /production origin/);
  assert.throws(() => assertDeployedDeleteRequest(request({ workspaceId: "not-a-workspace" })), /production origin/);
  assert.throws(() => assertDeployedDeleteRequest(request({ candidateReceiptDigest: "sha256:short" })), /production origin/);
  assert.throws(() => assertDeployedDeleteRequest(request({ extra: true })), /production origin/);
});

test("the deployed delete idempotency identity is scoped to the Candidate", () => {
  const key = deployedDeleteIdempotencyKey(CANDIDATE, "run-1");
  assert.equal(key, `deployed-workspace-delete:${"a".repeat(64)}:run-1`);
  assert.notEqual(key, deployedDeleteIdempotencyKey(`sha256:${"b".repeat(64)}`, "run-1"));
  assert.throws(() => deployedDeleteIdempotencyKey("sha256:short", "run-1"), /production origin/);
});

test("the deployed delete issues one command, follows it to deleted, and proves absence", async () => {
  const { http, calls } = transport({
    deletions: [
      null,
      { workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "pending" },
      { workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "deleted", stage: "compute", refundStatus: "refunded", refundOperationId: "wallet-adjustment-delete-1" }
    ],
    workspaces: [workspace, null]
  });

  const receipt = await runDeployedWorkspaceDelete(request(), caller(http));

  assert.equal(calls.filter((call) => call.method === "DELETE").length, 1);
  assert.equal(calls.find((call) => call.method === "DELETE").body.acknowledgeDataDestruction, true);
  assert.equal(calls.find((call) => call.method === "DELETE").body.confirmationName, WORKSPACE_NAME);
  assert.equal(receipt.operation.status, "deleted");
  assert.equal(receipt.operation.stage, "compute");
  assert.equal(receipt.deletion.workspaceAbsent, true);
  assert.equal(receipt.deletion.refundStatus, "refunded");
  assert.equal(receipt.deletion.refundOperationId, "wallet-adjustment-delete-1");
  assert.equal(receipt.operation.resumed, false);
  assert.equal(receipt.mutations.observed.deletions, 1);
  assert.equal(JSON.stringify(receipt).includes(WORKSPACE_NAME), false, "the receipt carries no Workspace name");
});

test("the deployed delete resumes an already accepted deletion and destroys nothing again", async () => {
  const { http, calls } = transport({
    deletions: [{ workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "deleted", stage: "compute" }],
    workspaces: [workspace, null]
  });

  const receipt = await runDeployedWorkspaceDelete(request(), caller(http));

  assert.equal(calls.filter((call) => call.method === "DELETE").length, 0, "a resume must not issue a second command");
  assert.equal(receipt.operation.resumed, true);
  assert.equal(receipt.mutations.observed.deletions, 0);
  assert.equal(receipt.deletion.workspaceAbsent, true);
});

test("the deployed delete reports a deletion that stopped for manual review", async () => {
  const { http } = transport({
    deletions: [
      null,
      { workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "pending" },
      { workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "manual_review", stage: "compute" }
    ]
  });

  const receipt = await runDeployedWorkspaceDelete(request(), caller(http));

  assert.equal(receipt.operation.status, "manual_review");
  assert.equal(receipt.deletion.workspaceAbsent, false);
  assert.equal(receipt.mutations.observed.deletions, 1);
});

test("the deployed delete refuses a deleted Workspace the owner still serves", async () => {
  const { http } = transport({
    deletions: [{ workspaceId: WORKSPACE_ID, operationId: "op-delete-1", status: "deleted", stage: "compute" }],
    workspaces: [workspace, workspace]
  });

  await assert.rejects(() => runDeployedWorkspaceDelete(request(), caller(http)), /still serves/);
});

test("the deployed delete refuses a session that is not the declared customer", async () => {
  const { http } = transport({ deletions: [] });
  await assert.rejects(
    () => runDeployedWorkspaceDelete(request({ accountEmailIdentityDigest: `sha256:${"c".repeat(64)}` }), caller(http)),
    /does not match the declared customer/
  );
});

test("the deployed delete refuses a Workspace whose name is not the declared confirmation", async () => {
  const { http } = transport({ deletions: [] });
  await assert.rejects(
    () => runDeployedWorkspaceDelete(request({ workspaceName: "another workspace" }), caller(http)),
    /does not match the declared confirmation/
  );
});

test("the deployed delete receipt ties absence, status and mutation count together", () => {
  const base = {
    schemaVersion: 1,
    kind: "opl.deployed-workspace-delete.v1",
    candidate: { receiptDigest: CANDIDATE },
    account: { identityDigest: deployedAccountIdentityDigest(EMAIL) },
    operation: {
      idempotencyKey: deployedDeleteIdempotencyKey(CANDIDATE, "run-1"),
      operationId: "op-delete-1",
      status: "deleted",
      stage: "compute",
      resumed: false
    },
    deletion: { workspaceAbsent: true, refundStatus: "refunded", refundOperationId: "wallet-adjustment-delete-1" },
    mutations: { declared: { deletions: 1 }, observed: { deletions: 1 } },
    startedAt: "2026-10-22T00:00:00.000Z",
    completedAt: "2026-10-22T00:05:00.000Z"
  };
  assert.equal(validateDeployedDeleteReceipt(base), base);

  assert.throws(
    () => validateDeployedDeleteReceipt({ ...base, deletion: { ...base.deletion, workspaceAbsent: false } }),
    /receipt is invalid/,
    "a deleted deletion must prove absence"
  );
  assert.throws(
    () => validateDeployedDeleteReceipt({ ...base, operation: { ...base.operation, status: "manual_review" } }),
    /receipt is invalid/,
    "a manual_review receipt must not claim absence"
  );
  assert.throws(
    () => validateDeployedDeleteReceipt({ ...base, operation: { ...base.operation, resumed: true } }),
    /receipt is invalid/,
    "resumed must match the observed count"
  );
  assert.throws(
    () => validateDeployedDeleteReceipt({ ...base, extra: 1 }),
    /receipt is invalid/,
    "the receipt is closed to unknown keys"
  );
});
