import { spawnSync } from "node:child_process";
import { readdirSync, readFileSync, realpathSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// A qualification harness that calls a name which is not in scope at that point
// fails at the first execution of that branch and nowhere earlier: the strict
// project list does not include these files, so
// `npm run typecheck:development-tools` cannot see it. Two such calls shipped
// (the launch readback bound by PR #1010 and the live-lane runtime readback
// fixed in #1012), each only observable on a real host run. This check narrows
// the compiler to the diagnostics that mean a name is not in scope (undeclared,
// declared later in the same scope, or a misspelling) and fails on any of them.
const unboundIdentifierCodes = Object.freeze(["TS2304", "TS2448", "TS2552"]);

// A run that never evaluated these sources must not report a clean scan. These
// codes mean the compiler gave up rather than that it looked: 1xxx a source it
// could not parse, 2688 a missing type definition, 18003 no inputs, 5xxx a
// compiler-option fault and 6xxx a command-line fault. A check that cannot run
// has to fail, not pass.
const diagnosticPattern = /error TS\d{4,5}:/u;
const compilerRefusalPattern = /error TS(?:1\d{3}|2688|18003|5\d{3}|6\d{3}):/u;

// These pragmas make the compiler skip a file or a line while still exiting 0,
// which is the same silent pass this check exists to prevent. No current tool
// source uses one; a future need is a decision to make here, not a per-line
// escape that quietly disables the check for that name.
const suppressionPattern = /^\s*\/\/\s*@ts-(?:nocheck|ignore|expect-error)\b/mu;

export const compilerFlags = Object.freeze([
  "--ignoreConfig",
  "--strict",
  "--noEmit",
  "--skipLibCheck",
  "--target", "ES2023",
  "--module", "NodeNext",
  "--moduleResolution", "NodeNext",
  "--allowImportingTsExtensions",
  "--types", "node",
  "--lib", "ES2023,DOM"
]);

export function unboundIdentifierOffenders(output: string): string[] {
  return output
    .split("\n")
    .filter((line) => unboundIdentifierCodes.some((code) => line.includes(`error ${code}:`)));
}

export function compilerRefusals(output: string): string[] {
  return output.split("\n").filter((line) => compilerRefusalPattern.test(line));
}

/** A non-zero exit with no diagnostic means the compiler never looked at the sources. */
export function unevaluatedRun(status: number | null, output: string): boolean {
  return status !== 0 && !diagnosticPattern.test(output);
}

function developmentToolSources(root: string): string[] {
  return readdirSync(join(root, "tools"))
    .filter((name) => name.endsWith(".ts"))
    .sort()
    .map((name) => join("tools", name));
}

function main(): void {
  const root = fileURLToPath(new URL("..", import.meta.url));
  const compiler = fileURLToPath(new URL("../node_modules/typescript/bin/tsc", import.meta.url));
  const sources = developmentToolSources(root);
  if (sources.length === 0) throw new Error("no development tool sources were found to check");

  const suppressed = sources.filter((path) => suppressionPattern.test(readFileSync(join(root, path), "utf8")));
  if (suppressed.length > 0) {
    console.error("Development tool sources suppress the compiler, so they were never checked:");
    for (const path of suppressed) console.error(`  ${path} suppresses the compiler with a @ts- pragma`);
    process.exit(1);
  }

  const compiled = spawnSync(process.execPath, [compiler, ...compilerFlags, ...sources], {
    cwd: root,
    encoding: "utf8",
    timeout: 120_000,
    maxBuffer: 32 * 1024 * 1024
  });
  if (compiled.error) throw new Error(`the TypeScript compiler could not run: ${compiled.error.message}`);
  if (compiled.signal) throw new Error(`the TypeScript compiler was terminated by ${compiled.signal}`);

  const output = `${compiled.stdout ?? ""}${compiled.stderr ?? ""}`;
  const refusals = compilerRefusals(output);
  if (refusals.length > 0) {
    console.error("The TypeScript compiler refused this configuration, so no source was evaluated:");
    for (const line of refusals) console.error(`  ${line.trim()}`);
    process.exit(1);
  }
  if (unevaluatedRun(compiled.status, output)) {
    console.error(`The TypeScript compiler exited ${compiled.status ?? "without a status"} and reported no diagnostic:`);
    for (const line of output.trim().split("\n").slice(0, 20)) console.error(`  ${line.trim()}`);
    console.error("No source was evaluated, so this check proves nothing.");
    process.exit(1);
  }
  const offenders = unboundIdentifierOffenders(output);
  if (offenders.length > 0) {
    console.error("Development tool sources reference names that are not in scope at that point:");
    for (const line of offenders) console.error(`  ${line.trim()}`);
    console.error("A qualification run reaches such a branch at runtime and nowhere earlier.");
    process.exit(1);
  }
  console.log(`Unbound identifiers: none across ${sources.length} development tool sources.`);
}

// realpath, not the raw argv path: node resolves symlinks for import.meta.url
// but not for argv[1], and an unresolved comparison skips main() silently.
if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) main();
