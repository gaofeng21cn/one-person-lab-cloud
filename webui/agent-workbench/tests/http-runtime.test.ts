import assert from "node:assert/strict";
import test from "node:test";
import { OplNativeRuntime, readNativeThread } from "../state/workbench/oplNativeRuntime.ts";
import { ApiHttpError, ApiResultError, type PackageDescriptor } from "../state/workbench/remoteModelAdapter.ts";

const agent: PackageDescriptor = {
  choiceId: JSON.stringify(["test-agent", "main"]),
  packageId: "test-agent",
  name: "Test Agent",
  selection: { package_id: "test-agent", shortcut_id: "main", codex_visible_entry: "test-agent:main", required_skill_ids: ["test-agent"] },
};
const directory = {
  app_state: { version: "g2", app_state: {
    schema_version: "opl_app_state.v1",
    core: { codex: { model_access_ready: true } },
    agent_packages: { directory: { entries: [{
      package_id: agent.packageId, display_name: agent.name, package_role: "standard_agent",
      readiness: { operational_ready: true },
      capability_metadata: { required_skill_ids: agent.selection.required_skill_ids },
      home_shortcuts: [{ shortcut_id: "main", route: { route_kind: "agent_package_shortcut", executor: "codex_cli", codex_visible_entry: "test-agent:main" } }],
    }] } },
  } },
};
const session = { authenticated: true, csrfToken: "session-csrf" };
const thread = (status = "completed", id = "thread-1") => ({ id, turns: [{
  id: "turn-1", status,
  items: [
    { id: "user-1", type: "userMessage", content: [{ type: "text", text: "真实问题" }, { type: "mention", name: "notes", path: "/inputs/notes.txt" }] },
    { id: "progress-1", type: "agentMessage", phase: "commentary", text: "正在分析" },
    { id: "tool-1", type: "mcpToolCall", result: { content: [{ type: "text", text: "不得成为答案 [99]" }] } },
    { id: "answer-1", type: "agentMessage", phase: "final_answer", text: "  原文回答 [1]\n\nhttps://example.test/paper  " },
  ],
  ...(status === "failed" ? { error: { message: "Owner execution failed", codexErrorInfo: "other" } } : {}),
}] });
const sendReply = (status = "completed", id = "thread-1") => ({
  executor: "codex_app_server", transport: "stdio_json_rpc", threadId: id, turnId: "turn-1",
  finalMessage: "  原文回答 [1]\n\nhttps://example.test/paper  ", canonicalThread: thread(status, id),
});

type Step = { path: string; response: unknown; status?: number; check?: (init: RequestInit) => void };
function fixture(steps: Step[]) {
  let index = 0;
  const fetcher: typeof fetch = async (input, init = {}) => {
    const step = steps[index++];
    assert.ok(step, `Unexpected HTTP request ${String(input)}`);
    assert.equal(String(input), step.path);
    assert.equal(init.credentials, "same-origin");
    assert.equal(init.redirect, "error");
    if (init.method === "POST") {
      const headers = new Headers(init.headers);
      assert.equal(headers.get("x-csrf-token"), session.csrfToken);
      assert.equal(headers.get("content-type"), "application/json");
    }
    step.check?.(init);
    return Response.json(step.response, { status: step.status ?? 200 });
  };
  return { runtime: new OplNativeRuntime(fetcher), done: () => assert.equal(index, steps.length) };
}
const connection = (): Step[] => [
  { path: "/api/auth/user", response: session },
  { path: "/api/opl/state?profile=fast", response: directory },
];
const body = (init: RequestInit) => JSON.parse(String(init.body));

test("connect consumes authenticated session then the exact g2 directory", async () => {
  const controller = new AbortController();
  const f = fixture(connection().map(step => ({ ...step, check: init => assert.equal(init.signal, controller.signal) })));
  assert.deepEqual(await f.runtime.connect(controller.signal), { packages: [agent], modelReady: true });
  f.done();
});

