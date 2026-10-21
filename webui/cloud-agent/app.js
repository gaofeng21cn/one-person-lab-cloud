import { readRuntimeAgents, agentMessageRequest, readAgentReply } from "./runtime-api.js";

const $ = (id) => document.getElementById(id);
const agentSelect = $("agent"), promptInput = $("prompt"), sendButton = $("send");
const status = $("runtime-status"), hint = $("hint"), messages = $("messages"), diagnostics = $("diagnostics-body");
let csrfToken = null, choices = [], active = false, threadId = null;

async function json(url, init = {}) {
  const headers = new Headers(init.headers || {});
  if (init.method && init.method !== "GET" && csrfToken) headers.set("x-csrf-token", csrfToken);
  const response = await fetch(url, { ...init, headers });
  const body = await response.json().catch(() => null);
  if (!response.ok || body?.error) throw new Error(body?.error?.message || body?.detail || `HTTP ${response.status}`);
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
    if (!choices.length) throw new Error("Runtime has no installed, operational Agent shortcut.");
    agentSelect.disabled = false;
    promptInput.disabled = !runtime.modelReady;
    sendButton.disabled = !runtime.modelReady;
    hint.textContent = runtime.modelReady ? `${choices.length} Agent(s) available` : "Agent loaded. Configure model access in the application before sending a prompt.";
    setStatus(runtime.modelReady ? "Runtime ready" : "Model access required", runtime.modelReady ? "ready" : "error");
    diagnostics.textContent = JSON.stringify({ transport: { state: "/api/opl/state?profile=fast", message: "POST /api/send-message" }, agents: choices.map((choice) => choice.selection), modelReady: runtime.modelReady }, null, 2);
  } catch (error) {
    setStatus("Runtime unavailable", "error");
    hint.textContent = error.message;
    diagnostics.textContent = error.stack || String(error);
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
  promptInput.value = "";
  const answer = addMessage("assistant", "Waiting for Agent…");
  active = true;
  sendButton.disabled = agentSelect.disabled = promptInput.disabled = true;
  try {
    const reply = readAgentReply(await json("/api/send-message", {
      method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify(agentMessageRequest(choice, prompt, threadId))
    }), threadId);
    threadId = reply.threadId;
    answer.textContent = reply.answer;
  } catch (error) {
    answer.remove();
    addMessage("error", error.message);
  } finally {
    active = false;
    sendButton.disabled = agentSelect.disabled = promptInput.disabled = false;
  }
});
load();
