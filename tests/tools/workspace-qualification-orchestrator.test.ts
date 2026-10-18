import assert from "node:assert/strict";
import test from "node:test";

import {
  LEGACY_QUALIFICATION_J0_READY_KINDS,
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
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, cloudImageDigest: "sha256:" + "1".repeat(64), clean: true },
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
  runtimeStatus = "running", runtimeComponents = [{ name: "console", ok: true }, { name: "gateway", ok: true }],
  extraChargeMicros = "0", receipt: receiptOverrides = {}, runtime: runtimeOverrides = {},
  // The committed default application installation progresses asynchronously
  // beside the resource Launch. `applicationReadyAfter` is how many owner reads
  // it takes; `applicationStalled` models the review that only the customer's
  // own continuation command can move; a status that never becomes ready models
  // an installation that never finishes.
  applicationReadyAfter = null, applicationStalled = false, applicationManualReview = false,
  workspacePage = 1, resumeProjection = null,
  launchProvisioningMode = "resource_only" } = {}) {
  const state = { orders: new Map(), byKey: new Map(), posted: 0, charges: [], commands: [], responsesLost: false,
    wallet: walletBeforeMicros, workspaceId: "", operationId: "", polled: 0,
    workspaceReads: 0, workspacePages: [], resumes: 0, resumedAtRead: -1, resumeKeys: [] };
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
        state.workspaceReads += 1;
        state.workspacePages.push(path);
        const page = Number(new URLSearchParams(path.split("?")[1] || "").get("page") || "1");
        if (page < workspacePage) {
          // A full page of other Workspaces, so the run has to keep looking
          // instead of trusting that the first page is the whole account.
          return { status: 200, payload: {
            source: "control-plane", available: true, status: "available",
            data: {
              items: Array.from({ length: 20 }, (_, index) => ({ id: `ws-filler-${index}`, url: "", openable: false })),
              total: workspacePage * 20, page, pageSize: 20,
            },
          } };
        }
        const readyAfter = applicationReadyAfter !== null
          ? applicationReadyAfter
          : (applicationStatus === "ready" ? 0 : Infinity);
        const ready = !applicationManualReview && (
          applicationStalled
            ? state.resumes > 0 && state.workspaceReads > state.resumedAtRead + readyAfter
            : state.workspaceReads > readyAfter
        );
        const installationStatus = ready ? "ready"
          : (applicationManualReview || applicationStalled && state.resumes === 0 ? "manual_review" : "installing");
        return { status: 200, payload: {
          source: "control-plane", available: true, status: "available",
          data: {
            items: [{
              id: state.workspaceId, url: ready ? ENTRY_URL : "", openable: workspaceOpenable,
              currentApplication: ready ? { status: "ready", entryUrl: ENTRY_URL, operationId: APPLICATION_OPERATION_ID } : null,
              applicationInstallation: {
                status: installationStatus, operationId: "app-install-0123456789abcdef",
                applicationId: "app-0123456789abcdef", revision: "revision-1",
                canResume: applicationStalled && state.resumes === 0,
              },
            }],
            total: 1, page, pageSize: 20,
          },
        } };
      }
      if (/^\/api\/workspaces\/[^/]+\/application-installation\/resume$/.test(path) && init.method === "POST") {
        state.resumes += 1;
        state.resumedAtRead = state.workspaceReads;
        state.resumeKeys.push(init.headers["idempotency-key"]);
        if (resumeProjection) {
          return { status: 202, payload: typeof resumeProjection === "function" ? resumeProjection(state) : resumeProjection };
        }
        return { status: 202, payload: {
          workspaceId: state.workspaceId,
          applicationInstallation: {
            status: "installing", operationId: "app-install-0123456789abcdef",
            applicationId: "app-0123456789abcdef", revision: "revision-1", canResume: false,
          },
        } };
      }
      if (/^\/api\/workspaces\/[^/]+\/runtime-status$/.test(path)) {
        return { status: 200, payload: {
          source: "fabric", available: true, status: "available",
          data: { workspaceId: state.workspaceId, ready: runtimeReady, status: runtimeStatus, url: ENTRY_URL,
            runtimeId: "rt-0123456789abcdef", checks: runtimeComponents,
            currentApplication: { operationId: APPLICATION_OPERATION_ID }, ...runtimeOverrides },
        } };
      }
      if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
        if (noBillingReceipt) throw new Error(`HTTP GET ${path} returned 404`);
        return { status: 200, payload: {
          source: "ledger", available: true, status: "available",
          data: { receiptId: decodeURIComponent(path.split("/").pop()),
            // The billing projection answers with the receipt's requestId, and
            // the Control Plane records the purchase receipt under the launch
            // operation identity. The fake names it the same way rather than
            // inventing an identity of its own.
            operationId: state.operationId,
            workspaceId: state.workspaceId,
            type: "billing.workspace_purchased.v1", status: "completed",
            chargeReference: "opl:workspace-launch:charge:0001",
            totalUsdMicros: receiptAmountMicros ?? quoteMicros,
            fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001" },
            ...receiptOverrides },
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
          // The persisted Launch is the resource purchase; the default
          // application is a separate committed operation. The readback says so.
          provisioningMode: launchProvisioningMode,
          status: admitPending ? "running" : launchStatus,
          phase: admitPending ? "runtime" : (launchStatus === "succeeded" ? "succeeded" : "debit"),
          receiptId: !admitPending && launchStatus === "succeeded" && !noReceiptId ? "rcpt_qualification_1" : "",
          computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001",
        };
        state.byKey.set(key, operationId);
        state.orders.set(operationId, admitted);
        state.workspaceId = workspaceId;
        state.operationId = operationId;
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
  assert.equal(state, "blocked", "a scope that cannot be opened is a blocked run, not a failed owner readback");
  assert.equal(receipt.reason, "provider_scope_unavailable");
  assert.equal(platform.state.posted, 0);
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
  assert.equal(second.receipt.mutations.observed.purchases, 0, "the redelivery bought nothing again");
  assert.equal(second.receipt.mutations.observed.charges, 0, "and charged nothing again");
  assert.equal(first.receipt.mutations.observed.purchases, 1, "the first delivery bought it once");
  assert.equal(first.receipt.mutations.observed.charges, 1, "the first delivery paid for it once");
  assert.equal(
    BigInt(second.receipt.accounting.walletBeforeUsdMicros) - BigInt(second.receipt.accounting.walletAfterUsdMicros),
    0n,
    "a replay that charged nothing again would show movement if it had",
  );
});

