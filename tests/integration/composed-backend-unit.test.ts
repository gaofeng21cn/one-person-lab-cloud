import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createServer } from "node:net";
import { chmod, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
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

// The owners state a schema digest the way the product reads it: the
// algorithm and the digest.
function sha256(content: Buffer | string) {
  return `sha256:${createHash("sha256").update(content).digest("hex")}`;
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
  addressPortEnv?: string;
  rpcAddressEnv?: string;
  defaultRPCAddress?: string;
  rpcEnabledByEnv?: string;
  additionalListeners?: Array<{ addressEnv: string; defaultAddress?: string }>;
  transportPrincipal: string;
  databaseEnv?: string;
  rpcDatabaseEnv?: string;
  peerTokensEnv?: string;
  env?: string[];
  optionalEnv?: string[];
  externalEnv?: string[];
};

type ContractEdge = {
  caller: string;
  target: string;
  activation: string;
  tokenEnv?: string;
  addressEnv?: string;
  activationEnvs?: string[];
};

type Contract = {
  processes: ContractProcess[];
  readinessCriteria: { criteria: Array<{ process: string; checks: ContractCheck[] }> };
  communication: { edges: ContractEdge[] };
  peerAddresses?: { entries: Array<{ caller: string; target: string; addressEnv?: string; tokenEnv?: string; targetAddressEnv?: string }> };
  sharedEnv?: Record<string, string[] | string>;
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
  const sub2apiName = `${project}-sub2api`;
  const backendName = `${project}-backend`;
  const mutatedName = `${project}-backend-mutated`;
  const tempRoot = await mkdtemp(join(tmpdir(), "opl-cloud-composed-"));
  const sub2apiScript = join(tempRoot, "sub2api.mjs");
  const sub2apiTLSRoot = join(tempRoot, "sub2api-tls");
  const sub2apiPort = 8080;
  const envFile = join(tempRoot, "install.env");
  const pkiDir = join(tempRoot, "pki");
  const objectRoot = join(tempRoot, "objects");
  const buildRoot = join(tempRoot, "build");
  const fabricRoot = join(tempRoot, "fabric");
  const workspaceRoot = join(tempRoot, "workspaces");
  const secretRoot = join(tempRoot, "secrets");

  const topologyPath = join(repoRoot, "deploy/portable/opl-cloud-owner-topology.json");
  const contract = JSON.parse(await readFile(topologyPath, "utf8")) as Contract;
  const revision = command(["git", "rev-parse", "HEAD"]).stdout.trim();

  // The per-owner name the composition reads one owner's own secret by.
  const ownerAlias = (process: ContractProcess) =>
    `OPL_${process.transportPrincipal.toUpperCase().replace(/-/g, "_")}`;

  // The listener the contract declares for one of an owner's own address
  // environments.
  const declaredListener = (process: ContractProcess, name?: string): string => {
    if (!name) return "";
    if (name === process.addressEnv) return process.defaultAddress ?? "";
    if (name === process.rpcAddressEnv) return process.defaultRPCAddress ?? "";
    for (const listener of process.additionalListeners ?? []) {
      if (listener.addressEnv === name) return listener.defaultAddress ?? "";
    }
    return "";
  };

  const hostPort = await unusedHostPort();
  const subnetOctet = 20 + (process.pid % 200);
  const subnet = `10.251.${subnetOctet}.0/24`;
  const postgresHost = `10.251.${subnetOctet}.10`;
  const sub2apiHost = `10.251.${subnetOctet}.11`;

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

  // The object store is reached at the installation's own public origin, the
  // way the published installation supplies it: the Capability owner signs
  // upload permits for it and the Build owner uploads through it. One
  // container has no published service name to name.
  const capabilityObjectURL = `https://127.0.0.1:${hostPort}/objects`;

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
    OPL_BUILD_REGISTRY_PREFIX: "registry.local-v2.test/opl",
    OPL_BUILD_REGISTRY_TOKEN: randomBytes(32).toString("hex"),
    OPL_BUILD_TOKEN: randomBytes(32).toString("hex"),
    OPL_CAPABILITY_OBJECT_INTERNAL_URL: capabilityObjectURL,
    OPL_CAPABILITY_OBJECT_SIGNING_KEY: randomBytes(32).toString("hex"),
    OPL_CAPABILITY_OBJECT_TOKEN: randomBytes(32).toString("hex"),
    OPL_CAPABILITY_OBJECT_URL: capabilityObjectURL,
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
    // The platform administrators are Gateway subject IDs, the way the
    // published installation supplies them.
    OPL_PLATFORM_ADMIN_SUBJECTS: "1",
    OPL_POSTGRES_ADMIN_PASSWORD: postgresPassword,
    OPL_POSTGRES_HOST: postgresHost,
    OPL_PROXY_PORT: String(hostPort),
    OPL_PUBLIC_URL: `https://127.0.0.1:${hostPort}`,
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
    OPL_SUB2API_BASE_URL: `https://${sub2apiName}:${sub2apiPort}`,
    OPL_SUB2API_USER_EMAIL: "user@composed.fixture",
    OPL_SUB2API_USER_PASSWORD: randomBytes(32).toString("hex"),
    OPL_WORKSPACE_DOMAIN: "workspace.composed.fixture",
    OPL_WORKSPACE_IMAGE: "registry.local-v2.test/workspace@sha256:" + "0".repeat(64),
    OPL_WORKSPACE_REGISTRY_HOST: "registry.local-v2.test",
    OPL_WORKSPACE_REGISTRY_NAMESPACE: "opl",
    // The one container environment carries the registry host for every owner
    // that declares it, so the installation also declares the repository set
    // the Control Plane approves for a Workspace. The published installation
    // leaves it empty because its Control Plane never carries the host.
    OPL_WORKSPACE_REGISTRY_REPOSITORIES: "oplcloud/workspace",
    OPL_WORKSPACE_TOKEN: randomBytes(32).toString("hex"),
    ...peerTokenEnvironment
  };
  for (const owner of databaseOwners) {
    installEnvironment[`OPL_${owner.toUpperCase()}_DATABASE_PASSWORD`] = databasePasswords[owner];
  }

  // The installation's Sub2API is an external service the Cloud backend
  // container does not own, and the published installation names it with an
  // HTTPS URL. This fixture stands for that authority inside the test
  // network: it answers the administrator identity the Control Plane reads
  // when it starts, over TLS with a certificate the installation's own trust
  // root signed. It moves no money, owns no resource and answers nothing
  // else.
  const writeSub2apiFixture = async () => {
    await writeFile(sub2apiScript, [
      'import { createServer } from "node:https";',
      'import { readFileSync } from "node:fs";',
      'const port = Number(process.env.PORT);',
      'const tls = { key: readFileSync(process.env.TLS_KEY_FILE), cert: readFileSync(process.env.TLS_CERT_FILE) };',
      'createServer(tls, (request, response) => {',
      '  let body = "";',
      '  request.on("data", (chunk) => { body += chunk; });',
      '  request.on("end", () => {',
      '    if (request.method !== "POST" || request.url !== "/api/v1/auth/login") {',
      '      response.writeHead(404, { "content-type": "application/json" });',
      '      response.end(JSON.stringify({ code: 404, data: null }));',
      '      return;',
      '    }',
      '    let email = "";',
      '    try { email = String(JSON.parse(body || "{}").email || ""); } catch { email = ""; }',
      '    response.writeHead(200, { "content-type": "application/json" });',
      '    response.end(JSON.stringify({',
      '      code: 0,',
      '      data: { access_token: "sub2api-fixture-token", user: { id: 1, email, status: "active" } }',
      '    }));',
      '  });',
      '}).listen(port, "0.0.0.0");',
      ''
    ].join("\n"));
  };

  // The fixture's certificate. The installation trusts exactly one root for
  // its own TLS (the local v2 CA the published PKI writes), so the fixture
  // authority gets a certificate that root signed and names the fixture by
  // the name the container network resolves.
  const issueSub2apiCertificate = async () => {
    await mkdir(sub2apiTLSRoot, { recursive: true });
    await copyFile(join(pkiDir, "ca.crt"), join(sub2apiTLSRoot, "ca.crt"));
    await copyFile(join(pkiDir, "ca.key"), join(sub2apiTLSRoot, "ca.key"));
    await writeFile(join(sub2apiTLSRoot, "sub2api.ext.cnf"), [
      "[v3_req]",
      `subjectAltName = DNS:${sub2apiName}, IP:127.0.0.1`,
      "extendedKeyUsage = serverAuth",
      "keyUsage = critical, digitalSignature, keyEncipherment",
      "basicConstraints = critical, CA:FALSE",
      ""
    ].join("\n"));
    await command(["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-sha256",
      "-keyout", join(sub2apiTLSRoot, "sub2api.key"),
      "-out", join(sub2apiTLSRoot, "sub2api.csr"),
      "-subj", "/CN=sub2api/O=opl-cloud local v2"]);
    await command(["openssl", "x509", "-req", "-in", join(sub2apiTLSRoot, "sub2api.csr"),
      "-CA", join(sub2apiTLSRoot, "ca.crt"), "-CAkey", join(sub2apiTLSRoot, "ca.key"),
      "-CAcreateserial", "-out", join(sub2apiTLSRoot, "sub2api.crt"),
      "-days", "825", "-sha256",
      "-extfile", join(sub2apiTLSRoot, "sub2api.ext.cnf"), "-extensions", "v3_req"]);
  };

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
  // it is the value the first declared owner carries. The per-owner databases
  // the published installation declares separately are carried under the
  // per-owner name the composition reads them by, because one container
  // environment carries one value per name.
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
    // The published installation declares the approved repository set at
    // installation level and wires it to no owner, so the union of the
    // published per-owner environments does not carry it. One container
    // carries the installation's declared value: the Control Plane reads it
    // because one environment also names the registry host.
    if (environment.OPL_WORKSPACE_REGISTRY_REPOSITORIES === undefined) {
      environment.OPL_WORKSPACE_REGISTRY_REPOSITORIES = installEnvironment.OPL_WORKSPACE_REGISTRY_REPOSITORIES;
    }
    // The published installation gives every owner its own database. One
    // container environment carries one value per name, so each owner's
    // database is carried under the per-owner name the composition reads it
    // by.
    for (const process of contract.processes) {
      if (!process.databaseEnv) continue;
      const dsn = ownerEnvironment.get(process.service)?.[process.databaseEnv];
      if (!dsn) continue;
      environment[ownerAlias(process) + "_DATABASE_URL"] = dsn;
    }
    environment.OPL_GRPC_CERT_DIR = "/pki";
    environment.OPL_RELEASE_SHA = revision;
    return environment;
  };

  // The one Cloud container. It runs the composition entrypoint, which starts
  // every owner the contract declares, and it reaches every peer through the
  // loopback interface: no published service name resolves inside one
  // container.
  const containerArguments = (
    environment: Record<string, string>,
    name: string,
    timeout: string,
    topologyPath: string
  ) => {
    const argumentsList = [
      "run", "-d", "--name", name, "--network", network, "--user", "root"
    ];
    argumentsList.push(
      "-v", `${pkiDir}:/pki:ro`,
      // The Local-Docker provider drives the host daemon and has no daemon of
      // its own, so the composed container carries the daemon's socket the
      // way the published installation gives it to the Fabric owner.
      "-v", "/var/run/docker.sock:/var/run/docker.sock",
      "-v", `${topologyPath}:/etc/opl/opl-cloud-owner-topology.json:ro`,
      "-v", `${packageSchemaPath}:/contracts/agent-plugins-1.0.0-plugin.schema.json:ro`,
      "-v", `${objectRoot}:/var/lib/opl/objects`,
      "-v", `${buildRoot}:/var/lib/opl/build`,
      "-v", `${fabricRoot}:/var/lib/opl/fabric`,
      "-v", `${workspaceRoot}:${workspaceRoot}`,
      "-v", `${secretRoot}:${secretRoot}`
    );
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
  // The address one declared listener is reached at inside the one shared
  // container.
  const loopback = (address: string) => {
    const separator = address.lastIndexOf(":");
    if (separator < 0) return address;
    const host = address.slice(0, separator);
    const port = address.slice(separator + 1);
    if (host === "" || host === "0.0.0.0") return `127.0.0.1:${port}`;
    if (host === "::") return `[::1]:${port}`;
    return address;
  };

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

    await writeSub2apiFixture();
    await issueSub2apiCertificate();
    docker(["network", "create", "--subnet", subnet, network]);
    docker([
      "run", "-d", "--name", sub2apiName, "--network", network, "--ip", sub2apiHost,
      "-e", `PORT=${sub2apiPort}`,
      "-e", "NODE_EXTRA_CA_CERTS=/pki/ca.crt",
      "-e", "TLS_KEY_FILE=/tls/sub2api.key",
      "-e", "TLS_CERT_FILE=/tls/sub2api.crt",
      "-v", `${pkiDir}:/pki:ro`,
      "-v", `${sub2apiTLSRoot}:/tls:ro`,
      "-v", `${sub2apiScript}:/sub2api.mjs:ro`,
      image, "node", "/sub2api.mjs"
    ]);
    waitFor(() => {
      const answer = docker([
        "exec", sub2apiName, "node", "-e",
        'fetch("https://127.0.0.1:" + process.env.PORT + "/api/v1/auth/login", { method: "POST", body: "{}" })' +
        '.then((response) => response.status).then((status) => { process.exit(status === 200 ? 0 : 1); })' +
        '.catch(() => process.exit(1));'
      ], false);
      return answer.status === 0;
    }, "the Sub2API fixture did not answer the administrator login");
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
    // composition entrypoint starts them, so the container runs the owner
    // binary itself and the entrypoint decides the environment it sees.
    for (const directory of [objectRoot, buildRoot, fabricRoot, workspaceRoot]) {
      await mkdir(directory, { recursive: true });
    }
    // The Local-Docker provider keeps its Gateway secrets in a private root
    // and refuses any other mode, the way the published installation declares
    // it.
    await mkdir(secretRoot, { recursive: true, mode: 0o700 });
    await chmod(secretRoot, 0o700);

    // The one Cloud container. It runs the composition entrypoint, which starts
    // every owner the contract declares.
    docker(containerArguments(shared, backendName, "3m", topologyPath));

    const declaredServices = contract.processes.map((process) => process.service);
    // The container holds the evidence while it lives: the composition stops
    // the container as soon as an owner never becomes ready, so the owners'
    // own environment and the container's listeners are read while it runs.
    const captureEvidence = () => {
      const processList = docker([
        "exec", backendName, "sh", "-c",
        "for entry in /proc/[0-9]*/cmdline; do printf '%s ' \"$(basename \"$(dirname \"$entry\")\")\"; tr '\\0' ' ' < \"$entry\"; echo; done"
      ], false).stdout.split("\n").map((line) => line.trim()).filter(Boolean);
      const sockets = docker([
        "exec", backendName, "sh", "-c", "cat /proc/net/tcp /proc/net/tcp6"
      ], false).stdout;
      const socketOwners = docker([
        "exec", backendName, "sh", "-c",
        "for entry in /proc/[0-9]*; do pid=${entry#/proc/}; cmd=$(tr '\\0' ' ' < \"$entry/cmdline\" 2>/dev/null); " +
        "for fd in \"$entry\"/fd/*; do link=$(readlink \"$fd\" 2>/dev/null) || continue; " +
        "inode=$(printf '%s' \"$link\" | sed -n 's/^socket:\\[\\([0-9]*\\)\\]$/\\1/p'); " +
        "[ -n \"$inode\" ] && printf '%s %s %s\\n' \"$inode\" \"$pid\" \"$cmd\"; done; done"
      ], false).stdout;
      const ownerEnvironments = new Map<string, Record<string, string>>();
      for (const process of contract.processes) {
        const entry = processList.find((line) => line.split(" ")[1] === process.binary);
        if (!entry) continue;
        const pid = entry.split(" ")[0];
        const raw = docker([
          "exec", backendName, "sh", "-c", `tr '\\0' '\\n' < /proc/${pid}/environ`
        ], false).stdout;
        const environment: Record<string, string> = {};
        for (const line of raw.split("\n")) {
          const separator = line.indexOf("=");
          if (separator > 0) environment[line.slice(0, separator)] = line.slice(separator + 1);
        }
        if (Object.keys(environment).length === 0) continue;
        ownerEnvironments.set(process.service, environment);
      }
      return { processList, sockets, socketOwners, ownerEnvironments };
    };
    let evidence = {
      processList: [] as string[],
      sockets: "",
      socketOwners: "",
      ownerEnvironments: new Map<string, Record<string, string>>()
    };
    // The composition reports that an owner never became ready. The cause is
    // the owner's own answer, so the wait reads the Fabric owner's readiness
    // surface while the container is still running.
    const fabricProcess = contract.processes.find((process) => process.transportPrincipal === "fabric");
    const fabricListener = fabricProcess ? declaredListener(fabricProcess, fabricProcess.addressEnv) : "";
    let fabricAnswer = "";
    // The readiness criterion the contract declares for an owner. The
    // published owner probe reads the owner's own health surface with the
    // owner's own identity, which is the readback the installer uses, and the
    // contract's own HTTP routes are read directly.
    const criterionFailures = (process: ContractProcess): string[] => {
      const failures: string[] = [];
      const criterion = contract.readinessCriteria.criteria.find((entry) => entry.process === process.service);
      if (!criterion) {
        failures.push(`the contract declares no readiness criterion for ${process.service}`);
        return failures;
      }
      const principal = process.transportPrincipal;
      const grpcCheck = criterion.checks.find((check) => check.transport === "grpc");
      const address = declaredListener(process, (grpcCheck ?? criterion.checks[0]).addressEnv);
      const probe = docker([
        "exec",
        "-e", `OPL_GRPC_CERT_FILE=/pki/${principal}.crt`,
        "-e", `OPL_GRPC_KEY_FILE=/pki/${principal}.key`,
        "-e", "OPL_GRPC_CA_FILE=/pki/ca.crt",
        backendName, "/usr/local/bin/opl-owner-probe",
        "--owner", principal, "--addr", dialable(address),
        "--expected-source-sha", revision, "--timeout", "10s"
      ], false);
      let report: {
        owner?: string; status?: string; health?: { status?: string };
        fabricReadiness?: { serviceReady?: boolean }; productSourceSha?: string; errorCode?: string;
      } = {};
      try {
        report = JSON.parse(probe.stdout || "{}");
      } catch {
        failures.push(`${process.service} probe answer: ${probe.stdout}${probe.stderr}`);
        return failures;
      }
      if (probe.status !== 0) {
        failures.push(`${process.service} did not satisfy its readiness criterion: ${probe.stdout}${probe.stderr}`);
        return failures;
      }
      if (report.status !== "ok") failures.push(`${process.service} probe status`);
      if (report.owner !== principal) failures.push(`${process.service} probe owner`);
      if (report.productSourceSha && report.productSourceSha !== revision) {
        failures.push(`${process.service} runs another revision`);
      }
      if (grpcCheck && report.health?.status !== "SERVING") {
        failures.push(`${process.service} gRPC health`);
      }
      for (const check of criterion.checks) {
        if (check.transport !== "http") continue;
        const url = httpURL(declaredListener(process, check.addressEnv), check.path as string);
        const response = docker([
          "exec", backendName, "curl", "--silent", "--show-error", "--write-out", "\n%{http_code}", url
        ], false);
        const [body, code] = response.stdout.split("\n").slice(-2);
        if (code?.trim() !== "200") {
          failures.push(`${process.service} ${check.path} answered ${code}`);
          continue;
        }
        if (check.projection === "contracts.FabricReadiness") {
          const projection = JSON.parse(body) as { serviceReady?: boolean };
          if (projection.serviceReady !== true) {
            failures.push(`${process.service} ${check.path} readiness projection`);
          }
        }
      }
      return failures;
    };

    let attempts = 0;
    const criterionResults = new Map<string, string[]>();
    waitFor(() => {
      const logs = containerLogs(backendName);
      attempts += 1;
      if (containerRunning(backendName)) {
        const captured = captureEvidence();
        // The composition stops its owners when one never becomes ready, so a
        // read can race with that stop. The richest capture wins, and an owner
        // keeps the environment a previous capture read.
        for (const [service, environment] of captured.ownerEnvironments) {
          evidence.ownerEnvironments.set(service, environment);
        }
        if (captured.processList.length > evidence.processList.length) {
          evidence.processList = captured.processList;
          evidence.sockets = captured.sockets;
          evidence.socketOwners = captured.socketOwners;
        }
        // An owner that became ready satisfies its criterion, and the
        // criterion is read while the container still runs.
        for (const process of contract.processes) {
          if (criterionResults.has(process.service)) continue;
          if (!logs.includes(`owner ${process.service} is ready`)) continue;
          criterionResults.set(process.service, criterionFailures(process));
        }
        if (attempts % 10 === 0 && fabricListener) {
          fabricAnswer = docker([
            "exec", backendName, "curl", "--silent", "--show-error", "--max-time", "5",
            "-H", `Authorization: Bearer ${installEnvironment.OPL_FABRIC_SERVICE_TOKEN}`,
            `http://${loopback(fabricListener)}/fabric/readiness`
          ], false).stdout.trim();
        }
      }
      if (declaredServices.every((service) => logs.includes(`owner ${service} is ready`))) return true;
      // A composition that stops reports the owner that failed, so the wait
      // ends with the report instead of with a timeout.
      return !containerRunning(backendName);
    }, `the composition did not bring every declared owner up:\n${containerLogs(backendName)}\nfabric readiness: ${fabricAnswer}`);

    const logs = containerLogs(backendName);
    const { processList, sockets, socketOwners, ownerEnvironments } = evidence;

    // The owner set the entrypoint started is the contract's declared process
    // set, in the contract's declared order. The composition starts an owner
    // once the owners it depends on are ready, so the owners it started are
    // the declared owners in the declared order.
    const started = [...logs.matchAll(/starting owner (\S+)/g)].map((match) => match[1]);
    assert.ok(started.length > 0, "the entrypoint started no owner");
    assert.deepEqual(started, declaredServices.slice(0, started.length),
      "the entrypoint started a different owner set or order");
    assert.match(logs, new RegExp(`composing ${contract.processes.length} owner processes`));

    // One Cloud container: no second container runs the product image. The
    // Sub2API fixture stands for the installation's external authority and is
    // not a Cloud backend container.
    const productContainers = docker([
      "ps", "-a", "--filter", `ancestor=${image}`, "--format", "{{.Names}}"
    ]).stdout.split("\n").map((line) => line.trim()).filter(Boolean)
      .filter((name) => name !== sub2apiName);
    assert.deepEqual(productContainers, [backendName], "the composition runs more than one Cloud container");

    // The owners are mutually consistent: no two owners share a listen
    // address, and the container listens on nothing the contract does not
    // declare. A listener that the contract does not declare would be a
    // workload that belongs outside the composed unit.
    const listeningSockets: { port: number; inode: string; local: string }[] = [];
    for (const line of sockets.split("\n").slice(1)) {
      const fields = line.trim().split(/\s+/);
      if (fields.length < 10) continue;
      if (fields[3] !== "0A") continue;
      const port = parseInt(fields[1].split(":")[1], 16);
      if (Number.isInteger(port)) listeningSockets.push({ port, inode: fields[9], local: fields[1] });
    }
    const listeningPorts = new Set(listeningSockets.map((entry) => entry.port));
    const socketOwner = new Map<string, string>();
    for (const line of socketOwners.split("\n")) {
      const [inode, pid, ...command] = line.trim().split(/\s+/);
      if (!inode || !pid) continue;
      socketOwner.set(inode, `${pid} ${command.join(" ")}`);
    }
    const declaredPorts = new Map<number, string>();
    for (const process of contract.processes) {
      const environments = [process.addressEnv, process.rpcAddressEnv, ...(process.additionalListeners ?? []).map((listener) => listener.addressEnv)]
        .filter((name): name is string => typeof name === "string");
      for (const name of environments) {
        const address = declaredListener(process, name);
        const port = Number(address.split(":").pop());
        assert.ok(Number.isInteger(port) && port > 0, `${process.service} declares no port for ${name}`);
        const owner = declaredPorts.get(port);
        assert.equal(owner, undefined, `${owner} and ${process.service} share the listen port ${port}`);
        declaredPorts.set(port, process.service);
      }
    }
    for (const process of contract.processes) {
      if (!started.includes(process.service)) continue;
      for (const name of [process.addressEnv, process.rpcAddressEnv, ...(process.additionalListeners ?? []).map((listener) => listener.addressEnv)]
        .filter((name): name is string => typeof name === "string")) {
        const port = Number(declaredListener(process, name).split(":").pop());
        assert.ok(listeningPorts.has(port), `${process.service} does not listen on its declared port ${port}`);
      }
    }
    // Every listener a process of the composed container owns is a listener
    // the contract declares. The network namespace also carries the
    // platform's own resolver, which no process of the container owns.
    const undeclared = listeningSockets.filter((entry) => !declaredPorts.has(entry.port) && socketOwner.has(entry.inode));
    assert.deepEqual(undeclared.map((entry) => entry.port), [],
      `the composed container listens on an address the contract does not declare: ${undeclared.map((entry) => `${entry.port} at ${entry.local} (${socketOwner.get(entry.inode)})`).join(", ")}`);

    // The excluded workloads stay outside the composed unit. The container
    // runs the composition entrypoint and the declared owner processes and
    // nothing else: no privileged build executor, no customer application.
    const allowedBinaries = new Set<string>([
      "/usr/local/bin/opl-cloud-composition",
      ...contract.processes.map((process) => process.binary)
    ]);
    for (const line of processList) {
      const binary = line.split(" ")[1];
      if (binary === "/bin/sh" || binary === "sh") continue;
      assert.ok(allowedBinaries.has(binary),
        `the composed container runs a process the contract does not declare: ${line}`);
    }
    for (const process of contract.processes) {
      if (!started.includes(process.service)) continue;
      assert.ok(
        processList.some((line) => line.split(" ")[1] === process.binary),
        `${process.service} does not run inside the composed container: ${processList.join(" | ")}`
      );
    }
    assert.equal(
      docker(["ps", "-a", "--filter", `ancestor=${image}`, "--format", "{{.Names}}"]).stdout.split("\n")
        .map((line) => line.trim()).filter(Boolean)
        .filter((name) => name !== sub2apiName).length,
      1,
      "a customer application container was deployed beside the composed unit"
    );

    // The projection keeps every owner inside its own contract declaration.
    // The environments are read from the running container, not from the
    // fixture: an owner receives the environment its own declaration allows,
    // and no owner receives another owner's service token, peer token
    // allowlist or database DSN.
    const running = contract.processes.filter((process) => ownerEnvironments.has(process.service));
    assert.deepEqual(running.map((process) => process.service), started,
      "the composed container does not run every owner the composition started");

    // The address an owner binds and the address it reaches a peer at both
    // come from the contract: the listener the owner declares, and the
    // listener the target declares reached through the loopback interface of
    // the one shared container.
    const ownerByPrincipal = new Map(contract.processes.map((process) => [process.transportPrincipal, process]));
    const declaredPeerListener = (target: ContractProcess): string => {
      const name = target.rpcAddressEnv ?? (target.kind === "ownerservice" ? target.addressEnv : undefined);
      return declaredListener(target, name);
    };
    for (const process of running) {
      const environment = ownerEnvironments.get(process.service) as Record<string, string>;
      for (const name of [
        process.addressEnv, process.rpcAddressEnv,
        ...(process.additionalListeners ?? []).map((listener) => listener.addressEnv)
      ]) {
        if (!name) continue;
        const declared = declaredListener(process, name);
        if (!declared) continue;
        assert.equal(environment[name], declared,
          `${process.service} does not bind the listener the contract declares for ${name}; it received ${Object.keys(environment).join(",")}`);
      }
      for (const edge of contract.communication.edges) {
        if (edge.caller !== process.transportPrincipal || !edge.addressEnv) continue;
        const target = ownerByPrincipal.get(edge.target);
        assert.ok(target, `the contract declares no owner ${edge.target}`);
        assert.equal(environment[edge.addressEnv], loopback(declaredPeerListener(target)),
          `${process.service} does not reach ${edge.target} at the listener the contract declares`);
      }
      for (const peer of contract.peerAddresses?.entries ?? []) {
        if (peer.caller !== process.transportPrincipal || !peer.addressEnv) continue;
        const target = ownerByPrincipal.get(peer.target);
        assert.ok(target, `the contract declares no owner ${peer.target}`);
        const listener = declaredListener(target, peer.targetAddressEnv);
        assert.ok(listener, `the contract declares no listener ${peer.targetAddressEnv} for ${peer.target}`);
        assert.equal(environment[peer.addressEnv], `http://${loopback(listener)}`,
          `${process.service} does not reach ${peer.target} at the listener the contract declares`);
      }
    }

    // Every name an owner receives is one its own declaration allows, and no
    // owner receives another owner's service token, peer token allowlist or
    // database DSN.
    for (const process of running) {
      const environment = ownerEnvironments.get(process.service) as Record<string, string>;
      const allowed = new Set<string>();
      for (const value of Object.values(contract.sharedEnv ?? {})) {
        for (const name of Array.isArray(value) ? value : [value]) allowed.add(name);
      }
      for (const name of [
        process.addressEnv, process.addressPortEnv, process.peerTokensEnv, process.rpcAddressEnv,
        process.rpcDatabaseEnv, process.rpcEnabledByEnv, process.databaseEnv,
        ...(process.env ?? []), ...(process.optionalEnv ?? []), ...(process.externalEnv ?? [])
      ]) {
        if (name) allowed.add(name);
      }
      for (const listener of process.additionalListeners ?? []) allowed.add(listener.addressEnv);
      for (const edge of contract.communication.edges) {
        if (edge.caller !== process.transportPrincipal) continue;
        for (const name of [edge.addressEnv, edge.tokenEnv, ...(edge.activationEnvs ?? [])]) {
          if (name) allowed.add(name);
        }
      }
      for (const peer of contract.peerAddresses?.entries ?? []) {
        if (peer.caller !== process.transportPrincipal) continue;
        for (const name of [peer.addressEnv, peer.tokenEnv]) if (name) allowed.add(name);
      }
      allowed.add("OPL_GRPC_CERT_FILE");
      allowed.add("OPL_GRPC_KEY_FILE");
      for (const name of Object.keys(environment)) {
        assert.ok(allowed.has(name),
          `${process.service} received ${name}, which its own contract declaration does not allow`);
      }
      for (const other of contract.processes) {
        if (other.service === process.service) continue;
        const otherToken = shared[`${ownerAlias(other)}_SERVICE_TOKEN`];
        if (otherToken) {
          assert.notEqual(environment["OPL_INTERNAL_SERVICE_TOKEN"], otherToken,
            `${process.service} received the ${other.service} service token`);
        }
        if (other.peerTokensEnv && other.peerTokensEnv !== process.peerTokensEnv) {
          assert.equal(environment[other.peerTokensEnv], undefined,
            `${process.service} received the ${other.service} peer token allowlist`);
        }
        const otherDatabase = `${ownerAlias(other)}_DATABASE_URL`;
        const otherDSN = shared[otherDatabase];
        if (otherDSN) {
          assert.notEqual(environment["DATABASE_URL"], otherDSN,
            `${process.service} received the ${other.service} database DSN`);
        }
        if (otherDatabase !== `${ownerAlias(process)}_DATABASE_URL`) {
          assert.equal(environment[otherDatabase], undefined,
            `${process.service} received the ${other.service} database credential`);
        }
      }
      if (process.databaseEnv) {
        assert.equal(environment[process.databaseEnv], shared[`${ownerAlias(process)}_DATABASE_URL`],
          `${process.service} did not receive its own database`);
      } else {
        assert.equal(environment["DATABASE_URL"], undefined,
          `${process.service} received a database DSN it does not consume`);
      }
    }

    // A failure is a failure. One owner that never becomes ready stops the
    // composition, and the composition names that owner.
    const mutated = { ...shared };
    const mutatedOwner = contract.processes[0];
    const mutatedDatabase = `${ownerAlias(mutatedOwner)}_DATABASE_URL`;
    mutated[mutatedDatabase] = mutated[mutatedDatabase].replace(/\/[^/?]*(\?|$)/, "/opl_absent_database$1");
    docker(containerArguments(mutated, mutatedName, "30s", topologyPath));
    waitFor(() => !containerRunning(mutatedName), "the mutated composition did not stop");
    const mutatedLogs = containerLogs(mutatedName);
    assert.match(mutatedLogs, /composition failed/, "the mutated composition did not report a failure");
    assert.match(mutatedLogs, new RegExp(`owner ${mutatedOwner.service} (did not become ready|exited)`),
      "the mutated composition did not name the owner that never became ready");
    assert.doesNotMatch(mutatedLogs, new RegExp(`owner ${mutatedOwner.service} is ready`),
      "the mutated composition reported the broken owner as ready");

    // Every owner that became ready satisfies the readiness criterion the
    // contract declares for it.
    for (const [service, failures] of criterionResults) {
      assert.deepEqual(failures, [], `${service} did not satisfy its readiness criterion`);
    }

    // Every declared owner became ready, and the container still runs. The
    // Fabric owner's readiness is the local-docker provider's readiness, and
    // the Control Plane's readiness is that projection, so an owner that
    // never becomes ready names the upstream cause.
    const ready = [...logs.matchAll(/owner (\S+) is ready/g)].map((match) => match[1]);
    if (fabricAnswer.includes("local_docker_storage_quota_unavailable")) {
      // The host cannot enforce the project quota the Local-Docker provider
      // requires of its Workspace storage, so the provider refuses readiness
      // and the Control Plane's readiness projection fails with it. The
      // published installation requires that host; the composition must still
      // name the owner that never became ready.
      assert.ok(!ready.includes("control-plane"),
        "the Control Plane owner became ready although its projection failed");
      assert.match(logs, /composition failed: owner control-plane (did not become ready|exited)/,
        `the composition did not name the owner that never became ready\nfabric readiness: ${fabricAnswer}`);
      return;
    }
    assert.deepEqual(ready, declaredServices,
      `the entrypoint did not bring every declared owner to ready\nfabric readiness: ${fabricAnswer}`);
    assert.ok(containerRunning(backendName),
      `the composed container is not running\nfabric readiness: ${fabricAnswer}`);

    // Every owner satisfies the readiness criterion the contract declares for
    // it.
    for (const process of contract.processes) {
      assert.deepEqual(criterionFailures(process), [],
        `${process.service} did not satisfy its readiness criterion`);
    }
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
    docker(["rm", "-f", sub2apiName], false);
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
