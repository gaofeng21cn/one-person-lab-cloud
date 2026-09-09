import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession, SourceEnvelope, WorkspaceDTO, WorkspaceRenewalReadDTO } from "../api/dtos.ts";
import {
  findWorkspaceInPages,
  getWorkspaceRenewal,
  updateWorkspaceRenewal
} from "../api/workspaces-api.ts";
import {
  resolveWorkspaceRenewalIntent,
  shouldRetainWorkspaceRenewalIntent,
  workspaceRenewalReadbackMatches,
  workspaceRenewalResponseMatches,
  type WorkspaceRenewalIntent,
  type WorkspaceRenewalIssue
} from "./workspace-renewal-controller-model.ts";
import type { WorkspaceRenewalController, WorkspaceSourceProjectionLease } from "./console-controller-types.ts";

interface WorkspaceRenewalDependencies {
  session: AuthSession | null;
  workspace: WorkspaceDTO | null;
  activeWorkspaceId: string;
  currentMutationRequest: () => () => boolean;
  workspaceDetailProjectionLease: () => WorkspaceSourceProjectionLease;
  onWorkspaceReadback?: (readback: SourceEnvelope<WorkspaceDTO | null>) => void;
  onRecovered: () => Promise<void>;
  flash: (text: string, tone?: "good" | "danger") => void;
  mutationError: (error: unknown) => string;
}

export interface WorkspaceRenewalCapability extends WorkspaceRenewalController {
  reset: () => void;
}

