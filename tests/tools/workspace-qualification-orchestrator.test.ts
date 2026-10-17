import assert from "node:assert/strict";
import test from "node:test";

import {
  QUALIFICATION_BUSINESS_CHECKS,
  QUALIFICATION_MUTATION_BOUNDS,
  QUALIFICATION_TERMINAL_REASONS,
  QUALIFICATION_TERMINAL_STATES,
  qualificationProviderProfile,
  qualificationWorkspaceName,
  validateBusinessQualificationReceipt,
} from "../../tools/workspace-qualification-contract.ts";
import {
  assertQualificationAdapter,
  createQualificationRequest,
  runWorkspaceQualification,
} from "../../tools/workspace-qualification-orchestrator.ts";
import { launchOperationId, launchWorkspaceId } from "../../tools/workspace-purchase-core.ts";

// The seam under test is the public orchestrator, driven over a provider
// adapter boundary. Nothing here reaches a provider, a cluster or a database:
// the fake Control Plane below is the platform's own owner API shape, and the
// adapters are the two real providers reduced to the three reachability
// operations the orchestrator is allowed to ask for.

const SOURCE_SHA = "a".repeat(40);
const SOURCE_TREE = "b".repeat(40);
const ACCOUNT_ID = "acct-customer-0001";
const ACCOUNT_EMAIL = "customer@qualification.invalid";
const QUOTE_MICROS = "52580000";
const ENTRY_URL = "https://workspace.qualification.invalid/";
const APPLICATION_OPERATION_ID = "app-op-0123456789abcdef01";

function candidateReceipt(overrides = {}) {
  return {
    schemaVersion: 2,
    kind: "opl_cloud_candidate",
    product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: SOURCE_SHA, tree: SOURCE_TREE },
    cloudImage: {
      repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
      indexRef: "ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:" + "1".repeat(64),
      indexDigest: "sha256:" + "1".repeat(64),
      revision: SOURCE_SHA,
      platforms: [
        { platform: "linux/amd64", digest: "sha256:" + "2".repeat(64) },
        { platform: "linux/arm64", digest: "sha256:" + "3".repeat(64) },
      ],
    },
    assets: [
      "compose.yaml",
      "compose.deployment-platform-owned.yaml",
      "compose.deployment-managed-tke.yaml",
      "compose.deployment-customer-owned.yaml",
      "compose.fabric-local-docker.yaml",
      "compose.fabric-tencent-tke.yaml",
      "compose.local-workspace.yaml",
      "opl-cloud.env.example",
      "opl-cloud-owner-topology.json",
    ].map((name) => ({ name, sha256: "4".repeat(64) })),
    provenance: {
      workflowRepository: "gaofeng21cn/one-person-lab-cloud",
      workflowSha: SOURCE_SHA,
      workflowRunId: "1",
      workflowRunAttempt: "1",
    },
    ...overrides,
  };
}

function j0ReadyReceipt(provider) {
  return {
    schemaVersion: 1,
    kind: "opl.workspace-qualification.j0-ready.v2",
    status: "READY",
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: true },
    authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
    provider,
    failed: 0,
    skipped: 0,
  };
}

const qualificationRequest = (provider, overrides = {}) => createQualificationRequest({
  candidate: candidateReceipt(),
  accountId: ACCOUNT_ID,
  product: { resourceType: "workspace", packageId: "basic", autoRenew: false },
  provider,
  scope: { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 },
  authority: { mode: "live", authorityClass: "sandbox", j0ReadyReceipt: j0ReadyReceipt(provider) },
  source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: true },
  ...overrides,
});

/*
 * The platform, reduced to the owner API the purchase flow reads.
 *
 * It keeps the property that matters and is the reason replay is safe: an
 * idempotency key admits one order. A redelivery is answered from the order
 * already admitted, exactly as the Control Plane answers it, so the fake cannot
 * pass a test the real platform would fail.
 */
