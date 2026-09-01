import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Locator, type Page } from "playwright";

import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const customerNavigation = ["概览", "工作空间", "API", "费用"];
const viewports = [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 390, height: 844 }
] as const;

const customerData = [
  "Pilot Workspace",
  "Second Workspace",
  "这是 localhost 内存数据，用于查看公告、已读状态和 Console 信息层级。"
];

async function visibleOwnedText(page: Page) {
  return page.evaluate((allowedValues) => {
    const values = new Set(allowedValues);
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const visible: string[] = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const value = node.textContent?.trim() || "";
      const parent = node.parentElement;
      if (!value || !parent || values.has(value)) continue;
      const closedDetails = parent.closest("details:not([open])");
      if (closedDetails && !parent.closest("summary")) continue;
      const style = getComputedStyle(parent);
      if (style.display === "none" || style.visibility === "hidden") continue;
      if (parent.getClientRects().length === 0) continue;
      visible.push(value);
    }
    return visible.join("\n");
  }, customerData);
}

async function assertCustomerLanguage(page: Page, scope: string) {
  const text = await visibleOwnedText(page);
  for (const forbidden of [
    /\bWorkspace\b/,
    /API Keys?/,
    /API Endpoint/,
    /\bEndpoint\b/,
    /\bBilling\b/,
    /\bAnnouncements\b/,
    /\bRuntime\b/,
    /Control Plane/,
    /原因代码/,
    /(?:control_plane|sub2api)_unavailable/,
    /最近账单/,
    /账单收据/,
    /公告列表/
  ]) {
    assert.doesNotMatch(text, forbidden, `${scope}: ${forbidden} should not be customer-visible`);
  }
}

async function assertNoHorizontalOverflow(page: Page, scope: string) {
  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth
  }));
  assert.ok(
    dimensions.scrollWidth <= dimensions.clientWidth,
    `${scope}: horizontal overflow ${dimensions.scrollWidth} > ${dimensions.clientWidth}`
  );
}

async function visibleExactTextCount(root: Page | Locator, value: string) {
  const matches = root.getByText(value, { exact: true });
  const count = await matches.count();
  assert.ok(count > 0, `${value} should exist in technical disclosure`);
  let visible = 0;
  for (let index = 0; index < count; index += 1) {
    if (await matches.nth(index).isVisible()) visible += 1;
  }
  return visible;
}

async function assertExactTextHidden(root: Page | Locator, values: string[], scope: string) {
  for (const value of values) {
    assert.equal(
      await visibleExactTextCount(root, value),
      0,
      `${scope}: ${value} should remain behind technical disclosure`
    );
  }
}

async function openCustomerPath(page: Page, origin: string, path: string, selector: string) {
  await page.goto(`${origin}${path}`, { waitUntil: "domcontentloaded" });
  await page.locator(selector).waitFor({ state: "visible" });
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
  await page.reload({ waitUntil: "domcontentloaded" });
}

