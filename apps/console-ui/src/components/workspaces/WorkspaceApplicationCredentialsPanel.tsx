import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import type { AuthSession } from "../../api/dtos.ts";
import { revealWorkspaceApplicationCredentials, type WorkspaceApplicationCredentials } from "../../api/workspace-application-credentials-api.ts";
import { Alert, Button } from "../ui/index.ts";

const CREDENTIAL_TTL_SECONDS = 60;

type Props = { session: AuthSession | null; workspaceId: string };
type State = Props & { busy?: boolean; credentials?: WorkspaceApplicationCredentials; error?: string };

// Scope changes hide the old value immediately, and abort cleanup prevents a
// late reply from restoring it. Credentials exist only in this component state
// and auto-clear after 60 seconds.
export function WorkspaceApplicationCredentialsPanel({ session, workspaceId }: Props) {
  const [state, setState] = useState<State>();
  const [remainingSeconds, setRemainingSeconds] = useState(0);
  const [copiedField, setCopiedField] = useState<"" | "username" | "password">("");
  const pending = useRef<AbortController | null>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const current = state?.session === session && state.workspaceId === workspaceId ? state : undefined;

  useEffect(() => {
    setState({ session, workspaceId });
    setRemainingSeconds(0);
    setCopiedField("");
    const leave = () => {
      pending.current?.abort();
      pending.current = null;
      setState({ session, workspaceId });
      setRemainingSeconds(0);
    };
    window.addEventListener("pagehide", leave);
    return () => {
      pending.current?.abort();
      pending.current = null;
      if (copyTimer.current) clearTimeout(copyTimer.current);
      window.removeEventListener("pagehide", leave);
    };
  }, [session, workspaceId]);

  useEffect(() => {
    if (!current?.credentials) {
      setRemainingSeconds(0);
      return;
    }
    setRemainingSeconds(CREDENTIAL_TTL_SECONDS);
    const interval = window.setInterval(() => {
      setRemainingSeconds((prev) => {
        if (prev <= 1) {
          window.clearInterval(interval);
          setState({ session, workspaceId });
          setCopiedField("");
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => window.clearInterval(interval);
  }, [current?.credentials, session, workspaceId]);

  const hide = () => {
    pending.current?.abort();
    pending.current = null;
    setState({ session, workspaceId });
    setRemainingSeconds(0);
    setCopiedField("");
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
      if (!abort.signal.aborted) setState({ session, workspaceId, error: "无法领取应用登录凭据，请确认权限及应用运行状态后重试。" });
    } finally {
      if (pending.current === abort) pending.current = null;
    }
  };

  const copyField = async (field: "username" | "password", value: string) => {
    try {
      await navigator.clipboard?.writeText(value);
      setCopiedField(field);
      if (copyTimer.current) clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopiedField(""), 2000);
    } catch {
      // Clipboard unavailable in restricted context; user can still select text.
    }
  };

  return <section className="panel" aria-label="应用登录凭据">
    <div className="panel-title"><h2>应用登录凭据</h2></div>
    {current?.error ? <Alert color="warning" title="应用登录凭据暂不可用" description={current.error} /> : null}
    {session ? <>
      <p>{current?.credentials ? `当前页面临时显示，${remainingSeconds} 秒后自动隐藏。` : "用于登录应用；查看后 60 秒或离开页面会自动隐藏。"}</p>
      {current?.credentials ? <dl className="data-list workspace-credentials-list">
        <div>
          <dt>登录用户名</dt>
          <dd className="workspace-credential-value">
            <code>{current.credentials.username}</code>
            <Button aria-label="复制登录用户名" onClick={() => void copyField("username", current.credentials!.username)} size="sm" variant="ghost">
              {copiedField === "username" ? <><Check aria-hidden size={14} />已复制</> : <><Copy aria-hidden size={14} />复制</>}
            </Button>
          </dd>
        </div>
        <div>
          <dt>登录密码</dt>
          <dd className="workspace-credential-value">
            <code>{current.credentials.password}</code>
            <Button aria-label="复制登录密码" onClick={() => void copyField("password", current.credentials!.password)} size="sm" variant="ghost">
              {copiedField === "password" ? <><Check aria-hidden size={14} />已复制</> : <><Copy aria-hidden size={14} />复制</>}
            </Button>
          </dd>
        </div>
      </dl> : null}
      <div className="workspace-actions">
        {current?.credentials || current?.busy ? <Button onClick={hide} variant="outline">隐藏登录凭据</Button> : <Button onClick={() => void reveal()} variant="outline">查看登录凭据</Button>}
        {current?.busy ? <span role="status">正在领取应用登录凭据…</span> : null}
      </div>
    </> : <p>请先登录。</p>}
  </section>;
}
