import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  GATEWAY_MODEL_SCOPE_OPERATIONS,
  REQUIRED_GATEWAY_INTEGRATION_CONTRACT,
  REQUIRED_SERVE_APPLICATION_CONTRACT,
  WorkspaceModelScopeGatewayResponseLostError,
  WorkspaceModelScopeServeResponseLostError,
  WorkspaceModelScopeStoreConflictError,
  createFileWorkspaceModelScopeStore,
  externalSubjectDigest,
  runWorkspaceModelScope,
  workspaceModelScope,
  workspaceModelScopeIdempotencyKey,
  workspaceModelScopeServeIdempotencyKey
} from "../../tools/workspace-model-scope.ts";
import type {
  GatewayModelScopeProvider,
  GatewayModelScopeOperation,
  GatewayScopeMutationResult,
  ServeApplicationApplyResult,
  ServeModelScopeBoundary,
  ServeModelScopeOperation,
  WorkspaceApplicationTarget,
  WorkspaceManagedKeyBinding,
  WorkspaceModelScope,
  WorkspaceModelScopeStore
} from "../../tools/workspace-model-scope.ts";

// A raw external subject, a raw key value and a raw Secret value exist only in
// this test's memory. The seam accepts the subject digest and the external key
// identity and persists neither raw value.
const rawSubject = "sub2api-user-41";
const rawKey = "sk-live-raw-key-12345";
const rawSecret = "service-control-credential";
const subjectDigest = externalSubjectDigest(rawSubject);

const exactBinding: WorkspaceManagedKeyBinding = Object.freeze({
  workspaceId: "ws-original",
  externalSubjectDigest: subjectDigest,
  keyId: "4101",
  gateway: Object.freeze({
    contract: REQUIRED_GATEWAY_INTEGRATION_CONTRACT.bindingSchema,
    keyBindingId: `gateway-key-${"a".repeat(32)}`,
    fingerprint: `sha256:${"b".repeat(64)}`
  })
});

const scopeA = workspaceModelScope({
  models: ["model-a", "model-b"],
  groups: ["group-a"]
});
const scopeB = workspaceModelScope({
  models: ["model-a"],
  groups: ["group-a"]
});
const target: WorkspaceApplicationTarget = Object.freeze({ applicationId: "app-main", version: 7 });

const gatewayOperations: readonly GatewayModelScopeOperation[] = Object.freeze([
  "readBinding", "readScope", "updateScope"
]);
const serveOperations: readonly ServeModelScopeOperation[] = Object.freeze(["readApplication", "apply"]);

type Call = Readonly<{ operation: string; request: any }>;
type MutationDecision =
  | "applied"
  | "applied-wrong-version"
  | "confirmed-not-applied"
  | "unknown"
  | "refused"
  | "unsupported"
  | "response-lost-applied"
  | "response-lost-not-applied"
  | "throw"
  | Readonly<{ outcome: "confirmed" }>
  | Readonly<{ outcome: "unknown" }>
  | Readonly<{ outcome: "refused"; code: string }>
  | Readonly<{ outcome: "unsupported"; code: string }>;
type MutationResolver = (context: Readonly<{ call: number; request: any; scope: WorkspaceModelScope }>) => MutationDecision;

function resolveConfigured<T>(configured: unknown, context: Record<string, unknown>): T | undefined {
  if (typeof configured === "function") return (configured as (value: Record<string, unknown>) => T)(context);
  return configured as T | undefined;
}

