import type { WorkspaceApplicationConfigurationDTO, WorkspaceApplicationSecretBindingDTO } from "./dtos.ts";
import { decodeDto, decodeSource } from "./dtos.ts";
import { cloudIdentity } from "../app/console-identity.ts";
import type {
  AnnouncementPageDTO,
  AnnouncementDTO,
  AnnouncementReadDTO,
  WorkspaceApplicationIntentDTO,
  WorkspaceApplicationRevisionAdmissionDTO,
  BillingReceipt,
  BillingReceiptPage,
  WorkspaceSettlementTrend,
  CreateGatewayKeyRequest,
  GatewayAccountUsageSummaryDTO,
  GatewayBalanceHistoryPageDTO,
  GatewayEndpointDTO,
  GatewayGroupPageDTO,
  GatewayUsagePeriod,
  GatewayKeyListQuery,
  GatewayKeyPageDTO,
  GatewayKeySecretDTO,
  GatewayKeySummaryDTO,
  GatewayKeyUsagePageDTO,
  GatewayUsageItem,
  UnavailableSource,
  GatewayUsageSummaryDTO,
  GatewayWallet,
  GatewayWalletReadbackDTO,
  AnnouncementDraftRequest,
  AnnouncementScheduleRequest,
  BillingReviewResolutionRequest,
  ProvisionAccountRequest,
  OperatorAccountCommandDTO,
  OperatorProvisionAccountCommandDTO,
  OperatorAccountPageDTO,
  OperatorWorkspacePurchaseEligibilityCommandDTO,
  OperatorAnnouncementPageDTO,
  OperatorHealthDTO,
  OperatorOverviewDTO,
  OperatorRuntimeObservationsDTO,
  OperatorReconciliationPageDTO,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  OperatorWorkspaceRuntimeImagePolicyDTO,
  OperatorWorkspaceRuntimeImagePreviewDTO,
  WorkspaceRuntimeImageReplacementDTO,
  WalletAdjustmentOperationDTO,
  WalletAdjustmentRecoveryRequest,
  WalletAdjustmentRequest,
  WorkspaceLaunchRecoveryDTO,
  WorkspaceLaunchRecoveryRequest,
  OperatorAccountsData,
  OperationStatusDTO,
  PricingCatalogResponse,
  PricingPreviewRequest,
  PricingPreviewResponse,
  SourceEnvelope,
  UpdateGatewayKeyRequest
} from "./dtos.ts";
import { deleteJson, getJson, patchJson, postJson, putJson, type ApiError } from "./console-api.ts";

const nonnegativeInt64 = /^(?:0|[1-9][0-9]{0,17}|[1-8][0-9]{18}|9[0-1][0-9]{17}|92[0-1][0-9]{16}|922[0-2][0-9]{15}|9223[0-2][0-9]{14}|92233[0-6][0-9]{13}|922337[0-1][0-9]{12}|92233720[0-2][0-9]{10}|922337203[0-5][0-9]{9}|9223372036[0-7][0-9]{8}|92233720368[0-4][0-9]{7}|922337203685[0-3][0-9]{6}|9223372036854[0-6][0-9]{5}|92233720368547[0-6][0-9]{4}|922337203685477[0-4][0-9]{3}|9223372036854775[0-7][0-9]{2}|92233720368547758[0-6]|922337203685477580[0-7]|922337203685477580[0-6]|922337203685477580[0-7])$/;

function decodeConsoleSource<T>(value: unknown): SourceEnvelope<T> {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const dto = value as Record<string, unknown>;
    if (dto.status === "unavailable" && dto.available === false && !("data" in dto)
      && dto.reasonCode === undefined && typeof dto.source === "string" && dto.source.trim()) {
      const source = dto.source.trim().toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "unknown";
      return decodeSource<T>({ ...dto, reasonCode: `${source}_unavailable` });
    }
  }
  return decodeSource<T>(value);
}

async function sourceGet<T>(path: string, signal?: AbortSignal): Promise<SourceEnvelope<T>> {
  try {
    return decodeConsoleSource<T>(await getJson<unknown>(path, { signal }));
  } catch (error) {
    const payload = (error as ApiError).payload;
    if (payload !== undefined) {
      try {
        return decodeConsoleSource<T>(payload);
      } catch {
        // Preserve the transport error when the server did not return a source envelope.
      }
    }
    throw error;
  }
}

async function sourceWrite<T>(request: () => Promise<unknown>): Promise<SourceEnvelope<T>> {
  try {
    return decodeConsoleSource<T>(await request());
  } catch (error) {
    const payload = (error as ApiError).payload;
    if (payload !== undefined) {
      try {
        return decodeConsoleSource<T>(payload);
      } catch {
        // Preserve the transport error when the server did not return a source envelope.
      }
    }
    throw error;
  }
}

