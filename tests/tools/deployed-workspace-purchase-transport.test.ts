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
import { stableID } from "../../tools/workspace-purchase-core.ts";

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

// The identities the Workspace owner and the catalog owners mint. They are
// pinned in their published shape so a receipt that names a derived identity
// instead of a minted one fails here rather than in a customer's receipt.
const RUNTIME_VERSION_ID = `runtime_${"1".repeat(32)}`;
const COMPUTE_PLAN_ID = `compute_${"2".repeat(32)}`;
const STORAGE_PLAN_ID = `storage_${"3".repeat(32)}`;
const QUOTE_ID = `quote_${"4".repeat(32)}`;
const OPERATION_ID = `op_${"5".repeat(32)}`;
const WORKSPACE_ID = `workspace_${"6".repeat(32)}`;

// The buyer's own idempotency identity, and the retained Control Plane launch
// identity it derives for the orders that route admitted before the cutover.
const IDEMPOTENCY_KEY = `deployed-workspace:${CANDIDATE_HEX}:${IDENTITY}`;
const RETAINED_OPERATION_ID = `workspace-launch-${stableID(ACCOUNT_ID, IDEMPOTENCY_KEY).slice(0, 18)}`;

const ENVIRONMENT = {
  OPL_DEPLOYED_PURCHASE_BASE_URL: BASE_URL,
  OPL_DEPLOYED_PURCHASE_AUTHORITY_CLASS: "production",
  OPL_DEPLOYED_PURCHASE_CANDIDATE_DIGEST: CANDIDATE_DIGEST,
  OPL_DEPLOYED_PURCHASE_ACCOUNT_DIGEST: ACCOUNT_DIGEST,
  OPL_DEPLOYED_PURCHASE_PACKAGE_ID: "pro",
  OPL_DEPLOYED_PURCHASE_AUTO_RENEW: "false",
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
 * The owner's own quote and operation. The owner mints both, and it keeps the
 * idempotency record that decides whether a repeated command is a replay of the
 * same order or a second one.
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

function ownerOperation(overrides = {}) {
  return {
    operationId: OPERATION_ID,
    owner: "workspace",
    kind: "create_workspace",
    resourceId: WORKSPACE_ID,
    status: "succeeded",
    stage: "succeeded",
    requestId: "request-declared",
    createdAt: "2026-10-18T00:00:00Z",
    updatedAt: "2026-10-18T00:00:10Z",
    ...overrides,
  };
}

/*
 * A platform that answers the way the Workspace owner does and counts the
 * purchase command it was actually issued. `commands` is the fact the money
 * gate is asserted against: a refused authorization has to leave it at zero,
 * because a gate that refuses after spending would be worse than no gate.
 *
 * `shared` lets two invocations of the transport address the same owner, which
 * is how the owner's own idempotency record is observed: the second invocation
 * replays the first one's quote and order instead of creating new ones.
 */
function fakePlatform({
  amountUsdMicros = AMOUNT,
  walletUsdMicros = "999999999",
  me = account(),
  catalog = {},
  shared = null,
} = {}) {
  const state = shared || {
    commands: 0,
    reads: 0,
    quoteCalls: 0,
    quotes: new Map(),
    orders: new Map(),
    quoteAccepted: false,
  };
  const runtimeVersions = catalog.runtimeVersions || { items: [{ id: RUNTIME_VERSION_ID, status: "approved" }] };
  const computePlans = catalog.computePlans || { items: [{ id: COMPUTE_PLAN_ID, availability: "available" }] };
  const storagePlans = catalog.storagePlans || { items: [{ id: STORAGE_PLAN_ID, availability: "available" }] };
  const ok = (payload) => ({ response: { status: 200, headers: new Headers() }, payload });
  const notFound = () => ({ response: { status: 404, headers: new Headers() }, payload: null });
  const accepted = (payload) => ({ response: { status: 202, headers: new Headers() }, payload });

  const request = async (path) => {
    state.reads += 1;
    if (path === "/api/auth/me") return ok(envelope("sub2api", me));
    if (path === "/api/gateway/wallet") {
      return ok(envelope("sub2api", { userId: "77", currency: "USD", usdMicros: walletUsdMicros, status: "active" }));
    }
    // The retained Control Plane route is read for the orders it admitted
    // before the cutover. It is never a purchase command.
    if (path === `/api/workspace-launches/${RETAINED_OPERATION_ID}`) return notFound();
    return notFound();
  };

  const json = async (path, init = {}, auth = null, statuses = [200]) => {
    const method = init.method || "GET";
    if (path.startsWith("/api/v2/catalog/runtime-versions")) return ok(runtimeVersions);
    if (path.startsWith("/api/v2/catalog/compute-plans")) return ok(computePlans);
    if (path.startsWith("/api/v2/catalog/storage-plans")) return ok(storagePlans);
    if (path === "/api/v2/quotes" && method === "POST") {
      state.quoteCalls += 1;
      const key = String(init.headers?.["idempotency-key"] || "");
      const body = JSON.stringify(init.body);
      const existing = state.quotes.get(key);
      if (existing) {
        if (existing.body !== body) throw new Error("idempotency key has different input");
        return ok(existing.quote);
      }
      const quote = ownerQuote({ totalUSDMicros: amountUsdMicros });
      state.quotes.set(key, { body, quote });
      return ok(quote);
    }
    if (path === `/api/v2/quotes/${QUOTE_ID}`) {
      return ok(ownerQuote({
        totalUSDMicros: amountUsdMicros,
        status: state.quoteAccepted ? "accepted" : "offered",
      }));
    }
    if (path === "/api/v2/workspaces" && method === "POST") {
      state.commands += 1;
      const key = String(init.headers?.["idempotency-key"] || "");
      const body = JSON.stringify(init.body);
      const existing = state.orders.get(key);
      if (existing) {
        if (existing.body !== body) throw new Error("idempotency key has different input");
        return accepted(existing.operation);
      }
      const operation = ownerOperation();
      state.orders.set(key, { body, operation });
      state.quoteAccepted = true;
      return accepted(operation);
    }
    if (path === `/api/v2/operations/workspace/${OPERATION_ID}`) return ok(ownerOperation());
    return notFound();
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
  assert.equal(platform.state.orders.size, 0);
  // The quote names the product the owner will sell: the application source and
  // the plan pair it priced.
  assert.deepEqual(result.quote.product, {
    resourceType: "workspace",
    applicationSelection: { kind: "opl_app", runtimeVersionId: RUNTIME_VERSION_ID },
    computePlanId: COMPUTE_PLAN_ID,
    storagePlanId: STORAGE_PLAN_ID,
    renewalMode: "manual",
  });
});

test("a quote for an account the purchase would refuse never reaches the owner catalog", async () => {
  const platform = fakePlatform({ me: account({ email: "someone-else@example.invalid" }) });
  await assert.rejects(
    () => quoteDeployedPurchaseTransport(platform),
    (error) => error.code === "deployed_purchase_account_mismatch",
  );
  assert.equal(platform.state.quoteCalls, 0, "the identity fence is read before anything is priced");
  assert.equal(platform.state.commands, 0);
});

test("an automatic renewal is refused before a price is asked for", async () => {
  // The owner admits manual renewal only for a first deployment, so there is no
  // price to show for an automatic renewal: the preflight refuses it the way
  // the purchase path does.
  const platform = fakePlatform();
  await assert.rejects(
    () => runDeployedPurchaseTransport(
      QUOTE_INPUT,
      { ...ENVIRONMENT, OPL_DEPLOYED_PURCHASE_AUTO_RENEW: "true" },
      transportDependencies(platform),
    ),
    (error) => error.code === "deployed_purchase_automatic_renewal_unsupported",
  );
  assert.equal(platform.state.quoteCalls, 0);
  assert.equal(platform.state.commands, 0);
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

test("the quote the operator approved is the quote the purchase buys", async () => {
  // Both commands carry the buyer's own stable idempotency identity, so the
  // owner replays the very quote it priced before the operator approved it.
  // The approved amount is therefore the amount the order is admitted at.
  const platform = fakePlatform();
  const seen = await runDeployedPurchaseTransport(QUOTE_INPUT, ENVIRONMENT, transportDependencies(platform));
  const bought = await runDeployedPurchaseTransport(
    { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: seen.quote.amountUsdMicros },
    ENVIRONMENT,
    transportDependencies(platform),
  );
  assert.equal(bought.quote.amountUsdMicros, seen.quote.amountUsdMicros);
  assert.equal(bought.receipt.purchase.amountUsdMicros, seen.quote.amountUsdMicros);
  assert.equal(platform.state.quotes.size, 1, "the owner priced one quote");
  assert.equal(platform.state.orders.size, 1, "the owner admitted one order");
  assert.equal(platform.state.commands, 1);
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

test("the command the entry issues carries the idempotency key the platform replays it under", async () => {
  const platform = fakePlatform();
  const issued = [];
  const inner = platform.http.request;
  const http = {
    request: async (path, init = {}, auth = null) => inner(path, init, auth),
    json: async (path, init = {}, auth = null, statuses = [200]) => {
      if (path === "/api/v2/workspaces" && init.method === "POST") {
        issued.push({ key: init.headers?.["idempotency-key"], body: init.body });
      }
      const result = await platform.http.json(path, init, auth, statuses);
      if (!statuses.includes(result.response.status)) {
        throw new Error(`HTTP ${path} returned ${result.response.status}`);
      }
      return result;
    },
  };
  await runDeployedPurchaseTransport(
    { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: AMOUNT },
    ENVIRONMENT,
    { createHTTP: () => http, login: () => ({ cookie: "session=declared", csrf: "declared-csrf" }) },
  );
  assert.deepEqual(issued, [{
    key: IDEMPOTENCY_KEY,
    body: { name: WORKSPACE_NAME, quoteId: QUOTE_ID, renewalMode: "manual" },
  }]);
});

test("an unaffordable quote is reported with its number instead of thrown away", async () => {
  // The operator's next question is the price itself: top up or buy something
  // cheaper is their decision, and it cannot be made from an error code.
  const platform = fakePlatform({ amountUsdMicros: "900000000", walletUsdMicros: "230538276" });
  const result = await runDeployedPurchaseTransport(QUOTE_INPUT, ENVIRONMENT, transportDependencies(platform));
  assert.equal(result.quote.amountUsdMicros, "900000000");
  assert.equal(result.quote.walletUsdMicros, "230538276");
  assert.equal(result.quote.fundable, false);
  assert.equal(result.spent, false);
  assert.equal(platform.state.commands, 0);
});

test("a quote that does not name the declared purchase is refused rather than reported", async () => {
  // A price about something else is not a smaller decision, it is a different
  // one, so there is no number to show and the caller is told nothing.
  for (const catalog of [
    { computePlans: { items: [{ id: `compute_${"8".repeat(32)}`, availability: "available" }] } },
    { storagePlans: { items: [{ id: `storage_${"8".repeat(32)}`, availability: "available" }] } },
  ]) {
    const platform = fakePlatform({ catalog });
    await assert.rejects(
      () => runDeployedPurchaseTransport(QUOTE_INPUT, ENVIRONMENT, transportDependencies(platform)),
      (error) => error.code === "deployed_purchase_quote_unavailable",
      `${JSON.stringify(catalog)} must be refused`,
    );
    assert.equal(platform.state.commands, 0);
  }
});

test("a catalog that cannot name an application source or a plan pair is refused", async () => {
  for (const catalog of [
    { runtimeVersions: { items: [{ id: `runtime_${"8".repeat(32)}`, status: "deprecated" }] } },
    { runtimeVersions: { items: [] } },
    { computePlans: { items: [{ id: COMPUTE_PLAN_ID, availability: "unavailable" }] } },
    { storagePlans: { items: [] } },
  ]) {
    const platform = fakePlatform({ catalog });
    await assert.rejects(
      () => runDeployedPurchaseTransport(QUOTE_INPUT, ENVIRONMENT, transportDependencies(platform)),
      (error) => error.code === "deployed_purchase_catalog_unavailable",
      `${JSON.stringify(catalog)} must be refused`,
    );
    assert.equal(platform.state.commands, 0);
  }
});

test("a purchase against an unaffordable quote is refused and issues no command", async () => {
  const platform = fakePlatform({ amountUsdMicros: "900000000", walletUsdMicros: "230538276" });
  await assert.rejects(
    () => runDeployedPurchaseTransport(
      { command: "purchase", outputPath: undefined, authorizedAmountUsdMicros: "900000000" },
      ENVIRONMENT,
      transportDependencies(platform),
    ),
    (error) => error.code === "deployed_purchase_wallet_cannot_fund",
  );
  assert.equal(platform.state.commands, 0, "a quote the wallet cannot fund must not be bought");
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
    quote: {
      baseURL: BASE_URL,
      accountId: ACCOUNT_ID,
      amountUsdMicros: AMOUNT,
      walletUsdMicros: "999999999",
      fundable: true,
      product: {
        resourceType: "workspace",
        applicationSelection: { kind: "opl_app", runtimeVersionId: RUNTIME_VERSION_ID },
        computePlanId: COMPUTE_PLAN_ID,
        storagePlanId: STORAGE_PLAN_ID,
        renewalMode: "manual",
      },
    },
    receipt: {
      candidate: { receiptDigest: CANDIDATE_DIGEST },
      operation: { operationId: OPERATION_ID, workspaceId: WORKSPACE_ID, resumed: false },
      purchase: { amountUsdMicros: AMOUNT },
      mutations: { observed: { purchases: 1 } },
    },
  });
  const serialized = JSON.stringify(summary);
  assert.equal(summary.amountUsdMicros, AMOUNT);
  assert.equal(summary.purchases, 1);
  assert.equal(summary.operationId, OPERATION_ID);
  assert.equal(summary.workspaceId, WORKSPACE_ID);
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