function gatewayHarness(options: Readonly<{
  scope?: WorkspaceModelScope;
  capabilities?: readonly GatewayModelScopeOperation[];
  bindingRead?: unknown;
  scopeRead?: unknown;
  update?: MutationDecision | MutationResolver;
}> = {}) {
  const calls: Call[] = [];
  const capabilities = options.capabilities ?? gatewayOperations;
  let scope = options.scope ?? scopeA;
  let bindingReads = 0;
  let scopeReads = 0;
  let updates = 0;

  const provider: GatewayModelScopeProvider = Object.freeze({
    contract: REQUIRED_GATEWAY_INTEGRATION_CONTRACT.modelScopeSchema,
    capabilities,
    async readBinding(request: any) {
      calls.push({ operation: "readBinding", request });
      bindingReads += 1;
      const configured = resolveConfigured<any>(options.bindingRead, { call: bindingReads, request });
      return configured ?? { outcome: "binding", binding: exactBinding };
    },
    async readScope(request: any) {
      calls.push({ operation: "readScope", request });
      scopeReads += 1;
      const configured = resolveConfigured<any>(options.scopeRead, { call: scopeReads, scope, request });
      return configured ?? { outcome: "scope", binding: exactBinding, scope };
    },
    async updateScope(request: any): Promise<GatewayScopeMutationResult> {
      calls.push({ operation: "updateScope", request });
      updates += 1;
      const configured = resolveConfigured<MutationDecision>(options.update, { call: updates, request, scope });
      const decision = configured ?? "applied";
      if (decision === "applied" || decision === "applied-wrong-version") {
        // The gateway boundary has no application version; either decision is a
        // Key-scope confirmation.
        scope = request.scope;
        return { outcome: "confirmed" };
      }
      if (decision === "confirmed-not-applied") return { outcome: "confirmed" };
      if (decision === "unknown") return { outcome: "unknown" };
      if (decision === "refused") return { outcome: "refused", code: "FailedPrecondition" };
      if (decision === "unsupported") return { outcome: "unsupported", code: "Unimplemented" };
      if (decision === "response-lost-applied") {
        scope = request.scope;
        throw new WorkspaceModelScopeGatewayResponseLostError();
      }
      if (decision === "response-lost-not-applied") throw new WorkspaceModelScopeGatewayResponseLostError();
      if (decision === "throw") throw new Error("gateway transport failed");
      return decision;
    }
  });

  return {
    provider,
    calls,
    get scope() {
      return scope;
    },
    get updateCount() {
      return updates;
    }
  };
}

function serveHarness(options: Readonly<{
  scope?: WorkspaceModelScope;
  version?: number;
  capabilities?: readonly ServeModelScopeOperation[];
  read?: unknown;
  apply?: MutationDecision | MutationResolver;
}> = {}) {
  const calls: Call[] = [];
  const capabilities = options.capabilities ?? serveOperations;
  let scope = options.scope ?? scopeA;
  let version = options.version ?? 6;
  let reads = 0;
  let applies = 0;

  const boundary: ServeModelScopeBoundary = Object.freeze({
    contract: REQUIRED_SERVE_APPLICATION_CONTRACT.schema,
    capabilities,
    async readApplication(request: any) {
      calls.push({ operation: "readApplication", request });
      reads += 1;
      const configured = resolveConfigured<any>(options.read, { call: reads, request, scope, version });
      return configured ?? {
        outcome: "application",
        application: {
          applicationId: target.applicationId,
          workspaceId: exactBinding.workspaceId,
          version,
          scope
        }
      };
    },
    async apply(request: any): Promise<ServeApplicationApplyResult> {
      calls.push({ operation: "apply", request });
      applies += 1;
      const configured = resolveConfigured<MutationDecision>(options.apply, {
        call: applies,
        request,
        scope
      });
      const decision = configured ?? "applied";
      if (decision === "applied") {
        scope = request.scope;
        version = request.target.version;
        return { outcome: "confirmed" };
      }
      if (decision === "applied-wrong-version") {
        scope = request.scope;
        version = request.target.version + 1;
        return { outcome: "confirmed" };
      }
      if (decision === "confirmed-not-applied") return { outcome: "confirmed" };
      if (decision === "unknown") return { outcome: "unknown" };
      if (decision === "refused") return { outcome: "refused", code: "FailedPrecondition" };
      if (decision === "unsupported") return { outcome: "unsupported", code: "Unimplemented" };
      if (decision === "response-lost-applied") {
        scope = request.scope;
        version = request.target.version;
        throw new WorkspaceModelScopeServeResponseLostError();
      }
      if (decision === "response-lost-not-applied") throw new WorkspaceModelScopeServeResponseLostError();
      if (decision === "throw") throw new Error("serve transport failed");
      return decision;
    }
  });

  return {
    boundary,
    calls,
    get scope() {
      return scope;
    },
    get version() {
      return version;
    },
    get applyCount() {
      return applies;
    }
  };
}