function sourcePost<T>(path: string, body: unknown, csrfToken: string, idempotencyKey = ""): Promise<SourceEnvelope<T>> {
  return sourceWrite<T>(() => postJson<unknown>(path, body, csrfToken, idempotencyKey));
}

function sourcePatch<T>(path: string, body: unknown, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<T>> {
  return sourceWrite<T>(() => patchJson<unknown>(path, body, csrfToken, idempotencyKey));
}

function sourceDelete<T>(path: string, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<T>> {
  return sourceWrite<T>(() => deleteJson<unknown>(path, csrfToken, idempotencyKey));
}

// The baked console identity decides which owner answers the wallet. Exported so
// both decisions are pinned by tests instead of only by the build environment.
export function walletReadRoute(cloud: boolean): "/api/v2/wallet" | "/api/gateway/wallet" {
  return cloud ? "/api/v2/wallet" : "/api/gateway/wallet";
}

// Personal financial history and announcements follow the same baked identity as
// the wallet. A cloud build reads the BFF route that carries a live CloudIdentity
// proof; a retained build keeps the Control Plane projection it authenticated
// against. Neither decision has a runtime fallback to the other route.
export function billingReadRoute(cloud: boolean): "/api/v2/legacy-billing" | "/api/billing" {
  return cloud ? "/api/v2/legacy-billing" : "/api/billing";
}

export function announcementReadRoute(cloud: boolean): "/api/v2/announcements" | "/api/announcements" {
  return cloud ? "/api/v2/announcements" : "/api/announcements";
}

// A valid readback is a nonnegative balance, including the zero a charge leaves
// behind. Rejecting zero would report an unavailable balance instead of an empty
// one, so the sufficiency decision belongs at the purchase-before boundary.
export function decodeGatewayWalletReadback(value: unknown): GatewayWallet {
  if (!value || typeof value !== "object") throw new Error("invalid_gateway_wallet_readback");
  const readback = value as GatewayWalletReadbackDTO;
  if (readback.source !== "gateway" || readback.status !== "available" || readback.currency !== "USD"
    || typeof readback.balanceUSDMicros !== "string" || !nonnegativeInt64.test(readback.balanceUSDMicros)
    || typeof readback.fetchedAt !== "string" || !readback.fetchedAt.trim()) {
    throw new Error("invalid_gateway_wallet_readback");
  }
  return { currency: readback.currency, usdMicros: readback.balanceUSDMicros, status: "active" };
}

export function getGatewayWallet(signal?: AbortSignal): Promise<SourceEnvelope<GatewayWallet>> {
  // Identity decides the owner. A cloud build authenticates through CloudIdentity
  // on the BFF, which serves the Gateway owner readback; a retained build
  // authenticates through /api/auth/login and must keep reading the Control Plane
  // answer it already had. This is a baked decision, not a fallback path.
  if (!cloudIdentity) return sourceGet<GatewayWallet>(walletReadRoute(false), signal);
  return getJson<unknown>(walletReadRoute(true), { signal }).then((value) => ({
    source: "gateway",
    status: "available" as const,
    available: true as const,
    fetchedAt: (value as { fetchedAt: string }).fetchedAt,
    data: decodeGatewayWalletReadback(value)
  }));
}

type PersonalGatewayKeyReadback = Omit<GatewayKeySummaryDTO,
  "quotaUsdMicros" | "quotaUsedUsdMicros" | "rateLimit5hUsdMicros" | "rateLimit1dUsdMicros" | "rateLimit7dUsdMicros" |
  "usage5hUsdMicros" | "usage1dUsdMicros" | "usage7dUsdMicros" | "groupId" | "lastUsedAt" | "lastUsedIp" | "expiresAt" | "createdAt" | "updatedAt"> & {
  quotaUSDMicros: string; quotaUsedUSDMicros: string;
  rateLimit5hUSDMicros: string; rateLimit1dUSDMicros: string; rateLimit7dUSDMicros: string;
  usage5hUSDMicros: string; usage1dUSDMicros: string; usage7dUSDMicros: string;
  groupId?: string; lastUsedAt?: string; lastUsedIp?: string;
  expiresAt?: string; createdAt?: string; updatedAt?: string;
};
type PersonalGatewayKeyPageReadback = Omit<GatewayKeyPageDTO, "items" | "total"> & { items: PersonalGatewayKeyReadback[]; total: string };
type PersonalGatewayUsageReadback = Omit<GatewayUsageItem, "inputTokens" | "outputTokens" | "cacheCreationTokens" | "cacheReadTokens" | "actualCostUsdMicros" | "durationMs" | "firstTokenMs"> & {
  inputTokens: string; outputTokens: string; cacheCreationTokens: string; cacheReadTokens: string;
  actualCostUSDMicros: string; durationMs?: string; firstTokenMs?: string;
};
type PersonalGatewayKeyUsagePageReadback = Omit<GatewayKeyUsagePageDTO, "items" | "total"> & { items: PersonalGatewayUsageReadback[]; total: string };

function personalGatewayNumber(value: unknown): number {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]*)$/.test(value)) throw new Error("invalid_personal_gateway_integer");
  const number = Number(value);
  if (!Number.isSafeInteger(number)) throw new Error("unsafe_personal_gateway_integer");
  return number;
}

