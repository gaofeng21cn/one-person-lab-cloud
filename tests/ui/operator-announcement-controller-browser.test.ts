import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import type {
  AnnouncementDTO,
  AnnouncementDraftRequest,
  OperatorAnnouncementPageDTO,
  SourceEnvelope
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";

const fetchedAt = "2026-08-27T00:00:00Z";
const unknownWriteMessage = "结果待确认，请刷新操作状态，不要重复提交";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
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

function unavailable<T>(): SourceEnvelope<T> {
  return {
    source: "control-plane",
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: "control_plane_unavailable"
  };
}

function announcement(
  id: string,
  title: string,
  status: AnnouncementDTO["status"] = "draft",
  overrides: Partial<AnnouncementDTO> = {}
): AnnouncementDTO {
  return {
    id,
    title,
    body: `${title} body`,
    status,
    createdAt: "2026-08-27T00:00:00Z",
    updatedAt: "2026-08-27T00:00:00Z",
    read: false,
    ...overrides
  };
}

function announcementPage(items: AnnouncementDTO[]): SourceEnvelope<OperatorAnnouncementPageDTO> {
  return source({ items, total: items.length, page: 1, pageSize: 20 });
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

async function openAnnouncements(page: Page, origin: string) {
  await page.goto(`${origin}/admin/overview`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { level: 2, name: "公告管理", exact: true }).waitFor({ state: "visible" });
}

function announcementCard(page: Page, title: string) {
  return page.locator(".announcement-item").filter({ hasText: title });
}

async function settle(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));
}

