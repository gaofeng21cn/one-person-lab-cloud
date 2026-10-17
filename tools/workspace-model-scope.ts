import { createHash, randomBytes } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { isAbsolute, join } from "node:path";

import { canonicalJson } from "./cloud-candidate-receipt.ts";

/*
 * The Cloud-owned seam for one approved model-scope change on one exact
 * Workspace-managed Gateway Key.
 *
 * A Workspace may change the model configuration its application uses without
 * giving up the external Key it already holds. This module owns that decision,
 * the applied-version compare-and-swap (CAS) over it, the intended scope and the
 * settlement rule. Gateway Integration coordinates the same-Key scope update
 * through Sub2API, Fabric owns the runtime Secret binding and provider readback,
 * and Serve owns the application apply and its authoritative readback. This
 * module owns none of those authorities: both owner readbacks are injected.
 *
 * The one sentence this seam enforces: an operation succeeds only when the
 * Gateway readback confirms the exact bound Key now carries scope B AND the
 * Serve readback confirms the exact target application/version applied scope B.
 * Ambiguous ownership, an unknown provider outcome, a lost response and a
 * Gateway/Serve disagreement are never success.
 *
 * What is deliberately NOT here:
 *
 * - No Key issuance. The Gateway boundary has no method that can mint a Key, so
 *   no retry, replay or recovery path can create a replacement.
 * - No scope expansion. Scope B must be a subset of the already approved scope
 *   A for every model and every group. A request that widens either is refused
 *   before any provider read or mutation. Widening needs a separate approved
 *   contract, not this seam.
 * - No raw Key, raw external subject or Secret value. The binding carries an
 *   external subject digest and an external Key identity; readbacks are
 *   persisted by their scope only.
 * - No shared Key store. The persisted record is this operation's journal. A
 *   production store is the calling Workspace owner's own persistence, never a
 *   new cross-owner Key table.
 * - No provider SDK and no guessed administrative endpoint.
 *
 * Current external facts this source must not pretend away: there is no
 * same-Key model-scope update endpoint in Sub2API today, and Gateway Integration
 * exposes no approved scope-update/readback contract. `UpdateUserKey` can change
 * name, group, quota, limits and enabled state, but not a model-scope
 * authorization; `Sub2APIWorkspaceKey` carries no model-access set. The seam is
 * caller-less and every result below comes from a declared fake boundary. It is
 * local behavior evidence, never evidence of live provider or application
 * control.
 */

// The typed contract this seam consumes. The binding schema names the existing
// Gateway managed-key binding facts. The model-scope schema is the required
// service contract for a same-Key scope read and update; it is not deployed
// yet, so any adapter implementing it is a test double or a future approved
// adapter.
export const REQUIRED_GATEWAY_INTEGRATION_CONTRACT = Object.freeze({
  owner: "gateway-integration",
  bindingSchema: "opl.gateway.managed-key-binding.v1",
  modelScopeSchema: "opl.gateway.workspace-model-scope.v1",
  bindingFields: Object.freeze(["keyBindingId", "workspaceId", "externalKeyId", "fingerprint"]),
  operations: Object.freeze(["readBinding", "readScope", "updateScope"])
});

// Serve's side is the application apply and its authoritative readback. The
// target is the exact application and immutable configuration version the
// caller approved, never a version inferred from the apply request.
export const REQUIRED_SERVE_APPLICATION_CONTRACT = Object.freeze({
  owner: "serve",
  schema: "opl.serve.workspace-application-model-scope.v1",
  operations: Object.freeze(["readApplication", "apply"])
});

export const GATEWAY_MODEL_SCOPE_OPERATIONS = Object.freeze(["readBinding", "readScope", "updateScope"] as const);
export type GatewayModelScopeOperation = (typeof GATEWAY_MODEL_SCOPE_OPERATIONS)[number];

export const SERVE_MODEL_SCOPE_OPERATIONS = Object.freeze(["readApplication", "apply"] as const);
export type ServeModelScopeOperation = (typeof SERVE_MODEL_SCOPE_OPERATIONS)[number];

/*
 * The convergence state machine. `requested` and `gateway_confirmed` are
 * progress states; `applied`, `converged_original` and `recovery-required` are
 * terminal for one invocation and only `applied` reports success.
 *
 *   requested -> gateway_confirmed -> applied
 *                                  -> converged_original
 *                                  -> recovery-required
 *
 * A recovery record may be resumed. It never becomes success without a fresh
 * pair of authoritative owner readbacks.
 */
export const WORKSPACE_MODEL_SCOPE_STATES = Object.freeze([
  "requested", "gateway_confirmed", "applied", "converged_original", "recovery-required"
] as const);
export type WorkspaceModelScopeState = (typeof WORKSPACE_MODEL_SCOPE_STATES)[number];

// Every state names exactly the reasons it may be reached by. A record cannot
// carry one outcome's evidence under another outcome's name, and an unknown
// reason is a corrupt record rather than an undocumented state.
export const WORKSPACE_MODEL_SCOPE_REASONS = Object.freeze({
  requested: Object.freeze(["created", "dispatching_scope"]),
  gateway_confirmed: Object.freeze(["confirmed", "applying"]),
  applied: Object.freeze(["confirmed"]),
  converged_original: Object.freeze([
    "confirmed", "gateway_refused", "apply_refused", "capability_unsupported",
    "application_capability_unavailable", "apply_unconfirmed", "gateway_scope_unconfirmed"
  ]),
  "recovery-required": Object.freeze([
    "ownership_ambiguous", "ownership_unverified", "ownership_mismatch", "capability_unsupported",
    "idempotency_conflict", "stale_applied_version", "stale_cas", "store_unavailable",
    "provider_outcome_unknown", "response_lost", "readback_unconfirmed", "readback_identity_mismatch",
    "gateway_scope_unexpected", "gateway_scope_unconfirmed", "scope_mismatch",
    "application_readback_mismatch", "application_version_mismatch", "both_readbacks_disagree",
    "restore_unconfirmed", "restore_refused", "application_capability_unavailable"
  ])
});

const OPERATION_KIND = "opl.workspace-model-scope.operation.v1";
const IDEMPOTENCY_PREFIX = "opl-workspace-model-scope:";

const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const FINGERPRINT = /^sha256:[0-9a-f]{64}$/u;
const OPERATION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const WORKSPACE_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const EXTERNAL_KEY_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/u;
const GATEWAY_KEY_BINDING_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const APPLICATION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,191}$/u;
const SCOPE_ITEM = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/u;
const PROVIDER_CODE = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/u;
const INSTANT = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/u;
const CONTROL = /[\u0000-\u001f\u007f]/u;

export class WorkspaceModelScopeError extends Error {
  readonly code: string;

  constructor(code: string) {
    super(code);
    this.name = "WorkspaceModelScopeError";
    this.code = code;
  }
}

/*
 * The adapter signals that the request may have reached the authority but its
 * answer did not come back. Any other throw from a mutation is treated the same
 * way, because nothing this seam can observe proves no effect happened.
 */
export class WorkspaceModelScopeGatewayResponseLostError extends Error {
  constructor(message = "gateway model-scope response was lost") {
    super(message);
    this.name = "WorkspaceModelScopeGatewayResponseLostError";
  }
}

export class WorkspaceModelScopeServeResponseLostError extends Error {
  constructor(message = "serve model-scope response was lost") {
    super(message);
    this.name = "WorkspaceModelScopeServeResponseLostError";
  }
}

export class WorkspaceModelScopeStoreConflictError extends Error {
  constructor(message = "workspace model-scope operation already exists") {
    super(message);
    this.name = "WorkspaceModelScopeStoreConflictError";
  }
}

