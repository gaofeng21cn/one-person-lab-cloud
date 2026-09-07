import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import test from "node:test";

import { chromium, type Page, type Response, type Route } from "playwright";

import type {
  GatewayKeyPageDTO,
  GatewayKeySummaryDTO,
  GatewayKeyUsagePageDTO,
  GatewayUsagePeriod,
  GatewayUsageSummaryDTO,
  SourceEnvelope,
  SourceValueStatus
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function source<T>(data: T, status: SourceValueStatus = "available"): SourceEnvelope<T> {
  return {
    source: "sub2api",
    status,
    available: true,
    fetchedAt: "2026-08-26T00:00:00Z",
    data
  };
}

const key: GatewayKeySummaryDTO = {
  id: "9",
  name: "Browser Test Key",
  groupId: null,
  status: "active",
  ipWhitelist: [],
  ipBlacklist: [],
  quotaUsdMicros: 1_000_000,
  quotaUsedUsdMicros: 34_000,
  rateLimit5hUsdMicros: 0,
  rateLimit1dUsdMicros: 0,
  rateLimit7dUsdMicros: 0,
  usage5hUsdMicros: 0,
  usage1dUsdMicros: 34_000,
  usage7dUsdMicros: 34_000,
  currentConcurrency: 0,
  lastUsedAt: "2026-08-26T00:00:00Z",
  lastUsedIp: null,
  expiresAt: null,
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-26T00:00:00Z",
  kind: "general",
  manageable: true,
  deletable: true
};

const secondKey: GatewayKeySummaryDTO = {
  ...key,
  id: "10",
  name: "Second Browser Test Key"
};

const twentyFirstKey: GatewayKeySummaryDTO = {
  ...key,
  id: "29",
  name: "Twenty First Browser Test Key",
  kind: "workspace"
};

const firstTwentyKeys: GatewayKeySummaryDTO[] = Array.from({ length: 20 }, (_, index) => ({
  ...key,
  id: String(index + 1),
  name: `Browser Test Key ${String(index + 1).padStart(2, "0")}`
}));

const populatedKeys: GatewayKeyPageDTO = {
  items: [key],
  total: 1,
  page: 1,
  pageSize: 20,
  pages: 1
};

const multipleKeys: GatewayKeyPageDTO = {
  ...populatedKeys,
  items: [key, secondKey],
  total: 2
};

const emptyKeys: GatewayKeyPageDTO = {
  items: [],
  total: 0,
  page: 1,
  pageSize: 20,
  pages: 0
};

function usagePage(
  period: GatewayUsagePeriod,
  selectedKey: GatewayKeySummaryDTO = key,
  requestId = `request-${period}`
): GatewayKeyUsagePageDTO {
  return {
    items: [{
      apiKeyId: selectedKey.id,
      requestId,
      createdAt: "2026-08-26T00:00:00Z",
      model: `model-${period}`,
      inboundEndpoint: "/v1/responses",
      requestType: "sync",
      inputTokens: 10,
      outputTokens: 2,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
      actualCostUsdMicros: 1_000,
      durationMs: 100,
      firstTokenMs: 20
    }],
    total: 1,
    page: 1,
    pageSize: 20,
    pages: 1
  };
}

function usageSummary(totalRequests: number): GatewayUsageSummaryDTO {
  return {
    totalRequests,
    totalInputTokens: totalRequests * 10,
    totalOutputTokens: totalRequests * 2,
    totalTokens: totalRequests * 12,
    totalActualCostUsdMicros: totalRequests * 1_000
  };
}

function requestedPeriod(route: Route): GatewayUsagePeriod {
  const period = new URL(route.request().url()).searchParams.get("period");
  assert.ok(period === "today" || period === "week" || period === "month");
  return period;
}

async function fulfill<T>(route: Route, envelope: SourceEnvelope<T>) {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(envelope)
  });
}

async function loginAndOpenUsage(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
  await page.goto(`${origin}/console/api/usage`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: "用量", exact: true }).waitFor({ state: "visible" });
}

async function routePopulatedKeys(page: Page) {
  await page.route("**/api/gateway/keys?page=1&pageSize=20", (route) => fulfill(route, source(populatedKeys)));
}