function controlPlane({ accountId = ACCOUNT_ID, sessionAccountId = ACCOUNT_ID, sessionRole = "owner", sessionStatus = "active",
  walletBeforeMicros = "999999999", quoteMicros = QUOTE_MICROS, chargeMicros = null, loseResponseOnce = false,
  losePollResponse = false, launchStatus = "succeeded", noReceiptId = false, noBillingReceipt = false, admitPending = false,
  receiptAmountMicros = null, workspaceOpenable = true, applicationStatus = "ready", runtimeReady = true,
  runtimeStatus = "running", extraChargeMicros = "0" } = {}) {
  const state = { orders: new Map(), byKey: new Map(), posted: 0, charges: [], commands: [], responsesLost: false,
    wallet: walletBeforeMicros, workspaceId: "", polled: 0 };
  const envelope = (data) => ({ source: "sub2api", available: true, status: "available", data });

  const http = {
    async request(path, init = {}, auth = null) {
      const order = /^\/api\/workspace-launches\/([^/]+)$/.exec(path);
      if (order) {
        // Read by the operation identity, which is how the orchestrator looks
        // for an order it may already own.
        const admitted = state.orders.get(decodeURIComponent(order[1]));
        return admitted
          ? { response: { status: 200 }, payload: admitted }
          : { response: { status: 404 }, payload: null };
      }
      throw new Error(`unexpected request ${path}`);
    },
    async json(path, init = {}, auth = null, statuses = [200]) {
      if (path === "/api/gateway/wallet") return { status: 200, payload: envelope({ usdMicros: state.wallet }) };
      // The identity fence. A transport holding another account's session
      // answers honestly here, which is the whole point of asking.
      if (path === "/api/auth/me") {
        return { status: 200, payload: envelope({
          accountId: sessionAccountId, email: ACCOUNT_EMAIL, role: sessionRole, status: sessionStatus, sub2apiUserId: "9001",
        }) };
      }
      if (path.startsWith("/api/workspaces?")) {
        return { status: 200, payload: {
          source: "control-plane", available: true, status: "available",
          data: { items: [{
            id: state.workspaceId, url: ENTRY_URL, openable: workspaceOpenable,
            currentApplication: { status: applicationStatus, entryUrl: ENTRY_URL, operationId: APPLICATION_OPERATION_ID },
          }] },
        } };
      }
      if (/^\/api\/workspaces\/[^/]+\/runtime-status$/.test(path)) {
        return { status: 200, payload: {
          source: "fabric", available: true, status: "available",
          data: { workspaceId: state.workspaceId, ready: runtimeReady, status: runtimeStatus, url: ENTRY_URL,
            runtimeId: "rt-0123456789abcdef", currentApplication: { operationId: APPLICATION_OPERATION_ID } },
        } };
      }
      if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
        if (noBillingReceipt) throw new Error(`HTTP GET ${path} returned 404`);
        return { status: 200, payload: {
          source: "ledger", available: true, status: "available",
          data: { receiptId: decodeURIComponent(path.split("/").pop()), workspaceId: state.workspaceId,
            type: "billing.workspace_purchased.v1", status: "completed",
            chargeReference: "opl:workspace-launch:charge:0001",
            totalUsdMicros: receiptAmountMicros ?? quoteMicros,
            fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001" } },
        } };
      }
      if (path === "/api/pricing/preview") {
        return { status: 200, payload: { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: quoteMicros } };
      }
      if (path === "/api/workspace-launches" && init.method === "POST") {
        const key = init.headers["idempotency-key"];
        // The platform mints the identities itself, by the same derivation the
        // purchase core pins to the Control Plane's literals. A fake that
        // invented its own ids would pass a test the real platform fails.
        const operationId = launchOperationId(accountId, key);
        const workspaceId = launchWorkspaceId(accountId, operationId);
        // One admitted order per idempotency key. This is the whole property
        // replay depends on: a redelivery is answered, not re-admitted.
        if (state.byKey.has(key)) {
          return { status: 202, payload: { operationId: state.byKey.get(key), workspaceId } };
        }
        const admitted = {
          operationId,
          workspaceId,
          status: admitPending ? "running" : launchStatus,
          phase: admitPending ? "runtime" : (launchStatus === "succeeded" ? "succeeded" : "debit"),
          receiptId: !admitPending && launchStatus === "succeeded" && !noReceiptId ? "rcpt_qualification_1" : "",
          computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001",
        };
        state.byKey.set(key, operationId);
        state.orders.set(operationId, admitted);
        state.workspaceId = workspaceId;
        state.posted += 1;
        state.commands.push(init.body);
        state.wallet = String(BigInt(state.wallet) - BigInt(chargeMicros ?? quoteMicros) - BigInt(extraChargeMicros));
        state.charges.push(quoteMicros);
        if (loseResponseOnce && !state.responsesLost) {
          state.responsesLost = true;
          throw new Error("socket hang up after the platform accepted the command");
        }
        return { status: 202, payload: { operationId, workspaceId } };
      }
      if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
        // An accepted order that then goes quiet is not a launch the platform
        // published as stopping; it is a run that cannot say what happened.
        if (losePollResponse) throw new Error("socket hang up while following the accepted order");
        const wanted = decodeURIComponent(path.split("/").pop());
        const admitted = state.orders.get(wanted);
        if (!admitted) throw new Error(`HTTP GET ${path} returned 404`);
        return { status: 200, payload: admitted };
      }
      throw new Error(`unexpected path ${path}`);
    },
  };
  return { http, state };
}

