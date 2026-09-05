import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceListData,
  WorkspaceRenewalResponse
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-26T00:00:00Z";

function budgetSource(data: WorkspaceGatewayBudgetDTO): SourceEnvelope<WorkspaceGatewayBudgetDTO> {
  return { source: "sub2api", status: "available", available: true, fetchedAt, data };
}

function workspaceSource(workspace: WorkspaceDTO, page: number, pageSize: number): SourceEnvelope<WorkspaceListData> {
  return {
    source: "control-plane",
    status: "available",
    available: true,
    fetchedAt,
    data: { items: [workspace], total: 1, page, pageSize }
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

async function openAdvancedSettings(page: Page) {
  const details = page.locator("details.workspace-advanced-details");
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}

test("Workspace refresh rejects an older Budget completion and preserves the refreshed Key projection", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.goto(`${demo.origin}/login`, { waitUntil: "domcontentloaded" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await openAdvancedSettings(page);
    await page.getByRole("button", { name: "重置总额度用量", exact: true }).waitFor();

    const staleBudget: WorkspaceGatewayBudgetDTO = {
      workspaceId: "ws-1",
      keyId: "9",
      status: "active",
      quotaUsdMicros: "10000000",
      quotaUsedUsdMicros: "0",
      rateLimit5hUsdMicros: "0",
      rateLimit1dUsdMicros: "0",
      rateLimit7dUsdMicros: "0",
      usage5hUsdMicros: "0",
      usage1dUsdMicros: "10000",
      usage7dUsdMicros: "25000",
      enabled: true,
      updatedAt: fetchedAt
    };
    const refreshedBudget: WorkspaceGatewayBudgetDTO = {
      ...staleBudget,
      quotaUsedUsdMicros: "7770000",
      updatedAt: "2026-08-26T00:01:00Z"
    };

    const mutationHeld = deferred<Route>();
    const releaseMutation = deferred<void>();
    const requestOrder: string[] = [];
    let budgetReads = 0;
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname.startsWith("/api/workspaces")) {
        requestOrder.push(`${request.method()} ${url.pathname}${url.search}`);
      }
    });
    await page.route("**/api/workspaces/ws-1/gateway-budget", async (route) => {
      if (route.request().method() === "PATCH") {
        mutationHeld.resolve(route);
        await releaseMutation.promise;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(budgetSource(staleBudget))
        });
        return;
      }
      budgetReads += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(budgetSource(budgetReads === 1 ? refreshedBudget : staleBudget))
      });
    });

    await page.getByRole("button", { name: "重置总额度用量", exact: true }).click();
    const mutationRoute = await mutationHeld.promise;
    assert.equal(mutationRoute.request().method(), "PATCH");
    assert.deepEqual(mutationRoute.request().postDataJSON(), { resetQuota: true });

    const refreshedKeyRead = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === "GET"
        && url.pathname === "/api/workspaces/ws-1/gateway-budget";
    });
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await refreshedKeyRead;

    const quotaUsed = page.locator(".workspace-budget-panel .data-list > div")
      .filter({ hasText: "总额度已用" })
      .locator("dd");
    await quotaUsed.waitFor({ state: "visible" });
    assert.equal(await quotaUsed.textContent(), "$7.77");
    assert.equal(requestOrder[0], "PATCH /api/workspaces/ws-1/gateway-budget");
    assert.equal(requestOrder[1], "GET /api/workspaces?page=1&pageSize=50");
    assert.ok(requestOrder.slice(2).includes("GET /api/workspaces/ws-1/runtime-status"));
    assert.ok(requestOrder.slice(2).includes("GET /api/workspaces/ws-1/gateway-budget"));

    const mutationResponse = page.waitForResponse((response) => response.request() === mutationRoute.request());
    releaseMutation.resolve();
    await mutationResponse;
    await page.getByRole("button", { name: "重置总额度用量", exact: true }).waitFor({ state: "visible" });
    await page.waitForTimeout(100);

    assert.equal(await quotaUsed.textContent(), "$7.77");
    assert.equal(await page.getByText("模型预算已更新", { exact: true }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Renewal projection commit rejects an older in-flight Workspace refresh", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let releaseStaleRefresh = () => {};
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.goto(`${demo.origin}/login`, { waitUntil: "domcontentloaded" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);

    const initialWorkspace: WorkspaceDTO = {
      id: "ws-1",
      ownerAccountId: "acct-1",
      ownerUserId: "user-customer",
      state: "running",
      createdAt: "2026-07-01T00:00:00Z",
      updatedAt: fetchedAt,
      name: "Initial Workspace",
      url: "https://workspace.example.invalid/w/ws-1/",
      packageId: "basic",
      storageGb: 10,
      autoRenew: false,
      priceVersion: "pilot-usd-2026-07-v1",
      currency: "USD",
      totalUsdMicros: 52_580_000,
      periodStart: "2026-07-01T00:00:00Z",
      paidThrough: "2026-08-01T00:00:00Z",
      nextRenewalAt: "2026-08-01T00:00:00Z",
      renewalStatus: "active",
      workspaceApiKeyId: "9"
    };
    const staleWorkspace: WorkspaceDTO = {
      ...initialWorkspace,
      name: "Stale Refresh Workspace",
      updatedAt: "2026-08-26T00:01:00Z"
    };
    const renewedWorkspace: WorkspaceDTO = {
      ...initialWorkspace,
      name: "Renewal Projected Workspace",
      autoRenew: true,
      updatedAt: "2026-08-26T00:02:00Z",
      paidThrough: "2026-09-01T00:00:00Z",
      nextRenewalAt: "2026-09-01T00:00:00Z"
    };
    const renewalResponse: WorkspaceRenewalResponse = {
      autoRenew: true,
      effectiveAfter: "2026-08-26T00:02:00Z",
      nextRenewalAt: renewedWorkspace.nextRenewalAt!,
      paidThrough: renewedWorkspace.paidThrough!,
      renewalStatus: renewedWorkspace.renewalStatus!
    };

    const staleRefreshHeld = deferred<Route>();
    const staleRefreshRelease = deferred<void>();
    releaseStaleRefresh = () => staleRefreshRelease.resolve();
    let workspaceReads = 0;
    await page.route("**/api/workspaces?*", async (route) => {
      workspaceReads += 1;
      const url = new URL(route.request().url());
      const pageNumber = Number(url.searchParams.get("page"));
      const pageSize = Number(url.searchParams.get("pageSize"));
      if (workspaceReads === 2) {
        staleRefreshHeld.resolve(route);
        await staleRefreshRelease.promise;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(workspaceSource(staleWorkspace, pageNumber, pageSize))
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workspaceSource(workspaceReads === 1 ? initialWorkspace : renewedWorkspace, pageNumber, pageSize))
      });
    });

    const renewalRequested = deferred<Route>();
    await page.route("**/api/workspaces/ws-1/auto-renew", async (route) => {
      renewalRequested.resolve(route);
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(renewalResponse)
      });
    });

    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    const disabledRenewal = page.getByRole("checkbox", { name: "已关闭", exact: true });
    await disabledRenewal.waitFor({ state: "visible" });

    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    const staleRefreshRoute = await staleRefreshHeld.promise;
    assert.equal(staleRefreshRoute.request().method(), "GET");

    await disabledRenewal.click();
    const renewalRoute = await renewalRequested.promise;
    assert.equal(renewalRoute.request().method(), "POST");
    assert.deepEqual(renewalRoute.request().postDataJSON(), { autoRenew: true });

    const enabledRenewal = page.getByRole("checkbox", { name: "已开启", exact: true });
    await enabledRenewal.waitFor({ state: "visible" });
    await page.getByRole("status").filter({ hasText: "自动续费已开启" }).waitFor({ state: "visible" });
    await page.getByRole("heading", { name: renewedWorkspace.name!, exact: true }).waitFor({ state: "visible" });
    assert.equal(workspaceReads, 3);

    const staleRefreshResponse = page.waitForResponse((response) => {
      const url = new URL(response.url());
      return response.request().method() === "GET"
        && url.pathname === "/api/workspaces"
        && url.searchParams.get("pageSize") === "50";
    });
    releaseStaleRefresh();
    await staleRefreshResponse;
    await page.waitForTimeout(100);

    assert.equal(await enabledRenewal.isChecked(), true);
    assert.equal(await page.getByRole("heading", { name: renewedWorkspace.name!, exact: true }).count(), 1);
    assert.equal(await page.getByText(staleWorkspace.name!, { exact: true }).count(), 0);
    const runtimeState = page.locator(".workspace-identity-panel .workspace-availability strong");
    await runtimeState.waitFor({ state: "visible" });
    assert.equal(await runtimeState.textContent(), "可使用");
  } finally {
    releaseStaleRefresh();
    await browser.close();
    await demo.close();
  }
});
