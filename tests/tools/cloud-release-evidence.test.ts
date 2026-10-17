import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import {
  CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_MS,
  CLOUD_BUSINESS_QUALIFICATION_PRODUCER,
  RELEASE_EVIDENCE_CLASS,
  validateReleaseAdmissionEvidence,
  validateReleaseBusinessQualification
} from "../../tools/cloud-release-evidence.ts";

const productRepository = "gaofeng21cn/one-person-lab-cloud";
const instanceRepository = "gaofeng21cn/opl-instance-medopl";
const productSha = "a".repeat(40);
const productTree = "b".repeat(40);
const instanceSha = "f".repeat(40);
const hash = (text: string) => `sha256:${createHash("sha256").update(text).digest("hex")}`;
const digest = (seed: string) => `sha256:${seed.repeat(64)}`;
const now = Date.parse("2026-10-18T00:00:00Z");

// This test exercises untrusted JSON at the release admission boundary, after
// the #923 contract and the Instance producer have validated their own
// receipts. It defines no substitute domain DTO.
async function evidenceInput() {
  const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-candidate-receipt-contract.json", "utf8"));
  const candidate = {
    schemaVersion: 2,
    kind: "opl_cloud_candidate",
    product: { repository: productRepository, sha: productSha, tree: productTree },
    cloudImage: {
      repository: `ghcr.io/${productRepository}`,
      indexRef: `ghcr.io/${productRepository}@${digest("c")}`,
      indexDigest: digest("c"),
      revision: productSha,
      platforms: [{ platform: "linux/amd64", digest: digest("d") }, { platform: "linux/arm64", digest: digest("e") }]
    },
    assets: contract.candidateReceiptV2.portableAssets.map((name: string) => ({ name, sha256: "1".repeat(64) })),
    provenance: {
      workflowRepository: productRepository,
      workflowSha: productSha,
      workflowRunId: "11",
      workflowRunAttempt: "1"
    }
  };
  const candidateDigest = candidateReceiptDigest(candidate);
  const identity = {
    candidateReceiptDigest: candidateDigest,
    productSha,
    productTree,
    cloudImageRef: candidate.cloudImage.indexRef,
    cloudImageDigest: candidate.cloudImage.indexDigest,
    cloudImageRevision: productSha,
    workspaceImageRef: `registry.example/workspace@${digest("2")}`,
    workspaceImageDigest: digest("2")
  };
  const receipt = {
    schemaVersion: 1,
    kind: "opl.workspace-qualification.purchase.v1",
    scope: "workspace_purchase",
    uncovered: {
      applicationInstallation: false,
      gatewayKeyLifecycle: false,
      keyUsage: false,
      ownerDeletion: false,
      businessQualification: false
    },
    state: "settled",
    reason: "none",
    provider: "local-docker",
    candidate: {
      receiptDigest: candidateDigest,
      productSha,
      productTree,
      cloudImageDigest: candidate.cloudImage.indexDigest
    },
    source: { sha: productSha, tree: productTree, clean: true },
    tenant: { accountDigest: digest("3") },
    product: { resourceType: "workspace", packageId: "basic", autoRenew: false },
    authority: { mode: "live", authorityClass: "sandbox", j0ReadyDigest: digest("4") },
    operation: {
      idempotencyKey: "opl-qualification:abc",
      operationId: "op-1",
      workspaceId: "ws-1",
      purchaseReceiptId: "receipt-1",
      resumed: false
    },
    mutations: {
      declared: { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 },
      observed: { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 }
    },
    checks: {
      account_bound: true,
      quote_priced: true,
      purchase_issued: true,
      purchase_receipt_bound: true,
      workspace_ready: true,
      runtime_ready: true,
      charge_receipted: true,
      wallet_confirmed: true
    },
    accounting: {
      quoteUsdMicros: "1000000",
      walletBeforeUsdMicros: "5000000",
      walletAfterUsdMicros: "4000000",
      chargeReferenceDigest: digest("5")
    },
    delivery: {
      workspaceEntryDigest: digest("6"),
      applicationOperationDigest: digest("7"),
      componentsObserved: 1
    },
    cleanup: { performed: true, restored: true },
    deploymentHealth: "healthy",
    startedAt: "2026-10-17T00:00:00Z",
    completedAt: "2026-10-17T00:05:00Z"
  };
  const businessRun = {
    id: 7001,
    repository: { full_name: productRepository },
    path: CLOUD_BUSINESS_QUALIFICATION_PRODUCER.workflow,
    event: "workflow_dispatch",
    head_branch: "main",
    status: "completed",
    conclusion: "success",
    head_sha: productSha
  };
  const deployment = {
    schemaVersion: 2,
    operationMode: "deployment_verification",
    environment: "production",
    status: "deployment_verified",
    stage: "complete",
    errorCode: "none",
    ...identity,
    cloudRuntimeChildDigest: digest("d"),
    runtimeConfig: {
      deploymentMode: "managed_tke",
      fabricProvider: "tencent-tke",
      providerProfileDigest: digest("8"),
      targetClusterDigest: digest("9"),
      region: "na-siliconvalley"
    }
  };
  const deploymentText = `${JSON.stringify(deployment)}\n`;
  const deploymentRunId = "22";
  const decision = {
    ...identity,
    deploymentVerification: {
      receiptDigest: hash(deploymentText),
      runDigest: hash(`github-actions-run:v1\0${deploymentRunId}`)
    },
    qualificationScope: {
      deploymentMode: "managed_tke",
      provider: "tencent-tke",
      providerProfileDigest: digest("8"),
      targetClusterDigest: digest("9"),
      region: "na-siliconvalley",
      workspaceImageDigest: identity.workspaceImageDigest
    }
  };
  const deploymentRun = {
    id: Number(deploymentRunId),
    repository: { full_name: instanceRepository },
    path: ".github/workflows/deployment-verify.yml",
    event: "workflow_dispatch",
    head_branch: "main",
    status: "completed",
    conclusion: "success",
    head_sha: instanceSha
  };
  return {
    candidate,
    receipt,
    businessRun,
    now,
    cloudBusinessQualification: {
      receiptText: `${JSON.stringify(receipt)}\n`,
      run: businessRun,
      runId: String(businessRun.id)
    },
    instanceDeployment: { deployment, deploymentText, deploymentRun, deploymentRunId, decision }
  };
}

