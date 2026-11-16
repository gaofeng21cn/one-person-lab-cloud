import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession, WorkspaceRenewalReadDTO } from "../api/dtos.ts";
import { getWorkspaceRenewal } from "../api/workspaces-api.ts";
import type { WorkspaceRenewalIssue } from "./workspace-renewal-controller-model.ts";
import type { WorkspaceRenewalController } from "./console-controller-types.ts";

interface WorkspaceRenewalDependencies {
  session: AuthSession | null;
  activeWorkspaceId: string;
  currentMutationRequest: () => () => boolean;
  onRecovered: () => Promise<void>;
}

// The renewal state is a read. The Control Plane's auto-renew command retired
// with the retained CP execution slice, so this controller reads the renewal
// state and the recovery condition and never mutates either.
export interface WorkspaceRenewalCapability extends WorkspaceRenewalController {
  reset: () => void;
}

export function useWorkspaceRenewalController({
  session,
  activeWorkspaceId,
  currentMutationRequest,
  onRecovered
}: WorkspaceRenewalDependencies): WorkspaceRenewalCapability {
  const [issue, setIssue] = useState<WorkspaceRenewalIssue>("");
  const [loading, setLoading] = useState(false);
  const [readback, setReadback] = useState<{ workspaceId: string; renewal: WorkspaceRenewalReadDTO | null }>({ workspaceId: "", renewal: null });
  const requestGeneration = useRef(0);
  const issues = useRef(new Map<string, WorkspaceRenewalIssue>());
  const scope = useRef({
    userId: session?.user.id || "",
    csrfToken: session?.csrfToken || "",
    workspaceId: activeWorkspaceId
  });
  scope.current = {
    userId: session?.user.id || "",
    csrfToken: session?.csrfToken || "",
    workspaceId: activeWorkspaceId
  };

  const reset = useCallback(() => {
    requestGeneration.current += 1;
    issues.current.clear();
    setIssue("");
    setLoading(false);
    setReadback({ workspaceId: "", renewal: null });
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    requestGeneration.current += 1;
    setIssue(issues.current.get(activeWorkspaceId) ?? "");
    setReadback({ workspaceId: "", renewal: null });
    if (session && activeWorkspaceId) void refresh();
  }, [activeWorkspaceId, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    if (readback.workspaceId !== activeWorkspaceId || readback.renewal?.recovery.state !== "pending" || loading || issue) return;
    const timer = window.setTimeout(() => void refresh(), 2000);
    return () => window.clearTimeout(timer);
  }, [readback, activeWorkspaceId, loading, issue]);

  const requestOwnsActiveScope = useCallback((
    generation: number,
    requestStillCurrent: () => boolean,
    userId: string,
    csrfToken: string,
    workspaceId: string
  ) => generation === requestGeneration.current
    && requestStillCurrent()
    && scope.current.userId === userId
    && scope.current.csrfToken === csrfToken
    && scope.current.workspaceId === workspaceId,
  []
  );

  const refresh = async () => {
    if (!session || !activeWorkspaceId) return;
    const generation = ++requestGeneration.current;
    const requestStillCurrent = currentMutationRequest();
    setLoading(true);
    try {
      // The renewal read is its own projection. It must not be discarded because
      // a Workspace detail read settled first: only a newer renewal read, a
      // changed session or a changed Workspace invalidates this answer.
      const renewal = await getWorkspaceRenewal(activeWorkspaceId);
      if (!requestOwnsActiveScope(generation, requestStillCurrent, session.user.id, session.csrfToken, activeWorkspaceId)) return;
      setReadback({ workspaceId: activeWorkspaceId, renewal });
      setIssue("");
      issues.current.delete(activeWorkspaceId);
      if (renewal.recovery.state === "not_required" && readback.renewal?.recovery.state === "pending") await onRecovered();
    } catch {
      if (requestOwnsActiveScope(generation, requestStillCurrent, session.user.id, session.csrfToken, activeWorkspaceId)) {
        setReadback((current) => ({ workspaceId: activeWorkspaceId, renewal: current.workspaceId === activeWorkspaceId ? current.renewal : null }));
        issues.current.set(activeWorkspaceId, "unconfirmed");
        setIssue("unconfirmed");
      }
    } finally {
      if (requestOwnsActiveScope(generation, requestStillCurrent, session.user.id, session.csrfToken, activeWorkspaceId)) setLoading(false);
    }
  };

  return { issue, loading: loading || Boolean(activeWorkspaceId && readback.workspaceId !== activeWorkspaceId), renewal: readback.workspaceId === activeWorkspaceId ? readback.renewal : null, refresh, reset };
}
