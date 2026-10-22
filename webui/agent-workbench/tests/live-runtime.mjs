import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { parseArgs } from "node:util";
import { createHash } from "node:crypto";
import { chromium } from "playwright";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import { readAgentReply } from "../state/workbench/remoteModelAdapter.ts";
const { values } = parseArgs({ options: { origin: { type: "string" }, "credentials-file": { type: "string" }, output: { type: "string" } } });
assert.ok(values.origin && values["credentials-file"] && values.output);
const origin = new URL(values.origin).origin;
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(new URL(origin).hostname), "Live replay accepts an authorized loopback forwarding origin only");
const credentials = JSON.parse(await readFile(values["credentials-file"], "utf8"));
const out = resolve(values.output); await mkdir(out, { mode: 0o700 });
const hash = bytes => "sha256:" + createHash("sha256").update(bytes).digest("hex");
const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, serviceWorkers: "block" });
const cdp = await page.context().newCDPSession(page);
let blocked = 0; const errors = []; page.on("pageerror", error => errors.push(error.message));
await cdp.send("Fetch.enable", { patterns: [{ urlPattern: "*", requestStage: "Request" }] });
cdp.on("Fetch.requestPaused", async event => {
  const allowed = new URL(event.request.url).origin === origin;
  if (!allowed) blocked += 1;
  try { await cdp.send(allowed ? "Fetch.continueRequest" : "Fetch.failRequest", allowed ? { requestId: event.requestId } : { requestId: event.requestId, errorReason: "BlockedByClient" }); } catch { /* context may already be closed */ }
});
const prompts = [
  "这是界面运行验收，不是医学问诊。请简要说明你的名称和功能；不要给出医学建议，不要声称已检索证据。",
  "这是同一会话的连续性验收。请只回复：原三栏 WebUI 连续对话验证成功。"
];
const turns = []; const assets = {};
try {
  await page.goto(origin, { waitUntil: "domcontentloaded" });
  assert.equal(new URL(page.url()).origin, origin);
  const form = page.locator("form").filter({ has: page.locator('input[name="password"]') });
  const action = await form.getAttribute("action");
  assert.equal(new URL(action || page.url(), page.url()).origin, origin);
  await page.locator('input[name="username"]').fill(credentials.username);
  await page.locator('input[name="password"]').fill(credentials.password);
  const login = page.waitForResponse(response => new URL(response.url()).pathname === "/api/auth/login" && response.request().method() === "POST");
  await form.locator('button[type="submit"]').click();
  assert.equal((await login).status(), 200);
  await page.locator("textarea.composer-input").waitFor({ state: "visible" });
  for (const relative of ["webui-config.json", ...await page.locator('script[src],link[rel="stylesheet"]').evaluateAll(elements => elements.map(element => element.getAttribute("src") || element.getAttribute("href")).filter(Boolean))]) {
    const url = new URL(relative, origin); assert.equal(url.origin, origin);
    const response = await page.request.get(url.href, { maxRedirects: 0 }); assert.equal(response.status(), 200);
    assets[url.pathname] = hash(await response.body());
  }
  let threadId;
  for (const [index, prompt] of prompts.entries()) {
    await page.locator("textarea.composer-input").fill(prompt);
    const completed = page.waitForResponse(response => new URL(response.url()).pathname === "/api/send-message" && response.request().method() === "POST", { timeout: 240000 });
    await page.getByRole("button", { name: "发送", exact: true }).click();
    const response = await completed; assert.equal(response.status(), 200);
    const request = response.request(); assert.ok(request.headers()["x-csrf-token"]);
    const reply = readAgentReply(await response.json(), threadId); threadId = reply.threadId;
    const answer = reply.result.reply;
    const html = renderToStaticMarkup(React.createElement(ReactMarkdown, null, answer));
    const expected = await page.evaluate(html => new DOMParser().parseFromString(html, "text/html").body.textContent, html);
    await page.waitForFunction(expected => document.querySelectorAll(".assistant-row .assistant-message-body").length > 0 && Array.from(document.querySelectorAll(".assistant-row .assistant-message-body")).at(-1).textContent === expected, expected, { timeout: 10000 });
    turns.push({ threadId, turnId: reply.runId, status: reply.status, executor: "codex_app_server", continuation: index > 0, csrfHeaderPresent: true, responseBytes: Buffer.byteLength(answer), responseSha256: hash(answer), renderedMarkdownTextMatches: true });
  }
  assert.deepEqual(errors, []); assert.equal(blocked, 0);
  await page.screenshot({ path: resolve(out, "desktop.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.screenshot({ path: resolve(out, "mobile.png"), fullPage: true });
  const receipt = { schemaVersion: 1, scope: "extracted_original_three_panel_webui_real_runtime", accepted: true, timestamp: new Date().toISOString(), nativeLoginHttpStatus: 200, turns, servedAssetHashes: assets, pageErrors: errors.length, blockedCrossOriginRequests: blocked, networkMocks: false, limits: ["Independent WebUI mounted from the OCI-extracted static content", "Not a rebuilt final Agent OCI, Workspace-owner or Tencent acceptance", "Native Runtime exposes no structured reference projection; citations remain empty"] };
  await writeFile(resolve(out, "browser.receipt.json"), JSON.stringify(receipt, null, 2) + "\n", { flag: "wx" });
  console.log(JSON.stringify({ accepted: true, completedTurns: turns.length, output: out }));
} catch (error) {
  await page.screenshot({ path: resolve(out, "failure.png"), fullPage: true }).catch(() => {});
  await writeFile(resolve(out, "failure.json"), JSON.stringify({ accepted: false, message: error instanceof Error ? error.message : String(error), pageErrors: errors, blockedCrossOriginRequests: blocked }, null, 2) + "\n", { flag: "wx" });
  throw error;
} finally { await browser.close(); }
