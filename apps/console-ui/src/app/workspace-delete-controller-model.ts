import type { SourceEnvelope, WorkspaceDTO } from "../api/dtos.ts";

export type WorkspaceDeleteIssue = "" | "unavailable" | "unconfirmed";

export interface WorkspaceDeleteIntent {
  readonly workspaceId: string;
  readonly idempotencyKey: string;
}

export function resolveWorkspaceDeleteIntent(
  current: WorkspaceDeleteIntent | null,
  workspaceId: string,
  createIdempotencyKey: () => string
): WorkspaceDeleteIntent {
  if (current?.workspaceId === workspaceId) return current;
  return { workspaceId, idempotencyKey: createIdempotencyKey() };
}

export function workspaceDeleteReadbackConfirmed(
  readback: SourceEnvelope<WorkspaceDTO | null>
): boolean {
  return readback.available && readback.data === null;
}

export interface WorkspaceDeleteErrorPayload {
  readonly error?: string;
}

function errorPayload(error: unknown): WorkspaceDeleteErrorPayload | null {
  if (!error || typeof error !== "object" || !("payload" in error)) return null;
  const payload = (error as { payload?: unknown }).payload;
  if (!payload || typeof payload !== "object") return null;
  const value = payload as Record<string, unknown>;
  return { error: typeof value.error === "string" ? value.error : undefined };
}

export function isWorkspaceDeleteNotFound(error: unknown): boolean {
  return errorPayload(error)?.error === "workspace_not_found";
}

export function shouldRetainWorkspaceDeleteIntent(error: unknown): boolean {
  if (!error || typeof error !== "object") return true;
  if (!("status" in error)) return true;
  const rawStatus = (error as { status?: unknown }).status;
  if (rawStatus === undefined || rawStatus === null || rawStatus === "") return true;
  const status = Number(rawStatus);
  return status === 0 || status === 404 || status >= 500;
}
