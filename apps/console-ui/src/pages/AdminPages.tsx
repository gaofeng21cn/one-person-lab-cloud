import {
  Activity,
  Ban,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Database,
  ExternalLink,
  Plus,
  RefreshCw,
  Search,
  Server,
  ShieldAlert,
  ShieldCheck,
  ShieldOff,
  WalletCards,
  X
} from "lucide-react";
import { useEffect, useRef, useState, type FormEvent, type ReactNode, type RefObject } from "react";

import type { OperatorAnnouncementController, OperatorResourceReadController } from "../app/console-controller-types.ts";
import { isOperatorBootstrapAdmin, isOperatorPlatformAdmin } from "../app/operator-account-controller-model.ts";
import type { AdminConsoleRoute } from "../app/console-router.ts";
import type { ConsoleController } from "../app/use-console-controller.ts";
import { cloudIdentity } from "../app/console-identity.ts";
import { recoverWorkspaceLaunch, getWorkspaceLaunchRecovery } from "../api/console-read-api.ts";
import {
  presentWorkspaceApplicationAvailability,
  presentWorkspaceLaunchCloseout,
  presentWorkspaceLifecycle,
  presentWorkspaceResourceReadiness
} from "../app/workspace-experience-model.ts";
import type {
  AnnouncementDTO,
  AnnouncementDraftRequest,
  OperatorAccountDTO,
  OperatorHealthDTO,
  OperatorFabricHealthDTO,
  OperatorRuntimeHealthDTO,
  OperatorReconciliationItemDTO,
  OperatorResourceDTO,
  OperatorWorkspaceDTO,
  ReadinessFact,
  SourceEnvelope,
  WalletAdjustmentRequest,
  WorkspaceDTO,
  WorkspaceLaunchRecoveryDTO,
  WorkspaceLaunchRecoveryRequest
} from "../api/dtos.ts";
import { SourceState } from "../components/source/SourceState.tsx";
import { Badge, Button, Field, Modal, SegmentedControl } from "../components/ui/index.ts";

import { formatCount, formatDate, formatUsdMicros } from "../console-model.ts";
import { AdminRuntimeManager } from "./AdminRuntimeManager.tsx";
import { OperatorRuntimeObservations } from "./OperatorRuntimeObservations.tsx";
import { observationLabel, observationReason, workspaceImageStatusMessage } from "./operator-observation-presentation.ts";

type BadgeTone = "danger" | "info" | "secondary" | "success" | "warning";

function assertNever(value: never): never {
  throw new Error(`Unhandled admin Console route: ${JSON.stringify(value)}`);
}

function sourceData<T>(source: SourceEnvelope<T> | null | undefined): T | null {
  return source?.available ? source.data : null;
}

function sourceStatusLabel(status?: string) {
  return ({ available: "可用", empty: "暂无数据", unavailable: "暂不可用" } as Record<string, string>)[status || ""] || "暂不可用";
}

function sourceTone(status?: string): BadgeTone {
  if (status === "available") return "success";
  if (status === "empty") return "secondary";
  return "warning";
}

function statusLabel(status?: string) {
  return ({
    active: "正常",
    disabled: "已停用",
    draft: "草稿",
    failed: "失败",
    manual_review: "待人工确认",
    processing: "处理中",
    published: "已发布",
    queued: "等待处理",
    running: "运行中",
    suspended: "已暂停",
    data_deleted: "数据已删除",
    provisioning: "开通中",
    expired_unpaid: "到期未续费",
    deleting: "删除中",
    scheduled: "已排期",
    started: "已提交",
    succeeded: "已完成",
    withdrawn: "已撤下"
  } as Record<string, string>)[status || ""] || (status || "暂不可用");
}

function statusTone(status?: string): BadgeTone {
  if (["active", "published", "succeeded", "ready", "running"].includes(status || "")) return "success";
  if (["disabled", "failed"].includes(status || "")) return "danger";
  if (["manual_review", "scheduled", "processing", "queued"].includes(status || "")) return "warning";
  return "secondary";
}

function resourceKindLabel(kind?: string) {
  return ({ compute: "计算资源", storage: "存储", attachment: "挂载", runtime: "运行环境", execution: "运行资源" } as Record<string, string>)[kind || ""] || kind || "资源类型暂不可用";
}

function resourcesUnavailable(item: OperatorWorkspaceDTO): boolean {
  return item.resourcesReadback?.available === false
    || item.workspace.source === "workspace" && !item.resourcesReadback;
}

function WorkspaceOwnerFacts({ item }: { item: OperatorWorkspaceDTO }) {
  const facts = [
    { label: "业务状态", read: (workspace: WorkspaceDTO) => presentWorkspaceLifecycle(workspace.state).label },
    { label: "资源就绪", read: (workspace: WorkspaceDTO) => presentWorkspaceResourceReadiness(workspace.resourceReadiness) },
    { label: "应用可用", read: (workspace: WorkspaceDTO) => presentWorkspaceApplicationAvailability(workspace.applicationAvailability) }
  ];
  return <>{facts.map(({ label, read }) => <div key={label}>
    <dt>{label}</dt><dd><SourceOwner source={item.workspace} /><SourceValue source={item.workspace}>{read}</SourceValue></dd>
  </div>)}</>;
}

function EmptyResourceFacts({ item }: { item: OperatorWorkspaceDTO }) {
  const readback = item.resourcesReadback;
  if (readback?.available) {
    if (readback.data.absenceConfirmed) return null;
    return <span>{readback.data.outcome === "unknown" ? "资源事实尚未核验" : "Fabric 未返回资源事实"}</span>;
  }
  return <span>暂无资源观测</span>;
}

function OwnershipSources({ item }: { item: OperatorWorkspaceDTO }) {
  const cloudOwned = item.workspace.source === "workspace";
  if (!cloudOwned) {
    return <span className="ops-owner"><SourceValue source={item.ownerAccount}>{(data) => data.id}</SourceValue><small><SourceValue source={item.ownerUser}>{(data) => data.email}</SourceValue></small></span>;
  }
  return <span className="ops-owner">
    <span><strong>Tenant</strong> <SourceOwner source={item.tenant} /><SourceValue source={item.tenant}>{(data) => data.id}</SourceValue></span>
    <small><strong>创建者</strong> <SourceOwner source={item.createdBy} /><SourceValue source={item.createdBy}>{(data) => data.id}</SourceValue></small>
  </span>;
}

function SourceBadge({ source }: { source: SourceEnvelope<unknown> | null | undefined }) {
  const tone = sourceTone(source?.status);
  const dot = tone === "success" ? "ss-dot--ok" : tone === "danger" ? "ss-dot--bad" : tone === "warning" ? "ss-dot--warn" : "ss-dot--muted";
  return <Badge color={tone}><span aria-hidden="true" className={`ss-dot ${dot}`} />{sourceStatusLabel(source?.status)}</Badge>;
}

function SourceValue<T>({ source, children, detail = false }: { source: SourceEnvelope<T> | null | undefined; children: (data: T) => ReactNode; detail?: boolean }) {
  if (!source?.available) return <span className="source-value source-value--unavailable">暂不可用{source?.status === "unavailable" ? <small>{observationReason(source.reasonCode)}</small> : null}</span>;
  if (!detail) return <span className="source-value"><strong>{children(source.data)}</strong></span>;
  return (
    <span className="source-value">
      <strong>{children(source.data)}</strong>
      <small>{source.source} · {sourceStatusLabel(source.status)}{source.fetchedAt ? ` · ${formatDate(source.fetchedAt, true)}` : ""}</small>
    </span>
  );
}

function Pagination({ current, label, onChange, pages }: { current: number; label: string; onChange: (page: number) => void; pages: number }) {
  if (pages <= 1) return null;
  return (
    <nav aria-label={label} className="pagination">
      <Button disabled={current <= 1} onClick={() => onChange(current - 1)} size="sm" variant="outline"><ChevronLeft aria-hidden size={16} />上一页</Button>
      <span>第 {current} / {pages} 页</span>
      <Button disabled={current >= pages} onClick={() => onChange(current + 1)} size="sm" variant="outline">下一页<ChevronRight aria-hidden size={16} /></Button>
    </nav>
  );
}

function Metric({ label, note, value }: { label: string; note: string; value: string }) {
  return <article className="band-metric"><span>{label}</span><strong>{value}</strong><small>{note}</small></article>;
}

function healthStatus(source: SourceEnvelope<ReadinessFact> | null | undefined, serviceKey?: string) {
  if (!source?.available || source.status === "empty") return { label: "暂不可用", tone: "warning" as BadgeTone };
  const ready = serviceKey === "fabric" ? (source.data as OperatorFabricHealthDTO).serviceReady : source.data.ready;
  if (ready === true) return { label: "正常", tone: "success" as BadgeTone };
  if (ready === false) return { label: "需处理", tone: "danger" as BadgeTone };
  return { label: "暂不可用", tone: "warning" as BadgeTone };
}

function overallHealth(source: SourceEnvelope<OperatorHealthDTO> | null | undefined) {
  if (!source?.available || source.status === "empty") return { label: "暂不可用", tone: "warning" as BadgeTone };
  for (const key of ["controlPlane", "gateway", "fabric", "runtime", "ledger"] as const) {
    const status = healthStatus(source.data[key], key);
    if (status.label !== "正常") return status;
  }
  return { label: "正常", tone: "success" as BadgeTone };
}

