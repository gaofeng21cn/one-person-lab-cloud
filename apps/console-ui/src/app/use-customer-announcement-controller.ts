import { useCallback, useEffect, useRef, useState } from "react";

import { getAnnouncements, markAnnouncementRead } from "../api/console-read-api.ts";
import type {
  AnnouncementPageDTO,
  AuthSession,
  SourceEnvelope
} from "../api/dtos.ts";
import type { CustomerAnnouncementController, RemoteState } from "./console-controller-types.ts";
import {
  announcementReadReceiptMatches,
  announcementReadbackPreservesReceipts,
  resolveAnnouncementReadIntent,
  type AnnouncementReadIntent
} from "./customer-announcement-controller-model.ts";

export type CustomerAnnouncementScope = "overview" | "list" | "";

interface CustomerAnnouncementDependencies {
  scope: CustomerAnnouncementScope;
  currentSession: () => AuthSession | null;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

interface AnnouncementProjection {
  scope: CustomerAnnouncementScope;
  remote: RemoteState<SourceEnvelope<AnnouncementPageDTO>>;
}

type ScopeRead =
  | { state: "committed" }
  | { state: "conflict" }
  | { state: "failed" }
  | { state: "stale" };

export interface CustomerAnnouncementCapability extends CustomerAnnouncementController {
  load: () => Promise<void>;
  reset: () => void;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function pageSizeForScope(scope: CustomerAnnouncementScope) {
  if (scope === "overview") return 3;
  if (scope === "list") return 20;
  return 0;
}

export function useCustomerAnnouncementController({
  scope,
  currentSession,
  flash,
  friendlyError,
  unavailableSource
}: CustomerAnnouncementDependencies): CustomerAnnouncementCapability {
  const [projection, setProjection] = useState<AnnouncementProjection>({ scope: "", remote: emptyRemote() });
  const [busyAnnouncementId, setBusyAnnouncementId] = useState("");

  const scopeRef = useRef(scope);
  const scopeGeneration = useRef(0);
  const queryGeneration = useRef(0);
  const mutationGeneration = useRef(0);
  const claim = useRef<symbol | null>(null);
  const readIntents = useRef(new Map<string, AnnouncementReadIntent>());
  const confirmedReadAnnouncementIds = useRef(new Set<string>());
  scopeRef.current = scope;

  const requestOwnsSession = useCallback((generation: number, userId: string, csrfToken: string) => {
    const session = currentSession();
    return generation === mutationGeneration.current
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const requestOwnsScope = useCallback((
    generation: number,
    boundary: number,
    expectedScope: CustomerAnnouncementScope,
    userId: string,
    csrfToken: string
  ) => {
    const session = currentSession();
    return generation === queryGeneration.current
      && boundary === scopeGeneration.current
      && expectedScope !== ""
      && scopeRef.current === expectedScope
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const reset = useCallback(() => {
    scopeGeneration.current += 1;
    queryGeneration.current += 1;
    mutationGeneration.current += 1;
    readIntents.current.clear();
    confirmedReadAnnouncementIds.current.clear();
    claim.current = null;
    setBusyAnnouncementId("");
    setProjection({ scope: "", remote: emptyRemote() });
  }, []);

  useEffect(() => {
    scopeGeneration.current += 1;
    queryGeneration.current += 1;
  }, [scope]);

  useEffect(() => reset, [reset]);

  const readScope = useCallback(async (
    expectedScope: CustomerAnnouncementScope,
    session: AuthSession,
    expectedReadAnnouncementId?: string
  ): Promise<ScopeRead> => {
    const pageSize = pageSizeForScope(expectedScope);
    if (!pageSize || scopeRef.current !== expectedScope) return { state: "stale" };

    const generation = ++queryGeneration.current;
    const boundary = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setProjection((current) => ({
      scope: expectedScope,
      remote: {
        value: current.scope === expectedScope ? current.remote.value : null,
        loading: true,
        error: ""
      }
    }));
    try {
      const result = await getAnnouncements(1, pageSize);
      if (!requestOwnsScope(generation, boundary, expectedScope, userId, csrfToken)) return { state: "stale" };
      if (result.available && (result.data.page !== 1 || result.data.pageSize !== pageSize)) {
        throw new Error("customer_announcement_page_mismatch");
      }
      if (!result.available && expectedReadAnnouncementId !== undefined) {
        setProjection((current) => current.scope === expectedScope
          ? { scope: expectedScope, remote: { ...current.remote, loading: false } }
          : current);
        return { state: "failed" };
      }
      if (result.available && !announcementReadbackPreservesReceipts(
        result.data,
        confirmedReadAnnouncementIds.current
      )) {
        setProjection((current) => current.scope === expectedScope
          ? { scope: expectedScope, remote: { ...current.remote, loading: false } }
          : current);
        return { state: "conflict" };
      }
      setProjection({ scope: expectedScope, remote: { value: result, loading: false, error: "" } });
      return { state: "committed" };
    } catch (error) {
      if (!requestOwnsScope(generation, boundary, expectedScope, userId, csrfToken)) return { state: "stale" };
      if (expectedReadAnnouncementId !== undefined) {
        setProjection((current) => current.scope === expectedScope
          ? { scope: expectedScope, remote: { ...current.remote, loading: false } }
          : current);
        return { state: "failed" };
      }
      setProjection({
        scope: expectedScope,
        remote: {
          value: unavailableSource("control-plane"),
          loading: false,
          error: friendlyError(error)
        }
      });
      return { state: "failed" };
    }
  }, [friendlyError, requestOwnsScope, unavailableSource]);

  const load = useCallback(async () => {
    const session = currentSession();
    const expectedScope = scopeRef.current;
    if (!session || !expectedScope) return;
    await readScope(expectedScope, session);
  }, [currentSession, readScope]);

  const releaseClaim = useCallback((owner: symbol) => {
    if (claim.current !== owner) return;
    claim.current = null;
    setBusyAnnouncementId("");
  }, []);

  const projectReceipt = useCallback((announcementId: string) => {
    const currentScope = scopeRef.current;
    if (!currentScope) return;
    setProjection((current) => {
      if (current.scope !== currentScope || !current.remote.value?.available) return current;
      return {
        scope: current.scope,
        remote: {
          ...current.remote,
          value: {
            ...current.remote.value,
            data: {
              ...current.remote.value.data,
              items: current.remote.value.data.items.map((announcement) => announcement.id === announcementId
                ? { ...announcement, read: true }
                : announcement)
            }
          }
        }
      };
    });
  }, []);

  const markRead = useCallback(async (announcementId: string) => {
    const session = currentSession();
    if (!session || !scopeRef.current || claim.current !== null) return;

    const intent = resolveAnnouncementReadIntent(
      readIntents.current.get(announcementId) ?? null,
      announcementId,
      () => `announcement-read:${announcementId}:${crypto.randomUUID()}`
    );
    readIntents.current.set(announcementId, intent);
    const owner = Symbol(`customer-announcement-read:${announcementId}`);
    const generation = mutationGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    claim.current = owner;
    setBusyAnnouncementId(announcementId);
    try {
      const command = await markAnnouncementRead(announcementId, csrfToken, intent.idempotencyKey);
      if (claim.current !== owner || !requestOwnsSession(generation, userId, csrfToken)) return;
      if (!announcementReadReceiptMatches(command, announcementId)) {
        throw new Error("customer_announcement_read_identity_mismatch");
      }
      confirmedReadAnnouncementIds.current.add(announcementId);
      if (readIntents.current.get(announcementId) === intent) readIntents.current.delete(announcementId);
      projectReceipt(announcementId);

      const expectedScope = scopeRef.current;
      if (expectedScope) {
        const readback = await readScope(expectedScope, session, announcementId);
        if (claim.current !== owner || !requestOwnsSession(generation, userId, csrfToken)) return;
        if (readback.state === "stale") return;
        if (readback.state === "conflict") {
          throw new Error("customer_announcement_readback_mismatch");
        }
      }
    } catch (error) {
      if (claim.current === owner && scopeRef.current
        && requestOwnsSession(generation, userId, csrfToken)) {
        flash(friendlyError(error), "danger");
      }
    } finally {
      releaseClaim(owner);
    }
  }, [currentSession, flash, friendlyError, projectReceipt, readScope, releaseClaim, requestOwnsSession]);

  const announcements = projection.scope === scope ? projection.remote : emptyRemote<SourceEnvelope<AnnouncementPageDTO>>();
  return {
    announcements,
    busyAnnouncementId,
    refresh: load,
    markRead,
    load,
    reset
  };
}
