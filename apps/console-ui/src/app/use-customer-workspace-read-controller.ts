import { useCallback, useEffect, useRef, useState } from "react";

import type {
  AuthSession,
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData
} from "../api/dtos.ts";
import { findWorkspaceInPages, getWorkspaces } from "../api/workspaces-api.ts";
import type { CustomerWorkspaceReadController, WorkspaceSourceProjectionLease } from "./console-controller-types.ts";
import {
  CUSTOMER_WORKSPACE_LIST_PAGE_SIZE,
  applyCustomerWorkspaceCompletion,
  commitCustomerWorkspaceDetailProjectionLease,
  createCustomerWorkspaceDetailScope,
  createCustomerWorkspaceDetailProjectionLease,
  createCustomerWorkspaceListScope,
  createCustomerWorkspaceReadState,
  customerWorkspaceRouteReadPlan,
  customerWorkspaceRouteScopeKey,
  customerWorkspaceDetailProjectionLeaseIsCurrent,
  customerWorkspaceScopeIsCurrent,
  customerWorkspaceSourceMatchesScope,
  isValidCustomerWorkspacePage,
  settleCustomerWorkspacePage,
  type CustomerWorkspaceDetailScope,
  type CustomerWorkspaceListScope,
  type CustomerWorkspaceReadEpoch,
  type CustomerWorkspaceReadState,
  type CustomerWorkspaceRouteScope
} from "./customer-workspace-read-controller-model.ts";

