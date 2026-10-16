import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import test from "node:test";
import YAML from "yaml";

test("portable Compose isolates service credentials and databases", async () => {
  const compose = YAML.parse(await readFile("compose.yaml", "utf8"));

  const databaseURLs = ["control-plane", "fabric", "ledger"].map(
    (service) => compose.services[service].environment.DATABASE_URL as string
  );
  assert.equal(new Set(databaseURLs).size, 3);
  assert.match(databaseURLs[0], /\/opl_control_plane\?sslmode=disable$/);
  assert.match(databaseURLs[1], /\/opl_fabric\?sslmode=disable$/);
  assert.match(databaseURLs[2], /\/opl_ledger\?sslmode=disable$/);

  const serviceTokens = [
    compose.services["control-plane"].environment.OPL_INTERNAL_SERVICE_TOKEN,
    compose.services.fabric.environment.OPL_INTERNAL_SERVICE_TOKEN,
    compose.services.ledger.environment.OPL_INTERNAL_SERVICE_TOKEN
  ];
  assert.equal(new Set(serviceTokens).size, 3);
  assert.equal(compose.services["control-plane"].environment.OPL_FABRIC_SERVICE_TOKEN, serviceTokens[1]);
  assert.equal(compose.services["control-plane"].environment.OPL_LEDGER_SERVICE_TOKEN, serviceTokens[2]);

  for (const [name, service] of Object.entries(compose.services) as Array<[string, { image?: string }]>) {
    const image = service.image || compose["x-opl-cloud-common"]?.image || "";
    assert.ok(
      /@sha256:[0-9a-f]{64}$/.test(image) || image === "${OPL_CLOUD_IMAGE:?Set OPL_CLOUD_IMAGE to an immutable GHCR digest}",
      `${name} image must be digest-pinned`
    );
  }
});

