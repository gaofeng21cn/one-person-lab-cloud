import { spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { constants, existsSync } from "node:fs";
import { mkdir, mkdtemp, open, readFile, realpath, rename, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseDocument } from "yaml";

import {
  collectLocalJ1RecoveryAuthority,
  createLocalJ1RecoveryArtifact,
  createLocalJ1RecoveryReadbackPendingArtifact,
  localJ1CleanupPlan,
  localJ1RecoveryArtifactPath,
  noLocalJ1ExternalWrites,
  writeLocalJ1RecoveryArtifact
} from "./local-workspace-recovery.ts";

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

function j0ReadyReceiptError() {
  const error = new Error("j0_ready_receipt_invalid");
  error.code = "j0_ready_receipt_invalid";
  return error;
}

function hasExactKeys(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

export function validateJ0ReadyReceipt(value, sourceSha, sourceTree) {
  if (!hasExactKeys(value, ["schemaVersion", "kind", "status", "source", "authority", "provider", "failed", "skipped"]) ||
    value.schemaVersion !== 1 || value.kind !== "opl.local-workspace.j0-ready.v1" || value.status !== "READY" ||
    !hasExactKeys(value.source, ["sha", "tree", "clean"]) ||
    value.source?.sha !== sourceSha || value.source?.tree !== sourceTree || value.source?.clean !== true ||
    value.failed !== 0 || value.skipped !== 0 || value.provider !== "local-docker" ||
    !hasExactKeys(value.authority, ["mode", "class", "dedicated", "confirmed"]) ||
    value.authority?.mode !== "live" || value.authority?.class !== "sandbox" ||
    value.authority?.dedicated !== true || value.authority?.confirmed !== true) {
    throw j0ReadyReceiptError();
  }
  return value;
}

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
      provider: "local-docker",
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
    value.j0Ready?.provider !== "local-docker" || value.j0Ready?.authority?.mode !== "live" ||
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
  const identityKeys = ["accountId", "sub2apiUserId", "launchOperationId", "workspaceId", "runtimeId", "keyId", "debitCode", "purchaseReceiptId"];
  if (!live) identityKeys.push("deleteOperationId", "deletionReceiptId");
  for (const key of identityKeys) {
    requireString(value.identities?.[key], `identity ${key}`);
  }
  if (value.debit?.count !== 1 || !String(value.debit?.code || "").startsWith("opl:") ||
    value.debit?.accountId !== value.identities.accountId || value.debit?.code !== value.identities.debitCode || value.debit?.operationId !== value.identities.launchOperationId ||
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
  // The purchase receipt binds exactly the resources this resource-only Launch
  // delivered. The Runtime and the Workspace Key belong to the default
  // application installation and are bound by its own evidence block.
  if (value.receipt?.count !== 1 || value.receipt?.id !== value.identities.purchaseReceiptId ||
    value.receipt?.accountId !== value.identities.accountId || value.receipt?.operationId !== value.identities.launchOperationId ||
    value.receipt?.workspaceId !== value.identities.workspaceId || value.receipt?.provisioningMode !== "resource_only" ||
    value.receipt?.runtimeId !== undefined || value.receipt?.keyId !== undefined ||
    !String(value.receipt?.computeAllocationId || "").trim() || !String(value.receipt?.storageId || "").trim() ||
    !String(value.receipt?.attachmentId || "").trim() ||
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
      String(value.deletion?.runtimeId) !== "" || String(value.deletion?.workspaceApiKeyId) !== "0") {
      throw new Error("owner deletion evidence is invalid");
    }
  }
  if (live) {
    if (value.deletionReceipt?.count !== 0) throw new Error("live qualification must not record a deletion receipt");
  } else if (value.deletionReceipt?.count !== 1 || value.deletionReceipt?.id !== value.identities.deletionReceiptId ||
    value.deletionReceipt?.type !== "workspace.deleted.v1" ||
    value.deletionReceipt?.operationId !== value.identities.deleteOperationId || value.deletionReceipt?.workspaceId !== value.identities.workspaceId ||
    value.deletionReceipt?.launchReceiptId !== value.identities.purchaseReceiptId ||
    value.deletionReceipt?.resourceType !== "workspace" || value.deletionReceipt?.resourceId !== value.identities.workspaceId ||
    value.deletionReceipt?.resourceStatus?.runtimeStatus !== "absent" || value.deletionReceipt?.resourceStatus?.gatewaySecretStatus !== "absent" ||
    value.deletionReceipt?.resourceStatus?.attachmentStatus !== "absent" || value.deletionReceipt?.resourceStatus?.storageStatus !== "absent" ||
    value.deletionReceipt?.resourceStatus?.computeStatus !== "absent" || value.deletionReceipt?.resourceStatus?.workspaceStatus !== "absent" ||
    !workspaceDeleteReceiptStagesMatch(value.deletionReceipt?.stageEvidence)) {
    throw new Error("deletion receipt binding is invalid");
  }
  if (["containers", "volumes", "networks"].some((key) => value.residuals?.[key] !== 0)) {
    throw new Error("exact-labelled residual evidence is invalid");
  }
  // Delete performs no Gateway mutation in either mode: the Workspace-reserved
  // Key stays retained in the authority, so no Key deletion is ever admitted.
  // The fixture lane performs the owner Delete, so Control Plane also performs
  // the platform hourly refund for the deleted Workspace; the live lane does not
  // delete at all and therefore records no refund.
  const expectedAuthorityWrites = live
    ? { keyCreates: 1, keyDeletes: 0, debits: 1, refunds: 0 }
    : { keyCreates: 1, keyDeletes: 0, debits: 1, refunds: 1 };
  if (Object.entries(expectedAuthorityWrites).some(([key, count]) => value.authorityWriteCounts?.[key] !== count)) {
    throw new Error("qualification authority write counts are invalid");
  }
  const expectedMutationCounts = live
    ? { accountProvisionPosts: 1, workspaceLaunchPosts: 1, workspaceDeleteRequests: 0, refundPosts: 0 }
    : { workspaceLaunchPosts: 1, workspaceDeleteRequests: 1, refundPosts: 0 };
  if (Object.entries(expectedMutationCounts).some(([key, count]) => value.mutationCounts?.[key] !== count)) {
    throw new Error("qualification mutation counts are invalid");
  }
  if (live) {
    if (value.refund?.count !== 0) throw new Error("live qualification must not refund");
  } else if (value.refund?.count !== 1 ||
    !/^wallet-adjustment-delete-[0-9a-f]{24}$/.test(String(value.refund?.walletOperationId || "")) ||
    !String(value.refund?.receiptId || "").trim() || !String(value.refund?.code || "").startsWith("opl:") ||
    String(value.refund?.userId || "") !== String(value.identities.sub2apiUserId) ||
    value.refund?.relatedOperationId !== value.identities.launchOperationId ||
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

export function stableID(...parts) {
  const hash = createHash("sha1");
  for (const part of parts) {
    hash.update(String(part));
    hash.update(Buffer.from([0]));
  }
  return hash.digest("hex");
}

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

export function createHTTP(origin) {
  const request = async (path, init = {}, auth = null) => {
    const headers = new Headers(init.headers || {});
    if (auth?.cookie) headers.set("cookie", auth.cookie);
    if (auth?.csrf) headers.set("x-opl-csrf", auth.csrf);
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
  return { request, json };
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

export async function waitForLaunch(http, operationId, auth, wait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds))) {
  let launch;
  for (let attempt = 0; attempt < 180; attempt += 1) {
    launch = (await http.json(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth)).payload;
    if (launch?.status === "succeeded" && launch?.phase === "succeeded") return launch;
    if (["manual_review", "failed", "refunded"].includes(String(launch?.status || ""))) {
      throw new Error(`Workspace launch stopped at ${launch.status}/${launch.phase}/${launch.errorCode || "none"}`);
    }
    await wait(1000);
  }
  throw new Error("Workspace launch did not reach succeeded within 180 seconds");
}

// The default OPL application installation is committed beside the resource
// Launch, so the owner reports it as its own pending generation until its
// Runtime is ready. A stalled credential preparation is resumed exactly once
// through the customer's own continuation command; an installation the owner
// parks for review without that continuation stops the qualification.
export async function waitForWorkspaceApplicationInstallation(http, auth, {
  workspaceId, operationId,
  wait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)),
  attempts = 300
} = {}) {
  let resumed = false;
  let installation = null;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const page = sourceData((await http.json("/api/workspaces?page=1&pageSize=20", {}, auth)).payload, "control-plane");
    const workspace = page?.items?.find((candidate) => candidate?.id === workspaceId);
    if (!workspace) throw new Error("Workspace owner readback is invalid");
    installation = workspace.applicationInstallation || null;
    if (workspace.currentApplication?.status === "ready") return { workspace, installation, resumed, attempts: attempt + 1 };
    if (installation?.canResume === true && !resumed) {
      const continued = await http.json(`/api/workspaces/${encodeURIComponent(workspaceId)}/application-installation/resume`, {
        method: "POST", headers: { "idempotency-key": `application-resume:${operationId}` }, body: {}
      }, auth, [202]);
      const projection = continued.payload;
      if (projection?.workspaceId !== workspaceId ||
        projection.applicationInstallation && (projection.applicationInstallation.operationId !== installation.operationId ||
          projection.applicationInstallation.applicationId !== installation.applicationId ||
          projection.applicationInstallation.revision !== installation.revision)) {
        throw new Error("Workspace application installation continuation is invalid");
      }
      resumed = true;
      continue;
    }
    if (installation?.status === "manual_review") throw new Error("Workspace application installation stopped for review");
    await wait(1000);
  }
  throw new Error("Workspace application installation did not become ready within 300 seconds");
}

