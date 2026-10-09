import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { execFile as execFileCallback } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import test from "node:test";
import { parse as parseYAML } from "yaml";
import { generationInputs, generatedOutputs } from "../../tools/verify-generated-contracts.ts";

import {
  databaseFreeGoTestSpecs,
  goModules,
  localVerificationSteps,
  parseVerifyLocalArgs,
  postgresImage,
  postgresVerificationSpecs,
  runDevelopmentCheck,
  runVerification,
  focusedVerificationSteps,
  focusedChangeScope,
  summarizeGoTestFailures
} from "../../tools/verify-local.ts";

test("verify-local exposes one default gate across Node, builds, and every Go module", () => {
  assert.deepEqual(parseVerifyLocalArgs([]), { withPostgres: false });
  assert.deepEqual(parseVerifyLocalArgs(["--with-postgres"]), { withPostgres: true });
  assert.throws(() => parseVerifyLocalArgs(["--production"]), /unknown verify-local argument/);

  const names = localVerificationSteps.map((step) => step.name);
  for (const expected of [
    "product boundary",
    "Generated contracts freshness",
    "Node source tests",
    "Console browser suite",
    "TypeScript typecheck",
    "TypeScript lint",
    "Console build",
    "Git whitespace"
  ]) {
    assert.ok(names.includes(expected), `missing ${expected}`);
  }
  for (const module of goModules) {
    assert.ok(names.includes(`${module} compile`));
  }
  for (const spec of databaseFreeGoTestSpecs) {
    assert.ok(names.includes(`${spec.cwd} database-free tests`));
  }
  assert.ok(goModules.includes("packages/contracts/go"));
  assert.deepEqual(
    databaseFreeGoTestSpecs.find((spec) => spec.cwd === "packages/contracts/go"),
    { cwd: "packages/contracts/go", packages: ["./..."] }
  );
});

test("focused verification selects only gates affected by an explicit change set", () => {
  assert.deepEqual(parseVerifyLocalArgs(["--focused", "--base", "origin/main"]), {
    withPostgres: false,
    focused: true,
    base: "origin/main"
  });
  assert.throws(() => parseVerifyLocalArgs(["--focused"]), /requires --base/);
  assert.throws(() => parseVerifyLocalArgs(["--base", "origin/main"]), /requires --focused/);

  const selected = focusedVerificationSteps([
    "tools/dev-session.ts",
    "tests/tools/dev-session.test.ts",
    "docs/spec/target/checks/development_plan.json",
    "services/fabric/internal/fabric/runtime.go",
    "services/fabric/internal/fabric/runtime_test.go"
  ]).map((step) => step.name);
  assert.deepEqual(selected, [
    "product boundary",
    "Development plan freshness",
    "Development tools typecheck",
    "Focused development tests",
    "services/fabric focused Go tests",
    "Git whitespace"
  ]);

  const docsOnly = focusedVerificationSteps(["docs/README.md"]).map((step) => step.name);
  assert.deepEqual(docsOnly, ["Git whitespace"]);
  assert.ok(focusedVerificationSteps(["apps/console-ui/src/main.tsx"], ["tests/ui/console-model.test.ts"]).some((step) => step.name === "TypeScript typecheck"));
  const browser = focusedVerificationSteps(["tests/ui/gateway-account-read-controller-browser.test.ts"]).map((step) => step.name);
  assert.deepEqual(browser, ["product boundary", "Focused E2E tests", "Git whitespace"]);
});

test("focused source verification requires declared behavior targets instead of passing typecheck alone", () => {
  assert.throws(() => focusedVerificationSteps(["apps/console-ui/src/main.tsx"]), /behavior targets.*--test/);
  const steps = focusedVerificationSteps(["apps/console-ui/src/main.tsx"], ["tests/ui/console-model.test.ts"]);
  assert.deepEqual(steps.find(step => step.name === "Focused development tests")?.args,
    ["--test", "--test-reporter=tap", "tests/ui/console-model.test.ts"]);
  assert.deepEqual(parseVerifyLocalArgs(["--focused", "--base", "origin/main", "--test", "tests/ui/console-model.test.ts"]),
    { withPostgres: false, focused: true, base: "origin/main", testTargets: ["tests/ui/console-model.test.ts"] });
  assert.throws(() => parseVerifyLocalArgs(["--test", "tests/ui/console-model.test.ts"]), /requires --focused/);
});

test("focused Go verification executes the affected package instead of only compiling its module", () => {
  const steps = focusedVerificationSteps(["services/fabric/internal/fabric/runtime.go"]);
  assert.deepEqual(steps.find(step => step.name === "services/fabric focused Go tests")?.args,
    ["test", "-count=1", "-json", "./internal/fabric"]);
  assert.ok(!steps.some(step => step.args.includes("^$")));
});

