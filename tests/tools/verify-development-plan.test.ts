import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cpSync, existsSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { generatedPlanArtifacts, runPlanGenerator, verifyDevelopmentPlan } from "../../tools/verify-development-plan.ts";
import { runDevelopmentCheck } from "../../tools/verify-local.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

function materializePlanSnapshot(scratch: string) {
  const target = "docs/spec/target";
  const inputs = [
    "00_master_index.md", "01_domain_ownership_matrix.md", "03_api_contract_complete.yaml", "12_product_spec.md",
    "checks/render_development_plan.py", "checks/validate_development_plan.py",
    "contracts/api_inventory.json", "contracts/db_inventory.json", "contracts/ui_inventory.json", "contracts/internal.proto"
  ].map((path) => join(target, path));
  const plan = JSON.parse(readFileSync(join(root, generatedPlanArtifacts[0]), "utf8"));
  // Path presence is also a render input. Preserve the admitted paths' actual
  // files/directories, without importing Git metadata or unrelated source trees.
  const readPaths: string[] = plan.workPackages.flatMap((item: { existingReadPaths: string[] }) => item.existingReadPaths);
  // Development test grants must be materialized too: the validator refuses a
  // grant that does not resolve to a real file in the checked tree.
  const testWritePaths: string[] = (plan.executionSlices ?? []).flatMap((item: { testWritePaths?: string[] }) => item.testWritePaths ?? []);
  for (const path of new Set([...inputs, ...generatedPlanArtifacts, "tools/verify-development-plan.ts", ...readPaths, ...testWritePaths])) {
    const source = join(root, path);
    const destination = join(scratch, path);
    if (statSync(source).isDirectory()) mkdirSync(destination, { recursive: true });
    else {
      mkdirSync(dirname(destination), { recursive: true });
      cpSync(source, destination);
    }
  }
  for (const [owner, path] of Object.entries(plan.sourceRoots) as [string, string][]) {
    if (owner !== "instance" && existsSync(join(root, path))) mkdirSync(join(scratch, path), { recursive: true });
  }
}

function snapshotState(scratch: string) {
  return readdirSync(scratch, { recursive: true, encoding: "utf8" }).sort().map((path) => {
    const file = join(scratch, path);
    const status = statSync(file);
    return { path, modified: status.mtimeMs, hash: status.isDirectory() ? null : createHash("sha256").update(readFileSync(file)).digest("hex") };
  });
}

test("real regeneration byte-matches the checked-in plan artifacts and validates", () => {
  const result = verifyDevelopmentPlan({ root });
  assert.deepEqual(result.artifacts, [...generatedPlanArtifacts]);
});

