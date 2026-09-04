import { AlertCircle, ChevronDown, ChevronLeft, Copy, ExternalLink, Eye, EyeOff, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";

import type { WorkspaceSecretController } from "../../app/console-controller-types.ts";
import type { ConsoleController } from "../../app/use-console-controller.ts";
import {
  formatWorkspaceBudgetUsdInput, parseWorkspaceBudgetUsdInput,
  presentWorkspaceBudget, presentWorkspaceRenewal, presentWorkspaceRuntime
} from "../../app/workspace-experience-model.ts";
import type { WorkspaceDTO, WorkspaceGatewayBudgetDTO, WorkspaceGatewayBudgetUpdateRequest, WorkspaceRuntimeDTO } from "../../api/dtos.ts";
import { Alert, Button, Checkbox, Field } from "../ui/index.ts";
import { formatDate, formatUsdMicros } from "../../console-model.ts";
import { sourceData } from "./workspace-shared.tsx";

type WorkspaceDetailController = Pick<ConsoleController,
  | "customerWorkspaceRead"
  | "deleteCurrentWorkspace"
  | "fabricRuntimeRead"
  | "navigate"
  | "refreshCurrentPage"
  | "sources"
  | "updateCurrentWorkspaceRenewal"
  | "updateWorkspaceBudget"
  | "workspaceBudgetBusy"
  | "workspaceDeleteBusy"
  | "workspaceDeleteIssue"
  | "workspaceRenewalBusy"
  | "workspaceRenewalIssue"
  | "workspaceSecrets"
>;
type WorkspaceBudgetViewController = Pick<WorkspaceDetailController, "refreshCurrentPage" | "sources" | "updateWorkspaceBudget" | "workspaceBudgetBusy">;
type WorkspaceMaintenanceController = Pick<WorkspaceDetailController, "updateWorkspaceBudget" | "workspaceBudgetBusy">;
type WorkspaceTechnicalViewController = Pick<WorkspaceDetailController, "fabricRuntimeRead" | "sources" | "workspaceDeleteIssue" | "workspaceRenewalIssue">;

function SecretRow({ busy, label, purpose, onCopy, onHide, onReveal, revealed, value }: { busy: boolean; label: string; purpose: string; onCopy: () => void; onHide: () => void; onReveal: () => void; revealed: boolean; value?: string }) {
  return <div><dt><span>{label}</span><small>{purpose}</small></dt><dd className="credential-actions"><code>{revealed ? value || "-" : "••••••••••••"}</code>{revealed ? <><Button aria-label="隐藏" onClick={onHide} size="sm" uniform variant="ghost"><EyeOff aria-hidden size={16} /></Button><Button aria-label="复制" onClick={onCopy} size="sm" uniform variant="ghost"><Copy aria-hidden size={16} /></Button></> : <Button aria-label="显示" busy={busy} onClick={onReveal} size="sm" variant="outline"><Eye aria-hidden size={16} />显示</Button>}</dd></div>;
}

type WorkspaceBudgetLimitField = "quotaUsdMicros" | "rateLimit5hUsdMicros" | "rateLimit1dUsdMicros" | "rateLimit7dUsdMicros";
type WorkspaceBudgetForm = Record<WorkspaceBudgetLimitField, string> & { enabled: boolean };

const workspaceBudgetLimitFields: ReadonlyArray<{ field: WorkspaceBudgetLimitField; label: string }> = [
  { field: "quotaUsdMicros", label: "总额度（美元）" },
  { field: "rateLimit5hUsdMicros", label: "5 小时限额（美元）" },
  { field: "rateLimit1dUsdMicros", label: "1 天限额（美元）" },
  { field: "rateLimit7dUsdMicros", label: "7 天限额（美元）" }
];

function workspaceBudgetForm(budget: WorkspaceGatewayBudgetDTO | null): WorkspaceBudgetForm {
  return {
    quotaUsdMicros: formatWorkspaceBudgetUsdInput(budget?.quotaUsdMicros),
    rateLimit5hUsdMicros: formatWorkspaceBudgetUsdInput(budget?.rateLimit5hUsdMicros),
    rateLimit1dUsdMicros: formatWorkspaceBudgetUsdInput(budget?.rateLimit1dUsdMicros),
    rateLimit7dUsdMicros: formatWorkspaceBudgetUsdInput(budget?.rateLimit7dUsdMicros),
    enabled: budget?.enabled ?? false
  };
}

function WorkspaceBudgetPanel({ controller }: { controller: WorkspaceBudgetViewController }) {
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
      const value = parseWorkspaceBudgetUsdInput(form[field]);
      if (value === null) nextErrors[field] = "请输入非负美元金额，最多 6 位小数";
      else input[field] = value;
    }
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) return;
    void controller.updateWorkspaceBudget(input);
  };

  return <div className="workspace-budget-panel">
    <div className="workspace-settings-heading"><h3>预算设置</h3><p>设置 API 密钥的消费上限。</p></div>
    {budget ? <div className="workspace-details key-form">
        <div className="key-form-grid">
          {workspaceBudgetLimitFields.map(({ field, label }) => <Field description="0 表示不限额，最多 6 位小数" disabled={controller.workspaceBudgetBusy} error={errors[field]} inputMode="decimal" key={field} label={label} max="9007199254.740991" min="0" onChange={(event) => updateLimit(field, event.currentTarget.value)} required step="0.000001" type="number" value={form[field]} />)}
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
        </div>
      </div> : controller.sources.workspaceBudget.loading && !controller.sources.workspaceBudget.value ? <div className="source-loading" aria-live="polite"><span className="spinner" />正在读取</div> : controller.sources.workspaceBudget.value?.available === false ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="模型预算暂不可用" description="暂时无法确认预算设置，请稍后刷新。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /> : controller.sources.workspaceBudget.error ? <Alert color="danger" title="模型预算暂不可用" description="暂时无法确认预算设置，请稍后刷新。" actions={<Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />重试</Button>} /> : <div className="source-loading" aria-live="polite"><span className="spinner" />等待读取</div>}
  </div>;
}

