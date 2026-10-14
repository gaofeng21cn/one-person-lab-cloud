import { useCallback, useEffect, useRef, useState } from "react";

import type {
  AuthSession,
  SourceEnvelope,
  WorkspaceModelConfigurationDTO,
  WorkspaceModelSelectionDTO,
  WorkspaceOwnerOperationDTO
} from "../api/dtos.ts";
import {
  getWorkspaceModels,
  getWorkspaceOwnerOperation,
  updateWorkspaceModels
} from "../api/workspaces-api.ts";
import type { RemoteState, WorkspaceModelsController, WorkspaceModelsIssue } from "./console-controller-types.ts";
import {
  resolveWorkspaceModelUpdateIntent,
  shouldRetainWorkspaceModelIntent,
  workspaceModelConfigurationApplied,
  workspaceModelConfigurationIssue,
  workspaceModelOperationConfirmed,
  workspaceModelOperationMatchesConfiguration,
  workspaceModelOperationRejected,
  workspaceModelSelectionsSignature,
  type WorkspaceModelUpdateIntent
} from "./workspace-models-controller-model.ts";

interface WorkspaceModelsDependencies {
  active: boolean;
  workspaceId: string;
  currentSession: () => AuthSession | null;
  friendlyError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
  flash: (text: string, tone?: "good" | "danger") => void;
}

export interface WorkspaceModelsCapability extends WorkspaceModelsController {
  load: () => Promise<void>;
  reset: () => void;
}

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function settledConfiguration(configuration: WorkspaceModelConfigurationDTO): SourceEnvelope<WorkspaceModelConfigurationDTO> {
  return {
    source: "workspace",
    status: "available",
    available: true,
    fetchedAt: new Date().toISOString(),
    data: configuration
  };
}

