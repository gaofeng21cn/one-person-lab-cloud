export const LOCAL_V2_OWNER_PROCESSES = Object.freeze([
  "tenant", "resource-catalog", "ledger", "capability", "build", "runtime-control",
  "workspace", "serve", "fabric", "control-plane", "console-bff",
]);

export const LOCAL_V2_STORE_IDENTITIES = Object.freeze([
  ["opl_control_plane", "opl_control_plane", "public"],
  ["opl_fabric", "opl_fabric_owner", "fabric"],
  ["opl_ledger", "opl_ledger", "public"],
  ["opl_tenant", "opl_tenant_owner", "tenant"],
  ["opl_gateway", "opl_gateway_owner", "gateway"],
  ["opl_resource_catalog", "opl_resource_catalog_owner", "resource_catalog"],
  ["opl_capability", "opl_capability_owner", "capability"],
  ["opl_build", "opl_build_owner", "build"],
  ["opl_runtime_control", "opl_runtime_control_owner", "runtime_control"],
  ["opl_workspace", "opl_workspace_owner", "workspace"],
  ["opl_serve", "opl_serve_owner", "serve"],
]);

type ImageEvidence = { input: string; repoDigest: string; digest: string; runningDigest: string };
export type LocalV2ReadyReceipt = {
  schemaVersion: 2;
  kind: "opl.local-workspace-qualification.v2";
  status: "READY";
  mode: "local-v2";
  startedAt: string;
  completedAt: string;
  command: string;
  source: { sha: string; tree: string; clean: boolean };
  qualification: { authorityMode: "fixture"; provider: "local-docker"; billingMode: "LOCAL_NO_CHARGE"; p0Ready: false };
  images: { cloud: ImageEvidence; workspace: ImageEvidence };
  processes: Record<string, string>;
  stores: { ownerSeparated: boolean; ownerStores: Record<string, { owner: string; schema: string; businessTables: number }> };
  identities: Record<string, string>;
  application: { applicationId: string; revision: string; exposurePolicy: string; runtimeId: string; entryUrl: string; publishedPort: number; httpStatus: number; imageRef: string; imageId: string; providerBindingVerified: boolean };
  restart: { performed: boolean; operationStable: boolean; workspaceStable: boolean; runtimeStable: boolean; receiptStable: boolean; sessionReestablished: boolean };
  deletion: { operationId: string; receiptId: string; workspaceAbsent: boolean; routeAbsent: boolean; runtimeAbsent: boolean; resourceAbsent: boolean; dataAbsent: boolean; resourceDeletionStatus: string; dataDeletionStatus: string; refundStatus: string; liveResources: number; retry: { status: number; operationIdStable: boolean; ledgerReceiptsUnchanged: boolean; liveResources: number } };
  billing: { quoteId: string; quoteTotalUSDMicros: string; refundStatus: string };
  residuals: { containers: number; volumes: number; networks: number };
};

const SHA = /^[0-9a-f]{40}$/;
const DIGEST = /^sha256:[0-9a-f]{64}$/;
const IDENTITIES = ["tenantId", "actorId", "providerAccountId", "workspaceId", "operationId", "runtimeId", "runtimeVersionId", "computePlanId", "storagePlanId", "deleteOperationId", "deletionReceiptId"];
function fail() { throw Object.assign(new Error("native Local-Docker READY evidence is incomplete or mismatched"), { code: "local_v2_ready_receipt_invalid" }); }
function exact(value: unknown, keys: string[]) {
  if (!value || typeof value !== "object" || Array.isArray(value) || JSON.stringify(Object.keys(value).sort()) !== JSON.stringify([...keys].sort())) fail();
}
function strings(value: object, keys: readonly string[]) {
  const fields = value as Record<string, unknown>;
  if (keys.some((key) => typeof fields?.[key] !== "string" || !String(fields[key]).trim())) fail();
}

