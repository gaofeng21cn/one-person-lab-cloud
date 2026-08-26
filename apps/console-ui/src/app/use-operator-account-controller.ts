import { useCallback, useEffect, useRef, useState } from "react";

import {
  disableOperatorAccount as disableOperatorAccountCommand,
  getOperatorAccountsPage,
  provisionOperatorAccount,
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
import type { OperatorAccountController, RemoteState } from "./console-controller-types.ts";
import {
  accountCommandSucceeded,
  accountIdentity,
  eligibilityCommandSucceeded,
  provisionCommandSucceeded,
  provisionReadbackMatches,
  resolveProvisionAccountIntent,
  sameAccountIdentity,
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

interface EligibilityIntent extends DisableIntent {
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
  const [provisionOperation, setProvisionOperation] = useState<OperatorProvisionAccountCommandDTO | null>(null);
  const [provisionBusy, setProvisionBusy] = useState(false);
  const [busyAccountIds, setBusyAccountIds] = useState<string[]>([]);

  const activeRef = useRef(active);
  const pageRef = useRef(1);
  const scopeGeneration = useRef(0);
  const listGeneration = useRef(0);
  const provisionGeneration = useRef(0);
  const provisionClaim = useRef<symbol | null>(null);
  const accountClaims = useRef(new Map<string, symbol>());
  const provisionIntent = useRef<ProvisionAccountIntent | null>(null);
  const disableIntents = useRef(new Map<string, DisableIntent>());
  const eligibilityIntents = useRef(new Map<string, EligibilityIntent>());
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
    provisionIntent.current = null;
    disableIntents.current.clear();
    eligibilityIntents.current.clear();
    setAccounts(emptyRemote());
    setPage(1);
    setProvisionOperation(null);
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

  const loadPage = useCallback(async (session: AuthSession, requestedPage: number) => {
    if (!activeRef.current || requestedPage < 1) return;
    const generation = ++listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    setAccounts((current) => ({ ...current, loading: true, error: "" }));
    try {
      const result = await getOperatorAccountsPage(requestedPage, pageSize);
      if (generation !== listGeneration.current || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (result.available && (result.data.page !== requestedPage || result.data.pageSize !== pageSize)) {
        throw new Error("operator_account_page_mismatch");
      }
      commitPage(result);
    } catch (error) {
      if (generation !== listGeneration.current || !requestOwnsScope(scope, userId, csrfToken)) return;
      setAccounts({
        value: unavailableSource("control-plane+sub2api"),
        loading: false,
        error: friendlyError(error)
      });
    }
  }, [commitPage, friendlyError, requestOwnsScope, unavailableSource]);

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

  const findAccountProjection = useCallback(async (
    session: AuthSession,
    scope: number,
    matcher: (account: OperatorAccountDTO) => boolean,
    preferredPage?: number
  ) => {
    const firstPage = preferredPage ?? 1;
    const readPage = async (requestedPage: number) => {
      const result = await getOperatorAccountsPage(requestedPage, pageSize);
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
  }, [requestOwnsScope]);

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
    if (!session || !activeRef.current || !account
      || !window.confirm("确认停用该客户？账号会立即停用；历史账单、收据和审计记录会保留。")) return;
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

  const setWorkspacePurchaseEligibility = useCallback(async (accountId: string, enabled: boolean) => {
    const session = currentSession();
    const account = currentAccount(accountId);
    const confirmation = enabled
      ? "确认授予该账户新购 Workspace 的资格？"
      : "确认撤销该账户新购 Workspace 的资格？已有 Workspace 不会受影响。";
    if (!session || !activeRef.current || !account || !window.confirm(confirmation)) return;
    const claim = claimAccount(accountId);
    if (!claim) return;
    const identity = accountIdentity(account);
    const reason = "operator_requested";
    const current = eligibilityIntents.current.get(accountId);
    const intent = current && current.enabled === enabled && current.reason === reason
      && sameAccountIdentity(current.identity, account)
      ? current
      : {
          accountId,
          identity,
          enabled,
          reason,
          idempotencyKey: `workspace-purchase-eligibility:${accountId}:${crypto.randomUUID()}`
        };
    eligibilityIntents.current.set(accountId, intent);
    const projectionPage = pageRef.current;
    const projectionGeneration = listGeneration.current;
    const scope = scopeGeneration.current;
    const userId = session.user.id;
    const csrfToken = session.csrfToken;
    try {
      const result = await setOperatorWorkspacePurchaseEligibilityCommand(
        accountId,
        enabled,
        reason,
        csrfToken,
        intent.idempotencyKey
      );
      if (accountClaims.current.get(accountId) !== claim || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!eligibilityCommandSucceeded(result, accountId, enabled)) {
        throw new Error("operator_account_eligibility_identity_mismatch");
      }
      const readback = await findAccountProjection(
        session,
        scope,
        (candidate) => sameAccountIdentity(intent.identity, candidate)
          && candidate.workspacePurchaseEnabled === enabled,
        projectionPage
      );
      if (accountClaims.current.get(accountId) !== claim || !requestOwnsScope(scope, userId, csrfToken)) return;
      if (!readback?.account) throw new Error("operator_account_eligibility_readback_mismatch");
      if (projectionGeneration === listGeneration.current && pageRef.current === projectionPage) {
        commitPage(readback.visibleResult);
      }
      if (eligibilityIntents.current.get(accountId) === intent) eligibilityIntents.current.delete(accountId);
      flash(enabled ? "已授予 Workspace 新购资格" : "已撤销 Workspace 新购资格");
    } catch (error) {
      if (accountClaims.current.get(accountId) === claim && requestOwnsScope(scope, userId, csrfToken)) {
        flash(mutationError(error), "danger");
      }
    } finally {
      releaseAccount(accountId, claim);
    }
  }, [claimAccount, commitPage, currentAccount, currentSession, findAccountProjection, flash, mutationError, releaseAccount, requestOwnsScope]);

  const pages = accounts.value?.available
    ? Math.ceil(accounts.value.data.total / accounts.value.data.pageSize)
    : 0;

  return {
    accounts,
    page,
    pages,
    provisionOperation,
    provisionBusy,
    busyAccountIds,
    refresh: load,
    changePage,
    setProvisionOperation: setProvisionOperationProjection,
    provision,
    disable,
    setWorkspacePurchaseEligibility,
    load,
    reset
  };
}
