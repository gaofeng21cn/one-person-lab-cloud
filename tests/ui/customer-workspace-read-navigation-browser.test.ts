import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  RuntimeCredentialResponse,
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceListData,
  WorkspaceRenewalResponse,
  WorkspaceRuntimeDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

function workspaceSource(
  workspace: WorkspaceDTO,
  page: number,
  pageSize: number
): SourceEnvelope<WorkspaceListData> {
  return {
    source: "control-plane",
    status: "available",
    available: true,
    fetchedAt,
    data: { items: [workspace], total: 1, page, pageSize }
  };
}

function workspacePageSource(
  items: WorkspaceDTO[],
  total: number,
  page: number,
  pageSize: number
): SourceEnvelope<WorkspaceListData> {
  return {
    source: "control-plane",
    status: items.length ? "available" : "empty",
    available: true,
    fetchedAt,
    data: { items, total, page, pageSize }
  };
}

function unavailable<T>(source: string): SourceEnvelope<T> {
  return {
    source,
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: `${source.replace(/-/g, "_")}_unavailable`
  };
}

function available<T>(source: string, data: T): SourceEnvelope<T> {
  return {
    source,
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

function workspace(id: string, name: string, overrides: Partial<WorkspaceDTO> = {}): WorkspaceDTO {
  return {
    id,
    ownerAccountId: "acct-1",
    ownerUserId: "user-customer",
    state: "running",
    createdAt: "2026-07-01T00:00:00Z",
    updatedAt: fetchedAt,
    name,
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
    workspaceApiKeyId: "9",
    ...overrides
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

async function openWorkspaceDisclosure(page: Page, selector: string) {
  const details = page.locator(selector);
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}

test("Customer Workspace routes use their exact list and detail read identities", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    const reads: string[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (request.method() === "GET" && url.pathname === "/api/workspaces") {
        reads.push(url.search);
      }
    });

    await login(page, demo.origin);
    await page.getByText("Pilot Workspace", { exact: true }).first().waitFor();
    assert.ok(reads.includes("?page=1&pageSize=1"));

    await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "domcontentloaded" });
    await page.locator(".workspace-list-row").first().waitFor();
    assert.ok(reads.includes("?page=1&pageSize=10"));

    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "Pilot Workspace", exact: true }).waitFor();
    assert.ok(reads.includes("?page=1&pageSize=50"));

    await page.goto(`${demo.origin}/console/billing`, { waitUntil: "domcontentloaded" });
    await page.getByText("Control Plane 当前商业条款", { exact: true }).waitFor();
    assert.ok(reads.filter((query) => query === "?page=1&pageSize=10").length >= 2);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("a late page response cannot replace a newer page selection on the same route", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const stalePageHeld = deferred<Route>();
  const releaseStalePage = deferred<void>();
  const stalePageSettled = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);

    let pageOneReads = 0;
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      const pageSize = Number(url.searchParams.get("pageSize"));
      if (pageSize !== 10) {
        await route.continue();
        return;
      }
      const pageNumber = Number(url.searchParams.get("page"));
      if (pageNumber === 3) {
        stalePageHeld.resolve(route);
        await releaseStalePage.promise;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(workspacePageSource([workspace("ws-stale-3", "Stale Page Three")], 30, 3, 10))
        });
        stalePageSettled.resolve();
        return;
      }
      if (pageNumber === 1) pageOneReads += 1;
      const name = pageNumber === 2
        ? "Current Page Two"
        : pageOneReads === 1 ? "Initial Page One" : "Fresh Page One";
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workspacePageSource([workspace(`ws-page-${pageNumber}`, name)], 30, pageNumber, 10))
      });
    });

    await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "domcontentloaded" });
    const pagination = page.getByRole("navigation", { name: "Workspace 分页" });
    await page.getByText("Initial Page One", { exact: true }).waitFor();
    await pagination.getByRole("button", { name: "下一页" }).click();
    await page.getByText("Current Page Two", { exact: true }).waitFor();
    await pagination.getByText("第 2 / 3 页", { exact: true }).waitFor();

    await pagination.getByRole("button", { name: "下一页" }).click();
    await stalePageHeld.promise;
    await pagination.getByRole("button", { name: "上一页" }).click();
    await page.getByText("Fresh Page One", { exact: true }).waitFor();
    await pagination.getByText("第 1 / 3 页", { exact: true }).waitFor();

    releaseStalePage.resolve();
    await stalePageSettled.promise;
    await page.waitForTimeout(100);

    assert.equal(await pagination.getByText("第 1 / 3 页", { exact: true }).count(), 1);
    assert.equal(await page.getByText("Fresh Page One", { exact: true }).count(), 1);
    assert.equal(await page.getByText("Stale Page Three", { exact: true }).count(), 0);
  } finally {
    releaseStalePage.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Renewal readback invalidates an older Workspace detail refresh", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const staleRefreshHeld = deferred<Route>();
  const releaseStaleRefresh = deferred<void>();
  const staleRefreshSettled = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);

    const initial = workspace("ws-1", "Initial Workspace");
    const stale = workspace("ws-1", "Stale Refresh Workspace");
    const renewed = workspace("ws-1", "Renewed Workspace", {
      autoRenew: true,
      nextRenewalAt: "2026-09-01T00:00:00Z",
      paidThrough: "2026-09-01T00:00:00Z"
    });
    let detailReads = 0;
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("pageSize") !== "50") {
        await route.continue();
        return;
      }
      detailReads += 1;
      if (detailReads === 2) {
        staleRefreshHeld.resolve(route);
        await releaseStaleRefresh.promise;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(workspaceSource(stale, 1, 50))
        });
        staleRefreshSettled.resolve();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workspaceSource(detailReads === 1 ? initial : renewed, 1, 50))
      });
    });

    const renewalResponse: WorkspaceRenewalResponse = {
      autoRenew: true,
      effectiveAfter: renewed.updatedAt,
      nextRenewalAt: renewed.nextRenewalAt!,
      paidThrough: renewed.paidThrough!,
      renewalStatus: renewed.renewalStatus!
    };
    await page.route("**/api/workspaces/ws-1/auto-renew", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(renewalResponse)
      });
    });

    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: initial.name!, exact: true }).waitFor();
    await page.getByRole("main").getByRole("button", { name: "刷新", exact: true }).click();
    await staleRefreshHeld.promise;
    await page.getByRole("checkbox", { name: "已关闭", exact: true }).click();
    await page.getByRole("heading", { name: renewed.name!, exact: true }).waitFor();
    await page.getByRole("checkbox", { name: "已开启", exact: true }).waitFor();

    releaseStaleRefresh.resolve();
    await staleRefreshSettled.promise;
    await page.waitForTimeout(100);

    assert.equal(await page.getByRole("heading", { name: renewed.name!, exact: true }).count(), 1);
    assert.equal(await page.getByText(stale.name!, { exact: true }).count(), 0);
    assert.equal(await page.getByRole("checkbox", { name: "已开启", exact: true }).count(), 1);
  } finally {
    releaseStaleRefresh.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Runtime credential rotation refreshes Workspace, Runtime, and Gateway Budget projections", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);

    const initialWorkspace = workspace("ws-1", "Workspace Before Rotation");
    const refreshedWorkspace = workspace("ws-1", "Workspace After Rotation", {
      updatedAt: "2026-08-27T00:05:00Z"
    });
    const initialRuntime: WorkspaceRuntimeDTO = {
      workspaceId: "ws-1",
      status: "running",
      ready: true,
      checks: [{ name: "workspace_service_ready", ok: true }],
      runtimeId: "runtime-ws-1",
      url: "https://runtime-before-rotation.example/ws-1",
      access: { username: "workspace-owner", credentialStatus: "active", credentialVersion: "1" }
    };
    const refreshedRuntime: WorkspaceRuntimeDTO = {
      ...initialRuntime,
      url: "https://runtime-after-rotation.example/ws-1",
      access: { username: "workspace-owner", credentialStatus: "active", credentialVersion: "2" }
    };
    const initialBudget: WorkspaceGatewayBudgetDTO = {
      workspaceId: "ws-1",
      keyId: "9",
      status: "active",
      quotaUsdMicros: "10000000",
      quotaUsedUsdMicros: "100000",
      rateLimit5hUsdMicros: "1000000",
      rateLimit1dUsdMicros: "2000000",
      rateLimit7dUsdMicros: "3000000",
      usage5hUsdMicros: "10000",
      usage1dUsdMicros: "20000",
      usage7dUsdMicros: "30000",
      enabled: true,
      updatedAt: fetchedAt
    };
    const refreshedBudget: WorkspaceGatewayBudgetDTO = {
      ...initialBudget,
      quotaUsedUsdMicros: "7654321",
      usage5hUsdMicros: "111111",
      updatedAt: "2026-08-27T00:05:00Z"
    };
    const rotation: RuntimeCredentialResponse = {
      workspaceId: "ws-1",
      access: {
        account: "owner",
        username: "workspace-owner",
        password: "rotated-runtime-password",
        credentialStatus: "active",
        credentialVersion: "2"
      }
    };

    let rotated = false;
    let detailReads = 0;
    let runtimeReads = 0;
    let budgetReads = 0;
    let rotationWrites = 0;
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("pageSize") !== "50") {
        await route.continue();
        return;
      }
      detailReads += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workspaceSource(rotated ? refreshedWorkspace : initialWorkspace, 1, 50))
      });
    });
    await page.route("**/api/workspaces/ws-1/runtime-status", async (route) => {
      runtimeReads += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(available("fabric", rotated ? refreshedRuntime : initialRuntime))
      });
    });
    await page.route("**/api/workspaces/ws-1/gateway-budget", async (route) => {
      budgetReads += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(available("sub2api", rotated ? refreshedBudget : initialBudget))
      });
    });
    await page.route("**/api/workspaces/ws-1/runtime-credentials/rotate", async (route) => {
      rotationWrites += 1;
      assert.equal(route.request().method(), "POST");
      assert.match(route.request().headers()["idempotency-key"] || "", /^runtime-credential:/);
      rotated = true;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(rotation)
      });
    });

    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: initialWorkspace.name!, exact: true }).waitFor();
    await openWorkspaceDisclosure(page, "details.workspace-technical-details");
    await openWorkspaceDisclosure(page, "details.workspace-advanced-details");
    await page.getByRole("link", { name: initialRuntime.url!, exact: true }).waitFor();
    await page.locator(".workspace-budget-panel .data-list > div")
      .filter({ hasText: "总额度已用" }).getByText("$0.10", { exact: true }).waitFor();
    assert.deepEqual({ detailReads, runtimeReads, budgetReads }, { detailReads: 1, runtimeReads: 1, budgetReads: 1 });

    await page.getByRole("button", { name: "轮换密码", exact: true }).click();
    await page.getByText("登录密码已轮换", { exact: true }).waitFor({ state: "visible" });
    await page.getByRole("heading", { name: refreshedWorkspace.name!, exact: true }).waitFor();
    await page.getByRole("link", { name: refreshedRuntime.url!, exact: true }).waitFor();
    await page.locator(".workspace-budget-panel .data-list > div")
      .filter({ hasText: "总额度已用" }).getByText("$7.65", { exact: true }).waitFor();
    const passwordRow = page.locator(".workspace-access-panel .data-list > div")
      .filter({ hasText: "登录密码" }).first();
    await passwordRow.locator("code").getByText(rotation.access.password, { exact: true }).waitFor();

    assert.equal(rotationWrites, 1);
    assert.deepEqual({ detailReads, runtimeReads, budgetReads }, { detailReads: 2, runtimeReads: 2, budgetReads: 2 });
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace detail failure settles before and independently from Runtime", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const runtimeHeld = deferred<Route>();
  const releaseRuntime = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);

    let budgetReads = 0;
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.endsWith("/gateway-budget")) budgetReads += 1;
    });
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("pageSize") !== "50") {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify(unavailable<WorkspaceListData>("control-plane"))
      });
    });
    await page.route("**/api/workspaces/ws-1/runtime-status", async (route) => {
      runtimeHeld.resolve(route);
      await releaseRuntime.promise;
      await route.continue();
    });

    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await runtimeHeld.promise;
    await page.getByText("Workspace 详情暂不可用", { exact: true }).waitFor();
    const technical = page.locator("details.workspace-technical-details");
    assert.equal(await technical.getByText("control_plane_unavailable", { exact: true }).isVisible(), false);
    await technical.locator("summary").click();
    await technical.getByText("control_plane_unavailable", { exact: true }).waitFor({ state: "visible" });

    assert.equal(budgetReads, 0);
    releaseRuntime.resolve();
  } finally {
    releaseRuntime.resolve();
    await browser.close();
    await demo.close();
  }
});

