import assert from "node:assert/strict";
import { createHash, createHmac } from "node:crypto";
import test from "node:test";
import { candidateReceipt } from "../helpers/protected-qualification-fixture.ts";
import { canonicalJson } from "../../tools/cloud-candidate-receipt.ts";
import {
  COMPUTE_DETACH_RECONCILIATION_CONFIRMATION,
  runWorkspaceComputeDetachReconciliation,
  type DetachReconciliationAccess,
} from "../../tools/workspace-compute-detach-reconciliation.ts";

const workspaceId = "workspace_1234567890abcdef", workspaceOperationId = "op_original_delete";
const tenantId = "tenant_original", resourceSetId = "set_original", fabricOperationId = "fabric_op_original", computeId = "compute_original";
const accountId = "account_original", destroyOperationId = "op_destroy_original", detachOperationId = "op_detach_original";
const detachRequestHash = "a".repeat(64), authorizationReference = "operator_intent_original";
const signingKey = "synthetic-private-capability-key-0000000001";
const refusal = /^workspace_compute_(?:detach_reconciliation_refused|readback_binding_invalid|destroy_journal_invalid)$/;
const isRefusal = (error: unknown) => error instanceof Error && refusal.test(error.message);
const detachAction = "detach_prepaid_compute_allocation";

function input() {
  return { candidate: candidateReceipt(), workspaceId, workspaceOperationId, authorizationReference,
    confirmation: COMPUTE_DETACH_RECONCILIATION_CONFIRMATION };
}
function workspaceRow() {
  return { operationId: workspaceOperationId, tenantId, resourceId: workspaceId, kind: "delete_workspace", workspaceTenantId: tenantId,
    frozenSetId: resourceSetId, resourceReadback: { workspaceId, resourceSetId },
    deleteOperation: { operationId: fabricOperationId, resourceId: resourceSetId } };
}
function fabricRow() {
  return { resourceSetId, workspaceId, tenantId,
    operation: { operationId: fabricOperationId, resourceId: resourceSetId, tenantId, kind: "resource_delete" },
    resources: [{ id: computeId, resourceSetId, kind: "compute" }],
    deletionActions: [{ resourceSetId, resourceId: computeId, commandId: `${fabricOperationId}:compute`, action: "delete",
      approvedInput: { deletionKind: "compute", resourceSetId, resourceId: computeId } }] };
}
function journalRows(): Record<string, unknown>[] {
  // These are the actual SQL projection shapes used by live-readback, not the
  // redacted receipt or the unprojected redacted_provider_payload object.
  const base = { id: "fop_destroy_1", operationId: destroyOperationId, resourceId: computeId, workspaceId, accountId,
    action: "destroy_compute_allocation", resourceKind: "compute_allocation", status: "failed",
    errorCode: "compute_prepaid_retirement_unconfirmed", retryable: false, providerRequestId: "", requestHash: "b".repeat(64),
    startedAt: "2026-11-11T01:30:00.000000123Z", finishedAt: "2026-11-11T01:31:00.000000123Z", createdAt: "2026-11-11T01:31:00.000000123Z",
    phase: "dispatch_authorized_uncertain", mutationCount: null,
    resource: { id: computeId, workspaceId, accountId, provider: "tencent-tke" }, originalDestroyOperationId: null,
    deleteRequests: { deleteMethod: null, scaleDown: null, deleteMode: null, describeClusterMachinesReq: null,
      describeNodePoolRequestId: null, modifySelfProvisioningReqId: null, verifyMachineDeletedReqId: null } };
  const stage = { ...base, operationId: detachOperationId, action: detachAction, resourceKind: "compute_prepaid_retirement",
    requestHash: detachRequestHash, originalDestroyOperationId: destroyOperationId, resource: null, phase: null,
    startedAt: "2026-11-11T01:32:31.000000123Z" };
  return [base,
    { ...stage, id: "fop_detach_start", status: "started", errorCode: "", finishedAt: null, createdAt: stage.startedAt },
    { ...stage, id: "fop_detach_failed", errorCode: "compute_prepaid_detach_dispatch_unconfirmed",
      finishedAt: "2026-11-11T01:32:31.883456789Z", createdAt: "2026-11-11T01:32:31.883456789Z" }];
}
function accepted() { return { operationId: "op_owner_reconciliation", dispatchAttempted: true, machineAbsent: false }; }
function fixture(response: { status: number; body: unknown } = { status: 202, body: accepted() }) {
  const calls: string[] = [], posts: Array<{ path: string; body: string; headers: Record<string, string> }> = [];
  const workspace = workspaceRow(), fabric = fabricRow();
  let journal = journalRows();
  const access: DetachReconciliationAccess = {
    async select(owner, sql) {
      calls.push(`select:${owner}`);
      assert.match(sql, /^SELECT /);
      assert.doesNotMatch(sql, /\b(?:INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE)\b/);
      if (owner === "workspace") {
        assert.ok(sql.includes(`o.id='${workspaceOperationId}'`));
        assert.ok(sql.includes(`o.resource_id='${workspaceId}'`));
        return [JSON.stringify(workspace)];
      }
      if (owner === "fabric") {
        assert.ok(sql.includes(`o.id='${fabricOperationId}'`));
        assert.ok(sql.includes(`s.id='${resourceSetId}'`));
        return [JSON.stringify(fabric)];
      }
      assert.ok(sql.includes(`resource_id='${computeId}'`));
      return journal.map(row => JSON.stringify(row));
    },
    async withFabric(image, consume) {
      calls.push("open");
      assert.deepEqual(image, candidateReceipt().cloudImage);
      try {
        const value = await consume({ async post(path, body, signCapability) {
          calls.push("post");
          const headers = signCapability(signingKey);
          posts.push({ path, body, headers });
          return response;
        } });
        return { pin: { imageIndexDigest: image.indexDigest }, value };
      } finally { calls.push("close"); }
    },
  };
  return { access, calls, posts, workspace, fabric, get journal() { return journal; }, set journal(value) { journal = value; } };
}

