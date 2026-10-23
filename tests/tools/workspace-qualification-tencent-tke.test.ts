import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  QUALIFICATION_BUSINESS_CHECKS,
  QUALIFICATION_UNCOVERED_SCOPE,
  validateBusinessQualificationReceipt,
} from "../../tools/workspace-qualification-contract.ts";
import {
  assertQualificationAdapter,
  createQualificationRequest,
  runWorkspaceQualification,
} from "../../tools/workspace-qualification-orchestrator.ts";
import {
  TENCENT_TKE_BOUNDARY_OPERATIONS,
  TENCENT_TKE_DEPLOYMENT_MODES,
  TENCENT_TKE_PROVIDER,
  createTencentTkeQualificationAdapter,
  tencentTkeDeployment,
} from "../../tools/workspace-qualification-tencent-tke.ts";

// The owner mints the operation and the Workspace identities, and the public
// contract publishes them as strings without a shape. These assertions hold
// that the evidence names an identity the owner minted, not one this buyer
// could have derived, without pinning how the owner spells it.
const OWNER_MINTED_IDENTITY = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/;

// The adapter is driven through the same public orchestrator entry the
// Local-Docker flow uses, over a provider boundary reduced to the three
// reachability operations. Nothing here reaches TKE, a cluster or a database:
// the fake Control Plane is the platform's own owner API shape, and the
// boundary is the Fabric/Instance adapter reduced to typed fixture answers.

const SOURCE_SHA = "a".repeat(40);
const SOURCE_TREE = "b".repeat(40);
const ACCOUNT_ID = "acct-customer-0001";
const ACCOUNT_EMAIL = "customer@qualification.invalid";
const QUOTE_MICROS = "52580000";
const ENTRY_URL = "https://workspace.qualification.invalid/";
const APPLICATION_OPERATION_ID = "app-op-0123456789abcdef01";
const OWNER_ORIGIN = "https://cloud.qualification.invalid";
const DEPLOYMENT = {
  deploymentMode: "managed_tke",
  fabricProvider: "tencent-tke",
  clusterId: "cls-qualification0001",
  region: "ap-guangzhou",
  namespace: "tke-qualification",
  ownerOrigin: OWNER_ORIGIN,
};

// The identities the Workspace owner mints. The owner mints the operation and
// the Workspace identity of every order it admits, so the fake mints them too
// and never derives them from the declared idempotency identity.
const ownerID = (prefix, ...parts) =>
  `${prefix}_${createHash("sha1").update(parts.join(":")).digest("hex").slice(0, 32)}`;

// The catalog the owner offers. The buyer resolves its declaration from these
// the way the Console does: the first approved Runtime Release and the first
// available compute and storage plan.
const OWNER_RUNTIME_VERSION_ID = `rtv_${"a".repeat(32)}`;
const OWNER_COMPUTE_PLAN_ID = `cmp_${"b".repeat(32)}`;
const OWNER_STORAGE_PLAN_ID = `stp_${"c".repeat(32)}`;

/*
 * The owner's idempotency record is keyed by the identity it was handed and
 * guarded by the body it was given: a repeated command is a replay only when
 * it repeats the same command, and an identity reused with another body is a
 * different purchase that the owner refuses rather than admits.
 */
const canonicalJson = (value) => {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort()
      .map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value === undefined ? null : value);
};
const bodyDigest = (body) => createHash("sha256").update(canonicalJson(body)).digest("hex");

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
  product: {
    resourceType: "workspace",
    applicationSelection: { kind: "opl_app", runtimeVersionId: OWNER_RUNTIME_VERSION_ID },
    computePlanId: OWNER_COMPUTE_PLAN_ID,
    storagePlanId: OWNER_STORAGE_PLAN_ID,
    autoRenew: false,
  },
  provider,
  scope: { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 },
  authority: { mode: "live", authorityClass: "sandbox", j0ReadyReceipt: j0ReadyReceipt(provider) },
  source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: true },
  ...overrides,
});

/*
 * The platform, reduced to the Workspace owner API the purchase flow reads.
 *
 * The Workspace owner is the only writer for a new purchase. It prices the
 * quote, it mints the operation and the Workspace identity, and it keeps the
 * idempotency record that decides whether a repeated command is a replay of
 * the same order or a second purchase. The fake below plays that owner: it
 * mints the identities itself, so the evidence a run produces is bound to
 * identities this test never derived.
 *
 * The retained Control Plane launch route is read, never written. This account
 * holds no order that route admitted before the cutover, and a purchase command
 * on it would be a second purchase, so the fake answers its read with nothing
 * and refuses its write.
 */
