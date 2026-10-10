import { execFileSync, spawn } from "node:child_process";
import { constants } from "node:fs";
import { access, chmod, cp, mkdir, mkdtemp, readFile, realpath, rm, stat, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parse as parseYAML } from "yaml";
import { generationInputs, generatedOutputs } from "./verify-generated-contracts.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function composePostgresImage() {
  const compose = parseYAML(readFileSync(join(root, "compose.yaml"), "utf8"));
  const image = String(compose?.services?.postgres?.image || "").trim();
  if (!/^postgres:[^\s@]+@sha256:[0-9a-f]{64}$/.test(image)) {
    throw new Error("compose.yaml services.postgres.image must be an exact postgres tag@sha256 reference");
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
  "services/capability",
  "services/gateway-integration",
  "services/build",
  "services/runtime-control",
  "services/workspace",
  "services/resource-catalog",
  "services/serve",
  "apps/console-bff"
]);

export const databaseFreeGoTestSpecs = Object.freeze([
  { cwd: "packages/contracts/go", packages: ["./..."] },
  { cwd: "services/control-plane", packages: ["./cmd/control-plane", "./internal/clients"] },
  // The customer settlement trend owner tests live in internal/server, whose
  // PostgreSQL-gated suites need the full lane. This bounded run keeps the new
  // read surface gated without a database.
  { cwd: "services/control-plane", run: "^TestWorkspaceSettlementTrend", packages: ["./internal/server"] },
  { cwd: "services/fabric", packages: ["./cmd/fabric", "./cmd/opl-tencent-provisioner", "./cmd/opl-node-image-retire", "./internal/http"] },
  { cwd: "services/ledger", packages: ["./cmd/ledger", "./internal/http"] },
  { cwd: "services/internal/postgresmigrate", run: "^TestValidateTLS", packages: ["./..."] },
  // The owner runtime's startup and Operation readback rules hold without a database;
  // its PostgreSQL-gated readiness proof runs in the full lane.
  { cwd: "services/internal/protectedresource", packages: ["./..."] },
  // The owner runtime's startup and Operation readback rules hold without a database;
  // its PostgreSQL-gated readiness proof runs in the full lane.
  { cwd: "services/internal/ownerservice", run: "^Test(Ready|Register|Operations|Operation|Start)", packages: ["./..."] }
]);

export const localVerificationSteps = Object.freeze([
  { name: "product boundary", command: "npm", args: ["run", "validate:product-boundary"] },
  { name: "Generated contracts freshness", command: "npm", args: ["run", "verify:generated-contracts"] },
  { name: "Development plan freshness", command: "npm", args: ["run", "verify:development-plan"] },
  { name: "Development tools typecheck", command: "npm", args: ["run", "typecheck:development-tools"] },
  { name: "Node source tests", command: "npm", args: ["run", "test:source"] },
  // One bounded-concurrency invocation instead of nine sequential ones: each
  // group used to boot its own demo server and Chromium before the next started.
  // The named groups stay available for targeted runs.
  { name: "Console browser suite", command: "npm", args: ["run", "test:browser:suite"] },
  { name: "TypeScript typecheck", command: "npm", args: ["run", "typecheck"] },
  { name: "TypeScript lint", command: "npm", args: ["run", "lint"] },
  { name: "Console build", command: "npm", args: ["run", "build"] },
  ...goModules.map((cwd) =>
    ({ name: `${cwd} compile`, command: "go", args: ["test", "-run", "^$", "./..."], cwd })
  ),
  ...databaseFreeGoTestSpecs.map((spec) => ({
    name: `${spec.cwd} database-free tests`,
    command: "go",
    args: ["test", "-count=1", ...(spec.run ? ["-run", spec.run] : []), ...spec.packages],
    cwd: spec.cwd
  })),
  { name: "Git whitespace", command: "git", args: ["diff", "--check"] }
]);

type VerificationStep = { name: string; command: string; args: string[]; cwd?: string; evidence?: "node" | "go" };

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
  if (changedPaths.some((path) => path.startsWith("docs/spec/target/"))) {
    add({ name: "Development plan freshness", command: "npm", args: ["run", "verify:development-plan"] });
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

export function parseVerifyLocalArgs(args = process.argv.slice(2)) {
  let withPostgres = false;
  let focused = false;
  let base: string | undefined;
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
    if (token === "--test") {
      const value = args[++index];
      if (!value || value.startsWith("--")) throw new Error("verify-local argument --test requires a file");
      if (testTargets.includes(value)) throw new Error(`duplicate focused test target: ${value}`);
      testTargets.push(value);
      continue;
    }
    if (token === "--base") {
      if (base !== undefined) throw new Error("verify-local argument --base may be provided once");
      const value = args[++index];
      if (!value || value.startsWith("--")) throw new Error("verify-local argument --base requires a ref");
      base = value;
      continue;
    }
    throw new Error(`unknown verify-local argument: ${token}`);
  }
  if (focused && withPostgres) throw new Error("cannot combine focused checks with the exhaustive PostgreSQL lane; supply the focused owner database environment explicitly");
  if (focused && base === undefined) throw new Error("focused verification requires --base <ref>");
  if (!focused && base !== undefined) throw new Error("--base requires --focused");
  if (!focused && testTargets.length) throw new Error("--test requires --focused");
  return { withPostgres, ...(focused ? { focused: true, base: base! } : {}), ...(testTargets.length ? { testTargets } : {}) };
}


