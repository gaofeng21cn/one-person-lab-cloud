import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, stat, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";

import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import {
  DEPLOYED_PURCHASE_RECEIPT_KIND,
  deployedAccountIdentityDigest,
  validateDeployedPurchaseReceipt,
} from "../../tools/deployed-workspace-purchase.ts";
import {
  J0_READY_MAX_BYTES,
  PROTECTED_BOUNDARY_FACTORY,
  PROTECTED_CUSTOMER_EMAIL,
  PROTECTED_CUSTOMER_PASSWORD,
  PROTECTED_ENVIRONMENT_NAME,
  PROTECTED_ORIGIN,
  PROTECTED_QUALIFICATION_ENVIRONMENT,
  PROTECTED_QUALIFICATION_SCOPE,
  assertProtectedReceiptBinding,
  buildProtectedQualificationRequest,
  loadProtectedJ0ReadyReceipt,
  loadProtectedProviderBoundary,
  main,
  parseProtectedQualificationArgs,
  protectedBusinessQualificationSummary,
  protectedQualificationPrerequisites,
  readProtectedCandidate,
  readProtectedDeployment,
  readProtectedInstallation,
  runProtectedBusinessQualification,
  selectedQualificationProvider,
} from "../../tools/protected-business-qualification.ts";
import {
  QUALIFICATION_BUSINESS_RECEIPT_KIND,
  QUALIFICATION_BUSINESS_SCOPE,
  QUALIFICATION_TERMINAL_REASONS,
  QUALIFICATION_UNCOVERED_SCOPE,
  validateBusinessQualificationReceipt,
} from "../../tools/workspace-qualification-contract.ts";
import {
  ACCOUNT_EMAIL,
  ACCOUNT_ID,
  CLOUD_IMAGE_DIGEST,
  CLUSTER_ID,
  NAMESPACE,
  OWNER_COMPUTE_PLAN_ID,
  OWNER_ORIGIN,
  OWNER_RUNTIME_VERSION_ID,
  OWNER_STORAGE_PLAN_ID,
  QUOTE_MICROS,
  candidateReceipt,
  createFixtureBoundary,
  createFixturePlatform,
  createRecordingBoundary,
  deploymentBinding,
  installationReceipt,
  installationRun,
  j0ReadyReceipt,
  protectedEnvironment,
} from "../helpers/protected-qualification-fixture.ts";

// The owner mints the operation and the Workspace identities, and the public
// contract publishes them as strings without a shape. These assertions hold
// that the evidence names an identity the owner minted, not one this buyer
// could have derived, without pinning how the owner spells it.
const OWNER_MINTED_IDENTITY = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/;

const REPOSITORY_ROOT = resolve(".");
const FIXTURE_HELPER_URL = pathToFileURL(resolve("tests/helpers/protected-qualification-fixture.ts")).href;

function candidateFixture() {
  const candidate = candidateReceipt();
  const digest = candidateReceiptDigest(candidate);
  return {
    candidate,
    digest,
    candidateText: JSON.stringify(candidate),
    installationText: `${JSON.stringify(installationReceipt(candidate))}\n`,
    installationRunText: JSON.stringify(installationRun()),
    deploymentText: JSON.stringify(deploymentBinding(digest)),
    j0Text: JSON.stringify(j0ReadyReceipt()),
  };
}

async function writeFixture(root: string) {
  const fixture = candidateFixture();
  const paths = {
    candidatePath: join(root, "candidate.json"),
    installationPath: join(root, "installation.json"),
    installationRunPath: join(root, "installation-run.json"),
    deploymentPath: join(root, "deployment.json"),
    j0ReadyReceiptPath: join(root, "j0-ready.json"),
    boundaryModulePath: join(root, "provider-boundary.ts"),
    outputPath: join(root, "evidence", "receipt.json"),
  };
  await writeFile(paths.candidatePath, fixture.candidateText, { mode: 0o600 });
  await writeFile(paths.installationPath, fixture.installationText, { mode: 0o600 });
  await writeFile(paths.installationRunPath, fixture.installationRunText, { mode: 0o600 });
  await writeFile(paths.deploymentPath, fixture.deploymentText, { mode: 0o600 });
  await writeFile(paths.j0ReadyReceiptPath, fixture.j0Text, { mode: 0o600 });
  await chmod(paths.j0ReadyReceiptPath, 0o600);
  await writeFile(
    paths.boundaryModulePath,
    `import { createFixtureBoundary, createFixturePlatform } from ${JSON.stringify(FIXTURE_HELPER_URL)};\n` +
      "export const platform = createFixturePlatform();\n" +
      `export function ${PROTECTED_BOUNDARY_FACTORY}({ deployment }) {\n` +
      "  return createFixtureBoundary(deployment, platform);\n" +
      "}\n",
  );
  return { ...fixture, paths };
}


// A second, fully valid Candidate. It is used only to ask the producer whether
// this run's evidence can be presented as another Candidate's.
function otherCandidateFixture() {
  const base = candidateReceipt();
  const sha = "9".repeat(40);
  const tree = "8".repeat(40);
  const indexDigest = `sha256:${"5".repeat(64)}`;
  return candidateReceipt({
    product: { ...base.product, sha, tree },
    cloudImage: {
      ...base.cloudImage,
      indexRef: `${base.cloudImage.repository}@${indexDigest}`,
      indexDigest,
      revision: sha,
      platforms: [
        { platform: "linux/amd64", digest: `sha256:${"6".repeat(64)}` },
        { platform: "linux/arm64", digest: `sha256:${"7".repeat(64)}` },
      ],
    },
  });
}

function input(paths: Awaited<ReturnType<typeof writeFixture>>["paths"], digest: string) {
  return {
    candidatePath: paths.candidatePath,
    candidateReceiptDigest: digest,
    installationPath: paths.installationPath,
    installationRunPath: paths.installationRunPath,
    deploymentPath: paths.deploymentPath,
    j0ReadyReceiptPath: paths.j0ReadyReceiptPath,
    boundaryModulePath: paths.boundaryModulePath,
    accountId: ACCOUNT_ID,
    runtimeVersionId: OWNER_RUNTIME_VERSION_ID,
    computePlanId: OWNER_COMPUTE_PLAN_ID,
    storagePlanId: OWNER_STORAGE_PLAN_ID,
    authorityClass: "sandbox" as const,
    outputPath: paths.outputPath,
  };
}

