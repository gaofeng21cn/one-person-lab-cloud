import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Browser, type BrowserContext, type Locator, type Page } from "playwright";

import type {
  PricingCatalogResponse,
  PricingPreviewResponse,
  RuntimeCredentialResponse,
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceListData,
  WorkspaceLaunchResponse,
  WorkspaceRuntimeDTO
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

async function visibleTextCount(page: Page, value: string | RegExp) {
  const matches = page.getByRole("main").getByText(value, { exact: typeof value === "string" });
  let visible = 0;
  for (let index = 0; index < await matches.count(); index += 1) {
    if (await matches.nth(index).isVisible()) visible += 1;
  }
  return visible;
}

async function focusByKeyboard(page: Page, selector: string) {
  const target = page.locator(selector);
  for (let index = 0; index < 40; index += 1) {
    await page.keyboard.press("Tab");
    if (await target.evaluate((element) => document.activeElement === element)) return target;
  }
  assert.fail(`keyboard focus did not reach ${selector}`);
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

async function credentialIsRevealed(row: Locator) {
  return row.locator("code").evaluate((element) => {
    const value = element.textContent || "";
    return value.length > 0 && value !== "-" && !value.includes("•");
  });
}

async function assertWorkspaceCustomerSurfaceDoesNotExposeImplementationTerms(page: Page) {
  for (const value of [
    "Workspace Key",
    "operation ID",
    "errorCode",
    "reasonCode",
    "manual"
  ]) {
    assert.equal(await visibleTextCount(page, value), 0, `${value} should not be visible by default`);
  }
  assert.equal(await visibleTextCount(page, /Runtime/i), 0, "Runtime terms should not be visible by default");
  assert.equal(await visibleTextCount(page, /Secret/), 0, "Secret should not be visible by default");
  assert.equal(await visibleTextCount(page, /micros/i), 0, "micros should not be visible by default");
}

test("customer completes one authoritative Workspace journey at desktop and mobile widths", { timeout: 120_000 }, async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of viewports) {
      await verifyWorkspaceCustomerJourney(browser, viewport);
    }
  } finally {
    await browser.close();
  }
});

test("Workspace detail prioritizes authoritative availability and entry while keeping policy and evidence disclosed", { timeout: 60_000 }, verifyWorkspaceDetailExperience);

