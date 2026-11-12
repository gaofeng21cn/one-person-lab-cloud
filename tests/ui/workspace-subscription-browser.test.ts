import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "vite";
import react from "@vitejs/plugin-react";
import type { Browser, Page } from "playwright";

import { getWorkspaceSubscription } from "../../apps/console-ui/src/api/workspace-subscription-api.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

function subscription(workspaceId = "workspace-one", overrides: Record<string, unknown> = {}) {
  return {
    id: `sub-${workspaceId}`, workspaceId, currentPeriodStart: "2026-11-01T00:00:00Z", currentPeriodEnd: "2026-12-01T00:00:00Z",
    periodMonths: 1, status: "active", createdAt: "2026-11-01T00:00:00Z", provenance: "quoted", acceptedQuoteId: "quote-original",
    refundPolicyVersionId: "refund-original", retentionPolicyVersionId: "retention-original", refundTerms: "original", retentionTerms: "original",
    renewalMode: "manual", renewalSettingsVersion: "1", version: "1", currentPricePolicyVersionId: "accepted-policy-original", currentMonthlyUSDMicros: "52580001",
    ...overrides
  };
}

function legacySubscription(workspaceId = "workspace-one", overrides: Record<string, unknown> = {}) {
  return subscription(workspaceId, {
    provenance: "legacy_import", legacyPurchaseId: "legacy-original", acceptedQuoteId: undefined,
    currentMonthlyUSDMicros: undefined, currentPricePolicyVersionId: undefined, ...overrides
  });
}

test("subscription API preserves absent and exact accepted prices and rejects foreign/malformed financial readback", async () => {
  const original = globalThis.fetch;
  const calls: Array<{ path: string; signal?: AbortSignal }> = [];
  let reply: unknown = subscription();
  globalThis.fetch = async (input, init) => {
    calls.push({ path: String(input), signal: init?.signal ?? undefined });
    return Response.json(reply);
  };
  try {
    const abort = new AbortController();
    assert.equal((await getWorkspaceSubscription("workspace-one", abort.signal)).currentMonthlyUSDMicros, "52580001");
    abort.abort();
    assert.equal(calls[0].signal?.aborted, true, "caller abort must reach the HTTP request");
    reply = subscription("workspace /one", { currentMonthlyUSDMicros: "9223372036854775807" });
    assert.equal((await getWorkspaceSubscription("workspace /one")).currentMonthlyUSDMicros, "9223372036854775807");
    assert.equal(calls.at(-1)?.path, "/api/v2/workspaces/workspace%20%2Fone/subscription");
    reply = legacySubscription();
    assert.equal((await getWorkspaceSubscription("workspace-one")).currentMonthlyUSDMicros, undefined);
    for (const status of ["pending", "active", "expired", "terminated"]) {
      reply = subscription("workspace-one", { status });
      assert.equal((await getWorkspaceSubscription("workspace-one")).status, status);
    }
    reply = legacySubscription("workspace-one", { currentMonthlyUSDMicros: "0", currentPricePolicyVersionId: "accepted-free-policy" });
    assert.equal((await getWorkspaceSubscription("workspace-one")).currentMonthlyUSDMicros, "0");
    for (const price of [0, 52580001, null, "", "-1", "01", "1.5", "9223372036854775808"]) {
      reply = subscription("workspace-one", { currentMonthlyUSDMicros: price });
      await assert.rejects(getWorkspaceSubscription("workspace-one"), /invalid_workspace_subscription/);
    }
    for (reply of [null, [], subscription("foreign"), subscription("workspace-one", { currentPeriodStart: "unknown" }), subscription("workspace-one", { currentPricePolicyVersionId: 7 }), subscription("workspace-one", { renewalMode: undefined })]) {
      await assert.rejects(getWorkspaceSubscription("workspace-one"), /invalid_workspace_subscription/);
    }
    for (const invalid of [
      { currentPeriodEnd: "2026-11-01T00:00:00Z" }, { currentPeriodEnd: "2026-10-01T00:00:00Z" },
      { status: "future_status" }, { provenance: "future_source" }, { renewalMode: "toString" },
      { currentMonthlyUSDMicros: undefined }, { currentPricePolicyVersionId: undefined },
      { currentMonthlyUSDMicros: undefined, currentPricePolicyVersionId: undefined },
      { legacyPurchaseId: "cannot-also-be-legacy" }, { renewalMode: "automatic" },
      { periodMonths: 0 }, { periodMonths: 1.5 }, { createdAt: "not-a-date" },
      { version: "-1" }, { renewalSettingsVersion: 1 }
    ]) {
      reply = subscription("workspace-one", invalid);
      await assert.rejects(getWorkspaceSubscription("workspace-one"), /invalid_workspace_subscription/);
    }
    for (const field of ["acceptedQuoteId", "refundPolicyVersionId", "retentionPolicyVersionId", "refundTerms", "retentionTerms"]) {
      reply = subscription("workspace-one", { [field]: undefined });
      await assert.rejects(getWorkspaceSubscription("workspace-one"), /invalid_workspace_subscription/);
      reply = subscription("workspace-one", { [field]: "" });
      await assert.rejects(getWorkspaceSubscription("workspace-one"), /invalid_workspace_subscription/);
    }
    for (const invalid of [{ currentMonthlyUSDMicros: "1" }, { currentPricePolicyVersionId: "one-sided-policy" }, { legacyPurchaseId: undefined }, { acceptedQuoteId: "foreign-quote" }]) {
      reply = legacySubscription("workspace-one", invalid);
      await assert.rejects(getWorkspaceSubscription("workspace-one"), /invalid_workspace_subscription/);
    }
  } finally { globalThis.fetch = original; }
});

