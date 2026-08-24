import assert from "node:assert/strict";
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
  const admission = workflow.jobs.admission;
  const publish = workflow.jobs.publish;
  const readback = workflow.jobs["public-readback"];
  const publisherGate = "${{ github.ref == 'refs/heads/main' && github.actor == github.triggering_actor && (github.actor == github.repository_owner || github.actor == 'RenDeHuang') }}";

  assert.deepEqual(Object.keys(workflow.on), ["workflow_dispatch"]);
  assert.deepEqual(Object.keys(workflow.jobs), ["admission", "publish", "public-readback"]);
  assert.equal(workflow.concurrency, undefined);
  assert.equal(admission.if, publisherGate);
  assert.equal(publish.if, publisherGate);
  assert.equal(readback.if, publisherGate);
  assert.deepEqual(admission.permissions, { actions: "read", contents: "read", packages: "read" });
  assert.equal(admission.environment, undefined);
  assert.equal(admission.concurrency, undefined);
  assert.equal(publish.environment, "cloud-release");
  assert.equal(publish.needs, "admission");
  assert.deepEqual(publish.concurrency, { group: "opl-cloud-publication-global", "cancel-in-progress": false });
  assert.equal(publish.permissions.contents, "write");
  assert.equal(publish.permissions.packages, "write");
  assert.equal(publish.permissions["id-token"], "write");
  assert.deepEqual(readback.needs, ["admission", "publish"]);
  assert.equal(readback.environment, undefined);
  assert.equal(readback.concurrency, undefined);
  assert.deepEqual(readback.permissions, { actions: "read", attestations: "read", contents: "read" });

  const admissionCommands = admission.steps.map((step) => step.run || "").join("\n");
  const publishCommands = publish.steps.map((step) => step.run || "").join("\n");
  const readbackCommands = readback.steps.map((step) => step.run || "").join("\n");
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
  assert.match(readbackCommands, /curl -fsSL/);
  assert.match(readbackCommands, /gh attestation verify/);
  assert.match(readbackCommands, /sha256sum --check --strict SHA256SUMS/);
});

test("Cloud distribution contract makes same-tag recovery explicit", async () => {
  const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-distribution-contract.json", "utf8"));
  const publication = contract.distribution.publication;

  assert.equal(contract.schemaVersion, 3);
  assert.equal(publication.promotionMode, "qualified_candidate_digest_without_rebuild");
  assert.deepEqual(publication.requiredEvidence, [
    "candidate",
    "local_qualification",
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
    unlockedJobs: ["admission", "public-readback"]
  });
  assert.deepEqual(publication.recovery, {
    sameTagAssetReplacement: true,
    versionBumpRequiredForPublicationFailure: false
  });
});
