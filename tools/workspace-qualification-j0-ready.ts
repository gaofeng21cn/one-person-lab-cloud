/*
 * The Cloud fabric J0 READY producer.
 *
 * The protected business qualification lane refuses to open a provider scope
 * without a J0 READY receipt for the exact Candidate. The repository owner
 * decided on 2026-10-18 that Cloud fabric produces that receipt, from a real
 * sandbox qualification run, instead of receiving it from outside the protected
 * job.
 *
 * This module is that producer. It reads the exact Candidate receipt and a
 * declared sandbox, drives the existing fabric provider-reachability
 * qualification (`runTencentTkeSandboxQualification` in the `tencent-tke`
 * adapter module) through the same Fabric/Instance boundary the protected lane
 * uses, and derives the shared
 * `opl.workspace-qualification.j0-ready.v2` receipt from what the boundary
 * answered. It does not purchase, read a business fact, or assert a result the
 * run did not produce: `status`, `failed`, `skipped` and `confirmed` come from
 * the observed checks, and a READY receipt is re-validated with the contract's
 * own validator (including its Candidate binding and byte cap) before it is
 * written.
 *
 * The receipt is a file, because the producer and the protected consumer run in
 * separate jobs on separate runners and the Cloud workflow still delivers
 * protected inputs as files under `$RUNNER_TEMP`. The producer creates it with
 * the same hardening the consumer requires: absolute path outside the
 * repository, same-descriptor creation (`O_CREAT | O_EXCL | O_NOFOLLOW`) at mode
 * 0600, the contract's byte cap enforced before writing, and the mode and size
 * re-read from the descriptor that wrote it. It never overwrites a receipt.
 *
 * The boundary module is not implemented here. The fabric sandbox runner
 * supplies one module exporting `createTencentTkeProviderBoundary`, exactly as
 * the protected lane does, so the same provider code answers both lanes. A
 * missing Candidate, sandbox declaration or boundary module is an external
 * prerequisite and no receipt is written as READY for it.
 *
 * Usage:
 *   node tools/workspace-qualification-j0-ready.ts run \
 *     --candidate <candidate.json> --candidate-digest <sha256:...> \
 *     --sandbox <sandbox.json> --boundary <boundary.mjs> \
 *     --output <absolute path outside the repository>
 */

