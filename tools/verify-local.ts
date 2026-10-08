import { execFileSync, spawn } from "node:child_process";
import { constants } from "node:fs";
import { access, mkdir, mkdtemp, readFile, realpath, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parse as parseYAML } from "yaml";

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

type VerificationStep = { name: string; command: string; args: string[]; cwd?: string };

const developmentToolPaths = ["tools/", "tests/tools/", "AGENTS.md", "DEV_GUIDE.md", "package.json", ".github/"];
const generatedContractPaths = ["packages/contracts/proto/", "packages/contracts/go/", "services/gateway-integration/identity/", "apps/console-bff/internal/httpapi/"];

function anyPath(paths: readonly string[], prefixes: readonly string[]) {
  return paths.some((path) => prefixes.some((prefix) => path === prefix || path.startsWith(prefix)));
}

/** Select the smallest local evidence set for an explicit Git change scope. */
export function focusedVerificationSteps(changedPaths: readonly string[]): VerificationStep[] {
  const steps: VerificationStep[] = [];
  const add = (step: VerificationStep) => steps.push(step);
  const hasRepositoryCode = changedPaths.some((path) => !path.startsWith("docs/"));
  if (hasRepositoryCode) add({ name: "product boundary", command: "npm", args: ["run", "validate:product-boundary"] });
  if (anyPath(changedPaths, generatedContractPaths)) {
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
  const testPaths = changedPaths.filter((path) => /^tests\/.*\.test\.[cm]?[jt]s$/u.test(path));
  const browserTests = testPaths.filter((path) => /browser\.test\.[cm]?[jt]s$/u.test(path));
  const nodeTests = testPaths.filter((path) => !browserTests.includes(path));
  if (nodeTests.length) add({ name: "Focused development tests", command: "node", args: ["--test", ...nodeTests] });
  if (browserTests.length) add({ name: "Focused E2E tests", command: "node", args: ["--test", ...browserTests] });
  const changedGoModules = goModules.filter((module) => changedPaths.some((path) => path === module || path.startsWith(`${module}/`)));
  for (const module of changedGoModules) {
    add({ name: `${module} focused Go compile`, command: "go", args: ["test", "-run", "^$", "./..."], cwd: module });
  }
  add({ name: "Git whitespace", command: "git", args: ["diff", "--check"] });
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
    if (token === "--base") {
      if (base !== undefined) throw new Error("verify-local argument --base may be provided once");
      const value = args[++index];
      if (!value || value.startsWith("--")) throw new Error("verify-local argument --base requires a ref");
      base = value;
      continue;
    }
    throw new Error(`unknown verify-local argument: ${token}`);
  }
  if (focused && base === undefined) throw new Error("focused verification requires --base <ref>");
  if (!focused && base !== undefined) throw new Error("--base requires --focused");
  return { withPostgres, ...(focused ? { focused: true, base: base! } : {}) };
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
  reason?: string;
};
type CapturedProcess = { code: number | null; stdout: string; stderr: string; problem?: string };
const developmentOutputLimit = 2_000_000;
const trustedToolDirectories = [dirname(process.execPath), "/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin", "/usr/sbin", "/sbin"];

function inside(directory: string, path: string) {
  const suffix = relative(directory, path);
  return suffix === "" || (!isAbsolute(suffix) && suffix !== ".." && !suffix.startsWith(`..${sep}`));
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
  const reason = !validPlan ? "inconsistent Node TAP summary" : tests === 0 ? "no registered Node tests executed" :
    failed + cancelled + skipped + todo > 0 ? "Node tests failed, cancelled, skipped or TODO" : undefined;
  return { tests, failed: failed + cancelled, skipped: skipped + todo, ...(reason ? { reason } : {}) };
}

