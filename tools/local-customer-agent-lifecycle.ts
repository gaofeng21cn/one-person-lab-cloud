/*
 * The customer agent lifecycle evidence machine.
 *
 * One run drives the whole customer loop against an owner installation and
 * leaves a machine-readable receipt behind:
 *
 *   upload an agent package -> the platform builds it into an OCI image ->
 *   the platform registers that image as a capability -> the customer buys a
 *   Workspace on that capability -> the published entry answers with the
 *   customer's own bytes -> the customer deletes the Workspace and the owners
 *   confirm the resources are gone.
 *
 * The tool observes. It never changes product behaviour, and it never invents a
 * step the product did not perform: when a step does not happen (today the
 * automatic build trigger and the automatic registration trigger are separate
 * pieces of work), the receipt names the step it stopped at instead of
 * reporting a pass.
 *
 * The acceptance criterion has three layers, and the receipt states which of
 * them actually held:
 *
 *   A. structure, always asserted and never string-based:
 *      the Workspace owner reports a capability version and no runtime version,
 *      so the purchase is on the customer's agent branch and not on the
 *      platform's own application branch; and the capability version's
 *      artifact is the same repository@digest as the build output, which is
 *      the same digest the registry reports for that manifest.
 *
 *   B. content, derived per run and never a constant: the run writes a fresh
 *      random nonce into a probe file inside the package it uploads, and the
 *      published entry has to answer with that nonce. The bytes that answered
 *      therefore come from this upload.
 *
 *   C. degradation, always explicit: a package whose runtime does not echo the
 *      probe file cannot support layer B. The receipt then carries the lower
 *      evidence level and the reason, and the run is never reported as passed
 *      on layer A alone.
 *
 * The probe convention is a requirement this harness places on the package
 * under test, not a product requirement: any package that echoes its probe
 * file can be qualified with this tool.
 */

import { createHash, randomUUID } from "node:crypto";
import { readFile, readdir, stat, writeFile } from "node:fs/promises";
import { request as httpsRequest } from "node:https";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  cloudIdentityLogin,
  createHTTP,
  deleteWorkspaceOnOwnerRoute,
  localV2QualificationCA,
  localV2QualificationOrigin,
  parseInstallationEnvFile,
  readWorkspaceDeletionEvidence,
  residualCounts,
  waitForWorkspaceApplicationInstallation,
} from "./local-workspace-qualification.ts";

export const CUSTOMER_AGENT_LIFECYCLE_RECEIPT_KIND = "opl.customer-agent-lifecycle-receipt.v1";
export const EVIDENCE_LEVEL_CONTENT_PROVENANCE = "content-provenance";
export const EVIDENCE_LEVEL_ARTIFACT_IDENTITY = "artifact-identity";
export const CUSTOMER_AGENT_LIFECYCLE_MODES = Object.freeze(["local-v2"]);

// The default location of the probe file inside the uploaded archive. A caller
// that qualifies a package with a different layout passes its own path; the
// convention belongs to the package under test.
export const DEFAULT_PROBE_PATH = "provenance/probe.txt";
export const DEFAULT_MANIFEST_NAME = "manifest.json";

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const artifactPattern = /^[^\s@]+@sha256:[0-9a-f]{64}$/;
const namePattern = /^[A-Za-z0-9._-]{1,128}$/;
const sourceShaPattern = /^[0-9a-f]{40}$/;

const DEFAULT_BUILD_TIMEOUT_MS = 15 * 60 * 1000;
const DEFAULT_REGISTRATION_TIMEOUT_MS = 15 * 60 * 1000;
const DEFAULT_POLL_INTERVAL_MS = 3000;
const DEFAULT_POLL_MAX_INTERVAL_MS = 15000;

export function namedCustomerAgentError(code, detail) {
  return Object.assign(new Error(`${code}: ${detail}`), { code, detail });
}

/*
 * Argument parsing.
 *
 * The tool names no agent: the package under test, the probe path and the
 * evidence level the caller expects are the caller's own facts.
 */
export function parseCustomerAgentLifecycleArgs(args = process.argv.slice(2)) {
  const values = new Map();
  const known = [
    "--source-sha", "--installation-env", "--package-dir", "--probe-path", "--manifest-name",
    "--package-name", "--package-version", "--expected-evidence-level", "--receipt",
    "--registry-url", "--registry-token", "--project", "--build-timeout-ms",
    "--registration-timeout-ms", "--mode",
  ];
  for (let index = 0; index < args.length; index += 1) {
    const token = args[index];
    if (!known.includes(token)) throw new Error(`unknown customer agent lifecycle argument: ${token}`);
    const value = args[index + 1];
    if (!value || value.startsWith("--") || values.has(token)) {
      throw new Error(`customer agent lifecycle argument ${token} requires one value`);
    }
    values.set(token, value);
    index += 1;
  }
  const mode = String(values.get("--mode") || "local-v2").trim();
  if (!CUSTOMER_AGENT_LIFECYCLE_MODES.includes(mode)) {
    throw new Error(`customer agent lifecycle mode must be one of ${CUSTOMER_AGENT_LIFECYCLE_MODES.join(", ")}`);
  }
  const sourceSha = String(values.get("--source-sha") || "").trim();
  if (!sourceShaPattern.test(sourceSha)) {
    throw new Error("--source-sha must be the exact 40-character product commit");
  }
  const installationEnv = String(values.get("--installation-env") || "").trim();
  if (!installationEnv) throw new Error("--installation-env is required");
  const packageDir = String(values.get("--package-dir") || "").trim();
  if (!packageDir) throw new Error("--package-dir is required");
  const expectedEvidenceLevel = String(values.get("--expected-evidence-level") || "").trim();
  if (expectedEvidenceLevel &&
      ![EVIDENCE_LEVEL_CONTENT_PROVENANCE, EVIDENCE_LEVEL_ARTIFACT_IDENTITY].includes(expectedEvidenceLevel)) {
    throw new Error(`--expected-evidence-level must be one of ${EVIDENCE_LEVEL_CONTENT_PROVENANCE}, ${EVIDENCE_LEVEL_ARTIFACT_IDENTITY}`);
  }
  const positive = (name, fallback) => {
    const raw = values.get(name);
    if (raw === undefined) return fallback;
    const parsed = Number(raw);
    if (!Number.isFinite(parsed) || parsed < 0) throw new Error(`${name} must be a non-negative number of milliseconds`);
    return parsed;
  };
  return {
    mode,
    sourceSha,
    installationEnv,
    packageDir,
    probePath: String(values.get("--probe-path") || DEFAULT_PROBE_PATH).trim(),
    manifestName: String(values.get("--manifest-name") || DEFAULT_MANIFEST_NAME).trim(),
    packageName: String(values.get("--package-name") || "").trim(),
    packageVersion: String(values.get("--package-version") || "").trim(),
    expectedEvidenceLevel,
    receiptPath: String(values.get("--receipt") || "").trim(),
    registryUrl: String(values.get("--registry-url") || "").trim(),
    registryToken: String(values.get("--registry-token") || "").trim(),
    project: String(values.get("--project") || "").trim(),
    buildTimeoutMs: positive("--build-timeout-ms", DEFAULT_BUILD_TIMEOUT_MS),
    registrationTimeoutMs: positive("--registration-timeout-ms", DEFAULT_REGISTRATION_TIMEOUT_MS),
  };
}

