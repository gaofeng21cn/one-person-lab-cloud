import { createHash, randomBytes } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { isAbsolute, join } from "node:path";

import { canonicalJson } from "./cloud-candidate-receipt.ts";

/*
 * The Cloud-owned lifecycle seam for one exact Workspace-managed Gateway Key.
 *
 * A Workspace may be closed or suspended and later recovered. Neither event
 * re-issues a key: it asks the external Gateway authority to disable, re-enable
 * or permanently revoke the one Key whose ownership this Workspace and external
 * subject already proved. This module owns that decision, its operation record
 * and its settlement rule. It owns no Key, no wallet, no Gateway authority and
 * no provider transport.
 *
 * What is deliberately NOT here:
 *
 * - No Key issuance. The provider boundary has no method that can mint a key, so
 *   no retry, replay or recovery path can create a replacement.
 * - No same-Key model-scope update. That external contract does not exist yet;
 *   this seam only disables, re-enables or revokes an existing key.
 * - No raw Key or raw external subject. The binding carries an external subject
 *   digest and an external Key identity; a provider readback is compared and
 *   discarded, never persisted.
 * - No shared Key store. The persisted record is this operation's journal. A
 *   production store is the calling Cloud owner's own persistence (for example
 *   Workspace's own PostgreSQL rows), never a new cross-owner key table.
 * - No provider SDK and no guessed administrative endpoint. Gateway Integration
 *   supplies a provider adapter that implements the declared boundary below.
 *
 * Current external facts this source must not pretend away: Gateway Integration
 * exposes issuance, but `RevokeWorkspaceKey` still returns `codes.Unimplemented`
 * and there is no approved same-Key disable, enable or state-readback service
 * contract yet. A provider adapter for this seam therefore does not exist in
 * production. The seam is intentionally caller-less: no owner route currently
 * requests a reversible Key disable, and this change does not invent one. Every
 * outcome below is a local/test-double result, never evidence of live control.
 *
 * The settlement rule is one sentence: a lifecycle operation is `settled` only
 * when the provider authoritatively confirms the exact declared binding and the
 * exact declared Key is in the intent's desired state. An unknown outcome and a
 * lost or unreadable response are `recovery-required`, never success. Resource
 * absence, Secret removal and deployment health are recorded when a caller
 * declares them, and never decide the Key's state.
 */

// The typed contract this seam consumes. `bindingSchema` names the existing
// Gateway `ManagedKeyBinding` facts (key binding id, workspace, external key
// id, fingerprint). `lifecycleSchema` is the required service contract for
// disable, enable and state readback; it is not deployed yet, so any provider
// adapter implementing it is necessarily a test or a future approved adapter.
export const REQUIRED_GATEWAY_INTEGRATION_CONTRACT = Object.freeze({
  owner: "gateway-integration",
  bindingSchema: "opl.gateway.managed-key-binding.v1",
  lifecycleSchema: "opl.gateway.workspace-key-lifecycle.v1",
  bindingFields: Object.freeze(["keyBindingId", "workspaceId", "externalKeyId", "fingerprint"]),
  operations: Object.freeze(["readBinding", "readState", "disable", "enable", "revoke"])
});

export const WORKSPACE_KEY_STATUSES = Object.freeze(["active", "disabled", "revoked"] as const);
export type WorkspaceKeyStatus = (typeof WORKSPACE_KEY_STATUSES)[number];

// `disable` is reversible, `enable` restores the same key, and `revoke` is the
// permanent, separate security effect. A revoked key is never re-enabled by
// this seam and is never reported as merely disabled.
export const WORKSPACE_KEY_LIFECYCLE_INTENTS = Object.freeze(["disable", "enable", "revoke"] as const);
export type WorkspaceKeyLifecycleIntent = (typeof WORKSPACE_KEY_LIFECYCLE_INTENTS)[number];

export const WORKSPACE_KEY_PROVIDER_OPERATIONS = Object.freeze(
  ["readBinding", "readState", "disable", "enable", "revoke"] as const
);
export type WorkspaceKeyProviderOperation = (typeof WORKSPACE_KEY_PROVIDER_OPERATIONS)[number];

export type WorkspaceKeyLifecycleState = "pending" | "settled" | "recovery-required" | "blocked";

/*
 * Every state names exactly the reasons it may be reached by. Keeping the table
 * closed means a record cannot carry one outcome's evidence under another
 * outcome's name, and an unknown reason is a corrupt record rather than a new
 * undocumented state.
 */
export const WORKSPACE_KEY_LIFECYCLE_REASONS = Object.freeze({
  pending: Object.freeze(["created", "dispatching"]),
  settled: Object.freeze(["confirmed"]),
  blocked: Object.freeze([
    "ownership_ambiguous", "ownership_mismatch", "ownership_unverified", "key_state_unconfirmed", "key_revoked",
    "capability_unsupported", "idempotency_conflict", "stale_applied_version", "provider_refused", "store_unavailable"
  ]),
  "recovery-required": Object.freeze([
    "provider_outcome_unknown", "response_lost", "readback_unconfirmed", "readback_identity_mismatch", "state_mismatch",
    "terminal_state_mismatch", "provider_capability_unavailable", "stale_cas", "store_unavailable"
  ])
});

const OPERATION_KIND = "opl.workspace-key-lifecycle.operation.v1";
const IDEMPOTENCY_PREFIX = "opl-workspace-key-lifecycle:";
const DESIRED_STATUS: Readonly<Record<WorkspaceKeyLifecycleIntent, WorkspaceKeyStatus>> = Object.freeze({
  disable: "disabled",
  enable: "active",
  revoke: "revoked"
});

const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const FINGERPRINT = /^sha256:[0-9a-f]{64}$/u;
const OPERATION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const WORKSPACE_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const EXTERNAL_KEY_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/u;
const GATEWAY_KEY_BINDING_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const PROVIDER_CODE = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/u;
const INSTANT = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/u;
const CONTROL = /[\u0000-\u001f\u007f]/u;

