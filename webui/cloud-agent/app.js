import { readRuntimeAgents, agentMessageRequest, readAgentReply } from "./runtime-api.js";

const $ = (id) => document.getElementById(id);
const agentSelect = $("agent");
const promptInput = $("prompt");
const sendButton = $("send");
const newChatButton = $("new-chat");
const exportChatButton = $("export-chat");
const consoleLink = $("console-link");
const workspaceTitle = $("workspace-title");
const status = $("runtime-status");
const hint = $("hint");
const guidance = $("guidance");
const messages = $("messages");
const diagnostics = $("diagnostics-body");

let csrfToken = null;
let choices = [];
let active = false;
let threadId = null;
let modelReady = false;
let consoleBaseUrl = "";
let workspaceId = "";
let history = [];

// The Console and the Workspace entry live on separate hosts by installation
// contract. Only render console/model links when the Runtime explicitly
// publishes an absolute consoleUrl; never guess a same-host /console path.
function resolveConsoleBase(identity) {
  const candidate = typeof identity?.consoleUrl === "string" ? identity.consoleUrl.trim() : "";
  if (candidate && /^https?:\/\//i.test(candidate)) return candidate.replace(/\/+$/, "");
  return "";
}

function consolePath(path) {
  return consoleBaseUrl ? `${consoleBaseUrl}${path}` : "";
}

function modelsConfigUrl() {
  if (!consoleBaseUrl) return "";
  return workspaceId
    ? consolePath(`/console/workspaces/${encodeURIComponent(workspaceId)}/models`)
    : consolePath("/console/workspaces");
}

function workspaceDetailUrl() {
  if (!consoleBaseUrl) return "";
  return workspaceId
    ? consolePath(`/console/workspaces/${encodeURIComponent(workspaceId)}`)
    : consolePath("/console/workspaces");
}

async function copyText(text, button, defaultLabel) {
  try {
    await navigator.clipboard?.writeText(text);
    button.textContent = "已复制";
    window.setTimeout(() => { button.textContent = defaultLabel; }, 2000);
  } catch {
    button.textContent = "复制失败";
    window.setTimeout(() => { button.textContent = defaultLabel; }, 2000);
  }
}

async function json(url, init = {}) {
  const headers = new Headers(init.headers || {});
  if (init.method && init.method !== "GET" && csrfToken) headers.set("x-csrf-token", csrfToken);
  const response = await fetch(url, { ...init, headers });
  const body = await response.json().catch(() => null);
  if (!response.ok || body?.error) {
    const error = new Error(body?.error?.message || body?.detail || `HTTP ${response.status}`);
    error.httpStatus = response.status;
    throw error;
  }
  return body;
}

function setStatus(text, kind = "") {
  status.textContent = text;
  status.className = kind ? `status ${kind}` : "status";
}

function hideGuidance() {
  guidance.hidden = true;
  guidance.className = "guidance";
  guidance.replaceChildren();
}

function showGuidance({ title, description, tone = "warning", actions = [] }) {
  guidance.hidden = false;
  guidance.className = tone === "danger" ? "guidance guidance--danger" : "guidance";
  const copy = document.createElement("div");
  copy.className = "guidance-copy";
  const heading = document.createElement("strong");
  heading.textContent = title;
  const body = document.createElement("p");
  body.textContent = description;
  copy.append(heading, body);

  const actionWrap = document.createElement("div");
  actionWrap.className = "guidance-actions";
  for (const action of actions) {
    if (action.href) {
      const link = document.createElement("a");
      link.href = action.href;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.className = action.primary ? "btn-primary" : "btn-link";
      link.textContent = action.label;
      actionWrap.append(link);
    } else if (!action.hrefOnly) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = action.primary ? "btn-primary" : "btn-secondary";
      button.textContent = action.label;
      button.addEventListener("click", action.onClick);
      actionWrap.append(button);
    }
  }
  guidance.replaceChildren(copy, actionWrap);
}

function updateToolbarState() {
  const hasConversation = history.length > 0;
  newChatButton.disabled = active || !hasConversation;
  exportChatButton.disabled = active || !hasConversation;
}

function showEmptyState() {
  const empty = document.createElement("div");
  empty.className = "empty";
  const title = document.createElement("strong");
  title.textContent = "开始对话";
  const desc = document.createElement("span");
  desc.textContent = "输入任务开始对话，按 Ctrl / ⌘ + Enter 发送。";
  empty.append(title, desc);
  messages.replaceChildren(empty);
}

