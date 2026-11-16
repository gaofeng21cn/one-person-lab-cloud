import { createHash, randomBytes } from "node:crypto";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { workspaceDeclaration } from "./workspace-purchase-core.ts";

const shaPattern = /^[0-9a-f]{40}$/;
const recoveryRefPattern = /^[A-Za-z0-9][A-Za-z0-9._/:-]{0,511}$/;

function requireExactKeys(value, keys, label) {
  if (!value || typeof value !== "object" || Array.isArray(value) ||
    Object.keys(value).sort().join("\0") !== [...keys].sort().join("\0")) {
    throw new Error(`${label} shape is invalid`);
  }
}

function requireKeys(value, keys, label) {
  if (!value || typeof value !== "object" || Array.isArray(value) || keys.some((key) => !Object.prototype.hasOwnProperty.call(value, key))) {
    throw new Error(`${label} shape is invalid`);
  }
}

function requireCount(value, label) {
  if (!Number.isSafeInteger(value) || value < 0) throw new Error(`${label} is invalid`);
  return value;
}

function requireRecoveryRef(value, label) {
  const normalized = String(value || "");
  const candidate = normalized.startsWith("/") ? normalized.slice(1) : normalized;
  if (!recoveryRefPattern.test(candidate) || candidate.split("/").includes("..") || candidate.includes("//")) {
    throw new Error(`${label} is invalid`);
  }
  return normalized;
}

export function recoveryIdentityDigest(value) {
  const normalized = String(value || "");
  if (!normalized) throw new Error("recovery identity is required");
  return `sha256:${createHash("sha256").update(normalized).digest("hex")}`;
}

export function noLocalJ1ExternalWrites() {
  const none = () => ({ attempted: 0, confirmed: 0, unknown: 0 });
  return {
    workspaceCreates: none(),
    keyCreates: none(),
    debits: none(),
    providerMutations: none(),
    purchaseReceipts: none()
  };
}

export function localJ1FailureDisposition({ ready = false, launchSubmitted = false, externalWrites }) {
  requireExactKeys(externalWrites, ["workspaceCreates", "keyCreates", "debits", "providerMutations", "purchaseReceipts"], "external write counts");
  const total = Object.entries(externalWrites).reduce((sum, [name, value]) => {
    requireExactKeys(value, ["attempted", "confirmed", "unknown"], `${name} write count`);
    const attempted = requireCount(value.attempted, `${name} attempted`);
    const confirmed = requireCount(value.confirmed, `${name} confirmed`);
    const unknown = requireCount(value.unknown, `${name} unknown`);
    if (confirmed + unknown > attempted) throw new Error(`${name} write count is invalid`);
    return sum + attempted;
  }, 0);
  if (ready) return Object.freeze({ cleanup: true, preserveRecoveryAuthority: false, reason: "ready" });
  if (!launchSubmitted && total === 0) {
    return Object.freeze({ cleanup: true, preserveRecoveryAuthority: false, reason: "pre_create_failure" });
  }
  return Object.freeze({ cleanup: false, preserveRecoveryAuthority: true, reason: "durable_launch_or_external_write" });
}




export function localJ1CleanupPlan({ ready = false, launchSubmitted = false, authority }) {
  const externalWrites = authority?.externalWrites || noLocalJ1ExternalWrites();
  const disposition = localJ1FailureDisposition({ ready, launchSubmitted, externalWrites });
  return {
    ...disposition,
    stopServices: disposition.preserveRecoveryAuthority ? [] : ["control-plane", "fabric", "ledger", "postgres"],
    removeComposeVolumes: disposition.cleanup,
    removeRecoveryRoot: disposition.cleanup
  };
}

