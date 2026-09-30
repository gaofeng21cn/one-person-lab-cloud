import { decodeDto, decodeSource } from "./dtos.ts";
import type {
  AvailableSource,
  RuntimeCredentialResponse,
  RuntimeCredentialRotationResponse,
  SourceEnvelope,
  WorkspaceLaunchRequest,
  WorkspaceLaunchListResponse,
  WorkspaceLaunchResponse,
  WorkspaceDeleteCommandResult,
  WorkspaceDeleteResponse,
  WorkspaceDeletionDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceGatewayBudgetUpdateRequest,
  WorkspaceListData,
  WorkspaceDTO,
  WorkspaceRenewalRequest,
  WorkspaceRenewalResponse,
  WorkspaceRenewalReadDTO,
  WorkspaceRuntimeDTO
} from "./dtos.ts";
import type { WorkspaceApplicationInstallationDTO } from "./dtos.ts";
import { deleteJson, postJson, getJson, patchJson, type ApiError } from "./console-api.ts";
import type {
  CapabilityVersionPageDTO,
  ComputePlanPageDTO,
  GatewayWalletReadbackDTO,
  LaunchModelPageDTO,
  RuntimeVersionPageDTO,
  StoragePlanPageDTO,
  WorkspaceOwnerAccessDTO,
  WorkspaceOwnerDTO,
  WorkspaceOwnerOperationDTO,
  WorkspaceQuoteDTO,
  WorkspaceQuoteRequestDTO
} from "./dtos.ts";

function requirePage<T>(value: unknown, name: string): { items: T[]; nextCursor?: string } {
  if (!value || typeof value !== "object" || !Array.isArray((value as { items?: unknown }).items)) {
    throw new Error(`invalid_${name}_page`);
  }
  const page = value as { items: T[]; nextCursor?: unknown };
  if (page.nextCursor !== undefined && (typeof page.nextCursor !== "string" || !page.nextCursor.trim())) {
    throw new Error(`invalid_${name}_cursor`);
  }
  return { items: page.items, ...(typeof page.nextCursor === "string" ? { nextCursor: page.nextCursor } : {}) };
}

async function listAllOwnerPages<T>(path: string, name: string): Promise<T[]> {
  const items: T[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | undefined;
  do {
    const separator = path.includes("?") ? "&" : "?";
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) query.set("cursor", cursor);
    const page = requirePage<T>(await getJson<unknown>(`${path}${separator}${query}`), name);
    items.push(...page.items);
    cursor = page.nextCursor;
    if (cursor && seenCursors.has(cursor)) throw new Error(`repeated_${name}_cursor`);
    if (cursor) seenCursors.add(cursor);
  } while (cursor);
  return items;
}

// F07 owner reads. These paths intentionally target the v2 BFF only. If an
// owner route is not deployed yet, the request rejects and the launch remains
// unavailable instead of consulting legacy catalog data or fixtures.
export async function listReadyCapabilityVersions(): Promise<CapabilityVersionPageDTO["items"]> {
  return listAllOwnerPages<CapabilityVersionPageDTO["items"][number]>("/api/v2/capability-versions?status=ready", "capability_version");
}

// listApprovedRuntimeVersions reads the approved default-OPL-App releases. The
// Runtime Control owner is the authority; an unapproved release is never offered
// as a choosable default App.
export async function listApprovedRuntimeVersions(): Promise<RuntimeVersionPageDTO["items"]> {
  return listAllOwnerPages<RuntimeVersionPageDTO["items"][number]>("/api/v2/catalog/runtime-versions", "runtime_version");
}

export async function listAvailableComputePlans(): Promise<ComputePlanPageDTO["items"]> {
  return listAllOwnerPages<ComputePlanPageDTO["items"][number]>("/api/v2/catalog/compute-plans", "compute_plan");
}

export async function listAvailableStoragePlans(): Promise<StoragePlanPageDTO["items"]> {
  return listAllOwnerPages<StoragePlanPageDTO["items"][number]>("/api/v2/catalog/storage-plans", "storage_plan");
}

export async function listAvailableLaunchModels(): Promise<LaunchModelPageDTO["items"]> {
  return listAllOwnerPages<LaunchModelPageDTO["items"][number]>("/api/v2/catalog/models", "model");
}

