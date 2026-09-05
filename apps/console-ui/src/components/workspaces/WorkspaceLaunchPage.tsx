import {
  AlertCircle, ArrowRight, ChevronLeft, CircleCheck, CircleX, RefreshCw
} from "lucide-react";
import { RadioGroup } from "@openai/apps-sdk-ui/components/RadioGroup";
import { useEffect, useRef, type ReactNode } from "react";

import type { WorkspaceLaunchController } from "../../app/console-controller-types.ts";
import {
  presentWorkspaceLaunch, presentWorkspaceLaunchStage, presentWorkspaceQuote
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
          <div><dt>续费</dt><dd>{operation?.autoRenew ? "自动续费开启" : "自动续费关闭"}</dd></div>
        </dl>
      )}
      {action ? <div className="workspace-order-summary__action">{action}</div> : null}
    </aside>
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
