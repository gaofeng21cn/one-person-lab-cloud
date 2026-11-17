import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createServer } from "node:net";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHash, randomBytes } from "node:crypto";
import test from "node:test";

// The Cloud backend is one container: one process space in which the declared
// owner processes collaborate. This suite proves that at the layer it can
// actually reach. It builds the product image, starts ONE container that runs
// the composition entrypoint, and observes the declared owner processes come up
// inside it, satisfy the readiness criterion the owner topology contract
// declares for them, stay mutually consistent, and leave the excluded
// workloads outside.
//
// The suite is opt-in, so ordinary CI stays fast. Run it with:
//
//   OPL_PORTABLE_RUNTIME_READBACK=1 node --test tests/integration/composed-backend-unit.test.ts
//
// The build needs a reachable registry. When the default Go module proxy is
// unreachable, point the build at one that is:
//
//   OPL_TEST_GOPROXY=https://goproxy.cn,direct OPL_PORTABLE_RUNTIME_READBACK=1 \
//     node --test tests/integration/composed-backend-unit.test.ts
//
// When the registry that holds the pinned base images cannot be reached, the
// same base images can be supplied from a reachable mirror with build
// contexts, one comma separated "<from-reference>=<local-image>" pair each:
//
//   OPL_TEST_BUILD_CONTEXTS="node:26-bookworm-slim@sha256:<digest>=docker-image://mirror/library/node:26-bookworm-slim" \
//     OPL_PORTABLE_RUNTIME_READBACK=1 node --test tests/integration/composed-backend-unit.test.ts
const enabled = process.env.OPL_PORTABLE_RUNTIME_READBACK === "1";
const repoRoot = process.cwd();

function command(args: string[], check = true) {
  const result = spawnSync(args[0], args.slice(1), {
    cwd: repoRoot,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024
  });
  if (result.error) throw result.error;
  if (check && result.status !== 0) {
    throw new Error([result.stdout, result.stderr].filter(Boolean).join("\n").trim());
  }
  return result;
}

function docker(args: string[], check = true) {
  return command(["docker", ...args], check);
}

async function unusedHostPort() {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  const port = address.port;
  await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  return port;
}

function sha256(content: Buffer | string) {
  return createHash("sha256").update(content).digest("hex");
}

