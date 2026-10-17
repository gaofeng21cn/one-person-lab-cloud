import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";

import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import {
  PROTECTED_BOUNDARY_FACTORY,
  PROTECTED_CUSTOMER_EMAIL,
  PROTECTED_CUSTOMER_PASSWORD,
  PROTECTED_ENVIRONMENT_NAME,
  PROTECTED_INSTANCE_EVIDENCE_TOKEN,
  PROTECTED_ORIGIN,
  PROTECTED_QUALIFICATION_ENVIRONMENT,
  PROTECTED_QUALIFICATION_SCOPE,
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
} from "../../tools/protected-business-qualification.ts";
import {
  QUALIFICATION_BUSINESS_RECEIPT_KIND,
  QUALIFICATION_BUSINESS_SCOPE,
  QUALIFICATION_UNCOVERED_SCOPE,
  validateBusinessQualificationReceipt,
} from "../../tools/workspace-qualification-contract.ts";
import {
  ACCOUNT_ID,
  CLUSTER_ID,
  NAMESPACE,
  OWNER_ORIGIN,
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
    packageId: "basic",
    authorityClass: "sandbox" as const,
    outputPath: paths.outputPath,
  };
}

test("the protected representation fails closed when the environment or a secret is absent", () => {
  const valid = protectedEnvironment();
  assert.deepEqual(protectedQualificationPrerequisites(valid), {
    environment: PROTECTED_QUALIFICATION_ENVIRONMENT,
    origin: OWNER_ORIGIN,
  });
  for (const name of [
    PROTECTED_ENVIRONMENT_NAME,
    PROTECTED_ORIGIN,
    PROTECTED_CUSTOMER_EMAIL,
    PROTECTED_CUSTOMER_PASSWORD,
    PROTECTED_INSTANCE_EVIDENCE_TOKEN,
  ]) {
    const missing = protectedEnvironment({ [name]: "" });
    assert.throws(
      () => protectedQualificationPrerequisites(missing),
      (error: any) => error.code === "external_prerequisite_absent" &&
        error.message === `external prerequisite absent: ${name}`,
      name,
    );
  }
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
    "--package-id", "basic",
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

test("the external J0 receipt is admitted only from the exact protected file and source", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "opl-protected-j0-"));
  try {
    const fixture = await writeFixture(root);
    const receipt = await loadProtectedJ0ReadyReceipt(fixture.paths.j0ReadyReceiptPath, {
      sourceSha: fixture.candidate.product.sha,
      sourceTree: fixture.candidate.product.tree,
      repositoryRoot: REPOSITORY_ROOT,
    });
    assert.equal(receipt.provider, "tencent-tke");
    assert.equal(receipt.status, "READY");
    await assert.rejects(
      loadProtectedJ0ReadyReceipt(join(root, "absent.json"), {
        sourceSha: fixture.candidate.product.sha,
        sourceTree: fixture.candidate.product.tree,
        repositoryRoot: REPOSITORY_ROOT,
      }),
      (error: any) => error.code === "external_prerequisite_absent" &&
        /J0 READY producer receipt/.test(error.message),
    );
    await chmod(fixture.paths.j0ReadyReceiptPath, 0o644);
    await assert.rejects(
      loadProtectedJ0ReadyReceipt(fixture.paths.j0ReadyReceiptPath, {
        sourceSha: fixture.candidate.product.sha,
        sourceTree: fixture.candidate.product.tree,
        repositoryRoot: REPOSITORY_ROOT,
      }),
      (error: any) => error.code === "protected_business_qualification_invalid" && /0600/.test(error.message),
    );
    await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
    for (const [patch, expectedCode] of [
      [{ provider: "local-docker" }, "j0_ready_receipt_invalid"],
      [{ source: { ...j0ReadyReceipt().source, sha: "0".repeat(40) } }, "j0_ready_receipt_invalid"],
      [{ authority: { ...j0ReadyReceipt().authority, class: "preproduction" } }, "j0_ready_receipt_invalid"],
    ] as const) {
      await writeFile(fixture.paths.j0ReadyReceiptPath, JSON.stringify({ ...j0ReadyReceipt(), ...patch }), { mode: 0o600 });
      await chmod(fixture.paths.j0ReadyReceiptPath, 0o600);
      await assert.rejects(
        loadProtectedJ0ReadyReceipt(fixture.paths.j0ReadyReceiptPath, {
          sourceSha: fixture.candidate.product.sha,
          sourceTree: fixture.candidate.product.tree,
          repositoryRoot: REPOSITORY_ROOT,
        }),
        (error: any) => error.code === expectedCode,
        JSON.stringify(patch),
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
      packageId: "basic",
      authorityClass: "sandbox",
    });
    assert.equal(request.provider, "tencent-tke");
    assert.equal(request.candidate.receiptDigest, fixture.digest);
    assert.equal(request.candidate.productSha, fixture.candidate.product.sha);
    assert.equal(request.candidate.productTree, fixture.candidate.product.tree);
    assert.equal(request.candidate.cloudImageDigest, fixture.candidate.cloudImage.indexDigest);
    assert.deepEqual(request.source, { sha: fixture.candidate.product.sha, tree: fixture.candidate.product.tree, clean: true });
    assert.equal(request.tenant.accountId, ACCOUNT_ID);
    assert.match(request.tenant.accountDigest, /^sha256:[0-9a-f]{64}$/);
    assert.deepEqual(request.product, { resourceType: "workspace", packageId: "basic", autoRenew: false });
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
      })),
      (error: any) => error.code === "qualification_tenant_invalid",
    );
    await assert.rejects(
      Promise.resolve().then(() => buildProtectedQualificationRequest({
        candidate: fixture.candidate,
        j0ReadyReceipt: j0ReadyReceipt(),
        accountId: ACCOUNT_ID,
        packageId: "Not A Package",
        authorityClass: "sandbox",
      })),
      (error: any) => error.code === "qualification_product_invalid",
    );
    await assert.rejects(
      Promise.resolve().then(() => buildProtectedQualificationRequest({
        candidate: fixture.candidate,
        j0ReadyReceipt: j0ReadyReceipt("local-docker"),
        accountId: ACCOUNT_ID,
        packageId: "basic",
        authorityClass: "sandbox",
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
      [PROTECTED_INSTANCE_EVIDENCE_TOKEN, protectedEnvironment({ [PROTECTED_INSTANCE_EVIDENCE_TOKEN]: "" }), {}],
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
      "--package-id", "basic",
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
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
