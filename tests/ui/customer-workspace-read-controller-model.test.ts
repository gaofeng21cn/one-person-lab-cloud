import assert from "node:assert/strict";
import test from "node:test";

import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CUSTOMER_WORKSPACE_LIST_PAGE_SIZE,
  applyCustomerWorkspaceCompletion,
  commitCustomerWorkspaceDetailProjectionLease,
  createCustomerWorkspaceDetailScope,
  createCustomerWorkspaceDetailProjectionLease,
  createCustomerWorkspaceListScope,
  createCustomerWorkspaceReadState,
  customerWorkspaceRouteReadPlan,
  customerWorkspaceDetailProjectionLeaseIsCurrent,
  customerWorkspaceScopeIsCurrent,
  customerWorkspaceSourceMatchesScope,
  invalidateCustomerWorkspaceReadEpoch,
  settleCustomerWorkspacePage,
  type CustomerWorkspacePageProjection,
  type CustomerWorkspaceReadEpoch,
  type CustomerWorkspaceRouteScope
} from "../../apps/console-ui/src/app/customer-workspace-read-controller-model.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

const alpha: WorkspaceDTO = {
  id: "workspace-alpha",
  ownerAccountId: "account-alpha",
  ownerUserId: "user-alpha",
  state: "running",
  createdAt: fetchedAt,
  updatedAt: fetchedAt,
  name: "Alpha"
};

const beta: WorkspaceDTO = {
  ...alpha,
  id: "workspace-beta",
  name: "Beta"
};

function source<T>(data: T, status: "available" | "empty" = "available"): SourceEnvelope<T> {
  return {
    source: "control-plane",
    status,
    available: true,
    fetchedAt,
    data
  };
}

function unavailable<T>(): SourceEnvelope<T> {
  return {
    source: "control-plane",
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: "control_plane_unavailable"
  };
}

function page(pageNumber: number, pageSize: number, items: WorkspaceDTO[] = [alpha]): WorkspaceListData {
  return { items, total: items.length, page: pageNumber, pageSize };
}

function epoch(sessionGeneration = 3, routeGeneration = 7): CustomerWorkspaceReadEpoch {
  return { sessionGeneration, routeGeneration };
}

test("route read plans keep overview/list/detail/terms responsibilities exact", () => {
  const cases: Array<{
    scope: CustomerWorkspaceRouteScope;
    page: number;
    plan: ReturnType<typeof customerWorkspaceRouteReadPlan>;
  }> = [
    { scope: { kind: "inactive" }, page: 4, plan: null },
    { scope: { kind: "overview" }, page: 4, plan: { kind: "list", page: 1, pageSize: 1 } },
    { scope: { kind: "list" }, page: 4, plan: { kind: "list", page: 4, pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE } },
    { scope: { kind: "detail", workspaceId: alpha.id }, page: 4, plan: { kind: "detail", workspaceId: alpha.id } },
    { scope: { kind: "terms" }, page: 4, plan: { kind: "list", page: 1, pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE } }
  ];

  for (const testCase of cases) {
    assert.deepEqual(customerWorkspaceRouteReadPlan(testCase.scope, testCase.page), testCase.plan);
  }
});

test("list completion requires the exact Control Plane page identity", () => {
  const scope = createCustomerWorkspaceListScope({
    ...epoch(),
    requestGeneration: 2,
    page: 3,
    pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE
  });

  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "list", scope, source: source(page(3, 10)) }), true);
  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "list", scope, source: source(page(2, 10)) }), false);
  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "list", scope, source: source(page(3, 1)) }), false);
  assert.equal(customerWorkspaceSourceMatchesScope({
    kind: "list",
    scope,
    source: { ...source(page(3, 10)), source: "fabric" }
  }), false);
  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "list", scope, source: unavailable() }), true);
});

