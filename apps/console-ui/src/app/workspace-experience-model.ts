import type {
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceLaunchCloseoutDTO,
  WorkspaceLaunchResponse,
  WorkspaceRenewalReadDTO,
  WorkspaceRuntimeDTO
} from "../api/dtos.ts";
import { formatUsdMicros } from "../console-model.ts";

export type WorkspaceExperienceTone = "info" | "success" | "warning" | "danger";

type KnownWorkspaceLaunchPresentation = {
  kind: "pending" | "manual_review" | "succeeded" | "failed" | "refunded";
  title: string;
  summary: string;
  tone: WorkspaceExperienceTone;
  canOpenWorkspace: boolean;
};

type UnconfirmedWorkspaceLaunchPresentation = {
  kind: "unconfirmed";
  title: "结果待确认";
  summary: string;
  tone: "warning";
  canOpenWorkspace: false;
  rawValue: string;
};

export type WorkspaceLaunchPresentation =
  | KnownWorkspaceLaunchPresentation
  | UnconfirmedWorkspaceLaunchPresentation;

export function presentWorkspaceLaunchCloseout(closeout: WorkspaceLaunchCloseoutDTO, launchStatus: string) {
  if (closeout.pendingConfirmation) {
    return { title: "结案结果仍在核对", summary: closeout.refundedUsdMicros > 0 ? `已确认退回原账户余额 ${formatUsdMicros(closeout.refundedUsdMicros)}。请稍后查看结案结果，无需重复提交。` : "原订单的结案结果尚未确认。请稍后查看，无需重复提交或另行退款。" };
  }
  switch (closeout.status) {
    case "confirming":
      return { title: "正在核对结案条件", summary: "正在确认原订单和扣款结果，尚未确认退款。可以关闭页面后再查看。" };
    case "closing":
      return { title: "正在结束未完成的开通", summary: "正在结束本次开通，处理完成后会核对并退回应退费用，请勿重复购买。" };
    case "refunding":
      return { title: "退款处理中", summary: "正在将应退费用退回原账户余额，请等待到账确认。" };
    case "recording":
      return { title: "正在记录结案结果", summary: closeout.refundedUsdMicros > 0 ? `已退回原账户余额 ${formatUsdMicros(closeout.refundedUsdMicros)}，正在完成结案记录。` : "正在完成本次开通的结案记录，请稍后查看。" };
    case "closed":
      return { title: "开通未完成，已结案", summary: launchStatus === "refunded" ? `已退回原账户余额 ${formatUsdMicros(closeout.refundedUsdMicros)}。可查看费用记录或重新购买。` : launchStatus === "failed" ? "本次开通未扣款。可查看费用记录或重新购买。" : "正在确认结案后的订单状态，请稍后刷新。" };
    case "fulfilled":
      return { title: launchStatus === "succeeded" ? "工作空间已可使用" : "正在完成开通记录", summary: launchStatus === "succeeded" ? "原订单已完成开通，工作空间可继续使用。" : "工作空间已就绪，正在完成原订单的开通记录。" };
  }
}

export function presentWorkspaceLaunch(
  operation: Pick<WorkspaceLaunchResponse, "status" | "workspaceId" | "closeout">
): WorkspaceLaunchPresentation {
  if (operation.closeout) {
    const presentation = presentWorkspaceLaunchCloseout(operation.closeout, operation.status);
    if (presentation && Number.isSafeInteger(operation.closeout.refundedUsdMicros) && operation.closeout.refundedUsdMicros >= 0) {
      if (operation.closeout.status === "closed" && ["failed", "refunded"].includes(operation.status)) {
        return { ...presentation, kind: operation.status as "failed" | "refunded", tone: "info", canOpenWorkspace: false };
      }
      if (operation.status === "pending" || operation.status === "manual_review") {
        return { ...presentation, kind: operation.status === "pending" ? "pending" : "manual_review", tone: "info", canOpenWorkspace: false };
      }
      if (operation.closeout.status === "fulfilled" && operation.status === "succeeded" && operation.workspaceId?.trim()) {
        return { ...presentation, kind: "succeeded", tone: "success", canOpenWorkspace: true };
      }
    }
    return { kind: "unconfirmed", title: "结果待确认", summary: "正在确认结案后的订单状态，请刷新查看，暂勿重复购买。", tone: "warning", canOpenWorkspace: false, rawValue: operation.status };
  }
  switch (operation.status) {
    case "pending":
      return {
        kind: "pending",
        title: "正在准备工作空间",
        summary: "系统正在后台准备所需资源。可以关闭页面，稍后回来查看，无需重复购买。",
        tone: "info",
        canOpenWorkspace: false
      };
    case "manual_review":
      return {
        kind: "manual_review",
        title: "订单待核验",
        summary: "原订单已保留，请勿重复购买。核验结果后再继续处理，可稍后刷新查看。",
        tone: "warning",
        canOpenWorkspace: false
      };
    case "succeeded":
      if (!operation.workspaceId?.trim()) break;
      return {
        kind: "succeeded",
        title: "工作空间已可使用",
        summary: "工作空间已完成开通，可以继续查看并进入。",
        tone: "success",
        canOpenWorkspace: true
      };
    case "failed":
      return {
        kind: "failed",
        title: "开通失败",
        summary: "工作空间未能完成开通，请查看技术详情并重试。",
        tone: "danger",
        canOpenWorkspace: false
      };
    case "refunded":
      return {
        kind: "refunded",
        title: "开通失败，费用已退回",
        summary: "工作空间未能完成开通，本次费用已退回。",
        tone: "danger",
        canOpenWorkspace: false
      };
    default:
      break;
  }
  return {
    kind: "unconfirmed",
    title: "结果待确认",
    summary: "当前开通结果尚未确认，请刷新状态，暂勿重复购买。",
    tone: "warning",
    canOpenWorkspace: false,
    rawValue: operation.status
  };
}