export class WorkspaceKeyLifecycleError extends Error {
  readonly code: string;

  constructor(code: string) {
    super(code);
    this.name = "WorkspaceKeyLifecycleError";
    this.code = code;
  }
}

/*
 * The adapter signals that the request may have reached the authority but its
 * answer did not come back. Any other throw from a mutation is treated the same
 * way, because nothing this seam can observe proves no effect happened.
 */
export class WorkspaceKeyProviderResponseLostError extends Error {
  constructor(message = "workspace key provider response was lost") {
    super(message);
    this.name = "WorkspaceKeyProviderResponseLostError";
  }
}

export class WorkspaceKeyLifecycleStoreConflictError extends Error {
  constructor(message = "workspace key lifecycle operation already exists") {
    super(message);
    this.name = "WorkspaceKeyLifecycleStoreConflictError";
  }
}

// The exact binding. `externalSubjectDigest` is a SHA-256 of the verified
// external subject identity; the raw subject never enters this seam, the
// operation record or a receipt.
export type WorkspaceManagedKeyBinding = Readonly<{
  workspaceId: string;
  externalSubjectDigest: string;
  keyId: string;
  gateway: Readonly<{
    contract: string;
    keyBindingId: string;
    fingerprint: string;
  }>;
}>;

/*
 * Facts a caller may have observed elsewhere in the same lifecycle. They are
 * recorded so an operator can see them beside the Key's own evidence, and they
 * are never used to settle the operation: a successful deployment, an absent
 * provider resource and a removed Secret all describe something other than the
 * external Key's state.
 */
export type WorkspaceKeyLifecycleNonKeyEvidence = Readonly<{
  deploymentResult: string;
  resourceAbsent: boolean;
  secretRemoved: boolean;
}>;

export type WorkspaceKeyLifecycleNonKeyEvidenceInput = Readonly<{
  deploymentResult?: string;
  resourceAbsent?: boolean;
  secretRemoved?: boolean;
}>;

export type WorkspaceKeyLifecycleRequest = Readonly<{
  operationId: string;
  intent: WorkspaceKeyLifecycleIntent;
  binding: WorkspaceManagedKeyBinding;
  /**
   * The operation version the caller last applied. A mismatch is refused before
   * any provider read or mutation. Undefined means "whatever version this
   * operation already has"; a new operation then starts at version 0.
   */
  expectedAppliedVersion?: number;
  nonKeyEvidence?: WorkspaceKeyLifecycleNonKeyEvidenceInput;
}>;

export type WorkspaceKeyProviderReadRequest = Readonly<{ binding: WorkspaceManagedKeyBinding }>;

export type WorkspaceKeyProviderMutationRequest = Readonly<{
  binding: WorkspaceManagedKeyBinding;
  intent: WorkspaceKeyLifecycleIntent;
  idempotencyKey: string;
}>;

// The Gateway Integration side of the boundary confirms the exact recorded
// binding: the same Workspace, external Key identity and Gateway binding.
export type WorkspaceKeyBindingReadResult =
  | Readonly<{ outcome: "binding"; binding: WorkspaceManagedKeyBinding }>
  | Readonly<{ outcome: "missing" }>
  | Readonly<{ outcome: "ambiguous"; candidates: number }>
  | Readonly<{ outcome: "unknown" }>;

// The Sub2API side of the boundary confirms the exact external Key's state.
// The adapter normalizes the provider vocabulary (`inactive` -> `disabled`,
// for example) and reports `unknown` for any state this contract does not name.
export type WorkspaceKeyReadResult =
  | Readonly<{ outcome: "state"; keyId: string; externalSubjectDigest: string; status: WorkspaceKeyStatus }>
  | Readonly<{ outcome: "missing" }>
  | Readonly<{ outcome: "ambiguous"; candidates: number }>
  | Readonly<{ outcome: "unknown" }>;

export type WorkspaceKeyMutationResult =
  | Readonly<{ outcome: "confirmed" }>
  | Readonly<{ outcome: "unknown" }>
  | Readonly<{ outcome: "refused"; code: string }>
  | Readonly<{ outcome: "unsupported"; code: string }>;

/*
 * The declared provider boundary, injected as a dependency.
 *
 * `readBinding` and `readState` are required. A mutation capability is declared
 * only when the approved adapter really provides it, so a provider that cannot
 * revoke (the current production state) is a valid provider that yields a
 * blocked `capability_unsupported`, not a missing method discovered halfway
 * through an effect. Every mutation must be idempotent by `idempotencyKey` for
 * the exact binding: a recovery repeats the same effect under the same key.
 */
export type WorkspaceKeyLifecycleProvider = Readonly<{
  contract: string;
  capabilities: readonly WorkspaceKeyProviderOperation[];
  readBinding(request: WorkspaceKeyProviderReadRequest): Promise<WorkspaceKeyBindingReadResult>;
  readState(request: WorkspaceKeyProviderReadRequest): Promise<WorkspaceKeyReadResult>;
  disable?(request: WorkspaceKeyProviderMutationRequest): Promise<WorkspaceKeyMutationResult>;
  enable?(request: WorkspaceKeyProviderMutationRequest): Promise<WorkspaceKeyMutationResult>;
  revoke?(request: WorkspaceKeyProviderMutationRequest): Promise<WorkspaceKeyMutationResult>;
}>;

/*
 * The persistent operation record.
 *
 * `bindingVerified` says a provider confirmed the exact binding before the
 * mutation this record may have dispatched. `appliedVersion` is the CAS
 * version: every transition is written with the version the writer read, and a
 * writer that lost the race changes nothing.
 */
export type WorkspaceKeyLifecycleRecord = Readonly<{
  schemaVersion: 1;
  kind: typeof OPERATION_KIND;
  operationId: string;
  requestDigest: string;
  idempotencyKey: string;
  intent: WorkspaceKeyLifecycleIntent;
  binding: WorkspaceManagedKeyBinding;
  appliedVersion: number;
  state: WorkspaceKeyLifecycleState;
  reason: string;
  providerCode: string;
  attempts: number;
  mutationAttempted: boolean;
  bindingVerified: boolean;
  readback: Readonly<{ status: WorkspaceKeyStatus | ""; observedAt: string }>;
  nonKeyEvidence: WorkspaceKeyLifecycleNonKeyEvidence;
  createdAt: string;
  updatedAt: string;
}>;