function SourceTable({ rows }: { rows: Array<{ label: string; source: SourceEnvelope<unknown> }> }) {
  return (
    <div aria-label="权威来源状态列表" className="table-wrap" role="region" tabIndex={0}>
      <table className="ops-table source-table">
        <thead><tr><th>来源</th><th>状态</th><th>Console 读回</th><th>权威更新时间</th></tr></thead>
        <tbody>{rows.map(({ label, source }) => (
          <tr key={label} data-state={source.status === "available" ? "active" : source.status === "empty" ? "" : "manual_review"}>
            <td><strong>{label}</strong><small>{source.source}</small></td>
            <td><SourceBadge source={source} /></td>
            <td>{source.fetchedAt ? formatDate(source.fetchedAt, true) : "暂不可用"}</td>
            <td>{source.sourceUpdatedAt ? formatDate(source.sourceUpdatedAt, true) : "-"}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  );
}

function AnnouncementList({ announcements, controller }: {
  announcements: AnnouncementDTO[];
  controller: OperatorAnnouncementController;
}) {
  return (
    <div className="announcement-list">
      {announcements.map((announcement) => {
        const tone = statusTone(announcement.status);
        const dot = tone === "success" ? "ss-dot--ok" : tone === "danger" ? "ss-dot--bad" : tone === "warning" ? "ss-dot--warn" : "ss-dot--muted";
        return (
          <article className="announcement-item" key={announcement.id}>
            <header>
              <div><h3>{announcement.title}</h3><Badge color={tone}><span aria-hidden="true" className={`ss-dot ${dot}`} />{statusLabel(announcement.status)}</Badge></div>
              <span>{formatDate(announcement.updatedAt, true)}</span>
            </header>
            <p>{announcement.body}</p>
            <footer className="announcement-item__footer">
              <div className="announcement-item__schedule">
                <span>开始 {announcement.startsAt ? formatDate(announcement.startsAt, true) : "未设置"}</span>
                <span aria-hidden="true">·</span>
                <span>结束 {announcement.endsAt ? formatDate(announcement.endsAt, true) : "未设置"}</span>
              </div>
              <div className="table-actions">
                {!cloudIdentity && ["draft", "scheduled"].includes(announcement.status) ? <Button busy={controller.busyAnnouncementIds.includes(announcement.id)} onClick={() => void controller.publish(announcement.id)} size="sm" variant="outline">发布</Button> : null}
                {!cloudIdentity && announcement.status === "published" ? <Button busy={controller.busyAnnouncementIds.includes(announcement.id)} color="danger" onClick={() => void controller.withdraw(announcement.id)} size="sm" variant="outline">撤下</Button> : null}
              </div>
            </footer>
          </article>
        );
      })}
    </div>
  );
}

function AnnouncementDraftModal({ controller, onClose, open }: { controller: OperatorAnnouncementController; onClose: () => void; open: boolean }) {
  const [form, setForm] = useState<AnnouncementDraftRequest>({ title: "", body: "", startsAt: "", endsAt: "" });
  const updateForm = (field: keyof AnnouncementDraftRequest, value: string) => setForm((current) => ({ ...current, [field]: value }));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!form.title.trim() || !form.body.trim()) return;
    const ok = await controller.create({
      title: form.title.trim(),
      body: form.body.trim(),
      ...(form.startsAt?.trim() ? { startsAt: form.startsAt.trim() } : {}),
      ...(form.endsAt?.trim() ? { endsAt: form.endsAt.trim() } : {})
    });
    if (ok) {
      setForm({ title: "", body: "", startsAt: "", endsAt: "" });
      onClose();
    }
  };
  return (
    <Modal
      className="modal"
      description="草稿由 Control Plane 保存；发布和撤下会单独确认并写入审计。"
      footer={<><Button disabled={controller.createBusy} onClick={onClose} variant="outline">取消</Button><Button busy={controller.createBusy} color="primary" form="announcement-draft-form" type="submit">保存草稿</Button></>}
      onClose={onClose}
      open={open}
      title="新建公告草稿"
    >
      <form id="announcement-draft-form" onSubmit={submit}>
        <Field autoFocus label="标题" maxLength={120} onChange={(event) => updateForm("title", event.currentTarget.value)} required value={form.title} />
        <Field label="正文" maxLength={4000} multiline onChange={(event) => updateForm("body", event.currentTarget.value)} required rows={7} value={form.body} />
        <Field label="开始时间" onChange={(event) => updateForm("startsAt", event.currentTarget.value)} optional placeholder="2026-07-30T09:00:00Z" value={form.startsAt} />
        <Field label="结束时间" onChange={(event) => updateForm("endsAt", event.currentTarget.value)} optional placeholder="2026-08-01T09:00:00Z" value={form.endsAt} />
      </form>
    </Modal>
  );
}

function OverviewPage({ controller }: { controller: ConsoleController }) {
  const [draftOpen, setDraftOpen] = useState(false);
  const overviewSource = controller.sources.operatorOverview.value;
  const overview = sourceData(overviewSource);
  const announcementController = controller.operatorAnnouncements;
  const announcements = sourceData(announcementController.announcements.value)?.items || [];
  const health = overallHealth(overview?.health);
  const reconciliationCount = overview?.reconciliation.available ? overview.reconciliation.data.total : null;
  const attentionPath = reconciliationCount && reconciliationCount > 0 ? "/admin/billing" : health.label !== "正常" ? "/admin/system" : "";
  const rows = overview ? [
    { label: "Control Plane 账户", source: overview.accounts },
    { label: "OPL 账户 Gateway 汇总余额", source: overview.wallet },
    { label: "OPL 账户 Gateway Key", source: overview.keys },
    { label: "OPL 账户 Gateway API 用量", source: overview.usage },
    { label: overview.workspaces.source === "workspace" ? "Workspace" : "历史 CP Workspace", source: overview.workspaces },
    { label: "Fabric 资源", source: overview.resources },
    { label: "Ledger 计费复核", source: overview.reconciliation },
    { label: "系统健康", source: overview.health }
  ] : [];
  const accountsTotal = overview?.accounts.available ? overview.accounts.data.total : null;
  const accountsActive = overview?.accounts.available ? overview.accounts.data.active : null;
  const accountsDisabled = overview?.accounts.available ? overview.accounts.data.disabled : null;
  const healthOk = health.label === "正常";
  const attentionItems = [
    reconciliationCount && reconciliationCount > 0 ? { key: "reconciliation", tone: "warning", title: `${formatCount(reconciliationCount)} 个项目等待计费复核`, description: "到计费复核处理开通结案与退款确认。", actionLabel: "去复核", path: "/admin/billing" } : null,
    !healthOk ? { key: "health", tone: "danger", title: `系统健康：${health.label}`, description: "五个服务域按最差真实状态汇总。", actionLabel: "看详情", path: "/admin/system" } : null,
    rows.some(({ source }) => source.status === "unavailable") ? { key: "sources", tone: "warning", title: "部分权威来源暂不可用", description: "相关值未显示为零，避免误读。", actionLabel: "", path: "" } : null
  ].filter(Boolean) as Array<{ key: string; tone: "warning" | "danger"; title: string; description: string; actionLabel: string; path: string }>;

  return (
    <section className="admin-dashboard" data-slide="A-OV-01 A-OV-02">
      <section className="account-band">
        <div className="account-band-copy"><h2>运营总览</h2></div>
        <div className="band-metrics operator-metrics">
          <Metric label="计费账户" note={accountsTotal === null ? "来源暂不可用" : `正常 ${formatCount(accountsActive ?? 0)} · 停用 ${formatCount(accountsDisabled ?? 0)}`} value={accountsTotal === null ? "暂不可用" : formatCount(accountsTotal)} />
          <Metric label="Workspace" note={cloudIdentity ? "Workspace owner" : "历史 Control Plane"} value={overview?.workspaces.available ? formatCount(overview.workspaces.data.total) : "暂不可用"} />
          <Metric label="资源" note="Fabric 聚合" value={overview?.resources.available ? formatCount(overview.resources.data.total) : "暂不可用"} />
          <Metric label="待复核" note="Ledger / Control Plane" value={reconciliationCount === null ? "暂不可用" : formatCount(reconciliationCount)} />
        </div>
      </section>

      <SourceState error={controller.sources.operatorOverview.error} loading={controller.sources.operatorOverview.loading} onRetry={() => void controller.refreshCurrentPage()} source={overviewSource} unavailableTitle="运营概览暂不可用">
        {(data) => (
          <>
            <section className="metric-row">
              <article className="metric-card--sky"><WalletCards aria-hidden size={20} /><span>汇总余额<strong>{data.wallet.available ? formatUsdMicros(data.wallet.data.usdMicros) : "暂不可用"}</strong><small>OPL 账户 · Gateway 权威余额</small>{data.wallet.status === "unavailable" ? <small>{observationReason(data.wallet.reasonCode)}</small> : null}</span></article>
              <article><Server aria-hidden size={20} /><span>Key 总数<strong>{data.keys.available ? formatCount(data.keys.data.total) : "暂不可用"}</strong><small>OPL 账户 · Gateway Key 汇总</small>{data.keys.status === "unavailable" ? <small>{observationReason(data.keys.reasonCode)}</small> : null}</span></article>
              <article><CircleDollarSign aria-hidden size={20} /><span>累计 API 实际费用<strong>{data.usage.available ? formatUsdMicros(data.usage.data.totalActualCostUsdMicros) : "暂不可用"}</strong><small>OPL 账户 · {data.usage.available ? `今日 ${formatUsdMicros(data.usage.data.todayActualCostUsdMicros)}` : data.usage.status === "unavailable" ? observationReason(data.usage.reasonCode) : "暂不可用"}</small></span></article>
              <article className={healthOk ? "metric-ok" : "metric-warn"}><Activity aria-hidden size={20} /><span>总体健康<strong>{health.label}</strong><small>五个服务域的最差真实状态</small><span className="metric-gauge" aria-hidden="true"><span className={`metric-gauge__fill ${healthOk ? "is-ok" : "is-warn"}`} style={{ width: healthOk ? "100%" : "38%" }} /></span></span></article>
            </section>

            <section className="panel">
              <div className="panel-title"><div><h2>注意事项</h2><span>按复核、健康和来源状态排序</span></div>{attentionPath ? <Button color="primary" onClick={() => controller.navigate(attentionPath)} size="sm">进入处理</Button> : null}</div>
              {attentionItems.length ? <div className="attention-list">{attentionItems.map((item) => (
                <div className={`attention-card attention-card--${item.tone}`} key={item.key}>
                  <span className="attention-card__dot" aria-hidden="true" />
                  <div className="attention-card__body"><strong>{item.title}</strong><small>{item.description}</small></div>
                  {item.actionLabel && item.path ? <Button onClick={() => controller.navigate(item.path)} size="sm" variant="outline">{item.actionLabel}</Button> : null}
                </div>
              ))}</div> : <div className="attention-empty"><span aria-hidden="true" className="ss-dot ss-dot--ok" /><span>当前没有待处理事项。</span></div>}
            </section>

            <section className="panel">
              <div className="panel-title"><h2>来源状态</h2></div>
              <SourceTable rows={rows} />
            </section>
          </>
        )}
      </SourceState>

      <section className="panel" data-slide="A-OV-02">
        <div className="panel-title"><div><h2>公告管理</h2></div><div className="table-actions"><Button onClick={() => controller.navigate("/admin/announcements")} size="sm" variant="outline">管理全部公告</Button><Button hidden={cloudIdentity} color="primary" onClick={() => setDraftOpen(true)} size="sm"><Plus aria-hidden size={16} />新建草稿</Button></div></div>
        <SourceState empty={announcements.length === 0} emptyTitle="暂无公告" error={announcementController.announcements.error} loading={announcementController.announcements.loading} onRetry={() => void announcementController.refresh()} source={announcementController.announcements.value} unavailableTitle="公告暂不可用">
          {() => <AnnouncementList announcements={announcements} controller={announcementController} />}
        </SourceState>
      </section>
      <AnnouncementDraftModal controller={announcementController} onClose={() => setDraftOpen(false)} open={draftOpen} />
    </section>
  );
}

function AnnouncementsPage({ controller }: { controller: ConsoleController }) {
  const [draftOpen, setDraftOpen] = useState(false);
  const announcementController = controller.operatorAnnouncements;
  const announcements = sourceData(announcementController.announcements.value)?.items || [];
  return (
    <section className="admin-dashboard" data-slide="A-ANN-01">
      <section className="panel">
        <div className="panel-title">
          <div><h2>公告管理</h2><span>{announcements.length ? `${announcements.length} 条` : ""}</span></div>
          <div className="table-actions">
            <Button onClick={() => void announcementController.refresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={16} />刷新</Button>
            <Button hidden={cloudIdentity} color="primary" onClick={() => setDraftOpen(true)} size="sm"><Plus aria-hidden size={16} />新建草稿</Button>
          </div>
        </div>
        <SourceState empty={announcements.length === 0} emptyTitle="暂无公告" error={announcementController.announcements.error} loading={announcementController.announcements.loading} onRetry={() => void announcementController.refresh()} source={announcementController.announcements.value} unavailableTitle="公告暂不可用">
          {() => <AnnouncementList announcements={announcements} controller={announcementController} />}
        </SourceState>
      </section>
      <AnnouncementDraftModal controller={announcementController} onClose={() => setDraftOpen(false)} open={draftOpen} />
    </section>
  );
}

type AccountDialog = "provision" | "wallet" | "";

function AccountSourceSummary({ account }: { account: OperatorAccountDTO }) {
  const sources = [
    { label: "身份", source: account.gatewayIdentity },
    { label: "余额", source: account.wallet },
    { label: "Key", source: account.keyCount },
    { label: "API", source: account.usage },
    { label: "历史 CP Workspace", source: account.workspaceCount }
  ];
  return <div className="account-source-summary">{sources.map(({ label, source }) => <div className="account-source-summary__item" key={label}><span>{label}</span><SourceBadge source={source} /><small>{source.source} · {formatDate(source.fetchedAt, true)}</small></div>)}</div>;
}

function AccountFact<T>({ source, children }: { source: SourceEnvelope<T>; children: (data: T) => ReactNode }) {
  if (!source.available) return <span className="account-fact-unavailable">暂不可用</span>;
  return children(source.data);
}

function AccountStatusBadges({ account }: { account: OperatorAccountDTO }) {
  const active = account.status === "active";
  return (
    <span className="account-status-stack">
      <Badge color={statusTone(account.status)}>
        <span aria-hidden="true" className={`ss-dot ${active ? "ss-dot--ok" : "ss-dot--bad"}`} />
        {statusLabel(account.status)}
      </Badge>
      <Badge color={account.workspacePurchaseEnabled ? "success" : "secondary"}>
        <span aria-hidden="true" className={`ss-dot ${account.workspacePurchaseEnabled ? "ss-dot--ok" : "ss-dot--muted"}`} />
        {account.workspacePurchaseEnabled ? "可新购 Workspace" : "不可新购 Workspace"}
      </Badge>
    </span>
  );
}

function serviceEntitlementMeta(serviceId: string): { title: string; subtitle: string } {
  switch (serviceId) {
    case "netfleet_cloud":
      return {
        title: "OPL NetFleet Cloud",
        subtitle: "多站点网络监视与加密订阅同步控制面，支持 OPL Gateway 账号免地址自动纳管。"
      };
    case "link_relay":
      return {
        title: "OPL Link Relay",
        subtitle: "受控跨网中继与远程桌面访问服务准入控制。"
      };
    default:
      return {
        title: serviceId,
        subtitle: "受 CloudIdentity 统一准入控制的伴生服务。"
      };
  }
}

function AccountCapabilityTags({ account }: { account: OperatorAccountDTO }) {
  return (
    <span className="account-capabilities-stack">
      <span className="capability-pill">
        <span aria-hidden="true" className="ss-dot ss-dot--sky" />
        Gateway
      </span>
      <Badge color={account.workspacePurchaseEnabled ? "success" : "secondary"}>
        <span aria-hidden="true" className={`ss-dot ${account.workspacePurchaseEnabled ? "ss-dot--ok" : "ss-dot--muted"}`} />
        {account.workspacePurchaseEnabled ? "可新购 Workspace" : "不可新购 Workspace"}
      </Badge>
    </span>
  );
}

function OperatorAccountMobileCard({ account, selected, onInspect }: {
  account: OperatorAccountDTO;
  selected: boolean;
  onInspect: (account: OperatorAccountDTO) => void;
}) {
  const isAdmin = isOperatorPlatformAdmin(account);
  return (
    <article
      className={`operator-object-card operator-account-mobile-card ${selected ? "is-selected" : ""}`}
      onClick={() => onInspect(account)}
    >
      <header className="operator-object-card__header">
        <span>
          <strong>{account.email}</strong>
          <small className="account-identity-meta">
            {isAdmin ? <span className="account-role-pill account-role-pill--admin">管理员</span> : null}
            <code className="account-subject-chip">#{account.sub2apiUserId}</code>
          </small>
        </span>
        <AccountStatusBadges account={account} />
      </header>
      <dl className="operator-object-card__facts operator-account-mobile-facts">
        <div className="operator-object-card__wide"><dt>已开放能力</dt><dd><AccountCapabilityTags account={account} /></dd></div>
        <div><dt>可用余额</dt><dd><AccountFact source={account.wallet}>{(wallet) => <strong>{formatUsdMicros(wallet.usdMicros)}</strong>}</AccountFact></dd></div>
        <div><dt>累计 API 消费</dt><dd><AccountFact source={account.usage}>{(usage) => <strong>{formatUsdMicros(usage.totalActualCostUsdMicros)}</strong>}</AccountFact></dd></div>
      </dl>
      <div className="operator-card-actions">
        <Button
          aria-label={`查看 ${account.email} 详情与权限`}
          onClick={(event) => { event.stopPropagation(); onInspect(account); }}
          size="sm"
          variant="outline"
        >
          管理权限<ChevronRight aria-hidden size={14} />
        </Button>
      </div>
    </article>
  );
}

function ProvisionAccountModal({ controller, onClose, open }: { controller: ConsoleController; onClose: () => void; open: boolean }) {
  const [form, setForm] = useState({ email: "", password: "", name: "", admission: "full_cloud_customer" as "full_cloud_customer" | "gateway_only" });
  const [submittedEmail, setSubmittedEmail] = useState("");
  const [completed, setCompleted] = useState(false);
  const [account, setAccount] = useState<OperatorAccountDTO | null>(null);
  const updateForm = (field: keyof typeof form, value: string) => setForm((current) => ({ ...current, [field]: value }));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!form.email.trim() || !form.password) return;
    const email = form.email.trim();
    controller.operatorAccounts.setProvisionOperation(null);
    const operation = await controller.operatorAccounts.provision({ email, password: form.password, admission: form.admission, ...(form.name.trim() ? { name: form.name.trim() } : {}) });
    if (operation) {
      setSubmittedEmail(email);
      setAccount(operation.account);
      setCompleted(Boolean(operation.account));
      setForm((value) => ({ ...value, password: "" }));
    }
  };
  const close = () => {
    setForm({ email: "", password: "", name: "", admission: "full_cloud_customer" });
    setSubmittedEmail("");
    setCompleted(false);
    setAccount(null);
    controller.operatorAccounts.setProvisionOperation(null);
    onClose();
  };
  return (
    <Modal
      className="modal"
      description="将创建 Account、Console User、Sub2API 身份及一对一映射；产品范围由本次准入选择决定。"
      footer={completed ? <Button onClick={close} variant="outline">完成</Button> : <><Button disabled={controller.operatorAccounts.provisionBusy} onClick={close} variant="outline">取消</Button><Button busy={controller.operatorAccounts.provisionBusy} color="primary" form="provision-account-form" type="submit">开通用户</Button></>}
      onClose={close}
      open={open}
      title="开通用户"
    >
      {completed ? (
        <section className="wallet-adjustment-readback" data-slide="A-ACC-02">
          <div className="inline-notice"><span>账户映射已完成权威读回</span></div>
          <dl className="data-list">
            <div><dt>登录邮箱</dt><dd>{submittedEmail}</dd></div>
            {account?.consoleUserId ? <div><dt>Account ID</dt><dd>{account.accountId}</dd></div> : null}
            {account?.consoleUserId ? <div><dt>Console User ID</dt><dd>{account.consoleUserId}</dd></div> : null}
            <div><dt>Sub2API User ID</dt><dd>{account?.gatewayIdentity.available ? account.gatewayIdentity.data.userId : account?.sub2apiUserId || "暂不可用"}</dd></div>
            <div><dt>operation ID</dt><dd>{controller.operatorAccounts.provisionOperation?.operationId || "暂不可用"}</dd></div>
            <div><dt>状态</dt><dd>{controller.operatorAccounts.provisionOperation?.status || "暂不可用"}</dd></div>
            <div><dt>phase</dt><dd>{controller.operatorAccounts.provisionOperation?.phase || "暂不可用"}</dd></div>
            <div><dt>errorCode</dt><dd>{controller.operatorAccounts.provisionOperation?.errorCode || "暂不可用"}</dd></div>
          </dl>
        </section>
      ) : (
        <form id="provision-account-form" onSubmit={submit}>
          <Field autoComplete="email" autoFocus label="登录邮箱" onChange={(event) => updateForm("email", event.currentTarget.value)} required type="email" value={form.email} />
          <Field autoComplete="new-password" label="初始密码" onChange={(event) => updateForm("password", event.currentTarget.value)} required type="password" value={form.password} />
          <Field label="姓名" onChange={(event) => updateForm("name", event.currentTarget.value)} optional value={form.name} />
          <div className="provision-admission-field">
            <span>产品范围</span>
            <SegmentedControl
              ariaLabel="账户产品范围"
              block
              onChange={(value) => updateForm("admission", value)}
              options={[
                { value: "full_cloud_customer", label: "完整 Cloud 客户" },
                { value: "gateway_only", label: "仅 Gateway" }
              ]}
              value={form.admission}
            />
          </div>
        </form>
      )}
    </Modal>
  );
}

