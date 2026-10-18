/*
 * The protected business qualification producer.
 *
 * This is the Cloud-owned caller for one protected business qualification. It
 * does not contain a second business flow: it turns the workflow's declared
 * Candidate receipt digest, deployment binding, account/product declarations and
 * external J0 READY receipt into the shared `createQualificationRequest`, runs the
 * shared `runWorkspaceQualification` through the `tencent-tke` adapter, and
 * writes the shared Candidate-bound receipt. The purchase step delegates to the
 * merged deployed buyer, `runDeployedWorkspacePurchase`: that buyer owns the
 * source-envelope account/wallet reads, the declared-account digest check, the
 * quote and the bounded purchase. The orchestrator consumes the buyer's result
 * and reads the charge, Workspace and Runtime facts through the owner APIs;
 * nothing here decodes or reimplements them. The buyer's purchase receipt is
 * preserved beside the qualification receipt.
 *
 * The provider boundary is not implemented here either. The protected
 * Fabric/Instance runner supplies one module that owns TKE reachability and the
 * owner transport. This producer passes the normalized deployment binding and
 * the protected environment to that module and hands the boundary to the
 * adapter. An absent boundary, environment, secret or J0 producer is an external
 * prerequisite, not a qualification result, so this module refuses before it
 * opens a scope and never writes a fabricated pass.
 *
 * The provider is selected, not assumed: the deployment binding's
 * `fabricProvider` (already normalized and Candidate-bound by
 * `tencentTkeDeployment`) names the provider the J0 receipt, the shared request
 * and the emitted receipt must all carry. An unsupported provider is refused
 * before a scope opens, and the receipt is asserted against that same selection
 * after the shared flow returns it.
 *
 * A settled receipt is still a statement about the declared Workspace purchase
 * only. It is not a production, release-admission or full business-qualification
 * claim; the shared contract records `uncovered.businessQualification: false`
 * and this producer adds no claim of its own.
 */