function shellQuote(value: string) {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

type ContractCheck = {
  transport: string;
  role: string;
  addressEnv: string;
  method?: string;
  path?: string;
  projection?: string;
};

type ContractProcess = {
  service: string;
  kind: string;
  binary: string;
  addressEnv?: string;
  defaultAddress?: string;
  rpcAddressEnv?: string;
  rpcEnabledByEnv?: string;
  additionalListeners?: Array<{ addressEnv: string; defaultAddress?: string }>;
  transportPrincipal: string;
  databaseEnv?: string;
};

type Contract = {
  processes: ContractProcess[];
  readinessCriteria: { criteria: Array<{ process: string; checks: ContractCheck[] }> };
  communication: { edges: Array<{ caller: string; target: string; activation: string; tokenEnv?: string; addressEnv?: string }> };
  distinctAddressesRequired: boolean;
  distinctAddressesScope: string;
};

if (enabled) test("the Cloud backend container is one deployable business unit", {
  timeout: 60 * 60_000
}, async () => {
  const suffix = `${process.pid}-${randomBytes(4).toString("hex")}`;
  const project = `opl-cloud-composed-${suffix}`;
  const image = `${project}:source`;
  const network = `${project}_net`;
  const postgresName = `${project}-postgres`;
  const backendName = `${project}-backend`;
  const mutatedName = `${project}-backend-mutated`;
  const stagedName = `${project}-stage`;
  const tempRoot = await mkdtemp(join(tmpdir(), "opl-cloud-composed-"));
  const envFile = join(tempRoot, "install.env");
  const pkiDir = join(tempRoot, "pki");
  const wrapperDir = join(tempRoot, "wrappers");
  const binaryDir = join(tempRoot, "bin");
  const objectRoot = join(tempRoot, "objects");
  const buildRoot = join(tempRoot, "build");
  const fabricRoot = join(tempRoot, "fabric");
  const workspaceRoot = join(tempRoot, "workspaces");
  const secretRoot = join(tempRoot, "secrets");

  const contract = JSON.parse(
    await readFile(join(repoRoot, "deploy/portable/opl-cloud-owner-topology.json"), "utf8")
  ) as Contract;
  const revision = command(["git", "rev-parse", "HEAD"]).stdout.trim();
  const hostPort = await unusedHostPort();
  const subnetOctet = 20 + (process.pid % 200);
  const subnet = `10.251.${subnetOctet}.0/24`;
  const postgresHost = `10.251.${subnetOctet}.10`;

  // The published installation declares one database and one schema per owner.
  // The published initialization script creates both roles and lets the owner's
  // own migration install the schema.
  const databaseOwners = [
    "capability", "build", "runtime_control", "workspace", "serve",
    "resource_catalog", "fabric", "tenant", "gateway", "ledger", "control_plane"
  ];
  const databasePasswords: Record<string, string> = {};
  for (const owner of databaseOwners) databasePasswords[owner] = randomBytes(32).toString("hex");
  const postgresPassword = randomBytes(32).toString("hex");

  // The transport tokens the contract's communication edges name. Every edge
  // that shares a token environment shares its value, so a caller and the owner
  // it calls always agree on the token.
  const tokenValues: Record<string, string> = {};
  const tokenFor = (name: string | undefined) => {
    if (!name) return "";
    if (!tokenValues[name]) tokenValues[name] = randomBytes(32).toString("hex");
    return tokenValues[name];
  };
  for (const edge of contract.communication.edges) tokenFor(edge.tokenEnv);

  // The peer token map each owner authenticates its callers with. The contract
  // states who calls whom, so the map is derived from the contract's edges
  // instead of being written down here.
  const peerMaps: Record<string, Record<string, string>> = {};
  for (const edge of contract.communication.edges) {
    if (!edge.tokenEnv || edge.caller === edge.target) continue;
    const map = peerMaps[edge.target] ?? (peerMaps[edge.target] = {});
    map[edge.caller] = tokenValues[edge.tokenEnv];
  }
  const peerTokenEnvironment: Record<string, string> = {};
  for (const [principal, map] of Object.entries(peerMaps)) {
    peerTokenEnvironment[`OPL_${principal.toUpperCase()}_PEER_TOKENS`] = JSON.stringify(map);
  }

  const packageSchemaPath = join(repoRoot, "deploy/portable/agent-plugins-1.0.0-plugin.schema.json");
  const publisherSchemaPath = join(repoRoot, "docs/spec/target/contracts/publisher-contract.schema.json");
  const packageSchemaDigest = sha256(await readFile(packageSchemaPath));
  const publisherSchemaDigest = sha256(await readFile(publisherSchemaPath));

  const installEnvironment: Record<string, string> = {
    COMPOSE_PROJECT_NAME: project,
    OPL_AIONUI_ADMIN_PASSWORD_SEED: randomBytes(32).toString("hex"),
    OPL_BIND_ADDRESS: "127.0.0.1",
    OPL_BUILD_BUILDER: "opl-buildkit-local",
    OPL_BUILD_INSECURE_HTTP_HOSTS: "",
    OPL_BUILD_REGISTRY_PREFIX: "registry.local-v2.test",
    OPL_BUILD_REGISTRY_TOKEN: randomBytes(32).toString("hex"),
    OPL_BUILD_TOKEN: randomBytes(32).toString("hex"),
    OPL_CAPABILITY_OBJECT_INTERNAL_URL: "http://capability:8191",
    OPL_CAPABILITY_OBJECT_SIGNING_KEY: randomBytes(32).toString("hex"),
    OPL_CAPABILITY_OBJECT_TOKEN: randomBytes(32).toString("hex"),
    OPL_CAPABILITY_OBJECT_URL: "http://capability:8191",
    OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST: packageSchemaDigest,
    OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH: packageSchemaPath,
    OPL_CAPABILITY_TOKEN: randomBytes(32).toString("hex"),
    OPL_CLOUD_IDENTITY_TOKEN: randomBytes(32).toString("hex"),
    OPL_CLOUD_IMAGE: image,
    OPL_CONTROL_PLANE_TOKEN: randomBytes(32).toString("hex"),
    OPL_CONTROL_PLANE_WORKSPACE_TOKEN: randomBytes(32).toString("hex"),
    OPL_DOCKER_SUBNET: subnet,
    OPL_FABRIC_CAPABILITY_KEY: randomBytes(32).toString("hex"),
    OPL_FABRIC_LOCAL_DOCKER_ALLOW_UNBOUNDED_SWAP: "0",
    OPL_FABRIC_LOCAL_DOCKER_HOST: "127.0.0.1",
    OPL_FABRIC_LOCAL_DOCKER_PUBLISH_HOST: "127.0.0.1",
    OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: JSON.stringify({
      schemaVersion: 1,
      profileId: "local-docker/local",
      capabilityVersion: "local-docker/v1",
      region: "local",
      accountBindings: [{ tenantId: "tenant_composed_fixture", accountId: "local-host" }],
      packages: [{
        id: "basic", name: "Basic Workspace", available: true,
        compute: { id: "local-basic", server: "configured", cpu: 2, memoryGb: 4, instanceType: "local-basic" },
        storage: { id: "local-basic-10gib", sizeGb: 10, quotaPolicy: "linux-project" }
      }]
    }),
    OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT: secretRoot,
    OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: workspaceRoot,
    OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES: "registry.local-v2.test/workspace@sha256:" + "0".repeat(64),
    OPL_FABRIC_RUNNER_SERVICE_TOKEN: randomBytes(32).toString("hex"),
    OPL_FABRIC_SERVE_CAPABILITY_KEY: randomBytes(32).toString("hex"),
    OPL_FABRIC_SERVE_SERVICE_TOKEN: randomBytes(32).toString("hex"),
    OPL_FABRIC_SERVICE_TOKEN: randomBytes(32).toString("hex"),
    OPL_FABRIC_TOKEN: randomBytes(32).toString("hex"),
    OPL_GATEWAY_WORKSPACE_KEY_GROUP: "composed-fixture-key-group",
    OPL_INTERNAL_SERVICE_TOKEN: randomBytes(32).toString("hex"),
    OPL_INVITATION_TTL: "168h",
    OPL_LEDGER_CAPABILITY_KEY: randomBytes(32).toString("hex"),
    OPL_LEDGER_SERVICE_TOKEN: randomBytes(32).toString("hex"),
    OPL_LEDGER_TOKEN: randomBytes(32).toString("hex"),
    OPL_PKI_DIR: pkiDir,
    OPL_PLATFORM_ADMIN_SUBJECTS: "platform-admin@composed.fixture",
    OPL_POSTGRES_ADMIN_PASSWORD: postgresPassword,
    OPL_POSTGRES_HOST: postgresHost,
    OPL_PROXY_PORT: String(hostPort),
    OPL_PUBLIC_URL: `http://127.0.0.1:${hostPort}`,
    OPL_PUBLISHER_SCHEMA_DIGEST: publisherSchemaDigest,
    OPL_RELEASE_SHA: revision,
    OPL_RESOURCE_CATALOG_PROFILE_JSON: JSON.stringify({
      provider: "local-docker", region: "local", billingMode: "LOCAL_NO_CHARGE",
      providerCapabilityVersion: "local-docker/v1"
    }),
    OPL_RESOURCE_CATALOG_TOKEN: randomBytes(32).toString("hex"),
    OPL_RUNTIME_CONTROL_TOKEN: randomBytes(32).toString("hex"),
    OPL_SERVE_TOKEN: randomBytes(32).toString("hex"),
    OPL_SESSION_SIGNING_KEY: randomBytes(32).toString("hex"),
    OPL_SUB2API_ADMIN_EMAIL: "admin@composed.fixture",
    OPL_SUB2API_ADMIN_PASSWORD: randomBytes(32).toString("hex"),
    OPL_SUB2API_BASE_URL: `http://127.0.0.1:${hostPort}`,
    OPL_SUB2API_USER_EMAIL: "user@composed.fixture",
    OPL_SUB2API_USER_PASSWORD: randomBytes(32).toString("hex"),
    OPL_WORKSPACE_DOMAIN: "workspace.composed.fixture",
    OPL_WORKSPACE_IMAGE: "registry.local-v2.test/workspace@sha256:" + "0".repeat(64),
    OPL_WORKSPACE_REGISTRY_HOST: "registry.local-v2.test",
    OPL_WORKSPACE_REGISTRY_NAMESPACE: "opl",
    OPL_WORKSPACE_TOKEN: randomBytes(32).toString("hex"),
    ...peerTokenEnvironment
  };
  for (const owner of databaseOwners) {
    installEnvironment[`OPL_${owner.toUpperCase()}_DATABASE_PASSWORD`] = databasePasswords[owner];
  }

  const writeInstallEnvironment = async () => {
    const values = Object.entries(installEnvironment).map(([name, value]) => `${name}=${value}`);
    await writeFile(envFile, values.join("\n") + "\n", { mode: 0o600 });
  };

  const composeArguments = [
    "compose", "--project-name", project, "--env-file", envFile,
    "-f", "deploy/portable/compose.cloud.yaml"
  ];
  const compose = (args: string[], check = true) => docker([...composeArguments, ...args], check);

  // The published installation is the authority on what each owner binds and
  // which environment variables it reads. This suite renders it and reads the
  // resolved per-owner environments out of the rendering, so no address, token
  // or path is written down here.
  type RenderedService = { command?: string | string[]; environment?: Record<string, string>; image?: string };
  let renderedServices: Record<string, RenderedService> = {};
  const ownerEnvironment = new Map<string, Record<string, string>>();
  let postgresImage = "postgres:16.8-bookworm";

  const loadPublishedInstallation = () => {
    const rendered = JSON.parse(compose(["config", "--format", "json"]).stdout) as {
      services: Record<string, RenderedService>;
    };
    renderedServices = rendered.services;
    const serviceNameByBinary = new Map<string, string>();
    for (const [name, service] of Object.entries(renderedServices)) {
      const command = Array.isArray(service.command) ? service.command[0] : service.command;
      if (command?.startsWith("/usr/local/bin/")) serviceNameByBinary.set(command, name);
    }
    ownerEnvironment.clear();
    for (const process of contract.processes) {
      const serviceName = serviceNameByBinary.get(process.binary);
      assert.ok(serviceName, `the published installation declares no service for ${process.service}`);
      ownerEnvironment.set(process.service, renderedServices[serviceName].environment ?? {});
    }
    const postgresService = Object.values(renderedServices).find((service) => service.image?.startsWith("postgres:"));
    if (postgresService?.image) postgresImage = postgresService.image;
  };

  // The addresses the contract declares. Every owner reaches its peers by the
  // address the published installation gives the caller, and the shared
  // container resolves the published service names to the loopback interface.
  const ownerOfAddressEnvironment = new Map<string, string>();
  for (const process of contract.processes) {
    if (process.addressEnv) ownerOfAddressEnvironment.set(process.addressEnv, process.service);
    if (process.rpcAddressEnv) ownerOfAddressEnvironment.set(process.rpcAddressEnv, process.service);
    for (const listener of process.additionalListeners ?? []) {
      ownerOfAddressEnvironment.set(listener.addressEnv, process.service);
    }
  }
  const certificateEnvironments = new Set(["OPL_GRPC_CERT_FILE", "OPL_GRPC_KEY_FILE"]);

  // The shared container environment. One value per name: for an address it is
  // the address the published installation's callers use, for every other name
  // it is the value the first declared owner carries. The per-owner facts the
  // published installation declares separately (the owner's own database, the
  // tenant's own CloudIdentity reachability, the owner's own trust root) are
  // carried by the owner's own process environment below.
  let shared: Record<string, string> = {};
  const sharedEnvironment = () => {
    const names = new Set<string>();
    for (const environment of ownerEnvironment.values()) {
      for (const name of Object.keys(environment)) names.add(name);
    }
    const environment: Record<string, string> = {};
    for (const name of names) {
      if (certificateEnvironments.has(name)) continue;
      const owner = ownerOfAddressEnvironment.get(name);
      if (owner) {
        const own = ownerEnvironment.get(owner)?.[name];
        const peers = [...ownerEnvironment.entries()]
          .filter(([service]) => service !== owner)
          .map(([, values]) => values[name])
          .filter((value): value is string => value !== undefined && value !== "");
        const peer = peers.find((value) => value === peers[0]);
        environment[name] = peer ?? own ?? "";
        continue;
      }
      for (const process of contract.processes) {
        const value = ownerEnvironment.get(process.service)?.[name];
        if (value !== undefined) {
          environment[name] = value;
          break;
        }
      }
    }
    environment.OPL_GRPC_CERT_DIR = "/pki";
    environment.OPL_RELEASE_SHA = revision;
    return environment;
  };

  // The published installation gives every owner its own environment. The
  // composition entrypoint starts every owner from one shared environment, so
  // the values the published installation declares per owner are carried by the
  // owner's own process environment. It holds that owner's own published
  // values and nothing else, and it runs the owner's own binary.
  const ownerProcessEnvironment = (service: string): string[] => {
    const published = ownerEnvironment.get(service) ?? {};
    const lines = ["#!/bin/sh"];
    for (const [name, value] of Object.entries(published)) {
      if (certificateEnvironments.has(name)) continue;
      if (shared[name] === value) continue;
      lines.push(`export ${name}=${shellQuote(value)}`);
    }
    const binary = contract.processes.find((process) => process.service === service)?.binary.split("/").pop();
    lines.push(`exec /opt/opl-owner-bin/${binary} "$@"`);
    return lines;
  };

  // The published service names the shared container resolves to its own
  // loopback interface, so one container serves the addresses the published
  // installation's callers use.
  const loopbackHosts = (environment: Record<string, string>) => {
    const hosts: string[] = [];
    for (const name of Object.keys(renderedServices)) {
      const pattern = new RegExp(`(^|[^A-Za-z0-9-])${name}(:\\d|:)`);
      if (Object.values(environment).some((value) => pattern.test(value))) hosts.push(name);
    }
    return hosts;
  };

  const containerArguments = (environment: Record<string, string>, name: string, timeout: string) => {
    const argumentsList = [
      "run", "-d", "--name", name, "--network", network, "--user", "root"
    ];
    for (const host of loopbackHosts(environment)) {
      argumentsList.push("--add-host", `${host}=127.0.0.1`);
    }
    argumentsList.push(
      "-v", `${pkiDir}:/pki:ro`,
      "-v", `${join(repoRoot, "deploy/portable/opl-cloud-owner-topology.json")}:/etc/opl/opl-cloud-owner-topology.json:ro`,
      "-v", `${binaryDir}:/opt/opl-owner-bin:ro`,
      "-v", `${packageSchemaPath}:/contracts/agent-plugins-1.0.0-plugin.schema.json:ro`,
      "-v", `${objectRoot}:/var/lib/opl/objects`,
      "-v", `${buildRoot}:/var/lib/opl/build`,
      "-v", `${fabricRoot}:/var/lib/opl/fabric`,
      "-v", `${workspaceRoot}:${workspaceRoot}`,
      "-v", `${secretRoot}:${secretRoot}`
    );
    for (const process of contract.processes) {
      const binary = process.binary.split("/").pop() as string;
      argumentsList.push("-v", `${join(wrapperDir, binary)}:${process.binary}:ro`);
    }
    for (const [key, value] of Object.entries(environment)) {
      argumentsList.push("-e", `${key}=${value}`);
    }
    argumentsList.push(image, "/usr/local/bin/opl-cloud-composition",
      "--topology", "/etc/opl/opl-cloud-owner-topology.json",
      "--readiness-timeout", timeout);
    return argumentsList;
  };

  const waitFor = (predicate: () => boolean, description: string, attempts = 300) => {
    for (let attempt = 0; attempt < attempts; attempt += 1) {
      if (predicate()) return;
      command(["sleep", "1"]);
    }
    assert.fail(description);
  };

  const containerLogs = (name: string) => {
    const logs = docker(["logs", name], false);
    return `${logs.stdout}${logs.stderr}`;
  };
  const containerRunning = (name: string) =>
    docker(["inspect", name, "--format", "{{.State.Running}}"], false).stdout.trim() === "true";

  // The address a caller reaches one listener on. A bare port binds every
  // interface of the shared container, so it is reached through the loopback
  // address.
  const dialable = (address: string) => address.startsWith(":") ? `127.0.0.1${address}` : address;
  const httpURL = (address: string, path: string) =>
    `http://${address.startsWith(":") ? `127.0.0.1${address}` : address}${path}`;

  try {
    await writeInstallEnvironment();
    docker(["version"]);
    docker(["compose", "version"]);

    const buildArguments = [
      "build", "--build-arg", `OPL_SOURCE_SHA=${revision}`,
      "--label", `org.opencontainers.image.revision=${revision}`,
      "--label", "opl.task=composed-backend-unit", "-t", image, "."
    ];
    if (process.env.OPL_TEST_GOPROXY) {
      buildArguments.splice(1, 0, "--build-arg", `GOPROXY=${process.env.OPL_TEST_GOPROXY}`);
    }
    for (const context of (process.env.OPL_TEST_BUILD_CONTEXTS ?? "").split(",").map((entry) => entry.trim()).filter(Boolean)) {
      buildArguments.splice(1, 0, "--build-context", context);
    }
    docker(buildArguments);

    loadPublishedInstallation();
    shared = sharedEnvironment();

    // The image carries the BuildKit client and not the BuildKit daemon: the
    // privileged build executor belongs to the build runner, not to the Cloud
    // backend container.
    const buildkitDaemon = docker([
      "run", "--rm", "--entrypoint", "sh", image, "-c",
      "command -v buildkitd || echo buildkitd-absent"
    ], false).stdout.trim();
    assert.equal(buildkitDaemon, "buildkitd-absent", "the product image ships a BuildKit daemon");
    const buildkitClient = docker([
      "run", "--rm", "--entrypoint", "sh", image, "-c", "docker buildx version"
    ], false).stdout.trim();
    assert.match(buildkitClient, /buildx/, "the product image ships the BuildKit client");

    // The published PKI writes one certificate and key per declared owner.
    command(["sh", join(repoRoot, "deploy/portable/local-v2-pki.sh"), pkiDir]);
    for (const process of contract.processes) {
      await readFile(join(pkiDir, `${process.transportPrincipal}.crt`));
      await readFile(join(pkiDir, `${process.transportPrincipal}.key`));
    }

    docker(["network", "create", "--subnet", subnet, network]);
    docker([
      "run", "-d", "--name", postgresName, "--network", network, "--ip", postgresHost,
      "-e", `POSTGRES_PASSWORD=${postgresPassword}`,
      ...databaseOwners.map((owner) => `-e=OPL_${owner.toUpperCase()}_DATABASE_PASSWORD=${databasePasswords[owner]}`),
      "-v", `${join(repoRoot, "deploy/portable/owner-databases.sh")}:/docker-entrypoint-initdb.d/10-opl-local-v2-databases.sh:ro`,
      postgresImage
    ]);
    waitFor(() => {
      const databases = docker([
        "exec", postgresName, "psql", "-U", "postgres", "-d", "postgres", "-Atqc",
        "select count(*) from pg_database where datname like 'opl_%'"
      ], false).stdout.trim();
      return databases === String(databaseOwners.length);
    }, "the published database initialization did not create every owner database");

    // The owner binaries the contract declares run from the product image. The
    // per-owner process environment runs them, so the real binary is kept
    // beside the wrapper that carries the owner's own environment.
    for (const directory of [binaryDir, wrapperDir, objectRoot, buildRoot, fabricRoot, workspaceRoot, secretRoot]) {
      await mkdir(directory, { recursive: true });
    }
    docker(["create", "--name", stagedName, image]);
    try {
      for (const process of contract.processes) {
        const binary = process.binary.split("/").pop() as string;
        docker(["cp", `${stagedName}:${process.binary}`, join(binaryDir, binary)]);
      }
    } finally {
      docker(["rm", "-f", stagedName], false);
    }
    for (const process of contract.processes) {
      const binary = process.binary.split("/").pop() as string;
      const wrapper = join(wrapperDir, binary);
      await writeFile(wrapper, ownerProcessEnvironment(process.service).join("\n") + "\n");
      await chmod(wrapper, 0o755);
    }

    // The one Cloud container. It runs the composition entrypoint, which starts
    // every owner the contract declares.
    docker(containerArguments(shared, backendName, "3m"));

    const declaredServices = contract.processes.map((process) => process.service);
    waitFor(() => {
      const logs = containerLogs(backendName);
      return declaredServices.every((service) => logs.includes(`owner ${service} is ready`));
    }, `the composition did not bring every declared owner up:\n${containerLogs(backendName)}`);

    const logs = containerLogs(backendName);
    assert.ok(containerRunning(backendName), "the composed container is not running");

    // The owner set the entrypoint started is the contract's declared process
    // set, in the contract's declared order.
    const started = [...logs.matchAll(/starting owner (\S+)/g)].map((match) => match[1]);
    assert.deepEqual(started, declaredServices, "the entrypoint started a different owner set or order");
    const ready = [...logs.matchAll(/owner (\S+) is ready/g)].map((match) => match[1]);
    assert.deepEqual(ready, declaredServices, "the entrypoint did not bring every declared owner to ready");
    assert.match(logs, new RegExp(`composing ${contract.processes.length} owner processes`));

    // One Cloud container: no second container runs the product image.
    const productContainers = docker([
      "ps", "-a", "--filter", `ancestor=${image}`, "--format", "{{.Names}}"
    ]).stdout.split("\n").map((line) => line.trim()).filter(Boolean);
    assert.deepEqual(productContainers, [backendName], "the composition runs more than one Cloud container");

    // Every owner satisfies the readiness criterion the contract declares for
    // it. The published owner probe reads the owner's own health surface with
    // the owner's own identity, which is the readback the installer uses, and
    // the contract's own HTTP routes are read directly.
    for (const process of contract.processes) {
      const criterion = contract.readinessCriteria.criteria.find((entry) => entry.process === process.service);
      assert.ok(criterion, `the contract declares no readiness criterion for ${process.service}`);
      const principal = process.transportPrincipal;
      const grpcCheck = criterion.checks.find((check) => check.transport === "grpc");
      const address = shared[grpcCheck?.addressEnv ?? criterion.checks[0].addressEnv];
      const probe = docker([
        "exec",
        "-e", `OPL_GRPC_CERT_FILE=/pki/${principal}.crt`,
        "-e", `OPL_GRPC_KEY_FILE=/pki/${principal}.key`,
        "-e", "OPL_GRPC_CA_FILE=/pki/ca.crt",
        backendName, "/usr/local/bin/opl-owner-probe",
        "--owner", principal, "--addr", dialable(address),
        "--expected-source-sha", revision, "--timeout", "10s"
      ], false);
      const report = JSON.parse(probe.stdout || "{}") as {
        owner?: string; status?: string; health?: { status?: string };
        fabricReadiness?: { serviceReady?: boolean }; productSourceSha?: string; errorCode?: string;
      };
      assert.equal(probe.status, 0,
        `${process.service} did not satisfy its readiness criterion: ${probe.stdout}${probe.stderr}`);
      assert.equal(report.status, "ok", `${process.service} probe status`);
      assert.equal(report.owner, principal, `${process.service} probe owner`);
      if (report.productSourceSha) {
        assert.equal(report.productSourceSha, revision, `${process.service} runs another revision`);
      }
      if (grpcCheck) {
        assert.equal(report.health?.status, "SERVING", `${process.service} gRPC health`);
      }
      for (const check of criterion.checks) {
        if (check.transport !== "http") continue;
        const url = httpURL(shared[check.addressEnv], check.path as string);
        const response = docker([
          "exec", backendName, "curl", "--silent", "--show-error", "--write-out", "\n%{http_code}", url
        ], false);
        const [body, code] = response.stdout.split("\n").slice(-2);
        assert.equal(code?.trim(), "200", `${process.service} ${check.path} answered ${code}`);
        if (check.projection === "contracts.FabricReadiness") {
          const projection = JSON.parse(body) as { serviceReady?: boolean };
          assert.equal(projection.serviceReady, true, `${process.service} ${check.path} readiness projection`);
        }
      }
    }

    // The owners are mutually consistent: no two owners share a listen
    // address, and the container listens on nothing the contract does not
    // declare. A listener that the contract does not declare would be a
    // workload that belongs outside the composed unit.
    const sockets = docker([
      "exec", backendName, "sh", "-c", "cat /proc/net/tcp /proc/net/tcp6"
    ], false).stdout;
    const listeningPorts = new Set<number>();
    for (const line of sockets.split("\n").slice(1)) {
      const fields = line.trim().split(/\s+/);
      if (fields.length < 4) continue;
      if (fields[3] !== "0A") continue;
      const port = parseInt(fields[1].split(":")[1], 16);
      if (Number.isInteger(port)) listeningPorts.add(port);
    }
    const declaredPorts = new Map<number, string>();
    for (const process of contract.processes) {
      const environments = [process.addressEnv, process.rpcAddressEnv, ...(process.additionalListeners ?? []).map((listener) => listener.addressEnv)]
        .filter((name): name is string => typeof name === "string");
      for (const name of environments) {
        const address = shared[name] ?? "";
        const port = Number(address.split(":").pop());
        assert.ok(Number.isInteger(port) && port > 0, `${process.service} declares no port for ${name}`);
        const owner = declaredPorts.get(port);
        assert.equal(owner, undefined, `${owner} and ${process.service} share the listen port ${port}`);
        declaredPorts.set(port, process.service);
      }
    }
    for (const [port, owner] of declaredPorts) {
      assert.ok(listeningPorts.has(port), `${owner} does not listen on its declared port ${port}`);
    }
    const undeclared = [...listeningPorts].filter((port) => !declaredPorts.has(port));
    assert.deepEqual(undeclared, [], "the composed container listens on an address the contract does not declare");

    // The excluded workloads stay outside the composed unit. The container
    // runs the composition entrypoint and the declared owner processes and
    // nothing else: no privileged build executor, no customer application.
    const processList = docker([
      "exec", backendName, "sh", "-c",
      "for entry in /proc/[0-9]*/cmdline; do tr '\\0' ' ' < \"$entry\"; echo; done"
    ], false).stdout.split("\n").map((line) => line.trim()).filter(Boolean);
    const allowedBinaries = new Set<string>([
      "/usr/local/bin/opl-cloud-composition",
      ...contract.processes.map((process) => `/opt/opl-owner-bin/${process.binary.split("/").pop()}`)
    ]);
    for (const line of processList) {
      const binary = line.split(" ")[0];
      if (binary === "/bin/sh" || binary === "sh") continue;
      assert.ok(allowedBinaries.has(binary),
        `the composed container runs a process the contract does not declare: ${line}`);
    }
    for (const process of contract.processes) {
      assert.ok(
        processList.some((line) => line.includes(`/opt/opl-owner-bin/${process.binary.split("/").pop()}`)),
        `${process.service} does not run inside the composed container: ${processList.join(" | ")}`
      );
    }
    assert.equal(
      docker(["ps", "-a", "--filter", `ancestor=${image}`, "--format", "{{.Names}}"]).stdout.split("\n").filter((line) => line.trim()).length,
      1,
      "a customer application container was deployed beside the composed unit"
    );

    // A failure is a failure. One owner that never becomes ready stops the
    // composition, and the composition names that owner.
    const mutated = { ...shared };
    const mutatedOwner = contract.processes[0];
    mutated[mutatedOwner.addressEnv as string] = "capability:9999";
    docker(containerArguments(mutated, mutatedName, "30s"));
    waitFor(() => !containerRunning(mutatedName), "the mutated composition did not stop");
    const mutatedLogs = containerLogs(mutatedName);
    assert.match(mutatedLogs, /composition failed/, "the mutated composition did not report a failure");
    assert.match(mutatedLogs, new RegExp(`owner ${mutatedOwner.service} (did not become ready|exited)`),
      "the mutated composition did not name the owner that never became ready");
    assert.doesNotMatch(mutatedLogs, new RegExp(`owner ${mutatedOwner.service} is ready`),
      "the mutated composition reported the broken owner as ready");
  } catch (error) {
    const inspection = docker(["inspect", backendName, "--format", "status={{.State.Status}} exit={{.State.ExitCode}}"], false);
    const logs = docker(["logs", "--tail", "200", backendName], false);
    throw new Error([
      error instanceof Error ? error.stack ?? error.message : String(error),
      inspection.stdout, inspection.stderr, logs.stdout, logs.stderr
    ].filter(Boolean).join("\n"));
  } finally {
    docker(["rm", "-f", backendName], false);
    docker(["rm", "-f", mutatedName], false);
    docker(["rm", "-f", postgresName], false);
    docker(["rm", "-f", stagedName], false);
    docker(["network", "rm", network], false);
    docker(["rmi", "-f", image], false);
    assert.equal(
      docker(["images", "--format", "{{.Repository}}:{{.Tag}}"], false).stdout.includes(image),
      false,
      "the composed unit image was not removed"
    );
    await rm(tempRoot, { recursive: true, force: true });
  }
});
