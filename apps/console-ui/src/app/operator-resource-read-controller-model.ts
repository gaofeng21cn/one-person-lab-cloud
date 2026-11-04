import type {
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  SourceEnvelope
} from "../api/dtos.ts";
import type { RemoteState } from "./console-controller-types.ts";

export const OPERATOR_RESOURCE_PAGE_SIZE = 20 as const;

export interface OperatorResourcePageProjection {
  readonly page: number;
  readonly requestPage: number;
}

export function isValidOperatorResourcePage(page: number): boolean {
  return Number.isInteger(page) && page >= 1;
}

/**
 * Keep the retry scope on the last committed page when a page read does not
 * produce an authoritative response. A successful response advances both
 * the visible page and the next refresh target together.
 */
export function settleOperatorResourcePage(
  projection: OperatorResourcePageProjection,
  requestedPage: number,
  source: SourceEnvelope<OperatorWorkspacePageDTO>
): OperatorResourcePageProjection {
  if (!source.available
    || !isValidOperatorResourcePage(requestedPage)
    || source.data.page !== requestedPage
    || source.data.pageSize !== OPERATOR_RESOURCE_PAGE_SIZE) {
    return { page: projection.page, requestPage: projection.page };
  }
  return { page: source.data.page, requestPage: source.data.page };
}

export interface OperatorResourceReadEpoch {
  readonly sessionGeneration: number;
  readonly routeGeneration: number;
}

export interface OperatorResourceListScope extends OperatorResourceReadEpoch {
  readonly kind: "workspaces";
  readonly requestGeneration: number;
  readonly page: number;
  readonly pageSize: typeof OPERATOR_RESOURCE_PAGE_SIZE;
}

export interface OperatorResourceDetailScope extends OperatorResourceReadEpoch {
  readonly kind: "detail";
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export type OperatorResourceScope =
  | OperatorResourceListScope
  | OperatorResourceDetailScope;

export interface OperatorResourceListScopeInput extends OperatorResourceReadEpoch {
  readonly requestGeneration: number;
  readonly page: number;
}

export interface OperatorResourceDetailScopeInput extends OperatorResourceReadEpoch {
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export function createOperatorResourceListScope(input: OperatorResourceListScopeInput): OperatorResourceListScope {
  return { ...input, kind: "workspaces", pageSize: OPERATOR_RESOURCE_PAGE_SIZE };
}

export function createOperatorResourceDetailScope(input: OperatorResourceDetailScopeInput): OperatorResourceDetailScope {
  return { ...input, kind: "detail" };
}

export function operatorResourceScopeKey(scope: OperatorResourceScope): string {
  const identity = scope.kind === "workspaces"
    ? `page=${scope.page}&pageSize=${scope.pageSize}`
    : `workspace=${scope.workspaceId}`;
  return `${scope.kind}|session=${scope.sessionGeneration}|route=${scope.routeGeneration}|request=${scope.requestGeneration}|${identity}`;
}

export function operatorResourceScopeIsCurrent(
  expected: OperatorResourceScope,
  current: OperatorResourceScope
): boolean {
  return operatorResourceScopeKey(expected) === operatorResourceScopeKey(current);
}

export function invalidateOperatorResourceReadEpoch(
  epoch: OperatorResourceReadEpoch
): OperatorResourceReadEpoch {
  return {
    sessionGeneration: epoch.sessionGeneration + 1,
    routeGeneration: epoch.routeGeneration + 1
  };
}

export type OperatorResourceScopedSource =
  | {
    readonly kind: "workspaces";
    readonly scope: OperatorResourceListScope;
    readonly source: SourceEnvelope<OperatorWorkspacePageDTO>;
  }
  | {
    readonly kind: "detail";
    readonly scope: OperatorResourceDetailScope;
    readonly source: SourceEnvelope<OperatorWorkspaceDTO>;
  };

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/**
 * A transport-unavailable source is still the result of its current request;
 * identity checks apply to data-bearing responses only.
 */
export function operatorResourceSourceMatchesScope(input: OperatorResourceScopedSource): boolean {
  if (!isRecord(input.scope) || input.scope.kind !== input.kind || !isRecord(input.source)) return false;

  switch (input.kind) {
    case "workspaces": {
      const { scope, source } = input;
      if (source.available === false) return true;
      if (source.available !== true || !isRecord(source.data)) return false;
      return source.data.page === scope.page && source.data.pageSize === scope.pageSize;
    }
    case "detail": {
      const { scope, source } = input;
      if (source.available === false) return true;
      if (source.available !== true || !isRecord(source.data) || !("workspace" in source.data)) return false;
      const workspace = source.data.workspace;
      if (!isRecord(workspace) || !("available" in workspace)) return false;
      if (workspace.available === false) return true;
      if (workspace.available !== true || !("data" in workspace) || !isRecord(workspace.data)) return false;
      return workspace.data.id === scope.workspaceId;
    }
  }
}

export interface OperatorResourceReadState {
  readonly workspaces: RemoteState<SourceEnvelope<OperatorWorkspacePageDTO>>;
  readonly detail: RemoteState<SourceEnvelope<OperatorWorkspaceDTO>>;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function settledRemote<T>(source: SourceEnvelope<T>, error?: string): RemoteState<SourceEnvelope<T>> {
  return { value: source, loading: false, error: error || "" };
}

export function createOperatorResourceReadState(): OperatorResourceReadState {
  return {
    workspaces: emptyRemote(),
    detail: emptyRemote()
  };
}

export type OperatorResourceCompletion =
  | {
    readonly kind: "workspaces";
    readonly activeScope: OperatorResourceListScope;
    readonly responseScope: OperatorResourceListScope;
    readonly source: SourceEnvelope<OperatorWorkspacePageDTO>;
    readonly error?: string;
  }
  | {
    readonly kind: "detail";
    readonly activeScope: OperatorResourceDetailScope;
    readonly responseScope: OperatorResourceDetailScope;
    readonly source: SourceEnvelope<OperatorWorkspaceDTO>;
    readonly error?: string;
  };

function completionSourceMatchesScope(completion: OperatorResourceCompletion): boolean {
  switch (completion.kind) {
    case "workspaces":
      return operatorResourceSourceMatchesScope({
        kind: "workspaces",
        scope: completion.activeScope,
        source: completion.source
      });
    case "detail":
      return operatorResourceSourceMatchesScope({
        kind: "detail",
        scope: completion.activeScope,
        source: completion.source
      });
  }
}

export function applyOperatorResourceCompletion(
  state: OperatorResourceReadState,
  completion: OperatorResourceCompletion
): OperatorResourceReadState | null {
  if (!operatorResourceScopeIsCurrent(completion.activeScope, completion.responseScope)
    || !completionSourceMatchesScope(completion)) return null;

  switch (completion.kind) {
    case "workspaces":
      return { ...state, workspaces: settledRemote(completion.source, completion.error) };
    case "detail":
      return { ...state, detail: settledRemote(completion.source, completion.error) };
  }
}
