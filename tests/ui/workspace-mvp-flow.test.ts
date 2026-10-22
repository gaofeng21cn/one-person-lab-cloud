import assert from "node:assert/strict";
import { afterEach, test } from "node:test";


import type { Page } from "playwright";

import type { AgentDeliveryDTO } from "../../apps/console-ui/src/api/delivery-api.ts";
import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
import * as workspaceApi from "../../apps/console-ui/src/api/workspaces-api.ts";
import { CUSTOMER_WORKSPACE_LIST_PAGE_SIZE } from "../../apps/console-ui/src/app/customer-workspace-read-controller-model.ts";
import type { AuthSession, SourceEnvelope, WorkspaceDeleteResponse, WorkspaceDeletionDTO, WorkspaceListData, WorkspaceOwnerDTO, WorkspaceOwnerDeletionDTO, WorkspaceOwnerOperationDTO } from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

async function openAdvancedSettings(page: import("playwright").Page) {
  const details = page.locator("details.workspace-advanced-details");
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test("Control Plane launch reads owner prices, refuses missing prices and zero balance, then submits the full App shape", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  demo.state.basicPlanAvailable = true;
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const errors: string[] = [];
      const external: string[] = [];
      const writes: Array<{ body: unknown; key: string; csrf: string }> = [];
      let pricesAvailable = false;
      let balance = "0";
      page.on("pageerror", (error) => errors.push(error.message));
      await context.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== demo.origin) {
          external.push(url.origin);
          return route.abort();
        }
        if (url.pathname === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
        if (url.pathname === "/api/pricing/preview" && !pricesAvailable) return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "price_unavailable" }) });
        if (url.pathname === "/api/gateway/wallet") return route.fulfill({ contentType: "application/json", body: JSON.stringify({
          available: true, source: "sub2api", status: "available", fetchedAt: new Date().toISOString(),
          data: { userId: "customer", currency: "USD", usdMicros: balance, status: "active" }
        }) });
        if (url.pathname === "/api/workspace-launches" && route.request().method() === "POST") {
          writes.push({ body: route.request().postDataJSON(), key: route.request().headers()["idempotency-key"] || "", csrf: route.request().headers()["x-opl-csrf"] || "" });
          return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "fixture_stops_before_resource_creation" }) });
        }
        return route.continue();
      });
      await page.goto(`${demo.origin}/login`, { waitUntil: "domcontentloaded" });
      await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
      await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
      await page.getByRole("button", { name: "登录", exact: true }).click();
      await page.waitForURL(/\/console\/overview$/);
      await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });
      await page.getByLabel("工作空间名称", { exact: true }).fill("Owner priced App");
      await page.getByText(/以下套餐暂无报价：basic、pro/).waitFor();
      const review = page.getByRole("button", { name: "核对开通信息" });
      assert.equal(await review.isDisabled(), true);
      pricesAvailable = true;
      await page.getByRole("button", { name: "重试", exact: true }).click();
      await page.getByText("余额不足，请联系管理员处理。", { exact: true }).waitFor();
      assert.equal(await review.isDisabled(), true);
      assert.equal(writes.length, 0);
      balance = "500000000";
      await page.reload({ waitUntil: "networkidle" });
      await page.getByLabel("工作空间名称", { exact: true }).fill("Owner priced App");
      await review.click();
      await page.getByRole("heading", { name: "确认开通信息", exact: true }).waitFor();
      await page.getByText("pilot-usd-2026-07-v1", { exact: true }).waitFor();
      const submit = page.getByRole("button", { name: "确认预付并开通", exact: true });
      assert.equal(await submit.isDisabled(), true);
      await page.getByRole("checkbox", { name: "我确认一次性预付工作空间月度总额并开通", exact: true }).click();
      const submitted = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/workspace-launches" && response.request().method() === "POST");
      await submit.click();
      await submitted;
      assert.equal(writes.length, 1);
      assert.deepEqual(writes[0].body, { name: "Owner priced App", packageId: "basic", autoRenew: false, provisioningMode: "full" });
      assert.ok(writes[0].key);
      assert.ok(writes[0].csrf);
      assert.deepEqual(errors, []);
      assert.deepEqual(external, []);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace delete adapter sends one typed Control Plane command", async () => {
  const requests: Array<{ path: string; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ path: String(input), init });
    return new Response(JSON.stringify({
      workspaceId: "workspace / alpha",
      status: "deleted",
      operationId: "delete-alpha"
    }), { status: 200, headers: { "content-type": "application/json" } });
  };

  const result = await workspaceApi.deleteWorkspace("workspace / alpha", "Alpha Workspace", "csrf-alpha", "delete-once");

  assert.deepEqual(result, {
    available: true,
    data: { workspaceId: "workspace / alpha", status: "deleted", operationId: "delete-alpha" }
  });
  assert.equal(requests.length, 1);
  assert.equal(requests[0].path, "/api/workspaces/workspace%20%2F%20alpha");
  assert.equal(requests[0].init?.method, "DELETE");
  assert.equal(new Headers(requests[0].init?.headers).get("x-opl-csrf"), "csrf-alpha");
  assert.equal(new Headers(requests[0].init?.headers).get("Idempotency-Key"), "delete-once");
});

