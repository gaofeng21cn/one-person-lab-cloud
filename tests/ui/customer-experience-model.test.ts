import assert from "node:assert/strict";
import test from "node:test";

import {
  presentAccountStatus,
  presentBalanceHistoryType,
  presentBillingReceiptType,
  presentCustomerStatus,
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
  assert.deepEqual(presentCustomerStatus("used"), {
    kind: "known",
    label: "已生效"
  });
});

test("billing receipt type and status use exact current values", () => {
  const typeCases = [
    ["billing.workspace_purchased.v1", "工作空间开通"],
    ["billing.workspace_renewed.v1", "工作空间续费"],
    ["billing.workspace_expired.v1", "工作空间到期"],
    ["billing.workspace_refunded.v1", "工作空间退款"]
  ] as const;
  for (const [type, label] of typeCases) {
    assert.deepEqual(presentBillingReceiptType(type), { kind: "known", label });
  }

  assert.deepEqual(presentCustomerStatus("completed"), {
    kind: "known",
    label: "已完成"
  });
  assert.deepEqual(presentCustomerStatus("succeeded"), {
    kind: "known",
    label: "已完成"
  });
});

test("unknown values are unconfirmed without becoming customer labels", () => {
  const cases = [
    presentAccountStatus("future_account"),
    presentGatewayKeyStatus("active_future"),
    presentBalanceHistoryType("workspace.created"),
    presentBillingReceiptType("billing.workspace_purchased.v2"),
    presentCustomerStatus("completed_future")
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
    presentCustomerStatus(undefined)
  ]) {
    assert.deepEqual(presentation, { kind: "unavailable", label: "暂不可用" });
  }
});
