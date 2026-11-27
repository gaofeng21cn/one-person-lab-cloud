import assert from "node:assert/strict";
import test from "node:test";

import type { Page } from "playwright";

import { launchBrowser } from "../../tools/launch-browser.ts";
import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";

const fetchedAt = "2026-11-27T12:00:00Z";

interface CloudStubOptions {
  tenantId?: string;
  loginReject?: boolean;
  workspaceMode?: "forbidden" | "unavailable";
  legacyBillingMode?: "not_found" | "unavailable" | "lineage_conflict";
  onRestrictedCall?: (path: string) => void;
}

async function installCloudRoutes(page: Page, origin: string, options: CloudStubOptions = {}) {
  const {
    tenantId,
    loginReject = false,
    workspaceMode = "forbidden",
    legacyBillingMode = "not_found",
    onRestrictedCall
  } = options;
  let signedIn = false;

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin !== origin) {
      await route.abort("blockedbyclient");
      return;
    }
    if (url.pathname === "/@vite/client") {
      await route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
      return;
    }
    if (!url.pathname.startsWith("/api/v2/")) {
      await route.continue();
      return;
    }

    const json = (body: unknown, status = 200) => route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body)
    });

    const sessionPayload = {
      actorId: "120",
      ...(tenantId ? { tenantId } : {}),
      displayName: CONSOLE_DEMO_CREDENTIALS.customer.email,
      ...(tenantId ? { role: "member" } : {}),
      permissions: [],
      csrfToken: "csrf-cloud-test",
      expiresAt: "2099-01-01T00:00:00Z"
    };

    if (url.pathname === "/api/v2/auth/context") return json({ csrfToken: "csrf-login", expiresAt: "2099-01-01T00:00:00Z" });
    if (url.pathname === "/api/v2/auth/login") {
      if (loginReject) {
        return json({ code: "UNAUTHENTICATED", message: "invalid credentials", requestId: "req-login" }, 401);
      }
      signedIn = true;
      return json(sessionPayload);
    }
    if (url.pathname === "/api/v2/auth/session") {
      if (!signedIn) return json({ code: "UNAUTHENTICATED", message: "session required", requestId: "req-session" }, 401);
      return json(sessionPayload);
    }
    if (url.pathname === "/api/v2/auth/logout") {
      signedIn = false;
      return json({}, 204);
    }

    if (url.pathname === "/api/v2/wallet") {
      return json({
        source: "gateway",
        status: "available",
        currency: "USD",
        balanceUSDMicros: "54097710000",
        fetchedAt
      });
    }
    if (url.pathname === "/api/v2/usage/summary") {
      return json({
        totalRequests: "221566",
        totalInputTokens: "120000",
        totalOutputTokens: "80000",
        totalTokens: "200000",
        totalActualCostUSDMicros: "27211730000"
      });
    }
    if (url.pathname === "/api/v2/announcements") {
      const pageNum = Number(url.searchParams.get("page") || "1");
      const pageSize = Number(url.searchParams.get("pageSize") || "20");
      return json({
        source: "control-plane",
        status: "empty",
        available: true,
        fetchedAt,
        data: { items: [], total: 0, page: pageNum, pageSize }
      });
    }

    if (url.pathname.startsWith("/api/v2/legacy-billing/")) {
      if (legacyBillingMode === "not_found") {
        return json({ code: "NOT_FOUND", message: "legacy_billing_account_not_found", requestId: "req-legacy" }, 404);
      }
      if (legacyBillingMode === "lineage_conflict") {
        return json({ code: "VALIDATION_FAILED", message: "legacy_billing_identity_lineage_invalid", requestId: "req-lineage" }, 422);
      }
      return json({ code: "DEPENDENCY_UNAVAILABLE", message: "publisher request could not be completed", requestId: "req-503" }, 503);
    }

    if (url.pathname.startsWith("/api/v2/workspaces")
      || url.pathname.startsWith("/api/v2/packages")
      || url.pathname.startsWith("/api/v2/namespaces")
      || url.pathname.startsWith("/api/v2/catalog/compute-plans")) {
      onRestrictedCall?.(url.pathname);
      if (workspaceMode === "unavailable") {
        return json({ code: "DEPENDENCY_UNAVAILABLE", message: "publisher request could not be completed", requestId: "req-ws-503" }, 503);
      }
      return json({ code: "FORBIDDEN", message: "action is not authorized", requestId: "req-ws-403" }, 403);
    }

    return json({ code: "NOT_FOUND", message: `unexpected ${url.pathname}`, requestId: "req-unexpected" }, 404);
  });
}

