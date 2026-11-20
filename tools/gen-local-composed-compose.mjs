// Derive the composed local installation from the proven split form.
//
// The split form gives every owner its own container and its own environment.
// The composed form runs all of them in one process space, so the owner's
// environments become one environment, and an address that named another
// container becomes an address in the same network namespace.
import { readFileSync, writeFileSync } from "node:fs";
import YAML from "yaml";

const split = YAML.parse(readFileSync("deploy/portable/compose.local-v2.yaml", "utf8"));

// The split file expresses its shared parts with YAML merge keys. The
// composed file has to be self-contained, so every merge is expanded here.
function expand(value) {
  if (Array.isArray(value)) return value.map(expand);
  if (!value || typeof value !== "object") return value;
  const merged = value["<<"];
  const base = merged === undefined ? {} : (Array.isArray(merged) ? Object.assign({}, ...merged.map(expand)) : expand(merged));
  const own = { ...base };
  for (const [key, entry] of Object.entries(value)) {
    if (key === "<<") continue;
    own[key] = expand(entry);
  }
  return own;
}

const svc = expand(split.services);
const common = expand(split["x-opl-v2-common"]);
const ownerNames = Object.keys(svc).filter(name => !["postgres", "sub2api-authority", "cloud-local-v2"].includes(name));

// One environment for the process space: the union of the owner environments.
const environment = {};
for (const name of ownerNames) {
  for (const [key, value] of Object.entries(svc[name].environment || {})) environment[key] = value;
}

// An address that named another container names a port in this process space
// now. A URL that named another container addresses the loopback interface.
const containerNames = ownerNames.join("|");
const addressPattern = new RegExp(`^(?:${containerNames}):(\\d+)$`);
const urlPattern = new RegExp(`\\b(?:${containerNames}):(\\d+)\\b`, "g");
for (const [key, value] of Object.entries(environment)) {
  if (typeof value !== "string") continue;
  const address = addressPattern.exec(value);
  if (address) {
    environment[key] = `:${address[1]}`;
    continue;
  }
  environment[key] = value.replace(urlPattern, "127.0.0.1:$1");
}

// The tenant is the CloudIdentity authority: a configured address would make
// it dial itself. The composed process space carries one environment, so the
// address stays unset for every owner and each one resolves the authority
// from the contract's own default address.
delete environment.OPL_CLOUD_IDENTITY_URL;

environment.OPL_CLOUD_OWNER_TOPOLOGY = "/etc/opl-cloud/owner-topology.json";

// The composed process space carries one certificate directory: every owner
// presents the certificate the shared directory holds under its own transport
// principal.
environment.OPL_GRPC_CERT_DIR = "/pki";

// The retained Control Plane refuses to start without the approved registry
// repositories it may browse. The split form never declares the fact, so the
// composed process space has to carry it as one.
environment.OPL_WORKSPACE_REGISTRY_REPOSITORIES =
  "${OPL_WORKSPACE_REGISTRY_REPOSITORIES:?Set OPL_WORKSPACE_REGISTRY_REPOSITORIES to the approved registry repositories}";

// The contract names the installation input every owner's database is
// projected from. The split form gives every owner its own DATABASE_URL, so
// the composed form carries the same DSN under the name the contract
// declares, and the composer projects it back to the child's DATABASE_URL.
const contract = JSON.parse(readFileSync("deploy/portable/opl-cloud-owner-topology.json", "utf8"));
const byBinary = new Map(contract.processes.map(entry => [entry.binary.split("/").pop(), entry]));
for (const name of ownerNames) {
  const command = svc[name].command?.[0] || "";
  const owner = byBinary.get(command.split("/").pop());
  if (!owner) continue;
  const dsn = (svc[name].environment || {}).DATABASE_URL;
  if (!dsn) continue;
  if (owner.compositionDatabaseEnv && owner.compositionDatabaseEnv !== "DATABASE_URL") {
    environment[owner.compositionDatabaseEnv] = dsn;
  }
  // The Fabric owner owns a database of its own in the deployed installation.
  if (owner.service === "fabric") environment.OPL_FABRIC_DATABASE_URL = dsn;
}

// The container directories every owner writes to, in one process space.
const volumes = [];
const volumeTargets = new Set();
for (const name of ownerNames) {
  for (const volume of svc[name].volumes || []) {
    const target = typeof volume === "string" ? volume.slice(volume.lastIndexOf(":") + 1) : volume.target;
    if (volumeTargets.has(target)) continue;
    volumeTargets.add(target);
    volumes.push(volume);
  }
}
// The owner topology contract is not in the image: the installation provides
// it, exactly as the Instance provides it to the composed Deployment.
volumes.push({ type: "bind", source: "./opl-cloud-owner-topology.json", target: "/etc/opl-cloud/owner-topology.json", read_only: true });

const { environment: _sharedEnvironment, ...commonService } = common;
const backend = {
  ...commonService,
  command: ["/usr/local/bin/opl-cloud-composition"],
  environment,
  volumes,
  depends_on: {
    postgres: { condition: "service_healthy" },
    "cloud-local-v2": { condition: "service_healthy" },
  },
};

const composed = {
  name: "opl-cloud-local-composed",
  services: {
    postgres: svc.postgres,
    "sub2api-authority": svc["sub2api-authority"],
    backend,
    "cloud-local-v2": {
      ...svc["cloud-local-v2"],
      volumes: svc["cloud-local-v2"].volumes.map(volume =>
        volume.type === "bind" && volume.target === "/etc/nginx/conf.d/default.conf"
          ? { ...volume, source: "./local-composed-proxy.nginx.conf" }
          : volume),
      depends_on: { "sub2api-authority": { condition: "service_healthy" } },
    },
  },
  volumes: expand(split.volumes),
  networks: expand(split.networks),
  configs: expand(split.configs),
};

writeFileSync("deploy/portable/compose.local-composed.yaml", YAML.stringify(composed, { lineWidth: 0 }));

// The same-origin entry: the route provider still runs in its own container,
// so it addresses the process space by its service name.
const proxy = readFileSync("deploy/portable/local-v2-proxy.nginx.conf", "utf8");
writeFileSync(
  "deploy/portable/local-composed-proxy.nginx.conf",
  proxy.replace(new RegExp(`\\b(${containerNames}):(\\d+)\\b`, "g"), "backend:$2"),
);

console.log("composed compose written");
console.log("owners:", ownerNames.length, "| environment keys:", Object.keys(environment).length);
console.log("volume targets:", [...volumeTargets].join(", "));
