import type {
  AnnouncementDraftRequest,
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
  GatewayUsagePeriod,
  GatewayUsageSummaryDTO,
  GatewayWallet,
  OperatorAccountPageDTO,
  OperatorAccountDTO,
  OperatorProvisionAccountCommandDTO,
  ProvisionAccountRequest,
  OperatorAnnouncementPageDTO,
  OperatorHealthDTO,
  OperatorOverviewDTO,
  OperatorReconciliationPageDTO,
  OperatorWorkspaceDTO,
  OperatorWorkspacePageDTO,
  OperatorWorkspaceRuntimeImagePolicyDTO,
  OperatorWorkspaceRuntimeImagePreviewDTO,
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
  WorkspaceGatewayBudgetUpdateRequest,
  WorkspaceLaunchResponse,
  WorkspacePricePreview,
  WorkspaceListData,
  WorkspaceRuntimeDTO,
  WorkspaceRuntimeImageReplacementDTO,
  WalletAdjustmentRequest
} from "../api/dtos.ts";

export interface RemoteState<T> {
  value: T | null;
  loading: boolean;
  error: string;
}

export interface WorkspaceSourceProjectionLease {
  isCurrent: () => boolean;
  commit: () => boolean;
}

export interface ConsoleSources {
  workspaces: RemoteState<SourceEnvelope<WorkspaceListData>>;
  workspaceDetail: RemoteState<SourceEnvelope<WorkspaceDTO | null>>;
  runtime: RemoteState<SourceEnvelope<WorkspaceRuntimeDTO>>;
  workspaceBudget: RemoteState<SourceEnvelope<WorkspaceGatewayBudgetDTO>>;
  wallet: RemoteState<SourceEnvelope<GatewayWallet>>;
  accountUsage: RemoteState<SourceEnvelope<GatewayAccountUsageSummaryDTO>>;
  balanceHistory: RemoteState<SourceEnvelope<GatewayBalanceHistoryPageDTO>>;
  endpoint: RemoteState<SourceEnvelope<GatewayEndpointDTO>>;
  operatorOverview: RemoteState<SourceEnvelope<OperatorOverviewDTO>>;
  operatorWorkspaces: RemoteState<SourceEnvelope<OperatorWorkspacePageDTO>>;
  operatorWorkspaceDetail: RemoteState<SourceEnvelope<OperatorWorkspaceDTO>>;
  operatorWorkspaceImagePolicy: RemoteState<SourceEnvelope<OperatorWorkspaceRuntimeImagePolicyDTO>>;
  operatorWorkspaceImagePreview: RemoteState<SourceEnvelope<OperatorWorkspaceRuntimeImagePreviewDTO>>;
  operatorReconciliation: RemoteState<SourceEnvelope<OperatorReconciliationPageDTO>>;
  operatorHealth: RemoteState<SourceEnvelope<OperatorHealthDTO>>;
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

export type WorkspaceRuntimeImageReplacementIssue = "" | "unavailable" | "unconfirmed" | "timeout";

export interface WorkspaceRuntimeImageReplacementController {
  operation: WorkspaceRuntimeImageReplacementDTO | null;
  busy: boolean;
  issue: WorkspaceRuntimeImageReplacementIssue;
  replaceWorkspaceRuntimeImage: () => Promise<boolean>;
  refreshWorkspaceRuntimeImageReplacement: () => Promise<void>;
}

export interface WorkspaceRenewalController {
  busy: boolean;
  issue: "" | "unconfirmed";
  updateCurrentWorkspaceRenewal: (autoRenew: boolean) => Promise<boolean>;
}

export interface WorkspaceBudgetController {
  busy: boolean;
  update: (input: WorkspaceGatewayBudgetUpdateRequest) => Promise<boolean>;
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

export interface GatewayUsageController {
  keys: RemoteState<SourceEnvelope<GatewayKeyPageDTO>>;
  usage: RemoteState<SourceEnvelope<GatewayKeyUsagePageDTO>>;
  summary: RemoteState<SourceEnvelope<GatewayUsageSummaryDTO>>;
  selectedKeyId: string;
  period: GatewayUsagePeriod;
  page: number;
  refresh: () => Promise<void>;
  selectKey: (keyId: string) => Promise<void>;
  selectPeriod: (period: GatewayUsagePeriod) => Promise<void>;
  changePage: (page: number) => Promise<void>;
}

export interface BillingController {
  view: BillingView;
  setView: (view: BillingView) => void;
  receipts: RemoteState<SourceEnvelope<BillingReceiptPage>>;
  detail: RemoteState<SourceEnvelope<BillingReceipt>>;
  selectedReceiptId: string;
  pageNumber: number;
  canNext: boolean;
  canPrevious: boolean;
  refresh: () => Promise<void>;
  openReceipt: (receiptId: string) => Promise<void>;
  closeReceipt: () => void;
  nextPage: () => Promise<void>;
  previousPage: () => Promise<void>;
}

export interface OperatorAccountController {
  accounts: RemoteState<SourceEnvelope<OperatorAccountPageDTO>>;
  page: number;
  pages: number;
  provisionOperation: OperatorProvisionAccountCommandDTO | null;
  provisionBusy: boolean;
  busyAccountIds: string[];
  refresh: () => Promise<void>;
  changePage: (page: number) => Promise<void>;
  setProvisionOperation: (operation: OperatorProvisionAccountCommandDTO | null) => void;
  provision: (input: ProvisionAccountRequest) => Promise<{
    operation: OperatorProvisionAccountCommandDTO;
    account: OperatorAccountDTO | null;
  } | null>;
  disable: (accountId: string) => Promise<void>;
  setWorkspacePurchaseEligibility: (accountId: string, enabled: boolean) => Promise<void>;
}

export interface OperatorAnnouncementController {
  announcements: RemoteState<SourceEnvelope<OperatorAnnouncementPageDTO>>;
  createBusy: boolean;
  busyAnnouncementIds: string[];
  refresh: () => Promise<void>;
  create: (input: AnnouncementDraftRequest) => Promise<boolean>;
  publish: (announcementId: string) => Promise<void>;
  withdraw: (announcementId: string) => Promise<void>;
}

export interface CustomerAnnouncementController {
  announcements: RemoteState<SourceEnvelope<AnnouncementPageDTO>>;
  busyAnnouncementId: string;
  refresh: () => Promise<void>;
  markRead: (announcementId: string) => Promise<void>;
}
