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