export async function waitForCompose(compose) {
  await compose(["up", "-d", "--wait", "--wait-timeout", "300"]);
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

// A Local purchase is a resource Launch: the Control Plane commits the default
// OPL application installation beside it, and the resource Launch never carries
// an application URL. The customer entry, the Runtime and the Workspace Key
// therefore belong to the selected application generation, so the owner
// readback binds that generation instead of a URL this purchase never had.
export async function readWorkspaceEvidence(http, auth, operationId, workspaceId, receiptId) {
  const launch = (await http.json(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth)).payload;
  if (launch?.status !== "succeeded" || launch?.phase !== "succeeded" || launch?.workspaceId !== workspaceId || launch?.receiptId !== receiptId) {
    throw new Error("Workspace launch continuity readback is invalid");
  }
  const page = sourceData((await http.json("/api/workspaces?page=1&pageSize=20", {}, auth)).payload, "control-plane");
  const workspace = page?.items?.find((candidate) => candidate?.id === workspaceId);
  const current = workspace?.currentApplication;
  if (!workspace || !current || current.status !== "ready" || !String(current.entryUrl || "").trim() ||
    workspace.url !== current.entryUrl || workspace.openable !== true) {
    throw new Error("Workspace owner readback is invalid");
  }
  const runtime = sourceData((await http.json(`/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-status`, {}, auth)).payload, "fabric");
  if (!runtime || runtime.workspaceId !== workspaceId || runtime.ready !== true || runtime.status !== "running" ||
    runtime.url !== current.entryUrl || runtime.currentApplication?.operationId !== current.operationId ||
    !String(runtime.runtimeId || "").trim()) {
    throw new Error("Workspace runtime readback is invalid");
  }
  // The resource-only purchase receipt binds exactly the resources it delivered:
  // a Runtime and a Workspace Key belong to the installation request, never to
  // this receipt.
  const receipt = sourceData((await http.json(`/api/billing/receipts/${encodeURIComponent(receiptId)}`, {}, auth)).payload, "ledger");
  if (receipt?.receiptId !== receiptId || receipt?.workspaceId !== workspaceId || receipt?.type !== "billing.workspace_purchased.v1" ||
    receipt?.status !== "completed" || receipt?.fulfillment?.runtimeId !== undefined || receipt?.fulfillment?.workspaceApiKeyId !== undefined ||
    String(receipt?.fulfillment?.computeAllocationId || "") !== String(launch.computeAllocationId || "") ||
    String(receipt?.fulfillment?.storageId || "") !== String(launch.storageId || "") ||
    String(receipt?.fulfillment?.attachmentId || "") !== String(launch.attachmentId || "")) {
    throw new Error("Ledger receipt readback is invalid");
  }
  return { launch, workspace, current, runtime, receipt };
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

async function readAllBillingReceipts(http, auth) {
  const receipts = [];
  const cursors = new Set();
  let cursor = "";
  for (;;) {
    const query = cursor ? `?limit=100&cursor=${encodeURIComponent(cursor)}` : "?limit=100";
    const page = sourceData((await http.json(`/api/billing/receipts${query}`, {}, auth)).payload, "ledger");
    if (!Array.isArray(page?.receipts) || typeof page.hasMore !== "boolean") {
      throw new Error("local qualification receipt inventory is invalid");
    }
    receipts.push(...page.receipts);
    if (!page.hasMore) break;
    const nextCursor = String(page.nextCursor || "");
    if (!nextCursor || cursors.has(nextCursor)) throw new Error("local qualification receipt inventory is invalid");
    cursors.add(nextCursor);
    cursor = nextCursor;
  }
  return receipts;
}

export function validateLocalJ1AccountingReadback(input) {
  const {
    operationId, workspaceId, receiptId, runtimeId, keyId, keyName, sub2apiUserId, debitCode, amountUsdMicros,
    beforeMicros, afterMicros, baselineKeys, baselineReceipts, keys, receipts, key, keyUsage, usage, history, debit, evidence
  } = input;
  if (![baselineKeys, baselineReceipts, keys, receipts].every(Array.isArray) || !Array.isArray(history?.items) ||
    !String(keyName || "").trim()) {
    throw new Error("local qualification accounting inventory is invalid");
  }
  // The installation creates the Workspace's reserved Key, so both its identity
  // and its exact name must be new to this qualification run.
  if (baselineKeys.some((candidate) => String(candidate?.id || "") === keyId || candidate?.name === keyName)) {
    throw new Error("local qualification exact workspace key predates this operation");
  }
  if (baselineReceipts.some((candidate) => candidate?.type === "billing.workspace_purchased.v1" &&
    (candidate?.receiptId === receiptId || candidate?.workspaceId === workspaceId))) {
    throw new Error("local qualification exact purchase receipt predates this operation");
  }

  const exactKeys = keys.filter((candidate) => String(candidate?.id || "") === keyId);
  if (exactKeys.length !== 1 || exactKeys[0]?.name !== keyName || key?.id !== keyId || key?.kind !== "workspace" || key?.status !== "active" ||
    exactKeys[0]?.kind !== "workspace" || exactKeys[0]?.status !== "active") {
    throw new Error("local qualification exact workspace key cardinality is invalid");
  }
  const workspacePurchaseReceipts = receipts.filter((candidate) =>
    candidate?.type === "billing.workspace_purchased.v1" && candidate?.workspaceId === workspaceId);
  if (workspacePurchaseReceipts.length !== 1 || workspacePurchaseReceipts[0]?.receiptId !== receiptId) {
    throw new Error("local qualification exact purchase receipt cardinality is invalid");
  }
  if (debit?.count !== 1 || debit?.code !== debitCode || String(debit?.userId) !== sub2apiUserId || String(debit?.amountUsdMicros) !== amountUsdMicros) {
    throw new Error("local qualification exact debit cardinality is invalid");
  }
  if (typeof usage?.totalRequests !== "number" || typeof keyUsage?.totalRequests !== "number" ||
    !/^\d+$/.test(afterMicros) ||
    evidence?.launch?.operationId !== operationId || evidence?.launch?.workspaceId !== workspaceId || evidence?.launch?.receiptId !== receiptId ||
    evidence?.current?.status !== "ready" || !String(evidence?.current?.entryUrl || "").trim() ||
    evidence?.workspace?.url !== evidence?.current?.entryUrl ||
    evidence?.runtime?.runtimeId !== runtimeId || evidence?.runtime?.currentApplication?.operationId !== evidence?.current?.operationId ||
    evidence?.receipt?.receiptId !== receiptId || evidence?.receipt?.workspaceId !== workspaceId ||
    evidence?.receipt?.chargeReference !== debitCode || String(evidence?.receipt?.totalUsdMicros) !== amountUsdMicros ||
    evidence?.receipt?.fulfillment?.runtimeId !== undefined || evidence?.receipt?.fulfillment?.workspaceApiKeyId !== undefined ||
    String(evidence?.receipt?.fulfillment?.computeAllocationId || "") !== String(evidence?.launch?.computeAllocationId || "") ||
    String(evidence?.receipt?.fulfillment?.storageId || "") !== String(evidence?.launch?.storageId || "") ||
    String(evidence?.receipt?.fulfillment?.attachmentId || "") !== String(evidence?.launch?.attachmentId || "")) {
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
    launchKey, operationId, workspaceId, workspaceName, receiptBase, readDebit, readRuntime, cleanup, wait,
    onStage = () => {}
  } = input;
  let cleanupEvidence;
  let cleanupScope = {};
  let launchSubmitted = false;
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
    auth = await login(http, qualificationEmail, qualificationPassword, provision.accountId);
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
    const [baselineKeys, baselineReceipts] = await Promise.all([
      readAllGatewayKeys(http, auth),
      readAllBillingReceipts(http, auth)
    ]);
    onStage("pricing_preview");
    const pricing = (await http.json("/api/pricing/preview", {
      method: "POST", body: { resourceType: "workspace", packageId: "basic" }
    }, auth)).payload;
    const amountUsdMicros = String(pricing?.totalChargeUsdMicros || "");
    if (pricing?.resourceType !== "workspace" || pricing?.packageId !== "basic" || pricing?.currency !== "USD" ||
      !/^[1-9][0-9]*$/.test(amountUsdMicros) || BigInt(beforeMicros) < BigInt(amountUsdMicros)) {
      throw new Error("qualification quote is invalid or wallet is insufficient");
    }
    onStage("workspace_launch");
    launchSubmitted = true;
    const initial = (await http.json("/api/workspace-launches", {
      method: "POST", headers: { "idempotency-key": launchKey },
      body: { name: workspaceName, packageId: "basic", autoRenew: false }
    }, auth, [202])).payload;
    if (initial?.operationId !== operationId || initial?.workspaceId !== workspaceId) throw new Error("deterministic launch identity is invalid");
    cleanupScope.workspaceId = workspaceId;
    const launch = await waitForLaunch(http, operationId, auth, wait);
    const receiptId = String(launch?.receiptId || "");
    if (!receiptId) throw new Error("terminal launch receipt identity is missing");
    onStage("application_installation");
    const installation = await waitForWorkspaceApplicationInstallation(http, auth, { workspaceId, operationId, wait });
    const workspaceKey = resolveWorkspaceKey(await readAllGatewayKeys(http, auth), workspaceId);
    onStage("terminal_readback");
    const evidence = await readWorkspaceEvidence(http, auth, operationId, workspaceId, receiptId);
    if (localApplicationEntryPort(evidence.current.entryUrl) === 0) throw new Error("Workspace application entry is invalid");
    const runtimeImage = await readRuntime({ accountId: provision.accountId, workspaceId, runtimeId: evidence.runtime.runtimeId });
    onStage("workspace_open");
    const opened = await fetchApplicationEntry(evidence.current.entryUrl);
    if (!opened.response.ok || !opened.text.includes("OPL Workspace READY")) throw new Error("Workspace Runtime open failed");

    onStage("accounting_readback");
    const [usage, walletAfter, keys, historyPage, receipts, debit] = await Promise.all([
      http.json("/api/gateway/usage-summary?period=month", {}, auth).then((result) => sourceData(result.payload, "sub2api")),
      http.json("/api/gateway/wallet", {}, auth).then((result) => sourceData(result.payload, "sub2api")),
      readAllGatewayKeys(http, auth),
      http.json("/api/gateway/balance-history?page=1&pageSize=20", {}, auth).then((result) => sourceData(result.payload, "sub2api")),
      readAllBillingReceipts(http, auth),
      readDebit({ accountId: provision.accountId, sub2apiUserId, code: evidence.receipt.chargeReference, amountUsdMicros })
    ]);
    const keyId = workspaceKey.id;
    const key = sourceData((await http.json(`/api/gateway/keys/${encodeURIComponent(keyId)}`, {}, auth)).payload, "sub2api");
    const keyUsage = sourceData((await http.json(`/api/gateway/keys/${encodeURIComponent(keyId)}/usage-summary?period=month`, {}, auth)).payload, "sub2api");
    const afterMicros = String(walletAfter?.usdMicros || "");
    const accounting = validateLocalJ1AccountingReadback({
      operationId, workspaceId, receiptId, runtimeId: evidence.runtime.runtimeId, keyId, keyName: workspaceKey.name, sub2apiUserId,
      debitCode: evidence.receipt.chargeReference, amountUsdMicros, beforeMicros, afterMicros,
      baselineKeys, baselineReceipts, keys, receipts, key, keyUsage, usage, history: historyPage, debit, evidence
    });

    const receiptCandidate = {
      ...receiptBase,
      images: {
        ...receiptBase.images,
        workspace: { ...receiptBase.images.workspace, runningDigest: runtimeImage.runningDigest }
      },
      identities: {
        accountId: provision.accountId, sub2apiUserId, launchOperationId: operationId, workspaceId,
        runtimeId: evidence.runtime.runtimeId, keyId, debitCode: debit.code, purchaseReceiptId: receiptId
      },
      debit: { count: 1, accountId: provision.accountId, operationId, workspaceId, code: debit.code, userId: sub2apiUserId, amountUsdMicros },
      wallet: { beforeUsdMicros: beforeMicros, afterUsdMicros: afterMicros, exactDeltaObserved: accounting.walletExactDeltaObserved },
      receipt: {
        count: 1, id: receiptId, accountId: provision.accountId, operationId, workspaceId,
        provisioningMode: "resource_only", computeAllocationId: String(evidence.receipt.fulfillment?.computeAllocationId || ""),
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
      mutationCounts: { accountProvisionPosts: 1, workspaceLaunchPosts: 1, workspaceDeleteRequests: 0, refundPosts: 0 },
      refund: { count: 0 },
      usage: { source: "sub2api", status: "available", totalRequests: usage.totalRequests }
    };
    onStage("receipt_validation");
    validateLocalQualificationReceipt({ ...receiptCandidate, residuals: { containers: 0, volumes: 0, networks: 0 } });
    onStage("qualification_cleanup");
    cleanupEvidence = await cleanup({ accountId: provision.accountId, workspaceId });
    const receipt = validateLocalQualificationReceipt({ ...receiptCandidate, residuals: cleanupEvidence });
    return { receipt, auth, provision, launch, evidence, cleanupEvidence };
  } finally {
    if (cleanupEvidence === undefined) {
      let recoveryAuthority = null;
      if (launchSubmitted && auth) {
        try {
          recoveryAuthority = await collectLocalJ1RecoveryAuthority({ http, auth, operationId, launchSubmitted });
        } catch {
          // The preserved service and PostgreSQL state remain the recovery authority when readback is temporarily unavailable.
        }
      }
      await cleanup({ ...cleanupScope, failed: true, launchSubmitted, recoveryAuthority });
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

// Delete retains the Workspace-reserved Gateway Key: after the Workspace is
// gone, the authority still owns and meters exactly the Key this installation
// created, unchanged, and no other Gateway Key was removed.
//
// The authority is the owner of that fact, and its own key record carries the
// identity the authority meters (id, name, user, group, status) without Control
// Plane's `kind` projection, so retention is proven by the exact before/after
// record of the reserved Key and an unchanged key inventory.
export function retainedWorkspaceKey(before, after, workspaceId, keyId) {
  const exact = (keys) => (keys || []).filter((candidate) => String(candidate?.id || "") === String(keyId));
  const beforeKey = exact(before);
  const afterKey = exact(after);
  const name = workspaceReservedKeyName(workspaceId);
  const record = (key) => [String(key?.id || ""), key?.name, String(key?.userId ?? ""), String(key?.groupId ?? ""), key?.status].join("\u0000");
  const inventory = (keys) => (keys || []).map((candidate) => String(candidate?.id ?? "")).sort().join(",");
  return beforeKey.length === 1 && afterKey.length === 1 &&
    beforeKey[0].name === name && afterKey[0].name === name && afterKey[0].status === "active" &&
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
  let operationId = `workspace-launch-${stableID(accountId, launchKey).slice(0, 18)}`;
  let workspaceId = `ws-${stableID("workspace-launch-v2", accountId, operationId).slice(0, 18)}`;
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
      operationId = `workspace-launch-${stableID(accountId, launchKey).slice(0, 18)}`;
      workspaceId = `ws-${stableID("workspace-launch-v2", accountId, operationId).slice(0, 18)}`;
    }
    composeEnvironment = qualificationComposeEnvironment(process.env, envEntries);
    const envFileEntries = qualificationEnvFileEntries(envEntries, options);
    await writeFile(envFile, `${envFileEntries.map(([key, value]) => `${key}=${value}`).join("\n")}\n`, { mode: 0o600 });
    await compose(["config", "--quiet"]);

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
        operationId,
        workspaceId,
        workspaceName: `Local qualification ${suffix}`,
        wait: (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds)),
        onStage: (name) => { stage = name; },
        readRuntime: async ({ accountId: exactAccountId, workspaceId: exactWorkspaceId }) => {
          const runtime = await runtimeImageReadback(exactAccountId, exactWorkspaceId, workspaceImage, workspaceInspection.Id);
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
            const artifact = scope?.recoveryAuthority
              ? createLocalJ1RecoveryArtifact({ ...common, authority })
              : createLocalJ1RecoveryReadbackPendingArtifact({ ...common, operationId, workspaceId });
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
    auth = await login(http, adminEmail, adminPassword);
    const me = sourceData((await http.json("/api/auth/me", {}, auth)).payload, "sub2api");
    const sub2apiUserId = String(me?.sub2apiUserId || "");
    if (me?.accountId !== accountId || !/^[1-9][0-9]*$/.test(sub2apiUserId) || me?.status !== "active") {
      throw new Error("qualification authority identity binding is invalid");
    }
    const walletBefore = sourceData((await http.json("/api/gateway/wallet", {}, auth)).payload, "sub2api");
    const beforeMicros = String(walletBefore?.usdMicros || "");
    if (!/^[1-9][0-9]*$/.test(beforeMicros)) throw new Error("qualification wallet readback is invalid");

    stage = "workspace_launch";
    const pricing = (await http.json("/api/pricing/preview", {
      method: "POST", body: { resourceType: "workspace", packageId: "basic" }
    }, auth)).payload;
    const amountUsdMicros = String(pricing?.totalChargeUsdMicros || "");
    if (!/^[1-9][0-9]*$/.test(amountUsdMicros) || BigInt(beforeMicros) < BigInt(amountUsdMicros)) {
      throw new Error("qualification quote is invalid or wallet is insufficient");
    }
    const initial = (await http.json("/api/workspace-launches", {
      method: "POST", headers: { "idempotency-key": launchKey },
      body: { name: `Local qualification ${suffix}`, packageId: "basic", autoRenew: false }
    }, auth, [202])).payload;
    if (initial?.operationId !== operationId || initial?.workspaceId !== workspaceId) throw new Error("deterministic launch identity is invalid");
    const launch = await waitForLaunch(http, operationId, auth);
    const receiptId = String(launch.receiptId || "");
    if (!receiptId) throw new Error("terminal launch receipt identity is missing");

    // The resource Launch carries no Runtime of its own: the owner commits the
    // default OPL application installation beside it. Wait for the owner's own
    // ready readback, then bind the Workspace Key the installation created.
    stage = "application_installation";
    const installation = await waitForWorkspaceApplicationInstallation(http, auth, { workspaceId, operationId });
    const workspaceKey = resolveWorkspaceKey(await readAllGatewayKeys(http, auth), workspaceId);
    const keyId = workspaceKey.id;
    stage = "terminal_readback";
    const evidence = await readWorkspaceEvidence(http, auth, operationId, workspaceId, receiptId);
    if (localApplicationEntryPort(evidence.current.entryUrl) === 0) throw new Error("Workspace application entry is invalid");
    const opened = await fetchApplicationEntry(evidence.current.entryUrl);
    if (!opened.response.ok || !opened.text.includes("OPL Workspace READY")) throw new Error("Workspace Runtime open failed");
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
    const receiptsPage = sourceData((await http.json("/api/billing/receipts?limit=50", {}, auth)).payload, "ledger");
    const receipts = (receiptsPage?.receipts || []).filter((candidate) => candidate?.type === "billing.workspace_purchased.v1");
    const receipt = evidence.receipt;
    if (receipts.length !== 1 || receipt.chargeReference !== debit.code || String(receipt.totalUsdMicros) !== amountUsdMicros ||
      receipt.fulfillment?.runtimeId !== undefined || receipt.fulfillment?.workspaceApiKeyId !== undefined ||
      String(receipt.fulfillment?.computeAllocationId || "") !== String(launch.computeAllocationId || "") ||
      String(receipt.fulfillment?.storageId || "") !== String(launch.storageId || "") ||
      String(receipt.fulfillment?.attachmentId || "") !== String(launch.attachmentId || "")) {
      throw new Error("Ledger receipt exact binding is invalid");
    }

    stage = "restart_continuity";
    await compose(["restart", "control-plane", "fabric", "ledger"]);
    await waitForCompose(compose);
    const restartedAuth = await login(http, adminEmail, adminPassword);
    const afterRestart = await readWorkspaceEvidence(http, restartedAuth, operationId, workspaceId, receiptId);
    const restart = {
      performed: true,
      operationStable: afterRestart.launch.operationId === launch.operationId,
      workspaceStable: afterRestart.workspace.id === evidence.workspace.id,
      runtimeStable: afterRestart.runtime.runtimeId === evidence.runtime.runtimeId,
      receiptStable: afterRestart.receipt.receiptId === receipt.receiptId
    };
    if (Object.values(restart).some((value) => value !== true)) throw new Error("restart continuity changed an exact identity");

    stage = "owner_delete";
    const expectedDeleteOperationId = `workspace-delete-${stableID("workspace.delete.v2", workspaceId).slice(0, 18)}`;
    const deletion = await continueWorkspaceDelete(http, `/api/workspaces/${encodeURIComponent(workspaceId)}`, {
      method: "DELETE", headers: { "idempotency-key": `local-qualification-delete:${workspaceId}` }, body: {}
    }, restartedAuth, {
      operationId: expectedDeleteOperationId, workspaceId,
      onPending: (pending) => { ownerDeletePending = pending; }
    });
    // The purchase is resource-only: its v2 Delete binds the Launch and the
    // Launch Receipt, never the application installation's Runtime or Workspace
    // Key. Delete performs no Gateway or wallet mutation, so the terminal
    // response reports both identities as empty and every owned absence it
    // confirmed.
    if (deletion?.status !== "deleted" || deletion?.accountId !== accountId || String(deletion?.sub2apiUserId) !== String(sub2apiUserId) ||
      deletion?.launchOperationId !== operationId || deletion?.operationId !== expectedDeleteOperationId || deletion?.launchReceiptId !== receiptId ||
      deletion?.workspaceId !== workspaceId || String(deletion?.runtimeId || "") !== "" || String(deletion?.workspaceApiKeyId ?? "") !== "0" ||
      deletion?.keyStatus !== undefined || !String(deletion?.deletionReceiptId || "").trim() ||
      deletion?.runtimeStatus !== "absent" || deletion?.secretStatus !== "absent") {
      throw new Error("owner-authorized Workspace DELETE terminal evidence is invalid");
    }
    const afterDeletePage = sourceData((await http.json("/api/workspaces?page=1&pageSize=20", {}, restartedAuth)).payload, "control-plane");
    const workspaceAbsent = !(afterDeletePage?.items || []).some((candidate) => candidate?.id === workspaceId);
    const runtimeAfterDelete = await http.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-status`, {}, restartedAuth);
    const runtimeAbsent = runtimeAfterDelete.response.status === 404;
    residuals = await residualCounts(accountId, workspaceId);
    const authorityAfterOwnerDelete = await authorityState(authorityPort, authorityToken);
    // The Workspace-reserved Gateway Key is the one object Delete must not
    // remove: the platform retains it, so the authority still owns and meters
    // exactly the Key this installation created after the Workspace is gone.
    const workspaceKeyRetained = retainedWorkspaceKey(authorityBeforeDelete?.keys || [], authorityAfterOwnerDelete?.keys || [], workspaceId, keyId);
    // Fabric's injected Secret is a provider secret-root directory named from
    // the Workspace identity; that exact ref must be gone after Delete.
    const fabricSecretAbsent = deletion.secretStatus === "absent" && !existsSync(join(fabricSecretRoot, workspaceGatewaySecretRef(workspaceId)));
    if (!workspaceAbsent || !runtimeAbsent || !workspaceKeyRetained || !fabricSecretAbsent || Object.values(residuals).some((count) => count !== 0)) {
      throw new Error("owner DELETE did not prove Workspace, Runtime and Secret cleanup with Gateway Key retention");
    }
    const deletionReceipt = sourceData((await http.json(`/api/billing/receipts/${encodeURIComponent(deletion.deletionReceiptId)}`, {}, restartedAuth)).payload, "ledger");
    const deletionResourceStatus = deletionReceipt?.resourceStatus || {};
    if (deletionReceipt?.receiptId !== deletion.deletionReceiptId || deletionReceipt?.type !== "workspace.deleted.v1" ||
      deletionReceipt?.status !== "completed" ||
      deletionReceipt?.operationId !== expectedDeleteOperationId || deletionReceipt?.workspaceId !== workspaceId ||
      deletionReceipt?.resourceType !== "workspace" || deletionReceipt?.resourceId !== workspaceId ||
      deletionReceipt?.launchReceiptId !== receiptId ||
      !["runtimeStatus", "gatewaySecretStatus", "attachmentStatus", "storageStatus", "computeStatus", "workspaceStatus"]
        .every((key) => deletionResourceStatus[key] === "absent") ||
      !workspaceDeleteReceiptStagesMatch(deletionReceipt?.stageEvidence)) {
      throw new Error("Ledger deletion Receipt binding is invalid");
    }
    let afterDeleteMicros = afterMicros;
    let refundEvidence = { count: 0 };
    if (options.authorityMode === "fixture") {
      const adjustmentsAfterDelete = authorityAfterOwnerDelete?.adjustments || [];
      const debitAfterDelete = adjustmentsAfterDelete.filter((candidate) => candidate?.kind === "debit" && candidate?.code === debit.code);
      const refundsAfterDelete = adjustmentsAfterDelete.filter((candidate) => candidate?.kind === "refund");
      // Control Plane's deletion closeout refunds the unused part of the paid
      // period as its own wallet operation, so exactly one refund adjustment and
      // exactly one business_refund receipt must exist, and both must bind to
      // this Delete operation: the wallet operation id is derived from the
      // Delete operation id, the refunded order is the charge the platform
      // debited, and the amount is the one the Ledger receipt records.
      const refundWalletOperationId = `wallet-adjustment-delete-${stableID(expectedDeleteOperationId).slice(0, 24)}`;
      const refundReceipts = (await readAllBillingReceipts(http, restartedAuth)).filter((candidate) =>
        candidate?.type === "gateway.wallet_adjustment.v1" && candidate?.kind === "business_refund");
      if (debitAfterDelete.length !== 1 || authorityAfterOwnerDelete?.writeCounts?.debits !== 1 ||
        refundsAfterDelete.length !== 1 || refundReceipts.length !== 1 || authorityAfterOwnerDelete?.writeCounts?.refunds !== 1) {
        throw new Error("qualification authority Delete accounting evidence is invalid");
      }
      const refundAdjustment = refundsAfterDelete[0];
      const refundReceipt = refundReceipts[0];
      if (refundReceipt?.operationId !== refundWalletOperationId || refundReceipt?.workspaceId !== workspaceId ||
        refundReceipt?.resourceId !== workspaceId || refundReceipt?.relatedOperationId !== operationId ||
        refundReceipt?.status !== "completed" || refundReceipt?.periodStart !== receipt.periodStart || refundReceipt?.paidThrough !== receipt.paidThrough ||
        String(refundReceipt?.refundUsdMicros || "") !== String(refundAdjustment?.valueUsdMicros || "") ||
        String(refundAdjustment?.amountUsdMicros || "") !== String(refundAdjustment?.valueUsdMicros || "") ||
        String(refundAdjustment?.userId || "") !== String(sub2apiUserId) || refundAdjustment?.status !== "used" ||
        !String(refundAdjustment?.code || "").startsWith("opl:") || !String(refundAdjustment?.usedAt || "").trim() ||
        !(BigInt(refundReceipt.refundUsdMicros) > 0n && BigInt(refundReceipt.refundUsdMicros) <= BigInt(amountUsdMicros))) {
        throw new Error("platform refund for the deleted Workspace is not exactly bound");
      }
      refundEvidence = {
        count: 1, walletOperationId: refundWalletOperationId, receiptId: String(refundReceipt.receiptId || ""),
        amountUsdMicros: String(refundReceipt.refundUsdMicros || ""), code: String(refundAdjustment.code || ""),
        userId: String(refundAdjustment.userId || ""), relatedOperationId: String(refundReceipt.relatedOperationId || "")
      };
      afterDeleteMicros = String(authorityAfterOwnerDelete?.wallet?.usdMicros || "");
      if (!/^\d+$/.test(afterDeleteMicros) || BigInt(afterDeleteMicros) !== BigInt(afterMicros) + BigInt(refundEvidence.amountUsdMicros)) {
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
        accountId, sub2apiUserId, launchOperationId: operationId, deleteOperationId: String(deletion.operationId || ""),
        deletionReceiptId: String(deletion.deletionReceiptId || ""), workspaceId,
        runtimeId: evidence.runtime.runtimeId, keyId, debitCode: debit.code,
        purchaseReceiptId: receiptId
      },
      debit: { count: debitCount, accountId, operationId, workspaceId, code: debit.code, userId: String(debit.userId), amountUsdMicros },
      wallet: { beforeUsdMicros: beforeMicros, afterUsdMicros: afterMicros, afterDeleteUsdMicros: afterDeleteMicros },
      receipt: {
        count: 1, id: receipt.receiptId, accountId, operationId, workspaceId: receipt.workspaceId,
        provisioningMode: "resource_only", computeAllocationId: String(receipt.fulfillment.computeAllocationId || ""),
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
        ownerAuthorized: true, accountId, operationId: String(deletion.operationId || ""), deletionReceiptId: String(deletion.deletionReceiptId || ""), workspaceId,
        runtimeId: String(deletion.runtimeId || ""), workspaceApiKeyId: String(deletion.workspaceApiKeyId ?? ""),
        workspaceAbsent, runtimeAbsent, workspaceKeyRetained, fabricSecretAbsent
      },
      deletionReceipt: {
        count: 1, id: deletionReceipt.receiptId, type: deletionReceipt.type,
        operationId: deletionReceipt.operationId, workspaceId: deletionReceipt.workspaceId,
        launchReceiptId: String(deletionReceipt.launchReceiptId || ""),
        resourceType: deletionReceipt.resourceType, resourceId: deletionReceipt.resourceId,
        resourceStatus: deletionReceipt.resourceStatus, stageEvidence: deletionReceipt.stageEvidence
      },
      residuals,
      authorityWriteCounts: authorityAfterOwnerDelete?.writeCounts,
      mutationCounts: { workspaceLaunchPosts: 1, workspaceDeleteRequests: 1, refundPosts: 0 },
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
      const observed = await residualCounts(accountId, workspaceId).catch(() => null);
      if (stop.code !== 0 || !observed || down.code !== 0) failure ||= new Error("local qualification teardown was not confirmed");
      if (observed) residuals = observed;
    }
    if (!preserveRecoveryAuthority) for (const tag of builtTags) await runProcess("docker", ["image", "rm", tag], { allowFailure: true });
    if (registryContainer && !preserveRecoveryAuthority) {
      const removed = await runProcess("docker", ["rm", "-f", registryContainer], { allowFailure: true });
      if (removed.code !== 0) failure ||= new Error("local source registry cleanup was not confirmed");
    }
    if (!preserveRecoveryAuthority) {
      // The PostgreSQL image chowns its bind-mounted data directory to the
      // in-container postgres uid, so a non-root Linux qualifier is denied the
      // scan of that single directory. That hygiene failure must not mask the
      // stage failure this run exists to record: the scratch removal is
      // attempted, and only a genuine non-EACCES removal error keeps the run
      // failed. The denial is reported instead of silently dropped.
      try {
        await rm(tempRoot, { recursive: true, force: true });
      } catch (error) {
        if ((error as NodeJS.ErrnoException)?.code === "EACCES") {
          console.error(`local qualification scratch cleanup was not confirmed: ${String((error as Error)?.message || error)}`);
        } else {
          failure ||= error;
        }
      }
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
      errorCode: "local_workspace_qualification_failed",
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
      deferred: [...deferredCloudGates]
    };
    await writeJSONAtomic(options.receiptPath, notReady);
    throw failure;
  }
  await writeJSONAtomic(options.receiptPath, finalReceipt);
  return finalReceipt;
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
