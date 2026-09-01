import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  SourceEnvelope,
  WorkspaceGatewayBudgetDTO
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

function budgetSource(data: WorkspaceGatewayBudgetDTO): SourceEnvelope<WorkspaceGatewayBudgetDTO> {
  return { source: "sub2api", status: "available", available: true, fetchedAt: "2026-08-26T00:00:00Z", data };
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

async function openAdvancedSettings(page: Page) {
  const details = page.locator("details.workspace-advanced-details");
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}

async function openWorkspace(page: Page, name: string, workspaceId: string) {
  await page.getByRole("button", { name: "Workspace 列表", exact: true }).click();
  await page.waitForURL(/\/console\/workspaces$/);
  await page.locator(".workspace-list-row").filter({ hasText: name }).click();
  await page.waitForURL(new RegExp(`/console/workspaces/${workspaceId}$`));
  await page.getByRole("heading", { name, exact: true }).waitFor({ state: "visible" });
  await openAdvancedSettings(page);
}

test("Workspace Budget keeps its intent across navigation and scopes busy to the active Workspace", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const releaseFirst = deferred<void>();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
    await openAdvancedSettings(page);
    await page.getByRole("button", { name: "重置总额度用量", exact: true }).waitFor({ state: "visible" });

    const response: WorkspaceGatewayBudgetDTO = {
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
      updatedAt: "2026-08-26T00:00:00Z"
    };
    const firstRequest = deferred<Route>();
    const retryObserved = deferred<void>();
    const idempotencyKeys: string[] = [];
    await page.route("**/api/workspaces/ws-1/gateway-budget", async (route) => {
      if (route.request().method() !== "PATCH") {
        await route.continue();
        return;
      }
      idempotencyKeys.push(route.request().headers()["idempotency-key"] || "");
      if (idempotencyKeys.length === 1) {
        firstRequest.resolve(route);
        await releaseFirst.promise;
      }
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(budgetSource(response)) });
      if (idempotencyKeys.length === 2) retryObserved.resolve();
    });

    const firstReset = page.getByRole("button", { name: "重置总额度用量", exact: true });
    await firstReset.click();
    const heldRoute = await firstRequest.promise;
    assert.equal(heldRoute.request().method(), "PATCH");
    assert.equal(await firstReset.getAttribute("aria-busy"), "true");

    await openWorkspace(page, "Second Workspace", "ws-2");
    const secondWorkspaceReset = page.getByRole("button", { name: "重置总额度用量", exact: true });
    assert.equal(await secondWorkspaceReset.isDisabled(), false);
    assert.equal(await secondWorkspaceReset.getAttribute("aria-busy"), null);

    const lateResponse = page.waitForResponse((candidate) => candidate.request() === heldRoute.request());
    releaseFirst.resolve();
    await lateResponse;
    assert.equal(await page.getByText("模型预算已更新", { exact: true }).count(), 0);

    await openWorkspace(page, "Pilot Workspace", "ws-1");
    const retryReset = page.getByRole("button", { name: "重置总额度用量", exact: true });
    assert.equal(await retryReset.isDisabled(), false);
    await retryReset.click();
    await retryObserved.promise;
    await page.getByText("模型预算已更新", { exact: true }).waitFor({ state: "visible" });
    await page.waitForFunction(() => {
      const button = Array.from(document.querySelectorAll<HTMLButtonElement>("button"))
        .find((candidate) => candidate.textContent?.trim() === "重置总额度用量");
      return button?.disabled === false && !button.hasAttribute("aria-busy");
    });

    assert.equal(idempotencyKeys.length, 2);
    assert.match(idempotencyKeys[0], /^workspace-gateway-budget:ws-1:/);
    assert.equal(idempotencyKeys[1], idempotencyKeys[0]);
    assert.equal(await retryReset.isDisabled(), false);
    assert.equal(await retryReset.getAttribute("aria-busy"), null);
  } finally {
    releaseFirst.resolve();
    await browser.close();
    await demo.close();
  }
});
