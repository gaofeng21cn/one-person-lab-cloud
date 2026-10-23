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
 * deployment, the declaration it resolves, the replay readback, the declared
 * inputs it may act on, and the receipt its caller binds its own evidence to.
 *
 * The Workspace owner is the only writer for a new purchase. It mints the
 * operation identity and the Workspace identity, it prices the purchase, and it
 * keeps the idempotency record that decides whether a repeated command is a
 * replay of the same order or a second one. Nothing here derives either
 * identity any more, because a buyer that derived one would be reproducing an
 * interop contract the owner no longer offers.
 *
 * The retained Control Plane launch route is no longer a purchase command. It
 * is still read, for the orders it admitted before the cutover, because the
 * owner cannot resume an order it never issued and an owner command for a
 * purchase that route already admitted would be a second purchase.
 *
 * It buys and it does nothing else. It does not delete, does not refund, does
 * not provision an account, does not touch a database and does not assert that
 * the Workspace is good. Those belong to the owners of those facts.
 */

import { createHash, randomUUID } from "node:crypto";

import { sourceData } from "./local-workspace-qualification.ts";
import {
  announceRequest,
  assertWalletFundsQuote,
  findDeclaredCatalogEntry,
  listOwnerCatalogPages,
  normalizedAccountEmail,
  quoteAmountUsdMicros,
  quoteWorkspaceOrder,
  stableID,
  submitWorkspaceOrder,
  workspaceDeclaration,
} from "./workspace-purchase-core.ts";

const WORKSPACE_NAME = /^[\x20-\x7e]{1,120}$/;
const IDEMPOTENCY_IDENTITY = /^[A-Za-z0-9._:-]{1,128}$/;
const IDEMPOTENCY_KEY = /^deployed-workspace:[0-9a-f]{64}:[A-Za-z0-9._:-]{1,128}$/;
const DIGEST = /^sha256:[a-f0-9]{64}$/;
const WALLET_MICROS = /^(?:0|[1-9][0-9]*)$/;
const POSITIVE_MICROS = /^[1-9][0-9]*$/;
// An identity the owner minted. The public contract publishes these as strings
// and says nothing about their shape, so a buyer may not pin one: it may only
// insist that the owner named an identity at all. The binding that makes an
// identity trustworthy is the one the owner itself published beside it — the
// operation the order admitted, the plan the quote froze — not a shape the
// buyer guessed.
const OPAQUE_ID = /^\S(?:.*\S)?$/;
// A declared identity. The declaration is this buyer's own input, so it is
// held to one token: it must not carry a path, a separator or a traversal into
// anything that later treats it as one. The identities the owner mints are
// opaque and are not held to this.
const DECLARED_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/;
// The retained Control Plane launch identity, kept so the orders that route
// admitted before the cutover can still be addressed. This one is the buyer's
// own derivation, not the owner's, so its shape is the buyer's to keep.
const RETAINED_OPERATION_ID = /^workspace-launch-[0-9a-f]{18}$/;

const TARGET_ERROR = "deployed purchase target is not a deployed production target";

// The receipt kind names the purchase, not the qualification. It joins the
// protected flow's business receipt only through the Candidate digest and the
// platform-minted operation identities; it does not claim the qualification.
export const DEPLOYED_PURCHASE_RECEIPT_KIND = "opl.deployed-workspace-purchase.v1";