function controlPlane({ walletBeforeMicros = "999999999", quoteMicros = QUOTE_MICROS, loseResponseOnce = false,
  noBillingReceipt = false, orderStatus = "succeeded" } = {}) {
  const state = {
    orders: new Map(), byKey: new Map(), quotes: new Map(), quoteById: new Map(),
    posted: 0, charges: [], commands: [], calls: [],
    wallet: walletBeforeMicros, workspaceId: "", operationId: "", receiptId: "", quoteId: "",
    responsesLost: false,
  };
  const envelope = (data) => ({ source: "sub2api", available: true, status: "available", data });
  // The owner's own projection of an operation it admitted. It carries the
  // identities the owner minted and the owner's own stage and status.
  const ownerOperation = (admitted) => ({
    operationId: admitted.operationId, owner: "workspace", kind: "create_workspace",
    resourceId: admitted.workspaceId, status: admitted.status, stage: admitted.stage,
    requestId: admitted.requestId,
  });
  // The Ledger's own record of the purchase. It names the order that caused
  // it, the Workspace it paid for and the resources it delivered.
  const chargeReceipt = () => ({
    receiptId: state.receiptId, workspaceId: state.workspaceId, operationId: state.operationId,
    type: "billing.workspace_purchased.v1", status: "completed",
    chargeReference: "opl:workspace-purchase:charge:0001",
    totalUsdMicros: quoteMicros,
    fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001" },
  });
  // The retained route still holds the orders it admitted before the cutover.
  // This account holds none, so its read answers with nothing.
  const retainedLaunch = () => ({ response: { status: 404 }, payload: null });

  // The owner's own answer to one request. The route decides what the owner
  // says, and the caller decides which status it is willing to read.
  const ownerAnswer = async (path, init) => {
    if (path === "/api/gateway/wallet") return { status: 200, payload: envelope({ usdMicros: state.wallet }) };
    if (path === "/api/auth/me") {
      return { status: 200, payload: envelope({
        accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active", sub2apiUserId: "9001",
      }) };
    }
    // The live owner session. The identity owner binds the actor it
    // authenticated to the Gateway user and publishes the Gateway email as the
    // display name, so this session and the Control Plane session name the same
    // customer.
    if (path === "/api/v2/auth/session") {
      return { status: 200, payload: {
        actorId: "9001", displayName: ACCOUNT_EMAIL, tenantId: "tenant-qualification",
        permissions: [], csrfToken: "owner-csrf", expiresAt: "2026-10-18T12:00:00Z",
      } };
    }
    // The wallet the Gateway owner holds, read under the owner session's own
    // authorization.
    if (path === "/api/v2/wallet") {
      return { status: 200, payload: {
        source: "gateway", status: "available", balanceUSDMicros: state.wallet,
        currency: "USD", fetchedAt: "2026-10-18T00:00:00Z",
      } };
    }

    // The owner owns the catalog a buyer prices from, and the Console reads
    // it the same way: the first approved Runtime Release and the first
    // available compute and storage plan. The page is the owner's own.
    if (path.startsWith("/api/v2/catalog/runtime-versions")) {
      return { status: 200, payload: { items: [{ id: OWNER_RUNTIME_VERSION_ID, name: "Qualification Runtime", status: "approved" }] } };
    }
    if (path.startsWith("/api/v2/catalog/compute-plans")) {
      return { status: 200, payload: { items: [{ id: OWNER_COMPUTE_PLAN_ID, name: "Qualification Compute", availability: "available" }] } };
    }
    if (path.startsWith("/api/v2/catalog/storage-plans")) {
      return { status: 200, payload: { items: [{ id: OWNER_STORAGE_PLAN_ID, name: "Qualification Storage", availability: "available" }] } };
    }
    // The quote is the owner's own price for the declared purchase. It is
    // recorded against the idempotency identity it was handed and guarded
    // by the body it was given, so a repeated quote replays this quote
    // instead of pricing a second one.
    if (path === "/api/v2/quotes" && init.method === "POST") {
      const key = String(init.headers?.["idempotency-key"] || "");
      const body = init.body || {};
      const digest = bodyDigest(body);
      const existing = state.quotes.get(key);
      if (existing) {
        if (existing.digest !== digest) throw new Error(`HTTP POST ${path} returned 409`);
        return { status: 200, payload: existing.quote };
      }
      const quote = {
        id: ownerID("qte", key),
        purpose: "deploy",
        runtimeVersionId: String(body?.applicationSelection?.runtimeVersionId || ""),
        computePlanId: String(body?.computePlanId || ""),
        storagePlanId: String(body?.storagePlanId || ""),
        modelSelections: body?.modelSelections || [],
        periodMonths: Number(body?.periodMonths || 0),
        totalUSDMicros: quoteMicros,
        // An offered quote has no order yet. The order takes it, and an
        // accepted quote is one an order already took.
        status: "offered",
      };
      state.quotes.set(key, { quote, digest });
      state.quoteById.set(quote.id, quote);
      state.quoteId = quote.id;
      return { status: 201, payload: quote };
    }
    if (/^\/api\/v2\/quotes\/[^/]+$/.test(path)) {
      const quote = state.quoteById.get(decodeURIComponent(path.split("/").pop()));
      return quote ? { status: 200, payload: quote } : { status: 404, payload: null };
    }
    // The order is admitted against the quote the owner priced. One
    // idempotency identity admits one order, and the body it was given
    // guards it, so a redelivery is answered from the order already
    // admitted rather than promoted into a second purchase.
    if (path === "/api/v2/workspaces" && init.method === "POST") {
      const key = String(init.headers?.["idempotency-key"] || "");
      const body = init.body || {};
      const digest = bodyDigest(body);
      state.commands.push(body);
      const replayed = state.byKey.get(key);
      if (replayed) {
        if (replayed.digest !== digest) throw new Error(`HTTP POST ${path} returned 409`);
        return { status: 202, payload: ownerOperation(replayed.admitted) };
      }
      const admitted = {
        operationId: ownerID("op", key, String(state.posted + 1)),
        workspaceId: ownerID("workspace", key, String(state.posted + 1)),
        // The owner records the request that admitted the order, so a
        // redelivery can tell its own order from the one it is replaying.
        requestId: String(init.headers?.["x-opl-request-id"] || ""),
        status: orderStatus,
        stage: orderStatus === "succeeded" ? "succeeded" : "creating",
      };
      state.byKey.set(key, { admitted, digest });
      state.orders.set(admitted.operationId, admitted);
      state.workspaceId = admitted.workspaceId;
      state.operationId = admitted.operationId;
      state.receiptId = ownerID("rcpt", admitted.operationId);
      state.posted += 1;
      state.wallet = String(BigInt(state.wallet) - BigInt(quoteMicros));
      state.charges.push(quoteMicros);
      // The quote this order took is an accepted quote now.
      const quote = state.quoteById.get(String(body?.quoteId || ""));
      if (quote) quote.status = "accepted";
      // The platform admitted the order and its answer was lost. This is
      // the response-loss case, not a refused command: the order above
      // exists. The answer is lost once, so the recovery reads the order
      // the owner already admitted instead of a second purchase.
      if (loseResponseOnce && !state.responsesLost) {
        state.responsesLost = true;
        throw new Error("socket hang up after the platform accepted the command");
      }
      return { status: 202, payload: ownerOperation(admitted) };
    }
    if (/^\/api\/v2\/operations\/workspace\/[^/]+$/.test(path)) {
      const admitted = state.orders.get(decodeURIComponent(path.split("/").pop()));
      if (!admitted) throw new Error(`HTTP GET ${path} returned 404`);
      return { status: 200, payload: ownerOperation(admitted) };
    }
    // The Ledger's own inventory of receipts. The receipt of an owner order
    // is found in that inventory, bound to the order and the Workspace the
    // owner minted.
    if (path.startsWith("/api/billing/receipts?")) {
      if (noBillingReceipt) throw new Error(`HTTP GET ${path} returned 404`);
      return { status: 200, payload: {
        source: "ledger", available: true, status: "available",
        data: { receipts: state.receiptId ? [chargeReceipt()] : [], hasMore: false },
      } };
    }
    if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
      if (noBillingReceipt) throw new Error(`HTTP GET ${path} returned 404`);
      const receiptId = decodeURIComponent(path.split("/").pop());
      if (receiptId !== state.receiptId) return { status: 404, payload: null };
      return { status: 200, payload: {
        source: "ledger", available: true, status: "available",
        data: chargeReceipt(),
      } };
    }
    // The retained Control Plane projection still holds the Workspace and
    // the application installation the platform committed beside it.
    if (path.startsWith("/api/workspaces?")) {
      return { status: 200, payload: {
        source: "control-plane", available: true, status: "available",
        data: { items: [{
          id: state.workspaceId, url: ENTRY_URL, openable: true,
          currentApplication: { status: "ready", entryUrl: ENTRY_URL, operationId: APPLICATION_OPERATION_ID },
        }], total: 1 },
      } };
    }
    if (/^\/api\/workspaces\/[^/]+\/runtime-status$/.test(path)) {
      return { status: 200, payload: {
        source: "fabric", available: true, status: "available",
        data: {
          workspaceId: state.workspaceId, ready: true, status: "running", url: ENTRY_URL,
          runtimeId: "rt-0123456789abcdef", checks: [{ name: "console", ok: true }, { name: "gateway", ok: true }],
          currentApplication: { operationId: APPLICATION_OPERATION_ID },
        },
      } };
    }
    throw new Error(`unexpected path ${path}`);
  };

  const http = {
    async request(path, init = {}, auth = null) {
      state.calls.push({ method: init.method || "GET", path });
      const operation = /^\/api\/v2\/operations\/workspace\/([^/]+)$/.exec(path);
      if (operation) {
        // The order is read back under the identity the owner minted.
        const admitted = state.orders.get(decodeURIComponent(operation[1]));
        return admitted
          ? { response: { status: 200 }, payload: ownerOperation(admitted) }
          : { response: { status: 404 }, payload: null };
      }
      if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) return retainedLaunch();
      throw new Error(`unexpected request ${path}`);
    },
    async json(path, init = {}, auth = null, statuses = [200]) {
      state.calls.push({ method: init.method || "GET", path });
      const answer = await ownerAnswer(path, init);
      // The owner answers at the status the caller asked for, and no other.
      if (!statuses.includes(answer.status)) throw new Error(`HTTP ${init.method || "GET"} ${path} returned ${answer.status}`);
      return answer;
    },
  };
  return { http, state };
}