function memoryStore(
  initial: readonly any[] = []
): WorkspaceModelScopeStore & { records: Map<string, any> } {
  const records = new Map<string, any>(
    initial.map((record) => [record.operationId, structuredClone(record)])
  );
  return {
    records,
    async read(operationId: string) {
      const record = records.get(operationId);
      return record ? structuredClone(record) : null;
    },
    async create(record: any) {
      if (records.has(record.operationId)) throw new WorkspaceModelScopeStoreConflictError();
      records.set(record.operationId, structuredClone(record));
    },
    async compareAndSwap(operationId: string, expectedAppliedVersion: number, next: any) {
      const current = records.get(operationId);
      if (!current || current.appliedVersion !== expectedAppliedVersion) return false;
      records.set(operationId, structuredClone(next));
      return true;
    }
  };
}

const request = (overrides: Record<string, unknown> = {}): any => ({
  operationId: "workspace-model-scope-op-1",
  binding: exactBinding,
  scopeA,
  scopeB,
  target,
  ...overrides
});

const updateCalls = (calls: readonly Call[]) => calls.filter((call) => call.operation === "updateScope");
const applyCalls = (calls: readonly Call[]) => calls.filter((call) => call.operation === "apply");

test("changes only the exact bound Key to the approved narrower scope and settles on both readbacks", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness();
  const store = memoryStore();
  const original = request();

  const result = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });

  assert.equal(result.state, "applied");
  assert.equal(result.reason, "confirmed");
  assert.equal(result.success, true);
  assert.equal(result.mutated, true);
  assert.equal(result.replayed, false);
  assert.equal(result.record?.state, "applied");
  assert.equal(result.record?.binding.keyId, exactBinding.keyId);
  assert.deepEqual(result.record?.scopeA, scopeA);
  assert.deepEqual(result.record?.scopeB, scopeB);
  assert.deepEqual(result.record?.gatewayReadback.scope, scopeB);
  assert.deepEqual(result.record?.applicationReadback.scope, scopeB);
  assert.equal(result.record?.applicationReadback.applicationId, target.applicationId);
  assert.equal(result.record?.applicationReadback.version, target.version);

  assert.deepEqual(gateway.scope, scopeB);
  assert.deepEqual(serve.scope, scopeB);
  assert.equal(serve.version, target.version);

  const updates = updateCalls(gateway.calls);
  assert.equal(updates.length, 1);
  assert.deepEqual(updates[0].request.binding, exactBinding);
  assert.deepEqual(updates[0].request.scope, scopeB);
  assert.equal(updates[0].request.idempotencyKey, workspaceModelScopeIdempotencyKey(original));
  assert.equal(updates[0].request.binding.keyId, exactBinding.keyId);
  assert.equal(applyCalls(serve.calls).length, 1);
  assert.deepEqual(applyCalls(serve.calls)[0].request.scope, scopeB);
  assert.equal(applyCalls(serve.calls)[0].request.idempotencyKey, workspaceModelScopeServeIdempotencyKey(original));
  assert.equal(gateway.calls.every((call) => call.request.binding.keyId === exactBinding.keyId), true);
});

