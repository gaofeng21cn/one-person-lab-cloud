import { useRef, useState } from "react";

import type {
  AuthSession,
  GatewayWallet,
  PricingCatalogResponse,
  SourceEnvelope,
  CapabilityVersionDTO,
  ComputePlanDTO,
  StoragePlanDTO,
  LaunchModelDTO,
  WorkspaceQuoteDTO,
  WorkspaceOwnerOperationDTO,
  WorkspaceOwnerDTO,
  WorkspaceOwnerAccessDTO,
  GatewayWalletReadbackDTO
} from "../api/dtos.ts";
import {
  createAgentWorkspace,
  createWorkspaceQuote,
  getLaunchWallet,
  getWorkspaceQuote,
  getWorkspaceOwnerAccess,
  getWorkspaceOwnerOperation,
  getWorkspaceOwner,
  listAvailableComputePlans,
  listAvailableLaunchModels,
  listAvailableStoragePlans,
  listReadyCapabilityVersions,
  workspaceLaunchIdempotencyKey
} from "../api/workspaces-api.ts";
import type { RemoteState, WorkspaceLaunchController, WorkspaceLaunchStep } from "./console-controller-types.ts";
import {
  canCreateAgentQuote,
  canCreateAgentWorkspace,
  operationPollDelayMs,
  type AgentLaunchReadiness
} from "./workspace-launch-controller-model.ts";

const agentOperationStorageKey = "opl-cloud:agent-workspace-operation";

interface PersistedAgentIntent {
  actorId: string;
  operationId?: string;
  quoteId: string;
  input: { name: string; quoteId: string; renewalMode: "manual" };
  idempotencyKey: string;
}

export interface AgentWorkspaceLaunchController extends WorkspaceLaunchController {
  agentCapabilityVersions: CapabilityVersionDTO[];
  agentComputePlans: ComputePlanDTO[];
  agentStoragePlans: StoragePlanDTO[];
  agentModels: LaunchModelDTO[];
  agentWallet: GatewayWalletReadbackDTO | null;
  agentSourceError: string;
  agentSourceLoading: boolean;
  agentCapabilityVersionId: string;
  setAgentCapabilityVersionId: (value: string) => void;
  agentComputePlanId: string;
  setAgentComputePlanId: (value: string) => void;
  agentStoragePlanId: string;
  setAgentStoragePlanId: (value: string) => void;
  agentModelSelections: Record<string, string>;
  setAgentModelSelection: (slot: string, modelId: string) => void;
  agentQuote: WorkspaceQuoteDTO | null;
  agentOperation: WorkspaceOwnerOperationDTO | null;
  agentWorkspace: WorkspaceOwnerDTO | null;
  agentAccess: WorkspaceOwnerAccessDTO | null;
  agentStep: "configure" | "quote" | "operation";
  agentConfirmed: boolean;
  setAgentConfirmed: (value: boolean) => void;
  agentBusy: boolean;
  agentPollIssue: "" | "unavailable" | "timeout" | "unknown";
  agentRecoveryPending: boolean;
  agentRecoveryActionable: boolean;
  reviewAgentLaunch: () => void;
  submitAgentLaunch: () => Promise<void>;
  retryAgentLaunch: () => Promise<void>;
  startAnotherAgentLaunch: () => void;
  openAgentWorkspace: () => void;
}

interface WorkspaceLaunchDependencies {
  session: AuthSession | null;
  wallet: RemoteState<SourceEnvelope<GatewayWallet>>;
  isRequestCurrent: (generation: number, userId?: string) => boolean;
  currentMutationRequest: () => () => boolean;
  currentRequestGeneration: () => number;
  navigate: (path: string) => void;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
}

export interface WorkspaceLaunchCapability extends AgentWorkspaceLaunchController {
  loadCatalog: (generation: number, activeSession: AuthSession) => Promise<void>;
  recover: (generation: number, activeSession: AuthSession) => Promise<void>;
  reset: () => void;
}

const emptyCatalog = (): RemoteState<PricingCatalogResponse> => ({ value: null, loading: false, error: "" });

function operationIsTerminal(operation: WorkspaceOwnerOperationDTO): boolean {
  return ["succeeded", "failed", "needs_attention", "cancelled"].includes(operation.status);
}