export function validateLocalV2ReadyReceipt(value: unknown): LocalV2ReadyReceipt {
  exact(value, ["schemaVersion", "kind", "status", "mode", "startedAt", "completedAt", "command", "source", "qualification", "images", "processes", "stores", "identities", "application", "restart", "deletion", "billing", "residuals"]);
  const receipt = value as LocalV2ReadyReceipt;
  if (receipt.schemaVersion !== 2 || receipt.kind !== "opl.local-workspace-qualification.v2" || receipt.status !== "READY" || receipt.mode !== "local-v2") fail();
  strings(receipt, ["command", "startedAt", "completedAt"]);
  if (!Number.isFinite(Date.parse(receipt.startedAt)) || !Number.isFinite(Date.parse(receipt.completedAt)) || Date.parse(receipt.completedAt) < Date.parse(receipt.startedAt)) fail();
  exact(receipt.source, ["sha", "tree", "clean"]);
  if (!SHA.test(receipt.source.sha) || !SHA.test(receipt.source.tree) || receipt.source.clean !== true) fail();
  exact(receipt.qualification, ["authorityMode", "provider", "billingMode", "p0Ready"]);
  if (receipt.qualification.authorityMode !== "fixture" || receipt.qualification.provider !== "local-docker" || receipt.qualification.billingMode !== "LOCAL_NO_CHARGE" || receipt.qualification.p0Ready !== false) fail();
  exact(receipt.images, ["cloud", "workspace"]);
  for (const image of Object.values(receipt.images)) {
    exact(image, ["input", "repoDigest", "digest", "runningDigest"]);
    if (!/^[^\s@]+@sha256:[0-9a-f]{64}$/.test(image.input) || image.repoDigest !== image.input || image.digest !== image.input.split("@")[1] || !DIGEST.test(image.runningDigest)) fail();
  }
  exact(receipt.processes, [...LOCAL_V2_OWNER_PROCESSES]);
  if (Object.values(receipt.processes).some((status) => status !== "ready")) fail();
  exact(receipt.stores, ["ownerSeparated", "ownerStores"]);
  exact(receipt.stores.ownerStores, LOCAL_V2_STORE_IDENTITIES.map(([database]) => database));
  if (receipt.stores.ownerSeparated !== true) fail();
  for (const [database, owner, schema] of LOCAL_V2_STORE_IDENTITIES) {
    const store = receipt.stores.ownerStores[database];
    exact(store, ["owner", "schema", "businessTables"]);
    if (store.owner !== owner || store.schema !== schema || !Number.isSafeInteger(store.businessTables) || store.businessTables < 1) fail();
  }
  exact(receipt.identities, IDENTITIES);
  strings(receipt.identities, IDENTITIES);
  exact(receipt.application, ["applicationId", "revision", "exposurePolicy", "runtimeId", "entryUrl", "publishedPort", "httpStatus", "imageRef", "imageId", "providerBindingVerified"]);
  strings(receipt.application, ["applicationId", "revision"]);
  if (receipt.application.exposurePolicy !== "anonymous" || receipt.application.runtimeId !== receipt.identities.runtimeId ||
      !/^http:\/\/127\.0\.0\.1:([1-9][0-9]{0,4})\/$/.test(receipt.application.entryUrl) || Number(new URL(receipt.application.entryUrl).port) > 65535 ||
      !Number.isSafeInteger(receipt.application.publishedPort) || String(receipt.application.publishedPort) !== new URL(receipt.application.entryUrl).port || receipt.application.httpStatus !== 200 ||
      receipt.application.imageRef !== receipt.images.workspace.input || receipt.application.imageId !== receipt.images.workspace.runningDigest || receipt.application.providerBindingVerified !== true) fail();
  const restartKeys = ["performed", "operationStable", "workspaceStable", "runtimeStable", "receiptStable", "sessionReestablished"] as const;
  exact(receipt.restart, [...restartKeys]);
  if (restartKeys.some((key) => receipt.restart[key] !== true)) fail();
  exact(receipt.deletion, ["operationId", "receiptId", "workspaceAbsent", "routeAbsent", "runtimeAbsent", "resourceAbsent", "dataAbsent", "resourceDeletionStatus", "dataDeletionStatus", "refundStatus", "liveResources", "retry"]);
  if (receipt.deletion.operationId !== receipt.identities.deleteOperationId || receipt.deletion.receiptId !== receipt.identities.deletionReceiptId ||
      (["workspaceAbsent", "routeAbsent", "runtimeAbsent", "resourceAbsent", "dataAbsent"] as const).some((key) => receipt.deletion[key] !== true) ||
      receipt.deletion.resourceDeletionStatus !== "confirmed" || receipt.deletion.dataDeletionStatus !== "confirmed" || receipt.deletion.refundStatus !== "not_applicable" || receipt.deletion.liveResources !== 0) fail();
  exact(receipt.deletion.retry, ["status", "operationIdStable", "ledgerReceiptsUnchanged", "liveResources"]);
  if (![200, 202].includes(receipt.deletion.retry.status) || receipt.deletion.retry.operationIdStable !== true || receipt.deletion.retry.ledgerReceiptsUnchanged !== true || receipt.deletion.retry.liveResources !== 0) fail();
  exact(receipt.billing, ["quoteId", "quoteTotalUSDMicros", "refundStatus"]);
  strings(receipt.billing, ["quoteId"]);
  if (receipt.billing.quoteTotalUSDMicros !== "0" || receipt.billing.refundStatus !== "not_applicable") fail();
  exact(receipt.residuals, ["containers", "volumes", "networks"]);
  if (Object.values(receipt.residuals).some((count) => count !== 0)) fail();
  return receipt;
}