// The exact Key binding, matching #948's owned lifecycle binding. The digest is
// a SHA-256 of the verified external subject; the raw subject never enters this
// seam, the operation record or a receipt.
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
 * The authorization a Key carries for model use: the models it may call and the
 * Sub2API groups it may spend against. The two sets are declared, sorted and
 * unique so one authorization has exactly one digest. Scope B is accepted only
 * when it is a subset of the already approved scope A.
 */
export type WorkspaceModelScope = Readonly<{
  models: readonly string[];
  groups: readonly string[];
}>;

export type WorkspaceApplicationTarget = Readonly<{
  applicationId: string;
  version: number;
}>;

export type WorkspaceModelScopeRequest = Readonly<{
  operationId: string;
  binding: WorkspaceManagedKeyBinding;
  scopeA: WorkspaceModelScope;
  scopeB: WorkspaceModelScope;
  target: WorkspaceApplicationTarget;
  /**
   * The operation version the caller last applied. A mismatch is refused before
   * any provider read or mutation. Undefined means "whatever version this
   * operation already has"; a new operation then starts at version 0.
   */
  expectedAppliedVersion?: number;
}>;

export type GatewayBindingReadResult =
  | Readonly<{ outcome: "binding"; binding: WorkspaceManagedKeyBinding }>
  | Readonly<{ outcome: "missing" }>
  | Readonly<{ outcome: "ambiguous"; candidates: number }>
  | Readonly<{ outcome: "unknown" }>;

export type GatewayScopeReadResult =
  | Readonly<{ outcome: "scope"; binding: WorkspaceManagedKeyBinding; scope: WorkspaceModelScope }>
  | Readonly<{ outcome: "missing" }>
  | Readonly<{ outcome: "ambiguous"; candidates: number }>
  | Readonly<{ outcome: "unknown" }>;

export type GatewayScopeMutationResult =
  | Readonly<{ outcome: "confirmed" }>
  | Readonly<{ outcome: "unknown" }>
  | Readonly<{ outcome: "refused"; code: string }>
  | Readonly<{ outcome: "unsupported"; code: string }>;

export type GatewayModelScopeProvider = Readonly<{
  contract: string;
  capabilities: readonly GatewayModelScopeOperation[];
  readBinding(request: Readonly<{ binding: WorkspaceManagedKeyBinding }>): Promise<GatewayBindingReadResult>;
  readScope(request: Readonly<{ binding: WorkspaceManagedKeyBinding }>): Promise<GatewayScopeReadResult>;
  updateScope?(request: Readonly<{
    binding: WorkspaceManagedKeyBinding;
    scope: WorkspaceModelScope;
    idempotencyKey: string;
  }>): Promise<GatewayScopeMutationResult>;
}>;

export type ServeApplicationReadResult =
  | Readonly<{
    outcome: "application";
    application: Readonly<{
      applicationId: string;
      workspaceId: string;
      version: number;
      scope: WorkspaceModelScope;
    }>;
  }>
  | Readonly<{ outcome: "missing" }>
  | Readonly<{ outcome: "ambiguous"; candidates: number }>
  | Readonly<{ outcome: "unknown" }>;

export type ServeApplicationApplyResult =
  | Readonly<{ outcome: "confirmed" }>
  | Readonly<{ outcome: "unknown" }>
  | Readonly<{ outcome: "refused"; code: string }>
  | Readonly<{ outcome: "unsupported"; code: string }>;

export type ServeModelScopeBoundary = Readonly<{
  contract: string;
  capabilities: readonly ServeModelScopeOperation[];
  readApplication(request: Readonly<{ target: WorkspaceApplicationTarget }>): Promise<ServeApplicationReadResult>;
  apply?(request: Readonly<{
    target: WorkspaceApplicationTarget;
    scope: WorkspaceModelScope;
    idempotencyKey: string;
  }>): Promise<ServeApplicationApplyResult>;
}>;

type GatewayReadback = Readonly<{ scope: WorkspaceModelScope | null; observedAt: string }>;
type ApplicationReadback = Readonly<{
  applicationId: string;
  version: number;
  scope: WorkspaceModelScope | null;
  observedAt: string;
}>;

/*
 * The persistent operation record.
 *
 * `gatewayMutationAttempted` and `serveMutationAttempted` are dispatch fences:
 * they are written before the boundary call, so a replay knows an effect may
 * exist even when the process died before receiving an answer. `appliedVersion`
 * is the CAS version: every transition is written with the version the writer
 * read, and a writer that lost the race changes nothing.
 */
export type WorkspaceModelScopeRecord = Readonly<{
  schemaVersion: 1;
  kind: typeof OPERATION_KIND;
  operationId: string;
  requestDigest: string;
  idempotencyKey: string;
  binding: WorkspaceManagedKeyBinding;
  scopeA: WorkspaceModelScope;
  scopeB: WorkspaceModelScope;
  target: WorkspaceApplicationTarget;
  appliedVersion: number;
  state: WorkspaceModelScopeState;
  reason: string;
  gatewayCode: string;
  serveCode: string;
  attempts: number;
  gatewayMutationAttempted: boolean;
  serveMutationAttempted: boolean;
  bindingVerified: boolean;
  gatewayReadback: GatewayReadback;
  applicationReadback: ApplicationReadback;
  createdAt: string;
  updatedAt: string;
}>;

export type WorkspaceModelScopeStore = Readonly<{
  read(operationId: string): Promise<WorkspaceModelScopeRecord | null>;
  create(record: WorkspaceModelScopeRecord): Promise<void>;
  compareAndSwap(
    operationId: string,
    expectedAppliedVersion: number,
    next: WorkspaceModelScopeRecord
  ): Promise<boolean>;
}>;

