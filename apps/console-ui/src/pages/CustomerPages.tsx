import {
  ArrowRight,
  ChevronLeft,
  ChevronDown,
  ChevronRight,
  CircleDollarSign,
  Copy,
  RefreshCw,
  Server,
  WalletCards
} from "lucide-react";
import { type ReactNode } from "react";

import type { BillingController, CustomerAnnouncementController } from "../app/console-controller-types.ts";
import type { CustomerConsoleRoute } from "../app/console-router.ts";
import type { ConsoleController } from "../app/use-console-controller.ts";
import {
  presentBalanceHistoryStatus,
  presentBalanceHistoryType,
  presentBillingReceiptType,
  presentBillingStatus
} from "../app/customer-experience-model.ts";
import { presentWorkspaceRenewal } from "../app/workspace-experience-model.ts";
import type { AnnouncementDTO, BillingReceipt, SourceEnvelope } from "../api/dtos.ts";
import { GatewayUsagePage } from "../components/gateway-usage/GatewayUsagePage.tsx";
import { KeysPanel } from "../components/keys/KeysPanel.tsx";
import { WorkspaceDetailPage } from "../components/workspaces/WorkspaceDetailPage.tsx";
import { WorkspaceLaunchPage } from "../components/workspaces/WorkspaceLaunchPage.tsx";
import { WorkspaceListPage, WorkspaceSummaryRow } from "../components/workspaces/WorkspaceListPage.tsx";
import { SourceState } from "../components/source/SourceState.tsx";
import { Badge, Button, SegmentedControl } from "../components/ui/index.ts";
import { apiMenu, formatCount, formatDate, formatUsdMicros } from "../console-model.ts";

type CustomerApiRoute = Extract<CustomerConsoleRoute, { navigationId: "customer.api" }>;

function assertNever(value: never): never {
  throw new Error(`Unhandled customer Console route: ${JSON.stringify(value)}`);
}

function sourceData<T>(source: SourceEnvelope<T> | null | undefined): T | null {
  return source?.available ? source.data : null;
}

function receiptAmount(receipt: BillingReceipt) {
  return receipt.refundUsdMicros ?? receipt.chargeUsdMicros ?? receipt.totalUsdMicros;
}

function Metric({ label, value, note, emphasis }: { label: string; value: string; note: string; emphasis?: boolean }) {
  return <article className={`band-metric ${emphasis ? "available-metric" : ""}`}><span>{label}</span><strong>{value}</strong><small>{note}</small></article>;
}

function PageLink({ children, controller, path, className = "" }: { children: ReactNode; controller: ConsoleController; path: string; className?: string }) {
  return <a className={className} href={path} onClick={(event) => { event.preventDefault(); controller.navigate(path); }}>{children}</a>;
}

function Pagination({ current, pages, onChange, label }: { current: number; pages: number; onChange: (page: number) => void; label: string }) {
  if (pages <= 1) return null;
  return (
    <nav aria-label={label} className="pagination">
      <Button disabled={current <= 1} onClick={() => onChange(current - 1)} size="sm" variant="outline"><ChevronLeft aria-hidden size={16} />上一页</Button>
      <span>第 {current} / {pages} 页</span>
      <Button disabled={current >= pages} onClick={() => onChange(current + 1)} size="sm" variant="outline">下一页<ChevronRight aria-hidden size={16} /></Button>
    </nav>
  );
}

