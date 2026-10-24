import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import YAML from "yaml";

const composePath = "deploy/portable/compose.local-v2.yaml";
const baseComposePath = "compose.yaml";
const environmentPath = "deploy/portable/opl-cloud.env.example";

// A variable the Compose file reads. The negative lookbehind keeps an escaped
// dollar sign out of the result: the PostgreSQL initialization script writes
// shell expansions as $${...}, and those are not Compose variables.
const interpolation = /(?<!\$)\$\{([A-Za-z_][A-Za-z0-9_]*)([:?\-][^}]*)?\}/g;

const declaredSubnets = (source: string): string[] =>
  [...source.matchAll(/subnet:\s*\$\{[A-Za-z_][A-Za-z0-9_]*:-([^}]+)\}/g)].map((match) => match[1]);

const declaredValues = (source: string): Map<string, string> => {
  const values = new Map<string, string>();
  for (const line of source.split("\n")) {
    const match = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line);
    if (match && !values.has(match[1])) {
      values.set(match[1], match[2]);
    }
  }
  return values;
};

test("the local v2 topology declares every variable it reads exactly once", async () => {
  const composeSource = await readFile(composePath, "utf8");
  const environmentSource = await readFile(environmentPath, "utf8");

  const referenced = new Set<string>();
  for (const match of composeSource.matchAll(interpolation)) {
    referenced.add(match[1]);
  }

  const declared = new Map<string, number>();
  for (const line of environmentSource.split("\n")) {
    const match = /^([A-Za-z_][A-Za-z0-9_]*)=/.exec(line);
    if (match) {
      declared.set(match[1], (declared.get(match[1]) ?? 0) + 1);
    }
  }

  const missing = [...referenced].filter((name) => !declared.has(name)).sort();
  assert.deepEqual(missing, [], `${environmentPath} must declare every variable ${composePath} reads`);

  // The Compose environment reader keeps the last definition of a repeated
  // name, while a reader that matches the first definition sees the other
  // value. A name that appears twice is therefore two disagreeing facts.
  const repeated = [...declared.entries()]
    .filter(([, count]) => count > 1)
    .map(([name]) => name)
    .sort();
  assert.deepEqual(
    repeated.filter((name) => referenced.has(name)),
    [],
    `${environmentPath} must declare a variable the topology reads exactly once`,
  );
});

test("the local v2 topology does not claim the base installation's subnet", async () => {
  const v2Source = await readFile(composePath, "utf8");
  const baseSource = await readFile(baseComposePath, "utf8");
  const environmentSource = await readFile(environmentPath, "utf8");

  const v2Subnets = declaredSubnets(v2Source);
  const baseSubnets = declaredSubnets(baseSource);
  assert.equal(v2Subnets.length, 1, `${composePath} must declare exactly one subnet`);
  assert.ok(baseSubnets.length >= 1, `${baseComposePath} must declare a subnet`);

  // Docker refuses two networks with the same subnet on one host, so a host
  // that runs both installations cannot start the second one.
  for (const v2 of v2Subnets) {
    for (const base of baseSubnets) {
      assert.notEqual(
        v2,
        base,
        `the local v2 topology's subnet must differ from the base installation's default subnet`,
      );
    }
  }

  const environment = declaredValues(environmentSource);
  const declaredSubnet = environment.get("OPL_LOCAL_V2_SUBNET");
  assert.equal(declaredSubnet, v2Subnets[0], `${environmentPath} must declare the topology's subnet`);

  const baseSubnet = environment.get("OPL_DOCKER_SUBNET");
  if (baseSubnet) {
    assert.notEqual(
      declaredSubnet,
      baseSubnet,
      `the local v2 topology's subnet must differ from the base installation's declared subnet`,
    );
  }

  // The PostgreSQL address belongs to the topology's own network.
  const postgresHost = environment.get("OPL_LOCAL_V2_POSTGRES_HOST");
  assert.ok(postgresHost, `${environmentPath} must declare the topology's PostgreSQL address`);
  const prefix = declaredSubnet!.split("/")[0].split(".").slice(0, 3).join(".");
  assert.ok(
    postgresHost!.startsWith(`${prefix}.`),
    `the PostgreSQL address must belong to the topology's own subnet`,
  );

  // Every owner connects to PostgreSQL over one explicit address with
  // PGSSLMODE=disable, and the owner's own migration refuses any host that is
  // not an IPv4 literal in a private range
  // (services/internal/postgresmigrate/migrate.go). A public or otherwise
  // non-private address in the topology's subnet would stop every owner from
  // starting, so the fact is checked here instead of at runtime.
  assert.match(postgresHost!, /^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/, `the PostgreSQL address must be an IPv4 literal`);
  const octets = postgresHost!.split(".").map((part) => Number(part));
  const isPrivate =
    octets[0] === 10 ||
    (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) ||
    (octets[0] === 192 && octets[1] === 168);
  assert.ok(isPrivate, `the PostgreSQL address must be a private IPv4 address`);
});

test("the local v2 topology names the Local-Docker gateway container once", async () => {
  const compose = YAML.parse(await readFile(composePath, "utf8"));
  const services = compose.services as Record<string, Record<string, unknown>>;

  const controlPlane = services["control-plane"];
  assert.ok(controlPlane, `${composePath} must declare the retained Control Plane`);

  // The provider reaches a workload through the container it bridges into that
  // workload's network, and it names that container with one installation
  // fact. The name has to be the same fact on both ends: a name Compose
  // generates and a name the operator sets can drift apart, and the provider
  // would then look for a container that does not exist.
  const containerName = controlPlane.container_name as string;
  assert.ok(containerName, `the Control Plane must carry the gateway container name`);
  assert.match(
    containerName,
    /^\$\{OPL_LOCAL_V2_GATEWAY_CONTAINER:-[^}]+\}$/,
    `the gateway container name must come from one installation fact`,
  );

  const fabric = services.fabric;
  const fabricEnvironment = fabric.environment as Record<string, string>;
  const gateway = fabricEnvironment.OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER;
  assert.equal(
    gateway,
    containerName,
    `the provider and the container must read the same gateway container fact`,
  );

  // No other service may claim the same name.
  for (const [name, service] of Object.entries(services)) {
    if (name === "control-plane") {
      continue;
    }
    assert.notEqual(
      (service as Record<string, unknown>).container_name,
      containerName,
      `only one service may carry the gateway container name`,
    );
  }
});
