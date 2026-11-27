import { execFileSync, spawn } from "node:child_process";
import { chmod, mkdtemp, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parse as parseYAML } from "yaml";
import { generationInputs, generatedOutputs } from "./verify-generated-contracts.ts";
import { openAPIInputs, openAPIOutputs } from "./verify-openapi-codegen.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// The published compose file is where the product's PostgreSQL image is
// declared. The root compose.yaml still declares the same image for the
// legacy installation, but the published file is the one a customer installs
// and the one the distribution contract names as the entrypoint, so the
// development gate reads the fact from the published file. The root file
// stays until the legacy compose files are retired.
//
// The path is exported because it is a declaration, not an implementation
// detail: the test asserts it literally, so re-pointing the fact source
// fails a check instead of silently reading another file that happens to
// declare the same image.
export const PUBLISHED_COMPOSE = "deploy/portable/compose.cloud.yaml";

function composePostgresImage() {
  const compose = parseYAML(readFileSync(join(root, PUBLISHED_COMPOSE), "utf8"));
  const image = String(compose?.services?.postgres?.image || "").trim();
  if (!/^postgres:[^\s@]+@sha256:[0-9a-f]{64}$/.test(image)) {
    throw new Error(`${PUBLISHED_COMPOSE} services.postgres.image must be an exact postgres tag@sha256 reference`);
  }
  return image;
}

export const postgresImage = composePostgresImage();

export const goModules = Object.freeze([
  "packages/contracts/go",
  "services/control-plane",
  "services/fabric",
  "services/ledger",
  "services/internal/postgresmigrate",
  "services/internal/ownerstore",
  "services/internal/ownerservice",
  "services/internal/tkeapply",
  "services/capability",
  "services/gateway-integration",
  "services/build",
  "services/runtime-control",
  "services/workspace",
  "services/resource-catalog",
  "services/serve",
  "services/cloud-composition",
  "apps/console-bff"
]);

/**
 * External capabilities this gate does not provision. A test leaves a
 * database-free lane only when it declares the requirement itself: a skip whose
 * reason carries one of these markers, or a failure carrying the
 * fixture-absent message the owning test helper emits when the capability is
 * missing. Nothing is filtered by name, so a new money or business test runs in
 * the lane by default. Anything that is neither executed nor self-declared
 * fails the gate instead of disappearing.
 */
export const databaseFreeCapabilityMarkers = Object.freeze([
  { capability: "isolated-owner-postgres", marker: "PostgreSQL test gate is not configured" },
  { capability: "isolated-owner-postgres", marker: "is required for PostgreSQL tests" },
  { capability: "isolated-owner-postgres", marker: "set OPL_OWNER_MIGRATION_TEST_ADMIN_DSN to an isolated PostgreSQL administrator DSN" },
  { capability: "isolated-data-scale", marker: "set OPL_CAPACITY_TESTS=1 to run the isolated 1000-user data-scale test" }
]);

/**
 * The money and business chain the control-plane database-free lane must always
 * account for. Each name must appear in that lane's own run as an executed
 * test, or as a test excluded by a declared capability marker. A name that
 * stops running, is renamed, or is filtered away fails the gate rather than
 * quietly reducing coverage.
 */
export const declaredMoneyChainTests = Object.freeze([
  "TestD1FinanceBusinessChain",
  "TestD1RefundReservationPostgres",
  "TestD1RenewalRejectsConflictingTransactionBeforeFulfillment",
  "TestD1RenewalRefundRequiresTheOriginalConfirmedDebit",
  "TestD1RenewalUnknownDebitWaitsForAuthoritativeHistoryAcrossRestart",
  "TestD4ExpiredWorkspaceRequiresExplicitOriginalRenewalAfterTopUp",
  "TestGatewayRevealAuditFailureDoesNotReturnKey",
  "TestGatewayRevealDoesNotLeakAcrossAccounts",
  "TestGatewayRevealFailsClosed"
]);

/**
 * Declared tests that must actually execute. Unlike declaredMoneyChainTests,
 * a capability marker may never excuse them: the Gateway reveal family runs
 * without a database, so "excluded" here means the lane stopped proving it.
 */
export const databaseFreeRequiredTests = Object.freeze([
  "TestGatewayRevealAuditFailureDoesNotReturnKey",
  "TestGatewayRevealDoesNotLeakAcrossAccounts",
  "TestGatewayRevealFailsClosed"
]);

export type DatabaseFreeGoTestSpec = {
  cwd: string;
  packages: string[];
  /** Tests this lane must always execute or explicitly exclude. */
  declaredTests?: readonly string[];
  /**
   * The lane must execute at least this many tests. A filter, rename or
   * truncated package list that shrinks the lane below the floor fails the gate
   * instead of passing as a smaller, unexplained run.
   */
  minimumExecutedTests?: number;
  /** Declared tests that must execute; a capability marker cannot exclude them. */
  requiredExecutedTests?: readonly string[];
};

export const databaseFreeGoTestSpecs: readonly DatabaseFreeGoTestSpec[] = Object.freeze([
  { cwd: "packages/contracts/go", packages: ["./..."] },
  { cwd: "services/control-plane", packages: ["./cmd/control-plane", "./internal/clients"] },
  // The customer settlement, refund, renewal and Gateway reconciliation chain
  // lives in internal/server, together with PostgreSQL-gated suites. The whole
  // package runs here: the gated suites declare their own capability and are
  // reported as excluded, and the declared money chain below proves it.
  // The executed-test floor is the broad-coverage proof this lane used to get
  // from a duplicated end-to-end meta test: it keeps a filtered or truncated
  // package run visible at the lane itself, without running the package twice.
  { cwd: "services/control-plane", packages: ["./internal/server"], declaredTests: declaredMoneyChainTests, minimumExecutedTests: 1001, requiredExecutedTests: databaseFreeRequiredTests },
  { cwd: "services/fabric", packages: ["./cmd/fabric", "./cmd/opl-tencent-provisioner", "./cmd/opl-node-image-retire", "./internal/http", "./coordination"] },
  { cwd: "services/ledger", packages: ["./cmd/ledger", "./internal/http"] },
  { cwd: "services/serve", packages: ["./cmd/server"] },
  // The composition entrypoint reads the owner topology contract and starts the
  // declared owner processes; its order, dependency and readiness rules hold
  // without a database.
  { cwd: "services/cloud-composition", packages: ["./..."] },
  { cwd: "services/internal/postgresmigrate", packages: ["./..."] },
  // The owner runtime's startup and Operation readback rules hold without a
  // database; its PostgreSQL-gated isolation proof declares the capability it
  // needs and is reported as excluded here instead of being name-filtered out.
  { cwd: "services/internal/protectedresource", packages: ["./..."] },
  { cwd: "services/internal/tkeapply", packages: ["./..."] },
  { cwd: "services/internal/ownerservice", packages: ["./..."] }
]);

