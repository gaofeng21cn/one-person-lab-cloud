import type { ThreadMessageLike } from "@assistant-ui/react";

export type AgentConfig = { choiceId: string; packageId: string; packageName: string; sidebarTitle: string; assistantTitle: string; notice?: { title: string; body: string } };
export type AgentSelection = { package_id: string; shortcut_id: string; codex_visible_entry: string; required_skill_ids: string[] };
export type PackageDescriptor = { choiceId: string; packageId: string; name: string; selection: AgentSelection };
export type ApiEvidence = {
  id: string; citationId?: number; title: string; detail: string; quote: string; sourceKind: string;
  source?: { documentId: string; fileName: string; url: string; locations: { page: number; rect: [number, number, number, number] }[] };
};
export type ApiTurnResult = { reply: string; evidence: ApiEvidence[]; runId: string; claims?: { text: string; evidence_ids: string[] }[]; uncertainties?: string[] };
export type ApiRun = {
  runId: string; threadId?: string | null; status: "running" | "cancelling" | "cancelled" | "succeeded" | "failed";
  question?: string; packageId?: string; progress: { phase: string; document_count?: number; attempt_role?: string };
  partialReply?: string; result?: ApiTurnResult; error?: { code: string; message: string };
};
export type UiFailure = { kind: "input_rejected" | "submission_unknown" | "status_read" | "execution_failed" | "result_invalid" | "cancel_failed" | "release_failed"; message: string; httpStatus?: number };
export class ApiHttpError extends Error { readonly status: number; constructor(message: string, status: number) { super(message); this.name = "ApiHttpError"; this.status = status; } }
export class ApiResultError extends Error {}
export const uiFailure = (kind: UiFailure["kind"], error: unknown): UiFailure => ({ kind, message: error instanceof Error ? error.message : String(error), ...(error instanceof ApiHttpError ? { httpStatus: error.status } : {}) });
export const isActiveRun = (run: ApiRun | null) => run?.status === "running" || run?.status === "cancelling";
export const needsStatusReadback = (failure: UiFailure | null) => Boolean(failure && ["submission_unknown", "status_read", "cancel_failed", "result_invalid"].includes(failure.kind));
export const failureTitle = (failure: UiFailure) => ({ input_rejected: "输入未提交", submission_unknown: "提交状态未确认", status_read: "运行状态读取失败", execution_failed: "运行失败", result_invalid: "结果校验失败", cancel_failed: "取消未完成", release_failed: "当前问题未释放" })[failure.kind];

/** Presentation data is domain-neutral; only a producer may supply citations. */
export function validateResult(run: ApiRun): ApiTurnResult {
  const result = run.result;
  if (run.status !== "succeeded" || !result || typeof result.reply !== "string" || !result.reply.trim() || result.runId !== run.runId || !Array.isArray(result.evidence)) throw new ApiResultError("Runtime 返回了无效的回答结果");
  const ids = new Set<string>();
  for (const item of result.evidence) {
    if (!item.id || ids.has(item.id) || !item.title || typeof item.quote !== "string" || !item.sourceKind) throw new ApiResultError("Runtime 返回了无效的引用资料");
    ids.add(item.id);
  }
  if (result.claims && result.claims.some(item => typeof item.text !== "string" || !Array.isArray(item.evidence_ids) || item.evidence_ids.some(id => !ids.has(id)))) throw new ApiResultError("回答引用关系无效");
  return result;
}
export function messagesFromRuns(runs: ApiRun[]): ThreadMessageLike[] {
  return runs.flatMap((run): ThreadMessageLike[] => {
    const messages: ThreadMessageLike[] = run.question ? [{ id: `${run.runId}:question`, role: "user", content: run.question }] : [];
    if (run.status === "succeeded") messages.push({ id: `${run.runId}:answer`, role: "assistant", content: validateResult(run).reply, metadata: { custom: { result: run.result, run } } });
    else if (run.status === "failed" || run.status === "cancelled") messages.push({ id: `${run.runId}:answer`, role: "assistant", content: run.error?.message || "回答未完成", metadata: { custom: { run } } });
    else messages.push({ id: `${run.runId}:answer`, role: "assistant", content: run.partialReply || [], status: { type: "running" }, metadata: { custom: { run } } });
    return messages;
  });
}

type Json = Record<string, any>;
const record = (value: unknown): value is Json => typeof value === "object" && value !== null && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === "string" && value.length > 0;
export function readRuntimeAgents(value: unknown): { packages: PackageDescriptor[]; modelReady: boolean } {
  if (!record(value) || value.app_state?.version !== "g2" || value.app_state?.app_state?.schema_version !== "opl_app_state.v1") throw new ApiResultError("Runtime Agent 目录协议不受支持");
  const state = value.app_state.app_state;
  const entries = state.agent_packages?.directory?.entries;
  if (!Array.isArray(entries)) throw new ApiResultError("Runtime 未返回 Agent 目录");
  const packages: PackageDescriptor[] = [];
  for (const item of entries) {
    if (item.package_role !== "standard_agent" || item.readiness?.operational_ready !== true) continue;
    for (const shortcut of item.home_shortcuts || []) {
      const route = shortcut.route;
      if (route?.route_kind !== "agent_package_shortcut" || route.executor !== "codex_cli") continue;
      if (!text(item.package_id) || !text(shortcut.shortcut_id) || !text(route.codex_visible_entry) || !Array.isArray(item.capability_metadata?.required_skill_ids) || !item.capability_metadata.required_skill_ids.every(text)) throw new ApiResultError("Agent 快捷入口缺少执行声明");
      packages.push({ choiceId: JSON.stringify([item.package_id, shortcut.shortcut_id]), packageId: item.package_id, name: item.display_name || item.package_id,
        selection: { package_id: item.package_id, shortcut_id: shortcut.shortcut_id, codex_visible_entry: route.codex_visible_entry, required_skill_ids: item.capability_metadata.required_skill_ids } });
    }
  }
  return { packages, modelReady: state.core?.codex?.model_access_ready === true };
}
export function readAgentReply(value: unknown, expectedThreadId?: string): ApiRun {
  if (!record(value) || value.executor !== "codex_app_server" || !text(value.threadId) || !text(value.turnId) || value.canonicalThread?.id !== value.threadId || (expectedThreadId && expectedThreadId !== value.threadId)) throw new ApiResultError("Runtime 未确认当前会话与回答身份");
  const turn = value.canonicalThread?.turns?.find((item: Json) => item.id === value.turnId);
  if (!turn || turn.status !== "completed" || !text(value.finalMessage) || !value.finalMessage.trim()) throw new ApiResultError(turn?.error?.message || "Runtime 尚未确认回答完成");
  return { runId: value.turnId, threadId: value.threadId, status: "succeeded", progress: { phase: "completed" }, result: { runId: value.turnId, reply: value.finalMessage, evidence: [] } };
}

export type Conversation = { id: string; title: string };
export type WorkbenchConfig = { schemaVersion: 1; runtimeProtocol: "opl-native-g2"; sidebarTitle: string; assistantTitle: string | null; notice: { title: string; body: string } | null };
export function readWorkbenchConfig(value: unknown): WorkbenchConfig {
  if (!record(value) || value.schemaVersion !== 1 || value.runtimeProtocol !== "opl-native-g2" || !text(value.sidebarTitle) || !(value.assistantTitle === null || text(value.assistantTitle)) || !(value.notice === null || (record(value.notice) && text(value.notice.title) && text(value.notice.body)))) throw new Error("WebUI 配置无效或 Runtime 协议不受支持");
  return value as WorkbenchConfig;
}