async function startFixture(t: { after: (fn: () => Promise<unknown>) => void }, initialSurface = "detail") {
  const cacheDir = await mkdtemp(join(tmpdir(), "opl-subscription-vite-"));
  const server = await createServer({
    configFile: false,
    cacheDir,
    define: { "import.meta.env.VITE_CONSOLE_IDENTITY": JSON.stringify("cloud") },
    plugins: [react(), {
      name: "native-subscription-consumers",
      configureServer(vite) {
        vite.middlewares.use(async (request, response, next) => {
          if (request.url !== "/__subscription") return next();
          response.setHeader("Content-Type", "text/html");
          response.end(await vite.transformIndexHtml(request.url, `<!doctype html><html lang="zh"><body><div id="root"></div><script type="module">
            import "/apps/console-ui/src/components/ui/apps-sdk.css";
            import "/apps/console-ui/src/components/ui/tokens.css";
            import "/apps/console-ui/src/components/ui/components.css";
            import "/apps/console-ui/src/styles.css";
            import "/apps/console-ui/src/components/ui/sub2api-style.css";
            import React from "react";
            import { createRoot } from "react-dom/client";
            import { flushSync } from "react-dom";
            import { currentSession } from "/apps/console-ui/src/api/auth-api.ts";
            import { CustomerPages } from "/apps/console-ui/src/pages/CustomerPages.tsx";
            import { WorkspaceDetailPage } from "/apps/console-ui/src/components/workspaces/WorkspaceDetailPage.tsx";
            import { WorkspaceSubscriptionPanel } from "/apps/console-ui/src/components/workspaces/WorkspaceSubscriptionPanel.tsx";
            const root = createRoot(document.getElementById("root"));
            const base = await currentSession();
            const source = data => ({ source: "workspace", status: "available", available: true, fetchedAt: "2026-11-12T00:00:00Z", data });
            const workspace = id => ({ id, name: id, state: "active", url: "https://workspace.invalid", computePlanId: "compute-original", storagePlanId: "storage-original", packageId: "pro", totalUsdMicros: 999000000, periodStart: "2001-01-01T00:00:00Z", paidThrough: "2001-02-01T00:00:00Z", autoRenew: true });
            window.mountSubscription = ({ surface = "detail", identity = {}, ids = ["workspace-one"], view = "terms", planIds = {} } = {}) => {
              const session = identity === null ? null : { ...base, ...identity, user: { ...base.user, ...identity.user } };
              const items = ids.map(id => ({ ...workspace(id), ...planIds }));
              const controller = {
                session, navigate: path => { document.body.dataset.navigation = path; }, refreshCurrentPage: async () => {},
                customerWorkspaceRead: { detail: { value: source(items[0]) }, workspaces: { value: source({ items }) }, page: 1, pages: 1, refresh: async () => {} },
                billing: { view, setView: value => window.mountSubscription({ surface: "billing", view: value, ids }),
                  receipts: { value: { ...source({ receipts: [{ receiptId: "old-receipt", type: "billing.workspace_purchased.v1", status: "completed", workspaceId: "workspace-one", createdAt: "2026-10-01T00:00:00Z", totalUsdMicros: 12340000 }] }), source: "ledger" } }, detail: { value: null } }
              };
              flushSync(() => root.render(surface === "panel" ? React.createElement(WorkspaceSubscriptionPanel, { session, workspace: items[0] }) : surface === "billing" ? React.createElement(CustomerPages, { controller, route: { kind: "customer.billing" } }) : React.createElement(WorkspaceDetailPage, { controller })));
            };
            window.leaveSubscription = () => flushSync(() => root.render(React.createElement("p", null, "left")));
            window.mountSubscription(${JSON.stringify({ surface: initialSurface })});
          </script></body></html>`));
        });
      }
    }], server: { host: "127.0.0.1", port: 0, hmr: false, watch: null }
  });
  await server.listen();
  let browser: Browser | undefined;
  t.after(async () => { await browser?.close(); await server.close(); await rm(cacheDir, { recursive: true, force: true }); });
  browser = await launchBrowser({ headless: true });
  const page = await browser.newPage();
  page.setDefaultTimeout(10_000);
  const errors: string[] = [];
  const catalogRequests: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/v2/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path === "/api/v2/catalog/compute-plans" || path === "/api/v2/catalog/storage-plans") {
      catalogRequests.push(`${path}${url.search}`);
      assert.equal(route.request().method(), "GET");
      assert.deepEqual([...url.searchParams.keys()].sort(), url.searchParams.has("cursor") ? ["cursor", "limit"] : ["limit"], "subscription plan labels must read unfiltered directories");
      if (path.endsWith("compute-plans")) return route.fulfill({ json: url.searchParams.has("cursor")
        ? { items: [{ id: "compute-original", name: "原计算方案", vcpus: 2, memoryMiB: 4096, availability: "retired", billingMode: "prepaid_monthly", monthlyPriceUSDMicros: "888000000", pricePolicyVersionId: "policy-today" }] }
        : { items: [{ id: "compute-other", name: "今日热销方案", vcpus: 8, memoryMiB: 16384, availability: "available", billingMode: "prepaid_monthly" }], nextCursor: "compute-history-page" } });
      return route.fulfill({ json: url.searchParams.has("cursor")
        ? { items: [{ id: "storage-original", name: "原数据盘", capacityGiB: 100, availability: "retired", billingMode: "prepaid_monthly", monthlyPriceUSDMicros: "777000000", pricePolicyVersionId: "storage-policy-today" }] }
        : { items: [{ id: "storage-other", name: "今日热销数据盘", capacityGiB: 2048, availability: "available", billingMode: "prepaid_monthly" }], nextCursor: "storage-history-page" } });
    }
    if (path.endsWith("/subscription")) return route.fallback();
    if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "actor-original", tenantId: "tenant-original", displayName: "Owner", role: "owner", csrfToken: "csrf-original", permissions: [], expiresAt: "2099-01-01T00:00:00Z" } });
    if (path.endsWith("/transactions")) return route.fulfill({ json: { items: [] } });
    if (path.startsWith("/api/v2/delivery/")) return route.fulfill({ json: { workspaceId: path.split("/").at(-1), ...Object.fromEntries(["workspace", "serve", "capabilityVersion", "build"].map((owner) => [owner, { owner, state: "ready", details: {} }])) } });
    return route.fulfill({ status: 503, json: { error: "owner_unavailable" } });
  });
  return { page, origin: server.resolvedUrls!.local[0], errors, catalogRequests };
}