import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { lstat, mkdir, open, readFile, writeFile } from "node:fs/promises";
import { basename, dirname, extname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { validateCloudInstallationIdentity } from "./cloud-business-readback.ts";
import { candidateReceiptDigest, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";
import {
  assertDeployedPurchaseRequest,
  deployedAccountIdentityDigest,
  deployedPurchaseIdempotencyKey,
  runDeployedWorkspacePurchase,
  validateDeployedPurchaseReceipt,
} from "./deployed-workspace-purchase.ts";
import {
  runWorkspaceQualification,
  type QualificationPurchaseCaller,
} from "./workspace-qualification-orchestrator.ts";
import {
  TENCENT_TKE_PROVIDER,
  createTencentTkeQualificationAdapter,
  tencentTkeDeployment,
  type TencentTkeDeployment,
  type TencentTkeProviderBoundary,
} from "./workspace-qualification-tencent-tke.ts";
import {
  QUALIFICATION_J0_READY_MAX_BYTES,
  createJ0ReadyReceiptValidator,
  createQualificationRequest,
  hasExactKeys,
  j0ReadyReceiptCandidateBinding,
  j0ReadyReceiptError,
  qualificationProviderProfile,
  qualificationWorkspaceName,
  validateBusinessQualificationReceipt,
} from "./workspace-qualification-contract.ts";
import { launchOperationId } from "./workspace-purchase-core.ts";

// The one protected context this producer may bind. The workflow sets this
// declaration explicitly; the environment's reviewer protection is enforced by
// the workflow admission job before this producer runs.
//
// The name is a protected-context marker rather than a GitHub environment
// lookup: this module never calls the environment API, never lists reviewers and
// never claims that protection exists. It checks that the step was exported from
// the declared protected context, and the workflow admission job is what proves
// the context is protected.
export const PROTECTED_QUALIFICATION_ENVIRONMENT = "cloud-business-qualification";

// The protected-input names. Values never enter the receipt or a summary.
export const PROTECTED_ENVIRONMENT_NAME = "OPL_QUALIFICATION_ENVIRONMENT";
export const PROTECTED_ORIGIN = "OPL_QUALIFICATION_ORIGIN";
export const PROTECTED_CUSTOMER_EMAIL = "OPL_QUALIFICATION_CUSTOMER_EMAIL";
export const PROTECTED_CUSTOMER_PASSWORD = "OPL_QUALIFICATION_CUSTOMER_PASSWORD";

// The protected runner exports this factory. It receives the normalized
// deployment binding and the protected environment; the adapter supplies the
// sealed request and operation key on each reachability call.
export const PROTECTED_BOUNDARY_FACTORY = "createTencentTkeProviderBoundary";

// The protected J0 receipt is a file outside the repository, owned by the runner
// and mode 0600, exactly like the existing Local-Docker J0 loader. The producer
// reads it through a no-follow descriptor and validates its source/provider
// binding before it buys anything. The cap is the contract's, so a producer
// writing the receipt and this reader cannot state two different limits.
export const J0_READY_MAX_BYTES = QUALIFICATION_J0_READY_MAX_BYTES;

// One declared purchase, one charge, no refund, no Workspace deletion and no
// managed Key. These are the only mutation bounds this producer may declare;
// they are not a workflow input.
export const PROTECTED_QUALIFICATION_SCOPE = Object.freeze({
  purchases: 1,
  charges: 1,
  refunds: 0,
  workspaceDeletions: 0,
  managedKeys: 0,
});

// The buyer's declared identity is stable across replays and Candidate-scoped
// by the shared buyer key derivation. It is not a customer identifier.
export const PROTECTED_DEPLOYED_PURCHASE_IDENTITY = "protected-business-qualification";

const DIGEST = /^sha256:[0-9a-f]{64}$/;
const DEPLOYMENT_KEYS = [
  "deploymentMode",
  "fabricProvider",
  "candidateReceiptDigest",
  "clusterId",
  "region",
  "namespace",
  "ownerOrigin",
] as const;
const RUN_OPTIONS = [
  "--candidate",
  "--candidate-digest",
  "--installation",
  "--installation-run",
  "--deployment",
  "--j0-ready-receipt",
  "--boundary",
  "--account-id",
  "--package-id",
  "--authority-class",
  "--output",
] as const;
const AUTHORITY_CLASSES = ["sandbox", "preproduction"] as const;

const EXTERNAL_PREREQUISITE_ABSENT = "external_prerequisite_absent";
const PROTECTED_QUALIFICATION_INVALID = "protected_business_qualification_invalid";
const PROTECTED_QUALIFICATION_NOT_SETTLED = "protected_business_qualification_not_settled";
const PROTECTED_QUALIFICATION_FAILED = "protected_business_qualification_failed";

export type ProtectedEnvironment = Readonly<Record<string, string | undefined>>;
export type ProtectedAuthorityClass = (typeof AUTHORITY_CLASSES)[number];

export type ProtectedBusinessQualificationInput = Readonly<{
  candidatePath: string;
  candidateReceiptDigest: string;
  installationPath: string;
  installationRunPath: string;
  deploymentPath: string;
  j0ReadyReceiptPath: string;
  boundaryModulePath: string;
  accountId: string;
  packageId: string;
  authorityClass: ProtectedAuthorityClass;
  outputPath: string;
}>;

export type ProtectedBusinessQualificationResult = Readonly<{
  state: string;
  reason: string;
  receipt: Record<string, any>;
  receiptDigest: string;
  purchaseReceipt: Record<string, any> | null;
  purchaseReceiptDigest: string;
  purchaseReceiptPath: string;
  candidateReceiptDigest: string;
  installationReceiptDigest: string;
}>;

export type ProtectedProviderBoundaryFactory = (input: {
  deployment: TencentTkeDeployment;
  environment: ProtectedEnvironment;
}) => TencentTkeProviderBoundary | Promise<TencentTkeProviderBoundary>;

function producerError(code: string, message: string): Error & { code: string } {
  const error = new Error(message) as Error & { code: string };
  error.code = code;
  return error;
}

function externalPrerequisiteAbsent(name: string): never {
  throw producerError(EXTERNAL_PREREQUISITE_ABSENT, `external prerequisite absent: ${name}`);
}

function invalid(message: string): never {
  throw producerError(PROTECTED_QUALIFICATION_INVALID, message);
}

function nonEmpty(value: unknown, maxLength = 1024): string {
  const text = typeof value === "string" ? value.trim() : "";
  return text && text.length <= maxLength && !/[\u0000-\u001f\u007f]/.test(text) ? text : "";
}

function protectedOrigin(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    invalid("protected qualification origin is not an absolute URL");
  }
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password ||
      url.search || url.hash || url.pathname !== "/") {
    invalid("protected qualification origin is not an exact HTTP(S) origin");
  }
  return url.origin;
}

/*
 * The protected prerequisite declaration.
 *
 * Every named declaration is checked before a file is read or a provider is
 * contacted. The two credential values are required to be present so a missing
 * protected Secret is a clear failure rather than an empty login; this producer
 * never uses them itself. The boundary module does, through the environment it
 * receives.
 *
 * There is deliberately no cross-repository evidence token here. This producer
 * reads only the files its own workflow materialized and the J0 receipt its own
 * protected context produced, so there is no cross-repository read to authorize;
 * a token it never reads would be a credential surface that buys nothing. The
 * Instance evidence reads that do need one belong to the workflow's own steps,
 * not to this producer.
 */
