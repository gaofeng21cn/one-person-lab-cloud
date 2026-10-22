import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cp, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import {
  compilerFlags,
  compilerRefusals,
  unboundIdentifierOffenders,
  unevaluatedRun
} from "../../tools/verify-unbound-identifiers.ts";

const repositoryRoot = fileURLToPath(new URL("../..", import.meta.url));
const checkSource = fileURLToPath(new URL("../../tools/verify-unbound-identifiers.ts", import.meta.url));

async function scratchCheck(t: { after: (fn: () => Promise<void>) => void }): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "unbound-identifiers-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, "tools"), { recursive: true });
  // NodeNext decides module format from the nearest package.json; without this
  // the scratch sources compile as CommonJS and `import.meta` alone is an error.
  await writeFile(join(root, "package.json"), '{"type":"module"}\n');
  await cp(checkSource, join(root, "tools", "verify-unbound-identifiers.ts"));
  return root;
}

function runCheck(root: string) {
  return spawnSync(process.execPath, ["tools/verify-unbound-identifiers.ts"], { cwd: root, encoding: "utf8" });
}

test("the unbound-identifier check reports the diagnostics that mean a name was never declared", () => {
  const output = [
    "tools/local-workspace-qualification.ts(1624,33): error TS2304: Cannot find name 'runtimeImageReadback'.",
    "tools/other.ts(4,9): error TS2448: Block-scoped variable 'launch' used before its declaration.",
    "tools/third.ts(8,15): error TS2552: Cannot find name 'receipt'. Did you mean 'receiptId'?",
    "tools/unrelated.ts(1,1): error TS7006: Parameter 'options' implicitly has an 'any' type."
  ].join("\n");

  const offenders = unboundIdentifierOffenders(output);
  assert.equal(offenders.length, 3);
  assert.match(offenders[0], /TS2304/);
  assert.match(offenders[1], /TS2448/);
  assert.match(offenders[2], /TS2552/);
  assert.ok(offenders.every((line) => !line.includes("TS7006")), "an unrelated strict diagnostic must not count as unbound");
});

test("the unbound-identifier check reports a compiler that refused to evaluate the sources", () => {
  assert.deepEqual(compilerRefusals("error TS5023: Unknown compiler option '--bogus-flag'."), [
    "error TS5023: Unknown compiler option '--bogus-flag'."
  ]);
  assert.deepEqual(compilerRefusals("error TS6064: Option 'project' cannot be mixed with source files."), [
    "error TS6064: Option 'project' cannot be mixed with source files."
  ]);
  assert.deepEqual(compilerRefusals("tools/broken.ts(3,1): error TS1005: ';' expected."), [
    "tools/broken.ts(3,1): error TS1005: ';' expected."
  ], "a source the compiler cannot parse was not meaningfully evaluated");
  assert.deepEqual(compilerRefusals("error TS2688: Cannot find type definition file for 'node'."), [
    "error TS2688: Cannot find type definition file for 'node'."
  ], "missing type definitions mean the program was not resolved");
  assert.deepEqual(compilerRefusals("error TS18003: No inputs were found in config file."), [
    "error TS18003: No inputs were found in config file."
  ], "a run with no inputs evaluated nothing");
  assert.deepEqual(compilerRefusals(""), []);
  assert.deepEqual(
    compilerRefusals("tools/local-workspace-qualification.ts(1624,33): error TS2304: Cannot find name 'x'."),
    [],
    "a source diagnostic is not a compiler refusal"
  );
});

test("the unbound-identifier check compiles under the same flags as the strict development-tools typecheck", async () => {
  const manifest = JSON.parse(await readFile(new URL("../../package.json", import.meta.url), "utf8"));
  assert.ok(
    manifest.scripts["typecheck:development-tools"].includes(compilerFlags.join(" ")),
    "the two invocations must not drift apart; a differing flag makes this check compile under settings nobody reviews"
  );
});

test("the unbound-identifier check treats a non-zero exit without any diagnostic as unevaluated", () => {
  assert.equal(unevaluatedRun(1, "node:internal/modules/cjs/loader:1423\nError: Cannot find module '...'"), true);
  assert.equal(unevaluatedRun(null, ""), true, "no exit status means the run did not complete");
  assert.equal(
    unevaluatedRun(1, "tools/a.ts(1,1): error TS2304: Cannot find name 'x'."),
    false,
    "a run that produced diagnostics did evaluate the sources"
  );
  assert.equal(unevaluatedRun(0, ""), false, "a clean compile is not an unevaluated run");
});

test("the unbound-identifier check passes on the current sources", () => {
  const run = spawnSync(process.execPath, ["tools/verify-unbound-identifiers.ts"], {
    cwd: repositoryRoot,
    encoding: "utf8"
  });
  assert.equal(run.status, 0, `${run.stdout}${run.stderr}`);
  assert.match(run.stdout, /Unbound identifiers: none across \d+ development tool sources\./);
});

test("the unbound-identifier check fails on a real source that calls an undeclared name", async (t) => {
  const root = await scratchCheck(t);
  await symlink(join(repositoryRoot, "node_modules"), join(root, "node_modules"));
  await writeFile(
    join(root, "tools", "fixture.ts"),
    [
      "export async function run() {",
      "  const runtime = await runtimeImageReadback('account', 'workspace', 'image', 'inspection');",
      "  return runtime.Image;",
      "}",
      ""
    ].join("\n")
  );

  const run = runCheck(root);
  assert.equal(run.status, 1, `expected a failure, got: ${run.stdout}${run.stderr}`);
  assert.match(run.stderr, /error TS2304: Cannot find name 'runtimeImageReadback'/);
});

test("the unbound-identifier check fails closed when the compiler cannot load", async (t) => {
  // No node_modules in the scratch root: node starts, the compiler module does
  // not, and nothing is evaluated. Reporting a clean result here would let a
  // broken install disable the check silently.
  const root = await scratchCheck(t);

  const run = runCheck(root);
  assert.notEqual(run.status, 0, `expected a failure, got: ${run.stdout}${run.stderr}`);
  assert.doesNotMatch(run.stdout, /Unbound identifiers: none/);
  assert.match(run.stderr, /No source was evaluated/);
});

test("the unbound-identifier check refuses a source that suppresses the compiler", async (t) => {
  const root = await scratchCheck(t);
  await symlink(join(repositoryRoot, "node_modules"), join(root, "node_modules"));
  await writeFile(
    join(root, "tools", "suppressed.ts"),
    [
      "// @ts-nocheck",
      "export function run() {",
      "  return neverDeclaredAnywhere();",
      "}",
      ""
    ].join("\n")
  );

  const run = runCheck(root);
  assert.equal(run.status, 1, `expected a failure, got: ${run.stdout}${run.stderr}`);
  assert.match(run.stderr, /suppresses the compiler with a @ts- pragma/);
});
