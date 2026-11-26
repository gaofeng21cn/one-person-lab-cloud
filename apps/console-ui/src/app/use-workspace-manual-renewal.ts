import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession } from "../api/dtos.ts";
import {
  createWorkspaceRenewalQuote,
  renewWorkspace,
  type WorkspaceRenewalQuote
} from "../api/workspace-renewal-api.ts";
import { getWorkspaceOwnerOperation } from "../api/workspaces-api.ts";
import { requestConfirm } from "./confirm-dialog-store.ts";
import {
  renewalConfirmationText,
  renewalFailureText,
  renewalOperationIsTerminal,
  renewalQuoteIsCurrent,
  renewalRequestFailureText,
  renewalSuccessText,
  type WorkspaceRenewalStage
} from "./workspace-manual-renewal-model.ts";

// The manual renewal runs one customer-visible sequence: the Catalog prices
// one renewal offer, the customer confirms that price, the Workspace owner
// accepts the renewal and the Console polls the owner's own operation until
// it reaches a terminal state. The Console adds no renewal rule of its own and
// never reports a renewal as done before the owner says so.

interface WorkspaceManualRenewalDependencies {
  session: AuthSession | null;
  workspaceId: string;
  computePlanId?: string;
  storagePlanId?: string;
  onRenewed: () => void;
}

export interface WorkspaceManualRenewalCapability {
  stage: WorkspaceRenewalStage;
  message: string;
  busy: boolean;
  quote: WorkspaceRenewalQuote | null;
  start: () => Promise<void>;
  reset: () => void;
}

const maxPolls = 120;

function sleep(milliseconds: number) {
  return new Promise<void>((resolve) => { window.setTimeout(resolve, milliseconds); });
}

export function useWorkspaceManualRenewal({
  session,
  workspaceId,
  computePlanId,
  storagePlanId,
  onRenewed
}: WorkspaceManualRenewalDependencies): WorkspaceManualRenewalCapability {
  const [stage, setStage] = useState<WorkspaceRenewalStage>("idle");
  const [message, setMessage] = useState("");
  const [quote, setQuote] = useState<WorkspaceRenewalQuote | null>(null);
  const generation = useRef(0);
  const scope = useRef({ userId: session?.user.id || "", csrfToken: session?.csrfToken || "", workspaceId });
  scope.current = { userId: session?.user.id || "", csrfToken: session?.csrfToken || "", workspaceId };

  const reset = useCallback(() => {
    generation.current += 1;
    setStage("idle");
    setMessage("");
    setQuote(null);
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id, workspaceId]);

  const start = useCallback(async () => {
    if (!session || !workspaceId || !computePlanId || !storagePlanId) return;
    if (stage !== "idle" && stage !== "failed" && stage !== "succeeded") return;
    const attempt = ++generation.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const owns = () => generation.current === attempt
      && scope.current.userId === userId
      && scope.current.csrfToken === csrfToken
      && scope.current.workspaceId === workspaceId;
    setStage("quoting");
    setMessage("");

    let offer: WorkspaceRenewalQuote;
    try {
      offer = await createWorkspaceRenewalQuote(
        { workspaceId, computePlanId, storagePlanId },
        csrfToken,
        `workspace-renewal-quote:${workspaceId}:${crypto.randomUUID()}`
      );
    } catch (error) {
      if (!owns()) return;
      setStage("failed");
      setMessage(renewalRequestFailureText(error));
      return;
    }
    if (!owns()) return;
    if (!renewalQuoteIsCurrent(offer, Date.now())) {
      setStage("failed");
      setMessage("续费报价已过期，请重新发起续费。");
      return;
    }
    setQuote(offer);
    setStage("confirming");

    const confirmed = await requestConfirm({
      title: "确认续费",
      description: renewalConfirmationText(offer),
      confirmLabel: "确认续费",
      cancelLabel: "取消"
    });
    if (!owns()) return;
    if (!confirmed) {
      setQuote(null);
      setStage("idle");
      return;
    }

    setStage("charging");
    let operation;
    try {
      operation = await renewWorkspace(
        workspaceId,
        offer.id,
        csrfToken,
        `workspace-renewal:${workspaceId}:${crypto.randomUUID()}`
      );
    } catch (error) {
      if (!owns()) return;
      setStage("failed");
      setMessage(renewalRequestFailureText(error));
      return;
    }
    if (!owns()) return;
    setStage("polling");

    let state = await getWorkspaceOwnerOperation(operation.operationId);
    for (let poll = 0; poll < maxPolls && !renewalOperationIsTerminal(state); poll += 1) {
      const wait = Math.min(Math.max(state.pollAfterSeconds ?? 5, 1), 30);
      await sleep(wait * 1000);
      if (!owns()) return;
      state = await getWorkspaceOwnerOperation(operation.operationId);
    }
    if (!owns()) return;
    if (!renewalOperationIsTerminal(state)) {
      setStage("failed");
      setMessage("续费结果待确认，请刷新订阅查看当前付费期。");
      return;
    }
    if (state.status === "succeeded") {
      setStage("succeeded");
      setMessage(renewalSuccessText());
      onRenewed();
      return;
    }
    setStage("failed");
    setMessage(renewalFailureText(state));
  }, [computePlanId, onRenewed, session, stage, storagePlanId, workspaceId]);

  return {
    stage,
    message,
    busy: stage === "quoting" || stage === "confirming" || stage === "charging" || stage === "polling",
    quote,
    start,
    reset
  };
}
