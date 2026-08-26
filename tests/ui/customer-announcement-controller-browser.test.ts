import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  AnnouncementDTO,
  AnnouncementPageDTO,
  AnnouncementReadDTO,
  SourceEnvelope
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

function announcement(id: string, title: string, read = false): AnnouncementDTO {
  return {
    id,
    title,
    body: `${title} body`,
    status: "published",
    publishedAt: fetchedAt,
    createdAt: fetchedAt,
    updatedAt: fetchedAt,
    read
  };
}

function source<T>(data: T): SourceEnvelope<T> {
  return {
    source: "control-plane",
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

function announcementPage(items: AnnouncementDTO[], pageSize: number): SourceEnvelope<AnnouncementPageDTO> {
  return source({ items, total: items.length, page: 1, pageSize });
}

function receipt(announcementId: string): AnnouncementReadDTO {
  return { announcementId, readAt: "2026-08-27T01:00:00Z" };
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
  await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
  await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForFunction(() => window.location.pathname === "/console/overview");
}

async function openAnnouncementList(page: Page) {
  await page.locator(".side-nav").getByRole("link", { name: "公告", exact: true }).click();
  await page.waitForURL(/\/console\/announcements$/);
  await page.getByRole("heading", { level: 2, name: "公告列表", exact: true }).waitFor({ state: "visible" });
}

function announcementCard(page: Page, title: string) {
  return page.locator(".announcement-item").filter({ hasText: title });
}

async function settle(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

test("Customer Announcement rejects a late Overview 3 result after the list 20 scope loads", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-overview", "Overview target");
  const overviewHeld = deferred();
  const releaseOverview = deferred();
  let overviewReads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", async (route) => {
      const pageSize = new URL(route.request().url()).searchParams.get("pageSize");
      if (pageSize === "3") {
        overviewReads += 1;
        if (overviewReads === 2) {
          overviewHeld.resolve();
          await releaseOverview.promise;
        }
        await fulfill(route, announcementPage(overviewReads === 1
          ? [target]
          : [announcement("announcement-stale", "Overview stale")], 3));
        return;
      }
      await fulfill(route, announcementPage([announcement("announcement-list", "List current")], 20));
    });

    await login(page, demo.origin);
    await announcementCard(page, target.title).waitFor({ state: "visible" });
    await openAnnouncementList(page);
    await page.evaluate(() => {
      window.history.pushState({}, "", "/console/overview");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await overviewHeld.promise;
    await page.evaluate(() => {
      window.history.pushState({}, "", "/console/announcements");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await page.waitForURL(/\/console\/announcements$/);
    await announcementCard(page, "List current").waitFor({ state: "visible" });

    releaseOverview.resolve();
    await settle(page);
    assert.equal(await announcementCard(page, "List current").count(), 1);
    assert.equal(await announcementCard(page, "Overview stale").count(), 0);
  } finally {
    releaseOverview.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Customer Announcement refreshes the current list scope after an Overview command settles", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-overview", "Overview target");
  const commandHeld = deferred();
  const releaseCommand = deferred();
  const commandSettled = deferred();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", async (route) => {
      const pageSize = Number(new URL(route.request().url()).searchParams.get("pageSize"));
      await fulfill(route, announcementPage(pageSize === 3
        ? [target]
        : [announcement("announcement-list", "List current")], pageSize));
    });
    await page.route("**/api/announcements/announcement-overview/read", async (route) => {
      commandHeld.resolve();
      await releaseCommand.promise;
      await fulfill(route, receipt(target.id));
      commandSettled.resolve();
    });

    await login(page, demo.origin);
    await announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true }).click();
    await commandHeld.promise;
    await openAnnouncementList(page);
    await announcementCard(page, "List current").waitFor({ state: "visible" });

    releaseCommand.resolve();
    await commandSettled.promise;
    await settle(page);
    assert.equal(await announcementCard(page, "List current").count(), 1);
  } finally {
    releaseCommand.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Customer Announcement retries response loss with the original read intent", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-retry", "Retry read");
  const other = announcement("announcement-other", "Other read");
  const keys = new Map<string, string[]>();
  let projection = [target, other];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", (route) => fulfill(route, announcementPage(projection, 20)));
    await page.route("**/api/announcements/*/read", async (route) => {
      const announcementId = decodeURIComponent(new URL(route.request().url()).pathname.split("/").at(-2) || "");
      const announcementKeys = keys.get(announcementId) ?? [];
      announcementKeys.push(route.request().headers()["idempotency-key"] || "");
      keys.set(announcementId, announcementKeys);
      if (announcementId === target.id && announcementKeys.length === 1) return route.abort("failed");
      projection = projection.map((item) => item.id === announcementId ? { ...item, read: true } : item);
      await fulfill(route, receipt(announcementId));
    });

    await login(page, demo.origin);
    await openAnnouncementList(page);
    const read = (title: string) => announcementCard(page, title).getByRole("button", { name: "标记已读", exact: true }).click();
    await read(target.title);
    await page.getByRole("status").waitFor({ state: "visible" });
    await read(other.title);
    await announcementCard(page, other.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });
    await read(target.title);
    await announcementCard(page, target.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });

    const targetKeys = keys.get(target.id) ?? [];
    const otherKeys = keys.get(other.id) ?? [];
    assert.equal(targetKeys.length, 2);
    assert.equal(otherKeys.length, 1);
    assert.ok(targetKeys[0]);
    assert.equal(targetKeys[1], targetKeys[0]);
    assert.notEqual(otherKeys[0], targetKeys[0]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Customer Announcement does not downgrade a valid receipt when projection refresh fails", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-refresh", "Refresh failure");
  const keys: string[] = [];
  let projection = target;
  let failRefresh = false;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", async (route) => {
      const pageSize = Number(new URL(route.request().url()).searchParams.get("pageSize"));
      if (pageSize === 20 && failRefresh) {
        failRefresh = false;
        return route.abort("failed");
      }
      await fulfill(route, announcementPage([projection], pageSize));
    });
    await page.route("**/api/announcements/announcement-refresh/read", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      projection = { ...target, read: true };
      failRefresh = true;
      await fulfill(route, receipt(target.id));
    });

    await login(page, demo.origin);
    await openAnnouncementList(page);
    await announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true }).click();
    await announcementCard(page, target.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });
    assert.equal(await page.locator(".toast").count(), 0);

    assert.equal(keys.length, 1);
    assert.ok(keys[0]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Customer Announcement retains its intent until receipt identity matches and does not commit conflicting readback", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-mismatch", "Mismatch read");
  const keys: string[] = [];
  let projection = target;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", (route) => fulfill(route, announcementPage([projection], 20)));
    await page.route("**/api/announcements/announcement-mismatch/read", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) return fulfill(route, receipt("announcement-other"));
      await fulfill(route, receipt(target.id));
    });

    await login(page, demo.origin);
    await openAnnouncementList(page);
    const read = () => announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true }).click();
    await read();
    await page.getByRole("status").waitFor({ state: "visible" });
    await read();
    await page.getByRole("status").waitFor({ state: "visible" });
    await announcementCard(page, target.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.equal(keys[1], keys[0]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Customer Announcement holds a route-spanning claim until its request settles", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-route", "Route read");
  const requestHeld = deferred();
  const releaseRequest = deferred();
  const requestSettled = deferred();
  let projection = target;
  let writes = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", (route) => {
      const pageSize = Number(new URL(route.request().url()).searchParams.get("pageSize"));
      return fulfill(route, announcementPage([projection], pageSize));
    });
    await page.route("**/api/announcements/announcement-route/read", async (route) => {
      writes += 1;
      requestHeld.resolve();
      await releaseRequest.promise;
      projection = { ...target, read: true };
      await fulfill(route, receipt(target.id));
      requestSettled.resolve();
    });

    await login(page, demo.origin);
    await announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true }).click();
    await requestHeld.promise;
    await page.locator(".side-nav").getByRole("link", { name: "Workspace", exact: true }).click();
    await page.waitForURL(/\/console\/workspaces$/);
    await page.locator(".side-nav").getByRole("link", { name: "概览", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    const read = announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true });
    await read.waitFor({ state: "visible" });
    assert.equal(await read.isDisabled(), true);
    assert.equal(writes, 1);

    releaseRequest.resolve();
    await requestSettled.promise;
    await announcementCard(page, target.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });
    assert.equal(writes, 1);
  } finally {
    releaseRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Customer Announcement session reset admits a new claim and rejects stale completion", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const target = announcement("announcement-session", "Session read");
  const requestHeld = deferred();
  const releaseRequest = deferred();
  const requestSettled = deferred();
  const keys: string[] = [];
  let projection = target;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/announcements?*", (route) => {
      const pageSize = Number(new URL(route.request().url()).searchParams.get("pageSize"));
      return fulfill(route, announcementPage([projection], pageSize));
    });
    await page.route("**/api/announcements/announcement-session/read", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) {
        requestHeld.resolve();
        await releaseRequest.promise;
        await fulfill(route, receipt(target.id));
        requestSettled.resolve();
        return;
      }
      projection = { ...target, read: true };
      await fulfill(route, receipt(target.id));
    });

    await login(page, demo.origin);
    await announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true }).click();
    await requestHeld.promise;
    await page.getByRole("button", { name: "打开账号菜单", exact: true }).click();
    await page.getByRole("complementary", { name: "Account Settings" }).getByRole("button", { name: "退出登录", exact: true }).click();
    await page.waitForFunction(() => window.location.pathname === "/");
    await page.locator(".public-nav").getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/login$/);
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    const read = announcementCard(page, target.title).getByRole("button", { name: "标记已读", exact: true });
    await read.waitFor({ state: "visible" });
    assert.equal(await read.isDisabled(), false);
    await read.click();
    await announcementCard(page, target.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });

    releaseRequest.resolve();
    await requestSettled.promise;
    await announcementCard(page, target.title).getByText("已读", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.ok(keys[1]);
    assert.notEqual(keys[1], keys[0]);
  } finally {
    releaseRequest.resolve();
    await browser.close();
    await demo.close();
  }
});