async function loginOnPage(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "networkidle" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

test("PublicHome and split LoginPage enforce generic OPL Cloud UX, accessibility, and auth error mapping", { timeout: 60_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
      const page = await browser.newPage({ viewport });
      const pageErrors: string[] = [];
      page.on("pageerror", (error) => pageErrors.push(error.message));
      await installCloudRoutes(page, server.origin, { loginReject: true });

      await page.goto(`${server.origin}/`, { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: "OPL Cloud", exact: true }).waitFor({ state: "visible" });
      await page.getByRole("button", { name: "登录", exact: true }).waitFor({ state: "visible" });
      const homeText = (await page.evaluate(() => document.body.innerText)).toLowerCase();
      assert.equal(homeText.includes("medopl"), false, "PublicHome must remain instance-neutral");
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false, "PublicHome must not overflow horizontally");

      await page.getByRole("button", { name: "登录", exact: true }).click();
      await page.waitForURL(/\/login$/);
      await page.getByRole("heading", { name: "登录 OPL Cloud", exact: true }).waitFor({ state: "visible" });

      if (viewport.width >= 960) {
        assert.equal(await page.locator(".login-showcase").isVisible(), true);
        assert.equal(await page.locator(".login-mobile-bar").isVisible(), false);
      } else {
        assert.equal(await page.locator(".login-showcase").isVisible(), false);
        assert.equal(await page.locator(".login-mobile-bar").isVisible(), true);
      }

      const email = page.getByLabel("邮箱");
      const password = page.getByLabel("密码");
      assert.equal(await email.getAttribute("type"), "email");
      assert.equal(await email.getAttribute("autocomplete"), "username");
      assert.equal(await password.getAttribute("type"), "password");
      assert.equal(await password.getAttribute("autocomplete"), "current-password");

      await password.fill("secret-pass");
      await page.getByRole("button", { name: "显示明文", exact: true }).click();
      assert.equal(await password.getAttribute("type"), "text");
      const hideToggle = page.getByRole("button", { name: "隐藏明文", exact: true });
      assert.equal(await hideToggle.getAttribute("aria-pressed"), "true");
      await hideToggle.click();
      assert.equal(await password.getAttribute("type"), "password");

      await email.fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
      await page.getByRole("button", { name: "登录", exact: true }).click();
      await page.getByRole("alert").getByText("邮箱或密码错误，请检查后重试", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false, "LoginPage must not overflow horizontally");
      assert.deepEqual(pageErrors, []);
      await page.close();
    }
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});

