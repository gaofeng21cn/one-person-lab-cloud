import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import test from "node:test";

// The installer reads every process back after the stack starts. It speaks two
// criteria, and each identity is probed by the one that belongs to it: a data
// owner over its gRPC health service, a deployment unit over the HTTP surface
// it serves.
//
// The Console BFF is a deployment unit. It carries a transport identity, not
// an owner identity, so probing it as an owner was refused before any request
// left the installer, and the installer could never finish. These tests lock
// the rule that keeps that from coming back.

const installScript = fileURLToPath(new URL("../../deploy/portable/install.mjs", import.meta.url));
const ownerIdentity = fileURLToPath(new URL("../../packages/contracts/go/owneridentity/owneridentity.go", import.meta.url));
const transport = fileURLToPath(new URL("../../packages/contracts/go/owneridentity/transport.go", import.meta.url));
const probe = fileURLToPath(new URL("../../packages/contracts/go/cmd/opl-owner-probe/main.go", import.meta.url));

type InstallerProbe = { service: string; identity: string; address: string };

// The canonical owner names are read from the contracts package rather than
// written here, so this test cannot accept an identity the probe would refuse.
async function ownerNames(): Promise<Set<string>> {
  const source = await readFile(ownerIdentity, "utf8");
  return new Set([...source.matchAll(/\bOwner\s*=\s*"([a-z_]+)"/g)].map((match) => match[1]));
}

// The deployment units are the transport principals: the processes that are
// not data owners and therefore have no owner identity to parse.
async function deploymentUnitNames(): Promise<Set<string>> {
  const source = await readFile(transport, "utf8");
  return new Set([...source.matchAll(/\bService\s*=\s*"([a-z_]+)"/g)].map((match) => match[1]));
}

async function installerProbes(): Promise<InstallerProbe[]> {
  const source = await readFile(installScript, "utf8");
  const block = /const probes = \[([\s\S]*?)\];/.exec(source)?.[1] ?? "";
  assert.notEqual(block, "", "the installer declares no owner readback probes");
  return [...block.matchAll(/\[\s*'([^']+)'\s*,\s*'([^']+)'\s*,\s*'([^']+)'\s*\]/g)].map((match) => ({
    service: match[1],
    identity: match[2],
    address: match[3],
  }));
}

// The identities the probe routes to a criterion of their own, read from the
// probe source. An identity that is not routed is parsed as an owner, and an
// owner identity that is not an owner is refused.
async function routedDeploymentUnits(): Promise<Set<string>> {
  const source = await readFile(probe, "utf8");
  const routed = new Set<string>();
  for (const match of source.matchAll(/EqualFold\(ownerText,\s*(\w+)\)/g)) {
    const constant = new RegExp(`\\b${match[1]}\\s*=\\s*"([a-z_]+)"`).exec(source)?.[1];
    if (constant) routed.add(constant);
  }
  return routed;
}

test("the installer probes no identity that the contracts package does not accept as an owner", async () => {
  const owners = await ownerNames();
  const probes = await installerProbes();
  const ownerProbes = probes.filter((entry) => !entry.address.includes("://"));
  assert.ok(ownerProbes.length > 0, "the installer probes no owner over its gRPC health criterion");
  for (const entry of ownerProbes) {
    assert.ok(
      owners.has(entry.identity),
      `${entry.service} is probed as an owner with ${entry.identity}, which owneridentity does not accept`,
    );
  }
});

test("a deployment unit is probed over its own surface, never as an owner", async () => {
  const owners = await ownerNames();
  const units = await deploymentUnitNames();
  const routed = await routedDeploymentUnits();
  const probes = await installerProbes();
  const unitProbes = probes.filter((entry) => entry.address.includes("://"));
  assert.ok(unitProbes.length > 0, "the installer probes no deployment unit over its own criterion");
  for (const entry of unitProbes) {
    assert.ok(
      units.has(entry.identity),
      `${entry.service} is probed as ${entry.identity}, which is not a declared deployment unit`,
    );
    assert.ok(
      !owners.has(entry.identity),
      `${entry.service} is probed as ${entry.identity}, which is a data owner and belongs to the gRPC criterion`,
    );
    assert.ok(
      routed.has(entry.identity),
      `the probe has no criterion of its own for ${entry.identity}, so it would be parsed as an owner`,
    );
  }
});

test("the installer still verifies the Console, through the Console's own criterion", async () => {
  const probes = await installerProbes();
  const console = probes.find((entry) => entry.identity === "console_bff");
  assert.ok(console, "the installer no longer verifies the Console at all");
  assert.ok(
    console.address.includes("://"),
    "the Console is probed over an owner gRPC address, which owneridentity refuses",
  );
  assert.equal(console.service, "console-bff");
});

test("every deployment unit the installer probes is one the probe can answer for", async () => {
  const probes = await installerProbes();
  const routed = await routedDeploymentUnits();
  for (const entry of probes.filter((item) => item.address.includes("://"))) {
    assert.ok(
      routed.has(entry.identity),
      `the probe cannot answer for ${entry.identity}, so the installer would fail on it`,
    );
  }
});
