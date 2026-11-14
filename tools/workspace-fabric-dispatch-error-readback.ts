import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { open, writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { candidateInstallationIdentity, canonicalJson, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";

const WORKSPACE_OPERATION_DOMAIN = "cloud-launch-readback:operation:v1";
const FABRIC_OPERATION_DOMAIN = "cloud-launch-readback:operation:v1";
const WORKSPACE_ID_DOMAIN = "console-customer-workspace-readback:workspace:v1";
const DIGEST = /^sha256:[0-9a-f]{64}$/;
const SHA = /^[0-9a-f]{40}$/;
const OPERATION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/;
const MAX_EXCERPT_BYTES = 1024 * 1024;
const SAFE_ERRORS = new Set([
  "compute_allocation_not_found",
  "tencent_read_compute_destroy_machine_failed",
  "tencent_read_compute_destroy_cvm_failed",
  "compute_destroy_status_readback_unavailable",
  "compute_destroy_status_readback_mismatch",
  "compute_destroy_status_fabric_identity_mismatch",
  "compute_destroy_status_retry_evidence_invalid",
  "compute_destroy_status_provider_identity_mismatch",
  "compute_destroy_status_provider_absence_mismatch",
  "compute_destroy_status_fabric_absence_evidence_invalid",
  "compute_readback_history_unavailable",
  "compute_readback_ownership_missing",
  "compute_readback_ownership_unavailable",
  "compute_readback_ownership_inactive",
  "compute_readback_ownership_invalid",
  "compute_readback_ownership_resource",
  "compute_readback_ownership_account",
  "compute_readback_ownership_workspace",
  "compute_readback_ownership_package",
  "compute_readback_ownership_node_pool",
  "compute_readback_ownership_machine",
  "compute_readback_ownership_instance",
  "compute_readback_ownership_node",
  "compute_readback_launch_unavailable",
  "compute_readback_launch_mismatch",
  "compute_readback_launch_binding_invalid",
  "compute_readback_launch_record_digest",
  "compute_readback_launch_record_invalid",
  "compute_readback_launch_stage_invalid",
  "compute_readback_identity_resource",
  "compute_readback_identity_account",
  "compute_readback_identity_workspace",
  "compute_readback_identity_package",
  "compute_readback_identity_provider",
  "compute_readback_identity_pool",
  "compute_readback_identity_node_pool",
  "compute_readback_identity_machine",
  "compute_readback_identity_node",
  "compute_readback_identity_private_ip",
  "compute_readback_identity_instance",
  "compute_readback_identity_instance_type",
  "compute_readback_identity_zone",
  "compute_readback_identity_cluster",
  "compute_readback_identity_region",
  "compute_readback_identity_account_tag",
  "compute_readback_identity_workspace_tag",
  "compute_readback_identity_resource_tag",
  "compute_readback_identity_operation_tag",
  "compute_readback_identity_machine_type",
  "compute_readback_identity_cpu",
  "compute_readback_identity_memory",
  "compute_destroy_recovery_identity_mismatch",
  "compute_destroy_recovery_phase_invalid",
  "compute_destroy_recovery_unconfirmed",
  "compute_destroy_finalization_identity_mismatch",
  "compute_final_release_not_eligible",
]);

type CandidateIdentity = ReturnType<typeof candidateInstallationIdentity>;
export type WorkspaceFabricDispatchErrorReceipt = {
  schemaVersion: 1;
  kind: "opl_cloud_workspace_fabric_dispatch_error_readback";
  status: "matched" | "blocked" | "unknown";
  candidate: CandidateIdentity;
  workspaceIdDigest: string;
  workspaceOperationIdDigest: string;
  fabricOperationIdDigest: string;
  fabricOperationUpdatedAt: string;
  excerptDigest: string;
  startedAt: string;
  completedAt: string;
  matchedCount: number;
  matches: Array<{ observedAt: string; errorToken: string | null; errorDigest: string }>;
};

function fail(): never { throw new Error("workspace_fabric_dispatch_error_readback_invalid"); }
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail();
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: readonly string[]) {
  const result = record(value);
  if (Object.keys(result).length !== keys.length || keys.some((key) => !Object.hasOwn(result, key))) fail();
  return result;
}
function hash(value: string | Buffer): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}
function identityDigest(domain: string, value: string): string {
  return hash(Buffer.from(`${domain}\0${value}`, "utf8"));
}
// kubectl's timestamp is the UTC observation clock. Go's wall-clock prefix
// has no timezone and must never be interpreted as UTC.
function instant(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const match = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?Z$/.exec(value);
  if (!match || !Number.isFinite(Date.parse(value))) return false;
  const milliseconds = `${match[1]}.${(match[2] || "").padEnd(3, "0").slice(0, 3)}Z`;
  return new Date(value).toISOString() === milliseconds;
}
function epochNanoseconds(value: string): bigint {
  const match = /^(.*?)(?:\.(\d{1,9}))?Z$/.exec(value)!;
  return BigInt(Date.parse(`${match[1]}Z`)) * 1_000_000n + BigInt((match[2] || "").padEnd(9, "0"));
}
function inOperationWindow(observedAt: string, updatedAt: string): boolean {
  const delta = epochNanoseconds(observedAt) - epochNanoseconds(updatedAt);
  return delta >= -60_000_000_000n && delta <= 60_000_000_000n;
}
function digest(value: unknown): value is string {
  return typeof value === "string" && DIGEST.test(value);
}

