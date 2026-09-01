import { useCallback, useEffect, useState } from "react";

export type ConsoleRouteSurface = "public" | "customer" | "admin";

export type ConsoleNavigationId =
  | "customer.overview"
  | "customer.workspaces"
  | "customer.api"
  | "customer.billing"
  | "customer.announcements"
  | "admin.overview"
  | "admin.accounts"
  | "admin.billing"
  | "admin.resources"
  | "admin.system"
  | "admin.announcements";

type ConsoleRouteDefinition = {
  kind: string;
  surface: ConsoleRouteSurface;
  title: string;
  requiresSession: boolean;
  sensitive: boolean;
  navigationId: ConsoleNavigationId | null;
};

const STATIC_ROUTE_DEFINITIONS = {
  "/": {
    kind: "public.home",
    surface: "public",
    title: "OPL Cloud",
    requiresSession: false,
    sensitive: false,
    navigationId: null
  },
  "/login": {
    kind: "public.login",
    surface: "public",
    title: "登录",
    requiresSession: false,
    sensitive: false,
    navigationId: null
  },
  "/403": {
    kind: "public.forbidden",
    surface: "public",
    title: "无权访问",
    requiresSession: false,
    sensitive: false,
    navigationId: null
  },
  "/console": {
    kind: "customer.overview",
    surface: "customer",
    title: "概览",
    requiresSession: true,
    sensitive: false,
    navigationId: "customer.overview"
  },
  "/console/overview": {
    kind: "customer.overview",
    surface: "customer",
    title: "概览",
    requiresSession: true,
    sensitive: false,
    navigationId: "customer.overview"
  },
  "/console/workspaces": {
    kind: "customer.workspaces",
    surface: "customer",
    title: "Workspace",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.workspaces"
  },
  "/console/workspaces/new": {
    kind: "customer.workspace-new",
    surface: "customer",
    title: "Workspace",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.workspaces"
  },
  "/console/api": {
    kind: "customer.api.overview",
    surface: "customer",
    title: "API 服务",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.api"
  },
  "/console/api/usage": {
    kind: "customer.api.usage",
    surface: "customer",
    title: "API 服务",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.api"
  },
  "/console/api/keys": {
    kind: "customer.api.keys",
    surface: "customer",
    title: "API 服务",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.api"
  },
  "/console/billing": {
    kind: "customer.billing",
    surface: "customer",
    title: "账单",
    requiresSession: true,
    sensitive: false,
    navigationId: "customer.billing"
  },
  "/console/announcements": {
    kind: "customer.announcements",
    surface: "customer",
    title: "公告",
    requiresSession: true,
    sensitive: false,
    navigationId: "customer.announcements"
  },
  "/admin": {
    kind: "admin.overview",
    surface: "admin",
    title: "运维概览",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.overview"
  },
  "/admin/overview": {
    kind: "admin.overview",
    surface: "admin",
    title: "运维概览",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.overview"
  },
  "/admin/accounts": {
    kind: "admin.accounts",
    surface: "admin",
    title: "客户与计费账户",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.accounts"
  },
  "/admin/billing": {
    kind: "admin.billing",
    surface: "admin",
    title: "计费复核",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.billing"
  },
  "/admin/resources": {
    kind: "admin.resources",
    surface: "admin",
    title: "资源状态",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.resources"
  },
  "/admin/system": {
    kind: "admin.system",
    surface: "admin",
    title: "系统状态",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.system"
  },
  "/admin/announcements": {
    kind: "admin.announcements",
    surface: "admin",
    title: "公告管理",
    requiresSession: true,
    sensitive: false,
    navigationId: "admin.announcements"
  }
} as const satisfies Record<string, ConsoleRouteDefinition>;

type StaticConsolePath = keyof typeof STATIC_ROUTE_DEFINITIONS;
type StaticConsoleRoute = {
  [Path in StaticConsolePath]: typeof STATIC_ROUTE_DEFINITIONS[Path] & { path: Path }
}[StaticConsolePath];

type WorkspaceDetailRoute = {
  kind: "customer.workspace-detail";
  path: string;
  surface: "customer";
  title: "Workspace";
  requiresSession: true;
  sensitive: true;
  navigationId: "customer.workspaces";
  workspaceId: string;
};

export type ConsoleRoute = StaticConsoleRoute | WorkspaceDetailRoute;

function normalizePath(pathname: string) {
  const withoutTrailingSlash = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
  if (withoutTrailingSlash === "/console/gateway" || withoutTrailingSlash.startsWith("/console/gateway/")) {
    return `/console/api${withoutTrailingSlash.slice("/console/gateway".length)}`;
  }
  return withoutTrailingSlash;
}

export function parseConsoleRoute(pathname: string): ConsoleRoute | null {
  const path = normalizePath(pathname);
  const staticDefinition = STATIC_ROUTE_DEFINITIONS[path as StaticConsolePath];
  if (staticDefinition) {
    return { ...staticDefinition, path } as StaticConsoleRoute;
  }

  const workspaceDetailPrefix = "/console/workspaces/";
  if (!path.startsWith(workspaceDetailPrefix)) return null;

  const encodedWorkspaceId = path.slice(workspaceDetailPrefix.length);
  if (!encodedWorkspaceId || encodedWorkspaceId.includes("/")) return null;

  let workspaceId: string;
  try {
    workspaceId = decodeURIComponent(encodedWorkspaceId);
  } catch {
    return null;
  }
  if (!workspaceId || workspaceId.includes("/")) return null;

  return {
    kind: "customer.workspace-detail",
    path,
    surface: "customer",
    title: "Workspace",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.workspaces",
    workspaceId
  };
}

export function useConsoleRouter() {
  const [path, setPath] = useState(() => normalizePath(window.location.pathname));

  useEffect(() => {
    const onPopState = () => setPath(normalizePath(window.location.pathname));
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const navigate = useCallback((next: string, replace = false) => {
    const normalized = normalizePath(next);
    if (replace) window.history.replaceState({}, "", normalized);
    else window.history.pushState({}, "", normalized);
    setPath(normalizePath(window.location.pathname));
  }, []);

  return { path, route: parseConsoleRoute(path), navigate };
}

export function isKnownConsoleRoute(path: string) {
  return parseConsoleRoute(path) !== null;
}

export function isSensitiveConsoleRoute(path: string) {
  return parseConsoleRoute(path)?.sensitive ?? false;
}
