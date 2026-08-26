import { useCallback, useEffect, useRef, useState } from "react";

import {
  getGatewayKeys,
  getGatewayKeyUsage,
  getGatewayKeyUsageSummary
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  GatewayKeyPageDTO,
  GatewayKeyUsagePageDTO,
  GatewayUsagePeriod,
  GatewayUsageSummaryDTO,
  SourceEnvelope
} from "../api/dtos.ts";
import type { GatewayUsageController, RemoteState } from "./console-controller-types.ts";

interface GatewayUsageDependencies {
  active: boolean;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

export interface GatewayUsageCapability extends GatewayUsageController {
  load: () => Promise<void>;
  reset: () => void;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

export function useGatewayUsageController({
  active,
  currentSession,
  friendlyError,
  unavailableSource
}: GatewayUsageDependencies): GatewayUsageCapability {
  const [keys, setKeys] = useState<RemoteState<SourceEnvelope<GatewayKeyPageDTO>>>(emptyRemote);
  const [usage, setUsage] = useState<RemoteState<SourceEnvelope<GatewayKeyUsagePageDTO>>>(emptyRemote);
  const [summary, setSummary] = useState<RemoteState<SourceEnvelope<GatewayUsageSummaryDTO>>>(emptyRemote);
  const [selectedKeyId, setSelectedKeyId] = useState("");
  const [period, setPeriod] = useState<GatewayUsagePeriod>("month");
  const [page, setPage] = useState(1);

  const activeRef = useRef(active);
  const keysGeneration = useRef(0);
  const usageGeneration = useRef(0);
  const selectedKeyIdRef = useRef("");
  const periodRef = useRef<GatewayUsagePeriod>("month");
  activeRef.current = active;

  const requestOwnsScope = useCallback((userId: string, csrfToken: string) => {
    const session = currentSession();
    return activeRef.current
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const clearUsage = useCallback(() => {
    usageGeneration.current += 1;
    selectedKeyIdRef.current = "";
    setSelectedKeyId("");
    setPage(1);
    setUsage(emptyRemote());
    setSummary(emptyRemote());
  }, []);

  const reset = useCallback(() => {
    keysGeneration.current += 1;
    setKeys(emptyRemote());
    clearUsage();
    periodRef.current = "month";
    setPeriod("month");
  }, [clearUsage]);

  useEffect(() => {
    keysGeneration.current += 1;
    usageGeneration.current += 1;
  }, [active]);

  useEffect(() => reset, [reset]);

  const loadUsage = useCallback(async (
    session: AuthSession,
    keyId: string,
    requestedPage: number,
    requestedPeriod: GatewayUsagePeriod
  ) => {
    if (!activeRef.current || !keyId) return;
    const generation = ++usageGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setUsage((current) => ({ ...current, loading: true, error: "" }));
    setSummary((current) => ({ ...current, loading: true, error: "" }));

    const [usageResult, summaryResult] = await Promise.allSettled([
      getGatewayKeyUsage(keyId, requestedPage, 20, requestedPeriod),
      getGatewayKeyUsageSummary(keyId, requestedPeriod)
    ]);
    if (generation !== usageGeneration.current
      || selectedKeyIdRef.current !== keyId
      || !requestOwnsScope(userId, csrfToken)) return;

    if (usageResult.status === "fulfilled") {
      setUsage({ value: usageResult.value, loading: false, error: "" });
      setPage(requestedPage);
    } else {
      setUsage({
        value: unavailableSource("sub2api"),
        loading: false,
        error: friendlyError(usageResult.reason)
      });
    }
    if (summaryResult.status === "fulfilled") {
      setSummary({ value: summaryResult.value, loading: false, error: "" });
    } else {
      setSummary({
        value: unavailableSource("sub2api"),
        loading: false,
        error: friendlyError(summaryResult.reason)
      });
    }
  }, [friendlyError, requestOwnsScope, unavailableSource]);

  const load = useCallback(async () => {
    const session = currentSession();
    if (!activeRef.current || !session) return;
    const generation = ++keysGeneration.current;
    usageGeneration.current += 1;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setKeys((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await getGatewayKeys({ page: 1, pageSize: 20 });
      if (generation !== keysGeneration.current || !requestOwnsScope(userId, csrfToken)) return;
      setKeys({ value: result, loading: false, error: "" });
      if (!result.available) return;
      if (result.data.items.length === 0) {
        clearUsage();
        return;
      }

      const keyId = result.data.items.some((key) => key.id === selectedKeyIdRef.current)
        ? selectedKeyIdRef.current
        : result.data.items[0].id;
      selectedKeyIdRef.current = keyId;
      setSelectedKeyId(keyId);
      await loadUsage(session, keyId, 1, periodRef.current);
    } catch (error) {
      if (generation !== keysGeneration.current || !requestOwnsScope(userId, csrfToken)) return;
      setKeys({
        value: unavailableSource("sub2api"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [clearUsage, currentSession, friendlyError, loadUsage, requestOwnsScope, unavailableSource]);

  const selectKey = useCallback(async (keyId: string) => {
    const session = currentSession();
    if (!activeRef.current || !session || !keys.value?.available
      || !keys.value.data.items.some((key) => key.id === keyId)) return;
    selectedKeyIdRef.current = keyId;
    setSelectedKeyId(keyId);
    await loadUsage(session, keyId, 1, periodRef.current);
  }, [currentSession, keys.value, loadUsage]);

  const selectPeriod = useCallback(async (nextPeriod: GatewayUsagePeriod) => {
    const session = currentSession();
    const keyId = selectedKeyIdRef.current;
    if (!activeRef.current || !session || !keyId) return;
    periodRef.current = nextPeriod;
    setPeriod(nextPeriod);
    await loadUsage(session, keyId, 1, nextPeriod);
  }, [currentSession, loadUsage]);

  const changePage = useCallback(async (nextPage: number) => {
    const session = currentSession();
    const keyId = selectedKeyIdRef.current;
    if (!activeRef.current || !session || !keyId || nextPage < 1) return;
    await loadUsage(session, keyId, nextPage, periodRef.current);
  }, [currentSession, loadUsage]);

  return {
    keys,
    usage,
    summary,
    selectedKeyId,
    period,
    page,
    load,
    refresh: load,
    selectKey,
    selectPeriod,
    changePage,
    reset
  };
}
