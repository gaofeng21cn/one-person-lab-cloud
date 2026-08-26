import { useCallback, useEffect, useRef, useState } from "react";

import {
  createOperatorAnnouncement,
  getOperatorAnnouncements,
  publishOperatorAnnouncement,
  withdrawOperatorAnnouncement
} from "../api/console-read-api.ts";
import type {
  AnnouncementDTO,
  AnnouncementDraftRequest,
  AuthSession,
  OperatorAnnouncementPageDTO,
  SourceEnvelope
} from "../api/dtos.ts";
import type { OperatorAnnouncementController, RemoteState } from "./console-controller-types.ts";
import {
  announcementProjectionMatches,
  createAnnouncementCommandMatches,
  publishAnnouncementCommandMatches,
  resolveAnnouncementCreateIntent,
  resolveAnnouncementPublishIntent,
  withdrawAnnouncementCommandMatches,
  type AnnouncementCreateIntent,
  type AnnouncementPublishIntent
} from "./operator-announcement-controller-model.ts";

interface OperatorAnnouncementDependencies {
  active: boolean;
  currentSession: () => AuthSession | null;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
  mutationError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

interface AnnouncementWithdrawIntent {
  announcementId: string;
  idempotencyKey: string;
}

export interface OperatorAnnouncementCapability extends OperatorAnnouncementController {
  load: () => Promise<void>;
  reset: () => void;
}

const pageSize = 20;
const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

export function useOperatorAnnouncementController({
  active,
  currentSession,
  flash,
  friendlyError,
  mutationError,
  unavailableSource
}: OperatorAnnouncementDependencies): OperatorAnnouncementCapability {
  const [announcements, setAnnouncements] = useState<RemoteState<SourceEnvelope<OperatorAnnouncementPageDTO>>>(emptyRemote);
  const [createBusy, setCreateBusy] = useState(false);
  const [busyAnnouncementIds, setBusyAnnouncementIds] = useState<string[]>([]);

  const activeRef = useRef(active);
  const scopeGeneration = useRef(0);
  const listGeneration = useRef(0);
  const createGeneration = useRef(0);
  const createClaim = useRef<symbol | null>(null);
  const announcementClaims = useRef(new Map<string, symbol>());
  const createIntent = useRef<AnnouncementCreateIntent | null>(null);
  const publishIntents = useRef(new Map<string, AnnouncementPublishIntent>());
  const withdrawIntents = useRef(new Map<string, AnnouncementWithdrawIntent>());
  activeRef.current = active;

  const requestOwnsScope = useCallback((generation: number, userId: string, csrfToken: string) => {
    const session = currentSession();
    return generation === scopeGeneration.current
      && activeRef.current
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const reset = useCallback(() => {
    scopeGeneration.current += 1;
    listGeneration.current += 1;
    createGeneration.current += 1;
    createIntent.current = null;
    publishIntents.current.clear();
    withdrawIntents.current.clear();
    setAnnouncements(emptyRemote());
  }, []);

  useEffect(() => {
    scopeGeneration.current += 1;
    listGeneration.current += 1;
    createGeneration.current += 1;
  }, [active]);

  useEffect(() => reset, [reset]);

  const commitProjection = useCallback((result: SourceEnvelope<OperatorAnnouncementPageDTO>) => {
    setAnnouncements({ value: result, loading: false, error: "" });
  }, []);

  const load = useCallback(async () => {
    const session = currentSession();
    if (!session || !activeRef.current) return;
    const generation = ++listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setAnnouncements((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await getOperatorAnnouncements(1, pageSize);
      if (generation !== listGeneration.current || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (result.available && (result.data.page !== 1 || result.data.pageSize !== pageSize)) {
        throw new Error("operator_announcement_page_mismatch");
      }
      commitProjection(result);
    } catch (error) {
      if (generation !== listGeneration.current || !requestOwnsScope(scope, userId, csrfToken)) return;
      setAnnouncements({
        value: unavailableSource("control-plane"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [commitProjection, currentSession, friendlyError, requestOwnsScope, unavailableSource]);

  const readback = useCallback(async (
    session: AuthSession,
    scope: number,
    command: AnnouncementDTO
  ) => {
    const result = await getOperatorAnnouncements(1, pageSize);
    if (!requestOwnsScope(scope, session.user.id, session.csrfToken)) return null;
    if (!result.available || result.data.page !== 1 || result.data.pageSize !== pageSize) return null;
    const announcement = result.data.items.find((candidate) => announcementProjectionMatches(command, candidate));
    return announcement ? { announcement, result } : null;
  }, [requestOwnsScope]);

  const create = useCallback(async (input: AnnouncementDraftRequest) => {
    const session = currentSession();
    if (!session || !activeRef.current || createClaim.current !== null) return false;
    const intent = resolveAnnouncementCreateIntent(
      createIntent.current,
      input,
      () => `announcement-create:${crypto.randomUUID()}`
    );
    createIntent.current = intent;
    const claim = Symbol("operator-announcement-create");
    const generation = ++createGeneration.current;
    const projectionGeneration = listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    createClaim.current = claim;
    setCreateBusy(true);
    try {
      const command = await createOperatorAnnouncement(intent.input, csrfToken, intent.idempotencyKey);
      if (generation !== createGeneration.current || createClaim.current !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return false;
      if (!createAnnouncementCommandMatches(command, intent.input)) {
        throw new Error("operator_announcement_create_identity_mismatch");
      }
      const projection = await readback(session, scope, command);
      if (generation !== createGeneration.current || createClaim.current !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return false;
      if (!projection) throw new Error("operator_announcement_create_readback_mismatch");
      if (projectionGeneration === listGeneration.current) commitProjection(projection.result);
      if (createIntent.current === intent) createIntent.current = null;
      flash("公告草稿已创建");
      return true;
    } catch (error) {
      if (generation === createGeneration.current && createClaim.current === claim
        && requestOwnsScope(scope, userId, csrfToken)) flash(mutationError(error), "danger");
      return false;
    } finally {
      if (createClaim.current === claim) {
        createClaim.current = null;
        setCreateBusy(false);
      }
    }
  }, [commitProjection, currentSession, flash, mutationError, readback, requestOwnsScope]);

  const currentAnnouncement = useCallback((announcementId: string) => announcements.value?.available
    ? announcements.value.data.items.find((announcement) => announcement.id === announcementId) ?? null
    : null, [announcements.value]);

  const claimAnnouncement = useCallback((announcementId: string) => {
    if (announcementClaims.current.has(announcementId)) return null;
    const claim = Symbol(`operator-announcement:${announcementId}`);
    announcementClaims.current.set(announcementId, claim);
    setBusyAnnouncementIds((current) => current.includes(announcementId) ? current : [...current, announcementId]);
    return claim;
  }, []);

  const releaseAnnouncement = useCallback((announcementId: string, claim: symbol) => {
    if (announcementClaims.current.get(announcementId) !== claim) return;
    announcementClaims.current.delete(announcementId);
    setBusyAnnouncementIds((current) => current.filter((id) => id !== announcementId));
  }, []);

  const publish = useCallback(async (announcementId: string) => {
    const session = currentSession();
    const announcement = currentAnnouncement(announcementId);
    if (!session || !activeRef.current || !announcement || !window.confirm("确认发布公告？")) return;
    const claim = claimAnnouncement(announcementId);
    if (!claim) return;
    const intent = resolveAnnouncementPublishIntent(
      publishIntents.current.get(announcementId) ?? null,
      announcement,
      () => new Date().toISOString(),
      () => `announcement-publish:${announcementId}:${crypto.randomUUID()}`
    );
    publishIntents.current.set(announcementId, intent);
    const projectionGeneration = listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    try {
      const command = await publishOperatorAnnouncement(announcementId, intent.input, csrfToken, intent.idempotencyKey);
      if (announcementClaims.current.get(announcementId) !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!publishAnnouncementCommandMatches(command, announcementId, intent.input)) {
        throw new Error("operator_announcement_publish_identity_mismatch");
      }
      const projection = await readback(session, scope, command);
      if (announcementClaims.current.get(announcementId) !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!projection) throw new Error("operator_announcement_publish_readback_mismatch");
      if (projectionGeneration === listGeneration.current) commitProjection(projection.result);
      if (publishIntents.current.get(announcementId) === intent) publishIntents.current.delete(announcementId);
      flash(command.status === "scheduled" ? "公告已排期" : "公告已发布");
    } catch (error) {
      if (announcementClaims.current.get(announcementId) === claim
        && requestOwnsScope(scope, userId, csrfToken)) flash(mutationError(error), "danger");
    } finally {
      releaseAnnouncement(announcementId, claim);
    }
  }, [claimAnnouncement, commitProjection, currentAnnouncement, currentSession, flash, mutationError, readback, releaseAnnouncement, requestOwnsScope]);

  const withdraw = useCallback(async (announcementId: string) => {
    const session = currentSession();
    const announcement = currentAnnouncement(announcementId);
    if (!session || !activeRef.current || !announcement || !window.confirm("确认撤下公告？")) return;
    const claim = claimAnnouncement(announcementId);
    if (!claim) return;
    const intent = withdrawIntents.current.get(announcementId) ?? {
      announcementId,
      idempotencyKey: `announcement-withdraw:${announcementId}:${crypto.randomUUID()}`
    };
    withdrawIntents.current.set(announcementId, intent);
    const projectionGeneration = listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    try {
      const command = await withdrawOperatorAnnouncement(announcementId, csrfToken, intent.idempotencyKey);
      if (announcementClaims.current.get(announcementId) !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!withdrawAnnouncementCommandMatches(command, announcementId)) {
        throw new Error("operator_announcement_withdraw_identity_mismatch");
      }
      const projection = await readback(session, scope, command);
      if (announcementClaims.current.get(announcementId) !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!projection) throw new Error("operator_announcement_withdraw_readback_mismatch");
      if (projectionGeneration === listGeneration.current) commitProjection(projection.result);
      if (withdrawIntents.current.get(announcementId) === intent) withdrawIntents.current.delete(announcementId);
      flash("公告已撤下");
    } catch (error) {
      if (announcementClaims.current.get(announcementId) === claim
        && requestOwnsScope(scope, userId, csrfToken)) flash(mutationError(error), "danger");
    } finally {
      releaseAnnouncement(announcementId, claim);
    }
  }, [claimAnnouncement, commitProjection, currentAnnouncement, currentSession, flash, mutationError, readback, releaseAnnouncement, requestOwnsScope]);

  return {
    announcements,
    createBusy,
    busyAnnouncementIds,
    refresh: load,
    create,
    publish,
    withdraw,
    load,
    reset
  };
}
