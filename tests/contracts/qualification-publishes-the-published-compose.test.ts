import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { PORTABLE_ASSETS } from "../../tools/cloud-candidate-receipt.ts";
import { cleanHostInstallation } from "../../tools/local-v2-clean-host.ts";
import { localV2InstallationFacts, localV2ReferencedVariables } from "../../tools/local-v2-installation-facts.ts";
import {
  LOCAL_V2_QUALIFICATION_COMPOSE,
  LOCAL_V2_QUALIFICATION_OVERLAY,
  qualificationModeBinding,
} from "../../tools/local-workspace-qualification.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

// The published installation: the artifact a customer installs, and the
// artifact the qualification starts.
const PUBLISHED_COMPOSE = "deploy/portable/compose.cloud.yaml";
// The qualification overlay: the fixture authority and the wiring that points
// the owners at it. A repository file, and not shipped.
const QUALIFICATION_OVERLAY = "deploy/portable/compose.qualification.yaml";
// The qualification topology that is being replaced: a second topology, which
// the qualification must not start.
const RETIRED_COMPOSE = "deploy/portable/compose.local-v2.yaml";

const publishedServices = [
  "build", "capability", "cloud-local-v2", "console-bff", "control-plane", "fabric",
  "ledger", "postgres", "resource-catalog", "runtime-control", "serve", "tenant", "workspace"
];

// One installation's facts, derived the way the clean host derives them, from
// the template and the merged topology.
function qualificationFacts(root, template, compose, directory) {
  return cleanHostInstallation({
    template,
    compose,
    directory,
    sourceSha: "0123456789012345678901234567890123456789",
    cloudImage: `ghcr.io/example/cloud@sha256:${"a".repeat(64)}`,
    workspaceImage: `ghcr.io/example/workspace@sha256:${"b".repeat(64)}`,
    storageRoot: join(directory, "storage"),
    project: "opl-qualification-1-1",
    publisherSchemaDigest: `sha256:${"c".repeat(64)}`
  });
}

test("the qualification starts the published installation and one repository-only overlay", () => {
  // The invariant this guard protects: the artifact the qualification verifies
  // is the artifact a customer installs. A qualification that started any
  // other topology would say nothing about the shipped product.
  const binding = qualificationModeBinding("local-v2");
  assert.ok(binding.composeFiles.includes(PUBLISHED_COMPOSE),
    `the qualification must start the published installation ${PUBLISHED_COMPOSE}`);
  assert.deepEqual(binding.composeFiles, [PUBLISHED_COMPOSE, QUALIFICATION_OVERLAY]);
  assert.equal(binding.composeFiles.includes(RETIRED_COMPOSE), false,
    "the qualification must not start a second topology beside the published one");
  assert.equal(LOCAL_V2_QUALIFICATION_COMPOSE, PUBLISHED_COMPOSE);
  assert.equal(LOCAL_V2_QUALIFICATION_OVERLAY, QUALIFICATION_OVERLAY);
});

test("the published compose stays the shipped one and the overlay stays out of the bundle", () => {
  // The published compose is renamed away from being the shipped one, the
  // qualification would verify a file no customer installs.
  assert.ok(PORTABLE_ASSETS.includes("compose.cloud.yaml"),
    "the published Compose file must be in the release bundle");
  assert.equal(PORTABLE_ASSETS.includes("compose.qualification.yaml"), false,
    "the qualification overlay must not be in the release bundle");
  // The retired qualification topology may still be in the bundle. The asset
  // list is an exact-ordered, digest-bound publication surface: dropping a
  // name leaves every already-issued candidate and release receipt matching no
  // format at all. What has to hold is that nothing reads it, and
  // tests/contracts/portable-compose-self-contained.test.ts guards that.
});

