import { spawn } from "node:child_process";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { constants, existsSync, readFileSync } from "node:fs";
import { lstat, mkdir, mkdtemp, open, readFile, readdir, realpath, rename, rm, stat, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { request as httpsRequest } from "node:https";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseDocument } from "yaml";

// The purchase sequence is the invariant and is owned by one module. Everything
// else in this file either composes it or asserts a result, and neither of those
// belongs to the purchase itself.
import {
  priceWorkspaceQuote, quoteWorkspaceOrder, listWorkspaceTransactions, readWorkspacePurchaseEvidence, stableID, submitWorkspaceOrder, workspaceDeclaration,
} from "./workspace-purchase-core.ts";

import {
  createLocalJ1RecoveryArtifact,
  createLocalJ1RecoveryReadbackPendingArtifact,
  localJ1CleanupPlan,
  localJ1RecoveryArtifactPath,
  noLocalJ1ExternalWrites,
  writeLocalJ1RecoveryArtifact
} from "./local-workspace-recovery.ts";
import {
  LEGACY_QUALIFICATION_J0_READY_KINDS,
  createJ0ReadyReceiptValidator,
  j0ReadyReceiptError,
  qualificationProviderProfile
} from "./workspace-qualification-contract.ts";
// The installation declaration is one fact with one owner. This driver reads it
// through the module that derives and validates it against the template and the
// Compose file, so it never keeps a second variable list of its own.
import { localV2InstallationFacts } from "./local-v2-installation-facts.ts";
import {
  LOCAL_V2_OWNER_PROCESSES, LOCAL_V2_STORE_IDENTITIES, createLocalV2ReadyReceipt, validateLocalV2ReadyReceipt,
} from "./local-v2-qualification-receipt.ts";

// Local-Docker is the first adapter over the provider-neutral contract. Its
// identity is declared here rather than asserted inside the validator, which is
// what lets a second provider reuse the same seam.
export const LOCAL_DOCKER_QUALIFICATION_PROFILE = qualificationProviderProfile("local-docker", {
  legacyReceiptKinds: LEGACY_QUALIFICATION_J0_READY_KINDS
});

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
// The v2 installation is one Compose file: its owners, its proxy, its fixture
// authority and its PKI are all declared there, so a v2 run starts exactly this
// file and no retained overlay.
export const LOCAL_V2_QUALIFICATION_COMPOSE = "deploy/portable/compose.local-v2.yaml";

// The entry of a Workspace the customer package chain bought.
//
// The entry may be published over plain HTTP or served by the installation's
// own origin with its own certificate, so the request follows the URL it is
// given and the certificate the installation declares.
// The uploaded bytes go to the installation's own object origin, signed by the
// installation's CA. A request that trusted the host store instead would fail
// there, so the part is written through the same trust anchor as every other
// request of the run.
export function customerUploadRequest(url, init = {}, caFile = "") {
  const ca = caFile ? readFileSync(caFile) : null;
  return caRequest(url, init, new Headers(init.headers || {}), init.body, ca);
}

async function customerEntryRequest(url, headers, caFile) {
  const target = new URL(url);
  if (target.protocol === "https:") {
    const response = await caRequest(url, { method: "GET" }, new Headers(headers || {}), undefined, caFile ? readFileSync(caFile) : undefined);
    return { status: response.status, text: await response.text() };
  }
  const response = await fetch(url, { method: "GET", headers: headers || {}, redirect: "manual" });
  return { status: response.status, text: await response.text() };
}

// The declaration template the installation is validated against: the file that
// names every variable the Compose file requires.
export const LOCAL_V2_INSTALLATION_TEMPLATE = "deploy/portable/opl-cloud.env.example";

// The driver runs one installation mode at a time, and the mode owns which
// Compose files it starts, which endpoints it may call and which authority it
// authenticates against. The mode defaults to local-v2; nothing below infers
// another from a partially configured environment, because a run that mixed the v2
// owners with a retained Control Plane writer would price and charge a purchase
// in the wrong authority.
export const QUALIFICATION_MODE_LOCAL_V2 = "local-v2";
// The version-one fixture and live runs are retired from this tool; local-v2
// is its only mode. Their names stay so a caller that
// still asks for one is told which mode it asked for, and that it is gone.
export const QUALIFICATION_MODE_LEGACY_FIXTURE = "legacy-fixture";
export const QUALIFICATION_MODE_LEGACY_LIVE = "legacy-live";

// Every owner in the v2 installation keeps its own database and role. One
// database per owner is what makes a receipt, a resource set and an order
// separately readable, so the v2 run asserts the whole set.
export const LOCAL_V2_OWNER_STORES = Object.freeze(LOCAL_V2_STORE_IDENTITIES.map(([database, owner]) => [database, owner]));

const LOCAL_V2_OWNER_SCHEMAS = Object.freeze(Object.fromEntries(LOCAL_V2_STORE_IDENTITIES.map(([database, , schema]) => [database, schema])));

// The owner processes a v2 qualification reaches through the same origin.
export const LOCAL_V2_QUALIFICATION_SERVICES = Object.freeze([
  "postgres", "sub2api-authority", ...LOCAL_V2_OWNER_PROCESSES, "cloud-local-v2"
]);

// The modes this tool offers. The retired version-one runs are named above so
// they can be refused by name, and they are not offered here.
export const QUALIFICATION_MODES = Object.freeze([QUALIFICATION_MODE_LOCAL_V2]);

// The retained Control Plane routes. The v2 installation does not serve them,
// and a v2 run that called one would be writing through the migration source
// instead of the owner that now owns the fact.
export const RETAINED_LEGACY_ENDPOINT_PREFIXES = Object.freeze([
  "/api/auth/", "/api/operator/", "/api/gateway/", "/api/v1/"
]);

const LOCAL_V2_ENDPOINTS = Object.freeze({
  authContext: "/api/v2/auth/context",
  authLogin: "/api/v2/auth/login",
  tenant: "/api/v2/tenant",
  createTenant: "/api/v2/admin/tenants",
  publisherNamespaces: "/api/v2/admin/catalog/publisher-namespaces",
  runtimeVersions: "/api/v2/admin/catalog/runtime-versions",
  computePlans: "/api/v2/admin/catalog/compute-plans",
  storagePlans: "/api/v2/admin/catalog/storage-plans",
  retentionPolicies: "/api/v2/admin/catalog/retention-policies",
  refundPolicies: "/api/v2/admin/catalog/refund-policies",
  pricePolicies: "/api/v2/admin/catalog/price-policies",
  catalogRuntimeVersions: "/api/v2/catalog/runtime-versions",
  catalogComputePlans: "/api/v2/catalog/compute-plans",
  catalogStoragePlans: "/api/v2/catalog/storage-plans",
  quotes: "/api/v2/quotes",
  workspaces: "/api/v2/workspaces",
  operations: "/api/v2/operations/workspace",
  wallet: "/api/v2/wallet"
});

// A retired mode is refused by name, and an unknown one is refused as unknown.
// The two answers are different facts: the first tells the caller which run it
// asked for and that the run is gone, the second tells it what it may ask for.
function retiredQualificationModeError(mode) {
  const name = String(mode || "").trim();
  if (name === QUALIFICATION_MODE_LEGACY_FIXTURE || name === QUALIFICATION_MODE_LEGACY_LIVE) {
    return namedQualificationError("local_qualification_mode_retired",
      `qualification mode ${name} is retired; this tool supports only local-v2`);
  }
  return namedQualificationError("local_qualification_mode_invalid",
    `qualification mode must be one of ${QUALIFICATION_MODES.join(", ")}`);
}

// One binding per mode: the routes that mode may call and the Compose files it
// starts. A caller asks for a mode and receives the whole binding, so no call
// site chooses an endpoint independently of the mode it selected.
export function qualificationModeBinding(mode) {
  if (mode === QUALIFICATION_MODE_LOCAL_V2) {
    return {
      mode,
      composeFiles: [LOCAL_V2_QUALIFICATION_COMPOSE],
      installationEnvRequired: true,
      endpoints: LOCAL_V2_ENDPOINTS,
      allowedPrefixes: ["/api/v2/"],
      allowedPaths: ["/healthz"],
      retainedPrefixes: RETAINED_LEGACY_ENDPOINT_PREFIXES
    };
  }
  throw retiredQualificationModeError(mode);
}

// Whether a path may be requested in this mode. The v2 installation is checked
// against its own prefix first, so a retained route is refused as a mode
// violation rather than surfacing later as an unexpected 404 from an owner that
// never served it.
export function qualificationPathAllowed(mode, path) {
  const requested = String(path || "");
  const binding = qualificationModeBinding(mode);
  if (mode === QUALIFICATION_MODE_LOCAL_V2 &&
    binding.retainedPrefixes.some((prefix) => requested.startsWith(prefix))) {
    return false;
  }
  if (binding.allowedPaths.includes(requested)) return true;
  return binding.allowedPrefixes.some((prefix) => prefix.endsWith("/") ? requested.startsWith(prefix) : requested === prefix);
}

function assertQualificationPath(mode, path) {
  if (!mode || qualificationPathAllowed(mode, path)) return;
  throw namedQualificationError(
    "local_v2_retained_endpoint_refused",
    `${mode} must not request ${String(path || "").split("?")[0]}: it is a retained Control Plane route`
  );
}
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const shaPattern = /^[0-9a-f]{40}$/;
const deferredCloudGates = Object.freeze([
  "tencent-tke",
  "tcr-and-tke-image-pull",
  "kubernetes-secret-and-pod-readiness",
  "production-sub2api",
  "production-secrets-and-network",
  "instance-deployment-and-rollback"
]);
export function immutableImageDigest(value) {
  const normalized = String(value || "").trim();
  if (digestPattern.test(normalized)) return normalized;
  const marker = normalized.lastIndexOf("@sha256:");
  if (marker <= 0) return "";
  const digest = normalized.slice(marker + 1);
  return digestPattern.test(digest) ? digest : "";
}

function immutableImageReference(value) {
  const normalized = String(value || "").trim();
  return /^(?:[^\s@]+)@sha256:[0-9a-f]{64}$/.test(normalized) ? normalized : "";
}

function optionValues(args) {
  const values = new Map();
  let buildSourceImages = false;
  for (let index = 0; index < args.length; index += 1) {
    const token = args[index];
    if (token === "--build-source-images") {
      buildSourceImages = true;
      continue;
    }
    if (!["--source-sha", "--cloud-image", "--workspace-image", "--receipt", "--authority-mode", "--j0-ready-receipt", "--sub2api-secret-file", "--mode", "--installation-env", "--application-contract", "--project", "--workspace-name", "--customer-package-dir", "--customer-package-archive", "--customer-probe-path", "--customer-package-name", "--customer-version-label", "--customer-expected-evidence-level"].includes(token)) {
      throw new Error(`unknown local qualification argument: ${token}`);
    }
    const value = args[index + 1];
    if (!value || value.startsWith("--") || values.has(token)) {
      throw new Error(`local qualification argument ${token} requires one value`);
    }
    values.set(token, value);
    index += 1;
  }
  return { values, buildSourceImages };
}

export function parseLocalQualificationArgs(args = process.argv.slice(2)) {
  const { values, buildSourceImages } = optionValues(args);
  const mode = String(values.get("--mode") || QUALIFICATION_MODE_LOCAL_V2).trim();
  if (!QUALIFICATION_MODES.includes(mode)) throw retiredQualificationModeError(mode);
  const sourceSha = String(values.get("--source-sha") || "").trim();
  const installationEnv = String(values.get("--installation-env") || "").trim();
  const applicationContract = String(values.get("--application-contract") || "").trim();
  const project = String(values.get("--project") || "").trim();
  const workspaceName = String(values.get("--workspace-name") || "").trim();
  const cloudImage = String(values.get("--cloud-image") || "").trim();
  const workspaceImage = String(values.get("--workspace-image") || "").trim();
  const receiptPath = String(values.get("--receipt") || "").trim();
  if (!shaPattern.test(sourceSha)) throw new Error("source SHA must be an exact 40-character lowercase commit");
  if (!receiptPath) throw new Error("receipt path is required");
  // The retired version-one options belonged to the retired modes. They are
  // refused by name, so a caller that still passes one is told which option is
  // gone rather than that its argument was never known.
  for (const retired of ["--authority-mode", "--j0-ready-receipt", "--sub2api-secret-file"]) {
    if (values.has(retired)) {
      throw namedQualificationError("local_qualification_option_retired",
        `${retired} belonged to the retired version-one qualification and is gone`);
    }
  }
  // A run takes its images, secrets, ports and provider facts from the
  // declared installation and nothing else, so a second spelling of any of
  // them is refused instead of silently overriding the declaration.
  if (!installationEnv) throw namedQualificationError("local_v2_installation_env_required", "--installation-env must name the declared installation environment file");
  if (!isAbsolute(installationEnv)) throw namedQualificationError("local_v2_installation_env_invalid", "--installation-env must be an absolute path");
  if (!applicationContract) throw namedQualificationError("local_v2_application_contract_required", "--application-contract must name the approved Runtime Publisher Contract file");
  if (!isAbsolute(applicationContract)) throw namedQualificationError("local_v2_application_contract_invalid", "--application-contract must be an absolute path");
  if (!project) throw namedQualificationError("local_v2_project_required", "--project must name the Compose project of the declared installation");
  if (cloudImage || workspaceImage) throw namedQualificationError("local_v2_images_are_declared", "the installation declaration owns OPL_CLOUD_IMAGE and OPL_WORKSPACE_IMAGE");
  if (buildSourceImages) throw namedQualificationError("local_v2_images_are_declared", "the installation declaration owns OPL_CLOUD_IMAGE and OPL_WORKSPACE_IMAGE");
  // The customer package is an input of the run and not a fact this tool owns:
  // which package is uploaded, where its probe sits and which evidence level
  // the caller asked for all travel with the declaration.
  const customer = customerEvidencePlan({
    packageDir: String(values.get("--customer-package-dir") || "").trim(),
    packageArchive: String(values.get("--customer-package-archive") || "").trim(),
    probePath: String(values.get("--customer-probe-path") || "").trim(),
    expectedEvidenceLevel: String(values.get("--customer-expected-evidence-level") || "").trim(),
  });
  if (customer.declared && customer.mode === "directory" && !isAbsolute(customer.packageDir)) {
    throw namedQualificationError("local_v2_customer_package_dir_invalid", "--customer-package-dir must be an absolute path");
  }
  if (customer.declared && customer.mode === "archive" && !isAbsolute(customer.packageArchive)) {
    throw namedQualificationError("local_v2_customer_package_archive_invalid", "--customer-package-archive must be an absolute path");
  }
  const customerPackageName = String(values.get("--customer-package-name") || "").trim();
  const customerVersionLabel = String(values.get("--customer-version-label") || "").trim();
  // A run that did not declare a customer package keeps the shape it had: the
  // customer keys appear only when the caller asked for the chain.
  if (!customer.declared) {
    return { mode, sourceSha, receiptPath, installationEnv, applicationContract, project, workspaceName };
  }
  return { mode, sourceSha, receiptPath, installationEnv, applicationContract, project, workspaceName, customer, customerPackageName, customerVersionLabel };
}

function namedQualificationError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

function requireString(value, label) {
  if (!String(value || "").trim()) throw new Error(`${label} is required`);
}

