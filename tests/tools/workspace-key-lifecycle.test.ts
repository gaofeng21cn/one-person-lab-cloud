import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  REQUIRED_GATEWAY_INTEGRATION_CONTRACT,
  WorkspaceKeyLifecycleStoreConflictError,
  WorkspaceKeyProviderResponseLostError,
  createFileWorkspaceKeyLifecycleStore,
  externalSubjectDigest,
  runWorkspaceKeyLifecycle,
  workspaceKeyLifecycleIdempotencyKey
} from "../../tools/workspace-key-lifecycle.ts";
import type {
  WorkspaceKeyLifecycleProvider,
  WorkspaceKeyLifecycleStore,
  WorkspaceKeyProviderOperation,
  WorkspaceManagedKeyBinding
} from "../../tools/workspace-key-lifecycle.ts";

// A raw external subject and a raw key value exist only in this test's memory.
// The seam accepts the digest and the external key identity and never persists
// either raw value.
const rawSubject = "sub2api-user-41";
const rawKey = "sk-live-raw-key-12345";
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

const ALL_CAPABILITIES: readonly WorkspaceKeyProviderOperation[] = Object.freeze(["readBinding", "readState", "disable", "enable", "revoke"]);
const MUTATIONS = Object.freeze(["disable", "enable", "revoke"]);

type Call = Readonly<{ operation: string; request: any }>;

type HarnessOptions = Readonly<{
  state?: "active" | "disabled" | "revoked";
  capabilities?: readonly WorkspaceKeyProviderOperation[];
  bindingRead?: unknown;
  stateRead?: unknown;
  mutation?: unknown;
}>;

function providerHarness(options: HarnessOptions = {}) {
  const calls: Call[] = [];
  const capabilities = options.capabilities ?? ALL_CAPABILITIES;
  let state: "active" | "disabled" | "revoked" = options.state ?? "active";
  let bindingReadCount = 0;
  let stateReadCount = 0;
  let mutationCount = 0;

  const resolve = (configured: unknown, context: Record<string, unknown>): any =>
    typeof configured === "function" ? (configured as (value: Record<string, unknown>) => unknown)(context) : configured;

  async function mutate(operation: string, next: "active" | "disabled" | "revoked", request: any) {
    calls.push({ operation, request });
    mutationCount += 1;
    const configured = resolve(options.mutation, { operation, request, state, mutationCount });
    if (configured === undefined || configured === null) {
      state = next;
      return { outcome: "confirmed" };
    }
    if (configured === "applied") {
      state = next;
      return { outcome: "confirmed" };
    }
    if (configured === "confirmed-not-applied") return { outcome: "confirmed" };
    if (configured === "unknown") return { outcome: "unknown" };
    if (configured === "refused") return { outcome: "refused", code: "FailedPrecondition" };
    if (configured === "unsupported") return { outcome: "unsupported", code: "Unimplemented" };
    if (configured === "response-lost-applied") {
      state = next;
      throw new WorkspaceKeyProviderResponseLostError();
    }
    if (configured === "response-lost-not-applied") throw new WorkspaceKeyProviderResponseLostError();
    if (configured === "throw") throw new Error("provider transport failed");
    return configured;
  }

  const provider: WorkspaceKeyLifecycleProvider = Object.freeze({
    contract: REQUIRED_GATEWAY_INTEGRATION_CONTRACT.lifecycleSchema,
    capabilities,
    async readBinding(request: any) {
      calls.push({ operation: "readBinding", request });
      bindingReadCount += 1;
      const configured = resolve(options.bindingRead, { request, call: bindingReadCount });
      if (configured !== undefined && configured !== null) return configured;
      return { outcome: "binding", binding: exactBinding };
    },
    async readState(request: any) {
      calls.push({ operation: "readState", request });
      stateReadCount += 1;
      const configured = resolve(options.stateRead, { request, call: stateReadCount, state });
      if (configured !== undefined && configured !== null) return configured;
      return {
        outcome: "state",
        keyId: request.binding.keyId,
        externalSubjectDigest: request.binding.externalSubjectDigest,
        status: state
      };
    },
    async disable(request: any) {
      return mutate("disable", "disabled", request);
    },
    async enable(request: any) {
      return mutate("enable", "active", request);
    },
    async revoke(request: any) {
      return mutate("revoke", "revoked", request);
    }
  });

  return {
    provider,
    calls,
    get state() {
      return state;
    },
    get mutationCount() {
      return mutationCount;
    }
  };
}

