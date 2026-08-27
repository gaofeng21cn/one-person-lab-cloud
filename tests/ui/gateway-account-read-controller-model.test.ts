import assert from "node:assert/strict";
import test from "node:test";

import type {
  GatewayAccountUsageSummaryDTO,
  GatewayBalanceHistoryPageDTO,
  GatewayEndpointDTO,
  GatewayWallet,
  SourceEnvelope
} from "../../apps/console-ui/src/api/dtos.ts";
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
  type GatewayAccountReadEpoch
} from "../../apps/console-ui/src/app/gateway-account-read-controller-model.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

function available<T>(data: T, source = "sub2api", status: "available" | "empty" = "available"): SourceEnvelope<T> {
  return {
    source,
    status,
    available: true,
    fetchedAt,
    data
  };
}

function unavailable<T>(source = "sub2api"): SourceEnvelope<T> {
  return {
    source,
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: `${source}_unavailable`
  };
}

const wallet: GatewayWallet = {
  userId: "41",
  currency: "USD",
  usdMicros: "25000000",
  status: "active"
};

const accountUsage: GatewayAccountUsageSummaryDTO = {
  totalRequests: 7,
  totalInputTokens: 70,
  totalOutputTokens: 14,
  totalTokens: 84,
  totalActualCostUsdMicros: 12_500
};

const endpoint: GatewayEndpointDTO = { baseUrl: "https://gateway.example/v1" };

function history(page: number, pageSize = GATEWAY_BALANCE_HISTORY_PAGE_SIZE): GatewayBalanceHistoryPageDTO {
  return {
    items: [{
      type: "balance",
      valueUsdMicros: "1000000",
      status: "used",
      usedAt: fetchedAt,
      createdAt: fetchedAt
    }],
    total: 41,
    page,
    pageSize,
    pages: 3
  };
}

function epoch(sessionGeneration = 3, routeGeneration = 5): GatewayAccountReadEpoch {
  return { sessionGeneration, routeGeneration };
}

test("Gateway Account Read plans only the projections consumed by each route scope", () => {
  assert.deepEqual(gatewayAccountReadPlan("inactive"), []);
  assert.deepEqual(gatewayAccountReadPlan("overview"), ["wallet", "accountUsage"]);
  assert.deepEqual(gatewayAccountReadPlan("workspace_launch"), ["wallet"]);
  assert.deepEqual(gatewayAccountReadPlan("api_overview"), ["wallet", "accountUsage", "balanceHistory", "endpoint"]);
  assert.deepEqual(gatewayAccountReadPlan("keys"), ["endpoint"]);
});

test("each projection request has an independent current scope", () => {
  const walletScope = createGatewayAccountReadRequestScope("wallet", {
    ...epoch(), routeScope: "overview", requestGeneration: 2
  });
  const usageScope = createGatewayAccountReadRequestScope("accountUsage", {
    ...epoch(), routeScope: "overview", requestGeneration: 2
  });

  assert.equal(gatewayAccountReadScopeIsCurrent(walletScope, walletScope), true);
  assert.equal(gatewayAccountReadScopeIsCurrent(walletScope, usageScope), false);
  assert.equal(gatewayAccountReadScopeIsCurrent(walletScope, createGatewayAccountReadRequestScope("wallet", {
    ...epoch(), routeScope: "overview", requestGeneration: 3
  })), false);
  assert.equal(gatewayAccountReadScopeIsCurrent(walletScope, createGatewayAccountReadRequestScope("wallet", {
    ...epoch(4, 5), routeScope: "overview", requestGeneration: 2
  })), false);
  assert.equal(gatewayAccountReadScopeIsCurrent(walletScope, createGatewayAccountReadRequestScope("wallet", {
    ...epoch(3, 6), routeScope: "overview", requestGeneration: 2
  })), false);
  assert.equal(gatewayAccountReadScopeIsCurrent(walletScope, createGatewayAccountReadRequestScope("wallet", {
    ...epoch(), routeScope: "workspace_launch", requestGeneration: 2
  })), false);
});

test("balance history accepts only Sub2API data for the requested page and fixed page size", () => {
  const scope = createGatewayAccountReadRequestScope("balanceHistory", {
    ...epoch(), routeScope: "api_overview", requestGeneration: 4, page: 2
  });

  assert.equal(scope.pageSize, 20);
  assert.equal(gatewayAccountReadSourceMatchesScope(scope, available(history(2))), true);
  assert.equal(gatewayAccountReadSourceMatchesScope(scope, available(history(1))), false);
  assert.equal(gatewayAccountReadSourceMatchesScope(scope, available(history(2, 10))), false);
  assert.equal(gatewayAccountReadSourceMatchesScope(scope, available(history(2), "control-plane")), false);
  assert.equal(gatewayAccountReadSourceMatchesScope(scope, unavailable()), true);
  assert.equal(gatewayAccountReadSourceMatchesScope(scope, unavailable("control-plane")), false);
});

