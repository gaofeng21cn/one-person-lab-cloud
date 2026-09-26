import assert from "node:assert/strict";
import test from "node:test";
import { chromium } from "playwright";
import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";

const digest = "sha256:" + "a".repeat(64);

test("Cloud WebUI draft renders owner refs and refuses mutable WebUI inputs", { timeout: 30_000 }, async () => {
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await page.route("**/api/v2/**", async (route) => {
      const url = new URL(route.request().url());
      const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (url.pathname === "/api/v2/auth/session") return json({ id: "user-customer", tenantId: "tenant-fixture", csrfToken: "csrf-fixture" });
      if (url.pathname === "/api/v2/namespaces") return json({ items: [{ id: "ns-1", name: "fixture", status: "active" }] });
      if (url.pathname === "/api/v2/packages") return json({ items: [{ id: "pkg-1", name: "fixture-agent", status: "ready" }] });
      if (url.pathname === "/api/v2/catalog/webui-versions") return json({ items: [
        { id: "webui-good", name: "Cloud WebUI fixture", versionLabel: "0.1.0", status: "approved", artifactDigest: digest, admissionReceiptId: "receipt-webui" },
        { id: "webui-latest", name: "mutable", versionLabel: "latest", status: "approved", artifactDigest: "ghcr.io/example/webui:latest" },
        { id: "webui-revoked", name: "revoked", versionLabel: "0.0.1", status: "revoked", artifactDigest: digest }
      ] });
      if (url.pathname === "/api/v2/catalog/runtime-versions") return json({ error: "runtime_catalog_unavailable" }, 503);
      return json({ error: "unexpected_cloud_webui_browser_request", path: url.pathname }, 500);
    });
    await page.goto(`${server.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.goto(`${server.origin}/console/publisher`, { waitUntil: "networkidle" });
    await page.getByText("Fail-closed boundary", { exact: true }).waitFor();
    const webui = page.getByLabel("WebUI");
    assert.equal(await webui.locator("option[value=webui-latest]").evaluate((item) => (item as HTMLOptionElement).disabled), true);
    assert.equal(await webui.locator("option[value=webui-revoked]").count(), 0);
    assert.match(await page.locator(".publisher-selection-readback").getByText("Runtime catalog", { exact: false }).textContent() || "", /unavailable|not exposed/);
    assert.equal(await page.evaluate(() => localStorage.length), 0);
    assert.equal(await page.evaluate(() => JSON.stringify(sessionStorage)), "{}");
    await page.keyboard.press("Tab");
    assert.ok(await page.evaluate(() => document.activeElement?.getAttribute("aria-label") || document.activeElement?.tagName));
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
  } finally {
    await browser.close();
    await server.close();
  }
});
