import { useCallback, useEffect, useRef, useState } from "react";

import type { WorkspaceApplicationIntentDTO } from "../api/dtos.ts";
import {
  admitOperatorApplicationRevision,
  createOperatorWorkspaceApplicationDeployment,
  getOperatorWorkspaceApplicationDeployment,
  retryOperatorWorkspaceApplicationDeployment
} from "../api/console-read-api.ts";
import {
  composeWorkspaceApplicationRevision,
  emptyWorkspaceApplicationRevisionDraft,
  validateWorkspaceApplicationRevisionDraft,
  type WorkspaceApplicationRevisionDraft
} from "./workspace-application-deployment-controller-model.ts";

const deploymentPollInterval = 2000;
const deploymentPollLimit = 150;

export interface WorkspaceApplicationDeploymentDependencies {
  session: { user: { id: string }; csrfToken: string } | null;
  workspaceId: string;
  refreshWorkspace: (workspaceId: string) => Promise<void>;
  flash: (message: string, tone?: string) => void;
  mutationError: (error: unknown) => string;
  currentMutationRequest: () => () => boolean;
}

export interface WorkspaceApplicationDeploymentCapability {
  applicationId: string;
  targetRevision: string;
  setApplicationId: (value: string) => void;
  setTargetRevision: (value: string) => void;
  draft: WorkspaceApplicationRevisionDraft;
  validation: ReturnType<typeof validateWorkspaceApplicationRevisionDraft>;
  setDraftField: <K extends keyof WorkspaceApplicationRevisionDraft>(field: K, value: WorkspaceApplicationRevisionDraft[K]) => void;
  addPersistentMount: () => void;
  removePersistentMount: (index: number) => void;
  addScratchMount: () => void;
  removeScratchMount: (index: number) => void;
  addDependency: () => void;
  removeDependency: (index: number) => void;
  setDraftListItem: (list: "persistentMounts" | "scratchMounts", index: number, field: "name" | "mountPath", value: string) => void;
  setDraftDependency: (index: number, field: "name" | "image", value: string) => void;
  intent: WorkspaceApplicationIntentDTO | null;
  busy: boolean;
  admitRevision: () => Promise<boolean>;
  deploy: (workspaceId: string) => Promise<boolean>;
  retry: (workspaceId: string, operationId: string) => Promise<boolean>;
  reset: () => void;
}