function memoryStore(initial: readonly any[] = []): WorkspaceKeyLifecycleStore & { records: Map<string, any> } {
  const records = new Map<string, any>(initial.map((record) => [record.operationId, structuredClone(record)]));
  return {
    records,
    async read(operationId: string) {
      const record = records.get(operationId);
      return record ? structuredClone(record) : null;
    },
    async create(record: any) {
      if (records.has(record.operationId)) throw new WorkspaceKeyLifecycleStoreConflictError();
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
  operationId: "workspace-close-op-1",
  intent: "disable",
  binding: exactBinding,
  ...overrides
});

const mutationCalls = (calls: readonly Call[]) => calls.filter((call) => MUTATIONS.includes(call.operation));

test("disables only the exact bound Key and settles on the exact-Key readback", async () => {
  const harness = providerHarness({ state: "active" });
  const store = memoryStore();
  const result = await runWorkspaceKeyLifecycle(request(), { provider: harness.provider, store });

  assert.equal(result.state, "settled");
  assert.equal(result.reason, "confirmed");
  assert.equal(result.mutated, true);
  assert.equal(result.replayed, false);
  assert.equal(result.record?.state, "settled");
  assert.equal(result.record?.intent, "disable");
  assert.equal(result.record?.attempts, 1);
  assert.equal(result.record?.mutationAttempted, true);
  assert.equal(result.record?.bindingVerified, true);
  assert.equal(result.record?.readback.status, "disabled");
  assert.equal(result.record?.appliedVersion, 2);

  assert.deepEqual(harness.calls.map((call) => call.operation), ["readBinding", "readState", "disable", "readState"]);
  const disable = mutationCalls(harness.calls);
  assert.equal(disable.length, 1);
  assert.equal(disable[0].request.binding.keyId, exactBinding.keyId);
  assert.equal(disable[0].request.binding.workspaceId, exactBinding.workspaceId);
  assert.equal(disable[0].request.binding.externalSubjectDigest, subjectDigest);
  assert.equal(disable[0].request.idempotencyKey,
    workspaceKeyLifecycleIdempotencyKey("workspace-close-op-1", "disable", exactBinding));
  assert.equal(harness.calls.some((call) => call.operation === "enable" || call.operation === "revoke"), false);
});

test("re-enables the same original Key and never mints a replacement", async () => {
  const harness = providerHarness({ state: "disabled" });
  const store = memoryStore();
  const result = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-recover-op-1", intent: "enable" }),
    { provider: harness.provider, store });

  assert.equal(result.state, "settled");
  assert.equal(result.reason, "confirmed");
  assert.equal(result.mutated, true);
  assert.equal(result.record?.intent, "enable");
  assert.equal(result.record?.readback.status, "active");
  assert.equal(result.record?.binding.keyId, exactBinding.keyId);

  const mutations = mutationCalls(harness.calls);
  assert.deepEqual(mutations.map((call) => call.operation), ["enable"]);
  assert.equal(mutations[0].request.binding.keyId, exactBinding.keyId);
  assert.equal(harness.calls.every((call) => call.request.binding.keyId === exactBinding.keyId), true);
  assert.equal(harness.calls.some((call) => call.operation === "disable" || call.operation === "revoke"), false);
});

test("permanent revocation stays distinct from reversible disablement", async () => {
  const revokeHarness = providerHarness({ state: "active" });
  const revoked = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-permanent", intent: "revoke" }),
    { provider: revokeHarness.provider, store: memoryStore() });

  assert.equal(revoked.state, "settled");
  assert.equal(revoked.reason, "confirmed");
  assert.equal(revoked.mutated, true);
  assert.equal(revoked.record?.intent, "revoke");
  assert.equal(revoked.record?.readback.status, "revoked");
  assert.deepEqual(mutationCalls(revokeHarness.calls).map((call) => call.operation), ["revoke"]);

  // A revoked Key is terminal for this seam. A later enable is blocked before
  // any mutation rather than reported as a successful re-enable.
  const enableHarness = providerHarness({ state: "revoked" });
  const enabled = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-recover-after-revoke", intent: "enable" }),
    { provider: enableHarness.provider, store: memoryStore() });

  assert.equal(enabled.state, "blocked");
  assert.equal(enabled.reason, "key_revoked");
  assert.equal(enabled.mutated, false);
  assert.equal(mutationCalls(enableHarness.calls).length, 0);
});