// This source-check boundary deliberately does not call runProcess/runStep:
// those belong to the independent, trusted Local/Instance qualification lane.
type DevelopmentCheckResult = {
  passed: boolean;
  status: "passed" | "failed" | "blocked";
  exitCode: number | null;
  tests: number;
  failed: number;
  skipped: number;
  output: string;
  /** The command line that actually executed; absent when nothing executed. */
  command?: string;
  /** The executed cwd within the snapshot, when it was not the snapshot root. */
  cwd?: string;
  reason?: string;
};
type CapturedProcess = { code: number | null; stdout: string; stderr: string; problem?: string };
const developmentOutputLimit = 2_000_000;
const trustedToolDirectories = [dirname(process.execPath), "/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin", "/usr/sbin", "/sbin"];
const commandLineToolsPython = "/Library/Developer/CommandLineTools/usr/bin";
const commandLineToolsPythonFramework = "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/";

function inside(directory: string, path: string) {
  const suffix = relative(directory, path);
  return suffix === "" || (!isAbsolute(suffix) && suffix !== ".." && !suffix.startsWith(`..${sep}`));
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

/**
 * The owner-database fixture is one of two host-provisioned forms: the macOS
 * loopback TCP DSN, or the Linux host-created Unix-socket directory. The
 * pairing is checked before any execution. Shape and pairing are boundary
 * guards, not a provenance proof: provenance comes from the host entry, which
 * is the only provisioner and never reads a DSN from the worker or the
 * invoking environment.
 */
export type OwnerDatabaseFixture =
  | { kind: "loopback-tcp"; dsn: string }
  | { kind: "linux-socket"; dsn: string; directory: string }
  | { blocked: string };

export function resolveOwnerDatabaseFixture(
  dsn: unknown,
  socketDirectory: unknown,
  platform: NodeJS.Platform = process.platform
): OwnerDatabaseFixture {
  if (socketDirectory === undefined) {
    if (typeof dsn !== "string" || !isLoopbackOwnerDatabaseDsn(dsn))
      return { blocked: "owner database fixture must be an isolated loopback PostgreSQL admin DSN" };
    return { kind: "loopback-tcp", dsn };
  }
  if (platform !== "linux") return { blocked: "owner database socket fixture only applies on Linux" };
  if (typeof socketDirectory !== "string" || !socketDirectory || socketDirectory.length > 512 ||
      !isAbsolute(socketDirectory) || /[\x00-\x1f]/u.test(socketDirectory))
    return { blocked: "owner database socket fixture directory must be an absolute host directory" };
  if (typeof dsn !== "string" || !dsn || dsn.length > 512)
    return { blocked: "owner database socket DSN must name the exact host-created fixture directory" };
  let parsed: URL;
  try { parsed = new URL(dsn); } catch { return { blocked: "owner database socket DSN must name the exact host-created fixture directory" }; }
  if (parsed.protocol !== "postgresql:" && parsed.protocol !== "postgres:")
    return { blocked: "owner database socket DSN must name the exact host-created fixture directory" };
  const host = parsed.searchParams.get("host") ?? "";
  if (!host.startsWith("/") || /[\x00-\x1f]/u.test(host) || host !== socketDirectory)
    return { blocked: "owner database socket DSN must name the exact host-created fixture directory" };
  return { kind: "linux-socket", dsn, directory: socketDirectory };
}

function exactRelativePath(path: string) {
  return typeof path === "string" && path.length > 0 && path.split("/").every((part) =>
    /^[A-Za-z0-9_][A-Za-z0-9_.-]*$/.test(part) && part !== "." && part !== "..");
}

async function trustedTool(name: string) {
  for (const directory of trustedToolDirectories) {
    const candidate = join(directory, name);
    try {
      await access(candidate, constants.X_OK);
      if ((await stat(candidate)).isFile()) return await realpath(candidate);
    } catch { /* Only these fixed installation locations are eligible. */ }
  }
  return undefined;
}

/**
 * Resolve one approved host interpreter from the fixed installation
 * locations. On macOS /usr/bin/python3 is an Xcode selector stub, not an
 * interpreter: executing it reads the developer selection under /var/select
 * and fails inside a sealed stage, so the Command Line Tools runtime is
 * resolved ahead of it. A candidate that resolves inside the snapshot is
 * never an approved runtime.
 */
export async function resolveTrustedPython(directories: readonly string[], snapshot: string): Promise<string | undefined> {
  for (const directory of directories) {
    try {
      const candidate = join(directory, "python3");
      await access(candidate, constants.X_OK);
      if ((await stat(candidate)).isFile()) {
        const tool = await realpath(candidate);
        if (!inside(snapshot, tool)) return tool;
      }
    } catch { /* Only fixed runtime installation locations are eligible. */ }
  }
  return undefined;
}

/**
 * Grant exactly one resolved runtime installation: the invoked executable
 * file plus, when the runtime ships as a multi-file installation, its own
 * fixed installation root. A user HOME, the developer selection or a shared
 * prefix such as /usr is never granted.
 */
function authorizeRuntimeTool(tool: string, readPaths: Set<string>) {
  readPaths.add(tool);
  if (tool.startsWith(commandLineToolsPythonFramework)) readPaths.add(resolve(dirname(tool), ".."));
  else if (tool.startsWith("/opt/homebrew/Cellar/")) readPaths.add(tool.split("/").slice(0, 6).join("/"));
}

// The trusted Go configuration probe reads host runtime configuration. A
// scratch-isolated GOPATH/GOBIN/GOENV/GOMODCACHE/GOROOT would silently
// redirect the probe into empty scratch storage (an inherited scratch GOPATH
// resolves GOMODCACHE under it), so only explicit host values survive and
// missing variables fall back to the host Go defaults.
export function goConfigurationEnv(scratchEnv: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const configuration: NodeJS.ProcessEnv = { ...scratchEnv, HOME: process.env.HOME };
  for (const name of ["GOPATH", "GOBIN", "GOENV", "GOMODCACHE", "GOROOT"]) {
    if (process.env[name] === undefined) delete configuration[name];
    else configuration[name] = process.env[name];
  }
  return configuration;
}

// Both the execution deadline and retained output are bounded. SIGKILL targets
// the process group, including a runner's tests/servers, not just its leader.
// A killed child can hold inherited pipes open; never wait indefinitely for close.
function captureDevelopmentProcess(command: string, args: string[], cwd: string,
  env: NodeJS.ProcessEnv, timeoutMs: number): Promise<CapturedProcess> {
  return new Promise((complete) => {
    const child = spawn(command, args, { cwd, env, detached: true, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "", stderr = "", bytes = 0, problem: string | undefined;
    let settled = false;
    let killDeadline: ReturnType<typeof setTimeout> | undefined;
    const killGroup = () => {
      if (!child.pid) return;
      try { process.kill(-child.pid, "SIGKILL"); } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== "ESRCH") problem ||= `process group kill failed: ${String(error)}`;
      }
    };
    const finish = (code: number | null) => {
      if (settled) return;
      settled = true;
      clearTimeout(deadline);
      if (killDeadline) clearTimeout(killDeadline);
      killGroup(); // Also reap any descendants surviving a successful leader.
      child.stdout.destroy(); child.stderr.destroy(); child.unref();
      complete({ code, stdout, stderr, ...(problem ? { problem } : {}) });
    };
    const abort = (detail: string) => {
      if (problem || settled) return;
      problem = detail;
      killGroup();
      killDeadline = setTimeout(() => finish(null), 250);
    };
    const append = (channel: "stdout" | "stderr", chunk: Buffer) => {
      const remaining = Math.max(0, developmentOutputLimit - bytes);
      const text = chunk.subarray(0, remaining).toString("utf8");
      if (channel === "stdout") stdout += text; else stderr += text;
      bytes += chunk.length;
      if (bytes > developmentOutputLimit) abort("output limit exceeded");
    };
    const deadline = setTimeout(() => abort(`timeout after ${timeoutMs}ms`), timeoutMs);
    child.stdout.on("data", (chunk: Buffer) => append("stdout", chunk));
    child.stderr.on("data", (chunk: Buffer) => append("stderr", chunk));
    child.on("error", (error) => { problem = `spawn failed: ${error.message}`; finish(null); });
    child.on("exit", killGroup);
    child.on("close", (code, signal) => {
      if (signal) problem ||= `terminated by ${signal}`;
      finish(code);
    });
  });
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

/**
 * The host approves one fixed system browser installation for browser stages;
 * an unapproved download or user-home cache is never admitted. The stage probe
 * launches this installation inside the OS sandbox before any test executes.
 */
export const approvedBrowserApplications = Object.freeze(
  process.platform === "darwin"
    ? [{ application: "/Applications/Google Chrome.app", executable: "Contents/MacOS/Google Chrome" }]
    : [{ application: "/opt/google/chrome", executable: "chrome" }]
);
export type ApprovedBrowser = { application: string; executable: string };

/** Resolve one complete approved browser installation; anything short of that stays absent. */
export async function resolveApprovedBrowser(
  applications: readonly { application: string; executable: string }[] = approvedBrowserApplications
): Promise<ApprovedBrowser | undefined> {
  for (const candidate of applications) {
    try {
      const application = await realpath(candidate.application);
      const executable = await realpath(join(application, candidate.executable));
      if (!inside(application, executable) || !(await stat(executable)).isFile()) continue;
      await access(executable, constants.X_OK);
      return { application, executable };
    } catch { /* Only a complete fixed installation is an approved browser. */ }
  }
  return undefined;
}

// A browser stage boots the approved browser and its fixture server; the
// focused Console target pair measured about five minutes in the open
// environment, so a browser stage receives the runner's full bounded budget
// instead of the two minutes that cover hermetic Node stages.
const browserStageTimeoutMs = 900_000;

export async function runDevelopmentCheck(options: {
  snapshotRoot: string;
  kind: "node" | "go" | "browser" | "generated" | "developmentPlan";
  targets?: string[];
  cwd?: string;
  timeoutMs?: number;
  /**
   * The host-owned isolated owner-database DSN for a stage whose gate declared
   * `database: "isolated-owner-postgres"`. It is passed in by the host runner
   * that provisioned the fixture, never inherited from the invoking process.
   */
  ownerDatabaseDsn?: string;
  /**
   * The Linux host-created socket directory of that same fixture. A private
   * network namespace cannot reach the host's published TCP port, so the
   * stage connects through this directory, which is the only fixture grant.
   */
  ownerDatabaseSocketDir?: string;
}): Promise<DevelopmentCheckResult> {
  const blocked = (reason: string, output = ""): DevelopmentCheckResult =>
    ({ passed: false, status: "blocked", exitCode: null, tests: 0, failed: 0, skipped: 0, output, reason });
  let scratch: string | undefined;
  try {
    const timeoutMs = options.timeoutMs ?? (options.kind === "browser" ? browserStageTimeoutMs : 120_000);
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0 || timeoutMs > 900_000) return blocked("invalid timeoutMs (1..900000 required)");
    if (!["node", "go", "browser", "generated", "developmentPlan"].includes(options.kind)) return blocked("unsupported development check kind");
    const snapshot = await realpath(options.snapshotRoot);
    const temporaryRoot = await realpath(tmpdir());
    const temporaryRoots = new Set([temporaryRoot]);
    for (const path of ["/tmp", "/var/tmp"]) {
      try { temporaryRoots.add(await realpath(path)); } catch { /* Absent OS temporary directory. */ }
    }
    if (![...temporaryRoots].some((path) => inside(path, snapshot)) || temporaryRoots.has(snapshot) || !(await stat(snapshot)).isDirectory() ||
        inside(root, snapshot) || inside(snapshot, await realpath(root))) {
      return blocked("snapshotRoot must be a separate materialized host temporary directory, not the source repository");
    }
    const requestedCwd = options.cwd ?? ".";
    if (requestedCwd !== "." && !isAbsolute(requestedCwd) && !exactRelativePath(requestedCwd)) return blocked("invalid snapshot cwd");
    const cwd = await realpath(resolve(snapshot, requestedCwd));
    if (!inside(snapshot, cwd) || !(await stat(cwd)).isDirectory()) return blocked("cwd escapes snapshot");
    const targets = options.targets ?? [];
    if (!Array.isArray(targets)) return blocked("targets must be exact relative test files");
    if (options.kind !== "node" && options.kind !== "browser" && targets.length) return blocked("targets are supported only for Node and browser checks");
    if (options.kind === "node" || options.kind === "browser") {
      if (targets.length === 0) return blocked(`${options.kind === "node" ? "Node" : "Browser"} checks require explicit test file targets`);
      for (const target of targets) {
        if (!exactRelativePath(target) || !/\.(?:[cm]?[jt]s)$/.test(target)) return blocked("invalid test target: flags, traversal and globs are forbidden");
        const file = await realpath(resolve(cwd, target));
        if (!inside(snapshot, file) || !(await stat(file)).isFile()) return blocked("test target escapes snapshot or is not a file");
      }
    }
    if ((options.kind === "browser" || options.kind === "generated" || options.kind === "developmentPlan") && cwd !== snapshot) return blocked("browser/generated/plan checks require the snapshot root cwd");
    const isolationTool = process.platform === "darwin" ? "/usr/bin/sandbox-exec" :
      process.platform === "linux" ? await trustedTool("bwrap") : undefined;
    if (!isolationTool) return blocked("OS sandbox isolation unavailable", "sandbox probe unavailable: sandbox-exec/bwrap is required; no unsandboxed execution attempted");
    try { await access(isolationTool, constants.X_OK); } catch {
      return blocked("OS sandbox isolation unavailable", `sandbox probe unavailable: ${isolationTool}`);
    }
    scratch = await realpath(await mkdtemp(join(tmpdir(), "opl-check-scratch-")));
    for (const name of ["home", "tmp", "go-cache", "go-path", "npm-cache"]) await mkdir(join(scratch, name));
    const env: NodeJS.ProcessEnv = {
      PATH: trustedToolDirectories.join(":"), HOME: join(scratch, "home"), TMPDIR: join(scratch, "tmp"),
      TMP: join(scratch, "tmp"), TEMP: join(scratch, "tmp"), LANG: "C.UTF-8", LC_ALL: "C.UTF-8", TZ: "UTC",
      CI: "1", NO_COLOR: "1", GOCACHE: join(scratch, "go-cache"), GOPATH: join(scratch, "go-path"),
      GOENV: "off", GOWORK: "off", GOPROXY: "off", GOSUMDB: "off", GOTOOLCHAIN: "local", GOFLAGS: "",
      npm_config_cache: join(scratch, "npm-cache"), npm_config_userconfig: "/dev/null", npm_config_globalconfig: join(scratch, "home/.npmrc-global"),
      npm_config_ignore_scripts: "true", npm_config_offline: "true", npm_config_update_notifier: "false", npm_config_audit: "false", npm_config_fund: "false"
    };
    // A declared owner-database stage receives only the host-provisioned
    // isolated endpoint. The invoking process environment is never a source
    // for it, so no production DSN can leak into a sealed stage.
    let ownerSocketDirectory: string | undefined;
    if (options.ownerDatabaseDsn !== undefined || options.ownerDatabaseSocketDir !== undefined) {
      const fixture = resolveOwnerDatabaseFixture(options.ownerDatabaseDsn, options.ownerDatabaseSocketDir);
      if ("blocked" in fixture) return blocked(fixture.blocked);
      if (fixture.kind === "linux-socket") {
        let resolved: string | undefined;
        try { resolved = await realpath(fixture.directory); } catch { resolved = undefined; }
        if (!resolved || !(await stat(resolved)).isDirectory()) return blocked("owner database socket fixture directory is unavailable");
        if (resolved !== fixture.directory) return blocked("owner database socket fixture directory must be the exact host-created directory");
        ownerSocketDirectory = resolved;
      }
      env.OPL_POSTGRES_TESTS = "1";
      env.OPL_OWNER_MIGRATION_TEST_ADMIN_DSN = fixture.dsn;
    }
    let command = await realpath(process.execPath);
    let args: string[] = [];
    let tapTargets = targets;
    // Bind only runtime installations; never the tool's user HOME, signing store,
    // source checkout, Docker socket or a guessed shared cache parent directory.
    const readPaths = new Set<string>([snapshot, command]);
    // A listed system path is bound only when it resolves on this host: an
    // absent path (for example /usr/lib64 on a merged-/usr image) is skipped
    // instead of becoming a bwrap source error, and a path that resolves is
    // bound both where programs look for it and at its real location, so an
    // ELF interpreter reached through a system symlink stays reachable.
    for (const path of process.platform === "darwin" ?
      ["/System/Library", "/usr/lib", "/usr/bin", "/bin", "/usr/share/zoneinfo", "/Library/Apple/System/Library"] :
      ["/usr/lib", "/usr/lib64", "/lib", "/lib64", "/usr/bin", "/bin", "/usr/share/zoneinfo", "/etc/ld.so.cache", "/etc/localtime"]) {
      try {
        const canonical = await realpath(path);
        readPaths.add(path); readPaths.add(canonical);
      } catch { /* Absent system installation is not mounted. */ }
    }
    // A stage that declares JavaScript dependencies must declare the complete
    // locked manifest pair and nothing else: both files have to be the
    // approved host manifests byte for byte. The installation itself is
    // materialized below as a private scratch copy; the shared installation
    // and the source checkout are never granted to the stage.
    let approvedInstallation: string | undefined;
    if (options.kind === "node" || options.kind === "browser") {
      const declaredManifest = join(snapshot, "package.json");
      if (existsSync(declaredManifest)) {
        if (!(await readFile(declaredManifest)).equals(await readFile(join(root, "package.json")))) {
          return blocked("snapshot dependencies require the approved package manifest");
        }
        const declaredLock = join(snapshot, "package-lock.json");
        if (!existsSync(declaredLock) || !(await readFile(declaredLock)).equals(await readFile(join(root, "package-lock.json")))) {
          return blocked("snapshot dependencies are not the approved locked host installation");
        }
        try {
          const installation = await realpath(join(root, "node_modules"));
          if ((await stat(installation)).isDirectory()) approvedInstallation = installation;
        } catch { /* An uninstalled host installation stays absent. */ }
        if (!approvedInstallation) return blocked("approved locked host installation unavailable: the locked dependencies are not installed on the host");
      } else if (options.kind === "browser") {
        return blocked("browser checks require the approved package manifest in the stage inputs");
      }
    }
    // Repository fixtures execute Git. macOS /usr/bin/git is an Xcode selector
    // that needs developer-selection state the sandbox intentionally never
    // receives, so one fixed real installation is resolved on the host from
    // known locations and only that installation is bound for Node stages.
    if (options.kind === "node") {
      // Hermetic Git configuration for every Node stage: no system file and no
      // terminal prompt. A stage that never executes Git must stay runnable on
      // a host without a fixed installation; a Git-dependent target then fails
      // on its own instead of silently widening host configuration.
      env.GIT_CONFIG_NOSYSTEM = "1";
      env.GIT_TERMINAL_PROMPT = "0";
      const gitDirectories = process.platform === "darwin"
        ? ["/Library/Developer/CommandLineTools/usr/bin", "/opt/homebrew/bin", ...trustedToolDirectories]
        : trustedToolDirectories;
      let git: string | undefined;
      for (const directory of gitDirectories) {
        try {
          const candidate = join(directory, "git");
          await access(candidate, constants.X_OK);
          if ((await stat(candidate)).isFile()) { git = await realpath(candidate); break; }
        } catch { /* Only fixed installation locations are eligible. */ }
      }
      if (git) {
        // Trusted Git reports its own command directory; the sandbox authorizes
        // exactly that installation instead of any developer selection or PATH.
        // The configuration probe is hermetic as well: nested isolation (a
        // development gate that itself runs this runner) has no system
        // gitconfig grant, and no prompt or user config may affect the answer.
        const configuration = await captureDevelopmentProcess(git, ["--exec-path"], temporaryRoot,
          { HOME: process.env.HOME ?? temporaryRoot, PATH: process.env.PATH ?? "", GIT_CONFIG_NOSYSTEM: "1", GIT_TERMINAL_PROMPT: "0" }, 10_000);
        const reported = configuration.stdout.trim();
        if (configuration.code !== 0 || configuration.problem || !reported) {
          return blocked("trusted Git runtime configuration unavailable", configuration.stderr + configuration.stdout + (configuration.problem || ""));
        }
        const execPath = await realpath(reported);
        if (inside(snapshot, execPath) || inside(root, execPath) || inside(execPath, root)) return blocked("unsafe Git runtime installation");
        readPaths.add(git); readPaths.add(execPath);
        try {
          const share = await realpath(resolve(execPath, "..", "..", "share", "git-core"));
          if (!inside(snapshot, share) && !inside(root, share) && !inside(share, root)) readPaths.add(share);
        } catch { /* A Git installation without shared templates stays usable. */ }
        env.GIT_EXEC_PATH = execPath;
        env.PATH = dirname(git) + ":" + env.PATH;
      }
    }
    if (options.kind === "node" || options.kind === "browser") args = ["--test", "--test-reporter=tap", ...targets];
    if (options.kind === "go" || options.kind === "generated") {
      const go = await trustedTool("go");
      if (options.kind === "go" && !go) return blocked("trusted Go installation unavailable");
      if (go) {
        // Trusted go env reads authoritative host runtime configuration, not a
        // snapshot go.mod or an attacker-supplied command. No tested code runs here.
        const configurationEnv = goConfigurationEnv(env);
        const configuration = await captureDevelopmentProcess(go, ["env", "-json", "GOROOT", "GOMODCACHE", "GOBIN", "GOPATH"], temporaryRoot, configurationEnv, 10_000);
        if (configuration.code !== 0 || configuration.problem) return blocked("trusted Go runtime configuration unavailable", configuration.stderr + configuration.stdout + (configuration.problem || ""));
        const values = JSON.parse(configuration.stdout);
        const goroot = await realpath(values.GOROOT);
        if (inside(snapshot, goroot) || inside(goroot, root)) return blocked("unsafe Go runtime installation");
        readPaths.add(goroot); readPaths.add(go); env.GOROOT = goroot;
        // A missing module cache remains missing: GOPROXY=off and network
        // isolation prevent a dependency download from production or the Internet.
        env.GOMODCACHE = values.GOMODCACHE;
        try {
          const modcache = await realpath(values.GOMODCACHE);
          if (modcache === temporaryRoot || inside(modcache, root) || inside(modcache, process.env.HOME || root)) return blocked("unsafe Go module cache configuration");
          readPaths.add(modcache); env.GOMODCACHE = modcache;
        } catch { /* Go reports missing dependencies as failure; never downloads. */ }
        if (options.kind === "go") {
          command = go; args = ["test", "-count=1", "-json", "./..."];
          const module = await realpath(join(cwd, "go.mod"));
          if (!inside(snapshot, module)) return blocked("Go module escapes snapshot");
        }
        if (options.kind === "generated") {
          // The generation chains run the two approved protoc plugins and
          // gofmt. The plugins are resolved from the host's own Go plugin
          // installation (GOBIN, or GOPATH/bin when GOBIN is unset) instead
          // of a PATH lookup that could execute an unapproved binary, and the
          // go toolchain's own bin directory provides the formatter.
          env.PATH = join(goroot, "bin") + ":" + env.PATH;
          const pluginDirectory = typeof values.GOBIN === "string" && values.GOBIN ? values.GOBIN :
            typeof values.GOPATH === "string" && values.GOPATH ? join(values.GOPATH, "bin") : undefined;
          if (!pluginDirectory || !isAbsolute(pluginDirectory)) return blocked("trusted Go plugin installation unavailable");
          let gobin: string;
          try { gobin = await realpath(pluginDirectory); } catch { return blocked("trusted Go plugin installation unavailable"); }
          if (gobin === temporaryRoot || inside(root, gobin) || inside(gobin, root) || inside(snapshot, gobin) ||
              inside(gobin, process.env.HOME || root)) return blocked("unsafe Go plugin installation");
          for (const name of ["protoc-gen-go", "protoc-gen-go-grpc"]) {
            const plugin = join(gobin, name);
            let resolved: string;
            try {
              if (!((await stat(plugin)).isFile())) return blocked(`trusted protoc plugin unavailable: ${name}`);
              await access(plugin, constants.X_OK);
              resolved = await realpath(plugin);
            } catch { return blocked(`trusted protoc plugin unavailable: ${name}`); }
            if (inside(snapshot, resolved) || inside(root, resolved)) return blocked("unsafe Go plugin installation");
            readPaths.add(resolved);
          }
          env.PATH = gobin + ":" + env.PATH;
        }
      }
    }
    if (options.kind === "generated") {
      const checker = await realpath(join(snapshot, "tools/verify-generated-contracts.ts"));
      if (!inside(snapshot, checker)) return blocked("generated checker escapes snapshot");
      args = ["tools/verify-generated-contracts.ts"];
      // The generation chains run the approved interpreter, and the pinned
      // generator packages (grpcio-tools, PyYAML) are installed in that
      // interpreter's user site, which the sealed stage would never reach
      // through its scratch HOME: the interpreter itself reports the
      // directory, and exactly that installation is bound read-only and
      // exposed through PYTHONPATH. Nothing else of the user HOME is granted.
      const pythonDirectories = process.platform === "darwin" ?
        [commandLineToolsPython, ...trustedToolDirectories] : trustedToolDirectories;
      const python = await resolveTrustedPython(pythonDirectories, snapshot);
      if (!python) return blocked("trusted Python runtime unavailable");
      authorizeRuntimeTool(python, readPaths);
      env.PATH = dirname(python) + ":" + env.PATH;
      const site = await captureDevelopmentProcess(python, ["-c", "import site;print(site.getusersitepackages())"],
        temporaryRoot, { HOME: process.env.HOME ?? temporaryRoot }, 10_000);
      if (site.code !== 0 || site.problem) return blocked("trusted Python runtime configuration unavailable", site.stderr + site.stdout + (site.problem || ""));
      const userSite = site.stdout.trim();
      if (userSite) {
        if (!isAbsolute(userSite)) return blocked("unsafe Python user-site installation");
        let resolved: string | undefined;
        try { resolved = await realpath(userSite); } catch { /* A missing user-site installation keeps the interpreter's own import path. */ }
        if (resolved) {
          if (inside(snapshot, resolved) || inside(resolved, snapshot) || inside(root, resolved) || inside(resolved, root) ||
              inside(resolved, process.env.HOME || root)) return blocked("unsafe Python user-site installation");
          readPaths.add(resolved); env.PYTHONPATH = resolved;
        }
      }
    }
    if (options.kind === "developmentPlan") {
      const checker = await realpath(join(snapshot, "tools/verify-development-plan.ts"));
      if (!inside(snapshot, checker)) return blocked("development plan checker escapes snapshot");
      args = ["tools/verify-development-plan.ts"];
      // The plan gate regenerates into an isolated tree and byte-compares; it
      // only needs the approved interpreter, never protoc, plugins or the
      // network.
      const pythonDirectories = process.platform === "darwin" ?
        [commandLineToolsPython, ...trustedToolDirectories] : trustedToolDirectories;
      const python = await resolveTrustedPython(pythonDirectories, snapshot);
      if (!python) return blocked("trusted Python runtime unavailable");
      authorizeRuntimeTool(python, readPaths);
      env.PATH = dirname(python) + ":" + env.PATH;
    }
    let browserApplication: ApprovedBrowser | undefined;
    if (options.kind === "browser") {
      // Focused browser execution stays inside the approved suite inventory:
      // the gate selects a subset of the existing public browser targets and
      // can never point the browser stage at an undeclared target.
      if (!Buffer.from(await readFile(join(snapshot, "package.json"))).equals(await readFile(join(root, "package.json")))) {
        return blocked("browser snapshot package.json is not the approved host manifest");
      }
      const manifest = JSON.parse(await readFile(join(snapshot, "package.json"), "utf8"));
      const suite = manifest.scripts?.["test:browser:suite"];
      if (typeof suite !== "string") return blocked("approved browser suite is unavailable");
      const inventory = new Set(suite.split(/\s+/).filter((token: string) => exactRelativePath(token) && /\.test\.(?:[cm]?[jt]s)$/.test(token)));
      if (inventory.size === 0) return blocked("approved browser suite has no exact test targets");
      for (const target of targets) {
        if (!inventory.has(target)) return blocked(`browser target is not part of the approved suite inventory: ${target}`);
      }
      browserApplication = await resolveApprovedBrowser();
      if (!browserApplication) return blocked("approved browser runtime unavailable: no complete fixed browser installation is present on the host");
      // The approved installation is the only browser runtime grant. Bundled
      // browser downloads are refused outright: a stage never reaches a
      // user-home cache or downloads a browser during verification.
      readPaths.add(browserApplication.application);
      env.PLAYWRIGHT_BROWSERS_PATH = join(scratch, "browser-runtime-absent");
      env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
      // Chromium resolves its private temporary directory through this fixed
      // macOS variable; without it the browser would attempt the per-user
      // temporary directory that the stage intentionally never receives.
      if (process.platform === "darwin") env.MAC_CHROMIUM_TMPDIR = join(scratch, "tmp");
    }
    // Materialize the approved locked installation as a private scratch copy.
    // The directory keeps the basename `node_modules` so Node's own resolution
    // walks from every copied package back into this tree, and the copy stays
    // the only writable dependency location: Vite's config and dependency
    // build writes land in admitted scratch, never in the shared installation.
    if (approvedInstallation) {
      const dependencies = join(scratch, "stage", "node_modules");
      await mkdir(dirname(dependencies), { recursive: true });
      if (existsSync(join(snapshot, "node_modules"))) return blocked("stage inputs must not contain node_modules; the host materializes the approved locked installation");
      await cp(approvedInstallation, dependencies, { recursive: true, verbatimSymlinks: true, force: false, errorOnExist: true });
      await symlink(dependencies, join(snapshot, "node_modules"));
    }
    // The stage receipt records the command line that actually executed. No
    // environment value (such as a fixture DSN) enters the record.
    const executedCommand = [basename(command), ...args].join(" ");
    const executedCwd = relative(snapshot, cwd) || undefined;
    // An installation grant may not accidentally include source or the user's
    // complete home directory. Snapshot itself is the sole source read grant.
    for (const path of readPaths) {
      if (path !== snapshot && (inside(path, root) || inside(path, process.env.HOME || root))) return blocked("unsafe runtime read grant");
    }
    let wrap: (executable: string, arguments_: string[]) => string[];
    if (process.platform === "darwin") {
      const readFilters = [...readPaths].map((path) => `(subpath ${JSON.stringify(path)})`).join(" ");
      const metadata = new Set<string>();
      for (const path of [...readPaths, scratch]) {
        for (let ancestor = dirname(path); ; ancestor = dirname(ancestor)) {
          metadata.add(ancestor);
          if (ancestor === dirname(ancestor)) break;
        }
      }
      const metadataFilters = [...metadata].map((path) => `(literal ${JSON.stringify(path)})`).join(" ");
      const profile = `(version 1)(deny default)
(allow process-fork)(allow process-exec)(allow signal (target same-sandbox))
(allow sysctl-read)(allow file-read-metadata ${metadataFilters})
; dyld/Node needs to read the root directory entry itself. This is literal,
; not subpath: it grants no child files, host HOME, /tmp, or source checkout.
(allow file-read-data (literal "/"))
(allow file-read* ${readFilters} (subpath ${JSON.stringify(scratch)}) (literal "/dev/null") (literal "/dev/random") (literal "/dev/urandom"))
(allow file-map-executable ${readFilters} (subpath ${JSON.stringify(scratch)}))
(allow file-write* (subpath ${JSON.stringify(scratch)}) (literal "/dev/null"))
(allow network-inbound (local ip "localhost:*"))
(allow network-outbound (remote ip "localhost:*")${browserApplication ? ` (subpath ${JSON.stringify(scratch)})` : ""})
${browserApplication ? `(allow network-bind (subpath ${JSON.stringify(scratch)}))
; The approved browser receives exactly the measured macOS services it needs:
; per-process rendezvous, the window server sentinel, the system
; configuration store, Launch Services and its power notification user
; client. No other Mach service, user client or host socket is reachable.
(allow mach-lookup (global-name-prefix "com.google.Chrome.MachPortRendezvousServer.") (global-name "com.apple.system.notification_center") (global-name "com.apple.windowserver.active") (global-name "com.apple.SystemConfiguration.configd") (global-name "com.apple.coreservices.launchservicesd"))
(allow mach-register (global-name-prefix "com.google.Chrome.MachPortRendezvousServer."))
(allow iokit-open (iokit-user-client-class "RootDomainUserClient"))
` : ""}`;
      wrap = (executable, arguments_) => ["-p", profile, executable, ...arguments_];
    } else {
      const mounts = [...readPaths].flatMap((path) => ["--ro-bind", path, path]);
      // A private network namespace has no host routes/interfaces, so a
      // declared owner-database stage reaches the host fixture through the
      // bound Unix socket directory; that directory is the only fixture grant
      // beyond snapshot and scratch.
      const socketBind = ownerSocketDirectory ? ["--bind", ownerSocketDirectory, ownerSocketDirectory] : [];
      const prefix = ["--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL",
        "--proc", "/proc", "--dev", "/dev", ...mounts, "--bind", scratch, scratch, ...socketBind, "--chdir", cwd];
      wrap = (executable, arguments_) => [...prefix, "--", executable, ...arguments_];
    }
    const probe = await captureDevelopmentProcess(isolationTool, wrap(await realpath(process.execPath), ["-e",
      `const fs=require('node:fs'),p=require('node:path'),a=require('node:assert/strict');
a.throws(()=>fs.readFileSync(${JSON.stringify(join(root, "package.json"))}));
a.throws(()=>{const fd=fs.openSync(${JSON.stringify(join(root, "package.json"))},'r+');fs.closeSync(fd)});
fs.writeFileSync(p.join(process.env.TMPDIR,'probe'),'scratch');console.log('sandbox probe PASS')`]), cwd, env, Math.min(timeoutMs, 5_000));
    const probeOutput = `sandbox probe: exit=${probe.code}; ${probe.problem || ""}\n${probe.stdout}${probe.stderr}`;
    if (probe.code !== 0 || probe.problem || probe.stdout.trim() !== "sandbox probe PASS") {
      return blocked("OS sandbox isolation probe unavailable or denied", probeOutput);
    }
    if (browserApplication) {
      // Resolve, launch and render with the *approved* browser installation
      // inside the OS sandbox before any repository test runs; no snapshot
      // package code is ever imported by the trusted host. An absent or
      // unusable approved browser fails closed with this diagnosis.
      const browserProbe = await captureDevelopmentProcess(isolationTool, wrap(await realpath(process.execPath), ["-e",
        `const {chromium}=require('playwright');chromium.launch({channel:'chrome',headless:true}).then(async b=>{const page=await b.newPage();await page.setContent('<h1>approved-browser-probe</h1>');await page.getByText('approved-browser-probe',{exact:true}).waitFor({timeout:15000});await b.close();console.log('approved browser probe PASS')}).catch(e=>{console.error(e&&e.message||String(e));process.exitCode=1})`]),
        cwd, env, Math.min(timeoutMs, 60_000));
      if (browserProbe.code !== 0 || browserProbe.problem || browserProbe.stdout.trim() !== "approved browser probe PASS") {
        return blocked("approved browser runtime unavailable in the OS sandbox",
          `${probeOutput}\nbrowser runtime probe: exit=${browserProbe.code}; ${browserProbe.problem || ""}\n${browserProbe.stdout}${browserProbe.stderr}`.slice(0, developmentOutputLimit));
      }
    }
    const execution = await captureDevelopmentProcess(isolationTool, wrap(command, args), cwd, env, timeoutMs);
    const output = (probeOutput + "\n" + execution.stdout + execution.stderr).slice(0, developmentOutputLimit);
    const summary = options.kind === "go" ? developmentGoJSON(execution.stdout) : options.kind === "generated" ?
      { tests: 0, failed: 0, skipped: 0, reason: /^Generated contracts freshness: PASS\r?$/m.test(execution.stdout) &&
        !/^Generated contracts freshness: FAIL\r?$/m.test(execution.stdout) ? undefined : "generated checker did not report freshness PASS" } :
      options.kind === "developmentPlan" ?
      { tests: 0, failed: 0, skipped: 0, reason: /^Development plan freshness: PASS\r?$/m.test(execution.stdout) &&
        !/^Development plan freshness: FAIL\r?$/m.test(execution.stdout) ? undefined : "development plan checker did not report freshness PASS" } :
      developmentTAP(execution.stdout, tapTargets, cwd);
    const reason = execution.problem || (execution.code !== 0 ? `check exited ${execution.code}` : summary.reason);
    return { passed: !reason, status: reason ? "failed" : "passed", exitCode: execution.code,
      tests: summary.tests, failed: reason ? Math.max(1, summary.failed) : summary.failed, skipped: summary.skipped,
      output, command: executedCommand, ...(executedCwd ? { cwd: executedCwd } : {}), ...(reason ? { reason } : {}) };
  } catch (error) {
    return blocked(`development check prerequisites unavailable: ${error instanceof Error ? error.message : String(error)}`);
  } finally {
    if (scratch) await rm(scratch, { recursive: true, force: true });
  }
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

export async function runVerification(options: { withPostgres?: boolean; focused?: boolean; base?: string; testTargets?: string[] } = {}, dependencies = defaultDependencies) {
  const { withPostgres = false, focused = false, base, testTargets = [] } = options;
  if (focused && withPostgres) throw new Error("cannot combine focused checks with the exhaustive PostgreSQL lane; supply the focused owner database environment explicitly");
  if (focused && !base) throw new Error("focused verification requires --base <ref>");
  const scope = focused ? focusedChangeScope(base!) : undefined;
  const steps = scope ? focusedVerificationSteps(scope.paths, testTargets, scope.mergeBase) : localVerificationSteps;
  for (const step of steps) await dependencies.runStep(step);
  if (withPostgres) {
    await dependencies.withTemporaryPostgres((env) => dependencies.runPostgresVerification(env));
  }
}

async function main() {
  const options = parseVerifyLocalArgs();
  await runVerification(options);
  const label = options.focused ? `Focused local checks against ${options.base}` : "Local verification";
  process.stdout.write(`\n${label} passed${options.withPostgres ? " with PostgreSQL and Docker integration" : ""}.\n`);
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}
