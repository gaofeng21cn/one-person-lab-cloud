import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Response, type Route } from "playwright";

import type {
  BillingReceipt,
  BillingReceiptPage,
  SourceEnvelope,
  WorkspaceSettlementTrend
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";

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
    hasText: `workspace-${receiptId}`
  });
}

function detailPanel(page: Page) {
  return page.locator(".receipt-detail");
}

async function openReceiptDetails(page: Page, workspaceId: string) {
  await detailPanel(page).getByText(`工作空间编号：${workspaceId}`, { exact: true }).waitFor({ state: "visible" });
  assert.equal(await detailPanel(page).locator("details.receipt-technical-details").count(), 0);
  assert.equal(await detailPanel(page).getByText("pilot-usd-2026-07-v1", { exact: true }).count(), 0);
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
    await openReceiptDetails(page, receiptB.workspaceId);

    const lateReceiptA = waitForDetailResponse(page, receiptA.receiptId);
    releaseReceiptA.resolve();
    await settleResponse(page, lateReceiptA);

    assert.equal(await detailPanel(page).getByText(`工作空间编号：${receiptB.workspaceId}`, { exact: true }).count(), 1);
    assert.equal(await detailPanel(page).getByText(`工作空间编号：${receiptA.workspaceId}`, { exact: true }).count(), 0);
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
    assert.equal(await detailPanel(page).getByText(`工作空间编号：${receiptA.workspaceId}`, { exact: true }).count(), 0);
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
    assert.equal(await detailPanel(page).getByText(`工作空间编号：${receiptA.workspaceId}`, { exact: true }).count(), 0);
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
    await openReceiptDetails(page, receiptB.workspaceId);
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
    await openReceiptDetails(page, receiptA.workspaceId);

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
    assert.equal(await page.getByText(`工作空间编号：${firstSessionReceipt.workspaceId}`, { exact: true }).count(), 0);
  } finally {
    releaseDetail.resolve();
    await browser.close();
    await demo.close();
  }
});

