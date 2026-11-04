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