function OverviewPage({ controller }: { controller: ConsoleController }) {
  const workspaceRead = controller.customerWorkspaceRead;
  const gatewayAccountRead = controller.gatewayAccountRead;
  const workspaces = sourceData(workspaceRead.workspaces.value);
  const wallet = sourceData(gatewayAccountRead.wallet.value);
  const usage = sourceData(gatewayAccountRead.accountUsage.value);
  const receipts = sourceData(controller.billing.receipts.value)?.receipts || [];
  const announcementController = controller.customerAnnouncements;
  const announcements = sourceData(announcementController.announcements.value)?.items || [];
  const primaryWorkspace = workspaces?.items[0];
  const primaryPath = primaryWorkspace ? `/console/workspaces/${encodeURIComponent(primaryWorkspace.id)}` : "/console/workspaces/new";
  const workspacesUnavailable = workspaceRead.workspaces.value?.status === "unavailable" || Boolean(workspaceRead.workspaces.error);
  const workspacesPending = !workspaceRead.workspaces.value || workspaceRead.workspaces.loading;
  const workspaceCountLabel = workspaces
    ? `当前账户共 ${formatCount(workspaces.total)} 个`
    : workspacesPending && !workspacesUnavailable
      ? "正在读取工作空间总数"
      : "工作空间总数暂不可用";

  return (
    <section className="overview-page" data-slide="C-OV-01">
      <section className="overview-summary" aria-label="账户关键指标">
        <Metric emphasis label="可用余额" note="API 服务余额" value={wallet ? formatUsdMicros(wallet.usdMicros) : "暂不可用"} />
        <Metric label="本月 API 实际费用" note="请求实际消费" value={usage ? formatUsdMicros(usage.totalActualCostUsdMicros) : "暂不可用"} />
        <Metric label="本月请求次数" note="账号级汇总" value={usage ? formatCount(usage.totalRequests) : "暂不可用"} />
        <Metric label="工作空间" note="当前账户总数" value={workspaces ? formatCount(workspaces.total) : "暂不可用"} />
      </section>

      <div className="overview-grid">
        <section className="panel overview-workspaces">
          <div className="panel-title overview-workspace-title">
            <div><h2>工作空间</h2><span>{workspaceCountLabel}</span></div>
            <div className="overview-workspace-actions">
              <Button onClick={() => controller.navigate("/console/workspaces")} size="sm" variant="ghost">全部</Button>
              <Button color="primary" disabled={workspacesPending && !workspacesUnavailable} onClick={() => workspacesUnavailable ? void workspaceRead.refresh() : controller.navigate(primaryPath)} size="sm">
                {workspacesUnavailable ? "重试读取工作空间" : primaryWorkspace ? "查看工作空间" : workspacesPending ? "正在读取工作空间" : "新建工作空间"}
                {workspacesUnavailable ? <RefreshCw aria-hidden size={16} /> : <ArrowRight aria-hidden size={16} />}
              </Button>
            </div>
          </div>
          <SourceState
            emptyTitle="暂无工作空间"
            error={workspaceRead.workspaces.error}
            errorDescription="暂时无法读取工作空间，请稍后重试。"
            loading={workspaceRead.workspaces.loading}
            onRetry={() => void workspaceRead.refresh()}
            source={workspaceRead.workspaces.value}
            unavailableDescription="暂时无法读取工作空间，请稍后重试。"
          >
            {(data) => (
              <div className="overview-workspace-table table-wrap">
                <table><thead><tr><th>工作空间</th><th>套餐</th><th>生命周期状态</th><th>已付至</th><th /></tr></thead><tbody>
                  {data.items.map((workspace) => <WorkspaceSummaryRow controller={controller} key={workspace.id} workspace={workspace} />)}
                </tbody></table>
              </div>
            )}
          </SourceState>
        </section>

        <section className="panel overview-receipts">
          <div className="panel-title"><h2>最近费用</h2><Button onClick={() => controller.navigate("/console/billing")} size="sm" variant="ghost">全部</Button></div>
          <SourceState
            empty={controller.billing.receipts.value?.status === "empty"}
            emptyTitle="暂无费用记录"
            error={controller.billing.receipts.error}
            errorDescription="暂时无法读取费用记录，请稍后重试。"
            loading={controller.billing.receipts.loading}
            onRetry={() => void controller.billing.refresh()}
            source={controller.billing.receipts.value}
            unavailableDescription="暂时无法读取费用记录，请稍后重试。"
            unavailableTitle="费用记录暂不可用"
          >
            {() => <div className="overview-receipt-list">{receipts.map((receipt) => (
              <button key={receipt.receiptId} onClick={() => { controller.billing.setView("receipts"); controller.navigate("/console/billing"); }} type="button">
                <span><strong>{presentBillingReceiptType(receipt.type).label}</strong><small>{formatDate(receipt.createdAt, true)}</small></span>
                <span><strong>{formatUsdMicros(receiptAmount(receipt))}</strong><small>{presentBillingStatus(receipt.status).label}</small></span>
                <ChevronRight aria-hidden size={17} />
              </button>
            ))}</div>}
          </SourceState>
        </section>

        <section className="panel overview-announcements">
          <div className="panel-title"><h2>消息</h2><Button onClick={() => controller.navigate("/console/announcements")} size="sm" variant="ghost">全部</Button></div>
          <SourceState
            empty={announcementController.announcements.value?.status === "empty"}
            emptyTitle="暂无消息"
            error={announcementController.announcements.error}
            errorDescription="暂时无法读取消息，请稍后重试。"
            loading={announcementController.announcements.loading}
            onRetry={() => void announcementController.refresh()}
            source={announcementController.announcements.value}
            unavailableDescription="暂时无法读取消息，请稍后重试。"
          >
            {() => <AnnouncementRows announcements={announcements} controller={announcementController} compact />}
          </SourceState>
        </section>
      </div>
    </section>
  );
}