test("refuses an authorization expansion or an unchanged scope before contacting any owner", async () => {
  const expandedModel = workspaceModelScope({
    models: ["model-a", "model-c"],
    groups: ["group-a"]
  });
  const expandedGroup = workspaceModelScope({
    models: ["model-a"],
    groups: ["group-a", "group-b"]
  });
  const unchanged = workspaceModelScope({ models: ["model-a", "model-b"], groups: ["group-a"] });

  for (const [scopeB, code] of [
    [expandedModel, /scope_expansion_refused/],
    [expandedGroup, /scope_expansion_refused/],
    [unchanged, /scope_unchanged/]
  ] as const) {
    const gateway = gatewayHarness();
    const serve = serveHarness();
    const store = memoryStore();
    await assert.rejects(
      runWorkspaceModelScope(request({ scopeB }), {
        gateway: gateway.provider,
        serve: serve.boundary,
        store
      }),
      code
    );
    assert.equal(gateway.calls.length, 0);
    assert.equal(serve.calls.length, 0);
    assert.equal(store.records.size, 0);
  }
});

test("a Serve refusal while A remains real restores the original approved scope on the same Key", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness({ apply: "refused" });
  const store = memoryStore();

  const result = await runWorkspaceModelScope(request(), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });

  assert.equal(result.state, "converged_original");
  assert.equal(result.reason, "apply_refused");
  assert.equal(result.success, false);
  assert.equal(result.mutated, true);
  assert.deepEqual(gateway.scope, scopeA);
  assert.deepEqual(serve.scope, scopeA);
  assert.deepEqual(result.record?.gatewayReadback.scope, scopeA);
  assert.deepEqual(result.record?.applicationReadback.scope, scopeA);
  assert.equal(result.record?.binding.keyId, exactBinding.keyId);

  const updates = updateCalls(gateway.calls);
  assert.equal(updates.length, 2);
  assert.deepEqual(updates[0].request.scope, scopeB);
  assert.deepEqual(updates[1].request.scope, scopeA);
  assert.equal(updates[0].request.idempotencyKey, workspaceModelScopeIdempotencyKey(request()));
  assert.equal(updates[1].request.idempotencyKey.endsWith(":restore"), true);
  assert.equal(updates.every((call) => call.request.binding.keyId === exactBinding.keyId), true);
  assert.equal(applyCalls(serve.calls).length, 1);
});

test("a Serve apply that reports confirmation without applying B converges to A instead of success", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness({ apply: "confirmed-not-applied" });
  const store = memoryStore();

  const result = await runWorkspaceModelScope(request(), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });

  assert.equal(result.state, "converged_original");
  assert.equal(result.reason, "apply_unconfirmed");
  assert.equal(result.success, false);
  assert.deepEqual(gateway.scope, scopeA);
  assert.deepEqual(serve.scope, scopeA);
  assert.deepEqual(result.record?.gatewayReadback.scope, scopeA);
  assert.deepEqual(result.record?.applicationReadback.scope, scopeA);
  assert.deepEqual(updateCalls(gateway.calls).map((call) => call.request.scope), [scopeB, scopeA]);
});

