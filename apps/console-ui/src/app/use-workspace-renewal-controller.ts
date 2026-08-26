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
import type { WorkspaceRenewalController } from "./console-controller-types.ts";

interface WorkspaceRenewalDependencies {
  session: AuthSession | null;
  workspace: WorkspaceDTO | null;
  activeWorkspaceId: string;
  currentMutationRequest: () => () => boolean;
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
  onWorkspaceReadback,
  flash,
  mutationError
}: WorkspaceRenewalDependencies): WorkspaceRenewalCapability {
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<WorkspaceRenewalIssue>("");
  const requestGeneration = useRef(0);
  const intent = useRef<WorkspaceRenewalIntent | null>(null);
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
    intent.current = null;
    setBusy(false);
    setIssue("");
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    reset();
    return reset;
  }, [activeWorkspaceId, reset]);

  useEffect(() => {
    const current = intent.current;
    if (!busy && current && workspace
      && current.workspaceId === workspace.id
      && current.autoRenew === workspace.autoRenew) {
      intent.current = null;
      setIssue("");
    }
  }, [busy, workspace?.autoRenew, workspace?.id]);

  const requestIsCurrent = useCallback((
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

  const updateCurrentWorkspaceRenewal = useCallback(async (autoRenew: boolean): Promise<boolean> => {
    if (!session || !workspace || workspace.id !== activeWorkspaceId || busy || workspace.renewalStatus !== "active") return false;

    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const workspaceId = workspace.id;
    let currentIntent = intent.current;
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
    intent.current = currentIntent;
    const generation = ++requestGeneration.current;
    setBusy(true);
    setIssue("");

    try {
      const response = await updateWorkspaceRenewal(workspaceId, { autoRenew }, csrfToken, currentIntent.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
      if (!workspaceRenewalResponseMatches(response, autoRenew)) {
        throw new Error("workspace_renewal_response_mismatch");
      }

      const readback = await findWorkspaceInPages(workspaceId);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
      if (!workspaceRenewalReadbackMatches(readback, workspaceId, response)) {
        throw new Error("workspace_renewal_readback_mismatch");
      }

      intent.current = null;
      setIssue("");
      onWorkspaceReadback?.(readback);
      flash(autoRenew ? "自动续费已开启" : "自动续费已关闭");
      return true;
    } catch (error) {
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
      if (!shouldRetainWorkspaceRenewalIntent(error) && intent.current === currentIntent) intent.current = null;
      setIssue("unconfirmed");
      flash(mutationError(error), "danger");
      return false;
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setBusy(false);
    }
  }, [activeWorkspaceId, busy, currentMutationRequest, flash, mutationError, onWorkspaceReadback, requestIsCurrent, session, workspace]);

  return { busy, issue, updateCurrentWorkspaceRenewal, reset };
}
