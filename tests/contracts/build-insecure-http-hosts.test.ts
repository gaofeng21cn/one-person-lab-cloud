import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// The Build worker reads registry blobs over the transport the installation
// declares. The per-host plain-HTTP list is a declared installation fact, so
// the three owners of that fact have to agree: the process that reads it, the
// published owner topology that declares what the process reads, and the
// published environment template that names every declared variable.

const buildEntrypoint = "services/build/cmd/server/main.go";
const topologyPath = "deploy/portable/opl-cloud-owner-topology.json";
const envExamplePath = "deploy/portable/opl-cloud.env.example";

const insecureHostsEnv = "OPL_BUILD_INSECURE_HTTP_HOSTS";

test("the Build owner reads the declared plain-HTTP registry hosts", async () => {
  const source = await readFile(buildEntrypoint, "utf8");

  assert.match(source, new RegExp(`Getenv\\("${insecureHostsEnv}"\\)`),
    `the Build owner must read ${insecureHostsEnv} from the environment`);
  assert.match(source, /InsecureHTTPHosts:\s*buildservice\.ParseInsecureHTTPHosts\(/,
    "the Build owner must fill the per-host list the worker already honours");
});

test("the published owner topology declares the plain-HTTP registry host list", async () => {
  const topology = JSON.parse(await readFile(topologyPath, "utf8"));
  const build = topology.processes.find((process: { service: string }) => process.service === "build");

  assert.ok(build, "the published owner topology must declare the Build owner");
  const declared = [...(build.env ?? []), ...(build.optionalEnv ?? []), ...(build.externalEnv ?? [])];
  assert.ok(declared.includes(insecureHostsEnv),
    `the published owner topology must declare ${insecureHostsEnv} for the Build owner`);
});

test("the published environment template names the plain-HTTP registry host list", async () => {
  const template = await readFile(envExamplePath, "utf8");

  assert.match(template, new RegExp(`^${insecureHostsEnv}=`, "m"),
    `the published environment template must name ${insecureHostsEnv}`);
});