// The Fabric/Instance boundary reduced to typed answers. It has no business
// evidence to give: it can only say whether the provider channel is open, what
// the provider's own readiness readback says, and whether the close was proven.
function providerBoundary(platform, {
  provider = TENCENT_TKE_PROVIDER,
  establish = "open",
  establishProvider = TENCENT_TKE_PROVIDER,
  release = "released",
  releaseProvider = TENCENT_TKE_PROVIDER,
  releaseClusterId = "",
  releaseThrows = false,
  health = "ready",
  healthProvider = TENCENT_TKE_PROVIDER,
  healthThrows = false,
  clusterId = DEPLOYMENT.clusterId,
  healthNamespace = DEPLOYMENT.namespace,
  ready = true,
  serviceReady = true,
  failedChecks = [],
  origin = OWNER_ORIGIN,
  transport = null,
} = {}) {
  const calls = { establish: 0, release: 0, health: 0 };
  const auth = { cookie: "opl_session=qualification", csrf: "qualification-csrf" };
  const identity = (resultProvider = provider, resultCluster = clusterId, resultNamespace = DEPLOYMENT.namespace) => ({
    provider: resultProvider, clusterId: resultCluster, namespace: resultNamespace,
  });
  return {
    provider,
    calls,
    auth,
    async establishReachability({ deployment, operationKey }) {
      calls.establish += 1;
      assert.equal(deployment.fabricProvider, TENCENT_TKE_PROVIDER);
      assert.match(operationKey, /^opl-qualification:[0-9a-f]{64}\nsha256:/);
      if (establish === "throw") throw new Error("tke reachability readback failed");
      if (establish === "open-then-throw") {
        // The provider opened the binding and the answer was lost on the way
        // back. The adapter cannot know whether it opened, so close must ask.
        throw new Error("the provider opened the binding and its answer was lost");
      }
      if (establish === "absent") return { state: "absent", reason: "workload_absent", ...identity(establishProvider) };
      if (establish === "unknown") return { state: "unknown", reason: "readback_unavailable", ...identity(establishProvider) };
      return { state: "open", ...identity(establishProvider), transport: transport ?? { origin, http: platform.http, auth } };
    },
    async releaseReachability() {
      calls.release += 1;
      if (releaseThrows) throw new Error("tke release readback failed");
      return { state: release, ...identity(releaseProvider, releaseClusterId || clusterId) };
    },
    async readProviderHealth() {
      calls.health += 1;
      if (healthThrows) throw new Error("tke health readback failed");
      return { state: health, ...identity(healthProvider, clusterId, healthNamespace), ready, serviceReady, failedChecks };
    },
  };
}

