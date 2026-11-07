import assert from "node:assert/strict";
import test from "node:test";

import type {
  OperatorResourceDTO,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  SourceEnvelope,
  WorkspaceBillingReceiptDTO,
  WorkspaceDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  OPERATOR_RESOURCE_PAGE_SIZE,
  applyOperatorResourceCompletion,
  createOperatorResourceDetailScope,
  createOperatorResourceListScope,
  createOperatorResourceReadState,
  invalidateOperatorResourceReadEpoch,
  operatorResourceScopeIsCurrent,
  operatorResourceScopeKey,
  operatorResourceSourceMatchesScope,
  isValidOperatorResourcePage,
  settleOperatorResourcePage,
  type OperatorResourcePageProjection,
  type OperatorResourceReadEpoch,
  type OperatorResourceScope
} from "../../apps/console-ui/src/app/operator-resource-read-controller-model.ts";
import { operatorResourceFallbackSource } from "../../apps/console-ui/src/app/use-operator-resource-read-controller.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

function source<T>(data: T, status: "available" | "empty" = "available"): SourceEnvelope<T> {
  return {
    source: "control-plane",
    status,
    available: true,
    fetchedAt,
    data
  };
}

function unavailable<T>(reasonCode = "upstream_unavailable"): SourceEnvelope<T> {
  return {
    source: "control-plane+fabric+ledger",
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode
  };
}

const workspace: WorkspaceDTO = {
  id: "workspace-alpha",
  ownerAccountId: "account-alpha",
  ownerUserId: "user-alpha",
  state: "running",
  createdAt: fetchedAt,
  updatedAt: fetchedAt,
  name: "Alpha"
};

const receipt: WorkspaceBillingReceiptDTO = {
  receiptId: "receipt-alpha",
  workspaceId: workspace.id,
  accountId: workspace.ownerAccountId,
  status: "settled",
  totalUsdMicros: 100,
  currency: "USD",
  createdAt: fetchedAt
};

const resource: OperatorResourceDTO = {
  ownerAccount: source({ id: workspace.ownerAccountId }),
  ownerUser: source({ id: workspace.ownerUserId, email: "alpha@example.com" }),
  workspace: source({ id: workspace.id, name: workspace.name }),
  resourceType: source("compute"),
  packageOrSpec: source("basic"),
  providerId: source("provider-alpha"),
  zone: source("zone-alpha"),
  status: source("running"),
  createdAt: source(fetchedAt),
  expiresAt: source(fetchedAt),
  lastReadAt: source(fetchedAt),
  operationRef: source("operation-alpha"),
  receiptRef: source(receipt.receiptId)
};

const detail: OperatorWorkspaceDTO = {
  workspace: source(workspace),
  ownerAccount: source({ id: workspace.ownerAccountId }),
  ownerUser: source({ id: workspace.ownerUserId, email: "alpha@example.com" }),
  resources: [resource],
  receipt: source(receipt),
  workspaceKeyUsage: source({ keyId: "key-alpha", todayActualCostUsdMicros: 1, totalActualCostUsdMicros: 2 })
};

function page(pageNumber: number, pageSize = OPERATOR_RESOURCE_PAGE_SIZE): OperatorWorkspacePageDTO {
  return { items: [detail], total: 1, page: pageNumber, pageSize };
}

function emptyPage(pageNumber: number): OperatorWorkspacePageDTO {
  return { items: [], total: 0, page: pageNumber, pageSize: OPERATOR_RESOURCE_PAGE_SIZE };
}

function epoch(sessionGeneration = 4, routeGeneration = 9): OperatorResourceReadEpoch {
  return { sessionGeneration, routeGeneration };
}

test("operator resource list scope fixes pageSize and accepts only matching page identity", () => {
  const scope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 2, page: 3 });

  assert.equal(scope.pageSize, OPERATOR_RESOURCE_PAGE_SIZE);
  assert.equal(operatorResourceSourceMatchesScope({ kind: "workspaces", scope, source: source(page(3)) }), true);
  assert.equal(operatorResourceSourceMatchesScope({ kind: "workspaces", scope, source: source(page(2)) }), false);
  assert.equal(operatorResourceSourceMatchesScope({ kind: "workspaces", scope, source: source(page(3, 10)) }), false);
});

