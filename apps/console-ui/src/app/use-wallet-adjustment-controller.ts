import { useCallback, useEffect, useRef, useState } from "react";

import {
  createWalletAdjustment,
  getWalletAdjustment,
  recoverWalletAdjustment
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  WalletAdjustmentOperationDTO,
  WalletAdjustmentRequest
} from "../api/dtos.ts";
import type { WalletAdjustmentController } from "./console-controller-types.ts";
import {
  resolveWalletAdjustmentIntent,
  walletRecoveryIdempotencyKey,
  type WalletAdjustmentIntent,
  type WalletAdjustmentRecoveryIntent
} from "./wallet-adjustment-controller-model.ts";

interface WalletAdjustmentDependencies {
  session: AuthSession | null;
  currentMutationRequest: () => () => boolean;
  refreshAccounts: () => Promise<void>;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
  mutationError: (error: unknown) => string;
}

export function useWalletAdjustmentController({
  session,
  currentMutationRequest,
  refreshAccounts,
  flash,
  friendlyError,
  mutationError
}: WalletAdjustmentDependencies): WalletAdjustmentController {
  const [operation, setOperation] = useState<WalletAdjustmentOperationDTO | null>(null);
  const [busy, setBusy] = useState(false);
  const intent = useRef<WalletAdjustmentIntent | null>(null);
  const recoveryIntent = useRef<WalletAdjustmentRecoveryIntent | null>(null);
  const requestGeneration = useRef(0);
  const scope = useRef({ userId: session?.user.id || "", csrfToken: session?.csrfToken || "" });
  scope.current = { userId: session?.user.id || "", csrfToken: session?.csrfToken || "" };

  const reset = useCallback(() => {
    requestGeneration.current += 1;
    intent.current = null;
    recoveryIntent.current = null;
    setOperation(null);
    setBusy(false);
  }, []);

  useEffect(() => {
    reset();
  }, [reset, session?.csrfToken, session?.user.id]);

  const setOperationProjection = useCallback((next: WalletAdjustmentOperationDTO | null) => {
    if (next === null) requestGeneration.current += 1;
    setOperation(next);
  }, []);

  const requestIsCurrent = (
    generation: number,
    requestStillCurrent: () => boolean,
    userId: string,
    csrfToken: string
  ) => generation === requestGeneration.current
    && requestStillCurrent()
    && scope.current.userId === userId
    && scope.current.csrfToken === csrfToken;

  const submit = async (accountId: string, input: WalletAdjustmentRequest) => {
    if (!session || busy || input.confirmationAccountId !== accountId || !input.amountUsd || !input.reason.trim()) return null;
    if (!window.confirm("请再次确认这笔余额操作：提交后会写入客户账户并保留操作记录。")) return null;
    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const generation = ++requestGeneration.current;
    const nextIntent = resolveWalletAdjustmentIntent(intent.current, accountId, input, () => `wallet-adjustment:${crypto.randomUUID()}`);
    intent.current = nextIntent;
    setBusy(true);
    try {
      const result = await createWalletAdjustment(accountId, nextIntent.input, csrfToken, nextIntent.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) return null;
      setOperation(result);
      if (result.status === "manual_review") flash("结果待确认，已进入人工复核", "danger");
      else {
        intent.current = null;
        flash("余额操作已提交");
      }
      await refreshAccounts();
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) return null;
      return result;
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) flash(mutationError(error), "danger");
      return null;
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) setBusy(false);
    }
  };

  const refresh = async () => {
    if (!session || !operation?.operationId || busy) return;
    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const generation = ++requestGeneration.current;
    setBusy(true);
    try {
      const result = await getWalletAdjustment(operation.operationId);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) return;
      setOperation(result);
      if (result.status === "succeeded") {
        intent.current = null;
        recoveryIntent.current = null;
      }
      await refreshAccounts();
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) return;
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) flash(friendlyError(error), "danger");
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) setBusy(false);
    }
  };

  const recover = async () => {
    const currentOperation = operation;
    if (!session || !currentOperation || busy || currentOperation.status !== "manual_review" || !currentOperation.allowedActions?.includes("recover_wallet_adjustment")) return;
    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    if (!recoveryIntent.current || recoveryIntent.current.operationId !== currentOperation.operationId) {
      const evidenceRef = (window.prompt("请输入 case-YYYYMMDD-xxx 证据引用") || "").trim();
      if (!evidenceRef) return;
      recoveryIntent.current = {
        operationId: currentOperation.operationId,
        input: { accountId: currentOperation.accountId, evidenceRef },
        idempotencyKey: walletRecoveryIdempotencyKey(currentOperation.operationId)
      };
    }
    if (!recoveryIntent.current.idempotencyKey) return;
    const generation = ++requestGeneration.current;
    setBusy(true);
    try {
      const result = await recoverWalletAdjustment(currentOperation.operationId, recoveryIntent.current.input, csrfToken, recoveryIntent.current.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) return;
      setOperation(result);
      if (result.status === "succeeded") {
        recoveryIntent.current = null;
        intent.current = null;
        flash("余额操作已确认");
      } else flash("恢复结果仍待人工确认", "danger");
      await refreshAccounts();
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) return;
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) flash(mutationError(error), "danger");
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken)) setBusy(false);
    }
  };

  return { operation, busy, setOperation: setOperationProjection, submit, refresh, recover, reset };
}
