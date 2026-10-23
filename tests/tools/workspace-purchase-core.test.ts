import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import {
  priceWorkspaceQuote, quoteWorkspaceOrder, stableID, submitWorkspaceOrder, waitForWorkspaceOrder,
  workspaceDeclaration,
} from "../../tools/workspace-purchase-core.ts";

// The purchase sequence is the invariant. It runs against whatever origin the
// caller hands it, so these tests pin the sequence itself and never a deployment.
const AUTH = { cookie: "session=qualification", csrfToken: "qualification-csrf" };
const OPERATION_ID = "op_0123456789abcdef01";
const WORKSPACE_ID = "workspace_0123456789abcdef01";
const QUOTE_ID = "quote_0123456789abcdef01";

const DECLARATION = workspaceDeclaration({
  applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime_release_approved" },
  computePlanId: "compute_basic",
  storagePlanId: "storage_basic",
  periodMonths: 1,
});

const quote = (overrides = {}) => ({
  id: QUOTE_ID,
  purpose: "deploy",
  runtimeVersionId: "runtime_release_approved",
  computePlanId: "compute_basic",
  storagePlanId: "storage_basic",
  periodMonths: 1,
  status: "offered",
  expiresAt: "2026-10-23T00:00:00Z",
  totalUSDMicros: "52580000",
  ...overrides,
});

const acceptedOrder = (overrides = {}) => ({
  operationId: OPERATION_ID,
  resourceId: WORKSPACE_ID,
  owner: "workspace",
  kind: "create_workspace",
  status: "accepted",
  stage: "quote_binding",
  ...overrides,
});

function ownerHTTP({ quoteBody = quote(), orderBody = acceptedOrder(), pollSequence = [] } = {}) {
  const calls = [];
  const http = {
    async json(path, init = {}, auth, expectedStatuses) {
      calls.push({ path, init, auth, expectedStatuses });
      if (path === "/api/v2/quotes" && init.method === "POST") {
        return { status: 200, payload: quoteBody };
      }
      if (path === "/api/v2/workspaces" && init.method === "POST") {
        return { status: 202, payload: orderBody };
      }
      if (path.startsWith("/api/v2/operations/workspace/")) {
        return { status: 200, payload: pollSequence.shift() ?? { operationId: OPERATION_ID, resourceId: WORKSPACE_ID, status: "succeeded", stage: "succeeded" } };
      }
      throw new Error(`unexpected path ${path}`);
    },
  };
  return { http, calls };
}

const submit = (overrides = {}) => submitWorkspaceOrder({
  http: ownerHTTP().http,
  auth: AUTH,
  name: "Purchase core",
  quote: quote(),
  idempotencyKey: "core-key",
  wait: async () => {},
  ...overrides,
});

test("the declaration names exactly one application source beside the plans", () => {
  assert.deepEqual(workspaceDeclaration({
    applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime_release_approved" },
    computePlanId: "compute_basic", storagePlanId: "storage_basic", periodMonths: 1,
  }), {
    applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime_release_approved" },
    computePlanId: "compute_basic", storagePlanId: "storage_basic", modelSelections: [], periodMonths: 1,
  });
  assert.deepEqual(workspaceDeclaration({
    applicationSelection: { kind: "agent", capabilityVersionId: "capability_version_built" },
    computePlanId: "compute_basic", storagePlanId: "storage_basic", periodMonths: 1,
  }).applicationSelection, { kind: "agent", capabilityVersionId: "capability_version_built" });
});