export function createLocalJ1RecoveryReadbackPendingArtifact({ source, failure, operationId, workspaceId, idempotencyKey, quoteId, workspaceName, declaration, compose }) {
  requireExactKeys(source, ["sha", "tree"], "recovery source");
  if (!shaPattern.test(source.sha) || !shaPattern.test(source.tree)) throw new Error("recovery source identity is invalid");
  requireExactKeys(failure, ["stage", "errorCode"], "recovery failure");
  if (!String(failure.stage || "") || !/^[a-z0-9_]+$/.test(String(failure.errorCode || ""))) {
    throw new Error("recovery failure classification is invalid");
  }
  requireExactKeys(compose, ["project", "recoveryRoot", "fabricSecretRoot", "postgresVolumeRefs", "providerVolumeRefs"], "recovery compose refs");
  const postgresVolumeRefs = compose.postgresVolumeRefs.map((value) => requireRecoveryRef(value, "PostgreSQL volume ref"));
  const providerVolumeRefs = compose.providerVolumeRefs.map((value) => requireRecoveryRef(value, "provider volume ref"));
  if (postgresVolumeRefs.length === 0) throw new Error("PostgreSQL recovery volume refs are required");
  const unknownWriteCount = () => ({ attempted: null, confirmed: null, unknown: null });
  // A command whose answer was lost may have been admitted even though the
  // buyer never saw the owner-minted operation or Workspace identity. The
  // recovery record then binds the original idempotency identity and says the
  // owner readback is unknown; it never invents an identity or buys again.
  const operationIdDigest = String(operationId || "") ? recoveryIdentityDigest(operationId) : null;
  const workspaceIdDigest = String(workspaceId || "") ? recoveryIdentityDigest(workspaceId) : null;
  const idempotencyKeyDigest = String(idempotencyKey || "") ? recoveryIdentityDigest(idempotencyKey) : null;
  if (!operationIdDigest && !idempotencyKeyDigest) throw new Error("recovery identity is required");
  // This private 0600 recovery artifact must retain replayable selectors when
  // the owner response was lost. Digests in the public failure summary alone
  // cannot address the accepted order. These fields contain no credential.
  let replay;
  if (idempotencyKeyDigest) {
    if (!/^[A-Za-z0-9._:-]{1,512}$/.test(String(idempotencyKey)) || typeof workspaceName !== "string" ||
      !workspaceName.trim() || !declaration || typeof declaration !== "object") {
      throw new Error("recovery replay inputs are required");
    }
    replay = { owner: "workspace", idempotencyKey, quoteId: String(quoteId || "") || null,
      operationId: String(operationId || "") || null, workspaceId: String(workspaceId || "") || null,
      name: workspaceName, declaration: workspaceDeclaration(declaration) };
  }
  return {
    schemaVersion: 1,
    kind: "opl.local-workspace.j1-recovery.v1",
    status: "RECOVERY_READBACK_REQUIRED",
    source: { sha: source.sha, tree: source.tree },
    failure: { stage: failure.stage, errorCode: failure.errorCode },
    ...(replay ? { replay } : {}),
    authority: {
      operationIdDigest,
      workspaceIdDigest,
      idempotencyKeyDigest,
      readbackStatus: "unavailable",
      externalWrites: {
        workspaceCreates: unknownWriteCount(),
        keyCreates: unknownWriteCount(),
        debits: unknownWriteCount(),
        providerMutations: unknownWriteCount(),
        purchaseReceipts: unknownWriteCount()
      }
    },
    compose: {
      project: requireRecoveryRef(compose.project, "Compose project"),
      recoveryRoot: requireRecoveryRef(compose.recoveryRoot, "recovery root"),
      fabricSecretRoot: requireRecoveryRef(compose.fabricSecretRoot, "Fabric Secret root"),
      postgresVolumeRefs,
      providerVolumeRefs
    },
    cleanup: { performed: false, reason: "durable_launch_or_external_write" }
  };
}

export function localJ1RecoveryArtifactPath(receiptPath) {
  const normalized = String(receiptPath || "");
  if (!normalized.startsWith("/") || !normalized.endsWith(".json")) {
    throw new Error("qualification receipt path must be an absolute JSON path");
  }
  return normalized.replace(/\.json$/, ".recovery.json");
}

export async function writeLocalJ1RecoveryArtifact(path, artifact) {
  const target = String(path || "");
  if (!target.startsWith("/") || !target.endsWith(".json")) throw new Error("recovery artifact path must be an absolute JSON path");
  await mkdir(dirname(target), { recursive: true });
  const temporary = `${target}.${process.pid}.${randomBytes(4).toString("hex")}.tmp`;
  await writeFile(temporary, `${JSON.stringify(artifact, null, 2)}\n`, { mode: 0o600 });
  await rename(temporary, target);
}
