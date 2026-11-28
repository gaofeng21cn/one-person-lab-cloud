import type {
  OperatorAccountCommandDTO,
  OperatorAccountDTO,
  OperatorLinkRelayEntitlementCommandDTO,
  OperatorPackageEntitlementCommandDTO,
  OperatorPlatformAdminCommandDTO,
  OperatorProvisionAccountCommandDTO,
  OperatorSeedLinkRelayEntitlementsCommandDTO,
  OperatorServiceEntitlementCommandDTO,
  OperatorWorkspacePurchaseEligibilityCommandDTO,
  ProvisionAccountRequest
} from "../api/dtos.ts";

export interface ProvisionAccountIntent {
  input: ProvisionAccountRequest;
  idempotencyKey: string;
}

export interface OperatorAccountIdentity {
  accountId: string;
  consoleUserId: string;
  sub2apiUserId: string;
  email: string;
}

function normalizedEmail(value: string) {
  return value.trim().toLowerCase();
}

export function isOperatorPlatformAdmin(account: Pick<OperatorAccountDTO, "accountId" | "role" | "platformAdminEnabled">) {
  return Boolean(account.platformAdminEnabled) || account.role === "admin" || account.accountId === "acct-admin";
}

export function isOperatorBootstrapAdmin(account: Pick<OperatorAccountDTO, "accountId" | "isBootstrapAdmin">) {
  if (typeof account.isBootstrapAdmin === "boolean") return account.isBootstrapAdmin;
  return account.accountId === "acct-admin";
}

export const packageEntitlementIdPattern = /^[a-z0-9][a-z0-9._-]{0,63}$/;

export function hasActivePackageEntitlement(
  account: Pick<OperatorAccountDTO, "packageEntitlements">,
  packageId: string,
  nowMs: number = Date.now()
) {
  const normalized = packageId.trim();
  if (!normalized || !account.packageEntitlements?.length) return false;
  return account.packageEntitlements.some((entry) => {
    if (entry.packageId !== normalized || !entry.enabled) return false;
    if (!entry.expiresAt) return true;
    const expiresMs = Date.parse(entry.expiresAt);
    return Number.isFinite(expiresMs) && expiresMs > nowMs;
  });
}

export function activePackageEntitlementIds(
  account: Pick<OperatorAccountDTO, "packageEntitlements">,
  nowMs: number = Date.now()
) {
  if (!account.packageEntitlements?.length) return [];
  const ids: string[] = [];
  for (const entry of account.packageEntitlements) {
    if (!entry.enabled) continue;
    if (entry.expiresAt) {
      const expiresMs = Date.parse(entry.expiresAt);
      if (!Number.isFinite(expiresMs) || expiresMs <= nowMs) continue;
    }
    if (!ids.includes(entry.packageId)) ids.push(entry.packageId);
  }
  return ids;
}

export function expectedWorkspacePurchaseEligibility(input: ProvisionAccountRequest) {
  return (input.admission ?? "full_cloud_customer") === "full_cloud_customer";
}

function normalizeProvisionInput(input: ProvisionAccountRequest): ProvisionAccountRequest {
  return {
    email: normalizedEmail(input.email),
    password: input.password,
    ...(input.name?.trim() ? { name: input.name.trim() } : {}),
    admission: input.admission ?? "full_cloud_customer"
  };
}

export function sameProvisionInput(left: ProvisionAccountRequest, right: ProvisionAccountRequest) {
  return normalizedEmail(left.email) === normalizedEmail(right.email)
    && left.password === right.password
    && (left.name?.trim() || "") === (right.name?.trim() || "")
    && (left.admission ?? "full_cloud_customer") === (right.admission ?? "full_cloud_customer");
}

export function resolveProvisionAccountIntent(
  current: ProvisionAccountIntent | null,
  input: ProvisionAccountRequest,
  createKey: () => string
): ProvisionAccountIntent {
  const normalized = normalizeProvisionInput(input);
  if (current && sameProvisionInput(current.input, normalized)) return current;
  return { input: normalized, idempotencyKey: createKey() };
}

