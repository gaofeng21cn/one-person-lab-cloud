import type {
  AnnouncementPageDTO,
  AuthSession,
  BillingReceipt,
  BillingReceiptPage,
  GatewayAccountUsageSummaryDTO,
  GatewayBalanceHistoryPageDTO,
  GatewayEndpointDTO,
  GatewayKeyPageDTO,
  GatewayKeySecretDTO,
  GatewayKeyUsagePageDTO,
  GatewayUsageSummaryDTO,
  GatewayWallet,
  OperatorAccountPageDTO,
  OperatorAnnouncementPageDTO,
  OperatorHealthDTO,
  OperatorOverviewDTO,
  OperatorReconciliationPageDTO,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  PlanId,
  PricingCatalogResponse,
  PricingPlan,
  SourceEnvelope,
  CreateSupportTicketMappingRequest,
  SupportTicketPageDTO,
  WalletAdjustmentOperationDTO,
  WorkspaceCredentialAccess,
  WorkspaceDTO,
  WorkspaceGatewayBudgetDTO,
  WorkspaceLaunchResponse,
  WorkspacePricePreview,
  WorkspaceListData,
  WorkspaceRuntimeDTO,
  WalletAdjustmentRequest
} from "../api/dtos.ts";

export interface RemoteState<T> {
  value: T | null;
  loading: boolean;
  error: string;
}

export interface ConsoleSources {
  workspaces: RemoteState<SourceEnvelope<WorkspaceListData>>;
  workspaceDetail: RemoteState<SourceEnvelope<WorkspaceDTO | null>>;
  runtime: RemoteState<SourceEnvelope<WorkspaceRuntimeDTO>>;
  workspaceBudget: RemoteState<SourceEnvelope<WorkspaceGatewayBudgetDTO>>;
  wallet: RemoteState<SourceEnvelope<GatewayWallet>>;
  accountUsage: RemoteState<SourceEnvelope<GatewayAccountUsageSummaryDTO>>;
  balanceHistory: RemoteState<SourceEnvelope<GatewayBalanceHistoryPageDTO>>;
  receipts: RemoteState<SourceEnvelope<BillingReceiptPage>>;
  receiptDetail: RemoteState<SourceEnvelope<BillingReceipt>>;
  announcements: RemoteState<SourceEnvelope<AnnouncementPageDTO>>;
  usageKeys: RemoteState<SourceEnvelope<GatewayKeyPageDTO>>;
  usage: RemoteState<SourceEnvelope<GatewayKeyUsagePageDTO>>;
  usageSummary: RemoteState<SourceEnvelope<GatewayUsageSummaryDTO>>;
  endpoint: RemoteState<SourceEnvelope<GatewayEndpointDTO>>;
  operatorOverview: RemoteState<SourceEnvelope<OperatorOverviewDTO>>;
  operatorAccounts: RemoteState<SourceEnvelope<OperatorAccountPageDTO>>;
  operatorWorkspaces: RemoteState<SourceEnvelope<OperatorWorkspacePageDTO>>;
  operatorWorkspaceDetail: RemoteState<SourceEnvelope<OperatorWorkspaceDTO>>;
  operatorReconciliation: RemoteState<SourceEnvelope<OperatorReconciliationPageDTO>>;
  operatorHealth: RemoteState<SourceEnvelope<OperatorHealthDTO>>;
  operatorAnnouncements: RemoteState<SourceEnvelope<OperatorAnnouncementPageDTO>>;
}

export type AuthStatus = "public" | "checking" | "ready" | "error" | "logout_pending" | "logout_unconfirmed";
export type BillingView = "terms" | "receipts";
export type WorkspaceLaunchStep = "configure" | "confirm";
export type GlobalSlide = "account" | "support" | "";

export interface WorkspaceSecretController {
  credential: WorkspaceCredentialAccess | null;
  gatewayKey: GatewayKeySecretDTO | null;
  workspaceBusy: boolean;
  gatewayKeyBusy: boolean;
  clear: () => void;
  revealWorkspacePassword: () => Promise<void>;
  revealWorkspaceKey: () => Promise<void>;
  rotateWorkspacePassword: () => Promise<void>;
  copyWorkspacePassword: () => Promise<void>;
  copyWorkspaceKey: () => Promise<void>;
}

export interface ConsoleTransientState {
  session: AuthSession | null;
  authStatus: AuthStatus;
  authError: string;
  toast: { text: string; tone: "good" | "danger" };
  globalSlide: GlobalSlide;
  walletAdjustmentOperation: WalletAdjustmentOperationDTO | null;
}

export interface WorkspaceLaunchController {
  catalog: RemoteState<PricingCatalogResponse>;
  previews: Partial<Record<PlanId, WorkspacePricePreview>>;
  launchName: string;
  setLaunchName: (value: string) => void;
  launchPlan: PlanId;
  setLaunchPlan: (value: PlanId) => void;
  launchAutoRenew: boolean;
  setLaunchAutoRenew: (value: boolean) => void;
  launchStep: WorkspaceLaunchStep;
  setLaunchStep: (value: WorkspaceLaunchStep) => void;
  launchConfirmed: boolean;
  setLaunchConfirmed: (value: boolean) => void;
  selectedPlan: PricingPlan | null;
  selectedPrice: number | null;
  walletUsdMicros: string | null;
  balanceSufficient: boolean | null;
  customerOwned: boolean;
  launchOperation: WorkspaceLaunchResponse | null;
  launchPollIssue: "" | "error" | "timeout" | "readback";
  busy: boolean;
  reviewWorkspaceLaunch: () => void;
  submitWorkspaceLaunch: () => Promise<void>;
  openLaunchedWorkspace: () => Promise<void>;
}

export interface WorkspaceDeleteController {
  busy: boolean;
  issue: "" | "unavailable" | "unconfirmed";
  deleteCurrentWorkspace: () => Promise<void>;
}

export interface SupportController {
  tickets: SupportTicketPageDTO | null;
  loading: boolean;
  error: string;
  busy: boolean;
  load: () => Promise<SupportTicketPageDTO | null>;
  createMapping: (input: Omit<CreateSupportTicketMappingRequest, "accountId">) => Promise<boolean>;
}

export interface WalletAdjustmentController {
  operation: WalletAdjustmentOperationDTO | null;
  busy: boolean;
  setOperation: (operation: WalletAdjustmentOperationDTO | null) => void;
  submit: (accountId: string, input: WalletAdjustmentRequest) => Promise<WalletAdjustmentOperationDTO | null>;
  refresh: () => Promise<void>;
  recover: () => Promise<void>;
  reset: () => void;
}
