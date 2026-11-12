import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { launchBrowser } from "../../tools/launch-browser.ts";
import { startConsoleDemoServer } from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import type { AdminRuntimeVersion, RuntimeAdmission } from "../../apps/console-ui/src/api/admin-runtime-api.ts";

const schema = JSON.parse(await readFile(new URL("../../docs/spec/target/contracts/publisher-contract.schema.json", import.meta.url), "utf8"));
const originalContract = schema.examples.find((entry: { kind: string }) => entry.kind === "runtime");
const contract = structuredClone(originalContract);
contract.image.digest = "sha256:" + "e".repeat(64);
contract.applicationRevisionTemplate.image = `${contract.image.repository}@${contract.image.digest}`;
const input: RuntimeAdmission = { name: "opl-app", versionLabel: "2.0.0", publisherNamespaceId: contract.publisherNamespaceId, admissionReceiptId: "receipt-new", publisherContract: contract };
function version(id: string, label: string): AdminRuntimeVersion {
  return { ...input, id, versionLabel: label, artifactDigest: contract.image.digest, status: "approved", runtimeAbiVersion: contract.runtimeAbiVersion,
    defaultForNewBuilds: id === "runtime-old", publisherContractDigest: "sha256:" + "c".repeat(64), publisherContractObjectRef: `runtime-contract:${id}`, createdAt: "2026-11-12T00:00:00Z" };
}
const old = { ...version("runtime-old", "1.0.0"), artifactDigest: originalContract.image.digest, publisherContract: originalContract };
const admitted = version("runtime-new", "2.0.0");

function gate() {
  let release!: () => void;
  const promise = new Promise<void>(resolve => { release = resolve; });
  return { promise, release };
}

