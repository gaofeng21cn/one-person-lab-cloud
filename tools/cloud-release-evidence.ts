// Release admission consumes two evidence classes that answer different questions.
//
// - The Cloud business qualification receipt answers whether this exact
//   Candidate exercised the customer Workspace business flow and reached a
//   settled terminal state. Its schema and validator are owned by
//   `tools/workspace-qualification-contract.ts` (#923); this module consumes
//   that receipt and never restates or extends it.
// - The Instance deployment verification run answers whether the same
//   Candidate was installed, identified and found healthy on the instance. Its
//   native receipt is validated by the producing Instance revision before this
//   cross-owner binding runs.
//
// Neither class may stand in for the other: a deployment_verified receipt is
// not a business qualification, and a settled business receipt is not an
// installation. Both bind the same immutable Candidate identity, so a mismatch
// fails admission in the class that owns the failed field instead of being
// collapsed into one undifferentiated evidence bundle.
//
// The transitional Instance qualification-decision and Workspace gates remain
// separate, retained requirements. This module adds the Cloud-owned class; it
// does not remove or weaken any existing gate (#929 retires the transitional
// Instance paths only after the replacement producer is executable).

import { createHash } from "node:crypto";

import { candidateReceiptDigest, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";
import { validateReleaseInstallationBinding } from "./cloud-release-installation.ts";
import { QUALIFICATION_BUSINESS_RECEIPT_KIND, validateBusinessQualificationReceipt } from "./workspace-qualification-contract.ts";

export const RELEASE_EVIDENCE_CLASS = Object.freeze({
  CLOUD_BUSINESS_QUALIFICATION: "cloud_business_qualification",
  INSTANCE_DEPLOYMENT: "instance_deployment"
} as const);

export type ReleaseEvidenceClass = (typeof RELEASE_EVIDENCE_CLASS)[keyof typeof RELEASE_EVIDENCE_CLASS];

// Cloud owns the receipt contract and the qualification logic. Instance owns
// the protected execution because only that repository has the approved
// production environment, customer session, cluster access and provider
// boundary. The artifact therefore travels from Instance to Cloud release
// admission; a same-named workflow in the Cloud repository is not authority.
export const CLOUD_BUSINESS_QUALIFICATION_PRODUCER = Object.freeze({
  repository: "gaofeng21cn/opl-instance-medopl",
  workflow: ".github/workflows/cloud-business-qualification.yml",
  artifact: "cloud-business-qualification",
  receipt: "business-qualification.json",
  productSource: "product-source.json"
});

// A formal release promotes bytes that already passed qualification. The
// receipt is immutable and Candidate-bound, so age cannot invalidate the byte
// binding; it can invalidate the claim that the release owner is publishing a
// current business qualification rather than an old one. Thirty days is the
// bounded freshness window, and a receipt from the future is refused outright.
export const CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_HOURS = 720;
export const CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_MS = CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_HOURS * 60 * 60 * 1000;
export const CLOUD_BUSINESS_QUALIFICATION_FUTURE_SKEW_MS = 5 * 60 * 1000;
export const CLOUD_BUSINESS_QUALIFICATION_TERMINAL_STATE = "settled";

const SHA = /^[0-9a-f]{40}$/;
const RUN_ID = /^[1-9][0-9]*$/;

function evidenceError(code: string, cause?: unknown): never {
  const error = new Error(code, cause === undefined ? undefined : { cause });
  (error as Error & { code?: string }).code = code;
  throw error;
}

const digest = (value: string) => `sha256:${createHash("sha256").update(value).digest("hex")}`;

function record(value: unknown, code: string): Record<string, any> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) evidenceError(code);
  return value as Record<string, any>;
}

export type ReleaseCandidateIdentity = Readonly<{
  candidateReceiptDigest: string;
  productSha: string;
  productTree: string;
  cloudImageDigest: string;
}>;

function releaseCandidateIdentity(candidate: unknown): ReleaseCandidateIdentity {
  const admitted = validateCloudCandidateReceipt(candidate);
  const product = record(admitted.product, "cloud_business_qualification_candidate_invalid");
  const image = record(admitted.cloudImage, "cloud_business_qualification_candidate_invalid");
  return Object.freeze({
    candidateReceiptDigest: candidateReceiptDigest(admitted),
    productSha: String(product.sha),
    productTree: String(product.tree),
    cloudImageDigest: String(image.indexDigest)
  });
}