/*
 * A minimal ZIP writer.
 *
 * The Capability owner accepts one ZIP archive and reads it with a standard
 * reader, so the archive is written with stored entries and a fixed timestamp:
 * the bytes of a package never depend on when the run happened.
 */
const crc32Table = (() => {
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
    value = crc32Table[(value ^ byte) & 0xff] ^ (value >>> 8);
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
    throw namedCustomerAgentError("customer_agent_package_path_invalid", `package entry path is not a safe relative path: ${name}`);
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

/*
 * The uploaded archive.
 *
 * The package directory is mirrored into the archive and the probe file is
 * written into it, so the nonce travels with the bytes the platform builds.
 */
export async function buildCustomerAgentPackageArchive({ packageDir, probePath, probeContent, manifestName = DEFAULT_MANIFEST_NAME }) {
  const root = resolve(packageDir);
  const files = await collectPackageFiles(root);
  const probe = safeArchivePath(probePath);
  const entries = [];
  let manifest = null;
  for (const file of files) {
    if (file.name === probe) continue;
    if (file.name === manifestName) {
      manifest = file;
      continue;
    }
    entries.push(file);
  }
  if (!manifest) {
    throw namedCustomerAgentError("customer_agent_package_manifest_missing", `the package directory has no ${manifestName}`);
  }
  entries.push({ name: manifestName, data: manifest.data, mode: manifest.mode });
  entries.push({ name: probe, data: Buffer.from(`${probeContent}\n`, "utf8"), mode: 0o644 });
  entries.sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0));
  const bytes = zipArchive(entries);
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  return { bytes, digest, entries: entries.map((entry) => entry.name), manifest: JSON.parse(manifest.data.toString("utf8")) };
}

/*
 * Polling.
 *
 * Every wait has a ceiling and a backoff, and the receipt names the step that
 * ran out of budget instead of the run reporting a pass it did not earn.
 */
export function pollSchedule(attempts, baseIntervalMs = DEFAULT_POLL_INTERVAL_MS, maxIntervalMs = DEFAULT_POLL_MAX_INTERVAL_MS) {
  const delays = [];
  let interval = Math.max(1, baseIntervalMs);
  for (let attempt = 0; attempt < Math.max(0, attempts); attempt += 1) {
    delays.push(interval);
    interval = Math.min(Math.max(1, maxIntervalMs), interval * 2);
  }
  return delays;
}

/*
 * Layer A, structure.
 *
 * The owner readback says which application branch the purchase froze. A
 * capability version without a runtime version is the customer's agent; a
 * runtime version is the platform's own application.
 */
export function evaluateAgentBranchSelection({ capabilityVersionId, runtimeVersionId }) {
  const capability = String(capabilityVersionId || "").trim();
  const runtime = String(runtimeVersionId || "").trim();
  if (!capability) {
    return {
      pass: false,
      code: "customer_agent_agent_branch_not_selected",
      detail: "the Workspace owner reports no capability version, so the purchase is not on a customer agent branch",
      capabilityVersionId: capability,
      runtimeVersionId: runtime,
    };
  }
  if (runtime) {
    return {
      pass: false,
      code: "customer_agent_platform_branch_selected",
      detail: "the Workspace owner reports a runtime version, so the purchase is on the platform application branch",
      capabilityVersionId: capability,
      runtimeVersionId: runtime,
    };
  }
  return {
    pass: true,
    code: "",
    detail: "the Workspace owner reports a capability version and no runtime version",
    capabilityVersionId: capability,
    runtimeVersionId: runtime,
  };
}

/*
 * Layer A, artifact identity.
 *
 * The build output, the registry manifest and the registered capability
 * version have to name one and the same immutable image.
 */
