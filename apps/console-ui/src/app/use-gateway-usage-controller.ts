import { useCallback, useEffect, useRef, useState } from "react";

import {
  getGatewayKey,
  getGatewayKeys,
  getGatewayKeyUsage,
  getGatewayKeyUsageSummary
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  GatewayKeyPageDTO,
  GatewayKeySummaryDTO,
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

type KeySelectionStatus = GatewayUsageController["selection"]["status"];

function apiErrorCode(error: unknown) {
  const payload = error && typeof error === "object" && "payload" in error
    ? (error as { payload?: unknown }).payload
    : null;
  return payload && typeof payload === "object" ? String((payload as { error?: unknown }).error || "") : "";
}

export function useGatewayUsageController({
  active,
  currentSession,
  friendlyError,
  unavailableSource
}: GatewayUsageDependencies): GatewayUsageCapability {
  const [keys, setKeys] = useState<RemoteState<SourceEnvelope<GatewayKeyPageDTO>>>(emptyRemote);
  const [usage, setUsage] = useState<RemoteState<SourceEnvelope<GatewayKeyUsagePageDTO>>>(emptyRemote);
  const [summary, setSummary] = useState<RemoteState<SourceEnvelope<GatewayUsageSummaryDTO>>>(emptyRemote);
  const [selection, setSelection] = useState<GatewayUsageController["selection"]>({ key: null, status: "idle" });
  const [keySearch, setKeySearch] = useState("");
  const [period, setPeriod] = useState<GatewayUsagePeriod>("month");
  const [page, setPage] = useState(1);

  const activeRef = useRef(active);
  const keysGeneration = useRef(0);
  const selectionGeneration = useRef(0);
  const usageGeneration = useRef(0);
  const summaryGeneration = useRef(0);
  const selectedKeyRef = useRef<GatewayKeySummaryDTO | null>(null);
  const selectedKeyIdRef = useRef("");
  const periodRef = useRef<GatewayUsagePeriod>("month");
  const keyQueryRef = useRef({ page: 1, search: "" });
  const usageTargetPageRef = useRef(1);
  activeRef.current = active;

  const requestOwnsScope = useCallback((userId: string, csrfToken: string) => {
    const session = currentSession();
    return activeRef.current
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const clearResults = useCallback(() => {
    usageGeneration.current += 1;
    summaryGeneration.current += 1;
    setPage(1);
    usageTargetPageRef.current = 1;
    setUsage(emptyRemote());
    setSummary(emptyRemote());
  }, []);

  const clearSelection = useCallback((status: KeySelectionStatus = "idle") => {
    selectedKeyRef.current = null;
    selectedKeyIdRef.current = "";
    setSelection({ key: null, status });
    clearResults();
  }, [clearResults]);

  const reset = useCallback(() => {
    keysGeneration.current += 1;
    selectionGeneration.current += 1;
    setKeys(emptyRemote());
    setKeySearch("");
    keyQueryRef.current = { page: 1, search: "" };
    clearSelection();
    periodRef.current = "month";
    setPeriod("month");
  }, [clearSelection]);

  useEffect(() => {
    keysGeneration.current += 1;
    selectionGeneration.current += 1;
    usageGeneration.current += 1;
    summaryGeneration.current += 1;
  }, [active]);

  useEffect(() => reset, [reset]);

  const usageRequestOwnsScope = useCallback((
    userId: string,
    csrfToken: string,
    keyId: string,
    requestedPeriod: GatewayUsagePeriod
  ) => requestOwnsScope(userId, csrfToken)
    && selectedKeyIdRef.current === keyId
    && periodRef.current === requestedPeriod, [requestOwnsScope]);

  const loadSummary = useCallback(async (
    session: AuthSession,
    keyId: string,
    requestedPeriod: GatewayUsagePeriod
  ) => {
    if (!activeRef.current || !keyId) return;
    const generation = ++summaryGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setSummary({ value: null, loading: true, error: "" });
    try {
      const result = await getGatewayKeyUsageSummary(keyId, requestedPeriod);
      if (generation !== summaryGeneration.current
        || !usageRequestOwnsScope(userId, csrfToken, keyId, requestedPeriod)) return;
      setSummary({ value: result, loading: false, error: "" });
    } catch (error) {
      if (generation !== summaryGeneration.current
        || !usageRequestOwnsScope(userId, csrfToken, keyId, requestedPeriod)) return;
      setSummary({
        value: unavailableSource("sub2api"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [friendlyError, unavailableSource, usageRequestOwnsScope]);

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
    usageTargetPageRef.current = requestedPage;
    setUsage({ value: null, loading: true, error: "" });
    try {
      const result = await getGatewayKeyUsage(keyId, requestedPage, 20, requestedPeriod);
      if (generation !== usageGeneration.current
        || !usageRequestOwnsScope(userId, csrfToken, keyId, requestedPeriod)) return;
      if (result.available && (result.data.page !== requestedPage || result.data.pageSize !== 20)) {
        throw new Error("gateway_usage_page_mismatch");
      }
      setUsage({ value: result, loading: false, error: "" });
      if (result.available) setPage(requestedPage);
    } catch (error) {
      if (generation !== usageGeneration.current
        || !usageRequestOwnsScope(userId, csrfToken, keyId, requestedPeriod)) return;
      setUsage({
        value: unavailableSource("sub2api"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [friendlyError, unavailableSource, usageRequestOwnsScope]);

  const loadScope = useCallback(async (
    session: AuthSession,
    keyId: string,
    requestedPeriod: GatewayUsagePeriod
  ) => {
    setPage(1);
    usageTargetPageRef.current = 1;
    await Promise.all([
      loadSummary(session, keyId, requestedPeriod),
      loadUsage(session, keyId, 1, requestedPeriod)
    ]);
  }, [loadSummary, loadUsage]);

  const loadKeys = useCallback(async (nextQuery: { page: number; search: string }) => {
    const session = currentSession();
    if (!activeRef.current || !session) return null;
    const query = { page: nextQuery.page, search: nextQuery.search.trim() };
    const generation = ++keysGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    keyQueryRef.current = query;
    setKeySearch(query.search);
    setKeys({ value: null, loading: true, error: "" });
    try {
      const result = await getGatewayKeys({ page: query.page, pageSize: 20, search: query.search });
      if (generation !== keysGeneration.current || !requestOwnsScope(userId, csrfToken)) return null;
      if (result.available && result.data.page !== query.page) throw new Error("gateway_key_page_mismatch");
      setKeys({ value: result, loading: false, error: "" });
      return result;
    } catch (error) {
      if (generation !== keysGeneration.current || !requestOwnsScope(userId, csrfToken)) return null;
      setKeys({
        value: unavailableSource("sub2api"),
        loading: false,
        error: friendlyError(error)
      });
      return null;
    }
  }, [currentSession, friendlyError, requestOwnsScope, unavailableSource]);

  const load = useCallback(async () => {
    const session = currentSession();
    if (!activeRef.current || !session) return;
    const selectedKey = selectedKeyRef.current;
    if (!selectedKey) {
      const result = await loadKeys({ page: 1, search: "" });
      if (!result?.available || result.data.items.length === 0) {
        if (result?.available) clearSelection();
        return;
      }
      const firstKey = result.data.items[0];
      selectedKeyRef.current = firstKey;
      selectedKeyIdRef.current = firstKey.id;
      setSelection({ key: firstKey, status: "ready" });
      await loadScope(session, firstKey.id, periodRef.current);
      return;
    }

    const generation = ++selectionGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    clearResults();
    setSelection({ key: selectedKey, status: "confirming" });
    try {
      const result = await getGatewayKey(selectedKey.id);
      if (generation !== selectionGeneration.current
        || selectedKeyIdRef.current !== selectedKey.id
        || !requestOwnsScope(userId, csrfToken)) return;
      if (!result.available || result.data.id !== selectedKey.id) {
        setSelection({ key: selectedKey, status: "unavailable" });
        return;
      }
      selectedKeyRef.current = result.data;
      setSelection({ key: result.data, status: "ready" });
      await loadScope(session, result.data.id, periodRef.current);
    } catch (error) {
      if (generation !== selectionGeneration.current
        || selectedKeyIdRef.current !== selectedKey.id
        || !requestOwnsScope(userId, csrfToken)) return;
      if (apiErrorCode(error) === "gateway_key_not_found") {
        clearSelection("missing");
        return;
      }
      setSelection({ key: selectedKey, status: "unavailable" });
    }
  }, [clearResults, clearSelection, currentSession, loadKeys, loadScope, requestOwnsScope]);

  const selectKey = useCallback(async (keyId: string) => {
    const session = currentSession();
    const selectedKey = keys.value?.available
      ? keys.value.data.items.find((key) => key.id === keyId)
      : null;
    if (!activeRef.current || !session || !selectedKey) return;
    selectionGeneration.current += 1;
    selectedKeyRef.current = selectedKey;
    selectedKeyIdRef.current = keyId;
    setSelection({ key: selectedKey, status: "ready" });
    await loadScope(session, keyId, periodRef.current);
  }, [currentSession, keys.value, loadScope]);

  const selectPeriod = useCallback(async (nextPeriod: GatewayUsagePeriod) => {
    const session = currentSession();
    const keyId = selectedKeyIdRef.current;
    if (!activeRef.current || !session || !keyId || nextPeriod === periodRef.current) return;
    periodRef.current = nextPeriod;
    setPeriod(nextPeriod);
    await loadScope(session, keyId, nextPeriod);
  }, [currentSession, loadScope]);

  const changePage = useCallback(async (nextPage: number) => {
    const session = currentSession();
    const keyId = selectedKeyIdRef.current;
    if (!activeRef.current || !session || !keyId || nextPage < 1) return;
    await loadUsage(session, keyId, nextPage, periodRef.current);
  }, [currentSession, loadUsage]);

  const searchKeys = useCallback(async (search: string) => {
    await loadKeys({ page: 1, search });
  }, [loadKeys]);

  const changeKeyPage = useCallback(async (nextPage: number) => {
    const pages = keys.value?.available ? keys.value.data.pages : 0;
    const currentPage = keys.value?.available ? keys.value.data.page : 0;
    if (nextPage < 1 || nextPage > pages || nextPage === currentPage) return;
    await loadKeys({ page: nextPage, search: keyQueryRef.current.search });
  }, [keys.value, loadKeys]);

  const retryKeys = useCallback(async () => {
    await loadKeys(keyQueryRef.current);
  }, [loadKeys]);

  const cancelKeyQuery = useCallback(() => {
    keysGeneration.current += 1;
    setKeys((current) => ({ ...current, loading: false }));
  }, []);

  const retrySummary = useCallback(async () => {
    const session = currentSession();
    const keyId = selectedKeyIdRef.current;
    if (!activeRef.current || !session || !keyId) return;
    await loadSummary(session, keyId, periodRef.current);
  }, [currentSession, loadSummary]);

  const retryUsage = useCallback(async () => {
    const session = currentSession();
    const keyId = selectedKeyIdRef.current;
    if (!activeRef.current || !session || !keyId) return;
    await loadUsage(session, keyId, usageTargetPageRef.current, periodRef.current);
  }, [currentSession, loadUsage]);

  return {
    keys,
    usage,
    summary,
    selection,
    keySearch,
    selectedKeyId: selection.key?.id || "",
    period,
    page,
    load,
    refresh: load,
    selectKey,
    selectPeriod,
    changePage,
    searchKeys,
    changeKeyPage,
    retryKeys,
    cancelKeyQuery,
    retrySummary,
    retryUsage,
    reset
  };
}