export function redactedError(error) {
  return String(error instanceof Error ? error.message : error)
    .replace(/([a-z][a-z0-9+.-]*:\/\/)[^\s/@:]+(?::[^\s/@]*)?@/gi, "$1[redacted]@")
    .replace(/([a-z][a-z0-9+.-]*:\/\/[^\s?#]+)\?[^\s#]*/gi, "$1?[redacted]")
    .replace(/([a-z][a-z0-9+.-]*:\/\/[^\s#]+)#[^\s]*/gi, "$1#[redacted]")
    .replace(/[\r\n]+/g, " ")
    .slice(0, 1000);
}

function exactQualificationSourceIdentity(value) {
  return value && shaPattern.test(String(value.sha || "")) && shaPattern.test(String(value.tree || "")) && value.clean === true;
}

export function validateQualificationSourceIdentity(before, after, requestedSourceSha) {
  if (!exactQualificationSourceIdentity(before) || !exactQualificationSourceIdentity(after)) {
    throw new Error("local qualification requires a clean exact source identity");
  }
  if (before.sha !== requestedSourceSha || after.sha !== requestedSourceSha) {
    throw new Error("local qualification HEAD does not equal the requested source SHA");
  }
  if (before.sha !== after.sha || before.tree !== after.tree) {
    throw new Error("local qualification source changed while qualification was running");
  }
  return after;
}

export const validateJ0ReadyReceipt = createJ0ReadyReceiptValidator(LOCAL_DOCKER_QUALIFICATION_PROFILE);

export async function loadJ0ReadyReceipt(path, sourceSha, sourceTree, { currentUid = typeof process.getuid === "function" ? process.getuid() : null } = {}) {
  const exactPath = resolve(String(path || ""));
  if (!isAbsolute(String(path || "")) || !Number.isSafeInteger(currentUid) || currentUid < 0) {
    throw j0ReadyReceiptError();
  }
  let handle;
  try {
    handle = await open(exactPath, constants.O_RDONLY | constants.O_NOFOLLOW);
    const stat = await handle.stat();
    const canonicalPath = await realpath(exactPath);
    if (canonicalPath === root || canonicalPath.startsWith(`${root}${sep}`) || !stat.isFile() ||
      (stat.mode & 0o777) !== 0o600 || stat.uid !== currentUid || stat.size <= 0 || stat.size > 1024 * 1024) {
      throw j0ReadyReceiptError();
    }
    const raw = await handle.readFile();
    const value = validateJ0ReadyReceipt(JSON.parse(raw.toString("utf8")), sourceSha, sourceTree);
    return {
      digest: `sha256:${createHash("sha256").update(raw).digest("hex")}`,
      source: { sha: value.source.sha, tree: value.source.tree, clean: true },
      authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
      provider: LOCAL_DOCKER_QUALIFICATION_PROFILE.provider,
      failed: 0,
      skipped: 0
    };
  } catch (error) {
    if (error?.code === "j0_ready_receipt_invalid") throw error;
    throw j0ReadyReceiptError();
  } finally {
    await handle?.close();
  }
}

export function validateLocalQualificationReceipt(value) {
  if (value?.schemaVersion === 2) return validateLocalV2ReadyReceipt(value);
  if (!value || value.schemaVersion !== 1 || value.status !== "READY") throw new Error("READY receipt is required");
  if (!shaPattern.test(String(value.source?.sha || "")) || !shaPattern.test(String(value.source?.tree || ""))) {
    throw new Error("source identity is invalid");
  }
  for (const name of ["cloud", "workspace"]) {
    const image = value.images?.[name];
    if (!immutableImageReference(image?.input) || image?.repoDigest !== image?.input || !digestPattern.test(String(image?.digest || "")) || !digestPattern.test(String(image?.runningDigest || ""))) {
      throw new Error(`${name} image identity is invalid`);
    }
  }
  requireString(value.command, "qualification command");
  if (!value.qualification || !["fixture", "live"].includes(value.qualification.authorityMode) ||
    value.qualification.p0Ready !== (value.qualification.authorityMode === "live" && value.j0Ready?.failed === 0 && value.j0Ready?.skipped === 0)) {
    throw new Error("qualification authority classification is invalid");
  }
  if (value.qualification.authorityMode === "live" && (!digestPattern.test(String(value.j0Ready?.digest || "")) || value.j0Ready?.source?.sha !== value.source.sha ||
    value.j0Ready?.source?.tree !== value.source.tree || value.j0Ready?.source?.clean !== true ||
    value.j0Ready?.provider !== LOCAL_DOCKER_QUALIFICATION_PROFILE.provider || value.j0Ready?.authority?.mode !== "live" ||
    value.j0Ready?.authority?.class !== "sandbox" || value.j0Ready?.authority?.dedicated !== true ||
    value.j0Ready?.authority?.confirmed !== true || value.j0Ready?.failed !== 0 || value.j0Ready?.skipped !== 0)) {
    throw new Error("live qualification requires the exact J0 READY receipt binding");
  }
  for (const name of ["console", "controlPlane", "fabric", "ledger"]) {
    if (value.processes?.[name] !== "ready") throw new Error(`${name} process is not ready`);
  }
  if (value.stores?.ownerSeparated !== true || ["controlPlane", "fabric", "ledger"].some((name) => value.stores?.[name] !== "durable")) {
    throw new Error("durable owner-separated stores are required");
  }
  const live = value.qualification.authorityMode === "live";
  const identityKeys = [
    "accountId", "sub2apiUserId", "operationId", "workspaceId", "runtimeId", "keyId", "debitCode", "purchaseReceiptId",
    "runtimeVersionId", "computePlanId", "storagePlanId"
  ];
  if (!live) identityKeys.push("deleteOperationId", "deletionReceiptId");
  for (const key of identityKeys) {
    requireString(value.identities?.[key], `identity ${key}`);
  }
  if (value.debit?.count !== 1 || !String(value.debit?.code || "").startsWith("opl:") ||
    value.debit?.accountId !== value.identities.accountId || value.debit?.code !== value.identities.debitCode || value.debit?.operationId !== value.identities.operationId ||
    value.debit?.workspaceId !== value.identities.workspaceId ||
    String(value.debit?.userId || "") !== String(value.identities.sub2apiUserId) || !/^[1-9][0-9]*$/.test(String(value.debit?.amountUsdMicros || ""))) {
    throw new Error("exact debit evidence is invalid");
  }
  const walletKeys = live ? ["beforeUsdMicros", "afterUsdMicros"] : ["beforeUsdMicros", "afterUsdMicros", "afterDeleteUsdMicros"];
  if (!walletKeys.every((key) => /^\d+$/.test(String(value.wallet?.[key] || "")))) {
    throw new Error("wallet readback is invalid");
  }
  if (value.qualification.authorityMode === "fixture" &&
    (BigInt(value.wallet.beforeUsdMicros) - BigInt(value.debit.amountUsdMicros) !== BigInt(value.wallet.afterUsdMicros) ||
      BigInt(value.wallet.afterDeleteUsdMicros) !== BigInt(value.wallet.afterUsdMicros) + BigInt(value.refund?.amountUsdMicros || "0"))) {
    throw new Error("isolated fixture wallet readback does not equal the exact debit and its platform refund");
  }
  // The purchase receipt binds exactly the resources this Workspace order
  // delivered. The Runtime and the Workspace Key belong to the application
  // installation and are bound by its own evidence block.
  // The owner's receipt binds the exact purchase: the receipt, the accepted
  // quote, the order, the Workspace and the charge. The owner publishes the
  // resource allocation identities only when its typed receipt carries them, so
  // they are either all present or all absent; a partial set is a mismatch and
  // is never completed from a foreign source.
  const fulfillmentIds = ["computeAllocationId", "storageId", "attachmentId"].map((key) => String(value.receipt?.[key] || "").trim());
  if (value.receipt?.count !== 1 || value.receipt?.id !== value.identities.purchaseReceiptId ||
    value.receipt?.accountId !== value.identities.accountId || value.receipt?.operationId !== value.identities.operationId ||
    value.receipt?.workspaceId !== value.identities.workspaceId || !String(value.receipt?.quoteId || "").trim() ||
    value.receipt?.runtimeId !== undefined || value.receipt?.keyId !== undefined ||
    fulfillmentIds.some((id) => id !== "") && fulfillmentIds.some((id) => id === "") ||
    value.receipt?.chargeReference !== value.identities.debitCode || String(value.receipt?.amountUsdMicros || "") !== String(value.debit.amountUsdMicros)) {
    throw new Error("receipt binding is invalid");
  }
  if (value.application?.status !== "ready" || !String(value.application?.operationId || "").trim() ||
    !String(value.application?.applicationId || "").trim() || !String(value.application?.revision || "").trim() ||
    localApplicationEntryPort(value.application?.entryUrl) === 0 ||
    value.application?.runtimeId !== value.identities.runtimeId ||
    String(value.application?.keyId || "") !== String(value.identities.keyId) ||
    typeof value.application?.resumed !== "boolean") {
    throw new Error("application installation binding is invalid");
  }
  if (live) {
    if (value.restart?.performed !== false || value.deletion?.performed !== false || value.deletion?.mode !== "qualification_owned_cleanup") {
      throw new Error("live qualification must not perform restart or owner deletion");
    }
  } else {
    if (!value.restart?.performed || ["operationStable", "workspaceStable", "runtimeStable", "receiptStable"].some((key) => value.restart?.[key] !== true)) {
      throw new Error("restart continuity is invalid");
    }
    // A resource-only purchase's v2 Delete binds its Launch and Launch Receipt,
    // never the application installation's Runtime or Workspace Key: the Runtime
    // is retired with its application generation and the Gateway Key stays
    // retained in the authority.
    if (value.deletion?.ownerAuthorized !== true || value.deletion?.workspaceAbsent !== true || value.deletion?.runtimeAbsent !== true ||
      value.deletion?.workspaceKeyRetained !== true || value.deletion?.fabricSecretAbsent !== true ||
      value.deletion?.accountId !== value.identities.accountId || value.deletion?.operationId !== value.identities.deleteOperationId ||
      value.deletion?.deletionReceiptId !== value.identities.deletionReceiptId || value.deletion?.workspaceId !== value.identities.workspaceId ||
      String(value.deletion?.runtimeId) !== "" || String(value.deletion?.workspaceApiKeyId) !== "") {
      throw new Error("owner deletion evidence is invalid");
    }
  }
  if (live) {
    if (value.deletionReceipt?.count !== 0) throw new Error("live qualification must not record a deletion receipt");
  } else if (value.deletionReceipt?.count !== 1 || value.deletionReceipt?.id !== value.identities.deletionReceiptId ||
    value.deletionReceipt?.kind !== "provider_action" || value.deletionReceipt?.owner !== "workspace" ||
    value.deletionReceipt?.outcome !== "confirmed" || value.deletionReceipt?.operationId !== value.identities.deleteOperationId ||
    value.deletionReceipt?.workspaceId !== value.identities.workspaceId ||
    !Number.isFinite(Date.parse(value.deletionReceipt?.createdAt))) {
    throw new Error("deletion receipt binding is invalid");
  }
  if (["containers", "volumes", "networks"].some((key) => value.residuals?.[key] !== 0)) {
    throw new Error("exact-labelled residual evidence is invalid");
  }
  // Delete disables the Workspace Key while retaining its external identity;
  // it never deletes a Key from the authority inventory.
  // The fixture lane performs the owner Delete, so the owner also refunds the
  // deleted Workspace through Gateway; the live lane does not delete at all.
  const expectedAuthorityWrites = live
    ? { keyCreates: 1, keyDeletes: 0, debits: 1, refunds: 0 }
    : { keyCreates: 1, keyDeletes: 0, debits: 1, refunds: 1 };
  if (Object.entries(expectedAuthorityWrites).some(([key, count]) => value.authorityWriteCounts?.[key] !== count)) {
    throw new Error("qualification authority write counts are invalid");
  }
  const expectedMutationCounts = live
    ? { accountProvisionPosts: 0, workspaceQuotePosts: 1, workspaceOrderPosts: 1, workspaceDeleteRequests: 0, refundPosts: 0 }
    : { workspaceQuotePosts: 1, workspaceOrderPosts: 1, workspaceDeleteRequests: 1, refundPosts: 0 };
  if (Object.entries(expectedMutationCounts).some(([key, count]) => value.mutationCounts?.[key] !== count)) {
    throw new Error("qualification mutation counts are invalid");
  }
  if (live) {
    if (value.refund?.count !== 0) throw new Error("live qualification must not refund");
  } else if (value.refund?.count !== 1 ||
    !/^[A-Za-z0-9:_-]{3,}$/.test(String(value.refund?.walletOperationId || "")) ||
    !String(value.refund?.receiptId || "").trim() || !String(value.refund?.code || "").startsWith("opl:") ||
    String(value.refund?.userId || "") !== String(value.identities.sub2apiUserId) ||
    !String(value.receipt?.chargeOperationId || "").trim() ||
    value.refund?.originalChargeOperationId !== value.receipt?.chargeOperationId ||
    !/^[1-9][0-9]*$/.test(String(value.refund?.amountUsdMicros || "")) ||
    BigInt(value.refund.amountUsdMicros) > BigInt(value.debit.amountUsdMicros)) {
    throw new Error("platform refund for the deleted Workspace is not exactly bound");
  }
  if (value.usage?.source !== "sub2api" || value.usage?.status !== "available") throw new Error("Sub2API usage readback is invalid");
  const serialized = JSON.stringify(value);
  if (/"(?:password|cookie|csrf|authorization|token|apiKey)"\s*:/i.test(serialized)) {
    throw new Error("receipt contains a forbidden credential field");
  }
  return value;
}

function runProcess(command, args, { cwd = root, env = process.env, capture = true, allowFailure = false, stdin } = {}) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, { cwd, env, stdio: [stdin === undefined ? "ignore" : "pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => { stdout += chunk; });
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    if (stdin !== undefined) child.stdin.end(stdin);
    child.on("error", reject);
    child.on("close", (code, signal) => {
      const result = { code, signal, stdout, stderr };
      if (code === 0 || allowFailure) {
        if (!capture && stdout) process.stdout.write(stdout);
        if (!capture && stderr) process.stderr.write(stderr);
        resolvePromise(result);
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} failed with ${signal || code}: ${(stderr || stdout).trim().slice(-8000)}`));
    });
  });
}

// The platform's identifier derivation is owned by the purchase core, because a
// buyer that derived it differently would refuse the platform's own answer. It
// is re-exported here because this module is where its callers already reach.
export { stableID };

async function writeJSONAtomic(path, value) {
  await mkdir(dirname(path), { recursive: true });
  const temporary = `${path}.${process.pid}.${randomBytes(4).toString("hex")}.tmp`;
  await writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  await rename(temporary, path);
}

async function dockerImageInspection(image, { pullIfMissing = false } = {}) {
  let inspected = await runProcess("docker", ["image", "inspect", image], { allowFailure: true });
  if (inspected.code !== 0 && pullIfMissing) {
    await runProcess("docker", ["pull", image], { capture: false });
    inspected = await runProcess("docker", ["image", "inspect", image]);
  }
  const values = JSON.parse(inspected.stdout);
  if (!Array.isArray(values) || values.length !== 1 || !digestPattern.test(String(values[0]?.Id || ""))) {
    throw new Error("Docker image inspection did not return one immutable image ID");
  }
  return values[0];
}

async function imageInspection(image) {
  if (!immutableImageReference(image)) throw new Error("qualified image input must be repository@sha256 digest");
  const inspection = await dockerImageInspection(image, { pullIfMissing: true });
  const [repository, digest] = image.split("@");
  if (!Array.isArray(inspection.RepoDigests) || !inspection.RepoDigests.includes(`${repository}@${digest}`)) {
    throw new Error(`Docker RepoDigest does not match admitted image ${repository}`);
  }
  return inspection;
}

export function exactRepoDigestFromInspection(repository, inspection) {
  const repoDigests = inspection?.RepoDigests;
  if (!String(repository || "").trim() || /[@\s]/.test(repository) || !digestPattern.test(String(inspection?.Id || "")) ||
    !Array.isArray(repoDigests) || repoDigests.length !== 1) {
    throw new Error(`source-built image ${repository || "unknown"} has no unique registry manifest digest`);
  }
  const [actualRepository, digest, extra] = String(repoDigests[0]).split("@");
  if (extra !== undefined || actualRepository !== repository || !digestPattern.test(String(digest || ""))) {
    throw new Error(`source-built image ${repository} has no unique registry manifest digest`);
  }
  return `${actualRepository}@${digest}`;
}

export function localBuildProxyArgs() {
  const proxy = String(process.env.OPL_LOCAL_BUILD_PROXY || "").trim();
  if (!proxy) return [];
  if (proxy.startsWith("socks5h://")) {
    throw new Error("OPL_LOCAL_BUILD_PROXY does not support socks5h://; use socks5:// for Dockerfile Go build stages");
  }
  if (!/^(?:https?|socks5):\/\/[^\s]+$/.test(proxy)) {
    throw new Error("OPL_LOCAL_BUILD_PROXY must use an explicit http://, https://, or socks5:// URL");
  }
  const parsed = new URL(proxy);
  if (parsed.username || parsed.password) {
    throw new Error("OPL_LOCAL_BUILD_PROXY must not contain credentials");
  }
  if (parsed.search || parsed.hash) {
    throw new Error("OPL_LOCAL_BUILD_PROXY must not contain query or fragment parameters");
  }
  return proxy.startsWith("socks5://")
    ? ["--build-arg", `HTTPS_PROXY=${proxy}`]
    : ["--build-arg", `HTTP_PROXY=${proxy}`, "--build-arg", `HTTPS_PROXY=${proxy}`];
}

export function qualificationComposeEnvironment(baseEnvironment, exactEntries) {
  const environment = { ...baseEnvironment };
  for (const [key, value] of exactEntries) {
    if (!/^[A-Z][A-Z0-9_]*$/.test(String(key)) || /[\r\n]/.test(String(value))) {
      throw new Error("qualification compose environment entry is invalid");
    }
    environment[key] = String(value);
  }
  return environment;
}

export function assertLocalV2ComposeEnvironment(output, values) {
  const observed = new Map();
  for (const line of String(output || "").split(/\r?\n/)) {
    const separator = line.indexOf("=");
    if (separator <= 0) continue;
    observed.set(line.slice(0, separator), line.slice(separator + 1));
  }
  const mismatches = Object.entries(values || {})
    .filter(([name, value]) => observed.get(name) !== String(value))
    .map(([name]) => name);
  if (mismatches.length) {
    throw namedQualificationError("local_v2_compose_environment_mismatch", mismatches.join(", "));
  }
  return observed;
}

export async function buildSourceImages(sourceSha, project, registryPort) {
  const registryContainer = `${project}-registry`;
  const cloudRepository = `127.0.0.1:${registryPort}/${project}-cloud`;
  const workspaceRepository = `127.0.0.1:${registryPort}/${project}-workspace`;
  const cloudTag = `${cloudRepository}:source`;
  const workspaceTag = `${workspaceRepository}:source`;
  try {
    await runProcess("docker", ["run", "-d", "--name", registryContainer, "-p", `127.0.0.1:${registryPort}:5000`, "registry:2"]);
    const proxyArgs = localBuildProxyArgs();
    await runProcess("docker", [
      "build", ...proxyArgs, "--build-arg", `OPL_SOURCE_SHA=${sourceSha}`,
      "--label", `org.opencontainers.image.revision=${sourceSha}`, "--tag", cloudTag, "."
    ], { capture: false });
    await runProcess("docker", [
      "build", ...proxyArgs, "--label", `org.opencontainers.image.revision=${sourceSha}`,
      "--file", "deploy/portable/qualification-workspace.Dockerfile", "--tag", workspaceTag, "."
    ], { capture: false });
    await runProcess("docker", ["push", cloudTag], { capture: false });
    await runProcess("docker", ["push", workspaceTag], { capture: false });
    const cloudImage = exactRepoDigestFromInspection(cloudRepository, await dockerImageInspection(cloudTag));
    const workspaceImage = exactRepoDigestFromInspection(workspaceRepository, await dockerImageInspection(workspaceTag));
    await imageInspection(cloudImage);
    await imageInspection(workspaceImage);
    return { cloudImage, workspaceImage, tags: [cloudTag, workspaceTag], registryContainer };
  } catch (error) {
    for (const tag of [cloudTag, workspaceTag]) await runProcess("docker", ["image", "rm", tag], { allowFailure: true });
    await runProcess("docker", ["rm", "-f", registryContainer], { allowFailure: true });
    throw error;
  }
}

async function readQualificationSourceIdentity() {
  const [sha, tree, status] = await Promise.all([
    runProcess("git", ["rev-parse", "HEAD"]),
    runProcess("git", ["rev-parse", "HEAD^{tree}"]),
    runProcess("git", ["status", "--porcelain", "--untracked-files=all"])
  ]);
  return { sha: sha.stdout.trim(), tree: tree.stdout.trim(), clean: status.stdout.trim() === "" };
}

export function sourceData(envelope, expectedSource) {
  if (!envelope || envelope.source !== expectedSource || envelope.available !== true || !["available", "empty"].includes(envelope.status)) {
    throw new Error(`${expectedSource} source readback is unavailable`);
  }
  return envelope.data;
}

function responseCookie(headers) {
  const value = headers.get("set-cookie");
  return value ? value.split(";", 1)[0] : "";
}

/*
 * Every cookie a response set, by name.
 *
 * The CloudIdentity login answers with two cookies whose names it owns, and the
 * login is only accepted when both of them come back on the command that
 * follows. Reading one cookie out of a joined `set-cookie` header would be a
 * guess about which one the server meant, so the names are read instead.
 */
function responseCookies(headers) {
  const values = typeof headers.getSetCookie === "function"
    ? headers.getSetCookie()
    : [headers.get("set-cookie") || ""];
  const cookies = new Map();
  for (const value of values) {
    // One set-cookie entry is one cookie plus its attributes, so only the pair
    // before the first separator is the cookie itself.
    const pair = String(value || "").split(";", 1)[0];
    const index = pair.indexOf("=");
    if (index <= 0) continue;
    const name = pair.slice(0, index).trim();
    const cookieValue = pair.slice(index + 1).trim();
    if (!name || !cookieValue) continue;
    cookies.set(name, `${name}=${cookieValue}`);
  }
  return cookies;
}

// The installation serves one HTTPS origin signed by its own CA. A request that
// trusted the host store instead would either fail or, worse, succeed against a
// certificate this installation never issued, so the declared CA is the only
// trust anchor a qualification request uses.
function caRequest(url, init, headers, body, ca) {
  return new Promise((resolvePromise, reject) => {
    const target = new URL(url);
    const request = httpsRequest({
      hostname: target.hostname,
      port: target.port || 443,
      path: `${target.pathname}${target.search}`,
      method: init.method || "GET",
      headers: Object.fromEntries(headers.entries()),
      ca,
      servername: target.hostname,
      signal: ownerRequestSignal(init)
    }, (response) => {
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => {
        const text = Buffer.concat(chunks).toString("utf8");
        const setCookie = response.headers["set-cookie"] || [];
        resolvePromise({
          status: response.statusCode,
          headers: {
            get: (name) => {
              const value = response.headers[String(name).toLowerCase()];
              return Array.isArray(value) ? value.join(", ") : value ?? null;
            },
            getSetCookie: () => setCookie
          },
          text: async () => text
        });
      });
    });
    request.on("error", reject);
    // One end() call lets Node set Content-Length for the body. A chunked DELETE
    // body is legal but the owner's write guard reads it as an empty command.
    request.end(body);
  });
}

// requestTimeoutMs is the budget one owner command gets. A read answers at once;
// a deletion drives the retirement chain inside the owner before it responds.
function requestTimeoutMs() {
  const configured = Number(process.env.OPL_QUALIFICATION_REQUEST_TIMEOUT_MS || 0);
  return configured > 0 ? configured : 120_000;
}

function ownerRequestSignal(init = {}) {
  const timeout = AbortSignal.timeout(requestTimeoutMs());
  return init.signal ? AbortSignal.any([init.signal, timeout]) : timeout;
}

export function createHTTP(origin, { caFile = "", mode = "" } = {}) {
  const ca = caFile ? readFileSync(caFile) : null;
  const request = async (path, init = {}, auth = null) => {
    // The mode refuses a retained route before a socket is opened, so a v2 run
    // cannot reach a Control Plane writer even if a caller asks for one.
    assertQualificationPath(mode, path);
    const headers = new Headers(init.headers || {});
    if (auth?.cookie) headers.set("cookie", auth.cookie);
    if (auth?.csrf) headers.set("x-opl-csrf", auth.csrf);
    // The owner route refuses a command that does not prove it came from the
    // origin it is served on, and the Control Plane refuses a login whose
    // Origin names somewhere else. Both accept the origin this transport was
    // built for, so every request carries it rather than each caller deciding.
    headers.set("origin", origin);
    // The CloudIdentity session carries its own CSRF token under its own header
    // name. The Control Plane token travels under a different one, so both
    // authorities' write guards are satisfied by one transport.
    if (auth?.ownerCsrf) headers.set("x-csrf-token", auth.ownerCsrf);
    const body = init.body === undefined ? undefined : JSON.stringify(init.body);
    if (body !== undefined) {
      headers.set("content-type", "application/json");
      // Node does not default a DELETE body to a length, and without it the
      // request leaves without the body at all. Every command here carries one.
      headers.set("content-length", String(Buffer.byteLength(body)));
    }
    const url = `${origin}${path}`;
    const response = ca
      ? await caRequest(url, init, headers, body, ca)
      : await fetch(url, {
        ...init,
        headers,
        body,
        signal: ownerRequestSignal(init)
      });
    const text = await response.text();
    let payload = null;
    if (text.trim()) {
      try { payload = JSON.parse(text); } catch { payload = null; }
    }
    return { response, payload, text };
  };
  const json = async (path, init = {}, auth = null, statuses = [200]) => {
    const result = await request(path, init, auth);
    if (!statuses.includes(result.response.status) || result.payload === null) {
      throw Object.assign(new Error(`HTTP ${init.method || "GET"} ${path} returned ${result.response.status}: ${result.text.slice(0, 500)}`),
        { status: result.response.status, method: init.method || "GET", path });
    }
    return result;
  };
  return { origin, request, json };
}

/*
 * The Workspace owner's own deletion.
 *
 * A Workspace bought on the owner route is deleted by the owner that minted it,
 * not by the retained Control Plane route. The command names the Workspace and
 * acknowledges data destruction, and the owner answers with the delete operation
 * it minted. The harness follows that identity to a terminal status and never
 * derives it, so a foreign or replayed deletion cannot name this purchase.
 */
export async function deleteWorkspaceOnOwnerRoute(http, auth, {
  workspaceId, confirmationName, idempotencyKey,
  wait = waitForWorkspaceDeleteSchedule,
  attempts = 120,
} = {}) {
  // A command whose answer is lost is not a refused command. The owner keys the
  // deletion by the idempotency key this caller chose, so re-sending the identical
  // command reaches the same operation instead of starting a second deletion. The
  // transport loss is recorded so a receipt can show which one happened.
  const command = {
    method: "DELETE",
    headers: { "idempotency-key": idempotencyKey },
    body: { confirmationName, acknowledgeDataDestruction: true },
  };
  const path = `/api/v2/workspaces/${encodeURIComponent(workspaceId)}`;
  let accepted = null;
  let lostResponses = 0;
  let lastLoss = "";
  for (let attempt = 0; attempt < 4; attempt += 1) {
    try {
      accepted = await http.json(path, command, auth, [202]);
      break;
    } catch (error) {
      lostResponses += 1;
      lastLoss = String(error?.message || error);
      await wait(2000);
    }
  }
  if (!accepted) {
    throw namedQualificationError("local_v2_workspace_delete_unreachable", `Workspace owner DELETE never answered: ${lastLoss}`);
  }
  const operation = accepted.payload;
  if (operation?.owner !== "workspace" || operation?.kind !== "delete_workspace" ||
    operation?.resourceId !== workspaceId || !String(operation?.operationId || "").trim()) {
    throw new Error("Workspace owner DELETE was not accepted for this Workspace");
  }
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const read = (await http.json(
      `/api/v2/operations/workspace/${encodeURIComponent(operation.operationId)}`, {}, auth,
    )).payload;
    if (read?.operationId !== operation.operationId || read?.owner !== "workspace" ||
      read?.kind !== "delete_workspace" || read?.resourceId !== workspaceId) {
      throw new Error("Workspace owner DELETE readback is invalid");
    }
    if (read.status === "failed" || read.status === "cancelled") {
      throw new Error(`Workspace owner DELETE did not succeed: ${read.status} ${String(read.stage || "")}`.trim());
    }
    if (read.status === "succeeded") {
      return {
        operationId: read.operationId, status: read.status, stage: String(read.stage || ""),
        accepted: operation, lostResponses, lastLoss,
      };
    }
    await wait(1000);
  }
  throw new Error("Workspace owner DELETE did not reach a terminal status within 120 seconds");
}

// A deleted Workspace remains readable for financial obligations. Its typed
// deletion readback, rather than a 404 or an empty deployment list, proves the
// original owners confirmed resource/data absence and Ledger recorded it.
export async function readWorkspaceDeletionEvidence(http, auth, { workspaceId, operationId, localNoCharge = false } = {}) {
  const deletion = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deletion`, {}, auth)).payload;
  const receipt = deletion?.deletionReceipt;
  // A local no-charge order has no money to return, so the owner records the
  // refund as not applicable rather than confirming one. That is only acceptable
  // when the run itself is a local no-charge purchase; a charged order must name
  // the confirmed refund and its operation.
  const refundSettled = deletion?.refundStatus === "confirmed"
    ? Boolean(String(deletion?.refundOperationId || "").trim())
    : localNoCharge && deletion?.refundStatus === "not_applicable";
  if (deletion?.workspaceId !== workspaceId || deletion?.operationId !== operationId ||
    deletion.resourceDeletionStatus !== "confirmed" || deletion.dataDeletionStatus !== "confirmed" ||
    !refundSettled ||
    !String(receipt?.id || "").trim() || receipt.kind !== "provider_action" || receipt.owner !== "workspace" ||
    receipt.operationId !== operationId || receipt.outcome !== "confirmed" ||
    !Number.isFinite(Date.parse(receipt.createdAt))) {
    throw new Error("Workspace deletion receipt readback is invalid");
  }
  return deletion;
}

function waitForWorkspaceDeleteSchedule(milliseconds) {
  return new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));
}

export async function login(http, email, password, expectedAccountId = "acct-admin") {
  const result = await http.json("/api/auth/login", { method: "POST", body: { email, password } });
  const auth = { cookie: responseCookie(result.response.headers), csrf: result.response.headers.get("x-opl-csrf-token") || "" };
  if (!auth.cookie || !auth.csrf || result.payload?.user?.accountId !== expectedAccountId) {
    throw new Error("local qualification login did not establish the expected account session");
  }
  return auth;
}

/*
 * The two sessions this qualification holds, together.
 *
 * The owner route is authorized by a CloudIdentity session and the retained
 * Control Plane reads answer for the Control Plane's own session. One credential
 * establishes both, because the Gateway directory is the authority behind each
 * of them, but one cookie is not one session: the buyer carries the two cookies
 * and each authority reads its own.
 */

/*
 * The CloudIdentity session.
 *
 * The owner route is reached through the Console BFF, and the BFF answers for a
 * CloudIdentity session: a cookie of its own name, a session row in the identity
 * owner's store, and a CSRF token that belongs to that session. The Control
 * Plane session is a different authority with a different cookie and a different
 * session store, so a buyer that carried only the Control Plane cookie would be
 * refused by the owner route for a session it never established.
 *
 * The login is the one the browser performs: a login context that carries a
 * double-submit CSRF challenge, and a command that presents that challenge beside
 * the challenge cookie. Both challenge cookies are scoped to the auth route, so
 * they travel with this command and nowhere else.
 *
 * The credential is the customer's own. CloudIdentity authenticates it against
 * the Gateway directory, which is the same authority the Control Plane session
 * authenticates against, so one credential establishes one customer in two
 * authorities rather than two identities for one person.
 *
 * The session answers with the identity owner's own projection: the actor it
 * bound, the display name the Gateway directory returned for that actor, the
 * tenant it belongs to, and the CSRF token the write guards compare against.
 */
export async function cloudIdentityLogin(http, origin, username, password, idempotencyKey, { wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms)) } = {}) {
  // Compose can report running before the restarted identity route answers.
  // Reconcile readiness with GETs; the login command is sent once afterward.
  const deadline = Date.now() + 30_000;
  let context;
  for (let attempt = 0; attempt < 30; attempt += 1) {
    try {
      context = await http.json("/api/v2/auth/context", { signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())) }, null, [200]);
      break;
    } catch (error) {
      if (![502, 503, 504].includes(Number(error?.status)) || attempt === 29 || Date.now() >= deadline) throw error;
      await wait(Math.min(1000, Math.max(1, deadline - Date.now())));
    }
  }
  const challengeCookies = responseCookies(context.response.headers);
  const challenge = challengeCookies.get("opl_login_context");
  const csrfCookie = challengeCookies.get("opl_login_csrf");
  const csrfToken = String(context.payload?.csrfToken || "");
  if (!challenge || !csrfCookie || !csrfToken) {
    throw new Error("CloudIdentity login context is unavailable");
  }
  const key = String(idempotencyKey || "");
  if (!key) throw new Error("CloudIdentity login requires an idempotency key");
  const result = await http.json("/api/v2/auth/login", {
    method: "POST",
    headers: {
      "x-csrf-token": csrfToken,
      "idempotency-key": key,
      cookie: `${challenge}; ${csrfCookie}`,
    },
    body: { username, password },
  }, null, [200]);
  const sessionCookie = responseCookies(result.response.headers).get("opl_console_session");
  const session = result.payload;
  if (!sessionCookie || !session?.actorId || !session?.csrfToken) {
    throw new Error("CloudIdentity login did not establish a session");
  }
  return {
    cookie: sessionCookie,
    ownerCsrf: String(session.csrfToken),
    session: {
      actorId: String(session.actorId),
      displayName: String(session.displayName || ""),
      tenantId: String(session.tenantId || ""),
      csrfToken: String(session.csrfToken),
      expiresAt: String(session.expiresAt || ""),
    },
  };
}

// Workspace publishes application availability from its owner chain. Poll that
// verdict; a retained CP installation/resume command cannot advance this order.
export async function waitForWorkspaceApplicationInstallation(http, auth, {
  workspaceId,
  wait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)),
  attempts = 300
} = {}) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const workspace = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth)).payload;
    if (workspace?.id !== workspaceId) throw new Error("Workspace owner readback is invalid");
    if (workspace.applicationAvailability === "available" && String(workspace.accessUrl || "").trim()) {
      return { workspace, installation: null, resumed: false, attempts: attempt + 1 };
    }
    if (workspace.applicationAvailability === "unavailable") {
      throw new Error("Workspace application installation is unavailable");
    }
    await wait(1000);
  }
  throw new Error("Workspace application installation did not become ready within 300 seconds");
}

