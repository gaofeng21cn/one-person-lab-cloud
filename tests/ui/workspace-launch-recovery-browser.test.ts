import assert from "node:assert/strict";
import test from "node:test";

import { chromium } from "playwright";

import type {
  OperatorReconciliationPageDTO,
  SourceEnvelope,
  WorkspaceLaunchRecoveryDTO,
  WorkspaceLaunchRecoveryRequest
} from "../../apps/console-ui/src/api/dtos.ts";
import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";

test("operators check the original launch using server actions, preserving uncertain results and retry identity", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const writes: Array<{ key: string; body: WorkspaceLaunchRecoveryRequest }> = [];
      const externalRequests: string[] = [];
      const pageErrors: string[] = [];
      let recovery: WorkspaceLaunchRecoveryDTO = {
        operationId: "launch-original", launchVersion: 7, status: "manual_review", stage: "storage", allowedActions: []
      };
      const queue: SourceEnvelope<OperatorReconciliationPageDTO> = {
        source: "control-plane", status: "available", available: true, fetchedAt: "2026-09-08T12:00:00Z",
        data: { items: [{
          id: recovery.operationId, resourceType: "workspace", status: "manual_review", accountId: "acct-1",
          billingOperationId: recovery.operationId, phase: "storage", errorCode: "workspace_launch_manual_review",
          progressionOwner: "control_plane_launch_reconciler", allowedActions: ["resume_workspace_launch"]
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
        if (url.pathname === `/api/operator/workspace-launches/${recovery.operationId}/recover`) {
          writes.push({ key: route.request().headers()["idempotency-key"] || "", body: route.request().postDataJSON() as WorkspaceLaunchRecoveryRequest });
          assert.ok(route.request().headers()["x-opl-csrf"]);
          if (writes.length === 2) {
            await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "readback_unavailable" }) });
            return;
          }
          recovery = writes.length === 1
            ? { ...recovery, launchVersion: 8 }
            : { ...recovery, launchVersion: 9, status: "pending", stage: "attachment", allowedActions: [] };
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
      await modal.getByText("当前没有服务端允许的核对动作。", { exact: true }).waitFor();
      assert.equal(await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).count(), 0);

      recovery = { ...recovery, allowedActions: ["check_result"] };
      await modal.getByRole("button", { name: "刷新允许动作", exact: true }).click();
      await modal.getByLabel("核对原因", { exact: true }).fill("客户询问原订单进度");
      await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).click();
      await modal.getByText("结果尚未确认，订单已保留。可稍后重新核对。", { exact: true }).waitFor();
      assert.equal(await modal.getByText("已确认原订单完成开通。", { exact: true }).count(), 0);
      assert.deepEqual(writes[0].body, { action: "check_result", launchVersion: 7, reason: "客户询问原订单进度" });
      assert.match(writes[0].key, /^check-[a-f0-9-]{36}$/);

      await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).click();
      await modal.getByText("核对结果暂未收到。可以重试本次核对，订单不会被重复创建。", { exact: true }).waitFor();
      assert.equal(await modal.getByLabel("核对原因", { exact: true }).isDisabled(), true);
      await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).click();
      await modal.getByText("已确认当前阶段，后台将继续处理原订单。", { exact: true }).waitFor();
      assert.equal(writes.length, 3);
      assert.deepEqual(writes[2], writes[1]);
      assert.equal(writes[1].body.launchVersion, 8);
      assert.equal(await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).count(), 0);
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

