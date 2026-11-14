// The published bundle must install itself.
//
// A bundle that ships an installer nobody has ever run is a bundle whose
// installability is a claim. This module turns that claim into a check: it
// derives the installation facts the bundle cannot fill on its own, runs the
// bundle's own installer in dry-run mode against the bundle's own bytes, and
// emits a receipt that binds the result to those bytes.
//
// The installer owns the fact list. It names the facts it cannot fill, and
// this module answers exactly those names from the bundle's own declarations.
// A fact the installer starts asking for has no derivation here, and the
// module refuses rather than shipping a guess; a derivation the installer no
// longer asks for is stale, and the module refuses that too. The bundle's
// required-fact count and its manual-fact count are read back from the
// installation state the installer itself wrote, so the distribution
// contract's declared numbers cannot drift away from the implementation.
//
// The fixture values are test values. They are derived from declarations the
// bundle already carries (the manifest's image reference, the template's
// declared values, the local topology's declared defaults), never copied from
// a customer installation, and they carry no credential.

import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

export const INSTALLABILITY_RECEIPT_KIND = "opl_cloud_bundle_installability";
export const INSTALLABILITY_RECEIPT_SCHEMA_VERSION = 1;

// The installation facts the installer cannot fill on its own. Each entry
// derives one value from a declaration the bundle carries, and names that
// declaration so the receipt can say where every test value came from.
//
// The probe run of the installer names the facts it is missing. The set below
// has to be exactly that set: a fact the installer asks for and this table
// cannot answer is a gap, and a fact this table answers but the installer no
// longer asks for is stale.
const MANUAL_FACT_DERIVATIONS = {
  // The image the bundle ships is the image the installation may run. The
  // candidate manifest carries its exact digest, so the approved Workspace
  // image is the bundle's own image rather than a second invented reference.
  OPL_WORKSPACE_IMAGE: "manifest.imageRef",
  OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES: "manifest.imageRef",

  // The bundle's local topology declares these three defaults, so they are the
  // bundle's own declared values rather than values this module chose.
  OPL_WORKSPACE_REGISTRY_HOST: "bundle.default",
  OPL_WORKSPACE_REGISTRY_NAMESPACE: "bundle.default",
  OPL_GATEWAY_WORKSPACE_KEY_GROUP: "bundle.default",
  OPL_SUB2API_BASE_URL: "bundle.default",

  // The Workspace host the installer owns. The template's own comment names
  // the shape: a Workspace subdomain of the installation's own host.
  OPL_WORKSPACE_DOMAIN: "derived.workspaceDomain",

  // The public object origin is the declared internal origin with its host
  // replaced by the loopback host: same scheme, same port, same path.
  OPL_CAPABILITY_OBJECT_URL: "template.derived",

  // The two Sub2API identities and the platform admin subject that names the
  // administrator. The subject is the administrator identity, so one identity
  // is spelled once.
  OPL_SUB2API_ADMIN_EMAIL: "derived.identity",
  OPL_SUB2API_USER_EMAIL: "derived.identity",
  OPL_PLATFORM_ADMIN_SUBJECTS: "derived.identity",

  // The Resource Catalog profile is the deployment-owned provider profile the
  // template already declares, narrowed to what the catalog prices in.
  OPL_RESOURCE_CATALOG_PROFILE_JSON: "template.derived",
  OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: "template.derived",
};

export function installabilityError(code, detail = "") {
  const error = new Error(detail ? `${code}: ${detail}` : code);
  error.code = code;
  return error;
}

function sha256(text) {
  return `sha256:${createHash("sha256").update(text).digest("hex")}`;
}

// Every assignment the template declares, in the template's order. A name the
// template declares twice is a defect of the template, and the Compose
// environment reader keeps the last definition, so a duplicate is refused
// rather than resolved silently.
function templateDeclarations(template) {
  const declarations = new Map();
  for (const line of String(template || "").split("\n")) {
    const match = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line);
    if (!match) continue;
    if (declarations.has(match[1])) {
      throw installabilityError("bundle_installability_template_duplicate", match[1]);
    }
    declarations.set(match[1], match[2]);
  }
  return declarations;
}

// The defaults the bundle's own local topology declares, such as
// "OPL_WORKSPACE_REGISTRY_HOST: ${OPL_WORKSPACE_REGISTRY_HOST:-registry.local-v2.test}".
// The bundle declares these values, so the fixture uses the bundle's own
// declaration instead of one this module chose.
export function bundleDeclaredDefaults(compose) {
  const defaults = new Map();
  for (const line of String(compose || "").split("\n")) {
    const match = /([A-Za-z_][A-Za-z0-9_]*):\s*\$\{[A-Za-z_][A-Za-z0-9_]*:-([^}]*)\}/.exec(line);
    if (!match) continue;
    if (!defaults.has(match[1])) defaults.set(match[1], match[2]);
  }
  return defaults;
}