test("a declaration that names no application source, or two, or a plan it does not have is refused", () => {
  for (const declaration of [
    { applicationSelection: { kind: "opl_app" }, computePlanId: "c", storagePlanId: "s" },
    { applicationSelection: { kind: "agent" }, computePlanId: "c", storagePlanId: "s" },
    { applicationSelection: { kind: "resource_only" }, computePlanId: "c", storagePlanId: "s" },
    { applicationSelection: { kind: "opl_app", runtimeVersionId: "r", capabilityVersionId: "cv" }, computePlanId: "c", storagePlanId: "s" },
    { applicationSelection: { kind: "agent", capabilityVersionId: "cv", runtimeVersionId: "r" }, computePlanId: "c", storagePlanId: "s" },
    { applicationSelection: { kind: "opl_app", runtimeVersionId: "r" }, computePlanId: "", storagePlanId: "s" },
    { applicationSelection: { kind: "opl_app", runtimeVersionId: "r" }, computePlanId: "c", storagePlanId: "" },
    { applicationSelection: { kind: "opl_app", runtimeVersionId: "r" }, computePlanId: "c", storagePlanId: "s", periodMonths: 0 },
  ]) {
    assert.throws(() => workspaceDeclaration(declaration), /workspace declaration/i, JSON.stringify(declaration));
  }
});

test("the quote request carries the declaration and nothing else the client may not price", async () => {
  const { http, calls } = ownerHTTP();
  await quoteWorkspaceOrder({ http, auth: AUTH, declaration: DECLARATION, idempotencyKey: "declared-key" });
  const command = calls.find((call) => call.path === "/api/v2/quotes" && call.init.method === "POST");
  assert.deepEqual(command.init.body, {
    purpose: "deploy",
    applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime_release_approved" },
    computePlanId: "compute_basic",
    storagePlanId: "storage_basic",
    modelSelections: [],
    periodMonths: 1,
  });
  assert.equal(command.init.headers["idempotency-key"], "declared-key");
  assert.deepEqual(command.auth, AUTH);
});

test("a quote that comes back without an identity is refused", async () => {
  await assert.rejects(
    quoteWorkspaceOrder({ http: ownerHTTP({ quoteBody: quote({ id: "" }) }).http, auth: AUTH, declaration: DECLARATION, idempotencyKey: "declared-key" }),
    /workspace quote identity is missing/,
  );
});

test("the order body carries the name, the quote it is buying and the renewal mode and nothing else", async () => {
  const { http, calls } = ownerHTTP();
  await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "declared-key", wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/v2/workspaces" && call.init.method === "POST");
  assert.deepEqual(command.init.body, {
    name: "Declared workspace", quoteId: QUOTE_ID, renewalMode: "manual",
  });
});

test("the order is keyed by the declared idempotency identity and expects an accepted answer", async () => {
  const { http, calls } = ownerHTTP();
  await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "declared-key", wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/v2/workspaces" && call.init.method === "POST");
  assert.equal(command.init.headers["idempotency-key"], "declared-key");
  assert.deepEqual(command.expectedStatuses, [202]);
});

test("the order runs under the session it was handed rather than a credential", async () => {
  const { http, calls } = ownerHTTP();
  await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "core-key", wait: async () => {},
  });
  const command = calls.find((call) => call.path === "/api/v2/workspaces" && call.init.method === "POST");
  assert.deepEqual(command.auth, AUTH);
});

test("the operation and Workspace identities come from the owner rather than from the buyer", async () => {
  const { http } = ownerHTTP({
    orderBody: acceptedOrder({ operationId: "op_minted_by_owner", resourceId: "workspace_minted_by_owner" }),
    pollSequence: [{ operationId: "op_minted_by_owner", resourceId: "workspace_minted_by_owner", status: "succeeded", stage: "succeeded" }],
  });
  const result = await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "core-key", wait: async () => {},
  });
  assert.equal(result.operationId, "op_minted_by_owner");
  assert.equal(result.workspaceId, "workspace_minted_by_owner");
});

test("an answer that carries no owner identity is refused rather than polled", async () => {
  await assert.rejects(
    submit({ http: ownerHTTP({ orderBody: acceptedOrder({ operationId: "" }) }).http }),
    /workspace order identity is missing/,
  );
  await assert.rejects(
    submit({ http: ownerHTTP({ orderBody: acceptedOrder({ resourceId: "" }) }).http }),
    /workspace order identity is missing/,
  );
});

