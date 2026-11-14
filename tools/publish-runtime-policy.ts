/*
 * Publish the platform's effective Runtime + WebUI policy.
 *
 * The Publisher page cannot start a customer build until the platform declares
 * which Runtime Release and which WebUI artifact a new build is allowed to use.
 * That declaration is three owner calls in a fixed order:
 *
 *   POST /api/v2/admin/catalog/runtime-versions  -> Runtime Control (catalog)
 *   POST /api/v2/admin/catalog/webui-versions     -> Capability (catalog)
 *   PUT  /api/v2/admin/catalog/build-policy       -> Runtime Control (catalog)
 *
 * The order matters. The policy row names a runtime version and a WebUI
 * version, so both catalog entries must exist before the policy can point at
 * them. This tool therefore writes the policy last, and it refuses to write a
 * policy whose two targets are not registered and readable.
 *
 * What this tool does NOT do:
 *   - it does not invent a Runtime Release. The publisher contract is an
 *     upstream artifact; this tool only carries the bytes the upstream wrote.
 *   - it does not derive a recipe, an image digest, a package format or a
 *     WebUI artifact. A missing immutable identity is refused by name so a
 *     caller cannot silently substitute a mutable tag or a fixture.
 *   - it does not widen the approval. Registering a Runtime Release still goes
 *     through the owner's own admission (approved publisher namespace, schema
 *     check, admission receipt); this tool cannot bypass it.
 *
 * The contract shapes are owned by Cloud and validated by the owner services:
 *   docs/spec/target/contracts/publisher-contract.schema.json
 *
 * The caller supplies:
 *   --runtime <file>  a Runtime publisher contract (kind: "runtime")
 *   --webui <file>    a WebUI publisher contract (kind: "webui")
 *   --base <url>      the Console base URL of the installation
 * and authenticates as a platform administrator through the Console session
 * endpoints, using OPL_CATALOG_ADMIN_EMAIL / OPL_CATALOG_ADMIN_PASSWORD.
 *
 * A receipt is written last and records what was published, so the publication
 * is readable after the fact without trusting the process that produced it.
 */

import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const DIGEST = /^sha256:[0-9a-f]{64}$/;
const ABI_VERSION = /^[A-Za-z0-9][A-Za-z0-9._/-]*$/;

export type PublisherContract = Record<string, unknown>;

export type Refusal = { reason: string; field: string };

export type RegistrationRequest = {
  name: string;
  versionLabel: string;
  publisherNamespaceId: string;
  publisherContract: PublisherContract;
  admissionReceiptId: string;
};

export type PolicyRequest = {
  runtimeVersionId: string;
  defaultWebuiVersionId: string;
};

export type EffectivePolicy = {
  runtimeVersionId?: string;
  defaultWebuiVersionId?: string;
  policyVersion?: string;
  effectiveAt?: string;
};

export type PublishOptions = {
  base: string;
  runtimeContract: PublisherContract;
  webuiContract: PublisherContract;
  runtimeAdmissionReceiptId: string;
  webuiAdmissionReceiptId: string;
  runtimeName: string;
  runtimeVersionLabel: string;
  webuiName: string;
  webuiVersionLabel: string;
  email: string;
  password: string;
  fetchImpl?: typeof fetch;
};

export type PublishResult = {
  published: boolean;
  runtimeVersionId?: string;
  webuiVersionId?: string;
  policyVersion?: string;
  refusals: Refusal[];
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim() !== "";
}

/**
 * The upstream contract is carried as bytes, so every refusal here is about a
 * missing immutable identity, never about a value this tool could have filled
 * in on the upstream's behalf.
 */
