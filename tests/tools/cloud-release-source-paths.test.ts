import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import YAML from "yaml";
import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import { RELEASE_SOURCE_PATHS, verifyReleaseSourcePaths } from "../../tools/cloud-release-source-paths.ts";

const hash = (value: string) => `sha256:${createHash("sha256").update(value).digest("hex")}`;
const sha = "a".repeat(40);
const digest = (value: string) => `sha256:${value.repeat(64)}`;
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "cloud-release-paths-"));
  const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-candidate-receipt-contract.json", "utf8"));
  const candidate = {
    schemaVersion: 2, kind: "opl_cloud_candidate",
    product: { repository: "gaofeng21cn/one-person-lab-cloud", sha, tree: "b".repeat(40) },
    cloudImage: { repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud", indexRef: `ghcr.io/gaofeng21cn/one-person-lab-cloud@${digest("c")}`,
      indexDigest: digest("c"), revision: sha,
      platforms: [{ platform: "linux/amd64", digest: digest("d") }, { platform: "linux/arm64", digest: digest("e") }] },
    assets: contract.candidateReceiptV2.portableAssets.map((name: string) => ({ name, sha256: "1".repeat(64) })),
    provenance: { workflowRepository: "gaofeng21cn/one-person-lab-cloud", workflowSha: sha, workflowRunId: "11", workflowRunAttempt: "1" },
  };
  for (const path of RELEASE_SOURCE_PATHS) {
    await mkdir(dirname(join(root, path)), { recursive: true });
    await writeFile(join(root, path), "fixture source\n");
  }
  const run = { id: 77, run_attempt: 1, repository: { full_name: candidate.product.repository }, head_sha: sha,
    path: ".github/workflows/pull-request-ci.yml", event: "pull_request", status: "completed", conclusion: "success" };
  const jobs = { total_count: 6, jobs: ["source", "go", "browser (1/2)", "browser (2/2)", "dependency-review", "validate"].map(name =>
    ({ name, run_id: 77, head_sha: sha, status: "completed", conclusion: "success" })) };
  return { root, candidate, run, jobs, input: { candidate, productRoot: root, run, jobs } };
}

test("source evidence binds exact completed CI and entry bytes without claiming live business", async () => {
  const f = await fixture();
  try {
    const result = await verifyReleaseSourcePaths(f.input);
    assert.equal(result.status, "source_verified");
    assert.equal(result.productSha, sha);
    assert.equal(result.paidBusinessExecuted, false);
    assert.equal(result.customerReadsExecuted, false);
    assert.equal(result.paths.length, RELEASE_SOURCE_PATHS.length);
    assert.equal(result.paths[0].digest, hash("fixture source\n"));
    for (const patch of [{ head_sha: "f".repeat(40) }, { conclusion: "failure" }, { status: "in_progress" },
      { path: ".github/workflows/build-opl-cloud-candidate.yml" }, { event: "workflow_dispatch" }, { id: 0 },
      { repository: { full_name: "other/repo" } }]) {
      await assert.rejects(verifyReleaseSourcePaths({ ...f.input, run: { ...f.run, ...patch } }), /release_source_ci_invalid/);
    }
    for (const patch of [{ conclusion: "skipped" }, { head_sha: "f".repeat(40) }, { run_id: 78 }]) {
      const jobs = structuredClone(f.jobs); Object.assign(jobs.jobs[0], patch);
      await assert.rejects(verifyReleaseSourcePaths({ ...f.input, jobs }), /release_source_ci_invalid/);
    }
    await assert.rejects(verifyReleaseSourcePaths({ ...f.input, jobs: { ...f.jobs, total_count: 7 } }), /release_source_ci_invalid/);
    await assert.rejects(verifyReleaseSourcePaths({ ...f.input, jobs: { jobs: [], total_count: 0 } }), /release_source_ci_invalid/);
  } finally { await rm(f.root, { recursive: true, force: true }); }
});

test("source-path inspection refuses absent, empty and symlinked entry files", async () => {
  const f = await fixture();
  const path = join(f.root, RELEASE_SOURCE_PATHS[0]);
  try {
    await rm(path);
    await assert.rejects(verifyReleaseSourcePaths(f.input));
    await writeFile(path, "");
    await assert.rejects(verifyReleaseSourcePaths(f.input), /release_source_path_invalid/);
    await rm(path); await symlink(join(f.root, RELEASE_SOURCE_PATHS[1]), path);
    await assert.rejects(verifyReleaseSourcePaths(f.input), /release_source_path_invalid/);
  } finally { await rm(f.root, { recursive: true, force: true }); }
});

