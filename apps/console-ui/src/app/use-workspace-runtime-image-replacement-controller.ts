import { useCallback, useEffect, useRef, useState } from "react";

import {
  createOperatorWorkspaceRuntimeImageReplacement,
  getOperatorWorkspaceRuntimeImageReplacement,
  getOperatorWorkspaceRuntimeImageReplacementPreview
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  OperatorWorkspaceRuntimeImagePreviewDTO,
  SourceEnvelope,
  WorkspaceRuntimeImageReplacementDTO
} from "../api/dtos.ts";
import type { WorkspaceRuntimeImageReplacementController, WorkspaceRuntimeImageReplacementIssue } from "./console-controller-types.ts";
import {
  isTerminalWorkspaceRuntimeImageReplacement,
  resolveWorkspaceRuntimeImageReplacementIntent,
  workspaceRuntimeImageReplacementIdempotencyKey,
  workspaceRuntimeImageReplacementReadbackMatches,
  type WorkspaceRuntimeImageReplacementIntent
} from "./workspace-runtime-image-replacement-controller-model.ts";

interface WorkspaceRuntimeImageReplacementDependencies {
  session: AuthSession | null;
  workspaceId: string;
  preview: SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO> | null;
  currentMutationRequest: () => () => boolean;
  refreshWorkspace: (workspaceId: string) => Promise<void>;
  refreshPreview: (workspaceId: string) => Promise<void>;
  flash: (text: string, tone?: "good" | "danger") => void;
  mutationError: (error: unknown) => string;
}

export interface WorkspaceRuntimeImageReplacementCapability extends WorkspaceRuntimeImageReplacementController {
  reset: () => void;
}

const pollDelayMs = 1_500;
const pollAttempts = 60;

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

export function useWorkspaceRuntimeImageReplacementController({
  session,
  workspaceId,
  preview,
  currentMutationRequest,
  refreshWorkspace,
  refreshPreview,
  flash,
  mutationError
}: WorkspaceRuntimeImageReplacementDependencies): WorkspaceRuntimeImageReplacementCapability {
  const [operation, setOperation] = useState<WorkspaceRuntimeImageReplacementDTO | null>(null);
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<WorkspaceRuntimeImageReplacementIssue>("");
  const requestGeneration = useRef(0);
  const intent = useRef<WorkspaceRuntimeImageReplacementIntent | null>(null);
  const scope = useRef({ userId: session?.user.id || "", csrfToken: session?.csrfToken || "", workspaceId });
  scope.current = { userId: session?.user.id || "", csrfToken: session?.csrfToken || "", workspaceId };

  const reset = useCallback(() => {
    requestGeneration.current += 1;
    intent.current = null;
    setOperation(null);
    setBusy(false);
    setIssue("");
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    requestGeneration.current += 1;
    intent.current = null;
    setOperation(null);
    setBusy(false);
    setIssue("");
  }, [workspaceId]);

  const requestIsCurrent = useCallback((generation: number, requestStillCurrent: () => boolean, userId: string, csrfToken: string, expectedWorkspaceId: string) => {
    return generation === requestGeneration.current
      && requestStillCurrent()
      && scope.current.userId === userId
      && scope.current.csrfToken === csrfToken
      && scope.current.workspaceId === expectedWorkspaceId;
  }, []);

  const refreshWorkspaceRuntimeImageReplacement = useCallback(async () => {
    const current = operation;
    if (!current?.operationId || !workspaceId || busy || isTerminalWorkspaceRuntimeImageReplacement(current.status)) return;
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const userId = session?.user.id || "";
    const csrfToken = session?.csrfToken || "";
    setBusy(true);
    try {
      const result = await getOperatorWorkspaceRuntimeImageReplacement(workspaceId, current.operationId);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      setOperation(result);
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        setIssue("unconfirmed");
        flash(mutationError(error), "danger");
      }
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setBusy(false);
    }
  }, [busy, currentMutationRequest, flash, mutationError, operation, requestIsCurrent, session?.csrfToken, session?.user.id, workspaceId]);

  const replaceWorkspaceRuntimeImage = useCallback(async (): Promise<boolean> => {
    const target = preview?.available ? preview.data : null;
    if (!session || !workspaceId || busy || !target?.canReplace || !target.targetImageDigest || !target.runtimeId) return false;
    const reason = "apply the active protected Workspace image release";
    if (!window.confirm(`确认将 Workspace ${workspaceId} 更新或回滚到当前默认版本？操作会重建 Runtime Pod，但保留 Compute、CBS、Attachment 和 Workspace URL。`)) return false;
    const requestStillCurrent = currentMutationRequest();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const generation = ++requestGeneration.current;
    const resolved = resolveWorkspaceRuntimeImageReplacementIntent(
      intent.current,
      workspaceId,
      target.targetImageDigest,
      reason,
      () => workspaceRuntimeImageReplacementIdempotencyKey(() => crypto.randomUUID())
    );
    intent.current = resolved;
    setBusy(true);
    setIssue("");
    try {
      let result = await createOperatorWorkspaceRuntimeImageReplacement(workspaceId, resolved.replacementImageDigest, resolved.reason, csrfToken, resolved.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
      setOperation(result);
      for (let attempt = 0; attempt < pollAttempts && !isTerminalWorkspaceRuntimeImageReplacement(result.status); attempt += 1) {
        await delay(pollDelayMs);
        if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
        result = await getOperatorWorkspaceRuntimeImageReplacement(workspaceId, result.operationId);
        if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
        setOperation(result);
      }
      if (!isTerminalWorkspaceRuntimeImageReplacement(result.status)) {
        setIssue("timeout");
        flash("镜像切换仍在处理中，请稍后刷新状态", "danger");
        return false;
      }
      if (result.status !== "succeeded") {
        setIssue("unconfirmed");
        flash(result.errorCode || "Workspace 镜像切换失败", "danger");
        return false;
      }
      const readback = await getOperatorWorkspaceRuntimeImageReplacementPreview(workspaceId);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)
        || !readback.available
        || !workspaceRuntimeImageReplacementReadbackMatches(readback.data, workspaceId, target.runtimeId, resolved.replacementImageDigest)) {
        setIssue("unconfirmed");
        flash("镜像切换已返回成功，但 Runtime 权威读回未确认", "danger");
        return false;
      }
      intent.current = null;
      setIssue("");
      await refreshWorkspace(workspaceId);
      await refreshPreview(workspaceId);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return false;
      flash("Workspace WebUI 镜像已更新 / 回滚");
      return true;
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        setIssue("unconfirmed");
        flash(mutationError(error), "danger");
      }
      return false;
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setBusy(false);
    }
  }, [busy, currentMutationRequest, flash, mutationError, preview, refreshPreview, refreshWorkspace, requestIsCurrent, session, workspaceId]);

  return { operation, busy, issue, replaceWorkspaceRuntimeImage, refreshWorkspaceRuntimeImageReplacement, reset };
}