test("a response lost after the platform accepted the command recovers that order and issues no second one", async () => {
  const platform = controlPlane({ loseResponseOnce: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "settled");
  assert.equal(platform.state.posted, 1, "the lost answer cost a retry of the read, never a second purchase");
  assert.equal(platform.state.charges.length, 1);
  assert.equal(receipt.operation.resumed, false,
    "this delivery issued the command, so recovering its order is not a resume and cannot skip the money");
  assert.equal(receipt.mutations.observed.purchases, 1);
  assert.equal(receipt.mutations.observed.charges, 1);
});

test("a recovered order whose charge has not settled yet is not a settled purchase", async () => {
  // The order was admitted and its Gateway debit has not landed when the
  // balance is read. This delivery issued the command, so it cannot report the
  // shape of a redelivery that bought nothing.
  const platform = controlPlane({ loseResponseOnce: true, chargeMicros: "0" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.notEqual(state, "settled");
  assert.equal(state, "failed");
  assert.equal(receipt.reason, "wallet_readback_mismatch");
  assert.equal(receipt.mutations.observed.purchases, 1, "the purchase was still admitted");
  assert.equal(receipt.checks.wallet_confirmed, false);
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
    // The platform installs the default application beside the resource Launch
    // only for a new customer Launch, and `resource_only` is the retained
    // historical contract that must not be sent for one.
    provisioningMode: "full",
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
      j0ReadyReceipt: {
        ...j0ReadyReceipt("local-docker"),
        source: { sha: secondSha, tree: secondTree, cloudImageDigest: "sha256:" + "5".repeat(64), clean: true },
      },
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
  assert.equal(receipt.reason, "launch_identity_mismatch",
    "the launch names another Workspace, which is an identity mismatch, not the session account fence");
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

test("a settled receipt may not claim a scope or an establishment it cannot prove", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(receipt.scope, "workspace_purchase");

  // Each of these is the settled receipt with one honesty field changed. If the
  // validator stops reading a field, the receipt that claims it is accepted.
  const forged = [
    ["a wider scope", { ...receipt, scope: "workspace_business" }],
    ["a full qualification", { ...receipt, scope: "workspace_business_qualification" }],
    ["an application installation", { ...receipt, uncovered: { ...receipt.uncovered, applicationInstallation: true } }],
    ["a Gateway Key lifecycle", { ...receipt, uncovered: { ...receipt.uncovered, gatewayKeyLifecycle: true } }],
    ["an owner deletion", { ...receipt, uncovered: { ...receipt.uncovered, ownerDeletion: true } }],
    ["a dropped uncovered field", { ...receipt, uncovered: { applicationInstallation: false } }],
    ["an extra uncovered field", { ...receipt, uncovered: { ...receipt.uncovered, somethingElse: false } }],
  ];
  for (const [what, value] of forged) {
    assert.throws(() => validateBusinessQualificationReceipt(value),
      { code: "qualification_business_receipt_invalid" }, what);
  }
});

test("a delivery claim has to carry the observation that proves it", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
  assert.equal(receipt.delivery.componentsObserved, 2);
  assert.match(receipt.delivery.workspaceEntryDigest, /^sha256:[0-9a-f]{64}$/);
  assert.doesNotMatch(JSON.stringify(receipt.delivery), /https?:\/\//, "the entry URL is recorded as a digest");

  for (const [what, value] of [
    ["no components observed", { ...receipt, delivery: { ...receipt.delivery, componentsObserved: 0 } }],
    ["an entry digest that is not one", { ...receipt, delivery: { ...receipt.delivery, workspaceEntryDigest: "" } }],
    ["delivery evidence on a run that observed none", {
      ...receipt, checks: { ...receipt.checks, workspace_ready: false },
      delivery: { workspaceEntryDigest: "", applicationOperationDigest: "", componentsObserved: 0 },
    }],
  ]) {
    assert.throws(() => validateBusinessQualificationReceipt(value),
      { code: "qualification_business_receipt_invalid" }, what);
  }
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


// The Runtime's own `ready` is derived by the platform from the application
// record alone, so a settled receipt has to come from the component list.
test("a Runtime whose components are unhealthy does not settle even when it reports ready", async () => {
  for (const components of [
    [{ name: "console", ok: false }],
    [{ name: "console", ok: true }, { name: "gateway", ok: false }],
    [],
  ]) {
    const platform = controlPlane({ runtimeComponents: components });
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.notEqual(state, "settled", JSON.stringify(components));
    assert.equal(receipt.reason, "runtime_not_ready", JSON.stringify(components));
    assert.equal(receipt.checks.runtime_ready, false, JSON.stringify(components));
  }
});

// An adapter that cannot produce a scope is a blocked run. Reporting it as an
// unreadable owner would assert an owner readback that was never attempted.
test("an adapter that cannot open its scope is blocked, and the reason is reachable", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform, { failOpen: "cluster unreachable" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), adapter);

  assert.equal(state, "blocked");
  assert.equal(receipt.reason, "provider_scope_unavailable");
  assert.ok(QUALIFICATION_TERMINAL_REASONS.blocked.includes(receipt.reason),
    "the declared reason is one this flow actually emits");
  assert.equal(platform.state.posted, 0);
  assert.equal(receipt.checks.account_bound, false, "no owner readback was attempted");
});

// The tenant digest the receipt publishes has to be the one derived from the
// account the fence read, or the two can name different accounts.
test("a hand-assembled request cannot bind the fence to one account and publish another", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("local-docker");
  const forged = { ...request, tenant: { ...request.tenant, accountDigest: `sha256:${"0".repeat(64)}` } };
  await assert.rejects(run(forged, providerAdapter("local-docker", platform)), { code: "qualification_request_invalid" });
  assert.equal(platform.state.posted, 0);
});


// ---------------------------------------------------------------------------
// Review fixes. Each test below fails against 14d01c48 for the reason its name
// states, and each keeps the evidence that closes it.
// ---------------------------------------------------------------------------

// C1: the Ledger receipt was bound to the Workspace and the amount but not to
// the operation that caused the charge, so a receipt for another launch sharing
// both could pay for this one.
test("a Ledger receipt naming another launch operation does not settle this run", async () => {
  const platform = controlPlane({
    receipt: { operationId: "workspace-launch-ffffffffffffffffff" },
  });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "charge_receipt_mismatch");
  assert.equal(receipt.checks.charge_receipted, false, "another operation's receipt is not this run's charge");
  assert.equal(receipt.checks.wallet_confirmed, false, "the balance is not used to infer a receipt that does not bind");
});

test("a launch readback that answers for another operation identity is refused", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("local-docker");
  const operationId = launchOperationId(ACCOUNT_ID, request.operation.idempotencyKey);
  const workspaceId = launchWorkspaceId(ACCOUNT_ID, operationId);
  // The order comes back under a different operation identity while naming this
  // run's Workspace, so the identity is the only thing wrong with it.
  platform.http.request = async () => ({
    response: { status: 200 },
    payload: { operationId: "workspace-launch-ffffffffffffffffff", workspaceId },
  });
  const { state, receipt } = await run(request, providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "launch_identity_mismatch");
  assert.equal(platform.state.posted, 0, "an order under another identity is never followed or reissued");
});

// C2: a redelivery whose order was still open bought nothing itself, but the
// original charge can land while it waits. Every check was true, yet the
// contract accepted only two settled shapes and threw after a real purchase.
test("a resumed order whose original charge lands while this run waits settles honestly", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("local-docker");
  const operationId = launchOperationId(ACCOUNT_ID, request.operation.idempotencyKey);
  const workspaceId = launchWorkspaceId(ACCOUNT_ID, operationId);
  // The earlier delivery admitted the order and its charge has not landed yet.
  const admitted = {
    operationId, workspaceId, provisioningMode: "resource_only",
    status: "running", phase: "runtime", receiptId: "",
    computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001",
  };
  platform.state.orders.set(operationId, admitted);
  platform.state.byKey.set(request.operation.idempotencyKey, operationId);
  platform.state.workspaceId = workspaceId;
  platform.state.operationId = operationId;

  let charged = false;
  const wait = async () => {
    if (charged) return;
    charged = true;
    // The original charge lands while this delivery waits, and the order reaches
    // the same terminal receipt the first delivery would have seen.
    platform.state.wallet = String(BigInt(platform.state.wallet) - BigInt(QUOTE_MICROS));
    platform.state.orders.set(operationId, {
      ...admitted, status: "succeeded", phase: "succeeded", receiptId: "rcpt_qualification_1",
    });
  };
  const { state, receipt } = await run(request, providerAdapter("local-docker", platform), { wait });

  assert.equal(state, "settled", "the purchase really settled and the evidence says so honestly");
  assert.equal(receipt.operation.resumed, true, "this delivery found the order instead of issuing one");
  assert.equal(platform.state.posted, 0, "a resumed order is never reissued");
  assert.equal(receipt.mutations.observed.purchases, 0, "this run bought nothing");
  assert.equal(receipt.mutations.observed.charges, 1, "the original charge landed while it waited");
  assert.equal(receipt.checks.wallet_confirmed, true);
  assert.equal(
    BigInt(receipt.accounting.walletBeforeUsdMicros) - BigInt(receipt.accounting.walletAfterUsdMicros),
    BigInt(QUOTE_MICROS),
    "the wallet moved by exactly the quote",
  );
});