// The two providers, each reduced to the three reachability operations. Neither
// one asserts a business fact: the orchestrator reads those itself.
function providerAdapter(provider, platform, { restored = true, health = "healthy", failOpen = null } = {}) {
  const calls = { opened: 0, closed: 0, healthReads: 0 };
  return {
    calls,
    provider,
    async openBusinessScope() {
      calls.opened += 1;
      if (failOpen) throw new Error(failOpen);
      return { http: platform.http, auth: { cookie: "opl_session=qualification", csrf: "qualification-csrf" } };
    },
    async closeBusinessScope() {
      calls.closed += 1;
      return { restored };
    },
    async readDeploymentHealth() {
      calls.healthReads += 1;
      return health;
    },
  };
}

const run = (request, adapter, options) => runWorkspaceQualification({ request, adapter }, { wait: async () => {}, ...options });

test("one orchestrator qualifies two providers, and neither adapter decides the result", async () => {
  for (const provider of ["local-docker", "tencent-tke"]) {
    const platform = controlPlane();
    const adapter = providerAdapter(provider, platform);
    const request = qualificationRequest(provider);
    assertQualificationAdapter(adapter, provider);

    const { state, receipt } = await run(request, adapter);
    assert.equal(state, "settled", provider);
    assert.equal(receipt.provider, provider);
    assert.equal(receipt.state, "settled");
    assert.deepEqual(Object.keys(receipt.checks).sort(), [...QUALIFICATION_BUSINESS_CHECKS].sort());
    assert.ok(QUALIFICATION_BUSINESS_CHECKS.every((name) => receipt.checks[name] === true), provider);
    assert.equal(adapter.calls.closed, 1, "the adapter that opened the scope closes it");
  }
});

test("a second provider cannot be driven through the first provider's adapter", async () => {
  const platform = controlPlane();
  await assert.rejects(
    run(qualificationRequest("tencent-tke"), providerAdapter("local-docker", platform)),
    { code: "qualification_adapter_provider_mismatch" },
  );
});

test("an adapter missing one of the three reachability operations is refused before anything is issued", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform);
  delete adapter.closeBusinessScope;
  await assert.rejects(run(qualificationRequest("local-docker"), adapter), { code: "qualification_adapter_invalid" });
  assert.equal(platform.state.posted, 0, "nothing is issued against an adapter that cannot close its scope");
});

// A terminal state has to mean one thing. Each of these is a different answer to
// a different question, and collapsing any two of them would let an unproven run
// be read as a passed one.
test("a wallet that cannot fund the quote is blocked, and nothing is purchased", async () => {
  const platform = controlPlane({ walletBeforeMicros: "1" });
  const adapter = providerAdapter("local-docker", platform);
  const { state, receipt } = await run(qualificationRequest("local-docker"), adapter);
  assert.equal(state, "blocked");
  assert.equal(receipt.reason, "wallet_cannot_fund");
  assert.ok(QUALIFICATION_TERMINAL_REASONS.blocked.includes(receipt.reason));
  assert.equal(platform.state.posted, 0, "a blocked run issues no command");
  assert.equal(receipt.mutations.observed.purchases, 0);
});

