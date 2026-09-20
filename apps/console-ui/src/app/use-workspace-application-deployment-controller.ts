import { useCallback, useEffect, useRef, useState } from "react";

import type { WorkspaceApplicationIntentDTO } from "../api/dtos.ts";
import {
  createOperatorWorkspaceApplicationDeployment,
  getOperatorWorkspaceApplicationDeployment,
  listOperatorRegistryRepositories,
  listOperatorRegistryTags,
  resolveOperatorRegistryImage,
  retryOperatorWorkspaceApplicationDeployment,
  type WorkspaceRegistryRepositoryCatalogDTO,
  type WorkspaceRegistryResolutionDTO,
  type WorkspaceRegistryTagDTO
} from "../api/console-read-api.ts";
import {
  composeWorkspaceApplicationRevision,
  emptyWorkspaceApplicationDeploymentSelection,
  parseWorkspaceApplicationDeploymentJSON,
  validateWorkspaceApplicationDeploymentForm,
  type WorkspaceApplicationDeploymentSelection
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

// The deployment form is one command: pick the image, state how it is exposed,
// deploy. Control Plane owns the application identity and reads what the image
// declares about itself, so there is no separate registration step and no
// operator-typed application or version.
export interface WorkspaceApplicationDeploymentCapability {
  registryNamespace: string;
  setRegistryNamespace: (value: string) => void;
  registryRepository: string;
  setRegistryRepository: (value: string) => void;
  registryTag: string;
  setRegistryTag: (value: string) => void;
  registryCatalog: WorkspaceRegistryRepositoryCatalogDTO | null;
  repositoryOptions: string[];
  registryTags: WorkspaceRegistryTagDTO[] | null;
  registryResolution: WorkspaceRegistryResolutionDTO | null;
  registryBusy: boolean;
  registryError: string;

  selection: WorkspaceApplicationDeploymentSelection;
  setSelectionField: <K extends keyof WorkspaceApplicationDeploymentSelection>(field: K, value: WorkspaceApplicationDeploymentSelection[K]) => void;
  advancedJSON: string;
  setAdvancedJSON: (value: string) => void;
  validation: ReturnType<typeof validateWorkspaceApplicationDeploymentForm>;

  configurationJSON: string;
  setConfigurationJSON: (value: string) => void;
  secretBindingsJSON: string;
  setSecretBindingsJSON: (value: string) => void;
  deploymentJSONError: string;

  intent: WorkspaceApplicationIntentDTO | null;
  busy: boolean;
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
  const [selection, setSelection] = useState<WorkspaceApplicationDeploymentSelection>(emptyWorkspaceApplicationDeploymentSelection);
  const [advancedJSON, setAdvancedJSON] = useState("");
  const [configurationJSON, setConfigurationJSON] = useState('{"environment":{}}');
  const [secretBindingsJSON, setSecretBindingsJSON] = useState("[]");
  let deploymentJSONError = "";
  try { parseWorkspaceApplicationDeploymentJSON(configurationJSON, secretBindingsJSON); } catch (error) { deploymentJSONError = (error as Error).message; }

  // Registry selection: the approved namespace and its declared repositories are
  // read once, then a repository's tags, then one tag's digest. Each step loads
  // the next automatically, so an operator selects a version instead of driving
  // three technical requests by hand.
  const [registryCatalog, setRegistryCatalog] = useState<WorkspaceRegistryRepositoryCatalogDTO | null>(null);
  const [registryNamespace, setRegistryNamespaceState] = useState("");
  const [registryRepository, setRegistryRepositoryState] = useState("");
  const [registryTag, setRegistryTagState] = useState("");
  const [registryTags, setRegistryTags] = useState<WorkspaceRegistryTagDTO[] | null>(null);
  const [registryResolution, setRegistryResolution] = useState<WorkspaceRegistryResolutionDTO | null>(null);
  const [registryBusy, setRegistryBusy] = useState(false);
  const [registryError, setRegistryError] = useState("");

  const [intent, setIntent] = useState<WorkspaceApplicationIntentDTO | null>(null);
  const [busy, setBusy] = useState(false);
  const requestGeneration = useRef(0);
  const registryGeneration = useRef(0);
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const selectedWorkspaceId = useRef(workspaceId);
  selectedWorkspaceId.current = workspaceId;

  const reset = useCallback(() => {
    requestGeneration.current += 1;
    registryGeneration.current += 1;
    if (pollTimer.current) clearTimeout(pollTimer.current);
    pollTimer.current = null;
    setIntent(null);
    setBusy(false);
    setRegistryBusy(false);
  }, []);

  useEffect(() => {
    reset();
    setConfigurationJSON('{"environment":{}}');
    setSecretBindingsJSON("[]");
    return reset;
  }, [reset, session?.csrfToken, session?.user.id, workspaceId]);

  const setSelectionField = useCallback(<K extends keyof WorkspaceApplicationDeploymentSelection>(field: K, value: WorkspaceApplicationDeploymentSelection[K]) => {
    setSelection((current) => ({ ...current, [field]: value }));
  }, []);

  const repositoryOptions = (registryCatalog?.items || [])
    .filter((item) => item.namespace === (registryNamespace || registryCatalog?.namespaces[0] || ""))
    .map((item) => item.repository);

  const ownsRegistryRequest = (generation: number) => generation === registryGeneration.current && currentMutationRequest()();

  const loadRegistryCatalog = useCallback(async (): Promise<boolean> => {
    if (!session || registryBusy) return false;
    const generation = ++registryGeneration.current;
    setRegistryBusy(true);
    try {
      // One read returns the installation's catalog namespaces and the
      // repositories it approved in them, so switching namespace never needs a
      // second request and an empty list is never confused with a failure.
      const catalog = await listOperatorRegistryRepositories("");
      if (!ownsRegistryRequest(generation)) return false;
      setRegistryCatalog(catalog);
      setRegistryError("");
      setRegistryNamespaceState((current) => current || catalog.namespaces[0] || "");
      return true;
    } catch (error) {
      if (ownsRegistryRequest(generation)) {
        const message = mutationError(error);
        setRegistryError(message);
        flash(message, "danger");
      }
      return false;
    } finally {
      if (ownsRegistryRequest(generation)) setRegistryBusy(false);
    }
  }, [currentMutationRequest, flash, mutationError, registryBusy, session]);

  useEffect(() => {
    // The catalogue is read for a deployment this operator is preparing, so it
    // belongs to a selected Workspace: a surface that renders no deployment
    // panel never calls an administrator registry route.
    if (!session || !workspaceId) return;
    void loadRegistryCatalog();
  }, [session?.csrfToken, session?.user.id, workspaceId]);

  const selectNamespace = useCallback((namespace: string) => {
    registryGeneration.current += 1;
    setRegistryNamespaceState(namespace);
    setRegistryRepositoryState("");
    setRegistryTagState("");
    setRegistryTags(null);
    setRegistryResolution(null);
    setSelection((current) => ({ ...current, image: "" }));
  }, []);

  const loadRegistryTags = useCallback(async (namespace: string, repository: string): Promise<boolean> => {
    if (!session || !namespace || !repository) return false;
    const generation = ++registryGeneration.current;
    setRegistryBusy(true);
    try {
      const result = await listOperatorRegistryTags(namespace, repository);
      if (!ownsRegistryRequest(generation)) return false;
      if (result.namespace !== namespace || result.repository !== repository) throw new Error("workspace_registry_readback_mismatch");
      setRegistryTags(result.tags);
      setRegistryError("");
      return true;
    } catch (error) {
      if (ownsRegistryRequest(generation)) {
        const message = mutationError(error);
        setRegistryError(message);
        flash(message, "danger");
      }
      return false;
    } finally {
      if (ownsRegistryRequest(generation)) setRegistryBusy(false);
    }
  }, [currentMutationRequest, flash, mutationError, session]);

  const selectRepository = useCallback((repository: string) => {
    registryGeneration.current += 1;
    setRegistryRepositoryState(repository);
    setRegistryTagState("");
    setRegistryTags(null);
    setRegistryResolution(null);
    setSelection((current) => ({ ...current, image: "" }));
    void loadRegistryTags(registryNamespace, repository);
  }, [loadRegistryTags, registryNamespace]);

  const resolveTag = useCallback(async (namespace: string, repository: string, tag: string): Promise<boolean> => {
    if (!session || !namespace || !repository || !tag) return false;
    const generation = ++registryGeneration.current;
    const csrfToken = session.csrfToken;
    setRegistryBusy(true);
    try {
      const resolution = await resolveOperatorRegistryImage(namespace, repository, tag, csrfToken, `registry-resolve:${namespace}/${repository}@${tag}`);
      if (!ownsRegistryRequest(generation)) return false;
      // The registry reference is Control Plane's fact: it is
      // host/namespace/repository@digest. Confirming it means comparing the
      // whole returned reference against the identity Control Plane reported,
      // never against a locally assembled repository@digest.
      if (resolution.host === "" || resolution.namespace !== namespace || resolution.repository !== repository || resolution.tag !== tag
        || !/^sha256:[0-9a-f]{64}$/.test(resolution.digest)
        || resolution.reference !== `${resolution.host}/${resolution.namespace}/${resolution.repository}@${resolution.digest}`) {
        throw new Error("workspace_registry_resolution_unconfirmed");
      }
      setRegistryResolution(resolution);
      setRegistryError("");
      setSelection((current) => ({ ...current, image: resolution.reference }));
      return true;
    } catch (error) {
      if (ownsRegistryRequest(generation)) {
        const message = mutationError(error);
        setRegistryError(message);
        flash(message, "danger");
      }
      return false;
    } finally {
      if (ownsRegistryRequest(generation)) setRegistryBusy(false);
    }
  }, [currentMutationRequest, flash, mutationError, session]);

  const selectTag = useCallback((tag: string) => {
    setRegistryTagState(tag);
    setRegistryResolution(null);
    setSelection((current) => ({ ...current, image: "" }));
    void resolveTag(registryNamespace, registryRepository, tag);
  }, [registryNamespace, registryRepository, resolveTag]);

  const setRegistryRepository = useCallback((repository: string) => selectRepository(repository), [selectRepository]);
  const setRegistryTag = useCallback((tag: string) => selectTag(tag), [selectTag]);

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

  const validation = validateWorkspaceApplicationDeploymentForm({ selection, advancedJSON });

  const deploy = useCallback(async (workspaceId: string): Promise<boolean> => {
    if (!session || busy || !workspaceId || workspaceId !== selectedWorkspaceId.current) return false;
    if (!validation.ok) {
      flash(validation.fieldErrors.image ?? Object.values(validation.fieldErrors)[0] ?? validation.advancedJSONError ?? "请先选择镜像并修正表单", "danger");
      return false;
    }
    if (deploymentJSONError) {
      flash(deploymentJSONError, "danger");
      return false;
    }
    const requestStillCurrent = currentMutationRequest();
    const generation = ++requestGeneration.current;
    const csrfToken = session.csrfToken;
    setBusy(true);
    try {
      const { configuration, secretBindings } = parseWorkspaceApplicationDeploymentJSON(configurationJSON, secretBindingsJSON);
      const revision = composeWorkspaceApplicationRevision({ selection, advancedJSON });
      const result = await createOperatorWorkspaceApplicationDeployment(
        workspaceId, configuration, csrfToken, `wsad-${crypto.randomUUID()}`, secretBindings, revision
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
  }, [advancedJSON, busy, configurationJSON, currentMutationRequest, deploymentJSONError, flash, mutationError, pollIntent, secretBindingsJSON, selection, session, validation]);

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
    registryNamespace: registryNamespace || registryCatalog?.namespaces[0] || "",
    setRegistryNamespace: selectNamespace,
    registryRepository, setRegistryRepository,
    registryTag, setRegistryTag,
    registryCatalog, repositoryOptions, registryTags, registryResolution, registryBusy, registryError,
    selection, setSelectionField, advancedJSON, setAdvancedJSON, validation,
    configurationJSON, setConfigurationJSON, secretBindingsJSON, setSecretBindingsJSON, deploymentJSONError,
    intent, busy, deploy, retry, reset
  };
}