test("the protected representation fails closed when the environment or a secret is absent", () => {
  const valid = protectedEnvironment();
  assert.deepEqual(protectedQualificationPrerequisites(valid), {
    environment: PROTECTED_QUALIFICATION_ENVIRONMENT,
    origin: OWNER_ORIGIN,
    customerEmail: ACCOUNT_EMAIL,
  });
  for (const name of [
    PROTECTED_ENVIRONMENT_NAME,
    PROTECTED_ORIGIN,
    PROTECTED_CUSTOMER_EMAIL,
    PROTECTED_CUSTOMER_PASSWORD,
  ]) {
    const missing = protectedEnvironment({ [name]: "" });
    assert.throws(
      () => protectedQualificationPrerequisites(missing),
      (error: any) => error.code === "external_prerequisite_absent" &&
        error.message === `external prerequisite absent: ${name}`,
      name,
    );
  }
  // The Instance-hosted context authorizes no cross-repository read, so the
  // producer declares no evidence token. The value is ignored when a caller
  // still exports it, rather than being required and never used.
  assert.deepEqual(
    protectedQualificationPrerequisites(protectedEnvironment({ OPL_INSTANCE_EVIDENCE_TOKEN: "" })),
    protectedQualificationPrerequisites(valid),
  );
  assert.throws(
    () => protectedQualificationPrerequisites(protectedEnvironment({ [PROTECTED_ENVIRONMENT_NAME]: "another-environment" })),
    (error: any) => error.code === "external_prerequisite_absent" && error.message.includes(PROTECTED_ENVIRONMENT_NAME),
  );
  for (const origin of ["http://cloud.example.com/path", "https://user:password@cloud.example.com", "https://cloud.example.com?x=1", "not-an-origin"]) {
    assert.throws(
      () => protectedQualificationPrerequisites(protectedEnvironment({ [PROTECTED_ORIGIN]: origin })),
      (error: any) => error.code === "protected_business_qualification_invalid",
      origin,
    );
  }
});

test("the run arguments name every declaration and refuse an unknown, duplicate or incomplete set", () => {
  const args = [
    "run",
    "--candidate", "/candidate.json",
    "--candidate-digest", `sha256:${"a".repeat(64)}`,
    "--installation", "/installation.json",
    "--installation-run", "/installation-run.json",
    "--deployment", "/deployment.json",
    "--j0-ready-receipt", "/j0-ready.json",
    "--boundary", "/boundary.mjs",
    "--account-id", ACCOUNT_ID,
    "--runtime-version-id", OWNER_RUNTIME_VERSION_ID,
    "--compute-plan-id", OWNER_COMPUTE_PLAN_ID,
    "--storage-plan-id", OWNER_STORAGE_PLAN_ID,
    "--authority-class", "sandbox",
    "--output", "/receipt.json",
  ];
  const parsed = parseProtectedQualificationArgs(args);
  assert.equal(parsed.authorityClass, "sandbox");
  assert.equal(parsed.accountId, ACCOUNT_ID);
  for (const broken of [
    ["run", ...args.slice(1, -1)],
    ["run", ...args, "--unknown", "x"],
    ["run", ...args, "--account-id", ACCOUNT_ID],
    ["run", ...args.slice(0, -1), ""],
    args.map((value) => value === "sandbox" ? "production" : value),
  ]) {
    assert.throws(
      () => parseProtectedQualificationArgs(broken),
      (error: any) => error.code === "protected_business_qualification_invalid",
    );
  }
});

