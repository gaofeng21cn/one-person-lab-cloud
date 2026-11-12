import { useEffect, useState } from "react";

import type { AuthSession } from "../api/dtos.ts";
import type { ApiError } from "../api/console-api.ts";
import { getWorkspaceSubscription, type WorkspaceSubscription } from "../api/workspace-subscription-api.ts";

type ReadState =
  | { status: "loading" }
  | { status: "available"; subscription: WorkspaceSubscription }
  | { status: "absent" | "forbidden" | "unauthenticated" | "unavailable" };
type ScopedRead = { session: AuthSession; workspaceId: string; result: ReadState };

export function useWorkspaceSubscription(session: AuthSession | null, workspaceId: string) {
  const [read, setRead] = useState<ScopedRead>();
  const [revision, setRevision] = useState(0);
  // Platform administration grants no tenant financial authority.
  const allowed = !!session?.user.accountId && session.user.status === "active" && (session.user.role === "owner" || session.user.role === "admin");
  useEffect(() => {
    if (!session || !allowed || !workspaceId) {
      setRead(undefined);
      return;
    }
    const abort = new AbortController();
    setRead({ session, workspaceId, result: { status: "loading" } });
    void getWorkspaceSubscription(workspaceId, abort.signal).then((subscription) => {
      if (!abort.signal.aborted) setRead({ session, workspaceId, result: { status: "available", subscription } });
    }).catch((error: ApiError) => {
      if (abort.signal.aborted) return;
      const status = error.status === 404 ? "absent" : error.status === 403 ? "forbidden" : error.status === 401 ? "unauthenticated" : "unavailable";
      setRead({ session, workspaceId, result: { status } });
    });
    return () => abort.abort();
  }, [session, workspaceId, allowed, revision]);

  // Hide facts on the render that changes identity, before effect cleanup runs.
  const result: ReadState = !session ? { status: "unauthenticated" }
    : !allowed ? { status: "forbidden" }
    : read?.session === session && read.workspaceId === workspaceId ? read.result : { status: "loading" };
  return { ...result, refresh: () => setRevision((value) => value + 1) };
}