export type VerificationStep = {
  name: string;
  command: string;
  args: string[];
  cwd?: string;
  evidence?: "node" | "go" | "go-coverage";
  /** Tests a database-free lane must execute or explicitly exclude. */
  declaredTests?: readonly string[];
  /** Minimum executed tests for a lane that also declares a coverage floor. */
  minimumExecutedTests?: number;
  /** Declared tests that must execute; a capability marker cannot exclude them. */
  requiredExecutedTests?: readonly string[];
};

export const verificationGroups = ["source", "browser", "go"] as const;
type VerificationGroup = (typeof verificationGroups)[number];
type LocalVerificationStep = VerificationStep & { group: VerificationGroup };
type VerificationOptions = {
  withPostgres?: boolean;
  focused?: boolean;
  base?: string;
  testTargets?: string[];
  group?: VerificationGroup;
  shard?: string;
};

export const localVerificationSteps: readonly LocalVerificationStep[] = Object.freeze([
  { group: "source", name: "product boundary", command: "npm", args: ["run", "validate:product-boundary"] },
  { group: "source", name: "Generated contracts freshness", command: "npm", args: ["run", "verify:generated-contracts"] },
  { group: "source", name: "OpenAPI codegen freshness", command: "npm", args: ["run", "verify:openapi"] },
  { group: "source", name: "Development tools typecheck", command: "npm", args: ["run", "typecheck:development-tools"] },
  { group: "source", name: "Node source tests", command: "npm", args: ["run", "test:source"] },
  // One bounded-concurrency invocation instead of nine sequential ones: each
  // group used to boot its own demo server and Chromium before the next started.
  // The named groups stay available for targeted runs.
  { group: "browser", name: "Console browser suite", command: "npm", args: ["run", "test:browser:suite"] },
  { group: "source", name: "TypeScript typecheck", command: "npm", args: ["run", "typecheck"] },
  { group: "source", name: "TypeScript lint", command: "npm", args: ["run", "lint"] },
  { group: "source", name: "Console build", command: "npm", args: ["run", "build"] },
  // The Control Plane retirement is proved by one instrument. It spans Go,
  // TypeScript, YAML and JSON, so it belongs to the source group: the Go
  // group cannot reach it.
  { group: "source", name: "Control Plane retirement residue", command: "node", args: ["tools/verify-no-control-plane.ts"] },
  ...goModules.map((cwd) =>
    ({ group: "go" as const, name: `${cwd} compile`, command: "go", args: ["test", "-run", "^$", "./..."], cwd })
  ),
  { group: "go", name: "Control Plane dead-code boundary", command: "go", args: ["run", "./internal/tools/deadcode"], cwd: "services/control-plane" },
  ...databaseFreeGoTestSpecs.map((spec) => ({
    group: "go" as const,
    name: `${spec.cwd} database-free tests`,
    command: "go",
    args: ["test", "-count=1", "-json", ...spec.packages],
    cwd: spec.cwd,
    evidence: "go-coverage" as const,
    ...(spec.declaredTests ? { declaredTests: spec.declaredTests } : {}),
    ...(spec.minimumExecutedTests ? { minimumExecutedTests: spec.minimumExecutedTests } : {}),
    ...(spec.requiredExecutedTests ? { requiredExecutedTests: spec.requiredExecutedTests } : {})
  })),
  { group: "source", name: "Git whitespace", command: "git", args: ["diff", "--check"] }
]);

const developmentToolPaths = ["tools/", "tests/tools/", "AGENTS.md", "DEV_GUIDE.md", "package.json", ".github/"];
const generatedContractPaths = ["packages/contracts/proto/", "packages/contracts/go/", "services/gateway-integration/identity/", "apps/console-bff/internal/httpapi/"];

function anyPath(paths: readonly string[], prefixes: readonly string[]) {
  return paths.some((path) => prefixes.some((prefix) => path === prefix || (prefix.endsWith("/") && path.startsWith(prefix))));
}