function panel(page: Page) { return page.getByRole("region", { name: "工作空间订阅", exact: true }); }
async function mount(page: Page, options: unknown) {
  await page.evaluate((value) => (window as unknown as { mountSubscription: (options: unknown) => void }).mountSubscription(value), options);
}

test("real detail and billing consumers read native subscriptions independently and preserve historical receipt amounts", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors, catalogRequests } = await startFixture(t);
  const reads: string[] = [];
  let onePrice: string | undefined = "52580001";
  await page.route("**/api/v2/workspaces/*/subscription", async (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-2)!;
    reads.push(id);
    assert.equal(route.request().method(), "GET");
    if (id === "workspace-none") return route.fulfill({ status: 404, json: { error: "not_found" } });
    if (id === "workspace-denied") return route.fulfill({ status: 403, json: { error: "forbidden" } });
    return route.fulfill({ json: id === "workspace-one" && onePrice === undefined ? legacySubscription(id) : subscription(id, id === "workspace-one" ? { currentMonthlyUSDMicros: onePrice } : { currentMonthlyUSDMicros: "0", renewalMode: "automatic", renewalConsentId: "consent-original" }) });
  });
  await page.goto(`${origin}__subscription`);
  await panel(page).getByText("$52.580001", { exact: true }).waitFor();
  await panel(page).getByText("手动续费", { exact: true }).waitFor();
  await panel(page).getByText("原计算方案 · 2 核 / 4 GiB 内存", { exact: true }).waitFor();
  await panel(page).getByText("原数据盘 · 100 GiB", { exact: true }).waitFor();
  assert.equal(catalogRequests.length, 4, "detail reads each directory once, including its history page");
  for (const raw of ["compute-original", "storage-original", "accepted-policy-original", "quoted"]) {
    assert.equal(await panel(page).getByText(raw, { exact: true }).isVisible(), false);
  }
  await panel(page).getByText("订阅技术详情", { exact: true }).click();
  for (const raw of ["compute-original", "storage-original", "accepted-policy-original", "quoted"]) {
    assert.equal(await panel(page).getByText(raw, { exact: true }).isVisible(), true);
  }
  await panel(page).getByText("订阅技术详情", { exact: true }).click();
  assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isEnabled(), true);
  await mount(page, { surface: "billing", ids: ["workspace-one", "workspace-zero", "workspace-none", "workspace-denied"] });
  await panel(page).getByText("$52.580001", { exact: true }).waitFor();
  await panel(page).getByText("$0.00", { exact: true }).waitFor();
  await panel(page).getByText("自动续费", { exact: true }).waitFor();
  await panel(page).getByText("暂无订阅记录。", { exact: true }).waitFor();
  await panel(page).getByText("原数据盘 · 100 GiB", { exact: true }).first().waitFor();
  await panel(page).getByText("原计算方案 · 2 核 / 4 GiB 内存", { exact: true }).first().waitFor();
  assert.equal(catalogRequests.length, 8, "all four Billing rows must share one paginated read of each Catalog");
  assert.equal(await page.getByText("$888.00", { exact: true }).count(), 0, "Catalog's current compute price cannot replace accepted subscription price");
  assert.equal(await page.getByText("$777.00", { exact: true }).count(), 0);
  assert.equal(await panel(page).getByText("compute-original", { exact: true }).first().isVisible(), false);
  await panel(page).getByText("仅租户所有者或管理员可查看订阅与费用。", { exact: true }).waitFor();
  assert.equal(reads.filter((id) => id === "workspace-one").length, 2, "billing must request once per row, without duplicate desktop/mobile requests");
  assert.equal(await page.getByText("$999.00", { exact: true }).count(), 0, "native billing cannot use the retired Workspace price projection");
  assert.equal(await page.getByText("PRO", { exact: true }).count(), 0);
  await page.getByRole("link", { name: "workspace-zero", exact: true }).click();
  assert.equal(await page.locator("body").getAttribute("data-navigation"), "/console/workspaces/workspace-zero");
  await page.getByRole("radio", { name: "个人历史账单", exact: true }).click();
  await page.getByText("扣款 $12.34", { exact: true }).first().waitFor();
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await mount(page, { surface: "billing", ids: ["workspace-one", "workspace-zero"] });
    await panel(page).getByText("$52.580001", { exact: true }).waitFor();
    await panel(page).getByText("$0.00", { exact: true }).waitFor();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `billing must fit ${width}px`);
    const bounds = await panel(page).first().evaluate((element) => ({
      factsBottom: element.querySelector("dl")!.getBoundingClientRect().bottom,
      refreshTop: element.querySelector("button")!.getBoundingClientRect().top
    }));
    assert.ok(bounds.refreshTop >= bounds.factsBottom, "refresh must not overlap subscription facts");
    const rows = await page.getByRole("listitem").evaluateAll((elements) => elements.map((element) => {
      const style = getComputedStyle(element);
      const amount = [...element.querySelectorAll("dt")].find((label) => label.textContent === "月度总价")!.nextElementSibling!;
      return { radius: style.borderRadius, shadow: style.boxShadow, divider: style.borderTopWidth, amountX: amount.getBoundingClientRect().x, technicalOpen: element.querySelector("details")!.open };
    }));
    assert.equal(rows.length, 2);
    assert.ok(rows.every((row) => row.radius === "0px" && row.shadow === "none"), "subscription entries must render as plain rows");
    assert.equal(rows[1].divider, "1px", "neighboring entries retain a visible separator");
    assert.equal(rows[0].amountX, rows[1].amountX, "amounts retain comparable columns across Workspace rows");
    assert.ok(rows.every((row) => !row.technicalOpen), "secondary technical facts stay collapsed");
  }
  onePrice = undefined;
  await mount(page, { surface: "detail" });
  await panel(page).locator(":scope > dl").getByText("待核对", { exact: true }).waitFor();
  assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isEnabled(), true, "unknown accepted price must not block resource access");
  assert.ok(catalogRequests.every((path) => path.includes("/compute-plans?") || path.includes("/storage-plans?")));
  assert.deepEqual(errors, []);
});

