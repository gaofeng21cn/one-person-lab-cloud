import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  OperatorResourceDTO,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  OperatorWorkspaceRuntimeImagePolicyDTO,
  OperatorWorkspaceRuntimeImagePreviewDTO,
  SourceEnvelope,
  WorkspaceBillingReceiptDTO,
  WorkspaceDTO,
  WorkspaceRuntimeImageReplacementDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-27T00:00:00Z";
const pageSize = 20;
const targetDigest = `sha256:${"b".repeat(64)}`;

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

function source<T>(data: T, sourceName = "control-plane"): SourceEnvelope<T> {
  return {
    source: sourceName,
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

function unavailable<T>(sourceName: string, reasonCode: string): SourceEnvelope<T> {
  return {
    source: sourceName,
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode
  };
}

function workspace(id: string, name: string, version = "current"): WorkspaceDTO {
  return {
    id,
    ownerAccountId: `account-${id}`,
    ownerUserId: `user-${id}`,
    state: "running",
    createdAt: fetchedAt,
    updatedAt: fetchedAt,
    name: `${name} ${version}`,
    url: `https://workspace.example.invalid/${id}`,
    packageId: "basic",
    storageGb: 10,
    autoRenew: false,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    totalUsdMicros: 52_580_000,
    periodStart: fetchedAt,
    paidThrough: "2026-09-27T00:00:00Z",
    renewalStatus: "manual",
    workspaceApiKeyId: `key-${id}`
  };
}

function receipt(value: WorkspaceDTO, version: string): WorkspaceBillingReceiptDTO {
  return {
    receiptId: `receipt-${value.id}-${version}`,
    type: "billing.workspace_purchased.v1",
    status: "settled",
    workspaceId: value.id,
    createdAt: fetchedAt,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    periodStart: fetchedAt,
    paidThrough: "2026-09-27T00:00:00Z",
    totalUsdMicros: 52_580_000,
    components: {
      compute: {
        resourceType: "compute",
        resourceId: `compute-${value.id}`,
        chargeUsdMicros: 42_580_000
      },
      storage: {
        resourceType: "storage",
        resourceId: `storage-${value.id}`,
        sizeGb: 10,
        chargeUsdMicros: 10_000_000
      }
    }
  };
}

function operatorWorkspace(id: string, name: string, version = "current"): OperatorWorkspaceDTO {
  const value = workspace(id, name, version);
  const ownerAccount = source({ id: value.ownerAccountId }, "control-plane");
  const ownerUser = source({
    id: value.ownerUserId,
    email: `${id}-${version}@example.com`
  }, "control-plane");
  const billingReceipt = receipt(value, version);
  const resource: OperatorResourceDTO = {
    ownerAccount,
    ownerUser,
    workspace: source({ id: value.id, name: value.name }, "control-plane"),
    resourceType: source("compute", "fabric"),
    packageOrSpec: source(`spec-${version}`, "fabric"),
    providerId: source(`provider-${id}-${version}`, "fabric"),
    zone: source("zone-fixture", "fabric"),
    status: source("RUNNING", "fabric"),
    createdAt: source(fetchedAt, "fabric"),
    expiresAt: source("2026-09-27T00:00:00Z", "fabric"),
    lastReadAt: source(fetchedAt, "fabric"),
    operationRef: source(`operation-${id}-${version}`, "control-plane"),
    receiptRef: source(billingReceipt.receiptId, "ledger")
  };
  return {
    workspace: source(value, "control-plane"),
    ownerAccount,
    ownerUser,
    resources: [resource],
    receipt: source(billingReceipt, "ledger"),
    workspaceKeyUsage: source({
      keyId: value.workspaceApiKeyId || "",
      todayActualCostUsdMicros: 10_000,
      totalActualCostUsdMicros: 25_000
    }, "sub2api")
  };
}

function workspacePage(
  items: OperatorWorkspaceDTO[],
  page: number,
  total = items.length
): SourceEnvelope<OperatorWorkspacePageDTO> {
  return source({ items, total, page, pageSize }, "control-plane+fabric+sub2api");
}

function policy(): SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO> {
  return source({
    image: "ghcr.io/opl/workspace:protected",
    digest: targetDigest,
    source: "OPL_WORKSPACE_IMAGE"
  });
}

function imageDigest(workspaceId: string, version: string): string {
  return `sha256:${workspaceId}-${version}`;
}

function preview(
  workspaceId: string,
  version = "current",
  overrides: Partial<OperatorWorkspaceRuntimeImagePreviewDTO> = {}
): OperatorWorkspaceRuntimeImagePreviewDTO {
  return {
    workspaceId,
    workspaceStatus: "running",
    runtimeId: `runtime-${workspaceId}`,
    runtimeStatus: "ready",
    currentImageDigest: imageDigest(workspaceId, version),
    targetImageDigest: targetDigest,
    canReplace: true,
    ...overrides
  };
}

async function fulfill<T>(route: Route, body: T, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body)
  });
}

