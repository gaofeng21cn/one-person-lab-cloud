import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page } from "playwright";

import type {
  PricingCatalogResponse,
  PricingPreviewResponse,
  SourceEnvelope,
  WorkspaceListData,
  WorkspaceLaunchResponse
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const viewports = [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 390, height: 844 }
] as const;

const pendingLaunch: WorkspaceLaunchResponse = {
  operationId: `launch-${"pending-technical-value-".repeat(8)}`,
  status: "pending",
  phase: "runtime",
  accountId: "acct-1",
  name: "Pending Workspace",
  packageId: "basic",
  sizeGb: 10,
  autoRenew: false,
  priceVersion: "pilot-usd-2026-07-v1",
  currency: "USD",
  totalChargeUsdMicros: 52_580_000,
  errorCode: "runtime_waiting",
  blockReason: "provider_capacity_pending",
  checks: [{ name: "fabric_runtime_ready", ok: false }],
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:01:00Z"
};

const customerOwnedCatalog: PricingCatalogResponse = {
  priceVersion: "pilot-usd-2026-07-v1",
  billingUnit: "calendar_month",
  displayCurrency: "USD",
  walletCurrency: "USD",
  currency: "USD",
  resourceBillingMode: "none",
  packages: [
    { id: "basic", name: "Basic", available: true },
    { id: "pro", name: "Pro", available: true }
  ]
};

const billedCatalog: PricingCatalogResponse = {
  ...customerOwnedCatalog,
  resourceBillingMode: "enabled"
};

const unavailablePreview: PricingPreviewResponse = {
  resourceType: "workspace",
  packageId: "basic",
  priceVersion: "pilot-usd-2026-07-v1",
  currency: "USD"
};

interface BrowserAudit {
  consoleErrors: string[];
  externalRequests: string[];
  pageErrors: string[];
}

const viteClientWithoutHmrTransport = `
const styles = new Map();
export class ErrorOverlay extends HTMLElement {}
export function createHotContext() {
  return {
    data: {},
    accept() {},
    acceptExports() {},
    decline() {},
    dispose() {},
    invalidate() {},
    off() {},
    on() {},
    prune() {},
    send() {}
  };
}
export function injectQuery(url) { return url; }
export function updateStyle(id, content) {
  let style = styles.get(id);
  if (!style) {
    style = document.createElement("style");
    style.setAttribute("data-vite-dev-id", id);
    document.head.appendChild(style);
    styles.set(id, style);
  }
  style.textContent = content;
}
export function removeStyle(id) {
  const style = styles.get(id);
  if (!style) return;
  style.remove();
  styles.delete(id);
}
`;

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

async function installBrowserAudit(page: Page, origin: string): Promise<BrowserAudit> {
  const audit: BrowserAudit = { consoleErrors: [], externalRequests: [], pageErrors: [] };
  page.on("pageerror", (error) => audit.pageErrors.push(error.stack || error.message));
  page.on("console", (message) => {
    if (message.type() === "error") audit.consoleErrors.push(message.text());
  });
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin === origin && url.pathname === "/@vite/client") {
      await route.fulfill({
        status: 200,
        contentType: "application/javascript",
        body: viteClientWithoutHmrTransport
      });
      return;
    }
    if (url.origin === origin || url.protocol === "data:" || url.protocol === "blob:") {
      await route.continue();
      return;
    }
    audit.externalRequests.push(`${request.method()} ${request.url()}`);
    await route.abort("blockedbyclient");
  });
  return audit;
}

function assertBrowserAuditClean(audit: BrowserAudit) {
  assert.deepEqual(audit.externalRequests, []);
  assert.deepEqual(audit.pageErrors, []);
  assert.deepEqual(audit.consoleErrors, []);
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(/\/console\/overview$/);
}

