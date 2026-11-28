import { useCallback, useEffect, useRef, useState } from "react";

import {
  disableOperatorAccount as disableOperatorAccountCommand,
  getOperatorAccountsPage,
  provisionOperatorAccount,
  seedActiveGatewayLinkRelayEntitlements as seedActiveGatewayLinkRelayEntitlementsCommand,
  setOperatorLinkRelayEntitlement as setOperatorLinkRelayEntitlementCommand,
  setOperatorPlatformAdmin as setOperatorPlatformAdminCommand,
  setOperatorWorkspacePurchaseEligibility as setOperatorWorkspacePurchaseEligibilityCommand
} from "../api/console-read-api.ts";
import type {
  AuthSession,
  OperatorAccountDTO,
  OperatorAccountPageDTO,
  OperatorProvisionAccountCommandDTO,
  ProvisionAccountRequest,
  SourceEnvelope
} from "../api/dtos.ts";
import { authorizationFailureState } from "./authorization-error.ts";
import { requestConfirm } from "./confirm-dialog-store.ts";
import { cloudIdentity } from "./console-identity.ts";
import type { OperatorAccountController, RemoteState } from "./console-controller-types.ts";
import {
  accountCommandSucceeded,
  accountIdentity,
  eligibilityCommandSucceeded,
  isOperatorPlatformAdmin,
  linkRelayCommandSucceeded,
  platformAdminCommandSucceeded,
  provisionCommandSucceeded,
  provisionReadbackMatches,
  resolveProvisionAccountIntent,
  sameAccountIdentity,
  seedLinkRelayCommandSucceeded,
  type OperatorAccountIdentity,
  type ProvisionAccountIntent
} from "./operator-account-controller-model.ts";

interface OperatorAccountDependencies {
  active: boolean;
  currentSession: () => AuthSession | null;
  flash: (text: string, tone?: "good" | "danger") => void;
  friendlyError: (error: unknown) => string;
  mutationError: (error: unknown) => string;
  unavailableSource: <T>(source: string) => SourceEnvelope<T>;
}

interface DisableIntent {
  accountId: string;
  identity: OperatorAccountIdentity;
  reason: string;
  idempotencyKey: string;
}

interface ToggleIntent extends DisableIntent {
  enabled: boolean;
}

export interface OperatorAccountCapability extends OperatorAccountController {
  load: () => Promise<void>;
  reset: () => void;
}

const pageSize = 20;
const emptyRemote = <T,>(): RemoteState<T> => ({ value: null, loading: false, error: "" });

