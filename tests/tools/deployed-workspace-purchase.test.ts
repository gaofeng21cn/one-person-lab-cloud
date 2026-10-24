import assert from "node:assert/strict";
import test from "node:test";

import {
  DEPLOYED_PURCHASE_RECEIPT_KIND,
  assertDeployedPurchaseRequest,
  assertDeployedPurchaseTarget,
  deployedAccountIdentityDigest,
  deployedPurchaseRequestFromEnvironment,
  runDeployedWorkspacePurchase,
  validateDeployedPurchaseReceipt,
} from "../../tools/deployed-workspace-purchase.ts";
import { stableID } from "../../tools/workspace-purchase-core.ts";

// The deployed entry is the only automated buyer that may reach a real customer.
// Its guard is deliberately the mirror image of the local harness's: the local
// harness refuses production authority, and this refuses anything that is not
// production authority or that points at the local stack. A mistake in one is
// caught by the other.
const BASE_URL = "https://cloud.medopl.com";
const CANDIDATE_DIGEST = `sha256:${"b".repeat(64)}`;
const CANDIDATE_HEX = CANDIDATE_DIGEST.slice("sha256:".length);
const ACCOUNT_EMAIL = "customer@example.invalid";
// Pinned to the literal the Control Plane's normalization produces, so a drift
// in the digest derivation fails here rather than as a refused purchase.
const ACCOUNT_DIGEST = "sha256:404c821f1a9a780045f491112906ccb5cf7af27cc81b4ae7e37b4dca35028644";
const TENANT_ID = "tenant-declared";
const BILLING_SUBJECT_ID = "billing-subject-88";
const RECEIPT_ID = "receipt-declared-charge";
const TENANT = {
  id: TENANT_ID,
  name: "Declared Tenant",
  status: "active",
  billingSub2apiUserId: BILLING_SUBJECT_ID,
};
const ACCOUNT = {
  consoleUserId: "user-declared",
  accountId: "acct-declared",
  role: "owner",
  sub2apiUserId: "77",
  email: ACCOUNT_EMAIL,
  status: "active",
};
// The wallet the Gateway owner holds, read under the owner session.
const WALLET = {
  source: "gateway",
  status: "available",
  balanceUSDMicros: "999999999",
  currency: "USD",
  fetchedAt: "2026-10-18T00:00:00Z",
};
// The live owner session. The identity owner binds the actor it authenticated
// to the Gateway user and publishes the Gateway email as the display name.
const OWNER_SESSION = {
  actorId: "77",
  displayName: ACCOUNT_EMAIL,
  tenantId: "tenant-declared",
  permissions: [],
  csrfToken: "owner-csrf",
  expiresAt: "2026-10-18T12:00:00Z",
};
const IDENTITY = "declared-identity";
const IDEMPOTENCY_KEY = `deployed-workspace:${CANDIDATE_HEX}:${IDENTITY}`;
// The identities the Workspace owner and the catalog owners mint. They are
// pinned here in their published shape, so a receipt that names a derived
// identity instead of a minted one fails in this file rather than in a
// customer's receipt.
const RUNTIME_VERSION_ID = `runtime_${"1".repeat(32)}`;
const COMPUTE_PLAN_ID = `compute_${"2".repeat(32)}`;
const STORAGE_PLAN_ID = `storage_${"3".repeat(32)}`;
const QUOTE_ID = `quote_${"4".repeat(32)}`;
// The owner mints the operation and the Workspace identities, and the public
// contract publishes them as strings without a shape. They are pinned here in a
// shape the contract does not describe, so a buyer that pinned a shape of its
// own would fail in this file rather than in a customer's receipt.
const OPERATION_ID = "op-1a2b3c4d-5e6f-7890-abcd-123456789abc";
const WORKSPACE_ID = "workspace-abcdef01-2345-6789-abcd-ef0123456789";
const AMOUNT = "52580000";
// The retained Control Plane launch identity, derived the way the Control
// Plane derived it. The buyer still reads that route for the orders it
// admitted before the cutover.
const RETAINED_OPERATION_ID = `workspace-launch-${stableID(ACCOUNT.accountId, IDEMPOTENCY_KEY).slice(0, 18)}`;

function declaredRequest(overrides = {}) {
  return {
    baseURL: BASE_URL,
    authorityClass: "production",
    candidateReceiptDigest: CANDIDATE_DIGEST,
    accountEmailIdentityDigest: ACCOUNT_DIGEST,
    targetTenantId: TENANT_ID,
    billingSubjectId: BILLING_SUBJECT_ID,
    applicationSelection: { kind: "opl_app", runtimeVersionId: RUNTIME_VERSION_ID },
    computePlanId: COMPUTE_PLAN_ID,
    storagePlanId: STORAGE_PLAN_ID,
    autoRenew: false,
    idempotencyIdentity: IDENTITY,
    workspaceName: "Declared workspace",
    ...overrides,
  };
}

