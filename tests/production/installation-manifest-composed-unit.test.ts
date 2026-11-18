import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  composedContainer,
  composedPolicy,
  composedService,
  composedSharedConfigMap,
  composedUnit,
  workedComposedInstallation
} from "./fixtures/installation.ts";

const cli = fileURLToPath(new URL("../../tools/validate-production-manifest.ts", import.meta.url));
async function invoke(manifest: unknown) {
  const dir = await mkdtemp(join(tmpdir(), "opl-composed-unit-cli-"));
  try {
    const path = join(dir, "rendered.json");
    await writeFile(path, JSON.stringify(manifest));
    const result = spawnSync(process.execPath, [cli, "--installation-manifest", path], { encoding: "utf8", cwd: dir });
    return { ...result, report: result.stdout ? JSON.parse(result.stdout) : undefined };
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

test("the composed deployment unit is admitted and every edge names the unit on both sides", async () => {
  const result = await invoke(workedComposedInstallation());
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.report.ok, true);
  assert.deepEqual(result.report.errors, []);
  const edges = result.report.communication.edges;
  assert.ok(edges.length > 0, "the composed unit proves its communication edges");
  for (const edge of edges) {
    assert.equal(edge.callerDeployment, "opl-cloud-backend", `${edge.caller} -> ${edge.target} caller`);
    assert.equal(edge.targetDeployment, "opl-cloud-backend", `${edge.caller} -> ${edge.target} target`);
  }
  // Every owner the Product declares is a member of the one unit.
  const targets = new Set(edges.map(edge => edge.target));
  for (const principal of ["capability", "build", "runtime_control", "workspace", "serve",
    "resource_catalog", "tenant", "fabric", "ledger"]) assert.ok(targets.has(principal), principal);
});

test("the composed unit resolves a caller's route to the target's own declared listener", async () => {
  const result = await invoke(workedComposedInstallation());
  assert.equal(result.status, 0, result.stderr);
  const edge = result.report.communication.edges.find(candidate =>
    candidate.caller === "capability" && candidate.target === "runtime_control");
  assert.deepEqual(edge, {
    caller: "capability", target: "runtime_control", addressEnv: "OPL_RUNTIME_CONTROL_ADDR", tokenEnv: "OPL_RUNTIME_CONTROL_TOKEN",
    callerDeployment: "opl-cloud-backend", targetDeployment: "opl-cloud-backend", port: 8183,
    tokenRef: { name: "opl-cloud-internal-service", key: "OPL_RUNTIME_CONTROL_TOKEN" },
    peerRef: { name: "opl-cloud-internal-service", key: "OPL_RUNTIME_CONTROL_PEER_TOKENS" },
    serviceRef: { name: "opl-cloud-runtime-control", namespace: "installation" },
    ingressPolicies: ["build-ingress", "capability-ingress", "console-bff-ingress", "control-plane-ingress", "fabric-ingress",
      "ledger-ingress", "resource-catalog-ingress", "runtime-control-ingress", "serve-ingress", "tenant-ingress", "workspace-ingress"],
    egressPolicies: []
  });
  // The Console BFF reaches the same owner over the Service the installation publishes.
  const bff = result.report.communication.edges.find(candidate =>
    candidate.caller === "console_bff" && candidate.target === "capability");
  assert.equal(bff.addressEnv, "OPL_CAPABILITY_URL");
  assert.equal(bff.port, 8181);
  assert.equal(bff.serviceRef.name, "opl-cloud-capability");
  // The Console BFF holds no Fabric route, so the shared Fabric credential alone
  // does not make that edge installed.
  assert.ok(!result.report.communication.edges.some(candidate =>
    candidate.caller === "console_bff" && candidate.target === "fabric"));
});

test("the composed unit refuses a container that does not run the contract's declared entrypoint", async (t) => {
  for (const form of ["wrong-entrypoint", "owner-binary", "extra-argument"]) await t.test(form, async () => {
    const list = workedComposedInstallation();
    const container = composedContainer(list);
    if (form === "wrong-entrypoint") container.command = ["/usr/local/bin/opl-cloud-composition-other"];
    if (form === "owner-binary") container.command = ["/usr/local/bin/opl-capability"];
    if (form === "extra-argument") container.args = ["--serve"];
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "installation_process_unproven"));
    assert.deepEqual(result.report.communication.edges, []);
  });
});

test("the composed unit refuses a container that does not run the product image", async () => {
  const list = workedComposedInstallation();
  composedContainer(list).image = "example.invalid/other@sha256:" + "b".repeat(64);
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_process_unproven"));
});

test("the composed unit refuses any other container that runs the product image", async () => {
  const list = workedComposedInstallation();
  list.items.push({ apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "opl-cloud-extra", namespace: "installation" },
    spec: { selector: { matchLabels: { app: "extra" } }, template: { metadata: { labels: { app: "extra" } }, spec: { containers: [{
      name: "extra", image: "example.invalid/cloud@sha256:" + "a".repeat(64), command: ["/usr/local/bin/opl-cloud-other"]
    }] } } } });
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_process_unproven"));
});

test("the composed unit refuses a manifest that mixes the composed and the per-owner form", async () => {
  const list = workedComposedInstallation();
  list.items.push({ apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "unit-capability", namespace: "installation" },
    spec: { selector: { matchLabels: { unit: "capability" } }, template: { metadata: { labels: { unit: "capability" } }, spec: { containers: [{
      name: "owner", image: "example.invalid/cloud@sha256:" + "a".repeat(64), command: ["/usr/local/bin/opl-capability"],
      ports: [{ name: "rpc", containerPort: 8181, protocol: "TCP" }]
    }] } } } });
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_deployment_unit_mixed"));
  assert.deepEqual(result.report.communication.edges, []);
});