test("Admin Runtime management uses immutable admission and owner readback only", { timeout: 180_000 }, async t => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    async function scenario(options: { holdReadback?: boolean; readFails?: boolean; loseCommand?: boolean; reject?: number; mismatch?: boolean; member?: boolean; empty?: boolean; listFails?: boolean } = {}) {
      const context = await browser.newContext();
      const page = await context.newPage();
      page.setDefaultTimeout(12_000);
      const requests: { method: string; path: string; body?: unknown; key?: string; csrf?: string }[] = [];
      const unexpected: string[] = [];
      const pageErrors: string[] = [];
      const readbackEntered = gate(); const readbackResponse = gate();
      let commands = 0; let readbacks = 0; let listFails = options.listFails;
      page.on("pageerror", error => pageErrors.push(error.message));
      await context.route("**/*", async route => {
        const request = route.request();
        const url = new URL(request.url());
        const path = url.pathname;
        const method = request.method();
        const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
        if (url.origin !== server.origin) { unexpected.push(request.url()); return route.abort("blockedbyclient"); }
        if (path === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
        if (!path.startsWith("/api/")) return route.continue();
        if (path === "/api/v2/auth/session") return json({ actorId: "admin-1", displayName: "Admin", permissions: options.member ? [] : ["createPublisherNamespace", "registerRuntimeVersion"], csrfToken: "csrf-admin", expiresAt: "2099-01-01T00:00:00Z" });
        if (path === "/api/v2/operator/health") return json({ source: "control-plane", status: "unavailable", available: false, fetchedAt: "2026-11-12T00:00:00Z", reasonCode: "owner_unavailable" });
        requests.push({ method, path, ...(method === "POST" ? { body: request.postDataJSON(), key: request.headers()["idempotency-key"], csrf: request.headers()["x-csrf-token"] } : {}) });
        if (method === "POST" && path === "/api/v2/admin/catalog/runtime-versions") {
          commands++;
          if (options.reject) return json({ message: "publisher contract fails approved schema" }, options.reject);
          if (options.loseCommand && commands === 1) return route.abort("failed");
          return json(admitted, 201);
        }
        if (method === "GET" && path === "/api/v2/catalog/runtime-versions") {
          if (listFails) return json({ message: "runtime catalog unavailable" }, 503);
          if (url.searchParams.get("cursor") === "after-old") {
            if (commands > 0) {
              readbacks++; readbackEntered.release();
              if (options.holdReadback && readbacks === 1) await readbackResponse.promise;
              if (options.readFails && readbacks === 1) return json({ message: "runtime catalog unavailable" }, 503);
              return json({ items: options.mismatch ? [] : [admitted] });
            }
            return json({ items: [] });
          }
          return json({ items: options.empty ? [] : [old], ...(options.empty ? {} : { nextCursor: "after-old" }) });
        }
        unexpected.push(`${method} ${path}`);
        return json({ message: "unexpected runtime management request" }, 500);
      });
      await page.goto(`${server.origin}/admin/system`, { waitUntil: "networkidle" });
      const panel = page.getByRole("region", { name: "Runtime 版本管理", exact: true });
      async function fill() {
        await panel.getByText("准入新 Runtime 版本", { exact: true }).click();
        await panel.getByLabel("Runtime 名称", { exact: true }).fill(input.name);
        await panel.getByLabel("版本标签", { exact: true }).fill(input.versionLabel);
        await panel.getByText("工程准入参数", { exact: true }).click();
        await panel.getByLabel("发布者命名空间 ID", { exact: true }).fill(input.publisherNamespaceId);
        await panel.getByLabel("准入回执 ID", { exact: true }).fill(input.admissionReceiptId);
        await panel.getByLabel("Runtime 发布契约 JSON").fill(JSON.stringify(contract));
      }
      return { page, panel, requests, fill, readbackEntered, readbackResponse, recoverList: () => { listFails = false; }, close: async () => {
        readbackResponse.release();
        assert.deepEqual(unexpected, []);
        assert.deepEqual(pageErrors, []);
        await context.close();
      } };
    }

    await t.test("entry reads all pages and success waits for exact catalog readback", async () => {
      const s = await scenario({ holdReadback: true });
      try {
        await s.panel.getByText("当前默认", { exact: true }).waitFor();
        await s.fill();
        await s.panel.getByRole("button", { name: "提交新版本准入" }).click();
        await s.readbackEntered.promise;
        assert.equal(await s.panel.getByText(/已从 Runtime Control 目录确认新版本/).count(), 0);
        assert.equal(await s.panel.getByRole("button", { name: "提交新版本准入" }).isDisabled(), true);
        s.readbackResponse.release();
        await s.panel.getByText(/已从 Runtime Control 目录确认新版本：opl-app \/ 2.0.0（runtime-new）/).waitFor();
        const writes = s.requests.filter(request => request.method !== "GET");
        assert.equal(writes.length, 1);
        assert.deepEqual(writes[0].body, input);
        assert.equal(writes[0].csrf, "csrf-admin");
        assert.ok(writes[0].key);
        assert.equal(await s.panel.getByRole("row").count(), 3);
        const rows = s.panel.getByRole("row");
        await rows.nth(1).getByText("查看版本事实", { exact: true }).click();
        await rows.nth(1).getByText(originalContract.image.digest, { exact: true }).waitFor();
        await rows.nth(2).getByText("查看版本事实", { exact: true }).click();
        await rows.nth(2).getByText(contract.image.digest, { exact: true }).waitFor();
        await s.page.setViewportSize({ width: 390, height: 844 });
        assert.ok(await s.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
      } finally { await s.close(); }
    });

    await t.test("readback outage retries reads without issuing a second admission", async () => {
      const s = await scenario({ readFails: true });
      try {
        await s.fill(); await s.panel.getByRole("button", { name: "提交新版本准入" }).click();
        await s.panel.getByRole("button", { name: "重试本次准入与读回" }).waitFor();
        assert.equal(await s.panel.getByText(/已从 Runtime Control 目录确认新版本/).count(), 0);
        assert.equal(await s.panel.getByLabel("Runtime 名称", { exact: true }).isDisabled(), true);
        await s.panel.getByRole("button", { name: "重试本次准入与读回" }).click();
        await s.panel.getByText(/已从 Runtime Control 目录确认新版本/).waitFor();
        assert.equal(s.requests.filter(request => request.method === "POST").length, 1);
      } finally { await s.close(); }
    });

    await t.test("lost command response retries the exact body and original idempotency key", async () => {
      const s = await scenario({ loseCommand: true });
      try {
        await s.fill(); await s.panel.getByRole("button", { name: "提交新版本准入" }).click();
        await s.panel.getByRole("button", { name: "重试本次准入与读回" }).click();
        await s.panel.getByText(/已从 Runtime Control 目录确认新版本/).waitFor();
        const writes = s.requests.filter(request => request.method === "POST");
        assert.equal(writes.length, 2);
        assert.deepEqual(writes[0], writes[1]);
      } finally { await s.close(); }
    });

    for (const status of [400, 403, 409]) await t.test(`owner ${status} rejection is actionable and never succeeds`, async () => {
      const s = await scenario({ reject: status });
      try {
        await s.fill(); await s.panel.getByRole("button", { name: "提交新版本准入" }).click();
        await s.panel.getByRole("alert").waitFor();
        assert.match(await s.panel.getByRole("alert").innerText(), status === 403 ? /平台管理员权限/ : status === 409 ? /刷新目录/ : /完整发布契约/);
        assert.equal(await s.panel.getByText(/已从 Runtime Control 目录确认新版本/).count(), 0);
        assert.equal(await s.panel.getByLabel("Runtime 名称", { exact: true }).isEnabled(), true);
      } finally { await s.close(); }
    });

    await t.test("missing new version in readback stays unconfirmed", async () => {
      const s = await scenario({ mismatch: true });
      try {
        await s.fill(); await s.panel.getByRole("button", { name: "提交新版本准入" }).click();
        await s.panel.getByRole("button", { name: "重试本次准入与读回" }).waitFor();
        assert.equal(await s.panel.getByText(/已从 Runtime Control 目录确认新版本/).count(), 0);
        assert.match(await s.panel.getByRole("alert").innerText(), /尚未从 Runtime 目录确认/);
      } finally { await s.close(); }
    });

    await t.test("invalid JSON is rejected before any mutation", async () => {
      const s = await scenario();
      try {
        await s.fill();
        await s.panel.getByLabel("Runtime 发布契约 JSON").fill("{");
        await s.panel.getByRole("button", { name: "提交新版本准入" }).click();
        await s.panel.getByRole("alert").waitFor();
        assert.equal(s.requests.filter(request => request.method === "POST").length, 0);
      } finally { await s.close(); }
    });

    await t.test("unavailable catalog offers refresh and recovers to authoritative empty state", async () => {
      const s = await scenario({ listFails: true, empty: true });
      try {
        await s.panel.getByRole("alert").waitFor();
        assert.equal(await s.panel.getByText(/目录暂无版本/).count(), 0);
        s.recoverList();
        await s.panel.getByRole("button", { name: "刷新版本目录" }).click();
        await s.panel.getByText(/Runtime Control 目录暂无版本/).waitFor();
        assert.equal(s.requests.filter(request => request.method !== "GET").length, 0);
      } finally { await s.close(); }
    });

    await t.test("member session cannot enter admin management or fetch the runtime directory", async () => {
      const s = await scenario({ member: true });
      try {
        assert.equal(new URL(s.page.url()).pathname, "/403");
        assert.equal(await s.panel.count(), 0);
        assert.deepEqual(s.requests, []);
      } finally { await s.close(); }
    });
  } finally {
    await browser.close(); await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
