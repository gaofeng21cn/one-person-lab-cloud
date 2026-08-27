import { useCallback, useEffect, useRef, useState } from "react";

import { getWorkspaceRuntimeStatus } from "../api/workspaces-api.ts";
import type {
  AuthSession,
  SourceEnvelope,
  WorkspaceRuntimeDTO
} from "../api/dtos.ts";
import type { FabricRuntimeReadController } from "./console-controller-types.ts";
import {
  applyFabricRuntimeReadCompletion,
  createFabricRuntimeReadScope,
  createFabricRuntimeReadState,
  fabricRuntimeReadScopeIsCurrent,
  fabricRuntimeReadSourceMatchesScope,
  type FabricRuntimeReadScope
} from "./fabric-runtime-read-controller-model.ts";

export interface FabricRuntimeReadDependencies {
  active: boolean;
  workspaceId: string;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

export interface FabricRuntimeReadCapability extends FabricRuntimeReadController {
  load: () => Promise<void>;
  reset: () => void;
}

function sessionKey(session: AuthSession | null): string {
  return session ? `${session.user.id}\u0000${session.csrfToken}` : "";
}

export function useFabricRuntimeReadController({
  active,
  workspaceId,
  currentSession,
  friendlyError,
  unavailableSource
}: FabricRuntimeReadDependencies): FabricRuntimeReadCapability {
  const [state, setState] = useState(createFabricRuntimeReadState);
  const activeRef = useRef(active);
  const workspaceIdRef = useRef(workspaceId);
  const sessionIdentityRef = useRef("");
  const sessionGeneration = useRef(0);
  const routeGeneration = useRef(0);
  const requestGeneration = useRef(0);
  activeRef.current = active;
  workspaceIdRef.current = workspaceId;

  const clearProjection = useCallback(() => {
    setState(createFabricRuntimeReadState());
  }, []);

  const reset = useCallback(() => {
    sessionGeneration.current += 1;
    routeGeneration.current += 1;
    requestGeneration.current += 1;
    sessionIdentityRef.current = "";
    clearProjection();
  }, [clearProjection]);

  useEffect(() => {
    routeGeneration.current += 1;
    requestGeneration.current += 1;
    clearProjection();
    return () => {
      routeGeneration.current += 1;
      requestGeneration.current += 1;
      clearProjection();
    };
  }, [active, clearProjection, workspaceId]);

  const syncSessionBoundary = useCallback((session: AuthSession) => {
    const nextKey = sessionKey(session);
    if (nextKey === sessionIdentityRef.current) return;
    sessionIdentityRef.current = nextKey;
    sessionGeneration.current += 1;
    requestGeneration.current += 1;
    clearProjection();
  }, [clearProjection]);

  const currentScope = useCallback((): FabricRuntimeReadScope => createFabricRuntimeReadScope({
    sessionGeneration: sessionGeneration.current,
    routeGeneration: routeGeneration.current,
    requestGeneration: requestGeneration.current,
    workspaceId: workspaceIdRef.current
  }), []);

  const requestOwnsScope = useCallback((
    scope: FabricRuntimeReadScope,
    userId: string,
    csrfToken: string
  ) => {
    const session = currentSession();
    return activeRef.current
      && workspaceIdRef.current === scope.workspaceId
      && session?.user.id === userId
      && session.csrfToken === csrfToken
      && fabricRuntimeReadScopeIsCurrent(scope, currentScope());
  }, [currentScope, currentSession]);

  const commit = useCallback((
    scope: FabricRuntimeReadScope,
    source: SourceEnvelope<WorkspaceRuntimeDTO>,
    error = ""
  ) => {
    setState((current) => applyFabricRuntimeReadCompletion(current, {
      activeScope: currentScope(),
      responseScope: scope,
      source,
      ...(error ? { error } : {})
    }) ?? current);
  }, [currentScope]);

  const load = useCallback(async () => {
    const session = currentSession();
    const requestedWorkspaceId = workspaceIdRef.current;
    if (!session || !activeRef.current || !requestedWorkspaceId) return;
    syncSessionBoundary(session);
    const scope = createFabricRuntimeReadScope({
      sessionGeneration: sessionGeneration.current,
      routeGeneration: routeGeneration.current,
      requestGeneration: ++requestGeneration.current,
      workspaceId: requestedWorkspaceId
    });
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setState({ runtime: { value: null, loading: true, error: "" } });
    try {
      const result = await getWorkspaceRuntimeStatus(requestedWorkspaceId);
      if (!requestOwnsScope(scope, userId, csrfToken)) return;
      if (!fabricRuntimeReadSourceMatchesScope(scope, result)) {
        throw new Error("fabric_runtime_read_identity_mismatch");
      }
      commit(scope, result);
    } catch (error) {
      if (!requestOwnsScope(scope, userId, csrfToken)) return;
      commit(scope, unavailableSource<WorkspaceRuntimeDTO>("fabric"), friendlyError(error));
    }
  }, [commit, currentSession, friendlyError, requestOwnsScope, syncSessionBoundary, unavailableSource]);

  const refresh = useCallback(async () => {
    await load();
  }, [load]);

  return {
    runtime: state.runtime,
    load,
    refresh,
    reset
  };
}
