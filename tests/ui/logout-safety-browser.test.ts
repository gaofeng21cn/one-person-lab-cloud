import assert from "node:assert/strict";
import test from "node:test";

import { chromium } from "playwright";

import type {
  RuntimeCredentialResponse,
  SourceEnvelope,
  WorkspaceListData
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

test("Console hides secrets and rejects late account data while logout is unconfirmed", async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await context.newPage();
    await page.goto(`${demo.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });

    const passwordRow = page.locator(".workspace-access-panel .data-list > div").filter({ hasText: "登录密码" }).first();
    await passwordRow.getByRole("button", { name: "显示", exact: true }).click();
    await passwordRow.locator("code").waitFor({ state: "visible" });
    await page.waitForFunction(() => {
      const rows = [...document.querySelectorAll(".workspace-access-panel .data-list > div")];
      const row = rows.find((candidate) => candidate.textContent?.includes("登录密码"));
      const value = row?.querySelector("code")?.textContent || "";
      return Boolean(value) && !value.includes("••");
    });
    const password = String(await passwordRow.locator("code").textContent());
    assert.ok(password && !password.includes("••"));
    const workspaceKeyRow = page.locator(".workspace-access-panel .data-list > div").filter({ hasText: "API 密钥" }).first();
    await workspaceKeyRow.getByRole("button", { name: "显示", exact: true }).click();
    await workspaceKeyRow.locator("code").waitFor({ state: "visible" });
    await page.waitForFunction(() => {
      const rows = [...document.querySelectorAll(".workspace-access-panel .data-list > div")];
      const row = rows.find((candidate) => candidate.textContent?.includes("API 密钥"));
      const value = row?.querySelector("code")?.textContent || "";
      return Boolean(value) && !value.includes("••");
    });
    const workspaceKey = String(await workspaceKeyRow.locator("code").textContent());
    assert.ok(workspaceKey && !workspaceKey.includes("••"));

    const lateWorkspace: SourceEnvelope<WorkspaceListData> = {
      source: "control-plane",
      status: "available",
      available: true,
      fetchedAt: "2026-08-27T00:00:00Z",
      data: {
        items: [{
          id: "ws-1",
          ownerAccountId: "LATE-ACCOUNT-RESULT",
          ownerUserId: "user-customer",
          state: "running",
          createdAt: "2026-07-01T00:00:00Z",
          updatedAt: "2026-08-27T00:00:00Z",
          name: "LATE-ACCOUNT-RESULT / LATE-WORKSPACE-RESULT",
          url: "https://workspace.example.invalid/w/ws-1/",
          packageId: "basic",
          storageGb: 10,
          autoRenew: false,
          priceVersion: "pilot-usd-2026-07-v1",
          currency: "USD",
          totalUsdMicros: 52_580_000,
          periodStart: "2026-07-01T00:00:00Z",
          paidThrough: "2026-08-01T00:00:00Z",
          renewalStatus: "manual",
          workspaceApiKeyId: "9"
        }],
        total: 1,
        page: 1,
        pageSize: 50
      }
    };
    let releaseWorkspace: (() => void) | undefined;
    let holdInitialWorkspace = true;
    const workspaceReleased = new Promise<void>((resolve) => { releaseWorkspace = resolve; });
    let holdWorkspace: ((route: import("playwright").Route) => void) | undefined;
    const workspaceHeld = new Promise<import("playwright").Route>((resolve) => { holdWorkspace = resolve; });
    await page.route("**/api/workspaces?*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (!holdInitialWorkspace || request.method() !== "GET" || url.searchParams.get("page") !== "1" || url.searchParams.get("pageSize") !== "50") {
        await route.continue();
        return;
      }
      holdInitialWorkspace = false;
      holdWorkspace?.(route);
      await workspaceReleased;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(lateWorkspace)
      });
    });
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await workspaceHeld;

    let releaseLogout: (() => void) | undefined;
    const logoutReleased = new Promise<void>((resolve) => { releaseLogout = resolve; });
    let holdLogout: ((route: import("playwright").Route) => void) | undefined;
    const logoutHeld = new Promise<import("playwright").Route>((resolve) => { holdLogout = resolve; });
    await page.route("**/api/auth/logout", async (route) => {
      holdLogout?.(route);
      await logoutReleased;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "state_persist_failed" })
      });
    });
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await logoutHeld;
    await page.getByRole("heading", { name: "正在安全退出", exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByText(password, { exact: true }).count(), 0);
    assert.equal(await page.getByText(workspaceKey, { exact: true }).count(), 0);
    assert.equal(await page.getByText(CONSOLE_DEMO_CREDENTIALS.customer.email, { exact: true }).count(), 0);
    assert.equal(await page.getByText("Pilot Workspace", { exact: true }).count(), 0);

    releaseLogout?.();
    await page.getByRole("heading", { name: "退出未确认", exact: true }).waitFor({ state: "visible" });

    assert.match(page.url(), /\/console\/workspaces\/ws-1$/);
    assert.equal(await page.getByText(password, { exact: true }).count(), 0);
    assert.equal(await page.getByText(workspaceKey, { exact: true }).count(), 0);
    assert.equal(await page.getByText(CONSOLE_DEMO_CREDENTIALS.customer.email, { exact: true }).count(), 0);
    assert.equal(await page.getByText("Pilot Workspace", { exact: true }).count(), 0);

    const lateWorkspaceResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === "GET"
        && url.pathname === "/api/workspaces"
        && url.searchParams.get("page") === "1"
        && url.searchParams.get("pageSize") === "50";
    });
    releaseWorkspace?.();
    await (await lateWorkspaceResponse).finished();
    await page.evaluate(() => new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    }));
    await page.getByRole("button", { name: "重试退出", exact: true }).waitFor({ state: "visible" });

    const verificationPage = await context.newPage();
    try {
      await verificationPage.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });
      await verificationPage.getByRole("heading", { name: "Pilot Workspace", exact: true }).waitFor({ state: "visible" });
      assert.equal(await verificationPage.getByText("LATE-ACCOUNT-RESULT", { exact: false }).count(), 0);
      assert.equal(await verificationPage.getByText("LATE-WORKSPACE-RESULT", { exact: false }).count(), 0);
    } finally {
      await verificationPage.close();
    }

    await page.unroute("**/api/auth/logout");
    await page.getByRole("button", { name: "重试退出", exact: true }).click();
    await page.waitForURL(new RegExp(`${demo.origin.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}/?$`));
    assert.equal(await page.getByRole("heading", { name: "退出未确认", exact: true }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("a late login response cannot restore an account or cancel logout", async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.addInitScript(() => {
      const originalFetch = window.fetch.bind(window);
      let holdFirstLogin = true;
      let releaseFirstLogin: (() => void) | undefined;
      Object.assign(window, {
        releaseFirstLogin: () => releaseFirstLogin?.()
      });
      window.fetch = (input, init) => {
        const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
        if (holdFirstLogin && new URL(url, window.location.origin).pathname === "/api/auth/login") {
          holdFirstLogin = false;
          return new Promise<Response>((resolve) => {
            releaseFirstLogin = () => resolve(new Response(JSON.stringify({
              user: {
                id: "user-late", accountId: "acct-late", email: "late@example.com", role: "owner", status: "active"
              },
              isOperator: false,
              csrfToken: "csrf-late"
            }), { status: 200, headers: { "content-type": "application/json" } }));
          });
        }
        return originalFetch(input, init);
      };
    });

    await page.goto(`${demo.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();

    await page.getByRole("button", { name: "返回", exact: true }).click();
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.admin.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.admin.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.getByRole("link", { name: "API", exact: true }).click();
    await page.getByText("余额历史", { exact: true }).waitFor({ state: "visible" });
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await page.waitForURL(`${demo.origin}/`);

    await page.evaluate(() => (window as Window & { releaseFirstLogin?: () => void }).releaseFirstLogin?.());
    await page.evaluate(() => new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    }));
    assert.equal(page.url(), `${demo.origin}/`);
    assert.equal(await page.getByText("late@example.com", { exact: true }).count(), 0);
    assert.equal(await page.getByText(CONSOLE_DEMO_CREDENTIALS.admin.email, { exact: true }).count(), 0);
    await page.getByRole("button", { name: "登录", exact: true }).waitFor({ state: "visible" });
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace Secret controller rejects late reveal and refreshes after rotation", async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.goto(`${demo.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });

    let releaseReveal: (() => void) | undefined;
    const revealReleased = new Promise<void>((resolve) => { releaseReveal = resolve; });
    let holdReveal: ((route: import("playwright").Route) => void) | undefined;
    const revealHeld = new Promise<import("playwright").Route>((resolve) => { holdReveal = resolve; });
    const lateResponse: RuntimeCredentialResponse = {
      workspaceId: "ws-1",
      access: {
        account: "owner",
        username: "owner",
        password: "late-workspace-secret",
        credentialStatus: "active",
        credentialVersion: "late"
      }
    };
    await page.route("**/api/workspaces/ws-1/runtime-credentials/reveal", async (route) => {
      holdReveal?.(route);
      await revealReleased;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(lateResponse)
      });
    });

    const passwordRow = page.locator(".workspace-access-panel .data-list > div").filter({ hasText: "登录密码" }).first();
    await passwordRow.getByRole("button", { name: "显示", exact: true }).click();
    await revealHeld;
    await page.getByRole("button", { name: "工作空间列表", exact: true }).click();
    await page.waitForURL(/\/console\/workspaces$/);

    const lateRevealResponse = page.waitForResponse((response) => response.request().method() === "POST"
      && new URL(response.url()).pathname === "/api/workspaces/ws-1/runtime-credentials/reveal");
    releaseReveal?.();
    await (await lateRevealResponse).finished();

    await page.getByRole("link", { name: /Pilot Workspace/ }).first().click();
    await page.waitForURL(/\/console\/workspaces\/ws-1$/);
    const hiddenPasswordRow = page.locator(".workspace-access-panel .data-list > div").filter({ hasText: "登录密码" }).first();
    await hiddenPasswordRow.locator("code").waitFor({ state: "visible" });
    assert.match(String(await hiddenPasswordRow.locator("code").textContent()), /••/);
    assert.equal(await page.getByText(lateResponse.access.password, { exact: true }).count(), 0);
    await page.unroute("**/api/workspaces/ws-1/runtime-credentials/reveal");
    const currentPasswordRow = page.locator(".workspace-access-panel .data-list > div").filter({ hasText: "登录密码" }).first();
    await currentPasswordRow.getByRole("button", { name: "显示", exact: true }).click();
    await page.waitForFunction(() => {
      const rows = [...document.querySelectorAll(".workspace-access-panel .data-list > div")];
      const row = rows.find((candidate) => candidate.textContent?.includes("登录密码"));
      const value = row?.querySelector("code")?.textContent || "";
      return Boolean(value) && !value.includes("••");
    });
    const currentPassword = String(await currentPasswordRow.locator("code").textContent());
    assert.ok(currentPassword && !currentPassword.includes("••"));
    assert.notEqual(currentPassword, lateResponse.access.password);

    const rotationDetailReadback = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return url.pathname === "/api/workspaces" && url.searchParams.get("pageSize") === "50";
    });
    const rotationRuntimeReadback = page.waitForRequest((request) => {
      return request.method() === "GET"
        && new URL(request.url()).pathname === "/api/workspaces/ws-1/runtime-status";
    });
    const rotationBudgetReadback = page.waitForRequest((request) => {
      return request.method() === "GET"
        && new URL(request.url()).pathname === "/api/workspaces/ws-1/gateway-budget";
    });
    await page.getByRole("button", { name: "轮换密码", exact: true }).click();
    await page.getByText("登录密码已轮换", { exact: true }).waitFor({ state: "visible" });
    await Promise.all([rotationDetailReadback, rotationRuntimeReadback, rotationBudgetReadback]);
    await page.waitForFunction((previousPassword) => {
      const rows = [...document.querySelectorAll(".workspace-access-panel .data-list > div")];
      const row = rows.find((candidate) => candidate.textContent?.includes("登录密码"));
      const value = row?.querySelector("code")?.textContent || "";
      return Boolean(value) && !value.includes("••") && value !== previousPassword;
    }, currentPassword);
  } finally {
    await browser.close();
    await demo.close();
  }
});