async function assertNoPost(f: ReturnType<typeof fixture>, request = input()) {
  await assert.rejects(() => runWorkspaceComputeDetachReconciliation(request, f.access), isRefusal);
  assert.deepEqual(f.posts, []);
  assert.equal(f.calls.includes("post"), false);
}

test("original Workspace/Fabric bindings produce one canonical signed command and accept HTTP 202", async () => {
  const f = fixture(), before = structuredClone(f.journal), earliest = Math.floor(Date.now() / 1000);
  const out = await runWorkspaceComputeDetachReconciliation(input(), f.access);
  assert.deepEqual(out, accepted());
  assert.deepEqual(f.calls, ["select:workspace", "select:fabric", "open", "select:fabric_domain", "post", "close"]);
  assert.equal(f.posts.length, 1);
  const post = f.posts[0];
  assert.equal(post.path, `/fabric/compute-allocations/${computeId}/reconcile-prepaid-detach`);
  const command = { accountId, workspaceId, destroyOperationId, detachOperationId, detachRequestHash,
    authorizationReference, confirmation: COMPUTE_DETACH_RECONCILIATION_CONFIRMATION };
  assert.deepEqual(JSON.parse(post.body), command);
  assert.equal(post.body, canonicalJson(command));
  assert.equal(post.headers["content-type"], "application/json");
  assert.equal(post.headers["Idempotency-Key"], authorizationReference);
  assert.deepEqual(Object.keys(post.headers).sort(), ["Idempotency-Key", "X-OPL-Fabric-Capability", "content-type"].sort());
  const [payload, signature, extra] = post.headers["X-OPL-Fabric-Capability"].split(".");
  assert.equal(extra, undefined);
  assert.equal(signature, createHmac("sha256", signingKey).update(payload).digest("base64url"));
  const claims = JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
  assert.ok(Number.isSafeInteger(claims.expiresAt));
  assert.ok(claims.expiresAt >= earliest + 60 && claims.expiresAt <= Math.floor(Date.now() / 1000) + 60);
  assert.deepEqual(claims, { version: 1, caller: "operator", accountId, workspaceId, resourceKind: "compute_allocation", resourceId: computeId,
    action: "reconcile_prepaid_compute_detachment", operationId: authorizationReference, expiresAt: claims.expiresAt,
    bodySha256: createHash("sha256").update(post.body).digest("hex") });
  assert.deepEqual(f.journal, before, "the original uncertain fence is never rewritten by the tool");
  assert.doesNotMatch(JSON.stringify(out), /account_original|op_destroy_original|op_detach_original|synthetic-private|X-OPL-Fabric-Capability/);
});

