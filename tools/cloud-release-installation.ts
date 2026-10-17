import { createHash } from "node:crypto";
import { candidateReceiptDigest, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";

const INSTANCE_REPOSITORY = "gaofeng21cn/opl-instance-medopl";
const SHA = /^[a-f0-9]{40}$/;
const RUN_ID = /^[1-9][0-9]*$/;

function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("release_installation_binding_invalid");
  }
  return value as Record<string, unknown>;
}

function digest(value: string) {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

// Native receipts must be validated by their producing revisions before this
// cross-owner binding is evaluated. Installation proves identity and health;
// the separate native qualification and Workspace gates remain mandatory.
export function validateReleaseInstallationBinding(input: {
  candidate: unknown;
  deployment: unknown;
  deploymentText: string;
  deploymentRun: unknown;
  deploymentRunId: string;
  decision: unknown;
}) {
  const candidate = validateCloudCandidateReceipt(input.candidate);
  const product = record(candidate.product);
  const image = record(candidate.cloudImage);
  const deployment = record(input.deployment);
  const run = record(input.deploymentRun);
  const decision = record(input.decision);
  const platforms = image.platforms as Array<Record<string, unknown>>;
  const amd64 = record(platforms.find((entry) => entry.platform === "linux/amd64"));
  if (!RUN_ID.test(input.deploymentRunId) || String(run.id) !== input.deploymentRunId ||
      record(run.repository).full_name !== INSTANCE_REPOSITORY || run.path !== ".github/workflows/deployment-verify.yml" ||
      run.event !== "workflow_dispatch" || run.head_branch !== "main" || run.status !== "completed" ||
      run.conclusion !== "success" || !SHA.test(String(run.head_sha || ""))) {
    throw new Error("release_installation_run_invalid");
  }
  // Hash exactly the downloaded bytes, and refuse an independently supplied
  // decoded value: the decision must reference the receipt actually validated.
  if (JSON.stringify(JSON.parse(input.deploymentText)) !== JSON.stringify(deployment)) {
    throw new Error("release_installation_bytes_invalid");
  }
  const identity = {
    candidateReceiptDigest: candidateReceiptDigest(candidate),
    productSha: product.sha,
    productTree: product.tree,
    cloudImageRef: image.indexRef,
    cloudImageDigest: image.indexDigest,
    cloudImageRevision: image.revision,
  };
  if (deployment.status !== "deployment_verified" || deployment.operationMode !== "deployment_verification" ||
      deployment.environment !== "production" || deployment.stage !== "complete" || deployment.errorCode !== "none" ||
      Object.entries(identity).some(([key, value]) => deployment[key] !== value || decision[key] !== value) ||
      deployment.cloudRuntimeChildDigest !== amd64.digest) {
    throw new Error("release_installation_candidate_invalid");
  }
  const receiptDigest = digest(input.deploymentText);
  const runDigest = digest(`github-actions-run:v1\0${input.deploymentRunId}`);
  const runtime = record(deployment.runtimeConfig);
  const scope = record(decision.qualificationScope);
  if (record(decision.deploymentVerification).receiptDigest !== receiptDigest ||
      record(decision.deploymentVerification).runDigest !== runDigest ||
      decision.workspaceImageRef !== deployment.workspaceImageRef ||
      decision.workspaceImageDigest !== deployment.workspaceImageDigest ||
      scope.workspaceImageDigest !== deployment.workspaceImageDigest ||
      scope.deploymentMode !== runtime.deploymentMode || scope.provider !== runtime.fabricProvider ||
      scope.providerProfileDigest !== runtime.providerProfileDigest ||
      scope.targetClusterDigest !== runtime.targetClusterDigest || scope.region !== runtime.region) {
    throw new Error("release_installation_qualification_binding_invalid");
  }
  return {
    evidenceClass: "instance_deployment",
    repository: INSTANCE_REPOSITORY,
    workflowRunId: input.deploymentRunId,
    receiptDigest,
    runDigest,
    authoritySha: String(run.head_sha),
    receiptStatus: "deployment_verified",
    identity: {
      candidateReceiptDigest: String(identity.candidateReceiptDigest),
      productSha: String(product.sha),
      productTree: String(product.tree),
      cloudImageDigest: String(image.indexDigest)
    }
  };
}
