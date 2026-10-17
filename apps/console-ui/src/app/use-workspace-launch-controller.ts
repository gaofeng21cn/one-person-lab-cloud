import { useRef, useState } from "react";

import type {
  AuthSession,
  GatewayWallet,
  PlanId,
  PricingCatalogResponse,
  SourceEnvelope,
  CapabilityVersionDTO,
  ComputePlanDTO,
  StoragePlanDTO,
  LaunchModelDTO,
  WorkspacePricePreview,
  WorkspaceQuoteDTO,
  WorkspaceOwnerOperationDTO,
  WorkspaceOwnerDTO,
  WorkspaceOwnerAccessDTO,
  GatewayWalletReadbackDTO,
  RuntimeVersionDTO,
  WorkspaceLaunchResponse
} from "../api/dtos.ts";
import { getPricingCatalog, packagePricingReadPlan, previewPricing } from "../api/console-read-api.ts";
import { cloudIdentity } from "./console-identity.ts";
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
  listApprovedRuntimeVersions,
  listReadyCapabilityVersions,
  workspaceLaunchIdempotencyKey
} from "../api/workspaces-api.ts";
import type { RemoteState, WorkspaceLaunchController, WorkspaceLaunchStep } from "./console-controller-types.ts";
import {
  canCreateAgentQuote,
  canCreateAgentWorkspace,
  canSubmitWorkspaceLaunch,
  operationPollDelayMs,
  workspaceLaunchBalance,
  workspaceLaunchPricingSelection,
  workspaceLaunchPreviewRequest,
  workspaceLaunchReviewBlocked,
  workspaceLaunchReviewReadiness,
  workspaceLaunchSubmission,
  workspacePricePreview,
  type AgentLaunchReadiness,
  type WorkspaceLaunchReviewState
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
  agentRuntimeVersions: RuntimeVersionDTO[];
  agentApplicationKind: "opl_app" | "agent";
  setAgentApplicationKind: (value: "opl_app" | "agent") => void;
  agentRuntimeVersionId: string;
  setAgentRuntimeVersionId: (value: string) => void;
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

// A package purchase is prepaid from the customer's own Gateway balance. An
// unavailable envelope carries no balance rather than a zero balance.
function gatewayBalanceMicros(wallet: RemoteState<SourceEnvelope<GatewayWallet>>): string | null {
  const value = wallet.value;
  return value && "data" in value ? value.data.usdMicros : null;
}

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
  wallet,
  isRequestCurrent,
  currentMutationRequest,
  currentRequestGeneration,
  navigate,
  flash,
  friendlyError
}: WorkspaceLaunchDependencies): WorkspaceLaunchCapability {
  const [catalog, setCatalog] = useState<RemoteState<PricingCatalogResponse>>(emptyCatalog);
  const [previews, setPreviews] = useState<Partial<Record<PlanId, WorkspacePricePreview>>>({});
  const [launchName, setLaunchName] = useState("");
  const [launchAutoRenew, setLaunchAutoRenew] = useState(false);
  const [launchStep, setLaunchStep] = useState<WorkspaceLaunchStep>("configure");
  const [launchConfirmed, setLaunchConfirmed] = useState(false);
  const [agentCapabilityVersions, setAgentCapabilityVersions] = useState<CapabilityVersionDTO[]>([]);
  const [agentRuntimeVersions, setAgentRuntimeVersions] = useState<RuntimeVersionDTO[]>([]);
  const [agentApplicationKind, setAgentApplicationKindState] = useState<"opl_app" | "agent">("opl_app");
  const [agentRuntimeVersionId, setAgentRuntimeVersionIdState] = useState("");
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

  // Normal workspace launch state
  const [normalLaunchPlan, setNormalLaunchPlan] = useState<PlanId>("basic");
  const [normalLaunchBusy, setNormalLaunchBusy] = useState(false);
  const [normalLaunchOperation, setNormalLaunchOperation] = useState<WorkspaceLaunchResponse | null>(null);

  const invalidateQuote = () => {
    setAgentQuote(null);
    setAgentConfirmed(false);
    setLaunchStep("configure");
  };

  const reset = () => {
    setCatalog(emptyCatalog());
    setPreviews({});
    setLaunchName("");
    setLaunchAutoRenew(false);
    setLaunchStep("configure");
    setLaunchConfirmed(false);
    setAgentCapabilityVersions([]);
    setAgentRuntimeVersions([]);
    setAgentApplicationKindState("opl_app");
    setAgentRuntimeVersionIdState("");
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

  // The legacy package pricing routes are the authority for the package plans and
  // for every price shown before confirmation. They require a retained Control
  // Plane session, so a CloudIdentity build must not call them: Cloud has no
  // package-pricing catalog owner yet, and the cloud launch surface prices the
  // same purchase through the Resource Catalog quote instead. The refusal is an
  // explicit unavailable state, never a fallback to the session the build does
  // not have. A package is only offered when the owner reports a fabric compute
  // pool for it, so a plan can read back as unavailable and earn no preview. A
  // plan the owner did not price stays unpriced, and an unread price is reported
  // through catalog.error so the page says so and offers a retry instead of
  // leaving a dead button unexplained.
  const loadPricingCatalog = async (generation: number, activeSession: AuthSession) => {
    const plan = packagePricingReadPlan(cloudIdentity);
    if (plan.state === "unavailable") {
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setCatalog({ value: null, loading: false, error: plan.reasonCode });
      setPreviews({});
      return;
    }
    setCatalog((current) => ({ ...current, loading: true, error: "" }));
    try {
      const priced = await getPricingCatalog();
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      const entries = await Promise.all(priced.packages.filter((plan) => plan.available).map(async (plan) => {
        try {
          const preview = await previewPricing(workspaceLaunchPreviewRequest(plan.id), activeSession.csrfToken);
          return [plan.id, workspacePricePreview(preview)] as const;
        } catch {
          return [plan.id, null] as const;
        }
      }));
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      const unread = entries.filter((entry) => entry[1] === null).map(([planId]) => planId);
      setPreviews(Object.fromEntries(entries.filter((entry): entry is readonly [PlanId, WorkspacePricePreview] => entry[1] !== null)));
      setCatalog({
        value: priced,
        loading: false,
        error: unread.length ? `unpriced_packages:${unread.join(",")}` : ""
      });
    } catch (error) {
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setCatalog({ value: null, loading: false, error: friendlyError(error) });
      setPreviews({});
    }
  };

  const loadAgentCatalog = async (generation: number, activeSession: AuthSession) => {
    setAgentSourceLoading(true);
    setAgentSourceError("");
    try {
      const [versions, releases, compute, storage, models, wallet] = await Promise.all([
        listReadyCapabilityVersions(), listApprovedRuntimeVersions(), listAvailableComputePlans(), listAvailableStoragePlans(), listAvailableLaunchModels(), getLaunchWallet()
      ]);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      const availableVersions = versions.filter((version) => version.status === "ready");
      const availableReleases = releases.filter((release) => release.status === "approved");
      const availableCompute = compute.filter((plan) => plan.availability === "available");
      const availableStorage = storage.filter((plan) => plan.availability === "available");
      const availableModels = models.filter((model) => model.available);
      // Two application combinations share one launch: the default OPL App needs
      // an approved Runtime Release, a built Agent needs a ready CapabilityVersion.
      // Resource plans and models are required for both, but neither application
      // combination is fabricated to satisfy the other.
      if ((!availableReleases.length && !availableVersions.length) || !availableCompute.length || !availableStorage.length || !availableModels.length) {
        throw new Error("agent_launch_catalog_empty");
      }
      setAgentCapabilityVersions(availableVersions);
      setAgentRuntimeVersions(availableReleases);
      setAgentComputePlans(availableCompute);
      setAgentStoragePlans(availableStorage);
      setAgentModels(availableModels);
      setAgentWallet(wallet);
      setAgentApplicationKindState((current) => current === "agent" ? (availableVersions.length ? "agent" : "opl_app") : (availableReleases.length ? "opl_app" : "agent"));
      setAgentRuntimeVersionIdState((current) => current && availableReleases.some((r) => r.id === current) ? current : (availableReleases[0]?.id ?? ""));
      setAgentCapabilityVersionIdState((current) => current && availableVersions.some((v) => v.id === current) ? current : (availableVersions[0]?.id ?? ""));
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

  // The package plans and the Agent source plans are different products that share
  // the Control Plane pricing surface. Neither read decides whether the other is
  // available, and neither one's balance answers for the other's purchase.
  const loadCatalog = async (generation: number, activeSession: AuthSession) => {
    await Promise.all([loadPricingCatalog(generation, activeSession), loadAgentCatalog(generation, activeSession)]);
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

  const setAgentApplicationKind = (value: "opl_app" | "agent") => { setAgentApplicationKindState(value); invalidateQuote(); };
  const setAgentRuntimeVersionId = (value: string) => { setAgentRuntimeVersionIdState(value); invalidateQuote(); };
  const setAgentCapabilityVersionId = (value: string) => { setAgentCapabilityVersionIdState(value); invalidateQuote(); };
  const setAgentComputePlanId = (value: string) => { setAgentComputePlanIdState(value); invalidateQuote(); };
  const setAgentStoragePlanId = (value: string) => { setAgentStoragePlanIdState(value); invalidateQuote(); };
  const setAgentModelSelection = (slot: string, modelId: string) => { setAgentModelSelections((current) => ({ ...current, [slot]: modelId })); invalidateQuote(); };

  const selectedCapability = agentCapabilityVersions.find((version) => version.id === agentCapabilityVersionId) || null;
  const selectedRelease = agentRuntimeVersions.find((release) => release.id === agentRuntimeVersionId) || null;
  const isDefaultApp = agentApplicationKind === "opl_app";
  // A built Agent declares its model requirement slots, so every required slot must
  // be chosen from its allowed models. The default OPL App ships its own model
  // configuration, so it carries no per-slot requirement and selects no models.
  const modelSelections = isDefaultApp || !selectedCapability ? [] : selectionList(selectedCapability.modelRequirements, agentModelSelections);
  const modelSelectionsReady = isDefaultApp
    ? Boolean(selectedRelease)
    : Boolean(selectedCapability) && selectedCapability.modelRequirements.every((requirement) => {
      const selection = modelSelections.find((item) => item.slot === requirement.slot);
      return !requirement.required && !selection || Boolean(selection && requirement.allowedModelIds.length > 0 && requirement.allowedModelIds.includes(selection.modelId));
    });
  const readiness: AgentLaunchReadiness = {
    sourceReady: !agentSourceLoading && !agentSourceError && !agentRecoveryPending,
    hasName: Boolean(launchName.trim()),
    hasCapabilityVersion: isDefaultApp ? Boolean(selectedRelease) : Boolean(selectedCapability),
    hasComputePlan: Boolean(agentComputePlanId),
    hasStoragePlan: Boolean(agentStoragePlanId),
    modelSelectionsReady,
    walletReadbackReady: Boolean(agentWallet),
    walletSufficient: Boolean(agentWallet && agentQuote && BigInt(agentWallet.balanceUSDMicros) >= BigInt(agentQuote.totalUSDMicros)),
    quoteReady: Boolean(agentQuote),
    quoteCurrent: Boolean(agentQuote && agentQuote.status === "offered" && new Date(agentQuote.expiresAt).getTime() > Date.now())
  };

  const reviewAgentLaunch = () => {
    if (!session || intent.current || !canCreateAgentQuote(readiness) || (isDefaultApp ? !selectedRelease : !selectedCapability)) return;
    setAgentBusy(true);
    // Capture current selection snapshot to validate against late-arriving quotes
    const requestedApplicationKind = agentApplicationKind;
    const requestedSourceId = isDefaultApp ? agentRuntimeVersionId : agentCapabilityVersionId;
    const requestedComputePlanId = agentComputePlanId;
    const requestedStoragePlanId = agentStoragePlanId;
    const applicationSelection = isDefaultApp
      ? { kind: "opl_app" as const, runtimeVersionId: selectedRelease!.id }
      : { kind: "agent" as const, capabilityVersionId: selectedCapability!.id };
    void createWorkspaceQuote({
      purpose: "deploy", applicationSelection, computePlanId: agentComputePlanId,
      storagePlanId: agentStoragePlanId, modelSelections, periodMonths: 1
    }, session.csrfToken, `workspace-quote:${crypto.randomUUID()}`).then((quote) => {
      // Reject late-arriving quote if selection changed while quote was in flight
      const currentSourceId = agentApplicationKind === "opl_app" ? agentRuntimeVersionId : agentCapabilityVersionId;
      if (requestedApplicationKind !== agentApplicationKind ||
          requestedSourceId !== currentSourceId ||
          requestedComputePlanId !== agentComputePlanId ||
          requestedStoragePlanId !== agentStoragePlanId) {
        // Silently discard stale quote; user already changed selection and invalidateQuote() cleared agentQuote
        setAgentBusy(false);
        return;
      }
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

  // Normal workspace launch implementation
  const reviewNormalWorkspaceLaunch = () => {
    if (workspaceLaunchReviewBlocked(reviewState)) return;
    setLaunchStep("confirm");
  };

  const submitNormalWorkspaceLaunch = async () => {
    if (!session || !canSubmitWorkspaceLaunch({
      ...workspaceLaunchReviewReadiness(reviewState),
      sessionAvailable: true,
      busy: launchBusy,
      step: launchStep,
      confirmed: launchConfirmed
    })) return;

    const stillCurrent = currentMutationRequest();
    setNormalLaunchBusy(true);

    try {
      const { launchWorkspace } = await import("../api/workspaces-api.ts");
      const operation = await launchWorkspace(
        workspaceLaunchSubmission(
          { name: launchName.trim(), packageId: normalLaunchPlan, autoRenew: launchAutoRenew },
          catalog.value?.resourceBillingMode ?? "enabled"
        ),
        session.csrfToken,
        workspaceLaunchIdempotencyKey()
      );

      if (!stillCurrent()) {
        return;
      }

      setNormalLaunchOperation(operation);
      setLaunchStep("confirm");
      flash("工作空间创建请求已提交", "good");
    } catch (error) {
      if (!stillCurrent()) return;
      flash(friendlyError(error), "danger");
    } finally {
      if (stillCurrent()) {
        setNormalLaunchBusy(false);
      }
    }
  };

  const openNormalWorkspace = async () => {
    if (!normalLaunchOperation?.workspaceId) return;
    navigate(`/console/workspaces/${normalLaunchOperation.workspaceId}`);
  };

  // Determine selectedPlan from catalog
  const { selectedPlan, selectedPrice } = workspaceLaunchPricingSelection({ catalog: catalog.value, planId: normalLaunchPlan, previews });
  const customerOwned = false;
  const balanceSufficient = workspaceLaunchBalance({
    resourceBillingMode: catalog.value?.resourceBillingMode,
    customerOwned,
    selectedPrice,
    walletUsdMicros: gatewayBalanceMicros(wallet)
  });
  const launchBusy = normalLaunchBusy || agentBusy;
  const reviewState: WorkspaceLaunchReviewState = {
    launchRecoveryState: "clear",
    launchName,
    selectedPlan,
    selectedPrice,
    balanceSufficient
  };

  return {
    catalog,
    previews,
    launchName,
    setLaunchName,
    launchPlan: normalLaunchPlan,
    setLaunchPlan: setNormalLaunchPlan,
    launchAutoRenew,
    setLaunchAutoRenew: (value) => { setLaunchAutoRenew(value); invalidateQuote(); },
    launchStep,
    setLaunchStep,
    launchConfirmed,
    setLaunchConfirmed,
    selectedPlan,
    selectedPrice,
    walletUsdMicros: gatewayBalanceMicros(wallet),
    balanceSufficient,
    customerOwned,
    launchOperation: normalLaunchOperation,
    launchRecoveryState: "clear",
    launchPollIssue: "",
    launchSessionAvailable: Boolean(session),
    busy: launchBusy,
    reviewWorkspaceLaunch: reviewNormalWorkspaceLaunch,
    submitWorkspaceLaunch: submitNormalWorkspaceLaunch,
    openLaunchedWorkspace: openNormalWorkspace,
    openLaunchBilling: () => navigate("/console/billing"),
    prepareNewWorkspaceLaunch: () => { if (agentRecoveryPending) return; setAgentOperation(null); setAgentWorkspace(null); setAgentAccess(null); setLaunchStep("configure"); setAgentConfirmed(false); },
    agentCapabilityVersions,
    agentRuntimeVersions,
    agentApplicationKind,
    setAgentApplicationKind,
    agentRuntimeVersionId,
    setAgentRuntimeVersionId,
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
