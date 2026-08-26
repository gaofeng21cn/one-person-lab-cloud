import type { SourceEnvelope, WorkspaceDTO, WorkspaceRenewalResponse } from "../api/dtos.ts";

export type WorkspaceRenewalIssue = "" | "unconfirmed";

export interface WorkspaceRenewalIntent {
  readonly workspaceId: string;
  readonly autoRenew: boolean;
  readonly idempotencyKey: string;
}

export function resolveWorkspaceRenewalIntent(
  current: WorkspaceRenewalIntent | null,
  workspaceId: string,
  autoRenew: boolean,
  createIdempotencyKey: () => string
): WorkspaceRenewalIntent {
  if (current?.workspaceId === workspaceId && current.autoRenew === autoRenew) return current;
  return { workspaceId, autoRenew, idempotencyKey: createIdempotencyKey() };
}

function validDate(value: unknown): value is string {
  return typeof value === "string" && value.trim() !== "" && !Number.isNaN(Date.parse(value));
}

export function workspaceRenewalResponseMatches(
  response: WorkspaceRenewalResponse,
  autoRenew: boolean
): boolean {
  return response.autoRenew === autoRenew
    && typeof response.renewalStatus === "string"
    && response.renewalStatus.trim() !== ""
    && validDate(response.effectiveAfter)
    && validDate(response.nextRenewalAt)
    && validDate(response.paidThrough);
}

export function workspaceRenewalReadbackMatches(
  readback: SourceEnvelope<WorkspaceDTO | null>,
  workspaceId: string,
  response: WorkspaceRenewalResponse
): boolean {
  return readback.available
    && readback.data !== null
    && readback.data.id === workspaceId
    && readback.data.autoRenew === response.autoRenew
    && readback.data.renewalStatus === response.renewalStatus
    && readback.data.paidThrough === response.paidThrough
    && readback.data.nextRenewalAt === response.nextRenewalAt;
}

export interface WorkspaceRenewalErrorShape {
  readonly status?: unknown;
  readonly payload?: unknown;
}

function renewalError(error: unknown): WorkspaceRenewalErrorShape | null {
  if (!error || typeof error !== "object") return null;
  return error as WorkspaceRenewalErrorShape;
}

/** Keep the original intent when the server may have accepted the mutation. */
export function shouldRetainWorkspaceRenewalIntent(error: unknown): boolean {
  const value = renewalError(error);
  if (!value || value.status === undefined || value.status === null || value.status === "") return true;
  const status = Number(value.status);
  return !Number.isFinite(status) || status === 0 || status >= 500;
}
