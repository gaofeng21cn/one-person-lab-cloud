import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, chmod, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { canonicalJson } from "../../tools/cloud-candidate-receipt.ts";
import { spawnSync } from "node:child_process";
import test from "node:test";
import YAML from "yaml";

const workflow = YAML.parse(await readFile(".github/workflows/cloud-business-readback.yml", "utf8"));
const release = YAML.parse(await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8"));
const readback = YAML.parse(await readFile(".github/workflows/release-opl-cloud-public-readback.yml", "utf8"));
const digest = (c: string) => `sha256:${c.repeat(64)}`;
const sha = "a".repeat(40);
const tree = "b".repeat(40);
const repository = "gaofeng21cn/one-person-lab-cloud";
const step = (job: any, name: string) => {
  const found = job.steps.find((value: any) => value.name === name);
  assert.ok(found, name);
  return found;
};
const run = (command: string, cwd: string, env: Record<string, string> = {}) =>
  spawnSync("bash", ["-c", command], { cwd, env: { ...process.env, ...env }, encoding: "utf8", timeout: 10_000 });

// Execute the authored pre-environment shell gate, so missing protection cannot
// be concealed by a test that merely searches the YAML for its own words.
test("customer execution refuses absent or insufficient environment protections", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-customer-environment-"));
  try {
    const gh = join(root, "gh");
    await writeFile(gh, '#!/bin/sh\ncase "$2" in\n  */deployment-branch-policies) cat "$BRANCH_FIXTURE" ;;\n  *) test -f "$ENV_FIXTURE" && cat "$ENV_FIXTURE" ;;\nesac\n');
    await chmod(gh, 0o700);
    const environment = {
      deployment_branch_policy: { custom_branch_policies: true, protected_branches: false },
      protection_rules: [{ type: "required_reviewers", prevent_self_review: true,
        reviewers: [{ type: "User", reviewer: { login: "gaofeng21cn" } }] }],
    };
    const branches = { total_count: 1, branch_policies: [{ name: "main", type: "branch" }] };
    const command = step(workflow.jobs.admission, "Require existing protected customer execution environment").run;
    const env = { PATH: `${root}:${process.env.PATH}`, ENV_FIXTURE: join(root, "environment.json"),
      BRANCH_FIXTURE: join(root, "branches.json"), GITHUB_REPOSITORY: repository };
    await writeFile(env.BRANCH_FIXTURE, JSON.stringify(branches));
    assert.notEqual(run(command, root, env).status, 0, "missing environment");
    for (const patch of [
      { protection_rules: [] },
      { protection_rules: [{ ...environment.protection_rules[0], prevent_self_review: false }] },
      { protection_rules: [{ ...environment.protection_rules[0], reviewers: [] }] },
      { protection_rules: [{ ...environment.protection_rules[0], reviewers: [{ type: "Team", reviewer: { login: "gaofeng21cn" } }] }] },
      { deployment_branch_policy: { custom_branch_policies: false, protected_branches: true } },
    ]) {
      await writeFile(env.ENV_FIXTURE, JSON.stringify({ ...environment, ...patch }));
      assert.notEqual(run(command, root, env).status, 0);
    }
    await writeFile(env.ENV_FIXTURE, JSON.stringify(environment));
    let result = run(command, root, env);
    assert.equal(result.status, 0, result.stderr);
    for (const policy of [{ name: "*", type: "branch" }, { name: "main", type: "tag" }]) {
      await writeFile(env.BRANCH_FIXTURE, JSON.stringify({ total_count: 1, branch_policies: [policy] }));
      assert.notEqual(run(command, root, env).status, 0);
    }
    await writeFile(env.BRANCH_FIXTURE, JSON.stringify({ total_count: 2, branch_policies: [...branches.branch_policies, { name: "feature/*", type: "branch" }] }));
    assert.notEqual(run(command, root, env).status, 0);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("Cloud customer workflow limits credentials to protected public product reads", () => {
  assert.deepEqual(Object.keys(workflow.jobs), ["admission", "readback"]);
  assert.equal(workflow.jobs.admission.environment, undefined);
  assert.equal(workflow.jobs.readback.needs, "admission");
  assert.equal(workflow.jobs.readback.environment, "cloud-business-qualification");
  for (const job of Object.values(workflow.jobs) as any[]) {
    assert.equal(job["runs-on"], "ubuntu-latest");
    assert.deepEqual(job.permissions, { actions: "read", contents: "read" });
    assert.match(job.if, /github\.actor == github\.triggering_actor/);
    assert.match(job.if, /refs\/heads\/main/);
    assert.match(job.if, /github\.actor == github\.repository_owner/);
  }
  const customer = step(workflow.jobs.readback, "Read customer product surfaces through CloudIdentity");
  assert.deepEqual(Object.keys(customer.env), ["OPL_QUALIFICATION_ORIGIN", "OPL_QUALIFICATION_CUSTOMER_EMAIL", "OPL_QUALIFICATION_CUSTOMER_PASSWORD"]);
  const commands = workflow.jobs.readback.steps.map((s: any) => s.run || "").join("\n");
  assert.doesNotMatch(commands, /kubectl|kubeconfig|port-forward|workflow run|POSTPAID_BY_HOUR|\/api\/v1\/admin/);
  assert.match(commands, /deployment-verification-receipt\.mjs validate/);
  assert.match(commands, /validate-bundle/);
  const installation = workflow.jobs.readback.steps.find((s: any) => s.name === "Checkout exact native installation validator");
  assert.equal(installation.with.ref, "${{ steps.inputs.outputs.installation_sha }}");
  assert.equal(installation.with["persist-credentials"], false);
});

test("protected business qualification runs only through the shared producer after exact evidence", () => {
  const readbackJob = workflow.jobs.readback;
  assert.equal(readbackJob["timeout-minutes"], 45);
  assert.equal(readbackJob.environment, "cloud-business-qualification");
  const inputs = workflow.on.workflow_dispatch.inputs;
  for (const name of [
    "candidate_receipt_digest",
    "deployment_binding",
    "qualification_account_id",
    "qualification_package_id",
    "j0_ready_receipt",
  ]) {
    assert.equal(inputs[name]?.required, true, name);
    assert.equal(inputs[name]?.type, "string", name);
  }
  assert.equal(inputs.qualification_authority_class?.required, true);
  assert.equal(inputs.qualification_authority_class?.type, "choice");
  assert.deepEqual(inputs.qualification_authority_class?.options, ["sandbox", "preproduction"]);

  const materialize = step(readbackJob, "Materialize protected qualification declarations");
  assert.equal(materialize.id, "declarations");
  assert.match(materialize.if, /steps\.evidence\.outcome == 'success'/);
  for (const name of [
    "OPL_QUALIFICATION_ENVIRONMENT",
    "OPL_QUALIFICATION_ORIGIN",
    "OPL_QUALIFICATION_PROVIDER_BOUNDARY",
    "OPL_QUALIFICATION_CUSTOMER_EMAIL",
    "OPL_QUALIFICATION_CUSTOMER_PASSWORD",
    "OPL_INSTANCE_EVIDENCE_TOKEN",
  ]) {
    assert.ok(Object.hasOwn(materialize.env || {}, name), name);
  }
  assert.match(materialize.run, /external prerequisite absent/);
  assert.match(materialize.run, /\.candidateReceiptDigest == \$candidate/);
  assert.match(materialize.run, /\.ownerOrigin == \$origin/);
  assert.match(materialize.run, /\.fabricProvider == "tencent-tke"/);
  assert.match(materialize.run, /\.kind == "opl\.workspace-qualification\.j0-ready\.v2"/);
  assert.match(materialize.run, /\.status == "READY"/);
  assert.match(materialize.run, /\.failed == 0 and \.skipped == 0/);
  assert.match(materialize.run, /chmod 600/);
  assert.match(materialize.run, /stat -c '%a'.*stat -f '%Lp'/);
  assert.doesNotMatch(materialize.run, /\/api\//);

  const producer = step(readbackJob, "Run protected business qualification through the shared Cloud flow");
  assert.match(producer.if, /steps\.evidence\.outcome == 'success'/);
  assert.match(producer.if, /steps\.declarations\.outcome == 'success'/);
  assert.match(producer.run, /node tools\/protected-business-qualification\.ts run/);
  for (const declaration of [
    /--candidate qualification-inputs\/candidate\/opl-cloud-candidate\.json/,
    /--candidate-digest "\$CANDIDATE_RECEIPT_DIGEST"/,
    /--installation qualification-inputs\/installation\/evidence\.json/,
    /--installation-run "\$RUNNER_TEMP\/customer-inputs\/installation-run\.json"/,
    /--deployment "\$RUNNER_TEMP\/qualification\/deployment\.json"/,
    /--j0-ready-receipt "\$RUNNER_TEMP\/j0-ready\/receipt\.json"/,
    /--boundary "\$OPL_QUALIFICATION_PROVIDER_BOUNDARY"/,
    /--account-id "\$QUALIFICATION_ACCOUNT_ID"/,
    /--package-id "\$QUALIFICATION_PACKAGE_ID"/,
    /--authority-class "\$QUALIFICATION_AUTHORITY_CLASS"/,
    /--output business-qualification-evidence\/receipt\.json/,
  ]) {
    assert.match(producer.run, declaration);
  }
  assert.doesNotMatch(producer.run, /cloud-business-readback\.ts|deployed-workspace-purchase|workspace-qualification-(?:orchestrator|tencent-tke)\.ts/);
  for (const name of [
    "OPL_QUALIFICATION_ENVIRONMENT",
    "OPL_QUALIFICATION_ORIGIN",
    "OPL_QUALIFICATION_PROVIDER_BOUNDARY",
    "OPL_QUALIFICATION_CUSTOMER_EMAIL",
    "OPL_QUALIFICATION_CUSTOMER_PASSWORD",
    "OPL_INSTANCE_EVIDENCE_TOKEN",
  ]) {
    assert.ok(Object.hasOwn(producer.env || {}, name), name);
  }

  const businessUpload = step(readbackJob, "Preserve Candidate-bound business qualification receipts");
  assert.equal(businessUpload.if, "always()");
  assert.equal(businessUpload.with["if-no-files-found"], "ignore");
  assert.equal(businessUpload.with.path, "business-qualification-evidence/");
  assert.match(businessUpload.with.name, /cloud-business-qualification-/);

  const names = readbackJob.steps.map((value: any) => value.name);
  const evidenceIndex = names.indexOf("Validate original installation and exact Candidate bytes");
  const materializeIndex = names.indexOf("Materialize protected qualification declarations");
  const producerIndex = names.indexOf("Run protected business qualification through the shared Cloud flow");
  assert.ok(evidenceIndex >= 0 && evidenceIndex < materializeIndex && materializeIndex < producerIndex);
  assert.match(step(readbackJob, "Validate original installation and exact Candidate bytes").run, /deployment-verification-receipt\.mjs validate/);
  assert.match(step(readbackJob, "Validate original installation and exact Candidate bytes").run, /cloud-candidate-receipt\.ts validate-bundle/);
  assert.match(step(readbackJob, "Read customer product surfaces through CloudIdentity").run, /cloud-business-readback\.ts run/);
  const commands = readbackJob.steps.map((value: any) => value.run || "").join("\n");
  assert.doesNotMatch(commands, /kubectl|kubeconfig|port-forward|workflow run|POSTPAID_BY_HOUR|\/api\/v1\/admin/);
});

test("the protected declaration gate materializes exact inputs and fails closed when a prerequisite is absent", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-declarations-"));
  try {
    const candidateSha = "a".repeat(40);
    const candidateTree = "b".repeat(40);
    const candidateDigest = digest("c");
    const j0 = {
      schemaVersion: 1,
      kind: "opl.workspace-qualification.j0-ready.v2",
      status: "READY",
      source: { sha: candidateSha, tree: candidateTree, cloudImageDigest: `sha256:${"e".repeat(64)}`, clean: true },
      authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
      provider: "tencent-tke",
      failed: 0,
      skipped: 0,
    };
    const deployment = {
      deploymentMode: "managed_tke",
      fabricProvider: "tencent-tke",
      candidateReceiptDigest: candidateDigest,
      clusterId: "cls-qualification0001",
      region: "ap-guangzhou",
      namespace: "tke-qualification",
      ownerOrigin: "https://cloud.qualification.invalid",
    };
    await mkdir(join(root, "qualification-inputs/candidate"), { recursive: true });
    await mkdir(join(root, "qualification-inputs/installation"), { recursive: true });
    await writeFile(join(root, "qualification-inputs/candidate/opl-cloud-candidate.json"), JSON.stringify({ product: { sha: candidateSha, tree: candidateTree } }));
    await writeFile(join(root, "qualification-inputs/installation/evidence.json"), "{}");
    await writeFile(join(root, "boundary.mjs"), "export function createTencentTkeProviderBoundary() {}\n");
    const environment = {
      RUNNER_TEMP: join(root, "runner-temp"),
      CANDIDATE_RECEIPT_DIGEST: candidateDigest,
      DEPLOYMENT_BINDING: JSON.stringify(deployment),
      QUALIFICATION_ACCOUNT_ID: "acct-customer-0001",
      QUALIFICATION_PACKAGE_ID: "basic",
      QUALIFICATION_AUTHORITY_CLASS: "sandbox",
      J0_READY_RECEIPT: JSON.stringify(j0),
      OPL_QUALIFICATION_ENVIRONMENT: "cloud-business-qualification",
      OPL_QUALIFICATION_ORIGIN: deployment.ownerOrigin,
      OPL_QUALIFICATION_PROVIDER_BOUNDARY: join(root, "boundary.mjs"),
      OPL_QUALIFICATION_CUSTOMER_EMAIL: "customer@qualification.invalid",
      OPL_QUALIFICATION_CUSTOMER_PASSWORD: "not-a-real-password",
      OPL_INSTANCE_EVIDENCE_TOKEN: "not-a-real-token",
    };
    const command = step(workflow.jobs.readback, "Materialize protected qualification declarations").run;
    let result = run(command, root, environment);
    assert.equal(result.status, 0, result.stderr);
    const deploymentFile = join(environment.RUNNER_TEMP, "qualification/deployment.json");
    const j0File = join(environment.RUNNER_TEMP, "j0-ready/receipt.json");
    assert.deepEqual(JSON.parse(await readFile(deploymentFile, "utf8")), deployment);
    assert.deepEqual(JSON.parse(await readFile(j0File, "utf8")), j0);
    assert.equal((await stat(j0File)).mode & 0o777, 0o600);
    for (const name of [
      "OPL_QUALIFICATION_ENVIRONMENT",
      "OPL_QUALIFICATION_ORIGIN",
      "OPL_QUALIFICATION_CUSTOMER_EMAIL",
      "OPL_QUALIFICATION_CUSTOMER_PASSWORD",
      "OPL_INSTANCE_EVIDENCE_TOKEN",
      "OPL_QUALIFICATION_PROVIDER_BOUNDARY",
    ]) {
      result = run(command, root, { ...environment, [name]: "" });
      assert.notEqual(result.status, 0, name);
      assert.match(result.stderr, new RegExp(`external prerequisite absent: ${name}`), name);
    }
    for (const binding of [
      { ...deployment, ownerOrigin: "https://another.example.invalid" },
      { ...deployment, candidateReceiptDigest: digest("d") },
      { ...deployment, fabricProvider: "local-docker" },
    ]) {
      result = run(command, root, { ...environment, DEPLOYMENT_BINDING: JSON.stringify(binding) });
      assert.notEqual(result.status, 0, JSON.stringify(binding));
    }
    for (const receipt of [
      { ...j0, kind: "opl.local-workspace.j0-ready.v1" },
      { ...j0, source: { ...j0.source, sha: "0".repeat(40) } },
      { ...j0, failed: 1 },
    ]) {
      result = run(command, root, { ...environment, J0_READY_RECEIPT: JSON.stringify(receipt) });
      assert.notEqual(result.status, 0, JSON.stringify(receipt));
    }
    result = run(command, root, { ...environment, J0_READY_RECEIPT: "{" });
    assert.notEqual(result.status, 0);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test("release seals separate Cloud customer and Instance business evidence and refuses tampering", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-customer-seal-"));
  try {
    await mkdir(join(root, "evidence/candidate"), { recursive: true });
    const seal = step(release.jobs.admission, "Seal release bundle from admitted Candidate bytes").run;
    const assets = ["compose.yaml", "compose.deployment-platform-owned.yaml", "compose.deployment-managed-tke.yaml",
      "compose.deployment-customer-owned.yaml", "compose.fabric-local-docker.yaml", "compose.fabric-tencent-tke.yaml",
      "compose.local-workspace.yaml", "opl-cloud.env.example"];
    for (const asset of assets) await writeFile(join(root, "evidence/candidate", asset), `synthetic ${asset}\n`);
    const env = {
      PRODUCT_REPOSITORY: repository, PRODUCT_SHA: sha, PRODUCT_TREE: tree, RELEASE_TAG: "v0.9.0",
      CANDIDATE_RUN_ID: "100", CANDIDATE_RUN_ATTEMPT: "1", CANDIDATE_RECEIPT_DIGEST: digest("c"),
      IMAGE_REF: `ghcr.io/${repository}@${digest("d")}`, IMAGE_DIGEST: digest("d"), AMD64_DIGEST: digest("e"), ARM64_DIGEST: digest("f"),
      LOCAL_QUALIFICATION_RUN_ID: "101", LOCAL_RECEIPT_DIGEST: digest("1"), INSTANCE_REPOSITORY: "gaofeng21cn/opl-instance-medopl",
      INSTANCE_DEPLOYMENT_RUN_ID: "102", DEPLOYMENT_RUN_DIGEST: digest("2"), DEPLOYMENT_RECEIPT_DIGEST: digest("3"), DEPLOYMENT_AUTHORITY_SHA: sha,
      CUSTOMER_RUN_ID: "105", CUSTOMER_RECEIPT_DIGEST: digest("4"), CUSTOMER_AUTHORITY_SHA: tree,
      CLOUD_BUSINESS_QUALIFICATION_RUN_ID: "107", BUSINESS_RECEIPT_DIGEST: digest("a"), BUSINESS_RUN_DIGEST: digest("b"),
      BUSINESS_COMPLETED_AT: "2026-10-17T00:05:00Z",
      INSTANCE_DECISION_RUN_ID: "103", DECISION_RUN_DIGEST: digest("5"), DECISION_RECEIPT_DIGEST: digest("6"), DECISION_AUTHORITY_SHA: sha,
      INSTANCE_WORKSPACE_RUN_ID: "104", WORKSPACE_RECEIPT_DIGEST: digest("7"), WORKSPACE_AUTHORITY_SHA: sha, QUALIFICATION_SCOPE_DIGEST: digest("8"),
      GITHUB_SHA: sha, GITHUB_REF: "refs/heads/main", GITHUB_RUN_ID: "106", GITHUB_OUTPUT: join(root, "outputs"),
    };
    let result = run(seal, root, env);
    assert.equal(result.status, 0, result.stderr);
    const manifestPath = join(root, "artifacts/publication/release/opl-cloud-release.json");
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    const provenance = JSON.parse(await readFile(join(root, "artifacts/publication/release-provenance.json"), "utf8"));
    assert.equal(manifest.schemaVersion, 3);
    assert.equal(manifest.installation.repository, env.INSTANCE_REPOSITORY);
    assert.equal(manifest.qualifications.customer.repository, repository);
    assert.equal(manifest.qualifications.customer.scope, "customer_reads");
    assert.equal(manifest.qualifications.customer.receiptDigest, provenance.qualification.customerReceiptDigest);
    assert.equal(manifest.qualifications.business.receiptDigest, env.BUSINESS_RECEIPT_DIGEST);
    assert.equal(manifest.qualifications.business.terminalState, "settled");
    assert.equal(manifest.qualifications.business.scope, "workspace_purchase");
    assert.equal(provenance.evidenceClasses.cloudBusinessQualification.receiptDigest, env.BUSINESS_RECEIPT_DIGEST);
    assert.equal(provenance.evidenceClasses.cloudBusinessQualification.runDigest, env.BUSINESS_RUN_DIGEST);
    assert.equal(provenance.evidenceClasses.instanceDeployment.receiptDigest, env.DEPLOYMENT_RECEIPT_DIGEST);
    assert.equal(provenance.evidenceClasses.instanceDeployment.status, "deployment_verified");
    assert.equal(manifest.qualifications.instance.workspace.status, "workspace_verified");
    const expressions: Record<string, string> = {
      "needs.admission.outputs.product_tree": tree,
      "needs.admission.outputs.candidate_receipt_digest": env.CANDIDATE_RECEIPT_DIGEST,
      "needs.admission.outputs.installation_receipt_digest": env.DEPLOYMENT_RECEIPT_DIGEST,
      "needs.admission.outputs.business_receipt_digest": env.BUSINESS_RECEIPT_DIGEST,
      "needs.admission.outputs.business_run_digest": env.BUSINESS_RUN_DIGEST,
      "needs.admission.outputs.customer_receipt_digest": env.CUSTOMER_RECEIPT_DIGEST,
      "needs.admission.outputs.qualification_scope_digest": env.QUALIFICATION_SCOPE_DIGEST,
    };
    const validate = step(release.jobs.publish, "Validate exact release bundle").run.replace(/\$\{\{ ([^}]+) \}\}/g,
      (_: string, key: string) => { assert.ok(expressions[key], key); return expressions[key]; });
    const validateEnv = {
      ...env, EXPECTED_IMAGE_DIGEST: env.IMAGE_DIGEST,
      EXPECTED_BUSINESS_RECEIPT_DIGEST: env.BUSINESS_RECEIPT_DIGEST,
      EXPECTED_BUSINESS_RUN_DIGEST: env.BUSINESS_RUN_DIGEST,
      EXPECTED_BUSINESS_RUN_ID: env.CLOUD_BUSINESS_QUALIFICATION_RUN_ID,
      EXPECTED_INSTALLATION_RECEIPT_DIGEST: env.DEPLOYMENT_RECEIPT_DIGEST,
      EXPECTED_INSTALLATION_RUN_DIGEST: env.DEPLOYMENT_RUN_DIGEST,
      EXPECTED_INSTALLATION_RUN_ID: env.INSTANCE_DEPLOYMENT_RUN_ID,
    };
    result = run(validate, root, validateEnv);
    assert.equal(result.status, 0, result.stderr);
    const publicValidation = step(readback.jobs["public-readback"], "Validate publication identity and provenance").run;
    result = run(publicValidation, root, { ...env, PUBLICATION_WORKFLOW_SHA: sha,
      PUBLICATION_WORKFLOW_REF: "refs/heads/main", GITHUB_ENV: join(root, "public-env") });
    assert.equal(result.status, 0, result.stderr);
    // Recompute checksums to prove semantic admission, not only checksum failure.
    for (const patch of [
      { qualifications: { ...manifest.qualifications, customer: { ...manifest.qualifications.customer, status: "blocked" } } },
      { qualifications: { ...manifest.qualifications, customer: { ...manifest.qualifications.customer, receiptDigest: digest("9") } } },
      { qualifications: { ...manifest.qualifications, business: { ...manifest.qualifications.business, terminalState: "unknown" } } },
      { qualifications: { ...manifest.qualifications, business: { ...manifest.qualifications.business, receiptDigest: digest("9") } } },
      { qualifications: { ...manifest.qualifications, instance: { ...manifest.qualifications.instance, workspace: { status: "blocked" } } } },
      { installation: { ...manifest.installation, status: "blocked" } },
    ]) {
      await writeFile(manifestPath, JSON.stringify({ ...manifest, ...patch }));
      const checksum = run("cd artifacts/publication/release && shasum -a 256 " + [...assets, "opl-cloud-release.json"].join(" ") + " > SHA256SUMS", root);
      assert.equal(checksum.status, 0, checksum.stderr);
      assert.notEqual(run(validate, root, validateEnv).status, 0);
    }
    // The sealed provenance records the same two evidence classes; a class that
    // does not bind the admitted digest or terminal state is refused even when
    // the public manifest is untouched.
    const provenancePath = join(root, "artifacts/publication/release-provenance.json");
    await writeFile(manifestPath, JSON.stringify(manifest));
    assert.equal(run("cd artifacts/publication/release && shasum -a 256 " + [...assets, "opl-cloud-release.json"].join(" ") + " > SHA256SUMS", root).status, 0);
    for (const evidenceClasses of [
      { ...provenance.evidenceClasses, cloudBusinessQualification: { ...provenance.evidenceClasses.cloudBusinessQualification, receiptDigest: digest("9") } },
      { ...provenance.evidenceClasses, cloudBusinessQualification: { ...provenance.evidenceClasses.cloudBusinessQualification, terminalState: "unknown" } },
      { ...provenance.evidenceClasses, instanceDeployment: { ...provenance.evidenceClasses.instanceDeployment, status: "blocked" } },
    ]) {
      await writeFile(provenancePath, JSON.stringify({ ...provenance, evidenceClasses }));
      assert.notEqual(run(validate, root, validateEnv).status, 0);
    }
  } finally { await rm(root, { recursive: true, force: true }); }
});


test("workflow executes the Candidate CLI against original bundle bytes", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-customer-cli-"));
  try {
    const bundle = join(root, "qualification-inputs/candidate");
    await mkdir(bundle, { recursive: true });
    const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-candidate-receipt-contract.json", "utf8"));
    const sha256 = (value: string | Buffer) => createHash("sha256").update(value).digest("hex");
    const assets = contract.candidateReceiptV2.portableAssets.map((name: string) => ({ name, sha256: sha256(name + "\n") }));
    for (const asset of assets) await writeFile(join(bundle, asset.name), asset.name + "\n");
    const candidate = { schemaVersion: 2, kind: "opl_cloud_candidate", product: { repository, sha, tree },
      cloudImage: { repository: `ghcr.io/${repository}`, indexRef: `ghcr.io/${repository}@${digest("c")}`,
        indexDigest: digest("c"), revision: sha, platforms: [{ platform: "linux/amd64", digest: digest("d") }, { platform: "linux/arm64", digest: digest("e") }] },
      assets, provenance: { workflowRepository: repository, workflowSha: sha, workflowRunId: "100", workflowRunAttempt: "1" } };
    const manifest = canonicalJson(candidate) + "\n";
    await writeFile(join(bundle, "opl-cloud-candidate.json"), manifest);
    await writeFile(join(bundle, "SHA256SUMS"), [...assets, { name: "opl-cloud-candidate.json", sha256: sha256(manifest) }]
      .map(asset => `${asset.sha256}  ${asset.name}\n`).join(""));
    const validation = step(workflow.jobs.readback, "Validate original installation and exact Candidate bytes");
    const authoredCommand = validation.run.split("\n").find((line: string) => line.includes("cloud-candidate-receipt.ts validate-bundle"));
    assert.ok(authoredCommand);
    const script = resolve("tools/cloud-candidate-receipt.ts");
    const command = authoredCommand.replace("tools/cloud-candidate-receipt.ts", `'${script.replaceAll("'", "'\\''")}'`);
    const result = run(command, root);
    assert.equal(result.status, 0, result.stderr);
    await writeFile(join(bundle, assets[0].name), "corrupted\n");
    assert.notEqual(run(command, root).status, 0, "altered original bundle must fail");
  } finally { await rm(root, { recursive: true, force: true }); }
});
