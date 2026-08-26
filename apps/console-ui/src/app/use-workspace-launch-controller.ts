import { useRef, useState } from "react";

import { getPricingCatalog, previewPricing } from "../api/console-read-api.ts";
import type {
  AuthSession,
  GatewayWallet,
  PlanId,
  PricingCatalogResponse,
  SourceEnvelope,
  WorkspaceLaunchResponse,
  WorkspacePricePreview
} from "../api/dtos.ts";
import {
  findWorkspaceInPages,
  getWorkspaceLaunch,
  getWorkspaceLaunches,
  launchWorkspace,
  workspaceLaunchIdempotencyKey
} from "../api/workspaces-api.ts";
import { hasSufficientWorkspaceLaunchBalance } from "../console-model.ts";
import type { RemoteState, WorkspaceLaunchController, WorkspaceLaunchStep } from "./console-controller-types.ts";
import {
  classifyWorkspaceLaunchRecovery,
  resolveWorkspaceLaunchIntent,
  shouldPollWorkspaceLaunch,
  shouldRetainWorkspaceLaunchIntent,
  workspaceLaunchSubmission,
  type WorkspaceLaunchIntent
} from "./workspace-launch-controller-model.ts";

const workspaceLaunchPollIntervalMs = 10_000;
const workspaceLaunchPollAttempts = 30;

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

export interface WorkspaceLaunchCapability extends WorkspaceLaunchController {
  loadCatalog: (generation: number, activeSession: AuthSession) => Promise<void>;
  recover: (generation: number, activeSession: AuthSession) => Promise<void>;
  reset: () => void;
}

