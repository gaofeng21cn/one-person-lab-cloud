/*
 * The deployed Workspace purchase: one buy, for one declared customer, against
 * a running product.
 *
 * The local qualification harness is not this entry and must not become it. That
 * harness builds a stack, brings it up, binds it to loopback and tears it down;
 * it is evidence about the platform. This entry does none of that. It assumes a
 * product is already deployed, refuses to run against anything that is not, and
 * buys exactly one Workspace for the customer whose own credentials it was
 * given.
 *
 * The purchase sequence itself is the shared invariant in
 * workspace-purchase-core, and so is the quote policy. This file owns only what
 * is genuinely this entry's: the guard that keeps it pointed at a real
 * deployment, and the declared inputs it may act on.
 *
 * It buys and it does nothing else. It does not delete, does not refund, does
 * not provision an account, does not touch a database and does not assert that
 * the Workspace is good. Those belong to the owners of those facts.
 */

import { launchOperationId, launchWorkspaceId, priceWorkspaceLaunch, submitWorkspaceLaunch } from "./workspace-purchase-core.ts";

const PACKAGE_ID = /^[a-z0-9][a-z0-9_-]{0,63}$/;
const STORAGE_SIZE_GB = /^(0*[1-9][0-9]{0,4})$/;
const RENEWAL_CHOICE = new Set(["true", "false"]);
const DIGEST = /^sha256:[a-f0-9]{64}$/;
const WORKSPACE_NAME = /^[\x20-\x7e]{1,120}$/;

const TARGET_ERROR = "deployed purchase target is not a deployed production target";

/*
 * The mirror of the local harness's guard.
 *
 * The harness refuses production authority, and this refuses anything that is
 * not production authority, so neither can be run in place of the other. It
 * also refuses loopback and private-range origins: a deployed purchase that
 * answered from the local stack would be a real purchase recorded against the
 * wrong evidence, which is worse than no purchase at all.
 */
export function assertDeployedPurchaseTarget(o) {
  const baseURL = String(o?.baseURL || "");
  if (!/^https:\/\/[^\s/]+(?::\d+)?(?:\/|$)/.test(baseURL) || o?.authorityClass !== "production") {
    throw new Error(TARGET_ERROR);
  }
  let hostname;
  try {
    // A URL keeps the brackets around an IPv6 literal, so "[::1]" has to be
    // unwrapped before it can be compared with an address.
    const raw = new URL(baseURL).hostname.toLowerCase();
    hostname = raw.startsWith("[") && raw.endsWith("]") ? raw.slice(1, -1) : raw;
  } catch {
    throw new Error(TARGET_ERROR);
  }
  if (hostname === "localhost" || hostname.endsWith(".localhost") || hostname.endsWith(".local") ||
      hostname === "0.0.0.0" || hostname === "::" || hostname === "::1" ||
      /^127\./.test(hostname) || /^10\./.test(hostname) ||
      /^192\.168\./.test(hostname) || /^169\.254\./.test(hostname) ||
      /^172\.(1[6-9]|2\d|3[01])\./.test(hostname) ||
      // IPv6 unique-local and link-local, the IPv6 equivalents of the ranges above.
      /^f[cd][0-9a-f]{2}:/.test(hostname) || /^fe[89ab][0-9a-f]:/.test(hostname)) {
    throw new Error(TARGET_ERROR);
  }
  if (!WORKSPACE_NAME.test(String(o?.workspaceName || "")) || !PACKAGE_ID.test(String(o?.packageId || "")) ||
      !STORAGE_SIZE_GB.test(String(o?.storageSizeGb || "")) || !RENEWAL_CHOICE.has(String(o?.autoRenew || "")) ||
      !String(o?.idempotencyIdentity || "") || !DIGEST.test(String(o?.accountEmailIdentityDigest || "")) ||
      typeof o?.customerEmail !== "string" || !o.customerEmail.includes("@") || o.customerEmail.length > 320 ||
      typeof o?.customerPassword !== "string" || o.customerPassword === "") {
    throw new Error(TARGET_ERROR);
  }
}

/*
 * Every declared input is read by name and none of them has a default. A
 * purchase that silently defaulted a package or a size would be buying something
 * the dispatch never named.
 */
export function deployedPurchaseOptionsFromEnvironment(environment = process.env) {
  const options = {
    baseURL: String(environment.OPL_DEPLOYED_PURCHASE_BASE_URL || ""),
    authorityClass: String(environment.OPL_DEPLOYED_PURCHASE_AUTHORITY_CLASS || ""),
    customerEmail: String(environment.OPL_DEPLOYED_PURCHASE_CUSTOMER_EMAIL || ""),
    customerPassword: String(environment.OPL_DEPLOYED_PURCHASE_CUSTOMER_PASSWORD || ""),
    accountEmailIdentityDigest: String(environment.OPL_DEPLOYED_PURCHASE_ACCOUNT_DIGEST || ""),
    packageId: String(environment.OPL_DEPLOYED_PURCHASE_PACKAGE_ID || ""),
    storageSizeGb: String(environment.OPL_DEPLOYED_PURCHASE_STORAGE_SIZE_GB || ""),
    autoRenew: String(environment.OPL_DEPLOYED_PURCHASE_AUTO_RENEW || ""),
    idempotencyIdentity: String(environment.OPL_DEPLOYED_PURCHASE_IDENTITY || ""),
    workspaceName: String(environment.OPL_DEPLOYED_PURCHASE_WORKSPACE_NAME || ""),
  };
  assertDeployedPurchaseTarget(options);
  return options;
}

/*
 * Buy one Workspace for the declared customer, under the declared idempotency
 * identity, and return only what the caller may bind its own evidence to. No
 * credential, session, balance, raw identifier or access URL is returned.
 */
export async function runDeployedWorkspacePurchase(options, { createHTTP, login, wait } = {}) {
  assertDeployedPurchaseTarget(options);
  if (!createHTTP || !login) throw new Error("deployed purchase dependencies are not provided");
  const auth = await login(options);
  const http = createHTTP(options.baseURL);
  const packageId = options.packageId;

  // The platform derives both identities from the account that holds the command
  // and the declared idempotency identity, so the account is read back from the
  // customer's own session first. Predicting them before the answer arrives is
  // what makes an order belonging to somebody else refusable rather than merely
  // noticed afterwards.
  const accountId = String((await http.json("/api/auth/me", {}, auth)).payload?.accountId || "");
  if (!accountId) throw new Error("deployed purchase could not read the declared customer account");

  const pricing = (await http.json("/api/pricing/preview", {
    method: "POST", body: { resourceType: "workspace", packageId, sizeGb: Number(options.storageSizeGb) },
  }, auth)).payload;
  const quoteTotalUsdMicros = priceWorkspaceLaunch({
    pricing, walletUsdMicros: (await http.json("/api/gateway/wallet", {}, auth)).payload?.usdMicros, packageId,
  });

  const idempotencyKey = `deployed-workspace:${options.idempotencyIdentity}`;
  const operationId = launchOperationId(accountId, idempotencyKey);
  const { launch, receiptId } = await submitWorkspaceLaunch({
    http, auth, name: options.workspaceName, packageId, autoRenew: options.autoRenew === "true",
    idempotencyKey, expectedOperationId: operationId,
    expectedWorkspaceId: launchWorkspaceId(accountId, operationId),
    wait,
  });
  return { quoteTotalUsdMicros, receiptId, operationId: launch.operationId, workspaceId: launch.workspaceId };
}