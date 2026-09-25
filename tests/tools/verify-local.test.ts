import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { execFile as execFileCallback } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import test from "node:test";
import { parse as parseYAML } from "yaml";

import {
  databaseFreeGoTestSpecs,
  goModules,
  localVerificationSteps,
  parseVerifyLocalArgs,
  postgresImage,
  postgresVerificationSpecs,
  runVerification,
  summarizeGoTestFailures
} from "../../tools/verify-local.ts";

test("verify-local exposes one default gate across Node, builds, and every Go module", () => {
  assert.deepEqual(parseVerifyLocalArgs([]), { withPostgres: false });
  assert.deepEqual(parseVerifyLocalArgs(["--with-postgres"]), { withPostgres: true });
  assert.throws(() => parseVerifyLocalArgs(["--production"]), /unknown verify-local argument/);

  const names = localVerificationSteps.map((step) => step.name);
  for (const expected of [
    "product boundary",
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

test("Qualification executes the independent Go contracts module", async () => {
  const qualification = parseYAML(await readFile(".github/workflows/qualification.yml", "utf8"));
  const contractJob = qualification.jobs.go_contracts;
  assert.equal(contractJob.name, "go-contracts");
  assert.equal(contractJob.steps.find((step) => step.name === "Set up Go").with.cache, false);
  assert.ok(contractJob.steps.some((step) => step["working-directory"] === "packages/contracts/go"
    && step.run === "go test -count=1 ./..."));
  assert.ok(qualification.jobs.validate.needs.includes("go_contracts"));
  const validateStep = qualification.jobs.validate.steps.find((step) => step.name === "Require successful test jobs");
  assert.equal(
    validateStep.env.GO_CONTRACTS_RESULT,
    "${{ needs.go_contracts.result }}"
  );
  assert.match(validateStep.run, /"\$GO_CONTRACTS_RESULT" != "success"/);
});

test("Local qualification uses one bounded runner filesystem and explicit privileged inputs", async (t) => {
  const workflow = parseYAML(await readFile(".github/workflows/qualification.yml", "utf8"));
  const job = workflow.jobs.fabric;
  assert.equal(job["runs-on"], "ubuntu-latest");
  assert.equal(job.environment, undefined);
  assert.deepEqual(workflow.permissions, { contents: "read" });
  const step = (name: string) => job.steps.find((item) => item.name === name);
  const initialize = step("Initialize Local qualification directory");
  const prepare = step("Prepare project quota filesystem");
  const compile = step("Compile Local qualification executables without privilege");
  const quota = step("Test Linux project quota as privileged capability");
  const deploy = step("Test first Local application deployment with real owners");
  const cleanup = step("Remove project quota filesystem");
  assert.equal(cleanup.if, "${{ always() }}");
  // Runner paths are unavailable in job-level env expressions. Execute their
  // initialization on the runner and persist them for later steps instead.
  assert.equal(job.env.OPL_QUALIFICATION_ROOT, undefined);
  assert.ok(job.steps.indexOf(initialize) < job.steps.indexOf(prepare));
  assert.ok(job.steps.indexOf(compile) < job.steps.indexOf(quota));
  assert.ok(job.steps.indexOf(quota) < job.steps.indexOf(deploy));
  assert.ok(job.steps.indexOf(deploy) < job.steps.indexOf(cleanup));
  assert.doesNotMatch(compile.run, /\bsudo\b/);
  const run = promisify(execFileCallback);
  for (const item of [initialize, prepare, compile, quota, deploy, cleanup]) await run("bash", ["-n", "-c", item.run]);

  const temporary = await mkdtemp(join(tmpdir(), "opl-qualification-shell-"));
  t.after(() => rm(temporary, { recursive: true, force: true }));
  const bin = join(temporary, "bin"), log = join(temporary, "commands.jsonl");
  await mkdir(bin);
  // Mock privileged/system commands, but execute the actual workflow shell.
  // These mocks never mount, format, truncate or remove a real filesystem.
  const mock = `#!${process.execPath}\nconst fs = require('node:fs'); const path = require('node:path');
const command = path.basename(process.argv[1]), args = process.argv.slice(2);
fs.appendFileSync(process.env.QUALIFICATION_COMMAND_LOG, JSON.stringify({command,args})+'\\n');
if (command === 'df') process.stdout.write('Avail\\n'+process.env.QUALIFICATION_AVAILABLE_BYTES+'\\n');
if (command === 'mountpoint') process.exit(process.env.QUALIFICATION_MOUNTED === '1' ? 0 : 1);
if (command === 'sudo' && args[0] === 'umount' && process.env.QUALIFICATION_UNMOUNT_FAIL === '1') process.exit(1);
`;
  for (const command of ["df", "truncate", "mkfs.ext4", "sudo", "mountpoint"]) {
    const path = join(bin, command);
    await writeFile(path, mock); await chmod(path, 0o755);
  }
  const root = join(temporary, "opl-local-first-deploy-test-1");
  const env = { ...process.env, PATH: `${bin}:${process.env.PATH}`, RUNNER_TEMP: temporary, OPL_QUALIFICATION_ROOT: root,
    GITHUB_RUN_ID: "test", GITHUB_RUN_ATTEMPT: "1", QUALIFICATION_COMMAND_LOG: log, QUALIFICATION_AVAILABLE_BYTES: String(16 * 1024 ** 3), QUALIFICATION_MOUNTED: "1" };
  const githubEnv = join(temporary, "github-env");
  const initialEnv = { ...env, GITHUB_ENV: githubEnv };
  delete initialEnv.OPL_QUALIFICATION_ROOT;
  await run("bash", ["-c", initialize.run], { env: initialEnv });
  assert.equal(await readFile(githubEnv, "utf8"), `OPL_QUALIFICATION_ROOT=${root}\n`);
  const commands = async () => (await readFile(log, "utf8")).trim().split("\n").filter(Boolean).map((line) => JSON.parse(line));
  await run("bash", ["-c", prepare.run], { env });
  let calls = await commands();
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
  await assert.rejects(run("bash", ["-c", prepare.run], { env: { ...env, GITHUB_RUN_ID: "low", GITHUB_RUN_ATTEMPT: "space", OPL_QUALIFICATION_ROOT: join(temporary, "opl-local-first-deploy-low-space"), QUALIFICATION_AVAILABLE_BYTES: "1024" } }));
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
  assert.equal(postgresVerificationSpecs.find((spec) => spec.cwd === "services/control-plane").timeout, "15m");
});

test("full verification adds the temporary PostgreSQL modules after the default checks", async () => {
  const events = [];
  const env = { OPL_POSTGRES_TESTS: "1" };
  const dependencies = {
    runStep: async (step) => { events.push(`step:${step.name}`); },
    withTemporaryPostgres: async (callback) => {
      events.push("postgres:start");
      try {
        await callback(env);
      } finally {
        events.push("postgres:stop");
      }
    },
    runPostgresVerification: async (actualEnv) => {
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