test("success requires the Gateway readback and the Serve readback to agree on B", async () => {
  // Gateway reports its mutation confirmed, but its own readback still says A.
  // The operation converges to A; it never reports success.
  const unchanged = gatewayHarness({ update: "confirmed-not-applied" });
  const unchangedServe = serveHarness();
  const converged = await runWorkspaceModelScope(request({ operationId: "scope-gateway-unconfirmed" }), {
    gateway: unchanged.provider,
    serve: unchangedServe.boundary,
    store: memoryStore()
  });
  assert.equal(converged.state, "converged_original");
  assert.equal(converged.reason, "gateway_scope_unconfirmed");
  assert.equal(converged.success, false);
  assert.equal(converged.record?.state, "converged_original");
  assert.equal(applyCalls(unchangedServe.calls).length, 0);

  // Serve applies B, but the final Gateway readback drifts back to A. Two
  // owners disagree, so the operation is recovery-required, never success.
  const drift = gatewayHarness({
    scopeRead: ({ call }: any) => call === 3
      ? { outcome: "scope", binding: exactBinding, scope: scopeA }
      : undefined
  });
  const appliedServe = serveHarness();
  const disagreement = await runWorkspaceModelScope(request({ operationId: "scope-readback-disagreement" }), {
    gateway: drift.provider,
    serve: appliedServe.boundary,
    store: memoryStore()
  });
  assert.equal(disagreement.state, "recovery-required");
  assert.equal(disagreement.reason, "both_readbacks_disagree");
  assert.equal(disagreement.success, false);
  assert.deepEqual(appliedServe.scope, scopeB);
  assert.deepEqual(disagreement.record?.gatewayReadback.scope, scopeA);
  assert.deepEqual(disagreement.record?.applicationReadback.scope, scopeB);

  // Serve reports B but not at the approved target version. Both facts must
  // agree, so this is also not success.
  const wrongVersion = serveHarness({ apply: "applied-wrong-version" });
  const wrongVersionResult = await runWorkspaceModelScope(request({ operationId: "scope-wrong-version" }), {
    gateway: gatewayHarness().provider,
    serve: wrongVersion.boundary,
    store: memoryStore()
  });
  assert.equal(wrongVersionResult.state, "recovery-required");
  assert.equal(wrongVersionResult.reason, "application_version_mismatch");
  assert.equal(wrongVersionResult.success, false);
});

test("a replay of an applied operation returns the recorded result without another mutation", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness();
  const store = memoryStore();
  const original = request();

  const first = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(first.state, "applied");
  assert.equal(first.success, true);
  const callsAfterFirst = gateway.calls.length + serve.calls.length;
  const versionAfterFirst = first.record?.appliedVersion;

  const replay = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(replay.state, "applied");
  assert.equal(replay.success, true);
  assert.equal(replay.mutated, false);
  assert.equal(replay.replayed, true);
  assert.equal(replay.record?.appliedVersion, versionAfterFirst);
  assert.equal(gateway.calls.length + serve.calls.length, callsAfterFirst);
});

test("the same operation id under another scope is an idempotency conflict and leaves the record untouched", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness();
  const store = memoryStore();
  const original = request();
  const first = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(first.state, "applied");
  const callsAfterFirst = gateway.calls.length + serve.calls.length;

  const otherScopeB = workspaceModelScope({ models: ["model-b"], groups: ["group-a"] });
  const conflict = await runWorkspaceModelScope(request({ operationId: original.operationId, scopeB: otherScopeB }), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(conflict.state, "recovery-required");
  assert.equal(conflict.reason, "idempotency_conflict");
  assert.equal(conflict.success, false);
  assert.equal(conflict.replayed, true);
  assert.equal(gateway.calls.length + serve.calls.length, callsAfterFirst);

  const stored = await store.read(original.operationId);
  assert.deepEqual(stored?.scopeB, scopeB);
  assert.equal(stored?.state, "applied");
});