async function assertNoHorizontalOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({
    viewportWidth: window.innerWidth,
    documentWidth: document.documentElement.scrollWidth,
    bodyWidth: document.body.scrollWidth
  }));
  assert.ok(
    dimensions.documentWidth <= dimensions.viewportWidth + 1,
    `document overflow: ${JSON.stringify(dimensions)}`
  );
  assert.ok(
    dimensions.bodyWidth <= dimensions.viewportWidth + 1,
    `body overflow: ${JSON.stringify(dimensions)}`
  );
}

async function assertTechnicalEvidenceClosed(page: Page) {
  for (const value of [
    "operation ID",
    pendingLaunch.operationId,
    pendingLaunch.status,
    pendingLaunch.phase,
    pendingLaunch.errorCode!,
    pendingLaunch.blockReason!,
    pendingLaunch.checks![0].name
  ]) {
    assert.equal(await page.getByText(value, { exact: true }).isVisible(), false, `${value} should be hidden`);
  }
}

async function assertTechnicalEvidenceOpen(page: Page) {
  for (const value of [
    "operation ID",
    pendingLaunch.operationId,
    pendingLaunch.status,
    pendingLaunch.phase,
    pendingLaunch.errorCode!,
    pendingLaunch.blockReason!,
    pendingLaunch.checks![0].name
  ]) {
    await page.getByText(value, { exact: true }).waitFor({ state: "visible" });
  }
}