/** Select the smallest local evidence set for an explicit Git change scope. */
export function focusedVerificationSteps(changedPaths: readonly string[], testTargets: readonly string[] = [], mergeBase?: string): VerificationStep[] {
  const steps: VerificationStep[] = [];
  const add = (step: VerificationStep) => steps.push(step);
  const hasRepositoryCode = changedPaths.some((path) => !path.startsWith("docs/"));
  if (hasRepositoryCode) add({ name: "product boundary", command: "npm", args: ["run", "validate:product-boundary"] });
  if (anyPath(changedPaths, [...generatedContractPaths, ...generationInputs, ...generatedOutputs, "tools/verify-generated-contracts.ts"])) {
    add({ name: "Generated contracts freshness", command: "npm", args: ["run", "verify:generated-contracts"] });
  }
  if (anyPath(changedPaths, [...openAPIInputs, ...openAPIOutputs])) {
    add({ name: "OpenAPI codegen freshness", command: "npm", args: ["run", "verify:openapi"] });
  }
  if (anyPath(changedPaths, developmentToolPaths)) {
    add({ name: "Development tools typecheck", command: "npm", args: ["run", "typecheck:development-tools"] });
  }
  if (changedPaths.some((path) => /^(?:apps|packages)\/.*\.[cm]?[jt]sx?$/u.test(path))) {
    add({ name: "TypeScript typecheck", command: "npm", args: ["run", "typecheck"] });
  }
  const testPattern = /^tests\/(?:[\w.-]+\/)*[\w.-]+\.test\.[cm]?[jt]sx?$/u;
  for (const path of testTargets) {
    if (!testPattern.test(path) || path.split("/").includes("..")) throw new Error(`invalid focused test target: ${path}`);
    if (!existsSync(join(root, path))) throw new Error(`focused test target does not exist: ${path}`);
  }
  const testPaths = [...new Set([...changedPaths.filter(path => testPattern.test(path) && existsSync(join(root, path))), ...testTargets])].sort();
  if (changedPaths.some(path => /\.[cm]?[jt]sx?$/u.test(path)) && testPaths.length === 0) {
    throw new Error("focused source verification requires behavior targets; provide --test <owner-test-file> (no exhaustive fallback)");
  }
  const scripts = JSON.parse(readFileSync(join(root, "package.json"), "utf8")).scripts;
  const browserInventory = new Set<string>(scripts["test:browser:suite"].match(/tests\/[^\s"']+\.test\.[cm]?[jt]sx?/gu) ?? []);
  const browserTests = testPaths.filter(path => browserInventory.has(path));
  const nodeTests = testPaths.filter((path) => !browserTests.includes(path));
  if (nodeTests.length) add({ name: "Focused development tests", command: "node", args: ["--test", "--test-reporter=tap", ...nodeTests], evidence: "node" });
  if (browserTests.length) add({ name: "Focused E2E tests", command: "node", args: ["--test", "--test-reporter=tap", ...browserTests], evidence: "node" });
  const modules = [...new Set([...goModules, ...databaseFreeGoTestSpecs.map(spec => spec.cwd)])];
  for (const module of modules) {
    const paths = changedPaths.filter(path => path.startsWith(`${module}/`));
    if (!paths.length) continue;
    const packages = [...new Set(paths.filter(path => path.endsWith(".go"))
      .map(path => { const directory = relative(module, dirname(path)); return directory ? `./${directory}` : "."; }))].sort();
    // Module metadata and non-Go assets affect module-level tests. A database
    // requirement is a real boundary failure, never a reason to compile-only.
    // Entrypoint packages often have no tests. Select the module's behavior
    // suites in that case instead of treating a compile-only package as proof.
    const hasPackageTests = packages.some(target => {
      const directory = join(root, module, target);
      return existsSync(directory) && readdirSync(directory).some(name => name.endsWith("_test.go"));
    });
    const targets = paths.some(path => !path.endsWith(".go")) || !hasPackageTests ? ["./..."] : packages;
    add({ name: `${module} focused Go tests`, command: "go", args: ["test", "-count=1", "-json", ...targets], cwd: module, evidence: "go" });
  }
  if (mergeBase) {
    add({ name: "Git committed whitespace", command: "git", args: ["diff", "--check", mergeBase, "HEAD", "--"] });
    add({ name: "Git staged whitespace", command: "git", args: ["diff", "--cached", "--check", "--"] });
  }
  add({ name: "Git whitespace", command: "git", args: ["diff", "--check", ...(mergeBase ? ["--"] : [])] });
  return steps;
}

export const postgresVerificationSpecs = Object.freeze([
  { cwd: "services/internal/postgresmigrate", race: true },
  // Each owner proves, against a real PostgreSQL server, that its own migration
  // entrypoint installs its schema, refuses a differently named database, and leaves
  // its runtime unable to reach another owner's schema.
  { cwd: "services/internal/ownerservice" },
  { cwd: "services/capability" },
  { cwd: "services/gateway-integration" },
  { cwd: "services/build" },
  { cwd: "services/runtime-control" },
  { cwd: "services/workspace" },
  { cwd: "services/resource-catalog" },
  { cwd: "services/serve" },
  { cwd: "services/ledger" },
  { cwd: "services/control-plane", timeout: "15m" },
  // The application replacement scenario has a 15-minute overall budget;
  // leave time for the other Fabric tests and exact fixture cleanup as well.
  { cwd: "services/fabric", timeout: "20m" }
]);

/** Validate selectors for both the CLI and callers of runVerification. */
function validateVerificationOptions(options: VerificationOptions) {
  const { focused = false, withPostgres = false, base, testTargets = [], group, shard } = options;
  if (focused && withPostgres) throw new Error("cannot combine focused checks with the exhaustive PostgreSQL lane; supply the focused owner database environment explicitly");
  if (focused && !base) throw new Error("focused verification requires --base <ref>");
  if (!focused && base !== undefined) throw new Error("--base requires --focused");
  if (!focused && testTargets.length) throw new Error("--test requires --focused");
  if (group !== undefined && !verificationGroups.includes(group)) throw new Error("--group requires source, browser or go");
  if (group !== undefined && (focused || withPostgres)) throw new Error("--group cannot be combined with --focused or --with-postgres");
  if (shard !== undefined) {
    if (group !== "browser") throw new Error("--shard requires --group browser");
    const match = /^([1-9][0-9]*)\/([1-9][0-9]*)$/u.exec(shard);
    if (!match || !Number.isSafeInteger(Number(match[1])) || !Number.isSafeInteger(Number(match[2])) || Number(match[1]) > Number(match[2])) {
      throw new Error("--shard requires index/total with 1 <= index <= total");
    }
  }
}

export function parseVerifyLocalArgs(args = process.argv.slice(2)): VerificationOptions & { withPostgres: boolean } {
  let withPostgres = false;
  let focused = false;
  let base: string | undefined;
  let group: VerificationGroup | undefined;
  let shard: string | undefined;
  const testTargets: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    const token = args[index];
    if (token === "--with-postgres") {
      if (withPostgres) throw new Error("verify-local argument --with-postgres may be provided once");
      withPostgres = true;
      continue;
    }
    if (token === "--focused") {
      if (focused) throw new Error("verify-local argument --focused may be provided once");
      focused = true;
      continue;
    }
    if (["--test", "--base", "--group", "--shard"].includes(token)) {
      const value = args[++index];
      if (!value || value.startsWith("--")) throw new Error(`verify-local argument ${token} requires a value`);
      if (token === "--test") {
        if (testTargets.includes(value)) throw new Error(`duplicate focused test target: ${value}`);
        testTargets.push(value);
      } else if (token === "--base") {
        if (base !== undefined) throw new Error("verify-local argument --base may be provided once");
        base = value;
      } else if (token === "--group") {
        if (group !== undefined) throw new Error("verify-local argument --group may be provided once");
        group = value as VerificationGroup;
      } else {
        if (shard !== undefined) throw new Error("verify-local argument --shard may be provided once");
        shard = value;
      }
      continue;
    }
    throw new Error(`unknown verify-local argument: ${token}`);
  }
  const options = { withPostgres, ...(focused ? { focused: true } : {}), ...(base !== undefined ? { base } : {}),
    ...(testTargets.length ? { testTargets } : {}), ...(group !== undefined ? { group } : {}), ...(shard !== undefined ? { shard } : {}) };
  validateVerificationOptions(options);
  return options;
}



/** Only an isolated loopback PostgreSQL admin DSN is an admissible owner fixture. */
export function isLoopbackOwnerDatabaseDsn(value: unknown): boolean {
  if (typeof value !== "string" || !value || value.length > 512) return false;
  let parsed: URL;
  try { parsed = new URL(value); } catch { return false; }
  if (parsed.protocol !== "postgresql:" && parsed.protocol !== "postgres:") return false;
  if (!["127.0.0.1", "localhost", "[::1]", "::1"].includes(parsed.hostname)) return false;
  const port = Number(parsed.port);
  return /^[0-9]{1,5}$/u.test(parsed.port) && port > 0 && port <= 65535;
}

function developmentTAP(stdout: string, targets: string[], cwd: string) {
  // npm can print its fixed lifecycle banner before the final Node runner.
  // Only a line-start TAP header is eligible; test-console output is '#'-prefixed.
  const marker = stdout.lastIndexOf("\nTAP version 13\n");
  const tap = stdout.startsWith("TAP version 13\n") ? stdout : marker >= 0 ? stdout.slice(marker + 1) : "";
  // Require the runner's complete, terminal TAP footer, not arbitrary '# pass'
  // logging from a test (Node prefixes those logs with another '#').
  const footer = tap.trimEnd().match(/(?:^|\n)1\.\.(\d+)\n# tests (\d+)\n# suites (\d+)\n# pass (\d+)\n# fail (\d+)\n# cancelled (\d+)\n# skipped (\d+)\n# todo (\d+)\n# duration_ms [\d.]+$/);
  if (!footer || !tap.startsWith("TAP version 13\n")) {
    return { tests: 0, failed: 0, skipped: 0, reason: "missing complete Node TAP footer" };
  }
  const [, plan, count, suites, passed, failed, cancelled, skipped, todo] = footer.map(Number);
  const fileNames = new Set(targets.flatMap((target) => [target, resolve(cwd, target)]));
  // Node emits a synthetic file test when no node:test test was registered.
  // That exact file-envelope is not evidence of an executed source test.
  const emptyFiles = tap.split("\n").filter((line) => {
    const subtest = line.match(/^# Subtest: (.+)$/);
    return subtest && fileNames.has(subtest[1]);
  }).length;
  const tests = Math.max(0, count - emptyFiles);
  const results = tap.split("\n").filter((line) => /^(?:not )?ok \d+ - /.test(line));
  const validPlan = plan === results.length && count + suites >= plan && passed + failed + cancelled + skipped + todo === count;
  const reason = !validPlan ? "inconsistent Node TAP summary" : emptyFiles > 0 || tests === 0 ? "no registered Node tests executed for one or more selected targets" :
    failed + cancelled + skipped + todo > 0 ? "Node tests failed, cancelled, skipped or TODO" : undefined;
  return { tests, failed: failed + cancelled, skipped: skipped + todo, ...(reason ? { reason } : {}) };
}

function developmentGoJSON(stdout: string) {
  const running = new Set<string>(), terminal = new Map<string, string>();
  const packages = new Map<string, string>(), started = new Set<string>();
  let invalid = false, packageFailures = 0;
  for (const line of stdout.split(/\r?\n/).filter((line) => line.trim())) {
    try {
      const event = JSON.parse(line);
      if (!event || typeof event.Package !== "string" || !event.Package || typeof event.Action !== "string") { invalid = true; continue; }
      const id = typeof event.Test === "string" && event.Test ? `${event.Package}\0${event.Test}` : undefined;
      if (id && event.Action === "run") running.add(id);
      if (id && ["pass", "fail", "skip"].includes(event.Action)) {
        if (!running.has(id) || terminal.has(id)) invalid = true;
        terminal.set(id, event.Action);
      }
      if (!id && event.Action === "start") started.add(event.Package);
      if (!id && ["pass", "fail", "skip"].includes(event.Action)) {
        if (packages.has(event.Package)) invalid = true;
        packages.set(event.Package, event.Action);
        if (event.Action === "fail") packageFailures++;
      }
    } catch { invalid = true; }
  }
  const failed = [...terminal.values()].filter((value) => value === "fail").length + packageFailures;
  // Go emits package-level "skip" for compiled packages with no test files.
  // Only registered test skips are skipped behavior; the positive test count
  // below still refuses a stage that executed no tests at all.
  const skipped = [...terminal.values()].filter((value) => value === "skip").length;
  const tests = terminal.size;
  const complete = running.size === terminal.size && packages.size > 0 && [...started].every((name) => packages.has(name)) &&
    [...running].every((id) => packages.has(id.split("\0")[0]));
  const reason = invalid || !complete ? "invalid or incomplete Go JSON summary" : tests === 0 ? "no Go tests executed" :
    failed + skipped > 0 ? "Go tests failed or skipped" : undefined;
  return { tests, failed, skipped, ...(reason ? { reason } : {}) };
}

type ProcessResult = { code: number | null; signal: NodeJS.Signals | null; stdout: string; stderr: string };
type GoTestEvent = { Action?: string; Package?: string; Test?: string; Output?: string };
type GoTestSummary = { failed: number; skipped: number; passedPackages: string[]; passedTests: { package: string; name: string }[] };

function stepCwd(step: VerificationStep) {
  return step.cwd ? join(root, step.cwd) : root;
}

function stepEnv() {
  return process.env;
}

// Progress banners are diagnostics. They must never share the standard output
// stream: the development entry speaks JSON-RPC over stdio in serve mode, and
// a stray stdout line corrupts that protocol. Step results themselves are
// printed by their own callers, not here.
function printStep(name: string) {
  process.stderr.write(`\n==> ${name}\n`);
}

function runProcess(command: string, args: string[], { cwd = root, env = process.env, capture = false, allowFailure = false }: { cwd?: string; env?: NodeJS.ProcessEnv; capture?: boolean; allowFailure?: boolean } = {}): Promise<ProcessResult> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, {
      cwd,
      env,
      stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit"
    });
    let stdout = "";
    let stderr = "";
    if (capture) {
      child.stdout!.setEncoding("utf8");
      child.stderr!.setEncoding("utf8");
      child.stdout!.on("data", (chunk) => { stdout += chunk; });
      child.stderr!.on("data", (chunk) => { stderr += chunk; });
    }
    child.on("error", reject);
    child.on("close", (code, signal) => {
      if (code === 0 || allowFailure) {
        resolvePromise({ code, signal, stdout, stderr });
        return;
      }
      const detail = capture ? `\n${stderr || stdout}` : "";
      reject(new Error(`${command} ${args.join(" ")} failed with ${signal || code}${detail}`));
    });
  });
}