type KnownWorkspaceLaunchStage =
  | "key"
  | "debit"
  | "ensure_compute_allocation"
  | "storage"
  | "attachment"
  | "secret"
  | "runtime"
  | "activation"
  | "receipt"
  | "succeeded";

export type WorkspaceLaunchStagePresentation =
  | {
      known: true;
      kind: KnownWorkspaceLaunchStage;
      label: string;
    }
  | {
      known: false;
      kind: "unknown";
      label: "等待服务更新处理阶段";
      rawValue: string;
    };

export function presentWorkspaceLaunchStage(stage: string): WorkspaceLaunchStagePresentation {
  switch (stage) {
    case "key":
      return { known: true, kind: "key", label: "准备访问凭据" };
    case "debit":
      return { known: true, kind: "debit", label: "确认费用" };
    case "ensure_compute_allocation":
      return { known: true, kind: "ensure_compute_allocation", label: "准备计算资源" };
    case "storage":
      return { known: true, kind: "storage", label: "准备存储空间" };
    case "attachment":
      return { known: true, kind: "attachment", label: "连接存储空间" };
    case "secret":
      return { known: true, kind: "secret", label: "配置登录凭据" };
    case "runtime":
      return { known: true, kind: "runtime", label: "启动工作空间" };
    case "activation":
      return { known: true, kind: "activation", label: "确认工作空间可用" };
    case "receipt":
      return { known: true, kind: "receipt", label: "记录开通结果" };
    case "succeeded":
      return { known: true, kind: "succeeded", label: "开通完成" };
    default:
      return {
        known: false,
        kind: "unknown",
        label: "等待服务更新处理阶段",
        rawValue: stage
      };
  }
}

export type WorkspaceRuntimePresentation =
  | {
      kind: "ready" | "unready" | "not_found" | "destroyed";
      label: string;
      description: string;
      canOpen: boolean;
      url: string | null;
    }
  | {
      kind: "unconfirmed";
      label: "入口待确认" | "状态待确认";
      description: string;
      canOpen: false;
      url: null;
      rawValue?: string;
    };

export function presentWorkspaceRuntime(runtime: WorkspaceRuntimeDTO): WorkspaceRuntimePresentation {
  switch (runtime.status) {
    case "running":
      if (runtime.ready !== true) {
        return {
          kind: "unready",
          label: "正在准备",
          description: "运行环境尚未就绪，请稍后刷新。",
          canOpen: false,
          url: null
        };
      }
      if (runtime.url === undefined || runtime.url === "") {
        return {
          kind: "unconfirmed",
          label: "入口待确认",
          description: "运行环境已就绪，但访问入口尚未确认，请刷新状态。",
          canOpen: false,
          url: null
        };
      }
      return {
        kind: "ready",
        label: "可使用",
        description: "工作空间已就绪，可以打开。",
        canOpen: true,
        url: runtime.url
      };
    case "unready":
      return {
        kind: "unready",
        label: "正在准备",
        description: "运行环境尚未就绪，请稍后刷新。",
        canOpen: false,
        url: null
      };
    case "not_found":
      return {
        kind: "not_found",
        label: "运行环境不存在",
        description: "尚未找到该工作空间的运行环境。",
        canOpen: false,
        url: null
      };
    case "destroyed":
      return {
        kind: "destroyed",
        label: "已停止",
        description: "工作空间的运行环境已销毁，无法打开。",
        canOpen: false,
        url: null
      };
    default:
      return {
        kind: "unconfirmed",
        label: "状态待确认",
        description: "运行环境状态尚未确认，请刷新状态。",
        canOpen: false,
        url: null,
        rawValue: (runtime as { status: string }).status
      };
  }
}