// S1: a hand-assembled partial scope skipped the bounds a real request declares
// and reached the purchase before failing on `undefined < 1`.
test("a hand-assembled request with a partial scope is refused before anything is posted", async () => {
  const platform = controlPlane();
  const request = qualificationRequest("local-docker");
  const partial = { ...request, scope: { purchases: 1, charges: 1 } };
  await assert.rejects(run(partial, providerAdapter("local-docker", platform)), { code: "qualification_request_invalid" });
  assert.equal(platform.state.posted, 0, "nothing is issued for a request the contract did not build");
});

// S7: a copy has every field of a valid request and none of its admission.
test("a spread copy of a valid request is refused as an admission", async () => {
  const platform = controlPlane();
  const copy = { ...qualificationRequest("local-docker") };
  await assert.rejects(run(copy, providerAdapter("local-docker", platform)), { code: "qualification_request_invalid" });
  assert.equal(platform.state.posted, 0);
});

// S2: an adapter that throws while opening may still have opened something, so
// cleanup has to run on the attempt rather than on a returned scope.
test("an adapter that throws while opening its scope still gets one cleanup attempt", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform, { failOpen: "cluster unreachable" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), adapter);

  assert.equal(state, "blocked");
  assert.equal(receipt.reason, "provider_scope_unavailable");
  assert.equal(adapter.calls.closed, 1, "the adapter that may have opened something is asked to put it back");
  assert.equal(adapter.calls.healthReads, 1, "deployment health is read on the same attempt");
  assert.equal(receipt.cleanup.performed, true);
  assert.equal(platform.state.posted, 0);
});

