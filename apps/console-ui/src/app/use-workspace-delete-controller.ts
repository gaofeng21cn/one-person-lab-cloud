import { useCallback, useEffect, useRef, useState } from "react";

import type { AuthSession, WorkspaceDeletionDTO, WorkspaceDTO } from "../api/dtos.ts";
import {
  deleteWorkspace,
  findWorkspaceInPages,
  getWorkspaceDeletion,
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
  const [loading, setLoading] = useState(false);
  const [readback, setReadback] = useState<{ workspaceId: string; operation: WorkspaceDeletionDTO | null }>({ workspaceId: "", operation: null });
  const requestGeneration = useRef(0);
  const intents = useRef(new Map<string, WorkspaceDeleteIntent>());
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
    setBusy(false);
    setIssue("");
    setLoading(false);
    setReadback({ workspaceId: "", operation: null });
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    requestGeneration.current += 1;
    setBusy(false);
    setIssue("");
    setReadback({ workspaceId: "", operation: null });
    if (session && activeWorkspaceId) void refresh();
  }, [activeWorkspaceId, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    if (readback.workspaceId !== activeWorkspaceId || readback.operation?.status !== "pending" || loading || busy || issue) return;
    const timer = window.setTimeout(() => void refresh(), 2000);
    return () => window.clearTimeout(timer);
  }, [readback, activeWorkspaceId, loading, busy, issue]);

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
      intents.current.delete(workspaceId);
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

  const readDeletion = async (
    workspaceId: string,
    generation: number,
    requestStillCurrent: () => boolean,
    userId: string,
    csrfToken: string,
    expectOperation = false
  ) => {
    setLoading(true);
    try {
      const operation = await getWorkspaceDeletion(workspaceId);
      if (expectOperation && operation === null) throw new Error("workspace_deletion_unconfirmed");
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      setReadback({ workspaceId, operation });
      setIssue("");
      if (operation?.status === "deleted") await confirmReadback(workspaceId, generation, requestStillCurrent, userId, csrfToken);
    } catch {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        setReadback((current) => ({ workspaceId, operation: current.workspaceId === workspaceId ? current.operation : null }));
        setIssue("unconfirmed");
      }
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setLoading(false);
    }
  };

  const refresh = async () => {
    if (!session || !activeWorkspaceId || busy) return;
    await readDeletion(activeWorkspaceId, ++requestGeneration.current, currentMutationRequest(), session.user.id, session.csrfToken);
  };

  const deleteCurrentWorkspace = async () => {
    if (!session || !workspace || workspace.id !== activeWorkspaceId || busy || loading
      || readback.workspaceId !== activeWorkspaceId || readback.operation || issue === "unconfirmed") return;
    if (!window.confirm(`确认删除工作空间“${workspace.name || workspace.id}”？请先自行下载需要的数据。删除后数据无法恢复，关闭页面后仍会继续处理，不会自动退款。`)) return;

    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const workspaceId = workspace.id;
    const generation = ++requestGeneration.current;
    const resolved = resolveWorkspaceDeleteIntent(
      intents.current.get(workspaceId) ?? null,
      workspaceId,
      () => workspaceDeleteIdempotencyKey(workspaceId)
    );
    intents.current.set(workspaceId, resolved);
    setBusy(true);
    setIssue("");

    try {
      const result = await deleteWorkspace(workspaceId, csrfToken, resolved.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (!result.available) {
        if (intents.current.get(workspaceId) === resolved) intents.current.delete(workspaceId);
        setIssue("unavailable");
        flash("Workspace 删除暂不可用", "danger");
        return;
      }
      await readDeletion(workspaceId, generation, requestStillCurrent, userId, csrfToken, true);
    } catch (error) {
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (isWorkspaceDeleteNotFound(error)) {
        await readDeletion(workspaceId, generation, requestStillCurrent, userId, csrfToken);
        return;
      }
      if (!shouldRetainWorkspaceDeleteIntent(error) && intents.current.get(workspaceId) === resolved) {
        intents.current.delete(workspaceId);
      }
      setIssue("unconfirmed");
      flash(friendlyError(error), "danger");
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setBusy(false);
    }
  };

  return { busy, issue, loading: loading || Boolean(activeWorkspaceId && readback.workspaceId !== activeWorkspaceId), operation: readback.workspaceId === activeWorkspaceId ? readback.operation : null, refresh, deleteCurrentWorkspace, reset };
}
