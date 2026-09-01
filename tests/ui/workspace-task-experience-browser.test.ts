import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page } from "playwright";

import type {
  PricingCatalogResponse,
  PricingPreviewResponse,
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
  } finally {
    await browser.close();
    await demo.close();
  }
});
