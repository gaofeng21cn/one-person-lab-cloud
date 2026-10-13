import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parse } from "yaml";

const workflowPath = new URL("../../.github/workflows/clean-host-qualification.yml", import.meta.url);

interface WorkflowStep {
  name?: string;
  run?: string;
  uses?: string;
  with?: Record<string, unknown>;
  env?: Record<string, string>;
  if?: string;
}

async function qualificationSteps() {
  const workflow = parse(await readFile(workflowPath, "utf8"));
  const trigger = workflow.on ?? workflow["on"];
  const job = workflow.jobs?.clean_host_qualification;
  return { trigger, job, steps: (job?.steps ?? []) as WorkflowStep[] };
}

test("clean-host workflow delegates qualification to the reusable local runner", async () => {
  const { trigger, job, steps } = await qualificationSteps();

  assert.equal(job?.["runs-on"], "ubuntu-latest");
  assert.equal(job?.environment, undefined);
  assert.equal(trigger?.workflow_dispatch?.inputs?.ref?.required, true);

  const checkout = steps.find((step) => step.name === "Checkout exact product source");
  assert.equal(checkout?.with?.ref, "${{ inputs.ref }}");
  assert.equal(checkout?.with?.["persist-credentials"], false);
  assert.ok(steps.some((step) => step.name === "Install locked dependencies" && /npm ci/.test(step.run || "")));

  const qualification = steps.find((step) => step.name === "Run local Workspace qualification");
  assert.ok(qualification?.run, "missing reusable qualification invocation");
  assert.match(qualification.run, /npm run qualify:local:workspace --/);
  assert.match(qualification.run, /--source-sha "\$PRODUCT_SHA"/);
  assert.match(qualification.run, /--authority-mode fixture/);
  assert.match(qualification.run, /--receipt "\$RUNNER_TEMP\/local-workspace-qualification\.json"/);

  const upload = steps.find((step) => step.name === "Upload local qualification receipt");
  assert.match(upload?.uses || "", /^actions\/upload-artifact@[0-9a-f]{40}$/);
  assert.equal(upload?.if, "${{ always() }}");
  assert.equal(upload?.with?.name, "local-workspace-qualification");
  assert.equal(upload?.with?.path, "${{ runner.temp }}/local-workspace-qualification.json");
  assert.equal(upload?.with?.["if-no-files-found"], "error");
});

test("clean-host workflow builds its own images instead of pulling published references", async () => {
  const { trigger, steps } = await qualificationSteps();

  // The runner refuses any image whose `org.opencontainers.image.revision` label
  // is not this run's source SHA. A published reference only ever carries the
  // label of the commit it was built from, so pulling one cannot satisfy the
  // gate. Building from this checkout makes the label correct by construction.
  const inputs = trigger?.workflow_dispatch?.inputs ?? {};
  assert.equal(inputs.cloud_image_ref, undefined, "an externally supplied Cloud image is no longer an input");
  assert.equal(inputs.workspace_image_ref, undefined, "an externally supplied Workspace image is no longer an input");

  const qualification = steps.find((step) => step.name === "Run local Workspace qualification");
  assert.ok(qualification, "missing reusable qualification invocation");
  assert.match(qualification.run || "", /--build-source-images/);
  assert.doesNotMatch(qualification.run || "", /--cloud-image/);
  assert.doesNotMatch(qualification.run || "", /--workspace-image/);

  // Building from source needs no registry, so the job must not read a
  // credential or a registry hostname to qualify this revision.
  const serialized = JSON.stringify(steps);
  assert.doesNotMatch(serialized, /docker login/);
  assert.doesNotMatch(serialized, /secrets\./);
  assert.doesNotMatch(serialized, /ccs\.tencentyun\.com/);
  assert.doesNotMatch(serialized, /ghcr\.io/);
});