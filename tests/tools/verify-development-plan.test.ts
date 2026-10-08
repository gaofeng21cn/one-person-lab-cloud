import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cpSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { generatedPlanArtifacts, runPlanGenerator, verifyDevelopmentPlan } from "../../tools/verify-development-plan.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const realPython = "/Library/Developer/CommandLineTools/usr/bin/python3";
const python = spawnSync(realPython, ["-c", "print('ok')"], { encoding: "utf8" }).status === 0 ? realPython : "python3";

function runner(command: string, args: string[], env: NodeJS.ProcessEnv) {
  if (command === "python3") return runPlanGenerator(python, args, env);
  return runPlanGenerator(command, args, env);
}

test("real regeneration byte-matches the checked-in plan artifacts and validates", () => {
  const result = verifyDevelopmentPlan({ root, runner });
  assert.deepEqual(result.artifacts, [...generatedPlanArtifacts]);
});

test("a stale or hand-edited plan artifact is rejected before validation", () => {
  const scratch = mkdtempSync(join(tmpdir(), "opl-plan-stale-"));
  try {
    for (const artifact of generatedPlanArtifacts) {
      const target = join(scratch, artifact);
      mkdirSync(dirname(target), { recursive: true });
      cpSync(join(root, artifact), target);
    }
    // Simulate an out-of-date projection: drop a required RPC record the renderer would re-add.
    const planPath = join(scratch, generatedPlanArtifacts[0]);
    const plan = JSON.parse(readFileSync(planPath, "utf8"));
    delete plan.rpcImplementers["FabricCoordination.RebindSecret"];
    writeFileSync(planPath, JSON.stringify(plan, null, 2) + "\n");
    assert.throws(
      () => verifyDevelopmentPlan({ root: scratch, runner }),
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
  const validateFails = (command: string, args: string[], env: NodeJS.ProcessEnv) => {
    call += 1;
    if (call === 1) return runPlanGenerator(python, args, env); // renderer OK
    return { status: 1, stdout: "{\"passed\":false}", stderr: "" }; // validator fails
  };
  assert.throws(() => verifyDevelopmentPlan({ root, runner: validateFails }), /development plan validation failed/u);
});
