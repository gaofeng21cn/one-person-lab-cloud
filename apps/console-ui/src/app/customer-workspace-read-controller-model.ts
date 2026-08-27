import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData
} from "../api/dtos.ts";
import type { RemoteState } from "./console-controller-types.ts";

export const CUSTOMER_WORKSPACE_LIST_PAGE_SIZE = 10 as const;

export type CustomerWorkspaceRouteScope =
  | { readonly kind: "inactive" }
  | { readonly kind: "overview" }
  | { readonly kind: "list" }
  | { readonly kind: "detail"; readonly workspaceId: string }
  | { readonly kind: "terms" };

export type CustomerWorkspaceRouteReadPlan =
  | { readonly kind: "list"; readonly page: number; readonly pageSize: number }
  | { readonly kind: "detail"; readonly workspaceId: string };

export function customerWorkspaceRouteScopeKey(scope: CustomerWorkspaceRouteScope): string {
  return scope.kind === "detail" ? `${scope.kind}:${scope.workspaceId}` : scope.kind;
}

export function customerWorkspaceRouteReadPlan(
  scope: CustomerWorkspaceRouteScope,
  committedPage: number
): CustomerWorkspaceRouteReadPlan | null {
  switch (scope.kind) {
    case "inactive":
      return null;
    case "overview":
      return { kind: "list", page: 1, pageSize: 1 };
    case "list":
      return {
        kind: "list",
        page: isValidCustomerWorkspacePage(committedPage) ? committedPage : 1,
        pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE
      };
    case "detail":
      return scope.workspaceId ? { kind: "detail", workspaceId: scope.workspaceId } : null;
    case "terms":
      return { kind: "list", page: 1, pageSize: CUSTOMER_WORKSPACE_LIST_PAGE_SIZE };
  }
}

export interface CustomerWorkspaceReadEpoch {
  readonly sessionGeneration: number;
  readonly routeGeneration: number;
}

export interface CustomerWorkspaceListScope extends CustomerWorkspaceReadEpoch {
  readonly kind: "list";
  readonly requestGeneration: number;
  readonly page: number;
  readonly pageSize: number;
}

