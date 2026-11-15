import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
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

for (const composePath of ["deploy/portable/compose.cloud.yaml", "deploy/portable/compose.local-v2.yaml"]) {
  test(`${composePath} passes optional plain-HTTP hosts to the Build process`, () => {
    const variables = spawnSync("docker", ["compose", "--env-file", "/dev/null", "-f", composePath, "config", "--variables", "--format", "json"], { encoding: "utf8" });
    assert.equal(variables.status, 0, variables.stderr);
    const declared = JSON.parse(variables.stdout);
    const clean = Object.fromEntries(Object.entries(process.env).filter(([name]) => !/^(OPL_|COMPOSE_)/.test(name)));
    const env = Object.fromEntries(Object.values(declared).map((item: any) => [item.Name, item.DefaultValue || "fixture-value"]));
    Object.assign(env, { OPL_CLOUD_IMAGE: "alpine:3", OPL_PROXY_PORT: "8443", OPL_DOCKER_SUBNET: "172.29.0.0/24", OPL_POSTGRES_HOST: "172.29.0.10", OPL_PKI_DIR: "/tmp/opl-fixture-pki" });
    for (const value of [undefined, "", "registry.example.test:5000,other.example.test:5001"]) {
      const selected = { ...env };
      if (value === undefined) delete selected[insecureHostsEnv];
      else selected[insecureHostsEnv] = value;
      const resolved = spawnSync("docker", ["compose", "--env-file", "/dev/null", "-f", composePath, "config", "--format", "json"], { encoding: "utf8", env: { ...clean, ...selected } });
      assert.equal(resolved.status, 0, resolved.stderr);
      assert.equal(JSON.parse(resolved.stdout).services.build.environment[insecureHostsEnv], value ?? "");
    }
  });
}