// S3: auto-renew would commit a future charge this flow never observes.
test("an auto-renewing product is refused rather than purchased", () => {
  assert.throws(() => qualificationRequest("local-docker", {
    product: { resourceType: "workspace", packageId: "basic", autoRenew: true },
  }), { code: "qualification_product_invalid" });
});

// S4: an error is not a result just because its code names the passing reason.
test("an error whose code is the passing reason name is not a pass", async () => {
  const platform = controlPlane();
  const original = platform.http.json.bind(platform.http);
  platform.http.json = async (path, init, auth, statuses) => {
    if (path.startsWith("/api/billing/receipts/")) {
      const error = new Error("receipt read failed");
      error.code = "none";
      throw error;
    }
    return original(path, init, auth, statuses);
  };
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "unknown", "an unclassified error is undetermined, never settled");
  assert.equal(receipt.reason, "owner_readback_unavailable");
  assert.equal(receipt.checks.account_bound, true, "the read that did succeed is still reported honestly");
  assert.equal(receipt.checks.charge_receipted, false);
  assert.equal(receipt.checks.wallet_confirmed, false);
});

// S5: the receipt's product and authority are part of the admission, so a forged
// receipt cannot name a product or an authority this flow never declared.
test("a settled receipt cannot name a product or an authority this flow never declared", async () => {
  const platform = controlPlane();
  const { receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  for (const [what, value] of [
    ["a compute resource", { ...receipt, product: { ...receipt.product, resourceType: "compute" } }],
    ["a traversing package id", { ...receipt, product: { ...receipt.product, packageId: "../basic" } }],
    ["an auto-renewing product", { ...receipt, product: { ...receipt.product, autoRenew: true } }],
    ["a fixture authority class", { ...receipt, authority: { ...receipt.authority, authorityClass: "fixture" } }],
  ]) {
    assert.throws(() => validateBusinessQualificationReceipt(value),
      { code: "qualification_business_receipt_invalid" }, what);
  }
});

// S13: the external J0 producer still emits the legacy provider-named kind, so a
// request may declare it; a preproduction request may not borrow a sandbox one.
test("a Local-Docker request may accept the legacy J0 kind it declares", async () => {
  const legacyReceipt = {
    ...j0ReadyReceipt("local-docker"),
    kind: LEGACY_QUALIFICATION_J0_READY_KINDS[0],
    // The legacy producer predates the Candidate binding, so its receipt
    // carries the source-only shape.
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: true },
  };
  assert.throws(() => qualificationRequest("local-docker", {
    authority: { mode: "live", authorityClass: "sandbox", j0ReadyReceipt: legacyReceipt },
  }), { code: "j0_ready_receipt_invalid" }, "a request that declared no legacy kind still refuses one");

  const platform = controlPlane();
  const request = qualificationRequest("local-docker", {
    legacyReceiptKinds: [...LEGACY_QUALIFICATION_J0_READY_KINDS],
    authority: { mode: "live", authorityClass: "sandbox", j0ReadyReceipt: legacyReceipt },
  });
  const { state } = await run(request, providerAdapter("local-docker", platform));
  assert.equal(state, "settled");
});

