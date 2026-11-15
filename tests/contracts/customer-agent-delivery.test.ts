import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";

/*
 * The customer agent delivery materials are the immutable artifact selection a
 * consumer deploys. They must stay inside the capability set the build owner
 * actually produced: a field the publisher never declared must never reappear
 * here, the platform fixture must never be presented as the delivered agent,
 * and the local lock must agree with the checked-in product bytes byte for byte.
 */

const PUBLICATION = "docs/delivery/applications/customer-agent.publication.json";
const REVISION = "docs/delivery/applications/customer-agent.revision.json";
const DESCRIPTOR = "docs/delivery/applications/customer-agent.deployment-descriptor.json";
const MATERIALS = "docs/delivery/customer-agent-delivery-materials.md";

const IMAGE_REFERENCE = /^[a-z0-9.-]+(?::[1-9][0-9]*)?\/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$/;
const DIGEST = /^sha256:[a-f0-9]{64}$/;

// The build's FROM input. Its CMD serves the platform fixture body, so it is
// the one digest that must never be presented as the delivered agent.
const FIXTURE_DIGEST = "sha256:ae7b48c14ccb26e108a69bbad0ff88f2adae45cc4dfcf220d85213ed02f6b7b3";
const FIXTURE_MARKER = "OPL Workspace READY";

async function json(path: string): Promise<Record<string, any>> {
  return JSON.parse(await readFile(path, "utf8"));
}

// The build owner stores the descriptor bytes and their digest together; the
// digest is sha256 over exactly those bytes, prefixed the same way.
function digestOf(bytes: Buffer): string {
  return "sha256:" + createHash("sha256").update(bytes).digest("hex");
}

test("the runtime descriptor declares exactly the supported capability set", async () => {
  const revision = await json(REVISION);
  assert.equal(revision.schemaVersion, 1);
  assert.equal(revision.platform, "linux/arm64");
  assert.ok(IMAGE_REFERENCE.test(revision.image), revision.image);
  // A publishing policy must name the entry it publishes; the platform never
  // guesses a port or a probe.
  assert.equal(revision.exposurePolicy, "anonymous");
  assert.equal(revision.entryPort, "http");
  assert.deepEqual(revision.ports, [{ name: "http", port: 3000, protocol: "TCP" }]);
  assert.deepEqual(revision.healthChecks, [{ port: 3000, path: "/" }]);
  // Exactly one persistent mount, and no permission request on it.
  assert.equal(revision.persistentMounts.length, 1);
  assert.deepEqual(Object.keys(revision.persistentMounts[0]).sort(), ["mountPath", "name", "readOnly"]);
  assert.equal(revision.persistentMounts[0].mountPath, "/data");
  assert.equal(revision.persistentMounts[0].readOnly, false);
  // Process identity was never declared by the publisher and never observed
  // running, so the descriptor states that absence instead of guessing one.
  assert.equal(revision.execution.declared, false);
  assert.equal(revision.execution.observed, false);
  assert.equal(revision.execution.userId, undefined);
  assert.equal(revision.execution.groupId, undefined);
});

test("the descriptor never claims a capability the publisher never declared", async () => {
  const revision = await json(REVISION);
  const encoded = JSON.stringify(revision);
  for (const forbidden of [
    "userId", "groupId", "init", "seccompProfile", "scratchMounts",
    "dependencies", "secretInputs", "configInputs", "privileged",
    "oci_index", "index_child", "multiplatform",
  ]) {
    assert.equal(encoded.includes(forbidden), false, `descriptor claims ${forbidden}`);
  }
});

