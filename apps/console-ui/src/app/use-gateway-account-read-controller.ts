import { useCallback, useEffect, useRef, useState } from "react";

import {
  getGatewayAccountUsageSummary,
  getGatewayBalanceHistory,
  getGatewayEndpoint,
  getGatewayWallet
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  SourceEnvelope
} from "../api/dtos.ts";
import type { GatewayAccountReadController } from "./console-controller-types.ts";
import {
  GATEWAY_BALANCE_HISTORY_PAGE_SIZE,
  applyGatewayAccountReadCompletion,
  beginGatewayAccountReadProjection,
  createGatewayAccountReadRequestScope,
  createGatewayAccountReadState,
  gatewayAccountReadPlan,
  gatewayAccountReadScopeIsCurrent,
  gatewayAccountReadSourceMatchesScope,
  invalidateGatewayAccountReadEpoch,
  type GatewayAccountReadCompletion,
  type GatewayAccountReadEpoch,
  type GatewayAccountReadProjectionKind,
  type GatewayAccountReadProjectionSourceMap,
  type GatewayAccountReadProjectionValueMap,
  type GatewayAccountReadRequestScope,
  type GatewayAccountReadRequestScopeMap,
  type GatewayAccountReadRouteScope
} from "./gateway-account-read-controller-model.ts";