test("focused Go entrypoint changes include the module behavior suite when the entrypoint has no tests", () => {
  const steps = focusedVerificationSteps(["services/runtime-control/cmd/server/main.go"]);
  assert.deepEqual(steps.find(step => step.name === "services/runtime-control focused Go tests")?.args,
    ["test", "-count=1", "-json", "./..."]);
});

test("focused freshness covers every authoritative generator input and output", () => {
  for (const path of [...generationInputs, ...generatedOutputs]) {
    const steps = focusedVerificationSteps([path], ["tests/tools/verify-generated-contracts.test.ts"]);
    assert.ok(steps.some(step => step.name === "Generated contracts freshness"), path);
  }
});

test("focused E2E selection follows the existing browser inventory, not filename guesses", () => {
  const steps = focusedVerificationSteps(["tests/ui/workspace-mvp-flow.test.ts"]);
  assert.deepEqual(steps.find(step => step.name === "Focused E2E tests")?.args,
    ["--test", "--test-reporter=tap", "tests/ui/workspace-mvp-flow.test.ts"]);
  assert.ok(!steps.some(step => step.name === "Focused development tests"));
});

test("focused verification never executes a deleted test and requires a current replacement target", () => {
  const deleted = "tests/tools/retired-focused-behavior.test.ts";
  assert.throws(() => focusedVerificationSteps([deleted]), /behavior targets.*--test/);
  const steps = focusedVerificationSteps([deleted], ["tests/tools/verify-local.test.ts"]);
  assert.deepEqual(steps.find(step => step.name === "Focused development tests")?.args,
    ["--test", "--test-reporter=tap", "tests/tools/verify-local.test.ts"]);
  assert.throws(() => focusedVerificationSteps([], [deleted]), /focused test target does not exist/);
});

test("focused Git scope excludes upstream-only changes and includes committed, staged, unstaged and untracked inputs", async t => {
  const repo = await mkdtemp(join(tmpdir(), "opl-focused-git-"));
  t.after(() => rm(repo, { recursive: true, force: true }));
  const run = promisify(execFileCallback);
  const git = (...args: string[]) => run("git", ["-C", repo, ...args]);
  await git("init", "-q");
  await writeFile(join(repo, "initial.txt"), "baseline\n");
  await git("add", "."); await git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "baseline");
  const base = (await git("rev-parse", "HEAD")).stdout.trim();
  await git("checkout", "-qb", "upstream");
  await writeFile(join(repo, "upstream-only.txt"), "upstream\n");
  await git("add", "."); await git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "upstream only");
  await git("checkout", "-qb", "owner", base);
  await writeFile(join(repo, "committed.txt"), "owner\n");
  await git("add", "."); await git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "owner change");
  await writeFile(join(repo, "staged.txt"), "staged\n"); await git("add", "staged.txt");
  await writeFile(join(repo, "initial.txt"), "unstaged\n");
  await writeFile(join(repo, "untracked.txt"), "untracked\n");
  const scope = focusedChangeScope("upstream", repo);
  assert.equal(scope.mergeBase, base);
  assert.deepEqual(scope.paths, ["committed.txt", "initial.txt", "staged.txt", "untracked.txt"]);
  const whitespace = focusedVerificationSteps([], [], scope.mergeBase).filter(step => step.command === "git");
  assert.deepEqual(whitespace.map(step => step.args), [
    ["diff", "--check", base, "HEAD", "--"], ["diff", "--cached", "--check", "--"], ["diff", "--check", "--"]
  ]);
});

test("focused verification refuses an implicit exhaustive PostgreSQL lane", () => {
  assert.throws(() => parseVerifyLocalArgs(["--focused", "--base", "origin/main", "--with-postgres"]), /cannot combine.*focused.*PostgreSQL/);
});

test("Qualification executes the independent Go contracts module", async () => {
  const qualification = parseYAML(await readFile(".github/workflows/qualification.yml", "utf8"));
  const contractJob = qualification.jobs.go_contracts;
  assert.equal(contractJob.name, "go-contracts");
  const setup = contractJob.steps.find((step: any) => step.name === "Set up Go").with;
  assert.equal(setup.cache, false);
  assert.equal(setup["go-version"], undefined);
  assert.equal(setup["go-version-file"], "packages/contracts/go/go.mod");
  assert.ok(contractJob.steps.some((step: any) => step["working-directory"] === "packages/contracts/go"
    && step.run === "go test -count=1 ./..."));
  assert.ok(qualification.jobs.validate.needs.includes("go_contracts"));
  const validateStep = qualification.jobs.validate.steps.find((step: any) => step.name === "Require successful test jobs");
  assert.equal(
    validateStep.env.GO_CONTRACTS_RESULT,
    "${{ needs.go_contracts.result }}"
  );
  assert.match(validateStep.run, /"\$GO_CONTRACTS_RESULT" != "success"/);
});

