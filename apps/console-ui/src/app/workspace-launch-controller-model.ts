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

export type WorkspaceLaunchRecoveryState = "idle" | "checking" | "clear" | "conflict" | "unavailable";

export interface WorkspaceLaunchReviewReadiness {
  recoveryState: WorkspaceLaunchRecoveryState;
  hasName: boolean;
  hasSelectedPlan: boolean;
  selectedPriceKnown: boolean;
  balanceSufficient: boolean;
}

export interface WorkspaceLaunchSubmitReadiness extends WorkspaceLaunchReviewReadiness {
  sessionAvailable: boolean;
  busy: boolean;
  step: "configure" | "confirm";
  confirmed: boolean;
}

function sameWorkspaceLaunchInput(left: WorkspaceLaunchRequest, right: WorkspaceLaunchRequest): boolean {
  return left.name === right.name
    && left.packageId === right.packageId
    && left.autoRenew === right.autoRenew
    && left.provisioningMode === right.provisioningMode;
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

export function canReviewWorkspaceLaunch(readiness: WorkspaceLaunchReviewReadiness): boolean {
  return readiness.recoveryState === "clear"
    && readiness.hasName
    && readiness.hasSelectedPlan
    && readiness.selectedPriceKnown
    && readiness.balanceSufficient;
}

export function canSubmitWorkspaceLaunch(readiness: WorkspaceLaunchSubmitReadiness): boolean {
  return readiness.sessionAvailable
    && !readiness.busy
    && readiness.step === "confirm"
    && readiness.confirmed
    && canReviewWorkspaceLaunch(readiness);
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

// workspaceLaunchProvisioningMode is the one place this client states its
// provisioning shape. A customer Launch delivers resourced capacity only; the
// application is deployed afterwards as its own authorized operation.
export const workspaceLaunchProvisioningMode = "resource_only" as const;

// WorkspaceLaunchIntentInput is what a customer chooses: a name, a plan and
// whether the monthly charge renews. The provisioning shape is not theirs to
// pick, so submission is the single owner that composes the wire request.
export type WorkspaceLaunchIntentInput = Omit<WorkspaceLaunchRequest, "provisioningMode">;

export function workspaceLaunchSubmission(
  input: WorkspaceLaunchIntentInput,
  resourceBillingMode: PricingCatalogResponse["resourceBillingMode"]
): WorkspaceLaunchRequest {
  const submitted: WorkspaceLaunchRequest = {
    ...input,
    provisioningMode: workspaceLaunchProvisioningMode
  };
  return resourceBillingMode === "none" ? { ...submitted, autoRenew: false } : submitted;
}
