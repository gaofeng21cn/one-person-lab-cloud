import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  bundleDeclaredDefaults,
  bundleInstallabilityFacts,
  installabilityReceiptDigest,
  parseInstallationFactsRequired,
  resolveBundleInstallabilityDeclaration,
} from "../../tools/bundle-installability.ts";

const contract = JSON.parse(await readFile("packages/contracts/opl-cloud-distribution-contract.json", "utf8"));
const template = await readFile("deploy/portable/opl-cloud.env.example", "utf8");
const publishedCompose = await readFile("deploy/portable/compose.cloud.yaml", "utf8");
const localV2Compose = await readFile("deploy/portable/compose.local-v2.yaml", "utf8");
const manifest = {
  product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: "a".repeat(40), tree: "b".repeat(40) },
  cloudImage: { indexRef: `ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:${"c".repeat(64)}` },
};

// The Compose files the published bundle is expected to carry: the one the
// installer runs and the one the qualification declares. Everything else the
// retired formats carried is an overlay that must not come back.
const publishedComposeFiles = ["compose.cloud.yaml", "compose.local-v2.yaml"];

// The overlays the retired formats carried are read from those formats, so
// the list of what must not come back is not written down here. They stay in
// the historical formats so the receipts published against them keep
// verifying.
function retiredComposeOverlays() {
  const current = contract.bundleFormats.current;
  return [...new Set(
    Object.entries(contract.bundleFormats.formats)
      .filter(([name]) => name !== current)
      .flatMap(([, spec]) => spec.candidateAssets)
      .filter((name) => name.startsWith("compose") && !publishedComposeFiles.includes(name))
  )].sort();
}

test("the bundle's own format decides, and the authority contract has to declare it", () => {
  // The bundle names its own format. A frozen Candidate built before the
  // current format still verifies against the declaration it was built for,
  // without its bytes changing.
  const frozen = { bundleFormats: { current: "portable-v1" } };
  assert.equal(resolveBundleInstallabilityDeclaration(frozen, contract).bundleFormat, "portable-v1");
  // The current bundle resolves against the current declaration.
  const current = { bundleFormats: { current: contract.bundleFormats.current } };
  assert.equal(resolveBundleInstallabilityDeclaration(current, contract).bundleFormat, contract.bundleFormats.current);
  // A format the authority does not declare proves nothing.
  assert.throws(
    () => resolveBundleInstallabilityDeclaration({ bundleFormats: { current: "not-a-format" } }, contract),
    { code: "bundle_installability_not_declared" }
  );
  // A declared format without an installability declaration is not declared.
  const withoutDeclaration = JSON.parse(JSON.stringify(contract));
  delete withoutDeclaration.bundleFormats.formats[contract.bundleFormats.current].installability;
  assert.throws(
    () => resolveBundleInstallabilityDeclaration(current, withoutDeclaration),
    { code: "bundle_installability_not_declared" }
  );
});

test("the published bundle carries the installation Compose files and no retired overlay", () => {
  const format = contract.bundleFormats.formats[contract.bundleFormats.current];
  assert.deepEqual(
    format.candidateAssets.filter((name: string) => name.startsWith("compose")),
    publishedComposeFiles
  );
  assert.deepEqual(
    format.releaseAssets.filter((name: string) => name.startsWith("compose")),
    publishedComposeFiles
  );
  for (const name of retiredComposeOverlays()) {
    assert.ok(!format.candidateAssets.includes(name), `the published bundle must not carry the retired overlay ${name}`);
    assert.ok(!format.releaseAssets.includes(name), `the published release must not carry the retired overlay ${name}`);
  }
  // The entrypoint the installer runs has to be in the bundle it installs.
  assert.ok(format.candidateAssets.includes(contract.portableInstallation.entrypoint));
});

function facts() {
  return bundleInstallabilityFacts({ template, publishedCompose, localV2Compose, manifest });
}

test("the derived fixture covers exactly the facts the contract declares as manual", () => {
  const declaration = contract.bundleFormats.formats[contract.bundleFormats.current].installability;
  assert.equal(declaration.installer, "install.sh");
  assert.equal(declaration.entrypoint, "install.mjs");
  assert.equal(declaration.publicationRequiresVerifiedInstallability, true);
  // The manual-fact count is the installer's own list, and the fixture has to
  // answer exactly that list: a fact the installer asks for and this fixture
  // cannot answer is a gap, and one the installer no longer asks for is stale.
  assert.equal(facts().values.size, declaration.manualFacts);
});