type KnownWorkspaceLifecycle =
  | "active"
  | "creating"
  | "data_deleted"
  | "expired"
  | "failed"
  | "pending"
  | "running"
  | "suspended";

export type WorkspaceLifecyclePresentation =
  | { known: true; kind: KnownWorkspaceLifecycle; label: string }
  | { known: false; kind: "unknown"; label: "待确认"; rawValue: string }
  | { known: false; kind: "unavailable"; label: "暂不可用" };

export function presentWorkspaceLifecycle(
  state: WorkspaceDTO["state"] | undefined
): WorkspaceLifecyclePresentation {
  switch (state) {
    case "active":
      return { known: true, kind: "active", label: "已激活" };
    case "creating":
      return { known: true, kind: "creating", label: "开通中" };
    case "data_deleted":
      return { known: true, kind: "data_deleted", label: "数据已删除" };
    case "expired":
      return { known: true, kind: "expired", label: "已到期" };
    case "failed":
      return { known: true, kind: "failed", label: "已失败" };
    case "pending":
      return { known: true, kind: "pending", label: "待开通" };
    case "running":
      return { known: true, kind: "running", label: "运行中" };
    case "suspended":
      return { known: true, kind: "suspended", label: "已暂停" };
    case undefined:
    case "":
      return { known: false, kind: "unavailable", label: "暂不可用" };
    default:
      return { known: false, kind: "unknown", label: "待确认", rawValue: state };
  }
}

type KnownWorkspaceRenewalStatus = "active" | "not_applicable" | "expired_unpaid" | "manual";

export type WorkspaceRenewalPresentation =
  | { known: true; kind: KnownWorkspaceRenewalStatus; label: string }
  | { known: false; kind: "unknown"; label: "待确认"; rawValue?: string };

export function presentWorkspaceRenewal(
  status: WorkspaceDTO["renewalStatus"]
): WorkspaceRenewalPresentation {
  switch (status) {
    case "active":
      return { known: true, kind: "active", label: "有效" };
    case "not_applicable":
      return { known: true, kind: "not_applicable", label: "不适用" };
    case "expired_unpaid":
      return { known: true, kind: "expired_unpaid", label: "已到期，未续费" };
    case "manual":
      return { known: true, kind: "manual", label: "手动续费" };
    default:
      return status === undefined
        ? { known: false, kind: "unknown", label: "待确认" }
        : { known: false, kind: "unknown", label: "待确认", rawValue: status };
  }
}

export function presentWorkspaceRecovery(recovery: WorkspaceRenewalReadDTO["recovery"]) {
  switch (recovery.state) {
    case "not_required": return null;
    case "recoverable": return { title: "可以续费恢复", description: "确认续费并完成扣款后，系统将恢复原工作空间。充值本身不会恢复使用。" };
    case "pending": return { title: "正在处理续费恢复", description: "原续费请求正在处理，请勿重复提交。可以关闭页面后再查看；工作空间显示可使用后才能打开。" };
    case "reclaimed": return { title: "原工作空间无法恢复", description: "原工作空间资源已回收，无法续费恢复。请重新购买工作空间，原数据不提供恢复。" };
    case "unavailable": {
      const reasons: Record<string, string> = {
        workspace_renewal_insufficient_balance: "余额不足。请补足余额后刷新续费条件；充值不会自动恢复工作空间。",
        workspace_renewal_account_unavailable: "暂时无法确认账户余额，请稍后刷新续费条件。",
        workspace_renewal_provider_truth_unavailable: "暂时无法确认原工作空间资源，请稍后刷新续费条件。",
        workspace_renewal_identity_mismatch: "原工作空间的续费条件需要管理员核对，请联系管理员处理。",
        workspace_renewal_manual_review: "原续费结果需要管理员核对，请勿重复付款。",
        workspace_renewal_period_elapsed: "原续费期间已过，当前无法续费恢复，请重新购买工作空间。",
        workspace_delete_in_progress: "工作空间正在删除，不能续费恢复。"
      };
      return { title: "暂时无法续费恢复", description: reasons[recovery.reason] || "暂时无法确认续费恢复条件，请稍后刷新。" };
    }
  }
}