test("Local qualification uses one bounded runner filesystem and explicit privileged inputs", async (t) => {
  const workflow = parseYAML(await readFile(".github/workflows/qualification.yml", "utf8"));
  const nodeStep = workflow.jobs.node_console.steps.find((item: any) => item.name === "Test Node");
  const packageScripts = JSON.parse(await readFile("package.json", "utf8")).scripts;
  const browserConcurrency = packageScripts["test:browser:suite"].match(/--test-concurrency=\d+/)?.[0];
  assert.ok(browserConcurrency);
  assert.ok(nodeStep.run.includes("npm run test:source"));
  assert.ok(nodeStep.run.includes("npm run test:browser:suite"));
  assert.ok(nodeStep.run.includes("Node SKIP result missing or nonzero"));
  assert.ok(nodeStep.run.includes("(?:#|ℹ) fail"));
  assert.ok(nodeStep.run.includes("(?:#|ℹ) skipped"));
  const job = workflow.jobs.fabric;
  assert.equal(job["runs-on"], "ubuntu-latest");
  assert.equal(job.environment, undefined);
  assert.deepEqual(workflow.permissions, { contents: "read" });
  const step = (name: string) => job.steps.find((item: any) => item.name === name);
  const initialize = step("Initialize Local qualification directory");
  const quotaSupport = step("Load runner project quota support");
  const prepare = step("Prepare project quota filesystem");
  const compile = step("Compile Local qualification executables without privilege");
  const fabric = step("Test Fabric");
  assert.equal(fabric.env.OPL_OWNER_MIGRATION_TEST_ADMIN_DSN, "postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable");
  const quota = step("Test Linux project quota as privileged capability");
  const deploy = step("Test first Local application deployment with real owners");
  const cleanup = step("Remove project quota filesystem");
  assert.equal(cleanup.if, "${{ always() }}");
  // Runner paths are unavailable in job-level env expressions. Execute their
  // initialization on the runner and persist them for later steps instead.
  assert.equal(job.env.OPL_QUALIFICATION_ROOT, undefined);
  assert.ok(job.steps.indexOf(initialize) < job.steps.indexOf(quotaSupport));
  assert.ok(job.steps.indexOf(quotaSupport) < job.steps.indexOf(prepare));
  assert.ok(job.steps.indexOf(compile) < job.steps.indexOf(quota));
  assert.ok(job.steps.indexOf(quota) < job.steps.indexOf(deploy));
  assert.ok(job.steps.indexOf(deploy) < job.steps.indexOf(cleanup));
  assert.doesNotMatch(compile.run, /\bsudo\b/);
  const run = promisify(execFileCallback);
  for (const item of [initialize, quotaSupport, prepare, compile, quota, deploy, cleanup]) await run("bash", ["-n", "-c", item.run]);

  const temporary = await mkdtemp(join(tmpdir(), "opl-qualification-shell-"));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  const bin = join(temporary, "bin"), log = join(temporary, "commands.jsonl");
  await mkdir(bin);
  // Mock privileged/system commands, but execute the actual workflow shell.
  // These mocks never mount, format, truncate or remove a real filesystem.
  const mock = `#!${process.execPath}\nconst fs = require('node:fs'); const path = require('node:path');
const command = path.basename(process.argv[1]), args = process.argv.slice(2);
fs.appendFileSync(process.env.QUALIFICATION_COMMAND_LOG, JSON.stringify({command,args})+'\\n');
if (command === 'uname') process.stdout.write('qualification-test-kernel\\n');
if (command === 'modinfo' && !fs.existsSync(process.env.QUALIFICATION_MODULE_MARKER)) process.exit(1);
if (command === 'sudo' && args[0] === 'modprobe' && (!fs.existsSync(process.env.QUALIFICATION_MODULE_MARKER) || process.env.QUALIFICATION_MODULE_LOAD_FAIL === '1')) process.exit(1);
if (command === 'sudo' && args[0] === 'apt-get' && args[1] === 'install') fs.writeFileSync(process.env.QUALIFICATION_MODULE_MARKER, 'present');
if (command === 'df') process.stdout.write('Avail\\n'+process.env.QUALIFICATION_AVAILABLE_BYTES+'\\n');
if (command === 'mountpoint') process.exit(process.env.QUALIFICATION_MOUNTED === '1' ? 0 : 1);
if (command === 'sudo' && args[0] === 'umount' && process.env.QUALIFICATION_UNMOUNT_FAIL === '1') process.exit(1);
`;
  for (const command of ["df", "truncate", "mkfs.ext4", "sudo", "mountpoint", "uname", "modinfo"]) {
    const path = join(bin, command);
    await writeFile(path, mock); await chmod(path, 0o755);
  }
  const root = join("/tmp", "opl-local-first-deploy-test-1");
  await rm(root, { recursive: true, force: true });
  t.after(() => rm(root, { recursive: true, force: true }));
  const env: NodeJS.ProcessEnv = { ...process.env, PATH: `${bin}:${process.env.PATH}`, RUNNER_TEMP: temporary, OPL_QUALIFICATION_ROOT: root,
    GITHUB_RUN_ID: "test", GITHUB_RUN_ATTEMPT: "1", QUALIFICATION_COMMAND_LOG: log, QUALIFICATION_AVAILABLE_BYTES: String(16 * 1024 ** 3), QUALIFICATION_MOUNTED: "1", QUALIFICATION_MODULE_MARKER: join(temporary, "quota-module-present") };
  const githubEnv = join(temporary, "github-env");
  const initialEnv: NodeJS.ProcessEnv = { ...env, GITHUB_ENV: githubEnv };
  delete initialEnv.OPL_QUALIFICATION_ROOT;
  await run("bash", ["-c", initialize.run], { env: initialEnv });
  assert.equal(await readFile(githubEnv, "utf8"), `OPL_QUALIFICATION_ROOT=${root}\n`);
  const commands = async () => (await readFile(log, "utf8")).trim().split("\n").filter(Boolean).map((line) => JSON.parse(line));
  await run("bash", ["-c", quotaSupport.run], { env });
  let calls = await commands();
  assert.ok(calls.some((call) => call.command === "sudo" && call.args.join(" ") === "apt-get install --no-install-recommends -y linux-modules-extra-qualification-test-kernel"));
  await writeFile(log, "");
  await run("bash", ["-c", quotaSupport.run], { env });
  assert.ok(!(await commands()).some((call) => call.command === "sudo" && call.args[0] === "apt-get"));
  await writeFile(log, "");
  await assert.rejects(run("bash", ["-c", quotaSupport.run], { env: { ...env, QUALIFICATION_MODULE_LOAD_FAIL: "1" } }));
  assert.ok(!(await commands()).some((call) => call.command === "sudo" && call.args[0] === "apt-get"));
  await writeFile(log, "");
  await run("bash", ["-c", prepare.run], { env });
  calls = await commands();
  assert.ok(calls.some((call) => call.command === "truncate" && call.args.join(" ") === `-s 12G ${root}/quota.img`));
  const format = calls.find((call) => call.command === "mkfs.ext4");
  assert.ok(format.args.includes("project,quota") && format.args.includes("quotatype=prjquota"));
  assert.ok(calls.some((call) => call.command === "sudo" && call.args.join(" ") === `mount -o loop,prjquota ${root}/quota.img ${root}/storage`));
  for (const item of [quota, deploy]) await run("bash", ["-c", item.run], { env });
  calls = await commands();
  const executions = calls.filter((call) => call.command === "sudo" && call.args[0] === "env");
  assert.equal(executions.length, 2);
  for (const execution of executions) {
    assert.equal(execution.args[1], "-i");
    assert.ok(execution.args.includes(`OPL_TEST_PROJECT_QUOTA_ROOT=${root}/storage`));
    assert.ok(execution.args.includes(`HOME=${root}/home`) && execution.args.includes(`TMPDIR=${root}/tmp`));
    assert.ok(!execution.args.includes("go"));
  }
  assert.ok(executions[1].args.includes(`OPL_QUALIFICATION_FABRIC_BINARY=${root}/fabric`));
  assert.ok(executions[1].args.includes(`OPL_QUALIFICATION_SERVE_BINARY=${root}/serve`));
  assert.ok(executions[1].args.includes("OPL_OWNER_MIGRATION_TEST_ADMIN_DSN=postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable"));
  assert.ok(executions[1].args.includes("^TestLocalFirstApplicationDeploymentQualification$"));
  await writeFile(log, "");
  await assert.rejects(run("bash", ["-c", cleanup.run], { env: { ...env, QUALIFICATION_UNMOUNT_FAIL: "1" } }));
  assert.ok(!(await commands()).some((call) => call.command === "sudo" && call.args[0] === "rm"));
  await writeFile(log, "");
  await run("bash", ["-c", cleanup.run], { env });
  calls = await commands();
  assert.ok(calls.some((call) => call.command === "sudo" && call.args.join(" ") === `umount ${root}/storage`));
  assert.ok(calls.some((call) => call.command === "sudo" && call.args.join(" ") === `rm -rf --one-file-system -- ${root}`));
  await writeFile(log, "");
  // The prepare step requires "$OPL_QUALIFICATION_ROOT" to equal
  // "/tmp/opl-local-first-deploy-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT" and to not
  // already exist. Give the low-space case its own run id so the check never
  // depends on a leftover directory from an earlier or concurrent run, and clean
  // it up afterwards.
  const lowSpaceRunId = `${process.pid}-${Date.now().toString(36)}`;
  const lowSpaceRoot = `/tmp/opl-local-first-deploy-${lowSpaceRunId}-1`;
  await rm(lowSpaceRoot, { recursive: true, force: true });
  t.after(() => rm(lowSpaceRoot, { recursive: true, force: true }));
  await assert.rejects(run("bash", ["-c", prepare.run], { env: { ...env, GITHUB_RUN_ID: lowSpaceRunId, GITHUB_RUN_ATTEMPT: "1", OPL_QUALIFICATION_ROOT: lowSpaceRoot, QUALIFICATION_AVAILABLE_BYTES: "1024" } }));
  assert.deepEqual((await commands()).map((call) => call.command), ["df"]);
  await writeFile(log, "");
  await assert.rejects(run("bash", ["-c", cleanup.run], { env: { ...env, OPL_QUALIFICATION_ROOT: temporary } }));
  assert.equal((await commands()).length, 0);
});

