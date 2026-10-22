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
 * that session's account back through the owner's source envelope and refuses to
 * destroy anything until the readback matches the declared account digest. The
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

import { deployedAccountIdentityDigest } from "./deployed-workspace-purchase.ts";
import { sourceData } from "./local-workspace-qualification.ts";

export const DEPLOYED_DELETE_RECEIPT_KIND = "opl.deployed-workspace-delete.v1";

const DIGEST = /^sha256:[a-f0-9]{64}$/;
const WORKSPACE_ID = /^ws-[a-z0-9]{1,64}$/;
const WORKSPACE_NAME = /^[\x20-\x7e]{1,120}$/;
const IDEMPOTENCY_IDENTITY = /^[A-Za-z0-9._:-]{1,128}$/;
const IDEMPOTENCY_KEY = /^deployed-workspace-delete:[0-9a-f]{64}:[A-Za-z0-9._:-]{1,128}$/;
const TOKEN = /^[a-z0-9_]{1,64}$/;

const TARGET_ERROR =
  "deployed delete target must be one https production origin, declared as authorityClass production";

// The owner publishes exactly these deletion states. `pending` is still working,
// `manual_review` stopped and needs a human, `deleted` is terminal success. A
// token outside this set is a contract this entry does not understand, so it is
// refused rather than recorded as if it were one of them.
const DELETION_STATES = Object.freeze(["pending", "manual_review", "deleted"]);

