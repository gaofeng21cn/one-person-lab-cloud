/*
 * The deployed Workspace purchase: one buy, for one declared customer, against
 * a running product.
 *
 * The local qualification harness is not this entry and must not become it. That
 * harness builds a stack, brings it up, binds it to loopback and tears it down;
 * it is evidence about the platform. This entry does none of that. It assumes a
 * product is already deployed, refuses to run against anything that is not, and
 * buys exactly one Workspace for the customer whose session its caller supplies.
 *
 * There is no credential here. The protected flow that invokes this entry owns
 * the login and hands over an already-authenticated transport; this entry reads
 * that session's account back through the owner's source envelope and refuses to
 * spend anything until the readback matches the declared account digest. The
 * caller also declares the exact Candidate the purchase belongs to, and the
 * receipt this entry returns binds that Candidate, that identity and the
 * mutation it caused.
 *
 * The purchase sequence itself is the shared invariant in
 * workspace-purchase-core, and so is the quote policy. This module owns only
 * what is genuinely this entry's: the guard that keeps it pointed at a real
 * deployment, the replay readback, the declared inputs it may act on, and the
 * receipt its caller binds its own evidence to.
 *
 * It buys and it does nothing else. It does not delete, does not refund, does
 * not provision an account, does not touch a database and does not assert that
 * the Workspace is good. Those belong to the owners of those facts.
 */

import { createHash } from "node:crypto";

import { sourceData } from "./local-workspace-qualification.ts";
import {
  launchOperationId,
  launchWorkspaceId,
  priceWorkspaceLaunch,
  submitWorkspaceLaunch,
  waitForLaunch,
} from "./workspace-purchase-core.ts";

const PACKAGE_ID = /^[a-z0-9][a-z0-9_-]{0,63}$/;
const WORKSPACE_NAME = /^[\x20-\x7e]{1,120}$/;
const IDEMPOTENCY_IDENTITY = /^[A-Za-z0-9._:-]{1,128}$/;
const IDEMPOTENCY_KEY = /^deployed-workspace:[0-9a-f]{64}:[A-Za-z0-9._:-]{1,128}$/;
const DIGEST = /^sha256:[a-f0-9]{64}$/;
const WALLET_MICROS = /^(?:0|[1-9][0-9]*)$/;
const POSITIVE_MICROS = /^[1-9][0-9]*$/;
const OPERATION_ID = /^workspace-launch-[0-9a-f]{18}$/;
const WORKSPACE_ID = /^ws-[0-9a-f]{18}$/;

const TARGET_ERROR = "deployed purchase target is not a deployed production target";

// The receipt kind names the purchase, not the qualification. It joins the
// protected flow's business receipt only through the Candidate digest and the
// platform-minted operation identities; it does not claim the qualification.
export const DEPLOYED_PURCHASE_RECEIPT_KIND = "opl.deployed-workspace-purchase.v1";

// Every declared input is named, and nothing else is accepted. A request that
// carried a credential, a client price or a storage size would either be a
// secret surface or a second pricing authority, so an unknown key is refused
// rather than silently ignored.
const REQUEST_KEYS = [
  "baseURL",
  "authorityClass",
  "candidateReceiptDigest",
  "accountEmailIdentityDigest",
  "packageId",
  "autoRenew",
  "idempotencyIdentity",
  "workspaceName",
];
const RECEIPT_KEYS = [
  "schemaVersion",
  "kind",
  "candidate",
  "account",
  "product",
  "operation",
  "purchase",
  "mutations",
  "startedAt",
  "completedAt",
];
const CANDIDATE_KEYS = ["receiptDigest"];
const ACCOUNT_KEYS = ["identityDigest"];
const PRODUCT_KEYS = ["resourceType", "packageId", "autoRenew"];
const OPERATION_KEYS = ["idempotencyKey", "operationId", "workspaceId", "purchaseReceiptId", "resumed"];
const PURCHASE_KEYS = ["amountUsdMicros"];
const MUTATION_KEYS = ["declared", "observed"];
const PURCHASE_MUTATION_KEYS = ["purchases"];

