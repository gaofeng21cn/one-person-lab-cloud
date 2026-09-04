import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceListData,
  WorkspaceRuntimeDTO
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

function source<T>(data: T, sourceName: string): SourceEnvelope<T> {
  return {
    source: sourceName,
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

function unavailable<T>(sourceName: string): SourceEnvelope<T> {
  return {
    source: sourceName,
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: `${sourceName}_unavailable`
  };
}

function workspace(id: string, name: string, keyId: string): WorkspaceDTO {
  return {
    id,
    ownerAccountId: "account-customer",
    ownerUserId: "user-customer",
    state: "running",
    createdAt: fetchedAt,
    updatedAt: fetchedAt,
    name,
    url: `https://workspace.example.invalid/w/${id}/`,
    packageId: "basic",
    storageGb: 10,
    autoRenew: false,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    totalUsdMicros: 52_580_000,
    periodStart: fetchedAt,
    paidThrough: "2026-09-27T00:00:00Z",
    renewalStatus: "manual",
    workspaceApiKeyId: keyId
  };
}

function runtime(workspaceId: string, version: string): WorkspaceRuntimeDTO {
  return {
    workspaceId,
    runtimeId: `runtime-${workspaceId}-${version}`,
    status: "running",
    ready: true,
    url: `https://runtime.example.invalid/${workspaceId}/${version}/`,
    serviceName: `runtime-${workspaceId}`,
    access: {
      username: "opl",
      credentialStatus: "configured",
      credentialVersion: version
    },
    checks: [
      { name: "deployment_ready", ok: true },
      { name: "ready_pod_uses_retained_pvc", ok: true }
    ]
  };
}

function budget(workspaceId: string, keyId: string): WorkspaceGatewayBudgetDTO {
  return {
    workspaceId,
    keyId,
    status: "active",
    quotaUsdMicros: "12345678",
    quotaUsedUsdMicros: "4321",
    rateLimit5hUsdMicros: "500000",
    rateLimit1dUsdMicros: "1000000",
    rateLimit7dUsdMicros: "7000000",
    usage5hUsdMicros: "111",
    usage1dUsdMicros: "222",
    usage7dUsdMicros: "333",
    enabled: true,
    updatedAt: fetchedAt
  };
}

async function fulfill<T>(route: Route, body: T, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body)
  });
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