function ApiTabs({ controller, route }: { controller: ConsoleController; route: CustomerApiRoute }) {
  return <nav aria-label="API 服务导航" className="gateway-tabs">{apiMenu.map((item) => <PageLink className={route.kind === item.kind ? "active" : ""} controller={controller} key={item.path} path={item.path}>{item.label}</PageLink>)}</nav>;
}

function ApiOverview({ controller }: { controller: ConsoleController }) {
  const gatewayAccountRead = controller.gatewayAccountRead;
  const wallet = sourceData(gatewayAccountRead.wallet.value);
  const usage = sourceData(gatewayAccountRead.accountUsage.value);
  const endpointSource = gatewayAccountRead.endpoint.value;
  return <div className="api-overview" data-slide="C-API-01">
    <section className="spend-strip"><div><WalletCards aria-hidden size={19} /><span>可用余额</span><strong>{wallet ? formatUsdMicros(wallet.usdMicros) : "暂不可用"}</strong></div><div><CircleDollarSign aria-hidden size={19} /><span>本月实际费用</span><strong>{usage ? formatUsdMicros(usage.totalActualCostUsdMicros) : "暂不可用"}</strong></div><div><Server aria-hidden size={19} /><span>本月请求次数</span><strong>{usage ? formatCount(usage.totalRequests) : "暂不可用"}</strong></div></section>
    <section className="panel gateway-detail">
      <div className="panel-title"><h2>API 地址</h2></div>
      <SourceState emptyTitle="API 地址暂不可用" error={gatewayAccountRead.endpoint.error} errorDescription="暂时无法读取 API 地址，请稍后重试。" loading={gatewayAccountRead.endpoint.loading} onRetry={() => void gatewayAccountRead.refresh()} source={endpointSource} unavailableDescription="暂时无法读取 API 地址，请稍后重试。" unavailableTitle="API 地址暂不可用">
        {(endpoint) => <div className="api-endpoint-row"><div><span>OpenAI 兼容地址</span><code>{endpoint.baseUrl}</code></div><Button aria-label="复制 API 地址" disabled={!endpoint.baseUrl} onClick={() => void controller.copyText(endpoint.baseUrl, "API 地址已复制")} size="sm" title="复制 API 地址" uniform variant="outline"><Copy aria-hidden size={16} /></Button></div>}
      </SourceState>
    </section>
    <section className="panel"><div className="panel-title"><h2>余额历史</h2></div><SourceState emptyTitle="暂无余额历史" error={gatewayAccountRead.balanceHistory.error} errorDescription="暂时无法读取余额历史，请稍后重试。" loading={gatewayAccountRead.balanceHistory.loading} onRetry={() => void gatewayAccountRead.refresh()} source={gatewayAccountRead.balanceHistory.value} unavailableDescription="暂时无法读取余额历史，请稍后重试。" unavailableTitle="余额历史暂不可用">{(data) => <><div className="table-wrap"><table><thead><tr><th>时间</th><th>类型</th><th>金额</th><th>状态</th></tr></thead><tbody>{data.items.map((item, index) => <tr key={`${item.createdAt}-${index}`}><td>{formatDate(item.usedAt || item.createdAt, true)}</td><td>{presentBalanceHistoryType(item.type).label}</td><td>{formatUsdMicros(item.valueUsdMicros)}</td><td>{presentBalanceHistoryStatus(item.status).label}</td></tr>)}</tbody></table></div><Pagination current={data.page} label="余额历史分页" onChange={(page) => void gatewayAccountRead.changeBalancePage(page)} pages={data.pages} /></>}</SourceState></section>
  </div>;
}