test("stale expected versions and lost CAS writes are recovery-required, never applied", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness();
  const store = memoryStore();

  const staleNew = await runWorkspaceModelScope(request({ expectedAppliedVersion: 1 }), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(staleNew.state, "recovery-required");
  assert.equal(staleNew.reason, "stale_applied_version");
  assert.equal(staleNew.success, false);
  assert.equal(staleNew.mutated, false);
  assert.equal(gateway.calls.length, 0);
  assert.equal(serve.calls.length, 0);
  assert.equal(store.records.size, 0);

  const first = await runWorkspaceModelScope(request({ operationId: "scope-cas-applied" }), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(first.state, "applied");
  const callsAfterFirst = gateway.calls.length + serve.calls.length;

  const staleReplay = await runWorkspaceModelScope(
    request({ operationId: "scope-cas-applied", expectedAppliedVersion: 0 }),
    { gateway: gateway.provider, serve: serve.boundary, store }
  );
  assert.equal(staleReplay.state, "recovery-required");
  assert.equal(staleReplay.reason, "stale_applied_version");
  assert.equal(staleReplay.success, false);
  assert.equal(staleReplay.replayed, true);
  assert.equal(gateway.calls.length + serve.calls.length, callsAfterFirst);

  // A CAS lost at the Gateway dispatch fence means no mutation was dispatched.
  const noDispatchStore: WorkspaceModelScopeStore = {
    ...store,
    async compareAndSwap(operationId, expectedAppliedVersion, next) {
      if (operationId === "scope-cas-dispatch") return false;
      return store.compareAndSwap(operationId, expectedAppliedVersion, next);
    }
  };
  const noDispatchGateway = gatewayHarness();
  const noDispatch = await runWorkspaceModelScope(request({ operationId: "scope-cas-dispatch" }), {
    gateway: noDispatchGateway.provider,
    serve: serveHarness().boundary,
    store: noDispatchStore
  });
  assert.equal(noDispatch.state, "recovery-required");
  assert.equal(noDispatch.reason, "stale_cas");
  assert.equal(noDispatch.mutated, false);
  assert.equal(updateCalls(noDispatchGateway.calls).length, 0);

  // A CAS lost after the Gateway mutation cannot prove the effect. The Key is
  // already at B, and the operation is recovery-required rather than settled.
  let casWrites = 0;
  const afterMutationStore: WorkspaceModelScopeStore = {
    ...store,
    async compareAndSwap(operationId, expectedAppliedVersion, next) {
      if (operationId === "scope-cas-after") {
        casWrites += 1;
        if (casWrites === 2) return false;
      }
      return store.compareAndSwap(operationId, expectedAppliedVersion, next);
    }
  };
  const afterMutationGateway = gatewayHarness();
  const lostCas = await runWorkspaceModelScope(request({ operationId: "scope-cas-after" }), {
    gateway: afterMutationGateway.provider,
    serve: serveHarness().boundary,
    store: afterMutationStore
  });
  assert.equal(lostCas.state, "recovery-required");
  assert.equal(lostCas.reason, "stale_cas");
  assert.equal(lostCas.success, false);
  assert.equal(lostCas.mutated, true);
  assert.deepEqual(afterMutationGateway.scope, scopeB);
});

test("a lost Gateway response is recovery-required, and a replay settles only from fresh readbacks", async () => {
  const gateway = gatewayHarness({ update: "response-lost-applied" });
  const serve = serveHarness();
  const store = memoryStore();
  const original = request();

  const first = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(first.state, "recovery-required");
  assert.equal(first.reason, "response_lost");
  assert.equal(first.success, false);
  assert.deepEqual(gateway.scope, scopeB);
  assert.deepEqual(serve.scope, scopeA);
  assert.equal(applyCalls(serve.calls).length, 0);

  const replay = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(replay.state, "applied");
  assert.equal(replay.success, true);
  assert.equal(replay.replayed, true);
  assert.deepEqual(gateway.scope, scopeB);
  assert.deepEqual(serve.scope, scopeB);
  assert.equal(updateCalls(gateway.calls).length, 1);
});

test("a lost Serve response is never success in that invocation and settles on a both-readback replay", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness({ apply: "response-lost-applied" });
  const store = memoryStore();
  const original = request();

  const first = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(first.state, "recovery-required");
  assert.equal(first.reason, "response_lost");
  assert.equal(first.success, false);
  assert.deepEqual(gateway.scope, scopeB);
  assert.deepEqual(serve.scope, scopeB);

  const replay = await runWorkspaceModelScope(original, {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });
  assert.equal(replay.state, "applied");
  assert.equal(replay.success, true);
  assert.equal(replay.replayed, true);
  assert.equal(replay.mutated, false);
  assert.equal(applyCalls(serve.calls).length, 1);
});