export function accountIdentity(account: OperatorAccountDTO): OperatorAccountIdentity {
  return {
    accountId: account.accountId,
    consoleUserId: account.consoleUserId,
    sub2apiUserId: account.sub2apiUserId,
    email: normalizedEmail(account.email)
  };
}

export function sameAccountIdentity(expected: OperatorAccountIdentity, account: OperatorAccountDTO) {
  const actual = accountIdentity(account);
  return actual.accountId === expected.accountId
    && actual.consoleUserId === expected.consoleUserId
    && actual.sub2apiUserId === expected.sub2apiUserId
    && actual.email === expected.email;
}

export function accountCommandSucceeded(command: OperatorAccountCommandDTO, accountId: string) {
  return command.accountId === accountId
    && command.status === "succeeded"
    && Boolean(command.operationId.trim());
}

export function provisionCommandSucceeded(
  command: OperatorProvisionAccountCommandDTO,
  input: ProvisionAccountRequest
) {
  return accountCommandSucceeded(command, command.accountId)
    && Boolean(command.accountId.trim())
    && command.workspacePurchaseEnabled === expectedWorkspacePurchaseEligibility(input);
}

export function eligibilityCommandSucceeded(
  command: OperatorWorkspacePurchaseEligibilityCommandDTO,
  accountId: string,
  enabled: boolean
) {
  return accountCommandSucceeded(command, accountId)
    && command.workspacePurchaseEnabled === enabled;
}

export function serviceEntitlementCommandSucceeded(
  command: OperatorServiceEntitlementCommandDTO,
  accountId: string,
  serviceId: string,
  enabled: boolean
) {
  return accountCommandSucceeded(command, accountId)
    && command.serviceId === serviceId
    && command.effectiveEnabled === enabled;
}

export function platformAdminCommandSucceeded(
  command: OperatorPlatformAdminCommandDTO,
  accountId: string,
  enabled: boolean
) {
  return accountCommandSucceeded(command, accountId)
    && command.platformAdminEnabled === enabled;
}

export function linkRelayCommandSucceeded(
  command: OperatorLinkRelayEntitlementCommandDTO,
  accountId: string,
  enabled: boolean
) {
  return accountCommandSucceeded(command, accountId)
    && command.linkRelayEnabled === enabled;
}

export function seedLinkRelayCommandSucceeded(
  command: OperatorSeedLinkRelayEntitlementsCommandDTO
) {
  return command.status === "succeeded"
    && Boolean(command.operationId.trim())
    && Number.isInteger(command.scannedCount) && command.scannedCount >= 0
    && Number.isInteger(command.seededCount) && command.seededCount >= 0
    && Number.isInteger(command.existingCount) && command.existingCount >= 0
    && command.seededCount + command.existingCount <= command.scannedCount;
}

export function packageEntitlementCommandSucceeded(
  command: OperatorPackageEntitlementCommandDTO,
  accountId: string,
  packageId: string,
  enabled: boolean
) {
  return accountCommandSucceeded(command, accountId)
    && command.packageId === packageId
    && command.enabled === enabled;
}

export function provisionReadbackMatches(
  account: OperatorAccountDTO,
  command: OperatorProvisionAccountCommandDTO,
  input: ProvisionAccountRequest
) {
  const email = normalizedEmail(input.email);
  const gatewayFirstProjection = Boolean(command.sub2apiUserId)
    && !account.consoleUserId
    && account.sub2apiUserId === command.sub2apiUserId
    && account.accountId === command.sub2apiUserId;
  const identityMatches = gatewayFirstProjection
    ? (account.role === "user" || account.role === "owner" || account.role === "admin")
    : account.accountId === command.accountId
      && account.role === "owner"
      && Boolean(account.consoleUserId);
  return identityMatches
    && normalizedEmail(account.email) === email
    && account.status === "active"
    && Boolean(account.sub2apiUserId)
    && account.workspacePurchaseEnabled === expectedWorkspacePurchaseEligibility(input)
    && account.gatewayIdentity.available
    && account.gatewayIdentity.data.userId === account.sub2apiUserId
    && normalizedEmail(account.gatewayIdentity.data.email) === email
    && account.gatewayIdentity.data.status === "active";
}
