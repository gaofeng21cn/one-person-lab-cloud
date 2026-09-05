import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  RefreshCw,
  Search
} from "lucide-react";
import { useEffect, useId, useState, type FormEvent } from "react";

import type { GatewayUsageController } from "../../app/console-controller-types.ts";
import { presentGatewayKeyStatus } from "../../app/customer-experience-model.ts";
import type { GatewayKeySummaryDTO, GatewayUsageItem, GatewayUsagePeriod } from "../../api/dtos.ts";
import { formatCount, formatDate, formatUsdMicros } from "../../console-model.ts";
import { SourceState } from "../source/SourceState.tsx";
import { Alert, Badge, Button, Field, Modal, SegmentedControl } from "../ui/index.ts";

export interface GatewayUsagePageProps {
  controller: GatewayUsageController;
  onCopyRequestId: (requestId: string) => void;
  onOpenKeys: () => void;
}

function formatLatency(value: number | null) {
  return value === null ? "-" : `${formatCount(value)} ms`;
}

function presentKeyKind(kind: GatewayKeySummaryDTO["kind"]) {
  switch (kind) {
    case "general":
      return "通用";
    case "workspace":
      return "工作空间";
  }
}

function keyStatusColor(status: GatewayKeySummaryDTO["status"]): "success" | "secondary" | "warning" | "danger" {
  if (status === "active") return "success";
  if (status === "quota_exhausted") return "warning";
  if (status === "expired") return "danger";
  return "secondary";
}

function KeyBadges({ item }: { item: GatewayKeySummaryDTO }) {
  return <span className="gateway-usage-key-badges">
    <Badge color={keyStatusColor(item.status)}>{presentGatewayKeyStatus(item.status).label}</Badge>
    <Badge color="secondary">{presentKeyKind(item.kind)}</Badge>
  </span>;
}

function RequestTechnicalDetails({ item, onCopyRequestId }: {
  item: GatewayUsageItem;
  onCopyRequestId: (requestId: string) => void;
}) {
  return <dl className="usage-request-technical-facts">
    <div><dt>API 路径</dt><dd><code>{item.inboundEndpoint || "-"}</code></dd></div>
    <div><dt>缓存读取 Token</dt><dd>{formatCount(item.cacheReadTokens)}</dd></div>
    <div><dt>缓存写入 Token</dt><dd>{formatCount(item.cacheCreationTokens)}</dd></div>
    <div><dt>首个 Token 延迟</dt><dd>{formatLatency(item.firstTokenMs)}</dd></div>
    <div><dt>总耗时</dt><dd>{formatLatency(item.durationMs)}</dd></div>
    <div className="usage-request-id-fact"><dt>请求 ID</dt><dd><code>{item.requestId}</code><Button aria-label={`复制请求 ID ${item.requestId}`} onClick={() => onCopyRequestId(item.requestId)} size="sm" title="复制请求 ID" uniform variant="ghost"><Copy aria-hidden size={14} /></Button></dd></div>
  </dl>;
}

