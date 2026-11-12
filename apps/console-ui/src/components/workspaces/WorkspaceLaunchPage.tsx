import {
  AlertCircle, ChevronLeft, CircleCheck, CircleX, RefreshCw
} from "lucide-react";
import { RadioGroup } from "@openai/apps-sdk-ui/components/RadioGroup";
import type { ReactNode } from "react";

import type { WorkspaceLaunchController } from "../../app/console-controller-types.ts";
import { cloudIdentity } from "../../app/console-identity.ts";
import { isAgentLaunchSurface } from "../../app/workspace-launch-controller-model.ts";
import type { AgentWorkspaceLaunchController } from "../../app/use-workspace-launch-controller.ts";
import {
  presentWorkspaceApplicationInstallation, presentWorkspaceLaunch, presentWorkspaceLaunchStage, presentWorkspaceQuote
} from "../../app/workspace-experience-model.ts";
import { Alert, Button, Checkbox, Field } from "../ui/index.ts";
import { formatDate, formatUsdMicros } from "../../console-model.ts";

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
  const release = controller.agentRuntimeVersions.find((item) => item.id === controller.agentRuntimeVersionId);
  const isDefaultApp = controller.agentApplicationKind === "opl_app";
  const compute = controller.agentComputePlans.find((item) => item.id === controller.agentComputePlanId);
  const storage = controller.agentStoragePlans.find((item) => item.id === controller.agentStoragePlanId);
  const offer = compute?.workspaceOffers?.find((item) => item.storagePlanId === controller.agentStoragePlanId);
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
            <div className="launch-operation-head"><div><h2>{operation.status === "succeeded" && canOpen ? "应用已可访问，订单确认中" : operation.status === "succeeded" ? "正在确认订单" : operation.status === "needs_attention" ? "需要管理员处理" : operation.status === "failed" ? "工作空间开通失败" : "正在开通工作空间"}</h2><p>{operation.status === "succeeded" && canOpen ? "现在可以打开应用。订单确认仍在进行中，请保留当前页面。" : operation.status === "succeeded" ? "正在核对订单和应用访问状态，请刷新查看进度，暂勿重复开通。" : operation.status === "needs_attention" ? "操作结果需要管理员核实，请勿重复提交。" : operation.status === "failed" ? "开通未完成，请联系管理员查看原因。" : "正在准备资源并部署应用。"}</p></div></div>
            <details className="launch-diagnostic"><summary>技术详情</summary><dl className="operation-readback"><div><dt>操作编号</dt><dd>{operation.operationId}</dd></div><div><dt>阶段</dt><dd>{operation.stage}</dd></div><div><dt>状态</dt><dd>{operation.status}</dd></div></dl></details>
            {controller.agentPollIssue === "unknown" ? <Alert color="warning" title="结果正在核实" description="暂时无法确认下一阶段，请勿重复提交；刷新后继续读取原操作。" /> : null}
            {controller.agentPollIssue === "timeout" ? <Alert color="warning" title="仍在处理中" description="处理时间较长，请稍后刷新查看原订单。" /> : null}
            {controller.agentWorkspace ? <details className="launch-diagnostic"><summary>工作空间详情</summary><dl className="operation-readback"><div><dt>工作空间</dt><dd>{controller.agentWorkspace.name}</dd></div><div><dt>资源</dt><dd>{controller.agentWorkspace.resourceReadiness}</dd></div><div><dt>应用</dt><dd>{controller.agentWorkspace.applicationAvailability}</dd></div><div><dt>订单确认</dt><dd>证据确认中</dd></div></dl></details> : null}
            <div className="launch-operation-actions">
              {canOpen ? <Button color="primary" onClick={controller.openAgentWorkspace}>打开应用</Button> : null}
              {operation.status === "succeeded" && canOpen ? <Button onClick={controller.startAnotherAgentLaunch} variant="outline">创建另一个工作空间</Button> : null}
              <Button onClick={() => void onRefresh()} variant="outline"><RefreshCw aria-hidden size={16} />刷新状态</Button>
              {operationDone && !canOpen ? <span className="inline-error">应用访问尚未确认，请稍后刷新。</span> : null}
            </div>
          </section>
          <aside className="workspace-order-summary"><header><span>订单与资源</span><strong>{controller.agentWorkspace?.name || controller.launchName || "工作空间"}</strong></header><dl className="workspace-order-summary__facts"><div><dt>报价</dt><dd>{quote ? formatUsdMicros(quote.totalUSDMicros) : "原报价读回中"}</dd></div><div><dt>套餐</dt><dd>{compute?.name || "暂不可用"} / {storage?.name || "暂不可用"}</dd></div><div><dt>应用来源</dt><dd>{isDefaultApp ? `默认 App · ${release?.versionLabel || release?.name || "暂不可用"}` : `Agent · ${version?.versionLabel || "暂不可用"}`}</dd></div><div><dt>模型</dt><dd>{isDefaultApp ? "应用内置配置" : (quote?.modelSelections.map((selection) => `${selection.slot}: ${modelFor(selection.modelId)}`).join("、") || "暂不可用")}</dd></div></dl></aside>
        </div>
      </section>
    );
  }

  return (
    <section className="workspace-launch-page workspace-launch-page--native" data-slide={controller.agentStep === "quote" ? "C-WS-03" : "C-WS-02"}>
      <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
      <WorkspaceLaunchSteps current={controller.agentStep === "quote" ? "confirm" : "configure"} />
      {controller.agentSourceLoading ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在加载应用与套餐</div> : null}
      {controller.agentSourceError ? <>
        <Alert color="warning" title="开通入口暂不可用" description="无法读取套餐，请稍后重试。" actions={<Button onClick={() => void onRefresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} />
        <details className="launch-diagnostic"><summary>技术详情</summary><p>{controller.agentSourceError}</p></details>
      </> : null}
      {controller.agentStep === "quote" && quote ? (
        <div className="workspace-launch-layout">
          <section className="workspace-launch-review">
            <header><h2>确认订单</h2></header>
            <dl className="launch-confirm-list">
              <div><dt>{isDefaultApp ? "应用版本" : "智能体版本"}</dt><dd>{isDefaultApp ? (release?.versionLabel || release?.name || release?.id) : (version?.versionLabel || version?.id)}</dd></div>
              <div><dt>工作空间套餐</dt><dd>{compute?.name}</dd></div>
              <div><dt>包含存储</dt><dd>{offer ? `${offer.storageCapacityGiB} GiB` : "待核对"}</dd></div>
              <div><dt>模型</dt><dd>{isDefaultApp ? "应用内置配置" : quote.modelSelections.map((selection) => `${selection.slot}: ${modelFor(selection.modelId)}`).join("、") || "无需配置"}</dd></div>
              <div><dt>计费周期</dt><dd>{formatDate(quote.periodStart)} 至 {formatDate(quote.periodEnd)}</dd></div>
              <div><dt>报价有效至</dt><dd>{formatDate(quote.expiresAt, true)}</dd></div>
            </dl>
            <div className="workspace-launch-terms"><p>{quote.refundTerms}</p><p>{quote.retentionTerms}</p>{quote.expectedInterruption ? <p>{quote.expectedInterruption}</p> : null}</div>
            {controller.agentWallet && BigInt(controller.agentWallet.balanceUSDMicros) < BigInt(quote.totalUSDMicros) ? <Alert color="warning" title="余额不足" description="钱包余额不足，请充值后重新报价。" /> : null}
            <div className="launch-confirm-check"><Checkbox checked={controller.agentConfirmed} label="我确认以上配置、费用与数据政策，同意开通工作空间。" onChange={controller.setAgentConfirmed} /></div>
            <footer><Button onClick={() => { controller.setLaunchStep("configure"); controller.setAgentConfirmed(false); }} variant="outline">返回修改</Button></footer>
          </section>
          <aside className="workspace-order-summary">
            <header><span>本次应付</span><strong>{formatUsdMicros(quote.totalUSDMicros)}</strong></header>
            <dl className="workspace-order-summary__facts">{quote.lineItems.map((item) => <div key={`${item.kind}:${item.description}`}><dt>{item.description}</dt><dd>{formatUsdMicros(item.amountUSDMicros)}</dd></div>)}</dl>
            <div className="workspace-order-summary__action"><Button busy={controller.agentBusy} color="primary" disabled={!controller.agentConfirmed || !controller.agentWallet || BigInt(controller.agentWallet.balanceUSDMicros) < BigInt(quote.totalUSDMicros)} onClick={() => void controller.submitAgentLaunch()}>确认并开通工作空间</Button></div>
          </aside>
        </div>
      ) : (
        <div className="workspace-launch-layout">
          <section className="workspace-launch-config">
            <header><h2>新建工作空间</h2></header>
            <Field label="工作空间名称" maxLength={256} onChange={(event) => controller.setLaunchName(event.currentTarget.value)} placeholder="例如：研究助手" required value={controller.launchName} />
            <fieldset><legend>应用</legend>
              <RadioGroup<"opl_app" | "agent"> aria-label="应用" className="workspace-application-options" direction="row" name="agent-application-kind" onChange={controller.setAgentApplicationKind} value={controller.agentApplicationKind}>
                <RadioGroup.Item block value="opl_app">OPL App</RadioGroup.Item>
                <RadioGroup.Item block value="agent">已构建智能体</RadioGroup.Item>
              </RadioGroup>
            </fieldset>
            {isDefaultApp ? controller.agentReadLoading.app ? <p className="workspace-launch-note" role="status">正在读取应用…</p> : controller.agentReadErrors.app ? <Alert color="warning" title="应用目录暂不可用" description="请刷新后重试。" /> : controller.agentRuntimeVersions.length > 1 ? <details className="launch-diagnostic"><summary>应用版本 · {release?.versionLabel || release?.name}</summary>
              <select aria-label="应用版本" onChange={(event) => controller.setAgentRuntimeVersionId(event.currentTarget.value)} value={controller.agentRuntimeVersionId}>
                {controller.agentRuntimeVersions.map((item) => <option key={item.id} value={item.id}>{item.versionLabel || item.name}</option>)}
              </select>
            </details> : !controller.agentSourceLoading && !release ? <p className="workspace-launch-note">暂无可用应用，请联系管理员。</p> : null : <fieldset><legend>智能体版本</legend>
              {controller.agentReadLoading.agent ? <p className="workspace-launch-note" role="status">正在读取智能体…</p> : controller.agentReadErrors.agent ? <Alert color="warning" title="智能体目录暂不可用" description="请刷新后重试。" /> : <>
                <select aria-label="智能体版本" disabled={!controller.agentCapabilityVersions.length} onChange={(event) => controller.setAgentCapabilityVersionId(event.currentTarget.value)} value={controller.agentCapabilityVersionId}>
                  <option value="">请选择智能体</option>{controller.agentCapabilityVersions.map((item) => <option key={item.id} value={item.id}>{item.versionLabel}</option>)}
                </select>
                {!controller.agentReadLoading.agent && !controller.agentCapabilityVersions.length ? <p className="workspace-launch-note">暂无可用智能体。<a href="/console/publisher">前往构建</a></p> : null}
              </>}
            </fieldset>}
            <fieldset><legend>工作空间套餐</legend>
              <RadioGroup<string> aria-label="工作空间套餐" className="workspace-plan-list workspace-native-plans" direction="col" name="agent-workspace-plan" onChange={(key) => {
                const choice = controller.agentComputePlans.flatMap((plan) => (plan.workspaceOffers ?? []).map((item) => ({ plan, item }))).find(({ item }) => item.pricePolicyVersionId === key);
                if (choice) controller.setAgentWorkspaceOffer(choice.plan.id, choice.item.storagePlanId);
              }} value={offer?.pricePolicyVersionId || ""}>
                {controller.agentComputePlans.flatMap((item) => item.workspaceOffers?.length ? item.workspaceOffers.map((included) => <RadioGroup.Item block key={included.pricePolicyVersionId} value={included.pricePolicyVersionId}>
                  <span className="workspace-native-plan"><span><strong>{item.name}</strong><small>{item.vcpus} 核 · {item.memoryMiB / 1024} GiB · 含 {included.storageCapacityGiB} GiB 存储</small></span><span className="workspace-native-plan__price">{formatUsdMicros(included.monthlyUSDMicros)}<small> / 月</small></span></span>
                </RadioGroup.Item>) : [<RadioGroup.Item block disabled key={item.id} value={`unpriced:${item.id}`}>
                  <span className="workspace-native-plan"><span><strong>{item.name}</strong><small>{item.vcpus} 核 · {item.memoryMiB / 1024} GiB</small></span><span className="workspace-native-plan__price"><small>套餐待配置</small></span></span>
                </RadioGroup.Item>])}
              </RadioGroup>
              {!controller.agentSourceLoading && !controller.agentComputePlans.length && !controller.agentSourceError ? <p className="workspace-launch-note">暂无可用套餐，请联系管理员。</p> : null}
            </fieldset>
            {!isDefaultApp && controller.agentReadErrors.models && version?.modelRequirements.length ? <Alert color="warning" title="模型目录暂不可用" description="请刷新后重试。" /> : null}
            {!isDefaultApp && version?.modelRequirements.map((requirement) => <fieldset key={requirement.slot}><legend>{requirement.slot}{requirement.required ? "（必选）" : "（可选）"}</legend><select aria-label={requirement.slot} onChange={(event) => controller.setAgentModelSelection(requirement.slot, event.currentTarget.value)} value={controller.agentModelSelections[requirement.slot] || ""}><option value="">请选择模型</option>{controller.agentModels.filter((model) => requirement.allowedModelIds.includes(model.id)).map((model) => <option key={model.id} value={model.id}>{model.name}</option>)}</select></fieldset>)}
          </section>
          <aside className="workspace-order-summary">
            <header><strong>{isDefaultApp ? (release?.name || "OPL App") : (version?.versionLabel || "未选择智能体")}</strong>{isDefaultApp && release?.versionLabel ? <span>版本 {release.versionLabel}</span> : null}</header>
            <dl className="workspace-order-summary__facts"><div><dt>套餐</dt><dd>{compute?.name || "未选择"}</dd></div><div><dt>包含存储</dt><dd>{offer ? `${offer.storageCapacityGiB} GiB` : "未选择"}</dd></div><div><dt>每月合计</dt><dd>{offer ? formatUsdMicros(offer.monthlyUSDMicros) : "未选择"}</dd></div><div><dt>钱包余额</dt><dd>{controller.agentWallet ? formatUsdMicros(controller.agentWallet.balanceUSDMicros) : controller.agentReadLoading.wallet ? "读取中" : "暂不可用"}</dd></div></dl>{controller.agentReadErrors.wallet ? <p className="workspace-launch-note" role="status">钱包暂不可用，请刷新后重试。</p> : null}
            <div className="workspace-order-summary__action"><Button busy={controller.agentBusy} color="primary" disabled={!controller.agentCanQuote} onClick={controller.reviewAgentLaunch}>查看报价</Button></div>
          </aside>
        </div>
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
  if (isAgentLaunchSurface({ ...agentController, cloudIdentity })) {
    return <AgentLaunchPage controller={agentController as AgentWorkspaceLaunchController} onBack={onBack} onRefresh={onRefresh} />;
  }
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
    <section className="workspace-launch-page" data-slide="C-WS-02" data-testid="workspace-launch-retired">
      <Button className="workspace-launch-back" onClick={onBack} size="sm" variant="ghost"><ChevronLeft aria-hidden size={16} />返回工作空间列表</Button>
      <Alert
        color="warning"
        indicator={<AlertCircle size={18} />}
        title="该购买入口已停用"
        description="请使用新版 Cloud 控制台购买工作空间。历史订单仍由原服务保留，可通过历史工作空间和运维页面查看与核对。"
      />
    </section>
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
