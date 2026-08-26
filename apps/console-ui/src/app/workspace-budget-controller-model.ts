import type { WorkspaceGatewayBudgetDTO, WorkspaceGatewayBudgetUpdateRequest } from "../api/dtos.ts";

export interface WorkspaceBudgetIntent {
  readonly workspaceId: string;
  readonly keyId: string;
  readonly input: WorkspaceGatewayBudgetUpdateRequest;
  readonly signature: string;
  readonly idempotencyKey: string;
}

const inputFields = [
  "quotaUsdMicros",
  "rateLimit5hUsdMicros",
  "rateLimit1dUsdMicros",
  "rateLimit7dUsdMicros",
  "enabled",
  "resetQuota",
  "resetRateLimitUsage"
] as const;

export function workspaceBudgetInputSignature(input: WorkspaceGatewayBudgetUpdateRequest): string {
  return JSON.stringify(inputFields.map((field) => input[field] ?? null));
}

export function sameWorkspaceBudgetInput(
  left: WorkspaceGatewayBudgetUpdateRequest,
  right: WorkspaceGatewayBudgetUpdateRequest
): boolean {
  return workspaceBudgetInputSignature(left) === workspaceBudgetInputSignature(right);
}

export function resolveWorkspaceBudgetIntent(
  current: WorkspaceBudgetIntent | null,
  workspaceId: string,
  keyId: string,
  input: WorkspaceGatewayBudgetUpdateRequest,
  createIdempotencyKey: () => string
): WorkspaceBudgetIntent {
  const signature = workspaceBudgetInputSignature(input);
  if (current && current.workspaceId === workspaceId && current.keyId === keyId && current.signature === signature) {
    return current;
  }
  return {
    workspaceId,
    keyId,
    input: { ...input },
    signature,
    idempotencyKey: createIdempotencyKey()
  };
}

export function workspaceBudgetResultMatchesInput(
  result: WorkspaceGatewayBudgetDTO,
  input: WorkspaceGatewayBudgetUpdateRequest
): boolean {
  return (input.quotaUsdMicros === undefined || result.quotaUsdMicros === String(input.quotaUsdMicros))
    && (input.rateLimit5hUsdMicros === undefined || result.rateLimit5hUsdMicros === String(input.rateLimit5hUsdMicros))
    && (input.rateLimit1dUsdMicros === undefined || result.rateLimit1dUsdMicros === String(input.rateLimit1dUsdMicros))
    && (input.rateLimit7dUsdMicros === undefined || result.rateLimit7dUsdMicros === String(input.rateLimit7dUsdMicros))
    && (input.enabled === undefined || result.enabled === input.enabled);
}

export function workspaceBudgetIdentityMatches(
  result: WorkspaceGatewayBudgetDTO,
  workspaceId: string,
  keyId: string
): boolean {
  return result.workspaceId === workspaceId && result.keyId === keyId;
}

interface ErrorWithStatus {
  readonly status?: unknown;
}

export function shouldRetainWorkspaceBudgetIntent(error: unknown): boolean {
  if (!error || typeof error !== "object" || !("status" in error)) return true;
  const status = Number((error as ErrorWithStatus).status);
  return !Number.isFinite(status) || status === 0 || status >= 500;
}
