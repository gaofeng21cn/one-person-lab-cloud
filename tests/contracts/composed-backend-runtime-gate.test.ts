import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parse } from "yaml";

// The composed backend's runtime claim is proved by one expensive suite. The
// gate that runs it is the only place that decides whether a broken claim
// fails the build, so this contract owns the gate's shape: what triggers it,
// which mount it provisions, which failure it refuses to swallow, and what it
// cleans up. It also keeps the gate's mount recipe identical to the one the
// clean-host qualification uses, so the two copies cannot drift apart.

const gatePath = new URL("../../.github/workflows/composed-backend-runtime-gate.yml", import.meta.url);
const cleanHostPath = new URL("../../.github/workflows/clean-host-qualification.yml", import.meta.url);
const pullRequestCiPath = new URL("../../.github/workflows/pull-request-ci.yml", import.meta.url);

type Step = {
  name?: string;
  run?: string;
  uses?: string;
  env?: Record<string, string>;
  if?: string;
  "continue-on-error"?: boolean;
};

type Job = {
  "runs-on"?: string;
  "timeout-minutes"?: number;
  permissions?: Record<string, string>;
  environment?: string;
  steps?: Step[];
};

type Workflow = {
  on?: Record<string, any>;
  permissions?: Record<string, string>;
  jobs?: Record<string, Job>;
};

const step = (job: Job, name: string) => {
  const found = job.steps?.find((value) => value.name === name);
  assert.ok(found, `missing step: ${name}`);
  return found;
};

test("the gate runs for the changes that can break the runtime claim", async () => {
  const workflow = parse(await readFile(gatePath, "utf8")) as Workflow;
  const trigger = workflow.on ?? {};

  assert.deepEqual(trigger.pull_request?.branches, ["main"]);
  assert.deepEqual(trigger.push?.branches, ["main"]);
  // The main push carries no path filter: the canonical branch keeps proving
  // the claim on every merge, not only on the merges that touch a listed path.
  assert.equal(trigger.push?.paths, undefined);

  const paths: string[] = trigger.pull_request?.paths ?? [];
  for (const required of [
    // The product image the composed unit runs.
    "Dockerfile", ".dockerignore", "package.json", "package-lock.json",
    "index.html", "vite.config.ts", "public/**", "apps/**", "cmd/**", "packages/**", "services/**",
    // The published installation and the owner topology contract.
    "deploy/portable/**", "docs/spec/target/contracts/**",
    // The suite and the gate itself.
    "tests/integration/**", "tests/contracts/composed-backend-runtime-gate.test.ts",
    ".github/workflows/composed-backend-runtime-gate.yml"
  ]) {
    assert.ok(paths.includes(required), `the gate must run for ${required}`);
  }
});

test("ordinary pull request CI keeps its present speed", async () => {
  const pullRequestCi = await readFile(pullRequestCiPath, "utf8");
  // The suite builds the product image and starts several containers, so it
  // must not join the ordinary pull request workflow.
  assert.doesNotMatch(pullRequestCi, /composed-backend-unit\.test\.ts/);
  assert.doesNotMatch(pullRequestCi, /OPL_PORTABLE_RUNTIME_READBACK/);
});

test("the gate provisions the project-quota mount the clean-host qualification uses", async () => {
  const gate = parse(await readFile(gatePath, "utf8")) as Workflow;
  const cleanHost = parse(await readFile(cleanHostPath, "utf8")) as Workflow;
  const storage = step(gate.jobs!.composed_backend_runtime!, "Prepare the project-quota storage root");
  const qualification = step(cleanHost.jobs!.clean_host_qualification!, "Prepare the Local-Docker storage root");

  // One recipe, two call sites: the mount is a project-quota filesystem in
  // both workflows, and the two copies are compared rather than assumed.
  for (const command of [
    "mkfs.ext4 -F -q -O project,quota -E quotatype=prjquota",
    "mount -o loop,prjquota",
    "truncate -s 12G"
  ]) {
    assert.ok(storage.run?.includes(command), `the gate must run ${command}`);
    assert.ok(qualification.run?.includes(command), `the qualification must run ${command}`);
  }
  // The mount has to carry project quota, not a plain quota filesystem.
  assert.match(storage.run || "", /quotatype=prjquota/);
  assert.match(storage.run || "", /prjquota/);
});

test("the gate runs the suite against the mounted path", async () => {
  const workflow = parse(await readFile(gatePath, "utf8")) as Workflow;
  const job = workflow.jobs!.composed_backend_runtime!;
  const suite = step(job, "Run the composed backend runtime suite");

  assert.equal(suite.env?.OPL_PORTABLE_RUNTIME_READBACK, "1");
  assert.equal(suite.env?.OPL_TEST_QUOTA_ROOT, "${{ steps.storage.outputs.storage_root }}");
  assert.match(suite.run || "", /node --test tests\/integration\/composed-backend-unit\.test\.ts/);
});

test("a broken runtime claim fails the gate", async () => {
  const workflow = parse(await readFile(gatePath, "utf8")) as Workflow;
  const job = workflow.jobs!.composed_backend_runtime!;
  const suite = step(job, "Run the composed backend runtime suite");

  // The suite step runs on its own success and never on a condition that
  // could take it out of the run.
  assert.equal(suite.if, undefined);
  assert.equal(suite["continue-on-error"], undefined);
  assert.doesNotMatch(suite.run || "", /\|\|\s*true/);
  assert.doesNotMatch(suite.run || "", /\|\|\s*:/);
  for (const value of Object.values(job.steps ?? {})) {
    assert.notEqual(value["continue-on-error"], true, `${value.name} must not continue on error`);
  }
  // The suite allows itself one hour, so the job must not cut it shorter.
  assert.ok((job["timeout-minutes"] ?? 0) >= 60, "the job must allow the suite its own hour");
});

test("the gate reaches no secret and no external system", async () => {
  const text = await readFile(gatePath, "utf8");
  const workflow = parse(text) as Workflow;
  const job = workflow.jobs!.composed_backend_runtime!;

  assert.deepEqual(workflow.permissions, { contents: "read" });
  assert.deepEqual(job.permissions, undefined);
  assert.equal(job.environment, undefined);
  assert.doesNotMatch(text, /secrets\./);
  // The gate runs the suite on its own runner; it dispatches no other workflow
  // and reaches no cluster.
  assert.doesNotMatch(text, /workflow_dispatch|gh workflow run|kubectl|helm/);
});

test("the gate cleans up the mount and its image", async () => {
  const workflow = parse(await readFile(gatePath, "utf8")) as Workflow;
  const job = workflow.jobs!.composed_backend_runtime!;
  const storage = step(job, "Prepare the project-quota storage root");
  const cleanup = step(job, "Remove the project-quota storage root");

  // The cleanup runs whether the suite passed or failed.
  assert.equal(cleanup.if, "${{ always() }}");
  assert.match(cleanup.run || "", /mountpoint -q/);
  assert.match(cleanup.run || "", /sudo umount/);
  assert.match(cleanup.run || "", /sudo rm -rf --one-file-system/);

  // The image the recipe writes lives inside the root the cleanup removes, so
  // the runner does not keep a 12 GiB file.
  assert.match(storage.run || "", /quota_image="\$qualification_root\/quota\.img"/);
  const root = storage.run?.match(/qualification_root="([^"]+)"/)?.[1];
  assert.ok(root, "the gate names one run-owned root");
  assert.match(cleanup.run || "", new RegExp(root.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
});
