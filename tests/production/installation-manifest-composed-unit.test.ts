import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  composedContainer,
  composedEndpointSlice,
  composedEndpoints,
  composedPod,
  composedPolicy,
  composedReplicaSet,
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

// The live workload behind the unit's Services. The Instance's production
// admission reads the cluster's live Pod, ReplicaSet, Endpoint and
// EndpointSlice objects and hands them to the Product inside the manifest it
// admits, so the composed form must prove the same live ownership the
// per-owner form proves for the per-owner Services.
type ComposedFixture = ReturnType<typeof workedComposedInstallation>;

test("the composed unit is admitted with the live workload behind every Service it publishes", async () => {
  const list = workedComposedInstallation();
  // The live workload the cluster runs for the unit: the ReplicaSet and Pod
  // the unit's Deployment owns, and the endpoint objects it publishes.
  assert.equal(composedReplicaSet(list).metadata.ownerReferences[0].uid, composedUnit(list).metadata.uid);
  assert.equal(composedPod(list).metadata.ownerReferences[0].uid, composedReplicaSet(list).metadata.uid);
  assert.equal(composedEndpointSlice(list, "opl-cloud-workspace").metadata.ownerReferences[0].uid,
    composedService(list, "opl-cloud-workspace").metadata.uid);
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.report.ok, true);
  assert.deepEqual(result.report.errors, []);
  assert.equal(result.report.communication.edges.length, 50);
});