export async function getLaunchWallet(): Promise<GatewayWalletReadbackDTO> {
  const wallet = await getJson<unknown>("/api/v2/wallet");
  if (!wallet || typeof wallet !== "object") throw new Error("invalid_gateway_wallet_readback");
  const value = wallet as Record<string, unknown>;
  if (value.source !== "gateway" || value.status !== "available" || value.currency !== "USD"
    || typeof value.balanceUSDMicros !== "string" || !/^(0|[1-9]\d*)$/.test(value.balanceUSDMicros)
    || typeof value.fetchedAt !== "string" || !value.fetchedAt.trim()) {
    throw new Error("invalid_gateway_wallet_readback");
  }
  return value as unknown as GatewayWalletReadbackDTO;
}

export async function createWorkspaceQuote(
  input: WorkspaceQuoteRequestDTO,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceQuoteDTO> {
  const quote = await postJson<unknown>("/api/v2/quotes", input, csrfToken, idempotencyKey);
  if (!quote || typeof quote !== "object") throw new Error("invalid_workspace_quote");
  const value = quote as WorkspaceQuoteDTO;
  // The owner freezes exactly the selection the caller sent: a built Agent returns
  // the same CapabilityVersion, the default App returns the same Runtime Release.
  const selectionMatches = input.applicationSelection.kind === "agent"
    ? value.capabilityVersionId === input.applicationSelection.capabilityVersionId && value.runtimeVersionId === undefined
    : value.runtimeVersionId === input.applicationSelection.runtimeVersionId && value.capabilityVersionId === undefined;
  if (!value.id || value.purpose !== "deploy" || value.status !== "offered"
    || !selectionMatches
    || value.computePlanId !== input.computePlanId || value.storagePlanId !== input.storagePlanId
    || value.periodMonths !== 1 || !Array.isArray(value.modelSelections)
    || value.modelSelections.length !== input.modelSelections.length
    || typeof value.totalUSDMicros !== "string" || !/^(0|[1-9]\d*)$/.test(value.totalUSDMicros)
    || !value.expiresAt || !value.periodStart || !value.periodEnd
    || !value.pricePolicyVersionId || !value.refundPolicyVersionId || !value.retentionPolicyVersionId
    || !value.refundTerms || !value.retentionTerms || !Array.isArray(value.lineItems)) {
    throw new Error("invalid_workspace_quote");
  }
  return value;
}

export async function getWorkspaceQuote(quoteId: string): Promise<WorkspaceQuoteDTO> {
  const value = await getJson<unknown>(`/api/v2/quotes/${encodeURIComponent(quoteId)}`);
  if (!value || typeof value !== "object" || (value as WorkspaceQuoteDTO).id !== quoteId) {
    throw new Error("invalid_workspace_quote_readback");
  }
  return value as WorkspaceQuoteDTO;
}

export async function createAgentWorkspace(
  input: { name: string; quoteId: string; renewalMode: "manual" | "automatic"; automaticRenewalConsent?: true },
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceOwnerOperationDTO> {
  const value = await postJson<unknown>("/api/v2/workspaces", input, csrfToken, idempotencyKey, 60_000);
  if (!value || typeof value !== "object") throw new Error("invalid_workspace_operation");
  const operation = value as WorkspaceOwnerOperationDTO;
  if (!operation.operationId || operation.owner !== "workspace" || operation.kind !== "create_workspace"
    || !operation.resourceId || !operation.status || !operation.stage) {
    throw new Error("invalid_workspace_operation");
  }
  return operation;
}

export async function getWorkspaceOwnerOperation(operationId: string): Promise<WorkspaceOwnerOperationDTO> {
  const value = await getJson<unknown>(`/api/v2/operations/workspace/${encodeURIComponent(operationId)}`);
  if (!value || typeof value !== "object") throw new Error("invalid_workspace_operation_readback");
  const operation = value as WorkspaceOwnerOperationDTO;
  if (operation.operationId !== operationId || operation.owner !== "workspace" || !operation.status || !operation.stage) {
    throw new Error("invalid_workspace_operation_readback");
  }
  return operation;
}

export async function getWorkspaceOwner(workspaceId: string): Promise<WorkspaceOwnerDTO> {
  const value = await getJson<unknown>(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`);
  if (!value || typeof value !== "object" || (value as WorkspaceOwnerDTO).id !== workspaceId) {
    throw new Error("invalid_workspace_readback");
  }
  return value as WorkspaceOwnerDTO;
}

export async function listWorkspaceOwnerRows(): Promise<WorkspaceOwnerDTO[]> {
  return listAllOwnerPages<WorkspaceOwnerDTO>("/api/v2/workspaces", "workspace");
}

// The Workspace owner is the authority for a cloud-identity Console's customer
// Workspace read. That Workspace lives in the owner's own database, so the
// Control Plane projection the legacy Console lists from does not contain it.
// The owner pages by cursor and carries neither the Control Plane plan fields
// nor an owner-account pair, so the complete owner list is projected once into
// the Console's page model and every column the owner does not return is
// rendered as unavailable rather than filled from another owner.
export const CUSTOMER_WORKSPACE_OWNER_SOURCE = "workspace";

export function projectCustomerWorkspace(workspace: WorkspaceOwnerDTO): WorkspaceDTO {
  if (!workspace.id || !workspace.createdAt || !workspace.updatedAt) {
    throw new Error("invalid_workspace_owner_identity");
  }
  return {
    id: workspace.id,
    state: workspace.status,
    createdAt: workspace.createdAt,
    updatedAt: workspace.updatedAt,
    ...(workspace.name ? { name: workspace.name } : {}),
    ...(workspace.accessUrl ? { url: workspace.accessUrl } : {}),
    ...(workspace.currentPeriodEnd ? { paidThrough: workspace.currentPeriodEnd } : {}),
    ...(workspace.deliveryModel ? { deliveryModel: workspace.deliveryModel } : {}),
    ...(workspace.resourceReadiness ? { resourceReadiness: workspace.resourceReadiness } : {}),
    ...(workspace.applicationAvailability ? { applicationAvailability: workspace.applicationAvailability } : {})
  };
}

export async function readCustomerWorkspaceOwnerList(
  page: number,
  pageSize: number
): Promise<SourceEnvelope<WorkspaceListData>> {
  if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1) {
    throw new Error("customer_workspace_page_invalid");
  }
  const items = (await listWorkspaceOwnerRows()).map(projectCustomerWorkspace);
  return {
    source: CUSTOMER_WORKSPACE_OWNER_SOURCE,
    status: items.length === 0 ? "empty" : "available",
    available: true,
    fetchedAt: new Date().toISOString(),
    data: {
      items: items.slice((page - 1) * pageSize, (page - 1) * pageSize + pageSize),
      total: items.length,
      page,
      pageSize
    }
  };
}

export async function readCustomerWorkspaceOwnerDetail(
  workspaceId: string
): Promise<SourceEnvelope<WorkspaceDTO | null>> {
  const workspace = projectCustomerWorkspace(await getWorkspaceOwner(workspaceId));
  return {
    source: CUSTOMER_WORKSPACE_OWNER_SOURCE,
    status: "available",
    available: true,
    fetchedAt: new Date().toISOString(),
    data: workspace
  };
}

export async function getWorkspaceOwnerAccess(
  workspaceId: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceOwnerAccessDTO> {
  const value = await postJson<unknown>(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/access`, {}, csrfToken, idempotencyKey);
  if (!value || typeof value !== "object") throw new Error("invalid_workspace_access_readback");
  const access = value as WorkspaceOwnerAccessDTO;
  if (access.workspaceId !== workspaceId || typeof access.url !== "string" || !/^https?:\/\//.test(access.url)) {
    throw new Error("invalid_workspace_access_readback");
  }
  return access;
}

const terminalLaunchStatuses = new Set(["succeeded", "failed", "refunded"]);

export function resumeWorkspaceApplicationInstallation(workspaceId: string, operationId: string, csrfToken: string): Promise<{ workspaceId: string; applicationInstallation: WorkspaceApplicationInstallationDTO | null }> {
  return postJson<unknown>(`/api/workspaces/${encodeURIComponent(workspaceId)}/application-installation/resume`, {}, csrfToken, `application-resume:${operationId}`)
    .then(decodeDto<{ workspaceId: string; applicationInstallation: WorkspaceApplicationInstallationDTO | null }>);
}
const workspaceGatewayBudgetStatuses = new Set(["active", "disabled", "quota_exhausted", "expired"]);
const workspaceGatewayBudgetFields = [
  "workspaceId", "keyId", "status", "quotaUsdMicros", "quotaUsedUsdMicros",
  "rateLimit5hUsdMicros", "rateLimit1dUsdMicros", "rateLimit7dUsdMicros",
  "usage5hUsdMicros", "usage1dUsdMicros", "usage7dUsdMicros", "enabled", "updatedAt"
] as const;
const workspaceGatewayBudgetMicrosFields = [
  "quotaUsdMicros", "quotaUsedUsdMicros", "rateLimit5hUsdMicros", "rateLimit1dUsdMicros",
  "rateLimit7dUsdMicros", "usage5hUsdMicros", "usage1dUsdMicros", "usage7dUsdMicros"
] as const;

async function sourceRequest<T>(request: () => Promise<unknown>): Promise<SourceEnvelope<T>> {
  try {
    return decodeSource<T>(await request());
  } catch (error) {
    const payload = (error as ApiError).payload;
    if (payload !== undefined) {
      try {
        return decodeSource<T>(payload);
      } catch {
        // Preserve the original error when no valid source envelope was returned.
      }
    }
    throw error;
  }
}

function isInt64DecimalString(value: unknown, positive = false): value is string {
  if (typeof value !== "string" || !/^(0|[1-9]\d*)$/.test(value) || positive && value === "0") return false;
  return value.length < 19 || value.length === 19 && value <= "9223372036854775807";
}

function hasExactFields(value: Record<string, unknown>, fields: readonly string[]) {
  const actual = Object.keys(value).sort();
  const expected = [...fields].sort();
  return actual.length === expected.length && actual.every((field, index) => field === expected[index]);
}

async function workspaceGatewayBudgetSourceRequest(
  request: () => Promise<unknown>,
  workspaceId: string,
  keyId: string
): Promise<SourceEnvelope<WorkspaceGatewayBudgetDTO>> {
  if (!workspaceId || !isInt64DecimalString(keyId, true)) throw new Error("invalid_workspace_gateway_budget_identity");
  const source = await sourceRequest<Record<string, unknown>>(request);
  if (source.source !== "sub2api") throw new Error("invalid_workspace_gateway_budget_source");
  if (source.available === false) return source;
  const data = source.data;
  if (source.status !== "available" || !data || !hasExactFields(data, workspaceGatewayBudgetFields)
    || data.workspaceId !== workspaceId || data.keyId !== keyId
    || !isInt64DecimalString(data.keyId, true)
    || typeof data.status !== "string" || !workspaceGatewayBudgetStatuses.has(data.status)
    || typeof data.enabled !== "boolean"
    || data.updatedAt !== null && (typeof data.updatedAt !== "string" || !data.updatedAt.trim())
    || workspaceGatewayBudgetMicrosFields.some((field) => !isInt64DecimalString(data[field]))) {
    throw new Error("invalid_workspace_gateway_budget_source");
  }
  return { ...source, data: data as unknown as WorkspaceGatewayBudgetDTO };
}

export function isTerminalWorkspaceLaunch(status: string): boolean {
  return terminalLaunchStatuses.has(status);
}

export function workspaceLaunchIdempotencyKey(): string {
  return `workspace-launch:${crypto.randomUUID()}`;
}

export function workspaceDeleteIdempotencyKey(workspaceId: string): string {
  return `workspace-delete:${workspaceId}:${crypto.randomUUID()}`;
}

export async function launchWorkspace(
  input: WorkspaceLaunchRequest,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceLaunchResponse> {
  try {
    return decodeDto<WorkspaceLaunchResponse>(await postJson<unknown>("/api/workspace-launches", input, csrfToken, idempotencyKey, 60_000));
  } catch (error) {
    const apiError = error as ApiError;
    if (apiError.payload !== undefined) throw error;
    const unknown: ApiError = new Error("workspace_launch_unknown", { cause: error });
    unknown.payload = { status: "unknown", retryable: true };
    throw unknown;
  }
}

export function getWorkspaceLaunch(operationId: string): Promise<WorkspaceLaunchResponse> {
  return getJson<unknown>(`/api/workspace-launches/${encodeURIComponent(operationId)}`).then(decodeDto<WorkspaceLaunchResponse>);
}

export function getWorkspaceLaunches(): Promise<WorkspaceLaunchListResponse> {
  return getJson<unknown>("/api/workspace-launches").then((value) => {
    if (!Array.isArray(value)) throw new Error("invalid_workspace_launch_list");
    return value.map(decodeDto<WorkspaceLaunchResponse>);
  });
}

function workspaceDeleteUnavailable(error: ApiError): boolean {
  if (error.status === 405 || error.status === 501) return true;
  if (error.status !== 404) return false;
  const payload = error.payload && typeof error.payload === "object"
    ? error.payload as Record<string, unknown>
    : null;
  return payload?.error !== "workspace_not_found";
}

export async function deleteWorkspace(
  workspaceId: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceDeleteCommandResult> {
  try {
    const dto = decodeDto<Record<string, unknown>>(await deleteJson<unknown>(
      `/api/workspaces/${encodeURIComponent(workspaceId)}`,
      csrfToken,
      idempotencyKey
    ));
    if (dto.workspaceId !== workspaceId || typeof dto.status !== "string" || !dto.status.trim()) {
      throw new Error("invalid_workspace_delete_response");
    }
    return {
      available: true,
      data: {
        workspaceId,
        status: dto.status,
        ...(typeof dto.operationId === "string" && dto.operationId ? { operationId: dto.operationId } : {})
      } satisfies WorkspaceDeleteResponse
    };
  } catch (error) {
    if (workspaceDeleteUnavailable(error as ApiError)) {
      return { available: false, reasonCode: "workspace_delete_unavailable" };
    }
    throw error;
  }
}

const workspaceDeletionStages = ["runtime_absent", "attachment_absent", "storage_absent", "compute_absent", "workspace_absent", "receipt_recorded"];
const workspaceDeletionPageStates = ["waiting", "retrying", "blocked", "completed"];
const workspaceDeleteRefundStates = ["blocked", "pending", "manual_review", "succeeded", "not_due"];

export async function getWorkspaceDeletion(workspaceId: string): Promise<WorkspaceDeletionDTO | null> {
  const value = await getJson<unknown>(`/api/workspaces/${encodeURIComponent(workspaceId)}/deletion`);
  if (value === null) return null;
  const dto = decodeDto<WorkspaceDeletionDTO>(value);
  if (dto.workspaceId !== workspaceId || !dto.operationId?.trim() || !dto.phase?.trim()
    || !["pending", "manual_review", "deleted"].includes(dto.status)) throw new Error("invalid_workspace_deletion_response");
  // A published stage and page state must come from the platform vocabulary, so a
  // response carrying an unknown token is rejected instead of rendered.
  if (dto.stage !== undefined && !workspaceDeletionStages.includes(dto.stage)) throw new Error("invalid_workspace_deletion_response");
  if (dto.pageState !== undefined && !workspaceDeletionPageStates.includes(dto.pageState)) throw new Error("invalid_workspace_deletion_response");
  if (dto.refundStatus !== undefined && !workspaceDeleteRefundStates.includes(dto.refundStatus)) throw new Error("invalid_workspace_deletion_response");
  // A published timestamp must be a real time, so a malformed readback time is
  // rejected instead of rendered as a plausible-looking value.
  for (const value of [dto.lastReadbackAt, dto.nextRetryAt]) {
    if (value !== undefined && Number.isNaN(new Date(value).getTime())) throw new Error("invalid_workspace_deletion_response");
  }
  return dto;
}

export async function getWorkspaceRenewal(workspaceId: string): Promise<WorkspaceRenewalReadDTO> {
  const dto = decodeDto<WorkspaceRenewalReadDTO>(await getJson<unknown>(`/api/workspaces/${encodeURIComponent(workspaceId)}/renewal`));
  if (!dto.recovery || !["not_required", "recoverable", "pending", "unavailable", "reclaimed"].includes(dto.recovery.state)
    || typeof dto.recovery.reason !== "string" || typeof dto.autoRenew !== "boolean"
    || typeof dto.paidThrough !== "string" || typeof dto.renewalStatus !== "string") throw new Error("invalid_workspace_renewal_response");
  return dto;
}

export function getWorkspaces(page = 1, pageSize = 20): Promise<SourceEnvelope<WorkspaceListData>> {
  const query = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  return sourceRequest<WorkspaceListData>(() => getJson<unknown>(`/api/workspaces?${query}`));
}

export async function findWorkspaceInPages(
  workspaceId: string,
  pageSize = 50
): Promise<SourceEnvelope<WorkspaceDTO | null>> {
  if (!Number.isSafeInteger(pageSize) || pageSize < 1) {
    throw new Error("workspace_list_page_size_invalid");
  }

  let page = 1;
  let total: number | null = null;
  let inspected = 0;
  let firstPage: AvailableSource<WorkspaceListData> | null = null;

  while (true) {
    const result = await getWorkspaces(page, pageSize);
    if (result.available === false) return result;
    if (result.data.page !== page || result.data.pageSize !== pageSize) {
      throw new Error("workspace_list_page_mismatch");
    }

    if (!firstPage) {
      firstPage = result;
      total = result.data.total;
      if (!Number.isSafeInteger(total) || total < 0) throw new Error("workspace_list_total_invalid");
    } else if (result.data.total !== total) {
      throw new Error("workspace_list_total_mismatch");
    }

    const workspace = result.data.items.find((item) => item.id === workspaceId);
    if (workspace) {
      return {
        ...result,
        status: "available",
        data: workspace
      };
    }

    inspected += result.data.items.length;
    if (inspected > total) throw new Error("workspace_list_page_overflow");
    if (inspected === total) {
      return {
        ...firstPage,
        status: "empty",
        data: null
      };
    }
    if (result.data.items.length === 0) throw new Error("workspace_list_page_incomplete");
    page += 1;
  }
}

export function getWorkspaceRuntimeStatus(workspaceId: string): Promise<SourceEnvelope<WorkspaceRuntimeDTO>> {
  return sourceRequest<WorkspaceRuntimeDTO>(() => getJson<unknown>(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-status`
  ));
}

export function getWorkspaceGatewayBudget(workspaceId: string, keyId: string): Promise<SourceEnvelope<WorkspaceGatewayBudgetDTO>> {
  return workspaceGatewayBudgetSourceRequest(
    () => getJson<unknown>(`/api/workspaces/${encodeURIComponent(workspaceId)}/gateway-budget`),
    workspaceId,
    keyId
  );
}

export function updateWorkspaceGatewayBudget(
  workspaceId: string,
  keyId: string,
  input: WorkspaceGatewayBudgetUpdateRequest,
  csrfToken: string,
  idempotencyKey: string
): Promise<SourceEnvelope<WorkspaceGatewayBudgetDTO>> {
  const payload: WorkspaceGatewayBudgetUpdateRequest = {};
  if (input.quotaUsdMicros !== undefined) payload.quotaUsdMicros = input.quotaUsdMicros;
  if (input.rateLimit5hUsdMicros !== undefined) payload.rateLimit5hUsdMicros = input.rateLimit5hUsdMicros;
  if (input.rateLimit1dUsdMicros !== undefined) payload.rateLimit1dUsdMicros = input.rateLimit1dUsdMicros;
  if (input.rateLimit7dUsdMicros !== undefined) payload.rateLimit7dUsdMicros = input.rateLimit7dUsdMicros;
  if (input.enabled !== undefined) payload.enabled = input.enabled;
  if (input.resetQuota !== undefined) payload.resetQuota = input.resetQuota;
  if (input.resetRateLimitUsage !== undefined) payload.resetRateLimitUsage = input.resetRateLimitUsage;
  return workspaceGatewayBudgetSourceRequest(
    () => patchJson<unknown>(
      `/api/workspaces/${encodeURIComponent(workspaceId)}/gateway-budget`,
      payload,
      csrfToken,
      idempotencyKey
    ),
    workspaceId,
    keyId
  );
}

export function revealWorkspaceCredentials(
  workspaceId: string,
  csrfToken: string,
  idempotencyKey = `runtime-credential-reveal:${crypto.randomUUID()}`
): Promise<RuntimeCredentialResponse> {
  return postJson<unknown>(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-credentials/reveal`,
    {},
    csrfToken,
    idempotencyKey
  ).then(decodeDto<RuntimeCredentialResponse>);
}

export function rotateWorkspaceCredentials(
  workspaceId: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<RuntimeCredentialRotationResponse> {
  return postJson<unknown>(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-credentials/rotate`,
    {},
    csrfToken,
    idempotencyKey
  ).then(decodeDto<RuntimeCredentialRotationResponse>);
}

export function updateWorkspaceRenewal(
  workspaceId: string,
  input: WorkspaceRenewalRequest,
  csrfToken: string,
  idempotencyKey = `workspace-renewal:${crypto.randomUUID()}`
): Promise<WorkspaceRenewalResponse> {
  return postJson<unknown>(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/auto-renew`,
    input,
    csrfToken,
    idempotencyKey
  ).then(decodeDto<WorkspaceRenewalResponse>);
}
