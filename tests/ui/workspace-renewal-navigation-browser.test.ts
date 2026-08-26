import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData,
  WorkspaceRenewalResponse
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

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
  await page.getByRole("button", { name: "Workspace 列表", exact: true }).click();
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
      effectiveAfter: "2026-08-26T00:00:00Z",
      nextRenewalAt: "2026-09-26T00:00:00Z",
      paidThrough: "2026-09-26T00:00:00Z",
      renewalStatus: "active"
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

    assert.equal(idempotencyKeys.length, 2);
    assert.match(idempotencyKeys[0], /^workspace-renewal:ws-1:/);
    assert.equal(idempotencyKeys[1], idempotencyKeys[0]);
  } finally {
    releaseFirst.resolve();
    await browser.close();
    await demo.close();
  }
});