test("verify-local reports leaf Go test failures separately from package failures", () => {
  const summary = summarizeGoTestFailures([
    { Action: "fail", Package: "opl-cloud/services/fabric/internal/fabric", Test: "TestRuntime/invalid" },
    { Action: "fail", Package: "opl-cloud/services/fabric/internal/fabric", Test: "TestRuntime" },
    { Action: "fail", Package: "opl-cloud/services/fabric/internal/fabric" },
    { Action: "fail", Package: "opl-cloud/services/control-plane/internal/server", Test: "TestLaunch" },
    { Action: "fail", Package: "opl-cloud/services/control-plane/internal/server" }
  ]);
  assert.deepEqual(summary, {
    tests: [
      { package: "opl-cloud/services/control-plane/internal/server", name: "TestLaunch" },
      { package: "opl-cloud/services/fabric/internal/fabric", name: "TestRuntime/invalid" }
    ],
    packages: [
      "opl-cloud/services/control-plane/internal/server",
      "opl-cloud/services/fabric/internal/fabric"
    ]
  });
});

test("full local gate covers every PostgreSQL owner with the CI-only extensions", async () => {
  const compose = parseYAML(await readFile("compose.yaml", "utf8"));
  assert.equal(postgresImage, compose.services.postgres.image);
  assert.match(postgresImage, /^postgres:[^\s@]+@sha256:[0-9a-f]{64}$/);
  assert.notEqual(postgresImage, "postgres:16");
  assert.deepEqual(postgresVerificationSpecs.map((spec) => spec.cwd), [
    "services/internal/postgresmigrate",
    "services/internal/ownerservice",
    "services/capability",
    "services/gateway-integration",
    "services/build",
    "services/runtime-control",
    "services/workspace",
    "services/resource-catalog",
    "services/serve",
    "services/ledger",
    "services/control-plane",
    "services/fabric"
  ]);
  assert.equal(postgresVerificationSpecs[0].race, true);
  assert.equal(postgresVerificationSpecs.find((spec) => spec.cwd === "services/control-plane")!.timeout, "15m");
});

