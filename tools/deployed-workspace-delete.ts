/*
 * The deployed Workspace deletion: one delete, for one declared Workspace,
 * against a running product.
 *
 * The local qualification harness is not this entry and must not become it. That
 * harness builds a stack, brings it up, binds it to loopback and tears it down;
 * it is evidence about the platform. This entry does none of that. It assumes a
 * product is already deployed, refuses to run against anything that is not, and
 * deletes exactly one Workspace the caller has already named and acknowledged.
 *
 * There is no credential here. The protected flow that invokes this entry owns
 * the login and hands over an already-authenticated transport; this entry reads
 * that session's Cloud actor and Tenant back through the session owner and refuses
 * destruction until they match the declared Cloud identity digest. The
 * caller also declares the exact Candidate the deletion belongs to, and the
 * receipt this entry returns binds that Candidate, that identity and the
 * mutation it caused.
 *
 * It deletes and it does nothing else. It does not launch, does not renew, does
 * not price, does not refund by hand and does not touch a database. The owner
 * owns the deletion, the residual readback and the refund; this entry forwards
 * the caller's exact Workspace name and explicit destruction acknowledgement,
 * follows the owner's own operation, and reports what the owner published.
 */

import { createHash } from "node:crypto";
import type { WorkspaceOwnerDeletionDTO, WorkspaceOwnerOperationDTO } from "../apps/console-ui/src/api/dtos.ts";

export const DEPLOYED_DELETE_RECEIPT_KIND = "opl.deployed-workspace-delete.v1";

const DIGEST = /^sha256:[a-f0-9]{64}$/;
const IDEMPOTENCY_IDENTITY = /^[A-Za-z0-9._:-]{1,128}$/;
const IDEMPOTENCY_KEY = /^deployed-workspace-delete:[0-9a-f]{64}:[A-Za-z0-9._:-]{1,128}$/;
const TOKEN = /^[a-z0-9_]{1,64}$/;

const TARGET_ERROR =
  "deployed delete target must be one https production origin, declared as authorityClass production";

const OPERATION_STATES = ["accepted", "running", "awaiting_confirmation", "succeeded", "failed", "needs_attention", "cancelled"];
const TERMINAL_STATES = ["succeeded", "failed", "cancelled"];
const STOPPED_STATES = [...TERMINAL_STATES, "needs_attention"];
const DELETION_FACTS = ["pending", "confirmed", "rejected", "unknown"];
const REFUND_FACTS = ["not_applicable", "requested", "confirmed", "rejected", "unknown"];

const REQUEST_KEYS = [
  "baseURL",
  "authorityClass",
  "candidateReceiptDigest",
  "cloudIdentityDigest",
  "workspaceId",
  "workspaceName",
  "idempotencyIdentity",
];
const RECEIPT_KEYS = [
  "schemaVersion",
  "kind",
  "candidate",
  "account",
  "operation",
  "deletion",
  "mutations",
  "startedAt",
  "completedAt",
];
const CANDIDATE_KEYS = ["receiptDigest"];
const ACCOUNT_KEYS = ["identityDigest"];
const OPERATION_KEYS = ["idempotencyKey", "operationId", "status", "stage", "resumed"];
const DELETION_KEYS = ["workspaceAbsent", "refundStatus", "refundOperationId"];
const MUTATION_KEYS = ["declared", "observed"];
const DELETION_MUTATION_KEYS = ["deletions"];

const DELETE_POLL_ATTEMPTS = 120;