// Shared by the log diagnostic and the live owner read. Never expose the raw
// provider message: it may contain cloud identifiers, addresses or credentials.
export function projectComputeReadbackError(rawError: string): { errorToken: string | null; errorDigest: string } {
  if (!rawError || Buffer.byteLength(rawError, "utf8") > MAX_EXCERPT_BYTES) fail();
  const errorToken = [...SAFE_ERRORS].find(code => rawError === code || rawError.startsWith(`${code}:`)) ?? null;
  return { errorToken, errorDigest: hash(rawError) };
}

export function produceWorkspaceFabricDispatchErrorReadback(input: {
  candidate: unknown;
  binding: unknown;
  fabricOperationUpdatedAt: unknown;
  excerpt: unknown;
}): WorkspaceFabricDispatchErrorReceipt {
  exact(input, ["candidate", "binding", "fabricOperationUpdatedAt", "excerpt"]);
  const candidate = validateCloudCandidateReceipt(input.candidate);
  const binding = exact(input.binding, ["workspaceId", "workspaceOperationId", "fabricOperationId"]);
  if ([binding.workspaceId, binding.workspaceOperationId, binding.fabricOperationId].some((value) =>
    typeof value !== "string" || !OPERATION_ID.test(value))) fail();
  const workspaceId = binding.workspaceId as string;
  const workspaceOperationId = binding.workspaceOperationId as string;
  const fabricOperationId = binding.fabricOperationId as string;
  if (!instant(input.fabricOperationUpdatedAt)) fail();

  const excerpt = exact(input.excerpt, ["text", "sha256", "startedAt", "completedAt"]);
  if (typeof excerpt.text !== "string" || Buffer.byteLength(excerpt.text, "utf8") > MAX_EXCERPT_BYTES ||
      !digest(excerpt.sha256) || hash(Buffer.from(excerpt.text, "utf8")) !== excerpt.sha256 ||
      !instant(excerpt.startedAt) || !instant(excerpt.completedAt) || epochNanoseconds(excerpt.completedAt) < epochNanoseconds(excerpt.startedAt)) fail();

  const candidateIdentity = candidateInstallationIdentity(candidate);
  const matches: WorkspaceFabricDispatchErrorReceipt["matches"] = [];
  for (const line of (excerpt.text as string).split(/\r?\n/)) {
    const parsed = line.match(/^(\S+(?: \d\d:\d\d:\d\d(?:\.\d{1,9})?)?) (?:\d{4}\/\d\d\/\d\d \d\d:\d\d:\d\d(?:\.\d{1,9})? )?fabric resource deletion unavailable operation=([^\s]+) kind=compute: (.*)$/);
    if (!parsed || parsed[2] !== fabricOperationId) continue;
    const observedAt = parsed[1];
    if (!instant(observedAt) || epochNanoseconds(observedAt) > epochNanoseconds(excerpt.completedAt as string)) fail();
    if (!inOperationWindow(observedAt, input.fabricOperationUpdatedAt)) continue;
    const rawError = parsed[3];
    if (!rawError) fail();
    matches.push({ observedAt, ...projectComputeReadbackError(rawError) });
  }

  const receipt: WorkspaceFabricDispatchErrorReceipt = {
    schemaVersion: 1,
    kind: "opl_cloud_workspace_fabric_dispatch_error_readback",
    status: matches.length === 0 ? "unknown" : matches.every((match) => match.errorToken !== null) ? "matched" : "blocked",
    candidate: candidateIdentity,
    workspaceIdDigest: identityDigest(WORKSPACE_ID_DOMAIN, workspaceId),
    workspaceOperationIdDigest: identityDigest(WORKSPACE_OPERATION_DOMAIN, workspaceOperationId),
    fabricOperationIdDigest: identityDigest(FABRIC_OPERATION_DOMAIN, fabricOperationId),
    fabricOperationUpdatedAt: input.fabricOperationUpdatedAt,
    excerptDigest: hash(Buffer.from(excerpt.text as string, "utf8")),
    startedAt: excerpt.startedAt as string,
    completedAt: excerpt.completedAt as string,
    matchedCount: matches.length,
    matches,
  };
  return validateWorkspaceFabricDispatchErrorReadbackReceipt(receipt);
}

