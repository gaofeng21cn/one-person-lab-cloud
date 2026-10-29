import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "vite";
import react from "@vitejs/plugin-react";
import { launchBrowser } from "../../tools/launch-browser.ts";
import { revealWorkspaceApplicationCredentials } from "../../apps/console-ui/src/api/workspace-application-credentials-api.ts";

test("native credential API rejects mismatched identities and undeclared secret fields", async () => {
  const original = globalThis.fetch;
  const valid = { workspaceId: "workspace-one", runtimeInstanceId: "runtime-current", username: "admin", password: "fixture-password" };
  const calls: { path: string; init?: RequestInit }[] = [];
  let reply: unknown = valid;
  globalThis.fetch = async (input, init) => { calls.push({ path: String(input), init }); return Response.json(reply); };
  try {
    assert.deepEqual(await revealWorkspaceApplicationCredentials("workspace-one", "csrf-one"), valid);
    assert.equal(calls[0].path, "/api/v2/workspaces/workspace-one/application-credentials/reveal");
    assert.equal(calls[0].init?.method, "POST");
    assert.equal((calls[0].init?.headers as Record<string, string>)["X-CSRF-Token"], "csrf-one");
    assert.ok((calls[0].init?.headers as Record<string, string>)["Idempotency-Key"]);
    for (reply of [null, { ...valid, workspaceId: "foreign" }, { ...valid, password: "" }, { ...valid, runtimeInstanceId: "" }, { ...valid, gatewayKey: "must-not-return" }, { ...valid, sessionSecret: "must-not-return" }]) {
      await assert.rejects(revealWorkspaceApplicationCredentials("workspace-one", "csrf-one"), /invalid_application_credentials/);
    }
  } finally { globalThis.fetch = original; }
});

test("native credential component reveals explicitly and clears hidden, departed and stale replies", { timeout: 60_000 }, async (t) => {
  const vite = await createServer({ configFile: false, plugins: [react(), {
    name: "credential-component-fixture",
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        if (request.url !== "/__credential_fixture") return next();
        response.setHeader("Content-Type", "text/html");
        response.end(await server.transformIndexHtml("/__credential_fixture", `<!doctype html><html><body><div id="root"></div><script type="module">
          import React from "react";
          import { createRoot } from "react-dom/client";
          import { WorkspaceApplicationCredentialsPanel } from "/apps/console-ui/src/components/workspaces/WorkspaceApplicationCredentialsPanel.tsx";
          const root = createRoot(document.getElementById("root"));
          const session = { user: { id: "owner", accountId: "account", role: "owner" }, isOperator: false, csrfToken: "csrf-one" };
          window.renderCredentials = (workspaceId = "workspace-one", signedIn = true) => root.render(React.createElement(WorkspaceApplicationCredentialsPanel, { workspaceId, session: signedIn ? session : null }));
          window.leaveCredentials = () => root.render(React.createElement("p", null, "Left workspace"));
          window.renderCredentials();
        </script></body></html>`));
      });
    }
  }], optimizeDeps: { include: ["react", "react-dom/client"] }, server: { host: "127.0.0.1", port: 0 } });
  await vite.listen();
  t.after(() => vite.close());
  const browser = await launchBrowser({ headless: true });
  t.after(() => browser.close());
  const page = await browser.newPage();
  page.setDefaultTimeout(10_000);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
  let reveals = 0;
  let hold = false;
  let release: (() => void) | undefined;
  const held = () => new Promise<void>((resolve) => { release = resolve; });
  const managementReads: string[] = [];
  page.on("request", (request) => { if (request.url().includes("/api/v2/")) managementReads.push(request.url()); });
  await page.route("**/api/v2/workspaces/*/application-credentials/reveal", async (route) => {
    reveals++;
    const workspaceId = route.request().url().split("/").at(-3);
    assert.equal(route.request().headers()["x-csrf-token"], "csrf-one");
    if (hold) await held();
    await route.fulfill({ json: { workspaceId, runtimeInstanceId: "runtime-current", username: "declared-admin", password: "ephemeral-fixture-password" } }).catch(() => {});
  });
  const origin = vite.resolvedUrls!.local[0];
  await page.goto(`${origin}__credential_fixture`);
  const reveal = page.getByRole("button", { name: "查看登录凭据", exact: true });
  await reveal.waitFor().catch(() => { throw new Error(`component failed to mount: ${errors.join("; ")}; url=${page.url()}`); });
  assert.equal(reveals, 0);
  assert.deepEqual(managementReads, [], "opening the detail must not read credentials or access");
  await reveal.click();
  await page.getByText("ephemeral-fixture-password", { exact: true }).waitFor();
  await page.getByRole("button", { name: "隐藏登录凭据" }).click();
  assert.equal(await page.getByText("ephemeral-fixture-password", { exact: true }).count(), 0);
  await reveal.click();
  await page.getByText("ephemeral-fixture-password", { exact: true }).waitFor();
  assert.equal(reveals, 2);
  await page.evaluate("window.renderCredentials('workspace-two')");
  await reveal.waitFor();
  assert.equal(await page.getByText("ephemeral-fixture-password", { exact: true }).count(), 0);
  hold = true;
  await reveal.click();
  await page.getByRole("status").waitFor();
  await page.getByRole("button", { name: "隐藏登录凭据" }).click();
  release?.();
  await reveal.waitFor();
  assert.equal(await page.getByText("ephemeral-fixture-password", { exact: true }).count(), 0);
  hold = false;
  await reveal.click();
  await page.getByText("ephemeral-fixture-password", { exact: true }).waitFor();
  assert.deepEqual(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } })), { local: {}, session: {} });
  assert.ok(!page.url().includes("password"));
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent("pagehide")));
  await reveal.waitFor();
  assert.equal(await page.getByText("ephemeral-fixture-password", { exact: true }).count(), 0);
  await page.evaluate("window.leaveCredentials()");
  await page.getByText("Left workspace", { exact: true }).waitFor();
  await page.evaluate("window.renderCredentials()");
  await reveal.waitFor();
  assert.equal(await page.getByText("ephemeral-fixture-password", { exact: true }).count(), 0);
  await page.evaluate("window.renderCredentials('workspace-one', false)");
  await page.getByText("请先登录。", { exact: true }).waitFor();
  assert.deepEqual(errors, []);
});
