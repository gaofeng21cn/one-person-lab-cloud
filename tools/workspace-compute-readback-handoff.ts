import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { lstat, open } from "node:fs/promises";
import { dirname, join, parse, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { candidateInstallationIdentity, canonicalJson } from "./cloud-candidate-receipt.ts";
import { validateWorkspaceComputeLiveReadbackReceipt } from "./workspace-compute-live-readback.ts";
import { validateComputeProviderRequestAuditReceipt } from "./workspace-compute-provider-audit.ts";

const INVALID = "workspace_compute_readback_handoff_invalid";
const MAX_BYTES = 1024 * 1024;
const INSTANCE_REPOSITORY = "gaofeng21cn/opl-instance-medopl";
const CLOUD_REPOSITORY = "gaofeng21cn/one-person-lab-cloud";
const WORKFLOW = ".github/workflows/cloud-workspace-compute-readback.yml";
const INPUT_KEYS = ["candidate", "run", "expectedWorkspaceId", "expectedWorkspaceOperationId", "expectedDiagnosticSourceSha"];

type HandoffInput = {
  candidate: unknown; receiptText: string; run: unknown;
  expectedWorkspaceId: string; expectedWorkspaceOperationId: string; expectedDiagnosticSourceSha: string;
};

function fail(): never { throw new Error(INVALID); }
function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) fail();
  return value as Record<string, unknown>;
}
function exact(value: unknown, keys: string[]) {
  const result = record(value);
  if (Object.keys(result).length !== keys.length || keys.some(key => !Object.hasOwn(result, key))) fail();
  return result;
}
function hash(text: string) { return `sha256:${createHash("sha256").update(text).digest("hex")}`; }
function positiveApiInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

// run must be the independently fetched GitHub API response. Receipt provenance
// alone authenticates nothing; the workflow is responsible for that GET boundary.
export function validateWorkspaceComputeReadbackHandoff(input: HandoffInput) {
  try {
    exact(input, [...INPUT_KEYS, "receiptText"]);
    if (typeof input.receiptText !== "string" || Buffer.byteLength(input.receiptText) > MAX_BYTES ||
        Buffer.from(input.receiptText).toString("utf8") !== input.receiptText ||
        typeof input.expectedWorkspaceId !== "string" || !/^workspace_[a-f0-9]{8,}$/.test(input.expectedWorkspaceId) ||
        typeof input.expectedWorkspaceOperationId !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$/.test(input.expectedWorkspaceOperationId) ||
        typeof input.expectedDiagnosticSourceSha !== "string" || !/^[a-f0-9]{40}$/.test(input.expectedDiagnosticSourceSha)) fail();
    const identity = candidateInstallationIdentity(input.candidate);
    // Decode precisely the text whose digest is retained, never a second object.
    const receipt = validateWorkspaceComputeLiveReadbackReceipt(JSON.parse(input.receiptText));
    if (Object.entries(identity).some(([key, value]) => receipt.candidate[key as keyof typeof identity] !== value) ||
        receipt.workspaceIdDigest !== hash(`console-customer-workspace-readback:workspace:v1\0${input.expectedWorkspaceId}`) ||
        receipt.workspaceOperationIdDigest !== hash(`cloud-launch-readback:operation:v1\0${input.expectedWorkspaceOperationId}`) ||
        receipt.provenance.diagnosticSourceSha !== input.expectedDiagnosticSourceSha) fail();
    const run = record(input.run);
    const provenance = receipt.provenance;
    if (record(run.repository).full_name !== INSTANCE_REPOSITORY || run.path !== WORKFLOW ||
        run.event !== "workflow_dispatch" || run.head_branch !== "main" || run.status !== "completed" ||
        run.conclusion !== "success" || run.head_sha !== provenance.instanceSha ||
        !positiveApiInteger(run.id) || String(run.id) !== provenance.runId ||
        !positiveApiInteger(run.run_attempt) || String(run.run_attempt) !== provenance.runAttempt) fail();
    const actors: string[] = [];
    for (const key of ["actor", "triggering_actor"]) {
      if (!Object.hasOwn(run, key)) continue;
      const login = record(run[key]).login;
      if (login !== "gaofeng21cn" && login !== "RenDeHuang") fail();
      actors.push(login);
    }
    if (actors.length === 2 && actors[0] !== actors[1]) fail();
    // Only allowlisted fields survive. blocked/observed describe diagnosis,
    // and neither is a deletion or refund completion assertion.
    return {
      schemaVersion: 1, kind: "opl_cloud_workspace_compute_readback_handoff",
      evidenceRepository: CLOUD_REPOSITORY, evidenceClass: "workspace_compute_readback",
      scope: receipt.scope, receiptDigest: hash(input.receiptText),
      receiptStatus: receipt.status, errorCode: receipt.errorCode,
      identity, workspaceIdDigest: receipt.workspaceIdDigest,
      workspaceOperationIdDigest: receipt.workspaceOperationIdDigest,
      provenance: {
        diagnosticSourceSha: provenance.diagnosticSourceSha, instanceRepository: INSTANCE_REPOSITORY,
        instanceSha: provenance.instanceSha, runId: String(run.id), runAttempt: String(run.run_attempt), workflowPath: WORKFLOW,
      },
    };
  } catch { fail(); }
}

