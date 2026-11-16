import type {
  PlanId,
  PricingCatalogResponse,
  PricingPlan,
  PricingPreviewRequest,
  PricingPreviewResponse,
  WorkspacePricePreview
} from "../api/dtos.ts";

export type WorkspaceLaunchRecoveryState = "idle" | "checking" | "clear" | "conflict" | "unavailable";

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

// workspaceLaunchPreviewRequest asks for the package purchase itself. A
// component preview carries no total, so asking the owner for the wrong resource
// type would return a number this client cannot turn into a price.
export function workspaceLaunchPreviewRequest(planId: PlanId): PricingPreviewRequest {
  return { resourceType: "workspace", packageId: planId };
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
