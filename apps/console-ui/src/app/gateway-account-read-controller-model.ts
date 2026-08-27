import type {
  GatewayAccountUsageSummaryDTO,
  GatewayBalanceHistoryPageDTO,
  GatewayEndpointDTO,
  GatewayWallet,
  SourceEnvelope
} from "../api/dtos.ts";
import type { RemoteState } from "./console-controller-types.ts";

export const GATEWAY_BALANCE_HISTORY_PAGE_SIZE = 20;

export type GatewayAccountReadRouteScope = "inactive" | "overview" | "workspace_launch" | "api_overview" | "keys";
export type GatewayAccountReadProjectionKind = "wallet" | "accountUsage" | "balanceHistory" | "endpoint";

export interface GatewayAccountReadEpoch {
  sessionGeneration: number;
  routeGeneration: number;
}

export type GatewayAccountReadInvalidationBoundary = "route" | "session" | "reset";

interface GatewayAccountReadRequestIdentity extends GatewayAccountReadEpoch {
  routeScope: GatewayAccountReadRouteScope;
  requestGeneration: number;
}

export interface GatewayAccountWalletRequestScope extends GatewayAccountReadRequestIdentity {
  kind: "wallet";
}

export interface GatewayAccountUsageRequestScope extends GatewayAccountReadRequestIdentity {
  kind: "accountUsage";
}

export interface GatewayAccountBalanceHistoryRequestScope extends GatewayAccountReadRequestIdentity {
  kind: "balanceHistory";
  page: number;
  pageSize: typeof GATEWAY_BALANCE_HISTORY_PAGE_SIZE;
}

export interface GatewayAccountEndpointRequestScope extends GatewayAccountReadRequestIdentity {
  kind: "endpoint";
}

export interface GatewayAccountReadRequestScopeMap {
  wallet: GatewayAccountWalletRequestScope;
  accountUsage: GatewayAccountUsageRequestScope;
  balanceHistory: GatewayAccountBalanceHistoryRequestScope;
  endpoint: GatewayAccountEndpointRequestScope;
}

export type GatewayAccountReadRequestScope = GatewayAccountReadRequestScopeMap[GatewayAccountReadProjectionKind];

export interface GatewayAccountReadRequestInputMap {
  wallet: GatewayAccountReadRequestIdentity;
  accountUsage: GatewayAccountReadRequestIdentity;
  balanceHistory: GatewayAccountReadRequestIdentity & { page: number };
  endpoint: GatewayAccountReadRequestIdentity;
}

export interface GatewayAccountReadProjectionSourceMap {
  wallet: SourceEnvelope<GatewayWallet>;
  accountUsage: SourceEnvelope<GatewayAccountUsageSummaryDTO>;
  balanceHistory: SourceEnvelope<GatewayBalanceHistoryPageDTO>;
  endpoint: SourceEnvelope<GatewayEndpointDTO>;
}

export interface GatewayAccountReadProjectionValueMap {
  wallet: GatewayWallet;
  accountUsage: GatewayAccountUsageSummaryDTO;
  balanceHistory: GatewayBalanceHistoryPageDTO;
  endpoint: GatewayEndpointDTO;
}

export interface GatewayAccountReadState {
  wallet: RemoteState<GatewayAccountReadProjectionSourceMap["wallet"]>;
  accountUsage: RemoteState<GatewayAccountReadProjectionSourceMap["accountUsage"]>;
  balanceHistory: RemoteState<GatewayAccountReadProjectionSourceMap["balanceHistory"]>;
  endpoint: RemoteState<GatewayAccountReadProjectionSourceMap["endpoint"]>;
  balanceHistoryPage: number;
}

type GatewayAccountReadCompletionMap = {
  [K in GatewayAccountReadProjectionKind]: {
    kind: K;
    activeScope: GatewayAccountReadRequestScopeMap[K];
    responseScope: GatewayAccountReadRequestScopeMap[K];
    source: GatewayAccountReadProjectionSourceMap[K];
    error?: string;
  };
};

export type GatewayAccountReadCompletion = GatewayAccountReadCompletionMap[GatewayAccountReadProjectionKind];

const routePlans: Record<GatewayAccountReadRouteScope, readonly GatewayAccountReadProjectionKind[]> = {
  inactive: [],
  overview: ["wallet", "accountUsage"],
  workspace_launch: ["wallet"],
  api_overview: ["wallet", "accountUsage", "balanceHistory", "endpoint"],
  keys: ["endpoint"]
};

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

export function gatewayAccountReadPlan(scope: GatewayAccountReadRouteScope): readonly GatewayAccountReadProjectionKind[] {
  return routePlans[scope];
}