async function runStep(step: VerificationStep) {
  printStep(step.name);
  if (step.evidence === "go-coverage") {
    const coverage = await runDatabaseFreeGoLane(step, stepEnv());
    // Independent Go lanes may overlap, so the lane name is part of the header:
    // every report in the log stays attributable to exactly one step.
    process.stderr.write(`${step.name}: ${databaseFreeCoverageReport(coverage)}\n`);
    if (coverage.reason) throw new Error(`${step.name}: ${coverage.reason}`);
    return;
  }
  const result = await runProcess(step.command, step.args, { cwd: stepCwd(step), env: stepEnv(), capture: Boolean(step.evidence) });
  if (step.evidence) {
    process.stdout.write(result.stdout);
    process.stderr.write(result.stderr);
    const summary = step.evidence === "go" ? developmentGoJSON(result.stdout) : developmentTAP(result.stdout, step.args.filter(arg => arg.startsWith("tests/")), stepCwd(step));
    if (summary.reason) throw new Error(`${step.name}: ${summary.reason}`);
  }
}

function parseDockerPort(output: string) {
  for (const line of String(output).trim().split(/\r?\n/)) {
    const match = line.match(/:(\d+)$/);
    if (match) return match[1];
  }
  throw new Error(`could not parse PostgreSQL Docker port: ${String(output).trim()}`);
}

