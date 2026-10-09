import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

// The Gateway Integration deployment unit contract.
//
// CloudIdentity (`tenant`) and Gateway Integration (`gateway`) are two data
// owners inside one process, and that process carries exactly one transport
// identity: `tenant`. The mTLS transport admits a peer only when its certificate
// identifies the exact service it dialed, so every caller that reaches the
// Gateway owner must resolve the deployment unit's transport principal. A caller
// that dials the service name `gateway` can never complete a handshake.
//
// This test derives the contract from the owning sources: the mapping in the
// transport owner, the topology declaration of the unit, and every DialOptions
// call site that can reach the Gateway owner. The mapping and the call sites are
// pinned together so neither can drift without the other.

const transportPath = "packages/contracts/go/owneridentity/transport.go";
const topologyPath = "deploy/portable/opl-cloud-owner-topology.json";
const gatewayCallSiteFiles = [
  "apps/console-bff/internal/clients/clients.go",
  "services/serve/internal/delivery/service.go",
  "services/ledger/eventconsumer/configure.go",
  "services/workspace/cmd/server/main.go",
];

// dialOptionsArguments returns the top-level arguments of every DialOptions(...)
// invocation in one Go source file. Parentheses are tracked so a nested call such
// as DeploymentUnitTransportPrincipal(owner) stays one argument.
function dialOptionsArguments(source: string): { args: string[]; line: number }[] {
  const calls: { args: string[]; line: number }[] = [];
  const needle = "DialOptions(";
  let index = source.indexOf(needle);
  while (index !== -1) {
    let depth = 0;
    let end = -1;
    for (let i = index + needle.length - 1; i < source.length; i += 1) {
      if (source[i] === "(") depth += 1;
      else if (source[i] === ")") {
        depth -= 1;
        if (depth === 0) {
          end = i;
          break;
        }
      }
    }
    assert.ok(end !== -1, `unterminated DialOptions call in source at index ${index}`);
    const args: string[] = [];
    let current = "";
    let localDepth = 0;
    for (const char of source.slice(index + needle.length, end)) {
      if (char === "(") localDepth += 1;
      if (char === ")") localDepth -= 1;
      if (char === "," && localDepth === 0) {
        args.push(current.trim());
        current = "";
        continue;
      }
      current += char;
    }
    if (current.trim()) args.push(current.trim());
    calls.push({ args, line: source.slice(0, index).split("\n").length });
    index = source.indexOf(needle, end);
  }
  return calls;
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

test("the transport owner maps the Gateway owner onto its deployment unit principal", async () => {
  const source = await readFile(transportPath, "utf8");
  const declaration = source.indexOf("func DeploymentUnitTransportPrincipal(owner Owner) Service {");
  assert.ok(declaration !== -1, "DeploymentUnitTransportPrincipal must be declared by the transport owner");
  const bodyStart = source.indexOf("{", declaration);
  let depth = 0;
  let bodyEnd = -1;
  for (let i = bodyStart; i < source.length; i += 1) {
    if (source[i] === "{") depth += 1;
    else if (source[i] === "}") {
      depth -= 1;
      if (depth === 0) {
        bodyEnd = i;
        break;
      }
    }
  }
  assert.ok(bodyEnd !== -1, "DeploymentUnitTransportPrincipal must have a body");
  const body = source.slice(bodyStart, bodyEnd);
  assert.match(body, /owner == Gateway/, "the only mapped owner is Gateway Integration");
  assert.match(body, /return Tenant\.Service\(\)/, "the Gateway owner is served by the tenant deployment unit");
  assert.match(body, /return owner\.Service\(\)/, "every other owner keeps its own transport identity");
});

test("the topology declares one tenant-identity process serving both Gateway API groups", async () => {
  const topology = JSON.parse(await readFile(topologyPath, "utf8")) as {
    processes: { service: string; ownerName?: string; ownerConstant?: string; registers?: string[] }[];
  };
  const unit = topology.processes.filter((process) => process.service === "gateway-integration");
  assert.equal(unit.length, 1, "the topology must declare exactly one gateway-integration process");
  assert.equal(unit[0].ownerName, "tenant", "the gateway-integration process is the tenant data owner and transport identity");
  for (const group of ["GatewayProductService", "GatewayCoordination", "CloudIdentityAuthorization"]) {
    assert.ok(unit[0].registers?.includes(group), `the gateway-integration process must register ${group}`);
  }
  assert.ok(
    !topology.processes.some((process) => process.ownerName === "gateway" && process.ownerConstant === "OwnerGateway"),
    "no process may declare gateway as its own transport identity",
  );
});

test("every DialOptions target in the Gateway-calling files resolves the deployment unit principal", async () => {
  for (const file of gatewayCallSiteFiles) {
    const source = await readFile(file, "utf8");
    const calls = dialOptionsArguments(source);
    assert.ok(calls.length > 0, `${file} must dial an owner through TLSConfig.DialOptions`);
    for (const { args, line } of calls) {
      assert.equal(args.length, 3, `${file}:${line} must keep the (caller, target, token) shape: ${args.join(" | ")}`);
      assert.match(
        args[1],
        /DeploymentUnitTransportPrincipal\(/,
        `${file}:${line} must resolve the deployment unit principal for target ${args[1]}`,
      );
    }
    assert.ok(
      !source.includes("Gateway.Service()"),
      `${file} must not dial a service identity the deployment unit cannot present`,
    );
  }
});

test("no non-test source dials the Gateway owner outside the deployment unit principal", async () => {
  for (const root of ["services", "apps", "packages"]) {
    for (const file of await goFiles(root)) {
      if (file.includes(`${path.sep}testdata${path.sep}`)) continue;
      const source = await readFile(file, "utf8");
      for (const { args, line } of dialOptionsArguments(source)) {
        if (args.length !== 3 || !args[1].includes("Gateway")) continue;
        assert.match(
          args[1],
          /DeploymentUnitTransportPrincipal\(/,
          `${file}:${line} dials the Gateway owner without resolving the deployment unit principal`,
        );
      }
    }
  }
});
