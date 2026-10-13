import type {
  PlanId,
  PricingCatalogResponse,
  PricingPlan,
  PricingPreviewResponse,
  WorkspaceLaunchRequest,
  WorkspaceLaunchResponse,
  WorkspacePricePreview
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

// workspacePricePreview keeps a quote only when the owner priced it in full.
// "No price" means the owner returned no total or no components, and that is
// reported as no price rather than as a fabricated amount. A total of zero is
// not that case: when resource billing is off the owner zeroes every component
// and returns 0, which is the owner's own price for this purchase.
export function workspacePricePreview(response: PricingPreviewResponse): WorkspacePricePreview | null {
  const { compute, storage, totalChargeUsdMicros } = response;
  if (!compute || !storage || typeof totalChargeUsdMicros !== "number") return null;
  return {
    resourceType: "workspace",
    priceVersion: response.priceVersion,
    packageId: response.packageId,
    currency: response.currency,
    displayCurrency: response.displayCurrency || response.currency,
    billingUnit: response.billingUnit || "",
    compute,
    storage,
    totalChargeUsdMicros
  };
}

// workspaceLaunchPricingSelection is the single derivation of the selected plan
// and its price. Both owners of the number are owner readbacks: the plan list
// from the Resource Catalog and the price from the owner's own preview. A plan
// the catalog did not return, or a price the owner did not quote, stays null.
export function workspaceLaunchPricingSelection(state: {
  catalog: PricingCatalogResponse | null;
  planId: PlanId;
  previews: Partial<Record<PlanId, WorkspacePricePreview>>;
}): { selectedPlan: PricingPlan | null; selectedPrice: number | null } {
  const selectedPlan = state.catalog?.packages.find((plan) => plan.id === state.planId) || null;
  const selectedPrice = selectedPlan ? state.previews[selectedPlan.id]?.totalChargeUsdMicros ?? null : null;
  return { selectedPlan, selectedPrice };
}

// WorkspaceLaunchReviewState is what a caller already holds. WorkspaceLaunchController
// satisfies it structurally, so the page and the controller read the same fields.
export interface WorkspaceLaunchReviewState {
  launchRecoveryState: WorkspaceLaunchRecoveryState;
  launchName: string;
  selectedPlan: PricingPlan | null;
  selectedPrice: number | null;
  balanceSufficient: boolean | null;
}

export function workspaceLaunchReviewReadiness(state: WorkspaceLaunchReviewState): WorkspaceLaunchReviewReadiness {
  return {
    recoveryState: state.launchRecoveryState,
    hasName: Boolean(state.launchName.trim()),
    hasSelectedPlan: Boolean(state.selectedPlan),
    selectedPriceKnown: state.selectedPrice !== null,
    balanceSufficient: state.balanceSufficient === true
  };
}

// The confirm button and the controller's own review action ask this one
// function, so the page can no longer offer a step the controller refuses, and a
// second readiness expression cannot drift away from the model.
export function workspaceLaunchReviewBlocked(state: WorkspaceLaunchReviewState): boolean {
  return !canReviewWorkspaceLaunch(workspaceLaunchReviewReadiness(state));
}

function micros(value: string | null): bigint | null {
  return value !== null && /^\d+$/.test(value) ? BigInt(value) : null;
}

// A read that priced only some packages names the ones it could not price, so
// the page can say which plan is unpriced instead of reporting the whole catalog
// as unavailable. Any other error means the catalog itself could not be read.
export function workspaceLaunchUnpricedPackages(error: string): string[] {
  const match = /^unpriced_packages:(.+)$/.exec(error);
  return match ? match[1].split(",").filter(Boolean) : [];
}

// A package purchase is prepaid from the customer's own Gateway balance, so that
// balance is the number that answers this question. The Agent launch wallet
// belongs to the Agent product and says nothing about whether this purchase can
// be paid for. null means the owner has not told us yet, and null blocks review:
// an unpriced or unread purchase must not be offered as affordable.
export function workspaceLaunchBalance(state: {
  resourceBillingMode: "enabled" | "none" | undefined;
  customerOwned: boolean;
  selectedPrice: number | null;
  walletUsdMicros: string | null;
}): boolean | null {
  if (state.customerOwned) return true;
  // Nothing is charged when resource billing is off, so no balance is owed.
  if (state.resourceBillingMode === "none") return true;
  if (state.selectedPrice === null || !Number.isSafeInteger(state.selectedPrice) || state.selectedPrice < 0) return null;
  const balance = micros(state.walletUsdMicros);
  return balance === null ? null : balance >= BigInt(state.selectedPrice);
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

export type AgentLaunchStep = "configure" | "quote" | "operation";

export interface AgentLaunchReadiness {
  sourceReady: boolean;
  hasName: boolean;
  hasCapabilityVersion: boolean;
  hasComputePlan: boolean;
  hasStoragePlan: boolean;
  modelSelectionsReady: boolean;
  walletReadbackReady: boolean;
  walletSufficient: boolean;
  quoteReady: boolean;
  quoteCurrent: boolean;
}

export function canCreateAgentQuote(readiness: AgentLaunchReadiness): boolean {
  return readiness.sourceReady && readiness.hasName && readiness.hasCapabilityVersion
    && readiness.hasComputePlan && readiness.hasStoragePlan && readiness.modelSelectionsReady
    && readiness.walletReadbackReady;
}

export function canCreateAgentWorkspace(readiness: AgentLaunchReadiness, confirmed: boolean): boolean {
  return canCreateAgentQuote(readiness) && readiness.walletSufficient && readiness.quoteReady && readiness.quoteCurrent && confirmed;
}

// The GLOSSARY splits Workspaces in two: a Legacy Workspace is opened through
// the Control Plane, a Cloud Workspace through the Workspace owner. Which of the
// two a build can open is decided by the build identity alone — the same identity
// that picks the customer Workspace owner, the customer menu and the routes.
//
// A loaded CapabilityVersion list is deliberately not part of that decision. An
// empty array is truthy in JS and once read as "this account belongs to the
// Agent/BFF flow", which handed every Legacy Workspace identity a page built
// entirely on /api/v2. It is also the wrong signal the other way: the default OPL
// App opens from an approved Runtime Release and carries no CapabilityVersion at
// all. Owner reads stay reported where the customer already reads them — the
// Agent page's loading, error and empty states.
export interface AgentLaunchSurfaceProbe {
  readonly cloudIdentity: boolean;
  readonly agentCapabilityVersions?: unknown;
  readonly reviewAgentLaunch?: unknown;
}

export function isAgentLaunchSurface(probe: AgentLaunchSurfaceProbe): boolean {
  if (!probe.cloudIdentity) return false;
  return typeof probe.reviewAgentLaunch === "function";
}

export function operationPollDelayMs(pollAfterSeconds: number | undefined): number {
  if (!Number.isInteger(pollAfterSeconds) || pollAfterSeconds === undefined || pollAfterSeconds < 1 || pollAfterSeconds > 300) {
    throw new Error("invalid_operation_poll_after_seconds");
  }
  return pollAfterSeconds * 1000;
}