function developmentGoJSON(stdout: string) {
  const running = new Set<string>(), terminal = new Map<string, string>();
  const packages = new Map<string, string>(), started = new Set<string>();
  let invalid = false, packageFailures = 0, packageSkips = 0;
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
        if (event.Action === "skip") packageSkips++;
      }
    } catch { invalid = true; }
  }
  const failed = [...terminal.values()].filter((value) => value === "fail").length + packageFailures;
  const skipped = [...terminal.values()].filter((value) => value === "skip").length + packageSkips;
  const tests = terminal.size;
  const complete = running.size === terminal.size && packages.size > 0 && [...started].every((name) => packages.has(name)) &&
    [...running].every((id) => packages.has(id.split("\0")[0]));
  const reason = invalid || !complete ? "invalid or incomplete Go JSON summary" : tests === 0 ? "no Go tests executed" :
    failed + skipped > 0 ? "Go tests failed or skipped" : undefined;
  return { tests, failed, skipped, ...(reason ? { reason } : {}) };
}

export async function runDevelopmentCheck(options: {
  snapshotRoot: string;
  kind: "node" | "go" | "browser" | "generated" | "developmentPlan";
  targets?: string[];
  cwd?: string;
  timeoutMs?: number;
}): Promise<DevelopmentCheckResult> {
  const blocked = (reason: string, output = ""): DevelopmentCheckResult =>
    ({ passed: false, status: "blocked", exitCode: null, tests: 0, failed: 0, skipped: 0, output, reason });
  let scratch: string | undefined;
  try {
    const timeoutMs = options.timeoutMs ?? 120_000;
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
    if (options.kind !== "node" && targets.length) return blocked("targets are supported only for Node checks");
    if (options.kind === "node") {
      if (targets.length === 0) return blocked("Node checks require explicit test file targets");
      for (const target of targets) {
        if (!exactRelativePath(target) || !/\.(?:[cm]?[jt]s)$/.test(target)) return blocked("invalid Node target: flags, traversal and globs are forbidden");
        const file = await realpath(resolve(cwd, target));
        if (!inside(snapshot, file) || !(await stat(file)).isFile()) return blocked("Node target escapes snapshot or is not a file");
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
    let command = await realpath(process.execPath);
    let args: string[] = [];
    let tapTargets = targets;
    // Bind only runtime installations; never the tool's user HOME, signing store,
    // source checkout, Docker socket or a guessed shared cache parent directory.
    const readPaths = new Set<string>([snapshot, command]);
    for (const path of process.platform === "darwin" ?
      ["/System/Library", "/usr/lib", "/usr/bin", "/bin", "/usr/share/zoneinfo", "/Library/Apple/System/Library"] :
      ["/usr/lib", "/usr/lib64", "/lib", "/lib64", "/usr/bin", "/bin", "/usr/share/zoneinfo", "/etc/ld.so.cache", "/etc/localtime"]) {
      try { readPaths.add(path); readPaths.add(await realpath(path)); } catch { /* Absent system installation is not mounted. */ }
    }
    try {
      const dependencies = await realpath(join(snapshot, "node_modules"));
      if (!inside(snapshot, dependencies)) {
        const approvedDependencies = await realpath(join(root, "node_modules"));
        if (dependencies !== approvedDependencies ||
            !(await readFile(join(snapshot, "package-lock.json"))).equals(await readFile(join(root, "package-lock.json")))) {
          return blocked("snapshot dependencies are not the approved locked host installation");
        }
        if (!(await readFile(join(snapshot, "package.json"))).equals(await readFile(join(root, "package.json")))) {
          return blocked("snapshot dependencies require the approved package manifest");
        }
        readPaths.add(dependencies);
      }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    if (options.kind === "node") args = ["--test", "--test-reporter=tap", ...targets];
    if (options.kind === "go" || options.kind === "generated") {
      const go = await trustedTool("go");
      if (options.kind === "go" && !go) return blocked("trusted Go installation unavailable");
      if (go) {
        // Trusted go env reads authoritative host runtime configuration, not a
        // snapshot go.mod or an attacker-supplied command. No tested code runs here.
        const configurationEnv = { ...env, HOME: process.env.HOME, GOENV: process.env.GOENV,
          GOMODCACHE: process.env.GOMODCACHE, GOROOT: process.env.GOROOT };
        const configuration = await captureDevelopmentProcess(go, ["env", "-json", "GOROOT", "GOMODCACHE"], temporaryRoot, configurationEnv, 10_000);
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
      }
    }
    if (options.kind === "generated") {
      const checker = await realpath(join(snapshot, "tools/verify-generated-contracts.ts"));
      if (!inside(snapshot, checker)) return blocked("generated checker escapes snapshot");
      args = ["tools/verify-generated-contracts.ts"];
      for (const name of ["python3", "protoc", "buf"]) {
        const tool = await trustedTool(name);
        if (tool && !inside(snapshot, tool)) {
          readPaths.add(tool);
          // Homebrew's versioned runtime is a tool installation, not user data.
          if (tool.startsWith("/opt/homebrew/Cellar/")) readPaths.add(tool.split("/").slice(0, 6).join("/"));
        }
      }
    }
    if (options.kind === "developmentPlan") {
      const checker = await realpath(join(snapshot, "tools/verify-development-plan.ts"));
      if (!inside(snapshot, checker)) return blocked("development plan checker escapes snapshot");
      args = ["tools/verify-development-plan.ts"];
      // The plan gate regenerates into an isolated tree and byte-compares; it only
      // needs the trusted python3 interpreter, never protoc or the network.
      // On macOS /usr/bin/python3 is an Xcode selector, not an interpreter.
      // Resolve the installed runtime from a fixed host installation location;
      // do not grant the worker access to developer selection/configuration.
      const pythonDirectories = process.platform === "darwin" ?
        ["/Library/Developer/CommandLineTools/usr/bin", ...trustedToolDirectories] : trustedToolDirectories;
      let tool: string | undefined;
      for (const directory of pythonDirectories) {
        try {
          const candidate = join(directory, "python3");
          await access(candidate, constants.X_OK);
          if ((await stat(candidate)).isFile()) { tool = await realpath(candidate); break; }
        } catch { /* Only fixed runtime installation locations are eligible. */ }
      }
      if (!tool || inside(snapshot, tool)) return blocked("trusted Python runtime unavailable");
      readPaths.add(tool);
      const framework = "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/";
      if (tool.startsWith(framework)) readPaths.add(resolve(dirname(tool), ".."));
      else if (tool.startsWith("/opt/homebrew/Cellar/")) readPaths.add(tool.split("/").slice(0, 6).join("/"));
      env.PATH = dirname(tool) + ":" + env.PATH;
    }
    if (options.kind === "browser") {
      if (!Buffer.from(await readFile(join(snapshot, "package.json"))).equals(await readFile(join(root, "package.json")))) {
        return blocked("browser snapshot package.json is not the approved host manifest");
      }
      const manifest = JSON.parse(await readFile(join(snapshot, "package.json"), "utf8"));
      const suite = manifest.scripts?.["test:browser:suite"];
      if (typeof suite !== "string") return blocked("approved browser suite is unavailable");
      tapTargets = suite.split(/\s+/).filter((token: string) => exactRelativePath(token) && /\.test\.(?:[cm]?[jt]s)$/.test(token));
      if (tapTargets.length === 0) return blocked("approved browser suite has no exact test targets");
      const dependency = await realpath(join(snapshot, "node_modules/playwright/package.json"));
      if (!inside(snapshot, dependency) && ![...readPaths].some((path) => path.endsWith("/node_modules") && inside(path, dependency))) return blocked("browser dependencies are not the approved snapshot installation");
      const npm = await trustedTool("npm");
      if (!npm) return blocked("trusted npm installation unavailable");
      readPaths.add(resolve(dirname(npm), ".."));
      args = [npm, "run", "test:browser:suite"];
      env.NODE_OPTIONS = "--test-reporter=tap";
      env.PLAYWRIGHT_BROWSERS_PATH = join(snapshot, ".cache/ms-playwright");
      env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
    }
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
(allow network-outbound (remote ip "localhost:*"))`;
      wrap = (executable, arguments_) => ["-p", profile, executable, ...arguments_];
    } else {
      const mounts = [...readPaths].flatMap((path) => ["--ro-bind", path, path]);
      // A private network namespace has no host routes/interfaces. Bubblewrap
      // sets up its loopback interface; no host Unix sockets are bound here.
      const prefix = ["--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL",
        "--proc", "/proc", "--dev", "/dev", ...mounts, "--bind", scratch, scratch, "--chdir", cwd];
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
    if (options.kind === "browser") {
      // Resolve/load and launch the approved browser dependency *inside* the OS
      // sandbox; no snapshot package code is ever imported by the trusted host.
      const browserProbe = await captureDevelopmentProcess(isolationTool, wrap(await realpath(process.execPath), ["-e",
        "require('playwright').chromium.launch({headless:true}).then(async b=>{await b.close();console.log('browser dependency probe PASS')}).catch(e=>{console.error(e.message);process.exitCode=1})"]),
        cwd, env, Math.min(timeoutMs, 10_000));
      if (browserProbe.code !== 0 || browserProbe.problem || browserProbe.stdout.trim() !== "browser dependency probe PASS") {
        return blocked("approved browser runtime dependencies unavailable in OS sandbox",
          `${probeOutput}\nbrowser dependency probe: exit=${browserProbe.code}; ${browserProbe.problem || ""}\n${browserProbe.stdout}${browserProbe.stderr}`.slice(0, developmentOutputLimit));
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
      output, ...(reason ? { reason } : {}) };
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

function printStep(name: string) {
  process.stdout.write(`\n==> ${name}\n`);
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
  await runProcess(step.command, step.args, { cwd: stepCwd(step), env: stepEnv() });
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

async function withTemporaryPostgres(callback: (env: NodeJS.ProcessEnv) => Promise<unknown>) {
  const containerName = `opl-cloud-verify-${process.pid}-${randomUUID().slice(0, 8)}`;
  let started = false;
  const stop = async () => {
    if (!started) return;
    await runProcess("docker", ["rm", "--force", containerName], { capture: true, allowFailure: true });
    started = false;
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
      postgresImage
    ]);
    started = true;
    await waitForHealthyPostgres(containerName);
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
    return await callback(postgresEnv);
  } finally {
    process.removeListener("SIGINT", interrupt);
    process.removeListener("SIGTERM", interrupt);
    await stop();
  }
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

function changedPathsForBase(base: string) {
  const git = (args: string[]) => execFileSync("git", ["-C", root, ...args], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024 });
  const baseSha = git(["rev-parse", "--verify", "--end-of-options", `${base}^{commit}`]).trim();
  const split = (value: string) => value.split("\0").filter(Boolean);
  return [...new Set([
    ...split(git(["diff", "--name-only", "-z", "--no-renames", `${baseSha}...HEAD`, "--"])),
    ...split(git(["diff", "--cached", "--name-only", "-z", "--no-renames", baseSha, "--"])),
    ...split(git(["diff", "--name-only", "-z", "--no-renames", "--"])),
    ...split(git(["ls-files", "--others", "--exclude-standard", "-z"]))
  ])].sort();
}

export async function runVerification(options: { withPostgres?: boolean; focused?: boolean; base?: string } = {}, dependencies = defaultDependencies) {
  const { withPostgres = false, focused = false, base } = options;
  if (focused && !base) throw new Error("focused verification requires --base <ref>");
  const steps = focused ? focusedVerificationSteps(changedPathsForBase(base!)) : localVerificationSteps;
  for (const step of steps) await dependencies.runStep(step);
  if (withPostgres) {
    await dependencies.withTemporaryPostgres((env) => dependencies.runPostgresVerification(env));
  }
}

async function main() {
  const options = parseVerifyLocalArgs();
  await runVerification(options);
  const mode = options.focused ? ` focused verification against ${options.base}` : "";
  process.stdout.write(`\nLocal${mode} verification passed${options.withPostgres ? " with PostgreSQL and Docker integration" : ""}.\n`);
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}
