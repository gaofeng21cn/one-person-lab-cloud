import assert from "node:assert/strict";
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
import { launchOperationId, launchWorkspaceId } from "../../tools/workspace-purchase-core.ts";

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
 * The platform, reduced to the owner API the purchase flow reads. An
 * idempotency key admits one order, so a redelivery is answered from the order
 * already admitted rather than promoted into a second purchase.
 */
function controlPlane({ walletBeforeMicros = "999999999", quoteMicros = QUOTE_MICROS, loseResponseOnce = false,
  noBillingReceipt = false, launchStatus = "succeeded" } = {}) {
  const state = {
    orders: new Map(), byKey: new Map(), posted: 0, charges: [], commands: [],
    wallet: walletBeforeMicros, workspaceId: "", operationId: "", responsesLost: false,
  };
  const envelope = (data) => ({ source: "sub2api", available: true, status: "available", data });

  const http = {
    async request(path) {
      const match = /^\/api\/workspace-launches\/([^/]+)$/.exec(path);
      if (match) {
        const admitted = state.orders.get(decodeURIComponent(match[1]));
        return admitted
          ? { response: { status: 200 }, payload: admitted }
          : { response: { status: 404 }, payload: null };
      }
      throw new Error(`unexpected request ${path}`);
    },
    async json(path, init = {}) {
      if (path === "/api/gateway/wallet") return { status: 200, payload: envelope({ usdMicros: state.wallet }) };
      if (path === "/api/auth/me") {
        return { status: 200, payload: envelope({ accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active" }) };
      }
      if (path.startsWith("/api/workspaces?")) {
        return { status: 200, payload: {
          source: "control-plane", available: true, status: "available",
          data: { items: [{
            id: state.workspaceId, url: ENTRY_URL, openable: true,
            currentApplication: { status: "ready", entryUrl: ENTRY_URL, operationId: APPLICATION_OPERATION_ID },
          }] },
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
      if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
        if (noBillingReceipt) throw new Error(`HTTP GET ${path} returned 404`);
        return { status: 200, payload: {
          source: "ledger", available: true, status: "available",
          data: {
            receiptId: decodeURIComponent(path.split("/").pop()), workspaceId: state.workspaceId,
            operationId: state.operationId,
            type: "billing.workspace_purchased.v1", status: "completed",
            chargeReference: "opl:workspace-launch:charge:0001", totalUsdMicros: quoteMicros,
            fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001" },
          },
        } };
      }
      if (path === "/api/pricing/preview") {
        return { status: 200, payload: { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: quoteMicros } };
      }
      if (path === "/api/workspace-launches" && init.method === "POST") {
        const key = init.headers["idempotency-key"];
        const operationId = launchOperationId(ACCOUNT_ID, key);
        const workspaceId = launchWorkspaceId(ACCOUNT_ID, operationId);
        if (state.byKey.has(key)) {
          return { status: 202, payload: { operationId: state.byKey.get(key), workspaceId } };
        }
        const admitted = {
          operationId, workspaceId, status: launchStatus,
          phase: launchStatus === "succeeded" ? "succeeded" : "debit",
          // The command asks for the full installation; the persisted Launch is
          // the resource-only purchase the receipt rule describes.
          provisioningMode: "resource_only",
          receiptId: launchStatus === "succeeded" ? "rcpt_qualification_1" : "",
          computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001",
        };
        state.byKey.set(key, operationId);
        state.orders.set(operationId, admitted);
        state.workspaceId = workspaceId;
        state.operationId = operationId;
        state.posted += 1;
        state.commands.push(init.body);
        state.wallet = String(BigInt(state.wallet) - BigInt(quoteMicros));
        state.charges.push(quoteMicros);
        if (loseResponseOnce && !state.responsesLost) {
          state.responsesLost = true;
          throw new Error("socket hang up after the platform accepted the command");
        }
        return { status: 202, payload: { operationId, workspaceId } };
      }
      if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
        const admitted = state.orders.get(decodeURIComponent(path.split("/").pop()));
        if (!admitted) throw new Error(`HTTP GET ${path} returned 404`);
        return { status: 200, payload: admitted };
      }
      throw new Error(`unexpected path ${path}`);
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
  assert.equal(platform.state.commands[0].provisioningMode, "full", "the command asks for the full installation");
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

  const failedPlatform = controlPlane({ launchStatus: "failed" });
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
  assert.equal(first.receipt.operation.resumed, false);
  assert.equal(first.receipt.mutations.observed.purchases, 1);
  assert.equal(second.receipt.operation.resumed, true);
  assert.equal(second.receipt.operation.operationId, first.receipt.operation.operationId);
  assert.equal(second.receipt.operation.workspaceId, first.receipt.operation.workspaceId);
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
  assert.equal(recovered.receipt.operation.resumed, false);
  assert.equal(recovered.receipt.mutations.observed.purchases, 1);

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
    ["the charge reference", "opl:workspace-launch:charge"],
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
