import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData,
  WorkspaceRenewalReadDTO,
  WorkspaceRuntimeDTO,
  WorkspaceRenewalResponse
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";

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

async function openWorkspace(page: Page, name: string, workspaceId: string) {
  await page.getByRole("button", { name: "工作空间列表", exact: true }).click();
  await page.waitForURL(/\/console\/workspaces$/);
  await page.locator(".workspace-list-row").filter({ hasText: name }).click();
  await page.waitForURL(new RegExp(`/console/workspaces/${workspaceId}$`));
  await page.getByRole("heading", { name, exact: true }).waitFor({ state: "visible" });
}

test("Workspace Renewal keeps its intent across navigation and scopes busy to the active Workspace", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseFirst = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    const workspaces: WorkspaceDTO[] = [
      {
        id: "ws-1", ownerAccountId: "acct-1", ownerUserId: "user-customer", state: "running",
        createdAt: "2026-07-01T00:00:00Z", updatedAt: "2026-08-26T00:00:00Z", name: "Pilot Workspace",
        packageId: "basic", storageGb: 10, autoRenew: false, paidThrough: "2026-08-01T00:00:00Z",
        nextRenewalAt: "2026-08-01T00:00:00Z", renewalStatus: "active", workspaceApiKeyId: "9"
      },
      {
        id: "ws-2", ownerAccountId: "acct-1", ownerUserId: "user-customer", state: "running",
        createdAt: "2026-07-15T00:00:00Z", updatedAt: "2026-08-26T00:00:00Z", name: "Second Workspace",
        packageId: "pro", storageGb: 100, autoRenew: false, paidThrough: "2026-08-15T00:00:00Z",
        nextRenewalAt: "2026-08-15T00:00:00Z", renewalStatus: "active", workspaceApiKeyId: "19"
      }
    ];
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      const pageNumber = Number(url.searchParams.get("page") || "1");
      const pageSize = Number(url.searchParams.get("pageSize") || "20");
      const source: SourceEnvelope<WorkspaceListData> = {
        source: "control-plane", status: "available", available: true,
        fetchedAt: "2026-08-26T00:00:00Z",
        data: { items: workspaces, total: workspaces.length, page: pageNumber, pageSize }
      };
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(source) });
    });
    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await page.getByRole("checkbox", { name: "已关闭", exact: true }).waitFor({ state: "visible" });

    const response: WorkspaceRenewalResponse = {
      autoRenew: true,
      effectiveAfter: "2026-08-01T00:00:00Z",
      nextRenewalAt: "2026-08-01T00:00:00Z",
      paidThrough: "2026-08-01T00:00:00Z",
      renewalStatus: "scheduled"
    };
    const firstRequest = deferred<Route>();
    const retryObserved = deferred<void>();
    const idempotencyKeys: string[] = [];
    await page.route("**/api/workspaces/ws-1/auto-renew", async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      idempotencyKeys.push(route.request().headers()["idempotency-key"] || "");
      if (idempotencyKeys.length === 1) {
        firstRequest.resolve(route);
        await releaseFirst.promise;
      }
      if (idempotencyKeys.length === 2) {
        workspaces[0] = { ...workspaces[0], autoRenew: true, renewalStatus: "active" };
      }
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(response) });
      if (idempotencyKeys.length === 2) retryObserved.resolve();
    });

    const firstToggle = page.getByRole("checkbox", { name: "已关闭", exact: true });
    await firstToggle.click();
    const heldRoute = await firstRequest.promise;
    assert.equal(heldRoute.request().method(), "POST");
    assert.equal(await firstToggle.isDisabled(), true);

    await openWorkspace(page, "Second Workspace", "ws-2");
    const secondWorkspaceToggle = page.getByRole("checkbox", { name: "已关闭", exact: true });
    assert.equal(await secondWorkspaceToggle.isDisabled(), false);

    const lateResponse = page.waitForResponse((candidate) => candidate.request() === heldRoute.request());
    releaseFirst.resolve();
    await lateResponse;
    assert.equal(await page.getByText("自动续费已开启", { exact: true }).count(), 0);

    await openWorkspace(page, "Pilot Workspace", "ws-1");
    const retryToggle = page.getByRole("checkbox", { name: "已关闭", exact: true });
    assert.equal(await retryToggle.isDisabled(), false);
    await retryToggle.click();
    await retryObserved.promise;
    await page.getByRole("status").getByText("自动续费已开启", { exact: true }).waitFor({ state: "visible" });
    const completedToggle = page.getByRole("checkbox", { name: "已开启", exact: true });
    await completedToggle.waitFor({ state: "visible" });

    assert.equal(idempotencyKeys.length, 2);
    assert.match(idempotencyKeys[0], /^workspace-renewal:ws-1:/);
    assert.equal(idempotencyKeys[1], idempotencyKeys[0]);
    assert.equal(await completedToggle.isChecked(), true);
    assert.equal(await completedToggle.isDisabled(), false);
  } finally {
    releaseFirst.resolve();
    await browser.close();
    await demo.close();
  }
});