test("ambiguous ownership fails closed before any mutation", async () => {
  const scenarios = [
    { bindingRead: { outcome: "ambiguous", candidates: 2 }, reason: "ownership_ambiguous" },
    { bindingRead: { outcome: "binding", binding: { ...exactBinding, workspaceId: "ws-other" } }, reason: "ownership_mismatch" },
    { bindingRead: { outcome: "binding", binding: { ...exactBinding, keyId: "9999" } }, reason: "ownership_mismatch" },
    {
      stateRead: { outcome: "state", keyId: "9999", externalSubjectDigest: subjectDigest, status: "active" },
      reason: "ownership_mismatch"
    }
  ];

  for (const scenario of scenarios) {
    const harness = providerHarness({ state: "active", ...scenario });
    const result = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-ambiguous" }),
      { provider: harness.provider, store: memoryStore() });

    assert.equal(result.state, "blocked", String(scenario.reason));
    assert.equal(result.reason, scenario.reason);
    assert.equal(result.mutated, false);
    assert.equal(result.record?.state, "blocked");
    assert.equal(result.record?.mutationAttempted, false);
    assert.equal(mutationCalls(harness.calls).length, 0);
  }
});

test("an unknown provider outcome requires recovery and never settles", async () => {
  const harness = providerHarness({ state: "active", mutation: "unknown" });
  const result = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-unknown" }),
    { provider: harness.provider, store: memoryStore() });

  assert.equal(result.state, "recovery-required");
  assert.equal(result.reason, "provider_outcome_unknown");
  assert.equal(result.mutated, true);
  assert.equal(result.record?.state, "recovery-required");
  assert.equal(result.record?.attempts, 1);
  assert.equal(result.record?.mutationAttempted, true);
  assert.equal(harness.state, "active");
  assert.deepEqual(harness.calls.map((call) => call.operation), ["readBinding", "readState", "disable"]);
});

test("a lost mutation response requires recovery instead of success", async () => {
  const harness = providerHarness({ state: "active", mutation: "response-lost-applied" });
  const result = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-lost" }),
    { provider: harness.provider, store: memoryStore() });

  assert.equal(result.state, "recovery-required");
  assert.equal(result.reason, "response_lost");
  assert.equal(result.mutated, true);
  assert.equal(result.record?.state, "recovery-required");
  assert.equal(result.record?.mutationAttempted, true);
  assert.equal(harness.state, "disabled");
  assert.deepEqual(harness.calls.map((call) => call.operation), ["readBinding", "readState", "disable"]);
});

test("a replay settles from an effect that landed before the lost response", async () => {
  const harness = providerHarness({ state: "active", mutation: "response-lost-applied" });
  const store = memoryStore();
  const original = request({ operationId: "workspace-close-lost-replay" });

  const first = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(first.state, "recovery-required");
  assert.equal(first.reason, "response_lost");

  const replay = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(replay.state, "settled");
  assert.equal(replay.reason, "confirmed");
  assert.equal(replay.mutated, false);
  assert.equal(replay.replayed, true);
  assert.equal(replay.record?.binding.keyId, exactBinding.keyId);
  assert.equal(mutationCalls(harness.calls).length, 1);
  assert.equal(mutationCalls(harness.calls)[0].request.idempotencyKey, first.idempotencyKey);
});

test("a replay re-dispatches the original effect under the original idempotency key", async () => {
  const harness = providerHarness({
    state: "active",
    mutation: ({ mutationCount }: any) => mutationCount === 1 ? "response-lost-not-applied" : "applied"
  });
  const store = memoryStore();
  const original = request({ operationId: "workspace-close-lost-redispatch" });

  const first = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(first.state, "recovery-required");
  assert.equal(first.reason, "response_lost");
  assert.equal(harness.state, "active");

  const replay = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(replay.state, "settled");
  assert.equal(replay.mutated, true);
  assert.equal(replay.replayed, true);
  assert.equal(harness.state, "disabled");

  const mutations = mutationCalls(harness.calls);
  assert.equal(mutations.length, 2);
  assert.equal(mutations.every((call) => call.operation === "disable"), true);
  assert.equal(mutations[0].request.idempotencyKey, mutations[1].request.idempotencyKey);
  assert.equal(mutations[0].request.binding.keyId, mutations[1].request.binding.keyId);
  assert.equal(replay.record?.binding.keyId, exactBinding.keyId);
});