async function login(page: Page, origin: string) {
  await page.goto(`${origin}/login`, { waitUntil: "domcontentloaded" });
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.admin.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.admin.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForFunction(() => window.location.pathname === "/console/overview");
}

async function openResources(page: Page, origin: string) {
  await page.goto(`${origin}/admin/resources`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { level: 2, name: "Workspace 资源列表", exact: true }).waitFor({ state: "visible" });
}

function workspaceRow(page: Page, workspaceId: string) {
  return page.locator(".operator-workspace-table tbody tr").filter({ hasText: workspaceId });
}

async function selectWorkspace(page: Page, workspaceId: string) {
  await workspaceRow(page, workspaceId).getByRole("button", { name: "查看资源", exact: true }).click();
}

async function settle(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

test("Operator Resource Read loads list and policy and rejects a late page 1 response", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const stalePageHeld = deferred();
  const releaseStalePage = deferred();
  const stalePageSettled = deferred();
  const initialPolicyLoaded = deferred();
  const first = operatorWorkspace("workspace-page-one", "Page One");
  const stale = operatorWorkspace("workspace-page-one-stale", "Page One Stale");
  const second = operatorWorkspace("workspace-page-two", "Page Two");
  let pageOneReads = 0;
  let policyReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/workspaces?*", async (route) => {
      const requestedPage = Number(new URL(route.request().url()).searchParams.get("page"));
      if (requestedPage === 1) {
        pageOneReads += 1;
        if (pageOneReads === 2) {
          stalePageHeld.resolve();
          await releaseStalePage.promise;
          await fulfill(route, workspacePage([stale], 1, 40));
          stalePageSettled.resolve();
          return;
        }
        await fulfill(route, workspacePage([first], 1, 40));
        return;
      }
      await fulfill(route, workspacePage([second], 2, 40));
    });
    await page.route("**/api/operator/workspace-runtime-image-policy", async (route) => {
      policyReads += 1;
      await fulfill(route, policy());
      initialPolicyLoaded.resolve();
    });

    await login(page, demo.origin);
    await openResources(page, demo.origin);
    await workspaceRow(page, first.workspace.available ? first.workspace.data.id : "").waitFor({ state: "visible" });
    await initialPolicyLoaded.promise;
    assert.equal(pageOneReads, 1);
    assert.equal(policyReads, 1);

    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await stalePageHeld.promise;
    await page.getByRole("navigation", { name: "Workspace 分页" })
      .getByRole("button", { name: "下一页", exact: true }).click();
    await workspaceRow(page, second.workspace.available ? second.workspace.data.id : "").waitFor({ state: "visible" });

    releaseStalePage.resolve();
    await stalePageSettled.promise;
    await settle(page);
    assert.equal(await workspaceRow(page, second.workspace.available ? second.workspace.data.id : "").count(), 1);
    assert.equal(await workspaceRow(page, stale.workspace.available ? stale.workspace.data.id : "").count(), 0);
    await page.getByText("第 2 / 2 页", { exact: true }).waitFor({ state: "visible" });
    assert.equal(policyReads, 2);
  } finally {
    releaseStalePage.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Resource Read rejects late detail and preview after selecting another Workspace", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const alphaDetailHeld = deferred();
  const alphaPreviewHeld = deferred();
  const releaseAlpha = deferred();
  const alphaDetailSettled = deferred();
  const alphaPreviewSettled = deferred();
  const alphaList = operatorWorkspace("workspace-alpha", "Alpha", "listed");
  const alphaLate = operatorWorkspace("workspace-alpha", "Alpha", "late");
  const beta = operatorWorkspace("workspace-beta", "Beta", "current");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/workspaces?*", (route) => fulfill(route, workspacePage([alphaList, beta], 1)));
    await page.route("**/api/operator/workspace-runtime-image-policy", (route) => fulfill(route, policy()));
    await page.route("**/api/operator/workspaces/**", async (route) => {
      const pathname = new URL(route.request().url()).pathname;
      const workspaceId = decodeURIComponent(pathname.split("/")[4] || "");
      if (pathname.endsWith("/runtime-image-replacements/preview")) {
        if (workspaceId === "workspace-alpha") {
          alphaPreviewHeld.resolve();
          await releaseAlpha.promise;
          await fulfill(route, source(preview(workspaceId, "late"), "control-plane+fabric"));
          alphaPreviewSettled.resolve();
          return;
        }
        await fulfill(route, source(preview(workspaceId, "current"), "control-plane+fabric"));
        return;
      }
      if (workspaceId === "workspace-alpha") {
        alphaDetailHeld.resolve();
        await releaseAlpha.promise;
        await fulfill(route, source(alphaLate, "control-plane+fabric+ledger"));
        alphaDetailSettled.resolve();
        return;
      }
      await fulfill(route, source(beta, "control-plane+fabric+ledger"));
    });

    await login(page, demo.origin);
    await openResources(page, demo.origin);
    await workspaceRow(page, "workspace-alpha").waitFor({ state: "visible" });
    await selectWorkspace(page, "workspace-alpha");
    await Promise.all([alphaDetailHeld.promise, alphaPreviewHeld.promise]);

    await selectWorkspace(page, "workspace-beta");
    await page.getByText("receipt-workspace-beta-current", { exact: true }).first().waitFor({ state: "visible" });
    await page.getByText(imageDigest("workspace-beta", "current"), { exact: true }).waitFor({ state: "visible" });

    releaseAlpha.resolve();
    await Promise.all([alphaDetailSettled.promise, alphaPreviewSettled.promise]);
    await settle(page);
    assert.ok(await page.getByText("receipt-workspace-beta-current", { exact: true }).count() > 0);
    assert.equal(await page.getByText("receipt-workspace-alpha-late", { exact: true }).count(), 0);
    assert.equal(await page.getByText(imageDigest("workspace-alpha", "late"), { exact: true }).count(), 0);
  } finally {
    releaseAlpha.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Resource Read keeps detail and preview failures independent", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const alpha = operatorWorkspace("workspace-alpha", "Alpha");
  const beta = operatorWorkspace("workspace-beta", "Beta");
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/workspaces?*", (route) => fulfill(route, workspacePage([alpha, beta], 1)));
    await page.route("**/api/operator/workspace-runtime-image-policy", (route) => fulfill(route, policy()));
    await page.route("**/api/operator/workspaces/**", async (route) => {
      const pathname = new URL(route.request().url()).pathname;
      const workspaceId = decodeURIComponent(pathname.split("/")[4] || "");
      if (pathname.endsWith("/runtime-image-replacements/preview")) {
        await fulfill(route, workspaceId === "workspace-beta"
          ? unavailable<OperatorWorkspaceRuntimeImagePreviewDTO>("control-plane+fabric", "preview_unavailable")
          : source(preview(workspaceId), "control-plane+fabric"));
        return;
      }
      await fulfill(route, workspaceId === "workspace-alpha"
        ? unavailable<OperatorWorkspaceDTO>("control-plane+fabric+ledger", "detail_unavailable")
        : source(beta, "control-plane+fabric+ledger"));
    });

    await login(page, demo.origin);
    await openResources(page, demo.origin);
    await workspaceRow(page, "workspace-alpha").waitFor({ state: "visible" });

    await selectWorkspace(page, "workspace-alpha");
    await page.getByText("资源详情暂不可用", { exact: true }).waitFor({ state: "visible" });
    await page.getByText(imageDigest("workspace-alpha", "current"), { exact: true }).waitFor({ state: "visible" });

    await selectWorkspace(page, "workspace-beta");
    await page.getByText("receipt-workspace-beta-current", { exact: true }).first().waitFor({ state: "visible" });
    assert.ok(await page.getByText("provider-workspace-beta-current", { exact: true }).count() > 0);
    assert.equal(await page.getByText(imageDigest("workspace-beta", "current"), { exact: true }).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Resource Read rejects late list, detail, and preview after route and Session reset", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const routeListHeld = deferred();
  const routeDetailHeld = deferred();
  const routePreviewHeld = deferred();
  const releaseRouteReads = deferred();
  const routeReadsSettled = deferred();
  const sessionListHeld = deferred();
  const sessionDetailHeld = deferred();
  const sessionPreviewHeld = deferred();
  const releaseSessionReads = deferred();
  const sessionReadsSettled = deferred();
  const logoutHeld = deferred();
  const releaseLogout = deferred();
  const routeOld = operatorWorkspace("workspace-route-old", "Route Old", "late");
  const current = operatorWorkspace("workspace-current", "Current", "fresh");
  const sessionLate = operatorWorkspace("workspace-current", "Current", "session-late");
  let listReads = 0;
  let sessionResetPending = false;
  let routeSettlements = 0;
  let sessionSettlements = 0;
  const settleRouteRead = () => {
    routeSettlements += 1;
    if (routeSettlements === 3) routeReadsSettled.resolve();
  };
  const settleSessionRead = () => {
    sessionSettlements += 1;
    if (sessionSettlements === 3) sessionReadsSettled.resolve();
  };
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/workspaces?*", async (route) => {
      listReads += 1;
      if (sessionResetPending) {
        sessionListHeld.resolve();
        await releaseSessionReads.promise;
        await fulfill(route, workspacePage([sessionLate], 1));
        settleSessionRead();
        return;
      }
      if (listReads === 2) {
        routeListHeld.resolve();
        await releaseRouteReads.promise;
        await fulfill(route, workspacePage([routeOld], 1));
        settleRouteRead();
        return;
      }
      await fulfill(route, workspacePage([listReads === 1 ? routeOld : current], 1));
    });
    await page.route("**/api/operator/workspace-runtime-image-policy", (route) => fulfill(route, policy()));
    await page.route("**/api/operator/workspaces/**", async (route) => {
      const pathname = new URL(route.request().url()).pathname;
      const workspaceId = decodeURIComponent(pathname.split("/")[4] || "");
      const isPreview = pathname.endsWith("/runtime-image-replacements/preview");
      if (sessionResetPending && workspaceId === "workspace-current") {
        (isPreview ? sessionPreviewHeld : sessionDetailHeld).resolve();
        await releaseSessionReads.promise;
        await fulfill(route, isPreview
          ? source(preview(workspaceId, "session-late"), "control-plane+fabric")
          : source(sessionLate, "control-plane+fabric+ledger"));
        settleSessionRead();
        return;
      }
      if (workspaceId === "workspace-route-old") {
        (isPreview ? routePreviewHeld : routeDetailHeld).resolve();
        await releaseRouteReads.promise;
        await fulfill(route, isPreview
          ? source(preview(workspaceId, "late"), "control-plane+fabric")
          : source(routeOld, "control-plane+fabric+ledger"));
        settleRouteRead();
        return;
      }
      await fulfill(route, isPreview
        ? source(preview(workspaceId, "fresh"), "control-plane+fabric")
        : source(current, "control-plane+fabric+ledger"));
    });
    await page.route("**/api/auth/logout", async (route) => {
      logoutHeld.resolve();
      await releaseLogout.promise;
      await fulfill(route, {});
    });

    await login(page, demo.origin);
    await openResources(page, demo.origin);
    await workspaceRow(page, "workspace-route-old").waitFor({ state: "visible" });
    await selectWorkspace(page, "workspace-route-old");
    await Promise.all([routeDetailHeld.promise, routePreviewHeld.promise]);
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await routeListHeld.promise;

    await page.locator(".side-nav").getByRole("link", { name: "系统状态", exact: true }).click();
    await page.waitForURL(/\/admin\/system$/);
    await page.locator(".side-nav").getByRole("link", { name: "资源状态", exact: true }).click();
    await page.waitForURL(/\/admin\/resources$/);
    await workspaceRow(page, "workspace-current").waitFor({ state: "visible" });
    await selectWorkspace(page, "workspace-current");
    await page.getByText("receipt-workspace-current-fresh", { exact: true }).first().waitFor({ state: "visible" });
    await page.getByText(imageDigest("workspace-current", "fresh"), { exact: true }).waitFor({ state: "visible" });

    releaseRouteReads.resolve();
    await routeReadsSettled.promise;
    await settle(page);
    assert.equal(await page.getByText("receipt-workspace-route-old-late", { exact: true }).count(), 0);
    assert.equal(await page.getByText(imageDigest("workspace-route-old", "late"), { exact: true }).count(), 0);
    assert.ok(await page.getByText("receipt-workspace-current-fresh", { exact: true }).count() > 0);

    sessionResetPending = true;
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await sessionListHeld.promise;
    await selectWorkspace(page, "workspace-current");
    await Promise.all([sessionDetailHeld.promise, sessionPreviewHeld.promise]);
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await logoutHeld.promise;
    await page.getByRole("heading", { name: "正在安全退出", exact: true }).waitFor({ state: "visible" });

    releaseSessionReads.resolve();
    await sessionReadsSettled.promise;
    await settle(page);
    assert.equal(await page.getByText("receipt-workspace-current-session-late", { exact: true }).count(), 0);
    assert.equal(await page.getByText(imageDigest("workspace-current", "session-late"), { exact: true }).count(), 0);

    releaseLogout.resolve();
    await page.waitForURL(`${demo.origin}/`);
  } finally {
    releaseRouteReads.resolve();
    releaseSessionReads.resolve();
    releaseLogout.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Runtime Image Replacement refreshes only the selected Workspace detail and preview", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const alpha = operatorWorkspace("workspace-alpha", "Alpha");
  const betaBefore = operatorWorkspace("workspace-beta", "Beta", "before");
  const betaAfter = operatorWorkspace("workspace-beta", "Beta", "after");
  const replacement: WorkspaceRuntimeImageReplacementDTO = {
    operationId: "replace-workspace-beta",
    status: "succeeded",
    phase: "completed",
    workspaceId: "workspace-beta",
    runtimeId: "runtime-workspace-beta",
    previousImageDigest: imageDigest("workspace-beta", "before"),
    replacementImageDigest: targetDigest,
    reason: "promote the protected Workspace image release",
    createdAt: fetchedAt,
    updatedAt: fetchedAt
  };
  let listReads = 0;
  let policyReads = 0;
  let alphaDetailReads = 0;
  let alphaPreviewReads = 0;
  let betaDetailReads = 0;
  let betaPreviewReads = 0;
  let replacementWrites = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/workspaces?*", async (route) => {
      listReads += 1;
      await fulfill(route, workspacePage([alpha, betaBefore], 1));
    });
    await page.route("**/api/operator/workspace-runtime-image-policy", async (route) => {
      policyReads += 1;
      await fulfill(route, policy());
    });
    await page.route("**/api/operator/workspaces/**", async (route) => {
      const pathname = new URL(route.request().url()).pathname;
      const workspaceId = decodeURIComponent(pathname.split("/")[4] || "");
      const isPreview = pathname.endsWith("/runtime-image-replacements/preview");
      const isReplacement = pathname.endsWith("/runtime-image-replacements") && route.request().method() === "POST";
      if (isReplacement) {
        replacementWrites += 1;
        await fulfill(route, replacement);
        return;
      }
      if (workspaceId === "workspace-alpha") {
        if (isPreview) alphaPreviewReads += 1;
        else alphaDetailReads += 1;
        await fulfill(route, isPreview
          ? source(preview(workspaceId), "control-plane+fabric")
          : source(alpha, "control-plane+fabric+ledger"));
        return;
      }
      if (isPreview) {
        betaPreviewReads += 1;
        const readback = betaPreviewReads === 1
          ? preview(workspaceId, "before")
          : preview(workspaceId, "after", {
            currentImageDigest: targetDigest,
            canReplace: false
          });
        await fulfill(route, source(readback, "control-plane+fabric"));
        return;
      }
      betaDetailReads += 1;
      await fulfill(route, source(betaDetailReads === 1 ? betaBefore : betaAfter, "control-plane+fabric+ledger"));
    });

    await login(page, demo.origin);
    await openResources(page, demo.origin);
    await workspaceRow(page, "workspace-beta").waitFor({ state: "visible" });
    await selectWorkspace(page, "workspace-beta");
    await page.getByText("receipt-workspace-beta-before", { exact: true }).first().waitFor({ state: "visible" });
    await page.getByText(imageDigest("workspace-beta", "before"), { exact: true }).waitFor({ state: "visible" });

    await page.getByRole("button", { name: "升级到受保护版本", exact: true }).click();
    await page.getByText("Workspace WebUI 镜像已升级", { exact: true }).waitFor({ state: "visible" });
    await page.getByText("receipt-workspace-beta-after", { exact: true }).first().waitFor({ state: "visible" });
    await page.getByText(targetDigest, { exact: true }).first().waitFor({ state: "visible" });

    assert.deepEqual({
      listReads,
      policyReads,
      alphaDetailReads,
      alphaPreviewReads,
      betaDetailReads,
      betaPreviewReads,
      replacementWrites
    }, {
      listReads: 1,
      policyReads: 1,
      alphaDetailReads: 0,
      alphaPreviewReads: 0,
      betaDetailReads: 2,
      betaPreviewReads: 3,
      replacementWrites: 1
    });
  } finally {
    await browser.close();
    await demo.close();
  }
});
