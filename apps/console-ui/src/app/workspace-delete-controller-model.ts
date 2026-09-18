import type { SourceEnvelope, WorkspaceDTO, WorkspaceDeletionDTO, WorkspaceDeletionPageState, WorkspaceDeletionStage } from "../api/dtos.ts";

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

// Presentation for the deletion progress panel. Console only renders the stage,
// page state, reason and refund status that Control Plane published; it never
// derives a stage from the durable phase, never infers that resources are gone,
// and never concludes a refund.
export interface WorkspaceDeletePresentation {
  readonly title: string;
  readonly detail: string;
  readonly tone: "info" | "warning" | "danger" | "good";
  readonly stageLabel: string;
  readonly refundLabel: string;
  /** Most recent observation recorded by the persisted stage evidence. */
  readonly lastReadbackLabel: string;
}

// formatWorkspaceDeleteTime renders a persisted platform timestamp without
// inventing a value when the field is absent.
export function formatWorkspaceDeleteTime(value: string | undefined): string {
  const raw = value?.trim() ?? "";
  if (!raw) return "";
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) return raw;
  return parsed.toISOString().replace("T", " ").slice(0, 19);
}

const workspaceDeleteStageLabels: Record<WorkspaceDeletionStage, string> = {
  runtime_absent: "正在释放应用运行环境",
  attachment_absent: "正在解除数据盘挂载",
  storage_absent: "正在等待数据盘解绑并销毁",
  compute_absent: "正在等待计算资源删除结果",
  workspace_absent: "正在确认工作空间资源已全部移除",
  receipt_recorded: "正在记录删除回执"
};

const workspaceDeleteReasonLabels: Record<string, string> = {
  fabric_runtime_absence_unconfirmed: "应用运行环境尚未确认释放",
  fabric_runtime_readback_unavailable: "暂时读不到应用运行环境状态",
  fabric_runtime_identity_conflict: "运行环境身份与原始开通记录不一致",
  fabric_storage_unconfirmed: "数据盘删除结果尚未确认",
  fabric_storage_identity_conflict: "数据盘身份与原始开通记录不一致",
  fabric_compute_absence_unconfirmed: "计算资源删除结果尚未确认",
  fabric_compute_readback_unavailable: "暂时读不到计算资源状态",
  fabric_compute_identity_conflict: "计算资源身份与原始开通记录不一致",
  fabric_attachment_unconfirmed: "数据盘挂载解除结果尚未确认",
  workspace_delete_identity_mismatch: "删除操作身份与原始记录不一致",
  workspace_delete_inventory_unavailable: "暂时读不到资源清单",
  workspace_delete_secret_inventory_unavailable: "暂时读不到密钥清单"
};

const workspaceDeleteRefundLabels: Record<string, string> = {
  blocked: "退款等待资源删除确认",
  pending: "退款处理中",
  manual_review: "退款需要核对",
  succeeded: "退款已完成",
  not_due: "本次删除无需退款"
};

export function presentWorkspaceDeleteReason(reasonCode: string | undefined): string {
  const code = reasonCode?.trim() ?? "";
  if (!code) return "";
  return workspaceDeleteReasonLabels[code] ?? "删除遇到未确认的结果";
}

// workspaceDeletePageState falls back to the platform's deletion status when the
// response omits the page state, so an older or partial response still renders a
// truthful state instead of a blank one. Both fields are platform-published.
function workspaceDeletePageState(operation: WorkspaceDeletionDTO): WorkspaceDeletionPageState {
  if (operation.pageState) return operation.pageState;
  if (operation.status === "deleted") return "completed";
  if (operation.status === "manual_review") return "blocked";
  return "waiting";
}

// presentWorkspaceDelete shows the current stage, the stable reason when the
// operation stopped, the automatic-retry state, and the separate refund status.
export function presentWorkspaceDelete(operation: WorkspaceDeletionDTO | null): WorkspaceDeletePresentation | null {
  if (!operation) return null;
  const stageLabel = operation.stage ? workspaceDeleteStageLabels[operation.stage] : "正在确认工作空间状态";
  const refundLabel = operation.refundStatus ? workspaceDeleteRefundLabels[operation.refundStatus] ?? "退款状态未知" : "";
  const reason = presentWorkspaceDeleteReason(operation.reasonCode);
  const lastReadbackLabel = formatWorkspaceDeleteTime(operation.lastReadbackAt);
  switch (workspaceDeletePageState(operation)) {
    case "completed":
      return { title: "工作空间已删除", detail: "资源删除已完成。", tone: "good", stageLabel, refundLabel, lastReadbackLabel };
    case "blocked":
      return {
        title: "删除已暂停，需要核对",
        detail: `${reason || "删除遇到身份冲突"}。系统不会继续删除，也不会退款，请联系管理员核对原始开通记录。`,
        tone: "danger", stageLabel, refundLabel, lastReadbackLabel
      };
    case "retrying":
      return {
        title: "正在重试删除",
        detail: `${stageLabel}。${reason ? `${reason}。` : ""}${operation.nextRetryAt ? "系统会按计划自动重试，无需重复提交。" : "系统会继续自动重试，无需重复提交。"}`,
        tone: "warning", stageLabel, refundLabel, lastReadbackLabel
      };
    default:
      return {
        title: "正在删除工作空间",
        detail: `${stageLabel}。删除在后台继续处理，可以关闭页面后再查看。`,
        tone: "info", stageLabel, refundLabel, lastReadbackLabel
      };
  }
}
