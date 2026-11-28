import assert from "node:assert/strict";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import YAML from "yaml";

// The installation contract for the Cloud owner topology.
//
// One product image carries every owner executable, and the instance selects
// which processes to run. Which processes exist, what each one listens on, which
// database it opens and which peers it admits are product facts, not instance
// inventions: an instance that had to derive them from the binaries would be a
// second writer of this contract. `deploy/portable/opl-cloud-owner-topology.json`
// is that declaration, and this test derives every asserted value from the
// owning Go source so the declaration cannot drift from what the processes
// actually read.

const topologyPath = "deploy/portable/opl-cloud-owner-topology.json";
const envExamplePath = "deploy/portable/opl-cloud.env.example";
const composePath = "deploy/portable/compose.cloud.yaml";
const dockerfilePath = "Dockerfile";
const ownerIdentityPath = "packages/contracts/go/owneridentity/owneridentity.go";

type ProcessEntry = {
  service: string;
  kind: string;
  ownerName?: string;
  ownerConstant?: string;
  binary: string;
  addressEnv: string;
  peerTokensEnv?: string;
  rpcAddressEnv?: string;
  defaultRPCAddress?: string;
  rpcDatabaseEnv?: string;
  defaultAddress: string;
  databaseEnv?: string | null;
  compositionDatabaseEnv?: string;
  addressPortEnv?: string;
  env: string[];
  optionalEnv?: string[];
  externalEnv?: string[];
  registers?: string[];
  notes?: string[];
  transportPrincipal?: string;
  additionalListeners?: ListenerEntry[];
};
type ListenerEntry = { addressEnv: string; defaultAddress?: string; valueSemantics?: string };
type ReadinessCheck = {
  transport: string;
  role: string;
  addressEnv: string;
  service?: string;
  method?: string;
  path?: string;
  projection?: string;
};
type ReadinessCriterion = {
  process: string;
  kind: string;
  checks: ReadinessCheck[];
  evidence?: string;
};
type SurfaceEntry = { prefix: string; processes: string[]; browserFacing: boolean; evidence: string };
type PeerAddressEntry = {
  caller: string;
  target: string;
  addressEnv: string;
  tokenEnv: string;
  targetAddressEnv?: string;
  originScheme?: string;
  activation?: string;
  configuredAddressSemantics?: string;
};
type UnitBoundary = { reason: string; reachedThrough: string; boundaryEnv: string };
type UnitEntry = {
  id: string;
  kind: string;
  relatedMember?: string;
  outsideUnit: UnitBoundary;
  note?: string;
  distinction?: string;
  evidence: string[];
};
type DeploymentUnitBlock = {
  rule: string;
  unit: {
    name: string;
    kind: string;
    memberSource: string;
    memberRule: string;
    imageSource: string;
    entrypoint?: string;
    boundaryEffect: string;
  };
  externalDependencies: UnitEntry[];
  exclusions: UnitEntry[];
};
type Topology = {
  sharedEnv: Record<string, string | string[]>;
  derivedEnv: { address: string; peerTokens: string };
  image: { env: string; binaryDirectory: string; defaultCommand: string[] };
  distinctAddressesRequired?: boolean;
  distinctAddressesScope?: string;
  distinctAddressesEvidence?: string;
  processes: ProcessEntry[];
  deploymentUnit?: DeploymentUnitBlock;
  readinessCriteria: { rule: string; criteria: ReadinessCriterion[] };
  consoleBundle: {
    buildArg: string;
    values: string[];
    default: string;
    bakedAt: string;
    imageLabel: string;
    identitySelects: Record<string, string>;
    congruence: string;
    evidence: Record<string, string>;
  };
  browserApiSurface: {
    rule: string;
    surfaces: SurfaceEntry[];
    requirement: string;
  };
  communication?: {
    edges: { caller?: string; target?: string; addressEnv: string; tokenEnv: string; activationEnvs?: string[] }[];
  };
  peerAddresses?: {
    schemaVersion?: number;
    scope?: string;
    binding?: string;
    originSchemes?: string[];
    entries: PeerAddressEntry[];
  };
};

async function loadTopology(): Promise<Topology> {
  return JSON.parse(await readFile(topologyPath, "utf8")) as Topology;
}

async function goFiles(dir: string): Promise<string[]> {
  const found: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) found.push(...(await goFiles(full)));
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) found.push(full);
  }
  return found;
}

// envNamesRead returns every environment name the service's own non-test source
// reads, so the declaration can be compared against the real startup contract.
async function envNamesRead(dir: string, declaredNames?: Set<string>): Promise<Set<string>> {
  const names = new Set<string>();
  for (const file of await goFiles(dir)) {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(/(?:\b\w+\.(?:Getenv|LookupEnv|Lookup|[eE]nv)|\b(?:[gG]etenv|[lL]ookupEnv))\("([A-Z0-9_]+)"\)/g)) {
      names.add(match[1]);
    }
    for (const match of source.matchAll(/(?:durationFromEnv|envInt)(?:With)?\((?:[^,\n]+,\s*)?"([A-Z0-9_]+)"/g)) names.add(match[1]);
    for (const match of source.matchAll(/(?:intFromEnv|nonNegativeIntFromEnv)\(env,\s*"([A-Z0-9_]+)"/g)) names.add(match[1]);
    if (/(?:os\.Getenv|(?:\w+\.)?[gG]etenv|\w+\.env)\(key\)/.test(source)) {
      for (const list of source.matchAll(/required\s*:=\s*\[\]string\{([^}]*)\}/g)) {
        for (const match of list[1].matchAll(/"([A-Z0-9_]+)"/g)) names.add(match[1]);
      }
    }
    // Provider constructors also read environment names through constants.
    for (const match of source.matchAll(/\b(?:const\s+)?(\w+)\s*=\s*"([A-Z0-9_]+)"/g)) {
      if (new RegExp(`(?:\\b\\w+\\.(?:Getenv|LookupEnv|Lookup|[eE]nv)|\\b(?:[gG]etenv|[lL]ookupEnv))\\(${match[1]}\\)`).test(source)) names.add(match[2]);
    }
    if (/(?:Getenv|getenv)\(contracts\.WorkspaceImageReleasesEnv\)/.test(source)) {
      const contract = await readFile("packages/contracts/go/workspace_image_release.go", "utf8");
      const match = contract.match(/const WorkspaceImageReleasesEnv = "([A-Z0-9_]+)"/);
      assert.ok(match, "the Workspace image release environment must retain its contract declaration");
      names.add(match[1]);
    }
    // Some owner entrypoints pass an environment map to an external
    // provisioner and index it directly (`env["NAME"]`) instead of calling
    // Getenv. Add only names already declared by the topology so this probe
    // verifies declared consumers without turning every test-only fixture key
    // into a production contract requirement.
    if (declaredNames) {
      for (const match of source.matchAll(/(?:env|environment)\["([A-Z0-9_]+)"\]/g)) {
        if (declaredNames.has(match[1])) names.add(match[1]);
      }
    }
  }
  return names;
}

// ownerNameByConstant derives the canonical owner identity from the shared
// owneridentity constants instead of restating the mapping here.
async function ownerNameByConstant(): Promise<Map<string, string>> {
  const source = await readFile(ownerIdentityPath, "utf8");
  const mapping = new Map<string, string>();
  for (const match of source.matchAll(/^\t(\w+)\s+Owner = "([a-z_]+)"$/gm)) mapping.set(`Owner${match[1]}`, match[2]);
  assert.ok(mapping.size >= 10, `expected the Cloud owner set, saw ${[...mapping.keys()].join(",")}`);
  return mapping;
}

