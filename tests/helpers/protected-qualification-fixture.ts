/*
 * Fixture platform and provider boundary for the protected business
 * qualification producer tests.
 *
 * This is the owner-API shape reduced to the reads and the one purchase the
 * shared orchestrator performs. It is a test fixture only: it reaches no
 * network, cluster or database, and it is not a second production path.
 */

import { createHash } from "node:crypto";

import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import type { TencentTkeProviderBoundary } from "../../tools/workspace-qualification-tencent-tke.ts";

export const SOURCE_SHA = "a".repeat(40);
export const SOURCE_TREE = "b".repeat(40);
export const ACCOUNT_ID = "acct-customer-0001";
export const ACCOUNT_EMAIL = "customer@qualification.invalid";
export const QUOTE_MICROS = "52580000";
export const ENTRY_URL = "https://workspace.qualification.invalid/";
export const APPLICATION_OPERATION_ID = "app-op-0123456789abcdef01";
export const OWNER_ORIGIN = "https://cloud.qualification.invalid";
export const CLUSTER_ID = "cls-qualification0001";
export const REGION = "ap-guangzhou";
export const NAMESPACE = "tke-qualification";
export const INSTANCE_SHA = "f".repeat(40);
// The Candidate's own Cloud image digest. The J0 READY receipt carries it so a
// sandbox run for the same source but another image is another Candidate.
export const CLOUD_IMAGE_DIGEST = "sha256:" + "1".repeat(64);

// The catalog the Workspace owner offers. The buyer declares which of these it
// buys, so the declaration names one of them and the owner confirms it still
// offers it.
export const OWNER_RUNTIME_VERSION_ID = `rtv_${"a".repeat(32)}`;
export const OWNER_COMPUTE_PLAN_ID = `cmp_${"b".repeat(32)}`;
export const OWNER_STORAGE_PLAN_ID = `stp_${"c".repeat(32)}`;

// The Gateway user the customer account is. The Control Plane session answers
// with it, and the identity owner's session answers with the same actor, so the
// two sessions can be shown to belong to one customer.
export const ACCOUNT_SUB2API_USER_ID = "10001";
export const ACCOUNT_TENANT_ID = "tenant-customer-0001";
export const OWNER_CSRF_TOKEN = "owner-csrf-token";
export const OWNER_SESSION_EXPIRES_AT = "2026-01-01T00:00:00Z";

// The identities the owner mints. The fixture never derives them from the
// declared idempotency identity: the owner mints the operation and the
// Workspace, and the buyer reads them back.
//
// They are minted in a shape the public contract does not describe, because the
// contract publishes them as strings and says nothing about their form. A
// buyer that only accepted one shape would refuse a correct answer whenever
// the owner changed how it mints one, so the fixture proves the buyer treats
// them as opaque.
const ownerID = (prefix: string, ...parts: string[]) => {
  const digest = createHash("sha256").update(parts.join(":")).digest("hex");
  return `${prefix}-${digest.slice(0, 8)}-${digest.slice(8, 12)}-${digest.slice(12, 16)}-${digest.slice(16, 28)}`;
};