test("unknown Gateway outcomes and ownership ambiguity never settle", async () => {
  const unknownGateway = gatewayHarness({ update: "unknown" });
  const unknown = await runWorkspaceModelScope(request({ operationId: "scope-gateway-unknown" }), {
    gateway: unknownGateway.provider,
    serve: serveHarness().boundary,
    store: memoryStore()
  });
  assert.equal(unknown.state, "recovery-required");
  assert.equal(unknown.reason, "provider_outcome_unknown");
  assert.equal(unknown.success, false);

  const ambiguous = gatewayHarness({ bindingRead: { outcome: "ambiguous", candidates: 2 } });
  const ambiguousResult = await runWorkspaceModelScope(request({ operationId: "scope-binding-ambiguous" }), {
    gateway: ambiguous.provider,
    serve: serveHarness().boundary,
    store: memoryStore()
  });
  assert.equal(ambiguousResult.state, "recovery-required");
  assert.equal(ambiguousResult.reason, "ownership_ambiguous");
  assert.equal(ambiguousResult.success, false);
  assert.equal(ambiguousResult.mutated, false);
  assert.equal(updateCalls(ambiguous.calls).length, 0);

  const mismatched = gatewayHarness({
    scopeRead: {
      outcome: "scope",
      binding: { ...exactBinding, keyId: "9999" },
      scope: scopeA
    }
  });
  const mismatchResult = await runWorkspaceModelScope(request({ operationId: "scope-key-mismatch" }), {
    gateway: mismatched.provider,
    serve: serveHarness().boundary,
    store: memoryStore()
  });
  assert.equal(mismatchResult.state, "recovery-required");
  assert.equal(mismatchResult.reason, "ownership_mismatch");
  assert.equal(mismatchResult.success, false);
  assert.equal(mismatchResult.mutated, false);
});

test("today's missing same-Key scope endpoint converges to the original scope, never success", async () => {
  const gateway = gatewayHarness({ capabilities: ["readBinding", "readScope"] });
  const serve = serveHarness();

  const result = await runWorkspaceModelScope(request(), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store: memoryStore()
  });

  assert.equal(result.state, "converged_original");
  assert.equal(result.reason, "capability_unsupported");
  assert.equal(result.success, false);
  assert.equal(result.mutated, false);
  assert.deepEqual(gateway.scope, scopeA);
  assert.deepEqual(serve.scope, scopeA);
  assert.equal(updateCalls(gateway.calls).length, 0);
  assert.equal(applyCalls(serve.calls).length, 0);
});

test("a missing Serve apply capability restores the Gateway scope instead of leaving B wider or settled", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness({ capabilities: ["readApplication"] });

  const result = await runWorkspaceModelScope(request(), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store: memoryStore()
  });

  assert.equal(result.state, "converged_original");
  assert.equal(result.reason, "application_capability_unavailable");
  assert.equal(result.success, false);
  assert.deepEqual(gateway.scope, scopeA);
  assert.deepEqual(updateCalls(gateway.calls).map((call) => call.request.scope), [scopeB, scopeA]);
  assert.equal(applyCalls(serve.calls).length, 0);
});