test("detail scope rejects a response for another Workspace", () => {
  const detailScope = createOperatorResourceDetailScope({ ...epoch(), requestGeneration: 3, workspaceId: workspace.id });

  assert.equal(operatorResourceSourceMatchesScope({ kind: "detail", scope: detailScope, source: source(detail) }), true);
  assert.equal(operatorResourceSourceMatchesScope({ kind: "detail", scope: detailScope, source: source({
    ...detail,
    workspace: source({ ...workspace, id: "workspace-beta" })
  }) }), false);
});

test("each projection has an independent scope key", () => {
  const listScope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 1, page: 1 });
  const detailScope = createOperatorResourceDetailScope({ ...epoch(), requestGeneration: 1, workspaceId: workspace.id });

  assert.notEqual(operatorResourceScopeKey(listScope), operatorResourceScopeKey(detailScope));
  assert.notEqual(
    operatorResourceScopeKey(listScope),
    operatorResourceScopeKey(createOperatorResourceListScope({ ...epoch(), requestGeneration: 1, page: 2 }))
  );
});

test("late completion is rejected after a Session or route generation changes", () => {
  const scope = createOperatorResourceDetailScope({ ...epoch(), requestGeneration: 5, workspaceId: workspace.id });

  assert.equal(operatorResourceScopeIsCurrent(scope, scope), true);
  assert.equal(operatorResourceScopeIsCurrent(scope, createOperatorResourceDetailScope({
    ...epoch(5, 9), requestGeneration: 5, workspaceId: workspace.id
  })), false);
  assert.equal(operatorResourceScopeIsCurrent(scope, createOperatorResourceDetailScope({
    ...epoch(4, 10), requestGeneration: 5, workspaceId: workspace.id
  })), false);
  assert.equal(operatorResourceScopeIsCurrent(scope, createOperatorResourceDetailScope({
    ...epoch(), requestGeneration: 6, workspaceId: workspace.id
  })), false);
});

test("scope-source validation rejects wrong kinds without throwing", () => {
  const detailScope = createOperatorResourceDetailScope({ ...epoch(), requestGeneration: 1, workspaceId: workspace.id });
  const listScope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 1, page: 1 });

  const wrongDetailSource = {
    kind: "detail",
    scope: detailScope,
    source: source(page(1))
  } as unknown as Parameters<typeof operatorResourceSourceMatchesScope>[0];
  const wrongListScope = {
    kind: "workspaces",
    scope: listScope,
    source: source(detail)
  } as unknown as Parameters<typeof operatorResourceSourceMatchesScope>[0];

  for (const input of [wrongDetailSource, wrongListScope]) {
    assert.doesNotThrow(() => operatorResourceSourceMatchesScope(input));
    assert.equal(operatorResourceSourceMatchesScope(input), false);
  }
});

test("reset invalidates every old scope without changing the source contract", () => {
  const before = epoch(11, 17);
  const oldScope = createOperatorResourceListScope({ ...before, requestGeneration: 1, page: 1 });
  const after = invalidateOperatorResourceReadEpoch(before);
  const newScope = createOperatorResourceListScope({ ...after, requestGeneration: 1, page: 1 });

  assert.deepEqual(after, { sessionGeneration: 12, routeGeneration: 18 });
  assert.equal(operatorResourceScopeIsCurrent(oldScope, newScope), false);
  assert.equal(operatorResourceScopeIsCurrent(newScope, newScope), true);
});

test("empty is a successful empty source and remains distinct from unavailable", () => {
  const empty = source(emptyPage(1), "empty");
  const missing = unavailable<OperatorWorkspacePageDTO>();

  assert.equal(empty.status, "empty");
  assert.equal(missing.status, "unavailable");
  assert.equal(empty.available, true);
  assert.equal(missing.available, false);
});

test("successful list completion commits authoritative data and stops loading", () => {
  const state = createOperatorResourceReadState();
  const scope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 1, page: 1 });
  const result = applyOperatorResourceCompletion(state, {
    kind: "workspaces",
    activeScope: scope,
    responseScope: scope,
    source: source(emptyPage(1), "empty")
  });

  assert.ok(result);
  assert.deepEqual(result.workspaces.value, source(emptyPage(1), "empty"));
  assert.equal(result.workspaces.loading, false);
});

