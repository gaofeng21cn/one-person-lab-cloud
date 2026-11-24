import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { parse } from "yaml";

import { cleanHostInstallation } from "../../tools/local-v2-clean-host.ts";

const workflowPath = new URL("../../.github/workflows/clean-host-qualification.yml", import.meta.url);
const buildFixturePath = new URL("../tools/fixtures/clean-host-build.sh", import.meta.url);
const templatePath = new URL("../../deploy/portable/opl-cloud.env.example", import.meta.url);
const composePath = new URL("../../deploy/portable/compose.local-v2.yaml", import.meta.url);

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

test("the customer package chain is qualified on the same installation", async () => {
  const workflow = parse(await readFile(workflowPath, "utf8"));
  const trigger = workflow.on ?? workflow["on"];
  const job = workflow.jobs?.clean_host_qualification;
  const steps = job?.steps as Array<{ name?: string; run?: string; env?: Record<string, string> }>;

  // The package, its probe and the evidence level are inputs of the run, so
  // the operator can qualify any customer package and not only one fixture.
  const inputs = trigger?.workflow_dispatch?.inputs;
  for (const input of ["customer_package_dir", "customer_package_url", "customer_probe_path", "customer_evidence_level"]) {
    assert.ok(inputs?.[input], `missing workflow input ${input}`);
  }
  assert.deepEqual(inputs?.customer_evidence_level?.options, ["content-provenance", "artifact-identity"]);

  const preparation = steps.find((step) => step.name === "Prepare the customer package to qualify");
  assert.ok(preparation?.run, "missing customer package preparation");
  assert.match(preparation.run, /customer-package-dir|CUSTOMER_PACKAGE_DIR/);
  assert.match(preparation.run, /tests\/tools\/fixtures\/customer-agent-package/);

  const qualification = steps.find((step) => step.name === "Prepare and exercise the native owner installation");
  assert.ok(qualification?.run, "missing reusable qualification invocation");
  assert.match(qualification.run, /--customer-package-dir "\$CUSTOMER_PACKAGE_DIR"/);
  assert.match(qualification.run, /--customer-probe-path "\$CUSTOMER_PROBE_PATH"/);
  assert.match(qualification.run, /--customer-expected-evidence-level "\$CUSTOMER_EVIDENCE_LEVEL"/);
  // One qualified run proves the installation and the customer package chain.
  assert.equal((qualification.run.match(/node tools\/local-workspace-qualification\.ts/g) || []).length, 2);
});

test("the plain-HTTP host of the lane registry is named by the lane that created it", async () => {
  const workflow = parse(await readFile(workflowPath, "utf8"));
  const job = workflow.jobs?.clean_host_qualification;
  const steps = job?.steps as Array<{ name?: string; run?: string; env?: Record<string, string> }>;

  const registry = steps.find((step) => step.name === "Prepare immutable qualification Build artifacts");
  assert.ok(registry?.run, "missing registry preparation");
  assert.equal(registry.run, "bash tests/tools/fixtures/clean-host-build.sh prepare");
  const fixture = await readFile(buildFixturePath, "utf8");
  assert.match(fixture, /registry_host="127\.0\.0\.1:\$port"/);
  assert.match(fixture, /-p "\$registry_host:\$port"/);
  assert.match(fixture, /registry_host=%s\\n' "\$registry" "\$registry_host" >> "\$GITHUB_OUTPUT"/);

  const qualification = steps.find((step) => step.name === "Prepare and exercise the native owner installation");
  assert.ok(qualification?.run, "missing reusable qualification invocation");
  // The host travels from the registry step into the installation declaration.
  assert.equal(qualification.env?.REGISTRY_HOST, "${{ steps.workspace.outputs.registry_host }}");
  assert.match(qualification.run, /--insecure-http-hosts "\$REGISTRY_HOST"/);
  assert.match(qualification.run, /--build-registry "\$REGISTRY_HOST"/);
});

test("the installation declares a plain-HTTP host only when the caller names one", async () => {
  const template = await readFile(templatePath, "utf8");
  const compose = await readFile(composePath, "utf8");
  const base = {
    template, compose, directory: "/tmp/opl-installation", sourceSha: "a".repeat(40),
    cloudImage: `127.0.0.1:5000/opl-cloud@sha256:${"b".repeat(64)}`,
    workspaceImage: `127.0.0.1:5000/opl-qualification-workspace@sha256:${"c".repeat(64)}`,
    storageRoot: "/tmp/opl-storage", project: "opl-native-1",
    publisherSchemaDigest: `sha256:${"d".repeat(64)}`,
  };
  // Without a named host the declaration keeps the empty value of the
  // template: the tool never guesses a host.
  const plain = cleanHostInstallation(base);
  assert.equal(plain.values.OPL_BUILD_INSECURE_HTTP_HOSTS, "");
  // The host the lane registry really serves on is the one the caller passes.
  const lane = cleanHostInstallation({ ...base, insecureHttpHosts: "127.0.0.1:5000" });
  assert.equal(lane.values.OPL_BUILD_INSECURE_HTTP_HOSTS, "127.0.0.1:5000");
  assert.equal(lane.values.OPL_BUILD_REGISTRY_PREFIX, "127.0.0.1:5000/qualification");
  // A value that is not a host list is refused.
  assert.throws(() => cleanHostInstallation({ ...base, insecureHttpHosts: "127.0.0.1:5000 127.0.0.1:5001" }), /clean_host_insecure_http_hosts_invalid/);
});