test("replaying a settled operation returns its original result without another mutation", async () => {
  const harness = providerHarness({ state: "active" });
  const store = memoryStore();
  const original = request({ operationId: "workspace-close-settled-replay" });

  const first = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(first.state, "settled");
  const callsAfterFirst = harness.calls.length;

  const replay = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(replay.state, "settled");
  assert.equal(replay.reason, "confirmed");
  assert.equal(replay.mutated, false);
  assert.equal(replay.replayed, true);
  assert.equal(replay.record?.appliedVersion, first.record?.appliedVersion);
  assert.equal(replay.record?.idempotencyKey, first.idempotencyKey);
  assert.equal(harness.calls.length, callsAfterFirst);
});

test("the same operation id with another intent is an idempotency conflict", async () => {
  const harness = providerHarness({ state: "active" });
  const store = memoryStore();
  const original = request({ operationId: "workspace-close-conflict" });

  const first = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(first.state, "settled");
  const callsAfterFirst = harness.calls.length;

  const conflict = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-conflict", intent: "enable" }),
    { provider: harness.provider, store });
  assert.equal(conflict.state, "blocked");
  assert.equal(conflict.reason, "idempotency_conflict");
  assert.equal(conflict.mutated, false);
  assert.equal(harness.calls.length, callsAfterFirst);

  const stored = await store.read("workspace-close-conflict");
  assert.equal(stored?.intent, "disable");
  assert.equal(stored?.state, "settled");
});

test("stale applied versions and lost CAS writes are refused before mutation", async () => {
  const harness = providerHarness({ state: "active" });
  const store = memoryStore();
  const original = request({ operationId: "workspace-close-cas" });

  const first = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
  assert.equal(first.state, "settled");
  const callsAfterFirst = harness.calls.length;

  const stale = await runWorkspaceKeyLifecycle({ ...original, expectedAppliedVersion: 0 },
    { provider: harness.provider, store });
  assert.equal(stale.state, "blocked");
  assert.equal(stale.reason, "stale_applied_version");
  assert.equal(stale.mutated, false);
  assert.equal(harness.calls.length, callsAfterFirst);

  const fresh = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-cas-fresh", expectedAppliedVersion: 1 }),
    { provider: harness.provider, store });
  assert.equal(fresh.state, "blocked");
  assert.equal(fresh.reason, "stale_applied_version");
  assert.equal(store.records.has("workspace-close-cas-fresh"), false);

  // A CAS lost at the dispatch fence means no mutation was dispatched.
  let rejectFirstCas = true;
  const racingStore: WorkspaceKeyLifecycleStore = {
    ...store,
    async compareAndSwap(operationId, expectedAppliedVersion, next) {
      if (rejectFirstCas) {
        rejectFirstCas = false;
        return false;
      }
      return store.compareAndSwap(operationId, expectedAppliedVersion, next);
    }
  };
  const racingHarness = providerHarness({ state: "active" });
  const raced = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-cas-race" }),
    { provider: racingHarness.provider, store: racingStore });
  assert.equal(raced.state, "blocked");
  assert.equal(raced.reason, "stale_applied_version");
  assert.equal(raced.mutated, false);
  assert.equal(mutationCalls(racingHarness.calls).length, 0);

  // A CAS lost after the dispatch fence cannot prove the provider effect, so it
  // is recovery-required rather than a settled or merely blocked operation.
  let casWrites = 0;
  const afterMutationStore: WorkspaceKeyLifecycleStore = {
    ...store,
    async compareAndSwap(operationId, expectedAppliedVersion, next) {
      casWrites += 1;
      if (casWrites === 2) return false;
      return store.compareAndSwap(operationId, expectedAppliedVersion, next);
    }
  };
  const afterMutationHarness = providerHarness({ state: "active" });
  const lostCas = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-cas-after" }),
    { provider: afterMutationHarness.provider, store: afterMutationStore });
  assert.equal(lostCas.state, "recovery-required");
  assert.equal(lostCas.reason, "stale_cas");
  assert.equal(lostCas.mutated, true);
  assert.equal(afterMutationHarness.state, "disabled");
});