test("full verification adds the temporary PostgreSQL modules after the default checks", async () => {
  const events: string[] = [];
  const env = { OPL_POSTGRES_TESTS: "1" };
  const dependencies = {
    runStep: async (step: (typeof localVerificationSteps)[number]) => { events.push(`step:${step.name}`); },
    withTemporaryPostgres: async (callback: (env: NodeJS.ProcessEnv) => Promise<unknown>) => {
      events.push("postgres:start");
      try {
        await callback(env);
      } finally {
        events.push("postgres:stop");
      }
    },
    runPostgresVerification: async (actualEnv: NodeJS.ProcessEnv) => {
      assert.equal(actualEnv, env);
      events.push("postgres:tests");
    }
  };

  await runVerification({ withPostgres: true }, dependencies);
  assert.equal(events[0], `step:${localVerificationSteps[0].name}`);
  assert.deepEqual(events.slice(-3), ["postgres:start", "postgres:tests", "postgres:stop"]);

  events.length = 0;
  await runVerification({ withPostgres: false }, dependencies);
  assert.equal(events.some((event) => event.startsWith("postgres:")), false);
});

// Use the same temporary-directory/after-cleanup fixture pattern as the existing
// qualification tests. All outside-snapshot secrets below are synthetic controls.
async function developmentFixture(t: import("node:test").TestContext) {
  const directory = await mkdtemp(join(tmpdir(), "opl-development-check-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const snapshotRoot = join(directory, "snapshot");
  await mkdir(join(snapshotRoot, "tests"), { recursive: true });
  return { directory, snapshotRoot };
}

test("development check uses real isolation: writable scratch, read-only snapshot, no host key access or inherited secrets", async (t) => {
  const { directory, snapshotRoot } = await developmentFixture(t);
  const key = join(directory, "host-signing-key");
  await writeFile(key, "synthetic signing control");
  const file = join(snapshotRoot, "tests/boundary.test.mjs");
  await writeFile(file, `import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
test('OS boundary', () => {
  fs.writeFileSync(path.join(process.env.TMPDIR, 'scratch-control'), 'writable');
  assert.throws(() => fs.readFileSync(${JSON.stringify(key)}));
  try { fs.writeFileSync(${JSON.stringify(key)}, 'escaped'); if (process.platform === 'darwin') throw new Error('host key write unexpectedly succeeded'); }
  catch (error) { if (String(error.message).includes('unexpectedly succeeded')) throw error; }
  assert.throws(() => fs.writeFileSync(${JSON.stringify(file)}, 'escaped'));
  assert.equal(process.env.OPL_DEVELOPMENT_TEST_SECRET, undefined);
});`);
  const original = process.env.OPL_DEVELOPMENT_TEST_SECRET;
  process.env.OPL_DEVELOPMENT_TEST_SECRET = "must-not-inherit";
  try {
    const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/boundary.test.mjs"] });
    if (result.status === "blocked") {
      assert.equal(result.passed, false);
      assert.match(result.reason || "", /sandbox|isolation/i);
      assert.match(result.output, /probe|unavailable/i);
      t.diagnostic(result.output);
    } else {
      assert.equal(result.status, "passed", result.output + result.reason);
      assert.equal(result.tests, 1);
    }
    assert.equal(await readFile(key, "utf8"), "synthetic signing control");
  } finally {
    if (original === undefined) delete process.env.OPL_DEVELOPMENT_TEST_SECRET;
    else process.env.OPL_DEVELOPMENT_TEST_SECRET = original;
  }
});

test("development check rejects exit zero without registered Node tests", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  await writeFile(join(snapshotRoot, "tests/empty.test.mjs"), "console.log('empty control'); process.exit(0);\n");
  const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/empty.test.mjs"] });
  assert.equal(result.passed, false, result.output);
  assert.notEqual(result.status, "passed");
  if (result.status !== "blocked") {
    assert.equal(result.exitCode, 0);
    assert.match(result.reason || "", /no.*tests|zero.*tests/i);
  }
});


test("development check rejects an empty owner target even when another target executes real tests", async t => {
  const { snapshotRoot } = await developmentFixture(t);
  await writeFile(join(snapshotRoot, "tests/empty.test.mjs"), "console.log('empty owner');\n");
  await writeFile(join(snapshotRoot, "tests/real.test.mjs"), "import test from 'node:test'; test('owner behavior', () => {});\n");
  const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/empty.test.mjs", "tests/real.test.mjs"] });
  assert.equal(result.passed, false, result.output);
  if (result.status !== "blocked") assert.match(result.reason || "", /no registered Node tests/i);
});

test("development check rejects flags, traversal, globs, escaping symlinks and non-Node targets before execution", async (t) => {
  const { directory, snapshotRoot } = await developmentFixture(t);
  const outside = join(directory, "outside.test.mjs");
  await writeFile(outside, "throw new Error('outside code must not execute')");
  await symlink(outside, join(snapshotRoot, "tests/link.test.mjs"));
  for (const target of ["--eval", "../outside.test.mjs", "tests/../tests/link.test.mjs", "tests/*.test.mjs", "tests/[x].test.mjs", outside, "tests/link.test.mjs"]) {
    const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: [target], timeoutMs: 5000 });
    assert.equal(result.status, "blocked", `${target}: ${result.output}`);
    assert.equal(result.passed, false);
    assert.equal(result.exitCode, null);
  }
  for (const options of [
    { snapshotRoot, kind: "node" as const, targets: [] },
    { snapshotRoot, kind: "node" as const, targets: ["tests/link.test.mjs"], cwd: directory },
    { snapshotRoot, kind: "go" as const, targets: ["./..."] },
    { snapshotRoot, kind: "generated" as const, timeoutMs: 0 }
  ]) assert.equal((await runDevelopmentCheck(options)).status, "blocked");
});