/*
 * A store is the calling owner's persistence. `create` is an atomic
 * create-if-absent; `compareAndSwap` writes `next` only when the stored record
 * still has `expectedAppliedVersion`. File-backed stores are single-writer
 * qualification journals; a production owner store must implement the same
 * interface with its own transactional CAS.
 */
export type WorkspaceKeyLifecycleStore = Readonly<{
  read(operationId: string): Promise<WorkspaceKeyLifecycleRecord | null>;
  create(record: WorkspaceKeyLifecycleRecord): Promise<void>;
  compareAndSwap(
    operationId: string,
    expectedAppliedVersion: number,
    next: WorkspaceKeyLifecycleRecord
  ): Promise<boolean>;
}>;

export type WorkspaceKeyLifecycleResult = Readonly<{
  operationId: string;
  intent: WorkspaceKeyLifecycleIntent;
  idempotencyKey: string;
  state: WorkspaceKeyLifecycleState;
  reason: string;
  appliedVersion: number;
  /** True only when this invocation dispatched the lifecycle mutation. */
  mutated: boolean;
  /** True when an existing record was resumed or replayed. */
  replayed: boolean;
  /** The persisted record, or null when nothing could be persisted. */
  record: WorkspaceKeyLifecycleRecord | null;
}>;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === "object" && !Array.isArray(value);

function exactKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  return isRecord(value) && Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

const sha256 = (value: string): string => createHash("sha256").update(value).digest("hex");
const digestOf = (value: string): string => `sha256:${sha256(value)}`;
const isDigest = (value: unknown): value is string => typeof value === "string" && DIGEST.test(value);
const isFingerprint = (value: unknown): value is string => typeof value === "string" && FINGERPRINT.test(value);
const isInstant = (value: unknown): value is string =>
  typeof value === "string" && INSTANT.test(value) && Number.isFinite(Date.parse(value));
const isProviderCode = (value: unknown): value is string =>
  value === "" || (typeof value === "string" && PROVIDER_CODE.test(value));
const isBoundedText = (value: unknown, limit: number): value is string =>
  typeof value === "string" && value.length <= limit && !CONTROL.test(value);

function lifecycleError(code: string): WorkspaceKeyLifecycleError {
  return new WorkspaceKeyLifecycleError(code);
}

/*
 * The digest a caller derives from a verified external subject. This module
 * deliberately accepts only the digest: an adapter resolves the subject through
 * its own owner binding and compares the digest there.
 */
export function externalSubjectDigest(subject: string): string {
  const value = subject;
  if (typeof value !== "string" || !value || value.length > 256 || value !== value.trim() || CONTROL.test(value)) {
    throw lifecycleError("binding_invalid");
  }
  return digestOf(value);
}

function parseBinding(value: unknown, code: string): WorkspaceManagedKeyBinding {
  if (!exactKeys(value, ["workspaceId", "externalSubjectDigest", "keyId", "gateway"]) ||
      typeof value.workspaceId !== "string" || !WORKSPACE_ID.test(value.workspaceId) ||
      !isDigest(value.externalSubjectDigest) ||
      typeof value.keyId !== "string" || !EXTERNAL_KEY_ID.test(value.keyId)) {
    throw lifecycleError(code);
  }
  const gateway = value.gateway;
  if (!exactKeys(gateway, ["contract", "keyBindingId", "fingerprint"]) ||
      gateway.contract !== REQUIRED_GATEWAY_INTEGRATION_CONTRACT.bindingSchema ||
      typeof gateway.keyBindingId !== "string" || !GATEWAY_KEY_BINDING_ID.test(gateway.keyBindingId) ||
      !isFingerprint(gateway.fingerprint)) {
    throw lifecycleError(code);
  }
  return Object.freeze({
    workspaceId: value.workspaceId,
    externalSubjectDigest: value.externalSubjectDigest,
    keyId: value.keyId,
    gateway: Object.freeze({
      contract: gateway.contract,
      keyBindingId: gateway.keyBindingId,
      fingerprint: gateway.fingerprint
    })
  });
}

function parseIntent(value: unknown): WorkspaceKeyLifecycleIntent {
  if (typeof value !== "string" || !WORKSPACE_KEY_LIFECYCLE_INTENTS.includes(value as WorkspaceKeyLifecycleIntent)) {
    throw lifecycleError("request_invalid");
  }
  return value as WorkspaceKeyLifecycleIntent;
}

function parseNonKeyEvidence(value: unknown): WorkspaceKeyLifecycleNonKeyEvidence {
  if (value === undefined) {
    return Object.freeze({ deploymentResult: "", resourceAbsent: false, secretRemoved: false });
  }
  if (!isRecord(value) ||
      Object.keys(value).some((key) => !["deploymentResult", "resourceAbsent", "secretRemoved"].includes(key)) ||
      (value.deploymentResult !== undefined && !isBoundedText(value.deploymentResult, 128)) ||
      (value.resourceAbsent !== undefined && typeof value.resourceAbsent !== "boolean") ||
      (value.secretRemoved !== undefined && typeof value.secretRemoved !== "boolean")) {
    throw lifecycleError("request_invalid");
  }
  return Object.freeze({
    deploymentResult: value.deploymentResult === undefined ? "" : value.deploymentResult,
    resourceAbsent: value.resourceAbsent === true,
    secretRemoved: value.secretRemoved === true
  });
}

function requestDigestOf(intent: WorkspaceKeyLifecycleIntent, binding: WorkspaceManagedKeyBinding): string {
  return digestOf(canonicalJson({ intent, binding }));
}

