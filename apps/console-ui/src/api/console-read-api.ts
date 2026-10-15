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
  GatewayUsageSummaryDTO,
  GatewayWallet,
  GatewayWalletReadbackDTO,
  ManagementState,
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
  ReadinessFact,
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

export function getGatewayKeys(query: GatewayKeyListQuery = {}, signal?: AbortSignal): Promise<SourceEnvelope<GatewayKeyPageDTO>> {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  const suffix = params.size > 0 ? `?${params}` : "";
  return sourceGet<GatewayKeyPageDTO>(`/api/gateway/keys${suffix}`, signal);
}

export function getGatewayGroups(signal?: AbortSignal): Promise<SourceEnvelope<GatewayGroupPageDTO>> {
  return sourceGet<GatewayGroupPageDTO>("/api/gateway/groups", signal);
}

export function getGatewayEndpoint(signal?: AbortSignal): Promise<SourceEnvelope<GatewayEndpointDTO>> {
  return sourceGet<GatewayEndpointDTO>(cloudIdentity ? "/api/v2/gateway/endpoint" : "/api/gateway/endpoint", signal);
}

export function getGatewayKey(keyId: string, signal?: AbortSignal): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  return sourceGet<GatewayKeySummaryDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}`, signal);
}

export function createGatewayKey(input: CreateGatewayKeyRequest, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  return sourcePost<GatewayKeySummaryDTO>("/api/gateway/keys", input, csrfToken, idempotencyKey);
}

export function updateGatewayKey(keyId: string, input: UpdateGatewayKeyRequest, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  return sourcePatch<GatewayKeySummaryDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}`, input, csrfToken, idempotencyKey);
}

export function deleteGatewayKey(keyId: string, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<OperationStatusDTO>> {
  return sourceDelete<OperationStatusDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}`, csrfToken, idempotencyKey);
}

export function getGatewayBalanceHistory(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<GatewayBalanceHistoryPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<GatewayBalanceHistoryPageDTO>(`${cloudIdentity ? "/api/v2/gateway" : "/api/gateway"}/balance-history?${params}`, signal);
}

export function revealGatewayKey(keyId: string, csrfToken: string): Promise<SourceEnvelope<GatewayKeySecretDTO>> {
  return sourcePost<GatewayKeySecretDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}/reveal`, {}, csrfToken);
}

export function getGatewayKeyUsage(keyId: string, page = 1, pageSize = 20, period: GatewayUsagePeriod = "month", signal?: AbortSignal): Promise<SourceEnvelope<GatewayKeyUsagePageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize), period });
  return sourceGet<GatewayKeyUsagePageDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}/usage?${params}`, signal);
}

export function getGatewayKeyUsageSummary(keyId: string, period: GatewayUsagePeriod = "month", signal?: AbortSignal): Promise<SourceEnvelope<GatewayUsageSummaryDTO>> {
  return sourceGet<GatewayUsageSummaryDTO>(`/api/gateway/keys/${encodeURIComponent(keyId)}/usage-summary?${new URLSearchParams({ period })}`, signal);
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
    totalActualCostUsdMicros: decodeGatewayUsageCount(readback.totalActualCostUSDMicros, "actual_cost_usd_micros")
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
  return sourceGet<BillingReceiptPage>(`${cloudIdentity ? "/api/v2/legacy-billing" : "/api/billing"}/receipts?${params}`, signal);
}

/** Reads the owner's confirmed Workspace wallet movements for its fixed window. */
export function getWorkspaceSettlementTrend(signal?: AbortSignal): Promise<SourceEnvelope<WorkspaceSettlementTrend>> {
  return sourceGet<WorkspaceSettlementTrend>(cloudIdentity ? "/api/v2/legacy-billing/workspace-settlements" : "/api/billing/workspace-settlements", signal);
}

export function getBillingReceipt(receiptId: string, signal?: AbortSignal): Promise<SourceEnvelope<BillingReceipt>> {
  return sourceGet<BillingReceipt>(`${cloudIdentity ? "/api/v2/legacy-billing" : "/api/billing"}/receipts/${encodeURIComponent(receiptId)}`, signal);
}

export function getAnnouncements(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<AnnouncementPageDTO>> {
  return sourceGet<AnnouncementPageDTO>(`${cloudIdentity ? "/api/v2/announcements" : "/api/announcements"}?${new URLSearchParams({ page: String(page), pageSize: String(pageSize) })}`, signal);
}

export function markAnnouncementRead(announcementId: string, csrfToken: string, idempotencyKey: string): Promise<AnnouncementReadDTO> {
  return postJson<unknown>(`${cloudIdentity ? "/api/v2/announcements" : "/api/announcements"}/${encodeURIComponent(announcementId)}/read`, {}, csrfToken, idempotencyKey).then(decodeDto<AnnouncementReadDTO>);
}

export function getPricingCatalog(): Promise<PricingCatalogResponse> {
  return getJson<unknown>("/api/pricing/catalog").then(decodeDto<PricingCatalogResponse>);
}

export function previewPricing(input: PricingPreviewRequest, csrfToken: string): Promise<PricingPreviewResponse> {
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

export function getManagementState(): Promise<ManagementState> {
  return getJson<unknown>("/api/management/state").then(decodeDto<ManagementState>);
}

export function getRuntimeReadiness(): Promise<ReadinessFact> {
  return getJson<unknown>("/api/runtime/readiness").then(decodeDto<ReadinessFact>);
}

export function getProductionReadiness(): Promise<ReadinessFact> {
  return getJson<unknown>("/api/production/readiness").then(decodeDto<ReadinessFact>);
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