export function useWorkspaceApplicationDeploymentController({
  session,
  workspaceId,
  refreshWorkspace,
  flash,
  mutationError,
  currentMutationRequest
}: WorkspaceApplicationDeploymentDependencies): WorkspaceApplicationDeploymentCapability {
  const [draft, setDraft] = useState<WorkspaceApplicationRevisionDraft>(emptyWorkspaceApplicationRevisionDraft());
  const [applicationId, setApplicationId] = useState("");
  const [targetRevision, setTargetRevision] = useState("");
  const [intent, setIntent] = useState<WorkspaceApplicationIntentDTO | null>(null);
  const [busy, setBusy] = useState(false);
  const requestGeneration = useRef(0);
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const selectedWorkspaceId = useRef(workspaceId);
  selectedWorkspaceId.current = workspaceId;

  const reset = useCallback(() => {
    requestGeneration.current += 1;
    if (pollTimer.current) clearTimeout(pollTimer.current);
    pollTimer.current = null;
    setIntent(null);
    setBusy(false);
  }, []);

  useEffect(() => {
    reset();
    return reset;
  }, [reset, session?.csrfToken, session?.user.id, workspaceId]);

  const setDraftField = useCallback(<K extends keyof WorkspaceApplicationRevisionDraft>(field: K, value: WorkspaceApplicationRevisionDraft[K]) => {
    setDraft((current) => ({ ...current, [field]: value }));
  }, []);

  const addPersistentMount = useCallback(() => {
    setDraft((current) => ({ ...current, persistentMounts: [...current.persistentMounts, { name: "", mountPath: "" }] }));
  }, []);
  const removePersistentMount = useCallback((index: number) => {
    setDraft((current) => ({ ...current, persistentMounts: current.persistentMounts.filter((_, i) => i !== index) }));
  }, []);
  const addScratchMount = useCallback(() => {
    setDraft((current) => ({ ...current, scratchMounts: [...current.scratchMounts, { name: "", mountPath: "" }] }));
  }, []);
  const removeScratchMount = useCallback((index: number) => {
    setDraft((current) => ({ ...current, scratchMounts: current.scratchMounts.filter((_, i) => i !== index) }));
  }, []);
  const addDependency = useCallback(() => {
    setDraft((current) => ({ ...current, dependencies: [...current.dependencies, { name: "", image: "" }] }));
  }, []);
  const removeDependency = useCallback((index: number) => {
    setDraft((current) => ({ ...current, dependencies: current.dependencies.filter((_, i) => i !== index) }));
  }, []);
  const setDraftListItem = useCallback((list: "persistentMounts" | "scratchMounts", index: number, field: "name" | "mountPath", value: string) => {
    setDraft((current) => ({
      ...current,
      [list]: current[list].map((item, i) => i === index ? { ...item, [field]: value } : item)
    }));
  }, []);
  const setDraftDependency = useCallback((index: number, field: "name" | "image", value: string) => {
    setDraft((current) => ({
      ...current,
      dependencies: current.dependencies.map((item, i) => i === index ? { ...item, [field]: value } : item)
    }));
  }, []);

  const pollIntent = useCallback((operationId: string, targetWorkspaceId: string, generation: number) => {
    if (pollTimer.current) clearTimeout(pollTimer.current);
    const requestStillCurrent = currentMutationRequest();
    const ownsRequest = () => generation === requestGeneration.current
      && selectedWorkspaceId.current === targetWorkspaceId && requestStillCurrent();
    let polls = 0;
    const tick = async () => {
      if (!ownsRequest()) return;
      polls += 1;
      try {
        const envelope = await getOperatorWorkspaceApplicationDeployment(operationId);
        const response = envelope.available ? envelope.data : null;
        if (!ownsRequest()) return;
        if (!response || response.intent.operationId !== operationId || response.intent.workspaceId !== targetWorkspaceId) {
          throw new Error("workspace_application_deployment_readback_unconfirmed");
        }
        setIntent(response.intent);
        if (response.intent.phase === "active" || response.intent.phase === "manual_review") {
          setBusy(false);
          if (response.intent.phase === "active") {
            await refreshWorkspace(targetWorkspaceId);
            if (ownsRequest()) flash("应用部署完成");
          }
          else flash(`应用部署待人工处理：${response.intent.lastError || "详见部署记录"}`, "danger");
          return;
        }
      } catch {
        if (!ownsRequest()) return;
        if (polls >= deploymentPollLimit) {
          setBusy(false);
          return;
        }
      }
      if (polls < deploymentPollLimit) {
        pollTimer.current = setTimeout(() => void tick(), deploymentPollInterval);
      } else {
        setBusy(false);
      }
    };
    void tick();
  }, [currentMutationRequest, flash, refreshWorkspace]);

  const admitRevision = useCallback(async (): Promise<boolean> => {
    if (!session || busy) return false;
    const validation = validateWorkspaceApplicationRevisionDraft(draft);
    if (!validation.ok) {
      flash("请先修正表单中标红的字段", "danger");
      return false;
    }
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const csrfToken = session.csrfToken;
    setBusy(true);
    try {
      const result = await admitOperatorApplicationRevision(
        composeWorkspaceApplicationRevision(draft), csrfToken, `${draft.applicationId}@${draft.version}`
      );
      if (generation !== requestGeneration.current || !requestStillCurrent()) return false;
      flash(`应用版本已准入（${result.decision === "identical" ? "与既有版本一致，幂等重放" : "新版本"}）：${result.revision.digest.slice(0, 16)}…`);
      return true;
    } catch (error) {
      if (generation === requestGeneration.current && requestStillCurrent()) flash(mutationError(error), "danger");
      return false;
    } finally {
      if (generation === requestGeneration.current && requestStillCurrent()) setBusy(false);
    }
  }, [busy, currentMutationRequest, draft, flash, mutationError, session]);

  const deploy = useCallback(async (workspaceId: string): Promise<boolean> => {
    if (!session || busy || !workspaceId || workspaceId !== selectedWorkspaceId.current) return false;
    if (!applicationId || !targetRevision) {
      flash("请先填写应用 ID 与目标版本", "danger");
      return false;
    }
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const csrfToken = session.csrfToken;
    setBusy(true);
    try {
      const result = await createOperatorWorkspaceApplicationDeployment(
        workspaceId, applicationId, targetRevision, { environment: {} }, csrfToken,
        `wsad-${crypto.randomUUID()}`
      );
      if (generation !== requestGeneration.current || workspaceId !== selectedWorkspaceId.current || !requestStillCurrent()) return false;
      if (result.intent.workspaceId !== workspaceId) throw new Error("workspace_application_deployment_identity_mismatch");
      setIntent(result.intent);
      pollIntent(result.intent.operationId, workspaceId, generation);
      return true;
    } catch (error) {
      if (generation === requestGeneration.current && workspaceId === selectedWorkspaceId.current && requestStillCurrent()) {
        flash(mutationError(error), "danger");
        setBusy(false);
      }
      return false;
    }
  }, [applicationId, busy, currentMutationRequest, flash, mutationError, pollIntent, session, targetRevision]);

  const validation = validateWorkspaceApplicationRevisionDraft(draft);
  const retry = useCallback(async (targetWorkspaceId: string, operationId: string): Promise<boolean> => {
    if (!session || busy || !operationId || !targetWorkspaceId || selectedWorkspaceId.current !== targetWorkspaceId) return false;
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const ownsRequest = () => generation === requestGeneration.current && selectedWorkspaceId.current === targetWorkspaceId && requestStillCurrent();
    setBusy(true);
    try {
      const result = await retryOperatorWorkspaceApplicationDeployment(operationId, session.csrfToken);
      if (!ownsRequest()) return false;
      if (result.intent.operationId !== operationId || result.intent.workspaceId !== targetWorkspaceId) throw new Error("workspace_application_deployment_identity_mismatch");
      setIntent(result.intent);
      pollIntent(operationId, targetWorkspaceId, generation);
      return true;
    } catch (error) {
      if (ownsRequest()) {
        flash(mutationError(error), "danger");
        setBusy(false);
      }
      return false;
    }
  }, [busy, currentMutationRequest, flash, mutationError, pollIntent, session]);
  return {
    applicationId, targetRevision, setApplicationId, setTargetRevision,
    draft, validation, setDraftField,
    addPersistentMount, removePersistentMount, addScratchMount, removeScratchMount,
    addDependency, removeDependency, setDraftListItem, setDraftDependency,
    intent, busy, admitRevision, deploy, retry, reset
  };
}