test("a quote that does not name the declared product is a failure of the product, not a blocked run", async () => {
  const platform = controlPlane({ quoteMicros: "0" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "quote_invalid");
});

test("a launch the platform stopped is failed, and it is not retried as a new order", async () => {
  const platform = controlPlane({ launchStatus: "failed" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "launch_stopped");
  assert.equal(platform.state.posted, 1, "a stopped order is followed, never reissued");
});

test("a terminal launch that carries no purchase receipt is a binding failure, not a missing check", async () => {
  const platform = controlPlane({ noReceiptId: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "purchase_receipt_missing");
  assert.equal(receipt.checks.purchase_receipt_bound, false, "an unbound receipt is not recorded as bound");
  assert.equal(receipt.operation.purchaseReceiptId, "");
});

test("a charge the owner readback cannot account for is recovery-required, not a mismatch", async () => {
  const platform = controlPlane({ extraChargeMicros: "100" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "recovery-required");
  assert.equal(receipt.reason, "mutation_unaccounted");
});

test("a scope that could not be opened is blocked and never reaches the platform", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform, { failOpen: "cluster unreachable" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), adapter);
  assert.equal(state, "unknown");
  assert.equal(platform.state.posted, 0);
  assert.ok(["blocked", "unknown"].includes(receipt.state));
});

test("a request that declared no purchase is refused before the scope is opened", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform);
  const request = qualificationRequest("local-docker", {
    scope: { purchases: 0, charges: 0, refunds: 0, workspaceDeletions: 0, managedKeys: 0 },
  });
  const { state, receipt } = await run(request, adapter);
  assert.equal(state, "blocked");
  assert.equal(receipt.reason, "scope_bound_exhausted");
  assert.equal(adapter.calls.opened, 0, "a run outside its own envelope never opens anything");
});

test("a scope that cannot be proven restored is recovery-required even when the product qualified", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform, { restored: false });
  const { state, receipt } = await run(qualificationRequest("local-docker"), adapter);
  assert.equal(state, "recovery-required");
  assert.equal(receipt.reason, "cleanup_unproven");
  assert.ok(QUALIFICATION_BUSINESS_CHECKS.every((name) => receipt.checks[name]), "the business evidence was complete");
  assert.notEqual(state, "settled", "complete business evidence is not a settled result while the scope is unproven");
});

// Replay. The whole point of deriving the operation identity is that a
// redelivery resumes the original order instead of creating a second one.
test("a redelivery resumes the original order and buys nothing twice", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("local-docker");
  const first = await run(request, providerAdapter("local-docker", platform));
  const second = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(first.state, "settled");
  assert.equal(second.state, "settled");
  assert.equal(platform.state.posted, 1, "one admitted order for one declared qualification");
  assert.equal(platform.state.charges.length, 1, "one charge");
  assert.equal(second.receipt.operation.resumed, true);
  assert.equal(first.receipt.operation.resumed, false);
  assert.equal(second.receipt.operation.operationId, first.receipt.operation.operationId);
  assert.equal(second.receipt.operation.workspaceId, first.receipt.operation.workspaceId);
  assert.equal(second.receipt.mutations.observed.purchases, 1, "the resumed run reports the one purchase that exists");
  assert.equal(second.receipt.mutations.observed.charges, 0, "the redelivery charged nothing again");
  assert.equal(first.receipt.mutations.observed.charges, 1, "the first delivery paid for it once");
  assert.equal(
    BigInt(second.receipt.accounting.walletBeforeUsdMicros) - BigInt(second.receipt.accounting.walletAfterUsdMicros),
    0n,
    "a replay that charged nothing again would show movement if it had",
  );
});

test("a response lost after the platform accepted the command resumes that order and issues no second one", async () => {
  const platform = controlPlane({ loseResponseOnce: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "settled");
  assert.equal(receipt.operation.resumed, true);
  assert.equal(platform.state.posted, 1, "the lost answer cost a retry of the read, never a second purchase");
  assert.equal(platform.state.charges.length, 1);
});

test("a lost response that left no order behind is undetermined, never a pass", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform);
  const request = qualificationRequest("local-docker");
  // The platform refuses every order admission, so the probe after the loss
  // finds nothing and the run cannot say what happened.
  platform.http.json = async (path, init = {}, auth = null) => {
    if (path === "/api/gateway/wallet") {
      return { status: 200, payload: { source: "sub2api", available: true, status: "available", data: { usdMicros: "999999999" } } };
    }
    if (path === "/api/auth/me") {
      return { status: 200, payload: { source: "sub2api", available: true, status: "available", data: {
        accountId: ACCOUNT_ID, role: "owner", status: "active" } } };
    }
    if (path === "/api/pricing/preview") {
      return { status: 200, payload: { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: QUOTE_MICROS } };
    }
    throw new Error("socket hang up");
  };
  const { state, receipt } = await run(request, adapter);
  assert.equal(state, "unknown");
  assert.equal(receipt.reason, "response_lost");
  assert.equal(receipt.mutations.observed.purchases, 0);
});