export function contractRefusals(contract: unknown, kind: "runtime" | "webui"): Refusal[] {
  const refusals: Refusal[] = [];
  if (!isRecord(contract)) {
    return [{ reason: "publisher_contract_missing", field: "publisherContract" }];
  }
  if (contract.kind !== kind) {
    refusals.push({ reason: "publisher_contract_kind_mismatch", field: "publisherContract.kind" });
  }
  if (!nonEmptyString(contract.schemaVersion)) {
    refusals.push({ reason: "publisher_contract_schema_version_missing", field: "publisherContract.schemaVersion" });
  }
  if (!nonEmptyString(contract.publisherNamespaceId)) {
    refusals.push({ reason: "publisher_namespace_missing", field: "publisherContract.publisherNamespaceId" });
  }
  const image = contract.image;
  if (!isRecord(image)) {
    refusals.push({ reason: "publisher_image_missing", field: "publisherContract.image" });
  } else {
    if (!nonEmptyString(image.repository)) {
      refusals.push({ reason: "publisher_image_repository_missing", field: "publisherContract.image.repository" });
    }
    if (!DIGEST.test(String(image.digest))) {
      refusals.push({ reason: "publisher_image_digest_not_immutable", field: "publisherContract.image.digest" });
    }
  }
  if (kind === "runtime") {
    if (!nonEmptyString(contract.runtimeAbiVersion) || !ABI_VERSION.test(String(contract.runtimeAbiVersion))) {
      refusals.push({ reason: "runtime_abi_version_missing", field: "publisherContract.runtimeAbiVersion" });
    }
    if (!Array.isArray(contract.packageFormatVersions) || contract.packageFormatVersions.length === 0) {
      refusals.push({ reason: "package_format_versions_missing", field: "publisherContract.packageFormatVersions" });
    }
    const recipe = contract.buildRecipe;
    if (!isRecord(recipe)) {
      refusals.push({ reason: "build_recipe_missing", field: "publisherContract.buildRecipe" });
    } else {
      const frontend = recipe.frontend;
      const recipeArtifact = recipe.recipe;
      if (!isRecord(frontend) || !DIGEST.test(String(frontend.digest))) {
        refusals.push({ reason: "build_recipe_frontend_digest_not_immutable", field: "publisherContract.buildRecipe.frontend.digest" });
      }
      if (!isRecord(recipeArtifact) || !DIGEST.test(String(recipeArtifact.digest))) {
        refusals.push({ reason: "build_recipe_digest_not_immutable", field: "publisherContract.buildRecipe.recipe.digest" });
      }
      const packageInput = recipe.packageInput;
      if (!isRecord(packageInput) || !nonEmptyString(packageInput.formatVersion)) {
        refusals.push({ reason: "build_recipe_package_format_missing", field: "publisherContract.buildRecipe.packageInput.formatVersion" });
      } else if (Array.isArray(contract.packageFormatVersions) && !contract.packageFormatVersions.includes(packageInput.formatVersion)) {
        refusals.push({
          reason: "build_recipe_package_format_not_declared_by_runtime",
          field: "publisherContract.buildRecipe.packageInput.formatVersion"
        });
      }
    }
  }
  return refusals;
}

export function buildRuntimeRegistrationRequest(
  contract: PublisherContract,
  admissionReceiptId: string,
  name: string,
  versionLabel: string
): RegistrationRequest {
  return {
    name,
    versionLabel,
    publisherNamespaceId: String(contract.publisherNamespaceId ?? ""),
    publisherContract: contract,
    admissionReceiptId
  };
}

export function buildWebuiRegistrationRequest(
  contract: PublisherContract,
  admissionReceiptId: string,
  name: string,
  versionLabel: string
): RegistrationRequest {
  return {
    name,
    versionLabel,
    publisherNamespaceId: String(contract.publisherNamespaceId ?? ""),
    publisherContract: contract,
    admissionReceiptId
  };
}

export function buildPolicyRequest(runtimeVersionId: string, webuiVersionId: string): PolicyRequest {
  return { runtimeVersionId, defaultWebuiVersionId: webuiVersionId };
}

/**
 * The readback is the proof that a customer session can see the policy. It is
 * compared against the two ids this tool actually registered, so a policy that
 * points somewhere else is a refusal, not a pass.
 */