test("projection loading and completion update only their owning state", () => {
  const original = createGatewayAccountReadState();
  const walletScope = createGatewayAccountReadRequestScope("wallet", {
    ...epoch(), routeScope: "overview", requestGeneration: 1
  });
  const usageScope = createGatewayAccountReadRequestScope("accountUsage", {
    ...epoch(), routeScope: "overview", requestGeneration: 1
  });
  const loading = beginGatewayAccountReadProjection(original, "wallet");

  assert.equal(loading.wallet.loading, true);
  assert.equal(loading.accountUsage.loading, false);

  const walletSettled = applyGatewayAccountReadCompletion(loading, {
    kind: "wallet",
    activeScope: walletScope,
    responseScope: walletScope,
    source: available(wallet)
  });
  assert.ok(walletSettled);
  assert.deepEqual(walletSettled.wallet.value, available(wallet));
  assert.equal(walletSettled.wallet.loading, false);
  assert.deepEqual(walletSettled.accountUsage, original.accountUsage);

  const endpointScope = createGatewayAccountReadRequestScope("endpoint", {
    ...epoch(), routeScope: "overview", requestGeneration: 1
  });
  const endpointSettled = applyGatewayAccountReadCompletion(walletSettled, {
    kind: "endpoint",
    activeScope: endpointScope,
    responseScope: endpointScope,
    source: available(endpoint)
  });
  assert.ok(endpointSettled);
  assert.deepEqual(endpointSettled.endpoint.value, available(endpoint));
  assert.deepEqual(endpointSettled.wallet.value, available(wallet));

  const staleUsage = applyGatewayAccountReadCompletion(endpointSettled, {
    kind: "accountUsage",
    activeScope: createGatewayAccountReadRequestScope("accountUsage", {
      ...epoch(), routeScope: "overview", requestGeneration: 2
    }),
    responseScope: usageScope,
    source: available(accountUsage)
  });
  assert.equal(staleUsage, null);
  assert.deepEqual(walletSettled.wallet.value, available(wallet));
});

test("history advances its committed page only after a matching available completion", () => {
  const state = createGatewayAccountReadState();
  const pageTwoScope = createGatewayAccountReadRequestScope("balanceHistory", {
    ...epoch(), routeScope: "api_overview", requestGeneration: 1, page: 2
  });
  const failed = applyGatewayAccountReadCompletion(state, {
    kind: "balanceHistory",
    activeScope: pageTwoScope,
    responseScope: pageTwoScope,
    source: unavailable<GatewayBalanceHistoryPageDTO>(),
    error: "请求失败，请重试"
  });

  assert.ok(failed);
  assert.equal(failed.balanceHistoryPage, 1);
  assert.equal(failed.balanceHistory.value?.status, "unavailable");

  const settled = applyGatewayAccountReadCompletion(failed, {
    kind: "balanceHistory",
    activeScope: pageTwoScope,
    responseScope: pageTwoScope,
    source: available(history(2))
  });
  assert.ok(settled);
  assert.equal(settled.balanceHistoryPage, 2);
  assert.deepEqual(settled.endpoint, state.endpoint);
  assert.deepEqual(settled.endpoint.value, null);
});

test("route, Session, and reset invalidation advance the owning epoch", () => {
  const before = epoch(8, 13);
  const oldScope = createGatewayAccountReadRequestScope("endpoint", {
    ...before, routeScope: "keys", requestGeneration: 3
  });
  const afterRoute = invalidateGatewayAccountReadEpoch(before, "route");
  const afterSession = invalidateGatewayAccountReadEpoch(afterRoute, "session");
  const afterReset = invalidateGatewayAccountReadEpoch(afterSession, "reset");
  const currentScope = createGatewayAccountReadRequestScope("endpoint", {
    ...afterReset, routeScope: "keys", requestGeneration: 3
  });
  const reset = createGatewayAccountReadState();

  assert.deepEqual(afterRoute, { sessionGeneration: 8, routeGeneration: 14 });
  assert.deepEqual(afterSession, { sessionGeneration: 9, routeGeneration: 14 });
  assert.deepEqual(afterReset, { sessionGeneration: 10, routeGeneration: 15 });
  assert.equal(gatewayAccountReadScopeIsCurrent(oldScope, currentScope), false);
  assert.equal(reset.balanceHistoryPage, 1);
  assert.deepEqual(reset.wallet, { value: null, loading: false, error: "" });
  assert.deepEqual(reset.accountUsage, { value: null, loading: false, error: "" });
  assert.deepEqual(reset.balanceHistory, { value: null, loading: false, error: "" });
  assert.deepEqual(reset.endpoint, { value: null, loading: false, error: "" });
});
