import { getJson, postJson } from "./console-api.ts";
import { listApprovedRuntimeVersions } from "./workspaces-api.ts";
import type { RuntimeVersionDTO, WorkspaceOwnerOperationDTO } from "./dtos.ts";

export interface NativeAppUpgradeContext {
  currentDeploymentId: string;
  releases: RuntimeVersionDTO[];
}
export interface NativeAppUpgradeIntent {
  workspaceId: string;
  expectedCurrentDeploymentId: string;
  runtimeVersionId: string;
  idempotencyKey: string;
}

// Both the current selection and all candidate releases come from their owners.
// No version is inferred from the original purchase or a mutable image tag.
export async function readNativeAppUpgradeContext(workspaceId: string, signal?: AbortSignal): Promise<NativeAppUpgradeContext> {
  const [releases, currentDeploymentId] = await Promise.all([listApprovedRuntimeVersions(), readCurrentDeployment(workspaceId, signal)]);
  return { currentDeploymentId, releases: releases.filter((release) => release.status === "approved") };
}

async function readCurrentDeployment(workspaceId: string, signal?: AbortSignal): Promise<string> {
  const page = await getJson<{ items: unknown[]; currentDeployment?: { id: string; workspaceId: string; status: string } }>(
    `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deployments?limit=1`, { signal });
  if (!page || !Array.isArray(page.items)) throw new Error("invalid_serve_deployment_page");
  // The owner's current selection is independent of history pagination. Never
  // derive a command predecessor from attempts or combine multiple page reads.
  const current = page.currentDeployment;
  if (!current || current.status !== "active") throw new Error("当前没有唯一可升级的应用部署，请先确认原应用状态。");
  if (current.workspaceId !== workspaceId || typeof current.id !== "string" || !current.id.trim()) throw new Error("invalid_serve_deployment_identity");
  return current.id;
}

// Replaying this exact intent resumes the same accepted replacement. An unknown
// response never changes its key, target release or expected predecessor.
export async function updateNativeDefaultApp(intent: NativeAppUpgradeIntent, csrfToken: string, signal?: AbortSignal): Promise<WorkspaceOwnerOperationDTO> {
  if (!intent.workspaceId || !intent.expectedCurrentDeploymentId || !intent.runtimeVersionId || !intent.idempotencyKey || !csrfToken) throw new Error("invalid_native_app_upgrade_intent");
  const operation = await postJson<WorkspaceOwnerOperationDTO>(`/api/v2/workspaces/${encodeURIComponent(intent.workspaceId)}/update`, {
    applicationSelection: { kind: "opl_app", runtimeVersionId: intent.runtimeVersionId },
    expectedCurrentDeploymentId: intent.expectedCurrentDeploymentId
  }, csrfToken, intent.idempotencyKey, 30_000, signal);
  if (!operation || operation.owner !== "serve" || operation.kind !== "update_workspace" || !operation.operationId ||
      !["accepted", "running", "awaiting_confirmation", "succeeded", "failed", "needs_attention", "cancelled"].includes(operation.status)) throw new Error("invalid_app_upgrade_operation");
  if (["accepted", "running", "awaiting_confirmation"].includes(operation.status) &&
      (!Number.isInteger(operation.pollAfterSeconds) || operation.pollAfterSeconds! < 1 || operation.pollAfterSeconds! > 300)) throw new Error("invalid_app_upgrade_poll_interval");
  return operation;
}