// Every declared input is named, and nothing else is accepted. A request that
// carried a credential, a client price or a storage size would either be a
// secret surface or a second pricing authority, so an unknown key is refused
// rather than silently ignored.
//
// The owner route has no package concept. It prices a compute plan, a storage
// plan and exactly one application source, so the declaration names those
// three. A `packageId` here would be declared and then select nothing, and a
// receipt that carried one beside the resolved plan pair would read as a
// promise the purchase did not make.
const REQUEST_KEYS = [
  "baseURL",
  "authorityClass",
  "candidateReceiptDigest",
  "accountEmailIdentityDigest",
  "applicationSelection",
  "computePlanId",
  "storagePlanId",
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
// The product names what was actually bought: the application source the owner
// priced beside the plan pair it priced, under the renewal mode it admits.
const PRODUCT_KEYS = ["resourceType", "applicationSelection", "computePlanId", "storagePlanId", "renewalMode"];
const APPLICATION_SELECTION_KEYS = ["kind", "runtimeVersionId"];
const OPERATION_KEYS = ["idempotencyKey", "operationId", "workspaceId", "resumed"];
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
const catalogUnavailable = () => purchaseError(
  "deployed_purchase_catalog_unavailable",
  "deployed purchase could not resolve the owner catalog into a declaration",
);
const quoteUnavailable = () => purchaseError(
  "deployed_purchase_quote_unavailable",
  "deployed purchase could not price the declared Workspace",
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
 * purchase that silently defaulted an application source, a plan pair, a renewal
 * choice or an identity would be buying something the dispatch never named.
 *
 * The declared selection is the one the owner route prices. It names exactly one
 * application source beside the compute and storage plan, and the owner admits a
 * deploy quote only for that shape. A declaration that names neither a runtime
 * release nor a capability version, or names both, is not a Workspace order.
 */
export function assertDeployedPurchaseRequest(request) {
  assertDeployedPurchaseTarget(request);
  const selection = request?.applicationSelection;
  if (!hasExactKeys(selection, APPLICATION_SELECTION_KEYS) ||
      selection.kind !== "opl_app" ||
      !DECLARED_ID.test(String(selection.runtimeVersionId || ""))) {
    throw targetError();
  }
  if (!hasExactKeys(request, REQUEST_KEYS) ||
      !DIGEST.test(String(request?.candidateReceiptDigest || "")) ||
      !DIGEST.test(String(request?.accountEmailIdentityDigest || "")) ||
      !DECLARED_ID.test(String(request?.computePlanId || "")) ||
      !DECLARED_ID.test(String(request?.storagePlanId || "")) ||
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
  const normalized = normalizedAccountEmail(email);
  if (!normalized) throw accountUnavailable();
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
    applicationSelection: {
      kind: "opl_app",
      runtimeVersionId: String(environment.OPL_DEPLOYED_PURCHASE_RUNTIME_VERSION_ID || ""),
    },
    computePlanId: String(environment.OPL_DEPLOYED_PURCHASE_COMPUTE_PLAN_ID || ""),
    storagePlanId: String(environment.OPL_DEPLOYED_PURCHASE_STORAGE_PLAN_ID || ""),
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
 * status the owner answered with, and the purchase sequence is the purchase
 * core's. The optional `now` clock exists so a test can force a backward step;
 * a real caller omits it and the entry reads the wall clock.
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

/*
 * The declaration this purchase is priced against, checked against the catalog.
 *
 * The owner prices a plan pair beside an application source and knows nothing
 * about a package, so the declaration names exactly what the owner prices. The
 * catalog is read to confirm that the declared selection is one the owner still
 * offers: an approved Runtime Release for the default OPL App, and an available
 * compute and storage plan.
 *
 * The catalog is not searched for a default. A buyer that took the first
 * approved release and the first available plans would buy something nobody
 * chose, and two runs of the same declared purchase could buy two different
 * things. A declared plan the catalog no longer offers is refused here instead
 * of being silently replaced by another one.
 *
 * The catalog pages by cursor, so every page is read. A cursor that repeats
 * itself would page forever, so it is refused rather than followed.
 */
async function listOwnerCatalog(http, auth, path) {
  try {
    return await listOwnerCatalogPages(http, auth, path);
  } catch {
    throw catalogUnavailable();
  }
}

/*
 * The one catalog entry the declaration names.
 *
 * A declared plan the catalog no longer offers is refused here instead of being
 * silently replaced by another one.
 */
function declaredCatalogEntry(items, id, available, kind) {
  const entry = findDeclaredCatalogEntry(items, id);
  if (!entry) {
    throw purchaseError(
      "deployed_purchase_selection_unavailable",
      `deployed purchase declared a ${kind} the owner catalog does not offer`,
    );
  }
  if (!available(entry)) {
    throw purchaseError(
      "deployed_purchase_selection_unavailable",
      `deployed purchase declared a ${kind} the owner catalog does not currently offer`,
    );
  }
  return entry;
}

/*
 * The declaration this purchase is priced against.
 *
 * The default OPL App is the application source the deployed buyer buys: it is
 * the one source every customer account can be served, and it names an
 * approved Runtime Release rather than a CapabilityVersion that a built Agent
 * would have to publish first.
 */
export async function resolveWorkspaceDeclaration(http, auth, declared) {
  const runtimeVersionId = String(declared?.applicationSelection?.runtimeVersionId || "");
  const computePlanId = String(declared?.computePlanId || "");
  const storagePlanId = String(declared?.storagePlanId || "");
  if (!DECLARED_ID.test(runtimeVersionId) || !DECLARED_ID.test(computePlanId) || !DECLARED_ID.test(storagePlanId)) {
    throw catalogUnavailable();
  }
  const [runtimeVersions, computePlans, storagePlans] = await Promise.all([
    listOwnerCatalog(http, auth, "/api/v2/catalog/runtime-versions"),
    listOwnerCatalog(http, auth, "/api/v2/catalog/compute-plans"),
    listOwnerCatalog(http, auth, "/api/v2/catalog/storage-plans"),
  ]);
  declaredCatalogEntry(runtimeVersions, runtimeVersionId, (item) => item?.status === "approved", "runtime release");
  declaredCatalogEntry(computePlans, computePlanId, (item) => item?.availability === "available", "compute plan");
  declaredCatalogEntry(storagePlans, storagePlanId, (item) => item?.availability === "available", "storage plan");
  return workspaceDeclaration({
    applicationSelection: { kind: "opl_app", runtimeVersionId },
    computePlanId,
    storagePlanId,
    modelSelections: [],
    periodMonths: 1,
  });
}

/*
 * The live owner session, and the customer it belongs to.
 *
 * The owner route authorizes every command against a CloudIdentity session, and
 * the identity owner publishes which actor that session belongs to. The actor is
 * the Gateway user id and the display name is the Gateway email, so they are the
 * same two facts the Control Plane session answered with. A buyer that could not
 * show the two to be one customer would be buying for an account it never
 * checked.
 */
async function readOwnerSession(http, auth) {
  let session;
  try {
    session = (await http.json("/api/v2/auth/session", {}, auth)).payload;
  } catch {
    throw accountUnavailable();
  }
  const actorId = String(session?.actorId || "");
  const displayName = String(session?.displayName || "");
  if (!actorId || !displayName) throw accountUnavailable();
  return { actorId, displayName, tenantId: String(session?.tenantId || "") };
}

/*
 * The wallet, read from the owner that holds it.
 *
 * The Gateway owner holds the balance, and the owner route reads it under the
 * session's own authorization, so the balance comes from the authority that
 * would charge it. The retained Control Plane projection answers for a
 * different authority and a different session, and a purchase funded against it
 * would be checking a balance that is not the one the owner spends.
 */
async function readOwnerWallet(http, auth) {
  let wallet;
  try {
    wallet = (await http.json("/api/v2/wallet", {}, auth)).payload;
  } catch {
    throw walletUnavailable();
  }
  if (wallet?.source !== "gateway" || wallet?.status !== "available" || wallet?.currency !== "USD") {
    throw walletUnavailable();
  }
  const balanceUsdMicros = String(wallet?.balanceUSDMicros || "");
  if (!WALLET_MICROS.test(balanceUsdMicros)) throw walletUnavailable();
  return balanceUsdMicros;
}

/*
 * Whether the owner has already admitted an order for this quote.
 *
 * The owner mints the operation identity, so a buyer cannot address its own
 * order by a derived identity any more: the only way back to it is the owner's
 * own idempotency record, and that record is consulted by the order command
 * itself.
 *
 * The quote is read because the owner publishes the state of the offer the
 * command buys, and a state the owner does not publish is a state this buyer
 * cannot act on. What the quote does not decide is whether this delivery made
 * the purchase: a quote is accepted after the order it was priced for was
 * admitted, so an order can exist while its quote still reads as offered.
 */
async function quoteState(http, auth, quoteId) {
  let payload;
  try {
    payload = (await http.json(`/api/v2/quotes/${encodeURIComponent(quoteId)}`, {}, auth)).payload;
  } catch {
    throw quoteUnavailable();
  }
  const status = String(payload?.status || "");
  if (!["offered", "accepted", "expired"].includes(status)) {
    throw purchaseError(
      "deployed_purchase_quote_unavailable",
      `deployed purchase quote reported an unusable state ${status || "none"}`,
    );
  }
  return status;
}

/*
 * The retained Control Plane launch read.
 *
 * The retained route is no longer a purchase command, but the orders it
 * admitted before the cutover are still there and still belong to the account
 * that bought them. The owner keeps its own idempotency record and cannot
 * resume an order it never issued, so an owner command for a declared purchase
 * that route already admitted would be a second purchase rather than a replay.
 * The buyer reads the retained order and refuses instead.
 *
 * The retained identity is still derived the way the Control Plane derived it,
 * because that is the only way to address an order that route admitted.
 */
function retainedLaunchOperationId(accountId, idempotencyKey) {
  return `workspace-launch-${stableID(accountId, idempotencyKey).slice(0, 18)}`;
}

async function readRetainedLaunch(http, auth, operationId) {
  let result;
  try {
    result = await http.request(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth);
  } catch {
    throw purchaseError(
      "deployed_purchase_launch_readback_unavailable",
      "deployed purchase could not read the retained launch",
    );
  }
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200) {
    throw purchaseError(
      "deployed_purchase_launch_readback_unavailable",
      "deployed purchase could not read the retained launch",
    );
  }
  const launch = result?.payload;
  if (!launch || !RETAINED_OPERATION_ID.test(String(launch.operationId || "")) ||
      launch.operationId !== operationId) {
    throw purchaseError("deployed_purchase_operation_mismatch", "deployed purchase read a retained order that is not its own");
  }
  return launch;
}

/*
 * The declared customer, proved by both authorities that speak for it.
 *
 * The Control Plane session answers with the Gateway identity it authenticated
 * against, so the account id, the role, the status and the email are the
 * platform's own facts. The account id is what the retained launch identity is
 * derived from, so it has to be the platform's and not the buyer's.
 *
 * The owner route is authorized by a CloudIdentity session, and the identity
 * owner publishes which customer that session belongs to. Its actor is the
 * Gateway user id and its display name is the Gateway email, so they are the
 * same two facts the Control Plane session answered with. Showing them to be
 * one customer is what makes the owner route's authorization this purchase's
 * own, and it is the platform's own identifiers that show it.
 */
export async function readDeclaredCustomer(http, auth, request) {
  const me = await readOwnerEnvelope(http, auth, "/api/auth/me", "sub2api", accountUnavailable);
  const accountId = String(me?.accountId || "");
  if (!accountId || me?.role !== "owner" || me?.status !== "active") throw accountUnavailable();
  const identityDigest = deployedAccountIdentityDigest(me?.email);
  if (identityDigest !== request.accountEmailIdentityDigest) {
    throw purchaseError("deployed_purchase_account_mismatch", "deployed purchase account digest does not match the declared customer");
  }
  const ownerSession = await readOwnerSession(http, auth);
  if (ownerSession.actorId !== String(me?.sub2apiUserId || "") ||
      deployedAccountIdentityDigest(ownerSession.displayName) !== identityDigest) {
    throw purchaseError(
      "deployed_purchase_account_mismatch",
      "deployed purchase owner session does not belong to the declared customer",
    );
  }
  return { accountId, identityDigest, ownerSession };
}

/*
 * Buy one Workspace for the declared customer and return the receipt the caller
 * may bind its own evidence to.
 *
 * The owner prices the declared purchase and the buyer only checks that the
 * price names what it declared and that the wallet can fund it. The order is
 * then issued once, under the buyer's own stable idempotency identity, so a
 * repeated delivery is the owner's replay of the same order rather than a
 * second purchase.
 *
 * A command whose answer was lost is issued again with the same identity and
 * the same request. That is the owner's own replay, so it returns the original
 * order and never a second purchase; it is not a resume either, because this
 * delivery issued the command, so whatever it caused is this delivery's to
 * report.
 *
 * The returned receipt carries no credential, session, balance, raw account
 * identifier or access URL. It carries the Candidate digest, the account digest,
 * the platform-minted operation and Workspace identities, the resolved
 * application source and plan pair, the admitted price and the bounded
 * mutation count.
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

  // The owner admits manual renewal only for a first deployment. Automatic
  // renewal is a different contract than the one this route offers, so it is
  // refused loudly here instead of being bought as a manual renewal that ends
  // at the first period.
  if (request.autoRenew === true) {
    throw purchaseError(
      "deployed_purchase_automatic_renewal_unsupported",
      "deployed purchase cannot buy automatic renewal on the Workspace owner route",
    );
  }

  // The platform key is scoped to the Candidate as well as the declared
  // identity. A replay for a different Candidate therefore addresses a
  // different order, so a receipt cannot bind one Candidate to an order
  // admitted for another. The quote and the order carry the same key: they are
  // two owners with two idempotency records, and both have to name the same
  // declared purchase for a replay to be a replay.
  const idempotencyKey = deployedPurchaseIdempotencyKey(
    request.candidateReceiptDigest,
    request.idempotencyIdentity,
  );

  // The Control Plane session proves which customer account is speaking, and
  // the owner session proves it is the same customer the owner route will
  // authorize.
  const { accountId, identityDigest } = await readDeclaredCustomer(http, auth, request);

  // A purchase the retained route already admitted belongs to that route. The
  // owner cannot resume an order it never issued, so buying it again here
  // would be a second purchase for one declared purchase.
  const retained = await readRetainedLaunch(http, auth, retainedLaunchOperationId(accountId, idempotencyKey));
  if (retained) {
    throw purchaseError(
      "deployed_purchase_retained_order_exists",
      "deployed purchase found an order the retained Control Plane route already admitted",
    );
  }

  // The declaration names exactly what the owner prices, and the catalog is read
  // to confirm the owner still offers it. The receipt names those resolved
  // identities, so the evidence names what was actually bought.
  const declaration = await resolveWorkspaceDeclaration(http, auth, {
    applicationSelection: request.applicationSelection,
    computePlanId: request.computePlanId,
    storagePlanId: request.storagePlanId,
  });

  // The quote is the owner's own price for the declared purchase. It is
  // recorded against the idempotency identity, so a repeated quote replays
  // this quote instead of pricing a second one.
  let quote;
  try {
    quote = await quoteWorkspaceOrder({ http, auth, declaration, idempotencyKey });
  } catch {
    throw quoteUnavailable();
  }

  // The quote's own state says whether the owner can still act on this offer,
  // and whether an order has already taken it.
  const quoteStatus = await quoteState(http, auth, quote.id);

  // The quote has to name the declared thing whether or not money is about to
  // move. A replay reports the price its order was admitted at, and that price
  // is only this purchase's price if it names the same thing.
  const amountUsdMicros = quoteAmountUsdMicros({ quote, declaration });

  if (quoteStatus !== "accepted") {
    // The money is about to move, so the wallet has to be able to fund it. The
    // balance is read from the Gateway owner under the owner session, so it is
    // the balance the owner will spend.
    //
    // An accepted quote is the one case where this check does not run: the
    // order it was priced for was already admitted, the money already moved, and
    // a wallet that has since been spent is not a reason to refuse to report an
    // order that exists. The order below is still the owner's own replay, so
    // nothing is bought a second time.
    const walletUsdMicros = await readOwnerWallet(http, auth);
    assertWalletFundsQuote(walletUsdMicros, amountUsdMicros);
  }

  // The request identity this delivery announces. It is fresh for this
  // delivery and stable across this delivery's attempts, so a command whose
  // answer was lost is recovered by the same request rather than by a second
  // one.
  const requestId = randomUUID();
  const transport = announceRequest(http, requestId);

  let order;
  try {
    order = await submitWorkspaceOrder({
      http: transport, auth, name: request.workspaceName, quote, idempotencyKey, wait,
    });
  } catch (error) {
    // The command answer may have been lost with the order admitted behind it.
    // That is not a second purchase: the owner records the command against the
    // idempotency identity, so the same command is the owner's own replay of
    // that order.
    let recovered;
    try {
      recovered = await submitWorkspaceOrder({
        http: transport, auth, name: request.workspaceName, quote, idempotencyKey, wait,
      });
    } catch (readbackError) {
      // Both the command and its replay failed. Keep the command error as the
      // cause so the caller can tell a lost response from a refused or
      // malformed one when it maps the terminal reason.
      if (readbackError && typeof readbackError === "object") readbackError.cause = error;
      throw readbackError;
    }
    order = recovered;
  }

  // Whether this delivery bought the order or found one an earlier delivery
  // already bought is the owner's own answer. The owner stores the request that
  // admitted an order on the operation it mints and publishes that request, so
  // an operation naming this delivery's request is this delivery's order and an
  // operation naming another request was admitted earlier.
  //
  // The quote's state is not that answer. A quote is accepted after the order
  // it was priced for was admitted, so an order can exist while its quote still
  // reads as offered, and a buyer that read the quote would claim a purchase it
  // did not make.
  const admittedRequestId = String(order?.order?.requestId || "");
  if (!admittedRequestId) {
    throw purchaseError(
      "deployed_purchase_operation_mismatch",
      "deployed purchase read an order that did not name the request that admitted it",
    );
  }
  const resumed = admittedRequestId !== requestId;

  const operationId = String(order?.operationId || "");
  const workspaceId = String(order?.workspaceId || "");
  if (!OPAQUE_ID.test(operationId) || !OPAQUE_ID.test(workspaceId)) {
    throw purchaseError("deployed_purchase_operation_mismatch", "deployed purchase read an order that is not its own");
  }

  const completed = new Date(clock());
  const completedAt = completed.getTime() < started.getTime() ? startedAt : completed.toISOString();

  /*
   * The receipt names what was actually bought, and that is the owner-resolved
   * application source and plan pair, not the declared package.
   *
   * The declared `packageId` is a Sub2API wallet package. The Workspace owner
   * route has no package concept: it prices a compute plan, a storage plan and
   * one application source, and it publishes the plan pair. So the declared
   * package does not select anything here, and it is deliberately not written
   * into the receipt either, because a receipt that showed a declared package
   * beside a resolved plan pair would read as a promise the purchase did not
   * make. The operator's control on this route is the exact amount they
   * authorise against the owner's frozen quote.
   *
   * The declared package stays on the request because it is part of this
   * tool's published request contract, and it is still validated, so a caller
   * that sends a malformed one is refused rather than silently accepted.
   */

  return validateDeployedPurchaseReceipt({
    schemaVersion: 1,
    kind: DEPLOYED_PURCHASE_RECEIPT_KIND,
    candidate: { receiptDigest: request.candidateReceiptDigest },
    account: { identityDigest },
    product: {
      resourceType: "workspace",
      applicationSelection: declaration.applicationSelection,
      computePlanId: declaration.computePlanId,
      storagePlanId: declaration.storagePlanId,
      renewalMode: "manual",
    },
    operation: { idempotencyKey, operationId, workspaceId, resumed },
    purchase: { amountUsdMicros },
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
      !hasExactKeys(value.product.applicationSelection, APPLICATION_SELECTION_KEYS) ||
      value.product.applicationSelection.kind !== "opl_app" ||
      !DECLARED_ID.test(String(value.product.applicationSelection.runtimeVersionId || "")) ||
      !DECLARED_ID.test(String(value.product.computePlanId || "")) ||
      !DECLARED_ID.test(String(value.product.storagePlanId || "")) ||
      value.product.renewalMode !== "manual" ||
      !hasExactKeys(value.operation, OPERATION_KEYS) ||
      !IDEMPOTENCY_KEY.test(String(value.operation.idempotencyKey || "")) ||
      !String(value.operation.idempotencyKey || "").startsWith(
        `deployed-workspace:${String(value.candidate.receiptDigest || "").slice("sha256:".length)}:`
      ) ||
      !OPAQUE_ID.test(String(value.operation.operationId || "")) ||
      !OPAQUE_ID.test(String(value.operation.workspaceId || "")) ||
      typeof value.operation.resumed !== "boolean" ||
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
