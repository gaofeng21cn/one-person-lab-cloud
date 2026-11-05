import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, chmod, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { canonicalJson } from "../../tools/cloud-candidate-receipt.ts";
import { spawnSync } from "node:child_process";
import test from "node:test";
import YAML from "yaml";

const workflow = YAML.parse(await readFile(".github/workflows/cloud-business-readback.yml", "utf8"));
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

test("optional customer readback preserves its artifact without running a business purchase", () => {
  assert.deepEqual(Object.keys(workflow.on.workflow_dispatch.inputs), ["candidate_run_id", "installation_run_id"]);
  const job = workflow.jobs.readback;
  const commands = job.steps.map((value: any) => value.run || "").join("\n");
  assert.doesNotMatch(commands, /protected-business-qualification|deployed-workspace-purchase|workspace-launches|OPL_QUALIFICATION_PROVIDER_BOUNDARY/);
  const uploads = job.steps.filter((value: any) => value.uses?.startsWith("actions/upload-artifact@"));
  assert.equal(uploads.length, 1);
  assert.equal(uploads[0].with.path, "qualification-evidence/receipt.json");
  assert.match(uploads[0].with.name, /^cloud-customer-readback-/);
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
