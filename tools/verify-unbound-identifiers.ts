import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// A qualification harness that calls an identifier it never bound fails at the
// first execution of that branch and nowhere earlier: the strict project list
// does not include these files, so `npm run typecheck:development-tools` cannot
// see it. Two such calls shipped (the launch readback bound by PR #1010 and the
// live-lane runtime readback tracked in #1012), each only observable on a real
// host run. This check narrows the compiler to the diagnostics that mean
// "referenced but never declared" and fails on any of them.
const unboundIdentifierCodes = Object.freeze(["TS2304", "TS2448", "TS2552"]);

const compilerFlags = Object.freeze([
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

const root = fileURLToPath(new URL("..", import.meta.url));
const compiler = fileURLToPath(new URL("../node_modules/typescript/bin/tsc", import.meta.url));

const sources = readdirSync(join(root, "tools"))
  .filter((name) => name.endsWith(".ts"))
  .sort()
  .map((name) => join("tools", name));
if (sources.length === 0) throw new Error("no development tool sources were found to check");

const compiled = spawnSync(process.execPath, [compiler, ...compilerFlags, ...sources], {
  cwd: root,
  encoding: "utf8",
  maxBuffer: 32 * 1024 * 1024
});
if (compiled.error) throw new Error(`the TypeScript compiler could not run: ${compiled.error.message}`);

const output = `${compiled.stdout ?? ""}${compiled.stderr ?? ""}`;
const offenders = output
  .split("\n")
  .filter((line) => unboundIdentifierCodes.some((code) => line.includes(`error ${code}:`)));
if (offenders.length > 0) {
  console.error("Development tool sources reference identifiers they never declare:");
  for (const line of offenders) console.error(`  ${line.trim()}`);
  console.error("A qualification run reaches such a branch at runtime and nowhere earlier.");
  process.exit(1);
}
console.log(`Unbound identifiers: none across ${sources.length} development tool sources.`);