function envelope(source, status, data) {
  const value = { source, status, available: status !== "unavailable", fetchedAt: "2026-10-18T00:00:00Z" };
  if (status === "unavailable") value.reasonCode = `${source}_unavailable`;
  else value.data = data;
  return value;
}

/*
 * The owner's own quote. It names the application source and the plan pair the
 * buyer declared, and it is the only price this purchase may act on.
 */
function ownerQuote(overrides = {}) {
  return {
    id: QUOTE_ID,
    purpose: "deploy",
    runtimeVersionId: RUNTIME_VERSION_ID,
    computePlanId: COMPUTE_PLAN_ID,
    storagePlanId: STORAGE_PLAN_ID,
    modelSelections: [],
    periodMonths: 1,
    totalUSDMicros: AMOUNT,
    status: "offered",
    expiresAt: "2026-10-19T00:00:00Z",
    createdAt: "2026-10-18T00:00:00Z",
    ...overrides,
  };
}

/*
 * The owner's own operation. The owner mints the operation identity and the
 * Workspace identity, so the buyer binds its evidence to what the owner
 * returned and never to an identity it derived.
 */
function ownerOperation(overrides = {}) {
  return {
    operationId: OPERATION_ID,
    owner: "workspace",
    kind: "create_workspace",
    resourceId: WORKSPACE_ID,
    status: "succeeded",
    stage: "succeeded",
    // The owner stores the request that admitted the order on the operation it
    // mints and publishes it, so a replay answers with the request that
    // created the order rather than with the one that asked again.
    requestId: "request-declared",
    createdAt: "2026-10-18T00:00:00Z",
    updatedAt: "2026-10-18T00:00:10Z",
    ...overrides,
  };
}

/*
 * A platform that answers the way the Workspace owner does: the account and
 * the wallet are source envelopes, the catalog is a paged owner read, the quote
 * and the order are the owner's own resources, and the order command is
 * idempotent on the buyer's identity the way the owner's record is.
 *
 * `orders` is the fact a replay must not change: it holds one order per
 * idempotency key, and a repeated command under the same key and the same body
 * replays that order instead of admitting a second one.
 */
