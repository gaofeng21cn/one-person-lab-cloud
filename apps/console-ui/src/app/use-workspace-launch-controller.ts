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
  operationId?: string;
  quoteId: string;
  input: { name: string; quoteId: string; renewalMode: "manual" | "automatic"; automaticRenewalConsent?: true };
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
  reviewAgentLaunch: () => void;
  submitAgentLaunch: () => Promise<void>;
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
    intent.current = null;
    sessionStorage.removeItem(agentOperationStorageKey);
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
    if (!session) return;
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

  const pollAgentOperation = async (operationId: string, generation: number, activeSession: AuthSession) => {
    for (let attempt = 0; attempt < 30; attempt += 1) {
      try {
        const operation = await getWorkspaceOwnerOperation(operationId);
        if (!isRequestCurrent(generation, activeSession.user.id)) return;
        setAgentOperation(operation);
        if (operationIsTerminal(operation)) {
          if (operation.status === "succeeded" && operation.resourceId) {
            try {
              const workspace = await getWorkspaceOwner(operation.resourceId);
              if (!isRequestCurrent(generation, activeSession.user.id)) return;
              setAgentWorkspace(workspace);
              if (workspace?.applicationAvailability === "available") await readAccess(workspace.id, generation, activeSession);
            } catch {
              setAgentPollIssue("unknown");
            }
          } else if (operation.status !== "cancelled") {
            setAgentPollIssue("unknown");
          }
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
    if (!raw) return;
    try {
      const saved = JSON.parse(raw) as Partial<PersistedAgentIntent>;
      if (!saved.operationId || !saved.quoteId || !saved.input || !saved.idempotencyKey) throw new Error("invalid_agent_operation_locator");
      intent.current = saved as PersistedAgentIntent;
      try { setAgentQuote(await getWorkspaceQuote(saved.quoteId)); } catch { setAgentPollIssue("unknown"); }
      setLaunchStep("confirm");
      setAgentBusy(false);
      const operation = await getWorkspaceOwnerOperation(saved.operationId);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setAgentOperation(operation);
      if (!operationIsTerminal(operation)) void pollAgentOperation(operation.operationId, generation, activeSession);
    } catch {
      if (isRequestCurrent(generation, activeSession.user.id)) setAgentPollIssue("unavailable");
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
    sourceReady: !agentSourceLoading && !agentSourceError,
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
    if (!session || !selectedCapability || !canCreateAgentQuote(readiness)) return;
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
    if (!session || !agentQuote) return;
    if (Date.parse(agentQuote.expiresAt) <= Date.now()) {
      setAgentQuote(null);
      setAgentConfirmed(false);
      flash("报价已过期，请重新获取准确报价", "danger");
      return;
    }
    if (!canCreateAgentWorkspace(readiness, agentConfirmed)) return;
    const stillCurrent = currentMutationRequest();
    const input = { name: launchName.trim(), quoteId: agentQuote.id, renewalMode: launchAutoRenew ? "automatic" as const : "manual" as const, ...(launchAutoRenew ? { automaticRenewalConsent: true as const } : {}) };
    if (intent.current && (intent.current.quoteId !== agentQuote.id || JSON.stringify(intent.current.input) !== JSON.stringify(input))) {
      flash("原开通请求待核实，请勿更换报价后重试", "danger");
      return;
    }
    const idempotencyKey = intent.current?.idempotencyKey || workspaceLaunchIdempotencyKey();
    intent.current = { quoteId: agentQuote.id, input, idempotencyKey };
    sessionStorage.setItem(agentOperationStorageKey, JSON.stringify(intent.current satisfies PersistedAgentIntent));
    setAgentBusy(true);
    setLaunchStep("confirm");
    try {
      const operation = await createAgentWorkspace(input, session.csrfToken, idempotencyKey);
      if (!stillCurrent()) return;
      setAgentOperation(operation);
      sessionStorage.setItem(agentOperationStorageKey, JSON.stringify({ operationId: operation.operationId, quoteId: agentQuote.id, input, idempotencyKey } satisfies PersistedAgentIntent));
      if (!operationIsTerminal(operation)) void pollAgentOperation(operation.operationId, currentRequestGeneration(), session);
      else if (operation.status !== "succeeded") setAgentPollIssue("unknown");
    } catch (error) {
      if (stillCurrent()) flash(friendlyError(error), "danger");
    } finally {
      if (stillCurrent()) setAgentBusy(false);
    }
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
    prepareNewWorkspaceLaunch: () => { setAgentOperation(null); setAgentWorkspace(null); setAgentAccess(null); setLaunchStep("configure"); setAgentConfirmed(false); },
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
    reviewAgentLaunch,
    submitAgentLaunch,
    openAgentWorkspace,
    loadCatalog,
    recover,
    reset
  };
}