export type WorkspaceModelScopeResult = Readonly<{
  operationId: string;
  idempotencyKey: string;
  state: WorkspaceModelScopeState;
  reason: string;
  /** True only for `applied`, where both owner readbacks agree on scope B. */
  success: boolean;
  appliedVersion: number;
  /** True only when this invocation dispatched a Gateway or Serve mutation. */
  mutated: boolean;
  /** True when an existing record was resumed or replayed. */
  replayed: boolean;
  gatewayMutationAttempted: boolean;
  serveMutationAttempted: boolean;
  record: WorkspaceModelScopeRecord | null;
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

function modelScopeError(code: string): WorkspaceModelScopeError {
  return new WorkspaceModelScopeError(code);
}

/*
 * The digest a caller derives from a verified external subject. This module
 * deliberately accepts only the digest: an adapter resolves the subject through
 * its own owner binding and compares the digest there.
 */
export function externalSubjectDigest(subject: string): string {
  if (typeof subject !== "string" || !subject || subject.length > 256 ||
      subject !== subject.trim() || CONTROL.test(subject)) {
    throw modelScopeError("binding_invalid");
  }
  return digestOf(subject);
}

function parseBinding(value: unknown, code: string): WorkspaceManagedKeyBinding {
  if (!exactKeys(value, ["workspaceId", "externalSubjectDigest", "keyId", "gateway"]) ||
      typeof value.workspaceId !== "string" || !WORKSPACE_ID.test(value.workspaceId) ||
      !isDigest(value.externalSubjectDigest) ||
      typeof value.keyId !== "string" || !EXTERNAL_KEY_ID.test(value.keyId)) {
    throw modelScopeError(code);
  }
  const gateway = value.gateway;
  if (!exactKeys(gateway, ["contract", "keyBindingId", "fingerprint"]) ||
      gateway.contract !== REQUIRED_GATEWAY_INTEGRATION_CONTRACT.bindingSchema ||
      typeof gateway.keyBindingId !== "string" || !GATEWAY_KEY_BINDING_ID.test(gateway.keyBindingId) ||
      !isFingerprint(gateway.fingerprint)) {
    throw modelScopeError(code);
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

function scopeItems(value: unknown, code: string, required: boolean): readonly string[] {
  if (!Array.isArray(value) || (required && value.length === 0) || value.length > 256) throw modelScopeError(code);
  const seen = new Set<string>();
  const items: string[] = [];
  for (const item of value) {
    if (typeof item !== "string" || !SCOPE_ITEM.test(item) || seen.has(item)) throw modelScopeError(code);
    seen.add(item);
    items.push(item);
  }
  // The semantic value is a set. Sorting here gives one authorization exactly
  // one representation and therefore one request digest.
  return Object.freeze(items.sort());
}

function parseScope(value: unknown, code: string): WorkspaceModelScope {
  if (!exactKeys(value, ["models", "groups"])) throw modelScopeError(code);
  const models = scopeItems(value.models, code, true);
  const groups = scopeItems(value.groups, code, false);
  return Object.freeze({ models, groups });
}

/*
 * The one constructor for a declared scope. It sorts and freezes the two sets,
 * so two callers that name the same authorization produce the same scope and the
 * same request digest. Duplicate, unsorted or malformed entries are refused
 * rather than silently normalized.
 */
export function workspaceModelScope(value: Readonly<{
  models: readonly string[];
  groups?: readonly string[];
}>): WorkspaceModelScope {
  if (!isRecord(value)) throw modelScopeError("scope_invalid");
  return parseScope({ models: value.models, groups: value.groups ?? [] }, "scope_invalid");
}

function sameScope(a: WorkspaceModelScope, b: WorkspaceModelScope): boolean {
  return a.models.length === b.models.length && a.groups.length === b.groups.length &&
    a.models.every((model, index) => model === b.models[index]) &&
    a.groups.every((group, index) => group === b.groups[index]);
}

function scopeSubset(candidate: WorkspaceModelScope, approved: WorkspaceModelScope): boolean {
  const models = new Set(approved.models);
  const groups = new Set(approved.groups);
  return candidate.models.every((model) => models.has(model)) &&
    candidate.groups.every((group) => groups.has(group));
}

function parseApplicationTarget(value: unknown): WorkspaceApplicationTarget {
  if (!exactKeys(value, ["applicationId", "version"]) ||
      typeof value.applicationId !== "string" || !APPLICATION_ID.test(value.applicationId) ||
      !Number.isSafeInteger(value.version) || (value.version as number) <= 0) {
    throw modelScopeError("request_invalid");
  }
  return Object.freeze({ applicationId: value.applicationId, version: value.version as number });
}

function requestDigestOf(
  binding: WorkspaceManagedKeyBinding,
  scopeA: WorkspaceModelScope,
  scopeB: WorkspaceModelScope,
  target: WorkspaceApplicationTarget
): string {
  return digestOf(canonicalJson({ binding, scopeA, scopeB, target }));
}

function idempotencyKeyOf(operationId: string, requestDigest: string): string {
  return `${IDEMPOTENCY_PREFIX}${sha256(`${operationId}\n${requestDigest}`)}`;
}

function scopeReadbackOf(scope: WorkspaceModelScope): GatewayReadback {
  return Object.freeze({ scope, observedAt: new Date().toISOString() });
}

function applicationReadbackOf(observation: Readonly<{
  applicationId: string;
  version: number;
  scope: WorkspaceModelScope;
}>): ApplicationReadback {
  return Object.freeze({
    applicationId: observation.applicationId,
    version: observation.version,
    scope: observation.scope,
    observedAt: new Date().toISOString()
  });
}

type PreparedRequest = Readonly<{
  operationId: string;
  binding: WorkspaceManagedKeyBinding;
  scopeA: WorkspaceModelScope;
  scopeB: WorkspaceModelScope;
  target: WorkspaceApplicationTarget;
  expectedAppliedVersion?: number;
  requestDigest: string;
  idempotencyKey: string;
  serveIdempotencyKey: string;
  restoreIdempotencyKey: string;
}>;

function prepareRequest(value: WorkspaceModelScopeRequest): PreparedRequest {
  if (!isRecord(value) ||
      Object.keys(value).some((key) =>
        !["operationId", "binding", "scopeA", "scopeB", "target", "expectedAppliedVersion"].includes(key)) ||
      typeof value.operationId !== "string" || !OPERATION_ID.test(value.operationId)) {
    throw modelScopeError("request_invalid");
  }
  const binding = parseBinding(value.binding, "binding_invalid");
  const scopeA = parseScope(value.scopeA, "scope_invalid");
  const scopeB = parseScope(value.scopeB, "scope_invalid");
  if (!scopeSubset(scopeB, scopeA)) {
    // This is the one refusal that must happen before any owner is contacted:
    // a scope change never becomes a path to a wider model or group access.
    throw modelScopeError("scope_expansion_refused");
  }
  if (sameScope(scopeA, scopeB)) throw modelScopeError("scope_unchanged");
  const target = parseApplicationTarget(value.target);
  const expectedAppliedVersion = value.expectedAppliedVersion;
  if (expectedAppliedVersion !== undefined &&
      (!Number.isSafeInteger(expectedAppliedVersion) || (expectedAppliedVersion as number) < 0)) {
    throw modelScopeError("request_invalid");
  }
  const requestDigest = requestDigestOf(binding, scopeA, scopeB, target);
  const idempotencyKey = idempotencyKeyOf(value.operationId, requestDigest);
  return Object.freeze({
    operationId: value.operationId,
    binding,
    scopeA,
    scopeB,
    target,
    ...(expectedAppliedVersion === undefined ? {} : { expectedAppliedVersion: expectedAppliedVersion as number }),
    requestDigest,
    idempotencyKey,
    serveIdempotencyKey: `${idempotencyKey}:serve`,
    restoreIdempotencyKey: `${idempotencyKey}:restore`
  });
}

export function workspaceModelScopeIdempotencyKey(request: WorkspaceModelScopeRequest): string {
  return prepareRequest(request).idempotencyKey;
}

export function workspaceModelScopeServeIdempotencyKey(request: WorkspaceModelScopeRequest): string {
  return prepareRequest(request).serveIdempotencyKey;
}

type NormalizedGateway = Readonly<{
  provider: GatewayModelScopeProvider;
  capabilities: ReadonlySet<GatewayModelScopeOperation>;
}>;

function assertGatewayProvider(value: unknown): NormalizedGateway {
  if (!isRecord(value) || value.contract !== REQUIRED_GATEWAY_INTEGRATION_CONTRACT.modelScopeSchema) {
    throw modelScopeError("provider_invalid");
  }
  if (!Array.isArray(value.capabilities) || value.capabilities.length === 0) throw modelScopeError("provider_invalid");
  const capabilities = new Set<GatewayModelScopeOperation>();
  for (const capability of value.capabilities) {
    if (typeof capability !== "string" ||
        !GATEWAY_MODEL_SCOPE_OPERATIONS.includes(capability as GatewayModelScopeOperation) ||
        capabilities.has(capability as GatewayModelScopeOperation)) {
      throw modelScopeError("provider_invalid");
    }
    capabilities.add(capability as GatewayModelScopeOperation);
  }
  if (!capabilities.has("readBinding") || typeof value.readBinding !== "function" ||
      !capabilities.has("readScope") || typeof value.readScope !== "function") {
    throw modelScopeError("provider_invalid");
  }
  if (capabilities.has("updateScope") && typeof value.updateScope !== "function") {
    throw modelScopeError("provider_invalid");
  }
  return Object.freeze({ provider: value as unknown as GatewayModelScopeProvider, capabilities });
}

type NormalizedServe = Readonly<{
  boundary: ServeModelScopeBoundary;
  capabilities: ReadonlySet<ServeModelScopeOperation>;
}>;

function assertServeBoundary(value: unknown): NormalizedServe {
  if (!isRecord(value) || value.contract !== REQUIRED_SERVE_APPLICATION_CONTRACT.schema) {
    throw modelScopeError("serve_invalid");
  }
  if (!Array.isArray(value.capabilities) || value.capabilities.length === 0) throw modelScopeError("serve_invalid");
  const capabilities = new Set<ServeModelScopeOperation>();
  for (const capability of value.capabilities) {
    if (typeof capability !== "string" ||
        !SERVE_MODEL_SCOPE_OPERATIONS.includes(capability as ServeModelScopeOperation) ||
        capabilities.has(capability as ServeModelScopeOperation)) {
      throw modelScopeError("serve_invalid");
    }
    capabilities.add(capability as ServeModelScopeOperation);
  }
  if (!capabilities.has("readApplication") || typeof value.readApplication !== "function") {
    throw modelScopeError("serve_invalid");
  }
  if (capabilities.has("apply") && typeof value.apply !== "function") throw modelScopeError("serve_invalid");
  return Object.freeze({ boundary: value as unknown as ServeModelScopeBoundary, capabilities });
}

function assertStore(value: unknown): WorkspaceModelScopeStore {
  if (!isRecord(value) || typeof value.read !== "function" ||
      typeof value.create !== "function" || typeof value.compareAndSwap !== "function") {
    throw modelScopeError("store_invalid");
  }
  return value as unknown as WorkspaceModelScopeStore;
}

function gatewayUpdateFor(
  gateway: NormalizedGateway
): ((request: Readonly<{
  binding: WorkspaceManagedKeyBinding;
  scope: WorkspaceModelScope;
  idempotencyKey: string;
}>) => Promise<GatewayScopeMutationResult>) | null {
  if (!gateway.capabilities.has("updateScope")) return null;
  const method = gateway.provider.updateScope;
  return typeof method === "function" ? method.bind(gateway.provider) : null;
}

function serveApplyFor(
  serve: NormalizedServe
): ((request: Readonly<{
  target: WorkspaceApplicationTarget;
  scope: WorkspaceModelScope;
  idempotencyKey: string;
}>) => Promise<ServeApplicationApplyResult>) | null {
  if (!serve.capabilities.has("apply")) return null;
  const method = serve.boundary.apply;
  return typeof method === "function" ? method.bind(serve.boundary) : null;
}

function normalizeBindingReadResult(value: unknown): GatewayBindingReadResult {
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

function normalizeScopeReadResult(value: unknown): GatewayScopeReadResult {
  if (exactKeys(value, ["outcome"]) && value.outcome === "missing") return Object.freeze({ outcome: "missing" });
  if (exactKeys(value, ["outcome"]) && value.outcome === "unknown") return Object.freeze({ outcome: "unknown" });
  if (exactKeys(value, ["outcome", "candidates"]) && value.outcome === "ambiguous" &&
      Number.isSafeInteger(value.candidates) && (value.candidates as number) >= 2) {
    return Object.freeze({ outcome: "ambiguous", candidates: value.candidates as number });
  }
  if (exactKeys(value, ["outcome", "binding", "scope"]) && value.outcome === "scope") {
    try {
      return Object.freeze({
        outcome: "scope",
        binding: parseBinding(value.binding, "provider_invalid"),
        scope: parseScope(value.scope, "provider_invalid")
      });
    } catch {
      return Object.freeze({ outcome: "unknown" });
    }
  }
  return Object.freeze({ outcome: "unknown" });
}

function normalizeApplicationReadResult(value: unknown): ServeApplicationReadResult {
  if (exactKeys(value, ["outcome"]) && value.outcome === "missing") return Object.freeze({ outcome: "missing" });
  if (exactKeys(value, ["outcome"]) && value.outcome === "unknown") return Object.freeze({ outcome: "unknown" });
  if (exactKeys(value, ["outcome", "candidates"]) && value.outcome === "ambiguous" &&
      Number.isSafeInteger(value.candidates) && (value.candidates as number) >= 2) {
    return Object.freeze({ outcome: "ambiguous", candidates: value.candidates as number });
  }
  if (exactKeys(value, ["outcome", "application"]) && value.outcome === "application") {
    const application = value.application;
    try {
      if (!exactKeys(application, ["applicationId", "workspaceId", "version", "scope"]) ||
          typeof application.applicationId !== "string" || !APPLICATION_ID.test(application.applicationId) ||
          typeof application.workspaceId !== "string" || !WORKSPACE_ID.test(application.workspaceId) ||
          !Number.isSafeInteger(application.version) || (application.version as number) <= 0) {
        return Object.freeze({ outcome: "unknown" });
      }
      return Object.freeze({
        outcome: "application",
        application: Object.freeze({
          applicationId: application.applicationId,
          workspaceId: application.workspaceId,
          version: application.version as number,
          scope: parseScope(application.scope, "provider_invalid")
        })
      });
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
  if (exactKeys(value, ["outcome", "code"]) && value.outcome === "refused" &&
      isProviderCode(value.code) && value.code !== "") {
    return Object.freeze({ outcome: "refused", code: value.code });
  }
  if (exactKeys(value, ["outcome", "code"]) && value.outcome === "unsupported" &&
      isProviderCode(value.code) && value.code !== "") {
    return Object.freeze({ outcome: "unsupported", code: value.code });
  }
  return Object.freeze({ outcome: "unknown", reason: "provider_outcome_unknown" });
}

function bindingMatches(a: WorkspaceManagedKeyBinding, b: WorkspaceManagedKeyBinding): boolean {
  return a.workspaceId === b.workspaceId &&
    a.externalSubjectDigest === b.externalSubjectDigest &&
    a.keyId === b.keyId &&
    a.gateway.contract === b.gateway.contract &&
    a.gateway.keyBindingId === b.gateway.keyBindingId &&
    a.gateway.fingerprint === b.gateway.fingerprint;
}

type GatewayScopeObservation =
  | Readonly<{ kind: "scope"; scope: WorkspaceModelScope }>
  | Readonly<{ kind: "missing" | "ambiguous" | "unknown" | "mismatch" }>;

function classifyScopeRead(
  result: GatewayScopeReadResult,
  binding: WorkspaceManagedKeyBinding
): GatewayScopeObservation {
  if (result.outcome === "missing") return Object.freeze({ kind: "missing" });
  if (result.outcome === "ambiguous") return Object.freeze({ kind: "ambiguous" });
  if (result.outcome === "unknown") return Object.freeze({ kind: "unknown" });
  if (!bindingMatches(result.binding, binding)) return Object.freeze({ kind: "mismatch" });
  return Object.freeze({ kind: "scope", scope: result.scope });
}

type ApplicationObservation =
  | Readonly<{
    kind: "application";
    applicationId: string;
    version: number;
    scope: WorkspaceModelScope;
  }>
  | Readonly<{ kind: "missing" | "ambiguous" | "unknown" | "mismatch" }>;

function classifyApplicationRead(
  result: ServeApplicationReadResult,
  target: WorkspaceApplicationTarget,
  binding: WorkspaceManagedKeyBinding
): ApplicationObservation {
  if (result.outcome === "missing") return Object.freeze({ kind: "missing" });
  if (result.outcome === "ambiguous") return Object.freeze({ kind: "ambiguous" });
  if (result.outcome === "unknown") return Object.freeze({ kind: "unknown" });
  if (result.application.applicationId !== target.applicationId ||
      result.application.workspaceId !== binding.workspaceId) {
    return Object.freeze({ kind: "mismatch" });
  }
  return Object.freeze({
    kind: "application",
    applicationId: result.application.applicationId,
    version: result.application.version,
    scope: result.application.scope
  });
}

async function readGatewayBinding(
  gateway: NormalizedGateway,
  binding: WorkspaceManagedKeyBinding
): Promise<GatewayBindingReadResult> {
  try {
    return normalizeBindingReadResult(await gateway.provider.readBinding({ binding }));
  } catch {
    return Object.freeze({ outcome: "unknown" });
  }
}

async function readGatewayScope(
  gateway: NormalizedGateway,
  binding: WorkspaceManagedKeyBinding
): Promise<GatewayScopeObservation> {
  try {
    return classifyScopeRead(normalizeScopeReadResult(await gateway.provider.readScope({ binding })), binding);
  } catch {
    return Object.freeze({ kind: "unknown" });
  }
}

async function readServeApplication(
  serve: NormalizedServe,
  target: WorkspaceApplicationTarget,
  binding: WorkspaceManagedKeyBinding
): Promise<ApplicationObservation> {
  try {
    return classifyApplicationRead(
      normalizeApplicationReadResult(await serve.boundary.readApplication({ target })),
      target,
      binding
    );
  } catch {
    return Object.freeze({ kind: "unknown" });
  }
}

async function invokeGatewayUpdate(
  update: (request: Readonly<{
    binding: WorkspaceManagedKeyBinding;
    scope: WorkspaceModelScope;
    idempotencyKey: string;
  }>) => Promise<GatewayScopeMutationResult>,
  prepared: PreparedRequest,
  scope: WorkspaceModelScope,
  idempotencyKey: string
): Promise<NormalizedMutation> {
  try {
    return normalizeMutationResult(await update({ binding: prepared.binding, scope, idempotencyKey }));
  } catch (error) {
    return error instanceof WorkspaceModelScopeGatewayResponseLostError
      ? Object.freeze({ outcome: "unknown", reason: "response_lost" })
      : Object.freeze({ outcome: "unknown", reason: "provider_outcome_unknown" });
  }
}

async function invokeServeApply(
  apply: (request: Readonly<{
    target: WorkspaceApplicationTarget;
    scope: WorkspaceModelScope;
    idempotencyKey: string;
  }>) => Promise<ServeApplicationApplyResult>,
  prepared: PreparedRequest,
  scope: WorkspaceModelScope
): Promise<NormalizedMutation> {
  try {
    return normalizeMutationResult(await apply({
      target: prepared.target,
      scope,
      idempotencyKey: prepared.serveIdempotencyKey
    }));
  } catch (error) {
    return error instanceof WorkspaceModelScopeServeResponseLostError
      ? Object.freeze({ outcome: "unknown", reason: "response_lost" })
      : Object.freeze({ outcome: "unknown", reason: "provider_outcome_unknown" });
  }
}

const RECORD_KEYS = Object.freeze([
  "schemaVersion", "kind", "operationId", "requestDigest", "idempotencyKey", "binding", "scopeA", "scopeB",
  "target", "appliedVersion", "state", "reason", "gatewayCode", "serveCode", "attempts",
  "gatewayMutationAttempted", "serveMutationAttempted", "bindingVerified", "gatewayReadback",
  "applicationReadback", "createdAt", "updatedAt"
]);

const GATEWAY_READBACK_KEYS = Object.freeze(["scope", "observedAt"]);
const APPLICATION_READBACK_KEYS = Object.freeze(["applicationId", "version", "scope", "observedAt"]);

/*
 * The record is closed to exactly these fields. There is no field for a raw Key,
 * a raw Secret, a raw external subject or a replacement Key identity, so such
 * material has no place to be written even by mistake.
 *
 * The state invariants tie each terminal name to the readbacks that prove it:
 * `applied` must carry scope B on both owners for the exact target, and
 * `converged_original` must carry scope A on both owners. A record cannot be
 * hand-built that claims success without the evidence.
 */
export function assertWorkspaceModelScopeRecord(value: unknown): WorkspaceModelScopeRecord {
  if (!exactKeys(value, RECORD_KEYS) || value.schemaVersion !== 1 || value.kind !== OPERATION_KIND ||
      typeof value.operationId !== "string" || !OPERATION_ID.test(value.operationId) ||
      !isDigest(value.requestDigest) || typeof value.idempotencyKey !== "string" ||
      !value.idempotencyKey.startsWith(IDEMPOTENCY_PREFIX) ||
      !Number.isSafeInteger(value.appliedVersion) || (value.appliedVersion as number) < 0 ||
      !Number.isSafeInteger(value.attempts) || (value.attempts as number) < 0 ||
      typeof value.gatewayMutationAttempted !== "boolean" ||
      typeof value.serveMutationAttempted !== "boolean" ||
      typeof value.bindingVerified !== "boolean" ||
      !isProviderCode(value.gatewayCode) || !isProviderCode(value.serveCode) ||
      !isInstant(value.createdAt) || !isInstant(value.updatedAt) ||
      Date.parse(value.updatedAt) < Date.parse(value.createdAt)) {
    throw modelScopeError("record_invalid");
  }
  let binding: WorkspaceManagedKeyBinding;
  let scopeA: WorkspaceModelScope;
  let scopeB: WorkspaceModelScope;
  let target: WorkspaceApplicationTarget;
  try {
    binding = parseBinding(value.binding, "record_invalid");
    scopeA = parseScope(value.scopeA, "record_invalid");
    scopeB = parseScope(value.scopeB, "record_invalid");
    target = parseApplicationTarget(value.target);
  } catch {
    throw modelScopeError("record_invalid");
  }
  if (!scopeSubset(scopeB, scopeA)) throw modelScopeError("record_invalid");
  if (value.requestDigest !== requestDigestOf(binding, scopeA, scopeB, target) ||
      value.idempotencyKey !== idempotencyKeyOf(value.operationId, value.requestDigest)) {
    throw modelScopeError("record_invalid");
  }
  if (!WORKSPACE_MODEL_SCOPE_STATES.includes(value.state as WorkspaceModelScopeState)) {
    throw modelScopeError("record_invalid");
  }
  if (typeof value.reason !== "string" ||
      !WORKSPACE_MODEL_SCOPE_REASONS[value.state as WorkspaceModelScopeState].includes(value.reason)) {
    throw modelScopeError("record_invalid");
  }
  if (!exactKeys(value.gatewayReadback, GATEWAY_READBACK_KEYS)) throw modelScopeError("record_invalid");
  const gatewayReadback = value.gatewayReadback;
  let gatewayScope: WorkspaceModelScope | null = null;
  if (gatewayReadback.scope !== null) {
    try {
      gatewayScope = parseScope(gatewayReadback.scope, "record_invalid");
    } catch {
      throw modelScopeError("record_invalid");
    }
  }
  if ((gatewayScope === null && gatewayReadback.observedAt !== "") ||
      (gatewayScope !== null && !isInstant(gatewayReadback.observedAt))) {
    throw modelScopeError("record_invalid");
  }
  if (!exactKeys(value.applicationReadback, APPLICATION_READBACK_KEYS)) throw modelScopeError("record_invalid");
  const applicationReadback = value.applicationReadback;
  let applicationScope: WorkspaceModelScope | null = null;
  if (applicationReadback.scope !== null) {
    try {
      applicationScope = parseScope(applicationReadback.scope, "record_invalid");
    } catch {
      throw modelScopeError("record_invalid");
    }
  }
  if (typeof applicationReadback.applicationId !== "string" ||
      !(applicationReadback.applicationId === "" || APPLICATION_ID.test(applicationReadback.applicationId)) ||
      !Number.isSafeInteger(applicationReadback.version) || (applicationReadback.version as number) < 0 ||
      applicationReadback.applicationId !== "" && (applicationReadback.version as number) <= 0 ||
      (applicationScope === null && (applicationReadback.observedAt !== "" || applicationReadback.applicationId !== "" ||
        applicationReadback.version !== 0)) ||
      (applicationScope !== null && !isInstant(applicationReadback.observedAt))) {
    throw modelScopeError("record_invalid");
  }
  if (value.serveMutationAttempted && !value.gatewayMutationAttempted) throw modelScopeError("record_invalid");
  if (value.gatewayMutationAttempted && !value.bindingVerified) throw modelScopeError("record_invalid");
  if (value.state === "gateway_confirmed" &&
      (gatewayScope === null || !sameScope(gatewayScope, scopeB))) {
    throw modelScopeError("record_invalid");
  }
  if (value.state === "applied") {
    if (!value.bindingVerified || gatewayScope === null || applicationScope === null ||
        !sameScope(gatewayScope, scopeB) || !sameScope(applicationScope, scopeB) ||
        applicationReadback.applicationId !== target.applicationId ||
        applicationReadback.version !== target.version) {
      throw modelScopeError("record_invalid");
    }
  }
  if (value.state === "converged_original") {
    if (!value.bindingVerified || gatewayScope === null || applicationScope === null ||
        !sameScope(gatewayScope, scopeA) || !sameScope(applicationScope, scopeA) ||
        applicationReadback.applicationId !== target.applicationId) {
      throw modelScopeError("record_invalid");
    }
  }
  return value as unknown as WorkspaceModelScopeRecord;
}

const nowInstant = (): string => new Date().toISOString();
const emptyGatewayReadback = (): GatewayReadback => Object.freeze({ scope: null, observedAt: "" });
const emptyApplicationReadback = (): ApplicationReadback =>
  Object.freeze({ applicationId: "", version: 0, scope: null, observedAt: "" });

function initialRecord(request: PreparedRequest): WorkspaceModelScopeRecord {
  const now = nowInstant();
  return Object.freeze({
    schemaVersion: 1,
    kind: OPERATION_KIND,
    operationId: request.operationId,
    requestDigest: request.requestDigest,
    idempotencyKey: request.idempotencyKey,
    binding: request.binding,
    scopeA: request.scopeA,
    scopeB: request.scopeB,
    target: request.target,
    appliedVersion: 0,
    state: "requested",
    reason: "created",
    gatewayCode: "",
    serveCode: "",
    attempts: 0,
    gatewayMutationAttempted: false,
    serveMutationAttempted: false,
    bindingVerified: false,
    gatewayReadback: emptyGatewayReadback(),
    applicationReadback: emptyApplicationReadback(),
    createdAt: now,
    updatedAt: now
  });
}

type RecordPatch = Partial<Omit<WorkspaceModelScopeRecord,
  "schemaVersion" | "kind" | "operationId" | "requestDigest" | "idempotencyKey" | "binding" |
  "scopeA" | "scopeB" | "target" | "appliedVersion" | "createdAt" | "updatedAt">>;

type TransitionOutcome =
  | Readonly<{ kind: "ok"; record: WorkspaceModelScopeRecord }>
  | Readonly<{ kind: "stale" }>
  | Readonly<{ kind: "error" }>;

type Run = {
  prepared: PreparedRequest;
  store: WorkspaceModelScopeStore;
  replayed: boolean;
  mutated: boolean;
};

async function transition(
  run: Run,
  current: WorkspaceModelScopeRecord,
  patch: RecordPatch
): Promise<TransitionOutcome> {
  const next: WorkspaceModelScopeRecord = Object.freeze({
    ...current,
    ...patch,
    appliedVersion: current.appliedVersion + 1,
    updatedAt: nowInstant()
  });
  try {
    return await run.store.compareAndSwap(current.operationId, current.appliedVersion, next)
      ? Object.freeze({ kind: "ok", record: next })
      : Object.freeze({ kind: "stale" });
  } catch {
    return Object.freeze({ kind: "error" });
  }
}

function resultOf(
  run: Run,
  record: WorkspaceModelScopeRecord | null,
  state: WorkspaceModelScopeState,
  reason: string
): WorkspaceModelScopeResult {
  return Object.freeze({
    operationId: run.prepared.operationId,
    idempotencyKey: run.prepared.idempotencyKey,
    state,
    reason,
    success: state === "applied",
    appliedVersion: record ? record.appliedVersion : 0,
    mutated: run.mutated,
    replayed: run.replayed,
    gatewayMutationAttempted: record ? record.gatewayMutationAttempted : false,
    serveMutationAttempted: record ? record.serveMutationAttempted : false,
    record
  });
}

function failedTransition(
  run: Run,
  record: WorkspaceModelScopeRecord,
  kind: "stale" | "error"
): WorkspaceModelScopeResult {
  // A transition that fails after a dispatch fence cannot prove what the
  // provider did, so it is recovery-required. Before any dispatch nothing was
  // mutated, and it is still recovery-required because the declared operation
  // has not reached a confirmed terminal state.
  return resultOf(run, record, "recovery-required", kind === "stale" ? "stale_cas" : "store_unavailable");
}

async function transitionTo(
  run: Run,
  record: WorkspaceModelScopeRecord,
  patch: RecordPatch
): Promise<WorkspaceModelScopeResult> {
  const outcome = await transition(run, record, patch);
  if (outcome.kind !== "ok") return failedTransition(run, record, outcome.kind);
  return resultOf(run, outcome.record, outcome.record.state, outcome.record.reason);
}

function ownershipReason(observation: Readonly<{ kind: string }>): string {
  if (observation.kind === "unknown") return "readback_unconfirmed";
  if (observation.kind === "ambiguous") return "ownership_ambiguous";
  if (observation.kind === "missing") return "ownership_unverified";
  return "ownership_mismatch";
}

/*
 * Restore the original approved scope when the forward effect cannot be
 * completed and Serve still holds A.
 *
 * This is deliberately not called "success": a caller reads `converged_original`
 * and knows the approved change did not happen. The function only returns that
 * state after the Gateway readback shows A and the Serve readback shows A for the
 * exact target. Any unconfirmed restore is recovery-required.
 */
async function convergeOriginal(
  run: Run,
  gateway: NormalizedGateway,
  serve: NormalizedServe,
  record: WorkspaceModelScopeRecord,
  options: Readonly<{
    reason: string;
    gatewayCode?: string;
    serveCode?: string;
  }>
): Promise<WorkspaceModelScopeResult> {
  const prepared = run.prepared;
  const application = await readServeApplication(serve, prepared.target, prepared.binding);
  if (application.kind !== "application") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: application.kind === "unknown" ? "readback_unconfirmed" : "application_readback_mismatch",
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      applicationReadback: record.applicationReadback
    });
  }
  if (!sameScope(application.scope, prepared.scopeA)) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "both_readbacks_disagree",
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      applicationReadback: applicationReadbackOf(application)
    });
  }
  const gatewayRead = await readGatewayScope(gateway, prepared.binding);
  if (gatewayRead.kind !== "scope") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: ownershipReason(gatewayRead),
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      applicationReadback: applicationReadbackOf(application)
    });
  }
  if (sameScope(gatewayRead.scope, prepared.scopeA)) {
    return transitionTo(run, record, {
      state: "converged_original",
      reason: options.reason,
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      bindingVerified: true,
      gatewayReadback: scopeReadbackOf(gatewayRead.scope),
      applicationReadback: applicationReadbackOf(application)
    });
  }
  if (!sameScope(gatewayRead.scope, prepared.scopeB)) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "gateway_scope_unexpected",
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      gatewayReadback: scopeReadbackOf(gatewayRead.scope),
      applicationReadback: applicationReadbackOf(application)
    });
  }
  const restore = gatewayUpdateFor(gateway);
  if (!restore) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "capability_unsupported",
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      gatewayReadback: scopeReadbackOf(gatewayRead.scope),
      applicationReadback: applicationReadbackOf(application)
    });
  }
  run.mutated = true;
  const mutation = await invokeGatewayUpdate(restore, prepared, prepared.scopeA, prepared.restoreIdempotencyKey);
  if (mutation.outcome === "unknown") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: mutation.reason,
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      gatewayReadback: scopeReadbackOf(gatewayRead.scope),
      applicationReadback: applicationReadbackOf(application)
    });
  }
  const after = await readGatewayScope(gateway, prepared.binding);
  if (after.kind !== "scope" || !sameScope(after.scope, prepared.scopeA)) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: mutation.outcome === "refused" || mutation.outcome === "unsupported"
        ? "restore_refused"
        : "restore_unconfirmed",
      gatewayCode: mutation.outcome === "refused" || mutation.outcome === "unsupported"
        ? mutation.code
        : options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      gatewayReadback: after.kind === "scope" ? scopeReadbackOf(after.scope) : record.gatewayReadback,
      applicationReadback: applicationReadbackOf(application)
    });
  }
  const applicationAfter = await readServeApplication(serve, prepared.target, prepared.binding);
  if (applicationAfter.kind !== "application" || !sameScope(applicationAfter.scope, prepared.scopeA)) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "both_readbacks_disagree",
      gatewayCode: options.gatewayCode ?? "",
      serveCode: options.serveCode ?? "",
      gatewayReadback: scopeReadbackOf(after.scope),
      applicationReadback: applicationAfter.kind === "application"
        ? applicationReadbackOf(applicationAfter)
        : record.applicationReadback
    });
  }
  return transitionTo(run, record, {
    state: "converged_original",
    reason: options.reason,
    gatewayCode: options.gatewayCode ?? "",
    serveCode: options.serveCode ?? "",
    bindingVerified: true,
    gatewayReadback: scopeReadbackOf(after.scope),
    applicationReadback: applicationReadbackOf(applicationAfter)
  });
}