test("customers can trace monthly charges and partial refunds to the original Workspace order on desktop and mobile", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const monthly: BillingReceipt = { ...receipt("monthly", "ws-1"), status: "completed", operationId: "renewal-local-order", type: "billing.workspace_renewed.v1" };
  const refund: BillingReceipt = { ...monthly, receiptId: "refund", type: "gateway.wallet_adjustment.v1", kind: "business_refund", refundUsdMicros: 3_000_000, operationId: "refund-local-order", relatedOperationId: monthly.operationId, chargeReference: "private-upstream-code" };
  const expiry: BillingReceipt = { ...monthly, receiptId: "expiry", type: "billing.workspace_expired.v1" };
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const page = await browser.newPage({ viewport });
      await page.route("**/api/billing/receipts?*", (route) => fulfill(route, receiptSource([refund, expiry, monthly])));
      await page.route("**/api/billing/receipts/refund", (route) => fulfill(route, detailSource(refund)));
      await login(page, demo.origin);
      await page.goto(`${demo.origin}/console/billing`, { waitUntil: "domcontentloaded" });
      await page.getByText("工作空间月费与 API 用量从同一账户余额扣除；退款退回原账户余额。", { exact: false }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("link", { name: "查看 API 用量", exact: true }).getAttribute("href"), "/console/api/usage");
      await page.getByRole("radio", { name: "账单记录", exact: true }).click();
      const surface = page.locator(viewport.width > 600 ? ".billing-table-desktop" : ".billing-list-mobile");
      await surface.getByText("扣款 $52.58", { exact: true }).waitFor({ state: "visible" });
      await surface.getByText("退款 $3.00", { exact: true }).waitFor({ state: "visible" });
      await surface.getByText("未扣款", { exact: true }).waitFor({ state: "visible" });
      if (viewport.width > 600) {
        await surface.locator("tbody tr").filter({ hasText: "退款 $3.00" }).getByRole("button", { name: "查看", exact: true }).click();
      } else {
        await surface.getByRole("listitem").filter({ hasText: "退款 $3.00" }).click();
      }
      const detail = detailPanel(page);
      for (const text of ["退款 $3.00", "原账户余额", "renewal-local-order", "refund-local-order", "工作空间编号：ws-1", "2026/08/01 至 2026/09/01"]) {
        await detail.getByText(text, { exact: true }).waitFor({ state: "visible" });
      }
      assert.equal(await detail.getByText("private-upstream-code", { exact: true }).count(), 0);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
      await page.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("customers distinguish a closed unfulfilled order from its refund and can identify an uncharged closure", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const closed: BillingReceipt = {
    ...receipt("closed", "ws-closed"),
    status: "completed",
    operationId: "launch-local-closed",
    type: "billing.workspace_closed.v1",
    chargeUsdMicros: 52_580_000
  };
  const refund: BillingReceipt = {
    ...closed,
    receiptId: "closed-refund",
    type: "gateway.wallet_adjustment.v1",
    kind: "business_refund",
    operationId: "refund-local-closed",
    relatedOperationId: closed.operationId,
    refundUsdMicros: 52_580_000
  };
  const uncharged: BillingReceipt = {
    ...closed,
    receiptId: "uncharged-closed",
    workspaceId: "ws-uncharged-closed",
    resourceId: "ws-uncharged-closed",
    operationId: "launch-local-uncharged",
    chargeUsdMicros: 0,
    periodStart: "",
    paidThrough: ""
  };
  const receipts = [closed, refund, uncharged];
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const page = await browser.newPage({ viewport });
      const pageErrors: string[] = [];
      const externalRequests: string[] = [];
      page.on("pageerror", (error) => pageErrors.push(error.message));
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== demo.origin) {
          externalRequests.push(route.request().url());
          await route.abort("blockedbyclient");
          return;
        }
        if (url.pathname === "/@vite/client") {
          await route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
          return;
        }
        await route.continue();
      });
      await page.route("**/api/billing/receipts?*", (route) => fulfill(route, receiptSource(receipts)));
      await page.route("**/api/billing/receipts/*", (route) => {
        const receiptId = new URL(route.request().url()).pathname.split("/").at(-1);
        const detail = receipts.find((item) => item.receiptId === receiptId);
        assert.ok(detail);
        return fulfill(route, detailSource(detail));
      });
      await login(page, demo.origin);
      await page.goto(`${demo.origin}/console/billing`, { waitUntil: "domcontentloaded" });
      await page.getByRole("radio", { name: "账单记录", exact: true }).click();
      const desktop = viewport.width > 600;
      const surface = page.locator(desktop ? ".billing-table-desktop" : ".billing-list-mobile");
      for (const amount of ["原扣款 $52.58（退款另列）", "退款 $52.58", "未扣款"]) {
        await surface.getByText(amount, { exact: true }).waitFor({ state: "visible" });
      }
      assert.equal(await surface.getByText("扣款 $52.58", { exact: true }).count(), 0);
      assert.equal(await surface.getByText("开通未完成，已结案", { exact: true }).count(), 2);
      for (const [amount, expected] of [
        ["原扣款 $52.58（退款另列）", [closed.operationId!, "工作空间编号：ws-closed", "2026/08/01 至 2026/09/01"]],
        ["退款 $52.58", [refund.operationId!, closed.operationId!, "原账户余额"]],
        ["未扣款", [uncharged.operationId!, "工作空间编号：ws-uncharged-closed", "未开始计费"]]
      ] as const) {
        const row = desktop
          ? surface.locator("tbody tr").filter({ hasText: amount })
          : surface.getByRole("listitem").filter({ hasText: amount });
        if (desktop) await row.getByRole("button", { name: "查看", exact: true }).click();
        else await row.click();
        await detailPanel(page).getByText(amount, { exact: true }).waitFor({ state: "visible" });
        for (const text of expected) await detailPanel(page).getByText(text, { exact: true }).waitFor({ state: "visible" });
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
        await page.getByRole("button", { name: "关闭收据详情", exact: true }).click();
      }
      assert.deepEqual(pageErrors, []);
      assert.deepEqual(externalRequests, []);
      await page.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

// settlementDay maps a day offset from the window's last day onto a fixed
// calendar so the rendered labels stay deterministic.
function settlementDay(offset: number): string {
  const date = new Date(Date.UTC(2026, 8, 18));
  date.setUTCDate(date.getUTCDate() - offset);
  return date.toISOString().slice(0, 10);
}

function settlementTrend(
  days: Array<Partial<WorkspaceSettlementTrend["days"][number]>>,
  overrides: Partial<WorkspaceSettlementTrend> = {}
): WorkspaceSettlementTrend {
  const filled = days.map((day, index) => ({
    date: day.date || settlementDay(days.length - 1 - index),
    chargedUsdMicros: day.chargedUsdMicros ?? 0,
    refundedUsdMicros: day.refundedUsdMicros ?? 0,
    netUsdMicros: (day.chargedUsdMicros ?? 0) - (day.refundedUsdMicros ?? 0),
    chargeCount: day.chargeCount ?? (day.chargedUsdMicros ? 1 : 0),
    refundCount: day.refundCount ?? (day.refundedUsdMicros ? 1 : 0)
  }));
  const chargedUsdMicros = filled.reduce((total, day) => total + day.chargedUsdMicros, 0);
  const refundedUsdMicros = filled.reduce((total, day) => total + day.refundedUsdMicros, 0);
  return {
    timezone: "Asia/Shanghai",
    asOf: fetchedAt,
    windowStart: `${filled[0].date}T00:00:00+08:00`,
    windowEnd: `${settlementDay(-1)}T00:00:00+08:00`,
    days: filled,
    chargedUsdMicros,
    refundedUsdMicros,
    netUsdMicros: chargedUsdMicros - refundedUsdMicros,
    settledCount: filled.filter((day) => day.chargeCount + day.refundCount > 0).length,
    inFlightCount: 0,
    unconfirmedCount: 0,
    unattributedCount: 0,
    outOfWindowCount: 0,
    complete: true,
    ...overrides
  };
}