const deploymentFor = (request, overrides = {}) => ({
  ...DEPLOYMENT, candidateReceiptDigest: request.candidate.receiptDigest, ...overrides,
});
const adapterFor = (platform, request, options) => createTencentTkeQualificationAdapter({
  deployment: deploymentFor(request),
  boundary: providerBoundary(platform, options),
});
const run = (request, adapter) => runWorkspaceQualification({ request, adapter }, { wait: async () => {} });

test("the TKE adapter satisfies the shared seam and settles the shared flow", async () => {
  assert.equal(TENCENT_TKE_PROVIDER, "tencent-tke");
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const boundary = providerBoundary(platform);
  const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });

  assert.deepEqual(Object.keys(adapter).sort(), ["closeBusinessScope", "openBusinessScope", "provider", "readDeploymentHealth"]);
  assertQualificationAdapter(adapter, TENCENT_TKE_PROVIDER);
  assert.throws(() => assertQualificationAdapter(adapter, "local-docker"), {
    code: "qualification_adapter_provider_mismatch",
  });

  const { state, receipt } = await run(request, adapter);
  assert.equal(state, "settled");
  assert.equal(receipt.provider, TENCENT_TKE_PROVIDER);
  assert.equal(receipt.state, "settled");
  assert.ok(QUALIFICATION_BUSINESS_CHECKS.every((name) => receipt.checks[name] === true));
  assert.equal(platform.state.posted, 1);
  // The purchase is issued on the Workspace owner route: the quote it buys
  // against and the order it admits. The retained launch route is not a
  // purchase command any more, so no purchase is written there.
  assert.deepEqual(
    platform.state.calls.filter((call) => call.method === "POST").map((call) => call.path),
    ["/api/v2/quotes", "/api/v2/workspaces"],
    "the quote and the order are issued on the Workspace owner route",
  );
  assert.equal(
    platform.state.calls.some((call) => call.path.startsWith("/api/workspace-launches") && call.method === "POST"),
    false,
    "the retained launch route admits no purchase",
  );
  // The command names the quote it is buying, and carries nothing else: the
  // owner route has no provisioning mode to ask for.
  assert.deepEqual(Object.keys(platform.state.commands[0]).sort(), ["name", "quoteId", "renewalMode"]);
  assert.equal(platform.state.commands[0].quoteId, platform.state.quoteId, "the command names the quote it is buying");
  assert.equal(platform.state.commands[0].renewalMode, "manual");
  assert.equal(Object.hasOwn(platform.state.commands[0], "provisioningMode"), false,
    "the owner route has no provisioning mode to declare");
  // The identities in the evidence are the ones the owner minted, and the order
  // is read back from the owner that minted them.
  assert.match(receipt.operation.operationId, OWNER_MINTED_IDENTITY);
  assert.match(receipt.operation.workspaceId, OWNER_MINTED_IDENTITY);
  assert.equal(receipt.operation.operationId, platform.state.operationId, "the evidence names the owner's own operation");
  assert.equal(receipt.operation.workspaceId, platform.state.workspaceId, "the evidence names the owner's own Workspace");
  assert.equal(
    platform.state.calls.some((call) => call.path === `/api/v2/operations/workspace/${receipt.operation.operationId}`),
    true,
    "the order is read back from the owner that minted it",
  );
  // The price the evidence reports is the owner's own frozen quote total.
  assert.equal(receipt.accounting.quoteUsdMicros, QUOTE_MICROS, "the evidence reports the owner's frozen quote total");
  assert.equal(boundary.calls.establish, 1);
  assert.equal(boundary.calls.release, 1, "the boundary that opened the scope closes it");
  assert.equal(boundary.calls.health, 1);
});