test("real sealing, publication and public-readback scripts agree on source-only acceptance", async () => {
  const f = await fixture();
  try {
    const release = YAML.parse(await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8"));
    const follower = YAML.parse(await readFile(".github/workflows/release-opl-cloud-public-readback.yml", "utf8"));
    const evidence = join(f.root, "evidence/candidate");
    await mkdir(evidence, { recursive: true });
    for (const asset of f.candidate.assets) await writeFile(join(evidence, asset.name), "fixture asset\n");
    await writeFile(join(evidence, "opl-cloud-candidate.json"), JSON.stringify(f.candidate));
    const paths = await verifyReleaseSourcePaths(f.input);
    const pathsText = JSON.stringify(paths) + "\n";
    await writeFile(join(f.root, "evidence/source-paths.json"), pathsText);
    const env = { ...process.env, PRODUCT_REPOSITORY: f.candidate.product.repository, PRODUCT_SHA: sha, RELEASE_TAG: "v0.1.8",
      PRODUCT_TREE: f.candidate.product.tree, CANDIDATE_RUN_ID: "11", CANDIDATE_RECEIPT_DIGEST: candidateReceiptDigest(f.candidate),
      LOCAL_QUALIFICATION_RUN_ID: "22", LOCAL_RECEIPT_DIGEST: digest("1"), INSTANCE_DEPLOYMENT_RUN_ID: "33",
      DEPLOYMENT_RECEIPT_DIGEST: digest("2"), DEPLOYMENT_RUN_DIGEST: digest("3"), DEPLOYMENT_AUTHORITY_SHA: "f".repeat(40),
      SOURCE_PATHS_DIGEST: hash(pathsText), GITHUB_SHA: "f".repeat(40), GITHUB_REF: "refs/heads/main", GITHUB_RUN_ID: "44",
      GITHUB_OUTPUT: join(f.root, "output"), GITHUB_ENV: join(f.root, "env"),
      EXPECTED_PRODUCT_TREE: f.candidate.product.tree, EXPECTED_CANDIDATE_RECEIPT_DIGEST: candidateReceiptDigest(f.candidate), EXPECTED_IMAGE_DIGEST: digest("c"), EXPECTED_INSTALLATION_RECEIPT_DIGEST: digest("2"), EXPECTED_INSTALLATION_RUN_DIGEST: digest("3"),
      EXPECTED_INSTALLATION_RUN_ID: "33", EXPECTED_SOURCE_PATHS_DIGEST: hash(pathsText),
      PUBLICATION_WORKFLOW_SHA: "f".repeat(40), PUBLICATION_WORKFLOW_REF: "refs/heads/main" };
    const shell = (script: string) => spawnSync("bash", ["-c", script], { cwd: f.root, env, encoding: "utf8" });
    const sealed = shell(release.jobs.admission.steps.find((s: any) => s.id === "release_bundle").run);
    assert.equal(sealed.status, 0, sealed.stderr);
    const publicationScript = release.jobs.publish.steps.find((s: any) => s.name === "Validate exact release bundle").run;
    const readbackScript = follower.jobs["public-readback"].steps.find((s: any) => s.name === "Validate publication identity and provenance").run;
    const published = shell(publicationScript); assert.equal(published.status, 0, published.stderr);
    const readback = shell(readbackScript); assert.equal(readback.status, 0, readback.stderr);
    const manifestPath = join(f.root, "artifacts/publication/release/opl-cloud-release.json");
    const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
    manifest.qualifications.sourcePaths.paidBusinessExecuted = true;
    await writeFile(manifestPath, JSON.stringify(manifest));
    const updated = shell("cd artifacts/publication/release && shasum -a 256 *.yaml opl-cloud.env.example opl-cloud-release.json > SHA256SUMS");
    assert.equal(updated.status, 0, updated.stderr);
    assert.notEqual(shell(publicationScript).status, 0);
    assert.notEqual(shell(readbackScript).status, 0);
  } finally { await rm(f.root, { recursive: true, force: true }); }
});