function deployedPlatform({
  account = { status: 200, body: envelope("sub2api", "available", ACCOUNT) },
  wallet = { status: 200, body: WALLET },
  tenant = { status: 200, body: TENANT },
  ownerSession = { status: 200, body: OWNER_SESSION },
  retainedLaunch = null,
  runtimeVersions = { items: [{ id: RUNTIME_VERSION_ID, status: "approved" }] },
  computePlans = { items: [{ id: COMPUTE_PLAN_ID, availability: "available" }] },
  storagePlans = { items: [{ id: STORAGE_PLAN_ID, availability: "available" }] },
  quote = ownerQuote(),
  loseCommandAnswer = false,
  failRecoveryRead = false,
  staleQuoteStatus = false,
} = {}) {
  const calls = [];
  const orders = new Map();
  let commands = 0;
  let quoteAccepted = false;
  let currentCatalog = { runtimeVersions, computePlans, storagePlans };
  const respond = (status, payload) => ({ response: { status }, payload, text: "" });

  const request = async (path, init = {}, auth) => {
    calls.push({ kind: "request", path, init, auth });
    if (path === "/api/auth/me") return respond(account.status, account.body);
    if (path === "/api/gateway/wallet") return respond(wallet.status, wallet.body);
    if (path === "/api/v2/auth/session") return respond(ownerSession.status, ownerSession.body);
    if (path === "/api/v2/tenant") return respond(tenant.status, tenant.body);
    if (path === "/api/v2/wallet") return respond(wallet.status, wallet.body);
    if (path === `/api/workspace-launches/${RETAINED_OPERATION_ID}`) {
      return retainedLaunch ? respond(200, retainedLaunch) : respond(404, { error: "workspace_launch_not_found" });
    }
    throw new Error(`unexpected request ${path}`);
  };

  const json = async (path, init = {}, auth, statuses) => {
    calls.push({ kind: "json", path, init, auth, statuses });
    const method = init.method || "GET";
    if (path === "/api/v2/auth/session") return respond(ownerSession.status, ownerSession.body);
    if (path === "/api/v2/tenant") return respond(tenant.status, tenant.body);
    if (path === "/api/v2/wallet") return respond(wallet.status, wallet.body);
    if (path.startsWith("/api/v2/catalog/runtime-versions")) return respond(200, currentCatalog.runtimeVersions);
    if (path.startsWith("/api/v2/catalog/compute-plans")) return respond(200, currentCatalog.computePlans);
    if (path.startsWith("/api/v2/catalog/storage-plans")) return respond(200, currentCatalog.storagePlans);
    if (path === "/api/v2/quotes" && method === "POST") {
      if (!quoteAccepted && (!currentCatalog.runtimeVersions.items.some((v) => v.id === RUNTIME_VERSION_ID && v.status === "approved") ||
        !currentCatalog.computePlans.items.some((v) => v.id === COMPUTE_PLAN_ID && v.availability === "available") ||
        !currentCatalog.storagePlans.items.some((v) => v.id === STORAGE_PLAN_ID && v.availability === "available"))) {
        throw new Error("owner refused unavailable selection");
      }
      return respond(200, quote);
    }
    if (path === `/api/v2/quotes/${QUOTE_ID}`) {
      return respond(200, { ...quote, status: quoteAccepted && !staleQuoteStatus ? "accepted" : "offered" });
    }
    if (path === "/api/v2/workspaces" && method === "POST") {
      commands += 1;
      const key = String(init.headers?.["idempotency-key"] || "");
      const body = JSON.stringify(init.body);
      const existing = orders.get(key);
      if (existing) {
        // The owner's own record: the same identity with the same request
        // replays the order, and a different request under it is a conflict.
        if (existing.body !== body) throw new Error("idempotency key has different input");
        return respond(202, existing.operation);
      }
      const operation = ownerOperation({ requestId: String(init.headers?.["x-opl-request-id"] || "") });
      orders.set(key, { body, operation });
      quoteAccepted = true;
      if (loseCommandAnswer && commands === 1) throw new Error("connection lost after the command");
      return respond(202, operation);
    }
    if (path === `/api/v2/operations/workspace/${OPERATION_ID}`) {
      if (failRecoveryRead) throw new Error("readback lost after the command");
      return respond(200, ownerOperation());
    }
    if (path === `/api/v2/workspaces/${WORKSPACE_ID}/transactions?limit=100`) {
      return respond(200, { items: quoteAccepted ? [{
        id: "wallet-charge-1", workspaceId: WORKSPACE_ID, kind: "charge", status: "confirmed",
        amountUSDMicros: AMOUNT, receiptId: RECEIPT_ID, externalReference: "native-charge-code",
        createdAt: "2026-10-18T00:00:05Z", updatedAt: "2026-10-18T00:00:05Z",
      }] : [] });
    }
    if (path === `/api/v2/workspaces/${WORKSPACE_ID}/financial-receipts/${RECEIPT_ID}`) {
      return respond(200, {
        workspaceId: WORKSPACE_ID,
        receipt: { id: RECEIPT_ID, kind: "wallet_action", owner: "workspace", outcome: "confirmed", operationId: OPERATION_ID,
          evidenceSummary: "confirmed charge", createdAt: "2026-10-18T00:00:05Z" },
        walletOperation: { id: "wallet-charge-1", workspaceId: WORKSPACE_ID, kind: "charge", status: "confirmed",
          amountUSDMicros: AMOUNT, receiptId: RECEIPT_ID, externalReference: "native-charge-code",
          createdAt: "2026-10-18T00:00:05Z", updatedAt: "2026-10-18T00:00:05Z" },
        quote: { id: QUOTE_ID, status: "accepted", totalUSDMicros: AMOUNT, lineItems: [] },
        evidenceDigest: `sha256:${"e".repeat(64)}`,
      });
    }
    throw new Error(`unexpected json ${method} ${path}`);
  };

  return {
    calls,
    request,
    json,
    commands: () => commands,
    orders: () => orders,
    setCatalog: (next) => { currentCatalog = next; },
  };
}

function callerFor(platform, overrides = {}) {
  return {
    origin: BASE_URL,
    http: { request: platform.request, json: platform.json },
    auth: AUTH,
    wait: async () => {},
    ...overrides,
  };
}

const AUTH = { cookie: "session=declared", csrf: "declared-csrf" };

test("the deployed buyer requires production authority", () => {
  assert.doesNotThrow(() => assertDeployedPurchaseTarget({ baseURL: BASE_URL, authorityClass: "production" }));
  for (const authorityClass of ["sandbox", "preproduction", "fixture", "", undefined]) {
    assert.throws(
      () => assertDeployedPurchaseTarget({ baseURL: BASE_URL, authorityClass }),
      /deployed purchase target/i,
      String(authorityClass),
    );
  }
});

