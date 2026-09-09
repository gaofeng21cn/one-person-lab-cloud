import assert from "node:assert/strict";
import { afterEach, test } from "node:test";

import { chromium } from "playwright";

import * as workspaceApi from "../../apps/console-ui/src/api/workspaces-api.ts";
import type { SourceEnvelope, WorkspaceDeleteResponse, WorkspaceDeletionDTO, WorkspaceListData } from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";

async function openAdvancedSettings(page: import("playwright").Page) {
  const details = page.locator("details.workspace-advanced-details");
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
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

  const result = await workspaceApi.deleteWorkspace("workspace / alpha", "csrf-alpha", "delete-once");

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

  assert.deepEqual(await workspaceApi.deleteWorkspace("workspace-alpha", "csrf-alpha", "delete-once"), {
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
    () => workspaceApi.deleteWorkspace("workspace-alpha", "csrf-alpha", "delete-once"),
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

test("Workspace delete scopes busy and reuses its intent after a late response", async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  let releaseDelete: (() => void) | undefined;
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

    page.once("dialog", (dialog) => { void dialog.accept(); });
    const retryDelete = page.getByRole("button", { name: "删除工作空间", exact: true });
    assert.equal(await retryDelete.getAttribute("aria-busy"), null);
    assert.equal(await retryDelete.isDisabled(), false);
    await retryDelete.click();
    await retryObserved;
    await page.waitForURL(/\/console\/workspaces$/);
    await page.getByText("Workspace 已删除", { exact: true }).waitFor({ state: "visible" });

    assert.equal(idempotencyKeys.length, 2);
    assert.match(idempotencyKeys[0], /^workspace-delete:/);
    assert.equal(idempotencyKeys[1], idempotencyKeys[0]);
    assert.equal(await page.locator(".workspace-list-row").filter({ hasText: "Pilot Workspace" }).count(), 0);
  } finally {
    releaseDelete?.();
    await browser.close();
    await demo.close();
  }
});

test("Workspace deletion survives closing the page and remains pending until its original receipt and absence are confirmed", { timeout: 90_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
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
        deletion = { workspaceId: "ws-1", operationId: "delete-original-ws-1", status: "pending", phase: "runtime" };
        await route.fulfill({ status: 202, contentType: "application/json", body: JSON.stringify(deletion) });
      });
      let page = await context.newPage();
      await page.goto(`${demo.origin}/login`, { waitUntil: "domcontentloaded" });
      await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
      await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
      await page.getByRole("button", { name: "登录", exact: true }).click();
      await page.waitForURL(/\/console\/overview$/);
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      page.once("dialog", (dialog) => {
        assert.match(dialog.message(), /请先自行下载/);
        assert.match(dialog.message(), /关闭页面后仍会继续处理/);
        assert.match(dialog.message(), /不会自动退款/);
        void dialog.accept();
      });
      await page.getByRole("button", { name: "删除工作空间", exact: true }).click();
      await page.getByRole("heading", { name: "正在删除工作空间", exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 1);
      assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).count(), 0);
      assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      await page.close();

      deletion = { workspaceId: "ws-1", operationId: "delete-original-ws-1", status: "pending", phase: "compute" };
      page = await context.newPage();
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      await page.getByRole("heading", { name: "正在删除工作空间", exact: true }).waitFor({ state: "visible" });
      assert.equal(writes, 1, "reopening reads the original operation without resubmitting DELETE");
      absent = true;
      deletion = { ...deletion, phase: "receipt" };
      await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await page.getByRole("heading", { name: "正在删除工作空间", exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByText("Workspace 已删除", { exact: true }).count(), 0);
      deletion = { ...deletion, status: "manual_review" };
      await page.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await page.getByRole("heading", { name: "删除需要核对", exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
      deletion = { ...deletion, status: "deleted", phase: "complete", receiptId: "receipt-delete-original-ws-1" };
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