export async function waitForCompose(compose) {
  await compose(["up", "-d", "--wait", "--wait-timeout", "300"]);
}

// These are the processes used by this caller's v2 requests. The old
// CP/Fabric/Ledger stack cannot qualify them; refuse before any stack or
// customer mutation rather than running a purchase against missing owners.
export function assertWorkspaceQualificationTopology(config, { services = ["workspace", "console-bff", "tenant", "serve"] } = {}) {
  const missing = services.filter((name) => !config?.services?.[name]);
  if (missing.length) {
    throw Object.assign(new Error(`v2 qualification topology is missing owner services: ${missing.join(", ")}`), {
      code: "local_v2_qualification_topology_missing",
    });
  }
}

async function inspectComposeImage(compose, service) {
  const id = (await compose(["ps", "-q", service])).stdout.trim();
  if (!id) throw new Error(`${service} Compose container is missing`);
  const values = JSON.parse((await runProcess("docker", ["inspect", id])).stdout);
  if (!Array.isArray(values) || values.length !== 1 || !digestPattern.test(String(values[0]?.Image || ""))) {
    throw new Error(`${service} running image readback is invalid`);
  }
  return values[0];
}

export async function verifyStores(compose, { stores = LOCAL_V2_OWNER_STORES } = {}) {
  const names = stores.map(([database]) => `'${database}'`).join(",");
  const query = `select datname || ':' || pg_get_userbyid(datdba) from pg_database where datname in (${names}) order by datname`;
  const rows = (await compose(["exec", "-T", "postgres", "psql", "-U", "postgres", "-d", "postgres", "-Atqc", query])).stdout.trim().split(/\r?\n/);
  // The query orders by database name, so the expected set is compared in the
  // same order rather than in declaration order.
  const expected = stores.map(([database, owner]) => `${database}:${owner}`).sort();
  if (JSON.stringify([...rows].sort()) !== JSON.stringify(expected)) throw new Error("PostgreSQL database ownership is not separated");
  const observed = {};
  for (const [database, owner] of stores) {
    const schema = LOCAL_V2_OWNER_SCHEMAS[database];
    const result = (await compose([
      "exec", "-T", "postgres", "psql", "-U", "postgres", "-d", database, "-Atqc",
      `select count(*)::text || ':' || count(*) filter (where tableowner='${owner}')::text from pg_tables where schemaname='${schema}' and tablename <> 'opl_schema_migrations'`
    ])).stdout.trim();
    const [totalText, ownedText] = result.split(":");
    const total = Number(totalText);
    const owned = Number(ownedText);
    if (!Number.isSafeInteger(total) || total <= 0 || owned !== total) {
      throw new Error(`${database} owner schema is missing or not owned by ${owner}`);
    }
    observed[database] = { owner, schema, businessTables: total };
  }
  return { ownerStores: observed, ownerSeparated: true };
}

/*
 * The purchase receipts this Workspace actually carries. The v2 transactions
 * projection is the owner-typed source: a confirmed charge names the receipt
 * that settled it, and the retained Control Plane billing list is not consulted
 * for an order placed on the owner route.
 */

async function exactLabelIDs(kind, accountId, workspaceId) {
  const base = [
    `label=opl.fabric.provider=local-docker`,
    `label=opl.account.id=${accountId}`,
    `label=opl.workspace.id=${workspaceId}`
  ];
  const command = kind === "containers" ? ["ps", "-aq"] : kind === "volumes" ? ["volume", "ls", "-q"] : ["network", "ls", "-q"];
  const args = [...command];
  for (const label of base) args.push("--filter", label);
  return (await runProcess("docker", args)).stdout.trim().split(/\r?\n/).filter(Boolean);
}

export async function residualCounts(accountId, workspaceId) {
  const [containers, volumes, networks] = await Promise.all([
    exactLabelIDs("containers", accountId, workspaceId),
    exactLabelIDs("volumes", accountId, workspaceId),
    exactLabelIDs("networks", accountId, workspaceId)
  ]);
  return { containers: containers.length, volumes: volumes.length, networks: networks.length };
}

export async function cleanupLocalQualificationResources(accountId, workspaceId, beforeNetworks = async () => {}) {
  for (const id of await exactLabelIDs("containers", accountId, workspaceId)) {
    await runProcess("docker", ["stop", "--time", "10", id], { allowFailure: true });
    const removed = await runProcess("docker", ["rm", id], { allowFailure: true });
    if (removed.code !== 0) throw new Error("qualification-owned Runtime cleanup failed");
  }
  for (const id of await exactLabelIDs("volumes", accountId, workspaceId)) {
    const removed = await runProcess("docker", ["volume", "rm", id], { allowFailure: true });
    if (removed.code !== 0) throw new Error("qualification-owned volume cleanup failed");
  }
  await beforeNetworks();
  for (const id of await exactLabelIDs("networks", accountId, workspaceId)) {
    const removed = await runProcess("docker", ["network", "rm", id], { allowFailure: true });
    if (removed.code !== 0) throw new Error("qualification-owned network cleanup failed");
  }
  return residualCounts(accountId, workspaceId);
}