function hasExactKeys(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

function purchaseError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

const targetError = () => purchaseError("deployed_purchase_target_invalid", TARGET_ERROR);
const accountUnavailable = () => purchaseError(
  "deployed_purchase_account_unavailable",
  "deployed purchase could not read the declared customer account",
);
const walletUnavailable = () => purchaseError(
  "deployed_purchase_wallet_unavailable",
  "deployed purchase could not read the customer wallet",
);

/*
 * The mirror of the local harness's guard.
 *
 * The harness refuses production authority, and this refuses anything that is
 * not production authority, so neither can be run in place of the other. It
 * also refuses loopback and private-range origins: a deployed purchase that
 * answered from the local stack would be a real purchase recorded against the
 * wrong evidence, which is worse than no purchase at all. The declared target
 * must be an origin, because every route below is appended to it.
 */
export function assertDeployedPurchaseTarget(options) {
  let target;
  try {
    target = new URL(String(options?.baseURL || ""));
  } catch {
    throw targetError();
  }
  if (target.protocol !== "https:" || target.username || target.password ||
      target.pathname !== "/" || target.search || target.hash ||
      options?.authorityClass !== "production") {
    throw targetError();
  }
  // A URL keeps the brackets around an IPv6 literal, so "[::1]" has to be
  // unwrapped before it can be compared with an address.
  const hostname = target.hostname.toLowerCase();
  const bare = hostname.startsWith("[") && hostname.endsWith("]") ? hostname.slice(1, -1) : hostname;
  if (bare === "localhost" || bare.endsWith(".localhost") || bare.endsWith(".local") ||
      bare === "0.0.0.0" || bare === "::" || bare === "::1" ||
      /^127\./.test(bare) || /^10\./.test(bare) || /^192\.168\./.test(bare) ||
      /^169\.254\./.test(bare) || /^172\.(1[6-9]|2\d|3[01])\./.test(bare) ||
      // IPv6 unique-local and link-local, the IPv6 equivalents of the ranges above.
      /^f[cd][0-9a-f]{2}:/i.test(bare) || /^fe[89ab][0-9a-f]:/i.test(bare)) {
    throw targetError();
  }
  return target.origin;
}

/*
 * Every declared input is read by name and none of them has a default. A
 * purchase that silently defaulted a package, a renewal choice or an identity
 * would be buying something the dispatch never named.
 */
export function assertDeployedPurchaseRequest(request) {
  assertDeployedPurchaseTarget(request);
  if (!hasExactKeys(request, REQUEST_KEYS) ||
      !DIGEST.test(String(request?.candidateReceiptDigest || "")) ||
      !DIGEST.test(String(request?.accountEmailIdentityDigest || "")) ||
      !PACKAGE_ID.test(String(request?.packageId || "")) ||
      typeof request?.autoRenew !== "boolean" ||
      !IDEMPOTENCY_IDENTITY.test(String(request?.idempotencyIdentity || "")) ||
      !WORKSPACE_NAME.test(String(request?.workspaceName || "")) ||
      // The Control Plane trims the name before it persists the order, so a
      // declared name with padding could never match the replay readback.
      String(request?.workspaceName || "") !== String(request?.workspaceName || "").trim()) {
    throw targetError();
  }
  return request;
}

/*
 * The platform idempotency identity this entry declares.
 *
 * The Candidate is part of the key so a replay for another Candidate addresses
 * another order; the caller supplies the remaining declared identity. Exported
 * so the protected caller can derive the same order identity without copying
 * the format.
 */
export function deployedPurchaseIdempotencyKey(candidateReceiptDigest, idempotencyIdentity) {
  const digest = String(candidateReceiptDigest || "");
  const identity = String(idempotencyIdentity || "");
  if (!DIGEST.test(digest) || !IDEMPOTENCY_IDENTITY.test(identity)) throw targetError();
  return `deployed-workspace:${digest.slice("sha256:".length)}:${identity}`;
}

/*
 * The declared-identity digest.
 *
 * The Control Plane normalizes an account email before it stores or compares it
 * (`normalizeEmail`), so the digest is taken over the same trimmed, lowercased
 * text. The caller computes it from the customer it authorized; this entry
 * computes it again from the session it was handed and refuses to price or buy
 * unless they are the same account. The raw email is never returned.
 */
export function deployedAccountIdentityDigest(email) {
  const normalized = String(email || "").trim().toLowerCase();
  if (!normalized || normalized.length > 320 || !normalized.includes("@")) throw accountUnavailable();
  return `sha256:${createHash("sha256").update(normalized).digest("hex")}`;
}

/*
 * The declared inputs a protected workflow may read from its own environment.
 *
 * Credentials are deliberately absent. The workflow owns its secret custody and
 * hands this entry an authenticated session; a customer password in this
 * process's environment would be a second credential surface that buys nothing.
 * The Candidate digest is a declaration like the package or the identity: the
 * receipt binds it, so a dispatch that does not name one is refused.
 */
export function deployedPurchaseRequestFromEnvironment(environment = process.env) {
  const renewal = environment.OPL_DEPLOYED_PURCHASE_AUTO_RENEW;
  const request = {
    baseURL: String(environment.OPL_DEPLOYED_PURCHASE_BASE_URL || ""),
    authorityClass: String(environment.OPL_DEPLOYED_PURCHASE_AUTHORITY_CLASS || ""),
    candidateReceiptDigest: String(environment.OPL_DEPLOYED_PURCHASE_CANDIDATE_DIGEST || ""),
    accountEmailIdentityDigest: String(environment.OPL_DEPLOYED_PURCHASE_ACCOUNT_DIGEST || ""),
    packageId: String(environment.OPL_DEPLOYED_PURCHASE_PACKAGE_ID || ""),
    autoRenew: renewal === "true" ? true : renewal === "false" ? false : "",
    idempotencyIdentity: String(environment.OPL_DEPLOYED_PURCHASE_IDENTITY || ""),
    workspaceName: String(environment.OPL_DEPLOYED_PURCHASE_WORKSPACE_NAME || ""),
  };
  assertDeployedPurchaseRequest(request);
  return request;
}

/*
 * The caller boundary.
 *
 * The protected flow already holds the customer session, so it supplies the
 * authenticated transport instead of this entry logging in. The transport must
 * answer for the target the request declared, and it must expose both the raw
 * request and the JSON helper: the source envelope is read at whatever HTTP
 * status the owner answered with, and the launch poll is the purchase core's.
 * The optional `now` clock exists so a test can force a backward step; a real
 * caller omits it and the entry reads the wall clock.
 */
export function assertDeployedPurchaseCaller(caller, request) {
  if (!caller || typeof caller !== "object" ||
      typeof caller.http?.request !== "function" || typeof caller.http?.json !== "function" ||
      !caller.auth || typeof caller.auth !== "object" ||
      (caller.wait !== undefined && typeof caller.wait !== "function") ||
      (caller.now !== undefined && typeof caller.now !== "function")) {
    throw purchaseError("deployed_purchase_caller_invalid", "deployed purchase caller is not provided");
  }
  let origin;
  try {
    origin = new URL(String(caller.origin || "")).origin;
  } catch {
    throw purchaseError("deployed_purchase_caller_invalid", "deployed purchase caller is not the declared target");
  }
  if (origin !== assertDeployedPurchaseTarget(request)) {
    throw purchaseError("deployed_purchase_caller_invalid", "deployed purchase caller is not the declared target");
  }
  return { origin, http: caller.http, auth: caller.auth, wait: caller.wait, now: caller.now };
}

async function readOwnerEnvelope(http, auth, path, source, fail) {
  let result;
  try {
    result = await http.request(path, {}, auth);
  } catch {
    throw fail();
  }
  try {
    return sourceData(result?.payload, source);
  } catch {
    throw fail();
  }
}

async function readLaunch(http, auth, operationId, workspaceId) {
  let result;
  try {
    result = await http.request(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth);
  } catch {
    throw purchaseError("deployed_purchase_launch_readback_unavailable", "deployed purchase could not read its own launch");
  }
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200) {
    throw purchaseError("deployed_purchase_launch_readback_unavailable", "deployed purchase could not read its own launch");
  }
  const launch = result?.payload;
  if (!launch || launch.operationId !== operationId || launch.workspaceId !== workspaceId) {
    throw purchaseError("deployed_purchase_operation_mismatch", "deployed purchase read an order that is not its own");
  }
  return launch;
}

