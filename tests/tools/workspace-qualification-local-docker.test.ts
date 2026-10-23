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
  LOCAL_DOCKER_BOUNDARY_OPERATIONS,
  LOCAL_DOCKER_PROVIDER,
  createLocalDockerQualificationAdapter,
  localDockerDeployment,
} from "../../tools/workspace-qualification-local-docker.ts";
import { login as harnessLogin, sourceData } from "../../tools/local-workspace-qualification.ts";

// The adapter is driven through the same public orchestrator entry the
// Tencent-TKE flow uses, over the Compose stack the Local-Docker harness already
// builds, reduced to an in-memory fake local stack. Nothing here reaches a
// Docker daemon, a container, a volume, a network or a database: the fake
// Control Plane is the platform's own owner API shape, and the boundary is the
// installation adapter reduced to the four local operations the seam allows.

const SOURCE_SHA = "a".repeat(40);
const SOURCE_TREE = "b".repeat(40);
const ACCOUNT_ID = "acct-customer-0001";
const ACCOUNT_EMAIL = "customer@qualification.invalid";
const QUOTE_MICROS = "52580000";
const ENTRY_URL = "https://workspace.qualification.invalid/";
const APPLICATION_OPERATION_ID = "app-op-0123456789abcdef01";
const PLATFORM_ORIGIN = "http://127.0.0.1:55221";
const COMPOSE_PROJECT = "opl-local-qualification-fixture";
const COMPOSE_FILES = [
  "compose.yaml",
  "deploy/portable/compose.deployment-platform-owned.yaml",
  "deploy/portable/compose.fabric-local-docker.yaml",
  "deploy/portable/compose.local-workspace.yaml",
  "deploy/portable/compose.local-qualification.yaml",
];
const QUALIFICATION_EMAIL = "qualification@qualification.invalid";
const QUALIFICATION_PASSWORD = "fixture-password-not-a-secret";
const SESSION_COOKIE = "opl_session=qualification";
const CSRF_TOKEN = "qualification-csrf";

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

// The harness's own envelope validator is the contract the fake answers. A fake
// that drifted from the real `{source, available, status, data}` shape would
// fail here rather than pass an owner readback the platform never sends.
function envelope(source, data) {
  const value = { source, available: true, status: "available", data };
  sourceData(value, source);
  return value;
}

function result(payload, { status = 200, headers = {} } = {}) {
  return {
    status,
    payload,
    text: JSON.stringify(payload),
    response: { status, headers: new Headers(headers) },
  };
}

/*
 * The platform, reduced to the owner API the purchase flow reads. An
 * idempotency key admits one order, so a redelivery is answered from the order
 * already admitted rather than promoted into a second purchase.
 */