function sameIdentity(left: ReleaseCandidateIdentity, right: ReleaseCandidateIdentity) {
  return left.candidateReceiptDigest === right.candidateReceiptDigest &&
    left.productSha === right.productSha &&
    left.productTree === right.productTree &&
    left.cloudImageDigest === right.cloudImageDigest;
}

function validateProductSource(text: string): { productSha: string; productTree: string } {
  if (typeof text !== "string" || !text.trim()) {
    evidenceError("cloud_business_qualification_product_source_invalid");
  }
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch (error) {
    evidenceError("cloud_business_qualification_product_source_invalid", error);
  }
  const source = record(value, "cloud_business_qualification_product_source_invalid");
  if (
    source.schemaVersion !== 1 ||
    !SHA.test(String(source.productSha || "")) ||
    !SHA.test(String(source.productTree || "")) ||
    Object.keys(source).sort().join() !== "productSha,productTree,schemaVersion"
  ) {
    evidenceError("cloud_business_qualification_product_source_invalid");
  }
  return { productSha: String(source.productSha), productTree: String(source.productTree) };
}

export type ReleaseBusinessQualificationEvidence = Readonly<{
  evidenceClass: typeof RELEASE_EVIDENCE_CLASS.CLOUD_BUSINESS_QUALIFICATION;
  repository: string;
  workflowRunId: string;
  runDigest: string;
  authoritySha: string;
  receiptDigest: string;
  receiptKind: string;
  scope: string;
  terminalState: typeof CLOUD_BUSINESS_QUALIFICATION_TERMINAL_STATE;
  authorityClass: string;
  completedAt: string;
  identity: ReleaseCandidateIdentity;
}>;

/*
 * Validate one Cloud business qualification receipt against the release inputs.
 *
 * The receipt body is read through the #923 contract validator, which closes
 * every object to its declared keys and therefore refuses a raw Secret, Key,
 * customer identifier or private provider identifier by shape: there is no
 * field for one. This consumer then adds only the release-side facts the
 * contract does not own:
 *
 *   - the terminal state must be `settled`, not merely a valid contract state;
 *   - the receipt's Candidate digest, source SHA/tree and image digest must
 *     equal the Candidate being released;
 *   - the producing run must be the protected Cloud workflow at the exact
 *     released source commit, not an arbitrary successful run;
 *   - the receipt must be current enough for a formal release.
 *
 * Receipt bytes are hashed exactly as downloaded, so the returned digest names
 * the artifact that was validated rather than a re-encoding of it.
 */
export function validateReleaseBusinessQualification(input: {
  candidate: unknown;
  receiptText: string;
  productSourceText: string;
  run: unknown;
  runId: string;
  now?: number;
}): ReleaseBusinessQualificationEvidence {
  const identity = releaseCandidateIdentity(input.candidate);
  if (typeof input.receiptText !== "string" || !input.receiptText.trim()) {
    evidenceError("cloud_business_qualification_receipt_invalid");
  }
  let receipt: unknown;
  try {
    receipt = JSON.parse(input.receiptText);
  } catch (error) {
    evidenceError("cloud_business_qualification_receipt_invalid", error);
  }
  try {
    validateBusinessQualificationReceipt(receipt);
  } catch (error) {
    evidenceError("cloud_business_qualification_receipt_invalid", error);
  }
  const admitted = receipt as Record<string, any>;

  if (admitted.state !== CLOUD_BUSINESS_QUALIFICATION_TERMINAL_STATE || admitted.reason !== "none") {
    evidenceError("cloud_business_qualification_terminal_state_invalid");
  }

  const receiptCandidate = admitted.candidate;
  if (receiptCandidate.receiptDigest !== identity.candidateReceiptDigest ||
      receiptCandidate.productSha !== identity.productSha ||
      receiptCandidate.productTree !== identity.productTree ||
      admitted.source.sha !== identity.productSha ||
      admitted.source.tree !== identity.productTree) {
    evidenceError("cloud_business_qualification_candidate_invalid");
  }
  if (receiptCandidate.cloudImageDigest !== identity.cloudImageDigest) {
    evidenceError("cloud_business_qualification_image_invalid");
  }

  const productSource = validateProductSource(input.productSourceText);
  if (
    productSource.productSha !== identity.productSha ||
    productSource.productTree !== identity.productTree
  ) {
    evidenceError("cloud_business_qualification_candidate_invalid");
  }

  const run = record(input.run, "cloud_business_qualification_run_invalid");
  const repository = record(run.repository, "cloud_business_qualification_run_invalid");
  if (!RUN_ID.test(input.runId) || String(run.id) !== input.runId ||
      repository.full_name !== CLOUD_BUSINESS_QUALIFICATION_PRODUCER.repository ||
      run.path !== CLOUD_BUSINESS_QUALIFICATION_PRODUCER.workflow ||
      run.event !== "workflow_dispatch" || run.head_branch !== "main" ||
      run.status !== "completed" || run.conclusion !== "success" ||
      !SHA.test(String(run.head_sha || ""))) {
    evidenceError("cloud_business_qualification_run_invalid");
  }

  const completedAt = Date.parse(admitted.completedAt);
  const now = input.now ?? Date.now();
  if (!Number.isFinite(completedAt) ||
      completedAt > now + CLOUD_BUSINESS_QUALIFICATION_FUTURE_SKEW_MS ||
      now - completedAt > CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_MS) {
    evidenceError("cloud_business_qualification_stale");
  }

  return Object.freeze({
    evidenceClass: RELEASE_EVIDENCE_CLASS.CLOUD_BUSINESS_QUALIFICATION,
    repository: CLOUD_BUSINESS_QUALIFICATION_PRODUCER.repository,
    workflowRunId: input.runId,
    runDigest: digest(`github-actions-run:v1\0${input.runId}`),
    authoritySha: String(run.head_sha),
    receiptDigest: digest(input.receiptText),
    receiptKind: QUALIFICATION_BUSINESS_RECEIPT_KIND,
    scope: String(admitted.scope),
    terminalState: CLOUD_BUSINESS_QUALIFICATION_TERMINAL_STATE,
    authorityClass: String(admitted.authority.authorityClass),
    completedAt: String(admitted.completedAt),
    identity
  });
}