export function validateWorkspaceFabricDispatchErrorReadbackReceipt(value: unknown): WorkspaceFabricDispatchErrorReceipt {
  const receipt = exact(value, ["schemaVersion", "kind", "status", "candidate", "workspaceIdDigest", "workspaceOperationIdDigest", "fabricOperationIdDigest", "fabricOperationUpdatedAt", "excerptDigest", "startedAt", "completedAt", "matchedCount", "matches"]);
  const candidate = exact(receipt.candidate, ["candidateReceiptDigest", "cloudImageDigest", "cloudImageRef", "cloudImageRevision", "cloudRuntimeChildDigest", "productSha", "productTree"]);
  if (receipt.schemaVersion !== 1 || receipt.kind !== "opl_cloud_workspace_fabric_dispatch_error_readback" ||
      (typeof receipt.status !== "string" || !["matched", "blocked", "unknown"].includes(receipt.status)) || !digest(receipt.workspaceIdDigest) ||
      !digest(receipt.workspaceOperationIdDigest) || !digest(receipt.fabricOperationIdDigest) ||
      !instant(receipt.fabricOperationUpdatedAt) || !digest(receipt.excerptDigest) || !instant(receipt.startedAt) || !instant(receipt.completedAt) ||
      epochNanoseconds(receipt.completedAt) < epochNanoseconds(receipt.startedAt) ||
      !Number.isSafeInteger(receipt.matchedCount) || (receipt.matchedCount as number) < 0 || !Array.isArray(receipt.matches) ||
      receipt.matchedCount !== receipt.matches.length || receipt.matches.length > MAX_EXCERPT_BYTES ||
      !digest(candidate.candidateReceiptDigest) || typeof candidate.productSha !== "string" || !SHA.test(candidate.productSha) || typeof candidate.productTree !== "string" || !SHA.test(candidate.productTree) ||
      !digest(candidate.cloudImageDigest) || candidate.cloudImageRevision !== candidate.productSha ||
      candidate.cloudImageRef !== `ghcr.io/gaofeng21cn/one-person-lab-cloud@${candidate.cloudImageDigest}` ||
      !digest(candidate.cloudRuntimeChildDigest)) fail();

  let allSafe = (receipt.matchedCount as number) > 0;
  for (const item of receipt.matches) {
    const match = exact(item, ["observedAt", "errorToken", "errorDigest"]);
    if (!instant(match.observedAt) || !inOperationWindow(match.observedAt, receipt.fabricOperationUpdatedAt as string) ||
        epochNanoseconds(match.observedAt) > epochNanoseconds(receipt.completedAt as string) || !digest(match.errorDigest) ||
        (match.errorToken !== null && (typeof match.errorToken !== "string" || !SAFE_ERRORS.has(match.errorToken)))) fail();
    if (match.errorToken === null) allSafe = false;
  }
  if ((receipt.status === "matched") !== allSafe ||
      (receipt.status === "unknown" && receipt.matchedCount !== 0) ||
      (receipt.status === "blocked" && (receipt.matchedCount === 0 || allSafe))) fail();
  return receipt as unknown as WorkspaceFabricDispatchErrorReceipt;
}

// The protected host supplies this bounded input file; only the Cloud receipt
// may leave that host. The CLI never prints private input or downstream errors.
async function readJson(path: string): Promise<unknown> {
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    if (!stat.isFile() || stat.size > 8 * MAX_EXCERPT_BYTES) fail();
    const bytes = Buffer.alloc(8 * MAX_EXCERPT_BYTES + 1);
    let count = 0;
    while (count < bytes.length) {
      const result = await file.read(bytes, count, bytes.length - count, null);
      if (result.bytesRead === 0) break;
      count += result.bytesRead;
    }
    if (count > 8 * MAX_EXCERPT_BYTES) fail();
    return JSON.parse(bytes.subarray(0, count).toString("utf8"));
  } finally { await file.close(); }
}
export async function runWorkspaceFabricDispatchErrorReadbackCli(args: string[]): Promise<number> {
  if (args.length === 5 && args[0] === "run" && args[1] === "--input" && args[3] === "--output" && args[2] && args[4]) {
    const input = await readJson(args[2]);
    const receipt = produceWorkspaceFabricDispatchErrorReadback(input as Parameters<typeof produceWorkspaceFabricDispatchErrorReadback>[0]);
    await writeFile(args[4], `${canonicalJson(receipt)}\n`, { mode: 0o600, flag: "wx" });
    return receipt.status === "matched" ? 0 : 1;
  }
  if (args.length === 3 && args[0] === "validate" && args[1] === "--receipt" && args[2]) {
    validateWorkspaceFabricDispatchErrorReadbackReceipt(await readJson(args[2]));
    return 0;
  }
  fail();
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  runWorkspaceFabricDispatchErrorReadbackCli(process.argv.slice(2)).then(code => { process.exitCode = code; }, () => {
    console.error("workspace_fabric_dispatch_error_readback_invalid");
    process.exitCode = 1;
  });
}