test("a post-mutation readback must still name the exact Key and its desired state", async () => {
  const identityMismatch = providerHarness({
    state: "active",
    mutation: "applied",
    stateRead: ({ call }: any) => call === 1 ? null : {
      outcome: "state", keyId: "9999", externalSubjectDigest: subjectDigest, status: "disabled"
    }
  });
  const mismatch = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-readback-mismatch" }),
    { provider: identityMismatch.provider, store: memoryStore() });
  assert.equal(mismatch.state, "recovery-required");
  assert.equal(mismatch.reason, "readback_identity_mismatch");
  assert.equal(mismatch.mutated, true);
  assert.notEqual(mismatch.state, "settled");

  const unchanged = providerHarness({ state: "active", mutation: "confirmed-not-applied" });
  const stateMismatch = await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-readback-unchanged" }),
    { provider: unchanged.provider, store: memoryStore() });
  assert.equal(stateMismatch.state, "recovery-required");
  assert.equal(stateMismatch.reason, "state_mismatch");
  assert.equal(stateMismatch.mutated, true);
  assert.equal(stateMismatch.record?.readback.status, "active");
});

test("a deployment result, resource absence and Secret removal do not prove Key state", async () => {
  const evidence = { deploymentResult: "succeeded", resourceAbsent: true, secretRemoved: true };
  const original = request({ operationId: "workspace-close-non-key-evidence", nonKeyEvidence: evidence });

  // An unavailable Key readback is recovery-required even when every other
  // lifecycle observation says the deployment succeeded and resources are gone.
  const unknownHarness = providerHarness({ stateRead: { outcome: "unknown" } });
  const unknown = await runWorkspaceKeyLifecycle(original, { provider: unknownHarness.provider, store: memoryStore() });
  assert.equal(unknown.state, "recovery-required");
  assert.equal(unknown.reason, "readback_unconfirmed");
  assert.equal(unknown.mutated, false);
  assert.equal(mutationCalls(unknownHarness.calls).length, 0);
  assert.deepEqual(unknown.record?.nonKeyEvidence, evidence);

  // A confirmed mutation with an unreadable post-mutation readback is still not
  // settled, no matter what the deployment result says.
  const postUnknownHarness = providerHarness({
    state: "active",
    stateRead: ({ call }: any) => call === 1 ? null : { outcome: "unknown" }
  });
  const postUnknown = await runWorkspaceKeyLifecycle(
    request({ operationId: "workspace-close-non-key-evidence-post", nonKeyEvidence: evidence }),
    { provider: postUnknownHarness.provider, store: memoryStore() }
  );
  assert.equal(postUnknown.state, "recovery-required");
  assert.equal(postUnknown.reason, "readback_unconfirmed");
  assert.equal(postUnknown.mutated, true);
  assert.notEqual(postUnknown.state, "settled");
  assert.deepEqual(postUnknown.record?.nonKeyEvidence, evidence);
});

test("a provider without the effect capability blocks it instead of faking success", async () => {
  const noRevoke = providerHarness({
    state: "active",
    capabilities: ["readBinding", "readState", "disable", "enable"]
  });
  const blocked = await runWorkspaceKeyLifecycle(
    request({ operationId: "workspace-close-revoke-unsupported", intent: "revoke" }),
    { provider: noRevoke.provider, store: memoryStore() }
  );
  assert.equal(blocked.state, "blocked");
  assert.equal(blocked.reason, "capability_unsupported");
  assert.equal(blocked.mutated, false);
  assert.equal(mutationCalls(noRevoke.calls).length, 0);

  // The current production issuer answers revocation with Unimplemented. An
  // adapter that translates that to `unsupported` yields a blocked operation,
  // never a settled one.
  const unimplemented = providerHarness({ state: "active", mutation: "unsupported" });
  const refused = await runWorkspaceKeyLifecycle(
    request({ operationId: "workspace-close-revoke-unimplemented", intent: "revoke" }),
    { provider: unimplemented.provider, store: memoryStore() }
  );
  assert.equal(refused.state, "blocked");
  assert.equal(refused.reason, "capability_unsupported");
  assert.equal(refused.record?.providerCode, "Unimplemented");

  // A bounded refusal is a definite no-effect answer, not an unknown outcome.
  const definite = providerHarness({ state: "active", mutation: "refused" });
  const denied = await runWorkspaceKeyLifecycle(
    request({ operationId: "workspace-close-revoke-refused", intent: "revoke" }),
    { provider: definite.provider, store: memoryStore() }
  );
  assert.equal(denied.state, "blocked");
  assert.equal(denied.reason, "provider_refused");
  assert.equal(denied.record?.providerCode, "FailedPrecondition");
  assert.equal(denied.mutated, true);
});