function idempotencyKeyOf(
  operationId: string,
  intent: WorkspaceKeyLifecycleIntent,
  binding: WorkspaceManagedKeyBinding
): string {
  return `${IDEMPOTENCY_PREFIX}${sha256(`${operationId}\n${requestDigestOf(intent, binding)}`)}`;
}

/*
 * The idempotency key is derived, not stored by the caller. A replay of the same
 * operation, intent and exact binding therefore addresses the same provider
 * effect, and a changed intent or binding under the same operation id is a
 * conflict rather than a second effect.
 */
export function workspaceKeyLifecycleIdempotencyKey(
  operationId: string,
  intent: WorkspaceKeyLifecycleIntent,
  binding: WorkspaceManagedKeyBinding
): string {
  if (typeof operationId !== "string" || !OPERATION_ID.test(operationId)) throw lifecycleError("request_invalid");
  const parsedIntent = parseIntent(intent);
  const parsedBinding = parseBinding(binding, "binding_invalid");
  return idempotencyKeyOf(operationId, parsedIntent, parsedBinding);
}

type PreparedRequest = Readonly<{
  operationId: string;
  intent: WorkspaceKeyLifecycleIntent;
  binding: WorkspaceManagedKeyBinding;
  expectedAppliedVersion?: number;
  nonKeyEvidence: WorkspaceKeyLifecycleNonKeyEvidence;
  requestDigest: string;
  idempotencyKey: string;
}>;

function prepareRequest(value: WorkspaceKeyLifecycleRequest): PreparedRequest {
  if (!isRecord(value) ||
      Object.keys(value).some((key) =>
        !["operationId", "intent", "binding", "expectedAppliedVersion", "nonKeyEvidence"].includes(key)) ||
      typeof value.operationId !== "string" || !OPERATION_ID.test(value.operationId)) {
    throw lifecycleError("request_invalid");
  }
  const intent = parseIntent(value.intent);
  const binding = parseBinding(value.binding, "binding_invalid");
  const expectedAppliedVersion = value.expectedAppliedVersion;
  if (expectedAppliedVersion !== undefined &&
      (!Number.isSafeInteger(expectedAppliedVersion) || (expectedAppliedVersion as number) < 0)) {
    throw lifecycleError("request_invalid");
  }
  const nonKeyEvidence = parseNonKeyEvidence(value.nonKeyEvidence);
  const requestDigest = requestDigestOf(intent, binding);
  return Object.freeze({
    operationId: value.operationId,
    intent,
    binding,
    ...(expectedAppliedVersion === undefined ? {} : { expectedAppliedVersion: expectedAppliedVersion as number }),
    nonKeyEvidence,
    requestDigest,
    idempotencyKey: idempotencyKeyOf(value.operationId, intent, binding)
  });
}

type NormalizedProvider = Readonly<{
  provider: WorkspaceKeyLifecycleProvider;
  capabilities: ReadonlySet<WorkspaceKeyProviderOperation>;
}>;

function assertProvider(value: unknown): NormalizedProvider {
  if (!isRecord(value) || value.contract !== REQUIRED_GATEWAY_INTEGRATION_CONTRACT.lifecycleSchema) {
    throw lifecycleError("provider_invalid");
  }
  if (!Array.isArray(value.capabilities) || value.capabilities.length === 0) throw lifecycleError("provider_invalid");
  const capabilities = new Set<WorkspaceKeyProviderOperation>();
  for (const capability of value.capabilities) {
    if (typeof capability !== "string" ||
        !WORKSPACE_KEY_PROVIDER_OPERATIONS.includes(capability as WorkspaceKeyProviderOperation) ||
        capabilities.has(capability as WorkspaceKeyProviderOperation)) {
      throw lifecycleError("provider_invalid");
    }
    capabilities.add(capability as WorkspaceKeyProviderOperation);
  }
  if (!capabilities.has("readBinding") || typeof value.readBinding !== "function" ||
      !capabilities.has("readState") || typeof value.readState !== "function") {
    throw lifecycleError("provider_invalid");
  }
  for (const capability of capabilities) {
    if (capability !== "readBinding" && capability !== "readState" && typeof value[capability] !== "function") {
      throw lifecycleError("provider_invalid");
    }
  }
  return Object.freeze({ provider: value as unknown as WorkspaceKeyLifecycleProvider, capabilities });
}

function assertStore(value: unknown): WorkspaceKeyLifecycleStore {
  if (!isRecord(value) || typeof value.read !== "function" ||
      typeof value.create !== "function" || typeof value.compareAndSwap !== "function") {
    throw lifecycleError("store_invalid");
  }
  return value as unknown as WorkspaceKeyLifecycleStore;
}

function mutationFor(
  provider: NormalizedProvider,
  intent: WorkspaceKeyLifecycleIntent
): ((request: WorkspaceKeyProviderMutationRequest) => Promise<WorkspaceKeyMutationResult>) | null {
  if (!provider.capabilities.has(intent)) return null;
  const method = provider.provider[intent];
  return typeof method === "function" ? method.bind(provider.provider) : null;
}

function normalizeReadResult(value: unknown): WorkspaceKeyReadResult {
  if (exactKeys(value, ["outcome"]) && value.outcome === "missing") return Object.freeze({ outcome: "missing" });
  if (exactKeys(value, ["outcome"]) && value.outcome === "unknown") return Object.freeze({ outcome: "unknown" });
  if (exactKeys(value, ["outcome", "candidates"]) && value.outcome === "ambiguous" &&
      Number.isSafeInteger(value.candidates) && (value.candidates as number) >= 2) {
    return Object.freeze({ outcome: "ambiguous", candidates: value.candidates as number });
  }
  if (exactKeys(value, ["outcome", "keyId", "externalSubjectDigest", "status"]) && value.outcome === "state" &&
      typeof value.keyId === "string" && EXTERNAL_KEY_ID.test(value.keyId) &&
      isDigest(value.externalSubjectDigest) &&
      WORKSPACE_KEY_STATUSES.includes(value.status as WorkspaceKeyStatus)) {
    return Object.freeze({
      outcome: "state",
      keyId: value.keyId,
      externalSubjectDigest: value.externalSubjectDigest,
      status: value.status as WorkspaceKeyStatus
    });
  }
  return Object.freeze({ outcome: "unknown" });
}