function ApiPage({ controller, route }: { controller: ConsoleController; route: CustomerApiRoute }) {
  let content: ReactNode;
  switch (route.kind) {
    case "customer.api.overview":
      content = <ApiOverview controller={controller} />;
      break;
    case "customer.api.usage":
      content = <GatewayUsagePage controller={controller.gatewayUsage} onCopyRequestId={(requestId) => void controller.copyText(requestId, "请求 ID 已复制")} onOpenKeys={() => controller.navigate("/console/api/keys")} />;
      break;
    case "customer.api.keys":
      content = <KeysPanel csrfToken={controller.session?.csrfToken || ""} endpoint={controller.gatewayAccountRead.endpoint} refreshEndpoint={controller.gatewayAccountRead.refresh} />;
      break;
    default:
      return assertNever(route);
  }
  return <section className="gateway-page api-page"><ApiTabs controller={controller} route={route} />{content}</section>;
}

function BillingPage({ controller }: { controller: ConsoleController }) {
  const billing = controller.billing;
  const workspaceRead = controller.customerWorkspaceRead;
  const receipts = sourceData(billing.receipts.value)?.receipts || [];
  const receipt = sourceData(billing.detail.value);
  const workspaces = sourceData(workspaceRead.workspaces.value)?.items || [];
  const workspaceName = (workspaceId?: string) => workspaces.find((item) => item.id === workspaceId)?.name || "暂不可用";
  return <section className="billing-page">
    <SegmentedControl ariaLabel="费用视图" block onChange={(value) => billing.setView(value)} options={[{ value: "terms", label: "订阅与续费" }, { value: "receipts", label: "账单记录" }]} value={billing.view} />
    {billing.view === "terms" ? <section className="panel billing-surface" data-slide="C-BIL-01"><div className="panel-title"><h2>订阅与续费</h2></div><SourceState source={workspaceRead.workspaces.value} empty={workspaceRead.workspaces.value?.status === "empty"} emptyTitle="暂无订阅" error={workspaceRead.workspaces.error} errorDescription="暂时无法读取订阅与续费信息，请稍后重试。" loading={workspaceRead.workspaces.loading} onRetry={() => void workspaceRead.refresh()} unavailableDescription="暂时无法读取订阅与续费信息，请稍后重试。" unavailableTitle="订阅与续费暂不可用">{(data) => <><div className="table-wrap billing-table-desktop"><table><thead><tr><th>工作空间</th><th>套餐</th><th>月度总价</th><th>计费周期</th><th>续费状态</th><th>自动续费</th></tr></thead><tbody>{data.items.map((item) => <tr key={item.id}><td><PageLink controller={controller} path={`/console/workspaces/${encodeURIComponent(item.id)}`}>{item.name || "未命名工作空间"}</PageLink></td><td>{item.packageId?.toUpperCase() || "-"}</td><td>{formatUsdMicros(item.totalUsdMicros)}</td><td>{item.periodStart && item.paidThrough ? `${formatDate(item.periodStart)} 至 ${formatDate(item.paidThrough)}` : "-"}</td><td>{presentWorkspaceRenewal(item.renewalStatus).label}</td><td>{item.autoRenew === true ? "开启" : item.autoRenew === false ? "关闭" : "-"}</td></tr>)}</tbody></table></div><div className="billing-list-mobile" role="list">{data.items.map((item) => <PageLink controller={controller} key={item.id} path={`/console/workspaces/${encodeURIComponent(item.id)}`}><span><strong>{item.name || "未命名工作空间"}</strong><small>{item.packageId?.toUpperCase() || "-"}</small></span><span><strong>{formatUsdMicros(item.totalUsdMicros)}</strong><small>已付至 {formatDate(item.paidThrough)}</small></span><ChevronRight aria-hidden size={18} /></PageLink>)}</div><Pagination current={workspaceRead.page} label="订阅分页" onChange={(page) => void workspaceRead.changePage(page)} pages={workspaceRead.pages} /></>}</SourceState></section> : <>
      <section className="panel billing-surface" data-slide="C-BIL-02"><div className="panel-title"><h2>账单记录</h2><span>按时间顺序分页</span></div><SourceState source={billing.receipts.value} empty={billing.receipts.value?.status === "empty"} emptyTitle="暂无账单记录" error={billing.receipts.error} errorDescription="暂时无法读取账单记录，请稍后重试。" loading={billing.receipts.loading} onRetry={() => void billing.refresh()} unavailableDescription="暂时无法读取账单记录，请稍后重试。" unavailableTitle="账单记录暂不可用">{() => <><div className="table-wrap billing-table-desktop"><table><thead><tr><th>时间</th><th>类型</th><th>工作空间</th><th>金额</th><th>状态</th><th>操作</th></tr></thead><tbody>{receipts.map((item) => <tr key={item.receiptId}><td>{formatDate(item.createdAt, true)}</td><td>{presentBillingReceiptType(item.type).label}<details className="receipt-row-technical-details"><summary>技术详情</summary><dl><div><dt>Receipt ID</dt><dd><code>{item.receiptId}</code></dd></div><div><dt>Workspace ID</dt><dd><code>{item.workspaceId || "-"}</code></dd></div><div><dt>type</dt><dd><code>{item.type}</code></dd></div><div><dt>status</dt><dd><code>{item.status}</code></dd></div></dl></details></td><td>{workspaceName(item.workspaceId)}</td><td>{formatUsdMicros(receiptAmount(item))}</td><td>{presentBillingStatus(item.status).label}</td><td><Button onClick={() => void billing.openReceipt(item.receiptId)} size="sm" variant="ghost">查看</Button></td></tr>)}</tbody></table></div><div className="billing-list-mobile" role="list">{receipts.map((item) => <button key={item.receiptId} onClick={() => void billing.openReceipt(item.receiptId)} role="listitem"><span><strong>{presentBillingReceiptType(item.type).label}</strong><small>{workspaceName(item.workspaceId)} · {formatDate(item.createdAt, true)}</small></span><span><strong>{formatUsdMicros(receiptAmount(item))}</strong><small>{presentBillingStatus(item.status).label}</small></span><ChevronRight aria-hidden size={18} /></button>)}</div><ReceiptCursorNotice controller={billing} /></>}</SourceState></section>
      {billing.selectedReceiptId ? <ReceiptDetail controller={billing} receipt={receipt} workspaceName={workspaceName(receipt?.workspaceId)} /> : null}
    </>}
  </section>;
}