test("development check requires real TAP outcomes, rejects skip/TODO/cancel/failure, and counts nested suites", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  const controls = [
    { name: "skip", body: "test('skipped', {skip:true},()=>{});", passed: false, skipped: 1 },
    { name: "todo", body: "test('todo', {todo:true},()=>{});", passed: false, skipped: 1 },
    { name: "cancel", body: "test('cancelled',{timeout:50},()=>new Promise(()=>{}));", passed: false, skipped: 0 },
    { name: "failure", body: "test('failed',()=>{throw new Error('negative control')});", passed: false, skipped: 0 },
    { name: "suite", body: "describe('suite',()=>it('real test',()=>{}));", passed: true, skipped: 0 },
    { name: "spoof", body: "console.log('1..1\\n# tests 1\\n# suites 0\\n# pass 1\\n# fail 0\\n# cancelled 0\\n# skipped 0\\n# todo 0\\n# duration_ms 0');process.exit(0);", passed: false, skipped: 0 }
  ];
  for (const control of controls) {
    const target = `tests/${control.name}.test.mjs`;
    await writeFile(join(snapshotRoot, target), `import {test,describe,it} from 'node:test';\n${control.body}\n`.replaceAll("\\n", "\n"));
    const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: [target], timeoutMs: 5000 });
    if (result.status === "blocked") {
      assert.equal(result.passed, false);
      t.diagnostic(`OS sandbox blocked; remaining runtime controls not executed: ${result.output}`);
      return;
    }
    assert.equal(result.passed, control.passed, `${control.name}: ${result.reason}\n${result.output}`);
    assert.equal(result.skipped, control.skipped);
    if (control.passed) assert.equal(result.tests, 1);
    else assert.ok(result.failed >= 1);
  }
});