function normalizeBindingReadResult(value: unknown): WorkspaceKeyBindingReadResult {
  if (exactKeys(value, ["outcome"]) && value.outcome === "missing") return Object.freeze({ outcome: "missing" });
  if (exactKeys(value, ["outcome"]) && value.outcome === "unknown") return Object.freeze({ outcome: "unknown" });
  if (exactKeys(value, ["outcome", "candidates"]) && value.outcome === "ambiguous" &&
      Number.isSafeInteger(value.candidates) && (value.candidates as number) >= 2) {
    return Object.freeze({ outcome: "ambiguous", candidates: value.candidates as number });
  }
  if (exactKeys(value, ["outcome", "binding"]) && value.outcome === "binding") {
    try {
      return Object.freeze({ outcome: "binding", binding: parseBinding(value.binding, "provider_invalid") });
    } catch {
      return Object.freeze({ outcome: "unknown" });
    }
  }
  return Object.freeze({ outcome: "unknown" });
}

type NormalizedMutation =
  | Readonly<{ outcome: "confirmed" }>
  | Readonly<{ outcome: "unknown"; reason: "provider_outcome_unknown" | "response_lost" }>
  | Readonly<{ outcome: "refused"; code: string }>
  | Readonly<{ outcome: "unsupported"; code: string }>;

function normalizeMutationResult(value: unknown): NormalizedMutation {
  if (exactKeys(value, ["outcome"]) && value.outcome === "confirmed") return Object.freeze({ outcome: "confirmed" });
  if (exactKeys(value, ["outcome"]) && value.outcome === "unknown") {
    return Object.freeze({ outcome: "unknown", reason: "provider_outcome_unknown" });
  }
  if (exactKeys(value, ["outcome", "code"]) && value.outcome === "refused" && isProviderCode(value.code) && value.code !== "") {
    return Object.freeze({ outcome: "refused", code: value.code });
  }
  if (exactKeys(value, ["outcome", "code"]) && value.outcome === "unsupported" && isProviderCode(value.code) && value.code !== "") {
    return Object.freeze({ outcome: "unsupported", code: value.code });
  }
  return Object.freeze({ outcome: "unknown", reason: "provider_outcome_unknown" });
}

type ReadClassification =
  | Readonly<{ kind: "state"; status: WorkspaceKeyStatus }>
  | Readonly<{ kind: "ambiguous" }>
  | Readonly<{ kind: "missing" }>
  | Readonly<{ kind: "unknown" }>
  | Readonly<{ kind: "mismatch" }>;

function classifyRead(result: WorkspaceKeyReadResult, binding: WorkspaceManagedKeyBinding): ReadClassification {
  if (result.outcome === "ambiguous") return Object.freeze({ kind: "ambiguous" });
  if (result.outcome === "missing") return Object.freeze({ kind: "missing" });
  if (result.outcome === "unknown") return Object.freeze({ kind: "unknown" });
  if (result.keyId !== binding.keyId || result.externalSubjectDigest !== binding.externalSubjectDigest) {
    return Object.freeze({ kind: "mismatch" });
  }
  return Object.freeze({ kind: "state", status: result.status });
}

function bindingMatches(value: WorkspaceManagedKeyBinding, binding: WorkspaceManagedKeyBinding): boolean {
  return value.workspaceId === binding.workspaceId &&
    value.externalSubjectDigest === binding.externalSubjectDigest &&
    value.keyId === binding.keyId &&
    value.gateway.contract === binding.gateway.contract &&
    value.gateway.keyBindingId === binding.gateway.keyBindingId &&
    value.gateway.fingerprint === binding.gateway.fingerprint;
}

const RECORD_KEYS = Object.freeze([
  "schemaVersion", "kind", "operationId", "requestDigest", "idempotencyKey", "intent", "binding", "appliedVersion",
  "state", "reason", "providerCode", "attempts", "mutationAttempted", "bindingVerified", "readback", "nonKeyEvidence",
  "createdAt", "updatedAt"
]);

/*
 * The record is closed to exactly these fields. There is no field for a raw Key,
 * a raw Secret or a raw external subject: such material has no place to be
 * written even by mistake.
 */
export function assertWorkspaceKeyLifecycleRecord(value: unknown): WorkspaceKeyLifecycleRecord {
  if (!exactKeys(value, RECORD_KEYS) || value.schemaVersion !== 1 || value.kind !== OPERATION_KIND ||
      typeof value.operationId !== "string" || !OPERATION_ID.test(value.operationId) ||
      !isDigest(value.requestDigest) || typeof value.idempotencyKey !== "string" || !value.idempotencyKey.startsWith(IDEMPOTENCY_PREFIX) ||
      !Number.isSafeInteger(value.appliedVersion) || (value.appliedVersion as number) < 0 ||
      !Number.isSafeInteger(value.attempts) || (value.attempts as number) < 0 ||
      typeof value.mutationAttempted !== "boolean" || typeof value.bindingVerified !== "boolean" ||
      !isProviderCode(value.providerCode) ||
      !isInstant(value.createdAt) || !isInstant(value.updatedAt) ||
      Date.parse(value.updatedAt) < Date.parse(value.createdAt)) {
    throw lifecycleError("record_invalid");
  }
  let intent: WorkspaceKeyLifecycleIntent;
  let binding: WorkspaceManagedKeyBinding;
  try {
    intent = parseIntent(value.intent);
    binding = parseBinding(value.binding, "record_invalid");
  } catch {
    throw lifecycleError("record_invalid");
  }
  if (value.state !== "pending" && value.state !== "settled" &&
      value.state !== "recovery-required" && value.state !== "blocked") {
    throw lifecycleError("record_invalid");
  }
  if (typeof value.reason !== "string" || !WORKSPACE_KEY_LIFECYCLE_REASONS[value.state].includes(value.reason)) {
    throw lifecycleError("record_invalid");
  }
  if (value.requestDigest !== requestDigestOf(intent, binding) ||
      value.idempotencyKey !== idempotencyKeyOf(value.operationId, intent, binding)) {
    throw lifecycleError("record_invalid");
  }
  if (!exactKeys(value.readback, ["status", "observedAt"])) throw lifecycleError("record_invalid");
  const status = value.readback.status;
  if (status !== "" && !WORKSPACE_KEY_STATUSES.includes(status as WorkspaceKeyStatus)) throw lifecycleError("record_invalid");
  if ((status === "" && value.readback.observedAt !== "") ||
      (status !== "" && !isInstant(value.readback.observedAt))) {
    throw lifecycleError("record_invalid");
  }
  if (!exactKeys(value.nonKeyEvidence, ["deploymentResult", "resourceAbsent", "secretRemoved"]) ||
      !isBoundedText(value.nonKeyEvidence.deploymentResult, 128) ||
      typeof value.nonKeyEvidence.resourceAbsent !== "boolean" ||
      typeof value.nonKeyEvidence.secretRemoved !== "boolean") {
    throw lifecycleError("record_invalid");
  }
  return value as unknown as WorkspaceKeyLifecycleRecord;
}