async function waitForHealthyPostgres(containerName: string) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    const result = await runProcess(
      "docker",
      ["inspect", "--format", "{{.State.Health.Status}}", containerName],
      { capture: true, allowFailure: true }
    );
    const status = result.stdout.trim();
    if (result.code === 0 && status === "healthy") return;
    if (result.code !== 0 || status === "unhealthy") {
      throw new Error(`temporary PostgreSQL container is ${status || "unavailable"}: ${result.stderr.trim()}`);
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 500));
  }
  throw new Error("temporary PostgreSQL did not become healthy within 60 seconds");
}

async function withTemporaryPostgres(callback: (env: NodeJS.ProcessEnv, fixture: { socketDirectory?: string }) => Promise<unknown>) {
  const containerName = `opl-cloud-verify-${process.pid}-${randomUUID().slice(0, 8)}`;
  // Linux stages run in a private network namespace, so the fixture is also
  // exposed as a Unix socket in a host-created directory. The macOS sandbox
  // shares the host loopback stack and keeps the published TCP port.
  const socketDirectory = process.platform === "linux"
    ? await realpath(await mkdtemp(join(tmpdir(), "opl-owner-db-sock-")))
    : undefined;
  if (socketDirectory) await chmod(socketDirectory, 0o777);
  let started = false;
  const stop = async () => {
    if (started) {
      await runProcess("docker", ["rm", "--force", containerName], { capture: true, allowFailure: true });
      started = false;
    }
    if (socketDirectory) await rm(socketDirectory, { recursive: true, force: true });
  };
  const interrupt = () => {
    void stop().finally(() => process.exit(130));
  };
  process.once("SIGINT", interrupt);
  process.once("SIGTERM", interrupt);
  try {
    printStep("temporary PostgreSQL 16");
    await runProcess("docker", [
      "run", "--detach", "--rm", "--name", containerName,
      "--env", "POSTGRES_HOST_AUTH_METHOD=trust",
      "--health-cmd", "pg_isready -U postgres -d postgres",
      "--health-interval", "1s",
      "--health-timeout", "5s",
      "--health-retries", "60",
      "--publish", "127.0.0.1::5432",
      ...(socketDirectory ? ["--volume", `${socketDirectory}:/var/run/postgresql`] : []),
      postgresImage
    ], { capture: true });
    started = true;
    await waitForHealthyPostgres(containerName);
    if (socketDirectory) await waitForOwnerSocket(socketDirectory);
    const portResult = await runProcess("docker", ["port", containerName, "5432/tcp"], { capture: true });
    const postgresEnv = {
      ...process.env,
      PGHOST: "127.0.0.1",
      PGPORT: parseDockerPort(portResult.stdout),
      PGUSER: "postgres",
      PGDATABASE: "postgres",
      PGSSLMODE: "disable",
      OPL_POSTGRES_TESTS: "1",
      // The owner isolation suites create roles and databases, which only an
      // administrative connection may do; the temporary container's superuser is the
      // one isolated authority for that.
      OPL_OWNER_MIGRATION_TEST_ADMIN_DSN: `postgresql://postgres@127.0.0.1:${parseDockerPort(portResult.stdout)}/postgres?sslmode=disable`,
      OPL_CAPACITY_TESTS: "1",
      OPL_FABRIC_LOCAL_DOCKER_INTEGRATION: "1"
    };
    return await callback(postgresEnv, { ...(socketDirectory ? { socketDirectory } : {}) });
  } finally {
    process.removeListener("SIGINT", interrupt);
    process.removeListener("SIGTERM", interrupt);
    await stop();
  }
}

async function waitForOwnerSocket(socketDirectory: string) {
  const socketPath = join(socketDirectory, ".s.PGSQL.5432");
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (existsSync(socketPath)) return;
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 200));
  }
  throw new Error(`temporary PostgreSQL did not publish its Unix socket in ${socketDirectory} within 30 seconds`);
}

/**
 * The host-owned isolated owner-database fixture for one acceptance stage: one
 * ephemeral PostgreSQL container from the pinned compose image, trust auth,
 * a loopback-only published port and, on Linux, a host-created Unix socket
 * directory. Only that endpoint pair is handed to the stage; it is never read
 * from the invoking environment.
 */
export async function withIsolatedOwnerDatabase<T>(
  callback: (fixture: { dsn: string; socketDirectory?: string }) => Promise<T>
): Promise<T> {
  return withTemporaryPostgres(async (env, fixture) => {
    const dsn = env.OPL_OWNER_MIGRATION_TEST_ADMIN_DSN;
    if (typeof dsn !== "string" || !isLoopbackOwnerDatabaseDsn(dsn))
      throw new Error("isolated owner database fixture did not report a loopback admin DSN");
    if (fixture.socketDirectory) {
      // The stage lives in a private network namespace, so the loopback TCP
      // port is unreachable from it; the socket in the host-created directory
      // crosses that boundary as a filesystem grant.
      // libpq's socket form: the authority stays parseable while the `host`
      // parameter carries the exact directory of the host-created socket.
      const socketDsn = `postgresql://postgres@localhost/postgres?host=${encodeURIComponent(fixture.socketDirectory)}&sslmode=disable`;
      return await callback({ dsn: socketDsn, socketDirectory: fixture.socketDirectory });
    }
    return await callback({ dsn });
  }) as Promise<T>;
}

