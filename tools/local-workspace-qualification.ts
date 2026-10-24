import { spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { constants, existsSync } from "node:fs";
import { lstat, mkdir, mkdtemp, open, readFile, realpath, rename, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, resolve, sep } from "node:path";
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

// Local-Docker is the first adapter over the provider-neutral contract. Its
// identity is declared here rather than asserted inside the validator, which is
// what lets a second provider reuse the same seam.
export const LOCAL_DOCKER_QUALIFICATION_PROFILE = qualificationProviderProfile("local-docker", {
  legacyReceiptKinds: LEGACY_QUALIFICATION_J0_READY_KINDS
});

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
// The Candidate file set this script qualifies: one base Compose file, the
// deployment overlay, the Fabric provider overlay, and the Local-Workspace
// overlay. The deployment and Fabric overlays are separate selections that the
// script fixes to the Local installation it qualifies.
const baseComposeFiles = [
  "compose.yaml",
  "deploy/portable/compose.deployment-platform-owned.yaml",
  "deploy/portable/compose.fabric-local-docker.yaml",
  "deploy/portable/compose.local-workspace.yaml"
];

// The qualification overlays stay mode-specific: the fixture run bills its own
// Sub2API authority under platform-owned billing, while a live run qualifies
// the deployment mode the installation actually runs.
export function localQualificationComposeFiles(authorityMode) {
  if (authorityMode !== "fixture" && authorityMode !== "live") throw new Error("authority mode must be fixture or live");
  const qualificationCompose = authorityMode === "fixture"
    ? "deploy/portable/compose.local-qualification.yaml"
    : "deploy/portable/compose.local-qualification-live.yaml";
  return [...baseComposeFiles, qualificationCompose];
}
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const shaPattern = /^[0-9a-f]{40}$/;
const sub2apiSecretFileFields = Object.freeze([
  "OPL_SUB2API_BASE_URL",
  "OPL_SUB2API_ADMIN_EMAIL",
  "OPL_SUB2API_ADMIN_PASSWORD",
  "OPL_QUALIFICATION_AUTHORITY_CLASS",
  "OPL_QUALIFICATION_USER_EMAIL",
  "OPL_QUALIFICATION_USER_PASSWORD"
]);
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
    if (!["--source-sha", "--cloud-image", "--workspace-image", "--receipt", "--authority-mode", "--j0-ready-receipt", "--sub2api-secret-file"].includes(token)) {
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
  const sourceSha = String(values.get("--source-sha") || "").trim();
  const cloudImage = String(values.get("--cloud-image") || "").trim();
  const workspaceImage = String(values.get("--workspace-image") || "").trim();
  const receiptPath = String(values.get("--receipt") || "").trim();
  const authorityMode = String(values.get("--authority-mode") || "fixture").trim();
  const j0ReadyReceipt = String(values.get("--j0-ready-receipt") || "").trim();
  const sub2apiSecretFile = String(values.get("--sub2api-secret-file") || "").trim();
  if (!shaPattern.test(sourceSha)) throw new Error("source SHA must be an exact 40-character lowercase commit");
  if (!receiptPath) throw new Error("receipt path is required");
  if (!buildSourceImages && !immutableImageReference(cloudImage)) throw new Error("immutable cloud image repository@digest is required");
  if (!buildSourceImages && !immutableImageReference(workspaceImage)) throw new Error("immutable workspace image repository@digest is required");
  if (buildSourceImages && (cloudImage || workspaceImage)) {
    throw new Error("source image build cannot be combined with explicit image inputs");
  }
  if (authorityMode !== "fixture" && authorityMode !== "live") throw new Error("authority mode must be fixture or live");
  if (authorityMode === "live" && !j0ReadyReceipt) throw new Error("live qualification requires a J0 READY receipt");
  if (j0ReadyReceipt && !isAbsolute(j0ReadyReceipt)) throw new Error("J0 READY receipt path must be absolute");
  if (j0ReadyReceipt && authorityMode !== "live") throw new Error("J0 READY receipt requires live authority mode");
  if (sub2apiSecretFile && authorityMode !== "live") throw new Error("Sub2API secret file requires live authority mode");
  if (sub2apiSecretFile && !isAbsolute(sub2apiSecretFile)) throw new Error("Sub2API secret file path must be absolute");
  return { sourceSha, cloudImage, workspaceImage, receiptPath, buildSourceImages, authorityMode, j0ReadyReceipt, sub2apiSecretFile };
}

function sub2apiSecretFileError(code) {
  return namedQualificationError(code, code);
}

function namedQualificationError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

function validLiveAuthorityConfiguration(value) {
  if (!value || !/^https:\/\/[^\s]+$/.test(value.baseURL) || !value.adminEmail || !value.adminPassword ||
    !value.qualificationUserEmail || !value.qualificationUserPassword || !["sandbox", "preproduction"].includes(value.authorityClass)) {
    return false;
  }
  return true;
}

export async function loadSub2APISecretFile(path, { environment = process.env, currentUid = typeof process.getuid === "function" ? process.getuid() : null } = {}) {
  if (!isAbsolute(String(path || "")) || !Number.isSafeInteger(currentUid) || currentUid < 0) {
    throw sub2apiSecretFileError("sub2api_secret_file_invalid");
  }
  if (sub2apiSecretFileFields.some((name) => Object.prototype.hasOwnProperty.call(environment, name))) {
    throw sub2apiSecretFileError("sub2api_secret_file_env_conflict");
  }
  let handle;
  try {
    handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const stat = await handle.stat();
    if (!stat.isFile() || (stat.mode & 0o777) !== 0o600 || stat.uid !== currentUid || stat.size <= 0 || stat.size > 64 * 1024) {
      throw sub2apiSecretFileError("sub2api_secret_file_invalid");
    }
    const document = parseDocument(await handle.readFile("utf8"), { uniqueKeys: true, maxAliasCount: 0 });
    if (document.errors.length !== 0) throw sub2apiSecretFileError("sub2api_secret_file_schema_invalid");
    const value = document.toJS({ maxAliasCount: 0 });
    if (!value || typeof value !== "object" || Array.isArray(value) ||
      Object.keys(value).sort().join("\0") !== [...sub2apiSecretFileFields].sort().join("\0") ||
      sub2apiSecretFileFields.some((name) => typeof value[name] !== "string" || !value[name].trim())) {
      throw sub2apiSecretFileError("sub2api_secret_file_schema_invalid");
    }
    const admitted = {
      baseURL: value.OPL_SUB2API_BASE_URL.trim(),
      adminEmail: value.OPL_SUB2API_ADMIN_EMAIL.trim(),
      adminPassword: value.OPL_SUB2API_ADMIN_PASSWORD,
      authorityClass: value.OPL_QUALIFICATION_AUTHORITY_CLASS,
      qualificationUserEmail: value.OPL_QUALIFICATION_USER_EMAIL.trim(),
      qualificationUserPassword: value.OPL_QUALIFICATION_USER_PASSWORD
    };
    if (!validLiveAuthorityConfiguration(admitted)) throw sub2apiSecretFileError("sub2api_secret_file_schema_invalid");
    return admitted;
  } catch (error) {
    if (String(error?.code || "").startsWith("sub2api_secret_file_")) throw error;
    throw sub2apiSecretFileError("sub2api_secret_file_invalid");
  } finally {
    await handle?.close();
  }
}

function liveAuthorityConfigurationFromEnvironment(environment) {
  const adminEmail = String(environment.OPL_SUB2API_ADMIN_EMAIL || "").trim();
  const adminPassword = String(environment.OPL_SUB2API_ADMIN_PASSWORD || "");
  return {
    baseURL: String(environment.OPL_SUB2API_BASE_URL || "").trim(),
    adminEmail,
    adminPassword,
    authorityClass: String(environment.OPL_QUALIFICATION_AUTHORITY_CLASS || ""),
    qualificationUserEmail: String(environment.OPL_QUALIFICATION_USER_EMAIL || adminEmail).trim(),
    qualificationUserPassword: String(environment.OPL_QUALIFICATION_USER_PASSWORD || adminPassword)
  };
}

export function qualificationEnvFileEntries(entries, options) {
  if (options.authorityMode !== "live" || !options.sub2apiSecretFile) return [...entries];
  return entries.filter(([name]) => !sub2apiSecretFileFields.includes(name));
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
    ? { accountProvisionPosts: 1, workspaceQuotePosts: 1, workspaceOrderPosts: 1, workspaceDeleteRequests: 0, refundPosts: 0 }
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

export async function unusedPort() {
  const server = createServer();
  await new Promise((resolvePromise, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolvePromise);
  });
  const address = server.address();
  const port = address && typeof address === "object" ? address.port : 0;
  await new Promise((resolvePromise, reject) => server.close((error) => error ? reject(error) : resolvePromise()));
  if (!port) throw new Error("local qualification could not allocate a loopback port");
  return port;
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

// Two Local-Docker facts belong to the installation, not to this script: the
// quota-enabled Workspace storage root, and the provider profile that lists the
// packages that root can host. A qualification run that invented either one
// would qualify a machine nobody deploys, so the script requires them and fails
// with a named reason before Compose or PostgreSQL initialization can produce
// an unrelated error.
export function localDockerInstallationInputs(environment = process.env) {
  const storageRoot = String(environment.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT || "").trim();
  if (!storageRoot) {
    throw namedQualificationError("local_docker_storage_root_missing", "OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT must name the quota-enabled Workspace storage root");
  }
  if (!isAbsolute(storageRoot)) {
    throw namedQualificationError("local_docker_storage_root_invalid", "OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT must be an absolute path");
  }
  const providerProfileJSON = String(environment.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON || "").trim();
  if (!providerProfileJSON) {
    throw namedQualificationError("local_docker_provider_profile_missing", "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON must carry the Local-Docker provider profile");
  }
  let profile;
  try {
    profile = JSON.parse(providerProfileJSON);
  } catch {
    throw namedQualificationError("local_docker_provider_profile_invalid", "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON must be valid JSON");
  }
  if (profile?.schemaVersion !== 1 || !Array.isArray(profile?.packages) || profile.packages.length === 0) {
    throw namedQualificationError("local_docker_provider_profile_invalid", "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON must carry schemaVersion 1 and at least one package");
  }
  return { storageRoot, providerProfileJSON };
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
    await runProcess("docker", ["build", ...proxyArgs, "--label", `org.opencontainers.image.revision=${sourceSha}`, "--tag", cloudTag, "."], { capture: false });
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

export function createHTTP(origin) {
  const request = async (path, init = {}, auth = null) => {
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
    if (body !== undefined) headers.set("content-type", "application/json");
    const response = await fetch(`${origin}${path}`, {
      ...init,
      headers,
      body,
      signal: AbortSignal.timeout(30_000)
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
      throw new Error(`HTTP ${init.method || "GET"} ${path} returned ${result.response.status}: ${result.text.slice(0, 500)}`);
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
  const accepted = await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {
    method: "DELETE",
    headers: { "idempotency-key": idempotencyKey },
    body: { confirmationName, acknowledgeDataDestruction: true },
  }, auth, [202]);
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
      return { operationId: read.operationId, status: read.status, stage: String(read.stage || ""), accepted: operation };
    }
    await wait(1000);
  }
  throw new Error("Workspace owner DELETE did not reach a terminal status within 120 seconds");
}

// The Gateway transaction is the monetary owner's confirmed refund, bound to
// the original charge and to the exact refund identity Workspace recorded.
// The financial-receipt route is charge-only; it is never queried for refunds.
export async function readWorkspaceRefundReceipt(http, auth, {
  workspaceId, chargeAmountUsdMicros, chargeOperationId, refundOperationId,
} = {}) {
  if (!String(chargeOperationId || "").trim() || !String(refundOperationId || "").trim()) {
    throw new Error("Workspace refund receipt binding is missing");
  }
  const transactions = await listWorkspaceTransactions(http, auth, workspaceId);
  const refunds = transactions.filter((item) => item.id === refundOperationId);
  if (refunds.length !== 1) throw new Error("Workspace refund cardinality is invalid");
  const refund = refunds[0];
  const amount = String(refund.amountUSDMicros || "");
  if (refund.kind !== "refund" || refund.status !== "confirmed" ||
    refund.originalChargeOperationId !== chargeOperationId || !String(refund.receiptId || "").trim() ||
    !String(refund.externalReference || "").trim() ||
    !/^[1-9][0-9]*$/.test(amount) || !/^[1-9][0-9]*$/.test(String(chargeAmountUsdMicros)) ||
    BigInt(amount) > BigInt(chargeAmountUsdMicros)) {
    throw new Error("Workspace refund receipt readback is invalid");
  }
  return {
    walletOperationId: refund.id, receiptId: refund.receiptId, amountUsdMicros: amount,
    code: refund.externalReference, originalChargeOperationId: refund.originalChargeOperationId,
  };
}

// A deleted Workspace remains readable for financial obligations. Its typed
// deletion readback, rather than a 404 or an empty deployment list, proves the
// original owners confirmed resource/data absence and Ledger recorded it.
export async function readWorkspaceDeletionEvidence(http, auth, { workspaceId, operationId } = {}) {
  const deletion = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deletion`, {}, auth)).payload;
  const receipt = deletion?.deletionReceipt;
  if (deletion?.workspaceId !== workspaceId || deletion?.operationId !== operationId ||
    deletion.resourceDeletionStatus !== "confirmed" || deletion.dataDeletionStatus !== "confirmed" ||
    deletion.refundStatus !== "confirmed" || !String(deletion.refundOperationId || "").trim() ||
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

export function workspaceDeletePendingReceiptEvidence(pending) {
  return {
    phase: pending.phase,
    ownerStage: pending.ownerStage,
    ordinal: Number(pending.computeReadbacks),
    max: Number(pending.maxComputeReadbacks),
    operationDigest: `sha256:${createHash("sha256").update(String(pending.operationId)).digest("hex")}`
  };
}

export function workspaceDeleteFailureEvidence(error) {
  const message = String(error instanceof Error ? error.message : error);
  const status = Number(message.match(/ returned ([0-9]{3}):/)?.[1] || 0);
  const reasonCode = String(message.match(/"(?:reasonCode|error)"\s*:\s*"([a-z0-9_]+)"/i)?.[1] ||
    (/timeout|aborted/i.test(message) ? "request_timeout" : "unknown"));
  return { status, reasonCode };
}

export async function continueWorkspaceDelete(http, path, init, auth, expected, waitForSchedule = waitForWorkspaceDeleteSchedule) {
  let previousReadback = Number(expected.initialReadback || 0);
  let maxReadbacks = Number(expected.initialMaxReadbacks || 0);
  for (;;) {
    let result;
    try {
      result = await http.json(path, init, auth, [200, 202]);
    } catch (error) {
      expected.onFailure?.(workspaceDeleteFailureEvidence(error));
      throw new Error("owner Workspace DELETE continuation failed");
    }
    if (result.response.status === 200) return result.payload;

    const pending = result.payload;
    const readback = Number(pending?.computeReadbacks);
    const maximum = Number(pending?.maxComputeReadbacks);
    const retryAfterRaw = result.response.headers?.get("retry-after");
    const retryAfter = retryAfterRaw === "1" ? 1 : Number.NaN;
    // The durable phase names the stage that just completed; ownerStage names the
    // stage the operation is working on, so a compute wait reports storage_absent.
    if (pending?.status !== "pending" || pending?.phase !== "storage_absent" || pending?.ownerStage !== "compute" ||
      pending?.computeStatus !== "destroying" || pending?.operationId !== expected.operationId || pending?.workspaceId !== expected.workspaceId ||
      !Number.isSafeInteger(readback) || !Number.isSafeInteger(maximum) || readback !== previousReadback + 1 ||
      maximum !== 8 || maxReadbacks !== 0 && maximum !== maxReadbacks || readback >= maximum || retryAfter !== 1) {
      throw new Error("owner Workspace DELETE pending evidence is invalid");
    }
    expected.onPending?.(workspaceDeletePendingReceiptEvidence(pending));
    previousReadback = readback;
    maxReadbacks = maximum;
    await waitForSchedule(retryAfter * 1000);
  }
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
export async function openQualificationSessions(http, email, password, accountId, idempotencyKey) {
  const controlPlane = await login(http, email, password, accountId);
  const owner = await cloudIdentityLogin(http, http.origin, email, password, idempotencyKey);
  if (!owner?.cookie || !owner?.ownerCsrf) {
    throw new Error("qualification did not establish an owner session");
  }
  return {
    cookie: `${controlPlane.cookie}; ${owner.cookie}`,
    csrf: controlPlane.csrf,
    ownerCsrf: owner.ownerCsrf,
  };
}

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
export async function cloudIdentityLogin(http, origin, username, password, idempotencyKey) {
  const context = await http.json("/api/v2/auth/context", {}, null, [200]);
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
export function assertWorkspaceQualificationTopology(config) {
  const missing = ["workspace", "console-bff", "tenant", "serve"].filter((name) => !config?.services?.[name]);
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

export async function verifyStores(compose) {
  const query = "select datname || ':' || pg_get_userbyid(datdba) from pg_database where datname in ('opl_control_plane','opl_fabric','opl_ledger') order by datname";
  const rows = (await compose(["exec", "-T", "postgres", "psql", "-U", "postgres", "-d", "postgres", "-Atqc", query])).stdout.trim().split(/\r?\n/);
  const expected = ["opl_control_plane:opl_control_plane", "opl_fabric:opl_fabric", "opl_ledger:opl_ledger"];
  if (JSON.stringify(rows) !== JSON.stringify(expected)) throw new Error("PostgreSQL database ownership is not separated");
  for (const owner of ["control_plane", "fabric", "ledger"]) {
    const count = Number((await compose([
      "exec", "-T", "postgres", "psql", "-U", "postgres", "-d", `opl_${owner}`, "-Atqc",
      `select count(*) from pg_tables where schemaname='public' and tableowner='opl_${owner}'`
    ])).stdout.trim());
    if (!Number.isSafeInteger(count) || count <= 0) throw new Error(`opl_${owner} owns no durable tables`);
  }
  return { controlPlane: "durable", fabric: "durable", ledger: "durable", ownerSeparated: true };
}

async function consoleReadback(http) {
  const home = await http.request("/");
  if (home.response.status !== 200 || !/<div[^>]+id=["']root["']/.test(home.text)) throw new Error("Console entry asset is unavailable");
  const script = home.text.match(/<script[^>]+src=["']([^"']+)["']/)?.[1];
  if (!script) throw new Error("Console hashed script is missing");
  const asset = await http.request(script);
  if (asset.response.status !== 200 || !asset.text.trim()) throw new Error("Console hashed script is unavailable");
}

// The owner owns the catalog a buyer prices from, and the Console resolves it
// the same way: the first approved Runtime Release for the default OPL App, the
// first available compute plan and the first available storage plan. The pages
// are read to the end, because a plan that is only offered on a later page is
// still a plan the owner offers.
async function readOwnerCatalogItems(http, auth, path, label) {
  const items = [];
  const seenCursors = new Set();
  let cursor = "";
  for (;;) {
    const query = cursor ? `?limit=100&cursor=${encodeURIComponent(cursor)}` : "?limit=100";
    const page = (await http.json(`${path}${query}`, {}, auth)).payload;
    if (!page || !Array.isArray(page.items)) throw new Error(`local qualification ${label} catalog is invalid`);
    items.push(...page.items);
    const next = page.nextCursor;
    if (next === undefined || next === null || next === "") break;
    const nextCursor = String(next);
    if (seenCursors.has(nextCursor)) throw new Error(`local qualification ${label} catalog cursor repeated`);
    seenCursors.add(nextCursor);
    cursor = nextCursor;
  }
  return items;
}

// What this purchase declares. The owner prices exactly one application source
// beside the compute and the storage plan, so the declaration names all three
// and the quote the owner returns is checked against it.
//
// This harness has no operator-declared selection, so it only runs against a
// catalog that offers exactly one purchasable choice of each kind. A catalog
// that offers several is refused rather than narrowed: picking one of them
// would be the harness choosing a purchase nobody declared, and a qualification
// run that buys a different Workspace each time it runs is not evidence about
// anything.
export async function resolveLocalWorkspaceDeclaration(http, auth) {
  const [runtimeVersions, computePlans, storagePlans] = await Promise.all([
    readOwnerCatalogItems(http, auth, "/api/v2/catalog/runtime-versions", "runtime version"),
    readOwnerCatalogItems(http, auth, "/api/v2/catalog/compute-plans", "compute plan"),
    readOwnerCatalogItems(http, auth, "/api/v2/catalog/storage-plans", "storage plan")
  ]);
  const onlyPurchasable = (candidates, label) => {
    const purchasable = candidates.filter((candidate) => candidate?.status === "approved" || candidate?.availability === "available");
    if (purchasable.length !== 1) {
      throw new Error(`local qualification owner catalog offers ${purchasable.length} purchasable ${label}s`);
    }
    return purchasable[0];
  };
  const runtimeVersion = onlyPurchasable(runtimeVersions, "runtime version");
  const computePlan = onlyPurchasable(computePlans, "compute plan");
  const storagePlan = onlyPurchasable(storagePlans, "storage plan");
  if (!runtimeVersion || !computePlan || !storagePlan) {
    throw new Error("local qualification owner catalog does not offer a purchasable Workspace");
  }
  return workspaceDeclaration({
    applicationSelection: { kind: "opl_app", runtimeVersionId: String(runtimeVersion.id || "") },
    computePlanId: String(computePlan.id || ""),
    storagePlanId: String(storagePlan.id || "")
  });
}

// Buy one declared Workspace through the owner.
//
// The owner records the quote and the order against the buyer's idempotency
// identity, so a repeated delivery with the same identity and the same request
// replays the original quote and the original order instead of pricing or
// buying twice. The harness replays the sequence when an answer is lost,
// because the identities it has to record are the ones the owner mints and it
// cannot invent them.
export async function buyLocalWorkspace({
  http, auth, name, declaration, walletUsdMicros, idempotencyKey, wait, attempts = 3
}) {
  let failure = null;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    let quote;
    try {
      quote = await quoteWorkspaceOrder({ http, auth, declaration, idempotencyKey: `${idempotencyKey}:quote` });
    } catch (error) {
      failure = error;
      continue;
    }
    // The owner priced the Workspace, so the buyer never computes a price. It
    // only confirms that the quote names the declared purchase and that its own
    // wallet can fund it.
    const amountUsdMicros = priceWorkspaceQuote({ quote, walletUsdMicros, declaration });
    try {
      const order = await submitWorkspaceOrder({ http, auth, name, quote, idempotencyKey, wait });
      return { quote, amountUsdMicros, ...order };
    } catch (error) {
      failure = error;
    }
  }
  throw failure;
}

// Read the purchase, application and funded charge from their current owners.
// Control Plane has no projection for this native Workspace order.
export async function readWorkspaceEvidence(http, auth, operationId, workspaceId, receiptId) {
  const operation = (await http.json(`/api/v2/operations/workspace/${encodeURIComponent(operationId)}`, {}, auth)).payload;
  if (operation?.owner !== "workspace" || operation?.kind !== "create_workspace" || operation?.status !== "succeeded" ||
    operation?.operationId !== operationId || operation?.resourceId !== workspaceId) {
    throw new Error("Workspace owner operation readback is invalid");
  }
  const workspace = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth)).payload;
  const entryUrl = String(workspace?.accessUrl || "");
  // The accepted application source is the owner's own fact: a built Agent
  // names its CapabilityVersion, the default App names its Runtime Release.
  const applicationId = String(workspace?.capabilityVersionId || workspace?.runtimeVersionId || "");
  if (workspace?.id !== workspaceId || workspace.applicationAvailability !== "available" || !entryUrl.trim() ||
    !applicationId.trim()) {
    throw new Error("Workspace owner readback is invalid");
  }
  const page = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/deployments?limit=100`, {}, auth)).payload;
  const deployments = Array.isArray(page?.items) ? page.items : [];
  const active = deployments.find((deployment) => deployment?.status === "active" &&
    deployment?.workspaceId === workspaceId && String(deployment?.runtimeInstanceId || "").trim());
  if (!active || !String(active.id || "").trim()) {
    throw new Error("Workspace runtime readback is invalid");
  }
  const transactions = await listWorkspaceTransactions(http, auth, workspaceId);
  const charges = transactions.filter((item) => item.kind === "charge" && item.status === "confirmed" && item.receiptId === receiptId);
  if (charges.length !== 1) throw new Error("Ledger charge readback is invalid");
  const financial = await readWorkspacePurchaseEvidence(http, auth, {
    workspaceId, operationId, expectedReceiptId: receiptId, amountUsdMicros: String(charges[0].amountUSDMicros),
  });
  // The v2 owner route publishes the accepted application source on the
  // Workspace and the running generation on the Serve deployment. Those are the
  // owner's own identities, so the receipt binds them rather than inventing an
  // application id or revision the owner never returned. A deployment that
  // publishes no capability version is still identified by its own deployment
  // generation, which is what Serve actually retired.
  const current = {
    status: "ready", entryUrl, operationId: active.id,
    applicationId, revision: String(active.capabilityVersionId || active.id),
  };
  return {
    operation,
    workspace: { ...workspace, url: entryUrl, openable: true, currentApplication: current },
    current,
    runtime: {
      workspaceId, ready: true, status: "running", url: entryUrl,
      runtimeId: active.runtimeInstanceId, currentApplication: { operationId: active.id },
    },
    receipt: {
      receiptId, workspaceId, kind: financial.receipt.kind, status: "completed",
      chargeReference: String(financial.walletOperation.externalReference || financial.walletOperation.id),
      totalUsdMicros: String(financial.walletOperation.amountUSDMicros),
      // The owner publishes fulfillment identities only when its typed receipt
      // carries them. The delivery identities are the Serve deployment and
      // Runtime instance above; no resource id is invented here.
      fulfillment: {},
      financial,
    },
  };
}

async function readAllGatewayKeys(http, auth) {
  const items = [];
  let expectedTotal = null;
  let expectedPages = null;
  for (let pageNumber = 1; ; pageNumber += 1) {
    const page = sourceData((await http.json(`/api/gateway/keys?page=${pageNumber}&pageSize=100`, {}, auth)).payload, "sub2api");
    if (!Array.isArray(page?.items) || !Number.isSafeInteger(page?.total) || page.total < 0 || page.page !== pageNumber || page.pageSize !== 100 ||
      !Number.isSafeInteger(page.pages) || page.pages !== Math.max(1, Math.ceil(page.total / page.pageSize)) ||
      expectedTotal !== null && (page.total !== expectedTotal || page.pages !== expectedPages)) {
      throw new Error("local qualification key inventory is invalid");
    }
    expectedTotal ??= page.total;
    expectedPages ??= page.pages;
    items.push(...page.items);
    if (pageNumber === page.pages) break;
  }
  if (items.length !== expectedTotal) throw new Error("local qualification key inventory is invalid");
  return items;
}

/*
 * The purchase receipts this Workspace actually carries. The v2 transactions
 * projection is the owner-typed source: a confirmed charge names the receipt
 * that settled it, and the retained Control Plane billing list is not consulted
 * for an order placed on the owner route.
 */
export async function readWorkspacePurchaseReceipts(http, auth, workspaceId) {
  const transactions = await listWorkspaceTransactions(http, auth, workspaceId);
  return transactions
    .filter((item) => item?.workspaceId === workspaceId && item?.kind === "charge" && item?.status === "confirmed" &&
      String(item?.receiptId || "").trim())
    .map((item) => ({ receiptId: String(item.receiptId), workspaceId }));
}

// The purchase receipt is the Ledger's own record of the purchase, and the
// Ledger binds it to the Workspace it paid for. The owner mints the Workspace
// identity, so the receipt for this purchase is the one the Ledger holds for
// exactly that Workspace, and it is the only one this account may hold.
export async function readLocalPurchaseReceiptId(http, auth, workspaceId) {
  const transactions = await listWorkspaceTransactions(http, auth, workspaceId);
  const charges = transactions.filter((candidate) =>
    candidate?.workspaceId === workspaceId && candidate?.kind === "charge" && candidate?.status === "confirmed" &&
    String(candidate?.receiptId || "").trim());
  if (charges.length !== 1) throw new Error("local qualification purchase receipt cardinality is invalid");
  return String(charges[0].receiptId);
}

export function validateLocalJ1AccountingReadback(input) {
  const {
    operationId, workspaceId, receiptId, runtimeId, keyId, keyName, sub2apiUserId, debitCode, amountUsdMicros,
    beforeMicros, afterMicros, baselineKeys, keys, receipts, key, keyUsage, usage, history, debit, evidence
  } = input;
  if (![baselineKeys, keys, receipts].every(Array.isArray) || !Array.isArray(history?.items) ||
    !String(keyName || "").trim()) {
    throw new Error("local qualification accounting inventory is invalid");
  }
  // The installation creates the Workspace's reserved Key, so both its identity
  // and its exact name must be new to this qualification run.
  if (baselineKeys.some((candidate) => String(candidate?.id || "") === keyId || candidate?.name === keyName)) {
    throw new Error("local qualification exact workspace key predates this operation");
  }
  // The purchase receipt is the Workspace owner's own confirmed charge, read
  // from the v2 transactions projection. The retained Control Plane billing list
  // is not this order's evidence, so there is no separate baseline to subtract:
  // the Workspace identity is owner-minted after this operation started, which is
  // what makes a pre-existing receipt for it impossible.
  const exactKeys = keys.filter((candidate) => String(candidate?.id || "") === keyId);
  if (exactKeys.length !== 1 || exactKeys[0]?.name !== keyName || key?.id !== keyId || key?.kind !== "workspace" || key?.status !== "active" ||
    exactKeys[0]?.kind !== "workspace" || exactKeys[0]?.status !== "active") {
    throw new Error("local qualification exact workspace key cardinality is invalid");
  }
  const workspacePurchaseReceipts = receipts.filter((candidate) => candidate?.workspaceId === workspaceId);
  if (workspacePurchaseReceipts.length !== 1 || workspacePurchaseReceipts[0]?.receiptId !== receiptId) {
    throw new Error("local qualification exact purchase receipt cardinality is invalid");
  }
  if (debit?.count !== 1 || debit?.code !== debitCode || String(debit?.userId) !== sub2apiUserId || String(debit?.amountUsdMicros) !== amountUsdMicros) {
    throw new Error("local qualification exact debit cardinality is invalid");
  }
  if (typeof usage?.totalRequests !== "number" || typeof keyUsage?.totalRequests !== "number" ||
    !/^\d+$/.test(afterMicros) ||
    evidence?.operation?.operationId !== operationId || evidence?.operation?.resourceId !== workspaceId ||
    evidence?.current?.status !== "ready" || !String(evidence?.current?.entryUrl || "").trim() ||
    evidence?.workspace?.url !== evidence?.current?.entryUrl ||
    evidence?.runtime?.runtimeId !== runtimeId || evidence?.runtime?.currentApplication?.operationId !== evidence?.current?.operationId ||
    evidence?.receipt?.receiptId !== receiptId || evidence?.receipt?.workspaceId !== workspaceId ||
    evidence?.receipt?.chargeReference !== debitCode || String(evidence?.receipt?.totalUsdMicros) !== amountUsdMicros ||
    evidence?.receipt?.fulfillment?.runtimeId !== undefined || evidence?.receipt?.fulfillment?.workspaceApiKeyId !== undefined) {
    throw new Error("local qualification operation accounting binding is invalid");
  }
  return { walletExactDeltaObserved: /^\d+$/.test(beforeMicros) && BigInt(beforeMicros) - BigInt(amountUsdMicros) === BigInt(afterMicros) };
}

export async function provisionLocalQualificationAccount(http, adminAuth, { email, password, idempotencyKey }) {
  const normalizedEmail = String(email || "").trim().toLowerCase();
  const accountId = `acct-${stableID("account", normalizedEmail).slice(0, 18)}`;
  const operationId = `account-provision-${stableID(idempotencyKey, normalizedEmail).slice(0, 18)}`;
  const provision = await http.json("/api/operator/accounts", {
    method: "POST", headers: { "idempotency-key": idempotencyKey },
    body: { email: normalizedEmail, password, name: "Local qualification" }
  }, adminAuth, [201]);
  if (provision.payload?.status !== "succeeded" || provision.payload?.accountId !== accountId || provision.payload?.operationId !== operationId) {
    throw new Error("local qualification account provision response is invalid");
  }
  const matches = [];
  for (let pageNumber = 1; pageNumber <= 100; pageNumber += 1) {
    const page = sourceData((await http.json(`/api/operator/accounts?page=${pageNumber}&pageSize=50`, {}, adminAuth)).payload, "control-plane+sub2api");
    if (!Array.isArray(page?.items) || !Number.isSafeInteger(page?.total) || page.total < 0 || page.page !== pageNumber || page.pageSize !== 50) {
      throw new Error("local qualification account mapping readback is invalid");
    }
    matches.push(...page.items.filter((candidate) => candidate?.accountId === accountId && candidate?.email === normalizedEmail));
    if (pageNumber >= Math.max(1, Math.ceil(page.total / page.pageSize))) break;
    if (pageNumber === 100) throw new Error("local qualification account mapping readback is invalid");
  }
  if (matches.length !== 1 || matches[0]?.status !== "active" || !/^[1-9][0-9]*$/.test(String(matches[0]?.sub2apiUserId || ""))) {
    throw new Error("local qualification account mapping readback is invalid");
  }
  return { accountId, operationId, mapping: matches[0] };
}

export async function runLocalWorkspaceJ1HTTPQualification(input) {
  const {
    http, adminEmail, adminPassword, qualificationEmail, qualificationPassword, accountProvisionKey,
    launchKey, workspaceName, receiptBase, readDebit, readRuntime, cleanup, wait,
    onStage = () => {}
  } = input;
  let cleanupEvidence;
  let qualificationFailure;
  let cleanupScope = {};
  let orderSubmitted = false;
  let operationId = "";
  let workspaceId = "";
  let auth = null;
  try {
    onStage("bootstrap_ready");
    const bootstrap = await http.json("/api/healthz");
    if (bootstrap.payload?.status !== "ok") throw new Error("local qualification bootstrap is not ready");
    await consoleReadback(http);
    onStage("admin_login");
    const adminAuth = await login(http, adminEmail, adminPassword);
    onStage("account_provision");
    const provision = await provisionLocalQualificationAccount(http, adminAuth, {
      email: qualificationEmail, password: qualificationPassword, idempotencyKey: accountProvisionKey
    });
    cleanupScope = { accountId: provision.accountId };
    onStage("qualification_login");
    auth = await openQualificationSessions(http, qualificationEmail, qualificationPassword, provision.accountId, `local-qualification-owner:${accountProvisionKey}`);
    const me = sourceData((await http.json("/api/auth/me", {}, auth)).payload, "sub2api");
    const sub2apiUserId = String(me?.sub2apiUserId || "");
    if (me?.accountId !== provision.accountId || me?.email !== qualificationEmail.toLowerCase() || me?.role !== "owner" || me?.status !== "active" ||
      sub2apiUserId !== String(provision.mapping.sub2apiUserId)) {
      throw new Error("qualification authority identity binding is invalid");
    }
    onStage("wallet_usage_baseline");
    const walletBefore = sourceData((await http.json("/api/gateway/wallet", {}, auth)).payload, "sub2api");
    const usageBefore = sourceData((await http.json("/api/gateway/usage-summary?period=month", {}, auth)).payload, "sub2api");
    const beforeMicros = String(walletBefore?.usdMicros || "");
    if (!/^[1-9][0-9]*$/.test(beforeMicros) || typeof usageBefore?.totalRequests !== "number") {
      throw new Error("qualification Wallet or Usage baseline is invalid");
    }
    const baselineKeys = await readAllGatewayKeys(http, auth);
    onStage("catalog_resolution");
    const declaration = await resolveLocalWorkspaceDeclaration(http, auth);
    onStage("workspace_order");
    cleanupScope = { ...cleanupScope, idempotencyKey: launchKey, workspaceName, declaration };
    orderSubmitted = true;
    // The purchase sequence and the quote policy are shared invariants. What
    // differs across authority profiles is the evidence below this point, and
    // the cleanup scope, which is recorded here because only this profile owns an
    // account it has to tear down.
    let purchase;
    try {
      purchase = await buyLocalWorkspace({
        http, auth, name: workspaceName, declaration, walletUsdMicros: beforeMicros, idempotencyKey: launchKey, wait,
      });
    } catch (error) {
      // The owner may have accepted the order before a later read failed. The
      // accepted identities are the recovery context even though the purchase
      // did not complete, and the original idempotency identity is retained so a
      // completely lost POST answer can be read back as the same order.
      operationId = String(error?.operationId || "");
      workspaceId = String(error?.workspaceId || "");
      cleanupScope = {
        ...cleanupScope,
        ...(operationId ? { operationId } : {}),
        ...(workspaceId ? { workspaceId } : {}),
        ...(String(error?.quoteId || "") ? { quoteId: String(error.quoteId) } : {}),
        idempotencyKey: launchKey,
      };
      throw error;
    }
    const { amountUsdMicros, operation, operationId: ownerOperationId, workspaceId: ownerWorkspaceId } = purchase;
    operationId = ownerOperationId;
    workspaceId = ownerWorkspaceId;
    cleanupScope = { ...cleanupScope, operationId, workspaceId, idempotencyKey: launchKey };
    onStage("application_installation");
    const installation = await waitForWorkspaceApplicationInstallation(http, auth, { workspaceId, operationId, wait });
    const workspaceKey = resolveWorkspaceKey(await readAllGatewayKeys(http, auth), workspaceId);
    onStage("terminal_readback");
    const receiptId = await readLocalPurchaseReceiptId(http, auth, workspaceId);
    const evidence = await readWorkspaceEvidence(http, auth, operationId, workspaceId, receiptId);
    if (localApplicationEntryPort(evidence.current.entryUrl) === 0) throw new Error("Workspace application entry is invalid");
    const runtimeImage = await readRuntime({ accountId: provision.accountId, workspaceId, runtimeId: evidence.runtime.runtimeId });
    onStage("workspace_open");
    const opened = await fetchApplicationEntry(evidence.current.entryUrl);
    assertWorkspaceOpenQualification(opened);

    onStage("accounting_readback");
    const [usage, walletAfter, keys, historyPage, receipts, debit] = await Promise.all([
      http.json("/api/gateway/usage-summary?period=month", {}, auth).then((result) => sourceData(result.payload, "sub2api")),
      http.json("/api/gateway/wallet", {}, auth).then((result) => sourceData(result.payload, "sub2api")),
      readAllGatewayKeys(http, auth),
      http.json("/api/gateway/balance-history?page=1&pageSize=20", {}, auth).then((result) => sourceData(result.payload, "sub2api")),
      readWorkspacePurchaseReceipts(http, auth, workspaceId),
      readDebit({ accountId: provision.accountId, sub2apiUserId, code: evidence.receipt.chargeReference, amountUsdMicros })
    ]);
    const keyId = workspaceKey.id;
    const key = sourceData((await http.json(`/api/gateway/keys/${encodeURIComponent(keyId)}`, {}, auth)).payload, "sub2api");
    const keyUsage = sourceData((await http.json(`/api/gateway/keys/${encodeURIComponent(keyId)}/usage-summary?period=month`, {}, auth)).payload, "sub2api");
    const afterMicros = String(walletAfter?.usdMicros || "");
    const accounting = validateLocalJ1AccountingReadback({
      operationId, workspaceId, receiptId, runtimeId: evidence.runtime.runtimeId, keyId, keyName: workspaceKey.name, sub2apiUserId,
      debitCode: evidence.receipt.chargeReference, amountUsdMicros, beforeMicros, afterMicros,
      baselineKeys, keys, receipts, key, keyUsage, usage, history: historyPage, debit, evidence
    });

    const receiptCandidate = {
      ...receiptBase,
      images: {
        ...receiptBase.images,
        workspace: { ...receiptBase.images.workspace, runningDigest: runtimeImage.runningDigest }
      },
      identities: {
        accountId: provision.accountId, sub2apiUserId, operationId, workspaceId,
        runtimeId: evidence.runtime.runtimeId, keyId, debitCode: debit.code, purchaseReceiptId: receiptId,
        runtimeVersionId: declaration.applicationSelection.runtimeVersionId,
        computePlanId: declaration.computePlanId, storagePlanId: declaration.storagePlanId
      },
      debit: { count: 1, accountId: provision.accountId, operationId, workspaceId, code: debit.code, userId: sub2apiUserId, amountUsdMicros },
      wallet: { beforeUsdMicros: beforeMicros, afterUsdMicros: afterMicros, exactDeltaObserved: accounting.walletExactDeltaObserved },
      receipt: {
        count: 1, id: receiptId, accountId: provision.accountId, operationId, workspaceId, quoteId: String(purchase.quote?.id || ""),
        computeAllocationId: String(evidence.receipt.fulfillment?.computeAllocationId || ""),
        storageId: String(evidence.receipt.fulfillment?.storageId || ""), attachmentId: String(evidence.receipt.fulfillment?.attachmentId || ""),
        chargeReference: evidence.receipt.chargeReference, amountUsdMicros: String(evidence.receipt.totalUsdMicros)
      },
      application: {
        status: evidence.current.status, operationId: evidence.current.operationId, applicationId: evidence.current.applicationId,
        revision: evidence.current.revision, entryUrl: evidence.current.entryUrl,
        runtimeId: evidence.runtime.runtimeId, keyId, resumed: installation.resumed === true
      },
      restart: { performed: false },
      deletion: { performed: false, mode: "qualification_owned_cleanup" },
      deletionReceipt: { count: 0 },
      residuals: cleanupEvidence,
      authorityWriteCounts: { keyCreates: 1, keyDeletes: 0, debits: 1, refunds: 0 },
      mutationCounts: { accountProvisionPosts: 1, workspaceQuotePosts: 1, workspaceOrderPosts: 1, workspaceDeleteRequests: 0, refundPosts: 0 },
      refund: { count: 0 },
      usage: { source: "sub2api", status: "available", totalRequests: usage.totalRequests }
    };
    onStage("receipt_validation");
    validateLocalQualificationReceipt({ ...receiptCandidate, residuals: { containers: 0, volumes: 0, networks: 0 } });
    onStage("qualification_cleanup");
    cleanupEvidence = await cleanup({ accountId: provision.accountId, workspaceId });
    const receipt = validateLocalQualificationReceipt({ ...receiptCandidate, residuals: cleanupEvidence });
    return { receipt, auth, provision, operation, evidence, cleanupEvidence };
  } catch (error) {
    qualificationFailure = error;
    throw error;
  } finally {
    if (cleanupEvidence === undefined) {
      // A Workspace bought on the owner route has no retained Control Plane
      // Launch, so the retained recovery surface has nothing to read, and the
      // owner operation does not publish the retained per-resource write
      // counters. The preserved service and PostgreSQL state remain the recovery
      // authority, and the recovery artifact names the owner-minted identities
      // the order was admitted under.
      //
      // Cleanup is recovery evidence, not the error being reported. A failure to
      // write it must not replace the original failure with a second one; the
      // scope carries the cleanup error beside the original cause instead.
      try {
        await cleanup({ ...cleanupScope, failed: true, launchSubmitted: orderSubmitted, recoveryAuthority: null });
      } catch (cleanupError) {
        if (qualificationFailure && typeof qualificationFailure === "object") {
          qualificationFailure.cleanupError = cleanupError;
          throw qualificationFailure;
        }
        throw cleanupError;
      }
    }
  }
}

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

export async function cleanupLocalQualificationSecretRoot(compose, fabricSecretRoot) {
  const cleanup = await compose([
    "exec", "-T", "--user", "root", "fabric", "sh", "-c",
    'rm -rf -- "$1"/* "$1"/.[!.]* "$1"/..?*',
    "opl-qualification-secret-cleanup", fabricSecretRoot
  ], { allowFailure: true });
  if (cleanup.code !== 0) throw new Error("qualification-owned secret cleanup failed");
}

// Only call after Compose down has confirmed that PostgreSQL is stopped. The
// pinned PostgreSQL image can traverse its own chowned data; the host cannot on
// Linux. Mount only this run's data root, clear it without following symlinks,
// then return the empty 0700 directory to the actual scratch-root owner.
export async function cleanupLocalQualificationPostgresRoot(tempRoot, postgresDataRoot, execute = runProcess) {
  const [scratch, data, scratchPath, dataPath] = await Promise.all([
    lstat(tempRoot), lstat(postgresDataRoot), realpath(tempRoot), realpath(postgresDataRoot)
  ]);
  if (!scratch.isDirectory() || !data.isDirectory() || scratch.isSymbolicLink() || data.isSymbolicLink() ||
    dataPath !== join(scratchPath, "postgres")) {
    throw new Error("qualification PostgreSQL cleanup requires the exact run-owned data root");
  }
  const image = parseDocument(await readFile(join(root, "compose.yaml"), "utf8")).getIn(["services", "postgres", "image"]);
  if (!immutableImageDigest(image)) throw new Error("qualification PostgreSQL cleanup requires the pinned Compose image");
  const cleanup = await execute("docker", [
    "run", "--rm", "--network", "none", "--read-only", "--user", "0:0",
    "--mount", `type=bind,source=${dataPath},target=/qualification-postgres`,
    "--entrypoint", "sh", image, "-ceu",
    'find /qualification-postgres -xdev -mindepth 1 -delete; chown "$1:$2" /qualification-postgres; chmod 0700 /qualification-postgres',
    "opl-qualification-postgres-cleanup", String(scratch.uid), String(scratch.gid)
  ], { allowFailure: true });
  if (cleanup.code !== 0) {
    throw new Error(`qualification PostgreSQL cleanup failed for ${postgresDataRoot} (owner ${data.uid}:${data.gid})`);
  }
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

// The installation owns the Runtime container. Its exact provider labels must
// name this account, Workspace, application Runtime generation and component,
// and run exactly the qualified Workspace image.
async function applicationRuntimeImageReadback(accountId, workspaceId, expectedImage, expectedImageID, expectedRuntimeId) {
  const ids = await exactLabelIDs("containers", accountId, workspaceId);
  const runtime = [];
  for (const id of ids) {
    const [inspection] = JSON.parse((await runProcess("docker", ["inspect", id])).stdout);
    if (inspection?.Config?.Labels?.["opl.fabric.kind"] === "application_runtime") runtime.push(inspection);
  }
  if (runtime.length !== 1) throw new Error(`expected one exact-labelled application Runtime container, found ${runtime.length}`);
  const labels = runtime[0].Config.Labels || {};
  if (labels["opl.image.ref"] !== expectedImage || runtime[0].Image !== expectedImageID ||
    labels["opl.runtime.id"] !== expectedRuntimeId || labels["opl.component.role"] !== "main" ||
    labels["opl.workspace.id"] !== workspaceId || labels["opl.account.id"] !== accountId) {
    throw new Error("Workspace Runtime image binding is invalid");
  }
  return runtime[0];
}

// The Workspace owner names every Workspace's reserved Sub2API Key from the
// Workspace identity, and the default installation creates exactly that Key.
export function workspaceReservedKeyName(workspaceId) {
  return `opl-workspace-${stableID(workspaceId).slice(0, 12)}`;
}

export function resolveWorkspaceKey(keys, workspaceId) {
  const name = workspaceReservedKeyName(workspaceId);
  const matches = keys.filter((candidate) => candidate?.name === name);
  if (matches.length !== 1 || matches[0]?.kind !== "workspace" || matches[0]?.status !== "active" ||
    !/^[1-9][0-9]*$/.test(String(matches[0]?.id || ""))) {
    throw new Error("local qualification Workspace Key inventory is invalid");
  }
  return { id: String(matches[0].id), name };
}

// The deleted Workspace's injected Gateway Secret is the provider secret-root
// directory Contracts names from the Workspace identity.
export function workspaceGatewaySecretRef(workspaceId) {
  return `opl-gateway-${createHash("sha256").update(String(workspaceId)).digest("hex").slice(0, 16)}`;
}

// Delete retains the original Gateway Key identity and confirms deactivation.
// An unchanged active Key is not a completed owner closeout.
//
// The authority is the owner of that fact, and its own key record carries the
// identity the authority meters (id, name, user, group, status) without Control
// Plane's `kind` projection. Identity and inventory stay unchanged; status must
// move from active to disabled under the Gateway owner's own readback.
export function retainedWorkspaceKey(before, after, workspaceId, keyId) {
  const exact = (keys) => (keys || []).filter((candidate) => String(candidate?.id || "") === String(keyId));
  const beforeKey = exact(before);
  const afterKey = exact(after);
  const name = workspaceReservedKeyName(workspaceId);
  const record = (key) => [String(key?.id || ""), key?.name, String(key?.userId ?? ""), String(key?.groupId ?? "")].join("\u0000");
  const inventory = (keys) => (keys || []).map((candidate) => String(candidate?.id ?? "")).sort().join(",");
  return beforeKey.length === 1 && afterKey.length === 1 &&
    beforeKey[0].name === name && afterKey[0].name === name && beforeKey[0].status === "active" && afterKey[0].status === "disabled" &&
    record(beforeKey[0]) === record(afterKey[0]) && inventory(before) === inventory(after);
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

export function workspaceDeleteReceiptStagesMatch(stageEvidence) {
  if (!Array.isArray(stageEvidence) || stageEvidence.length !== workspaceDeleteReceiptStages.length) return false;
  return workspaceDeleteReceiptStages.every((expected, index) => {
    const entry = stageEvidence[index];
    return entry?.stage === expected.stage && entry?.result === expected.result && entry?.evidenceKind === expected.evidenceKind &&
      typeof entry?.observedAt === "string" && entry.observedAt.trim() !== "" &&
      typeof entry?.evidenceRef === "string" && entry.evidenceRef.trim() !== "";
  });
}

// Only the Local-Docker publication shape is admissible: the application's own
// entry is the host-bound loopback port its provider reported.
export function localApplicationEntryPort(url) {
  const match = /^http:\/\/127\.0\.0\.1:([0-9]{1,5})\/$/.exec(String(url || ""));
  const port = match ? Number(match[1]) : 0;
  return port >= 1 && port <= 65535 ? port : 0;
}

function assertWorkspaceOpenQualification(opened) {
  if (!opened.response.ok) {
    throw namedQualificationError("workspace_open_http_failed", `Workspace Runtime open returned HTTP ${opened.response.status}`);
  }
  if (!opened.text.includes("OPL Workspace READY")) {
    throw namedQualificationError("workspace_open_fixture_marker_missing", "Workspace Runtime open is missing the qualification fixture marker");
  }
}

async function fetchApplicationEntry(url) {
  const response = await fetch(url, { redirect: "follow", signal: AbortSignal.timeout(30_000) });
  return { response, text: await response.text() };
}

export async function authorityState(port, token) {
  const response = await fetch(`http://127.0.0.1:${port}/qualification/state`, {
    headers: { authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(10_000)
  });
  const text = await response.text();
  if (response.status !== 200) throw new Error(`qualification authority state returned ${response.status}`);
  const payload = JSON.parse(text);
  if (payload?.code !== 0 || !payload.data || typeof payload.data !== "object") {
    throw new Error("qualification authority state envelope is invalid");
  }
  return payload.data;
}

function usdMicrosFromDecimal(value) {
  const normalized = String(value ?? "").trim();
  const match = normalized.match(/^(-?)([0-9]+)(?:\.([0-9]{1,6}))?$/);
  if (!match) throw new Error("live Sub2API adjustment amount is not an exact decimal");
  const micros = BigInt(match[2]) * 1_000_000n + BigInt((match[3] || "").padEnd(6, "0") || "0");
  return `${match[1] === "-" ? "-" : ""}${micros.toString()}`;
}

export async function liveAuthorityAdjustmentReadback(baseURL, email, password, userId, code, expectedValueUsdMicros, request = fetch) {
  const signal = AbortSignal.timeout(10_000);
  const loginResponse = await request(`${baseURL.replace(/\/$/, "")}/api/v1/auth/login`, {
    method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email, password }),
    signal
  });
  const loginPayload = await loginResponse.json();
  const token = String(loginPayload?.data?.access_token || "");
  if (!loginResponse.ok || loginPayload?.code !== 0 || !token) throw new Error("live Sub2API admin authentication failed");
  let authorityTotal = -1;
  let authorityPages = -1;
  let match = null;
  for (let page = 1; ; page += 1) {
    const url = new URL(`${baseURL.replace(/\/$/, "")}/api/v1/admin/users/${encodeURIComponent(userId)}/balance-history`);
    url.searchParams.set("page", String(page));
    url.searchParams.set("page_size", "100");
    url.searchParams.set("type", "balance");
    const response = await request(url, { headers: { authorization: `Bearer ${token}` }, signal });
    const payload = await response.json();
    if (!response.ok || payload?.code !== 0 || !Array.isArray(payload?.data?.items)) throw new Error("live Sub2API balance-history readback failed");
    const data = payload.data;
    const total = Number(data.total);
    const pages = Number(data.pages);
    const expectedPages = total > 0 ? Math.ceil(total / 100) : 1;
    const expectedItems = total > 0 ? Math.min(100, total - (page - 1) * 100) : 0;
    if (!Number.isSafeInteger(total) || total < 0 || !Number.isSafeInteger(pages) || pages !== expectedPages ||
      data.page !== page || data.page_size !== 100 || page > pages || data.items.length !== expectedItems ||
      page > 1 && (total !== authorityTotal || pages !== authorityPages)) {
      throw new Error("live Sub2API balance-history pagination is invalid");
    }
    if (page === 1) {
      authorityTotal = total;
      authorityPages = pages;
    }
    for (const candidate of data.items) {
      if (candidate?.code !== code) continue;
      if (match) throw new Error("live Sub2API exact adjustment cardinality is invalid");
      const valueUsdMicros = usdMicrosFromDecimal(candidate.value);
      if (candidate.type !== "balance" || candidate.status !== "used" || String(candidate.used_by) !== String(userId) ||
        !String(candidate.used_at || "").trim() || !String(candidate.created_at || "").trim() || valueUsdMicros !== expectedValueUsdMicros) {
        throw new Error("live Sub2API adjustment readback differs from the expected identity");
      }
      match = { code, userId: String(userId), valueUsdMicros, status: "used", count: 1 };
    }
    if (page === authorityPages) break;
  }
  if (match) return match;
  throw new Error("live Sub2API exact adjustment was not found");
}

function receiptCommand(options) {
  const imageArgs = options.buildSourceImages
    ? "--build-source-images"
    : `--cloud-image ${options.cloudImage} --workspace-image ${options.workspaceImage}`;
  const j0Arg = options.j0ReadyReceipt ? ` --j0-ready-receipt ${options.j0ReadyReceipt}` : "";
  return `npm run qualify:local:workspace -- --source-sha ${options.sourceSha} ${imageArgs} --authority-mode ${options.authorityMode}${j0Arg} --receipt ${options.receiptPath}`;
}

async function writeEarlyNotReady(options, stage, errorCode, error) {
  let tree = "unavailable";
  try {
    tree = (await runProcess("git", ["rev-parse", `${options.sourceSha}^{tree}`])).stdout.trim() || tree;
  } catch {
    // The input receipt still records the exact requested source when the local checkout is incomplete.
  }
  await writeJSONAtomic(options.receiptPath, {
    schemaVersion: 1,
    status: "NOT_READY",
    completedAt: new Date().toISOString(),
    source: { sha: options.sourceSha, tree },
    images: {
      cloud: { input: options.cloudImage || "unavailable", digest: immutableImageDigest(options.cloudImage) || "unavailable" },
      workspace: { input: options.workspaceImage || "unavailable", digest: immutableImageDigest(options.workspaceImage) || "unavailable" }
    },
    command: receiptCommand(options),
    stage,
    errorCode,
    error: redactedError(error),
    deferred: [...deferredCloudGates]
  });
}

// The run reports READY only after its own scratch root is actually gone. Any
// unconfirmed removal — including the Linux EACCES raised when the PostgreSQL
// image's chowned bind-mount directory blocks the qualifier's scan — keeps the
// attempt failed, preserving the earliest stage failure when one exists, so
// the receipt records NOT_READY instead of claiming a clean run. The removal
// stays confined to the run-owned scratch root. PostgreSQL's container-owned
// contents are cleared separately after confirmed Compose shutdown.
export async function finishLocalWorkspaceQualification(state, dependencies = {}) {
  const { options, startedAt, sourceTree, cloudImage, workspaceImage, stage, residuals } = state;
  const { recovery, ownerDeletePending, ownerDeleteFailure, finalReceipt } = state;
  let { failure } = state;
  if (!state.preserveRecoveryAuthority) {
    const removeScratch = dependencies.removeScratch || ((target) => rm(target, { recursive: true, force: true }));
    try {
      await removeScratch(state.tempRoot);
    } catch (error) {
      failure ||= error;
    }
  }

  if (failure) {
    const notReady = {
      schemaVersion: 1,
      status: "NOT_READY",
      startedAt,
      completedAt: new Date().toISOString(),
      source: { sha: options.sourceSha, tree: sourceTree },
      images: {
        cloud: { input: cloudImage || "unavailable", digest: immutableImageDigest(cloudImage) || "unavailable" },
        workspace: { input: workspaceImage || "unavailable", digest: immutableImageDigest(workspaceImage) || "unavailable" }
      },
      command: receiptCommand({ ...options, cloudImage, workspaceImage }),
      stage,
      errorCode: ["workspace_open_http_failed", "workspace_open_fixture_marker_missing"].includes(failure?.code)
        ? failure.code : "local_workspace_qualification_failed",
      error: redactedError(failure),
      residuals,
      ...(recovery?.artifact ? {
        recovery: {
          status: recovery.artifact.status,
          path: recovery.path,
          operationIdDigest: recovery.artifact.authority.operationIdDigest,
          workspaceIdDigest: recovery.artifact.authority.workspaceIdDigest,
          externalWrites: recovery.artifact.authority.externalWrites,
          compose: recovery.artifact.compose,
          cleanup: recovery.artifact.cleanup
        }
      } : {}),
      ...(ownerDeletePending ? { ownerDeletePending } : {}),
      ...(ownerDeleteFailure ? { ownerDeleteFailure } : {}),
      deferred: [...deferredCloudGates]
    };
    await writeJSONAtomic(options.receiptPath, notReady);
    throw failure;
  }
  await writeJSONAtomic(options.receiptPath, finalReceipt);
  return finalReceipt;
}

export async function runLocalWorkspaceQualification(options, dependencies = {}) {
  let liveAuthority = null;
  if (options.authorityMode === "live") {
    try {
      liveAuthority = dependencies.loadLiveAuthority ? await dependencies.loadLiveAuthority(options) : options.sub2apiSecretFile
        ? await loadSub2APISecretFile(options.sub2apiSecretFile)
        : liveAuthorityConfigurationFromEnvironment(process.env);
      if (!validLiveAuthorityConfiguration(liveAuthority)) {
        throw new Error("live qualification requires protected non-production Sub2API credentials and HTTPS authority");
      }
    } catch (error) {
      const errorCode = String(error?.code || "live_authority_configuration_missing");
      await writeEarlyNotReady(options, "authority_preflight", errorCode, error);
      throw error;
    }
  }
  const readSourceIdentity = dependencies.readSourceIdentity || readQualificationSourceIdentity;
  const sourceBefore = await readSourceIdentity();
  validateQualificationSourceIdentity(sourceBefore, sourceBefore, options.sourceSha);
  const sourceTree = sourceBefore.tree;
  let j0Ready = null;
  if (options.authorityMode === "live") {
    try {
      j0Ready = await loadJ0ReadyReceipt(options.j0ReadyReceipt, options.sourceSha, sourceTree);
    } catch (error) {
      await writeEarlyNotReady(options, "j0_ready_preflight", "j0_ready_receipt_invalid", error);
      throw error;
    }
  }
  // Every path below assembles the Local-Docker store stack, so the
  // installation facts it binds are admitted before anything starts.
  let installationInputs;
  try {
    installationInputs = localDockerInstallationInputs(process.env);
  } catch (error) {
    await writeEarlyNotReady(options, "installation_preflight", String(error?.code || "local_docker_installation_inputs_invalid"), error);
    throw error;
  }
  if (options.authorityMode === "live" && dependencies.runLiveJ1) {
    const result = await dependencies.runLiveJ1({ options, liveAuthority, source: sourceBefore, j0Ready });
    const receipt = validateLocalQualificationReceipt(result?.receipt || result);
    validateQualificationSourceIdentity(sourceBefore, await readSourceIdentity(), options.sourceSha);
    await writeJSONAtomic(options.receiptPath, receipt);
    return receipt;
  }
  const startedAt = new Date().toISOString();
  const suffix = `${process.pid}-${randomBytes(4).toString("hex")}`;
  const project = `opl-local-qualification-${suffix}`.toLowerCase();
  const tempRoot = await mkdtemp(join(tmpdir(), "opl-local-qualification-"));
  const fabricSecretRoot = join(tempRoot, "fabric-secrets");
  await mkdir(fabricSecretRoot, { recursive: true, mode: 0o700 });
  // The Local-Workspace overlay binds the PostgreSQL data root with
  // create_host_path: false, so the directory must exist before Compose starts;
  // PostgreSQL initializes and owns its contents inside the container.
  const postgresDataRoot = join(tempRoot, "postgres");
  await mkdir(postgresDataRoot, { recursive: true, mode: 0o700 });
  const envFile = join(tempRoot, "qualification.env");
  const publicPort = await unusedPort();
  const authorityPort = await unusedPort();
  const registryPort = await unusedPort();
  const subnetOctet = 20 + (Number.parseInt(randomBytes(1).toString("hex"), 16) % 200);
  let accountId = "acct-admin";
  const fixtureEmail = "local-qualification@example.test";
  const fixturePassword = `Local-${randomBytes(18).toString("base64url")}-Aa1!`;
  const adminEmail = options.authorityMode === "live" ? liveAuthority.qualificationUserEmail : fixtureEmail;
  const adminPassword = options.authorityMode === "live" ? liveAuthority.qualificationUserPassword : fixturePassword;
  const userToken = randomBytes(32).toString("hex");
  const authorityToken = randomBytes(32).toString("hex");
  const launchKey = `local-qualification:${suffix}`;
  // The owner mints the operation identity and the Workspace identity of a new
  // purchase, so the harness does not derive them and waits for the owner to
  // name them.
  let operationId = "";
  let workspaceId = "";
  let cloudImage = options.cloudImage;
  let workspaceImage = options.workspaceImage;
  let builtTags = [];
  let registryContainer = "";
  let stage = "image_admission";
  let composeStarted = false;
  let composeDown = false;
  let auth = null;
  let finalReceipt;
  let failure;
  let recovery = null;
  let preserveRecoveryAuthority = false;
  let ownerDeletePending = null;
  let ownerDeleteFailure = null;
  let residuals = { containers: null, volumes: null, networks: null };
  const composePrefix = ["compose", "--project-name", project, "--env-file", envFile];
  for (const file of localQualificationComposeFiles(options.authorityMode)) composePrefix.push("-f", file);
  let composeEnvironment = process.env;
  const compose = (args, settings = {}) => runProcess("docker", [...composePrefix, ...args], { ...settings, env: composeEnvironment });

  try {
    await runProcess("docker", ["version"]);
    await runProcess("docker", ["compose", "version"]);
    if (options.buildSourceImages) {
      ({ cloudImage, workspaceImage, tags: builtTags, registryContainer } = await buildSourceImages(options.sourceSha, project, registryPort));
    }
    const cloudInspection = await imageInspection(cloudImage);
    const workspaceInspection = await imageInspection(workspaceImage);
    const cloudRevision = String(cloudInspection.Config?.Labels?.["org.opencontainers.image.revision"] || "");
    if (cloudRevision !== options.sourceSha) throw new Error("Cloud image revision label does not equal source SHA");
    const cloudDigest = immutableImageDigest(cloudImage);
    const workspaceDigest = immutableImageDigest(workspaceImage);
    if (!cloudDigest || !workspaceDigest) throw new Error("qualified images must be immutable");

    const context = await runProcess("docker", ["context", "inspect", "--format", "{{(index .Endpoints \"docker\").Host}}"]);
    const dockerHost = context.stdout.trim();
    const dockerSocket = dockerHost.startsWith("unix://") ? dockerHost.slice("unix://".length) : "/var/run/docker.sock";
    const secrets = Array.from({ length: 10 }, () => randomBytes(32).toString("hex"));
    const envEntries = [
      ["OPL_CLOUD_IMAGE", cloudImage],
      ["OPL_WORKSPACE_IMAGE", workspaceImage],
      ["OPL_QUALIFICATION_SOURCE_SHA", options.sourceSha],
      ["OPL_BIND_ADDRESS", "127.0.0.1"],
      ["OPL_HTTP_PORT", publicPort],
      ["OPL_PUBLIC_URL", `http://127.0.0.1:${publicPort}`],
      // The Control Plane refuses to start without a Workspace host, and it must
      // differ from OPL_PUBLIC_URL because that host serves Workspace entries
      // while every other host serves the Console this script drives.
      ["OPL_WORKSPACE_DOMAIN", `ws-qualification-${suffix}.localhost`],
      // The qualification run bills its own fixture authority, so it qualifies
      // the platform-owned deployment mode.
      ["OPL_DEPLOYMENT_MODE", "platform_owned"],
      ["OPL_POSTGRES_DATA_ROOT", postgresDataRoot],
      ["OPL_WORKSPACE_IMAGE_RELEASES_JSON", ""],
      // Installation-owned facts, already verified by the installation
      // preflight: the quota-enabled Workspace storage root and the provider
      // profile that describes the packages this host can host.
      ["OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT", installationInputs.storageRoot],
      ["OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", installationInputs.providerProfileJSON],
      ["OPL_DOCKER_SUBNET", `10.251.${subnetOctet}.0/24`],
      ["OPL_POSTGRES_HOST", `10.251.${subnetOctet}.10`],
      ["OPL_POSTGRES_ADMIN_PASSWORD", secrets[0]],
      ["OPL_CONTROL_PLANE_DATABASE_PASSWORD", secrets[1]],
      ["OPL_FABRIC_DATABASE_PASSWORD", secrets[2]],
      ["OPL_LEDGER_DATABASE_PASSWORD", secrets[3]],
      ["OPL_CONTROL_PLANE_SERVICE_TOKEN", secrets[4]],
      ["OPL_FABRIC_SERVICE_TOKEN", secrets[5]],
      ["OPL_LEDGER_SERVICE_TOKEN", secrets[6]],
      ["OPL_FABRIC_RUNNER_SERVICE_TOKEN", secrets[7]],
      ["OPL_FABRIC_CAPABILITY_KEY", secrets[8]],
      ["OPL_LEDGER_CAPABILITY_KEY", secrets[9]],
      ["OPL_AIONUI_ADMIN_PASSWORD_SEED", randomBytes(32).toString("hex")],
      ["OPL_SUB2API_BASE_URL", options.authorityMode === "live" ? liveAuthority.baseURL : "http://sub2api-authority:8080"],
      ["OPL_SUB2API_ADMIN_EMAIL", options.authorityMode === "live" ? liveAuthority.adminEmail : adminEmail],
      ["OPL_SUB2API_ADMIN_PASSWORD", options.authorityMode === "live" ? liveAuthority.adminPassword : adminPassword],
      ["OPL_QUALIFICATION_USER_EMAIL", adminEmail],
      ["OPL_QUALIFICATION_USER_PASSWORD", adminPassword],
      ["OPL_QUALIFICATION_USER_TOKEN", userToken],
      ["OPL_QUALIFICATION_AUTHORITY_TOKEN", authorityToken],
      ["OPL_QUALIFICATION_AUTHORITY_HOST_PORT", authorityPort],
      ["OPL_QUALIFICATION_INITIAL_USD_MICROS", "1000000000"],
      ["OPL_DOCKER_SOCKET_PATH", dockerSocket],
      ["OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT", fabricSecretRoot],
      ["OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER", `${project}-control-plane-1`],
      ["OPL_SUB2API_REQUEST_TIMEOUT_MS", "5000"],
      ["OPL_MONTHLY_BILLING_WORKER_ENABLED", "0"],
      ["OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "1"],
      ["OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES", workspaceImage],
      ["OPL_FABRIC_LOCAL_DOCKER_HOST", "127.0.0.1"]
    ];
    if (options.authorityMode === "live") {
      accountId = `acct-${stableID("account", adminEmail.toLowerCase()).slice(0, 18)}`;
    }
    composeEnvironment = qualificationComposeEnvironment(process.env, envEntries);
    const envFileEntries = qualificationEnvFileEntries(envEntries, options);
    await writeFile(envFile, `${envFileEntries.map(([key, value]) => `${key}=${value}`).join("\n")}\n`, { mode: 0o600 });
    stage = "owner_topology_preflight";
    assertWorkspaceQualificationTopology(JSON.parse((await compose(["config", "--format", "json"])).stdout));

    stage = "compose_start";
    composeStarted = true;
    await waitForCompose(compose);
    const [controlPlaneContainer, fabricContainer, ledgerContainer] = await Promise.all([
      inspectComposeImage(compose, "control-plane"),
      inspectComposeImage(compose, "fabric"),
      inspectComposeImage(compose, "ledger")
    ]);
    if ([controlPlaneContainer, fabricContainer, ledgerContainer].some((container) => container.Image !== cloudInspection.Id)) {
      throw new Error("one or more Cloud services did not run the admitted image");
    }
    const stores = await verifyStores(compose);
    const http = createHTTP(`http://127.0.0.1:${publicPort}`);

    if (options.authorityMode === "live") {
      finalReceipt = (await runLocalWorkspaceJ1HTTPQualification({
        http,
        adminEmail: liveAuthority.adminEmail,
        adminPassword: liveAuthority.adminPassword,
        qualificationEmail: adminEmail,
        qualificationPassword: adminPassword,
        accountProvisionKey: `local-qualification-account:${stableID(adminEmail.toLowerCase()).slice(0, 24)}`,
        launchKey,
        workspaceName: `Local qualification ${suffix}`,
        wait: (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)),
        onStage: (name) => { stage = name; },
        readRuntime: async ({ accountId: exactAccountId, workspaceId: exactWorkspaceId, runtimeId: exactRuntimeId }) => {
          const runtime = await applicationRuntimeImageReadback(exactAccountId, exactWorkspaceId, workspaceImage, workspaceInspection.Id, exactRuntimeId);
          return { runningDigest: runtime.Image };
        },
        readDebit: async ({ sub2apiUserId, code, amountUsdMicros }) => {
          const readback = await liveAuthorityAdjustmentReadback(
            liveAuthority.baseURL, liveAuthority.adminEmail, liveAuthority.adminPassword,
            sub2apiUserId, code, `-${amountUsdMicros}`
          );
          if (readback.valueUsdMicros !== `-${amountUsdMicros}`) throw new Error("live Sub2API debit amount readback is invalid");
          return { code: readback.code, userId: readback.userId, amountUsdMicros, count: readback.count };
        },
        cleanup: async (scope) => {
          const authority = scope?.recoveryAuthority || { externalWrites: noLocalJ1ExternalWrites() };
          const cleanupPlan = localJ1CleanupPlan({
            ready: !scope?.failed,
            launchSubmitted: scope?.launchSubmitted === true,
            authority
          });
          if (cleanupPlan.preserveRecoveryAuthority) {
            preserveRecoveryAuthority = true;
            const postgresVolumeRefs = (await runProcess("docker", [
              "volume", "ls", "-q", "--filter", `label=com.docker.compose.project=${project}`
            ])).stdout.trim().split(/\r?\n/).filter(Boolean);
            const providerVolumeRefs = scope?.accountId && scope?.workspaceId
              ? await exactLabelIDs("volumes", scope.accountId, scope.workspaceId)
              : [];
            const recoveryPath = localJ1RecoveryArtifactPath(options.receiptPath);
            const common = {
              source: { sha: options.sourceSha, tree: sourceTree },
              failure: { stage, errorCode: "local_workspace_qualification_failed" },
              compose: {
                project,
                recoveryRoot: tempRoot,
                fabricSecretRoot,
                postgresVolumeRefs,
                providerVolumeRefs
              }
            };
            // The order was admitted under the identities the owner minted, and
            // the recovery artifact names those identities rather than a
            // retained launch identity the harness no longer derives.
            const artifact = scope?.recoveryAuthority
              ? createLocalJ1RecoveryArtifact({ ...common, authority })
              : createLocalJ1RecoveryReadbackPendingArtifact({
                ...common, operationId: String(scope?.operationId || ""), workspaceId: String(scope?.workspaceId || ""),
                idempotencyKey: String(scope?.idempotencyKey || ""),
                quoteId: String(scope?.quoteId || ""), workspaceName: scope?.workspaceName, declaration: scope?.declaration,
              });
            await writeLocalJ1RecoveryArtifact(recoveryPath, artifact);
            recovery = { status: artifact.status, path: recoveryPath, artifact };
            return null;
          }
          if (!scope?.accountId || !scope?.workspaceId) return { containers: 0, volumes: 0, networks: 0 };
          await cleanupLocalQualificationSecretRoot(compose, fabricSecretRoot);
          const counts = await cleanupLocalQualificationResources(scope.accountId, scope.workspaceId, async () => {
            const down = await compose(["down", "--volumes", "--remove-orphans", "--timeout", "30"], { allowFailure: false });
            if (down.code !== 0) throw new Error("local qualification compose teardown failed");
            composeDown = true;
          });
          await rm(fabricSecretRoot, { recursive: true, force: true });
          await mkdir(fabricSecretRoot, { recursive: true, mode: 0o700 });
          return counts;
        },
        receiptBase: {
          schemaVersion: 1,
          status: "READY",
          startedAt,
          completedAt: new Date().toISOString(),
          source: { sha: options.sourceSha, tree: sourceTree },
          images: {
            cloud: { input: cloudImage, repoDigest: cloudImage, digest: cloudDigest, runningDigest: cloudInspection.Id },
            workspace: { input: workspaceImage, repoDigest: workspaceImage, digest: workspaceDigest, runningDigest: workspaceInspection.Id }
          },
          command: receiptCommand({ ...options, cloudImage, workspaceImage }),
          processes: { console: "ready", controlPlane: "ready", fabric: "ready", ledger: "ready" },
          stores,
          j0Ready,
          qualification: { authorityMode: "live", p0Ready: true },
          deferred: [...deferredCloudGates]
        }
      })).receipt;
      validateQualificationSourceIdentity(sourceBefore, await readQualificationSourceIdentity(), options.sourceSha);
    } else {
    stage = "console_and_login";
    await consoleReadback(http);
    auth = await openQualificationSessions(http, adminEmail, adminPassword, "acct-admin", `local-workspace-owner:${launchKey}`);
    const me = sourceData((await http.json("/api/auth/me", {}, auth)).payload, "sub2api");
    const sub2apiUserId = String(me?.sub2apiUserId || "");
    if (me?.accountId !== accountId || !/^[1-9][0-9]*$/.test(sub2apiUserId) || me?.status !== "active") {
      throw new Error("qualification authority identity binding is invalid");
    }
    const walletBefore = sourceData((await http.json("/api/gateway/wallet", {}, auth)).payload, "sub2api");
    const beforeMicros = String(walletBefore?.usdMicros || "");
    if (!/^[1-9][0-9]*$/.test(beforeMicros)) throw new Error("qualification wallet readback is invalid");

    stage = "catalog_resolution";
    const declaration = await resolveLocalWorkspaceDeclaration(http, auth);
    stage = "workspace_order";
    const workspaceName = `Local qualification ${suffix}`;
    const { amountUsdMicros, quote, operation, operationId: ownerOperationId, workspaceId: ownerWorkspaceId } = await buyLocalWorkspace({
      http, auth, name: workspaceName, declaration, walletUsdMicros: beforeMicros, idempotencyKey: launchKey,
    });
    operationId = ownerOperationId;
    workspaceId = ownerWorkspaceId;

    // The owner order names the application it installs, so the owner commits
    // the default OPL application installation beside it. Wait for the owner's
    // own ready readback, then bind the Workspace Key the installation created.
    stage = "application_installation";
    const installation = await waitForWorkspaceApplicationInstallation(http, auth, { workspaceId, operationId });
    const workspaceKey = resolveWorkspaceKey(await readAllGatewayKeys(http, auth), workspaceId);
    const keyId = workspaceKey.id;
    stage = "terminal_readback";
    const receiptId = await readLocalPurchaseReceiptId(http, auth, workspaceId);
    const evidence = await readWorkspaceEvidence(http, auth, operationId, workspaceId, receiptId);
    if (localApplicationEntryPort(evidence.current.entryUrl) === 0) throw new Error("Workspace application entry is invalid");
    stage = "workspace_open";
    const opened = await fetchApplicationEntry(evidence.current.entryUrl);
    assertWorkspaceOpenQualification(opened);
    const runtimeContainer = await applicationRuntimeImageReadback(accountId, workspaceId, workspaceImage, workspaceInspection.Id, evidence.runtime.runtimeId);
    const usage = sourceData((await http.json("/api/gateway/usage-summary?period=month", {}, auth)).payload, "sub2api");
    const walletAfterCharge = sourceData((await http.json("/api/gateway/wallet", {}, auth)).payload, "sub2api");
    const chargedMicros = String(walletAfterCharge?.usdMicros || "");
    if (!usage || typeof usage.totalRequests !== "number") throw new Error("Sub2API usage readback is invalid");
    const authorityBeforeDelete = options.authorityMode === "fixture" ? await authorityState(authorityPort, authorityToken) : null;
    const debits = authorityBeforeDelete?.adjustments?.filter((candidate) => candidate?.kind === "debit") || [];
    const liveDebit = options.authorityMode === "live" ? await liveAuthorityAdjustmentReadback(
      liveAuthority.baseURL, liveAuthority.adminEmail, liveAuthority.adminPassword,
      sub2apiUserId, evidence.receipt.chargeReference, `-${amountUsdMicros}`
    ) : null;
    const debit = options.authorityMode === "fixture" ? debits.find((candidate) => candidate?.code === evidence.receipt.chargeReference) : {
      code: liveDebit.code, userId: liveDebit.userId, amountUsdMicros, count: liveDebit.count
    };
    const debitCount = options.authorityMode === "fixture" ? debits.filter((candidate) => candidate?.code === evidence.receipt.chargeReference).length : liveDebit.count;
    const afterMicros = options.authorityMode === "fixture" ? String(authorityBeforeDelete.wallet?.usdMicros || "") : chargedMicros;
    if (options.authorityMode === "fixture" && (debits.length !== 1 || authorityBeforeDelete.writeCounts?.debits !== 1 || !debit || String(debit.userId) !== "41" || String(debit.amountUsdMicros) !== amountUsdMicros)) {
      throw new Error("qualification authority exact debit evidence is invalid");
    }
    if (!/^\d+$/.test(afterMicros) || options.authorityMode === "fixture" && BigInt(beforeMicros) - BigInt(amountUsdMicros) !== BigInt(afterMicros)) {
      throw new Error("qualification wallet debit snapshot is invalid");
    }
    const receipt = evidence.receipt;
    // The Workspace owner's typed financial receipt is the only purchase
    // evidence this order has. It names the exact charge the authority debited
    // and no Runtime or Gateway Key of the application installation, so the
    // retained Control Plane billing projection is never consulted or backfilled.
    if (receipt.chargeReference !== debit.code || String(receipt.totalUsdMicros) !== amountUsdMicros ||
      receipt.financial?.receipt?.kind !== "wallet_action" || receipt.financial?.receipt?.outcome !== "confirmed" ||
      receipt.financial?.walletOperation?.kind !== "charge" || receipt.financial?.walletOperation?.status !== "confirmed" ||
      receipt.fulfillment?.runtimeId !== undefined || receipt.fulfillment?.workspaceApiKeyId !== undefined) {
      throw new Error("Ledger receipt exact binding is invalid");
    }

    stage = "restart_continuity";
    await compose(["restart", "workspace", "console-bff", "tenant", "serve", "fabric", "ledger"]);
    await waitForCompose(compose);
    // Re-establish CloudIdentity authentication under the same declared customer
    // after restart; persisted session lifetime remains the identity owner's.
    // the Control Plane cookie alone is refused by the BFF's RequireSession.
    const restartedAuth = await openQualificationSessions(
      http, adminEmail, adminPassword, accountId, `local-workspace-restart:${launchKey}`,
    );
    if (!String(restartedAuth.cookie || "").includes("opl_console_session=")) {
      throw new Error("restart continuity did not re-establish the CloudIdentity session");
    }
    const afterRestart = await readWorkspaceEvidence(http, restartedAuth, operationId, workspaceId, receiptId);
    const restart = {
      performed: true,
      operationStable: afterRestart.operation.operationId === operation.operationId,
      workspaceStable: afterRestart.workspace.id === evidence.workspace.id,
      runtimeStable: afterRestart.runtime.runtimeId === evidence.runtime.runtimeId,
      receiptStable: afterRestart.receipt.receiptId === receipt.receiptId
    };
    if (Object.values(restart).some((value) => value !== true)) throw new Error("restart continuity changed an exact identity");

    stage = "owner_delete";
    // A Workspace bought on the owner route is deleted by the Workspace owner.
    // The retained Control Plane route never saw this Workspace, so the harness
    // sends the owner's own Delete under the restarted customer session and
    // follows the operation identity the owner minted rather than deriving one.
    const deletion = await deleteWorkspaceOnOwnerRoute(http, restartedAuth, {
      workspaceId, confirmationName: workspaceName, idempotencyKey: `local-qualification-delete:${workspaceId}`,
    });
    const deletionEvidence = await readWorkspaceDeletionEvidence(http, restartedAuth, {
      workspaceId, operationId: deletion.operationId,
    });
    const workspaceAbsent = deletionEvidence.resourceDeletionStatus === "confirmed";
    const runtimeAbsent = deletionEvidence.dataDeletionStatus === "confirmed";
    residuals = await residualCounts(accountId, workspaceId);
    const authorityAfterOwnerDelete = await authorityState(authorityPort, authorityToken);
    // The Workspace-reserved Gateway Key is the one object Delete must not
    // remove: the platform retains it, so the authority still owns and meters
    // exactly the Key this installation created after the Workspace is gone.
    const workspaceKeyRetained = retainedWorkspaceKey(authorityBeforeDelete?.keys || [], authorityAfterOwnerDelete?.keys || [], workspaceId, keyId);
    // Fabric's injected Secret is a provider secret-root directory named from
    // the Workspace identity; that exact ref must be gone after Delete.
    const fabricSecretAbsent = !existsSync(join(fabricSecretRoot, workspaceGatewaySecretRef(workspaceId)));
    if (!runtimeAbsent || !workspaceKeyRetained || !fabricSecretAbsent || Object.values(residuals).some((count) => count !== 0)) {
      throw new Error("owner DELETE did not prove Runtime and Secret cleanup with Gateway Key retention");
    }
    // Workspace already read back the Ledger absence receipt. Gateway's
    // transaction readback separately proves its refund of the original charge.
    const refund = await readWorkspaceRefundReceipt(http, restartedAuth, {
      workspaceId, chargeAmountUsdMicros: amountUsdMicros,
      chargeOperationId: evidence.receipt.financial.walletOperation.id, refundOperationId: deletionEvidence.refundOperationId,
    });
    const refundEvidence = {
      count: 1, walletOperationId: refund.walletOperationId, receiptId: refund.receiptId,
      amountUsdMicros: refund.amountUsdMicros, code: refund.code,
      originalChargeOperationId: refund.originalChargeOperationId,
    };
    let afterDeleteMicros = afterMicros;
    if (options.authorityMode === "fixture") {
      const adjustmentsAfterDelete = authorityAfterOwnerDelete?.adjustments || [];
      const debitAfterDelete = adjustmentsAfterDelete.filter((candidate) => candidate?.kind === "debit" && candidate?.code === debit.code);
      const refundsAfterDelete = adjustmentsAfterDelete.filter((candidate) => candidate?.kind === "refund");
      // The fixture authority is the wallet provider this run billed, so its own
      // adjustment ledger must show exactly the debit it accepted and exactly one
      // refund whose amount and payer are the ones the owner's receipt publishes.
      const adjustment = refundsAfterDelete[0];
      if (debitAfterDelete.length !== 1 || authorityAfterOwnerDelete?.writeCounts?.debits !== 1 ||
        refundsAfterDelete.length !== 1 || authorityAfterOwnerDelete?.writeCounts?.refunds !== 1 ||
        String(adjustment?.valueUsdMicros || "") !== refund.amountUsdMicros ||
        String(adjustment?.amountUsdMicros || "") !== refund.amountUsdMicros ||
        String(adjustment?.userId || "") !== String(sub2apiUserId) || adjustment?.status !== "used" ||
        String(adjustment?.code || "") !== refund.code || !String(adjustment?.code || "").startsWith("opl:") ||
        !Number.isFinite(Date.parse(adjustment?.usedAt))) {
        throw new Error("platform refund for the deleted Workspace is not exactly bound");
      }
      refundEvidence.userId = String(adjustment.userId);
      refundEvidence.code = String(adjustment.code);
      afterDeleteMicros = String(authorityAfterOwnerDelete?.wallet?.usdMicros || "");
      if (!/^\d+$/.test(afterDeleteMicros) || BigInt(afterDeleteMicros) !== BigInt(afterMicros) + BigInt(refund.amountUsdMicros)) {
        throw new Error("qualification wallet changed during Workspace Delete");
      }
    }

    validateQualificationSourceIdentity(sourceBefore, await readQualificationSourceIdentity(), options.sourceSha);
    finalReceipt = validateLocalQualificationReceipt({
      schemaVersion: 1,
      status: "READY",
      startedAt,
      completedAt: new Date().toISOString(),
      source: { sha: options.sourceSha, tree: sourceTree },
      images: {
        cloud: { input: cloudImage, repoDigest: cloudImage, digest: cloudDigest, runningDigest: cloudInspection.Id },
        workspace: { input: workspaceImage, repoDigest: workspaceImage, digest: workspaceDigest, runningDigest: runtimeContainer.Image }
      },
      command: receiptCommand({ ...options, cloudImage, workspaceImage }),
      processes: { console: "ready", controlPlane: "ready", fabric: "ready", ledger: "ready" },
      stores,
      identities: {
        accountId, sub2apiUserId, operationId, deleteOperationId: String(deletion.operationId || ""),
        deletionReceiptId: deletionEvidence.deletionReceipt.id, workspaceId,
        runtimeId: evidence.runtime.runtimeId, keyId, debitCode: debit.code,
        purchaseReceiptId: receiptId,
        runtimeVersionId: declaration.applicationSelection.runtimeVersionId,
        computePlanId: declaration.computePlanId, storagePlanId: declaration.storagePlanId
      },
      debit: { count: debitCount, accountId, operationId, workspaceId, code: debit.code, userId: String(debit.userId), amountUsdMicros },
      wallet: { beforeUsdMicros: beforeMicros, afterUsdMicros: afterMicros, afterDeleteUsdMicros: afterDeleteMicros },
      receipt: {
        count: 1, id: receipt.receiptId, accountId, operationId, workspaceId: receipt.workspaceId, quoteId: String(quote?.id || ""),
        chargeOperationId: evidence.receipt.financial.walletOperation.id,
        computeAllocationId: String(receipt.fulfillment.computeAllocationId || ""),
        storageId: String(receipt.fulfillment.storageId || ""), attachmentId: String(receipt.fulfillment.attachmentId || ""),
        chargeReference: receipt.chargeReference, amountUsdMicros: String(receipt.totalUsdMicros)
      },
      application: {
        status: evidence.current.status, operationId: evidence.current.operationId, applicationId: evidence.current.applicationId,
        revision: evidence.current.revision, entryUrl: evidence.current.entryUrl,
        runtimeId: evidence.runtime.runtimeId, keyId, resumed: installation.resumed === true
      },
      restart,
      deletion: {
        // The owner reports the deletion it executed; the Workspace is gone, so
        // the harness publishes the absence it observed and not a Runtime or Key
        // identity the owner never returned for this deletion.
        ownerAuthorized: true, accountId, operationId: String(deletion.operationId || ""),
        deletionReceiptId: deletionEvidence.deletionReceipt.id, workspaceId,
        runtimeId: "", workspaceApiKeyId: "",
        workspaceAbsent, runtimeAbsent, workspaceKeyRetained, fabricSecretAbsent
      },
      deletionReceipt: {
        count: 1, ...deletionEvidence.deletionReceipt, workspaceId,
      },
      residuals,
      authorityWriteCounts: authorityAfterOwnerDelete?.writeCounts,
      mutationCounts: { workspaceQuotePosts: 1, workspaceOrderPosts: 1, workspaceDeleteRequests: 1, refundPosts: 0 },
      refund: refundEvidence,
      usage: { source: "sub2api", status: "available", totalRequests: usage.totalRequests },
      qualification: { authorityMode: options.authorityMode, p0Ready: false },
      deferred: [...deferredCloudGates]
    });
    }
  } catch (error) {
    failure = error;
  } finally {
    if (composeStarted && !preserveRecoveryAuthority) {
      if (!composeDown) {
        try {
          await cleanupLocalQualificationSecretRoot(compose, fabricSecretRoot);
        } catch (error) {
          failure ||= error;
        }
      }
      const stop = await compose(["stop", "--timeout", "30", "control-plane", "fabric"], { allowFailure: true });
      const down = await compose(["down", "--volumes", "--remove-orphans"], { allowFailure: true });
      if (down.code === 0) {
        try {
          await cleanupLocalQualificationPostgresRoot(tempRoot, postgresDataRoot);
        } catch (error) {
          failure ||= error;
        }
      }
      const observed = await residualCounts(accountId, workspaceId).catch(() => null);
      if (stop.code !== 0 || !observed || down.code !== 0) failure ||= new Error("local qualification teardown was not confirmed");
      if (observed) residuals = observed;
    }
    if (!preserveRecoveryAuthority) for (const tag of builtTags) await runProcess("docker", ["image", "rm", tag], { allowFailure: true });
    if (registryContainer && !preserveRecoveryAuthority) {
      const removed = await runProcess("docker", ["rm", "-f", registryContainer], { allowFailure: true });
      if (removed.code !== 0) failure ||= new Error("local source registry cleanup was not confirmed");
    }
  }

  return finishLocalWorkspaceQualification({
    options,
    startedAt,
    sourceTree,
    cloudImage,
    workspaceImage,
    stage,
    failure,
    residuals,
    recovery,
    ownerDeletePending,
    ownerDeleteFailure,
    finalReceipt,
    tempRoot,
    preserveRecoveryAuthority
  }, dependencies);
}

async function main() {
  let options;
  try {
    options = parseLocalQualificationArgs();
    const receipt = await runLocalWorkspaceQualification(options);
    process.stdout.write(`${JSON.stringify(receipt, null, 2)}\n`);
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
