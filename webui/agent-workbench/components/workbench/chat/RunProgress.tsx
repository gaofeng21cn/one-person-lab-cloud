import { LoaderCircle, RefreshCw, Square } from "lucide-react";
import { failureTitle, isActiveRun, needsStatusReadback, type ApiRun, type UiFailure } from "../../../state/workbench/remoteModelAdapter";

export function RunProgress({ run, loading = false, failure, canCancel = false, onCancel, onReconnect }: {
  run?: ApiRun | null; loading?: boolean; failure?: UiFailure | null; canCancel?: boolean; onCancel?: () => void; onReconnect?: () => void;
}) {
  const active = isActiveRun(run ?? null);
  if (!active && !loading && !failure) return null;
  const phase = run?.status === "cancelling" ? "正在停止"
    : run?.progress.phase === "observation_error" ? "暂时无法读取进度，等待执行结果"
    : run?.progress.phase === "retrieval" ? "正在检索资料"
    : !run?.partialReply && (run?.progress.phase === "retrieved" || run?.progress.phase === "answering") && run.progress.document_count !== undefined
      ? `已检索到 ${run.progress.document_count} 个来源，正在准备回答`
    : run?.partialReply || run?.progress.phase === "generation" ? "正在回答"
    : run?.progress.attempt_role === "reviewer" || run?.progress.attempt_role === "re-reviewer" ? "正在核对回答与引用"
    : run?.progress.attempt_role === "repairer" ? "正在修订回答"
    : run?.progress.phase === "stage" ? "正在分析证据"
    : run?.progress.phase === "validation" ? "正在校验引用"
    : "正在处理";
  return <div className="message-progress" aria-live="polite" data-component-id="workbench.chat.run_status">
    <div className="message-progress-heading">
      {(active || loading) && !failure && <LoaderCircle className="spin" aria-hidden="true" />}
      <span>{failure ? failureTitle(failure) : loading && !run ? "正在读取会话" : phase}</span>
      {needsStatusReadback(failure ?? null) && <button className="message-action" type="button" title="重新读取运行状态" onClick={onReconnect}><RefreshCw /></button>}
      {active && canCancel && onCancel && <button className="message-action" type="button" title="停止回答" disabled={run?.status === "cancelling"} onClick={onCancel}><Square /></button>}
    </div>
    {failure && <p role="alert">{failure.message}</p>}
  </div>;
}