export function summarizeGoTestFailures(events: readonly GoTestEvent[]) {
  const failedTests = new Map<string, { package: string; name: string }>();
  const failedPackages = new Set();
  for (const event of events) {
    if (event?.Action !== "fail" || typeof event.Package !== "string" || !event.Package) continue;
    if (typeof event.Test === "string" && event.Test) {
      failedTests.set(`${event.Package}\0${event.Test}`, { package: event.Package, name: event.Test });
    } else {
      failedPackages.add(event.Package);
    }
  }
  const values = [...failedTests.values()];
  const tests = values.filter((candidate) => !values.some((other) =>
    other.package === candidate.package && other.name.startsWith(`${candidate.name}/`)
  )).sort((left, right) => left.package.localeCompare(right.package) || left.name.localeCompare(right.name));
  return { tests, packages: [...failedPackages].sort() };
}

/** Parse one captured `go test -json` stream into its events, in order. */
export function parseGoTestEvents(stdout: string): GoTestEvent[] {
  const events: GoTestEvent[] = [];
  for (const line of stdout.split(/\r?\n/)) {
    if (!line.trim()) continue;
    try {
      const event = JSON.parse(line);
      if (event && typeof event === "object") events.push(event as GoTestEvent);
    } catch { events.push({ Output: line }); }
  }
  return events;
}

export type DatabaseFreeExclusion = { name: string; capability: string; reason: string };
export type DatabaseFreeCoverage = {
  executed: string[];
  excluded: DatabaseFreeExclusion[];
  failed: { name: string; output: string }[];
  undeclared: { name: string; reason: string }[];
  declared: { accounted: string[]; missing: string[] };
  reason?: string;
};

function firstReason(text: string) {
  for (const line of text.split(/\r?\n/)) {
    const match = line.match(/^\s+\S+\.go:\d+: (.*\S)\s*$/u);
    if (match) return match[1];
  }
  return "no reported reason";
}

/**
 * Classify one database-free lane run against the declared capability markers.
 * Executed tests, capability-excluded tests, real failures, undeclared skips
 * and unaccounted declared tests are all reported separately, so coverage that
 * shrinks — by rename, deletion or filtering — is visible instead of silent.
 */
export function databaseFreeCoverage(
  events: readonly GoTestEvent[],
  options: { markers?: readonly { capability: string; marker: string }[]; declared?: readonly string[]; minimumExecuted?: number; requiredExecuted?: readonly string[] } = {}
): DatabaseFreeCoverage {
  const markers = options.markers ?? databaseFreeCapabilityMarkers;
  const declared = options.declared ?? [];
  const minimumExecuted = options.minimumExecuted ?? 0;
  const requiredExecuted = options.requiredExecuted ?? [];
  const output = new Map<string, string>();
  const started = new Set<string>();
  const terminal = new Map<string, "pass" | "fail" | "skip">();
  const packages = new Map<string, string>();
  let invalid = false;
  for (const event of events) {
    if (!event || typeof event.Package !== "string" || !event.Package || typeof event.Action !== "string") { invalid = true; continue; }
    const id = typeof event.Test === "string" && event.Test ? `${event.Package}\0${event.Test}` : undefined;
    if (id && typeof event.Output === "string") output.set(id, `${output.get(id) ?? ""}${event.Output}`);
    if (id && event.Action === "run") started.add(id);
    if (id && ["pass", "fail", "skip"].includes(event.Action)) {
      if (!started.has(id) || terminal.has(id)) invalid = true;
      terminal.set(id, event.Action as "pass" | "fail" | "skip");
    }
    if (!id && ["pass", "fail", "skip"].includes(event.Action)) packages.set(event.Package, event.Action);
  }
  const byName = new Map<string, { id: string; action: "pass" | "fail" | "skip" }>();
  for (const [id, action] of terminal) byName.set(id.split("\0")[1], { id, action });
  const executed: string[] = [];
  const excluded = new Map<string, DatabaseFreeExclusion>();
  const failed: { name: string; output: string }[] = [];
  const undeclared: { name: string; reason: string }[] = [];
  // Subtests are decided before their parents so a parent that fails only
  // because its capability-gated children never ran is excluded with them.
  const order = [...terminal].sort((left, right) => {
    const depth = (id: string) => id.split("\0")[1].split("/").length;
    return depth(right[0]) - depth(left[0]) || left[0].localeCompare(right[0]);
  });
  for (const [id, action] of order) {
    const name = id.split("\0")[1];
    const text = output.get(id) ?? "";
    if (action === "pass") { executed.push(name); continue; }
    // Only the fixture helper's sole reported reason can declare a missing
    // capability. A marker elsewhere in the log cannot erase a real assertion.
    const reasons = text.split(/\r?\n/).filter(line => /^\s+\S+\.go:\d+: /u.test(line));
    const declaredMarker = reasons.length === 1 ? markers.find((candidate) => firstReason(text).includes(candidate.marker)) : undefined;
    if (declaredMarker) {
      excluded.set(name, { name, capability: declaredMarker.capability, reason: firstReason(text) });
      continue;
    }
    if (action === "skip") { undeclared.push({ name, reason: firstReason(text) }); continue; }
    const children = [...byName].filter(([child]) => child.startsWith(`${name}/`));
    const inherited = [...new Set(children.map(([child]) => excluded.get(child)?.capability).filter(Boolean) as string[])].sort();
    const childFailure = children.some(([child, entry]) => entry.action === "fail" && !excluded.has(child));
    if (inherited.length > 0 && !childFailure) {
      excluded.set(name, { name, capability: inherited[0], reason: `children require ${inherited.join(", ")}` });
      continue;
    }
    failed.push({ name, output: text });
  }
  const account = (name: string) => executed.includes(name) || excluded.has(name);
  const accounted = declared.filter(account);
  const missing = declared.filter((name) => !account(name));
  const requiredMissed = requiredExecuted.filter((name) => !executed.includes(name));
  const unexplainedPackages = [...packages].filter(([pkg, action]) => action === "fail" &&
    ![...terminal].some(([id, testAction]) => id.startsWith(`${pkg}\0`) && testAction === "fail"));
  const reason = invalid || started.size !== terminal.size || packages.size === 0 ? "invalid or incomplete Go JSON summary" :
    unexplainedPackages.length ? `Go package failed without a test failure: ${unexplainedPackages.map(([pkg]) => pkg).join(",")}` :
    executed.length === 0 ? "no Go tests executed" :
    failed.length > 0 ? `Go tests failed: ${failed.map((entry) => entry.name).join(",")}` :
    requiredMissed.length > 0 ? `declared tests did not execute: ${requiredMissed.join(",")}` :
    executed.length < minimumExecuted ? `only ${executed.length} of the required ${minimumExecuted} tests executed` :
    undeclared.length > 0 ? `tests skipped without a declared capability marker: ${undeclared.map((entry) => `${entry.name} (${entry.reason})`).join("; ")}` :
    missing.length > 0 ? `declared coverage is not accounted for: ${missing.join(",")}` : undefined;
  return {
    executed: executed.sort(), excluded: [...excluded.values()].sort((left, right) => left.name.localeCompare(right.name)),
    failed: failed.sort((left, right) => left.name.localeCompare(right.name)), undeclared: undeclared.sort((left, right) => left.name.localeCompare(right.name)),
    declared: { accounted, missing }, ...(reason ? { reason } : {})
  };
}

