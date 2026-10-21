import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";
import type { Page } from "playwright";
import { launchBrowser } from "../../tools/launch-browser.ts";
import { readAgentReply } from "../../webui/cloud-agent/runtime-api.js";

const hash = (value: Uint8Array | string) => "sha256:" + createHash("sha256").update(value).digest("hex");
const prompts = [
  "这是 Agent 运行验收，不是医学问诊。请简要说明你的名称、功能和证据使用边界；如果当前没有指南检索证据，请明确说明，勿生成医学建议或假称已检索。",
  "这是同一会话的连续性验收。请只回复：IBD demo 会话连续性验证成功。"
];

export async function verifyAgentWebuiBrowser(page: Page, options: {
  origin: string;
  credentials: { username: string; password: string };
  packageId: string;
  timeoutMs?: number;
  onStage?: (stage: string) => void;
}) {
  const entry = new URL(options.origin);
  assert.ok((entry.protocol === "http:" || entry.protocol === "https:") && !entry.username && !entry.password &&
    entry.pathname === "/" && !entry.search && !entry.hash, "Supply a direct application origin without credentials, path, query or fragment");
  assert.ok(options.credentials.username && options.credentials.password && options.packageId, "Application credentials and package ID are required");
  const origin = entry.origin;
  const timeout = options.timeoutMs ?? 240_000;
  page.setDefaultTimeout(timeout);
  const pageErrors: string[] = [];
  let blockedCrossOriginRequests = 0;
  let guardErrors = 0;
  // Page.route only handles the first URL of a redirect chain. Chromium Fetch
  // pauses every request, including 307/308 hops, before its body is sent.
  const guard = await page.context().newCDPSession(page);
  guard.on("Fetch.requestPaused", ({ requestId, request }) => {
    const allowed = new URL(request.url).origin === origin;
    if (!allowed) blockedCrossOriginRequests += 1;
    void guard.send(allowed ? "Fetch.continueRequest" : "Fetch.failRequest", allowed
      ? { requestId } : { requestId, errorReason: "BlockedByClient" }).catch(() => { guardErrors += 1; });
  });
  await guard.send("Fetch.enable", { patterns: [{ urlPattern: "*", requestStage: "Request" }] });
  // Browser error contents can contain application data; persist only the count.
  page.on("pageerror", () => pageErrors.push("pageerror"));
  options.onStage?.("native_login");
  await page.goto(origin, { waitUntil: "domcontentloaded" });
  assert.equal(new URL(page.url()).origin, origin, "Never fill credentials into a foreign page");
  await page.locator('input[name="username"]').fill(options.credentials.username);
  await page.locator('input[name="password"]').fill(options.credentials.password);
  const [login] = await Promise.all([
    page.waitForResponse(response => new URL(response.url()).origin === origin &&
      new URL(response.url()).pathname === "/api/auth/login" && response.request().method() === "POST"),
    page.getByRole("button", { name: /sign in|log in|login|登录|登入/i }).click()
  ]);
  assert.equal(login.status(), 200, "Native application login must succeed");
  await page.locator('[data-opl-cloud-agent-webui="v1"]').waitFor();
  await page.getByText("Runtime ready", { exact: true }).waitFor();

  // The Runtime owns the served HTML shell (including its bootstrap injection).
  // Bind that observed page separately; immutable JS/CSS assets must match byte-for-byte.
  options.onStage?.("served_asset_readback");
  const htmlResponse = await page.request.get(origin, { maxRedirects: 0 });
  assert.equal(htmlResponse.status(), 200, "Authenticated application HTML must be readable");
  const html = {
    sourceSha256: hash(await readFile(new URL("../../webui/cloud-agent/index.html", import.meta.url))),
    servedSha256: hash(await htmlResponse.body()), renderedWebuiMarker: true
  };
  const webuiFiles: Record<string, string> = {};
  for (const file of ["app.js", "runtime-api.js", "style.css"]) {
    const expected = await readFile(new URL(`../../webui/cloud-agent/${file}`, import.meta.url));
    const response = await page.request.get(origin + "/" + file, { maxRedirects: 0 });
    assert.equal(response.status(), 200, "Authenticated WebUI asset must be readable");
    const actual = await response.body();
    assert.equal(hash(actual), hash(expected), "Served WebUI must match this checkout's source");
    webuiFiles[file] = hash(actual);
  }

  options.onStage?.("agent_discovery");
  const choices = await page.locator("#agent option").evaluateAll(nodes => nodes.map(node => ({
    id: (node as HTMLOptionElement).value, label: node.textContent
  })));
  const selected = choices.find(choice => JSON.parse(choice.id)[0] === options.packageId);
  assert.ok(selected, "The declared Agent must be discoverable from the actual Runtime");
  await page.getByLabel("Agent", { exact: true }).selectOption(selected.id);
  const turns: { threadId: string; turnId: string; status: string; executor: string; csrfHeaderPresent: boolean;
    continuation: boolean; responseBytes: number; responseSha256: string }[] = [];
  for (const [index, prompt] of prompts.entries()) {
    options.onStage?.(`agent_turn_${index + 1}`);
    await page.getByLabel("Prompt", { exact: true }).fill(prompt);
    const observed = page.waitForResponse(response => new URL(response.url()).origin === origin &&
      new URL(response.url()).pathname === "/api/send-message" && response.request().method() === "POST");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const response = await observed;
    assert.equal(response.status(), 200, "The native message request must succeed");
    const value = await response.json();
    const reply = readAgentReply(value, index ? turns[0].threadId : undefined);
    const request = response.request();
    const input = request.postDataJSON();
    assert.ok(request.headers()["x-csrf-token"], "The UI must send the native CSRF header");
    if (index === 0) assert.equal(input.agentSelection?.package_id, options.packageId);
    else {
      assert.equal(input.threadId, turns[0].threadId, "Continuation must use the original thread");
      assert.equal(input.agentSelection, undefined, "Continuation must not reselect the Agent");
    }
    await page.waitForFunction(() => !(document.querySelector("#send") as HTMLButtonElement).disabled);
    assert.equal(await page.locator(".message.assistant").nth(index).textContent(), reply.answer, "UI reply must equal the actual canonical reply");
    turns.push({ threadId: reply.threadId, turnId: value.turnId, status: "completed", executor: "codex_app_server",
      csrfHeaderPresent: true, continuation: index > 0, responseBytes: Buffer.byteLength(reply.answer), responseSha256: hash(reply.answer) });
  }
  options.onStage?.("final_browser_checks");
  assert.equal(await page.locator(".message.error").count(), 0, "The UI must not display an Agent error");
  assert.equal(pageErrors.length, 0, "The browser must not report an unhandled page error");
  assert.equal(blockedCrossOriginRequests, 0, "Verification must stay on the supplied application origin");
  assert.equal(guardErrors, 0, "The same-origin request guard must remain active");
  return { schemaVersion: 1, scope: "independent_webui_native_login_and_real_agent_conversation", accepted: true,
    timestamp: new Date().toISOString(), packageId: options.packageId, nativeLoginHttpStatus: 200, html, webuiFiles, turns,
    browserPageErrors: pageErrors.length, blockedCrossOriginRequests, originGuard: "chromium_fetch_request_stage", networkMocks: false,
    limits: ["Caller must independently verify the application's immutable image", "Source-mounted WebUI is not a rebuilt application OCI",
      "Does not execute Package upload, Build, Workspace, Serve/Fabric orchestration, Tencent operations, business deletion or refund"] };
}

