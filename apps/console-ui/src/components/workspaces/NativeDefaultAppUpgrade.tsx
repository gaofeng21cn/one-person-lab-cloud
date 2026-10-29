import { useEffect, useRef, useState } from "react";
import { Button } from "../ui/index.ts";
import { readNativeAppUpgradeContext, updateNativeDefaultApp, type NativeAppUpgradeContext, type NativeAppUpgradeIntent } from "../../api/workspace-app-upgrade-api.ts";
import type { WorkspaceOwnerOperationDTO } from "../../api/dtos.ts";

export interface NativeDefaultAppUpgradeProps {
  workspaceId: string;
  actorId: string;
  csrfToken: string;
  onUpgraded?: () => void | Promise<void>;
}

// Mount only in the native customer's Workspace detail. The scope key discards
// a previous account/Workspace's pending UI state and aborts its requests.
export function NativeDefaultAppUpgrade(props: NativeDefaultAppUpgradeProps) {
  return <UpgradePanel key={JSON.stringify([props.actorId, props.workspaceId, props.csrfToken])} {...props} />;
}

function UpgradePanel({ workspaceId, csrfToken, onUpgraded }: NativeDefaultAppUpgradeProps) {
  const [context, setContext] = useState<NativeAppUpgradeContext | null>(null);
  const [target, setTarget] = useState("");
  const [operation, setOperation] = useState<WorkspaceOwnerOperationDTO | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const intent = useRef<NativeAppUpgradeIntent | null>(null);
  const lifecycle = useRef<AbortController | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const completed = useRef(false);
  const inFlight = useRef(false);
  const operationId = useRef("");
  const completion = useRef(onUpgraded);
  completion.current = onUpgraded;

  useEffect(() => {
    const controller = new AbortController();
    lifecycle.current = controller;
    return () => { controller.abort(); if (timer.current) clearTimeout(timer.current); };
  }, [workspaceId]);

  async function chooseVersion() {
    const signal = lifecycle.current?.signal;
    if (!signal || signal.aborted || busy || context) return;
    setBusy(true); setError("");
    try {
      const value = await readNativeAppUpgradeContext(workspaceId, signal);
      if (!signal.aborted) setContext(value);
    } catch (reason: unknown) {
      if (!signal.aborted) setError(reason instanceof Error ? reason.message : "读取应用版本失败");
    } finally { if (!signal.aborted) setBusy(false); }
  }

  async function advance() {
    const signal = lifecycle.current?.signal;
    if (!intent.current || !signal || signal.aborted || inFlight.current) return;
    inFlight.current = true;
    setBusy(true); setError("");
    try {
      const value = await updateNativeDefaultApp(intent.current, csrfToken, signal);
      if (signal.aborted) return;
      if (operationId.current && operationId.current !== value.operationId) throw new Error("升级操作身份与原命令不一致");
      operationId.current = value.operationId;
      setOperation(value);
      if (value.status === "succeeded") {
        if (!completed.current) { completed.current = true; await completion.current?.(); }
      } else if (["accepted", "running", "awaiting_confirmation"].includes(value.status)) {
        // Same command, same key: Serve resumes its frozen replacement and reads
        // actual readiness. A GET-only Operation read cannot advance that work.
        timer.current = setTimeout(() => void advance(), value.pollAfterSeconds! * 1000);
      }
    } catch (reason: unknown) {
      if (!signal.aborted) setError(reason instanceof Error ? reason.message : "升级结果待确认，请继续同一次升级");
    } finally { inFlight.current = false; if (!signal.aborted) setBusy(false); }
  }

  function submit() {
    if (!context || !target || busy || completed.current) return;
    intent.current ??= { workspaceId, expectedCurrentDeploymentId: context.currentDeploymentId, runtimeVersionId: target, idempotencyKey: `workspace-app-upgrade:${crypto.randomUUID()}` };
    void advance();
  }
  const pending = operation && ["accepted", "running", "awaiting_confirmation"].includes(operation.status);
  const terminal = operation && ["succeeded", "failed", "needs_attention", "cancelled"].includes(operation.status);
  return <section className="workspace-settings-panel" aria-label="默认 App 版本升级">
    <h3>升级默认 App</h3>
    <p>选择已批准的应用版本。升级期间应用会短暂中断，原工作空间、数据和密钥保持原有身份。</p>
    {!context ? <Button busy={busy} onClick={() => void chooseVersion()}>选择升级版本</Button> : null}
    {context ? <label className="console-field"><span className="console-field__label">目标版本</span>
      <select aria-label="目标 App 版本" value={target} disabled={busy || intent.current !== null} onChange={(event) => setTarget(event.currentTarget.value)}>
        <option value="">请选择版本</option>
        {context.releases.map((release) => <option key={release.id} value={release.id}>{release.name} · {release.versionLabel}</option>)}
      </select>
    </label> : null}
    {context && !context.releases.length ? <p role="status">当前没有获批的默认 App 版本。</p> : null}
    {error ? <p role="alert">{error}</p> : null}
    {operation ? <p role="status">{operation.status === "succeeded" ? "应用升级已完成" : operation.status === "failed" ? "应用升级失败，请核对原操作" : operation.status === "needs_attention" ? "升级需要管理员核对原操作" : operation.status === "cancelled" ? "应用升级已取消" : "正在升级，等待新应用就绪"}</p> : null}
    {context && !terminal ? <Button busy={busy} disabled={!target || !csrfToken || Boolean(pending && !error)} onClick={submit}>{intent.current ? "继续同一次升级" : "升级应用"}</Button> : null}
  </section>;
}