// The installer refuses a value that still holds a placeholder. A placeholder
// is an unfilled fact, and a raw "<" is a redirection operator to any shell
// that reads the file.
function isPlaceholder(value) {
  return /<[^>\n]+>/.test(String(value ?? ""));
}

// The template declares the installation's billing mode in the comment above
// the Resource Catalog profile: "Its billingMode must be exactly
// LOCAL_NO_CHARGE, which is the only mode this installation prices in."
// (deploy/portable/opl-cloud.env.example:445-446). The value is checked
// against that declaration here, so a template that changes the rule breaks
// this module instead of silently pricing the wrong mode.
const LOCAL_BILLING_MODE = "LOCAL_NO_CHARGE";

function assertTemplateDeclaresBillingMode(template) {
  if (!String(template || "").includes(`billingMode must be exactly\n#                                     ${LOCAL_BILLING_MODE}`)) {
    throw installabilityError("bundle_installability_billing_mode_undeclared", LOCAL_BILLING_MODE);
  }
}

// The provider profile the template declares. It is the deployment-owned
// profile the Resource Catalog profile is derived from.
function templateProviderProfile(declarations) {
  const raw = declarations.get("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON");
  if (!raw) throw installabilityError("bundle_installability_provider_profile_missing");
  let profile;
  try { profile = JSON.parse(raw); } catch { throw installabilityError("bundle_installability_provider_profile_invalid"); }
  if (!profile || typeof profile !== "object" || Array.isArray(profile)) {
    throw installabilityError("bundle_installability_provider_profile_invalid");
  }
  return profile;
}

// The tenant the installation serves. The template's provider profile carries
// it as a placeholder; the bundle's own product identity names it.
function deriveTenantId(project) {
  return `${project}-installability`;
}

// The derived test values. Every value names the declaration it came from.
export function bundleInstallabilityFacts({ template, publishedCompose, localV2Compose, manifest }) {
  const declarations = templateDeclarations(template);
  const defaults = bundleDeclaredDefaults(localV2Compose);
  assertTemplateDeclaresBillingMode(template);

  const imageRef = String(manifest?.cloudImage?.indexRef || "");
  if (!/^[^\s@]+@sha256:[0-9a-f]{64}$/.test(imageRef)) {
    throw installabilityError("bundle_installability_image_ref_invalid", imageRef);
  }
  const project = String(manifest?.project || "opl-cloud");
  const values = new Map();
  const sources = new Map();

  const set = (name, value, source) => {
    if (values.has(name)) throw installabilityError("bundle_installability_fact_derived_twice", name);
    values.set(name, value);
    sources.set(name, source);
  };

  // The bundle's image reference, used for the Workspace image and for the
  // approved image list Fabric admits.
  set("OPL_WORKSPACE_IMAGE", imageRef, "manifest.imageRef");
  set("OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES", imageRef, "manifest.imageRef");

  // The values the bundle's own local topology declares.
  for (const name of ["OPL_WORKSPACE_REGISTRY_HOST", "OPL_WORKSPACE_REGISTRY_NAMESPACE", "OPL_GATEWAY_WORKSPACE_KEY_GROUP", "OPL_SUB2API_BASE_URL"]) {
    const declared = defaults.get(name);
    if (!declared || isPlaceholder(declared)) {
      throw installabilityError("bundle_installability_default_missing", name);
    }
    set(name, declared, "bundle.default");
  }

  // The Workspace host the installer owns: a Workspace subdomain of the
  // installation's own loopback host.
  set("OPL_WORKSPACE_DOMAIN", `ws.${project}.localhost`, "derived.workspaceDomain");

  // The public object origin: the declared internal origin with its host
  // replaced by the loopback host.
  const internal = declarations.get("OPL_CAPABILITY_OBJECT_INTERNAL_URL");
  if (!internal || isPlaceholder(internal)) throw installabilityError("bundle_installability_object_internal_url_missing");
  let objectUrl;
  try {
    const url = new URL(internal);
    url.hostname = "127.0.0.1";
    objectUrl = url.toString();
  } catch { throw installabilityError("bundle_installability_object_internal_url_invalid", internal); }
  set("OPL_CAPABILITY_OBJECT_URL", objectUrl, "template.derived");

  // The two Sub2API identities and the platform admin subject.
  const adminIdentity = `${project}-installability-admin`;
  set("OPL_SUB2API_ADMIN_EMAIL", `${adminIdentity}@example.test`, "derived.identity");
  set("OPL_SUB2API_USER_EMAIL", `${project}-installability-user@example.test`, "derived.identity");
  set("OPL_PLATFORM_ADMIN_SUBJECTS", adminIdentity, "derived.identity");

  // The Resource Catalog profile: the declared provider profile narrowed to
  // what this installation prices in.
  const provider = templateProviderProfile(declarations);
  const providerId = String(provider.profileId || "");
  const providerNamespace = providerId.includes("/") ? providerId.split("/")[0] : providerId;
  set("OPL_RESOURCE_CATALOG_PROFILE_JSON", JSON.stringify({
    provider: providerNamespace,
    region: String(provider.region || ""),
    billingMode: LOCAL_BILLING_MODE,
    providerCapabilityVersion: String(provider.capabilityVersion || ""),
  }), "template.derived");

  // The Fabric provider profile: the declared profile with its tenant
  // placeholder replaced by the identity this installation serves.
  const providerProfileRaw = String(declarations.get("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON") || "");
  const tenantId = deriveTenantId(project);
  const filled = providerProfileRaw.replace(/<[^>\n]+>/g, tenantId);
  if (isPlaceholder(filled)) throw installabilityError("bundle_installability_provider_profile_placeholder");
  set("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", filled, "template.derived");

  return { values, sources };
}