test("Non-whitelisted CloudIdentity users see calm notices instead of unavailable retry alerts", { timeout: 90_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const restrictedCalls: string[] = [];
      const page = await browser.newPage({ viewport });
      const pageErrors: string[] = [];
      page.on("pageerror", (error) => pageErrors.push(error.message));
      await installCloudRoutes(page, server.origin, {
        legacyBillingMode: "not_found",
        onRestrictedCall: (path) => restrictedCalls.push(path)
      });

      await loginOnPage(page, server.origin);
      await page.getByText("$54,097.71", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("$27,211.73", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("221,566", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("暂无消息", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("工作空间尚未开通", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("无历史工作空间扣款记录", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("无历史费用记录", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.locator(".overview-summary").getByText("未开通", { exact: true }).count(), 1);
      assert.equal(await page.locator('[role="alert"]').count(), 0);
      assert.equal(await page.getByText("暂不可用", { exact: false }).count(), 0);
      assert.equal(await page.getByText("请稍后重试", { exact: false }).count(), 0);
      assert.equal(await page.getByRole("button", { name: "重试", exact: true }).count(), 0);
      assert.equal(await page.getByRole("button", { name: "重试读取工作空间" }).count(), 0);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);

      // Workspace list, Launch, Agents, Publisher, and both Billing tabs must all stay calm with zero alerts.
      for (const routePath of ["/console/workspaces", "/console/workspaces/new", "/console/agents", "/console/publisher"]) {
        await page.goto(`${server.origin}${routePath}`, { waitUntil: "networkidle" });
        await page.locator('.source-notice[data-notice-kind="permission"]').waitFor({ state: "visible" });
        assert.equal(await page.locator('[role="alert"]').count(), 0, `${routePath} must not show alerts`);
        assert.equal(await page.getByText("读取失败", { exact: true }).count(), 0, `${routePath} must not show 读取失败`);
        assert.equal(await page.getByText("无法继续", { exact: true }).count(), 0, `${routePath} must not show 无法继续`);
        assert.equal(await page.getByText("暂不可用", { exact: false }).count(), 0, `${routePath} must not say 暂不可用`);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false, `${routePath} overflow`);
      }

      await page.goto(`${server.origin}/console/billing`, { waitUntil: "networkidle" });
      await page.getByText("工作空间订阅尚未开通", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.locator('[role="alert"]').count(), 0);
      assert.equal(await page.getByText("暂不可用", { exact: false }).count(), 0);
      await page.getByRole("radio", { name: "个人历史账单", exact: true }).click();
      await page.getByText("无历史账单记录", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.locator('[role="alert"]').count(), 0);
      assert.equal(await page.getByText("暂不可用", { exact: false }).count(), 0);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);

      assert.deepEqual(restrictedCalls, [], "CloudIdentity session without tenantId must not send doomed 403 workspace/package requests");
      assert.deepEqual(pageErrors, []);
      await page.close();
    }
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});

test("Real upstream failures and role denials remain explicit and fail-closed for whitelisted tenants", { timeout: 60_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    // Case 1: Real 503 outage stays unavailable with retry buttons
    const outagePage = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await installCloudRoutes(outagePage, server.origin, {
      tenantId: "tenant-1",
      workspaceMode: "unavailable",
      legacyBillingMode: "unavailable"
    });
    await loginOnPage(outagePage, server.origin);
    await outagePage.locator(".overview-workspaces").getByText("暂时无法读取工作空间，请稍后重试。", { exact: true }).waitFor({ state: "visible" });
    await outagePage.getByText("工作空间扣款趋势暂不可用", { exact: true }).waitFor({ state: "visible" });
    await outagePage.getByText("费用记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    await outagePage.getByRole("button", { name: "重试读取工作空间" }).waitFor({ state: "visible" });
    assert.equal(await outagePage.getByRole("button", { name: "重试", exact: true }).count(), 3);
    await outagePage.close();

    // Case 2: 403 forbidden role vs 422 legacy lineage conflict
    const mixedPage = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await installCloudRoutes(mixedPage, server.origin, {
      tenantId: "tenant-1",
      workspaceMode: "forbidden",
      legacyBillingMode: "lineage_conflict"
    });
    await loginOnPage(mixedPage, server.origin);
    await mixedPage.getByText("无权查看工作空间", { exact: true }).waitFor({ state: "visible" });
    await mixedPage.getByText("费用记录暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await mixedPage.getByRole("button", { name: "重试读取工作空间" }).count(), 0);
    await mixedPage.close();
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
