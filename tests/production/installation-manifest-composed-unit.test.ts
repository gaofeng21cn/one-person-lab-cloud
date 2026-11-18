import assert from "node:assert/strict";
import test from "node:test";
import { validateInstallationManifest, type InstallationTopology } from "../../tools/ops/installation-manifest.ts";

import {
  composedContainer,
  composedTopology,
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

// Exercise the real untrusted-manifest decoder with #1247's explicit test
// topology. The public CLI's on-disk contract integration remains a separate
// check after the canonical contract worker is merged.
async function invoke(manifest: unknown, topology: InstallationTopology = composedTopology()) {
  const report = validateInstallationManifest(JSON.parse(JSON.stringify(manifest)), topology);
  return { status: report.ok ? 0 : 1, report, stdout: JSON.stringify(report), stderr: "" };
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
    bindAddress: { host: "", port: 8183 },
    ingressPolicies: ["build-ingress", "capability-ingress", "console-bff-ingress", "control-plane-ingress", "fabric-ingress",
      "ledger-ingress", "resource-catalog-ingress", "runtime-control-ingress", "serve-ingress", "tenant-ingress", "workspace-ingress"],
    egressPolicies: []
  });
  // The Console BFF reaches the same owner over the Service the installation publishes.
  const bff = result.report.communication.edges.find(candidate =>
    candidate.caller === "console_bff" && candidate.target === "capability");
  assert.ok(bff?.serviceRef);
  assert.equal(bff.addressEnv, "OPL_CAPABILITY_URL");
  assert.equal(bff.port, 8181);
  assert.equal(bff.serviceRef.name, "opl-cloud-capability");
  assert.ok(result.report.communication.edges.some(candidate =>
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
  assert.ok(reasons.includes("listener_unpublished"), result.stdout);
});

test("the composed unit refuses a route that does not reach the target's declared listener", async () => {
  const list = workedComposedInstallation();
  composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = ":8184";
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_listener_conflict"), result.stdout);
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

test("the composed unit leaves equality of distinct token references to Instance material validation", async () => {
  const list = workedComposedInstallation();
  const identity = composedContainer(list).env.find((entry: any) => entry.name === "OPL_CLOUD_IDENTITY_TOKEN");
  identity.valueFrom.secretKeyRef = { name: "another-token-secret", key: "identity-token-copy" };
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stdout);
  assert.deepEqual(result.report.communication.edges.find(edge => edge.tokenEnv === "OPL_CLOUD_IDENTITY_TOKEN")?.tokenRef,
    { name: "another-token-secret", key: "identity-token-copy" });
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
  assert.ok(edge?.serviceRef);
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
  assert.equal(result.report.communication.edges.length, 51);
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
  assert.ok(error);
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
    assert.equal(result.report.communication.edges.length, 51);
  });
});

function moveListener(list: ComposedFixture, oldPort: number, newPort: number) {
  // Keep the declaration and captured live workload coherent for acceptance
  // cases, rather than weakening endpoint readback assertions.
  for (const resource of list.items) {
    const spec = resource.kind === "Pod" ? resource.spec : resource.spec?.template?.spec;
    for (const container of spec?.containers ?? []) for (const port of container.ports ?? [])
      if (port.containerPort === oldPort) port.containerPort = newPort;
    if (resource.kind === "Service") for (const port of resource.spec.ports)
      if (port.targetPort === oldPort) port.targetPort = newPort;
    if (resource.kind === "EndpointSlice") for (const port of resource.ports)
      if (port.port === oldPort) port.port = newPort;
    if (resource.kind === "Endpoints") for (const subset of resource.subsets ?? []) for (const port of subset.ports ?? [])
      if (port.port === oldPort) port.port = newPort;
    if (resource.kind === "NetworkPolicy") for (const rule of resource.spec.ingress ?? []) for (const port of rule.ports ?? [])
      if (port.port === oldPort) port.port = newPort;
  }
}

function assertRejected(result: Awaited<ReturnType<typeof invoke>>, code: string, reasonCode?: string) {
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === code && (!reasonCode || error.reasonCode === reasonCode)), result.stdout);
  assert.deepEqual(result.report.communication.edges, []);
}