test("expired customers explicitly renew after funding and reopen the original recovery without gaining unpaid access", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport });
      const pageErrors: string[] = [];
      const externalRequests: string[] = [];
      context.on("page", (page) => page.on("pageerror", (error) => pageErrors.push(error.message)));
      await context.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== demo.origin) {
          externalRequests.push(route.request().url());
          return route.abort("blockedbyclient");
        }
        if (url.pathname === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
        return route.continue();
      });
      let workspace: WorkspaceDTO = {
        id: "ws-1", ownerAccountId: "acct-1", ownerUserId: "user-customer", state: "suspended",
        createdAt: "2026-07-01T00:00:00Z", updatedAt: "2026-09-01T00:00:00Z", name: "Expired Workspace",
        packageId: "basic", storageGb: 10, autoRenew: true, totalUsdMicros: 52_580_000,
        paidThrough: "2026-09-01T00:00:00Z", nextRenewalAt: "2026-09-01T00:00:00Z",
        renewalStatus: "expired_unpaid", workspaceApiKeyId: "9"
      };
      let renewal: WorkspaceRenewalReadDTO = {
        autoRenew: false, effectiveAfter: workspace.paidThrough!, nextRenewalAt: workspace.paidThrough!,
        paidThrough: workspace.paidThrough!, renewalStatus: "expired_unpaid",
        recovery: { state: "unavailable", reason: "workspace_renewal_insufficient_balance" }
      };
      let runtimeReady = true;
      let writes = 0;
      const recoveryKeys: string[] = [];
      await context.route("**/api/workspaces?*", async (route) => {
        const source: SourceEnvelope<WorkspaceListData> = {
          source: "control-plane", status: "available", available: true, fetchedAt: workspace.updatedAt,
          data: { items: [workspace], total: 1, page: 1, pageSize: 50 }
        };
        await route.fulfill({ contentType: "application/json", body: JSON.stringify(source) });
      });
      await context.route("**/api/workspaces/ws-1/renewal", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(renewal) }));
      await context.route("**/api/workspaces/ws-1/runtime-status", async (route) => {
        const source: SourceEnvelope<WorkspaceRuntimeDTO> = {
          source: "fabric", status: "available", available: true, fetchedAt: workspace.updatedAt,
          data: { workspaceId: workspace.id, status: runtimeReady ? "running" : "unready", ready: runtimeReady, checks: [], url: "https://workspace.example.invalid/w/ws-1/" }
        };
        await route.fulfill({ contentType: "application/json", body: JSON.stringify(source) });
      });
      await context.route("**/api/workspaces/ws-1/auto-renew", async (route) => {
        writes += 1;
        assert.equal(route.request().method(), "POST");
        assert.deepEqual(route.request().postDataJSON(), { autoRenew: true });
        assert.ok(route.request().headers()["idempotency-key"]);
        recoveryKeys.push(route.request().headers()["idempotency-key"]);
        assert.ok(route.request().headers()["x-opl-csrf"]);
        if (writes === 2) renewal = { ...renewal, autoRenew: true, recovery: { state: "pending", reason: "workspace_renewal_pending" } };
        await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "upstream_unavailable" }) });
      });

      let page = await context.newPage();
      await login(page, demo.origin);
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      const plan = page.locator(".workspace-plan-panel");
      await plan.getByText("余额不足。请补足余额后刷新续费条件；充值不会自动恢复工作空间。", { exact: true }).waitFor({ state: "visible" });
      await plan.getByText("数据应在到期前由您自行下载并妥善保存。到期后，平台不承担数据保管或恢复责任。", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
      assert.equal(await plan.getByRole("button", { name: "续费并恢复", exact: true }).count(), 0);
      renewal = { ...renewal, recovery: { state: "recoverable", reason: "workspace_renewal_authorization_required" } };
      await plan.getByRole("button", { name: "刷新续费条件", exact: true }).click();
      await plan.getByRole("button", { name: "续费并恢复", exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 0, "funding and eligibility reads do not authorize a renewal");
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
      page.once("dialog", (dialog) => {
        assert.match(dialog.message(), /\$52\.58/);
        assert.match(dialog.message(), /开启后续自动续费/);
        void dialog.accept();
      });
      await plan.getByRole("button", { name: "续费并恢复", exact: true }).click();
      await plan.getByText("续费结果待确认", { exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 1);
      await plan.getByRole("button", { name: "刷新续费条件", exact: true }).click();
      await plan.getByText("续费结果待确认", { exact: true }).waitFor({ state: "hidden" });
      page.once("dialog", (dialog) => { void dialog.accept(); });
      await plan.getByRole("button", { name: "续费并恢复", exact: true }).click();
      await plan.getByText("续费结果待确认", { exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 2);
      assert.equal(recoveryKeys[1], recoveryKeys[0], "unknown recovery retains its original intent even when auto-renew was already true");
      await page.close();

      page = await context.newPage();
      await page.clock.install();
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      await page.locator(".workspace-plan-panel").getByText("正在处理续费恢复", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
      assert.equal(await page.getByRole("button", { name: "续费并恢复", exact: true }).count(), 0);
      workspace = { ...workspace, state: "running", renewalStatus: "active", paidThrough: "2026-10-01T00:00:00Z", autoRenew: true };
      renewal = { ...renewal, paidThrough: workspace.paidThrough!, renewalStatus: "active", recovery: { state: "not_required", reason: "workspace_paid_period_active" } };
      runtimeReady = false;
      await page.clock.runFor(2100);
      await page.locator(".workspace-identity-panel").getByText("正在准备", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
      runtimeReady = true;
      await page.locator(".workspace-identity-panel").getByRole("button", { name: "刷新", exact: true }).click();
      await page.locator(".workspace-identity-panel").getByText("可使用", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), false);
      assert.equal(writes, 2, "reopening and polling continue the original renewal without another charge request");
      const blockedRecoveries: WorkspaceRenewalReadDTO["recovery"][] = [
        { state: "unavailable", reason: "workspace_renewal_provider_truth_unavailable" },
        { state: "unavailable", reason: "workspace_renewal_manual_review" },
        { state: "reclaimed", reason: "workspace_renewal_resources_reclaimed" }
      ];
      for (const recovery of blockedRecoveries) {
        workspace = { ...workspace, state: "suspended", renewalStatus: "expired_unpaid" };
        renewal = { ...renewal, recovery };
        await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
        await page.locator(".workspace-plan-panel").getByText(recovery.state === "reclaimed" ? "原工作空间无法恢复" : "暂时无法续费恢复", { exact: true }).waitFor({ state: "visible" });
        assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
        assert.equal(await page.getByRole("button", { name: "续费并恢复", exact: true }).count(), 0);
        assert.equal(await page.getByRole("button", { name: "保存预算", exact: true }).count(), 0);
        assert.equal(writes, 2);
      }
      await page.getByRole("button", { name: "重新购买", exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
      assert.deepEqual(pageErrors, []);
      assert.deepEqual(externalRequests, []);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