test("a preproduction request cannot be qualified by a sandbox J0 receipt", () => {
  assert.throws(() => qualificationRequest("local-docker", {
    authority: { mode: "live", authorityClass: "preproduction", j0ReadyReceipt: j0ReadyReceipt("local-docker") },
  }), { code: "qualification_authority_invalid" }, "the declared class has to be the class the J0 receipt carries");
});

// S14: the Runtime readback has to describe this exact application, URL and
// Workspace, not merely something that is running.
test("a Runtime readback bound to another application, URL or Workspace does not settle", async () => {
  for (const runtime of [
    { workspaceId: "ws-ffffffffffffffff" },
    { url: "https://another.qualification.invalid/" },
    { currentApplication: { operationId: "app-op-ffffffffffffffff" } },
  ]) {
    const platform = controlPlane({ runtime });
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(state, "failed", JSON.stringify(runtime));
    assert.equal(receipt.reason, "runtime_not_ready", JSON.stringify(runtime));
    assert.equal(receipt.checks.runtime_ready, false, JSON.stringify(runtime));
  }
});

test("a Ledger receipt that differs in type, status, Workspace, charge reference or fulfillment does not settle", async () => {
  for (const ledger of [
    { type: "billing.workspace_renewed.v1" },
    { status: "pending" },
    { workspaceId: "ws-ffffffffffffffff" },
    { chargeReference: "" },
    { fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001" } },
    { fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001", runtimeId: "rt-1" } },
    { fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-ffff" } },
  ]) {
    const platform = controlPlane({ receipt: ledger });
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(state, "failed", JSON.stringify(ledger));
    assert.equal(receipt.reason, "charge_receipt_mismatch", JSON.stringify(ledger));
    assert.equal(receipt.checks.charge_receipted, false, JSON.stringify(ledger));
  }
});

test("an owner envelope that is not from the named source is not an owner readback", async () => {
  for (const [what, override, accountBound] of [
    ["a wallet from another source", (path) => path === "/api/gateway/wallet"
      ? { status: 200, payload: { source: "fixture", available: true, status: "available", data: { usdMicros: "999999999" } } }
      : null, true],
    ["an identity answer that is not available", (path) => path === "/api/auth/me"
      ? { status: 200, payload: { source: "sub2api", available: false, status: "unavailable", data: {} } }
      : null, false],
  ]) {
    const platform = controlPlane();
    const original = platform.http.json.bind(platform.http);
    platform.http.json = async (path, init, auth, statuses) => override(path) || original(path, init, auth, statuses);
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(state, "unknown", what);
    assert.equal(receipt.reason, "owner_readback_unavailable", what);
    assert.equal(receipt.checks.account_bound, accountBound, what);
    assert.equal(receipt.checks.wallet_confirmed, false, what);
  }
});

// The purchase commits the default application installation beside the resource
// Launch, and the Control Plane progresses that installation after the Launch
// reports success. A run that reads the Workspace the moment the Launch succeeds
// reports a paid delivery as a product failure; a fake that answers `ready`
// synchronously cannot show the difference.
test("the run waits for the committed application installation before reading the delivery", async () => {
  const platform = controlPlane({ applicationStatus: "installing", applicationReadyAfter: 3 });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "settled");
  assert.equal(receipt.checks.workspace_ready, true);
  assert.ok(platform.state.workspaceReads > 3, "the owner was asked until the installation finished");
  assert.equal(platform.state.resumes, 0, "an installation that is progressing needs no continuation");
});

test("a stalled application installation is resumed exactly once through the customer's own command", async () => {
  const platform = controlPlane({ applicationStatus: "installing", applicationStalled: true, applicationReadyAfter: 1 });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "settled");
  assert.equal(platform.state.resumes, 1, "one continuation, however many polls the delivery then needs");
  assert.deepEqual(platform.state.resumeKeys, [`application-resume:${receipt.operation.operationId}`]);
});

test("a continuation projection that names another installation does not settle", async () => {
  const platform = controlPlane({
    applicationStatus: "installing", applicationStalled: true,
    resumeProjection: (state) => ({ workspaceId: state.workspaceId, applicationInstallation: {
      operationId: "app-install-0123456789abcdef", applicationId: "app-ffffffffffffffff", revision: "revision-1",
    } }),
  });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "workspace_not_ready");
  assert.equal(platform.state.resumes, 1, "another installation's continuation is not followed twice");
});