export function evaluateArtifactIdentity({ buildArtifact, capabilityArtifact, registryManifestDigest }) {
  const build = String(buildArtifact || "").trim();
  const capability = String(capabilityArtifact || "").trim();
  const registry = String(registryManifestDigest || "").trim();
  if (!artifactPattern.test(build)) {
    return {
      pass: false,
      code: "customer_agent_build_artifact_invalid",
      detail: `the build output is not an immutable repository@digest: ${build}`,
      buildArtifact: build,
      capabilityArtifact: capability,
      registryManifestDigest: registry,
    };
  }
  if (!artifactPattern.test(capability)) {
    return {
      pass: false,
      code: "customer_agent_capability_artifact_invalid",
      detail: `the registered capability version does not carry an immutable repository@digest: ${capability}`,
      buildArtifact: build,
      capabilityArtifact: capability,
      registryManifestDigest: registry,
    };
  }
  const reasons = [];
  if (capability !== build) {
    reasons.push(`the registered capability artifact ${capability} is not the build output ${build}`);
  }
  const buildDigest = build.slice(build.lastIndexOf("@") + 1);
  if (!registry) {
    reasons.push("the registry manifest digest was not read back");
  } else if (registry !== buildDigest) {
    reasons.push(`the registry manifest digest ${registry} is not the build output digest ${buildDigest}`);
  }
  if (reasons.length > 0) {
    return {
      pass: false,
      code: "customer_agent_artifact_identity_mismatch",
      detail: reasons.join("; "),
      buildArtifact: build,
      capabilityArtifact: capability,
      registryManifestDigest: registry,
    };
  }
  return {
    pass: true,
    code: "",
    detail: "the build output, the registry manifest and the registered capability version name one image",
    buildArtifact: build,
    capabilityArtifact: capability,
    registryManifestDigest: registry,
  };
}

/*
 * Layer B, content.
 *
 * The nonce is minted by this run and written into the uploaded package, so
 * the entry that answers with it answered from this upload.
 */
export function evaluateContentProvenance({ nonce, entryText }) {
  const expected = String(nonce || "").trim();
  const body = String(entryText || "");
  const observed = Boolean(expected) && body.includes(expected);
  return {
    pass: observed,
    code: observed ? "" : "customer_agent_content_provenance_not_observed",
    detail: observed
      ? "the published entry answered with the nonce this run wrote into the uploaded package"
      : "the published entry did not answer with the nonce this run wrote into the uploaded package",
    nonceDigest: expected ? `sha256:${createHash("sha256").update(expected).digest("hex")}` : "",
    entryBytes: Buffer.byteLength(body, "utf8"),
  };
}

export function evaluateCustomerAgentCriteria({ branch, artifact, content }) {
  return {
    pass: Boolean(branch?.pass) && Boolean(artifact?.pass) && Boolean(content?.pass),
    structurePass: Boolean(branch?.pass) && Boolean(artifact?.pass),
    contentPass: Boolean(content?.pass),
  };
}

/*
 * The receipt.
 */
export function validateCustomerAgentLifecycleReceipt(receipt) {
  const errors = [];
  const value = receipt || {};
  if (value.schemaVersion !== 1) errors.push("schemaVersion must be 1");
  if (value.kind !== CUSTOMER_AGENT_LIFECYCLE_RECEIPT_KIND) errors.push("kind must be the customer agent lifecycle receipt kind");
  const statuses = ["passed", "degraded", "blocked", "failed"];
  if (!statuses.includes(String(value.status || ""))) errors.push(`status must be one of ${statuses.join(", ")}`);
  const levels = ["", EVIDENCE_LEVEL_CONTENT_PROVENANCE, EVIDENCE_LEVEL_ARTIFACT_IDENTITY];
  if (!levels.includes(String(value.evidenceLevel || ""))) errors.push(`evidenceLevel must be one of ${levels.join(", ")}`);
  const digests = value.digests || {};
  if (!digestPattern.test(String(digests.packageArchive || ""))) errors.push("digests.packageArchive must be a sha256 digest");
  if (!artifactPattern.test(String(digests.buildArtifact || ""))) errors.push("digests.buildArtifact must be a repository@digest");
  if (!digestPattern.test(String(digests.registryManifestDigest || ""))) errors.push("digests.registryManifestDigest must be a sha256 digest");
  if (!artifactPattern.test(String(digests.capabilityArtifact || ""))) errors.push("digests.capabilityArtifact must be a repository@digest");
  const criteria = value.criteria || {};
  if (typeof criteria.agentBranch?.pass !== "boolean") errors.push("criteria.agentBranch.pass must be a boolean");
  if (typeof criteria.artifactIdentity?.pass !== "boolean") errors.push("criteria.artifactIdentity.pass must be a boolean");
  if (typeof criteria.contentProvenance?.pass !== "boolean") errors.push("criteria.contentProvenance.pass must be a boolean");
  if (value.status === "passed") {
    if (value.evidenceLevel !== EVIDENCE_LEVEL_CONTENT_PROVENANCE) {
      errors.push("a passed run must carry the content provenance evidence level");
    }
    if (criteria.contentProvenance?.pass !== true) errors.push("a passed run must have observed the content provenance");
    if (criteria.agentBranch?.pass !== true) errors.push("a passed run must have observed the agent branch");
    if (criteria.artifactIdentity?.pass !== true) errors.push("a passed run must have observed the artifact identity");
    if (value.degradation) errors.push("a passed run must not carry a degradation");
  }
  if (value.status === "degraded") {
    if (value.evidenceLevel !== EVIDENCE_LEVEL_ARTIFACT_IDENTITY) {
      errors.push("a degraded run must carry the artifact identity evidence level");
    }
    if (!String(value.degradation?.code || "").trim()) errors.push("a degraded run must name the degradation code");
    if (!String(value.degradation?.detail || "").trim()) errors.push("a degraded run must name the degradation reason");
  }
  if (["blocked", "failed"].includes(String(value.status || ""))) {
    if (!String(value.blocked?.step || "").trim()) errors.push("a blocked run must name the step it stopped at");
    if (!String(value.blocked?.code || "").trim()) errors.push("a blocked run must name the code it stopped at");
  }
  return { valid: errors.length === 0, errors };
}

/*
 * The registry readback.
 *
 * The registry that holds the build output is the one the installation
 * declares, so the manifest is read from the same host the build pushed to.
 */
