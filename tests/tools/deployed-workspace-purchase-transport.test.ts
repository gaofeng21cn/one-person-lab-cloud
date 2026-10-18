import assert from "node:assert/strict";
import test from "node:test";

import {
  PURCHASE_ACCOUNT_ID,
  PURCHASE_CUSTOMER_EMAIL,
  PURCHASE_CUSTOMER_PASSWORD,
  deployedPurchaseTransportSummary,
  parseDeployedPurchaseTransportArgs,
  quoteDeployedWorkspacePurchase,
  runDeployedPurchaseTransport,
} from "../../tools/deployed-workspace-purchase-transport.ts";
import { launchOperationId, launchWorkspaceId } from "../../tools/workspace-purchase-core.ts";

/*
 * The transport is the only piece that turns a customer's credential into a
 * purchase. Every test here runs against an injected `createHTTP` and an injected
 * `login`, so no test in this file opens a socket: the production wiring is the
 * one thing these tests deliberately do not exercise, because exercising it means
 * buying a Workspace.
 *
 * The assertions concentrate on the two facts this module exists to add. The
 * quote command must spend nothing, and the purchase command must spend only the
 * exact micros amount the operator approved. Everything else is the credential
 * and argument plumbing those two facts travel through.
 */
const BASE_URL = "https://cloud.medopl.com";
const CANDIDATE_DIGEST = `sha256:${"b".repeat(64)}`;
const CANDIDATE_HEX = CANDIDATE_DIGEST.slice("sha256:".length);
// Pinned to the digest the purchase entry derives for this exact address, so a
// drift in the derivation fails here rather than as a refused purchase.
const ACCOUNT_EMAIL = "customer@example.invalid";
const ACCOUNT_DIGEST = "sha256:404c821f1a9a780045f491112906ccb5cf7af27cc81b4ae7e37b4dca35028644";
const AMOUNT = "52580000";
const ACCOUNT_ID = "acct-declared";
const IDENTITY = "declared-identity";
const WORKSPACE_NAME = "Declared workspace";
const PACKAGE_ID = "pro";

// The identities the platform derives from the account that owns the session and
// the declared idempotency key. They are computed here with the shared core's own
// derivation so the fake platform is indistinguishable from the Control Plane,
// which is what lets the entry believe it and reach the command it issues.
const IDEMPOTENCY_KEY = `deployed-workspace:${CANDIDATE_HEX}:${IDENTITY}`;
const OPERATION_ID = launchOperationId(ACCOUNT_ID, IDEMPOTENCY_KEY);
const WORKSPACE_ID = launchWorkspaceId(ACCOUNT_ID, OPERATION_ID);

const ENVIRONMENT = {
  OPL_DEPLOYED_PURCHASE_BASE_URL: BASE_URL,
  OPL_DEPLOYED_PURCHASE_AUTHORITY_CLASS: "production",
  OPL_DEPLOYED_PURCHASE_CANDIDATE_DIGEST: CANDIDATE_DIGEST,
  OPL_DEPLOYED_PURCHASE_ACCOUNT_DIGEST: ACCOUNT_DIGEST,
  OPL_DEPLOYED_PURCHASE_PACKAGE_ID: PACKAGE_ID,
  OPL_DEPLOYED_PURCHASE_AUTO_RENEW: "true",
  OPL_DEPLOYED_PURCHASE_IDENTITY: IDENTITY,
  OPL_DEPLOYED_PURCHASE_WORKSPACE_NAME: WORKSPACE_NAME,
  [PURCHASE_CUSTOMER_EMAIL]: ACCOUNT_EMAIL,
  [PURCHASE_CUSTOMER_PASSWORD]: "not-a-real-password",
  [PURCHASE_ACCOUNT_ID]: ACCOUNT_ID,
};

const QUOTE_INPUT = { command: "quote", outputPath: undefined, authorizedAmountUsdMicros: undefined };

function envelope(source, data) {
  return { source, status: "available", available: true, fetchedAt: "2026-10-18T00:00:00Z", data };
}

function account(overrides = {}) {
  return {
    consoleUserId: "user-declared",
    accountId: ACCOUNT_ID,
    role: "owner",
    sub2apiUserId: "77",
    email: ACCOUNT_EMAIL,
    status: "active",
    ...overrides,
  };
}

/*
 * A platform that answers the way the Control Plane does and counts the purchase
 * command it was actually issued. `commands` is the fact the money gate is
 * asserted against: a refused authorization has to leave it at zero, because a
 * gate that refuses after spending would be worse than no gate.
 *
 * The order is absent until it is created, exactly as it is in production, so the
 * entry takes its first-delivery path. `quotes` lets a test make the platform
 * answer a different price on the second call, which is how a price that moved
 * between the quote and the purchase is modelled.
 */