const nowInstant = (): string => new Date().toISOString();
const emptyReadback = (): WorkspaceKeyLifecycleRecord["readback"] => Object.freeze({ status: "", observedAt: "" });
const readbackOf = (status: WorkspaceKeyStatus): WorkspaceKeyLifecycleRecord["readback"] =>
  Object.freeze({ status, observedAt: nowInstant() });

function initialRecord(request: PreparedRequest): WorkspaceKeyLifecycleRecord {
  const now = nowInstant();
  return Object.freeze({
    schemaVersion: 1,
    kind: OPERATION_KIND,
    operationId: request.operationId,
    requestDigest: request.requestDigest,
    idempotencyKey: request.idempotencyKey,
    intent: request.intent,
    binding: request.binding,
    appliedVersion: 0,
    state: "pending",
    reason: "created",
    providerCode: "",
    attempts: 0,
    mutationAttempted: false,
    bindingVerified: false,
    readback: emptyReadback(),
    nonKeyEvidence: request.nonKeyEvidence,
    createdAt: now,
    updatedAt: now
  });
}

type RecordPatch = Partial<Omit<WorkspaceKeyLifecycleRecord,
  "schemaVersion" | "kind" | "operationId" | "requestDigest" | "idempotencyKey" | "intent" | "binding" |
  "appliedVersion" | "createdAt" | "updatedAt">>;

type TransitionOutcome =
  | Readonly<{ kind: "ok"; record: WorkspaceKeyLifecycleRecord }>
  | Readonly<{ kind: "stale" }>
  | Readonly<{ kind: "error" }>;

async function transition(
  store: WorkspaceKeyLifecycleStore,
  current: WorkspaceKeyLifecycleRecord,
  patch: RecordPatch
): Promise<TransitionOutcome> {
  const next: WorkspaceKeyLifecycleRecord = Object.freeze({
    ...current,
    ...patch,
    appliedVersion: current.appliedVersion + 1,
    updatedAt: nowInstant()
  });
  try {
    return await store.compareAndSwap(current.operationId, current.appliedVersion, next)
      ? Object.freeze({ kind: "ok", record: next })
      : Object.freeze({ kind: "stale" });
  } catch {
    return Object.freeze({ kind: "error" });
  }
}

function resultOf(
  request: PreparedRequest,
  record: WorkspaceKeyLifecycleRecord | null,
  state: WorkspaceKeyLifecycleState,
  reason: string,
  mutated: boolean,
  replayed: boolean
): WorkspaceKeyLifecycleResult {
  return Object.freeze({
    operationId: request.operationId,
    intent: request.intent,
    idempotencyKey: request.idempotencyKey,
    state,
    reason,
    appliedVersion: record ? record.appliedVersion : 0,
    mutated,
    replayed,
    record
  });
}

function failedTransition(
  request: PreparedRequest,
  record: WorkspaceKeyLifecycleRecord,
  kind: "stale" | "error",
  mutated: boolean,
  replayed: boolean
): WorkspaceKeyLifecycleResult {
  // A transition that fails after the dispatch fence cannot prove what the
  // provider did, so it is recovery-required. Before that fence nothing was
  // mutated and the operation is blocked.
  const attempted = record.mutationAttempted;
  const state: WorkspaceKeyLifecycleState = attempted ? "recovery-required" : "blocked";
  const reason = kind === "stale"
    ? (attempted ? "stale_cas" : "stale_applied_version")
    : "store_unavailable";
  return resultOf(request, record, state, reason, mutated, replayed);
}

async function transitionTo(
  store: WorkspaceKeyLifecycleStore,
  request: PreparedRequest,
  record: WorkspaceKeyLifecycleRecord,
  patch: RecordPatch,
  mutated: boolean,
  replayed: boolean
): Promise<WorkspaceKeyLifecycleResult> {
  const outcome = await transition(store, record, patch);
  if (outcome.kind !== "ok") return failedTransition(request, record, outcome.kind, mutated, replayed);
  return resultOf(request, outcome.record, outcome.record.state, outcome.record.reason, mutated, replayed);
}

async function readBinding(
  provider: NormalizedProvider,
  binding: WorkspaceManagedKeyBinding
): Promise<WorkspaceKeyBindingReadResult> {
  try {
    return normalizeBindingReadResult(await provider.provider.readBinding({ binding }));
  } catch {
    return Object.freeze({ outcome: "unknown" });
  }
}

async function readState(
  provider: NormalizedProvider,
  binding: WorkspaceManagedKeyBinding
): Promise<WorkspaceKeyReadResult> {
  try {
    return normalizeReadResult(await provider.provider.readState({ binding }));
  } catch {
    return Object.freeze({ outcome: "unknown" });
  }
}