test("customer shell exposes four tasks and keeps account internals disclosed", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of viewports) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      let supportRequests = 0;
      page.on("request", (request) => {
        if (new URL(request.url()).pathname === "/api/support/tickets") supportRequests += 1;
      });

      await login(page, demo.origin);

      const navigation = viewport.name === "desktop"
        ? page.locator(".side-nav")
        : page.getByRole("navigation", { name: "移动端导航" });
      const links = viewport.name === "desktop"
        ? navigation.locator(":scope > a")
        : navigation.getByRole("link");
      await links.first().waitFor({ state: "visible" });
      assert.deepEqual((await links.allTextContents()).map((value) => value.trim()), customerNavigation);
      assert.deepEqual(await navigation.locator('[aria-current="page"]').allTextContents(), ["概览"]);
      assert.equal(await page.locator(".side-subnav").count(), 0);

      await navigation.getByRole("link", { name: "API", exact: true }).click();
      await page.waitForURL(/\/console\/api$/);
      assert.deepEqual(await navigation.locator('[aria-current="page"]').allTextContents(), ["API"]);

      const message = page.locator(".topbar-actions").getByRole("button", { name: "消息", exact: true });
      await message.click();
      await page.waitForURL(/\/console\/announcements$/);
      assert.equal(await navigation.locator('[aria-current="page"]').count(), 0);

      assert.equal(await page.getByRole("button", { name: "Support", exact: true }).count(), 0);
      assert.equal(await page.getByRole("complementary", { name: "Support", exact: true }).count(), 0);
      assert.equal(await page.getByText("Account Settings", { exact: true }).count(), 0);

      const accountCommand = viewport.name === "desktop"
        ? page.locator(".topbar-actions").getByRole("button", { name: "账号信息", exact: true })
        : page.locator(".sidebar").getByRole("button", { name: "账号信息", exact: true });
      if (viewport.name === "mobile") {
        await page.getByRole("button", { name: "打开导航", exact: true }).click();
      }
      await accountCommand.click();

      const account = page.getByRole("complementary", { name: "账号信息", exact: true });
      await account.getByRole("heading", { name: "账号信息", exact: true }).waitFor({ state: "visible" });
      await account.getByText(CONSOLE_DEMO_CREDENTIALS.customer.email, { exact: true }).waitFor({ state: "visible" });
      await account.getByText("身份", { exact: true }).waitFor({ state: "visible" });
      await account.getByText("客户", { exact: true }).waitFor({ state: "visible" });
      await account.getByText("账户状态", { exact: true }).waitFor({ state: "visible" });
      await account.getByText("正常", { exact: true }).waitFor({ state: "visible" });
      await account.getByRole("button", { name: "退出登录", exact: true }).waitFor({ state: "visible" });

      const technicalDetails = account.locator("details");
      assert.equal(await technicalDetails.getAttribute("open"), null);
      await assertExactTextHidden(account, ["acct-1", "user-customer", "9"], `${viewport.name}: account`);

      await technicalDetails.getByText("技术详情", { exact: true }).click();
      assert.notEqual(await technicalDetails.getAttribute("open"), null);
      for (const value of ["Account ID", "acct-1", "Console User ID", "user-customer", "Sub2API User ID", "9", "Session ID", "Session 到期"]) {
        await account.getByText(value, { exact: true }).waitFor({ state: "visible" });
      }
      await assertNoHorizontalOverflow(page, `${viewport.name}: account technical details`);

      assert.equal(supportRequests, 0);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("customer task pages use customer language and disclose technical evidence", { timeout: 120_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of viewports) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      await login(page, demo.origin);

      await page.getByText("Pilot Workspace", { exact: true }).waitFor({ state: "visible" });
      await assertCustomerLanguage(page, `${viewport.name}: overview`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: overview`);

      await openCustomerPath(page, demo.origin, "/console/workspaces", ".workspace-list-page");
      await page.getByText("Pilot Workspace", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("Second Workspace", { exact: true }).waitFor({ state: "visible" });
      await page.getByRole("button", { name: "新建工作空间", exact: true }).waitFor({ state: "visible" });
      await assertCustomerLanguage(page, `${viewport.name}: workspaces`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: workspaces`);

      await openCustomerPath(page, demo.origin, "/console/api", ".api-overview");
      const apiNavigation = page.getByRole("navigation", { name: "API 服务导航" });
      assert.equal(await apiNavigation.count(), 1);
      assert.deepEqual(
        (await apiNavigation.getByRole("link").allTextContents()).map((value) => value.trim()),
        ["服务信息", "用量", "密钥"]
      );
      await page.getByText("https://gflabtoken.cn/v1", { exact: true }).waitFor({ state: "visible" });
      await assertCustomerLanguage(page, `${viewport.name}: API overview`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: API overview`);

      await openCustomerPath(page, demo.origin, "/console/api/usage", "[data-slide='C-API-02']");
      const usageSurface = viewport.name === "desktop"
        ? page.locator(".request-table-desktop")
        : page.locator(".request-list-mobile");
      await usageSurface.getByText("request-fixture", { exact: true }).waitFor({ state: "visible" });
      await assertCustomerLanguage(page, `${viewport.name}: API usage`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: API usage`);

      await openCustomerPath(page, demo.origin, "/console/api/keys", ".keys-panel");
      const keySurface = viewport.name === "desktop"
        ? page.locator(".keys-table tbody tr").filter({ hasText: "General fixture key" })
        : page.locator(".mobile-key-card").filter({ hasText: "General fixture key" });
      await keySurface.getByText("General fixture key", { exact: true }).waitFor({ state: "visible" });
      const sortControl = page.locator(".keys-filters .console-field")
        .filter({ has: page.getByText("排序", { exact: true }) })
        .locator(".console-select")
        .getByRole("button");
      await sortControl.click();
      assert.deepEqual(
        (await page.getByRole("option").allTextContents()).map((value) => value.trim()),
        ["创建时间", "名称", "过期时间", "状态", "最近使用"]
      );
      await page.keyboard.press("Escape");
      const keyTechnicalDetailsCopies = page.locator("details.key-technical-details");
      await assertExactTextHidden(keyTechnicalDetailsCopies, ["11", "openai", "101", "0"], `${viewport.name}: API keys`);
      await assertCustomerLanguage(page, `${viewport.name}: API keys`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: API keys`);

      const keyTechnicalDetails = keySurface.locator("details.key-technical-details");
      assert.equal(await keyTechnicalDetails.getAttribute("open"), null);
      await keyTechnicalDetails.getByText("技术详情", { exact: true }).click();
      await keyTechnicalDetails.getByText("11", { exact: true }).waitFor({ state: "visible" });
      await keyTechnicalDetails.getByText("openai", { exact: true }).waitFor({ state: "visible" });
      await keyTechnicalDetails.getByText("101", { exact: true }).waitFor({ state: "visible" });
      const concurrencyFact = keyTechnicalDetails.locator(".data-list > div").filter({ hasText: "current concurrency" });
      await concurrencyFact.locator("dd").getByText("0", { exact: true }).waitFor({ state: "visible" });
      await assertNoHorizontalOverflow(page, `${viewport.name}: API key technical details`);

      const billingReceiptsLoaded = page.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === "/api/billing/receipts"
          && url.searchParams.get("limit") === "20"
          && response.ok();
      });
      await openCustomerPath(page, demo.origin, "/console/billing", ".billing-page");
      await billingReceiptsLoaded;
      const billingSwitch = page.getByRole("radiogroup", { name: "费用视图" });
      assert.deepEqual(
        (await billingSwitch.getByRole("radio").allTextContents()).map((value) => value.trim()),
        ["订阅与续费", "账单记录"]
      );
      const subscriptionSurface = viewport.name === "desktop"
        ? page.locator(".billing-table-desktop")
        : page.locator(".billing-list-mobile");
      await subscriptionSurface.getByText("Pilot Workspace", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByText("Control Plane 当前商业条款", { exact: true }).count(), 0);
      await assertCustomerLanguage(page, `${viewport.name}: fees terms`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: fees terms`);

      await billingSwitch.getByRole("radio", { name: "账单记录", exact: true }).click();
      await page.getByRole("heading", { name: "账单记录", exact: true }).waitFor({ state: "visible" });
      const receiptListSurface = viewport.name === "desktop"
        ? page.locator(".billing-table-desktop")
        : page.locator(".billing-list-mobile");
      await receiptListSurface.getByText(
        viewport.name === "desktop" ? "Pilot Workspace" : "工作空间开通",
        { exact: true }
      ).waitFor({ state: "visible" });
      const receiptButton = viewport.name === "desktop"
        ? receiptListSurface.getByRole("button", { name: "查看", exact: true })
        : receiptListSurface.locator("button").first();
      await receiptButton.click();
      const receiptDetail = page.locator(".receipt-detail");
      await receiptDetail.getByText("工作空间开通", { exact: true }).waitFor({ state: "visible" });
      await receiptDetail.getByText("待确认", { exact: true }).waitFor({ state: "visible" });
      await receiptDetail.getByText("Pilot Workspace", { exact: true }).waitFor({ state: "visible" });
      await assertExactTextHidden(receiptDetail, ["receipt-fixture", "pilot-usd-2026-07-v1", "succeeded"], `${viewport.name}: fees receipts`);
      await assertCustomerLanguage(page, `${viewport.name}: fees receipts`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: fees receipts`);

      const receiptTechnicalDetails = receiptDetail.locator("details.receipt-technical-details");
      await receiptTechnicalDetails.getByText("技术详情", { exact: true }).click();
      await receiptTechnicalDetails.getByText("receipt-fixture", { exact: true }).waitFor({ state: "visible" });
      await receiptTechnicalDetails.getByText("succeeded", { exact: true }).waitFor({ state: "visible" });
      await receiptTechnicalDetails.getByText("pilot-usd-2026-07-v1", { exact: true }).waitFor({ state: "visible" });
      await assertNoHorizontalOverflow(page, `${viewport.name}: receipt technical details`);

      await openCustomerPath(page, demo.origin, "/console/announcements", ".announcements-page");
      await page.getByRole("heading", { name: "消息列表", exact: true }).waitFor({ state: "visible" });
      await page.getByText(customerData[2], { exact: true }).waitFor({ state: "visible" });
      await assertCustomerLanguage(page, `${viewport.name}: messages`);
      await assertNoHorizontalOverflow(page, `${viewport.name}: messages`);

      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