// loadConfigCalls reads each owner entrypoint's own ownerservice.LoadConfig call:
// the owner identity it declares and the listen address it falls back to.
async function loadConfigCalls(): Promise<Map<string, { ownerConstant: string; defaultAddress: string }>> {
  const calls = new Map<string, { ownerConstant: string; defaultAddress: string }>();
  const scanned = new Set<string>();
  for (const entry of await readdir("services", { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const candidates: string[] = [`services/${entry.name}/module/run.go`];
    try {
      const commands = (await readdir(`services/${entry.name}/cmd`, { withFileTypes: true })).filter((item) => item.isDirectory()).map((item) => item.name);
      for (const command of commands) candidates.push(`services/${entry.name}/cmd/${command}/main.go`);
    } catch {
      // Directory may have no cmd/ subdir.
    }
    for (const candidate of candidates) {
      let source: string;
      try {
        source = await readFile(candidate, "utf8");
      } catch {
        continue;
      }
      const match = source.match(/ownerservice\.LoadConfig\(\s*[\w.]+,\s*ownerservice\.(Owner\w+),\s*"([^"]*)"\s*\)/);
      if (!match || scanned.has(entry.name)) continue;
      scanned.add(entry.name);
      calls.set(entry.name, { ownerConstant: match[1], defaultAddress: match[2] });
    }
  }
  return calls;
}

function declaredEnvNames(processes: ProcessEntry[], shared: Topology["sharedEnv"]): Set<string> {
  const names = new Set<string>();
  for (const values of Object.values(shared)) {
    for (const name of Array.isArray(values) ? values : [values]) names.add(name);
  }
  for (const process of processes) {
    for (const name of [...process.env, ...(process.optionalEnv ?? []), ...(process.externalEnv ?? [])]) names.add(name);
    if (process.addressEnv && process.kind === "ownerservice") names.add(process.addressEnv);
    if (process.peerTokensEnv) names.add(process.peerTokensEnv);
  }
  return names;
}

type ComposeService = { command?: string[]; environment?: Record<string, unknown> };

// installedEnvironments returns the environment the published portable
// installation gives each owner product binary. When the published Compose file
// runs the single composed backend unit (`cloud-backend` running
// `opl-cloud-composition`), every declared owner binary receives the shared
// composition environment.
async function installedEnvironments(): Promise<Map<string, Record<string, string>>> {
  const topology = await loadTopology();
  const compose = YAML.parse(await readFile(composePath, "utf8")) as { services?: Record<string, ComposeService> };
  const environments = new Map<string, Record<string, string>>();
  const composed = compose.services?.["cloud-backend"];
  if (composed?.command?.[0] === topology.deploymentUnit?.unit.entrypoint) {
    const environment: Record<string, string> = {};
    for (const [name, value] of Object.entries(composed.environment ?? {})) {
      if (typeof value === "string") environment[name] = value;
    }
    for (const process of topology.processes) {
      environments.set(process.binary, environment);
    }
    return environments;
  }
  for (const service of Object.values(compose.services ?? {})) {
    const binary = service.command?.[0];
    if (!binary) continue;
    const environment: Record<string, string> = {};
    for (const [name, value] of Object.entries(service.environment ?? {})) {
      if (typeof value === "string") environment[name] = value;
    }
    environments.set(binary, environment);
  }
  return environments;
}

// installedListenAddresses returns the listen addresses the published portable
// installation binds for each owner product binary. A listener binds the
// wildcard interface, so the installation writes its address without a host; an
// address the installation dials names a host and is therefore not a listener
// of that owner. The value is read from the owner's own Compose service, so the
// client address another service declares for the same environment is never
// mistaken for the address that owner binds.
function installedListenAddresses(environments: Map<string, Record<string, string>>): Map<string, Map<string, string>> {
  const listeners = new Map<string, Map<string, string>>();
  for (const [binary, environment] of environments) {
    const addresses = new Map<string, string>();
    for (const [name, value] of Object.entries(environment)) {
      if (/^:[0-9]{2,5}$/.test(value)) addresses.set(name, value);
    }
    if (addresses.size > 0) listeners.set(binary, addresses);
  }
  return listeners;
}

// ownerListenAddresses returns the listen addresses the contract declares for
// one process: its own address, its typed owner address and every additional
// listener it serves. A listener the contract declares without an address is
// absent here, so it cannot take part in the comparison.
function ownerListenAddresses(process: ProcessEntry): Map<string, string> {
  const declared = new Map<string, string>();
  if (process.defaultAddress) declared.set(process.addressEnv, process.defaultAddress);
  if (process.rpcAddressEnv && process.defaultRPCAddress) declared.set(process.rpcAddressEnv, process.defaultRPCAddress);
  for (const listener of process.additionalListeners ?? []) {
    if (listener.defaultAddress) declared.set(listener.addressEnv, listener.defaultAddress);
  }
  return declared;
}

// ownerListenBindings returns every listen address the contract declares for one
// process as a list, so an environment the contract declares twice appears
// twice and collides with itself.
function ownerListenBindings(process: ProcessEntry): { env: string; address: string }[] {
  const bindings: { env: string; address: string }[] = [];
  if (process.defaultAddress) bindings.push({ env: process.addressEnv, address: process.defaultAddress });
  if (process.rpcAddressEnv && process.defaultRPCAddress) bindings.push({ env: process.rpcAddressEnv, address: process.defaultRPCAddress });
  for (const listener of process.additionalListeners ?? []) {
    if (listener.defaultAddress) bindings.push({ env: listener.addressEnv, address: listener.defaultAddress });
  }
  return bindings;
}

// sourceListenAddresses returns the listen addresses each owner's own source
// declares, read from the owner's own source tree. They are the fallbacks the
// owner binds when the installation declares no listen environment.
async function sourceListenAddresses(): Promise<Map<string, Set<string>>> {
  const addresses = new Map<string, Set<string>>();
  for (const [service, directory] of ownerProcessDirectories) {
    const declared = new Set<string>();
    for (const file of await goFiles(directory)) {
      const source = await readFile(file, "utf8");
      for (const match of source.matchAll(/"(:[0-9]{2,5})"/g)) declared.add(match[1]);
    }
    addresses.set(service, declared);
  }
  return addresses;
}

test("every owner entrypoint is declared with the address and peer identity it reads", async () => {
  const topology = await loadTopology();
  const [calls, names, environments] = await Promise.all([loadConfigCalls(), ownerNameByConstant(), installedEnvironments()]);
  const listeners = installedListenAddresses(environments);
  const byService = new Map(topology.processes.map((process) => [process.service, process]));

  assert.ok(calls.size >= 8, `expected the ownerservice owner set, saw ${[...calls.keys()].join(",")}`);
  for (const [service, call] of calls) {
    const ownerName = names.get(call.ownerConstant);
    assert.ok(ownerName, `${service} declares unknown owner constant ${call.ownerConstant}`);
    const process = byService.get(service);
    assert.ok(process, `${service} runs an ownerservice entrypoint and must be declared in ${topologyPath}`);
    assert.equal(process.kind, "ownerservice");
    assert.equal(process.ownerConstant, call.ownerConstant);
    assert.equal(process.ownerName, ownerName);
    assert.equal(process.addressEnv, `OPL_${ownerName.toUpperCase()}_ADDR`);
    assert.equal(process.peerTokensEnv, `OPL_${ownerName.toUpperCase()}_PEER_TOKENS`);
    // The declared listen address is the address the published installation
    // binds. The source's own fallback is the address it binds when the
    // installation declares no listen environment, and the contract names that
    // fallback where the installation overrides it.
    const bound = listeners.get(process.binary)?.get(process.addressEnv);
    assert.equal(process.defaultAddress, bound ?? call.defaultAddress,
      `${service} must declare the listen address the published installation binds`);
    assert.equal(process.databaseEnv, "DATABASE_URL");
    assert.equal(process.binary, `/usr/local/bin/opl-${service}`);
  }
});

test("every process that is not an ownerservice entrypoint is declared with its own listen contract", async () => {
  const topology = await loadTopology();
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  // These three own their own startup path rather than ownerservice.LoadConfig,
  // so the exact literal and default are asserted from the owning file.
  for (const [service, file, defaultAddress] of [
    ["fabric", "services/fabric/module/run.go", ":8082"],
    ["console-bff", "apps/console-bff/module/run.go", ":8190"],
    ["control-plane", "services/control-plane/module/run.go", ":8787"]
  ] as const) {
    const process = byService.get(service);
    assert.ok(process, `${service} must be declared in ${topologyPath}`);
    assert.notEqual(process.kind, "ownerservice");
    assert.equal(process.defaultAddress, defaultAddress);
    const source = await readFile(file, "utf8");
    assert.ok(source.includes(`"${process.addressEnv}"`), `${file} must read ${process.addressEnv}`);
    assert.ok(source.includes(`"${defaultAddress}"`), `${file} must default its listen address to ${defaultAddress}`);
  }
});

test("the declaration accounts for every environment name each process reads", async () => {
  const topology = await loadTopology();
  for (const process of topology.processes) {
    const dir = process.service === "console-bff" ? "apps/console-bff" : `services/${process.service}`;
    const declared = new Set([
      ...process.env,
      ...(process.optionalEnv ?? []),
      ...(process.externalEnv ?? []),
      ...(process.kind === "ownerservice" ? [process.addressEnv, process.peerTokensEnv as string] : []),
      ...Object.values(topology.sharedEnv).flatMap((value) => Array.isArray(value) ? value : [value])
    ]);
    const read = await envNamesRead(dir, declared);
    const undeclared = [...read].filter((name) => !declared.has(name)).sort();
    const stale = [...declared].filter((name) => !read.has(name) && name !== process.addressEnv && name !== process.peerTokensEnv
      && !Object.values(topology.sharedEnv).flatMap((value) => Array.isArray(value) ? value : [value]).includes(name)).sort();
    assert.deepEqual(undeclared, [], `${process.service} reads undeclared environment names`);
    assert.deepEqual(stale, [], `${process.service} declares environment names no source reads`);
  }
});

test("the Fabric process declares the Tencent credentials its provisioner consumes", async () => {
  const topology = await loadTopology();
  const fabric = topology.processes.find((process) => process.service === "fabric");
  assert.ok(fabric, "the Fabric process must be declared");
  const provisioner = (await Promise.all((await goFiles("services/fabric/cmd/opl-tencent-provisioner")).map((file) => readFile(file, "utf8")))).join("\n");
  const required = [
    "TENCENTCLOUD_SECRET_ID",
    "TENCENTCLOUD_SECRET_KEY",
    "TENCENTCLOUD_REGION",
    "OPL_TENCENT_PREDEBIT_IAM_ATTESTATION",
  ];
  for (const name of required) {
    assert.match(provisioner, new RegExp(`(?:env\\[|\\\")${name}(?:\\\"|\\])`),
      `the Tencent provisioner must consume ${name}`);
    assert.ok(fabric.env.includes(name),
      `the Fabric environment allowlist must carry ${name} to the composed child`);
  }
});

test("the declaration covers every owner binary the product image builds", async () => {
  const topology = await loadTopology();
  const dockerfile = await readFile(dockerfilePath, "utf8");
  const goBuild = dockerfile.slice(dockerfile.indexOf("AS go-build"), dockerfile.indexOf("FROM docker:")).replace(/\\\r?\n/g, " ");
  assert.ok(goBuild.includes("-o /out/opl-cloud ./cmd/opl-cloud"), "go-build must compile unified /out/opl-cloud");
  const symlinkLoop = goBuild.match(/for bin in ([^;]+); do\s+ln -s opl-cloud "\/out\/\$bin"/);
  assert.ok(symlinkLoop, "go-build must symlink every declared owner binary to opl-cloud");
  const symlinked = new Set(symlinkLoop[1].trim().split(/\s+/));
  const byBinary = new Map(topology.processes.map((process) => [path.posix.basename(process.binary), process]));
  for (const binary of symlinked) {
    const process = byBinary.get(binary);
    assert.ok(process, `${binary} is shipped in the product image and must be declared in ${topologyPath}`);
    assert.equal(process.binary, `/usr/local/bin/${binary}`);
  }
  for (const process of topology.processes) {
    const binary = path.posix.basename(process.binary);
    assert.ok(symlinked.has(binary), `Dockerfile must ship symlink for ${binary}`);
    assert.ok(dockerfile.includes("COPY --from=go-build /out/ /usr/local/bin/"), `the runtime stage must ship ${binary}`);
  }
});

test("the environment template names every declared installation variable", async () => {
  const topology = await loadTopology();
  const template = await readFile(envExamplePath, "utf8");
  const hostProvided = new Set(Object.values(topology.sharedEnv.hostProvided as string[]));
  const missing = [...declaredEnvNames(topology.processes, topology.sharedEnv)]
    .filter((name) => !hostProvided.has(name))
    .filter((name) => !new RegExp(`^${name}=`, "m").test(template))
    .sort();
  assert.deepEqual(missing, [], `${envExamplePath} must name every declared installation variable`);
});

test("the declaration records that two owners share a default listen address", async () => {
  const topology = await loadTopology();
  const calls = await loadConfigCalls();
  const addressOwners = new Map<string, string[]>();
  for (const [service, call] of calls) {
    const owners = addressOwners.get(call.defaultAddress) ?? [];
    owners.push(service);
    addressOwners.set(call.defaultAddress, owners);
  }
  // The Fabric owner starts its typed owner listener from its coordination
  // package rather than from its command entrypoint, so its own source
  // fallback is read from that package.
  const coordination = await readFile("services/fabric/coordination/process.go", "utf8");
  const coordinationFallback = coordination.match(/ownerservice\.LoadConfig\(ownerEnv, ownerservice\.OwnerFabric, "(:[0-9]+)"\)/);
  assert.ok(coordinationFallback, "the Fabric coordination package must declare its own listen fallback");
  const coordinationOwners = addressOwners.get(coordinationFallback[1]) ?? [];
  coordinationOwners.push("fabric");
  addressOwners.set(coordinationFallback[1], coordinationOwners);
  const collisions = [...addressOwners.entries()].filter(([, services]) => services.length > 1);
  if (collisions.length === 0) {
    assert.equal((topology as unknown as { distinctAddressesRequired?: boolean }).distinctAddressesRequired, undefined,
      "the source no longer shares a default listen address, so the declaration must drop that fact");
    return;
  }
  assert.equal((topology as unknown as { distinctAddressesRequired?: boolean }).distinctAddressesRequired, true);
  for (const [address, services] of collisions) {
    assert.ok((topology as unknown as { distinctAddressesEvidence: string }).distinctAddressesEvidence.includes(address),
      `the shared default address ${address} (${services.join(", ")}) must be named as evidence`);
  }
});

test("the contract declares the listen address the published installation binds for every owner listener", async () => {
  const topology = await loadTopology();
  const compose = YAML.parse(await readFile(composePath, "utf8")) as { services?: Record<string, ComposeService> };
  const composedMode = isComposedBackendCompose(topology, compose.services ?? {});
  const environments = await installedEnvironments();
  const listeners = installedListenAddresses(environments);
  const allDeclaredListenEnvs = new Set<string>();
  for (const process of topology.processes) {
    for (const env of ownerListenAddresses(process).keys()) allDeclaredListenEnvs.add(env);
  }
  for (const process of topology.processes) {
    const bound = listeners.get(process.binary);
    assert.ok(bound, `the published installation must run ${process.binary}`);
    // Every listener the contract declares carries an address, so an owner can
    // never declare a listener the installation binds without saying where that
    // listener listens.
    for (const listener of process.additionalListeners ?? []) {
      assert.ok(listener.defaultAddress,
        `${process.service} must declare the listen address of ${listener.addressEnv}`);
    }
    const declared = ownerListenAddresses(process);
    // Every listener the installation binds must be declared by this owner (or by
    // one of the composed owners when running inside the shared cloud-backend).
    for (const [env] of bound) {
      if (composedMode) {
        assert.ok(allDeclaredListenEnvs.has(env),
          `cloud-backend binds ${env}, which no declared process in ${topologyPath} declares`);
      } else {
        assert.ok(declared.has(env),
          `${process.service} does not declare the listen address ${env} the published installation binds`);
      }
    }
    // Every declared listener is the address the installation binds for it.
    for (const [env, address] of declared) {
      assert.equal(address, bound.get(env),
        `${process.service} declares ${env} as ${address}; the published installation binds ${bound.get(env)}`);
    }
    if (process.addressPortEnv) {
      const port = environments.get(process.binary)?.[process.addressPortEnv];
      assert.ok(port, `the published installation must declare ${process.addressPortEnv} for ${process.service}`);
      assert.equal(`:${port}`, declared.get(process.addressEnv),
        `${process.service} must declare the same port in ${process.addressPortEnv} and ${process.addressEnv}`);
    }
  }
});

test("no two owners declare the same listen address", async () => {
  const topology = await loadTopology();
  assert.equal(topology.distinctAddressesRequired, true,
    "the contract must require the declared listen addresses to be distinct");
  assert.equal(topology.distinctAddressesScope, "pod_network_namespace",
    "the scope must be the one the Cloud backend deployment unit shares");
  const owners = new Map<string, string>();
  for (const process of topology.processes) {
    for (const { env, address } of ownerListenBindings(process)) {
      const other = owners.get(address);
      assert.equal(other, undefined,
        `${process.service} (${env}) and ${other} would both listen on ${address} in one ${topology.distinctAddressesScope}`);
      owners.set(address, `${process.service} (${env})`);
    }
  }
});

test("every listen address an owner's own source declares is declared by the contract or named as the fallback the installation overrides", async () => {
  const topology = await loadTopology();
  const evidence = topology.distinctAddressesEvidence ?? "";
  const sources = await sourceListenAddresses();
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  for (const process of topology.processes) {
    const declared = new Set(ownerListenAddresses(process).values());
    for (const address of sources.get(process.service) ?? []) {
      if (declared.has(address)) continue;
      assert.ok(evidence.includes(address),
        `${process.service} binds ${address} when the installation declares no listen environment; the contract must name it`);
    }
  }
  // The owner entrypoints' own listen fallback is the address they bind when
  // the installation declares no listen environment. It is the declared
  // address, or the contract names it as the fallback the installation
  // overrides.
  for (const [service, call] of await loadConfigCalls()) {
    const process = byService.get(service);
    assert.ok(process, `${service} must be declared in ${topologyPath}`);
    if (ownerListenAddresses(process).get(process.addressEnv) === call.defaultAddress) continue;
    assert.ok(evidence.includes(call.defaultAddress),
      `${service} falls back to ${call.defaultAddress} when the installation declares no listen environment; the contract must name it`);
  }
});



// sourceFiles lists files with any of the given extensions, recursively.
async function sourceFiles(dir: string, extensions: string[]): Promise<string[]> {
  const found: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) found.push(...(await sourceFiles(full, extensions)));
    else if (extensions.some((extension) => entry.name.endsWith(extension))) found.push(full);
  }
  return found;
}