function controlPlane({ walletBeforeMicros = "999999999", quoteMicros = QUOTE_MICROS, loseResponseOnce = false,
  billingReceiptUnavailable = false, operationStatus = "succeeded" } = {}) {
  const state = {
    orders: new Map(), byKey: new Map(), posted: 0, charges: [], commands: [], quotes: [],
    wallet: walletBeforeMicros, workspaceId: "", operationId: "", responsesLost: false,
    pageReads: 0, logins: 0, minted: 0, retainedReads: 0, ownerReads: 0,
  };

  // One shape for the owner operation, so both seams answer the same operation.
  const ownerOperationPayload = (admitted) => ({
    operationId: admitted.operationId, owner: "workspace", kind: "create_workspace",
    resourceId: admitted.workspaceId, status: admitted.status, stage: admitted.stage,
    requestId: admitted.requestId,
  });

  // The owner mints the operation identity and the Workspace identity of every
  // order it admits, so the fixture mints them instead of deriving them.
  const mintOwnerIdentity = () => {
    state.minted += 1;
    const ordinal = String(state.minted).padStart(4, "0");
    return { operationId: `workspace-operation-${ordinal}`, workspaceId: `ws-owner-${ordinal}` };
  };

  const http = {
    async request(path) {
      // A successful Console page load is deliberately served here, because it
      // is the thing deployment health must not be inferred from.
      if (path === "/") {
        state.pageReads += 1;
        return { response: { status: 200 }, payload: null, text: '<div id="root"></div>' };
      }
      const match = /^\/api\/workspace-launches\/([^/]+)$/.exec(path);
      if (match) {
        state.retainedReads += 1;
        const admitted = state.orders.get(decodeURIComponent(match[1]));
        return admitted
          ? { response: { status: 200 }, payload: admitted }
          : { response: { status: 404 }, payload: null };
      }
      const ownerOperation = /^\/api\/v2\/operations\/workspace\/([^/]+)$/.exec(path);
      if (ownerOperation) {
        state.ownerReads += 1;
        const admitted = state.orders.get(decodeURIComponent(ownerOperation[1]));
        return admitted
          ? { response: { status: 200 }, payload: ownerOperationPayload(admitted) }
          : { response: { status: 404 }, payload: null };
      }
      throw new Error(`unexpected request ${path}`);
    },
    async json(path, init = {}) {
      if (path === "/api/auth/login" && init.method === "POST") {
        state.logins += 1;
        if (init.body?.email !== QUALIFICATION_EMAIL || init.body?.password !== QUALIFICATION_PASSWORD) {
          throw new Error("fixture login credentials are invalid");
        }
        return result({ user: { accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active" } }, {
          headers: { "set-cookie": SESSION_COOKIE, "x-opl-csrf-token": CSRF_TOKEN },
        });
      }
      if (path === "/api/gateway/wallet") return result(envelope("sub2api", { usdMicros: state.wallet }));
      if (path === "/api/auth/me") {
        return result(envelope("sub2api", {
          accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active", sub2apiUserId: "9001",
        }));
      }
      if (path.startsWith("/api/workspaces?")) {
        return result(envelope("control-plane", {
          items: [{
            id: state.workspaceId, url: ENTRY_URL, openable: true,
            currentApplication: { status: "ready", entryUrl: ENTRY_URL, operationId: APPLICATION_OPERATION_ID },
          }],
          total: 1, page: 1, pageSize: 20,
        }));
      }
      if (/^\/api\/workspaces\/[^/]+\/runtime-status$/.test(path)) {
        return result(envelope("fabric", {
          workspaceId: state.workspaceId, ready: true, status: "running", url: ENTRY_URL,
          runtimeId: "rt-0123456789abcdef", checks: [{ name: "console", ok: true }, { name: "gateway", ok: true }],
          currentApplication: { operationId: APPLICATION_OPERATION_ID },
        }));
      }
      if (path.startsWith("/api/billing/receipts?")) {
        // The receipt of an owner order is found in the Ledger's own inventory,
        // so an unavailable inventory is an unavailable readback.
        if (billingReceiptUnavailable) throw new Error(`HTTP GET ${path} returned 404`);
        return result(envelope("ledger", {
          receipts: [{
            receiptId: "rcpt_qualification_1", workspaceId: state.workspaceId, operationId: state.operationId,
            type: "billing.workspace_purchased.v1", status: "completed",
            chargeReference: "opl:workspace-launch:charge:0001", totalUsdMicros: quoteMicros,
            fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001" },
          }],
          hasMore: false,
        }));
      }
      if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
        if (billingReceiptUnavailable) throw new Error(`HTTP GET ${path} returned 404`);
        return result(envelope("ledger", {
          receiptId: decodeURIComponent(path.split("/").pop()), workspaceId: state.workspaceId,
          operationId: state.operationId,
          type: "billing.workspace_purchased.v1", status: "completed",
          chargeReference: "opl:workspace-launch:charge:0001", totalUsdMicros: quoteMicros,
          fulfillment: { computeAllocationId: "ca-0001", storageId: "st-0001", attachmentId: "at-0001" },
        }));
      }
      // The owner owns the catalog a buyer prices from. The Console reads it the
      // same way: the first approved Runtime Release, the first available
      // compute plan and the first available storage plan.
      if (path.startsWith("/api/v2/catalog/runtime-versions")) {
        return result({ items: [{ id: "rv-qualification", status: "approved" }] });
      }
      if (path.startsWith("/api/v2/catalog/compute-plans")) {
        return result({ items: [{ id: "cp-qualification", availability: "available" }] });
      }
      if (path.startsWith("/api/v2/catalog/storage-plans")) {
        return result({ items: [{ id: "sp-qualification", availability: "available" }] });
      }
      if (path === "/api/v2/quotes" && init.method === "POST") {
        state.quotes.push({ body: init.body, idempotencyKey: init.headers["idempotency-key"] });
        return result({
          id: "quote_qualification_1", purpose: "deploy", runtimeVersionId: "rv-qualification",
          computePlanId: "cp-qualification", storagePlanId: "sp-qualification",
          modelSelections: [], periodMonths: 1, totalUSDMicros: quoteMicros, status: "offered",
        }, { status: 201 });
      }
      if (path === "/api/v2/workspaces" && init.method === "POST") {
        const key = init.headers["idempotency-key"];
        // An idempotency key admits one order, so a redelivery is answered from
        // the order already admitted rather than promoted into a second purchase.
        if (state.byKey.has(key)) {
          const replayed = state.byKey.get(key);
          return result({
            operationId: replayed.operationId, resourceId: replayed.workspaceId,
            owner: "workspace", kind: "create_workspace", status: "accepted", stage: "creating",
            requestId: replayed.requestId,
          }, { status: 202 });
        }
        const { operationId, workspaceId } = mintOwnerIdentity();
        const admitted = {
          operationId, workspaceId, requestId: String(init.headers["x-opl-request-id"] || ""),
          status: operationStatus === "succeeded" ? "succeeded" : operationStatus,
          stage: operationStatus === "succeeded" ? "succeeded" : "creating",
        };
        state.byKey.set(key, admitted);
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
        return result({
          operationId, resourceId: workspaceId, owner: "workspace", kind: "create_workspace",
          status: "accepted", stage: "creating", requestId: admitted.requestId,
        }, { status: 202 });
      }
      if (/^\/api\/v2\/operations\/workspace\/[^/]+$/.test(path)) {
        state.ownerReads += 1;
        const admitted = state.orders.get(decodeURIComponent(path.split("/").pop()));
        if (!admitted) throw new Error(`HTTP GET ${path} returned 404`);
        return result(ownerOperationPayload(admitted));
      }
      throw new Error(`unexpected path ${path}`);
    },
  };
  return { http, state };
}