test("a TKE receipt has exactly the Local-Docker business receipt shape", async () => {
  const tkePlatform = controlPlane();
  const tkeRequest = qualificationRequest(TENCENT_TKE_PROVIDER);
  const tke = await run(tkeRequest, adapterFor(tkePlatform, tkeRequest));

  const localPlatform = controlPlane();
  const localRequest = qualificationRequest("local-docker");
  const localAdapter = {
    provider: "local-docker",
    async openBusinessScope() { return { http: localPlatform.http, auth: { cookie: "opl_session=local" } }; },
    async closeBusinessScope() { return { restored: true }; },
    async readDeploymentHealth() { return "healthy"; },
  };
  const local = await run(localRequest, localAdapter);

  // The shape, not the values: every block and nested block must have the same
  // keys and value kinds for both providers, because one contract owns it.
  const shape = (value) => Object.fromEntries(Object.entries(value).map(([key, item]) => [
    key,
    Array.isArray(item) ? "array" : item && typeof item === "object" ? shape(item) : typeof item,
  ]).sort(([left], [right]) => left.localeCompare(right)));
  assert.deepEqual(shape(tke.receipt), shape(local.receipt));
  assert.equal(tke.receipt.provider, TENCENT_TKE_PROVIDER);
  assert.equal(local.receipt.provider, "local-docker");
  assert.equal(tke.receipt.state, "settled");
  assert.equal(local.receipt.state, "settled");
});

test("the adapter refuses a boundary that is not the exact TKE provider", () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const deployment = deploymentFor(request);

  assert.throws(
    () => createTencentTkeQualificationAdapter({ deployment, boundary: providerBoundary(platform, { provider: "local-docker" }) }),
    { code: "tencent_tke_boundary_invalid" },
  );
  for (const operation of TENCENT_TKE_BOUNDARY_OPERATIONS) {
    const boundary = providerBoundary(platform);
    delete boundary[operation];
    assert.throws(() => createTencentTkeQualificationAdapter({ deployment, boundary }), {
      code: "tencent_tke_boundary_invalid",
    }, operation);
  }
});

test("the deployment binding must name a hosted TKE deployment and a usable owner origin", () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const deployment = deploymentFor(request);
  assert.deepEqual([...TENCENT_TKE_DEPLOYMENT_MODES], ["managed_tke", "platform_owned"]);

  for (const [label, overrides] of [
    ["a customer-owned deployment", { deploymentMode: "customer_owned" }],
    ["a Local-Docker provider", { fabricProvider: "local-docker" }],
    ["a Candidate digest that is not one", { candidateReceiptDigest: "not-a-digest" }],
    ["an owner origin with a path", { ownerOrigin: `${OWNER_ORIGIN}/admin` }],
    ["an owner origin with credentials", { ownerOrigin: "https://user:pass@cloud.qualification.invalid" }],
    ["no namespace", { namespace: "" }],
    ["an invalid namespace", { namespace: "Opl_Qualification" }],
    ["no cluster", { clusterId: "" }],
    ["no region", { region: "" }],
  ]) {
    assert.throws(
      () => createTencentTkeQualificationAdapter({ deployment: { ...deployment, ...overrides }, boundary: providerBoundary(platform) }),
      { code: "tencent_tke_deployment_invalid" }, label,
    );
  }

  const normalized = tencentTkeDeployment({ ...deployment, ownerOrigin: `${OWNER_ORIGIN}/` });
  assert.equal(normalized.ownerOrigin, OWNER_ORIGIN);
  assert.equal(Object.isFrozen(normalized), true);
  assert.equal(tencentTkeDeployment({ ...deployment, deploymentMode: "platform_owned" }).deploymentMode, "platform_owned");
});

test("a request for another provider or another installed Candidate cannot use this adapter", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const adapter = adapterFor(platform, request);

  await assert.rejects(adapter.openBusinessScope({ request: qualificationRequest("local-docker") }), {
    code: "tencent_tke_adapter_provider_mismatch",
  });

  const otherCandidate = createTencentTkeQualificationAdapter({
    deployment: deploymentFor(request, { candidateReceiptDigest: `sha256:${"9".repeat(64)}` }),
    boundary: providerBoundary(platform),
  });
  await assert.rejects(otherCandidate.openBusinessScope({ request }), {
    code: "tencent_tke_deployment_mismatch",
  });
  assert.equal(platform.state.posted, 0, "no purchase is issued for a mismatched deployment");
});