test("HTTP 202 accepts both observed absence and an attempted but still uncertain reconciliation", async t => {
  for (const body of [accepted(), { ...accepted(), machineAbsent: true }, { operationId: "", dispatchAttempted: false, machineAbsent: true }]) {
    await t.test(JSON.stringify(body), async () => {
      const f = fixture({ status: 202, body });
      assert.deepEqual(await runWorkspaceComputeDetachReconciliation(input(), f.access), body);
      assert.equal(f.posts.length, 1);
    });
  }
});

test("bounded original SDK failure classes retain the same explicitly authorized recovery path", async t => {
  for (const providerClass of ["AuthFailure", "InvalidParameterValue", "UnsupportedOperation", "RequestLimitExceeded", "InternalError", "ResourceNotFound"]) {
    await t.test(providerClass, async () => {
      const f = fixture();
      f.journal[2].errorCode = `compute_prepaid_detach_dispatch_unconfirmed:${providerClass}`;
      assert.deepEqual(await runWorkspaceComputeDetachReconciliation(input(), f.access), accepted());
      assert.equal(f.posts.length, 1);
    });
  }
});

test("binding decoders reject foreign original Workspace operations, Fabric commands and approved compute identities before POST", async t => {
  const cases: Array<[string, (f: ReturnType<typeof fixture>) => void]> = [
    ["Workspace operation", f => { f.workspace.operationId = "op_foreign"; }],
    ["Workspace identity", f => { f.workspace.resourceId = "workspace_foreign"; }],
    ["Workspace tenant", f => { f.workspace.workspaceTenantId = "tenant_foreign"; }],
    ["frozen resource set", f => { f.workspace.frozenSetId = "set_foreign"; }],
    ["Fabric operation", f => { f.fabric.operation.operationId = "op_foreign"; }],
    ["Fabric tenant", f => { f.fabric.tenantId = "tenant_foreign"; }],
    ["original deletion command", f => { f.fabric.deletionActions[0].commandId = "foreign:compute"; }],
    ["approved compute", f => { f.fabric.deletionActions[0].approvedInput.resourceId = "compute_foreign"; }],
    ["multiple compute resources", f => { f.fabric.resources.push({ ...f.fabric.resources[0], id: "compute_foreign" }); }],
  ];
  for (const [name, change] of cases) await t.test(name, async () => { const f = fixture(); change(f); await assertNoPost(f); });
});

test("unknown, foreign, refused and conflicting original detach fences never issue POST", async t => {
  const cases: Array<[string, (f: ReturnType<typeof fixture>) => void]> = [
    ["unknown journal", f => { f.journal = []; }],
    ["no detach fence", f => { f.journal = f.journal.slice(0, 1); }],
    ["sender still running", f => { f.journal.pop(); }],
    ["no original claim", f => { f.journal.splice(1, 1); }],
    ["already succeeded", f => { f.journal[2].status = "succeeded"; f.journal[2].errorCode = ""; }],
    ["refused outcome", f => { f.journal[2].errorCode = "compute_prepaid_detach_identity_or_state_mismatch"; }],
    ["unknown outcome", f => { f.journal[2].errorCode = "private-provider-message"; }],
    ["foreign original destroy", f => { f.journal[2].originalDestroyOperationId = "op_foreign"; }],
    ["foreign account", f => { f.journal[2].accountId = "account_foreign"; }],
    ["foreign Workspace", f => { f.journal[2].workspaceId = "workspace_foreign"; }],
    ["foreign compute", f => { f.journal[2].resourceId = "compute_foreign"; }],
    ["foreign operation", f => { f.journal[2].operationId = "op_foreign"; }],
    ["foreign request hash", f => { f.journal[2].requestHash = "c".repeat(64); }],
    ["missing request hash", f => { f.journal[1].requestHash = f.journal[2].requestHash = null; }],
    ["invalid request hash", f => { f.journal[1].requestHash = f.journal[2].requestHash = "not-a-sha256"; }],
    ["different start instant", f => { f.journal[2].startedAt = "2026-11-11T01:32:31.000000999Z"; }],
    ["missing completion", f => { f.journal[2].finishedAt = null; }],
    ["duplicate terminal", f => { f.journal.push({ ...f.journal[2], id: "fop_extra_failed" }); }],
    ["wrong resource kind", f => { f.journal[2].resourceKind = "compute_allocation"; }],
    ["raw private payload", f => { f.journal[2].redactedProviderPayload = { secret: "never-export" }; }],
  ];
  for (const [name, change] of cases) await t.test(name, async () => { const f = fixture(); change(f); await assertNoPost(f); });
});