// The independently fetched run authenticates both original byte streams. The
// auxiliary audit stays a diagnosis; it never grants another provider send.
export function validateWorkspaceComputeProviderAuditHandoff(input: HandoffInput & { auditReceiptText: string }) {
  try {
    const { auditReceiptText, ...parentInput } = exact(input, [...INPUT_KEYS, "receiptText", "auditReceiptText"]);
    const parentBinding = validateWorkspaceComputeReadbackHandoff(parentInput as HandoffInput);
    if (typeof auditReceiptText !== "string" || Buffer.byteLength(auditReceiptText) > MAX_BYTES) fail();
    const parent = validateWorkspaceComputeLiveReadbackReceipt(JSON.parse(parentInput.receiptText as string));
    const audit = validateComputeProviderRequestAuditReceipt(JSON.parse(auditReceiptText));
    if (parent.status !== "observed" || parent.schemaVersion !== 2 || !parent.journal ||
        canonicalJson(audit.candidate) !== canonicalJson(parent.candidate) ||
        canonicalJson(audit.provenance) !== canonicalJson(parent.provenance) ||
        audit.owner.workspaceIdDigest !== parent.workspaceIdDigest || audit.owner.workspaceOperationIdDigest !== parent.workspaceOperationIdDigest ||
        audit.owner.fabricOperationIdDigest !== parent.fabricOperationIdDigest || audit.owner.computeIdDigest !== parent.computeIdDigest ||
        audit.owner.accountIdDigest !== parent.journal.accountIdDigest || audit.owner.destroyOperationIdDigest !== parent.journal.destroyOperationIdDigest) fail();
    if (audit.stage) {
      const starts = parent.journal.records.filter(r => r.recordIdDigest === audit.stage!.startedRecordIdDigest && r.status === "started");
      const failures = parent.journal.records.filter(r => r.recordIdDigest === audit.stage!.failedRecordIdDigest && r.status === "failed");
      if (starts.length !== 1 || failures.length !== 1 || [...starts, ...failures].some(r =>
          r.action !== audit.scope || r.operationIdDigest !== audit.stage!.operationIdDigest ||
          r.requestHashDigest !== audit.stage!.requestHashDigest || r.originalDestroyOperationIdDigest !== audit.owner.destroyOperationIdDigest) ||
          Date.parse(starts[0].startedAt) !== Date.parse(audit.stage.startedAt) || Date.parse(failures[0].finishedAt!) !== Date.parse(audit.stage.failedAt)) fail();
    }
    return { schemaVersion: 1, kind: "opl_cloud_compute_provider_request_audit_handoff", evidenceRepository: CLOUD_REPOSITORY,
      evidenceClass: "workspace_compute_provider_request_audit", scope: audit.scope, receiptDigest: hash(auditReceiptText),
      parentReceiptDigest: parentBinding.receiptDigest, receiptStatus: audit.status, errorCode: audit.errorCode,
      identity: parentBinding.identity, owner: audit.owner, stage: audit.stage, provenance: parentBinding.provenance };
  } catch { fail(); }
}

async function noSymlinks(path: string) {
  const absolute = resolve(path);
  let cursor = parse(absolute).root;
  for (const component of absolute.slice(cursor.length).split("/").filter(Boolean)) {
    cursor = join(cursor, component);
    if ((await lstat(cursor)).isSymbolicLink()) fail();
  }
}
async function readText(path: string) {
  await noSymlinks(path);
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  try {
    const info = await handle.stat();
    if (!info.isFile() || info.size > MAX_BYTES) fail();
    const buffer = Buffer.alloc(MAX_BYTES + 1);
    let size = 0;
    while (size < buffer.length) {
      const { bytesRead } = await handle.read(buffer, size, buffer.length - size, null);
      if (bytesRead === 0) break;
      size += bytesRead;
    }
    if (size > MAX_BYTES) fail();
    const bytes = buffer.subarray(0, size), text = bytes.toString("utf8");
    if (!Buffer.from(text).equals(bytes)) fail();
    return text;
  } finally { await handle.close(); }
}

export async function main(args = process.argv.slice(2)) {
  try {
    const audit = args[0] === "import-provider-audit";
    if ((!audit && (args.length !== 7 || args[0] !== "import")) || (audit && args.length !== 9)) fail();
    const options = new Map<string, string>();
    for (let i = 1; i < args.length; i += 2) {
      if (!["--input", "--receipt", "--output", ...(audit ? ["--parent-receipt"] : [])].includes(args[i]) || options.has(args[i]) ||
          !args[i + 1] || args[i + 1].startsWith("--")) fail();
      options.set(args[i], resolve(args[i + 1]));
    }
    if (options.size !== (audit ? 4 : 3) || new Set(options.values()).size !== options.size) fail();
    const input = exact(JSON.parse(await readText(options.get("--input")!)), INPUT_KEYS);
    const binding = audit ? validateWorkspaceComputeProviderAuditHandoff({
      ...input, receiptText: await readText(options.get("--parent-receipt")!), auditReceiptText: await readText(options.get("--receipt")!),
    } as HandoffInput & { auditReceiptText: string }) : validateWorkspaceComputeReadbackHandoff({
      ...input, receiptText: await readText(options.get("--receipt")!),
    } as HandoffInput);
    const output = options.get("--output")!;
    await noSymlinks(dirname(output));
    const handle = await open(output, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    try {
      await handle.chmod(0o600);
      await handle.writeFile(`${JSON.stringify(binding, null, 2)}\n`);
    } finally { await handle.close(); }
    return binding;
  } catch { fail(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(() => { console.error(INVALID); process.exitCode = 1; });
}