test("the deployed buyer refuses the local stack it can never legitimately reach", () => {
  for (const baseURL of [
    "http://127.0.0.1:8080",
    "http://localhost:8080",
    "https://127.0.0.1:8080",
    "https://localhost:8080",
    "https://[::1]:8080",
    "https://[fd00::1]",
    "https://[fe80::1]",
    "https://0.0.0.0",
    "https://172.16.0.1",
    "https://172.31.255.255",
    "https://192.168.1.1",
    "https://169.254.1.1",
    "http://cloud.medopl.com",
    "cloud.medopl.com",
    // The target is an origin, because every route is appended to it.
    "https://cloud.medopl.com/path",
    "https://cloud.medopl.com/?query=1",
    "https://user:password@cloud.medopl.com",
  ]) {
    assert.throws(
      () => assertDeployedPurchaseTarget({ baseURL, authorityClass: "production" }),
      /deployed purchase target/i,
      baseURL,
    );
  }
});

test("every declared input is required and nothing else is accepted", () => {
  assert.doesNotThrow(() => assertDeployedPurchaseRequest(declaredRequest()));
  for (const missing of Object.keys(declaredRequest())) {
    const without = { ...declaredRequest() };
    delete without[missing];
    assert.throws(() => assertDeployedPurchaseRequest(without), /deployed purchase target/i, missing);
  }
  for (const extra of [
    { storageSizeGb: "20" },
    { customerEmail: ACCOUNT_EMAIL },
    { customerPassword: "customer-password" },
    { priceUsdMicros: AMOUNT },
    { testBilling: true },
  ]) {
    assert.throws(
      () => assertDeployedPurchaseRequest({ ...declaredRequest(), ...extra }),
      /deployed purchase target/i,
      JSON.stringify(extra),
    );
  }
  // The Control Plane trims the name it persists, so padding would break the
  // replay readback that has to match the declared name.
  for (const workspaceName of [" Declared workspace", "Declared workspace "]) {
    assert.throws(
      () => assertDeployedPurchaseRequest(declaredRequest({ workspaceName })),
      /deployed purchase target/i,
      JSON.stringify(workspaceName),
    );
  }
});

test("the environment reader declares inputs and never a credential", () => {
  const environment = {
    OPL_DEPLOYED_PURCHASE_BASE_URL: BASE_URL,
    OPL_DEPLOYED_PURCHASE_AUTHORITY_CLASS: "production",
    OPL_DEPLOYED_PURCHASE_CANDIDATE_DIGEST: CANDIDATE_DIGEST,
    OPL_DEPLOYED_PURCHASE_ACCOUNT_DIGEST: ACCOUNT_DIGEST,
    OPL_DEPLOYED_PURCHASE_TENANT_ID: TENANT_ID,
    OPL_DEPLOYED_PURCHASE_BILLING_SUBJECT_ID: BILLING_SUBJECT_ID,
    OPL_DEPLOYED_PURCHASE_RUNTIME_VERSION_ID: RUNTIME_VERSION_ID,
    OPL_DEPLOYED_PURCHASE_COMPUTE_PLAN_ID: COMPUTE_PLAN_ID,
    OPL_DEPLOYED_PURCHASE_STORAGE_PLAN_ID: STORAGE_PLAN_ID,
    OPL_DEPLOYED_PURCHASE_AUTO_RENEW: "false",
    OPL_DEPLOYED_PURCHASE_IDENTITY: IDENTITY,
    OPL_DEPLOYED_PURCHASE_WORKSPACE_NAME: "Declared workspace",
    // This entry never reads a customer credential. A workflow that still has
    // one in its environment must hand that secret to its own session, not here.
    OPL_DEPLOYED_PURCHASE_CUSTOMER_PASSWORD: "customer-password",
  };
  const request = deployedPurchaseRequestFromEnvironment(environment);
  assert.deepEqual(request, declaredRequest());
  assert.equal("customerEmail" in request, false);
  assert.equal("customerPassword" in request, false);
  assert.equal(deployedPurchaseRequestFromEnvironment({
    ...environment, OPL_DEPLOYED_PURCHASE_AUTO_RENEW: "true",
  }).autoRenew, true);
  for (const renewal of ["TRUE", "1", "yes", "", "false "]) {
    assert.throws(
      () => deployedPurchaseRequestFromEnvironment({ ...environment, OPL_DEPLOYED_PURCHASE_AUTO_RENEW: renewal }),
      /deployed purchase target/i,
      JSON.stringify(renewal),
    );
  }
  const withoutCandidate = { ...environment };
  delete withoutCandidate.OPL_DEPLOYED_PURCHASE_CANDIDATE_DIGEST;
  assert.throws(() => deployedPurchaseRequestFromEnvironment(withoutCandidate), /deployed purchase target/i);
});