// The deletion Receipt attests every stage that preceded it: the frozen order,
// each stage's own result and evidence kind, and a real provider or committed
// local observation with its opaque reference.
const workspaceDeleteReceiptStages = Object.freeze([
  Object.freeze({ stage: "runtime_absent", result: "absent", evidenceKind: "provider_readback" }),
  Object.freeze({ stage: "attachment_absent", result: "released", evidenceKind: "local_transition" }),
  Object.freeze({ stage: "storage_absent", result: "absent", evidenceKind: "provider_readback" }),
  Object.freeze({ stage: "compute_absent", result: "absent", evidenceKind: "provider_readback" }),
  Object.freeze({ stage: "workspace_absent", result: "removed", evidenceKind: "local_transition" })
]);

// Only the Local-Docker publication shape is admissible: the application's own
// entry is the host-bound loopback port its provider reported.
export function localApplicationEntryPort(url) {
  const match = /^http:\/\/127\.0\.0\.1:([0-9]{1,5})\/$/.exec(String(url || ""));
  const port = match ? Number(match[1]) : 0;
  return port >= 1 && port <= 65535 ? port : 0;
}

function usdMicrosFromDecimal(value) {
  const normalized = String(value ?? "").trim();
  const match = normalized.match(/^(-?)([0-9]+)(?:\.([0-9]{1,6}))?$/);
  if (!match) throw new Error("live Sub2API adjustment amount is not an exact decimal");
  const micros = BigInt(match[2]) * 1_000_000n + BigInt((match[3] || "").padEnd(6, "0") || "0");
  return `${match[1] === "-" ? "-" : ""}${micros.toString()}`;
}

function receiptCommand(options) {
  const base = `npm run qualify:local:workspace -- --mode local-v2 --source-sha ${options.sourceSha} --installation-env ${options.installationEnv} --application-contract ${options.applicationContract} --project ${options.project} --receipt ${options.receiptPath}`;
  // The customer package is part of the command when it was declared, so the
  // receipt names the package the run uploaded.
  if (!options.customer?.declared) return base;
  const customer = options.customer.mode === "directory"
    ? `--customer-package-dir ${options.customer.packageDir} --customer-probe-path ${options.customer.probePath}`
    : `--customer-package-archive ${options.customer.packageArchive}`;
  return `${base} ${customer} --customer-expected-evidence-level ${options.customer.requestedEvidenceLevel}`;
}

// A declared installation environment file is one NAME=value entry per line, in
// the template's own spelling. Nothing here evaluates a shell, interpolates a
// variable or accepts a second spelling of a name: the file the operator
// declares is the file Compose reads.
export function parseInstallationEnvFile(text) {
  const values = {};
  for (const line of String(text ?? "").split(/\r?\n/)) {
    const entry = line.trim();
    if (!entry || entry.startsWith("#")) continue;
    const index = entry.indexOf("=");
    const name = index > 0 ? entry.slice(0, index).trim() : "";
    if (!/^[A-Z][A-Z0-9_]*$/.test(name)) {
      throw namedQualificationError("local_v2_installation_env_invalid", `installation declaration entry is not a NAME=value line: ${entry.slice(0, 80)}`);
    }
    if (Object.prototype.hasOwnProperty.call(values, name)) {
      throw namedQualificationError("local_v2_installation_env_duplicate", `installation declaration names ${name} twice`);
    }
    values[name] = entry.slice(index + 1);
  }
  return values;
}

// The installation this run qualifies, read from the declaration the operator
// wrote and validated against the template and the Compose file the repository
// ships. Every fact below uses this map, so a qualification run cannot qualify a
// machine the installation does not describe.
export async function readLocalV2InstallationDeclaration({
  envFile,
  templatePath = join(root, LOCAL_V2_INSTALLATION_TEMPLATE),
  composePath = join(root, LOCAL_V2_QUALIFICATION_COMPOSE)
} = {}) {
  const declaredPath = String(envFile || "").trim();
  if (!declaredPath) {
    throw namedQualificationError("local_v2_installation_env_required", "--installation-env must name the declared installation environment file");
  }
  if (!isAbsolute(declaredPath)) {
    throw namedQualificationError("local_v2_installation_env_invalid", "--installation-env must be an absolute path");
  }
  const [template, compose, text] = await Promise.all([
    readFile(templatePath, "utf8"),
    readFile(composePath, "utf8"),
    readFile(declaredPath, "utf8")
  ]);
  const facts = localV2InstallationFacts({ template, compose, values: parseInstallationEnvFile(text) });
  return { envFile: declaredPath, values: facts.values, facts };
}

// Authenticated Console and owner requests use this declared HTTPS origin and
// its own CA. The anonymous Local-Docker application has a separate binding.
export function localV2QualificationOrigin(values) {
  const declared = String(values?.OPL_PUBLIC_URL || "").trim();
  let url;
  try {
    url = new URL(declared);
  } catch {
    throw namedQualificationError("local_v2_public_url_invalid", "OPL_PUBLIC_URL must be the installation's HTTPS origin");
  }
  if (url.protocol !== "https:" || url.pathname !== "/" || url.search || url.hash) {
    throw namedQualificationError("local_v2_public_url_invalid", "OPL_PUBLIC_URL must be one HTTPS origin without a path");
  }
  return url.origin;
}

export function localV2QualificationCA(values) {
  const directory = String(values?.OPL_PKI_DIR || "").trim();
  if (!directory || !isAbsolute(directory)) {
    throw namedQualificationError("local_v2_pki_dir_invalid", "OPL_PKI_DIR must be the absolute PKI directory the topology serves");
  }
  return join(directory, "ca.crt");
}

// Same-origin entries must match the installation's actual host, protocol and
// port. A separate anonymous Docker entry requires the Runtime proof below.
export function localV2ApplicationEntryBoundary(entryUrl, publicOrigin) {
  const entry = String(entryUrl || "").trim();
  if (!entry) return { ok: false, code: "local_v2_application_entry_missing", detail: "the Workspace owner has not published an application entry" };
  let parsed;
  try {
    parsed = new URL(entry);
  } catch {
    return { ok: false, code: "local_v2_application_entry_invalid", detail: "the owner published an application entry that is not a URL" };
  }
  let origin;
  try {
    origin = new URL(String(publicOrigin || ""));
  } catch {
    return { ok: false, code: "local_v2_public_url_invalid", detail: "the installation public origin is not a URL" };
  }
  const entryPort = parsed.port || (parsed.protocol === "https:" ? "443" : "80");
  const publicPort = origin.port || (origin.protocol === "https:" ? "443" : "80");
  // The published entry carries no port, so the only way it is reachable is the
  // installation serving that host on the port the entry implies.
  if (parsed.protocol !== origin.protocol || parsed.hostname !== origin.hostname ||
      parsed.username || parsed.password || entryPort !== publicPort) {
    return {
      ok: false,
      code: "local_v2_application_entry_port_unreachable",
      detail: `the entry ${parsed.origin} is not the declared origin ${origin.origin}`
    };
  }
  return { ok: true, code: "", detail: `${parsed.origin} is served by the declared origin` };
}

// A Local-Docker anonymous application has its own loopback publication rather
// than the Console's HTTPS origin. Admit that origin only after the provider's
// actual container, immutable image, owner labels and declared port agree.
export function localV2AnonymousDockerOrigin({ entryUrl, workspaceId, accountId, imageRef, imageId, revision, containers }) {
  const port = localApplicationEntryPort(entryUrl);
  const target = (revision?.ports || []).find((value) => value.name === revision.entryPort);
  const runtime = (containers || []).filter((container) => container?.Config?.Labels?.["opl.fabric.kind"] === "application_runtime" &&
    container?.Config?.Labels?.["opl.component.role"] === "main");
  const container = runtime[0];
  const labels = container?.Config?.Labels || {};
  const bindings = container?.NetworkSettings?.Ports?.[`${target?.port}/tcp`] || [];
  if (!port || revision?.exposurePolicy !== "anonymous" || !target || runtime.length !== 1 ||
      container?.State?.Running !== true || container?.Image !== imageId || !digestPattern.test(String(imageId || "")) ||
      labels["opl.fabric.provider"] !== "local-docker" || labels["opl.workspace.id"] !== workspaceId ||
      labels["opl.account.id"] !== accountId || labels["opl.image.ref"] !== imageRef || !labels["opl.runtime.id"] ||
      !bindings.some((binding) => binding.HostIp === "127.0.0.1" && binding.HostPort === String(port))) {
    throw namedQualificationError("local_v2_application_runtime_binding_invalid", "the published entry does not match the declared anonymous Local-Docker Runtime");
  }
  return new URL(entryUrl).origin;
}

// The Workspace owner reports a parked saga as a dependency failure; the
// provider's own reason lives in the Fabric process log, which is the owner's
// record of why it refused the dispatch. A qualification run reports that reason
// rather than the generic mapping.
async function localV2ProviderBoundary(compose, composePrefix, sinceIso, query) {
  // The Fabric owner records the dispatch it refused under its own operation id,
  // so the run joins through that record instead of guessing which log line
  // belongs to this attempt. The provider writes its reason when it retries, on
  // its own schedule, so the run watches for a bounded window and reports only
  // what it observed.
  if (typeof query !== "function") return null;
  let fabricOperationId = "";
  try {
    fabricOperationId = (await query("opl_fabric",
      `select id from fabric.operations where created_at >= '${String(sinceIso).replace(/'/g, "")}' order by created_at desc limit 1`)).trim();
  } catch {
    return null;
  }
  if (!fabricOperationId) return null;
  let container = "";
  try {
    container = (await compose([...composePrefix, "ps", "-q", "fabric"])).stdout.trim().split(/\r?\n/)[0] || "";
  } catch {
    return null;
  }
  if (!container) return null;
  const sinceEpoch = String(Math.max(0, Math.floor(Date.parse(sinceIso) / 1000)));
  const deadline = Date.now() + Number(process.env.OPL_QUALIFICATION_PROVIDER_LOG_WAIT_MS || 30_000);
  let observed = "";
  while (Date.now() < deadline) {
    let log = "";
    try {
      // The owner writes its own log lines to stderr, and `docker logs` keeps
      // the two streams apart, so both are read.
      const output = await runProcess("docker", ["logs", container, "--since", sinceEpoch], { allowFailure: true });
      log = `${output.stdout}\n${output.stderr}`;
    } catch {
      return null;
    }
    const lines = log.split(/\r?\n/).filter((line) => line.includes(`operation=${fabricOperationId}`));
    observed = lines[lines.length - 1] || "";
    if (/local_docker_[a-z_]+/.test(observed)) break;
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 3000));
  }
  const code = (observed.match(/local_docker_[a-z_]+/) || [""])[0];
  if (!code) return null;
  return { code, detail: observed.replace(/^\S+\s+\S+\s+/, "").slice(0, 400), line: observed.slice(0, 400) };
}

function localV2Boundary(operation, workspace) {
  const code = String(operation?.errorCode || operation?.error?.code || workspace?.errorCode || "").trim();
  const detail = String(operation?.error?.message || operation?.message || workspace?.error?.message || "").trim();
  return { code: code || "local_v2_operation_stopped", detail: detail || `the owner stopped the operation at ${String(operation?.stage || "an unnamed stage")}`, stage: String(operation?.stage || "") };
}

// `awaiting_confirmation` is not terminal: the owner parks a saga there while it
// waits for a dependency it re-drives itself, and a real installation moved from
// `runtime` to `activation` after several minutes in that state. The run keeps
// polling and only reads a park as the boundary when the state stops changing.
const LOCAL_V2_TERMINAL_OPERATION = new Set(["succeeded", "failed", "cancelled", "needs_attention"]);
// A Local no-charge order has no terminal state of its own: the owner records the
// Serve-confirmed READY runtime at stage `activation` and keeps re-driving that
// same saga, so a poll can catch the operation mid-cycle at an earlier stage. The
// boundary is the state the owner reached, not the state the last poll saw.
const LOCAL_V2_ACTIVATION_STAGE = "activation";
const LOCAL_V2_STALLED_POLLS = 40;
const LOCAL_V2_MAX_POLL_INTERVAL_MS = 60_000;
const LOCAL_V2_MAX_POLL_ATTEMPTS = 1_000;
const LOCAL_V2_MAX_RECOVERY_REPLAYS = 10;
const LOCAL_V2_MAX_POLL_DURATION_MS = 30 * 60 * 1_000;

function localV2Budget(value, { name, code, minimum, maximum }) {
  const parsed = typeof value === "number" ? value : typeof value === "string" && value.trim() ? Number(value) : NaN;
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    throw namedQualificationError(code, `${name} must be an integer between ${minimum} and ${maximum}`);
  }
  return parsed;
}

function localV2ConfiguredBudget(dependencyValue, environmentValue, defaultValue, options) {
  const value = dependencyValue !== undefined
    ? dependencyValue
    : environmentValue === undefined
      ? defaultValue
      : environmentValue;
  return localV2Budget(value, options);
}

function validateLocalV2Replay(operation, operationId, workspaceId) {
  if (operation?.owner !== "workspace" || operation?.kind !== "create_workspace" ||
    operation?.operationId !== operationId || operation?.resourceId !== workspaceId) {
    throw namedQualificationError("local_v2_replay_identity_mismatch", "the Workspace owner replay did not identify the original order");
  }
}

function localV2LaunchReadbackBoundary(workspace, workspaceId, origin) {
  if (workspace?.id !== workspaceId) {
    return { ok: false, code: "local_v2_workspace_readback_invalid", detail: "the Workspace owner readback does not identify the accepted Workspace" };
  }
  if (workspace.resourceReadiness !== "ready" || workspace.applicationAvailability !== "available") {
    return { ok: false, code: "local_v2_application_readiness_unconfirmed", detail: "the Workspace resource and Serve application readbacks do not confirm readiness" };
  }
  return localV2ApplicationEntryBoundary(workspace.accessUrl, origin);
}

