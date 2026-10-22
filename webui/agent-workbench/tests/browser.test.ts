import test, { type TestContext } from "node:test";
import assert from "node:assert/strict";
import { createServer, type ServerResponse } from "node:http";
import { readFile, mkdir } from "node:fs/promises";
import { resolve, extname, sep } from "node:path";
import { chromium } from "playwright";
import type { AgentSelection } from "../state/workbench/remoteModelAdapter.ts";

async function workbench(t: TestContext) {
  const posts: { path: string; body: Record<string, any> }[] = [];
  const pending: { response: ServerResponse; body: Record<string, any> }[] = [];
  const threads: { id: string; summary: string; turns: any[] }[] = [];
  const streams = new Set<ServerResponse>();
  const heldReads: { response: ServerResponse; value: unknown }[] = [];
  let holdNextRead = false;
  let failNextRead = false;
  const imageRequests: string[] = [];
  let imageObserved!: () => void;
  const imageRequest = new Promise<void>(resolve => { imageObserved = resolve; });
  const csrf = "fixture-session-csrf";
  const selections: AgentSelection[] = ["writing-agent", "research-agent"].map(id => ({ package_id: id, shortcut_id: "ask", codex_visible_entry: id, required_skill_ids: [id] }));
  const catalog = { app_state: { version: "g2", app_state: { schema_version: "opl_app_state.v1", core: { codex: { model_access_ready: true } }, agent_packages: { directory: { entries: selections.map((selection, i) => ({ package_id: selection.package_id, display_name: i === 0 ? "写作助手" : "研究助手", package_role: "standard_agent", readiness: { operational_ready: true }, capability_metadata: { required_skill_ids: selection.required_skill_ids }, home_shortcuts: [{ shortcut_id: "ask", route: { route_kind: "agent_package_shortcut", executor: "codex_cli", codex_visible_entry: selection.codex_visible_entry } }] })) } } } } };
  const dist = resolve("dist");
  const server = createServer(async (request, response) => {
    const path = new URL(request.url!, "http://fixture").pathname;
    const json = (body: unknown, code = 200) => { response.writeHead(code, { "content-type": "application/json" }); response.end(JSON.stringify(body)); };
    if (path === "/markdown-image.png") { imageRequests.push(request.url!); imageObserved(); response.writeHead(200, { "content-type": "image/png" }); response.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl2lRAAAAAASUVORK5CYII=", "base64")); return; }
    if (path === "/api/auth/user") return json({ authenticated: true, username: "fixture", csrfToken: csrf });
    if (path === "/api/opl/state") return json(catalog);
    if (path === "/api/opl-events") { response.writeHead(200, { "content-type": "text/event-stream" }); response.write(": fixture\n\n"); streams.add(response); response.on("close", () => streams.delete(response)); return; }
    if (request.method === "POST") {
      let raw = ""; for await (const chunk of request) raw += chunk;
      const body = JSON.parse(raw); posts.push({ path, body });
      if (request.headers["x-csrf-token"] !== csrf) return json({ error: { message: "CSRF header missing" } }, 403);
      if (path === "/api/threads/list") return json({ data: threads, nextCursor: null });
      if (path === "/api/threads/read") {
        if (failNextRead) { failNextRead = false; return json({ error: "temporary read failure" }, 503); }
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
  t.after(async () => {
    for (const stream of streams) stream.destroy();
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  });
  const address = server.address(); assert.ok(address && typeof address !== "string");
  const browser = await chromium.launch({ channel: process.env.WEBUI_BROWSER_CHANNEL ?? "chrome", headless: true });
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } }); page.setDefaultTimeout(5000);
  await page.addInitScript(() => {
    (window as any).__workbenchEvents = { handled: 0, opened: 0, reads: 0, reading: 0 };
    const NativeEventSource = EventSource;
    class ObservedEventSource extends NativeEventSource {
      constructor(url: string | URL, init?: EventSourceInit) {
        super(url, init);
        (window as any).__workbenchEvents.active = this;
        this.addEventListener("open", () => { (window as any).__workbenchEvents.opened += 1; });
      }
    }
    const message = Object.getOwnPropertyDescriptor(NativeEventSource.prototype, "onmessage")!;
    Object.defineProperty(ObservedEventSource.prototype, "onmessage", {
      get: message.get,
      set(handler) { message.set!.call(this, function (this: EventSource, event: MessageEvent) {
        try { handler?.call(this, event); }
        finally { (window as any).__workbenchEvents.handled += 1; }
      }); },
    });
    window.EventSource = ObservedEventSource;
    const nativeFetch = window.fetch;
    window.fetch = async (...args) => {
      const reading = String(args[0]).includes("/api/threads/read");
      if (reading) (window as any).__workbenchEvents.reading += 1;
      try {
        const response = await nativeFetch(...args);
        if (reading) await response.clone().arrayBuffer();
        return response;
      } finally {
        if (reading) { (window as any).__workbenchEvents.reading -= 1; (window as any).__workbenchEvents.reads += 1; }
      }
    };
  });
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
  const complete = (index: number, answer: string, options: { status?: string; httpStatus?: number } = {}) => {
    const { response, body } = pending[index];
    const id = body.threadId ?? `thread-${threads.length + 1}`;
    let thread = threads.find(item => item.id === id);
    if (!thread) { thread = { id, summary: body.prompt, turns: [] }; threads.unshift(thread); }
    const turnId = `turn-${index + 1}`;
    thread.turns.push({ id: turnId, status: options.status ?? "completed", items: [{ id: `user-${index}`, type: "userMessage", content: [{ type: "text", text: body.prompt }] }, { id: `agent-${index}`, type: "agentMessage", phase: "final_answer", text: answer }] });
    if (options.httpStatus) { response.writeHead(options.httpStatus, { "content-type": "application/json" }); response.end(JSON.stringify({ error: "response lost after canonical commit" })); return; }
    response.writeHead(200, { "content-type": "application/json" }); response.end(JSON.stringify({ executor: "codex_app_server", threadId: id, turnId, finalMessage: answer, canonicalThread: thread }));
  };
  const origin = `http://127.0.0.1:${address.port}`;
  const emit = () => { for (const stream of streams) stream.write(`data: ${JSON.stringify({ method: "turn/completed", params: { threadId: "thread-1" } })}\n\n`); };
  const settleSubscription = () => page.waitForFunction(() => {
    const observed = (window as any).__workbenchEvents;
    return observed.active?.readyState === EventSource.OPEN && observed.reading === 0;
  });
  const open = async () => {
    await page.goto(origin);
    await page.getByRole("button", { name: "配置 Agent", exact: true }).click();
    await page.getByLabel("选择 Agent", { exact: true }).selectOption(JSON.stringify(["writing-agent", "ask"]));
    await page.getByRole("button", { name: "应用配置", exact: true }).click();
    await page.locator("textarea.composer-input").waitFor();
  };
  const send = async (question = "review question") => {
    const count = pending.length + 1;
    await page.locator("textarea.composer-input").fill(question);
    await page.getByRole("button", { name: "发送", exact: true }).click();
    await awaitMessages(count);
  };
  return { page, posts, pending, threads, streams, heldReads, errors, origin, selections, imageRequests, imageRequest,
    awaitMessages, awaitCondition, releaseRead, complete, emit, settleSubscription, open, send,
    holdRead: () => { holdNextRead = true; }, failRead: () => { failNextRead = true; } };
}

test("original three-panel UI supports different Agents, native continuity, history and mobile drawers", { timeout: 60_000 }, async (t) => {
  const { page, posts, pending, streams, heldReads, errors, origin, selections, awaitMessages, awaitCondition, releaseRead, complete, holdRead, settleSubscription } = await workbench(t);
  try {
    await page.goto(origin);
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
    await settleSubscription();
    holdRead();
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
    await settleSubscription();
    await page.getByRole("button", { name: "新对话", exact: true }).click();
    holdRead();
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
  }
});

// Value: protects=canonical updates during in-flight reads; fails_when=completion is dropped; why_new=existing races invalidate a selection instead; seam=real HTTP/SSE browser fixture.
test("SSE received during a held read schedules another canonical read", { timeout: 60_000 }, async t => {
  const f = await workbench(t); await f.open(); await f.send();
  f.complete(0, "first canonical answer");
  await f.page.getByText("first canonical answer", { exact: true }).waitFor(); await f.settleSubscription();
  const handled = await f.page.evaluate(() => (window as any).__workbenchEvents.handled);
  f.holdRead(); f.emit();
  await f.awaitCondition(() => f.heldReads.length === 1, "first SSE read captured");
  f.threads[0].turns.push({ id: "turn-next", status: "completed", items: [
    { type: "userMessage", content: [{ type: "text", text: "next canonical question" }] },
    { type: "agentMessage", phase: "final_answer", text: "next canonical answer" },
  ] });
  f.emit(); await f.page.waitForFunction(count => (window as any).__workbenchEvents.handled === count, handled + 2);
  f.releaseRead(0);
  await f.page.getByText("next canonical answer", { exact: true }).waitFor();
  assert.ok(f.posts.filter(item => item.path === "/api/threads/read").length >= 2);
});

// Value: protects=recovery unlocks a confirmed thread; fails_when=status_read survives a successful read; why_new=HTTP tests do not mount App; seam=real HTTP/SSE browser fixture.
test("successful SSE read clears the previous status-read failure", { timeout: 60_000 }, async t => {
  const f = await workbench(t); await f.open(); await f.send(); f.complete(0, "first canonical answer");
  await f.page.getByText("first canonical answer", { exact: true }).waitFor(); await f.settleSubscription();
  f.failRead(); f.emit(); await f.page.getByRole("alert").getByText("Runtime HTTP 503", { exact: true }).waitFor();
  f.threads[0].turns[0].items[1].text = "recovered canonical answer";
  f.emit(); await f.page.getByText("recovered canonical answer", { exact: true }).waitFor();
  assert.equal(await f.page.locator("textarea.composer-input").isDisabled(), false);
  assert.equal(await f.page.getByText("Runtime HTTP 503", { exact: true }).count(), 0);
});

for (const continuation of [false, true]) {
  // Value: protects=unknown submissions require explicit history inspection; fails_when=reconnect/reload reenables send or the active history item cannot be selected; why_new=adapter tests cannot observe UI navigation and persistence; seam=real browser and canonical fixture history.
  test(`unknown ${continuation ? "continuation" : "first submission"} requires explicit history inspection after reconnect`, { timeout: 60_000 }, async t => {
    const f = await workbench(t); await f.open();
    if (continuation) {
      await f.send("confirmed conversation"); f.complete(0, "confirmed initial answer");
      await f.page.getByText("confirmed initial answer", { exact: true }).waitFor(); await f.settleSubscription();
    }
    await f.send("unknown submission question");
    f.complete(continuation ? 1 : 0, "committed answer with lost response", { httpStatus: 503 });
    await f.page.getByText("提交状态未确认", { exact: true }).first().waitFor();
    if (continuation) await f.settleSubscription();
    const input = f.page.locator("textarea.composer-input");
    assert.equal(await input.isDisabled(), true);
    const refreshed = f.page.waitForResponse(response => new URL(response.url()).pathname === "/api/threads/list");
    await f.page.getByRole("button", { name: "重新读取运行状态", exact: true }).first().click(); await refreshed;
    const history = f.page.locator(".aui-thread-trigger").filter({ hasText: continuation ? "confirmed conversation" : "unknown submission question" });
    await history.waitFor();
    const blocked = [await input.isDisabled()];
    if (!continuation) { await f.page.reload(); await history.waitFor(); blocked.push(await input.isDisabled()); }
    assert.ok(blocked.every(Boolean), `unconfirmed submission blocking after reconnect/reload: ${JSON.stringify(blocked)}`);
    if (!continuation) assert.equal(f.posts.filter(item => item.path === "/api/threads/read").length, 0);
    assert.equal(f.pending.length, continuation ? 2 : 1, "reconnect and reload must not resubmit");
    // Inspect immediately after reconnect for continuations: reload would hide an already-selected-id early return.
    const reads = f.posts.filter(item => item.path === "/api/threads/read").length;
    await history.click(); await f.page.getByText("committed answer with lost response", { exact: true }).waitFor();
    await f.page.waitForFunction(() => !document.querySelector<HTMLTextAreaElement>("textarea.composer-input")?.disabled);
    assert.ok(f.posts.filter(item => item.path === "/api/threads/read").length > reads);
    assert.ok(f.posts.some(item => item.path === "/api/threads/read" && item.body.threadId === "thread-1"));
  });
}

for (const status of ["completed", "inProgress"]) {
  // Value: protects=model Markdown cannot load image URLs; fails_when=completed or partial answers mount an image; why_new=plain-text fixtures never exercise Markdown images; seam=browser DOM plus actual loopback GET observation.
  test(`Markdown images are inert in ${status} answers`, { timeout: 60_000 }, async t => {
    const f = await workbench(t); await f.open(); await f.send();
    f.complete(0, `Image policy marker.\n\n![synthetic tracker](${f.origin}/markdown-image.png?status=${status})`, { status });
    await f.page.getByText("Image policy marker.", { exact: true }).waitFor();
    const images = await f.page.locator(".assistant-message-body img").count();
    if (images > 0) await f.imageRequest;
    console.log(JSON.stringify({ probe: "markdown-image", status, images, imageGets: f.imageRequests.length }));
    assert.deepEqual({ images, imageGets: f.imageRequests.length }, { images: 0, imageGets: 0 });
  });
}

// Value: protects=keyboard users can locate the focused history search; fails_when=the search rule removes the focus outline; why_new=mobile drawer assertions do not exercise keyboard focus; seam=installed Chrome keyboard navigation and computed focus-visible style.
test("history search retains a visible keyboard focus indicator", { timeout: 60_000 }, async t => {
  const f = await workbench(t); await f.open();
  await f.page.getByRole("button", { name: "新对话", exact: true }).focus();
  await f.page.keyboard.press("Tab");
  const focus = await f.page.getByRole("searchbox", { name: "搜索会话", exact: true }).evaluate(element => {
    const style = getComputedStyle(element);
    return { active: document.activeElement === element, focusVisible: element.matches(":focus-visible"), outlineStyle: style.outlineStyle, outlineWidth: style.outlineWidth };
  });
  console.log(JSON.stringify({ probe: "keyboard-focus", ...focus }));
  assert.equal(focus.active, true); assert.equal(focus.focusVisible, true);
  assert.notEqual(focus.outlineStyle, "none"); assert.ok(parseFloat(focus.outlineWidth) > 0);
});