/*
 * Run one lifecycle decision.
 *
 * The operation is a value, not an exception: a provider that cannot prove its
 * effect yields a `recovery-required` record, and an ownership or request
 * conflict yields `blocked`. Only a malformed request, an invalid binding or a
 * broken dependency wiring throws, because those are programming errors rather
 * than outcomes.
 */
export async function runWorkspaceKeyLifecycle(
  request: WorkspaceKeyLifecycleRequest,
  dependencies: Readonly<{ provider: WorkspaceKeyLifecycleProvider; store: WorkspaceKeyLifecycleStore }>
): Promise<WorkspaceKeyLifecycleResult> {
  const prepared = prepareRequest(request);
  const provider = assertProvider(dependencies?.provider);
  const store = assertStore(dependencies?.store);

  let existing: WorkspaceKeyLifecycleRecord | null;
  try {
    existing = await store.read(prepared.operationId);
  } catch {
    return resultOf(prepared, null, "blocked", "store_unavailable", false, false);
  }

  let record: WorkspaceKeyLifecycleRecord;
  let replayed = false;
  if (existing) {
    if (existing.requestDigest !== prepared.requestDigest || existing.idempotencyKey !== prepared.idempotencyKey) {
      // The same operation id under a different intent or binding is not a
      // replay. Nothing is mutated and the original record is left untouched.
      return resultOf(prepared, existing, "blocked", "idempotency_conflict", false, true);
    }
    if (prepared.expectedAppliedVersion !== undefined && existing.appliedVersion !== prepared.expectedAppliedVersion) {
      return resultOf(prepared, existing, "blocked", "stale_applied_version", false, true);
    }
    if (existing.state === "settled" || existing.state === "blocked") {
      return resultOf(prepared, existing, existing.state, existing.reason, false, true);
    }
    // `pending` and `recovery-required` describe an operation whose original
    // effect may still be in flight. A replay resumes it rather than starting
    // anything new.
    record = existing;
    replayed = true;
  } else {
    if (prepared.expectedAppliedVersion !== undefined && prepared.expectedAppliedVersion !== 0) {
      return resultOf(prepared, null, "blocked", "stale_applied_version", false, false);
    }
    record = initialRecord(prepared);
    try {
      await store.create(record);
    } catch (error) {
      const reason = error instanceof WorkspaceKeyLifecycleStoreConflictError ? "stale_applied_version" : "store_unavailable";
      return resultOf(prepared, null, "blocked", reason, false, false);
    }
  }

  // Ownership first: the exact Gateway binding has to be confirmed before the
  // Key's state is even read, and both have to be confirmed before any mutation.
  const bindingRead = await readBinding(provider, prepared.binding);
  const bindingVerified = bindingRead.outcome === "binding" && bindingMatches(bindingRead.binding, prepared.binding);
  if (!bindingVerified) {
    // An unreadable ownership answer is an unknown provider outcome even though
    // no mutation was dispatched: it stays recovery-required so a later pass can
    // read the same binding again. A definite ambiguity or mismatch fails closed
    // before any mutation instead.
    if (bindingRead.outcome === "unknown") {
      return transitionTo(store, prepared, record, {
        state: "recovery-required", reason: "readback_unconfirmed", providerCode: "", readback: record.readback
      }, false, replayed);
    }
    const preMutationReason = bindingRead.outcome === "ambiguous" ? "ownership_ambiguous"
      : bindingRead.outcome === "missing" ? "ownership_unverified"
      : "ownership_mismatch";
    const attempted = record.mutationAttempted;
    return transitionTo(store, prepared, record, {
      state: attempted ? "recovery-required" : "blocked",
      reason: attempted
        ? (bindingRead.outcome === "binding" ? "readback_identity_mismatch" : "readback_unconfirmed")
        : preMutationReason,
      providerCode: "",
      readback: record.readback
    }, false, replayed);
  }

  const stateRead = await readState(provider, prepared.binding);
  const classified = classifyRead(stateRead, prepared.binding);
  if (classified.kind !== "state") {
    if (classified.kind === "unknown") {
      return transitionTo(store, prepared, record, {
        state: "recovery-required",
        reason: "readback_unconfirmed",
        providerCode: "",
        bindingVerified: record.bindingVerified || bindingVerified,
        readback: record.readback
      }, false, replayed);
    }
    const attempted = record.mutationAttempted;
    const preMutationReason = classified.kind === "mismatch" ? "ownership_mismatch"
      : classified.kind === "ambiguous" ? "ownership_ambiguous"
      : "key_state_unconfirmed";
    const postMutationReason = classified.kind === "mismatch" ? "readback_identity_mismatch" : "readback_unconfirmed";
    return transitionTo(store, prepared, record, {
      state: attempted ? "recovery-required" : "blocked",
      reason: attempted ? postMutationReason : preMutationReason,
      providerCode: "",
      bindingVerified: record.bindingVerified || bindingVerified,
      readback: record.readback
    }, false, replayed);
  }

  const status = classified.status;
  const readback = readbackOf(status);
  const desired = DESIRED_STATUS[prepared.intent];

  // The key is already in the state this intent asks for. No mutation is issued;
  // the operation still settles only on this authoritative exact-Key readback.
  if (status === desired) {
    return transitionTo(store, prepared, record, {
      state: "settled",
      reason: "confirmed",
      providerCode: "",
      bindingVerified: true,
      readback
    }, false, replayed);
  }

  // A permanent revocation is not a stronger disablement. Once the exact key is
  // revoked, a disable or enable intent cannot reach its reversible target and
  // fails closed instead of pretending the key is merely disabled.
  if (status === "revoked" && prepared.intent !== "revoke") {
    const attempted = record.mutationAttempted;
    return transitionTo(store, prepared, record, {
      state: attempted ? "recovery-required" : "blocked",
      reason: attempted ? "terminal_state_mismatch" : "key_revoked",
      providerCode: "",
      bindingVerified: true,
      readback
    }, false, replayed);
  }

  const mutate = mutationFor(provider, prepared.intent);
  if (!mutate) {
    // The provider does not declare this effect. That is a blocked request, not
    // an unknown mutation, unless an earlier attempt already dispatched one.
    const attempted = record.mutationAttempted;
    return transitionTo(store, prepared, record, {
      state: attempted ? "recovery-required" : "blocked",
      reason: attempted ? "provider_capability_unavailable" : "capability_unsupported",
      providerCode: "",
      bindingVerified: true,
      readback
    }, false, replayed);
  }

  // Persist the dispatch fence before calling the provider. If this CAS loses,
  // nothing was mutated; if the process dies after this write, a replay knows an
  // effect may exist and must read it back before dispatching again.
  const dispatch = await transition(store, record, {
    state: "pending",
    reason: "dispatching",
    providerCode: "",
    attempts: record.attempts + 1,
    mutationAttempted: true,
    bindingVerified: true,
    readback
  });
  if (dispatch.kind !== "ok") return failedTransition(prepared, record, dispatch.kind, false, replayed);
  record = dispatch.record;

  let mutation: NormalizedMutation;
  try {
    mutation = normalizeMutationResult(await mutate({
      binding: prepared.binding,
      intent: prepared.intent,
      idempotencyKey: prepared.idempotencyKey
    }));
  } catch (error) {
    mutation = error instanceof WorkspaceKeyProviderResponseLostError
      ? Object.freeze({ outcome: "unknown", reason: "response_lost" })
      : Object.freeze({ outcome: "unknown", reason: "provider_outcome_unknown" });
  }

  if (mutation.outcome === "unsupported") {
    return transitionTo(store, prepared, record, {
      state: "blocked", reason: "capability_unsupported", providerCode: mutation.code, readback
    }, true, replayed);
  }
  if (mutation.outcome === "refused") {
    // A bounded refusal answered before any effect. The provider code is the
    // fact the operator acts on and is preserved exactly.
    return transitionTo(store, prepared, record, {
      state: "blocked", reason: "provider_refused", providerCode: mutation.code, readback
    }, true, replayed);
  }
  if (mutation.outcome === "unknown") {
    return transitionTo(store, prepared, record, {
      state: "recovery-required", reason: mutation.reason, providerCode: "", readback
    }, true, replayed);
  }

  // A confirmed mutation is still only a provider assertion. Settlement needs a
  // second authoritative readback of the exact key's state.
  const after = await readState(provider, prepared.binding);
  const afterClass = classifyRead(after, prepared.binding);
  if (afterClass.kind === "state" && afterClass.status === desired) {
    return transitionTo(store, prepared, record, {
      state: "settled",
      reason: "confirmed",
      providerCode: "",
      bindingVerified: true,
      readback: readbackOf(afterClass.status)
    }, true, replayed);
  }
  const recoveryReason = afterClass.kind === "mismatch" ? "readback_identity_mismatch"
    : afterClass.kind === "state" && afterClass.status === "revoked" && prepared.intent !== "revoke" ? "terminal_state_mismatch"
    : afterClass.kind === "state" ? "state_mismatch"
    : "readback_unconfirmed";
  return transitionTo(store, prepared, record, {
    state: "recovery-required",
    reason: recoveryReason,
    providerCode: "",
    bindingVerified: true,
    readback: afterClass.kind === "state" ? readbackOf(afterClass.status) : readback
  }, true, replayed);
}