test("an application installation parked for review is its own product failure", async () => {
  const platform = controlPlane({ applicationManualReview: true });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "application_installation_manual_review");
  assert.equal(receipt.checks.workspace_ready, false);
  assert.equal(receipt.checks.charge_receipted, true, "the charge is still reported honestly");
  assert.equal(platform.state.posted, 1, "a product failure is not retried as another purchase");
});

test("an application installation that never becomes ready is not settled", async () => {
  const platform = controlPlane({ applicationStatus: "installing" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "workspace_not_ready");
  assert.equal(receipt.checks.workspace_ready, false);
  assert.equal(platform.state.posted, 1, "the bounded wait gives up without buying again");
});

test("the Workspace is found on a later owner page rather than assumed to be on the first", async () => {
  const platform = controlPlane({ workspacePage: 2 });
  const { state } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "settled");
  assert.ok(platform.state.workspacePages.includes("/api/workspaces?page=2&pageSize=20"),
    "the owner list is paginated until the Workspace this run bought is found");
});

test("a Launch that is not the resource-only purchase this flow accounts for does not settle", async () => {
  const platform = controlPlane({ launchProvisioningMode: "full" });
  const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));

  assert.equal(state, "failed");
  assert.equal(receipt.reason, "launch_identity_mismatch");
  assert.equal(receipt.checks.charge_receipted, false, "the three-resource receipt rule is not applied to another shape");
});