function fakePlatform({
  amountUsdMicros = AMOUNT,
  walletUsdMicros = "999999999",
  me = account(),
  launchStatus = "succeeded",
} = {}) {
  const state = { commands: 0, reads: 0, quoteCalls: 0, order: null };
  const price = () => {
    state.quoteCalls += 1;
    return amountUsdMicros;
  };
  const notFound = () => ({ response: { status: 404, headers: new Headers() }, payload: null });
  const ok = (payload) => ({ response: { status: 200, headers: new Headers() }, payload });

  const request = async (path, init = {}) => {
    state.reads += 1;
    if (path === "/api/auth/me") return ok(envelope("sub2api", me));
    if (path === "/api/gateway/wallet") {
      return ok(envelope("sub2api", { userId: "77", currency: "USD", usdMicros: walletUsdMicros, status: "active" }));
    }
    if (path === "/api/pricing/preview") {
      return ok({ resourceType: "workspace", packageId: PACKAGE_ID, currency: "USD", totalChargeUsdMicros: price() });
    }
    if (path === "/api/workspace-launches" && init.method === "POST") {
      state.commands += 1;
      state.order = {
        operationId: OPERATION_ID,
        workspaceId: WORKSPACE_ID,
        accountId: ACCOUNT_ID,
        name: WORKSPACE_NAME,
        packageId: PACKAGE_ID,
        autoRenew: true,
        status: launchStatus,
        phase: launchStatus === "succeeded" ? "succeeded" : "key",
        currency: "USD",
        totalChargeUsdMicros: amountUsdMicros,
        receiptId: launchStatus === "succeeded" ? "rcpt_declared_purchase" : "",
      };
      return { response: { status: 202, headers: new Headers() }, payload: state.order };
    }
    if (path === `/api/workspace-launches/${encodeURIComponent(OPERATION_ID)}`) {
      return state.order ? ok(state.order) : notFound();
    }
    return notFound();
  };
  const json = async (path, init = {}, auth = null, statuses = [200]) => {
    const result = await request(path, init, auth);
    if (!statuses.includes(result.response.status)) {
      throw new Error(`HTTP ${init.method || "GET"} ${path} returned ${result.response.status}`);
    }
    return result;
  };
  return { http: { request, json }, state };
}

const transportDependencies = (platform) => ({
  createHTTP: () => platform.http,
  login: () => ({ cookie: "session=declared", csrf: "declared-csrf" }),
});

test("the quote command spends nothing and reports the exact amount", async () => {
  const platform = fakePlatform();
  const result = await runDeployedPurchaseTransport(QUOTE_INPUT, ENVIRONMENT, transportDependencies(platform));
  assert.equal(result.spent, false);
  assert.equal(result.quote.amountUsdMicros, AMOUNT);
  assert.equal(result.quote.accountId, ACCOUNT_ID);
  assert.equal(platform.state.commands, 0, "the quote must not issue a purchase command");
});

test("a quote for an account the purchase would refuse never reaches the pricing route", async () => {
  const platform = fakePlatform({ me: account({ email: "someone-else@example.invalid" }) });
  await assert.rejects(
    () => quoteDeployedPurchaseTransport(platform),
    (error) => error.code === "deployed_purchase_account_mismatch",
  );
  assert.equal(platform.state.quoteCalls, 0, "the identity fence is read before anything is priced");
});

test("a purchase whose authorized amount is not the quoted amount spends nothing", async () => {
  const platform = fakePlatform();
  await assert.rejects(
    () => runDeployedPurchaseTransport(
      { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: "1" },
      ENVIRONMENT,
      transportDependencies(platform),
    ),
    (error) => error.code === "deployed_purchase_amount_not_authorized",
  );
  assert.equal(platform.state.commands, 0, "a refused authorization must not issue a purchase command");
});

test("a price that moved between the quote and the purchase is refused, not silently re-spent", async () => {
  // The two commands are two invocations, which is how an operator meets them:
  // one run shows the number, the next run authorizes it. Modelling the move as
  // two platforms in one process is what makes this an actual scenario rather
  // than a counter that happens to tick.
  const quoted = fakePlatform({ amountUsdMicros: AMOUNT });
  const seen = await runDeployedPurchaseTransport(QUOTE_INPUT, ENVIRONMENT, transportDependencies(quoted));
  assert.equal(seen.quote.amountUsdMicros, AMOUNT);

  const moved = fakePlatform({ amountUsdMicros: "52580001" });
  await assert.rejects(
    () => runDeployedPurchaseTransport(
      { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: seen.quote.amountUsdMicros },
      ENVIRONMENT,
      transportDependencies(moved),
    ),
    (error) => error.code === "deployed_purchase_amount_not_authorized",
  );
  assert.equal(moved.state.commands, 0, "a price the operator did not approve must not be bought");
});

