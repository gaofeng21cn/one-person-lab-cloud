import { readFile } from "node:fs/promises";
import { parseArgs } from "node:util";

import { loadInstallationTopology, validateInstallationManifest } from "../services/control-plane/ops/installation-manifest.ts";

import { validateProductionManifest } from "../services/control-plane/ops/production-manifest.ts";

function optionValue(value) {
  return value === undefined ? undefined : String(value || "true");
}

export async function runProductionManifestCli({
  argv = process.argv.slice(2),
  stdout = process.stdout,
  stderr = process.stderr
} = {}) {
  const { values: args } = parseArgs({
    args: argv,
    options: { manifest: { type: "string" }, "installation-manifest": { type: "string" },
      "installation-bindings": { type: "string" }, "installation-secrets": { type: "string" } },
    strict: false,
    allowPositionals: true
  });
  if (args["installation-manifest"] !== undefined) {
    if (args.manifest !== undefined || !args["installation-bindings"] || !args["installation-secrets"]) throw new Error("installation_arguments_invalid");
    let report;
    try {
      const inputs = await Promise.all(["installation-manifest", "installation-bindings", "installation-secrets"].map(async key => JSON.parse(await readFile(String(args[key]), "utf8"))));
      report = validateInstallationManifest(inputs[0], inputs[1], inputs[2], await loadInstallationTopology());
    } catch {
      // JSON parse errors and filesystem errors may contain private material.
      stderr.write("installation_input_invalid\n");
      return 1;
    }
    stdout.write(`${JSON.stringify(report, null, 2)}\n`);
    if (!report.ok) stderr.write("installation_manifest_invalid\n");
    return report.ok ? 0 : 1;
  }
  if (args["installation-bindings"] !== undefined || args["installation-secrets"] !== undefined) throw new Error("installation_arguments_invalid");
  const manifestPath = optionValue(args.manifest);
  if (!manifestPath) throw new Error("manifest_path_required");
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  const report = validateProductionManifest(manifest);
  stdout.write(`${JSON.stringify(report, null, 2)}\n`);
  if (!report.ok) {
    stderr.write("production_manifest_invalid\n");
    return 1;
  }
  return 0;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  runProductionManifestCli().then((code) => {
    process.exitCode = code;
  }).catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
