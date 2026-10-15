import { getJson } from "./console-api.ts";

// Projection of the existing Gateway WalletOperation contract. Gateway remains
// the record owner; the browser never recomputes settlement from these pages.
export interface WorkspaceTransaction {
  id: string;
  workspaceId: string;
  kind: "charge" | "refund" | "recharge";
  status: "requested" | "confirmed" | "rejected" | "unknown";
  amountUSDMicros: string;
  createdAt: string;
  updatedAt: string;
  purpose?: string;
  originalChargeOperationId?: string;
  externalReference?: string;
  receiptId?: string;
  errorCode?: string;
  planChangeId?: string;
  coverageStart?: string;
  coverageEnd?: string;
}

export interface WorkspaceTransactionPage {
  items: WorkspaceTransaction[];
  nextCursor?: string;
}

export function decodeWorkspaceTransactions(value: unknown, workspaceId: string): WorkspaceTransactionPage {
  const invalid = () => new Error("invalid_workspace_transactions");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const page = value as Record<string, unknown>;
  if (!Array.isArray(page.items) || (page.nextCursor !== undefined && (typeof page.nextCursor !== "string" || !page.nextCursor.trim()))) throw invalid();
  const ids = new Set<string>();
  const items = page.items.map((raw) => {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
    const item = raw as Record<string, unknown>;
    if (typeof item.id !== "string" || !item.id || ids.has(item.id) || item.workspaceId !== workspaceId
      || !["charge", "refund", "recharge"].includes(String(item.kind))
      || !["requested", "confirmed", "rejected", "unknown"].includes(String(item.status))
      || typeof item.amountUSDMicros !== "string" || !/^[1-9]\d{0,18}$/.test(item.amountUSDMicros) || BigInt(item.amountUSDMicros) > 9223372036854775807n
      || typeof item.createdAt !== "string" || !Number.isFinite(Date.parse(item.createdAt))
      || typeof item.updatedAt !== "string" || !Number.isFinite(Date.parse(item.updatedAt))) throw invalid();
    for (const field of ["purpose", "originalChargeOperationId", "externalReference", "receiptId", "errorCode", "planChangeId", "coverageStart", "coverageEnd"]) {
      if (item[field] !== undefined && (typeof item[field] !== "string" || !item[field])) throw invalid();
    }
    ids.add(item.id);
    return item as unknown as WorkspaceTransaction;
  });
  return { items, ...(typeof page.nextCursor === "string" ? { nextCursor: page.nextCursor } : {}) };
}

export async function getWorkspaceTransactions(workspaceId: string, cursor: string, signal: AbortSignal): Promise<WorkspaceTransactionPage> {
  const query = new URLSearchParams({ limit: "25" });
  if (cursor) query.set("cursor", cursor);
  const page = decodeWorkspaceTransactions(await getJson<unknown>(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/transactions?${query}`, { signal }), workspaceId);
  if (cursor && page.nextCursor === cursor) throw new Error("repeated_workspace_transaction_cursor");
  return page;
}

export interface WorkspaceTransactionTrendDay {
  date: string;
  confirmedChargeUSDMicros: string;
  confirmedRefundUSDMicros: string;
}
export interface WorkspaceTransactionTrend {
  workspaceId: string;
  periodStart: string;
  periodEnd: string;
  days: WorkspaceTransactionTrendDay[];
  unknownChargeUSDMicros: string;
  unknownRefundUSDMicros: string;
  requestedChargeUSDMicros: string;
  requestedRefundUSDMicros: string;
}

function validTrendAmount(value: unknown): value is string {
  return typeof value === "string" && /^(0|[1-9]\d{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n;
}

export function decodeWorkspaceTransactionTrend(value: unknown, workspaceId: string): WorkspaceTransactionTrend {
  const invalid = () => new Error("invalid_workspace_transaction_trend");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const trend = value as Record<string, unknown>;
  if (trend.workspaceId !== workspaceId || typeof trend.periodStart !== "string" || typeof trend.periodEnd !== "string" || !Array.isArray(trend.days) || trend.days.length !== 14) throw invalid();
  const start = Date.parse(trend.periodStart), end = Date.parse(trend.periodEnd);
  const dayMillis = 86_400_000;
  if (!Number.isFinite(start) || !Number.isFinite(end) || start % dayMillis !== 0 || end < start + 13 * dayMillis || end >= start + 14 * dayMillis) throw invalid();
  for (const field of ["unknownChargeUSDMicros", "unknownRefundUSDMicros", "requestedChargeUSDMicros", "requestedRefundUSDMicros"]) {
    if (!validTrendAmount(trend[field])) throw invalid();
  }
  for (const [index, raw] of trend.days.entries()) {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw invalid();
    const day = raw as Record<string, unknown>;
    if (day.date !== new Date(start + index * dayMillis).toISOString().slice(0, 10) || !validTrendAmount(day.confirmedChargeUSDMicros) || !validTrendAmount(day.confirmedRefundUSDMicros)) throw invalid();
  }
  return trend as unknown as WorkspaceTransactionTrend;
}

export async function getWorkspaceTransactionTrend(workspaceId: string, signal: AbortSignal): Promise<WorkspaceTransactionTrend> {
  return decodeWorkspaceTransactionTrend(await getJson<unknown>(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/transactions/trend`, { signal }), workspaceId);
}