test("no boundary can issue a replacement Key and no record stores raw Key or Secret material", async () => {
  const gateway = gatewayHarness();
  const serve = serveHarness({ apply: "refused" });
  const store = memoryStore();
  const result = await runWorkspaceModelScope(request(), {
    gateway: gateway.provider,
    serve: serve.boundary,
    store
  });

  assert.equal(result.state, "converged_original");
  assert.deepEqual(GATEWAY_MODEL_SCOPE_OPERATIONS, ["readBinding", "readScope", "updateScope"]);
  assert.deepEqual(Object.keys(gateway.provider).sort(), ["capabilities", "contract", "readBinding", "readScope", "updateScope"]);
  for (const forbidden of ["createKey", "issueKey", "replaceKey", "CreateUserKey", "UpdateUserKey"]) {
    assert.equal(forbidden in gateway.provider, false, forbidden);
  }
  assert.equal(result.record?.binding.keyId, exactBinding.keyId);
  assert.equal(gateway.calls.every((call) => call.request.binding?.keyId === exactBinding.keyId), true);
  for (const call of updateCalls(gateway.calls)) {
    assert.deepEqual(Object.keys(call.request).sort(), ["binding", "idempotencyKey", "scope"]);
    assert.equal("key" in call.request, false);
    assert.equal("replacementKey" in call.request, false);
  }
  const serialized = JSON.stringify(result.record);
  for (const forbidden of [rawSubject, rawKey, rawSecret, "sk-", "newKeyId", "replacementKeyId"]) {
    assert.equal(serialized.includes(forbidden), false, forbidden);
  }
});

test("the operation journal survives a store restart and refuses corrupt records", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-workspace-model-scope-"));
  try {
    const store = await createFileWorkspaceModelScopeStore(root);
    const gateway = gatewayHarness();
    const serve = serveHarness();
    const original = request();

    const first = await runWorkspaceModelScope(original, {
      gateway: gateway.provider,
      serve: serve.boundary,
      store
    });
    assert.equal(first.state, "applied");
    assert.equal(first.success, true);

    const files = await readdir(root);
    assert.deepEqual(files, ["workspace-model-scope-op-1.json"]);
    const serialized = await readFile(join(root, files[0]), "utf8");
    const parsed = JSON.parse(serialized);
    assert.equal(parsed.state, "applied");
    assert.equal(parsed.binding.keyId, exactBinding.keyId);
    assert.equal(parsed.binding.externalSubjectDigest, subjectDigest);
    assert.equal(parsed.binding.gateway.keyBindingId, exactBinding.gateway.keyBindingId);
    assert.equal(parsed.binding.gateway.fingerprint, exactBinding.gateway.fingerprint);
    for (const forbidden of [rawSubject, rawKey, rawSecret, "api_key", "rawKey", "rawSecret"]) {
      assert.equal(serialized.includes(forbidden), false, forbidden);
    }

    // A new store instance over the same directory reads the same operation, so
    // a replay does not depend on in-process state and never mutates again.
    const reopened = await createFileWorkspaceModelScopeStore(root);
    const replay = await runWorkspaceModelScope(original, {
      gateway: gateway.provider,
      serve: serve.boundary,
      store: reopened
    });
    assert.equal(replay.state, "applied");
    assert.equal(replay.mutated, false);
    assert.equal(replay.replayed, true);
    assert.equal(updateCalls(gateway.calls).length, 1);

    // A converged record and a recovery record also pass the closed journal
    // validator when written through the real file store.
    const converged = await runWorkspaceModelScope(
      request({ operationId: "scope-file-converged" }),
      { gateway: gatewayHarness().provider, serve: serveHarness({ apply: "refused" }).boundary, store }
    );
    assert.equal(converged.state, "converged_original");
    const recovery = await runWorkspaceModelScope(
      request({ operationId: "scope-file-recovery" }),
      { gateway: gatewayHarness({ update: "unknown" }).provider, serve: serveHarness().boundary, store }
    );
    assert.equal(recovery.state, "recovery-required");
    assert.deepEqual((await readdir(root)).sort(), [
      "scope-file-converged.json", "scope-file-recovery.json", "workspace-model-scope-op-1.json"
    ]);

    const file = join(root, "workspace-model-scope-op-1.json");
    const corrupt = JSON.parse(await readFile(file, "utf8"));
    corrupt.state = "successful";
    await writeFile(file, JSON.stringify(corrupt));
    await assert.rejects(reopened.read("workspace-model-scope-op-1"), /record_invalid/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
