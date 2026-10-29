import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "vite";
import react from "@vitejs/plugin-react";
import { launchBrowser } from "../../tools/launch-browser.ts";

// Serve the real standalone component, leaving the shared detail page and its
// other worker's credentials wiring untouched.
test("native Default App UI keeps the original command after unknown response and waits for readiness", { timeout: 60_000 }, async () => {
  const server = await createServer({ configFile: false, server: { host: "127.0.0.1", port: 0 }, plugins: [react(), {
    name: "native-app-upgrade-test-page",
    configureServer(vite) {
      vite.middlewares.use(async (request, response, next) => {
        if (request.url !== "/__native-app-upgrade.html") return next();
        const html = await vite.transformIndexHtml(request.url, `<!doctype html><html><body><div id="root"></div><script type="module">
          import React from 'react'; import { createRoot } from 'react-dom/client';
          import { NativeDefaultAppUpgrade } from '/apps/console-ui/src/components/workspaces/NativeDefaultAppUpgrade.tsx';
          const root = createRoot(document.getElementById('root'));
          window.mountUpgrade = (workspaceId, actorId = 'actor-original') => root.render(React.createElement(NativeDefaultAppUpgrade, { workspaceId, actorId, csrfToken: 'csrf-original', onUpgraded: () => { document.body.dataset.completions = String(Number(document.body.dataset.completions || 0) + 1); } }));
          window.mountUpgrade('ws_1042');
        </script></body></html>`);
        response.setHeader("Content-Type", "text/html"); response.end(html);
      });
    }
  }] });
  await server.listen();
  const address = server.httpServer!.address();
  if (!address || typeof address === "string") throw new Error("test_vite_address_missing");
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage();
    const writes: Array<{ body: unknown; key: string | undefined }> = [];
    const reads: string[] = [];
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request(), url = new URL(request.url());
      if (request.method() === "GET") reads.push(url.pathname);
      const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(value) });
      if (url.pathname === "/api/v2/catalog/runtime-versions") return json({ items: [{ id: "rv-next", name: "OPL App", versionLabel: "2", status: "approved" }, { id: "rv-revoked", name: "Old App", versionLabel: "0", status: "revoked" }] });
      if (url.pathname.endsWith("/deployments")) return json({ items: [{ id: "dep-original", workspaceId: url.pathname.split("/")[4], status: "active" }] });
      if (url.pathname.endsWith("/update")) {
        writes.push({ body: request.postDataJSON(), key: request.headers()["idempotency-key"] });
        assert.equal(request.headers()["x-csrf-token"], "csrf-original");
        if (writes.length === 1) return route.abort("failed");
        return json({ owner: "serve", kind: "update_workspace", operationId: "op-original", status: writes.length === 2 ? "running" : "succeeded", ...(writes.length === 2 ? { pollAfterSeconds: 1 } : {}) }, 202);
      }
      return json({ error: "unexpected_route" }, 500);
    });
    await page.goto(`http://127.0.0.1:${address.port}/__native-app-upgrade.html`);
    const choose = page.getByRole("button", { name: "选择升级版本", exact: true });
    await choose.waitFor();
    assert.deepEqual(reads, [], "opening a Workspace must not load upgrade-only owner facts");
    await choose.click();
    const target = page.getByRole("combobox", { name: "目标 App 版本" });
    await target.waitFor();
    assert.equal(await target.locator("option").count(), 2);
    await target.selectOption("rv-next");
    await page.getByRole("button", { name: "升级应用", exact: true }).click();
    await page.getByRole("alert").waitFor();
    assert.equal(await target.isDisabled(), true);
    assert.equal(await page.locator("body").getAttribute("data-completions"), null);
    await page.getByRole("button", { name: "继续同一次升级", exact: true }).click();
    await page.getByText("应用升级已完成", { exact: true }).waitFor();
    assert.equal(writes.length, 3);
    assert.ok(writes[0]!.key);
    for (const write of writes) {
      assert.equal(write.key, writes[0]!.key);
      assert.deepEqual(write.body, { applicationSelection: { kind: "opl_app", runtimeVersionId: "rv-next" }, expectedCurrentDeploymentId: "dep-original" });
    }
    assert.equal(await page.locator("body").getAttribute("data-completions"), "1");
    await page.evaluate(() => (window as unknown as { mountUpgrade: (id: string) => void }).mountUpgrade("ws_other"));
    await choose.waitFor();
    assert.equal(await page.getByRole("combobox", { name: "目标 App 版本" }).count(), 0);
    const previousReads = reads.length;
    await choose.click();
    await page.getByRole("button", { name: "升级应用", exact: true }).waitFor();
    assert.equal(await page.getByRole("combobox", { name: "目标 App 版本" }).inputValue(), "");
    assert.ok(reads.length > previousReads);
    assert.equal(await page.getByText("应用升级已完成", { exact: true }).count(), 0);
  } finally { await browser.close(); await server.close(); }
});
