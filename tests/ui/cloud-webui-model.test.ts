import assert from "node:assert/strict";
import test from "node:test";
import { admitWebuiSelection, isImmutableDigest, resolveEffectiveBuildDefaults } from "../../apps/console-ui/src/app/cloud-webui-model.ts";

const digest = "sha256:" + "a".repeat(64);
const webui = { id: "webui-1", status: "approved", artifactDigest: digest, port: 3000, healthPath: "/healthz", runtimeAbiVersions: ["opl-runtime-abi/v1"], packageFormatVersions: ["opl-agent-package/v1"] };
const runtime = { id: "runtime-1", status: "approved", artifactDigest: digest, runtimeAbiVersion: "opl-runtime-abi/v1", packageFormatVersions: ["opl-agent-package/v1"] };

test("Cloud WebUI admission accepts exact immutable compatible inputs", () => {
  assert.deepEqual(admitWebuiSelection(webui, runtime), { accepted: true, reasons: [] });
  assert.equal(isImmutableDigest(digest), true);
});

test("Cloud WebUI admission refuses mutable or incompatible inputs", () => {
  const decision = admitWebuiSelection({ ...webui, artifactDigest: "ghcr.io/example/webui:latest", port: 8080 }, { ...runtime, runtimeAbiVersion: "opl-runtime-abi/v2" });
  assert.deepEqual(decision.reasons, ["webui_artifact_digest_not_immutable", "webui_port_not_3000", "runtime_abi_not_supported_by_webui"]);
});

test("Cloud publisher resolves the new Build's Runtime from the owner's effective policy projection", () => {
  const approved = { id: "runtime-approved", name: "OPL App Runtime", versionLabel: "26.9.26", status: "approved", artifactDigest: digest };
  const resolution = resolveEffectiveBuildDefaults([
    approved,
    { id: "runtime-other", status: "approved", artifactDigest: digest },
    { id: "runtime-mutable", status: "approved", artifactDigest: "ghcr.io/example/runtime:latest" }
  ].map((row) => ({ ...row, defaultForNewBuilds: row.id === "runtime-approved" })), null);
  assert.deepEqual(resolution.runtime, { id: "runtime-approved", name: "OPL App Runtime", versionLabel: "26.9.26", artifactDigest: digest });
  assert.equal(resolution.webui, undefined);
  // No customer session may read the effective default WebUI today, so the
  // Console reports the missing owner fact instead of guessing a catalog row.
  assert.deepEqual(resolution.reasons, ["effective_default_webui_not_readable"]);
});

test("Cloud publisher refuses to guess when the owner policy names no usable default", () => {
  const approved = { id: "runtime-approved", status: "approved", artifactDigest: digest, defaultForNewBuilds: true };
  assert.deepEqual(resolveEffectiveBuildDefaults([{ ...approved, defaultForNewBuilds: false }], null).reasons, ["effective_default_runtime_missing", "effective_default_webui_not_readable"]);
  assert.deepEqual(resolveEffectiveBuildDefaults([approved, { ...approved, id: "runtime-second" }], null).reasons, ["effective_default_runtime_ambiguous", "effective_default_webui_not_readable"]);
  assert.deepEqual(resolveEffectiveBuildDefaults([{ ...approved, status: "revoked" }], null).reasons, ["effective_default_runtime_not_approved", "effective_default_webui_not_readable"]);
  assert.deepEqual(resolveEffectiveBuildDefaults([{ ...approved, artifactDigest: "ghcr.io/example/runtime:latest" }], null).reasons, ["effective_default_runtime_digest_not_immutable", "effective_default_webui_not_readable"]);
});

test("Cloud publisher carries an owner-served effective default WebUI into the frozen inputs", () => {
  const runtime = { id: "runtime-approved", status: "approved", artifactDigest: digest, defaultForNewBuilds: true };
  const resolution = resolveEffectiveBuildDefaults([runtime], { id: "webui-default", name: "OPL WebUI", versionLabel: "1.4.0", artifactDigest: digest });
  assert.deepEqual(resolution.reasons, []);
  assert.equal(resolution.runtime?.id, "runtime-approved");
  assert.deepEqual(resolution.webui, { id: "webui-default", name: "OPL WebUI", versionLabel: "1.4.0", artifactDigest: digest });
});
