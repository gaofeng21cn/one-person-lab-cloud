import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page } from "playwright";

import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const customerNavigation = ["概览", "工作空间", "API", "费用"];
const viewports = [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 390, height: 844 }
] as const;

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
      for (const value of ["acct-1", "user-customer", "9"]) {
        assert.equal(await account.getByText(value, { exact: true }).isVisible(), false, `${viewport.name}: ${value} should be disclosed`);
      }

      await technicalDetails.getByText("技术详情", { exact: true }).click();
      assert.notEqual(await technicalDetails.getAttribute("open"), null);
      for (const value of ["Account ID", "acct-1", "Console User ID", "user-customer", "Sub2API User ID", "9", "Session ID", "Session 到期"]) {
        await account.getByText(value, { exact: true }).waitFor({ state: "visible" });
      }

      assert.equal(supportRequests, 0);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