export function protectedQualificationPrerequisites(environment: ProtectedEnvironment) {
  const bound = String(environment[PROTECTED_ENVIRONMENT_NAME] || "").trim();
  if (bound !== PROTECTED_QUALIFICATION_ENVIRONMENT) externalPrerequisiteAbsent(PROTECTED_ENVIRONMENT_NAME);
  const originText = String(environment[PROTECTED_ORIGIN] || "");
  if (!originText.trim()) externalPrerequisiteAbsent(PROTECTED_ORIGIN);
  const origin = protectedOrigin(originText);
  if (!nonEmpty(environment[PROTECTED_CUSTOMER_EMAIL])) externalPrerequisiteAbsent(PROTECTED_CUSTOMER_EMAIL);
  if (!nonEmpty(environment[PROTECTED_CUSTOMER_PASSWORD])) externalPrerequisiteAbsent(PROTECTED_CUSTOMER_PASSWORD);
  return Object.freeze({
    environment: bound,
    origin,
    customerEmail: String(environment[PROTECTED_CUSTOMER_EMAIL]).trim(),
  });
}

async function readRequiredFile(path: string, label: string): Promise<string> {
  const normalized = String(path || "").trim();
  if (!normalized) externalPrerequisiteAbsent(label);
  let info;
  try {
    info = await lstat(normalized);
  } catch {
    externalPrerequisiteAbsent(label);
  }
  if (!info.isFile()) externalPrerequisiteAbsent(label);
  try {
    return await readFile(normalized, "utf8");
  } catch {
    externalPrerequisiteAbsent(label);
  }
}

function parseJson(text: string, message: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    invalid(message);
  }
}

/*
 * The declared Candidate and its receipt digest.
 *
 * The Candidate's own validator and digest function define the identity; the
 * workflow's declared digest is checked against that derived value rather than
 * trusted. A different bundle therefore cannot be qualified under the declared
 * Candidate.
 */
export async function readProtectedCandidate(path: string, declaredDigest: string) {
  const normalizedDigest = String(declaredDigest || "").trim();
  if (!DIGEST.test(normalizedDigest)) invalid("declared Candidate receipt digest is invalid");
  const text = await readRequiredFile(path, "Candidate receipt");
  const value = parseJson(text, "Candidate receipt is not valid JSON");
  let candidate;
  try {
    candidate = validateCloudCandidateReceipt(value) as Record<string, any>;
  } catch {
    invalid("Candidate receipt is invalid");
  }
  const digest = candidateReceiptDigest(candidate);
  if (digest !== normalizedDigest) invalid("Candidate receipt digest does not match the declared digest");
  return Object.freeze({ candidate, digest, text });
}

/*
 * The approved deployment binding.
 *
 * The protected origin is authoritative. A binding that names another origin,
 * another Candidate, another provider or another deployment shape is refused
 * here and never reaches the adapter.
 */
export async function readProtectedDeployment(path: string, candidateDigest: string, protectedOwnerOrigin: string) {
  const text = await readRequiredFile(path, "deployment binding");
  const value = parseJson(text, "deployment binding is not valid JSON");
  if (!hasExactKeys(value, DEPLOYMENT_KEYS)) invalid("deployment binding has an invalid shape");
  const binding = value as Record<string, any>;
  let deployment: TencentTkeDeployment;
  try {
    deployment = tencentTkeDeployment(binding) as TencentTkeDeployment;
  } catch {
    invalid("deployment binding is invalid");
  }
  if (deployment.candidateReceiptDigest !== candidateDigest) invalid("deployment binding names another Candidate");
  if (deployment.ownerOrigin !== protectedOwnerOrigin) invalid("deployment binding names another owner origin");
  return deployment;
}

/*
 * The original Instance deployment evidence.
 *
 * The existing readback binding validator owns the receipt/run association. It
 * checks the exact Instance workflow, the successful run, the deployment receipt
 * status and every Candidate identity field. This producer reuses it so the
 * deployment binding is joined to the same Candidate through the owner's own
 * evidence, not through a second decoder.
 */
