import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  OperatorAccountCommandDTO,
  OperatorAccountDTO,
  OperatorAccountPageDTO,
  OperatorProvisionAccountCommandDTO,
  OperatorWorkspacePurchaseEligibilityCommandDTO,
  SourceEnvelope
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-26T00:00:00Z";
const unknownWriteMessage = "结果待确认，请刷新操作状态，不要重复提交";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

function source<T>(data: T): SourceEnvelope<T> {
  return {
    source: "control-plane+sub2api",
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

function unavailable<T>(): SourceEnvelope<T> {
  return {
    source: "control-plane+sub2api",
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: "control_plane_sub2api_unavailable"
  };
}

function account(
  accountId: string,
  email: string,
  overrides: Partial<Pick<OperatorAccountDTO, "status" | "workspacePurchaseEnabled">> = {}
): OperatorAccountDTO {
  const suffix = accountId.replace(/[^a-z0-9]/gi, "");
  const status = overrides.status ?? "active";
  const workspacePurchaseEnabled = overrides.workspacePurchaseEnabled ?? true;
  return {
    accountId,
    consoleUserId: `user-${suffix}`,
    role: "owner",
    sub2apiUserId: `gateway-${suffix}`,
    email,
    status,
    workspacePurchaseEnabled,
    gatewayIdentity: {
      source: "sub2api",
      status: "available",
      available: true,
      fetchedAt,
      data: { userId: `gateway-${suffix}`, email, status: "active" }
    },
    wallet: {
      source: "sub2api",
      status: "available",
      available: true,
      fetchedAt,
      data: { userId: `gateway-${suffix}`, currency: "USD", usdMicros: "0", status: "active" }
    },
    keyCount: { source: "sub2api", status: "available", available: true, fetchedAt, data: 0 },
    usage: {
      source: "sub2api",
      status: "available",
      available: true,
      fetchedAt,
      data: { todayActualCostUsdMicros: 0, totalActualCostUsdMicros: 0 }
    },
    workspaceCount: { source: "control-plane", status: "available", available: true, fetchedAt, data: 0 }
  };
}

function accountPage(items: OperatorAccountDTO[], page = 1, total = items.length): SourceEnvelope<OperatorAccountPageDTO> {
  return source({ items, total, page, pageSize: 20 });
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

async function openAccounts(page: Page, origin: string) {
  await page.goto(`${origin}/admin/accounts`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { level: 2, name: "客户与计费账户", exact: true }).waitFor({ state: "visible" });
}

function accountRow(page: Page, email: string) {
  return page.locator(".operator-account-table tbody tr").filter({ hasText: email });
}

async function settle(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

test("Operator Account rejects an older retry result for the same page", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const oldRequestHeld = deferred();
  const releaseOldRequest = deferred();
  const oldRequestSettled = deferred();
  let reads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/accounts?*", async (route) => {
      reads += 1;
      if (reads === 1) return fulfill(route, unavailable<OperatorAccountPageDTO>());
      if (reads === 2) {
        oldRequestHeld.resolve();
        await releaseOldRequest.promise;
        await fulfill(route, accountPage([account("acct-old", "old@example.com")]));
        oldRequestSettled.resolve();
        return;
      }
      await fulfill(route, accountPage([account("acct-new", "new@example.com")]));
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    const retry = page.getByRole("button", { name: "重试", exact: true });
    await retry.click();
    await oldRequestHeld.promise;
    await retry.click();
    await accountRow(page, "new@example.com").waitFor({ state: "visible" });

    releaseOldRequest.resolve();
    await oldRequestSettled.promise;
    await settle(page);
    assert.equal(await accountRow(page, "new@example.com").count(), 1);
    assert.equal(await accountRow(page, "old@example.com").count(), 0);
  } finally {
    releaseOldRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Account invalidates a pending list completion after route exit", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const firstRequestHeld = deferred();
  const releaseFirstRequest = deferred();
  const firstRequestSettled = deferred();
  const secondRequestHeld = deferred();
  const releaseSecondRequest = deferred();
  let reads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/accounts?*", async (route) => {
      reads += 1;
      if (reads === 1) {
        firstRequestHeld.resolve();
        await releaseFirstRequest.promise;
        await fulfill(route, accountPage([account("acct-old", "old-route@example.com")]));
        firstRequestSettled.resolve();
        return;
      }
      secondRequestHeld.resolve();
      await releaseSecondRequest.promise;
      await fulfill(route, accountPage([account("acct-new", "new-route@example.com")]));
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await firstRequestHeld.promise;
    await page.locator(".side-nav").getByRole("link", { name: "运维概览", exact: true }).click();
    await page.waitForURL(/\/admin\/overview$/);
    releaseFirstRequest.resolve();
    await firstRequestSettled.promise;

    await page.locator(".side-nav").getByRole("link", { name: "客户与计费账户", exact: true }).click();
    await secondRequestHeld.promise;
    assert.equal(await accountRow(page, "old-route@example.com").count(), 0);
    releaseSecondRequest.resolve();
    await accountRow(page, "new-route@example.com").waitFor({ state: "visible" });
  } finally {
    releaseFirstRequest.resolve();
    releaseSecondRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Account suppresses a pending command completion after route exit", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const commandHeld = deferred();
  const releaseCommand = deferred();
  const commandSettled = deferred();
  const active = account("acct-route-command", "route-command@example.com");
  let reads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/accounts?*", async (route) => {
      reads += 1;
      await fulfill(route, accountPage([active]));
    });
    await page.route("**/api/operator/accounts/acct-route-command/workspace-purchase-eligibility", async (route) => {
      commandHeld.resolve();
      await releaseCommand.promise;
      const result: OperatorWorkspacePurchaseEligibilityCommandDTO = {
        operationId: "workspace-purchase-eligibility-route-exit",
        accountId: active.accountId,
        status: "succeeded",
        workspacePurchaseEnabled: false
      };
      await fulfill(route, result);
      commandSettled.resolve();
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await accountRow(page, active.email).getByRole("button", { name: "撤销新购", exact: true }).click();
    await commandHeld.promise;
    await page.locator(".side-nav").getByRole("link", { name: "运维概览", exact: true }).click();
    await page.waitForFunction(() => window.location.pathname === "/admin/overview");
    releaseCommand.resolve();
    await commandSettled.promise;
    await settle(page);

    assert.equal(reads, 1);
    assert.equal(await page.getByText("已撤销 Workspace 新购资格", { exact: true }).count(), 0);
    assert.equal(await page.getByText(unknownWriteMessage, { exact: true }).count(), 0);
  } finally {
    releaseCommand.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Account provision retries response loss with the original normalized intent", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const base = account("acct-base", "base@example.com");
  const provisioned = account("acct-provisioned", "retry@example.com");
  const keys: string[] = [];
  let commandSucceeded = false;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/accounts?*", (route) => fulfill(
      route,
      accountPage(commandSucceeded ? [base, provisioned] : [base])
    ));
    await page.route("**/api/operator/accounts", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      keys.push(route.request().headers()["idempotency-key"] || "");
      const input = route.request().postDataJSON() as { email: string; name?: string; admission?: string };
      assert.deepEqual(input, {
        email: "retry@example.com",
        password: "browser-retry-password",
        name: "Retry Owner",
        admission: "full_cloud_customer"
      });
      if (keys.length === 1) return route.abort("failed");
      commandSucceeded = true;
      const result: OperatorProvisionAccountCommandDTO = {
        operationId: "account-provision-retry",
        accountId: provisioned.accountId,
        status: "succeeded",
        workspacePurchaseEnabled: true
      };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await page.getByRole("button", { name: "开通用户", exact: true }).first().click();
    const dialog = page.getByRole("dialog", { name: "开通用户" });
    await dialog.getByLabel("登录邮箱").fill(" Retry@Example.COM ");
    await dialog.getByLabel("初始密码").fill("browser-retry-password");
    await dialog.getByLabel("姓名").fill("  Retry Owner  ");
    const submit = dialog.getByRole("button", { name: "开通用户", exact: true });
    await submit.click();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    await submit.click();
    await dialog.getByText("账户映射已完成权威读回", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.equal(keys[1], keys[0]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Account keeps a pending provision claimed when its Modal closes and reopens", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const base = account("acct-provision-claim-base", "provision-claim-base@example.com");
  const provisioned = account("acct-provision-claim-created", "provision-claim@example.com");
  const firstRequestHeld = deferred();
  const releaseFirstRequest = deferred();
  const firstRequestSettled = deferred();
  const keys: string[] = [];
  let projection = base;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/accounts?*", (route) => fulfill(route, accountPage([projection])));
    await page.route("**/api/operator/accounts", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) {
        firstRequestHeld.resolve();
        await releaseFirstRequest.promise;
        await route.abort("failed");
        firstRequestSettled.resolve();
        return;
      }
      projection = provisioned;
      const result: OperatorProvisionAccountCommandDTO = {
        operationId: "account-provision-claim",
        accountId: provisioned.accountId,
        status: "succeeded",
        workspacePurchaseEnabled: true
      };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await page.getByRole("button", { name: "开通用户", exact: true }).first().click();
    let dialog = page.getByRole("dialog", { name: "开通用户" });
    await dialog.getByLabel("登录邮箱").fill(provisioned.email);
    await dialog.getByLabel("初始密码").fill("pending-provision-password");
    await dialog.getByRole("button", { name: "开通用户", exact: true }).click();
    await firstRequestHeld.promise;

    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await page.getByRole("button", { name: "开通用户", exact: true }).first().click();
    dialog = page.getByRole("dialog", { name: "开通用户" });
    await dialog.getByLabel("登录邮箱").fill(provisioned.email);
    await dialog.getByLabel("初始密码").fill("pending-provision-password");
    const reopenedSubmit = dialog.getByRole("button", { name: "开通用户", exact: true });
    assert.equal(await reopenedSubmit.isDisabled(), true);
    assert.equal(keys.length, 1);

    releaseFirstRequest.resolve();
    await firstRequestSettled.promise;
    await page.waitForFunction(() => {
      const modal = document.querySelector<HTMLElement>("[role='dialog']");
      const button = [...(modal?.querySelectorAll<HTMLButtonElement>("button") || [])]
        .find((candidate) => candidate.textContent?.trim() === "开通用户");
      return Boolean(button && !button.disabled);
    });
    await reopenedSubmit.click();
    await dialog.getByText("账户映射已完成权威读回", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.equal(keys[1], keys[0]);
  } finally {
    releaseFirstRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Account disable retains its key until identity, status, and readback all match", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const active = account("acct-disable", "disable@example.com");
  let projection = active;
  let attempts = 0;
  const keys: string[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/accounts?*", (route) => fulfill(route, accountPage([projection])));
    await page.route("**/api/operator/accounts/acct-disable/disable", async (route) => {
      attempts += 1;
      keys.push(route.request().headers()["idempotency-key"] || "");
      const result: OperatorAccountCommandDTO = {
        operationId: `account-disable-${attempts}`,
        accountId: attempts === 2 ? "acct-other" : active.accountId,
        status: attempts === 3 ? "pending" : "succeeded"
      };
      if (attempts === 4) projection = { ...active, status: "disabled" };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      await accountRow(page, active.email).getByRole("button", { name: "停用", exact: true }).click();
      await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    }
    await accountRow(page, active.email).getByRole("button", { name: "停用", exact: true }).click();
    await page.getByText("客户已停用", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 4);
    assert.ok(keys[0]);
    assert.deepEqual(keys, [keys[0], keys[0], keys[0], keys[0]]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Account keeps a pending disable claimed across route exit and re-entry", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const active = account("acct-disable-claim", "disable-claim@example.com");
  const disabled = { ...active, status: "disabled" as const };
  const firstRequestHeld = deferred();
  const releaseFirstRequest = deferred();
  const firstRequestSettled = deferred();
  const secondRequestHeld = deferred();
  const releaseSecondRequest = deferred();
  const keys: string[] = [];
  const readPages: number[] = [];
  const firstPageAccount = account("acct-disable-claim-first-page", "disable-claim-first-page@example.com");
  const secondPageAccount = account("acct-disable-claim-second-page", "disable-claim-second-page@example.com");
  let projection = active;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/accounts?*", (route) => {
      const requestedPage = Number(new URL(route.request().url()).searchParams.get("page"));
      readPages.push(requestedPage);
      const items = requestedPage === 1
        ? [firstPageAccount]
        : requestedPage === 2
          ? [projection === active ? active : secondPageAccount]
          : [projection];
      return fulfill(route, accountPage(items, requestedPage, 41));
    });
    await page.route("**/api/operator/accounts/acct-disable-claim/disable", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) {
        firstRequestHeld.resolve();
        await releaseFirstRequest.promise;
        await route.abort("failed");
        firstRequestSettled.resolve();
        return;
      }
      projection = disabled;
      secondRequestHeld.resolve();
      await releaseSecondRequest.promise;
      const result: OperatorAccountCommandDTO = {
        operationId: "account-disable-claim",
        accountId: active.accountId,
        status: "succeeded"
      };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await page.getByRole("navigation", { name: "账号分页" }).getByRole("button", { name: "下一页" }).click();
    await accountRow(page, active.email).waitFor({ state: "visible" });
    await accountRow(page, active.email).getByRole("button", { name: "停用", exact: true }).click();
    await firstRequestHeld.promise;

    await page.locator(".side-nav").getByRole("link", { name: "运维概览", exact: true }).click();
    await page.waitForFunction(() => window.location.pathname === "/admin/overview");
    await page.locator(".side-nav").getByRole("link", { name: "客户与计费账户", exact: true }).click();
    await accountRow(page, active.email).waitFor({ state: "visible" });
    const reenteredDisable = accountRow(page, active.email).getByRole("button", { name: "停用", exact: true });
    assert.equal(await reenteredDisable.isDisabled(), true);
    assert.equal(keys.length, 1);

    releaseFirstRequest.resolve();
    await firstRequestSettled.promise;
    await page.waitForFunction(() => {
      const row = [...document.querySelectorAll<HTMLTableRowElement>(".operator-account-table tbody tr")]
        .find((candidate) => candidate.textContent?.includes("disable-claim@example.com"));
      const button = [...(row?.querySelectorAll<HTMLButtonElement>("button") || [])]
        .find((candidate) => candidate.textContent?.trim() === "停用");
      return Boolean(button && !button.disabled);
    });
    const secondCommandReadStart = readPages.length;
    await reenteredDisable.click();
    await secondRequestHeld.promise;
    await page.getByRole("navigation", { name: "账号分页" }).getByRole("button", { name: "上一页" }).click();
    await accountRow(page, firstPageAccount.email).waitFor({ state: "visible" });
    releaseSecondRequest.resolve();
    await page.getByText("客户已停用", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.equal(keys[1], keys[0]);
    assert.deepEqual(readPages.slice(secondCommandReadStart), [1, 2, 1, 3]);
    await page.getByRole("navigation", { name: "账号分页" }).getByText("第 1 / 3 页", { exact: true }).waitFor({ state: "visible" });
    await accountRow(page, firstPageAccount.email).waitFor({ state: "visible" });
    assert.equal(await accountRow(page, active.email).count(), 0);
  } finally {
    releaseFirstRequest.resolve();
    releaseSecondRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Account removes a stale origin row when disable readback finds a migrated target", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const active = account("acct-disable-migrated", "disable-migrated@example.com");
  const disabled = { ...active, status: "disabled" as const };
  const firstPageAccount = account("acct-disable-migrated-first", "disable-migrated-first@example.com");
  const secondPageAccount = account("acct-disable-migrated-second", "disable-migrated-second@example.com");
  const readPages: number[] = [];
  const keys: string[] = [];
  let moved = false;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/accounts?*", (route) => {
      const requestedPage = Number(new URL(route.request().url()).searchParams.get("page"));
      readPages.push(requestedPage);
      const items = requestedPage === 1
        ? [firstPageAccount]
        : requestedPage === 2
          ? [moved ? secondPageAccount : active]
          : [disabled];
      return fulfill(route, accountPage(items, requestedPage, 41));
    });
    await page.route("**/api/operator/accounts/acct-disable-migrated/disable", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      moved = true;
      const result: OperatorAccountCommandDTO = {
        operationId: "account-disable-migrated",
        accountId: active.accountId,
        status: "succeeded"
      };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await page.getByRole("navigation", { name: "账号分页" }).getByRole("button", { name: "下一页" }).click();
    await accountRow(page, active.email).waitFor({ state: "visible" });
    const commandReadStart = readPages.length;
    await accountRow(page, active.email).getByRole("button", { name: "停用", exact: true }).click();
    await page.getByText("客户已停用", { exact: true }).waitFor({ state: "visible" });

    assert.deepEqual(readPages.slice(commandReadStart), [2, 1, 3]);
    assert.equal(keys.length, 1);
    assert.ok(keys[0]);
    await page.getByRole("navigation", { name: "账号分页" }).getByText("第 2 / 3 页", { exact: true }).waitFor({ state: "visible" });
    await accountRow(page, secondPageAccount.email).waitFor({ state: "visible" });
    assert.equal(await accountRow(page, active.email).count(), 0);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Account eligibility retains its key across command and readback mismatches", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const eligible = account("acct-eligibility-stale", "eligibility-stale@example.com", {
    workspacePurchaseEnabled: true
  });
  let projection = eligible;
  let attempts = 0;
  const keys: string[] = [];
  let reads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/accounts?*", async (route) => {
      reads += 1;
      await fulfill(route, accountPage([projection]));
    });
    await page.route("**/api/operator/accounts/acct-eligibility-stale/workspace-purchase-eligibility", async (route) => {
      attempts += 1;
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (attempts === 3) projection = { ...eligible, workspacePurchaseEnabled: false };
      const result: OperatorWorkspacePurchaseEligibilityCommandDTO = {
        operationId: `workspace-purchase-eligibility-${attempts}`,
        accountId: eligible.accountId,
        status: "succeeded",
        workspacePurchaseEnabled: attempts === 1
      };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    const revoke = accountRow(page, eligible.email).getByRole("button", { name: "撤销新购", exact: true });
    await revoke.click();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    assert.equal(reads, 1);
    assert.equal(await page.getByText("已撤销 Workspace 新购资格", { exact: true }).count(), 0);

    await revoke.click();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    assert.equal(reads, 2);
    assert.equal(await page.getByText("已撤销 Workspace 新购资格", { exact: true }).count(), 0);

    await revoke.click();
    await page.getByText("已撤销 Workspace 新购资格", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 3);
    assert.ok(keys[0]);
    assert.deepEqual(keys, [keys[0], keys[0], keys[0]]);
    assert.equal(reads, 3);
    await accountRow(page, eligible.email).getByText("不可新购 Workspace", { exact: true }).waitFor({ state: "visible" });
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Account session reset discards a stale eligibility intent", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const ineligible = account("acct-eligibility", "eligibility@example.com", { workspacePurchaseEnabled: false });
  let projection = ineligible;
  const keys: string[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/accounts?*", (route) => fulfill(route, accountPage([projection])));
    await page.route("**/api/operator/accounts/acct-eligibility/workspace-purchase-eligibility", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 2) projection = { ...ineligible, workspacePurchaseEnabled: true };
      const result: OperatorWorkspacePurchaseEligibilityCommandDTO = {
        operationId: `workspace-purchase-eligibility-${keys.length}`,
        accountId: ineligible.accountId,
        status: "succeeded",
        workspacePurchaseEnabled: true
      };
      await fulfill(route, result);
    });

    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await accountRow(page, ineligible.email).getByRole("button", { name: "授予新购", exact: true }).click();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });

    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await page.waitForFunction(() => window.location.pathname === "/");
    await login(page, demo.origin);
    await openAccounts(page, demo.origin);
    await accountRow(page, ineligible.email).getByRole("button", { name: "授予新购", exact: true }).click();
    await page.getByText("已授予 Workspace 新购资格", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.ok(keys[1]);
    assert.notEqual(keys[1], keys[0]);
  } finally {
    await browser.close();
    await demo.close();
  }
});