// The facts the installer refused to run without. The installer names them in
// one error message, so this module reads that message instead of deciding
// which facts are manual.
export function parseInstallationFactsRequired(stderr) {
  const text = String(stderr || "");
  const marker = "installation_facts_required:";
  const index = text.indexOf(marker);
  if (index < 0) return [];
  const names = [];
  for (const line of text.slice(index + marker.length).split("\n")) {
    const name = line.trim();
    if (!name) continue;
    if (!/^[A-Z][A-Z0-9_]*$/.test(name)) break;
    names.push(name);
  }
  return names;
}

function runInstaller(bundle, installDir) {
  try {
    const stdout = execFileSync("bash", [join(bundle, "install.sh"), "--install-dir", installDir, "--dry-run"], {
      cwd: bundle, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
    });
    return { ok: true, stdout, stderr: "" };
  } catch (error) {
    return {
      ok: false,
      stdout: String(error.stdout || ""),
      stderr: String(error.stderr || ""),
      code: error.status ?? null,
    };
  }
}

// The installation state the installer itself wrote. Its values are the
// facts the bundle actually installed with, so the count of them is the
// bundle's required-fact count.
async function readInstallationState(installDir) {
  const raw = await readFile(join(installDir, "installation.json"), "utf8");
  let state;
  try { state = JSON.parse(raw); } catch { throw installabilityError("bundle_installability_state_invalid"); }
  if (!state || typeof state !== "object" || !state.values || typeof state.values !== "object") {
    throw installabilityError("bundle_installability_state_invalid");
  }
  return state;
}

/*
 * Run the bundle's own installer against the bundle's own bytes and report
 * what it proved.
 */