type PermissionFilter = "all" | "eligible" | "restricted" | "admin" | "disabled";

function AccountInspectorDrawer({ account, controller, onClose, onOpenWallet }: {
  account: OperatorAccountDTO | null;
  controller: ConsoleController;
  onClose: () => void;
  onOpenWallet: (account: OperatorAccountDTO) => void;
}) {
  useEffect(() => {
    if (!account) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (document.querySelector('[role="dialog"]')) return;
      event.preventDefault();
      onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [account, onClose]);

  if (!account) return null;
  const isAdmin = isOperatorPlatformAdmin(account);
  const bootstrapAdmin = isOperatorBootstrapAdmin(account);
  const selfAccount = Boolean(
    controller.session && (
      (account.sub2apiUserId && account.sub2apiUserId === controller.session.user.id)
      || account.email.toLowerCase() === controller.session.user.email.toLowerCase()
    )
  );
  const hasLegacyMapping = Boolean(account.consoleUserId) || (Boolean(account.accountId) && account.accountId !== account.sub2apiUserId);
  const busy = controller.operatorAccounts.busyAccountIds.includes(account.accountId);
  const active = account.status === "active";

  return (
    <>
      <div aria-hidden="true" className="account-inspector-scrim" onClick={onClose} />
      <aside aria-label="用户详情与权限" className="account-inspector-drawer" data-slide="A-ACC-03">
        <header className="account-inspector-drawer__header">
          <div className="account-inspector-drawer__identity">
            <span aria-hidden="true" className={`account-avatar account-avatar--lg ${isAdmin ? "account-avatar--admin" : ""}`}>
              {(account.email[0] || "?").toUpperCase()}
            </span>
            <div className="account-inspector-drawer__title">
              <strong>{account.email}</strong>
              <span className="account-identity-meta">
                <span className={`account-role-pill ${isAdmin ? "account-role-pill--admin" : ""}`}>{bootstrapAdmin ? "引导根管理员" : isAdmin ? "管理员" : "普通用户"}</span>
                <code className="account-subject-chip">Sub2API #{account.sub2apiUserId}</code>
                <Badge color={statusTone(account.status)}>
                  <span aria-hidden="true" className={`ss-dot ${active ? "ss-dot--ok" : "ss-dot--bad"}`} />
                  {statusLabel(account.status)}
                </Badge>
              </span>
            </div>
          </div>
          <Button aria-label="关闭详情" color="secondary" onClick={onClose} size="sm" uniform variant="ghost">
            <X aria-hidden size={18} />
          </Button>
        </header>

        <div className="account-inspector-drawer__body">
          <section className="inspector-section">
            <div className="inspector-section__head">
              <h3>能力与权限</h3>
              <small>按 OPL Gateway 主体配置产品权限白名单</small>
            </div>

            <div className="inspector-capability-row">
              <div className="inspector-capability-row__copy">
                <strong>平台角色</strong>
                <small>{bootstrapAdmin ? "引导根管理员（环境配置固定，不可撤销）。" : isAdmin ? "委派平台管理员：拥有普通 Gateway 账户能力与平台管理界面权限。" : "普通 OPL Gateway 用户，可按需叠加平台管理角色。"}</small>
              </div>
              <div className="inspector-capability-row__control">
                <span className={`account-role-pill ${isAdmin ? "account-role-pill--admin" : ""}`}>{bootstrapAdmin ? "引导根管理员" : isAdmin ? "管理员" : "普通用户"}</span>
                {bootstrapAdmin
                  ? <span className="account-read-only">根管理员不可撤销</span>
                  : cloudIdentity
                    ? isAdmin && selfAccount
                      ? <span className="account-read-only">不可撤销当前登录账户</span>
                      : active
                        ? <Button
                            className="account-permission-toggle"
                            color={isAdmin ? "danger" : "secondary"}
                            disabled={busy}
                            onClick={() => void controller.operatorAccounts.setPlatformAdmin(account.accountId, !isAdmin)}
                            size="sm"
                            variant="outline"
                          >
                            {isAdmin ? <ShieldOff aria-hidden size={14} /> : <ShieldCheck aria-hidden size={14} />}
                            {isAdmin ? "撤销管理员" : "授予管理员"}
                          </Button>
                        : <span className="account-read-only">账户已停用</span>
                    : null}
              </div>
            </div>

            <div className="inspector-capability-row">
              <div className="inspector-capability-row__copy">
                <strong>OPL Gateway</strong>
                <small>API 密钥、模型用量与账户余额随 Gateway 主体默认可用。</small>
              </div>
              <Badge color="success">
                <span aria-hidden="true" className="ss-dot ss-dot--ok" />
                已开通
              </Badge>
            </div>

            <div className="inspector-capability-row">
              <div className="inspector-capability-row__copy">
                <strong>Workspace 新购资格</strong>
                <small>控制是否可新购云端 Workspace；已有工作空间不受撤销影响。</small>
              </div>
              <div className="inspector-capability-row__control">
                <Badge color={account.workspacePurchaseEnabled ? "success" : "secondary"}>
                  <span aria-hidden="true" className={`ss-dot ${account.workspacePurchaseEnabled ? "ss-dot--ok" : "ss-dot--muted"}`} />
                  {account.workspacePurchaseEnabled ? "可新购 Workspace" : "不可新购 Workspace"}
                </Badge>
                {bootstrapAdmin
                  ? <span className="account-read-only">默认拥有平台权限</span>
                  : active
                    ? <Button
                        className="account-permission-toggle"
                        color={account.workspacePurchaseEnabled ? "danger" : "primary"}
                        disabled={busy}
                        onClick={() => void controller.operatorAccounts.setWorkspacePurchaseEligibility(account.accountId, !account.workspacePurchaseEnabled)}
                        size="sm"
                        variant="outline"
                      >
                        {account.workspacePurchaseEnabled ? <ShieldOff aria-hidden size={14} /> : <ShieldCheck aria-hidden size={14} />}
                        {account.workspacePurchaseEnabled ? "撤销新购" : "授予新购"}
                      </Button>
                    : <span className="account-read-only">账户已停用</span>}
              </div>
            </div>

            {(account.serviceEntitlements ?? []).map((item) => {
              const meta = serviceEntitlementMeta(item.serviceId);
              const statusText = item.effectiveEnabled
                ? (item.hasExplicitOverride ? "已开通" : "默认开通")
                : "已停用";
              return (
                <div className="inspector-capability-row" key={item.serviceId}>
                  <div className="inspector-capability-row__copy">
                    <strong>{meta.title}</strong>
                    <small>{meta.subtitle}</small>
                  </div>
                  <div className="inspector-capability-row__control">
                    <Badge color={item.effectiveEnabled ? "success" : "secondary"}>
                      <span aria-hidden="true" className={`ss-dot ${item.effectiveEnabled ? "ss-dot--ok" : "ss-dot--muted"}`} />
                      {statusText}
                    </Badge>
                    {active ? (
                      <Button
                        className="account-permission-toggle"
                        color={item.effectiveEnabled ? "danger" : "primary"}
                        disabled={busy}
                        onClick={() => void controller.operatorAccounts.setServiceEntitlement(account.accountId, item.serviceId, !item.effectiveEnabled)}
                        size="sm"
                        variant="outline"
                      >
                        {item.effectiveEnabled ? <ShieldOff aria-hidden size={14} /> : <ShieldCheck aria-hidden size={14} />}
                        {item.effectiveEnabled ? "停用" : "开通"}
                      </Button>
                    ) : <span className="account-read-only">账户已停用</span>}
                  </div>
                </div>
              );
            })}

            {!(account.serviceEntitlements && account.serviceEntitlements.length > 0) && (
              <p className="inspector-roadmap-note">
                当前实例尚未配置伴生服务目录；接入 Service Catalog 后将自动在此展示与控制各服务准入。
              </p>
            )}
          </section>

          <section className="inspector-section">
            <div className="inspector-section__head">
              <h3>账户概览与操作</h3>
            </div>
            <div className="account-detail-metrics">
              <div><span>钱包余额</span><strong><AccountFact source={account.wallet}>{(data) => formatUsdMicros(data.usdMicros)}</AccountFact></strong></div>
              <div><span>今日 API 费用</span><strong><AccountFact source={account.usage}>{(data) => formatUsdMicros(data.todayActualCostUsdMicros)}</AccountFact></strong></div>
              <div><span>累计 API 费用</span><strong><AccountFact source={account.usage}>{(data) => formatUsdMicros(data.totalActualCostUsdMicros)}</AccountFact></strong></div>
              <div><span>Key / 历史 Workspace</span><strong><AccountFact source={account.keyCount}>{formatCount}</AccountFact> / <AccountFact source={account.workspaceCount}>{formatCount}</AccountFact></strong></div>
            </div>
            {!cloudIdentity ? (
              <div className="inspector-actions-bar">
                <Button onClick={() => onOpenWallet(account)} size="sm" variant="outline">
                  <WalletCards aria-hidden size={14} />
                  余额操作
                </Button>
                {!bootstrapAdmin && active ? (
                  <Button color="danger" disabled={busy} onClick={() => void controller.operatorAccounts.disable(account.accountId)} size="sm" variant="outline">
                    <Ban aria-hidden size={14} />
                    停用
                  </Button>
                ) : null}
              </div>
            ) : null}
          </section>

          <details className="inspector-tech-details">
            <summary>
              <span>技术映射与来源读回</span>
              <ChevronDown aria-hidden size={15} />
            </summary>
            <div className="inspector-tech-details__content">
              {hasLegacyMapping ? (
                <section className="data-section"><h4>Control Plane 映射</h4><dl className="data-list">
                  <div><dt>Account</dt><dd>{account.accountId}</dd></div>
                  {account.consoleUserId ? <div><dt>Console User</dt><dd>{account.consoleUserId}</dd></div> : null}
                  <div><dt>Sub2API User 记录</dt><dd>{account.sub2apiUserId}</dd></div>
                  <div><dt>角色</dt><dd>{account.role}</dd></div>
                </dl></section>
              ) : (
                <section className="data-section"><h4>OPL Gateway 主体映射</h4><dl className="data-list">
                  <div><dt>Gateway Subject ID</dt><dd>{account.sub2apiUserId}</dd></div>
                  <div><dt>平台角色</dt><dd>{bootstrapAdmin ? "引导根管理员" : isAdmin ? "委派管理员" : "普通用户"}</dd></div>
                  <div><dt>来源</dt><dd>{account.gatewayIdentity.source}</dd></div>
                </dl></section>
              )}
              <section className="data-section"><h4>Gateway 与 Workspace 权威读回</h4><dl className="data-list">
                <div><dt>Sub2API 实时映射</dt><dd><SourceValue source={account.gatewayIdentity}>{(data) => `${data.userId} · ${data.email} · ${data.status}`}</SourceValue></dd></div>
                <div><dt>钱包余额</dt><dd><SourceValue source={account.wallet}>{(data) => `${formatUsdMicros(data.usdMicros)} · ${data.status}`}</SourceValue></dd></div>
                <div><dt>Key 汇总</dt><dd><SourceValue source={account.keyCount}>{formatCount}</SourceValue></dd></div>
                <div><dt>API Usage</dt><dd><SourceValue source={account.usage}>{(data) => `今日 ${formatUsdMicros(data.todayActualCostUsdMicros)} · 累计 ${formatUsdMicros(data.totalActualCostUsdMicros)}`}</SourceValue></dd></div>
                <div><dt>历史 CP Workspace 汇总</dt><dd><SourceValue source={account.workspaceCount}>{formatCount}</SourceValue></dd></div>
              </dl></section>
              <section className="data-section account-source-detail"><h4>来源状态与读回时间</h4><AccountSourceSummary account={account} /></section>
            </div>
          </details>
        </div>
      </aside>
    </>
  );
}

function WalletOperationReadback({ controller }: { controller: ConsoleController }) {
  const operation = controller.walletAdjustmentOperation;
  if (!operation) return null;
  const recoverable = operation.status === "manual_review" && operation.allowedActions?.includes("recover_wallet_adjustment");
  const settled = operation.status === "succeeded" || operation.status === "failed";
  const facts = [
    { label: "操作 ID", value: operation.operationId },
    { label: "调整前余额", value: operation.beforeBalance.available ? formatUsdMicros(operation.beforeBalance.data.usdMicros) : "暂不可用" },
    { label: "调整后余额", value: operation.afterBalance.available ? formatUsdMicros(operation.afterBalance.data.usdMicros) : "暂不可用" },
    { label: "原因", value: operation.reason },
    { label: "关联操作", value: operation.relatedOperationId || "—" },
    { label: "余额历史引用", value: operation.balanceHistoryRef || "—" },
    { label: "回执 ID", value: operation.receiptId || "—" },
    { label: "操作人", value: operation.actor || "暂不可用" },
    { label: "错误码", value: operation.errorCode || "—" },
    { label: "上游阶段 / HTTP", value: operation.upstreamFailure ? `${operation.upstreamFailure.phase} / ${operation.upstreamFailure.httpStatus ?? "暂不可用"}` : "—" },
    { label: "上游错误码 / 请求 ID", value: operation.upstreamFailure ? `${operation.upstreamFailure.errorCode} / ${operation.upstreamFailure.requestId || "暂不可用"}` : "—" },
    { label: "允许动作", value: operation.allowedActions?.length ? operation.allowedActions.join(", ") : "无" }
  ];
  return (
    <section className="wallet-adjustment-readback">
      <div className="wallet-readback-banner" data-status={operation.status}>
        <div className="wallet-readback-banner__main">
          <strong>{settled ? (operation.status === "succeeded" ? "操作完成" : "操作失败") : "结果待确认"}</strong>
          <small>操作 ID {operation.operationId} · 当前状态：{statusLabel(operation.status)}</small>
        </div>
        {settled ? null : <Button onClick={() => void controller.refreshWalletOperation()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />刷新</Button>}
      </div>
      <details className="wallet-readback-facts" open={settled ? false : true}>
        <summary>操作详情与证据引用</summary>
        <dl className="wallet-readback-grid">
          {facts.map((fact) => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}
        </dl>
      </details>
      {recoverable ? <div className="page-actions"><span>仅核对原操作结果，不会再次扣款或退款。</span><Button busy={controller.walletAdjustmentBusy} color="primary" onClick={() => void controller.recoverWalletOperation()}>重新核验</Button></div> : null}
    </section>
  );
}

function WalletAdjustmentModal({ account, controller, onClose }: { account: OperatorAccountDTO | null; controller: ConsoleController; onClose: () => void }) {
  const [form, setForm] = useState<WalletAdjustmentRequest>({ kind: "recharge", amountUsd: "", reason: "", relatedOperationId: "", confirmationAccountId: "" });
  const open = Boolean(account);
  const updateForm = (field: keyof WalletAdjustmentRequest, value: string) => setForm((current) => ({ ...current, [field]: value }));
  const reset = () => {
    setForm({ kind: "recharge", amountUsd: "", reason: "", relatedOperationId: "", confirmationAccountId: "" });
    controller.setWalletAdjustmentOperation(null);
    onClose();
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!account) return;
    const input: WalletAdjustmentRequest = {
      kind: form.kind,
      amountUsd: form.amountUsd.trim(),
      reason: form.reason.trim(),
      confirmationAccountId: account.accountId,
      ...(form.kind === "business_refund" && form.relatedOperationId?.trim() ? { relatedOperationId: form.relatedOperationId.trim() } : {})
    };
    void controller.submitWalletAdjustment(account.accountId, input);
  };
  const valid = Boolean(account && form.amountUsd.trim() && form.reason.trim() && (form.kind !== "business_refund" || form.relatedOperationId?.trim()));
  return (
    <Modal
      className="modal wallet-adjustment-modal"
      description={account ? `目标 Account ID：${account.accountId}` : undefined}
      footer={<><Button disabled={controller.walletAdjustmentBusy} onClick={reset} variant="outline">关闭</Button>{controller.walletAdjustmentOperation ? null : <Button busy={controller.walletAdjustmentBusy} color="primary" disabled={!valid} form="wallet-adjustment-form" type="submit">确认操作</Button>}</>}
      onClose={reset}
      open={open}
      title="余额操作"
    >
      {account ? (
        <div data-slide="A-ACC-03">
          {controller.walletAdjustmentOperation ? null : <>
            <div className="wallet-target-card">
              <div className="wallet-target-card__id"><small>目标账户</small><code>{account.accountId}</code></div>
              <div className="wallet-target-card__balance"><AccountFact source={account.wallet}>{(wallet) => <><small>当前余额</small><strong>{formatUsdMicros(wallet.usdMicros)}</strong></>}</AccountFact></div>
            </div>
            <form id="wallet-adjustment-form" onSubmit={submit}>
              <div className="wallet-kind-picker">
                <span className="wallet-kind-picker__label">操作类型</span>
                <SegmentedControl ariaLabel="操作类型" block onChange={(kind) => setForm((value) => ({ ...value, kind: kind as WalletAdjustmentRequest["kind"] }))} options={[{ value: "recharge", label: "充值" }, { value: "debit", label: "扣减" }, { value: "business_refund", label: "业务退款" }]} value={form.kind} />
              </div>
              <div className="wallet-amount-row">
                <Field inputMode="decimal" label="金额（USD）" min="0.000001" onChange={(event) => updateForm("amountUsd", event.currentTarget.value)} required step="0.000001" type="number" value={form.amountUsd} />
              </div>
              <Field label="业务原因" maxLength={200} multiline onChange={(event) => updateForm("reason", event.currentTarget.value)} required rows={3} value={form.reason} />
              {form.kind === "business_refund" ? <Field label="关联 operation ID" onChange={(event) => updateForm("relatedOperationId", event.currentTarget.value)} required value={form.relatedOperationId || ""} /> : null}
            </form>
          </>}
          <WalletOperationReadback controller={controller} />
        </div>
      ) : null}
    </Modal>
  );
}

function AccountsPage({ controller }: { controller: ConsoleController }) {
  const [dialog, setDialog] = useState<AccountDialog>("");
  const [inspectedAccountId, setInspectedAccountId] = useState("");
  const [walletAccountId, setWalletAccountId] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [permissionFilter, setPermissionFilter] = useState<PermissionFilter>("all");
  const accountController = controller.operatorAccounts;
  const accounts = sourceData(accountController.accounts.value)?.items || [];
  const inspectedAccount = accounts.find((account) => account.accountId === inspectedAccountId) || null;
  const walletAccount = accounts.find((account) => account.accountId === walletAccountId) || null;
  const openInspector = (account: OperatorAccountDTO) => {
    setInspectedAccountId(account.accountId);
  };
  const openWallet = (account: OperatorAccountDTO) => {
    controller.setWalletAdjustmentOperation(null);
    setWalletAccountId(account.accountId);
    setDialog("wallet");
  };
  const resetFilters = () => {
    setSearchQuery("");
    setPermissionFilter("all");
    if (cloudIdentity && accountController.searchQuery) {
      void accountController.searchAccounts("");
    }
  };
  return (
    <>
      <div className={`accounts-master-detail ${inspectedAccount ? "has-inspector" : ""}`}>
        <section className="panel" data-slide="A-ACC-01 A-ACC-02 A-ACC-03">
          <div className="panel-title operator-accounts-panel-title">
            <div className="operator-accounts-title"><h2>用户与权限</h2></div>
            <div className="operator-accounts-panel-actions">
              <Button aria-label="刷新账户列表" disabled={accountController.accounts.loading} onClick={() => void accountController.refresh()} size="sm" variant="outline"><RefreshCw aria-hidden size={14} />刷新</Button>
              <Button hidden={!cloudIdentity} color="primary" onClick={() => setDialog("provision")}><Plus aria-hidden size={16} />开通用户</Button>
            </div>
          </div>
      <SourceState empty={accounts.length === 0 && !accountController.searchQuery} emptyTitle="暂无用户" error={accountController.accounts.error} loading={accountController.accounts.loading} onRetry={() => void accountController.refresh()} source={accountController.accounts.value} unavailableTitle="账户数据暂不可用">
        {(data) => {
          const items = data.items;
          const activeCount = items.filter((item) => item.status === "active").length;
          const disabledCount = items.filter((item) => item.status !== "active").length;
          const eligibleCount = items.filter((item) => item.workspacePurchaseEnabled).length;
          const restrictedCount = items.filter((item) => !item.workspacePurchaseEnabled && item.status === "active" && !isOperatorPlatformAdmin(item)).length;
          const adminCount = items.filter(isOperatorPlatformAdmin).length;
          const normalizedQuery = searchQuery.trim().toLowerCase();
          const filterOptions: Array<{ id: PermissionFilter; label: string; count: number }> = [
            { id: "all", label: "全部", count: items.length },
            { id: "eligible", label: "已开放 Workspace", count: eligibleCount },
            { id: "restricted", label: "仅 Gateway", count: restrictedCount },
            { id: "admin", label: "管理员", count: adminCount },
            { id: "disabled", label: "已停用", count: disabledCount }
          ];
          const filteredItems = items.filter((item) => {
            if (permissionFilter === "eligible" && !item.workspacePurchaseEnabled) return false;
            if (permissionFilter === "restricted" && (item.workspacePurchaseEnabled || item.status !== "active" || isOperatorPlatformAdmin(item))) return false;
            if (permissionFilter === "admin" && !isOperatorPlatformAdmin(item)) return false;
            if (permissionFilter === "disabled" && item.status === "active") return false;
            if (!normalizedQuery) return true;
            return [item.email, item.accountId, item.consoleUserId, item.sub2apiUserId]
              .some((value) => String(value || "").toLowerCase().includes(normalizedQuery));
          });

          return <>
            <div className="operator-accounts-summary">
              <div className="operator-accounts-summary__cell">
                <span className="operator-accounts-summary__label">用户总数</span>
                <strong className="operator-accounts-summary__value">{formatCount(data.total)}</strong>
                <span className="ss-telemetry-note">当前页 {formatCount(items.length)} 位</span>
              </div>
              <div className="operator-accounts-summary__cell">
                <span className="operator-accounts-summary__label">活跃状态</span>
                <strong className="operator-accounts-summary__value">{formatCount(activeCount)} / {formatCount(items.length)}</strong>
                <span className="ss-telemetry-note"><span aria-hidden="true" className="ss-dot ss-dot--ok" />正常 · 停用 {formatCount(disabledCount)}</span>
              </div>
              <div className="operator-accounts-summary__cell">
                <span className="operator-accounts-summary__label">Workspace 白名单</span>
                <strong className="operator-accounts-summary__value">{formatCount(eligibleCount)} / {formatCount(items.length)}</strong>
                <span className="ss-telemetry-note"><span aria-hidden="true" className="ss-dot ss-dot--ok" />显式白名单放行</span>
              </div>
              <div className="operator-accounts-summary__cell">
                <span className="operator-accounts-summary__label">平台管理员</span>
                <strong className="operator-accounts-summary__value">{formatCount(adminCount)}</strong>
                <span className="ss-telemetry-note"><span aria-hidden="true" className="ss-dot ss-dot--sky" />普通用户能力 + 管理权限</span>
              </div>
            </div>

            {items.length > 0 || accountController.searchQuery ? (
              <div className="operator-accounts-toolbar">
                <form
                  className="operator-accounts-search"
                  onSubmit={(event) => {
                    event.preventDefault();
                    if (cloudIdentity) void accountController.searchAccounts(searchQuery);
                  }}
                >
                  <Search aria-hidden size={15} />
                  <input
                    aria-label="筛选当前页用户"
                    onChange={(event) => setSearchQuery(event.currentTarget.value)}
                    placeholder={cloudIdentity ? "搜索邮箱或 Gateway ID（回车查全库）…" : "搜索邮箱、Gateway ID 或 Account ID…"}
                    type="search"
                    value={searchQuery}
                  />
                  {searchQuery ? (
                    <button
                      aria-label="清除搜索"
                      className="operator-accounts-search__clear"
                      onClick={() => {
                        setSearchQuery("");
                        if (cloudIdentity && accountController.searchQuery) void accountController.searchAccounts("");
                      }}
                      type="button"
                    >
                      <X aria-hidden size={14} />
                    </button>
                  ) : null}
                </form>
                <div aria-label="按权限与状态筛选" className="ss-filter-bar" role="group">
                  {filterOptions.map((option) => (
                    <button
                      aria-pressed={permissionFilter === option.id}
                      className="ss-filter-chip"
                      key={option.id}
                      onClick={() => setPermissionFilter(option.id)}
                      type="button"
                    >
                      <span>{option.label}</span>
                      <span className="ss-filter-chip__count">{formatCount(option.count)}</span>
                    </button>
                  ))}
                </div>
              </div>
            ) : null}

            {filteredItems.length === 0 ? (
              <div className="operator-accounts-filter-empty">
                <span>{accountController.searchQuery ? "全量用户检索无匹配结果" : "当前页没有匹配筛选条件的用户"}</span>
                <Button onClick={resetFilters} size="sm" variant="outline">重置筛选</Button>
              </div>
            ) : <>
              <div aria-label="用户与权限列表" className="table-wrap operator-account-table" role="region" tabIndex={0}><table className="ops-table"><thead><tr><th>用户</th><th>已开放能力</th><th>余额与消费</th><th>状态</th><th>操作</th></tr></thead><tbody>{filteredItems.map((account) => {
                const isAdmin = isOperatorPlatformAdmin(account);
                const selected = inspectedAccount?.accountId === account.accountId;
                const active = account.status === "active";
                return (
                  <tr
                    className={selected ? "is-selected" : undefined}
                    data-state={account.status}
                    key={account.accountId}
                    onClick={() => openInspector(account)}
                  >
                    <td>
                      <span className="operator-account-identity">
                        <span className={`account-avatar ${isAdmin ? "account-avatar--admin" : ""}`} aria-hidden="true">{(account.email[0] || "?").toUpperCase()}</span>
                        <span className="operator-account-identity__text">
                          <strong>{account.email}</strong>
                          <small className="account-identity-meta">
                            {isAdmin ? <span className="account-role-pill account-role-pill--admin">管理员</span> : null}
                            <code className="account-subject-chip">#{account.sub2apiUserId}</code>
                          </small>
                        </span>
                      </span>
                    </td>
                    <td><AccountCapabilityTags account={account} /></td>
                    <td>
                      <span className="account-balance-merged">
                        <AccountFact source={account.wallet}>{(wallet) => <strong>{formatUsdMicros(wallet.usdMicros)}</strong>}</AccountFact>
                        <AccountFact source={account.usage}>{(usage) => <small>累计 {formatUsdMicros(usage.totalActualCostUsdMicros)} · 今日 {formatUsdMicros(usage.todayActualCostUsdMicros)}</small>}</AccountFact>
                      </span>
                    </td>
                    <td>
                      <span className="account-status-stack">
                        <Badge color={statusTone(account.status)}>
                          <span aria-hidden="true" className={`ss-dot ${active ? "ss-dot--ok" : "ss-dot--bad"}`} />
                          {statusLabel(account.status)}
                        </Badge>
                      </span>
                    </td>
                    <td className="table-actions">
                      <Button
                        aria-label={`查看 ${account.email} 详情与权限`}
                        onClick={(event) => { event.stopPropagation(); openInspector(account); }}
                        size="sm"
                        variant="ghost"
                      >
                        详情<ChevronRight aria-hidden size={14} />
                      </Button>
                    </td>
                  </tr>
                );
              })}</tbody></table></div>
              <div className="operator-account-mobile-list">{filteredItems.map((account) => (
                <OperatorAccountMobileCard
                  account={account}
                  key={account.accountId}
                  onInspect={openInspector}
                  selected={inspectedAccount?.accountId === account.accountId}
                />
              ))}</div>
            </>}
          </>;
        }}
      </SourceState>
          <Pagination current={accountController.page} label="账号分页" onChange={(page) => void accountController.changePage(page)} pages={accountController.pages} />
          {cloudIdentity ? null : <p className="inline-notice">新用户开通请使用 Cloud 控制台。</p>}
        </section>
        <AccountInspectorDrawer account={inspectedAccount} controller={controller} onClose={() => setInspectedAccountId("")} onOpenWallet={openWallet} />
      </div>
      {cloudIdentity ? <ProvisionAccountModal controller={controller} onClose={() => setDialog("")} open={dialog === "provision"} /> : null}
      <WalletAdjustmentModal account={dialog === "wallet" ? walletAccount : null} controller={controller} onClose={() => { setDialog(""); setWalletAccountId(""); }} />
    </>
  );
}

function ReviewDetails({ review }: { review: OperatorReconciliationItemDTO }) {
  return (
    <div>
      <section className="data-section"><h2>计费与操作</h2><dl className="data-list">
        <div><dt>账户</dt><dd>{review.accountId || "暂不可用"}</dd></div>
        <div><dt>计费操作</dt><dd>{review.billingOperationId || "暂不可用"}</dd></div>
        <div><dt>阶段</dt><dd>{review.phase || "暂不可用"}</dd></div>
        <div><dt>错误码</dt><dd>{review.errorCode || "暂不可用"}</dd></div>
        <div><dt>操作引用</dt><dd>{review.operationRef || "暂不可用"}</dd></div>
      </dl></section>
      <section className="data-section"><h2>账本回执</h2><dl className="data-list">
        <div><dt>回执引用</dt><dd>{review.receiptRef || "暂不可用"}</dd></div>
        <div><dt>复核状态</dt><dd>{review.status || "暂不可用"}</dd></div>
      </dl></section>
    </div>
  );
}

interface LaunchRecoveryIntent {
  input: WorkspaceLaunchRecoveryRequest;
  key: string;
  operationId: string;
  userId: string;
  csrfToken: string;
}

function LaunchResultCheck({ controller, operationId, intent }: { controller: ConsoleController; operationId: string; intent: RefObject<LaunchRecoveryIntent | null> }) {
  const [recovery, setRecovery] = useState<WorkspaceLaunchRecoveryDTO | null>(null);
  const [reason, setReason] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState("");
  const [message, setMessage] = useState("");
  const [pendingIntent, setPendingIntent] = useState(false);
  const generation = useRef(0);
  const csrfToken = controller.session?.csrfToken || "";
  const userId = controller.session?.user.id || "";

  const read = async () => {
    const request = ++generation.current;
    setLoading(true);
    setIssue("");
    setMessage("");
    try {
      const result = await getWorkspaceLaunchRecovery(operationId);
      if (request !== generation.current) return;
      setRecovery(result);
      if (intent.current && intent.current.input.launchVersion !== result.launchVersion) {
        intent.current = null;
        setPendingIntent(false);
      }
    } catch {
      if (request === generation.current) {
        setRecovery(null);
        setIssue("暂时无法读取允许动作，请刷新。");
      }
    } finally {
      if (request === generation.current) setLoading(false);
    }
  };

  useEffect(() => {
    if (intent.current?.operationId !== operationId || intent.current.userId !== userId || intent.current.csrfToken !== csrfToken) intent.current = null;
    setPendingIntent(Boolean(intent.current));
    setBusy(false);
    setReason(intent.current?.input.reason || "");
    setRecovery(null);
    void read();
    return () => { generation.current += 1; };
  }, [operationId, csrfToken, userId]);

  const submitRecovery = async (action: WorkspaceLaunchRecoveryRequest["action"]) => {
    if (busy || loading || !csrfToken || !recovery?.allowedActions.includes(action) || !reason.trim() || intent.current && intent.current.input.action !== action) return;
    const request = ++generation.current;
    intent.current ||= { input: { action, launchVersion: recovery.launchVersion, reason: reason.trim() }, key: `${action === "check_result" ? "check" : "close"}-${crypto.randomUUID()}`, operationId, userId, csrfToken };
    setBusy(true);
    setPendingIntent(true);
    setIssue("");
    setMessage("");
    try {
      const result = await recoverWorkspaceLaunch(operationId, intent.current.input, csrfToken, intent.current.key);
      if (request !== generation.current) return;
      setRecovery(result);
      intent.current = null;
      setPendingIntent(false);
      setMessage(result.closeout ? "" : result.status === "manual_review" ? "结果尚未确认，订单已保留。可稍后重新核对。"
        : result.status === "succeeded" ? "已确认原订单完成开通。"
        : result.status === "pending" ? "已确认当前阶段，后台将继续处理原订单。"
        : "原订单状态已更新，请查看最新结果。");
      await controller.refreshCurrentPage();
    } catch (error) {
      if (request !== generation.current) return;
      if (error && typeof error === "object" && "status" in error && error.status === 409) {
        setRecovery(null);
        intent.current = null;
        setPendingIntent(false);
        setIssue("订单状态已变化，请刷新后重新核对。");
      } else if (error && typeof error === "object" && "status" in error && (error.status === 401 || error.status === 403)) {
        setRecovery(null);
        intent.current = null;
        setPendingIntent(false);
        setIssue("当前账号无权处理此订单，请使用有权限的管理员账号。");
      } else {
        setIssue(action === "close_unfulfilled" ? "结案请求结果暂未收到。可以重试本次请求，请勿另行退款。" : "核对结果暂未收到。可以重试本次核对，订单不会被重复创建。");
      }
    } finally {
      if (request === generation.current) setBusy(false);
    }
  };

  const closeout = recovery?.closeout ? presentWorkspaceLaunchCloseout(recovery.closeout, recovery.status) : null;

  return <section className="data-section" aria-label="核对原开通结果">
    <div className="panel-title"><h2>核对原开通结果</h2><Button busy={loading} disabled={busy} onClick={() => void read()} size="sm" variant="outline">刷新允许动作</Button></div>
    <p>重新读取原订单的实际结果。不会重复购买或增加扣款。</p>
    {closeout ? <div role="status"><strong>{closeout.title}</strong><p>{closeout.summary}</p></div> : recovery ? <p>当前状态：{statusLabel(recovery.status)}</p> : null}
    {issue ? <p role="alert">{issue}</p> : null}
    {message ? <p role="status">{message}</p> : null}
    {recovery?.allowedActions.length ? <>
      <Field disabled={busy || pendingIntent} label="核对原因" onChange={(event) => setReason(event.target.value)} value={reason} />
      {!cloudIdentity && recovery.allowedActions.includes("close_unfulfilled") ? <p>结束未完成的开通后，将确认停止服务并将应退费用退回原账户余额；若工作空间已就绪，将继续完成原开通，不执行退款。</p> : null}
      <div className="page-actions">
        {!cloudIdentity && recovery.allowedActions.includes("check_result") ? <Button busy={busy && intent.current?.input.action === "check_result"} color="primary" disabled={busy || loading || !reason.trim() || pendingIntent && intent.current?.input.action !== "check_result"} onClick={() => void submitRecovery("check_result")}>重新核对原开通结果</Button> : null}
        {!cloudIdentity && recovery.allowedActions.includes("close_unfulfilled") ? <Button busy={busy && intent.current?.input.action === "close_unfulfilled"} color="danger" disabled={busy || loading || !reason.trim() || pendingIntent && intent.current?.input.action !== "close_unfulfilled"} onClick={() => void submitRecovery("close_unfulfilled")}>结束未完成开通并退款</Button> : null}
      </div>
    </> : !loading && recovery ? <p>当前没有服务端允许的核对动作。</p> : null}
  </section>;
}

function ReviewModal({ controller, onClose, review, intent }: { controller: ConsoleController; onClose: () => void; review: OperatorReconciliationItemDTO | null; intent: RefObject<LaunchRecoveryIntent | null> }) {
  return (
    <Modal
      className="modal"
      description="服务端复核队列项目与证据，操作由 Control Plane 权威决定。"
      footer={<Button onClick={onClose} variant="outline">关闭</Button>}
      onClose={onClose}
      open={Boolean(review)}
      title="复核详情"
    >
      {review ? <div data-slide="A-REC-02">{review.allowedActions.includes("resume_workspace_launch") ? <LaunchResultCheck controller={controller} intent={intent} key={review.billingOperationId} operationId={review.billingOperationId} /> : <div className="inline-notice"><ShieldAlert aria-hidden size={17} /><span>当前项目仅展示复核证据。</span></div>}<ReviewDetails review={review} /></div> : null}
    </Modal>
  );
}

function ReconciliationPage({ controller }: { controller: ConsoleController }) {
  const [selectedReview, setSelectedReview] = useState<OperatorReconciliationItemDTO | null>(null);
  const recoveryIntent = useRef<LaunchRecoveryIntent | null>(null);
  const reviews = sourceData(controller.sources.operatorReconciliation.value)?.items || [];
  return (
    <section className="panel" data-slide="A-REC-01 A-REC-02">
      <div className="panel-title"><div><h2>计费复核</h2></div><span>服务端队列</span></div>
      <SourceState empty={reviews.length === 0} emptyTitle="暂无待复核项目" error={controller.sources.operatorReconciliation.error} loading={controller.sources.operatorReconciliation.loading} onRetry={() => void controller.refreshCurrentPage()} source={controller.sources.operatorReconciliation.value} unavailableTitle="复核数据暂不可用">
        {(data) => <div className="review-list">{data.items.map((review) => {
          return <article className="review-card" key={review.id}>
            <header className="review-card__head">
              <div className="review-card__identity"><strong>{review.accountId || "账户暂不可用"}</strong><small>{review.resourceType} · {review.billingOperationId || "计费操作暂不可用"}</small></div>
              <Badge color={statusTone(review.status)}>{statusLabel(review.status)}</Badge>
            </header>
            <dl className="review-card__facts">
              <div><dt>阶段</dt><dd><code>{review.phase || "暂不可用"}</code></dd></div>
              <div><dt>错误码</dt><dd><code>{review.errorCode || "—"}</code></dd></div>
              <div><dt>允许动作</dt><dd><code>{review.allowedActions.length ? review.allowedActions.join(", ") : "无"}</code></dd></div>
              <div><dt>操作引用</dt><dd><code>{review.operationRef || "暂不可用"}</code></dd></div>
              <div><dt>回执引用</dt><dd><code>{review.receiptRef || "暂不可用"}</code></dd></div>
            </dl>
            <footer className="review-card__actions"><Button onClick={() => setSelectedReview(review)} size="sm" variant="outline">查看证据</Button></footer>
          </article>;
        })}</div>}
      </SourceState>
      <ReviewModal controller={controller} intent={recoveryIntent} onClose={() => setSelectedReview(null)} review={selectedReview} />
    </section>
  );
}

function ResourceObservation({ resource, showReadTime = false }: { resource: OperatorResourceDTO; showReadTime?: boolean }) {
  const reason = sourceData(resource.providerErrorCode);
  return <div className="operator-resource-observation">
    <SourceOwner source={resource.status} />
    <SourceValue source={resource.status}>{fabricObservationLabel}</SourceValue>
    {resource.status.available && reason ? <small>{observationReason(reason)}</small> : null}
    {showReadTime ? <small>读取时间：{resource.lastReadAt.available ? formatDate(resource.lastReadAt.data, true) : "暂不可用"}</small> : null}
  </div>;
}

function SourceOwner({ source }: { source: SourceEnvelope<unknown> | undefined }) {
  return source ? <small>来源：{source.source}</small> : null;
}

function ResourceReadback({ item }: { item: OperatorWorkspaceDTO }) {
  const readback = item.resourcesReadback;
  if (!readback) {
    return item.workspace.source === "workspace"
      ? <div className="source-value source-value--unavailable">资源读回暂不可用：Workspace 响应缺少资源读回</div>
      : null;
  }
  if (readback.available === false) {
    return <div className="source-value source-value--unavailable"><strong>资源读回暂不可用：{observationReason(readback.reasonCode)}</strong><SourceOwner source={readback} /></div>;
  }
  return <div className="source-value operator-resources-readback">
    <SourceOwner source={readback} />
    <span>Fabric 观测：{fabricObservationLabel(readback.data.outcome)}</span>
    <small>资源版本：{readback.data.resourceVersion || "暂不可用"}</small>
    <small>观测时间：{readback.data.observedAt ? formatDate(readback.data.observedAt, true) : "暂不可用"}</small>
    {readback.data.absenceConfirmed ? <strong>Fabric 已确认资源不存在</strong> : null}
  </div>;
}

function fabricObservationLabel(state: string) {
  return ({ confirmed: "已确认", rejected: "已拒绝", not_dispatched: "尚未下发" } as Record<string, string>)[state] || observationLabel(state);
}

function WorkspaceResourceObservations({ item }: { item: OperatorWorkspaceDTO }) {
  const unavailable = resourcesUnavailable(item);
  return <div className="operator-workspace-observations">
    <ResourceReadback item={item} />
    {!unavailable ? item.resources.length ? item.resources.map((resource, index) => <div key={index}>
      <strong>{resource.resourceType.available ? resourceKindLabel(resource.resourceType.data) : "资源类型暂不可用"}</strong>
      <ResourceObservation resource={resource} showReadTime />
    </div>) : <EmptyResourceFacts item={item} /> : null}
  </div>;
}

function ResourceRow({ resource }: { resource: OperatorResourceDTO }) {
  return (
    <tr className="resource-detail-row">
      <td colSpan={13}>
        <div className="resource-detail-card">
          <div className="resource-detail-card__head">
            <span className="resource-detail-card__identity">
              <SourceValue source={resource.workspace}>{(data) => <strong>{data.name || data.id}</strong>}</SourceValue>
              <small><SourceValue source={resource.ownerUser}>{(data) => data.email}</SourceValue> · <SourceValue source={resource.ownerAccount}>{(data) => data.id}</SourceValue></small>
            </span>
            <span className="resource-detail-card__type">
              <SourceValue source={resource.resourceType}>{(data) => ({ compute: "计算资源", storage: "存储", attachment: "挂载", runtime: "运行环境" } as Record<string, string>)[data] || data}</SourceValue>
              <SourceValue source={resource.packageOrSpec}>{(data) => data}</SourceValue>
            </span>
            <ResourceObservation resource={resource} showReadTime />
          </div>
          <dl className="resource-detail-card__facts">
            <div><dt>provider ID</dt><dd><SourceValue source={resource.providerId}>{(data) => data}</SourceValue></dd></div>
            <div><dt>可用区</dt><dd><SourceValue source={resource.zone}>{(data) => data}</SourceValue></dd></div>
            <div><dt>创建时间</dt><dd><SourceValue source={resource.createdAt}>{(data) => formatDate(data, true)}</SourceValue></dd></div>
            <div><dt>到期时间</dt><dd><SourceValue source={resource.expiresAt}>{(data) => formatDate(data, true)}</SourceValue></dd></div>
            <div><dt>操作引用</dt><dd><SourceValue source={resource.operationRef}>{(data) => data}</SourceValue></dd></div>
            <div><dt>回执引用</dt><dd><SourceValue source={resource.receiptRef}>{(data) => data}</SourceValue></dd></div>
          </dl>
        </div>
      </td>
    </tr>
  );
}

function OperatorResourceMobileCard({ resource }: { resource: OperatorResourceDTO }) {
  const workspace = sourceData(resource.workspace);
  return (
    <article className="operator-object-card operator-resource-mobile-card">
      <header className="operator-object-card__header">
        <span><strong>{workspace?.name || workspace?.id || "Workspace 暂不可用"}</strong><small>{workspace?.id || "身份暂不可用"}</small></span>
        <ResourceObservation resource={resource} />
      </header>
      <dl className="operator-object-card__facts">
        <div><dt>资源类型</dt><dd><SourceValue source={resource.resourceType}>{(data) => data}</SourceValue></dd></div>
        <div><dt>套餐 / 规格</dt><dd><SourceValue source={resource.packageOrSpec}>{(data) => data}</SourceValue></dd></div>
        <div className="operator-object-card__wide"><dt>provider ID</dt><dd><SourceValue source={resource.providerId}>{(data) => data}</SourceValue></dd></div>
        <div><dt>owner Account</dt><dd><SourceValue source={resource.ownerAccount}>{(data) => data.id}</SourceValue></dd></div>
        <div><dt>owner User</dt><dd><SourceValue source={resource.ownerUser}>{(data) => data.email}</SourceValue></dd></div>
        <div><dt>Zone</dt><dd><SourceValue source={resource.zone}>{(data) => data}</SourceValue></dd></div>
        <div><dt>创建时间</dt><dd><SourceValue source={resource.createdAt}>{(data) => formatDate(data, true)}</SourceValue></dd></div>
        <div><dt>到期时间</dt><dd><SourceValue source={resource.expiresAt}>{(data) => formatDate(data, true)}</SourceValue></dd></div>
        <div><dt>最近 provider 读回</dt><dd><SourceValue source={resource.lastReadAt}>{(data) => formatDate(data, true)}</SourceValue></dd></div>
        <div><dt>operation reference</dt><dd><SourceValue source={resource.operationRef}>{(data) => data}</SourceValue></dd></div>
        <div className="operator-object-card__wide"><dt>Receipt reference</dt><dd><SourceValue source={resource.receiptRef}>{(data) => data}</SourceValue></dd></div>
      </dl>
    </article>
  );
}

function ResourceDetail({ controller }: { controller: OperatorResourceReadController }) {
  const selected = controller.selectedWorkspaceId;
  if (!selected) return <section className="panel" data-slide="A-RES-02"><div className="panel-title"><div><h2>资源详情</h2></div></div><div className="empty-panel">请选择 Workspace 查看资源详情。</div></section>;
  return (
    <section className="panel" data-slide="A-RES-02">
      <div className="panel-title"><div><h2>资源详情</h2></div><span>{selected}</span></div>
      <SourceState error={controller.detail.error} loading={controller.detail.loading} onRetry={() => void controller.refreshWorkspace(selected)} source={controller.detail.value} unavailableTitle="资源详情暂不可用">
        {(detail) => <>
          <dl className="data-list">
            <div><dt>归属</dt><dd><OwnershipSources item={detail} /></dd></div>
            {detail.workspace.source === "workspace" ? <>
              <div><dt>Account 映射</dt><dd><SourceValue source={detail.ownerAccount}>{(data) => data.id}</SourceValue></dd></div>
              <div><dt>User 映射</dt><dd><SourceValue source={detail.ownerUser}>{(data) => `${data.email} · ${data.id}`}</SourceValue></dd></div>
            </> : null}
            <div><dt>Workspace</dt><dd><SourceValue source={detail.workspace}>{(data) => `${data.name || data.id} · ${data.id}`}</SourceValue></dd></div>
            <WorkspaceOwnerFacts item={detail} />
            <div><dt>Ledger Receipt</dt><dd><SourceValue source={detail.receipt}>{(data) => data.receiptId}</SourceValue></dd></div>
            <div><dt>Workspace Key 累计实际费用</dt><dd><SourceValue source={detail.workspaceKeyUsage}>{(data) => `${formatUsdMicros(data.totalActualCostUsdMicros)} · ${data.keyId}`}</SourceValue></dd></div>
          </dl>
          <ResourceReadback item={detail} />
          {resourcesUnavailable(detail) ? null
            : detail.resources.length ? <>
              <div aria-label="Workspace 底层资源列表" className="table-wrap operator-resource-detail-table" role="region" tabIndex={0}><table className="ops-table resource-detail-cards"><thead><tr className="sr-only"><th>归属账户</th><th>归属用户</th><th>Workspace</th><th>资源类型</th><th>套餐 / 规格</th><th>provider ID</th><th>Zone</th><th>实时状态</th><th>创建时间</th><th>到期时间</th><th>最近 provider 读回</th><th>operation reference</th><th>Receipt reference</th></tr></thead><tbody>{detail.resources.map((resource, index) => <ResourceRow key={`${index}-${resource.providerId.source}-${resource.operationRef.source}`} resource={resource} />)}</tbody></table></div>
              <div className="operator-resource-mobile-list">{detail.resources.map((resource, index) => <OperatorResourceMobileCard key={`${index}-${resource.providerId.source}-${resource.operationRef.source}`} resource={resource} />)}</div>
            </> : <div className="empty-panel"><EmptyResourceFacts item={detail} /></div>}
        </>}
      </SourceState>
    </section>
  );
}

function OperatorWorkspaceMobileCard({ controller, item }: { controller: OperatorResourceReadController; item: OperatorWorkspaceDTO }) {
  const workspace = sourceData(item.workspace);
  const id = workspace?.id || "";
  return (
    <article className="operator-object-card operator-workspace-mobile-card">
      <header className="operator-object-card__header">
        <span><strong>{workspace?.name || id || "Workspace 暂不可用"}</strong><small>{id || "身份暂不可用"}</small></span>
        <SourceValue source={item.workspace}>{(value) => presentWorkspaceLifecycle(value.state).label}</SourceValue>
      </header>
      <dl className="operator-object-card__facts">
        <div className="operator-object-card__wide"><dt>归属</dt><dd><OwnershipSources item={item} /></dd></div>
        <div><dt>套餐 / 月费</dt><dd><SourceValue source={item.workspace}>{(value) => `${value.packageId?.toUpperCase() || "暂不可用"} · ${value.totalUsdMicros === undefined ? "暂不可用" : formatUsdMicros(value.totalUsdMicros)}`}</SourceValue></dd></div>
        <div><dt>权益截止</dt><dd><SourceValue source={item.workspace}>{(value) => value.paidThrough ? formatDate(value.paidThrough) : "暂不可用"}</SourceValue></dd></div>
        <WorkspaceOwnerFacts item={item} />
        <div className="operator-object-card__wide"><dt>Fabric 观测</dt><dd><WorkspaceResourceObservations item={item} /></dd></div>
        <div className="operator-object-card__wide"><dt>Key 累计实际费用</dt><dd><SourceValue source={item.workspaceKeyUsage}>{(value) => formatUsdMicros(value.totalActualCostUsdMicros)}</SourceValue></dd></div>
      </dl>
      <div className="operator-card-actions">
        {workspace?.url ? <a className="operator-object-link" href={workspace.url} rel="noreferrer" target="_blank">打开 Workspace<ExternalLink aria-hidden size={14} /></a> : <span className="account-read-only">URL 暂不可用</span>}
        <Button disabled={!id} onClick={() => id && void controller.selectWorkspace(id)} size="sm" variant="outline">查看资源</Button>
      </div>
    </article>
  );
}

function ResourcesPage({ controller }: { controller: OperatorResourceReadController }) {
  const workspaces = sourceData(controller.workspaces.value)?.items || [];
  return (
    <section className="admin-dashboard" data-slide="A-RES-01 A-RES-02">
      <section className="panel">
        <div className="panel-title"><div><h2>Workspace 资源列表</h2></div><span>当前页资源状态</span></div>
        <SourceState empty={workspaces.length === 0} emptyTitle="暂无 Workspace" error={controller.workspaces.error} loading={controller.workspaces.loading} onRetry={() => void controller.refresh()} source={controller.workspaces.value} unavailableTitle="Workspace 资源暂不可用">
          {(data) => <>
            <div aria-label="Workspace 资源列表" className="table-wrap operator-workspace-table" role="region" tabIndex={0}><table className="ops-table"><thead><tr><th>Workspace</th><th>归属</th><th>套餐 / 月费</th><th>权益截止</th><th>Workspace 状态</th><th>Fabric 观测 / 读取时间</th><th>入口</th><th>Key 累计费用</th><th>操作</th></tr></thead><tbody>{data.items.map((item, index) => {
              const workspace = sourceData(item.workspace);
              const id = workspace?.id || "";
              const state = sourceData(item.workspace)?.state;
              return <tr key={id || index} data-state={state || ""}><td><SourceValue source={item.workspace}>{(value) => `${value.name || value.id} · ${value.id}`}</SourceValue></td><td><OwnershipSources item={item} /></td><td><SourceValue source={item.workspace}>{(value) => `${value.packageId?.toUpperCase() || "暂不可用"} · ${value.totalUsdMicros === undefined ? "暂不可用" : formatUsdMicros(value.totalUsdMicros)}`}</SourceValue></td><td><SourceValue source={item.workspace}>{(value) => value.paidThrough ? formatDate(value.paidThrough) : "暂不可用"}</SourceValue></td><td><dl className="operator-workspace-owner-facts"><WorkspaceOwnerFacts item={item} /></dl></td><td><WorkspaceResourceObservations item={item} /></td><td>{workspace?.url ? <a href={workspace.url} rel="noreferrer" target="_blank">打开<ExternalLink aria-hidden size={14} /></a> : "暂不可用"}</td><td><SourceValue source={item.workspaceKeyUsage}>{(value) => formatUsdMicros(value.totalActualCostUsdMicros)}</SourceValue></td><td><Button disabled={!id} onClick={() => id && void controller.selectWorkspace(id)} size="sm" variant="outline">查看资源</Button></td></tr>;
            })}</tbody></table></div>
            <div className="operator-workspace-mobile-list">{data.items.map((item, index) => <OperatorWorkspaceMobileCard controller={controller} item={item} key={sourceData(item.workspace)?.id || index} />)}</div>
          </>}
        </SourceState>
        <Pagination current={controller.page} label="Workspace 分页" onChange={(page) => void controller.changePage(page)} pages={controller.pages} />
      </section>
      <ResourceDetail controller={controller} />
    </section>
  );
}

const healthServices = [
  { key: "controlPlane", name: "Control Plane", icon: Activity },
  { key: "gateway", name: "API 服务 / Sub2API Gateway", icon: Server },
  { key: "fabric", name: "Fabric 资源服务", icon: Database },
  { key: "runtime", name: "Workspace Runtime 服务", icon: Activity },
  { key: "ledger", name: "Ledger 账单记录", icon: CircleDollarSign }
] as const;

function HealthDiagnostics({ service, serviceKey }: { service: SourceEnvelope<ReadinessFact>; serviceKey: string }) {
  if (service.status === "unavailable") return <span>{observationReason(service.reasonCode)}</span>;
  if (serviceKey === "fabric") {
    const facts = service.data as OperatorFabricHealthDTO;
    const targetsMatch = facts.releaseReady === true && (facts.workspaceImageStatus === undefined || facts.workspaceImageStatus === "installed_target_matches");
    const verifiedVersionDifference = facts.workspaceImageStatus === "workspace_targets_verified" && facts.cloudImagesReady;
    return <div className="operator-health-diagnostics">
      <span>安装镜像目标一致性：<Badge color={targetsMatch ? "success" : verifiedVersionDifference ? "info" : "warning"}>{targetsMatch ? "全部一致" : verifiedVersionDifference ? "存量版本不同" : "尚未核验完成"}</Badge></span>
      <small>Cloud 运行镜像与安装目标：{facts.cloudImagesReady ? "一致" : "尚未确认一致"}</small>
      {facts.workspaceImageStatus ? <small>{workspaceImageStatusMessage(facts.workspaceImageStatus)}</small> : null}
      {facts.workspaceImageUnverifiedWorkspaceIds?.length ? <small>未核验 Workspace：{facts.workspaceImageUnverifiedWorkspaceIds.join("、")}</small> : null}
      {facts.failedChecks?.filter((check) => check !== "workspace_image_id" || !facts.workspaceImageStatus).map((check) => <small key={check}>{observationReason(check)}</small>)}
      <small>安装默认与存量目标分别管理；更改默认不会自动升级存量 Workspace。</small>
    </div>;
  }
  if (serviceKey === "runtime") {
    const facts = service.data as OperatorRuntimeHealthDTO;
    return <div className="operator-health-diagnostics">
      <span>业务 Workspace {formatCount(facts.businessTotal)} · 实际运行对象 {formatCount(facts.observedTotal)}</span>
      <small>运行正常 {formatCount(facts.runningCount)} · 正常暂停 {formatCount(facts.suspendedCount)} · 处理中 {formatCount(facts.pendingCount)}</small>
      <small>需处理 {formatCount(facts.attentionCount)} · 无当前业务记录对象 {formatCount(facts.unmatchedCount)}</small>
    </div>;
  }
  return <span>{service.data.ready === true ? "服务就绪" : "服务就绪检查未通过"}</span>;
}

function healthObservedAt(service: SourceEnvelope<ReadinessFact>, key: string): string | undefined {
  if (!service.available) return undefined;
  return key === "runtime" ? (service.data as OperatorRuntimeHealthDTO).observedAt : service.data.generatedAt || service.data.updatedAt;
}

function OperatorHealthMobileCard({ controller, name, service, serviceKey, onRuntimeDetails, icon: Icon }: {
  controller: ConsoleController;
  name: string;
  service: SourceEnvelope<ReadinessFact>;
  serviceKey: string;
  onRuntimeDetails: () => void;
  icon: typeof Activity;
}) {
  const state = healthStatus(service, serviceKey);
  return (
    <article className="operator-object-card operator-health-mobile-card">
      <header className="operator-object-card__header">
        <span className="resource-type"><Icon aria-hidden size={16} /><strong>{name}</strong></span>
        <Badge color={state.tone}>{state.label}</Badge>
      </header>
      <dl className="operator-object-card__facts">
        <div><dt>来源观测时间</dt><dd>{formatDate(healthObservedAt(service, serviceKey), true)}</dd></div>
        <div><dt>Console 读回时间</dt><dd>{service.fetchedAt ? formatDate(service.fetchedAt, true) : "暂不可用"}</dd></div>
        <div className="operator-object-card__wide"><dt>诊断</dt><dd><HealthDiagnostics service={service} serviceKey={serviceKey} /></dd></div>
      </dl>
      <div className="operator-card-actions">{serviceKey === "runtime" ? <Button onClick={onRuntimeDetails} size="sm" variant="outline">查看 Runtime 明细</Button> : null}<Button aria-label={`刷新 ${name}`} onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={15} />刷新</Button></div>
    </article>
  );
}

function SystemPage({ controller }: { controller: ConsoleController }) {
  const [runtimeDetailsOpen, setRuntimeDetailsOpen] = useState(false);
  const healthSource = controller.sources.operatorHealth.value;
  const summary = overallHealth(healthSource);
  return (
    <section className="admin-dashboard" data-slide="A-SYS-01">
      <section className="account-band">
        <div className="account-band-copy"><h2>系统状态</h2></div>
        <div className="band-metrics"><Metric label="总体状态" note="按最差真实状态" value={summary.label} /><Metric label="服务域" note="固定展示" value="5" /><Metric label="Console 读回" note="最近读取" value={healthSource?.fetchedAt ? formatDate(healthSource.fetchedAt, true) : "暂不可用"} /></div>
      </section>
      <section className="panel">
        <div className="panel-title"><div className="panel-title__inline"><h2>服务健康</h2><Badge color={summary.tone}><span aria-hidden="true" className={`ss-dot ${summary.tone === "success" ? "ss-dot--ok" : summary.tone === "danger" ? "ss-dot--bad" : "ss-dot--warn"}`} />{summary.label}</Badge></div><Button onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={16} />刷新</Button></div>
        <SourceState error={controller.sources.operatorHealth.error} loading={controller.sources.operatorHealth.loading} onRetry={() => void controller.refreshCurrentPage()} source={healthSource} unavailableTitle="系统状态暂不可用">
          {(health) => <>
            <div className="operator-health-table">
              <table className="ops-table health-cards">
                <thead><tr className="sr-only"><th>服务</th><th>状态</th><th>来源观测时间</th><th>Console 读回时间</th><th>诊断</th><th>操作</th></tr></thead><tbody>{healthServices.map(({ key, name, icon: Icon }) => {
              const service = health[key];
              const state = healthStatus(service, key);
              const stateKey = state.tone === "success" ? "active" : state.tone === "danger" ? "disabled" : "manual_review";
              return <tr key={key} data-state={stateKey} className="health-card-row"><td colSpan={6}>
                <div className="health-card">
                  <span className={`health-card__icon health-card__icon--${state.tone}`} aria-hidden="true"><Icon size={20} /></span>
                  <div className="health-card__main">
                    <div className="health-card__head"><strong>{name}</strong><Badge color={state.tone}>{state.label}</Badge></div>
                    <HealthDiagnostics service={service} serviceKey={key} />
                  </div>
                  <div className="health-card__meta">
                    <small>来源观测：{formatDate(healthObservedAt(service, key), true)}</small>
                    <small>Console 读回：{service.fetchedAt ? formatDate(service.fetchedAt, true) : "暂不可用"}</small>
                  </div>
                  <div className="operator-card-actions">
                    {key === "runtime" ? <Button onClick={() => setRuntimeDetailsOpen(true)} size="sm" variant="outline">查看 Runtime 明细</Button> : null}
                    <Button aria-label={`刷新 ${name}`} onClick={() => void controller.refreshCurrentPage()} size="sm" variant="outline"><RefreshCw aria-hidden size={15} />刷新</Button>
                  </div>
                </div>
              </td></tr>;
            })}</tbody></table></div>
            <div className="operator-health-mobile-list">{healthServices.map(({ key, name, icon }) => <OperatorHealthMobileCard controller={controller} icon={icon} key={key} name={name} onRuntimeDetails={() => setRuntimeDetailsOpen(true)} service={health[key]} serviceKey={key} />)}</div>
          </>}
        </SourceState>
      </section>
      {cloudIdentity && controller.session?.isOperator ? <AdminRuntimeManager key={`${controller.session.user.id}:${controller.session.csrfToken}`} csrfToken={controller.session.csrfToken} /> : null}
      {runtimeDetailsOpen && controller.session ? <OperatorRuntimeObservations key={`${controller.session.user.id}:${controller.session.csrfToken}`} onClose={() => setRuntimeDetailsOpen(false)} refreshKey={healthSource?.fetchedAt || ""} sessionIdentity={`${controller.session.user.id}:${controller.session.csrfToken}`} /> : null}
    </section>
  );
}

export function AdminPages({ controller, route }: { controller: ConsoleController; route: AdminConsoleRoute }) {
  switch (route.kind) {
    case "admin.overview":
      return <OverviewPage controller={controller} />;
    case "admin.accounts":
      return <AccountsPage controller={controller} />;
    case "admin.billing":
      return <ReconciliationPage controller={controller} />;
    case "admin.resources":
      return <ResourcesPage controller={controller.operatorResourceRead} />;
    case "admin.system":
      return <SystemPage controller={controller} />;
    case "admin.announcements":
      return <AnnouncementsPage controller={controller} />;
    default:
      return assertNever(route);
  }
}
