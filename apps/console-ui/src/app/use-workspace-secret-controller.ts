import { useCallback, useEffect, useRef, useState } from "react";

import { revealGatewayKey } from "../api/console-read-api.ts";
import type { AuthSession, WorkspaceDTO } from "../api/dtos.ts";
import { revealWorkspaceCredentials, rotateWorkspaceCredentials } from "../api/workspaces-api.ts";
import type { WorkspaceSecretController } from "./console-controller-types.ts";
import {
  acceptWorkspaceSecretCompletion,
  resolveRuntimeRotationIntent,
  shouldExpireWorkspaceSecret,
  workspaceSecretLifetimeMs,
  type RuntimeRotationIntent,
  type WorkspaceSecretProjection
} from "./workspace-secret-controller-model.ts";

interface WorkspaceSecretDependencies {
  session: AuthSession | null;
  workspace: WorkspaceDTO | null;
  activeWorkspaceId: string;
  currentMutationRequest: () => () => boolean;
  refreshWorkspaceDetail: (workspaceId: string) => Promise<void>;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
  mutationError: (error: unknown) => string;
}

export interface WorkspaceSecretCapability extends WorkspaceSecretController {
  reset: () => void;
}

type SecretOperation = "" | "workspace" | "gateway-key";

const emptyProjection = (): WorkspaceSecretProjection => ({ apiKey: null, workspace: null });

export function useWorkspaceSecretController({
  session,
  workspace,
  activeWorkspaceId,
  currentMutationRequest,
  refreshWorkspaceDetail,
  flash,
  friendlyError,
  mutationError
}: WorkspaceSecretDependencies): WorkspaceSecretCapability {
  const [projection, setProjection] = useState<WorkspaceSecretProjection>(emptyProjection);
  const [operation, setOperation] = useState<SecretOperation>("");
  const requestGeneration = useRef(0);
  const timer = useRef<number | undefined>(undefined);
  const revealedAt = useRef<number | undefined>(undefined);
  const rotationIntent = useRef<RuntimeRotationIntent | null>(null);
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

  const clearTimer = useCallback(() => {
    if (timer.current !== undefined) window.clearTimeout(timer.current);
    timer.current = undefined;
    revealedAt.current = undefined;
  }, []);

  const clear = useCallback(() => {
    requestGeneration.current += 1;
    clearTimer();
    setProjection(emptyProjection());
    setOperation("");
  }, [clearTimer]);

  const reset = useCallback(() => {
    clear();
    rotationIntent.current = null;
  }, [clear]);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id]);

  useEffect(() => {
    clear();
    return clear;
  }, [activeWorkspaceId, clear]);

  const armTimeout = () => {
    clearTimer();
    const startedAt = Date.now();
    revealedAt.current = startedAt;
    const expire = () => {
      if (revealedAt.current !== startedAt) return;
      const now = Date.now();
      if (shouldExpireWorkspaceSecret(startedAt, now)) {
        clear();
        return;
      }
      timer.current = window.setTimeout(expire, workspaceSecretLifetimeMs - (now - startedAt));
    };
    timer.current = window.setTimeout(expire, workspaceSecretLifetimeMs);
  };

  const beginOperation = (next: SecretOperation) => {
    requestGeneration.current += 1;
    clearTimer();
    setProjection(emptyProjection());
    setOperation(next);
    return requestGeneration.current;
  };

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

  const revealWorkspacePassword = async () => {
    if (!session || !workspace || operation === "workspace") return;
    const { csrfToken } = session;
    const userId = session.user.id;
    const workspaceId = workspace.id;
    const requestStillCurrent = currentMutationRequest();
    const generation = beginOperation("workspace");
    try {
      const response = await revealWorkspaceCredentials(workspaceId, csrfToken);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (response.workspaceId !== workspaceId) throw new Error("workspace_credentials_unavailable");
      const accepted = acceptWorkspaceSecretCompletion(generation, requestGeneration.current, {
        kind: "runtime-credential",
        response
      });
      if (!accepted) return;
      setProjection(accepted);
      armTimeout();
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        flash(friendlyError(error), "danger");
      }
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setOperation("");
    }
  };

  const revealWorkspaceKey = async () => {
    if (!session || !workspace?.workspaceApiKeyId || operation === "gateway-key") return;
    const { csrfToken } = session;
    const userId = session.user.id;
    const workspaceId = workspace.id;
    const keyId = workspace.workspaceApiKeyId;
    const requestStillCurrent = currentMutationRequest();
    const generation = beginOperation("gateway-key");
    try {
      const response = await revealGatewayKey(keyId, csrfToken);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (!response.available || response.data.id !== keyId || !response.data.value) {
        throw new Error("gateway_key_unavailable");
      }
      const accepted = acceptWorkspaceSecretCompletion(generation, requestGeneration.current, {
        kind: "workspace-key",
        response: response.data
      });
      if (!accepted) return;
      setProjection(accepted);
      armTimeout();
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        flash(friendlyError(error), "danger");
      }
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setOperation("");
    }
  };

  const rotateWorkspacePassword = async () => {
    if (!session || !workspace || operation === "workspace") return;
    const { csrfToken } = session;
    const userId = session.user.id;
    const workspaceId = workspace.id;
    const requestStillCurrent = currentMutationRequest();
    const intent = resolveRuntimeRotationIntent(
      rotationIntent.current,
      workspaceId,
      () => `runtime-credential:${crypto.randomUUID()}`
    );
    rotationIntent.current = intent;
    const generation = beginOperation("workspace");
    try {
      const response = await rotateWorkspaceCredentials(workspaceId, csrfToken, intent.idempotencyKey);
      if (!requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) return;
      if (response.workspaceId !== workspaceId) throw new Error("workspace_credentials_unavailable");
      const accepted = acceptWorkspaceSecretCompletion(generation, requestGeneration.current, {
        kind: "runtime-credential",
        response
      });
      if (!accepted) return;
      rotationIntent.current = null;
      setProjection(accepted);
      armTimeout();
      flash("Workspace 凭证已轮换");
      await refreshWorkspaceDetail(workspaceId);
    } catch (error) {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) {
        flash(mutationError(error), "danger");
      }
    } finally {
      if (requestIsCurrent(generation, requestStillCurrent, userId, csrfToken, workspaceId)) setOperation("");
    }
  };

  const copy = async (value: string | undefined, message: string) => {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      flash(message);
    } catch {
      flash("复制失败，请重试", "danger");
    }
  };

  return {
    credential: projection.workspace,
    gatewayKey: projection.apiKey,
    workspaceBusy: operation === "workspace",
    gatewayKeyBusy: operation === "gateway-key",
    clear,
    reset,
    revealWorkspacePassword,
    revealWorkspaceKey,
    rotateWorkspacePassword,
    copyWorkspacePassword: () => copy(projection.workspace?.password, "Workspace 密码已复制"),
    copyWorkspaceKey: () => copy(projection.apiKey?.value, "Workspace Key 已复制")
  };
}