function personalGatewayMoney(value: unknown): string {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]{0,18})$/.test(value) || BigInt(value) > 9223372036854775807n) {
    throw new Error("invalid_personal_gateway_money");
  }
  return value;
}

function decodePersonalGatewayKey(value: PersonalGatewayKeyReadback): GatewayKeySummaryDTO {
  return {
    id: value.id, name: value.name, groupId: value.groupId ?? null, status: value.status, kind: value.kind,
    manageable: value.manageable, deletable: value.deletable,
    ipWhitelist: value.ipWhitelist, ipBlacklist: value.ipBlacklist,
    quotaUsdMicros: personalGatewayMoney(value.quotaUSDMicros), quotaUsedUsdMicros: personalGatewayMoney(value.quotaUsedUSDMicros),
    rateLimit5hUsdMicros: personalGatewayMoney(value.rateLimit5hUSDMicros),
    rateLimit1dUsdMicros: personalGatewayMoney(value.rateLimit1dUSDMicros),
    rateLimit7dUsdMicros: personalGatewayMoney(value.rateLimit7dUSDMicros),
    usage5hUsdMicros: personalGatewayMoney(value.usage5hUSDMicros), usage1dUsdMicros: personalGatewayMoney(value.usage1dUSDMicros),
    usage7dUsdMicros: personalGatewayMoney(value.usage7dUSDMicros), currentConcurrency: value.currentConcurrency,
    lastUsedAt: value.lastUsedAt ?? null, lastUsedIp: value.lastUsedIp ?? null, expiresAt: value.expiresAt ?? null,
    createdAt: value.createdAt ?? null, updatedAt: value.updatedAt ?? null
  };
}

async function personalGatewayGet<Wire, Data>(path: string, decode: (value: Wire) => Data, empty: (data: Data) => boolean, signal?: AbortSignal): Promise<SourceEnvelope<Data>> {
  const data = decode(await getJson<Wire>(path, { signal }));
  return { source: "gateway", available: true, status: empty(data) ? "empty" : "available", fetchedAt: new Date().toISOString(), data };
}

function personalGatewayUnavailable(reasonCode: string): UnavailableSource {
  return { source: "gateway", available: false, status: "unavailable", fetchedAt: new Date().toISOString(), reasonCode };
}

export function gatewayKeyReadFailure(error: unknown): UnavailableSource {
  const payload = (error as ApiError)?.payload as { code?: string; error?: string } | undefined;
  const reauthentication = (payload?.code ?? payload?.error)?.toUpperCase() === "REAUTHENTICATION_REQUIRED";
  return { ...personalGatewayUnavailable(reauthentication ? "REAUTHENTICATION_REQUIRED" : cloudIdentity ? "gateway_key_read_unavailable" : "sub2api_unavailable"),
    source: cloudIdentity ? "gateway" : "sub2api" };
}

export function getGatewayKeys(query: GatewayKeyListQuery = {}, signal?: AbortSignal): Promise<SourceEnvelope<GatewayKeyPageDTO>> {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  const suffix = params.size > 0 ? `?${params}` : "";
  if (!cloudIdentity) return sourceGet<GatewayKeyPageDTO>(`/api/gateway/keys${suffix}`, signal);
  return personalGatewayGet(`/api/v2/gateway/keys${suffix}`, (value: PersonalGatewayKeyPageReadback) => ({
    items: value.items.map(decodePersonalGatewayKey), total: personalGatewayNumber(value.total),
    page: value.page, pageSize: value.pageSize, pages: value.pages
  }), (data) => data.items.length === 0, signal);
}

export function getGatewayGroups(signal?: AbortSignal): Promise<SourceEnvelope<GatewayGroupPageDTO>> {
  if (!cloudIdentity) return sourceGet<GatewayGroupPageDTO>("/api/gateway/groups", signal);
  return personalGatewayGet("/api/v2/gateway/groups", (value: GatewayGroupPageDTO) => ({
    items: value.items.map(({ id, name, description, platform, rateMultiplier, subscriptionType, status }) => ({
      id, name, description, platform, rateMultiplier, subscriptionType, status
    })), total: value.total
  }), (data) => data.items.length === 0, signal);
}

