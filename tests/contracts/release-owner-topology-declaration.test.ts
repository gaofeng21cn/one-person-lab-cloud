import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import YAML from "yaml";

const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-distribution-contract.json", "utf8"));
const topology = JSON.parse(await readFile("deploy/portable/opl-cloud-owner-topology.json", "utf8"));
const installation = contract.portableInstallation;
const format = contract.bundleFormats.formats[contract.bundleFormats.current];

function parseCompose(text: string) {
  return YAML.parseDocument(text, {
    customTags: [
      { tag: "!override", collection: "seq" },
      { tag: "!override", collection: "map" },
      { tag: "!reset", resolve: () => null }
    ],
    logLevel: "silent"
  }).toJS();
}

test("the distribution contract declares the published surface as the owner topology", () => {
  assert.equal(installation.entrypoint, "compose.cloud.yaml");
  assert.equal(installation.entrypoint, installation.ownerTopology.compose);
  assert.equal(installation.ownerTopology.contract, "opl-cloud-owner-topology.json");
  assert.equal(installation.composeScope, "owner_topology_processes_only");
  assert.deepEqual(installation.processes, topology.processes.map((process: any) => process.service));
});

test("the candidate asset set carries the entrypoint and every file it references", async () => {
  const compose = parseCompose(await readFile("deploy/portable/compose.cloud.yaml", "utf8"));
  const services = compose.services || {};
  assert.ok(format.candidateAssets.includes(installation.entrypoint), installation.entrypoint);
  for (const service of Object.values(services) as Array<{ extends?: { file?: string } }>) {
    const file = service.extends?.file;
    if (!file) continue;
    assert.ok(format.candidateAssets.includes(file.replace(/^\.\//, "")), file);
  }
  const backend = services["cloud-backend"] as { command?: string[]; environment?: Record<string, string> } | undefined;
  assert.ok(backend, "cloud-backend service must be declared in compose.cloud.yaml");
  assert.deepEqual(backend.command, [topology.deploymentUnit.unit.entrypoint]);
  assert.equal(backend.environment?.OPL_CLOUD_OWNER_TOPOLOGY, "/app/contracts/opl-cloud-owner-topology.json");
});

test("the owner topology contract stays in the Candidate bundle and is not published", () => {
  assert.deepEqual(contract.bundleFormats.candidateOnlyAssets, ["opl-cloud-owner-topology.json"]);
  assert.ok(format.candidateAssets.includes("opl-cloud-owner-topology.json"));
  assert.ok(!format.releaseAssets.includes("opl-cloud-owner-topology.json"));
  assert.deepEqual(
    format.releaseAssets,
    format.candidateAssets.filter((name: string) => !contract.bundleFormats.candidateOnlyAssets.includes(name))
  );
});
