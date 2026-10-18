import { spawnSync } from "node:child_process";
import { parseArgs } from "node:util";

// npm appends script arguments after the file inventory. Node only consumes
// test-runner options before the files, so place the shard there explicitly.
const { values, positionals } = parseArgs({
  options: { "test-shard": { type: "string" } },
  allowPositionals: true
});
if (positionals.length === 0) throw new Error("browser suite requires explicit test files");
const result = spawnSync(process.execPath, [
  "--test", "--test-concurrency=3",
  ...(values["test-shard"] === undefined ? [] : [`--test-shard=${values["test-shard"]}`]),
  ...positionals
], { stdio: "inherit" });
if (result.error) throw result.error;
process.exitCode = result.status ?? 1;