export function getGatewayEndpoint(signal?: AbortSignal): Promise<SourceEnvelope<GatewayEndpointDTO>> {
  return sourceGet<GatewayEndpointDTO>(cloudIdentity ? "/api/v2/gateway/endpoint" : "/api/gateway/endpoint", signal);
}

export function getGatewayKey(keyId: string, signal?: AbortSignal): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  if (!cloudIdentity) return sourceGet<GatewayKeySummaryDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}`, signal);
  return personalGatewayGet(`/api/v2/gateway/keys/${encodeURIComponent(keyId)}`, decodePersonalGatewayKey, () => false, signal);
}

export function createGatewayKey(input: CreateGatewayKeyRequest, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  if (cloudIdentity) return Promise.resolve(personalGatewayUnavailable("gateway_key_capability_unavailable"));
  return sourcePost<GatewayKeySummaryDTO>("/api/gateway/keys", input, csrfToken, idempotencyKey);
}

export function updateGatewayKey(keyId: string, input: UpdateGatewayKeyRequest, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  if (cloudIdentity) return Promise.resolve(personalGatewayUnavailable("gateway_key_capability_unavailable"));
  return sourcePatch<GatewayKeySummaryDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}`, input, csrfToken, idempotencyKey);
}

export function deleteGatewayKey(keyId: string, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<OperationStatusDTO>> {
  if (cloudIdentity) return Promise.resolve(personalGatewayUnavailable("gateway_key_capability_unavailable"));
  return sourceDelete<OperationStatusDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}`, csrfToken, idempotencyKey);
}

export function getGatewayBalanceHistory(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<GatewayBalanceHistoryPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<GatewayBalanceHistoryPageDTO>(`${cloudIdentity ? "/api/v2/gateway" : "/api/gateway"}/balance-history?${params}`, signal);
}

export function revealGatewayKey(keyId: string, csrfToken: string): Promise<SourceEnvelope<GatewayKeySecretDTO>> {
  if (cloudIdentity) return Promise.resolve(personalGatewayUnavailable("gateway_key_capability_unavailable"));
  return sourcePost<GatewayKeySecretDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}/reveal`, {}, csrfToken);
}

export function getGatewayKeyUsage(keyId: string, page = 1, pageSize = 20, period: GatewayUsagePeriod = "month", signal?: AbortSignal): Promise<SourceEnvelope<GatewayKeyUsagePageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize), period });
  if (!cloudIdentity) return sourceGet<GatewayKeyUsagePageDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}/usage?${params}`, signal);
  return personalGatewayGet(`/api/v2/gateway/keys/${encodeURIComponent(keyId)}/usage?${params}`, (value: PersonalGatewayKeyUsagePageReadback) => ({
    items: value.items.map((item) => ({
      apiKeyId: item.apiKeyId, requestId: item.requestId, createdAt: item.createdAt, model: item.model,
      inboundEndpoint: item.inboundEndpoint, requestType: item.requestType,
      inputTokens: personalGatewayNumber(item.inputTokens), outputTokens: personalGatewayNumber(item.outputTokens),
      cacheCreationTokens: personalGatewayNumber(item.cacheCreationTokens), cacheReadTokens: personalGatewayNumber(item.cacheReadTokens),
      actualCostUsdMicros: personalGatewayMoney(item.actualCostUSDMicros),
      durationMs: item.durationMs === undefined ? null : personalGatewayNumber(item.durationMs),
      firstTokenMs: item.firstTokenMs === undefined ? null : personalGatewayNumber(item.firstTokenMs)
    })), total: personalGatewayNumber(value.total), page: value.page, pageSize: value.pageSize, pages: value.pages
  }), (data) => data.items.length === 0, signal);
}

export function getGatewayKeyUsageSummary(keyId: string, period: GatewayUsagePeriod = "month", signal?: AbortSignal): Promise<SourceEnvelope<GatewayUsageSummaryDTO>> {
  if (!cloudIdentity) return sourceGet<GatewayUsageSummaryDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}/usage-summary?${new URLSearchParams({ period })}`, signal);
  return personalGatewayGet(`/api/v2/gateway/keys/${encodeURIComponent(keyId)}/usage-summary?${new URLSearchParams({ period })}`,
    decodeGatewayUsageSummaryReadback, () => false, signal);
}