function ReceiptCursorNotice({ controller }: { controller: BillingController }) {
  const page = sourceData(controller.receipts.value);
  if (!page || (controller.pageNumber === 1 && !page.hasMore)) return null;
  return <nav aria-label="账单记录分页" className="pagination"><Button disabled={!controller.canPrevious} onClick={() => void controller.previousPage()} size="sm" variant="outline"><ChevronLeft aria-hidden size={16} />上一页</Button><span>第 {controller.pageNumber} 页</span><Button disabled={!controller.canNext} onClick={() => void controller.nextPage()} size="sm" variant="outline">下一页<ChevronRight aria-hidden size={16} /></Button></nav>;
}

function ReceiptDetail({ controller, receipt, workspaceName }: { controller: BillingController; receipt: BillingReceipt | null; workspaceName: string }) {
  const components = receipt?.components;
  return <section className="panel receipt-detail" data-slide="C-BIL-03"><div className="panel-title"><h2>收据详情</h2><Button aria-label="关闭收据详情" onClick={controller.closeReceipt} size="sm" variant="ghost">关闭</Button></div><SourceState error={controller.detail.error} errorDescription="暂时无法读取收据详情，请稍后重试。" loading={controller.detail.loading} onRetry={() => controller.selectedReceiptId && void controller.openReceipt(controller.selectedReceiptId)} source={controller.detail.value} unavailableDescription="暂时无法读取收据详情，请稍后重试。" unavailableTitle="收据详情暂不可用">{(detail) => <><dl className="data-list"><div><dt>类型</dt><dd>{presentBillingReceiptType(detail.type).label}</dd></div><div><dt>状态</dt><dd>{presentBillingStatus(detail.status).label}</dd></div><div><dt>日期</dt><dd>{formatDate(detail.createdAt, true)}</dd></div><div><dt>金额</dt><dd>{formatUsdMicros(receiptAmount(detail))}</dd></div><div><dt>计费周期</dt><dd>{detail.periodStart && detail.paidThrough ? `${formatDate(detail.periodStart)} 至 ${formatDate(detail.paidThrough)}` : "-"}</dd></div><div><dt>工作空间</dt><dd>{workspaceName}</dd></div></dl><details className="receipt-technical-details"><summary><span>技术详情</span><ChevronDown aria-hidden size={16} /></summary><div className="receipt-technical-details__body"><dl className="data-list"><div><dt>Receipt ID</dt><dd><code>{detail.receiptId}</code></dd></div><div><dt>type</dt><dd><code>{detail.type}</code></dd></div><div><dt>status</dt><dd><code>{detail.status}</code></dd></div><div><dt>Workspace ID</dt><dd><code>{detail.workspaceId || "-"}</code></dd></div><div><dt>priceVersion</dt><dd><code>{detail.priceVersion || "-"}</code></dd></div><div><dt>chargeReference</dt><dd><code>{detail.chargeReference || "-"}</code></dd></div><div><dt>compute evidence</dt><dd>{components?.compute ? formatUsdMicros(components.compute.chargeUsdMicros) : "-"}</dd></div><div><dt>storage evidence</dt><dd>{components?.storage ? `${formatUsdMicros(components.storage.chargeUsdMicros)} · ${components.storage.sizeGb} GB` : "-"}</dd></div>{detail.refundUsdMicros !== undefined ? <div><dt>refund evidence</dt><dd>{formatUsdMicros(detail.refundUsdMicros)}</dd></div> : null}</dl></div></details></>}</SourceState></section>;
}