test("multiple active Workspace launches block repeat purchase until recovery is unambiguous", { timeout: 30_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const conflictingLaunches: WorkspaceLaunchResponse[] = [
    pendingLaunch,
    {
      ...pendingLaunch,
      operationId: "launch-manual-review-conflict",
      status: "manual_review",
      updatedAt: "2026-09-01T00:02:00Z"
    }
  ];
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    let launchListReadCount = 0;
    let launchPostCount = 0;
    let recoveryResult: "conflict" | "unavailable" | "clear" = "conflict";
    const initialRecovery = deferred();
    await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspace-launches", async (route) => {
      if (route.request().method() === "GET") {
        launchListReadCount += 1;
        if (launchListReadCount === 1) await initialRecovery.promise;
        if (recoveryResult === "unavailable") {
          await route.fulfill({ status: 200, contentType: "application/json", body: "null" });
        } else {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify(recoveryResult === "conflict" ? conflictingLaunches : [])
          });
        }
        return;
      }
      if (route.request().method() === "POST") launchPostCount += 1;
      await route.fallback();
    });

    await login(page, demo.origin);
    const launchPageNavigation = page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });

    await page.getByText("正在确认是否存在未完成的开通操作", { exact: true }).waitFor({ state: "visible" });
    assert.equal(launchListReadCount, 1);
    assert.equal(await page.getByLabel("工作空间名称").count(), 0);
    assert.equal(await page.getByRole("button", { name: "核对开通信息", exact: true }).count(), 0);
    assert.equal(await page.getByRole("button", { name: "确认预付并开通", exact: true }).count(), 0);
    assert.equal(launchPostCount, 0);

    initialRecovery.resolve();
    await launchPageNavigation;

    await page.getByText("存在多个待确认的开通操作", { exact: true }).waitFor({ state: "visible" });
    await page.getByText("为避免重复扣费，请暂勿再次购买。刷新后确认仅有一个或没有未完成操作，才能继续开通。", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByLabel("工作空间名称").count(), 0);
    assert.equal(await page.getByRole("button", { name: "核对开通信息", exact: true }).count(), 0);
    assert.equal(await page.getByRole("button", { name: "确认预付并开通", exact: true }).count(), 0);
    assert.equal(launchPostCount, 0);

    const refreshResponse = page.waitForResponse((response) => {
      const request = response.request();
      return request.method() === "GET" && new URL(response.url()).pathname === "/api/workspace-launches";
    });
    await page.getByRole("button", { name: "重新检查", exact: true }).click();
    await refreshResponse;
    await page.getByText("存在多个待确认的开通操作", { exact: true }).waitFor({ state: "visible" });
    assert.equal(launchListReadCount, 2);
    assert.equal(await page.getByLabel("工作空间名称").count(), 0);
    assert.equal(launchPostCount, 0);

    recoveryResult = "unavailable";
    const unavailableResponse = page.waitForResponse((response) => {
      const request = response.request();
      return request.method() === "GET" && new URL(response.url()).pathname === "/api/workspace-launches";
    });
    await page.getByRole("button", { name: "重新检查", exact: true }).click();
    await unavailableResponse;
    await page.getByText("暂时无法确认开通状态", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByLabel("工作空间名称").count(), 0);
    assert.equal(launchPostCount, 0);

    recoveryResult = "clear";
    const clearResponse = page.waitForResponse((response) => {
      const request = response.request();
      return request.method() === "GET" && new URL(response.url()).pathname === "/api/workspace-launches";
    });
    await page.getByRole("button", { name: "重新检查", exact: true }).click();
    await clearResponse;
    await page.getByLabel("工作空间名称").waitFor({ state: "visible" });
    await page.getByRole("button", { name: "核对开通信息", exact: true }).waitFor({ state: "visible" });
    assert.equal(launchListReadCount, 4);
    assert.equal(launchPostCount, 0);
    await assertNoHorizontalOverflow(page);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("succeeded launch without a Workspace identity keeps raw success behind technical details", { timeout: 30_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const malformedSuccess: WorkspaceLaunchResponse = {
    operationId: "launch-missing-workspace-identity",
    status: "succeeded",
    phase: "receipt",
    accountId: "acct-1",
    name: "Missing Identity Workspace",
    packageId: "basic",
    sizeGb: 10,
    autoRenew: false,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    totalChargeUsdMicros: 52_580_000,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:01:00Z"
  };
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    let launchPostCount = 0;
    let authoritativeReadCount = 0;
    let launchIdempotencyKey = "";
    await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspace-launches", async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }
      launchPostCount += 1;
      launchIdempotencyKey = route.request().headers()["idempotency-key"] || "";
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(malformedSuccess) });
    });
    await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspaces", async (route) => {
      const requestUrl = new URL(route.request().url());
      if (requestUrl.searchParams.get("pageSize") === "50") authoritativeReadCount += 1;
      await route.fallback();
    });

    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });
    await page.getByLabel("工作空间名称").fill(malformedSuccess.name);
    await page.getByRole("button", { name: "核对开通信息", exact: true }).click();
    await page.getByRole("checkbox", {
      name: "我确认一次性预付工作空间月度总额并开通",
      exact: true
    }).click();
    await page.getByRole("button", { name: "确认预付并开通", exact: true }).click();

    await page.getByRole("heading", { name: "结果待确认", exact: true }).waitFor({ state: "visible" });
    await page.getByText("当前开通结果尚未确认，请刷新状态，暂勿重复购买。", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("heading", { name: "工作空间已可使用", exact: true }).count(), 0);
    assert.equal(await visibleTextCount(page, "工作空间已完成开通，可以继续查看并进入。"), 0);
    assert.equal(await page.getByRole("button", { name: "查看工作空间", exact: true }).count(), 0);
    assert.equal(await visibleTextCount(page, "succeeded"), 0);
    assert.equal(authoritativeReadCount, 0);
    assert.equal(launchPostCount, 1);
    assert.notEqual(launchIdempotencyKey, "");

    const technical = page.locator("details.launch-technical-details");
    await technical.locator("summary").click();
    const statusCode = technical.locator(".operation-readback dt")
      .filter({ hasText: /^status$/ })
      .locator("xpath=following-sibling::dd/code");
    await statusCode.waitFor({ state: "visible" });
    assert.equal(await statusCode.textContent(), "succeeded");
    await assertNoHorizontalOverflow(page);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace detail fails closed without exposing Runtime or delete reason codes by default", { timeout: 30_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await page.route("**/api/workspaces/ws-1/runtime-status", async (route) => {
      const unavailableRuntime: SourceEnvelope<never> = {
        source: "fabric",
        status: "unavailable",
        available: false,
        fetchedAt: "2026-09-01T00:00:00Z",
        reasonCode: "fabric_runtime_temporarily_unavailable"
      };
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(unavailableRuntime) });
    });
    let deleteWrites = 0;
    await page.route("**/api/workspaces/ws-1", async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.fallback();
        return;
      }
      deleteWrites += 1;
      await route.fulfill({
        status: 405,
        contentType: "application/json",
        body: JSON.stringify({ error: "method_not_allowed" })
      });
    });

    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });
    await page.getByText("入口暂不可用", { exact: true }).waitFor({ state: "visible" });
    await page.getByText("访问凭据暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).isDisabled(), true);
    assert.equal(await visibleTextCount(page, "fabric_runtime_temporarily_unavailable"), 0);

    const advanced = page.locator("details.workspace-advanced-details");
    await advanced.locator("summary").click();
    page.once("dialog", (dialog) => { void dialog.accept(); });
    await advanced.getByRole("button", { name: "删除工作空间", exact: true }).click();
    await advanced.getByText("工作空间删除暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(deleteWrites, 1);
    assert.equal(await visibleTextCount(page, "workspace_delete_unavailable"), 0);

    const technical = page.locator("details.workspace-technical-details");
    await technical.locator("summary").click();
    await technical.getByText("fabric_runtime_temporarily_unavailable", { exact: true }).waitFor({ state: "visible" });
    await technical.getByText("workspace_delete_unavailable", { exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(audit.consoleErrors, ["Failed to load resource: the server responded with a status of 405 (Method Not Allowed)"]);
    audit.consoleErrors.length = 0;
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace credential mismatch uses customer terminology", { timeout: 30_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    const mismatchedCredential: RuntimeCredentialResponse = {
      workspaceId: "ws-other",
      access: {
        account: "opl",
        username: "opl",
        password: "mismatched-password",
        credentialStatus: "configured",
        credentialVersion: "1"
      }
    };
    await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspaces/ws-1/runtime-credentials/reveal", async (route) => {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(mismatchedCredential) });
    });

    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });
    const passwordRow = page.locator(".workspace-access-panel .data-list > div").filter({ hasText: "登录密码" }).first();
    await passwordRow.getByRole("button", { name: "显示", exact: true }).click();
    await page.getByText("登录凭据暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await visibleTextCount(page, "Workspace 凭证暂不可用"), 0);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

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

    await page.getByLabel("工作空间名称").fill("Customer Entitlement Workspace");
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
    await page.getByLabel("工作空间名称").fill("Readback Retry Workspace");
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

async function verifyWorkspaceCustomerJourney(browser: Browser, viewport: typeof viewports[number]) {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  let context: BrowserContext | null = null;
  const authoritativeReadStarted = deferred();
  const customerOpenReadStarted = deferred();
  const releaseAuthoritativeRead = deferred();
  let launchPostCount = 0;
  let launchedWorkspaceId = "";
  let workspaceDtoFixtureReads = 0;
  let runtimeFixtureReads = 0;
  const authoritativeReadRequests: Array<{ page: string | null; pageSize: string | null }> = [];
  const launchIdempotencyKeys = new Set<string>();
  const journeyName = `Customer Journey ${viewport.name}`;

  try {
    context = await browser.newContext({
      viewport,
      permissions: ["clipboard-read", "clipboard-write"]
    });
    const page = await context.newPage();
    const audit = await installBrowserAudit(page, demo.origin);
    await page.addInitScript({ content: `
      window.openedWorkspace = null;
      window.open = (url, target, features) => {
        window.openedWorkspace = {
          url: String(url || ""),
          target: String(target || ""),
          features: String(features || "")
        };
        return null;
      };
    ` });

    await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspace-launches", async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }
      launchPostCount += 1;
      launchIdempotencyKeys.add(route.request().headers()["idempotency-key"] || "");
      const upstream = await route.fetch();
      const operation = await upstream.json() as WorkspaceLaunchResponse;
      launchedWorkspaceId = operation.workspaceId || "";
      await route.fulfill({ response: upstream, body: JSON.stringify(operation) });
    });

    await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspaces", async (route) => {
      const requestUrl = new URL(route.request().url());
      if (route.request().method() !== "GET" || requestUrl.searchParams.get("pageSize") !== "50" || !launchedWorkspaceId) {
        await route.fallback();
        return;
      }
      authoritativeReadRequests.push({
        page: requestUrl.searchParams.get("page"),
        pageSize: requestUrl.searchParams.get("pageSize")
      });
      const upstream = await route.fetch();
      const payload = await upstream.json() as SourceEnvelope<WorkspaceListData>;
      assert.equal(payload.available, true, "authoritative Workspace readback must be available");
      if (!payload.available) return;
      const workspaceDtoUrl = `https://dto-entry.example.invalid/w/${launchedWorkspaceId}/`;
      const items = payload.data.items.map((workspace) => workspace.id === launchedWorkspaceId
        ? { ...workspace, url: workspaceDtoUrl }
        : workspace);
      if (items.some((workspace) => workspace.id === launchedWorkspaceId)) workspaceDtoFixtureReads += 1;
      if (authoritativeReadRequests.length === 1) {
        authoritativeReadStarted.resolve();
      } else if (authoritativeReadRequests.length === 2) {
        customerOpenReadStarted.resolve();
      }
      await releaseAuthoritativeRead.promise;
      await route.fulfill({
        response: upstream,
        body: JSON.stringify({ ...payload, data: { ...payload.data, items } })
      });
    });

    await page.route((url) => url.origin === demo.origin && /\/api\/workspaces\/[^/]+\/runtime-status$/.test(url.pathname), async (route) => {
      const workspaceId = decodeURIComponent(new URL(route.request().url()).pathname.split("/")[3] || "");
      if (route.request().method() !== "GET" || workspaceId !== launchedWorkspaceId) {
        await route.fallback();
        return;
      }
      const upstream = await route.fetch();
      const payload = await upstream.json() as SourceEnvelope<WorkspaceRuntimeDTO>;
      assert.equal(payload.available, true, "Fabric Runtime read must be available");
      if (!payload.available) return;
      runtimeFixtureReads += 1;
      await route.fulfill({
        response: upstream,
        body: JSON.stringify({
          ...payload,
          data: {
            ...payload.data,
            url: `https://runtime-entry.example.invalid/w/${launchedWorkspaceId}/`
          }
        })
      });
    });

    await login(page, demo.origin);
    await page.getByRole("link", { name: "工作空间", exact: true }).filter({ visible: true }).first().click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces");
    await page.getByRole("button", { name: "新建工作空间", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces/new");
    await page.getByRole("heading", { name: "新建工作空间", exact: true }).waitFor({ state: "visible" });
    await assertNoHorizontalOverflow(page);

    const basicPlan = page.getByRole("radio", { name: /Basic/ });
    await basicPlan.waitFor({ state: "visible" });
    await basicPlan.click();
    assert.equal(await basicPlan.isChecked(), true);
    await page.getByLabel("工作空间名称").fill(journeyName);
    const actualDue = page.locator(".workspace-order-summary__total");
    await actualDue.getByText("实际应付", { exact: true }).waitFor({ state: "visible" });
    await actualDue.getByText("$52.58", { exact: true }).waitFor({ state: "visible" });
    await page.getByRole("button", { name: "核对开通信息", exact: true }).click();
    await page.getByRole("heading", { name: "确认开通信息", exact: true }).waitFor({ state: "visible" });
    await actualDue.getByText("$52.58", { exact: true }).waitFor({ state: "visible" });
    const confirmation = page.getByRole("checkbox", {
      name: "我确认一次性预付工作空间月度总额并开通",
      exact: true
    });
    await confirmation.click();
    await page.getByRole("button", { name: "确认预付并开通", exact: true }).waitFor({ state: "visible" });
    await assertNoHorizontalOverflow(page);

    const launchResponsePromise = page.waitForResponse((response) => {
      const request = response.request();
      return request.method() === "POST" && new URL(response.url()).pathname === "/api/workspace-launches";
    });
    await page.getByRole("button", { name: "确认预付并开通", exact: true }).click();
    const launchResponse = await launchResponsePromise;
    const launch = await launchResponse.json() as WorkspaceLaunchResponse;
    assert.equal(launch.status, "succeeded");
    assert.ok(launch.workspaceId);
    await authoritativeReadStarted.promise;
    assert.equal(new URL(page.url()).pathname, "/console/workspaces/new");
    assert.equal(await page.locator(".workspace-identity-panel").isVisible(), false);
    assert.deepEqual(authoritativeReadRequests[0], { page: "1", pageSize: "50" });
    await page.getByRole("heading", { name: "工作空间已可使用", exact: true }).waitFor({ state: "visible" });
    const viewWorkspace = page.getByRole("button", { name: "查看工作空间", exact: true });
    await viewWorkspace.waitFor({ state: "visible" });
    await viewWorkspace.click();
    await customerOpenReadStarted.promise;
    assert.equal(new URL(page.url()).pathname, "/console/workspaces/new");
    assert.equal(await page.locator(".workspace-identity-panel").isVisible(), false);
    assert.deepEqual(authoritativeReadRequests, [
      { page: "1", pageSize: "50" },
      { page: "1", pageSize: "50" }
    ]);
    assert.equal(launchPostCount, 1);
    assert.equal(launchIdempotencyKeys.size, 1);
    assert.notEqual([...launchIdempotencyKeys][0], "");

    releaseAuthoritativeRead.resolve();
    await page.waitForURL((url) => url.pathname === `/console/workspaces/${encodeURIComponent(launch.workspaceId!)}`);
    const identity = page.locator(".workspace-identity-panel");
    await identity.getByRole("heading", { name: journeyName, exact: true }).waitFor({ state: "visible" });
    await identity.getByText("可使用", { exact: true }).waitFor({ state: "visible" });
    await identity.getByText("BASIC", { exact: true }).waitFor({ state: "visible" });
    await identity.getByText("$52.58", { exact: true }).waitFor({ state: "visible" });
    await identity.getByText("2026/08/19", { exact: true }).waitFor({ state: "visible" });
    await assertWorkspaceCustomerSurfaceDoesNotExposeImplementationTerms(page);
    await assertNoHorizontalOverflow(page);

    const access = page.locator(".workspace-access-panel");
    const passwordRow = access.locator(".data-list > div").filter({ hasText: "登录密码" }).first();
    const keyRow = access.locator(".data-list > div").filter({ hasText: "API 密钥" }).first();
    assert.equal(await credentialIsRevealed(passwordRow), false);
    assert.equal(await credentialIsRevealed(keyRow), false);
    await passwordRow.getByRole("button", { name: "显示", exact: true }).click();
    await page.waitForFunction(() => {
      const row = [...document.querySelectorAll(".workspace-access-panel .data-list > div")]
        .find((candidate) => candidate.textContent?.includes("登录密码"));
      return Boolean(row?.querySelector("code")?.textContent?.replaceAll("•", ""));
    });
    assert.equal(await credentialIsRevealed(passwordRow), true);
    assert.equal(await credentialIsRevealed(keyRow), false);
    const revealedPassword = await passwordRow.locator("code").textContent();
    assert.equal(Boolean(revealedPassword), true, "revealed password must be present");
    await passwordRow.getByRole("button", { name: "复制", exact: true }).click();
    await page.getByText("登录密码已复制", { exact: true }).waitFor({ state: "visible" });
    const copiedPassword = await page.evaluate(() => navigator.clipboard.readText());
    assert.equal(copiedPassword === revealedPassword, true, "clipboard must equal the revealed password");

    await keyRow.getByRole("button", { name: "显示", exact: true }).click();
    await page.waitForFunction(() => {
      const row = [...document.querySelectorAll(".workspace-access-panel .data-list > div")]
        .find((candidate) => candidate.textContent?.includes("API 密钥"));
      return Boolean(row?.querySelector("code")?.textContent?.replaceAll("•", ""));
    });
    assert.equal(await credentialIsRevealed(passwordRow), false);
    assert.equal(await credentialIsRevealed(keyRow), true);
    const revealedKey = await keyRow.locator("code").textContent();
    assert.equal(Boolean(revealedKey), true, "revealed API key must be present");
    assert.equal(revealedKey !== revealedPassword, true, "password and API key must differ");
    await keyRow.getByRole("button", { name: "复制", exact: true }).click();
    await page.getByText("API 密钥已复制", { exact: true }).waitFor({ state: "visible" });
    const copiedKey = await page.evaluate(() => navigator.clipboard.readText());
    assert.equal(copiedKey === revealedKey, true, "clipboard must equal the revealed API key");
    assert.equal(copiedKey !== copiedPassword, true, "copied password and API key must differ");

    const openWorkspace = identity.getByRole("button", { name: "打开工作空间", exact: true });
    await openWorkspace.click();
    const openedWorkspace = await page.evaluate(() => (window as Window & {
      openedWorkspace?: { url: string; target: string; features: string } | null;
    }).openedWorkspace);
    const expectedRuntimeUrl = `https://runtime-entry.example.invalid/w/${launch.workspaceId}/`;
    const workspaceDtoUrl = `https://dto-entry.example.invalid/w/${launch.workspaceId}/`;
    assert.deepEqual(openedWorkspace, {
      url: expectedRuntimeUrl,
      target: "_blank",
      features: "noopener,noreferrer"
    });
    assert.notEqual(openedWorkspace?.url, workspaceDtoUrl);
    assert.ok(workspaceDtoFixtureReads >= 1);
    assert.ok(runtimeFixtureReads >= 1);

    const advanced = page.locator("details.workspace-advanced-details");
    await advanced.locator("summary").click();
    await advanced.getByRole("heading", { name: "模型预算", exact: true }).waitFor({ state: "visible" });
    await advanced.getByLabel("总额度（micros）").waitFor({ state: "visible" });
    await advanced.getByText("$0.25", { exact: true }).waitFor({ state: "visible" });
    const technical = page.locator("details.workspace-technical-details");
    await technical.locator("summary").click();
    await technical.getByText("Workspace ID", { exact: true }).waitFor({ state: "visible" });
    await technical.getByText("Runtime ready", { exact: true }).waitFor({ state: "visible" });
    await technical.getByText("Runtime URL", { exact: true }).waitFor({ state: "visible" });
    await technical.getByText(expectedRuntimeUrl, { exact: true }).waitFor({ state: "visible" });
    await technical.getByText("manual", { exact: true }).waitFor({ state: "visible" });
    await technical.getByText("ready_pod_uses_retained_pvc", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await visibleTextCount(page, workspaceDtoUrl), 0);
    await assertNoHorizontalOverflow(page);

    await page.getByRole("button", { name: "工作空间列表", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces");
    assert.equal(await page.locator(".workspace-access-panel").count(), 0);
    assert.equal(await page.locator(".credential-actions code").count(), 0);
    const journeyRow = page.locator(".workspace-list-row").filter({ hasText: journeyName });
    await journeyRow.click();
    await page.waitForURL((url) => url.pathname === `/console/workspaces/${encodeURIComponent(launch.workspaceId!)}`);
    const returnedAccess = page.locator(".workspace-access-panel");
    const returnedPasswordRow = returnedAccess.locator(".data-list > div").filter({ hasText: "登录密码" }).first();
    const returnedKeyRow = returnedAccess.locator(".data-list > div").filter({ hasText: "API 密钥" }).first();
    await returnedPasswordRow.waitFor({ state: "visible" });
    assert.equal(await credentialIsRevealed(returnedPasswordRow), false);
    assert.equal(await credentialIsRevealed(returnedKeyRow), false);
    assert.equal(launchPostCount, 1);
    assert.equal(launchIdempotencyKeys.size, 1);
    await assertNoHorizontalOverflow(page);
    assertBrowserAuditClean(audit);
  } finally {
    releaseAuthoritativeRead.resolve();
    await context?.close();
    await demo.close();
  }
}

async function verifyWorkspaceDetailExperience() {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const workspaceDtoUrl = "https://dto-entry.example.invalid/w/ws-1/";
  const expectedRuntimeUrl = "https://runtime-entry.example.invalid/w/ws-1/";
  const workspaceDetail: WorkspaceDTO = {
    id: "ws-1",
    ownerAccountId: "acct-1",
    ownerUserId: "user-customer",
    state: "running",
    createdAt: "2026-07-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
    name: "Pilot Workspace",
    url: workspaceDtoUrl,
    packageId: "basic",
    storageGb: 10,
    autoRenew: false,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    totalUsdMicros: 52_580_000,
    periodStart: "2026-07-01T00:00:00Z",
    paidThrough: "2026-08-01T00:00:00Z",
    renewalStatus: "manual",
    workspaceApiKeyId: "9"
  };
  const workspaceList: SourceEnvelope<WorkspaceListData> = {
    source: "control-plane",
    status: "available",
    available: true,
    fetchedAt: "2026-09-01T00:00:00Z",
    data: {
      items: [workspaceDetail],
      total: 1,
      page: 1,
      pageSize: 50
    }
  };
  const workspaceRuntime: SourceEnvelope<WorkspaceRuntimeDTO> = {
    source: "fabric",
    status: "available",
    available: true,
    fetchedAt: "2026-09-01T00:00:00Z",
    data: {
      workspaceId: "ws-1",
      status: "running",
      ready: true,
      runtimeId: "runtime-ws-1",
      url: expectedRuntimeUrl,
      serviceName: "runtime-ws-1",
      checks: [{ name: "ready_pod_uses_retained_pvc", ok: true }],
      access: {
        username: "opl",
        credentialStatus: "configured",
        credentialVersion: "1"
      }
    }
  };
  try {
    for (const viewport of viewports) {
      const context = await browser.newContext({
        viewport,
        permissions: ["clipboard-read", "clipboard-write"]
      });
      const page = await context.newPage();
      const audit = await installBrowserAudit(page, demo.origin);
      let workspaceDetailFixtureReads = 0;
      await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspaces", async (route) => {
        const requestUrl = new URL(route.request().url());
        if (route.request().method() === "GET" && requestUrl.searchParams.get("pageSize") === "50") {
          workspaceDetailFixtureReads += 1;
          await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(workspaceList) });
          return;
        }
        await route.fallback();
      });
      await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspaces/ws-1/runtime-status", async (route) => {
        await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(workspaceRuntime) });
      });
      await page.addInitScript({ content: `
        window.openedWorkspace = null;
        window.open = (url, target, features) => {
          window.openedWorkspace = {
            url: String(url || ""),
            target: String(target || ""),
            features: String(features || "")
          };
          return null;
        };
      ` });
      await login(page, demo.origin);
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });
      assert.ok(workspaceDetailFixtureReads >= 1);

      const identity = page.locator(".workspace-identity-panel");
      await identity.getByRole("heading", { name: "Pilot Workspace", exact: true }).waitFor({ state: "visible" });
      await identity.getByText("可使用", { exact: true }).waitFor({ state: "visible" });
      await identity.getByText("BASIC", { exact: true }).waitFor({ state: "visible" });
      await identity.getByText("$52.58", { exact: true }).waitFor({ state: "visible" });
      await identity.getByText("2026/08/01", { exact: true }).waitFor({ state: "visible" });

      const openWorkspace = identity.getByRole("button", { name: "打开工作空间", exact: true });
      assert.equal(await openWorkspace.isEnabled(), true);
      await openWorkspace.click();
      const openedWorkspace = await page.evaluate(() => (window as Window & {
        openedWorkspace?: { url: string; target: string; features: string } | null;
      }).openedWorkspace);
      assert.deepEqual(openedWorkspace, {
        url: expectedRuntimeUrl,
        target: "_blank",
        features: "noopener,noreferrer"
      });
      assert.notEqual(openedWorkspace?.url, workspaceDtoUrl);

      const access = page.locator(".workspace-access-panel");
      await access.getByText("登录账号", { exact: true }).waitFor({ state: "visible" });
      await access.getByText("登录密码", { exact: true }).waitFor({ state: "visible" });
      await access.getByText("API 密钥", { exact: true }).waitFor({ state: "visible" });
      await access.getByText("敏感信息将在 60 秒后自动隐藏", { exact: true }).waitFor({ state: "visible" });

      const passwordRow = access.locator(".data-list > div").filter({ hasText: "登录密码" }).first();
      const keyRow = access.locator(".data-list > div").filter({ hasText: "API 密钥" }).first();
      await passwordRow.getByRole("button", { name: "显示", exact: true }).click();
      await page.waitForFunction(() => {
        const rows = [...document.querySelectorAll(".workspace-access-panel .data-list > div")];
        const row = rows.find((candidate) => candidate.textContent?.includes("登录密码"));
        const value = row?.querySelector("code")?.textContent || "";
        return Boolean(value) && !value.includes("••");
      });
      const password = String(await passwordRow.locator("code").textContent());
      assert.ok(password && !password.includes("••"));
      await passwordRow.getByRole("button", { name: "复制", exact: true }).click();
      await page.getByText("登录密码已复制", { exact: true }).waitFor({ state: "visible" });
      await keyRow.getByRole("button", { name: "显示", exact: true }).click();
      await page.waitForFunction(() => {
        const rows = [...document.querySelectorAll(".workspace-access-panel .data-list > div")];
        const row = rows.find((candidate) => candidate.textContent?.includes("API 密钥"));
        const value = row?.querySelector("code")?.textContent || "";
        return Boolean(value) && !value.includes("••");
      });
      const key = String(await keyRow.locator("code").textContent());
      assert.ok(key && !key.includes("••"));
      await keyRow.getByRole("button", { name: "复制", exact: true }).click();
      await page.getByText("API 密钥已复制", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByText(password, { exact: true }).count(), 0);

      const renewal = page.locator(".workspace-plan-panel");
      await renewal.getByRole("heading", { name: "续费与存储", exact: true }).waitFor({ state: "visible" });
      await renewal.getByText("续费方式", { exact: true }).waitFor({ state: "visible" });
      await renewal.getByText("手动续费", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await renewal.getByText("自动续费", { exact: true }).count(), 0);
      assert.equal(await renewal.getByRole("checkbox").count(), 0);
      assert.equal(await visibleTextCount(page, "BASIC"), 1);
      assert.equal(await visibleTextCount(page, "$52.58"), 1);
      assert.equal(await visibleTextCount(page, "2026/08/01"), 1);

      for (const hidden of [
        "Runtime ready",
        "Workspace URL",
        "Workspace Key",
        "manual",
        "ready_pod_uses_retained_pvc",
        "CPU / 内存规格"
      ]) {
        assert.equal(await visibleTextCount(page, hidden), 0, `${hidden} should be disclosed`);
      }
      assert.equal(await visibleTextCount(page, /Secret/), 0, "Secret should not be visible by default");
      assert.equal(await visibleTextCount(page, /micros/), 0, "micros should not be visible by default");

      const advanced = page.locator("details.workspace-advanced-details");
      assert.equal(await advanced.getAttribute("open"), null);
      const advancedSummary = advanced.locator("summary");
      const advancedChevron = advancedSummary.locator("svg.lucide-chevron-down");
      assert.equal(await advancedChevron.count(), 1);
      assert.equal(await advancedChevron.getAttribute("aria-hidden"), "true");
      assert.ok((await advancedSummary.boundingBox())!.height >= 44);
      await focusByKeyboard(page, "details.workspace-advanced-details > summary");
      assert.notEqual(await advancedSummary.evaluate((element) => getComputedStyle(element).outlineStyle), "none");
      await advancedSummary.click();
      await page.waitForFunction(() => {
        const chevron = document.querySelector("details.workspace-advanced-details summary svg.lucide-chevron-down");
        return chevron !== null && getComputedStyle(chevron).transform !== "none";
      });
      assert.notEqual(await advancedChevron.evaluate((element) => getComputedStyle(element).transform), "none");
      await advanced.getByText("$0.25", { exact: true }).waitFor({ state: "visible" });
      await advanced.getByRole("button", { name: "删除工作空间", exact: true }).waitFor({ state: "visible" });

      const technical = page.locator("details.workspace-technical-details");
      assert.equal(await technical.getAttribute("open"), null);
      const technicalSummary = technical.locator("summary");
      const technicalChevron = technicalSummary.locator("svg.lucide-chevron-down");
      assert.equal(await technicalChevron.count(), 1);
      assert.equal(await technicalChevron.getAttribute("aria-hidden"), "true");
      assert.ok((await technicalSummary.boundingBox())!.height >= 44);
      await focusByKeyboard(page, "details.workspace-technical-details > summary");
      assert.notEqual(await technicalSummary.evaluate((element) => getComputedStyle(element).outlineStyle), "none");
      await technicalSummary.click();
      await page.waitForFunction(() => {
        const chevron = document.querySelector("details.workspace-technical-details summary svg.lucide-chevron-down");
        return chevron !== null && getComputedStyle(chevron).transform !== "none";
      });
      assert.notEqual(await technicalChevron.evaluate((element) => getComputedStyle(element).transform), "none");
      await technical.getByText("ws-1", { exact: true }).first().waitFor({ state: "visible" });
      await technical.getByText(expectedRuntimeUrl, { exact: true }).waitFor({ state: "visible" });
      assert.equal(await visibleTextCount(page, workspaceDtoUrl), 0);
      await technical.getByText("ready_pod_uses_retained_pvc", { exact: true }).waitFor({ state: "visible" });
      await technical.getByText("manual", { exact: true }).first().waitFor({ state: "visible" });

      const sectionOrder = await page.locator(".workspace-detail-page > .workspace-detail-content > *").evaluateAll((elements) => elements.map((element) => element.className));
      assert.deepEqual(sectionOrder, [
        "panel workspace-identity-panel",
        "panel workspace-access-panel",
        "panel workspace-plan-panel",
        "panel workspace-settings-panel",
        "panel workspace-technical-panel"
      ]);
      await assertNoHorizontalOverflow(page);
      assertBrowserAuditClean(audit);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
}