/*
 * Confirm success only from two fresh owner readbacks: the exact bound Key now
 * carries B and the exact target application/version carries B. Either side
 * alone is not enough.
 */
async function confirmApplied(
  run: Run,
  gateway: NormalizedGateway,
  record: WorkspaceModelScopeRecord,
  application: Extract<ApplicationObservation, { kind: "application" }>
): Promise<WorkspaceModelScopeResult> {
  const prepared = run.prepared;
  const gatewayRead = await readGatewayScope(gateway, prepared.binding);
  if (gatewayRead.kind !== "scope") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: ownershipReason(gatewayRead),
      applicationReadback: applicationReadbackOf(application)
    });
  }
  if (!sameScope(gatewayRead.scope, prepared.scopeB)) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "both_readbacks_disagree",
      gatewayReadback: scopeReadbackOf(gatewayRead.scope),
      applicationReadback: applicationReadbackOf(application)
    });
  }
  return transitionTo(run, record, {
    state: "applied",
    reason: "confirmed",
    gatewayCode: "",
    serveCode: "",
    bindingVerified: true,
    gatewayReadback: scopeReadbackOf(gatewayRead.scope),
    applicationReadback: applicationReadbackOf(application)
  });
}

/*
 * Apply B through Serve after Gateway has confirmed B on the same Key.
 *
 * If Serve remains at A, the operation restores the Gateway scope to A and
 * reports `converged_original`; it never reports `applied`. If Serve reports
 * another scope or version, or either owner cannot be read, the operation is
 * recovery-required.
 */