// The Local v2 qualification: one declared installation, the owner processes it
// starts, and the owner routes it serves. Every step records the readback it
// observed. Incomplete runs retain their diagnostic boundary. The complete
// anonymous Local-Docker profile emits its native version-two receipt; retained
// CP identities and paid-fixture facts stay with the version-one profile.
export async function runLocalV2Qualification(options, dependencies = {}) {
  const pollIntervalMs = localV2ConfiguredBudget(dependencies.pollIntervalMs, process.env.OPL_QUALIFICATION_POLL_INTERVAL_MS, 3000, {
    name: "OPL_QUALIFICATION_POLL_INTERVAL_MS", code: "local_v2_poll_interval_invalid", minimum: 0, maximum: LOCAL_V2_MAX_POLL_INTERVAL_MS
  });
  const pollAttempts = localV2ConfiguredBudget(dependencies.pollAttempts, process.env.OPL_QUALIFICATION_POLL_ATTEMPTS, 100, {
    name: "OPL_QUALIFICATION_POLL_ATTEMPTS", code: "local_v2_poll_attempts_invalid", minimum: 1, maximum: LOCAL_V2_MAX_POLL_ATTEMPTS
  });
  const recoveryReplays = localV2ConfiguredBudget(dependencies.recoveryReplays, process.env.OPL_QUALIFICATION_RECOVERY_REPLAYS, 3, {
    name: "OPL_QUALIFICATION_RECOVERY_REPLAYS", code: "local_v2_recovery_replays_invalid", minimum: 0, maximum: LOCAL_V2_MAX_RECOVERY_REPLAYS
  });
  if (pollAttempts * pollIntervalMs > LOCAL_V2_MAX_POLL_DURATION_MS) {
    throw namedQualificationError("local_v2_poll_budget_invalid", "the combined Workspace polling budget must not exceed 30 minutes");
  }
  const mode = QUALIFICATION_MODE_LOCAL_V2;
  const binding = qualificationModeBinding(mode);
  const readDeclaration = dependencies.readDeclaration || readLocalV2InstallationDeclaration;
  const createClient = dependencies.createClient || ((origin, settings) => createHTTP(origin, settings));
  let composeEnvironment = process.env;
  const compose = dependencies.compose || ((args, settings = {}) => runProcess("docker", args, { cwd: root, ...settings, env: composeEnvironment }));
  const ownerQuery = dependencies.ownerQuery || null;
  const wait = dependencies.wait || ((milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)));
  const startedAt = new Date().toISOString();
  const stages = [];
  const readbacks = {};
  const record = (stage, status, detail = {}) => { stages.push({ ...detail, stage, status, observedAt: new Date().toISOString() }); };
  let stage = "installation_preflight";
  let boundary = null;
  // The customer package chain keeps its own boundary. It is not merged into
  // the run's boundary while the run continues, because a failure there is a
  // fact of its own and the run's own stages still have something to say.
  let customerBoundary = null;
  let failure = null;
  let composePrefix = [];
  let sourceTree = "unavailable";
  let sourceIdentity;
  let admittedImages;
  const processes = {};
  const readSource = dependencies.readSourceIdentity || readQualificationSourceIdentity;
  const inspectImage = dependencies.inspectImage || imageInspection;
  const inspectOwner = dependencies.inspectOwner || inspectComposeImage;
  const workspaceName = String(options.workspaceName || "Local v2 qualification");
  try {
    sourceIdentity = await readSource();
    validateQualificationSourceIdentity(sourceIdentity, sourceIdentity, options.sourceSha);
    sourceTree = sourceIdentity.tree;
    const declaration = await readDeclaration({ envFile: options.installationEnv });
    const values = declaration.values;
    const origin = localV2QualificationOrigin(values);
    const caFile = localV2QualificationCA(values);
    readbacks.installation = {
      envFile: declaration.envFile,
      origin,
      pkiDir: String(values.OPL_PKI_DIR || ""),
      proxyPort: String(values.OPL_PROXY_PORT || ""),
      subnet: String(values.OPL_DOCKER_SUBNET || ""),
      postgresHost: String(values.OPL_POSTGRES_HOST || ""),
      workspaceDomain: String(values.OPL_WORKSPACE_DOMAIN || ""),
      cloudImage: String(values.OPL_CLOUD_IMAGE || ""),
      workspaceImage: String(values.OPL_WORKSPACE_IMAGE || ""),
      qualificationEmail: String(values.OPL_QUALIFICATION_USER_EMAIL || ""),
      undeclaredFacts: [...(declaration.facts.unresolved || [])]
    };
    record("installation_preflight", "confirmed", { declared: (declaration.facts.entries || []).length, unresolved: readbacks.installation.undeclaredFacts.length });
    if (!isAbsolute(String(options.applicationContract || ""))) {
      throw namedQualificationError("local_v2_application_contract_required", "--application-contract must name the approved Runtime Publisher Contract file");
    }
    // The contract file is the Runtime Publisher Contract exactly as the owner
    // accepts it, so the run posts the declared document rather than composing
    // one from installation facts.
    const contract = JSON.parse(await readFile(options.applicationContract, "utf8"));
    const contractImage = `${contract?.image?.repository || ""}@${contract?.image?.digest || ""}`;
    if (contractImage !== readbacks.installation.workspaceImage) {
      throw namedQualificationError(
        "local_v2_application_contract_image_mismatch",
        `the declared application contract names ${contractImage}, the installation declares ${readbacks.installation.workspaceImage}`
      );
    }
    const revision = contract?.applicationRevisionTemplate || {};
    if (!String(revision.applicationId || "") || !String(revision.version || "")) {
      throw namedQualificationError("local_v2_application_contract_invalid", "the declared application contract names no application id and version");
    }
    readbacks.application = {
      image: contractImage,
      applicationId: String(revision.applicationId),
      revision: String(revision.version),
      exposurePolicy: String(revision.exposurePolicy || "")
    };
    record("application_contract", "confirmed", { image: contractImage });
    stage = "compose_topology";
    composeEnvironment = qualificationComposeEnvironment(process.env, Object.entries(values));
    composePrefix = ["compose", "--project-name", String(options.project || "").trim(), "--env-file", declaration.envFile,
      ...binding.composeFiles.flatMap((file) => ["-f", file])];
    if (!String(options.project || "").trim()) throw namedQualificationError("local_v2_project_required", "--project must name the Compose project of the declared installation");
    const effectiveEnvironment = await compose([...composePrefix, "config", "--environment"]);
    assertLocalV2ComposeEnvironment(effectiveEnvironment.stdout, values);
    record("compose_environment", "confirmed", { declared: Object.keys(values).length });
    stage = "image_admission";
    admittedImages = {};
    for (const [name, reference] of [["cloud", values.OPL_CLOUD_IMAGE], ["workspace", values.OPL_WORKSPACE_IMAGE]]) {
      const image = await inspectImage(reference);
      const digest = immutableImageDigest(reference);
      if (!digest || !image.RepoDigests?.includes(reference) || !digestPattern.test(String(image.Id || "")) ||
          name === "cloud" && image.Config?.Labels?.["org.opencontainers.image.revision"] !== options.sourceSha) {
        throw namedQualificationError("local_v2_image_binding_invalid", `${name} image does not match the admitted immutable source`);
      }
      admittedImages[name] = { input: reference, repoDigest: reference, digest, runningDigest: image.Id };
    }
    stage = "compose_topology";
    const up = await compose([...composePrefix, "up", "-d", "--wait", "--wait-timeout", "300"], { allowFailure: true });
    if (up.code !== 0) {
      throw namedQualificationError("local_v2_compose_start_failed", (up.stderr || up.stdout || "docker compose up failed").slice(0, 500));
    }
    const config = JSON.parse((await compose([...composePrefix, "config", "--format", "json"])).stdout);
    assertWorkspaceQualificationTopology(config, { services: LOCAL_V2_QUALIFICATION_SERVICES });
    for (const owner of LOCAL_V2_OWNER_PROCESSES) {
      const container = await inspectOwner((args) => compose([...composePrefix, ...args]), owner);
      if (container.Image !== admittedImages.cloud.runningDigest) {
        throw namedQualificationError("local_v2_running_image_mismatch", `${owner} is not running the admitted Cloud image`);
      }
      processes[owner] = "ready";
    }
    readbacks.topology = { services: Object.keys(config.services || {}).sort() };
    record("compose_topology", "confirmed", { services: readbacks.topology.services.length });
    const stores = await verifyStores((args) => compose([...composePrefix, ...args]));
    readbacks.stores = stores;
    record("owner_stores", "confirmed", { databases: Object.keys(stores.ownerStores || {}).length });
    const query = ownerQuery || (async (database, sql) => (await compose([
      ...composePrefix, "exec", "-T", "postgres", "psql", "-U", "postgres", "-d", database, "-Atqc", sql
    ])).stdout.trim());
    stage = "cloud_identity";
    const http = createClient(origin, { caFile, mode });
    const session = await cloudIdentityLogin(http, origin, readbacks.installation.qualificationEmail,
      String(values.OPL_QUALIFICATION_USER_PASSWORD || ""), `local-v2-login:${options.sourceSha}`);
    let auth = { cookie: session.cookie, ownerCsrf: session.ownerCsrf, csrf: "" };
    readbacks.cloudIdentity = { actorId: session.session.actorId, displayName: session.session.displayName, tenantId: session.session.tenantId, expiresAt: session.session.expiresAt };
    record("cloud_identity", "confirmed", { actorId: session.session.actorId });
    stage = "tenant";
    let tenant = (await http.request(binding.endpoints.tenant, {}, auth)).payload;
    if (!tenant?.id) {
      const created = await http.json(binding.endpoints.createTenant, {
        method: "POST",
        headers: { "idempotency-key": `local-v2-tenant:${options.sourceSha}` },
        body: { name: "Local v2 qualification", billingSub2apiUserId: session.session.actorId, ownerGatewaySubjectId: session.session.actorId }
      }, auth, [200, 201, 202]);
      const createdPayload = created.payload || {};
      const createdTenantId = String(createdPayload.resourceId || "");
      readbacks.tenantCreation = {
        operationId: String(createdPayload.operationId || ""),
        operationStatus: String(createdPayload.status || ""),
        tenantId: createdTenantId
      };
      if (createdPayload.owner !== "tenant" || createdPayload.kind !== "create_tenant" ||
        !["accepted", "succeeded"].includes(String(createdPayload.status || "")) || !createdTenantId) {
        throw namedQualificationError("local_v2_tenant_creation_invalid", "the Tenant owner returned no valid create operation");
      }
      // A session binds its Tenant when it is issued, so the session that
      // created the Tenant cannot read it back; the run signs in again and
      // confirms the binding the next session actually carries.
      const rebound = await cloudIdentityLogin(http, origin, readbacks.installation.qualificationEmail,
        String(values.OPL_QUALIFICATION_USER_PASSWORD || ""), `local-v2-login-rebound:${options.sourceSha}`);
      auth.cookie = rebound.cookie;
      auth.ownerCsrf = rebound.ownerCsrf;
      auth.csrf = rebound.ownerCsrf;
      tenant = (await http.request(binding.endpoints.tenant, {}, auth)).payload;
      if (String(rebound.session.actorId || "") !== String(session.session.actorId || "") ||
        String(rebound.session.tenantId || "") !== createdTenantId ||
        String(tenant?.id || "") !== createdTenantId) {
        throw namedQualificationError("local_v2_tenant_binding_mismatch", "the rebound CloudIdentity session and Tenant readback disagree with the created Tenant");
      }
      readbacks.cloudIdentity.tenantId = String(rebound.session.tenantId || "");
      readbacks.cloudIdentity.reboundActorId = String(rebound.session.actorId || "");
    }
    if (!tenant?.id) throw namedQualificationError("local_v2_tenant_unavailable", "the Tenant owner published no Tenant for the qualified identity");
    readbacks.tenant = { id: tenant.id, billingSub2apiUserId: String(tenant.billingSub2apiUserId || ""), status: String(tenant.status || "") };
    record("tenant", "confirmed", { tenantId: tenant.id });
    stage = "account_binding";
    // The provider profile names the Tenant account it executes for. A profile
    // that binds another Tenant cannot dispatch for this one, so the run reports
    // the binding the installation must declare instead of failing later inside
    // the provider with a generic mismatch.
    const declaredProfile = (() => {
      try { return JSON.parse(String(values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON || "")); } catch { return null; }
    })();
    const bindings = Array.isArray(declaredProfile?.accountBindings) ? declaredProfile.accountBindings : [];
    const accountBinding = bindings.find((candidate) => String(candidate?.tenantId || "") === tenant.id && String(candidate?.accountId || "").trim());
    if (!accountBinding) {
      throw namedQualificationError(
        "local_v2_account_binding_required",
        `the declared provider profile binds ${JSON.stringify(bindings.map((candidate) => candidate?.tenantId || ""))}, the qualified Tenant is ${tenant.id}; ` +
        `declare {"tenantId":"${tenant.id}","accountId":"<host account>"} in OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON and re-run`
      );
    }
    readbacks.accountBinding = { tenantId: tenant.id, accountId: String(accountBinding.accountId) };
    record("account_binding", "confirmed", readbacks.accountBinding);
    stage = "catalog";
    const packageProfile = (() => {
      let profile;
      try { profile = JSON.parse(String(values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON || "")); } catch { profile = null; }
      const selected = (profile?.packages || []).find((candidate) => candidate?.available !== false);
      if (!profile?.profileId || !profile?.capabilityVersion || !profile?.region || !selected?.compute?.id || !selected?.storage?.id) {
        throw namedQualificationError("local_v2_provider_profile_incomplete", "the declared provider profile names no executable package");
      }
      return { profile, selected };
    })();
    const repository = readbacks.installation.workspaceImage.split("@")[0];
    // The namespace records the registry the installation publishes to, and the
    // owner admits it by name: letters, digits, dots, hyphens and underscores.
    // A registry host that carries a port keeps it in the repository prefix,
    // which is the field that names the full path.
    const registryHost = repository.includes("/") ? repository.split("/")[0] : repository;
    const registryId = registryHost.split(":")[0];
    const namespaceId = await declareOrRead(http, auth, binding.endpoints.publisherNamespaces, "name", "local-v2-qualification-publisher", {
      name: "local-v2-qualification-publisher", kind: "official", registryId, repositoryPrefix: repository,
      admissionReceiptId: `local-v2-admission:${options.sourceSha}`
    });
    const runtimeVersionId = await declareOrRead(http, auth, binding.endpoints.runtimeVersions, "name", readbacks.application.applicationId, {
      name: readbacks.application.applicationId, versionLabel: readbacks.application.revision,
      publisherNamespaceId: namespaceId, admissionReceiptId: `local-v2-admission:${options.sourceSha}`,
      publisherContract: { ...contract, publisherNamespaceId: namespaceId }
    }, { readPath: binding.endpoints.catalogRuntimeVersions });
    const computePlanId = await declareOrRead(http, auth, binding.endpoints.computePlans, "name", "local-v2-compute", {
      name: "local-v2-compute", vcpus: packageProfile.selected.compute.cpu, memoryMiB: packageProfile.selected.compute.memoryGb * 1024,
      providerProfileId: packageProfile.profile.profileId, providerSkuId: packageProfile.selected.compute.id,
      providerCapabilityVersion: packageProfile.profile.capabilityVersion, validFrom: "2026-01-01T00:00:00Z"
    }, { readPath: binding.endpoints.catalogComputePlans });
    const storagePlanId = await declareOrRead(http, auth, binding.endpoints.storagePlans, "name", "local-v2-storage", {
      name: "local-v2-storage", capacityGiB: packageProfile.selected.storage.sizeGb,
      providerProfileId: packageProfile.profile.profileId, providerSkuId: packageProfile.selected.storage.id,
      shrinkSupported: false, validFrom: "2026-01-01T00:00:00Z"
    }, { readPath: binding.endpoints.catalogStoragePlans });
    const retentionPolicyId = await declareOrRead(http, auth, binding.endpoints.retentionPolicies, "versionLabel", "local-v2-retention", {
      versionLabel: "local-v2-retention", customerTerms: "The local installation keeps Tenant data until the Workspace is deleted."
    });
    const refundPolicyId = await declareOrRead(http, auth, binding.endpoints.refundPolicies, "versionLabel", "local-v2-refund", {
      versionLabel: "local-v2-refund", algorithm: "workspace-delete-refund-v1", retentionPolicyVersionId: retentionPolicyId,
      validFrom: "2026-01-01T00:00:00Z", customerTerms: "The local installation charges nothing for this Workspace."
    });
    await declareOrRead(http, auth, binding.endpoints.pricePolicies, "versionLabel", "local-v2-price", {
      versionLabel: "local-v2-price", periodMonths: 1, computeMonthlyUSDMicros: "0", storageMonthlyUSDMicros: "0", productMonthlyUSDMicros: "0",
      validFrom: "2026-01-01T00:00:00Z", computePlanId, storagePlanId,
      renewalPolicy: { version: "renewal-policy/v1", trigger: "manual_or_explicitly_consented_automatic", effectiveStart: "previous_paid_through", months: 1, usesAcceptedPriceSnapshot: true },
      planChangePolicyVersion: "workspace-plan-change-v1"
    });
    readbacks.catalog = {
      publisherNamespaceId: namespaceId, runtimeVersionId, computePlanId, storagePlanId, refundPolicyId,
      runtimeVersions: await catalogIds(http, auth, binding.endpoints.catalogRuntimeVersions),
      computePlans: await catalogIds(http, auth, binding.endpoints.catalogComputePlans),
      storagePlans: await catalogIds(http, auth, binding.endpoints.catalogStoragePlans)
    };
    record("catalog", "confirmed", { runtimeVersionId, computePlanId, storagePlanId });
    let catalogProfile;
    try {
      catalogProfile = JSON.parse(String(values.OPL_RESOURCE_CATALOG_PROFILE_JSON || ""));
    } catch {
      throw namedQualificationError("local_v2_resource_catalog_profile_invalid", "the installation must declare a valid Resource Catalog provider profile");
    }
    const provider = String(catalogProfile?.provider || "");
    const billingMode = String(catalogProfile?.billingMode || "");
    if (provider !== "local-docker" || billingMode !== "LOCAL_NO_CHARGE") {
      throw namedQualificationError("local_v2_resource_catalog_profile_invalid", "the local-v2 installation must declare local-docker and LOCAL_NO_CHARGE");
    }
    // The customer package evidence chain.
    //
    // It runs where the plans are known and before this run's own purchase,
    // and it owns every step of the customer loop: the uploaded package, the
    // explicit Build it becomes, the exact Capability Version that Build
    // registered, the Workspace that runs that version and the content its
    // entry answers with. The purchase, the deletion and the receipt of this
    // qualification are untouched by it.
    if (options.customer?.declared) {
      stage = "customer_package";
      const customerReadback = {};
      readbacks.customerPackage = customerReadback;
      const customerWorkspaceName = `${workspaceName} customer package`;
      try {
        await runCustomerPackageEvidenceChain({
          http, auth, wait, plan: options.customer,
          packageName: customerPackageName(options.customer, options.customerPackageName),
          versionLabel: String(options.customerVersionLabel || "").trim() || "1.0.0",
          workspaceName: customerWorkspaceName,
          provider, billingMode, computePlanId, storagePlanId,
          sourceSha: options.sourceSha,
          pollIntervalMs, pollAttempts,
          readback: customerReadback,
          fetchEntry: (url, headers) => customerEntryRequest(url, headers, caFile),
          putPart: (url, init) => customerUploadRequest(url, init, caFile),
        });
        record("customer_package", customerReadback.degradationReason ? "degraded" : "confirmed", {
          evidenceLevel: String(customerReadback.evidenceLevel || ""),
          requestedEvidenceLevel: String(customerReadback.requestedEvidenceLevel || ""),
          buildJobId: String(customerReadback.build?.buildJobId || ""),
          capabilityVersionId: String(customerReadback.deployedVersion?.capabilityVersionId || ""),
          workspaceId: String(customerReadback.customerOrder?.workspaceId || ""),
        });
        // A package that cannot carry this run's probe reaches the weaker
        // evidence level on purpose. The run says so and stops short of a pass
        // it did not earn.
        if (customerReadback.degradationReason) {
          customerBoundary = { code: "local_v2_customer_evidence_degraded", detail: customerReadback.degradationReason, stage: "customer_package" };
        }
      } catch (error) {
        customerBoundary = { code: String(error?.code || "local_v2_customer_chain_failed"), detail: redactedError(error), stage: "customer_package" };
        record("customer_package", "stopped", { code: customerBoundary.code, detail: customerBoundary.detail });
        // What the chain bought before it stopped leaves the installation with
        // it, so the owner is asked to delete it before the run ends.
        const boughtWorkspaceId = String(customerReadback.customerOrder?.workspaceId || "");
        if (boughtWorkspaceId) {
          try {
            await deleteWorkspaceOnOwnerRoute(http, auth, {
              workspaceId: boughtWorkspaceId, confirmationName: customerWorkspaceName,
              idempotencyKey: `local-v2-customer-delete:${options.sourceSha}:${String(customerReadback.upload?.packageVersionId || "stopped")}`, wait,
            });
          } catch {
            // The host resources are cleaned below whatever the owner says.
          }
        }
      } finally {
        // Whatever this chain bought leaves the host with it. The qualification
        // is about an installation that holds nothing it did not declare.
        const customerWorkspaceId = String(customerReadback.customerOrder?.workspaceId || "");
        if (customerWorkspaceId) {
          await cleanupLocalQualificationResources(readbacks.accountBinding.accountId, customerWorkspaceId);
        }
      }
    }
    stage = "quote";
    const quote = (await http.json(binding.endpoints.quotes, {
      method: "POST", headers: { "idempotency-key": `local-v2-quote:${options.sourceSha}:${workspaceName}` },
      body: { purpose: "deploy", computePlanId, storagePlanId, periodMonths: 1, applicationSelection: { kind: "opl_app", runtimeVersionId } }
    }, auth, [200, 201])).payload;
    if (!quote?.id) throw namedQualificationError("local_v2_quote_invalid", "the Workspace owner returned no quote identity");
    if (quote.purpose !== "deploy" || quote.runtimeVersionId !== runtimeVersionId ||
        quote.computePlanId !== computePlanId || quote.storagePlanId !== storagePlanId || quote.periodMonths !== 1) {
      throw namedQualificationError("local_v2_quote_invalid", "the quote does not identify the declared application, plans and period");
    }
    const totalUSDMicros = String(quote.totalUSDMicros ?? "");
    if (provider !== "local-docker" || billingMode !== "LOCAL_NO_CHARGE" || totalUSDMicros !== "0") {
      throw namedQualificationError("local_v2_non_local_no_charge_quote", "the Workspace quote is not local-docker LOCAL_NO_CHARGE at zero total");
    }
    readbacks.quote = { id: quote.id, provider, billingMode, totalUSDMicros, status: String(quote.status || ""), purpose: quote.purpose, runtimeVersionId: quote.runtimeVersionId };
    record("quote", "confirmed", { quoteId: quote.id, totalUSDMicros: readbacks.quote.totalUSDMicros });
    stage = "order";
    const orderKey = `local-v2-order:${options.sourceSha}:${workspaceName}`;
    const orderBody = { name: workspaceName, quoteId: quote.id, renewalMode: "manual" };
    const order = (await http.json(binding.endpoints.workspaces, {
      method: "POST", headers: { "idempotency-key": orderKey }, body: orderBody
    }, auth, [202])).payload;
    const operationId = String(order?.operationId || "");
    const workspaceId = String(order?.resourceId || "");
    if (!operationId || !workspaceId) throw namedQualificationError("local_v2_order_identity_missing", "the Workspace owner returned no operation identity");
    validateLocalV2Replay(order, operationId, workspaceId);
    const workspaceSQL = `'${workspaceId.replace(/'/g, "''")}'`;
    readbacks.order = { operationId, workspaceId };
    record("order", "accepted", { operationId, workspaceId });
    stage = "workspace_operation";
    let operation = null;
    let unchanged = 0;
    let previousState = "";
    let polls = 0;
    let replays = 0;
    let activationReached = null;
    const now = dependencies.now || Date.now;
    const pollDeadline = now() + Math.max(1000, pollAttempts * pollIntervalMs);
    const pollRequest = (path, init = {}, statuses = [200]) => {
      const remaining = pollDeadline - now();
      if (remaining <= 0) throw namedQualificationError("local_v2_operation_poll_budget_exhausted", "the Workspace operation polling deadline expired");
      return http.json(path, { ...init, signal: AbortSignal.timeout(remaining) }, auth, statuses).catch((error) => {
        if (now() >= pollDeadline) throw namedQualificationError("local_v2_operation_poll_budget_exhausted", "the Workspace operation polling deadline expired");
        throw error;
      });
    };
    while (polls < pollAttempts && now() < pollDeadline) {
      operation = (await pollRequest(`${binding.endpoints.operations}/${encodeURIComponent(operationId)}`)).payload;
      polls += 1;
      if (now() >= pollDeadline) break;
      // The activation boundary is the state this installation declares for a
      // Local no-charge order, so it is read as soon as the owner reaches it
      // instead of being waited out as a stall.
      if (String(operation?.status || "") === "awaiting_confirmation" && String(operation?.stage || "") === LOCAL_V2_ACTIVATION_STAGE) {
        activationReached = operation;
        break;
      }
      if (LOCAL_V2_TERMINAL_OPERATION.has(String(operation?.status || ""))) break;
      const state = `${String(operation?.status || "")}:${String(operation?.stage || "")}`;
      unchanged = state === previousState ? unchanged + 1 : 0;
      previousState = state;
      if (unchanged >= LOCAL_V2_STALLED_POLLS) {
        const parked = String(operation?.status || "") === "awaiting_confirmation" && String(operation?.stage || "") !== "activation";
        if (!parked || replays >= recoveryReplays || polls >= pollAttempts) break;
        const replay = (await pollRequest(binding.endpoints.workspaces, {
          method: "POST", headers: { "idempotency-key": orderKey }, body: orderBody
        }, [202])).payload;
        validateLocalV2Replay(replay, operationId, workspaceId);
        replays += 1;
        unchanged = 0;
        previousState = "";
      }
      await wait(Math.min(pollIntervalMs, Math.max(0, pollDeadline - now())));
    }
    if (activationReached) operation = activationReached;
    const activationBoundary = String(operation?.status || "") === "awaiting_confirmation" && String(operation?.stage || "") === LOCAL_V2_ACTIVATION_STAGE;
    const launchCandidate = (String(operation?.status || "") === "succeeded" || activationBoundary) &&
      String(operation?.observationResult || "") === "confirmed";
    readbacks.operation = {
      status: String(operation?.status || ""), stage: String(operation?.stage || ""),
      observationResult: String(operation?.observationResult || ""), errorCode: String(operation?.errorCode || ""),
      activationBoundary, recoveryReplays: replays, polls
    };
    record("workspace_operation", launchCandidate ? "confirmed" : "stopped", readbacks.operation);
    stage = "workspace_readback";
    const workspaceRead = await http.request(`${binding.endpoints.workspaces}/${encodeURIComponent(workspaceId)}`, {}, auth);
    const workspace = workspaceRead.payload || {};
    readbacks.workspace = {
      id: String(workspace.id || ""), status: String(workspace.status || ""),
      applicationAvailability: String(workspace.applicationAvailability || ""), resourceReadiness: String(workspace.resourceReadiness || ""),
      accessUrl: String(workspace.accessUrl || ""), runtimeVersionId: String(workspace.runtimeVersionId || "")
    };
    const transactions = await http.request(`${binding.endpoints.workspaces}/${encodeURIComponent(workspaceId)}/transactions?limit=100`, {}, auth);
    readbacks.transactions = { status: transactions.response.status, items: Array.isArray(transactions.payload?.items) ? transactions.payload.items.length : 0 };
    if (typeof query === "function") {
      readbacks.ledger = { receipts: Number(await query("opl_ledger", `select count(*) from public.evidence_receipts where workspace_id=${workspaceSQL}`)) };
      readbacks.fabric = {
        resourceSets: Number(await query("opl_fabric", "select count(*) from fabric.resource_sets")),
        operations: Number(await query("opl_fabric", "select count(*) from fabric.operations"))
      };
    }
    record("owner_readback", "confirmed", { workspaceStatus: readbacks.workspace.status, transactions: readbacks.transactions.items });
    // The activation state is the park this installation declares, so reaching it
    // is a stop the run can read, not poll budget that ran out.
    const operationStopped = LOCAL_V2_TERMINAL_OPERATION.has(String(operation?.status || "")) ||
      activationBoundary || unchanged >= LOCAL_V2_STALLED_POLLS;
    if (now() >= pollDeadline || !operationStopped) {
      boundary = { code: "local_v2_operation_poll_budget_exhausted", detail: `the Workspace operation remained non-terminal after ${polls} polls`, stage: "workspace_operation" };
      stage = "workspace_operation";
    } else if (!launchCandidate) {
      boundary = localV2Boundary(operation, workspace);
      stage = "workspace_operation";
      const provider = await localV2ProviderBoundary(compose, composePrefix, startedAt, query);
      if (provider?.code) {
        boundary = { ...boundary, workspaceCode: boundary.code, code: provider.code, detail: provider.detail, providerLog: provider.line };
      }
    } else {
      let applicationOrigin = origin;
      if (contract.applicationAccess?.mode === "anonymous" && revision.exposurePolicy === "anonymous" &&
          localApplicationEntryPort(workspace.accessUrl) !== 0) {
        const ids = await exactLabelIDs("containers", readbacks.accountBinding.accountId, workspaceId);
        const containers = ids.length ? JSON.parse((await runProcess("docker", ["inspect", ...ids])).stdout) : [];
        const image = await imageInspection(readbacks.installation.workspaceImage);
        applicationOrigin = localV2AnonymousDockerOrigin({ entryUrl: workspace.accessUrl, workspaceId,
          accountId: readbacks.accountBinding.accountId, imageRef: readbacks.installation.workspaceImage,
          imageId: image.Id, revision, containers });
        readbacks.applicationRuntime = { imageRef: readbacks.installation.workspaceImage, imageId: image.Id,
          runtimeId: containers.find((value) => value.Config?.Labels?.["opl.fabric.kind"] === "application_runtime" &&
            value.Config?.Labels?.["opl.component.role"] === "main")?.Config?.Labels?.["opl.runtime.id"],
          origin: applicationOrigin, publishedPort: localApplicationEntryPort(workspace.accessUrl), providerBindingVerified: true };
      }
      const entryBoundary = localV2LaunchReadbackBoundary(workspace, workspaceId, applicationOrigin);
      if (!entryBoundary.ok) {
        boundary = entryBoundary;
        stage = "application_entry";
      } else {
        stage = "application_entry";
        try {
          const fetchEntry = dependencies.fetchEntry || (async (url) => {
            if (applicationOrigin !== origin) {
              const response = await fetch(url, { redirect: "manual", signal: ownerRequestSignal() });
              return { response, text: await response.text() };
            }
            const response = await caRequest(url, { method: "GET" }, new Headers(), undefined, readFileSync(caFile));
            return { response, text: await response.text() };
          });
          const opened = await fetchEntry(workspace.accessUrl);
          readbacks.applicationEntry = { url: workspace.accessUrl, status: opened.response.status };
          if (opened.response.status !== 200) throw new Error(`the declared application entry returned HTTP ${opened.response.status}`);
          record("application_entry", "confirmed", readbacks.applicationEntry);
        } catch (error) {
          boundary = { code: "local_v2_application_entry_unreachable", detail: String(error?.message || error), stage };
        }
      }
    }
    // DELETE is authorized only after the Workspace and Serve readiness and the
    // declared-origin entry boundary are confirmed. A missing, mismatched or
    // unreachable entry keeps the boundary and leaves the Workspace intact.
    if (launchCandidate && !boundary) {
      // A restart is the other half of recovery: a lost response is replayed against
      // a live owner, and a restarted installation must answer the same identities.
      // The same owner set the live chain restarts is restarted here, and every
      // readback is compared with the snapshot taken before it stopped.
      const beforeRestart = {
        // The operation's status advances while its owner keeps working, so the
        // identity that must survive a restart is the one the owner minted.
        operation: {
          operationId: String(operation?.operationId || ""), owner: String(operation?.owner || ""),
          kind: String(operation?.kind || ""), resourceId: String(operation?.resourceId || ""),
        },
        workspace: { ...readbacks.workspace },
        receipts: Number(readbacks.ledger?.receipts || 0),
      };
      stage = "restart_continuity";
      const restartStarted = Date.now();
      // This caller's compose wrapper takes the raw docker arguments and the
      // project prefix travels as its own argument list.
      const composeProject = (args, settings = {}) => compose([...composePrefix, ...args], settings);
      await composeProject(["restart", "workspace", "console-bff", "tenant", "serve", "fabric", "ledger"], { capture: false });
      await waitForCompose(composeProject);
      // The session belonged to the process that stopped, so the run signs in
      // again on the same declared customer before it reads anything back. The
      // owner route answers only the CloudIdentity session the BFF issues.
      const restartedSession = await cloudIdentityLogin(http, origin, readbacks.installation.qualificationEmail,
        String(values.OPL_QUALIFICATION_USER_PASSWORD || ""), `local-v2-restart:${options.sourceSha}:${workspaceName}`);
      const restartedAuth = { cookie: restartedSession.cookie, ownerCsrf: restartedSession.ownerCsrf, csrf: "" };
      const afterRestartWorkspace = await http.json(`${binding.endpoints.workspaces}/${encodeURIComponent(workspaceId)}`, {}, restartedAuth);
      const afterRestartOperation = await http.json(`${binding.endpoints.operations}/${encodeURIComponent(operationId)}`, {}, restartedAuth);
      const afterRestartWorkspaceView = {
        id: String(afterRestartWorkspace.payload?.id || ""),
        status: String(afterRestartWorkspace.payload?.status || ""),
        applicationAvailability: String(afterRestartWorkspace.payload?.applicationAvailability || ""),
        runtimeVersionId: String(afterRestartWorkspace.payload?.runtimeVersionId || ""),
      };
      const afterRestartReceipts = typeof query === "function"
        ? Number(await query("opl_ledger", `select count(*) from public.evidence_receipts where workspace_id=${workspaceSQL}`))
        : beforeRestart.receipts;
      readbacks.restart = {
        performed: true,
        seconds: Math.round((Date.now() - restartStarted) / 1000),
        operationStable: String(afterRestartOperation.payload?.operationId || "") === beforeRestart.operation.operationId &&
          String(afterRestartOperation.payload?.owner || "") === beforeRestart.operation.owner &&
          String(afterRestartOperation.payload?.kind || "") === beforeRestart.operation.kind &&
          String(afterRestartOperation.payload?.resourceId || "") === beforeRestart.operation.resourceId,
        workspaceStable: afterRestartWorkspaceView.id === beforeRestart.workspace.id &&
          afterRestartWorkspaceView.status === beforeRestart.workspace.status &&
          afterRestartWorkspaceView.runtimeVersionId === beforeRestart.workspace.runtimeVersionId,
        runtimeStable: afterRestartWorkspaceView.applicationAvailability === beforeRestart.workspace.applicationAvailability,
        receiptStable: afterRestartReceipts === beforeRestart.receipts,
        sessionReestablished: String(restartedAuth.cookie || "").includes("opl_console_session="),
      };
      auth = restartedAuth;
      const restartStable = readbacks.restart.sessionReestablished && readbacks.restart.operationStable &&
        readbacks.restart.workspaceStable && readbacks.restart.runtimeStable && readbacks.restart.receiptStable;
      record("restart_continuity", restartStable ? "confirmed" : "stopped", readbacks.restart);
      if (!restartStable) {
        throw namedQualificationError("local_v2_restart_continuity_lost",
          "the installation answered different identities after its restart");
      }
      const launchBoundary = boundary;
      stage = "workspace_delete";
      const deletion = await deleteWorkspaceOnOwnerRoute(http, auth, {
        workspaceId, confirmationName: workspaceName,
        idempotencyKey: `local-v2-delete:${options.sourceSha}:${workspaceName}`, wait
      });
      readbacks.deletion = { operationId: String(deletion?.operationId || ""), status: String(deletion?.status || "") };
      // The owner answers the deleted Workspace with the tombstone it keeps for
      // financial obligations, so absence is the owner's own deleted state plus
      // the owners' receipts, never a 404 or an empty list.
      const tombstone = await http.request(`${binding.endpoints.workspaces}/${encodeURIComponent(workspaceId)}`, {}, auth);
      readbacks.deletion.workspaceStatus = String(tombstone.payload?.status || "");
      readbacks.deletion.workspaceAbsent = tombstone.payload?.status === "deleted";
      readbacks.deletion.routeAbsent = !String(tombstone.payload?.accessUrl || "").trim();
      // The owner's own deletion readback is recorded before it is judged, so a
      // refused readback still shows the owner's answer in the receipt.
      const ownerDeletion = (await http.json(
        `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deletion`, {}, auth,
      )).payload;
      readbacks.deletion.owner = ownerDeletion;
      const deletionEvidence = await readWorkspaceDeletionEvidence(http, auth, {
        workspaceId, operationId: readbacks.deletion.operationId,
        localNoCharge: String(readbacks.quote?.billingMode || readbacks.catalog?.billingMode || "") === "LOCAL_NO_CHARGE",
      });
      readbacks.deletion.resourceAbsent = deletionEvidence.resourceDeletionStatus === "confirmed";
      readbacks.deletion.dataAbsent = deletionEvidence.dataDeletionStatus === "confirmed";
      readbacks.deletion.refundStatus = String(deletionEvidence.refundStatus || "");
      readbacks.deletion.refundEvidence = String(deletionEvidence.refundStatus || "") === "not_applicable"
        ? "local_no_charge: no money moved, so no refund is owed"
        : "confirmed refund bound to its own operation";
      readbacks.deletion.refundOperationId = String(deletionEvidence.refundOperationId || "");
      readbacks.deletion.receiptId = String(deletionEvidence.deletionReceipt?.id || "");
      if (typeof query === "function") {
        readbacks.deletion.runtimeAbsent = Number(await query("opl_serve",
          `select count(*) from serve.agent_runtime_instances where workspace_id=${workspaceSQL} and status <> 'terminated'`)) === 0;
        readbacks.deletion.liveResources = Number(await query("opl_fabric",
          `select count(*) from fabric.resources r join fabric.resource_sets s on s.id=r.resource_set_id where s.workspace_id=${workspaceSQL} and r.deleted_at is null`));
        readbacks.deletion.ledgerReceipts = Number(await query("opl_ledger",
          `select count(*) from public.evidence_receipts where workspace_id=${workspaceSQL}`));
      }
      // The same command is replayed after it succeeded. The owner keys the
      // deletion by the idempotency key this caller chose, so a second send must
      // reach the same operation and produce no second effect: no new Ledger
      // receipt and no revived provider resource.
      const replay = await http.json(
        `/api/v2/workspaces/${encodeURIComponent(workspaceId)}`,
        { method: "DELETE", headers: { "idempotency-key": `local-v2-delete:${options.sourceSha}:${workspaceName}` },
          body: { confirmationName: workspaceName, acknowledgeDataDestruction: true } },
        auth, [200, 202, 404, 409],
      );
      const replayOperationId = String(replay.payload?.operationId || "");
      const receiptsAfterReplay = typeof query === "function"
        ? Number(await query("opl_ledger", `select count(*) from public.evidence_receipts where workspace_id=${workspaceSQL}`))
        : readbacks.deletion.ledgerReceipts;
      const resourcesAfterReplay = typeof query === "function"
        ? Number(await query("opl_fabric",
          `select count(*) from fabric.resources r join fabric.resource_sets s on s.id=r.resource_set_id where s.workspace_id=${workspaceSQL} and r.deleted_at is null`))
        : readbacks.deletion.liveResources;
      readbacks.deletion.retry = {
        status: replay.response.status,
        operationIdStable: replayOperationId === readbacks.deletion.operationId,
        ledgerReceiptsUnchanged: receiptsAfterReplay === readbacks.deletion.ledgerReceipts,
        liveResources: resourcesAfterReplay,
      };
      const retryClean = readbacks.deletion.retry.operationIdStable && readbacks.deletion.retry.ledgerReceiptsUnchanged &&
        readbacks.deletion.retry.liveResources === 0;
      // readWorkspaceDeletionEvidence has already bound the refund or its
      // no-charge not-applicable outcome to this exact deletion.
      const deleted = retryClean && readbacks.deletion.workspaceAbsent === true && readbacks.deletion.routeAbsent === true &&
        readbacks.deletion.runtimeAbsent === true && readbacks.deletion.resourceAbsent === true && readbacks.deletion.dataAbsent === true;
      record("workspace_delete", deleted ? "confirmed" : "stopped", readbacks.deletion);
      if (!deleted) {
        boundary = launchBoundary || { code: "local_v2_workspace_absence_unconfirmed", detail: "an owner still retains the deleted Workspace", stage };
      } else {
        boundary = launchBoundary;
      }
      if (boundary?.stage) stage = boundary.stage;
    }
  } catch (error) {
    failure = error;
    if (!boundary) boundary = { code: String(error?.code || "local_v2_qualification_incomplete"), detail: String(error?.message || error), stage };
  }
  // The customer package chain's boundary is the run's boundary. A run that
  // could not tie the deployed version to this upload, or that could not tie
  // the entry's content to it, has not earned a READY result.
  if (customerBoundary) {
    boundary = customerBoundary;
    stage = customerBoundary.stage || stage;
  }
  const result = {
    schemaVersion: 1,
    status: "NOT_READY",
    mode,
    completedAt: new Date().toISOString(),
    startedAt,
    source: { sha: String(options.sourceSha || ""), tree: sourceTree },
    command: receiptCommand(options),
    stage,
    errorCode: boundary?.code || "local_v2_qualification_incomplete",
    error: failure ? redactedError(failure) : { message: boundary?.detail || "the run stopped before a READY result", code: boundary?.code || "" },
    boundary: boundary ? { code: boundary.code, detail: boundary.detail, stage: boundary.stage || stage } : null,
    evidence: { mode, stages, readbacks },
    compose: { project: String(options.project || ""), files: binding.composeFiles, prefix: composePrefix },
    retainedWritersUsed: [],
    receiptCompatibility: { nativeSchemaVersion: 2, legacyReceiptCompatible: false },
    deferred: [...deferredCloudGates]
  };
  if (!failure && !boundary && readbacks.applicationRuntime?.providerBindingVerified === true) {
    try {
      validateQualificationSourceIdentity(sourceIdentity, await readSource(), options.sourceSha);
      const residuals = await residualCounts(readbacks.accountBinding.accountId, readbacks.order.workspaceId);
      const ready = createLocalV2ReadyReceipt(result, {
        sha: sourceIdentity.sha, tree: sourceIdentity.tree, clean: sourceIdentity.clean,
      }, admittedImages, processes, residuals);
      await writeJSONAtomic(`${options.receiptPath}.diagnostic.json`, {
        ...result, status: "EVIDENCE_COLLECTED", errorCode: "", error: null,
        receiptCompatibility: { nativeSchemaVersion: 2, legacyReceiptCompatible: false },
      });
      await writeJSONAtomic(options.receiptPath, ready);
      return ready;
    } catch (error) {
      result.stage = "receipt_admission";
      result.errorCode = String(error?.code || "local_v2_ready_receipt_invalid");
      result.error = redactedError(error);
      result.boundary = { code: result.errorCode, detail: result.error, stage: result.stage };
    }
  }
  await writeJSONAtomic(options.receiptPath, result);
  return result;
}