test("Operator Announcement route renders its own page without the overview projection", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of [
      { width: 1280, height: 900 },
      { width: 390, height: 844 }
    ]) {
      const page = await browser.newPage({ viewport });
      const listed = announcement("announcement-route", "Route-owned announcement");
      let overviewRequests = 0;
      await page.route("**/api/operator/overview", async (route) => {
        overviewRequests += 1;
        await route.abort("blockedbyclient");
      });
      await page.route("**/api/operator/announcements?*", (route) => fulfill(route, announcementPage([listed])));

      await login(page, demo.origin);
      await page.goto(`${demo.origin}/admin/announcements`, { waitUntil: "domcontentloaded" });

      await page.getByRole("heading", { level: 1, name: "公告管理", exact: true }).waitFor({ state: "visible" });
      await page.getByRole("heading", { level: 2, name: "公告管理", exact: true }).waitFor({ state: "visible" });
      await announcementCard(page, listed.title).waitFor({ state: "visible" });
      await page.getByRole("button", { name: "新建草稿", exact: true }).waitFor({ state: "visible" });
      assert.equal(await page.getByRole("heading", { level: 2, name: "运营总览", exact: true }).count(), 0);
      assert.equal(overviewRequests, 0);

      if (viewport.width >= 1000) {
        await page.locator(".side-nav").getByRole("link", { name: "公告管理", exact: true }).waitFor({ state: "visible" });
        assert.equal(
          await page.locator(".side-nav").getByRole("link", { name: "公告管理", exact: true }).getAttribute("aria-current"),
          "page"
        );
      } else {
        assert.equal(await page.locator(".mobile-bottom-nav a").count(), 5);
        assert.equal(await page.locator(".mobile-bottom-nav").getByRole("link", { name: "公告管理", exact: true }).count(), 0);
        assert.equal(await page.evaluate(() => document.body.scrollWidth <= document.body.clientWidth), true);
      }

      await page.close();
    }
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Announcement rejects an older retry result", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const oldRequestHeld = deferred();
  const releaseOldRequest = deferred();
  const oldRequestSettled = deferred();
  let reads = 0;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/announcements?*", async (route) => {
      reads += 1;
      if (reads === 1) return fulfill(route, unavailable<OperatorAnnouncementPageDTO>());
      if (reads === 2) {
        oldRequestHeld.resolve();
        await releaseOldRequest.promise;
        await fulfill(route, announcementPage([announcement("announcement-old", "Old announcement")]));
        oldRequestSettled.resolve();
        return;
      }
      await fulfill(route, announcementPage([announcement("announcement-new", "New announcement")]));
    });

    await login(page, demo.origin);
    await openAnnouncements(page, demo.origin);
    const retry = page.getByRole("button", { name: "重试", exact: true });
    await retry.click();
    await oldRequestHeld.promise;
    await retry.click();
    await announcementCard(page, "New announcement").waitFor({ state: "visible" });

    releaseOldRequest.resolve();
    await oldRequestSettled.promise;
    await settle(page);
    assert.equal(await announcementCard(page, "New announcement").count(), 1);
    assert.equal(await announcementCard(page, "Old announcement").count(), 0);
  } finally {
    releaseOldRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Announcement create retries response loss with one normalized intent", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const created = announcement("announcement-created", "Normalized title", "draft", {
    body: "Normalized body",
    startsAt: "2026-08-28T01:00:00Z"
  });
  const keys: string[] = [];
  const inputs: AnnouncementDraftRequest[] = [];
  let projection: AnnouncementDTO[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/announcements?*", (route) => fulfill(route, announcementPage(projection)));
    await page.route("**/api/operator/announcements", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      keys.push(route.request().headers()["idempotency-key"] || "");
      inputs.push(route.request().postDataJSON() as AnnouncementDraftRequest);
      if (keys.length === 1) {
        projection = [created];
        return route.abort("failed");
      }
      await fulfill(route, created);
    });

    await login(page, demo.origin);
    await openAnnouncements(page, demo.origin);
    await page.getByRole("button", { name: "新建草稿", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "新建公告草稿" });
    await dialog.getByLabel("标题").fill("  Normalized title  ");
    await dialog.getByLabel("正文").fill("  Normalized body  ");
    await dialog.getByLabel("开始时间").fill("  2026-08-28T01:00:00Z  ");
    const save = dialog.getByRole("button", { name: "保存草稿", exact: true });
    await save.click();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    await save.click();
    await announcementCard(page, created.title).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.equal(keys[1], keys[0]);
    assert.deepEqual(inputs, [
      { title: created.title, body: created.body, startsAt: created.startsAt },
      { title: created.title, body: created.body, startsAt: created.startsAt }
    ]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Announcement keeps a pending publish claimed across route exit and re-entry", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const draft = announcement("announcement-publish", "Pending publish");
  const published = { ...draft, status: "published" as const, startsAt: "2026-08-27T03:00:00Z", publishedAt: "2026-08-27T03:00:00Z", updatedAt: "2026-08-27T03:00:00Z" };
  const firstRequestHeld = deferred();
  const releaseFirstRequest = deferred();
  const firstRequestSettled = deferred();
  const keys: string[] = [];
  let projection = draft;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/announcements?*", (route) => fulfill(route, announcementPage([projection])));
    await page.route("**/api/operator/announcements/announcement-publish/publish", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) {
        firstRequestHeld.resolve();
        await releaseFirstRequest.promise;
        await route.abort("failed");
        firstRequestSettled.resolve();
        return;
      }
      const input = route.request().postDataJSON() as { startsAt: string };
      projection = { ...published, startsAt: input.startsAt, publishedAt: input.startsAt, updatedAt: input.startsAt };
      await fulfill(route, projection);
    });

    await login(page, demo.origin);
    await openAnnouncements(page, demo.origin);
    await announcementCard(page, draft.title).getByRole("button", { name: "发布", exact: true }).click();
    await firstRequestHeld.promise;

    await page.locator(".side-nav").getByRole("link", { name: "系统状态", exact: true }).click();
    await page.waitForURL(/\/admin\/system$/);
    await page.locator(".side-nav").getByRole("link", { name: "运维概览", exact: true }).click();
    await page.waitForURL(/\/admin\/overview$/);
    const reenteredPublish = announcementCard(page, draft.title).getByRole("button", { name: "发布", exact: true });
    await reenteredPublish.waitFor({ state: "visible" });
    assert.equal(await reenteredPublish.isDisabled(), true);
    assert.equal(keys.length, 1);

    releaseFirstRequest.resolve();
    await firstRequestSettled.promise;
    await page.waitForFunction(() => {
      const item = [...document.querySelectorAll<HTMLElement>(".announcement-item")]
        .find((candidate) => candidate.textContent?.includes("Pending publish"));
      const button = [...(item?.querySelectorAll<HTMLButtonElement>("button") || [])]
        .find((candidate) => candidate.textContent?.trim() === "发布");
      return Boolean(button && !button.disabled);
    });
    await reenteredPublish.click();
    await announcementCard(page, draft.title).getByText("已发布", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.equal(keys[1], keys[0]);
  } finally {
    releaseFirstRequest.resolve();
    await browser.close();
    await demo.close();
  }
});