test("a late Workspace detail cannot replace the current route Workspace", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const staleReadHeld = deferred<Route>();
  const releaseStaleRead = deferred<void>();
  const staleReadSettled = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);

    const staleWorkspace: WorkspaceDTO = {
      id: "ws-1",
      ownerAccountId: "acct-1",
      ownerUserId: "user-customer",
      state: "running",
      createdAt: "2026-07-01T00:00:00Z",
      updatedAt: fetchedAt,
      name: "Stale Pilot Workspace",
      packageId: "basic",
      storageGb: 10,
      workspaceApiKeyId: "9"
    };
    let detailReads = 0;
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("pageSize") !== "50") {
        await route.continue();
        return;
      }
      detailReads += 1;
      if (detailReads !== 1) {
        await route.continue();
        return;
      }
      staleReadHeld.resolve(route);
      await releaseStaleRead.promise;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(workspaceSource(staleWorkspace, 1, 50))
      });
      staleReadSettled.resolve();
    });

    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await staleReadHeld.promise;
    await page.getByRole("button", { name: "Workspace 列表", exact: true }).click();
    await page.waitForURL(/\/console\/workspaces$/);
    await page.getByRole("link", { name: /Second Workspace/ }).first().click();
    await page.waitForURL(/\/console\/workspaces\/ws-2$/);
    await page.getByRole("heading", { name: "Second Workspace", exact: true }).waitFor();

    releaseStaleRead.resolve();
    await staleReadSettled.promise;
    await page.waitForTimeout(100);

    assert.match(page.url(), /\/console\/workspaces\/ws-2$/);
    assert.equal(await page.getByRole("heading", { name: "Second Workspace", exact: true }).count(), 1);
    assert.equal(await page.getByRole("heading", { name: staleWorkspace.name!, exact: true }).count(), 0);
  } finally {
    releaseStaleRead.resolve();
    await browser.close();
    await demo.close();
  }
});
