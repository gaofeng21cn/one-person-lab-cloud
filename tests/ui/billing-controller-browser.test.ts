import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Response, type Route } from "playwright";

import type {
  BillingReceipt,
  BillingReceiptPage,
  SourceEnvelope
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-26T00:00:00Z";

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function receipt(receiptId: string, workspaceId = `workspace-${receiptId}`): BillingReceipt {
  return {
    receiptId,
    type: "billing.workspace_purchased.v1",
    status: "succeeded",
    workspaceId,
    createdAt: fetchedAt,
    resourceType: "workspace",
    resourceId: workspaceId,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    periodStart: "2026-08-01T00:00:00Z",
    paidThrough: "2026-09-01T00:00:00Z",
    totalUsdMicros: 52_580_000
  };
}

function receiptSource(
  receipts: BillingReceipt[],
  { nextCursor = "", hasMore = false }: Pick<BillingReceiptPage, "nextCursor" | "hasMore"> = {}
): SourceEnvelope<BillingReceiptPage> {
  return {
    source: "ledger",
    status: "available",
    available: true,
    fetchedAt,
    data: { receipts, nextCursor, hasMore }
  };
}

function detailSource(detail: BillingReceipt): SourceEnvelope<BillingReceipt> {
  return {
    source: "ledger",
    status: "available",
    available: true,
    fetchedAt,
    data: detail
  };
}

async function fulfill<T>(route: Route, envelope: SourceEnvelope<T>) {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(envelope)
  });
}

async function fail(route: Route) {
  await route.fulfill({
    status: 503,
    contentType: "application/json",
    body: JSON.stringify({ error: "upstream_unavailable" })
  });
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

async function openBillingReceipts(page: Page) {
  await page.locator(".side-nav").getByRole("link", { name: "费用", exact: true }).click();
  await page.waitForURL(/\/console\/billing$/);
  await page.getByRole("radio", { name: "账单记录", exact: true }).click();
  await page.getByRole("heading", { name: "账单记录", exact: true }).waitFor({ state: "visible" });
}

function receiptRow(page: Page, receiptId: string) {
  return page.locator(".billing-table-desktop tbody tr").filter({
    has: page.locator("details.receipt-row-technical-details").filter({ hasText: receiptId })
  });
}

function detailPanel(page: Page) {
  return page.locator(".receipt-detail");
}

async function openReceiptTechnicalDetails(page: Page, receiptId: string) {
  const details = detailPanel(page).locator("details.receipt-technical-details");
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
  await details.getByText(receiptId, { exact: true }).waitFor({ state: "visible" });
}

function waitForListResponse(page: Page, limit: number, cursor: string) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/billing/receipts"
      && url.searchParams.get("limit") === String(limit)
      && (url.searchParams.get("cursor") || "") === cursor;
  });
}

function waitForDetailResponse(page: Page, receiptId: string) {
  return page.waitForResponse((response) => new URL(response.url()).pathname === `/api/billing/receipts/${receiptId}`);
}