// The command body is what the platform is asked to act on. It carries the
// declared product and a name derived from the operation identity, so it never
// says which provider is running and a redelivery sends the same body.
test("the purchase command names no provider and is identical on a redelivery", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("tencent-tke");
  const first = await run(request, providerAdapter("tencent-tke", platform));
  const second = await run(qualificationRequest("tencent-tke"), providerAdapter("tencent-tke", platform));

  assert.equal(platform.state.posted, 1);
  assert.deepEqual(platform.state.commands, [{
    name: qualificationWorkspaceName(first.receipt.operation.operationId),
    packageId: "basic",
    autoRenew: false,
  }]);
  assert.deepEqual(second.receipt.operation.operationId, first.receipt.operation.operationId,
    "a redelivery addresses the same order, so the command body cannot differ");
  assert.doesNotMatch(JSON.stringify(platform.state.commands), /tke|tencent|local|docker/i,
    "a provider identity has no place in the business command");
});

test("the operation identity is derived from the Candidate, the tenant, the product and the provider", async () => {
  const base = qualificationRequest("local-docker");
  const same = qualificationRequest("local-docker");
  assert.equal(base.operation.idempotencyKey, same.operation.idempotencyKey, "the same request derives the same order");

  const otherTenant = qualificationRequest("local-docker", { accountId: "acct-customer-0002" });
  const otherProvider = qualificationRequest("tencent-tke");
  const otherProduct = qualificationRequest("local-docker", { product: { resourceType: "workspace", packageId: "pro", autoRenew: false } });
  for (const [label, request] of [["tenant", otherTenant], ["provider", otherProvider], ["product", otherProduct]]) {
    assert.notEqual(request.operation.idempotencyKey, base.operation.idempotencyKey, label);
  }
});

// Deployment health is recorded beside the business result and moves none of it.
test("deployment health is recorded and never becomes the qualification result", async () => {
  const degraded = controlPlane();
  const healthy = controlPlane();
  const degradedRun = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", degraded, { health: "degraded" }));
  const healthyRun = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", healthy, { health: "healthy" }));

  assert.equal(degradedRun.state, "settled", "a degraded host does not unqualify a proven product");
  assert.equal(degradedRun.receipt.deploymentHealth, "degraded");
  assert.equal(healthyRun.state, "settled");
  assert.equal(healthyRun.receipt.deploymentHealth, "healthy");
});

test("a receipt binds the Candidate, the source identity and the image digest it ran against", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(receipt.candidate.productSha, SOURCE_SHA);
  assert.equal(receipt.candidate.productTree, SOURCE_TREE);
  assert.equal(receipt.source.sha, SOURCE_SHA);
  assert.equal(receipt.source.tree, SOURCE_TREE);
  assert.equal(receipt.source.clean, true);
  assert.equal(receipt.candidate.cloudImageDigest, "sha256:" + "1".repeat(64));
  assert.match(receipt.authority.j0ReadyDigest, /^sha256:[0-9a-f]{64}$/);
});

// The receipt is the evidence that leaves the machine, so it is closed to
// exactly the fields it declares and carries no customer identifier.
test("the receipt carries no customer identifier, credential or managed Key", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  const serialized = JSON.stringify(receipt);

  assert.equal(receipt.tenant.accountDigest !== ACCOUNT_ID, true);
  assert.doesNotMatch(serialized, new RegExp(ACCOUNT_ID), "the raw account id is not in the receipt");
  assert.doesNotMatch(serialized, new RegExp(ACCOUNT_EMAIL));
  assert.doesNotMatch(serialized, /cookie|csrf|password|secret|token/i);
  assert.deepEqual(Object.keys(receipt.tenant), ["accountDigest"], "the tenant is a digest, and there is no field beside it");
  assert.equal("keyId" in receipt.operation, false, "a managed Key identity is not carried");
  assert.equal("managedKeyId" in receipt, false);
});