/*
 * The installation boundary reduced to typed answers. `login` is the real
 * harness login flow against the fake platform, so the session shape this
 * adapter validates is the one the local qualification harness actually
 * produces. The boundary holds the fixture credentials; the adapter never sees
 * them.
 */
function localStack(platform, {
  provider = LOCAL_DOCKER_PROVIDER,
  project = COMPOSE_PROJECT,
  up = "ready",
  upProvider = LOCAL_DOCKER_PROVIDER,
  upProject = project,
  upThrows = false,
  login = "authenticated",
  loginProvider = LOCAL_DOCKER_PROVIDER,
  loginProject = project,
  loginThrows = false,
  transport = null,
  release = "released",
  releaseProvider = LOCAL_DOCKER_PROVIDER,
  releaseProject = project,
  releaseThrows = false,
  health = "ready",
  healthProvider = LOCAL_DOCKER_PROVIDER,
  healthProject = project,
  healthThrows = false,
  running = true,
  serviceReady = true,
  failedChecks = [],
} = {}) {
  const calls = { up: 0, login: 0, down: 0, health: 0, order: [] };
  const identity = (resultProvider = provider, resultProject = project) => ({
    provider: resultProvider, composeProject: resultProject,
  });
  return {
    provider,
    calls,
    async up() {
      calls.up += 1;
      calls.order.push("up");
      if (upThrows) throw new Error("compose up failed before its readback");
      if (up === "absent") return { state: "absent", reason: "stack_absent", ...identity(upProvider, upProject) };
      if (up === "unknown") return { state: "unknown", reason: "readback_unavailable", ...identity(upProvider, upProject) };
      return { state: "ready", ...identity(upProvider, upProject) };
    },
    async down() {
      calls.down += 1;
      calls.order.push("down");
      if (releaseThrows) throw new Error("compose down readback failed");
      return { state: release, ...identity(releaseProvider, releaseProject) };
    },
    async readProviderHealth() {
      calls.health += 1;
      calls.order.push("health");
      if (healthThrows) throw new Error("provider health readback failed");
      return { state: health, ...identity(healthProvider, healthProject), running, serviceReady, failedChecks };
    },
    async login() {
      calls.login += 1;
      calls.order.push("login");
      if (loginThrows) throw new Error("local qualification login failed");
      if (login === "absent") return { state: "absent", reason: "stack_absent", ...identity(loginProvider, loginProject) };
      if (login === "unknown") return { state: "unknown", reason: "session_unavailable", ...identity(loginProvider, loginProject) };
      const session = await harnessLogin(platform.http, QUALIFICATION_EMAIL, QUALIFICATION_PASSWORD, ACCOUNT_ID);
      return {
        state: "authenticated",
        ...identity(loginProvider, loginProject),
        transport: transport ?? { origin: PLATFORM_ORIGIN, http: platform.http, auth: session },
      };
    },
  };
}

