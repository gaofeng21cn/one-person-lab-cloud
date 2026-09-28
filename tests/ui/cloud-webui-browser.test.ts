import assert from "node:assert/strict";
import test from "node:test";
import { launchBrowser } from "../../tools/launch-browser.ts";
import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";

const digest = "sha256:" + "a".repeat(64);

test("Cloud publisher selects immutable Runtime and Agent WebUI owner versions", { timeout: 30_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await page.route("**/api/v2/**", async (route) => {
      const url = new URL(route.request().url());
      const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (url.pathname === "/api/v2/auth/context") return json({ csrfToken: "csrf-fixture" });
      if (url.pathname === "/api/v2/auth/login") return json({ actorId: "user-customer", tenantId: "tenant-fixture", displayName: "Customer", permissions: [], csrfToken: "csrf-fixture", expiresAt: "2099-01-01T00:00:00Z" });
      if (url.pathname === "/api/v2/auth/session") return json({ actorId: "user-customer", tenantId: "tenant-fixture", displayName: "Customer", permissions: [], csrfToken: "csrf-fixture", expiresAt: "2099-01-01T00:00:00Z" });
      if (url.pathname === "/api/v2/namespaces") return json({ items: [{ id: "ns-1", name: "fixture", status: "active" }] });
      if (url.pathname === "/api/v2/packages") return json({ items: [{ id: "pkg-1", name: "fixture-agent", status: "ready" }] });
      if (url.pathname === "/api/v2/catalog/webui-versions") return json({ items: [
        { id: "webui-good", name: "Cloud WebUI fixture", versionLabel: "0.1.0", status: "approved", artifactDigest: digest, admissionReceiptId: "receipt-webui" },
        { id: "webui-latest", name: "mutable", versionLabel: "latest", status: "approved", artifactDigest: "ghcr.io/example/webui:latest" },
        { id: "webui-revoked", name: "revoked", versionLabel: "0.0.1", status: "revoked", artifactDigest: digest }
      ] });
      if (url.pathname === "/api/v2/catalog/runtime-versions") return json({ items: [
        { id: "runtime-good", name: "OPL App Runtime", versionLabel: "26.9.26", status: "approved", artifactDigest: digest, admissionReceiptId: "receipt-runtime" },
        { id: "runtime-latest", name: "mutable", versionLabel: "latest", status: "approved", artifactDigest: "ghcr.io/example/runtime:latest" },
        { id: "runtime-revoked", name: "revoked", versionLabel: "0.0.1", status: "revoked", artifactDigest: digest }
      ] });
      return json({ error: "unexpected_cloud_webui_browser_request", path: url.pathname }, 500);
    });
    await page.goto(`${server.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.goto(`${server.origin}/console/publisher`, { waitUntil: "networkidle" });
    await page.getByText("Fail-closed boundary", { exact: true }).waitFor();
    const webui = page.getByLabel("WebUI");
    const runtime = page.getByLabel("Runtime Release");
    assert.equal(await runtime.locator("option[value=runtime-good]").count(), 1);
    assert.equal(await runtime.locator("option[value=runtime-latest]").count(), 0);
    assert.equal(await runtime.locator("option[value=runtime-revoked]").count(), 0);
    assert.equal(await webui.locator("option[value=webui-latest]").evaluate((item) => (item as HTMLOptionElement).disabled), true);
    assert.equal(await webui.locator("option[value=webui-revoked]").count(), 0);
    assert.match(await page.locator(".publisher-selection-readback").getByText("Runtime catalog", { exact: false }).textContent() || "", /sha256:/);
    assert.equal(await page.evaluate(() => localStorage.length), 0);
    assert.equal(await page.evaluate(() => JSON.stringify(sessionStorage)), "{}");
    await page.keyboard.press("Tab");
    assert.ok(await page.evaluate(() => document.activeElement?.getAttribute("aria-label") || document.activeElement?.tagName));
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
