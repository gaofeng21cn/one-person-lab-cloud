import assert from "node:assert/strict";
import test from "node:test";
import type { BillingReceipt } from "../../apps/console-ui/src/api/dtos.ts";

import {
  billingReceiptSpend,
  buildBillingSpendTrend,
  presentAccountStatus,
  presentBalanceHistoryStatus,
  presentBalanceHistoryType,
  presentBillingStatus,
  presentBillingReceiptType,
  presentBillingReceiptAmount,
  presentGatewayKeyStatus
} from "../../apps/console-ui/src/app/customer-experience-model.ts";

test("account statuses use exact customer labels", () => {
  assert.deepEqual(presentAccountStatus("active"), { kind: "known", label: "正常" });
  assert.deepEqual(presentAccountStatus("disabled"), { kind: "known", label: "已停用" });
});

test("API key statuses use only exact owner values", () => {
  const cases = [
    ["active", "启用"],
    ["disabled", "停用"],
    ["quota_exhausted", "额度用尽"],
    ["expired", "已过期"]
  ] as const;

  for (const [status, label] of cases) {
    assert.deepEqual(presentGatewayKeyStatus(status), { kind: "known", label });
  }
});

test("balance history type and status use exact current values", () => {
  assert.deepEqual(presentBalanceHistoryType("balance"), {
    kind: "known",
    label: "余额变动"
  });
  assert.deepEqual(presentBalanceHistoryStatus("used"), {
    kind: "known",
    label: "已生效"
  });
});

test("billing receipt type and status use exact current values", () => {
  const typeCases = [
    ["billing.workspace_purchased.v1", "工作空间开通"],
    ["billing.workspace_renewed.v1", "工作空间续费"],
    ["billing.workspace_expired.v1", "工作空间到期"],
    ["billing.workspace_closed.v1", "开通未完成，已结案"],
    ["billing.workspace_refunded.v1", "工作空间退款"]
  ] as const;
  for (const [type, label] of typeCases) {
    assert.deepEqual(presentBillingReceiptType(type), { kind: "known", label });
  }

  assert.deepEqual(presentBillingStatus("completed"), {
    kind: "known",
    label: "已完成"
  });
});

test("billing and balance history statuses reject values from the other owner", () => {
  for (const presentation of [
    presentBillingStatus("used"),
    presentBillingStatus("succeeded"),
    presentBalanceHistoryStatus("completed"),
    presentBalanceHistoryStatus("succeeded")
  ]) {
    assert.equal(presentation.kind, "unknown");
    assert.equal(presentation.label, "待确认");
    assert.ok("rawValue" in presentation);
  }
});

test("unknown values are unconfirmed without becoming customer labels", () => {
  const cases = [
    presentAccountStatus("future_account"),
    presentGatewayKeyStatus("active_future"),
    presentBalanceHistoryType("workspace.created"),
    presentBillingReceiptType("billing.workspace_purchased.v2"),
    presentBillingStatus("completed_future"),
    presentBalanceHistoryStatus("used_future")
  ];

  for (const presentation of cases) {
    assert.equal(presentation.kind, "unknown");
    assert.equal(presentation.label, "待确认");
    assert.ok("rawValue" in presentation);
    assert.notEqual(presentation.label, presentation.rawValue);
  }
});

test("missing values are unavailable without raw evidence", () => {
  for (const presentation of [
    presentAccountStatus(undefined),
    presentGatewayKeyStatus(undefined),
    presentBalanceHistoryType(undefined),
    presentBillingReceiptType(undefined),
    presentBillingStatus(undefined),
    presentBalanceHistoryStatus(undefined)
  ]) {
    assert.deepEqual(presentation, { kind: "unavailable", label: "暂不可用" });
  }
});

test("customer receipts distinguish monthly charges, partial refunds and expiry without a new charge", () => {
  const base: BillingReceipt = {
    receiptId: "local-receipt", type: "billing.workspace_purchased.v1", status: "completed", workspaceId: "local-workspace",
    createdAt: "2026-09-08T00:00:00Z", resourceType: "workspace", resourceId: "local-workspace", priceVersion: "local-price",
    currency: "USD", periodStart: "2026-09-01T00:00:00Z", paidThrough: "2026-10-01T00:00:00Z", totalUsdMicros: 52_580_000
  };
  assert.equal(presentBillingReceiptAmount(base), "扣款 $52.58");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_renewed.v1" }), "扣款 $52.58");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_refunded.v1", refundUsdMicros: 3_000_000 }), "退款 $3.00");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_expired.v1" }), "未扣款");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_closed.v1", chargeUsdMicros: 52_580_000 }), "原扣款 $52.58（退款另列）");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_closed.v1", chargeUsdMicros: 0 }), "未扣款");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_closed.v1" }), "原扣款金额暂不可用");
  assert.equal(presentBillingReceiptAmount({ ...base, status: "pending" }), "金额待确认");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "gateway.wallet_adjustment.v1", kind: "business_refund", refundUsdMicros: 3_000_000 }), "退款 $3.00");
  assert.equal(presentBillingReceiptType("gateway.wallet_adjustment.v1", "business_refund").label, "工作空间退款");
  assert.equal(presentBillingReceiptType("gateway.wallet_adjustment.v1").kind, "unknown");
  assert.equal(presentBillingReceiptAmount({ ...base, type: "billing.workspace_refunded.v1" }), "退款金额暂不可用");
});

