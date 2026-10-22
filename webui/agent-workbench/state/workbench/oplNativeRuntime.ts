import {
  ApiHttpError,
  ApiResultError,
  readAgentReply,
  readRuntimeAgents,
  type ApiRun,
  type Conversation,
  type PackageDescriptor,
} from "./remoteModelAdapter.ts";

type JsonObject = Record<string, unknown>;
const object = (value: unknown): value is JsonObject =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const nonempty = (value: unknown): value is string =>
  typeof value === "string" && value.trim().length > 0;

function invalid(message: string): never {
  throw new ApiResultError(message);
}

/** Only the native Thread -> turns -> items representation is accepted. */
export function readNativeThread(value: unknown, expectedThreadId?: string): ApiRun[] {
  if (!object(value) || !nonempty(value.id) || !Array.isArray(value.turns)
    || (expectedThreadId !== undefined && value.id !== expectedThreadId)) {
    return invalid("Runtime 未返回匹配的原生线程");
  }
  const threadId = value.id;
  return value.turns.map((turn: unknown): ApiRun => {
    if (!object(turn) || !nonempty(turn.id) || !Array.isArray(turn.items)) {
      return invalid("Runtime 原生 turn 结构无效");
    }
    const questions: string[] = [];
    const answers: string[] = [];
    for (const item of turn.items) {
      if (!object(item) || typeof item.type !== "string") {
        return invalid("Runtime 原生 item 结构无效");
      }
      if (item.type === "userMessage") {
        if (!Array.isArray(item.content)) return invalid("Runtime 用户消息结构无效");
        const blocks: string[] = [];
        for (const block of item.content) {
          if (!object(block) || typeof block.type !== "string") return invalid("Runtime 用户消息内容无效");
          if (block.type === "text") {
            if (typeof block.text !== "string") return invalid("Runtime 用户消息文本无效");
            blocks.push(block.text);
          }
        }
        if (blocks.length) questions.push(blocks.join("\n"));
      } else if (item.type === "agentMessage") {
        if (typeof item.text !== "string") return invalid("Runtime 助手消息文本无效");
        if (item.phase !== "commentary") answers.push(item.text);
      }
    }
    const reply = answers.join("\n\n");
    const base = {
      runId: turn.id,
      threadId,
      ...(questions.length ? { question: questions.join("\n\n") } : {}),
      progress: { phase: String(turn.status) },
    };
    switch (turn.status) {
      case "completed":
        if (!reply.trim()) return invalid("Runtime 已完成 turn 缺少原生回答正文");
        return { ...base, status: "succeeded", result: { runId: turn.id, reply, evidence: [] } };
      case "inProgress":
        return { ...base, status: "running", ...(reply ? { partialReply: reply } : {}) };
      case "failed":
      case "interrupted":
      case "cancelled": {
        const error = turn.error;
        if (error !== undefined && error !== null
          && (!object(error) || typeof error.message !== "string")) {
          return invalid("Runtime turn 错误结构无效");
        }
        return {
          ...base,
          status: turn.status === "failed" ? "failed" : "cancelled",
          ...(reply ? { partialReply: reply } : {}),
          ...(object(error) ? { error: {
            code: nonempty(error.code) ? error.code : `native_turn_${turn.status}`,
            message: error.message as string,
          } } : {}),
        };
      }
      default:
        return invalid("Runtime 原生 turn 状态不受支持");
    }
  });
}

export class OplNativeRuntime {
  private readonly fetcher: typeof fetch;
  private csrfToken: string | null = null;

  constructor(fetcher: typeof fetch = fetch) {
    this.fetcher = fetcher;
  }

  private async request(url: string, init: RequestInit = {}): Promise<unknown> {
    const fetcher = this.fetcher;
    const response = await fetcher(url, {
      ...init,
      credentials: "same-origin",
      redirect: "error",
    });
    if (!response.ok) throw new ApiHttpError(`Runtime HTTP ${response.status}`, response.status);
    return response.json();
  }

  private post(url: string, value: unknown, signal?: AbortSignal): Promise<unknown> {
    if (this.csrfToken === null) return Promise.reject(new ApiResultError("请先连接 Runtime session"));
    return this.request(url, {
      method: "POST",
      headers: { "content-type": "application/json", "x-csrf-token": this.csrfToken },
      body: JSON.stringify(value),
      ...(signal ? { signal } : {}),
    });
  }

  async connect(signal?: AbortSignal): Promise<{ packages: PackageDescriptor[]; modelReady: boolean }> {
    this.csrfToken = null;
    const session = await this.request("/api/auth/user", { signal });
    if (!object(session) || session.authenticated !== true || !nonempty(session.csrfToken)) {
      return invalid("Runtime 未确认已认证的 session 与 CSRF token");
    }
    const agents = readRuntimeAgents(await this.request("/api/opl/state?profile=fast", { signal }));
    this.csrfToken = session.csrfToken;
    return agents;
  }

  async listThreads(signal?: AbortSignal): Promise<Conversation[]> {
    const value = await this.post("/api/threads/list", {}, signal);
    if (!object(value) || !Array.isArray(value.data)) return invalid("Runtime 未返回原生线程目录 data");
    return value.data.map((thread: unknown) => {
      if (!object(thread) || !nonempty(thread.id) || typeof thread.summary !== "string") {
        return invalid("Runtime 线程目录条目无效");
      }
      return { id: thread.id, title: thread.summary };
    });
  }

  async readThread(threadId: string, signal?: AbortSignal): Promise<ApiRun[]> {
    return readNativeThread(await this.post("/api/threads/read", { threadId, includeTurns: true }, signal), threadId);
  }

  async sendMessage(agent: PackageDescriptor, question: string, threadId?: string): Promise<ApiRun> {
    const value = await this.post("/api/send-message", {
      prompt: question,
      ...(threadId !== undefined ? { threadId } : { agentSelection: agent.selection }),
    });
    if (!object(value) || value.executor !== "codex_app_server"
      || !nonempty(value.threadId) || !nonempty(value.turnId)
      || (threadId !== undefined && threadId !== value.threadId)) {
      return invalid("Runtime 未确认当前会话与回答身份");
    }
    const runs = readNativeThread(value.canonicalThread, value.threadId);
    const canonical = runs.find(run => run.runId === value.turnId);
    if (!canonical) return invalid("Runtime canonical thread 缺少当前 turn");
    if (canonical.status === "succeeded") {
      const accepted = readAgentReply(value, threadId);
      if (accepted.result?.reply !== canonical.result?.reply) {
        return invalid("Runtime finalMessage 与原生历史正文不一致");
      }
      return { ...accepted, question, packageId: agent.packageId };
    }
    return { ...canonical, question, packageId: agent.packageId };
  }

  async renameThread(id: string, title: string): Promise<void> {
    const value = await this.post("/api/threads/rename", { threadId: id, name: title });
    if (!object(value) || value.id !== id) return invalid("Runtime 未确认重命名的线程身份");
  }

  async deleteThread(id: string): Promise<void> {
    const value = await this.post("/api/threads/delete", { threadId: id, confirmed: true });
    if (!object(value) || value.threadId !== id || value.deleted !== true) {
      return invalid("Runtime 未确认线程删除结果");
    }
  }
}