async function applyThroughServe(
  run: Run,
  gateway: NormalizedGateway,
  serve: NormalizedServe,
  record: WorkspaceModelScopeRecord
): Promise<WorkspaceModelScopeResult> {
  const prepared = run.prepared;
  const current = await readServeApplication(serve, prepared.target, prepared.binding);
  if (current.kind !== "application") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: current.kind === "unknown" ? "readback_unconfirmed" : "application_readback_mismatch",
      applicationReadback: record.applicationReadback
    });
  }
  if (sameScope(current.scope, prepared.scopeB)) {
    if (current.version !== prepared.target.version) {
      return transitionTo(run, record, {
        state: "recovery-required",
        reason: "application_version_mismatch",
        applicationReadback: applicationReadbackOf(current)
      });
    }
    return confirmApplied(run, gateway, record, current);
  }
  if (!sameScope(current.scope, prepared.scopeA)) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "application_readback_mismatch",
      applicationReadback: applicationReadbackOf(current)
    });
  }
  const apply = serveApplyFor(serve);
  if (!apply) {
    return convergeOriginal(run, gateway, serve, record, {
      reason: "application_capability_unavailable",
      serveCode: "Unimplemented"
    });
  }
  // Persist the Serve dispatch fence before calling it. A replay that reads this
  // record knows B may already be applied and reads Serve back instead of
  // assuming otherwise.
  const dispatch = await transition(run, record, {
    state: "gateway_confirmed",
    reason: "applying",
    attempts: record.attempts + 1,
    serveMutationAttempted: true,
    bindingVerified: true,
    applicationReadback: applicationReadbackOf(current)
  });
  if (dispatch.kind !== "ok") return failedTransition(run, record, dispatch.kind);
  record = dispatch.record;
  run.mutated = true;
  const mutation = await invokeServeApply(apply, prepared, prepared.scopeB);
  if (mutation.outcome === "unknown") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: mutation.reason,
      applicationReadback: applicationReadbackOf(current)
    });
  }
  if (mutation.outcome === "refused" || mutation.outcome === "unsupported") {
    return convergeOriginal(run, gateway, serve, record, {
      reason: mutation.outcome === "refused" ? "apply_refused" : "application_capability_unavailable",
      serveCode: mutation.code
    });
  }
  const after = await readServeApplication(serve, prepared.target, prepared.binding);
  if (after.kind !== "application") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: after.kind === "unknown" ? "readback_unconfirmed" : "application_readback_mismatch",
      applicationReadback: record.applicationReadback
    });
  }
  if (!sameScope(after.scope, prepared.scopeB)) {
    if (sameScope(after.scope, prepared.scopeA)) {
      return convergeOriginal(run, gateway, serve, record, {
        reason: "apply_unconfirmed"
      });
    }
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "application_readback_mismatch",
      applicationReadback: applicationReadbackOf(after)
    });
  }
  if (after.version !== prepared.target.version) {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "application_version_mismatch",
      applicationReadback: applicationReadbackOf(after)
    });
  }
  return confirmApplied(run, gateway, record, after);
}

