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

// Named for the qualification. A business qualification receipt is the Cloud
// product's own evidence; it joins the Instance deployment receipt only through
// the immutable Candidate identity, never by merging the two.
export const QUALIFICATION_BUSINESS_RECEIPT_KIND = "opl.workspace-qualification.business.v1";

// A terminal result has to say which of these it is. `unknown` and
// `recovery-required` are not softer forms of `failed`: the first means the run
// could not determine the outcome, the second means it changed something and
// cannot prove it put the scope back. Collapsing any two of them would let an
// unproven run be read as a passed one.
export const QUALIFICATION_TERMINAL_STATES = Object.freeze([
  "ready", "blocked", "failed", "unknown", "recovery-required"
]);

// Every state names exactly the reasons it may be reached by, so a receipt
// cannot carry one outcome's evidence under another outcome's name.
export const QUALIFICATION_TERMINAL_REASONS = Object.freeze({
  ready: Object.freeze(["none"]),
  blocked: Object.freeze([
    "live_authority_missing", "provider_scope_unavailable", "wallet_cannot_fund", "scope_bound_exhausted"
  ]),
  failed: Object.freeze([
    "quote_invalid", "launch_stopped", "charge_readback_mismatch", "receipt_binding_mismatch", "owner_evidence_incomplete"
  ]),
  unknown: Object.freeze(["response_lost", "owner_readback_unavailable"]),
  "recovery-required": Object.freeze(["mutation_unaccounted", "cleanup_unproven"])
});

// The business checks a ready receipt has to carry. Deployment health is
// deliberately not one of them: a product can qualify while the host carrying
// it is degraded, and a healthy host proves nothing about the product.
export const QUALIFICATION_BUSINESS_CHECKS = Object.freeze([
  "quote_priced", "purchase_issued", "charge_settled", "receipt_bound"
]);

// What a run may cause. Each bound is declared before anything happens, and a
// run that caused more than it declared cannot come back ready.
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
 * Candidate: the same customer qualifying two Candidates produces two
 * unrelated digests, so a receipt cannot be used to follow one customer across
 * Candidates, and it cannot be reversed without the Candidate it names.
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
  return Object.freeze({ resourceType: resource, packageId: product, autoRenew });
}

/*
 * A live business qualification runs under an authority someone else confirmed,
 * and the J0 receipt is that proof. The fixture authority is not a weaker
 * business run, it is a different question, so it is refused here rather than
 * qualified and relabelled later.
 */
export function qualificationLiveAuthority({ mode, authorityClass, j0ReadyReceipt }, profile, sourceSha, sourceTree) {
  const receipt = createJ0ReadyReceiptValidator(profile)(j0ReadyReceipt, sourceSha, sourceTree);
  if (mode !== "live" || !["sandbox", "preproduction"].includes(String(authorityClass || ""))) {
    throw qualificationContractError("qualification_authority_invalid");
  }
  return Object.freeze({
    mode: "live",
    authorityClass: String(authorityClass),
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
 */
export function createQualificationRequest({ candidate, accountId, product, provider, scope, authority, source }) {
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
  const profile = qualificationProviderProfile(provider);
  return Object.freeze({
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
}

const RECEIPT_KEYS = [
  "schemaVersion", "kind", "state", "reason", "provider", "candidate", "source", "tenant", "product",
  "authority", "operation", "scope", "checks", "accounting", "cleanup", "deploymentHealth", "startedAt", "completedAt"
];
const CANDIDATE_KEYS = ["receiptDigest", "productSha", "productTree", "cloudImageDigest"];
const SOURCE_KEYS = ["sha", "tree", "clean"];
const PRODUCT_KEYS = ["resourceType", "packageId", "autoRenew"];
const AUTHORITY_KEYS = ["mode", "authorityClass", "j0ReadyDigest"];
const OPERATION_KEYS = ["idempotencyKey", "operationId", "workspaceId", "purchaseReceiptId", "resumed"];
const ACCOUNTING_KEYS = ["quoteUsdMicros", "walletBeforeUsdMicros", "walletAfterUsdMicros"];
const CLEANUP_KEYS = ["performed", "restored"];
const DEPLOYMENT_HEALTH = ["healthy", "degraded", "unknown"];

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
 * `ready` and "not ready" are separated in both directions. A ready receipt
 * carries the complete business evidence; a receipt that is not ready fails at
 * least one of the conditions readiness requires. A run therefore cannot report
 * a failure while holding every piece of evidence a pass needs, and cannot
 * report a pass while withholding one.
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
      !isDigest(value.authority.j0ReadyDigest) ||
      !hasExactKeys(value.operation, OPERATION_KEYS) || typeof value.operation.resumed !== "boolean" ||
      !hasExactKeys(value.scope, ["declared", "observed"]) ||
      !hasExactKeys(value.checks, QUALIFICATION_BUSINESS_CHECKS) ||
      !hasExactKeys(value.accounting, ACCOUNTING_KEYS) ||
      !hasExactKeys(value.cleanup, CLEANUP_KEYS) ||
      typeof value.cleanup.performed !== "boolean" || typeof value.cleanup.restored !== "boolean" ||
      !DEPLOYMENT_HEALTH.includes(value.deploymentHealth)) {
    invalidReceipt();
  }

  for (const bounds of [value.scope.declared, value.scope.observed]) {
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
  instant(value.startedAt);
  instant(value.completedAt);
  if (Date.parse(value.completedAt) < Date.parse(value.startedAt)) invalidReceipt();

  // A check that claims something happened has to name the thing that proves
  // it. Without this a receipt could hold a true check beside an empty
  // operation, and a receipt is only as strong as its least supported claim.
  if (value.checks.quote_priced && quote === null) invalidReceipt();
  if (value.checks.purchase_issued && value.scope.observed.purchases < 1) invalidReceipt();
  if (value.checks.receipt_bound && !String(value.operation.purchaseReceiptId || "").trim()) invalidReceipt();
  // A settled charge is a claim about money, so the wallet has to have been
  // read on both sides of the run for the claim to exist at all.
  if (value.checks.charge_settled && (before === null || after === null)) invalidReceipt();

  const withinBounds = QUALIFICATION_MUTATION_BOUNDS.every((name) => value.scope.observed[name] <= value.scope.declared[name]);
  const everyCheck = QUALIFICATION_BUSINESS_CHECKS.every((name) => value.checks[name]);
  const cleanupProven = value.cleanup.performed && value.cleanup.restored;
  const observed = value.scope.observed;
  // A settled purchase is evidenced two honest ways, and the receipt says
  // which: a delivery that bought it and watched the wallet move, or a
  // redelivery that resumed its own finished order and watched the wallet
  // stand still. The second is the idempotency claim, and it is only
  // reachable when the delivery says it resumed.
  const boughtAndPaid = observed.purchases === 1 && observed.charges === 1 &&
    quote !== null && moved === BigInt(quote);
  const resumedFinishedOrder = value.operation.resumed && observed.purchases === 1 && observed.charges === 0 && moved === 0n;
  const qualified = everyCheck && withinBounds && cleanupProven && observed.refunds === 0 &&
    (boughtAndPaid || resumedFinishedOrder);
  if ((value.state === "ready") !== qualified) invalidReceipt();
  return value;
}