async function main() {
  const { values } = parseArgs({ options: {
    origin: { type: "string" }, "credentials-file": { type: "string" }, "package-id": { type: "string" }, output: { type: "string" }
  } });
  assert.ok(values.origin && values["credentials-file"] && values["package-id"] && values.output,
    "Usage: node tests/live/cloud-agent-webui-browser.ts --origin <application-origin> --credentials-file <private-json-file> --package-id <id> --output <new-directory>");
  const credentials = JSON.parse(await readFile(values["credentials-file"], "utf8"));
  assert.ok(typeof credentials.username === "string" && typeof credentials.password === "string", "Credentials file must supply username/password strings");
  const output = resolve(values.output);
  await mkdir(output, { mode: 0o700 }); // One action, one new directory; never overwrite an earlier receipt.
  const browser = await launchBrowser({ headless: true });
  let stage = "browser_started";
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, serviceWorkers: "block" });
    const receipt = await verifyAgentWebuiBrowser(page, { origin: values.origin, credentials, packageId: values["package-id"], onStage: value => { stage = value; } });
    await page.screenshot({ path: resolve(output, "desktop.png"), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Mobile view must not overflow horizontally");
    await page.screenshot({ path: resolve(output, "mobile.png"), fullPage: true });
    await writeFile(resolve(output, "browser.receipt.json"), JSON.stringify(receipt, null, 2) + "\n", { flag: "wx", mode: 0o600 });
    console.log(JSON.stringify({ accepted: true, completedTurns: receipt.turns.length, output }));
  } catch (error) {
    const failure = { accepted: false, timestamp: new Date().toISOString(), stage,
      errorKind: error instanceof Error ? error.name : "UnknownError" };
    await writeFile(resolve(output, "failure.json"), JSON.stringify(failure, null, 2) + "\n", { flag: "wx", mode: 0o600 });
    console.error(JSON.stringify(failure));
    throw error;
  } finally { await browser.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch(() => { console.error("Live WebUI verification failed; no successful receipt was written."); process.exitCode = 1; });
}
