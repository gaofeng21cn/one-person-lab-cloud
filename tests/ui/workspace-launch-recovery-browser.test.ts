import assert from "node:assert/strict";
import test from "node:test";

import type {
  OperatorReconciliationPageDTO,
  SourceEnvelope,
  WorkspaceLaunchRecoveryDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

// The recovery command retired with the retained Control Plane launch execution.
// The operator still reads what the accepted order became, and the read offers
// no action, so the Console renders the evidence without a command button.
test("operators read the original launch outcome and are offered no recovery action", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const externalRequests: string[] = [];
      const pageErrors: string[] = [];
      const recovery: WorkspaceLaunchRecoveryDTO = {
        operationId: "launch-original", launchVersion: 7, status: "manual_review", stage: "storage", allowedActions: []
      };
      const queue: SourceEnvelope<OperatorReconciliationPageDTO> = {
        source: "control-plane", status: "available", available: true, fetchedAt: "2026-09-08T12:00:00Z",
        data: { items: [{
          id: recovery.operationId, resourceType: "workspace", status: "manual_review", accountId: "acct-1",
          billingOperationId: recovery.operationId, phase: "storage", errorCode: "workspace_launch_manual_review",
          allowedActions: ["read_workspace_launch_recovery"]
        }], total: 1, page: 1, pageSize: 20 }
      };
      page.on("pageerror", (error) => pageErrors.push(error.message));
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== demo.origin) {
          externalRequests.push(url.origin);
          await route.abort();
          return;
        }
        if (url.pathname === "/@vite/client") {
          await route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
          return;
        }
        if (url.pathname === "/api/operator/reconciliation") {
          await route.fulfill({ contentType: "application/json", body: JSON.stringify(queue) });
          return;
        }
        if (url.pathname === `/api/operator/workspace-launches/${recovery.operationId}/recovery`) {
          await route.fulfill({ contentType: "application/json", body: JSON.stringify(recovery) });
          return;
        }
        await route.continue();
      });
      await page.goto(`${demo.origin}/login`, { waitUntil: "domcontentloaded" });
      await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.admin.email);
      await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.admin.password);
      await page.getByRole("button", { name: "登录", exact: true }).click();
      await page.waitForURL(/\/console\/overview$/);
      await page.goto(`${demo.origin}/admin/billing`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: "查看证据", exact: true }).click();
      const modal = page.getByRole("dialog");
      await modal.getByText("核对原开通结果", { exact: true }).waitFor();
      await modal.getByText("当前没有服务端允许的核对动作。", { exact: true }).waitFor();
      assert.equal(await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).count(), 0);
      assert.equal(await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).count(), 0);
      assert.equal(await modal.getByLabel("核对原因", { exact: true }).count(), 0);
      const text = await modal.innerText();
      assert.equal(/mutationBudget|idempotentReplayBudget|authoritativeReadBudget|launchVersion/.test(text), false);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
      assert.deepEqual(externalRequests, []);
      assert.deepEqual(pageErrors, []);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