test("public renderer reports Instance as external-owner/unverified regardless of sibling presence", () => {
  const directory = mkdtempSync(join(tmpdir(), "opl-plan-external-owner-"));
  const scratch = join(directory, "checkout");
  const out = join(directory, "rendered");
  try {
    mkdirSync(scratch);
    materializePlanSnapshot(scratch);
    let previous: Buffer[] | undefined;
    for (const present of [false, true]) {
      if (present) mkdirSync(join(directory, "opl-instance-medopl"));
      const rendered = spawnSync("python3", [join(scratch, "docs/spec/target/checks/render_development_plan.py"), "--check"], {
        cwd: scratch,
        env: { ...process.env, OPL_DEVELOPMENT_PLAN_OUT: out, PYTHONDONTWRITEBYTECODE: "1" },
        encoding: "utf8"
      });
      assert.equal(rendered.status, 0, rendered.stderr);
      const artifacts = ["checks/development_plan.json", "14_implementation_work_packages.md"].map((path) => readFileSync(join(out, path)));
      const plan = JSON.parse(artifacts[0].toString());
      assert.equal(plan.sourceRootStatus.instance, "external-owner/unverified");
      assert.match(artifacts[1].toString(), /\| instance \| `\.\.\/opl-instance-medopl` \| external-owner\/unverified \|/u);
      if (previous) assert.deepEqual(artifacts, previous, "external sibling presence changed the Cloud projection");
      previous = artifacts;
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("public validator rejects missing or locally verified Instance status without writing evidence", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-instance-status-"));
  try {
    materializePlanSnapshot(scratch);
    const planPath = join(scratch, generatedPlanArtifacts[0]);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    for (const status of ["existing", "planned_not_created", undefined]) {
      if (status === undefined) delete plan.sourceRootStatus.instance;
      else plan.sourceRootStatus.instance = status;
      writeFileSync(planPath, JSON.stringify(plan, null, 2) + "\n");
      const before = snapshotState(scratch);
      const checked = spawnSync("python3", [join(scratch, "docs/spec/target/checks/validate_development_plan.py"), "--check"], {
        cwd: scratch, env: { ...process.env, PYTHONDONTWRITEBYTECODE: "1" }, encoding: "utf8"
      });
      assert.equal(checked.status, 1, checked.stderr + checked.stdout);
      assert.ok(JSON.parse(checked.stdout).errors.includes("Instance source root must be external-owner/unverified"));
      assert.deepEqual(snapshotState(scratch), before);
    }
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

test("public generation and validation keep external Instance paths unverified without following sibling symlinks", () => {
  const directory = mkdtempSync(join(tmpdir(), "opl-plan-instance-link-"));
  const scratch = join(directory, "checkout");
  const out = join(directory, "rendered");
  try {
    mkdirSync(scratch);
    materializePlanSnapshot(scratch);
    // A synthetic external sibling points back into this fixture. Its physical
    // target must not redefine an unverified external ownership declaration.
    symlinkSync(scratch, join(directory, "opl-instance-medopl"));
    const rendered = spawnSync("python3", [join(scratch, "docs/spec/target/checks/render_development_plan.py"), "--check"], {
      cwd: scratch,
      env: { ...process.env, OPL_DEVELOPMENT_PLAN_OUT: out, PYTHONDONTWRITEBYTECODE: "1" },
      encoding: "utf8"
    });
    assert.equal(rendered.status, 0, rendered.stderr);
    for (const artifact of generatedPlanArtifacts) {
      cpSync(join(out, artifact.replace(/^docs\/spec\/target\//u, "")), join(scratch, artifact));
    }
    const before = snapshotState(scratch);
    const checked = spawnSync("python3", [join(scratch, "docs/spec/target/checks/validate_development_plan.py"), "--check"], {
      cwd: scratch, env: { ...process.env, PYTHONDONTWRITEBYTECODE: "1" }, encoding: "utf8"
    });
    assert.equal(checked.status, 0, checked.stderr + checked.stdout);
    assert.deepEqual(JSON.parse(checked.stdout).errors, []);
    assert.deepEqual(snapshotState(scratch), before);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("public validator refuses to claim an external Instance file as an existing Cloud input", () => {
  const directory = mkdtempSync(join(tmpdir(), "opl-plan-external-input-"));
  const scratch = join(directory, "checkout");
  try {
    mkdirSync(scratch);
    materializePlanSnapshot(scratch);
    const external = join(directory, "opl-instance-medopl");
    mkdirSync(external);
    writeFileSync(join(external, "source.txt"), "synthetic external owner source\n");
    const planPath = join(scratch, generatedPlanArtifacts[0]);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    plan.sourceRootStatus.instance = "external-owner/unverified";
    plan.workPackages.find((item: { id: string }) => item.id === "W29").existingReadPaths.push("../opl-instance-medopl/source.txt");
    writeFileSync(planPath, JSON.stringify(plan, null, 2) + "\n");
    const before = snapshotState(scratch);
    const checked = spawnSync("python3", [join(scratch, "docs/spec/target/checks/validate_development_plan.py"), "--check"], {
      cwd: scratch, env: { ...process.env, PYTHONDONTWRITEBYTECODE: "1" }, encoding: "utf8"
    });
    assert.equal(checked.status, 1, checked.stderr + checked.stdout);
    assert.ok(JSON.parse(checked.stdout).errors.includes("W29 falsely labels existing source: ../opl-instance-medopl/source.txt"));
    assert.deepEqual(snapshotState(scratch), before);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("the default subprocess reads renderer inputs from the requested root", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-root-"));
  try {
    const checks = join(scratch, "docs/spec/target/checks");
    const contracts = join(scratch, "docs/spec/target/contracts");
    mkdirSync(checks, { recursive: true });
    mkdirSync(contracts, { recursive: true });
    cpSync(join(root, "docs/spec/target/checks/render_development_plan.py"), join(checks, "render_development_plan.py"));
    writeFileSync(join(contracts, "api_inventory.json"), "not JSON\n");
    assert.throws(() => verifyDevelopmentPlan({ root: scratch }), /plan renderer failed:[\s\S]*JSONDecodeError/u);
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

test("the freshness API can be imported by a stdin caller without running its CLI", () => {
  const imported = spawnSync(process.execPath, ["--input-type=module", "-"], {
    cwd: root,
    input: "const { verifyDevelopmentPlan } = await import('./tools/verify-development-plan.ts'); console.log(typeof verifyDevelopmentPlan);\n",
    encoding: "utf8"
  });
  assert.equal(imported.status, 0, imported.stderr);
  assert.equal(imported.stdout, "function\n");
});

test("freshness runs real subprocesses in a Git-free snapshot without rewriting it", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-snapshot-"));
  try {
    materializePlanSnapshot(scratch);
    assert.equal(existsSync(join(scratch, ".git")), false);
    assert.notEqual(spawnSync("git", ["-C", scratch, "rev-parse", "HEAD"]).status, 0);
    const entry = join(scratch, "freshness.ts");
    symlinkSync(join(scratch, "tools/verify-development-plan.ts"), entry);
    const before = snapshotState(scratch);
    assert.deepEqual(verifyDevelopmentPlan({ root: relative(process.cwd(), scratch) }).artifacts, [...generatedPlanArtifacts]);
    const cli = spawnSync(process.execPath, [entry], {
      cwd: tmpdir(), env: { ...process.env, PYTHONDONTWRITEBYTECODE: "1" }, encoding: "utf8"
    });
    assert.equal(cli.status, 0, cli.stderr);
    assert.equal(cli.stdout, "Development plan freshness: PASS\n");
    assert.deepEqual(snapshotState(scratch), before);
    assert.equal(existsSync(join(scratch, "docs/spec/target/checks/development_plan_validation.json")), false);
    assert.equal(existsSync(join(scratch, "docs/spec/target/checks/runs")), false);
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

test("renderer read-path projections are independent of the caller's working directory", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-cwd-"));
  const out = mkdtempSync(join(tmpdir(), "opl-plan-render-"));
  try {
    materializePlanSnapshot(scratch);
    const rendered = spawnSync("python3", [join(scratch, "docs/spec/target/checks/render_development_plan.py"), "--check"], {
      cwd: out,
      env: { ...process.env, OPL_DEVELOPMENT_PLAN_OUT: out, PYTHONDONTWRITEBYTECODE: "1" },
      encoding: "utf8"
    });
    assert.equal(rendered.status, 0, rendered.stderr);
    assert.ok(readFileSync(join(out, "14_implementation_work_packages.md")).equals(readFileSync(join(scratch, generatedPlanArtifacts[1]))), "Markdown projection changed with cwd");
    const plan = JSON.parse(readFileSync(join(out, "checks/development_plan.json"), "utf8"));
    assert.equal(Object.hasOwn(plan, "sourceSHA"), false, "freshness render must not invent provenance");
  } finally {
    rmSync(scratch, { recursive: true, force: true });
    rmSync(out, { recursive: true, force: true });
  }
});

test("the default subprocess validates the requested snapshot rather than the host checkout", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-validator-"));
  try {
    materializePlanSnapshot(scratch);
    // This input is used by the real validator, not by the renderer.
    rmSync(join(scratch, "docs/spec/target/00_master_index.md"));
    assert.throws(() => verifyDevelopmentPlan({ root: scratch }), /development plan validation failed:[\s\S]*00_master_index\.md/u);
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

for (const path of ["services/internal/postgresmigrate", "services/runtime-control"]) {
  test(`a snapshot missing the projection input ${path} does not borrow host path presence`, () => {
    const scratch = mkdtempSync(join(tmpdir(), "opl-plan-missing-"));
    try {
      materializePlanSnapshot(scratch);
      rmSync(join(scratch, path), { recursive: true });
      assert.throws(() => verifyDevelopmentPlan({ root: scratch }), /stale generated plan artifact: docs\/spec\/target\/checks\/development_plan\.json/u);
    } finally {
      rmSync(scratch, { recursive: true, force: true });
    }
  });
}

test("default generation in a Git-free snapshot fails before emitting artifacts instead of fabricating provenance", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-provenance-"));
  const out = mkdtempSync(join(tmpdir(), "opl-plan-output-"));
  try {
    materializePlanSnapshot(scratch);
    const before = snapshotState(scratch);
    const rendered = spawnSync("python3", [join(scratch, "docs/spec/target/checks/render_development_plan.py")], {
      cwd: scratch,
      env: { ...process.env, OPL_DEVELOPMENT_PLAN_OUT: out, PYTHONDONTWRITEBYTECODE: "1" },
      encoding: "utf8"
    });
    assert.equal(rendered.status, 1, rendered.stderr);
    assert.match(rendered.stderr, /not a git repository/u);
    assert.deepEqual(readdirSync(out), []);
    assert.deepEqual(snapshotState(scratch), before);
  } finally {
    rmSync(scratch, { recursive: true, force: true });
    rmSync(out, { recursive: true, force: true });
  }
});

test("default generation retains the real checkout SHA even when launched outside the checkout", () => {
  const out = mkdtempSync(join(tmpdir(), "opl-plan-provenance-output-"));
  try {
    const revision = spawnSync("git", ["-C", root, "rev-parse", "HEAD"], { encoding: "utf8" });
    assert.equal(revision.status, 0, revision.stderr);
    const rendered = spawnSync("python3", [join(root, "docs/spec/target/checks/render_development_plan.py")], {
      cwd: out,
      env: { ...process.env, OPL_DEVELOPMENT_PLAN_OUT: out, PYTHONDONTWRITEBYTECODE: "1" },
      encoding: "utf8"
    });
    assert.equal(rendered.status, 0, rendered.stderr);
    assert.equal(JSON.parse(readFileSync(join(out, "checks/development_plan.json"), "utf8")).sourceSHA, revision.stdout.trim());
    assert.ok(readFileSync(join(out, "14_implementation_work_packages.md")).equals(readFileSync(join(root, generatedPlanArtifacts[1]))));
  } finally {
    rmSync(out, { recursive: true, force: true });
  }
});

test("freshness rendering refuses to write into the input checkout or use the default output", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-check-output-"));
  try {
    materializePlanSnapshot(scratch);
    const before = snapshotState(scratch);
    for (const output of [undefined, join(scratch, "docs/spec/target"), join(scratch, "new-output")]) {
      const env: NodeJS.ProcessEnv = { ...process.env, PYTHONDONTWRITEBYTECODE: "1" };
      delete env.OPL_DEVELOPMENT_PLAN_OUT;
      if (output) env.OPL_DEVELOPMENT_PLAN_OUT = output;
      const rendered = spawnSync("python3", [join(scratch, "docs/spec/target/checks/render_development_plan.py"), "--check"], {
        cwd: scratch, env, encoding: "utf8"
      });
      assert.equal(rendered.status, 2, rendered.stderr);
      assert.match(rendered.stderr, /--check requires OPL_DEVELOPMENT_PLAN_OUT outside the input checkout/u);
    }
    assert.deepEqual(snapshotState(scratch), before);
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

test("a stale or hand-edited plan artifact is rejected before validation", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-stale-"));
  try {
    materializePlanSnapshot(scratch);
    // Simulate an out-of-date projection: drop a required RPC record the renderer would re-add.
    const planPath = join(scratch, generatedPlanArtifacts[0]);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    delete plan.rpcImplementers["FabricCoordination.RebindSecret"];
    writeFileSync(planPath, JSON.stringify(plan, null, 2) + "\n");
    assert.throws(
      () => verifyDevelopmentPlan({ root: scratch }),
      /stale generated plan artifact/u
    );
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

test("a failing renderer or validator blocks the gate instead of passing", () => {
  const failing = () => ({ status: 2, stdout: "", stderr: "renderer crashed" });
  assert.throws(() => verifyDevelopmentPlan({ root, runner: failing }), /plan renderer failed/u);
  let call = 0;
  const validateFails = (command: string, args: string[], env: NodeJS.ProcessEnv, executionRoot: string) => {
    call += 1;
    if (call === 1) return runPlanGenerator(command, args, env, executionRoot); // renderer OK
    return { status: 1, stdout: "{\"passed\":false}", stderr: "" }; // validator fails
  };
  assert.throws(() => verifyDevelopmentPlan({ root, runner: validateFails }), /development plan validation failed/u);
});


test("the plan acceptance runner executes real freshness in an OS-isolated Git-free snapshot", async () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-os-snapshot-"));
  try {
    materializePlanSnapshot(scratch);
    const before = snapshotState(scratch);
    const result = await runDevelopmentCheck({ snapshotRoot: scratch, kind: "developmentPlan" });
    assert.equal(result.status, "passed", result.reason + "\n" + result.output);
    assert.equal(result.passed, true);
    assert.match(result.output, /sandbox probe PASS/u);
    assert.match(result.output, /Development plan freshness: PASS/u);
    assert.deepEqual(snapshotState(scratch), before);
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});