/*
 * Run one same-Key model-scope operation.
 *
 * The operation is a value, not an exception: an owner that cannot prove its
 * effect yields a `recovery-required` record, and a completed change that could
 * not be kept yields `converged_original`. Only a malformed request, a scope
 * expansion, an unchanged scope or broken dependency wiring throws, because
 * those are caller/programming errors rather than outcomes.
 */
export async function runWorkspaceModelScope(
  request: WorkspaceModelScopeRequest,
  dependencies: Readonly<{
    gateway: GatewayModelScopeProvider;
    serve: ServeModelScopeBoundary;
    store: WorkspaceModelScopeStore;
  }>
): Promise<WorkspaceModelScopeResult> {
  const prepared = prepareRequest(request);
  const gateway = assertGatewayProvider(dependencies?.gateway);
  const serve = assertServeBoundary(dependencies?.serve);
  const store = assertStore(dependencies?.store);
  const run: Run = { prepared, store, replayed: false, mutated: false };

  let existing: WorkspaceModelScopeRecord | null;
  try {
    existing = await store.read(prepared.operationId);
  } catch {
    return resultOf(run, null, "recovery-required", "store_unavailable");
  }

  let record: WorkspaceModelScopeRecord;
  if (existing) {
    // Every result below replays an existing record, including a conflict, a
    // stale expected version and a terminal record.
    run.replayed = true;
    if (existing.requestDigest !== prepared.requestDigest ||
        existing.idempotencyKey !== prepared.idempotencyKey) {
      // The same operation id under another binding, scope or target is not a
      // replay. Nothing is contacted and the original record is left untouched.
      return resultOf(run, existing, "recovery-required", "idempotency_conflict");
    }
    if (prepared.expectedAppliedVersion !== undefined &&
        existing.appliedVersion !== prepared.expectedAppliedVersion) {
      return resultOf(run, existing, "recovery-required", "stale_applied_version");
    }
    if (existing.state === "applied" || existing.state === "converged_original") {
      return resultOf(run, existing, existing.state, existing.reason);
    }
    // `requested`, `gateway_confirmed` and `recovery-required` describe an
    // operation whose effects may still be in flight. A replay resumes it.
    record = existing;
  } else {
    if (prepared.expectedAppliedVersion !== undefined && prepared.expectedAppliedVersion !== 0) {
      return resultOf(run, null, "recovery-required", "stale_applied_version");
    }
    record = initialRecord(prepared);
    try {
      await store.create(record);
    } catch (error) {
      const reason = error instanceof WorkspaceModelScopeStoreConflictError
        ? "stale_applied_version"
        : "store_unavailable";
      return resultOf(run, null, "recovery-required", reason);
    }
  }

  // Ownership first: the exact Gateway binding has to be confirmed before the
  // Key's scope is even read, and both have to be confirmed before a mutation.
  const bindingRead = await readGatewayBinding(gateway, prepared.binding);
  const bindingVerified = bindingRead.outcome === "binding" &&
    bindingMatches(bindingRead.binding, prepared.binding);
  if (!bindingVerified) {
    const reason = bindingRead.outcome === "unknown" ? "readback_unconfirmed"
      : bindingRead.outcome === "ambiguous" ? "ownership_ambiguous"
      : bindingRead.outcome === "missing" ? "ownership_unverified"
      : "ownership_mismatch";
    return transitionTo(run, record, {
      state: "recovery-required",
      reason,
      gatewayCode: "",
      serveCode: "",
      bindingVerified: record.bindingVerified,
      gatewayReadback: record.gatewayReadback
    });
  }

  const gatewayRead = await readGatewayScope(gateway, prepared.binding);
  if (gatewayRead.kind !== "scope") {
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: ownershipReason(gatewayRead),
      gatewayCode: "",
      serveCode: "",
      bindingVerified: true,
      gatewayReadback: record.gatewayReadback
    });
  }

  if (sameScope(gatewayRead.scope, prepared.scopeB)) {
    // Gateway already carries B. This is a resume, or an effect that landed
    // before a lost answer; the state still requires a Serve readback to settle.
    if (record.state !== "gateway_confirmed") {
      const confirmed = await transitionTo(run, record, {
        state: "gateway_confirmed",
        reason: "confirmed",
        gatewayCode: "",
        serveCode: "",
        bindingVerified: true,
        gatewayReadback: scopeReadbackOf(gatewayRead.scope)
      });
      if (confirmed.state !== "gateway_confirmed") return confirmed;
      record = confirmed.record as WorkspaceModelScopeRecord;
    }
  } else if (sameScope(gatewayRead.scope, prepared.scopeA)) {
    const update = gatewayUpdateFor(gateway);
    if (!update) {
      // The approved same-Key scope contract is not available. If Serve still
      // holds A, report convergence to the original scope; never invent success.
      return convergeOriginal(run, gateway, serve, record, {
        reason: "capability_unsupported",
        gatewayCode: "Unimplemented"
      });
    }
    // Persist the Gateway dispatch fence before calling it. If this CAS loses,
    // nothing was mutated; if the process dies after this write, a replay knows
    // an effect may exist and reads the scope back before dispatching again.
    const dispatch = await transition(run, record, {
      state: "requested",
      reason: "dispatching_scope",
      attempts: record.attempts + 1,
      gatewayMutationAttempted: true,
      bindingVerified: true,
      gatewayReadback: scopeReadbackOf(gatewayRead.scope)
    });
    if (dispatch.kind !== "ok") return failedTransition(run, record, dispatch.kind);
    record = dispatch.record;
    run.mutated = true;
    const mutation = await invokeGatewayUpdate(update, prepared, prepared.scopeB, prepared.idempotencyKey);
    if (mutation.outcome === "unknown") {
      return transitionTo(run, record, {
        state: "recovery-required",
        reason: mutation.reason,
        gatewayCode: "",
        serveCode: "",
        gatewayReadback: scopeReadbackOf(gatewayRead.scope)
      });
    }
    if (mutation.outcome === "refused" || mutation.outcome === "unsupported") {
      return convergeOriginal(run, gateway, serve, record, {
        reason: mutation.outcome === "refused" ? "gateway_refused" : "capability_unsupported",
        gatewayCode: mutation.code
      });
    }
    // A confirmed mutation is still only a provider assertion. The scope is
    // confirmed only by a fresh authoritative readback of the exact Key.
    const after = await readGatewayScope(gateway, prepared.binding);
    if (after.kind === "scope" && sameScope(after.scope, prepared.scopeB)) {
      const confirmed = await transitionTo(run, record, {
        state: "gateway_confirmed",
        reason: "confirmed",
        gatewayCode: "",
        serveCode: "",
        bindingVerified: true,
        gatewayReadback: scopeReadbackOf(after.scope)
      });
      if (confirmed.state !== "gateway_confirmed") return confirmed;
      record = confirmed.record as WorkspaceModelScopeRecord;
    } else if (after.kind === "scope" && sameScope(after.scope, prepared.scopeA)) {
      return convergeOriginal(run, gateway, serve, record, {
        reason: "gateway_scope_unconfirmed"
      });
    } else {
      return transitionTo(run, record, {
        state: "recovery-required",
        reason: after.kind === "unknown" || after.kind === "missing" || after.kind === "ambiguous"
          ? "readback_unconfirmed"
          : after.kind === "mismatch" ? "readback_identity_mismatch" : "gateway_scope_unexpected",
        gatewayCode: "",
        serveCode: "",
        bindingVerified: true,
        gatewayReadback: after.kind === "scope" ? scopeReadbackOf(after.scope) : record.gatewayReadback
      });
    }
  } else {
    // A scope that is neither the approved original nor the target is external
    // drift. Settling either way would pretend to know which owner is right.
    return transitionTo(run, record, {
      state: "recovery-required",
      reason: "gateway_scope_unexpected",
      gatewayCode: "",
      serveCode: "",
      bindingVerified: true,
      gatewayReadback: scopeReadbackOf(gatewayRead.scope)
    });
  }

  return applyThroughServe(run, gateway, serve, record);
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
export async function createFileWorkspaceModelScopeStore(
  directory: string
): Promise<WorkspaceModelScopeStore> {
  const root = String(directory ?? "");
  if (!root || !isAbsolute(root)) throw modelScopeError("store_invalid");
  await mkdir(root, { recursive: true });
  const pathFor = (operationId: string): string => {
    if (typeof operationId !== "string" || !OPERATION_ID.test(operationId)) {
      throw modelScopeError("operation_invalid");
    }
    return join(root, `${operationId}.json`);
  };
  const readRecord = async (operationId: string): Promise<WorkspaceModelScopeRecord | null> => {
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
      throw modelScopeError("record_invalid");
    }
    return assertWorkspaceModelScopeRecord(parsed);
  };
  return Object.freeze({
    async read(operationId: string): Promise<WorkspaceModelScopeRecord | null> {
      return readRecord(operationId);
    },
    async create(record: WorkspaceModelScopeRecord): Promise<void> {
      const next = assertWorkspaceModelScopeRecord(record);
      try {
        await writeFile(pathFor(next.operationId), `${JSON.stringify(next, null, 2)}\n`, {
          flag: "wx",
          mode: 0o600
        });
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code === "EEXIST") {
          throw new WorkspaceModelScopeStoreConflictError();
        }
        throw error;
      }
    },
    async compareAndSwap(
      operationId: string,
      expectedAppliedVersion: number,
      nextRecord: WorkspaceModelScopeRecord
    ): Promise<boolean> {
      const next = assertWorkspaceModelScopeRecord(nextRecord);
      if (next.operationId !== operationId || next.appliedVersion !== expectedAppliedVersion + 1) {
        throw modelScopeError("record_invalid");
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