export async function verifyBundleInstallability({ bundle, installDir, contract, manifest }) {
  const contractFormat = String(contract?.bundleFormats?.current || "");
  const declaration = contract?.bundleFormats?.formats?.[contractFormat]?.installability;
  if (!declaration || typeof declaration !== "object") {
    throw installabilityError("bundle_installability_not_declared", contractFormat);
  }
  const template = await readFile(join(bundle, "opl-cloud.env.example"), "utf8");
  const publishedCompose = await readFile(join(bundle, "compose.cloud.yaml"), "utf8");
  const localV2Compose = await readFile(join(bundle, "compose.local-v2.yaml"), "utf8");
  const manifestBytes = manifest ?? JSON.parse(await readFile(join(bundle, "opl-cloud-candidate.json"), "utf8"));

  const facts = bundleInstallabilityFacts({
    template, publishedCompose, localV2Compose, manifest: manifestBytes,
  });

  await mkdir(installDir, { recursive: true, mode: 0o700 });

  // The first run asks the bundle's questions. The installer names the facts
  // it cannot fill, and that list has to be exactly the list this module
  // derives: a missing derivation is a gap, a stale derivation is drift.
  const probe = runInstaller(bundle, installDir);
  const asked = parseInstallationFactsRequired(probe.stderr);
  if (!asked.length) {
    throw installabilityError("bundle_installability_probe_failed", probe.stderr.trim().split("\n").slice(-3).join(" "));
  }
  const derived = [...facts.values.keys()].sort();
  const missing = asked.filter((name) => !facts.values.has(name));
  if (missing.length) throw installabilityError("bundle_installability_fact_not_derived", missing.join(","));
  const stale = derived.filter((name) => !asked.includes(name));
  if (stale.length) throw installabilityError("bundle_installability_fact_derivation_stale", stale.join(","));

  // The installer reads the facts it keeps from the installation state, so the
  // derived values are seeded there, bound to this bundle's identity.
  await writeFile(join(installDir, "installation.json"), `${JSON.stringify({
    schemaVersion: 1,
    project: "opl-cloud",
    imageRef: String(manifestBytes.cloudImage?.indexRef || ""),
    sourceSha: String(manifestBytes.product?.sha || ""),
    sourceTree: String(manifestBytes.product?.tree || ""),
    values: Object.fromEntries(facts.values),
    updatedAt: new Date().toISOString(),
  }, null, 2)}\n`, { mode: 0o600 });

  // The second run installs. It pulls no image and starts no container: the
  // dry run returns before the first pull.
  const install = runInstaller(bundle, installDir);
  if (!install.ok) {
    throw installabilityError("bundle_installability_dry_run_failed", install.stderr.trim().split("\n").slice(-3).join(" "));
  }

  const state = await readInstallationState(installDir);
  const installedFacts = Object.keys(state.values).length;
  const manualFacts = asked.length;
  if (installedFacts !== Number(declaration.installedFacts)) {
    throw installabilityError("bundle_installability_installed_facts_drift",
      `${installedFacts} != ${declaration.installedFacts}`);
  }
  if (manualFacts !== Number(declaration.manualFacts)) {
    throw installabilityError("bundle_installability_manual_facts_drift",
      `${manualFacts} != ${declaration.manualFacts}`);
  }

  const composeVersion = String(state.composeVersion || "");
  return {
    schemaVersion: INSTALLABILITY_RECEIPT_SCHEMA_VERSION,
    kind: INSTALLABILITY_RECEIPT_KIND,
    bundleFormat: contractFormat,
    installer: String(declaration.installer || ""),
    entrypoint: String(declaration.entrypoint || ""),
    contract: String(declaration.contract || ""),
    imageRef: String(manifestBytes.cloudImage?.indexRef || ""),
    productSha: String(manifestBytes.product?.sha || ""),
    productTree: String(manifestBytes.product?.tree || ""),
    installedFacts,
    manualFacts,
    composeVersion,
    dryRun: "verified",
    facts: asked.map((name) => ({
      name,
      source: String(facts.sources.get(name) || ""),
      value: String(facts.values.get(name) ?? ""),
    })),
  };
}

export function installabilityReceiptDigest(receipt) {
  return sha256(`${JSON.stringify(receipt, null, 2)}\n`);
}

async function main(args) {
  const options = { bundle: "", installDir: "", receipt: "" };
  const rest = args[0] === "verify" ? args.slice(1) : args;
  for (let index = 0; index < rest.length; index += 1) {
    const arg = rest[index];
    if (arg === "--bundle") { options.bundle = rest[++index] ?? ""; continue; }
    if (arg === "--install-dir") { options.installDir = rest[++index] ?? ""; continue; }
    if (arg === "--receipt") { options.receipt = rest[++index] ?? ""; continue; }
    throw installabilityError("bundle_installability_argument_unknown", arg);
  }
  if (!options.bundle || !options.installDir) {
    throw installabilityError("bundle_installability_arguments_required", "--bundle and --install-dir");
  }
  const contract = JSON.parse(await readFile(join(options.bundle, "opl-cloud-distribution-contract.json"), "utf8"));
  const receipt = await verifyBundleInstallability({
    bundle: options.bundle, installDir: options.installDir, contract,
  });
  const text = `${JSON.stringify(receipt, null, 2)}\n`;
  const digest = installabilityReceiptDigest(receipt);
  if (options.receipt) {
    await writeFile(options.receipt, text, { mode: 0o600 });
    process.stdout.write(`receipt_digest=${digest}\n`);
    return;
  }
  process.stdout.write(text);
  process.stdout.write(`receipt_digest=${digest}\n`);
}

if (process.argv[1] && process.argv[1].endsWith("bundle-installability.ts")) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(`bundle-installability: ${error.message}`);
    process.exitCode = error.exitCode || 1;
  });
}
