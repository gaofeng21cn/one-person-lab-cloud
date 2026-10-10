import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createServer, type ServerResponse } from "node:http";
import test from "node:test";
import { launchBrowser } from "../../tools/launch-browser.ts";
import { CONSOLE_DEMO_CREDENTIALS, startConsoleDemoServer } from "../../tools/start-console-demo.ts";

const digest = "sha256:" + "a".repeat(64);

test("Cloud Agent keeps the current turn locked until the Runtime completes it", { timeout: 30_000 }, async () => {
  let events: ServerResponse | undefined;
  const actions: unknown[] = [];
  const files = new Map(await Promise.all([
    ["/", "index.html", "text/html"],
    ["/app.js", "app.js", "text/javascript"],
    ["/style.css", "style.css", "text/css"]
  ].map(async ([path, file, type]) => [path, { type, body: await readFile(new URL(`../../webui/cloud-agent/${file}`, import.meta.url)) }] as const)));
  const server = createServer(async (request, response) => {
    const reply = (body: unknown) => {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(body));
    };
    if (request.url === "/api/auth/user") return reply({ csrfToken: "local-csrf" });
    if (request.url === "/api/opl/state?profile=fast") return reply({ app_state: { agent_packages: { directory: { entries: [{
      package_id: "local-agent", display_name: "Local Agent", package_role: "standard_agent",
      readiness: { operational_ready: true },
      available_actions: [{ action_id: "invoke_agent_package_for_current_turn", payload: { package_id: "local-agent" } }]
    }] } } } });
    if (request.url === "/api/opl-events") {
      events = response;
      response.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" });
      response.flushHeaders();
      return;
    }
    if (request.url === "/api/opl/action") {
      let body = "";
      for await (const chunk of request) body += chunk;
      actions.push(JSON.parse(body));
      return reply({ status: "executed" });
    }
    const file = files.get(request.url || "");
    response.writeHead(file ? 200 : 404, { "content-type": file?.type || "text/plain" });
    response.end(file?.body);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address && typeof address !== "string");
  const browser = await launchBrowser({ headless: true });
  try {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${address.port}/`);
    await page.getByText("Runtime connected", { exact: true }).waitFor();
    const send = page.getByRole("button", { name: "Send", exact: true });
    const prompt = page.getByLabel("Prompt", { exact: true });
    await prompt.fill("First task");
    await Promise.all([page.waitForResponse("**/api/opl/action"), send.click()]);
    // Older code displayed Stop despite having no owner cancellation API.
    // Clicking it locally unlocked a second request while the first kept running.
    const stop = page.getByRole("button", { name: "Stop", exact: true });
    if (await stop.count()) await stop.click();
    assert.equal(await send.isDisabled(), true, "Only Runtime completion may unlock a turn");
    assert.equal(await prompt.isDisabled(), true);
    await page.locator("#composer").dispatchEvent("submit");
    events?.write(`data: ${JSON.stringify({ method: "item/agentMessage/delta", params: { delta: "First answer" } })}\n\n`);
    await page.getByText("First answer", { exact: true }).waitFor();
    assert.equal(actions.length, 1, "No overlapping Agent action is submitted");
    events?.write(`data: ${JSON.stringify({ method: "turn/completed", params: {} })}\n\n`);
    await page.waitForFunction(() => !(document.querySelector("#send") as HTMLButtonElement).disabled);
    await prompt.fill("Second task");
    await Promise.all([page.waitForResponse("**/api/opl/action"), send.click()]);
    events?.write(`data: ${JSON.stringify({ method: "item/agentMessage/delta", params: { delta: "Second answer" } })}\n\n`);
    await page.getByText("Second answer", { exact: true }).waitFor();
    assert.deepEqual(await page.locator(".message.assistant").allTextContents(), ["First answer", "Second answer"]);
    assert.equal(actions.length, 2);
  } finally {
    await browser.close();
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
});

test("Publisher presents one customer task with the owner facts behind technical details", { timeout: 30_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  let policyActive = true;
  // Publisher commands only: an unavailable platform default must not reach
  // upload or build, and the page must never substitute a catalog row.
  const commands: string[] = [];
  const stateChanging = (method: string, path: string) => method !== "GET" && /^\/api\/v2\/(builds|uploads|packages|namespaces|capability-versions)/.test(path);
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await page.route("**/api/v2/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
      if (stateChanging(request.method(), url.pathname)) commands.push(`${request.method()} ${url.pathname}`);
      if (url.pathname === "/api/v2/auth/context") return json({ csrfToken: "csrf-fixture" });
      if (url.pathname === "/api/v2/auth/login") return json({ actorId: "user-customer", tenantId: "tenant-fixture", displayName: "Customer", permissions: [], csrfToken: "csrf-fixture", expiresAt: "2099-01-01T00:00:00Z" });
      if (url.pathname === "/api/v2/auth/session") return json({ actorId: "user-customer", tenantId: "tenant-fixture", displayName: "Customer", permissions: [], csrfToken: "csrf-fixture", expiresAt: "2099-01-01T00:00:00Z" });
      if (url.pathname === "/api/v2/namespaces") return json({ items: [{ id: "ns-1", name: "fixture", status: "active" }] });
      if (url.pathname === "/api/v2/packages") return json({ items: [{ id: "pkg-1", name: "fixture-agent", status: "ready" }] });
      // Runtime Control projects the one Runtime its effective policy names onto
      // this member-readable catalog. The policy default here is deliberately the
      // second approved row, so a page that picks the first catalog entry fails.
      if (url.pathname === "/api/v2/catalog/runtime-versions") return json({ items: [
        { id: "runtime-first", name: "First catalog row", versionLabel: "0.0.1", status: "approved", artifactDigest: "sha256:" + "b".repeat(64) },
        { id: "runtime-default", name: "OPL App Runtime default", versionLabel: "26.9.26", status: "approved", artifactDigest: digest, defaultForNewBuilds: policyActive },
        { id: "runtime-latest", name: "mutable", versionLabel: "latest", status: "approved", artifactDigest: "ghcr.io/example/runtime:latest" },
        { id: "runtime-revoked", name: "revoked", versionLabel: "0.0.1", status: "revoked", artifactDigest: digest, defaultForNewBuilds: false }
      ] });
      return json({ error: "unexpected_cloud_webui_browser_request", path: url.pathname }, 500);
    });
    await page.goto(`${server.origin}/login`, { waitUntil: "networkidle" });
    await page.getByLabel("邮箱").fill(CONSOLE_DEMO_CREDENTIALS.customer.email);
    await page.getByLabel("密码").fill(CONSOLE_DEMO_CREDENTIALS.customer.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.goto(`${server.origin}/console/publisher`, { waitUntil: "networkidle" });

    // The page is one customer task: none of the development vocabulary is on the
    // customer-visible surface, while the collapsed disclosure may carry the real
    // owner facts in technical wording.
    const pageRegion = page.locator(".publisher-page");
    await pageRegion.getByRole("heading", { name: "上传智能体", exact: true }).waitFor({ state: "visible" });
    for (const jargon of ["Draft / fixture-aware", "Fail-closed boundary", "owner readback", "Runtime Control 生效策略"]) {
      const matches = page.getByText(jargon, { exact: false });
      let visible = 0;
      for (let index = 0; index < await matches.count(); index += 1) if (await matches.nth(index).isVisible()) visible += 1;
      assert.equal(visible, 0, `${jargon} must not be customer-visible`);
    }

    // The customer supplies Package/name/version only: the platform resolves the
    // Runtime and WebUI, and no picker is offered.
    assert.equal(await page.getByLabel("Runtime Release").count(), 0);
    assert.equal(await page.getByLabel("WebUI").count(), 0);

    // The installed readback belongs behind one labelled disclosure, closed first.
    const details = pageRegion.locator("details.publisher-technical-details");
    assert.equal(await details.count(), 1, "the page must expose exactly one technical disclosure");
    assert.equal(await details.evaluate((element: HTMLDetailsElement) => element.open), false, "technical details start closed");
    assert.equal(await details.getByText("Runtime Release", { exact: false }).isVisible(), false);

    // The unavailable owner fact is stated as one actionable customer sentence,
    // never together with a contradictory loading message.
    const blocked = pageRegion.getByRole("status").filter({ hasText: "暂时无法开始构建" });
    await blocked.first().waitFor({ state: "visible" });
    assert.equal(await page.getByText("正在读取平台默认输入…", { exact: true }).count(), 0, "loading and unavailable states must not coexist");

    const submit = page.getByRole("button", { name: "上传并构建 / 继续上传" });
    assert.equal(await submit.isDisabled(), true);
    assert.equal(commands.length, 0);

    // The fixed tab bar must render every task on one row: a wrapped bar grows
    // past the space the shell reserves and covers the page's last actions.
    const navigationLinks = page.locator(".mobile-bottom-nav a");
    assert.equal(await navigationLinks.count(), 5, "the cloud customer surface exposes five mobile tasks");
    const navigationRows = await navigationLinks.evaluateAll((links) =>
      [...new Set(links.map((link) => Math.round(link.getBoundingClientRect().y)))]);
    assert.equal(navigationRows.length, 1, `mobile navigation must render one row: ${JSON.stringify(navigationRows)}`);

    // Expanding the disclosure reveals the real owner facts, including that the
    // effective default WebUI half has no member-authorized read yet.
    const summary = details.locator("summary");
    await summary.click();
    await details.getByText("Runtime Release", { exact: false }).waitFor({ state: "visible" });
    const facts = (await details.textContent()) || "";
    assert.match(facts, /OPL App Runtime default/);
    assert.doesNotMatch(facts, /First catalog row/);

    // The primary action stays reachable above mobile navigation and focusable by keyboard.
    const navigation = page.locator(".mobile-bottom-nav");
    await submit.scrollIntoViewIfNeeded();
    const [submitBox, navigationBox] = await Promise.all([submit.boundingBox(), navigation.boundingBox()]);
    assert.ok(submitBox && navigationBox);
    assert.ok(submitBox.height >= 44, `primary action must be a usable touch target: ${JSON.stringify(submitBox)}`);
    assert.ok(submitBox.y + submitBox.height <= navigationBox.y + 1, `primary action is obscured by navigation: ${JSON.stringify({ submitBox, navigationBox })}`);
    await summary.scrollIntoViewIfNeeded();
    const [summaryBox, navigationBoxAtSummary] = await Promise.all([summary.boundingBox(), navigation.boundingBox()]);
    assert.ok(summaryBox && navigationBoxAtSummary);
    assert.ok(summaryBox.y + summaryBox.height <= navigationBoxAtSummary.y + 1, `technical details are obscured by navigation: ${JSON.stringify({ summaryBox, navigationBoxAtSummary })}`);
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth));
    assert.equal(await page.evaluate(() => localStorage.length), 0);
    assert.equal(await page.evaluate(() => JSON.stringify(sessionStorage)), "{}");

    // An inactive default policy is still reported as the same customer state.
    policyActive = false;
    await page.reload({ waitUntil: "networkidle" });
    await pageRegion.getByRole("status").filter({ hasText: "暂时无法开始构建" }).first().waitFor({ state: "visible" });
    assert.equal(await submit.isDisabled(), true);
    assert.equal(commands.length, 0);
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