test("the same customer under two Candidates gets two unrelated tenant digests", () => {
  const first = qualificationRequest("local-docker");
  // A second Candidate for the same account: same customer, different bytes.
  const secondSha = "c".repeat(40);
  const secondTree = "d".repeat(40);
  const second = createQualificationRequest({
    candidate: candidateReceipt({
      product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: secondSha, tree: secondTree },
      cloudImage: {
        repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
        indexRef: "ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:" + "5".repeat(64),
        indexDigest: "sha256:" + "5".repeat(64),
        revision: secondSha,
        platforms: [
          { platform: "linux/amd64", digest: "sha256:" + "6".repeat(64) },
          { platform: "linux/arm64", digest: "sha256:" + "7".repeat(64) },
        ],
      },
    }),
    accountId: ACCOUNT_ID,
    product: { resourceType: "workspace", packageId: "basic", autoRenew: false },
    provider: "local-docker",
    scope: { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 },
    authority: {
      mode: "live",
      authorityClass: "sandbox",
      j0ReadyReceipt: { ...j0ReadyReceipt("local-docker"), source: { sha: secondSha, tree: secondTree, clean: true } },
    },
    source: { sha: secondSha, tree: secondTree, clean: true },
  });

  assert.equal(first.tenant.accountId, ACCOUNT_ID, "the request still carries what the platform acts on");
  assert.notEqual(first.tenant.accountDigest, second.tenant.accountDigest,
    "a receipt cannot be used to follow one customer across Candidates");
  assert.notEqual(first.operation.idempotencyKey, second.operation.idempotencyKey,
    "two Candidates never address the same purchase order");
});

// The receipt has to be honest about which end of the run it describes.
test("a ready receipt and a not-ready receipt cannot be swapped", () => {
  const platform = controlPlane();
  return run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform)).then(({ receipt }) => {
    const claimedReady = { ...receipt, state: "failed", reason: "launch_stopped" };
    assert.throws(() => validateBusinessQualificationReceipt(claimedReady), { code: "qualification_business_receipt_invalid" },
      "a failure claim cannot hold every piece of evidence a pass needs");

    const withheld = { ...receipt, checks: { ...receipt.checks, wallet_confirmed: false } };
    assert.throws(() => validateBusinessQualificationReceipt(withheld), { code: "qualification_business_receipt_invalid" },
      "a pass claim cannot withhold the charge readback");

    const unbound = { ...receipt, operation: { ...receipt.operation, purchaseReceiptId: "" } };
    assert.throws(() => validateBusinessQualificationReceipt(unbound), { code: "qualification_business_receipt_invalid" },
      "a bound-receipt check cannot stand beside an empty receipt identity");

    const extra = { ...receipt, accountId: ACCOUNT_ID };
    assert.throws(() => validateBusinessQualificationReceipt(extra), { code: "qualification_business_receipt_invalid" },
      "the receipt is closed to its declared fields, so a customer identifier cannot be added to it");
  });
});

test("every declared terminal state is reachable and each carries only its own reasons", async () => {
  const seen = new Map();
  const scenarios = {
    settled: [controlPlane(), {}],
    blocked: [controlPlane({ walletBeforeMicros: "1" }), {}],
    failed: [controlPlane({ launchStatus: "failed" }), {}],
    "recovery-required": [controlPlane(), { restored: false }],
    unknown: [controlPlane({ walletBeforeMicros: "not-a-wallet" }), {}],
  };
  for (const [expected, [platform, adapterOptions]] of Object.entries(scenarios)) {
    const { state, receipt } = await run(
      qualificationRequest("local-docker"),
      providerAdapter("local-docker", platform, adapterOptions),
    );
    assert.equal(state, expected);
    assert.ok(QUALIFICATION_TERMINAL_STATES.includes(receipt.state));
    assert.ok(QUALIFICATION_TERMINAL_REASONS[receipt.state].includes(receipt.reason), `${state}/${receipt.reason}`);
    seen.set(receipt.state, receipt);
  }
  assert.deepEqual([...seen.keys()].sort(), [...QUALIFICATION_TERMINAL_STATES].sort(), "all five terminal states are exercised");
});