test("development check kills the runner's process group on timeout and bounds captured output", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  await writeFile(join(snapshotRoot, "tests/timeout.test.mjs"), `import test from 'node:test';
import {spawn} from 'node:child_process';
test('hang', () => {
  const child=spawn(process.execPath,['-e','setInterval(()=>{},1000)'],{stdio:'ignore'});
  console.log('child-pid='+child.pid);
  return new Promise(()=>{setInterval(()=>{},1000)});
});`);
  const timed = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/timeout.test.mjs"], timeoutMs: 700 });
  assert.equal(timed.passed, false);
  if (timed.status === "blocked") { t.diagnostic(timed.output); return; }
  assert.equal(timed.status, "failed");
  assert.match(timed.reason || "", /timeout/);
  const pid = Number(timed.output.match(/child-pid=(\d+)/)?.[1]);
  assert.ok(pid > 0, timed.output);
  await new Promise((done) => setTimeout(done, 100));
  if (process.platform === 'darwin') assert.throws(() => process.kill(pid, 0), /ESRCH/);
  await writeFile(join(snapshotRoot, "tests/flood.test.mjs"), "process.stdout.write('x'.repeat(2100000));setInterval(()=>{},1000);");
  const flood = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/flood.test.mjs"], timeoutMs: 10_000 });
  assert.equal(flood.status, "failed", flood.output.slice(0, 100));
  assert.match(flood.reason || "", /output limit/);
  assert.ok(flood.output.length <= 2_000_000);
});

test("development check executes real Git fixtures with the fixed host installation inside the OS sandbox", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  // Real repository fixtures (init/add/commit/log) are the exact operation the
  // governance tests perform. macOS /usr/bin/git is an Xcode selector that
  // cannot work without developer-selection state inside the sandbox, so the
  // runner must bind one real fixed installation and authorize exactly it.
  await writeFile(join(snapshotRoot, "tests/git.test.mjs"), `import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
test('git fixture', () => {
  const directory = path.join(process.env.TMPDIR, 'git-fixture');
  fs.mkdirSync(directory, { recursive: true });
  const git = (...args) => execFileSync('git', ['-C', directory, ...args], { encoding: 'utf8' });
  git('init', '-q');
  fs.writeFileSync(path.join(directory, 'input.txt'), 'fixture input' + String.fromCharCode(10));
  git('add', '.');
  git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture baseline');
  assert.equal(git('log', '--format=%s').trim(), 'fixture baseline');
  assert.match(git('rev-parse', 'HEAD').trim(), /^[a-f0-9]{40}$/u);
});`);
  const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/git.test.mjs"] });
  assert.equal(result.status, "passed", result.output + (result.reason ?? ""));
  assert.equal(result.tests, 1);
  assert.equal(result.failed, 0);
  assert.equal(result.skipped, 0);

  if (process.platform === "darwin") {
    // No host developer-selection state was granted: the fixed installation
    // works, while the Apple selector shim cannot resolve its data link here.
    await writeFile(join(snapshotRoot, "tests/shim.test.mjs"), `import test from 'node:test';
import { execFileSync } from 'node:child_process';
test('apple selector shim', () => { execFileSync('/usr/bin/git', ['--version']); });`);
    const shim = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/shim.test.mjs"], timeoutMs: 30_000 });
    assert.equal(shim.status, "failed", shim.output);
    assert.match(shim.output, /developer_dir|developer tools|xcode-select/i);
  }
});

test("development check permits only loopback TCP, not Internet endpoints or host Unix sockets", async (t) => {
  const { directory, snapshotRoot } = await developmentFixture(t);
  const socket = join(directory, "host-store.sock");
  const { createServer } = await import("node:net");
  const hostStore = createServer();
  await new Promise<void>((done, reject) => { hostStore.once("error", reject); hostStore.listen(socket, done); });
  t.after(() => new Promise<void>((done) => hostStore.close(() => done())));
  await writeFile(join(snapshotRoot, "tests/network.test.mjs"), `import test from 'node:test';
import assert from 'node:assert/strict';import net from 'node:net';
test('network boundary', async()=>{
  const server=net.createServer(c=>c.end('loopback'));
  await new Promise((done,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',done)});
  try {
    await new Promise((done,reject)=>{const c=net.connect(server.address().port,'127.0.0.1');c.once('error',reject);c.once('data',d=>{assert.equal(d.toString(),'loopback');c.destroy();done()})});
    for(const options of [{host:'192.0.2.1',port:443},{path:${JSON.stringify(socket)}}]){
      await new Promise((done,reject)=>{const c=net.connect(options);c.once('connect',()=>{c.destroy();reject(new Error('escaped network'))});c.once('error',e=>{assert.ok(['EPERM','EACCES','ENOENT','ENETUNREACH'].includes(e.code),e.code);done()});c.setTimeout(1000,()=>{c.destroy();reject(new Error('not denied by sandbox'))})});
    }
  } finally {await new Promise(done=>server.close(done))}
});`);
  const result = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/network.test.mjs"] });
  if (result.status === "blocked") { assert.equal(result.passed, false); t.diagnostic(result.output); return; }
  assert.equal(result.status, "passed", result.reason + result.output);
});