export function createLocalV2ReadyReceipt(diagnostic: Record<string, any>, source: LocalV2ReadyReceipt["source"], images: LocalV2ReadyReceipt["images"], processes: LocalV2ReadyReceipt["processes"], residuals: LocalV2ReadyReceipt["residuals"]): LocalV2ReadyReceipt {
  const readbacks = diagnostic.evidence?.readbacks;
  const order = readbacks?.order;
  const deletion = readbacks?.deletion;
  const owner = deletion?.owner;
  const application = readbacks?.applicationRuntime;
  if (!order || !deletion || !owner || diagnostic.mode !== "local-v2" || diagnostic.boundary !== null || diagnostic.source?.sha !== source.sha || diagnostic.source?.tree !== source.tree ||
      readbacks?.quote?.provider !== "local-docker" || readbacks?.quote?.billingMode !== "LOCAL_NO_CHARGE" ||
      images.cloud.input !== readbacks?.installation?.cloudImage || images.workspace.input !== readbacks?.installation?.workspaceImage ||
      readbacks?.accountBinding?.tenantId !== readbacks?.tenant?.id || readbacks?.workspace?.runtimeVersionId !== readbacks?.catalog?.runtimeVersionId ||
      readbacks?.workspace?.id !== order?.workspaceId || owner?.workspaceId !== order?.workspaceId || owner?.operationId !== deletion?.operationId ||
      owner?.deletionReceipt?.id !== deletion?.receiptId || owner?.deletionReceipt?.operationId !== deletion?.operationId || owner?.deletionReceipt?.owner !== "workspace" ||
      owner?.deletionReceipt?.kind !== "provider_action" || owner?.deletionReceipt?.outcome !== "confirmed" || deletion?.status !== "succeeded") fail();
  return validateLocalV2ReadyReceipt({
    schemaVersion: 2, kind: "opl.local-workspace-qualification.v2", status: "READY", mode: "local-v2",
    startedAt: diagnostic.startedAt, completedAt: diagnostic.completedAt, command: diagnostic.command, source, images, processes,
    qualification: { authorityMode: "fixture", provider: "local-docker", billingMode: "LOCAL_NO_CHARGE", p0Ready: false },
    stores: readbacks.stores,
    identities: { tenantId: readbacks.tenant?.id, actorId: readbacks.cloudIdentity?.actorId, providerAccountId: readbacks.accountBinding?.accountId,
      workspaceId: order.workspaceId, operationId: order.operationId, runtimeId: application?.runtimeId,
      runtimeVersionId: readbacks.catalog?.runtimeVersionId, computePlanId: readbacks.catalog?.computePlanId, storagePlanId: readbacks.catalog?.storagePlanId,
      deleteOperationId: deletion.operationId, deletionReceiptId: deletion.receiptId },
    application: { applicationId: readbacks.application?.applicationId, revision: readbacks.application?.revision,
      exposurePolicy: readbacks.application?.exposurePolicy, runtimeId: application?.runtimeId,
      entryUrl: readbacks.applicationEntry?.url, publishedPort: application?.publishedPort, httpStatus: readbacks.applicationEntry?.status,
      imageRef: application?.imageRef, imageId: application?.imageId, providerBindingVerified: application?.providerBindingVerified },
    restart: Object.fromEntries(["performed", "operationStable", "workspaceStable", "runtimeStable", "receiptStable", "sessionReestablished"].map((key) => [key, readbacks.restart?.[key]])),
    deletion: { operationId: deletion.operationId, receiptId: deletion.receiptId,
      workspaceAbsent: deletion.workspaceAbsent, routeAbsent: deletion.routeAbsent, runtimeAbsent: deletion.runtimeAbsent,
      resourceAbsent: deletion.resourceAbsent, dataAbsent: deletion.dataAbsent,
      resourceDeletionStatus: owner.resourceDeletionStatus, dataDeletionStatus: owner.dataDeletionStatus,
      refundStatus: owner.refundStatus, liveResources: deletion.liveResources, retry: deletion.retry },
    billing: { quoteId: readbacks.quote.id, quoteTotalUSDMicros: readbacks.quote.totalUSDMicros, refundStatus: deletion.refundStatus }, residuals,
  });
}
