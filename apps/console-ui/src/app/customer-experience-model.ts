export type CustomerPresentation =
  | { kind: "known"; label: string }
  | { kind: "unknown"; label: "待确认"; rawValue: string }
  | { kind: "unavailable"; label: "暂不可用" };

export function presentCustomerStatus(status: string | undefined): CustomerPresentation {
  switch (status) {
    case "completed":
    case "succeeded":
      return { kind: "known", label: "已完成" };
    case "used":
      return { kind: "known", label: "已生效" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: status };
  }
}

export function presentBillingReceiptType(type: string | undefined): CustomerPresentation {
  switch (type) {
    case "billing.workspace_purchased.v1":
      return { kind: "known", label: "工作空间开通" };
    case "billing.workspace_renewed.v1":
      return { kind: "known", label: "工作空间续费" };
    case "billing.workspace_expired.v1":
      return { kind: "known", label: "工作空间到期" };
    case "billing.workspace_refunded.v1":
      return { kind: "known", label: "工作空间退款" };
    case undefined:
    case "":
      return { kind: "unavailable", label: "暂不可用" };
    default:
      return { kind: "unknown", label: "待确认", rawValue: type };
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
