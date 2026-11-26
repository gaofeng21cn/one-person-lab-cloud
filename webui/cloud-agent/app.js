import { readRuntimeAgents, agentMessageRequest, readAgentReply } from "./runtime-api.js";
import { renderMarkdown, conversationMarkdown } from "./markdown.js";

const $ = (id) => document.getElementById(id);
const agentSelect = $("agent"), promptInput = $("prompt"), sendButton = $("send");
const status = $("runtime-status"), hint = $("hint"), messages = $("messages"), diagnostics = $("diagnostics-body");
const sessionNotice = $("session-notice"), modelHint = $("model-hint");
const conversationActions = $("conversation-actions"), conversationStatus = $("conversation-status");
let csrfToken = null, choices = [], active = false, threadId = null;
let conversation = [];

async function json(url, init = {}) {
  const headers = new Headers(init.headers || {});
  if (init.method && init.method !== "GET" && csrfToken) headers.set("x-csrf-token", csrfToken);
  const response = await fetch(url, { ...init, headers });
  const body = await response.json().catch(() => null);
  // An expired session is a customer state, not a stack trace: the page says
  // so and offers the way back in.
  if (response.status === 401) throw new Error("登录状态已失效");
  if (!response.ok || body?.error) throw new Error(body?.error?.message || body?.detail || `请求失败（HTTP ${response.status}）`);
  return body;
}
function setStatus(text, kind = "") { status.textContent = text; status.className = `status ${kind}`; }
function addMessage(role, text) {
  messages.querySelector(".empty")?.remove();
  const item = document.createElement("div");
  item.className = `message ${role}`;
  item.textContent = text;
  messages.append(item);
  messages.scrollTop = messages.scrollHeight;
  return item;
}
function refreshConversationActions() { conversationActions.hidden = conversation.length === 0; }
function conversationText() { return conversationMarkdown(conversation); }
$("copy-conversation").addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(conversationText());
    conversationStatus.textContent = "已复制对话";
  } catch {
    conversationStatus.textContent = "复制失败，请手动选择对话内容复制。";
  }
});
$("export-conversation").addEventListener("click", () => {
  const blob = new Blob([conversationText()], { type: "text/markdown;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `agent-conversation-${new Date().toISOString().replace(/[:.]/g, "-")}.md`;
  document.body.append(link);
  link.click();
  link.remove();
  // The download reads the blob after this turn, so the URL is released later.
  setTimeout(() => URL.revokeObjectURL(url), 0);
  conversationStatus.textContent = "已导出 Markdown 文件";
});
$("reload-session").addEventListener("click", () => { window.location.reload(); });
async function load() {
  try {
    const identity = await json("/api/auth/user");
    csrfToken = typeof identity?.csrfToken === "string" ? identity.csrfToken : null;
    const runtime = readRuntimeAgents(await json("/api/opl/state?profile=fast"));
    choices = runtime.choices;
    agentSelect.replaceChildren(...choices.map((choice) => {
      const option = document.createElement("option");
      option.value = choice.id;
      option.textContent = choice.label;
      return option;
    }));
    if (!choices.length) throw new Error("运行时没有已安装且可用的智能体。");
    agentSelect.disabled = false;
    promptInput.disabled = !runtime.modelReady;
    sendButton.disabled = !runtime.modelReady;
    modelHint.hidden = runtime.modelReady;
    hint.textContent = runtime.modelReady ? `已就绪 ${choices.length} 个智能体` : "智能体已加载，但还没有可用的模型访问权限。";
    setStatus(runtime.modelReady ? "运行时就绪" : "需要配置模型", runtime.modelReady ? "ready" : "error");
    diagnostics.textContent = JSON.stringify({ transport: { state: "/api/opl/state?profile=fast", message: "POST /api/send-message" }, agents: choices.map((choice) => choice.selection), modelReady: runtime.modelReady }, null, 2);
  } catch (error) {
    const expired = error.message === "登录状态已失效";
    sessionNotice.hidden = !expired;
    setStatus(expired ? "需要重新登录" : "运行时不可用", "error");
    hint.textContent = error.message;
    // A customer-facing page never opens with a JavaScript stack.
    diagnostics.textContent = expired ? "会话已过期，等待重新登录。" : (error.stack || String(error));
  }
}
agentSelect.addEventListener("change", () => { threadId = null; });
$("composer").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (active || sendButton.disabled) return;
  const prompt = promptInput.value.trim();
  const choice = choices.find((item) => item.id === agentSelect.value);
  if (!prompt || !choice) return;
  addMessage("user", prompt);
  conversation.push({ role: "user", text: prompt });
  refreshConversationActions();
  promptInput.value = "";
  const answer = addMessage("assistant", "正在等待智能体回答…");
  active = true;
  sendButton.disabled = agentSelect.disabled = promptInput.disabled = true;
  try {
    const reply = readAgentReply(await json("/api/send-message", {
      method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify(agentMessageRequest(choice, prompt, threadId))
    }), threadId);
    threadId = reply.threadId;
    answer.replaceChildren(renderMarkdown(reply.answer));
    conversation.push({ role: "assistant", text: reply.answer });
    refreshConversationActions();
  } catch (error) {
    answer.remove();
    addMessage("error", error.message);
  } finally {
    active = false;
    sendButton.disabled = agentSelect.disabled = promptInput.disabled = false;
  }
});
load();
