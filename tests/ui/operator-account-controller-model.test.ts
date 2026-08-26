import assert from "node:assert/strict";
import test from "node:test";

import type {
  OperatorAccountDTO,
  OperatorAccountCommandDTO,
  OperatorProvisionAccountCommandDTO,
  OperatorWorkspacePurchaseEligibilityCommandDTO,
  ProvisionAccountRequest
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  accountCommandSucceeded,
  eligibilityCommandSucceeded,
  expectedWorkspacePurchaseEligibility,
  provisionCommandSucceeded,
  provisionReadbackMatches,
  resolveProvisionAccountIntent,
  sameProvisionInput,
  type ProvisionAccountIntent
} from "../../apps/console-ui/src/app/operator-account-controller-model.ts";

const fullCustomerInput: ProvisionAccountRequest = {
  email: "owner@example.com",
  password: "CorrectHorseBatteryStaple!",
  name: "Owner",
  admission: "full_cloud_customer"
};

const provisionCommand: OperatorProvisionAccountCommandDTO = {
  operationId: "account-provision-operation-alpha",
  accountId: "acct-alpha",
  status: "succeeded",
  workspacePurchaseEnabled: true
};

const account: OperatorAccountDTO = {
  accountId: "acct-alpha",
  consoleUserId: "usr-alpha",
  role: "owner",
  sub2apiUserId: "41",
  email: "owner@example.com",
  status: "active",
  workspacePurchaseEnabled: true,
  gatewayIdentity: {
    source: "sub2api",
    status: "available",
    available: true,
    fetchedAt: "2026-08-26T00:00:00Z",
    data: { userId: "41", email: "owner@example.com", status: "active" }
  },
  wallet: {
    source: "sub2api",
    status: "available",
    available: true,
    fetchedAt: "2026-08-26T00:00:00Z",
    data: { userId: "41", currency: "USD", usdMicros: "0", status: "active" }
  },
  keyCount: {
    source: "sub2api",
    status: "available",
    available: true,
    fetchedAt: "2026-08-26T00:00:00Z",
    data: 0
  },
  usage: {
    source: "sub2api",
    status: "available",
    available: true,
    fetchedAt: "2026-08-26T00:00:00Z",
    data: { todayActualCostUsdMicros: 0, totalActualCostUsdMicros: 0 }
  },
  workspaceCount: {
    source: "control-plane",
    status: "available",
    available: true,
    fetchedAt: "2026-08-26T00:00:00Z",
    data: 0
  }
};

test("provision input normalization preserves one semantic command", () => {
  const input: ProvisionAccountRequest = {
    email: "  OWNER@Example.COM ",
    password: fullCustomerInput.password,
    name: "  Owner  "
  };

  const intent = resolveProvisionAccountIntent(null, input, () => "account-provision:new");

  assert.deepEqual(intent, {
    input: fullCustomerInput,
    idempotencyKey: "account-provision:new"
  });
  assert.equal(sameProvisionInput(intent.input, input), true);
  assert.equal(expectedWorkspacePurchaseEligibility(input), true);
  assert.equal(expectedWorkspacePurchaseEligibility({ ...input, admission: "gateway_only" }), false);
});

test("same provision semantics reuse the original key independently of field order", () => {
  const current: ProvisionAccountIntent = {
    input: fullCustomerInput,
    idempotencyKey: "account-provision:existing"
  };
  const reordered: ProvisionAccountRequest = {
    admission: "full_cloud_customer",
    name: " Owner ",
    password: fullCustomerInput.password,
    email: " OWNER@EXAMPLE.COM "
  };
  let keysCreated = 0;

  const result = resolveProvisionAccountIntent(current, reordered, () => {
    keysCreated += 1;
    return "account-provision:new";
  });

  assert.equal(result, current);
  assert.equal(keysCreated, 0);
});