export function registryManifestUrl(repository, digest, registryUrl = "") {
  const reference = String(repository || "").trim();
  const target = String(digest || "").trim();
  if (!reference || !digestPattern.test(target)) {
    throw namedCustomerAgentError("customer_agent_registry_reference_invalid", `the artifact ${reference}@${target} is not an immutable reference`);
  }
  let base = String(registryUrl || "").trim();
  if (!base) {
    const host = reference.split("/")[0];
    base = /^(127\.0\.0\.1|localhost|\[::1\])(:|$)/.test(host) ? `http://${host}` : `https://${host}`;
  }
  base = base.replace(/\/+$/, "");
  return `${base}/v2/${reference.split("/").slice(1).join("/")}/manifests/${target}`;
}

export async function readRegistryManifestDigest({ repository, digest, registryUrl = "", token = "" }) {
  const url = registryManifestUrl(repository, digest, registryUrl);
  const headers = {
    accept: "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json",
    ...(token ? { authorization: `Bearer ${token}` } : {}),
  };
  const response = await fetch(url, { headers });
  const body = await response.text();
  const reported = response.headers.get("docker-content-digest") || "";
  return { url, status: response.status, digest: reported.trim(), bodyBytes: Buffer.byteLength(body, "utf8") };
}

/*
 * The published entry.
 *
 * The entry is served by the installation's own origin or by the anonymous
 * loopback publication the local provider gives a Workspace, so both transports
 * are here: one trusts the installation's certificate, the other is plain.
 */
export async function fetchCustomerAgentEntry(url, { caFile = "" } = {}) {
  const target = new URL(String(url || ""));
  if (target.protocol !== "https:") {
    const response = await fetch(target, { redirect: "manual" });
    return { status: response.status, text: await response.text() };
  }
  const ca = caFile ? await readFile(caFile) : undefined;
  return new Promise((resolvePromise, rejectPromise) => {
    const request = httpsRequest({
      hostname: target.hostname,
      port: target.port || 443,
      path: `${target.pathname}${target.search}`,
      method: "GET",
      ca,
      servername: target.hostname,
    }, (response) => {
      const chunks = [];
      response.on("data", (chunk) => chunks.push(chunk));
      response.on("end", () => {
        resolvePromise({ status: response.statusCode, text: Buffer.concat(chunks).toString("utf8") });
      });
    });
    request.on("error", rejectPromise);
    request.end();
  });
}

function artifactReference(payload) {
  const repository = String(payload?.artifact?.repository || "").trim();
  const digest = String(payload?.artifact?.digest || "").trim();
  return repository && digest ? `${repository}@${digest}` : "";
}

async function defaultWait(milliseconds) {
  await new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));
}

/*
 * The run.
 *
 * Every step records what was asked and what was answered, and the receipt
 * names the step the run stopped at when a step does not happen.
 */
