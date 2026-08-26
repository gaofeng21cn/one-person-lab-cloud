import type {
  OperatorAccountCommandDTO,
  OperatorAccountDTO,
  OperatorProvisionAccountCommandDTO,
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

export function provisionReadbackMatches(
  account: OperatorAccountDTO,
  command: OperatorProvisionAccountCommandDTO,
  input: ProvisionAccountRequest
) {
  const email = normalizedEmail(input.email);
  return account.accountId === command.accountId
    && normalizedEmail(account.email) === email
    && account.status === "active"
    && account.role === "owner"
    && Boolean(account.consoleUserId)
    && Boolean(account.sub2apiUserId)
    && account.workspacePurchaseEnabled === expectedWorkspacePurchaseEligibility(input)
    && account.gatewayIdentity.available
    && account.gatewayIdentity.data.userId === account.sub2apiUserId
    && normalizedEmail(account.gatewayIdentity.data.email) === email
    && account.gatewayIdentity.data.status === "active";
}
