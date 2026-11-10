import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { candidateInstallationIdentity, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";
import { projectComputeReadbackError } from "./workspace-fabric-dispatch-error-readback.ts";
import {
  workspaceComputeBindingSql, decodeWorkspaceComputeBinding,
  fabricComputeBindingSql, decodeFabricComputeBinding,
} from "./workspace-compute-readback-binding.ts";

const SHA = /^[0-9a-f]{40}$/;
const DIGEST = /^sha256:[0-9a-f]{64}$/;
const ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/;
const WORKFLOW = ".github/workflows/cloud-workspace-compute-readback.yml";
export const COMPUTE_READBACK_STATUS = Object.freeze({ OBSERVED: "observed", BLOCKED: "blocked" } as const);
export const COMPUTE_READBACK_ERROR = Object.freeze({
  INPUT: "compute_live_input_invalid",
  BINDING: "compute_live_binding_invalid", // Retained for previously persisted receipts.
  WORKSPACE_ACCESS: "compute_live_workspace_access_unavailable",
  WORKSPACE_BINDING: "compute_live_workspace_binding_invalid",
  FABRIC_ACCESS: "compute_live_fabric_access_unavailable",
  FABRIC_BINDING: "compute_live_fabric_binding_invalid",
  ACCESS: "compute_live_access_unavailable", IMAGE: "compute_live_image_mismatch",
  HTTP: "compute_live_owner_http_failed", RESPONSE: "compute_live_owner_response_invalid",
  NONE: "none", INVALID: "workspace_compute_live_readback_invalid",
} as const);
type DatabaseOwner = "workspace" | "fabric";
const DATABASE_FAILURE_KINDS = [
  "url_invalid", "url_parameter_invalid", "private_file_missing", "private_file_unreadable",
  "client_required", "client_spawn_failed", "client_interrupted", "target_session_attrs_invalid",
  "dns_unavailable", "connection_refused", "connection_timeout", "authentication_failed",
  "permission_denied", "schema_unavailable", "unclassified",
] as const;
type DatabaseErrorCode = `compute_live_${DatabaseOwner}_database_${
  typeof DATABASE_FAILURE_KINDS[number] | `sqlstate_${string}` | `psql_exit_${string}`
}`;
type ErrorCode = typeof COMPUTE_READBACK_ERROR[keyof typeof COMPUTE_READBACK_ERROR] | DatabaseErrorCode;
type CandidateIdentity = ReturnType<typeof candidateInstallationIdentity>;
export type ComputeReadbackProvenance = {
  diagnosticSourceSha: string; instanceRepository: "gaofeng21cn/opl-instance-medopl";
  instanceSha: string; runId: string; runAttempt: string; workflowPath: typeof WORKFLOW;
};
export type ComputeReadbackPin = {
  podUid: string; deploymentUid: string; replicaSetUid: string;
  imageIndexDigest: string; platformDigest: string; runtimeDigest: string;
};
export type ComputeReadbackAccess = {
  select(owner: "workspace" | "fabric", sql: string): Promise<string[]>;
  withFabric<T>(image: { indexRef: string; indexDigest: string; platforms: Array<{ platform: string; digest: string }> },
    consume: (session: { get(path: string): Promise<{ status: number; body: unknown }> }) => Promise<T>): Promise<{ pin: ComputeReadbackPin; value: T }>;
};
type Cell = { value: string | null; digest: string | null };
type Observation = {
  machineType: "NativeCVM" | "Native" | "CXM"; machinePresent: boolean;
  allocationStatus: Cell; tkeStatus: Cell; cvmStatus: Cell; isolatedSource: Cell;
  observedAt: string; readbackIdDigest: string; providerRequestIdDigest: string;
  machineRequestIdDigest: string; cvmRequestIdDigest: string | null;
};
type Installation = {
  podUidDigest: string; deploymentUidDigest: string; replicaSetUidDigest: string;
  imageIndexDigest: string; platformDigest: string; runtimeDigest: string;
};
export type WorkspaceComputeLiveReadbackReceipt = {
  schemaVersion: 1; kind: "opl_cloud_workspace_compute_live_readback";
  scope: "compute_destroy_status"; status: typeof COMPUTE_READBACK_STATUS[keyof typeof COMPUTE_READBACK_STATUS];
  errorCode: ErrorCode; candidate: CandidateIdentity; provenance: ComputeReadbackProvenance;
  workspaceIdDigest: string; workspaceOperationIdDigest: string;
  fabricOperationIdDigest: string | null; computeIdDigest: string | null;
  startedAt: string; completedAt: string; installation: Installation | null;
  httpStatus: number | null; failure: { errorToken: string | null; errorDigest: string } | null;
  observation: Observation | null;
};
function fail(code: ErrorCode = COMPUTE_READBACK_ERROR.INVALID): never { throw new Error(code); }
function record(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v)) fail();
  return v as Record<string, unknown>;
}
function exact(v: unknown, keys: readonly string[]) {
  const r = record(v);
  if (Object.keys(r).length !== keys.length || keys.some(k => !Object.hasOwn(r, k))) fail();
  return r;
}
function text(v: unknown): v is string { return typeof v === "string" && v.length > 0 && v.length <= 4096; }
function databaseErrorCode(owner: DatabaseOwner, error: unknown): ErrorCode {
  const unavailable = owner === "workspace" ? COMPUTE_READBACK_ERROR.WORKSPACE_ACCESS : COMPUTE_READBACK_ERROR.FABRIC_ACCESS;
  const value = error instanceof Error ? error.cause : null;
  if (!value || typeof value !== "object" || Array.isArray(value)) return unavailable;
  const cause = value as Record<string, unknown>;
  const keys = ["kind", "phase", "clientCode", "exitCode", "signal", "stderr"];
  if (Object.keys(cause).length !== keys.length || keys.some(key => !Object.hasOwn(cause, key)) ||
      cause.kind !== "opl_compute_database_transport_failure" || typeof cause.phase !== "string" || !["private_file", "connection_url", "psql"].includes(cause.phase) ||
      [cause.clientCode, cause.signal, cause.stderr].some(v => v !== null && (typeof v !== "string" || Buffer.byteLength(v, "utf8") > 1024 * 1024)) ||
      !(cause.exitCode === null || Number.isInteger(cause.exitCode) && Number(cause.exitCode) >= 0 && Number(cause.exitCode) <= 999)) return unavailable;
  const prefix = `compute_live_${owner}_database_` as const;
  if (cause.phase === "private_file") return `${prefix}${cause.clientCode === "ENOENT" ? "private_file_missing" : "private_file_unreadable"}`;
  if (cause.phase === "connection_url") {
    if (cause.clientCode === "database_url_invalid") return `${prefix}url_invalid`;
    if (cause.clientCode === "database_url_parameter_invalid") return `${prefix}url_parameter_invalid`;
    return `${prefix}unclassified`;
  }
  if (cause.clientCode === "postgresql_client_required" || cause.clientCode === "ENOENT") return `${prefix}client_required`;
  if (cause.clientCode === "EACCES" || cause.clientCode === "ENOEXEC") return `${prefix}client_spawn_failed`;
  if (cause.signal !== null) return `${prefix}client_interrupted`;
  const stderr = typeof cause.stderr === "string" ? cause.stderr : "";
  // Extract PostgreSQL's verbose protocol field before considering libpq text.
  // Words appearing in a relation name or statement must not become diagnoses.
  const states = [...stderr.matchAll(/^(?:psql:[^\r\n]*?:[0-9]+: )?(?:ERROR|FATAL|PANIC):[ \t]+([A-Z0-9]{5}):[^\r\n]*$/gm)];
  if (states.length === 1) {
    const sqlstate = states[0][1];
    if (sqlstate === "42501") return `${prefix}permission_denied`;
    if (sqlstate === "42P01" || sqlstate === "42703") return `${prefix}schema_unavailable`;
    return `${prefix}sqlstate_${sqlstate}`;
  }
  if (/^psql: error: invalid target_session_attrs value: "[^"\r\n]+"$/m.test(stderr)) return `${prefix}target_session_attrs_invalid`;
  if (/^psql: error: could not translate host name "[^"\r\n]+" to address: [^\r\n]+$/m.test(stderr)) return `${prefix}dns_unavailable`;
  if (/^psql: error: connection to server at [^\r\n]+ failed: Connection refused$/m.test(stderr)) return `${prefix}connection_refused`;
  if (/^psql: error: connection to server at [^\r\n]+ failed: (?:timeout expired|connection timed out)$/m.test(stderr)) return `${prefix}connection_timeout`;
  if (/^psql: error: connection to server at [^\r\n]+ failed: FATAL:  password authentication failed for user "[^"\r\n]+"$/m.test(stderr)) return `${prefix}authentication_failed`;
  if (Number.isInteger(cause.exitCode) && Number(cause.exitCode) > 0) return `${prefix}psql_exit_${cause.exitCode}`;
  return `${prefix}unclassified`;
}
function isErrorCode(value: unknown): value is ErrorCode {
  if (typeof value !== "string") return false;
  if (Object.values(COMPUTE_READBACK_ERROR).includes(value as never)) return true;
  const match = /^compute_live_(?:workspace|fabric)_database_(.+)$/.exec(value);
  return match !== null && (DATABASE_FAILURE_KINDS.includes(match[1] as never) ||
    /^(?:sqlstate_[A-Z0-9]{5}|psql_exit_[1-9][0-9]{0,2})$/.test(match[1]));
}
function hash(v: string): string { return `sha256:${createHash("sha256").update(v).digest("hex")}`; }
function identity(domain: string, v: string) { return hash(`${domain}\0${v}`); }
function digest(v: unknown): v is string { return typeof v === "string" && DIGEST.test(v); }
function instant(v: unknown): v is string {
  if (typeof v !== "string") return false;
  const m = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)$/.exec(v);
  if (!m || !Number.isFinite(Date.parse(v))) return false;
  const local = `${m[1]}.${(m[2] || "").padEnd(3, "0").slice(0, 3)}Z`;
  if (!Number.isFinite(Date.parse(local)) || new Date(local).toISOString() !== local) return false;
  return m[3] === "Z" || (Number(m[3].slice(1, 3)) <= 23 && Number(m[3].slice(4, 6)) <= 59);
}
function provenance(v: unknown): ComputeReadbackProvenance {
  const p = exact(v, ["diagnosticSourceSha", "instanceRepository", "instanceSha", "runId", "runAttempt", "workflowPath"]);
  if (typeof p.diagnosticSourceSha !== "string" || !SHA.test(p.diagnosticSourceSha) ||
      p.instanceRepository !== "gaofeng21cn/opl-instance-medopl" || typeof p.instanceSha !== "string" || !SHA.test(p.instanceSha) ||
      typeof p.runId !== "string" || !/^[1-9][0-9]*$/.test(p.runId) || typeof p.runAttempt !== "string" || !/^[1-9][0-9]*$/.test(p.runAttempt) ||
      p.workflowPath !== WORKFLOW) fail();
  return p as ComputeReadbackProvenance;
}
function cell(raw: unknown, admitted: readonly string[]): Cell {
  if (raw === undefined || raw === "") return { value: null, digest: null };
  if (!text(raw)) fail(COMPUTE_READBACK_ERROR.RESPONSE);
  return { value: admitted.includes(raw) ? raw : null, digest: hash(raw) };
}
function validateCell(v: unknown, admitted: readonly string[]) {
  const c = exact(v, ["value", "digest"]);
  if (!(c.value === null || typeof c.value === "string" && admitted.includes(c.value)) ||
      !(c.digest === null || digest(c.digest)) || c.value !== null && c.digest !== hash(c.value as string)) fail();
  return c as Cell;
}
function observe(body: unknown, workspaceId: string, computeId: string): Observation {
  const r = record(body), data = record(r.providerData);
  if (r.id !== computeId || r.workspaceId !== workspaceId || r.provider !== "tencent-tke" ||
      !text(r.accountId) || !text(r.packageId) || !text(r.status) || typeof r.machinePresent !== "boolean" ||
      !instant(r.observedAt) || !text(r.readbackId) || !text(r.providerRequestId) ||
      !text(data.describeClusterMachinesReq) || !["NativeCVM", "Native", "CXM"].includes(String(data.machineType)) ||
      Object.values(data).some(v => typeof v !== "string" || v.length > 4096)) fail(COMPUTE_READBACK_ERROR.RESPONSE);
  const o: Observation = {
    machineType: data.machineType as Observation["machineType"], machinePresent: r.machinePresent,
    allocationStatus: cell(r.status, ["external_deleted"]), tkeStatus: cell(r.tkeStatus, ["NOT_FOUND"]),
    cvmStatus: cell(r.cvmStatus, ["NOT_FOUND", "SHUTDOWN"]), isolatedSource: cell(data.isolatedSource, ["MANMADE"]),
    observedAt: r.observedAt, readbackIdDigest: identity("compute-readback:readback:v1", r.readbackId),
    providerRequestIdDigest: identity("compute-readback:request:v1", r.providerRequestId),
    machineRequestIdDigest: identity("compute-readback:request:v1", data.describeClusterMachinesReq as string),
    cvmRequestIdDigest: text(data.describeCvmRequestId) ? identity("compute-readback:request:v1", data.describeCvmRequestId) : null,
  };
  validateObservation(o);
  return o;
}
function validateObservation(value: unknown): Observation {
  const o = exact(value, ["machineType", "machinePresent", "allocationStatus", "tkeStatus", "cvmStatus", "isolatedSource", "observedAt", "readbackIdDigest", "providerRequestIdDigest", "machineRequestIdDigest", "cvmRequestIdDigest"]);
  if (typeof o.machineType !== "string" || !["NativeCVM", "Native", "CXM"].includes(o.machineType) || typeof o.machinePresent !== "boolean" || !instant(o.observedAt) ||
      [o.readbackIdDigest, o.providerRequestIdDigest, o.machineRequestIdDigest].some(v => !digest(v)) ||
      !(o.cvmRequestIdDigest === null || digest(o.cvmRequestIdDigest))) fail();
  const allocation = validateCell(o.allocationStatus, ["external_deleted"]), tke = validateCell(o.tkeStatus, ["NOT_FOUND"]);
  const cvm = validateCell(o.cvmStatus, ["NOT_FOUND", "SHUTDOWN"]);
  const isolated = validateCell(o.isolatedSource, ["MANMADE"]);
  if (o.machineType !== "NativeCVM" && (cvm.digest !== null || o.cvmRequestIdDigest !== null || isolated.digest !== null)) fail();
  if (allocation.digest === null || tke.digest === null || o.machineType === "NativeCVM" && o.machinePresent === false && (cvm.digest === null || o.cvmRequestIdDigest === null)) fail();
  if (allocation.value === "external_deleted" && (o.machinePresent !== false || tke.value !== "NOT_FOUND" ||
      o.machineType === "NativeCVM" && cvm.value !== "NOT_FOUND")) fail();
  return o as Observation;
}
function installation(v: ComputeReadbackPin, c: CandidateIdentity): Installation {
  exact(v, ["podUid", "deploymentUid", "replicaSetUid", "imageIndexDigest", "platformDigest", "runtimeDigest"]);
  if (![v.podUid, v.deploymentUid, v.replicaSetUid].every(text) || v.imageIndexDigest !== c.cloudImageDigest ||
      v.platformDigest !== c.cloudRuntimeChildDigest || ![v.imageIndexDigest, v.platformDigest].includes(v.runtimeDigest)) fail(COMPUTE_READBACK_ERROR.IMAGE);
  return { podUidDigest: identity("compute-readback:pod:v1", v.podUid),
    deploymentUidDigest: identity("compute-readback:deployment:v1", v.deploymentUid),
    replicaSetUidDigest: identity("compute-readback:replica-set:v1", v.replicaSetUid),
    imageIndexDigest: v.imageIndexDigest, platformDigest: v.platformDigest, runtimeDigest: v.runtimeDigest };
}
export async function runWorkspaceComputeLiveReadback(input: {
  candidate: unknown; workspaceId: string; workspaceOperationId: string; provenance: ComputeReadbackProvenance;
}, access: ComputeReadbackAccess): Promise<WorkspaceComputeLiveReadbackReceipt> {
  exact(input, ["candidate", "workspaceId", "workspaceOperationId", "provenance"]);
  if (typeof input.workspaceId !== "string" || !/^workspace_[a-f0-9]{8,}$/.test(input.workspaceId) || typeof input.workspaceOperationId !== "string" || !ID.test(input.workspaceOperationId)) fail(COMPUTE_READBACK_ERROR.INPUT);
  const candidate = validateCloudCandidateReceipt(input.candidate), c = candidateInstallationIdentity(candidate);
  const p = provenance(input.provenance);
  const receipt: WorkspaceComputeLiveReadbackReceipt = {
    schemaVersion: 1, kind: "opl_cloud_workspace_compute_live_readback", scope: "compute_destroy_status",
    status: COMPUTE_READBACK_STATUS.BLOCKED, errorCode: COMPUTE_READBACK_ERROR.ACCESS,
    candidate: c, provenance: p, workspaceIdDigest: identity("console-customer-workspace-readback:workspace:v1", input.workspaceId),
    workspaceOperationIdDigest: identity("cloud-launch-readback:operation:v1", input.workspaceOperationId),
    fabricOperationIdDigest: null, computeIdDigest: null, startedAt: new Date().toISOString(), completedAt: new Date().toISOString(),
    installation: null, httpStatus: null, failure: null, observation: null,
  };
  try {
    let binding;
    let bindingError: ErrorCode = COMPUTE_READBACK_ERROR.WORKSPACE_BINDING;
    try {
      const workspaceSql = workspaceComputeBindingSql(input.workspaceId, input.workspaceOperationId);
      bindingError = COMPUTE_READBACK_ERROR.WORKSPACE_ACCESS;
      const workspaceRows = await access.select("workspace", workspaceSql);
      bindingError = COMPUTE_READBACK_ERROR.WORKSPACE_BINDING;
      const workspace = decodeWorkspaceComputeBinding(workspaceRows, input.workspaceId, input.workspaceOperationId);
      bindingError = COMPUTE_READBACK_ERROR.FABRIC_BINDING;
      const fabricSql = fabricComputeBindingSql(workspace);
      bindingError = COMPUTE_READBACK_ERROR.FABRIC_ACCESS;
      let fabricRows: string[];
      try { fabricRows = await access.select("fabric", fabricSql); }
      catch (e) { bindingError = databaseErrorCode("fabric", e); fail(bindingError); }
      bindingError = COMPUTE_READBACK_ERROR.FABRIC_BINDING;
      binding = decodeFabricComputeBinding(fabricRows, workspace);
    } catch (e) {
      if (bindingError === COMPUTE_READBACK_ERROR.WORKSPACE_ACCESS) fail(databaseErrorCode("workspace", e));
      fail(bindingError);
    }
    receipt.fabricOperationIdDigest = identity("cloud-launch-readback:operation:v1", binding.fabricOperationId);
    receipt.computeIdDigest = identity("compute-readback:compute:v1", binding.computeId);
    const image = record(candidate.cloudImage);
    const result = await access.withFabric({ indexRef: image.indexRef as string, indexDigest: image.indexDigest as string,
      platforms: image.platforms as Array<{ platform: string; digest: string }> }, async session => {
      const response = await session.get(`/fabric/compute-allocations/${encodeURIComponent(binding.computeId)}/destroy-status`);
      if (!Number.isInteger(response.status) || response.status < 100 || response.status > 599) fail(COMPUTE_READBACK_ERROR.RESPONSE);
      // Delay publishing any response until the host rechecks Pod/image identity
      // and terminates its transport. A failed cleanup discards the observation.
      if (response.status !== 200) {
        const body = exact(response.body, ["error"]);
        if (!text(body.error)) fail(COMPUTE_READBACK_ERROR.RESPONSE);
        return { httpStatus: response.status, failure: projectComputeReadbackError(body.error), observation: null };
      }
      return { httpStatus: response.status, failure: null, observation: observe(response.body, input.workspaceId, binding.computeId) };
    });
    receipt.installation = installation(result.pin, c);
    receipt.httpStatus = result.value.httpStatus; receipt.failure = result.value.failure; receipt.observation = result.value.observation;
    receipt.errorCode = receipt.observation ? COMPUTE_READBACK_ERROR.NONE : COMPUTE_READBACK_ERROR.HTTP;
    receipt.status = receipt.observation ? COMPUTE_READBACK_STATUS.OBSERVED : COMPUTE_READBACK_STATUS.BLOCKED;
  } catch (e) {
    receipt.errorCode = e instanceof Error && isErrorCode(e.message) ? e.message : COMPUTE_READBACK_ERROR.ACCESS;
    receipt.observation = null; receipt.failure = null; receipt.httpStatus = null; receipt.installation = null;
  }
  receipt.completedAt = new Date().toISOString();
  return validateWorkspaceComputeLiveReadbackReceipt(receipt);
}
export function validateWorkspaceComputeLiveReadbackReceipt(value: unknown): WorkspaceComputeLiveReadbackReceipt {
  const r = exact(value, ["schemaVersion", "kind", "scope", "status", "errorCode", "candidate", "provenance", "workspaceIdDigest", "workspaceOperationIdDigest", "fabricOperationIdDigest", "computeIdDigest", "startedAt", "completedAt", "installation", "httpStatus", "failure", "observation"]);
  if (r.schemaVersion !== 1 || r.kind !== "opl_cloud_workspace_compute_live_readback" || r.scope !== "compute_destroy_status" ||
      !Object.values(COMPUTE_READBACK_STATUS).includes(r.status as never) || !isErrorCode(r.errorCode) ||
      !digest(r.workspaceIdDigest) || !digest(r.workspaceOperationIdDigest) || !(r.fabricOperationIdDigest === null || digest(r.fabricOperationIdDigest)) ||
      !(r.computeIdDigest === null || digest(r.computeIdDigest)) || !instant(r.startedAt) || !instant(r.completedAt) || Date.parse(r.completedAt) < Date.parse(r.startedAt)) fail();
  provenance(r.provenance);
  const c = exact(r.candidate, ["candidateReceiptDigest", "cloudImageDigest", "cloudImageRef", "cloudImageRevision", "cloudRuntimeChildDigest", "productSha", "productTree"]);
  if (![c.candidateReceiptDigest, c.cloudImageDigest, c.cloudRuntimeChildDigest].every(digest) || !SHA.test(String(c.productSha)) || !SHA.test(String(c.productTree)) ||
      c.cloudImageRevision !== c.productSha || c.cloudImageRef !== `ghcr.io/gaofeng21cn/one-person-lab-cloud@${c.cloudImageDigest}`) fail();
  if (r.installation !== null) {
    const i = exact(r.installation, ["podUidDigest", "deploymentUidDigest", "replicaSetUidDigest", "imageIndexDigest", "platformDigest", "runtimeDigest"]);
    if (!Object.values(i).every(digest) || i.imageIndexDigest !== c.cloudImageDigest || i.platformDigest !== c.cloudRuntimeChildDigest ||
        ![i.imageIndexDigest, i.platformDigest].includes(i.runtimeDigest)) fail();
  }
  if (!(r.httpStatus === null || Number.isInteger(r.httpStatus) && Number(r.httpStatus) >= 100 && Number(r.httpStatus) <= 599)) fail();
  if (r.failure !== null) {
    const f = exact(r.failure, ["errorToken", "errorDigest"]);
    if (!digest(f.errorDigest) || !(f.errorToken === null || typeof f.errorToken === "string" && projectComputeReadbackError(f.errorToken).errorToken === f.errorToken)) fail();
  }
  if (r.observation !== null) validateObservation(r.observation);
  const observed = r.status === COMPUTE_READBACK_STATUS.OBSERVED;
  if (observed !== (r.observation !== null) || observed !== (r.errorCode === COMPUTE_READBACK_ERROR.NONE) ||
      observed && (r.httpStatus !== 200 || r.failure !== null) ||
      r.observation !== null && (r.installation === null || r.fabricOperationIdDigest === null || r.computeIdDigest === null) ||
      r.failure !== null && (r.errorCode !== COMPUTE_READBACK_ERROR.HTTP || r.httpStatus === null || r.httpStatus === 200 || r.installation === null) ||
      r.errorCode === COMPUTE_READBACK_ERROR.HTTP && r.failure === null ||
      r.installation === null && r.httpStatus !== null ||
      r.installation !== null && (r.fabricOperationIdDigest === null || r.computeIdDigest === null || r.httpStatus === null)) fail();
  if ((r.fabricOperationIdDigest === null) !== (r.computeIdDigest === null)) fail();
  if ([COMPUTE_READBACK_ERROR.BINDING, COMPUTE_READBACK_ERROR.WORKSPACE_ACCESS,
    COMPUTE_READBACK_ERROR.WORKSPACE_BINDING, COMPUTE_READBACK_ERROR.FABRIC_ACCESS,
    COMPUTE_READBACK_ERROR.FABRIC_BINDING].includes(r.errorCode as never) && r.computeIdDigest !== null ||
      /^compute_live_(?:workspace|fabric)_database_/.test(String(r.errorCode)) && r.computeIdDigest !== null) fail();
  if (!observed && r.errorCode !== COMPUTE_READBACK_ERROR.HTTP &&
      (r.installation !== null || r.httpStatus !== null || r.failure !== null)) fail();
  return r as WorkspaceComputeLiveReadbackReceipt;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv.length !== 5 || process.argv[2] !== "validate" || process.argv[3] !== "--receipt") fail();
    const handle = await open(process.argv[4], constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const info = await handle.stat();
      if (!info.isFile() || info.size > 1024 * 1024) fail();
      validateWorkspaceComputeLiveReadbackReceipt(JSON.parse(await handle.readFile("utf8")));
    } finally { await handle.close(); }
  } catch { console.error(COMPUTE_READBACK_ERROR.INVALID); process.exitCode = 1; }
}