test("subscription unknown values, 404, 403, 401 and failure stay distinct and retry uses owner readback", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors } = await startFixture(t);
  let status = 200;
  let value: unknown = subscription("workspace-one", { status: "future_status", renewalMode: "toString", provenance: "future_source", currentMonthlyUSDMicros: undefined, currentPricePolicyVersionId: undefined });
  await page.route("**/api/v2/workspaces/*/subscription", (route) => route.fulfill({ status, json: value }));
  await page.goto(`${origin}__subscription`);
  await panel(page).getByText("订阅暂不可用，请重试。", { exact: true }).waitFor();
  assert.equal(await panel(page).locator("dl").count(), 0, "unrecognized enums must never become an available subscription");
  assert.equal(await panel(page).getByText("自动续费", { exact: true }).count(), 0);
  const reload = async (code: number, message: string) => {
    status = code;
    value = { error: "owner_read_rejected" };
    await page.getByRole("button", { name: "刷新订阅", exact: true }).click();
    await panel(page).getByText(message, { exact: true }).waitFor();
    assert.equal(await panel(page).locator("dl").count(), 0);
    assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isEnabled(), true);
  };
  await reload(404, "暂无订阅记录。");
  await reload(503, "订阅暂不可用，请重试。");
  status = 200;
  value = subscription("workspace-one", { currentMonthlyUSDMicros: "9223372036854775807" });
  await page.getByRole("button", { name: "重试读取订阅", exact: true }).click();
  await panel(page).getByText("$9,223,372,036,854.775807", { exact: true }).waitFor();
  await reload(403, "仅租户所有者或管理员可查看订阅与费用。");
  await mount(page, { surface: "panel", identity: { csrfToken: "csrf-next" } });
  await panel(page).getByText("仅租户所有者或管理员可查看订阅与费用。", { exact: true }).waitFor();
  status = 401;
  await mount(page, { surface: "panel", identity: { csrfToken: "csrf-new-login" } });
  await panel(page).getByText("请先登录后查看订阅。", { exact: true }).waitFor();
  assert.equal(await page.getByRole("button", { name: "刷新订阅", exact: true }).count(), 0);
  assert.deepEqual(errors, []);
});

