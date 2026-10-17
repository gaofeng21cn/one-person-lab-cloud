// The provider-neutral qualification contract.
//
// Before this module, provider identity was asserted as a literal inside the
// J0 validator (`value.kind !== "opl.local-workspace.j0-ready.v1"` and
// `value.provider !== "local-docker"`), so no provider other than Local-Docker
// could ever produce a qualifying receipt and the schema itself named one
// provider. A Tencent-TKE adapter therefore had no seam to reuse and would have
// had to copy the validator.
//
// Here the provider is declared by the caller as data. The contract knows the
// shape of a qualification receipt and nothing about which provider runs it.
//
// The second half of the seam is the request side: what a business
// qualification is allowed to act on. A receipt validator alone gives two
// adapters one way to agree on a result, but not one way to ask for it. The
// declarations below are that request: the exact Candidate, the tenant it runs
// as, the product it purchases, the provider that hosts it and the mutations it
// is allowed to cause. Nothing here reaches a provider, a cluster or a database.

import { createHash } from "node:crypto";

import { canonicalJson, candidateReceiptDigest, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";

// Named for the qualification, not for any provider.
export const QUALIFICATION_J0_READY_KIND = "opl.workspace-qualification.j0-ready.v2";

// The J0 producer lives outside this repository and still emits the old,
// provider-named kind. It stays acceptable to any profile that declares it, so
// moving the in-repo schema to the neutral kind breaks no live path.
export const LEGACY_QUALIFICATION_J0_READY_KINDS = ["opl.local-workspace.j0-ready.v1"];

const J0_READY_KEYS = ["schemaVersion", "kind", "status", "source", "authority", "provider", "failed", "skipped"];

export function hasExactKeys(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

export function j0ReadyReceiptError() {
  const error = new Error("j0_ready_receipt_invalid");
  error.code = "j0_ready_receipt_invalid";
  return error;
}

export function qualificationProviderProfile(provider, { legacyReceiptKinds = [] } = {}) {
  const name = String(provider || "").trim();
  if (!name) throw new Error("qualification provider profile requires a provider identity");
  return Object.freeze({
    provider: name,
    receiptKinds: Object.freeze([QUALIFICATION_J0_READY_KIND, ...legacyReceiptKinds])
  });
}

export function createJ0ReadyReceiptValidator(profile) {
  const { provider, receiptKinds } = qualificationProviderProfile(profile?.provider, {
    legacyReceiptKinds: (profile?.receiptKinds || []).filter((kind) => kind !== QUALIFICATION_J0_READY_KIND)
  });
  return function validateJ0ReadyReceiptForProfile(value, sourceSha, sourceTree) {
    if (!hasExactKeys(value, J0_READY_KEYS) ||
      value.schemaVersion !== 1 || !receiptKinds.includes(value.kind) || value.status !== "READY" ||
      !hasExactKeys(value.source, ["sha", "tree", "clean"]) ||
      value.source?.sha !== sourceSha || value.source?.tree !== sourceTree || value.source?.clean !== true ||
      value.failed !== 0 || value.skipped !== 0 || value.provider !== provider ||
      !hasExactKeys(value.authority, ["mode", "class", "dedicated", "confirmed"]) ||
      value.authority?.mode !== "live" || value.authority?.class !== "sandbox" ||
      value.authority?.dedicated !== true || value.authority?.confirmed !== true) {
      throw j0ReadyReceiptError();
    }
    return value;
  };
}

/*
 * What a business qualification is allowed to act on, and what it may claim.
 *
 * The orchestrator needs a request it cannot be talked out of, and a receipt
 * whose terminal state means one thing only. Both are declared here, so a
 * provider adapter supplies a transport and nothing else, and two providers
 * cannot come back with two different meanings of the same result.
 */

// Named for the qualification step it records, which is the Workspace
// purchase. It is not the business qualification receipt and does not claim to
// be: it joins the Instance deployment receipt only through the immutable
// Candidate identity, and it carries the scope it did not establish beside the
// scope it did.
export const QUALIFICATION_BUSINESS_RECEIPT_KIND = "opl.workspace-qualification.purchase.v1";

// The one scope this flow establishes, and therefore the only scope value a
// receipt of this kind may publish. It is named once so the orchestrator and the
// validator cannot drift apart.
export const QUALIFICATION_BUSINESS_SCOPE = "workspace_purchase";

// The deployment health values an adapter may report. They are recorded beside
// the business result and never allowed to move it.
export const DEPLOYMENT_HEALTH = Object.freeze(["healthy", "degraded", "unknown"]);

// The terminal result has to say which of these it is. `unknown` and
// `recovery-required` are not softer forms of `failed`: the first means the run
// could not determine the outcome, the second means it changed something and
// cannot prove it put the scope back or place it. Collapsing any two of them
// would let an unproven run be read as a settled one.

// Every state names exactly the reasons it may be reached by, so a receipt
// cannot carry one outcome's evidence under another outcome's name.
//
// The passing state is `settled`, not `ready`. This flow proves the Workspace
// purchase: the declared product was priced by the platform, bought once, paid
// once against a Ledger receipt bound to that exact operation, and delivered a
// ready Workspace with a running Runtime behind the owner's own readbacks. It
// does not install the application, issue the Workspace Gateway Key, exercise
// that Key, or delete the Workspace. A verdict called `ready` on that evidence
// would read as a qualified customer journey, and no reader of the receipt
// could tell it apart from one. `settled` says what was actually established.
export const QUALIFICATION_TERMINAL_REASONS = Object.freeze({
  settled: Object.freeze(["none"]),
  blocked: Object.freeze([
    "provider_scope_unavailable", "wallet_cannot_fund", "scope_bound_exhausted"
  ]),
  failed: Object.freeze([
    "quote_invalid", "launch_stopped", "account_binding_mismatch", "launch_identity_mismatch",
    "workspace_not_ready", "runtime_not_ready",
    "charge_receipt_mismatch", "wallet_readback_mismatch", "purchase_receipt_missing"
  ]),
  unknown: Object.freeze(["response_lost", "owner_readback_unavailable"]),
  "recovery-required": Object.freeze(["mutation_unaccounted", "cleanup_unproven"])
});

// The terminal states are exactly the states the reasons above define. Keeping
// one list derived from the other means a state cannot be added to one and
// forgotten in the other.
export const QUALIFICATION_TERMINAL_STATES = Object.freeze(Object.keys(QUALIFICATION_TERMINAL_REASONS));

// A failed state has to name a real check or read failure. There is deliberately
// no "evidence was incomplete" failure: if every read succeeded the contract,
// not the orchestrator, decides whether the evidence is settled.

// What a settled receipt must have read, from the owner APIs, before it may say
// so. Each is a fact about the declared account's own purchase, not about the
// host that carried it.
//
// A wallet amount is deliberately not on this list. A balance that moved is not
// evidence of what moved it: it can fall for a refund, an adjustment or an
// unrelated charge. The charge is proven by the Ledger receipt that names this
// operation, this Workspace and this amount, and the wallet is then used only to
// confirm that receipt against the balance.
//
// Deployment health is deliberately not on this list either. A product can be
// settled while the host carrying it is degraded, and a healthy host proves
// nothing about the product.
export const QUALIFICATION_BUSINESS_CHECKS = Object.freeze([
  "account_bound", "quote_priced", "purchase_issued", "purchase_receipt_bound",
  "workspace_ready", "runtime_ready", "charge_receipted", "wallet_confirmed"
]);

// What this flow does not establish, stated on every receipt it returns. These
// are not gaps in the evidence above; they are separate steps this flow never
// performs, and a reader must not infer them from a settled receipt.
export const QUALIFICATION_UNCOVERED_SCOPE = Object.freeze({
  applicationInstallation: false,
  gatewayKeyLifecycle: false,
  keyUsage: false,
  ownerDeletion: false,
  businessQualification: false
});

// What a run may cause. Each bound is declared before anything happens, and a
// run that caused more than it declared cannot come back settled.
export const QUALIFICATION_MUTATION_BOUNDS = Object.freeze([
  "purchases", "charges", "refunds", "workspaceDeletions", "managedKeys"
]);

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const shaPattern = /^[0-9a-f]{40}$/;
const microsPattern = /^(?:0|[1-9][0-9]*)$/;

function qualificationContractError(code) {
  const error = new Error(code);
  error.code = code;
  return error;
}

const digestOf = (value) => `sha256:${createHash("sha256").update(value).digest("hex")}`;
const isDigest = (value) => digestPattern.test(String(value || ""));

export function qualificationMutationScope(declared = {}) {
  const scope = {};
  for (const name of QUALIFICATION_MUTATION_BOUNDS) {
    const bound = declared[name];
    if (!Number.isSafeInteger(bound) || bound < 0) throw qualificationContractError("qualification_scope_invalid");
    scope[name] = bound;
  }
  return Object.freeze(scope);
}

/*
 * The tenant a qualification acts for, in two forms.
 *
 * The account id is what the platform is asked to act on, so it travels in the
 * request. The receipt keeps only the digest, and that digest is scoped to the
 * Candidate: the same customer qualifying two Candidates produces two unrelated
 * digests, so a receipt cannot be used to follow one customer across Candidates.
 *
 * The digest is a stable, Candidate-scoped pseudonym, not an anonymization: a
 * holder of the Candidate digest can confirm a guessed low-entropy account id by
 * recomputing it. That is acceptable for this receipt, which is read by the same
 * owner, and HMAC-ing the digest belongs to the protected qualification entry
 * rather than here (tracked as a follow-up).
 */
export function qualificationTenantScope(accountId, candidateReceiptDigest) {
  const account = String(accountId || "").trim();
  if (!account || account.length > 256 || /[\s\u0000-\u001f\u007f]/.test(account) || !isDigest(candidateReceiptDigest)) {
    throw qualificationContractError("qualification_tenant_invalid");
  }
  return Object.freeze({
    accountId: account,
    accountDigest: digestOf(`${candidateReceiptDigest}\n${account}`)
  });
}

/*
 * The product combination this run purchases. The platform prices the launch and
 * refuses a client that supplies a size or a price, so the declaration carries
 * only what the platform itself takes as input.
 */
export function qualificationProduct({ resourceType = "workspace", packageId = "basic", autoRenew = false } = {}) {
  const resource = String(resourceType || "").trim();
  const product = String(packageId || "").trim();
  if (resource !== "workspace" || !product || product.length > 64 || !/^[a-z0-9][a-z0-9-]*$/.test(product)) {
    throw qualificationContractError("qualification_product_invalid");
  }
  if (typeof autoRenew !== "boolean") throw qualificationContractError("qualification_product_invalid");
  // This qualification buys one product once. Auto-renew would commit the
  // customer to a later charge this run never observes, so it is refused here
  // rather than purchased and left out of the accounting.
  if (autoRenew) throw qualificationContractError("qualification_product_invalid");
  return Object.freeze({ resourceType: resource, packageId: product, autoRenew });
}

// The authority classes a live qualification may declare. The J0 validator in
// this repository currently confirms only the sandbox class, so a preproduction
// request has no acceptable J0 receipt yet; that is a refusal rather than a
// relabelling, and the comparison below keeps the two in step.
const QUALIFICATION_AUTHORITY_CLASSES = Object.freeze(["sandbox", "preproduction"]);

/*
 * A live business qualification runs under an authority someone else confirmed,
 * and the J0 receipt is that proof. The fixture authority is not a weaker
 * business run, it is a different question, so it is refused here rather than
 * qualified and relabelled later.
 *
 * The declared class has to be the class the J0 receipt itself carries. A
 * preproduction run whose only authority is a sandbox receipt is a different
 * question again, and accepting it would publish a preproduction authority
 * backed by sandbox evidence.
 */
export function qualificationLiveAuthority({ mode, authorityClass, j0ReadyReceipt }, profile, sourceSha, sourceTree) {
  const receipt = createJ0ReadyReceiptValidator(profile)(j0ReadyReceipt, sourceSha, sourceTree);
  const declaredClass = String(authorityClass || "");
  if (mode !== "live" || !QUALIFICATION_AUTHORITY_CLASSES.includes(declaredClass) ||
      declaredClass !== receipt.authority.class) {
    throw qualificationContractError("qualification_authority_invalid");
  }
  return Object.freeze({
    mode: "live",
    authorityClass: declaredClass,
    j0ReadyDigest: digestOf(canonicalJson(receipt))
  });
}

/*
 * The identity a repeated delivery reuses.
 *
 * It is derived from the Candidate, the tenant digest, the product and the
 * provider, and from nothing else. A redelivery of the same qualification
 * therefore addresses the same launch operation the first delivery created, and
 * the Control Plane answers from the order it already admitted instead of
 * admitting a second one. Because it is derived rather than stored, it survives
 * a lost response with no local state: the orchestrator rediscovers the
 * operation from the request alone.
 */
export function qualificationIdempotencyKey({ candidateReceiptDigest, accountDigest, product, provider }) {
  const declared = qualificationProduct(product);
  const name = String(provider || "").trim();
  if (!isDigest(candidateReceiptDigest) || !isDigest(accountDigest) || !name) {
    throw qualificationContractError("qualification_operation_identity_invalid");
  }
  return `opl-qualification:${digestOf(canonicalJson({
    candidateReceiptDigest,
    accountDigest,
    resourceType: declared.resourceType,
    packageId: declared.packageId,
    autoRenew: declared.autoRenew,
    provider: name
  })).slice("sha256:".length)}`;
}

/*
 * The Workspace name a run declares.
 *
 * Derived from the operation identity rather than chosen per provider, so a
 * redelivery sends the same command body, and so the name never says which host
 * happened to run the qualification. A name that named Local-Docker would put a
 * provider into the business command, which is exactly what this seam exists to
 * keep out of it.
 */
export function qualificationWorkspaceName(operationId) {
  const declared = String(operationId || "").trim();
  if (!/^workspace-launch-[0-9a-f]{18}$/.test(declared)) {
    throw qualificationContractError("qualification_operation_identity_invalid");
  }
  return `Qualification ${declared.slice("workspace-launch-".length)}`;
}

/*
 * One request, validated once, before anything is touched.
 *
 * The Candidate is read through the distribution contract's own validator and
 * summarised by its own digest function, so this contract cannot become a
 * second place that decides what a Candidate is. The source identity the run
 * declares has to equal that Candidate's: a qualification that ran against
 * different bytes than it names is not evidence about the Candidate at all.
 *
 * A provider may also declare the legacy, provider-named J0 kinds it still
 * accepts. The declaration is per request so one profile can keep a live
 * producer working without widening every other provider's acceptance.
 */
export function createQualificationRequest({
  candidate, accountId, product, provider, scope, authority, source, legacyReceiptKinds = []
}) {
  const admitted = validateCloudCandidateReceipt(candidate);
  const receiptDigest = candidateReceiptDigest(admitted);
  const product_ = admitted.product;
  const image = admitted.cloudImage;
  if (!hasExactKeys(source, ["sha", "tree", "clean"]) || source.clean !== true ||
      source.sha !== product_.sha || source.tree !== product_.tree) {
    throw qualificationContractError("qualification_source_identity_invalid");
  }
  const tenant = qualificationTenantScope(accountId, receiptDigest);
  const declaredProduct = qualificationProduct(product);
  const legacy = Array.isArray(legacyReceiptKinds)
    ? legacyReceiptKinds.filter((kind) => typeof kind === "string" && kind.trim())
    : [];
  const profile = qualificationProviderProfile(provider, { legacyReceiptKinds: legacy });
  const request = Object.freeze({
    candidate: Object.freeze({
      receiptDigest,
      productSha: product_.sha,
      productTree: product_.tree,
      cloudImageDigest: image.indexDigest
    }),
    source: Object.freeze({ sha: source.sha, tree: source.tree, clean: true }),
    tenant,
    product: declaredProduct,
    provider: profile.provider,
    profile,
    scope: qualificationMutationScope(scope),
    authority: qualificationLiveAuthority(authority, profile, source.sha, source.tree),
    operation: Object.freeze({
      idempotencyKey: qualificationIdempotencyKey({
        candidateReceiptDigest: receiptDigest, accountDigest: tenant.accountDigest, product: declaredProduct, provider: profile.provider
      })
    })
  });
  brandedRequests.add(request);
  return request;
}

// The requests this contract built, and nothing else. The brand is what lets the
// orchestrator accept a request as its admission rather than re-checking a shape
// that a hand-assembled object can imitate: a spread copy has the same fields
// and no brand.
const brandedRequests = new WeakSet();

export function assertQualificationRequest(request) {
  if (!request || typeof request !== "object" || !brandedRequests.has(request)) {
    throw qualificationContractError("qualification_request_invalid");
  }
  return request;
}

const RECEIPT_KEYS = [
  "schemaVersion", "kind", "scope", "uncovered", "state", "reason", "provider", "candidate", "source", "tenant", "product",
  "authority", "operation", "mutations", "checks", "accounting", "delivery", "cleanup", "deploymentHealth", "startedAt", "completedAt"
];
const CANDIDATE_KEYS = ["receiptDigest", "productSha", "productTree", "cloudImageDigest"];
const SOURCE_KEYS = ["sha", "tree", "clean"];
const PRODUCT_KEYS = ["resourceType", "packageId", "autoRenew"];
const AUTHORITY_KEYS = ["mode", "authorityClass", "j0ReadyDigest"];
const OPERATION_KEYS = ["idempotencyKey", "operationId", "workspaceId", "purchaseReceiptId", "resumed"];
const ACCOUNTING_KEYS = ["quoteUsdMicros", "walletBeforeUsdMicros", "walletAfterUsdMicros", "chargeReferenceDigest"];
const DELIVERY_KEYS = ["workspaceEntryDigest", "applicationOperationDigest", "componentsObserved"];
const QUALIFICATION_UNCOVERED_KEYS = Object.freeze(Object.keys(QUALIFICATION_UNCOVERED_SCOPE));
const CLEANUP_KEYS = ["performed", "restored"];

const invalidReceipt = () => { throw qualificationContractError("qualification_business_receipt_invalid"); };
const micros = (value) => (value === null || microsPattern.test(String(value)) ? value : invalidReceipt());
const instant = (value) => (typeof value === "string" && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(value) &&
  Number.isFinite(Date.parse(value)) ? value : invalidReceipt());

/*
 * The receipt a business qualification returns.
 *
 * Every block is closed to exactly the keys listed here, which is what keeps a
 * raw Secret, Key, customer identifier or private provider identifier out of
 * it: there is no field for one. The tenant appears only as its Candidate-scoped
 * digest, the purchase only as platform-minted operation identities, and the
 * managed Key not at all, because this flow issues none.
 *
 * `settled` and "not settled" are separated in both directions. A settled
 * receipt carries the complete business evidence and one of the honest
 * settlement shapes stated at the end of this function: this delivery bought and
 * paid for the order, a redelivery found its own finished order and spent
 * nothing, or a redelivery waited on its open order while the original charge
 * landed. A receipt that is not settled fails at least one of the conditions
 * settlement requires. A run therefore cannot report a failure while holding
 * every piece of evidence a pass needs, and cannot report a pass while
 * withholding one.
 *
 * Deployment health is recorded beside all of this and enters none of it. A
 * healthy host is not a qualified product, and a qualified product does not
 * imply a healthy host, so it can never move the terminal state in either
 * direction.
 */
export function validateBusinessQualificationReceipt(value) {
  if (!hasExactKeys(value, RECEIPT_KEYS) || value.schemaVersion !== 1 ||
      value.kind !== QUALIFICATION_BUSINESS_RECEIPT_KIND ||
      !QUALIFICATION_TERMINAL_STATES.includes(value.state) ||
      !(QUALIFICATION_TERMINAL_REASONS[value.state] || []).includes(value.reason) ||
      typeof value.provider !== "string" || !value.provider.trim() ||
      !hasExactKeys(value.candidate, CANDIDATE_KEYS) ||
      !isDigest(value.candidate.receiptDigest) || !isDigest(value.candidate.cloudImageDigest) ||
      !shaPattern.test(String(value.candidate.productSha || "")) ||
      !shaPattern.test(String(value.candidate.productTree || "")) ||
      !hasExactKeys(value.source, SOURCE_KEYS) || value.source.clean !== true ||
      value.source.sha !== value.candidate.productSha || value.source.tree !== value.candidate.productTree ||
      !hasExactKeys(value.tenant, ["accountDigest"]) || !isDigest(value.tenant.accountDigest) ||
      !hasExactKeys(value.product, PRODUCT_KEYS) || typeof value.product.autoRenew !== "boolean" ||
      !hasExactKeys(value.authority, AUTHORITY_KEYS) || value.authority.mode !== "live" ||
      !QUALIFICATION_AUTHORITY_CLASSES.includes(value.authority.authorityClass) ||
      !isDigest(value.authority.j0ReadyDigest) ||
      !hasExactKeys(value.operation, OPERATION_KEYS) || typeof value.operation.resumed !== "boolean" ||
      !hasExactKeys(value.mutations, ["declared", "observed"]) ||
      value.scope !== QUALIFICATION_BUSINESS_SCOPE ||
      !hasExactKeys(value.uncovered, QUALIFICATION_UNCOVERED_KEYS) ||
      Object.entries(QUALIFICATION_UNCOVERED_SCOPE).some(([name, covered]) => value.uncovered[name] !== covered) ||
      !hasExactKeys(value.checks, QUALIFICATION_BUSINESS_CHECKS) ||
      !hasExactKeys(value.accounting, ACCOUNTING_KEYS) ||
      !hasExactKeys(value.delivery, DELIVERY_KEYS) ||
      !hasExactKeys(value.cleanup, CLEANUP_KEYS) ||
      typeof value.cleanup.performed !== "boolean" || typeof value.cleanup.restored !== "boolean" ||
      !DEPLOYMENT_HEALTH.includes(value.deploymentHealth)) {
    invalidReceipt();
  }

  // The product has to be one this contract itself accepts, and the receipt must
  // carry exactly the normalized declaration. Otherwise a receipt could name a
  // resource type, a package identity or an auto-renew commitment this flow
  // never declares.
  let declaredProduct = null;
  try {
    declaredProduct = qualificationProduct(value.product);
  } catch {
    invalidReceipt();
  }
  if (!declaredProduct ||
      declaredProduct.resourceType !== value.product.resourceType ||
      declaredProduct.packageId !== value.product.packageId ||
      declaredProduct.autoRenew !== value.product.autoRenew) {
    invalidReceipt();
  }

  for (const bounds of [value.mutations.declared, value.mutations.observed]) {
    for (const name of QUALIFICATION_MUTATION_BOUNDS) {
      if (!hasExactKeys(bounds, QUALIFICATION_MUTATION_BOUNDS) || !Number.isSafeInteger(bounds[name]) || bounds[name] < 0) {
        invalidReceipt();
      }
    }
  }
  for (const name of QUALIFICATION_BUSINESS_CHECKS) {
    if (typeof value.checks[name] !== "boolean") invalidReceipt();
  }
  const quote = micros(value.accounting.quoteUsdMicros);
  const before = micros(value.accounting.walletBeforeUsdMicros);
  const after = micros(value.accounting.walletAfterUsdMicros);
  const moved = before !== null && after !== null ? BigInt(before) - BigInt(after) : null;
  // An empty charge reference means the run never reached the Ledger receipt,
  // which is the case for every outcome that stops earlier than that.
  if (value.accounting.chargeReferenceDigest !== "" && !isDigest(value.accounting.chargeReferenceDigest)) invalidReceipt();
  instant(value.startedAt);
  instant(value.completedAt);
  if (Date.parse(value.completedAt) < Date.parse(value.startedAt)) invalidReceipt();

  // A check that claims something happened has to name the thing that proves
  // it. Without this a receipt could hold a true check beside an empty
  // operation, and a receipt is only as strong as its least supported claim.
  if (value.checks.quote_priced && quote === null) invalidReceipt();
  // A purchase claim is about an order existing under this identity, not about
  // this delivery having issued the command, so a resume is allowed to report
  // zero purchases while still holding the order.
  if (value.checks.purchase_issued &&
      (!String(value.operation.operationId || "").trim() || !String(value.operation.workspaceId || "").trim())) {
    invalidReceipt();
  }
  // Once an order is in play the two must agree, in both directions. A run that
  // never issued or resumed one simply reports zero.
  if (value.mutations.observed.purchases > 1) invalidReceipt();
  if (value.checks.purchase_issued && value.operation.resumed && value.mutations.observed.purchases !== 0) invalidReceipt();
  if (value.checks.purchase_issued && !value.operation.resumed && value.mutations.observed.purchases !== 1) invalidReceipt();
  if (value.checks.purchase_receipt_bound && !String(value.operation.purchaseReceiptId || "").trim()) invalidReceipt();
  // A claim about money needs the wallet read on both sides of the run, and a
  // claim about the charge needs the Ledger receipt that names this operation.
  if (value.checks.wallet_confirmed && (before === null || after === null)) invalidReceipt();
  if (value.checks.charge_receipted && value.accounting.chargeReferenceDigest === "") invalidReceipt();
  // The delivery block and the delivery checks are bound to each other in both
  // directions: a receipt that claims a ready Workspace and a running Runtime
  // has to carry the observation that proves it, and one that never got there
  // must not carry a delivery it did not observe.
  const delivered = value.checks.workspace_ready && value.checks.runtime_ready;
  if (delivered
      ? (!isDigest(value.delivery.workspaceEntryDigest) || !isDigest(value.delivery.applicationOperationDigest) ||
         !Number.isSafeInteger(value.delivery.componentsObserved) || value.delivery.componentsObserved < 1)
      : (value.delivery.workspaceEntryDigest !== "" || value.delivery.applicationOperationDigest !== "" ||
         value.delivery.componentsObserved !== 0)) {
    invalidReceipt();
  }

  const withinBounds = QUALIFICATION_MUTATION_BOUNDS.every((name) =>
    value.mutations.observed[name] <= value.mutations.declared[name]);
  const everyCheck = QUALIFICATION_BUSINESS_CHECKS.every((name) => value.checks[name]);
  const cleanupProven = value.cleanup.performed && value.cleanup.restored;
  const observed = value.mutations.observed;
  // A settled purchase is evidenced three honest ways, and the receipt says
  // which. All three require every business check and the full mutation
  // accounting; they differ only in how the money got there.
  //
  // The first is a delivery that issued the command, saw the Ledger receipt for
  // it and watched the wallet move by exactly the quoted amount. The second is
  // a redelivery that found its own finished order, bought nothing again and
  // watched the wallet stand still. `resumed` is true only for the second and
  // the third: a delivery that recovered its own order after a lost answer did
  // issue the command, and cannot claim either shape by skipping the money.
  //
  // The third is a redelivery whose order was still open when it arrived. The
  // earlier delivery admitted the order; this run bought nothing; the original
  // charge landed while it waited; the wallet moved by the quote; and the Ledger
  // receipt is bound to that same operation. Refusing this shape failed a real
  // purchase, so it is stated rather than folded into one of the other two.
  const boughtAndPaid = observed.purchases === 1 && observed.charges === 1 &&
    quote !== null && moved === BigInt(quote);
  const resumedFinishedOrder = value.operation.resumed && observed.purchases === 0 &&
    observed.charges === 0 && moved === 0n;
  const resumedSettledCharge = value.operation.resumed && observed.purchases === 0 &&
    observed.charges === 1 && quote !== null && moved === BigInt(quote);
  const qualified = everyCheck && withinBounds && cleanupProven && observed.refunds === 0 &&
    (boughtAndPaid || resumedFinishedOrder || resumedSettledCharge);
  if ((value.state === "settled") !== qualified) invalidReceipt();
  return value;
}
