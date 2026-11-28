import { decodeDto, decodeSource } from "./dtos.ts";
import { cloudIdentity } from "../app/console-identity.ts";
import type {
  AnnouncementPageDTO,
  AnnouncementDTO,
  AnnouncementReadDTO,
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
  OperatorPlatformAdminCommandDTO,
  OperatorProvisionAccountCommandDTO,
  OperatorAccountPageDTO,
  OperatorServiceEntitlementCommandDTO,
  OperatorWorkspacePurchaseEligibilityCommandDTO,
  OperatorAnnouncementPageDTO,
  OperatorHealthDTO,
  OperatorOverviewDTO,
  OperatorRuntimeObservationsDTO,
  OperatorReconciliationPageDTO,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  WalletAdjustmentOperationDTO,
  WalletAdjustmentRecoveryRequest,
  WalletAdjustmentRequest,
  WorkspaceLaunchRecoveryDTO,
  WorkspaceLaunchRecoveryRequest,
  OperatorAccountsData,
  OperationStatusDTO,
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

// The personal Gateway Key writes follow the baked identity, exactly like the
// personal reads above: a cloud build asks the Gateway owner through the BFF, and
// a retained build keeps the Control Plane route it authenticated against. This
// is a baked decision, not a fallback path.
export function personalGatewayKeyCreateRoute(cloud: boolean): "/api/v2/gateway/keys" | "/api/gateway/keys" {
  return cloud ? "/api/v2/gateway/keys" : "/api/gateway/keys";
}

// The update and the revoke address the same Key, so they share one path and
// differ by their method.
export function personalGatewayKeyUpdateRoute(cloud: boolean, keyId: string): string {
  return `${cloud ? "/api/v2" : "/api"}/gateway/keys/${encodeURIComponent(keyId)}`;
}

export function personalGatewayKeyRevokeRoute(cloud: boolean, keyId: string): string {
  return `${cloud ? "/api/v2" : "/api"}/gateway/keys/${encodeURIComponent(keyId)}`;
}

export function personalGatewayKeyRevealRoute(cloud: boolean, keyId: string): string {
  return `${cloud ? "/api/v2" : "/api"}/gateway/keys/${encodeURIComponent(keyId)}/reveal`;
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
// The Gateway owner answers an explicit reveal with the Key and its raw
// secret. The secret is handed back once and is never kept by the Console.
// The create answer carries the Key summary only: the plaintext never reaches
// the browser.
type PersonalGatewayKeySecretReadback = { key: PersonalGatewayKeyReadback; secret: string };
// The Gateway owner answers a revoke with its own operation. The operation is
// already terminal, because the owner confirms the Key's absence itself.
type PersonalGatewayOperationReadback = {
  operationId: string; kind: string; status: string; stage?: string; requestId?: string; createdAt?: string; updatedAt?: string;
};

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

// personalGatewayWrite carries one personal Key command to the Gateway owner and
// projects the owner's answer. It is the write counterpart of personalGatewayGet:
// the route is the owner's, the state is the owner's, and a failed command never
// becomes an available envelope.
async function personalGatewayWrite<Wire, Data>(request: () => Promise<Wire>, decode: (value: Wire) => Data): Promise<SourceEnvelope<Data>> {
  const data = decode(await request());
  return { source: "gateway", available: true, status: "available", fetchedAt: new Date().toISOString(), data };
}

// personalGatewayRequestMoney renders one exact USD micros amount as the decimal
// string the personal Gateway contract publishes. The amount is a safe integer,
// so the rendering never loses a digit.
function personalGatewayRequestMoney(value: number): string {
  if (!Number.isSafeInteger(value) || value < 0) throw new Error("invalid_personal_gateway_money");
  return String(value);
}

// personalGatewayCreateBody is the create command in the personal Gateway
// contract's own spelling: the money is an exact decimal string and the address
// lists are plain lists.
export function personalGatewayCreateBody(input: CreateGatewayKeyRequest): Record<string, unknown> {
  const body: Record<string, unknown> = {
    name: input.name,
    groupId: input.groupId,
    quotaUSDMicros: personalGatewayRequestMoney(input.quotaUsdMicros)
  };
  if (input.ipWhitelist) body.ipWhitelist = input.ipWhitelist;
  if (input.ipBlacklist) body.ipBlacklist = input.ipBlacklist;
  if (input.expiresInDays !== undefined) body.expiresInDays = input.expiresInDays;
  if (input.rateLimit5hUsdMicros !== undefined) body.rateLimit5hUSDMicros = personalGatewayRequestMoney(input.rateLimit5hUsdMicros);
  if (input.rateLimit1dUsdMicros !== undefined) body.rateLimit1dUSDMicros = personalGatewayRequestMoney(input.rateLimit1dUsdMicros);
  if (input.rateLimit7dUsdMicros !== undefined) body.rateLimit7dUSDMicros = personalGatewayRequestMoney(input.rateLimit7dUsdMicros);
  return body;
}

// personalGatewayUpdateBody is the update command in the personal Gateway
// contract's own spelling. An absent property leaves that fact unchanged, an
// address list replaces the Key's list, and an empty expiry is stated as the
// explicit instruction to clear it.
export function personalGatewayUpdateBody(input: UpdateGatewayKeyRequest): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  if (input.name !== undefined) body.name = input.name;
  if (input.groupId !== undefined) body.groupId = input.groupId;
  if (input.ipWhitelist !== undefined) body.ipWhitelist = { values: input.ipWhitelist };
  if (input.ipBlacklist !== undefined) body.ipBlacklist = { values: input.ipBlacklist };
  if (input.quotaUsdMicros !== undefined) body.quotaUSDMicros = personalGatewayRequestMoney(input.quotaUsdMicros);
  if (input.expiresAt !== undefined) {
    if (input.expiresAt) body.expiresAt = new Date(input.expiresAt).toISOString();
    else body.clearExpiresAt = true;
  }
  if (input.rateLimit5hUsdMicros !== undefined) body.rateLimit5hUSDMicros = personalGatewayRequestMoney(input.rateLimit5hUsdMicros);
  if (input.rateLimit1dUsdMicros !== undefined) body.rateLimit1dUSDMicros = personalGatewayRequestMoney(input.rateLimit1dUsdMicros);
  if (input.rateLimit7dUsdMicros !== undefined) body.rateLimit7dUSDMicros = personalGatewayRequestMoney(input.rateLimit7dUsdMicros);
  if (input.resetQuota !== undefined) body.resetQuota = input.resetQuota;
  if (input.resetRateLimitUsage !== undefined) body.resetRateLimitUsage = input.resetRateLimitUsage;
  if (input.enabled !== undefined) body.enabled = input.enabled;
  return body;
}

// decodePersonalGatewayKeySecret projects the owner's create or reveal answer
// into the Console's secret vocabulary. The secret is carried as it is and is
// never retained beyond this answer.
export function decodePersonalGatewayKeySecret(value: PersonalGatewayKeySecretReadback): GatewayKeySecretDTO {
  const key = decodePersonalGatewayKey(value.key);
  if (typeof value.secret !== "string" || !value.secret) throw new Error("invalid_personal_gateway_secret");
  return { id: key.id, name: key.name, status: key.status, value: value.secret };
}

// decodePersonalGatewayKeyOperation projects the owner's terminal revoke operation
// into the Console's delete vocabulary. The owner confirmed the Key's absence
// itself, so the delete is the reported outcome and not the Console's guess.
export function decodePersonalGatewayKeyOperation(value: PersonalGatewayOperationReadback): OperationStatusDTO {
  if (typeof value.operationId !== "string" || !value.operationId || value.kind !== "revoke_key" || value.status !== "succeeded") {
    throw new Error("invalid_personal_gateway_operation");
  }
  return {
    operationId: value.operationId,
    status: "deleted",
    ...(value.stage ? { phase: value.stage } : {}),
    ...(value.createdAt ? { createdAt: value.createdAt } : {}),
    ...(value.updatedAt ? { updatedAt: value.updatedAt } : {})
  };
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
  // The baked identity decides the owner. A cloud build asks the Gateway owner
  // through the BFF, so the Key is created by the owner that owns Gateway Key
  // mappings; a retained build keeps the Control Plane route it authenticated
  // against. This is a baked decision, not a fallback path.
  if (!cloudIdentity) return sourcePost<GatewayKeySummaryDTO>(personalGatewayKeyCreateRoute(false), input, csrfToken, idempotencyKey);
  return personalGatewayWrite(
    // The owner's create answer is the Key summary. The plaintext never crosses
    // this boundary: the customer takes the secret from the explicit reveal,
    // which is the surface that hands a secret back once.
    () => postJson<PersonalGatewayKeyReadback>(personalGatewayKeyCreateRoute(true), personalGatewayCreateBody(input), csrfToken, idempotencyKey),
    decodePersonalGatewayKey
  );
}

export function updateGatewayKey(keyId: string, input: UpdateGatewayKeyRequest, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<GatewayKeySummaryDTO>> {
  if (!cloudIdentity) return sourcePatch<GatewayKeySummaryDTO>(personalGatewayKeyUpdateRoute(false, keyId), input, csrfToken, idempotencyKey);
  return personalGatewayWrite(
    () => patchJson<PersonalGatewayKeyReadback>(personalGatewayKeyUpdateRoute(true, keyId), personalGatewayUpdateBody(input), csrfToken, idempotencyKey),
    decodePersonalGatewayKey
  );
}

export function deleteGatewayKey(keyId: string, csrfToken: string, idempotencyKey: string): Promise<SourceEnvelope<OperationStatusDTO>> {
  if (!cloudIdentity) return sourceDelete<OperationStatusDTO>(personalGatewayKeyRevokeRoute(false, keyId), csrfToken, idempotencyKey);
  return personalGatewayWrite(
    () => deleteJson<PersonalGatewayOperationReadback>(personalGatewayKeyRevokeRoute(true, keyId), csrfToken, idempotencyKey),
    decodePersonalGatewayKeyOperation
  );
}

export function getGatewayBalanceHistory(page = 1, pageSize = 20, signal?: AbortSignal): Promise<SourceEnvelope<GatewayBalanceHistoryPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<GatewayBalanceHistoryPageDTO>(`${cloudIdentity ? "/api/v2/gateway" : "/api/gateway"}/balance-history?${params}`, signal);
}

export function revealGatewayKey(keyId: string, csrfToken: string): Promise<SourceEnvelope<GatewayKeySecretDTO>> {
  if (!cloudIdentity) return sourcePost<GatewayKeySecretDTO>(personalGatewayKeyRevealRoute(false, keyId), {}, csrfToken);
  // The BFF guards every write with the caller's own idempotency key, and the
  // retained route never carried one. A reveal asks the owner again for the
  // secret, so a fresh key names this one reveal.
  return personalGatewayWrite(
    () => postJson<PersonalGatewayKeySecretReadback>(personalGatewayKeyRevealRoute(true, keyId), {}, csrfToken, crypto.randomUUID()),
    decodePersonalGatewayKeySecret
  );
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

// The Control Plane published a package-pricing catalog and a price preview
// until the R6 withdrawal. The Resource Catalog is the pricing owner now: it
// publishes the plan reads and quotes a purchase (`POST /api/v2/quotes`), and
// the cloud launch surface prices through it. The retained Console reads an
// accepted operation's own terms instead, so no price is lost or invented.

// The operator console reads resolve exactly like the wallet and billing reads:
// a cloud build asks the BFF, which authorizes the CloudIdentity session as a
// platform operator; a retained build keeps the Control Plane route it
// authenticated against. The decision is baked at build time, never a runtime
// fallback.
export function operatorReadRoute(cloud: boolean): "/api/v2/operator" | "/api/operator" {
  return cloud ? "/api/v2/operator" : "/api/operator";
}

// Only activation has a Cloud BFF command owner. Other retained operator
// commands remain available to the legacy identity; a Cloud session must never
// be sent to those state-changing Control Plane routes.
export function operatorWriteRoute(cloud: boolean, path: string): string {
  if (!path.startsWith("/") || path.startsWith("//")) throw new Error("operator_write_path_invalid");
  if (cloud) throw new Error("operator_write_unavailable");
  return `/api/operator${path}`;
}

export function operatorActivationRoute(cloud: boolean): "/api/v2/operator/accounts" {
  if (!cloud) throw new Error("activation_moved");
  return "/api/v2/operator/accounts";
}

export function operatorPurchaseEligibilityRoute(cloud: boolean, targetId: string): string {
  const encoded = encodeURIComponent(targetId);
  return cloud
    ? `/api/v2/operator/accounts/${encoded}/workspace-purchase-eligibility`
    : `/api/operator/accounts/${encoded}/workspace-purchase-eligibility`;
}

export function operatorServiceEntitlementRoute(cloud: boolean, targetId: string, serviceId: string): string {
  if (!cloud) throw new Error("service_entitlements_cloud_only");
  return `/api/v2/operator/accounts/${encodeURIComponent(targetId)}/service-entitlements/${encodeURIComponent(serviceId)}`;
}

export function operatorPlatformAdminRoute(cloud: boolean, targetId: string): string {
  if (!cloud) throw new Error("operator_write_unavailable");
  return `/api/v2/operator/accounts/${encodeURIComponent(targetId)}/platform-admin`;
}

export function getOperatorAccounts(cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorAccountsData>> {
  return sourceGet<OperatorAccountsData>(`${operatorReadRoute(cloud)}/accounts`);
}

export function getOperatorOverview(signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorOverviewDTO>> {
  return sourceGet<OperatorOverviewDTO>(`${operatorReadRoute(cloud)}/overview`, signal);
}

export function getOperatorAccountsPage(page = 1, pageSize = 20, signal?: AbortSignal, cloud: boolean = cloudIdentity, search?: string): Promise<SourceEnvelope<OperatorAccountPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  const trimmedSearch = search?.trim();
  if (trimmedSearch) params.set("search", trimmedSearch);
  return sourceGet<OperatorAccountPageDTO>(`${operatorReadRoute(cloud)}/accounts?${params}`, signal);
}

export function getOperatorWorkspaces(page = 1, pageSize = 20, signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorWorkspacePageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorWorkspacePageDTO>(`${operatorReadRoute(cloud)}/workspaces?${params}`, signal);
}

export function getOperatorWorkspace(workspaceId: string, signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorWorkspaceDTO>> {
  return sourceGet<OperatorWorkspaceDTO>(`${operatorReadRoute(cloud)}/workspaces/${encodeURIComponent(workspaceId)}`, signal);
}

export function getOperatorReconciliation(page = 1, pageSize = 20, signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorReconciliationPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorReconciliationPageDTO>(`${operatorReadRoute(cloud)}/reconciliation?${params}`, signal);
}

export function getOperatorHealth(signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorHealthDTO>> {
  return sourceGet<OperatorHealthDTO>(`${operatorReadRoute(cloud)}/health`, signal);
}

export function getOperatorRuntimeObservations(signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorRuntimeObservationsDTO>> {
  return sourceGet<OperatorRuntimeObservationsDTO>(`${operatorReadRoute(cloud)}/runtime-observations`, signal);
}

export function getOperatorAnnouncements(page = 1, pageSize = 20, signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<SourceEnvelope<OperatorAnnouncementPageDTO>> {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceGet<OperatorAnnouncementPageDTO>(`${operatorReadRoute(cloud)}/announcements?${params}`, signal);
}

export function createWalletAdjustment(accountId: string, input: WalletAdjustmentRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<WalletAdjustmentOperationDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/accounts/${encodeURIComponent(accountId)}/wallet-adjustments`), input, csrfToken, idempotencyKey).then(decodeDto<WalletAdjustmentOperationDTO>);
}

export function getWalletAdjustment(operationId: string, signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<WalletAdjustmentOperationDTO> {
  return getJson<unknown>(`${operatorReadRoute(cloud)}/wallet-adjustments/${encodeURIComponent(operationId)}`, { signal }).then(decodeDto<WalletAdjustmentOperationDTO>);
}

export function recoverWalletAdjustment(operationId: string, input: WalletAdjustmentRecoveryRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<WalletAdjustmentOperationDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/wallet-adjustments/${encodeURIComponent(operationId)}/recover`), input, csrfToken, idempotencyKey).then(decodeDto<WalletAdjustmentOperationDTO>);
}

export function provisionOperatorAccount(input: ProvisionAccountRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<OperatorProvisionAccountCommandDTO> {
  return postJson<unknown>(operatorActivationRoute(cloud), input, csrfToken, idempotencyKey).then(decodeDto<OperatorProvisionAccountCommandDTO>);
}

export function disableOperatorAccount(accountId: string, reason: string, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<OperatorAccountCommandDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/accounts/${encodeURIComponent(accountId)}/disable`), { confirmationAccountId: accountId, reason }, csrfToken, idempotencyKey).then(decodeDto<OperatorAccountCommandDTO>);
}

export function setOperatorWorkspacePurchaseEligibility(accountId: string, enabled: boolean, reason: string, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<OperatorWorkspacePurchaseEligibilityCommandDTO> {
  return postJson<unknown>(operatorPurchaseEligibilityRoute(cloud, accountId), { confirmationAccountId: accountId, enabled, reason }, csrfToken, idempotencyKey).then(decodeDto<OperatorWorkspacePurchaseEligibilityCommandDTO>);
}

export function setOperatorServiceEntitlement(accountId: string, serviceId: string, enabled: boolean, reason: string, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<OperatorServiceEntitlementCommandDTO> {
  return postJson<unknown>(operatorServiceEntitlementRoute(cloud, accountId, serviceId), { confirmationAccountId: accountId, enabled, reason }, csrfToken, idempotencyKey).then(decodeDto<OperatorServiceEntitlementCommandDTO>);
}

export function setOperatorPlatformAdmin(accountId: string, enabled: boolean, reason: string, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<OperatorPlatformAdminCommandDTO> {
  return postJson<unknown>(operatorPlatformAdminRoute(cloud, accountId), { confirmationAccountId: accountId, enabled, reason }, csrfToken, idempotencyKey).then(decodeDto<OperatorPlatformAdminCommandDTO>);
}

export function resolveBillingReview(resourceType: string, resourceId: string, input: BillingReviewResolutionRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<OperationStatusDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/billing-reviews/${encodeURIComponent(resourceType)}/${encodeURIComponent(resourceId)}/resolve`), input, csrfToken, idempotencyKey).then(decodeDto<OperationStatusDTO>);
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

export function getWorkspaceLaunchRecovery(operationId: string, signal?: AbortSignal, cloud: boolean = cloudIdentity): Promise<WorkspaceLaunchRecoveryDTO> {
  return getJson<unknown>(`${operatorReadRoute(cloud)}/workspace-launches/${encodeURIComponent(operationId)}/recovery`, { signal })
    .then((value) => decodeWorkspaceLaunchRecovery(value, operationId));
}

export function recoverWorkspaceLaunch(operationId: string, input: WorkspaceLaunchRecoveryRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<WorkspaceLaunchRecoveryDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/workspace-launches/${encodeURIComponent(operationId)}/recover`), input, csrfToken, idempotencyKey)
    .then((value) => decodeWorkspaceLaunchRecovery(value, operationId));
}

export function createOperatorAnnouncement(input: AnnouncementDraftRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<AnnouncementDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, "/announcements"), input, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function updateOperatorAnnouncement(announcementId: string, input: AnnouncementDraftRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<AnnouncementDTO> {
  return putJson<unknown>(operatorWriteRoute(cloud, `/announcements/${encodeURIComponent(announcementId)}`), input, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function publishOperatorAnnouncement(announcementId: string, input: AnnouncementScheduleRequest, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<AnnouncementDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/announcements/${encodeURIComponent(announcementId)}/publish`), input, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}

export function withdrawOperatorAnnouncement(announcementId: string, csrfToken: string, idempotencyKey: string, cloud: boolean = cloudIdentity): Promise<AnnouncementDTO> {
  return postJson<unknown>(operatorWriteRoute(cloud, `/announcements/${encodeURIComponent(announcementId)}/withdraw`), {}, csrfToken, idempotencyKey).then(decodeDto<AnnouncementDTO>);
}
