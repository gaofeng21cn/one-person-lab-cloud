import { cloudIdentity } from "./app/console-identity.ts";
import type { ConsoleNavigationId, CustomerConsoleRoute } from "./app/console-router.ts";

type ConsoleMenuItem = {
  id: ConsoleNavigationId;
  label: string;
  mobileLabel?: string;
  path: string;
  icon: string;
};

type CustomerApiRouteKind = Extract<CustomerConsoleRoute, { navigationId: "customer.api" }>["kind"];
type ApiMenuItem = {
  kind: CustomerApiRouteKind;
  label: string;
  path: string;
};

export const customerMenu = Object.freeze([
  { id: "customer.overview", label: "概览", path: "/console/overview", icon: "LayoutDashboard" },
  ...(cloudIdentity ? [{ id: "customer.agents", label: "智能体", path: "/console/agents", icon: "Bot" } as const] : []),
  { id: "customer.workspaces", label: "工作空间", path: "/console/workspaces", icon: "Database" },
  { id: "customer.api", label: "OPL Gateway", mobileLabel: "Gateway", path: "/console/api", icon: "Server" },
  { id: "customer.billing", label: "费用", path: "/console/billing", icon: "ReceiptText" }
] as const satisfies readonly ConsoleMenuItem[]);

export const apiMenu = Object.freeze([
  { kind: "customer.api.overview", label: "服务信息", path: "/console/api" },
  { kind: "customer.api.usage", label: "用量", path: "/console/api/usage" },
  { kind: "customer.api.keys", label: "API 密钥", path: "/console/api/keys" }
] as const satisfies readonly ApiMenuItem[]);

export const adminMenu = Object.freeze([
  { id: "admin.overview", label: "运维概览", path: "/admin/overview", icon: "LayoutDashboard" },
  { id: "admin.accounts", label: "客户与计费账户", path: "/admin/accounts", icon: "UsersRound" },
  { id: "admin.billing", label: "计费复核", path: "/admin/billing", icon: "CircleDollarSign" },
  { id: "admin.resources", label: "资源状态", path: "/admin/resources", icon: "Database" },
  { id: "admin.system", label: "系统状态", path: "/admin/system", icon: "Activity" },
  { id: "admin.announcements", label: "公告管理", path: "/admin/announcements", icon: "Megaphone" }
] as const satisfies readonly ConsoleMenuItem[]);

export function parseUsdMicros(value: unknown): bigint | null {
  if (typeof value === "bigint") return value;
  if (typeof value === "string" && /^-?(0|[1-9][0-9]*)$/.test(value)) {
    return BigInt(value);
  }
  if (typeof value === "number" && Number.isSafeInteger(value)) {
    return BigInt(value);
  }
  return null;
}

export function formatUsdMicros(value: unknown): string {
  const micros = parseUsdMicros(value);
  if (micros === null) return "-";
  const negative = micros < 0n;
  const absolute = negative ? -micros : micros;
  const roundedCents = (absolute + 5_000n) / 10_000n;
  const dollars = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 }).format(roundedCents / 100n);
  const cents = String(roundedCents % 100n).padStart(2, "0");
  return `${negative ? "-" : ""}$${dollars}.${cents}`;
}

export function formatCount(value: unknown): string {
  return typeof value === "number" && Number.isSafeInteger(value)
    ? new Intl.NumberFormat("zh-CN").format(value)
    : "-";
}

export function hasSufficientWorkspaceLaunchBalance(balanceUsdMicros: string, quoteUsdMicros: number): boolean {
  return /^\d+$/.test(balanceUsdMicros)
    && Number.isSafeInteger(quoteUsdMicros)
    && quoteUsdMicros >= 0
    && BigInt(balanceUsdMicros) >= BigInt(quoteUsdMicros);
}

export function formatDate(value: unknown, includeTime = false): string {
  if (!value) return "-";
  const date = new Date(String(value));
  if (Number.isNaN(date.getTime())) return "-";
  return new Intl.DateTimeFormat("zh-CN", includeTime
    ? { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }
    : { year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
}
