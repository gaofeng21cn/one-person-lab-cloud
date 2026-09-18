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
    case "billing.workspace_closed.v1":
      return { kind: "known", label: "开通未完成，已结案" };
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
    case "billing.workspace_closed.v1":
      return receipt.chargeUsdMicros === 0 ? "未扣款" : receipt.chargeUsdMicros === undefined ? "原扣款金额暂不可用" : `原扣款 ${formatUsdMicros(receipt.chargeUsdMicros)}（退款另列）`;
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

/** 单笔费用回执对账户余额的影响：扣款为正、退款为负，未扣款为零，其余未确认。 */
export type BillingReceiptSpend =
  | { kind: "charge"; usdMicros: number }
  | { kind: "refund"; usdMicros: number }
  | { kind: "no_charge" }
  | { kind: "unknown" };

function knownReceiptMicros(value: number | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
}

/**
 * 由 typed receipt 的原始金额字段判定费用：只有 `completed` 回执才产生已确认金额，
 * 金额一律读取 Ledger 投影的 micros 字段，不从展示文案反解。
 */
export function billingReceiptSpend(receipt: BillingReceipt): BillingReceiptSpend {
  if (receipt.status !== "completed") return { kind: "unknown" };
  switch (receipt.type) {
    case "billing.workspace_purchased.v1":
    case "billing.workspace_renewed.v1": {
      const charged = knownReceiptMicros(receipt.totalUsdMicros);
      return charged === null ? { kind: "unknown" } : { kind: "charge", usdMicros: charged };
    }
    case "billing.workspace_refunded.v1": {
      const refunded = knownReceiptMicros(receipt.refundUsdMicros);
      return refunded === null ? { kind: "unknown" } : { kind: "refund", usdMicros: refunded };
    }
    case "gateway.wallet_adjustment.v1": {
      if (receipt.kind !== "business_refund") return { kind: "unknown" };
      const refunded = knownReceiptMicros(receipt.refundUsdMicros);
      return refunded === null ? { kind: "unknown" } : { kind: "refund", usdMicros: refunded };
    }
    case "billing.workspace_expired.v1":
      return { kind: "no_charge" };
    case "billing.workspace_closed.v1": {
      const charged = knownReceiptMicros(receipt.chargeUsdMicros);
      if (charged === null) return { kind: "unknown" };
      return charged === 0 ? { kind: "no_charge" } : { kind: "charge", usdMicros: charged };
    }
    default:
      return { kind: "unknown" };
  }
}

export interface BillingSpendTrendDay {
  key: string;
  label: string;
  shortLabel: string;
  chargedUsdMicros: number;
  refundedUsdMicros: number;
  netUsdMicros: number;
}

export interface BillingSpendTrend {
  days: BillingSpendTrendDay[];
  windowStart: string;
  windowEnd: string;
  chargedUsdMicros: number;
  refundedUsdMicros: number;
  netUsdMicros: number;
  receiptCount: number;
  chargeCount: number;
  refundCount: number;
  noChargeCount: number;
  unknownReceiptCount: number;
}

function localCalendarDayKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

/** 汇总窗口按本地日历日计算：`days` 天，最后一天为 `now` 当天。 */
export function billingSpendTrendWindowStart(now: Date, days: number): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() - (days - 1));
}

export function billingSpendTrendWindowEnd(now: Date): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
}

/**
 * 按本地日历日聚合已确认费用。窗口外的回执不参与统计；金额未确认的回执计入
 * `unknownReceiptCount` 而不进入任何一天的金额，避免把未知当成零消费。
 */
export function buildBillingSpendTrend(receipts: BillingReceipt[], now: Date, days = 14): BillingSpendTrend {
  const windowStart = billingSpendTrendWindowStart(now, days);
  const windowEnd = billingSpendTrendWindowEnd(now);
  const buckets = new Map<string, BillingSpendTrendDay>();
  const windowDays: BillingSpendTrendDay[] = [];
  for (let offset = days - 1; offset >= 0; offset--) {
    const date = new Date(now.getFullYear(), now.getMonth(), now.getDate() - offset);
    const day: BillingSpendTrendDay = {
      key: localCalendarDayKey(date),
      label: `${date.getMonth() + 1}/${date.getDate()}`,
      shortLabel: offset % 2 === 0 || offset === days - 1 ? `${date.getDate()}日` : "",
      chargedUsdMicros: 0,
      refundedUsdMicros: 0,
      netUsdMicros: 0
    };
    buckets.set(day.key, day);
    windowDays.push(day);
  }

  const trend: BillingSpendTrend = {
    days: windowDays,
    windowStart: windowStart.toISOString(),
    windowEnd: windowEnd.toISOString(),
    chargedUsdMicros: 0,
    refundedUsdMicros: 0,
    netUsdMicros: 0,
    receiptCount: 0,
    chargeCount: 0,
    refundCount: 0,
    noChargeCount: 0,
    unknownReceiptCount: 0
  };

  for (const receipt of receipts) {
    const at = new Date(receipt.createdAt);
    if (Number.isNaN(at.getTime())) {
      trend.unknownReceiptCount += 1;
      continue;
    }
    if (at.getTime() < windowStart.getTime() || at.getTime() >= windowEnd.getTime()) continue;
    const spend = billingReceiptSpend(receipt);
    if (spend.kind === "unknown") {
      trend.unknownReceiptCount += 1;
      continue;
    }
    trend.receiptCount += 1;
    if (spend.kind === "no_charge") {
      trend.noChargeCount += 1;
      continue;
    }
    const day = buckets.get(localCalendarDayKey(at));
    if (!day) continue;
    if (spend.kind === "charge") {
      trend.chargeCount += 1;
      trend.chargedUsdMicros += spend.usdMicros;
      day.chargedUsdMicros += spend.usdMicros;
    } else {
      trend.refundCount += 1;
      trend.refundedUsdMicros += spend.usdMicros;
      day.refundedUsdMicros += spend.usdMicros;
    }
    day.netUsdMicros = day.chargedUsdMicros - day.refundedUsdMicros;
  }
  trend.netUsdMicros = trend.chargedUsdMicros - trend.refundedUsdMicros;
  return trend;
}
