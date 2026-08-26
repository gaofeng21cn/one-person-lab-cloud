import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession, SourceEnvelope, WorkspaceDTO } from "../api/dtos.ts";
import {
  findWorkspaceInPages,
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
  flash,
  mutationError
}: WorkspaceRenewalDependencies): WorkspaceRenewalCapability {
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<WorkspaceRenewalIssue>("");
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
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    requestGeneration.current += 1;
    setBusy(false);
    setIssue(issues.current.get(activeWorkspaceId) ?? "");
  }, [activeWorkspaceId]);

  useEffect(() => {
    const current = workspace ? intents.current.get(workspace.id) : undefined;
    if (!busy && current && workspace
      && current.workspaceId === workspace.id
      && current.autoRenew === workspace.autoRenew) {
      intents.current.delete(workspace.id);
      issues.current.delete(workspace.id);
      setIssue("");
    }
  }, [busy, workspace?.autoRenew, workspace?.id]);

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

  const updateCurrentWorkspaceRenewal = useCallback(async (autoRenew: boolean): Promise<boolean> => {
    if (!session || !workspace || workspace.id !== activeWorkspaceId || busy || workspace.renewalStatus !== "active") return false;

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
  }, [activeWorkspaceId, busy, currentMutationRequest, flash, mutationError, onWorkspaceReadback, requestIsCurrent, requestOwnsActiveScope, session, workspace, workspaceDetailProjectionLease]);

  return { busy, issue, updateCurrentWorkspaceRenewal, reset };
}
