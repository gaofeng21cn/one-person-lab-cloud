import { createHash } from "node:crypto";
import { candidateInstallationIdentity } from "./cloud-candidate-receipt.ts";

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

// The producing Instance revision validates its native receipt first. This
// binding proves installation identity and health, independently of purchases.
export function validateReleaseInstallationReadback(input: {
  candidate: unknown;
  deployment: unknown;
  deploymentText: string;
  deploymentRun: unknown;
  deploymentRunId: string;
}) {
  const { cloudRuntimeChildDigest, ...identity } = candidateInstallationIdentity(input.candidate);
  const deployment = record(input.deployment);
  const run = record(input.deploymentRun);
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
  if (deployment.status !== "deployment_verified" || deployment.operationMode !== "deployment_verification" ||
      deployment.environment !== "production" || deployment.stage !== "complete" || deployment.errorCode !== "none" ||
      Object.entries(identity).some(([key, value]) => deployment[key] !== value) ||
      deployment.cloudRuntimeChildDigest !== cloudRuntimeChildDigest) {
    throw new Error("release_installation_candidate_invalid");
  }
  const receiptDigest = digest(input.deploymentText);
  const runDigest = digest(`github-actions-run:v1\0${input.deploymentRunId}`);
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
      productSha: identity.productSha,
      productTree: identity.productTree,
      cloudImageDigest: identity.cloudImageDigest
    }
  };
}

// Retained full business qualification consumers additionally bind their own
// decision and Workspace scope. They do not substitute for installation proof.
export function validateReleaseInstallationBinding(input: Parameters<typeof validateReleaseInstallationReadback>[0] & { decision: unknown }) {
  const installation = validateReleaseInstallationReadback(input);
  const deployment = record(input.deployment);
  const decision = record(input.decision);
  const identity = Object.entries(candidateInstallationIdentity(input.candidate)).filter(([key]) => key !== "cloudRuntimeChildDigest");
  if (identity.some(([key, value]) => decision[key] !== value)) {
    throw new Error("release_installation_candidate_invalid");
  }
  const { receiptDigest, runDigest } = installation;
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
  return installation;
}