test("a delivery that stops for a reason the owner published stops the purchase", async () => {
  for (const stopped of [
    { status: "failed", stage: "runtime", errorCode: "workspace_runtime_not_ready", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
    { status: "needs_attention", stage: "attachment", errorCode: "workspace_closeout_unknown", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
    { status: "cancelled", stage: "debit", errorCode: "monthly_balance_insufficient", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
  ]) {
    await assert.rejects(
      submit({ http: ownerHTTP({ pollSequence: [stopped] }).http }),
      /Workspace order stopped at/,
      JSON.stringify(stopped),
    );
  }
});

test("a status the owner never published is refused rather than read as an end", async () => {
  await assert.rejects(
    submit({ http: ownerHTTP({ pollSequence: [{ status: "whatever", stage: "x", operationId: OPERATION_ID, resourceId: WORKSPACE_ID }] }).http }),
    /unknown status/,
  );
});

test("a delivery that stops still names the order the owner minted", async () => {
  const failure = await submit({ http: ownerHTTP({ pollSequence: [{ status: "failed", stage: "runtime", errorCode: "workspace_runtime_not_ready", operationId: OPERATION_ID, resourceId: WORKSPACE_ID }] }).http })
    .then(() => null, (error) => error);
  assert.ok(failure, "a stopped delivery rejects");
  assert.equal(failure.operationId, OPERATION_ID);
  assert.equal(failure.workspaceId, WORKSPACE_ID);
  assert.equal(failure.quoteId, QUOTE_ID);
  assert.match(failure.message, /Workspace order stopped at failed/);
});

test("a failure keeps the original failure as its cause", async () => {
  const failure = await submit({ http: ownerHTTP({ pollSequence: [{ status: "failed", stage: "runtime", operationId: OPERATION_ID, resourceId: WORKSPACE_ID }] }).http })
    .then(() => null, (error) => error);
  assert.ok(failure?.cause, "the original failure is kept as the cause");
  assert.match(failure.cause.message, /Workspace order stopped at failed/);
});

test("a delivery still in progress is polled rather than read as an end", async () => {
  let waited = 0;
  const { http, calls } = ownerHTTP({
    pollSequence: [
      { status: "accepted", stage: "quote_binding", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
      { status: "running", stage: "storage", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
    ],
  });
  const result = await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "core-key", wait: async () => { waited += 1; },
  });
  assert.equal(result.operationId, OPERATION_ID);
  assert.equal(waited, 2, "both in-progress answers are waited on before the terminal one is accepted");
  assert.equal(calls.filter((call) => call.path === `/api/v2/operations/workspace/${OPERATION_ID}`).length, 3);
});

test("the wait between reads is the interval the owner published", async () => {
  const waited = [];
  const { http } = ownerHTTP({
    pollSequence: [{ status: "running", stage: "storage", pollAfterSeconds: 7, operationId: OPERATION_ID, resourceId: WORKSPACE_ID }],
  });
  await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "core-key", wait: async (milliseconds) => { waited.push(milliseconds); },
  });
  assert.deepEqual(waited, [7000]);
});

test("an interval the owner did not publish falls back to the buyer's own bound", async () => {
  for (const pollAfterSeconds of [0, 301, 1.5, undefined]) {
    const waited = [];
    const { http } = ownerHTTP({
      pollSequence: [{ status: "running", stage: "storage", pollAfterSeconds, operationId: OPERATION_ID, resourceId: WORKSPACE_ID }],
    });
    await submitWorkspaceOrder({
      http, auth: AUTH, name: "Declared workspace", quote: quote(),
      idempotencyKey: "core-key", wait: async (milliseconds) => { waited.push(milliseconds); },
    });
    assert.deepEqual(waited, [1000], String(pollAfterSeconds));
  }
});

test("the order is issued exactly once however long the poll runs", async () => {
  const { http, calls } = ownerHTTP({
    pollSequence: [
      { status: "running", stage: "storage", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
      { status: "running", stage: "runtime", operationId: OPERATION_ID, resourceId: WORKSPACE_ID },
    ],
  });
  await submitWorkspaceOrder({
    http, auth: AUTH, name: "Declared workspace", quote: quote(),
    idempotencyKey: "core-key", wait: async () => {},
  });
  assert.equal(calls.filter((call) => call.path === "/api/v2/workspaces" && call.init.method === "POST").length, 1);
});

test("an unbounded poll is refused rather than left to run forever", async () => {
  const { http } = ownerHTTP({
    pollSequence: Array.from({ length: 200 }, () => ({ status: "running", stage: "storage", operationId: OPERATION_ID, resourceId: WORKSPACE_ID })),
  });
  await assert.rejects(
    submitWorkspaceOrder({
      http, auth: AUTH, name: "Declared workspace", quote: quote(),
      idempotencyKey: "core-key", wait: async () => {},
    }),
    /did not reach succeeded within 180 seconds/,
  );
});

test("the wait is a bounded read of one owner operation", async () => {
  const { http, calls } = ownerHTTP();
  const operation = await waitForWorkspaceOrder(http, OPERATION_ID, WORKSPACE_ID, AUTH, async () => {});
  assert.equal(operation.status, "succeeded");
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, `/api/v2/operations/workspace/${OPERATION_ID}`);
});

// The quote policy is part of the invariant, not of any one buyer. Every buyer
// has to price the same order by the same rule, or the sequence is shared but
// its meaning is not.
test("a quote is priced once, by one policy, whoever asks", () => {
  assert.equal(priceWorkspaceQuote({ quote: quote(), walletUsdMicros: "52580000", declaration: DECLARATION }), "52580000");
  assert.equal(priceWorkspaceQuote({ quote: quote(), walletUsdMicros: "999999999", declaration: DECLARATION }), "52580000");
});

test("a quote that does not name the declared thing is refused", () => {
  for (const wrong of [
    quote({ purpose: "resize" }),
    quote({ runtimeVersionId: "runtime_release_other" }),
    quote({ computePlanId: "compute_other" }),
    quote({ storagePlanId: "storage_other" }),
    quote({ periodMonths: 3 }),
    quote({ totalUSDMicros: "0" }),
    quote({ totalUSDMicros: "-100" }),
    quote({ totalUSDMicros: "" }),
    quote({ totalUSDMicros: "52.58" }),
  ]) {
    assert.throws(
      () => priceWorkspaceQuote({ quote: wrong, walletUsdMicros: "999999999", declaration: DECLARATION }),
      /quote/i,
      JSON.stringify(wrong),
    );
  }
});

test("a quote for another application source is refused when the declaration named one", () => {
  const agentDeclaration = workspaceDeclaration({
    applicationSelection: { kind: "agent", capabilityVersionId: "capability_version_built" },
    computePlanId: "compute_basic", storagePlanId: "storage_basic", periodMonths: 1,
  });
  assert.throws(
    () => priceWorkspaceQuote({ quote: quote(), walletUsdMicros: "999999999", declaration: agentDeclaration }),
    /quote/i,
  );
  assert.equal(priceWorkspaceQuote({
    quote: quote({ runtimeVersionId: undefined, capabilityVersionId: "capability_version_built" }),
    walletUsdMicros: "999999999",
    declaration: agentDeclaration,
  }), "52580000");
});

test("a wallet one micro short is short, and a wallet that cannot answer is not a large balance", () => {
  assert.equal(priceWorkspaceQuote({ quote: quote(), walletUsdMicros: "52580000", declaration: DECLARATION }), "52580000");
  for (const walletUsdMicros of ["52579999", "", "not-a-number", "0"]) {
    assert.throws(
      () => priceWorkspaceQuote({ quote: quote(), walletUsdMicros, declaration: DECLARATION }),
      /wallet|quote/i,
      String(walletUsdMicros),
    );
  }
});

// The retained Control Plane still owns the account, so its identifier rule
// stays an interop contract. The Workspace owner mints its own identities and
// no buyer derives them.
test("the retained account identifier rule is the one the Control Plane mints", () => {
  assert.equal(`acct-${stableID("account", "customer@example.test").slice(0, 18)}`, "acct-3a68404963063acf3d");
});

test("a missing trailing separator is a different identity, so the rule cannot be approximated", () => {
  const naiveJoin = (parts) => createHash("sha1").update(parts.join("\0")).digest("hex").slice(0, 18);
  assert.notEqual(stableID("account", "customer@example.test"), naiveJoin(["account", "customer@example.test"]));
});