const deploymentFor = (request, overrides = {}) => ({
  candidateReceiptDigest: request.candidate.receiptDigest,
  composeProject: COMPOSE_PROJECT,
  composeFiles: [...COMPOSE_FILES],
  platformOrigin: PLATFORM_ORIGIN,
  accountDigest: request.tenant.accountDigest,
  sourceTree: request.source.tree,
  ...overrides,
});
const adapterFor = (platform, request, options) => createLocalDockerQualificationAdapter({
  deployment: deploymentFor(request),
  boundary: localStack(platform, options),
});
const run = (request, adapter) => runWorkspaceQualification({ request, adapter }, { wait: async () => {} });

test("the Local-Docker adapter satisfies the shared seam and settles the shared flow", async () => {
  assert.equal(LOCAL_DOCKER_PROVIDER, "local-docker");
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const boundary = localStack(platform);
  const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });

  assert.deepEqual(Object.keys(adapter).sort(), ["closeBusinessScope", "openBusinessScope", "provider", "readDeploymentHealth"]);
  assertQualificationAdapter(adapter, LOCAL_DOCKER_PROVIDER);
  assert.throws(() => assertQualificationAdapter(adapter, "tencent-tke"), {
    code: "qualification_adapter_provider_mismatch",
  });

  const { state, receipt } = await run(request, adapter);
  assert.equal(state, "settled");
  assert.equal(receipt.provider, LOCAL_DOCKER_PROVIDER);
  assert.equal(receipt.state, "settled");
  assert.ok(QUALIFICATION_BUSINESS_CHECKS.every((name) => receipt.checks[name] === true));
  assert.equal(platform.state.posted, 1, "one owner order for one declared qualification");
  // The order names the quote the owner priced and asks for manual renewal,
  // and the quote froze the application selection and the plan pair it prices.
  assert.equal(platform.state.commands[0].quoteId, "quote_qualification_1", "the order names the quote the owner priced");
  assert.equal(platform.state.commands[0].renewalMode, "manual", "a first deployment asks for manual renewal");
  assert.deepEqual(platform.state.quotes[0].body.applicationSelection, { kind: "opl_app", runtimeVersionId: "rv-qualification" });
  assert.equal(platform.state.quotes[0].body.computePlanId, "cp-qualification");
  assert.equal(platform.state.quotes[0].body.storagePlanId, "sp-qualification");
  // The owner mints the operation identity and the Workspace identity of the
  // purchase, and the order is read back under the identity it minted.
  assert.ok(platform.state.operationId, "the owner minted the operation identity");
  assert.ok(platform.state.workspaceId, "the owner minted the Workspace identity");
  assert.ok(platform.state.ownerReads >= 1, "the owner operation was read back");
  assert.equal(platform.state.retainedReads, 0, "a new purchase is not read back through the retained launch route");
  assert.equal(boundary.calls.up, 1);
  assert.equal(boundary.calls.login, 1, "the authenticated transport comes from the login flow");
  assert.equal(boundary.calls.down, 1, "the boundary that started the stack closes it");
  assert.equal(boundary.calls.health, 1);
  assert.deepEqual(boundary.calls.order, ["up", "login", "health", "down"],
    "health is read before the stack is released");
  assert.equal(platform.state.pageReads, 0, "no Console page load participates in the result");
});

test("a Local-Docker receipt has exactly the shared business receipt shape", async () => {
  const localPlatform = controlPlane();
  const localRequest = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const local = await run(localRequest, adapterFor(localPlatform, localRequest));

  const tkePlatform = controlPlane();
  const tkeRequest = qualificationRequest("tencent-tke");
  const tkeAdapter = {
    provider: "tencent-tke",
    async openBusinessScope() {
      return { http: tkePlatform.http, auth: { cookie: "opl_session=tke", csrf: "tke-csrf" } };
    },
    async closeBusinessScope() { return { restored: true }; },
    async readDeploymentHealth() { return "healthy"; },
  };
  const tke = await run(tkeRequest, tkeAdapter);

  // The shape, not the values: every block and nested block must have the same
  // keys and value kinds for both providers, because one contract owns it.
  const shape = (value) => Object.fromEntries(Object.entries(value).map(([key, item]) => [
    key,
    Array.isArray(item) ? "array" : item && typeof item === "object" ? shape(item) : typeof item,
  ]).sort(([left], [right]) => left.localeCompare(right)));
  assert.deepEqual(shape(local.receipt), shape(tke.receipt));
  assert.equal(local.receipt.provider, LOCAL_DOCKER_PROVIDER);
  assert.equal(tke.receipt.provider, "tencent-tke");
  assert.equal(local.receipt.state, "settled");
  assert.equal(tke.receipt.state, "settled");
});