/*
 * A file-backed operation journal.
 *
 * This is deliberately not a shared Key store and not a production database:
 * each record is one operation's evidence, contains no Key material, and is
 * written atomically with mode 0600. It is a single-writer qualification store;
 * a production owner store must implement the same interface with its own
 * transactional compare-and-swap.
 */
export async function createFileWorkspaceKeyLifecycleStore(directory: string): Promise<WorkspaceKeyLifecycleStore> {
  const root = String(directory ?? "");
  if (!root || !isAbsolute(root)) throw lifecycleError("store_invalid");
  await mkdir(root, { recursive: true });
  const pathFor = (operationId: string): string => {
    if (typeof operationId !== "string" || !OPERATION_ID.test(operationId)) throw lifecycleError("operation_invalid");
    return join(root, `${operationId}.json`);
  };
  const readRecord = async (operationId: string): Promise<WorkspaceKeyLifecycleRecord | null> => {
    let encoded: string;
    try {
      encoded = await readFile(pathFor(operationId), "utf8");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return null;
      throw error;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(encoded);
    } catch {
      throw lifecycleError("record_invalid");
    }
    return assertWorkspaceKeyLifecycleRecord(parsed);
  };
  return Object.freeze({
    async read(operationId: string): Promise<WorkspaceKeyLifecycleRecord | null> {
      return readRecord(operationId);
    },
    async create(record: WorkspaceKeyLifecycleRecord): Promise<void> {
      const next = assertWorkspaceKeyLifecycleRecord(record);
      try {
        await writeFile(pathFor(next.operationId), `${JSON.stringify(next, null, 2)}\n`, { flag: "wx", mode: 0o600 });
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code === "EEXIST") throw new WorkspaceKeyLifecycleStoreConflictError();
        throw error;
      }
    },
    async compareAndSwap(
      operationId: string,
      expectedAppliedVersion: number,
      nextRecord: WorkspaceKeyLifecycleRecord
    ): Promise<boolean> {
      const next = assertWorkspaceKeyLifecycleRecord(nextRecord);
      if (next.operationId !== operationId || next.appliedVersion !== expectedAppliedVersion + 1) {
        throw lifecycleError("record_invalid");
      }
      const path = pathFor(operationId);
      const current = await readRecord(operationId);
      if (!current || current.appliedVersion !== expectedAppliedVersion) return false;
      const temporary = `${path}.${process.pid}.${randomBytes(4).toString("hex")}.tmp`;
      try {
        await writeFile(temporary, `${JSON.stringify(next, null, 2)}\n`, { mode: 0o600 });
        await rename(temporary, path);
      } catch (error) {
        await rm(temporary, { force: true });
        throw error;
      }
      return true;
    }
  });
}
