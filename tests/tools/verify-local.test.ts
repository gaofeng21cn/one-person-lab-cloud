import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { execFile as execFileCallback, spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { promisify } from "node:util";
import test from "node:test";
import { parse as parseYAML } from "yaml";
import { generationInputs, generatedOutputs } from "../../tools/verify-generated-contracts.ts";

import {
  type VerificationStep,
  verificationGroups,
  databaseFreeCapabilityMarkers,
  databaseFreeCoverage,
  databaseFreeCoverageReport,
  databaseFreeGoTestSpecs,
  databaseFreeRequiredTests,
  declaredMoneyChainTests,
  goModules,
  goStepConcurrency,
  isLoopbackOwnerDatabaseDsn,
  localVerificationSteps,
  parseVerifyLocalArgs,
  postgresImage,
  postgresVerificationSpecs,
  PUBLISHED_COMPOSE,
  runDatabaseFreeGoLane,
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
  assert.ok(!names.includes("Development plan freshness"));
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

test("PR validate keeps the exhaustive source gate without removed plan-check gates", async () => {
  const workflow = parseYAML(await readFile(".github/workflows/pull-request-ci.yml", "utf8"));
  assert.deepEqual(workflow.jobs.validate.needs.slice().sort(), ["browser", "bundle_installability", "dependency_review", "go", "source"]);
  const steps = workflow.jobs.source.steps;
  const source = steps.find((step: any) => step.name === "Validate source");
  assert.equal(source.if, undefined);
  assert.equal(source.run.trim(), "npm run verify:local -- --group source");
  assert.equal(steps.find((step: any) => step.name === "Validate changed development plan"), undefined);
  assert.equal(steps.find((step: any) => step.name === "Checkout").with["fetch-depth"], 0);
});

test("PR validate caches the Go toolchain, Python requirements and pinned contract generators", async () => {
  const workflow = parseYAML(await readFile(".github/workflows/pull-request-ci.yml", "utf8"));
  const job = workflow.jobs.source;
  const steps: Array<{ name: string; with?: Record<string, any> }> = job.steps;
  const step = (name: string) => {
    const found = steps.find((candidate) => candidate.name === name);
    assert.ok(found, `missing ${name} step`);
    return found!;
  };

  // Every module the default gate compiles must key the Go cache, so a module
  // added to the gate cannot silently fall back to a cold module and build cache.
  const goSetup = step("Set up Go");
  assert.equal(goSetup.with?.cache, true);
  assert.equal(goSetup.with?.["go-version"], "1.26.x");
  const dependencyPaths = String(goSetup.with?.["cache-dependency-path"] ?? "")
    .split("\n").map((path) => path.trim()).filter(Boolean);
  assert.deepEqual(dependencyPaths, ["go.sum"], "Go cache must key off the single root go.sum");

  const pythonSetup = step("Set up Python");
  assert.equal(pythonSetup.with?.cache, "pip");
  assert.equal(pythonSetup.with?.["cache-dependency-path"], "packages/contracts/proto/requirements.txt");

  // The generator cache key must carry both pinned versions through the shared
  // env values, so a version bump installs fresh binaries instead of restoring
  // the previous ones.
  assert.equal(workflow.env.PROTOC_GEN_GO_VERSION, "1.36.6");
  assert.equal(workflow.env.PROTOC_GEN_GO_GRPC_VERSION, "1.5.1");
  const generatorKey = String(step("Restore contract generator binaries").with?.key ?? "");
  assert.match(generatorKey, /protoc-gen-go-\$\{\{ env\.PROTOC_GEN_GO_VERSION \}\}/u);
  assert.match(generatorKey, /protoc-gen-go-grpc-\$\{\{ env\.PROTOC_GEN_GO_GRPC_VERSION \}\}/u);
});

test("CI groups execute every exhaustive step once and reject invalid selectors before execution", async () => {
  const seen: VerificationStep[] = [];
  const dependencies = {
    runStep: async (step: VerificationStep) => { seen.push(step); },
    withTemporaryPostgres: async () => { throw new Error("group entered PostgreSQL"); },
    runPostgresVerification: async () => { throw new Error("group entered PostgreSQL"); }
  };
  for (const group of verificationGroups) {
    assert.deepEqual(parseVerifyLocalArgs(["--group", group]), { withPostgres: false, group });
    const offset = seen.length;
    await runVerification({ group }, dependencies);
    assert.deepEqual(seen.slice(offset), localVerificationSteps.filter(step => step.group === group));
    assert.ok(seen.length > offset);
  }
  assert.equal(seen.length, localVerificationSteps.length);
  for (const step of localVerificationSteps) assert.equal(seen.filter(candidate => candidate === step).length, 1, step.name);
  // A misspelled or incompatible selector cannot turn a required job into a
  // successful invocation that executes no checks.
  for (const args of [
    ["--group", "unknown"], ["--group"], ["--group", "go", "--group", "source"],
    ["--group", "source", "--focused", "--base", "HEAD"], ["--group", "go", "--with-postgres"],
    ["--group", "source", "--base", "HEAD"], ["--shard", "1/2"],
    ["--group", "source", "--shard", "1/2"], ["--group", "browser", "--shard"],
    ["--group", "browser", "--shard", "1/2", "--shard", "2/2"]
  ]) assert.throws(() => parseVerifyLocalArgs(args), /requires|cannot|once/u, args.join(" "));
  for (const shard of ["0/2", "1/0", "3/2", "1", "1/2/3", "01/2", "-1/2", "1/9007199254740992"]) {
    assert.throws(() => parseVerifyLocalArgs(["--group", "browser", "--shard", shard]), /--shard requires/);
  }
  const before = seen.length;
  await assert.rejects(runVerification({ group: "unknown" as any }, dependencies), /--group requires/);
  await assert.rejects(runVerification({ group: "go", shard: "1/2" }, dependencies), /--shard requires/);
  await assert.rejects(runVerification({ group: "source", focused: true, base: "HEAD" }, dependencies), /cannot be combined/);
  assert.equal(seen.length, before);
  for (const group of verificationGroups) {
    await assert.rejects(runVerification({ group }, {
      ...dependencies, runStep: async () => { throw new Error(`${group} failure`); }
    }), new RegExp(`${group} failure`));
  }
});

test("PR jobs keep all verification groups and main alone produces the shared caches", async () => {
  const workflow = parseYAML(await readFile(".github/workflows/pull-request-ci.yml", "utf8"));
  assert.deepEqual(workflow.on, { pull_request: { branches: ["main"] } });
  assert.deepEqual(workflow.permissions, { contents: "read" });
  assert.deepEqual(Object.keys(workflow.jobs).sort(), ["browser", "bundle_installability", "dependency_review", "go", "source", "validate"]);
  assert.equal(workflow.concurrency["cancel-in-progress"], true);
  assert.equal(workflow.concurrency.group, "pull-request-ci-${{ github.event.pull_request.number }}");
  for (const name of ["dependency_review", "bundle_installability", ...verificationGroups]) {
    assert.equal(workflow.jobs[name].if, "github.event_name == 'pull_request'");
    assert.equal(workflow.jobs[name]["continue-on-error"], undefined);
  }
  const commands = (name: string) => workflow.jobs[name].steps.map((step: any) => step.run || "").join("\n");
  for (const group of verificationGroups) assert.ok(commands(group).includes(`npm run verify:local -- --group ${group}`));
  assert.equal(workflow.jobs.browser.strategy["fail-fast"], false);
  assert.deepEqual(workflow.jobs.browser.strategy.matrix.shard, ["1/2", "2/2"]);
  assert.match(commands("browser"), /--shard "\$\{\{ matrix\.shard \}\}"/u);
  const cacheWorkflow = parseYAML(await readFile(".github/workflows/warm-ci-caches.yml", "utf8"));
  assert.equal(cacheWorkflow.name, "Warm CI Caches");
  assert.deepEqual(cacheWorkflow.on, { push: { branches: ["main"] } });
  assert.deepEqual(cacheWorkflow.permissions, { contents: "read" });
  assert.equal(cacheWorkflow.concurrency["cancel-in-progress"], false);
  assert.deepEqual(cacheWorkflow.env, workflow.env);
  const warm = cacheWorkflow.jobs.warm_cache;
  assert.equal(warm.if, "github.event_name == 'push' && github.ref == 'refs/heads/main'");
  const warming = warm.steps.map((step: any) => step.run || "").join("\n");
  assert.ok(warming.includes("npm run verify:local -- --group go"));
  assert.ok(warming.includes("npm run verify:generated-contracts"));
  // Producer and consumers use identical action inputs, including Go's resolved
  // toolchain version in the generator-binary key; their cache scope may differ.
  for (const name of ["Checkout", "Set up Node 24", "Set up Python", "Install spec generator toolchain", "Set up Go", "Restore contract generator binaries", "Install contract generators", "Install dependencies"]) {
    assert.deepEqual(warm.steps.find((step: any) => step.name === name), workflow.jobs.source.steps.find((step: any) => step.name === name), name);
  }
  for (const name of ["go", "browser"]) {
    for (const step of workflow.jobs[name].steps.filter((step: any) => step.uses)) {
      assert.deepEqual(step, workflow.jobs.source.steps.find((candidate: any) => candidate.name === step.name));
    }
  }
  const generators = warm.steps.find((step: any) => step.name === "Restore contract generator binaries");
  assert.match(generators.with.key, /steps\.go\.outputs\.go-version/u);
  assert.equal(workflow.jobs.source.steps.find((step: any) => step.name === "Set up Go").id, "go");
});

test("validate actually refuses failed, cancelled, skipped or missing required results", async () => {
  const workflow = parseYAML(await readFile(".github/workflows/pull-request-ci.yml", "utf8"));
  const gate = workflow.jobs.validate;
  assert.equal(gate.name, "validate");
  assert.equal(workflow.jobs.dependency_review.name, "dependency-review");
  assert.equal(gate.if, "${{ always() && github.event_name == 'pull_request' }}");
  assert.deepEqual(gate.needs.slice().sort(), ["browser", "bundle_installability", "dependency_review", "go", "source"]);
  const step = gate.steps.find((candidate: any) => candidate.name === "Require every validation job");
  assert.equal(step.env.REQUIRED_RESULTS, "${{ toJSON(needs) }}");
  const run = promisify(execFileCallback);
  const results = Object.fromEntries(gate.needs.map((name: string) => [name, { result: "success" }]));
  const invoke = (value: unknown) => run("bash", ["-e", "-c", step.run], {
    env: { ...process.env, REQUIRED_RESULTS: JSON.stringify(value) }
  });
  await invoke(results);
  for (const name of gate.needs) {
    const missing = { ...results };
    delete missing[name];
    await assert.rejects(invoke(missing), (error: any) => error.code !== 0);
    for (const result of ["failure", "cancelled", "skipped", "", undefined]) {
      await assert.rejects(invoke({ ...results, [name]: { result } }), (error: any) => error.code !== 0);
    }
  }
  await assert.rejects(invoke({}), (error: any) => error.code !== 0);
});

test("real Node sharding runs the entire browser file inventory exactly once", async t => {
  const directory = await mkdtemp(join(tmpdir(), "opl-browser-shards-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const manifest = JSON.parse(await readFile("package.json", "utf8"));
  const script = manifest.scripts["test:browser:suite"];
  const paths: string[] = script.match(/tests\/[^\s"']+\.test\.[cm]?[jt]sx?/gu) || [];
  assert.ok(paths.length > 0);
  await writeFile(join(directory, "package.json"), JSON.stringify({
    type: "module", scripts: { "test:ownership": "node ownership.cjs", "test:browser:suite": script }
  }));
  await mkdir(join(directory, "tools"));
  await writeFile(join(directory, "tools/run-browser-suite.ts"), await readFile("tools/run-browser-suite.ts", "utf8"));
  const log = join(directory, "executed.jsonl");
  await writeFile(log, "");
  await writeFile(join(directory, "ownership.cjs"), `require('node:fs').appendFileSync(${JSON.stringify(log)}, '"ownership"\\n');\n`);
  for (const path of paths) {
    await mkdir(dirname(join(directory, path)), { recursive: true });
    await writeFile(join(directory, path), `import test from 'node:test'; import { appendFileSync } from 'node:fs';\ntest(${JSON.stringify(path)}, () => appendFileSync(${JSON.stringify(log)}, ${JSON.stringify(JSON.stringify(path) + "\n")}));\n`);
  }
  const run = promisify(execFileCallback);
  const workflow = parseYAML(await readFile(".github/workflows/pull-request-ci.yml", "utf8"));
  // This is a new test runner, not a worker of the enclosing node --test.
  const env = { ...process.env };
  delete env.NODE_TEST_CONTEXT;
  for (const shard of workflow.jobs.browser.strategy.matrix.shard) {
    await runVerification(parseVerifyLocalArgs(["--group", "browser", "--shard", shard]), {
      runStep: async step => { await run(step.command, step.args, { cwd: directory, env }); },
      withTemporaryPostgres: async () => { throw new Error("browser entered PostgreSQL"); },
      runPostgresVerification: async () => { throw new Error("browser entered PostgreSQL"); }
    });
  }
  const executed = (await readFile(log, "utf8")).trim().split("\n").map(line => JSON.parse(line));
  assert.equal(executed.filter(path => path === "ownership").length, 2);
  assert.deepEqual(executed.filter(path => path !== "ownership").sort(), paths.slice().sort());
  await writeFile(log, "");
  await run("npm", ["run", "test:browser:suite"], { cwd: directory, env });
  const full = (await readFile(log, "utf8")).trim().split("\n").map(line => JSON.parse(line));
  assert.deepEqual(full.filter(path => path !== "ownership").sort(), paths.slice().sort());
  await writeFile(join(directory, paths[0]), "import test from 'node:test'; test('failure control', () => { throw new Error('browser failure control'); });\n");
  await assert.rejects(run("npm", ["run", "test:browser:suite"], { cwd: directory, env }), (error: any) => error.code !== 0);
  await assert.rejects(run("node", ["tools/run-browser-suite.ts"], { cwd: directory, env }), /explicit test files/u);
});

test("no database-free lane decides coverage by a test name prefix", () => {
  for (const spec of databaseFreeGoTestSpecs) {
    assert.ok(!("run" in spec), `${spec.cwd} still selects tests by name prefix`);
  }
  const lane = localVerificationSteps.find((step) => step.cwd === "services/control-plane" && step.args.includes("./internal/server"));
  assert.ok(lane, "control-plane internal/server must run in the database-free lane");
  assert.deepEqual(lane!.args, ["test", "-count=1", "-json", "./internal/server"]);
  assert.equal(lane!.evidence, "go-coverage");
  assert.deepEqual(lane!.declaredTests, declaredMoneyChainTests);
  for (const name of [
    "TestGatewayRevealFailsClosed",
    "TestGatewayRevealDoesNotLeakAcrossAccounts",
    "TestGatewayRevealAuditFailureDoesNotReturnKey"
  ]) {
    assert.ok(declaredMoneyChainTests.includes(name), `${name} must be declared coverage`);
  }
  assert.deepEqual(
    databaseFreeGoTestSpecs.filter((spec) => spec.declaredTests),
    [{
      cwd: "services/control-plane",
      packages: ["./internal/server"],
      declaredTests: declaredMoneyChainTests,
      // The executed-test floor and the must-execute list are part of the lane
      // now: they replaced the duplicated end-to-end run that used to assert
      // the same breadth and the same Gateway reveal family.
      minimumExecutedTests: 1001,
      requiredExecutedTests: databaseFreeRequiredTests
    }]
  );
});

const coveragePackage = "opl-cloud/services/control-plane/internal/server";
function coverageEvents(...events: Record<string, unknown>[]) {
  return [
    { Action: "start", Package: coveragePackage },
    ...events.map((event) => ({ Package: coveragePackage, ...event })),
    { Action: events.some(event => event.Action === "fail") ? "fail" : "pass", Package: coveragePackage }
  ];
}
const fixtureAbsent = "  d1_finance_chain_test.go:28: CONTROL_PLANE_TEST_DATABASE_URL or OPL_POSTGRES_TESTS=1 is required for PostgreSQL tests\n";
const skipDeclared = "  d2_finance_chain_test.go:20: PostgreSQL test gate is not configured\n";

test("database-free coverage separates executed, capability-excluded and failed tests", () => {
  const coverage = databaseFreeCoverage(coverageEvents(
    { Action: "run", Test: "TestGatewayRevealFailsClosed" },
    { Action: "output", Test: "TestGatewayRevealFailsClosed", Output: "=== RUN   TestGatewayRevealFailsClosed\n" },
    { Action: "pass", Test: "TestGatewayRevealFailsClosed" },
    { Action: "run", Test: "TestD1FinanceBusinessChain" },
    { Action: "output", Test: "TestD1FinanceBusinessChain", Output: skipDeclared },
    { Action: "skip", Test: "TestD1FinanceBusinessChain" },
    { Action: "run", Test: "TestD1RefundReservationPostgres" },
    { Action: "output", Test: "TestD1RefundReservationPostgres", Output: "=== RUN   TestD1RefundReservationPostgres\n" },
    { Action: "fail", Test: "TestD1RefundReservationPostgres" },
    { Action: "run", Test: "TestD1RefundReservationPostgres/postgres" },
    { Action: "output", Test: "TestD1RefundReservationPostgres/postgres", Output: fixtureAbsent },
    { Action: "fail", Test: "TestD1RefundReservationPostgres/postgres" },
    { Action: "run", Test: "TestSettlementRoundTrips" },
    { Action: "output", Test: "TestSettlementRoundTrips", Output: "    chain_test.go:90: settlement mismatch\n" },
    { Action: "fail", Test: "TestSettlementRoundTrips" }
  ), { declared: declaredMoneyChainTests });
  assert.deepEqual(coverage.executed, ["TestGatewayRevealFailsClosed"]);
  assert.deepEqual(coverage.excluded.map((entry) => entry.name), [
    "TestD1FinanceBusinessChain", "TestD1RefundReservationPostgres", "TestD1RefundReservationPostgres/postgres"
  ]);
  assert.equal(coverage.excluded[0].capability, "isolated-owner-postgres");
  // A parent fails only because its capability-gated child never ran.
  assert.equal(coverage.excluded[1].reason, "children require isolated-owner-postgres");
  assert.deepEqual(coverage.failed.map((entry) => entry.name), ["TestSettlementRoundTrips"]);
  assert.deepEqual(coverage.declared.missing, [
    "TestD1RenewalRejectsConflictingTransactionBeforeFulfillment",
    "TestD1RenewalRefundRequiresTheOriginalConfirmedDebit",
    "TestD1RenewalUnknownDebitWaitsForAuthoritativeHistoryAcrossRestart",
    "TestD4ExpiredWorkspaceRequiresExplicitOriginalRenewalAfterTopUp",
    "TestGatewayRevealAuditFailureDoesNotReturnKey",
    "TestGatewayRevealDoesNotLeakAcrossAccounts"
  ]);
  // A real failure outranks every other finding.
  assert.equal(coverage.reason, "Go tests failed: TestSettlementRoundTrips");
  const report = databaseFreeCoverageReport(coverage);
  // The accounted-for count follows the declared chain: this branch retires
  // TestD1RefundReservationPostgres with the refund reservation engine it
  // covered, so the fixture now accounts for two declared names of eight.
  assert.match(report, /database-free coverage: executed=1 excluded=3 declared=2\/8/);
  assert.match(report, /excluded by unprovisioned capability isolated-owner-postgres: 3/);
  assert.match(report, /\n {4}TestD1FinanceBusinessChain \(PostgreSQL test gate is not configured\)/);
  assert.match(report, /failed TestSettlementRoundTrips/);
  assert.match(report, /declared but not accounted for TestGatewayRevealAuditFailureDoesNotReturnKey$/m);
});

test("database-free coverage refuses an undeclared skip and an unaccounted declared test", () => {
  const quiet = databaseFreeCoverage(coverageEvents(
    { Action: "run", Test: "TestGatewayRevealFailsClosed" },
    { Action: "pass", Test: "TestGatewayRevealFailsClosed" }
  ), { declared: ["TestGatewayRevealFailsClosed"] });
  assert.equal(quiet.reason, undefined);

  const hidden = databaseFreeCoverage(coverageEvents(
    { Action: "run", Test: "TestGatewayRevealFailsClosed" },
    { Action: "pass", Test: "TestGatewayRevealFailsClosed" },
    { Action: "run", Test: "TestSettlementFlaky" },
    { Action: "output", Test: "TestSettlementFlaky", Output: "    chain_test.go:12: too flaky for today\n" },
    { Action: "skip", Test: "TestSettlementFlaky" }
  ));
  assert.deepEqual(hidden.undeclared.map((entry) => entry.name), ["TestSettlementFlaky"]);
  assert.equal(hidden.reason, "tests skipped without a declared capability marker: TestSettlementFlaky (too flaky for today)");
  assert.match(databaseFreeCoverageReport(hidden), /skipped without a declared capability marker TestSettlementFlaky/);

  const renamed = databaseFreeCoverage(coverageEvents(
    { Action: "run", Test: "TestSomethingElse" },
    { Action: "pass", Test: "TestSomethingElse" }
  ), { declared: ["TestGatewayRevealAuditFailureDoesNotReturnKey"] });
  assert.deepEqual(renamed.declared.accounted, []);
  assert.equal(renamed.reason, "declared coverage is not accounted for: TestGatewayRevealAuditFailureDoesNotReturnKey");

  // Every marker is a capability this gate deliberately leaves unprovisioned.
  assert.deepEqual([...new Set(databaseFreeCapabilityMarkers.map((entry) => entry.capability))].sort(),
    ["isolated-data-scale", "isolated-owner-postgres"]);
});

test("capability declarations do not mask assertions or unrelated package failures", () => {
  const marker = databaseFreeCapabilityMarkers[0].marker;
  const mixed = databaseFreeCoverage(coverageEvents(
    { Action: "run", Test: "TestMoney" },
    { Action: "output", Test: "TestMoney", Output: `    fixture_test.go:12: ${marker}\n    money_test.go:20: wrong refund\n` },
    { Action: "fail", Test: "TestMoney" }
  ));
  assert.deepEqual(mixed.excluded, []);
  assert.deepEqual(mixed.failed.map(entry => entry.name), ["TestMoney"]);
  const packageFailure = databaseFreeCoverage([
    ...coverageEvents({ Action: "run", Test: "TestPassing" }, { Action: "pass", Test: "TestPassing" }),
    { Package: "broken/package", Action: "fail" }
  ]);
  assert.match(packageFailure.reason!, /Go package failed without a test failure: broken\/package/);
});

test("the real database-free lane executes the Gateway reveal family and names its excluded PostgreSQL suites", async (t) => {
  // This runs the whole control-plane package a second time. The exhaustive gate
  // already executes the same lane as its own step and now enforces the same
  // executed-test floor there (minimumExecutedTests), so the duplicate run is an
  // opt-in end-to-end proof instead of a fixed cost on every run:
  //   OPL_LANE_E2E=1 node --test tests/tools/verify-local.test.ts
  // A diagnostic plus return keeps the suite's zero-skip result intact.
  if (process.env.OPL_LANE_E2E !== "1") {
    t.diagnostic("duplicate end-to-end lane run skipped; set OPL_LANE_E2E=1 to execute it");
    return;
  }
  if (spawnSync("go", ["version"], { encoding: "utf8" }).status !== 0) {
    t.diagnostic("no Go toolchain available; control-plane lane fixture skipped");
    return;
  }
  const lane = localVerificationSteps.find((step) => step.cwd === "services/control-plane" && step.args.includes("./internal/server"));
  assert.ok(lane);
  const env = { ...process.env };
  for (const name of ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"]) delete env[name];
  const coverage = await runDatabaseFreeGoLane(lane!, env);
  assert.equal(coverage.reason, undefined, databaseFreeCoverageReport(coverage));
  for (const name of [
    "TestGatewayRevealFailsClosed",
    "TestGatewayRevealDoesNotLeakAcrossAccounts",
    "TestGatewayRevealAuditFailureDoesNotReturnKey"
  ]) {
    assert.ok(coverage.executed.includes(name), `${name} must execute in the database-free lane`);
    assert.ok(!coverage.excluded.some((entry) => entry.name === name), `${name} must not be excluded`);
    assert.ok(!coverage.failed.some((entry) => entry.name === name), `${name} must not fail`);
  }
  assert.deepEqual(coverage.declared.missing, []);
  const excluded = coverage.excluded.find((entry) => entry.name === "TestD1FinanceBusinessChain");
  assert.equal(excluded?.capability, "isolated-owner-postgres");
  assert.ok(coverage.executed.length > 1000, `only ${coverage.executed.length} tests executed`);
  assert.match(databaseFreeCoverageReport(coverage), new RegExp(`declared=${declaredMoneyChainTests.length}\\/${declaredMoneyChainTests.length}`));
});

test("the control-plane lane keeps its executed-test floor without the duplicate run", () => {
  // The floor replaces the broad-coverage assertion the duplicated end-to-end
  // run used to make, so it has to hold for the lane that actually runs in CI.
  const lane = localVerificationSteps.find((step) => step.cwd === "services/control-plane" && step.args.includes("./internal/server"));
  assert.ok(lane, "the control-plane database-free lane must stay in the default gate");
  assert.ok((lane!.minimumExecutedTests ?? 0) > 1000, "the lane must keep the more-than-1000 executed-test floor");
  assert.ok(lane!.declaredTests?.includes("TestGatewayRevealFailsClosed"));

  const narrow = coverageEvents(
    { Action: "run", Test: "TestGatewayRevealFailsClosed" },
    { Action: "pass", Test: "TestGatewayRevealFailsClosed" }
  );
  assert.equal(databaseFreeCoverage(narrow, { minimumExecuted: 1 }).reason, undefined);
  // Shrinking coverage is a failure, not a quiet pass.
  assert.equal(
    databaseFreeCoverage(narrow, { minimumExecuted: 2 }).reason,
    "only 1 of the required 2 tests executed"
  );

  // A capability marker may never excuse a required-executed test: the Gateway
  // reveal family runs without a database, so "excluded" there means the lane
  // stopped proving it.
  assert.ok(lane!.requiredExecutedTests?.includes("TestGatewayRevealFailsClosed"));
  const excused = databaseFreeCoverage(coverageEvents(
    { Action: "run", Test: "TestSettlementRoundTrips" },
    { Action: "pass", Test: "TestSettlementRoundTrips" },
    { Action: "run", Test: "TestGatewayRevealFailsClosed" },
    { Action: "output", Test: "TestGatewayRevealFailsClosed", Output: skipDeclared },
    { Action: "skip", Test: "TestGatewayRevealFailsClosed" }
  ), { requiredExecuted: ["TestGatewayRevealFailsClosed"] });
  assert.equal(excused.reason, "declared tests did not execute: TestGatewayRevealFailsClosed");
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
    "tools/verify-local.ts",
    "tests/tools/verify-local.test.ts",
    "services/fabric/internal/fabric/runtime.go",
    "services/fabric/internal/fabric/runtime_test.go"
  ]).map((step) => step.name);
  assert.deepEqual(selected, [
    "product boundary",
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

test("Local qualification uses one bounded runner filesystem and explicit privileged inputs", async (t) => {
  const workflow = parseYAML(await readFile(".github/workflows/qualification.yml", "utf8"));
  assert.deepEqual(Object.keys(workflow.jobs), ["fabric", "validate"]);
  assert.equal(workflow.jobs.validate.needs, "fabric");
  const job = workflow.jobs.fabric;
  assert.equal(job["runs-on"], "ubuntu-latest");
  assert.equal(job.environment, undefined);
  assert.deepEqual(workflow.permissions, { contents: "read" });
  const step = (name: string) => job.steps.find((item: any) => item.name === name);
  const initialize = step("Initialize Local qualification directory");
  const quotaSupport = step("Load runner project quota support");
  const prepare = step("Prepare project quota filesystem");
  const compile = step("Compile Local qualification executables without privilege");
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
  // The development gate reads the postgres image fact from the published
  // compose file. The path is asserted literally here: the fact source is a
  // declaration, so re-pointing it fails this check instead of silently
  // reading another file that happens to declare the same image.
  assert.equal(PUBLISHED_COMPOSE, "deploy/portable/compose.cloud.yaml");
  const compose = parseYAML(await readFile("deploy/portable/compose.cloud.yaml", "utf8"));
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
    runStep: async (step: VerificationStep) => { events.push(`step:${step.name}`); },
    withTemporaryPostgres: async (callback: (env: NodeJS.ProcessEnv, fixture: { socketDirectory?: string }) => Promise<unknown>) => {
      events.push("postgres:start");
      try {
        await callback(env, {});
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
  assert.deepEqual(events.slice(0, -3), localVerificationSteps.map(step => `step:${step.name}`));
  assert.ok(!events.includes("step:Development plan freshness"));
  assert.deepEqual(events.slice(-3), ["postgres:start", "postgres:tests", "postgres:stop"]);

  events.length = 0;
  await runVerification({ withPostgres: false }, dependencies);
  assert.deepEqual(events, localVerificationSteps.map(step => `step:${step.name}`));
  assert.equal(events.some((event) => event.startsWith("postgres:")), false);
});

test("the exhaustive gate overlaps only the database-free lanes and keeps every step", async () => {
  const order: string[] = [];
  const lanes = new Set(
    localVerificationSteps.filter((step) => step.evidence === "go-coverage").map((step) => step.name)
  );
  let laneInFlight = 0;
  let lanePeak = 0;
  let compileInFlight = 0;
  let compilePeak = 0;
  const dependencies = {
    runStep: async (step: VerificationStep) => {
      order.push(step.name);
      const lane = lanes.has(step.name);
      const compile = step.command === "go" && !lane;
      if (lane) laneInFlight += 1;
      if (compile) compileInFlight += 1;
      lanePeak = Math.max(lanePeak, laneInFlight);
      compilePeak = Math.max(compilePeak, compileInFlight);
      if (lane || compile) await new Promise((resolvePromise) => setTimeout(resolvePromise, 5));
      if (lane) laneInFlight -= 1;
      if (compile) compileInFlight -= 1;
    },
    withTemporaryPostgres: async () => {},
    runPostgresVerification: async () => {}
  };

  await runVerification({}, dependencies);
  // Bounded overlap must not drop, reorder or duplicate a declared step.
  assert.deepEqual(order, localVerificationSteps.map((step) => step.name));
  assert.equal(lanePeak, goStepConcurrency);
  // Cold module compiles oversubscribe a four-core runner (measured 126s -> 178s
  // in CI), so the compile group stays sequential.
  assert.equal(compilePeak, 1);
});

test("one failing database-free lane fails the exhaustive gate instead of being absorbed by its batch", async () => {
  const failing = "services/ledger database-free tests";
  const started: string[] = [];
  const dependencies = {
    runStep: async (step: VerificationStep) => {
      started.push(step.name);
      if (step.name === failing) throw new Error(`${failing} failed`);
    },
    withTemporaryPostgres: async () => {},
    runPostgresVerification: async () => {}
  };

  await assert.rejects(runVerification({}, dependencies), /services\/ledger database-free tests failed/u);
  assert.ok(started.includes(failing));
  // A failed batch stops the gate before later steps, like the sequential path did.
  assert.ok(!started.includes("Git whitespace"));
});


test("isLoopbackOwnerDatabaseDsn accepts only isolated loopback PostgreSQL DSNs", () => {
  assert.equal(isLoopbackOwnerDatabaseDsn("postgresql://postgres@127.0.0.1:54321/postgres?sslmode=disable"), true);
  for (const refused of [
    "postgresql://postgres@db.production.example:5432/postgres?sslmode=disable",
    "postgresql://postgres@10.0.0.5:5432/postgres",
    "postgresql://postgres@127.0.0.1/postgres",
    "mysql://postgres@127.0.0.1:3306/postgres",
    "not a dsn",
    ""
  ]) assert.equal(isLoopbackOwnerDatabaseDsn(refused), false, refused);
});


test("PR source gate executes installation CLI admission and the native Serve probe suite", async () => {
  const scripts = JSON.parse(await readFile("package.json", "utf8")).scripts;
  assert.match(scripts["test:source"], /tests\/production\/\*\.test\.ts/);
  const lane = databaseFreeGoTestSpecs.find(spec => spec.cwd === "services/serve" && spec.packages.includes("./cmd/server"));
  assert.ok(lane, "Serve probe behavior must execute, not just compile, in actual PR verification");
  const step = localVerificationSteps.find(step => step.cwd === "services/serve" && step.evidence === "go-coverage");
  assert.deepEqual(step?.args, ["test", "-count=1", "-json", "./cmd/server"]);
});
