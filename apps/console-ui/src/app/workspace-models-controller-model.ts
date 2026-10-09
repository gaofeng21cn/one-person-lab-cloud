import type { WorkspaceModelConfigurationDTO, WorkspaceModelSelectionDTO, WorkspaceOwnerOperationDTO } from "../api/dtos.ts";

// The model configuration is the Workspace owner's own fact: the intent version
// it persists, the version the runtime confirmed, and the operation that carries
// the reload. These pure helpers only bind an accepted draft to one idempotent
// command and read the owner's operation back; they never derive an applied
// version the owner did not confirm.

export interface WorkspaceModelUpdateIntent {
  readonly workspaceId: string;
  readonly expectedVersion: string;
  readonly selections: WorkspaceModelSelectionDTO[];
  readonly signature: string;
  readonly idempotencyKey: string;
}

export function workspaceModelSelectionsSignature(selections: readonly WorkspaceModelSelectionDTO[]): string {
  return JSON.stringify(selections.map((selection) => [selection.slot, selection.modelId]));
}

export function resolveWorkspaceModelUpdateIntent(
  current: WorkspaceModelUpdateIntent | null,
  workspaceId: string,
  expectedVersion: string,
  selections: readonly WorkspaceModelSelectionDTO[],
  createIdempotencyKey: () => string
): WorkspaceModelUpdateIntent {
  const signature = `${expectedVersion}\u0000${workspaceModelSelectionsSignature(selections)}`;
  if (current && current.workspaceId === workspaceId && current.signature === signature) return current;
  return {
    workspaceId,
    expectedVersion,
    selections: selections.map((selection) => ({ ...selection })),
    signature,
    idempotencyKey: createIdempotencyKey()
  };
}

interface ErrorWithStatus {
  readonly status?: unknown;
}

// A refusal the owner itself decided clears the intent: the command was answered,
// so retrying it with the same key would only replay the same refusal. A lost or
// unanswered command keeps the intent so a retry carries the original key.
export function shouldRetainWorkspaceModelIntent(error: unknown): boolean {
  if (!error || typeof error !== "object" || !("status" in error)) return true;
  const status = Number((error as ErrorWithStatus).status);
  return !Number.isFinite(status) || status === 0 || status >= 500;
}

export function workspaceModelOperationConfirmed(operation: WorkspaceOwnerOperationDTO): boolean {
  return operation.status === "succeeded";
}

export function workspaceModelOperationRejected(operation: WorkspaceOwnerOperationDTO): boolean {
  return operation.status === "failed" || operation.status === "needs_attention" || operation.status === "cancelled";
}

// The displayed target is the persisted intent version; the applied version only
// advances after the runtime confirmed the reload. A pending or failed intent is
// never presented as the configuration the application is running.
export function workspaceModelConfigurationApplied(configuration: WorkspaceModelConfigurationDTO): boolean {
  return configuration.status === "applied"
    && configuration.appliedVersion === configuration.version;
}

export interface WorkspaceModelStatusPresentation {
  readonly label: string;
  readonly tone: "good" | "warning" | "danger";
  readonly description: string;
}

export function presentWorkspaceModelStatus(configuration: WorkspaceModelConfigurationDTO): WorkspaceModelStatusPresentation {
  switch (configuration.status) {
    case "applied":
      return {
        label: "已生效",
        tone: "good",
        description: "运行中的应用已确认加载当前模型配置。"
      };
    case "pending":
      return {
        label: "等待应用",
        tone: "warning",
        description: "配置已提交，运行中的应用尚未确认加载，请以原操作读回为准。"
      };
    case "failed":
      return {
        label: "更新失败",
        tone: "danger",
        description: "本次模型配置未应用，运行中的应用保留上一次已确认的配置。"
      };
    case "needs_attention":
      return {
        label: "需要处理",
        tone: "danger",
        description: "本次模型配置结果需要管理员确认，运行中的应用保留上一次已确认的配置。"
      };
  }
}

export function workspaceModelDraft(configuration: WorkspaceModelConfigurationDTO): Record<string, string> {
  const draft: Record<string, string> = {};
  for (const selection of configuration.selections) draft[selection.slot] = selection.modelId;
  return draft;
}

export function workspaceModelDraftSelections(
  configuration: WorkspaceModelConfigurationDTO,
  draft: Record<string, string>
): WorkspaceModelSelectionDTO[] {
  return configuration.selections.map((selection) => ({ slot: selection.slot, modelId: draft[selection.slot] ?? "" }));
}

export function workspaceModelDraftReady(configuration: WorkspaceModelConfigurationDTO, draft: Record<string, string>): boolean {
  return configuration.selections.length > 0
    && configuration.selections.every((selection) => Boolean((draft[selection.slot] ?? "").trim()));
}

export function workspaceModelDraftDirty(configuration: WorkspaceModelConfigurationDTO, draft: Record<string, string>): boolean {
  return configuration.selections.some((selection) => (draft[selection.slot] ?? "") !== selection.modelId);
}
