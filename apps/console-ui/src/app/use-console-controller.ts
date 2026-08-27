import { useEffect, useMemo, useRef, useState } from "react";

import { currentSession, login as loginRequest, logoutAndConfirm } from "../api/auth-api.ts";
import {
  getGatewayAccountUsageSummary,
  getGatewayBalanceHistory,
  getGatewayEndpoint,
  getGatewayWallet,
  getOperatorHealth,
  getOperatorOverview,
  getOperatorReconciliation,
  getOperatorWorkspace,
  getOperatorWorkspaceRuntimeImagePolicy,
  getOperatorWorkspaceRuntimeImageReplacementPreview,
  getOperatorWorkspaces
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  SourceEnvelope
} from "../api/dtos.ts";
import {
  findWorkspaceInPages,
  getWorkspaceGatewayBudget,
  getWorkspaces,
  getWorkspaceRuntimeStatus
} from "../api/workspaces-api.ts";
import { defaultAuthenticatedRoute, needsSession, workspaceIdFromPath, workspacePage } from "../console-model.ts";
import { isKnownConsoleRoute, isSensitiveConsoleRoute, useConsoleRouter } from "./console-router.ts";
import type { AuthStatus, BillingController, ConsoleSources, CustomerAnnouncementController, GatewayUsageController, GlobalSlide, OperatorAccountController, OperatorAnnouncementController, RemoteState, SupportController, WalletAdjustmentController, WorkspaceBudgetController, WorkspaceDeleteController, WorkspaceLaunchController, WorkspaceRenewalController, WorkspaceRuntimeImageReplacementController, WorkspaceSecretController, WorkspaceSourceProjectionLease } from "./console-controller-types.ts";
import { useBillingController } from "./use-billing-controller.ts";
import { useCustomerAnnouncementController } from "./use-customer-announcement-controller.ts";
import { useGatewayUsageController } from "./use-gateway-usage-controller.ts";
import { useOperatorAccountController } from "./use-operator-account-controller.ts";
import { useOperatorAnnouncementController } from "./use-operator-announcement-controller.ts";
import { useWorkspaceBudgetController } from "./use-workspace-budget-controller.ts";
import { useWorkspaceDeleteController } from "./use-workspace-delete-controller.ts";
import { useWorkspaceLaunchController } from "./use-workspace-launch-controller.ts";
import { useWorkspaceRenewalController } from "./use-workspace-renewal-controller.ts";
import { useWorkspaceRuntimeImageReplacementController } from "./use-workspace-runtime-image-replacement-controller.ts";
import { useWorkspaceSecretController } from "./use-workspace-secret-controller.ts";
import { useWalletAdjustmentController } from "./use-wallet-adjustment-controller.ts";
import { useSupportController } from "./use-support-controller.ts";

const operatorPageSize = 20;

const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

function initialSources(): ConsoleSources {
  return {
    workspaces: emptyRemote(),
    workspaceDetail: emptyRemote(),
    runtime: emptyRemote(),
    workspaceBudget: emptyRemote(),
    wallet: emptyRemote(),
    accountUsage: emptyRemote(),
    balanceHistory: emptyRemote(),
    endpoint: emptyRemote(),
    operatorOverview: emptyRemote(),
    operatorWorkspaces: emptyRemote(),
    operatorWorkspaceDetail: emptyRemote(),
    operatorWorkspaceImagePolicy: emptyRemote(),
    operatorWorkspaceImagePreview: emptyRemote(),
    operatorReconciliation: emptyRemote(),
    operatorHealth: emptyRemote()
  };
}

export function unavailableSource<T>(source: string): SourceEnvelope<T> {
  const normalizedSource = source.trim().toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "unknown";
  return {
    source,
    status: "unavailable",
    available: false,
    fetchedAt: new Date().toISOString(),
    reasonCode: `${normalizedSource}_unavailable`
  };
}

export function friendlyError(error: unknown) {
  const raw = String(error && typeof error === "object" && "message" in error ? error.message : error || "request_failed");
  const messages: Record<string, string> = {
    not_authenticated: "登录已失效，请重新登录",
    account_scope_forbidden: "没有权限访问该资源",
    workspace_purchase_not_enabled: "该账户尚未获得 Workspace 新购资格",
    insufficient_balance: "可用余额不足",
    monthly_balance_insufficient: "可用余额不足",
    gateway_key_missing: "API Key 尚未就绪",
    gateway_key_ambiguous: "API Key 状态异常，请联系管理员",
    monthly_account_unmapped: "API 服务尚未开通",
    authentication_unavailable: "身份服务暂不可用，请稍后重试",
    workspace_not_found: "Workspace 不存在或无权访问",
    workspace_credentials_unavailable: "Workspace 凭证暂不可用",
    workspace_not_running: "Workspace 尚未就绪",
    workspace_reactivation_required: "Workspace 已到期，需先完成重新激活",
    upstream_unavailable: "服务暂不可用，请稍后重试"
  };
  return messages[raw] || (raw.includes("failed") || raw.includes("_") ? "请求失败，请重试" : raw);
}

