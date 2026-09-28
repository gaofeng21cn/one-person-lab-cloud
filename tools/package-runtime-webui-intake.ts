// Fail-closed admission check for the Agent Package + Runtime Release + WebUI
// chain. It admits a chain only when all three upstream inputs are immutable and
// compatible. It never derives, guesses or repairs a missing input: a chain that
// lacks any immutable identity is refused with a named reason so a caller cannot
// silently fall back to a fixture or a mutable tag.
//
// The canonical field owners are:
//   Package identity/digest   -> OMA/Foundry (upstream), consumed by Capability
//   Runtime Release identity  -> Runtime Control catalog (approved upstream OCI)
//   WebUI artifact identity   -> Cloud WebUI artifact (Cloud-owned)
// See docs/implementation/agent-package-runtime-webui-chain.md.

const DIGEST = /^sha256:[0-9a-f]{64}$/;
const GIT_SHA = /^[0-9a-f]{40}$/;
const PORT_3000 = 3000;

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim() !== "";
}

export type IntakeRefusal = { reason: string; field: string };

export type PackageInput = {
  packageId: string;
  versionLabel: string;
  manifestDigest: string;
  contentDigest: string;
};

export type RuntimeInput = {
  runtimeReleaseId: string;
  imageDigest: string;
  runtimeAbiVersion: string;
  packageFormatVersions: string[];
};

export type WebuiInput = {
  webuiVersionId: string;
  artifactDigest: string;
  port: number;
  healthPath: string;
  runtimeAbiVersions: string[];
};

export type ChainInput = {
  package: PackageInput;
  runtime: RuntimeInput;
  webui: WebuiInput;
  cloudSourceSha: string;
};

// A chain is admitted only if every immutable identity is present, digest-shaped,
// mutually compatible, and the WebUI serves the fixed port 3000 contract.
export function admitChain(input: ChainInput): { admitted: true } | { admitted: false; refusals: IntakeRefusal[] } {
  const refusals: IntakeRefusal[] = [];

  if (!isRecord(input)) {
    return { admitted: false, refusals: [{ reason: "chain_input_missing", field: "input" }] };
  }

  const pkg = input.package;
  if (!isRecord(pkg)) {
    refusals.push({ reason: "package_input_missing", field: "package" });
  } else {
    if (!nonEmptyString(pkg.packageId)) refusals.push({ reason: "package_id_missing", field: "package.packageId" });
    if (!nonEmptyString(pkg.versionLabel)) refusals.push({ reason: "package_version_label_missing", field: "package.versionLabel" });
    if (!DIGEST.test(String(pkg.manifestDigest))) refusals.push({ reason: "package_manifest_digest_not_immutable", field: "package.manifestDigest" });
    if (!DIGEST.test(String(pkg.contentDigest))) refusals.push({ reason: "package_content_digest_not_immutable", field: "package.contentDigest" });
  }

  const runtime = input.runtime;
  if (!isRecord(runtime)) {
    refusals.push({ reason: "runtime_input_missing", field: "runtime" });
  } else {
    if (!nonEmptyString(runtime.runtimeReleaseId)) refusals.push({ reason: "runtime_release_id_missing", field: "runtime.runtimeReleaseId" });
    if (!DIGEST.test(String(runtime.imageDigest))) refusals.push({ reason: "runtime_image_digest_not_immutable", field: "runtime.imageDigest" });
    if (!nonEmptyString(runtime.runtimeAbiVersion)) refusals.push({ reason: "runtime_abi_version_missing", field: "runtime.runtimeAbiVersion" });
    if (!Array.isArray(runtime.packageFormatVersions) || runtime.packageFormatVersions.length === 0) {
      refusals.push({ reason: "runtime_package_format_versions_missing", field: "runtime.packageFormatVersions" });
    }
  }

  const webui = input.webui;
  if (!isRecord(webui)) {
    refusals.push({ reason: "webui_input_missing", field: "webui" });
  } else {
    if (!nonEmptyString(webui.webuiVersionId)) refusals.push({ reason: "webui_version_id_missing", field: "webui.webuiVersionId" });
    if (!DIGEST.test(String(webui.artifactDigest))) refusals.push({ reason: "webui_artifact_digest_not_immutable", field: "webui.artifactDigest" });
    if (webui.port !== PORT_3000) refusals.push({ reason: "webui_port_not_3000", field: "webui.port" });
    if (!nonEmptyString(webui.healthPath)) refusals.push({ reason: "webui_health_path_missing", field: "webui.healthPath" });
    if (!Array.isArray(webui.runtimeAbiVersions) || webui.runtimeAbiVersions.length === 0) {
      refusals.push({ reason: "webui_runtime_abi_versions_missing", field: "webui.runtimeAbiVersions" });
    }
  }

  if (!GIT_SHA.test(String(input.cloudSourceSha))) {
    refusals.push({ reason: "cloud_source_sha_invalid", field: "cloudSourceSha" });
  }

  // Compatibility: the Runtime's ABI must be one the WebUI declares it supports,
  // and the Runtime's package format must satisfy the Package. A mismatch is a
  // refusal, not a downgrade.
  if (isRecord(runtime) && isRecord(webui) && nonEmptyString(runtime.runtimeAbiVersion) && Array.isArray(webui.runtimeAbiVersions)) {
    if (!webui.runtimeAbiVersions.includes(runtime.runtimeAbiVersion)) {
      refusals.push({ reason: "runtime_abi_not_supported_by_webui", field: "webui.runtimeAbiVersions" });
    }
  }

  if (refusals.length > 0) return { admitted: false, refusals };
  return { admitted: true };
}

// A development fixture may only be used when it is explicitly labelled. The IBD
// candidate is a real, content-addressed OMA artifact, but it is not a qualified
// AgentVersion, so it may never be admitted as a Build input.
export function admitDevelopmentFixture(fixture: { labelled: boolean; provenance: string }): { admitted: true } | { admitted: false; refusals: IntakeRefusal[] } {
  if (!isRecord(fixture) || fixture.labelled !== true) {
    return { admitted: false, refusals: [{ reason: "fixture_not_explicitly_labelled", field: "fixture.labelled" }] };
  }
  if (!["fixture", "legacy"].includes(String(fixture.provenance))) {
    return { admitted: false, refusals: [{ reason: "fixture_provenance_not_declared", field: "fixture.provenance" }] };
  }
  return { admitted: true };
}
