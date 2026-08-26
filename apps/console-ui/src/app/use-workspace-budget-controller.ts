import { useCallback, useEffect, useRef, useState } from "react";

import type {
  AuthSession,
  SourceEnvelope,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceGatewayBudgetUpdateRequest
} from "../api/dtos.ts";
import {
  getWorkspaceGatewayBudget,
  updateWorkspaceGatewayBudget
} from "../api/workspaces-api.ts";
import {
  resolveWorkspaceBudgetIntent,
  shouldRetainWorkspaceBudgetIntent,
  workspaceBudgetIdentityMatches,
  workspaceBudgetResultMatchesInput,
  type WorkspaceBudgetIntent
} from "./workspace-budget-controller-model.ts";
import type { WorkspaceBudgetController, WorkspaceSourceProjectionLease } from "./console-controller-types.ts";

interface WorkspaceBudgetDependencies {
  session: AuthSession | null;
  workspace: WorkspaceDTO | null;
  budget: SourceEnvelope<WorkspaceGatewayBudgetDTO> | null;
  activeWorkspaceId: string;
  currentMutationRequest: () => () => boolean;
  workspaceBudgetProjectionLease: () => WorkspaceSourceProjectionLease;
  updateBudgetSource: (value: SourceEnvelope<WorkspaceGatewayBudgetDTO>) => void;
  flash: (text: string, tone?: "good" | "danger") => void;
  mutationError: (error: unknown) => string;
}

export interface WorkspaceBudgetCapability extends WorkspaceBudgetController {
  reset: () => void;
}

export function useWorkspaceBudgetController({
  session,
  workspace,
  budget,
  activeWorkspaceId,
  currentMutationRequest,
  workspaceBudgetProjectionLease,
  updateBudgetSource,
  flash,
  mutationError
}: WorkspaceBudgetDependencies): WorkspaceBudgetCapability {
  const [busy, setBusy] = useState(false);
  const requestGeneration = useRef(0);
  const intents = useRef(new Map<string, WorkspaceBudgetIntent>());
  const busyClaim = useRef<symbol | null>(null);
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
    busyClaim.current = null;
    setBusy(false);
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    requestGeneration.current += 1;
    busyClaim.current = null;
    setBusy(false);
  }, [activeWorkspaceId]);

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
    && scope.current.workspaceId === workspaceId, []);

  const requestIsCurrent = useCallback((
    generation: number,
    requestStillCurrent: () => boolean,
    projectionLease: WorkspaceSourceProjectionLease,
    userId: string,
    csrfToken: string,
    workspaceId: string
  ) => projectionLease.isCurrent()
    && requestOwnsActiveScope(generation, requestStillCurrent, userId, csrfToken, workspaceId),
  [requestOwnsActiveScope]);

  const update = useCallback(async (input: WorkspaceGatewayBudgetUpdateRequest): Promise<boolean> => {
    const currentWorkspace = workspace;
    const currentBudget = budget;
    if (!session || !currentWorkspace || currentWorkspace.id !== activeWorkspaceId
      || !currentBudget?.available || !currentBudget.data
      || currentBudget.data.workspaceId !== currentWorkspace.id
      || busyClaim.current !== null) return false;

    const workspaceId = currentWorkspace.id;
    const keyId = currentBudget.data.keyId;
    const requestStillCurrent = currentMutationRequest();
    const projectionLease = workspaceBudgetProjectionLease();
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const generation = ++requestGeneration.current;
    const currentIntent = intents.current.get(workspaceId) ?? null;
    const nextIntent = resolveWorkspaceBudgetIntent(
      currentIntent,
      workspaceId,
      keyId,
      input,
      () => `workspace-gateway-budget:${workspaceId}:${crypto.randomUUID()}`
    );
    if (currentIntent && currentIntent.keyId === keyId && nextIntent !== currentIntent) {
      flash("上次模型预算更新结果待确认，请使用相同设置重试", "danger");
      return false;
    }
    intents.current.set(workspaceId, nextIntent);
    const claim = Symbol("workspace-budget");
    busyClaim.current = claim;
    setBusy(true);

    try {
      const response = await updateWorkspaceGatewayBudget(
        workspaceId,
        keyId,
        nextIntent.input,
        csrfToken,
        nextIntent.idempotencyKey
      );
      if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
      if (!response.available || !workspaceBudgetIdentityMatches(response.data, workspaceId, keyId)) {
        throw new Error("workspace_gateway_budget_response_mismatch");
      }
      if (!workspaceBudgetResultMatchesInput(response.data, nextIntent.input)) {
        if (!projectionLease.commit()) return false;
        if (intents.current.get(workspaceId) === nextIntent) intents.current.delete(workspaceId);
        updateBudgetSource(response);
        flash("模型预算已变化，请按最新状态重新提交", "danger");
        return false;
      }

      // The mutation response is only an acknowledgement. Read the owner projection again
      // before clearing the intent or showing success to the user.
      const readback = await getWorkspaceGatewayBudget(workspaceId, keyId);
      if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
      if (!readback.available || !workspaceBudgetIdentityMatches(readback.data, workspaceId, keyId)
        || !workspaceBudgetResultMatchesInput(readback.data, nextIntent.input)) {
        if (readback.available) {
          if (!projectionLease.commit()) return false;
          updateBudgetSource(readback);
        }
        flash(mutationError(new Error("workspace_gateway_budget_readback_mismatch")), "danger");
        return false;
      }
      if (!projectionLease.commit()) return false;
      if (intents.current.get(workspaceId) === nextIntent) intents.current.delete(workspaceId);
      updateBudgetSource(readback);
      flash("模型预算已更新");
      return true;
    } catch (error) {
      if (!requestIsCurrent(generation, requestStillCurrent, projectionLease, userId, csrfToken, workspaceId)) return false;
      if (!shouldRetainWorkspaceBudgetIntent(error) && intents.current.get(workspaceId) === nextIntent) {
        intents.current.delete(workspaceId);
      }
      flash(mutationError(error), "danger");
      return false;
    } finally {
      if (busyClaim.current === claim
        && requestOwnsActiveScope(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        busyClaim.current = null;
        setBusy(false);
      }
    }
  }, [activeWorkspaceId, budget, currentMutationRequest, flash, mutationError, requestIsCurrent, requestOwnsActiveScope, session, updateBudgetSource, workspace, workspaceBudgetProjectionLease]);

  return { busy, update, reset };
}