test("the descriptor names the built agent, not the platform fixture", async () => {
  const revision = await json(REVISION);
  const publication = await json(PUBLICATION);
  assert.equal(revision.image.includes(FIXTURE_DIGEST), false);
  // One delivered component. The build inputs are build inputs: they ran
  // nothing, and they are not part of the delivery.
  assert.equal(publication.delivered_components.length, 1);
  const delivered = publication.delivered_components[0];
  assert.equal(delivered.digest, publication.digest);
  assert.equal(delivered.reference, revision.image);
  assert.ok(DIGEST.test(delivered.digest), delivered.digest);
  assert.equal(publication.build_inputs.runtime_base_image.digest, FIXTURE_DIGEST);
  assert.equal(publication.build_inputs.runtime_base_image.is_substitute, true);
  // The fixture is named as the thing that must never be presented as the
  // delivery, with the reason recorded next to it.
  assert.equal(
    publication.historical_must_not_be_presented_as_delivery.runtime_base_image.digest,
    FIXTURE_DIGEST,
  );
  // Nothing is retained as a predecessor, so nothing may be presented as one.
  assert.equal(publication.previous_retained.digest, undefined);
});

test("the local lock agrees with the checked-in product bytes byte for byte", async () => {
  const publication = await json(PUBLICATION);
  const revision = await json(REVISION);
  const descriptorBytes = await readFile(DESCRIPTOR);
  const descriptor = JSON.parse(descriptorBytes.toString("utf8"));

  // The digest of the checked-in bytes is the digest the build owner recorded.
  assert.equal(digestOf(descriptorBytes), publication.deployment_descriptor.digest);
  assert.equal(digestOf(descriptorBytes), "sha256:37f8c298f7513f24bda6da04d76cc183ffb09196181c8dcf9c5da1dc2a385b6f");
  // The product's own descriptor, the publication and the runtime descriptor
  // all name the same artifact.
  assert.equal(descriptor.artifact.digest, publication.digest);
  assert.equal(descriptor.artifact.digest, revision.image.slice(revision.image.indexOf("@") + 1));
  assert.equal(descriptor.artifact.repository, publication.repository);
  assert.equal(`${descriptor.artifact.platform.os}/${descriptor.artifact.platform.architecture}`, revision.platform);

  // The runtime descriptor is a faithful projection of the product's render:
  // every field the product emitted, and only the execution absence on top.
  const rendered = descriptor.applicationRevision;
  assert.deepEqual(Object.keys(revision).sort(), [...Object.keys(rendered), "execution"].sort());
  for (const [key, value] of Object.entries(rendered)) {
    assert.deepEqual(revision[key], value, `runtime descriptor diverges from the product render at ${key}`);
  }

  // An index identity and a platform manifest identity are two identities.
  // This publisher emits single-platform manifests, so there is no index to
  // name and no child manifest digest to stand in for one.
  assert.equal(publication.artifact_kind, "oci_image_manifest");
  assert.equal(publication.oci_index_digest, null);
  assert.equal(publication.oci_index_child_manifest_linux_arm64, null);
  assert.ok(publication.manifest_media_type.endsWith("image.manifest.v1+json"));
});

test("the delivered image runs the agent's runtime, not the fixture's command", async () => {
  const descriptor = await json(DESCRIPTOR);
  const revision = await json(REVISION);
  const materials = await readFile(MATERIALS, "utf8");

  // The build's entrypoint is the package's own runtime. The base image's CMD
  // (the fixture) is never the entrypoint of the delivered image.
  const command = descriptor.runtimeContract.buildRecipe.outputImageCommand;
  assert.deepEqual(command.entrypoint, ["/opt/opl/bin/runtime"]);
  assert.deepEqual(command.cmd, ["serve"]);

  // The served body must carry the agent's identity, and the fixture's marker
  // is the negative of that check: it proves the platform fixture is being
  // served instead of the agent.
  assert.ok(materials.includes(revision.applicationId), "materials must name the agent identity the served body carries");
  assert.ok(materials.includes(FIXTURE_MARKER), "materials must name the fixture marker as the negative signal");
  assert.equal(revision.applicationId.includes(FIXTURE_MARKER), false);
});