export function verifyEffectivePolicy(
  policy: unknown,
  expected: { runtimeVersionId: string; webuiVersionId: string }
): Refusal[] {
  const refusals: Refusal[] = [];
  if (!isRecord(policy)) {
    return [{ reason: "effective_policy_missing", field: "buildPolicy" }];
  }
  if (policy.runtimeVersionId !== expected.runtimeVersionId) {
    refusals.push({ reason: "effective_policy_runtime_mismatch", field: "buildPolicy.runtimeVersionId" });
  }
  if (policy.defaultWebuiVersionId !== expected.webuiVersionId) {
    refusals.push({ reason: "effective_policy_webui_mismatch", field: "buildPolicy.defaultWebuiVersionId" });
  }
  if (!nonEmptyString(policy.policyVersion)) {
    refusals.push({ reason: "effective_policy_version_missing", field: "buildPolicy.policyVersion" });
  }
  if (!nonEmptyString(policy.effectiveAt) || Number.isNaN(Date.parse(String(policy.effectiveAt)))) {
    refusals.push({ reason: "effective_policy_effective_at_invalid", field: "buildPolicy.effectiveAt" });
  } else if (Date.parse(String(policy.effectiveAt)) > Date.now()) {
    refusals.push({ reason: "effective_policy_not_yet_effective", field: "buildPolicy.effectiveAt" });
  }
  return refusals;
}

function idempotencyKey(scope: string, bytes: string): string {
  return `opl-publish-${scope}-${createHash("sha256").update(bytes).digest("hex").slice(0, 32)}`;
}

function canonical(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, v]) => v !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonical(v)}`).join(",")}}`;
}

type Session = { csrfToken?: string; actorId?: string };

async function readSession(fetchImpl: typeof fetch, base: string, cookie: string): Promise<{ session: Session; cookie: string }> {
  const response = await fetchImpl(`${base}/api/v2/auth/session`, { headers: { cookie } });
  const setCookies = typeof response.headers.getSetCookie === "function" ? response.headers.getSetCookie() : [];
  const merged = mergeCookies(cookie, setCookies);
  if (!response.ok) return { session: {}, cookie: merged };
  const body = (await response.json()) as Session;
  return { session: body, cookie: merged };
}

function mergeCookies(current: string, setCookies: string[]): string {
  const jar = new Map<string, string>();
  for (const part of current.split(";")) {
    const trimmed = part.trim();
    const index = trimmed.indexOf("=");
    if (index > 0) jar.set(trimmed.slice(0, index), trimmed.slice(index + 1));
  }
  for (const raw of setCookies) {
    const pair = raw.split(";")[0]?.trim() ?? "";
    const index = pair.indexOf("=");
    if (index > 0) jar.set(pair.slice(0, index), pair.slice(index + 1));
  }
  return [...jar.entries()].map(([k, v]) => `${k}=${v}`).join("; ");
}

async function login(
  fetchImpl: typeof fetch,
  base: string,
  email: string,
  password: string
): Promise<{ csrfToken: string; cookie: string }> {
  const initial = await readSession(fetchImpl, base, "");
  const csrf = initial.session.csrfToken ?? "";
  const response = await fetchImpl(`${base}/api/v2/auth/login`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-csrf-token": csrf,
      origin: new URL(base).origin,
      cookie: initial.cookie
    },
    body: JSON.stringify({ username: email, password })
  });
  const setCookies = typeof response.headers.getSetCookie === "function" ? response.headers.getSetCookie() : [];
  const cookie = mergeCookies(initial.cookie, setCookies);
  if (!response.ok) throw new Error(`console login failed with status ${response.status}`);
  const session = (await response.json()) as Session;
  if (!nonEmptyString(session.csrfToken)) throw new Error("console login returned no CSRF token");
  return { csrfToken: session.csrfToken, cookie };
}

async function writeJson(
  fetchImpl: typeof fetch,
  url: string,
  method: "POST" | "PUT",
  body: unknown,
  headers: Record<string, string>
): Promise<unknown> {
  const response = await fetchImpl(url, {
    method,
    headers: { "content-type": "application/json", ...headers },
    body: JSON.stringify(body)
  });
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`${method} ${new URL(url).pathname} failed with status ${response.status}: ${text.slice(0, 400)}`);
  }
  if (!text.trim()) return {};
  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

/**
 * Publishes the two catalog entries and then the effective policy. The policy
 * is written only after both entries are registered, and the readback decides
 * whether the publication counts as done.
 */