function appendInlineMarkdown(parent, text) {
  const tokenPattern = /(`[^`]+`|\*\*[^*]+\*\*|\[([^\]]+)\]\((https?:\/\/[^\s)]+)\))/g;
  let lastIndex = 0;
  let match;
  while ((match = tokenPattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      parent.append(document.createTextNode(text.slice(lastIndex, match.index)));
    }
    const token = match[0];
    if (token.startsWith("`")) {
      const code = document.createElement("code");
      code.className = "md-inline-code";
      code.textContent = token.slice(1, -1);
      parent.append(code);
    } else if (token.startsWith("**")) {
      const strong = document.createElement("strong");
      strong.textContent = token.slice(2, -2);
      parent.append(strong);
    } else if (match[2] && match[3]) {
      const link = document.createElement("a");
      link.className = "md-link";
      link.href = match[3];
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = match[2];
      parent.append(link);
    }
    lastIndex = tokenPattern.lastIndex;
  }
  if (lastIndex < text.length) {
    parent.append(document.createTextNode(text.slice(lastIndex)));
  }
}

function renderSafeMarkdown(container, markdown) {
  container.replaceChildren();
  const lines = String(markdown).replace(/\r\n/g, "\n").split("\n");
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];
    if (line.trim().startsWith("```")) {
      const lang = line.trim().slice(3).trim();
      const codeLines = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith("```")) {
        codeLines.push(lines[index]);
        index += 1;
      }
      if (index < lines.length) index += 1;
      const codeText = codeLines.join("\n");
      const block = document.createElement("div");
      block.className = "md-code-block";
      const header = document.createElement("div");
      header.className = "md-code-header";
      const langSpan = document.createElement("span");
      langSpan.textContent = lang || "代码";
      const copyBtn = document.createElement("button");
      copyBtn.type = "button";
      copyBtn.className = "md-code-copy";
      copyBtn.textContent = "复制代码";
      copyBtn.addEventListener("click", () => void copyText(codeText, copyBtn, "复制代码"));
      header.append(langSpan, copyBtn);
      const pre = document.createElement("pre");
      const code = document.createElement("code");
      code.textContent = codeText;
      pre.append(code);
      block.append(header, pre);
      container.append(block);
      continue;
    }

    const headingMatch = /^(#{1,3})\s+(.+)$/.exec(line.trim());
    if (headingMatch) {
      const heading = document.createElement("h3");
      heading.className = "md-h";
      appendInlineMarkdown(heading, headingMatch[2]);
      container.append(heading);
      index += 1;
      continue;
    }

    if (/^\s*[-*]\s+/.test(line)) {
      const ul = document.createElement("ul");
      ul.className = "md-list";
      while (index < lines.length && /^\s*[-*]\s+/.test(lines[index])) {
        const li = document.createElement("li");
        appendInlineMarkdown(li, lines[index].replace(/^\s*[-*]\s+/, ""));
        ul.append(li);
        index += 1;
      }
      container.append(ul);
      continue;
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const ol = document.createElement("ol");
      ol.className = "md-list";
      while (index < lines.length && /^\s*\d+\.\s+/.test(lines[index])) {
        const li = document.createElement("li");
        appendInlineMarkdown(li, lines[index].replace(/^\s*\d+\.\s+/, ""));
        ol.append(li);
        index += 1;
      }
      container.append(ol);
      continue;
    }

    if (!line.trim()) {
      index += 1;
      continue;
    }

    const paragraphLines = [];
    while (
      index < lines.length &&
      lines[index].trim() &&
      !lines[index].trim().startsWith("```") &&
      !/^(#{1,3})\s+/.test(lines[index].trim()) &&
      !/^\s*[-*]\s+/.test(lines[index]) &&
      !/^\s*\d+\.\s+/.test(lines[index])
    ) {
      paragraphLines.push(lines[index]);
      index += 1;
    }
    const p = document.createElement("p");
    p.className = "md-p";
    paragraphLines.forEach((pLine, lineIndex) => {
      if (lineIndex > 0) p.append(document.createElement("br"));
      appendInlineMarkdown(p, pLine);
    });
    container.append(p);
  }
}

