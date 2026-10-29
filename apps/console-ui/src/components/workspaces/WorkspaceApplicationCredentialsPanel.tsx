import { useEffect, useRef, useState } from "react";

import type { AuthSession } from "../../api/dtos.ts";
import { revealWorkspaceApplicationCredentials, type WorkspaceApplicationCredentials } from "../../api/workspace-application-credentials-api.ts";
import { Alert, Button } from "../ui/index.ts";

type Props = { session: AuthSession | null; workspaceId: string };
type State = Props & { busy?: boolean; credentials?: WorkspaceApplicationCredentials; error?: string };

// Scope changes hide the old value immediately, and abort cleanup prevents a
// late reply from restoring it. Credentials exist only in this component state.
export function WorkspaceApplicationCredentialsPanel({ session, workspaceId }: Props) {
  const [state, setState] = useState<State>();
  const pending = useRef<AbortController | null>(null);
  const current = state?.session === session && state.workspaceId === workspaceId ? state : undefined;
  useEffect(() => {
    setState({ session, workspaceId });
    const leave = () => {
      pending.current?.abort();
      pending.current = null;
      setState({ session, workspaceId });
    };
    window.addEventListener("pagehide", leave);
    return () => { pending.current?.abort(); pending.current = null; window.removeEventListener("pagehide", leave); };
  }, [session, workspaceId]);

  const hide = () => {
    pending.current?.abort();
    pending.current = null;
    setState({ session, workspaceId });
  };
  const reveal = async () => {
    if (!session || pending.current) return;
    const abort = new AbortController();
    pending.current = abort;
    setState({ session, workspaceId, busy: true });
    try {
      const credentials = await revealWorkspaceApplicationCredentials(workspaceId, session.csrfToken, abort.signal);
      if (!abort.signal.aborted) setState({ session, workspaceId, credentials });
    } catch {
      if (!abort.signal.aborted) setState({ session, workspaceId, error: "无法领取应用登录凭据，请确认所有者权限及应用运行状态后重试。" });
    } finally {
      if (pending.current === abort) pending.current = null;
    }
  };

  return <section className="panel" aria-label="应用登录凭据">
    <div className="panel-title"><h2>应用登录凭据</h2></div>
    {current?.error ? <Alert color="warning" title="应用登录凭据暂不可用" description={current.error} /> : null}
    {session ? <>
      <p>用于登录当前应用；隐藏或离开本页后清除。</p>
      {current?.credentials ? <dl className="data-list">
        <div><dt>登录用户名</dt><dd><code>{current?.credentials.username}</code></dd></div>
        <div><dt>登录密码</dt><dd><code>{current?.credentials.password}</code></dd></div>
      </dl> : null}
      <div className="workspace-actions">
        {current?.credentials || current?.busy ? <Button onClick={hide} variant="outline">隐藏登录凭据</Button> : <Button onClick={() => void reveal()} variant="outline">查看登录凭据</Button>}
        {current?.busy ? <span role="status">正在领取应用登录凭据…</span> : null}
      </div>
    </> : <p>请先登录。</p>}
  </section>;
}