test("every derived fact carries a value that is not a placeholder", () => {
  const derived = facts();
  assert.equal(derived.values.size, 13);
  for (const [name, value] of derived.values) {
    assert.ok(value.length > 0, `${name} is empty`);
    assert.doesNotMatch(value, /<[^>\n]+>/, `${name} still holds a placeholder`);
    assert.doesNotMatch(value, /[\r\n]/, `${name} holds a line break`);
    assert.ok(derived.sources.get(name), `${name} has no declared source`);
  }
});

test("every derived value names the declaration it came from", () => {
  const sources = new Set(facts().sources.values());
  for (const source of sources) {
    assert.match(source, /^(manifest\.imageRef|bundle\.default|derived\.[a-zA-Z]+|template\.derived)$/);
  }
  // The Workspace image and the approved image list are the bundle's own image
  // reference, not a second invented one.
  const derived = facts();
  assert.equal(derived.values.get("OPL_WORKSPACE_IMAGE"), manifest.cloudImage.indexRef);
  assert.equal(derived.values.get("OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES"), manifest.cloudImage.indexRef);
});

test("the fixture takes the bundle's own declared defaults where the bundle declares them", () => {
  const defaults = bundleDeclaredDefaults(localV2Compose);
  assert.equal(defaults.get("OPL_WORKSPACE_REGISTRY_HOST"), "registry.local-v2.test");
  assert.equal(defaults.get("OPL_WORKSPACE_REGISTRY_NAMESPACE"), "opl");
  assert.equal(defaults.get("OPL_GATEWAY_WORKSPACE_KEY_GROUP"), "Codex");
  const derived = facts();
  assert.equal(derived.values.get("OPL_WORKSPACE_REGISTRY_HOST"), defaults.get("OPL_WORKSPACE_REGISTRY_HOST"));
  assert.equal(derived.values.get("OPL_GATEWAY_WORKSPACE_KEY_GROUP"), defaults.get("OPL_GATEWAY_WORKSPACE_KEY_GROUP"));
});

test("the derived profiles are the declared ones, with the declared billing mode", () => {
  const derived = facts();
  const catalog = JSON.parse(derived.values.get("OPL_RESOURCE_CATALOG_PROFILE_JSON"));
  assert.equal(catalog.billingMode, "LOCAL_NO_CHARGE");
  assert.equal(catalog.provider, "local-docker");
  assert.equal(catalog.providerCapabilityVersion, "local-docker/v1");
  const provider = JSON.parse(derived.values.get("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON"));
  assert.equal(provider.accountBindings[0].tenantId, "opl-cloud-installability");
  assert.doesNotMatch(derived.values.get("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON"), /<[^>\n]+>/);
});

test("the fixture refuses to derive anything from a placeholder", () => {
  // A template that carries a placeholder where the bundle declares a value is
  // the defect #1197 found: reading a placeholder as a fact. The derivation
  // reads the bundle's declared value, never the placeholder.
  const placeholderTemplate = template.replace(
    "OPL_CAPABILITY_OBJECT_INTERNAL_URL=https://cloud-local-v2:8443/objects",
    "OPL_CAPABILITY_OBJECT_INTERNAL_URL=<replace-with-internal-object-origin>",
  );
  assert.throws(() => bundleInstallabilityFacts({
    template: placeholderTemplate, publishedCompose, localV2Compose, manifest,
  }), /bundle_installability_object_internal_url_missing/);
});

test("the installer names the facts it cannot fill, and only those names are read", () => {
  const stderr = [
    "install.mjs: installation_facts_required:",
    "OPL_CAPABILITY_OBJECT_URL",
    "OPL_WORKSPACE_DOMAIN",
    "",
  ].join("\n");
  assert.deepEqual(parseInstallationFactsRequired(stderr), ["OPL_CAPABILITY_OBJECT_URL", "OPL_WORKSPACE_DOMAIN"]);
  assert.deepEqual(parseInstallationFactsRequired("install.mjs: some other failure"), []);
});

test("the receipt digest binds the receipt bytes", () => {
  const receipt = { schemaVersion: 1, kind: "opl_cloud_bundle_installability", manualFacts: 13 };
  const digest = installabilityReceiptDigest(receipt);
  assert.match(digest, /^sha256:[0-9a-f]{64}$/);
  assert.equal(digest, installabilityReceiptDigest(receipt));
  assert.notEqual(digest, installabilityReceiptDigest({ ...receipt, manualFacts: 12 }));
});
