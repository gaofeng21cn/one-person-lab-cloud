import assert from "node:assert/strict";
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
  await page.getByRole("heading", { name: "使用记录", exact: true }).waitFor({ state: "visible" });
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
    await page.locator(".request-table-desktop").getByText("request-month", { exact: true }).waitFor({ state: "visible" });

    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await weekRequestsHeld.promise;
    await page.getByRole("radio", { name: "今日", exact: true }).click();

    const requestTable = page.locator(".request-table-desktop");
    await requestTable.getByText("request-today", { exact: true }).waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("7", { exact: true }).waitFor({ state: "visible" });

    const lateWeekUsage = waitForUsageResponse(page, key.id, "week", false);
    const lateWeekSummary = waitForUsageResponse(page, key.id, "week", true);
    releaseWeek.resolve();
    await waitForResponsesToSettle(page, [lateWeekUsage, lateWeekSummary]);
    assert.equal(await requestTable.getByText("request-today", { exact: true }).count(), 1);
    assert.equal(await requestTable.getByText("request-week", { exact: true }).count(), 0);
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
    await page.locator(".request-table-desktop").getByText("9-month", { exact: true }).waitFor({ state: "visible" });
    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await firstKeyRequestsHeld.promise;

    await page.locator(".gateway-usage-toolbar .console-select").getByRole("button").click();
    await page.getByRole("option", { name: `${secondKey.name} · ${secondKey.id}`, exact: true }).click();
    const requestTable = page.locator(".request-table-desktop");
    await requestTable.getByText("10-week", { exact: true }).waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("22", { exact: true }).waitFor({ state: "visible" });

    const lateUsage = waitForUsageResponse(page, key.id, "week", false);
    const lateSummary = waitForUsageResponse(page, key.id, "week", true);
    releaseFirstKey.resolve();
    await waitForResponsesToSettle(page, [lateUsage, lateSummary]);
    assert.equal(await requestTable.getByText("10-week", { exact: true }).count(), 1);
    assert.equal(await requestTable.getByText("9-week", { exact: true }).count(), 0);
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
    await page.locator(".request-table-desktop").getByText("request-month", { exact: true }).waitFor({ state: "visible" });
    await page.getByRole("radio", { name: "本周", exact: true }).click();
    await firstVisitRequestsHeld.promise;
    await page.getByRole("navigation", { name: "API 服务导航" }).getByRole("link", { name: "概览", exact: true }).click();
    await page.waitForURL(/\/console\/api$/);
    await page.getByRole("navigation", { name: "API 服务导航" }).getByRole("link", { name: "使用记录", exact: true }).click();
    await page.waitForURL(/\/console\/api\/usage$/);
    const requestTable = page.locator(".request-table-desktop");
    await requestTable.getByText("new-route-week", { exact: true }).waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("77", { exact: true }).waitFor({ state: "visible" });

    const lateUsage = waitForUsageResponse(page, key.id, "week", false);
    const lateSummary = waitForUsageResponse(page, key.id, "week", true);
    releaseFirstVisit.resolve();
    await waitForResponsesToSettle(page, [lateUsage, lateSummary]);
    assert.equal(await requestTable.getByText("new-route-week", { exact: true }).count(), 1);
    assert.equal(await requestTable.getByText("old-route-week", { exact: true }).count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").getByText("77", { exact: true }).count(), 1);
  } finally {
    releaseFirstVisit.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage clears selection and derived data after an empty key refresh", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let keyReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/gateway/keys?page=1&pageSize=20", async (route) => {
      keyReads += 1;
      await fulfill(route, keyReads === 1 ? source(populatedKeys) : source(emptyKeys, "empty"));
    });
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      await fulfill(route, url.pathname.endsWith("/usage-summary")
        ? source(usageSummary(30))
        : source(usagePage("month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.locator(".request-table-desktop").getByText("request-month", { exact: true }).waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("30", { exact: true }).waitFor({ state: "visible" });

    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await page.getByRole("heading", { name: "暂无 API Key", exact: true }).waitFor({ state: "visible" });

    assert.equal(keyReads, 2);
    assert.equal(await page.getByLabel("API Key").inputValue(), "");
    assert.equal(await page.locator(".request-table-desktop").getByText("request-month", { exact: true }).count(), 0);
    assert.equal(await page.locator(".usage-summary-strip").count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage renders successful records independently from a failed summary", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/usage-summary")) {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "summary_unavailable" })
        });
        return;
      }
      await fulfill(route, source(usagePage("month")));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByText("使用汇总暂不可用", { exact: true }).waitFor({ state: "visible" });
    const requestTable = page.locator(".request-table-desktop");
    await requestTable.getByText("request-month", { exact: true }).waitFor({ state: "visible" });

    assert.equal(await requestTable.getByText("model-month", { exact: true }).count(), 1);
    assert.equal(await requestTable.getByText("request-month", { exact: true }).count(), 1);
    assert.equal(await page.locator(".usage-summary-strip").count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway usage renders a successful summary independently from failed records", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routePopulatedKeys(page);
    await page.route("**/api/gateway/keys/*/usage*", async (route) => {
      const url = new URL(route.request().url());
      if (!url.pathname.endsWith("/usage-summary")) {
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "usage_unavailable" })
        });
        return;
      }
      await fulfill(route, source(usageSummary(11)));
    });

    await loginAndOpenUsage(page, demo.origin);
    await page.getByText("使用记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    await page.locator(".usage-summary-strip").getByText("11", { exact: true }).waitFor({ state: "visible" });

    assert.equal(await page.locator(".usage-summary-strip").getByText("132", { exact: true }).count(), 1);
    assert.equal(await page.locator(".request-table-desktop").count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});
