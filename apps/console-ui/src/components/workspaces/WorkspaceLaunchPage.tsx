import {
  AlertCircle, ArrowRight, ChevronLeft, CircleCheck, CircleX, RefreshCw
} from "lucide-react";
import { RadioGroup } from "@openai/apps-sdk-ui/components/RadioGroup";
import { useEffect, useRef, type ReactNode } from "react";

import type { WorkspaceLaunchController } from "../../app/console-controller-types.ts";
import type { AgentWorkspaceLaunchController } from "../../app/use-workspace-launch-controller.ts";
import {
  presentWorkspaceApplicationInstallation, presentWorkspaceLaunch, presentWorkspaceLaunchStage, presentWorkspaceQuote
} from "../../app/workspace-experience-model.ts";
import type { PlanId, PricingPlan } from "../../api/dtos.ts";
import { Alert, Badge, Button, Checkbox, Field } from "../ui/index.ts";
import { formatDate, formatUsdMicros } from "../../console-model.ts";
import { billingUnitLabel } from "./workspace-shared.tsx";

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
          {!operation?.closeout || operation.closeout.status === "fulfilled" ? <div><dt>续费</dt><dd>{operation?.autoRenew ? "自动续费开启" : "自动续费关闭"}</dd></div> : null}
        </dl>
      )}
      {action ? <div className="workspace-order-summary__action">{action}</div> : null}
    </aside>
  );
}