export function useWorkspaceRenewalController({
  session,
  workspace,
  activeWorkspaceId,
  currentMutationRequest,
  workspaceDetailProjectionLease,
  onWorkspaceReadback,
  onRecovered,
  flash,
  mutationError
}: WorkspaceRenewalDependencies): WorkspaceRenewalCapability {
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<WorkspaceRenewalIssue>("");
  const [loading, setLoading] = useState(false);
  const [readback, setReadback] = useState<{ workspaceId: string; renewal: WorkspaceRenewalReadDTO | null }>({ workspaceId: "", renewal: null });
  const requestGeneration = useRef(0);
  const intents = useRef(new Map<string, WorkspaceRenewalIntent>());
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
    intents.current.clear();
    issues.current.clear();
    setBusy(false);
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
    setBusy(false);
    setIssue(issues.current.get(activeWorkspaceId) ?? "");
    setReadback({ workspaceId: "", renewal: null });
    if (session && activeWorkspaceId) void refresh();
  }, [activeWorkspaceId, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    if (readback.workspaceId !== activeWorkspaceId || readback.renewal?.recovery.state !== "pending" || loading || busy || issue) return;
    const timer = window.setTimeout(() => void refresh(), 2000);
    return () => window.clearTimeout(timer);
  }, [readback, activeWorkspaceId, loading, busy, issue]);

  useEffect(() => {
    const current = workspace ? intents.current.get(workspace.id) : undefined;
    if (!busy && current && workspace
      && current.workspaceId === workspace.id
      && readback.workspaceId === workspace.id && readback.renewal?.recovery.state === "not_required"
      && current.autoRenew === workspace.autoRenew) {
      intents.current.delete(workspace.id);
      issues.current.delete(workspace.id);
      setIssue("");
    }
  }, [busy, workspace?.autoRenew, workspace?.id, readback]);

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

  const requestIsCurrent = useCallback((
    generation: number,
    requestStillCurrent: () => boolean,
    projectionLease: WorkspaceSourceProjectionLease,
    userId: string,
    csrfToken: string,
    workspaceId: string
  ) => projectionLease.isCurrent()
    && requestOwnsActiveScope(generation, requestStillCurrent, userId, csrfToken, workspaceId),
  [requestOwnsActiveScope]
  );

  const refresh = async () => {
    if (!session || !activeWorkspaceId || busy) return;
    const generation = ++requestGeneration.current;
    const requestStillCurrent = currentMutationRequest();
    setLoading(true);
    try {
      const renewal = await getWorkspaceRenewal(activeWorkspaceId);
      if (!requestOwnsActiveScope(generation, requestStillCurrent, session.user.id, session.csrfToken, activeWorkspaceId)) return;
      setReadback({ workspaceId: activeWorkspaceId, renewal });
      setIssue("");
      issues.current.delete(activeWorkspaceId);
      if (renewal.recovery.state === "pending") intents.current.delete(activeWorkspaceId);
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

  const updateCurrentWorkspaceRenewal = useCallback(async (autoRenew: boolean): Promise<boolean> => {
    if (!session || !workspace || workspace.id !== activeWorkspaceId || busy || loading || readback.workspaceId !== activeWorkspaceId || !readback.renewal) return false;
    const recovering = readback.renewal.recovery.state === "recoverable";
    if (recovering ? !autoRenew : readback.renewal.recovery.state !== "not_required" || workspace.renewalStatus !== "active") return false;

    const requestStillCurrent = currentMutationRequest();
    const projectionLease = workspaceDetailProjectionLease();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const workspaceId = workspace.id;
    let currentIntent = intents.current.get(workspaceId);
    if (currentIntent && currentIntent.workspaceId === workspaceId && currentIntent.autoRenew !== autoRenew) {
      flash("上次自动续费更新结果待确认，请按原设置重试", "danger");
      return false;
    }
    currentIntent = resolveWorkspaceRenewalIntent(
      currentIntent,
      workspaceId,
      autoRenew,
      () => `workspace-renewal:${workspaceId}:${crypto.randomUUID()}`
    );
    intents.current.set(workspaceId, currentIntent);
    const generation = ++requestGeneration.current;
    setBusy(true);
    setIssue("");

    try {
      const response = await updateWorkspaceRenewal(workspaceId, { autoRenew }, csrfToken, currentIntent.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
      if (!workspaceRenewalResponseMatches(response, autoRenew)) {
        throw new Error("workspace_renewal_response_mismatch");
      }

      if (recovering) {
        const renewal = await getWorkspaceRenewal(workspaceId);
        if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
        setReadback({ workspaceId, renewal });
        if (renewal.recovery.state !== "pending" && renewal.recovery.state !== "not_required") throw new Error("workspace_renewal_recovery_unconfirmed");
        intents.current.delete(workspaceId);
        issues.current.delete(workspaceId);
        setIssue("");
        flash("续费请求已提交，请以工作空间状态确认恢复结果");
        if (renewal.recovery.state === "not_required") await onRecovered();
        return true;
      }

      const readback = await findWorkspaceInPages(workspaceId);
      if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
      if (!workspaceRenewalReadbackMatches(readback, workspaceId, currentIntent.autoRenew)) {
        throw new Error("workspace_renewal_readback_mismatch");
      }
      if (!projectionLease.commit()) return false;

      if (intents.current.get(workspaceId) === currentIntent) intents.current.delete(workspaceId);
      issues.current.delete(workspaceId);
      setIssue("");
      onWorkspaceReadback?.(readback);
      flash(autoRenew ? "自动续费已开启" : "自动续费已关闭");
      return true;
    } catch (error) {
      if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
      if (!shouldRetainWorkspaceRenewalIntent(error) && intents.current.get(workspaceId) === currentIntent) {
        intents.current.delete(workspaceId);
      }
      issues.current.set(workspaceId, "unconfirmed");
      setIssue("unconfirmed");
      flash(mutationError(error), "danger");
      return false;
    } finally {
      if (requestOwnsActiveScope(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setBusy(false);
    }
  }, [activeWorkspaceId, busy, loading, readback, currentMutationRequest, flash, mutationError, onRecovered, onWorkspaceReadback, requestIsCurrent, requestOwnsActiveScope, session, workspace, workspaceDetailProjectionLease]);

  return { busy, issue, loading: loading || Boolean(activeWorkspaceId && readback.workspaceId !== activeWorkspaceId), renewal: readback.workspaceId === activeWorkspaceId ? readback.renewal : null, refresh, updateCurrentWorkspaceRenewal, reset };
}
