import { readFile, realpath } from "node:fs/promises";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";

import { installationFailure, installationSecurity, validateInstallationManifest } from "./ops/installation-manifest.ts";

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
    options: { manifest: { type: "string" }, "installation-manifest": { type: "string" } },
    strict: false,
    allowPositionals: true
  });
  const installationPath = optionValue(args["installation-manifest"]);
  if (installationPath) {
    let topology;
    let report;
    try {
      topology = JSON.parse(await readFile(new URL("../deploy/portable/opl-cloud-owner-topology.json", import.meta.url), "utf8"));
    } catch {
      report = installationFailure("installation_topology_unreadable");
    }
    const security = installationSecurity(topology);
    if (!report && !security) report = installationFailure("installation_topology_security_invalid");
    let manifest;
    if (!report) {
      try {
        manifest = await readFile(installationPath, "utf8");
      } catch {
        report = installationFailure("installation_manifest_read_failed", security);
      }
    }
    if (!report) {
      try {
        manifest = JSON.parse(manifest);
      } catch {
        report = installationFailure("installation_manifest_json_invalid", security);
      }
    }
    report ??= validateInstallationManifest(manifest, topology);
    stdout.write(`${JSON.stringify(report, null, 2)}\n`);
    if (!report.ok) stderr.write("installation_manifest_invalid\n");
    return report.ok ? 0 : 1;
  }
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

if (process.argv[1] && import.meta.url === pathToFileURL(await realpath(process.argv[1])).href) {
  runProductionManifestCli().then((code) => {
    process.exitCode = code;
  }).catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