test("effective listener overrides reject stale ports and admit coherent published ports", async t => {
  const cases = [
    ["OPL_BFF_ADDR", 8190, 9999],
    ["OPL_CAPABILITY_OBJECT_LISTEN_ADDR", 8191, 9998],
    ["OPL_SERVE_ACCESS_ADDR", 8192, 9997],
    ["OPL_FABRIC_ADDR", 8188, 9996],
    ["OPL_RUNTIME_CONTROL_ADDR", 8183, 9995]
  ] as const;
  for (const [name, oldPort, newPort] of cases) await t.test(name, async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data[name] = `:${newPort}`;
    assertRejected(await invoke(list), "installation_unit_membership", "member_missing");
    moveListener(list, oldPort, newPort);
    if (name === "OPL_FABRIC_ADDR") composedSharedConfigMap(list).data.OPL_FABRIC_COORDINATION_ADDR = `:${newPort}`;
    const result = await invoke(list);
    assert.equal(result.status, 0, result.stdout);
    if (name === "OPL_RUNTIME_CONTROL_ADDR") assert.ok(result.report.communication.edges.some(edge =>
      edge.target === "runtime_control" && edge.port === newPort));
  });
});

test("effective listeners honor PORT, defaults, value semantics and unresolved configuration", async t => {
  await t.test("PORT supplies an unset primary HTTP address", async () => {
    const list = workedComposedInstallation();
    const data = composedSharedConfigMap(list).data;
    delete data.CONTROL_PLANE_ADDR;
    data.PORT = "9898";
    assertRejected(await invoke(list), "installation_unit_membership", "member_missing");
    moveListener(list, 8787, 9898);
    assert.equal((await invoke(list)).status, 0);
  });
  await t.test("an explicit address takes precedence over PORT", async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data.PORT = "9999";
    assert.equal((await invoke(list)).status, 0);
  });
  await t.test("unset HTTP listeners use the declared defaults", async () => {
    const list = workedComposedInstallation();
    delete composedSharedConfigMap(list).data.OPL_BFF_ADDR;
    assert.equal((await invoke(list)).status, 0);
  });
  for (const [name, value, accepted] of [
    ["OPL_RUNTIME_CONTROL_ADDR", "\u0085:8183\u0085", true],
    ["OPL_BFF_ADDR", " :8190 ", false],
    ["OPL_CAPABILITY_OBJECT_LISTEN_ADDR", " :8191 ", false],
    ["OPL_SERVE_ACCESS_ADDR", " :8192 ", true],
    ["OPL_RUNTIME_CONTROL_ADDR", "\uFEFF:8183", false]
  ] as const) await t.test(`${name} ${JSON.stringify(value)}`, async () => {
    const list = workedComposedInstallation();
    const topology = composedTopology();
    if (name === "OPL_RUNTIME_CONTROL_ADDR" && accepted) {
      topology.processes.find(process => process.service === "runtime-control")!.addressEnv = "TEST_RUNTIME_LISTENER";
      composedSharedConfigMap(list).data.TEST_RUNTIME_LISTENER = value;
    } else composedSharedConfigMap(list).data[name] = value;
    const result = await invoke(list, topology);
    if (accepted) assert.equal(result.status, 0, result.stdout);
    else assertRejected(result, "installation_listener_unproven");
  });
  await t.test("a Secret listener cannot be replaced with its default", async () => {
    const list = workedComposedInstallation();
    composedContainer(list).env.push({ name: "OPL_BFF_ADDR", valueFrom: { secretKeyRef: { name: "listen", key: "address" } } });
    assertRejected(await invoke(list), "installation_listener_unproven");
  });
});

test("gRPC edges refuse HTTP and additional listeners even when a Service publishes them", async t => {
  for (const [env, value] of [
    ["OPL_CAPABILITY_URL", "opl-cloud-capability:8281"],
    ["OPL_FABRIC_URL", "opl-cloud-fabric:8082"],
    ["OPL_LEDGER_URL", "opl-cloud-ledger:8081"],
    ["OPL_SERVE_URL", "opl-cloud-serve:8188"]
  ]) await t.test(env, async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data[env] = value;
    assertRejected(await invoke(list), "rpc_target_listener_invalid");
  });
});

