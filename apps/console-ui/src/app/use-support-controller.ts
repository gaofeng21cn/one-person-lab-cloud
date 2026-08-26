import { useCallback, useEffect, useRef, useState } from "react";

import {
  createSupportTicketMapping,
  getSupportTickets
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  CreateSupportTicketMappingRequest,
  SupportTicketPageDTO
} from "../api/dtos.ts";
import type { SupportController } from "./console-controller-types.ts";
import {
  resolveSupportMappingIntent,
  supportMappingReadbackMatches,
  supportMappingResponseMatches,
  type SupportMappingIntent
} from "./support-controller-model.ts";

interface SupportDependencies {
  session: AuthSession | null;
  currentMutationRequest: () => () => boolean;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
  mutationError: (error: unknown) => string;
}

export interface SupportCapability extends SupportController {
  reset: () => void;
}

export function useSupportController({
  session,
  currentMutationRequest,
  flash,
  friendlyError,
  mutationError
}: SupportDependencies): SupportCapability {
  const [tickets, setTickets] = useState<SupportTicketPageDTO | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const lifecycleGeneration = useRef(0);
  const readGeneration = useRef(0);
  const intent = useRef<SupportMappingIntent | null>(null);
  const scope = useRef({ userId: session?.user.id || "", csrfToken: session?.csrfToken || "" });
  scope.current = { userId: session?.user.id || "", csrfToken: session?.csrfToken || "" };

  const reset = useCallback(() => {
    lifecycleGeneration.current += 1;
    readGeneration.current += 1;
    intent.current = null;
    setTickets(null);
    setLoading(false);
    setError("");
    setBusy(false);
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  const isCurrent = useCallback((generation: number, requestStillCurrent: () => boolean, userId: string, csrfToken: string) => (
    generation === lifecycleGeneration.current
      && requestStillCurrent()
      && scope.current.userId === userId
      && scope.current.csrfToken === csrfToken
  ), []);

  const load = useCallback(async (): Promise<SupportTicketPageDTO | null> => {
    if (!session) return null;
    const generation = lifecycleGeneration.current;
    const read = ++readGeneration.current;
    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setLoading(true);
    setError("");
    try {
      const result = await getSupportTickets();
      if (!isCurrent(generation, requestStillCurrent, userId, csrfToken) || read !== readGeneration.current) return null;
      setTickets(result);
      return result;
    } catch (requestError) {
      if (!isCurrent(generation, requestStillCurrent, userId, csrfToken) || read !== readGeneration.current) return null;
      setTickets(null);
      setError(friendlyError(requestError));
      return null;
    } finally {
      if (isCurrent(generation, requestStillCurrent, userId, csrfToken) && read === readGeneration.current) setLoading(false);
    }
  }, [currentMutationRequest, friendlyError, isCurrent, session]);

  const createMapping = useCallback(async (input: Omit<CreateSupportTicketMappingRequest, "accountId">): Promise<boolean> => {
    if (!session || busy) return false;
    const requestStillCurrent = currentMutationRequest();
    const generation = lifecycleGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const request: CreateSupportTicketMappingRequest = { ...input, accountId: session.user.accountId };
    const nextIntent = resolveSupportMappingIntent(
      intent.current,
      request,
      () => `support-map:${crypto.randomUUID()}`
    );
    intent.current = nextIntent;
    setBusy(true);
    try {
      const response = await createSupportTicketMapping(nextIntent.input, csrfToken, nextIntent.idempotencyKey);
      if (!isCurrent(generation, requestStillCurrent, userId, csrfToken)) return false;
      if (!supportMappingResponseMatches(response, nextIntent.input)) throw new Error("support_mapping_response_mismatch");
      const readback = await load();
      if (!isCurrent(generation, requestStillCurrent, userId, csrfToken)) return false;
      if (!readback || !supportMappingReadbackMatches(readback, nextIntent.input)) {
        throw new Error("support_mapping_readback_mismatch");
      }
      intent.current = null;
      flash("外部工单映射已保存");
      return true;
    } catch (requestError) {
      if (!isCurrent(generation, requestStillCurrent, userId, csrfToken)) return false;
      flash(mutationError(requestError), "danger");
      return false;
    } finally {
      if (isCurrent(generation, requestStillCurrent, userId, csrfToken)) setBusy(false);
    }
  }, [busy, currentMutationRequest, flash, isCurrent, load, mutationError, session]);

  return { tickets, loading, error, busy, load, createMapping, reset };
}