function addMessage(role, text, options = {}) {
  messages.querySelector(".empty")?.remove();
  const row = document.createElement("div");
  row.className = `message-row message-row--${role}`;
  const item = document.createElement("div");
  item.className = `message ${role}${options.pending ? " pending" : ""}`;
  if (role === "assistant" && !options.pending) {
    renderSafeMarkdown(item, text);
  } else {
    item.textContent = text;
  }
  row.append(item);

  const meta = document.createElement("div");
  meta.className = "message-meta";
  if (role === "assistant" && !options.pending) {
    const copyBtn = document.createElement("button");
    copyBtn.type = "button";
    copyBtn.className = "btn-ghost";
    copyBtn.textContent = "复制回复";
    copyBtn.addEventListener("click", () => void copyText(text, copyBtn, "复制回复"));
    meta.append(copyBtn);
  }
  if (role === "error" && options.onRetry) {
    const retryBtn = document.createElement("button");
    retryBtn.type = "button";
    retryBtn.className = "btn-ghost";
    retryBtn.textContent = "重试发送";
    retryBtn.addEventListener("click", () => {
      row.remove();
      options.onRetry();
    });
    meta.append(retryBtn);
  }
  if (meta.childElementCount > 0) row.append(meta);

  messages.append(row);
  messages.scrollTop = messages.scrollHeight;
  return { row, item };
}

function updateSelectedTitle() {
  const choice = choices.find((item) => item.id === agentSelect.value);
  if (choice?.label && !workspaceTitle.dataset.locked) {
    workspaceTitle.textContent = choice.label;
  }
}

function formatErrorForUser(error) {
  if (error?.httpStatus === 401 || error?.httpStatus === 403) {
    return "登录状态已失效，请刷新页面或返回控制台重新打开工作空间。";
  }
  return error?.message || "请求失败，请稍后重试。";
}

function renderAuthExpiredGuidance() {
  setStatus("登录已失效", "error");
  hint.textContent = "请刷新或返回控制台重试。";
  const actions = [{ label: "刷新页面", primary: true, onClick: () => window.location.reload() }];
  const backUrl = workspaceDetailUrl();
  if (backUrl) actions.push({ label: "返回控制台", href: backUrl });
  showGuidance({
    title: "登录状态已失效",
    description: "会话已过期，请刷新页面或返回控制台重新进入。",
    tone: "danger",
    actions
  });
}

async function load() {
  hideGuidance();
  setStatus("正在连接…");
  hint.textContent = "正在检查运行状态…";
  try {
    const identity = await json("/api/auth/user");
    csrfToken = typeof identity?.csrfToken === "string" ? identity.csrfToken : null;
    consoleBaseUrl = resolveConsoleBase(identity);
    workspaceId = typeof identity?.workspaceId === "string" ? identity.workspaceId.trim() : "";
    const detailUrl = workspaceDetailUrl();
    consoleLink.hidden = !detailUrl;
    if (detailUrl) consoleLink.href = detailUrl;
    else consoleLink.removeAttribute("href");
    if (identity?.workspaceName) {
      workspaceTitle.textContent = identity.workspaceName;
      workspaceTitle.dataset.locked = "true";
    }

    const runtime = readRuntimeAgents(await json("/api/opl/state?profile=fast"));
    choices = runtime.choices;
    modelReady = runtime.modelReady;
    agentSelect.replaceChildren(...choices.map((choice) => {
      const option = document.createElement("option");
      option.value = choice.id;
      option.textContent = choice.label;
      return option;
    }));
    if (!choices.length) {
      throw new Error("当前工作空间暂无可用智能体，请稍后刷新或返回控制台检查状态。");
    }
    updateSelectedTitle();
    agentSelect.disabled = false;
    promptInput.disabled = !modelReady;
    sendButton.disabled = !modelReady;

    if (modelReady) {
      hideGuidance();
      hint.textContent = choices.length > 1 ? `${choices.length} 个智能体可用` : "Ctrl / ⌘ + Enter 发送";
      setStatus("运行就绪", "ready");
    } else {
      hint.textContent = "请先配置模型";
      setStatus("需配置模型", "warning");
      const configUrl = modelsConfigUrl();
      const actions = [];
      if (configUrl) actions.push({ label: "前往控制台配置模型", primary: true, href: configUrl });
      actions.push({ label: "刷新状态", primary: !configUrl, onClick: () => void load() });
      showGuidance({
        title: "尚未配置可用模型",
        description: "请先在控制台配置可用模型，完成后刷新本页即可开始对话。",
        tone: "warning",
        actions
      });
    }

    diagnostics.textContent = JSON.stringify({
      transport: { state: "/api/opl/state?profile=fast", message: "POST /api/send-message" },
      agents: choices.map((choice) => choice.selection),
      modelReady
    }, null, 2);
  } catch (error) {
    promptInput.disabled = true;
    sendButton.disabled = true;
    diagnostics.textContent = error?.stack || String(error);
    if (error?.httpStatus === 401 || error?.httpStatus === 403) {
      renderAuthExpiredGuidance();
      return;
    }
    setStatus("服务暂不可用", "error");
    hint.textContent = "暂时无法连接，请重试。";
    const actions = [{ label: "重新连接", primary: true, onClick: () => void load() }];
    const backUrl = workspaceDetailUrl();
    if (backUrl) actions.push({ label: "返回控制台", href: backUrl });
    showGuidance({
      title: "暂时无法连接工作空间",
      description: error?.message && !/^HTTP \d+$/.test(error.message)
        ? error.message
        : "暂时无法连接，请稍后重试或返回控制台查看状态。",
      tone: "danger",
      actions
    });
  }
}

