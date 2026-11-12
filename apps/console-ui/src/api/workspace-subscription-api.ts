import { getJson } from "./console-api.ts";

// Read model of the existing Subscription schema. Workspace remains the sole
// writer of accepted price, period, renewal and provenance facts.
export interface WorkspaceSubscription {
  id: string;
  workspaceId: string;
  currentPeriodStart: string;
  currentPeriodEnd: string;
  periodMonths: number;
  status: "pending" | "active" | "expired" | "terminated";
  createdAt: string;
  renewalMode: "manual" | "automatic";
  renewalSettingsVersion: string;
  version: string;
  provenance: "quoted" | "legacy_import";
  acceptedQuoteId?: string;
  legacyPurchaseId?: string;
  renewalOperationId?: string;
  renewalConsentId?: string;
  refundPolicyVersionId?: string;
  retentionPolicyVersionId?: string;
  refundTerms?: string;
  retentionTerms?: string;
  currentMonthlyUSDMicros?: string;
  currentPricePolicyVersionId?: string;
}

function nonnegativeInt64(value: unknown): value is string {
  return typeof value === "string" && /^(0|[1-9][0-9]{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n;
}

export async function getWorkspaceSubscription(workspaceId: string, signal?: AbortSignal): Promise<WorkspaceSubscription> {
  const value = await getJson<unknown>(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/subscription`, { signal });
  const invalid = () => new Error("invalid_workspace_subscription");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const subscription = value as Record<string, unknown>;
  const ids = ["id", "workspaceId", "acceptedQuoteId", "legacyPurchaseId", "renewalOperationId", "renewalConsentId", "refundPolicyVersionId", "retentionPolicyVersionId", "currentPricePolicyVersionId"];
  const fields = [...ids, "currentPeriodStart", "currentPeriodEnd", "periodMonths", "status", "createdAt", "renewalMode", "renewalSettingsVersion", "version", "provenance", "refundTerms", "retentionTerms", "currentMonthlyUSDMicros"];
  if (Object.keys(subscription).some((field) => !fields.includes(field)) || subscription.workspaceId !== workspaceId) throw invalid();
  for (const field of ["id", "workspaceId", "currentPeriodStart", "currentPeriodEnd", "createdAt", "status", "renewalMode", "provenance"]) {
    if (typeof subscription[field] !== "string" || !subscription[field].trim()) throw invalid();
  }
  for (const field of ids) {
    if (subscription[field] !== undefined && (typeof subscription[field] !== "string" || !subscription[field].trim() || subscription[field].length > 256)) throw invalid();
  }
  for (const field of ["currentPeriodStart", "currentPeriodEnd", "createdAt"]) {
    if (!Number.isFinite(Date.parse(subscription[field] as string))) throw invalid();
  }
  if (Date.parse(subscription.currentPeriodEnd as string) <= Date.parse(subscription.currentPeriodStart as string)
    || !Number.isInteger(subscription.periodMonths) || (subscription.periodMonths as number) < 1 || (subscription.periodMonths as number) > 2147483647
    || !nonnegativeInt64(subscription.renewalSettingsVersion) || !nonnegativeInt64(subscription.version)
    || !["pending", "active", "expired", "terminated"].includes(subscription.status as string)
    || !["quoted", "legacy_import"].includes(subscription.provenance as string)
    || !["manual", "automatic"].includes(subscription.renewalMode as string)) throw invalid();
  for (const field of ["refundTerms", "retentionTerms"]) {
    if (subscription[field] !== undefined && (typeof subscription[field] !== "string" || !subscription[field].trim())) throw invalid();
  }
  const price = subscription.currentMonthlyUSDMicros;
  const policy = subscription.currentPricePolicyVersionId;
  if ((price !== undefined && !nonnegativeInt64(price)) || (price === undefined) !== (policy === undefined)) throw invalid();
  if (subscription.provenance === "quoted") {
    if (price === undefined || subscription.legacyPurchaseId !== undefined
      || ["acceptedQuoteId", "refundPolicyVersionId", "retentionPolicyVersionId", "refundTerms", "retentionTerms"].some((field) => subscription[field] === undefined)) throw invalid();
  } else if (subscription.legacyPurchaseId === undefined || subscription.acceptedQuoteId !== undefined) throw invalid();
  if (subscription.renewalMode === "automatic" && subscription.renewalConsentId === undefined) throw invalid();
  return subscription as unknown as WorkspaceSubscription;
}