test("malformed bindings and broken boundary wiring fail closed", async () => {
  assert.throws(() => externalSubjectDigest(" "), /binding_invalid/);

  const harness = providerHarness();
  await assert.rejects(
    runWorkspaceKeyLifecycle(
      request({ operationId: "workspace-close-malformed", binding: { ...exactBinding, externalSubjectDigest: "not-a-digest" } }),
      { provider: harness.provider, store: memoryStore() }
    ),
    /binding_invalid/
  );
  await assert.rejects(
    runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-extra", unexpected: true }),
      { provider: harness.provider, store: memoryStore() }),
    /request_invalid/
  );
  await assert.rejects(
    runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-provider-contract" }),
      { provider: { ...harness.provider, contract: "other" }, store: memoryStore() }),
    /provider_invalid/
  );
  await assert.rejects(
    runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-provider-read" }),
      { provider: { ...harness.provider, readBinding: undefined } as any, store: memoryStore() }),
    /provider_invalid/
  );
  assert.equal(mutationCalls(harness.calls).length, 0);
});

test("the persisted record carries only exact binding facts and survives a store restart", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-workspace-key-lifecycle-"));
  try {
    const store = await createFileWorkspaceKeyLifecycleStore(root);
    const harness = providerHarness({ state: "active" });
    const original = request({ operationId: "workspace-close-file" });

    const first = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store });
    assert.equal(first.state, "settled");

    const files = await readdir(root);
    assert.deepEqual(files, ["workspace-close-file.json"]);
    const serialized = await readFile(join(root, files[0]), "utf8");
    const parsed = JSON.parse(serialized);
    assert.equal(parsed.appliedVersion, 2);
    assert.equal(parsed.binding.workspaceId, exactBinding.workspaceId);
    assert.equal(parsed.binding.externalSubjectDigest, subjectDigest);
    assert.equal(parsed.binding.keyId, exactBinding.keyId);
    assert.equal(parsed.binding.gateway.keyBindingId, exactBinding.gateway.keyBindingId);
    assert.equal(parsed.binding.gateway.fingerprint, exactBinding.gateway.fingerprint);
    for (const forbidden of [rawSubject, rawKey, "api_key", "rawKey", "rawSecret"]) {
      assert.equal(serialized.includes(forbidden), false, forbidden);
    }

    // A new store instance over the same directory reads the same operation, so
    // a replay does not depend on in-process state and never mutates again.
    const reopened = await createFileWorkspaceKeyLifecycleStore(root);
    const replay = await runWorkspaceKeyLifecycle(original, { provider: harness.provider, store: reopened });
    assert.equal(replay.state, "settled");
    assert.equal(replay.mutated, false);
    assert.equal(replay.replayed, true);
    assert.equal(mutationCalls(harness.calls).length, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a file store refuses corrupt records and relative roots", async () => {
  await assert.rejects(createFileWorkspaceKeyLifecycleStore("relative/path"), /store_invalid/);
  const root = await mkdtemp(join(tmpdir(), "opl-workspace-key-lifecycle-corrupt-"));
  try {
    const store = await createFileWorkspaceKeyLifecycleStore(root);
    const harness = providerHarness();
    await runWorkspaceKeyLifecycle(request({ operationId: "workspace-close-corrupt" }),
      { provider: harness.provider, store });
    const file = join(root, "workspace-close-corrupt.json");
    const parsed = JSON.parse(await readFile(file, "utf8"));
    parsed.state = "successful";
    await writeFile(file, JSON.stringify(parsed));
    await assert.rejects(store.read("workspace-close-corrupt"), /record_invalid/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