test("provider absence, an unreadable provider and a lost open answer are a blocked scope, never a purchase", async () => {
  for (const establish of ["absent", "unknown", "throw"]) {
    const platform = controlPlane();
    const request = qualificationRequest(TENCENT_TKE_PROVIDER);
    const boundary = providerBoundary(platform, { establish });
    const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });

    const { state, receipt } = await run(request, adapter);
    assert.equal(state, "blocked", establish);
    assert.equal(receipt.reason, "provider_scope_unavailable", establish);
    assert.equal(receipt.checks.account_bound, false, "no owner readback was attempted");
    assert.equal(platform.state.posted, 0, "a blocked scope buys nothing");
    assert.equal(boundary.calls.release, 1, "the attempted open is put back");
    assert.equal(boundary.calls.health, 1, "the attempted scope's health is still recorded");
    assert.equal(receipt.cleanup.performed, true);
    assert.equal(receipt.cleanup.restored, true, "the release readback proved nothing was left");
  }
});

test("a provider readback naming another provider, cluster or namespace is refused", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  for (const [label, options, restored] of [
    ["another provider", { establishProvider: "local-docker" }, true],
    ["another cluster", { clusterId: "cls-somewhere-else" }, false],
  ]) {
    const boundary = providerBoundary(platform, options);
    const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });
    await assert.rejects(adapter.openBusinessScope({ request }), { code: "tencent_tke_readback_invalid" }, label);
    assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored }, label);
    assert.equal(boundary.calls.release, 1, `${label}: a refused open is put back`);
  }
  assert.equal(platform.state.posted, 0);
});

test("a transport bound to another origin or carrying no session is refused", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  for (const [label, transport] of [
    ["another origin", { origin: "https://other.qualification.invalid", http: platform.http, auth: { cookie: "opl_session=x" } }],
    ["an origin with a path", { origin: `${OWNER_ORIGIN}/api`, http: platform.http, auth: { cookie: "opl_session=x" } }],
    ["no session", { origin: OWNER_ORIGIN, http: platform.http, auth: {} }],
    ["no http client", { origin: OWNER_ORIGIN, auth: { cookie: "opl_session=x" } }],
  ]) {
    const boundary = providerBoundary(platform, { transport });
    const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });
    await assert.rejects(adapter.openBusinessScope({ request }), { code: "tencent_tke_transport_invalid" }, label);
    assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true }, label);
    assert.equal(boundary.calls.release, 1, `${label}: a refused open is put back`);
  }
  assert.equal(platform.state.posted, 0);
});

test("an open failure still releases the attempted scope through the orchestrator", async () => {
  const cases = [
    ["a lost open answer", () => ({ establish: "open-then-throw" })],
    ["a readback from another deployment", () => ({ establishProvider: "local-docker" })],
    ["a malformed transport", (platform) => ({
      transport: { origin: `${OWNER_ORIGIN}/api`, http: platform.http, auth: { cookie: "opl_session=x" } },
    })],
  ];
  for (const [label, optionsFor] of cases) {
    const platform = controlPlane();
    const request = qualificationRequest(TENCENT_TKE_PROVIDER);
    const boundary = providerBoundary(platform, optionsFor(platform));
    const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });

    const { state, receipt } = await run(request, adapter);
    assert.equal(boundary.calls.establish, 1, label);
    assert.equal(boundary.calls.release, 1, `${label}: the attempted open is put back`);
    assert.equal(receipt.cleanup.performed, true, label);
    assert.equal(receipt.cleanup.restored, true, label);
    assert.equal(state, "blocked", label);
    assert.equal(platform.state.posted, 0, label);
  }

  // The release readback, not the failed open, decides whether the attempt was
  // put back. An unknown release cannot be read as restored.
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const boundary = providerBoundary(platform, { establish: "open-then-throw", release: "unknown" });
  const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });
  const { state, receipt } = await run(request, adapter);
  assert.equal(boundary.calls.release, 1);
  assert.equal(receipt.cleanup.performed, true);
  assert.equal(receipt.cleanup.restored, false);
  assert.equal(state, "recovery-required");
  assert.equal(receipt.reason, "cleanup_unproven");
});

test("deployment health maps the provider's readiness facts and never moves the business result", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const health = async (options) => {
    const adapter = createTencentTkeQualificationAdapter({
      deployment: deploymentFor(request), boundary: providerBoundary(platform, options),
    });
    return adapter.readDeploymentHealth({ request });
  };

  assert.equal(await health({ health: "ready", ready: true, serviceReady: true, failedChecks: [] }), "healthy");
  assert.equal(await health({ health: "ready", ready: false, serviceReady: true, failedChecks: ["workspace_image_id"] }), "degraded");
  assert.equal(await health({ health: "degraded", ready: false, serviceReady: true, failedChecks: [] }), "degraded");
  assert.equal(await health({ health: "ready", ready: true, serviceReady: false, failedChecks: [] }), "unknown");
  assert.equal(await health({ health: "absent" }), "unknown");
  assert.equal(await health({ health: "unknown" }), "unknown");
  assert.equal(await health({ healthThrows: true }), "unknown");
  assert.equal(await health({ healthProvider: "local-docker" }), "unknown");
  assert.equal(await health({ clusterId: "cls-somewhere-else" }), "unknown");
  assert.equal(await health({ healthNamespace: "another-namespace" }), "unknown");

  const degradedRun = await run(request, adapterFor(platform, request, {
    health: "ready", ready: false, serviceReady: true, failedChecks: ["workspace_image_id"],
  }));
  assert.equal(degradedRun.state, "settled", "a degraded host does not unqualify a proven product");
  assert.equal(degradedRun.receipt.deploymentHealth, "degraded");

  const failedPlatform = controlPlane({ orderStatus: "failed" });
  const failedRun = await run(request, adapterFor(failedPlatform, request, { health: "ready" }));
  assert.equal(failedRun.state, "failed", "a healthy host does not make a failed business run pass");
  assert.equal(failedRun.receipt.deploymentHealth, "healthy");
});

