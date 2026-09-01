import {
  AlertCircle,
  ArrowRight,
  ChevronLeft,
  ChevronDown,
  ChevronRight,
  CircleCheck,
  CircleX,
  CircleDollarSign,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Plus,
  RefreshCw,
  Server,
  Trash2,
  WalletCards
} from "lucide-react";
import { RadioGroup } from "@openai/apps-sdk-ui/components/RadioGroup";
import { useEffect, useRef, useState, type ReactNode } from "react";

import type { BillingController, CustomerAnnouncementController, GatewayUsageController, WorkspaceLaunchController, WorkspaceSecretController } from "../app/console-controller-types.ts";
import type { CustomerConsoleRoute } from "../app/console-router.ts";
import type { ConsoleController } from "../app/use-console-controller.ts";
import {
  presentBalanceHistoryStatus,
  presentBalanceHistoryType,
  presentBillingReceiptType,
  presentBillingStatus
} from "../app/customer-experience-model.ts";
import {
  presentWorkspaceBudget,
  presentWorkspaceLaunch,
  presentWorkspaceLaunchStage,
  presentWorkspaceLifecycle,
  presentWorkspaceQuote,
  presentWorkspaceRenewal,
  presentWorkspaceRuntime
} from "../app/workspace-experience-model.ts";
import type {
  AnnouncementDTO,
  BillingReceipt,
  GatewayUsageItem,
  GatewayUsagePeriod,
  PlanId,
  PricingPlan,
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceGatewayBudgetUpdateRequest,
  WorkspaceRuntimeDTO
} from "../api/dtos.ts";
import { KeysPanel } from "../components/keys/KeysPanel.tsx";
import { SourceState } from "../components/source/SourceState.tsx";
import { Alert, Badge, Button, Checkbox, Field, SegmentedControl, Select } from "../components/ui/index.ts";
import { apiMenu, formatCount, formatDate, formatUsdMicros } from "../console-model.ts";

type CustomerApiRoute = Extract<CustomerConsoleRoute, { navigationId: "customer.api" }>;

function assertNever(value: never): never {
  throw new Error(`Unhandled customer Console route: ${JSON.stringify(value)}`);
}

function billingUnitLabel(billingUnit?: string) {
  if (billingUnit === "calendar_month") return "按自然月计费";
  return "暂不可用";
}

function sourceData<T>(source: SourceEnvelope<T> | null | undefined): T | null {
  return source?.available ? source.data : null;
}