test("operators close an unfulfilled order through one retained request and can reopen its refund progress", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport });
      let page = await context.newPage();
      let recovery: WorkspaceLaunchRecoveryDTO = {
        operationId: "launch-close-original", launchVersion: 7, status: "manual_review", stage: "storage",
        allowedActions: ["check_result", "close_unfulfilled"]
      };
      const writes: Array<{ key: string; body: WorkspaceLaunchRecoveryRequest }> = [];
      const pageErrors: string[] = [];
      context.on("page", (opened) => opened.on("pageerror", (error) => pageErrors.push(error.message)));
      page.on("pageerror", (error) => pageErrors.push(error.message));
      await context.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        assert.equal(url.origin, demo.origin);
        if (url.pathname === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
        if (url.pathname === "/api/operator/reconciliation") {
          const queue: SourceEnvelope<OperatorReconciliationPageDTO> = {
            source: "control-plane", status: "available", available: true, fetchedAt: "2026-09-09T01:00:00Z",
            data: { items: [{ id: recovery.operationId, resourceType: "workspace", status: "manual_review", accountId: "acct-1",
              billingOperationId: recovery.operationId, phase: recovery.stage, errorCode: "workspace_launch_manual_review",
              progressionOwner: "control_plane_launch_reconciler", allowedActions: ["resume_workspace_launch"] }], total: 1, page: 1, pageSize: 20 }
          };
          return route.fulfill({ contentType: "application/json", body: JSON.stringify(queue) });
        }
        if (url.pathname.endsWith(`/workspace-launches/${recovery.operationId}/recovery`)) return route.fulfill({ contentType: "application/json", body: JSON.stringify(recovery) });
        if (url.pathname.endsWith(`/workspace-launches/${recovery.operationId}/recover`)) {
          writes.push({ key: route.request().headers()["idempotency-key"] || "", body: route.request().postDataJSON() });
          if (writes.length === 1) return route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: "operator_required" }) });
          if (writes.length === 2) return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "response_unconfirmed" }) });
          recovery = { ...recovery, launchVersion: 8, status: "pending", allowedActions: [], closeout: { status: "confirming", refundedUsdMicros: 0 } };
          return route.fulfill({ contentType: "application/json", body: JSON.stringify(recovery) });
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
      let modal = page.getByRole("dialog");
      await modal.getByLabel("核对原因", { exact: true }).fill("客户选择结束未完成的开通");
      await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).click();
      await modal.getByText("当前账号无权处理此订单，请使用有权限的管理员账号。", { exact: true }).waitFor();
      assert.equal(await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).count(), 0);
      await modal.getByRole("button", { name: "刷新允许动作", exact: true }).click();
      await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).click();
      await modal.getByText("结案请求结果暂未收到。可以重试本次请求，请勿另行退款。", { exact: true }).waitFor();
      await modal.getByRole("button", { name: "关闭", exact: true }).last().click();
      await page.getByRole("button", { name: "查看证据", exact: true }).click();
      modal = page.getByRole("dialog");
      await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).waitFor();
      assert.equal(await modal.getByLabel("核对原因", { exact: true }).inputValue(), "客户选择结束未完成的开通");
      assert.equal(await modal.getByRole("button", { name: "重新核对原开通结果", exact: true }).isDisabled(), true);
      await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).click();
      await modal.getByText("正在核对结案条件", { exact: true }).waitFor();
      assert.equal(writes.length, 3);
      assert.deepEqual(writes[2], writes[1]);
      assert.deepEqual(writes[2].body, { action: "close_unfulfilled", launchVersion: 7, reason: "客户选择结束未完成的开通" });
      assert.equal(await modal.getByRole("button", { name: "结束未完成开通并退款", exact: true }).count(), 0);
      recovery = { ...recovery, launchVersion: 9, closeout: { status: "refunding", refundedUsdMicros: 0, pendingConfirmation: true } };
      await page.close();
      page = await context.newPage();
      await page.goto(`${demo.origin}/admin/billing`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: "查看证据", exact: true }).click();
      modal = page.getByRole("dialog");
      await modal.getByText("结案结果仍在核对", { exact: true }).waitFor();
      assert.equal((await modal.innerText()).includes("已退回原账户余额"), false);
      recovery = { ...recovery, launchVersion: 10, status: "refunded", closeout: { status: "closed", refundedUsdMicros: 52_580_000, receiptId: "receipt-closed-original" } };
      await modal.getByRole("button", { name: "刷新允许动作", exact: true }).click();
      await modal.getByText("已退回原账户余额 $52.58。可查看费用记录或重新购买。", { exact: true }).waitFor();
      assert.equal(writes.length, 3);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));
      assert.deepEqual(pageErrors, []);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});
