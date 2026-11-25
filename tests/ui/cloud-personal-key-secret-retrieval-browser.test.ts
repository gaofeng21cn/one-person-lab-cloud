import assert from "node:assert/strict";
import test from "node:test";

import type { Page } from "playwright";

import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

// The Gateway owner answers a personal Key command with two different
// answers: the create answer carries the Key summary, and the reveal answer
// carries the plaintext. A customer whose reveal fails must still be able to
// tell that the Key itself was created, because the create already happened
// and only the plaintext is missing. The cloud build is the one that reaches
// the owner through the BFF, so this behavior is proven in that build.
const ownerKey = {
  id: "71",
  name: "Browser Personal Key",
  groupId: "4",
  status: "active",
  kind: "general",
  manageable: true,
  deletable: true,
  ipWhitelist: [] as string[],
  ipBlacklist: [] as string[],
  // The owner's contract publishes the money as an exact decimal string, and
  // the Console compares a readback against the command it sent.
  quotaUSDMicros: "0",
  quotaUsedUSDMicros: "0",
  rateLimit5hUSDMicros: "0",
  rateLimit1dUSDMicros: "0",
  rateLimit7dUSDMicros: "0",
  usage5hUSDMicros: "0",
  usage1dUSDMicros: "0",
  usage7dUSDMicros: "0",
  currentConcurrency: 0,
  lastUsedAt: null,
  lastUsedIp: null,
  expiresAt: null,
  createdAt: "2026-11-25T00:00:00.000Z",
  updatedAt: "2026-11-25T00:00:00.000Z"
};

const ownerGroups = {
  items: [{
    id: "4",
    name: "Browser Group",
    description: "browser group",
    platform: "openai",
    rateMultiplier: 1,
    subscriptionType: "standard",
    status: "active"
  }],
  total: 1
};

interface CreateCommand {
  name?: string;
  groupId?: string;
  quotaUSDMicros?: string;
  ipWhitelist?: string[];
  ipBlacklist?: string[];
  rateLimit5hUSDMicros?: string;
  rateLimit1dUSDMicros?: string;
  rateLimit7dUSDMicros?: string;
}

// The owner answers a create with the Key it stored, so the fixture echoes
// the command instead of inventing a Key of its own.
function storedKey(command: CreateCommand) {
  return {
    ...ownerKey,
    name: command.name ?? ownerKey.name,
    groupId: command.groupId ?? ownerKey.groupId,
    quotaUSDMicros: command.quotaUSDMicros ?? ownerKey.quotaUSDMicros,
    ipWhitelist: command.ipWhitelist ?? [],
    ipBlacklist: command.ipBlacklist ?? [],
    rateLimit5hUSDMicros: command.rateLimit5hUSDMicros ?? ownerKey.rateLimit5hUSDMicros,
    rateLimit1dUSDMicros: command.rateLimit1dUSDMicros ?? ownerKey.rateLimit1dUSDMicros,
    rateLimit7dUSDMicros: command.rateLimit7dUSDMicros ?? ownerKey.rateLimit7dUSDMicros
  };
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

async function routeGatewayOwner(page: Page, answers: { revealFails: boolean }) {
  const requests: string[] = [];
  let key = storedKey({});
  await page.route("**/api/v2/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    requests.push(`${request.method()} ${path}`);
    if (path === "/api/v2/auth/session") {
      return route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
    }
    if (path === "/api/v2/gateway/endpoint") {
      return route.fulfill({ json: { source: "gateway", status: "available", available: true, fetchedAt: "2026-11-25T00:00:00.000Z", data: { baseUrl: "https://api.example.invalid" } } });
    }
    if (path === "/api/v2/gateway/groups") return route.fulfill({ json: ownerGroups });
    if (path === "/api/v2/gateway/keys" && request.method() === "GET") {
      return route.fulfill({ json: { items: [key], total: "1", page: 1, pageSize: 20, pages: 1 } });
    }
    if (path === "/api/v2/gateway/keys" && request.method() === "POST") {
      key = storedKey(request.postDataJSON() as CreateCommand);
      return route.fulfill({ status: 201, json: key });
    }
    if (path === `/api/v2/gateway/keys/${ownerKey.id}` && request.method() === "GET") {
      return route.fulfill({ json: key });
    }
    if (path === `/api/v2/gateway/keys/${ownerKey.id}/reveal` && request.method() === "POST") {
      if (answers.revealFails) {
        // The BFF relays an owner failure with its own machine code and an
        // English sentence that was never written for the customer.
        return route.fulfill({ status: 503, json: { ok: false, error: "owner_request_failed", safeMessage: "publisher request could not be completed" } });
      }
      return route.fulfill({ json: { key, secret: "sk-browser-plaintext" } });
    }
    return route.fulfill({ status: 404, json: { error: "unexpected_v2_route" } });
  });
  return requests;
}

async function openKeyPanel(page: Page, origin: string) {
  const session = await page.request.post(`${origin}/api/auth/login`, { data: CONSOLE_DEMO_CREDENTIALS.customer });
  assert.equal(session.ok(), true);
  await page.goto(`${origin}/console/api/keys`, { waitUntil: "domcontentloaded" });
  await page.locator(".keys-panel").waitFor({ state: "visible" });
}

async function createKey(page: Page, name: string) {
  await page.getByRole("button", { name: "创建 API 密钥", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "创建 API 密钥" });
  await dialog.getByLabel("名称").fill(name);
  await dialog.getByRole("button", { name: "创建", exact: true }).click();
}

test("a created personal Key whose plaintext was not retrieved is reported as a created Key", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    const requests = await routeGatewayOwner(page, { revealFails: true });
    await openKeyPanel(page, demo.origin);
    await createKey(page, "Browser Personal Key");

    // The create is confirmed by the owner's readback, so the Key exists.
    await page.getByText("密钥已创建，明文未取回", { exact: true }).waitFor({ state: "visible" });
    await page.getByText(/已创建，但完整密钥未取回/).waitFor({ state: "visible" });
    // The half-finished create is never reported as the generic failure, and
    // the owner's own English sentence never reaches the customer page.
    assert.equal(await page.getByText("操作未确认", { exact: true }).count(), 0);
    assert.equal(await page.getByText(/publisher request could not be completed/).count(), 0);
    // The Key itself is listed, so the customer can see that it exists.
    await page.locator(".keys-table tbody tr").filter({ hasText: "Browser Personal Key" }).first().waitFor({ state: "visible" });

    assert.ok(requests.includes("POST /api/v2/gateway/keys"), requests.join(", "));
    assert.ok(requests.includes(`POST /api/v2/gateway/keys/${ownerKey.id}/reveal`), requests.join(", "));
  } finally {
    await browser.close();
    await demo.close();
  }
});

test("a created personal Key whose plaintext was retrieved is reported as a finished action", { timeout: 60_000 }, async () => {
  const demo = await startCloudConsoleDemo();
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await routeGatewayOwner(page, { revealFails: false });
    await openKeyPanel(page, demo.origin);
    await createKey(page, "Browser Personal Key");

    await page.getByText("操作完成", { exact: true }).waitFor({ state: "visible" });
    await page.getByText("API 密钥已创建，请立即复制；完整密钥将在 60 秒后隐藏", { exact: true }).waitFor({ state: "visible" });
    // The plaintext reached the customer, so the half-finished answer is absent.
    assert.equal(await page.getByText("密钥已创建，明文未取回", { exact: true }).count(), 0);
    await page.locator(".keys-secret").getByText("sk-browser-plaintext", { exact: true }).waitFor({ state: "visible" });
  } finally {
    await browser.close();
    await demo.close();
  }
});