function formatLatency(value: number | null) {
  return value === null ? "-" : `${formatCount(value)} ms`;
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

  return (
    <section className="overview-page" data-slide="C-OV-01">
      <section className="overview-summary" aria-label="账户关键指标">
        <Metric emphasis label="可用余额" note="API 服务余额" value={wallet ? formatUsdMicros(wallet.usdMicros) : "暂不可用"} />
        <Metric label="本月 API 实际费用" note="请求实际消费" value={usage ? formatUsdMicros(usage.totalActualCostUsdMicros) : "暂不可用"} />
        <Metric label="本月请求次数" note="账号级汇总" value={usage ? formatCount(usage.totalRequests) : "暂不可用"} />
        <Metric label="工作空间" note="当前账户总数" value={workspaces ? formatCount(workspaces.total) : "暂不可用"} />
      </section>

      <div className="overview-primary-action">
        <Button color="primary" disabled={workspacesPending && !workspacesUnavailable} onClick={() => workspacesUnavailable ? void workspaceRead.refresh() : controller.navigate(primaryPath)}>
          {workspacesUnavailable ? "重试读取工作空间" : primaryWorkspace ? "查看工作空间" : workspacesPending ? "正在读取工作空间" : "新建工作空间"}
          {workspacesUnavailable ? <RefreshCw aria-hidden size={16} /> : <ArrowRight aria-hidden size={16} />}
        </Button>
      </div>

      <div className="overview-grid">
        <section className="panel overview-workspaces">
          <div className="panel-title"><h2>工作空间</h2><Button onClick={() => controller.navigate("/console/workspaces")} size="sm" variant="ghost">全部</Button></div>
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

function WorkspaceSummaryRow({ controller, workspace }: { controller: ConsoleController; workspace: WorkspaceDTO }) {
  const path = `/console/workspaces/${encodeURIComponent(workspace.id)}`;
  return <tr><td><PageLink controller={controller} path={path}><strong>{workspace.name || "未命名工作空间"}</strong></PageLink></td><td>{workspace.packageId?.toUpperCase() || "暂不可用"}</td><td><Badge color="secondary">{presentWorkspaceLifecycle(workspace.state).label}</Badge></td><td>{formatDate(workspace.paidThrough)}</td><td><PageLink controller={controller} path={path}><ChevronRight aria-label="查看" size={17} /></PageLink></td></tr>;
}

function WorkspaceListPage({ controller }: { controller: ConsoleController }) {
  const workspaceRead = controller.customerWorkspaceRead;
  const workspacesUnavailable = workspaceRead.workspaces.value?.status === "unavailable" || Boolean(workspaceRead.workspaces.error);
  const workspacesPending = !workspaceRead.workspaces.value || workspaceRead.workspaces.loading;
  return (
    <section className="workspace-list-page" data-slide="C-WS-01">
      <div className="page-toolbar"><p>工作空间总数：{workspaceRead.workspaces.value?.available ? formatCount(workspaceRead.workspaces.value.data.total) : "暂不可用"}</p><Button color="primary" disabled={workspacesPending && !workspacesUnavailable} onClick={() => workspacesUnavailable ? void workspaceRead.refresh() : controller.navigate("/console/workspaces/new")}>{workspacesUnavailable ? <RefreshCw aria-hidden size={16} /> : <Plus aria-hidden size={16} />}{workspacesUnavailable ? "重试读取" : workspacesPending ? "正在读取" : "新建工作空间"}</Button></div>
      {controller.workspaceLaunch.launchOperation && !["succeeded", "failed", "refunded"].includes(controller.workspaceLaunch.launchOperation.status) ? (
        <LaunchOperation controller={controller.workspaceLaunch} compact onBack={() => controller.navigate("/console/workspaces")} onRefresh={controller.refreshCurrentPage} />
      ) : null}
      <section className="panel workspace-list-panel">
        <div className="workspace-list-head"><span>工作空间</span><span>套餐</span><span>生命周期状态</span><span>已付至</span><span /></div>
        <SourceState
          emptyTitle="暂无工作空间"
          emptyDescription="当前账号尚未开通工作空间。"
          error={workspaceRead.workspaces.error}
          errorDescription="暂时无法读取工作空间，请稍后重试。"
          loading={workspaceRead.workspaces.loading}
          onRetry={() => void workspaceRead.refresh()}
          source={workspaceRead.workspaces.value}
          unavailableDescription="暂时无法读取工作空间，请稍后重试。"
        >
          {(data) => <div className="workspace-list" role="list">{data.items.map((workspace) => (
            <PageLink className="workspace-list-row" controller={controller} key={workspace.id} path={`/console/workspaces/${encodeURIComponent(workspace.id)}`}>
              <span className="workspace-list-name"><strong>{workspace.name || "未命名工作空间"}</strong></span>
              <span><strong>{workspace.packageId?.toUpperCase() || "暂不可用"}</strong><small>{workspace.storageGb ? `${workspace.storageGb} GB` : "规格暂不可用"}</small></span>
              <span><strong>{presentWorkspaceLifecycle(workspace.state).label}</strong><small>生命周期状态</small></span>
              <span><strong>{formatDate(workspace.paidThrough)}</strong><small>权益截止</small></span>
              <ChevronRight aria-hidden size={18} />
            </PageLink>
          ))}</div>}
        </SourceState>
        <Pagination current={workspaceRead.page} label="工作空间分页" onChange={(page) => void workspaceRead.changePage(page)} pages={workspaceRead.pages} />
      </section>
    </section>
  );
}

function PlanOption({ controller, plan }: { controller: WorkspaceLaunchController; plan: PricingPlan }) {
  const preview = controller.previews[plan.id];
  const selected = controller.launchPlan === plan.id && plan.available;
  const unavailablePrice = plan.available ? "报价读取中" : "暂不可用";
  return (
    <RadioGroup.Item block className={`workspace-plan-option ${selected ? "selected" : ""} ${plan.available ? "" : "unavailable"}`} disabled={!plan.available} value={plan.id}>
      <span className="workspace-plan-option__identity"><span><strong>{plan.name}</strong><Badge color={plan.available ? "success" : "secondary"}>{plan.available ? "可售" : "暂不可用"}</Badge></span></span>
      <span className="workspace-plan-option__fact workspace-plan-option__component"><strong>{preview?.compute ? formatUsdMicros(preview.compute.chargeUsdMicros) : unavailablePrice}</strong><small>计算</small></span>
      <span className="workspace-plan-option__fact workspace-plan-option__component"><strong>{preview?.storage ? formatUsdMicros(preview.storage.chargeUsdMicros) : unavailablePrice}</strong><small>存储</small></span>
      <span className="workspace-plan-option__fact workspace-plan-option__total"><strong>{preview ? formatUsdMicros(preview.totalChargeUsdMicros) : unavailablePrice}</strong><small>{preview ? billingUnitLabel(preview.billingUnit) : "月度总额"}</small></span>
    </RadioGroup.Item>
  );
}

function WorkspaceLaunchSteps({ current }: { current: "configure" | "confirm" | "operation" }) {
  const steps = [
    ["configure", "配置"],
    ["confirm", "核对"],
    ["operation", "开通状态"]
  ] as const;
  return (
    <ol aria-label="工作空间开通步骤" className="workspace-launch-steps">
      {steps.map(([step, label], index) => (
        <li aria-current={current === step ? "step" : undefined} className={current === step ? "active" : ""} key={step}>
          <span>{index + 1}</span><strong>{label}</strong>
        </li>
      ))}
    </ol>
  );
}

function WorkspaceOrderSummary({
  action,
  controller,
  mode = "quote"
}: {
  action?: ReactNode;
  controller: WorkspaceLaunchController;
  mode?: "quote" | "operation";
}) {
  const operation = mode === "operation" ? controller.launchOperation : null;
  const planId = operation?.packageId || controller.selectedPlan?.id;
  const plan = planId ? controller.catalog.value?.packages.find((item) => item.id === planId) : null;
  const preview = planId ? controller.previews[planId] : undefined;
  const quote = mode === "quote" ? presentWorkspaceQuote({
    selectedPriceUsdMicros: controller.selectedPrice,
    customerOwned: controller.customerOwned
  }) : null;
  const total = operation?.totalChargeUsdMicros ?? quote?.totalUsdMicros ?? null;
  const billingCycle = preview?.billingUnit === "calendar_month" ? "按自然月计费" : "暂不可用";

  return (
    <aside className="workspace-order-summary">
      <header><span>{mode === "operation" ? "开通摘要" : "订单摘要"}</span><strong>{plan?.name || operation?.packageId?.toUpperCase() || "暂未选择"}</strong></header>
      {mode === "quote" ? (
        <>
          <section className="workspace-order-summary__prices">
            <h3>价格构成（参考）</h3>
            <dl>
              <div><dt>计算</dt><dd>{preview?.compute ? formatUsdMicros(preview.compute.chargeUsdMicros) : "暂不可用"}</dd></div>
              <div><dt>存储</dt><dd>{preview?.storage ? formatUsdMicros(preview.storage.chargeUsdMicros) : "暂不可用"}</dd></div>
              <div className="workspace-order-summary__total"><dt>实际应付</dt><dd>{total !== null ? formatUsdMicros(total) : "暂不可用"}</dd></div>
            </dl>
          </section>
          <dl className="workspace-order-summary__facts">
            {controller.customerOwned ? (
              <div><dt>开通方式</dt><dd>客户权益（无需预付）</dd></div>
            ) : (
              <div><dt>可用余额</dt><dd>{controller.walletUsdMicros ? formatUsdMicros(controller.walletUsdMicros) : "暂不可用"}</dd></div>
            )}
            <div><dt>计费周期</dt><dd>{billingCycle}</dd></div>
            <div><dt>续费</dt><dd>{controller.customerOwned ? "不适用" : controller.launchAutoRenew ? "自动续费开启" : "自动续费关闭"}</dd></div>
          </dl>
          {quote?.kind === "prepaid" && controller.balanceSufficient === false ? <p className="workspace-order-summary__warning">余额不足，请联系管理员处理。</p> : null}
        </>
      ) : (
        <dl className="workspace-order-summary__facts">
          <div><dt>工作空间</dt><dd>{operation?.name || "暂不可用"}</dd></div>
          <div><dt>月度总额</dt><dd>{total !== null ? formatUsdMicros(total) : "暂不可用"}</dd></div>
          <div><dt>价格版本</dt><dd>{operation?.priceVersion || "暂不可用"}</dd></div>
          <div><dt>续费</dt><dd>{operation?.autoRenew ? "自动续费开启" : "自动续费关闭"}</dd></div>
        </dl>
      )}
      {action ? <div className="workspace-order-summary__action">{action}</div> : null}
    </aside>
  );
}

function WorkspaceLaunchPage({
  controller,
  onBack,
  onRefresh
}: {
  controller: WorkspaceLaunchController;
  onBack: () => void;
  onRefresh: () => Promise<void>;
}) {
  const catalog = controller.catalog.value;
  if (controller.launchOperation) {
    return <section className="workspace-launch-page" data-slide="C-WS-04"><LaunchOperation controller={controller} onBack={onBack} onRefresh={onRefresh} /></section>;
  }
  if (controller.launchRecoveryState !== "clear") {
    const checking = controller.launchRecoveryState === "idle" || controller.launchRecoveryState === "checking";
    const conflict = controller.launchRecoveryState === "conflict";
    return (
      <section className="workspace-launch-page" data-slide="C-WS-04">
        <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
        {checking ? (
          <div className="source-loading" aria-live="polite"><span className="spinner" />正在确认是否存在未完成的开通操作</div>
        ) : (
          <Alert
            color="warning"
            indicator={<AlertCircle size={18} />}
            title={conflict ? "存在多个待确认的开通操作" : "暂时无法确认开通状态"}
            description={conflict
              ? "为避免重复扣费，请暂勿再次购买。刷新后确认仅有一个或没有未完成操作，才能继续开通。"
              : "暂时无法确认是否已有未完成操作。为避免重复扣费，请暂勿再次购买。"}
            actions={<Button onClick={() => void onRefresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重新检查</Button>}
          />
        )}
      </section>
    );
  }

  return (
    <section className="workspace-launch-page" data-slide={controller.launchStep === "confirm" ? "C-WS-03" : "C-WS-02"}>
      <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
      <WorkspaceLaunchSteps current={controller.launchStep} />
      {controller.launchStep === "configure" ? (
        <form className="workspace-launch-layout" onSubmit={(event) => { event.preventDefault(); controller.reviewWorkspaceLaunch(); }}>
          <section className="workspace-launch-config">
            <header><h2>新建工作空间</h2></header>
            <Field label="工作空间名称" maxLength={80} onChange={(event) => controller.setLaunchName(event.currentTarget.value)} placeholder="例如：产品研发" required value={controller.launchName} />
            <fieldset><legend>选择套餐</legend>
              {controller.catalog.loading && !catalog ? <div className="source-loading"><span className="spinner" />正在读取计划与价格</div> : null}
              {controller.catalog.error ? <div className="inline-error"><AlertCircle aria-hidden size={16} />计划与价格暂不可用<Button onClick={() => void onRefresh()} size="sm" variant="ghost">重试</Button></div> : null}
              {catalog ? <RadioGroup<PlanId> aria-label="工作空间套餐" className="workspace-plan-list" direction="col" name="workspace-plan" onChange={controller.setLaunchPlan} value={controller.launchPlan}>{catalog.packages.filter((plan) => plan.available && (plan.id === "basic" || plan.id === "pro")).map((plan) => <PlanOption controller={controller} key={plan.id} plan={plan} />)}</RadioGroup> : null}
            </fieldset>
            {!controller.customerOwned ? <div className="launch-confirm-check"><Checkbox checked={controller.launchAutoRenew} label="自动续费" onChange={controller.setLaunchAutoRenew} /></div> : null}
          </section>
          <WorkspaceOrderSummary
            action={<Button color="primary" disabled={!controller.launchName.trim() || !controller.selectedPlan || controller.selectedPrice === null || controller.balanceSufficient !== true} type="submit">核对开通信息<ArrowRight aria-hidden size={16} /></Button>}
            controller={controller}
          />
        </form>
      ) : <WorkspaceLaunchConfirm controller={controller} />}
    </section>
  );
}

function WorkspaceLaunchConfirm({ controller }: { controller: WorkspaceLaunchController }) {
  const headingRef = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    window.scrollTo({ top: 0, left: 0, behavior: "auto" });
    headingRef.current?.focus({ preventScroll: true });
  }, []);
  const plan = controller.selectedPlan;
  const preview = plan ? controller.previews[plan.id] : undefined;
  if (!plan) return <div className="empty-panel">计划与价格暂不可用</div>;
  const quote = presentWorkspaceQuote({
    selectedPriceUsdMicros: controller.selectedPrice,
    customerOwned: controller.customerOwned
  });
  return (
    <div className="workspace-launch-layout">
      <section className="workspace-launch-review">
        <header><h2 ref={headingRef} tabIndex={-1}>确认开通信息</h2></header>
        <dl className="launch-confirm-list">
          <div><dt>工作空间名称</dt><dd>{controller.launchName.trim()}</dd></div>
          <div><dt>套餐</dt><dd>{plan.name}</dd></div>
          <div><dt>价格版本</dt><dd>{preview?.priceVersion || controller.catalog.value?.priceVersion || "暂不可用"}</dd></div>
          <div><dt>计费周期</dt><dd>{billingUnitLabel(preview?.billingUnit || controller.catalog.value?.billingUnit)}</dd></div>
          <div><dt>自动续费</dt><dd>{controller.customerOwned ? "不适用" : controller.launchAutoRenew ? "开启" : "关闭"}</dd></div>
        </dl>
        <div className="launch-confirm-check"><Checkbox checked={controller.launchConfirmed} label={quote.confirmationLabel} onChange={controller.setLaunchConfirmed} /></div>
        <footer><Button onClick={() => { controller.setLaunchStep("configure"); controller.setLaunchConfirmed(false); }} variant="outline">返回修改</Button></footer>
      </section>
      <WorkspaceOrderSummary
        action={<Button busy={controller.busy} color="primary" disabled={!quote.canConfirm || !controller.launchConfirmed || controller.balanceSufficient !== true} onClick={() => void controller.submitWorkspaceLaunch()}>{quote.submitLabel}</Button>}
        controller={controller}
      />
    </div>
  );
}

function LaunchOperation({
  compact,
  controller,
  onBack,
  onRefresh
}: {
  compact?: boolean;
  controller: WorkspaceLaunchController;
  onBack: () => void;
  onRefresh: () => Promise<void>;
}) {
  const operation = controller.launchOperation;
  if (!operation) return null;
  const operationPresentation = presentWorkspaceLaunch(operation);
  const presentation = controller.launchPollIssue
    ? presentWorkspaceLaunch({ status: "unconfirmed", workspaceId: undefined })
    : operationPresentation;
  const stagePresentation = presentWorkspaceLaunchStage(operation.phase);
  const resultUnconfirmed = presentation.kind === "unconfirmed";
  const content = (
    <section className={`launch-operation ${compact ? "launch-operation--compact" : ""}`} data-slide="C-WS-04">
      <div className="launch-operation-head"><div><h2>{presentation.title}</h2><p>{presentation.summary}</p></div></div>
      <div className="launch-current-phase"><span>当前进度</span><strong>{stagePresentation.label}</strong></div>
      <details className="launch-technical-details">
        <summary>技术详情</summary>
        <div className="launch-technical-details__body">
          <dl className="operation-readback">
            <div><dt>operation ID</dt><dd><code>{operation.operationId}</code></dd></div>
            <div><dt>status</dt><dd><code>{operation.status}</code></dd></div>
            <div><dt>phase</dt><dd><code>{operation.phase}</code></dd></div>
            <div><dt>errorCode</dt><dd><code>{operation.errorCode || "无"}</code></dd></div>
            <div><dt>blockReason</dt><dd><code>{operation.blockReason || "无"}</code></dd></div>
            <div><dt>failureStage</dt><dd><code>{operation.failureStage || "无"}</code></dd></div>
            <div><dt>创建时间</dt><dd>{formatDate(operation.createdAt, true)}</dd></div>
            <div><dt>最后更新</dt><dd>{formatDate(operation.updatedAt, true)}</dd></div>
          </dl>
          <section aria-label="开通检查" className="launch-diagnostic">
            <header><span>checks</span></header>
          {operation.checks?.length ? (
            <ul>
              {operation.checks.map((check) => (
                <li className={check.ok ? "is-ready" : "is-blocked"} key={check.name}>
                  {check.ok ? <CircleCheck aria-hidden size={16} /> : <CircleX aria-hidden size={16} />}
                  <code>{check.name}</code>
                  <span>{check.ok ? "通过" : "未通过"}</span>
                </li>
              ))}
            </ul>
            ) : <p>暂无检查记录</p>}
          </section>
        </div>
      </details>
      <div className="launch-operation-actions">
        {!resultUnconfirmed && operationPresentation.canOpenWorkspace ? <Button color="primary" onClick={() => void controller.openLaunchedWorkspace()}>查看工作空间</Button> : null}
        <Button onClick={() => void (controller.launchPollIssue === "readback" ? controller.openLaunchedWorkspace() : onRefresh())} variant="outline"><RefreshCw aria-hidden size={16} />刷新状态</Button>
        {!resultUnconfirmed && ["failed", "refunded"].includes(operationPresentation.kind) ? <Button onClick={onBack} variant="outline">返回列表</Button> : null}
      </div>
    </section>
  );
  if (compact) return content;
  return (
    <>
      <WorkspaceLaunchSteps current="operation" />
      <div className="workspace-launch-layout workspace-launch-layout--operation">
        {content}
        <WorkspaceOrderSummary controller={controller} mode="operation" />
      </div>
    </>
  );
}

function SecretRow({ busy, label, onCopy, onHide, onReveal, revealed, value }: { busy: boolean; label: string; onCopy: () => void; onHide: () => void; onReveal: () => void; revealed: boolean; value?: string }) {
  return <div><dt>{label}</dt><dd className="credential-actions"><code>{revealed ? value || "-" : "••••••••••••"}</code>{revealed ? <><Button aria-label="隐藏" onClick={onHide} size="sm" uniform variant="ghost"><EyeOff aria-hidden size={16} /></Button><Button aria-label="复制" onClick={onCopy} size="sm" uniform variant="ghost"><Copy aria-hidden size={16} /></Button></> : <Button aria-label="显示" busy={busy} onClick={onReveal} size="sm" variant="outline"><Eye aria-hidden size={16} />显示</Button>}</dd></div>;
}

type WorkspaceBudgetLimitField = "quotaUsdMicros" | "rateLimit5hUsdMicros" | "rateLimit1dUsdMicros" | "rateLimit7dUsdMicros";
type WorkspaceBudgetForm = Record<WorkspaceBudgetLimitField, string> & { enabled: boolean };

const workspaceBudgetLimitFields: ReadonlyArray<{ field: WorkspaceBudgetLimitField; label: string }> = [
  { field: "quotaUsdMicros", label: "总额度（micros）" },
  { field: "rateLimit5hUsdMicros", label: "5 小时限额（micros）" },
  { field: "rateLimit1dUsdMicros", label: "1 天限额（micros）" },
  { field: "rateLimit7dUsdMicros", label: "7 天限额（micros）" }
];

function workspaceBudgetForm(budget: WorkspaceGatewayBudgetDTO | null): WorkspaceBudgetForm {
  return {
    quotaUsdMicros: budget?.quotaUsdMicros || "",
    rateLimit5hUsdMicros: budget?.rateLimit5hUsdMicros || "",
    rateLimit1dUsdMicros: budget?.rateLimit1dUsdMicros || "",
    rateLimit7dUsdMicros: budget?.rateLimit7dUsdMicros || "",
    enabled: budget?.enabled ?? false
  };
}

function parseBudgetMicros(value: string) {
  if (value.trim() === "" || !/^(0|[1-9]\d*)$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

function WorkspaceBudgetPanel({ controller }: { controller: ConsoleController }) {
  const budget = sourceData(controller.sources.workspaceBudget.value);
  const [form, setForm] = useState<WorkspaceBudgetForm>(() => workspaceBudgetForm(budget));
  const [errors, setErrors] = useState<Partial<Record<WorkspaceBudgetLimitField, string>>>({});

  useEffect(() => {
    setForm(workspaceBudgetForm(budget));
    setErrors({});
  }, [budget]);

  const updateLimit = (field: WorkspaceBudgetLimitField, value: string) => {
    setForm((current) => ({ ...current, [field]: value }));
    setErrors((current) => ({ ...current, [field]: "" }));
  };

  const save = () => {
    const input: WorkspaceGatewayBudgetUpdateRequest = { enabled: form.enabled };
    const nextErrors: Partial<Record<WorkspaceBudgetLimitField, string>> = {};
    for (const { field } of workspaceBudgetLimitFields) {
      const value = parseBudgetMicros(form[field]);
      if (value === null) nextErrors[field] = "请输入非负安全整数";
      else input[field] = value;
    }
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) return;
    void controller.updateWorkspaceBudget(input);
  };

  return <div className="workspace-budget-panel">
    <div className="workspace-settings-heading"><h3>模型预算</h3><p>设置 API 密钥的消费上限。</p></div>
    {budget ? <div className="workspace-details key-form">
        <div className="key-form-grid">
          {workspaceBudgetLimitFields.map(({ field, label }) => <Field description="0 表示不限额" disabled={controller.workspaceBudgetBusy} error={errors[field]} inputMode="numeric" key={field} label={label} min="0" onChange={(event) => updateLimit(field, event.currentTarget.value)} required step="1" type="number" value={form[field]} />)}
        </div>
        <Checkbox checked={form.enabled} disabled={controller.workspaceBudgetBusy} label="启用 API 密钥" onChange={(enabled) => setForm((current) => ({ ...current, enabled }))} />
        <dl className="data-list">
          <div><dt>状态</dt><dd>{presentWorkspaceBudget(budget.status).label}</dd></div>
          <div><dt>总额度已用</dt><dd>{formatUsdMicros(budget.quotaUsedUsdMicros)}</dd></div>
          <div><dt>5 小时已用</dt><dd>{formatUsdMicros(budget.usage5hUsdMicros)}</dd></div>
          <div><dt>1 天已用</dt><dd>{formatUsdMicros(budget.usage1dUsdMicros)}</dd></div>
          <div><dt>7 天已用</dt><dd>{formatUsdMicros(budget.usage7dUsdMicros)}</dd></div>
          <div><dt>更新时间</dt><dd>{budget.updatedAt ? formatDate(budget.updatedAt, true) : "-"}</dd></div>
        </dl>
        <div className="workspace-actions">
          <Button busy={controller.workspaceBudgetBusy} color="primary" onClick={save}>保存预算</Button>
          <Button busy={controller.workspaceBudgetBusy} onClick={() => void controller.updateWorkspaceBudget({ resetQuota: true })} variant="outline">重置总额度用量</Button>
          <Button busy={controller.workspaceBudgetBusy} onClick={() => void controller.updateWorkspaceBudget({ resetRateLimitUsage: true })} variant="outline">重置滚动窗口用量</Button>
        </div>
      </div> : controller.sources.workspaceBudget.loading && !controller.sources.workspaceBudget.value ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div> : controller.sources.workspaceBudget.value?.available === false ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="模型预算暂不可用" description="暂时无法确认预算设置，请稍后刷新。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /> : controller.sources.workspaceBudget.error ? <Alert color="danger" title="模型预算暂不可用" description="暂时无法确认预算设置，请稍后刷新。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /> : <div className="source-loading" aria-live="polite"><span className="spinner" />等待读取</div>}
  </div>;
}

function WorkspaceAccessRows({ controller, runtime }: {
  controller: WorkspaceSecretController;
  runtime: WorkspaceRuntimeDTO;
}) {
  return <dl className="data-list"><div><dt>登录账号</dt><dd>{runtime.access?.username || controller.credential?.username || "-"}</dd></div>
    <SecretRow busy={controller.workspaceBusy} label="登录密码" onCopy={() => void controller.copyWorkspacePassword()} onHide={controller.clear} onReveal={() => void controller.revealWorkspacePassword()} revealed={Boolean(controller.credential)} value={controller.credential?.password} />
    <SecretRow busy={controller.gatewayKeyBusy} label="API 密钥" onCopy={() => void controller.copyWorkspaceKey()} onHide={controller.clear} onReveal={() => void controller.revealWorkspaceKey()} revealed={Boolean(controller.gatewayKey)} value={controller.gatewayKey?.value} />
    <div><dt>密码管理</dt><dd className="workspace-actions"><Button busy={controller.workspaceBusy} onClick={() => void controller.rotateWorkspacePassword()} variant="outline">轮换密码</Button></dd></div>
  </dl>;
}

function WorkspaceTechnicalDetails({ controller, detail, runtime }: {
  controller: ConsoleController;
  detail: WorkspaceDTO;
  runtime: WorkspaceRuntimeDTO | null;
}) {
  const runtimeSource = controller.fabricRuntimeRead.runtime.value;
  const budgetSource = controller.sources.workspaceBudget.value;
  const budget = sourceData(budgetSource);
  return <details className="workspace-technical-details">
    <summary><span>技术详情</span><ChevronDown aria-hidden size={16} /></summary>
    <div className="workspace-technical-details__body">
      <dl className="data-list">
        <div><dt>Workspace ID</dt><dd><code>{detail.id}</code></dd></div>
        <div><dt>priceVersion</dt><dd><code>{detail.priceVersion || "-"}</code></dd></div>
        <div><dt>lifecycle status</dt><dd><code>{detail.state}</code></dd></div>
        <div><dt>renewal status</dt><dd><code>{detail.renewalStatus || "-"}</code></dd></div>
        <div><dt>runtime status</dt><dd><code>{runtime?.status || "-"}</code></dd></div>
        <div><dt>Runtime ready</dt><dd><code>{runtime ? String(runtime.ready) : "-"}</code></dd></div>
        <div><dt>Runtime ID</dt><dd><code>{runtime?.runtimeId || "-"}</code></dd></div>
        <div><dt>Runtime URL</dt><dd>{runtime?.url ? <a href={runtime.url} rel="noreferrer" target="_blank"><code>{runtime.url}</code><ExternalLink aria-hidden size={14} /></a> : "-"}</dd></div>
        <div><dt>serviceName</dt><dd><code>{runtime?.serviceName || "-"}</code></dd></div>
        <div><dt>Workspace Key ID</dt><dd><code>{budget?.keyId || detail.workspaceApiKeyId || "-"}</code></dd></div>
        <div><dt>budget status</dt><dd><code>{budget?.status || "-"}</code></dd></div>
        <div><dt>quotaUsdMicros</dt><dd><code>{budget?.quotaUsdMicros || "-"}</code></dd></div>
        <div><dt>quotaUsedUsdMicros</dt><dd><code>{budget?.quotaUsedUsdMicros || "-"}</code></dd></div>
        <div><dt>runtime source reason</dt><dd><code>{runtimeSource?.available === false ? runtimeSource.reasonCode : controller.fabricRuntimeRead.runtime.error || "-"}</code></dd></div>
        <div><dt>budget source reason</dt><dd><code>{budgetSource?.available === false ? budgetSource.reasonCode : controller.sources.workspaceBudget.error || "-"}</code></dd></div>
        <div><dt>delete reason</dt><dd><code>{controller.workspaceDeleteIssue === "unavailable" ? "workspace_delete_unavailable" : controller.workspaceDeleteIssue || "-"}</code></dd></div>
        <div><dt>renewal issue</dt><dd><code>{controller.workspaceRenewalIssue || "-"}</code></dd></div>
      </dl>
      <div className="workspace-runtime-checks">
        <h3>Runtime checks</h3>
        {runtime?.checks.length ? <ul>{runtime.checks.map((check) => <li key={check.name}><code>{check.name}</code><span>{check.ok ? "true" : "false"}</span></li>)}</ul> : <p>暂无检查记录</p>}
      </div>
    </div>
  </details>;
}

function WorkspaceDetailPage({ controller }: { controller: ConsoleController }) {
  const workspaceRead = controller.customerWorkspaceRead;
  const runtimeRead = controller.fabricRuntimeRead;
  const workspaceSource = workspaceRead.detail.value;
  const runtime = sourceData(runtimeRead.runtime.value);
  if (workspaceRead.detail.loading && !workspaceSource) return <section className="workspace-detail-page"><div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div></section>;
  if (workspaceSource?.available === false) return <section className="workspace-detail-page">
    <Button onClick={() => controller.navigate("/console/workspaces")} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />工作空间列表</Button>
    <Alert color="warning" indicator={<AlertCircle size={18} />} title="工作空间详情暂不可用" description="暂时无法确认该工作空间，请稍后重试。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} />
    <section className="panel workspace-technical-panel"><details className="workspace-technical-details"><summary><span>技术详情</span><ChevronDown aria-hidden size={16} /></summary><div className="workspace-technical-details__body"><dl className="data-list"><div><dt>workspace source reason</dt><dd><code>{workspaceSource.reasonCode}</code></dd></div></dl></div></details></section>
  </section>;
  if (workspaceRead.detail.error && !workspaceSource) return <section className="workspace-detail-page"><Button onClick={() => controller.navigate("/console/workspaces")} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />工作空间列表</Button><Alert color="danger" title="工作空间详情暂不可用" description="暂时无法确认该工作空间，请稍后重试。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /><section className="panel workspace-technical-panel"><details className="workspace-technical-details"><summary><span>技术详情</span><ChevronDown aria-hidden size={16} /></summary><div className="workspace-technical-details__body"><dl className="data-list"><div><dt>workspace read error</dt><dd><code>{workspaceRead.detail.error}</code></dd></div></dl></div></details></section></section>;
  if (!workspaceSource) return <section className="workspace-detail-page"><div className="source-loading" aria-live="polite"><span className="spinner" />等待读取</div></section>;
  if (workspaceSource?.available && workspaceSource.data === null) return <section className="workspace-detail-page"><div className="empty-panel"><AlertCircle /><h2>工作空间不存在</h2><p>该工作空间不存在或当前账号无权访问。</p><Button onClick={() => controller.navigate("/console/workspaces")} variant="outline">返回列表</Button></div></section>;
  const detail = workspaceSource.data;
  const runtimePresentation = runtime ? presentWorkspaceRuntime(runtime) : null;
  const renewalPresentation = presentWorkspaceRenewal(detail.renewalStatus);
  const runtimeUnavailable = runtimeRead.runtime.value?.available === false;
  const runtimeLabel = runtimePresentation?.label || (runtimeUnavailable ? "入口暂不可用" : "正在确认");
  const runtimeDescription = runtimePresentation?.description || (runtimeUnavailable ? "暂时无法确认工作空间入口，请稍后刷新。" : "正在确认工作空间是否可用。");
  const runtimeUrl = runtimePresentation?.canOpen ? runtimePresentation.url : null;
  return (
    <section className="workspace-detail-page" data-slide="C-WS-05">
      <Button onClick={() => controller.navigate("/console/workspaces")} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />工作空间列表</Button>
      <div className="workspace-detail-content">
        <section className="panel workspace-identity-panel"><div className="workspace-heading"><div><h2>{detail.name || "未命名工作空间"}</h2><div className={`workspace-availability workspace-availability--${runtimePresentation?.kind || "pending"}`}><strong>{runtimeLabel}</strong><span>{runtimeDescription}</span></div></div><div className="workspace-entry-actions"><Button color="primary" disabled={!runtimeUrl} onClick={() => runtimeUrl && window.open(runtimeUrl, "_blank", "noopener,noreferrer")}>打开工作空间<ExternalLink aria-hidden size={16} /></Button><Button onClick={() => void controller.refreshCurrentPage()} variant="outline"><RefreshCw aria-hidden size={16} />刷新</Button></div></div><dl className="workspace-primary-facts"><div><dt>套餐</dt><dd>{detail.packageId?.toUpperCase() || "-"}</dd></div><div><dt>实际月费</dt><dd>{formatUsdMicros(detail.totalUsdMicros)}</dd></div><div><dt>权益截止</dt><dd>{formatDate(detail.paidThrough)}</dd></div></dl></section>
        <section className="panel workspace-access-panel"><div className="panel-title"><h2>访问凭据</h2><span>敏感信息将在 60 秒后自动隐藏</span></div>
          {runtime ? <WorkspaceAccessRows controller={controller.workspaceSecrets} runtime={runtime} /> : runtimeRead.runtime.loading && !runtimeRead.runtime.value ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div> : <Alert color="warning" indicator={<AlertCircle size={18} />} title="访问凭据暂不可用" description="暂时无法确认登录信息，请稍后刷新。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} />}
        </section>
        <section className="panel workspace-plan-panel"><div className="panel-title"><h2>续费与存储</h2></div>{controller.workspaceRenewalIssue === "unconfirmed" ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="续费结果待确认" description="工作空间的续费设置尚未获得确认，请稍后刷新。" /> : null}<dl className="data-list"><div><dt>{renewalPresentation.kind === "manual" ? "续费方式" : "续费状态"}</dt><dd>{renewalPresentation.label}</dd></div>{renewalPresentation.kind === "active" ? <div><dt>自动续费</dt><dd><Checkbox checked={detail.autoRenew === true} disabled={controller.workspaceRenewalBusy || controller.workspaceDeleteBusy} label={detail.autoRenew ? "已开启" : "已关闭"} onChange={() => void controller.updateCurrentWorkspaceRenewal(!detail.autoRenew)} /></dd></div> : null}<div><dt>持久存储</dt><dd>{detail.storageGb ? `${detail.storageGb} GB` : "-"}</dd></div></dl></section>
        <section className="panel workspace-settings-panel"><details className="workspace-advanced-details"><summary><span>高级设置</span><ChevronDown aria-hidden size={16} /></summary><div className="workspace-advanced-details__body"><WorkspaceBudgetPanel controller={controller} /><div className="workspace-delete-panel"><div className="workspace-settings-heading"><h3>删除工作空间</h3><p>删除后将无法继续访问该工作空间。</p></div>{controller.workspaceDeleteIssue === "unavailable" ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="工作空间删除暂不可用" description="当前无法执行删除，请稍后重试。" /> : null}{controller.workspaceDeleteIssue === "unconfirmed" ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="删除结果待确认" description="工作空间列表尚未确认删除结果。" /> : null}<Button busy={controller.workspaceDeleteBusy} color="danger" disabled={controller.workspaceRenewalBusy} onClick={() => void controller.deleteCurrentWorkspace()} variant="outline"><Trash2 aria-hidden size={16} />删除工作空间</Button></div></div></details></section>
        <section className="panel workspace-technical-panel"><WorkspaceTechnicalDetails controller={controller} detail={detail} runtime={runtime} /></section>
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

function UsageTokenFacts({ item }: { item: GatewayUsageItem }) {
  return <span className="usage-fact-stack usage-token-stack">
    <span><small>输入</small><strong>{formatCount(item.inputTokens)}</strong></span>
    <span><small>输出</small><strong>{formatCount(item.outputTokens)}</strong></span>
    {item.cacheReadTokens > 0 ? <span><small>缓存读取</small><strong>{formatCount(item.cacheReadTokens)}</strong></span> : null}
    {item.cacheCreationTokens > 0 ? <span><small>缓存写入</small><strong>{formatCount(item.cacheCreationTokens)}</strong></span> : null}
  </span>;
}

function UsageLatencyFacts({ item }: { item: GatewayUsageItem }) {
  return <span className="usage-fact-stack usage-latency-stack">
    <span><small>首字</small><strong>{formatLatency(item.firstTokenMs)}</strong></span>
    <span><small>总耗时</small><strong>{formatLatency(item.durationMs)}</strong></span>
  </span>;
}

function RequestRows({ items, onCopyRequestId }: { items: GatewayUsageItem[]; onCopyRequestId: (requestId: string) => void }) {
  return <>
    <div className="table-wrap request-table-desktop"><table className="gateway-usage-table"><thead><tr><th>模型 / API 地址</th><th>Token</th><th>费用</th><th>延迟</th><th>时间</th><th>请求 ID</th></tr></thead><tbody>{items.map((item) => <tr key={item.requestId}>
      <td><span className="usage-request-context"><strong>{item.model}</strong><code>{item.inboundEndpoint || "-"}</code></span></td>
      <td><UsageTokenFacts item={item} /></td>
      <td><strong className="usage-cost">{formatUsdMicros(item.actualCostUsdMicros)}</strong></td>
      <td><UsageLatencyFacts item={item} /></td>
      <td><time dateTime={item.createdAt}>{formatDate(item.createdAt, true)}</time></td>
      <td><span className="usage-request-id"><code>{item.requestId}</code><Button aria-label="复制请求 ID" onClick={() => onCopyRequestId(item.requestId)} size="sm" title="复制请求 ID" uniform variant="ghost"><Copy aria-hidden size={14} /></Button></span></td>
    </tr>)}</tbody></table></div>
    <div className="request-list-mobile" role="list">{items.map((item) => <article className="request-mobile-card" key={item.requestId} role="listitem">
      <header className="request-mobile-heading"><strong>{item.model}</strong><code>{item.inboundEndpoint || "-"}</code></header>
      <dl className="request-mobile-facts">
        <div><dt>Token</dt><dd><UsageTokenFacts item={item} /></dd></div>
        <div><dt>费用</dt><dd><strong className="usage-cost">{formatUsdMicros(item.actualCostUsdMicros)}</strong></dd></div>
        <div><dt>延迟</dt><dd><UsageLatencyFacts item={item} /></dd></div>
        <div><dt>时间</dt><dd><time dateTime={item.createdAt}>{formatDate(item.createdAt, true)}</time></dd></div>
      </dl>
      <footer className="request-mobile-id"><span>请求 ID</span><code>{item.requestId}</code><Button aria-label="复制请求 ID" onClick={() => onCopyRequestId(item.requestId)} size="sm" title="复制请求 ID" uniform variant="ghost"><Copy aria-hidden size={14} /></Button></footer>
    </article>)}</div>
  </>;
}

function UsagePage({ controller: usage, onCopyRequestId }: {
  controller: GatewayUsageController;
  onCopyRequestId: (requestId: string) => void;
}) {
  const keys = sourceData(usage.keys.value)?.items || [];
  return <section className="panel" data-slide="C-API-02"><div className="panel-title"><h2>用量记录</h2><span>请求级事实来自 API 服务</span></div><div className="gateway-usage-toolbar">
    <Select block label="API 密钥" onChange={(value) => void usage.selectKey(value)} options={keys.map((key) => ({ label: key.name, value: key.id }))} placeholder="选择 API 密钥" value={usage.selectedKeyId} />
    <SegmentedControl ariaLabel="统计周期" block onChange={(value) => void usage.selectPeriod(value as GatewayUsagePeriod)} options={[{ value: "today", label: "今日" }, { value: "week", label: "本周" }, { value: "month", label: "本月" }]} value={usage.period} />
  </div>
    <SourceState empty={usage.keys.value?.status === "empty"} emptyTitle="暂无 API 密钥" error={usage.keys.error} errorDescription="暂时无法读取 API 密钥，请稍后重试。" loading={usage.keys.loading} onRetry={() => void usage.refresh()} source={usage.keys.value} unavailableDescription="暂时无法读取 API 密钥，请稍后重试。" unavailableTitle="API 密钥暂不可用">{() => <>
      <SourceState error={usage.summary.error} errorDescription="暂时无法读取用量汇总，请稍后重试。" loading={usage.summary.loading} onRetry={() => void usage.refresh()} source={usage.summary.value} unavailableDescription="暂时无法读取用量汇总，请稍后重试。" unavailableTitle="用量汇总暂不可用">{(summary) => <dl className="usage-summary-strip"><div><dt>汇总请求次数</dt><dd>{formatCount(summary.totalRequests)}</dd></div><div><dt>汇总总 Token</dt><dd>{formatCount(summary.totalTokens)}</dd></div><div><dt>汇总实际金额</dt><dd>{formatUsdMicros(summary.totalActualCostUsdMicros)}</dd></div></dl>}</SourceState>
      <SourceState empty={usage.usage.value?.status === "empty"} emptyTitle="暂无请求记录" error={usage.usage.error} errorDescription="暂时无法读取用量记录，请稍后重试。" loading={usage.usage.loading} onRetry={() => void usage.refresh()} source={usage.usage.value} unavailableDescription="暂时无法读取用量记录，请稍后重试。" unavailableTitle="用量记录暂不可用">{(data) => <><RequestRows items={data.items} onCopyRequestId={onCopyRequestId} /><Pagination current={data.page} label="请求记录分页" onChange={(page) => void usage.changePage(page)} pages={data.pages} /></>}</SourceState>
    </>}</SourceState>
  </section>;
}

function ApiPage({ controller, route }: { controller: ConsoleController; route: CustomerApiRoute }) {
  let content: ReactNode;
  switch (route.kind) {
    case "customer.api.overview":
      content = <ApiOverview controller={controller} />;
      break;
    case "customer.api.usage":
      content = <UsagePage controller={controller.gatewayUsage} onCopyRequestId={(requestId) => void controller.copyText(requestId, "请求 ID 已复制")} />;
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