function AgentLaunchPage({
  controller,
  onBack,
  onRefresh
}: {
  controller: AgentWorkspaceLaunchController;
  onBack: () => void;
  onRefresh: () => Promise<void>;
}) {
  const version = controller.agentCapabilityVersions.find((item) => item.id === controller.agentCapabilityVersionId);
  const compute = controller.agentComputePlans.find((item) => item.id === controller.agentComputePlanId);
  const storage = controller.agentStoragePlans.find((item) => item.id === controller.agentStoragePlanId);
  const quote = controller.agentQuote;
  const operation = controller.agentOperation;
  const operationDone = operation && ["succeeded", "failed", "needs_attention", "cancelled"].includes(operation.status);
  const canOpen = Boolean(controller.agentAccess?.url);
  const modelFor = (id: string) => controller.agentModels.find((model) => model.id === id)?.name || id;

  if (controller.agentRecoveryPending && !operation) {
    return <section className="workspace-launch-page" data-slide="C-WS-04">
      <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
      <Alert color="warning" title="正在核对原开通请求" description={controller.agentRecoveryActionable ? "上次请求可能已被接受。只能使用原报价和原请求继续核对，请勿重新开通。" : "原开通请求需由提交账户核对，请返回原账户；当前账户不能重复开通。"} actions={controller.agentRecoveryActionable ? <Button busy={controller.agentBusy} onClick={() => void controller.retryAgentLaunch()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />核对原请求</Button> : undefined} />
    </section>;
  }

  if (operation) {
    return (
      <section className="workspace-launch-page" data-slide="C-WS-04">
        <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
        <WorkspaceLaunchSteps current="operation" />
        <div className="workspace-launch-layout workspace-launch-layout--operation">
          <section className="launch-operation">
            <div className="launch-operation-head"><div><h2>{operation.status === "succeeded" && canOpen ? "Serve Agent 可访问，订单证据确认中" : operation.status === "succeeded" ? "Workspace 证据确认中" : operation.status === "needs_attention" ? "需要管理员处理" : operation.status === "failed" ? "Workspace 开通失败" : "正在开通 Workspace"}</h2><p>{operation.status === "succeeded" && canOpen ? "Serve confirmed access 已确认，可以打开 Agent WebUI；客户 Workspace 订单 receipt 仍需 owner 证据确认。" : operation.status === "succeeded" ? "操作已收到终态，但订单 receipt 与 Serve access 尚未同时确认；请刷新原操作，暂不重复提交。" : operation.status === "needs_attention" ? "操作结果需要管理员核实，请勿重复提交。" : operation.status === "failed" ? "开通未完成；页面不会把资源状态伪装成可用应用。" : "订单、资源和 Serve Agent 正在由各自 owner 继续处理。"}</p></div></div>
            <div className="launch-current-phase"><span>订单阶段</span><strong>{operation.stage}</strong><small>状态：{operation.status}</small></div>
            {controller.agentPollIssue === "unknown" ? <Alert color="warning" title="结果正在核实" description="暂时无法确认下一阶段，请勿重复提交；刷新后继续读取原操作。" /> : null}
            {controller.agentPollIssue === "timeout" ? <Alert color="warning" title="仍在处理中" description="owner 没有在当前轮次内完成读回，原操作仍被保留。" /> : null}
            {controller.agentWorkspace ? <div className="launch-diagnostic"><header><span>Workspace owner readback</span></header><dl className="operation-readback"><div><dt>Workspace</dt><dd>{controller.agentWorkspace.name}</dd></div><div><dt>资源</dt><dd>{controller.agentWorkspace.resourceReadiness}</dd></div><div><dt>Serve Agent</dt><dd>{controller.agentWorkspace.applicationAvailability}</dd></div><div><dt>订单 receipt</dt><dd>证据确认中</dd></div></dl></div> : null}
            <div className="launch-operation-actions">
              {canOpen ? <Button color="primary" onClick={controller.openAgentWorkspace}>打开 Agent WebUI</Button> : null}
              {operation.status === "succeeded" && canOpen ? <Button onClick={controller.startAnotherAgentLaunch} variant="outline">创建另一个 Workspace</Button> : null}
              <Button onClick={() => void onRefresh()} variant="outline"><RefreshCw aria-hidden size={16} />刷新状态</Button>
              {operationDone && !canOpen ? <span className="inline-error">Serve confirmed access 未确认，暂不开放 WebUI。</span> : null}
            </div>
          </section>
          <aside className="workspace-order-summary"><header><span>订单与资源</span><strong>{controller.agentWorkspace?.name || controller.launchName || "Workspace"}</strong></header><dl className="workspace-order-summary__facts"><div><dt>报价</dt><dd>{quote ? formatUsdMicros(quote.totalUSDMicros) : "原报价读回中"}</dd></div><div><dt>套餐</dt><dd>{compute?.name || "暂不可用"} / {storage?.name || "暂不可用"}</dd></div><div><dt>Agent 版本</dt><dd>{version?.versionLabel || "暂不可用"}</dd></div><div><dt>模型</dt><dd>{quote?.modelSelections.map((selection) => `${selection.slot}: ${modelFor(selection.modelId)}`).join("、") || "暂不可用"}</dd></div></dl></aside>
        </div>
      </section>
    );
  }

  return (
    <section className="workspace-launch-page" data-slide={controller.agentStep === "quote" ? "C-WS-03" : "C-WS-02"}>
      <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
      <WorkspaceLaunchSteps current={controller.agentStep === "quote" ? "confirm" : "configure"} />
      {controller.agentSourceLoading ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取 Agent、套餐、模型与 Gateway 钱包权威数据</div> : null}
      {controller.agentSourceError ? <Alert color="warning" title="开通入口暂不可用" description={`无法取得真实 owner 数据：${controller.agentSourceError}。未使用旧目录、缓存余额或模拟 Agent。`} actions={<Button onClick={() => void onRefresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /> : null}
      {controller.agentStep === "quote" && quote ? (
        <div className="workspace-launch-layout">
          <section className="workspace-launch-review"><header><h2>确认准确报价与部署条款</h2></header><dl className="launch-confirm-list"><div><dt>Agent 版本</dt><dd>{version?.versionLabel || version?.id}</dd></div><div><dt>计算套餐</dt><dd>{compute?.name}</dd></div><div><dt>存储套餐</dt><dd>{storage?.name}</dd></div><div><dt>模型</dt><dd>{quote.modelSelections.map((selection) => `${selection.slot}: ${modelFor(selection.modelId)}`).join("、")}</dd></div><div><dt>计费周期</dt><dd>{quote.periodStart} 至 {quote.periodEnd}</dd></div><div><dt>报价有效至</dt><dd>{quote.expiresAt}</dd></div></dl><p>{quote.refundTerms}</p><p>{quote.retentionTerms}</p>{controller.agentWallet && BigInt(controller.agentWallet.balanceUSDMicros) < BigInt(quote.totalUSDMicros) ? <Alert color="warning" title="余额不足" description="Gateway 权威余额不足本次报价，请联系管理员充值后重新报价。" /> : null}<div className="launch-confirm-check"><Checkbox checked={controller.agentConfirmed} label="我确认以上 Agent、套餐、模型、费用与数据政策，并同意创建同一 Workspace。" onChange={controller.setAgentConfirmed} /></div><footer><Button onClick={() => { controller.setLaunchStep("configure"); controller.setAgentConfirmed(false); }} variant="outline">返回修改</Button></footer></section><aside className="workspace-order-summary"><header><span>确定报价</span><strong>{formatUsdMicros(quote.totalUSDMicros)}</strong></header><dl className="workspace-order-summary__facts">{quote.lineItems.map((item) => <div key={`${item.kind}:${item.description}`}><dt>{item.description}</dt><dd>{formatUsdMicros(item.amountUSDMicros)}</dd></div>)}</dl><div className="workspace-order-summary__action"><Button busy={controller.agentBusy} color="primary" disabled={!controller.agentConfirmed || !controller.agentWallet || BigInt(controller.agentWallet.balanceUSDMicros) < BigInt(quote.totalUSDMicros)} onClick={() => void controller.submitAgentLaunch()}>确认并开通 Workspace</Button></div></aside></div>
      ) : (
        <div className="workspace-launch-layout"><section className="workspace-launch-config"><header><h2>新建 Agent Workspace</h2><p>选择已 ready 的 Agent CapabilityVersion、计算/存储套餐和模型；价格只由 Resource Catalog 报价。</p></header><Field label="工作空间名称" maxLength={256} onChange={(event) => controller.setLaunchName(event.currentTarget.value)} placeholder="例如：IBD 研究助手" required value={controller.launchName} />
          <fieldset><legend>Agent CapabilityVersion</legend><select aria-label="Agent CapabilityVersion" disabled={!controller.agentCapabilityVersions.length} onChange={(event) => controller.setAgentCapabilityVersionId(event.currentTarget.value)} value={controller.agentCapabilityVersionId}><option value="">请选择 ready 版本</option>{controller.agentCapabilityVersions.map((item) => <option key={item.id} value={item.id}>{item.versionLabel} · {item.provenance}</option>)}</select></fieldset>
          <fieldset><legend>计算套餐</legend><RadioGroup<string> aria-label="计算套餐" className="workspace-plan-list" direction="col" name="agent-compute-plan" onChange={controller.setAgentComputePlanId} value={controller.agentComputePlanId}>{controller.agentComputePlans.map((item) => <RadioGroup.Item block key={item.id} value={item.id}><strong>{item.name}</strong><small>{item.vcpus} vCPU / {item.memoryMiB} MiB</small></RadioGroup.Item>)}</RadioGroup></fieldset>
          <fieldset><legend>存储套餐</legend><select aria-label="存储套餐" onChange={(event) => controller.setAgentStoragePlanId(event.currentTarget.value)} value={controller.agentStoragePlanId}>{controller.agentStoragePlans.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.capacityGiB} GiB</option>)}</select></fieldset>
          {version?.modelRequirements.map((requirement) => <fieldset key={requirement.slot}><legend>{requirement.slot}{requirement.required ? "（必选）" : "（可选）"}</legend><select aria-label={requirement.slot} onChange={(event) => controller.setAgentModelSelection(requirement.slot, event.currentTarget.value)} value={controller.agentModelSelections[requirement.slot] || ""}><option value="">请选择模型</option>{controller.agentModels.filter((model) => requirement.allowedModelIds.includes(model.id)).map((model) => <option key={model.id} value={model.id}>{model.name}</option>)}</select></fieldset>)}
        </section><aside className="workspace-order-summary"><header><span>当前选择</span><strong>{version?.versionLabel || "未选择 Agent"}</strong></header><dl className="workspace-order-summary__facts"><div><dt>计算</dt><dd>{compute?.name || "未选择"}</dd></div><div><dt>存储</dt><dd>{storage?.name || "未选择"}</dd></div><div><dt>Gateway 钱包</dt><dd>{controller.agentWallet ? `${formatUsdMicros(controller.agentWallet.balanceUSDMicros)}（实时）` : "暂不可用"}</dd></div></dl><div className="workspace-order-summary__action"><Button busy={controller.agentBusy} color="primary" disabled={!controller.agentWallet || controller.agentSourceLoading || Boolean(controller.agentSourceError)} onClick={controller.reviewAgentLaunch}>获取准确报价</Button></div></aside></div>
      )}
    </section>
  );
}

export function WorkspaceLaunchPage({
  controller,
  onBack,
  onRefresh
}: {
  controller: WorkspaceLaunchController;
  onBack: () => void;
  onRefresh: () => Promise<void>;
}) {
  const agentController = controller as WorkspaceLaunchController & Partial<AgentWorkspaceLaunchController>;
  if (agentController.agentCapabilityVersions && agentController.reviewAgentLaunch) {
    return <AgentLaunchPage controller={agentController as AgentWorkspaceLaunchController} onBack={onBack} onRefresh={onRefresh} />;
  }
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
        {!controller.customerOwned ? <p>请在权益到期前自行从工作空间下载并妥善保存数据。到期后，平台不承担数据保管或恢复责任。</p> : null}
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

export function LaunchOperation({
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
  const installation = operation.applicationInstallation ? presentWorkspaceApplicationInstallation(operation.applicationInstallation) : null;
  const content = (
    <section className={`launch-operation ${compact ? "launch-operation--compact" : ""}`} data-slide="C-WS-04">
      <div className="launch-operation-head"><div><h2>{presentation.title}</h2><p>{presentation.summary}</p></div></div>
      {installation ? <Alert color={installation.tone} title={installation.title} description={installation.description} /> : null}
      {!operation.closeout ? <div className="launch-current-phase"><span>当前进度</span><strong>{stagePresentation.label}</strong></div> : null}
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
        {operation.closeout ? <Button onClick={controller.openLaunchBilling} variant="outline">查看费用</Button> : null}
        {!resultUnconfirmed && operation.closeout?.status === "closed" && ["failed", "refunded"].includes(operation.status) ? <Button onClick={() => { controller.prepareNewWorkspaceLaunch(); void onRefresh(); }}>重新购买</Button> : null}
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