function AnnouncementRows({ announcements, compact, controller }: { announcements: AnnouncementDTO[]; compact?: boolean; controller: CustomerAnnouncementController }) {
  return <div className={compact ? "compact-announcement-list" : "announcement-list"}>{announcements.map((announcement) => <article className="announcement-item" key={announcement.id}><header><div><h3>{announcement.title}</h3><Badge color={announcement.read ? "secondary" : "info"}>{announcement.read ? "已读" : "未读"}</Badge></div><span>{formatDate(announcement.publishedAt || announcement.startsAt || announcement.updatedAt, true)}</span></header><p>{announcement.body}</p>{announcement.read ? null : <Button busy={controller.busyAnnouncementId === announcement.id} onClick={() => void controller.markRead(announcement.id)} size="sm" variant="outline">标记已读</Button>}</article>)}</div>;
}

function AnnouncementsPage({ controller }: { controller: ConsoleController }) {
  const announcementController = controller.customerAnnouncements;
  const announcements = sourceData(announcementController.announcements.value)?.items || [];
  return <section className="announcements-page" data-slide="C-ANN-01"><section className="panel"><div className="panel-title"><div><h2>消息列表</h2><span>{announcements.length ? `${announcements.length} 条` : ""}</span></div><Button onClick={() => void announcementController.refresh()} variant="outline"><RefreshCw aria-hidden size={16} />刷新</Button></div><SourceState empty={announcementController.announcements.value?.status === "empty"} emptyTitle="暂无消息" error={announcementController.announcements.error} errorDescription="暂时无法读取消息，请稍后重试。" loading={announcementController.announcements.loading} onRetry={() => void announcementController.refresh()} source={announcementController.announcements.value} unavailableDescription="暂时无法读取消息，请稍后重试。" unavailableTitle="消息暂不可用">{() => <AnnouncementRows announcements={announcements} controller={announcementController} />}</SourceState></section></section>;
}

export function CustomerPages({ controller, route }: { controller: ConsoleController; route: CustomerConsoleRoute }) {
  switch (route.kind) {
    case "customer.overview":
      return <OverviewPage controller={controller} />;
    case "customer.workspaces":
      return <WorkspaceListPage controller={controller} />;
    case "customer.workspace-new":
      return <WorkspaceLaunchPage controller={controller.workspaceLaunch} onBack={() => controller.navigate("/console/workspaces")} onRefresh={controller.refreshCurrentPage} />;
    case "customer.workspace-detail":
      return <WorkspaceDetailPage controller={controller} />;
    case "customer.api.overview":
    case "customer.api.usage":
    case "customer.api.keys":
      return <ApiPage controller={controller} route={route} />;
    case "customer.billing":
      return <BillingPage controller={controller} />;
    case "customer.announcements":
      return <AnnouncementsPage controller={controller} />;
    default:
      return assertNever(route);
  }
}