interface CustomerWorkspaceReadDependencies {
  scope: CustomerWorkspaceRouteScope;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

export interface CustomerWorkspaceReadCapability extends CustomerWorkspaceReadController {
  load: () => Promise<SourceEnvelope<WorkspaceDTO | null> | null>;
  workspaceDetailProjectionLease: () => WorkspaceSourceProjectionLease;
  applyWorkspaceReadback: (readback: SourceEnvelope<WorkspaceDTO | null>) => boolean;
  reset: () => void;
}

function sessionKey(session: AuthSession | null): string {
  return session ? `${session.user.id}\u0000${session.csrfToken}` : "";
}

export function useCustomerWorkspaceReadController({
  scope,
  currentSession,
  friendlyError,
  unavailableSource
}: CustomerWorkspaceReadDependencies): CustomerWorkspaceReadCapability {
  const [state, setState] = useState(createCustomerWorkspaceReadState);
  const [page, setPage] = useState(1);

  const scopeRef = useRef(scope);
  const routeKeyRef = useRef(customerWorkspaceRouteScopeKey(scope));
  const sessionIdentityRef = useRef("");
  const sessionGeneration = useRef(0);
  const routeGeneration = useRef(0);
  const listGeneration = useRef(0);
  const detailGeneration = useRef(0);
  const committedPageRef = useRef(1);
  const listRequestedPageRef = useRef(1);
  const listRequestedPageSizeRef = useRef<number>(CUSTOMER_WORKSPACE_LIST_PAGE_SIZE);
  scopeRef.current = scope;

  const clearProjection = useCallback(() => {
    committedPageRef.current = 1;
    listRequestedPageRef.current = 1;
    listRequestedPageSizeRef.current = CUSTOMER_WORKSPACE_LIST_PAGE_SIZE;
    setPage(1);
    setState(createCustomerWorkspaceReadState());
  }, []);

  const reset = useCallback(() => {
    sessionGeneration.current += 1;
    routeGeneration.current += 1;
    listGeneration.current += 1;
    detailGeneration.current += 1;
    sessionIdentityRef.current = "";
    clearProjection();
  }, [clearProjection]);

  const syncSessionBoundary = useCallback((session: AuthSession | null) => {
    const nextKey = sessionKey(session);
    if (nextKey === sessionIdentityRef.current) return;
    sessionIdentityRef.current = nextKey;
    sessionGeneration.current += 1;
    listGeneration.current += 1;
    detailGeneration.current += 1;
    clearProjection();
  }, [clearProjection]);

  const syncRouteBoundary = useCallback((nextRouteKey: string) => {
    if (nextRouteKey === routeKeyRef.current) return;
    routeKeyRef.current = nextRouteKey;
    routeGeneration.current += 1;
    listGeneration.current += 1;
    detailGeneration.current += 1;
    setState((current) => ({
      ...current,
      detail: { value: null, loading: false, error: "" },
      ...(scopeRef.current.kind === "inactive"
        ? { workspaces: { value: null, loading: false, error: "" } }
        : {})
    }));
    if (scopeRef.current.kind === "inactive") {
      committedPageRef.current = 1;
      listRequestedPageRef.current = 1;
      setPage(1);
    }
  }, []);

  useEffect(() => {
    syncRouteBoundary(customerWorkspaceRouteScopeKey(scope));
  }, [scope, syncRouteBoundary]);

  useEffect(() => () => {
    reset();
  }, [reset]);

  const currentEpoch = useCallback((): CustomerWorkspaceReadEpoch => ({
    sessionGeneration: sessionGeneration.current,
    routeGeneration: routeGeneration.current
  }), []);

  const requestOwnsScope = useCallback((
    requestScope: CustomerWorkspaceListScope | CustomerWorkspaceDetailScope,
    userId: string,
    csrfToken: string,
    requestRouteKey: string
  ): boolean => {
    const session = currentSession();
    if (session?.user.id !== userId || session.csrfToken !== csrfToken
      || customerWorkspaceRouteScopeKey(scopeRef.current) !== requestRouteKey) return false;

    const activeScope = requestScope.kind === "list"
      ? createCustomerWorkspaceListScope({
          ...currentEpoch(),
          requestGeneration: listGeneration.current,
          page: listRequestedPageRef.current,
          pageSize: listRequestedPageSizeRef.current
        })
      : scopeRef.current.kind === "detail"
        ? createCustomerWorkspaceDetailScope({
            ...currentEpoch(),
            requestGeneration: detailGeneration.current,
            workspaceId: scopeRef.current.workspaceId
          })
        : null;
    return activeScope !== null && customerWorkspaceScopeIsCurrent(requestScope, activeScope);
  }, [currentEpoch, currentSession]);

  const beginProjection = useCallback((kind: "list" | "detail") => {
    setState((current) => ({
      ...current,
      [kind === "list" ? "workspaces" : "detail"]: {
        ...current[kind === "list" ? "workspaces" : "detail"],
        loading: true,
        error: ""
      }
    } as CustomerWorkspaceReadState));
  }, []);

  const commitList = useCallback((
    requestScope: CustomerWorkspaceListScope,
    source: SourceEnvelope<WorkspaceListData>,
    error = ""
  ) => {
    setState((current) => applyCustomerWorkspaceCompletion(current, {
      kind: "list",
      activeScope: requestScope,
      responseScope: requestScope,
      source,
      ...(error ? { error } : {})
    }) ?? current);
  }, []);

  const commitDetail = useCallback((
    requestScope: CustomerWorkspaceDetailScope,
    source: SourceEnvelope<WorkspaceDTO | null>,
    error = ""
  ) => {
    setState((current) => applyCustomerWorkspaceCompletion(current, {
      kind: "detail",
      activeScope: requestScope,
      responseScope: requestScope,
      source,
      ...(error ? { error } : {})
    }) ?? current);
  }, []);

  const loadList = useCallback(async (
    requestedPage: number,
    requestedPageSize: number
  ): Promise<void> => {
    const activeSession = currentSession();
    if (!activeSession) return;
    syncSessionBoundary(activeSession);
    const requestRouteKey = customerWorkspaceRouteScopeKey(scopeRef.current);
    syncRouteBoundary(requestRouteKey);
    const requestScope = createCustomerWorkspaceListScope({
      ...currentEpoch(),
      requestGeneration: ++listGeneration.current,
      page: requestedPage,
      pageSize: requestedPageSize
    });
    listRequestedPageRef.current = requestedPage;
    listRequestedPageSizeRef.current = requestedPageSize;
    const userId = activeSession.user.id;
    const csrfToken = activeSession.csrfToken;
    beginProjection("list");

    try {
      const result = await getWorkspaces(requestedPage, requestedPageSize);
      if (!requestOwnsScope(requestScope, userId, csrfToken, requestRouteKey)) return;
      if (!customerWorkspaceSourceMatchesScope({ kind: "list", scope: requestScope, source: result })) {
        throw new Error("customer_workspace_list_identity_mismatch");
      }
      commitList(requestScope, result);
      const settled = settleCustomerWorkspacePage(
        { page: committedPageRef.current, requestPage: listRequestedPageRef.current },
        requestedPage,
        requestedPageSize,
        result
      );
      committedPageRef.current = settled.page;
      listRequestedPageRef.current = settled.requestPage;
      setPage(settled.page);
    } catch (error) {
      if (!requestOwnsScope(requestScope, userId, csrfToken, requestRouteKey)) return;
      listRequestedPageRef.current = committedPageRef.current;
      commitList(
        requestScope,
        unavailableSource<WorkspaceListData>("control-plane"),
        friendlyError(error)
      );
    }
  }, [beginProjection, commitList, currentEpoch, currentSession, friendlyError, requestOwnsScope, syncRouteBoundary, syncSessionBoundary, unavailableSource]);

  const loadDetail = useCallback(async (
    workspaceId: string
  ): Promise<SourceEnvelope<WorkspaceDTO | null> | null> => {
    const activeSession = currentSession();
    if (!activeSession || !workspaceId) return null;
    syncSessionBoundary(activeSession);
    const requestRouteKey = customerWorkspaceRouteScopeKey(scopeRef.current);
    syncRouteBoundary(requestRouteKey);
    const requestScope = createCustomerWorkspaceDetailScope({
      ...currentEpoch(),
      requestGeneration: ++detailGeneration.current,
      workspaceId
    });
    const userId = activeSession.user.id;
    const csrfToken = activeSession.csrfToken;
    beginProjection("detail");

    try {
      const result = await findWorkspaceInPages(workspaceId);
      if (!requestOwnsScope(requestScope, userId, csrfToken, requestRouteKey)) return null;
      if (!customerWorkspaceSourceMatchesScope({ kind: "detail", scope: requestScope, source: result })) {
        throw new Error("customer_workspace_detail_identity_mismatch");
      }
      commitDetail(requestScope, result);
      return result;
    } catch (error) {
      if (!requestOwnsScope(requestScope, userId, csrfToken, requestRouteKey)) return null;
      const fallback = unavailableSource<WorkspaceDTO | null>("control-plane");
      commitDetail(requestScope, fallback, friendlyError(error));
      return fallback;
    }
  }, [beginProjection, commitDetail, currentEpoch, currentSession, friendlyError, requestOwnsScope, syncRouteBoundary, syncSessionBoundary, unavailableSource]);

  const load = useCallback(async (): Promise<SourceEnvelope<WorkspaceDTO | null> | null> => {
    const plan = customerWorkspaceRouteReadPlan(scopeRef.current, committedPageRef.current);
    if (!plan) return null;
    if (plan.kind === "detail") return loadDetail(plan.workspaceId);
    await loadList(plan.page, plan.pageSize);
    return null;
  }, [loadDetail, loadList]);

  const changePage = useCallback(async (nextPage: number) => {
    if (!isValidCustomerWorkspacePage(nextPage)
      || !["list", "terms"].includes(scopeRef.current.kind)) return;
    await loadList(nextPage, CUSTOMER_WORKSPACE_LIST_PAGE_SIZE);
  }, [loadList]);

  const workspaceDetailProjectionLease = useCallback((): WorkspaceSourceProjectionLease => {
    const activeSession = currentSession();
    const activeScope = scopeRef.current;
    const requestRouteKey = customerWorkspaceRouteScopeKey(activeScope);
    const epoch = currentEpoch();
    const userId = activeSession?.user.id || "";
    const csrfToken = activeSession?.csrfToken || "";
    const workspaceId = activeScope.kind === "detail" ? activeScope.workspaceId : "";

    const currentDetailScope = () => createCustomerWorkspaceDetailScope({
      sessionGeneration: sessionGeneration.current,
      routeGeneration: routeGeneration.current,
      requestGeneration: detailGeneration.current,
      workspaceId
    });
    const lease = createCustomerWorkspaceDetailProjectionLease(createCustomerWorkspaceDetailScope({
      ...epoch,
      requestGeneration: detailGeneration.current,
      workspaceId
    }));

    const isCurrent = () => {
      const session = currentSession();
      return Boolean(workspaceId)
        && session?.user.id === userId
        && session.csrfToken === csrfToken
        && customerWorkspaceRouteScopeKey(scopeRef.current) === requestRouteKey
        && sessionGeneration.current === epoch.sessionGeneration
        && routeGeneration.current === epoch.routeGeneration
        && customerWorkspaceDetailProjectionLeaseIsCurrent(lease, currentDetailScope());
    };

    return {
      isCurrent,
      commit: () => {
        if (!isCurrent()) return false;
        const committedScope = commitCustomerWorkspaceDetailProjectionLease(lease, currentDetailScope());
        if (!committedScope) return false;
        detailGeneration.current = committedScope.requestGeneration;
        return true;
      }
    };
  }, [currentEpoch, currentSession]);

  const applyWorkspaceReadback = useCallback((
    readback: SourceEnvelope<WorkspaceDTO | null>
  ): boolean => {
    const activeScope = scopeRef.current;
    if (activeScope.kind !== "detail" || !readback.available || !readback.data) return false;
    const validationScope = createCustomerWorkspaceDetailScope({
      ...currentEpoch(),
      requestGeneration: detailGeneration.current,
      workspaceId: activeScope.workspaceId
    });
    if (!customerWorkspaceSourceMatchesScope({
      kind: "detail",
      scope: validationScope,
      source: readback
    })) return false;

    const workspace = readback.data;
    setState((current) => {
      const list = current.workspaces.value;
      const workspaces = list?.available
        ? {
            ...current.workspaces,
            value: {
              ...list,
              data: {
                ...list.data,
                items: list.data.items.map((item) => item.id === workspace.id ? workspace : item)
              }
            }
          }
        : current.workspaces;
      return {
        ...current,
        workspaces,
        detail: { value: readback, loading: false, error: "" }
      };
    });
    return true;
  }, [currentEpoch]);

  const activeWorkspace = scope.kind === "detail"
    && state.detail.value?.available
    && state.detail.value.data?.id === scope.workspaceId
    ? state.detail.value.data
    : null;
  const pages = state.workspaces.value?.available
    ? Math.ceil(state.workspaces.value.data.total / state.workspaces.value.data.pageSize)
    : 0;

  return {
    workspaces: state.workspaces,
    detail: state.detail,
    page,
    pages,
    activeWorkspace,
    load,
    refresh: load,
    changePage,
    workspaceDetailProjectionLease,
    applyWorkspaceReadback,
    reset
  };
}