test("the Candidate digest is derived, checked against the declaration and never trusted from the deployment", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-candidate-"));
  try {
    const fixture = await writeFixture(root);
    const loaded = await readProtectedCandidate(fixture.paths.candidatePath, fixture.digest);
    assert.equal(loaded.digest, fixture.digest);
    assert.equal(candidateReceiptDigest(loaded.candidate), fixture.digest);
    await assert.rejects(
      readProtectedCandidate(fixture.paths.candidatePath, `sha256:${"0".repeat(64)}`),
      (error: any) => error.code === "protected_business_qualification_invalid" &&
        /does not match the declared digest/.test(error.message),
    );
    await writeFile(fixture.paths.candidatePath, "{", { mode: 0o600 });
    await assert.rejects(
      readProtectedCandidate(fixture.paths.candidatePath, fixture.digest),
      (error: any) => error.code === "protected_business_qualification_invalid",
    );
    await assert.rejects(
      readProtectedCandidate(join(root, "absent.json"), fixture.digest),
      (error: any) => error.code === "external_prerequisite_absent",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the deployment binding is exact, Candidate-bound and pinned to the protected origin", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-deployment-"));
  try {
    const fixture = await writeFixture(root);
    const deployment = await readProtectedDeployment(fixture.paths.deploymentPath, fixture.digest, OWNER_ORIGIN);
    assert.equal(deployment.deploymentMode, "managed_tke");
    assert.equal(deployment.candidateReceiptDigest, fixture.digest);
    assert.equal(deployment.ownerOrigin, OWNER_ORIGIN);
    assert.equal(deployment.clusterId, CLUSTER_ID);
    assert.equal(deployment.namespace, NAMESPACE);
    for (const patch of [
      { candidateReceiptDigest: `sha256:${"1".repeat(64)}` },
      { ownerOrigin: "https://another.example.invalid" },
      { fabricProvider: "local-docker" },
      { deploymentMode: "customer_owned" },
      { namespace: "Not A Namespace" },
      { extra: "declaration" },
    ]) {
      await writeFile(fixture.paths.deploymentPath, JSON.stringify({ ...deploymentBinding(fixture.digest), ...patch }), { mode: 0o600 });
      await assert.rejects(
        readProtectedDeployment(fixture.paths.deploymentPath, fixture.digest, OWNER_ORIGIN),
        (error: any) => error.code === "protected_business_qualification_invalid",
        JSON.stringify(patch),
      );
    }
    await writeFile(fixture.paths.deploymentPath, "{", { mode: 0o600 });
    await assert.rejects(
      readProtectedDeployment(fixture.paths.deploymentPath, fixture.digest, OWNER_ORIGIN),
      (error: any) => error.code === "protected_business_qualification_invalid",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the original Instance deployment evidence must bind the exact Candidate and run", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-installation-"));
  try {
    const fixture = await writeFixture(root);
    const installation = await readProtectedInstallation(
      fixture.paths.installationPath,
      fixture.paths.installationRunPath,
      fixture.candidate,
      fixture.digest,
    );
    assert.equal(installation.run.repository.full_name, "gaofeng21cn/opl-instance-medopl");
    assert.equal(
      installation.identity.receiptDigest,
      `sha256:${createHash("sha256").update(fixture.installationText).digest("hex")}`,
    );
    const receipt = JSON.parse(fixture.installationText);
    for (const patch of [
      { candidateReceiptDigest: `sha256:${"1".repeat(64)}` },
      { status: "failed" },
      { cloudRuntimeChildDigest: `sha256:${"e".repeat(64)}` },
      { productTree: "0".repeat(40) },
    ]) {
      await writeFile(fixture.paths.installationPath, `${JSON.stringify({ ...receipt, ...patch })}\n`, { mode: 0o600 });
      await assert.rejects(
        readProtectedInstallation(fixture.paths.installationPath, fixture.paths.installationRunPath, fixture.candidate, fixture.digest),
        (error: any) => error.code === "protected_business_qualification_invalid",
        JSON.stringify(patch),
      );
    }
    await writeFile(fixture.paths.installationPath, fixture.installationText, { mode: 0o600 });
    await writeFile(fixture.paths.installationRunPath, JSON.stringify({ ...installationRun(), conclusion: "failure" }), { mode: 0o600 });
    await assert.rejects(
      readProtectedInstallation(fixture.paths.installationPath, fixture.paths.installationRunPath, fixture.candidate, fixture.digest),
      (error: any) => error.code === "protected_business_qualification_invalid",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

function loadJ0(path: string) {
  return loadProtectedJ0ReadyReceipt(path, {
    sourceSha: j0ReadyReceipt().source.sha,
    sourceTree: j0ReadyReceipt().source.tree,
    cloudImageDigest: CLOUD_IMAGE_DIGEST,
    provider: "tencent-tke",
    repositoryRoot: REPOSITORY_ROOT,
  });
}

test("the external J0 receipt is admitted only from the exact protected file", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-j0-"));
  try {
    const fixture = await writeFixture(root);
    const receipt = await loadJ0(fixture.paths.j0ReadyReceiptPath);
    assert.equal(receipt.provider, "tencent-tke");
    assert.equal(receipt.status, "READY");
    assert.equal(receipt.source.cloudImageDigest, CLOUD_IMAGE_DIGEST);
    await assert.rejects(
      loadJ0(join(root, "absent.json")),
      (error: any) => error.code === "external_prerequisite_absent" &&
        /J0 READY producer receipt/.test(error.message),
    );
    // A symlink is not followed, so it is an absent prerequisite rather than a
    // receipt read through a path the producer did not create.
    const link = join(root, "j0-ready-link.json");
    await symlink(fixture.paths.j0ReadyReceiptPath, link);
    await assert.rejects(
      loadJ0(link),
      (error: any) => error.code === "external_prerequisite_absent",
    );
    await chmod(fixture.paths.j0ReadyReceiptPath, 0o644);
    await assert.rejects(
      loadJ0(fixture.paths.j0ReadyReceiptPath),
      (error: any) => error.code === "protected_business_qualification_invalid" && /0600/.test(error.message),
    );
    await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
    // The cap is the contract's, and a file over it is refused before any of it
    // is parsed.
    await writeFile(fixture.paths.j0ReadyReceiptPath, "x".repeat(J0_READY_MAX_BYTES + 1), { mode: 0o600 });
    await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
    await assert.rejects(
      loadJ0(fixture.paths.j0ReadyReceiptPath),
      (error: any) => error.code === "protected_business_qualification_invalid" && /file is invalid/.test(error.message),
    );
    // The same bytes inside the declared repository root are refused: a receipt
    // the provider checkout could rewrite is not external evidence.
    await writeFile(fixture.paths.j0ReadyReceiptPath, JSON.stringify(j0ReadyReceipt()), { mode: 0o600 });
    await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
    await assert.rejects(
      loadProtectedJ0ReadyReceipt(fixture.paths.j0ReadyReceiptPath, {
        sourceSha: fixture.candidate.product.sha,
        sourceTree: fixture.candidate.product.tree,
        cloudImageDigest: CLOUD_IMAGE_DIGEST,
        provider: "tencent-tke",
        repositoryRoot: root,
      }),
      (error: any) => error.code === "protected_business_qualification_invalid" &&
        /outside the repository/.test(error.message),
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a J0 receipt bound to another Candidate is refused as an absent prerequisite", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-j0-candidate-"));
  try {
    const fixture = await writeFixture(root);
    const receipt = j0ReadyReceipt();
    for (const [name, patch] of [
      ["cloud image", { source: { ...receipt.source, cloudImageDigest: "sha256:" + "9".repeat(64) } }],
      ["product sha", { source: { ...receipt.source, sha: "9".repeat(40) } }],
      ["product tree", { source: { ...receipt.source, tree: "9".repeat(40) } }],
    ] as const) {
      await writeFile(fixture.paths.j0ReadyReceiptPath, JSON.stringify({ ...receipt, ...patch }), { mode: 0o600 });
      await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
      await assert.rejects(
        loadJ0(fixture.paths.j0ReadyReceiptPath),
        (error: any) => error.code === "external_prerequisite_absent" &&
          /for this Candidate/.test(error.message),
        name,
      );
    }
    // A receipt with no usable Candidate binding is not another Candidate's
    // receipt; it is malformed and keeps the contract's own code.
    for (const source of [
      { sha: receipt.source.sha, tree: receipt.source.tree, clean: true },
      { ...receipt.source, cloudImageDigest: "not-a-digest" },
    ]) {
      await writeFile(fixture.paths.j0ReadyReceiptPath, JSON.stringify({ ...receipt, source }), { mode: 0o600 });
      await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
      await assert.rejects(
        loadJ0(fixture.paths.j0ReadyReceiptPath),
        (error: any) => error.code === "j0_ready_receipt_invalid",
        JSON.stringify(source),
      );
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a J0 receipt that is not a confirmed sandbox live pass is refused", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-j0-refusal-"));
  try {
    const fixture = await writeFixture(root);
    const receipt = j0ReadyReceipt();
    for (const [name, patch] of [
      ["provider", { provider: "local-docker" }],
      ["authority class", { authority: { ...receipt.authority, class: "preproduction" } }],
      ["authority mode", { authority: { ...receipt.authority, mode: "fixture" } }],
      ["dedicated", { authority: { ...receipt.authority, dedicated: false } }],
      ["confirmed", { authority: { ...receipt.authority, confirmed: false } }],
      ["failed", { failed: 1 }],
      ["skipped", { skipped: 1 }],
      ["status", { status: "BLOCKED" }],
      ["legacy kind", { kind: "opl.local-workspace.j0-ready.v1" }],
    ] as const) {
      await writeFile(fixture.paths.j0ReadyReceiptPath, JSON.stringify({ ...receipt, ...patch }), { mode: 0o600 });
      await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
      await assert.rejects(
        loadJ0(fixture.paths.j0ReadyReceiptPath),
        (error: any) => error.code === "j0_ready_receipt_invalid",
        name,
      );
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the shared request carries the exact Candidate, tenant, product, authority and fixed scope", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-request-"));
  try {
    const fixture = await writeFixture(root);
    const deployment = await readProtectedDeployment(fixture.paths.deploymentPath, fixture.digest, OWNER_ORIGIN);
    const request = buildProtectedQualificationRequest({
      candidate: fixture.candidate,
      j0ReadyReceipt: j0ReadyReceipt(),
      accountId: ACCOUNT_ID,
      runtimeVersionId: OWNER_RUNTIME_VERSION_ID,
      computePlanId: OWNER_COMPUTE_PLAN_ID,
      storagePlanId: OWNER_STORAGE_PLAN_ID,
      authorityClass: "sandbox",
      provider: "tencent-tke",
    });
    assert.equal(request.provider, "tencent-tke");
    assert.equal(request.candidate.receiptDigest, fixture.digest);
    assert.equal(request.candidate.productSha, fixture.candidate.product.sha);
    assert.equal(request.candidate.productTree, fixture.candidate.product.tree);
    assert.equal(request.candidate.cloudImageDigest, fixture.candidate.cloudImage.indexDigest);
    assert.deepEqual(request.source, { sha: fixture.candidate.product.sha, tree: fixture.candidate.product.tree, clean: true });
    assert.equal(request.tenant.accountId, ACCOUNT_ID);
    assert.match(request.tenant.accountDigest, /^sha256:[0-9a-f]{64}$/);
    assert.deepEqual(request.product, {
      resourceType: "workspace",
      applicationSelection: { kind: "opl_app", runtimeVersionId: OWNER_RUNTIME_VERSION_ID },
      computePlanId: OWNER_COMPUTE_PLAN_ID,
      storagePlanId: OWNER_STORAGE_PLAN_ID,
      autoRenew: false,
    });
    assert.deepEqual(request.scope, PROTECTED_QUALIFICATION_SCOPE);
    assert.equal(request.authority.mode, "live");
    assert.equal(request.authority.authorityClass, "sandbox");
    assert.match(request.authority.j0ReadyDigest, /^sha256:[0-9a-f]{64}$/);
    assert.match(request.operation.idempotencyKey, /^opl-qualification:[0-9a-f]{64}$/);
    assert.equal(deployment.candidateReceiptDigest, fixture.digest);
    await assert.rejects(
      Promise.resolve().then(() => buildProtectedQualificationRequest({
        candidate: fixture.candidate,
        j0ReadyReceipt: j0ReadyReceipt(),
        accountId: " ",
        packageId: "basic",
        authorityClass: "sandbox",
        provider: "tencent-tke",
      })),
      (error: any) => error.code === "qualification_tenant_invalid",
    );
    await assert.rejects(
      Promise.resolve().then(() => buildProtectedQualificationRequest({
        candidate: fixture.candidate,
        j0ReadyReceipt: j0ReadyReceipt(),
        accountId: ACCOUNT_ID,
        runtimeVersionId: "Not A Runtime Release",
        computePlanId: OWNER_COMPUTE_PLAN_ID,
        storagePlanId: OWNER_STORAGE_PLAN_ID,
        authorityClass: "sandbox",
        provider: "tencent-tke",
      })),
      (error: any) => error.code === "qualification_product_invalid",
    );
    await assert.rejects(
      Promise.resolve().then(() => buildProtectedQualificationRequest({
        candidate: fixture.candidate,
        j0ReadyReceipt: j0ReadyReceipt("local-docker"),
        accountId: ACCOUNT_ID,
        runtimeVersionId: OWNER_RUNTIME_VERSION_ID,
        computePlanId: OWNER_COMPUTE_PLAN_ID,
        storagePlanId: OWNER_STORAGE_PLAN_ID,
        authorityClass: "sandbox",
        provider: "tencent-tke",
      })),
      (error: any) => error.code === "j0_ready_receipt_invalid",
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("an absent environment, secret, J0 producer or boundary refuses before a provider scope opens", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-prerequisite-"));
  try {
    const fixture = await writeFixture(root);
    const recording = createRecordingBoundary();
    const base = input(fixture.paths, fixture.digest);
    for (const [name, environment, patch] of [
      [PROTECTED_ENVIRONMENT_NAME, {}, {}],
      [PROTECTED_ORIGIN, protectedEnvironment({ [PROTECTED_ORIGIN]: "" }), {}],
      [PROTECTED_CUSTOMER_EMAIL, protectedEnvironment({ [PROTECTED_CUSTOMER_EMAIL]: "" }), {}],
      [PROTECTED_CUSTOMER_PASSWORD, protectedEnvironment({ [PROTECTED_CUSTOMER_PASSWORD]: "" }), {}],
    ] as const) {
      await assert.rejects(
        runProtectedBusinessQualification({ ...base, ...patch }, { environment, boundary: recording.boundary }),
        (error: any) => error.code === "external_prerequisite_absent" && error.message.includes(name),
        name,
      );
    }
    const j0Absent = { ...base, outputPath: join(root, "evidence", "j0-absent.json"), j0ReadyReceiptPath: join(root, "absent-j0.json") };
    await assert.rejects(
      runProtectedBusinessQualification(j0Absent, { environment: protectedEnvironment(), boundary: recording.boundary }),
      (error: any) => error.code === "external_prerequisite_absent" && /J0 READY producer receipt/.test(error.message),
    );
    const boundaryAbsent = { ...base, outputPath: join(root, "evidence", "boundary-absent.json"), boundaryModulePath: join(root, "absent-boundary.mjs") };
    await assert.rejects(
      runProtectedBusinessQualification(boundaryAbsent, { environment: protectedEnvironment() }),
      (error: any) => error.code === "external_prerequisite_absent" && /protected provider boundary module/.test(error.message),
    );
    assert.deepEqual(recording.calls(), { opens: 0, releases: 0, healths: 0 });
    await assert.rejects(stat(join(root, "evidence")), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the protected boundary module is external, typed and refused when it is absent or malformed", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-boundary-"));
  try {
    const fixture = await writeFixture(root);
    const deployment = await readProtectedDeployment(fixture.paths.deploymentPath, fixture.digest, OWNER_ORIGIN);
    const boundary = await loadProtectedProviderBoundary(fixture.paths.boundaryModulePath, {
      deployment,
      environment: protectedEnvironment(),
    });
    assert.equal(boundary.provider, "tencent-tke");
    await assert.rejects(
      loadProtectedProviderBoundary(join(root, "absent.mjs"), { deployment, environment: protectedEnvironment() }),
      (error: any) => error.code === "external_prerequisite_absent",
    );
    const malformed = join(root, "malformed.ts");
    await writeFile(malformed, "export const notTheFactory = 1;\n");
    await assert.rejects(
      loadProtectedProviderBoundary(malformed, { deployment, environment: protectedEnvironment() }),
      (error: any) => error.code === "protected_business_qualification_invalid" &&
        error.message.includes(PROTECTED_BOUNDARY_FACTORY),
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the producer emits the shared Candidate-bound receipt through the real orchestrator and adapter", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-emission-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const boundary = createFixtureBoundary(deploymentBinding(fixture.digest), platform);
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary,
      wait: async () => {},
    });
    assert.equal(result.state, "settled");
    assert.equal(result.reason, "none");
    assert.equal(result.candidateReceiptDigest, fixture.digest);
    assert.match(result.installationReceiptDigest, /^sha256:[0-9a-f]{64}$/);
    const text = await readFile(fixture.paths.outputPath, "utf8");
    const receipt = validateBusinessQualificationReceipt(JSON.parse(text));
    assert.equal(receipt.kind, QUALIFICATION_BUSINESS_RECEIPT_KIND);
    assert.equal(receipt.scope, QUALIFICATION_BUSINESS_SCOPE);
    assert.equal(receipt.state, "settled");
    assert.equal(receipt.reason, "none");
    assert.equal(receipt.provider, "tencent-tke");
    assert.deepEqual(receipt.uncovered, QUALIFICATION_UNCOVERED_SCOPE);
    assert.deepEqual(receipt.candidate, result.receipt.candidate);
    assert.equal(receipt.candidate.receiptDigest, fixture.digest);
    assert.equal(receipt.source.sha, fixture.candidate.product.sha);
    assert.equal(receipt.source.tree, fixture.candidate.product.tree);
    assert.match(receipt.tenant.accountDigest, /^sha256:[0-9a-f]{64}$/);
    assert.notEqual(receipt.tenant.accountDigest, ACCOUNT_ID);
    assert.deepEqual(receipt.mutations.declared, PROTECTED_QUALIFICATION_SCOPE);
    assert.deepEqual(receipt.mutations.observed, { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 });
    assert.equal(receipt.accounting.quoteUsdMicros, QUOTE_MICROS);
    assert.equal(receipt.cleanup.performed, true);
    assert.equal(receipt.cleanup.restored, true);
    assert.equal(receipt.deploymentHealth, "healthy");
    assert.equal(result.installationReceiptDigest, `sha256:${createHash("sha256").update(fixture.installationText).digest("hex")}`);
    assert.equal(platform.state.purchases, 1);
    assert.equal(platform.state.charges, 1);
    assert.equal(platform.state.calls.filter((call) => call === "boundary:open").length, 1);
    assert.equal((await stat(fixture.paths.outputPath)).mode & 0o777, 0o600);
    for (const forbidden of [
      ACCOUNT_ID, "customer@qualification.invalid", "cloud.qualification.invalid",
      CLUSTER_ID, NAMESPACE, "workspace.qualification.invalid", "ap-guangzhou",
    ]) {
      assert.doesNotMatch(text, new RegExp(forbidden.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
    }
    assert.doesNotMatch(text, /password|secret|instance-token/i);
    const summary = protectedBusinessQualificationSummary(result);
    assert.equal(summary.status, "settled");
    assert.equal(summary.provider, "tencent-tke");
    assert.equal(summary.scope, QUALIFICATION_BUSINESS_SCOPE);
    assert.equal(summary.businessQualification, false);
    assert.equal(Object.hasOwn(summary, "production"), false);
    assert.equal(Object.hasOwn(summary, "qualified"), false);
    assert.doesNotMatch(JSON.stringify(summary), /production|qualified|acct-customer|cloud\.qualification/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the producer invokes the deployed buyer and preserves its Candidate-bound purchase receipt", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-buyer-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const boundary = createFixtureBoundary(deploymentBinding(fixture.digest), platform);
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary,
      wait: async () => {},
    });
    assert.equal(result.state, "settled");
    assert.ok(result.purchaseReceipt);
    const purchaseText = await readFile(result.purchaseReceiptPath, "utf8");
    const purchase = validateDeployedPurchaseReceipt(JSON.parse(purchaseText));
    assert.equal(purchase.kind, DEPLOYED_PURCHASE_RECEIPT_KIND);
    assert.equal(purchase.candidate.receiptDigest, fixture.digest);
    assert.equal(purchase.account.identityDigest, deployedAccountIdentityDigest(ACCOUNT_EMAIL));
    assert.equal(
      purchase.operation.idempotencyKey,
      `deployed-workspace:${fixture.digest.slice("sha256:".length)}:protected-business-qualification`,
    );
    assert.equal(purchase.operation.resumed, false);
    assert.deepEqual(purchase.mutations.observed, { purchases: 1 });
    assert.equal(purchase.purchase.amountUsdMicros, QUOTE_MICROS);
    // The owner mints the operation and the Workspace identity, so the receipt
    // names the owner's own shapes rather than a derived launch identity.
    assert.match(purchase.operation.operationId, OWNER_MINTED_IDENTITY);
    assert.match(purchase.operation.workspaceId, OWNER_MINTED_IDENTITY);
    // The purchase names what was actually bought: the application source and
    // the plan pair the owner priced beside it, under the renewal mode it
    // admitted.
    assert.equal(purchase.product.resourceType, "workspace");
    assert.deepEqual(purchase.product.applicationSelection, { kind: "opl_app", runtimeVersionId: OWNER_RUNTIME_VERSION_ID });
    assert.equal(purchase.product.computePlanId, OWNER_COMPUTE_PLAN_ID);
    assert.equal(purchase.product.storagePlanId, OWNER_STORAGE_PLAN_ID);
    assert.equal(purchase.product.renewalMode, "manual");
    assert.equal(result.receipt.operation.idempotencyKey, purchase.operation.idempotencyKey);
    assert.equal(result.receipt.operation.operationId, purchase.operation.operationId);
    assert.equal(result.receipt.operation.workspaceId, purchase.operation.workspaceId);
    // The Ledger receipt is the owner's record of the charge, and the
    // qualification receipt binds it to the order the owner minted.
    assert.equal(result.receipt.operation.purchaseReceiptId, platform.state.receiptId);
    assert.equal(result.receipt.mutations.observed.purchases, 1);
    assert.equal(result.receipt.checks.purchase_issued, true);
    assert.equal(result.receipt.checks.purchase_receipt_bound, true);
    assert.equal(result.purchaseReceiptDigest, `sha256:${createHash("sha256").update(purchaseText).digest("hex")}`);
    const summary = protectedBusinessQualificationSummary(result);
    assert.equal(summary.purchaseReceiptDigest, result.purchaseReceiptDigest);
    assert.equal(summary.purchaseArtifact, "receipt.purchase.json");
    assert.equal(summary.businessQualification, false);
    assert.equal(platform.state.purchases, 1);
    assert.equal(platform.state.charges, 1);
    assert.ok(platform.state.calls.includes("request:/api/auth/me"), "the buyer read the session account");
    // The wallet is read from the Gateway owner under the owner session, so the
    // balance it checked the quote against is the balance the owner spends.
    assert.ok(platform.state.calls.includes("/api/v2/wallet"), "the buyer read the owner wallet");
    assert.ok(platform.state.calls.includes("/api/v2/auth/session"), "the buyer read the live owner session");
    assert.equal(platform.state.calls.filter((call) => call === "purchase:issued").length, 1);
    // The purchase ran on the Workspace owner route: the owner priced a deploy
    // quote and admitted the order against it. The retained Control Plane route
    // no longer admits purchases and is not asked to.
    for (const catalog of ["runtime-versions", "compute-plans", "storage-plans"]) {
      assert.ok(platform.state.calls.some((call) => call.startsWith(`/api/v2/catalog/${catalog}`)), catalog);
    }
    assert.ok(platform.state.calls.includes("/api/v2/quotes"), "the owner priced the declared purchase");
    assert.ok(platform.state.calls.includes("/api/v2/workspaces"), "the owner admitted the order");
    assert.equal(platform.state.calls.includes("/api/pricing/preview"), false, "the retained pricing route is not used");
    assert.equal(platform.state.calls.includes("/api/workspace-launches"), false, "the retained launch route is not used to buy");
    for (const forbidden of [
      ACCOUNT_ID, ACCOUNT_EMAIL, OWNER_ORIGIN, CLUSTER_ID, NAMESPACE,
      "workspace.qualification.invalid", "ap-guangzhou",
    ]) {
      assert.doesNotMatch(purchaseText, new RegExp(forbidden.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
    }
    assert.doesNotMatch(purchaseText, /password|secret|instance-token/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the deployed buyer refuses an account digest the supplied session does not match", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-buyer-mismatch-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const boundary = createFixtureBoundary(deploymentBinding(fixture.digest), platform);
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment({ [PROTECTED_CUSTOMER_EMAIL]: "other@example.invalid" }),
      boundary,
      wait: async () => {},
    });
    assert.equal(result.state, "failed");
    assert.equal(result.reason, "account_binding_mismatch");
    assert.equal(result.purchaseReceipt, null);
    assert.equal(result.purchaseReceiptDigest, "");
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.state, "failed");
    assert.equal(receipt.reason, "account_binding_mismatch");
    assert.equal(platform.state.purchases, 0);
    assert.equal(platform.state.charges, 0);
    assert.ok(platform.state.calls.includes("request:/api/auth/me"), "the buyer performed the account check");
    await assert.rejects(stat(join(root, "evidence", "receipt.purchase.json")), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a repeated protected delivery resumes the original order and emits the same settled shape", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-replay-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const boundary = createFixtureBoundary(deploymentBinding(fixture.digest), platform);
    const firstOutput = join(root, "evidence", "first.json");
    const secondOutput = join(root, "evidence", "second.json");
    const first = await runProtectedBusinessQualification(
      { ...input(fixture.paths, fixture.digest), outputPath: firstOutput },
      { environment: protectedEnvironment(), boundary, wait: async () => {} },
    );
    const second = await runProtectedBusinessQualification(
      { ...input(fixture.paths, fixture.digest), outputPath: secondOutput },
      { environment: protectedEnvironment(), boundary, wait: async () => {} },
    );
    assert.equal(first.state, "settled");
    assert.equal(second.state, "settled");
    assert.equal(second.receipt.operation.resumed, true);
    assert.equal(second.receipt.mutations.observed.purchases, 0);
    assert.equal(second.receipt.mutations.observed.charges, 0);
    assert.equal(platform.state.purchases, 1);
    assert.equal(platform.state.charges, 1);
    assert.equal(platform.state.calls.filter((call) => call === "purchase:issued").length, 1);
    const firstPurchase = validateDeployedPurchaseReceipt(
      JSON.parse(await readFile(join(root, "evidence", "first.purchase.json"), "utf8")),
    );
    const secondPurchase = validateDeployedPurchaseReceipt(
      JSON.parse(await readFile(join(root, "evidence", "second.purchase.json"), "utf8")),
    );
    assert.equal(firstPurchase.operation.resumed, false);
    assert.deepEqual(firstPurchase.mutations.observed, { purchases: 1 });
    assert.equal(secondPurchase.operation.resumed, true);
    assert.deepEqual(secondPurchase.mutations.observed, { purchases: 0 });
    assert.equal(second.receipt.operation.idempotencyKey, secondPurchase.operation.idempotencyKey);
    assert.equal((await stat(secondOutput)).mode & 0o777, 0o600);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a non-settled result is preserved as the shared receipt and is never summarized as a pass", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-blocked-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform({ walletMicros: "1" });
    const boundary = createFixtureBoundary(deploymentBinding(fixture.digest), platform);
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary,
      wait: async () => {},
    });
    assert.notEqual(result.state, "settled");
    assert.equal(result.state, "blocked");
    assert.equal(result.reason, "wallet_cannot_fund");
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.state, "blocked");
    assert.equal(receipt.reason, "wallet_cannot_fund");
    assert.equal(receipt.uncovered.businessQualification, false);
    const summary = protectedBusinessQualificationSummary(result);
    assert.equal(summary.status, "blocked");
    assert.equal(summary.businessQualification, false);
    assert.equal(platform.state.purchases, 0);
    assert.equal(platform.state.charges, 0);
    assert.ok(platform.state.calls.includes("request:/api/auth/me"), "the buyer checked the session before pricing");
    assert.ok(platform.state.calls.includes("/api/v2/wallet"), "the buyer read the owner wallet before pricing");
    assert.equal(platform.state.calls.filter((call) => call === "boundary:release").length, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the CLI loads the external boundary module and writes the receipt artifact", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-cli-"));
  try {
    const fixture = await writeFixture(root);
    const moduleUrl = pathToFileURL(fixture.paths.boundaryModulePath).href;
    const boundaryModule = await import(moduleUrl) as { platform: ReturnType<typeof createFixturePlatform> };
    const summary = await main([
      "run",
      "--candidate", fixture.paths.candidatePath,
      "--candidate-digest", fixture.digest,
      "--installation", fixture.paths.installationPath,
      "--installation-run", fixture.paths.installationRunPath,
      "--deployment", fixture.paths.deploymentPath,
      "--j0-ready-receipt", fixture.paths.j0ReadyReceiptPath,
      "--boundary", fixture.paths.boundaryModulePath,
      "--account-id", ACCOUNT_ID,
      "--runtime-version-id", OWNER_RUNTIME_VERSION_ID,
      "--compute-plan-id", OWNER_COMPUTE_PLAN_ID,
      "--storage-plan-id", OWNER_STORAGE_PLAN_ID,
      "--authority-class", "sandbox",
      "--output", fixture.paths.outputPath,
    ], protectedEnvironment());
    assert.equal(summary.status, "settled");
    assert.equal(summary.businessQualification, false);
    assert.equal(boundaryModule.platform.state.purchases, 1);
    assert.equal((await stat(fixture.paths.outputPath)).mode & 0o777, 0o600);
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.candidate.receiptDigest, fixture.digest);
    assert.equal(receipt.provider, "tencent-tke");
    const purchase = validateDeployedPurchaseReceipt(
      JSON.parse(await readFile(join(root, "evidence", "receipt.purchase.json"), "utf8")),
    );
    assert.equal(purchase.candidate.receiptDigest, fixture.digest);
    assert.equal(purchase.mutations.observed.purchases, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the shared qualification flow runs for the provider the deployment binding selects", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-shared-flow-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const deployment = deploymentBinding(fixture.digest);
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary: createFixtureBoundary(deployment, platform),
      wait: async () => {},
    });
    // The deployment binding selects the provider; the same value reaches the
    // J0 profile, the shared request and the emitted receipt.
    assert.equal(selectedQualificationProvider(deployment), "tencent-tke");
    assert.equal(result.state, "settled");
    assert.equal(result.receipt.provider, deployment.fabricProvider);
    assert.equal(result.receipt.reason, "none");
    // The provider operation carries the shared contract's own operation key,
    // derived from the Candidate, the tenant, the product and the provider. A
    // producer with its own business flow could not produce this key.
    assert.equal(platform.state.boundaryOperationKeys.length, 1);
    assert.match(
      platform.state.boundaryOperationKeys[0],
      /^opl-qualification:[0-9a-f]{64}\nsha256:[0-9a-f]{64}\nsha256:[0-9a-f]{64}$/,
    );
    // The boundary's transport carried the shared flow's own reads: the tenant
    // fence, the owner catalog, the owner's quote and order, the owner
    // operation, the Ledger charge, the Workspace, the Runtime and both sides
    // of the wallet.
    const calls = platform.state.calls;
    assert.equal(calls[0], "boundary:open");
    for (const read of [
      "/api/auth/me",
      "/api/v2/auth/session",
      "/api/v2/wallet",
      "request:/api/auth/me",
      "/api/v2/quotes",
      "/api/v2/workspaces",
    ]) {
      assert.ok(calls.includes(read), read);
    }
    for (const catalog of ["runtime-versions", "compute-plans", "storage-plans"]) {
      assert.ok(calls.some((call) => call.startsWith(`/api/v2/catalog/${catalog}`)), catalog);
    }
    // The order is read back from the owner that minted it, under the identity
    // the owner minted.
    const mintedOperationId = String(result.purchaseReceipt?.operation?.operationId || "");
    assert.ok(mintedOperationId, "the owner minted the operation identity");
    assert.ok(calls.some((call) => call.endsWith(`/api/v2/operations/workspace/${mintedOperationId}`)));
    assert.ok(calls.some((call) => call === `request:/api/v2/operations/workspace/${mintedOperationId}`));
    // The retained Control Plane route is consulted for the order it may have
    // admitted before the cutover, and for nothing else. It never admits a
    // purchase here.
    assert.ok(calls.some((call) => /^request:\/api\/workspace-launches\/workspace-launch-[0-9a-f]{18}$/.test(call)));
    assert.equal(calls.includes("/api/workspace-launches"), false, "the retained route never admits a purchase");
    assert.equal(calls.includes("/api/pricing/preview"), false, "the retained route never prices a purchase");
    // The Ledger receipt is found in the Ledger's own inventory and read back
    // under the identity it holds.
    assert.ok(calls.some((call) => call.startsWith("/api/billing/receipts?")));
    // The receipt identity is one the owner minted, and the public contract
    // publishes it as a string without a shape.
    assert.ok(calls.some((call) => /^\/api\/billing\/receipts\/[^/]+$/.test(call)));
    assert.ok(calls.some((call) => call.startsWith("/api/workspaces?")));
    assert.ok(calls.some((call) => /^\/api\/workspaces\/[^/]+\/runtime-status$/.test(call)));
    // The wallet is read on both sides of the purchase by the shared flow, and
    // the buyer reads the same owner's balance once more before it prices. Every
    // one of them reads the Gateway owner under the owner session, so no
    // balance is taken from the retained Control Plane projection.
    assert.equal(calls.filter((call) => call === "/api/v2/wallet").length, 3);
    assert.equal(calls.filter((call) => call === "/api/gateway/wallet").length, 0);
    // Nothing else can select the provider: an unsupported one is refused before
    // any boundary operation, even when the deployment is handed in directly.
    assert.throws(
      () => selectedQualificationProvider({ ...deployment, fabricProvider: "local-docker" } as any),
      (error: any) => error.code === "protected_business_qualification_invalid",
    );
    await writeFile(fixture.paths.deploymentPath, JSON.stringify({ ...deployment, fabricProvider: "local-docker" }), { mode: 0o600 });
    const recording = createRecordingBoundary();
    await assert.rejects(
      runProtectedBusinessQualification(
        { ...input(fixture.paths, fixture.digest), outputPath: join(root, "evidence", "other-provider.json") },
        { environment: protectedEnvironment(), boundary: recording.boundary, wait: async () => {} },
      ),
      (error: any) => error.code === "protected_business_qualification_invalid",
    );
    assert.deepEqual(recording.calls(), { opens: 0, releases: 0, healths: 0 });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the emitted receipt binds the declared Candidate and another Candidate cannot reuse it", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-candidate-binding-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const deployment = deploymentBinding(fixture.digest);
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary: createFixtureBoundary(deployment, platform),
      wait: async () => {},
    });
    assert.equal(result.state, "settled");
    assert.equal(result.receipt.candidate.receiptDigest, fixture.digest);
    assert.equal(result.receipt.candidate.productSha, fixture.candidate.product.sha);
    assert.equal(result.receipt.candidate.productTree, fixture.candidate.product.tree);
    assert.equal(result.receipt.candidate.cloudImageDigest, fixture.candidate.cloudImage.indexDigest);
    assert.equal(result.receipt.source.sha, fixture.candidate.product.sha);
    assert.equal(result.receipt.source.tree, fixture.candidate.product.tree);
    // The producer accepts its own receipt and refuses every receipt that names
    // another Candidate's bytes, even when the receipt is internally consistent.
    assert.doesNotThrow(() => assertProtectedReceiptBinding(result.receipt, {
      candidate: fixture.candidate,
      candidateDigest: fixture.digest,
      provider: "tencent-tke",
      deployment,
    }));
    const other = otherCandidateFixture();
    const otherDigest = candidateReceiptDigest(other);
    assert.notEqual(otherDigest, fixture.digest);
    for (const [name, patch] of [
      ["candidate digest", { candidate: { ...result.receipt.candidate, receiptDigest: otherDigest } }],
      ["product sha", { candidate: { ...result.receipt.candidate, productSha: other.product.sha } }],
      ["product tree", { candidate: { ...result.receipt.candidate, productTree: other.product.tree } }],
      ["image digest", { candidate: { ...result.receipt.candidate, cloudImageDigest: other.cloudImage.indexDigest } }],
      ["source sha", { source: { ...result.receipt.source, sha: other.product.sha } }],
      ["source tree", { source: { ...result.receipt.source, tree: other.product.tree } }],
      ["source clean", { source: { ...result.receipt.source, clean: false } }],
      ["provider", { provider: "local-docker" }],
    ] as const) {
      assert.throws(
        () => assertProtectedReceiptBinding({ ...result.receipt, ...patch }, {
          candidate: fixture.candidate,
          candidateDigest: fixture.digest,
          provider: "tencent-tke",
          deployment,
        }),
        (error: any) => error.code === "protected_business_qualification_invalid",
        name,
      );
    }
    // A second Candidate cannot consume this Candidate's deployment binding or
    // installation evidence, and neither refusal opens a provider scope.
    const purchasesBefore = platform.state.purchases;
    const recording = createRecordingBoundary();
    await writeFile(fixture.paths.candidatePath, JSON.stringify(other), { mode: 0o600 });
    await assert.rejects(
      runProtectedBusinessQualification(
        { ...input(fixture.paths, otherDigest), outputPath: join(root, "evidence", "other.json") },
        { environment: protectedEnvironment(), boundary: recording.boundary, wait: async () => {} },
      ),
      (error: any) => error.code === "protected_business_qualification_invalid" && /another Candidate/.test(error.message),
    );
    await writeFile(fixture.paths.deploymentPath, JSON.stringify(deploymentBinding(otherDigest)), { mode: 0o600 });
    await assert.rejects(
      runProtectedBusinessQualification(
        { ...input(fixture.paths, otherDigest), outputPath: join(root, "evidence", "other.json") },
        { environment: protectedEnvironment(), boundary: recording.boundary, wait: async () => {} },
      ),
      (error: any) => error.code === "protected_business_qualification_invalid" &&
        /does not bind the declared Candidate/.test(error.message),
    );
    assert.deepEqual(recording.calls(), { opens: 0, releases: 0, healths: 0 });
    assert.equal(platform.state.purchases, purchasesBefore);
    await assert.rejects(stat(join(root, "evidence", "other.json")), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a lost answer to the purchase command resumes the original order without a second purchase", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-response-loss-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform({ losePurchaseResponse: true });
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary: createFixtureBoundary(deploymentBinding(fixture.digest), platform),
      wait: async () => {},
    });
    // The platform admitted one order and its answer was lost. The run recovered
    // that order under its derived identity and settled against it.
    assert.equal(result.state, "settled");
    assert.equal(result.reason, "none");
    assert.equal(platform.state.purchases, 1);
    assert.equal(platform.state.charges, 1);
    // The order command was issued once and the lost answer was recovered by
    // the owner's own replay of it, never by a second order.
    assert.equal(platform.state.calls.filter((call) => call === "/api/v2/workspaces").length, 2);
    assert.equal(platform.state.calls.filter((call) => call === "purchase:issued").length, 1);
    assert.equal(result.receipt.operation.resumed, false);
    assert.equal(result.receipt.mutations.observed.purchases, 1);
    assert.equal(result.receipt.mutations.observed.charges, 1);
    assert.ok(result.purchaseReceipt);
    assert.equal(result.purchaseReceipt.operation.resumed, false);
    assert.deepEqual(result.purchaseReceipt.mutations.observed, { purchases: 1 });
    // Both receipts name the one recovered operation, not two.
    assert.equal(result.purchaseReceipt.operation.operationId, result.receipt.operation.operationId);
    assert.equal(result.purchaseReceipt.operation.workspaceId, result.receipt.operation.workspaceId);
    // The deployed receipt names the owner-minted order; the Ledger receipt
    // that proves the charge is bound to it by the qualification receipt.
    assert.match(result.purchaseReceipt.operation.operationId, OWNER_MINTED_IDENTITY);
    assert.equal(result.receipt.operation.purchaseReceiptId, platform.state.receiptId);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a lost answer followed by an unreadable owner readback is unknown, never a pass", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-response-lost-unknown-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform({ losePurchaseResponse: true, failOwnerReadbackAfterPurchase: true });
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary: createFixtureBoundary(deploymentBinding(fixture.digest), platform),
      wait: async () => {},
    });
    assert.equal(result.state, "unknown");
    assert.equal(result.reason, "owner_readback_unavailable");
    assert.ok(QUALIFICATION_TERMINAL_REASONS.unknown.includes(result.reason));
    assert.equal(result.purchaseReceipt, null);
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.state, "unknown");
    assert.equal(receipt.checks.purchase_issued, false);
    assert.equal(receipt.checks.charge_receipted, false);
    // The receipt says what the run could not determine; it does not claim a
    // purchase the platform did admit.
    assert.equal(platform.state.purchases, 1);
    assert.equal(platform.state.calls.filter((call) => call === "purchase:issued").length, 1);
    await assert.rejects(stat(join(root, "evidence", "receipt.purchase.json")), /ENOENT/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a provider scope the protected boundary refuses is blocked and buys nothing", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-scope-refused-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      // The protected boundary answers `unknown`: this is how an unapproved or
      // unimplemented provider endpoint surfaces at this seam.
      boundary: createFixtureBoundary(deploymentBinding(fixture.digest), platform, { open: "unknown" }),
      wait: async () => {},
    });
    assert.equal(result.state, "blocked");
    assert.equal(result.reason, "provider_scope_unavailable");
    assert.ok(QUALIFICATION_TERMINAL_REASONS.blocked.includes(result.reason));
    assert.equal(result.purchaseReceipt, null);
    assert.equal(platform.state.purchases, 0);
    assert.equal(platform.state.charges, 0);
    assert.equal(platform.state.calls.filter((call) => call === "/api/auth/me").length, 0);
    assert.equal(platform.state.calls.filter((call) => call === "boundary:open").length, 1);
    assert.equal(platform.state.calls.filter((call) => call === "boundary:release").length, 1);
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.state, "blocked");
    assert.equal(receipt.reason, "provider_scope_unavailable");
    assert.equal(receipt.checks.account_bound, false);
    assert.equal(receipt.cleanup.performed, true);
    assert.equal(receipt.deploymentHealth, "healthy");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a scope that cannot be proven released is recovery-required after a settled purchase", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-cleanup-unproven-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform();
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      // The business evidence is complete, but the provider cannot prove the
      // scope was put back. That is an unfinished operation, not a failure and
      // certainly not a pass.
      boundary: createFixtureBoundary(deploymentBinding(fixture.digest), platform, { release: "unknown" }),
      wait: async () => {},
    });
    assert.equal(result.state, "recovery-required");
    assert.equal(result.reason, "cleanup_unproven");
    assert.ok(QUALIFICATION_TERMINAL_REASONS["recovery-required"].includes(result.reason));
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.checks.charge_receipted, true);
    assert.equal(receipt.checks.runtime_ready, true);
    assert.equal(receipt.cleanup.performed, true);
    assert.equal(receipt.cleanup.restored, false);
    assert.equal(receipt.mutations.observed.purchases, 1);
    assert.equal(platform.state.purchases, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("an unimplemented managed-key lifecycle can never become a business success", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-managed-key-"));
  try {
    const fixture = await writeFixture(root);
    // The fixture owner answers every managed-key lifecycle route with the
    // Sub2API contract's `codes.Unimplemented`, exactly as the approved service
    // does today (Cloud #963).
    const platform = createFixturePlatform();
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary: createFixtureBoundary(deploymentBinding(fixture.digest), platform),
      wait: async () => {},
    });
    assert.equal(result.state, "settled");
    // This flow issues and mutates no Key, so the unapproved endpoint is never
    // asked and its refusal can be neither observed nor relabelled as a pass.
    assert.deepEqual(platform.state.keyLifecycleCalls, []);
    assert.equal(result.receipt.mutations.declared.managedKeys, 0);
    assert.equal(result.receipt.mutations.observed.managedKeys, 0);
    assert.equal(result.receipt.uncovered.gatewayKeyLifecycle, false);
    assert.equal(Object.hasOwn(result.receipt, "key"), false);
    assert.equal(Object.hasOwn(result.receipt.accounting, "keyId"), false);
    // The shared contract refuses any receipt that claims a Key mutation or Key
    // lifecycle coverage, so no producer path can publish a refused lifecycle
    // as success.
    for (const [name, patch] of [
      ["observed key mutation", {
        mutations: {
          ...result.receipt.mutations,
          observed: { ...result.receipt.mutations.observed, managedKeys: 1 },
        },
      }],
      ["key lifecycle coverage", { uncovered: { ...result.receipt.uncovered, gatewayKeyLifecycle: true } }],
      ["key usage coverage", { uncovered: { ...result.receipt.uncovered, keyUsage: true } }],
    ] as const) {
      assert.throws(
        () => validateBusinessQualificationReceipt({ ...result.receipt, ...patch }),
        (error: any) => error.code === "qualification_business_receipt_invalid",
        name,
      );
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a charge that fulfilled an undeclared managed Key is failed, never settled", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-undeclared-key-"));
  try {
    const fixture = await writeFixture(root);
    const platform = createFixturePlatform({ undeclaredKeyFulfillment: true });
    const result = await runProtectedBusinessQualification(input(fixture.paths, fixture.digest), {
      environment: protectedEnvironment(),
      boundary: createFixtureBoundary(deploymentBinding(fixture.digest), platform),
      wait: async () => {},
    });
    // The platform's own charge receipt named a resource this operation never
    // declared. That is not a qualified purchase, and it is not a settled
    // receipt: the flow cannot say what it paid for.
    assert.equal(result.state, "failed");
    assert.equal(result.reason, "charge_receipt_mismatch");
    assert.ok(QUALIFICATION_TERMINAL_REASONS.failed.includes(result.reason));
    const receipt = validateBusinessQualificationReceipt(JSON.parse(await readFile(fixture.paths.outputPath, "utf8")));
    assert.equal(receipt.checks.purchase_issued, true);
    assert.equal(receipt.checks.charge_receipted, false);
    assert.equal(receipt.mutations.observed.charges, 0);
    assert.equal(receipt.mutations.observed.managedKeys, 0);
    assert.deepEqual(platform.state.keyLifecycleCalls, []);
    assert.equal(platform.state.purchases, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