test("the adapter refuses a boundary that is not the exact Local-Docker provider", () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const deployment = deploymentFor(request);

  assert.throws(
    () => createLocalDockerQualificationAdapter({ deployment, boundary: localStack(platform, { provider: "tencent-tke" }) }),
    { code: "local_docker_boundary_invalid" },
  );
  for (const operation of LOCAL_DOCKER_BOUNDARY_OPERATIONS) {
    const boundary = localStack(platform);
    delete boundary[operation];
    assert.throws(() => createLocalDockerQualificationAdapter({ deployment, boundary }), {
      code: "local_docker_boundary_invalid",
    }, operation);
  }
});

test("the deployment binding is closed and names one local Compose stack", () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const deployment = deploymentFor(request);
  assert.deepEqual([...LOCAL_DOCKER_BOUNDARY_OPERATIONS], ["up", "down", "readProviderHealth", "login"]);

  for (const [label, overrides] of [
    ["an extra field", { credential: "must-not-travel" }],
    ["a missing source tree", { sourceTree: undefined }],
    ["a Candidate digest that is not one", { candidateReceiptDigest: "not-a-digest" }],
    ["a tenant digest that is not one", { accountDigest: "not-a-digest" }],
    ["an uppercase source tree", { sourceTree: SOURCE_TREE.toUpperCase() }],
    ["no Compose project", { composeProject: "" }],
    ["an invalid Compose project", { composeProject: "Opl_Project" }],
    ["a Compose project starting with a dash", { composeProject: "-opl" }],
    ["a Compose project that is too long", { composeProject: "a".repeat(64) }],
    ["no Compose files", { composeFiles: [] }],
    ["a Compose file list that is not a list", { composeFiles: "compose.yaml" }],
    ["an absolute Compose file", { composeFiles: ["/compose.yaml"] }],
    ["a traversing Compose file", { composeFiles: ["deploy/../compose.yaml"] }],
    ["a non-Compose file", { composeFiles: ["compose.txt"] }],
    ["a duplicated Compose file", { composeFiles: ["compose.yaml", "compose.yaml"] }],
    ["a platform origin with a path", { platformOrigin: `${PLATFORM_ORIGIN}/console` }],
    ["a platform origin with credentials", { platformOrigin: "http://user:pass@127.0.0.1:55221" }],
    ["a platform origin with a query", { platformOrigin: `${PLATFORM_ORIGIN}?debug=1` }],
  ]) {
    const candidate = { ...deployment, ...overrides };
    if (overrides.sourceTree === undefined) delete candidate.sourceTree;
    assert.throws(
      () => createLocalDockerQualificationAdapter({ deployment: candidate, boundary: localStack(platform) }),
      { code: "local_docker_deployment_invalid" }, label,
    );
  }

  const normalized = localDockerDeployment({ ...deployment, platformOrigin: `${PLATFORM_ORIGIN}/`, composeFiles: ["compose.yml"] });
  assert.equal(normalized.platformOrigin, PLATFORM_ORIGIN);
  assert.equal(Object.isFrozen(normalized), true);
  assert.equal(Object.isFrozen(normalized.composeFiles), true);
  assert.equal(normalized.composeProject, COMPOSE_PROJECT);
  assert.equal(localDockerDeployment(deployment).candidateReceiptDigest, request.candidate.receiptDigest);
});

test("a request for another provider or another installation cannot use this adapter", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const adapter = adapterFor(platform, request);

  await assert.rejects(adapter.openBusinessScope({ request: qualificationRequest("tencent-tke") }), {
    code: "local_docker_adapter_provider_mismatch",
  });

  for (const [label, overrides] of [
    ["another Candidate", { candidateReceiptDigest: `sha256:${"9".repeat(64)}` }],
    ["another tenant", { accountDigest: `sha256:${"8".repeat(64)}` }],
    ["another source tree", { sourceTree: "c".repeat(40) }],
  ]) {
    const otherInstallation = createLocalDockerQualificationAdapter({
      deployment: deploymentFor(request, overrides),
      boundary: localStack(platform),
    });
    await assert.rejects(otherInstallation.openBusinessScope({ request }), {
      code: "local_docker_deployment_mismatch",
    }, label);
  }
  assert.equal(platform.state.posted, 0, "no purchase is issued for a mismatched installation");
});