// Declare one owner fact, or read the one an earlier run already declared. The
// owner mints the identity and the run uses it. A collection whose administrator
// route is write-only is read through the route that lists it, and a replayed
// declaration that the owner reports as a conflict is read back rather than
// created twice.
async function declareOrRead(http, auth, collectionPath, matchKey, matchValue, body, { readPath = collectionPath } = {}) {
  const list = async () => {
    const page = (await http.json(`${readPath}?limit=100`, {}, auth)).payload;
    return Array.isArray(page?.items) ? page.items : [];
  };
  const existing = (await list()).find((item) => String(item?.[matchKey] || "") === String(matchValue));
  if (existing?.id) return String(existing.id);
  const created = await http.request(collectionPath, {
    method: "POST",
    headers: { "idempotency-key": `local-v2-declare:${matchValue}` },
    body
  }, auth);
  const status = created.response.status;
  if (status === 409) {
    const declared = (await list()).find((item) => String(item?.[matchKey] || "") === String(matchValue));
    if (!declared?.id) {
      throw namedQualificationError("local_v2_declaration_conflict", `the owner reported a conflict for ${matchValue} but lists no such declaration`);
    }
    return String(declared.id);
  }
  if (status !== 200 && status !== 201) {
    throw namedQualificationError("local_v2_declaration_failed", `declaring ${matchValue} returned ${status}: ${String(created.text || "").slice(0, 300)}`);
  }
  const id = String(created.payload?.id || created.payload?.resourceId || "");
  if (id) return id;
  const declared = (await list()).find((item) => String(item?.[matchKey] || "") === String(matchValue));
  if (!declared?.id) throw namedQualificationError("local_v2_declaration_missing", `the owner published no identity for ${matchValue}`);
  return String(declared.id);
}

