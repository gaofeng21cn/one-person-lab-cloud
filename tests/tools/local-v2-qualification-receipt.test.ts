import assert from "node:assert/strict";
import test from "node:test";
import { validateLocalQualificationReceipt } from "../../tools/local-workspace-qualification.ts";
import { createLocalV2ReadyReceipt, LOCAL_V2_OWNER_PROCESSES, LOCAL_V2_STORE_IDENTITIES } from "../../tools/local-v2-qualification-receipt.ts";

function fixture() {
  const source = { sha: "a".repeat(40), tree: "b".repeat(40), clean: true };
  const image = (name, digest, running) => ({ input: `registry.example/${name}@sha256:${digest.repeat(64)}`,
    repoDigest: `registry.example/${name}@sha256:${digest.repeat(64)}`, digest: `sha256:${digest.repeat(64)}`, runningDigest: `sha256:${running.repeat(64)}` });
  const images = { cloud: image("cloud", "c", "d"), workspace: image("workspace", "e", "f") };
  const diagnostic = JSON.parse(JSON.stringify({
    mode: "local-v2", startedAt: "2026-11-05T00:00:00Z", completedAt: "2026-11-05T00:01:00Z",
    command: "npm run qualify:local:workspace -- --mode local-v2", source: { sha: source.sha, tree: source.tree }, boundary: null,
    evidence: { readbacks: {
      installation: { cloudImage: images.cloud.input, workspaceImage: images.workspace.input },
      stores: { ownerSeparated: true, ownerStores: Object.fromEntries(LOCAL_V2_STORE_IDENTITIES.map(([database, owner, schema]) => [database, { owner, schema, businessTables: 1 }])) },
      tenant: { id: "tenant_test" }, cloudIdentity: { actorId: "41" }, accountBinding: { tenantId: "tenant_test", accountId: "local-host" },
      order: { operationId: "operation_test", workspaceId: "workspace_test" }, workspace: { id: "workspace_test", runtimeVersionId: "runtime_version_test" },
      catalog: { runtimeVersionId: "runtime_version_test", computePlanId: "compute_test", storagePlanId: "storage_test" },
      quote: { id: "quote_test", provider: "local-docker", billingMode: "LOCAL_NO_CHARGE", totalUSDMicros: "0" },
      application: { applicationId: "fixture", revision: "1.0.0", exposurePolicy: "anonymous" },
      applicationRuntime: { runtimeId: "runtime_test", imageRef: images.workspace.input, imageId: images.workspace.runningDigest, publishedPort: 32769, providerBindingVerified: true },
      applicationEntry: { url: "http://127.0.0.1:32769/", status: 200 },
      restart: { performed: true, operationStable: true, workspaceStable: true, runtimeStable: true, receiptStable: true, sessionReestablished: true },
      deletion: { operationId: "delete_test", receiptId: "receipt_test", status: "succeeded", workspaceAbsent: true, routeAbsent: true,
        runtimeAbsent: true, resourceAbsent: true, dataAbsent: true, liveResources: 0, refundStatus: "not_applicable",
        retry: { status: 202, operationIdStable: true, ledgerReceiptsUnchanged: true, liveResources: 0 },
        owner: { workspaceId: "workspace_test", operationId: "delete_test", resourceDeletionStatus: "confirmed", dataDeletionStatus: "confirmed", refundStatus: "not_applicable",
          deletionReceipt: { id: "receipt_test", operationId: "delete_test", owner: "workspace", kind: "provider_action", outcome: "confirmed" } } },
    } },
  }));
  const processes = Object.fromEntries(LOCAL_V2_OWNER_PROCESSES.map((owner) => [owner, "ready"]));
  const residuals = { containers: 0, volumes: 0, networks: 0 };
  return { source, images, diagnostic, processes, residuals };
}

test("the native producer's receipt survives JSON decoding through the release consumer", () => {
  const input = fixture();
  const receipt = createLocalV2ReadyReceipt(input.diagnostic, input.source, input.images, input.processes, input.residuals);
  const admitted = validateLocalQualificationReceipt(JSON.parse(JSON.stringify(receipt)));
  assert.equal(admitted.kind, "opl.local-workspace-qualification.v2");
  assert.equal(admitted.qualification.p0Ready, false);
  assert.equal(admitted.billing.quoteTotalUSDMicros, "0");
});

test("the release decoder refuses incomplete, inconsistent and credential-bearing native evidence", () => {
  const input = fixture();
  const valid = createLocalV2ReadyReceipt(input.diagnostic, input.source, input.images, input.processes, input.residuals);
  const changes = [
    (value) => { value.source.clean = false; },
    (value) => { value.qualification.p0Ready = true; },
    (value) => { value.processes.workspace = "unknown"; },
    (value) => { value.stores.ownerStores.opl_gateway.owner = "opl_tenant_owner"; },
    (value) => { value.application.entryUrl = "http://127.0.0.1:32770/"; },
    (value) => { value.application.imageId = `sha256:${"0".repeat(64)}`; },
    (value) => { value.restart.runtimeStable = false; },
    (value) => { value.deletion.receiptId = "another_receipt"; },
    (value) => { value.deletion.retry.liveResources = 1; },
    (value) => { value.billing.quoteTotalUSDMicros = "1"; },
    (value) => { value.residuals.containers = 1; },
    (value) => { value.token = "must-not-enter-a-receipt"; },
  ];
  for (const change of changes) {
    const invalid = JSON.parse(JSON.stringify(valid));
    change(invalid);
    assert.throws(() => validateLocalQualificationReceipt(JSON.parse(JSON.stringify(invalid))), { code: "local_v2_ready_receipt_invalid" });
  }
});

test("a stopped run or another Workspace's deletion cannot become a native READY receipt", () => {
  for (const change of [
    (input) => { input.diagnostic.boundary = { code: "DEPENDENCY_UNAVAILABLE" }; },
    (input) => { input.diagnostic.evidence.readbacks.deletion.owner.workspaceId = "workspace_other"; },
    (input) => { input.diagnostic.source.sha = "0".repeat(40); },
  ]) {
    const input = fixture();
    change(input);
    assert.throws(() => createLocalV2ReadyReceipt(input.diagnostic, input.source, input.images, input.processes, input.residuals), { code: "local_v2_ready_receipt_invalid" });
  }
});