function waitForUsageResponse(page: Page, keyId: string, period: GatewayUsagePeriod, summary: boolean) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    const suffix = summary ? "/usage-summary" : "/usage";
    return url.pathname.endsWith(`/keys/${keyId}${suffix}`) && url.searchParams.get("period") === period;
  });
}

async function waitForResponsesToSettle(page: Page, responses: Promise<Response>[]) {
  const completed = await Promise.all(responses);
  await Promise.all(completed.map((response) => response.finished()));
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

function requestEntry(page: Page, requestId: string) {
  return page.locator(".request-table-desktop .usage-request-entry").filter({ hasText: requestId });
}

interface GatewayUsageProjectionProbe {
  requestIds: string[];
  selectedKeyId: string;
  summaryRequests: number | null;
}

async function readGatewayUsageProjection(page: Page): Promise<GatewayUsageProjectionProbe> {
  return page.locator("main").evaluate((root) => {
    type Fiber = {
      return?: Fiber | null;
      memoizedProps?: unknown;
    };
    const fiberName = Object.keys(root).find((name) => name.startsWith("__reactFiber$"));
    if (!fiberName) throw new Error("console_probe_react_fiber_missing");
    let fiber = (root as unknown as Record<string, unknown>)[fiberName] as Fiber | undefined;
    let controller: unknown;
    while (fiber) {
      const props = fiber.memoizedProps;
      if (props && typeof props === "object" && "controller" in props) {
        controller = (props as { controller?: unknown }).controller;
        break;
      }
      fiber = fiber.return || undefined;
    }
    if (!controller || typeof controller !== "object") throw new Error("console_probe_controller_invalid");
    const gatewayUsage = (controller as { gatewayUsage?: unknown }).gatewayUsage;
    if (!gatewayUsage || typeof gatewayUsage !== "object") throw new Error("console_probe_gateway_usage_invalid");
    const usage = gatewayUsage as {
      selectedKeyId?: unknown;
      summary?: { value?: SourceEnvelope<GatewayUsageSummaryDTO> | null };
      usage?: { value?: SourceEnvelope<GatewayKeyUsagePageDTO> | null };
    };
    return {
      requestIds: usage.usage?.value?.available ? usage.usage.value.data.items.map((item) => item.requestId) : [],
      selectedKeyId: typeof usage.selectedKeyId === "string" ? usage.selectedKeyId : "",
      summaryRequests: usage.summary?.value?.available ? usage.summary.value.data.totalRequests : null
    };
  });
}

test("Gateway usage rejects a late week result after today commits", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseWeek = deferred<void>();
  const weekRequestsHeld = deferred<void>();
  let heldWeekRequests = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      const period = requestedPeriod(route);
      const isSummary = url.pathname.endsWith("/usage-summary");
      if (period === "week") {
        heldWeekRequests += 1;
        if (heldWeekRequests === 2) weekRequestsHeld.resolve();
        await releaseWeek.promise;
      }
      await fulfill(route, isSummary
        ? source(usageSummary(period === "today" ? 7 : period === "week" ? 70 : 30))
        : source(usagePage(period)));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-month").waitFor({ state: "visible" });

    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await weekRequestsHeld.promise;
    assert.equal(await requestEntry(page, "request-month").count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").count(), 0);
    assert.equal(await page.locator(".gateway-usage-results .source-loading").count(), 2);
    await page.getByRole("radio", { name: "今日", exact: true }).click();

    await requestEntry(page, "request-today").waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("7", { exact: true }).waitFor({ state: "visible" });

    const lateWeekUsage = waitForUsageResponse(page, key.id, "week", false);
    const lateWeekSummary = waitForUsageResponse(page, key.id, "week", true);
    releaseWeek.resolve();
    await waitForResponsesToSettle(page, [lateWeekUsage, lateWeekSummary]);
    assert.equal(await requestEntry(page, "request-today").count(), 1);
    assert.equal(await requestEntry(page, "request-week").count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").getByText("7", { exact: true }).count(), 1);
    assert.equal(await page.locator(".usage-summary-strip").getByText("70", { exact: true }).count(), 0);
  } finally {
    releaseWeek.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage rejects late results for a previously selected key", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseFirstKey = deferred<void>();
  const firstKeyRequestsHeld = deferred<void>();
  let heldRequests = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/gateway/keys?page=1&pageSize=20", (route) => fulfill(route, source(multipleKeys)));
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      const period = requestedPeriod(route);
      const keyId = url.pathname.split("/")[4];
      const isSummary = url.pathname.endsWith("/usage-summary");
      if (keyId === key.id && period === "week") {
        heldRequests += 1;
        if (heldRequests === 2) firstKeyRequestsHeld.resolve();
        await releaseFirstKey.promise;
      }
      const selectedKey = keyId === secondKey.id ? secondKey : key;
      await fulfill(route, isSummary
        ? source(usageSummary(selectedKey === secondKey ? 22 : 9))
        : source(usagePage(period, selectedKey, `${selectedKey.id}-${period}`)));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "9-month").waitFor({ state: "visible" });
    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await firstKeyRequestsHeld.promise;

    await page.getByRole("button", { name: "更换 API 密钥", exact: true }).click();
    await page.getByRole("dialog", { name: "选择 API 密钥" }).getByRole("button", { name: new RegExp(secondKey.name) }).click();
    await requestEntry(page, "10-week").waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("22", { exact: true }).waitFor({ state: "visible" });

    const lateUsage = waitForUsageResponse(page, key.id, "week", false);
    const lateSummary = waitForUsageResponse(page, key.id, "week", true);
    releaseFirstKey.resolve();
    await waitForResponsesToSettle(page, [lateUsage, lateSummary]);
    assert.equal(await requestEntry(page, "10-week").count(), 1);
    assert.equal(await requestEntry(page, "9-week").count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").getByText("22", { exact: true }).count(), 1);
  } finally {
    releaseFirstKey.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage rejects a response from an earlier route activation", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseFirstVisit = deferred<void>();
  const firstVisitRequestsHeld = deferred<void>();
  let weekRequests = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route(`**/api/gateway/keys/${key.id}`, (route) => fulfill(route, source(key)));
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      const period = requestedPeriod(route);
      const isSummary = url.pathname.endsWith("/usage-summary");
      let oldVisit = false;
      if (period === "week") {
        weekRequests += 1;
        oldVisit = weekRequests <= 2;
        if (weekRequests === 2) firstVisitRequestsHeld.resolve();
        if (oldVisit) await releaseFirstVisit.promise;
      }
      await fulfill(route, isSummary
        ? source(usageSummary(oldVisit ? 70 : period === "week" ? 77 : 30))
        : source(usagePage(period, key, oldVisit ? "old-route-week" : period === "week" ? "new-route-week" : "request-month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-month").waitFor({ state: "visible" });
    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await firstVisitRequestsHeld.promise;
    await page.getByRole("navigation", { name: "API 服务导航" }).getByRole("link", { name: "服务信息", exact: true }).click();
    await page.waitForURL(/\/console\/api$/);
    await page.getByRole("navigation", { name: "API 服务导航" }).getByRole("link", { name: "用量", exact: true }).click();
    await page.waitForURL(/\/console\/api\/usage$/);
    await requestEntry(page, "new-route-week").waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("77", { exact: true }).waitFor({ state: "visible" });

    const lateUsage = waitForUsageResponse(page, key.id, "week", false);
    const lateSummary = waitForUsageResponse(page, key.id, "week", true);
    releaseFirstVisit.resolve();
    await waitForResponsesToSettle(page, [lateUsage, lateSummary]);
    assert.equal(await requestEntry(page, "new-route-week").count(), 1);
    assert.equal(await requestEntry(page, "old-route-week").count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").getByText("77", { exact: true }).count(), 1);
  } finally {
    releaseFirstVisit.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage rejects late results after a Session reset", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const refreshHeld = deferred<void>();
  const releaseRefresh = deferred<void>();
  const refreshSettled = deferred<void>();
  const logoutHeld = deferred<void>();
  const releaseLogout = deferred<void>();
  let holdRefresh = false;
  let heldReads = 0;
  let settledReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route(`**/api/gateway/keys/${key.id}`, (route) => fulfill(route, source(key)));
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const isSummary = new URL(route.request().url()).pathname.endsWith("/usage-summary");
      if (holdRefresh) {
        heldReads += 1;
        if (heldReads === 2) refreshHeld.resolve();
        await releaseRefresh.promise;
        await fulfill(route, isSummary
          ? source(usageSummary(999))
          : source(usagePage("month", key, "request-session-late")));
        settledReads += 1;
        if (settledReads === 2) refreshSettled.resolve();
        return;
      }
      await fulfill(route, isSummary ? source(usageSummary(30)) : source(usagePage("month")));
    });
    await page.route("**/api/auth/logout", async (route) => {
      logoutHeld.resolve();
      await releaseLogout.promise;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "state_persist_failed" })
      });
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-month").waitFor({ state: "visible" });
    holdRefresh = true;
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await refreshHeld.promise;

    await page.locator(".topbar-actions").getByRole("button", { name: "账号信息", exact: true }).click();
    await page.getByRole("complementary", { name: "账号信息", exact: true }).getByRole("button", { name: "退出登录", exact: true }).click();
    await logoutHeld.promise;
    await page.getByRole("heading", { name: "正在安全退出", exact: true }).waitFor({ state: "visible" });

    releaseRefresh.resolve();
    await refreshSettled.promise;
    await page.evaluate(() => new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    }));
    releaseLogout.resolve();
    await page.getByRole("heading", { name: "退出未确认", exact: true }).waitFor({ state: "visible" });

    assert.deepEqual(await readGatewayUsageProjection(page), {
      requestIds: [],
      selectedKeyId: "",
      summaryRequests: null
    });
  } finally {
    releaseRefresh.resolve();
    releaseLogout.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage clears its current range only after a single-key not-found readback", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let listReads = 0;
  let keyReadbacks = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/gateway/keys?page=1&pageSize=20", async (route) => {
      listReads += 1;
      await fulfill(route, source(populatedKeys));
    });
    await page.route(`**/api/gateway/keys/${key.id}`, async (route) => {
      keyReadbacks += 1;
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "gateway_key_not_found" })
      });
    });
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      await fulfill(route, url.pathname.endsWith("/usage-summary")
        ? source(usageSummary(30))
        : source(usagePage("month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-month").waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("30", { exact: true }).waitFor({ state: "visible" });

    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await page.getByText("当前 API 密钥已不存在", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keyReadbacks, 1);
    assert.equal(listReads, 1);
    assert.equal(await page.locator(".gateway-usage-current-key").getByText("未选择", { exact: true }).count(), 1);
    assert.equal(await requestEntry(page, "request-month").count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage preserves key identity when authoritative readback is transiently unavailable", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let listReads = 0;
  let keyReadbacks = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/gateway/keys?page=1&pageSize=20", async (route) => {
      listReads += 1;
      await fulfill(route, source(populatedKeys));
    });
    await page.route(`**/api/gateway/keys/${key.id}`, async (route) => {
      keyReadbacks += 1;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "gateway_key_unavailable" })
      });
    });
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      await fulfill(route, url.pathname.endsWith("/usage-summary")
        ? source(usageSummary(30))
        : source(usagePage("month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-month").waitFor({ state: "visible" });
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await page.getByText("当前 API 密钥暂时无法确认", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keyReadbacks, 1);
    assert.equal(listReads, 1);
    assert.equal(await page.locator(".gateway-usage-current-key").getByText(key.name, { exact: true }).count(), 1);
    assert.equal(await requestEntry(page, "request-month").count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage renders successful records independently from a failed summary", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let summaryReads = 0;
  let usageReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/usage-summary")) {
        summaryReads += 1;
        if (summaryReads === 1) {
          await route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ error: "summary_unavailable" })
          });
        } else {
          await fulfill(route, source(usageSummary(12)));
        }
        return;
      }
      usageReads += 1;
      await fulfill(route, source(usagePage("month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByText("用量结果暂不可用", { exact: true }).waitFor({ state: "visible" });
    const requestTable = page.locator(".request-table-desktop");
    await requestEntry(page, "request-month").waitFor({ state: "visible" });

    assert.equal(await requestTable.getByText("model-month", { exact: true }).count(), 1);
    assert.equal(await requestEntry(page, "request-month").count(), 1);
    assert.equal(await page.locator(".usage-summary-strip").count(), 0);
    await page.locator(".gateway-usage-summary-section").getByRole("button", { name: "重试", exact: true }).click();
    await page.locator(".usage-summary-strip").getByText("12", { exact: true }).waitFor({ state: "visible" });
    assert.equal(summaryReads, 2);
    assert.equal(usageReads, 1);
    assert.equal(await requestEntry(page, "request-month").count(), 1);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage renders a successful summary independently from failed records", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let summaryReads = 0;
  let usageReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      if (!url.pathname.endsWith("/usage-summary")) {
        usageReads += 1;
        if (usageReads === 1) {
          await route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ error: "usage_unavailable" })
          });
        } else {
          await fulfill(route, source(usagePage("month")));
        }
        return;
      }
      summaryReads += 1;
      await fulfill(route, source(usageSummary(11)));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByText("请求记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("11", { exact: true }).waitFor({ state: "visible" });

    assert.equal(await page.locator(".usage-summary-strip").getByText("132", { exact: true }).count(), 1);
    assert.equal(await page.locator(".request-table-desktop").count(), 0);
    await page.locator(".gateway-usage-requests-section").getByRole("button", { name: "重试", exact: true }).click();
    await requestEntry(page, "request-month").waitFor({ state: "visible" });
    assert.equal(usageReads, 2);
    assert.equal(summaryReads, 1);
    assert.equal(await page.locator(".usage-summary-strip").getByText("11", { exact: true }).count(), 1);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage selects the twenty-first key and refreshes it through authoritative readback", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let listReads = 0;
  let selectedKeyReadbacks = 0;
  const keyQueries: Array<{ page: number; search: string }> = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route(`**/api/gateway/keys/${twentyFirstKey.id}`, async (route) => {
      selectedKeyReadbacks += 1;
      await fulfill(route, source(twentyFirstKey));
    });
    await page.route("**/api/gateway/keys*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname !== "/api/gateway/keys") {
        await route.fallback();
        return;
      }
      listReads += 1;
      const requestedPage = Number(url.searchParams.get("page"));
      const search = url.searchParams.get("search") || "";
      keyQueries.push({ page: requestedPage, search });
      await fulfill(route, source({
        items: requestedPage === 2 ? [twentyFirstKey] : firstTwentyKeys,
        total: 21,
        page: requestedPage,
        pageSize: 20,
        pages: 2
      } satisfies GatewayKeyPageDTO));
    });
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      const keyId = url.pathname.split("/")[4];
      const selectedKey = keyId === twentyFirstKey.id
        ? twentyFirstKey
        : firstTwentyKeys.find((item) => item.id === keyId) || key;
      const period = requestedPeriod(route);
      await fulfill(route, url.pathname.endsWith("/usage-summary")
        ? source(usageSummary(selectedKey === twentyFirstKey ? 21 : 1))
        : source(usagePage(period, selectedKey, `request-${selectedKey.id}`)));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await requestEntry(page, "request-1").waitFor({ state: "visible" });
    await page.getByRole("button", { name: "更换 API 密钥", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "选择 API 密钥" });
    const readsBeforeTyping = listReads;
    await dialog.getByLabel("搜索 API 密钥").fill("Browser");
    await page.waitForTimeout(100);
    assert.equal(listReads, readsBeforeTyping);
    await dialog.getByLabel("搜索 API 密钥").press("Enter");
    await dialog.getByText("共 21 个", { exact: true }).waitFor({ state: "visible" });
    await dialog.getByRole("button", { name: "下一页", exact: true }).click();
    await dialog.getByRole("button", { name: new RegExp(twentyFirstKey.name) }).click();

    await page.getByText(twentyFirstKey.name, { exact: true }).waitFor({ state: "visible" });
    await requestEntry(page, `request-${twentyFirstKey.id}`).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("radio", { name: "本周", exact: true }).getAttribute("aria-checked"), "true");
    assert.equal(await page.locator(".gateway-usage-current-key").getByText("启用", { exact: true }).count(), 1);
    assert.equal(await page.locator(".gateway-usage-current-key").getByText("工作空间", { exact: true }).count(), 1);
    assert.equal(await dialog.count(), 0);
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await page.locator(".usage-summary-strip").getByText("21", { exact: true }).waitFor({ state: "visible" });

    assert.equal(selectedKeyReadbacks, 1);
    assert.equal(listReads, 4);
    assert.deepEqual(keyQueries.slice(-2), [{ page: 1, search: "Browser" }, { page: 2, search: "Browser" }]);
    assert.equal(await page.getByText(twentyFirstKey.name, { exact: true }).count(), 1);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage key search rejects stale responses and restores focus after closing", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseFirstOldSearch = deferred<void>();
  const releaseClosedSearch = deferred<void>();
  const firstOldSearchStarted = deferred<void>();
  const closedSearchStarted = deferred<void>();
  const oldKey = { ...key, id: "31", name: "Old Search Result" } satisfies GatewayKeySummaryDTO;
  const latestKey = { ...key, id: "32", name: "Latest Search Result" } satisfies GatewayKeySummaryDTO;
  let oldSearchReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/gateway/keys*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname !== "/api/gateway/keys") {
        await route.fallback();
        return;
      }
      const search = url.searchParams.get("search") || "";
      if (search === "old") {
        oldSearchReads += 1;
        if (oldSearchReads === 1) {
          firstOldSearchStarted.resolve();
          await releaseFirstOldSearch.promise;
        } else {
          closedSearchStarted.resolve();
          await releaseClosedSearch.promise;
        }
        await fulfill(route, source({ ...populatedKeys, items: [oldKey] }));
        return;
      }
      if (search === "latest") {
        await fulfill(route, source({ ...populatedKeys, items: [latestKey] }));
        return;
      }
      await fulfill(route, source(populatedKeys));
    });
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      await fulfill(route, url.pathname.endsWith("/usage-summary")
        ? source(usageSummary(30))
        : source(usagePage("month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-month").waitFor({ state: "visible" });
    const trigger = page.getByRole("button", { name: "更换 API 密钥", exact: true });
    await trigger.click();
    let dialog = page.getByRole("dialog", { name: "选择 API 密钥" });
    assert.equal(await dialog.evaluate((node) => node.contains(document.activeElement)), true);
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    assert.equal(await trigger.evaluate((node) => node === document.activeElement), true);

    await trigger.click();
    dialog = page.getByRole("dialog", { name: "选择 API 密钥" });
    const search = dialog.getByLabel("搜索 API 密钥");
    await search.fill("old");
    await search.press("Enter");
    await firstOldSearchStarted.promise;
    await search.fill("latest");
    await search.press("Enter");
    await dialog.getByRole("button", { name: new RegExp(latestKey.name) }).waitFor({ state: "visible" });
    releaseFirstOldSearch.resolve();
    await page.waitForTimeout(100);
    assert.equal(await dialog.getByRole("button", { name: new RegExp(latestKey.name) }).count(), 1);
    assert.equal(await dialog.getByRole("button", { name: new RegExp(oldKey.name) }).count(), 0);

    await search.fill("old");
    await search.press("Enter");
    await closedSearchStarted.promise;
    await page.keyboard.press("Escape");
    await trigger.click();
    dialog = page.getByRole("dialog", { name: "选择 API 密钥" });
    await dialog.getByRole("button", { name: new RegExp(key.name) }).waitFor({ state: "visible" });
    assert.equal(await dialog.getByLabel("搜索 API 密钥").inputValue(), "");
    releaseClosedSearch.resolve();
    await page.waitForTimeout(100);
    assert.equal(await dialog.getByRole("button", { name: new RegExp(key.name) }).count(), 1);
    assert.equal(await dialog.getByRole("button", { name: new RegExp(oldKey.name) }).count(), 0);
    assert.equal(await page.locator(".gateway-usage-current-key").getByText(key.name, { exact: true }).count(), 1);
    assert.equal(await page.locator(".usage-summary-strip").getByText("30", { exact: true }).count(), 1);
    assert.equal(await requestEntry(page, "request-month").count(), 1);
  } finally {
    releaseFirstOldSearch.resolve();
    releaseClosedSearch.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage pagination retries the requested page without reloading summary", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let summaryReads = 0;
  const usagePages: number[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/usage-summary")) {
        summaryReads += 1;
        await fulfill(route, source(usageSummary(30)));
        return;
      }
      const requestedPage = Number(url.searchParams.get("page"));
      usagePages.push(requestedPage);
      if (requestedPage === 2 && usagePages.filter((pageNumber) => pageNumber === 2).length === 1) {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "usage_unavailable" })
        });
        return;
      }
      await fulfill(route, source({
        ...usagePage("month", key, `request-page-${requestedPage}`),
        total: 21,
        page: requestedPage,
        pages: 2
      }));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-page-1").waitFor({ state: "visible" });
    await page.getByRole("button", { name: "下一页请求记录", exact: true }).click();
    await page.getByText("请求记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(summaryReads, 1);
    assert.equal(await page.locator(".usage-summary-strip").getByText("30", { exact: true }).count(), 1);
    await page.locator(".gateway-usage-requests-section").getByRole("button", { name: "重试", exact: true }).click();
    await requestEntry(page, "request-page-2").waitFor({ state: "visible" });
    await page.getByText("第 2 / 2 页", { exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(usagePages, [1, 2, 2]);
    assert.equal(summaryReads, 1);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage rejects available records with the wrong page identity", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseWrongPageSize = deferred<void>();
  let pageTwoReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/usage-summary")) {
        await fulfill(route, source(usageSummary(30)));
        return;
      }
      const requestedPage = Number(url.searchParams.get("page"));
      if (requestedPage === 1) {
        await fulfill(route, source({
          ...usagePage("month", key, "request-page-1"),
          total: 21,
          pages: 2
        }));
        return;
      }

      pageTwoReads += 1;
      if (pageTwoReads === 1) {
        await fulfill(route, source({
          ...usagePage("month", key, "request-wrong-page"),
          total: 21,
          page: 1,
          pages: 2
        }));
        return;
      }
      if (pageTwoReads === 2) {
        await releaseWrongPageSize.promise;
        await fulfill(route, source({
          ...usagePage("month", key, "request-wrong-page-size"),
          total: 21,
          page: 2,
          pageSize: 50,
          pages: 2
        }));
        return;
      }
      await fulfill(route, source({
        ...usagePage("month", key, "request-page-2"),
        total: 21,
        page: 2,
        pages: 2
      }));
    });

    await loginAndOpenUsage(page, demo.origin);
    await requestEntry(page, "request-page-1").waitFor({ state: "visible" });
    await page.getByRole("button", { name: "下一页请求记录", exact: true }).click();
    await page.getByText("请求记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await requestEntry(page, "request-wrong-page").count(), 0);

    const requestSection = page.locator(".gateway-usage-requests-section");
    await requestSection.getByRole("button", { name: "重试", exact: true }).click();
    await requestSection.getByText("正在读取", { exact: true }).waitFor({ state: "visible" });
    releaseWrongPageSize.resolve();
    await requestSection.getByText("请求记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await requestEntry(page, "request-wrong-page-size").count(), 0);

    await requestSection.getByRole("button", { name: "重试", exact: true }).click();
    await requestEntry(page, "request-page-2").waitFor({ state: "visible" });
    await page.getByText("第 2 / 2 页", { exact: true }).waitFor({ state: "visible" });
    assert.equal(pageTwoReads, 3);
  } finally {
    releaseWrongPageSize.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage keeps an available zero summary when request records are empty", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      await fulfill(route, url.pathname.endsWith("/usage-summary")
        ? source(usageSummary(0))
        : source({ ...usagePage("month"), items: [], total: 0 }, "empty"));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByText("当前范围暂无请求记录", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.locator(".usage-summary-strip dd").allTextContents().then((values) => values.join("|")), "0|0|$0.00");
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage does not request usage when the account has no keys", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let usageReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/gateway/keys?page=1&pageSize=20", (route) => fulfill(route, source(emptyKeys, "empty")));
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      usageReads += 1;
      await route.abort();
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByRole("heading", { name: "暂无 API 密钥", exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: "前往 API 密钥", exact: true }).count(), 1);
    assert.equal(usageReads, 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage preserves the result-first hierarchy in desktop and mobile viewports", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const screenshotDir = process.env.OPL_GATEWAY_USAGE_SCREENSHOT_DIR || "";
  if (screenshotDir) await mkdir(screenshotDir, { recursive: true });
  try {
    for (const [name, viewport] of Object.entries({
      desktop: { width: 1280, height: 900 },
      mobile: { width: 390, height: 844 }
    })) {
      const page = await browser.newPage({ viewport });
      const externalRequests: string[] = [];
      page.on("request", (request) => {
        const url = new URL(request.url());
        if (url.origin !== demo.origin && url.protocol !== "data:") externalRequests.push(request.url());
      });
      await routePopulatedKeys(page);
      await page.route("**/api/gateway/keys/*/usage*", async (route) => {
        const url = new URL(route.request().url());
        await fulfill(route, url.pathname.endsWith("/usage-summary")
          ? source(usageSummary(7))
          : source(usagePage("month")));
      });

      await loginAndOpenUsage(page, demo.origin);
      const request = name === "desktop"
        ? requestEntry(page, "request-month")
        : page.locator(".request-list-mobile").getByRole("listitem").filter({ hasText: "request-month" });
      await request.waitFor({ state: "visible" });
      await page.locator(".usage-summary-strip").getByText("7", { exact: true }).waitFor({ state: "visible" });
      if (name === "desktop") {
        const table = page.getByRole("table", { name: "请求记录" });
        await table.waitFor({ state: "visible" });
        assert.deepEqual(await table.getByRole("columnheader").allTextContents(), ["时间", "模型", "Token", "实际费用", "操作"]);
      }
      assert.equal(await page.locator(".gateway-usage-current-key").getByText(key.name, { exact: true }).count(), 1);
      assert.equal(await page.getByText("Sub2API", { exact: true }).count(), 0);
      assert.equal(await request.getByText("API 路径", { exact: true }).isVisible(), false);

      const layout = await page.evaluate(() => ({
        clientWidth: document.documentElement.clientWidth,
        scrollWidth: document.documentElement.scrollWidth,
        bodyClientWidth: document.body.clientWidth,
        bodyScrollWidth: document.body.scrollWidth
      }));
      assert.ok(layout.scrollWidth <= layout.clientWidth, JSON.stringify(layout));
      assert.ok(layout.bodyScrollWidth <= layout.bodyClientWidth, JSON.stringify(layout));

      if (name === "mobile") {
        const modelBox = await request.getByText("model-month", { exact: true }).boundingBox();
        const costBox = await request.locator(".usage-cost").boundingBox();
        const displays = await request.evaluate((node) => ({
          businessFacts: getComputedStyle(node.querySelector<HTMLElement>(".request-mobile-business-facts")!).display,
          detailLabel: getComputedStyle(node.querySelector<HTMLElement>(".usage-detail-label")!).display,
          heading: getComputedStyle(node.querySelector<HTMLElement>(".request-mobile-heading")!).display
        }));
        assert.deepEqual(displays, { businessFacts: "flex", detailLabel: "flex", heading: "flex" });
        assert.ok(modelBox && modelBox.y + modelBox.height <= viewport.height, JSON.stringify(modelBox));
        assert.ok(costBox && costBox.y + costBox.height <= viewport.height, JSON.stringify(costBox));
      }

      if (screenshotDir) await page.screenshot({ path: join(screenshotDir, `gateway-usage-${name}.png`), fullPage: true });
      if (name === "desktop") await request.getByRole("button", { name: "查看详情", exact: true }).click();
      else await request.locator("summary").click();
      await request.getByText("request-month", { exact: true }).waitFor({ state: "visible" });

      const trigger = page.getByRole("button", { name: "更换 API 密钥", exact: true });
      await trigger.click();
      const dialog = page.getByRole("dialog", { name: "选择 API 密钥" });
      await dialog.waitFor({ state: "visible" });
      assert.equal(await dialog.evaluate((node) => node.contains(document.activeElement)), true);
      if (name === "mobile") {
        for (const target of [dialog.getByLabel("搜索 API 密钥"), dialog.locator(".gateway-key-picker-option").first(), dialog.getByRole("button", { name: "下一页", exact: true })]) {
          const box = await target.boundingBox();
          assert.ok(box && box.width >= 44 && box.height >= 44, JSON.stringify(box));
        }
      }
      await page.keyboard.press("Escape");
      await dialog.waitFor({ state: "hidden" });
      assert.equal(await trigger.evaluate((node) => node === document.activeElement), true);
      assert.deepEqual(externalRequests, [], `${name}: external requests`);
      await page.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
