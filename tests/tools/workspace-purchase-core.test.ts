import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import {
  launchOperationId, launchWorkspaceId, priceWorkspaceLaunch, submitWorkspaceLaunch,
} from "../../tools/workspace-purchase-core.ts";

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
// The quote policy is part of the invariant, not of any one buyer. The two
// authority arms and any deployed buyer have to price a launch by the same
// rule, or the sequence is shared but its meaning is not.
test("a quote is priced once, by one policy, whoever asks", () => {
  const pricing = { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "52580000" };
  assert.equal(priceWorkspaceLaunch({ pricing, walletUsdMicros: "52580000", packageId: "pro" }), "52580000");
  assert.equal(priceWorkspaceLaunch({ pricing, walletUsdMicros: "999999999", packageId: "pro" }), "52580000");
});

test("a quote that does not name the declared thing is refused", () => {
  const base = { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "52580000" };
  for (const wrong of [
    { ...base, resourceType: "gateway" },
    { ...base, resourceType: "storage" },
    { ...base, packageId: "basic" },
    { ...base, currency: "CNY" },
    { ...base, totalChargeUsdMicros: "0" },
    { ...base, totalChargeUsdMicros: "-100" },
    { ...base, totalChargeUsdMicros: "" },
    { ...base, totalChargeUsdMicros: "52.58" },
  ]) {
    assert.throws(() => priceWorkspaceLaunch({ pricing: wrong, walletUsdMicros: "999999999", packageId: "pro" }), /quote/i, JSON.stringify(wrong));
  }
});

test("a wallet one micro short is short, and a wallet that cannot answer is not a large balance", () => {
  const pricing = { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: "52580000" };
  assert.equal(priceWorkspaceLaunch({ pricing, walletUsdMicros: "52580000" }), "52580000");
  for (const walletUsdMicros of ["52579999", "", "not-a-number", "0"]) {
    assert.throws(() => priceWorkspaceLaunch({ pricing, walletUsdMicros }), /wallet|quote/i, String(walletUsdMicros));
  }
});

// The identifiers are an interop contract with the Control Plane, not an
// internal detail. These literals are the platform's own output for the inputs
// named, derived from the zero-byte-after-every-part rule at
// services/control-plane/internal/server/app_state.go:502 and the two mint
// sites at workspace_launch.go:97 and :51. If either side changes, this fails
// here rather than as a buyer refusing a correct answer.
test("the derived identities are the ones the Control Plane mints for the same inputs", () => {
  const accountId = "acct-test";
  const idempotencyKey = "deployed-workspace:declared-identity";
  const operationId = launchOperationId(accountId, idempotencyKey);
  assert.equal(operationId, "workspace-launch-d8e76ea5e172009490");
  assert.equal(launchWorkspaceId(accountId, operationId), "ws-c3d9e772d4d4708dfa");
});

test("a missing trailing separator is a different identity, so the rule cannot be approximated", () => {
  const naiveJoin = (parts) => createHash("sha1").update(parts.join("\0")).digest("hex").slice(0, 18);
  const accountId = "acct-test";
  const key = "declared-identity";
  assert.notEqual(launchOperationId(accountId, key), `workspace-launch-${naiveJoin([accountId, key])}`);
  const operationId = launchOperationId(accountId, key);
  assert.notEqual(launchWorkspaceId(accountId, operationId), `ws-${naiveJoin(["workspace-launch-v2", accountId, operationId])}`);
});

test("the two identities are both eighteen hex characters, because the platform truncates there", () => {
  for (const accountId of ["acct-a", "acct-test", "acct-" + "x".repeat(40)]) {
    const operationId = launchOperationId(accountId, "k");
    assert.equal(operationId.slice("workspace-launch-".length).length, 18);
    assert.equal(launchWorkspaceId(accountId, operationId).slice("ws-".length).length, 18);
  }
});
