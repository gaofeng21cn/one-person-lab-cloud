/*
 * Fixture platform and provider boundary for the protected business
 * qualification producer tests.
 *
 * This is the owner-API shape reduced to the reads and the one purchase the
 * shared orchestrator performs. It is a test fixture only: it reaches no
 * network, cluster or database, and it is not a second production path.
 */

import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import type { TencentTkeProviderBoundary } from "../../tools/workspace-qualification-tencent-tke.ts";
import { launchOperationId, launchWorkspaceId } from "../../tools/workspace-purchase-core.ts";

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
  failLaunchReadbackAfterPurchase = false,
  unavailableRuntimeStatus = false,
  undeclaredKeyFulfillment = false,
} = {}) {
  const state = {
    wallet: walletMicros,
    quote: quoteMicros,
    orders: new Map<string, any>(),
    byKey: new Map<string, string>(),
    workspaceId: "",
    operationId: "",
    purchases: 0,
    charges: 0,
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

  const json = async (path: string, init: any = {}) => {
    state.calls.push(path);
    if (keyLifecyclePath(path)) {
      state.keyLifecycleCalls.push(path);
      return { status: 501, payload: { code: "Unimplemented", message: "no approved Sub2API service endpoint is configured" } };
    }
    if (path === "/api/auth/me") {
      return { status: 200, payload: envelope({ accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active" }) };
    }
    if (path === "/api/gateway/wallet") {
      return { status: 200, payload: envelope({ usdMicros: state.wallet }) };
    }
    if (path === "/api/pricing/preview") {
      return { status: 200, payload: { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: state.quote } };
    }
    if (path === "/api/workspace-launches" && init.method === "POST") {
      const key = String(init.headers?.["idempotency-key"] || "");
      const operationId = launchOperationId(ACCOUNT_ID, key);
      const workspaceId = launchWorkspaceId(ACCOUNT_ID, operationId);
      const existing = state.byKey.get(key);
      if (existing) {
        state.calls.push("purchase:resumed");
        return { status: 202, payload: { operationId: existing, workspaceId } };
      }
      state.byKey.set(key, operationId);
      state.operationId = operationId;
      state.workspaceId = workspaceId;
      state.orders.set(operationId, {
        operationId,
        workspaceId,
        accountId: ACCOUNT_ID,
        packageId: String(init.body?.packageId || ""),
        autoRenew: init.body?.autoRenew === true,
        name: String(init.body?.name || ""),
        status: "succeeded",
        phase: "succeeded",
        receiptId: `receipt-${operationId.slice(-18)}`,
        provisioningMode: "resource_only",
        totalChargeUsdMicros: state.quote,
        computeAllocationId: "ca-qualification-0001",
        storageId: "st-qualification-0001",
        attachmentId: "at-qualification-0001",
      });
      state.purchases += 1;
      state.charges += 1;
      state.wallet = String(BigInt(state.wallet) - BigInt(state.quote));
      state.calls.push("purchase:issued");
      // The platform admitted the order and its answer was lost. This is the
      // response-loss case, not a refused command: the order above exists.
      if (losePurchaseResponse) throw new Error("workspace launch response lost");
      return { status: 202, payload: { operationId, workspaceId } };
    }
    if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
      const admitted = order(decodeURIComponent(path.split("/").pop() as string));
      return admitted ? { status: 200, payload: admitted } : { status: 404, payload: null };
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
    if (/^\/api\/billing\/receipts\/[^/]+$/.test(path)) {
      const receiptId = decodeURIComponent(path.split("/").pop() as string);
      return { status: 200, payload: {
        source: "ledger", available: true, status: "available",
        data: {
          receiptId,
          workspaceId: state.workspaceId,
          operationId: state.operationId,
          type: "billing.workspace_purchased.v1",
          status: "completed",
          chargeReference: "opl:workspace-launch:charge:0001",
          totalUsdMicros: state.quote,
          fulfillment: {
            computeAllocationId: "ca-qualification-0001",
            storageId: "st-qualification-0001",
            attachmentId: "at-qualification-0001",
            // An undeclared managed Key in the charge receipt. The shared flow
            // must refuse to read this as the declared purchase.
            ...(undeclaredKeyFulfillment ? { managedKeyId: "mk-qualification-0001" } : {}),
          },
        },
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
      return { response: { status: 200 }, payload: envelope({ accountId: ACCOUNT_ID, email: ACCOUNT_EMAIL, role: "owner", status: "active" }) };
    }
    if (path === "/api/gateway/wallet") {
      return { response: { status: 200 }, payload: envelope({ usdMicros: state.wallet }) };
    }
    if (/^\/api\/workspace-launches\/[^/]+$/.test(path)) {
      const admitted = order(decodeURIComponent(path.split("/").pop() as string));
      if (admitted && failLaunchReadbackAfterPurchase) {
        return { response: { status: 500 }, payload: null };
      }
      return admitted ? { response: { status: 200 }, payload: admitted } : { response: { status: 404 }, payload: null };
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