test("changed provision semantics receive a new key", () => {
  const current: ProvisionAccountIntent = {
    input: fullCustomerInput,
    idempotencyKey: "account-provision:existing"
  };
  const changes: ProvisionAccountRequest[] = [
    { ...fullCustomerInput, email: "other@example.com" },
    { ...fullCustomerInput, password: "DifferentCorrectPassword!" },
    { ...fullCustomerInput, name: "Other Owner" },
    { ...fullCustomerInput, admission: "gateway_only" }
  ];

  for (const [index, input] of changes.entries()) {
    const key = `account-provision:new-${index}`;
    const result = resolveProvisionAccountIntent(current, input, () => key);
    assert.notEqual(result, current);
    assert.equal(result.idempotencyKey, key);
    assert.equal(sameProvisionInput(current.input, input), false);
  }
});

test("account commands fail closed on operation status or account identity mismatch", () => {
  const command: OperatorAccountCommandDTO = {
    operationId: "account-disable-operation-alpha",
    accountId: "acct-alpha",
    status: "succeeded"
  };

  assert.equal(accountCommandSucceeded(command, "acct-alpha"), true);
  assert.equal(accountCommandSucceeded({ ...command, accountId: "acct-beta" }, "acct-alpha"), false);
  assert.equal(accountCommandSucceeded({ ...command, status: "failed" }, "acct-alpha"), false);
  assert.equal(accountCommandSucceeded({ ...command, operationId: " " }, "acct-alpha"), false);
});

test("provision and eligibility commands require their exact eligibility target", () => {
  const eligibilityCommand: OperatorWorkspacePurchaseEligibilityCommandDTO = {
    operationId: "workspace-eligibility-operation-alpha",
    accountId: "acct-alpha",
    status: "succeeded",
    workspacePurchaseEnabled: false
  };

  assert.equal(provisionCommandSucceeded(provisionCommand, fullCustomerInput), true);
  assert.equal(
    provisionCommandSucceeded({ ...provisionCommand, workspacePurchaseEnabled: false }, fullCustomerInput),
    false
  );
  assert.equal(provisionCommandSucceeded({ ...provisionCommand, status: "failed" }, fullCustomerInput), false);
  assert.equal(eligibilityCommandSucceeded(eligibilityCommand, "acct-alpha", false), true);
  assert.equal(eligibilityCommandSucceeded(eligibilityCommand, "acct-beta", false), false);
  assert.equal(eligibilityCommandSucceeded(eligibilityCommand, "acct-alpha", true), false);
  assert.equal(
    eligibilityCommandSucceeded({ ...eligibilityCommand, status: "failed" }, "acct-alpha", false),
    false
  );
});

test("provision readback requires the complete authoritative account identity", () => {
  assert.equal(provisionReadbackMatches(account, provisionCommand, fullCustomerInput), true);

  const mismatches: OperatorAccountDTO[] = [
    { ...account, accountId: "acct-beta" },
    { ...account, email: "other@example.com" },
    { ...account, role: "admin" },
    { ...account, status: "disabled" },
    { ...account, consoleUserId: "" },
    { ...account, sub2apiUserId: "" },
    { ...account, workspacePurchaseEnabled: false },
    {
      ...account,
      gatewayIdentity: {
        source: "sub2api",
        status: "unavailable",
        available: false,
        fetchedAt: "2026-08-26T00:00:00Z",
        reasonCode: "identity_unavailable"
      }
    },
    { ...account, gatewayIdentity: { ...account.gatewayIdentity, data: { ...account.gatewayIdentity.data, userId: "42" } } },
    { ...account, gatewayIdentity: { ...account.gatewayIdentity, data: { ...account.gatewayIdentity.data, email: "other@example.com" } } },
    { ...account, gatewayIdentity: { ...account.gatewayIdentity, data: { ...account.gatewayIdentity.data, status: "disabled" } } }
  ];

  for (const mismatch of mismatches) {
    assert.equal(provisionReadbackMatches(mismatch, provisionCommand, fullCustomerInput), false);
  }
});