export async function readProtectedInstallation(
  path: string,
  runPath: string,
  candidate: unknown,
  candidateDigest: string,
) {
  const text = await readRequiredFile(path, "Instance deployment receipt");
  const runText = await readRequiredFile(runPath, "Instance deployment run");
  const run = parseJson(runText, "Instance deployment run is not valid JSON");
  try {
    const identity = validateCloudInstallationIdentity(candidate, text, run);
    if (identity.receiptDigest && typeof identity.receiptDigest === "string" && !DIGEST.test(identity.receiptDigest)) {
      invalid("Instance deployment receipt digest is invalid");
    }
    return Object.freeze({ text, run: run as Record<string, any>, identity });
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === PROTECTED_QUALIFICATION_INVALID) throw error;
    invalid(`Instance deployment evidence does not bind the declared Candidate ${candidateDigest}`);
  }
}

function insideRepository(path: string, repositoryRoot: string): boolean {
  const rel = relative(repositoryRoot, path);
  return rel === "" || (rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel));
}

/*
 * The J0 READY producer's receipt, as this lane consumes it.
 *
 * This is the same protected-file standard as the existing Local-Docker J0
 * loader: an absolute path outside the repository, a regular file, mode 0600,
 * owned by the runner, no-follow and bounded in size. The cap is enforced again
 * while the bytes are read, so a file that grows after the stat cannot make this
 * reader hold more than the contract allows.
 *
 * Two refusals are deliberately different. A receipt that cannot be read is an
 * absent prerequisite. A receipt that is well formed but names another
 * Candidate is also absent *for this run*: its sandbox qualification is not
 * evidence about the Candidate being qualified, so `external_prerequisite_absent`
 * is the honest answer and no provider scope opens. A receipt that is present
 * and malformed keeps the contract's own `j0_ready_receipt_invalid` code.
 *
 * `provider` is the deployment-selected provider. The receipt has to name that
 * same provider, so a sandbox pass for one provider cannot authorize another
 * provider's business run.
 */
