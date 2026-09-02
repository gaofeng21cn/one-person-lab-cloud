import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  GatewayAccountUsageSummaryDTO,
  GatewayBalanceHistoryPageDTO,
  GatewayEndpointDTO,
  GatewayWallet,
  SourceEnvelope
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

function source<T>(data: T): SourceEnvelope<T> {
  return {
    source: "sub2api",
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

const wallet: GatewayWallet = {
  userId: "41",
  currency: "USD",
  usdMicros: "25000000",
  status: "active"
};

const accountUsage: GatewayAccountUsageSummaryDTO = {
  totalRequests: 17,
  totalInputTokens: 170,
  totalOutputTokens: 34,
  totalTokens: 204,
  totalActualCostUsdMicros: 125_000
};

const endpoint: GatewayEndpointDTO = { baseUrl: "https://gateway.read-owner.example/v1" };

function history(page: number, type = `balance-page-${page}`): GatewayBalanceHistoryPageDTO {
  return {
    items: [{
      type,
      valueUsdMicros: "1000000",
      status: "used",
      usedAt: fetchedAt,
      createdAt: fetchedAt
    }],
    total: 41,
    page,
    pageSize: 20,
    pages: 3
  };
}

async function fulfill<T>(route: Route, envelope: SourceEnvelope<T>) {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(envelope)
  });
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

async function settle(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

test("Gateway Account Read settles API overview projections independently", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);
    await page.route("**/api/gateway/wallet", (route) => fulfill(route, source(wallet)));
    await page.route("**/api/gateway/usage-summary?*", (route) => route.abort("failed"));
    await page.route("**/api/gateway/balance-history?*", (route) => fulfill(route, source(history(1))));
    await page.route("**/api/gateway/endpoint", (route) => fulfill(route, source(endpoint)));

    await page.goto(`${demo.origin}/console/api`, { waitUntil: "domcontentloaded" });
    await page.locator(".api-endpoint-row code").getByText(endpoint.baseUrl, { exact: true }).waitFor({ state: "visible" });
    await page.getByText("待确认", { exact: true }).first().waitFor({ state: "visible" });

    const metrics = page.locator(".spend-strip strong");
    assert.notEqual(await metrics.nth(0).textContent(), "暂不可用");
    assert.equal(await metrics.nth(1).textContent(), "暂不可用");
    assert.equal(await metrics.nth(2).textContent(), "暂不可用");
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Gateway Account Read rejects wallet and history completions from an earlier route activation", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const oldWalletHeld = deferred();
  const oldHistoryHeld = deferred();
  const releaseOldReads = deferred();
  const oldWalletSettled = deferred();
  const oldHistorySettled = deferred();
  const staleWallet: GatewayWallet = { ...wallet, usdMicros: "111000000" };
  const freshWallet: GatewayWallet = { ...wallet, usdMicros: "222000000" };
  let walletReads = 0;
  let historyReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);
    await page.route("**/api/gateway/wallet", async (route) => {
      walletReads += 1;
      if (walletReads === 1) {
        oldWalletHeld.resolve();
        await releaseOldReads.promise;
        await fulfill(route, source(staleWallet));
        oldWalletSettled.resolve();
        return;
      }
      await fulfill(route, source(freshWallet));
    });
    await page.route("**/api/gateway/balance-history?*", async (route) => {
      historyReads += 1;
      if (historyReads === 1) {
        oldHistoryHeld.resolve();
        await releaseOldReads.promise;
        await fulfill(route, source(history(1, "stale-route-history")));
        oldHistorySettled.resolve();
        return;
      }
      await fulfill(route, source(history(1, "fresh-route-history")));
    });

    await page.goto(`${demo.origin}/console/api`, { waitUntil: "domcontentloaded" });
    await Promise.all([oldWalletHeld.promise, oldHistoryHeld.promise]);
    const tabs = page.getByRole("navigation", { name: "API 服务导航" });
    await tabs.getByRole("link", { name: "用量", exact: true }).click();
    await page.waitForURL(/\/console\/api\/usage$/);
    await page.getByRole("navigation", { name: "API 服务导航" })
      .getByRole("link", { name: "服务信息", exact: true }).click();
    await page.waitForURL(/\/console\/api$/);
    await page.getByText("待确认", { exact: true }).first().waitFor({ state: "visible" });
    await page.locator(".spend-strip strong").nth(0).getByText("$222.00", { exact: true }).waitFor({ state: "visible" });

    releaseOldReads.resolve();
    await Promise.all([oldWalletSettled.promise, oldHistorySettled.promise]);
    await settle(page);
    assert.equal(walletReads, 2);
    assert.equal(historyReads, 2);
    assert.equal(await page.locator(".spend-strip strong").nth(0).textContent(), "$222.00");
    assert.equal(await page.getByText("待确认", { exact: true }).count(), 1);
    assert.equal(await page.getByText("stale-route-history", { exact: true }).count(), 0);
  } finally {
    releaseOldReads.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway Account Read rejects wallet and history completions from a logged-out Session", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const oldWalletHeld = deferred();
  const oldHistoryHeld = deferred();
  const releaseOldSessionReads = deferred();
  const oldWalletSettled = deferred();
  const oldHistorySettled = deferred();
  const staleWallet: GatewayWallet = { ...wallet, usdMicros: "444000000" };
  const freshWallet: GatewayWallet = { ...wallet, usdMicros: "555000000" };
  let walletReads = 0;
  let historyReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);
    await page.route("**/api/gateway/wallet", async (route) => {
      walletReads += 1;
      if (walletReads === 1) {
        oldWalletHeld.resolve();
        await releaseOldSessionReads.promise;
        await fulfill(route, source(staleWallet));
        oldWalletSettled.resolve();
        return;
      }
      await fulfill(route, source(freshWallet));
    });
    await page.route("**/api/gateway/balance-history?*", async (route) => {
      historyReads += 1;
      if (historyReads === 1) {
        oldHistoryHeld.resolve();
        await releaseOldSessionReads.promise;
        await fulfill(route, source(history(1, "stale-session-history")));
        oldHistorySettled.resolve();
        return;
      }
      await fulfill(route, source(history(1, "fresh-session-history")));
    });

    await page.goto(`${demo.origin}/console/api`, { waitUntil: "domcontentloaded" });
    await Promise.all([oldWalletHeld.promise, oldHistoryHeld.promise]);
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await page.waitForURL(`${demo.origin}/`);
    assert.equal(await page.locator(".spend-strip").count(), 0);
    assert.equal(await page.getByText("stale-session-history", { exact: true }).count(), 0);

    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/login$/);
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.locator(".side-nav").getByRole("link", { name: "API", exact: true }).click();
    await page.waitForURL(/\/console\/api$/);
    await page.getByText("待确认", { exact: true }).first().waitFor({ state: "visible" });
    await page.locator(".spend-strip strong").nth(0).getByText("$555.00", { exact: true }).waitFor({ state: "visible" });

    releaseOldSessionReads.resolve();
    await Promise.all([oldWalletSettled.promise, oldHistorySettled.promise]);
    await settle(page);
    assert.equal(page.url(), `${demo.origin}/console/api`);
    assert.equal(await page.locator(".sidebar-account strong").textContent(), CONSOLE_DEMO_CREDENTIALS.customer.email);
    assert.equal(await page.locator(".spend-strip strong").nth(0).textContent(), "$555.00");
    assert.equal(await page.getByText("待确认", { exact: true }).count(), 1);
    assert.equal(await page.getByText("stale-session-history", { exact: true }).count(), 0);
  } finally {
    releaseOldSessionReads.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway Account Read rejects an older response for the same balance page", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const firstPageTwoHeld = deferred();
  const releaseFirstPageTwo = deferred();
  const firstPageTwoSettled = deferred();
  let pageTwoRequests = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);
    await page.route("**/api/gateway/balance-history?*", async (route) => {
      const requestedPage = Number(new URL(route.request().url()).searchParams.get("page"));
      if (requestedPage !== 2) {
        await fulfill(route, source(history(1)));
        return;
      }
      pageTwoRequests += 1;
      if (pageTwoRequests === 1) {
        firstPageTwoHeld.resolve();
        await releaseFirstPageTwo.promise;
        await fulfill(route, source(history(2, "stale-page-2")));
        firstPageTwoSettled.resolve();
        return;
      }
      await fulfill(route, source(history(2, "fresh-page-2")));
    });

    await page.goto(`${demo.origin}/console/api`, { waitUntil: "domcontentloaded" });
    await page.getByText("待确认", { exact: true }).first().waitFor({ state: "visible" });
    const next = page.getByRole("navigation", { name: "余额历史分页" })
      .getByRole("button", { name: "下一页", exact: true });
    await next.click();
    await firstPageTwoHeld.promise;
    await next.click();
    await page.getByText("待确认", { exact: true }).first().waitFor({ state: "visible" });

    releaseFirstPageTwo.resolve();
    await firstPageTwoSettled.promise;
    await settle(page);
    assert.equal(pageTwoRequests, 2);
    assert.equal(await page.getByText("待确认", { exact: true }).count(), 1);
    assert.equal(await page.getByText("stale-page-2", { exact: true }).count(), 0);
  } finally {
    releaseFirstPageTwo.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Gateway Account Read follows the route plan and leaves one endpoint owner on Keys", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const reads = { wallet: 0, accountUsage: 0, balanceHistory: 0, endpoint: 0 };
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);
    await page.route("**/api/gateway/wallet", async (route) => {
      reads.wallet += 1;
      await fulfill(route, source(wallet));
    });
    await page.route("**/api/gateway/usage-summary?*", async (route) => {
      reads.accountUsage += 1;
      await fulfill(route, source(accountUsage));
    });
    await page.route("**/api/gateway/balance-history?*", async (route) => {
      reads.balanceHistory += 1;
      await fulfill(route, source(history(1)));
    });
    await page.route("**/api/gateway/endpoint", async (route) => {
      reads.endpoint += 1;
      await fulfill(route, source(endpoint));
    });

    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "新建工作空间", exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(reads, { wallet: 1, accountUsage: 0, balanceHistory: 0, endpoint: 0 });

    reads.wallet = 0;
    reads.accountUsage = 0;
    reads.balanceHistory = 0;
    reads.endpoint = 0;
    await page.goto(`${demo.origin}/console/api/usage`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "用量记录", exact: true }).waitFor({ state: "visible" });
    await settle(page);
    assert.deepEqual(reads, { wallet: 0, accountUsage: 0, balanceHistory: 0, endpoint: 0 });

    await page.goto(`${demo.origin}/console/api/keys`, { waitUntil: "domcontentloaded" });
    await page.locator(".keys-endpoint code").getByText(endpoint.baseUrl, { exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(reads, { wallet: 0, accountUsage: 0, balanceHistory: 0, endpoint: 1 });
  } finally {
    await browser.close();
    await demo.close();
  }
});
