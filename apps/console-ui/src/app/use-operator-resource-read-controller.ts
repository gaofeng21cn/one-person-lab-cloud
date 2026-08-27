import { useCallback, useEffect, useRef, useState } from "react";

import {
  getOperatorWorkspace,
  getOperatorWorkspaceRuntimeImagePolicy,
  getOperatorWorkspaceRuntimeImageReplacementPreview,
  getOperatorWorkspaces
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  OperatorWorkspaceRuntimeImagePolicyDTO,
  OperatorWorkspaceRuntimeImagePreviewDTO,
  SourceEnvelope
} from "../api/dtos.ts";
import type { RemoteState } from "./console-controller-types.ts";
import {
  applyOperatorResourceCompletion,
  createOperatorResourceDetailScope,
  createOperatorResourceListScope,
  createOperatorResourcePolicyScope,
  createOperatorResourcePreviewScope,
  createOperatorResourceReadState,
  isValidOperatorResourcePage,
  operatorResourceScopeIsCurrent,
  operatorResourceSourceMatchesScope,
  OPERATOR_RESOURCE_PAGE_SIZE,
  settleOperatorResourcePage,
  type OperatorResourceCompletion,
  type OperatorResourceDetailScope,
  type OperatorResourceListScope,
  type OperatorResourcePolicyScope,
  type OperatorResourcePreviewScope,
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

export interface OperatorResourceReadCapability {
  workspaces: RemoteState<SourceEnvelope<OperatorWorkspacePageDTO>>;
  detail: RemoteState<SourceEnvelope<OperatorWorkspaceDTO>>;
  imagePolicy: RemoteState<SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>>;
  imagePreview: RemoteState<SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>>;
  page: number;
  pages: number;
  selectedWorkspaceId: string;
  load: () => Promise<void>;
  changePage: (page: number) => Promise<void>;
  selectWorkspace: (workspaceId: string) => Promise<void>;
  refresh: () => Promise<void>;
  refreshWorkspace: (workspaceId: string) => Promise<void>;
  refreshPreview: (workspaceId: string) => Promise<void>;
  reset: () => void;
}

type ProjectionKind = "workspaces" | "detail" | "imagePolicy" | "imagePreview";

type ProjectionSourceMap = {
  workspaces: SourceEnvelope<OperatorWorkspacePageDTO>;
  detail: SourceEnvelope<OperatorWorkspaceDTO>;
  imagePolicy: SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>;
  imagePreview: SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>;
};

type ProjectionValueMap = {
  workspaces: OperatorWorkspacePageDTO;
  detail: OperatorWorkspaceDTO;
  imagePolicy: OperatorWorkspaceRuntimeImagePolicyDTO;
  imagePreview: OperatorWorkspaceRuntimeImagePreviewDTO;
};

type ProjectionScopeMap = {
  workspaces: OperatorResourceListScope;
  detail: OperatorResourceDetailScope;
  imagePolicy: OperatorResourcePolicyScope;
  imagePreview: OperatorResourcePreviewScope;
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
    policy: number;
    preview: number;
  },
  page: number,
  workspaceId: string
): OperatorResourceScope | null {
  if (kind === "workspaces") {
    return createOperatorResourceListScope({ ...epoch, requestGeneration: generations.list, page });
  }
  if (kind === "detail") {
    return workspaceId
      ? createOperatorResourceDetailScope({ ...epoch, requestGeneration: generations.detail, workspaceId })
      : null;
  }
  if (kind === "imagePolicy") {
    return createOperatorResourcePolicyScope({ ...epoch, requestGeneration: generations.policy });
  }
  return workspaceId
    ? createOperatorResourcePreviewScope({ ...epoch, requestGeneration: generations.preview, workspaceId })
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
  const policyGeneration = useRef(0);
  const previewGeneration = useRef(0);
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
    policyGeneration.current += 1;
    previewGeneration.current += 1;
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
    policyGeneration.current += 1;
    previewGeneration.current += 1;
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
        detail: detailGeneration.current,
        policy: policyGeneration.current,
        preview: previewGeneration.current
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
        case "imagePolicy":
          return applyCompletion(current, {
            kind,
            activeScope: scope as ProjectionScopeMap["imagePolicy"],
            responseScope: scope as ProjectionScopeMap["imagePolicy"],
            source: source as ProjectionSourceMap["imagePolicy"],
            ...(error ? { error } : {})
          });
        case "imagePreview":
          return applyCompletion(current, {
            kind,
            activeScope: scope as ProjectionScopeMap["imagePreview"],
            responseScope: scope as ProjectionScopeMap["imagePreview"],
            source: source as ProjectionSourceMap["imagePreview"],
            ...(error ? { error } : {})
          });
      }
    });
  }, [applyCompletion]);

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
      case "imagePolicy":
        return operatorResourceSourceMatchesScope({
          kind,
          scope: scope as OperatorResourcePolicyScope,
          source: source as ProjectionSourceMap["imagePolicy"]
        });
      case "imagePreview":
        return operatorResourceSourceMatchesScope({
          kind,
          scope: scope as OperatorResourcePreviewScope,
          source: source as ProjectionSourceMap["imagePreview"]
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
      commitProjection(
        kind,
        scope,
        unavailableSource<ProjectionValueMap[K]>(fallbackSource) as ProjectionSourceMap[K],
        friendlyError(error)
      );
      return null;
    }
  }, [beginProjection, commitProjection, currentSession, friendlyError, requestOwnsScope, sourceMatchesScope, unavailableSource]);

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

  const loadPolicy = useCallback(async () => {
    const scope = createOperatorResourcePolicyScope({
      ...currentEpoch(),
      requestGeneration: ++policyGeneration.current
    });
    await settle(
      "imagePolicy",
      scope,
      () => getOperatorWorkspaceRuntimeImagePolicy(),
      "control-plane"
    );
  }, [currentEpoch, settle]);

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

  const loadPreview = useCallback(async (workspaceId: string) => {
    if (!workspaceId || selectedWorkspaceIdRef.current !== workspaceId) return;
    const scope = createOperatorResourcePreviewScope({
      ...currentEpoch(),
      requestGeneration: ++previewGeneration.current,
      workspaceId
    });
    await settle(
      "imagePreview",
      scope,
      () => getOperatorWorkspaceRuntimeImageReplacementPreview(workspaceId),
      "control-plane+fabric"
    );
  }, [currentEpoch, settle]);

  const load = useCallback(async () => {
    const session = currentSession();
    if (!session || !activeRef.current) return;
    syncSessionBoundary(session);
    await Promise.allSettled([
      loadList(pageRef.current),
      loadPolicy()
    ]);
  }, [currentSession, loadList, loadPolicy, syncSessionBoundary]);

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
    previewGeneration.current += 1;
    selectedWorkspaceIdRef.current = workspaceId;
    setSelectedWorkspaceId(workspaceId);
    setState((current) => ({
      ...current,
      detail: emptyRemote(),
      imagePreview: emptyRemote()
    }));
    if (!workspaceId) return;
    await Promise.allSettled([
      loadDetail(workspaceId),
      loadPreview(workspaceId)
    ]);
  }, [currentSession, loadDetail, loadPreview, syncSessionBoundary]);

  const refreshWorkspace = useCallback(async (workspaceId: string) => {
    const session = currentSession();
    if (!session || !activeRef.current || selectedWorkspaceIdRef.current !== workspaceId) return;
    syncSessionBoundary(session);
    await loadDetail(workspaceId);
  }, [currentSession, loadDetail, syncSessionBoundary]);

  const refreshPreview = useCallback(async (workspaceId: string) => {
    const session = currentSession();
    if (!session || !activeRef.current || selectedWorkspaceIdRef.current !== workspaceId) return;
    syncSessionBoundary(session);
    await loadPreview(workspaceId);
  }, [currentSession, loadPreview, syncSessionBoundary]);

  const refresh = useCallback(async () => {
    await load();
  }, [load]);

  const pages = state.workspaces.value?.available
    ? Math.ceil(state.workspaces.value.data.total / state.workspaces.value.data.pageSize)
    : 0;

  return {
    workspaces: state.workspaces,
    detail: state.detail,
    imagePolicy: state.imagePolicy,
    imagePreview: state.imagePreview,
    page,
    pages,
    selectedWorkspaceId,
    load,
    changePage,
    selectWorkspace,
    refresh,
    refreshWorkspace,
    refreshPreview,
    reset
  };
}