async function catalogIds(http, auth, path) {
  const page = (await http.json(`${path}?limit=100`, {}, auth)).payload;
  return (Array.isArray(page?.items) ? page.items : []).map((item) => String(item?.id || "")).filter(Boolean);
}

// ---------------------------------------------------------------------------
// The customer package evidence chain.
//
// The qualification already proves that the installation prices a Workspace,
// buys it, serves it and deletes it. This section adds the other half of the
// customer loop: the package a customer uploads, the explicit Build it becomes,
// the exact Capability Version that Build registers, the Workspace that runs
// that exact version, and the content that Workspace's entry answers with.
//
// The tool carries no agent name, no package identity, no probe path and no
// expected response content. The package and the probe path are inputs, the
// only content the tool knows is the nonce it mints for this run, and the
// entry is judged by whether it answers with that nonce.
// ---------------------------------------------------------------------------

export const CUSTOMER_EVIDENCE_LEVEL_CONTENT = "content-provenance";
export const CUSTOMER_EVIDENCE_LEVEL_ARTIFACT = "artifact-identity";
export const CUSTOMER_EVIDENCE_LEVELS = Object.freeze([CUSTOMER_EVIDENCE_LEVEL_CONTENT, CUSTOMER_EVIDENCE_LEVEL_ARTIFACT]);

// The states a Build reaches that are not "still working".
const CUSTOMER_BUILD_TERMINAL = Object.freeze(["succeeded", "failed", "cancelled"]);

// The name the owner accepts for a package.
const CUSTOMER_PACKAGE_NAME_PATTERN = /^[A-Za-z0-9._-]{1,128}$/;

// The package name is derived from the package the caller named, so the run
// carries no name of its own and a name the owner refuses is reported before
// anything is uploaded.
export function customerPackageName(plan, declared) {
  const fallback = basename(plan.mode === "directory" ? plan.packageDir : plan.packageArchive).replace(/\.[A-Za-z0-9]+$/, "");
  const name = String(declared || "").trim() || fallback;
  if (!CUSTOMER_PACKAGE_NAME_PATTERN.test(name)) {
    throw namedQualificationError("local_v2_customer_package_name_invalid",
      "the customer package name must be 1-128 letters, digits, dots, hyphens or underscores");
  }
  return name;
}

// The probe path is the carrier of the run's content provenance, and it is an
// input: this tool never names a file inside a package it did not receive.
export function customerPackageProbePath(probePath) {
  const path = String(probePath || "").trim();
  if (!path) {
    throw namedQualificationError("local_v2_customer_probe_path_required", "the customer package must name the probe path that carries the run nonce");
  }
  if (path.startsWith("/") || path.includes("\\") || path.split("/").some((part) => !part || part === "." || part === "..")) {
    throw namedQualificationError("local_v2_customer_probe_path_invalid", "the customer package probe path must be a relative path inside the package");
  }
  return path;
}

// The entry the recipe starts, relative to the directory the package is
// copied into. The recipe names an absolute path inside the image and the
// target path the package is copied to, so the difference is the file the
// package has to carry.
export function customerEntrypointRelativePath(targetPath, entrypoint) {
  const entry = Array.isArray(entrypoint) ? entrypoint.map((value) => String(value)).filter(Boolean) : [];
  const first = entry[0] || "";
  if (!first) return "";
  const target = String(targetPath || "").replace(/\/+$/, "");
  if (target && first.startsWith(`${target}/`)) return first.slice(target.length + 1);
  return first.replace(/^\/+/, "");
}

// The package layout against the Runtime's recipe.
//
// The Build Runner copies one directory out of the package, and it refuses a
// source root of ".", ".." or an absolute one. The probe that carries the
// nonce and the runtime the recipe starts must both live inside that one
// directory, so the package is checked against the root the recipe declares
// before the Build is asked for. A package that spreads its runtime and its
// probe over two roots is refused here rather than inside the Builder.
export function assertCustomerPackageLayout({ entries, sourceRoot, targetPath, entrypoint, probePath } = {}) {
  const root = String(sourceRoot || "");
  if (!root) {
    throw namedQualificationError("local_v2_customer_build_recipe_unavailable", "the approved Runtime declares no package source root, so a Build cannot copy this package");
  }
  if (root === "." || root === ".." || root.startsWith("/") || root.startsWith("../") || root.split("/").some((part) => !part || part === "..")) {
    throw namedQualificationError("local_v2_customer_build_recipe_source_root_invalid", "the approved Runtime declares a package source root the Build Runner refuses");
  }
  const probe = String(probePath || "").trim();
  if (probe) {
    customerPackageProbePath(probe);
    if (!probe.startsWith(`${root}/`)) {
      throw namedQualificationError("local_v2_customer_probe_outside_source_root",
        `the package carries its probe at ${probe}, the approved Runtime copies the package from ${root}`);
    }
  }
  const relative = customerEntrypointRelativePath(targetPath, entrypoint);
  const runtimePath = relative ? `${root}/${relative}` : "";
  if (runtimePath && Array.isArray(entries) && !entries.includes(runtimePath)) {
    throw namedQualificationError("local_v2_customer_runtime_outside_source_root",
      `the package carries no ${runtimePath}, the approved Runtime starts ${runtimePath}`);
  }
  return { sourceRoot: root, probePath: probe, runtimePath };
}

// The plan is what the caller declared, and it is decided before anything is
// uploaded: which package is uploaded, where its probe sits, which evidence
// level the caller asked for and which one this package can reach. A package
// that cannot carry the run's nonce reaches the weaker level, and the reason
// travels with the run instead of the run reporting a pass it did not earn.
export function customerEvidencePlan({ packageDir = "", packageArchive = "", probePath = "", expectedEvidenceLevel = "" } = {}) {
  const directory = String(packageDir || "").trim();
  const archive = String(packageArchive || "").trim();
  if (directory && archive) {
    throw namedQualificationError("local_v2_customer_package_ambiguous", "the customer package is declared either as a directory or as an archive, not both");
  }
  if (!directory && !archive) return { declared: false, mode: "", packageDir: "", packageArchive: "", probePath: "", sourceRoot: "", requestedEvidenceLevel: "", achievableEvidenceLevel: "", degradationReason: "" };
  const mode = directory ? "directory" : "archive";
  const probe = String(probePath || "").trim();
  if (mode === "directory") {
    if (!probe) {
      throw namedQualificationError("local_v2_customer_probe_path_required", "a customer package directory must name the probe path that carries the run nonce");
    }
    customerPackageProbePath(probe);
  }
  const requested = String(expectedEvidenceLevel || "").trim() ||
    (mode === "directory" ? CUSTOMER_EVIDENCE_LEVEL_CONTENT : CUSTOMER_EVIDENCE_LEVEL_ARTIFACT);
  if (!CUSTOMER_EVIDENCE_LEVELS.includes(requested)) {
    throw namedQualificationError("local_v2_customer_evidence_level_invalid", `the customer evidence level must be one of ${CUSTOMER_EVIDENCE_LEVELS.join(", ")}`);
  }
  // An archive is the bytes the caller handed over. This run cannot write its
  // nonce into them, so the entry content cannot be tied to this upload.
  const achievable = mode === "directory" ? CUSTOMER_EVIDENCE_LEVEL_CONTENT : CUSTOMER_EVIDENCE_LEVEL_ARTIFACT;
  const degradationReason = achievable === requested ? "" :
    "the supplied customer package archive is immutable and cannot carry this run's probe, so the entry content cannot be tied to this upload";
  return {
    declared: true, mode, packageDir: directory, packageArchive: archive, probePath: probe, sourceRoot: "",
    requestedEvidenceLevel: requested, achievableEvidenceLevel: achievable, degradationReason,
  };
}

// The upload completion is the owner's own operation. The owner, the kind, the
// version it names and the status it reached are all asserted, because a
// completion that names another version is a completion of another upload.
export function assertCustomerCompleteUploadOperation(operation, { packageVersionId } = {}) {
  const owner = String(operation?.owner || "");
  const kind = String(operation?.kind || "");
  const operationId = String(operation?.operationId || "");
  const resourceId = String(operation?.resourceId || "");
  const status = String(operation?.status || "");
  const stage = String(operation?.stage || "");
  const observationResult = String(operation?.observationResult || "");
  if (!operationId || owner !== "capability" || kind !== "complete_upload" || !resourceId || resourceId !== String(packageVersionId || "") || status !== "succeeded" || stage !== "succeeded") {
    throw namedQualificationError("local_v2_customer_upload_operation_invalid",
      "the upload completion operation does not name the capability owner, the complete_upload kind, the uploaded version and a succeeded status and stage");
  }
  if (observationResult !== "confirmed") {
    throw namedQualificationError("local_v2_customer_upload_operation_unconfirmed", "the upload completion operation is not confirmed");
  }
  return { owner, kind, resourceId, status, stage, observationResult, operationId };
}

// The uploaded version, read back from the owner that stored it.
export function assertCustomerPackageVersionReadback(readback, expected = {}) {
  const fields = {
    id: String(readback?.id || ""),
    packageId: String(readback?.packageId || ""),
    versionLabel: String(readback?.versionLabel || ""),
    sha256: String(readback?.sha256 || ""),
    sizeBytes: String(readback?.sizeBytes ?? ""),
    status: String(readback?.status || ""),
  };
  const sameDigest = (left, right) => String(left || "").replace(/^sha256:/, "") === String(right || "").replace(/^sha256:/, "");
  if (fields.id !== String(expected.id || "") || fields.packageId !== String(expected.packageId || "") ||
      fields.versionLabel !== String(expected.versionLabel || "") || !sameDigest(fields.sha256, expected.sha256) ||
      fields.sizeBytes !== String(expected.sizeBytes ?? "") || fields.status !== "uploaded") {
    throw namedQualificationError("local_v2_customer_package_version_mismatch",
      "the read back package version does not match the uploaded bytes");
  }
  return fields;
}

// The deployed version identity.
//
// The Build this run asked for, the Capability Version that Build registered
// and the Workspace that runs it are three different facts, and this is the
// lock between them: the version is never chosen by readiness, it is the one
// the Build names, the Build the Capability Version names, and the version the
// Workspace runs.
export function assertCustomerDeployedVersionIdentity({ build, capabilityVersion, workspace } = {}) {
  const buildJobId = String(build?.id || "");
  const resultCapabilityVersionId = String(build?.resultCapabilityVersionId || "");
  const buildPackageVersionId = String(build?.packageVersionId || "");
  const capabilityVersionId = String(capabilityVersion?.id || "");
  const workspaceCapabilityVersionId = String(workspace?.capabilityVersionId || "");
  const capabilityBuildJobId = String(capabilityVersion?.buildJobId || "");
  const capabilityPackageVersionId = String(capabilityVersion?.packageVersionId || "");
  if (!buildJobId || !resultCapabilityVersionId || capabilityVersionId !== resultCapabilityVersionId ||
      capabilityBuildJobId !== buildJobId || !buildPackageVersionId ||
      capabilityPackageVersionId !== buildPackageVersionId ||
      workspaceCapabilityVersionId !== capabilityVersionId) {
    throw namedQualificationError("local_v2_customer_deployed_version_unbound",
      "the deployed Capability Version is not the one this Build registered");
  }
  return { buildJobId, capabilityVersionId, workspaceCapabilityVersionId, packageVersionId: capabilityPackageVersionId };
}

// The entry content.
//
// The only content the tool knows is the nonce it minted for this run and
// wrote into the uploaded package. An answer that carries it can only come
// from this upload, so the entry is judged by that and by nothing else.
export function assertCustomerEntryContentProvenance(entryText, nonce) {
  const body = String(entryText || "");
  const value = String(nonce || "");
  if (!value || !body.includes(value)) {
    throw namedQualificationError("local_v2_customer_entry_content_unbound", "the published entry does not answer with this run's probe");
  }
  return { matched: true, responseBytes: Buffer.byteLength(body, "utf8") };
}

// The purchase guard.
//
// A quote that is not the local provider's free quote is refused before the
// purchase, so a paid quote can never reach a purchase that reports itself as
// a passed qualification.
export function assertCustomerLocalNoChargeQuote(quote, { provider, billingMode } = {}) {
  const totalUSDMicros = String(quote?.totalUSDMicros ?? "");
  if (String(provider || "") !== "local-docker" || String(billingMode || "") !== "LOCAL_NO_CHARGE" || totalUSDMicros !== "0") {
    throw namedQualificationError("local_v2_customer_quote_not_local_no_charge",
      "the customer quote is not local-docker LOCAL_NO_CHARGE at zero total");
  }
  return { provider: String(provider), billingMode: String(billingMode), totalUSDMicros };
}

// The Workspace identity is the operation's resource id. The owner mints the
// resource and names it there; the body of a purchase carries no identity of
// its own.
export function customerWorkspaceIdentity(operation) {
  const operationId = String(operation?.operationId || "");
  const workspaceId = String(operation?.resourceId || "");
  if (!operationId || !workspaceId) {
    throw namedQualificationError("local_v2_customer_workspace_identity_missing", "the purchase returned no operation identity");
  }
  return { operationId, workspaceId };
}

// The package archive.
//
// The package directory is mirrored into an archive and the probe file is
// written into it, so the nonce travels with the bytes the platform builds.
// The archive is written here because the platform uploads a package, not a
// directory.
const CRC32_TABLE = (() => {
  const table = new Int32Array(256);
  for (let index = 0; index < 256; index += 1) {
    let value = index;
    for (let bit = 0; bit < 8; bit += 1) {
      value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
    }
    table[index] = value;
  }
  return table;
})();

export function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value = CRC32_TABLE[(value ^ byte) & 0xff] ^ (value >>> 8);
  }
  return (value ^ 0xffffffff) >>> 0;
}

function zipArchive(entries) {
  const chunks = [];
  const directory = [];
  let offset = 0;
  for (const entry of entries) {
    const name = Buffer.from(entry.name, "utf8");
    const data = entry.data;
    const checksum = crc32(data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0, 6);
    local.writeUInt16LE(0, 8);
    local.writeUInt16LE(0, 10);
    local.writeUInt16LE(0x0021, 12);
    local.writeUInt32LE(checksum, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(name.length, 26);
    local.writeUInt16LE(0, 28);
    chunks.push(local, name, data);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(0x0314, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0, 8);
    central.writeUInt16LE(0, 10);
    central.writeUInt16LE(0, 12);
    central.writeUInt16LE(0x0021, 14);
    central.writeUInt32LE(checksum, 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt16LE(0, 30);
    central.writeUInt16LE(0, 32);
    central.writeUInt16LE(0, 34);
    central.writeUInt16LE(0, 36);
    central.writeUInt32LE(((0o100000 | (entry.mode & 0o777)) << 16) >>> 0, 38);
    central.writeUInt32LE(offset, 42);
    directory.push(central, name);
    offset += local.length + name.length + data.length;
  }
  const directoryBytes = Buffer.concat(directory);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(0, 4);
  end.writeUInt16LE(0, 6);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(directoryBytes.length, 12);
  end.writeUInt32LE(offset, 16);
  end.writeUInt16LE(0, 20);
  return Buffer.concat([...chunks, directoryBytes, end]);
}

function safeArchivePath(name) {
  const normalized = String(name || "").replace(/\\/g, "/").replace(/^\.\//, "");
  if (!normalized || normalized.startsWith("/") || normalized.includes("..") || normalized.includes(":")) {
    throw namedQualificationError("local_v2_customer_package_path_invalid", `the package entry path is not a safe relative path: ${name}`);
  }
  return normalized;
}

async function collectPackageFiles(directory, prefix = "") {
  const found = [];
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries.sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0))) {
    const absolute = join(directory, entry.name);
    const name = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) {
      found.push(...await collectPackageFiles(absolute, name));
      continue;
    }
    if (!entry.isFile()) continue;
    const info = await stat(absolute);
    found.push({ name: safeArchivePath(name), data: await readFile(absolute), mode: info.mode });
  }
  return found;
}