test("stack absence, an unknown readback and a failed compose up are a blocked scope, never a purchase", async () => {
  for (const up of ["absent", "unknown", "throw"]) {
    const platform = controlPlane();
    const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
    const boundary = localStack(platform, { up, upThrows: up === "throw" });
    const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });

    const { state, receipt } = await run(request, adapter);
    assert.equal(state, "blocked", up);
    assert.equal(receipt.reason, "provider_scope_unavailable", up);
    assert.equal(receipt.checks.account_bound, false, "no owner readback was attempted");
    assert.equal(boundary.calls.login, 0, "a stack that never came up is never logged in to");
    assert.equal(platform.state.posted, 0, "a blocked scope buys nothing");
    assert.equal(boundary.calls.down, 1, "the attempted open is put back");
    assert.equal(boundary.calls.health, 1, "the attempted scope's health is still recorded");
    assert.equal(receipt.cleanup.performed, true);
    assert.equal(receipt.cleanup.restored, true, "the release readback proved nothing was left");
  }
});

test("a stack, session or readback naming another provider or Compose project is refused", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  for (const [label, options, restored, loginCalls] of [
    ["another up provider", { upProvider: "tencent-tke" }, true, 0],
    ["another Compose project at up", { upProject: "opl-somewhere-else" }, true, 0],
    ["another login provider", { loginProvider: "tencent-tke" }, true, 1],
    ["another Compose project at login", { loginProject: "opl-somewhere-else" }, true, 1],
  ]) {
    const boundary = localStack(platform, options);
    const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });
    await assert.rejects(adapter.openBusinessScope({ request }), { code: "local_docker_readback_invalid" }, label);
    assert.equal(boundary.calls.login, loginCalls, `${label}: the stack is not asked for a session it cannot bind`);
    assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored }, label);
    assert.equal(boundary.calls.down, 1, `${label}: a refused open is put back`);
  }
  assert.equal(platform.state.posted, 0);
});

test("a transport bound to a foreign origin or carrying no session is refused", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const session = { cookie: SESSION_COOKIE, csrf: CSRF_TOKEN };
  for (const [label, transport] of [
    ["another origin", { origin: "https://other.qualification.invalid", http: platform.http, auth: session }],
    ["an origin with a path", { origin: `${PLATFORM_ORIGIN}/api`, http: platform.http, auth: session }],
    ["no session", { origin: PLATFORM_ORIGIN, http: platform.http, auth: {} }],
    ["no cookie", { origin: PLATFORM_ORIGIN, http: platform.http, auth: { cookie: "", csrf: CSRF_TOKEN } }],
    ["no CSRF token", { origin: PLATFORM_ORIGIN, http: platform.http, auth: { cookie: SESSION_COOKIE, csrf: "" } }],
    ["no http client", { origin: PLATFORM_ORIGIN, auth: session }],
  ]) {
    const boundary = localStack(platform, { transport });
    const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });
    await assert.rejects(adapter.openBusinessScope({ request }), { code: "local_docker_transport_invalid" }, label);
    assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true }, label);
    assert.equal(boundary.calls.down, 1, `${label}: a refused open is put back`);
  }
  assert.equal(platform.state.posted, 0);
});

test("an open that fails after starting the stack still releases the attempt through the orchestrator", async () => {
  const cases = [
    ["a lost compose answer", { upThrows: true }],
    ["a login failure after up", { loginThrows: true }],
    ["a session that never authenticated", { login: "unknown" }],
    ["an absent session", { login: "absent" }],
  ];
  for (const [label, options] of cases) {
    const platform = controlPlane();
    const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
    const boundary = localStack(platform, options);
    const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });

    const { state, receipt } = await run(request, adapter);
    assert.equal(boundary.calls.up, 1, label);
    assert.equal(boundary.calls.down, 1, `${label}: the attempted open is put back`);
    assert.equal(receipt.cleanup.performed, true, label);
    assert.equal(receipt.cleanup.restored, true, label);
    assert.equal(state, "blocked", label);
    assert.equal(receipt.reason, "provider_scope_unavailable", label);
    assert.equal(platform.state.posted, 0, label);
  }

  // The attempt is recorded before the stack is asked anything, so a direct
  // close after a failed open still releases it.
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const boundary = localStack(platform, { upThrows: true });
  const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });
  await assert.rejects(adapter.openBusinessScope({ request }), { code: "provider_scope_unavailable" });
  assert.equal(boundary.calls.up, 1);
  assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true });
  assert.equal(boundary.calls.down, 1);

  // The release readback, not the failed open, decides whether the attempt was
  // put back. An unknown release cannot be read as restored.
  const unprovenPlatform = controlPlane();
  const unproven = await run(qualificationRequest(LOCAL_DOCKER_PROVIDER), adapterFor(
    unprovenPlatform, qualificationRequest(LOCAL_DOCKER_PROVIDER), { loginThrows: true, release: "unknown" },
  ));
  assert.equal(unproven.state, "recovery-required");
  assert.equal(unproven.receipt.reason, "cleanup_unproven");
  assert.equal(unproven.receipt.cleanup.restored, false);
});