export async function publishRuntimePolicy(options: PublishOptions): Promise<PublishResult> {
  const refusals: Refusal[] = [
    ...contractRefusals(options.runtimeContract, "runtime"),
    ...contractRefusals(options.webuiContract, "webui")
  ];
  if (!nonEmptyString(options.runtimeAdmissionReceiptId)) {
    refusals.push({ reason: "runtime_admission_receipt_missing", field: "runtimeAdmissionReceiptId" });
  }
  if (!nonEmptyString(options.webuiAdmissionReceiptId)) {
    refusals.push({ reason: "webui_admission_receipt_missing", field: "webuiAdmissionReceiptId" });
  }
  // The WebUI declares the Runtime ABIs it can run against, and the Runtime
  // declares the ABI it speaks. A pair that does not agree would publish a
  // default the Publisher page then builds against and cannot serve.
  const runtimeAbi = String(options.runtimeContract?.runtimeAbiVersion ?? "");
  const webuiAbis = options.webuiContract?.runtimeAbiVersions;
  if (runtimeAbi && Array.isArray(webuiAbis) && !webuiAbis.includes(runtimeAbi)) {
    refusals.push({ reason: "webui_does_not_declare_runtime_abi", field: "publisherContract.runtimeAbiVersions" });
  }
  if (refusals.length > 0) return { published: false, refusals };

  const fetchImpl = options.fetchImpl ?? fetch;
  const base = options.base.replace(/\/+$/, "");
  const { csrfToken, cookie } = await login(fetchImpl, base, options.email, options.password);
  const headers = {
    "x-csrf-token": csrfToken,
    origin: new URL(base).origin,
    cookie
  };

  const runtimeRequest = buildRuntimeRegistrationRequest(
    options.runtimeContract,
    options.runtimeAdmissionReceiptId,
    options.runtimeName,
    options.runtimeVersionLabel
  );
  const webuiRequest = buildWebuiRegistrationRequest(
    options.webuiContract,
    options.webuiAdmissionReceiptId,
    options.webuiName,
    options.webuiVersionLabel
  );

  const runtime = (await writeJson(
    fetchImpl,
    `${base}/api/v2/admin/catalog/runtime-versions`,
    "POST",
    runtimeRequest,
    { ...headers, "idempotency-key": idempotencyKey("runtime", canonical(runtimeRequest)) }
  )) as { id?: string };
  if (!nonEmptyString(runtime.id)) {
    return { published: false, refusals: [{ reason: "runtime_registration_returned_no_id", field: "runtimeVersion.id" }] };
  }

  const webui = (await writeJson(
    fetchImpl,
    `${base}/api/v2/admin/catalog/webui-versions`,
    "POST",
    webuiRequest,
    { ...headers, "idempotency-key": idempotencyKey("webui", canonical(webuiRequest)) }
  )) as { id?: string };
  if (!nonEmptyString(webui.id)) {
    return {
      published: false,
      runtimeVersionId: runtime.id,
      refusals: [{ reason: "webui_registration_returned_no_id", field: "webuiVersion.id" }]
    };
  }

  const policy = await writeJson(
    fetchImpl,
    `${base}/api/v2/admin/catalog/build-policy`,
    "PUT",
    buildPolicyRequest(runtime.id, webui.id),
    { ...headers, "idempotency-key": idempotencyKey("policy", `${runtime.id}/${webui.id}`) }
  );

  const readbackResponse = await fetchImpl(`${base}/api/v2/catalog/build-policy`, { headers: { cookie } });
  if (!readbackResponse.ok) {
    return {
      published: false,
      runtimeVersionId: runtime.id,
      webuiVersionId: webui.id,
      refusals: [{ reason: "effective_policy_readback_failed", field: "buildPolicy" }]
    };
  }
  const readback = (await readbackResponse.json()) as EffectivePolicy;
  const verification = verifyEffectivePolicy(readback, {
    runtimeVersionId: runtime.id,
    webuiVersionId: webui.id
  });
  if (verification.length > 0) {
    return { published: false, runtimeVersionId: runtime.id, webuiVersionId: webui.id, refusals: verification };
  }
  return {
    published: true,
    runtimeVersionId: runtime.id,
    webuiVersionId: webui.id,
    policyVersion: readback.policyVersion ?? String((policy as { policyVersion?: string })?.policyVersion ?? ""),
    refusals: []
  };
}

export type PublishReceipt = {
  schema: "opl.cloud.catalog-policy-publication.v1";
  publishedAt: string;
  base: string;
  runtimeVersionId?: string;
  webuiVersionId?: string;
  policyVersion?: string;
  runtimeContractDigest: string;
  webuiContractDigest: string;
  refusals: Refusal[];
};

