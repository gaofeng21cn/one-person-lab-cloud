import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
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
    "Billing browser tests",
    "Gateway usage browser tests",
    "Console owner read browser tests",
    "Customer Announcement browser tests",
    "Operator Account browser tests",
    "Operator Announcement browser tests",
    "Operator Resource Read browser tests",
    "Workspace lifecycle browser tests",
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
    "services/ledger",
    "services/control-plane",
    "services/fabric"
  ]);
  assert.equal(postgresVerificationSpecs[0].race, true);
  assert.equal(postgresVerificationSpecs[2].timeout, "15m");
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
