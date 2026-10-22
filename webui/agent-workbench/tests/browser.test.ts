import test from "node:test";
import assert from "node:assert/strict";
import { createServer, type ServerResponse } from "node:http";
import { readFile, mkdir } from "node:fs/promises";
import { resolve, extname, sep } from "node:path";
import { chromium } from "playwright";
import type { AgentSelection } from "../state/workbench/remoteModelAdapter.ts";

test("original three-panel UI supports different Agents, native continuity, history and mobile drawers", { timeout: 60_000 }, async () => {
  const posts: { path: string; body: Record<string, any> }[] = [];
  const pending: { response: ServerResponse; body: Record<string, any> }[] = [];
  const threads: { id: string; summary: string; turns: any[] }[] = [];
  const streams = new Set<ServerResponse>();
  const heldReads: { response: ServerResponse; value: unknown }[] = [];
  let holdNextRead = false;
  const csrf = "fixture-session-csrf";
  const selections: AgentSelection[] = ["writing-agent", "research-agent"].map(id => ({ package_id: id, shortcut_id: "ask", codex_visible_entry: id, required_skill_ids: [id] }));
  const catalog = { app_state: { version: "g2", app_state: { schema_version: "opl_app_state.v1", core: { codex: { model_access_ready: true } }, agent_packages: { directory: { entries: selections.map((selection, i) => ({ package_id: selection.package_id, display_name: i === 0 ? "写作助手" : "研究助手", package_role: "standard_agent", readiness: { operational_ready: true }, capability_metadata: { required_skill_ids: selection.required_skill_ids }, home_shortcuts: [{ shortcut_id: "ask", route: { route_kind: "agent_package_shortcut", executor: "codex_cli", codex_visible_entry: selection.codex_visible_entry } }] })) } } } } };
  const dist = resolve("dist");
  const server = createServer(async (request, response) => {
    const path = new URL(request.url!, "http://fixture").pathname;
    const json = (body: unknown, code = 200) => { response.writeHead(code, { "content-type": "application/json" }); response.end(JSON.stringify(body)); };
    if (path === "/api/auth/user") return json({ authenticated: true, username: "fixture", csrfToken: csrf });
    if (path === "/api/opl/state") return json(catalog);
    if (path === "/api/opl-events") { response.writeHead(200, { "content-type": "text/event-stream" }); response.write(": fixture\n\n"); streams.add(response); response.on("close", () => streams.delete(response)); return; }
    if (request.method === "POST") {
      let raw = ""; for await (const chunk of request) raw += chunk;
      const body = JSON.parse(raw); posts.push({ path, body });
      if (request.headers["x-csrf-token"] !== csrf) return json({ error: { message: "CSRF header missing" } }, 403);
      if (path === "/api/threads/list") return json({ data: threads, nextCursor: null });
      if (path === "/api/threads/read") {
        const value = structuredClone(threads.find(item => item.id === body.threadId));
        if (holdNextRead) { holdNextRead = false; heldReads.push({ response, value }); return; }
        return json(value);
      }
      if (path === "/api/threads/rename") { const thread = threads.find(item => item.id === body.threadId)!; thread.summary = body.name; return json(thread); }
      if (path === "/api/threads/delete") { const i = threads.findIndex(item => item.id === body.threadId); threads.splice(i, 1); return json({ threadId: body.threadId, deleted: body.confirmed === true }); }
      if (path === "/api/send-message") { pending.push({ response, body }); return; }
      return json({ error: { message: "unexpected fixture POST" } }, 404);
    }
    try {
      if (path === "/webui-config.json") return json({ schemaVersion: 1, runtimeProtocol: "opl-native-g2", sidebarTitle: "通用工作台", assistantTitle: null, notice: null });
      const file = resolve(dist, path === "/" ? "index.html" : `.${path}`);
      if (file !== resolve(dist, "index.html") && !file.startsWith(dist + sep)) return json({}, 404);
      const data = await readFile(file);
      response.writeHead(200, { "content-type": ({ ".html": "text/html", ".js": "text/javascript", ".css": "text/css" } as Record<string, string>)[extname(file)] || "application/octet-stream" }); response.end(data);
    } catch { json({}, 404); }
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  const address = server.address(); assert.ok(address && typeof address !== "string");
  const browser = await chromium.launch({ channel: process.env.WEBUI_BROWSER_CHANNEL ?? "chrome", headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } }); page.setDefaultTimeout(5000);
  const errors: string[] = []; page.on("pageerror", error => errors.push(error.message));
  const awaitMessages = async (count: number) => {
    const deadline = Date.now() + 5000;
    while (pending.length < count && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20));
    assert.equal(pending.length, count, "native message must arrive within the stage deadline");
  };
  const awaitCondition = async (condition: () => boolean, stage: string) => {
    const deadline = Date.now() + 5000;
    while (!condition() && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20));
    assert.ok(condition(), stage);
  };
  const releaseRead = (index: number) => {
    const { response, value } = heldReads[index]; response.writeHead(200, { "content-type": "application/json" }); response.end(JSON.stringify(value));
  };
  const complete = (index: number, answer: string) => {
    const { response, body } = pending[index];
    const id = body.threadId ?? `thread-${threads.length + 1}`;
    let thread = threads.find(item => item.id === id);
    if (!thread) { thread = { id, summary: body.prompt, turns: [] }; threads.unshift(thread); }
    const turnId = `turn-${index + 1}`;
    thread.turns.push({ id: turnId, status: "completed", items: [{ id: `user-${index}`, type: "userMessage", content: [{ type: "text", text: body.prompt }] }, { id: `agent-${index}`, type: "agentMessage", phase: "final_answer", text: answer }] });
    response.writeHead(200, { "content-type": "application/json" }); response.end(JSON.stringify({ executor: "codex_app_server", threadId: id, turnId, finalMessage: answer, canonicalThread: thread }));
  };
  try {
    await page.goto(`http://127.0.0.1:${address.port}`);
    await page.getByRole("button", { name: "配置 Agent", exact: true }).click();
    await page.getByLabel("选择 Agent", { exact: true }).selectOption(JSON.stringify(["writing-agent", "ask"]));
    await page.getByLabel("助手名称", { exact: true }).fill("写作工作台");
    await page.getByRole("button", { name: "应用配置", exact: true }).click();
    await page.locator(".chat-header").getByText("写作工作台", { exact: true }).waitFor();
    const input = page.locator("textarea.composer-input");
    await input.fill("帮我整理一段介绍");
    await page.getByRole("button", { name: "发送", exact: true }).click(); await awaitMessages(1);
    assert.deepEqual(pending[0].body.agentSelection, selections[0]);
    assert.equal(await input.isDisabled(), true);
    assert.equal(await page.getByRole("button", { name: "停止生成", exact: true }).count(), 0);
    complete(0, "这是**写作助手**的回答。");
    await page.locator(".assistant-message-body").getByText("写作助手", { exact: true }).waitFor();
    await awaitCondition(() => streams.size > 0, "native SSE subscription established");
    holdNextRead = true;
    for (const stream of streams) stream.write(`data: ${JSON.stringify({ method: "turn/completed", params: { threadId: "thread-1" } })}\n\n`);
    await awaitCondition(() => heldReads.length === 1, "pre-send readback captured");
    await input.fill("继续整理"); await page.getByRole("button", { name: "发送", exact: true }).click(); await awaitMessages(2);
    assert.equal(pending[1].body.threadId, "thread-1"); assert.equal(pending[1].body.agentSelection, undefined);
    releaseRead(0);
    complete(1, "同一会话继续完成。"); await page.getByText("同一会话继续完成。", { exact: true }).waitFor();
    assert.equal(await page.getByText("指南信息不能替代医生诊断或个体化治疗决策。", { exact: true }).count(), 0);
    assert.equal(await page.locator(".evidence-document").count(), 0, "plain model text must not manufacture citations");
    await page.getByRole("button", { name: "配置 Agent", exact: true }).click();
    await page.getByLabel("选择 Agent", { exact: true }).selectOption(JSON.stringify(["research-agent", "ask"]));
    await page.getByLabel("助手名称", { exact: true }).fill("研究工作台"); await page.getByRole("button", { name: "应用配置", exact: true }).click();
    await page.locator(".chat-header").getByText("研究工作台", { exact: true }).waitFor();
    await input.fill("研究任务"); await page.getByRole("button", { name: "发送", exact: true }).click(); await awaitMessages(3);
    assert.deepEqual(pending[2].body.agentSelection, selections[1]); assert.equal(pending[2].body.threadId, undefined);
    complete(2, "研究助手已接收任务。"); await page.getByText("研究助手已接收任务。", { exact: true }).waitFor();
    await page.getByRole("button", { name: "新对话", exact: true }).click();
    holdNextRead = true;
    await page.locator(".aui-thread-trigger").filter({ hasText: "帮我整理一段介绍" }).click();
    await awaitCondition(() => heldReads.length === 2, "switch readback captured");
    await page.getByRole("button", { name: "配置 Agent", exact: true }).click();
    await page.getByRole("button", { name: "应用配置", exact: true }).click();
    releaseRead(1);
    await input.waitFor({ state: "visible" });
    await page.waitForFunction(() => !document.querySelector<HTMLTextAreaElement>("textarea.composer-input")?.disabled);
    await page.locator(".aui-thread-trigger").filter({ hasText: "帮我整理一段介绍" }).click();
    await page.getByText("同一会话继续完成。", { exact: true }).waitFor();
    await page.reload(); await page.getByText("同一会话继续完成。", { exact: true }).waitFor();
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.getByRole("button", { name: "打开会话列表", exact: true }).click();
    await page.getByRole("complementary", { name: "会话列表", exact: true }).waitFor({ state: "visible" });
    await page.getByRole("button", { name: "关闭会话列表", exact: true }).click();
    await page.getByRole("button", { name: "打开提示与证据", exact: true }).click();
    await page.getByRole("complementary", { name: "提示与证据", exact: true }).waitFor({ state: "visible" });
    await page.getByRole("button", { name: "关闭证据栏", exact: true }).click();
    assert.deepEqual(errors, []);
    if (process.env.WEBUI_EVIDENCE_DIR) {
      await mkdir(process.env.WEBUI_EVIDENCE_DIR, { recursive: true });
      await page.screenshot({ path: resolve(process.env.WEBUI_EVIDENCE_DIR, "generic-mobile.png"), fullPage: true });
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.screenshot({ path: resolve(process.env.WEBUI_EVIDENCE_DIR, "generic-desktop.png"), fullPage: true });
    }
  } catch (error) {
    console.error(JSON.stringify({ phase: "browser-fixture", pageErrors: errors, body: await page.locator("body").innerText(), posts }, null, 2));
    throw error;
  } finally {
    await browser.close(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