test("deployment health maps the provider's own facts and never the page", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const health = async (options) => {
    const adapter = createLocalDockerQualificationAdapter({
      deployment: deploymentFor(request), boundary: localStack(platform, options),
    });
    return adapter.readDeploymentHealth({ request });
  };

  assert.equal(await health({ health: "ready", running: true, serviceReady: true, failedChecks: [] }), "healthy");
  assert.equal(await health({ health: "ready", running: true, serviceReady: true, failedChecks: ["control-plane"] }), "degraded");
  assert.equal(await health({ health: "ready", running: true, serviceReady: false, failedChecks: [] }), "degraded");
  assert.equal(await health({ health: "degraded", running: true, serviceReady: false, failedChecks: ["fabric"] }), "degraded");
  assert.equal(await health({ health: "ready", running: false }), "unknown");
  assert.equal(await health({ health: "degraded", running: false }), "unknown");
  assert.equal(await health({ health: "absent" }), "unknown");
  assert.equal(await health({ health: "unknown" }), "unknown");
  assert.equal(await health({ healthThrows: true }), "unknown");
  assert.equal(await health({ healthProvider: "tencent-tke" }), "unknown");
  assert.equal(await health({ healthProject: "opl-somewhere-else" }), "unknown");
  assert.equal(await health({ serviceReady: 1 }), "unknown");
  assert.equal(await health({ failedChecks: ["not-a-string", 7] }), "unknown");
  assert.equal(platform.state.pageReads, 0, "the Console page is never read for health");

  const degradedRun = await run(request, adapterFor(platform, request, {
    health: "ready", running: true, serviceReady: true, failedChecks: ["ledger"],
  }));
  assert.equal(degradedRun.state, "settled", "a degraded host does not unqualify a proven product");
  assert.equal(degradedRun.receipt.deploymentHealth, "degraded");

  const absentRun = await run(request, adapterFor(platform, request, { health: "absent" }));
  assert.equal(absentRun.receipt.deploymentHealth, "unknown", "a reachable page is not a healthy stack");
  assert.equal(platform.state.pageReads, 0);

  const failedPlatform = controlPlane({ operationStatus: "failed" });
  const failedRun = await run(request, adapterFor(failedPlatform, request, { health: "ready" }));
  assert.equal(failedRun.state, "failed", "a healthy host does not make a failed business run pass");
  assert.equal(failedRun.receipt.deploymentHealth, "healthy");
});

test("a scope is only reported restored when the release readback proves it", async () => {
  for (const [release, expectedState, expectedReason, restored] of [
    ["released", "settled", "none", true],
    ["absent", "settled", "none", true],
    ["unknown", "recovery-required", "cleanup_unproven", false],
  ]) {
    const platform = controlPlane();
    const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
    const { state, receipt } = await run(request, adapterFor(platform, request, { release }));
    assert.equal(state, expectedState, release);
    assert.equal(receipt.reason, expectedReason, release);
    assert.equal(receipt.cleanup.performed, true, release);
    assert.equal(receipt.cleanup.restored, restored, release);
    assert.ok(QUALIFICATION_BUSINESS_CHECKS.every((name) => receipt.checks[name]), release);
  }

  const throwingPlatform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const thrown = await run(request, adapterFor(throwingPlatform, request, { releaseThrows: true }));
  assert.equal(thrown.state, "recovery-required");
  assert.equal(thrown.receipt.reason, "cleanup_unproven");

  const neverOpened = adapterFor(controlPlane(), request);
  assert.deepEqual(await neverOpened.closeBusinessScope({ request }), { restored: true },
    "nothing this adapter opened is left behind");

  for (const options of [{ releaseProvider: "tencent-tke" }, { releaseProject: "opl-somewhere-else" }]) {
    const mismatched = await run(request, adapterFor(controlPlane(), request, options));
    assert.equal(mismatched.state, "recovery-required",
      "a close readback from another stack cannot prove this scope was restored");
    assert.equal(mismatched.receipt.reason, "cleanup_unproven");
  }
});