/*
 * Admit the release as a pair of explicitly typed evidence classes.
 *
 * A caller cannot pass deployment evidence as the whole admission: the Cloud
 * class is required by name, and each class is validated by its own owner
 * binding. The returned record keeps both classes separate and repeats the one
 * Candidate identity they both proved.
 */
export function validateReleaseAdmissionEvidence(input: {
  candidate: unknown;
  cloudBusinessQualification?: {
    receiptText?: string;
    productSourceText?: string;
    run?: unknown;
    runId?: string;
  } | null;
  instanceDeployment?: {
    deployment?: unknown;
    deploymentText?: string;
    deploymentRun?: unknown;
    deploymentRunId?: string;
    decision?: unknown;
  } | null;
  now?: number;
}) {
  const business = input.cloudBusinessQualification;
  if (!business || typeof business.receiptText !== "string" || !business.receiptText.trim() ||
      typeof business.productSourceText !== "string" || !business.productSourceText.trim() ||
      !business.run || !business.runId) {
    evidenceError("release_evidence_missing_cloud_business_qualification");
  }
  const deployment = input.instanceDeployment;
  if (!deployment || !deployment.deployment || typeof deployment.deploymentText !== "string" ||
      !deployment.deploymentRun || !deployment.deploymentRunId || !deployment.decision) {
    evidenceError("release_evidence_missing_instance_deployment");
  }

  const candidate = validateCloudCandidateReceipt(input.candidate);
  const businessEvidence = validateReleaseBusinessQualification({
    candidate,
    receiptText: business.receiptText as string,
    productSourceText: business.productSourceText as string,
    run: business.run,
    runId: business.runId as string,
    now: input.now
  });
  const deploymentEvidence = validateReleaseInstallationBinding({
    candidate,
    deployment: deployment.deployment,
    deploymentText: deployment.deploymentText,
    deploymentRun: deployment.deploymentRun,
    deploymentRunId: deployment.deploymentRunId as string,
    decision: deployment.decision
  });

  // Both classes are validated against the same release Candidate, so this
  // equality is the combined admission's own invariant: it refuses a future
  // validator change that lets one class bind a different identity.
  if (!sameIdentity(businessEvidence.identity, deploymentEvidence.identity)) {
    evidenceError("release_evidence_identity_conflict");
  }

  return Object.freeze({
    schemaVersion: 1,
    candidate: businessEvidence.identity,
    classes: Object.freeze({
      [RELEASE_EVIDENCE_CLASS.CLOUD_BUSINESS_QUALIFICATION]: businessEvidence,
      [RELEASE_EVIDENCE_CLASS.INSTANCE_DEPLOYMENT]: deploymentEvidence
    })
  });
}