export async function runCustomerAgentLifecycle(options, dependencies: any = {}) {
  const wait = dependencies.wait || defaultWait;
  const now = dependencies.now || (() => Date.now());
  const requests = [];
  const steps = [];
  const receipt: any = {
    schemaVersion: 1,
    kind: CUSTOMER_AGENT_LIFECYCLE_RECEIPT_KIND,
    status: "blocked",
    evidenceLevel: "",
    mode: String(options.mode || "local-v2"),
    startedAt: new Date(now()).toISOString(),
    completedAt: "",
    source: { sha: String(options.sourceSha || ""), installationEnv: String(options.installationEnv || "") },
    package: {},
    digests: { packageArchive: "", buildArtifact: "", buildArtifactDigest: "", registryManifestDigest: "", capabilityArtifact: "" },
    criteria: { agentBranch: {}, artifactIdentity: {}, contentProvenance: {} },
    degradation: null,
    blocked: null,
    steps,
    evidence: { requests, readbacks: {} },
  };
  const record = (step, status, detail) => {
    steps.push({ step, status, detail, observedAt: new Date(now()).toISOString() });
  };
  const note = (step, method, path, status) => {
    requests.push({ step, method, path, status });
  };
  const stop = (step, code, detail) => {
    receipt.blocked = { step, code, detail };
    receipt.status = "blocked";
    receipt.completedAt = new Date(now()).toISOString();
    record(step, "blocked", { code, detail });
    return receipt;
  };
  const fail = (step, code, detail) => {
    receipt.blocked = { step, code, detail };
    receipt.status = "failed";
    receipt.completedAt = new Date(now()).toISOString();
    record(step, "failed", { code, detail });
    return receipt;
  };

  let values: any = {};
  try {
    values = parseInstallationEnvFile(await readFile(options.installationEnv, "utf8"));
  } catch (error) {
    return stop("installation", "customer_agent_installation_unreadable", String(error?.message || error));
  }
  let origin = "";
  let caFile = "";
  try {
    origin = localV2QualificationOrigin(values);
    caFile = localV2QualificationCA(values);
  } catch (error) {
    return stop("installation", "customer_agent_installation_invalid", String(error?.message || error));
  }
  const username = String(values.OPL_QUALIFICATION_USER_EMAIL || "");
  const password = String(values.OPL_QUALIFICATION_USER_PASSWORD || "");
  if (!username || !password) {
    return stop("installation", "customer_agent_installation_credentials_missing", "the installation declares no customer credential");
  }
  const http = dependencies.http || createHTTP(origin, { caFile, mode: "local-v2" });

  // The package under test. The nonce is minted here, so it cannot be
  // predicted from anything the installation already holds.
  const nonce = randomUUID();
  let archive = null;
  try {
    archive = await buildCustomerAgentPackageArchive({
      packageDir: options.packageDir,
      probePath: options.probePath,
      probeContent: nonce,
      manifestName: options.manifestName,
    });
  } catch (error) {
    return stop("package", "customer_agent_package_unreadable", String(error?.message || error));
  }
  const packageName = String(options.packageName || archive.manifest?.name || "").trim();
  const versionLabel = String(options.packageVersion || archive.manifest?.version || "").trim();
  receipt.package = {
    name: packageName,
    versionLabel,
    archiveDigest: archive.digest,
    archiveBytes: archive.bytes.length,
    entries: archive.entries,
    probePath: String(options.probePath || DEFAULT_PROBE_PATH),
    probeNonceDigest: `sha256:${createHash("sha256").update(nonce).digest("hex")}`,
  };
  receipt.digests.packageArchive = archive.digest;
  record("package", "confirmed", { entries: archive.entries.length, digest: archive.digest });

  // The customer session.
  let auth = null;
  let tenantId = "";
  try {
    const session = await cloudIdentityLogin(http, origin, username, password,
      `customer-agent-login:${options.sourceSha}:${packageName || "package"}`, { wait });
    auth = { cookie: session.cookie, ownerCsrf: session.ownerCsrf, csrf: "" };
    tenantId = String(session.session.tenantId || "");
    receipt.evidence.readbacks.session = { actorId: session.session.actorId, tenantId };
  } catch (error) {
    return stop("login", "customer_agent_login_failed", String(error?.message || error));
  }
  record("login", "confirmed", { actorId: receipt.evidence.readbacks.session.actorId });

  // The customer's own registry binding. The Build owner pushes the build
  // output to the repository this binding names, so the repository is known
  // before the build runs and does not come from the registered capability
  // version the artifact identity is judged against.
  let buildRepository = "";
  let registryHost = "";
  try {
    const binding = (await http.json("/api/v2/auth/external-bindings", {}, auth)).payload;
    registryHost = String(binding?.registryHost || "").trim();
    const namespace = String(binding?.registryNamespace || "").trim();
    const repository = String(binding?.repository || "").trim();
    if (!registryHost || !namespace || !repository) {
      throw new Error("the customer holds no registry binding, so the build output has no repository");
    }
    buildRepository = `${registryHost}/${namespace}/${repository}`;
    receipt.evidence.readbacks.registryBinding = { registryHost, registryNamespace: namespace, repository, buildRepository };
  } catch (error) {
    return stop("registry_binding", "customer_agent_registry_binding_absent", String(error?.message || error));
  }
  record("registry_binding", "confirmed", { buildRepository });

  // The account the local provider holds this Tenant's resources under. The
  // provider profile the installation declares is the only place that name
  // exists.
  let accountId = "";
  try {
    const profile = JSON.parse(String(values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON || "{}"));
    const bindings = Array.isArray(profile?.accountBindings) ? profile.accountBindings : [];
    const binding = bindings.find((candidate) => String(candidate?.tenantId || "") === tenantId);
    accountId = String(binding?.accountId || "").trim();
  } catch {
    accountId = "";
  }
  receipt.evidence.readbacks.accountBinding = { tenantId, accountId };

  // The namespace the package belongs to.
  let namespaceId = "";
  try {
    const namespaces = (await http.json("/api/v2/namespaces", {}, auth)).payload;
    const items = Array.isArray(namespaces?.items) ? namespaces.items : [];
    const chosen = items.find((item) => item?.isDefault) || items[0] || null;
    namespaceId = String(chosen?.id || "").trim();
    if (!namespaceId) {
      const created = (await http.json("/api/v2/namespaces", { method: "POST", body: { name: "customer-agent" } }, auth)).payload;
      namespaceId = String(created?.id || "").trim();
    }
    if (!namespaceId) throw new Error("the owner holds no namespace for the package");
  } catch (error) {
    return stop("namespace", "customer_agent_namespace_unavailable", String(error?.message || error));
  }
  record("namespace", "confirmed", { namespaceId });

  // The package.
  const effectiveName = packageName || `customer-agent-${now()}`;
  const effectiveVersion = versionLabel || "1.0.0";
  if (!namePattern.test(effectiveName) || !namePattern.test(effectiveVersion)) {
    return stop("package", "customer_agent_package_name_invalid", "the package name and version label must be 1-128 letters, digits, dots, hyphens or underscores");
  }
  let packageId = "";
  try {
    const created = (await http.json("/api/v2/packages", {
      method: "POST",
      headers: { "idempotency-key": `customer-agent-package:${options.sourceSha}:${effectiveName}` },
      body: { namespaceId, name: effectiveName, description: "customer agent lifecycle evidence package" },
    }, auth)).payload;
    packageId = String(created?.id || "").trim();
    if (!packageId) throw new Error("the owner created no package");
    receipt.evidence.readbacks.package = { id: packageId, namespaceId, name: effectiveName };
  } catch (error) {
    return stop("package", "customer_agent_package_create_failed", String(error?.message || error));
  }
  record("package_create", "confirmed", { packageId });

  // The upload.
  let uploadId = "";
  let packageVersionId = "";
  try {
    const session = (await http.json(`/api/v2/packages/${encodeURIComponent(packageId)}/uploads`, {
      method: "POST",
      headers: { "idempotency-key": `customer-agent-upload:${options.sourceSha}:${effectiveName}` },
      body: { versionLabel: effectiveVersion, fileName: `${effectiveName}.zip`, sizeBytes: archive.bytes.length, sha256: archive.digest },
    }, auth)).payload;
    uploadId = String(session?.id || "").trim();
    packageVersionId = String(session?.packageVersionId || "").trim();
    const partSize = Number(session?.partSizeBytes || 0);
    if (!uploadId || !packageVersionId) throw new Error("the owner created no upload session");
    receipt.evidence.readbacks.upload = { id: uploadId, packageVersionId, partSizeBytes: partSize };
    const parts = [];
    const count = partSize > 0 ? Math.ceil(archive.bytes.length / partSize) : 1;
    for (let number = 1; number <= count; number += 1) {
      const slice = archive.bytes.subarray((number - 1) * partSize, number * partSize);
      const partDigest = `sha256:${createHash("sha256").update(slice).digest("hex")}`;
      const authorization = (await http.json(`/api/v2/uploads/${encodeURIComponent(uploadId)}/parts`, {
        method: "POST",
        headers: { "idempotency-key": `customer-agent-part:${options.sourceSha}:${uploadId}:${number}` },
        body: { partNumber: number, sizeBytes: slice.length, sha256: partDigest },
      }, auth)).payload;
      const url = String(authorization?.url || "").trim();
      if (!url) throw new Error(`the owner authorized no URL for part ${number}`);
      const putHeaders: Record<string, string> = { "content-type": String(authorization?.contentType || "application/octet-stream") };
      const checksumName = String(authorization?.requiredChecksumHeaderName || "");
      const checksumValue = String(authorization?.requiredChecksumHeaderValue || "");
      if (checksumName) putHeaders[checksumName.toLowerCase()] = checksumValue;
      const put = await fetch(url, { method: String(authorization?.method || "PUT"), headers: putHeaders, body: slice });
      if (put.status >= 300) {
        throw new Error(`part ${number} was refused with HTTP ${put.status}`);
      }
      parts.push({ partNumber: number, etag: partDigest.replace(/^sha256:/, ""), sizeBytes: slice.length, sha256: partDigest });
      note("upload_part", "PUT", url.replace(/\?.*$/, ""), put.status);
    }
    const completed = await http.json(`/api/v2/uploads/${encodeURIComponent(uploadId)}/complete`, {
      method: "POST",
      headers: { "idempotency-key": `customer-agent-complete:${options.sourceSha}:${uploadId}` },
      body: { parts },
    }, auth);
    note("upload_complete", "POST", `/api/v2/uploads/${uploadId}/complete`, completed.response.status);
    receipt.evidence.readbacks.upload.operationId = String(completed.payload?.operationId || "");
  } catch (error) {
    return stop("upload", "customer_agent_upload_failed", String(error?.message || error));
  }
  record("upload", "confirmed", { uploadId, packageVersionId });

  // The automatic build. The platform is expected to start it when the upload
  // completes; this run only watches for it and reports its absence.
  let build = null;
  try {
    const delays = pollSchedule(200, DEFAULT_POLL_INTERVAL_MS, DEFAULT_POLL_MAX_INTERVAL_MS);
    const deadline = now() + options.buildTimeoutMs;
    for (let attempt = 0; attempt < delays.length; attempt += 1) {
      const listed = (await http.json(`/api/v2/builds?packageVersionId=${encodeURIComponent(packageVersionId)}`, {}, auth)).payload;
      note("automatic_build", "GET", `/api/v2/builds?packageVersionId=${packageVersionId}`, 200);
      const items = Array.isArray(listed?.items) ? listed.items : [];
      const found = items.find((item) => String(item?.packageVersionId || "") === packageVersionId) || null;
      if (found) {
        build = found;
        break;
      }
      if (now() >= deadline) break;
      await wait(Math.min(delays[attempt], Math.max(1, deadline - now())));
    }
    if (!build) {
      return stop("automatic_build", "customer_agent_automatic_build_not_triggered",
        `the owner started no build for package version ${packageVersionId} within ${options.buildTimeoutMs} ms`);
    }
    const buildId = String(build.id || "");
    const detail = (await http.json(`/api/v2/builds/${encodeURIComponent(buildId)}`, {}, auth)).payload;
    note("build_readback", "GET", `/api/v2/builds/${buildId}`, 200);
    const status = String(detail?.status || build.status || "");
    if (status !== "succeeded") {
      return stop("automatic_build", "customer_agent_build_not_succeeded",
        `build ${buildId} is ${status || "unknown"}: ${String(detail?.errorCode || detail?.stage || "")}`);
    }
    const digest = String(detail?.artifactDigest || detail?.artifact?.digest || "").trim();
    if (!digestPattern.test(digest)) {
      return stop("automatic_build", "customer_agent_build_artifact_missing", `build ${buildId} reported no immutable artifact digest`);
    }
    build = { ...detail, id: buildId, artifactDigest: digest };
    receipt.digests.buildArtifactDigest = digest;
    receipt.evidence.readbacks.build = { id: buildId, status, stage: String(detail?.stage || ""), artifactDigest: digest };
  } catch (error) {
    return stop("automatic_build", "customer_agent_build_readback_failed", String(error?.message || error));
  }
  record("automatic_build", "confirmed", { buildId: build.id, artifactDigest: build.artifactDigest });

  // The registry readback of the build output.
  try {
    const buildArtifact = `${buildRepository}@${build.artifactDigest}`;
    if (!artifactPattern.test(buildArtifact)) {
      return stop("registry_manifest", "customer_agent_build_artifact_unknown", `the build reported no immutable artifact for ${build.artifactDigest}`);
    }
    receipt.digests.buildArtifact = buildArtifact;
    const manifest = dependencies.readManifest
      ? await dependencies.readManifest({ repository: buildRepository, digest: build.artifactDigest })
      : await readRegistryManifestDigest({
        repository: buildRepository,
        digest: build.artifactDigest,
        registryUrl: options.registryUrl || "",
        token: options.registryToken || String(values.OPL_BUILD_REGISTRY_TOKEN || ""),
      });
    note("registry_manifest", "GET", String(manifest.url || ""), manifest.status);
    if (manifest.status >= 300) {
      return stop("registry_manifest", "customer_agent_registry_manifest_unreadable", `the registry answered HTTP ${manifest.status} for ${buildArtifact}`);
    }
    receipt.digests.registryManifestDigest = String(manifest.digest || "");
  } catch (error) {
    return stop("registry_manifest", "customer_agent_registry_manifest_failed", String(error?.message || error));
  }
  record("registry_manifest", "confirmed", { artifact: receipt.digests.buildArtifact, digest: receipt.digests.registryManifestDigest });

  // The registered capability version. The platform is expected to register the
  // build output; this run watches for it and reports its absence.
  let capabilityVersion = null;
  try {
    const delays = pollSchedule(200, DEFAULT_POLL_INTERVAL_MS, DEFAULT_POLL_MAX_INTERVAL_MS);
    const deadline = now() + options.registrationTimeoutMs;
    for (let attempt = 0; attempt < delays.length; attempt += 1) {
      const listed = (await http.json(
        `/api/v2/capability-versions?packageId=${encodeURIComponent(packageId)}&status=ready`, {}, auth)).payload;
      note("capability_registration", "GET", `/api/v2/capability-versions?packageId=${packageId}&status=ready`, 200);
      const items = Array.isArray(listed?.items) ? listed.items : [];
      const matching = items.filter((item) => String(item?.packageVersionId || "") === packageVersionId);
      const preferred = matching.find((item) => String(item?.buildJobId || "") === String(build.id || "")) ||
        matching.sort((left, right) => String(right?.createdAt || "").localeCompare(String(left?.createdAt || "")))[0] || null;
      if (preferred) {
        capabilityVersion = preferred;
        break;
      }
      if (now() >= deadline) break;
      await wait(Math.min(delays[attempt], Math.max(1, deadline - now())));
    }
    if (!capabilityVersion) {
      return stop("capability_registration", "customer_agent_capability_version_not_registered",
        `the owner registered no capability version for package version ${packageVersionId} within ${options.registrationTimeoutMs} ms`);
    }
    const detail = (await http.json(`/api/v2/capability-versions/${encodeURIComponent(capabilityVersion.id)}`, {}, auth)).payload;
    note("capability_readback", "GET", `/api/v2/capability-versions/${capabilityVersion.id}`, 200);
    capabilityVersion = { ...capabilityVersion, ...detail, id: String(detail?.id || capabilityVersion.id) };
    receipt.digests.capabilityArtifact = artifactReference(capabilityVersion);
    receipt.evidence.readbacks.capabilityVersion = {
      id: capabilityVersion.id,
      status: String(capabilityVersion.status || ""),
      packageVersionId: String(capabilityVersion.packageVersionId || ""),
      buildJobId: String(capabilityVersion.buildJobId || ""),
      artifact: receipt.digests.capabilityArtifact,
    };
  } catch (error) {
    return stop("capability_registration", "customer_agent_capability_readback_failed", String(error?.message || error));
  }
  record("capability_registration", "confirmed", { capabilityVersionId: capabilityVersion.id });

  // The quote. The plan pair is the customer's own catalog read.
  let quoteId = "";
  try {
    const computePlans = (await http.json("/api/v2/catalog/compute-plans", {}, auth)).payload;
    const storagePlans = (await http.json("/api/v2/catalog/storage-plans", {}, auth)).payload;
    note("catalog", "GET", "/api/v2/catalog/compute-plans", 200);
    const computePlanId = String((Array.isArray(computePlans?.items) ? computePlans.items : [])[0]?.id || "").trim();
    const storagePlanId = String((Array.isArray(storagePlans?.items) ? storagePlans.items : [])[0]?.id || "").trim();
    if (!computePlanId || !storagePlanId) throw new Error("the catalog offers no compute plan and storage plan pair");
    const quote = (await http.json("/api/v2/quotes", {
      method: "POST",
      headers: { "idempotency-key": `customer-agent-quote:${options.sourceSha}:${capabilityVersion.id}` },
      body: {
        purpose: "deploy",
        capabilityVersionId: capabilityVersion.id,
        computePlanId,
        storagePlanId,
        periodMonths: 1,
      },
    }, auth)).payload;
    quoteId = String(quote?.quoteId || quote?.id || "").trim();
    if (!quoteId) throw new Error("the catalog answered no quote");
    receipt.evidence.readbacks.quote = { quoteId, computePlanId, storagePlanId };
  } catch (error) {
    return stop("quote", "customer_agent_quote_failed", String(error?.message || error));
  }
  record("quote", "confirmed", { quoteId });

  // The Workspace.
  const workspaceName = `customer-agent-${effectiveName}-${now()}`;
  let workspaceId = "";
  try {
    const created = await http.json("/api/v2/workspaces", {
      method: "POST",
      headers: { "idempotency-key": `customer-agent-workspace:${options.sourceSha}:${quoteId}` },
      body: { name: workspaceName, quote_id: quoteId },
    }, auth, [200, 201, 202]);
    const payload = created.payload || {};
    workspaceId = String(payload.id || payload.workspaceId || "").trim();
    if (!workspaceId) throw new Error("the owner created no workspace");
    receipt.evidence.readbacks.workspace = { id: workspaceId, name: workspaceName };
  } catch (error) {
    return stop("workspace", "customer_agent_workspace_create_failed", String(error?.message || error));
  }
  record("workspace", "confirmed", { workspaceId });

  // The Workspace owner's own readback.
  let workspace = null;
  try {
    const installation: any = { workspaceId, wait, attempts: 300 };
    await waitForWorkspaceApplicationInstallation(http, auth, installation);
    workspace = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth)).payload;
    note("workspace_readback", "GET", `/api/v2/workspaces/${workspaceId}`, 200);
    receipt.evidence.readbacks.workspace.status = String(workspace?.status || "");
    receipt.evidence.readbacks.workspace.accessUrl = String(workspace?.accessUrl || "");
    receipt.evidence.readbacks.workspace.capabilityVersionId = String(workspace?.capabilityVersionId || "");
    receipt.evidence.readbacks.workspace.runtimeVersionId = String(workspace?.runtimeVersionId || "");
  } catch (error) {
    return stop("workspace_readback", "customer_agent_workspace_readback_failed", String(error?.message || error));
  }
  record("workspace_readback", "confirmed", { status: String(workspace?.status || "") });

  // Layer A, structure.
  const branch = evaluateAgentBranchSelection({
    capabilityVersionId: workspace?.capabilityVersionId,
    runtimeVersionId: workspace?.runtimeVersionId,
  });
  receipt.criteria.agentBranch = branch;
  if (!branch.pass) {
    return fail("criteria_agent_branch", branch.code, branch.detail);
  }
  record("criteria_agent_branch", "confirmed", { capabilityVersionId: branch.capabilityVersionId });

  // Layer A, artifact identity.
  const artifact = evaluateArtifactIdentity({
    buildArtifact: receipt.digests.buildArtifact,
    capabilityArtifact: receipt.digests.capabilityArtifact,
    registryManifestDigest: receipt.digests.registryManifestDigest,
  });
  receipt.criteria.artifactIdentity = artifact;
  if (!artifact.pass) {
    return fail("criteria_artifact_identity", artifact.code, artifact.detail);
  }
  record("criteria_artifact_identity", "confirmed", { artifact: artifact.buildArtifact });

  // Layer B, content.
  const entryUrl = String(workspace?.accessUrl || "").trim();
  let entry = null;
  try {
    entry = dependencies.fetchEntry
      ? await dependencies.fetchEntry(entryUrl)
      : await fetchCustomerAgentEntry(entryUrl, { caFile });
    note("application_entry", "GET", entryUrl, entry.status);
    receipt.evidence.readbacks.applicationEntry = { url: entryUrl, status: entry.status };
  } catch (error) {
    return stop("application_entry", "customer_agent_entry_unreachable", String(error?.message || error));
  }
  if (entry.status !== 200) {
    return fail("application_entry", "customer_agent_entry_not_served", `the published entry answered HTTP ${entry.status}`);
  }
  const content = evaluateContentProvenance({ nonce, entryText: entry.text });
  receipt.criteria.contentProvenance = content;
  record("application_entry", content.pass ? "confirmed" : "degraded", {
    status: entry.status,
    nonceDigest: content.nonceDigest,
    entryBytes: content.entryBytes,
  });
  if (content.pass) {
    receipt.evidenceLevel = EVIDENCE_LEVEL_CONTENT_PROVENANCE;
  } else {
    receipt.evidenceLevel = EVIDENCE_LEVEL_ARTIFACT_IDENTITY;
    receipt.degradation = {
      code: content.code,
      detail: "the package under test did not echo the probe file it was uploaded with, so the published entry could not be tied to this upload by content",
    };
    if (options.expectedEvidenceLevel === EVIDENCE_LEVEL_CONTENT_PROVENANCE) {
      return fail("application_entry", content.code, receipt.degradation.detail);
    }
  }

  // The deletion, and the owners' readback of the absence.
  try {
    const deletionCommand: any = {
      workspaceId,
      confirmationName: workspaceName,
      idempotencyKey: `customer-agent-delete:${options.sourceSha}:${workspaceId}`,
      wait,
    };
    const deleted = await deleteWorkspaceOnOwnerRoute(http, auth, deletionCommand);
    const tombstone = (await http.request(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}`, {}, auth)).payload || {};
    const deletionRequest = {
      workspaceId,
      operationId: String(deleted.operationId || ""),
      localNoCharge: true,
    };
    const evidence = await readWorkspaceDeletionEvidence(http, auth, deletionRequest);
    const deletion = {
      operationId: String(deleted.operationId || ""),
      status: String(deleted.status || ""),
      workspaceAbsent: tombstone.status === "deleted",
      routeAbsent: !String(tombstone.accessUrl || "").trim(),
      resourceDeletionStatus: String(evidence.resourceDeletionStatus || ""),
      dataDeletionStatus: String(evidence.dataDeletionStatus || ""),
      refundStatus: String(evidence.refundStatus || ""),
      receiptId: String(evidence.deletionReceipt?.id || ""),
    };
    const residuals = dependencies.residualCounts
      ? await dependencies.residualCounts(accountId, workspaceId)
      : accountId
        ? await residualCounts(accountId, workspaceId)
        : null;
    if (residuals) {
      deletion.residuals = residuals;
      deletion.liveResources = Number(residuals.containers || 0) + Number(residuals.volumes || 0) + Number(residuals.networks || 0);
    }
    receipt.evidence.readbacks.deletion = deletion;
    const absent = deletion.workspaceAbsent && deletion.routeAbsent &&
      deletion.resourceDeletionStatus === "confirmed" && deletion.dataDeletionStatus === "confirmed" &&
      (residuals ? deletion.liveResources === 0 : true);
    if (!absent) {
      return fail("deletion_readback", "customer_agent_deletion_not_confirmed",
        `the owners did not confirm the absence of the deleted workspace: ${JSON.stringify(deletion)}`);
    }
    record("deletion_readback", "confirmed", deletion);
  } catch (error) {
    return stop("deletion_readback", "customer_agent_deletion_readback_failed", String(error?.message || error));
  }

  const criteria = evaluateCustomerAgentCriteria({
    branch: receipt.criteria.agentBranch,
    artifact: receipt.criteria.artifactIdentity,
    content: receipt.criteria.contentProvenance,
  });
  receipt.status = receipt.degradation ? "degraded" : "passed";
  receipt.completedAt = new Date(now()).toISOString();
  receipt.evidence.criteria = criteria;
  return receipt;
}

async function main() {
  const options = parseCustomerAgentLifecycleArgs();
  const receipt = await runCustomerAgentLifecycle(options);
  const validation = validateCustomerAgentLifecycleReceipt(receipt);
  if (options.receiptPath) {
    await writeFile(options.receiptPath, `${JSON.stringify(receipt, null, 2)}\n`, { mode: 0o600 });
  }
  process.stdout.write(`${JSON.stringify(receipt, null, 2)}\n`);
  if (!validation.valid) {
    process.stderr.write(`customer agent lifecycle receipt is invalid: ${validation.errors.join("; ")}\n`);
    process.exitCode = 1;
    return;
  }
  if (receipt.status !== "passed") process.exitCode = 1;
}

const invoked = Boolean(process.argv[1]) && resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invoked) {
  await main();
}