export interface CustomerWorkspaceDetailScope extends CustomerWorkspaceReadEpoch {
  readonly kind: "detail";
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export type CustomerWorkspaceProjectionScope =
  | CustomerWorkspaceListScope
  | CustomerWorkspaceDetailScope;

export interface CustomerWorkspaceListScopeInput extends CustomerWorkspaceReadEpoch {
  readonly requestGeneration: number;
  readonly page: number;
  readonly pageSize: number;
}

export interface CustomerWorkspaceDetailScopeInput extends CustomerWorkspaceReadEpoch {
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export function createCustomerWorkspaceListScope(
  input: CustomerWorkspaceListScopeInput
): CustomerWorkspaceListScope {
  return { ...input, kind: "list" };
}

export function createCustomerWorkspaceDetailScope(
  input: CustomerWorkspaceDetailScopeInput
): CustomerWorkspaceDetailScope {
  return { ...input, kind: "detail" };
}

export function customerWorkspaceScopeKey(scope: CustomerWorkspaceProjectionScope): string {
  const identity = scope.kind === "list"
    ? `page=${scope.page}&pageSize=${scope.pageSize}`
    : `workspaceId=${scope.workspaceId}`;
  return `${scope.kind}|session=${scope.sessionGeneration}|route=${scope.routeGeneration}|request=${scope.requestGeneration}|${identity}`;
}

export function customerWorkspaceScopeIsCurrent(
  expected: CustomerWorkspaceProjectionScope,
  current: CustomerWorkspaceProjectionScope
): boolean {
  return customerWorkspaceScopeKey(expected) === customerWorkspaceScopeKey(current);
}

export interface CustomerWorkspaceDetailProjectionLease {
  readonly scope: CustomerWorkspaceDetailScope;
}

export function createCustomerWorkspaceDetailProjectionLease(
  scope: CustomerWorkspaceDetailScope
): CustomerWorkspaceDetailProjectionLease {
  return { scope };
}

export function customerWorkspaceDetailProjectionLeaseIsCurrent(
  lease: CustomerWorkspaceDetailProjectionLease,
  currentScope: CustomerWorkspaceDetailScope
): boolean {
  return customerWorkspaceScopeIsCurrent(lease.scope, currentScope);
}

export function commitCustomerWorkspaceDetailProjectionLease(
  lease: CustomerWorkspaceDetailProjectionLease,
  currentScope: CustomerWorkspaceDetailScope
): CustomerWorkspaceDetailScope | null {
  if (!customerWorkspaceDetailProjectionLeaseIsCurrent(lease, currentScope)) return null;
  return createCustomerWorkspaceDetailScope({
    sessionGeneration: currentScope.sessionGeneration,
    routeGeneration: currentScope.routeGeneration,
    requestGeneration: currentScope.requestGeneration + 1,
    workspaceId: currentScope.workspaceId
  });
}

export function invalidateCustomerWorkspaceReadEpoch(
  epoch: CustomerWorkspaceReadEpoch
): CustomerWorkspaceReadEpoch {
  return {
    sessionGeneration: epoch.sessionGeneration + 1,
    routeGeneration: epoch.routeGeneration + 1
  };
}

type CustomerWorkspaceScopedSource =
  | {
    readonly kind: "list";
    readonly scope: CustomerWorkspaceListScope;
    readonly source: SourceEnvelope<WorkspaceListData>;
  }
  | {
    readonly kind: "detail";
    readonly scope: CustomerWorkspaceDetailScope;
    readonly source: SourceEnvelope<WorkspaceDTO | null>;
  };

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function customerWorkspaceSourceMatchesScope(input: CustomerWorkspaceScopedSource): boolean {
  const { source } = input;
  if (!isRecord(source) || source.source !== "control-plane") return false;
  if (source.available === false) return source.status === "unavailable";
  if (source.available !== true) return false;

  if (input.kind === "list") {
    if (!isRecord(source.data)) return false;
    return source.data.page === input.scope.page
      && source.data.pageSize === input.scope.pageSize
      && typeof source.data.total === "number"
      && Number.isSafeInteger(source.data.total)
      && source.data.total >= 0
      && Array.isArray(source.data.items);
  }

  if (source.data === null) return source.status === "empty";
  return source.status === "available"
    && isRecord(source.data)
    && source.data.id === input.scope.workspaceId;
}

export interface CustomerWorkspaceReadState {
  readonly workspaces: RemoteState<SourceEnvelope<WorkspaceListData>>;
  readonly detail: RemoteState<SourceEnvelope<WorkspaceDTO | null>>;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function settledRemote<T>(source: SourceEnvelope<T>, error = ""): RemoteState<SourceEnvelope<T>> {
  return { value: source, loading: false, error };
}

export function createCustomerWorkspaceReadState(): CustomerWorkspaceReadState {
  return {
    workspaces: emptyRemote(),
    detail: emptyRemote()
  };
}

export type CustomerWorkspaceCompletion =
  | {
    readonly kind: "list";
    readonly activeScope: CustomerWorkspaceListScope;
    readonly responseScope: CustomerWorkspaceListScope;
    readonly source: SourceEnvelope<WorkspaceListData>;
    readonly error?: string;
  }
  | {
    readonly kind: "detail";
    readonly activeScope: CustomerWorkspaceDetailScope;
    readonly responseScope: CustomerWorkspaceDetailScope;
    readonly source: SourceEnvelope<WorkspaceDTO | null>;
    readonly error?: string;
  };

export function applyCustomerWorkspaceCompletion(
  state: CustomerWorkspaceReadState,
  completion: CustomerWorkspaceCompletion
): CustomerWorkspaceReadState | null {
  if (!customerWorkspaceScopeIsCurrent(completion.activeScope, completion.responseScope)) return null;

  if (completion.kind === "list") {
    if (!customerWorkspaceSourceMatchesScope({
      kind: "list",
      scope: completion.activeScope,
      source: completion.source
    })) return null;
    return {
      ...state,
      workspaces: settledRemote(completion.source, completion.error)
    };
  }

  if (!customerWorkspaceSourceMatchesScope({
    kind: "detail",
    scope: completion.activeScope,
    source: completion.source
  })) return null;
  return {
    ...state,
    detail: settledRemote(completion.source, completion.error)
  };
}

export interface CustomerWorkspacePageProjection {
  readonly page: number;
  readonly requestPage: number;
}

export function isValidCustomerWorkspacePage(page: number): boolean {
  return Number.isInteger(page) && page >= 1;
}

export function settleCustomerWorkspacePage(
  projection: CustomerWorkspacePageProjection,
  requestedPage: number,
  requestedPageSize: number,
  source: SourceEnvelope<WorkspaceListData>
): CustomerWorkspacePageProjection {
  if (!isValidCustomerWorkspacePage(requestedPage)
    || !Number.isInteger(requestedPageSize)
    || requestedPageSize < 1
    || !source.available
    || source.data.page !== requestedPage
    || source.data.pageSize !== requestedPageSize) {
    return projection;
  }
  return { page: source.data.page, requestPage: source.data.page };
}