test("partial failure settles one projection without clearing a successful sibling", () => {
  const state = createOperatorResourceReadState();
  const detailScope = createOperatorResourceDetailScope({ ...epoch(), requestGeneration: 1, workspaceId: workspace.id });

  const listScope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 1, page: 1 });
  const withList = applyOperatorResourceCompletion(state, {
    kind: "workspaces",
    activeScope: listScope,
    responseScope: listScope,
    source: source(page(1))
  });
  assert.ok(withList);
  const withDetailFailure = applyOperatorResourceCompletion(withList, {
    kind: "detail",
    activeScope: detailScope,
    responseScope: detailScope,
    source: unavailable("ledger_unavailable"),
    error: "detail unavailable"
  });

  assert.ok(withDetailFailure);
  assert.equal(withDetailFailure.detail.value?.available, false);
  assert.equal(withDetailFailure.detail.error, "detail unavailable");
  assert.deepEqual(withDetailFailure.workspaces.value, source(page(1)));
});

test("stale completion cannot mutate any projection", () => {
  const state = createOperatorResourceReadState();
  const activeScope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 2, page: 2 });
  const staleScope = createOperatorResourceListScope({ ...epoch(), requestGeneration: 1, page: 2 });

  const result = applyOperatorResourceCompletion(state, {
    kind: "workspaces",
    activeScope,
    responseScope: staleScope,
    source: source(page(2))
  });

  assert.equal(result, null);
});

test("failed page read keeps public page and refresh request on the last committed page", () => {
  const projection: OperatorResourcePageProjection = { page: 2, requestPage: 2 };
  const failed = settleOperatorResourcePage(projection, 3, unavailable<OperatorWorkspacePageDTO>());

  assert.deepEqual(failed, { page: 2, requestPage: 2 });
  assert.deepEqual(
    settleOperatorResourcePage(failed, failed.requestPage, source(page(failed.requestPage))),
    { page: 2, requestPage: 2 }
  );
});

test("operator resource page navigation accepts only positive integers", () => {
  assert.equal(isValidOperatorResourcePage(1), true);
  assert.equal(isValidOperatorResourcePage(2.5), false);
  assert.equal(isValidOperatorResourcePage(Number.NaN), false);
  assert.equal(isValidOperatorResourcePage(Number.POSITIVE_INFINITY), false);
  assert.equal(isValidOperatorResourcePage(0), false);
  assert.equal(isValidOperatorResourcePage(-1), false);
});

test("operator resource fallback sources distinguish Cloud composition from retained reads", () => {
  assert.equal(operatorResourceFallbackSource("workspaces", true), "workspace+fabric");
  assert.equal(operatorResourceFallbackSource("detail", true), "workspace+fabric");
  assert.equal(operatorResourceFallbackSource("workspaces", false), "control-plane+fabric+sub2api");
  assert.equal(operatorResourceFallbackSource("detail", false), "control-plane+fabric+ledger");
});

for (const cloud of [true, false]) {
  test(`operator resource completions preserve ${cloud ? "Cloud" : "legacy"} owner envelopes and unavailable readback`, () => {
    const scope = createOperatorResourceDetailScope({ ...epoch(), requestGeneration: 1, workspaceId: workspace.id });
    const owner = cloud ? "workspace" : "control-plane";
    const item: OperatorWorkspaceDTO = {
      ...detail,
      workspace: { ...source(workspace), source: owner },
      ...(cloud ? {
        tenant: { ...source({ id: "tenant-alpha" }), source: owner },
        createdBy: { ...source({ id: "actor-alpha" }), source: owner },
        resourcesReadback: { ...unavailable("fabric_read_unavailable"), source: "fabric" },
        resources: []
      } : {})
    };
    const result = applyOperatorResourceCompletion(createOperatorResourceReadState(), {
      kind: "detail", activeScope: scope, responseScope: scope,
      source: { ...source(item), source: operatorResourceFallbackSource("detail", cloud) }
    });
    assert.ok(result?.detail.value?.available);
    assert.deepEqual(result.detail.value.data, item);
    assert.equal(result.detail.value.source, operatorResourceFallbackSource("detail", cloud));
    assert.equal(result.detail.value.data.workspace.source, owner);
  });
}