function hasExactKeys(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

function deleteError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

const targetError = () => deleteError("deployed_delete_target_invalid", TARGET_ERROR);
const accountUnavailable = () => deleteError(
  "deployed_delete_account_unavailable",
  "deployed delete could not read the declared customer account",
);
const workspaceUnavailable = () => deleteError(
  "deployed_delete_workspace_unavailable",
  "deployed delete could not read the declared Workspace",
);

/*
 * The mirror of the local harness's guard.
 *
 * The harness refuses production authority, and this refuses anything that is
 * not production authority, so neither can be run in place of the other. It also
 * refuses loopback and private-range origins: a deletion that answered from the
 * local stack would destroy nothing while reporting that it had, which is worse
 * than no deletion at all. The declared target must be an origin, because every
 * route below is appended to it.
 */
export function assertDeployedDeleteTarget(options) {
  let target;
  try {
    target = new URL(String(options?.baseURL || ""));
  } catch {
    throw targetError();
  }
  if (target.protocol !== "https:" || target.username || target.password ||
      target.pathname !== "/" || target.search || target.hash ||
      options?.authorityClass !== "production") {
    throw targetError();
  }
  const hostname = target.hostname.toLowerCase();
  const bare = hostname.startsWith("[") && hostname.endsWith("]") ? hostname.slice(1, -1) : hostname;
  if (bare === "localhost" || bare.endsWith(".localhost") || bare.endsWith(".local") ||
      bare === "0.0.0.0" || bare === "::" || bare === "::1" ||
      /^127\./.test(bare) || /^10\./.test(bare) || /^192\.168\./.test(bare) ||
      /^169\.254\./.test(bare) || /^172\.(1[6-9]|2\d|3[01])\./.test(bare) ||
      /^f[cd][0-9a-f]{2}:/i.test(bare) || /^fe[89ab][0-9a-f]:/i.test(bare)) {
    throw targetError();
  }
  return target.origin;
}

/*
 * Every declared input is read by name and none of them has a default. The
 * Workspace name is required because the owner demands it as the destruction
 * confirmation; this entry never derives it, so a caller that cannot state the
 * name cannot delete anything.
 */
export function assertDeployedDeleteRequest(request) {
  assertDeployedDeleteTarget(request);
  if (!hasExactKeys(request, REQUEST_KEYS) ||
      !DIGEST.test(String(request?.candidateReceiptDigest || "")) ||
      !DIGEST.test(String(request?.cloudIdentityDigest || "")) ||
      typeof request?.workspaceId !== "string" || [...request.workspaceId].length < 1 || [...request.workspaceId].length > 256 ||
      typeof request?.workspaceName !== "string" || [...request.workspaceName].length < 1 || [...request.workspaceName].length > 256 ||
      !IDEMPOTENCY_IDENTITY.test(String(request?.idempotencyIdentity || "")) ||
      // The owner trims the name before it compares the confirmation, so a
      // declared name with padding could never match and would fail after the
      // destruction was already acknowledged.
      String(request?.workspaceName || "") !== String(request?.workspaceName || "").trim()) {
    throw targetError();
  }
  return request;
}

/*
 * The platform idempotency identity this entry declares.
 *
 * The Candidate is part of the key so a replay for another Candidate addresses a
 * different command, and a receipt cannot bind one Candidate to a deletion
 * admitted for another.
 */
export function deployedDeleteIdempotencyKey(candidateReceiptDigest, idempotencyIdentity) {
  const digest = String(candidateReceiptDigest || "");
  if (!DIGEST.test(digest) || !IDEMPOTENCY_IDENTITY.test(String(idempotencyIdentity || ""))) {
    throw targetError();
  }
  return `deployed-workspace-delete:${digest.slice("sha256:".length)}:${idempotencyIdentity}`;
}

export function assertDeployedDeleteCaller(caller, request) {
  if (!caller || typeof caller !== "object" ||
      typeof caller.http?.request !== "function" || typeof caller.http?.json !== "function" ||
      !caller.auth || typeof caller.auth !== "object" ||
      (caller.wait !== undefined && typeof caller.wait !== "function") ||
      (caller.now !== undefined && typeof caller.now !== "function")) {
    throw deleteError("deployed_delete_caller_invalid", "deployed delete caller is not provided");
  }
  let origin;
  try {
    origin = new URL(String(caller.origin || "")).origin;
  } catch {
    throw deleteError("deployed_delete_caller_invalid", "deployed delete caller is not the declared target");
  }
  if (origin !== assertDeployedDeleteTarget(request)) {
    throw deleteError("deployed_delete_caller_invalid", "deployed delete caller is not the declared target");
  }
  return { origin, http: caller.http, auth: caller.auth, wait: caller.wait, now: caller.now };
}

// Bind a protected caller's declared Cloud actor and Tenant without emitting
// either raw identity, display name, session or credential in the receipt.
export function deployedCloudIdentityDigest(actorId, tenantId) {
  if (typeof actorId !== "string" || !actorId.trim() || typeof tenantId !== "string" || !tenantId.trim()) throw accountUnavailable();
  return `sha256:${createHash("sha256").update(JSON.stringify([actorId, tenantId])).digest("hex")}`;
}

async function readCloudSession(http, auth) {
  const result = await http.request("/api/v2/auth/session", {}, auth);
  const session = result?.payload;
  if (result?.response?.status !== 200 || !session?.actorId || !session.tenantId || !session.csrfToken
      || !["owner", "admin"].includes(session.role)) throw accountUnavailable();
  return session;
}

async function readDeletion(http, auth, workspaceId): Promise<WorkspaceOwnerDeletionDTO | null> {
  const result = await http.request(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deletion`, {}, auth);
  if (result?.response?.status !== 200) throw workspaceUnavailable();
  const deletion = result.payload as WorkspaceOwnerDeletionDTO | null;
  if (deletion === null) return null;
  if (!deletion || deletion.workspaceId !== workspaceId || !deletion.operationId
      || !DELETION_FACTS.includes(deletion.resourceDeletionStatus) || !DELETION_FACTS.includes(deletion.dataDeletionStatus)
      || !REFUND_FACTS.includes(deletion.refundStatus) || !Number.isFinite(Date.parse(deletion.updatedAt))) {
    throw deleteError("deployed_delete_readback_invalid", "deployed delete read an invalid owner deletion");
  }
  return deletion;
}

async function readOperation(http, auth, workspaceId, operationId): Promise<WorkspaceOwnerOperationDTO> {
  const result = await http.request(`/api/v2/operations/workspace/${encodeURIComponent(operationId)}`, {}, auth);
  const operation = result?.payload as WorkspaceOwnerOperationDTO;
  if (result?.response?.status !== 200 || !operation || operation.operationId !== operationId
      || operation.resourceId !== workspaceId || operation.owner !== "workspace" || operation.kind !== "delete_workspace"
      || !OPERATION_STATES.includes(operation.status) || !TOKEN.test(operation.stage || "")
      || operation.status === "succeeded" && (operation.stage !== "succeeded" || operation.observationResult !== "confirmed")
      || ["failed", "needs_attention"].includes(operation.status) && !operation.errorCode
      || !TERMINAL_STATES.includes(operation.status) && (!Number.isInteger(operation.pollAfterSeconds)
        || operation.pollAfterSeconds! < 1 || operation.pollAfterSeconds! > 300)) {
    throw deleteError("deployed_delete_operation_mismatch", "deployed delete read an invalid or unrelated owner operation");
  }
  return operation;
}

async function readWorkspace(http, auth, workspaceId) {
  const result = await http.request(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth);
  if (result?.response?.status !== 200 || result?.payload?.id !== workspaceId) throw workspaceUnavailable();
  return result.payload;
}

async function waitForDeletion(http, auth, workspaceId, operationId, wait) {
  for (let attempt = 0; attempt < DELETE_POLL_ATTEMPTS; attempt += 1) {
    const deletion = await readDeletion(http, auth, workspaceId);
    if (!deletion || operationId && deletion.operationId !== operationId) {
      throw deleteError("deployed_delete_operation_mismatch", "deployed delete could not confirm its accepted operation");
    }
    operationId = deletion.operationId;
    const operation = await readOperation(http, auth, workspaceId, operationId);
    if (STOPPED_STATES.includes(operation.status)) return { deletion, operation };
    await wait(operation.pollAfterSeconds! * 1000);
  }
  throw deleteError("deployed_delete_timeout", "deployed delete did not reach an owner stopping state in the bounded observation window");
}

/*
 * Delete one Workspace for the declared customer and return the receipt the
 * caller may bind its own evidence to.
 *
 * The deletion is read back before anything is issued, so a replay of the same
 * declared identity follows the command it already caused and destroys nothing
 * again. A first delivery issues exactly one command. A command whose answer was
 * lost is looked up under its owner readback rather than reissued; that delivery
 * still caused the deletion, so its receipt says so.
 *
 * The returned receipt carries no credential, session, balance, raw account
 * identifier or Workspace name. It carries the Candidate digest, the account
 * digest, the platform-minted operation identity, the owner's published deletion
 * state and refund state, and the bounded mutation count.
 */
export async function runDeployedWorkspaceDelete(request, caller) {
  assertDeployedDeleteRequest(request);
  const { http, auth, wait, now } = assertDeployedDeleteCaller(caller, request);
  const clock = now || (() => new Date());
  const started = new Date(clock());
  const startedAt = started.toISOString();
  const workspaceId = request.workspaceId;
  const idempotencyKey = deployedDeleteIdempotencyKey(
    request.candidateReceiptDigest,
    request.idempotencyIdentity,
  );

  const session = await readCloudSession(http, auth);
  const identityDigest = deployedCloudIdentityDigest(session.actorId, session.tenantId);
  if (identityDigest !== request.cloudIdentityDigest) {
    throw deleteError("deployed_delete_account_mismatch", "deployed delete Cloud identity does not match the declared customer");
  }
  const workspace = await readWorkspace(http, auth, workspaceId);
  if (workspace.name !== request.workspaceName) {
    throw deleteError("deployed_delete_workspace_mismatch", "deployed delete Workspace name does not match the declared confirmation");
  }
  let deletion = await readDeletion(http, auth, workspaceId);
  const resumed = deletion !== null;
  let acceptedId = deletion?.operationId;
  if (!deletion) {
    let issued;
    try {
      issued = await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {
        method: "DELETE",
        headers: { "idempotency-key": idempotencyKey, "x-csrf-token": session.csrfToken },
        body: { confirmationName: request.workspaceName, acknowledgeDataDestruction: true }
      }, auth, [202]);
    } catch (error) {
      // A lost answer can only continue an owner-recorded intent; never issue
      // another mutation or substitute another operation identity.
      deletion = await readDeletion(http, auth, workspaceId);
      if (!deletion) throw error;
      acceptedId = deletion.operationId;
    }
    if (issued) {
      const accepted = issued.payload;
      if (accepted?.owner !== "workspace" || accepted.kind !== "delete_workspace" || accepted.resourceId !== workspaceId || !accepted.operationId) {
        throw deleteError("deployed_delete_operation_mismatch", "deployed delete received an unrelated command response");
      }
      acceptedId = accepted.operationId;
    }
  }
  const observed = await waitForDeletion(http, auth, workspaceId, acceptedId, wait || (ms => new Promise(resolve => setTimeout(resolve, ms))));
  deletion = observed.deletion;
  const operation = observed.operation;
  const after = await readWorkspace(http, auth, workspaceId);
  // This is the owner's confirmed deletion fact, not physical row absence or an
  // independent provider/route probe. The retained deleted row is expected.
  const workspaceAbsent = after.status === "deleted" && deletion.resourceDeletionStatus === "confirmed" && deletion.dataDeletionStatus === "confirmed";
  if (operation.status === "succeeded" && !workspaceAbsent) {
    throw deleteError("deployed_delete_residual_workspace", "deployed delete succeeded without owner-confirmed resource and Workspace deletion");
  }

  const completed = new Date(clock());
  const completedAt = completed.getTime() < started.getTime() ? startedAt : completed.toISOString();

  return validateDeployedDeleteReceipt({
    schemaVersion: 1,
    kind: DEPLOYED_DELETE_RECEIPT_KIND,
    candidate: { receiptDigest: request.candidateReceiptDigest },
    account: { identityDigest },
    operation: {
      idempotencyKey,
      operationId: String(deletion.operationId || ""),
      status: operation.status,
      stage: operation.stage,
      resumed,
    },
    deletion: {
      workspaceAbsent,
      refundStatus: typeof deletion.refundStatus === "string" && deletion.refundStatus.trim()
        ? deletion.refundStatus.trim()
        : null,
      refundOperationId: typeof deletion.refundOperationId === "string" && deletion.refundOperationId.trim()
        ? deletion.refundOperationId.trim()
        : null,
    },
    mutations: { declared: { deletions: 1 }, observed: { deletions: resumed ? 0 : 1 } },
    startedAt,
    completedAt,
  });
}

/*
 * The receipt is closed to exactly these keys, which is what keeps a Session, a
 * balance, a raw account identifier or the Workspace name out of it: there is no
 * field for one.
 *
 * `workspaceAbsent` records the owner deletion facts and retained tombstone.
 * A succeeded operation must have that confirmation; other outcomes may still
 * have confirmed resource absence and an independently unresolved obligation.
 * `resumed` and the observed count are tied the same way,
 * so a delivery cannot claim to have caused nothing while reporting a fresh
 * command.
 */
export function validateDeployedDeleteReceipt(value) {
  const status = String(value?.operation?.status || "");
  if (!hasExactKeys(value, RECEIPT_KEYS) || value.schemaVersion !== 1 ||
      value.kind !== DEPLOYED_DELETE_RECEIPT_KIND ||
      !hasExactKeys(value.candidate, CANDIDATE_KEYS) || !DIGEST.test(String(value.candidate.receiptDigest || "")) ||
      !hasExactKeys(value.account, ACCOUNT_KEYS) || !DIGEST.test(String(value.account.identityDigest || "")) ||
      !hasExactKeys(value.operation, OPERATION_KEYS) ||
      !IDEMPOTENCY_KEY.test(String(value.operation.idempotencyKey || "")) ||
      !String(value.operation.idempotencyKey || "").startsWith(
        `deployed-workspace-delete:${String(value.candidate.receiptDigest || "").slice("sha256:".length)}:`
      ) ||
      !String(value.operation.operationId || "").trim() ||
      !OPERATION_STATES.includes(status) ||
      typeof value.operation.stage !== "string" ||
      (value.operation.stage !== "" && !TOKEN.test(value.operation.stage)) ||
      typeof value.operation.resumed !== "boolean" ||
      !hasExactKeys(value.deletion, DELETION_KEYS) ||
      typeof value.deletion.workspaceAbsent !== "boolean" ||
      (value.deletion.refundStatus !== null &&
        (typeof value.deletion.refundStatus !== "string" || !TOKEN.test(value.deletion.refundStatus))) ||
      (value.deletion.refundOperationId !== null && typeof value.deletion.refundOperationId !== "string") ||
      status === "succeeded" && !value.deletion.workspaceAbsent ||
      !hasExactKeys(value.mutations, MUTATION_KEYS) ||
      !hasExactKeys(value.mutations.declared, DELETION_MUTATION_KEYS) ||
      !hasExactKeys(value.mutations.observed, DELETION_MUTATION_KEYS) ||
      value.mutations.declared.deletions !== 1 ||
      ![0, 1].includes(value.mutations.observed.deletions) ||
      value.mutations.observed.deletions > value.mutations.declared.deletions ||
      value.operation.resumed !== (value.mutations.observed.deletions === 0) ||
      typeof value.startedAt !== "string" || !Number.isFinite(Date.parse(value.startedAt)) ||
      typeof value.completedAt !== "string" || !Number.isFinite(Date.parse(value.completedAt)) ||
      Date.parse(value.completedAt) < Date.parse(value.startedAt)) {
    throw deleteError("deployed_delete_receipt_invalid", "deployed delete receipt is invalid");
  }
  return value;
}
