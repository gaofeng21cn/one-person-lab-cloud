import type { WorkspaceRenewalQuote } from "../api/workspace-renewal-api.ts";
import { formatWorkspaceTransactionAmount } from "./workspace-experience-model.ts";

// A manual renewal is one continuation of a paid period, not a new purchase.
// The customer sees the owner's own price before anything is charged, and the
// owner's own refusal code afterwards. The Console adds no rule of its own
// about when a renewal is allowed: the Workspace owner refuses an expired or
// already-paid period, and the Console reports that refusal in the customer's
// language.

export type WorkspaceRenewalStage =
  | "idle"
  | "quoting"
  | "confirming"
  | "charging"
  | "polling"
  | "succeeded"
  | "failed";

export type WorkspaceRenewalOperationView = {
  status: string;
  errorCode?: string;
};

const terminalStatuses = new Set(["succeeded", "failed", "cancelled", "needs_attention"]);

export function renewalOperationIsTerminal(operation: WorkspaceRenewalOperationView): boolean {
  return terminalStatuses.has(operation.status);
}

// renewalQuoteIsCurrent reports whether the owner's offer can still be
// accepted. An expired offer is not charged; the customer is asked to price a
// new one instead.
export function renewalQuoteIsCurrent(quote: WorkspaceRenewalQuote, now: number): boolean {
  const expiresAt = Date.parse(quote.expiresAt);
  return Number.isFinite(expiresAt) && expiresAt > now;
}

export function renewalConfirmationText(quote: WorkspaceRenewalQuote): string {
  const price = formatWorkspaceTransactionAmount(quote.totalUSDMicros);
  return `本次续费 1 个月，费用 ${price}。续费在当前付费期结束后顺延，不额外购买新的资源；若该付费月已购买，续费会被拒绝且不会扣款。`;
}

// renewalFailureText reads the owner's own refusal code. The Workspace owner
// refuses a renewal before it charges, so a rejected renewal never took the
// customer's money; a renewal whose result stayed unconfirmed may have, and
// says so instead of claiming a refund that does not exist.
export function renewalFailureText(operation: WorkspaceRenewalOperationView): string {
  switch (operation.errorCode) {
    case "RENEWAL_PERIOD_ELAPSED":
      return "当前付费期已结束，续费已被拒绝，未扣款。";
    case "QUOTE_MISMATCH":
      return "续费报价与当前订阅不匹配，续费已被拒绝，未扣款。请重新发起续费。";
    case "WORKSPACE_NOT_READY":
      return "工作空间当前状态不允许续费，续费已被拒绝，未扣款。";
    case "OWNER_CAPABILITY_UNAVAILABLE":
      return "资源方暂不支持续期，续费未完成。";
    case "VERSION_CONFLICT":
      return "订阅状态已变化，续费已被拒绝，未扣款。请刷新后重试。";
    default:
      return "续费结果待确认，请刷新订阅查看当前付费期。";
  }
}

export function renewalSuccessText(): string {
  return "续费成功，订阅已更新为新的付费期。";
}

// renewalErrorCode reads the owner's own refusal code out of a failed request.
// The public error envelope names the code in `code`, while the older
// publisher envelope names it in `error`; a consumer that reads only one of
// them cannot see a refusal at all.
export function renewalErrorCode(error: unknown): string {
  const payload = error && typeof error === "object" && "payload" in error
    ? (error as { payload?: unknown }).payload
    : null;
  if (!payload || typeof payload !== "object") return "";
  const envelope = payload as { code?: unknown; error?: unknown };
  return String(envelope.code ?? envelope.error ?? "");
}

// renewalRequestFailureText reports a renewal the owner refused before it
// accepted anything. The Workspace owner refuses such a renewal before it
// charges, so no money was taken.
export function renewalRequestFailureText(error: unknown): string {
  const code = renewalErrorCode(error);
  switch (code) {
    case "RENEWAL_PERIOD_ELAPSED":
      return "当前付费期已结束，续费已被拒绝，未扣款。";
    case "QUOTE_MISMATCH":
    case "QUOTE_EXPIRED":
      return "续费报价已失效，续费已被拒绝，未扣款。请重新发起续费。";
    case "WORKSPACE_NOT_READY":
      return "工作空间当前状态不允许续费，续费已被拒绝，未扣款。";
    case "OWNER_CAPABILITY_UNAVAILABLE":
    case "DEPENDENCY_UNAVAILABLE":
      return "资源方暂不可用，续费未完成，请稍后重试。";
    case "VERSION_CONFLICT":
    case "SUBSCRIPTION_VERSION_CONFLICT":
      return "订阅状态已变化，续费已被拒绝，未扣款。请刷新后重试。";
    case "INSUFFICIENT_BALANCE":
      return "可用余额不足，续费未发起。";
    default:
      return "续费未能发起，请稍后重试。";
  }
}
