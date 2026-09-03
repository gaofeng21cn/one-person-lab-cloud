import type {
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceLaunchResponse,
  WorkspaceRuntimeDTO
} from "../api/dtos.ts";

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

export function presentWorkspaceLaunch(
  operation: Pick<WorkspaceLaunchResponse, "status" | "workspaceId">
): WorkspaceLaunchPresentation {
  switch (operation.status) {
    case "pending":
      return {
        kind: "pending",
        title: "正在准备工作空间",
        summary: "系统正在准备所需资源，请稍后刷新状态。",
        tone: "info",
        canOpenWorkspace: false
      };
    case "manual_review":
      return {
        kind: "manual_review",
        title: "需要人工处理",
        summary: "订单已保留，工作人员正在处理，请稍后刷新状态。",
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

type KnownWorkspaceBudgetStatus = WorkspaceGatewayBudgetDTO["status"];

export type WorkspaceBudgetPresentation =
  | { known: true; kind: KnownWorkspaceBudgetStatus; label: string }
  | { known: false; kind: "unknown"; label: "待确认"; rawValue: string };

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