function assertSameDeclaredLaunch(launch, declared) {
  if (launch.operationId !== declared.operationId || launch.workspaceId !== declared.workspaceId ||
      launch.accountId !== declared.accountId || launch.packageId !== declared.packageId ||
      launch.autoRenew !== declared.autoRenew || launch.name !== declared.workspaceName) {
    throw purchaseError("deployed_purchase_replay_conflict", "deployed purchase found an order for a different declaration");
  }
}

function admittedUsdMicros(launch) {
  const amount = String(launch?.totalChargeUsdMicros ?? "");
  if (!POSITIVE_MICROS.test(amount)) {
    throw purchaseError("deployed_purchase_price_unavailable", "deployed purchase could not read the price its order was admitted at");
  }
  return amount;
}

/*
 * Buy one Workspace for the declared customer and return the receipt the caller
 * may bind its own evidence to.
 *
 * The order is read back before anything is priced, so a replay of the same
 * declared identity resumes its own order and spends nothing again. A first
 * delivery prices the declared package, checks that the wallet can fund the
 * quote, and issues exactly one command through the shared core. A command whose
 * answer was lost is looked up under its deterministic identity rather than
 * reissued; that delivery still caused the purchase, so its receipt says so.
 *
 * The returned receipt carries no credential, session, balance, raw account
 * identifier or access URL. It carries the Candidate digest, the account digest,
 * the platform-minted operation and Workspace identities, the admitted price and
 * the bounded mutation count.
 */