export async function loadProtectedJ0ReadyReceipt(path: string, {
  sourceSha,
  sourceTree,
  cloudImageDigest,
  provider,
  repositoryRoot,
}: {
  sourceSha: string;
  sourceTree: string;
  cloudImageDigest: string;
  provider: string;
  repositoryRoot: string;
}) {
  const normalized = String(path || "").trim();
  if (!normalized) externalPrerequisiteAbsent("J0 READY producer receipt");
  if (!isAbsolute(normalized)) invalid("J0 READY producer receipt path must be absolute");
  let handle;
  try {
    handle = await open(normalized, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  } catch {
    externalPrerequisiteAbsent("J0 READY producer receipt");
  }
  try {
    const info = await handle.stat();
    if (!info.isFile() || info.size === 0 || info.size > J0_READY_MAX_BYTES) {
      invalid("J0 READY producer receipt file is invalid");
    }
    if (typeof process.getuid === "function" && info.uid !== process.getuid()) {
      invalid("J0 READY producer receipt is not owned by the runner");
    }
    if ((info.mode & 0o777) !== 0o600) invalid("J0 READY producer receipt mode must be 0600");
    if (insideRepository(normalized, repositoryRoot)) invalid("J0 READY producer receipt must be outside the repository");
    // One bounded read from the descriptor just opened and stat'ed: no second
    // path lookup and no unbounded buffer, so the cap above holds for the bytes
    // actually parsed rather than only for the size that was announced.
    const buffer = Buffer.alloc(J0_READY_MAX_BYTES + 1);
    let offset = 0;
    for (;;) {
      const { bytesRead } = await handle.read(buffer, offset, buffer.length - offset, offset);
      if (bytesRead === 0) break;
      offset += bytesRead;
      if (offset > J0_READY_MAX_BYTES) invalid("J0 READY producer receipt exceeds the byte cap");
    }
    const raw = buffer.subarray(0, offset);
    let value;
    try {
      value = JSON.parse(raw.toString("utf8"));
    } catch {
      throw j0ReadyReceiptError();
    }
    const expectedCandidate = Object.freeze({
      productSha: sourceSha,
      productTree: sourceTree,
      cloudImageDigest,
    });
    // A receipt that names another Candidate is stale for this run. It is
    // detected before the shape validator so the refusal says "absent for this
    // Candidate" instead of "invalid". A receipt with no complete binding (for
    // example a missing or non-digest `cloudImageDigest`) has no Candidate to
    // compare and falls through to the contract as malformed.
    const named = j0ReadyReceiptCandidateBinding(value);
    if (named && (named.productSha !== expectedCandidate.productSha ||
        named.productTree !== expectedCandidate.productTree ||
        named.cloudImageDigest !== expectedCandidate.cloudImageDigest)) {
      externalPrerequisiteAbsent("J0 READY producer receipt for this Candidate");
    }
    const profile = qualificationProviderProfile(provider);
    let receipt;
    try {
      receipt = createJ0ReadyReceiptValidator(profile)(value, sourceSha, sourceTree, expectedCandidate);
    } catch {
      throw j0ReadyReceiptError();
    }
    return receipt;
  } finally {
    await handle.close();
  }
}

/*
 * The protected Fabric/Instance boundary module.
 *
 * Cloud does not implement TKE reachability. The protected runner supplies one
 * module exporting the named factory; it receives the normalized deployment and
 * the protected environment and returns the typed three-operation boundary. A
 * missing module is an external prerequisite. A module that exists but cannot be
 * loaded, does not export the factory or returns a malformed boundary is a
 * producer configuration error, not an absent prerequisite.
 */
export async function loadProtectedProviderBoundary(
  modulePath: string,
  { deployment, environment }: { deployment: TencentTkeDeployment; environment: ProtectedEnvironment },
) {
  const normalized = String(modulePath || "").trim();
  if (!normalized) externalPrerequisiteAbsent("protected provider boundary module");
  if (!isAbsolute(normalized)) invalid("protected provider boundary module path must be absolute");
  let info;
  try {
    info = await lstat(normalized);
  } catch {
    externalPrerequisiteAbsent("protected provider boundary module");
  }
  if (!info.isFile()) externalPrerequisiteAbsent("protected provider boundary module");
  let imported: Record<string, unknown>;
  try {
    imported = await import(pathToFileURL(normalized).href) as Record<string, unknown>;
  } catch {
    invalid("protected provider boundary module could not be loaded");
  }
  const factory = imported[PROTECTED_BOUNDARY_FACTORY] ?? imported.default;
  if (typeof factory !== "function") {
    invalid(`protected provider boundary module must export ${PROTECTED_BOUNDARY_FACTORY}`);
  }
  let boundary;
  try {
    boundary = await (factory as ProtectedProviderBoundaryFactory)({ deployment, environment });
  } catch {
    invalid("protected provider boundary module failed to construct the provider boundary");
  }
  if (!boundary || typeof boundary !== "object" || Array.isArray(boundary)) {
    invalid("protected provider boundary module returned an invalid boundary");
  }
  return boundary;
}

/*
 * The provider the shared flow runs for.
 *
 * The deployment binding selects it, and the binding was already normalized and
 * Candidate-bound by the provider's own `tencentTkeDeployment`. This is the
 * producer's own stop as well: the selected provider has to be one this
 * producer has an adapter and a J0 profile for, so no other value can reach the
 * shared request, the J0 validator or the emitted receipt.
 */
export function selectedQualificationProvider(deployment: TencentTkeDeployment): string {
  const provider = String(deployment?.fabricProvider || "").trim();
  if (provider !== TENCENT_TKE_PROVIDER) invalid("deployment binding names an unsupported provider");
  return provider;
}

/*
 * The one request this producer is allowed to build.
 *
 * The Candidate's own product identity is the source identity, the authority is
 * live with the external J0 receipt, the provider is the one the deployment
 * binding selected, and the mutation scope is the fixed bound above. The shared
 * contract validates and brands the request; a hand-assembled object cannot be
 * substituted.
 */
export function buildProtectedQualificationRequest({
  candidate,
  j0ReadyReceipt,
  accountId,
  packageId,
  authorityClass,
  provider,
}: {
  candidate: any;
  j0ReadyReceipt: unknown;
  accountId: string;
  packageId: string;
  authorityClass: ProtectedAuthorityClass;
  provider: string;
}) {
  const product = Object.freeze({ resourceType: "workspace", packageId: String(packageId || "").trim(), autoRenew: false });
  return createQualificationRequest({
    candidate,
    accountId,
    product,
    provider,
    scope: PROTECTED_QUALIFICATION_SCOPE,
    authority: { mode: "live", authorityClass, j0ReadyReceipt },
    source: { sha: candidate.product.sha, tree: candidate.product.tree, clean: true },
  });
}

/*
 * The buyer request the producer's purchase step declares.
 *
 * The buyer owns the source-envelope account/wallet reads, the declared
 * account email digest, the quote and the purchase sequence. This builder only
 * names the declarations the protected flow already holds; it does not read an
 * owner API or perform a purchase. The buyer key is Candidate-scoped, so a
 * replay for another Candidate cannot address this order.
 */
export function buildProtectedDeployedPurchaseRequest({
  candidateDigest,
  environment,
  accountId,
  packageId,
}: {
  candidateDigest: string;
  environment: { origin: string; customerEmail: string };
  accountId: string;
  packageId: string;
}) {
  const idempotencyIdentity = PROTECTED_DEPLOYED_PURCHASE_IDENTITY;
  const idempotencyKey = deployedPurchaseIdempotencyKey(candidateDigest, idempotencyIdentity);
  const operationId = launchOperationId(accountId, idempotencyKey);
  const request = {
    baseURL: environment.origin,
    authorityClass: "production",
    candidateReceiptDigest: candidateDigest,
    accountEmailIdentityDigest: deployedAccountIdentityDigest(environment.customerEmail),
    packageId,
    autoRenew: false,
    idempotencyIdentity,
    workspaceName: qualificationWorkspaceName(operationId),
  };
  assertDeployedPurchaseRequest(request);
  return Object.freeze(request);
}

/*
 * The buyer's refusal is an outcome of the protected purchase step, not an
 * unclassified crash. Map its typed codes onto the shared orchestrator's
 * terminal reasons so the qualification receipt keeps the distinction between
 * a refused identity, an unavailable readback and a lost response.
 */
function protectedPurchaseFailure(error): Error {
  const code = String((error as { code?: string } | null)?.code || "");
  const message = String((error as { message?: string } | null)?.message || "deployed purchase failed");
  let mapped = "owner_readback_unavailable";
  if (code === "deployed_purchase_account_mismatch") mapped = "account_binding_mismatch";
  else if (code === "deployed_purchase_target_invalid" || code === "deployed_purchase_caller_invalid") {
    mapped = "provider_scope_unavailable";
  } else if (code === "deployed_purchase_operation_mismatch" || code === "deployed_purchase_replay_conflict") {
    mapped = "launch_identity_mismatch";
  } else if (code === "deployed_purchase_receipt_missing" || code === "deployed_purchase_receipt_invalid") {
    mapped = "purchase_receipt_missing";
  } else if (code === "deployed_purchase_quote_unavailable" || code === "deployed_purchase_price_unavailable") {
    mapped = "quote_invalid";
  } else if (/wallet cannot fund/i.test(message)) {
    mapped = "wallet_cannot_fund";
  }
  const failure = new Error(`${code || "deployed_purchase_failed"}: ${message}`);
  (failure as { code?: string }).code = mapped;
  (failure as { cause?: unknown }).cause = error;
  return failure;
}

export function purchaseReceiptPath(outputPath: string): string {
  const normalized = String(outputPath || "").trim();
  const extension = extname(normalized) || ".json";
  return join(dirname(normalized), `${basename(normalized, extname(normalized))}.purchase${extension}`);
}

export async function emitBusinessQualificationReceipt(path: string, text: string) {
  const normalized = String(path || "").trim();
  if (!normalized) externalPrerequisiteAbsent("business qualification receipt output path");
  await mkdir(dirname(normalized), { recursive: true, mode: 0o700 });
  await writeFile(normalized, text, { mode: 0o600, flag: "wx" });
}

/*
 * The Candidate binding the producer asserts on the receipt it emits.
 *
 * The shared contract closes a receipt to its declared keys and checks that it
 * is internally consistent, but it cannot know which Candidate this run
 * declared: a receipt whose candidate block agreed with itself could still name
 * another Candidate's bytes. The producer read the exact Candidate, so it
 * asserts the executed identity triple, the source identity, the deployment's
 * Candidate digest and the selected provider before the receipt is written. A
 * mismatch is a producer defect rather than a qualification outcome, so the
 * receipt is refused instead of being persisted under the wrong Candidate.
 */
export function assertProtectedReceiptBinding(receipt: any, {
  candidate,
  candidateDigest,
  provider,
  deployment,
}: {
  candidate: any;
  candidateDigest: string;
  provider: string;
  deployment: TencentTkeDeployment;
}) {
  const product = candidate?.product || {};
  const image = candidate?.cloudImage || {};
  if (!receipt || typeof receipt !== "object" ||
      receipt.candidate?.receiptDigest !== candidateDigest ||
      receipt.candidate?.productSha !== product.sha ||
      receipt.candidate?.productTree !== product.tree ||
      receipt.candidate?.cloudImageDigest !== image.indexDigest ||
      receipt.source?.sha !== product.sha ||
      receipt.source?.tree !== product.tree ||
      receipt.source?.clean !== true ||
      receipt.provider !== provider ||
      deployment?.candidateReceiptDigest !== candidateDigest ||
      deployment?.fabricProvider !== provider) {
    invalid("protected business qualification receipt does not bind the declared Candidate and provider");
  }
  return receipt;
}

/*
 * Run one protected qualification.
 *
 * The order is deliberate: protected environment, Candidate, deployment binding,
 * Instance deployment evidence, J0 producer, request, boundary, then the shared
 * orchestrator. No provider is contacted before every declaration and every
 * external receipt has been validated. The receipt is written before the caller
 * learns whether it settled, so a failed, blocked, unknown or recovery-required
 * outcome is preserved as an artifact instead of being lost; only an absent
 * prerequisite or an invalid declaration has no qualification receipt to emit.
 */
export async function runProtectedBusinessQualification(
  input: ProtectedBusinessQualificationInput,
  dependencies: {
    environment?: ProtectedEnvironment;
    boundary?: TencentTkeProviderBoundary;
    wait?: (milliseconds: number) => Promise<void>;
  } = {},
): Promise<ProtectedBusinessQualificationResult> {
  const environment = dependencies.environment ?? process.env;
  const protectedEnvironment = protectedQualificationPrerequisites(environment);
  const candidate = await readProtectedCandidate(input.candidatePath, input.candidateReceiptDigest);
  const deployment = await readProtectedDeployment(
    input.deploymentPath,
    candidate.digest,
    protectedEnvironment.origin,
  );
  const installation = await readProtectedInstallation(
    input.installationPath,
    input.installationRunPath,
    candidate.candidate,
    candidate.digest,
  );
  // The deployment binding selects the provider. Everything below carries that
  // one value: the J0 profile, the shared request and the emitted receipt.
  const provider = selectedQualificationProvider(deployment);
  const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const j0ReadyReceipt = await loadProtectedJ0ReadyReceipt(input.j0ReadyReceiptPath, {
    sourceSha: candidate.candidate.product.sha,
    sourceTree: candidate.candidate.product.tree,
    cloudImageDigest: candidate.candidate.cloudImage.indexDigest,
    provider,
    repositoryRoot,
  });
  const request = buildProtectedQualificationRequest({
    candidate: candidate.candidate,
    j0ReadyReceipt,
    accountId: input.accountId,
    packageId: input.packageId,
    authorityClass: input.authorityClass,
    provider,
  });
  let deployedPurchaseRequest;
  try {
    deployedPurchaseRequest = buildProtectedDeployedPurchaseRequest({
      candidateDigest: candidate.digest,
      environment: protectedEnvironment,
      accountId: input.accountId,
      packageId: input.packageId,
    });
  } catch {
    invalid("protected deployed purchase declaration is invalid");
  }
  const boundary = dependencies.boundary ?? await loadProtectedProviderBoundary(input.boundaryModulePath, {
    deployment,
    environment,
  });
  const adapter = createTencentTkeQualificationAdapter({ deployment, boundary });
  let purchaseReceipt: Record<string, any> | null = null;
  const purchase = Object.freeze({
    async run(caller: QualificationPurchaseCaller) {
      let receipt;
      try {
        receipt = await runDeployedWorkspacePurchase(deployedPurchaseRequest, {
          origin: protectedEnvironment.origin,
          http: caller.http,
          auth: caller.auth,
          wait: caller.wait,
        });
        validateDeployedPurchaseReceipt(receipt);
      } catch (error) {
        throw protectedPurchaseFailure(error);
      }
      purchaseReceipt = receipt;
      return Object.freeze({
        idempotencyKey: receipt.operation.idempotencyKey,
        operationId: receipt.operation.operationId,
        workspaceId: receipt.operation.workspaceId,
        purchaseReceiptId: receipt.operation.purchaseReceiptId,
        resumed: receipt.operation.resumed,
        amountUsdMicros: receipt.purchase.amountUsdMicros,
        purchases: receipt.mutations.observed.purchases,
        candidateReceiptDigest: receipt.candidate.receiptDigest,
        product: Object.freeze({ ...receipt.product }),
      });
    },
  });
  const result = await runWorkspaceQualification(
    { request, adapter },
    { wait: dependencies.wait, purchase },
  );
  // The buyer's receipt is written first, before the qualification receipt is
  // validated. It is the only durable evidence that a real charge happened, so a
  // qualification receipt the contract rejects (or one that fails the Candidate
  // binding below) must not suppress it. The write order is the same as the
  // producer had before the binding assertion existed; only the assertion is new.
  let purchaseArtifactPath = "";
  let purchaseArtifactDigest = "";
  if (purchaseReceipt) {
    const purchaseText = `${JSON.stringify(purchaseReceipt, null, 2)}\n`;
    purchaseArtifactPath = purchaseReceiptPath(input.outputPath);
    purchaseArtifactDigest = `sha256:${createHash("sha256").update(purchaseText).digest("hex")}`;
    await emitBusinessQualificationReceipt(purchaseArtifactPath, purchaseText);
  }
  // The qualification receipt is bound to the Candidate and the selected
  // provider before it is written, so a receipt that names other bytes is
  // refused instead of being persisted.
  const receipt = validateBusinessQualificationReceipt(result.receipt) as Record<string, any>;
  assertProtectedReceiptBinding(receipt, {
    candidate: candidate.candidate,
    candidateDigest: candidate.digest,
    provider,
    deployment,
  });
  const receiptText = `${JSON.stringify(receipt, null, 2)}\n`;
  await emitBusinessQualificationReceipt(input.outputPath, receiptText);
  return Object.freeze({
    state: String(receipt.state),
    reason: String(receipt.reason),
    receipt,
    receiptDigest: `sha256:${createHash("sha256").update(receiptText).digest("hex")}`,
    purchaseReceipt,
    purchaseReceiptDigest: purchaseArtifactDigest,
    purchaseReceiptPath: purchaseArtifactPath,
    candidateReceiptDigest: candidate.digest,
    installationReceiptDigest: String(installation.identity.receiptDigest),
  });
}

export function parseProtectedQualificationArgs(args: readonly string[]): ProtectedBusinessQualificationInput {
  if (args[0] !== "run") invalid("expected the run command");
  const options = new Map<string, string>();
  for (let index = 1; index < args.length; index += 2) {
    const name = args[index];
    const value = args[index + 1];
    if (!(RUN_OPTIONS as readonly string[]).includes(name) || options.has(name) ||
        typeof value !== "string" || value.trim() === "") {
      invalid("protected business qualification arguments are invalid");
    }
    options.set(name, value);
  }
  for (const name of RUN_OPTIONS) {
    if (!options.has(name)) invalid(`protected business qualification requires ${name}`);
  }
  const authorityClass = options.get("--authority-class");
  if (!(AUTHORITY_CLASSES as readonly string[]).includes(String(authorityClass))) {
    invalid("authority class must be sandbox or preproduction");
  }
  return Object.freeze({
    candidatePath: options.get("--candidate") as string,
    candidateReceiptDigest: options.get("--candidate-digest") as string,
    installationPath: options.get("--installation") as string,
    installationRunPath: options.get("--installation-run") as string,
    deploymentPath: options.get("--deployment") as string,
    j0ReadyReceiptPath: options.get("--j0-ready-receipt") as string,
    boundaryModulePath: options.get("--boundary") as string,
    accountId: options.get("--account-id") as string,
    packageId: options.get("--package-id") as string,
    authorityClass: authorityClass as ProtectedAuthorityClass,
    outputPath: options.get("--output") as string,
  });
}

export type ProtectedBusinessQualificationSummary = Readonly<{
  status: string;
  reason: string;
  provider: string;
  scope: string;
  businessQualification: false;
  receiptDigest: string;
  purchaseReceiptDigest: string;
  candidateReceiptDigest: string;
  installationReceiptDigest: string;
  artifact: "receipt.json";
  purchaseArtifact: string;
}>;

export function protectedBusinessQualificationSummary(
  result: ProtectedBusinessQualificationResult,
): ProtectedBusinessQualificationSummary {
  return Object.freeze({
    status: result.state,
    reason: result.reason,
    provider: String(result.receipt.provider),
    scope: String(result.receipt.scope),
    businessQualification: false,
    receiptDigest: result.receiptDigest,
    purchaseReceiptDigest: result.purchaseReceiptDigest,
    candidateReceiptDigest: result.candidateReceiptDigest,
    installationReceiptDigest: result.installationReceiptDigest,
    artifact: "receipt.json",
    purchaseArtifact: result.purchaseReceiptPath ? basename(result.purchaseReceiptPath) : "",
  });
}

export async function main(
  args: readonly string[] = process.argv.slice(2),
  environment: ProtectedEnvironment = process.env,
) {
  const input = parseProtectedQualificationArgs(args);
  const result = await runProtectedBusinessQualification(input, { environment });
  const summary = protectedBusinessQualificationSummary(result);
  process.stdout.write(`${JSON.stringify(summary)}\n`);
  if (result.state !== "settled") {
    throw producerError(
      PROTECTED_QUALIFICATION_NOT_SETTLED,
      `protected business qualification is ${result.state}: ${result.reason}`,
    );
  }
  return summary;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error: unknown) => {
    const code = String((error as { code?: string } | null)?.code || PROTECTED_QUALIFICATION_FAILED);
    const message = String((error as { message?: string } | null)?.message || "protected business qualification failed");
    process.stderr.write(`${code}: ${message}\n`);
    process.exitCode = 1;
  });
}
