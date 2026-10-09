import assert from "node:assert/strict";
import test from "node:test";

import type { Browser, BrowserContext, Locator, Page } from "playwright";

import type {
  PricingCatalogResponse,
  PricingPreviewResponse,
  RuntimeCredentialResponse,
  RuntimeCredentialRotationResponse,
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceModelConfigurationDTO,
  WorkspaceOwnerOperationDTO,
  WorkspaceGatewayBudgetUpdateRequest,
  WorkspaceListData,
  WorkspaceLaunchResponse,
  WorkspaceCurrentApplicationDTO,
  WorkspaceRenewalReadDTO,
  WorkspaceRuntimeDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

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

for (const identity of ["legacy", "cloud"] as const) {
  test(`${identity} Console exposes only its deployed publisher and delivery routes`, { timeout: 60_000 }, async () => {
    const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
    process.env.VITE_CONSOLE_IDENTITY = identity;
    let demo: Awaited<ReturnType<typeof startConsoleDemoServer>>;
    try {
      demo = await startConsoleDemoServer({ port: 0, log: false });
    } catch (error) {
      if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
      else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
      throw error;
    }
    const browser = await launchBrowser({ headless: true });
    try {
      const page = await browser.newPage();
      const audit = await installBrowserAudit(page, demo.origin);
      const v2Requests: string[] = [];
      await page.route("**/api/v2/**", async (route) => {
        const path = new URL(route.request().url()).pathname;
        v2Requests.push(path);
        if (identity === "legacy") {
          await route.fulfill({ status: 404, json: { error: "bff_not_deployed" } });
          return;
        }
        // The cloud identity reads the customer Workspace from the Workspace
        // owner, so the fixture answers the owner list and detail routes.
        const ownerWorkspace = { id: "ws-1", name: "Customer Workspace", computePlanId: "compute-1", storagePlanId: "storage-1", deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available", currentPeriodEnd: "2026-10-27T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", version: "1" };
        const responses: Record<string, unknown> = {
          "/api/v2/auth/session": { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" },
          "/api/v2/workspaces": { items: [ownerWorkspace] },
          "/api/v2/workspaces/ws-1": ownerWorkspace,
          "/api/v2/namespaces": { items: [{ id: "namespace-1", name: "Fixture namespace" }] },
          "/api/v2/packages": { items: [] },
          "/api/v2/catalog/webui-versions": { items: [{ id: "webui-1", name: "Fixture WebUI", versionLabel: "1.0.0", status: "approved" }] },
          "/api/v2/catalog/runtime-versions": { items: [{ id: "runtime-1", name: "Fixture Runtime", versionLabel: "1.0.0", status: "approved", artifactDigest: "sha256:" + "a".repeat(64) }] },
          "/api/v2/delivery/ws-1": {
            workspaceId: "ws-1",
            workspace: { owner: "workspace", state: "active", details: {} },
            capabilityVersion: { owner: "capability", state: "ready", details: {} },
            build: { owner: "build", state: "succeeded", details: {} },
            serve: { owner: "serve", state: "ready", details: {} }
          }
        };
        assert.ok(Object.hasOwn(responses, path), `unexpected BFF request: ${path}`);
        await route.fulfill({ json: responses[path] });
      });
      // The retained Workspace fixture still owns its legacy data API session.
      const session = await page.request.post(`${demo.origin}/api/auth/login`, { data: CONSOLE_DEMO_CREDENTIALS.customer });
      assert.equal(session.ok(), true);
      await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "networkidle" });
      await page.locator(".workspace-list-row").first().waitFor({ state: "visible" });
      assert.equal(await page.getByRole("link", { name: "发布 Package", exact: true }).count(), identity === "cloud" ? 1 : 0);

      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "networkidle" });
      await page.locator(".workspace-technical-details").waitFor({ state: "visible" });
      assert.equal(await page.locator("[data-agent-delivery]").count(), identity === "cloud" ? 1 : 0);
      if (identity === "cloud") {
        await page.locator(".workspace-technical-details > summary").click();
        await page.locator("[data-agent-delivery]").getByText("Capability", { exact: true }).waitFor({ state: "visible" });
        assert.ok(v2Requests.includes("/api/v2/delivery/ws-1"));
        await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "networkidle" });
        await page.getByRole("link", { name: "发布 Package", exact: true }).click();
        await page.locator(".publisher-page").getByRole("heading", { name: "Cloud WebUI / Agent Package", exact: true }).waitFor({ state: "visible" });
        await page.waitForFunction(() => (document.querySelector('[aria-label="WebUI"]') as HTMLSelectElement | null)?.value === "webui-1");
        assert.equal(await page.locator(".publisher-page fieldset").isDisabled(), false);
        assert.ok(v2Requests.includes("/api/v2/namespaces"));
      }

      // A direct URL must obey the same build boundary as visible navigation.
      await page.goto(`${demo.origin}/console/publisher/`, { waitUntil: "networkidle" });
      if (identity === "legacy") {
        await page.getByRole("heading", { name: "页面不存在", exact: true }).waitFor({ state: "visible" });
        assert.equal(await page.locator(".publisher-page").count(), 0);
        assert.deepEqual(v2Requests, []);
      } else {
        await page.locator(".publisher-page").getByRole("heading", { name: "Cloud WebUI / Agent Package", exact: true }).waitFor({ state: "visible" });
        assert.equal(await page.getByRole("heading", { name: "页面不存在", exact: true }).count(), 0);
      }
      assertBrowserAuditClean(audit);
    } finally {
      await browser.close();
      await demo.close();
      if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
      else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
    }
  });
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

async function assertAboveMobileNavigation(page: Page, target: Locator) {
  await target.evaluate((element) => element.scrollIntoView({ block: "center", inline: "nearest" }));
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));
  const navigation = page.locator(".mobile-bottom-nav");
  const [targetBox, navigationBox] = await Promise.all([target.boundingBox(), navigation.boundingBox()]);
  assert.ok(targetBox, "mobile action must have a rendered box");
  assert.ok(navigationBox, "mobile navigation must have a rendered box");
  assert.ok(targetBox.height >= 44, `mobile action height must be at least 44px: ${JSON.stringify(targetBox)}`);
  assert.ok(targetBox.y + targetBox.height <= navigationBox.y + 1, `mobile action is obscured by navigation: ${JSON.stringify({ targetBox, navigationBox })}`);
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