const emptyCatalog = (): RemoteState<PricingCatalogResponse> => ({ value: null, loading: false, error: "" });

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
  const [launchPlan, setLaunchPlan] = useState<PlanId>("basic");
  const [launchAutoRenew, setLaunchAutoRenew] = useState(false);
  const [launchStep, setLaunchStep] = useState<WorkspaceLaunchStep>("configure");
  const [launchConfirmed, setLaunchConfirmed] = useState(false);
  const [launchOperation, setLaunchOperation] = useState<WorkspaceLaunchResponse | null>(null);
  const [launchPollIssue, setLaunchPollIssue] = useState<"" | "error" | "timeout" | "readback">("");
  const [busy, setBusy] = useState(false);
  const intent = useRef<WorkspaceLaunchIntent | null>(null);

  const reset = () => {
    setCatalog(emptyCatalog());
    setPreviews({});
    setLaunchName("");
    setLaunchPlan("basic");
    setLaunchAutoRenew(false);
    setLaunchStep("configure");
    setLaunchConfirmed(false);
    setLaunchOperation(null);
    setLaunchPollIssue("");
    setBusy(false);
    intent.current = null;
  };

  const loadCatalog = async (generation: number, activeSession: AuthSession) => {
    setCatalog((current) => ({ ...current, loading: true, error: "" }));
    setPreviews({});
    try {
      const value = await getPricingCatalog();
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      setCatalog({ value, loading: false, error: "" });
      if (value.resourceBillingMode === "none") setLaunchAutoRenew(false);
      if (!value.packages.some((plan) => plan.id === launchPlan && plan.available)) {
        const firstAvailablePlan = value.packages.find((plan) => plan.available);
        if (firstAvailablePlan) setLaunchPlan(firstAvailablePlan.id);
      }
      const entries = await Promise.all(value.packages.filter((plan) => plan.available).map(async (plan) => {
        const preview = await previewPricing({ resourceType: "workspace", packageId: plan.id }, activeSession.csrfToken);
        return [plan.id, preview] as const;
      }));
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      const next: Partial<Record<PlanId, WorkspacePricePreview>> = {};
      for (const [planId, preview] of entries) {
        if (typeof preview.totalChargeUsdMicros === "number") next[planId] = preview as WorkspacePricePreview;
      }
      setPreviews(next);
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) {
        setCatalog((current) => ({ ...current, value: null, loading: false, error: friendlyError(error) }));
      }
    }
  };

  const confirmReadback = async (workspaceId: string, generation: number, activeSession: AuthSession) => {
    try {
      const detail = await findWorkspaceInPages(workspaceId);
      if (!isRequestCurrent(generation, activeSession.user.id)) return false;
      if (!detail.available || detail.data === null) {
        setLaunchPollIssue("readback");
        flash("开通操作已完成，但 Workspace 权威回读尚未确认", "danger");
        return false;
      }
      setLaunchPollIssue("");
      flash("Workspace 已开通");
      navigate(`/console/workspaces/${encodeURIComponent(workspaceId)}`);
      return true;
    } catch {
      if (isRequestCurrent(generation, activeSession.user.id)) {
        setLaunchPollIssue("readback");
        flash("开通操作已完成，但 Workspace 权威回读尚未确认", "danger");
      }
      return false;
    }
  };

  const poll = async (operationId: string, generation: number, activeSession: AuthSession) => {
    setLaunchPollIssue("");
    for (let attempt = 0; attempt < workspaceLaunchPollAttempts; attempt += 1) {
      await new Promise<void>((resolve) => window.setTimeout(resolve, workspaceLaunchPollIntervalMs));
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      try {
        const operation = await getWorkspaceLaunch(operationId);
        if (!isRequestCurrent(generation, activeSession.user.id)) return;
        setLaunchOperation(operation);
        if (!shouldPollWorkspaceLaunch(operation)) {
          if (operation.status === "succeeded" && operation.workspaceId) {
            await confirmReadback(operation.workspaceId, generation, activeSession);
          } else if (operation.status === "refunded") {
            flash("Workspace 未完成，已退款", "danger");
          }
          return;
        }
      } catch (error) {
        if (isRequestCurrent(generation, activeSession.user.id)) {
          setLaunchPollIssue("error");
          flash(friendlyError(error), "danger");
        }
        return;
      }
    }
    if (isRequestCurrent(generation, activeSession.user.id)) setLaunchPollIssue("timeout");
  };

  const recover = async (generation: number, activeSession: AuthSession) => {
    setLaunchPollIssue("");
    try {
      const recovery = classifyWorkspaceLaunchRecovery(await getWorkspaceLaunches());
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      if (recovery.kind === "none") {
        setLaunchOperation(null);
        return;
      }
      if (recovery.kind === "conflict") {
        setLaunchPollIssue("error");
        return;
      }
      setLaunchOperation(recovery.operation);
      if (shouldPollWorkspaceLaunch(recovery.operation)) {
        void poll(recovery.operation.operationId, generation, activeSession);
      }
    } catch {
      if (isRequestCurrent(generation, activeSession.user.id)) setLaunchPollIssue("error");
    }
  };

  const selectedPlan = catalog.value?.packages.find((plan) => plan.id === launchPlan && plan.available) || null;
  const customerOwned = catalog.value?.resourceBillingMode === "none";
  const selectedPrice = selectedPlan ? (customerOwned ? 0 : previews[selectedPlan.id]?.totalChargeUsdMicros ?? null) : null;
  const walletValue = wallet.value?.available ? wallet.value.data : null;
  const balanceSufficient = customerOwned ? true : walletValue && selectedPrice !== null
    ? hasSufficientWorkspaceLaunchBalance(walletValue.usdMicros, selectedPrice)
    : walletValue ? false : null;

  const reviewWorkspaceLaunch = () => {
    if (!launchName.trim() || !selectedPlan || selectedPrice === null || balanceSufficient !== true) return;
    setLaunchConfirmed(false);
    setLaunchStep("confirm");
  };

  const submitWorkspaceLaunch = async () => {
    if (!session || busy || launchStep !== "confirm" || !launchConfirmed || !selectedPlan || selectedPrice === null || balanceSufficient !== true || !launchName.trim()) return;
    const requestStillCurrent = currentMutationRequest();
    const input = workspaceLaunchSubmission({
      name: launchName.trim(),
      packageId: selectedPlan.id,
      autoRenew: launchAutoRenew
    }, catalog.value?.resourceBillingMode);
    const resolution = resolveWorkspaceLaunchIntent(intent.current, input, workspaceLaunchIdempotencyKey);
    if (resolution.kind === "conflict") {
      flash("上次 Workspace 开通结果待确认，请按原配置重试", "danger");
      return;
    }
    intent.current = resolution.intent;
    setBusy(true);
    try {
      const operation = await launchWorkspace(input, session.csrfToken, resolution.intent.idempotencyKey);
      if (!requestStillCurrent()) return;
      intent.current = null;
      setLaunchOperation(operation);
      if (operation.status === "succeeded" && operation.workspaceId) {
        await confirmReadback(operation.workspaceId, currentRequestGeneration(), session);
      } else if (operation.status === "refunded") {
        flash("Workspace 未完成，已退款", "danger");
      } else if (shouldPollWorkspaceLaunch(operation)) {
        void poll(operation.operationId, currentRequestGeneration(), session);
      }
    } catch (error) {
      if (!requestStillCurrent()) return;
      if (!shouldRetainWorkspaceLaunchIntent(error)) intent.current = null;
      flash(friendlyError(error), "danger");
    } finally {
      if (requestStillCurrent()) setBusy(false);
    }
  };

  const openLaunchedWorkspace = async () => {
    if (!session || !launchOperation?.workspaceId) return;
    await confirmReadback(launchOperation.workspaceId, currentRequestGeneration(), session);
  };

  return {
    catalog,
    previews,
    launchName,
    setLaunchName,
    launchPlan,
    setLaunchPlan,
    launchAutoRenew,
    setLaunchAutoRenew,
    launchStep,
    setLaunchStep,
    launchConfirmed,
    setLaunchConfirmed,
    selectedPlan,
    selectedPrice,
    walletUsdMicros: walletValue?.usdMicros || null,
    balanceSufficient,
    customerOwned,
    launchOperation,
    launchPollIssue,
    busy,
    reviewWorkspaceLaunch,
    submitWorkspaceLaunch,
    openLaunchedWorkspace,
    loadCatalog,
    recover,
    reset
  };
}
