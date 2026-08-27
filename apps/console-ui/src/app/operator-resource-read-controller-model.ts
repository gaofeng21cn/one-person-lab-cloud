import type {
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  OperatorWorkspaceRuntimeImagePolicyDTO,
  OperatorWorkspaceRuntimeImagePreviewDTO,
  SourceEnvelope
} from "../api/dtos.ts";
import type { RemoteState } from "./console-controller-types.ts";

export const OPERATOR_RESOURCE_PAGE_SIZE = 20 as const;

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

export interface OperatorResourcePolicyScope extends OperatorResourceReadEpoch {
  readonly kind: "imagePolicy";
  readonly requestGeneration: number;
}

export interface OperatorResourcePreviewScope extends OperatorResourceReadEpoch {
  readonly kind: "imagePreview";
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export type OperatorResourceScope =
  | OperatorResourceListScope
  | OperatorResourceDetailScope
  | OperatorResourcePolicyScope
  | OperatorResourcePreviewScope;

export interface OperatorResourceListScopeInput extends OperatorResourceReadEpoch {
  readonly requestGeneration: number;
  readonly page: number;
}

export interface OperatorResourceDetailScopeInput extends OperatorResourceReadEpoch {
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export interface OperatorResourcePolicyScopeInput extends OperatorResourceReadEpoch {
  readonly requestGeneration: number;
}

export interface OperatorResourcePreviewScopeInput extends OperatorResourceReadEpoch {
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export function createOperatorResourceListScope(input: OperatorResourceListScopeInput): OperatorResourceListScope {
  return { ...input, kind: "workspaces", pageSize: OPERATOR_RESOURCE_PAGE_SIZE };
}

export function createOperatorResourceDetailScope(input: OperatorResourceDetailScopeInput): OperatorResourceDetailScope {
  return { ...input, kind: "detail" };
}

export function createOperatorResourcePolicyScope(input: OperatorResourcePolicyScopeInput): OperatorResourcePolicyScope {
  return { ...input, kind: "imagePolicy" };
}

export function createOperatorResourcePreviewScope(input: OperatorResourcePreviewScopeInput): OperatorResourcePreviewScope {
  return { ...input, kind: "imagePreview" };
}

export function operatorResourceScopeKey(scope: OperatorResourceScope): string {
  const identity = scope.kind === "workspaces"
    ? `page=${scope.page}&pageSize=${scope.pageSize}`
    : scope.kind === "imagePolicy"
      ? "identity=global"
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

type OperatorResourceSource =
  | SourceEnvelope<OperatorWorkspacePageDTO>
  | SourceEnvelope<OperatorWorkspaceDTO>
  | SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>
  | SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>;

/**
 * A transport-unavailable source is still the result of its current request;
 * identity checks apply to data-bearing responses only.
 */
export function operatorResourceSourceMatchesScope(
  scope: OperatorResourceScope,
  source: OperatorResourceSource
): boolean {
  if (scope.kind === "workspaces") {
    const page = source as SourceEnvelope<OperatorWorkspacePageDTO>;
    return !page.available || (page.data.page === scope.page && page.data.pageSize === scope.pageSize);
  }

  if (scope.kind === "detail") {
    const detail = source as SourceEnvelope<OperatorWorkspaceDTO>;
    return !detail.available
      || !detail.data.workspace.available
      || detail.data.workspace.data.id === scope.workspaceId;
  }

  if (scope.kind === "imagePreview") {
    const preview = source as SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>;
    return !preview.available || preview.data.workspaceId === scope.workspaceId;
  }

  return scope.kind === "imagePolicy";
}

export function operatorResourceProjectionStatus(source: OperatorResourceSource) {
  return source.status;
}

export interface OperatorResourceReadState {
  readonly workspaces: RemoteState<SourceEnvelope<OperatorWorkspacePageDTO>>;
  readonly detail: RemoteState<SourceEnvelope<OperatorWorkspaceDTO>>;
  readonly imagePolicy: RemoteState<SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>>;
  readonly imagePreview: RemoteState<SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>>;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function settledRemote<T>(source: SourceEnvelope<T>, error?: string): RemoteState<SourceEnvelope<T>> {
  return { value: source, loading: false, error: error || "" };
}

export function createOperatorResourceReadState(): OperatorResourceReadState {
  return {
    workspaces: emptyRemote(),
    detail: emptyRemote(),
    imagePolicy: emptyRemote(),
    imagePreview: emptyRemote()
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
  }
  | {
    readonly kind: "imagePolicy";
    readonly activeScope: OperatorResourcePolicyScope;
    readonly responseScope: OperatorResourcePolicyScope;
    readonly source: SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>;
    readonly error?: string;
  }
  | {
    readonly kind: "imagePreview";
    readonly activeScope: OperatorResourcePreviewScope;
    readonly responseScope: OperatorResourcePreviewScope;
    readonly source: SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>;
    readonly error?: string;
  };

export function applyOperatorResourceCompletion(
  state: OperatorResourceReadState,
  completion: OperatorResourceCompletion
): OperatorResourceReadState | null {
  if (!operatorResourceScopeIsCurrent(completion.activeScope, completion.responseScope)
    || !operatorResourceSourceMatchesScope(completion.activeScope, completion.source)) return null;

  switch (completion.kind) {
    case "workspaces":
      return { ...state, workspaces: settledRemote(completion.source, completion.error) };
    case "detail":
      return { ...state, detail: settledRemote(completion.source, completion.error) };
    case "imagePolicy":
      return { ...state, imagePolicy: settledRemote(completion.source, completion.error) };
    case "imagePreview":
      return { ...state, imagePreview: settledRemote(completion.source, completion.error) };
  }
}