export function buildReceipt(result: PublishResult, base: string, runtimeContract: unknown, webuiContract: unknown): PublishReceipt {
  return {
    schema: "opl.cloud.catalog-policy-publication.v1",
    publishedAt: new Date().toISOString(),
    base,
    runtimeVersionId: result.runtimeVersionId,
    webuiVersionId: result.webuiVersionId,
    policyVersion: result.policyVersion,
    runtimeContractDigest: `sha256:${createHash("sha256").update(canonical(runtimeContract)).digest("hex")}`,
    webuiContractDigest: `sha256:${createHash("sha256").update(canonical(webuiContract)).digest("hex")}`,
    refusals: result.refusals
  };
}

function parseArgs(argv: string[]): Record<string, string> {
  const args: Record<string, string> = {};
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith("--")) continue;
    const key = token.slice(2);
    const value = argv[index + 1];
    if (value === undefined || value.startsWith("--")) throw new Error(`argument --${key} needs a value`);
    args[key] = value;
    index += 1;
  }
  return args;
}

async function readJsonFile(path: string): Promise<PublisherContract> {
  const raw = await readFile(path, "utf8");
  const parsed = JSON.parse(raw) as unknown;
  if (!isRecord(parsed)) throw new Error(`${path} does not contain a JSON object`);
  return parsed;
}

export async function main(argv: string[], env: Record<string, string | undefined>): Promise<number> {
  const args = parseArgs(argv);
  const base = args.base ?? env.OPL_CLOUD_BASE_URL ?? "";
  if (!base) {
    process.stderr.write("publish-runtime-policy: --base or OPL_CLOUD_BASE_URL is required\n");
    return 2;
  }
  if (!args.runtime || !args.webui) {
    process.stderr.write("publish-runtime-policy: --runtime and --webui are required\n");
    return 2;
  }
  const email = env.OPL_CATALOG_ADMIN_EMAIL ?? "";
  const password = env.OPL_CATALOG_ADMIN_PASSWORD ?? "";
  if (!email || !password) {
    process.stderr.write("publish-runtime-policy: OPL_CATALOG_ADMIN_EMAIL and OPL_CATALOG_ADMIN_PASSWORD are required\n");
    return 2;
  }

  let runtimeContract: PublisherContract;
  let webuiContract: PublisherContract;
  try {
    runtimeContract = await readJsonFile(args.runtime);
    webuiContract = await readJsonFile(args.webui);
  } catch (error) {
    process.stderr.write(`publish-runtime-policy: ${String(error)}\n`);
    return 2;
  }

  const runtimeAdmissionReceiptId = args["runtime-receipt"] ?? String(runtimeContract.admissionReceiptId ?? "");
  const webuiAdmissionReceiptId = args["webui-receipt"] ?? String(webuiContract.admissionReceiptId ?? "");

  const result = await publishRuntimePolicy({
    base,
    runtimeContract,
    webuiContract,
    runtimeAdmissionReceiptId,
    webuiAdmissionReceiptId,
    runtimeName: args["runtime-name"] ?? String(runtimeContract.name ?? "runtime"),
    runtimeVersionLabel: args["runtime-version"] ?? String(runtimeContract.versionLabel ?? "v1"),
    webuiName: args["webui-name"] ?? String(webuiContract.name ?? "webui"),
    webuiVersionLabel: args["webui-version"] ?? String(webuiContract.versionLabel ?? "v1"),
    email,
    password
  });

  const receipt = buildReceipt(result, base, runtimeContract, webuiContract);
  if (args.receipt) {
    await writeFile(args.receipt, `${JSON.stringify(receipt, null, 2)}\n`, "utf8");
  }
  process.stdout.write(`${JSON.stringify(receipt, null, 2)}\n`);
  if (!result.published) {
    process.stderr.write(
      `publish-runtime-policy: refused: ${result.refusals.map((r) => `${r.field}:${r.reason}`).join(", ")}\n`
    );
    return 1;
  }
  return 0;
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main(process.argv.slice(2), process.env).then(
    (code) => {
      process.exitCode = code;
    },
    (error: unknown) => {
      process.stderr.write(`publish-runtime-policy: ${String(error)}\n`);
      process.exitCode = 1;
    }
  );
}