test("connect rejects unauthenticated or missing/empty CSRF sessions before directory reads", async () => {
  for (const response of [{ authenticated: false, csrfToken: "token" }, { authenticated: true }, { authenticated: true, csrfToken: " " }]) {
    const f = fixture([{ path: "/api/auth/user", response }]);
    await assert.rejects(f.runtime.connect(), ApiResultError);
    f.done();
  }
});

test("failed reconnect revokes the old CSRF session", async () => {
  const f = fixture([...connection(), { path: "/api/auth/user", response: { authenticated: false } }]);
  await f.runtime.connect();
  await assert.rejects(f.runtime.connect(), ApiResultError);
  await assert.rejects(f.runtime.listThreads(), ApiResultError);
  f.done();
});

test("listThreads sends CSRF and maps only data[].id/summary without changing empty titles", async () => {
  const signal = new AbortController().signal;
  const f = fixture([...connection(), {
    path: "/api/threads/list", response: { data: [{ id: "native-id", sessionId: "different-session", summary: "原生标题" }, { id: "empty", summary: "" }], nextCursor: null },
    check: init => { assert.deepEqual(body(init), {}); assert.equal(init.signal, signal); },
  }]);
  await f.runtime.connect();
  assert.deepEqual(await f.runtime.listThreads(signal), [{ id: "native-id", title: "原生标题" }, { id: "empty", title: "" }]);
  f.done();
});

test("listThreads rejects legacy lists and missing native summary", async () => {
  for (const response of [{ threads: [{ id: "x", summary: "old" }] }, { data: [{ id: "x", name: "not summary" }] }]) {
    const f = fixture([...connection(), { path: "/api/threads/list", response }]);
    await f.runtime.connect();
    await assert.rejects(f.runtime.listThreads(), ApiResultError);
    f.done();
  }
});

test("readThread requests includeTurns and converts actual native messages without references", async () => {
  const signal = new AbortController().signal;
  const f = fixture([...connection(), { path: "/api/threads/read", response: thread(), check: init => {
    assert.deepEqual(body(init), { threadId: "thread-1", includeTurns: true });
    assert.equal(init.signal, signal);
  } }]);
  await f.runtime.connect();
  assert.deepEqual(await f.runtime.readThread("thread-1", signal), [{
    runId: "turn-1", threadId: "thread-1", question: "真实问题", status: "succeeded", progress: { phase: "completed" },
    result: { runId: "turn-1", reply: sendReply().finalMessage, evidence: [] },
  }]);
  f.done();
});

test("readNativeThread preserves chronological original final messages, excluding commentary and tools", () => {
  const value = thread();
  value.turns[0].items.push({ id: "answer-2", type: "agentMessage", phase: "final_answer", text: "第二段原文\n" });
  assert.equal(readNativeThread(value)[0].result?.reply, `${sendReply().finalMessage}\n\n第二段原文\n`);
  assert.deepEqual(readNativeThread(value)[0].result?.evidence, []);
});

test("readNativeThread retains running/failed/interrupted states and owner errors", () => {
  const running = readNativeThread(thread("inProgress"))[0];
  assert.equal(running.status, "running");
  assert.equal(running.partialReply, sendReply().finalMessage);
  assert.equal(running.result, undefined);
  const failed = readNativeThread(thread("failed"))[0];
  assert.equal(failed.status, "failed");
  assert.equal(failed.error?.message, "Owner execution failed");
  assert.equal(failed.result, undefined);
  assert.equal(readNativeThread(thread("interrupted"))[0].status, "cancelled");
});

test("readNativeThread rejects wrappers, old shapes, identity mismatch and unconfirmed completion", () => {
  for (const value of [{ thread: thread() }, { id: "thread-1", messages: [] }, { id: "thread-1", turns: [{ id: "turn-1", status: "completed", items: [] }] }, thread("succeeded")]) {
    assert.throws(() => readNativeThread(value), ApiResultError);
  }
  assert.throws(() => readNativeThread(thread(), "different-thread"), ApiResultError);
});