test("deployment health is read before the scope is closed", async () => {
  const platform = controlPlane();
  const adapter = providerAdapter("local-docker", platform);
  const order = [];
  adapter.readDeploymentHealth = async () => { order.push("health"); return "healthy"; };
  adapter.closeBusinessScope = async () => { order.push("close"); return { restored: true }; };
  const { state } = await run(qualificationRequest("local-docker"), adapter);

  assert.equal(state, "settled");
  assert.deepEqual(order, ["health", "close"], "a closed scope cannot answer for deployment health");
});

test("a backward clock cannot invalidate a receipt after the purchase", async () => {
  const platform = controlPlane();
  const RealDate = globalThis.Date;
  const startedAt = "2030-01-01T00:00:00.000Z";
  const completedAt = "2020-01-01T00:00:00.000Z";
  const readings = [startedAt, completedAt];
  let read = 0;
  globalThis.Date = class extends RealDate {
    constructor(...args) {
      if (args.length === 0) super(readings[Math.min(read++, readings.length - 1)]);
      else super(...args);
    }
  };
  try {
    const { state, receipt } = await run(qualificationRequest("local-docker"), providerAdapter("local-docker", platform));
    assert.equal(read, 2, "the run read the clock exactly twice");
    assert.equal(receipt.startedAt, startedAt);
    assert.equal(receipt.completedAt, startedAt, "a backdated completion is clamped to the start");
    assert.equal(state, "settled");
  } finally {
    globalThis.Date = RealDate;
  }
});
