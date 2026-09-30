import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

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
  defaultAddress: string;
  databaseEnv?: string | null;
  env: string[];
  optionalEnv?: string[];
  externalEnv?: string[];
  registers?: string[];
  notes?: string[];
};
type Topology = {
  sharedEnv: Record<string, string | string[]>;
  derivedEnv: { address: string; peerTokens: string };
  processes: ProcessEntry[];
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
async function envNamesRead(dir: string): Promise<Set<string>> {
  const names = new Set<string>();
  for (const file of await goFiles(dir)) {
    const source = await readFile(file, "utf8");
    for (const match of source.matchAll(/(?:\w+\.)?[gG]etenv\("([A-Z0-9_]+)"\)/g)) names.add(match[1]);
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
    let commands: string[];
    try {
      commands = (await readdir(`services/${entry.name}/cmd`, { withFileTypes: true })).filter((item) => item.isDirectory()).map((item) => item.name);
    } catch {
      continue;
    }
    for (const command of commands) {
      const main = `services/${entry.name}/cmd/${command}/main.go`;
      let source: string;
      try {
        source = await readFile(main, "utf8");
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

test("every owner entrypoint is declared with the address and peer identity it reads", async () => {
  const topology = await loadTopology();
  const [calls, names] = await Promise.all([loadConfigCalls(), ownerNameByConstant()]);
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
    assert.equal(process.defaultAddress, call.defaultAddress);
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
    ["fabric", "services/fabric/cmd/fabric/main.go", ":8082"],
    ["console-bff", "apps/console-bff/cmd/server/main.go", ":8190"],
    ["control-plane", "services/control-plane/cmd/control-plane/main.go", ":8787"]
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
    const read = await envNamesRead(dir);
    const declared = new Set([
      ...process.env,
      ...(process.optionalEnv ?? []),
      ...(process.externalEnv ?? []),
      ...(process.kind === "ownerservice" ? [process.addressEnv, process.peerTokensEnv as string] : []),
      ...Object.values(topology.sharedEnv).flatMap((value) => Array.isArray(value) ? value : [value])
    ]);
    const undeclared = [...read].filter((name) => !declared.has(name)).sort();
    const stale = [...declared].filter((name) => !read.has(name) && name !== process.addressEnv && name !== process.peerTokensEnv
      && !Object.values(topology.sharedEnv).flatMap((value) => Array.isArray(value) ? value : [value]).includes(name)).sort();
    assert.deepEqual(undeclared, [], `${process.service} reads undeclared environment names`);
    assert.deepEqual(stale, [], `${process.service} declares environment names no source reads`);
  }
});

test("the declaration covers every owner binary the product image builds", async () => {
  const topology = await loadTopology();
  const dockerfile = await readFile(dockerfilePath, "utf8");
  const publisher = dockerfile.slice(dockerfile.indexOf("AS publisher-build"), dockerfile.indexOf("FROM docker:27.5.1-cli"));
  const loop = publisher.match(/for service in ([^;]+); do/);
  assert.ok(loop, "publisher-build must iterate its owner set");
  const byService = new Map(topology.processes.map((process) => [process.service, process]));
  for (const service of [...loop[1].trim().split(/\s+/), "console-bff"].sort()) {
    const process = byService.get(service);
    assert.ok(process, `${service} is built into the product image and must be declared in ${topologyPath}`);
    assert.ok(publisher.includes(`-o /out/opl-${service} `) || publisher.includes("-o /out/opl-$service "),
      `publisher-build must emit opl-${service}`);
    assert.equal(process.binary, `/usr/local/bin/opl-${service}`);
  }
  for (const [service, binary] of [["control-plane", "opl-control-plane"], ["fabric", "opl-fabric"], ["ledger", "opl-ledger"]] as const) {
    assert.equal(byService.get(service)?.binary, `/usr/local/bin/${binary}`);
    assert.ok(dockerfile.includes(`/out/${binary} /usr/local/bin/${binary}`), `the runtime stage must ship ${binary}`);
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