test("sendMessage sends Agent selection only on first turn; continuation uses canonical thread id", async () => {
  const f = fixture([...connection(),
    { path: "/api/send-message", response: sendReply(), check: init => assert.deepEqual(body(init), { prompt: "首轮", agentSelection: agent.selection }) },
    { path: "/api/send-message", response: sendReply(), check: init => assert.deepEqual(body(init), { prompt: "续轮", threadId: "thread-1" }) },
  ]);
  await f.runtime.connect();
  const first = await f.runtime.sendMessage(agent, "首轮");
  assert.equal(first.question, "首轮");
  assert.equal(first.packageId, "test-agent");
  assert.equal(first.result?.reply, readNativeThread(thread())[0].result?.reply);
  assert.deepEqual(first.result?.evidence, []);
  const next = await f.runtime.sendMessage(agent, "续轮", "thread-1");
  assert.equal(next.question, "续轮");
  assert.equal(next.runId, "turn-1");
  assert.equal(next.threadId, "thread-1");
  f.done();
});

test("sendMessage returns failed/running canonical states without fabricating success", async () => {
  const f = fixture([...connection(),
    { path: "/api/send-message", response: sendReply("failed") },
    { path: "/api/send-message", response: sendReply("inProgress") },
  ]);
  await f.runtime.connect();
  const failed = await f.runtime.sendMessage(agent, "失败问题");
  assert.equal(failed.status, "failed");
  assert.equal(failed.question, "失败问题");
  assert.equal(failed.error?.message, "Owner execution failed");
  assert.equal(failed.result, undefined);
  assert.equal((await f.runtime.sendMessage(agent, "运行中")).status, "running");
  f.done();
});

test("sendMessage rejects divergent final text and a different canonical thread or turn", async () => {
  const values = [
    { ...sendReply(), finalMessage: "不一致" },
    { ...sendReply(), canonicalThread: thread("completed", "other") },
    { ...sendReply(), turnId: "missing-turn" },
    sendReply("completed", "other"),
  ];
  for (const response of values) {
    const f = fixture([...connection(), { path: "/api/send-message", response }]);
    await f.runtime.connect();
    await assert.rejects(f.runtime.sendMessage(agent, "问题", "thread-1"), ApiResultError);
    f.done();
  }
});

test("rename/delete use native fields and validate mutation response identity", async () => {
  const f = fixture([...connection(),
    { path: "/api/threads/rename", response: { id: "thread-1", summary: "新标题" }, check: init => assert.deepEqual(body(init), { threadId: "thread-1", name: "新标题" }) },
    { path: "/api/threads/delete", response: { threadId: "thread-1", deleted: true }, check: init => assert.deepEqual(body(init), { threadId: "thread-1", confirmed: true }) },
  ]);
  await f.runtime.connect();
  await f.runtime.renameThread("thread-1", "新标题");
  await f.runtime.deleteThread("thread-1");
  f.done();
});

test("delete rejects false confirmation and another thread identity", async () => {
  for (const response of [{ threadId: "thread-1", deleted: false }, { threadId: "other", deleted: true }]) {
    const f = fixture([...connection(), { path: "/api/threads/delete", response }]);
    await f.runtime.connect();
    await assert.rejects(f.runtime.deleteThread("thread-1"), ApiResultError);
    f.done();
  }
});

test("HTTP errors retain the shared typed ApiHttpError and status", async () => {
  const f = fixture([...connection(), { path: "/api/threads/list", status: 403, response: { error: { code: "invalid_csrf_token" } } }]);
  await f.runtime.connect();
  await assert.rejects(f.runtime.listThreads(), error => error instanceof ApiHttpError && error.status === 403);
  f.done();
});


test("the browser fetch function is called without an adapter receiver", async () => {
  const fetcher = function (this: unknown, _input: RequestInfo | URL, _init?: RequestInit): Promise<Response> {
    assert.equal(this, undefined);
    return Promise.resolve(Response.json({ authenticated: false }));
  } as typeof fetch;
  await assert.rejects(new OplNativeRuntime(fetcher).connect(), /session/);
});