test("tenant role, actor, tenant, login and Workspace changes abort and hide old identity facts before late replies", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors } = await startFixture(t);
  let count = 0;
  let hold = false;
  let release: (() => void) | undefined;
  let pending: Promise<void> | undefined;
  await page.route("**/api/v2/workspaces/*/subscription", async (route) => {
    count++;
    const id = new URL(route.request().url()).pathname.split("/").at(-2)!;
    const amount = String(10_000_000 + count * 1_000_000);
    if (hold) await pending;
    await route.fulfill({ json: subscription(id, { currentMonthlyUSDMicros: amount }) }).catch(() => {});
  });
  await page.goto(`${origin}__subscription`);
  await panel(page).getByText("$11.00", { exact: true }).waitFor();
  // Observe the real combined HTTP signal without replacing fetch or the API.
  await page.evaluate(() => {
    const actualFetch = window.fetch;
    (window as unknown as { subscriptionSignals: AbortSignal[] }).subscriptionSignals = [];
    window.fetch = (input, init) => {
      if (String(input).endsWith("/subscription")) (window as unknown as { subscriptionSignals: AbortSignal[] }).subscriptionSignals.push(init!.signal!);
      return actualFetch(input, init);
    };
  });
  for (const identity of [
    { user: { role: "member" } },
    { user: { role: "member" }, isOperator: true },
    { user: { role: "" }, isOperator: true },
    { user: { accountId: "" }, isOperator: true },
    { user: { status: "disabled" } }
  ]) {
    await mount(page, { surface: "panel", identity });
    await panel(page).getByText("仅租户所有者或管理员可查看订阅与费用。", { exact: true }).waitFor();
    assert.equal(await panel(page).getByText("$11.00", { exact: true }).count(), 0);
    assert.equal(count, 1, "member/platform-only/disabled identities must not request tenant financial facts");
  }
  for (const [index, identity] of [
    { user: { id: "actor-next" } },
    { user: { accountId: "tenant-next" } },
    { user: { role: "admin" } },
    { csrfToken: "csrf-next" },
    { expiresAt: "2099-02-01T00:00:00Z" }
  ].entries()) {
    hold = true;
    pending = new Promise<void>((resolve) => { release = resolve; });
    const requested = page.waitForRequest("**/subscription");
    await mount(page, { surface: "panel" });
    await requested;
    const newest = page.waitForRequest("**/subscription");
    await mount(page, { surface: "panel", identity });
    assert.equal(await panel(page).locator("dl").count(), 0, "identity switch must synchronously hide the previous financial read");
    await newest;
    assert.equal(await page.evaluate(() => (window as unknown as { subscriptionSignals: AbortSignal[] }).subscriptionSignals.at(-2)!.aborted), true);
    hold = false;
    release!();
    await panel(page).getByText(`$${13 + index * 2}.00`, { exact: true }).waitFor();
  }
  hold = true;
  pending = new Promise<void>((resolve) => { release = resolve; });
  const requested = page.waitForRequest("**/subscription");
  await mount(page, { surface: "panel" });
  await requested;
  await mount(page, { surface: "panel", identity: null });
  await panel(page).getByText("请先登录后查看订阅。", { exact: true }).waitFor();
  assert.equal(await page.evaluate(() => (window as unknown as { subscriptionSignals: AbortSignal[] }).subscriptionSignals.at(-1)!.aborted), true);
  hold = false;
  release!();
  await mount(page, { surface: "panel", ids: ["workspace-other"] });
  await panel(page).getByText("$23.00", { exact: true }).waitFor();
  hold = true;
  pending = new Promise<void>((resolve) => { release = resolve; });
  const oldWorkspace = page.waitForRequest("**/subscription");
  await mount(page, { surface: "panel", ids: ["workspace-other"] });
  await oldWorkspace;
  const nextWorkspace = page.waitForRequest("**/subscription");
  await mount(page, { surface: "panel", ids: ["workspace-next"] });
  await nextWorkspace;
  assert.equal(await panel(page).locator("dl").count(), 0);
  assert.equal(await page.evaluate(() => (window as unknown as { subscriptionSignals: AbortSignal[] }).subscriptionSignals.at(-2)!.aborted), true);
  hold = false;
  release!();
  await panel(page).getByText("$25.00", { exact: true }).waitFor();
  hold = true;
  pending = new Promise<void>((resolve) => { release = resolve; });
  const departed = page.waitForRequest("**/subscription");
  await mount(page, { surface: "panel" });
  await departed;
  await page.evaluate("window.leaveSubscription()");
  assert.equal(await page.evaluate(() => (window as unknown as { subscriptionSignals: AbortSignal[] }).subscriptionSignals.at(-1)!.aborted), true);
  hold = false;
  release!();
  assert.equal(await panel(page).count(), 0);
  assert.deepEqual(errors, []);
});


