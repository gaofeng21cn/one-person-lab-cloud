import type {
  PricingCatalogResponse,
  WorkspaceLaunchRequest,
  WorkspaceLaunchResponse
} from "../api/dtos.ts";
import { isTerminalWorkspaceLaunch } from "../api/workspaces-api.ts";

export interface WorkspaceLaunchIntent {
  readonly input: Readonly<WorkspaceLaunchRequest>;
  readonly idempotencyKey: string;
}

export type WorkspaceLaunchIntentResolution =
  | { kind: "ready"; intent: WorkspaceLaunchIntent }
  | { kind: "conflict" };

export type WorkspaceLaunchRecovery =
  | { kind: "none" }
  | { kind: "resume"; operation: WorkspaceLaunchResponse }
  | { kind: "conflict" };

function sameWorkspaceLaunchInput(left: WorkspaceLaunchRequest, right: WorkspaceLaunchRequest): boolean {
  return left.name === right.name
    && left.packageId === right.packageId
    && left.autoRenew === right.autoRenew;
}

export function resolveWorkspaceLaunchIntent(
  current: WorkspaceLaunchIntent | null,
  input: WorkspaceLaunchRequest,
  createIdempotencyKey: () => string
): WorkspaceLaunchIntentResolution {
  if (current) {
    return sameWorkspaceLaunchInput(current.input, input)
      ? { kind: "ready", intent: current }
      : { kind: "conflict" };
  }
  return {
    kind: "ready",
    intent: { input: { ...input }, idempotencyKey: createIdempotencyKey() }
  };
}

export function shouldRetainWorkspaceLaunchIntent(error: unknown): boolean {
  if (!error || typeof error !== "object" || !("payload" in error)) return false;
  const payload = (error as { payload?: unknown }).payload;
  return Boolean(
    payload
    && typeof payload === "object"
    && "status" in payload
    && payload.status === "unknown"
  );
}

export function classifyWorkspaceLaunchRecovery(
  operations: WorkspaceLaunchResponse[]
): WorkspaceLaunchRecovery {
  const active = operations.filter((operation) => !isTerminalWorkspaceLaunch(operation.status));
  if (active.length === 0) return { kind: "none" };
  if (active.length === 1) return { kind: "resume", operation: active[0] };
  return { kind: "conflict" };
}

export function shouldPollWorkspaceLaunch(operation: WorkspaceLaunchResponse): boolean {
  return operation.status !== "manual_review" && !isTerminalWorkspaceLaunch(operation.status);
}

export function workspaceLaunchSubmission(
  input: WorkspaceLaunchRequest,
  resourceBillingMode: PricingCatalogResponse["resourceBillingMode"]
): WorkspaceLaunchRequest {
  return resourceBillingMode === "none" ? { ...input, autoRenew: false } : { ...input };
}