function DesktopRequestRows({ item, onCopyRequestId }: {
  item: GatewayUsageItem;
  onCopyRequestId: (requestId: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const detailId = useId();

  return <tbody className="usage-request-entry" data-request-id={item.requestId}>
    <tr className="usage-request-row">
      <td><time dateTime={item.createdAt}>{formatDate(item.createdAt, true)}</time></td>
      <td><strong>{item.model}</strong></td>
      <td><span className="usage-token-pair"><span>输入 {formatCount(item.inputTokens)}</span><span>输出 {formatCount(item.outputTokens)}</span></span></td>
      <td><strong className="usage-cost">{formatUsdMicros(item.actualCostUsdMicros)}</strong></td>
      <td><Button aria-controls={detailId} aria-expanded={expanded} className="usage-detail-button" onClick={() => setExpanded((current) => !current)} size="sm" variant="ghost">{expanded ? "收起详情" : "查看详情"}<ChevronDown aria-hidden size={15} /></Button></td>
    </tr>
    <tr className="usage-request-detail-row" hidden={!expanded}>
      <td colSpan={5}><div className="usage-request-technical-body" id={detailId}><RequestTechnicalDetails item={item} onCopyRequestId={onCopyRequestId} /></div></td>
    </tr>
  </tbody>;
}

function RequestRows({ items, onCopyRequestId }: {
  items: GatewayUsageItem[];
  onCopyRequestId: (requestId: string) => void;
}) {
  return <>
    <div className="gateway-usage-table table-wrap request-table-desktop">
      <table aria-label="请求记录">
        <thead><tr><th>时间</th><th>模型</th><th>Token</th><th>实际费用</th><th>操作</th></tr></thead>
        {items.map((item) => <DesktopRequestRows item={item} key={item.requestId} onCopyRequestId={onCopyRequestId} />)}
      </table>
    </div>
    <div className="request-list-mobile" role="list">{items.map((item) => <details className="request-mobile-card" key={item.requestId} role="listitem">
      <summary className="request-mobile-summary">
        <span className="request-mobile-heading"><strong>{item.model}</strong><time dateTime={item.createdAt}>{formatDate(item.createdAt, true)}</time></span>
        <span className="request-mobile-business-facts"><span>输入 {formatCount(item.inputTokens)}</span><span>输出 {formatCount(item.outputTokens)}</span><strong className="usage-cost">{formatUsdMicros(item.actualCostUsdMicros)}</strong></span>
        <span className="usage-detail-label">查看详情<ChevronDown aria-hidden size={15} /></span>
      </summary>
      <div className="usage-request-technical-body"><RequestTechnicalDetails item={item} onCopyRequestId={onCopyRequestId} /></div>
    </details>)}</div>
  </>;
}

function UsagePagination({ controller, current, pages }: {
  controller: GatewayUsageController;
  current: number;
  pages: number;
}) {
  if (pages <= 1) return null;
  return <nav aria-label="请求记录分页" className="pagination gateway-usage-pagination">
    <Button aria-label="上一页请求记录" disabled={current <= 1 || controller.usage.loading} onClick={() => void controller.changePage(current - 1)} size="sm" variant="outline"><ChevronLeft aria-hidden size={16} />上一页</Button>
    <span>第 {current} / {pages} 页</span>
    <Button aria-label="下一页请求记录" disabled={current >= pages || controller.usage.loading} onClick={() => void controller.changePage(current + 1)} size="sm" variant="outline">下一页<ChevronRight aria-hidden size={16} /></Button>
  </nav>;
}

function KeyPicker({ controller, onClose, open }: {
  controller: GatewayUsageController;
  onClose: () => void;
  open: boolean;
}) {
  const [draft, setDraft] = useState("");

  useEffect(() => {
    if (open) setDraft("");
  }, [open]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void controller.searchKeys(draft);
  };

  const clear = () => {
    setDraft("");
    void controller.searchKeys("");
  };

  return <Modal
    className="gateway-key-picker"
    footer={<Button onClick={onClose} variant="outline">取消</Button>}
    onClose={onClose}
    open={open}
    title="选择 API 密钥"
  >
    <form className="gateway-key-picker-search" onSubmit={submit}>
      <Field label="搜索 API 密钥" maxLength={100} onChange={(event) => setDraft(event.currentTarget.value)} placeholder="输入名称" type="search" value={draft} />
      <div className="gateway-key-picker-search-actions">
        <Button type="submit" variant="outline"><Search aria-hidden size={16} />搜索</Button>
        <Button disabled={!draft && !controller.keySearch} onClick={clear} type="button" variant="ghost">清除</Button>
      </div>
    </form>
    {controller.keys.loading ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div> : <SourceState
      empty={controller.keys.value?.status === "empty"}
      emptyDescription={controller.keySearch ? "没有符合当前搜索条件的 API 密钥。" : "当前账户还没有 API 密钥。"}
      emptyTitle={controller.keySearch ? "未找到 API 密钥" : "暂无 API 密钥"}
      error={controller.keys.error}
      errorDescription="暂时无法读取 API 密钥，请稍后重试。"
      loading={false}
      onRetry={() => void controller.retryKeys()}
      source={controller.keys.value}
      unavailableDescription="暂时无法读取 API 密钥，请稍后重试。"
      unavailableTitle="API 密钥暂不可用"
    >
      {(keys) => <>
        <ul aria-label="API 密钥列表" className="gateway-key-picker-list">
          {keys.items.map((item) => <li key={item.id}><button
            aria-current={item.id === controller.selectedKeyId ? "true" : undefined}
            className="gateway-key-picker-option"
            onClick={() => { onClose(); void controller.selectKey(item.id); }}
            type="button"
          >
            <span><strong>{item.name}</strong>{item.id === controller.selectedKeyId ? <small>当前选择</small> : null}</span>
            <KeyBadges item={item} />
          </button></li>)}
        </ul>
        <footer className="gateway-key-picker-pagination">
          <span>共 {keys.total} 个</span>
          <Button aria-label="上一页" disabled={keys.page <= 1} onClick={() => void controller.changeKeyPage(keys.page - 1)} size="sm" uniform variant="outline"><ChevronLeft aria-hidden size={16} /></Button>
          <strong>{keys.page} / {keys.pages || 1}</strong>
          <Button aria-label="下一页" disabled={keys.page >= keys.pages} onClick={() => void controller.changeKeyPage(keys.page + 1)} size="sm" uniform variant="outline"><ChevronRight aria-hidden size={16} /></Button>
        </footer>
      </>}
    </SourceState>}
  </Modal>;
}

export function GatewayUsagePage({ controller, onCopyRequestId, onOpenKeys }: GatewayUsagePageProps) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const selectedKey = controller.selection.key;

  const openPicker = () => {
    setPickerOpen(true);
    void controller.searchKeys("");
  };

  const closePicker = () => {
    setPickerOpen(false);
    controller.cancelKeyQuery();
  };

  const selectionContent = () => {
    if (controller.selection.status === "confirming") {
      return <div className="source-loading gateway-usage-selection-state" aria-live="polite"><span className="spinner" />正在确认当前 API 密钥</div>;
    }
    if (controller.selection.status === "unavailable") {
      return <div className="gateway-usage-selection-state"><Alert color="warning" title="当前 API 密钥暂时无法确认" description="已保留当前选择，请重试后再查看用量。" actions={<Button onClick={() => void controller.refresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /></div>;
    }
    if (controller.selection.status === "missing") {
      return <div className="gateway-usage-selection-state"><Alert color="warning" title="当前 API 密钥已不存在" description="请选择其他 API 密钥后再查看用量。" actions={<Button onClick={openPicker} size="sm" variant="outline">选择 API 密钥</Button>} /></div>;
    }
    if (!selectedKey) {
      if (controller.keys.loading || !controller.keys.value) return <div className="source-loading gateway-usage-selection-state" aria-live="polite"><span className="spinner" />正在读取 API 密钥</div>;
      if (controller.keys.value.status === "empty") return <div className="source-empty gateway-usage-selection-state"><h3>暂无 API 密钥</h3><p>创建 API 密钥后即可查看用量。</p><Button onClick={onOpenKeys} size="sm" variant="outline">前往 API 密钥</Button></div>;
      return <div className="gateway-usage-selection-state"><SourceState error={controller.keys.error} errorDescription="暂时无法读取 API 密钥，请稍后重试。" onRetry={() => void controller.refresh()} source={controller.keys.value} unavailableDescription="暂时无法读取 API 密钥，请稍后重试。" unavailableTitle="API 密钥暂不可用">{() => null}</SourceState></div>;
    }

    return <div className="gateway-usage-results">
      <section aria-labelledby="gateway-usage-summary-heading" className="gateway-usage-summary-section">
        <h3 className="sr-only" id="gateway-usage-summary-heading">用量结果</h3>
        <SourceState error={controller.summary.error} errorDescription="暂时无法读取用量结果，请稍后重试。" loading={controller.summary.loading} onRetry={() => void controller.retrySummary()} source={controller.summary.value} unavailableDescription="暂时无法读取用量结果，请稍后重试。" unavailableTitle="用量结果暂不可用">
          {(summary) => <dl className="usage-summary-strip"><div><dt>请求次数</dt><dd>{formatCount(summary.totalRequests)}</dd></div><div><dt>总 Token</dt><dd>{formatCount(summary.totalTokens)}</dd></div><div><dt>实际费用</dt><dd>{formatUsdMicros(summary.totalActualCostUsdMicros)}</dd></div></dl>}
        </SourceState>
      </section>
      <section aria-labelledby="gateway-usage-requests-heading" className="gateway-usage-requests-section">
        <div className="gateway-usage-requests-heading"><h3 id="gateway-usage-requests-heading">请求记录</h3>{controller.usage.value?.available ? <span>共 {formatCount(controller.usage.value.data.total)} 条</span> : null}</div>
        <SourceState empty={controller.usage.value?.status === "empty"} emptyDescription="这个 API 密钥在所选周期内没有请求记录。" emptyTitle="当前范围暂无请求记录" error={controller.usage.error} errorDescription="暂时无法读取请求记录，请稍后重试。" loading={controller.usage.loading} onRetry={() => void controller.retryUsage()} source={controller.usage.value} unavailableDescription="暂时无法读取请求记录，请稍后重试。" unavailableTitle="请求记录暂不可用">
          {(data) => <><RequestRows items={data.items} onCopyRequestId={onCopyRequestId} /><UsagePagination controller={controller} current={controller.page} pages={data.pages} /></>}
        </SourceState>
      </section>
    </div>;
  };

  return <section className="panel gateway-usage-panel" data-slide="C-API-02">
    <div className="panel-title gateway-usage-title"><h2>用量</h2><Button aria-label="刷新" disabled={!selectedKey} onClick={() => void controller.refresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={16} />刷新</Button></div>
    <div className="gateway-usage-toolbar">
      <section aria-label="当前 API 密钥" className="gateway-usage-current-key">
        <span>当前 API 密钥</span>
        <div className="gateway-usage-current-key-row"><div><strong>{selectedKey?.name || "未选择"}</strong>{selectedKey ? <KeyBadges item={selectedKey} /> : null}</div><Button onClick={openPicker} size="sm" variant="outline">更换 API 密钥</Button></div>
      </section>
      <SegmentedControl ariaLabel="统计周期" block onChange={(value) => void controller.selectPeriod(value as GatewayUsagePeriod)} options={[{ value: "today", label: "今日" }, { value: "week", label: "本周" }, { value: "month", label: "本月" }]} value={controller.period} />
    </div>
    {selectionContent()}
    <KeyPicker controller={controller} onClose={closePicker} open={pickerOpen} />
  </section>;
}