test("a scope is only reported restored when the provider readback proves it", async () => {
  for (const [release, expectedState, expectedReason, restored] of [
    ["released", "settled", "none", true],
    ["absent", "settled", "none", true],
    ["unknown", "recovery-required", "cleanup_unproven", false],
  ]) {
    const platform = controlPlane();
    const request = qualificationRequest(TENCENT_TKE_PROVIDER);
    const { state, receipt } = await run(request, adapterFor(platform, request, { release }));
    assert.equal(state, expectedState, release);
    assert.equal(receipt.reason, expectedReason, release);
    assert.equal(receipt.cleanup.performed, true, release);
    assert.equal(receipt.cleanup.restored, restored, release);
    assert.ok(QUALIFICATION_BUSINESS_CHECKS.every((name) => receipt.checks[name]), release);
  }

  const throwingPlatform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const thrown = await run(request, adapterFor(throwingPlatform, request, { releaseThrows: true }));
  assert.equal(thrown.state, "recovery-required");
  assert.equal(thrown.receipt.reason, "cleanup_unproven");

  const neverOpenedPlatform = controlPlane();
  const neverOpened = adapterFor(neverOpenedPlatform, request);
  assert.deepEqual(await neverOpened.closeBusinessScope({ request }), { restored: true },
    "nothing this adapter opened is left behind");

  for (const options of [{ releaseProvider: "local-docker" }, { releaseClusterId: "cls-somewhere-else" }]) {
    const mismatched = await run(request, adapterFor(controlPlane(), request, options));
    assert.equal(mismatched.state, "recovery-required",
      "a close readback from another deployment cannot prove this scope was restored");
    assert.equal(mismatched.receipt.reason, "cleanup_unproven");
  }
});

test("repeated open and close calls are idempotent under the shared operation identity", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const boundary = providerBoundary(platform);
  const adapter = createTencentTkeQualificationAdapter({ deployment: deploymentFor(request), boundary });

  const first = await adapter.openBusinessScope({ request });
  const second = await adapter.openBusinessScope({ request });
  assert.equal(second, first, "a retry reuses the same scope");
  assert.deepEqual(Object.keys(second).sort(), ["auth", "http"], "no deployment fact leaks into the scope");
  assert.equal(second.http, platform.http, "the boundary's transport is used as given");
  assert.equal(second.auth, boundary.auth, "the boundary's session is used as given");
  assert.equal(boundary.calls.establish, 1, "one provider reachability binding per operation key");

  assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true });
  assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true });
  assert.equal(boundary.calls.release, 1, "one provider release per operation key");

  // A redelivery after a proven close opens the same operation key again; the
  // earlier close cannot be mistaken for the new scope's.
  await adapter.openBusinessScope({ request });
  assert.equal(boundary.calls.establish, 2);
  assert.equal((await adapter.closeBusinessScope({ request })).restored, true);
  assert.equal(boundary.calls.release, 2);
});

test("a redelivery resumes the original TKE purchase and buys nothing twice", async () => {
  const platform = controlPlane();
  const firstRequest = qualificationRequest(TENCENT_TKE_PROVIDER);
  const secondRequest = qualificationRequest(TENCENT_TKE_PROVIDER);
  const first = await run(firstRequest, adapterFor(platform, firstRequest));
  const second = await run(secondRequest, adapterFor(platform, secondRequest));

  assert.equal(first.state, "settled");
  assert.equal(second.state, "settled");
  assert.equal(platform.state.posted, 1, "one admitted order for one declared qualification");
  assert.equal(platform.state.charges.length, 1, "one charge");
  assert.equal(platform.state.commands.length, 2, "the redelivery re-issued the order command");
  assert.equal(platform.state.commands[1].quoteId, platform.state.commands[0].quoteId,
    "the replay names the same quote the first delivery bought");
  assert.equal(first.receipt.operation.resumed, false);
  assert.equal(first.receipt.mutations.observed.purchases, 1);
  assert.equal(second.receipt.operation.resumed, true);
  assert.equal(second.receipt.operation.operationId, first.receipt.operation.operationId);
  assert.equal(second.receipt.operation.workspaceId, first.receipt.operation.workspaceId);
  assert.match(second.receipt.operation.operationId, OWNER_MINTED_IDENTITY,
    "the replayed order keeps the identity the owner minted");
  assert.match(second.receipt.operation.workspaceId, OWNER_MINTED_IDENTITY,
    "the replayed order keeps the Workspace identity the owner minted");
  assert.equal(second.receipt.accounting.quoteUsdMicros, QUOTE_MICROS,
    "the replay is priced by the same frozen quote");
  assert.equal(second.receipt.mutations.observed.purchases, 0, "the redelivery bought nothing again");
  assert.equal(second.receipt.mutations.observed.charges, 0, "and charged nothing again");
  assert.equal(second.receipt.mutations.observed.refunds, 0);
  assert.equal(second.receipt.mutations.observed.workspaceDeletions, 0);
  assert.equal(second.receipt.mutations.observed.managedKeys, 0);
  assert.equal(
    BigInt(second.receipt.accounting.walletBeforeUsdMicros) - BigInt(second.receipt.accounting.walletAfterUsdMicros),
    0n,
    "the replay's wallet stood still",
  );
});

