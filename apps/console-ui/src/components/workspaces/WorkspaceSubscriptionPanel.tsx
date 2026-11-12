import type { AuthSession } from "../../api/dtos.ts";
import { formatWorkspaceTransactionAmount } from "../../app/workspace-experience-model.ts";
import { useWorkspaceSubscription } from "../../app/use-workspace-subscription.ts";
import { useWorkspaceSubscriptionPlans, type WorkspaceSubscriptionPlans } from "../../app/use-workspace-subscription-plans.ts";
import { formatDate } from "../../console-model.ts";
import { Button } from "../ui/index.ts";
import "./workspace-subscription.css";

type Props = {
  session: AuthSession | null;
  workspace: { id: string; computePlanId?: string; storagePlanId?: string };
  layout?: "detail" | "billing";
  plans?: WorkspaceSubscriptionPlans;
};

const statusLabels = { pending: "待确认", active: "生效中", expired: "已到期", terminated: "已终止" };
const renewalLabels = { manual: "手动续费", automatic: "自动续费" };
const provenanceLabels = { quoted: "已接受报价", legacy_import: "原订阅导入" };

function computeDescription(source: WorkspaceSubscriptionPlans["compute"], id?: string) {
  if (!id) return "套餐信息暂不可用";
  if (source.status === "loading") return "正在读取套餐信息…";
  if (source.status !== "available") return "套餐信息暂不可用";
  const matches = source.items.filter((plan) => plan?.id === id);
  const plan = matches.length === 1 ? matches[0] : undefined;
  if (!plan || typeof plan.name !== "string" || !plan.name.trim() || !Number.isSafeInteger(plan.vcpus) || plan.vcpus < 1 || !Number.isSafeInteger(plan.memoryMiB) || plan.memoryMiB < 1) return "套餐信息暂不可用";
  return `${plan.name} · ${plan.vcpus} 核 / ${plan.memoryMiB / 1024} GiB 内存`;
}

function storageDescription(source: WorkspaceSubscriptionPlans["storage"], id?: string) {
  if (!id) return "套餐信息暂不可用";
  if (source.status === "loading") return "正在读取套餐信息…";
  if (source.status !== "available") return "套餐信息暂不可用";
  const matches = source.items.filter((plan) => plan?.id === id);
  const plan = matches.length === 1 ? matches[0] : undefined;
  if (!plan || typeof plan.name !== "string" || !plan.name.trim() || !Number.isSafeInteger(plan.capacityGiB) || plan.capacityGiB < 1) return "套餐信息暂不可用";
  return `${plan.name} · ${plan.capacityGiB} GiB`;
}

export function WorkspaceSubscriptionPanel({ session, workspace, layout = "detail", plans }: Props) {
  const read = useWorkspaceSubscription(session, workspace.id);
  const ownPlans = useWorkspaceSubscriptionPlans(session, plans === undefined);
  const catalog = plans ?? ownPlans;
  let content;
  if (read.status === "loading") content = <p role="status">正在读取订阅…</p>;
  else if (read.status === "unauthenticated") content = <p role="status">请先登录后查看订阅。</p>;
  else if (read.status === "forbidden") content = <p role="status">仅租户所有者或管理员可查看订阅与费用。</p>;
  else if (read.status === "absent") content = <p role="status">暂无订阅记录。</p>;
  else if (read.status === "unavailable") content = <div role="alert"><p>订阅暂不可用，请重试。</p><Button onClick={read.refresh} size="sm" variant="outline">重试读取订阅</Button></div>;
  else if (read.status === "available") {
    const subscription = read.subscription;
    content = <>
      <dl className={layout === "detail" ? "data-list" : "workspace-primary-facts"}>
        <div><dt>计算方案</dt><dd>{computeDescription(catalog.compute, workspace.computePlanId)}</dd></div>
        <div><dt>存储方案</dt><dd>{storageDescription(catalog.storage, workspace.storagePlanId)}</dd></div>
        <div><dt>月度总价</dt><dd>{subscription.currentMonthlyUSDMicros === undefined ? "待核对" : formatWorkspaceTransactionAmount(subscription.currentMonthlyUSDMicros)}</dd></div>
        <div><dt>计费周期</dt><dd>{formatDate(subscription.currentPeriodStart)} 至 {formatDate(subscription.currentPeriodEnd)}</dd></div>
        <div><dt>订阅状态</dt><dd>{statusLabels[subscription.status]}</dd></div>
        <div><dt>续费方式</dt><dd>{renewalLabels[subscription.renewalMode]}</dd></div>
      </dl>
      {catalog.compute.status === "unavailable" || catalog.storage.status === "unavailable" ? <div className="workspace-actions"><Button onClick={catalog.refresh} size="sm" variant="outline">重试套餐信息</Button></div> : null}
      <details className="workspace-technical-details workspace-subscription-details">
        <summary>订阅技术详情</summary>
        <dl className="data-list">
          <div><dt>计算方案 ID</dt><dd><code>{workspace.computePlanId || "未提供"}</code></dd></div>
          <div><dt>存储方案 ID</dt><dd><code>{workspace.storagePlanId || "未提供"}</code></dd></div>
          <div><dt>价格政策版本</dt><dd><code>{subscription.currentPricePolicyVersionId || "待核对"}</code></dd></div>
          <div><dt>订阅来源</dt><dd>{provenanceLabels[subscription.provenance]}（<code>{subscription.provenance}</code>）</dd></div>
        </dl>
      </details>
    </>;
  }
  return <section aria-label="工作空间订阅" className={layout === "detail" ? "panel workspace-subscription-panel" : "workspace-subscription-panel"}>
    {layout === "detail" ? <div className="panel-title"><h2>订阅与费用</h2>{read.status !== "forbidden" && read.status !== "unauthenticated" ? <Button onClick={read.refresh} size="sm" variant="outline">刷新订阅</Button> : null}</div> : null}
    {content}
    {layout === "billing" && (read.status === "available" || read.status === "absent") ? <div className="workspace-actions"><Button onClick={read.refresh} size="sm" variant="outline">刷新订阅</Button></div> : null}
  </section>;
}
