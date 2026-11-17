import assert from "node:assert/strict";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
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
  rpcAddressEnv?: string;
  defaultRPCAddress?: string;
  rpcDatabaseEnv?: string;
  defaultAddress: string;
  databaseEnv?: string | null;
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
type Topology = {
  sharedEnv: Record<string, string | string[]>;
  derivedEnv: { address: string; peerTokens: string };
  processes: ProcessEntry[];
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
    if (/os\.Getenv\("OPL_"\s*\+/.test(source)) {
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
  assert.equal(fabric.defaultRPCAddress, ":8187");
  assert.equal(fabric.rpcDatabaseEnv, "OPL_FABRIC_DATABASE_URL");
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