function settlementSource(trend: WorkspaceSettlementTrend): SourceEnvelope<WorkspaceSettlementTrend> {
  return { source: "control_plane", status: "available", available: true, fetchedAt, data: trend };
}

function fourteenDays(): Array<Partial<WorkspaceSettlementTrend["days"][number]>> {
  return Array.from({ length: 14 }, () => ({}));
}

test("Overview trend counts the owner's confirmed Workspace charges", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/billing/workspace-settlements", (route) => fulfill(route, settlementSource(settlementTrend(
      fourteenDays().map((day, index) => (index === 11 ? { chargedUsdMicros: 52_580_000 } : day))
    ))));

    await login(page, demo.origin);
    const trend = page.locator(".overview-trend");
    await trend.getByText("近 14 天扣款 $52.58、退款 $0.00，净额 $52.58", { exact: false }).waitFor({ state: "visible" });

    // 该卡是工作空间钱包净扣款，不是 API 用量
    await trend.getByRole("heading", { name: "工作空间净扣款趋势", exact: true }).waitFor({ state: "visible" });
    assert.equal(await trend.getByRole("link", { name: "查看用量", exact: true }).count(), 0);
    assert.equal(await trend.getByText("API", { exact: false }).count(), 0);
    assert.equal(await trend.locator(".trend-chart").getAttribute("aria-label"), "工作空间近 14 天每日净扣款条形图，最高 $52.58");
    assert.equal(await trend.locator(".trend-col").count(), 14);
    assert.equal(await trend.locator('.trend-bar[data-empty="false"]').count(), 1);
    assert.equal(await trend.getByText("暂无数据", { exact: true }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Overview trend keeps a failed renewal charge and its full refund distinct", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    // 已确认扣款 $52.58 后全额退款：只有一张退款回执，但资金发生两次。
    await page.route("**/api/billing/workspace-settlements", (route) => fulfill(route, settlementSource(settlementTrend(
      fourteenDays().map((day, index) => (index === 13 ? { chargedUsdMicros: 52_580_000, refundedUsdMicros: 52_580_000 } : day))
    ))));

    await login(page, demo.origin);
    const trend = page.locator(".overview-trend");
    await trend.getByText("近 14 天扣款 $52.58、退款 $52.58，净额 $0.00", { exact: false }).waitFor({ state: "visible" });
    await trend.getByText("另有 0 笔", { exact: false }).waitFor({ state: "hidden" }).catch(() => {});

    assert.equal(await trend.getByText("扣款 $0.00", { exact: false }).count(), 0);
    // 同日扣退净额为 0：净额条形按零显示，但汇总与当日明细仍分别给出扣款与退款。
    assert.equal(await trend.locator('.trend-bar[data-empty="true"]').count(), 14);
    assert.equal(await trend.locator('.trend-bar[data-refund="true"]').count(), 0);
    const dayTitles = await trend.locator(".trend-col").evaluateAll((nodes) => nodes.map((node) => node.getAttribute("title") || ""));
    assert.ok(dayTitles[13].includes("扣款 $52.58"), dayTitles[13]);
    assert.ok(dayTitles[13].includes("退款 $52.58"), dayTitles[13]);
    assert.ok(dayTitles[13].includes("净额 $0.00"), dayTitles[13]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Overview trend renders the owner's days and never re-buckets them in the browser", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    // 服务端给出的窗口与浏览器本地日历不同（7 月），页面必须原样呈现。
    const days = fourteenDays().map((day, index) => ({
      date: `2026-07-${String(5 + index).padStart(2, "0")}`,
      chargedUsdMicros: index === 3 ? 10_000_000 : 0,
      refundedUsdMicros: index === 12 ? 3_000_000 : 0
    }));
    await page.route("**/api/billing/workspace-settlements", (route) => fulfill(route, settlementSource(settlementTrend(days, { outOfWindowCount: 1 }))));

    await login(page, demo.origin);
    const trend = page.locator(".overview-trend");
    await trend.getByText("近 14 天扣款 $10.00、退款 $3.00，净额 $7.00", { exact: false }).waitFor({ state: "visible" });

    const titles = await trend.locator(".trend-col").evaluateAll((nodes) => nodes.map((node) => node.getAttribute("title") || ""));
    assert.equal(titles.length, 14);
    assert.ok(titles[0].startsWith("7/5 "), titles[0]);
    assert.ok(titles[13].startsWith("7/18 "), titles[13]);
    assert.ok(titles[3].includes("扣款 $10.00"), titles[3]);
    assert.ok(titles[12].includes("退款 $3.00"), titles[12]);
    assert.equal(await trend.locator('.trend-bar[data-refund="true"]').count(), 1);
    await trend.getByText("另有 1 笔已确认的资金变动发生在这 14 天之前，未计入上述汇总。", { exact: true }).waitFor({ state: "visible" });
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Overview trend separates unavailable, unconfirmed, in-flight and real zero", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    let mode: "unavailable" | "unconfirmed" | "in-flight" | "zero" = "unavailable";
    await page.route("**/api/billing/workspace-settlements", async (route) => {
      if (mode === "unavailable") {
        await route.fulfill({ status: 502, contentType: "application/json", body: JSON.stringify({ source: "control_plane", status: "unavailable", available: false, fetchedAt, reasonCode: "control_plane_unavailable" }) });
        return;
      }
      if (mode === "unconfirmed") {
        await fulfill(route, settlementSource(settlementTrend(fourteenDays(), { unconfirmedCount: 1, complete: false })));
        return;
      }
      if (mode === "in-flight") {
        await fulfill(route, settlementSource(settlementTrend(fourteenDays(), { inFlightCount: 1 })));
        return;
      }
      await fulfill(route, settlementSource(settlementTrend(fourteenDays().map((day, index) => (index === 13 ? { chargedUsdMicros: 52_580_000, refundedUsdMicros: 52_580_000 } : day)))));
    });

    await login(page, demo.origin);
    const trend = page.locator(".overview-trend");
    await trend.getByText("工作空间扣款趋势暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await trend.locator(".trend-chart").count(), 0);
    assert.equal(await trend.getByText("无扣款", { exact: true }).count(), 0);

    mode = "unconfirmed";
    await trend.getByRole("button", { name: "重试", exact: true }).click();
    await trend.getByText("另有 1 笔资金变动暂不可确认，未计入上述汇总。", { exact: true }).waitFor({ state: "visible" });

    mode = "in-flight";
    await page.locator(".topbar").getByRole("button", { name: "刷新", exact: true }).click();
    await trend.getByText("另有 1 笔开通或续费仍在处理中，尚未产生资金变动。", { exact: true }).waitFor({ state: "visible" });

    mode = "zero";
    await page.locator(".topbar").getByRole("button", { name: "刷新", exact: true }).click();
    await trend.getByText("近 14 天扣款 $52.58、退款 $52.58，净额 $0.00", { exact: false }).waitFor({ state: "visible" });
    assert.equal(await trend.getByText("暂不可确认", { exact: false }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Overview trend reads only the owner projection, not receipts or paging", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const settlementRequests: string[] = [];
  const receiptRequests: string[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    // 「最近费用」列表只看到一张退款回执，而趋势仍须给出已确认的扣款。
    const refundReceipt: BillingReceipt = { ...receipt("refund-only"), type: "gateway.wallet_adjustment.v1", kind: "business_refund", status: "completed", refundUsdMicros: 3_000_000 };
    await page.route("**/api/billing/workspace-settlements", (route) => {
      settlementRequests.push(route.request().url());
      return fulfill(route, settlementSource(settlementTrend(fourteenDays().map((day, index) => (index === 13 ? { chargedUsdMicros: 52_580_000 } : day)))));
    });
    await page.route("**/api/billing/receipts?*", (route) => {
      receiptRequests.push(route.request().url());
      return fulfill(route, receiptSource([refundReceipt]));
    });

    await login(page, demo.origin);
    const trend = page.locator(".overview-trend");
    await trend.getByText("近 14 天扣款 $52.58、退款 $0.00，净额 $52.58", { exact: false }).waitFor({ state: "visible" });

    assert.deepEqual(settlementRequests.filter((url) => !url.includes("limit=")), settlementRequests);
    assert.equal(settlementRequests.length, 1);
    // 趋势只读取一次，且不借回执分页拼凑金额。
    for (const url of receiptRequests) {
      assert.ok(url.includes("limit=3"), url);
    }
    assert.equal(await trend.getByText("$3.00", { exact: false }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});