test("pending launch keeps raw evidence behind technical details at desktop and mobile widths", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  demo.state.launches = [pendingLaunch];
  try {
    for (const viewport of viewports) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const audit = await installBrowserAudit(page, demo.origin);
      await login(page, demo.origin);

      await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "domcontentloaded" });
      const compact = page.locator(".launch-operation--compact");
      await compact.getByRole("heading", { name: "正在准备工作空间", exact: true }).waitFor({ state: "visible" });
      await compact.getByText("启动工作空间", { exact: true }).waitFor({ state: "visible" });
      await assertTechnicalEvidenceClosed(page);
      await compact.getByText("技术详情", { exact: true }).click();
      await assertTechnicalEvidenceOpen(page);
      await assertNoHorizontalOverflow(page);

      await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "domcontentloaded" });
      const full = page.locator(".workspace-launch-page .launch-operation");
      await full.getByRole("heading", { name: "正在准备工作空间", exact: true }).waitFor({ state: "visible" });
      await assertTechnicalEvidenceClosed(page);
      await full.getByText("技术详情", { exact: true }).click();
      await assertTechnicalEvidenceOpen(page);
      await assertNoHorizontalOverflow(page);
      assertBrowserAuditClean(audit);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("customer entitlement shows authoritative zero due without prepayment language", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await page.route("**/api/pricing/catalog", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(customerOwnedCatalog)
      });
    });
    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });

    const actualDue = page.locator(".workspace-order-summary__total");
    await actualDue.getByText("实际应付", { exact: true }).waitFor({ state: "visible" });
    await actualDue.getByText("$0.00", { exact: true }).waitFor({ state: "visible" });
    assert.ok(await page.getByText("$52.58", { exact: true }).count() > 0, "positive preview remains reference evidence");

    await page.getByLabel("Workspace 名称").fill("Customer Entitlement Workspace");
    await page.getByRole("button", { name: "核对开通信息", exact: true }).click();
    await page.getByRole("checkbox", {
      name: "我确认使用当前客户权益开通工作空间（无需预付）",
      exact: true
    }).click();
    await page.getByRole("button", { name: "确认并开通", exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: /预付/ }).count(), 0);
    await assertNoHorizontalOverflow(page);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("unavailable quote remains distinct from an authoritative zero price", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await page.route("**/api/pricing/catalog", async (route) => {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(billedCatalog) });
    });
    await page.route("**/api/pricing/preview", async (route) => {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(unavailablePreview) });
    });
    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });

    const actualDue = page.locator(".workspace-order-summary__total");
    await actualDue.getByText("实际应付", { exact: true }).waitFor({ state: "visible" });
    await actualDue.getByText("暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await actualDue.getByText("$0.00", { exact: true }).count(), 0);
    assert.equal(await page.getByRole("button", { name: "核对开通信息", exact: true }).isDisabled(), true);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("readback refresh retains the succeeded launch and retries only authoritative Workspace discovery", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const retryReadStarted = deferred();
  const releaseRetryRead = deferred();
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    const unavailableReadback: SourceEnvelope<WorkspaceListData> = {
      source: "control-plane",
      status: "unavailable",
      available: false,
      fetchedAt: "2026-09-01T00:00:00Z",
      reasonCode: "workspace_readback_temporarily_unavailable"
    };
    let authoritativeReads = 0;
    await page.route("**/api/workspaces?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("pageSize") !== "50") {
        await route.fallback();
        return;
      }
      authoritativeReads += 1;
      if (authoritativeReads === 1) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(unavailableReadback)
        });
        return;
      }
      if (authoritativeReads === 2) {
        retryReadStarted.resolve();
        await releaseRetryRead.promise;
      }
      await route.fallback();
    });

    let launchPostCount = 0;
    const launchIdempotencyKeys = new Set<string>();
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (request.method() !== "POST" || url.pathname !== "/api/workspace-launches") return;
      launchPostCount += 1;
      launchIdempotencyKeys.add(request.headers()["idempotency-key"] || "");
    });

    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });
    await page.getByLabel("Workspace 名称").fill("Readback Retry Workspace");
    await page.getByRole("button", { name: "核对开通信息", exact: true }).click();
    await page.getByRole("checkbox", {
      name: "我确认一次性预付工作空间月度总额并开通",
      exact: true
    }).click();

    const launchResponsePromise = page.waitForResponse((response) => {
      const request = response.request();
      return request.method() === "POST" && new URL(response.url()).pathname === "/api/workspace-launches";
    });
    await page.getByRole("button", { name: "确认预付并开通", exact: true }).click();
    const launchResponse = await launchResponsePromise;
    const launch = await launchResponse.json() as WorkspaceLaunchResponse;
    assert.equal(launch.status, "succeeded");
    assert.ok(launch.workspaceId);
    await page.getByRole("heading", { name: "结果待确认", exact: true }).waitFor({ state: "visible" });
    await page.getByText("当前开通结果尚未确认，请刷新状态，暂勿重复购买。", { exact: true }).waitFor({ state: "visible" });

    const retryRequest = page.waitForRequest((request) => {
      const url = new URL(request.url());
      return request.method() === "GET" && url.pathname === "/api/workspaces" && url.searchParams.get("pageSize") === "50";
    }, { timeout: 3_000 });
    await page.getByRole("button", { name: "刷新状态", exact: true }).click();
    await retryRequest;
    await retryReadStarted.promise;

    await page.getByRole("heading", { name: "结果待确认", exact: true }).waitFor({ state: "visible" });
    assert.equal(authoritativeReads, 2);
    assert.equal(await page.getByRole("button", { name: /确认.*开通/ }).count(), 0);
    assert.equal(await page.getByRole("checkbox", { name: /确认.*开通/ }).count(), 0);
    assert.equal(launchPostCount, 1);
    assert.equal(launchIdempotencyKeys.size, 1);
    assert.notEqual([...launchIdempotencyKeys][0], "");

    releaseRetryRead.resolve();
    await page.waitForURL((url) => url.pathname === `/console/workspaces/${encodeURIComponent(launch.workspaceId!)}`);
    await page.getByRole("heading", { name: "Readback Retry Workspace", exact: true }).waitFor({ state: "visible" });
    assert.equal(launchPostCount, 1);
    assert.equal(launchIdempotencyKeys.size, 1);
    assertBrowserAuditClean(audit);
  } finally {
    releaseRetryRead.resolve();
    await browser.close();
    await demo.close();
  }
});
