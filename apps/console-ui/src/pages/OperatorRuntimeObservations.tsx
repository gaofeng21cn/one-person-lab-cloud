import { RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";

import { getOperatorRuntimeObservations } from "../api/console-read-api.ts";
import type { OperatorRuntimeObservationsDTO, SourceEnvelope } from "../api/dtos.ts";
import { SourceState } from "../components/source/SourceState.tsx";
import { Badge, Button, Modal } from "../components/ui/index.ts";
import { formatCount, formatDate } from "../console-model.ts";
import { observationLabel, observationReason } from "./operator-observation-presentation.ts";

export function OperatorRuntimeObservations({ onClose, refreshKey, sessionIdentity }: {
  onClose: () => void;
  refreshKey: string;
  sessionIdentity: string;
}) {
  const [source, setSource] = useState<SourceEnvelope<OperatorRuntimeObservationsDTO> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [requestVersion, setRequestVersion] = useState(0);
  const [view, setView] = useState<"attention" | "all">("attention");

  useEffect(() => {
    const request = new AbortController();
    setSource(null);
    setError("");
    setLoading(true);
    void getOperatorRuntimeObservations(request.signal).then((result) => {
      if (request.signal.aborted) return;
      setSource(result);
    }).catch(() => {
      if (!request.signal.aborted) setError("无法读取 Runtime 观测，请重试。");
    }).finally(() => {
      if (!request.signal.aborted) setLoading(false);
    });
    return () => request.abort();
  }, [requestVersion, refreshKey, sessionIdentity]);

  return <Modal
    className="operator-runtime-observations"
    description="对账当前 Workspace 与实际运行对象；观测不修改资源。"
    footer={<><Button onClick={onClose} variant="outline">关闭</Button><Button busy={loading} onClick={() => setRequestVersion((current) => current + 1)} variant="outline"><RefreshCw aria-hidden size={15} />刷新观测</Button></>}
    onClose={onClose}
    open
    title="Workspace Runtime 观测"
  >
    <SourceState error={error} loading={loading} onRetry={() => setRequestVersion((current) => current + 1)} source={source} unavailableTitle="Runtime 观测暂不可用">
      {(data) => {
        const items = view === "all" ? data.items : data.items.filter((item) => item.status === "attention");
        return <>
          <p>读取时间：{formatDate(data.observedAt, true)} · 来源：{source?.source}</p>
          <dl className="data-list">
            <div><dt>业务 Workspace</dt><dd>{formatCount(data.businessTotal)}</dd></div>
            <div><dt>实际运行对象</dt><dd>{formatCount(data.observedTotal)}</dd></div>
            <div><dt>运行正常</dt><dd>{formatCount(data.runningCount)}</dd></div>
            <div><dt>正常暂停</dt><dd>{formatCount(data.suspendedCount)}</dd></div>
            <div><dt>处理中</dt><dd>{formatCount(data.pendingCount)}</dd></div>
            <div><dt>需处理</dt><dd>{formatCount(data.attentionCount)}</dd></div>
            <div><dt>无当前业务记录对象</dt><dd>{formatCount(data.unmatchedCount)}</dd></div>
          </dl>
          <div aria-label="Runtime 观测范围" className="operator-card-actions" role="group">
            <Button aria-pressed={view === "attention"} color={view === "attention" ? "primary" : "secondary"} onClick={() => setView("attention")} size="sm" variant="outline">需处理</Button>
            <Button aria-pressed={view === "all"} color={view === "all" ? "primary" : "secondary"} onClick={() => setView("all")} size="sm" variant="outline">全部</Button>
          </div>
          {items.length ? <div className="operator-runtime-observation-list">{items.map((item, index) => <article className="operator-object-card" key={item.objectRef || `${item.workspaceId}-${index}`}>
            <header className="operator-object-card__header">
              <span><strong>{item.workspaceId || "无 Workspace 身份"}</strong><small>{item.runtimeId || item.objectRef || "运行对象尚未创建"}</small></span>
              <Badge color={item.status === "attention" ? "danger" : item.status === "pending" ? "warning" : "success"}>{item.status === "suspended" ? "正常暂停" : observationLabel(item.status)}</Badge>
            </header>
            <dl className="operator-object-card__facts">
              <div><dt>业务状态</dt><dd>{observationLabel(item.businessState)}</dd></div>
              <div><dt>控制器目标</dt><dd>{observationLabel(item.desiredState)}</dd></div>
              <div><dt>实际状态</dt><dd>{observationLabel(item.observedState)}</dd></div>
              <div><dt>归属</dt><dd>{observationLabel(item.ownership)}</dd></div>
            </dl>
            {item.reasonCode ? <p>{observationReason(item.reasonCode)}</p> : null}
          </article>)}</div> : <div className="empty-panel">{view === "attention" ? "当前没有需要处理的 Runtime 对象。" : "当前没有 Runtime 观测对象。"}</div>}
        </>;
      }}
    </SourceState>
  </Modal>;
}