function resetConversation() {
  if (active) return;
  threadId = null;
  history = [];
  showEmptyState();
  updateToolbarState();
}

function exportConversationMarkdown() {
  if (!history.length) return;
  const choice = choices.find((item) => item.id === agentSelect.value);
  const title = workspaceTitle.textContent || choice?.label || "智能体对话";
  const lines = [
    `# ${title}`,
    "",
    `- 导出时间：${new Date().toISOString()}`,
    choice?.label ? `- 智能体：${choice.label}` : "",
    "",
    ...history.flatMap((entry) => [
      `## ${entry.role === "user" ? "用户" : "智能体"}`,
      "",
      entry.text,
      ""
    ])
  ].filter((line, idx, arr) => !(line === "" && arr[idx - 1] === ""));
  const blob = new Blob([lines.join("\n")], { type: "text/markdown;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `conversation-${new Date().toISOString().slice(0, 10)}.md`;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

async function submitPrompt(promptText, reuseUserMessage = false) {
  if (active || !modelReady) return;
  const prompt = promptText.trim();
  const choice = choices.find((item) => item.id === agentSelect.value);
  if (!prompt || !choice) return;

  if (!reuseUserMessage) {
    addMessage("user", prompt);
    history.push({ role: "user", text: prompt });
    promptInput.value = "";
  }
  const pending = addMessage("assistant", "智能体正在思考…", { pending: true });
  active = true;
  sendButton.disabled = agentSelect.disabled = promptInput.disabled = true;
  updateToolbarState();

  try {
    const reply = readAgentReply(await json("/api/send-message", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(agentMessageRequest(choice, prompt, threadId))
    }), threadId);
    threadId = reply.threadId;
    pending.item.className = "message assistant";
    renderSafeMarkdown(pending.item, reply.answer);
    const meta = document.createElement("div");
    meta.className = "message-meta";
    const copyBtn = document.createElement("button");
    copyBtn.type = "button";
    copyBtn.className = "btn-ghost";
    copyBtn.textContent = "复制回复";
    copyBtn.addEventListener("click", () => void copyText(reply.answer, copyBtn, "复制回复"));
    meta.append(copyBtn);
    pending.row.append(meta);
    history.push({ role: "assistant", text: reply.answer });
    messages.scrollTop = messages.scrollHeight;
  } catch (error) {
    pending.row.remove();
    if (error?.httpStatus === 401 || error?.httpStatus === 403) {
      renderAuthExpiredGuidance();
    }
    addMessage("error", formatErrorForUser(error), {
      onRetry: () => void submitPrompt(prompt, true)
    });
  } finally {
    active = false;
    agentSelect.disabled = false;
    promptInput.disabled = !modelReady;
    sendButton.disabled = !modelReady;
    updateToolbarState();
  }
}

agentSelect.addEventListener("change", () => {
  threadId = null;
  updateSelectedTitle();
});

newChatButton.addEventListener("click", resetConversation);
exportChatButton.addEventListener("click", exportConversationMarkdown);

promptInput.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    event.preventDefault();
    $("composer").requestSubmit();
  }
});

$("composer").addEventListener("submit", (event) => {
  event.preventDefault();
  if (active || sendButton.disabled) return;
  void submitPrompt(promptInput.value);
});

load();