test("bind addresses retain and prove their host in the unit network namespace", async t => {
  for (const address of ["192.0.2.200:8183", "999.999.999.999:8183", "[2001:db8::ffff]:8183", "[gg::1]:8183"]) {
    await t.test(`unowned or malformed listener ${address}`, async () => {
      const list = workedComposedInstallation();
      composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = address;
      assertRejected(await invoke(list), "installation_listener_unproven");
    });
    await t.test(`unowned or malformed caller ${address}`, async () => {
      const list = workedComposedInstallation();
      composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_URL = address;
      assertRejected(await invoke(list), "rpc_address_invalid");
    });
  }
  for (const address of [":8183", "127.0.0.1:8183", "[::1]:8183", "192.0.2.1:8183", "[2001:db8::1]:8183"]) {
    await t.test(`local caller ${address}`, async () => {
      const list = workedComposedInstallation();
      composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_URL = address;
      const result = await invoke(list);
      assert.equal(result.status, 0, result.stdout);
      const edge = result.report.communication.edges.find(edge => edge.caller === "console_bff" && edge.target === "runtime_control")!;
      assert.equal(edge.port, 8183);
      assert.ok(edge.bindAddress);
      assert.equal(edge.serviceRef, undefined);
    });
  }
  await t.test("a loopback-only listener cannot be reached through a Service", async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = "127.0.0.1:8183";
    assertRejected(await invoke(list), "rpc_target_listener_invalid");
    composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_URL = "127.0.0.1:8183";
    assert.equal((await invoke(list)).status, 0);
  });
  await t.test("a different loopback host does not match a concrete bind", async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = "127.0.0.2:8183";
    composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_URL = "127.0.0.1:8183";
    assertRejected(await invoke(list), "rpc_target_listener_invalid");
  });
  await t.test("a local Pod address needs its exact controller ownership", async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = "192.0.2.1:8183";
    assert.equal((await invoke(list)).status, 0);
    composedPod(list).metadata.ownerReferences[0].uid = "foreign-rs";
    assertRejected(await invoke(list), "installation_listener_unproven");
  });
});

test("configured edges preserve canonical token and activationEnvs activation", async t => {
  for (const missingAddress of [undefined, "", " \u0085 "]) await t.test(`token only ${JSON.stringify(missingAddress)}`, async () => {
    const list = workedComposedInstallation();
    if (missingAddress === undefined) delete composedSharedConfigMap(list).data.OPL_WORKSPACE_URL;
    else composedSharedConfigMap(list).data.OPL_WORKSPACE_URL = missingAddress;
    const result = await invoke(list);
    assertRejected(result, "rpc_address_required");
    assert.ok(result.report.errors.some(error => error.caller === "console_bff" && error.target === "workspace"), result.stdout);
  });
  for (const value of ["enabled", { secretKeyRef: { name: "activation", key: "enabled" } }]) await t.test(`activationEnvs ${JSON.stringify(value)}`, async () => {
    const list = workedComposedInstallation();
    const topology = composedTopology();
    const edge = topology.communication.edges.find(edge => edge.caller === "control_plane" && edge.target === "tenant")!;
    // Isolate the activationEnvs branch from other callers' required identity
    // edge without changing those callers' material.
    edge.addressEnv = "TEST_CONTROL_PLANE_IDENTITY_ADDR";
    edge.tokenEnv = "TEST_CONTROL_PLANE_IDENTITY_TOKEN";
    composedContainer(list).env.push({ name: edge.activationEnvs![0], ...(typeof value === "string" ? { value } : { valueFrom: value }) });
    const result = await invoke(list, topology);
    assertRejected(result, "rpc_address_required");
    assert.ok(result.report.errors.some(error => error.caller === "control_plane" && error.addressEnv === edge.addressEnv));
  });
  await t.test("an edge is inactive only when all its activation inputs are empty", async () => {
    const list = workedComposedInstallation();
    const topology = composedTopology();
    const edge = topology.communication.edges.find(edge => edge.caller === "control_plane" && edge.target === "tenant")!;
    edge.addressEnv = "TEST_INACTIVE_ADDRESS";
    edge.tokenEnv = "TEST_INACTIVE_TOKEN";
    composedContainer(list).env.push({ name: edge.activationEnvs![0], value: "\u0085 " });
    const result = await invoke(list, topology);
    assert.equal(result.status, 0, result.stdout);
    assert.ok(!result.report.communication.edges.some(edge => edge.caller === "control_plane" && edge.target === "tenant"));
  });
});

