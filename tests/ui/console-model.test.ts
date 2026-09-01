import assert from "node:assert/strict";
import test from "node:test";

import {
  isKnownConsoleRoute,
  parseConsoleRoute
} from "../../apps/console-ui/src/app/console-router.ts";
import {
  adminMenu,
  apiMenu,
  customerMenu,
  formatAvailableBalance,
  formatCount,
  formatUsdMicros,
  hasSufficientWorkspaceLaunchBalance,
  readinessRows,
  workspaceStatusLabel
} from "../../apps/console-ui/src/console-model.ts";

const staticConsoleRoutes = [
  ["/", "public.home", "public", "OPL Cloud", false, false, null],
  ["/login", "public.login", "public", "登录", false, false, null],
  ["/403", "public.forbidden", "public", "无权访问", false, false, null],
  ["/console", "customer.overview", "customer", "概览", true, false, "customer.overview"],
  ["/console/overview", "customer.overview", "customer", "概览", true, false, "customer.overview"],
  ["/console/workspaces", "customer.workspaces", "customer", "Workspace", true, true, "customer.workspaces"],
  ["/console/workspaces/new", "customer.workspace-new", "customer", "Workspace", true, true, "customer.workspaces"],
  ["/console/api", "customer.api.overview", "customer", "API 服务", true, true, "customer.api"],
  ["/console/api/usage", "customer.api.usage", "customer", "API 服务", true, true, "customer.api"],
  ["/console/api/keys", "customer.api.keys", "customer", "API 服务", true, true, "customer.api"],
  ["/console/billing", "customer.billing", "customer", "账单", true, false, "customer.billing"],
  ["/console/announcements", "customer.announcements", "customer", "公告", true, false, "customer.announcements"],
  ["/admin", "admin.overview", "admin", "运维概览", true, false, "admin.overview"],
  ["/admin/overview", "admin.overview", "admin", "运维概览", true, false, "admin.overview"],
  ["/admin/accounts", "admin.accounts", "admin", "客户与计费账户", true, false, "admin.accounts"],
  ["/admin/billing", "admin.billing", "admin", "计费复核", true, false, "admin.billing"],
  ["/admin/resources", "admin.resources", "admin", "资源状态", true, false, "admin.resources"],
  ["/admin/system", "admin.system", "admin", "系统状态", true, false, "admin.system"],
  ["/admin/announcements", "admin.announcements", "admin", "公告管理", true, false, "admin.announcements"]
] as const;

test("Console route owner describes every static product route", () => {
  for (const [path, kind, surface, title, requiresSession, sensitive, navigationId] of staticConsoleRoutes) {
    const route = parseConsoleRoute(path);
    assert.ok(route, `missing route: ${path}`);
    assert.deepEqual({
      kind: route.kind,
      path: route.path,
      surface: route.surface,
      title: route.title,
      requiresSession: route.requiresSession,
      sensitive: route.sensitive,
      navigationId: route.navigationId
    }, { kind, path, surface, title, requiresSession, sensitive, navigationId });
    assert.equal(isKnownConsoleRoute(path), true);
  }
});

test("Console route owner parses one decoded Workspace detail segment", () => {
  assert.deepEqual(parseConsoleRoute("/console/workspaces/ws%20alpha"), {
    kind: "customer.workspace-detail",
    path: "/console/workspaces/ws%20alpha",
    surface: "customer",
    title: "Workspace",
    requiresSession: true,
    sensitive: true,
    navigationId: "customer.workspaces",
    workspaceId: "ws alpha"
  });
});

test("Console route owner normalizes aliases and trailing slashes", () => {
  assert.equal(parseConsoleRoute("/console/")?.path, "/console");
  assert.equal(parseConsoleRoute("/admin/announcements///")?.path, "/admin/announcements");
  assert.deepEqual(parseConsoleRoute("/console/gateway"), parseConsoleRoute("/console/api"));
  assert.deepEqual(parseConsoleRoute("/console/gateway/keys/"), parseConsoleRoute("/console/api/keys"));
});

test("Console entry aliases share all metadata with their canonical overview routes", () => {
  for (const [aliasPath, canonicalPath] of [
    ["/console", "/console/overview"],
    ["/admin", "/admin/overview"]
  ] as const) {
    const aliasRoute = parseConsoleRoute(aliasPath);
    const canonicalRoute = parseConsoleRoute(canonicalPath);
    assert.ok(aliasRoute);
    assert.ok(canonicalRoute);
    const { path: normalizedAliasPath, ...aliasMetadata } = aliasRoute;
    const { path: normalizedCanonicalPath, ...canonicalMetadata } = canonicalRoute;
    assert.equal(normalizedAliasPath, aliasPath);
    assert.equal(normalizedCanonicalPath, canonicalPath);
    assert.deepEqual(aliasMetadata, canonicalMetadata);
  }
});

test("Console route owner rejects unknown or malformed paths without guessing", () => {
  for (const path of [
    "/console/unknown",
    "/console/api/unknown",
    "/console/workspaces/ws-alpha/extra",
    "/console/workspaces/%E0%A4%A",
    "/console/workspaces/ws%2Falpha",
    "/admin/unknown"
  ]) {
    assert.equal(parseConsoleRoute(path), null, path);
    assert.equal(isKnownConsoleRoute(path), false, path);
  }
  assert.equal(parseConsoleRoute("/console/workspaces/new")?.kind, "customer.workspace-new");
  assert.equal("workspaceId" in (parseConsoleRoute("/console/workspaces/new") ?? {}), false);
});

test("Console navigation targets carry route-owner identities", () => {
  for (const item of [...customerMenu, ...adminMenu]) {
    assert.equal(parseConsoleRoute(item.path)?.navigationId, item.id, item.path);
  }
  for (const item of apiMenu) {
    assert.equal(parseConsoleRoute(item.path)?.kind, item.kind, item.path);
  }
});

test("Workspace status never invents a running state", () => {
  assert.equal(workspaceStatusLabel({ status: "running", ready: true }), "运行中");
  assert.equal(workspaceStatusLabel({ status: "unready", ready: false }), "暂不可用");
  assert.equal(workspaceStatusLabel({}), "暂不可用");
});

test("Workspace launch accepts balance equal to or greater than the server quote", async () => {
  assert.equal(hasSufficientWorkspaceLaunchBalance("52579999", 52_580_000), false);
  assert.equal(hasSufficientWorkspaceLaunchBalance("52580000", 52_580_000), true);
  assert.equal(hasSufficientWorkspaceLaunchBalance("52580001", 52_580_000), true);
});

test("unavailable and zero are distinct source facts", () => {
  assert.equal(formatAvailableBalance({ available: false, status: "unavailable" }), "暂不可用");
  assert.equal(formatAvailableBalance({ available: true, status: "available", usdMicros: "0" }), "$0.00");
  assert.equal(formatUsdMicros("9223372036854775807"), "$9,223,372,036,854.78");
  assert.equal(formatUsdMicros("-9223372036854775808"), "-$9,223,372,036,854.78");
  assert.equal(formatCount(undefined), "-");
  assert.equal(formatUsdMicros(undefined), "-");
  assert.deepEqual(readinessRows(null, null), [
    { label: "运行依赖", status: "暂不可用", updatedAt: "-" },
    { label: "生产依赖", status: "暂不可用", updatedAt: "-" }
  ]);
});