const trendBase: BillingReceipt = {
  receiptId: "trend-receipt", type: "billing.workspace_purchased.v1", status: "completed", workspaceId: "trend-workspace",
  createdAt: "2026-09-18T03:00:00Z", resourceType: "workspace", resourceId: "trend-workspace", priceVersion: "local-price",
  currency: "USD", periodStart: "2026-09-01T00:00:00Z", paidThrough: "2026-10-01T00:00:00Z", totalUsdMicros: 52_580_000
};

test("receipt spend reads typed micros instead of parsing the presented amount", () => {
  assert.equal(presentBillingReceiptAmount(trendBase), "扣款 $52.58");
  assert.deepEqual(billingReceiptSpend(trendBase), { kind: "charge", usdMicros: 52_580_000 });
  assert.deepEqual(
    billingReceiptSpend({ ...trendBase, type: "billing.workspace_renewed.v1" }),
    { kind: "charge", usdMicros: 52_580_000 }
  );
  assert.deepEqual(
    billingReceiptSpend({ ...trendBase, type: "billing.workspace_refunded.v1", refundUsdMicros: 3_000_000 }),
    { kind: "refund", usdMicros: 3_000_000 }
  );
  assert.deepEqual(
    billingReceiptSpend({ ...trendBase, type: "gateway.wallet_adjustment.v1", kind: "business_refund", refundUsdMicros: 3_000_000 }),
    { kind: "refund", usdMicros: 3_000_000 }
  );
  assert.deepEqual(billingReceiptSpend({ ...trendBase, type: "billing.workspace_expired.v1" }), { kind: "no_charge" });
  assert.deepEqual(
    billingReceiptSpend({ ...trendBase, type: "billing.workspace_closed.v1", chargeUsdMicros: 52_580_000 }),
    { kind: "charge", usdMicros: 52_580_000 }
  );
  assert.deepEqual(
    billingReceiptSpend({ ...trendBase, type: "billing.workspace_closed.v1", chargeUsdMicros: 0 }),
    { kind: "no_charge" }
  );
});

test("receipt spend stays unknown when the owner fields cannot confirm an amount", () => {
  for (const receipt of [
    { ...trendBase, status: "pending" },
    { ...trendBase, totalUsdMicros: undefined },
    { ...trendBase, type: "gateway.wallet_adjustment.v1" },
    { ...trendBase, type: "billing.workspace_closed.v1" },
    { ...trendBase, type: "billing.resource_purchased.v1" },
    { ...trendBase, type: "billing.workspace_renewed.v2" }
  ]) {
    assert.deepEqual(billingReceiptSpend(receipt), { kind: "unknown" }, `${receipt.type} must stay unconfirmed`);
  }
});

test("14-day trend aggregates typed net spend per local calendar day", () => {
  const now = new Date(2026, 8, 18, 10, 30, 0);
  const day = (offset: number, hour: number) => new Date(2026, 8, 18 - offset, hour, 0, 0).toISOString();
  const spent = [
    { ...trendBase, receiptId: "today-charge", createdAt: day(0, 1) },
    { ...trendBase, receiptId: "yesterday-refund", createdAt: day(1, 23), type: "billing.workspace_refunded.v1", refundUsdMicros: 3_000_000 },
    { ...trendBase, receiptId: "today-expired", createdAt: day(0, 2), type: "billing.workspace_expired.v1" },
    { ...trendBase, receiptId: "pending-renewal", createdAt: day(0, 3), type: "billing.workspace_renewed.v1", status: "pending" },
    { ...trendBase, receiptId: "outside-window", createdAt: day(20, 3), totalUsdMicros: 250_000_000 }
  ];

  const trend = buildBillingSpendTrend(spent, now, 14);

  assert.equal(trend.days.length, 14);
  assert.deepEqual(trend.days[0].key, "2026-09-05");
  assert.deepEqual(trend.days[13].key, "2026-09-18");
  assert.equal(trend.days[13].chargedUsdMicros, 52_580_000);
  assert.equal(trend.days[13].netUsdMicros, 52_580_000);
  assert.equal(trend.days[12].refundedUsdMicros, 3_000_000);
  assert.equal(trend.days[12].netUsdMicros, -3_000_000);
  assert.equal(trend.chargedUsdMicros, 52_580_000);
  assert.equal(trend.refundedUsdMicros, 3_000_000);
  assert.equal(trend.netUsdMicros, 49_580_000);
  assert.equal(trend.receiptCount, 3);
  assert.equal(trend.noChargeCount, 1);
  assert.equal(trend.unknownReceiptCount, 1);
});

test("14-day trend uses local calendar boundaries and drops records outside the window", () => {
  const now = new Date(2026, 8, 18, 0, 30, 0);
  const firstDayStart = new Date(2026, 8, 5, 0, 0, 0).toISOString();
  const beforeWindow = new Date(2026, 8, 4, 23, 59, 59).toISOString();
  const afterWindow = new Date(2026, 8, 19, 0, 0, 0).toISOString();
  const trend = buildBillingSpendTrend([
    { ...trendBase, receiptId: "boundary", createdAt: firstDayStart },
    { ...trendBase, receiptId: "before", createdAt: beforeWindow, totalUsdMicros: 100_000_000 },
    { ...trendBase, receiptId: "after", createdAt: afterWindow, totalUsdMicros: 200_000_000 }
  ], now, 14);

  assert.equal(trend.windowStart, new Date(2026, 8, 5, 0, 0, 0).toISOString());
  assert.equal(trend.windowEnd, new Date(2026, 8, 19, 0, 0, 0).toISOString());
  assert.equal(trend.receiptCount, 1);
  assert.equal(trend.netUsdMicros, 52_580_000);
  assert.equal(trend.days[0].chargedUsdMicros, 52_580_000);
});