function WorkspaceMaintenancePanel({ controller }: { controller: WorkspaceMaintenanceController }) {
  return <section className="workspace-maintenance-panel" aria-labelledby="workspace-maintenance-heading">
    <div className="workspace-settings-heading"><h3 id="workspace-maintenance-heading">用量维护</h3><p>仅在需要重新开始统计时重置用量。</p></div>
    <div className="workspace-actions">
      <Button busy={controller.workspaceBudgetBusy} onClick={() => void controller.updateWorkspaceBudget({ resetQuota: true })} variant="outline">重置总额度用量</Button>
      <Button busy={controller.workspaceBudgetBusy} onClick={() => void controller.updateWorkspaceBudget({ resetRateLimitUsage: true })} variant="outline">重置滚动窗口用量</Button>
    </div>
  </section>;
}

function WorkspaceAccessRows({ controller, runtime }: {
  controller: WorkspaceSecretController;
  runtime: WorkspaceRuntimeDTO;
}) {
  return <dl className="data-list"><div><dt><span>登录账号</span><small>用于登录工作空间</small></dt><dd>{runtime.access?.username || controller.credential?.username || "-"}</dd></div>
    <SecretRow busy={controller.workspaceBusy} label="登录密码" purpose="用于登录工作空间" onCopy={() => void controller.copyWorkspacePassword()} onHide={controller.clear} onReveal={() => void controller.revealWorkspacePassword()} revealed={Boolean(controller.credential)} value={controller.credential?.password} />
    <SecretRow busy={controller.gatewayKeyBusy} label="API 密钥" purpose="用于 Gateway API 调用" onCopy={() => void controller.copyWorkspaceKey()} onHide={controller.clear} onReveal={() => void controller.revealWorkspaceKey()} revealed={Boolean(controller.gatewayKey)} value={controller.gatewayKey?.value} />
    <div><dt>密码管理</dt><dd className="workspace-actions"><Button busy={controller.workspaceBusy} onClick={() => void controller.rotateWorkspacePassword()} variant="outline">轮换密码</Button></dd></div>
  </dl>;
}

function WorkspaceTechnicalDetails({ controller, detail, runtime }: {
  controller: WorkspaceTechnicalViewController;
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

export function WorkspaceDetailPage({ controller }: { controller: WorkspaceDetailController }) {
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
        <section className="panel workspace-settings-panel"><details className="workspace-advanced-details"><summary><span>预算与用量</span><ChevronDown aria-hidden size={16} /></summary><div className="workspace-advanced-details__body"><WorkspaceBudgetPanel controller={controller} /><WorkspaceMaintenancePanel controller={controller} /></div></details></section>
        <section className="panel workspace-delete-panel"><div className="workspace-settings-heading"><h3>删除工作空间</h3><p>删除后将无法继续访问该工作空间。</p></div>{controller.workspaceDeleteIssue === "unavailable" ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="工作空间删除暂不可用" description="当前无法执行删除，请稍后重试。" /> : null}{controller.workspaceDeleteIssue === "unconfirmed" ? <Alert color="warning" indicator={<AlertCircle size={18} />} title="删除结果待确认" description="工作空间列表尚未确认删除结果。" /> : null}<Button busy={controller.workspaceDeleteBusy} color="danger" disabled={controller.workspaceRenewalBusy} onClick={() => void controller.deleteCurrentWorkspace()} variant="outline"><Trash2 aria-hidden size={16} />删除工作空间</Button></section>
        <section className="panel workspace-technical-panel"><WorkspaceTechnicalDetails controller={controller} detail={detail} runtime={runtime} /></section>
      </div>
    </section>
  );
}
