import type {
  GatewayKeySecretDTO,
  RuntimeCredentialResponse,
  WorkspaceCredentialAccess
} from "../api/dtos.ts";

export const workspaceSecretLifetimeMs = 60_000;

export interface WorkspaceSecretProjection {
  apiKey: GatewayKeySecretDTO | null;
  workspace: WorkspaceCredentialAccess | null;
}

export type WorkspaceSecretCompletion =
  | { kind: "runtime-credential"; response: RuntimeCredentialResponse }
  | { kind: "workspace-key"; response: GatewayKeySecretDTO };

export interface RuntimeRotationIntent {
  readonly workspaceId: string;
  readonly idempotencyKey: string;
}

export function acceptWorkspaceSecretCompletion(
  activeGeneration: number,
  currentGeneration: number,
  completion: WorkspaceSecretCompletion
): WorkspaceSecretProjection | null {
  if (activeGeneration !== currentGeneration) return null;
  return completion.kind === "runtime-credential"
    ? { apiKey: null, workspace: completion.response.access }
    : { apiKey: completion.response, workspace: null };
}

export function shouldExpireWorkspaceSecret(revealedAtMs: number, nowMs: number): boolean {
  return nowMs - revealedAtMs >= workspaceSecretLifetimeMs;
}

export function resolveRuntimeRotationIntent(
  current: RuntimeRotationIntent | null,
  workspaceId: string,
  createIdempotencyKey: () => string
): RuntimeRotationIntent {
  if (current?.workspaceId === workspaceId) return current;
  return { workspaceId, idempotencyKey: createIdempotencyKey() };
}