test("Cloud Release promotes an admitted Candidate and isolates public mutation", async () => {
  const workflow = YAML.parse(await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8"));
  const readbackWorkflow = YAML.parse(await readFile(".github/workflows/release-opl-cloud-public-readback.yml", "utf8"));
  const admission = workflow.jobs.admission;
  const publish = workflow.jobs.publish;
  const readback = readbackWorkflow.jobs["public-readback"];
  const publisherGate = "${{ github.ref == 'refs/heads/main' && github.actor == github.triggering_actor && (github.actor == github.repository_owner || github.actor == 'RenDeHuang') }}";

  assert.deepEqual(Object.keys(workflow.on), ["workflow_dispatch"]);
  assert.deepEqual(Object.keys(workflow.jobs), ["admission", "publish"]);
  assert.equal(workflow.concurrency, undefined);
  assert.equal(admission.if, publisherGate);
  assert.equal(publish.if, publisherGate);
  assert.deepEqual(admission.permissions, { actions: "read", contents: "read", packages: "read" });
  assert.equal(admission.environment, undefined);
  assert.equal(admission.concurrency, undefined);
  assert.equal(publish.environment, "cloud-release");
  assert.equal(publish.needs, "admission");
  assert.deepEqual(publish.concurrency, { group: "opl-cloud-publication-global", "cancel-in-progress": false });
  assert.equal(publish.permissions.contents, "write");
  assert.equal(publish.permissions.packages, "write");
  assert.equal(publish.permissions["id-token"], "write");
  assert.deepEqual(Object.keys(readbackWorkflow.on), ["workflow_run", "workflow_dispatch"]);
  assert.deepEqual(readbackWorkflow.on.workflow_run.workflows, ["Release OPL Cloud"]);
  assert.deepEqual(readbackWorkflow.on.workflow_run.types, ["completed"]);
  assert.equal(readbackWorkflow.on.workflow_dispatch.inputs.publication_run_id.required, true);
  assert.match(readback.if, /github\.event_name == 'workflow_run'/);
  assert.match(readback.if, /github\.event_name == 'workflow_dispatch'/);
  assert.equal(readback.environment, undefined);
  assert.equal(readback.concurrency, undefined);
  assert.deepEqual(readback.permissions, { actions: "read", attestations: "read", contents: "read", packages: "read" });

  const admissionCommands = admission.steps.map((step) => step.run || "").join("\n");
  const publishCommands = publish.steps.map((step) => step.run || "").join("\n");
  const readbackCommands = readback.steps.map((step) => step.run || "").join("\n");
  const downloadStep = readback.steps.find((step) => step.uses?.startsWith("actions/download-artifact@"));
  const instanceGate = admission.steps.find((step) => step.id === "instance");
  assert.match(admissionCommands, /cloud-candidate-receipt\.ts validate-bundle/);
  assert.match(admissionCommands, /qualification-decision\.mjs.*gate/s);
  assert.match(admissionCommands, /fresh-workspace-readback\.mjs.*validate/s);
  assert.match(instanceGate.run, /--github-output "\$gate_output"/);
  assert.doesNotMatch(instanceGate.run, /\$\{\{ steps\.instance\.outputs/);
  assert.doesNotMatch(admissionCommands, /docker buildx build/);
  assert.match(publishCommands, /docker buildx imagetools create/);
  assert.match(publishCommands, /published_digest.*EXPECTED_IMAGE_DIGEST/s);
  assert.match(publishCommands, /gh release create/);
  assert.match(publishCommands, /gh release upload.*--clobber/s);
  assert.doesNotMatch(publishCommands, /curl -fsSL/);
  assert.doesNotMatch(publishCommands, /gh attestation verify/);
  assert.match(readbackCommands, /actions\/runs\/\$run_id\/artifacts/);
  assert.equal(downloadStep.with["run-id"], "${{ steps.authority.outputs.run_id }}");
  assert.match(readbackCommands, /curl -fsSL/);
  assert.match(readbackCommands, /gh attestation verify/);
  assert.match(readbackCommands, /sha256sum --check --strict SHA256SUMS/);
});

test("Cloud distribution contract makes same-tag recovery explicit", async () => {
  const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-distribution-contract.json", "utf8"));
  const publication = contract.distribution.publication;

  assert.equal(contract.schemaVersion, 4);
  assert.equal(publication.promotionMode, "qualified_candidate_digest_without_rebuild");
  assert.deepEqual(publication.requiredEvidence, [
    "candidate",
    "local_qualification",
    "instance_deployment_verified",
    "instance_qualification_decision",
    "instance_workspace_verified"
  ]);
  assert.deepEqual(publication.instanceEvidenceReader, {
    secret: "OPL_INSTANCE_EVIDENCE_TOKEN",
    repository: "gaofeng21cn/opl-instance-medopl",
    permissions: ["actions:read", "contents:read"]
  });
  assert.deepEqual(publication.serialization, {
    concurrencyGroup: "opl-cloud-publication-global",
    ownerJob: "publish",
    unlockedJobs: ["admission"]
  });
  assert.deepEqual(publication.recovery, {
    sameTagAssetReplacement: true,
    versionBumpRequiredForPublicationFailure: false
  });
  assert.deepEqual(contract.distribution.publicReadback, {
    workflow: ".github/workflows/release-opl-cloud-public-readback.yml",
    job: "public-readback",
    trigger: "workflow_run",
    manualRetryInput: "publication_run_id",
    nonBlocking: true
  });
});

// #825: the Candidate build admits any workflow-admitted branch, so the promotion
// gate must decide promotability from the recorded artifact digest and source SHA
// instead of from the branch the Candidate run was dispatched on. These helpers
// read the promotion gate's own bash so the contract follows the gate, not a copy.
const promotionEvidenceScript = (release: any): string =>
  String(release.jobs.admission.steps.find((step: any) => step.id === "evidence")?.run ?? "");

const calleeOf = (script: string, authorityFile: string): string => {
  const call = new RegExp(`^\\s*(validate[A-Za-z0-9_]*)\\s.*"\\$authority/${authorityFile}"`, "m").exec(script)?.[1];
  assert.ok(call, `the promotion workflow must admit $authority/${authorityFile} through a named predicate`);
  return call;
};

const bashFunctionBody = (script: string, name: string): string => {
  const lines = script.split("\n");
  const declaration = lines.findIndex((line) => line.trim() === `${name}() {`);
  assert.notEqual(declaration, -1, `the promotion workflow must define ${name}()`);
  const indentation = lines[declaration].search(/\S/);
  const closing = lines.findIndex(
    (line, index) => index > declaration && line.trim() === "}" && line.search(/\S/) === indentation
  );
  assert.notEqual(closing, -1, `${name}() must close its body`);
  return lines.slice(declaration, closing + 1).join("\n");
};

const candidatePredicate = (release: any): string => {
  const evidenceScript = promotionEvidenceScript(release);
  return bashFunctionBody(evidenceScript, calleeOf(evidenceScript, "candidate-run.json"));
};

test("Candidate promotion gate agrees with the Candidate branch-source rule", async () => {
  const build = YAML.parse(await readFile(".github/workflows/build-opl-cloud-candidate.yml", "utf8"));
  const release = YAML.parse(await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8"));
  const readback = YAML.parse(await readFile(".github/workflows/release-opl-cloud-public-readback.yml", "utf8"));

  const buildGuard = String(build.jobs.candidate.if);
  assert.ok(
    buildGuard.includes("github.ref_type == 'branch'") && !buildGuard.includes("refs/heads/main"),
    "the Candidate build side admits any workflow-admitted branch; this contract assumes that rule"
  );

  // The promotion gate is authoritative for promotion, so it must not re-impose the
  // dispatch branch on the Candidate run: that predicate refuses every Candidate
  // built from an integration branch, which is exactly what the build side admits.
  const evidenceScript = promotionEvidenceScript(release);
  const gate = bashFunctionBody(evidenceScript, calleeOf(evidenceScript, "candidate-run.json"));
  assert.doesNotMatch(
    gate,
    /head_branch/,
    "the Candidate promotion gate must key on the recorded artifact digest and source SHA, not on the Candidate run's dispatch branch"
  );
  assert.match(gate, /\.head_sha == \$product_sha/);
  assert.match(gate, /\.conclusion == "success"/);

  // Only the Candidate run is source-keyed. Every other evidence run keeps its
  // main-dispatched requirement.
  for (const authorityFile of ["local-run.json", "instance-deployment-run.json", "instance-decision-run.json", "instance-workspace-run.json"]) {
    assert.match(
      bashFunctionBody(evidenceScript, calleeOf(evidenceScript, authorityFile)),
      /\.head_branch == "main"/,
      `${authorityFile} must stay main-dispatched`
    );
  }

  // Dropping the dispatch branch must not drop the published-source invariant: the
  // promoted commit stays bound to canonical main history, verified at promotion.
  const admissionScript = release.jobs.admission.steps.map((step: any) => step.run || "").join("\n");
  assert.match(admissionScript, /fetch --no-tags "https:\/\/github\.com\/\$PRODUCT_REPOSITORY\.git" main:/);
  assert.match(admissionScript, /merge-base --is-ancestor "\$PRODUCT_SHA"/);

  // Publication itself remains main-only and owner-gated.
  assert.match(String(release.jobs.publish.if), /github\.ref == 'refs\/heads\/main'/);
  const readbackScript = readback.jobs["public-readback"].steps.map((step: any) => step.run || "").join("\n");
  assert.match(readbackScript, /\.head_branch == "main"/);
});

test("Candidate build admits a write-access dispatcher instead of only the repository owner", async () => {
  const build = YAML.parse(await readFile(".github/workflows/build-opl-cloud-candidate.yml", "utf8"));
  const buildGuard = String(build.jobs.candidate.if);

  // The Candidate build was the only evidence producer no write-access collaborator
  // could reach: it demanded github.repository_owner while the publication gate in
  // release-opl-cloud-image.yml already admits RenDeHuang. That refused every
  // build-from-a-pushed-branch path the promotion contract above assumes, so the
  // protected launch in #856 could never obtain a promotable Candidate.
  assert.doesNotMatch(
    buildGuard,
    /github\.repository_owner/,
    "the Candidate build must not be owner-only; publication already admits a write-access collaborator"
  );

  // Dropping the owner predicate must not admit a delegated dispatch: the account
  // that triggered the run must still be the account GitHub attributes it to.
  assert.match(
    buildGuard,
    /github\.actor == github\.triggering_actor/,
    "the Candidate build must still refuse a delegated dispatch"
  );

  // Unchanged: any branch may be built, and only a commit reachable from canonical
  // main may be promoted. Neither invariant lives in this guard.
  assert.match(buildGuard, /github\.ref_type == 'branch'/);
  assert.doesNotMatch(buildGuard, /refs\/heads\/main/);
});

test("Candidate promotion gate refuses every run that is not the promoted source commit", async (t) => {
  const release = YAML.parse(await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8"));
  const gate = candidatePredicate(release);
  const jqProgram = /'\n([\s\S]*?)\n\s*'\s+"\$file"/.exec(gate)?.[1];
  assert.ok(jqProgram, "the Candidate promotion gate must be one jq program");

  if (spawnSync("jq", ["--version"]).status !== 0) {
    t.skip("jq is required to execute the promotion gate predicate");
    return;
  }

  const repository = "gaofeng21cn/one-person-lab-cloud";
  const workflowPath = ".github/workflows/build-opl-cloud-candidate.yml";
  const promotedSha = "a1b2c3d4".repeat(5);
  const otherSha = "f0e1d2c3".repeat(5);
  const branchSourcedRun = {
    id: 900000001,
    repository: { full_name: repository },
    path: workflowPath,
    event: "workflow_dispatch",
    head_branch: "codex/tke-serial-integration",
    head_sha: promotedSha,
    status: "completed",
    conclusion: "success"
  };

  const admits = (run: Record<string, unknown>, productSha = promotedSha) => {
    const evaluated = spawnSync(
      "jq",
      ["-e", "--arg", "run_id", String(run.id), "--arg", "repository", repository, "--arg", "workflow", workflowPath,
        "--arg", "product_sha", productSha, jqProgram],
      { input: JSON.stringify(run), encoding: "utf8" }
    );
    return evaluated.status === 0;
  };

  assert.equal(admits(branchSourcedRun), true, "a Candidate built from an integration branch at the promoted commit stays promotable");

  const refusals: Record<string, Record<string, unknown>> = {
    "a Candidate built from another source commit": { ...branchSourcedRun, head_sha: otherSha },
    "an unfinished run": { ...branchSourcedRun, status: "in_progress" },
    "a failed run": { ...branchSourcedRun, conclusion: "failure" },
    "another repository": { ...branchSourcedRun, repository: { full_name: "gaofeng21cn/opl-instance-medopl" } },
    "another workflow": { ...branchSourcedRun, path: ".github/workflows/clean-host-qualification.yml" },
    "an undispatched run": { ...branchSourcedRun, event: "push" }
  };
  for (const [reason, run] of Object.entries(refusals)) {
    assert.equal(admits(run), false, `the promotion gate must refuse ${reason}`);
  }

  // The source SHA comes from the promotion inputs, so a qualified Candidate cannot
  // be promoted at a commit other than the one the publisher released.
  assert.equal(admits(branchSourcedRun, otherSha), false, "the promotion gate must refuse a Candidate whose run commit is not the released ref");
});


test("Release requires independent installation evidence before business qualification", async () => {
  const release = YAML.parse(await readFile(".github/workflows/release-opl-cloud-image.yml", "utf8"));
  const installation = release.jobs.admission.steps.find((step: any) => step.id === "installation");
  assert.equal(release.on.workflow_dispatch.inputs.instance_deployment_verification_run_id?.required, true,
    "a qualification decision's deployment digest is not an independently validated installation receipt");
  assert.ok(installation, "installation admission must be distinct from Workspace qualification");
  assert.match(installation.run, /deployment-verification-receipt\.mjs.*validate/s);
  assert.match(installation.env.BINDING_VALIDATOR, /cloud-release-installation\.ts$/);
  assert.doesNotMatch(installation.run, /fresh-workspace-readback/);
  const commands = release.jobs.admission.steps.map((step: any) => step.run || "").join("\n");
  assert.match(commands, /qualification-decision\.mjs.*gate/s);
  assert.match(commands, /fresh-workspace-readback\.mjs.*validate/s);
});