export function gatewayUsageSummaryRoute(cloud: boolean): "/api/v2/usage/summary" | "/api/gateway/usage-summary" {
  return cloud ? "/api/v2/usage/summary" : "/api/gateway/usage-summary";
}

function decodeGatewayUsageCount(value: unknown, field: string): number {
  if (typeof value !== "string" || !nonnegativeInt64.test(value)) throw new Error(`invalid_gateway_usage_summary_${field}`);
  const decoded = Number(value);
  if (!Number.isSafeInteger(decoded)) throw new Error(`invalid_gateway_usage_summary_${field}`);
  return decoded;
}

export function decodeGatewayUsageSummaryReadback(value: unknown): GatewayAccountUsageSummaryDTO {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("invalid_gateway_usage_summary");
  const readback = value as Record<string, unknown>;
  return {
    totalRequests: decodeGatewayUsageCount(readback.totalRequests, "requests"),
    totalInputTokens: decodeGatewayUsageCount(readback.totalInputTokens, "input_tokens"),
    totalOutputTokens: decodeGatewayUsageCount(readback.totalOutputTokens, "output_tokens"),
    totalTokens: decodeGatewayUsageCount(readback.totalTokens, "tokens"),
    totalActualCostUsdMicros: personalGatewayMoney(readback.totalActualCostUSDMicros)
  };
}

export function getGatewayAccountUsageSummary(period: GatewayUsagePeriod = "month", signal?: AbortSignal): Promise<SourceEnvelope<GatewayAccountUsageSummaryDTO>> {
  const path = `${gatewayUsageSummaryRoute(cloudIdentity)}?${new URLSearchParams({ period })}`;
  if (!cloudIdentity) return sourceGet<GatewayAccountUsageSummaryDTO>(path, signal);
  return getJson<unknown>(path, { signal }).then((value) => ({
    source: "gateway",
    status: "available" as const,
    available: true as const,
    fetchedAt: new Date().toISOString(),
    data: decodeGatewayUsageSummaryReadback(value)
  }));
}

export function getBillingReceipts(cursor = "", limit = 20, signal?: AbortSignal): Promise<SourceEnvelope<BillingReceiptPage>> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return sourceGet<BillingReceiptPage>(`${billingReadRoute(cloudIdentity)}/receipts?${params}`, signal);
}

/** Reads the owner's confirmed Workspace wallet movements for its fixed window. */
export function getWorkspaceSettlementTrend(signal?: AbortSignal): Promise<SourceEnvelope<WorkspaceSettlementTrend>> {
  return sourceGet<WorkspaceSettlementTrend>(`${billingReadRoute(cloudIdentity)}/workspace-settlements`, signal);
}

export function getBillingReceipt(receiptId: string, signal?: AbortSignal): Promise<SourceEnvelope<BillingReceipt>> {
  return sourceGet<BillingReceipt>(`${billingReadRoute(cloudIdentity)}/receipts/${encodeURIComponent(receiptId)}`, signal);
}

