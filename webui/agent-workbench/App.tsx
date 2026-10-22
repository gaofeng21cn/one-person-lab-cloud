import { AssistantRuntimeProvider, MessageNotSentError, useExternalStoreRuntime, type AppendMessage } from "@assistant-ui/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { WorkbenchChatRoute } from "./routes/workbench/chat";
import { OplNativeRuntime } from "./state/workbench/oplNativeRuntime";
import { ApiHttpError, ApiResultError, isActiveRun, messagesFromRuns, needsStatusReadback, readWorkbenchConfig, uiFailure, type AgentConfig, type ApiRun, type Conversation, type PackageDescriptor, type UiFailure, type WorkbenchConfig } from "./state/workbench/remoteModelAdapter";

const selectionKey = "opl.agent-workbench.thread.v1";
const agentKey = "opl.agent-workbench.agent.v1";
const submissionKey = "opl.agent-workbench.unconfirmed-submission.v1";
const unconfirmedSubmission: UiFailure = {
  kind: "submission_unknown",
  message: "上次提交是否完成尚未确认。请查看并选择 Runtime 会话记录，或明确开始新对话；重连不代表原任务已取消。",
};
export function App() {
  const service = useMemo(() => new OplNativeRuntime(), []);
  const [config, setConfig] = useState<WorkbenchConfig | null>(null);
  const [agentConfig, setAgentConfig] = useState<AgentConfig | null>(null);
  const [packages, setPackages] = useState<PackageDescriptor[]>([]);
  const [modelReady, setModelReady] = useState(false);
  const [threads, setThreads] = useState<Conversation[]>([]);
  const [runs, setRuns] = useState<ApiRun[]>([]);
  const [threadId, setThreadId] = useState<string>();
  const [connectionError, setConnectionError] = useState<UiFailure | null>(null);
  const [restoring, setRestoring] = useState(true);
  const [sending, setSending] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const inFlight = useRef(false);
  const selectionVersion = useRef(0);
  const currentRun = runs.at(-1) ?? null;
  const select = useCallback((id?: string) => {
    selectionVersion.current += 1;
    // Explicit navigation acknowledges the unresolved request; reconnect never does.
    sessionStorage.removeItem(submissionKey);
    setThreadId(id); setRuns([]); setConnectionError(null); setRestoring(false);
    if (id) sessionStorage.setItem(selectionKey, id); else sessionStorage.removeItem(selectionKey);
  }, []);
  const applyConfig = useCallback((next: AgentConfig) => {
    if (inFlight.current) return;
    setAgentConfig(next); sessionStorage.setItem(agentKey, next.choiceId); select();
  }, [select]);

  useEffect(() => {
    const controller = new AbortController();
    const version = ++selectionVersion.current;
    setRestoring(true);
    void (async () => {
      try {
        const response = await fetch("/webui-config.json", { signal: controller.signal, credentials: "same-origin", redirect: "error" });
        if (!response.ok) throw new Error(`WebUI 配置读取失败：HTTP ${response.status}`);
        const next = readWorkbenchConfig(await response.json());
        const catalog = await service.connect(controller.signal);
        const history = await service.listThreads(controller.signal);
        if (controller.signal.aborted || version !== selectionVersion.current) return;
        setConfig(next); setPackages(catalog.packages); setModelReady(catalog.modelReady); setThreads(history);
        const savedAgent = sessionStorage.getItem(agentKey);
        const selected = catalog.packages.find(item => item.choiceId === savedAgent) ?? (catalog.packages.length === 1 ? catalog.packages[0] : undefined);
        setAgentConfig(selected ? { choiceId: selected.choiceId, packageId: selected.packageId, packageName: selected.name, sidebarTitle: next.sidebarTitle, assistantTitle: next.assistantTitle ?? selected.name, ...(next.notice ? { notice: next.notice } : {}) } : null);
        const savedThread = sessionStorage.getItem(selectionKey);
        const unconfirmed = sessionStorage.getItem(submissionKey) !== null;
        if (unconfirmed) {
          // Leave history selectable, including a previously active continuation.
          setThreadId(undefined); setRuns([]);
        } else if (savedThread && history.some(item => item.id === savedThread)) {
          const restored = await service.readThread(savedThread, controller.signal);
          if (controller.signal.aborted || version !== selectionVersion.current) return;
          setThreadId(savedThread); setRuns(restored);
        }
        setConnectionError(unconfirmed ? unconfirmedSubmission : null);
      } catch (error) { if (!controller.signal.aborted && version === selectionVersion.current) setConnectionError(uiFailure("status_read", error)); }
      finally { if (!controller.signal.aborted && version === selectionVersion.current) setRestoring(false); }
    })();
    return () => controller.abort();
  }, [service, refresh]);

  useEffect(() => { document.title = agentConfig?.assistantTitle || config?.sidebarTitle || "Agent Workbench"; }, [agentConfig?.assistantTitle, config?.sidebarTitle]);

  const switchThread = useCallback(async (id?: string) => {
    if (inFlight.current || runs.some(isActiveRun)) return;
    select(id);
    if (!id) return;
    const version = selectionVersion.current;
    setRestoring(true);
    try {
      const restored = await service.readThread(id);
      if (version === selectionVersion.current) setRuns(restored);
    } catch (error) { if (version === selectionVersion.current) setConnectionError(uiFailure("status_read", error)); }
    finally { if (version === selectionVersion.current) setRestoring(false); }
  }, [service, runs, select]);

  // Runtime events trigger authoritative thread readback; event text is never a completed answer.
  useEffect(() => {
    if (!threadId || sending || restoring) return;
    const events = new EventSource("/api/opl-events");
    const controller = new AbortController();
    let reading = false;
    let dirty = false;
    const version = selectionVersion.current;
    const readLatest = async () => {
      if (reading) return;
      reading = true;
      try {
        do {
          dirty = false;
          const value = await service.readThread(threadId, controller.signal);
          if (controller.signal.aborted || version !== selectionVersion.current) return;
          setRuns(value);
          setConnectionError(previous => sessionStorage.getItem(submissionKey) !== null
            ? unconfirmedSubmission : previous?.kind === "status_read" ? null : previous);
        } while (dirty);
      } catch (error) {
        if (!controller.signal.aborted && version === selectionVersion.current) setConnectionError(uiFailure("status_read", error));
      } finally {
        reading = false;
        if (dirty && !controller.signal.aborted && version === selectionVersion.current) void readLatest();
      }
    };
    // Reconcile on subscription/reconnection, including changes while disconnected.
    events.onopen = () => { dirty = true; void readLatest(); };
    events.onmessage = (event) => {
      let payload: { params?: { threadId?: string } };
      try { payload = JSON.parse(event.data); } catch { return; }
      if (payload.params?.threadId !== threadId) return;
      dirty = true;
      void readLatest();
    };
    return () => { controller.abort(); events.close(); };
  }, [threadId, sending, restoring, service, refresh]);

  const newMessage = async (message: AppendMessage) => {
    if (inFlight.current || !agentConfig || !modelReady || restoring || runs.some(isActiveRun) || needsStatusReadback(connectionError)) throw new MessageNotSentError("当前尚不能发送消息");
    const agent = packages.find(item => item.choiceId === agentConfig.choiceId);
    if (!agent) throw new MessageNotSentError("请选择已部署的 Agent");
    if (message.content.some(item => item.type !== "text")) throw new MessageNotSentError("当前 Runtime 适配器仅支持文本输入");
    const question = message.content.filter(item => item.type === "text").map(item => item.text).join("\n").trim();
    if (!question) throw new MessageNotSentError("请输入消息");
    const pendingId = `pending:${crypto.randomUUID()}`;
    // Only navigation identity is stored, never prompt text or credentials.
    sessionStorage.setItem(submissionKey, threadId ?? "new");
    selectionVersion.current += 1;
    inFlight.current = true; setSending(true); setConnectionError(null);
    setRuns(previous => [...previous, { runId: pendingId, threadId, question, status: "running", progress: { phase: "generation" } }]);
    try {
      const result = await service.sendMessage(agent, question, threadId);
      sessionStorage.removeItem(submissionKey);
      setRuns(previous => previous.map(item => item.runId === pendingId ? result : item));
      setThreadId(result.threadId!); sessionStorage.setItem(selectionKey, result.threadId!);
      try { setThreads(await service.listThreads()); }
      catch (error) { setConnectionError(uiFailure("status_read", error)); }
    } catch (error) {
      if (error instanceof ApiHttpError && [400, 422].includes(error.status)) sessionStorage.removeItem(submissionKey);
      setRuns(previous => previous.map(item => item.runId === pendingId ? { ...item, status: "failed", error: { code: "request_unconfirmed", message: error instanceof Error ? error.message : String(error) } } : item));
      setConnectionError(uiFailure(error instanceof ApiHttpError && [400, 422].includes(error.status) ? "input_rejected" : error instanceof ApiResultError ? "result_invalid" : "submission_unknown", error));
      throw error;
    } finally { inFlight.current = false; setSending(false); }
  };
  const messages = useMemo(() => messagesFromRuns(runs), [runs]);
  const blocked = !modelReady || !agentConfig || sending || restoring || runs.some(isActiveRun) || needsStatusReadback(connectionError);
  const runtime = useExternalStoreRuntime({
    messages, convertMessage: message => message, isRunning: sending || runs.some(isActiveRun), isLoading: restoring,
    isSendDisabled: blocked, onNew: newMessage,
    adapters: { threadList: { threadId, isLoading: restoring, threads: threads.map(item => ({ ...item, status: "regular" as const })),
      onSwitchToNewThread: () => { void switchThread(); }, onSwitchToThread: id => { void switchThread(id); },
      onRename: async (id, title) => {
        if (inFlight.current) return;
        try { await service.renameThread(id, title); setThreads(await service.listThreads()); }
        catch (error) { setConnectionError(uiFailure("status_read", error)); }
      },
      onDelete: async id => {
        if (inFlight.current || runs.some(isActiveRun)) return;
        try { await service.deleteThread(id); setThreads(await service.listThreads()); if (threadId === id) select(); }
        catch (error) { setConnectionError(uiFailure("status_read", error)); }
      },
    } },
  });
  const shownConfig = agentConfig ?? (config ? { choiceId: "", packageId: "", packageName: "", sidebarTitle: config.sidebarTitle, assistantTitle: config.assistantTitle ?? "选择 Agent 开始对话", ...(config.notice ? { notice: config.notice } : {}) } : null);
  return <AssistantRuntimeProvider runtime={runtime}>
    {!restoring && !modelReady && !connectionError && <div className="runtime-notice" role="status">Runtime 尚未配置模型访问，请先在应用中完成模型配置。</div>}
    {!restoring && packages.length === 0 && !connectionError && <div className="runtime-notice" role="status">Runtime 尚未提供可用的 Agent。</div>}
    <WorkbenchChatRoute key={threadId ?? "new"} agentConfig={shownConfig} packages={packages} onAgentConfigChange={applyConfig}
      run={currentRun} restoring={restoring || sending || !modelReady || !agentConfig} connectionError={connectionError} canCancel={false}
      onReconnect={() => { if (!inFlight.current) setRefresh(value => value + 1); }} />
  </AssistantRuntimeProvider>;
}