import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { mkdir, open, readFile } from "node:fs/promises";
import { basename, dirname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { canonicalJson, candidateReceiptDigest, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";
import { loadProtectedProviderBoundary } from "./protected-business-qualification.ts";
import {
  QUALIFICATION_J0_READY_KIND,
  QUALIFICATION_J0_READY_MAX_BYTES,
  createJ0ReadyReceiptValidator,
  hasExactKeys,
  qualificationProviderProfile,
} from "./workspace-qualification-contract.ts";
import {
  TENCENT_TKE_PROVIDER,
  runTencentTkeSandboxQualification,
  tencentTkeDeployment,
} from "./workspace-qualification-tencent-tke.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// The only authority class this producer may emit. A sandbox qualification is
// the point of the producer; a caller that wants another class needs another
// producer and another decision, not a relabelled receipt.
export const FABRIC_J0_SANDBOX_AUTHORITY_CLASS = "sandbox";

// The declaration a fabric sandbox run is bound to. The class and the dedicated
// flag describe how the sandbox is provisioned, so they are declared by the
// environment that provisions it and refused here when they say anything else.
export const FABRIC_J0_SANDBOX_DECLARATION_KIND = "opl.workspace-qualification.j0-sandbox.v1";

const DECLARATION_KEYS = ["schemaVersion", "authorityClass", "dedicated", "deployment"] as const;
const DEPLOYMENT_KEYS = [
  "deploymentMode",
  "fabricProvider",
  "candidateReceiptDigest",
  "clusterId",
  "region",
  "namespace",
  "ownerOrigin",
] as const;
const RUN_OPTIONS = [
  "--candidate",
  "--candidate-digest",
  "--sandbox",
  "--boundary",
  "--output",
] as const;

const DIGEST = /^sha256:[0-9a-f]{64}$/;
const EXTERNAL_PREREQUISITE_ABSENT = "external_prerequisite_absent";
const FABRIC_J0_READY_INVALID = "fabric_j0_ready_invalid";
const FABRIC_J0_READY_NOT_READY = "fabric_j0_ready_not_ready";
const FABRIC_J0_READY_FAILED = "fabric_j0_ready_failed";

export type FabricJ0ReadyEnvironment = Readonly<Record<string, string | undefined>>;

export type FabricJ0ReadyInput = Readonly<{
  candidatePath: string;
  candidateReceiptDigest: string;
  sandboxPath: string;
  boundaryModulePath: string;
  outputPath: string;
}>;

export type FabricJ0ReadyRun = Awaited<ReturnType<typeof runTencentTkeSandboxQualification>>;

export type FabricJ0ReadyResult = Readonly<{
  status: string;
  receipt: Record<string, any>;
  receiptDigest: string;
  receiptText: string;
  candidateReceiptDigest: string;
  run: FabricJ0ReadyRun;
}>;

function producerError(code: string, message: string): Error & { code: string } {
  const error = new Error(message) as Error & { code: string };
  error.code = code;
  return error;
}

function externalPrerequisiteAbsent(name: string): never {
  throw producerError(EXTERNAL_PREREQUISITE_ABSENT, `external prerequisite absent: ${name}`);
}

function invalid(message: string): never {
  throw producerError(FABRIC_J0_READY_INVALID, message);
}

function insideRepository(path: string, repositoryRoot: string): boolean {
  const rel = relative(repositoryRoot, path);
  return rel === "" || (rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel));
}

async function readRequiredJson(path: string, label: string): Promise<unknown> {
  const normalized = String(path || "").trim();
  if (!normalized) externalPrerequisiteAbsent(label);
  let text;
  try {
    text = await readFile(normalized, "utf8");
  } catch {
    externalPrerequisiteAbsent(label);
  }
  try {
    return JSON.parse(text);
  } catch {
    invalid(`${label} is not valid JSON`);
  }
}

/*
 * The exact Candidate the receipt will be bound to.
 *
 * The Candidate's own validator and digest function define the identity, and the
 * declared digest is checked against that derived value rather than trusted, so
 * a receipt cannot be produced for a bundle other than the one named.
 */
export async function readFabricCandidate(path: string, declaredDigest: string) {
  const normalizedDigest = String(declaredDigest || "").trim();
  if (!DIGEST.test(normalizedDigest)) invalid("declared Candidate receipt digest is invalid");
  const value = await readRequiredJson(path, "Candidate receipt");
  let candidate;
  try {
    candidate = validateCloudCandidateReceipt(value) as Record<string, any>;
  } catch {
    invalid("Candidate receipt is invalid");
  }
  const digest = candidateReceiptDigest(candidate);
  if (digest !== normalizedDigest) invalid("Candidate receipt digest does not match the declared digest");
  return Object.freeze({ candidate, digest });
}

/*
 * The declared sandbox this run qualifies.
 *
 * The deployment half is the same seven-field binding the protected lane uses,
 * validated by the provider module's own normalizer, so the two lanes cannot
 * disagree about what a TKE deployment is. The declaration half is what makes
 * the emitted authority checkable: a sandbox declaration that says another
 * class or is not dedicated is refused before a provider is contacted.
 */
export async function readFabricSandboxDeclaration(path: string, candidateDigest: string) {
  const value = await readRequiredJson(path, "fabric sandbox declaration");
  if (!hasExactKeys(value, [...DECLARATION_KEYS])) invalid("fabric sandbox declaration has an invalid shape");
  const declaration = value as Record<string, any>;
  if (declaration.schemaVersion !== 1 ||
      declaration.authorityClass !== FABRIC_J0_SANDBOX_AUTHORITY_CLASS ||
      declaration.dedicated !== true) {
    invalid("fabric sandbox declaration must be a dedicated sandbox");
  }
  if (!hasExactKeys(declaration.deployment, [...DEPLOYMENT_KEYS])) {
    invalid("fabric sandbox deployment binding has an invalid shape");
  }
  let deployment;
  try {
    deployment = tencentTkeDeployment(declaration.deployment);
  } catch {
    invalid("fabric sandbox deployment binding is invalid");
  }
  if (deployment.candidateReceiptDigest !== candidateDigest) {
    invalid("fabric sandbox deployment binding names another Candidate");
  }
  return Object.freeze({
    candidateReceiptDigest: deployment.candidateReceiptDigest,
    authorityClass: FABRIC_J0_SANDBOX_AUTHORITY_CLASS as "sandbox",
    dedicated: true as const,
    deployment,
  });
}

/*
 * One operation identity per Candidate and sandbox.
 *
 * Derived rather than persisted, so a redelivery of the same qualification
 * addresses the same provider operation and a different Candidate or sandbox
 * cannot reuse it. It names the qualification, not the cluster or the namespace
 * that ran it, so a provider log of the key carries no private identifier.
 */
export function fabricSandboxOperationKey(candidateDigest: string, deployment: { clusterId: string; namespace: string }) {
  const digest = createHash("sha256").update(canonicalJson({
    candidateReceiptDigest: String(candidateDigest || ""),
    provider: TENCENT_TKE_PROVIDER,
    clusterId: String(deployment?.clusterId || ""),
    namespace: String(deployment?.namespace || ""),
  })).digest("hex");
  return `opl-j0-sandbox:${digest}`;
}

/*
 * The shared v2 receipt, derived from the sandbox run.
 *
 * `status` is READY only when the run evaluated every check and none failed.
 * `confirmed` records the narrower fact the provider itself answered for: the
 * scope opened, its transport belongs to this deployment, and the scope was
 * released. `failed` and `skipped` are the run's own counts; nothing here can
 * lower them. The identity is re-read from the Candidate's own bytes here, and
 * the declared sandbox must still name that Candidate, so the receipt cannot be
 * built for one bundle from another's declaration.
 */
export function fabricJ0ReadyReceipt({
  candidate,
  sandbox,
  run,
}: {
  candidate: Record<string, any>;
  sandbox: Awaited<ReturnType<typeof readFabricSandboxDeclaration>>;
  run: FabricJ0ReadyRun;
}) {
  const admitted = validateCloudCandidateReceipt(candidate) as Record<string, any>;
  if (sandbox?.candidateReceiptDigest !== candidateReceiptDigest(admitted)) {
    invalid("the declared sandbox names another Candidate");
  }
  const failed = run?.failed;
  const skipped = run?.skipped;
  if (!Number.isSafeInteger(failed) || failed < 0 || !Number.isSafeInteger(skipped) || skipped < 0) {
    invalid("sandbox qualification result is invalid");
  }
  const confirmed = run.checks?.scope_opened === true && run.checks?.transport_bound === true &&
    run.checks?.scope_released === true;
  const ready = confirmed && run.checks?.provider_healthy === true && failed === 0 && skipped === 0;
  return {
    schemaVersion: 1,
    kind: QUALIFICATION_J0_READY_KIND,
    status: ready ? "READY" : "BLOCKED",
    source: {
      sha: admitted.product.sha,
      tree: admitted.product.tree,
      cloudImageDigest: admitted.cloudImage.indexDigest,
      clean: true,
    },
    authority: {
      mode: "live",
      class: sandbox.authorityClass,
      dedicated: sandbox.dedicated,
      confirmed,
    },
    provider: TENCENT_TKE_PROVIDER,
    failed,
    skipped,
  };
}

/*
 * Write the receipt with the consumer's own file standard.
 *
 * Same-descriptor creation and write, so the path cannot be swapped between the
 * check and the bytes: the file is created mode 0600 with `O_EXCL` (no
 * overwrite) and `O_NOFOLLOW` (no symlink target), written through that
 * descriptor, and then re-read from it to confirm the mode and size that will be
 * on disk. The byte cap is the contract's, enforced before a byte is written.
 */
export async function writeFabricJ0ReadyReceipt(
  path: string,
  receipt: Record<string, any>,
  { repositoryRoot = root }: { repositoryRoot?: string } = {},
) {
  const normalized = String(path || "").trim();
  if (!normalized) externalPrerequisiteAbsent("J0 READY receipt output path");
  if (!isAbsolute(normalized)) invalid("J0 READY receipt output path must be absolute");
  if (insideRepository(normalized, repositoryRoot)) invalid("J0 READY receipt must be outside the repository");
  const text = `${JSON.stringify(receipt, null, 2)}\n`;
  const bytes = Buffer.byteLength(text);
  if (bytes === 0 || bytes > QUALIFICATION_J0_READY_MAX_BYTES) {
    invalid("J0 READY receipt exceeds the byte cap");
  }
  await mkdir(dirname(normalized), { recursive: true, mode: 0o700 });
  let handle;
  try {
    handle = await open(
      normalized,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | (constants.O_NOFOLLOW ?? 0),
      0o600,
    );
  } catch {
    invalid("J0 READY receipt output already exists or cannot be created");
  }
  try {
    await handle.writeFile(text, "utf8");
    await handle.sync();
    const info = await handle.stat();
    if (!info.isFile() || (info.mode & 0o777) !== 0o600 || info.size !== bytes) {
      invalid("J0 READY receipt was not written with the required mode and size");
    }
  } finally {
    await handle.close();
  }
  return text;
}

/*
 * Run one fabric sandbox qualification and produce its J0 READY receipt.
 *
 * The order is deliberate: Candidate, sandbox declaration, boundary, provider
 * reachability, receipt self-check, then the file. No provider is contacted
 * before the Candidate and the declared sandbox are validated, and a READY
 * receipt is refused unless it satisfies the contract's own validator with the
 * Candidate binding it was derived from. A non-READY run still writes its
 * BLOCKED receipt so the operator can read what the provider answered; the
 * consumer refuses it and no protected scope opens.
 */
export async function runFabricJ0Ready(
  input: FabricJ0ReadyInput,
  dependencies: {
    boundary?: Awaited<ReturnType<typeof loadProtectedProviderBoundary>>;
    environment?: FabricJ0ReadyEnvironment;
  } = {},
): Promise<FabricJ0ReadyResult> {
  const environment = dependencies.environment ?? process.env;
  const candidate = await readFabricCandidate(input.candidatePath, input.candidateReceiptDigest);
  const sandbox = await readFabricSandboxDeclaration(input.sandboxPath, candidate.digest);
  const boundary = dependencies.boundary ?? await loadProtectedProviderBoundary(input.boundaryModulePath, {
    deployment: sandbox.deployment,
    environment,
  });
  const request = Object.freeze({
    kind: FABRIC_J0_SANDBOX_DECLARATION_KIND,
    candidate: Object.freeze({
      receiptDigest: candidate.digest,
      productSha: candidate.candidate.product.sha,
      productTree: candidate.candidate.product.tree,
      cloudImageDigest: candidate.candidate.cloudImage.indexDigest,
    }),
    authority: Object.freeze({ mode: "live", class: sandbox.authorityClass, dedicated: sandbox.dedicated }),
  });
  let run;
  try {
    run = await runTencentTkeSandboxQualification({
      deployment: sandbox.deployment,
      boundary,
      request,
      operationKey: fabricSandboxOperationKey(candidate.digest, sandbox.deployment),
    });
  } catch (error) {
    throw producerError(FABRIC_J0_READY_FAILED, `fabric sandbox qualification could not run: ${String((error as any)?.code || "unknown")}`);
  }
  const receipt = fabricJ0ReadyReceipt({ candidate: candidate.candidate, sandbox, run });
  if (receipt.status === "READY") {
    try {
      createJ0ReadyReceiptValidator(qualificationProviderProfile(TENCENT_TKE_PROVIDER))(
        receipt,
        candidate.candidate.product.sha,
        candidate.candidate.product.tree,
        {
          productSha: candidate.candidate.product.sha,
          productTree: candidate.candidate.product.tree,
          cloudImageDigest: candidate.candidate.cloudImage.indexDigest,
        },
      );
    } catch {
      throw producerError(FABRIC_J0_READY_FAILED, "fabric produced a READY receipt the contract does not accept");
    }
  }
  const receiptText = await writeFabricJ0ReadyReceipt(input.outputPath, receipt);
  return Object.freeze({
    status: String(receipt.status),
    receipt,
    receiptDigest: `sha256:${createHash("sha256").update(receiptText).digest("hex")}`,
    receiptText,
    candidateReceiptDigest: candidate.digest,
    run,
  });
}

export type FabricJ0ReadySummary = Readonly<{
  status: string;
  provider: string;
  authorityClass: string;
  failed: number;
  skipped: number;
  candidateReceiptDigest: string;
  receiptDigest: string;
  artifact: string;
}>;

export function fabricJ0ReadySummary(result: FabricJ0ReadyResult, outputPath: string): FabricJ0ReadySummary {
  return Object.freeze({
    status: result.status,
    provider: String(result.receipt.provider),
    authorityClass: String(result.receipt.authority.class),
    failed: result.receipt.failed,
    skipped: result.receipt.skipped,
    candidateReceiptDigest: result.candidateReceiptDigest,
    receiptDigest: result.receiptDigest,
    artifact: basename(String(outputPath || "")),
  });
}

export function parseFabricJ0ReadyArgs(args: readonly string[]): FabricJ0ReadyInput {
  if (args[0] !== "run") invalid("expected the run command");
  const options = new Map<string, string>();
  for (let index = 1; index < args.length; index += 2) {
    const name = args[index];
    const value = args[index + 1];
    if (!(RUN_OPTIONS as readonly string[]).includes(name) || options.has(name) ||
        typeof value !== "string" || value.trim() === "") {
      invalid("fabric J0 READY arguments are invalid");
    }
    options.set(name, value);
  }
  for (const name of RUN_OPTIONS) {
    if (!options.has(name)) invalid(`fabric J0 READY requires ${name}`);
  }
  return Object.freeze({
    candidatePath: options.get("--candidate") as string,
    candidateReceiptDigest: options.get("--candidate-digest") as string,
    sandboxPath: options.get("--sandbox") as string,
    boundaryModulePath: options.get("--boundary") as string,
    outputPath: options.get("--output") as string,
  });
}

export async function main(
  args: readonly string[] = process.argv.slice(2),
  environment: FabricJ0ReadyEnvironment = process.env,
) {
  const input = parseFabricJ0ReadyArgs(args);
  const result = await runFabricJ0Ready(input, { environment });
  const summary = fabricJ0ReadySummary(result, input.outputPath);
  process.stdout.write(`${JSON.stringify(summary)}\n`);
  if (result.status !== "READY") {
    throw producerError(
      FABRIC_J0_READY_NOT_READY,
      `fabric sandbox qualification is not ready: ${result.receipt.failed} failed, ${result.receipt.skipped} skipped`,
    );
  }
  return summary;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error: unknown) => {
    const code = String((error as { code?: string } | null)?.code || FABRIC_J0_READY_FAILED);
    const message = String((error as { message?: string } | null)?.message || "fabric J0 READY producer failed");
    process.stderr.write(`${code}: ${message}\n`);
    process.exitCode = 1;
  });
}