test("a lost answer to the command recovers the admitted order, and a lost owner readback stays unknown", async () => {
  const recoveredPlatform = controlPlane({ loseResponseOnce: true });
  const recovered = await run(qualificationRequest(TENCENT_TKE_PROVIDER), adapterFor(
    recoveredPlatform, qualificationRequest(TENCENT_TKE_PROVIDER),
  ));
  assert.equal(recovered.state, "settled");
  assert.equal(recoveredPlatform.state.posted, 1, "the lost answer cost a retry of the read, never a second purchase");
  assert.equal(recoveredPlatform.state.commands.length, 2, "the lost answer was retried once");
  assert.equal(recoveredPlatform.state.commands[1].quoteId, recoveredPlatform.state.commands[0].quoteId,
    "the retry repeats the same order command");
  assert.equal(recovered.receipt.operation.resumed, false);
  assert.equal(recovered.receipt.mutations.observed.purchases, 1);
  assert.match(recovered.receipt.operation.operationId, OWNER_MINTED_IDENTITY);
  assert.match(recovered.receipt.operation.workspaceId, OWNER_MINTED_IDENTITY);

  const lostPlatform = controlPlane({ noBillingReceipt: true });
  const lost = await run(qualificationRequest(TENCENT_TKE_PROVIDER), adapterFor(
    lostPlatform, qualificationRequest(TENCENT_TKE_PROVIDER),
  ));
  assert.equal(lost.state, "unknown");
  assert.equal(lost.receipt.reason, "owner_readback_unavailable");
  assert.equal(lost.receipt.checks.charge_receipted, false);
  assert.equal(lost.receipt.mutations.observed.purchases, 1, "the purchase really was admitted");
});

test("the TKE receipt is redacted and claims no business qualification", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(TENCENT_TKE_PROVIDER);
  const { receipt } = await run(request, adapterFor(platform, request));
  const serialized = JSON.stringify(receipt);

  for (const [label, value] of [
    ["the raw account id", ACCOUNT_ID],
    ["the account email", ACCOUNT_EMAIL],
    ["the cluster", DEPLOYMENT.clusterId],
    ["the region", DEPLOYMENT.region],
    ["the namespace", DEPLOYMENT.namespace],
    ["the owner origin", OWNER_ORIGIN],
    ["the session cookie", "opl_session=qualification"],
    ["the CSRF token", "qualification-csrf"],
    ["the entry URL", ENTRY_URL],
    ["the charge reference", "opl:workspace-purchase:charge"],
  ]) {
    assert.equal(serialized.includes(value), false, label);
  }
  assert.doesNotMatch(serialized, /https?:\/\//, "provider and owner addresses are not receipt fields");
  assert.deepEqual(receipt.uncovered, { ...QUALIFICATION_UNCOVERED_SCOPE });
  assert.equal(receipt.uncovered.businessQualification, false);
  assert.equal(receipt.scope, "workspace_purchase");
  assert.equal(receipt.deploymentHealth, "healthy");
  validateBusinessQualificationReceipt(receipt);
});

test("the Cloud business flow carries no TKE SDK or cluster mutation", async () => {
  const orchestrator = await readFile(
    new URL("../../tools/workspace-qualification-orchestrator.ts", import.meta.url), "utf8",
  );
  const imports = [...orchestrator.matchAll(/from\s+"([^"]+)"/gu)].map((match) => match[1]).sort();
  assert.deepEqual(imports, [
    "./workspace-purchase-core.ts", "./workspace-qualification-contract.ts", "node:crypto",
  ], "the shared flow imports only the contract, the purchase core and inert crypto helpers");
  assert.doesNotMatch(orchestrator, /import\s*\(|\brequire\s*\(/u,
    "the shared flow cannot reach the provider through a dynamic import");

  const adapter = await readFile(
    new URL("../../tools/workspace-qualification-tencent-tke.ts", import.meta.url), "utf8",
  );
  assert.equal((adapter.match(/^import\s/mgu) || []).length, 0,
    "the adapter imports no provider client; every provider operation is injected");
  assert.deepEqual([...TENCENT_TKE_BOUNDARY_OPERATIONS], [
    "establishReachability", "releaseReachability", "readProviderHealth",
  ]);
});