test("Workspace delete adapter reports an unlanded backend route as unavailable", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ error: "method_not_allowed" }), {
    status: 405,
    headers: { "content-type": "application/json" }
  });

  assert.deepEqual(await workspaceApi.deleteWorkspace("workspace-alpha", "Alpha Workspace", "csrf-alpha", "delete-once"), {
    available: false,
    reasonCode: "workspace_delete_unavailable"
  });
});

test("Workspace delete does not relabel an owner not-found response as route unavailability", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ error: "workspace_not_found" }), {
    status: 404,
    headers: { "content-type": "application/json" }
  });

  await assert.rejects(
    () => workspaceApi.deleteWorkspace("workspace-alpha", "Alpha Workspace", "csrf-alpha", "delete-once"),
    /workspace_not_found/
  );
});

test("Workspace deletion read distinguishes confirmed no intent from inaccessible or mismatched operations", async () => {
  globalThis.fetch = async () => new Response("null", { status: 200, headers: { "content-type": "application/json" } });
  assert.equal(await workspaceApi.getWorkspaceDeletion("workspace-alpha"), null);
  for (const [status, error] of [[404, "workspace_not_found"], [403, "workspace_owner_required"], [503, "upstream_unavailable"]] as const) {
    globalThis.fetch = async () => new Response(JSON.stringify({ error }), { status, headers: { "content-type": "application/json" } });
    await assert.rejects(() => workspaceApi.getWorkspaceDeletion("workspace-alpha"), new RegExp(error));
  }
  const operation: WorkspaceDeletionDTO = { workspaceId: "workspace-beta", operationId: "delete-beta", status: "pending", phase: "runtime" };
  globalThis.fetch = async () => new Response(JSON.stringify(operation), { status: 200, headers: { "content-type": "application/json" } });
  await assert.rejects(() => workspaceApi.getWorkspaceDeletion("workspace-alpha"), /invalid_workspace_deletion_response/);
});

test("Workspace owner deletion separates resource facts from refund and operation completion", () => {
  const deletion: WorkspaceOwnerDeletionDTO = {
    workspaceId: "ws-1", operationId: "delete-original", resourceDeletionStatus: "confirmed", dataDeletionStatus: "confirmed",
    refundStatus: "requested", updatedAt: "2026-10-19T00:00:00Z"
  };
  const operation: WorkspaceOwnerOperationDTO = {
    operationId: deletion.operationId, owner: "workspace", kind: "delete_workspace", resourceId: deletion.workspaceId,
    status: "awaiting_confirmation", stage: "refund", pollAfterSeconds: 5, requestId: "request-1", createdAt: deletion.updatedAt, updatedAt: deletion.updatedAt
  };
  const cases: Array<{ refundStatus: WorkspaceOwnerDeletionDTO["refundStatus"]; status: WorkspaceOwnerOperationDTO["status"]; expectedStatus: WorkspaceDeletionDTO["status"]; pageState: WorkspaceDeletionDTO["pageState"]; refund: WorkspaceDeletionDTO["refundStatus"] }> = [
    { refundStatus: "requested", status: "awaiting_confirmation", expectedStatus: "deleted", pageState: "retrying", refund: "pending" },
    { refundStatus: "unknown", status: "awaiting_confirmation", expectedStatus: "deleted", pageState: "blocked", refund: "manual_review" },
    { refundStatus: "rejected", status: "needs_attention", expectedStatus: "manual_review", pageState: "blocked", refund: "manual_review" },
    { refundStatus: "not_applicable", status: "running", expectedStatus: "deleted", pageState: "waiting", refund: "not_due" },
    { refundStatus: "confirmed", status: "succeeded", expectedStatus: "deleted", pageState: "completed", refund: "succeeded" },
    { refundStatus: "not_applicable", status: "succeeded", expectedStatus: "deleted", pageState: "completed", refund: "not_due" }
  ];
  for (const scenario of cases) {
    const projected = workspaceApi.projectWorkspaceOwnerDeletion(
      { ...deletion, refundStatus: scenario.refundStatus }, { ...operation, status: scenario.status }
    );
    assert.equal(projected.status, scenario.expectedStatus, "rejected or failed owner facts require manual review");
    assert.equal(projected.pageState, scenario.pageState, "the original operation determines completion or attention");
    assert.equal(projected.refundStatus, scenario.refund);
  }
  assert.equal(workspaceApi.projectWorkspaceOwnerDeletion({ ...deletion, dataDeletionStatus: "pending" }, operation).status, "pending");
  assert.equal(workspaceApi.projectWorkspaceOwnerDeletion({ ...deletion, dataDeletionStatus: "unknown" }, operation).status, "manual_review");
});