export async function runDeployedWorkspacePurchase(request, caller) {
  assertDeployedPurchaseRequest(request);
  const { http, auth, wait, now } = assertDeployedPurchaseCaller(caller, request);
  // The receipt orders its two timestamps. A wall clock can step backwards
  // after a command was accepted, and throwing at that point would lose the
  // fact that money was spent, so the completion is clamped to the start.
  const clock = now || (() => new Date());
  const started = new Date(clock());
  const startedAt = started.toISOString();
  const packageId = request.packageId;
  const autoRenew = request.autoRenew;
  // The platform key is scoped to the Candidate as well as the declared
  // identity. A replay for a different Candidate therefore addresses a different
  // order, so a receipt cannot bind one Candidate to an order admitted for
  // another.
  const idempotencyKey = deployedPurchaseIdempotencyKey(
    request.candidateReceiptDigest,
    request.idempotencyIdentity,
  );

  // The session is asked who it is before anything is priced or bought. A
  // transport holding another account costs one read and no money.
  const me = await readOwnerEnvelope(http, auth, "/api/auth/me", "sub2api", accountUnavailable);
  const accountId = String(me?.accountId || "");
  if (!accountId || me?.role !== "owner" || me?.status !== "active") throw accountUnavailable();
  const identityDigest = deployedAccountIdentityDigest(me?.email);
  if (identityDigest !== request.accountEmailIdentityDigest) {
    throw purchaseError("deployed_purchase_account_mismatch", "deployed purchase account digest does not match the declared customer");
  }

  // The platform derives both identities from the account that owns the session
  // and the declared idempotency identity, so they are announced before the
  // order is read or issued. An answer belonging to another order is refused.
  const operationId = launchOperationId(accountId, idempotencyKey);
  const workspaceId = launchWorkspaceId(accountId, operationId);
  const declared = { accountId, operationId, workspaceId, packageId, autoRenew, workspaceName: request.workspaceName };

  const existing = await readLaunch(http, auth, operationId, workspaceId);
  let launch;
  let resumed = false;
  if (existing) {
    assertSameDeclaredLaunch(existing, declared);
    resumed = true;
    launch = await waitForLaunch(http, operationId, auth, wait);
  } else {
    const wallet = await readOwnerEnvelope(http, auth, "/api/gateway/wallet", "sub2api", walletUnavailable);
    const walletUsdMicros = String(wallet?.usdMicros || "");
    if (!WALLET_MICROS.test(walletUsdMicros)) throw walletUnavailable();

    // The platform prices the launch and refuses a client that supplies a size
    // or a price, so the preview carries only the declared resource and package.
    let pricing;
    try {
      pricing = (await http.json("/api/pricing/preview", {
        method: "POST", body: { resourceType: "workspace", packageId },
      }, auth)).payload;
    } catch {
      throw purchaseError("deployed_purchase_quote_unavailable", "deployed purchase could not price the declared Workspace");
    }
    priceWorkspaceLaunch({ pricing, walletUsdMicros, packageId });

    try {
      ({ launch } = await submitWorkspaceLaunch({
        http, auth, name: request.workspaceName, packageId, autoRenew,
        idempotencyKey, expectedOperationId: operationId, expectedWorkspaceId: workspaceId, wait,
      }));
    } catch (error) {
      // The command may have been accepted with its answer lost. That is not a
      // second purchase, it is the same order, and the only question left is
      // whether the platform holds it. It is not a resume either: this delivery
      // issued the command, so whatever it caused is this delivery's to report.
      let recovered;
      try {
        recovered = await readLaunch(http, auth, operationId, workspaceId);
      } catch (readbackError) {
        // Both the command answer and the recovery readback failed. Keep the
        // command error as the cause so the caller can tell a lost response
        // from a refused or malformed one when it maps the terminal reason.
        if (readbackError && typeof readbackError === "object") readbackError.cause = error;
        throw readbackError;
      }
      if (!recovered) throw error;
      assertSameDeclaredLaunch(recovered, declared);
      launch = await waitForLaunch(http, operationId, auth, wait);
    }
  }

  const purchaseReceiptId = String(launch?.receiptId || "");
  if (!purchaseReceiptId) {
    throw purchaseError("deployed_purchase_receipt_missing", "deployed purchase did not receive a purchase receipt");
  }
  if (launch?.operationId !== operationId || launch?.workspaceId !== workspaceId) {
    throw purchaseError("deployed_purchase_operation_mismatch", "deployed purchase read an order that is not its own");
  }

  const completed = new Date(clock());
  const completedAt = completed.getTime() < started.getTime() ? startedAt : completed.toISOString();

  return validateDeployedPurchaseReceipt({
    schemaVersion: 1,
    kind: DEPLOYED_PURCHASE_RECEIPT_KIND,
    candidate: { receiptDigest: request.candidateReceiptDigest },
    account: { identityDigest },
    product: { resourceType: "workspace", packageId, autoRenew },
    operation: { idempotencyKey, operationId, workspaceId, purchaseReceiptId, resumed },
    purchase: { amountUsdMicros: admittedUsdMicros(launch) },
    mutations: { declared: { purchases: 1 }, observed: { purchases: resumed ? 0 : 1 } },
    startedAt,
    completedAt,
  });
}

