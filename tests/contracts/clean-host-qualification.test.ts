import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parse } from "yaml";

const workflowPath = new URL("../../.github/workflows/clean-host-qualification.yml", import.meta.url);

test("clean-host workflow delegates qualification to the reusable local runner", async () => {
  const workflow = parse(await readFile(workflowPath, "utf8"));
  const trigger = workflow.on ?? workflow["on"];
  const job = workflow.jobs?.clean_host_qualification;
  const steps = job?.steps as Array<{
    name?: string;
    run?: string;
    uses?: string;
    with?: Record<string, unknown>;
    env?: Record<string, string>;
    if?: string;
  }>;

  assert.equal(job?.["runs-on"], "ubuntu-latest");
  assert.equal(job?.environment, undefined);
  for (const input of ["ref", "cloud_image_ref"]) {
    assert.equal(trigger?.workflow_dispatch?.inputs?.[input]?.required, true);
  }
  assert.equal(trigger?.workflow_dispatch?.inputs?.workspace_image_ref?.required, false);

  const checkout = steps.find((step) => step.name === "Checkout exact product source");
  assert.equal(checkout?.with?.ref, "${{ inputs.ref }}");
  assert.equal(checkout?.with?.["persist-credentials"], false);
  assert.ok(steps.some((step) => step.name === "Install locked dependencies" && /npm ci/.test(step.run || "")));

  const qualification = steps.find((step) => step.name === "Prepare and exercise the native owner installation");
  assert.ok(qualification?.run, "missing reusable qualification invocation");
  assert.equal(qualification.if, "${{ inputs.mode == 'local-v2' }}");
  assert.match(qualification.run, /node tools\/local-workspace-qualification\.ts/);
  assert.match(qualification.run, /--mode local-v2/);
  assert.match(qualification.run, /--source-sha "\$PRODUCT_SHA"/);
  assert.match(qualification.run, /--installation-env "\$directory\/installation\.env"/);
  assert.match(qualification.run, /--application-contract "\$directory\/application\.json"/);
  assert.match(qualification.run, /--project "\$project"/);
  assert.match(qualification.run, /--receipt "\$RUNNER_TEMP\/local-workspace-qualification\.json"/);

  const upload = steps.find((step) => step.name === "Upload local qualification receipt");
  assert.match(upload?.uses || "", /^actions\/upload-artifact@[0-9a-f]{40}$/);
  assert.equal(upload?.if, "${{ always() }}");
  assert.equal(upload?.with?.name, "local-workspace-qualification");
  assert.equal(upload?.with?.path, "${{ runner.temp }}/local-workspace-qualification.json");
  assert.equal(upload?.with?.["if-no-files-found"], "error");
});

test("clean-host workflow offers native local-v2 and no retired qualification mode", async () => {
  const workflow = parse(await readFile(workflowPath, "utf8"));
  const trigger = workflow.on ?? workflow["on"];
  const job = workflow.jobs?.clean_host_qualification;
  const steps = job?.steps as Array<{ name?: string; run?: string; if?: string }>;

  // The version-one fixture and live runs are retired. The workflow may still
  // name the qualification it dispatches, but it may only name the one that
  // exists, so the input cannot dispatch a mode the tool now refuses.
  const mode = trigger?.workflow_dispatch?.inputs?.mode;
  assert.equal(mode?.default, "local-v2");
  assert.deepEqual(mode?.options, ["local-v2"]);

  // The retired modes are refused by the tool, so the workflow must not carry
  // an invocation that could ask for one.
  for (const step of steps) {
    const run = step.run || "";
    assert.doesNotMatch(run, /--authority-mode/, `${step.name} must not dispatch the retired authority mode`);
    assert.doesNotMatch(run, /legacy-fixture/, `${step.name} must not dispatch the retired fixture mode`);
    assert.doesNotMatch(run, /legacy-live/, `${step.name} must not dispatch the retired live mode`);
  }
  assert.equal(steps.find((step) => step.name === "Run local Workspace qualification"), undefined);
});