test("development Go checks consume real JSON summaries and reject zero tests, skips and missing offline dependencies", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  const module = join(snapshotRoot, "module");
  await mkdir(module);
  await writeFile(join(module, "go.mod"), "module fixture.invalid/control\n\ngo 1.22\n");
  for (const control of [
    { body: 'func TestReal(t *testing.T) {}', passed: true },
    { body: 'func TestSkip(t *testing.T) { t.Skip("negative control") }', passed: false },
    { body: '', passed: false }
  ]) {
    await writeFile(join(module, "control_test.go"), `package control\nimport "testing"\n${control.body || 'var _ = testing.T{}'}\n`.replaceAll("\\n", "\n"));
    const result = await runDevelopmentCheck({ snapshotRoot, kind: "go", cwd: "module" });
    if (result.status === "blocked") { assert.equal(result.passed, false); t.diagnostic(result.reason + result.output); return; }
    assert.equal(result.passed, control.passed, result.reason + result.output);
    if (control.passed) assert.equal(result.tests, 1);
    else assert.ok(result.failed > 0);
  }
  await writeFile(join(module, "control_test.go"), 'package control\nimport _ "fixture.invalid/not-cached"\n');
  const missing = await runDevelopmentCheck({ snapshotRoot, kind: "go", cwd: "module" });
  assert.equal(missing.status, "failed", missing.output);
  assert.equal(missing.passed, false);
});

test("generated checks require the actual checker freshness PASS and zero exit, and browser requires the approved manifest", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  await mkdir(join(snapshotRoot, "tools"));
  for (const control of [
    { code: "console.log('Generated contracts freshness: PASS')", passed: true },
    { code: "console.log('PASS')", passed: false },
    { code: "console.log('Generated contracts freshness: FAIL')", passed: false },
    { code: "console.log('Generated contracts freshness: PASS');process.exit(1)", passed: false }
  ]) {
    await writeFile(join(snapshotRoot, "tools/verify-generated-contracts.ts"), control.code);
    const result = await runDevelopmentCheck({ snapshotRoot, kind: "generated" });
    if (result.status === "blocked") { assert.equal(result.passed, false); t.diagnostic(result.reason + result.output); break; }
    assert.equal(result.passed, control.passed, result.reason + result.output);
    assert.equal(result.tests, 0);
  }
  await writeFile(join(snapshotRoot, "package.json"), JSON.stringify({ scripts: { "test:browser:suite": "exit 0" } }));
  const browser = await runDevelopmentCheck({ snapshotRoot, kind: "browser" });
  assert.equal(browser.status, "blocked");
  assert.match(browser.reason || "", /approved.*manifest/);
});


test("development check admits only approved locked dependency symlinks, read-only, without exposing the source repository", async (t) => {
  const { snapshotRoot } = await developmentFixture(t);
  const source = process.cwd();
  await writeFile(join(snapshotRoot, "package.json"), await readFile(join(source, "package.json")));
  await writeFile(join(snapshotRoot, "package-lock.json"), await readFile(join(source, "package-lock.json")));
  await symlink(join(source, "node_modules"), join(snapshotRoot, "node_modules"));
  await writeFile(join(snapshotRoot, "tests/dependency.test.mjs"), `import test from 'node:test';
import assert from 'node:assert/strict';import fs from 'node:fs';import {parse} from 'yaml';
test('locked dependency boundary',()=>{
  assert.deepEqual(parse('key: approved'),{key:'approved'});
  assert.throws(()=>fs.readFileSync(${JSON.stringify(join(source, "package.json"))}));
  assert.throws(()=>{const f=fs.openSync(${JSON.stringify(join(source, "node_modules/yaml/package.json"))},'r+');fs.closeSync(f)});
});`);
  const approved = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/dependency.test.mjs"] });
  if (approved.status === "blocked") { assert.equal(approved.passed, false); t.diagnostic(approved.reason + approved.output); }
  else assert.equal(approved.status, "passed", approved.reason + approved.output);
  await writeFile(join(snapshotRoot, "package-lock.json"), "{}");
  const unapproved = await runDevelopmentCheck({ snapshotRoot, kind: "node", targets: ["tests/dependency.test.mjs"] });
  assert.equal(unapproved.status, "blocked");
  assert.match(unapproved.reason || "", /approved locked/);
});