test("Workspace delete scopes busy and reuses its intent after a late response", async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  let releaseDelete: (() => void) | undefined;
  let releaseListRefresh: (() => void) | undefined;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    let workspaceListReads = 0;
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (request.method() === "GET" && url.pathname === "/api/workspaces") workspaceListReads += 1;
    });

    await page.goto(`${demo.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });
    await openAdvancedSettings(page);

    const deleteResponse: WorkspaceDeleteResponse = {
      workspaceId: "ws-1",
      status: "deleted",
      operationId: "delete-ws-1"
    };
    const idempotencyKeys: string[] = [];
    let deletion: WorkspaceDeletionDTO | null = null;
    await page.route("**/api/workspaces/ws-1/deletion", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(deletion) }));
    let holdDelete: (() => void) | undefined;
    let observeRetry: (() => void) | undefined;
    const deleteHeld = new Promise<void>((resolve) => { holdDelete = resolve; });
    const retryObserved = new Promise<void>((resolve) => { observeRetry = resolve; });
    const deleteReleased = new Promise<void>((resolve) => { releaseDelete = resolve; });
    await page.route("**/api/workspaces/ws-1", async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.continue();
        return;
      }
      idempotencyKeys.push(route.request().headers()["idempotency-key"] || "");
      const firstAttempt = idempotencyKeys.length === 1;
      if (firstAttempt) {
        holdDelete?.();
        await deleteReleased;
      } else {
        demo.state.workspaces = demo.state.workspaces.filter((workspace) => workspace.id !== "ws-1");
        deletion = { workspaceId: "ws-1", operationId: "delete-ws-1", status: "deleted", phase: "complete", receiptId: "receipt-delete-ws-1" };
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(deleteResponse)
      });
      if (!firstAttempt) observeRetry?.();
    });

    page.once("dialog", (dialog) => { void dialog.accept(); });
    const firstDelete = page.getByRole("button", { name: "删除工作空间", exact: true });
    await firstDelete.click();
    await deleteHeld;
    await page.getByRole("heading", { name: "正在提交删除请求", exact: true }).waitFor({ state: "visible" });

    await page.getByRole("button", { name: "工作空间列表", exact: true }).click();
    await page.waitForURL(/\/console\/workspaces$/);
    await page.locator(".workspace-list-row").filter({ hasText: "Second Workspace" }).click();
    await page.waitForURL(/\/console\/workspaces\/ws-2$/);
    await page.getByRole("heading", { name: "Second Workspace", exact: true }).waitFor({ state: "visible" });
    await openAdvancedSettings(page);

    const secondDelete = page.getByRole("button", { name: "删除工作空间", exact: true });
    assert.equal(await secondDelete.getAttribute("aria-busy"), null);
    assert.equal(await secondDelete.isDisabled(), false);
    const readsBeforeRelease = workspaceListReads;

    const lateDeleteResponse = page.waitForResponse((response) => {
      const request = response.request();
      return request.method() === "DELETE" && new URL(response.url()).pathname === "/api/workspaces/ws-1";
    });
    releaseDelete?.();
    await lateDeleteResponse;
    await page.evaluate(() => new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    }));

    assert.equal(await secondDelete.isDisabled(), false);
    assert.equal(workspaceListReads, readsBeforeRelease);
    assert.match(page.url(), /\/console\/workspaces\/ws-2$/);
    assert.equal(await page.locator(".toast").count(), 0);
    assert.equal(await page.getByText("Workspace 已删除", { exact: true }).count(), 0);
    assert.equal(await page.getByText("删除结果尚未获得权威回读确认", { exact: true }).count(), 0);

    await page.getByRole("button", { name: "工作空间列表", exact: true }).click();
    await page.waitForURL(/\/console\/workspaces$/);
    await page.locator(".workspace-list-row").filter({ hasText: "Pilot Workspace" }).click();
    await page.waitForURL(/\/console\/workspaces\/ws-1$/);
    await page.getByRole("heading", { name: "Pilot Workspace", exact: true }).waitFor({ state: "visible" });
    await openAdvancedSettings(page);

    // Deletion confirmation precedes the list route's own authoritative read.
    // Hold that response so the assertions cannot race its rendering commit.
    let observeListRefresh: (() => void) | undefined;
    const listRefreshHeld = new Promise<void>((resolve) => { observeListRefresh = resolve; });
    const listRefreshReleased = new Promise<void>((resolve) => { releaseListRefresh = resolve; });
    await page.route("**/api/workspaces?*", async (route) => {
      if (new URL(route.request().url()).searchParams.get("pageSize") !== String(CUSTOMER_WORKSPACE_LIST_PAGE_SIZE)) return route.continue();
      const response = await route.fetch();
      observeListRefresh?.();
      await listRefreshReleased;
      await route.fulfill({ response });
    });

    page.once("dialog", (dialog) => { void dialog.accept(); });
    const retryDelete = page.getByRole("button", { name: "删除工作空间", exact: true });
    assert.equal(await retryDelete.getAttribute("aria-busy"), null);
    assert.equal(await retryDelete.isDisabled(), false);
    await retryDelete.click();
    await retryObserved;
    await page.waitForURL(/\/console\/workspaces$/);
    await page.getByText("Workspace 已删除", { exact: true }).waitFor({ state: "visible" });
    await listRefreshHeld;
    await page.getByRole("button", { name: "正在读取", exact: true }).waitFor({ state: "visible" });
    releaseListRefresh?.();
    await page.getByRole("button", { name: "新建工作空间", exact: true }).waitFor({ state: "visible" });
    await page.locator(".workspace-list-row").filter({ hasText: "Second Workspace" }).waitFor({ state: "visible" });

    assert.equal(idempotencyKeys.length, 2);
    assert.match(idempotencyKeys[0], /^workspace-delete:/);
    assert.equal(idempotencyKeys[1], idempotencyKeys[0]);
    assert.equal(await page.locator(".workspace-list-row").filter({ hasText: "Pilot Workspace" }).count(), 0);
  } finally {
    releaseDelete?.();
    releaseListRefresh?.();
    await browser.close();
    await demo.close();
  }
});

test("Workspace deletion survives closing the page and remains pending until its original receipt and absence are confirmed", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const context = await browser.newContext({ viewport });
      const pageErrors: string[] = [];
      const externalRequests: string[] = [];
      context.on("page", (page) => page.on("pageerror", (error) => pageErrors.push(error.message)));
      let deletion: WorkspaceDeletionDTO | null = null;
      let absent = false;
      let writes = 0;
      await context.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (url.origin !== demo.origin) {
          externalRequests.push(route.request().url());
          return route.abort("blockedbyclient");
        }
        if (url.pathname === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
        return route.continue();
      });
      await context.route("**/api/workspaces?*", async (route) => {
        const upstream = await route.fetch();
        const source = await upstream.json() as SourceEnvelope<WorkspaceListData>;
        assert.equal(source.available, true);
        if (source.available && absent) {
          source.data.items = source.data.items.filter((workspace) => workspace.id !== "ws-1");
          source.data.total = source.data.items.length;
        }
        await route.fulfill({ response: upstream, body: JSON.stringify(source) });
      });
      await context.route("**/api/workspaces/ws-1/deletion", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(deletion) }));
      await context.route("**/api/workspaces/ws-1", async (route) => {
        assert.equal(route.request().method(), "DELETE");
        writes += 1;
        assert.ok(route.request().headers()["idempotency-key"]);
        assert.ok(route.request().headers()["x-opl-csrf"]);
        deletion = {
          workspaceId: "ws-1", operationId: "delete-original-ws-1", status: "pending", phase: "runtime_secret_absent",
          stage: "runtime_absent", pageState: "waiting", refundStatus: "blocked"
        };
        await route.fulfill({ status: 202, contentType: "application/json", body: JSON.stringify(deletion) });
      });
      let page = await context.newPage();
      await page.goto(`${demo.origin}/login`, { waitUntil: "domcontentloaded" });
      await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
      await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
      await page.getByRole("button", { name: "登录", exact: true }).click();
      await page.waitForURL(/\/console\/overview$/);
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: "删除工作空间", exact: true }).waitFor({ state: "visible" });
      assert.match(await page.locator(".workspace-delete-panel").innerText(), /删除和退款是独立操作/);
      assert.doesNotMatch(await page.locator(".workspace-delete-panel").innerText(), /符合原单退款政策时自动处理|不会自动退款/);
      page.once("dialog", (dialog) => {
        assert.match(dialog.message(), /请先自行下载/);
        assert.match(dialog.message(), /关闭页面后仍会继续处理/);
        assert.match(dialog.message(), /删除和退款是独立操作/);
        assert.match(dialog.message(), /退款结果以原单回读为准/);
        assert.doesNotMatch(dialog.message(), /符合原单退款政策时自动处理|不会自动退款/);
        void dialog.accept();
      });
      await page.getByRole("button", { name: "删除工作空间", exact: true }).click();
      await page.getByRole("heading", { name: "正在删除工作空间", exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 1);
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).count(), 0);
      assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      await page.close();

      // A converging readback is retrying: the page names the stage, the scheduled
      // retry and the separate refund status instead of a generic "pending".
      deletion = {
        workspaceId: "ws-1", operationId: "delete-original-ws-1", status: "pending", phase: "storage_absent",
        stage: "storage_absent", pageState: "retrying", nextRetryAt: new Date(Date.now() + 60_000).toISOString(), refundStatus: "blocked"
      };
      page = await context.newPage();
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      await page.getByRole("heading", { name: "正在重试删除", exact: true }).waitFor({ state: "visible" });
      await page.getByText("正在等待数据盘解绑并销毁", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("退款等待资源删除确认", { exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 1, "reopening reads the original operation without resubmitting DELETE");
      assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      absent = true;
      deletion = { ...deletion, phase: "workspace_absent", stage: "workspace_absent" };
      await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await page.getByRole("heading", { name: "正在重试删除", exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByText("Workspace 已删除", { exact: true }).count(), 0);
      // An identity conflict blocks the deletion, keeps the refund out of reach and
      // offers no manual completion control.
      deletion = {
        ...deletion, status: "manual_review", phase: "storage_absent", stage: "storage_absent", pageState: "blocked",
        reasonCode: "fabric_storage_identity_conflict", nextRetryAt: undefined
      };
      await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await page.getByRole("heading", { name: "删除已暂停，需要核对", exact: true }).waitFor({ state: "visible" });
      await page.getByText("数据盘身份与原始开通记录不一致", { exact: false }).first().waitFor({ state: "visible" });
      await page.getByText("fabric_storage_identity_conflict", { exact: true }).first().waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      assert.equal(await page.getByRole("button", { name: /确认删除完成|强制完成/ }).count(), 0);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
      deletion = {
        ...deletion, status: "deleted", phase: "complete", stage: "receipt_recorded", pageState: "completed",
        reasonCode: undefined, receiptId: "receipt-delete-original-ws-1", refundStatus: "succeeded"
      };
      await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await page.waitForURL(/\/console\/workspaces$/);
      await page.getByText("Workspace 已删除", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.locator(".workspace-list-row").filter({ hasText: "Pilot Workspace" }).count(), 0);
      assert.equal(writes, 1);
      assert.deepEqual(pageErrors, []);
      assert.deepEqual(externalRequests, []);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

async function startCloudWorkspaceDeleteBrowser() {
  const session: AuthSession = {
    user: { id: "user-customer", accountId: "acct-1", role: "", email: "customer@example.invalid", status: "active" },
    isOperator: false, csrfToken: "cloud-delete-csrf"
  };
  const workspace: WorkspaceOwnerDTO = {
    id: "ws-cloud-delete", name: "Cloud Delete Workspace", computePlanId: "compute-1", storagePlanId: "storage-1",
    deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available",
    createdAt: "2026-10-19T00:00:00Z", updatedAt: "2026-10-19T00:00:00Z", version: "1"
  };
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  try {
    return { ...await startConsoleDemoServer({ port: 0, log: false }), session, workspace };
  } finally {
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
}

async function installCloudWorkspaceDeletePage(page: Page, demo: Awaited<ReturnType<typeof startCloudWorkspaceDeleteBrowser>>) {
  const pageErrors: string[] = [];
  const legacyRequests: string[] = [];
  const externalRequests: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin !== demo.origin) {
      externalRequests.push(request.url());
      return route.abort("blockedbyclient");
    }
    if (url.pathname === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
    if (url.pathname.startsWith("/api/") && !url.pathname.startsWith("/api/v2/")) legacyRequests.push(`${request.method()} ${url.pathname}`);
    return route.continue();
  });
  await page.route("**/api/v2/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v2/auth/session") return route.fulfill({ json: {
      actorId: demo.session.user.id, tenantId: demo.session.user.accountId, displayName: demo.session.user.email,
      permissions: [], csrfToken: demo.session.csrfToken, expiresAt: "2099-01-01T00:00:00Z"
    } });
    if (path === "/api/v2/workspaces") return route.fulfill({ json: { items: [demo.workspace] } });
    if (path === `/api/v2/workspaces/${demo.workspace.id}`) return route.fulfill({ json: demo.workspace });
    if (path === `/api/v2/delivery/${demo.workspace.id}`) return route.fulfill({ json: {
      workspaceId: demo.workspace.id,
      workspace: { owner: "workspace", state: "active", details: {} },
      capabilityVersion: { owner: "capability", state: "ready", details: {} },
      build: { owner: "build", state: "succeeded", details: {} },
      serve: { owner: "serve", state: "ready", details: {} }
    } satisfies AgentDeliveryDTO });
    throw new Error(`unexpected cloud detail request: ${route.request().method()} ${path}`);
  });
  return { pageErrors, legacyRequests, externalRequests };
}

test("Cloud Workspace detail first deletion confirms once and displays the original owner operation and independent refund", { timeout: 60_000 }, async () => {
  const demo = await startCloudWorkspaceDeleteBrowser();
  const { origin, workspace, session } = demo;
  const browser = await launchBrowser({ headless: true });
  let releaseOwnerReadback!: () => void;
  const ownerReadbackRelease = new Promise<void>((resolve) => { releaseOwnerReadback = resolve; });
  let ownerReadbackStarted!: () => void;
  const ownerReadbackHeld = new Promise<void>((resolve) => { ownerReadbackStarted = resolve; });
  try {
    const page = await browser.newPage();
    const audit = await installCloudWorkspaceDeletePage(page, demo);
    let deletion: WorkspaceOwnerDeletionDTO | null = null;
    let ownerDeleted = false;
    let holdOwnerReadback = false;
    const operation: WorkspaceOwnerOperationDTO = {
      operationId: "delete-original-cloud", owner: "workspace", kind: "delete_workspace", resourceId: workspace.id,
      status: "running", stage: "compute_deletion", pollAfterSeconds: 1, requestId: "request-cloud-delete",
      createdAt: workspace.createdAt, updatedAt: workspace.updatedAt
    };
    const requests: string[] = [];
    let writes = 0;
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === `/api/v2/workspaces/${workspace.id}/deletion`) {
        requests.push(`${request.method()} ${path}`);
        assert.equal(request.method(), "GET");
        return route.fulfill({ json: deletion });
      }
      if (path === `/api/v2/workspaces/${workspace.id}` && request.method() === "DELETE") {
        requests.push(`${request.method()} ${path}`);
        writes += 1;
        assert.deepEqual(request.postDataJSON(), { confirmationName: workspace.name, acknowledgeDataDestruction: true });
        assert.equal(request.headers()["x-csrf-token"], session.csrfToken);
        assert.match(request.headers()["idempotency-key"], /^workspace-delete:/);
        deletion = {
          workspaceId: workspace.id, operationId: operation.operationId,
          resourceDeletionStatus: "pending", dataDeletionStatus: "pending",
          refundStatus: "requested", refundOperationId: "refund-original-cloud", updatedAt: workspace.updatedAt
        };
        return route.fulfill({ status: 202, json: operation });
      }
      if (path === `/api/v2/operations/workspace/${operation.operationId}`) {
        requests.push(`${request.method()} ${path}`);
        assert.equal(request.method(), "GET");
        return route.fulfill({ json: operation });
      }
      if (path === `/api/v2/workspaces/${workspace.id}`) {
        assert.equal(request.method(), "GET");
        if (holdOwnerReadback) {
          ownerReadbackStarted();
          await ownerReadbackRelease;
        }
        return route.fulfill({ json: { ...workspace, status: ownerDeleted ? "deleted" : "active" } satisfies WorkspaceOwnerDTO });
      }
      return route.fallback();
    });
    const detailUrl = `${origin}/console/workspaces/${workspace.id}`;
    await page.goto(detailUrl, { waitUntil: "networkidle" });
    const deleteButton = page.getByRole("button", { name: "删除工作空间", exact: true });
    await deleteButton.waitFor({ state: "visible", timeout: 5_000 });
    assert.ok(requests.includes(`GET /api/v2/workspaces/${workspace.id}/deletion`));
    assert.equal(requests.some((request) => request.includes("/operations/")), false, "no intent must not poll an operation");
    assert.match(await page.locator(".workspace-delete-panel").innerText(), /符合原单退款政策时自动处理/);
    assert.doesNotMatch(await page.locator(".workspace-delete-panel").innerText(), /不会自动退款/);

    page.once("dialog", (dialog) => { void dialog.dismiss(); });
    await deleteButton.click();
    assert.equal(writes, 0, "cancelling confirmation must not issue DELETE");
    page.once("dialog", (dialog) => {
      assert.match(dialog.message(), /Cloud Delete Workspace/);
      assert.match(dialog.message(), /请先自行下载/);
      assert.match(dialog.message(), /删除后数据无法恢复/);
      assert.match(dialog.message(), /资源删除与退款分别确认/);
      assert.match(dialog.message(), /符合原单退款政策时自动处理/);
      assert.match(dialog.message(), /退款结果以原单回读为准/);
      void dialog.accept();
    });
    await deleteButton.click();
    await page.getByRole("heading", { name: "正在删除工作空间", exact: true }).waitFor();
    const progress = page.locator("[data-workspace-delete-progress]");
    await progress.getByText(operation.operationId, { exact: true }).waitFor();
    await progress.getByText("待确认", { exact: true }).waitFor();
    await progress.getByText("退款处理中", { exact: true }).waitFor();
    assert.equal(writes, 1);
    assert.equal(await deleteButton.count(), 0);
    assert.equal(await page.getByText("Workspace 已删除", { exact: true }).count(), 0);

    await page.reload({ waitUntil: "networkidle" });
    await progress.getByText(operation.operationId, { exact: true }).waitFor();
    await progress.getByText("退款处理中", { exact: true }).waitFor();
    assert.equal(writes, 1, "reopening must only read the accepted operation");
    deletion = { ...deletion!, resourceDeletionStatus: "confirmed", dataDeletionStatus: "pending" };
    await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
    await progress.getByText("待确认", { exact: true }).waitFor();
    assert.equal(await page.getByRole("heading", { name: "工作空间已删除", exact: true }).count(), 0,
      "resource deletion without data deletion is not complete");

    deletion = { ...deletion, resourceDeletionStatus: "rejected" };
    operation.status = "needs_attention";
    await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
    await page.getByRole("heading", { name: "删除已暂停，需要核对", exact: true }).waitFor();
    await progress.getByText("退款处理中", { exact: true }).waitFor();
    assert.doesNotMatch(await page.locator(".workspace-delete-panel").innerText(), /不会退款/,
      "a resource problem must not overwrite the owner's independent refund fact");

    deletion = { ...deletion, resourceDeletionStatus: "confirmed", dataDeletionStatus: "confirmed" };
    operation.status = "awaiting_confirmation";
    operation.stage = "refund";
    await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
    await page.getByRole("heading", { name: "资源与数据已删除，操作仍在处理", exact: true }).waitFor();
    await progress.getByText("已确认", { exact: true }).waitFor();
    await progress.getByText("退款处理中", { exact: true }).waitFor();
    assert.equal(await page.getByRole("heading", { name: "删除状态暂不可读", exact: true }).count(), 0,
      "the owner's deleting row is expected while its original refund is pending");
    assert.equal(page.url(), detailUrl);

    // The real owner only completes its operation and Workspace row after the
    // refund is confirmed or not applicable. Poll that same original operation.
    deletion = { ...deletion, refundStatus: "confirmed" };
    operation.status = "succeeded";
    operation.stage = "succeeded";
    holdOwnerReadback = true;
    await ownerReadbackHeld;
    await page.getByRole("heading", { name: "正在确认工作空间状态", exact: true }).waitFor({ timeout: 5_000 });
    assert.equal(await page.getByRole("heading", { name: "工作空间已删除", exact: true }).count(), 0);
    holdOwnerReadback = false;
    releaseOwnerReadback();
    await page.getByRole("heading", { name: "删除状态暂不可读", exact: true }).waitFor();
    assert.equal(page.url(), detailUrl, "the Workspace owner row must confirm the deletion too");
    ownerDeleted = true;
    await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
    await page.getByRole("heading", { name: "工作空间已删除", exact: true }).waitFor();
    await progress.getByText("已确认", { exact: true }).waitFor();
    await progress.getByText("退款已完成", { exact: true }).waitFor();
    assert.equal(page.url(), detailUrl);
    assert.equal(writes, 1, "refund polling must never resubmit DELETE");
    assert.ok(requests.filter((request) => request.includes("/operations/")).every((request) => request === `GET /api/v2/operations/workspace/${operation.operationId}`));
    assert.deepEqual(audit, { pageErrors: [], legacyRequests: [], externalRequests: [] });
  } finally {
    releaseOwnerReadback();
    await browser.close();
    await demo.close();
  }
});

test("Cloud Workspace detail keeps 404, authorization, upstream and malformed deletion reads blocked", { timeout: 60_000 }, async () => {
  const demo = await startCloudWorkspaceDeleteBrowser();
  const { origin, workspace } = demo;
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage();
    const audit = await installCloudWorkspaceDeletePage(page, demo);
    let dialogs = 0;
    page.on("dialog", (dialog) => { dialogs += 1; void dialog.dismiss(); });
    const cases = [
      { status: 404, body: { error: "workspace_not_found" }, message: "workspace_not_found" },
      { status: 401, body: { error: "authentication_required" }, message: "authentication_required" },
      { status: 403, body: { error: "workspace_owner_required" }, message: "workspace_owner_required" },
      { status: 503, body: { error: "upstream_unavailable" }, message: "upstream_unavailable" },
      { status: 200, body: {}, message: "invalid_workspace_deletion_response" }
    ];
    for (const failure of cases) {
      const requests: string[] = [];
      const pattern = `**/api/v2/workspaces/${workspace.id}/deletion`;
      await page.route(pattern, async (route) => {
        assert.equal(route.request().method(), "GET");
        requests.push(route.request().method());
        return route.fulfill({ status: failure.status, json: failure.body });
      });
      await page.goto(`${origin}/console/workspaces/${workspace.id}`, { waitUntil: "networkidle" });
      await page.getByRole("heading", { name: "删除状态暂不可读", exact: true }).waitFor({ timeout: 5_000 });
      assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      const error = await page.evaluate(async (workspaceId) => {
        const { getWorkspaceDeletion } = await import("/apps/console-ui/src/api/workspaces-api.ts");
        try {
          await getWorkspaceDeletion(workspaceId);
          return null;
        } catch (error) {
          const failure = error as ApiError;
          return { message: failure.message, status: failure.status, payload: failure.payload };
        }
      }, workspace.id);
      assert.deepEqual(error, failure.status === 200
        ? { message: failure.message, status: undefined, payload: undefined }
        : { message: failure.message, status: failure.status, payload: failure.body });
      assert.equal(dialogs, 0);
      assert.ok(requests.length >= 2);
      await page.unroute(pattern);
    }
    assert.deepEqual(audit, { pageErrors: [], legacyRequests: [], externalRequests: [] });
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Cloud Workspace detail preserves owner DELETE authorization and requires a current session", { timeout: 60_000 }, async () => {
  const demo = await startCloudWorkspaceDeleteBrowser();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage();
    const audit = await installCloudWorkspaceDeletePage(page, demo);
    let writes = 0;
    await page.route(`**/api/v2/workspaces/${demo.workspace.id}/deletion`, (route) => route.fulfill({ json: null }));
    await page.route(`**/api/v2/workspaces/${demo.workspace.id}`, async (route) => {
      if (route.request().method() !== "DELETE") return route.fallback();
      writes += 1;
      assert.equal(route.request().headers()["x-csrf-token"], demo.session.csrfToken);
      assert.deepEqual(route.request().postDataJSON(), { confirmationName: demo.workspace.name, acknowledgeDataDestruction: true });
      return route.fulfill({ status: 403, json: { error: "workspace_owner_required" } });
    });
    await page.goto(`${demo.origin}/console/workspaces/${demo.workspace.id}`, { waitUntil: "networkidle" });
    page.once("dialog", (dialog) => { void dialog.accept(); });
    await page.getByRole("button", { name: "删除工作空间", exact: true }).click();
    await page.getByRole("heading", { name: "删除状态暂不可读", exact: true }).waitFor();
    assert.equal(writes, 1);
    assert.equal(await page.getByRole("heading", { name: "工作空间已删除", exact: true }).count(), 0);
    assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
    await page.route("**/api/v2/auth/session", (route) => route.fulfill({ status: 401, json: { error: "authentication_required" } }));
    await page.reload({ waitUntil: "networkidle" });
    await page.waitForURL(/\/login(?:\?|$)/);
    assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
    assert.equal(writes, 1, "unauthenticated access must never repeat the command");
    assert.deepEqual(audit, { pageErrors: [], legacyRequests: [], externalRequests: [] });
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace deletion preserves the owner polling interval and stops terminal polling", () => {
  const deletion: WorkspaceOwnerDeletionDTO = { workspaceId: "workspace-poll", operationId: "op-poll", resourceDeletionStatus: "pending", dataDeletionStatus: "pending", refundStatus: "unknown", updatedAt: "2026-10-23T00:00:00Z" };
  const operation: WorkspaceOwnerOperationDTO = { owner: "workspace", operationId: deletion.operationId, resourceId: deletion.workspaceId, kind: "delete_workspace", status: "running", stage: "compute_deletion", requestId: "req", createdAt: deletion.updatedAt, updatedAt: deletion.updatedAt, pollAfterSeconds: 30 };
  assert.equal(workspaceApi.projectWorkspaceOwnerDeletion(deletion, operation).pollAfterSeconds, 30);
  assert.equal(workspaceApi.projectWorkspaceOwnerDeletion(deletion, { ...operation, status: "cancelled" }).pollAfterSeconds, undefined);
  assert.equal(workspaceApi.projectWorkspaceOwnerDeletion(deletion, { ...operation, status: "cancelled" }).pageState, "blocked");
});

test("Cloud deletion browser waits for the owner schedule and stops after cancellation", { timeout: 60_000 }, async () => {
  const demo = await startCloudWorkspaceDeleteBrowser();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage();
    await page.clock.install();
    await page.clock.pauseAt(new Date(Date.now() + 1000));
    await installCloudWorkspaceDeletePage(page, demo);
    let reads = 0;
    let cancelled = false;
    await page.route(`**/api/v2/workspaces/${demo.workspace.id}/deletion`, route => route.fulfill({ json: {
      workspaceId: demo.workspace.id, operationId: "op-schedule", resourceDeletionStatus: "pending", dataDeletionStatus: "pending", refundStatus: "unknown", updatedAt: demo.workspace.updatedAt
    } satisfies WorkspaceOwnerDeletionDTO }));
    await page.route("**/api/v2/operations/workspace/op-schedule", route => {
      reads += 1;
      return route.fulfill({ json: { operationId: "op-schedule", resourceId: demo.workspace.id, owner: "workspace", kind: "delete_workspace", status: cancelled ? "cancelled" : "running", stage: "compute_deletion", requestId: "req-schedule", createdAt: demo.workspace.createdAt, updatedAt: demo.workspace.updatedAt, ...(cancelled ? {} : { pollAfterSeconds: 30 }) } satisfies WorkspaceOwnerOperationDTO });
    });
    await page.goto(`${demo.origin}/console/workspaces/${demo.workspace.id}`, { waitUntil: "networkidle" });
    await page.locator("[data-workspace-delete-progress]").getByText("op-schedule", { exact: true }).waitFor();
    assert.equal(reads, 1);
    await page.clock.runFor(29999);
    await page.evaluate(() => undefined);
    assert.equal(reads, 1, "the browser must not poll before the owner's 30-second interval");
    cancelled = true;
    const response = page.waitForResponse(r => new URL(r.url()).pathname === "/api/v2/operations/workspace/op-schedule");
    await page.clock.runFor(1);
    await response;
    await page.getByRole("heading", { name: "删除已暂停，需要核对", exact: true }).waitFor();
    assert.equal(reads, 2);
    await page.clock.runFor(300000);
    await page.evaluate(() => undefined);
    assert.equal(reads, 2, "a cancelled operation must stop polling");
  } finally {
    await browser.close();
    await demo.close();
  }
});