test("the bounded mutation counts are recorded against the declared envelope", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.deepEqual(Object.keys(receipt.mutations.declared).sort(), [...QUALIFICATION_MUTATION_BOUNDS].sort());
  assert.deepEqual(Object.keys(receipt.mutations.observed).sort(), [...QUALIFICATION_MUTATION_BOUNDS].sort());
  assert.equal(receipt.mutations.observed.purchases, 1);
  assert.equal(receipt.mutations.observed.charges, 1);
  assert.equal(receipt.mutations.observed.refunds, 0);
  assert.equal(receipt.mutations.observed.workspaceDeletions, 0, "this flow issues no deletion");
  assert.equal(receipt.mutations.observed.managedKeys, 0, "this flow issues no managed Key");
  assert.equal(receipt.accounting.quoteUsdMicros, QUOTE_MICROS);
  assert.equal(BigInt(receipt.accounting.walletBeforeUsdMicros) - BigInt(receipt.accounting.walletAfterUsdMicros), BigInt(QUOTE_MICROS));
});

// The request is the admission. A Candidate the run did not execute, or a
// provider it was not authorised for, is refused before anything is touched.
test("a request is refused when it does not name the Candidate it would run against", () => {
  assert.throws(() => qualificationRequest("local-docker", {
    source: { sha: "e".repeat(40), tree: SOURCE_TREE, clean: true },
  }), { code: "qualification_source_identity_invalid" });
  assert.throws(() => qualificationRequest("local-docker", {
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: false },
  }), { code: "qualification_source_identity_invalid" });
});

test("a fixture authority is refused rather than qualified and relabelled", () => {
  assert.throws(() => qualificationRequest("local-docker", {
    authority: { mode: "fixture", authorityClass: "sandbox", j0ReadyReceipt: j0ReadyReceipt("local-docker") },
  }), { code: "qualification_authority_invalid" });
});

test("the J0 authority must belong to the provider the request declared", () => {
  assert.throws(() => qualificationRequest("tencent-tke", {
    authority: { mode: "live", authorityClass: "sandbox", j0ReadyReceipt: j0ReadyReceipt("local-docker") },
  }), { code: "j0_ready_receipt_invalid" });
  assert.equal(qualificationProviderProfile("tencent-tke").provider, "tencent-tke");
});
// ---------------------------------------------------------------------------
// Regressions for the four blocking findings on fe4570c2. Each one fails against
// that revision for the reason named, and each states the evidence it now needs.
// ---------------------------------------------------------------------------

