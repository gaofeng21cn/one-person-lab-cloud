import type { WalletAdjustmentRequest } from "../api/dtos.ts";

export interface WalletAdjustmentIntent {
  readonly accountId: string;
  readonly input: WalletAdjustmentRequest;
  readonly idempotencyKey: string;
}

export interface WalletAdjustmentRecoveryIntent {
  readonly operationId: string;
  readonly input: { accountId: string; evidenceRef: string };
  readonly idempotencyKey: string;
}

export function sameWalletAdjustmentInput(left: WalletAdjustmentRequest, right: WalletAdjustmentRequest): boolean {
  return left.kind === right.kind
    && left.amountUsd === right.amountUsd
    && left.reason === right.reason
    && (left.relatedOperationId || "") === (right.relatedOperationId || "")
    && left.confirmationAccountId === right.confirmationAccountId;
}

export function resolveWalletAdjustmentIntent(
  current: WalletAdjustmentIntent | null,
  accountId: string,
  input: WalletAdjustmentRequest,
  createIdempotencyKey: () => string
): WalletAdjustmentIntent {
  if (current?.accountId === accountId && sameWalletAdjustmentInput(current.input, input)) return current;
  return { accountId, input, idempotencyKey: createIdempotencyKey() };
}

export function walletRecoveryIdempotencyKey(operationId: string): string {
  const suffix = /^wallet-adjustment-([0-9a-f]{18})$/.exec(operationId)?.[1] || "";
  return suffix ? `wallet-recovery-${suffix.slice(0, 16)}` : "";
}