export function getAnnouncements(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<AnnouncementPageDTO>> {
  return sourceGet<AnnouncementPageDTO>(`${announcementReadRoute(cloudIdentity)}?${new URLSearchParams({ page: String(page), pageSize: String(pageSize) })}`, signal);
}

export function markAnnouncementRead(announcementId: string, csrfToken: string, idempotencyKey: string): Promise<AnnouncementReadDTO> {
  return postJson<unknown>(`${announcementReadRoute(cloudIdentity)}/${encodeURIComponent(announcementId)}/read`, {}, csrfToken, idempotencyKey).then(decodeDto<AnnouncementReadDTO>);
}

// The retained Control Plane owns the legacy package pricing catalog and its
// preview. A CloudIdentity session has no retained Account session there, and
// Cloud has no package-pricing catalog owner yet, so a cloud build refuses this
// read instead of falling back to a route that answers 401. The cloud launch
// surface prices through the Resource Catalog quote (`POST /api/v2/quotes`) and
// the Resource Catalog plan reads, so no price is lost or invented.
export type PackagePricingReadPlan =
  | { state: "read"; owner: "control_plane" }
  | { state: "unavailable"; owner: "resource_catalog"; reasonCode: "pricing_catalog_owner_unavailable" };

export function packagePricingReadPlan(cloud: boolean): PackagePricingReadPlan {
  return cloud
    ? { state: "unavailable", owner: "resource_catalog", reasonCode: "pricing_catalog_owner_unavailable" }
    : { state: "read", owner: "control_plane" };
}

export function getPricingCatalog(cloud: boolean = cloudIdentity): Promise<PricingCatalogResponse> {
  const plan = packagePricingReadPlan(cloud);
  if (plan.state === "unavailable") return Promise.reject(new Error(plan.reasonCode));
  return getJson<unknown>("/api/pricing/catalog").then(decodeDto<PricingCatalogResponse>);
}

export function previewPricing(input: PricingPreviewRequest, csrfToken: string, cloud: boolean = cloudIdentity): Promise<PricingPreviewResponse> {
  const plan = packagePricingReadPlan(cloud);
  if (plan.state === "unavailable") return Promise.reject(new Error(plan.reasonCode));
  return postJson<unknown>("/api/pricing/preview", input, csrfToken).then(decodeDto<PricingPreviewResponse>);
}

export function getOperatorAccounts(): Promise<SourceEnvelope<OperatorAccountsData>> {
  return sourceGet<OperatorAccountsData>("/api/operator/accounts");
}

export function getOperatorOverview(signal?: AbortSignal): Promise<SourceEnvelope<OperatorOverviewDTO>> {
  return sourceGet<OperatorOverviewDTO>("/api/operator/overview", signal);
}

export function getOperatorAccountsPage(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<OperatorAccountPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorAccountPageDTO>(`/api/operator/accounts?${params}`, signal);
}

export function getOperatorWorkspaces(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<OperatorWorkspacePageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorWorkspacePageDTO>(`/api/operator/workspaces?${params}`, signal);
}

export function getOperatorWorkspace(workspaceId: string, signal?: AbortSignal): Promise<SourceEnvelope<OperatorWorkspaceDTO>> {
  return sourceGet<OperatorWorkspaceDTO>(`/api/operator/workspaces/${encodeURIComponent(workspaceId)}`, signal);
}

export function getOperatorWorkspaceRuntimeImagePolicy(signal?: AbortSignal): Promise<SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>> {
  return sourceGet<OperatorWorkspaceRuntimeImagePolicyDTO>("/api/operator/workspace-runtime-image-policy", signal);
}

export function activateOperatorWorkspaceImageRelease(
  releaseVersion: string,
  expectedRevision: number,
  reason: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<OperatorWorkspaceRuntimeImagePolicyDTO> {
  return postJson<unknown>(
    "/api/operator/workspace-image-release-activations",
    { releaseVersion, expectedRevision, reason }, csrfToken, idempotencyKey
  ).then(decodeDto<OperatorWorkspaceRuntimeImagePolicyDTO>);
}

export function getOperatorWorkspaceRuntimeImageReplacementPreview(workspaceId: string, signal?: AbortSignal): Promise<SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>> {
  return sourceGet<OperatorWorkspaceRuntimeImagePreviewDTO>(`/api/operator/workspaces/${encodeURIComponent(workspaceId)}/runtime-image-replacements/preview`, signal);
}

export function createOperatorWorkspaceRuntimeImageReplacement(
  workspaceId: string,
  replacementImageDigest: string,
  reason: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceRuntimeImageReplacementDTO> {
  return postJson<unknown>(
    `/api/operator/workspaces/${encodeURIComponent(workspaceId)}/runtime-image-replacements`,
    { replacementImageDigest, reason }, csrfToken, idempotencyKey
  ).then(decodeDto<WorkspaceRuntimeImageReplacementDTO>);
}

export function getOperatorWorkspaceRuntimeImageReplacement(
  workspaceId: string,
  operationId: string,
  signal?: AbortSignal
): Promise<WorkspaceRuntimeImageReplacementDTO> {
  return getJson<unknown>(
    `/api/operator/workspaces/${encodeURIComponent(workspaceId)}/runtime-image-replacements/${encodeURIComponent(operationId)}`,
    { signal }
  ).then(decodeDto<WorkspaceRuntimeImageReplacementDTO>);
}

export function getOperatorReconciliation(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<OperatorReconciliationPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorReconciliationPageDTO>(`/api/operator/reconciliation?${params}`, signal);
}

export function getOperatorHealth(signal?: AbortSignal): Promise<SourceEnvelope<OperatorHealthDTO>> {
  return sourceGet<OperatorHealthDTO>("/api/operator/health", signal);
}

export function getOperatorRuntimeObservations(signal?: AbortSignal): Promise<SourceEnvelope<OperatorRuntimeObservationsDTO>> {
  return sourceGet<OperatorRuntimeObservationsDTO>("/api/operator/runtime-observations", signal);
}

export function getOperatorAnnouncements(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<OperatorAnnouncementPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorAnnouncementPageDTO>(`/api/operator/announcements?${params}`, signal);
}

export function createWalletAdjustment(accountId: string, input: WalletAdjustmentRequest, csrfToken: string, idempotencyKey: string): Promise<WalletAdjustmentOperationDTO> {
  return postJson<unknown>(`/api/operator/accounts/${encodeURIComponent(accountId)}/wallet-adjustments`, input, csrfToken, idempotencyKey).then(decodeDto<WalletAdjustmentOperationDTO>);
}

export function getWalletAdjustment(operationId: string, signal?: AbortSignal): Promise<WalletAdjustmentOperationDTO> {
  return getJson<unknown>(`/api/operator/wallet-adjustments/${encodeURIComponent(operationId)}`, { signal }).then(decodeDto<WalletAdjustmentOperationDTO>);
}

export function recoverWalletAdjustment(operationId: string, input: WalletAdjustmentRecoveryRequest, csrfToken: string, idempotencyKey: string): Promise<WalletAdjustmentOperationDTO> {
  return postJson<unknown>(`/api/operator/wallet-adjustments/${encodeURIComponent(operationId)}/recover`, input, csrfToken, idempotencyKey).then(decodeDto<WalletAdjustmentOperationDTO>);
}

export function provisionOperatorAccount(input: ProvisionAccountRequest, csrfToken: string, idempotencyKey: string): Promise<OperatorProvisionAccountCommandDTO> {
  return postJson<unknown>("/api/operator/accounts", input, csrfToken, idempotencyKey).then(decodeDto<OperatorProvisionAccountCommandDTO>);
}

export function disableOperatorAccount(accountId: string, reason: string, csrfToken: string, idempotencyKey: string): Promise<OperatorAccountCommandDTO> {
  return postJson<unknown>(`/api/operator/accounts/${encodeURIComponent(accountId)}/disable`, { confirmationAccountId: accountId, reason }, csrfToken, idempotencyKey).then(decodeDto<OperatorAccountCommandDTO>);
}

export function setOperatorWorkspacePurchaseEligibility(accountId: string, enabled: boolean, reason: string, csrfToken: string, idempotencyKey: string): Promise<OperatorWorkspacePurchaseEligibilityCommandDTO> {
  return postJson<unknown>(`/api/operator/accounts/${encodeURIComponent(accountId)}/workspace-purchase-eligibility`, { confirmationAccountId: accountId, enabled, reason }, csrfToken, idempotencyKey).then(decodeDto<OperatorWorkspacePurchaseEligibilityCommandDTO>);
}

export function resolveBillingReview(resourceType: string, resourceId: string, input: BillingReviewResolutionRequest, csrfToken: string, idempotencyKey: string): Promise<OperationStatusDTO> {
  return postJson<unknown>(`/api/operator/billing-reviews/${encodeURIComponent(resourceType)}/${encodeURIComponent(resourceId)}/resolve`, input, csrfToken, idempotencyKey).then(decodeDto<OperationStatusDTO>);
}

function decodeWorkspaceLaunchRecovery(value: unknown, operationId: string): WorkspaceLaunchRecoveryDTO {
  const result = decodeDto<WorkspaceLaunchRecoveryDTO>(value);
  if (!result || result.operationId !== operationId || !Number.isSafeInteger(result.launchVersion) || result.launchVersion < 1
    || typeof result.status !== "string" || !result.status || typeof result.stage !== "string" || !result.stage
    || !Array.isArray(result.allowedActions) || result.allowedActions.length > 2
    || result.allowedActions.some((action) => action !== "check_result" && action !== "close_unfulfilled")
    || new Set(result.allowedActions).size !== result.allowedActions.length
    || result.status !== "manual_review" && result.allowedActions.length > 0) {
    throw new Error("invalid_workspace_launch_recovery");
  }
  return result;
}

export function getWorkspaceLaunchRecovery(operationId: string, signal?: AbortSignal): Promise<WorkspaceLaunchRecoveryDTO> {
  return getJson<unknown>(`/api/operator/workspace-launches/${encodeURIComponent(operationId)}/recovery`, { signal })
    .then((value) => decodeWorkspaceLaunchRecovery(value, operationId));
}

export function recoverWorkspaceLaunch(operationId: string, input: WorkspaceLaunchRecoveryRequest, csrfToken: string, idempotencyKey: string): Promise<WorkspaceLaunchRecoveryDTO> {
  return postJson<unknown>(`/api/operator/workspace-launches/${encodeURIComponent(operationId)}/recover`, input, csrfToken, idempotencyKey)
    .then((value) => decodeWorkspaceLaunchRecovery(value, operationId));
}

export function createOperatorAnnouncement(input: AnnouncementDraftRequest, csrfToken: string, idempotencyKey: string): Promise<AnnouncementDTO> {
  return postJson<unknown>("/api/operator/announcements", input, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function updateOperatorAnnouncement(announcementId: string, input: AnnouncementDraftRequest, csrfToken: string, idempotencyKey: string): Promise<AnnouncementDTO> {
  return putJson<unknown>(`/api/operator/announcements/${encodeURIComponent(announcementId)}`, input, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function publishOperatorAnnouncement(announcementId: string, input: AnnouncementScheduleRequest, csrfToken: string, idempotencyKey: string): Promise<AnnouncementDTO> {
  return postJson<unknown>(`/api/operator/announcements/${encodeURIComponent(announcementId)}/publish`, input, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function withdrawOperatorAnnouncement(announcementId: string, csrfToken: string, idempotencyKey: string): Promise<AnnouncementDTO> {
  return postJson<unknown>(`/api/operator/announcements/${encodeURIComponent(announcementId)}/withdraw`, {}, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function admitOperatorApplicationRevision(
  revision: unknown,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceApplicationRevisionAdmissionDTO> {
  return postJson<unknown>(
    "/api/operator/application-revisions", revision, csrfToken, idempotencyKey
  ).then(decodeDto<WorkspaceApplicationRevisionAdmissionDTO>);
}

export function createOperatorWorkspaceApplicationDeployment(
  workspaceId: string,
  applicationId: string,
  targetRevision: string,
  configuration: WorkspaceApplicationConfigurationDTO,
  csrfToken: string,
  idempotencyKey: string,
  secretBindings: WorkspaceApplicationSecretBindingDTO[] = [],
  revision?: unknown
): Promise<{ intent: WorkspaceApplicationIntentDTO }> {
  // An inline revision lets one deployment command carry the image and its run
  // requirements. The Control Plane admits that revision into its single
  // revision owner as part of the same command; sending the publisher
  // description is not a second, separate business step for the operator.
  return postJson<unknown>(
    "/api/operator/application-deployments",
    {
      workspaceId, applicationId, targetRevision, configuration,
      ...(secretBindings.length ? { secretBindings } : {}),
      ...(revision === undefined ? {} : { revision })
    },
    csrfToken, idempotencyKey
  ).then(decodeDto<{ intent: WorkspaceApplicationIntentDTO }>);
}

export function getOperatorWorkspaceApplicationDeployment(operationId: string, signal?: AbortSignal): Promise<SourceEnvelope<{ status: string; intent: WorkspaceApplicationIntentDTO }>> {
  return sourceGet<{ status: string; intent: WorkspaceApplicationIntentDTO }>(`/api/operator/application-deployments/${encodeURIComponent(operationId)}`, signal);
}

export function retryOperatorWorkspaceApplicationDeployment(operationId: string, csrfToken: string): Promise<{ intent: WorkspaceApplicationIntentDTO }> {
  return postJson<unknown>(`/api/operator/application-deployments/${encodeURIComponent(operationId)}/retry`, {}, csrfToken, `application-retry:${operationId}`)
    .then(decodeDto<{ intent: WorkspaceApplicationIntentDTO }>);
}

export interface WorkspaceRegistryRepositoryDTO {
  namespace: string;
  repository: string;
}

export interface WorkspaceRegistryRepositoryCatalogDTO {
  host: string;
  namespaces: string[];
  items: WorkspaceRegistryRepositoryDTO[];
}

export interface WorkspaceRegistryTagDTO {
  tag: string;
}

export interface WorkspaceRegistryResolutionDTO {
  // host is the registry endpoint Control Plane resolved against. The client
  // confirms the returned reference against it instead of assembling a second
  // reference format from repository and digest alone.
  host: string;
  namespace: string;
  repository: string;
  tag: string;
  digest: string;
  reference: string;
}

export function listOperatorRegistryRepositories(namespace: string): Promise<WorkspaceRegistryRepositoryCatalogDTO> {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return getJson<unknown>(`/api/operator/registry/repositories${query}`).then(decodeDto<WorkspaceRegistryRepositoryCatalogDTO>);
}

export function listOperatorRegistryTags(namespace: string, repository: string): Promise<{ namespace: string; repository: string; tags: WorkspaceRegistryTagDTO[] }> {
  return getJson<unknown>(`/api/operator/registry/tags/${encodeURIComponent(namespace)}/${encodeURIComponent(repository)}`)
    .then(decodeDto<{ namespace: string; repository: string; tags: WorkspaceRegistryTagDTO[] }>);
}

export function resolveOperatorRegistryImage(
  namespace: string,
  repository: string,
  tag: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceRegistryResolutionDTO> {
  return postJson<unknown>("/api/operator/registry/resolve", { namespace, repository, tag }, csrfToken, idempotencyKey)
    .then(decodeDto<WorkspaceRegistryResolutionDTO>);
}
