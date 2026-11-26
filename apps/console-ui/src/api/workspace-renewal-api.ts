import { postJson } from "./console-api.ts";
import type { OpenAPIOperation, OpenAPIQuote } from "./dtos.ts";

// A manual renewal is a Workspace-owner command. The Resource Catalog prices the
// renewal, the Workspace owner charges the wallet and extends the accepted
// period. The Console names the renewal quote it wants and publishes the
// owner's own operation; it never derives a price, a period or a charge of its
// own, and it never reads another owner's financial fact to answer a customer.

export type WorkspaceRenewalQuoteRequest = {
  workspaceId: string;
  computePlanId: string;
  storagePlanId: string;
};

export type WorkspaceRenewalQuote = OpenAPIQuote & {
  purpose: "renew";
  workspaceId: string;
};

function text(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function usdMicros(value: unknown): string | undefined {
  return typeof value === "string" && /^(0|[1-9]\d*)$/.test(value) ? value : undefined;
}

function instant(value: unknown): number | undefined {
  if (typeof value !== "string" || !value.trim()) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

// renewalQuoteRequest is the Catalog's own renew variant: the renewal is bound
// to one existing Workspace and keeps that Workspace's own application and
// model configuration, so the request carries no selection of its own.
export function renewalQuoteRequest(input: WorkspaceRenewalQuoteRequest) {
  return {
    purpose: "renew",
    workspaceId: input.workspaceId,
    computePlanId: input.computePlanId,
    storagePlanId: input.storagePlanId,
    modelSelections: [],
    periodMonths: 1
  };
}

// createWorkspaceRenewalQuote asks the Resource Catalog to price one renewal of
// an existing Workspace. The returned offer is the owner's own fact: the
// Console only checks that the offer is the renewal it asked for.
export async function createWorkspaceRenewalQuote(
  input: WorkspaceRenewalQuoteRequest,
  csrfToken: string,
  idempotencyKey: string
): Promise<WorkspaceRenewalQuote> {
  const value = await postJson<unknown>("/api/v2/quotes", renewalQuoteRequest(input), csrfToken, idempotencyKey);
  const invalid = () => new Error("invalid_workspace_renewal_quote");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const quote = value as Record<string, unknown>;
  if (!text(quote.id) || quote.purpose !== "renew" || quote.workspaceId !== input.workspaceId
    || quote.computePlanId !== input.computePlanId || quote.storagePlanId !== input.storagePlanId
    || quote.periodMonths !== 1 || quote.status !== "offered"
    || !Array.isArray(quote.modelSelections) || quote.modelSelections.length !== 0
    || !Array.isArray(quote.lineItems)
    || !usdMicros(quote.totalUSDMicros)
    || !text(quote.pricePolicyVersionId) || !text(quote.refundPolicyVersionId) || !text(quote.retentionPolicyVersionId)
    || !text(quote.refundTerms) || !text(quote.retentionTerms)
    || instant(quote.periodStart) === undefined || instant(quote.periodEnd) === undefined
    || instant(quote.expiresAt) === undefined || instant(quote.createdAt) === undefined) {
    throw invalid();
  }
  return quote as unknown as WorkspaceRenewalQuote;
}

// renewWorkspace hands the owner the renewal quote the customer confirmed. The
// owner answers with its own accepted operation, which is not a finished
// renewal: the caller polls that operation until it reaches a terminal state.
export async function renewWorkspace(
  workspaceId: string,
  quoteId: string,
  csrfToken: string,
  idempotencyKey: string
): Promise<OpenAPIOperation> {
  const value = await postJson<unknown>(
    `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/renew`,
    { quoteId },
    csrfToken,
    idempotencyKey,
    60_000
  );
  const invalid = () => new Error("invalid_workspace_renewal_operation");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const operation = value as Record<string, unknown>;
  if (!text(operation.operationId) || operation.owner !== "workspace" || operation.kind !== "renew_workspace"
    || operation.resourceId !== workspaceId || !text(operation.status) || !text(operation.stage)) {
    throw invalid();
  }
  return operation as unknown as OpenAPIOperation;
}