// The uploaded bytes. The directory is mirrored into an archive and the probe
// file is written into it, so the nonce the entry must answer with travels
// with the bytes the platform builds.
export async function buildCustomerPackageArchive({ packageDir, probePath, probeContent }) {
  const root = resolve(packageDir);
  const files = await collectPackageFiles(root);
  const probe = safeArchivePath(probePath);
  const entries = files.filter((file) => file.name !== probe).map((file) => ({ ...file }));
  entries.push({ name: probe, data: Buffer.from(`${probeContent}\n`, "utf8"), mode: 0o644 });
  entries.sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0));
  const bytes = zipArchive(entries);
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  return { bytes, digest, entries: entries.map((entry) => entry.name) };
}

// The archive the caller handed over. It is uploaded as it is.
export async function readCustomerPackageArchive(archivePath) {
  const bytes = await readFile(archivePath);
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  return { bytes, digest, entries: [] };
}

// The chain.
//
// It runs against the installation the qualification just built and it owns
// every step of the customer loop: upload, read back, explicit Build, the
// exact Capability Version that Build registered, the purchase of a Workspace
// that runs that version, the content its entry answers with, and the deletion
// of what it bought.
export async function runCustomerPackageEvidenceChain({
  http, auth, wait, plan, packageName, versionLabel, workspaceName, provider, billingMode,
  computePlanId, storagePlanId, sourceSha, pollIntervalMs = 3000, pollAttempts = 100, readback = {},
  fetchEntry = null, putPart = null,
}) {
  // The nonce is minted for this run and written into the package before the
  // package is uploaded. The receipt keeps its digest and never its value.
  const nonce = randomUUID();
  const nonceDigest = `sha256:${createHash("sha256").update(nonce).digest("hex")}`;
  readback.evidenceLevel = plan.achievableEvidenceLevel;
  readback.requestedEvidenceLevel = plan.requestedEvidenceLevel;
  readback.degradationReason = plan.degradationReason;
  readback.nonceDigest = nonceDigest;

  const archive = plan.mode === "directory"
    ? await buildCustomerPackageArchive({ packageDir: plan.packageDir, probePath: plan.probePath, probeContent: nonce })
    : await readCustomerPackageArchive(plan.packageArchive);
  readback.package = {
    name: packageName, versionLabel, source: plan.mode,
    sizeBytes: String(archive.bytes.length), sha256: archive.digest, entries: archive.entries.length,
  };

  // The namespace the package belongs to.
  const namespaces = (await http.json("/api/v2/namespaces", {}, auth)).payload;
  const namespaceItems = Array.isArray(namespaces?.items) ? namespaces.items : [];
  let namespaceId = String(namespaceItems.find((item) => item?.isDefault)?.id || namespaceItems[0]?.id || "");
  if (!namespaceId) {
    const created = (await http.json("/api/v2/namespaces", {
      method: "POST", headers: { "idempotency-key": `local-v2-customer-namespace:${sourceSha}` }, body: { name: "local-v2-qualification" },
    }, auth, [200, 201, 202])).payload;
    namespaceId = String(created?.id || "");
  }
  if (!namespaceId) throw namedQualificationError("local_v2_customer_namespace_unavailable", "the owner holds no namespace for the uploaded package");
  readback.namespace = { id: namespaceId };

  // The package.
  const packagePayload = (await http.json("/api/v2/packages", {
    method: "POST",
    headers: { "idempotency-key": `local-v2-customer-package-create:${sourceSha}:${packageName}` },
    body: { namespaceId, name: packageName, description: `uploaded by the local v2 qualification on behalf of ${packageName}` },
  }, auth, [200, 201, 202])).payload;
  const packageId = String(packagePayload?.id || "");
  if (!packageId) throw namedQualificationError("local_v2_customer_package_unavailable", "the owner created no package for the uploaded bytes");
  readback.package.id = packageId;

  // The upload session.
  const session = (await http.json(`/api/v2/packages/${encodeURIComponent(packageId)}/uploads`, {
    method: "POST",
    headers: { "idempotency-key": `local-v2-customer-upload:${sourceSha}:${packageName}` },
    body: { versionLabel, fileName: `${packageName}.zip`, sizeBytes: String(archive.bytes.length), sha256: archive.digest },
  }, auth, [200, 201, 202])).payload;
  const uploadId = String(session?.id || "");
  const packageVersionId = String(session?.packageVersionId || "");
  const partSize = Number(session?.partSizeBytes || 0);
  if (!uploadId || !packageVersionId) {
    throw namedQualificationError("local_v2_customer_upload_session_unavailable", "the owner created no upload session for the package");
  }
  readback.upload = { id: uploadId, packageVersionId, partSizeBytes: String(partSize) };

  // The parts. The owner authorizes each part and the bytes go to the URL it
  // names, with the checksum header it asked for.
  const parts = [];
  const partCount = partSize > 0 ? Math.ceil(archive.bytes.length / partSize) : 1;
  for (let number = 1; number <= partCount; number += 1) {
    const slice = archive.bytes.subarray((number - 1) * partSize, number * partSize);
    const partDigest = `sha256:${createHash("sha256").update(slice).digest("hex")}`;
    const permit = (await http.json(`/api/v2/uploads/${encodeURIComponent(uploadId)}/parts`, {
      method: "POST",
      headers: { "idempotency-key": `local-v2-customer-part:${sourceSha}:${uploadId}:${number}` },
      body: { partNumber: number, sizeBytes: String(slice.length), sha256: partDigest },
    }, auth, [200, 201, 202])).payload;
    const url = String(permit?.url || "");
    if (!url) throw namedQualificationError("local_v2_customer_upload_part_unauthorized", `the owner authorized no URL for part ${number}`);
    const putHeaders = { "content-type": String(permit?.contentType || "application/octet-stream") };
    const checksumName = String(permit?.requiredChecksumHeaderName || "");
    if (checksumName) putHeaders[checksumName.toLowerCase()] = String(permit?.requiredChecksumHeaderValue || "");
    const putInit = { method: String(permit?.method || "PUT"), headers: putHeaders, body: slice };
    const put = putPart ? await putPart(url, putInit) : await fetch(url, putInit);
    if (put.status >= 300) {
      throw namedQualificationError("local_v2_customer_upload_part_refused", `part ${number} was refused with HTTP ${put.status}`);
    }
    const etag = String(put.headers.get("etag") || "").trim() || partDigest.replace(/^sha256:/, "");
    parts.push({ partNumber: number, etag, sizeBytes: String(slice.length), sha256: partDigest });
  }

  // The completion. The owner's operation names the version it completed.
  const completed = (await http.json(`/api/v2/uploads/${encodeURIComponent(uploadId)}/complete`, {
    method: "POST",
    headers: { "idempotency-key": `local-v2-customer-complete:${sourceSha}:${uploadId}` },
    body: { parts },
  }, auth, [200, 201, 202])).payload;
  readback.upload = { ...readback.upload, ...assertCustomerCompleteUploadOperation(completed, { packageVersionId }) };

  // The uploaded version, read back from the owner that stored it.
  const version = (await http.json(`/api/v2/package-versions/${encodeURIComponent(packageVersionId)}`, {}, auth)).payload;
  readback.packageVersion = assertCustomerPackageVersionReadback(version, {
    id: packageVersionId, packageId, versionLabel, sha256: archive.digest, sizeBytes: archive.bytes.length,
  });

  // The effective Runtime and the independent WebUI the installation approves
  // for new builds. The Build below is explicit: the run asks for these three
  // inputs and follows the job it gets.
  const policy = (await http.json("/api/v2/catalog/build-policy", {}, auth)).payload;
  const runtimeVersionId = String(policy?.runtimeVersionId || "");
  const webuiVersionId = String(policy?.defaultWebuiVersionId || "");
  if (!runtimeVersionId || !webuiVersionId) {
    throw namedQualificationError("local_v2_customer_build_policy_unavailable", "the installation approves no Runtime and WebUI combination for a new Build");
  }
  readback.buildPolicy = { runtimeVersionId, webuiVersionId };

  // The approved Runtime's recipe names the source root the Build Runner
  // copies the package from and the runtime it starts. The Runner refuses a
  // source root of ".", ".." or an absolute one, and the probe that carries
  // the nonce and the runtime must both live inside that one directory.
  const runtimeVersion = (await http.json(`/api/v2/catalog/runtime-versions/${encodeURIComponent(runtimeVersionId)}`, {}, auth)).payload;
  const recipe = runtimeVersion?.publisherContract?.buildRecipe || null;
  const layout = assertCustomerPackageLayout({
    entries: plan.mode === "directory" ? archive.entries : null,
    sourceRoot: String(recipe?.packageInput?.sourceRoot || ""),
    targetPath: String(recipe?.packageInput?.targetPath || ""),
    entrypoint: recipe?.outputImageCommand?.entrypoint,
    probePath: plan.probePath,
  });
  readback.buildRecipe = {
    sourceRoot: layout.sourceRoot, probePath: layout.probePath, runtimePath: layout.runtimePath,
    packageSourceRoot: layout.sourceRoot,
  };

  // The explicit Build. Three inputs, one job, and this run follows that job.
  const accepted = (await http.json("/api/v2/builds", {
    method: "POST",
    headers: { "idempotency-key": `local-v2-customer-build:${sourceSha}:${packageVersionId}` },
    body: { packageVersionId, runtimeVersionId, webuiVersionId },
  }, auth, [200, 201, 202])).payload;
  const buildJobId = String(accepted?.id || "");
  if (!buildJobId) throw namedQualificationError("local_v2_customer_build_unavailable", "the Build owner accepted no build job");
  readback.build = {
    buildJobId, status: String(accepted?.status || ""), stage: String(accepted?.stage || ""),
    packageVersionId: String(accepted?.packageVersionId || packageVersionId),
    runtimeVersionId: String(accepted?.runtimeVersionId || runtimeVersionId),
    webuiVersionId: String(accepted?.webuiVersionId || webuiVersionId),
    resultCapabilityVersionId: "",
  };
  let build = accepted;
  for (let attempt = 0; attempt < pollAttempts; attempt += 1) {
    build = (await http.json(`/api/v2/builds/${encodeURIComponent(buildJobId)}`, {}, auth)).payload;
    if (CUSTOMER_BUILD_TERMINAL.has(String(build?.status || ""))) break;
    await wait(pollIntervalMs);
  }
  if (!CUSTOMER_BUILD_TERMINAL.has(String(build?.status || ""))) {
    throw namedQualificationError("local_v2_customer_build_not_terminal", "the Build this run asked for did not reach a terminal state");
  }
  readback.build.status = String(build?.status || "");
  readback.build.stage = String(build?.stage || "");
  readback.build.resultCapabilityVersionId = String(build?.resultCapabilityVersionId || "");
  if (String(build?.status || "") !== "succeeded" || !readback.build.resultCapabilityVersionId) {
    throw namedQualificationError("local_v2_customer_build_failed",
      `the Build ended ${readback.build.status} without registering a Capability Version`);
  }

  // The Capability Version that Build registered.
  const capabilityVersion = (await http.json(`/api/v2/capability-versions/${encodeURIComponent(readback.build.resultCapabilityVersionId)}`, {}, auth)).payload;
  readback.capabilityVersion = {
    id: String(capabilityVersion?.id || ""), status: String(capabilityVersion?.status || ""),
    buildJobId: String(capabilityVersion?.buildJobId || ""), packageVersionId: String(capabilityVersion?.packageVersionId || ""),
    artifactDigest: String(capabilityVersion?.artifactDigest || ""),
  };

  // The quote for that exact version. The local provider's free quote is
  // asserted before the purchase, so a paid quote can never be bought here.
  const quote = (await http.json("/api/v2/quotes", {
    method: "POST",
    headers: { "idempotency-key": `local-v2-customer-quote:${sourceSha}:${packageVersionId}` },
    body: { purpose: "deploy", capabilityVersionId: readback.build.resultCapabilityVersionId, computePlanId, storagePlanId, periodMonths: 1 },
  }, auth, [200, 201, 202])).payload;
  if (!quote?.id) throw namedQualificationError("local_v2_customer_quote_unavailable", "the Workspace owner returned no quote for the built version");
  if (String(quote?.capabilityVersionId || "") !== readback.build.resultCapabilityVersionId) {
    throw namedQualificationError("local_v2_customer_quote_version_mismatch", "the quote does not name the built Capability Version");
  }
  readback.customerQuote = { id: String(quote.id), ...assertCustomerLocalNoChargeQuote(quote, { provider, billingMode }) };

  // The purchase. The Workspace identity is the operation's resource id.
  const order = (await http.json("/api/v2/workspaces", {
    method: "POST",
    headers: { "idempotency-key": `local-v2-customer-order:${sourceSha}:${packageVersionId}` },
    body: { name: workspaceName, quoteId: String(quote.id), renewalMode: "manual" },
  }, auth, [202])).payload;
  const { operationId, workspaceId } = customerWorkspaceIdentity(order);
  readback.customerOrder = { operationId, workspaceId };

  // The purchase reaches its terminal state before the entry is read.
  let operation = order;
  for (let attempt = 0; attempt < pollAttempts; attempt += 1) {
    operation = (await http.json(`/api/v2/operations/workspace/${encodeURIComponent(operationId)}`, {}, auth)).payload;
    if (["succeeded", "failed", "cancelled"].includes(String(operation?.status || ""))) break;
    await wait(pollIntervalMs);
  }
  readback.customerOperation = { status: String(operation?.status || ""), stage: String(operation?.stage || "") };
  if (String(operation?.status || "") !== "succeeded") {
    throw namedQualificationError("local_v2_customer_workspace_operation_failed",
      `the purchase operation ended ${readback.customerOperation.status} ${readback.customerOperation.stage}`.trim());
  }

  // The Workspace that runs the built version.
  const workspace = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth)).payload;
  readback.customerWorkspace = {
    id: String(workspace?.id || ""), status: String(workspace?.status || ""),
    capabilityVersionId: String(workspace?.capabilityVersionId || ""), accessUrl: String(workspace?.accessUrl || ""),
  };
  readback.deployedVersion = assertCustomerDeployedVersionIdentity({
    build: { id: buildJobId, resultCapabilityVersionId: readback.build.resultCapabilityVersionId, packageVersionId },
    capabilityVersion, workspace,
  });

  // The entry, and the content it answers with.
  const entryUrl = String(workspace?.accessUrl || "");
  if (!entryUrl) throw namedQualificationError("local_v2_customer_entry_unavailable", "the bought Workspace publishes no entry");
  const opened = await fetchCustomerEntry(entryUrl, auth, fetchEntry);
  readback.customerEntry = { url: entryUrl, status: opened.status };
  if (opened.status !== 200) {
    throw namedQualificationError("local_v2_customer_entry_unreachable", `the published entry returned HTTP ${opened.status}`);
  }
  try {
    readback.customerEntry = { ...readback.customerEntry, ...assertCustomerEntryContentProvenance(opened.text, nonce) };
  } catch (error) {
    // The package did not carry this run's probe to its entry. When the caller
    // asked for the content level, that is the answer it refuses to accept.
    // When the caller accepted the artifact level, the chain keeps the
    // identity it proved and says that the content it did not prove.
    if (plan.requestedEvidenceLevel === CUSTOMER_EVIDENCE_LEVEL_CONTENT) throw error;
    readback.evidenceLevel = CUSTOMER_EVIDENCE_LEVEL_ARTIFACT;
    readback.degradationReason = "the published entry did not answer with this run's probe, so the entry content cannot be tied to this upload";
  }

  // The deletion of what this chain bought.
  const deletion = await deleteWorkspaceOnOwnerRoute(http, auth, {
    workspaceId, confirmationName: workspaceName,
    idempotencyKey: `local-v2-customer-delete:${sourceSha}:${packageVersionId}`, wait,
  });
  readback.customerDeletion = { operationId: String(deletion?.operationId || ""), status: String(deletion?.status || "") };
  const tombstone = (await http.request(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth)).payload;
  readback.customerDeletion.workspaceAbsent = String(tombstone?.status || "") === "deleted" || !String(tombstone?.accessUrl || "").trim();
  if (!readback.customerDeletion.workspaceAbsent) {
    throw namedQualificationError("local_v2_customer_workspace_absent_unconfirmed", "an owner still retains the Workspace this chain bought");
  }
  return readback;
}

// The entry of a bought Workspace. The session is offered first because the
// entry may be private, and the entry is asked for again without it because
// the entry may be published.
async function fetchCustomerEntry(url, auth, fetchEntry) {
  const attempts = [
    { headers: { cookie: String(auth?.cookie || "") } },
    { headers: {} },
  ];
  let last = null;
  for (const attempt of attempts) {
    if (typeof fetchEntry === "function") {
      last = await fetchEntry(url, attempt.headers);
    } else {
      const response = await fetch(url, { method: "GET", headers: attempt.headers, redirect: "manual" });
      last = { status: response.status, text: await response.text() };
    }
    if (last?.status !== 401 && last?.status !== 403) return last;
  }
  return last || { status: 0, text: "" };
}

async function main() {
  let options;
  try {
    options = parseLocalQualificationArgs();
    const receipt = await runLocalV2Qualification(options);
    process.stdout.write(`${JSON.stringify(receipt, null, 2)}\n`);
    // A boundary is a real result and never a success: the process exit code
    // says the qualification is not READY even though evidence was collected.
    if (receipt?.status !== "READY") process.exitCode = 1;
  } catch (error) {
    if (!options) {
      const rawPathIndex = process.argv.indexOf("--receipt");
      const rawPath = rawPathIndex >= 0 ? process.argv[rawPathIndex + 1] : "";
      if (rawPath) {
        await writeJSONAtomic(rawPath, {
          schemaVersion: 1, status: "NOT_READY", stage: "input_validation",
          errorCode: "local_workspace_qualification_input_invalid",
          error: redactedError(error),
          deferred: [...deferredCloudGates]
        });
      }
    }
    console.error(redactedError(error));
    process.exitCode = 1;
  }
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  await main();
}