interface GatewayAccountReadDependencies {
  scope: GatewayAccountReadRouteScope;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

export interface GatewayAccountReadCapability extends GatewayAccountReadController {
  load: () => Promise<void>;
  reset: () => void;
}

type ProjectionGenerations = Record<GatewayAccountReadProjectionKind, number>;

const createProjectionGenerations = (): ProjectionGenerations => ({
  wallet: 0,
  accountUsage: 0,
  balanceHistory: 0,
  endpoint: 0
});

function sessionIdentity(session: AuthSession): string {
  return `${session.user.id}\u0000${session.csrfToken}`;
}

export function useGatewayAccountReadController({
  scope,
  currentSession,
  friendlyError,
  unavailableSource
}: GatewayAccountReadDependencies): GatewayAccountReadCapability {
  const [state, setState] = useState(createGatewayAccountReadState);

  const scopeRef = useRef(scope);
  const sessionIdentityRef = useRef("");
  const epochRef = useRef<GatewayAccountReadEpoch>({ sessionGeneration: 0, routeGeneration: 0 });
  const projectionGenerations = useRef<ProjectionGenerations>(createProjectionGenerations());
  const requestedBalanceHistoryPageRef = useRef(1);
  const committedBalanceHistoryPageRef = useRef(1);
  scopeRef.current = scope;

  const invalidateProjectionRequests = useCallback(() => {
    projectionGenerations.current.wallet += 1;
    projectionGenerations.current.accountUsage += 1;
    projectionGenerations.current.balanceHistory += 1;
    projectionGenerations.current.endpoint += 1;
  }, []);

  const clearProjection = useCallback(() => {
    requestedBalanceHistoryPageRef.current = 1;
    committedBalanceHistoryPageRef.current = 1;
    setState(createGatewayAccountReadState());
  }, []);

  const reset = useCallback(() => {
    epochRef.current = invalidateGatewayAccountReadEpoch(epochRef.current, "reset");
    invalidateProjectionRequests();
    sessionIdentityRef.current = "";
    clearProjection();
  }, [clearProjection, invalidateProjectionRequests]);

  useEffect(() => {
    epochRef.current = invalidateGatewayAccountReadEpoch(epochRef.current, "route");
    invalidateProjectionRequests();
    if (scope === "inactive") clearProjection();
  }, [clearProjection, invalidateProjectionRequests, scope]);

  useEffect(() => () => {
    scopeRef.current = "inactive";
    epochRef.current = invalidateGatewayAccountReadEpoch(epochRef.current, "reset");
    invalidateProjectionRequests();
  }, [invalidateProjectionRequests]);

  const syncSessionBoundary = useCallback((session: AuthSession) => {
    const nextIdentity = sessionIdentity(session);
    if (nextIdentity === sessionIdentityRef.current) return;
    sessionIdentityRef.current = nextIdentity;
    epochRef.current = invalidateGatewayAccountReadEpoch(epochRef.current, "session");
    invalidateProjectionRequests();
    clearProjection();
  }, [clearProjection, invalidateProjectionRequests]);

  const currentScopeFor = useCallback((kind: GatewayAccountReadProjectionKind): GatewayAccountReadRequestScope => {
    const input = {
      ...epochRef.current,
      routeScope: scopeRef.current,
      requestGeneration: projectionGenerations.current[kind]
    };
    if (kind === "balanceHistory") {
      return createGatewayAccountReadRequestScope(kind, {
        ...input,
        page: requestedBalanceHistoryPageRef.current
      });
    }
    switch (kind) {
      case "wallet":
        return createGatewayAccountReadRequestScope(kind, input);
      case "accountUsage":
        return createGatewayAccountReadRequestScope(kind, input);
      case "endpoint":
        return createGatewayAccountReadRequestScope(kind, input);
    }
  }, []);

  const requestOwnsScope = useCallback((
    requestScope: GatewayAccountReadRequestScope,
    userId: string,
    csrfToken: string
  ) => {
    const session = currentSession();
    if (session?.user.id !== userId || session.csrfToken !== csrfToken
      || requestScope.routeScope !== scopeRef.current
      || !gatewayAccountReadPlan(scopeRef.current).includes(requestScope.kind)) {
      return false;
    }
    return gatewayAccountReadScopeIsCurrent(requestScope, currentScopeFor(requestScope.kind));
  }, [currentScopeFor, currentSession]);

  const commit = useCallback((completion: GatewayAccountReadCompletion) => {
    setState((current) => applyGatewayAccountReadCompletion(current, completion) ?? current);
  }, []);

  const settle = useCallback(async <K extends GatewayAccountReadProjectionKind>(
    session: AuthSession,
    requestScope: GatewayAccountReadRequestScopeMap[K],
    request: () => Promise<GatewayAccountReadProjectionSourceMap[K]>
  ) => {
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setState((current) => beginGatewayAccountReadProjection(current, requestScope.kind));
    try {
      const result = await request();
      if (!requestOwnsScope(requestScope, userId, csrfToken)) return;
      if (!gatewayAccountReadSourceMatchesScope(requestScope, result)) {
        throw new Error(`gateway_account_${requestScope.kind}_identity_mismatch`);
      }
      if (requestScope.kind === "balanceHistory" && result.available) {
        committedBalanceHistoryPageRef.current = requestScope.page;
      }
      commit({
        kind: requestScope.kind,
        activeScope: requestScope,
        responseScope: requestScope,
        source: result
      } as unknown as GatewayAccountReadCompletion);
    } catch (error) {
      if (!requestOwnsScope(requestScope, userId, csrfToken)) return;
      commit({
        kind: requestScope.kind,
        activeScope: requestScope,
        responseScope: requestScope,
        source: unavailableSource<GatewayAccountReadProjectionValueMap[K]>("sub2api"),
        error: friendlyError(error)
      } as unknown as GatewayAccountReadCompletion);
    }
  }, [commit, friendlyError, requestOwnsScope, unavailableSource]);

  const loadWallet = useCallback(async (session: AuthSession) => {
    const requestScope = createGatewayAccountReadRequestScope("wallet", {
      ...epochRef.current,
      routeScope: scopeRef.current,
      requestGeneration: ++projectionGenerations.current.wallet
    });
    await settle(session, requestScope, () => getGatewayWallet());
  }, [settle]);

  const loadAccountUsage = useCallback(async (session: AuthSession) => {
    const requestScope = createGatewayAccountReadRequestScope("accountUsage", {
      ...epochRef.current,
      routeScope: scopeRef.current,
      requestGeneration: ++projectionGenerations.current.accountUsage
    });
    await settle(session, requestScope, () => getGatewayAccountUsageSummary("month"));
  }, [settle]);

  const loadBalanceHistory = useCallback(async (session: AuthSession, page: number) => {
    requestedBalanceHistoryPageRef.current = page;
    const requestScope = createGatewayAccountReadRequestScope("balanceHistory", {
      ...epochRef.current,
      routeScope: scopeRef.current,
      requestGeneration: ++projectionGenerations.current.balanceHistory,
      page
    });
    await settle(
      session,
      requestScope,
      () => getGatewayBalanceHistory(page, GATEWAY_BALANCE_HISTORY_PAGE_SIZE)
    );
  }, [settle]);

  const loadEndpoint = useCallback(async (session: AuthSession) => {
    const requestScope = createGatewayAccountReadRequestScope("endpoint", {
      ...epochRef.current,
      routeScope: scopeRef.current,
      requestGeneration: ++projectionGenerations.current.endpoint
    });
    await settle(session, requestScope, () => getGatewayEndpoint());
  }, [settle]);

  const load = useCallback(async () => {
    const session = currentSession();
    const routeScope = scopeRef.current;
    if (!session || routeScope === "inactive") return;
    syncSessionBoundary(session);
    const requests = gatewayAccountReadPlan(routeScope).map((kind) => {
      switch (kind) {
        case "wallet":
          return loadWallet(session);
        case "accountUsage":
          return loadAccountUsage(session);
        case "balanceHistory":
          return loadBalanceHistory(session, committedBalanceHistoryPageRef.current);
        case "endpoint":
          return loadEndpoint(session);
      }
    });
    await Promise.allSettled(requests);
  }, [currentSession, loadAccountUsage, loadBalanceHistory, loadEndpoint, loadWallet, syncSessionBoundary]);

  const changeBalancePage = useCallback(async (page: number) => {
    const session = currentSession();
    if (!session || scopeRef.current !== "api_overview" || !Number.isSafeInteger(page) || page < 1) return;
    syncSessionBoundary(session);
    await loadBalanceHistory(session, page);
  }, [currentSession, loadBalanceHistory, syncSessionBoundary]);

  return {
    wallet: state.wallet,
    accountUsage: state.accountUsage,
    balanceHistory: state.balanceHistory,
    endpoint: state.endpoint,
    balanceHistoryPage: state.balanceHistoryPage,
    load,
    refresh: load,
    changeBalancePage,
    reset
  };
}