export function candidateReceipt(overrides = {}) {
  return {
    schemaVersion: 2,
    kind: "opl_cloud_candidate",
    product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: SOURCE_SHA, tree: SOURCE_TREE },
    cloudImage: {
      repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
      indexRef: "ghcr.io/gaofeng21cn/one-person-lab-cloud@" + CLOUD_IMAGE_DIGEST,
      indexDigest: CLOUD_IMAGE_DIGEST,
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

export function installationReceipt(candidate: any) {
  return {
    schemaVersion: 2,
    operationMode: "deployment_verification",
    environment: "production",
    status: "deployment_verified",
    stage: "complete",
    errorCode: "none",
    candidateReceiptDigest: candidateReceiptDigest(candidate),
    productSha: candidate.product.sha,
    productTree: candidate.product.tree,
    cloudImageRef: candidate.cloudImage.indexRef,
    cloudImageDigest: candidate.cloudImage.indexDigest,
    cloudImageRevision: candidate.cloudImage.revision,
    cloudRuntimeChildDigest: candidate.cloudImage.platforms.find((entry: any) => entry.platform === "linux/amd64").digest,
  };
}

export function installationRun() {
  return {
    id: 22,
    run_attempt: 1,
    repository: { full_name: "gaofeng21cn/opl-instance-medopl" },
    path: ".github/workflows/deployment-verify.yml",
    event: "workflow_dispatch",
    head_branch: "main",
    head_sha: INSTANCE_SHA,
    status: "completed",
    conclusion: "success",
  };
}

export function j0ReadyReceipt(provider = "tencent-tke") {
  return {
    schemaVersion: 1,
    kind: "opl.workspace-qualification.j0-ready.v2",
    status: "READY",
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, cloudImageDigest: CLOUD_IMAGE_DIGEST, clean: true },
    authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
    provider,
    failed: 0,
    skipped: 0,
  };
}

export function deploymentBinding(candidateReceiptDigest: string) {
  return {
    deploymentMode: "managed_tke",
    fabricProvider: "tencent-tke",
    candidateReceiptDigest,
    clusterId: CLUSTER_ID,
    region: REGION,
    namespace: NAMESPACE,
    ownerOrigin: OWNER_ORIGIN,
  };
}

export function protectedEnvironment(overrides = {}) {
  return {
    OPL_QUALIFICATION_ENVIRONMENT: "cloud-business-qualification",
    OPL_QUALIFICATION_ORIGIN: OWNER_ORIGIN,
    OPL_QUALIFICATION_CUSTOMER_EMAIL: ACCOUNT_EMAIL,
    OPL_QUALIFICATION_CUSTOMER_PASSWORD: "not-a-real-password",
    ...overrides,
  };
}

/*
 * The owner-API fixture.
 *
 * Every failure switch below is injected state, not a simulated production
 * path: the fixture never reaches a network, a cluster or a database. The
 * switches exist so the producer entry point can be driven through a lost
 * command answer, an unreadable owner readback and an unapproved managed-key
 * endpoint without pretending any of them succeeded.
 */
export function createFixturePlatform({
  walletMicros = "999999999",
  quoteMicros = QUOTE_MICROS,
  losePurchaseResponse = false,
  failOwnerReadbackAfterPurchase = false,
  retainedOrderExists = false,
  unavailableRuntimeStatus = false,
  undeclaredKeyFulfillment = false,
} = {}) {
  const state = {
    wallet: walletMicros,
    quote: quoteMicros,
    // The owner's own records: the orders it admitted and the quotes it
    // priced, each addressed by the idempotency identity it was handed.
    orders: new Map<string, any>(),
    byKey: new Map<string, any>(),
    quotes: new Map<string, any>(),
    quoteById: new Map<string, any>(),
    workspaceId: "",
    operationId: "",
    receiptId: "",
    purchases: 0,
    charges: 0,
    minted: 0,
    responsesLost: false,
    // The unapproved managed-key lifecycle endpoint, if a code path ever calls
    // it. The Sub2API contract is issuance-only, so the owner answers with
    // `codes.Unimplemented`; the producer must never turn that into a success.
    keyLifecycleCalls: [] as string[],
    boundaryOperationKeys: [] as string[],
    calls: [] as string[],
  };
  const envelope = (data: unknown, source = "sub2api") => ({
    source, available: true, status: "available", data,
  });
  const order = (operationId: string) => state.orders.get(operationId) ?? null;
  const keyLifecyclePath = (path: string) =>
    /managed-?key|workspace-key|gateway\/keys|\/api\/keys?\b/i.test(path);
  // The owner's own projection of an operation it admitted. It carries the
  // identities the owner minted and nothing the buyer derived.
  const ownerOperation = (admitted: any) => ({
    operationId: admitted.operationId,
    owner: "workspace",
    kind: "create_workspace",
    resourceId: admitted.workspaceId,
    status: admitted.status,
    stage: admitted.stage,
    requestId: admitted.requestId,
    createdAt: admitted.createdAt,
    updatedAt: admitted.updatedAt,
  });
  // The Ledger's own record of the purchase. It names the order that caused
  // it, the Workspace it paid for and the resources it delivered.
  const chargeReceipt = (receiptId: string) => ({
    receiptId,
    workspaceId: state.workspaceId,
    operationId: state.operationId,
    type: "billing.workspace_purchased.v1",
    status: "completed",
    chargeReference: "opl:workspace-purchase:charge:0001",
    totalUsdMicros: state.quote,
    fulfillment: {
      computeAllocationId: "ca-qualification-0001",
      storageId: "st-qualification-0001",
      attachmentId: "at-qualification-0001",
      // An undeclared managed Key in the charge receipt. The shared flow
      // must refuse to read this as the declared purchase.
      ...(undeclaredKeyFulfillment ? { managedKeyId: "mk-qualification-0001" } : {}),
    },
  });

  const json = async (path: string, init: any = {}) => {
    state.calls.push(path);
    if (keyLifecyclePath(path)) {
      state.keyLifecycleCalls.push(path);
      return { status: 501, payload: { code: "Unimplemented", message: "no approved Sub2API service endpoint is configured" } };
    }
    if (path === "/api/auth/me") {
      return {
        status: 200,
        payload: envelope({
          accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active",
          sub2apiUserId: ACCOUNT_SUB2API_USER_ID,
        }),
      };
    }
    if (path === "/api/gateway/wallet") {
      return { status: 200, payload: envelope({ usdMicros: state.wallet }) };
    }
    // The live owner session. The identity owner binds the actor it
    // authenticated to the Gateway user and publishes the Gateway email as the
    // display name, so this session and the Control Plane session name the same
    // customer.
    if (path === "/api/v2/auth/session") {
      return {
        status: 200,
        payload: {
          actorId: ACCOUNT_SUB2API_USER_ID, displayName: ACCOUNT_EMAIL, tenantId: ACCOUNT_TENANT_ID,
          permissions: [], csrfToken: OWNER_CSRF_TOKEN, expiresAt: OWNER_SESSION_EXPIRES_AT,
        },
      };
    }
    // The wallet the Gateway owner holds, read under the owner session's own
    // authorization.
    if (path === "/api/v2/wallet") {
      return {
        status: 200,
        payload: {
          source: "gateway", status: "available", balanceUSDMicros: state.wallet,
          currency: "USD", fetchedAt: OWNER_SESSION_EXPIRES_AT,
        },
      };
    }
    // The owner owns the catalog a buyer prices from. The buyer declares which
    // entry it buys, so the catalog offers exactly the entries the declaration
    // may name.
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
    // recorded against the idempotency identity it was handed, so a repeated
    // quote replays this quote instead of pricing a second one.
    if (path === "/api/v2/quotes" && init.method === "POST") {
      const key = String(init.headers?.["idempotency-key"] || "");
      const existing = state.quotes.get(key);
      if (existing) {
        state.calls.push("quote:replayed");
        return { status: 200, payload: existing };
      }
      const body = init.body || {};
      const quote = {
        id: ownerID("qte", key),
        purpose: "deploy",
        runtimeVersionId: String(body?.applicationSelection?.runtimeVersionId || ""),
        computePlanId: String(body?.computePlanId || ""),
        storagePlanId: String(body?.storagePlanId || ""),
        modelSelections: body?.modelSelections || [],
        periodMonths: Number(body?.periodMonths || 0),
        totalUSDMicros: state.quote,
        // An offered quote has no order yet. The order takes it, and an
        // accepted quote is one an order already took.
        status: "offered",
        createdAt: "2026-01-01T00:00:00.000Z",
        expiresAt: "2026-01-01T01:00:00.000Z",
      };
      state.quotes.set(key, quote);
      state.quoteById.set(quote.id, quote);
      state.calls.push("quote:issued");
      return { status: 201, payload: quote };
    }
    if (/^\/api\/v2\/quotes\/[^/]+$/.test(path)) {
      const quote = state.quoteById.get(decodeURIComponent(path.split("/").pop() as string));
      return quote ? { status: 200, payload: quote } : { status: 404, payload: null };
    }
    // The order is admitted against the quote the owner priced. An idempotency
    // identity admits one order, so a redelivery is answered from the order
    // already admitted rather than promoted into a second purchase.
    if (path === "/api/v2/workspaces" && init.method === "POST") {
      const key = String(init.headers?.["idempotency-key"] || "");
      const existing = state.byKey.get(key);
      if (existing) {
        state.calls.push("purchase:resumed");
        return { status: 202, payload: ownerOperation(order(existing)) };
      }
      state.minted += 1;
      const admitted = {
        operationId: ownerID("op", key, String(state.minted)),
        workspaceId: ownerID("workspace", key, String(state.minted)),
        requestId: String(init.headers?.["x-opl-request-id"] || ""),
        status: "succeeded",
        stage: "succeeded",
        createdAt: "2026-01-01T00:00:00.000Z",
        updatedAt: "2026-01-01T00:00:01.000Z",
      };
      state.byKey.set(key, admitted.operationId);
      state.orders.set(admitted.operationId, admitted);
      state.operationId = admitted.operationId;
      state.workspaceId = admitted.workspaceId;
      state.receiptId = ownerID("rcpt", admitted.operationId);
      state.purchases += 1;
      state.charges += 1;
      state.wallet = String(BigInt(state.wallet) - BigInt(state.quote));
      // The quote this order took is an accepted quote now.
      const quote = state.quoteById.get(String(init.body?.quoteId || ""));
      if (quote) quote.status = "accepted";
      state.calls.push("purchase:issued");
      // The platform admitted the order and its answer was lost. This is the
      // response-loss case, not a refused command: the order above exists. The
      // answer is lost once, so the recovery reads the order the owner already
      // admitted instead of a second purchase.
      if (losePurchaseResponse && !state.responsesLost) {
        state.responsesLost = true;
        throw new Error("workspace order response lost");
      }
      return { status: 202, payload: ownerOperation(admitted) };
    }
    if (/^\/api\/v2\/operations\/workspace\/[^/]+$/.test(path)) {
      const admitted = order(decodeURIComponent(path.split("/").pop() as string));
      if (!admitted) return { status: 404, payload: null };
      if (failOwnerReadbackAfterPurchase) return { status: 500, payload: null };
      return { status: 200, payload: ownerOperation(admitted) };
    }
    // The retained route no longer admits purchases. The orders it admitted
    // before the cutover are still there and still belong to the account that
    // bought them, so a buyer has to be able to see that one exists.
    if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
      if (!retainedOrderExists) return { status: 404, payload: null };
      const operationId = decodeURIComponent(path.split("/").pop() as string);
      return { status: 200, payload: {
        operationId, workspaceId: "workspace-retained-0001", status: "succeeded", phase: "succeeded",
        receiptId: "rcpt-retained-0001", provisioningMode: "resource_only",
      } };
    }
    if (path.startsWith("/api/workspaces?")) {
      return { status: 200, payload: {
        source: "control-plane", available: true, status: "available",
        data: { items: [{
          id: state.workspaceId,
          url: ENTRY_URL,
          openable: true,
          currentApplication: { status: "ready", entryUrl: ENTRY_URL, operationId: APPLICATION_OPERATION_ID },
        }], total: 1 },
      } };
    }
    if (/^\/api\/workspaces\/[^/]+\/runtime-status$/.test(path)) {
      if (unavailableRuntimeStatus) {
        return { status: 200, payload: {
          source: "fabric", available: false, status: "unavailable", data: null,
        } };
      }
      return { status: 200, payload: {
        source: "fabric", available: true, status: "available",
        data: {
          workspaceId: state.workspaceId,
          ready: true,
          status: "running",
          url: ENTRY_URL,
          runtimeId: "rt-qualification-0001",
          checks: [{ name: "console", ok: true }, { name: "gateway", ok: true }],
          currentApplication: { operationId: APPLICATION_OPERATION_ID },
        },
      } };
    }
    // The Ledger's own inventory of receipts. The receipt of an owner order is
    // found in that inventory, bound to the order and the Workspace the owner
    // minted.
    if (path.startsWith("/api/billing/receipts?")) {
      return { status: 200, payload: {
        source: "ledger", available: true, status: "available",
        data: { receipts: state.receiptId ? [chargeReceipt(state.receiptId)] : [], hasMore: false },
      } };
    }
    if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
      const receiptId = decodeURIComponent(path.split("/").pop() as string);
      return { status: 200, payload: {
        source: "ledger", available: true, status: "available",
        data: chargeReceipt(receiptId),
      } };
    }
    throw new Error(`unexpected request ${path}`);
  };

  const request = async (path: string, _init: any = {}) => {
    state.calls.push(`request:${path}`);
    if (keyLifecyclePath(path)) {
      state.keyLifecycleCalls.push(path);
      return { response: { status: 501 }, payload: { code: "Unimplemented", message: "no approved Sub2API service endpoint is configured" } };
    }
    if (path === "/api/auth/me") {
      return {
        response: { status: 200 },
        payload: envelope({
          accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active",
          sub2apiUserId: ACCOUNT_SUB2API_USER_ID,
        }),
      };
    }
    if (path === "/api/gateway/wallet") {
      return { response: { status: 200 }, payload: envelope({ usdMicros: state.wallet }) };
    }
    if (path === "/api/v2/auth/session") {
      return {
        response: { status: 200 },
        payload: {
          actorId: ACCOUNT_SUB2API_USER_ID, displayName: ACCOUNT_EMAIL, tenantId: ACCOUNT_TENANT_ID,
          permissions: [], csrfToken: OWNER_CSRF_TOKEN, expiresAt: OWNER_SESSION_EXPIRES_AT,
        },
      };
    }
    if (path === "/api/v2/wallet") {
      return {
        response: { status: 200 },
        payload: {
          source: "gateway", status: "available", balanceUSDMicros: state.wallet,
          currency: "USD", fetchedAt: OWNER_SESSION_EXPIRES_AT,
        },
      };
    }
    if (/^\/api\/v2\/operations\/workspace\/[^/]+$/.test(path)) {
      const admitted = order(decodeURIComponent(path.split("/").pop() as string));
      if (!admitted) return { response: { status: 404 }, payload: null };
      if (failOwnerReadbackAfterPurchase) return { response: { status: 500 }, payload: null };
      return { response: { status: 200 }, payload: ownerOperation(admitted) };
    }
    if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
      if (!retainedOrderExists) return { response: { status: 404 }, payload: null };
      const operationId = decodeURIComponent(path.split("/").pop() as string);
      return { response: { status: 200 }, payload: {
        operationId, workspaceId: "workspace-retained-0001", status: "succeeded", phase: "succeeded",
        receiptId: "rcpt-retained-0001", provisioningMode: "resource_only",
      } };
    }
    throw new Error(`unexpected request ${path}`);
  };

  return { state, http: { json, request } };
}