test("the merged topology resolves to the published services plus the fixture authority", async () => {
  const directory = await mkdtemp(join(tmpdir(), "opl-qualify-published-"));
  try {
    const template = await readFile(resolve(root, "deploy/portable/opl-cloud.env.example"), "utf8");
    const published = await readFile(resolve(root, PUBLISHED_COMPOSE), "utf8");
    const overlay = await readFile(resolve(root, QUALIFICATION_OVERLAY), "utf8");
    const facts = qualificationFacts(root, template, `${published}\n${overlay}`, directory);
    await writeFile(facts.values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH, "{}\n");
    const envFile = join(directory, "installation.env");
    await writeFile(envFile, facts.entries.map(([name, value]) => `${name}=${value}`).join("\n"));

    const result = spawnSync("docker", [
      "compose", "--env-file", envFile, "-p", "opl-qualification-1-1",
      "-f", PUBLISHED_COMPOSE, "-f", QUALIFICATION_OVERLAY, "config", "--format", "json"
    ], { encoding: "utf8", cwd: root });
    assert.equal(result.status, 0, result.stderr);
    const model = JSON.parse(result.stdout);
    assert.deepEqual(Object.keys(model.services).sort(), [...publishedServices, "sub2api-authority"].sort());
    // Every published owner keeps the published image.
    for (const name of publishedServices.filter((name) => name !== "postgres" && name !== "cloud-local-v2")) {
      assert.equal(model.services[name].image, facts.values.OPL_CLOUD_IMAGE, name);
    }
    // The owners reach the qualification authority, not an external Sub2API.
    assert.equal(model.services.tenant.environment.OPL_SUB2API_URL, "https://cloud-local-v2:8443/sub2api");
    assert.equal(model.services["control-plane"].environment.OPL_SUB2API_BASE_URL, "https://cloud-local-v2:8443/sub2api");
    assert.equal(model.services.tenant.environment.OPL_GATEWAY_DIRECTORY_EMAIL, facts.values.OPL_QUALIFICATION_USER_EMAIL);
    // The published topology names the gateway container from the Compose
    // project name, and the qualification declares the same name.
    assert.equal(model.services.fabric.environment.OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER, "opl-qualification-1-1-control-plane-1");
    // The qualification authority keeps its own state in its own volume.
    assert.ok(model.volumes["opl-cloud-local-v2-qualification-sub2api"], "the fixture authority must own a volume");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("the installation facts are derived against the merged topology", async () => {
  const template = await readFile(resolve(root, "deploy/portable/opl-cloud.env.example"), "utf8");
  const published = await readFile(resolve(root, PUBLISHED_COMPOSE), "utf8");
  const overlay = await readFile(resolve(root, QUALIFICATION_OVERLAY), "utf8");
  const directory = await mkdtemp(join(tmpdir(), "opl-qualify-facts-"));
  try {
    const facts = qualificationFacts(root, template, `${published}\n${overlay}`, directory);
    // The published topology reads facts the retired topology did not, and the
    // qualification environment supplies every one of them.
    for (const name of ["OPL_SUB2API_BASE_URL", "OPL_SUB2API_USER_EMAIL", "OPL_SUB2API_USER_PASSWORD"]) {
      assert.ok(facts.values[name], `${name} must be supplied`);
      assert.equal(facts.unresolved.includes(name), false, `${name} must not be an unfilled fact`);
      assert.equal(facts.referenced.includes(name), true, `the published topology reads ${name}`);
    }
    // Every variable the merged topology reads is either declared by the
    // template or declared by the Compose reader itself.
    const merged = `${published}\n${overlay}`;
    const declared = new Set([...template.matchAll(/^([A-Za-z_][A-Za-z0-9_]*)=/gm)].map((match) => match[1]));
    const composeReader = new Set(["COMPOSE_PROJECT_NAME"]);
    const missing = localV2ReferencedVariables(merged).filter((name) => !declared.has(name) && !composeReader.has(name));
    assert.deepEqual(missing, [], "the merged topology must not read a variable nobody declares");
    // The facts tool refuses the merged topology when a fact is missing.
    const values = { ...facts.values };
    delete values.OPL_SUB2API_BASE_URL;
    assert.throws(() => localV2InstallationFacts({ template, compose: merged, values }),
      (error) => error.code === "local_v2_variable_unfilled" && error.message.includes("OPL_SUB2API_BASE_URL"));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("the overlay carries the fixture authority and only qualification facts", async () => {
  const template = await readFile(resolve(root, "deploy/portable/opl-cloud.env.example"), "utf8");
  const overlay = await readFile(resolve(root, QUALIFICATION_OVERLAY), "utf8");
  const declared = new Set([...template.matchAll(/^([A-Za-z_][A-Za-z0-9_]*)=/gm)].map((match) => match[1]));
  const missing = localV2ReferencedVariables(overlay).filter((name) => !declared.has(name));
  assert.deepEqual(missing, [], "the overlay must not read a variable the template does not declare");
  assert.match(overlay, /^ {2}sub2api-authority:$/m, "the overlay must carry the fixture authority");
});