test("detail completion accepts only the requested Workspace or authoritative absence", () => {
  const scope = createCustomerWorkspaceDetailScope({
    ...epoch(),
    requestGeneration: 5,
    workspaceId: alpha.id
  });

  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "detail", scope, source: source(alpha) }), true);
  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "detail", scope, source: source(beta) }), false);
  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "detail", scope, source: source(null, "empty") }), true);
  assert.equal(customerWorkspaceSourceMatchesScope({
    kind: "detail",
    scope,
    source: { ...source(alpha), source: "sub2api" }
  }), false);
  assert.equal(customerWorkspaceSourceMatchesScope({ kind: "detail", scope, source: unavailable() }), true);
});

test("list and detail settle independently", () => {
  const initial = createCustomerWorkspaceReadState();
  const listScope = createCustomerWorkspaceListScope({
    ...epoch(),
    requestGeneration: 1,
    page: 1,
    pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE
  });
  const detailScope = createCustomerWorkspaceDetailScope({
    ...epoch(),
    requestGeneration: 1,
    workspaceId: alpha.id
  });
  const listResult = applyCustomerWorkspaceCompletion(initial, {
    kind: "list",
    activeScope: listScope,
    responseScope: listScope,
    source: source(page(1, 10))
  });

  assert.ok(listResult);
  const detailFailure = applyCustomerWorkspaceCompletion(listResult, {
    kind: "detail",
    activeScope: detailScope,
    responseScope: detailScope,
    source: unavailable(),
    error: "Workspace detail unavailable"
  });

  assert.ok(detailFailure);
  assert.deepEqual(detailFailure.workspaces, listResult.workspaces);
  assert.equal(detailFailure.detail.value?.available, false);
  assert.equal(detailFailure.detail.error, "Workspace detail unavailable");
});

test("stale completion cannot mutate either projection", () => {
  const state = createCustomerWorkspaceReadState();
  const activeScope = createCustomerWorkspaceDetailScope({
    ...epoch(),
    requestGeneration: 4,
    workspaceId: beta.id
  });
  const staleScope = createCustomerWorkspaceDetailScope({
    ...epoch(),
    requestGeneration: 3,
    workspaceId: alpha.id
  });

  assert.equal(applyCustomerWorkspaceCompletion(state, {
    kind: "detail",
    activeScope,
    responseScope: staleScope,
    source: source(alpha)
  }), null);
});

test("page settlement advances only for a matching authoritative page", () => {
  const projection: CustomerWorkspacePageProjection = { page: 2, requestPage: 2 };

  assert.deepEqual(settleCustomerWorkspacePage(projection, 3, 10, source(page(3, 10))), {
    page: 3,
    requestPage: 3
  });
  assert.deepEqual(settleCustomerWorkspacePage(projection, 3, 10, source(page(2, 10))), projection);
  assert.deepEqual(settleCustomerWorkspacePage(projection, 3, 10, unavailable()), projection);
});

test("Session, route, request, and Workspace identity are all part of freshness", () => {
  const scope = createCustomerWorkspaceDetailScope({
    ...epoch(),
    requestGeneration: 6,
    workspaceId: alpha.id
  });

  assert.equal(customerWorkspaceScopeIsCurrent(scope, scope), true);
  assert.equal(customerWorkspaceScopeIsCurrent(scope, createCustomerWorkspaceDetailScope({
    ...epoch(4, 7), requestGeneration: 6, workspaceId: alpha.id
  })), false);
  assert.equal(customerWorkspaceScopeIsCurrent(scope, createCustomerWorkspaceDetailScope({
    ...epoch(3, 8), requestGeneration: 6, workspaceId: alpha.id
  })), false);
  assert.equal(customerWorkspaceScopeIsCurrent(scope, createCustomerWorkspaceDetailScope({
    ...epoch(), requestGeneration: 7, workspaceId: alpha.id
  })), false);
  assert.equal(customerWorkspaceScopeIsCurrent(scope, createCustomerWorkspaceDetailScope({
    ...epoch(), requestGeneration: 6, workspaceId: beta.id
  })), false);
});