test("repeated open and close calls are idempotent under the shared operation identity", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const boundary = localStack(platform);
  const adapter = createLocalDockerQualificationAdapter({ deployment: deploymentFor(request), boundary });

  const first = await adapter.openBusinessScope({ request });
  const second = await adapter.openBusinessScope({ request });
  assert.equal(second, first, "a retry reuses the same scope");
  assert.deepEqual(Object.keys(second).sort(), ["auth", "http"], "no installation fact leaks into the scope");
  assert.equal(second.http, platform.http, "the boundary's transport is used as given");
  assert.equal(second.auth.cookie, SESSION_COOKIE, "the harness login session is used as given");
  assert.equal(second.auth.csrf, CSRF_TOKEN);
  assert.equal(boundary.calls.up, 1, "one stack start per operation key");
  assert.equal(boundary.calls.login, 1, "one login per operation key");

  assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true });
  assert.deepEqual(await adapter.closeBusinessScope({ request }), { restored: true });
  assert.equal(boundary.calls.down, 1, "one stack release per operation key");

  // A redelivery after a proven close opens the same operation key again; the
  // earlier close cannot be mistaken for the new scope's.
  await adapter.openBusinessScope({ request });
  assert.equal(boundary.calls.up, 2);
  assert.equal(boundary.calls.login, 2);
  assert.equal((await adapter.closeBusinessScope({ request })).restored, true);
  assert.equal(boundary.calls.down, 2);
});

test("a redelivery resumes the original Local-Docker purchase and buys nothing twice", async () => {
  const platform = controlPlane();
  const firstRequest = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const secondRequest = qualificationRequest(LOCAL_DOCKER_PROVIDER);
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
  const recovered = await run(qualificationRequest(LOCAL_DOCKER_PROVIDER), adapterFor(
    recoveredPlatform, qualificationRequest(LOCAL_DOCKER_PROVIDER),
  ));
  assert.equal(recovered.state, "settled");
  assert.equal(recoveredPlatform.state.posted, 1, "the lost answer cost a retry of the read, never a second purchase");
  assert.equal(recovered.receipt.operation.resumed, false);
  assert.equal(recovered.receipt.mutations.observed.purchases, 1);

  const lostPlatform = controlPlane({ billingReceiptUnavailable: true });
  const lost = await run(qualificationRequest(LOCAL_DOCKER_PROVIDER), adapterFor(
    lostPlatform, qualificationRequest(LOCAL_DOCKER_PROVIDER),
  ));
  assert.equal(lost.state, "unknown");
  assert.equal(lost.receipt.reason, "owner_readback_unavailable");
  assert.equal(lost.receipt.checks.charge_receipted, false);
  assert.equal(lost.receipt.mutations.observed.purchases, 1, "the purchase really was admitted");
});

test("the Local-Docker receipt is redacted, Candidate-bound and claims no business qualification", async () => {
  const platform = controlPlane();
  const request = qualificationRequest(LOCAL_DOCKER_PROVIDER);
  const { receipt } = await run(request, adapterFor(platform, request));
  const serialized = JSON.stringify(receipt);

  for (const [label, value] of [
    ["the raw account id", ACCOUNT_ID],
    ["the account email", ACCOUNT_EMAIL],
    ["the qualification email", QUALIFICATION_EMAIL],
    ["the qualification password", QUALIFICATION_PASSWORD],
    ["the Compose project", COMPOSE_PROJECT],
    ["a Compose file", COMPOSE_FILES[0]],
    ["the platform origin", PLATFORM_ORIGIN],
    ["the session cookie", SESSION_COOKIE],
    ["the CSRF token", CSRF_TOKEN],
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
  assert.equal(receipt.candidate.receiptDigest, request.candidate.receiptDigest,
    "the receipt stays bound to the exact declared Candidate");
  validateBusinessQualificationReceipt(receipt);
});

test("the Cloud business flow carries no Local-Docker command and this adapter no provider client", async () => {
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
    new URL("../../tools/workspace-qualification-local-docker.ts", import.meta.url), "utf8",
  );
  const adapterImports = [...adapter.matchAll(/from\s+"([^"]+)"/gu)].map((match) => match[1]).sort();
  assert.deepEqual(adapterImports, ["./workspace-qualification-contract.ts"],
    "the adapter imports only the shared contract; every local operation is injected");
  for (const forbidden of [
    /node:child_process/u, /\bprocess\.env\b/u, /\bfetch\s*\(/u,
    /\bspawn\s*\(/u, /\bexec\s*\(/u, /\bcompose\s+up\b/u,
  ]) {
    assert.doesNotMatch(adapter, forbidden, "the adapter holds no provider command, credential or environment");
  }
  assert.doesNotMatch(adapter, /accountId/u, "the adapter never decodes the raw account identity");
  assert.deepEqual([...LOCAL_DOCKER_BOUNDARY_OPERATIONS], ["up", "down", "readProviderHealth", "login"]);
});