/*
 * The fixture provider boundary.
 *
 * `open` and `release` select the provider's own answer, so the producer entry
 * point can be driven through a refused or unobservable scope and through a
 * release the provider cannot prove, without any of those becoming a business
 * success.
 */
export function createFixtureBoundary(
  deployment: any,
  platform: ReturnType<typeof createFixturePlatform>,
  { open = "open", release = "released" }: { open?: "open" | "unknown"; release?: "released" | "unknown" } = {},
): TencentTkeProviderBoundary {
  const identity = { provider: "tencent-tke" as const, clusterId: deployment.clusterId, namespace: deployment.namespace };
  return {
    ...identity,
    async establishReachability({ operationKey }: any = {}) {
      platform.state.calls.push("boundary:open");
      platform.state.boundaryOperationKeys.push(String(operationKey || ""));
      if (open !== "open") {
        return { state: "unknown", reason: "no approved provider endpoint is configured", ...identity };
      }
      return {
        state: "open",
        ...identity,
        transport: { origin: deployment.ownerOrigin, http: platform.http, auth: { accountId: ACCOUNT_ID } },
      };
    },
    async releaseReachability() {
      platform.state.calls.push("boundary:release");
      if (release !== "released") {
        return { state: "unknown", reason: "the provider could not prove the scope was released", ...identity };
      }
      return { state: "released", ...identity };
    },
    async readProviderHealth() {
      platform.state.calls.push("boundary:health");
      return { state: "ready", ...identity, ready: true, serviceReady: true, failedChecks: [] };
    },
  };
}

export function createRecordingBoundary(): { boundary: TencentTkeProviderBoundary; calls: () => { opens: number; releases: number; healths: number } } {
  let opens = 0;
  let releases = 0;
  let healths = 0;
  return {
    boundary: {
      provider: "tencent-tke" as const,
      clusterId: CLUSTER_ID,
      namespace: NAMESPACE,
      async establishReachability() {
        opens += 1;
        throw new Error("the provider must not be opened");
      },
      async releaseReachability() {
        releases += 1;
        throw new Error("the provider must not be released");
      },
      async readProviderHealth() {
        healths += 1;
        throw new Error("provider health must not be read");
      },
    },
    calls: () => ({ opens, releases, healths }),
  };
}
