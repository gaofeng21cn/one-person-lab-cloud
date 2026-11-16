import assert from "node:assert/strict";
import test from "node:test";

import type { Page } from "playwright";

import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData,
  WorkspaceRenewalReadDTO,
  WorkspaceRuntimeDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

test("expired customers read their renewal condition and never gain unpaid access", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
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
      // The Control Plane auto-renew command retired with the retained CP
      // execution slice. The legacy Console reads the renewal condition and never
      // starts or enables a renewal, so any write here is a regression.
      await context.route("**/api/workspaces/ws-1/auto-renew", async (route) => {
        writes += 1;
        await route.fulfill({ status: 405, contentType: "application/json", body: JSON.stringify({ error: "method_not_allowed" }) });
      });

      let page = await context.newPage();
      await login(page, demo.origin);
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      const plan = page.locator(".workspace-plan-panel");
      await plan.getByText("余额不足。请补足余额后刷新续费条件；充值不会自动恢复工作空间。", { exact: true }).waitFor({ state: "visible" });
      await plan.getByText("数据应在到期前由您自行下载并妥善保存。到期后，平台不承担数据保管或恢复责任。", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
      assert.equal(await plan.getByRole("button", { name: "续费并恢复", exact: true }).count(), 0);
      assert.equal(await page.getByRole("checkbox", { name: "已关闭", exact: true }).count(), 0);
      renewal = { ...renewal, recovery: { state: "recoverable", reason: "workspace_renewal_authorization_required" } };
      await plan.getByRole("button", { name: "刷新续费条件", exact: true }).click();
      await plan.getByText("可以续费恢复", { exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 0, "funding and eligibility reads do not authorize a renewal");
      assert.equal(await plan.getByRole("button", { name: "续费并恢复", exact: true }).count(), 0);
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
      await page.close();

      renewal = { ...renewal, recovery: { state: "pending", reason: "workspace_renewal_pending" } };
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
      assert.equal(writes, 0, "reopening and polling continue the original renewal without another charge request");
      const blockedRecoveries: WorkspaceRenewalReadDTO["recovery"][] = [
        { state: "unavailable", reason: "workspace_renewal_provider_truth_unavailable" },
        { state: "unavailable", reason: "workspace_renewal_manual_review" },
        { state: "unavailable", reason: "workspace_renewal_period_elapsed" },
        { state: "reclaimed", reason: "workspace_renewal_resources_reclaimed" }
      ];
      for (const recovery of blockedRecoveries) {
        workspace = { ...workspace, state: recovery.state === "reclaimed" ? "data_deleted" : "suspended", renewalStatus: recovery.state === "reclaimed" ? "active" : "expired_unpaid", autoRenew: false };
        renewal = { ...renewal, recovery };
        await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
        await page.locator(".workspace-plan-panel").getByText(recovery.state === "reclaimed" ? "原工作空间无法恢复" : "暂时无法续费恢复", { exact: true }).waitFor({ state: "visible" });
        assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
        assert.equal(await page.getByRole("button", { name: "续费并恢复", exact: true }).count(), 0);
        assert.equal(await page.getByRole("button", { name: "保存预算", exact: true }).count(), 0);
        assert.equal(await page.getByRole("button", { name: "重新购买", exact: true }).count(), 0);
        assert.equal((await page.locator(".workspace-plan-panel").innerText()).includes("请重新购买"), false);
        assert.equal(writes, 0);
      }
      assert.equal(await page.getByRole("checkbox", { name: "已关闭", exact: true }).count(), 0);
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