// surfacePrefix reduces an HTTP path to the surface routing distinguishes: the
// first segment, plus a version segment when the second segment is one, so
// /api/workspaces/... shares /api/ while /api/v2/... does not.
function surfacePrefix(path: string): string {
  const segments = path.split("/").filter(Boolean);
  if (segments.length === 0) return "/";
  const version = segments[1] && /^v\d+$/.test(segments[1]) ? `${segments[1]}/` : "";
  return `/${segments[0]}/${version}`;
}

// ownerProcesses is the process set whose HTTP surfaces the declaration covers.
const ownerProcessDirectories: Array<[string, string]> = [
  ["control-plane", "services/control-plane"],
  ["console-bff", "apps/console-bff"],
  ["fabric", "services/fabric"],
  ["ledger", "services/ledger"],
  ["capability", "services/capability"],
  ["build", "services/build"],
  ["runtime-control", "services/runtime-control"],
  ["workspace", "services/workspace"],
  ["serve", "services/serve"],
  ["resource-catalog", "services/resource-catalog"],
  ["gateway-integration", "services/gateway-integration"]
];

// The AST helper reads literal arguments to actual Handle/HandleFunc calls.
// Computed patterns are outside this source inventory; owner runtime tests
// remain necessary. Quoted response metadata cannot claim another HTTP owner.
function registeredLiteralPaths(files: string[]): string[] {
  return JSON.parse(execFileSync("go", ["run", "services/control-plane/internal/server/testdata/http-surface-scan.go"], {
    input: JSON.stringify(files), encoding: "utf8", maxBuffer: 4 * 1024 * 1024,
  })) as string[];
}

async function registeredHttpSurfaces(): Promise<Map<string, Set<string>>> {
  const byPrefix = new Map<string, Set<string>>();
  for (const [service, directory] of ownerProcessDirectories) {
    for (const registered of registeredLiteralPaths(await goFiles(directory))) {
      const prefix = surfacePrefix(registered);
      const owners = byPrefix.get(prefix) ?? new Set<string>();
      owners.add(service);
      byPrefix.set(prefix, owners);
    }
  }
  return byPrefix;
}