// useWorkspaceModelsController owns the Console side of the Workspace model
// configuration: read the owner's persisted configuration, submit one
// expectedVersion command, and recover the acceptance from the owner's original
// operation. A 202 only records the intent; the applied version is only shown
// after the owner's own configuration readback confirms it.
export function useWorkspaceModelsController({
  active,
  workspaceId,
  currentSession,
  friendlyError,
  unavailableSource,
  flash
}: WorkspaceModelsDependencies): WorkspaceModelsCapability {
  const [configuration, setConfiguration] = useState<RemoteState<SourceEnvelope<WorkspaceModelConfigurationDTO>>>(emptyRemote);
  const [operation, setOperation] = useState<RemoteState<WorkspaceOwnerOperationDTO>>(emptyRemote);
  const [busy, setBusy] = useState(false);
  const [issue, setIssue] = useState<WorkspaceModelsIssue>("");
  const [updateError, setUpdateError] = useState("");

  const intent = useRef<WorkspaceModelUpdateIntent | null>(null);
  const generation = useRef(0);
  const busyClaim = useRef<symbol | null>(null);
  const sessionIdentityRef = useRef("");
  const routeKeyRef = useRef("");
  const scope = useRef({ active: false, workspaceId: "" });
  scope.current = { active, workspaceId };
  // The session is read at call time, not captured at render time: the first
  // route load after a full page boot resolves the session in the same effect
  // that calls this controller, so a render-time snapshot would be empty.
  const routeKey = `${active ? "models" : "inactive"}:${workspaceId}`;

  const reset = useCallback(() => {
    generation.current += 1;
    intent.current = null;
    busyClaim.current = null;
    setConfiguration(emptyRemote());
    setOperation(emptyRemote());
    setBusy(false);
    setIssue("");
    setUpdateError("");
  }, []);

  // A different signed-in user or session invalidates the projection; the same
  // boundary is re-checked at call time so an overlapping load cannot commit the
  // previous session's facts.
  const syncSessionBoundary = useCallback((session: AuthSession | null) => {
    const key = session ? `${session.user.id}\u0000${session.csrfToken}` : "";
    if (key === sessionIdentityRef.current) return;
    sessionIdentityRef.current = key;
    reset();
  }, [reset]);

  useEffect(() => {
    if (routeKeyRef.current === routeKey) return;
    routeKeyRef.current = routeKey;
    reset();
  }, [reset, routeKey]);

  useEffect(() => () => reset(), [reset]);

  const owns = useCallback((
    scopeGeneration: number,
    requestActive: boolean,
    userId: string,
    csrfToken: string,
    requestWorkspaceId: string
  ) => {
    const activeSession = currentSession();
    return scopeGeneration === generation.current
      && requestActive
      && scope.current.active
      && activeSession?.user.id === userId
      && activeSession.csrfToken === csrfToken
      && scope.current.workspaceId === requestWorkspaceId;
  }, [currentSession]);

  const clearCompletedIntent = useCallback((read: WorkspaceModelConfigurationDTO) => {
    const current = intent.current;
    if (!current || current.workspaceId !== workspaceId) return;
    if (workspaceModelSelectionsSignature(current.selections) === workspaceModelSelectionsSignature(read.selections)
      && read.version !== current.expectedVersion
      && workspaceModelConfigurationApplied(read)) {
      intent.current = null;
    }
  }, [workspaceId]);

  // Configuration readback and original operation are separate owner facts.
  // Re-read configuration after a terminal answer, retaining the previous
  // application fact and the operation if that second configuration read fails.
  const readOwnerOperation = useCallback(async (
    operationId: string,
    applicationConfiguration: WorkspaceModelConfigurationDTO | null,
    requestGeneration: number,
    userId: string,
    csrfToken: string,
    submittedCommand = false
  ): Promise<WorkspaceOwnerOperationDTO | null> => {
    try {
      const read = await getWorkspaceOwnerOperation(operationId);
      if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return null;
      setOperation({ value: read, loading: false, error: "" });
      const applicationIssue = applicationConfiguration
        ? workspaceModelConfigurationIssue(applicationConfiguration, read)
        : "unconfirmed";
      if (workspaceModelOperationConfirmed(read) || workspaceModelOperationRejected(read)) {
        if (workspaceModelOperationRejected(read)) intent.current = null;
        try {
          const configurationRead = await getWorkspaceModels(workspaceId);
          if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return null;
          setConfiguration({ value: settledConfiguration(configurationRead), loading: false, error: "" });
          if (workspaceModelOperationConfirmed(read)) setUpdateError("");
          if (workspaceModelConfigurationApplied(configurationRead)) clearCompletedIntent(configurationRead);
          const matches = workspaceModelOperationMatchesConfiguration(configurationRead, read);
          if (submittedCommand && !matches && workspaceModelOperationRejected(read)) {
            // An accepted command can fail before it records any configuration.
            // The previous applied version does not prove this command applied.
            setUpdateError("本次模型配置更新未确认生效，请以原操作结果核实。");
          }
          setIssue(workspaceModelConfigurationIssue(configurationRead, read));
          if (configurationRead.operationId && !matches) {
            // The latest read belongs to a different command. Bind closeout to
            // that exact operation, without another configuration read that
            // could recursively chase concurrent updates.
            setOperation({ value: null, loading: true, error: "" });
            try {
              const configurationOperation = await getWorkspaceOwnerOperation(configurationRead.operationId);
              if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return null;
              setOperation({ value: configurationOperation, loading: false, error: "" });
              setIssue(workspaceModelConfigurationIssue(configurationRead, configurationOperation));
            } catch (error) {
              if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return null;
              setOperation({ value: null, loading: false, error: friendlyError(error) });
              setIssue(workspaceModelConfigurationIssue(configurationRead));
            }
          }
        } catch (error) {
          if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return null;
          setConfiguration((current) => ({ ...current, error: friendlyError(error) }));
          setIssue(applicationIssue);
        }
        return read;
      }
      setIssue(applicationIssue);
      return read;
    } catch (error) {
      if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return null;
      // The operation read failed, so report no inferred closeout verdict.
      setOperation({ value: null, loading: false, error: friendlyError(error) });
      setIssue(applicationConfiguration ? workspaceModelConfigurationIssue(applicationConfiguration) : "unconfirmed");
      return null;
    }
  }, [clearCompletedIntent, friendlyError, owns, workspaceId]);

  const load = useCallback(async () => {
    const session = currentSession();
    syncSessionBoundary(session);
    if (!active || !workspaceId || !session) return;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const requestGeneration = ++generation.current;
    setConfiguration((current) => ({ ...current, loading: true, error: "" }));
    setUpdateError("");
    try {
      const configurationRead = await getWorkspaceModels(workspaceId);
      if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return;
      setConfiguration({ value: settledConfiguration(configurationRead), loading: false, error: "" });
      const configurationIssue = workspaceModelConfigurationIssue(configurationRead);
      if (workspaceModelConfigurationApplied(configurationRead)) clearCompletedIntent(configurationRead);
      setIssue(configurationIssue);
      // APPLIED confirms the reload, not predecessor-key retirement. Read the
      // original operation even for an applied configuration to expose closeout.
      if (configurationRead.operationId) {
        await readOwnerOperation(configurationRead.operationId, configurationRead, requestGeneration, userId, csrfToken);
      }
    } catch (error) {
      if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return;
      setConfiguration({ value: unavailableSource("workspace"), loading: false, error: friendlyError(error) });
      setIssue("unavailable");
    }
  }, [active, clearCompletedIntent, currentSession, friendlyError, owns, readOwnerOperation, syncSessionBoundary, unavailableSource, workspaceId]);

  const update = useCallback(async (selections: WorkspaceModelSelectionDTO[]): Promise<boolean> => {
    const session = currentSession();
    syncSessionBoundary(session);
    const current = configuration.value?.available ? configuration.value.data : null;
    if (!session || !active || !current) return false;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    if (busyClaim.current !== null) return false;
    const nextIntent = resolveWorkspaceModelUpdateIntent(intent.current, workspaceId, current.version, selections, () => crypto.randomUUID());
    if (intent.current && intent.current !== nextIntent) {
      // An earlier command's result is still unconfirmed. Overwriting its
      // idempotency identity would make the Console unable to recover it, so the
      // caller must retry the same selection or refresh the original operation.
      setUpdateError("上次模型配置更新结果待确认，请使用相同设置重试，或先按原操作刷新。");
      return false;
    }
    const claim = Symbol("workspace-models");
    busyClaim.current = claim;
    intent.current = nextIntent;
    setBusy(true);
    setUpdateError("");
    const requestGeneration = ++generation.current;
    try {
      const accepted = await updateWorkspaceModels(
        workspaceId,
        { expectedVersion: nextIntent.expectedVersion, selections: nextIntent.selections },
        csrfToken,
        nextIntent.idempotencyKey
      );
      if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return false;
      setOperation({ value: accepted, loading: false, error: "" });
      setIssue("unconfirmed");
      // Only a configuration read for this command can establish its application.
      let applicationConfiguration: WorkspaceModelConfigurationDTO | null = null;
      if (!workspaceModelOperationConfirmed(accepted) && !workspaceModelOperationRejected(accepted)) {
        // The command is persisted. Read the owner's own intent readback so the
        // displayed target version is what the owner stored, never an optimistic
        // local value; the applied version still only advances from confirmation.
        try {
          const readback = await getWorkspaceModels(workspaceId);
          if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return false;
          applicationConfiguration = readback;
          setConfiguration({ value: settledConfiguration(readback), loading: false, error: "" });
        } catch {
          // The accepted operation remains authoritative; the previous readback
          // stays displayed and the pending recovery stays available.
        }
      }
      const read = await readOwnerOperation(accepted.operationId, applicationConfiguration, requestGeneration, userId, csrfToken, true);
      return read ? workspaceModelOperationConfirmed(read) : false;
    } catch (error) {
      if (!owns(requestGeneration, true, userId, csrfToken, workspaceId)) return false;
      const retains = shouldRetainWorkspaceModelIntent(error);
      if (!retains && intent.current === nextIntent) intent.current = null;
      setUpdateError(friendlyError(error));
      setIssue(retains ? "unconfirmed" : "failed");
      return false;
    } finally {
      if (busyClaim.current === claim) {
        busyClaim.current = null;
        setBusy(false);
      }
    }
  }, [active, configuration, currentSession, friendlyError, owns, readOwnerOperation, syncSessionBoundary, workspaceId]);

  const refreshOperation = useCallback(async () => {
    const session = currentSession();
    syncSessionBoundary(session);
    const currentConfiguration = configuration.value?.available ? configuration.value.data : null;
    const operationId = operation.value?.operationId || currentConfiguration?.operationId || "";
    if (!session || !active || !operationId || busyClaim.current !== null) return;
    const claim = Symbol("workspace-models-refresh");
    busyClaim.current = claim;
    setBusy(true);
    const requestGeneration = ++generation.current;
    try {
      const read = await readOwnerOperation(operationId, currentConfiguration, requestGeneration, session.user.id, session.csrfToken);
      if (read && workspaceModelOperationConfirmed(read)) flash("模型配置原操作已完成");
    } finally {
      if (busyClaim.current === claim) {
        busyClaim.current = null;
        setBusy(false);
      }
    }
  }, [active, configuration, currentSession, flash, readOwnerOperation, operation, syncSessionBoundary]);

  const refresh = useCallback(async () => {
    await load();
  }, [load]);

  return { configuration, operation, busy, issue, updateError, update, refresh, refreshOperation, load, reset };
}