test("explicit operator authorization and Candidate are validated before any owner access", async t => {
  const cases = [
    { ...input(), confirmation: "RETRY" }, { ...input(), authorizationReference: "" },
    { ...input(), authorizationReference: "operator\nprivate" }, { ...input(), candidate: {} },
    { ...input(), computeId: "compute_injected" }, { ...input(), detachOperationId: "op_injected" },
  ];
  for (const [index, value] of cases.entries()) await t.test(String(index), async () => {
    const f = fixture();
    await assert.rejects(() => runWorkspaceComputeDetachReconciliation(value, f.access));
    assert.deepEqual(f.calls, []);
  });
});

test("only exact typed response fields are accepted, including no leaked messages or capability", async t => {
  const badBodies: Array<[string, unknown]> = [
    ["null", null], ["array", []], ["text", JSON.stringify(accepted())],
    ["missing operation", { dispatchAttempted: true, machineAbsent: false }],
    ["missing attempted", { operationId: "op_owner_reconciliation", machineAbsent: false }],
    ["string attempted", { ...accepted(), dispatchAttempted: "true" }],
    ["numeric absent", { ...accepted(), machineAbsent: 0 }],
    ["message leak", { ...accepted(), message: "SecretKey=never-export" }],
    ["capability leak", { ...accepted(), capability: signingKey }],
    ["invalid operation", { ...accepted(), operationId: "op\nprivate-secret" }],
    ["long operation", { ...accepted(), operationId: "a".repeat(257) }],
    ["attempt with empty operation", { ...accepted(), operationId: "" }],
    ["no attempt and no absence", { operationId: "", dispatchAttempted: false, machineAbsent: false }],
  ];
  for (const status of [200, 202]) for (const [name, body] of badBodies) await t.test(`${status}/${name}`, async () => {
    const f = fixture({ status, body });
    await assert.rejects(() => runWorkspaceComputeDetachReconciliation(input(), f.access), isRefusal);
    assert.equal(f.posts.length, 1);
    assert.equal(f.calls.at(-1), "close");
  });
});

test("HTTP owner refusal, lost response and transport cleanup failure never cause a second POST", async t => {
  for (const status of [200, 201, 204, 400, 401, 403, 409, 500]) await t.test(String(status), async () => {
    const f = fixture({ status, body: status < 400 ? accepted() : { error: "private-provider-detail SecretKey=never-export" } });
    await assert.rejects(() => runWorkspaceComputeDetachReconciliation(input(), f.access), isRefusal);
    assert.equal(f.posts.length, 1);
    assert.equal(f.calls.at(-1), "close");
  });
  for (const phase of ["post", "cleanup"]) await t.test(phase, async () => {
    const f = fixture(); let sends = 0;
    f.access.withFabric = async (_image, consume) => {
      const value = await consume({ async post(_path, _body, signCapability) {
        sends++; signCapability(signingKey);
        if (phase === "post") throw new Error("synthetic lost response");
        return { status: 202, body: accepted() };
      } });
      throw new Error(`synthetic cleanup failure ${Boolean(value)}`);
    };
    await assert.rejects(() => runWorkspaceComputeDetachReconciliation(input(), f.access));
    assert.equal(sends, 1);
  });
});