test("route inventory distinguishes real registrations from redirects and comments", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "opl-http-surface-"));
  try {
    const file = path.join(directory, "routes.go");
    await writeFile(file, `package fixture
func routes(mux *http.ServeMux) {
 // mux.HandleFunc("POST /api/v99/comment", handler)
 mux.HandleFunc("POST /api/workspace-launches", handler)
 mux.Handle("GET /api/v2/workspaces", handler)
 writeJSON(map[string]any{"next": []string{"POST /api/v3/quotes", "POST /api/v3/workspaces"}})
}
`);
    assert.deepEqual(registeredLiteralPaths([file]), ["/api/v2/workspaces", "/api/workspace-launches"]);
    await writeFile(file, "package fixture\nfunc broken(");
    assert.throws(() => registeredLiteralPaths([file]), /expected/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

// consoleApiPrefixes reads every API path literal the Console source itself
// names, at the same surface granularity.
async function consoleApiPrefixes(): Promise<Set<string>> {
  const prefixes = new Set<string>();
  for (const file of await sourceFiles("apps/console-ui/src", [".ts", ".tsx"])) {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(/[`"'](\/api\/[^`"'\s]*)[`"']/g)) {
      if (match[1].includes("${")) continue;
      prefixes.add(surfacePrefix(match[1]));
    }
  }
  assert.ok(prefixes.size >= 2, `the Console must name its API surfaces, saw ${[...prefixes].join(",")}`);
  return prefixes;
}

test("the declaration matches literal HTTP registrations in the Cloud processes", async () => {
  const topology = await loadTopology();
  const registered = await registeredHttpSurfaces();
  assert.ok(registered.size >= 5, `expected the owner HTTP surfaces, saw ${[...registered.keys()].join(",")}`);
  const declared = new Map(topology.browserApiSurface.surfaces.map((surface) => [surface.prefix, surface]));
  for (const [prefix, owners] of [...registered].sort(([a], [b]) => (a < b ? -1 : 1))) {
    const surface = declared.get(prefix);
    assert.ok(surface, `${prefix} is registered by ${[...owners].join(",")} and must be declared in the browser API surface`);
    assert.deepEqual([...owners].sort(), [...surface.processes].sort(),
      `${prefix} is declared for ${surface.processes.join(",")} but registered by ${[...owners].join(",")}`);
    assert.ok(surface.browserFacing === (prefix.startsWith("/api/")),
      `${prefix} must be marked browser-facing exactly when it is an API surface the Console calls`);
  }
  for (const surface of topology.browserApiSurface.surfaces) {
    assert.ok(registered.has(surface.prefix), `${surface.prefix} is declared but no process registers it`);
    assert.ok(surface.evidence.length > 0, `${surface.prefix} must carry the evidence that registers it`);
  }
});

test("every API path the Console calls is served by a declared browser-facing process", async () => {
  const topology = await loadTopology();
  const browserFacing = topology.browserApiSurface.surfaces.filter((surface) => surface.browserFacing);
  assert.ok(browserFacing.length >= 2, "the declaration must name the browser-facing surfaces");
  for (const prefix of [...(await consoleApiPrefixes())].sort()) {
    const match = browserFacing.filter((surface) => prefix.startsWith(surface.prefix)).sort((a, b) => b.prefix.length - a.prefix.length)[0];
    assert.ok(match, `the Console calls ${prefix} but no declared browser-facing process serves it`);
  }
  // The Console is served from one origin and calls more than one surface, so each
  // identity must declare every surface it requires, and each required surface
  // must be one a conforming installation routes.
  for (const value of topology.consoleBundle.values) {
    const selection = topology.consoleBundle.identitySelects[value];
    assert.ok(selection && Array.isArray(selection.requiresApiSurface) && selection.requiresApiSurface.length > 0,
      `the ${value} identity must declare the API surface it requires`);
    for (const prefix of selection.requiresApiSurface) {
      assert.ok(browserFacing.some((surface) => surface.prefix === prefix),
        `the ${value} identity requires ${prefix} and the declaration must route it`);
    }
  }
  const cloud = topology.consoleBundle.identitySelects.cloud;
  for (const prefix of await consoleApiPrefixes()) {
    assert.ok(cloud.requiresApiSurface.some((required) => prefix.startsWith(required)),
      `the Console calls ${prefix} and the cloud identity must declare it reachable`);
  }
});

test("the Console identity is an image fact the declaration and the image agree on", async () => {
  const topology = await loadTopology();
  const consoleBundle = topology.consoleBundle;
  const dockerfile = await readFile(dockerfilePath, "utf8");
  const declarations = [...dockerfile.matchAll(new RegExp(`ARG ${consoleBundle.buildArg}=(\\S+)`, "g"))].map((match) => match[1]);
  assert.ok(declarations.length >= 2, `${consoleBundle.buildArg} must be declared in both the build and runtime stages`);
  assert.equal(new Set(declarations).size, 1, `the ${consoleBundle.buildArg} defaults disagree: ${declarations.join(",")}`);
  assert.equal(declarations[0], consoleBundle.default);
  assert.ok(dockerfile.includes(`LABEL ${consoleBundle.imageLabel}=$${consoleBundle.buildArg}`),
    `the image must label itself with ${consoleBundle.imageLabel}`);
  const identitySource = await readFile("apps/console-ui/src/app/console-identity.ts", "utf8");
  assert.ok(identitySource.includes(consoleBundle.buildArg), "the Console must read the declared identity argument");
  for (const value of consoleBundle.values) {
    assert.ok(Object.prototype.hasOwnProperty.call(consoleBundle.identitySelects, value),
      `the declaration must state what the ${value} identity selects`);
  }
});


// An owner's peer allowlist is an admission list, not documentation. It decides
// what the owner answers at run time: an owner whose allowlist lacks a real
// caller refuses that caller with PermissionDenied, and an allowlist that names
// a peer nothing dials is permission the owner never granted. The previous
// assertions above only checked that the variable *name* appears in the
// template, so every allowlist could have been emptied and the contract still
// passed. The derivations below read the Go source instead, so the template's
// peer keys are compared against the callers the processes actually construct.

// transportPrincipalByEnvPrefix maps an environment name that carries an owner
// address to the transport identity that owner answers with. GATEWAY is the one
// owner whose deployment unit is another owner's principal, so the remap is read
// from the wire convention rather than restated here.
async function transportPrincipalByEnvPrefix(names: Map<string, string>): Promise<{ byPrefix: Map<string, string>; consoleBFF: string }> {
  const source = await readFile("packages/contracts/go/owneridentity/transport.go", "utf8");
  const consoleBFF = source.match(/^const ConsoleBFF Service = "([a-z_]+)"$/m);
  assert.ok(consoleBFF, "the wire convention must declare the Console BFF transport identity");
  const remap = new Map<string, string>();
  for (const match of source.matchAll(/if owner == (\w+) \{\s*return (\w+)\.Service\(\)/g)) {
    remap.set(names.get(`Owner${match[1]}`) as string, names.get(`Owner${match[2]}`) as string);
  }
  assert.ok(remap.size > 0, "the wire convention must still declare its deployment-unit remap");
  const byPrefix = new Map<string, string>();
  for (const [constant, owner] of names) {
    const principal = remap.get(owner) ?? owner;
    byPrefix.set(owner.toUpperCase(), principal);
    byPrefix.set(constant, principal);
  }
  return { byPrefix, consoleBFF: consoleBFF[1] };
}

// processPrincipal names the transport identity one process presents on every
// outbound owner call: the owner identity it loads its own configuration with,
// or the Console BFF identity, which is a transport principal and not an owner.
// The retained Control Plane is also a transport principal, not a target owner.
async function processPrincipal(names: Map<string, string>, consoleBFF: string): Promise<Map<string, string>> {
  const principals = new Map<string, string>();
  const transport = await readFile("packages/contracts/go/owneridentity/transport.go", "utf8");
  const controlPlane = transport.match(/^const ControlPlane Service = "([a-z_]+)"$/m);
  assert.ok(controlPlane, "the retained reader needs its actual transport identity");
  for (const [service, directory] of ownerProcessDirectories) {
    for (const file of await goFiles(directory)) {
      const source = await readFile(file, "utf8");
      for (const match of source.matchAll(/ownerservice\.LoadConfig\(\s*[\w.]+,\s*ownerservice\.(Owner\w+)/g)) {
        principals.set(service, names.get(match[1]) as string);
      }
      if (/DialOptions\(\s*owneridentity\.ConsoleBFF\s*,/.test(source)) principals.set(service, consoleBFF);
      if (/DialOptions\(\s*owneridentity\.ControlPlane\s*,/.test(source)) principals.set(service, controlPlane[1]);
    }
  }
  assert.ok(principals.get("gateway-integration") === "tenant",
    "the gateway-integration process must be recognised as the tenant deployment unit");
  return principals;
}

// runsSharedAuthorizer reports whether a process opens the one owner call that
// has no environment name of its own: the shared authorizer every owner uses to
// reach user authorization over the CloudIdentity boundary.
async function runsSharedAuthorizer(directory: string): Promise<boolean> {
  for (const file of await goFiles(directory)) {
    const source = await readFile(file, "utf8");
    if (/ownerservice\.(?:Start|StartWithDatabase|AuthorizerFromConfig)\(/.test(source)) return true;
  }
  return false;
}

// dialTargets reads every owner a process's own source dials: the owner address
// names it names, plus the owners it names only at run time.
function dialTargetsFromSource(source: string, byPrefix: Map<string, string>): Set<string> {
  const targets = new Set<string>();
    // An owner address name is OPL_<OWNER>_ADDR for an owner process and
    // OPL_<OWNER>_URL for a caller that reaches an owner it does not own. A
    // process that names one routes to that owner, however it spells the lookup:
    // directly, or through a table of address names it dials in turn. Every
    // other environment name is not a route to an owner and is not claimed here.
    for (const match of source.matchAll(/"OPL_([A-Z_0-9]+)_(?:ADDR|URL)"/g)) {
      const owner = byPrefix.get(match[1]) ?? byPrefix.get(match[1].replace(/_COORDINATION$/, ""));
      if (owner) targets.add(owner);
    }
    // Typed targets remain authoritative when a shared address environment name
    // (for example CloudIdentity) does not spell the target owner's name.
    for (const match of source.matchAll(/DialOptions\([^,]+,\s*owneridentity\.(\w+)\.Service\(\)/g)) {
      const target = byPrefix.get(`Owner${match[1]}`);
      if (target) targets.add(target);
    }
    // A process that composes the owner environment name from the owner it is
    // looping over names no address literal to find, so the owner set it loops
    // over is the only place that call is written down.
    if (/(?:os\.Getenv|env\.Env|getenv)\("OPL_"\s*\+/.test(source)) {
      for (const helper of source.matchAll(/func\s+(\w+)\([^\n]*target owneridentity\.Owner/g)) {
        for (const call of source.matchAll(new RegExp(`\\b${helper[1]}\\([^\\n]*?owneridentity\\.(\\w+)`, "g"))) {
          const target = byPrefix.get(`Owner${call[1]}`);
          if (target) targets.add(target);
        }
      }
    }
    if (/getenv\(prefix ?\+\s*"_(?:ADDR|TOKEN)"\)/.test(source)) {
    for (const list of source.matchAll(/\[\]owneridentity\.Owner\{([^}]*)\}/g)) {
      for (const owner of list[1].matchAll(/owneridentity\.(\w+)/g)) {
        const target = byPrefix.get(`Owner${owner[1]}`);
        if (target) targets.add(target);
      }
    }
  }
  return targets;
}

async function dialTargets(directory: string, byPrefix: Map<string, string>): Promise<Set<string>> {
  const targets = new Set<string>();
  for (const file of await goFiles(directory)) {
    for (const target of dialTargetsFromSource(await readFile(file, "utf8"), byPrefix)) targets.add(target);
  }
  return targets;
}

test("dynamic peer helpers retain their actual owner arguments without address literals", () => {
  const source = `
    peer, err := dialPeer(config, tls, owneridentity.Gateway, config.CloudIdentityAddr)
    func dialPeer(config ownerservice.Config, tls owneridentity.TLSConfig, target owneridentity.Owner, address string) {
      token := os.Getenv("OPL_" + strings.ToUpper(target.String()) + "_TOKEN")
    }
  `;
  assert.deepEqual([...dialTargetsFromSource(source, new Map([["OwnerGateway", "tenant"]]))], ["tenant"]);
});

test("the Fabric HTTP process also declares its real owner RPC admission surface", async () => {
  const topology = await loadTopology();
  const fabric = topology.processes.find(process => process.service === "fabric")!;
  const source = await readFile("services/fabric/coordination/process.go", "utf8");
  assert.match(source, /ownerservice.LoadConfig\(ownerEnv, ownerservice.OwnerFabric, ":8187"\)/);
  assert.match(source, /key == "DATABASE_URL"[\s\S]*getenv\("OPL_FABRIC_DATABASE_URL"\)/);
  assert.equal(fabric.ownerName, "fabric");
  assert.equal(fabric.ownerConstant, "OwnerFabric");
  assert.equal(fabric.peerTokensEnv, "OPL_FABRIC_PEER_TOKENS");
  assert.equal(fabric.rpcAddressEnv, "OPL_FABRIC_ADDR");
  // The coordination listener is the address the published installation binds
  // for it. The source's own fallback is the address it binds when the
  // installation declares no listen environment.
  const listeners = installedListenAddresses(await installedEnvironments());
  assert.equal(fabric.defaultRPCAddress, listeners.get(fabric.binary)?.get(fabric.rpcAddressEnv!),
    "the contract must declare the coordination address the published installation binds");
  assert.equal(fabric.rpcDatabaseEnv, "OPL_FABRIC_DATABASE_URL");
  assert.equal(fabric.compositionDatabaseEnv, "DATABASE_URL");
});

// peerCallers derives the callers every owner must admit from the source that
// opens the connections.
async function peerCallers(): Promise<Map<string, Set<string>>> {
  const names = await ownerNameByConstant();
  const { byPrefix, consoleBFF } = await transportPrincipalByEnvPrefix(names);
  const principals = await processPrincipal(names, consoleBFF);
  // Every owner reaches user authorization over the CloudIdentity gRPC boundary
  // except the owner that answers it, so the tenant process is not its own
  // caller. The startup path states that exception; if it stops doing so the
  // allowlist below would be quietly one key short.
  const bootstrap = await readFile("services/internal/ownerservice/bootstrap.go", "utf8");
  assert.ok(bootstrap.includes("config.Owner != OwnerTenant"),
    "the startup path must still exempt the tenant owner from calling the CloudIdentity boundary");

  const callers = new Map<string, Set<string>>();
  for (const [service, directory] of ownerProcessDirectories) {
    const principal = principals.get(service);
    if (!principal) continue;
    const targets = await dialTargets(directory, byPrefix);
    if (principal !== "tenant" && await runsSharedAuthorizer(directory)) targets.add("tenant");
    targets.delete(principal);
    for (const target of targets) {
      const admitted = callers.get(target) ?? new Set<string>();
      admitted.add(principal);
      callers.set(target, admitted);
    }
  }
  return callers;
}

test("each peer allowlist names exactly the callers that reach that owner", async () => {
  const topology = await loadTopology();
  const template = await readFile(envExamplePath, "utf8");
  const declared = new Map<string, string>();
  for (const process of topology.processes) {
    if (!process.peerTokensEnv || !process.ownerName) continue;
    declared.set(process.peerTokensEnv, process.ownerName);
  }
  assert.ok(declared.size >= 8, `expected the owner peer allowlists, saw ${[...declared.keys()].join(",")}`);
  const written = new Map<string, string>();
  for (const match of template.matchAll(/^(OPL_[A-Z0-9_]*PEER_TOKENS)=(\S*)$/gm)) written.set(match[1], match[2]);
  assert.deepEqual([...written.keys()].sort(), [...declared.keys()].sort(),
    `${envExamplePath} must set exactly the peer allowlists the installation contract declares`);

  const callers = await peerCallers();
  for (const [env, owner] of [...declared].sort()) {
    const peers = Object.keys(JSON.parse(written.get(env) as string)).sort();
    assert.deepEqual(peers, [...(callers.get(owner) ?? [])].sort(),
      `${env} must admit exactly the callers that dial ${owner}, and no peer nothing dials`);
  }
});

test("every environment name the owner topology declares resolves in the installation", async () => {
  const topology = await loadTopology();
  const template = await readFile(envExamplePath, "utf8");
  const declared = new Set<string>();
  for (const line of template.split("\n")) {
    const match = /^([A-Za-z_][A-Za-z0-9_]*)=/.exec(line);
    if (match) declared.add(match[1]);
  }

  // The names the contract says an installation provides.
  const required = new Set<string>();
  const add = (name: string | undefined) => { if (name) required.add(name); };
  for (const name of topology.sharedEnv?.tls || []) add(name);
  add(topology.sharedEnv?.database);
  for (const name of topology.sharedEnv?.cloudIdentity || []) add(name);
  for (const process of topology.processes || []) {
    for (const name of process.env || []) add(name);
    add(process.addressEnv);
    add(process.peerTokensEnv);
    add(process.databaseEnv);
  }
  for (const edge of topology.communication?.edges || []) {
    add(edge.addressEnv);
    add(edge.tokenEnv);
  }

  // The host provides these, and the build takes its argument; neither is an
  // installation fact, so neither appears in the environment template.
  const hostProvided = new Set<string>(topology.sharedEnv?.hostProvided || []);
  const buildArgs = new Set<string>([topology.consoleBundle?.buildArg].filter(Boolean) as string[]);

  // The Compose files derive a container's environment from another fact.
  // The derived set is a closure: a derived name resolves only when the fact
  // it derives from resolves.
  const derivedPairs: [string, string][] = [];
  const files = [
    "compose.yaml",
    ...(await readdir("deploy/portable")).filter((name) => name.endsWith(".yaml")).map((name) => `deploy/portable/${name}`)
  ];
  for (const file of files) {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(/^\s*([A-Za-z_][A-Za-z0-9_]*):\s*\$\{([A-Za-z_][A-Za-z0-9_]*)/gm)) {
      derivedPairs.push([match[1], match[2]]);
    }
  }
  const derived = new Set<string>();
  let grew = true;
  while (grew) {
    grew = false;
    for (const [key, source] of derivedPairs) {
      if (derived.has(key)) continue;
      if (declared.has(source) || derived.has(source)) {
        derived.add(key);
        grew = true;
      }
    }
  }

  const unresolved = [...required]
    .filter((name) => !declared.has(name) && !hostProvided.has(name) && !buildArgs.has(name) && !derived.has(name))
    .sort();
  assert.deepEqual(unresolved, [], "every environment name the owner topology declares must resolve in the installation");
});


// The readiness criterion of an owner is a product fact: which check answers
// "is this process ready", on which transport, and on which of the addresses
// the process already declares. It used to live hardcoded in the owner probe
// and in the installer's address list, which made those two a second writer of
// the topology. The owner topology contract is that single writer now, and the
// assertions below read the owning Go source so the declaration cannot drift
// from what the processes actually serve.

// declaredListenAddresses returns the address environment names the contract
// declares for one process, split by the transport each one carries: the
// owner's gRPC address and the HTTP addresses the process listens on. The
// split is read from the declaration itself (kind, rpcAddressEnv, addressEnv
// and the additional listeners) rather than restated per process.
function declaredListenAddresses(process: ProcessEntry): { grpc: Set<string>; http: Set<string> } {
  const grpc = new Set<string>();
  const http = new Set<string>();
  if (process.rpcAddressEnv) grpc.add(process.rpcAddressEnv);
  if (process.kind === "ownerservice") {
    if (process.addressEnv) grpc.add(process.addressEnv);
  } else if (process.addressEnv) {
    http.add(process.addressEnv);
  }
  for (const listener of process.additionalListeners ?? []) http.add(listener.addressEnv);
  return { grpc, http };
}

// declaredDefaultAddresses returns every default listen address the contract
// declares for one address environment name of a process. A criterion may only
// name an environment the contract declares, so a criterion never introduces an
// address of its own.
function declaredDefaultAddresses(process: ProcessEntry, addressEnv: string): string[] {
  const defaults: string[] = [];
  if (process.addressEnv === addressEnv && process.defaultAddress) defaults.push(process.defaultAddress);
  if (process.rpcAddressEnv === addressEnv && process.defaultRPCAddress) defaults.push(process.defaultRPCAddress);
  for (const listener of process.additionalListeners ?? []) {
    if (listener.addressEnv === addressEnv && listener.defaultAddress) defaults.push(listener.defaultAddress);
  }
  return defaults;
}

// readinessGaps reports the declared processes that carry no readiness
// criterion and the criteria that name no declared process.
function readinessGaps(processes: ProcessEntry[], criteria: ReadinessCriterion[]): { missing: string[]; unknown: string[] } {
  const declared = new Set(processes.map((process) => process.service));
  const named = new Set(criteria.map((criterion) => criterion.process));
  return {
    missing: [...declared].filter((service) => !named.has(service)).sort(),
    unknown: [...named].filter((service) => !declared.has(service)).sort()
  };
}

// servesOwnerHealthService reports whether the process's own source starts the
// shared owner server, which is the only server that registers the gRPC health
// service.
async function servesOwnerHealthService(directory: string): Promise<boolean> {
  for (const file of await goFiles(directory)) {
    const source = await readFile(file, "utf8");
    if (/ownerservice\.(?:Start|StartWithDatabase|NewServer)\(/.test(source)) return true;
  }
  return false;
}

// registersRoute reports whether the process's own source registers one HTTP
// route, in the literal form Go's ServeMux uses.
async function registersRoute(directory: string, method: string, routePath: string): Promise<boolean> {
  const literal = `"${method} ${routePath}"`;
  for (const file of await goFiles(directory)) {
    const source = await readFile(file, "utf8");
    if (source.includes(literal)) return true;
  }
  return false;
}

// ownerProcessDirectory is the source tree that owns one declared process.
function ownerProcessDirectory(service: string): string {
  const entry = ownerProcessDirectories.find(([name]) => name === service);
  assert.ok(entry, `${service} must be a known owner process directory`);
  return entry![1];
}

test("every declared process declares exactly one readiness criterion, and every criterion names a declared process", async () => {
  const topology = await loadTopology();
  const declared = topology.readinessCriteria?.criteria;
  assert.ok(Array.isArray(declared), `${topologyPath} must declare the readiness criteria`);
  assert.ok(topology.readinessCriteria.rule.length > 0, "the readiness declaration must state the rule it follows");
  const named = declared!.map((criterion) => criterion.process);
  assert.deepEqual(named.filter((service, index) => named.indexOf(service) !== index), [],
    "no process may carry two readiness criteria");
  const gaps = readinessGaps(topology.processes, declared!);
  assert.deepEqual(gaps.missing, [], "every declared process must carry a readiness criterion");
  assert.deepEqual(gaps.unknown, [], "every readiness criterion must name a declared process");
});

test("every readiness check names the addresses this contract already declares for that process", async () => {
  const topology = await loadTopology();
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  for (const criterion of topology.readinessCriteria.criteria) {
    const process = byService.get(criterion.process);
    assert.ok(process, `${criterion.process} must be a declared process`);
    assert.ok(criterion.kind.length > 0, `${criterion.process} must declare a readiness criterion kind`);
    assert.ok(criterion.evidence && criterion.evidence.length > 0,
      `${criterion.process} must carry the evidence that establishes its criterion`);
    assert.ok(criterion.checks.length > 0, `${criterion.process} must declare at least one readiness check`);
    assert.ok(criterion.checks.some((check) => check.role === "readiness"),
      `${criterion.process} must declare at least one readiness check`);
    const addresses = declaredListenAddresses(process!);
    for (const check of criterion.checks) {
      assert.ok(check.transport === "grpc" || check.transport === "http",
        `${criterion.process} check must name the transport it uses`);
      assert.ok(check.role === "readiness" || check.role === "liveness",
        `${criterion.process} check must name whether it answers the readiness or the liveness question`);
      const allowed = check.transport === "grpc" ? addresses.grpc : addresses.http;
      assert.ok(allowed.has(check.addressEnv),
        `${criterion.process} ${check.transport} check names ${check.addressEnv}, which the contract does not declare as a ${check.transport} address of that process`);
      const defaults = declaredDefaultAddresses(process!, check.addressEnv);
      assert.equal(defaults.length, 1,
        `${criterion.process} must declare exactly one default listen address for ${check.addressEnv}`);
      assert.match(defaults[0], /^:[0-9]{2,5}$/,
        `${criterion.process} default listen address for ${check.addressEnv} must be a listen port`);
      if (check.transport === "grpc") {
        assert.equal(check.service, "grpc.health.v1.Health");
        assert.equal(check.method, "Check");
        assert.equal(check.path, undefined);
      } else {
        assert.equal(check.method, "GET");
        assert.ok(check.path && check.path.startsWith("/"),
          `${criterion.process} HTTP check must name the route it calls`);
        assert.equal(check.service, undefined);
      }
    }
  }
});

test("the declared readiness kind matches the checks the criterion actually makes", async () => {
  const topology = await loadTopology();
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  for (const criterion of topology.readinessCriteria.criteria) {
    const process = byService.get(criterion.process);
    assert.ok(process, `${criterion.process} must be a declared process`);
    const transports = new Set(criterion.checks.map((check) => check.transport));
    const projections = criterion.checks.filter((check) => check.projection !== undefined);
    const isOwner = process!.ownerName !== undefined;
    let expected: string;
    if (transports.has("grpc")) {
      expected = transports.has("http") ? "ownerGrpcHealthAndHttpHealth" : "ownerGrpcHealth";
    } else if (projections.length > 0) {
      expected = "deploymentUnitHttpHealthAndFabricReadiness";
    } else {
      expected = isOwner ? "ownerHttpHealth" : "deploymentUnitHttpHealth";
    }
    assert.equal(criterion.kind, expected,
      `${criterion.process} declares kind ${criterion.kind} but its own checks make ${expected}`);
    // A deployment unit publishes no owner identity, so it serves no owner gRPC
    // health service and cannot be probed by one.
    if (!isOwner) {
      assert.ok(!transports.has("grpc"),
        `${criterion.process} is a deployment unit and serves no owner gRPC health service`);
    }
  }
});

test("every HTTP readiness check is a route the owning source registers", async () => {
  const topology = await loadTopology();
  for (const criterion of topology.readinessCriteria.criteria) {
    const directory = ownerProcessDirectory(criterion.process);
    for (const check of criterion.checks) {
      if (check.transport !== "http") continue;
      assert.ok(await registersRoute(directory, check.method as string, check.path as string),
        `${criterion.process} must register ${check.method} ${check.path} for its readiness criterion`);
    }
  }
});

test("every gRPC readiness check is a health service the owning process actually serves", async () => {
  const topology = await loadTopology();
  const ownerServer = await readFile("services/internal/ownerservice/server.go", "utf8");
  assert.ok(ownerServer.includes("healthpb.RegisterHealthServer(server, healthServer)"),
    "the shared owner server must register the gRPC health service");
  assert.ok(ownerServer.includes("healthpb.HealthCheckResponse_SERVING"),
    "the shared owner server must report SERVING only once the owner is ready");
  assert.ok(ownerServer.includes("/grpc.health.v1.Health/Check"),
    "the shared owner server must exempt the health method from peer authentication");
  for (const criterion of topology.readinessCriteria.criteria) {
    if (!criterion.checks.some((check) => check.transport === "grpc")) continue;
    const directory = ownerProcessDirectory(criterion.process);
    assert.ok(await servesOwnerHealthService(directory),
      `${criterion.process} declares a gRPC readiness check, but its own source starts no owner server that serves the health service`);
  }
});

test("a readiness projection names a type the owning source really carries", async () => {
  const topology = await loadTopology();
  for (const criterion of topology.readinessCriteria.criteria) {
    const directory = ownerProcessDirectory(criterion.process);
    for (const check of criterion.checks) {
      if (!check.projection) continue;
      assert.equal(check.projection, "contracts.FabricReadiness",
        `${criterion.process} may only project a readiness state this contract names`);
      let carried = false;
      for (const file of await goFiles(directory)) {
        if ((await readFile(file, "utf8")).includes(check.projection)) {
          carried = true;
          break;
        }
      }
      assert.ok(carried, `${criterion.process} must carry ${check.projection} in its own source`);
    }
  }
});

test("a process added to the declaration without a readiness criterion is refused", async () => {
  const topology = await loadTopology();
  const added: ProcessEntry = { ...topology.processes[0], service: "added-owner" };
  const gaps = readinessGaps([...topology.processes, added], topology.readinessCriteria.criteria);
  assert.deepEqual(gaps.missing, ["added-owner"], "a declared process without a readiness criterion must be refused");
  assert.deepEqual(gaps.unknown, []);
});

test("a readiness criterion for a process the declaration does not declare is refused", async () => {
  const topology = await loadTopology();
  const orphan: ReadinessCriterion = {
    process: "undeclared-owner",
    kind: "ownerGrpcHealth",
    checks: [{ transport: "grpc", role: "readiness", addressEnv: "OPL_CAPABILITY_ADDR", service: "grpc.health.v1.Health", method: "Check" }],
    evidence: "fixture"
  };
  const gaps = readinessGaps(topology.processes, [...topology.readinessCriteria.criteria, orphan]);
  assert.deepEqual(gaps.unknown, ["undeclared-owner"], "a criterion for an undeclared process must be refused");
  assert.deepEqual(gaps.missing, []);
});

test("the readiness proofs tell a served health service apart from a route that only looks like one", async () => {
  const directory = await mkdtemp(path.join(tmpdir(), "opl-readiness-fixture-"));
  try {
    const file = path.join(directory, "routes.go");
    await writeFile(file, `package fixture
func routes(mux *http.ServeMux) {
 mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
}
`);
    assert.equal(await registersRoute(directory, "GET", "/healthz"), true,
      "a registered route must be found");
    assert.equal(await registersRoute(directory, "GET", "/readyz"), false,
      "a route the fixture does not register must not be found");
    assert.equal(await servesOwnerHealthService(directory), false,
      "a process that starts no owner server serves no health service");
    await writeFile(file, `package fixture
func routes(mux *http.ServeMux) {
 mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
}
func serve(ctx context.Context, config ownerservice.Config, source ownerstore.MigrationSource) {
 ownerservice.Start(ctx, config, source, nil)
}
`);
    assert.equal(await servesOwnerHealthService(directory), true,
      "a process that starts the shared owner server serves the health service");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});


// The Cloud backend deployment unit
// ----------------------------------
//
// The owner processes this contract declares are not eleven microservices. They
// are eleven processes that collaborate inside ONE Cloud backend container. The
// contract declared the members and each member's readiness, but it never
// declared the unit they share, what that unit reaches from outside itself, or
// which workloads must stay outside it. A reader could not tell which container
// the declaration described, and could not tell what a restart of that
// container would take with it.
//
// The assertions below read the owning source for every claim the declaration
// makes, so the declaration cannot drift from the repository: a member without
// a start path the image builds, a dependency without a provider boundary this
// contract already declares, or an exclusion that is actually inside the unit
// fails the test.

// The reason a dependency or an exclusion is outside the unit. A dependency is
// something the unit reaches but does not contain; an exclusion is something
// the unit deliberately leaves outside its own restart and upgrade boundary.
const outsideUnitReasons = ["notAProcessOfThisUnit", "notStartedInsideThisUnit"];

// Every kind carries its own proof obligation, so a dependency or an exclusion
// cannot be declared without one.
const dependencyKinds = ["databaseService", "objectStore", "resourceApi"];
const exclusionKinds = ["privilegedExecutor", "customerWorkload"];

// dockerfileBuildStage is one image build stage and the binaries it emits.
type DockerfileBuildStage = {
  name: string;
  workdir: string;
  binaries: Map<string, { moduleDir: string; package: string }>;
};

// parseDockerfileStages reads the image build: which Go package produces which
// binary, and in which stage. The start path of a member is therefore read from
// the image build rather than restated here.
function parseDockerfileStages(dockerfile: string): DockerfileBuildStage[] {
  const joined = dockerfile.replace(/\\\r?\n/g, " ");
  const stages: DockerfileBuildStage[] = [];
  let stage: DockerfileBuildStage | undefined;
  for (const line of joined.split("\n")) {
    const trimmed = line.trim();
    const from = /^FROM\s+(?:--platform=\S+\s+)?(\S+)(?:\s+AS\s+(\S+))?/i.exec(trimmed);
    if (from) {
      stage = { name: from[2] ?? from[1], workdir: "", binaries: new Map() };
      stages.push(stage);
      continue;
    }
    if (!stage) continue;
    const workdir = /^WORKDIR\s+(\S+)/.exec(trimmed);
    if (workdir) {
      stage.workdir = workdir[1];
      continue;
    }
    const loop = /for service in ([^;]+); do/.exec(line);
    const loopServices = loop ? loop[1].trim().split(/\s+/) : [];
    for (const rule of line.matchAll(/go (?:-C (\S+) )?build\b.*?-o \/out\/(\S+) (\.\/cmd\/\S+)/g)) {
      const [, maybeModuleDir, binary, packageDir] = rule;
      const moduleDir = maybeModuleDir ?? (stage.workdir.replace(/^\/src\/?/, "") || ".");
      if (binary.includes("$")) {
        for (const service of loopServices) {
          stage.binaries.set(binary.replace("$service", service), {
            moduleDir: moduleDir.replace("$service", service),
            package: packageDir
          });
        }
        continue;
      }
      stage.binaries.set(binary, { moduleDir, package: packageDir });
    }
    for (const link of line.matchAll(/ln -s (\S+) \/out\/([a-z0-9-]+)\b/g)) {
      const [, targetBinary, linkBinary] = link;
      const targetSpec = stage.binaries.get(targetBinary);
      if (targetSpec) stage.binaries.set(linkBinary, targetSpec);
    }
    const binLoop = /for bin in ([^;]+); do\s+ln -s (\S+) "\/out\/\$bin"/.exec(line);
    if (binLoop) {
      const targetSpec = stage.binaries.get(binLoop[2]);
      if (targetSpec) {
        for (const linkBinary of binLoop[1].trim().split(/\s+/)) {
          stage.binaries.set(linkBinary, targetSpec);
        }
      }
    }
  }
  return stages;
}

// shippedBinaries reads the runtime stage of the image and returns the binaries
// it carries, so a member the image does not ship cannot be declared.
function shippedBinaries(dockerfile: string, stages: DockerfileBuildStage[]): Set<string> {
  const emitted = new Map<string, Set<string>>();
  for (const stage of stages) emitted.set(stage.name, new Set(stage.binaries.keys()));
  const shipped = new Set<string>();
  for (const line of dockerfile.split("\n")) {
    const copy = /^COPY\s+--from=(\S+)\s+(\S+)\s+\S+\s*$/.exec(line.trim());
    if (!copy) continue;
    const stageName = copy[1];
    const source = copy[2];
    const binaries = emitted.get(stageName);
    if (!binaries) continue;
    if (source.endsWith("/")) {
      for (const binary of binaries) shipped.add(binary);
      continue;
    }
    const binary = path.posix.basename(source);
    if (binaries.has(binary)) shipped.add(binary);
  }
  return shipped;
}

// composeFiles lists the installation's Compose files.
async function composeFiles(): Promise<string[]> {
  const names = (await readdir("deploy/portable")).filter((name) => name.endsWith(".yaml"));
  return names.map((name) => `deploy/portable/${name}`).sort();
}

// contractEnvNames reads every environment name this contract declares, so a
// boundary that names an environment name is checked against the contract's own
// declarations instead of introducing a new one.
function contractEnvNames(topology: Topology): Set<string> {
  const names = new Set<string>();
  for (const values of Object.values(topology.sharedEnv)) {
    for (const name of Array.isArray(values) ? values : [values]) names.add(name);
  }
  for (const process of topology.processes) {
    for (const name of [...process.env, ...(process.optionalEnv ?? []), ...(process.externalEnv ?? [])]) names.add(name);
    if (process.addressEnv) names.add(process.addressEnv);
    if (process.peerTokensEnv) names.add(process.peerTokensEnv);
    if (process.rpcAddressEnv) names.add(process.rpcAddressEnv);
    if (process.databaseEnv) names.add(process.databaseEnv);
    for (const listener of process.additionalListeners ?? []) names.add(listener.addressEnv);
  }
  for (const edge of topology.communication?.edges ?? []) {
    names.add(edge.addressEnv);
    names.add(edge.tokenEnv);
    for (const name of edge.activationEnvs ?? []) names.add(name);
  }
  return names;
}

// collectArrays walks a declaration and returns every array in it, so the test
// can prove the unit block never carries a member list of its own.
function collectArrays(value: unknown, found: unknown[][] = []): unknown[][] {
  if (Array.isArray(value)) {
    found.push(value);
    for (const item of value) collectArrays(item, found);
    return found;
  }
  if (value && typeof value === "object") {
    for (const item of Object.values(value)) collectArrays(item, found);
  }
  return found;
}

// evidenceLocation reads a `path:line` reference out of an evidence string.
const evidenceLocation = /((?:[\w.-]+\/)*[\w.-]+):(\d+)/g;

// assertEvidencePointsAtRealSource proves that every cited location names a
// file of this repository and a line that file actually has. An evidence
// string that cites nothing is refused: a claim without a source is not a
// proof.
async function assertEvidencePointsAtRealSource(evidence: string[], subject: string): Promise<void> {
  assert.ok(Array.isArray(evidence) && evidence.length > 0, `${subject} must carry evidence`);
  let located = 0;
  for (const entry of evidence) {
    for (const match of entry.matchAll(evidenceLocation)) {
      const file = match[1];
      const line = Number(match[2]);
      let content: string;
      try {
        content = await readFile(file, "utf8");
      } catch {
        assert.fail(`${subject} cites ${file}, which is not a file in this repository`);
      }
      const lines = content.split("\n");
      assert.ok(line >= 1 && line <= lines.length, `${subject} cites ${file}:${line}, which is outside that file`);
      assert.ok(lines[line - 1].trim().length > 0, `${subject} cites ${file}:${line}, which is an empty line`);
      located += 1;
    }
  }
  assert.ok(located > 0, `${subject} must cite at least one concrete source location`);
}

// parseCompose reads the installation's entrypoint Compose file, which is the
// file the distribution contract names as the portable installation's
// entrypoint.
async function parseEntrypointCompose(): Promise<Record<string, any>> {
  const source = await readFile("deploy/portable/compose.cloud.yaml", "utf8");
  const document = YAML.parseDocument(source, {
    customTags: [
      { tag: "!override", collection: "seq" },
      { tag: "!override", collection: "map" },
      { tag: "!reset", resolve: () => null }
    ],
    logLevel: "silent"
  });
  return (document.toJS() as { services: Record<string, any> }).services;
}

// memberComposeServices names the Compose services that run this unit's
// members. In the composed single-container topology, `cloud-backend` runs the
// composition entrypoint for all declared processes.
function memberComposeServices(topology: Topology, compose: Record<string, any>): Set<string> {
  const composed = compose["cloud-backend"];
  if (composed?.command?.[0] === topology.deploymentUnit?.unit.entrypoint) {
    return new Set(["cloud-backend"]);
  }
  const names = new Set<string>();
  for (const process of topology.processes) {
    const name = compose[process.service] ? process.service : process.ownerName;
    assert.ok(name && compose[name], `${process.service} must run as a Compose service of the installation`);
    names.add(name);
  }
  return names;
}

// unitEntryGaps reports the boundary entries this contract does not support,
// so a fixture is refused the same way the real declaration is.
function unitEntryGaps(topology: Topology, entries: UnitEntry[], kinds: string[], reason: string): string[] {
  const env = contractEnvNames(topology);
  const declared = new Set(topology.processes.map((process) => process.service));
  const gaps: string[] = [];
  for (const entry of entries) {
    if (!kinds.includes(entry.kind)) gaps.push(`${entry.id}:unknown-kind`);
    if (declared.has(entry.id)) gaps.push(`${entry.id}:is-a-declared-process`);
    if (entry.outsideUnit?.reason !== reason) gaps.push(`${entry.id}:wrong-reason`);
    if (!env.has(entry.outsideUnit?.boundaryEnv)) gaps.push(`${entry.id}:undeclared-environment-name`);
  }
  return gaps;
}

test("the Cloud backend unit declares its membership through this contract's own process list", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  assert.ok(block.rule.length > 0, "the deployment unit declaration must state the rule it follows");
  assert.equal(block.unit.memberSource, "processes",
    "the unit must name this contract's own process list as its membership source");
  assert.equal(block.unit.imageSource, "image",
    "the unit must name this contract's own image declaration");
  assert.ok(block.unit.memberRule.length > 0, "the unit must state how its membership is derived");
  assert.ok(block.unit.boundaryEffect.length > 0, "the unit must state what its restart and upgrade boundary means");

  // The contract keeps one writer of the member list. The unit block names the
  // list it references and carries no member list of its own.
  assert.equal((block.unit as unknown as { members?: unknown }).members, undefined,
    "the unit must not declare a member list beside the process list it references");
  const declared = new Set(topology.processes.map((process) => process.service));
  for (const array of collectArrays(block)) {
    if (!array.every((value) => typeof value === "string")) continue;
    const names = array as string[];
    assert.ok(!names.every((name) => declared.has(name)),
      `the unit block must not restate the member list: ${names.join(",")}`);
  }

  // Every declared process is a member of the unit. The membership is the
  // contract's process list, so there is nothing else to enumerate.
  assert.ok(topology.processes.length > 0, "the contract must declare the processes the unit contains");
  assert.deepEqual(block.externalDependencies.map((entry) => entry.id).filter((id) => declared.has(id)), [],
    "no declared process may also be an external dependency of the unit");
  assert.deepEqual(block.exclusions.map((entry) => entry.id).filter((id) => declared.has(id)), [],
    "no declared process may also be excluded from the unit");
});

test("every member of the Cloud backend unit has a start path the product image builds", async () => {
  const topology = await loadTopology();
  const dockerfile = await readFile(dockerfilePath, "utf8");
  const stages = parseDockerfileStages(dockerfile);
  const shipped = shippedBinaries(dockerfile, stages);
  const singleBinaryMain = await readFile("cmd/opl-cloud/main.go", "utf8");
  assert.match(singleBinaryMain, /^package main\b/m, "cmd/opl-cloud/main.go must be package main");
  for (const process of topology.processes) {
    const binary = path.posix.basename(process.binary);
    const rule = stages.flatMap((stage) => [...stage.binaries.entries()]
      .map(([name, value]) => ({ name, value })))
      .find((entry) => entry.name === binary);
    assert.ok(rule, `${process.service} declares ${process.binary}, which the product image does not build`);
    const startPath = path.posix.join(rule.value.moduleDir, rule.value.package, "main.go");
    const source = await readFile(startPath, "utf8");
    assert.match(source, /^package main\b/m, `${startPath} must be package main`);
    const directory = ownerProcessDirectory(process.service);
    assert.ok(singleBinaryMain.includes(`"opl-cloud/${directory}/module"`),
      `${process.service} must register opl-cloud/${directory}/module in ${startPath}`);
    assert.ok(singleBinaryMain.includes(`"${binary}"`),
      `${process.service} symlink binary ${binary} must be dispatched in ${startPath}`);
    const moduleRun = await readFile(`${directory}/module/run.go`, "utf8");
    assert.match(moduleRun, /^package module\b/m, `${directory}/module/run.go must declare package module`);
    assert.ok(moduleRun.includes("func Run("), `${directory}/module/run.go must export Run`);
    assert.ok(shipped.has(binary), `the product image must ship ${binary}`);
  }
});

test("the Cloud backend unit declares the composition entrypoint the product image ships", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  const entrypoint = block.unit.entrypoint;
  assert.equal(typeof entrypoint, "string",
    "the unit must declare the entrypoint its container runs, so an installation can prove the unit by its command");
  assert.ok(entrypoint.startsWith(`${topology.image.binaryDirectory}/`),
    `the declared entrypoint must be a binary of the image's own binary directory: ${entrypoint}`);
  const binary = path.posix.basename(entrypoint);
  const dockerfile = await readFile(dockerfilePath, "utf8");
  const stages = parseDockerfileStages(dockerfile);
  const shipped = shippedBinaries(dockerfile, stages);
  assert.ok(shipped.has(binary), `the product image must ship ${binary}`);
  const rule = stages.flatMap((stage) => [...stage.binaries.entries()]
    .map(([name, value]) => ({ name, value })))
    .find((entry) => entry.name === binary);
  assert.ok(rule, `the product image must build ${binary}`);
  assert.equal(rule.value.package, "./cmd/opl-cloud",
    "the unit's entrypoint must resolve to the unified single-binary composition root");
  const startPath = path.posix.join(rule.value.moduleDir, rule.value.package, "main.go");
  let source: string;
  try {
    source = await readFile(startPath, "utf8");
  } catch {
    assert.fail(`the unit's entrypoint has no real start path: ${startPath} does not exist`);
  }
  assert.match(source, /^package main\b/m, `${startPath} must be the main package of the composition entrypoint`);
  assert.ok(source.includes("composition entrypoint"),
    `${startPath} must declare that it is the composition entrypoint`);
  // The published image keeps its own default command. An installation opts
  // into the composition entrypoint explicitly, so shipping the binary must
  // not change what the image runs on its own.
  assert.ok(!dockerfile.includes(`CMD [${JSON.stringify(entrypoint)}]`),
    "the product image must not change its published default command to the composition entrypoint");
});

test("every external dependency of the Cloud backend unit is reached across a boundary this contract declares", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  assert.ok(block.externalDependencies.length > 0,
    "the unit must declare the dependencies it reaches from outside itself");
  assert.deepEqual(unitEntryGaps(topology, block.externalDependencies, dependencyKinds, "notAProcessOfThisUnit"), [],
    "every declared dependency must be reached across a boundary this contract declares");
  for (const dependency of block.externalDependencies) {
    assert.ok(dependency.outsideUnit.reachedThrough.length > 0,
      `${dependency.id} must state how a consumer tells it is reached from outside the unit`);
    await assertEvidencePointsAtRealSource(dependency.evidence, dependency.id);
  }
});

test("every declared dependency of the Cloud backend unit is proved against the source that reaches it", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  const compose = await parseEntrypointCompose();
  const members = memberComposeServices(topology, compose);
  for (const dependency of block.externalDependencies) {
    const boundaryEnv = dependency.outsideUnit.boundaryEnv;
    if (dependency.kind === "databaseService") {
      assert.equal(boundaryEnv, topology.sharedEnv.database,
        "the database dependency must be reached through the contract's own shared database name");
      const config = await readFile("services/internal/ownerservice/config.go", "utf8");
      assert.ok(config.includes(`getenv("${boundaryEnv}")`),
        "an owner must read the declared database name");
      const database = await readFile("services/internal/ownerservice/database.go", "utf8");
      assert.match(database, /postgresmigrate\.OpenPool\(ctx,\s*databaseURL/,
        "an owner must open a PostgreSQL connection from that name");
      // The database is the installation's own service. It is not a process of
      // the unit, and the installation deploys it as its own container.
      const databaseServices = Object.entries(compose)
        .filter(([, service]) => String(service.image ?? "").startsWith("postgres:"))
        .map(([name]) => name);
      assert.equal(databaseServices.length, 1, "the installation must deploy exactly one PostgreSQL service");
      assert.ok(!members.has(databaseServices[0]),
        "the PostgreSQL service must not be a process of the Cloud backend unit");
    } else if (dependency.kind === "objectStore") {
      const capability = byService.get("capability");
      assert.ok(capability, "the Capability owner must be a declared process");
      assert.ok(capability.env.includes(boundaryEnv),
        "the object store boundary must be an environment name the Capability owner declares");
      const source = await readFile("services/capability/module/run.go", "utf8");
      assert.ok(source.includes(`env.Env("${boundaryEnv}")`),
        "the Capability owner must read the declared storage kind");
      const cosBranch = source.slice(source.indexOf('case "cos":'), source.indexOf("default:"));
      assert.ok(cosBranch.includes("NewCOSObjects"), "the cos kind must build the instance bucket store");
      assert.ok(!cosBranch.includes("OPL_CAPABILITY_OBJECT_ROOT"),
        "the cos kind must read no local object root, so it deploys no local storage");
      const cosStorage = await readFile("services/capability/catalog/cos_storage.go", "utf8");
      assert.match(cosStorage, /cos\.NewClient\(/,
        "the cos store must be a client of the instance's bucket, not a container of the unit");
      // The local kind keeps the bytes on a volume of the Capability container,
      // which is the unit's own container.
      const carriers = Object.entries(compose)
        .filter(([, service]) => JSON.stringify(service.volumes ?? []).includes("/objects"))
        .map(([name]) => name);
      assert.deepEqual(carriers, ["cloud-backend"],
        "only the composed backend container may carry the object store, so the installation deploys no storage container");
      // Every service the installation deploys besides the unit's members is
      // either its PostgreSQL service or its same-origin entry. No other
      // container exists, so no storage container can be deployed.
      for (const [name, service] of Object.entries(compose)) {
        if (members.has(name)) continue;
        const isDatabase = String(service.image ?? "").startsWith("postgres:");
        const isSameOriginEntry = Array.isArray(service.ports) && service.ports.length > 0;
        assert.ok(isDatabase || isSameOriginEntry,
          `the installation deploys ${name}, which is neither a member of the Cloud backend unit, nor its PostgreSQL service, nor its same-origin entry`);
      }
    } else if (dependency.kind === "resourceApi") {
      const fabric = byService.get("fabric");
      assert.ok(fabric, "the Fabric owner must be a declared process");
      assert.ok(fabric.env.includes(boundaryEnv),
        "the resource API boundary must be an environment name the Fabric owner declares");
      const main = await readFile("services/fabric/module/run.go", "utf8");
      assert.ok(main.includes(`env.Env("${boundaryEnv}")`),
        "the Fabric owner must read the declared provider selection");
      const port = await readFile("services/fabric/internal/fabric/provider_port.go", "utf8");
      assert.match(port, /type Provider interface \{/,
        "the Fabric module must declare the provider port its adapters implement");
      const tencent = await readFile("services/fabric/internal/fabric/tencent_provider.go", "utf8");
      assert.match(tencent, /exec\.CommandContext\(ctx, path\)/,
        "the provider must reach the resource API through an external executable");
      const provisioner = (await Promise.all((await goFiles("services/fabric/cmd/opl-tencent-provisioner")).map((file) => readFile(file, "utf8")))).join("\n");
      assert.match(provisioner, /tencentcloudapi\.com/,
        "the provisioner must reach the Tencent Cloud resource API");
    }
  }
});

test("every excluded workload of the Cloud backend unit is genuinely outside it", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  assert.ok(block.exclusions.length > 0, "the unit must declare the workloads it leaves outside itself");
  assert.deepEqual(unitEntryGaps(topology, block.exclusions, exclusionKinds, "notStartedInsideThisUnit"), [],
    "every declared exclusion must be proved outside the unit");
  for (const exclusion of block.exclusions) {
    assert.ok(exclusion.distinction && exclusion.distinction.length > 0,
      `${exclusion.id} must state what the unit contains that is distinct from it`);
    await assertEvidencePointsAtRealSource(exclusion.evidence, exclusion.id);
  }
});

test("the excluded privileged build executor is an external builder, not a process of the unit", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  for (const exclusion of block.exclusions.filter((entry) => entry.kind === "privilegedExecutor")) {
    // The build owner service is a member; the builder it invokes is not.
    const member = byService.get(exclusion.relatedMember as string);
    assert.ok(member, `${exclusion.id} must be distinguished from a declared process of the unit`);
    assert.equal(member.service, "build", "the privileged build executor is distinguished from the Build owner service");
    const runner = await readFile("services/build/internal/build/runner.go", "utf8");
    assert.match(runner, /exec\.CommandContext\(bounded, "docker", args\.\.\.\)/,
      "the Build owner must reach the builder through an external docker client");
    assert.match(runner, /"--builder", r\.Builder/,
      "the Build owner must name the installation's builder, not start one");
    const dockerfile = await readFile(dockerfilePath, "utf8");
    assert.ok(dockerfile.includes("docker-buildx"), "the product image ships the build client");
    assert.ok(!/moby\/buildkit|buildkitd/.test(dockerfile),
      "the product image must not ship a BuildKit daemon: the builder is outside the unit");
    for (const file of await composeFiles()) {
      const source = await readFile(file, "utf8");
      assert.ok(!/moby\/buildkit|buildkitd/.test(source), `${file} must not deploy a BuildKit daemon`);
    }
  }
});

test("the excluded customer Workspace application is created by the cluster, not started inside the unit", async () => {
  const topology = await loadTopology();
  const block = topology.deploymentUnit;
  assert.ok(block, `${topologyPath} must declare the Cloud backend deployment unit`);
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  for (const exclusion of block.exclusions.filter((entry) => entry.kind === "customerWorkload")) {
    // The Serve owner service is a member; the customer's container is not.
    const member = byService.get(exclusion.relatedMember as string);
    assert.ok(member, `${exclusion.id} must be distinguished from a declared process of the unit`);
    assert.equal(member.service, "serve", "the customer workload is distinguished from the Serve owner service");
    const execution = await readFile("services/serve/internal/delivery/execution.go", "utf8");
    assert.ok(execution.includes("tkeapply.NewClusterClient("),
      "the Serve owner must reach the cluster through the k8s.io/client-go execution boundary");
    const client = await readFile("services/internal/tkeapply/client.go", "utf8");
    assert.match(client, /kubernetes\.NewForConfig\(cfg\)/,
      "the cluster boundary must talk to the Kubernetes API via client-go, not start the workload in the unit");
    assert.match(client, /Deployments\(c\.namespace\)\.Apply\(/,
      "the customer workload must be applied through Kubernetes Server-Side Apply");
    const runtime = await readFile("services/internal/tkeapply/runtime.go", "utf8");
    assert.match(runtime, /applyappsv1\.Deployment\(/,
      "the customer workload must be applied as a typed Deployment manifest the cluster creates");
    // The customer application image is an installation fact: the product image
    // builds no customer workload image.
    const dockerfile = await readFile(dockerfilePath, "utf8");
    assert.ok(!dockerfile.includes("OPL_WORKSPACE_IMAGE"),
      "the product image must not carry a customer application image");
    assert.ok(contractEnvNames(topology).has("OPL_WORKSPACE_IMAGE"),
      "the customer application image must be an installation fact this contract declares");
  }
});

test("a boundary entry this contract does not support is refused", () => {
  const topology = {
    sharedEnv: { database: "DATABASE_URL" },
    processes: [{ service: "capability", kind: "ownerservice", binary: "/usr/local/bin/opl-capability", addressEnv: "OPL_CAPABILITY_ADDR", defaultAddress: ":8181", env: ["OPL_CAPABILITY_STORAGE_KIND"] }]
  } as unknown as Topology;
  const entry = (over: Partial<UnitEntry>): UnitEntry => ({
    id: "fixture", kind: "objectStore",
    outsideUnit: { reason: "notAProcessOfThisUnit", reachedThrough: "the fixture boundary", boundaryEnv: "OPL_CAPABILITY_STORAGE_KIND" },
    evidence: ["services/capability/module/run.go:159 reads the storage kind"],
    ...over
  });
  const base = entry({});
  assert.deepEqual(unitEntryGaps(topology, [base], dependencyKinds, "notAProcessOfThisUnit"), [],
    "a supported dependency must pass");
  assert.deepEqual(unitEntryGaps(topology, [entry({ kind: "unknownKind" })], dependencyKinds, "notAProcessOfThisUnit"),
    ["fixture:unknown-kind"], "a dependency kind without a proof obligation must be refused");
  assert.deepEqual(unitEntryGaps(topology, [entry({ id: "capability" })], dependencyKinds, "notAProcessOfThisUnit"),
    ["capability:is-a-declared-process"], "a declared process must not also be an external dependency");
  assert.deepEqual(unitEntryGaps(topology, [entry({ outsideUnit: { reason: "notStartedInsideThisUnit", reachedThrough: "x", boundaryEnv: "OPL_CAPABILITY_STORAGE_KIND" } })],
    dependencyKinds, "notAProcessOfThisUnit"), ["fixture:wrong-reason"],
    "a dependency must be reached from outside the unit, not merely left outside it");
  assert.deepEqual(unitEntryGaps(topology, [entry({ outsideUnit: { reason: "notAProcessOfThisUnit", reachedThrough: "x", boundaryEnv: "OPL_UNKNOWN_ENV" } })],
    dependencyKinds, "notAProcessOfThisUnit"), ["fixture:undeclared-environment-name"],
    "a boundary must name an environment name this contract already declares");
  assert.deepEqual(unitEntryGaps(topology, [entry({ outsideUnit: { reason: "notStartedInsideThisUnit", reachedThrough: "x", boundaryEnv: "OPL_CAPABILITY_STORAGE_KIND" } })],
    exclusionKinds, "notStartedInsideThisUnit"), ["fixture:unknown-kind"],
    "an exclusion must declare an exclusion kind, not a dependency kind");
});

// The peer address of an owner is the caller-side environment it uses to reach
// another owner, and the target owner's own declared listener that environment
// names. The contract declares what every owner listens on; until now it did
// not declare the environments the installation uses to reach a peer, so a
// consumer that runs the owners in one network namespace had no way to resolve
// a peer to the address it actually listens on. The assertions below read the
// published installation and this contract and derive every expected value
// from them, so no address is restated here.

type InstalledPeerAddress = {
  caller: string;
  target: string;
  addressEnv: string;
  value: string;
  port: number;
};

type PeerAddressDeclaration = {
  kind: "edge" | "peerAddress";
  caller: string;
  target: string;
  addressEnv: string;
  tokenEnv: string;
  targetAddressEnv?: string;
};

// peerOrigin reads the peer a caller-side address value names: the host it
// carries and the port it reaches. A value that carries no host and port names
// no peer, so it is not a peer address.
function peerOrigin(value: string): { host: string; port: number } | undefined {
  const bare = /^([a-z0-9][a-z0-9-]*):([0-9]{1,5})$/.exec(value);
  const url = /^[a-z][a-z0-9+.-]*:\/\/([a-z0-9][a-z0-9.-]*):([0-9]{1,5})$/.exec(value);
  const match = bare ?? url;
  if (!match) return undefined;
  const port = Number(match[2]);
  if (!Number.isInteger(port) || port < 1 || port > 65535) return undefined;
  return { host: match[1], port };
}

function isComposedBackendCompose(topology: Topology, compose: Record<string, any>): boolean {
  return compose["cloud-backend"]?.command?.[0] === topology.deploymentUnit?.unit.entrypoint;
}

// ownerOfComposeService maps every Compose service of the published
// installation to the owner process that runs in it.
function ownerOfComposeService(topology: Topology, compose: Record<string, any>): Map<string, ProcessEntry> {
  const owners = new Map<string, ProcessEntry>();
  for (const process of topology.processes) {
    const name = compose[process.service] ? process.service : process.ownerName;
    assert.ok(name && compose[name], `${process.service} must run as a Compose service of the installation`);
    owners.set(name, process);
  }
  return owners;
}

// installedPeerAddresses returns every peer address the published portable
// installation sets on an owner. In composed single-container mode, peer
// addresses in cloud-backend target 127.0.0.1:<port> matching each declared
// target listener port.
async function installedPeerAddresses(topology: Topology): Promise<InstalledPeerAddress[]> {
  const compose = await parseEntrypointCompose();
  if (isComposedBackendCompose(topology, compose)) {
    const environment = (compose["cloud-backend"]?.environment ?? {}) as Record<string, unknown>;
    const byPrincipal = new Map(topology.processes.map((process) => [process.transportPrincipal, process]));
    const peers: InstalledPeerAddress[] = [];
    for (const declaration of declaredPeerAddresses(topology)) {
      const raw = environment[declaration.addressEnv];
      if (typeof raw !== "string") continue;
      const origin = peerOrigin(raw.trim());
      if (!origin || origin.host !== "127.0.0.1") continue;
      const target = byPrincipal.get(declaration.target);
      assert.ok(target, `${declaration.target} must be a declared owner process`);
      peers.push({
        caller: declaration.caller,
        target: declaration.target,
        addressEnv: declaration.addressEnv,
        value: raw.trim(),
        port: origin.port
      });
    }
    return peers;
  }
  const owners = ownerOfComposeService(topology, compose);
  const peers: InstalledPeerAddress[] = [];
  for (const [service, owner] of owners) {
    const environment = (compose[service]?.environment ?? {}) as Record<string, unknown>;
    for (const [addressEnv, raw] of Object.entries(environment)) {
      if (typeof raw !== "string") continue;
      const origin = peerOrigin(raw.trim());
      if (!origin) continue;
      const target = owners.get(origin.host);
      if (!target) continue;
      peers.push({
        caller: owner.transportPrincipal as string,
        target: target.transportPrincipal as string,
        addressEnv,
        value: raw.trim(),
        port: origin.port
      });
    }
  }
  return peers;
}

// declaredPeerAddresses returns every peer address this contract declares: the
// gRPC transport edges and the retained HTTP peer origins. Both carry the same
// caller, target, address and token facts, and each is declared exactly once.
function declaredPeerAddresses(topology: Topology): PeerAddressDeclaration[] {
  const declarations: PeerAddressDeclaration[] = [];
  for (const edge of topology.communication?.edges ?? []) {
    declarations.push({
      kind: "edge",
      caller: edge.caller as string,
      target: edge.target as string,
      addressEnv: edge.addressEnv,
      tokenEnv: edge.tokenEnv
    });
  }
  for (const entry of topology.peerAddresses?.entries ?? []) {
    declarations.push({
      kind: "peerAddress",
      caller: entry.caller,
      target: entry.target,
      addressEnv: entry.addressEnv,
      tokenEnv: entry.tokenEnv,
      targetAddressEnv: entry.targetAddressEnv
    });
  }
  return declarations;
}

// declaredListenerPorts returns the ports the contract declares for each listen
// environment of one process, read from that process's own declaration.
function declaredListenerPorts(process: ProcessEntry): Map<string, number[]> {
  const ports = new Map<string, number[]>();
  const add = (addressEnv: string | undefined, address: string | undefined) => {
    if (!addressEnv || !address) return;
    const port = Number(address.slice(address.lastIndexOf(":") + 1));
    if (!Number.isInteger(port)) return;
    ports.set(addressEnv, [...(ports.get(addressEnv) ?? []), port]);
  };
  add(process.addressEnv, process.defaultAddress);
  add(process.rpcAddressEnv, process.defaultRPCAddress);
  for (const listener of process.additionalListeners ?? []) add(listener.addressEnv, listener.defaultAddress);
  return ports;
}

test("every peer address the published installation sets on an owner is declared by this contract and bound to the target's declared listener", async () => {
  const topology = await loadTopology();
  const peers = await installedPeerAddresses(topology);
  assert.ok(peers.length > 0, "the published installation must set peer addresses on its owners");
  const byPrincipal = new Map(topology.processes.map((process) => [process.transportPrincipal, process]));
  const declarations = declaredPeerAddresses(topology);

  for (const peer of peers) {
    const matching = declarations.filter((declaration) =>
      declaration.caller === peer.caller && declaration.addressEnv === peer.addressEnv);
    assert.ok(matching.length > 0,
      `${peer.caller} sets ${peer.addressEnv} to reach ${peer.target}, which this contract must declare`);
    for (const declaration of matching) {
      assert.equal(declaration.target, peer.target,
        `${peer.caller} reaches ${peer.target} through ${peer.addressEnv}, so the contract must not declare it against ${declaration.target}`);
    }
    const declaration = matching[0];
    const target = byPrincipal.get(peer.target);
    assert.ok(target, `${peer.target} must be a declared owner process`);
    const ports = declaredListenerPorts(target);
    if (declaration.kind === "peerAddress") {
      assert.ok(declaration.targetAddressEnv,
        `${peer.caller} reaches ${peer.target} through ${peer.addressEnv}, so the declaration must bind it to a declared listener`);
      const bound = ports.get(declaration.targetAddressEnv);
      assert.ok(bound, `${peer.target} must declare the listener ${declaration.targetAddressEnv} this contract binds ${peer.addressEnv} to`);
      assert.ok(bound.includes(peer.port),
        `${peer.addressEnv} reaches ${peer.target} on port ${peer.port}, which ${declaration.targetAddressEnv} does not declare`);
    } else {
      // A transport edge reaches the target's own gRPC listener, so the port
      // the installation dials must be a port that listener declares.
      const declared = [...declaredListenAddresses(target).grpc]
        .flatMap((addressEnv) => ports.get(addressEnv) ?? []);
      assert.ok(declared.includes(peer.port),
        `${peer.addressEnv} reaches ${peer.target} on port ${peer.port}, which no declared gRPC listener of that owner carries`);
    }
  }
});

test("every peer address this contract declares is one the published installation actually sets on that owner", async () => {
  const topology = await loadTopology();
  const compose = await parseEntrypointCompose();
  const byPrincipal = new Map(topology.processes.map((process) => [process.transportPrincipal, process]));
  const declarations = declaredPeerAddresses(topology);

  if (isComposedBackendCompose(topology, compose)) {
    const environment = (compose["cloud-backend"]?.environment ?? {}) as Record<string, unknown>;
    for (const declaration of declarations) {
      assert.equal(typeof environment[declaration.addressEnv], "string",
        `cloud-backend must declare ${declaration.addressEnv} for ${declaration.caller}`);
      assert.equal(typeof environment[declaration.tokenEnv], "string",
        `cloud-backend must declare ${declaration.tokenEnv} for ${declaration.addressEnv}`);
      const target = byPrincipal.get(declaration.target);
      assert.ok(target, `${declaration.target} must be a declared owner process`);
      const targetPorts = declaredListenerPorts(target);
      const raw = String(environment[declaration.addressEnv]).trim();
      const origin = peerOrigin(raw);
      if (!origin) continue;
      assert.equal(origin.host, "127.0.0.1",
        `${declaration.addressEnv} in composed cloud-backend must use loopback 127.0.0.1, got ${raw}`);
      const expectedPorts = declaration.targetAddressEnv
        ? (targetPorts.get(declaration.targetAddressEnv) ?? [])
        : [...declaredListenAddresses(target).grpc].flatMap((env) => targetPorts.get(env) ?? []);
      assert.ok(expectedPorts.includes(origin.port),
        `${declaration.addressEnv} targets port ${origin.port} which ${declaration.target} does not declare`);
    }
    return;
  }

  const owners = ownerOfComposeService(topology, compose);
  for (const declaration of declarations) {
    const caller = byPrincipal.get(declaration.caller);
    assert.ok(caller, `${declaration.caller} must be a declared owner process`);
    const service = owners.has(caller.service) ? caller.service : caller.ownerName;
    const environment = (compose[service]?.environment ?? {}) as Record<string, unknown>;
    assert.equal(typeof environment[declaration.addressEnv], "string",
      `the installation must set ${declaration.addressEnv} on ${declaration.caller}, which this contract declares as a peer address`);
    assert.equal(typeof environment[declaration.tokenEnv], "string",
      `the installation must set ${declaration.tokenEnv} on ${declaration.caller}, the token this contract declares for ${declaration.addressEnv}`);
    const origin = peerOrigin(String(environment[declaration.addressEnv]).trim());
    if (!origin) continue;
    const target = owners.get(origin.host);
    assert.ok(target, `${declaration.addressEnv} names ${origin.host}, which is not an owner of the installation`);
    assert.equal(target.transportPrincipal, declaration.target,
      `${declaration.caller} reaches ${origin.host} through ${declaration.addressEnv}, so the contract must not declare it against ${declaration.target}`);
  }
});

test("every peer address this contract declares is declared exactly once", async () => {
  const topology = await loadTopology();
  const edges = topology.communication?.edges ?? [];
  const entries = topology.peerAddresses?.entries ?? [];

  // The transport edges and the peer-address origins are two declarations of
  // the same fact, so an owner may not carry a peer address in both.
  const edgeAddresses = new Set(edges.map((edge) => `${edge.caller}:${edge.addressEnv}`));
  const restated = entries.filter((entry) => edgeAddresses.has(`${entry.caller}:${entry.addressEnv}`));
  assert.deepEqual(restated.map((entry) => `${entry.caller}:${entry.addressEnv}`), [],
    "the contract keeps one writer of every peer address, so the peer-address origins must not restate a transport edge");

  // Within one declaration block a peer address appears once per credential
  // spelling the installation carries, and never for two peers at once.
  const seen = new Map<string, number>();
  for (const entry of entries) {
    const key = `${entry.caller}:${entry.addressEnv}`;
    seen.set(key, (seen.get(key) ?? 0) + 1);
  }
  const repeated = [...seen].filter(([, count]) => count > 1).map(([key]) => key);
  assert.deepEqual(repeated, [],
    "an owner may declare the same peer address once per credential spelling, never twice");
});

// declaredHostsAndPorts reports every host or port the declaration carries. The
// peer-address block names a listener environment and a scheme, never an
// address: a consumer reads the port from the target's own declaration, so a
// host or a port restated here would be a second writer of that fact.
function declaredHostsAndPorts(value: unknown, path: string[] = []): string[] {
  const found: string[] = [];
  if (typeof value === "string") {
    if (/:\/\//.test(value) || /:\d{1,5}(?!\d)/.test(value)) found.push(`${path.join(".")}=${value}`);
    return found;
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => found.push(...declaredHostsAndPorts(item, [...path, String(index)])));
    return found;
  }
  if (value && typeof value === "object") {
    for (const [key, item] of Object.entries(value)) {
      if (/port/i.test(key)) found.push(`${[...path, key].join(".")} declares a port`);
      found.push(...declaredHostsAndPorts(item, [...path, key]));
    }
  }
  return found;
}

test("every peer address origin declares the scheme of the peer it binds, and the declaration names no host and no port", async () => {
  const topology = await loadTopology();
  const block = topology.peerAddresses;
  assert.ok(block, `${topologyPath} must declare the peer addresses the installation sets on its owners`);
  assert.ok(block.binding && block.binding.length > 0,
    "the peer-address declaration must state how a consumer resolves a peer it binds");
  const schemes = block.originSchemes ?? [];
  assert.ok(schemes.length > 0, "the peer-address declaration must recognize the schemes its origins use");
  for (const entry of block.entries) {
    assert.ok(entry.originScheme,
      `${entry.caller} reaches ${entry.target} through ${entry.addressEnv}, so the declaration must carry the scheme of that origin`);
    assert.ok(schemes.includes(entry.originScheme),
      `${entry.caller} reaches ${entry.target} through ${entry.addressEnv} with the scheme ${entry.originScheme}, which this contract does not recognize`);
  }
  assert.deepEqual(declaredHostsAndPorts(block), [],
    "the peer-address declaration names a listener environment and a scheme, never a host and never a port");
});

test("every peer address that names another Cloud owner and that no transport edge declares is carried by the peer-address origins", async () => {
  const topology = await loadTopology();
  const peers = await installedPeerAddresses(topology);
  const edges = topology.communication?.edges ?? [];
  const entries = topology.peerAddresses?.entries ?? [];
  const edgeDeclared = new Set(edges.map((edge) => `${edge.caller}:${edge.addressEnv}`));
  const originDeclared = new Set(entries.map((entry) => `${entry.caller}:${entry.addressEnv}`));
  for (const peer of peers) {
    const key = `${peer.caller}:${peer.addressEnv}`;
    if (edgeDeclared.has(key)) continue;
    assert.ok(originDeclared.has(key),
      `${peer.caller} sets ${peer.addressEnv} to reach ${peer.target}, which no transport edge declares, so the peer-address origins must carry it`);
  }
});
