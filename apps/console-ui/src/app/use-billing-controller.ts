import { useCallback, useEffect, useRef, useState } from "react";

import { getBillingReceipt, getBillingReceipts } from "../api/console-read-api.ts";
import {
  buildBillingSpendTrend,
  billingSpendTrendWindowStart,
  type BillingSpendTrend
} from "./customer-experience-model.ts";
import type {
  AuthSession,
  BillingReceipt,
  BillingReceiptPage,
  SourceEnvelope,
  UnavailableSource
} from "../api/dtos.ts";
import type {
  BillingController,
  BillingView,
  RemoteState
} from "./console-controller-types.ts";

type BillingRoute = "billing" | "overview" | "";

interface BillingDependencies {
  route: BillingRoute;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

export interface BillingCapability extends BillingController {
  loadOverview: () => Promise<void>;
  loadBilling: () => Promise<void>;
  reset: () => void;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

// 概览趋势必须覆盖完整窗口：Ledger 按 createdAt 倒序分页，逐页读到早于窗口起点的回执为止。
const TREND_WINDOW_DAYS = 14;
const TREND_PAGE_LIMIT = 100;
const TREND_MAX_PAGES = 20;

export function useBillingController({
  route,
  currentSession,
  friendlyError,
  unavailableSource
}: BillingDependencies): BillingCapability {
  const [view, setView] = useState<BillingView>("terms");
  const [receipts, setReceipts] = useState<RemoteState<SourceEnvelope<BillingReceiptPage>>>(emptyRemote);
  const [trend, setTrend] = useState<RemoteState<SourceEnvelope<BillingSpendTrend>>>(emptyRemote);
  const [detail, setDetail] = useState<RemoteState<SourceEnvelope<BillingReceipt>>>(emptyRemote);
  const [selectedReceiptId, setSelectedReceiptId] = useState("");
  const [cursorStack, setCursorStack] = useState<string[]>([]);

  const routeRef = useRef(route);
  const cursorRef = useRef("");
  const cursorStackRef = useRef<string[]>([]);
  const selectedReceiptIdRef = useRef("");
  const listGeneration = useRef(0);
  const trendGeneration = useRef(0);
  const detailGeneration = useRef(0);
  routeRef.current = route;

  const requestOwnsScope = useCallback((
    userId: string,
    csrfToken: string,
    expectedRoute: Exclude<BillingRoute, "">
  ) => {
    const session = currentSession();
    return routeRef.current === expectedRoute
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const closeReceipt = useCallback(() => {
    detailGeneration.current += 1;
    selectedReceiptIdRef.current = "";
    setSelectedReceiptId("");
    setDetail(emptyRemote());
  }, []);

  const resetPagination = useCallback(() => {
    cursorRef.current = "";
    cursorStackRef.current = [];
    setCursorStack([]);
  }, []);

  const reset = useCallback(() => {
    listGeneration.current += 1;
    trendGeneration.current += 1;
    setView("terms");
    setReceipts(emptyRemote());
    setTrend(emptyRemote());
    closeReceipt();
    resetPagination();
  }, [closeReceipt, resetPagination]);

  useEffect(() => {
    listGeneration.current += 1;
    trendGeneration.current += 1;
    detailGeneration.current += 1;
  }, [route]);

  useEffect(() => reset, [reset]);

  const loadList = useCallback(async (
    session: AuthSession,
    expectedRoute: Exclude<BillingRoute, "">,
    cursor: string,
    limit: number
  ) => {
    if (routeRef.current !== expectedRoute) return;
    const generation = ++listGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    closeReceipt();
    setReceipts((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await getBillingReceipts(cursor, limit);
      if (generation !== listGeneration.current
        || cursor !== cursorRef.current
        || !requestOwnsScope(userId, csrfToken, expectedRoute)) return;
      setReceipts({ value: result, loading: false, error: "" });
    } catch (error) {
      if (generation !== listGeneration.current
        || cursor !== cursorRef.current
        || !requestOwnsScope(userId, csrfToken, expectedRoute)) return;
      setReceipts({
        value: unavailableSource("ledger"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [closeReceipt, friendlyError, requestOwnsScope, unavailableSource]);

  const loadTrend = useCallback(async (
    session: AuthSession,
    expectedRoute: Exclude<BillingRoute, "">
  ) => {
    if (routeRef.current !== expectedRoute) return;
    const generation = ++trendGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const now = new Date();
    const windowStart = billingSpendTrendWindowStart(now, TREND_WINDOW_DAYS).getTime();
    setTrend((current) => ({ ...current, loading: true, error: "" }));
    const collected: BillingReceipt[] = [];
    let windowCovered = false;
    try {
      let cursor = "";
      for (let page = 0; page < TREND_MAX_PAGES; page++) {
        const result = await getBillingReceipts(cursor, TREND_PAGE_LIMIT);
        if (generation !== trendGeneration.current
          || !requestOwnsScope(userId, csrfToken, expectedRoute)) return;
        if (!result.available) {
          // 上游没有返回可用结果时原样保留来源与原因代码，概览不把它当成零费用。
          const unavailable = result as UnavailableSource;
          setTrend({ value: unavailable, loading: false, error: "" });
          return;
        }
        collected.push(...result.data.receipts);
        const oldest = result.data.receipts[result.data.receipts.length - 1];
        if (!result.data.hasMore) {
          windowCovered = true;
          break;
        }
        if (oldest && Date.parse(oldest.createdAt) < windowStart) {
          windowCovered = true;
          break;
        }
        if (!oldest || !result.data.nextCursor) break;
        cursor = result.data.nextCursor;
      }
      if (!windowCovered) {
        setTrend({
          value: unavailableSource<BillingSpendTrend>("ledger"),
          loading: false,
          error: `近 ${TREND_WINDOW_DAYS} 天费用记录数量超出单次读取上限，无法确认完整汇总。`
        });
        return;
      }
      const envelope: SourceEnvelope<BillingSpendTrend> = {
        source: "ledger",
        status: collected.length === 0 ? "empty" : "available",
        available: true,
        fetchedAt: new Date().toISOString(),
        data: buildBillingSpendTrend(collected, now, TREND_WINDOW_DAYS)
      };
      setTrend({ value: envelope, loading: false, error: "" });
    } catch (error) {
      if (generation !== trendGeneration.current
        || !requestOwnsScope(userId, csrfToken, expectedRoute)) return;
      setTrend({
        value: unavailableSource<BillingSpendTrend>("ledger"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [friendlyError, requestOwnsScope, unavailableSource]);

  const loadOverview = useCallback(async () => {
    const session = currentSession();
    if (!session || routeRef.current !== "overview") return;
    resetPagination();
    await Promise.all([
      loadList(session, "overview", "", 3),
      loadTrend(session, "overview")
    ]);
  }, [currentSession, loadList, loadTrend, resetPagination]);

  const loadBilling = useCallback(async () => {
    const session = currentSession();
    if (!session || routeRef.current !== "billing") return;
    await loadList(session, "billing", cursorRef.current, 20);
  }, [currentSession, loadList]);

  const refresh = useCallback(async () => {
    if (routeRef.current === "overview") await loadOverview();
    else if (routeRef.current === "billing") await loadBilling();
  }, [loadBilling, loadOverview]);

  const openReceipt = useCallback(async (receiptId: string) => {
    const session = currentSession();
    if (!receiptId || !session || routeRef.current !== "billing") return;
    closeReceipt();
    const generation = ++detailGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    selectedReceiptIdRef.current = receiptId;
    setSelectedReceiptId(receiptId);
    setDetail((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await getBillingReceipt(receiptId);
      if (generation !== detailGeneration.current
        || selectedReceiptIdRef.current !== receiptId
        || !requestOwnsScope(userId, csrfToken, "billing")) return;
      if (result.available && result.data.receiptId !== receiptId) {
        throw new Error("billing_receipt_identity_mismatch");
      }
      setDetail({ value: result, loading: false, error: "" });
    } catch (error) {
      if (generation !== detailGeneration.current
        || selectedReceiptIdRef.current !== receiptId
        || !requestOwnsScope(userId, csrfToken, "billing")) return;
      setDetail({
        value: unavailableSource("ledger"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [closeReceipt, currentSession, friendlyError, requestOwnsScope, unavailableSource]);

  const nextPage = useCallback(async () => {
    const session = currentSession();
    const page = receipts.value?.available ? receipts.value.data : null;
    if (!session || routeRef.current !== "billing" || receipts.loading || !page?.hasMore || !page.nextCursor) return;
    cursorStackRef.current = [...cursorStackRef.current, cursorRef.current];
    setCursorStack(cursorStackRef.current);
    cursorRef.current = page.nextCursor;
    await loadList(session, "billing", page.nextCursor, 20);
  }, [currentSession, loadList, receipts.loading, receipts.value]);

  const previousPage = useCallback(async () => {
    const session = currentSession();
    if (!session || routeRef.current !== "billing" || receipts.loading || cursorStackRef.current.length === 0) return;
    const previousCursor = cursorStackRef.current[cursorStackRef.current.length - 1] || "";
    cursorStackRef.current = cursorStackRef.current.slice(0, -1);
    setCursorStack(cursorStackRef.current);
    cursorRef.current = previousCursor;
    await loadList(session, "billing", previousCursor, 20);
  }, [currentSession, loadList, receipts.loading]);

  const page = receipts.value?.available ? receipts.value.data : null;
  return {
    view,
    setView,
    receipts,
    trend,
    detail,
    selectedReceiptId,
    pageNumber: cursorStack.length + 1,
    canNext: route === "billing" && !receipts.loading && Boolean(page?.hasMore && page.nextCursor),
    canPrevious: route === "billing" && !receipts.loading && cursorStack.length > 0,
    refresh,
    openReceipt,
    closeReceipt,
    nextPage,
    previousPage,
    loadOverview,
    loadBilling,
    reset
  };
}
