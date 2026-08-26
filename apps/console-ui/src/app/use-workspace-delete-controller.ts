import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession, WorkspaceDTO } from "../api/dtos.ts";
import {
  deleteWorkspace,
  findWorkspaceInPages,
  workspaceDeleteIdempotencyKey
} from "../api/workspaces-api.ts";
import {
  isWorkspaceDeleteNotFound,
  resolveWorkspaceDeleteIntent,
  shouldRetainWorkspaceDeleteIntent,
  workspaceDeleteReadbackConfirmed,
  type WorkspaceDeleteIntent,
  type WorkspaceDeleteIssue
} from "./workspace-delete-controller-model.ts";
import type { WorkspaceDeleteController } from "./console-controller-types.ts";

interface WorkspaceDeleteDependencies {
  session: AuthSession | null;
  workspace: WorkspaceDTO | null;
  activeWorkspaceId: string;
  currentMutationRequest: () => () => boolean;
  navigate: (path: string) => void;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
}

export interface WorkspaceDeleteCapability extends WorkspaceDeleteController {
  reset: () => void;
}

export function useWorkspaceDeleteController({
  session,
  workspace,
  activeWorkspaceId,
  currentMutationRequest,
  navigate,
  flash,
  friendlyError
}: WorkspaceDeleteDependencies): WorkspaceDeleteCapability {
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<WorkspaceDeleteIssue>("");
  const requestGeneration = useRef(0);
  const intent = useRef<WorkspaceDeleteIntent | null>(null);
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

  const requestIsCurrent = (
    generation: number,
    requestStillCurrent: () => boolean,
    userId: string,
    csrfToken: string,
    workspaceId: string
  ) => generation === requestGeneration.current
    && requestStillCurrent()
    && scope.current.userId === userId
    && scope.current.csrfToken === csrfToken
    && scope.current.workspaceId === workspaceId;

  const confirmReadback = async (
    workspaceId: string,
    generation: number,
    requestStillCurrent: () => boolean,
    userId: string,
    csrfToken: string
  ): Promise<boolean> => {
    try {
      const readback = await findWorkspaceInPages(workspaceId);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
      if (!workspaceDeleteReadbackConfirmed(readback)) {
        setIssue("unconfirmed");
        flash("删除结果尚未获得权威回读确认", "danger");
        return false;
      }
      intent.current = null;
      setIssue("");
      flash("Workspace 已删除");
      navigate("/console/workspaces");
      return true;
    } catch {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        setIssue("unconfirmed");
        flash("删除结果尚未获得权威回读确认", "danger");
      }
      return false;
    }
  };

  const deleteCurrentWorkspace = async () => {
    if (!session || !workspace || workspace.id !== activeWorkspaceId || busy) return;
    if (!window.confirm(`确认删除 Workspace “${workspace.name || workspace.id}”？`)) return;

    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const workspaceId = workspace.id;
    const generation = ++requestGeneration.current;
    const resolved = resolveWorkspaceDeleteIntent(
      intent.current,
      workspaceId,
      () => workspaceDeleteIdempotencyKey(workspaceId)
    );
    intent.current = resolved;
    setBusy(true);
    setIssue("");

    try {
      const result = await deleteWorkspace(workspaceId, csrfToken, resolved.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (!result.available) {
        intent.current = null;
        setIssue("unavailable");
        flash("Workspace 删除暂不可用", "danger");
        return;
      }
      await confirmReadback(workspaceId, generation, requestStillCurrent, userId, csrfToken);
    } catch (error) {
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (isWorkspaceDeleteNotFound(error)) {
        await confirmReadback(workspaceId, generation, requestStillCurrent, userId, csrfToken);
        return;
      }
      if (!shouldRetainWorkspaceDeleteIntent(error)) intent.current = null;
      setIssue("unconfirmed");
      flash(friendlyError(error), "danger");
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setBusy(false);
    }
  };

  return { busy, issue, deleteCurrentWorkspace, reset };
}