test("Catalog failure, latency and missing exact plans preserve subscription amount, period and access", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors } = await startFixture(t);
  let subscriptionReads = 0;
  let compute: "pending" | "ready" | "failed" | "missing" = "pending";
  let storage: "failed" | "ready" = "failed";
  const held = Promise.withResolvers<void>();
  await page.route("**/api/v2/workspaces/*/subscription", (route) => {
    subscriptionReads++;
    return route.fulfill({ json: subscription() });
  });
  await page.route("**/api/v2/catalog/*-plans**", async (route) => {
    if (new URL(route.request().url()).pathname.endsWith("compute-plans")) {
      if (compute === "pending") await held.promise;
      if (compute === "failed") return route.fulfill({ status: 503, json: { error: "catalog_unavailable" } });
      return route.fulfill({ json: { items: [{ id: compute === "missing" ? "compute-original-other" : "compute-original", name: compute === "missing" ? "不能猜测的相似方案" : "历史计算方案", vcpus: 2, memoryMiB: 4096, availability: "retired", billingMode: "prepaid_monthly", monthlyPriceUSDMicros: "888000000" }] } });
    }
    return storage === "failed" ? route.fulfill({ status: 503, json: { error: "catalog_unavailable" } }) : route.fulfill({ json: { items: [{ id: "storage-original", name: "历史数据盘", capacityGiB: 100, availability: "retired", billingMode: "prepaid_monthly" }] } });
  });
  await page.goto(`${origin}__subscription`);
  const facts = panel(page).locator(":scope > dl");
  await facts.getByText("$52.580001", { exact: true }).waitFor();
  await facts.getByText("套餐信息暂不可用", { exact: true }).waitFor();
  assert.equal(await facts.getByText("正在读取套餐信息…", { exact: true }).count(), 1);
  assert.equal(await facts.locator("dt").filter({ hasText: "计费周期" }).locator("..").locator("dd").textContent().then((text) => text!.includes("2026")), true);
  assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isEnabled(), true);
  compute = "ready";
  held.resolve();
  await facts.getByText("历史计算方案 · 2 核 / 4 GiB 内存", { exact: true }).waitFor();
  compute = "failed";
  storage = "ready";
  await panel(page).getByRole("button", { name: "重试套餐信息", exact: true }).click();
  await facts.getByText("历史数据盘 · 100 GiB", { exact: true }).waitFor();
  await facts.getByText("套餐信息暂不可用", { exact: true }).waitFor();
  assert.equal(await facts.getByText("$52.580001", { exact: true }).count(), 1);
  assert.equal(subscriptionReads, 1, "Catalog retry cannot reload or replace financial facts");
  compute = "missing";
  await panel(page).getByRole("button", { name: "重试套餐信息", exact: true }).click();
  await facts.getByText("历史数据盘 · 100 GiB", { exact: true }).waitFor();
  await facts.getByText("套餐信息暂不可用", { exact: true }).waitFor();
  assert.equal(await facts.getByText("不能猜测的相似方案", { exact: false }).count(), 0);
  assert.equal(await facts.getByText("$52.580001", { exact: true }).count(), 1);
  compute = "failed";
  storage = "failed";
  await mount(page, { surface: "detail", identity: { csrfToken: "csrf-catalog-failure" } });
  await facts.getByText("$52.580001", { exact: true }).waitFor();
  await page.waitForFunction(() => [...document.querySelectorAll('.workspace-subscription-panel > dl dd')].filter((node) => node.textContent === "套餐信息暂不可用").length === 2);
  assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isEnabled(), true);
  assert.equal(await panel(page).getByText("compute-original", { exact: true }).isVisible(), false);
  await mount(page, { surface: "detail", planIds: { computePlanId: undefined, storagePlanId: undefined } });
  await facts.getByText("$52.580001", { exact: true }).waitFor();
  assert.equal(await facts.getByText("套餐信息暂不可用", { exact: true }).count(), 2);
  assert.deepEqual(errors, []);
});