async function settleResponse(page: Page, responsePromise: Promise<Response>) {
  const response = await responsePromise;
  await response.finished();
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

test("Billing rejects a late overview limit-3 page after the billing limit-20 page commits", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const overviewHeld = deferred<void>();
  const releaseOverview = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", async (route) => {
      const url = new URL(route.request().url());
      const limit = Number(url.searchParams.get("limit"));
      if (limit === 3) {
        overviewHeld.resolve();
        await releaseOverview.promise;
        await fulfill(route, receiptSource([receipt("overview-late")])).catch(() => {});
        return;
      }
      await fulfill(route, receiptSource([receipt("billing-current")]));
    });

    await login(page, demo.origin);
    await overviewHeld.promise;
    await openBillingReceipts(page);
    await receiptRow(page, "billing-current").waitFor({ state: "visible" });

    const lateOverview = waitForListResponse(page, 3, "");
    releaseOverview.resolve();
    await settleResponse(page, lateOverview);

    assert.equal(await receiptRow(page, "billing-current").count(), 1);
    assert.equal(await receiptRow(page, "overview-late").count(), 0);
  } finally {
    releaseOverview.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Billing rejects Receipt A when its detail arrives after Receipt B", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const receiptAHeld = deferred<void>();
  const releaseReceiptA = deferred<void>();
  const receiptA = receipt("receipt-A");
  const receiptB = receipt("receipt-B");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", (route) => fulfill(route, receiptSource([receiptA, receiptB])));
    await page.route("**/api/billing/receipts/*", async (route) => {
      const receiptId = decodeURIComponent(new URL(route.request().url()).pathname.split("/").at(-1) || "");
      if (receiptId === receiptA.receiptId) {
        receiptAHeld.resolve();
        await releaseReceiptA.promise;
        await fulfill(route, detailSource(receiptA)).catch(() => {});
        return;
      }
      await fulfill(route, detailSource(receiptB));
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, receiptA.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await receiptAHeld.promise;
    await receiptRow(page, receiptB.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await openReceiptTechnicalDetails(page, receiptB.receiptId);

    const lateReceiptA = waitForDetailResponse(page, receiptA.receiptId);
    releaseReceiptA.resolve();
    await settleResponse(page, lateReceiptA);

    assert.equal(await detailPanel(page).getByText(receiptB.receiptId, { exact: true }).count(), 1);
    assert.equal(await detailPanel(page).getByText(receiptA.receiptId, { exact: true }).count(), 0);
  } finally {
    releaseReceiptA.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Billing close invalidates an in-flight Receipt detail", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const detailHeld = deferred<void>();
  const releaseDetail = deferred<void>();
  const receiptA = receipt("receipt-close");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", (route) => fulfill(route, receiptSource([receiptA])));
    await page.route("**/api/billing/receipts/*", async (route) => {
      detailHeld.resolve();
      await releaseDetail.promise;
      await fulfill(route, detailSource(receiptA)).catch(() => {});
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, receiptA.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await detailHeld.promise;
    await page.getByRole("button", { name: "关闭收据详情", exact: true }).click();
    assert.equal(await detailPanel(page).count(), 0);

    const lateDetail = waitForDetailResponse(page, receiptA.receiptId);
    releaseDetail.resolve();
    await settleResponse(page, lateDetail);

    assert.equal(await detailPanel(page).count(), 0);
    assert.equal(await detailPanel(page).getByText(receiptA.receiptId, { exact: true }).count(), 0);
  } finally {
    releaseDetail.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Billing preserves opaque cursor order across next and previous navigation", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const cursorA = "opaque:A/+?=";
  const cursorB = "opaque:B/+?=";
  const billingCursors: string[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", async (route) => {
      const url = new URL(route.request().url());
      const cursor = url.searchParams.get("cursor") || "";
      const limit = Number(url.searchParams.get("limit"));
      if (limit === 20) billingCursors.push(cursor);
      if (cursor === cursorA) {
        await fulfill(route, receiptSource([receipt("page-2")], { nextCursor: cursorB, hasMore: true }));
        return;
      }
      if (cursor === cursorB) {
        await fulfill(route, receiptSource([receipt("page-3")]));
        return;
      }
      await fulfill(route, receiptSource([receipt("page-1")], { nextCursor: cursorA, hasMore: true }));
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    const pagination = page.getByRole("navigation", { name: "账单记录分页" });
    await receiptRow(page, "page-1").waitFor({ state: "visible" });

    await pagination.getByRole("button", { name: "下一页", exact: true }).click();
    await receiptRow(page, "page-2").waitFor({ state: "visible" });
    await pagination.getByText("第 2 页", { exact: true }).waitFor({ state: "visible" });
    await pagination.getByRole("button", { name: "下一页", exact: true }).click();
    await receiptRow(page, "page-3").waitFor({ state: "visible" });
    await pagination.getByText("第 3 页", { exact: true }).waitFor({ state: "visible" });

    await pagination.getByRole("button", { name: "上一页", exact: true }).click();
    await receiptRow(page, "page-2").waitFor({ state: "visible" });
    await pagination.getByRole("button", { name: "上一页", exact: true }).click();
    await receiptRow(page, "page-1").waitFor({ state: "visible" });

    assert.deepEqual(billingCursors, ["", cursorA, cursorB, cursorA, ""]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Billing page navigation clears detail and rejects the old detail completion", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const cursor = "next-page-cursor";
  const detailHeld = deferred<void>();
  const releaseDetail = deferred<void>();
  const receiptA = receipt("receipt-page-1");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", async (route) => {
      const requestCursor = new URL(route.request().url()).searchParams.get("cursor") || "";
      await fulfill(route, requestCursor === cursor
        ? receiptSource([receipt("receipt-page-2")])
        : receiptSource([receiptA], { nextCursor: cursor, hasMore: true }));
    });
    await page.route("**/api/billing/receipts/*", async (route) => {
      detailHeld.resolve();
      await releaseDetail.promise;
      await fulfill(route, detailSource(receiptA)).catch(() => {});
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, receiptA.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await detailHeld.promise;
    await page.getByRole("navigation", { name: "账单记录分页" })
      .getByRole("button", { name: "下一页", exact: true }).click();
    await receiptRow(page, "receipt-page-2").waitFor({ state: "visible" });
    assert.equal(await detailPanel(page).count(), 0);

    const lateDetail = waitForDetailResponse(page, receiptA.receiptId);
    releaseDetail.resolve();
    await settleResponse(page, lateDetail);

    assert.equal(await detailPanel(page).count(), 0);
    assert.equal(await receiptRow(page, "receipt-page-2").count(), 1);
  } finally {
    releaseDetail.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Billing route exit rejects an in-flight Receipt detail", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const detailHeld = deferred<void>();
  const releaseDetail = deferred<void>();
  const receiptA = receipt("receipt-route-exit");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", (route) => fulfill(route, receiptSource([receiptA])));
    await page.route("**/api/billing/receipts/*", async (route) => {
      detailHeld.resolve();
      await releaseDetail.promise;
      await fulfill(route, detailSource(receiptA)).catch(() => {});
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, receiptA.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await detailHeld.promise;
    await page.locator(".side-nav").getByRole("link", { name: "OPL Gateway", exact: true }).click();
    await page.waitForURL(/\/console\/api$/);

    const lateDetail = waitForDetailResponse(page, receiptA.receiptId);
    releaseDetail.resolve();
    await settleResponse(page, lateDetail);
    await openBillingReceipts(page);

    assert.equal(await detailPanel(page).count(), 0);
    assert.equal(await detailPanel(page).getByText(receiptA.receiptId, { exact: true }).count(), 0);
  } finally {
    releaseDetail.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Billing keeps list and detail failure state isolated", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const receiptA = receipt("receipt-fails");
  const receiptB = receipt("receipt-succeeds");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", (route) => fulfill(route, receiptSource([receiptA, receiptB])));
    await page.route("**/api/billing/receipts/*", async (route) => {
      const receiptId = decodeURIComponent(new URL(route.request().url()).pathname.split("/").at(-1) || "");
      if (receiptId === receiptA.receiptId) {
        await fail(route);
        return;
      }
      await fulfill(route, detailSource(receiptB));
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, receiptA.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await detailPanel(page).getByText("收据详情暂不可用", { exact: true }).waitFor({ state: "visible" });

    assert.equal(await receiptRow(page, receiptA.receiptId).count(), 1);
    assert.equal(await receiptRow(page, receiptB.receiptId).count(), 1);
    assert.equal(await page.locator(".billing-surface").getByText("收据详情暂不可用", { exact: true }).count(), 0);

    await receiptRow(page, receiptB.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await openReceiptTechnicalDetails(page, receiptB.receiptId);
    assert.equal(await detailPanel(page).getByText("收据详情暂不可用", { exact: true }).count(), 0);
    assert.equal(await receiptRow(page, receiptA.receiptId).count(), 1);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Billing list failure does not become a Receipt detail failure", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const receiptA = receipt("receipt-before-list-failure");
  let failBillingList = false;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", async (route) => {
      const limit = Number(new URL(route.request().url()).searchParams.get("limit"));
      if (limit === 20 && failBillingList) {
        await fail(route);
        return;
      }
      await fulfill(route, receiptSource([receiptA]));
    });
    await page.route("**/api/billing/receipts/*", (route) => fulfill(route, detailSource(receiptA)));

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, receiptA.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await openReceiptTechnicalDetails(page, receiptA.receiptId);

    failBillingList = true;
    await page.locator(".side-nav").getByRole("link", { name: "OPL Gateway", exact: true }).click();
    await page.waitForURL(/\/console\/api$/);
    await openBillingReceipts(page);
    await page.locator(".billing-surface").getByText("账单记录暂不可用", { exact: true }).waitFor({ state: "visible" });

    assert.equal(await detailPanel(page).count(), 0);
    assert.equal(await page.getByText("收据详情暂不可用", { exact: true }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Billing Session reset rejects a detail completion from the signed-out Session", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const detailHeld = deferred<void>();
  const releaseDetail = deferred<void>();
  const firstSessionReceipt = receipt("first-session");
  const secondSessionReceipt = receipt("second-session");
  let secondSession = false;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/receipts?*", (route) => fulfill(
      route,
      receiptSource([secondSession ? secondSessionReceipt : firstSessionReceipt])
    ));
    await page.route("**/api/billing/receipts/*", async (route) => {
      detailHeld.resolve();
      await releaseDetail.promise;
      await fulfill(route, detailSource(firstSessionReceipt)).catch(() => {});
    });

    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, firstSessionReceipt.receiptId).getByRole("button", { name: "查看", exact: true }).click();
    await detailHeld.promise;
    await page.locator(".topbar-actions").getByRole("button", { name: "账号信息", exact: true }).click();
    await page.getByRole("complementary", { name: "账号信息", exact: true }).getByRole("button", { name: "退出登录", exact: true }).click();
    await page.waitForURL(new RegExp(`${demo.origin.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}/?$`));

    secondSession = true;
    const lateDetail = waitForDetailResponse(page, firstSessionReceipt.receiptId);
    releaseDetail.resolve();
    await settleResponse(page, lateDetail);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await login(page, demo.origin);
    await openBillingReceipts(page);
    await receiptRow(page, secondSessionReceipt.receiptId).waitFor({ state: "visible" });

    assert.equal(await receiptRow(page, firstSessionReceipt.receiptId).count(), 0);
    assert.equal(await detailPanel(page).count(), 0);
    assert.equal(await page.getByText(firstSessionReceipt.receiptId, { exact: true }).count(), 0);
  } finally {
    releaseDetail.resolve();
    await browser.close();
    await demo.close();
  }
});