test("a committed detail projection lease invalidates the older detail refresh", () => {
  const staleRefreshScope = createCustomerWorkspaceDetailScope({
    ...epoch(),
    requestGeneration: 6,
    workspaceId: alpha.id
  });
  const lease = createCustomerWorkspaceDetailProjectionLease(staleRefreshScope);

  assert.equal(customerWorkspaceDetailProjectionLeaseIsCurrent(lease, staleRefreshScope), true);
  const committedScope = commitCustomerWorkspaceDetailProjectionLease(lease, staleRefreshScope);
  assert.ok(committedScope);
  assert.equal(committedScope.requestGeneration, staleRefreshScope.requestGeneration + 1);
  assert.equal(customerWorkspaceDetailProjectionLeaseIsCurrent(lease, committedScope), false);
  assert.equal(customerWorkspaceScopeIsCurrent(staleRefreshScope, committedScope), false);
  assert.equal(commitCustomerWorkspaceDetailProjectionLease(lease, committedScope), null);
});

test("reset invalidates old leases and production settlement keeps empty, absence, and unavailable distinct", () => {
  const before = epoch(8, 13);
  const oldScope = createCustomerWorkspaceDetailScope({
    ...before,
    requestGeneration: 2,
    workspaceId: alpha.id
  });
  const after = invalidateCustomerWorkspaceReadEpoch(before);
  const newScope = createCustomerWorkspaceDetailScope({
    ...after,
    requestGeneration: 2,
    workspaceId: alpha.id
  });

  assert.deepEqual(after, { sessionGeneration: 9, routeGeneration: 14 });
  assert.equal(customerWorkspaceScopeIsCurrent(oldScope, newScope), false);

  const emptyListScope = createCustomerWorkspaceListScope({
    ...after,
    requestGeneration: 2,
    page: 1,
    pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE
  });
  const emptyList = source(page(1, CUSTOMER_WORKSPACE_LIST_PAGE_SIZE, []), "empty");
  assert.equal(customerWorkspaceSourceMatchesScope({
    kind: "list",
    scope: emptyListScope,
    source: emptyList
  }), true);
  const emptyListState = applyCustomerWorkspaceCompletion(createCustomerWorkspaceReadState(), {
    kind: "list",
    activeScope: emptyListScope,
    responseScope: emptyListScope,
    source: emptyList
  });
  assert.ok(emptyListState?.workspaces.value?.available);
  assert.equal(emptyListState.workspaces.value.status, "empty");
  assert.deepEqual(emptyListState.workspaces.value.data.items, []);

  const absence = source<WorkspaceDTO | null>(null, "empty");
  assert.equal(customerWorkspaceSourceMatchesScope({
    kind: "detail",
    scope: newScope,
    source: absence
  }), true);
  const absentDetailState = applyCustomerWorkspaceCompletion(createCustomerWorkspaceReadState(), {
    kind: "detail",
    activeScope: newScope,
    responseScope: newScope,
    source: absence
  });
  assert.ok(absentDetailState?.detail.value?.available);
  assert.equal(absentDetailState.detail.value.status, "empty");
  assert.equal(absentDetailState.detail.value.data, null);

  const unavailableDetail = unavailable<WorkspaceDTO | null>();
  assert.equal(customerWorkspaceSourceMatchesScope({
    kind: "detail",
    scope: newScope,
    source: unavailableDetail
  }), true);
  const unavailableDetailState = applyCustomerWorkspaceCompletion(createCustomerWorkspaceReadState(), {
    kind: "detail",
    activeScope: newScope,
    responseScope: newScope,
    source: unavailableDetail,
    error: "Workspace detail unavailable"
  });
  assert.equal(unavailableDetailState?.detail.value?.available, false);
  assert.equal(unavailableDetailState?.detail.value?.status, "unavailable");
  assert.equal(unavailableDetailState?.detail.error, "Workspace detail unavailable");
});
