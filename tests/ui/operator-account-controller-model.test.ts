import assert from "node:assert/strict";
import test from "node:test";

import type {
  OperatorAccountDTO,
  OperatorAccountCommandDTO,
  OperatorLinkRelayEntitlementCommandDTO,
  OperatorPackageEntitlementCommandDTO,
  OperatorPlatformAdminCommandDTO,
  OperatorProvisionAccountCommandDTO,
  OperatorSeedLinkRelayEntitlementsCommandDTO,
  OperatorServiceEntitlementCommandDTO,
  OperatorWorkspacePurchaseEligibilityCommandDTO,
  ProvisionAccountRequest
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  accountCommandSucceeded,
  activePackageEntitlementIds,
  eligibilityCommandSucceeded,
  expectedWorkspacePurchaseEligibility,
  hasActivePackageEntitlement,
  isOperatorBootstrapAdmin,
  isOperatorPlatformAdmin,
  linkRelayCommandSucceeded,
  packageEntitlementCommandSucceeded,
  platformAdminCommandSucceeded,
  provisionCommandSucceeded,
  provisionReadbackMatches,
  resolveProvisionAccountIntent,
  sameProvisionInput,
  seedLinkRelayCommandSucceeded,
  serviceEntitlementCommandSucceeded,
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

test("service entitlement commands require exact account, service, and effective state", () => {
  const command: OperatorServiceEntitlementCommandDTO = {
    operationId: "service-entitlement-1",
    accountId: "42",
    serviceId: "netfleet_cloud",
    status: "succeeded",
    enabled: false,
    effectiveEnabled: false,
    role: "user"
  };

  assert.equal(serviceEntitlementCommandSucceeded(command, "42", "netfleet_cloud", false), true);
  assert.equal(serviceEntitlementCommandSucceeded(command, "43", "netfleet_cloud", false), false);
  assert.equal(serviceEntitlementCommandSucceeded(command, "42", "link_relay", false), false);
  assert.equal(serviceEntitlementCommandSucceeded(command, "42", "netfleet_cloud", true), false);
  assert.equal(serviceEntitlementCommandSucceeded({ ...command, status: "failed" }, "42", "netfleet_cloud", false), false);
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

test("gateway-first provision readback matches without legacy console user id and validates platform admin commands", () => {
  const cloudProvisionCommand: OperatorProvisionAccountCommandDTO = {
    operationId: "account-provision-cloud-45",
    accountId: "acct-cloud-45",
    sub2apiUserId: "45",
    status: "succeeded",
    workspacePurchaseEnabled: true
  };
  const gatewayFirstAccount: OperatorAccountDTO = {
    ...account,
    accountId: "45",
    actorId: "45",
    consoleUserId: "",
    role: "user",
    sub2apiUserId: "45",
    platformAdminEnabled: false,
    isBootstrapAdmin: false,
    gatewayIdentity: {
      ...account.gatewayIdentity,
      data: { userId: "45", email: "owner@example.com", status: "active" }
    }
  };
  assert.equal(provisionReadbackMatches(gatewayFirstAccount, cloudProvisionCommand, fullCustomerInput), true);
  assert.equal(provisionReadbackMatches({ ...gatewayFirstAccount, sub2apiUserId: "46", accountId: "46" }, cloudProvisionCommand, fullCustomerInput), false);

  const adminCmd: OperatorPlatformAdminCommandDTO = {
    operationId: "platform-admin-45",
    accountId: "45",
    status: "succeeded",
    platformAdminEnabled: true,
    isBootstrapAdmin: false
  };
  assert.equal(platformAdminCommandSucceeded(adminCmd, "45", true), true);
  assert.equal(platformAdminCommandSucceeded(adminCmd, "46", true), false);
  assert.equal(platformAdminCommandSucceeded(adminCmd, "45", false), false);

  assert.equal(isOperatorPlatformAdmin(gatewayFirstAccount), false);
  assert.equal(isOperatorPlatformAdmin({ ...gatewayFirstAccount, platformAdminEnabled: true, role: "admin" }), true);
  assert.equal(isOperatorBootstrapAdmin({ ...gatewayFirstAccount, platformAdminEnabled: true, isBootstrapAdmin: false }), false);
  assert.equal(isOperatorBootstrapAdmin({ accountId: "43", isBootstrapAdmin: true }), true);

  const relayCmd: OperatorLinkRelayEntitlementCommandDTO = {
    operationId: "link-relay-45",
    accountId: "45",
    status: "succeeded",
    linkRelayEnabled: true
  };
  assert.equal(linkRelayCommandSucceeded(relayCmd, "45", true), true);
  assert.equal(linkRelayCommandSucceeded(relayCmd, "46", true), false);
  assert.equal(linkRelayCommandSucceeded(relayCmd, "45", false), false);

  const seedCmd: OperatorSeedLinkRelayEntitlementsCommandDTO = {
    operationId: "link-relay-seed-1",
    status: "succeeded",
    scannedCount: 5,
    seededCount: 3,
    existingCount: 2
  };
  assert.equal(seedLinkRelayCommandSucceeded(seedCmd), true);
  assert.equal(seedLinkRelayCommandSucceeded({ ...seedCmd, existingCount: 3 }), false);
  assert.equal(seedLinkRelayCommandSucceeded({ ...seedCmd, status: "failed" }), false);

  const pkgCmd: OperatorPackageEntitlementCommandDTO = {
    operationId: "pkg-entitlement-45-occ",
    accountId: "45",
    packageId: "occ",
    status: "succeeded",
    enabled: true
  };
  assert.equal(packageEntitlementCommandSucceeded(pkgCmd, "45", "occ", true), true);
  assert.equal(packageEntitlementCommandSucceeded(pkgCmd, "46", "occ", true), false);
  assert.equal(packageEntitlementCommandSucceeded(pkgCmd, "45", "glkvm-native", true), false);
  assert.equal(packageEntitlementCommandSucceeded(pkgCmd, "45", "occ", false), false);

  const nowMs = Date.parse("2026-11-28T06:00:00Z");
  const pkgAccount: OperatorAccountDTO = {
    ...gatewayFirstAccount,
    packageEntitlements: [
      { packageId: "occ", enabled: true, reason: "granted" },
      { packageId: "glkvm-native", enabled: true, reason: "expiring", expiresAt: "2026-11-28T05:59:59Z" },
      { packageId: "future-pkg", enabled: true, reason: "expiring", expiresAt: "2026-11-29T00:00:00Z" },
      { packageId: "revoked-pkg", enabled: false, reason: "revoked" }
    ]
  };
  assert.equal(hasActivePackageEntitlement(pkgAccount, "occ", nowMs), true);
  assert.equal(hasActivePackageEntitlement(pkgAccount, "glkvm-native", nowMs), false);
  assert.equal(hasActivePackageEntitlement(pkgAccount, "future-pkg", nowMs), true);
  assert.equal(hasActivePackageEntitlement(pkgAccount, "revoked-pkg", nowMs), false);
  assert.deepEqual(activePackageEntitlementIds(pkgAccount, nowMs), ["occ", "future-pkg"]);
});