async function startCloudConsoleDemo() {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  try {
    return await startConsoleDemoServer({ port: 0, log: false });
  } finally {
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
}

async function loginCloudFixture(page: Page, origin: string) {
  const session = await page.request.post(`${origin}/api/auth/login`, { data: CONSOLE_DEMO_CREDENTIALS.customer });
  assert.equal(session.ok(), true);
}

async function verifyWorkspaceCustomerJourney(browser: Browser, viewport: typeof viewports[number]) {
  const demo = await startCloudConsoleDemo();
  const context = await browser.newContext({ viewport, permissions: ["clipboard-read", "clipboard-write"] });
  const page = await context.newPage();
  const audit = await installBrowserAudit(page, demo.origin);
  const operationId = `operation-${viewport.name}`;
  const workspaceId = `workspace-${viewport.name}`;
  const seenIdempotencyKeys = new Set<string>();
  let operationReads = 0;
  let accessReads = 0;
  let createdBody: Record<string, unknown> | null = null;
  try {
    await page.addInitScript({ content: `window.openedWorkspace = null; window.open = (url, target, features) => { window.openedWorkspace = { url: String(url || ""), target: String(target || ""), features: String(features || "") }; return null; };` });
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const path = url.pathname;
      if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/capability-versions") {
        assert.equal(url.searchParams.get("status"), "ready");
        return route.fulfill({ json: { items: [{ id: "cap-ready-1", versionLabel: "IBD Agent 1.0", artifactDigest: "sha256:agent", status: "ready", provenance: "build", modelRequirements: [{ slot: "default", required: true, capability: "chat", allowedModelIds: ["model-1"] }] }] } });
      }
      if (path === "/api/v2/catalog/runtime-versions") return route.fulfill({ json: { items: [{ id: "runtime-1", name: "Default App", versionLabel: "App 1.0", artifactDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", status: "approved", defaultForNewBuilds: true }] } });
      if (path === "/api/v2/catalog/compute-plans") return route.fulfill({ json: { items: [{ id: "compute-1", name: "Compute Standard", vcpus: 2, memoryMiB: 4096, availability: "available", billingMode: "prepaid_monthly" }] } });
      if (path === "/api/v2/catalog/storage-plans") return route.fulfill({ json: { items: [{ id: "storage-1", name: "Storage Standard", capacityGiB: 50, availability: "available", billingMode: "prepaid_monthly" }] } });
      if (path === "/api/v2/catalog/models") return route.fulfill({ json: { items: [{ id: "model-1", name: "IBD Model", capabilities: ["chat"], available: true, inputPricePerMillionTokensUSDMicros: "1", outputPricePerMillionTokensUSDMicros: "2", priceSource: "gateway", fetchedAt: "2026-09-27T00:00:00Z" }] } });
      if (path === "/api/v2/wallet") return route.fulfill({ json: { source: "gateway", status: "available", balanceUSDMicros: "100000000", currency: "USD", fetchedAt: "2026-09-27T00:00:00Z" } });
      if (path === "/api/v2/quotes" && request.method() === "POST") {
        assert.deepEqual(request.postDataJSON(), { purpose: "deploy", applicationSelection: { kind: "agent", capabilityVersionId: "cap-ready-1" }, computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [{ slot: "default", modelId: "model-1" }], periodMonths: 1 });
        return route.fulfill({ status: 201, json: { id: "quote-1", purpose: "deploy", capabilityVersionId: "cap-ready-1", computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [{ slot: "default", modelId: "model-1" }], periodMonths: 1, periodStart: "2026-09-27T00:00:00Z", periodEnd: "2026-10-27T00:00:00Z", pricePolicyVersionId: "price-1", refundPolicyVersionId: "refund-1", retentionPolicyVersionId: "retention-1", refundTerms: "按报价政策处理退款。", retentionTerms: "数据保留按报价政策执行。", expectedInterruption: "部署期间可能短暂不可用。", lineItems: [{ kind: "compute", description: "计算套餐", quantity: 1, amountUSDMicros: "52580000" }], totalUSDMicros: "52580000", status: "offered", expiresAt: "2099-01-01T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", runtimeReadbackRequirement: "required" } });
      }
      if (path === "/api/v2/quotes/quote-1") return route.fulfill({ json: { id: "quote-1", purpose: "deploy", capabilityVersionId: "cap-ready-1", computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [{ slot: "default", modelId: "model-1" }], periodMonths: 1, periodStart: "2026-09-27T00:00:00Z", periodEnd: "2026-10-27T00:00:00Z", pricePolicyVersionId: "price-1", refundPolicyVersionId: "refund-1", retentionPolicyVersionId: "retention-1", refundTerms: "按报价政策处理退款。", retentionTerms: "数据保留按报价政策执行。", lineItems: [], totalUSDMicros: "52580000", status: "offered", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/workspaces" && request.method() === "POST") {
        createdBody = request.postDataJSON() as Record<string, unknown>;
        assert.deepEqual(createdBody, { name: `Customer Journey ${viewport.name}`, quoteId: "quote-1", renewalMode: "manual" });
        seenIdempotencyKeys.add(request.headers()["idempotency-key"] || "");
        return route.fulfill({ status: 202, json: { operationId, owner: "workspace", kind: "create_workspace", resourceId: workspaceId, status: "accepted", stage: "admission", requestId: "request-1", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", pollAfterSeconds: 1 } });
      }
      if (path === `/api/v2/operations/workspace/${operationId}`) {
        operationReads += 1;
        return route.fulfill({ json: { operationId, owner: "workspace", kind: "create_workspace", resourceId: workspaceId, status: operationReads === 1 ? "running" : "succeeded", stage: operationReads === 1 ? "runtime" : "succeeded", ...(operationReads === 1 ? { pollAfterSeconds: 1 } : { observationResult: "confirmed" }), requestId: "request-1", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z" } });
      }
      if (path === "/api/v2/workspaces" && request.method() === "GET") return route.fulfill({ json: { items: [{ id: workspaceId, name: `Customer Journey ${viewport.name}`, capabilityVersionId: "cap-ready-1", computePlanId: "compute-1", storagePlanId: "storage-1", deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available", currentPeriodEnd: "2026-10-27T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", version: "1" }] } });
      if (path === `/api/v2/workspaces/${workspaceId}`) return route.fulfill({ json: { id: workspaceId, name: `Customer Journey ${viewport.name}`, capabilityVersionId: "cap-ready-1", computePlanId: "compute-1", storagePlanId: "storage-1", deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available", currentPeriodEnd: "2026-10-27T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", version: "1" } });
      if (path === `/api/v2/workspaces/${workspaceId}/access`) {
        accessReads += 1;
        assert.equal(request.method(), "POST");
        return route.fulfill({ json: { workspaceId, url: `https://agent.example.invalid/${workspaceId}`, authenticationMode: "application_login" } });
      }
      return route.fulfill({ status: 404, json: { error: "unexpected_v2_route" } });
    });
    await loginCloudFixture(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "networkidle" });
    await page.locator(".workspace-list-row").first().waitFor({ state: "visible" });
    await page.getByRole("button", { name: "新建工作空间", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces/new");
    await page.getByRole("heading", { name: "新建 Agent Workspace", exact: true }).waitFor({ state: "visible" });
    await page.getByLabel("工作空间名称").fill(`Customer Journey ${viewport.name}`);
    await page.getByText("已构建 Agent", { exact: true }).click();
    await page.getByLabel("default").selectOption("model-1");
    await page.getByRole("button", { name: "获取准确报价", exact: true }).click();
    await page.getByRole("heading", { name: "确认准确报价与部署条款", exact: true }).waitFor({ state: "visible" });
    await page.getByRole("checkbox", { name: /我确认以上 Agent/ }).check();
    await page.getByRole("button", { name: "确认并开通 Workspace", exact: true }).click();
    await page.getByRole("button", { name: "打开 Agent WebUI", exact: true }).waitFor({ state: "visible", timeout: 5_000 });
    assert.equal(accessReads, 1);
    assert.equal(seenIdempotencyKeys.size, 1);
    assert.deepEqual(createdBody, { name: `Customer Journey ${viewport.name}`, quoteId: "quote-1", renewalMode: "manual" });
    await page.getByRole("button", { name: "打开 Agent WebUI", exact: true }).click();
    assert.deepEqual(await page.evaluate(() => (window as Window & { openedWorkspace?: unknown }).openedWorkspace), { url: `https://agent.example.invalid/${workspaceId}`, target: "_blank", features: "noopener,noreferrer" });
    assert.ok(operationReads >= 2);
    await page.reload({ waitUntil: "networkidle" });
    await page.getByRole("button", { name: "打开 Agent WebUI", exact: true }).waitFor({ state: "visible", timeout: 5_000 }).catch(async (error) => {
      throw new Error(`reload access: ${await page.locator("body").innerText()} storage=${await page.evaluate(() => sessionStorage.getItem("opl-cloud:agent-workspace-operation"))}`, { cause: error });
    });
    assert.equal(seenIdempotencyKeys.size, 1);
    assert.ok(accessReads >= 2);
    await page.getByRole("button", { name: "创建另一个 Workspace" }).click();
    await page.getByRole("heading", { name: "新建 Agent Workspace", exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.evaluate(() => sessionStorage.getItem("opl-cloud:agent-workspace-operation")), null);
    assertBrowserAuditClean(audit);
  } finally {
    await context.close();
    await demo.close();
  }
}

test("customer completes one authoritative Workspace journey at desktop and mobile widths", { timeout: 120_000 }, async () => {
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of viewports) {
      await verifyWorkspaceCustomerJourney(browser, viewport);
    }
  } finally {
    await browser.close();
  }
});

test("lost create response reloads into the original idempotent Workspace request", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    let createWrites = 0;
    let activeActor = "user-customer";
    const keys = new Set<string>();
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: activeActor, tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/capability-versions") return route.fulfill({ json: { items: [{ id: "cap-1", versionLabel: "Agent", status: "ready", provenance: "build", modelRequirements: [] }] } });
      if (path === "/api/v2/catalog/runtime-versions") return route.fulfill({ json: { items: [{ id: "runtime-1", name: "Default App", versionLabel: "App 1.0", artifactDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", status: "approved", defaultForNewBuilds: true }] } });
      if (path === "/api/v2/catalog/compute-plans") return route.fulfill({ json: { items: [{ id: "compute-1", name: "Compute", vcpus: 2, memoryMiB: 1024, availability: "available" }] } });
      if (path === "/api/v2/catalog/storage-plans") return route.fulfill({ json: { items: [{ id: "storage-1", name: "Storage", capacityGiB: 10, availability: "available" }] } });
      if (path === "/api/v2/catalog/models") return route.fulfill({ json: { items: [{ id: "model-1", name: "Model", capabilities: [], available: true }] } });
      if (path === "/api/v2/wallet") return route.fulfill({ json: { source: "gateway", status: "available", balanceUSDMicros: "100000000", currency: "USD", fetchedAt: "2026-09-27T00:00:00Z" } });
      if (path === "/api/v2/quotes" && request.method() === "POST") return route.fulfill({ status: 201, json: { id: "quote-lost", purpose: "deploy", capabilityVersionId: "cap-1", computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [], periodMonths: 1, periodStart: "2026-09-27T00:00:00Z", periodEnd: "2026-10-27T00:00:00Z", pricePolicyVersionId: "p", refundPolicyVersionId: "r", retentionPolicyVersionId: "t", refundTerms: "refund", retentionTerms: "retention", lineItems: [], totalUSDMicros: "1", status: "offered", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/quotes/quote-lost") return route.fulfill({ json: { id: "quote-lost", purpose: "deploy", status: "offered", totalUSDMicros: "1", expiresAt: "2099-01-01T00:00:00Z", modelSelections: [], lineItems: [] } });
      if (path === "/api/v2/workspaces" && request.method() === "POST") {
        createWrites += 1;
        keys.add(request.headers()["idempotency-key"] || "");
        assert.deepEqual(request.postDataJSON(), { name: "Recovery Test", quoteId: "quote-lost", renewalMode: "manual" });
        if (createWrites === 1) return route.abort("failed");
        return route.fulfill({ status: 202, json: { operationId: "op-lost", owner: "workspace", kind: "create_workspace", resourceId: "ws-lost", status: "accepted", stage: "admission", requestId: "req", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", pollAfterSeconds: 1 } });
      }
      if (path === "/api/v2/operations/workspace/op-lost") return route.fulfill({ json: { operationId: "op-lost", owner: "workspace", kind: "create_workspace", resourceId: "ws-lost", status: "running", stage: "runtime", requestId: "req", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", pollAfterSeconds: 5 } });
      return route.fulfill({ status: 404, json: { error: "unexpected_v2_route" } });
    });
    await loginCloudFixture(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });
    await page.getByLabel("工作空间名称").fill("Recovery Test");
    await page.getByText("已构建 Agent", { exact: true }).click();
    await page.getByRole("button", { name: "获取准确报价", exact: true }).click();
    await page.getByRole("checkbox", { name: /我确认以上 Agent/ }).check();
    await page.getByRole("button", { name: "确认并开通 Workspace", exact: true }).click();
    await page.getByText("正在核对原开通请求", { exact: true }).waitFor({ state: "visible", timeout: 5_000 }).catch(async (error) => {
      throw new Error(`lost response: ${await page.locator("body").innerText()} storage=${await page.evaluate(() => sessionStorage.getItem("opl-cloud:agent-workspace-operation"))}`, { cause: error });
    });
    await page.reload({ waitUntil: "networkidle" });
    await page.getByText("正在核对原开通请求", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: "确认并开通 Workspace" }).count(), 0);
    activeActor = "another-customer";
    await page.reload({ waitUntil: "networkidle" });
    await page.getByText("原开通请求需由提交账户核对", { exact: false }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: "核对原请求" }).count(), 0);
    assert.equal(createWrites, 1);
    activeActor = "user-customer";
    await page.reload({ waitUntil: "networkidle" });
    await page.getByRole("button", { name: "核对原请求" }).click();
    await page.getByText("正在开通 Workspace", { exact: true }).waitFor({ state: "visible" });
    assert.equal(createWrites, 2);
    assert.equal(keys.size, 1);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace detail prioritizes authoritative availability and entry while keeping policy and evidence disclosed", { timeout: 60_000 }, verifyWorkspaceDetailExperience);

test("Current application status and declared capabilities replace legacy Workspace entry and controls", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  const application: WorkspaceCurrentApplicationDTO = {
    operationId: "application-knowledge", applicationId: "knowledge-app", revision: "1.2.3", status: "pending",
    capabilities: { credentials: false, gateway: false }
  };
  let runtimeOperationId = application.operationId;
  let budgetReads = 0;
  const detail: WorkspaceDTO = {
    id: "ws-1", ownerAccountId: "acct-1", ownerUserId: "user-customer", name: "Application Workspace",
    state: "running", createdAt: "2026-09-13T00:00:00Z", updatedAt: "2026-09-13T00:00:00Z",
    packageId: "basic", storageGb: 10, renewalStatus: "manual", workspaceApiKeyId: "9",
    applicationBinding: "knowledge-app@1.2.3", currentApplication: application
  };
  const source = <T,>(data: T, sourceName = "control-plane"): SourceEnvelope<T> => ({
    source: sourceName, status: "available", available: true, fetchedAt: detail.updatedAt, data
  });
  try {
    for (const viewport of viewports) {
      application.status = "pending";
      application.capabilities.credentials = false;
      delete application.entryUrl;
      runtimeOperationId = application.operationId;
      detail.applicationBinding = "empty";
      delete detail.currentApplication;
      detail.applicationInstallation = { operationId: "install-default", applicationId: "opl-app", revision: "1.2.3", status: "running" };
      let installationResumes = 0;
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const audit = await installBrowserAudit(page, demo.origin);
      await page.addInitScript("window.open = (url) => { window.openedApplication = url; return null; };");
      await page.route("**/api/workspaces?*", (route) => route.fulfill({ json: source<WorkspaceListData>({ items: [detail], total: 1, page: 1, pageSize: 50 }) }));
      await page.route("**/api/workspaces/ws-1/application-installation/resume", async (route) => {
        assert.equal(route.request().method(), "POST");
        assert.deepEqual(route.request().postDataJSON(), {});
        installationResumes += 1;
        detail.applicationInstallation = { operationId: "install-default", applicationId: "opl-app", revision: "1.2.3", status: "running", canResume: false };
        await route.fulfill({ status: 202, json: { workspaceId: "ws-1", applicationInstallation: detail.applicationInstallation } });
      });
      await page.route("**/api/workspaces/ws-1/runtime-status", (route) => route.fulfill({ json: source<WorkspaceRuntimeDTO>({
        workspaceId: "ws-1", runtimeId: "runtime-ws-1", status: application.status === "ready" ? "running" : "unready",
        ready: application.status === "ready", checks: [], url: "https://retired-entry.example.invalid/",
        access: { username: "retired-user", credentialStatus: "configured" },
        currentApplication: detail.currentApplication ? { ...application, operationId: runtimeOperationId } : undefined
      }, "fabric") }));
      await page.route("**/api/workspaces/ws-1/renewal", (route) => route.fulfill({ json: {
        workspaceId: "ws-1", autoRenew: false, paidThrough: "2026-10-13T00:00:00Z", renewalStatus: "manual",
        recovery: { state: "not_required", reason: "" }
      } satisfies WorkspaceRenewalReadDTO }));
      await page.route("**/api/workspaces/ws-1/gateway-budget", async (route) => {
        budgetReads += 1;
        await route.fulfill({ status: 500, json: { error: "unexpected_legacy_budget_read" } });
      });
      await login(page, demo.origin);
      await page.goto(`${demo.origin}/console/workspaces/ws-1`, { waitUntil: "domcontentloaded" });
      const availability = page.locator(".workspace-availability");
      const open = page.getByRole("button", { name: "打开工作空间", exact: true });
      const refresh = page.locator(".workspace-identity-panel").getByRole("button", { name: "刷新", exact: true });
      await page.getByText("应用安装中", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await open.isDisabled(), true);
      detail.applicationInstallation.status = "manual_review";
      await refresh.click();
      await page.getByText("应用安装需要处理", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("button", { name: "继续安装应用", exact: true }).count(), 0);
      detail.applicationInstallation.canResume = true;
      await refresh.click();
      await page.getByRole("button", { name: "继续安装应用", exact: true }).click();
      await page.getByText("应用安装中", { exact: true }).waitFor({ state: "visible" });
      assert.equal(installationResumes, 1);
      assert.equal(await page.getByRole("button", { name: "重新购买", exact: true }).count(), 0);
      detail.applicationBinding = "knowledge-app@1.2.3";
      detail.currentApplication = application;
      delete detail.applicationInstallation;
      await refresh.click();
      await availability.getByText("应用正在部署", { exact: true }).waitFor({ state: "visible" });
      await page.getByText("knowledge-app · 1.2.3", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await open.isDisabled(), true);
      assert.equal(await page.locator(".workspace-access-panel").count(), 0);
      assert.equal(await page.locator(".workspace-settings-panel").count(), 0);
      application.status = "ready";
      application.entryUrl = "https://knowledge-app.example.invalid/";
      await refresh.click();
      await availability.getByText("可使用", { exact: true }).waitFor({ state: "visible" });
      await open.click();
      assert.equal(await page.evaluate(() => (window as unknown as { openedApplication: string }).openedApplication), application.entryUrl);
      delete application.entryUrl;
      await refresh.click();
      await availability.getByText("运行中", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await open.isDisabled(), true);
      for (const [status, label] of [["suspended", "应用已暂停"], ["failed", "应用运行失败"]] as const) {
        application.status = status;
        await refresh.click();
        await availability.getByText(label, { exact: true }).waitFor({ state: "visible" });
        assert.equal(await open.isDisabled(), true);
      }
      application.status = "ready";
      application.entryUrl = "https://knowledge-app.example.invalid/";
      runtimeOperationId = "retired-application";
      await refresh.click();
      await availability.getByText("状态待确认", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await open.isDisabled(), true);
      assert.equal(budgetReads, 0);

      let rotationSubmitted = false;
      let rotationWrites = 0;
      await page.route("**/api/workspaces/ws-1/runtime-credentials/reveal", (route) => route.fulfill({ json: {
        workspaceId: "ws-1", access: {
          account: "application-user", username: "application-user", password: rotationSubmitted ? "FixtureAfterRotation" : "FixtureBeforeRotation",
          credentialStatus: "configured", credentialVersion: rotationSubmitted ? "2" : "1"
        }
      } satisfies RuntimeCredentialResponse }));
      await page.route("**/api/workspaces/ws-1/runtime-credentials/rotate", (route) => {
        rotationSubmitted = true;
        rotationWrites += 1;
        application.status = "pending";
        return route.fulfill({ status: 202, json: {
          workspaceId: "ws-1", operationId: "application-after-rotation", status: "pending"
        } satisfies RuntimeCredentialRotationResponse });
      });
      application.capabilities.credentials = true;
      runtimeOperationId = application.operationId;
      await refresh.click();
      await availability.getByText("可使用", { exact: true }).waitFor({ state: "visible" });
      const accessPanel = page.locator(".workspace-access-panel");
      await accessPanel.getByRole("button", { name: "显示", exact: true }).click();
      await accessPanel.getByText("FixtureBeforeRotation", { exact: true }).waitFor({ state: "visible" });
      await accessPanel.getByRole("button", { name: "轮换密码", exact: true }).click();
      await page.getByText("密码轮换已提交，应用正在更新；完成后请重新显示密码", { exact: true }).waitFor({ state: "visible" });
      await availability.getByText("应用正在部署", { exact: true }).waitFor({ state: "visible" });
      assert.equal(await accessPanel.getByText("FixtureBeforeRotation", { exact: true }).count(), 0);
      assert.equal(await page.getByText("登录密码已轮换", { exact: true }).count(), 0);
      application.operationId = "application-after-rotation";
      runtimeOperationId = application.operationId;
      application.status = "ready";
      await refresh.click();
      await availability.getByText("可使用", { exact: true }).waitFor({ state: "visible" });
      await accessPanel.getByRole("button", { name: "显示", exact: true }).click();
      await accessPanel.getByText("FixtureAfterRotation", { exact: true }).waitFor({ state: "visible" });
      assert.equal(rotationWrites, 1);
      assert.equal(await accessPanel.getByText("API 密钥", { exact: true }).count(), 0);
      assert.deepEqual(audit.pageErrors, []);
      assert.deepEqual(audit.externalRequests, []);
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("v2 launch fails closed when Gateway or capability owner routes are unavailable", { timeout: 30_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await page.route("**/api/v2/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/api/v2/auth/session") {
        await route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
        return;
      }
      await route.fulfill({ status: 503, json: { error: "DEPENDENCY_UNAVAILABLE" } });
    });
    await loginCloudFixture(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });
    await page.getByText("开通入口暂不可用", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: "获取准确报价", exact: true }).isDisabled(), true);
    assert.equal(await page.getByRole("button", { name: /确认并开通/ }).count(), 0);
    assert.equal(await page.getByText("模拟 Agent", { exact: true }).count(), 0);
    assert.ok(audit.consoleErrors.length > 0);
    assert.ok(audit.consoleErrors.every((message) => message.includes("503 (Service Unavailable)")));
    audit.consoleErrors.length = 0;
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});


test("default App confirm step shows its Runtime Release instead of an empty CapabilityVersion", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/workspaces" && request.method() === "GET") return route.fulfill({ json: { items: [{ id: "workspace-existing", name: "Existing", computePlanId: "compute-1", storagePlanId: "storage-1", deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available", currentPeriodEnd: "2026-10-27T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", version: "1" }] } });
      if (path === "/api/v2/capability-versions") return route.fulfill({ json: { items: [] } });
      if (path === "/api/v2/catalog/runtime-versions") return route.fulfill({ json: { items: [{ id: "runtime-1", name: "Default App", versionLabel: "App 1.0", status: "approved", artifactDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" }] } });
      if (path === "/api/v2/catalog/compute-plans") return route.fulfill({ json: { items: [{ id: "compute-1", name: "Compute Standard", vcpus: 2, memoryMiB: 4096, availability: "available", billingMode: "prepaid_monthly" }] } });
      if (path === "/api/v2/catalog/storage-plans") return route.fulfill({ json: { items: [{ id: "storage-1", name: "Storage Standard", capacityGiB: 50, availability: "available", billingMode: "prepaid_monthly" }] } });
      if (path === "/api/v2/catalog/models") return route.fulfill({ json: { items: [{ id: "model-1", name: "IBD Model", capabilities: ["chat"], available: true, inputPricePerMillionTokensUSDMicros: "1", outputPricePerMillionTokensUSDMicros: "2", priceSource: "gateway", fetchedAt: "2026-09-27T00:00:00Z" }] } });
      if (path === "/api/v2/wallet") return route.fulfill({ json: { source: "gateway", status: "available", balanceUSDMicros: "100000000", currency: "USD", fetchedAt: "2026-09-27T00:00:00Z" } });
      if (path === "/api/v2/quotes" && request.method() === "POST") {
        assert.deepEqual(request.postDataJSON(), { purpose: "deploy", applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime-1" }, computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [], periodMonths: 1 });
        return route.fulfill({ status: 201, json: { id: "quote-1", purpose: "deploy", runtimeVersionId: "runtime-1", computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [], periodMonths: 1, periodStart: "2026-09-27T00:00:00Z", periodEnd: "2026-10-27T00:00:00Z", pricePolicyVersionId: "price-1", refundPolicyVersionId: "refund-1", retentionPolicyVersionId: "retention-1", refundTerms: "按报价政策处理退款。", retentionTerms: "数据保留按报价政策执行。", lineItems: [{ kind: "runtime_release", description: "默认 OPL App 运行时", quantity: 1, amountUSDMicros: "1000000" }], totalUSDMicros: "1000000", status: "offered", expiresAt: "2099-01-01T00:00:00Z" } });
      }
      return route.fulfill({ status: 404, json: { error: "unexpected_v2_route", path } });
    });
    await loginCloudFixture(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "networkidle" });
    await page.locator(".workspace-list-row").first().waitFor({ state: "visible" });
    await page.getByRole("button", { name: "新建工作空间", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces/new");
    await page.getByRole("heading", { name: "新建 Agent Workspace", exact: true }).waitFor({ state: "visible", timeout: 10_000 });
    await page.getByLabel("工作空间名称").fill("Default App Confirm");
    await page.getByText("默认 OPL App", { exact: true }).click();
    await page.getByLabel("Runtime Release", { exact: true }).selectOption("runtime-1");
    await page.getByRole("button", { name: "获取准确报价", exact: true }).click();
    await page.getByRole("heading", { name: "确认准确报价与部署条款", exact: true }).waitFor({ state: "visible" });
    const confirm = page.locator(".launch-confirm-list");
    const releaseRow = confirm.locator("div").filter({ hasText: "Runtime Release" }).first();
    await releaseRow.waitFor({ state: "visible" });
    assert.equal((await releaseRow.locator("dd").innerText()).trim(), "App 1.0");
    assert.equal(await confirm.getByText("Agent 版本", { exact: true }).count(), 0, "the default App confirm step must not render the Agent CapabilityVersion row");
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace detail fails closed without exposing Runtime or delete reason codes by default", { timeout: 30_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
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
    const deletePanel = page.locator(".workspace-delete-panel");
    await deletePanel.getByRole("button", { name: "删除工作空间", exact: true }).click();
    await deletePanel.getByText("工作空间删除暂不可用", { exact: true }).waitFor({ state: "visible" });
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
  const browser = await launchBrowser({ headless: true });
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

test("legacy resource-only Workspace history remains readable without becoming the new launch entry", { timeout: 30_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await login(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "networkidle" });
    await page.locator(".workspace-list-row").first().waitFor({ state: "visible" });
    await page.locator(".workspace-list-row").first().getByText("查看详情", { exact: true }).click();
    await page.locator(".workspace-identity-panel").waitFor({ state: "visible" });
    assert.equal(await page.getByRole("button", { name: "打开工作空间", exact: true }).count() > 0, true);
    assert.equal(await page.getByRole("button", { name: "新建工作空间", exact: true }).count(), 0);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("v2 operation refresh keeps the original order and never opens before Serve access confirmation", { timeout: 30_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    let operationReads = 0;
    let createWrites = 0;
    await page.addInitScript("window.open = (url) => { window.openedWorkspace = url; return null; };");
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.pathname === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
      if (url.pathname === "/api/v2/capability-versions") return route.fulfill({ json: { items: [{ id: "cap-1", versionLabel: "Agent", artifactDigest: "sha256:x", status: "ready", provenance: "build", modelRequirements: [] }] } });
      if (url.pathname === "/api/v2/catalog/runtime-versions") return route.fulfill({ json: { items: [{ id: "runtime-1", name: "Default App", versionLabel: "App 1.0", artifactDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", status: "approved", defaultForNewBuilds: true }] } });
      if (url.pathname === "/api/v2/catalog/compute-plans") return route.fulfill({ json: { items: [{ id: "compute-1", name: "Compute", vcpus: 2, memoryMiB: 1024, availability: "available", billingMode: "prepaid_monthly" }] } });
      if (url.pathname === "/api/v2/catalog/storage-plans") return route.fulfill({ json: { items: [{ id: "storage-1", name: "Storage", capacityGiB: 10, availability: "available", billingMode: "prepaid_monthly" }] } });
      if (url.pathname === "/api/v2/catalog/models") return route.fulfill({ json: { items: [{ id: "model-1", name: "Model", capabilities: [], available: true, inputPricePerMillionTokensUSDMicros: "1", outputPricePerMillionTokensUSDMicros: "1", priceSource: "gateway", fetchedAt: "2026-09-27T00:00:00Z" }] } });
      if (url.pathname === "/api/v2/wallet") return route.fulfill({ json: { source: "gateway", status: "available", balanceUSDMicros: "100000000", currency: "USD", fetchedAt: "2026-09-27T00:00:00Z" } });
      if (url.pathname === "/api/v2/quotes" && request.method() === "POST") return route.fulfill({ status: 201, json: { id: "quote-refresh", purpose: "deploy", capabilityVersionId: "cap-1", computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [], periodMonths: 1, periodStart: "2026-09-27T00:00:00Z", periodEnd: "2026-10-27T00:00:00Z", pricePolicyVersionId: "p", refundPolicyVersionId: "r", retentionPolicyVersionId: "t", refundTerms: "refund", retentionTerms: "retention", expectedInterruption: "none", lineItems: [], totalUSDMicros: "1", status: "offered", expiresAt: "2099-01-01T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", runtimeReadbackRequirement: "required" } });
      if (url.pathname === "/api/v2/quotes/quote-refresh") return route.fulfill({ json: { id: "quote-refresh", purpose: "deploy", capabilityVersionId: "cap-1", computePlanId: "compute-1", storagePlanId: "storage-1", modelSelections: [], periodMonths: 1, periodStart: "2026-09-27T00:00:00Z", periodEnd: "2026-10-27T00:00:00Z", pricePolicyVersionId: "p", refundPolicyVersionId: "r", retentionPolicyVersionId: "t", refundTerms: "refund", retentionTerms: "retention", expectedInterruption: "none", lineItems: [], totalUSDMicros: "1", status: "offered", expiresAt: "2099-01-01T00:00:00Z", createdAt: "2026-09-27T00:00:00Z", runtimeReadbackRequirement: "required" } });
      if (url.pathname === "/api/v2/workspaces" && request.method() === "POST") { createWrites += 1; return route.fulfill({ status: 202, json: { operationId: "op-refresh", owner: "workspace", kind: "create_workspace", resourceId: "ws-refresh", status: "accepted", stage: "admission", requestId: "req", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", pollAfterSeconds: 1 } }); }
      if (url.pathname === "/api/v2/operations/workspace/op-refresh") { operationReads += 1; return route.fulfill({ json: { operationId: "op-refresh", owner: "workspace", kind: "create_workspace", resourceId: "ws-refresh", status: "succeeded", stage: "succeeded", observationResult: "confirmed", requestId: "req", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z" } }); }
      if (url.pathname === "/api/v2/workspaces" && request.method() === "GET") return route.fulfill({ json: { items: [{ id: "ws-refresh", name: "Refresh", capabilityVersionId: "cap-1", computePlanId: "compute-1", storagePlanId: "storage-1", deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", version: "1" }] } });
      if (url.pathname === "/api/v2/workspaces/ws-refresh") return route.fulfill({ json: { id: "ws-refresh", name: "Refresh", capabilityVersionId: "cap-1", computePlanId: "compute-1", storagePlanId: "storage-1", deliveryModel: "agent_saas", status: "active", resourceReadiness: "ready", applicationAvailability: "available", createdAt: "2026-09-27T00:00:00Z", updatedAt: "2026-09-27T00:00:00Z", version: "1" } });
      if (url.pathname === "/api/v2/workspaces/ws-refresh/access") return route.fulfill({ status: 503, json: { error: "DEPENDENCY_UNAVAILABLE" } });
      return route.fulfill({ status: 404, json: { error: "unexpected_v2_route" } });
    });
    await loginCloudFixture(page, demo.origin);
    await page.goto(`${demo.origin}/console/workspaces/new`, { waitUntil: "networkidle" });
    await page.getByLabel("工作空间名称").fill("Refresh");
    await page.getByText("已构建 Agent", { exact: true }).click();
    await page.getByRole("button", { name: "获取准确报价", exact: true }).click();
    await page.getByRole("heading", { name: "确认准确报价与部署条款", exact: true }).waitFor();
    await page.getByRole("checkbox", { name: /我确认以上 Agent/ }).check();
    await page.getByRole("button", { name: "确认并开通 Workspace", exact: true }).click();
    await page.getByText("Serve confirmed access 未确认，暂不开放 WebUI。", { exact: true }).waitFor({ state: "visible", timeout: 5_000 });
    assert.equal(await page.getByRole("button", { name: "打开 Agent WebUI", exact: true }).count(), 0);
    assert.equal(createWrites, 1);
    assert.ok(operationReads >= 1);
    await page.getByRole("button", { name: "刷新状态", exact: true }).click();
    await page.getByText("Serve confirmed access 未确认，暂不开放 WebUI。", { exact: true }).waitFor({ state: "visible" });
    assert.equal(createWrites, 1);
    assert.ok(audit.consoleErrors.every((message) => message.includes("503 (Service Unavailable)")));
    audit.consoleErrors.length = 0;
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});


async function verifyWorkspaceDetailExperience() {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
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
      let budgetUpdate: WorkspaceGatewayBudgetUpdateRequest | null = null;
      await page.route((url) => url.origin === demo.origin && url.pathname === "/api/workspaces/ws-1/gateway-budget", async (route) => {
        if (route.request().method() !== "PATCH") {
          await route.fallback();
          return;
        }
        budgetUpdate = route.request().postDataJSON() as WorkspaceGatewayBudgetUpdateRequest;
        const updatedBudget: SourceEnvelope<WorkspaceGatewayBudgetDTO> = {
          source: "sub2api",
          status: "available",
          available: true,
          fetchedAt: "2026-09-01T00:00:01Z",
          data: {
            workspaceId: "ws-1",
            keyId: "9",
            status: budgetUpdate.enabled === false ? "disabled" : "active",
            quotaUsdMicros: String(budgetUpdate.quotaUsdMicros ?? 10_000_000),
            quotaUsedUsdMicros: "250000",
            rateLimit5hUsdMicros: String(budgetUpdate.rateLimit5hUsdMicros ?? 0),
            rateLimit1dUsdMicros: String(budgetUpdate.rateLimit1dUsdMicros ?? 0),
            rateLimit7dUsdMicros: String(budgetUpdate.rateLimit7dUsdMicros ?? 0),
            usage5hUsdMicros: "0",
            usage1dUsdMicros: "10000",
            usage7dUsdMicros: "30000",
            enabled: budgetUpdate.enabled ?? true,
            updatedAt: "2026-09-01T00:00:01Z"
          }
        };
        await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(updatedBudget) });
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

      if (viewport.name === "mobile") {
        await assertAboveMobileNavigation(page, openWorkspace);
        await assertAboveMobileNavigation(page, passwordRow.getByRole("button").first());
        await assertAboveMobileNavigation(page, keyRow.getByRole("button").first());
      }

      const renewal = page.locator(".workspace-plan-panel");
      await renewal.getByRole("heading", { name: "续费与存储", exact: true }).waitFor({ state: "visible" });
      await renewal.getByText("请在权益到期前自行从工作空间下载并妥善保存数据。到期后，平台不承担数据保管或恢复责任。", { exact: true }).waitFor({ state: "visible" });
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
      const budgetPanel = advanced.locator(".workspace-budget-panel");
      const maintenancePanel = advanced.locator(".workspace-maintenance-panel");
      const deletePanel = page.locator(".workspace-delete-panel");
      await budgetPanel.getByRole("heading", { name: "预算设置", exact: true }).waitFor({ state: "visible" });
      const quotaInput = budgetPanel.getByLabel("总额度（美元）");
      await quotaInput.waitFor({ state: "visible" });
      assert.equal(await quotaInput.inputValue(), "10");
      assert.equal(await budgetPanel.getByText(/micros/i).count(), 0);
      assert.equal(await budgetPanel.getByRole("button", { name: "保存预算", exact: true }).count(), 1);
      assert.equal(await budgetPanel.getByRole("button", { name: /^重置/ }).count(), 0);
      if (viewport.name === "desktop") {
        await quotaInput.fill("12.345678");
        const budgetResponsePromise = page.waitForResponse((response) => response.request().method() === "PATCH"
          && new URL(response.url()).pathname === "/api/workspaces/ws-1/gateway-budget");
        await budgetPanel.getByRole("button", { name: "保存预算", exact: true }).click();
        await budgetResponsePromise;
        assert.deepEqual(budgetUpdate, {
          quotaUsdMicros: 12_345_678,
          rateLimit5hUsdMicros: 0,
          rateLimit1dUsdMicros: 0,
          rateLimit7dUsdMicros: 0,
          enabled: true
        });
      }
      await maintenancePanel.getByRole("heading", { name: "用量维护", exact: true }).waitFor({ state: "visible" });
      assert.equal(await maintenancePanel.getByRole("button", { name: "重置总额度用量", exact: true }).count(), 1);
      assert.equal(await maintenancePanel.getByRole("button", { name: "重置滚动窗口用量", exact: true }).count(), 1);
      assert.equal(await maintenancePanel.getByRole("button", { name: "保存预算", exact: true }).count(), 0);
      assert.equal(await advanced.getByRole("button", { name: "删除工作空间", exact: true }).count(), 0);
      await deletePanel.getByRole("button", { name: "删除工作空间", exact: true }).waitFor({ state: "visible" });

      if (viewport.name === "mobile") {
        await assertAboveMobileNavigation(page, page.getByRole("button", { name: "工作空间列表", exact: true }));
        await assertAboveMobileNavigation(page, quotaInput);
        await assertAboveMobileNavigation(page, maintenancePanel.getByRole("button", { name: "重置总额度用量", exact: true }));
        await assertAboveMobileNavigation(page, maintenancePanel.getByRole("button", { name: "重置滚动窗口用量", exact: true }));
        await assertAboveMobileNavigation(page, deletePanel.getByRole("button", { name: "删除工作空间", exact: true }));
      }

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
        "panel workspace-delete-panel",
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

test("cloud Console reads the customer Workspace from the Workspace owner only", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  const audit = await installBrowserAudit(page, demo.origin);
  const ownerReads: string[] = [];
  const controlPlaneReads: string[] = [];
  const ownerWorkspace = {
    id: "ws-owner",
    name: "Owner Workspace",
    capabilityVersionId: "cap-ready-1",
    computePlanId: "compute-1",
    storagePlanId: "storage-1",
    deliveryModel: "agent_saas",
    status: "active",
    resourceReadiness: "ready",
    applicationAvailability: "available",
    currentPeriodEnd: "2026-10-27T00:00:00Z",
    accessUrl: "https://agent.example.invalid/ws-owner",
    createdAt: "2026-09-27T00:00:00Z",
    updatedAt: "2026-09-27T00:00:00Z",
    version: "1"
  };
  try {
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.origin !== demo.origin) return;
      if (url.pathname.startsWith("/api/v2/workspaces")) ownerReads.push(`${request.method()} ${url.pathname}`);
      if (url.pathname.startsWith("/api/workspaces")) controlPlaneReads.push(`${request.method()} ${url.pathname}`);
    });
    await page.addInitScript({ content: `window.openedWorkspace = null; window.open = (url, target, features) => { window.openedWorkspace = { url: String(url || ""), target: String(target || ""), features: String(features || "") }; return null; };` });
    await page.route("**/api/v2/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      const responses: Record<string, unknown> = {
        "/api/v2/auth/session": { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" },
        "/api/v2/workspaces": { items: [ownerWorkspace] },
        "/api/v2/workspaces/ws-owner": ownerWorkspace,
        "/api/v2/delivery/ws-owner": {
          workspaceId: "ws-owner",
          workspace: { owner: "workspace", state: "active", details: {} },
          capabilityVersion: { owner: "capability", state: "ready", details: {} },
          build: { owner: "build", state: "succeeded", details: {} },
          serve: { owner: "serve", state: "ready", details: { accessUrl: "https://agent.example.invalid/ws-owner" } }
        }
      };
      assert.ok(Object.hasOwn(responses, path), `unexpected BFF request: ${path}`);
      await route.fulfill({ json: responses[path] });
    });
    await loginCloudFixture(page, demo.origin);

    await page.goto(`${demo.origin}/console/workspaces`, { waitUntil: "networkidle" });
    const row = page.locator(".workspace-list-row").first();
    await row.waitFor({ state: "visible" });
    const rowText = await row.innerText();
    assert.match(rowText, /Owner Workspace/);
    assert.match(rowText, /智能体应用/);
    assert.deepEqual(controlPlaneReads, [], "the cloud Console must not list from the Control Plane projection");
    assert.ok(ownerReads.includes("GET /api/v2/workspaces"));

    await row.click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces/ws-owner");
    await page.locator(".workspace-technical-details").waitFor({ state: "visible" });
    const openButton = page.getByRole("button", { name: "打开工作空间", exact: true });
    await openButton.waitFor({ state: "visible" });
    assert.equal(await openButton.isDisabled(), false);
    await openButton.click();
    assert.deepEqual(await page.evaluate(() => (window as Window & { openedWorkspace?: unknown }).openedWorkspace), {
      url: "https://agent.example.invalid/ws-owner",
      target: "_blank",
      features: "noopener,noreferrer"
    });
    await page.locator(".workspace-technical-details > summary").click();
    await page.locator("[data-agent-delivery]").getByText("Capability", { exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(controlPlaneReads, [], "the cloud Console must not read the Control Plane Workspace routes");
    assert.ok(ownerReads.includes("GET /api/v2/workspaces/ws-owner"));
    assertBrowserAuditClean(audit);
  } finally {
    await page.close();
    await browser.close();
    await demo.close();
  }
});


// The model configuration page renders the Workspace owner's own readback. A
// terminal update_models operation whose closeout still needs attention is not
// proof that the running application kept the previous configuration: the
// owner's configuration readback decides which version is applied.
const modelsFixtureUpdatedAt = "2026-10-01T00:00:00Z";

const modelsWorkspaceFixture: WorkspaceDTO = {
  id: "ws-models",
  name: "Model Workspace",
  state: "active",
  deliveryModel: "agent_saas",
  resourceReadiness: "ready",
  applicationAvailability: "available",
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-01T00:00:00Z"
};

const modelsCatalogFixture = {
  items: [
    { id: "model-1", name: "IBD Model", capabilities: ["chat"], available: true, fetchedAt: modelsFixtureUpdatedAt },
    { id: "model-2", name: "Backup Model", capabilities: ["chat"], available: true, fetchedAt: modelsFixtureUpdatedAt }
  ]
};

const modelsDeliveryFixture = {
  workspaceId: "ws-models",
  workspace: { owner: "workspace", state: "active", details: {} },
  capabilityVersion: { owner: "capability", state: "ready", details: {} },
  build: { owner: "build", state: "succeeded", details: {} },
  serve: { owner: "serve", state: "ready", details: {} }
};

const modelsPendingConfiguration: WorkspaceModelConfigurationDTO = {
  workspaceId: "ws-models",
  version: "2",
  appliedVersion: "1",
  selections: [{ slot: "default", modelId: "model-1" }],
  status: "pending",
  operationId: "op-models-1",
  updatedAt: modelsFixtureUpdatedAt
};

const modelsAppliedConfiguration: WorkspaceModelConfigurationDTO = {
  ...modelsPendingConfiguration,
  appliedVersion: "2",
  status: "applied"
};

const modelsUnappliedConfiguration: WorkspaceModelConfigurationDTO = {
  ...modelsPendingConfiguration,
  status: "needs_attention"
};

const modelsNeedsAttentionOperation: WorkspaceOwnerOperationDTO = {
  operationId: "op-models-1",
  owner: "workspace",
  kind: "update_models",
  resourceId: "ws-models",
  status: "needs_attention",
  stage: "verification",
  requestId: "request-models-1",
  createdAt: modelsFixtureUpdatedAt,
  updatedAt: modelsFixtureUpdatedAt
};

async function openWorkspaceModelsRoute(
  page: Page,
  origin: string,
  settledConfiguration: () => WorkspaceModelConfigurationDTO
) {
  const operationReads: string[] = [];
  const unexpectedRequests: string[] = [];
  let configurationReads = 0;
  await page.route("**/api/v2/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
    if (path === "/api/v2/workspaces/ws-models") return route.fulfill({ json: modelsWorkspaceFixture });
    if (path === "/api/v2/workspaces/ws-models/models") {
      configurationReads += 1;
      // The persisted intent is still pending; the terminal operation answer
      // forces the readback that carries the owner's applied facts.
      return route.fulfill({ json: configurationReads === 1 ? modelsPendingConfiguration : settledConfiguration() });
    }
    if (path === "/api/v2/operations/workspace/op-models-1") {
      operationReads.push(`${request.method()} ${path}`);
      return route.fulfill({ json: modelsNeedsAttentionOperation });
    }
    if (path === "/api/v2/catalog/models") return route.fulfill({ json: modelsCatalogFixture });
    unexpectedRequests.push(`${request.method()} ${path}`);
    return route.fulfill({ status: 404, json: { error: "unexpected_request" } });
  });
  const operationRead = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v2/operations/workspace/op-models-1");
  await page.goto(`${origin}/console/workspaces/ws-models/models`, { waitUntil: "domcontentloaded" });
  await operationRead;
  // Both the failure alert and the applied badge are terminal page states, so
  // this settles identically before and after the fix.
  await page.waitForFunction(() => {
    const panel = document.querySelector(".workspace-models-panel");
    if (!panel) return false;
    const text = panel.textContent || "";
    return text.includes("模型配置未生效") || text.includes("已生效");
  });
  return { operationReads, unexpectedRequests, configurationReads: () => configurationReads };
}

function modelStatusCell(page: Page) {
  return page.locator(".workspace-models-panel .data-list > div").filter({ hasText: "配置状态" }).locator("dd");
}

test("Workspace model readback keeps a needs_attention closeout out of the failure alert", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await loginCloudFixture(page, demo.origin);
    const models = await openWorkspaceModelsRoute(page, demo.origin, () => modelsAppliedConfiguration);
    assert.equal(await page.getByText("模型配置未生效", { exact: true }).count(), 0, "a confirmed applied version must not render the failure alert even when the closeout needs attention");
    await modelStatusCell(page).getByText("已生效", { exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(models.operationReads, ["GET /api/v2/operations/workspace/op-models-1"]);
    assert.ok(models.configurationReads() >= 2, "the terminal operation answer must be decided by the owner's configuration readback");

    await page.reload({ waitUntil: "domcontentloaded" });
    await modelStatusCell(page).getByText("已生效", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByText("模型配置未生效", { exact: true }).count(), 0, "a reload of a confirmed applied readback must stay applied without the failure alert");
    assert.deepEqual(models.operationReads, ["GET /api/v2/operations/workspace/op-models-1"], "an applied readback needs no operation read on reload");

    assert.deepEqual(models.unexpectedRequests, []);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Workspace model readback keeps an unapplied closeout on the failure alert", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    await loginCloudFixture(page, demo.origin);
    const models = await openWorkspaceModelsRoute(page, demo.origin, () => modelsUnappliedConfiguration);
    assert.ok(models.configurationReads() >= 2, "the terminal operation answer must be decided by the owner's configuration readback");
    await page.getByText("模型配置未生效", { exact: true }).waitFor({ state: "visible" });
    await modelStatusCell(page).getByText("需要处理", { exact: true }).waitFor({ state: "visible" });
    assert.deepEqual(models.unexpectedRequests, []);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("a late model configuration readback cannot replace the route that replaced its page", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  const releaseLateRead = deferred();
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    let configurationReads = 0;
    await page.route("**/api/v2/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/workspaces/ws-models") return route.fulfill({ json: modelsWorkspaceFixture });
      if (path === "/api/v2/delivery/ws-models") return route.fulfill({ json: modelsDeliveryFixture });
      if (path === "/api/v2/workspaces/ws-models/models") {
        configurationReads += 1;
        if (configurationReads === 1) return route.fulfill({ json: modelsPendingConfiguration });
        await releaseLateRead.promise;
        return route.fulfill({ json: modelsAppliedConfiguration });
      }
      if (path === "/api/v2/operations/workspace/op-models-1") return route.fulfill({ json: modelsNeedsAttentionOperation });
      if (path === "/api/v2/catalog/models") return route.fulfill({ json: modelsCatalogFixture });
      return route.fulfill({ status: 404, json: { error: "unexpected_request" } });
    });
    await loginCloudFixture(page, demo.origin);
    const operationRead = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v2/operations/workspace/op-models-1");
    const lateReadFinished = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v2/workspaces/ws-models/models" && configurationReads >= 2, { timeout: 15_000 });
    await page.goto(`${demo.origin}/console/workspaces/ws-models/models`, { waitUntil: "domcontentloaded" });
    await operationRead;
    await page.getByRole("button", { name: "工作空间详情", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces/ws-models");
    await page.locator(".workspace-identity-panel").getByText("Model Workspace", { exact: true }).waitFor({ state: "visible" });
    releaseLateRead.resolve();
    await lateReadFinished;
    assert.equal(page.url().endsWith("/console/workspaces/ws-models"), true, "the Console must stay on the route the user opened");
    assert.equal(await page.locator(".workspace-models-panel").count(), 0, "the replaced models page must not render on the Workspace detail route");
    assert.equal(await page.getByText("模型配置未生效", { exact: true }).count(), 0, "the late readback must not inject the replaced page's failure alert");
    await page.locator(".workspace-identity-panel").getByRole("button", { name: "模型配置", exact: true }).click();
    await page.waitForURL((url) => url.pathname === "/console/workspaces/ws-models/models");
    await modelStatusCell(page).getByText("已生效", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByText("模型配置未生效", { exact: true }).count(), 0, "re-entering the models page must show the owner's applied readback");
    assertBrowserAuditClean(audit);
  } finally {
    releaseLateRead.resolve();
    await browser.close();
    await demo.close();
  }
});

test("an unanswered model configuration readback never turns into the unapplied verdict", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: viewports[0] });
    const audit = await installBrowserAudit(page, demo.origin);
    const unexpectedRequests: string[] = [];
    let configurationReads = 0;
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === "/api/v2/auth/session") return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
      if (path === "/api/v2/workspaces/ws-models") return route.fulfill({ json: modelsWorkspaceFixture });
      if (path === "/api/v2/workspaces/ws-models/models") {
        configurationReads += 1;
        if (configurationReads === 1) return route.fulfill({ json: modelsPendingConfiguration });
        // The readback the terminal operation forces cannot be decoded, so the
        // owner never returns a verdict for this configuration.
        return route.fulfill({ json: { unexpected: "unreadable_model_readback" } });
      }
      if (path === "/api/v2/operations/workspace/op-models-1") return route.fulfill({ json: modelsNeedsAttentionOperation });
      if (path === "/api/v2/catalog/models") return route.fulfill({ json: modelsCatalogFixture });
      unexpectedRequests.push(`${request.method()} ${path}`);
      return route.fulfill({ status: 404, json: { error: "unexpected_request" } });
    });
    await loginCloudFixture(page, demo.origin);
    const operationRead = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v2/operations/workspace/op-models-1");
    const failedReadback = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v2/workspaces/ws-models/models" && configurationReads >= 2, { timeout: 15_000 });
    await page.goto(`${demo.origin}/console/workspaces/ws-models/models`, { waitUntil: "domcontentloaded" });
    await operationRead;
    await failedReadback;
    await page.getByText("读取模型配置失败", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByText("模型配置未生效", { exact: true }).count(), 0, "a readback the owner never answered must not become the unapplied verdict");
    await page.getByText("模型配置结果待确认", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.getByText("原操作读回不可用", { exact: true }).count(), 0, "the terminal operation was read back and stays displayed");
    assert.deepEqual(unexpectedRequests, []);
    assertBrowserAuditClean(audit);
  } finally {
    await browser.close();
    await demo.close();
  }
});