/*
 * The receipt is closed to exactly these keys, which is what keeps a Session, a
 * balance, a raw account identifier or an access URL out of it: there is no
 * field for one. `resumed` and the observed purchase count are tied in both
 * directions, so a delivery cannot claim to have bought nothing while reporting
 * a new order, or to have bought once while calling itself a replay.
 */
export function validateDeployedPurchaseReceipt(value) {
  if (!hasExactKeys(value, RECEIPT_KEYS) || value.schemaVersion !== 1 ||
      value.kind !== DEPLOYED_PURCHASE_RECEIPT_KIND ||
      !hasExactKeys(value.candidate, CANDIDATE_KEYS) || !DIGEST.test(String(value.candidate.receiptDigest || "")) ||
      !hasExactKeys(value.account, ACCOUNT_KEYS) || !DIGEST.test(String(value.account.identityDigest || "")) ||
      !hasExactKeys(value.product, PRODUCT_KEYS) || value.product.resourceType !== "workspace" ||
      !PACKAGE_ID.test(String(value.product.packageId || "")) || typeof value.product.autoRenew !== "boolean" ||
      !hasExactKeys(value.operation, OPERATION_KEYS) ||
      !IDEMPOTENCY_KEY.test(String(value.operation.idempotencyKey || "")) ||
      !String(value.operation.idempotencyKey || "").startsWith(
        `deployed-workspace:${String(value.candidate.receiptDigest || "").slice("sha256:".length)}:`
      ) ||
      !OPERATION_ID.test(String(value.operation.operationId || "")) ||
      !WORKSPACE_ID.test(String(value.operation.workspaceId || "")) ||
      !String(value.operation.purchaseReceiptId || "").trim() || typeof value.operation.resumed !== "boolean" ||
      !hasExactKeys(value.purchase, PURCHASE_KEYS) || !POSITIVE_MICROS.test(String(value.purchase.amountUsdMicros || "")) ||
      !hasExactKeys(value.mutations, MUTATION_KEYS) ||
      !hasExactKeys(value.mutations.declared, PURCHASE_MUTATION_KEYS) ||
      !hasExactKeys(value.mutations.observed, PURCHASE_MUTATION_KEYS) ||
      value.mutations.declared.purchases !== 1 ||
      ![0, 1].includes(value.mutations.observed.purchases) ||
      value.mutations.observed.purchases > value.mutations.declared.purchases ||
      value.operation.resumed !== (value.mutations.observed.purchases === 0) ||
      typeof value.startedAt !== "string" || !Number.isFinite(Date.parse(value.startedAt)) ||
      typeof value.completedAt !== "string" || !Number.isFinite(Date.parse(value.completedAt)) ||
      Date.parse(value.completedAt) < Date.parse(value.startedAt)) {
    throw purchaseError("deployed_purchase_receipt_invalid", "deployed purchase receipt is invalid");
  }
  return value;
}