test("the composed unit refuses a Service whose live workload is not the unit's own", async (t) => {
  const foreignPod = (list: ComposedFixture) => {
    const pod = structuredClone(composedPod(list));
    pod.metadata.name = "unmanaged-backend-pod";
    pod.metadata.uid = "unmanaged-pod-uid";
    delete pod.metadata.ownerReferences;
    list.items.push(pod);
  };
  const foreignProducer = (kind: string, apiVersion: string) => (list: ComposedFixture) => {
    list.items.push({ apiVersion, kind, metadata: { name: `foreign-${kind.toLowerCase()}`, namespace: "installation" },
      spec: { template: { metadata: { labels: { "app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "backend" } },
        spec: { containers: [{ name: "foreign", image: "example.invalid/foreign:1" }] } } } });
  };
  const cases: [string, string, (fixture: ComposedFixture) => void][] = [
    ["a foreign unmanaged Pod", "endpoint_pod_replicaset_unresolved", foreignPod],
    ["a live Pod the unit no longer owns", "endpoint_pod_replicaset_unresolved", (list) => { composedPod(list).metadata.ownerReferences = []; }],
    ["a foreign Deployment", "service_backend_owner", foreignProducer("Deployment", "apps/v1")],
    ["a foreign StatefulSet", "service_backend_owner", foreignProducer("StatefulSet", "apps/v1")],
    ["a foreign DaemonSet", "service_backend_owner", foreignProducer("DaemonSet", "apps/v1")],
    ["a foreign ReplicaSet", "pod_owner_chain_unproven", foreignProducer("ReplicaSet", "apps/v1")],
    ["a ReplicaSet whose owner was replaced", "pod_owner_chain_unproven", (list) => {
      composedReplicaSet(list).metadata.ownerReferences[0].uid = "replaced-deployment";
    }],
    ["a unit Deployment that was replaced", "pod_owner_chain_unproven", (list) => {
      composedUnit(list).metadata.uid = "replaced-deployment";
    }],
    ["a Pod the Service does not select", "endpoint_pod_selector_mismatch", (list) => {
      composedPod(list).metadata.labels["app.kubernetes.io/component"] = "foreign";
    }],
    ["an unknown workload producer in the unit's namespace", "rpc_service_backend_unproven", (list) => {
      list.items.push({ apiVersion: "example.invalid/v1", kind: "Widget", metadata: { name: "widget", namespace: "installation" } });
    }]
  ];
  for (const [name, reasonCode, change] of cases) await t.test(name, async () => {
    const list = workedComposedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    const codes = reasonCode === "rpc_service_backend_unproven" ? [reasonCode] : [`rpc_service_backend_ambiguous/${reasonCode}`];
    assert.deepEqual([...new Set(result.report.errors.map((error: any) =>
      error.reasonCode ? `${error.code}/${error.reasonCode}` : error.code))], codes, result.stdout);
    assert.deepEqual(result.report.communication.edges, []);
  });
});

test("the composed unit refuses a published endpoint that does not name the unit's own Pod", async (t) => {
  const cases: [string, string, (fixture: ComposedFixture) => void][] = [
    ["an EndpointSlice whose target Pod was replaced", "endpoint_pod_absent", (list) => {
      composedEndpointSlice(list, "opl-cloud-workspace").endpoints[0].targetRef.uid = "replaced-pod";
    }],
    ["a legacy Endpoints whose target Pod was replaced", "endpoint_pod_absent", (list) => {
      composedEndpoints(list, "opl-cloud-control-plane").subsets[0].addresses[0].targetRef.uid = "replaced-pod";
    }],
    ["an EndpointSlice that publishes a foreign address", "endpoint_address_mismatch", (list) => {
      composedEndpointSlice(list, "opl-cloud-workspace").endpoints[0].addresses[0] = "192.0.2.99";
    }],
    ["a live Pod whose command drifted from the unit's", "endpoint_pod_command_mismatch", (list) => {
      composedPod(list).spec.containers[0].command = ["/bin/sh", "-c", "exec sleep 3600"];
    }],
    ["a live sidecar that claims the published port", "endpoint_pod_port_conflict", (list) => {
      composedPod(list).spec.containers.push({ name: "live-sidecar", image: "example.invalid/sidecar:1", ports: [{ containerPort: 8184 }] });
    }],
    ["an EndpointSlice that publishes a port the Service does not publish", "endpoint_port_mismatch", (list) => {
      composedEndpointSlice(list, "opl-cloud-workspace").ports[0].port = 9999;
    }]
  ];
  for (const [name, reasonCode, change] of cases) await t.test(name, async () => {
    const list = workedComposedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.deepEqual([...new Set(result.report.errors.map((error: any) => `${error.code}/${error.reasonCode ?? ""}`))],
      [`rpc_service_backend_ambiguous/${reasonCode}`], result.stdout);
    assert.deepEqual(result.report.communication.edges, []);
  });
});

test("the composed unit names the Service whose live workload it could not prove", async () => {
  const list = workedComposedInstallation();
  composedEndpointSlice(list, "opl-cloud-workspace").endpoints[0].targetRef.uid = "replaced-pod";
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  const error = result.report.errors.find((candidate: any) => candidate.code === "rpc_service_backend_ambiguous");
  assert.equal(error.service, "opl-cloud-workspace");
  assert.equal(error.reasonCode, "endpoint_pod_absent");
});

test("the composed unit admits an endpoint object that lists no address", async (t) => {
  // An EndpointSlice or Endpoints object that lists no address delivers no
  // traffic, so it cannot hand the Service to another workload and it proves
  // nothing about the unit. The Deploy that creates the backends is the only
  // repair for a Service that has none.
  const cases: [string, (fixture: ComposedFixture) => void][] = [
    ["an EndpointSlice with no endpoint", (list) => { composedEndpointSlice(list, "opl-cloud-workspace").endpoints = []; }],
    ["an EndpointSlice with no endpoint list", (list) => { delete composedEndpointSlice(list, "opl-cloud-workspace").endpoints; }],
    ["a legacy Endpoints with no subset", (list) => { composedEndpoints(list, "opl-cloud-control-plane").subsets = []; }],
    ["a legacy Endpoints with no address", (list) => { composedEndpoints(list, "opl-cloud-control-plane").subsets[0].addresses = []; }]
  ];
  for (const [name, change] of cases) await t.test(name, async () => {
    const list = workedComposedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.equal(result.report.ok, true);
    assert.deepEqual(result.report.errors, []);
    assert.equal(result.report.communication.edges.length, 50);
  });
});