const REQUEST_KEYS = [
  "baseURL",
  "authorityClass",
  "candidateReceiptDigest",
  "accountEmailIdentityDigest",
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
const DELETE_POLL_INTERVAL_MS = 1000;

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
      !DIGEST.test(String(request?.accountEmailIdentityDigest || "")) ||
      !WORKSPACE_ID.test(String(request?.workspaceId || "")) ||
      !WORKSPACE_NAME.test(String(request?.workspaceName || "")) ||
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

async function readOwnerEnvelope(http, auth, path, source, fail) {
  let result;
  try {
    result = await http.request(path, {}, auth);
  } catch {
    throw fail();
  }
  try {
    return sourceData(result?.payload, source);
  } catch {
    throw fail();
  }
}

/*
 * The owner's deletion readback.
 *
 * `null` means the Workspace exists and no deletion was ever accepted, which is
 * the only state in which this entry may issue a command. A 404 means the
 * Workspace itself is gone, and that is reported rather than read as "nothing to
 * do": a Workspace this entry cannot see is not one it can prove it deleted.
 */
async function readDeletion(http, auth, workspaceId) {
  let result;
  try {
    result = await http.request(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deletion`, {}, auth);
  } catch {
    throw deleteError("deployed_delete_readback_unavailable", "deployed delete could not read the deletion state");
  }
  const status = Number(result?.response?.status || 0);
  if (status === 404) throw workspaceUnavailable();
  if (status !== 200) {
    throw deleteError("deployed_delete_readback_unavailable", "deployed delete could not read the deletion state");
  }
  const deletion = result?.payload;
  if (deletion === null || deletion === undefined) return null;
  if (!deletion || typeof deletion !== "object" ||
      String(deletion.workspaceId || "") !== workspaceId ||
      !DELETION_STATES.includes(String(deletion.status || ""))) {
    throw deleteError("deployed_delete_readback_invalid", "deployed delete read a deletion state it does not understand");
  }
  return deletion;
}

async function readWorkspace(http, auth, workspaceId) {
  let result;
  try {
    result = await http.request(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth);
  } catch {
    throw workspaceUnavailable();
  }
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200) throw workspaceUnavailable();
  return result?.payload ?? null;
}

async function waitForDeletion(http, auth, workspaceId, wait) {
  let deletion = null;
  for (let attempt = 0; attempt < DELETE_POLL_ATTEMPTS; attempt += 1) {
    deletion = await readDeletion(http, auth, workspaceId);
    if (!deletion) {
      throw deleteError("deployed_delete_command_not_accepted", "deployed delete did not find an accepted deletion after issuing one");
    }
    if (deletion.status !== "pending") return deletion;
    await wait(DELETE_POLL_INTERVAL_MS);
  }
  // Still `pending` after the bounded window: the outcome is genuinely unknown,
  // so this entry reports that rather than inventing a terminal state.
  throw deleteError(
    "deployed_delete_timeout",
    `deployed delete did not reach a terminal state within ${DELETE_POLL_ATTEMPTS} seconds`,
  );
}

function deletionStage(deletion) {
  const stage = String(deletion?.stage || "");
  return TOKEN.test(stage) ? stage : "";
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

  // The session is asked who it is before anything is destroyed. A transport
  // holding another account costs one read and no destruction.
  const me = await readOwnerEnvelope(http, auth, "/api/auth/me", "sub2api", accountUnavailable);
  const accountId = String(me?.accountId || "");
  if (!accountId || me?.role !== "owner" || me?.status !== "active") throw accountUnavailable();
  const identityDigest = deployedAccountIdentityDigest(me?.email);
  if (identityDigest !== request.accountEmailIdentityDigest) {
    throw deleteError("deployed_delete_account_mismatch", "deployed delete account digest does not match the declared customer");
  }

  // The Workspace must be visible to this session before it is destroyed, and it
  // must be the one the caller named. Destroying a Workspace this entry could not
  // read first would be destroying something it cannot describe.
  const workspace = await readWorkspace(http, auth, workspaceId);
  if (!workspace) throw workspaceUnavailable();
  const observedAccount = String(workspace?.accountId || workspace?.ownerAccountId || "");
  if (observedAccount && observedAccount !== accountId) {
    throw deleteError("deployed_delete_workspace_mismatch", "deployed delete read a Workspace owned by another account");
  }
  if (String(workspace?.name || "") !== request.workspaceName) {
    throw deleteError("deployed_delete_workspace_mismatch", "deployed delete Workspace name does not match the declared confirmation");
  }

  let deletion = await readDeletion(http, auth, workspaceId);
  let resumed = false;

  if (deletion) {
    // A deletion was already accepted. Issuing a second command would be a second
    // mutation for one declared intent, so this delivery follows the existing one
    // and reports that it caused nothing.
    resumed = true;
    if (deletion.status === "pending") deletion = await waitForDeletion(http, auth, workspaceId, wait);
  } else {
    let issued;
    try {
      issued = await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {
        method: "DELETE",
        headers: { "idempotency-key": idempotencyKey },
        body: {
          confirmationName: request.workspaceName,
          acknowledgeDataDestruction: true,
        },
      }, auth, [200, 202]);
    } catch (error) {
      // The command may have been accepted with its answer lost. That is not a
      // second deletion, it is the same one, and the only question left is
      // whether the owner holds it.
      let recovered;
      try {
        recovered = await readDeletion(http, auth, workspaceId);
      } catch (readbackError) {
        if (readbackError && typeof readbackError === "object") readbackError.cause = error;
        throw readbackError;
      }
      if (!recovered) throw error;
      issued = null;
    }
    const accepted = issued?.payload;
    if (accepted && typeof accepted === "object" &&
        accepted.kind !== undefined && String(accepted.kind) !== "delete_workspace") {
      throw deleteError("deployed_delete_operation_mismatch", "deployed delete read an operation that is not a Workspace deletion");
    }
    deletion = await waitForDeletion(http, auth, workspaceId, wait);
  }

  const status = String(deletion.status || "");
  // The owner publishes `deleted` only after its own residual readback, so the
  // absence is asked again here rather than inferred from the status alone.
  const after = status === "deleted" ? await readWorkspace(http, auth, workspaceId) : workspace;
  const workspaceAbsent = status === "deleted" && after === null;
  if (status === "deleted" && !workspaceAbsent) {
    throw deleteError(
      "deployed_delete_residual_workspace",
      "deployed delete reported a deleted Workspace that the owner still serves",
    );
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
      status,
      stage: deletionStage(deletion),
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
 * `workspaceAbsent` is tied to the published state in both directions: a receipt
 * may claim absence only for a `deleted` deletion, and a `deleted` deletion must
 * have proven absence. `resumed` and the observed count are tied the same way,
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
      !DELETION_STATES.includes(status) ||
      typeof value.operation.stage !== "string" ||
      (value.operation.stage !== "" && !TOKEN.test(value.operation.stage)) ||
      typeof value.operation.resumed !== "boolean" ||
      !hasExactKeys(value.deletion, DELETION_KEYS) ||
      typeof value.deletion.workspaceAbsent !== "boolean" ||
      (value.deletion.refundStatus !== null &&
        (typeof value.deletion.refundStatus !== "string" || !TOKEN.test(value.deletion.refundStatus))) ||
      (value.deletion.refundOperationId !== null && typeof value.deletion.refundOperationId !== "string") ||
      value.deletion.workspaceAbsent !== (status === "deleted") ||
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
