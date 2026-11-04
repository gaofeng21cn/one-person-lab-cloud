import { useCallback, useEffect, useRef, useState } from "react";

import {
  getOperatorWorkspace,
  getOperatorWorkspaces
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  SourceEnvelope
} from "../api/dtos.ts";
import { authorizationFailureState } from "./authorization-error.ts";
import type { OperatorResourceReadController, RemoteState } from "./console-controller-types.ts";
import {
  applyOperatorResourceCompletion,
  createOperatorResourceDetailScope,
  createOperatorResourceListScope,
  createOperatorResourceReadState,
  isValidOperatorResourcePage,
  operatorResourceScopeIsCurrent,
  operatorResourceSourceMatchesScope,
  OPERATOR_RESOURCE_PAGE_SIZE,
  settleOperatorResourcePage,
  type OperatorResourceCompletion,
  type OperatorResourceDetailScope,
  type OperatorResourceListScope,
  type OperatorResourceReadEpoch,
  type OperatorResourceReadState,
  type OperatorResourceScope
} from "./operator-resource-read-controller-model.ts";

interface OperatorResourceReadDependencies {
  active: boolean;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

export type OperatorResourceReadCapability = OperatorResourceReadController;

type ProjectionKind = "workspaces" | "detail";

type ProjectionSourceMap = {
  workspaces: SourceEnvelope<OperatorWorkspacePageDTO>;
  detail: SourceEnvelope<OperatorWorkspaceDTO>;
};

type ProjectionValueMap = {
  workspaces: OperatorWorkspacePageDTO;
  detail: OperatorWorkspaceDTO;
};

type ProjectionScopeMap = {
  workspaces: OperatorResourceListScope;
  detail: OperatorResourceDetailScope;
};

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function sessionKey(session: AuthSession | null): string {
  return session ? `${session.user.id}\u0000${session.csrfToken}` : "";
}

function scopeForCurrentProjection(
  kind: ProjectionKind,
  epoch: OperatorResourceReadEpoch,
  generations: {
    list: number;
    detail: number;
  },
  page: number,
  workspaceId: string
): OperatorResourceScope | null {
  if (kind === "workspaces") {
    return createOperatorResourceListScope({ ...epoch, requestGeneration: generations.list, page });
  }
  return workspaceId
    ? createOperatorResourceDetailScope({ ...epoch, requestGeneration: generations.detail, workspaceId })
    : null;
}

export function useOperatorResourceReadController({
  active,
  currentSession,
  friendlyError,
  unavailableSource
}: OperatorResourceReadDependencies): OperatorResourceReadCapability {
  const [state, setState] = useState(createOperatorResourceReadState);
  const [page, setPage] = useState(1);
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState("");

  const activeRef = useRef(active);
  const pageRef = useRef(1);
  const listRequestedPageRef = useRef(1);
  const selectedWorkspaceIdRef = useRef("");
  const sessionIdentityRef = useRef("");
  const sessionGeneration = useRef(0);
  const routeGeneration = useRef(0);
  const listGeneration = useRef(0);
  const detailGeneration = useRef(0);
  activeRef.current = active;

  const currentEpoch = useCallback((): OperatorResourceReadEpoch => ({
    sessionGeneration: sessionGeneration.current,
    routeGeneration: routeGeneration.current
  }), []);

  const clearProjection = useCallback(() => {
    pageRef.current = 1;
    listRequestedPageRef.current = 1;
    selectedWorkspaceIdRef.current = "";
    setPage(1);
    setSelectedWorkspaceId("");
    setState(createOperatorResourceReadState());
  }, []);

  const reset = useCallback(() => {
    sessionGeneration.current += 1;
    routeGeneration.current += 1;
    listGeneration.current += 1;
    detailGeneration.current += 1;
    clearProjection();
  }, [clearProjection]);

  useEffect(() => {
    activeRef.current = active;
    reset();
    return () => {
      activeRef.current = false;
      reset();
    };
  }, [active, reset]);

  const syncSessionBoundary = useCallback((session: AuthSession | null) => {
    const nextKey = sessionKey(session);
    if (nextKey === sessionIdentityRef.current) return;
    sessionIdentityRef.current = nextKey;
    sessionGeneration.current += 1;
    listGeneration.current += 1;
    detailGeneration.current += 1;
    clearProjection();
  }, [clearProjection]);

  const requestOwnsScope = useCallback((scope: OperatorResourceScope, userId: string, csrfToken: string) => {
    const session = currentSession();
    if (!activeRef.current || session?.user.id !== userId || session.csrfToken !== csrfToken) return false;
    const current = scopeForCurrentProjection(
      scope.kind,
      currentEpoch(),
      {
        list: listGeneration.current,
        detail: detailGeneration.current
      },
      listRequestedPageRef.current,
      selectedWorkspaceIdRef.current
    );
    return current !== null && operatorResourceScopeIsCurrent(scope, current);
  }, [currentEpoch, currentSession]);

  const beginProjection = useCallback((kind: ProjectionKind) => {
    setState((current) => ({
      ...current,
      [kind]: {
        ...current[kind],
        loading: true,
        error: ""
      }
    }));
  }, []);

  const applyCompletion = useCallback((current: OperatorResourceReadState, completion: OperatorResourceCompletion) => {
    return applyOperatorResourceCompletion(current, completion) ?? current;
  }, []);

  const commitProjection = useCallback(<K extends ProjectionKind>(
    kind: K,
    scope: ProjectionScopeMap[K],
    source: ProjectionSourceMap[K],
    error = ""
  ) => {
    setState((current) => {
      switch (kind) {
        case "workspaces":
          return applyCompletion(current, {
            kind,
            activeScope: scope as ProjectionScopeMap["workspaces"],
            responseScope: scope as ProjectionScopeMap["workspaces"],
            source: source as ProjectionSourceMap["workspaces"],
            ...(error ? { error } : {})
          });
        case "detail":
          return applyCompletion(current, {
            kind,
            activeScope: scope as ProjectionScopeMap["detail"],
            responseScope: scope as ProjectionScopeMap["detail"],
            source: source as ProjectionSourceMap["detail"],
            ...(error ? { error } : {})
          });
      }
    });
  }, [applyCompletion]);

  // A 401/403 is an authorization decision, not a source outage. Keep it out of
  // the synthesized unavailable envelope so a missing operator privilege cannot
  // render as "source unavailable".
  const failProjection = useCallback((kind: ProjectionKind, message: string) => {
    setState((current) => {
      const failed = { value: null, loading: false, error: message };
      switch (kind) {
        case "workspaces":
          return { ...current, workspaces: failed };
        case "detail":
          return { ...current, detail: failed };
      }
    });
  }, []);

  const sourceMatchesScope = useCallback(<K extends ProjectionKind>(
    kind: K,
    scope: ProjectionScopeMap[K],
    source: ProjectionSourceMap[K]
  ) => {
    switch (kind) {
      case "workspaces":
        return operatorResourceSourceMatchesScope({
          kind,
          scope: scope as OperatorResourceListScope,
          source: source as ProjectionSourceMap["workspaces"]
        });
      case "detail":
        return operatorResourceSourceMatchesScope({
          kind,
          scope: scope as OperatorResourceDetailScope,
          source: source as ProjectionSourceMap["detail"]
        });
    }
  }, []);

  const settle = useCallback(async <K extends ProjectionKind>(
    kind: K,
    scope: ProjectionScopeMap[K],
    request: () => Promise<ProjectionSourceMap[K]>,
    fallbackSource: string
  ): Promise<ProjectionSourceMap[K] | null> => {
    const userId = currentSession()?.user.id || "";
    const csrfToken = currentSession()?.csrfToken || "";
    beginProjection(kind);
    try {
      const result = await request();
      if (!requestOwnsScope(scope, userId, csrfToken)) return null;
      if (!sourceMatchesScope(kind, scope, result)) {
        throw new Error(`operator_resource_${kind}_identity_mismatch`);
      }
      commitProjection(kind, scope, result);
      return result;
    } catch (error) {
      if (!requestOwnsScope(scope, userId, csrfToken)) return null;
      const authorization = authorizationFailureState(error);
      if (authorization) {
        failProjection(kind, authorization.error);
        return null;
      }
      commitProjection(
        kind,
        scope,
        unavailableSource<ProjectionValueMap[K]>(fallbackSource) as ProjectionSourceMap[K],
        friendlyError(error)
      );
      return null;
    }
  }, [beginProjection, commitProjection, currentSession, failProjection, friendlyError, requestOwnsScope, sourceMatchesScope, unavailableSource]);

  const loadList = useCallback(async (requestedPage: number) => {
    if (!isValidOperatorResourcePage(requestedPage)) return;
    const session = currentSession();
    if (!session || !activeRef.current) return;
    listRequestedPageRef.current = requestedPage;
    const scope = createOperatorResourceListScope({
      ...currentEpoch(),
      requestGeneration: ++listGeneration.current,
      page: requestedPage
    });
    const result = await settle(
      "workspaces",
      scope,
      () => getOperatorWorkspaces(requestedPage, OPERATOR_RESOURCE_PAGE_SIZE),
      "control-plane+fabric+sub2api"
    );
    if (!requestOwnsScope(scope, session.user.id, session.csrfToken)) return;
    const next = settleOperatorResourcePage(
      { page: pageRef.current, requestPage: listRequestedPageRef.current },
      requestedPage,
      result || unavailableSource<OperatorWorkspacePageDTO>("control-plane+fabric+sub2api")
    );
    listRequestedPageRef.current = next.requestPage;
    if (next.page !== pageRef.current) {
      pageRef.current = next.page;
      setPage(next.page);
    }
  }, [currentEpoch, currentSession, requestOwnsScope, settle, unavailableSource]);

  const loadDetail = useCallback(async (workspaceId: string) => {
    if (!workspaceId || selectedWorkspaceIdRef.current !== workspaceId) return;
    const scope = createOperatorResourceDetailScope({
      ...currentEpoch(),
      requestGeneration: ++detailGeneration.current,
      workspaceId
    });
    await settle(
      "detail",
      scope,
      () => getOperatorWorkspace(workspaceId),
      "control-plane+fabric+ledger"
    );
  }, [currentEpoch, settle]);

  const load = useCallback(async () => {
    const session = currentSession();
    if (!session || !activeRef.current) return;
    syncSessionBoundary(session);
    const selected = selectedWorkspaceIdRef.current;
    await Promise.allSettled([
      loadList(pageRef.current),
      ...(selected ? [loadDetail(selected)] : [])
    ]);
  }, [currentSession, loadDetail, loadList, syncSessionBoundary]);

  const changePage = useCallback(async (nextPage: number) => {
    const session = currentSession();
    if (!session || !activeRef.current || !isValidOperatorResourcePage(nextPage)) return;
    syncSessionBoundary(session);
    await loadList(nextPage);
  }, [currentSession, loadList, syncSessionBoundary]);

  const selectWorkspace = useCallback(async (workspaceId: string) => {
    const session = currentSession();
    if (!session || !activeRef.current) return;
    syncSessionBoundary(session);
    detailGeneration.current += 1;
    selectedWorkspaceIdRef.current = workspaceId;
    setSelectedWorkspaceId(workspaceId);
    setState((current) => ({
      ...current,
      detail: emptyRemote()
    }));
    if (!workspaceId) return;
    await loadDetail(workspaceId);
  }, [currentSession, loadDetail, syncSessionBoundary]);

  const refreshWorkspace = useCallback(async (workspaceId: string) => {
    const session = currentSession();
    if (!session || !activeRef.current || selectedWorkspaceIdRef.current !== workspaceId) return;
    syncSessionBoundary(session);
    await loadDetail(workspaceId);
  }, [currentSession, loadDetail, syncSessionBoundary]);

  const refresh = useCallback(async () => {
    await load();
  }, [load]);

  const pages = state.workspaces.value?.available
    ? Math.ceil(state.workspaces.value.data.total / state.workspaces.value.data.pageSize)
    : 0;

  return {
    workspaces: state.workspaces,
    detail: state.detail,
    page,
    pages,
    selectedWorkspaceId,
    load,
    changePage,
    selectWorkspace,
    refresh,
    refreshWorkspace,
    reset
  };
}
