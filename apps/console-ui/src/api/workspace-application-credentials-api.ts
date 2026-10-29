import { postJson } from "./console-api.ts";

export interface WorkspaceApplicationCredentials {
  workspaceId: string;
  runtimeInstanceId: string;
  username: string;
  password: string;
}

// Each explicit reveal reaches the owner again. Only the four declared response
// fields enter transient component state; error payloads are never retained.
export async function revealWorkspaceApplicationCredentials(workspaceId: string, csrfToken: string, signal?: AbortSignal): Promise<WorkspaceApplicationCredentials> {
  const value = await postJson<unknown>(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/application-credentials/reveal`, {}, csrfToken, crypto.randomUUID(), 10_000, signal);
  if (!value || typeof value !== "object") throw new Error("invalid_application_credentials");
  const credentials = value as Record<string, unknown>;
  if (Object.keys(credentials).some((key) => !["workspaceId", "runtimeInstanceId", "username", "password"].includes(key))
    || credentials.workspaceId !== workspaceId || typeof credentials.runtimeInstanceId !== "string" || !credentials.runtimeInstanceId.trim()
    || typeof credentials.username !== "string" || !credentials.username.trim() || typeof credentials.password !== "string" || !credentials.password) {
    throw new Error("invalid_application_credentials");
  }
  return { workspaceId, runtimeInstanceId: credentials.runtimeInstanceId, username: credentials.username, password: credentials.password };
}
