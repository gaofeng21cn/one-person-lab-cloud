import assert from "node:assert/strict";
import test from "node:test";

// The quote policy is part of the invariant, not of any one buyer: the local
// harness and the deployed entry must price a launch by the same rule, or the
// sequence is shared but its meaning is not.
import { priceWorkspaceLaunch } from "../../tools/workspace-purchase-core.ts";
import {
  assertDeployedPurchaseTarget,
  deployedPurchaseOptionsFromEnvironment,
} from "../../tools/deployed-workspace-purchase.ts";

// The deployed entry is the only automated buyer that may reach a real customer.
// Its guard is deliberately the mirror image of the local harness's: the local
// harness refuses production authority, and this refuses anything that is not
// production authority or that points at the local stack. A mistake in one is
// caught by the other.
const ACCOUNT_DIGEST = `sha256:${"a".repeat(64)}`;

// A complete, valid target. Each test varies exactly one thing from this, so a
// rejection can only be caused by the thing that was varied.
const validTarget = () => ({
  baseURL: "https://cloud.medopl.com",
  authorityClass: "production",
  customerEmail: "customer@example.invalid",
  customerPassword: "customer-password",
  accountEmailIdentityDigest: ACCOUNT_DIGEST,
  packageId: "pro",
  storageSizeGb: "20",
  autoRenew: "true",
  idempotencyIdentity: "declared-identity",
  workspaceName: "Declared workspace",
});

test("the deployed buyer requires production authority", () => {
  assert.doesNotThrow(() => assertDeployedPurchaseTarget(validTarget()));
  for (const authorityClass of ["sandbox", "preproduction", "fixture", "", undefined]) {
    assert.throws(
      () => assertDeployedPurchaseTarget({ ...validTarget(), authorityClass }),
      /deployed purchase target/i,
      String(authorityClass),
    );
  }
});

test("the deployed buyer refuses the local stack it can never legitimately reach", () => {
  for (const baseURL of [
    "http://127.0.0.1:8080",
    "http://localhost:8080",
    "https://127.0.0.1:8080",
    "https://localhost:8080",
    "https://[::1]:8080",
    "https://0.0.0.0",
    "http://cloud.medopl.com",
    "cloud.medopl.com",
  ]) {
    assert.throws(
      () => assertDeployedPurchaseTarget({ ...validTarget(), baseURL }),
      /deployed purchase target/i,
      baseURL,
    );
  }
});

test("a declared package, storage size and renewal choice are required rather than defaulted", () => {
  const valid = validTarget();
  assert.doesNotThrow(() => assertDeployedPurchaseTarget(valid));
  for (const missing of ["packageId", "storageSizeGb", "autoRenew", "idempotencyIdentity", "workspaceName"]) {
    const without = { ...valid };
    delete without[missing];
    assert.throws(() => assertDeployedPurchaseTarget(without), /deployed purchase target/i, missing);
  }
});

test("the renewal choice is exactly true or false, never anything else truthy", () => {
  for (const autoRenew of ["TRUE", "1", "yes", "", "false "]) {
    assert.throws(
      () => assertDeployedPurchaseTarget({ ...validTarget(), autoRenew }),
      /deployed purchase target/i,
      JSON.stringify(autoRenew),
    );
  }
});

test("the environment reader refuses an unconfigured credential instead of admitting an empty one", () => {
  const environment = {
    OPL_DEPLOYED_PURCHASE_BASE_URL: "https://cloud.medopl.com",
    OPL_DEPLOYED_PURCHASE_AUTHORITY_CLASS: "production",
    OPL_DEPLOYED_PURCHASE_CUSTOMER_EMAIL: "",
    OPL_DEPLOYED_PURCHASE_CUSTOMER_PASSWORD: "customer-password",
    OPL_DEPLOYED_PURCHASE_ACCOUNT_DIGEST: ACCOUNT_DIGEST,
    OPL_DEPLOYED_PURCHASE_PACKAGE_ID: "pro",
    OPL_DEPLOYED_PURCHASE_STORAGE_SIZE_GB: "20",
    OPL_DEPLOYED_PURCHASE_AUTO_RENEW: "true",
    OPL_DEPLOYED_PURCHASE_IDENTITY: "declared-identity",
    OPL_DEPLOYED_PURCHASE_WORKSPACE_NAME: "Declared workspace",
  };
  assert.throws(() => deployedPurchaseOptionsFromEnvironment(environment), /deployed purchase target|customer/i);
  assert.doesNotThrow(() => deployedPurchaseOptionsFromEnvironment({ ...environment, OPL_DEPLOYED_PURCHASE_CUSTOMER_EMAIL: "customer@example.invalid" }));
});

test("a quote is priced once, by one policy, whoever asks", () => {
  const valid = { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "52580000" };
  assert.equal(priceWorkspaceLaunch({ pricing: valid, walletUsdMicros: "52580000", packageId: "pro" }), "52580000");
  assert.equal(priceWorkspaceLaunch({ pricing: valid, walletUsdMicros: "999999999", packageId: "pro" }), "52580000");
});

test("a quote that does not name the declared thing is refused", () => {
  const base = { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "52580000" };
  const wallet = "999999999";
  for (const wrong of [
    { resourceType: "gateway", packageId: "pro", currency: "USD", totalChargeUsdMicros: "52580000" },
    { resourceType: "workspace", packageId: "basic", currency: "USD", totalChargeUsdMicros: "52580000" },
    { resourceType: "workspace", packageId: "pro", currency: "CNY", totalChargeUsdMicros: "52580000" },
    { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "0" },
    { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "-100" },
    { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "" },
  ]) {
    assert.throws(() => priceWorkspaceLaunch({ pricing: wrong, walletUsdMicros: wallet, packageId: "pro" }), /quote/i, JSON.stringify(wrong));
  }
});

test("a wallet one micro short is short, and a wallet that cannot answer is not a large balance", () => {
  const pricing = { resourceType: "workspace", packageId: "pro", currency: "USD", totalChargeUsdMicros: "52580000" };
  assert.throws(() => priceWorkspaceLaunch({ pricing, walletUsdMicros: "52579999", packageId: "pro" }), /quote/i);
  assert.throws(() => priceWorkspaceLaunch({ pricing, walletUsdMicros: "", packageId: "pro" }), /quote/i);
  assert.throws(() => priceWorkspaceLaunch({ pricing, walletUsdMicros: "not-a-number", packageId: "pro" }), /quote/i);
});