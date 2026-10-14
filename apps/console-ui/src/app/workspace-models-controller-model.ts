import type { WorkspaceModelConfigurationDTO, WorkspaceModelSelectionDTO, WorkspaceOwnerOperationDTO } from "../api/dtos.ts";
import type { WorkspaceModelsIssue } from "./console-controller-types.ts";

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
// advances after the runtime confirmed the reload. Equal versions are therefore
// the owner's own application evidence and stand on their own: the owner confirms
// the reload before it attempts the predecessor-key revocation, so a configuration
// the runtime is demonstrably running can still carry an unfinished operation. A
// pending or failed intent never has equal versions and is never presented as the
// configuration the application is running.
export function workspaceModelConfigurationApplied(configuration: WorkspaceModelConfigurationDTO): boolean {
  return configuration.appliedVersion === configuration.version;
}

// A configuration can predate the accepted command or be replaced by another
// client. Only its own Workspace operation is evidence about its closeout.
export function workspaceModelOperationMatchesConfiguration(
  configuration: WorkspaceModelConfigurationDTO,
  operation: WorkspaceOwnerOperationDTO
): boolean {
  return Boolean(configuration.operationId)
    && configuration.operationId === operation.operationId
    && configuration.workspaceId === operation.resourceId
    && operation.owner === "workspace"
    && operation.kind === "update_models";
}

// Configuration readback decides application; the original operation separately
// decides closeout. An unread operation has no verdict and uses its error channel.
export function workspaceModelConfigurationIssue(
  configuration: WorkspaceModelConfigurationDTO,
  operation?: WorkspaceOwnerOperationDTO | null
): WorkspaceModelsIssue {
  if (workspaceModelConfigurationApplied(configuration)) {
    return operation && workspaceModelOperationMatchesConfiguration(configuration, operation)
      && !workspaceModelOperationConfirmed(operation) ? "closeout_unconfirmed" : "";
  }
  return configuration.status === "pending" ? "unconfirmed" : "failed";
}

export interface WorkspaceModelStatusPresentation {
  readonly label: string;
  readonly tone: "good" | "warning" | "danger";
  readonly description: string;
}

// The status cell describes application only; operation closeout has its own alert.
export function presentWorkspaceModelStatus(configuration: WorkspaceModelConfigurationDTO): WorkspaceModelStatusPresentation {
  if (workspaceModelConfigurationApplied(configuration)) {
    return {
      label: "已生效",
      tone: "good",
      description: "运行中的应用已确认加载当前模型配置。"
    };
  }
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

export interface WorkspaceModelCloseoutPresentation {
  readonly color: "warning" | "danger";
  readonly title: string;
  readonly description: string;
}

// Only an actual operation read can report closeout. Failure after application
// cannot be presented as a failure to load the confirmed configuration.
export function presentWorkspaceModelCloseout(
  configuration: WorkspaceModelConfigurationDTO | null,
  operation: WorkspaceOwnerOperationDTO | null
): WorkspaceModelCloseoutPresentation | null {
  if (!configuration || !workspaceModelConfigurationApplied(configuration) || !operation
    || !workspaceModelOperationMatchesConfiguration(configuration, operation)
    || workspaceModelOperationConfirmed(operation)) return null;
  if (workspaceModelOperationRejected(operation)) {
    return {
      color: "danger",
      title: "模型配置已生效，收尾待处理",
      description: "运行中的应用已确认加载当前模型配置；本次更新的收尾尚未确认（包括旧模型密钥的退役），需要管理员处理。"
    };
  }
  return {
    color: "warning",
    title: "模型配置已生效，原操作收尾中",
    description: "运行中的应用已确认加载当前模型配置；原操作仍在收尾，完成前请以原操作读回为准。"
  };
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