test("Operator Announcement retains withdraw intent until command and readback match", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const published = announcement("announcement-withdraw", "Withdraw mismatch", "published", {
    startsAt: "2026-08-27T01:00:00Z",
    publishedAt: "2026-08-27T01:00:00Z"
  });
  const withdrawn = { ...published, status: "withdrawn" as const, updatedAt: "2026-08-27T02:00:00Z" };
  const keys: string[] = [];
  let projection = published;
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("dialog", (dialog) => void dialog.accept());
    await page.route("**/api/operator/announcements?*", (route) => fulfill(route, announcementPage([projection])));
    await page.route("**/api/operator/announcements/announcement-withdraw/withdraw", async (route) => {
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) return fulfill(route, { ...withdrawn, id: "announcement-other" });
      if (keys.length === 3) projection = withdrawn;
      await fulfill(route, withdrawn);
    });

    await login(page, demo.origin);
    await openAnnouncements(page, demo.origin);
    const withdraw = () => announcementCard(page, published.title).getByRole("button", { name: "撤下", exact: true }).click();
    await withdraw();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    await withdraw();
    await page.getByText(unknownWriteMessage, { exact: true }).waitFor({ state: "visible" });
    await withdraw();
    await announcementCard(page, published.title).getByText("已撤下", { exact: true }).waitFor({ state: "visible" });

    assert.equal(keys.length, 3);
    assert.ok(keys[0]);
    assert.deepEqual(keys, [keys[0], keys[0], keys[0]]);
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("Operator Announcement session reset holds the stale claim until settlement and discards its intent", { timeout: 60_000 }, async () => {
  const demo = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  const created = announcement("announcement-session", "Session reset", "draft", { body: "Session reset body" });
  const firstRequestHeld = deferred();
  const releaseFirstRequest = deferred();
  const firstRequestSettled = deferred();
  const keys: string[] = [];
  let projection: AnnouncementDTO[] = [];
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route("**/api/operator/announcements?*", (route) => fulfill(route, announcementPage(projection)));
    await page.route("**/api/operator/announcements", async (route) => {
      if (route.request().method() !== "POST") return route.fallback();
      keys.push(route.request().headers()["idempotency-key"] || "");
      if (keys.length === 1) {
        firstRequestHeld.resolve();
        await releaseFirstRequest.promise;
        await fulfill(route, created);
        firstRequestSettled.resolve();
        return;
      }
      if (keys.length === 2) projection = [created];
      await fulfill(route, created);
    });

    const submit = async () => {
      await page.getByRole("button", { name: "新建草稿", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "新建公告草稿" });
      await dialog.getByLabel("标题").fill(created.title);
      await dialog.getByLabel("正文").fill(created.body);
      await dialog.getByRole("button", { name: "保存草稿", exact: true }).click();
    };

    await login(page, demo.origin);
    await openAnnouncements(page, demo.origin);
    await submit();
    await firstRequestHeld.promise;
    await page.keyboard.press("Escape");
    await page.getByRole("dialog", { name: "新建公告草稿" }).waitFor({ state: "hidden" });
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await page.waitForFunction(() => window.location.pathname === "/");

    await page.locator(".public-nav").getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/login$/);
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.admin.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.admin.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.waitForURL(/\/console\/overview$/);
    await page.locator(".side-nav").getByRole("link", { name: "运维概览", exact: true }).click();
    await page.waitForURL(/\/admin\/overview$/);
    await page.getByRole("heading", { level: 2, name: "公告管理", exact: true }).waitFor({ state: "visible" });
    await page.getByRole("button", { name: "新建草稿", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "新建公告草稿" });
    await dialog.getByLabel("标题").fill(created.title);
    await dialog.getByLabel("正文").fill(created.body);
    const save = dialog.getByRole("button", { name: "保存草稿", exact: true });
    assert.equal(await save.isDisabled(), true);

    releaseFirstRequest.resolve();
    await firstRequestSettled.promise;
    await page.waitForFunction(() => {
      const button = [...document.querySelectorAll<HTMLButtonElement>("button")]
        .find((candidate) => candidate.textContent?.trim() === "保存草稿");
      return Boolean(button && !button.disabled);
    });
    assert.equal(await announcementCard(page, created.title).count(), 0);
    await save.click();
    await announcementCard(page, created.title).waitFor({ state: "visible" });

    assert.equal(keys.length, 2);
    assert.ok(keys[0]);
    assert.ok(keys[1]);
    assert.notEqual(keys[1], keys[0]);
  } finally {
    releaseFirstRequest.resolve();
    await browser.close();
    await demo.close();
  }
});
