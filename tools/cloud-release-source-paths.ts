import { createHash } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";

// These are the existing product entry points, not substitute business owners.
export const RELEASE_SOURCE_PATHS = Object.freeze([
  "tools/protected-business-qualification.ts",
  "tools/deployed-workspace-purchase.ts",
  "tools/deployed-workspace-delete.ts",
  "tools/workspace-qualification-j0-ready.ts",
  "tools/workspace-qualification-tencent-tke.ts",
  ".github/workflows/cloud-business-readback.yml",
  "services/workspace/internal/launch/service.go",
  "tests/tools/protected-business-qualification.test.ts",
]);
const JOBS = ["source", "go", "browser (1/2)", "browser (2/2)", "dependency-review", "validate"];
const hash = (text: string | Buffer) => `sha256:${createHash("sha256").update(text).digest("hex")}`;

export async function verifyReleaseSourcePaths(input: {
  candidate: unknown;
  productRoot: string;
  run: Record<string, any>;
  jobs: Record<string, any>;
}) {
  const candidate = validateCloudCandidateReceipt(input.candidate);
  const run = input.run;
  if (run.repository?.full_name !== candidate.product.repository ||
      run.path !== ".github/workflows/pull-request-ci.yml" || run.event !== "pull_request" ||
      run.head_sha !== candidate.product.sha || run.status !== "completed" || run.conclusion !== "success" ||
      !Number.isSafeInteger(run.id) || run.id < 1 || !Number.isSafeInteger(run.run_attempt) || run.run_attempt < 1 ||
      !Array.isArray(input.jobs.jobs) || input.jobs.total_count !== input.jobs.jobs.length ||
      JOBS.some((name) => input.jobs.jobs.filter((job: any) => job.name === name && job.run_id === run.id &&
        job.head_sha === candidate.product.sha && job.status === "completed" && job.conclusion === "success").length !== 1)) {
    throw new Error("release_source_ci_invalid");
  }
  const paths = [];
  for (const path of RELEASE_SOURCE_PATHS) {
    const absolute = resolve(input.productRoot, path);
    if (!(await lstat(absolute)).isFile()) throw new Error("release_source_path_invalid");
    const bytes = await readFile(absolute);
    if (bytes.length === 0) throw new Error("release_source_path_invalid");
    paths.push({ path, digest: hash(bytes) });
  }
  return {
    status: "source_verified", scope: "business_source_paths",
    repository: candidate.product.repository, productSha: candidate.product.sha, productTree: candidate.product.tree,
    workflowRunId: String(run.id), workflowRunAttempt: String(run.run_attempt),
    runDigest: hash(JSON.stringify(run)), jobsDigest: hash(JSON.stringify(input.jobs)), paths,
    paidBusinessExecuted: false, customerReadsExecuted: false,
  };
}
