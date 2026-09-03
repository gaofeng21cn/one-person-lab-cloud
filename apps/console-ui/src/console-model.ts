import type { GatewayKeySecretDTO, GatewayWallet, ReadinessFact, WorkspaceRuntimeDTO } from "./api/dtos.ts";
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

export function defaultAuthenticatedRoute(_isOperator = false): string {
  return "/console/overview";
}

export function formatUsdMicros(value: unknown): string {
  let micros: bigint;
  if (typeof value === "string" && /^-?(0|[1-9][0-9]*)$/.test(value)) {
    micros = BigInt(value);
  } else if (typeof value === "number" && Number.isSafeInteger(value)) {
    micros = BigInt(value);
  } else {
    return "-";
  }
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

export function formatAvailableBalance(balance: Partial<GatewayWallet> & { available?: boolean } = {}): string {
  return balance.available === false ? "暂不可用" : formatUsdMicros(balance.usdMicros);
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

export function workspaceStatusLabel(runtime: Partial<WorkspaceRuntimeDTO> = {}): string {
  if (runtime.status === "running" && runtime.ready === true) return "运行中";
  if (runtime.status === "unready" || runtime.status === "not_found" || runtime.status === "destroyed") return "暂不可用";
  return "暂不可用";
}

export function readinessRows(runtime: ReadinessFact | null, production: ReadinessFact | null) {
  const row = (label: string, value: ReadinessFact | null) => ({
    label,
    status: value?.ready === true ? "正常" : value?.ready === false ? "需处理" : "暂不可用",
    updatedAt: value?.generatedAt || value?.updatedAt || "-"
  });
  return [row("运行依赖", runtime), row("生产依赖", production)];
}

export function maskGatewayKey(key: GatewayKeySecretDTO | null): GatewayKeySecretDTO | null {
  if (!key) return null;
  return { ...key, value: "" };
}