function rewriteBusinessReceipt(input: Awaited<ReturnType<typeof evidenceInput>>, mutate: (receipt: any) => void) {
  const receipt = structuredClone(input.receipt);
  mutate(receipt);
  input.cloudBusinessQualification.receiptText = `${JSON.stringify(receipt)}\n`;
  return input;
}

test("release admission admits a settled Cloud business receipt beside an Instance deployment receipt", async () => {
  const input = await evidenceInput();
  const admitted = validateReleaseAdmissionEvidence(input);

  assert.deepEqual(Object.keys(admitted.classes), [
    RELEASE_EVIDENCE_CLASS.CLOUD_BUSINESS_QUALIFICATION,
    RELEASE_EVIDENCE_CLASS.INSTANCE_DEPLOYMENT
  ]);
  const business = admitted.classes[RELEASE_EVIDENCE_CLASS.CLOUD_BUSINESS_QUALIFICATION];
  const deployment = admitted.classes[RELEASE_EVIDENCE_CLASS.INSTANCE_DEPLOYMENT];
  assert.equal(business.evidenceClass, "cloud_business_qualification");
  assert.equal(business.receiptKind, "opl.workspace-qualification.purchase.v1");
  assert.equal(business.scope, "workspace_purchase");
  assert.equal(business.terminalState, "settled");
  assert.equal(business.authorityClass, "sandbox");
  assert.equal(business.receiptDigest, hash(input.cloudBusinessQualification.receiptText));
  assert.equal(business.workflowRunId, "7001");
  assert.equal(business.repository, productRepository);
  assert.equal(deployment.receiptDigest, hash(input.instanceDeployment.deploymentText));
  assert.equal(deployment.workflowRunId, "22");
  // The two classes hash different artifacts and prove different things.
  assert.notEqual(business.receiptDigest, deployment.receiptDigest);
  // Both bind the same immutable Candidate identity.
  assert.deepEqual(business.identity, deployment.identity);
  assert.deepEqual(admitted.candidate, {
    candidateReceiptDigest: candidateReceiptDigest(input.candidate),
    productSha,
    productTree,
    cloudImageDigest: input.candidate.cloudImage.indexDigest
  });
});

test("release admission refuses a deployment-only bundle that omits the Cloud business class", async () => {
  const input = await evidenceInput();
  assert.throws(
    () => validateReleaseAdmissionEvidence({ ...input, cloudBusinessQualification: null }),
    /release_evidence_missing_cloud_business_qualification/
  );
  assert.throws(
    () => validateReleaseAdmissionEvidence({
      ...input,
      cloudBusinessQualification: { receiptText: "", run: input.businessRun, runId: "7001" }
    }),
    /release_evidence_missing_cloud_business_qualification/
  );
  // A valid deployment receipt alone is still not a business qualification.
  assert.throws(
    () => validateReleaseBusinessQualification({
      candidate: input.candidate,
      receiptText: input.instanceDeployment.deploymentText,
      run: input.instanceDeployment.deploymentRun,
      runId: input.instanceDeployment.deploymentRunId
    }),
    /cloud_business_qualification_receipt_invalid/
  );
  assert.throws(
    () => validateReleaseAdmissionEvidence({ ...input, instanceDeployment: null }),
    /release_evidence_missing_instance_deployment/
  );
});