test("the composed unit refuses a unit that does not host every declared member", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  container.ports = container.ports.filter((port: any) => port.containerPort !== 8181);
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_membership" && error.reasonCode === "member_missing"));
  assert.deepEqual(result.report.communication.edges, []);
});

test("the composed unit refuses a unit that hosts a listener no declared member declares", async () => {
  const list = workedComposedInstallation();
  composedContainer(list).ports.push({ name: "undeclared", containerPort: 9999, protocol: "TCP" });
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_membership" && error.reasonCode === "member_undeclared"));
});

test("the composed unit refuses a member listener that no Service routes to", async () => {
  const list = workedComposedInstallation();
  composedService(list, "opl-cloud-capability").spec.ports =
    composedService(list, "opl-cloud-capability").spec.ports.filter((port: any) => port.port !== 8181);
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_membership" && error.reasonCode === "listener_unpublished"));
});

test("the composed unit refuses a Service that points at the wrong container port", async () => {
  const list = workedComposedInstallation();
  const service = composedService(list, "opl-cloud-capability");
  service.spec.ports[0].targetPort = "capability-objects";
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  const reasons = result.report.errors.filter((error: any) => error.code === "installation_unit_membership").map((error: any) => error.reasonCode);
  assert.ok(reasons.includes("listener_unpublished") && reasons.includes("listener_ambiguous"), result.stdout);
});

test("the composed unit refuses a route that does not reach the target's declared listener", async () => {
  const list = workedComposedInstallation();
  composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = ":8184";
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "rpc_target_listener_invalid"), result.stdout);
  assert.deepEqual(result.report.communication.edges, []);
});

test("the composed unit refuses a missing edge credential", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  container.env = container.env.filter((entry: any) => entry.name !== "OPL_CAPABILITY_TOKEN");
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "rpc_token_ref_invalid"), result.stdout);
  assert.deepEqual(result.report.communication.edges, []);
});

test("the composed unit refuses a missing target admission map", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  container.env = container.env.filter((entry: any) => entry.name !== "OPL_LEDGER_PEER_TOKENS");
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "rpc_peer_ref_invalid"), result.stdout);
});

test("the composed unit refuses a missing NetworkPolicy for a member listener", async () => {
  const list = workedComposedInstallation();
  list.items = list.items.filter(item => item.kind !== "NetworkPolicy" || item.metadata.name !== "ledger-ingress");
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "rpc_ingress_denied" || error.code === "rpc_ingress_unisolated"), result.stdout);
});

test("the composed unit refuses one member's admission map under another member's name", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  const build = container.env.find((entry: any) => entry.name === "OPL_BUILD_PEER_TOKENS");
  build.valueFrom.secretKeyRef.key = "OPL_CAPABILITY_PEER_TOKENS";
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_identity_unproven" && error.reasonCode === "peer_token_shared"),
    result.stdout);
});

test("the composed unit refuses two credentials for one owner boundary", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  const identity = container.env.find((entry: any) => entry.name === "OPL_CLOUD_IDENTITY_TOKEN");
  identity.valueFrom.secretKeyRef.key = "OPL_CLOUD_IDENTITY_TOKEN";
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_identity_unproven" && error.reasonCode === "boundary_token_conflict"),
    result.stdout);
});

test("the composed unit refuses a member whose database material is absent", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  container.env = container.env.filter((entry: any) => entry.name !== "DATABASE_URL");
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_identity_unproven" && error.reasonCode === "database_missing"),
    result.stdout);
});

test("the composed unit refuses a member whose own database is the shared one", async () => {
  const list = workedComposedInstallation();
  const container = composedContainer(list);
  const fabric = container.env.find((entry: any) => entry.name === "OPL_FABRIC_DATABASE_URL");
  fabric.valueFrom.secretKeyRef = { name: "opl-cloud-database", key: "DATABASE_URL" };
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_unit_identity_unproven" && error.reasonCode === "database_shared"),
    result.stdout);
});

test("the composed unit still admits a trimmed configured address", async () => {
  const list = workedComposedInstallation();
  composedSharedConfigMap(list).data.OPL_WORKSPACE_URL = "  opl-cloud-workspace:8184  ";
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stderr);
  const edge = result.report.communication.edges.find(candidate =>
    candidate.caller === "console_bff" && candidate.target === "workspace");
  assert.equal(edge.port, 8184);
  assert.equal(edge.serviceRef.name, "opl-cloud-workspace");
});

test("the composed unit refuses a member listener the network policy no longer grants", async () => {
  const list = workedComposedInstallation();
  const policy = composedPolicy(list, "ledger");
  policy.spec.ingress = policy.spec.ingress.filter((rule: any) => rule.ports[0].port !== 8189);
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "rpc_ingress_denied"), result.stdout);
});

test("the composed unit is proved against the unit the contract declares", async () => {
  const list = workedComposedInstallation();
  assert.equal(composedUnit(list).metadata.name, "opl-cloud-backend");
  assert.equal(composedContainer(list).command[0], "/usr/local/bin/opl-cloud-composition");
  assert.equal(composedSharedConfigMap(list).data.OPL_CLOUD_IMAGE, composedContainer(list).image);
});