test("shared Catalog reads reject old tenant replies and never multiply with Billing rows", { timeout: 60_000 }, async (t) => {
  const { page, origin, errors } = await startFixture(t, "billing");
  const old = Promise.withResolvers<void>();
  const started = Promise.withResolvers<void>();
  let currentIdentity = false;
  const counts = { compute: 0, storage: 0 };
  await page.route("**/api/v2/workspaces/*/subscription", (route) => route.fulfill({ json: subscription(new URL(route.request().url()).pathname.split("/").at(-2)) }));
  await page.route("**/api/v2/catalog/*-plans**", async (route) => {
    const isCompute = new URL(route.request().url()).pathname.endsWith("compute-plans");
    const fresh = currentIdentity;
    counts[isCompute ? "compute" : "storage"]++;
    if (counts.compute === 1 && counts.storage === 1) started.resolve();
    if (!fresh) await old.promise;
    const name = `${fresh ? "当前身份" : "旧身份"}${isCompute ? "计算方案" : "数据盘"}`;
    await route.fulfill({ json: { items: [isCompute
      ? { id: "compute-original", name, vcpus: 2, memoryMiB: 4096, availability: "retired", billingMode: "prepaid_monthly" }
      : { id: "storage-original", name, capacityGiB: 100, availability: "retired", billingMode: "prepaid_monthly" }] } }).catch(() => {});
  });
  await page.goto(`${origin}__subscription`);
  await panel(page).getByText("$52.580001", { exact: true }).waitFor();
  await page.waitForFunction(() => [...document.querySelectorAll('.workspace-subscription-panel > dl dd')].filter((node) => node.textContent === "正在读取套餐信息…").length === 2);
  await started.promise;
  assert.deepEqual(counts, { compute: 1, storage: 1 });
  currentIdentity = true;
  await mount(page, { surface: "billing", ids: ["workspace-one", "workspace-two"], identity: { user: { accountId: "tenant-next" } } });
  await panel(page).getByText("当前身份计算方案 · 2 核 / 4 GiB 内存", { exact: true }).first().waitFor();
  await panel(page).getByText("当前身份数据盘 · 100 GiB", { exact: true }).first().waitFor();
  assert.deepEqual(counts, { compute: 2, storage: 2 }, "two Billing rows share the new tenant's directories");
  const departedResponses = ["compute-plans", "storage-plans"].map((suffix) => page.waitForResponse(async (response) => {
    if (!new URL(response.url()).pathname.endsWith(suffix)) return false;
    return (await response.json()).items[0].name.startsWith("旧身份");
  }));
  old.resolve();
  await Promise.all(departedResponses);
  await page.evaluate(() => new Promise(requestAnimationFrame));
  assert.equal(await page.getByText("旧身份", { exact: false }).count(), 0);
  assert.equal(await panel(page).getByText("当前身份计算方案 · 2 核 / 4 GiB 内存", { exact: true }).count(), 2);
  await mount(page, { surface: "billing", identity: { user: { role: "member" }, isOperator: true }, ids: ["workspace-one", "workspace-two"] });
  await panel(page).getByText("仅租户所有者或管理员可查看订阅与费用。", { exact: true }).first().waitFor();
  assert.deepEqual(counts, { compute: 2, storage: 2 });
  assert.equal(await page.getByText("当前身份计算方案", { exact: false }).count(), 0);
  assert.deepEqual(errors, []);
});