test("the declared identity digest is the normalization the Control Plane applies", () => {
  assert.equal(deployedAccountIdentityDigest(ACCOUNT_EMAIL), ACCOUNT_DIGEST);
  assert.equal(deployedAccountIdentityDigest("  Customer@Example.Invalid  "), ACCOUNT_DIGEST);
  assert.throws(() => deployedAccountIdentityDigest(""), { code: "deployed_purchase_account_unavailable" });
  assert.throws(() => deployedAccountIdentityDigest("not-an-email"), { code: "deployed_purchase_account_unavailable" });
});

test("the buyer reads the owner envelope and buys one declared Workspace through the owner route", async () => {
  const platform = deployedPlatform();
  const receipt = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));

  assert.equal(receipt.schemaVersion, 1);
  assert.equal(receipt.kind, DEPLOYED_PURCHASE_RECEIPT_KIND);
  assert.deepEqual(receipt.candidate, { receiptDigest: CANDIDATE_DIGEST });
  // Existing v1 artifacts keep their original closed shape.
  assert.deepEqual(receipt.account, { identityDigest: ACCOUNT_DIGEST });
  // The product names what was actually bought: the application source and the
  // plan pair the owner priced, not a Control Plane package.
  assert.deepEqual(receipt.product, {
    resourceType: "workspace",
    applicationSelection: { kind: "opl_app", runtimeVersionId: RUNTIME_VERSION_ID },
    computePlanId: COMPUTE_PLAN_ID,
    storagePlanId: STORAGE_PLAN_ID,
    renewalMode: "manual",
  });
  // The operation and the Workspace identity are the owner's own, minted by the
  // owner, and the receipt binds the evidence to them.
  assert.deepEqual(receipt.operation, {
    idempotencyKey: IDEMPOTENCY_KEY,
    operationId: OPERATION_ID,
    workspaceId: WORKSPACE_ID,
    resumed: false,
  });
  assert.deepEqual(receipt.purchase, { amountUsdMicros: AMOUNT });
  assert.deepEqual(receipt.mutations, { declared: { purchases: 1 }, observed: { purchases: 1 } });
  assert.ok(Date.parse(receipt.completedAt) >= Date.parse(receipt.startedAt));

  assert.deepEqual(
    platform.calls.map((call) => `${call.kind} ${call.path}${call.init?.method ? ` ${call.init.method}` : ""}`),
    [
      "request /api/auth/me",
      "json /api/v2/auth/session",
      "json /api/v2/tenant",
      `request /api/workspace-launches/${RETAINED_OPERATION_ID}`,
      "json /api/v2/quotes POST",
      `json /api/v2/quotes/${QUOTE_ID}`,
      "json /api/v2/workspaces POST",
      `json /api/v2/operations/workspace/${OPERATION_ID}`,
      `json /api/v2/workspaces/${WORKSPACE_ID}/transactions?limit=100`,
      `json /api/v2/workspaces/${WORKSPACE_ID}/financial-receipts/${RECEIPT_ID}`,
    ],
  );

  // The retained Control Plane route is read for the orders it admitted before
  // the cutover. It is never a purchase command.
  assert.equal(
    platform.calls.some((call) => call.path === "/api/workspace-launches" && call.init?.method === "POST"),
    false,
    "the retained route must never be issued as a purchase command",
  );

  const quote = platform.calls.find((call) => call.path === "/api/v2/quotes" && call.init.method === "POST");
  assert.deepEqual(quote.init.body, {
    purpose: "deploy",
    applicationSelection: { kind: "opl_app", runtimeVersionId: RUNTIME_VERSION_ID },
    computePlanId: COMPUTE_PLAN_ID,
    storagePlanId: STORAGE_PLAN_ID,
    modelSelections: [],
    periodMonths: 1,
  });
  assert.equal(quote.init.headers["idempotency-key"], IDEMPOTENCY_KEY);

  const command = platform.calls.find((call) => call.path === "/api/v2/workspaces" && call.init.method === "POST");
  assert.deepEqual(command.init.body, {
    name: "Declared workspace",
    quoteId: QUOTE_ID,
    renewalMode: "manual",
  });
  assert.equal(command.init.headers["idempotency-key"], IDEMPOTENCY_KEY);
  assert.deepEqual(command.auth, AUTH);
  assert.equal(platform.commands(), 1);
  assert.equal(platform.orders().size, 1);

  // The receipt is evidence, not a transcript: no raw account, session,
  // balance or origin is in it.
  const encoded = JSON.stringify(receipt);
  for (const sensitive of [ACCOUNT.accountId, ACCOUNT_EMAIL, AUTH.cookie, WALLET.usdMicros, BASE_URL]) {
    assert.equal(encoded.includes(sensitive), false, sensitive);
  }
});