function selectionList(requirements: CapabilityVersionDTO["modelRequirements"], selections: Record<string, string>) {
  return requirements.flatMap((requirement) => {
    const modelId = selections[requirement.slot];
    return modelId ? [{ slot: requirement.slot, modelId }] : [];
  });
}

export function useWorkspaceLaunchController({
  session,
  isRequestCurrent,
  currentMutationRequest,
  currentRequestGeneration,
  navigate,
  flash,
  friendlyError
}: WorkspaceLaunchDependencies): WorkspaceLaunchCapability {
  const [catalog, setCatalog] = useState<RemoteState<PricingCatalogResponse>>(emptyCatalog);
  const [launchName, setLaunchName] = useState("");
  const [launchAutoRenew, setLaunchAutoRenew] = useState(false);
  const [launchStep, setLaunchStep] = useState<WorkspaceLaunchStep>("configure");
  const [launchConfirmed, setLaunchConfirmed] = useState(false);
  const [agentCapabilityVersions, setAgentCapabilityVersions] = useState<CapabilityVersionDTO[]>([]);
  const [agentComputePlans, setAgentComputePlans] = useState<ComputePlanDTO[]>([]);
  const [agentStoragePlans, setAgentStoragePlans] = useState<StoragePlanDTO[]>([]);
  const [agentModels, setAgentModels] = useState<LaunchModelDTO[]>([]);
  const [agentWallet, setAgentWallet] = useState<GatewayWalletReadbackDTO | null>(null);
  const [agentSourceError, setAgentSourceError] = useState("");
  const [agentSourceLoading, setAgentSourceLoading] = useState(false);
  const [agentCapabilityVersionId, setAgentCapabilityVersionIdState] = useState("");
  const [agentComputePlanId, setAgentComputePlanIdState] = useState("");
  const [agentStoragePlanId, setAgentStoragePlanIdState] = useState("");
  const [agentModelSelections, setAgentModelSelections] = useState<Record<string, string>>({});
  const [agentQuote, setAgentQuote] = useState<WorkspaceQuoteDTO | null>(null);
  const [agentOperation, setAgentOperation] = useState<WorkspaceOwnerOperationDTO | null>(null);
  const [agentWorkspace, setAgentWorkspace] = useState<WorkspaceOwnerDTO | null>(null);
  const [agentAccess, setAgentAccess] = useState<WorkspaceOwnerAccessDTO | null>(null);
  const [agentConfirmed, setAgentConfirmed] = useState(false);
  const [agentBusy, setAgentBusy] = useState(false);
  const [agentPollIssue, setAgentPollIssue] = useState<"" | "unavailable" | "timeout" | "unknown">("");
  const [agentRecoveryPending, setAgentRecoveryPending] = useState(() => Boolean(sessionStorage.getItem(agentOperationStorageKey)));
  const intent = useRef<PersistedAgentIntent | null>(null);

  const invalidateQuote = () => {
    setAgentQuote(null);
    setAgentConfirmed(false);
    setLaunchStep("configure");
  };

  const reset = () => {
    setCatalog(emptyCatalog());
    setLaunchName("");
    setLaunchAutoRenew(false);
    setLaunchStep("configure");
    setLaunchConfirmed(false);
    setAgentCapabilityVersions([]);
    setAgentComputePlans([]);
    setAgentStoragePlans([]);
    setAgentModels([]);
    setAgentWallet(null);
    setAgentSourceError("");
    setAgentCapabilityVersionIdState("");
    setAgentComputePlanIdState("");
    setAgentStoragePlanIdState("");
    setAgentModelSelections({});
    setAgentQuote(null);
    setAgentOperation(null);
    setAgentWorkspace(null);
    setAgentAccess(null);
    setAgentConfirmed(false);
    setAgentBusy(false);
    setAgentPollIssue("");
    setAgentRecoveryPending(false);
    intent.current = null;
  };

  const loadCatalog = async (generation: number, activeSession: AuthSession) => {
    setAgentSourceLoading(true);
    setAgentSourceError("");
    try {
      const [versions, compute, storage, models, wallet] = await Promise.all([
        listReadyCapabilityVersions(), listAvailableComputePlans(), listAvailableStoragePlans(), listAvailableLaunchModels(), getLaunchWallet()
      ]);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      const availableVersions = versions.filter((version) => version.status === "ready");
      const availableCompute = compute.filter((plan) => plan.availability === "available");
      const availableStorage = storage.filter((plan) => plan.availability === "available");
      const availableModels = models.filter((model) => model.available);
      if (!availableVersions.length || !availableCompute.length || !availableStorage.length || !availableModels.length) {
        throw new Error("agent_launch_catalog_empty");
      }
      setAgentCapabilityVersions(availableVersions);
      setAgentComputePlans(availableCompute);
      setAgentStoragePlans(availableStorage);
      setAgentModels(availableModels);
      setAgentWallet(wallet);
      setAgentCapabilityVersionIdState((current) => current && availableVersions.some((v) => v.id === current) ? current : availableVersions[0].id);
      setAgentComputePlanIdState((current) => current && availableCompute.some((p) => p.id === current) ? current : availableCompute[0].id);
      setAgentStoragePlanIdState((current) => current && availableStorage.some((p) => p.id === current) ? current : availableStorage[0].id);
      setAgentSourceLoading(false);
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) {
        setAgentSourceLoading(false);
        setAgentSourceError(friendlyError(error));
        setAgentWallet(null);
        setAgentQuote(null);
      }
    }
  };

  const readAccess = async (workspaceId: string, generation: number, activeSession: AuthSession) => {
    try {
      const access = await getWorkspaceOwnerAccess(workspaceId, activeSession.csrfToken, `workspace-access:${workspaceId}`);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setAgentAccess(access);
      setAgentPollIssue("");
    } catch {
      if (isRequestCurrent(generation, activeSession.user.id)) {
        setAgentAccess(null);
        setAgentPollIssue("unknown");
      }
    }
  };

  const readCompletedAgentOperation = async (operation: WorkspaceOwnerOperationDTO, generation: number, activeSession: AuthSession) => {
    if (operation.status !== "succeeded" || !operation.resourceId) {
      if (operation.status !== "cancelled") setAgentPollIssue("unknown");
      return;
    }
    try {
      const workspace = await getWorkspaceOwner(operation.resourceId);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setAgentWorkspace(workspace);
      if (workspace.applicationAvailability === "available") await readAccess(workspace.id, generation, activeSession);
    } catch {
      if (isRequestCurrent(generation, activeSession.user.id)) setAgentPollIssue("unknown");
    }
  };

  const pollAgentOperation = async (operationId: string, generation: number, activeSession: AuthSession) => {
    for (let attempt = 0; attempt < 30; attempt += 1) {
      try {
        const operation = await getWorkspaceOwnerOperation(operationId);
        if (!isRequestCurrent(generation, activeSession.user.id)) return;
        setAgentOperation(operation);
        if (operationIsTerminal(operation)) {
          await readCompletedAgentOperation(operation, generation, activeSession);
          return;
        }
        await new Promise<void>((resolve) => window.setTimeout(resolve, operationPollDelayMs(operation.pollAfterSeconds)));
      } catch {
        if (isRequestCurrent(generation, activeSession.user.id)) setAgentPollIssue("unknown");
        return;
      }
    }
    if (isRequestCurrent(generation, activeSession.user.id)) setAgentPollIssue("timeout");
  };

  const recover = async (generation: number, activeSession: AuthSession) => {
    const raw = sessionStorage.getItem(agentOperationStorageKey);
    if (!raw) { setAgentRecoveryPending(false); return; }
    try {
      const saved = JSON.parse(raw) as Partial<PersistedAgentIntent>;
      if (saved.actorId !== activeSession.user.id) { setAgentRecoveryPending(true); return; }
      if (!saved.quoteId || !saved.input || !saved.idempotencyKey) throw new Error("invalid_agent_operation_locator");
      intent.current = saved as PersistedAgentIntent;
      try { setAgentQuote(await getWorkspaceQuote(saved.quoteId)); } catch { setAgentPollIssue("unknown"); }
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setLaunchStep("confirm");
      setAgentBusy(false);
      if (!saved.operationId) {
        setAgentRecoveryPending(true);
        return;
      }
      setAgentRecoveryPending(false);
      const operation = await getWorkspaceOwnerOperation(saved.operationId);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setAgentOperation(operation);
      if (!operationIsTerminal(operation)) void pollAgentOperation(operation.operationId, generation, activeSession);
      else await readCompletedAgentOperation(operation, generation, activeSession);
    } catch {
      if (isRequestCurrent(generation, activeSession.user.id)) {
        setAgentRecoveryPending(true);
        setAgentPollIssue("unavailable");
      }
    }
  };

  const setAgentCapabilityVersionId = (value: string) => { setAgentCapabilityVersionIdState(value); invalidateQuote(); };
  const setAgentComputePlanId = (value: string) => { setAgentComputePlanIdState(value); invalidateQuote(); };
  const setAgentStoragePlanId = (value: string) => { setAgentStoragePlanIdState(value); invalidateQuote(); };
  const setAgentModelSelection = (slot: string, modelId: string) => { setAgentModelSelections((current) => ({ ...current, [slot]: modelId })); invalidateQuote(); };

  const selectedCapability = agentCapabilityVersions.find((version) => version.id === agentCapabilityVersionId) || null;
  const modelSelections = selectedCapability ? selectionList(selectedCapability.modelRequirements, agentModelSelections) : [];
  const modelSelectionsReady = Boolean(selectedCapability) && selectedCapability.modelRequirements.every((requirement) => {
    const selection = modelSelections.find((item) => item.slot === requirement.slot);
    return !requirement.required && !selection || Boolean(selection && requirement.allowedModelIds.length > 0 && requirement.allowedModelIds.includes(selection.modelId));
  });
  const readiness: AgentLaunchReadiness = {
    sourceReady: !agentSourceLoading && !agentSourceError && !agentRecoveryPending,
    hasName: Boolean(launchName.trim()),
    hasCapabilityVersion: Boolean(selectedCapability),
    hasComputePlan: Boolean(agentComputePlanId),
    hasStoragePlan: Boolean(agentStoragePlanId),
    modelSelectionsReady,
    walletReadbackReady: Boolean(agentWallet),
    walletSufficient: Boolean(agentWallet && agentQuote && BigInt(agentWallet.balanceUSDMicros) >= BigInt(agentQuote.totalUSDMicros)),
    quoteReady: Boolean(agentQuote),
    quoteCurrent: Boolean(agentQuote && agentQuote.status === "offered" && new Date(agentQuote.expiresAt).getTime() > Date.now())
  };

  const reviewAgentLaunch = () => {
    if (!session || intent.current || !selectedCapability || !canCreateAgentQuote(readiness)) return;
    setAgentBusy(true);
    void createWorkspaceQuote({
      purpose: "deploy", capabilityVersionId: selectedCapability.id, computePlanId: agentComputePlanId,
      storagePlanId: agentStoragePlanId, modelSelections, periodMonths: 1
    }, session.csrfToken, `workspace-quote:${crypto.randomUUID()}`).then((quote) => {
      setAgentQuote(quote);
      setAgentConfirmed(false);
      setLaunchStep("confirm");
    }).catch((error) => flash(friendlyError(error), "danger")).finally(() => setAgentBusy(false));
  };

  const submitAgentLaunch = async () => {
    if (!session || !agentQuote || agentRecoveryPending) return;
    if (Date.parse(agentQuote.expiresAt) <= Date.now()) {
      setAgentQuote(null);
      setAgentConfirmed(false);
      flash("报价已过期，请重新获取准确报价", "danger");
      return;
    }
    if (!canCreateAgentWorkspace(readiness, agentConfirmed)) return;
    const stillCurrent = currentMutationRequest();
    const input = { name: launchName.trim(), quoteId: agentQuote.id, renewalMode: "manual" as const };
    if (intent.current && (intent.current.quoteId !== agentQuote.id || JSON.stringify(intent.current.input) !== JSON.stringify(input))) {
      flash("原开通请求待核实，请勿更换报价后重试", "danger");
      return;
    }
    const idempotencyKey = intent.current?.idempotencyKey || workspaceLaunchIdempotencyKey();
    intent.current = { actorId: session.user.id, quoteId: agentQuote.id, input, idempotencyKey };
    sessionStorage.setItem(agentOperationStorageKey, JSON.stringify(intent.current satisfies PersistedAgentIntent));
    setAgentBusy(true);
    setLaunchStep("confirm");
    try {
      const operation = await createAgentWorkspace(input, session.csrfToken, idempotencyKey);
      if (!stillCurrent()) return;
      setAgentOperation(operation);
      intent.current = { ...intent.current, operationId: operation.operationId } as PersistedAgentIntent;
      sessionStorage.setItem(agentOperationStorageKey, JSON.stringify(intent.current));
      if (!operationIsTerminal(operation)) void pollAgentOperation(operation.operationId, currentRequestGeneration(), session);
      else await readCompletedAgentOperation(operation, currentRequestGeneration(), session);
    } catch (error) {
      if (stillCurrent()) {
        setAgentRecoveryPending(true);
        setAgentPollIssue("unavailable");
        flash(friendlyError(error), "danger");
      }
    } finally {
      if (stillCurrent()) setAgentBusy(false);
    }
  };

  const retryAgentLaunch = async () => {
    const saved = intent.current;
    if (!session || !saved || saved.actorId !== session.user.id || saved.operationId || agentBusy) return;
    const stillCurrent = currentMutationRequest();
    setAgentBusy(true);
    try {
      const operation = await createAgentWorkspace(saved.input, session.csrfToken, saved.idempotencyKey);
      if (!stillCurrent()) return;
      setAgentOperation(operation);
      intent.current = { ...saved, operationId: operation.operationId };
      sessionStorage.setItem(agentOperationStorageKey, JSON.stringify(intent.current));
      setAgentRecoveryPending(false);
      setAgentPollIssue("");
      if (!operationIsTerminal(operation)) void pollAgentOperation(operation.operationId, currentRequestGeneration(), session);
      else await readCompletedAgentOperation(operation, currentRequestGeneration(), session);
    } catch (error) {
      if (stillCurrent()) {
        setAgentPollIssue("unavailable");
        flash(friendlyError(error), "danger");
      }
    } finally {
      if (stillCurrent()) setAgentBusy(false);
    }
  };

  const startAnotherAgentLaunch = () => {
    if (agentOperation?.status !== "succeeded" || !agentAccess?.url) return;
    sessionStorage.removeItem(agentOperationStorageKey);
    intent.current = null;
    setAgentOperation(null);
    setAgentWorkspace(null);
    setAgentAccess(null);
    setAgentQuote(null);
    setAgentConfirmed(false);
    setAgentPollIssue("");
    setLaunchName("");
    setLaunchStep("configure");
  };

  const openAgentWorkspace = async () => {
    if (!agentAccess?.url) return;
    window.open(agentAccess.url, "_blank", "noopener,noreferrer");
  };

  return {
    catalog,
    previews: {},
    launchName,
    setLaunchName,
    launchPlan: "basic",
    setLaunchPlan: () => undefined,
    launchAutoRenew,
    setLaunchAutoRenew: (value) => { setLaunchAutoRenew(value); invalidateQuote(); },
    launchStep,
    setLaunchStep,
    launchConfirmed,
    setLaunchConfirmed,
    selectedPlan: null,
    selectedPrice: null,
    walletUsdMicros: agentWallet?.balanceUSDMicros || null,
    balanceSufficient: agentWallet !== null,
    customerOwned: false,
    launchOperation: null,
    launchRecoveryState: "clear",
    launchPollIssue: "",
    busy: agentBusy,
    reviewWorkspaceLaunch: reviewAgentLaunch,
    submitWorkspaceLaunch: submitAgentLaunch,
    openLaunchedWorkspace: openAgentWorkspace,
    openLaunchBilling: () => navigate("/console/billing"),
    prepareNewWorkspaceLaunch: () => { if (agentRecoveryPending) return; setAgentOperation(null); setAgentWorkspace(null); setAgentAccess(null); setLaunchStep("configure"); setAgentConfirmed(false); },
    agentCapabilityVersions,
    agentComputePlans,
    agentStoragePlans,
    agentModels,
    agentWallet,
    agentSourceError,
    agentSourceLoading,
    agentCapabilityVersionId,
    setAgentCapabilityVersionId,
    agentComputePlanId,
    setAgentComputePlanId,
    agentStoragePlanId,
    setAgentStoragePlanId,
    agentModelSelections,
    setAgentModelSelection,
    agentQuote,
    agentOperation,
    agentWorkspace,
    agentAccess,
    agentStep: launchStep === "confirm" ? "quote" : agentOperation ? "operation" : "configure",
    agentConfirmed,
    setAgentConfirmed,
    agentBusy,
    agentPollIssue,
    agentRecoveryPending,
    agentRecoveryActionable: Boolean(intent.current && intent.current.actorId === session?.user.id && !intent.current.operationId),
    reviewAgentLaunch,
    submitAgentLaunch,
    retryAgentLaunch,
    startAnotherAgentLaunch,
    openAgentWorkspace,
    loadCatalog,
    recover,
    reset
  };
}