test("database admission consumes only the declared composition projection and RPC database", async t => {
  for (const owner of ["CAPABILITY", "BUILD", "RUNTIME_CONTROL", "WORKSPACE", "SERVE", "RESOURCE_CATALOG", "TENANT"]) {
    await t.test(`missing actual ${owner} alias`, async () => {
      const list = workedComposedInstallation();
      composedContainer(list).env = composedContainer(list).env.filter((entry: any) => entry.name !== `OPL_${owner}_DATABASE_URL`);
      assertRejected(await invoke(list), "installation_unit_identity_unproven", "database_missing");
    });
  }
  await t.test("two projected owner databases cannot reuse the same material reference", async () => {
    const list = workedComposedInstallation();
    const entries = composedContainer(list).env;
    entries.find((entry: any) => entry.name === "OPL_BUILD_DATABASE_URL").valueFrom =
      structuredClone(entries.find((entry: any) => entry.name === "OPL_CAPABILITY_DATABASE_URL").valueFrom);
    assertRejected(await invoke(list), "installation_unit_identity_unproven", "database_shared");
  });
  await t.test("a declared alias is required even when the shared parent database exists", async () => {
    const list = workedComposedInstallation();
    const topology = composedTopology();
    topology.processes.find(process => process.service === "build")!.compositionDatabaseEnv = "TEST_BUILD_DATABASE";
    assertRejected(await invoke(list, topology), "installation_unit_identity_unproven", "database_missing");
    composedContainer(list).env.push({ name: "TEST_BUILD_DATABASE", valueFrom: { secretKeyRef: { name: "build-database", key: "dsn" } } });
    assert.equal((await invoke(list, topology)).status, 0);
  });
  await t.test("complete explicit aliases need no unused shared parent DATABASE_URL", async () => {
    const list = workedComposedInstallation();
    const topology = composedTopology();
    for (const process of topology.processes.filter(process => process.compositionDatabaseEnv === "DATABASE_URL")) {
      process.compositionDatabaseEnv = `TEST_${process.service.replaceAll("-", "_")}_DATABASE`;
      composedContainer(list).env.push({ name: process.compositionDatabaseEnv, valueFrom: { secretKeyRef: { name: "legacy-databases", key: process.service } } });
    }
    composedContainer(list).env = composedContainer(list).env.filter((entry: any) => entry.name !== "DATABASE_URL");
    assert.equal((await invoke(list, topology)).status, 0);
  });
  for (const field of [undefined, null, "", 123, {}, []]) await t.test(`missing or malformed mapping ${JSON.stringify(field)}`, async () => {
    const topology = composedTopology();
    (topology.processes[0] as any).compositionDatabaseEnv = field;
    assertRejected(await invoke(workedComposedInstallation(), topology), "installation_topology_process_invalid");
  });
  await t.test("Fabric RPC database is distinct from its legacy database and other projections", async () => {
    const list = workedComposedInstallation();
    const entries = composedContainer(list).env;
    entries.find((entry: any) => entry.name === "OPL_FABRIC_DATABASE_URL").valueFrom =
      structuredClone(entries.find((entry: any) => entry.name === "OPL_BUILD_DATABASE_URL").valueFrom);
    assertRejected(await invoke(list), "installation_unit_identity_unproven", "database_shared");
  });
});

test("all init containers exclude additional owner and composition execution", async t => {
  for (const location of ["Deployment", "ReplicaSet", "Pod"]) for (const restartPolicy of [undefined, "Always"]) {
    for (const form of ["composition", "owner", "alternate-image-composition", "alternate-image-owner"]) {
      await t.test(`${location} ${restartPolicy ?? "ordinary"} ${form}`, async () => {
        const list = workedComposedInstallation();
        const extra = structuredClone(composedContainer(list));
        extra.name = "second-product";
        if (form.includes("owner")) extra.command = ["/usr/local/bin/opl-capability"];
        if (form.startsWith("alternate")) extra.image = "example.invalid/another-build:1";
        if (restartPolicy) extra.restartPolicy = restartPolicy;
        const resource = list.items.find(item => item.kind === location);
        const spec = location === "Pod" ? resource.spec : resource.spec.template.spec;
        spec.initContainers = [extra];
        assertRejected(await invoke(list), form.includes("owner") ? "installation_deployment_unit_mixed" : "installation_process_unproven");
      });
    }
  }
  await t.test("an unrelated preparation image does not run a product process", async () => {
    const list = workedComposedInstallation();
    composedUnit(list).spec.template.spec.initContainers = [{ name: "prepare", image: "example.invalid/prepare:1", command: ["/bin/prepare"] }];
    assert.equal((await invoke(list)).status, 0);
  });
});