async function navigate(page: Page, path: string) {
  await page.evaluate((nextPath) => {
    window.history.pushState({}, "", nextPath);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, path);
}

async function openDisclosure(page: Page, selector: string) {
  const details = page.locator(selector);
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}

test("Fabric Runtime Read rejects late Workspace and refresh responses and settles failure independently", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseAlpha = deferred();
  const alphaHeld = deferred();
  const alphaSettled = deferred();
  const releaseStaleBeta = deferred();
  const staleBetaHeld = deferred();
  const staleBetaSettled = deferred();
  const alpha = workspace("workspace-alpha", "Alpha Workspace", "91");
  const beta = workspace("workspace-beta", "Beta Workspace", "92");
  let betaRuntimeReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      const pageNumber = Number(url.searchParams.get("page"));
      const pageSize = Number(url.searchParams.get("pageSize"));
      const items = pageSize === 1 ? [alpha] : [alpha, beta];
      await fulfill(route, source<WorkspaceListData>({
        items,
        total: 2,
        page: pageNumber,
        pageSize
      }, "control-plane"));
    });
    await page.route("**/api/workspaces/*/gateway-budget", async (route) => {
      const workspaceId = new URL(route.request().url()).pathname.split("/")[3];
      const value = workspaceId === alpha.id ? alpha : beta;
      await fulfill(route, source(budget(value.id, value.workspaceApiKeyId || ""), "sub2api"));
    });
    await page.route("**/api/workspaces/*/runtime-status", async (route) => {
      const workspaceId = new URL(route.request().url()).pathname.split("/")[3];
      if (workspaceId === alpha.id) {
        alphaHeld.resolve();
        await releaseAlpha.promise;
        await fulfill(route, source(runtime(alpha.id, "stale-alpha"), "fabric"));
        alphaSettled.resolve();
        return;
      }
      betaRuntimeReads += 1;
      if (betaRuntimeReads === 2) {
        staleBetaHeld.resolve();
        await releaseStaleBeta.promise;
        await fulfill(route, source(runtime(beta.id, "stale-refresh"), "fabric"));
        staleBetaSettled.resolve();
        return;
      }
      if (betaRuntimeReads === 4) {
        await fulfill(route, unavailable<WorkspaceRuntimeDTO>("fabric"), 502);
        return;
      }
      const version = betaRuntimeReads === 1 ? "initial" : "fresh-refresh";
      await fulfill(route, source(runtime(beta.id, version), "fabric"));
    });

    await login(page, demo.origin);
    await navigate(page, `/console/workspaces/${alpha.id}`);
    await alphaHeld.promise;

    await navigate(page, `/console/workspaces/${beta.id}`);
    await page.getByRole("heading", { name: beta.name || "", exact: true }).waitFor({ state: "visible" });
    await openDisclosure(page, "details.workspace-technical-details");
    await page.getByText(runtime(beta.id, "initial").url || "", { exact: true }).waitFor({ state: "visible" });

    releaseAlpha.resolve();
    await alphaSettled.promise;
    await page.waitForTimeout(50);
    assert.equal(await page.getByText(runtime(alpha.id, "stale-alpha").url || "", { exact: true }).count(), 0);
    assert.equal(await page.getByText(runtime(beta.id, "initial").url || "", { exact: true }).count(), 1);

    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await staleBetaHeld.promise;
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await page.getByText(runtime(beta.id, "fresh-refresh").url || "", { exact: true }).waitFor({ state: "visible" });
    releaseStaleBeta.resolve();
    await staleBetaSettled.promise;
    await page.waitForTimeout(50);
    assert.equal(await page.getByText(runtime(beta.id, "stale-refresh").url || "", { exact: true }).count(), 0);
    assert.equal(await page.getByText(runtime(beta.id, "fresh-refresh").url || "", { exact: true }).count(), 1);

    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await page.getByText("入口暂不可用", { exact: true }).waitFor({ state: "visible" });
    await page.getByText("访问凭据暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("heading", { name: beta.name || "", exact: true }).count(), 1);
    await openDisclosure(page, "details.workspace-advanced-details");
    const budgetPanel = page.locator(".workspace-budget-panel");
    assert.equal(await budgetPanel.getByLabel("总额度（美元）").inputValue(), "12.345678");
    assert.equal(await budgetPanel.getByRole("checkbox", { name: "启用 API 密钥", exact: true }).isChecked(), true);
    assert.equal(await budgetPanel.locator(".data-list > div").filter({ hasText: /^状态已启用$/ }).locator("dd").textContent(), "已启用");
    assert.equal(await budgetPanel.locator(".data-list > div").filter({ hasText: "总额度已用" }).locator("dd").textContent(), "$0.00");
    assert.equal(await budgetPanel.getByText("模型预算暂不可用", { exact: true }).count(), 0);
    assert.equal(await budgetPanel.getByText("原因代码：sub2api_unavailable", { exact: true }).count(), 0);
    await page.locator("details.workspace-technical-details").getByText(beta.workspaceApiKeyId || "", { exact: true }).first().waitFor({ state: "visible" });
    await page.locator("details.workspace-technical-details").getByText("fabric_unavailable", { exact: true }).waitFor({ state: "visible" });
  } finally {
    releaseAlpha.resolve();
    releaseStaleBeta.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Fabric Runtime request starts before Customer Workspace detail completes", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const detailHeld = deferred();
  const releaseDetail = deferred();
  const runtimeStarted = deferred();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("pageSize") !== "50") {
        await route.fallback();
        return;
      }
      detailHeld.resolve();
      await releaseDetail.promise;
      await route.fallback();
    });
    await page.route("**/api/workspaces/ws-1/runtime-status", async (route) => {
      runtimeStarted.resolve();
      await route.fallback();
    });

    await login(page, demo.origin);
    await navigate(page, "/console/workspaces/ws-1");
    await detailHeld.promise;
    await runtimeStarted.promise;
    releaseDetail.resolve();
    await page.getByText("可使用", { exact: true }).waitFor({ state: "visible" });
  } finally {
    releaseDetail.resolve();
    await browser.close();
    await demo.close();
  }
});

test("leaving the Workspace detail route rejects an in-flight Runtime completion", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const staleRuntimeHeld = deferred();
  const releaseStaleRuntime = deferred();
  const staleRuntimeSettled = deferred();
  const freshRuntimeHeld = deferred();
  const releaseFreshRuntime = deferred();
  let runtimeReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/workspaces/ws-1/runtime-status", async (route) => {
      runtimeReads += 1;
      if (runtimeReads === 1) {
        staleRuntimeHeld.resolve();
        await releaseStaleRuntime.promise;
        await fulfill(route, source(runtime("ws-1", "route-exit-stale"), "fabric"));
        staleRuntimeSettled.resolve();
        return;
      }
      freshRuntimeHeld.resolve();
      await releaseFreshRuntime.promise;
      await fulfill(route, source(runtime("ws-1", "route-reentry-fresh"), "fabric"));
    });

    await login(page, demo.origin);
    await navigate(page, "/console/workspaces/ws-1");
    await staleRuntimeHeld.promise;
    await page.getByRole("button", { name: "工作空间列表", exact: true }).click();
    await page.locator(".workspace-list-page").waitFor({ state: "visible" });

    releaseStaleRuntime.resolve();
    await staleRuntimeSettled.promise;
    await page.waitForTimeout(50);
    assert.equal(new URL(page.url()).pathname, "/console/workspaces");
    assert.equal(await page.getByText(runtime("ws-1", "route-exit-stale").url || "", { exact: true }).count(), 0);

    await navigate(page, "/console/workspaces/ws-1");
    await freshRuntimeHeld.promise;
    assert.equal(await page.getByText(runtime("ws-1", "route-exit-stale").url || "", { exact: true }).count(), 0);
    await page.locator(".workspace-access-panel").getByText("正在读取", { exact: true }).waitFor({ state: "visible" });
    releaseFreshRuntime.resolve();
    await openDisclosure(page, "details.workspace-technical-details");
    await page.getByText(runtime("ws-1", "route-reentry-fresh").url || "", { exact: true }).waitFor({ state: "visible" });
    assert.equal(runtimeReads, 2);
  } finally {
    releaseStaleRuntime.resolve();
    releaseFreshRuntime.resolve();
    await browser.close();
    await demo.close();
  }
});