test("a backward clock step cannot discard a purchase that already happened", async () => {
  const platform = deployedPlatform();
  const times = [
    new Date("2026-10-18T00:00:10.000Z"),
    new Date("2026-10-18T00:00:00.000Z"),
  ];
  let reads = 0;
  const now = () => times[Math.min(reads++, times.length - 1)];
  const receipt = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform, { now }));

  assert.equal(platform.commands(), 1, "the purchase was issued and is not reissued");
  assert.equal(receipt.startedAt, "2026-10-18T00:00:10.000Z");
  assert.equal(receipt.completedAt, receipt.startedAt);
  assert.deepEqual(receipt.mutations.observed, { purchases: 1 });
});

test("an unavailable account readback is refused before the wallet is read or anything is bought", async () => {
  const platform = deployedPlatform({
    account: { status: 500, body: envelope("sub2api", "unavailable") },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    { code: "deployed_purchase_account_unavailable" },
  );
  assert.deepEqual(platform.calls.map((call) => call.path), ["/api/auth/me"]);
  assert.equal(platform.commands(), 0);
});

test("an owner readback from another source is not an account readback", async () => {
  const platform = deployedPlatform({
    account: { status: 200, body: envelope("control-plane", "available", ACCOUNT) },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    { code: "deployed_purchase_account_unavailable" },
  );
  assert.deepEqual(platform.calls.map((call) => call.path), ["/api/auth/me"]);
});

/*
 * The preserved first attempt read `payload.accountId` and `payload.usdMicros`
 * off the source envelope itself. Those routes answer with
 * `{source, status, available, data}`, so that shape can only find `undefined`.
 * The bare bodies below are complete values, not missing ones: if the buyer
 * read them at the top level the account check would pass and the wallet could
 * fund the quote. They are refused only because they are not the envelope the
 * owner contract requires, which is the exact defect this pins.
 */
test("a bare account body is refused, and a wallet that is not the owner's own projection is refused too", async () => {
  const bareAccount = deployedPlatform({ account: { status: 200, body: ACCOUNT } });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(bareAccount)),
    {
      code: "deployed_purchase_account_unavailable",
      message: "deployed purchase could not read the declared customer account",
    },
  );
  assert.deepEqual(bareAccount.calls.map((call) => call.path), ["/api/auth/me"]);
  assert.equal(bareAccount.commands(), 0);

  // A Tenant without an active billing binding is not a payer, even when the
  // signed-in User has a personal wallet. The buyer refuses before pricing.
  const unboundTenant = deployedPlatform({
    tenant: { status: 200, body: { ...TENANT, status: "suspended" } },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(unboundTenant)),
    { code: "deployed_purchase_billing_subject_mismatch" },
  );
  assert.equal(unboundTenant.calls.some((call) => call.path === "/api/v2/quotes"), false);
  assert.equal(unboundTenant.commands(), 0);
});

test("an account readback without an active owner session is refused", async () => {
  for (const data of [
    { ...ACCOUNT, accountId: "" },
    { ...ACCOUNT, role: "member" },
    { ...ACCOUNT, status: "disabled" },
  ]) {
    const platform = deployedPlatform({ account: { status: 200, body: envelope("sub2api", "available", data) } });
    await assert.rejects(
      runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
      { code: "deployed_purchase_account_unavailable" },
      JSON.stringify(data),
    );
    assert.deepEqual(platform.calls.map((call) => call.path), ["/api/auth/me"]);
  }
});

test("a declared account digest that does not match the session is refused before anything is issued", async () => {
  const platform = deployedPlatform({
    account: { status: 200, body: envelope("sub2api", "available", { ...ACCOUNT, email: "other@example.invalid" }) },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    { code: "deployed_purchase_account_mismatch" },
  );
  assert.deepEqual(platform.calls.map((call) => call.path), ["/api/auth/me"]);
  assert.equal(platform.calls.some((call) => call.path === "/api/gateway/wallet"), false);
  assert.equal(platform.commands(), 0);
});

test("an unavailable Tenant billing readback is refused before the order is issued", async () => {
  const platform = deployedPlatform({
    tenant: { status: 502, body: null },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    { code: "deployed_purchase_billing_subject_mismatch" },
  );
  assert.deepEqual(
    platform.calls.map((call) => `${call.kind} ${call.path}`),
    [
      "request /api/auth/me",
      "json /api/v2/auth/session",
      "json /api/v2/tenant",
    ],
  );
  assert.equal(platform.calls.some((call) => call.path === "/api/v2/workspaces"), false);
  assert.equal(platform.commands(), 0);
});

test("the caller must be the declared target and expose the owner readback transport", async () => {
  const platform = deployedPlatform();
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform, { origin: "https://staging.cloud.example" })),
    { code: "deployed_purchase_caller_invalid" },
  );
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), { origin: BASE_URL, http: { json: platform.json }, auth: AUTH }),
    { code: "deployed_purchase_caller_invalid" },
  );
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), {
      origin: BASE_URL, http: { request: platform.request, json: platform.json },
    }),
    { code: "deployed_purchase_caller_invalid" },
  );
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform, { now: "not-a-clock" })),
    { code: "deployed_purchase_caller_invalid" },
  );
  assert.equal(platform.calls.length, 0);
});