test("an approved amount that the platform still quotes buys exactly one command", async () => {
  const platform = fakePlatform();
  const result = await runDeployedPurchaseTransport(
    { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: AMOUNT },
    ENVIRONMENT,
    transportDependencies(platform),
  );
  assert.equal(result.spent, true);
  assert.equal(result.quote.amountUsdMicros, AMOUNT);
  assert.equal(platform.state.commands, 1, "one approved amount is one purchase command");
  assert.equal(result.receipt.operation.operationId, OPERATION_ID);
  assert.equal(result.receipt.operation.workspaceId, WORKSPACE_ID);
  assert.equal(result.receipt.purchase.amountUsdMicros, AMOUNT);
  assert.equal(result.receipt.mutations.observed.purchases, 1);
});

test("the command the entry issues carries the idempotency key the platform derives its identity from", async () => {
  const platform = fakePlatform();
  const issued = [];
  const inner = platform.http.request;
  const http = {
    request: async (path, init = {}, auth = null) => {
      if (path === "/api/workspace-launches" && init.method === "POST") issued.push(init.headers?.["idempotency-key"]);
      return inner(path, init, auth);
    },
    json: async (path, init = {}, auth = null, statuses = [200]) => {
      const result = await http.request(path, init, auth);
      if (!statuses.includes(result.response.status)) throw new Error(`HTTP ${path} returned ${result.response.status}`);
      return result;
    },
  };
  await runDeployedPurchaseTransport(
    { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: AMOUNT },
    ENVIRONMENT,
    { createHTTP: () => http, login: () => ({ cookie: "session=declared", csrf: "declared-csrf" }) },
  );
  assert.deepEqual(issued, [IDEMPOTENCY_KEY]);
});

test("the purchase refuses a credential that is absent rather than defaulting one", async () => {
  const platform = fakePlatform();
  for (const missing of [PURCHASE_CUSTOMER_EMAIL, PURCHASE_CUSTOMER_PASSWORD, PURCHASE_ACCOUNT_ID]) {
    await assert.rejects(
      () => runDeployedPurchaseTransport(QUOTE_INPUT, { ...ENVIRONMENT, [missing]: "" }, transportDependencies(platform)),
      (error) => error.code === "deployed_purchase_transport_invalid",
      `${missing} must be required`,
    );
  }
});

test("a caller bound to a target that is not production authority is refused", async () => {
  const platform = fakePlatform();
  await assert.rejects(
    () => runDeployedPurchaseTransport(
      QUOTE_INPUT,
      { ...ENVIRONMENT, OPL_DEPLOYED_PURCHASE_BASE_URL: "http://127.0.0.1:8080" },
      transportDependencies(platform),
    ),
    (error) => error.code === "deployed_purchase_target_invalid",
  );
});

test("the summary carries the amount and the identities but no credential", () => {
  const summary = deployedPurchaseTransportSummary({
    command: "purchase",
    spent: true,
    quote: { baseURL: BASE_URL, accountId: ACCOUNT_ID, packageId: PACKAGE_ID, amountUsdMicros: AMOUNT, walletUsdMicros: "999999999" },
    receipt: {
      candidate: { receiptDigest: CANDIDATE_DIGEST },
      operation: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID, purchaseReceiptId: "rcpt_declared_purchase", resumed: false },
      purchase: { amountUsdMicros: AMOUNT },
      mutations: { observed: { purchases: 1 } },
    },
  });
  const serialized = JSON.stringify(summary);
  assert.equal(summary.amountUsdMicros, AMOUNT);
  assert.equal(summary.purchases, 1);
  assert.ok(!serialized.includes("not-a-real-password"), "the summary must not carry the password");
  assert.ok(!serialized.includes("session="), "the summary must not carry the session");
  assert.ok(!serialized.includes(ACCOUNT_EMAIL), "the summary must not carry the customer address");
});

test("quote takes no authorization and purchase cannot be run without one", () => {
  assert.throws(
    () => parseDeployedPurchaseTransportArgs(["quote", "--authorize-amount-usd-micros", AMOUNT]),
    (error) => error.code === "deployed_purchase_transport_invalid",
  );
  assert.throws(
    () => parseDeployedPurchaseTransportArgs(["purchase"]),
    (error) => error.code === "deployed_purchase_transport_invalid",
  );
  assert.throws(
    () => parseDeployedPurchaseTransportArgs(["buy"]),
    (error) => error.code === "deployed_purchase_transport_invalid",
  );
  assert.equal(parseDeployedPurchaseTransportArgs(["purchase", "--authorize-amount-usd-micros", AMOUNT]).command, "purchase");
});

async function quoteDeployedPurchaseTransport(platform) {
  const { deployedPurchaseCaller } = await import("../../tools/deployed-workspace-purchase-transport.ts");
  const { deployedPurchaseRequestFromEnvironment } = await import("../../tools/deployed-workspace-purchase.ts");
  const request = deployedPurchaseRequestFromEnvironment(ENVIRONMENT);
  const caller = await deployedPurchaseCaller(request, ENVIRONMENT, transportDependencies(platform));
  return quoteDeployedWorkspacePurchase(caller, request);
}