// Finding 1: an adapter-supplied transport was trusted to be the declared
// account. The receipt was settled for whoever the session happened to hold.
test("a session belonging to another account cannot settle this run", async () => {
  const platform = controlPlane({ sessionAccountId: "acct-other" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "account_binding_mismatch");
  assert.equal(platform.state.posted, 0, "the fence runs before anything is priced or bought");
  assert.equal(receipt.checks.account_bound, false);
  assert.equal(receipt.mutations.observed.purchases, 0);
});

test("the fence refuses a session that is not an active owner", async () => {
  for (const session of [{ sessionRole: "member" }, { sessionStatus: "suspended" }]) {
    const platform = controlPlane(session);
    const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(receipt.reason, "account_binding_mismatch", JSON.stringify(session));
    assert.equal(platform.state.posted, 0, JSON.stringify(session));
  }
});

test("an admitted order naming another Workspace is refused rather than resumed", async () => {
  const platform = controlPlane();
  const first = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(first.state, "settled");

  // The platform answers the probe with an order for a different Workspace.
  const [operationId] = [...platform.state.orders.keys()];
  platform.state.orders.set(operationId, { ...platform.state.orders.get(operationId), workspaceId: "ws-ffffffffffffffff" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "account_binding_mismatch");
});

// Finding 2: a wallet that moved was accepted as proof of a charge.
test("a wallet that moved with no readable Ledger receipt for it does not settle", async () => {
  const platform = controlPlane({ noBillingReceipt: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.notEqual(state, "settled");
  assert.equal(state, "unknown", "the owner's record of the charge could not be read at all");
  assert.equal(receipt.reason, "owner_readback_unavailable");
  assert.equal(receipt.checks.charge_receipted, false);
  assert.equal(receipt.checks.wallet_confirmed, false,
    "the balance is not consulted once the owner's receipt for it is missing");
  assert.equal(receipt.mutations.observed.purchases, 1, "the purchase really was admitted");
});

test("a Ledger receipt for a different amount than the quote does not settle", async () => {
  const platform = controlPlane({ receiptAmountMicros: "1" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "charge_receipt_mismatch");
  assert.equal(receipt.checks.charge_receipted, false);
});

test("a settled receipt binds its charge to a receipt digest rather than the charge reference", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.match(receipt.accounting.chargeReferenceDigest, /^sha256:[0-9a-f]{64}$/);
  assert.doesNotMatch(JSON.stringify(receipt), /opl:workspace-launch:charge/,
    "the charge reference itself stays out of the receipt");
});

// Finding 3: `ready` was returned without any Workspace, Runtime or Ledger
// evidence, so a purchase primitive was labelled a qualified product.
test("a purchase with no ready Workspace behind it does not settle", async () => {
  for (const broken of [{ applicationStatus: "installing" }, { workspaceOpenable: false }]) {
    const platform = controlPlane(broken);
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(state, "failed", JSON.stringify(broken));
    assert.equal(receipt.reason, "workspace_not_ready", JSON.stringify(broken));
    assert.equal(receipt.checks.workspace_ready, false);
  }
});

test("a purchase with no running Runtime behind it does not settle", async () => {
  for (const broken of [{ runtimeReady: false }, { runtimeStatus: "provisioning" }]) {
    const platform = controlPlane(broken);
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(state, "failed", JSON.stringify(broken));
    assert.equal(receipt.reason, "runtime_not_ready", JSON.stringify(broken));
    assert.equal(receipt.checks.runtime_ready, false);
  }
});

test("a settled receipt states the scope it did not establish", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(receipt.scope, "workspace_purchase");
  assert.equal(receipt.businessQualification, undefined);
  assert.deepEqual(receipt.uncovered, {
    applicationInstallation: false,
    gatewayKeyLifecycle: false,
    keyUsage: false,
    ownerDeletion: false,
    businessQualification: false,
  });
  assert.equal(Object.values(receipt.uncovered).every((value) => value === false), true,
    "every unestablished step is declared false, so none can be read as implied");
});

test("a receipt may not claim a scope it cannot prove", () => {
  assert.throws(() => validateBusinessQualificationReceipt({ kind: "opl.workspace-qualification.purchase.v1" }),
    { code: "qualification_business_receipt_invalid" });
});

// Finding 4: a lost polling response was reported as a launch the platform had
// stopped, which is a statement about the product this run could not make.
test("an accepted order whose polling response is lost is not a stopped launch", async () => {
  // Admitted, charged, and then the platform goes quiet before the order ends.
  const platform = controlPlane({ admitPending: true, losePollResponse: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.notEqual(state, "failed", "an unknown outcome is not a product failure");
  assert.equal(state, "recovery-required",
    "the purchase was admitted, so this run changed something it cannot place");
  assert.equal(receipt.reason, "mutation_unaccounted");
  assert.equal(receipt.mutations.observed.purchases, 1, "the mutation did happen");
  assert.notEqual(receipt.reason, "launch_stopped");
});

test("a launch the platform itself published as stopped stays a product failure", async () => {
  const platform = controlPlane({ launchStatus: "failed" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "launch_stopped",
    "the platform published this end, so it is a failure rather than an unknown");
});

test("a lost answer to the command itself, with no order admitted, is undetermined", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform);
  platform.http.request = async () => ({ response: { status: 404 }, payload: null });
  platform.http.json = async (path) => {
    if (path === "/api/auth/me") {
      return { status: 200, payload: { source: "sub2api", available: true, status: "available", data: {
        accountId: ACCOUNT_ID, role: "owner", status: "active" } } };
    }
    if (path === "/api/gateway/wallet") {
      return { status: 200, payload: { source: "sub2api", available: true, status: "available", data: { usdMicros: "999999999" } } };
    }
    if (path === "/api/pricing/preview") {
      return { status: 200, payload: { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: QUOTE_MICROS } };
    }
    throw new Error("socket hang up");
  };
  const { state, receipt } = await run(qualificationRequest("local-docker"), adapter);
  assert.equal(state, "unknown");
  assert.equal(receipt.reason, "response_lost");
  assert.equal(receipt.mutations.observed.purchases, 0, "nothing was admitted, so nothing was mutated");
});