test("a replay of the same declared identity resumes its own order and buys nothing again", async () => {
  const platform = deployedPlatform();
  const first = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));
  const callsAfterFirst = platform.calls.length;
  const second = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));

  // The owner mints the operation identity, so the buyer cannot address its
  // own order without the owner. The repeated command is the owner's own
  // replay of the order it already holds, and it admits no second purchase.
  assert.equal(platform.orders().size, 1, "a replay must not admit a second order");
  assert.equal(platform.commands(), 2, "the command is issued again and the owner replays it");
  assert.deepEqual(second.operation, { ...first.operation, resumed: true });
  assert.equal(second.operation.operationId, first.operation.operationId);
  assert.deepEqual(second.mutations, { declared: { purchases: 1 }, observed: { purchases: 0 } });
  assert.equal(second.purchase.amountUsdMicros, first.purchase.amountUsdMicros);
  assert.deepEqual(
    platform.calls.slice(callsAfterFirst).map((call) => `${call.kind} ${call.path}`),
    [
      "request /api/auth/me",
      "json /api/v2/auth/session",
      "json /api/v2/tenant",
      `request /api/workspace-launches/${RETAINED_OPERATION_ID}`,
      "json /api/v2/quotes",
      `json /api/v2/quotes/${QUOTE_ID}`,
      "json /api/v2/workspaces",
      `json /api/v2/operations/workspace/${OPERATION_ID}`,
      `json /api/v2/workspaces/${WORKSPACE_ID}/transactions?limit=100`,
      `json /api/v2/workspaces/${WORKSPACE_ID}/financial-receipts/${RECEIPT_ID}`,
    ],
  );
  // A resumed delivery does not read the personal wallet: the Tenant payer's
  // binding and the Ledger receipt are the evidence, and a personal balance
  // that has since been spent cannot revoke an order that exists.
  assert.equal(platform.calls.slice(callsAfterFirst).some((call) => call.path === "/api/v2/wallet"), false);
  assert.equal(platform.calls.slice(callsAfterFirst).some((call) => call.path.startsWith("/api/v2/catalog/")), false);
});

test("an admitted order replays while its quote remains offered and the catalog retired", async () => {
  const platform = deployedPlatform({ staleQuoteStatus: true });
  const first = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));
  // The order exists and its quote is frozen. The current catalog no longer
  // offers the Runtime or the plans, which must not turn recovery into a new
  // purchase or a refusal.
  platform.setCatalog({
    runtimeVersions: { items: [] },
    computePlans: { items: [] },
    storagePlans: { items: [] },
  });
  const second = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));
  assert.equal(platform.orders().size, 1);
  assert.equal(second.operation.operationId, first.operation.operationId);
  assert.equal(second.operation.resumed, true);
  assert.deepEqual(second.mutations.observed, { purchases: 0 });
});

test("the personal wallet is not the Tenant payer and cannot make the purchase pass or fail", async () => {
  const platform = deployedPlatform({ wallet: { status: 502, body: null } });
  const receipt = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));
  assert.equal(receipt.purchase.amountUsdMicros, AMOUNT);
  assert.equal(platform.calls.some((call) => call.path === `/api/v2/workspaces/${WORKSPACE_ID}/financial-receipts/${RECEIPT_ID}`), true);
  assert.equal(platform.calls.some((call) => call.path === "/api/v2/wallet"), false);
});

test("a lost command answer is recovered as this delivery's own purchase, not reissued", async () => {
  const platform = deployedPlatform({ loseCommandAnswer: true });
  const receipt = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));

  assert.equal(platform.orders().size, 1, "the order is admitted once");
  assert.equal(platform.commands(), 2, "the command is issued again and the owner replays it");
  assert.equal(receipt.operation.resumed, false);
  assert.deepEqual(receipt.mutations.observed, { purchases: 1 });
  assert.equal(receipt.operation.operationId, OPERATION_ID);
  assert.equal(receipt.operation.workspaceId, WORKSPACE_ID);
});

