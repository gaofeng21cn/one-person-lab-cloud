import type {
  SourceEnvelope,
  WorkspaceRuntimeDTO
} from "../api/dtos.ts";
import type { RemoteState } from "./console-controller-types.ts";

export interface FabricRuntimeReadScope {
  readonly sessionGeneration: number;
  readonly routeGeneration: number;
  readonly requestGeneration: number;
  readonly workspaceId: string;
}

export function createFabricRuntimeReadScope(scope: FabricRuntimeReadScope): FabricRuntimeReadScope {
  return { ...scope };
}

function fabricRuntimeReadScopeKey(scope: FabricRuntimeReadScope): string {
  return `session=${scope.sessionGeneration}|route=${scope.routeGeneration}|request=${scope.requestGeneration}|workspace=${scope.workspaceId}`;
}

export function fabricRuntimeReadScopeIsCurrent(
  expected: FabricRuntimeReadScope,
  current: FabricRuntimeReadScope
): boolean {
  return fabricRuntimeReadScopeKey(expected) === fabricRuntimeReadScopeKey(current);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function fabricRuntimeReadSourceMatchesScope(
  scope: FabricRuntimeReadScope,
  source: SourceEnvelope<WorkspaceRuntimeDTO>
): boolean {
  if (!isRecord(scope) || !scope.workspaceId || !isRecord(source) || source.source !== "fabric") return false;
  if (source.available === false) return true;
  return source.available === true
    && isRecord(source.data)
    && source.data.workspaceId === scope.workspaceId;
}

export interface FabricRuntimeReadState {
  readonly runtime: RemoteState<SourceEnvelope<WorkspaceRuntimeDTO>>;
}

export function createFabricRuntimeReadState(): FabricRuntimeReadState {
  return {
    runtime: { value: null, loading: false, error: "" }
  };
}

export interface FabricRuntimeReadCompletion {
  readonly activeScope: FabricRuntimeReadScope;
  readonly responseScope: FabricRuntimeReadScope;
  readonly source: SourceEnvelope<WorkspaceRuntimeDTO>;
  readonly error?: string;
}

export function applyFabricRuntimeReadCompletion(
  state: FabricRuntimeReadState,
  completion: FabricRuntimeReadCompletion
): FabricRuntimeReadState | null {
  if (!fabricRuntimeReadScopeIsCurrent(completion.responseScope, completion.activeScope)
    || !fabricRuntimeReadSourceMatchesScope(completion.responseScope, completion.source)) {
    return null;
  }
  return {
    ...state,
    runtime: {
      value: completion.source,
      loading: false,
      error: completion.error || ""
    }
  };
}
