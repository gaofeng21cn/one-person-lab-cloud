import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import { validateReleaseInstallationBinding } from "../../tools/cloud-release-installation.ts";

const hash = (text: string) => `sha256:${createHash("sha256").update(text).digest("hex")}`;
const digest = (seed: string) => `sha256:${seed.repeat(64)}`;
const productSha = "a".repeat(40);
const instanceSha = "f".repeat(40);

// This test exercises an untrusted JSON cross-owner binding, after each native
// producer has validated its own receipt. It defines no substitute domain DTO.
async function bindingInput() {
  const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-candidate-receipt-contract.json", "utf8"));
  const candidate = {
    schemaVersion: 2, kind: "opl_cloud_candidate",
    product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: productSha, tree: "b".repeat(40) },
    cloudImage: {
      repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
      indexRef: `ghcr.io/gaofeng21cn/one-person-lab-cloud@${digest("c")}`,
      indexDigest: digest("c"), revision: productSha,
      platforms: [{ platform: "linux/amd64", digest: digest("d") }, { platform: "linux/arm64", digest: digest("e") }],
    },
    assets: contract.candidateReceiptV2.portableAssets.map((name: string) => ({ name, sha256: "1".repeat(64) })),
    provenance: { workflowRepository: "gaofeng21cn/one-person-lab-cloud", workflowSha: productSha, workflowRunId: "11", workflowRunAttempt: "1" },
  };
  const identity = {
    candidateReceiptDigest: candidateReceiptDigest(candidate), productSha, productTree: candidate.product.tree,
    cloudImageRef: candidate.cloudImage.indexRef, cloudImageDigest: candidate.cloudImage.indexDigest, cloudImageRevision: productSha,
    workspaceImageRef: `registry.example/workspace@${digest("2")}`, workspaceImageDigest: digest("2"),
  };
  const deployment = {
    schemaVersion: 2, operationMode: "deployment_verification", environment: "production",
    status: "deployment_verified", stage: "complete", errorCode: "none", ...identity,
    cloudRuntimeChildDigest: digest("d"),
    runtimeConfig: {
      deploymentMode: "managed_tke", fabricProvider: "tencent-tke", providerProfileDigest: digest("3"),
      targetClusterDigest: digest("4"), region: "na-siliconvalley",
    },
  };
  const deploymentText = `${JSON.stringify(deployment)}\n`;
  const deploymentRunId = "22";
  const decision = {
    ...identity,
    deploymentVerification: { receiptDigest: hash(deploymentText), runDigest: hash(`github-actions-run:v1\0${deploymentRunId}`) },
    qualificationScope: {
      deploymentMode: "managed_tke", provider: "tencent-tke", providerProfileDigest: digest("3"),
      targetClusterDigest: digest("4"), region: "na-siliconvalley", workspaceImageDigest: identity.workspaceImageDigest,
    },
  };
  const deploymentRun = {
    id: Number(deploymentRunId), repository: { full_name: "gaofeng21cn/opl-instance-medopl" },
    path: ".github/workflows/deployment-verify.yml", event: "workflow_dispatch", head_branch: "main",
    status: "completed", conclusion: "success", head_sha: instanceSha,
  };
  return { candidate, deployment, deploymentText, deploymentRun, deploymentRunId, decision };
}

test("installation binding keeps native installation evidence distinct from business qualification", async () => {
  const input = await bindingInput();
  assert.deepEqual(validateReleaseInstallationBinding(input), {
    evidenceClass: "instance_deployment", repository: "gaofeng21cn/opl-instance-medopl",
    workflowRunId: input.deploymentRunId, receiptStatus: "deployment_verified",
    receiptDigest: hash(input.deploymentText), runDigest: input.decision.deploymentVerification.runDigest, authoritySha: instanceSha,
    identity: {
      candidateReceiptDigest: candidateReceiptDigest(input.candidate),
      productSha,
      productTree: input.candidate.product.tree,
      cloudImageDigest: input.candidate.cloudImage.indexDigest,
    },
  });
});

test("installation binding refuses unproven, stale and wrong-owner workflow provenance", async () => {
  for (const patch of [
    { id: 23 }, { repository: { full_name: "gaofeng21cn/one-person-lab-cloud" } },
    { path: ".github/workflows/deploy.yml" }, { path: ".github/workflows/verify.yml" },
    { head_branch: "codex/unmerged" }, { head_sha: "unknown" },
    { event: "pull_request" }, { status: "in_progress" }, { conclusion: "failure" },
  ]) {
    const input = await bindingInput();
    assert.throws(() => validateReleaseInstallationBinding({ ...input, deploymentRun: { ...input.deploymentRun, ...patch } }),
      /release_installation_run_invalid/);
  }
});

test("installation binding refuses a different Candidate, child image, unknown health and substituted receipt bytes", async () => {
  for (const patch of [
    { candidateReceiptDigest: digest("5") }, { productSha: "f".repeat(40) }, { productTree: "f".repeat(40) },
    { cloudImageRef: `registry.example/other@${digest("c")}` }, { cloudImageDigest: digest("6") },
    { cloudImageRevision: "f".repeat(40) }, { cloudRuntimeChildDigest: digest("e") },
    { status: "blocked" }, { status: "workspace_verified" }, { errorCode: "unknown" },
  ]) {
    const input = await bindingInput();
    const deployment = { ...input.deployment, ...patch };
    assert.throws(() => validateReleaseInstallationBinding({ ...input, deployment, deploymentText: JSON.stringify(deployment) }),
      /release_installation_candidate_invalid/);
  }
  const input = await bindingInput();
  assert.throws(() => validateReleaseInstallationBinding({ ...input, deploymentText: "{}" }), /release_installation_bytes_invalid/);
});

test("a native qualification decision cannot substitute an unbound installation, Workspace or provider scope", async () => {
  const input = await bindingInput();
  for (const decision of [
    { ...input.decision, deploymentVerification: { ...input.decision.deploymentVerification, receiptDigest: digest("7") } },
    { ...input.decision, deploymentVerification: { ...input.decision.deploymentVerification, runDigest: digest("7") } },
    { ...input.decision, workspaceImageRef: `registry.example/other@${digest("2")}` },
    { ...input.decision, workspaceImageDigest: digest("7") },
    ...Object.keys(input.decision.qualificationScope).map((key) => ({
      ...input.decision, qualificationScope: { ...input.decision.qualificationScope, [key]: "unknown" },
    })),
  ]) {
    assert.throws(() => validateReleaseInstallationBinding({ ...input, decision }), /release_installation_qualification_binding_invalid/);
  }
  // Reformatting is valid JSON but changes the native receipt's byte digest.
  assert.throws(() => validateReleaseInstallationBinding({ ...input, deploymentText: JSON.stringify(input.deployment, null, 2) }),
    /release_installation_qualification_binding_invalid/);
});
