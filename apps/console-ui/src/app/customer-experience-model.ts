import type { BillingReceipt } from "../api/dtos.ts";
import { formatUsdMicros } from "../console-model.ts";

export type CustomerPresentation =
  | { kind: "known"; label: string }
  | { kind: "unknown"; label: "待确认"; rawValue: string }
  | { kind: "unavailable"; label: "暂不可用" };

export function presentBillingStatus(status: string | undefined): CustomerPresentation {
  switch (status) {
    case "completed":
      return { kind: "known", label: "已完成" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: status };
  }
}

export function presentBalanceHistoryStatus(status: string | undefined): CustomerPresentation {
  switch (status) {
    case "used":
      return { kind: "known", label: "已生效" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: status };
  }
}

export function presentBillingReceiptType(type: string | undefined, kind?: BillingReceipt["kind"]): CustomerPresentation {
  switch (type) {
    case "billing.workspace_purchased.v1":
      return { kind: "known", label: "工作空间开通" };
    case "billing.workspace_renewed.v1":
      return { kind: "known", label: "工作空间续费" };
    case "billing.workspace_expired.v1":
      return { kind: "known", label: "工作空间到期" };
    case "billing.workspace_refunded.v1":
      return { kind: "known", label: "工作空间退款" };
    case "gateway.wallet_adjustment.v1":
      return kind === "business_refund" ? { kind: "known", label: "工作空间退款" } : { kind: "unknown", label: "待确认", rawValue: type };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: type };
  }
}

export function presentBillingReceiptAmount(receipt: BillingReceipt): string {
  if (receipt.status !== "completed") return "金额待确认";
  switch (receipt.type) {
    case "billing.workspace_purchased.v1":
    case "billing.workspace_renewed.v1":
      return `扣款 ${formatUsdMicros(receipt.totalUsdMicros)}`;
    case "billing.workspace_refunded.v1":
      return receipt.refundUsdMicros === undefined ? "退款金额暂不可用" : `退款 ${formatUsdMicros(receipt.refundUsdMicros)}`;
    case "gateway.wallet_adjustment.v1":
      return receipt.kind !== "business_refund" ? "金额待确认" : receipt.refundUsdMicros === undefined ? "退款金额暂不可用" : `退款 ${formatUsdMicros(receipt.refundUsdMicros)}`;
    case "billing.workspace_expired.v1":
      return "未扣款";
    default:
      return formatUsdMicros(receipt.chargeUsdMicros);
  }
}

export function presentBalanceHistoryType(type: string | undefined): CustomerPresentation {
  switch (type) {
    case "balance":
      return { kind: "known", label: "余额变动" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: type };
  }
}

export function presentAccountStatus(status: string | undefined): CustomerPresentation {
  switch (status) {
    case "active":
      return { kind: "known", label: "正常" };
    case "disabled":
      return { kind: "known", label: "已停用" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: status };
  }
}

export function presentGatewayKeyStatus(status: string | undefined): CustomerPresentation {
  switch (status) {
    case "active":
      return { kind: "known", label: "启用" };
    case "disabled":
      return { kind: "known", label: "停用" };
    case "quota_exhausted":
      return { kind: "known", label: "额度用尽" };
    case "expired":
      return { kind: "known", label: "已过期" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: status };
  }
}
