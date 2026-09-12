import { useCallback, useEffect, useRef, useState } from "react";

import type { WorkspaceApplicationIntentDTO } from "../api/dtos.ts";
import {
  admitOperatorApplicationRevision,
  createOperatorWorkspaceApplicationDeployment,
  getOperatorWorkspaceApplicationDeployment
} from "../api/console-read-api.ts";
import {
  composeWorkspaceApplicationRevision,
  emptyWorkspaceApplicationRevisionDraft,
  validateWorkspaceApplicationRevisionDraft,
  workspaceApplicationDeploymentConfigurationDigestValid,
  type WorkspaceApplicationRevisionDraft
} from "./workspace-application-deployment-controller-model.ts";

const deploymentPollInterval = 2000;
const deploymentPollLimit = 150;

export interface WorkspaceApplicationDeploymentDependencies {
  session: { user: { id: string }; csrfToken: string } | null;
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
  configurationDigest: string;
  setConfigurationDigest: (value: string) => void;
  intent: WorkspaceApplicationIntentDTO | null;
  busy: boolean;
  admitRevision: () => Promise<boolean>;
  deploy: (workspaceId: string) => Promise<boolean>;
  reset: () => void;
}

export function useWorkspaceApplicationDeploymentController({
  session,
  flash,
  mutationError,
  currentMutationRequest
}: WorkspaceApplicationDeploymentDependencies): WorkspaceApplicationDeploymentCapability {
  const [draft, setDraft] = useState<WorkspaceApplicationRevisionDraft>(emptyWorkspaceApplicationRevisionDraft());
  const [applicationId, setApplicationId] = useState("");
  const [targetRevision, setTargetRevision] = useState("");
  const [configurationDigest, setConfigurationDigest] = useState("");
  const [intent, setIntent] = useState<WorkspaceApplicationIntentDTO | null>(null);
  const [busy, setBusy] = useState(false);
  const requestGeneration = useRef(0);
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const scope = useRef({ userId: session?.user.id || "", csrfToken: session?.csrfToken || "" });
  scope.current = { userId: session?.user.id || "", csrfToken: session?.csrfToken || "" };

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
  }, [reset, session?.csrfToken, session?.user.id]);

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

  const pollIntent = useCallback((operationId: string, generation: number) => {
    if (pollTimer.current) clearTimeout(pollTimer.current);
    let polls = 0;
    const tick = async () => {
      if (generation !== requestGeneration.current) return;
      polls += 1;
      try {
        const envelope = await getOperatorWorkspaceApplicationDeployment(operationId);
        const response = envelope.available ? envelope.data : null;
        if (generation !== requestGeneration.current || !response) return;
        setIntent(response.intent);
        if (response.intent.phase === "active" || response.intent.phase === "manual_review") {
          setBusy(false);
          if (response.intent.phase === "active") flash("应用部署完成");
          else flash(`应用部署待人工处理：${response.intent.lastError || "详见部署记录"}`, "danger");
          return;
        }
      } catch {
        if (generation !== requestGeneration.current) return;
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
  }, [flash]);

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
    if (!session || busy || !workspaceId) return false;
    if (!applicationId || !targetRevision) {
      flash("请先填写应用 ID 与目标版本", "danger");
      return false;
    }
    if (!workspaceApplicationDeploymentConfigurationDigestValid(configurationDigest)) {
      flash("配置摘要需为 64 位十六进制", "danger");
      return false;
    }
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const csrfToken = session.csrfToken;
    setBusy(true);
    try {
      const result = await createOperatorWorkspaceApplicationDeployment(
        workspaceId, applicationId, targetRevision, configurationDigest, csrfToken,
        `wsad-${crypto.randomUUID()}`
      );
      if (generation !== requestGeneration.current || !requestStillCurrent()) return false;
      setIntent(result.intent);
      pollIntent(result.intent.operationId, generation);
      return true;
    } catch (error) {
      if (generation === requestGeneration.current && requestStillCurrent()) flash(mutationError(error), "danger");
      setBusy(false);
      return false;
    }
  }, [applicationId, busy, configurationDigest, currentMutationRequest, flash, mutationError, pollIntent, session, targetRevision]);

  const validation = validateWorkspaceApplicationRevisionDraft(draft);
  return {
    applicationId, targetRevision, setApplicationId, setTargetRevision,
    draft, validation, setDraftField,
    addPersistentMount, removePersistentMount, addScratchMount, removeScratchMount,
    addDependency, removeDependency, setDraftListItem, setDraftDependency,
    configurationDigest, setConfigurationDigest,
    intent, busy, admitRevision, deploy, reset
  };
}