/** The visible account of one lane: every executed, excluded or failed test. */
export function databaseFreeCoverageReport(coverage: DatabaseFreeCoverage) {
  const lines = [`database-free coverage: executed=${coverage.executed.length} excluded=${coverage.excluded.length} declared=${coverage.declared.accounted.length}/${coverage.declared.accounted.length + coverage.declared.missing.length}`];
  const byCapability = new Map<string, string[]>();
  for (const item of coverage.excluded) byCapability.set(item.capability, [...(byCapability.get(item.capability) ?? []), `${item.name} (${item.reason})`]);
  for (const [capability, names] of byCapability) {
    lines.push(`  excluded by unprovisioned capability ${capability}: ${names.length}`);
    for (const name of names) lines.push(`    ${name}`);
  }
  for (const item of coverage.failed) {
    lines.push(`  failed ${item.name}`);
    for (const line of item.output.split(/\r?\n/).filter((value) => value.trim()).slice(-8)) lines.push(`    ${line.trim()}`);
  }
  for (const item of coverage.undeclared) lines.push(`  skipped without a declared capability marker ${item.name}: ${item.reason}`);
  for (const name of coverage.declared.missing) lines.push(`  declared but not accounted for ${name}`);
  return `${lines.join("\n")}\n`;
}

/**
 * Execute one whole-package database-free lane and classify its own run. The
 * non-zero exit code of a capability-gated suite is classified, not trusted:
 * only a declared capability marker excuses it.
 */
export async function runDatabaseFreeGoLane(step: VerificationStep, env: NodeJS.ProcessEnv = process.env): Promise<DatabaseFreeCoverage> {
  const result = await runProcess(step.command, step.args, { cwd: stepCwd(step), env, capture: true, allowFailure: true });
  const coverage = databaseFreeCoverage(parseGoTestEvents(result.stdout), {
    declared: step.declaredTests ?? [],
    minimumExecuted: step.minimumExecutedTests ?? 0,
    requiredExecuted: step.requiredExecutedTests ?? []
  });
  // A build or invocation failure produces no test events at all; the compiler
  // output is the only evidence, so it becomes the reason instead of a
  // misleading "no tests" verdict.
  if (coverage.executed.length + coverage.excluded.length + coverage.failed.length + coverage.undeclared.length === 0) {
    return { ...coverage, reason: `go test executed no tests (${result.signal || `exit ${result.code}`}): ${result.stderr.trim().slice(-2000)}` };
  }
  return coverage;
}

function runGoJSONWithoutSkips(args: string[], { cwd, env }: { cwd: string; env: NodeJS.ProcessEnv }): Promise<GoTestSummary> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn("go", args, { cwd, env, stdio: ["ignore", "pipe", "inherit"] });
    let pending = "";
    const failureEvents: GoTestEvent[] = [];
    let skipped = 0;
    let outputLog = "";
    let parseError: Error | undefined;
    const passedPackages = new Set<string>();
    const passedTests = new Map<string, { package: string; name: string }>();
    const consume = (line: string) => {
      if (!line.trim()) return;
      try {
        const event = JSON.parse(line);
        if (event.Action === "fail") failureEvents.push(event);
        if (event.Action === "skip") skipped += 1;
        if (event.Action === "pass" && !event.Test && event.Package) {
          passedPackages.add(event.Package);
        }
        if (event.Action === "pass" && event.Test && event.Package) {
          passedTests.set(`${event.Package}\0${event.Test}`, { package: event.Package, name: event.Test });
        }
        if (event.Output && outputLog.length < 2_000_000) outputLog += event.Output;
      } catch (error) {
        parseError ||= error instanceof Error ? error : new Error(String(error));
      }
    };
    child.stdout!.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      pending += chunk;
      const lines = pending.split(/\r?\n/);
      pending = lines.pop() || "";
      for (const line of lines) consume(line);
    });
    child.on("error", reject);
    child.on("close", (code, signal) => {
      consume(pending);
      const failures = summarizeGoTestFailures(failureEvents);
      const failed = failures.tests.length + failures.packages.length;
      if (parseError) {
        reject(new Error(`invalid go test JSON output: ${parseError.message}`));
      } else if (code !== 0 || failures.tests.length > 0 || failures.packages.length > 0 || skipped > 0) {
        if (outputLog) process.stderr.write(outputLog.slice(-2_000_000));
        const details = [
          failures.tests.length > 0 ? `tests=${failures.tests.map((entry) => `${entry.package}:${entry.name}`).join(",")}` : "",
          failures.packages.length > 0 ? `packages=${failures.packages.join(",")}` : ""
        ].filter(Boolean).join("; ");
        reject(new Error(`Go FAIL tests=${failures.tests.length} packages=${failures.packages.length} SKIP ${skipped}; process=${signal || code}${details ? `; ${details}` : ""}`));
      } else {
        process.stdout.write(`Go packages passed: ${passedPackages.size}; skipped: 0\n`);
        resolvePromise({
          failed,
          skipped,
          passedPackages: [...passedPackages].sort(),
          passedTests: [...passedTests.values()].sort((left, right) =>
            left.package.localeCompare(right.package) || left.name.localeCompare(right.name))
        });
      }
    });
  });
}