export function createGatewayAccountReadRequestScope<K extends GatewayAccountReadProjectionKind>(
  kind: K,
  input: GatewayAccountReadRequestInputMap[K]
): GatewayAccountReadRequestScopeMap[K] {
  if (kind === "balanceHistory") {
    const historyInput = input as GatewayAccountReadRequestInputMap["balanceHistory"];
    if (!Number.isSafeInteger(historyInput.page) || historyInput.page < 1) {
      throw new Error("invalid_gateway_balance_history_page");
    }
    return {
      kind,
      ...historyInput,
      pageSize: GATEWAY_BALANCE_HISTORY_PAGE_SIZE
    } as GatewayAccountReadRequestScopeMap[K];
  }
  return { kind, ...input } as GatewayAccountReadRequestScopeMap[K];
}

export function gatewayAccountReadScopeIsCurrent(
  responseScope: GatewayAccountReadRequestScope,
  activeScope: GatewayAccountReadRequestScope
): boolean {
  if (responseScope.kind !== activeScope.kind
    || responseScope.sessionGeneration !== activeScope.sessionGeneration
    || responseScope.routeGeneration !== activeScope.routeGeneration
    || responseScope.routeScope !== activeScope.routeScope
    || responseScope.requestGeneration !== activeScope.requestGeneration) {
    return false;
  }
  if (responseScope.kind !== "balanceHistory" || activeScope.kind !== "balanceHistory") return true;
  return responseScope.page === activeScope.page && responseScope.pageSize === activeScope.pageSize;
}

export function gatewayAccountReadSourceMatchesScope<K extends GatewayAccountReadProjectionKind>(
  scope: GatewayAccountReadRequestScopeMap[K],
  source: GatewayAccountReadProjectionSourceMap[K]
): boolean {
  if (source.source !== "sub2api") return false;
  if (scope.kind !== "balanceHistory") return true;
  const historySource = source as GatewayAccountReadProjectionSourceMap["balanceHistory"];
  if (!historySource.available) return true;
  return historySource.data.page === scope.page
    && historySource.data.pageSize === scope.pageSize;
}

export function createGatewayAccountReadState(): GatewayAccountReadState {
  return {
    wallet: emptyRemote(),
    accountUsage: emptyRemote(),
    balanceHistory: emptyRemote(),
    endpoint: emptyRemote(),
    balanceHistoryPage: 1
  };
}

export function beginGatewayAccountReadProjection(
  state: GatewayAccountReadState,
  kind: GatewayAccountReadProjectionKind
): GatewayAccountReadState {
  return {
    ...state,
    [kind]: {
      ...state[kind],
      loading: true,
      error: ""
    }
  };
}

export function applyGatewayAccountReadCompletion(
  state: GatewayAccountReadState,
  completion: GatewayAccountReadCompletion
): GatewayAccountReadState | null {
  if (completion.kind !== completion.activeScope.kind
    || completion.kind !== completion.responseScope.kind
    || !gatewayAccountReadScopeIsCurrent(completion.responseScope, completion.activeScope)) {
    return null;
  }

  switch (completion.kind) {
    case "wallet":
      if (!gatewayAccountReadSourceMatchesScope(completion.responseScope, completion.source)) return null;
      return {
        ...state,
        wallet: { value: completion.source, loading: false, error: completion.error || "" }
      };
    case "accountUsage":
      if (!gatewayAccountReadSourceMatchesScope(completion.responseScope, completion.source)) return null;
      return {
        ...state,
        accountUsage: { value: completion.source, loading: false, error: completion.error || "" }
      };
    case "balanceHistory":
      if (!gatewayAccountReadSourceMatchesScope(completion.responseScope, completion.source)) return null;
      return {
        ...state,
        balanceHistory: { value: completion.source, loading: false, error: completion.error || "" },
        balanceHistoryPage: completion.source.available ? completion.responseScope.page : state.balanceHistoryPage
      };
    case "endpoint":
      if (!gatewayAccountReadSourceMatchesScope(completion.responseScope, completion.source)) return null;
      return {
        ...state,
        endpoint: { value: completion.source, loading: false, error: completion.error || "" }
      };
  }
}

export function invalidateGatewayAccountReadEpoch(
  epoch: GatewayAccountReadEpoch,
  boundary: GatewayAccountReadInvalidationBoundary
): GatewayAccountReadEpoch {
  return {
    sessionGeneration: epoch.sessionGeneration + (boundary === "route" ? 0 : 1),
    routeGeneration: epoch.routeGeneration + (boundary === "session" ? 0 : 1)
  };
}
