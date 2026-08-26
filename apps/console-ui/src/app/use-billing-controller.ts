import { useCallback, useEffect, useRef, useState } from "react";

import { getBillingReceipt, getBillingReceipts } from "../api/console-read-api.ts";
import type {
  AuthSession,
  BillingReceipt,
  BillingReceiptPage,
  SourceEnvelope
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

export function useBillingController({
  route,
  currentSession,
  friendlyError,
  unavailableSource
}: BillingDependencies): BillingCapability {
  const [view, setView] = useState<BillingView>("terms");
  const [receipts, setReceipts] = useState<RemoteState<SourceEnvelope<BillingReceiptPage>>>(emptyRemote);
  const [detail, setDetail] = useState<RemoteState<SourceEnvelope<BillingReceipt>>>(emptyRemote);
  const [selectedReceiptId, setSelectedReceiptId] = useState("");
  const [cursorStack, setCursorStack] = useState<string[]>([]);

  const routeRef = useRef(route);
  const cursorRef = useRef("");
  const cursorStackRef = useRef<string[]>([]);
  const selectedReceiptIdRef = useRef("");
  const listGeneration = useRef(0);
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
    setView("terms");
    setReceipts(emptyRemote());
    closeReceipt();
    resetPagination();
  }, [closeReceipt, resetPagination]);

  useEffect(() => {
    listGeneration.current += 1;
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

  const loadOverview = useCallback(async () => {
    const session = currentSession();
    if (!session || routeRef.current !== "overview") return;
    resetPagination();
    await loadList(session, "overview", "", 3);
  }, [currentSession, loadList, resetPagination]);

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