type KnownWorkspaceBudgetStatus = WorkspaceGatewayBudgetDTO["status"];

export type WorkspaceBudgetPresentation =
  | { known: true; kind: KnownWorkspaceBudgetStatus; label: string }
  | { known: false; kind: "unknown"; label: "待确认"; rawValue: string };

const USD_MICROS_PER_DOLLAR = 1_000_000n;
const MAX_SAFE_USD_MICROS = BigInt(Number.MAX_SAFE_INTEGER);

export function formatWorkspaceBudgetUsdInput(value?: string): string {
  if (!value || !/^(0|[1-9]\d*)$/.test(value)) return "";
  const micros = BigInt(value);
  const dollars = micros / USD_MICROS_PER_DOLLAR;
  const remainder = micros % USD_MICROS_PER_DOLLAR;
  if (remainder === 0n) return String(dollars);
  const fraction = String(remainder).padStart(6, "0").replace(/0+$/, "");
  return `${dollars}.${fraction}`;
}

export function parseWorkspaceBudgetUsdInput(value: string): number | null {
  const match = /^(0|[1-9]\d*)(?:\.(\d{1,6}))?$/.exec(value.trim());
  if (!match) return null;
  const micros = BigInt(match[1]) * USD_MICROS_PER_DOLLAR
    + BigInt((match[2] || "").padEnd(6, "0"));
  return micros <= MAX_SAFE_USD_MICROS ? Number(micros) : null;
}

export function presentWorkspaceBudget(status: string): WorkspaceBudgetPresentation {
  switch (status) {
    case "active":
      return { known: true, kind: "active", label: "已启用" };
    case "disabled":
      return { known: true, kind: "disabled", label: "已停用" };
    case "quota_exhausted":
      return { known: true, kind: "quota_exhausted", label: "额度已用尽" };
    case "expired":
      return { known: true, kind: "expired", label: "已过期" };
    default:
      return { known: false, kind: "unknown", label: "待确认", rawValue: status };
  }
}

export interface WorkspaceQuotePresentationInput {
  selectedPriceUsdMicros: number | null;
  customerOwned: boolean;
}

export type WorkspaceQuotePresentation =
  | {
      kind: "unavailable" | "unconfirmed";
      totalUsdMicros: null;
      confirmationLabel: string;
      submitLabel: string;
      requiresPrepayment: false;
      canConfirm: false;
    }
  | {
      kind: "included";
      totalUsdMicros: 0;
      confirmationLabel: string;
      submitLabel: string;
      requiresPrepayment: false;
      canConfirm: true;
    }
  | {
      kind: "prepaid";
      totalUsdMicros: number;
      confirmationLabel: string;
      submitLabel: string;
      requiresPrepayment: true;
      canConfirm: true;
    };

export function presentWorkspaceQuote(
  input: WorkspaceQuotePresentationInput
): WorkspaceQuotePresentation {
  if (input.selectedPriceUsdMicros === null) {
    return {
      kind: "unavailable",
      totalUsdMicros: null,
      confirmationLabel: "价格确认后才能开通工作空间",
      submitLabel: "价格暂不可用",
      requiresPrepayment: false,
      canConfirm: false
    };
  }
  if (input.customerOwned && input.selectedPriceUsdMicros === 0) {
    return {
      kind: "included",
      totalUsdMicros: 0,
      confirmationLabel: "我确认使用当前客户权益开通工作空间（无需预付）",
      submitLabel: "确认并开通",
      requiresPrepayment: false,
      canConfirm: true
    };
  }
  if (input.customerOwned === false && input.selectedPriceUsdMicros > 0) {
    return {
      kind: "prepaid",
      totalUsdMicros: input.selectedPriceUsdMicros,
      confirmationLabel: "我确认一次性预付工作空间月度总额并开通",
      submitLabel: "确认预付并开通",
      requiresPrepayment: true,
      canConfirm: true
    };
  }
  return {
    kind: "unconfirmed",
    totalUsdMicros: null,
    confirmationLabel: "价格结果待确认，暂不能开通工作空间",
    submitLabel: "价格待确认",
    requiresPrepayment: false,
    canConfirm: false
  };
}

export interface WorkspaceApplicationBindingPresentation {
  known: boolean;
  label: string;
}

export function presentWorkspaceApplicationBinding(binding: string | undefined): WorkspaceApplicationBindingPresentation {
  switch (binding) {
    case "empty":
      return { known: true, label: "未安装应用" };
    case "opl_app":
      return { known: true, label: "OPL App" };
    default:
      return { known: false, label: "待确认" };
  }
}