test("a failed recovery readback keeps the original command error as its cause", async () => {
  const platform = deployedPlatform({ loseCommandAnswer: true, failRecoveryRead: true });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    (error) => {
      assert.match(String(error.message || ""), /readback lost after the command/);
      assert.match(String(error.cause?.message || ""), /connection lost after the command/);
      return true;
    },
  );
  assert.equal(platform.orders().size, 1, "the order is admitted once");
});

test("a Tenant whose billing subject differs from the declared payer is refused before the order is issued", async () => {
  const platform = deployedPlatform({
    tenant: { status: 200, body: { ...TENANT, billingSub2apiUserId: "another-payer" } },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    { code: "deployed_purchase_billing_subject_mismatch" },
  );
  assert.equal(platform.commands(), 0);
});

test("a quote that names another plan is refused before the order is issued", async () => {
  const platform = deployedPlatform({ quote: ownerQuote({ computePlanId: `compute_${"9".repeat(32)}` }) });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    /quote is invalid/i,
  );
  assert.equal(platform.commands(), 0);
});

test("a catalog that cannot name an application source or a plan pair is refused", async () => {
  const cases = [
    { runtimeVersions: { items: [{ id: RUNTIME_VERSION_ID, status: "deprecated" }] } },
    { runtimeVersions: { items: [] } },
    { computePlans: { items: [{ id: COMPUTE_PLAN_ID, availability: "unavailable" }] } },
    { computePlans: { items: [] } },
    { storagePlans: { items: [{ id: STORAGE_PLAN_ID, availability: "retired" }] } },
    { storagePlans: { items: [] } },
  ];
  for (const options of cases) {
    const platform = deployedPlatform(options);
    await assert.rejects(
      runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
      { code: "deployed_purchase_quote_unavailable" },
      JSON.stringify(options),
    );
    assert.equal(platform.commands(), 0, "no order may be issued without a declaration");
  }
});

test("an automatic renewal is refused loudly rather than bought as a manual one", async () => {
  const platform = deployedPlatform();
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest({ autoRenew: true }), callerFor(platform)),
    { code: "deployed_purchase_automatic_renewal_unsupported" },
  );
  assert.equal(platform.calls.length, 0, "the refusal costs no read and no money");
  assert.equal(platform.commands(), 0);
});

test("an order the retained route already admitted is refused rather than bought again", async () => {
  const platform = deployedPlatform({
    retainedLaunch: {
      operationId: RETAINED_OPERATION_ID,
      workspaceId: `ws-${"7".repeat(18)}`,
      accountId: ACCOUNT.accountId,
      name: "Declared workspace",
      packageId: "pro",
      autoRenew: false,
      status: "succeeded",
      phase: "succeeded",
      currency: "USD",
      totalChargeUsdMicros: 52580000,
      receiptId: "rcpt_declared_purchase",
    },
  });
  await assert.rejects(
    runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform)),
    { code: "deployed_purchase_retained_order_exists" },
  );
  assert.equal(platform.calls.some((call) => call.path === "/api/v2/workspaces"), false);
  assert.equal(platform.commands(), 0);
});

test("the receipt cannot report a replay that bought and a first delivery that bought nothing", async () => {
  const platform = deployedPlatform();
  const receipt = await runDeployedWorkspacePurchase(declaredRequest(), callerFor(platform));
  assert.throws(
    () => validateDeployedPurchaseReceipt({ ...receipt, operation: { ...receipt.operation, resumed: true } }),
    { code: "deployed_purchase_receipt_invalid" },
  );
  assert.throws(
    () => validateDeployedPurchaseReceipt({
      ...receipt, mutations: { declared: { purchases: 1 }, observed: { purchases: 0 } },
    }),
    { code: "deployed_purchase_receipt_invalid" },
  );
  assert.throws(
    () => validateDeployedPurchaseReceipt({ ...receipt, account: { identityDigest: "not-a-digest" } }),
    { code: "deployed_purchase_receipt_invalid" },
  );
  // The owner mints the operation, the Workspace and the plan identities, and
  // the public contract publishes them as strings without a shape. The receipt
  // therefore refuses an identity that is missing rather than one whose form it
  // cannot know: the binding that makes an identity trustworthy is the one the
  // owner published beside it, and that binding is established when the order
  // is admitted, not by the shape of a string.
  assert.throws(
    () => validateDeployedPurchaseReceipt({
      ...receipt, operation: { ...receipt.operation, operationId: "" },
    }),
    { code: "deployed_purchase_receipt_invalid" },
  );
  assert.throws(
    () => validateDeployedPurchaseReceipt({
      ...receipt, product: { ...receipt.product, computePlanId: "" },
    }),
    { code: "deployed_purchase_receipt_invalid" },
  );
});
