export interface WebuiAdmissionInput {
  id: string;
  status?: string;
  artifactDigest?: string;
  port?: number;
  healthPath?: string;
  runtimeAbiVersions?: string[];
  packageFormatVersions?: string[];
}

export interface RuntimeAdmissionInput {
  id: string;
  status?: string;
  artifactDigest?: string;
  runtimeAbiVersion?: string;
  packageFormatVersions?: string[];
}

export interface AdmissionDecision {
  accepted: boolean;
  reasons: string[];
}

const IMMUTABLE_DIGEST = /^sha256:[a-f0-9]{64}$/;

export function isImmutableDigest(value: unknown): value is string {
  return typeof value === "string" && IMMUTABLE_DIGEST.test(value);
}

export function admitWebuiSelection(webui: WebuiAdmissionInput, runtime?: RuntimeAdmissionInput): AdmissionDecision {
  const reasons: string[] = [];
  if (webui.status !== "approved") reasons.push("webui_not_approved");
  if (!isImmutableDigest(webui.artifactDigest)) reasons.push("webui_artifact_digest_not_immutable");
  if (webui.port !== 3000) reasons.push("webui_port_not_3000");
  if (webui.healthPath !== "/healthz") reasons.push("webui_health_path_not_healthz");
  if (!webui.runtimeAbiVersions?.length) reasons.push("webui_runtime_abi_versions_missing");
  if (!webui.packageFormatVersions?.length) reasons.push("webui_package_format_versions_missing");
  if (runtime) {
    if (runtime.status !== "approved") reasons.push("runtime_not_approved");
    if (!isImmutableDigest(runtime.artifactDigest)) reasons.push("runtime_artifact_digest_not_immutable");
    if (!runtime.runtimeAbiVersion || !webui.runtimeAbiVersions?.includes(runtime.runtimeAbiVersion)) reasons.push("runtime_abi_not_supported_by_webui");
    if (!runtime.packageFormatVersions?.some((format) => webui.packageFormatVersions?.includes(format))) reasons.push("runtime_package_format_not_supported_by_webui");
  }
  return { accepted: reasons.length === 0, reasons };
}

export interface EffectiveRuntimeRow {
  id: string;
  name?: string;
  versionLabel?: string;
  status?: string;
  artifactDigest?: string;
  defaultForNewBuilds?: boolean;
}

export interface EffectiveInput {
  id: string;
  name?: string;
  versionLabel?: string;
  artifactDigest: string;
}

export interface EffectiveBuildDefaults {
  runtime?: EffectiveInput;
  webui?: EffectiveInput;
  reasons: string[];
}

function effectiveInput(row: EffectiveRuntimeRow): EffectiveInput {
  return { id: row.id, name: row.name, versionLabel: row.versionLabel, artifactDigest: row.artifactDigest as string };
}

/**
 * Resolve the exact inputs of one new Agent Build from the Runtime Control
 * owner's effective policy.
 *
 * The Runtime half is read from the member-authorized Runtime catalog, where
 * Runtime Control itself marks the one runtime its effective policy
 * (`runtime_control.catalog_policies`) names with `defaultForNewBuilds`. The
 * Console therefore reports the owner's own resolution and never substitutes
 * another approved release.
 *
 * The same policy also names the effective default WebUI. The only route that
 * serves `BuildRuntimePolicy` answers a platform administrator session, so a
 * customer session has no authorized read for that half yet; the caller passes
 * what the owner served it (`null` when there is none) and the Console keeps the
 * Build command unavailable instead of guessing an approved catalog entry.
 */
export function resolveEffectiveBuildDefaults(runtimeRows: EffectiveRuntimeRow[], effectiveWebuiDefault: EffectiveInput | null): EffectiveBuildDefaults {
  const reasons: string[] = [];
  let runtime: EffectiveInput | undefined;
  const flagged = runtimeRows.filter((row) => row.defaultForNewBuilds === true);
  if (flagged.length === 0) reasons.push("effective_default_runtime_missing");
  else if (flagged.length > 1) reasons.push("effective_default_runtime_ambiguous");
  else if (flagged[0].status !== "approved") reasons.push("effective_default_runtime_not_approved");
  else if (!isImmutableDigest(flagged[0].artifactDigest)) reasons.push("effective_default_runtime_digest_not_immutable");
  else runtime = effectiveInput(flagged[0]);
  let webui: EffectiveInput | undefined;
  if (effectiveWebuiDefault) webui = effectiveWebuiDefault;
  else reasons.push("effective_default_webui_not_readable");
  return { runtime, webui, reasons };
}