function apiErrorCode(error: unknown) {
  const payload = error && typeof error === "object" && "payload" in error
    ? (error as { payload?: unknown }).payload
    : null;
  return payload && typeof payload === "object" ? String((payload as { error?: unknown }).error || "") : "";
}

function mutationError(error: unknown) {
  const code = apiErrorCode(error);
  return code ? friendlyError(code) : "结果待确认，请刷新操作状态，不要重复提交";
}

export function useConsoleController() {
  const { path, navigate } = useConsoleRouter();
  const [session, setSession] = useState<AuthSession | null>(null);
  const [authStatus, setAuthStatus] = useState<AuthStatus>(needsSession(path) ? "checking" : "public");
  const [authError, setAuthError] = useState("");
  const [sources, setSources] = useState<ConsoleSources>(initialSources);
  const [toast, setToast] = useState<{ text: string; tone: "good" | "danger" }>({ text: "", tone: "good" });
  const [globalSlide, setGlobalSlide] = useState<GlobalSlide>("");
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [workspacePageNumber, setWorkspacePageNumber] = useState(1);
  const [balanceHistoryPage, setBalanceHistoryPage] = useState(1);
  const [operatorWorkspacePage, setOperatorWorkspacePage] = useState(1);
  const [selectedOperatorWorkspaceId, setSelectedOperatorWorkspaceId] = useState("");

  const requestGeneration = useRef(0);
  const sessionGeneration = useRef(0);
  const loginAttemptGeneration = useRef(0);
  const loginAbortController = useRef<AbortController | null>(null);
  const logoutAttemptGeneration = useRef(0);
  const logoutInFlight = useRef(false);
  const logoutState = useRef<"idle" | "pending" | "unconfirmed">("idle");
  const sessionRef = useRef<AuthSession | null>(null);
  const workspaceDetailRequestGeneration = useRef(0);
  const workspaceRuntimeRequestGeneration = useRef(0);
  const workspaceBudgetRequestGeneration = useRef(0);
  const selectedOperatorWorkspaceIdRef = useRef("");
  const toastTimer = useRef<number | undefined>(undefined);

  const updateSource = <K extends keyof ConsoleSources>(key: K, patch: Partial<RemoteState<ConsoleSources[K]["value"]>>) => {
    setSources((current) => ({ ...current, [key]: { ...current[key], ...patch } } as ConsoleSources));
  };

  const beginSource = <K extends keyof ConsoleSources>(key: K) => updateSource(key, { loading: true, error: "" });

  const failSource = <K extends keyof ConsoleSources>(key: K, error: unknown, fallback?: ConsoleSources[K]["value"]) => {
    updateSource(key, { loading: false, error: friendlyError(error), ...(fallback !== undefined ? { value: fallback } : {}) });
  };

  const flash = (text: string, tone: "good" | "danger" = "good") => {
    setToast({ text, tone });
    if (toastTimer.current) window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(() => setToast({ text: "", tone: "good" }), 3200);
  };

  const resetConsoleState = () => {
    workspaceSecretCapability.reset();
    supportCapability.reset();
    setSources(initialSources());
    setGlobalSlide("");
    workspaceLaunchCapability.reset();
    walletAdjustmentCapability.reset();
    workspaceDeleteCapability.reset();
    workspaceRenewalCapability.reset();
    workspaceRuntimeImageReplacementCapability.reset();
    workspaceBudgetCapability.reset();
    gatewayUsageCapability.reset();
    billingCapability.reset();
    operatorAccountCapability.reset();
    operatorAnnouncementCapability.reset();
    customerAnnouncementCapability.reset();
    setWorkspacePageNumber(1);
    setBalanceHistoryPage(1);
    setOperatorWorkspacePage(1);
    setSelectedOperatorWorkspaceId("");
    selectedOperatorWorkspaceIdRef.current = "";
    workspaceDetailRequestGeneration.current += 1;
    workspaceRuntimeRequestGeneration.current += 1;
    workspaceBudgetRequestGeneration.current += 1;
  };

  const invalidateLoginAttempt = () => {
    loginAttemptGeneration.current += 1;
    loginAbortController.current?.abort();
    loginAbortController.current = null;
  };

  const replaceSession = (next: AuthSession | null) => {
    invalidateLoginAttempt();
    logoutAttemptGeneration.current += 1;
    logoutInFlight.current = false;
    logoutState.current = "idle";
    sessionGeneration.current += 1;
    requestGeneration.current += 1;
    resetConsoleState();
    sessionRef.current = next;
    setSession(next);
  };

  const isRequestCurrent = (generation: number, userId?: string) => generation === requestGeneration.current
    && (!userId || sessionRef.current?.user.id === userId);

  const currentMutationRequest = () => {
    const generation = sessionGeneration.current;
    const userId = sessionRef.current?.user.id;
    return () => generation === sessionGeneration.current && userId === sessionRef.current?.user.id;
  };

  const workspaceDetailProjectionLease = (): WorkspaceSourceProjectionLease => {
    const generation = workspaceDetailRequestGeneration.current;
    return {
      isCurrent: () => generation === workspaceDetailRequestGeneration.current,
      commit: () => {
        if (generation !== workspaceDetailRequestGeneration.current) return false;
        workspaceDetailRequestGeneration.current += 1;
        return true;
      }
    };
  };

  const workspaceBudgetProjectionLease = (): WorkspaceSourceProjectionLease => {
    const generation = workspaceBudgetRequestGeneration.current;
    return {
      isCurrent: () => generation === workspaceBudgetRequestGeneration.current,
      commit: () => {
        if (generation !== workspaceBudgetRequestGeneration.current) return false;
        workspaceBudgetRequestGeneration.current += 1;
        return true;
      }
    };
  };

  const activeWorkspaceId = workspaceIdFromPath(path);
  const activeWorkspaceSource = sources.workspaceDetail.value;
  const activeWorkspace = activeWorkspaceSource?.available
    && activeWorkspaceSource.data?.id === activeWorkspaceId
    ? activeWorkspaceSource.data
    : null;

  const workspaceSecretCapability = useWorkspaceSecretController({
    session,
    workspace: activeWorkspace,
    activeWorkspaceId,
    currentMutationRequest,
    refreshWorkspaceDetail: async (workspaceId) => {
      if (!session) return;
      await loadWorkspaceDetail(requestGeneration.current, session, workspaceId);
    },
    flash,
    friendlyError,
    mutationError
  });
  const workspaceSecrets: WorkspaceSecretController = workspaceSecretCapability;

  const workspaceLaunchCapability = useWorkspaceLaunchController({
    session,
    wallet: sources.wallet,
    isRequestCurrent,
    currentMutationRequest,
    currentRequestGeneration: () => requestGeneration.current,
    navigate,
    flash,
    friendlyError
  });
  const workspaceLaunch: WorkspaceLaunchController = workspaceLaunchCapability;

  const workspaceDeleteCapability = useWorkspaceDeleteController({
    session,
    workspace: activeWorkspace,
    activeWorkspaceId,
    currentMutationRequest,
    navigate,
    flash,
    friendlyError
  });
  const workspaceDelete: WorkspaceDeleteController = workspaceDeleteCapability;

  const workspaceRenewalCapability = useWorkspaceRenewalController({
    session,
    workspace: activeWorkspace,
    activeWorkspaceId,
    currentMutationRequest,
    workspaceDetailProjectionLease,
    onWorkspaceReadback: (readback) => {
      if (!readback.available || !readback.data) return;
      const workspace = readback.data;
      setSources((current) => {
        const currentList = current.workspaces.value;
        const workspaces = currentList?.available
          ? {
              ...current.workspaces,
              value: {
                ...currentList,
                data: {
                  ...currentList.data,
                  items: currentList.data.items.map((item) => item.id === workspace.id ? workspace : item)
                }
              }
            }
          : current.workspaces;
        return { ...current, workspaces, workspaceDetail: { value: readback, loading: false, error: "" } };
      });
    },
    flash,
    mutationError
  });
  const workspaceRenewal: WorkspaceRenewalController = workspaceRenewalCapability;

  const workspaceBudgetCapability = useWorkspaceBudgetController({
    session,
    workspace: activeWorkspace,
    budget: sources.workspaceBudget.value,
    activeWorkspaceId,
    currentMutationRequest,
    workspaceBudgetProjectionLease,
    updateBudgetSource: (value) => updateSource("workspaceBudget", { value, loading: false, error: "" }),
    flash,
    mutationError
  });
  const workspaceBudget: WorkspaceBudgetController = workspaceBudgetCapability;

  const operatorAccountCapability = useOperatorAccountController({
    active: path === "/admin/accounts",
    currentSession: () => sessionRef.current,
    flash,
    friendlyError,
    mutationError,
    unavailableSource
  });
  const operatorAccounts: OperatorAccountController = operatorAccountCapability;

  const customerAnnouncementCapability = useCustomerAnnouncementController({
    scope: path === "/console" || path === "/console/overview"
      ? "overview"
      : path === "/console/announcements" ? "list" : "",
    currentSession: () => sessionRef.current,
    flash,
    friendlyError,
    unavailableSource
  });
  const customerAnnouncements: CustomerAnnouncementController = customerAnnouncementCapability;

  const operatorAnnouncementCapability = useOperatorAnnouncementController({
    active: path === "/admin" || path === "/admin/overview" || path === "/admin/announcements",
    currentSession: () => sessionRef.current,
    flash,
    friendlyError,
    mutationError,
    unavailableSource
  });
  const operatorAnnouncements: OperatorAnnouncementController = operatorAnnouncementCapability;

  const walletAdjustmentCapability = useWalletAdjustmentController({
    session,
    currentMutationRequest,
    refreshAccounts: operatorAccountCapability.refresh,
    flash,
    friendlyError,
    mutationError
  });
  const walletAdjustment: WalletAdjustmentController = walletAdjustmentCapability;

  const supportCapability = useSupportController({
    session,
    currentMutationRequest,
    flash,
    friendlyError,
    mutationError
  });
  const support: SupportController = supportCapability;

  const workspaceRuntimeImageReplacementCapability = useWorkspaceRuntimeImageReplacementController({
    session,
    workspaceId: selectedOperatorWorkspaceId,
    preview: sources.operatorWorkspaceImagePreview.value,
    currentMutationRequest,
    refreshWorkspace: async (workspaceId) => {
      if (!session) return;
      await openOperatorWorkspace(workspaceId);
    },
    refreshPreview: async (workspaceId) => {
      if (!session) return;
      await loadOperatorWorkspaceImagePreview(requestGeneration.current, session, workspaceId);
    },
    flash,
    mutationError
  });
  const workspaceRuntimeImageReplacement: WorkspaceRuntimeImageReplacementController = workspaceRuntimeImageReplacementCapability;

  const billingCapability = useBillingController({
    route: path === "/console" || path === "/console/overview"
      ? "overview"
      : path === "/console/billing" ? "billing" : "",
    currentSession: () => sessionRef.current,
    friendlyError,
    unavailableSource
  });
  const billing: BillingController = billingCapability;

  const gatewayUsageCapability = useGatewayUsageController({
    active: path === "/console/api/usage",
    currentSession: () => sessionRef.current,
    friendlyError,
    unavailableSource
  });
  const gatewayUsage: GatewayUsageController = gatewayUsageCapability;

  const loadWorkspaces = async (generation: number, activeSession: AuthSession, page = workspacePageNumber, pageSize = 10) => {
    beginSource("workspaces");
    try {
      const result = await getWorkspaces(page, pageSize);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      if (result.available && (result.data.page !== page || result.data.pageSize !== pageSize)) throw new Error("workspace_page_mismatch");
      updateSource("workspaces", { value: result, loading: false, error: "" });
      if (result.available) setWorkspacePageNumber(result.data.page);
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("workspaces", error, unavailableSource("control-plane"));
    }
  };

  const loadWallet = async (generation: number, activeSession: AuthSession) => {
    beginSource("wallet");
    try {
      const result = await getGatewayWallet();
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("wallet", { value: result, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("wallet", error, unavailableSource("sub2api"));
    }
  };

  const loadAccountUsage = async (generation: number, activeSession: AuthSession) => {
    beginSource("accountUsage");
    try {
      const result = await getGatewayAccountUsageSummary("month");
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("accountUsage", { value: result, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("accountUsage", error, unavailableSource("sub2api"));
    }
  };

  const loadBalanceHistory = async (generation: number, activeSession: AuthSession, page = balanceHistoryPage) => {
    beginSource("balanceHistory");
    try {
      const result = await getGatewayBalanceHistory(page, 20);
      if (!isRequestCurrent(generation, activeSession.user.id)) return;
      updateSource("balanceHistory", { value: result, loading: false, error: "" });
      if (result.available) setBalanceHistoryPage(result.data.page);
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("balanceHistory", error, unavailableSource("sub2api"));
    }
  };

  const loadWorkspaceRuntime = async (generation: number, activeSession: AuthSession, workspaceId: string) => {
    const runtimeGeneration = ++workspaceRuntimeRequestGeneration.current;
    const readStillCurrent = () => isRequestCurrent(generation, activeSession.user.id)
      && runtimeGeneration === workspaceRuntimeRequestGeneration.current
      && workspaceIdFromPath(window.location.pathname) === workspaceId;
    updateSource("runtime", { value: null, loading: true, error: "" });
    try {
      const result = await getWorkspaceRuntimeStatus(workspaceId);
      if (readStillCurrent()) updateSource("runtime", { value: result, loading: false, error: "" });
    } catch (error) {
      if (readStillCurrent()) failSource("runtime", error, unavailableSource("fabric"));
    }
  };

  const loadWorkspaceDetail = async (generation: number, activeSession: AuthSession, workspaceId: string) => {
    const workspaceDetailGeneration = ++workspaceDetailRequestGeneration.current;
    const workspaceBudgetGeneration = ++workspaceBudgetRequestGeneration.current;
    const routeStillCurrent = () => isRequestCurrent(generation, activeSession.user.id)
      && workspaceIdFromPath(window.location.pathname) === workspaceId;
    const detailReadStillCurrent = () => routeStillCurrent()
      && workspaceDetailGeneration === workspaceDetailRequestGeneration.current;
    const budgetReadStillCurrent = () => routeStillCurrent()
      && workspaceBudgetGeneration === workspaceBudgetRequestGeneration.current;
    beginSource("workspaceDetail");
    updateSource("workspaceBudget", { value: null, loading: false, error: "" });
    const detailRequest = findWorkspaceInPages(workspaceId);
    const runtimeRead = loadWorkspaceRuntime(generation, activeSession, workspaceId);
    try {
      let workspaceKeyId = "";
      try {
        const detail = await detailRequest;
        if (!routeStillCurrent()) return;
        if (detailReadStillCurrent()) {
          updateSource("workspaceDetail", { value: detail, loading: false, error: "" });
        }
        if (!detail.available || detail.data === null) {
          if (detailReadStillCurrent() && budgetReadStillCurrent()) {
            updateSource("workspaceBudget", { value: unavailableSource("sub2api"), loading: false, error: "" });
          }
          return;
        }
        workspaceKeyId = detail.data.workspaceApiKeyId || "";
      } catch (error) {
        if (!detailReadStillCurrent()) return;
        failSource("workspaceDetail", error, unavailableSource("control-plane"));
        if (budgetReadStillCurrent()) {
          updateSource("workspaceBudget", { value: unavailableSource("sub2api"), loading: false, error: "" });
        }
        return;
      }
      beginSource("workspaceBudget");
      try {
        const result = await getWorkspaceGatewayBudget(workspaceId, workspaceKeyId);
        if (budgetReadStillCurrent()) updateSource("workspaceBudget", { value: result, loading: false, error: "" });
      } catch (error) {
        if (budgetReadStillCurrent()) failSource("workspaceBudget", error, unavailableSource("sub2api"));
      }
    } finally {
      await runtimeRead;
    }
  };

  const loadEndpoint = async (generation: number, activeSession: AuthSession) => {
    beginSource("endpoint");
    try {
      const endpoint = await getGatewayEndpoint();
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("endpoint", { value: endpoint, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("endpoint", error, unavailableSource("sub2api"));
    }
  };

  const loadOperatorOverview = async (generation: number, activeSession: AuthSession) => {
    beginSource("operatorOverview");
    try {
      const result = await getOperatorOverview();
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("operatorOverview", { value: result, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("operatorOverview", error, unavailableSource("control-plane"));
    }
  };

  const loadOperatorWorkspaces = async (generation: number, activeSession: AuthSession, page = operatorWorkspacePage) => {
    beginSource("operatorWorkspaces");
    try {
      const result = await getOperatorWorkspaces(page, operatorPageSize);
      if (isRequestCurrent(generation, activeSession.user.id)) {
        updateSource("operatorWorkspaces", { value: result, loading: false, error: "" });
        setOperatorWorkspacePage(page);
      }
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("operatorWorkspaces", error, unavailableSource("control-plane+fabric+sub2api"));
    }
  };

  const loadOperatorWorkspaceImagePolicy = async (generation: number, activeSession: AuthSession) => {
    beginSource("operatorWorkspaceImagePolicy");
    try {
      const result = await getOperatorWorkspaceRuntimeImagePolicy();
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("operatorWorkspaceImagePolicy", { value: result, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("operatorWorkspaceImagePolicy", error, unavailableSource("control-plane"));
    }
  };

  const loadOperatorWorkspaceImagePreview = async (generation: number, activeSession: AuthSession, workspaceId: string) => {
    beginSource("operatorWorkspaceImagePreview");
    try {
      const result = await getOperatorWorkspaceRuntimeImageReplacementPreview(workspaceId);
      if (isRequestCurrent(generation, activeSession.user.id) && selectedOperatorWorkspaceIdRef.current === workspaceId) {
        updateSource("operatorWorkspaceImagePreview", { value: result, loading: false, error: "" });
      }
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id) && selectedOperatorWorkspaceIdRef.current === workspaceId) {
        failSource("operatorWorkspaceImagePreview", error, unavailableSource("control-plane+fabric"));
      }
    }
  };

  const loadOperatorReconciliation = async (generation: number, activeSession: AuthSession) => {
    beginSource("operatorReconciliation");
    try {
      const result = await getOperatorReconciliation();
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("operatorReconciliation", { value: result, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("operatorReconciliation", error, unavailableSource("control-plane"));
    }
  };

  const loadOperatorHealth = async (generation: number, activeSession: AuthSession) => {
    beginSource("operatorHealth");
    try {
      const result = await getOperatorHealth();
      if (isRequestCurrent(generation, activeSession.user.id)) updateSource("operatorHealth", { value: result, loading: false, error: "" });
    } catch (error) {
      if (isRequestCurrent(generation, activeSession.user.id)) failSource("operatorHealth", error, unavailableSource("control-plane"));
    }
  };

  const loadRoute = async (generation: number, activeSession: AuthSession, routePath: string) => {
    if (routePath === "/console" || routePath === "/console/overview") {
      await Promise.all([loadWorkspaces(generation, activeSession, 1, 1), loadWallet(generation, activeSession), loadAccountUsage(generation, activeSession), billingCapability.loadOverview(), customerAnnouncementCapability.load()]);
      return;
    }
    if (routePath === "/console/workspaces") {
      await Promise.all([loadWorkspaces(generation, activeSession, workspacePageNumber, 10), workspaceLaunchCapability.recover(generation, activeSession)]);
      return;
    }
    if (routePath === "/console/workspaces/new") {
      await Promise.all([loadWallet(generation, activeSession), workspaceLaunchCapability.loadCatalog(generation, activeSession), workspaceLaunchCapability.recover(generation, activeSession)]);
      return;
    }
    if (workspacePage(routePath) === "detail") {
      await loadWorkspaceDetail(generation, activeSession, workspaceIdFromPath(routePath));
      return;
    }
    if (routePath === "/console/api") {
      await Promise.all([loadWallet(generation, activeSession), loadAccountUsage(generation, activeSession), loadBalanceHistory(generation, activeSession, balanceHistoryPage), loadEndpoint(generation, activeSession)]);
      return;
    }
    if (routePath === "/console/api/usage") {
      await Promise.all([gatewayUsageCapability.load(), loadEndpoint(generation, activeSession)]);
      return;
    }
    if (routePath === "/console/billing") {
      await Promise.all([loadWorkspaces(generation, activeSession, 1, 10), billingCapability.loadBilling()]);
      return;
    }
    if (routePath === "/console/announcements") {
      await customerAnnouncementCapability.load();
      return;
    }
    if (routePath === "/admin" || routePath === "/admin/overview") {
      await Promise.all([loadOperatorOverview(generation, activeSession), operatorAnnouncementCapability.load()]);
      return;
    }
    if (routePath === "/admin/accounts") {
      await operatorAccountCapability.load();
      return;
    }
    if (routePath === "/admin/billing") {
      await loadOperatorReconciliation(generation, activeSession);
      return;
    }
    if (routePath === "/admin/resources") {
      await Promise.all([
        loadOperatorWorkspaces(generation, activeSession),
        loadOperatorWorkspaceImagePolicy(generation, activeSession)
      ]);
      return;
    }
    if (routePath === "/admin/announcements") {
      await operatorAnnouncementCapability.load();
      return;
    }
    if (routePath === "/admin/system") await loadOperatorHealth(generation, activeSession);
  };

  useEffect(() => {
    if (path !== "/login") invalidateLoginAttempt();
    const generation = ++requestGeneration.current;
    workspaceSecretCapability.clear();
    setSidebarOpen(false);
    setGlobalSlide("");
    if (logoutState.current !== "idle") return;
    if (!needsSession(path)) {
      setAuthStatus("public");
      setAuthError("");
      return;
    }
    setAuthStatus("checking");
    setAuthError("");
    const run = async () => {
      let activeSession = session;
      try {
        if (!activeSession) {
          activeSession = await currentSession();
          if (generation !== requestGeneration.current) return;
          if (!activeSession) {
            navigate(`/login?redirect=${encodeURIComponent(window.location.pathname + window.location.search)}`);
            return;
          }
          sessionRef.current = activeSession;
          setSession(activeSession);
        }
        if (path.startsWith("/admin") && activeSession.isOperator !== true) {
          navigate("/403");
          return;
        }
        setAuthStatus("ready");
        if (isKnownConsoleRoute(path)) await loadRoute(generation, activeSession, path);
      } catch (error) {
        if (generation === requestGeneration.current) {
          setAuthStatus("error");
          setAuthError(friendlyError(error));
        }
      }
    };
    void run();
    return () => {
      if (path === "/login") invalidateLoginAttempt();
    };
  }, [path]);

  useEffect(() => () => {
    invalidateLoginAttempt();
    logoutAttemptGeneration.current += 1;
    logoutInFlight.current = false;
    requestGeneration.current += 1;
    sessionGeneration.current += 1;
    if (toastTimer.current) window.clearTimeout(toastTimer.current);
  }, []);

  const submitLogin = async (email: string, password: string) => {
    invalidateLoginAttempt();
    const attempt = loginAttemptGeneration.current;
    const abortController = new AbortController();
    loginAbortController.current = abortController;
    setAuthError("");
    setAuthStatus("checking");
    try {
      const next = await loginRequest({ email, password }, abortController.signal);
      if (attempt !== loginAttemptGeneration.current || abortController.signal.aborted || logoutState.current !== "idle" || path !== "/login") return;
      replaceSession(next);
      setAuthStatus("ready");
      const requested = new URLSearchParams(window.location.search).get("redirect");
      const allowed = requested?.startsWith("/console") || (next.isOperator && requested?.startsWith("/admin"));
      navigate(allowed && requested ? requested : defaultAuthenticatedRoute(next.isOperator));
    } catch (error) {
      if (attempt !== loginAttemptGeneration.current || abortController.signal.aborted) return;
      setAuthStatus("public");
      setAuthError(friendlyError(error));
    } finally {
      if (attempt === loginAttemptGeneration.current) loginAbortController.current = null;
    }
  };

  const signOut = async () => {
    const activeSession = sessionRef.current;
    if (!activeSession || logoutInFlight.current) return;
    invalidateLoginAttempt();
    logoutInFlight.current = true;
    const attempt = ++logoutAttemptGeneration.current;
    logoutState.current = "pending";
    requestGeneration.current += 1;
    sessionGeneration.current += 1;
    resetConsoleState();
    setAuthError("");
    setAuthStatus("logout_pending");

    const result = await logoutAndConfirm(activeSession.csrfToken);
    if (attempt !== logoutAttemptGeneration.current) return;
    logoutInFlight.current = false;
    if (result.state === "confirmed") {
      replaceSession(null);
      setAuthStatus("public");
      navigate("/", true);
      return;
    }

    logoutState.current = "unconfirmed";
    if (result.session) {
      sessionRef.current = result.session;
      setSession(result.session);
    }
    setAuthError(result.reason === "session_still_active"
      ? "服务器仍报告 Session 有效。受保护内容已隐藏，请重试退出。"
      : "无法确认服务器 Session 是否已撤销。受保护内容已隐藏，请重试退出。");
    setAuthStatus("logout_unconfirmed");
  };

  const refreshCurrentPage = async () => {
    if (!session) return;
    const generation = ++requestGeneration.current;
    workspaceSecretCapability.clear();
    await loadRoute(generation, session, path);
  };

  const changeWorkspacePage = async (page: number) => {
    if (!session || page < 1) return;
    const generation = requestGeneration.current;
    await loadWorkspaces(generation, session, page, 10);
  };

  const copyText = async (value: string | undefined, message: string) => {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      flash(message);
    } catch {
      flash("复制失败，请重试", "danger");
    }
  };

  const changeBalancePage = async (page: number) => {
    if (!session || page < 1) return;
    await loadBalanceHistory(requestGeneration.current, session, page);
  };

  const openOperatorWorkspace = async (workspaceId: string) => {
    if (!session) return;
    selectedOperatorWorkspaceIdRef.current = workspaceId;
    setSelectedOperatorWorkspaceId(workspaceId);
    beginSource("operatorWorkspaceDetail");
    beginSource("operatorWorkspaceImagePreview");
    try {
      const [result, preview] = await Promise.all([
        getOperatorWorkspace(workspaceId),
        getOperatorWorkspaceRuntimeImageReplacementPreview(workspaceId)
      ]);
      if (selectedOperatorWorkspaceIdRef.current !== workspaceId) return;
      updateSource("operatorWorkspaceDetail", { value: result, loading: false, error: "" });
      updateSource("operatorWorkspaceImagePreview", { value: preview, loading: false, error: "" });
    } catch (error) {
      if (selectedOperatorWorkspaceIdRef.current === workspaceId) {
        failSource("operatorWorkspaceDetail", error, unavailableSource("control-plane+fabric+ledger"));
        failSource("operatorWorkspaceImagePreview", error, unavailableSource("control-plane+fabric"));
      }
    }
  };

  const changeOperatorWorkspacePage = async (page: number) => {
    if (!session || page < 1) return;
    await loadOperatorWorkspaces(requestGeneration.current, session, page);
  };

  const workspaceRows = sources.workspaces.value?.available ? sources.workspaces.value.data.items : [];
  const workspacePages = sources.workspaces.value?.available ? Math.ceil(sources.workspaces.value.data.total / sources.workspaces.value.data.pageSize) : 0;
  const operatorWorkspacePages = sources.operatorWorkspaces.value?.available ? Math.ceil(sources.operatorWorkspaces.value.data.total / sources.operatorWorkspaces.value.data.pageSize) : 0;
  const isAdminRoute = path === "/admin" || path.startsWith("/admin/");
  const isKnownRoute = isKnownConsoleRoute(path);

  const pageTitle = useMemo(() => {
    if (path === "/console" || path === "/console/overview") return "概览";
    if (path.startsWith("/console/workspaces")) return "Workspace";
    if (path.startsWith("/console/api")) return "API 服务";
    if (path === "/console/billing") return "账单";
    if (path === "/console/announcements") return "公告";
    if (path === "/admin" || path === "/admin/overview") return "运维概览";
    if (path === "/admin/accounts") return "客户与计费账户";
    if (path === "/admin/billing") return "计费复核";
    if (path === "/admin/resources") return "资源状态";
    if (path === "/admin/system") return "系统状态";
    if (path === "/admin/announcements") return "公告管理";
    return "页面不存在";
  }, [path]);

  return {
    path,
    navigate,
    session,
    authStatus,
    authError,
    sources,
    toast,
    pageTitle,
    isAdminRoute,
    isKnownRoute,
    isSensitiveRoute: isSensitiveConsoleRoute(path),
    sidebarOpen,
    setSidebarOpen,
    globalSlide,
    setGlobalSlide: (slide: GlobalSlide) => {
      setGlobalSlide(slide);
      if (slide === "support") void support.load();
    },
    submitLogin,
    signOut,
    refreshCurrentPage,
    workspaceRows,
    workspacePageNumber,
    workspacePages,
    changeWorkspacePage,
    workspaceLaunch,
    workspaceDeleteBusy: workspaceDelete.busy,
    workspaceDeleteIssue: workspaceDelete.issue,
    deleteCurrentWorkspace: workspaceDelete.deleteCurrentWorkspace,
    workspaceRenewalBusy: workspaceRenewal.busy,
    workspaceRenewalIssue: workspaceRenewal.issue,
    updateCurrentWorkspaceRenewal: workspaceRenewal.updateCurrentWorkspaceRenewal,
    support,
    workspaceSecrets,
    workspaceBudgetBusy: workspaceBudget.busy,
    updateWorkspaceBudget: workspaceBudget.update,
    copyText,
    billing,
    customerAnnouncements,
    gatewayUsage,
    balanceHistoryPage,
    changeBalancePage,
    operatorAccounts,
    operatorWorkspacePage,
    operatorWorkspacePages,
    changeOperatorWorkspacePage,
    operatorAnnouncements,
    selectedOperatorWorkspaceId,
    setSelectedOperatorWorkspaceId,
    openOperatorWorkspace,
    workspaceRuntimeImageReplacement,
    walletAdjustmentOperation: walletAdjustment.operation,
    setWalletAdjustmentOperation: walletAdjustment.setOperation,
    submitWalletAdjustment: walletAdjustment.submit,
    refreshWalletOperation: walletAdjustment.refresh,
    recoverWalletOperation: walletAdjustment.recover,
    walletAdjustmentBusy: walletAdjustment.busy
  };
}

export type ConsoleController = ReturnType<typeof useConsoleController>;