async function runPostgresVerification(env: NodeJS.ProcessEnv) {
  const verifyModule = async (spec: { cwd: string; race?: boolean; timeout?: string }) => {
    const cwd = join(root, spec.cwd);
    printStep(`${spec.cwd} PostgreSQL compile`);
    await runProcess("go", ["test", "-run", "^$", "./..."], { cwd, env });
    const packagesResult = await runProcess(
      "go",
      ["list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}", "./..."],
      { cwd, env, capture: true }
    );
    const packages = packagesResult.stdout.split(/\r?\n/).map((item) => item.trim()).filter(Boolean).sort();
    if (packages.length === 0) throw new Error(`no Go test packages found under ${spec.cwd}`);
    const args = ["test"];
    if (spec.race) args.push("-race");
    if (spec.timeout) args.push(`-timeout=${spec.timeout}`);
    args.push("-count=1", "-json", ...packages);
    printStep(`${spec.cwd} PostgreSQL tests (zero skips)`);
    const result = await runGoJSONWithoutSkips(args, { cwd, env });
    return { cwd: spec.cwd, command: "go", args: [...args], packages: [...packages], ...result };
  };
  // These suites share one PostgreSQL instance and Docker daemon. Serialize
  // module setup/teardown so migration I/O cannot starve runtime readback;
  // each suite retains its own concurrency checks and original deadlines.
  const failures: string[] = [];
  for (const spec of postgresVerificationSpecs) {
    try {
      await verifyModule(spec);
    } catch (error) {
      failures.push(`${spec.cwd}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  if (failures.length > 0) throw new Error(`PostgreSQL module verification failed:\n${failures.join("\n")}`);
}

const defaultDependencies = Object.freeze({
  runStep,
  withTemporaryPostgres,
  runPostgresVerification
});

/** PR scope contains only the committed change relative to its explicit event SHAs. */
export function pullRequestChangedPaths(baseSha: string, headSha: string, repositoryRoot = root): string[] {
  if (![baseSha, headSha].every((sha) => typeof sha === "string" && /^[0-9a-f]{40}$/u.test(sha))) {
    throw new Error("pull request scope requires explicit base and head Git SHAs");
  }
  return execFileSync("git", ["-C", repositoryRoot, "diff", "--name-only", "-z", "--no-renames", `${baseSha}...${headSha}`, "--"],
    { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 }).split("\0").filter(Boolean).sort();
}

export function focusedChangeScope(base: string, repositoryRoot = root) {
  const git = (args: string[]) => execFileSync("git", ["-C", repositoryRoot, ...args], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 });
  const baseSha = git(["rev-parse", "--verify", "--end-of-options", `${base}^{commit}`]).trim();
  const mergeBase = git(["merge-base", baseSha, "HEAD"]).trim();
  const split = (value: string) => value.split("\0").filter(Boolean);
  const paths = [...new Set([
    ...split(git(["diff", "--name-only", "-z", "--no-renames", mergeBase, "HEAD", "--"])),
    ...split(git(["diff", "--cached", "--name-only", "-z", "--no-renames", "--"])),
    ...split(git(["diff", "--name-only", "-z", "--no-renames", "--"])),
    ...split(git(["ls-files", "--others", "--exclude-standard", "-z"]))
  ])].sort();
  return { mergeBase, paths };
}

/**
 * Steps that may run with bounded overlap. Only the database-free lanes qualify:
 * they run after every module compile, so they share a warm Go build cache and
 * three of them fit a four-core runner (measured 23s -> 20s in CI, 38s -> 15s on
 * an eight-core host with an identical classified outcome). Overlapping cold
 * `go test -run ^$` compiles instead oversubscribes the runner (measured
 * 126s -> 178s in CI), so that group stays sequential.
 */
export function isOverlappableStep(step: VerificationStep): boolean {
  return step.command === "go" && step.evidence === "go-coverage";
}

export const goStepConcurrency = 3;

/**
 * Run one batch group with at most `limit` child steps in flight. Steps are
 * started in the declared order; the first failure rejects the group, so a
 * failing lane still fails the gate instead of being absorbed by its batch.
 */
export async function runBatchedSteps<T>(
  steps: readonly T[],
  runStep: (step: T) => Promise<void>,
  limit = goStepConcurrency
): Promise<void> {
  for (let index = 0; index < steps.length; index += limit) {
    const batch = steps.slice(index, index + limit);
    const settled = await Promise.allSettled(batch.map((step) => runStep(step)));
    const failed = settled.find((result): result is PromiseRejectedResult => result.status === "rejected");
    if (failed) throw failed.reason;
  }
}

export async function runVerification(options: VerificationOptions = {}, dependencies = defaultDependencies) {
  validateVerificationOptions(options);
  const { withPostgres = false, focused = false, base, testTargets = [], group, shard } = options;
  const scope = focused ? focusedChangeScope(base!) : undefined;
  const steps = scope ? focusedVerificationSteps(scope.paths, testTargets, scope.mergeBase) : localVerificationSteps
    .filter(step => group === undefined || step.group === group)
    .map(step => shard === undefined ? step : { ...step, args: [...step.args, "--", `--test-shard=${shard}`] });
  // Only the database-free lanes overlap; every other step, including the cold
  // module compiles, keeps the documented order (see isOverlappableStep).
  let laneSteps: VerificationStep[] = [];
  for (const step of steps) {
    if (isOverlappableStep(step)) {
      laneSteps.push(step);
      continue;
    }
    if (laneSteps.length > 0) {
      await runBatchedSteps(laneSteps, dependencies.runStep);
      laneSteps = [];
    }
    await dependencies.runStep(step);
  }
  if (laneSteps.length > 0) await runBatchedSteps(laneSteps, dependencies.runStep);
  if (withPostgres) {
    await dependencies.withTemporaryPostgres((env) => dependencies.runPostgresVerification(env));
  }
}

async function main() {
  const options = parseVerifyLocalArgs();
  await runVerification(options);
  const label = options.focused ? `Focused local checks against ${options.base}` : options.group
    ? `Local verification group ${options.group}${options.shard ? ` shard ${options.shard}` : ""}` : "Local verification";
  process.stdout.write(`\n${label} passed${options.withPostgres ? " with PostgreSQL and Docker integration" : ""}.\n`);
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}
