import assert from "node:assert/strict";
import test from "node:test";

import {
  QUALIFICATION_BUSINESS_CHECKS,
  QUALIFICATION_MUTATION_BOUNDS,
  QUALIFICATION_TERMINAL_REASONS,
  QUALIFICATION_TERMINAL_STATES,
  qualificationProviderProfile,
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
function controlPlane({ accountId = ACCOUNT_ID, walletBeforeMicros = "999999999", quoteMicros = QUOTE_MICROS, chargeMicros = null,
  loseResponseOnce = false, launchStatus = "succeeded", extraChargeMicros = "0" } = {}) {
  const state = { orders: new Map(), byKey: new Map(), posted: 0, charges: [], responsesLost: false, wallet: walletBeforeMicros };
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
          status: launchStatus,
          phase: launchStatus === "succeeded" ? "succeeded" : "debit",
          receiptId: launchStatus === "succeeded" ? "rcpt_qualification_1" : "",
        };
        state.byKey.set(key, operationId);
        state.orders.set(operationId, admitted);
        state.posted += 1;
        state.wallet = String(BigInt(state.wallet) - BigInt(chargeMicros ?? quoteMicros) - BigInt(extraChargeMicros));
        state.charges.push(quoteMicros);
        if (loseResponseOnce && !state.responsesLost) {
          state.responsesLost = true;
          throw new Error("socket hang up after the platform accepted the command");
        }
        return { status: 202, payload: { operationId, workspaceId } };
      }
      if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
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
    assert.equal(state, "ready", provider);
    assert.equal(receipt.provider, provider);
    assert.equal(receipt.state, "ready");
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
  assert.equal(receipt.scope.observed.purchases, 0);
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
  assert.notEqual(state, "ready", "complete business evidence is not a ready result while the scope is unproven");
});

// Replay. The whole point of deriving the operation identity is that a
// redelivery resumes the original order instead of creating a second one.
test("a redelivery resumes the original order and buys nothing twice", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("local-docker");
  const first = await run(request, providerAdapter("local-docker", platform));
  const second = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(first.state, "ready");
  assert.equal(second.state, "ready");
  assert.equal(platform.state.posted, 1, "one admitted order for one declared qualification");
  assert.equal(platform.state.charges.length, 1, "one charge");
  assert.equal(second.receipt.operation.resumed, true);
  assert.equal(first.receipt.operation.resumed, false);
  assert.equal(second.receipt.operation.operationId, first.receipt.operation.operationId);
  assert.equal(second.receipt.operation.workspaceId, first.receipt.operation.workspaceId);
  assert.equal(second.receipt.scope.observed.purchases, 1, "the resumed run reports the one purchase that exists");
  assert.equal(second.receipt.scope.observed.charges, 0, "the redelivery charged nothing again");
  assert.equal(first.receipt.scope.observed.charges, 1, "the first delivery paid for it once");
  assert.equal(
    BigInt(second.receipt.accounting.walletBeforeUsdMicros) - BigInt(second.receipt.accounting.walletAfterUsdMicros),
    0n,
    "a replay that charged nothing again would show movement if it had",
  );
});

test("a response lost after the platform accepted the command resumes that order and issues no second one", async () => {
  const platform = controlPlane({ loseResponseOnce: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(state, "ready");
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
    if (path === "/api/pricing/preview") {
      return { status: 200, payload: { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: QUOTE_MICROS } };
    }
    throw new Error("socket hang up");
  };
  const { state, receipt } = await run(request, adapter);
  assert.equal(state, "unknown");
  assert.equal(receipt.reason, "response_lost");
  assert.equal(receipt.scope.observed.purchases, 0);
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

  assert.equal(degradedRun.state, "ready", "a degraded host does not unqualify a proven product");
  assert.equal(degradedRun.receipt.deploymentHealth, "degraded");
  assert.equal(healthyRun.state, "ready");
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

    const withheld = { ...receipt, checks: { ...receipt.checks, charge_settled: false } };
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
    ready: [controlPlane(), {}],
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
  assert.deepEqual(Object.keys(receipt.scope.declared).sort(), [...QUALIFICATION_MUTATION_BOUNDS].sort());
  assert.deepEqual(Object.keys(receipt.scope.observed).sort(), [...QUALIFICATION_MUTATION_BOUNDS].sort());
  assert.equal(receipt.scope.observed.purchases, 1);
  assert.equal(receipt.scope.observed.charges, 1);
  assert.equal(receipt.scope.observed.refunds, 0);
  assert.equal(receipt.scope.observed.workspaceDeletions, 0, "this flow issues no deletion");
  assert.equal(receipt.scope.observed.managedKeys, 0, "this flow issues no managed Key");
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