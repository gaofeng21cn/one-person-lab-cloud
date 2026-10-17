import assert from "node:assert/strict";
import test from "node:test";

import { submitWorkspaceLaunch } from "../../tools/workspace-purchase-core.ts";

// The purchase sequence is the invariant. It runs against whatever origin the
// caller hands it, so these tests pin the sequence itself and never a deployment.
const AUTH = { cookie: "session=qualification", csrfToken: "qualification-csrf" };
const OPERATION_ID = "workspace-launch-0123456789abcdef01";
const WORKSPACE_ID = "ws-0123456789abcdef01";

function purchaseHTTP({ launchBody, pollSequence = [], accepted = [202] } = {}) {
  const calls = [];
  const http = {
    async json(path, init = {}, auth, expectedStatuses) {
      calls.push({ path, init, auth, expectedStatuses });
      if (path === "/api/workspace-launches" && init.method === "POST") {
        return { status: 202, payload: launchBody };
      }
      if (path === `/api/workspace-launches/${OPERATION_ID}`) {
        return { status: 200, payload: pollSequence.shift() ?? { status: "succeeded", phase: "succeeded", receiptId: "rcpt_purchase_core" } };
      }
      throw new Error(`unexpected path ${path}`);
    },
  };
  return { http, calls };
}

const acceptedLaunch = () => ({ operationId: OPERATION_ID, workspaceId: WORKSPACE_ID });

const submit = (overrides = {}) => submitWorkspaceLaunch({
  http: purchaseHTTP({ launchBody: acceptedLaunch() }).http,
  auth: AUTH,
  name: "Purchase core",
  idempotencyKey: "core-key",
  expectedOperationId: OPERATION_ID,
  expectedWorkspaceId: WORKSPACE_ID,
  wait: async () => {},
  ...overrides,
});

test("the command body carries the name, the package and the renewal choice and nothing else", async () => {
  const { http, calls } = purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID } });
  await submitWorkspaceLaunch({
    http, auth: AUTH, name: "Declared workspace", packageId: "pro", autoRenew: true,
    idempotencyKey: "core-key", expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID,
    wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/workspace-launches" && call.init.method === "POST");
  assert.deepEqual(command.init.body, { name: "Declared workspace", packageId: "pro", autoRenew: true });
});

test("the command is keyed by the declared idempotency identity and expects an accepted answer", async () => {
  const { http, calls } = purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID } });
  await submitWorkspaceLaunch({
    http, auth: AUTH, name: "Declared workspace", idempotencyKey: "declared-key",
    expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID, wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/workspace-launches" && call.init.method === "POST");
  assert.equal(command.init.headers["idempotency-key"], "declared-key");
  assert.deepEqual(command.expectedStatuses, [202]);
});

test("the command runs under the session it was handed rather than a credential", async () => {
  const { http, calls } = purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID } });
  await submitWorkspaceLaunch({
    http, auth: AUTH, name: "Declared workspace", idempotencyKey: "core-key",
    expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID, wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/workspace-launches" && call.init.method === "POST");
  assert.deepEqual(command.auth, AUTH);
});

test("an answer that is not the declared operation is refused rather than polled", async () => {
  await assert.rejects(
    submit({ http: purchaseHTTP({ launchBody: { operationId: "workspace-launch-ffffffffffffffff", workspaceId: WORKSPACE_ID } }).http }),
    /deterministic launch identity is invalid/,
  );
  await assert.rejects(
    submit({ http: purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: "ws-ffffffffffffffff" } }).http }),
    /deterministic launch identity is invalid/,
  );
});

test("the declared package and the renewal choice are optional and default to what the catalog publishes", async () => {
  const { http, calls } = purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID } });
  await submitWorkspaceLaunch({
    http, auth: AUTH, name: "Declared workspace", idempotencyKey: "core-key",
    expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID, wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/workspace-launches" && call.init.method === "POST");
  assert.deepEqual(command.init.body, { name: "Declared workspace", packageId: "basic", autoRenew: false });
});

test("a terminal success that carries no receipt identity is refused", async () => {
  await assert.rejects(
    submit({ http: purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID }, pollSequence: [{ status: "succeeded", phase: "succeeded", receiptId: "" }] }).http }),
    /terminal launch receipt identity is missing/,
  );
});

test("the terminal receipt identity is returned so the caller can bind its own evidence to it", async () => {
  const result = await submit();
  assert.equal(result.receiptId, "rcpt_purchase_core");
});

test("a delivery that stops for a reason the platform published stops the purchase", async () => {
  for (const stopped of [
    { status: "failed", phase: "runtime", errorCode: "workspace_runtime_not_ready" },
    { status: "manual_review", phase: "attachment", errorCode: "workspace_launch_manual_review" },
    { status: "refunded", phase: "debit", errorCode: "monthly_balance_insufficient" },
  ]) {
    await assert.rejects(
      submit({ http: purchaseHTTP({ launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID }, pollSequence: [stopped] }).http }),
      /Workspace launch stopped at/,
      JSON.stringify(stopped),
    );
  }
});

test("a delivery still in progress is polled rather than read as an end", async () => {
  let waited = 0;
  const { http, calls } = purchaseHTTP({
    launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID },
    pollSequence: [{ status: "running", phase: "storage" }, { status: "pending", phase: "debit" }],
  });
  const result = await submitWorkspaceLaunch({
    http, auth: AUTH, name: "Declared workspace", idempotencyKey: "core-key",
    expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID,
    wait: async () => { waited += 1; },
  });
  assert.equal(result.receiptId, "rcpt_purchase_core");
  assert.equal(waited, 2, "both in-progress answers are waited on before the terminal one is accepted");
  assert.equal(calls.filter((call) => call.path === `/api/workspace-launches/${OPERATION_ID}`).length, 3);
});

test("the purchase is issued exactly once however long the poll runs", async () => {
  const { http, calls } = purchaseHTTP({
    launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID },
    pollSequence: [{ status: "running", phase: "storage" }, { status: "running", phase: "runtime" }],
  });
  await submitWorkspaceLaunch({
    http, auth: AUTH, name: "Declared workspace", idempotencyKey: "core-key",
    expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID, wait: async () => {},
  });
  assert.equal(calls.filter((call) => call.path === "/api/workspace-launches" && call.init.method === "POST").length, 1);
});

test("an unbounded poll is refused rather than left to run forever", async () => {
  const { http } = purchaseHTTP({
    launchBody: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID },
    pollSequence: Array.from({ length: 200 }, () => ({ status: "running", phase: "storage" })),
  });
  await assert.rejects(
    submitWorkspaceLaunch({
      http, auth: AUTH, name: "Declared workspace", idempotencyKey: "core-key",
      expectedOperationId: OPERATION_ID, expectedWorkspaceId: WORKSPACE_ID, wait: async () => {},
    }),
    /did not reach succeeded within 180 seconds/,
  );
});