export function useOperatorAccountController({
  active,
  currentSession,
  flash,
  friendlyError,
  mutationError,
  unavailableSource
}: OperatorAccountDependencies): OperatorAccountCapability {
  const [accounts, setAccounts] = useState<RemoteState<SourceEnvelope<OperatorAccountPageDTO>>>(emptyRemote);
  const [page, setPage] = useState(1);
  const [searchQuery, setSearchQuery] = useState("");
  const [provisionOperation, setProvisionOperation] = useState<OperatorProvisionAccountCommandDTO | null>(null);
  const [provisionBusy, setProvisionBusy] = useState(false);
  const [relaySeedBusy, setRelaySeedBusy] = useState(false);
  const [busyAccountIds, setBusyAccountIds] = useState<string[]>([]);

  const activeRef = useRef(active);
  const pageRef = useRef(1);
  const searchRef = useRef("");
  const scopeGeneration = useRef(0);
  const listGeneration = useRef(0);
  const provisionGeneration = useRef(0);
  const provisionClaim = useRef<symbol | null>(null);
  const relaySeedClaim = useRef<symbol | null>(null);
  const accountClaims = useRef(new Map<string, symbol>());
  const provisionIntent = useRef<ProvisionAccountIntent | null>(null);
  const relaySeedIntent = useRef<{ reason: string; idempotencyKey: string } | null>(null);
  const disableIntents = useRef(new Map<string, DisableIntent>());
  const eligibilityIntents = useRef(new Map<string, ToggleIntent>());
  const platformAdminIntents = useRef(new Map<string, ToggleIntent>());
  const linkRelayIntents = useRef(new Map<string, ToggleIntent>());
  activeRef.current = active;

  const requestOwnsScope = useCallback((generation: number, userId: string, csrfToken: string) => {
    const session = currentSession();
    return generation === scopeGeneration.current
      && activeRef.current
      && session?.user.id === userId
      && session.csrfToken === csrfToken;
  }, [currentSession]);

  const reset = useCallback(() => {
    scopeGeneration.current += 1;
    listGeneration.current += 1;
    provisionGeneration.current += 1;
    pageRef.current = 1;
    searchRef.current = "";
    provisionClaim.current = null;
    relaySeedClaim.current = null;
    provisionIntent.current = null;
    relaySeedIntent.current = null;
    disableIntents.current.clear();
    eligibilityIntents.current.clear();
    platformAdminIntents.current.clear();
    linkRelayIntents.current.clear();
    setAccounts(emptyRemote());
    setPage(1);
    setSearchQuery("");
    setProvisionOperation(null);
    setRelaySeedBusy(false);
  }, []);

  useEffect(() => {
    scopeGeneration.current += 1;
    listGeneration.current += 1;
    provisionGeneration.current += 1;
  }, [active]);

  useEffect(() => reset, [reset]);

  const commitPage = useCallback((result: SourceEnvelope<OperatorAccountPageDTO>) => {
    setAccounts({ value: result, loading: false, error: "" });
    if (result.available) {
      pageRef.current = result.data.page;
      setPage(result.data.page);
    }
  }, []);

  const queryForApi = useCallback(() => (
    cloudIdentity ? searchRef.current.trim() || undefined : undefined
  ), []);

  const loadPage = useCallback(async (session: AuthSession, requestedPage: number) => {
    if (!activeRef.current || requestedPage < 1) return;
    const generation = ++listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setAccounts((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await getOperatorAccountsPage(requestedPage, pageSize, undefined, cloudIdentity, queryForApi());
      if (generation !== listGeneration.current || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (result.available && (result.data.page !== requestedPage || result.data.pageSize !== pageSize)) {
        throw new Error("operator_account_page_mismatch");
      }
      commitPage(result);
    } catch (error) {
      if (generation !== listGeneration.current || !requestOwnsScope(scope, userId, csrfToken)) return;
      const authorization = authorizationFailureState(error);
      setAccounts(authorization ?? {
        value: unavailableSource(cloudIdentity ? "tenant+sub2api" : "control-plane+sub2api"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [commitPage, friendlyError, queryForApi, requestOwnsScope, unavailableSource]);

  const load = useCallback(async () => {
    const session = currentSession();
    if (!session || !activeRef.current) return;
    await loadPage(session, pageRef.current);
  }, [currentSession, loadPage]);

  const changePage = useCallback(async (nextPage: number) => {
    const session = currentSession();
    if (!session || !activeRef.current || nextPage < 1) return;
    pageRef.current = nextPage;
    await loadPage(session, nextPage);
  }, [currentSession, loadPage]);

  const searchAccounts = useCallback(async (query: string) => {
    const normalized = query.trim();
    searchRef.current = normalized;
    setSearchQuery(query);
    if (!cloudIdentity) return;
    const session = currentSession();
    if (!session || !activeRef.current) return;
    pageRef.current = 1;
    await loadPage(session, 1);
  }, [currentSession, loadPage]);

  const findAccountProjection = useCallback(async (
    session: AuthSession,
    scope: number,
    matcher: (account: OperatorAccountDTO) => boolean,
    preferredPage?: number
  ) => {
    const firstPage = preferredPage ?? 1;
    const activeSearch = queryForApi();
    const readPage = async (requestedPage: number) => {
      const result = await getOperatorAccountsPage(requestedPage, pageSize, undefined, cloudIdentity, activeSearch);
      if (!requestOwnsScope(scope, session.user.id, session.csrfToken)) return null;
      if (!result.available) return null;
      if (result.data.page !== requestedPage || result.data.pageSize !== pageSize) {
        throw new Error("operator_account_page_mismatch");
      }
      const account = result.data.items.find(matcher);
      return { account: account ?? null, result };
    };

    const first = await readPage(firstPage);
    if (!first) return null;
    if (first.account) return { ...first, visibleResult: first.result };
    const pages = Math.max(1, Math.ceil(first.result.data.total / first.result.data.pageSize));
    for (let requestedPage = 1; requestedPage <= pages; requestedPage += 1) {
      if (requestedPage === firstPage) continue;
      const candidate = await readPage(requestedPage);
      if (!candidate) return null;
      if (candidate.account) {
        return {
          ...candidate,
          visibleResult: preferredPage === undefined ? candidate.result : first.result
        };
      }
    }
    return null;
  }, [queryForApi, requestOwnsScope]);

  const setProvisionOperationProjection = useCallback((operation: OperatorProvisionAccountCommandDTO | null) => {
    setProvisionOperation(operation);
  }, []);

  const provision = useCallback(async (input: ProvisionAccountRequest) => {
    const session = currentSession();
    if (!session || !activeRef.current || provisionClaim.current !== null) return null;
    const nextIntent = resolveProvisionAccountIntent(
      provisionIntent.current,
      input,
      () => `account-provision:${crypto.randomUUID()}`
    );
    provisionIntent.current = nextIntent;
    const claim = Symbol("operator-account-provision");
    const generation = ++provisionGeneration.current;
    const projectionGeneration = listGeneration.current;
    const projectionPage = pageRef.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    provisionClaim.current = claim;
    setProvisionBusy(true);
    try {
      const result = await provisionOperatorAccount(nextIntent.input, csrfToken, nextIntent.idempotencyKey);
      if (generation !== provisionGeneration.current || provisionClaim.current !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return null;
      if (!provisionCommandSucceeded(result, nextIntent.input)) {
        throw new Error("operator_account_provision_identity_mismatch");
      }
      setProvisionOperation(result);
      const readback = await findAccountProjection(
        session,
        scope,
        (candidate) => provisionReadbackMatches(candidate, result, nextIntent.input)
      );
      if (generation !== provisionGeneration.current || provisionClaim.current !== claim
        || !requestOwnsScope(scope, userId, csrfToken)) return null;
      if (!readback?.account) {
        flash("开户命令已返回，但账户映射读回暂不可用，请重试", "danger");
        return { operation: result, account: null };
      }
      if (projectionGeneration === listGeneration.current && pageRef.current === projectionPage) {
        commitPage(readback.visibleResult);
      }
      if (provisionIntent.current === nextIntent) provisionIntent.current = null;
      flash("用户已开通");
      return { operation: result, account: readback.account };
    } catch (error) {
      if (generation === provisionGeneration.current && provisionClaim.current === claim
        && requestOwnsScope(scope, userId, csrfToken)) flash(mutationError(error), "danger");
      return null;
    } finally {
      if (provisionClaim.current === claim) {
        provisionClaim.current = null;
        setProvisionBusy(false);
      }
    }
  }, [commitPage, currentSession, findAccountProjection, flash, mutationError, requestOwnsScope]);

  const claimAccount = useCallback((accountId: string) => {
    if (accountClaims.current.has(accountId)) return null;
    const claim = Symbol(`operator-account:${accountId}`);
    accountClaims.current.set(accountId, claim);
    setBusyAccountIds((current) => current.includes(accountId) ? current : [...current, accountId]);
    return claim;
  }, []);

  const releaseAccount = useCallback((accountId: string, claim: symbol) => {
    if (accountClaims.current.get(accountId) !== claim) return;
    accountClaims.current.delete(accountId);
    setBusyAccountIds((current) => current.filter((id) => id !== accountId));
  }, []);

  const currentAccount = useCallback((accountId: string) => accounts.value?.available
    ? accounts.value.data.items.find((account) => account.accountId === accountId) ?? null
    : null, [accounts.value]);

  const disable = useCallback(async (accountId: string) => {
    const session = currentSession();
    const account = currentAccount(accountId);
    if (!session || !activeRef.current || !account) return;
    const confirmed = await requestConfirm({
      title: "停用客户账号",
      description: "确认停用该客户？账号会立即停用；历史账单、收据和审计记录会保留。",
      confirmLabel: "确认停用",
      tone: "danger"
    });
    if (!confirmed) return;
    const claim = claimAccount(accountId);
    if (!claim) return;
    const identity = accountIdentity(account);
    const reason = "operator_requested";
    const current = disableIntents.current.get(accountId);
    const intent = current && current.reason === reason && sameAccountIdentity(current.identity, account)
      ? current
      : { accountId, identity, reason, idempotencyKey: `account-disable:${accountId}:${crypto.randomUUID()}` };
    disableIntents.current.set(accountId, intent);
    const projectionPage = pageRef.current;
    const projectionGeneration = listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    try {
      const result = await disableOperatorAccountCommand(accountId, reason, csrfToken, intent.idempotencyKey);
      if (accountClaims.current.get(accountId) !== claim || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!accountCommandSucceeded(result, accountId)) throw new Error("operator_account_disable_identity_mismatch");
      const readback = await findAccountProjection(
        session,
        scope,
        (candidate) => sameAccountIdentity(intent.identity, candidate) && candidate.status === "disabled",
        projectionPage
      );
      if (accountClaims.current.get(accountId) !== claim || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!readback?.account) throw new Error("operator_account_disable_readback_mismatch");
      if (projectionGeneration === listGeneration.current && pageRef.current === projectionPage) {
        commitPage(readback.visibleResult);
      }
      if (disableIntents.current.get(accountId) === intent) disableIntents.current.delete(accountId);
      flash("客户已停用");
    } catch (error) {
      if (accountClaims.current.get(accountId) === claim && requestOwnsScope(scope, userId, csrfToken)) {
        flash(mutationError(error), "danger");
      }
    } finally {
      releaseAccount(accountId, claim);
    }
  }, [claimAccount, commitPage, currentAccount, currentSession, findAccountProjection, flash, mutationError, releaseAccount, requestOwnsScope]);

  const executeToggle = useCallback(async ({
    accountId,
    enabled,
    intents,
    intentPrefix,
    confirm,
    runCommand,
    commandMatches,
    readbackMatches,
    readbackError,
    successMessage
  }: {
    accountId: string;
    enabled: boolean;
    intents: Map<string, ToggleIntent>;
    intentPrefix: string;
    confirm: { title: string; description: string; confirmLabel: string; tone: "primary" | "danger" };
    runCommand: (targetId: string, reason: string, csrfToken: string, idempotencyKey: string) => Promise<void>;
    commandMatches?: never;
    readbackMatches: (candidate: OperatorAccountDTO) => boolean;
    readbackError: string;
    successMessage: string;
  }) => {
    void commandMatches;
    const session = currentSession();
    const account = currentAccount(accountId);
    if (!session || !activeRef.current || !account) return;
    const confirmed = await requestConfirm(confirm);
    if (!confirmed) return;
    const claim = claimAccount(accountId);
    if (!claim) return;
    const identity = accountIdentity(account);
    const reason = "operator_requested";
    const current = intents.get(accountId);
    const intent = current && current.enabled === enabled && current.reason === reason
      && sameAccountIdentity(current.identity, account)
      ? current
      : {
          accountId,
          identity,
          enabled,
          reason,
          idempotencyKey: `${intentPrefix}:${accountId}:${crypto.randomUUID()}`
        };
    intents.set(accountId, intent);
    const projectionPage = pageRef.current;
    const projectionGeneration = listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const targetId = cloudIdentity ? String(account.sub2apiUserId).trim() : accountId;
    if (!targetId) {
      releaseAccount(accountId, claim);
      return;
    }
    try {
      await runCommand(targetId, reason, csrfToken, intent.idempotencyKey);
      if (accountClaims.current.get(accountId) !== claim || !requestOwnsScope(scope, userId, csrfToken)) return;
      const readback = await findAccountProjection(
        session,
        scope,
        (candidate) => sameAccountIdentity(intent.identity, candidate) && readbackMatches(candidate),
        projectionPage
      );
      if (accountClaims.current.get(accountId) !== claim || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!readback?.account) throw new Error(readbackError);
      if (projectionGeneration === listGeneration.current && pageRef.current === projectionPage) {
        commitPage(readback.visibleResult);
      }
      if (intents.get(accountId) === intent) intents.delete(accountId);
      flash(successMessage);
    } catch (error) {
      if (accountClaims.current.get(accountId) === claim && requestOwnsScope(scope, userId, csrfToken)) {
        flash(mutationError(error), "danger");
      }
    } finally {
      releaseAccount(accountId, claim);
    }
  }, [claimAccount, commitPage, currentAccount, currentSession, findAccountProjection, flash, mutationError, releaseAccount, requestOwnsScope]);

  const setWorkspacePurchaseEligibility = useCallback(async (accountId: string, enabled: boolean) => {
    await executeToggle({
      accountId,
      enabled,
      intents: eligibilityIntents.current,
      intentPrefix: "workspace-purchase-eligibility",
      confirm: {
        title: enabled ? "授予工作空间购买资格" : "撤销工作空间购买资格",
        description: enabled
          ? "确认授予该账户新购工作空间的资格？"
          : "确认撤销该账户新购工作空间的资格？已有工作空间不会受影响。",
        confirmLabel: enabled ? "确认授予" : "确认撤销",
        tone: enabled ? "primary" : "danger"
      },
      runCommand: async (targetId, reason, csrfToken, idempotencyKey) => {
        const result = await setOperatorWorkspacePurchaseEligibilityCommand(targetId, enabled, reason, csrfToken, idempotencyKey);
        if (!eligibilityCommandSucceeded(result, targetId, enabled)) {
          throw new Error("operator_account_eligibility_identity_mismatch");
        }
      },
      readbackMatches: (candidate) => candidate.workspacePurchaseEnabled === enabled,
      readbackError: "operator_account_eligibility_readback_mismatch",
      successMessage: enabled ? "已授予 Workspace 新购资格" : "已撤销 Workspace 新购资格"
    });
  }, [executeToggle]);

  const setPlatformAdmin = useCallback(async (accountId: string, enabled: boolean) => {
    await executeToggle({
      accountId,
      enabled,
      intents: platformAdminIntents.current,
      intentPrefix: "platform-admin",
      confirm: {
        title: enabled ? "授予平台管理员角色" : "撤销平台管理员角色",
        description: enabled
          ? "确认授予该 Gateway 账户平台管理员权限？该账户将获得用户管理、权限委派与运营控制台访问权限。"
          : "确认撤销该账户的平台管理员权限？撤销后将保留普通 Gateway 身份与工作空间使用权限。",
        confirmLabel: enabled ? "确认授予" : "确认撤销",
        tone: enabled ? "primary" : "danger"
      },
      runCommand: async (targetId, reason, csrfToken, idempotencyKey) => {
        const result = await setOperatorPlatformAdminCommand(targetId, enabled, reason, csrfToken, idempotencyKey);
        if (!platformAdminCommandSucceeded(result, targetId, enabled)) {
          throw new Error("operator_account_platform_admin_identity_mismatch");
        }
      },
      readbackMatches: (candidate) => isOperatorPlatformAdmin(candidate) === enabled,
      readbackError: "operator_account_platform_admin_readback_mismatch",
      successMessage: enabled ? "已授予平台管理员权限" : "已撤销平台管理员权限"
    });
  }, [executeToggle]);

  const setLinkRelayEntitlement = useCallback(async (accountId: string, enabled: boolean) => {
    await executeToggle({
      accountId,
      enabled,
      intents: linkRelayIntents.current,
      intentPrefix: "link-relay-entitlement",
      confirm: {
        title: enabled ? "授予 OPL Link Relay 转发准入" : "撤销 OPL Link Relay 转发准入",
        description: enabled
          ? "确认将该 Gateway 用户纳入 OPL Link Relay 允许名单？"
          : "确认撤销该用户的 OPL Link Relay 转发准入？撤销后将无法新注册设备或绑定主机。",
        confirmLabel: enabled ? "确认授予" : "确认撤销",
        tone: enabled ? "primary" : "danger"
      },
      runCommand: async (targetId, reason, csrfToken, idempotencyKey) => {
        const result = await setOperatorLinkRelayEntitlementCommand(targetId, enabled, reason, csrfToken, idempotencyKey);
        if (!linkRelayCommandSucceeded(result, targetId, enabled)) {
          throw new Error("operator_account_link_relay_identity_mismatch");
        }
      },
      readbackMatches: (candidate) => Boolean(candidate.linkRelayEnabled) === enabled,
      readbackError: "operator_account_link_relay_readback_mismatch",
      successMessage: enabled ? "已授予 OPL Link Relay 转发准入" : "已撤销 OPL Link Relay 转发准入"
    });
  }, [executeToggle]);

  const seedActiveLinkRelayEntitlements = useCallback(async () => {
    const session = currentSession();
    if (!session || !activeRef.current || !cloudIdentity || relaySeedClaim.current) return null;
    const confirmed = await requestConfirm({
      title: "补录存量 Gateway 用户 Link Relay 白名单",
      description: "将当前 OPL Gateway 中所有正常状态的活跃用户批量纳入 Link Relay 白名单；已被显式撤销的用户记录不会被覆盖。确认执行？",
      confirmLabel: "确认补录",
      tone: "primary"
    });
    if (!confirmed) return null;
    const reason = "initial_active_gateway_seed";
    const intent = relaySeedIntent.current?.reason === reason
      ? relaySeedIntent.current
      : { reason, idempotencyKey: `link-relay-seed:${crypto.randomUUID()}` };
    relaySeedIntent.current = intent;
    const claim = Symbol("link-relay-seed");
    relaySeedClaim.current = claim;
    setRelaySeedBusy(true);
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    const projectionPage = pageRef.current;
    try {
      const result = await seedActiveGatewayLinkRelayEntitlementsCommand(intent.reason, csrfToken, intent.idempotencyKey);
      if (!seedLinkRelayCommandSucceeded(result)) {
        throw new Error("operator_account_link_relay_seed_invalid");
      }
      if (relaySeedClaim.current !== claim || !requestOwnsScope(scope, userId, csrfToken)) return null;
      if (relaySeedIntent.current === intent) relaySeedIntent.current = null;
      await loadPage(session, projectionPage);
      if (relaySeedClaim.current !== claim || !requestOwnsScope(scope, userId, csrfToken)) return null;
      flash(`已扫描 ${result.scannedCount} 位活跃用户：新增 ${result.seededCount} 位，已有 ${result.existingCount} 位`);
      return result;
    } catch (error) {
      if (relaySeedClaim.current === claim && requestOwnsScope(scope, userId, csrfToken)) {
        flash(mutationError(error), "danger");
      }
      return null;
    } finally {
      if (relaySeedClaim.current === claim) {
        relaySeedClaim.current = null;
        setRelaySeedBusy(false);
      }
    }
  }, [currentSession, flash, loadPage, mutationError, requestOwnsScope]);

  const pages = accounts.value?.available
    ? Math.ceil(accounts.value.data.total / accounts.value.data.pageSize)
    : 0;

  return {
    accounts,
    page,
    pages,
    searchQuery,
    provisionOperation,
    provisionBusy,
    relaySeedBusy,
    busyAccountIds,
    refresh: load,
    changePage,
    searchAccounts,
    setProvisionOperation: setProvisionOperationProjection,
    provision,
    disable,
    setWorkspacePurchaseEligibility,
    setPlatformAdmin,
    setLinkRelayEntitlement,
    seedActiveLinkRelayEntitlements,
    load,
    reset
  };
}