test("release admission refuses a business receipt bound to another Candidate digest or source", async () => {
  // The #923 contract already keeps source SHA/tree equal to the receipt's own
  // Candidate. These patches stay internally consistent, so the refusal comes
  // from the release-side binding and not from the receipt shape.
  for (const mutate of [
    (receipt: any) => { receipt.candidate.receiptDigest = digest("a"); },
    (receipt: any) => { receipt.candidate.productSha = "f".repeat(40); receipt.source.sha = "f".repeat(40); },
    (receipt: any) => { receipt.candidate.productTree = "f".repeat(40); receipt.source.tree = "f".repeat(40); }
  ]) {
    const input = await evidenceInput();
    rewriteBusinessReceipt(input, mutate);
    assert.throws(
      () => validateReleaseAdmissionEvidence(input),
      /cloud_business_qualification_candidate_invalid/
    );
  }
});

test("release admission refuses a business receipt bound to another image digest", async () => {
  const input = await evidenceInput();
  rewriteBusinessReceipt(input, (receipt) => { receipt.candidate.cloudImageDigest = digest("f"); });
  assert.throws(
    () => validateReleaseAdmissionEvidence(input),
    /cloud_business_qualification_image_invalid/
  );
});

test("release admission refuses stale, future-dated and non-settled business receipts", async () => {
  const stale = await evidenceInput();
  rewriteBusinessReceipt(stale, (receipt) => {
    receipt.completedAt = new Date(now - CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_MS - 60_000).toISOString();
    receipt.startedAt = new Date(now - CLOUD_BUSINESS_QUALIFICATION_MAX_AGE_MS - 120_000).toISOString();
  });
  assert.throws(() => validateReleaseAdmissionEvidence(stale), /cloud_business_qualification_stale/);

  const future = await evidenceInput();
  rewriteBusinessReceipt(future, (receipt) => {
    receipt.completedAt = new Date(now + 10 * 60_000).toISOString();
    receipt.startedAt = new Date(now + 9 * 60_000).toISOString();
  });
  assert.throws(() => validateReleaseAdmissionEvidence(future), /cloud_business_qualification_stale/);

  for (const [state, reason] of [
    ["unknown", "response_lost"],
    ["blocked", "provider_scope_unavailable"],
    ["failed", "quote_invalid"],
    ["recovery-required", "mutation_unaccounted"]
  ]) {
    const input = await evidenceInput();
    rewriteBusinessReceipt(input, (receipt) => {
      receipt.state = state;
      receipt.reason = reason;
      receipt.checks.quote_priced = false;
      receipt.accounting.quoteUsdMicros = null;
    });
    assert.throws(
      () => validateReleaseAdmissionEvidence(input),
      /cloud_business_qualification_terminal_state_invalid/,
      `${state} is a valid contract state but never a release-qualifying terminal state`
    );
  }

  const unknownState = await evidenceInput();
  rewriteBusinessReceipt(unknownState, (receipt) => { receipt.state = "ready"; });
  assert.throws(() => validateReleaseAdmissionEvidence(unknownState), /cloud_business_qualification_receipt_invalid/);
});

test("release admission refuses evidence of one class presented in place of the other", async () => {
  const input = await evidenceInput();
  assert.throws(
    () => validateReleaseAdmissionEvidence({
      ...input,
      cloudBusinessQualification: {
        receiptText: input.instanceDeployment.deploymentText,
        run: input.instanceDeployment.deploymentRun,
        runId: input.instanceDeployment.deploymentRunId
      }
    }),
    /cloud_business_qualification_receipt_invalid/
  );
  assert.throws(
    () => validateReleaseAdmissionEvidence({
      ...input,
      instanceDeployment: {
        deployment: input.receipt,
        deploymentText: input.cloudBusinessQualification.receiptText,
        deploymentRun: input.businessRun,
        deploymentRunId: "7001",
        decision: input.receipt
      }
    }),
    /release_installation_run_invalid/
  );
});

test("release admission refuses business evidence from another run, workflow or source commit", async () => {
  const input = await evidenceInput();
  for (const patch of [
    { id: 7002 },
    { repository: { full_name: instanceRepository } },
    { path: ".github/workflows/release-opl-cloud-image.yml" },
    { event: "push" },
    { head_branch: "codex/unmerged" },
    { head_sha: "f".repeat(40) },
    { status: "in_progress" },
    { conclusion: "failure" }
  ] as Array<Record<string, unknown>>) {
    input.cloudBusinessQualification.run = { ...input.businessRun, ...patch } as typeof input.businessRun;
    assert.throws(
      () => validateReleaseAdmissionEvidence(input),
      /cloud_business_qualification_run_invalid/,
      JSON.stringify(patch)
    );
  }
});

test("release admission refuses a receipt that carries an undeclared customer or Key field", async () => {
  const input = await evidenceInput();
  rewriteBusinessReceipt(input, (receipt) => { receipt.customerEmail = "customer@example.invalid"; });
  assert.throws(
    () => validateReleaseAdmissionEvidence(input),
    /cloud_business_qualification_receipt_invalid/
  );

  const keyed = await evidenceInput();
  rewriteBusinessReceipt(keyed, (receipt) => { receipt.operation.gatewayKey = "raw-key"; });
  assert.throws(
    () => validateReleaseAdmissionEvidence(keyed),
    /cloud_business_qualification_receipt_invalid/
  );
});