test("Service aliases resolve per edge without global listener cardinality restrictions", async t => {
  for (const useAlias of [false, true]) await t.test(`named edge ${useAlias ? "alias" : "original"}`, async () => {
    const list = workedComposedInstallation();
    const alias = structuredClone(composedService(list, "opl-cloud-workspace"));
    alias.metadata.name = "workspace-extra-alias";
    alias.metadata.uid = "workspace-alias-uid";
    list.items.push(alias);
    if (useAlias) composedSharedConfigMap(list).data.OPL_WORKSPACE_URL = "workspace-extra-alias:8184";
    const result = await invoke(list);
    assert.equal(result.status, 0, result.stdout);
    assert.equal(result.report.communication.edges.length, 51);
    const edge = result.report.communication.edges.find(edge => edge.caller === "console_bff" && edge.target === "workspace")!;
    assert.equal(edge.serviceRef?.name, useAlias ? "workspace-extra-alias" : "opl-cloud-workspace");
    const localEdge = result.report.communication.edges.find(edge => edge.caller === "serve" && edge.target === "workspace")!;
    assert.deepEqual(localEdge.bindAddress, { host: "", port: 8184 });
    assert.equal(localEdge.serviceRef, undefined);
  });
  await t.test("ambiguous ports on the Service actually selected by the edge are rejected", async () => {
    const list = workedComposedInstallation();
    const service = composedService(list, "opl-cloud-workspace");
    service.spec.ports.push({ ...service.spec.ports[0], name: "duplicate-rpc" });
    composedEndpointSlice(list, "opl-cloud-workspace").ports.push({ name: "duplicate-rpc", port: 8184, protocol: "TCP" });
    assertRejected(await invoke(list), "rpc_service_port_invalid");
  });
});


test("gRPC target decoding refuses HTTP origins and resolver authorities", async t => {
  for (const address of ["http://opl-cloud-capability:8181", "https://opl-cloud-capability:8181",
    "dns://foreign/opl-cloud-capability:8181", "opl-cloud-capability:8181/path"]) await t.test(address, async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data.OPL_CAPABILITY_URL = address;
    assertRejected(await invoke(list), "rpc_address_invalid");
  });
  const list = workedComposedInstallation();
  composedSharedConfigMap(list).data.OPL_CAPABILITY_URL = "dns:///opl-cloud-capability.installation.svc.cluster.local:8181";
  assert.equal((await invoke(list)).status, 0);
});

test("direct bind reachability respects an IPv4-only wildcard listener", async () => {
  const list = workedComposedInstallation();
  composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = "0.0.0.0:8183";
  composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_URL = "[::1]:8183";
  assertRejected(await invoke(list), "rpc_target_listener_invalid");
  composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_URL = "127.0.0.1:8183";
  assert.equal((await invoke(list)).status, 0);
});


test("IPv6-only listeners require matching direct and Service address families", async () => {
  const list = workedComposedInstallation();
  const data = composedSharedConfigMap(list).data;
  data.OPL_RUNTIME_CONTROL_ADDR = "[::]:8183";
  data.OPL_RUNTIME_CONTROL_URL = "127.0.0.1:8183";
  assertRejected(await invoke(list), "rpc_target_listener_invalid");
  data.OPL_RUNTIME_CONTROL_URL = "[::1]:8183";
  assert.equal((await invoke(list)).status, 0);
  data.OPL_RUNTIME_CONTROL_URL = "opl-cloud-runtime-control:8183";
  assertRejected(await invoke(list), "rpc_target_listener_invalid");
  composedService(list, "opl-cloud-runtime-control").spec.ipFamilies = ["IPv6"];
  assert.equal((await invoke(list)).status, 0);
  composedService(list, "opl-cloud-runtime-control").spec.ipFamilies = ["IPv4", "IPv6"];
  assertRejected(await invoke(list), "rpc_target_listener_invalid");
});

test("PodIP reachability requires the current ReplicaSet template and actual Pod listener configuration", async t => {
  for (const form of ["stale-revision", "drifted-live-listener", "drifted-live-image", "host-network"]) await t.test(form, async () => {
    const list = workedComposedInstallation();
    composedSharedConfigMap(list).data.OPL_RUNTIME_CONTROL_ADDR = "192.0.2.1:8183";
    assert.equal((await invoke(list)).status, 0);
    if (form === "stale-revision") composedReplicaSet(list).spec.template.spec.containers[0].image = "example.invalid/old:1";
    if (form === "drifted-live-listener") composedPod(list).spec.containers[0].env.push({ name: "OPL_RUNTIME_CONTROL_ADDR", value: "127.0.0.1:9999" });
    if (form === "drifted-live-image") composedPod(list).spec.containers[0].image = "example.invalid/old:1";
    if (form === "host-network") composedPod(list).spec.hostNetwork = true;
    assertRejected(await invoke(list), "installation_listener_unproven");
  });
});
