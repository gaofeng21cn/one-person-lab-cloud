import { AlertCircle, Inbox, Info, LockKeyhole, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";

import type { SourceEnvelope } from "../../api/dtos.ts";
import { Alert, Button } from "../ui/index.ts";

export interface SourceStateProps<T> {
  source: SourceEnvelope<T> | null;
  loading?: boolean;
  error?: string;
  empty?: boolean;
  emptyTitle?: string;
  emptyDescription?: string;
  unavailableTitle?: string;
  unavailableDescription?: string;
  errorDescription?: string;
  notice?: boolean;
  noticeKind?: "permission" | "info";
  noticeTitle?: string;
  noticeDescription?: string;
  noticeAction?: ReactNode;
  onRetry?: () => void;
  children: (data: T) => ReactNode;
}

export function SourceState<T>({
  children,
  empty,
  emptyDescription = "当前没有记录。",
  emptyTitle = "暂无数据",
  error,
  errorDescription,
  loading,
  notice,
  noticeAction,
  noticeDescription,
  noticeKind = "info",
  noticeTitle,
  onRetry,
  source,
  unavailableDescription,
  unavailableTitle = "暂不可用"
}: SourceStateProps<T>) {
  if (loading && !source && !notice) return <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div>;

  if (notice) {
    const Icon = noticeKind === "permission" ? LockKeyhole : Info;
    return (
      <div className="source-notice" data-notice-kind={noticeKind} role="status">
        <span className="source-notice__icon" aria-hidden="true"><Icon size={20} /></span>
        <h3>{noticeTitle || "暂无可展示内容"}</h3>
        {noticeDescription ? <p>{noticeDescription}</p> : null}
        {noticeAction ? <div className="source-notice__action">{noticeAction}</div> : null}
      </div>
    );
  }

  if (source?.status === "unavailable") {
    return <Alert color="warning" indicator={<AlertCircle size={18} />} title={unavailableTitle} description={unavailableDescription ?? (source.reasonCode ? `原因代码：${source.reasonCode}` : "来源未返回原因代码。")} actions={onRetry ? <Button onClick={onRetry} size="sm" variant="outline"><RefreshCw size={14} />重试</Button> : undefined} />;
  }

  if (error) {
    return <Alert color="danger" title="服务暂不可用" description={errorDescription ?? error} actions={onRetry ? <Button onClick={onRetry} size="sm" variant="outline"><RefreshCw size={14} />重试</Button> : undefined} />;
  }

  if (!source) return <div className="source-loading" aria-live="polite"><span className="spinner" />等待读取</div>;

  if (source.status === "empty" || empty) {
    return <div className="source-empty"><Inbox aria-hidden="true" size={24} /><h3>{emptyTitle}</h3><p>{emptyDescription}</p></div>;
  }

  return <>{children(source.data)}</>;
}
