import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { container, deployment, policy, workedInstallation } from "./fixtures/installation.ts";

const cli = fileURLToPath(new URL("../../tools/validate-production-manifest.ts", import.meta.url));
async function invoke(manifest: unknown, executable = cli) {
  const dir = await mkdtemp(join(tmpdir(), "opl-installation-cli-"));
  try {
    const path = join(dir, "rendered.json");
    await writeFile(path, typeof manifest === "string" ? manifest : JSON.stringify(manifest));
    const result = spawnSync(process.execPath, [executable, "--installation-manifest", path], { encoding: "utf8", cwd: dir });
    return { ...result, report: result.stdout ? JSON.parse(result.stdout) : undefined };
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

test("installation CLI returns executable Serve and BFF RPC edges from a worked Kubernetes List", async () => {
  const result = await invoke(workedInstallation());
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.report.ok, true);
  assert.deepEqual(result.report.errors, []);
  assert.deepEqual(result.report.communication.edges.find(edge => edge.caller === "serve" && edge.target === "workspace"), {
    caller: "serve", target: "workspace", addressEnv: "OPL_WORKSPACE_ADDR", tokenEnv: "OPL_WORKSPACE_TOKEN",
    callerDeployment: "unit-serve", targetDeployment: "unit-workspace", port: 8184,
    tokenRef: { name: "serve-auth", key: "OPL_WORKSPACE_TOKEN" }, peerRef: { name: "workspace-admission", key: "peers" },
    serviceRef: { name: "workspace-rpc", namespace: "installation" }, ingressPolicies: ["workspace-rpc"], egressPolicies: []
  });
  assert.ok(result.report.communication.edges.some(edge => edge.caller === "console_bff" && edge.target === "workspace" && edge.addressEnv === "OPL_WORKSPACE_URL"));
  assert.ok(result.report.communication.edges.some(edge => edge.caller === "console_bff" && edge.target === "runtime_control"));
});

test("installation CLI refuses one-shot commands as service processes", async t => {
  for (const form of ["args", "command", "unique-image"]) await t.test(form, async () => {
    const list = workedInstallation();
    const serve = container(list, "serve");
    if (form === "command") serve.command.push("probe-workspace-access");
    else serve.args = ["probe-workspace-access"];
    if (form === "unique-image") serve.image = "example.invalid/alternate@sha256:" + "b".repeat(64);
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "installation_process_unproven"));
    assert.deepEqual(result.report.communication.edges, []);
  });
  const list = workedInstallation();
  container(list, "serve").args = [];
  assert.equal((await invoke(list)).status, 0, "empty args retain the service entrypoint");
});

test("installation CLI rejects competing listeners in one Pod network namespace", async t => {
  for (const form of ["numeric-target", "different-port-name", "product-listener-without-ports", "restartable-init-container"]) await t.test(form, async () => {
    const list = workedInstallation();
    const pod = deployment(list, "workspace").spec.template.spec;
    const sidecar: any = { name: "other-listener", image: "example.invalid/sidecar:1", ports: [{ name: "other", containerPort: 8184 }] };
    if (form === "numeric-target") list.items.find(item => item.kind === "Service" && item.metadata.name === "workspace-rpc").spec.ports[0].targetPort = 8184;
    if (form === "product-listener-without-ports") {
      sidecar.image = container(list, "workspace").image;
      sidecar.command = ["/usr/local/bin/opl-control-plane"];
      sidecar.env = [{ name: "CONTROL_PLANE_ADDR", value: ":8184" }];
      delete sidecar.ports;
    }
    if (form === "restartable-init-container") pod.initContainers = [{ ...sidecar, restartPolicy: "Always" }];
    else pod.containers.push(sidecar);
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => ["rpc_service_backend_ambiguous", "rpc_target_port_invalid", "installation_listener_conflict"].includes(error.code)));
    assert.deepEqual(result.report.communication.edges, []);
  });
  const list = workedInstallation();
  deployment(list, "workspace").spec.template.spec.containers.push({ name: "non-conflicting", image: "example.invalid/sidecar:1",
    ports: [{ name: "udp", containerPort: 8184, protocol: "UDP" }, { name: "https", containerPort: 9443 }] });
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stdout);
  const dualListener = workedInstallation();
  container(dualListener, "fabric").env.push({ name: "FABRIC_ADDR", value: ":8188" });
  const conflict = await invoke(dualListener);
  assert.equal(conflict.status, 1, conflict.stdout);
  assert.ok(conflict.report.errors.some(error => ["rpc_service_backend_ambiguous", "rpc_target_port_invalid", "installation_listener_conflict"].includes(error.code)));
});

test("installation CLI rejects other declared Service backend producers", async t => {
  for (const kind of ["Pod", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job", "CronJob", "Endpoints", "EndpointSlice"]) await t.test(kind, async () => {
    const list = workedInstallation();
    const template = structuredClone(deployment(list, "workspace").spec.template);
    template.spec.containers = [{ name: "foreign", image: "example.invalid/foreign:1" }];
    const metadata = { name: "foreign-backend", namespace: "installation" };
    if (kind === "Pod") list.items.push({ apiVersion: "v1", kind, metadata: { ...metadata, labels: template.metadata.labels }, spec: template.spec });
    else if (kind === "Endpoints") list.items.push({ apiVersion: "v1", kind, metadata: { ...metadata, name: "workspace-rpc" }, subsets: [{ addresses: [{ ip: "192.0.2.1" }], ports: [{ port: 8184 }] }] });
    else if (kind === "EndpointSlice") list.items.push({ apiVersion: "discovery.k8s.io/v1", kind, metadata: { ...metadata, labels: { "kubernetes.io/service-name": "workspace-rpc" } }, addressType: "IPv4", endpoints: [{ addresses: ["192.0.2.1"] }], ports: [{ port: 8184 }] });
    else list.items.push({ apiVersion: ["Job", "CronJob"].includes(kind) ? "batch/v1" : kind === "ReplicationController" ? "v1" : "apps/v1", kind, metadata,
      spec: kind === "CronJob" ? { schedule: "* * * * *", jobTemplate: { spec: { template } } } : { template } });
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
    assert.deepEqual(result.report.communication.edges, []);
  });
  const list = workedInstallation();
  list.items.push({ apiVersion: "v1", kind: "Pod", metadata: { name: "unrelated", namespace: "installation", labels: { unit: "other" } }, spec: { containers: [{ name: "foreign", image: "example.invalid/foreign:1" }] } });
  list.items.push({ apiVersion: "apps/v1", kind: "StatefulSet", metadata: { name: "other-namespace", namespace: "other" }, spec: { template: structuredClone(deployment(list, "workspace").spec.template) } });
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stdout);
});

function withLiveWorkspaceBackends() {
  const list = workedInstallation();
  const target = deployment(list, "workspace");
  target.metadata.uid = "workspace-deployment-uid";
  const reference = (resource: any) => ({ apiVersion: resource.apiVersion, kind: resource.kind, name: resource.metadata.name, uid: resource.metadata.uid, controller: true });
  const replicaSet = { apiVersion: "apps/v1", kind: "ReplicaSet", metadata: { name: "workspace-revision", namespace: "installation", uid: "workspace-replicaset-uid", ownerReferences: [reference(target)] }, spec: { template: structuredClone(target.spec.template) } };
  const pod = { apiVersion: "v1", kind: "Pod", metadata: { ...structuredClone(target.spec.template.metadata), name: "workspace-pod", namespace: "installation", uid: "workspace-pod-uid", ownerReferences: [reference(replicaSet)] }, spec: structuredClone(target.spec.template.spec), status: { podIP: "192.0.2.1", podIPs: [{ ip: "192.0.2.1" }, { ip: "2001:db8::1" }] } };
  const service = list.items.find(item => item.kind === "Service" && item.metadata.name === "workspace-rpc");
  service.metadata.uid = "workspace-service-uid";
  const targetRef = { apiVersion: "v1", kind: "Pod", name: pod.metadata.name, namespace: "installation", uid: pod.metadata.uid };
  const endpoints = { apiVersion: "v1", kind: "Endpoints", metadata: { name: service.metadata.name, namespace: "installation" }, subsets: [{ addresses: [{ ip: "192.0.2.1", targetRef }], ports: [{ name: "rpc", port: 8184 }] }] };
  const slice = { apiVersion: "discovery.k8s.io/v1", kind: "EndpointSlice", metadata: { name: "workspace-slice", namespace: "installation", labels: { "kubernetes.io/service-name": service.metadata.name }, ownerReferences: [reference(service)] }, addressType: "IPv6", endpoints: [{ addresses: ["2001:db8::1"], targetRef }], ports: [{ name: "rpc", port: 8184 }] };
  list.items.push(replicaSet, pod, endpoints, slice);
  return { list, target, replicaSet, pod, service, endpoints, slice };
}

test("installation CLI accepts live replicas only through exact controller and endpoint identities", async t => {
  const live = withLiveWorkspaceBackends();
  const success = await invoke(live.list);
  assert.equal(success.status, 0, success.stdout);
  assert.equal(success.report.communication.edges.length, 46);
  const cases: [string, (fixture: ReturnType<typeof withLiveWorkspaceBackends>) => void][] = [
    ["stale Deployment UID", ({ target }) => { target.metadata.uid = "replaced-deployment"; }],
    ["stale ReplicaSet UID", ({ replicaSet }) => { replicaSet.metadata.uid = "replaced-replicaset"; }],
    ["unmanaged Pod", ({ pod }) => { pod.metadata.ownerReferences = []; }],
    ["non-controller reference", ({ pod }) => { pod.metadata.ownerReferences[0].controller = false; }],
    ["conflicting controller references", ({ pod }) => { pod.metadata.ownerReferences.push(structuredClone(pod.metadata.ownerReferences[0])); }],
    ["foreign ReplicaSet", ({ replicaSet }) => { replicaSet.metadata.ownerReferences[0].name = "other-deployment"; }],
    ["unselected endpoint Pod", ({ pod }) => { pod.metadata.labels.unit = "foreign"; }],
    ["stale endpoint Pod UID", ({ endpoints }) => { endpoints.subsets[0].addresses[0].targetRef.uid = "replaced-pod"; }],
    ["wrong endpoint namespace", ({ endpoints }) => { endpoints.subsets[0].addresses[0].targetRef.namespace = "other"; }],
    ["forged endpoint address", ({ endpoints }) => { endpoints.subsets[0].addresses[0].ip = "192.0.2.99"; }],
    ["invalid endpoint address", ({ endpoints }) => { endpoints.subsets[0].addresses[0].ip = "not-an-ip"; }],
    ["forged slice address", ({ slice }) => { slice.endpoints[0].addresses[0] = "2001:db8::99"; }],
    ["wrong slice address family", ({ slice }) => { slice.addressType = "IPv4"; }],
    ["malformed Pod IP list", ({ pod }) => { pod.status.podIPs = [null]; }],
    ["contradictory Pod IP list", ({ pod }) => { pod.status.podIP = "192.0.2.99"; }],
    ["drifted live target container", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").image = "example.invalid/foreign:1"; }],
    ["drifted live target command", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").command = ["/usr/local/bin/foreign"]; }],
    ["drifted live target args", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").args = ["--foreign"]; }],
    ["drifted live target env", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").env = []; }],
    ["drifted live target envFrom", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").envFrom = [{ configMapRef: { name: "foreign" } }]; }],
    ["drifted live target ports", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").ports[0].containerPort = 9999; }],
    ["duplicate live container name", ({ pod }) => { pod.spec.containers.push(structuredClone(pod.spec.containers[0])); }],
    ["live host network", ({ pod }) => { pod.spec.hostNetwork = true; }],
    ["live host PID namespace", ({ pod }) => { pod.spec.hostPID = true; }],
    ["live host users setting", ({ pod }) => { pod.spec.hostUsers = false; }],
    ["live runtime class", ({ pod }) => { pod.spec.runtimeClassName = "untrusted-runtime"; }],
    ["malformed live host network", ({ pod }) => { pod.spec.hostNetwork = "yes"; }],
    ["drifted live image pull policy", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").imagePullPolicy = "Never"; }],
    ["drifted live termination message path", ({ pod }) => { pod.spec.containers.find((container: any) => container.name === "owner").terminationMessagePath = "/tmp/foreign"; }],
    ["live privileged sidecar", ({ pod }) => { pod.spec.containers.push({ name: "live-sidecar", image: "example.invalid/sidecar:1", securityContext: { privileged: true } }); }],
    ["live ephemeral container", ({ pod }) => { pod.spec.ephemeralContainers = [{ name: "debugger", image: "example.invalid/debugger:1" }]; }],
    ["live host path volume", ({ pod }) => { pod.spec.volumes = [{ name: "escape", hostPath: { path: "/" } }]; }],
    ["missing live containers", ({ pod }) => { delete pod.spec.containers; }],
    ["malformed live init containers", ({ pod }) => { pod.spec.initContainers = "invalid"; }],
    ["stale slice Service UID", ({ service }) => { service.metadata.uid = "replaced-service"; }],
    ["live sidecar listener", ({ pod }) => { pod.spec.containers.push({ name: "live-sidecar", image: "example.invalid/sidecar:1", ports: [{ containerPort: 8184 }] }); }],
    ["live sidecar listener env", ({ pod }) => { pod.spec.containers.push({ name: "live-sidecar", image: "example.invalid/sidecar:1", env: [{ name: "OPL_WORKSPACE_ADDR", value: ":8184" }] }); }],
    ["live sidecar listener envFrom", ({ list, pod }) => {
      pod.spec.containers.push({ name: "live-sidecar", image: "example.invalid/sidecar:1", envFrom: [{ configMapRef: { name: "live-sidecar-config" } }] });
      list.items.push({ apiVersion: "v1", kind: "ConfigMap", metadata: { name: "live-sidecar-config", namespace: "installation" }, data: { OPL_WORKSPACE_ADDR: ":8184" } });
    }],
    ["live restartable init listener", ({ pod }) => { pod.spec.initContainers = [{ name: "live-sidecar", image: "example.invalid/sidecar:1", restartPolicy: "Always", ports: [{ containerPort: 8184 }] }]; }],
    ["wrong endpoint port", ({ endpoints }) => { endpoints.subsets[0].ports[0].port = 9999; }],
    ["wrong slice port", ({ slice }) => { slice.ports[0].port = 9999; }]
  ];
  for (const [name, change] of cases) await t.test(name, async () => {
    const fixture = withLiveWorkspaceBackends();
    change(fixture);
    const result = await invoke(fixture.list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
    assert.deepEqual(result.report.communication.edges, []);
  });
  const reordered = withLiveWorkspaceBackends();
  const owner = reordered.pod.spec.containers[0];
  reordered.pod.spec.containers[0] = {
    ports: owner.ports, env: owner.env, command: owner.command, image: owner.image, name: owner.name
  };
  const reorderedResult = await invoke(reordered.list);
  assert.equal(reorderedResult.status, 0, reorderedResult.stdout + reorderedResult.stderr);
  assert.equal(reorderedResult.report.communication.edges.length, 46);
  const injectedServiceAccountVolume = withLiveWorkspaceBackends();
  injectedServiceAccountVolume.pod.spec.volumes = [{
    name: "kube-api-access-abc12",
    projected: { sources: [
      { serviceAccountToken: { expirationSeconds: 3607, path: "token" } },
      { configMap: { name: "kube-root-ca.crt", items: [{ key: "ca.crt", path: "ca.crt" }] } },
      { downwardAPI: { items: [{ fieldRef: { apiVersion: "v1", fieldPath: "metadata.namespace" }, path: "namespace" }] } }
    ] }
  }];
  const injectedVolumeResult = await invoke(injectedServiceAccountVolume.list);
  assert.equal(injectedVolumeResult.status, 0, injectedVolumeResult.stdout + injectedVolumeResult.stderr);
  assert.equal(injectedVolumeResult.report.communication.edges.length, 46);
  const forgedServiceAccountVolume = withLiveWorkspaceBackends();
  forgedServiceAccountVolume.pod.spec.volumes = [{
    name: "kube-api-access-abc12",
    projected: { sources: [{ serviceAccountToken: { path: "token" } }, { secret: { name: "foreign" } }] }
  }];
  const forgedVolumeResult = await invoke(forgedServiceAccountVolume.list);
  assert.equal(forgedVolumeResult.status, 1, forgedVolumeResult.stdout);
  assert.ok(forgedVolumeResult.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
  const reorderedInit = withLiveWorkspaceBackends();
  const initA = { name: "init-a", image: "example.invalid/init-a:1" };
  const initB = { name: "init-b", image: "example.invalid/init-b:1" };
  reorderedInit.target.spec.template.spec.initContainers = [initA, initB];
  reorderedInit.pod.spec.initContainers = [initB, initA];
  const reorderedInitResult = await invoke(reorderedInit.list);
  assert.equal(reorderedInitResult.status, 1, reorderedInitResult.stdout);
  assert.ok(reorderedInitResult.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
});


test("installation CLI refuses empty or incomplete live endpoint inventories", async t => {
  for (const [name, change] of [
    ["empty Endpoints", (live: ReturnType<typeof withLiveWorkspaceBackends>) => { live.endpoints.subsets = []; }],
    ["missing Endpoints subsets", (live: ReturnType<typeof withLiveWorkspaceBackends>) => { delete live.endpoints.subsets; }],
    ["empty EndpointSlice", (live: ReturnType<typeof withLiveWorkspaceBackends>) => { live.slice.endpoints = []; }],
  ] as const) await t.test(name, async () => {
    const live = withLiveWorkspaceBackends();
    change(live);
    const result = await invoke(live.list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
    assert.deepEqual(result.report.communication.edges, []);
  });
});

test("installation CLI rejects live endpoints that are not serviceable", async t => {
  for (const [name, change] of [
    ["EndpointSlice ready=false", (live: ReturnType<typeof withLiveWorkspaceBackends>) => { live.slice.endpoints[0].conditions = { ready: false }; }],
    ["EndpointSlice serving=false", (live: ReturnType<typeof withLiveWorkspaceBackends>) => { live.slice.endpoints[0].conditions = { serving: false }; }],
    ["EndpointSlice terminating", (live: ReturnType<typeof withLiveWorkspaceBackends>) => { live.slice.endpoints[0].conditions = { ready: true, serving: true, terminating: true }; }],
    ["Endpoints notReadyAddresses", (live: ReturnType<typeof withLiveWorkspaceBackends>) => {
      live.endpoints.subsets[0].notReadyAddresses = live.endpoints.subsets[0].addresses;
      delete live.endpoints.subsets[0].addresses;
    }],
  ] as const) await t.test(name, async () => {
    const live = withLiveWorkspaceBackends();
    change(live);
    const result = await invoke(live.list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
    assert.deepEqual(result.report.communication.edges, []);
  });
  const published = withLiveWorkspaceBackends();
  published.service.spec.publishNotReadyAddresses = true;
  published.endpoints.subsets[0].notReadyAddresses = published.endpoints.subsets[0].addresses;
  delete published.endpoints.subsets[0].addresses;
  const allowed = await invoke(published.list);
  assert.equal(allowed.status, 0, allowed.stdout + allowed.stderr);
  assert.equal(allowed.report.communication.edges.length, 46);
});

test("installation CLI rejects malformed initContainers through its consumable error report", async t => {
  for (const value of ["invalid", [null], [{ name: "sidecar" }]]) await t.test(JSON.stringify(value), async () => {
    const list = workedInstallation();
    deployment(list, "workspace").spec.template.spec.initContainers = value;
    const result = await invoke(list);
    assert.equal(result.status, 1);
    assert.equal(result.stderr, "installation_manifest_invalid\n");
    assert.deepEqual(result.report, {
      ok: false, errors: [{ code: "installation_manifest_shape_invalid" }],
      communication: { edges: [], security: { tokenMinimumLength: 32 } }
    });
  });
});

test("installation CLI ignores standard non-workload resources in a complete namespace inventory", async t => {
  for (const [apiVersion, kind] of [
    ["coordination.k8s.io/v1", "Lease"],
    ["events.k8s.io/v1", "Event"],
    ["apps/v1", "ControllerRevision"],
    ["metrics.k8s.io/v1beta1", "PodMetrics"],
    ["certificates.k8s.io/v1", "CertificateSigningRequest"],
  ] as const) await t.test(`${apiVersion}/${kind}`, async () => {
    const live = withLiveWorkspaceBackends();
    live.list.items.push({ apiVersion, kind, metadata: { name: `unrelated-${kind.toLowerCase()}`, namespace: "installation" } });
    const result = await invoke(live.list);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.equal(result.report.communication.edges.length, 46);
  });
});

test("installation CLI rejects malformed inputs with a sanitized consumable report", async () => {
  for (const manifest of [null, {}, { kind: "List", items: [] }, { apiVersion: "v1", kind: "List", items: "bad" },
    { apiVersion: "v1", kind: "List", items: [null] },
    { apiVersion: "v1", kind: "List", items: [{ kind: "Deployment", metadata: { name: "broken" }, spec: {} }] },
    '{"private":"10.27.6.9","secret":"do-not-disclose",']) {
    const result = await invoke(manifest);
    assert.equal(result.status, 1);
    assert.equal(result.report?.ok, false, result.stderr);
    assert.ok(result.report.errors.every(error => typeof error.code === "string"));
    assert.deepEqual(result.report.communication.edges, []);
    assert.equal(result.stderr, "installation_manifest_invalid\n");
    assert.doesNotMatch(result.stdout + result.stderr, /10\.27\.6\.9|do-not-disclose|SyntaxError|TypeError/);
  }
});


test("installation CLI refuses RPC addresses without an exact Service and target listener proof", async () => {
  const cases: [string, (list: ReturnType<typeof workedInstallation>) => void][] = [
    ["rpc_address_required", list => { container(list, "serve").env = container(list, "serve").env.filter(entry => entry.name !== "OPL_WORKSPACE_ADDR"); }],
    ["rpc_address_invalid", list => { container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").value = "10.27.6.9:8184"; }],
    ["rpc_address_invalid", list => { container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").value = "http://workspace-rpc:9184"; }],
    ["rpc_service_missing", list => { container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").value = "missing:9184"; }],
    ["rpc_service_target_mismatch", list => { list.items.find(item => item.kind === "Service" && item.metadata.name === "workspace-rpc").spec.selector = { unit: "serve" }; }],
    ["rpc_service_port_invalid", list => { list.items.find(item => item.kind === "Service" && item.metadata.name === "workspace-rpc").spec.ports[0].port = 1111; }],
    ["rpc_target_port_invalid", list => { list.items.find(item => item.kind === "Service" && item.metadata.name === "workspace-rpc").spec.ports[0].targetPort = "unknown"; }],
    ["rpc_target_listener_invalid", list => { container(list, "workspace").env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").value = ":3333"; }],
    ["rpc_target_missing", list => { list.items = list.items.filter(item => item !== deployment(list, "workspace")); }]
  ];
  for (const [code, change] of cases) {
    const list = workedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 1, code);
    assert.equal(result.report.ok, false);
    assert.ok(result.report.errors.some(error => error.code === code), JSON.stringify(result.report.errors));
    assert.deepEqual(result.report.communication.edges, []);
    assert.doesNotMatch(result.stdout + result.stderr, /10\.27\.6\.9|http:\/\//);
  }
});

test("installation CLI requires non-optional token and target peer Secret references without reading Secret bodies", async () => {
  const cases: [string, (list: ReturnType<typeof workedInstallation>) => void][] = [
    ["rpc_token_ref_invalid", list => { container(list, "serve").env = container(list, "serve").env.filter(entry => entry.name !== "OPL_WORKSPACE_TOKEN"); }],
    ["rpc_token_ref_invalid", list => { const entry = container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_TOKEN"); delete entry.valueFrom; entry.value = "do-not-disclose-token-".repeat(3); }],
    ["rpc_token_ref_invalid", list => { container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_TOKEN").valueFrom.secretKeyRef.optional = true; }],
    ["rpc_token_ref_invalid", list => { container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_TOKEN").valueFrom.secretKeyRef.key = ""; }],
    ["rpc_peer_ref_invalid", list => { container(list, "workspace").env = container(list, "workspace").env.filter(entry => entry.name !== "OPL_WORKSPACE_PEER_TOKENS"); }],
    ["rpc_peer_ref_invalid", list => { const entry = container(list, "workspace").env.find(entry => entry.name === "OPL_WORKSPACE_PEER_TOKENS"); delete entry.valueFrom; entry.value = '{"serve":"do-not-disclose-token"}'; }],
    ["installation_config_map_key_unproven", list => { container(list, "workspace").env.find(entry => entry.name === "OPL_WORKSPACE_PEER_TOKENS").valueFrom = { configMapKeyRef: { name: "not-a-secret", key: "peers" } }; }]
  ];
  for (const [code, change] of cases) {
    const list = workedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 1, code);
    assert.ok(result.report.errors.some(error => error.code === code), JSON.stringify(result.report.errors));
    assert.deepEqual(result.report.communication.edges, []);
    assert.doesNotMatch(result.stdout + result.stderr, /do-not-disclose-token/);
  }
});

test("installation CLI proves Kubernetes ingress and egress semantics without guessing policy intent", async () => {
  const cases: [string | null, (list: ReturnType<typeof workedInstallation>) => void][] = [
    ["rpc_ingress_unisolated", list => { list.items = list.items.filter(item => item !== policy(list, "workspace")); }],
    ["rpc_ingress_denied", list => { policy(list, "workspace").spec.ingress = []; }],
    ["rpc_ingress_denied", list => { policy(list, "workspace").spec.ingress[0].from = [{ podSelector: { matchLabels: { unit: "unknown" } } }]; }],
    ["rpc_ingress_denied", list => { policy(list, "workspace").spec.ingress[0].ports = [{ port: 9184 }]; }],
    ["rpc_ingress_denied", list => { policy(list, "workspace").spec.ingress[0].ports = [{ port: 8184, protocol: "UDP" }]; }],
    ["rpc_ingress_unproven", list => { policy(list, "workspace").spec.ingress[0].from = [{ ipBlock: { cidr: "10.27.0.0/16" } }]; }],
    ["rpc_ingress_unproven", list => { policy(list, "workspace").spec.ingress[0].from = [{ namespaceSelector: { matchLabels: { environment: "protected" } } }]; }],
    ["rpc_ingress_denied", list => { policy(list, "workspace").spec.ingress[0].from.forEach(peer => { peer.namespaceSelector = { matchLabels: { "kubernetes.io/metadata.name": "other" } }; }); }],
    ["rpc_egress_denied", list => { list.items.push({ apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", metadata: { name: "serve-egress", namespace: "installation" }, spec: { podSelector: { matchLabels: { unit: "serve" } }, policyTypes: ["Egress"], egress: [] } }); }],
    [null, list => { delete policy(list, "workspace").spec.policyTypes; }],
    [null, list => { const deny = structuredClone(policy(list, "workspace")); deny.metadata.name = "additional-deny"; deny.spec.ingress = []; list.items.push(deny); }],
    [null, list => { policy(list, "workspace").spec.ingress[0].ports = [{ port: 8180, endPort: 8189 }]; }],
    [null, list => { policy(list, "workspace").spec.ingress = [{}]; }],
    [null, list => { policy(list, "workspace").spec.ingress = [{ from: [], ports: [] }]; }],
    [null, list => { policy(list, "workspace").spec.ingress[0].from.forEach(peer => { peer.namespaceSelector = { matchLabels: { "kubernetes.io/metadata.name": "installation" } }; }); }],
    [null, list => { policy(list, "workspace").spec.ingress[0].from.forEach(peer => { peer.namespaceSelector = { matchLabels: { environment: "protected" } }; }); list.items.push({ apiVersion: "v1", kind: "Namespace", metadata: { name: "installation", labels: { environment: "protected" } } }); }],
    [null, list => { policy(list, "workspace").spec.ingress[0].from = [{ podSelector: { matchExpressions: [{ key: "unit", operator: "In", values: ["serve", "console-bff", "gateway-integration", "ledger"] }, { key: "blocked", operator: "DoesNotExist" }, { key: "unknown", operator: "NotIn", values: ["blocked"] }] } }]; }]
  ];
  for (const [code, change] of cases) {
    const list = workedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, code ? 1 : 0, `${code}: ${JSON.stringify(result.report?.errors)} ${result.stderr}`);
    if (code) {
      assert.ok(result.report.errors.some(error => error.code === code), JSON.stringify(result.report.errors));
      assert.deepEqual(result.report.communication.edges, []);
    } else {
      assert.equal(result.report.communication.edges.length, 46);
    }
    assert.doesNotMatch(result.stdout + result.stderr, /10\.27\.0\.0/);
  }
});

test("installation CLI rejects ambiguous Kubernetes process, env, Service and namespace bindings", async () => {
  const cases: [string, (list: ReturnType<typeof workedInstallation>) => void][] = [
    ["installation_process_ambiguous", list => { const copy = structuredClone(deployment(list, "serve")); copy.metadata.name = "duplicate-serve"; list.items.push(copy); }],
    ["installation_env_ambiguous", list => { container(list, "serve").env.push({ name: "OPL_WORKSPACE_ADDR", value: "workspace-rpc:9184" }); }],
    ["installation_env_from_unproven", list => { container(list, "serve").envFrom = [{ secretRef: { name: "opaque" } }]; }],
    ["installation_resource_duplicate", list => { list.items.push(structuredClone(policy(list, "workspace"))); }],
    ["rpc_service_backend_ambiguous", list => { const copy = structuredClone(deployment(list, "serve")); copy.metadata.name = "foreign-backend"; copy.spec.template.metadata.labels = { unit: "workspace" }; copy.spec.template.spec.containers[0].command = ["/bin/foreign-server"]; copy.spec.template.spec.containers[0].image = "foreign.invalid/backend:1"; list.items.push(copy); }],
    ["installation_namespace_unsupported", list => { const bff = deployment(list, "console-bff"); bff.metadata.namespace = "other"; container(list, "console-bff").env.filter(entry => entry.name.endsWith("_URL")).forEach(entry => { entry.value = entry.value.replace("-rpc:", "-rpc.installation.svc.cluster.local:"); }); }],
    ["installation_process_unproven", list => { delete container(list, "console-bff").command; container(list, "console-bff").args = ["/bin/unknown"]; }],
    ["installation_pod_network_unsupported", list => { deployment(list, "serve").spec.template.spec.hostNetwork = true; }],
    ["installation_process_missing", list => { list.items = list.items.filter(item => item.kind !== "Deployment"); }]
  ];
  for (const [code, change] of cases) {
    const list = workedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 1, code);
    assert.ok(result.report?.errors.some(error => error.code === code), `${code}: ${result.stdout} ${result.stderr}`);
    assert.deepEqual(result.report.communication.edges, []);
  }
});

test("installation CLI consumes the whole explicit RPC graph and preserves optional and legacy profiles", async () => {
  const full = workedInstallation();
  const control = structuredClone(deployment(full, "console-bff"));
  control.metadata.name = "unit-control-plane";
  control.spec.template.metadata.labels = { unit: "control-plane" };
  control.spec.template.spec.containers[0].command = ["/usr/local/bin/opl-control-plane"];
  control.spec.template.spec.containers[0].env = [{ name: "CONTROL_PLANE_ADDR", value: ":8787" }];
  full.items.push(control);
  const result = await invoke(full);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(result.report.communication.edges.length, 46);
  const signatures = result.report.communication.edges.map(edge => `${edge.caller}:${edge.target}:${edge.addressEnv}:${edge.tokenEnv}`).sort();
  const expected = [
    "capability:runtime_control:OPL_RUNTIME_CONTROL_ADDR:OPL_RUNTIME_CONTROL_TOKEN", "capability:build:OPL_BUILD_ADDR:OPL_BUILD_TOKEN", "capability:ledger:OPL_LEDGER_ADDR:OPL_LEDGER_TOKEN", "capability:serve:OPL_SERVE_ADDR:OPL_SERVE_TOKEN",
    "build:capability:OPL_CAPABILITY_ADDR:OPL_CAPABILITY_TOKEN", "build:ledger:OPL_LEDGER_ADDR:OPL_LEDGER_TOKEN", "build:tenant:OPL_CLOUD_IDENTITY_URL:OPL_TENANT_TOKEN",
    "runtime_control:capability:OPL_CAPABILITY_ADDR:OPL_CAPABILITY_TOKEN",
    "workspace:resource_catalog:OPL_RESOURCE_CATALOG_ADDR:OPL_RESOURCE_CATALOG_TOKEN", "workspace:fabric:OPL_FABRIC_ADDR:OPL_FABRIC_TOKEN", "workspace:ledger:OPL_LEDGER_ADDR:OPL_LEDGER_TOKEN", "workspace:capability:OPL_CAPABILITY_ADDR:OPL_CAPABILITY_TOKEN", "workspace:runtime_control:OPL_RUNTIME_CONTROL_ADDR:OPL_RUNTIME_CONTROL_TOKEN", "workspace:serve:OPL_SERVE_ADDR:OPL_SERVE_TOKEN", "workspace:tenant:OPL_GATEWAY_ADDR:OPL_GATEWAY_TOKEN",
    "serve:capability:OPL_CAPABILITY_ADDR:OPL_CAPABILITY_TOKEN", "serve:runtime_control:OPL_RUNTIME_CONTROL_ADDR:OPL_RUNTIME_CONTROL_TOKEN", "serve:fabric:OPL_FABRIC_COORDINATION_ADDR:OPL_FABRIC_COORDINATION_TOKEN", "serve:tenant:OPL_GATEWAY_ADDR:OPL_GATEWAY_TOKEN", "serve:ledger:OPL_LEDGER_ADDR:OPL_LEDGER_TOKEN", "serve:workspace:OPL_WORKSPACE_ADDR:OPL_WORKSPACE_TOKEN",
    "resource_catalog:ledger:OPL_LEDGER_ADDR:OPL_LEDGER_TOKEN",
    "tenant:fabric:OPL_FABRIC_ADDR:OPL_FABRIC_TOKEN", "tenant:build:OPL_BUILD_ADDR:OPL_BUILD_TOKEN", "tenant:workspace:OPL_WORKSPACE_ADDR:OPL_WORKSPACE_TOKEN",
    "fabric:resource_catalog:OPL_RESOURCE_CATALOG_ADDR:OPL_RESOURCE_CATALOG_TOKEN", "fabric:ledger:OPL_LEDGER_ADDR:OPL_LEDGER_TOKEN",
    "ledger:resource_catalog:OPL_RESOURCE_CATALOG_ADDR:OPL_RESOURCE_CATALOG_TOKEN", "ledger:workspace:OPL_WORKSPACE_ADDR:OPL_WORKSPACE_TOKEN", "ledger:tenant:OPL_GATEWAY_ADDR:OPL_GATEWAY_TOKEN",
    "console_bff:capability:OPL_CAPABILITY_URL:OPL_CAPABILITY_TOKEN", "console_bff:runtime_control:OPL_RUNTIME_CONTROL_URL:OPL_RUNTIME_CONTROL_TOKEN", "console_bff:build:OPL_BUILD_URL:OPL_BUILD_TOKEN", "console_bff:workspace:OPL_WORKSPACE_URL:OPL_WORKSPACE_TOKEN", "console_bff:serve:OPL_SERVE_URL:OPL_SERVE_TOKEN", "console_bff:resource_catalog:OPL_RESOURCE_CATALOG_URL:OPL_RESOURCE_CATALOG_TOKEN", "console_bff:tenant:OPL_GATEWAY_URL:OPL_GATEWAY_TOKEN",
    ...["capability", "build", "runtime_control", "workspace", "serve", "resource_catalog", "fabric", "ledger", "console_bff"].map(caller => `${caller}:tenant:OPL_CLOUD_IDENTITY_URL:OPL_CLOUD_IDENTITY_TOKEN`)
  ].sort();
  assert.deepEqual(signatures, expected);
  const minimal = workedInstallation();
  const retained: Record<string, string[]> = {
    capability: ["OPL_RUNTIME_CONTROL_ADDR", "OPL_BUILD_ADDR", "OPL_LEDGER_ADDR"], build: ["OPL_CAPABILITY_ADDR", "OPL_LEDGER_ADDR"],
    "runtime-control": ["OPL_CAPABILITY_ADDR"], workspace: ["OPL_RESOURCE_CATALOG_ADDR", "OPL_FABRIC_ADDR"], serve: ["OPL_WORKSPACE_ADDR"],
    fabric: ["OPL_RESOURCE_CATALOG_ADDR", "OPL_LEDGER_ADDR"]
  };
  for (const item of minimal.items.filter(item => item.kind === "Deployment")) {
    const service = item.metadata.name.slice("unit-".length);
    const owner = item.spec.template.spec.containers[0];
    const keep = new Set(["OPL_CLOUD_IDENTITY_URL", ...(retained[service] ?? [])]);
    if (service === "ledger" || service === "gateway-integration") keep.delete("OPL_CLOUD_IDENTITY_URL");
    owner.env = owner.env.filter(entry => !expected.some(signature => signature.startsWith(`${service === "gateway-integration" ? "tenant" : service.replaceAll("-", "_")}:`) &&
      [signature.split(":")[2], signature.split(":")[3]].includes(entry.name)) ||
      keep.has(entry.name) || entry.name === "OPL_TENANT_TOKEN" && service === "build" ||
      entry.name.endsWith("_TOKEN") && keep.has(entry.name.replace(/_TOKEN$/, "_ADDR")) ||
      entry.name === "OPL_CLOUD_IDENTITY_TOKEN" && keep.has("OPL_CLOUD_IDENTITY_URL"));
  }
  const optional = await invoke(minimal);
  assert.equal(optional.status, 0, optional.stdout + optional.stderr);
  assert.equal(optional.report.communication.edges.length, 20);
  const legacy = workedInstallation();
  legacy.items = legacy.items.filter(item => item.kind === "Deployment" && ["unit-fabric", "unit-ledger"].includes(item.metadata.name));
  for (const item of legacy.items) item.spec.template.spec.containers[0].env = [];
  legacy.items.push(control);
  const oldProfile = await invoke(legacy);
  assert.equal(oldProfile.status, 0, oldProfile.stdout + oldProfile.stderr);
  assert.deepEqual(oldProfile.report.communication.edges, []);
});


test("installation CLI reports selected policies and requires a proof on both sides of an isolated connection", async () => {
  const list = workedInstallation();
  const egress = { apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", metadata: { name: "serve-egress", namespace: "installation" }, spec: {
    podSelector: { matchLabels: { unit: "serve" } }, policyTypes: ["Egress"], egress: [{ to: [{ podSelector: {} }], ports: [{ protocol: "TCP" }] }]
  } };
  list.items.push(egress);
  const success = await invoke(list);
  assert.equal(success.status, 0, success.stdout + success.stderr);
  const edge = success.report.communication.edges.find(edge => edge.caller === "serve" && edge.target === "workspace");
  assert.deepEqual(edge.ingressPolicies, ["workspace-rpc"]);
  assert.deepEqual(edge.egressPolicies, ["serve-egress"]);
  egress.spec.egress = [{ to: [{ podSelector: { matchLabels: { unit: "workspace" } } }], ports: [{ port: "rpc" }] }] as any;
  const restricted = await invoke(list);
  assert.equal(restricted.status, 1);
  assert.ok(restricted.report.errors.some(error => error.code === "rpc_egress_denied" && error.caller === "serve" && error.target === "capability"));
  assert.ok(!restricted.report.errors.some(error => error.caller === "serve" && error.target === "workspace"));
  egress.spec.egress = [{ to: [{ namespaceSelector: { matchLabels: { unknown: "unobserved" } } }] }] as any;
  const unknown = await invoke(list);
  assert.equal(unknown.status, 1);
  assert.ok(unknown.report.errors.some(error => error.code === "rpc_egress_unproven"));
  egress.spec.egress = [{ to: [{ ipBlock: { cidr: "10.27.0.0/16" } }] }] as any;
  const ipBlock = await invoke(list);
  assert.equal(ipBlock.status, 1);
  assert.ok(ipBlock.report.errors.some(error => error.code === "rpc_egress_unproven"));
  const defaulted = workedInstallation();
  policy(defaulted, "workspace").spec.policyTypes = [];
  assert.equal((await invoke(defaulted)).status, 0);
});


test("installation CLI executes through a symlink and emits JSON through the public /tmp absolute path", async () => {
  const dir = await mkdtemp(join(tmpdir(), "opl-cli-alias-"));
  try {
    const alias = join(dir, "validate.ts");
    await symlink(cli, alias);
    const link = await invoke(workedInstallation(), alias);
    assert.equal(link.status, 0, link.stderr);
    assert.equal(link.report?.ok, true, "exit 0 without JSON is not successful CLI execution");
    const tmpPath = cli.replace(/^\/private\/tmp\//, "/tmp/");
    const direct = await invoke(workedInstallation(), tmpPath);
    assert.equal(direct.status, 0, direct.stderr);
    assert.equal(direct.report?.communication.edges.length, 46);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("installation CLI resolves declared ConfigMap envFrom with Kubernetes overrides and keeps Secret envFrom unproven", async () => {
  const list = workedInstallation();
  list.items.push({ apiVersion: "v1", kind: "ConfigMap", metadata: { name: "shared", namespace: "installation" }, data: {
    OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON: "__PROVIDER_PROFILE_JSON__",
    OPL_RESOURCE_CATALOG_PROFILE_JSON: "__CATALOG_PROFILE_JSON__",
    OPL_PUBLIC_URL: "__DO_NOT_DISCLOSE_PRIVATE_ORIGIN__",
    OPL_CLOUD_IDENTITY_URL: "gateway-integration-rpc:9187",
    OPL_WORKSPACE_ADDR: "__SHADOWED_ADDRESS__"
  } });
  container(list, "serve").envFrom = [{ configMapRef: { name: "shared" } }];
  container(list, "serve").env = container(list, "serve").env.filter(entry => entry.name !== "OPL_CLOUD_IDENTITY_URL");
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(result.report.communication.edges.length, 46);
  assert.doesNotMatch(result.stdout + result.stderr, /__PROVIDER|__CATALOG|__DO_NOT|__SHADOWED/);
  container(list, "serve").envFrom.push({ secretRef: { name: "opaque-secret" } });
  const opaque = await invoke(list);
  assert.equal(opaque.status, 1);
  assert.ok(opaque.report.errors.some(error => error.code === "installation_env_from_unproven"));
  container(list, "serve").envFrom = [{ configMapRef: { name: "missing" } }];
  assert.equal((await invoke(list)).report.errors[0].code, "installation_config_map_missing");
  container(list, "serve").envFrom = [{ configMapRef: { name: "shared" } }];
  container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").valueFrom = { configMapKeyRef: { name: "shared", key: "OPL_CLOUD_IDENTITY_URL" } };
  delete container(list, "serve").env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").value;
  assert.equal((await invoke(list)).report.errors.some(error => error.code === "rpc_service_target_mismatch"), true);
});

test("installation CLI recognizes the Product image default Control Plane command without inventing an RPC caller", async () => {
  const list = workedInstallation();
  const control = structuredClone(deployment(list, "console-bff"));
  control.metadata.name = "retained-control-plane";
  control.spec.template.metadata.labels = { unit: "control-plane" };
  delete control.spec.template.spec.containers[0].command;
  control.spec.template.spec.containers[0].env = [];
  list.items.push(control);
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(result.report.communication.edges.length, 46);
  assert.ok(!result.report.communication.edges.some(edge => edge.callerDeployment === "retained-control-plane"));
  control.spec.template.spec.containers[0].args = ["/bin/unknown"];
  const opaque = await invoke(list);
  assert.equal(opaque.status, 1);
  assert.ok(opaque.report.errors.some(error => error.code === "installation_process_unproven"));
});

test("installation CLI still proves policy for a Service call back into the same Deployment", async () => {
  const list = workedInstallation();
  container(list, "gateway-integration").env.push({ name: "OPL_CLOUD_IDENTITY_URL", value: "gateway-integration-rpc:9187" },
    { name: "OPL_CLOUD_IDENTITY_TOKEN", valueFrom: { secretKeyRef: { name: "tenant-auth", key: "identity" } } });
  const denied = await invoke(list);
  assert.equal(denied.status, 1);
  assert.ok(denied.report.errors.some(error => error.code === "rpc_ingress_denied" && error.caller === "tenant" && error.target === "tenant"));
  policy(list, "gateway-integration").spec.ingress[0].from.push({ podSelector: { matchLabels: { unit: "gateway-integration" } } });
  const allowed = await invoke(list);
  assert.equal(allowed.status, 0, allowed.stdout + allowed.stderr);
  assert.equal(allowed.report.communication.edges.length, 47);
  assert.deepEqual(allowed.report.communication.edges.find(edge => edge.caller === "tenant" && edge.target === "tenant").ingressPolicies, ["gateway-integration-rpc"]);
});

test("installation CLI resolves Product listener defaults only when the target env is absent or runtime-empty", async () => {
  const list = workedInstallation();
  for (const [service, listen] of [["capability", "OPL_CAPABILITY_ADDR"], ["build", "OPL_BUILD_ADDR"], ["runtime-control", "OPL_RUNTIME_CONTROL_ADDR"]]) {
    container(list, service).env = container(list, service).env.filter(entry => entry.name !== listen);
  }
  const defaults = await invoke(list);
  assert.equal(defaults.status, 0, defaults.stdout + defaults.stderr);
  assert.equal(defaults.report.communication.edges.length, 46);
  for (const [service, listen] of [["capability", "OPL_CAPABILITY_ADDR"], ["build", "OPL_BUILD_ADDR"], ["runtime-control", "OPL_RUNTIME_CONTROL_ADDR"]]) {
    container(list, service).env.push({ name: listen, value: " " });
  }
  const empty = await invoke(list);
  assert.equal(empty.status, 0, empty.stdout + empty.stderr);
  container(list, "build").env.find(entry => entry.name === "OPL_BUILD_ADDR").value = ":3333";
  const wrong = await invoke(list);
  assert.equal(wrong.status, 1);
  assert.ok(wrong.report.errors.some(error => error.code === "rpc_target_listener_invalid" && error.target === "build"));
  container(list, "build").env.find(entry => entry.name === "OPL_BUILD_ADDR").value = ":8182";
  container(list, "fabric").env = container(list, "fabric").env.filter(entry => entry.name !== "OPL_FABRIC_ADDR");
  const fabric = await invoke(list);
  assert.equal(fabric.status, 1, "Product Fabric default :8187 cannot be silently replaced by this worked example's :8188 port");
  assert.ok(fabric.report.errors.some(error => error.code === "rpc_target_listener_invalid" && error.target === "fabric"));
});

test("installation CLI rejects malformed nested Kubernetes facts without uncaught errors or input disclosure", async () => {
  const changes: ((list: ReturnType<typeof workedInstallation>) => void)[] = [
    list => { container(list, "serve").env.push(null); },
    list => { container(list, "serve").env.push({ value: "do-not-disclose" }); },
    list => { container(list, "workspace").ports.push(null); },
    list => { list.items.find(item => item.kind === "Service" && item.metadata.name === "workspace-rpc").spec.ports = { private: "do-not-disclose" }; },
    list => { deployment(list, "serve").spec.template.metadata.labels = "do-not-disclose"; },
    list => { deployment(list, "serve").metadata.namespace = 123; },
    list => { container(list, "serve").envFrom = "do-not-disclose"; }
  ];
  for (const change of changes) {
    const list = workedInstallation();
    change(list);
    const result = await invoke(list);
    assert.equal(result.status, 1);
    assert.equal(result.report?.ok, false, result.stderr);
    assert.ok(result.report.errors.length > 0);
    assert.deepEqual(result.report.communication.edges, []);
    assert.equal(result.stderr, "installation_manifest_invalid\n");
    assert.doesNotMatch(result.stdout + result.stderr, /TypeError|do-not-disclose/);
  }
});

test("installation CLI always declares the Product minimum token length for the protected executor", async () => {
  const valid = await invoke(workedInstallation());
  assert.equal(valid.status, 0, valid.stderr);
  assert.deepEqual(valid.report.communication.security, { tokenMinimumLength: 32 });
  const invalid = await invoke({});
  assert.equal(invalid.status, 1);
  assert.deepEqual(invalid.report.communication.security, { tokenMinimumLength: 32 });
});


test("installation CLI retains the current Control Plane CloudIdentity caller from the default image command", async () => {
  const list = workedInstallation();
  const image = container(list, "serve").image;
  list.items.push({ apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "retained-http", namespace: "installation" }, spec: {
    template: { metadata: { labels: { unit: "retained-http" } }, spec: { containers: [{ name: "control-plane", image,
      env: [
        { name: "OPL_CONTROL_PLANE_BFF_TOKEN", valueFrom: { secretKeyRef: { name: "http-auth", key: "inbound" } } },
        { name: "OPL_CLOUD_IDENTITY_URL", value: "gateway-integration-rpc:9187" },
        { name: "OPL_CLOUD_IDENTITY_TOKEN", valueFrom: { secretKeyRef: { name: "http-auth", key: "outbound" } } },
      ] }] } }
  } });
  policy(list, "gateway-integration").spec.ingress[0].from.push({ podSelector: { matchLabels: { unit: "retained-http" } } });
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(result.report.communication.edges.find(e => e.caller === "control_plane"), {
    caller: "control_plane", target: "tenant", addressEnv: "OPL_CLOUD_IDENTITY_URL", tokenEnv: "OPL_CLOUD_IDENTITY_TOKEN",
    callerDeployment: "retained-http", targetDeployment: "unit-gateway-integration", port: 8187,
    tokenRef: { name: "http-auth", key: "outbound" }, peerRef: { name: "gateway-integration-admission", key: "peers" },
    serviceRef: { name: "gateway-integration-rpc", namespace: "installation" }, ingressPolicies: ["gateway-integration-rpc"], egressPolicies: [],
  });
  const retained = list.items.find(item => item.metadata.name === "retained-http").spec.template.spec.containers[0];
  retained.env = retained.env.filter(e => !["OPL_CLOUD_IDENTITY_URL", "OPL_CLOUD_IDENTITY_TOKEN"].includes(e.name));
  const invalid = await invoke(list);
  assert.equal(invalid.status, 1);
  assert.ok(invalid.report.errors.some(e => e.code === "rpc_address_required" && e.caller === "control_plane"));
});


test("installation CLI proves the complete daemon argv before admitting a Product process", async t => {
  for (const [name, command, args] of [
    ["probe in args", ["/usr/local/bin/opl-serve"], ["probe-workspace-access"]],
    ["probe in command", ["/usr/local/bin/opl-serve", "probe-workspace-access"], undefined],
    ["probe split across command and args", ["/usr/local/bin/opl-serve", "probe-workspace-access"], ["extra"]],
    ["unproven daemon args", ["/usr/local/bin/opl-serve"], ["unknown-mode"]],
    ["args-only probe cannot prove an inherited entrypoint", undefined, ["probe-workspace-access"]],
  ] as const) await t.test(name, async () => {
    const list = workedInstallation();
    const serve = container(list, "serve");
    serve.command = command; serve.args = args;
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "installation_process_unproven"));
    assert.deepEqual(result.report.communication.edges, []);
  });
  for (const [name, command, args] of [
    ["explicit empty args", ["/usr/local/bin/opl-serve"], []],
    ["image default with explicit empty args", undefined, []],
    ["empty Kubernetes command uses image CMD", [], []],
  ] as const) await t.test(name, async () => {
    const list = workedInstallation();
    const selected = container(list, name.startsWith("image default") || name.includes("image CMD") ? "console-bff" : "serve");
    selected.command = command; selected.args = args;
    if (name.startsWith("image default") || name.includes("image CMD")) {
      selected.env = [];
    }
    const result = await invoke(list);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.ok(result.report.communication.edges.some(edge => edge.caller === "serve"));
  });
});

test("installation CLI follows Ledger raw activation and Fabric trimmed activation independently of listener defaults", async t => {
  for (const [owner, activationEnv, raw, enabled] of [
    ["ledger", "OPL_LEDGER_ADDR", " ", true],
    ["ledger", "OPL_LEDGER_ADDR", "\t\n", true],
    ["ledger", "OPL_LEDGER_ADDR", "", false],
    ["fabric", "OPL_FABRIC_DATABASE_URL", " ", false],
    ["fabric", "OPL_FABRIC_DATABASE_URL", "\t\n", false],
    ["fabric", "OPL_FABRIC_DATABASE_URL", "", false],
    ["fabric", "OPL_FABRIC_DATABASE_URL", "configured", true],
    ["fabric", "OPL_FABRIC_DATABASE_URL", "\u0085", false],
    ["fabric", "OPL_FABRIC_DATABASE_URL", "\uFEFF", true],
  ] as const) await t.test(`${owner} ${JSON.stringify(raw)} enabled=${enabled}`, async () => {
    const list = workedInstallation();
    const unit = deployment(list, owner);
    const process = container(list, owner);
    list.items = [unit];
    process.env = [{ name: activationEnv, value: raw }, { name: "OPL_CLOUD_IDENTITY_URL", value: "missing:8187" }];
    const result = await invoke(list);
    assert.equal(result.status, enabled ? 1 : 0, result.stdout + result.stderr);
    if (enabled) assert.ok(result.report.errors.some(error => error.code === "rpc_target_missing" && error.caller === owner));
    assert.deepEqual(result.report.communication.edges, []);
  });
  for (const whitespace of [" ", "\u0085"]) await t.test(`Ledger ${JSON.stringify(whitespace)} selects its runtime default listener`, async () => {
    const list = workedInstallation();
    container(list, "ledger").env.find(entry => entry.name === "OPL_LEDGER_ADDR").value = whitespace;
    list.items.find(item => item.kind === "Service" && item.metadata.name === "ledger-rpc").spec.ports[0].targetPort = 8186;
    container(list, "ledger").ports[0].containerPort = 8186;
    const result = await invoke(list);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.equal(result.report.communication.edges.length, 46);
    assert.ok(result.report.communication.edges.filter(edge => edge.target === "ledger").every(edge => edge.port === 8186));
  });
});

test("installation CLI refuses shared Pod wildcard listener collisions for numeric and named targetPorts", async t => {
  for (const targetPort of ["numeric", "named"] as const) {
    for (const [left, right] of [[":8186", ":8186"], ["0.0.0.0:8186", ":8186"], ["[::]:8186", "0.0.0.0:8186"], ["", " "]] as const) {
      await t.test(`${targetPort} ${JSON.stringify([left, right])}`, async () => {
        const list = workedInstallation();
        const catalog = deployment(list, "resource-catalog"), ledger = deployment(list, "ledger");
        const catalogContainer = container(list, "resource-catalog"), ledgerContainer = container(list, "ledger");
        catalogContainer.env.find(entry => entry.name === "OPL_RESOURCE_CATALOG_ADDR").value = left;
        ledgerContainer.env.find(entry => entry.name === "OPL_LEDGER_ADDR").value = right;
        catalogContainer.name = "catalog"; ledgerContainer.name = "ledger";
        catalogContainer.ports[0] = { name: "catalog-rpc", containerPort: 8186 };
        ledgerContainer.ports[0] = { name: "ledger-rpc", containerPort: 8186 };
        catalog.spec.template.spec.containers.push(ledgerContainer);
        list.items = list.items.filter(item => item !== ledger);
        for (const service of ["resource-catalog", "ledger"]) {
          const svc = list.items.find(item => item.kind === "Service" && item.metadata.name === `${service}-rpc`);
          svc.spec.selector = structuredClone(catalog.spec.template.metadata.labels);
          svc.spec.ports[0].targetPort = targetPort === "numeric" ? 8186 : service === "ledger" ? "ledger-rpc" : "catalog-rpc";
        }
        for (const item of list.items.filter(item => item.kind === "NetworkPolicy")) item.spec = { podSelector: {}, policyTypes: ["Ingress"], ingress: [{}] };
        const result = await invoke(list);
        assert.equal(result.status, 1, result.stdout);
        assert.ok(result.report.errors.some(error => error.code === "installation_listener_conflict"), result.stdout);
        assert.deepEqual(result.report.communication.edges, []);
      });
    }
  }
  await t.test("distinct listeners in a shared Pod remain admissible", async () => {
    const list = workedInstallation();
    const serve = deployment(list, "serve"), workspace = deployment(list, "workspace");
    container(list, "serve").name = "serve"; container(list, "workspace").name = "workspace";
    container(list, "serve").ports[0].name = "serve-rpc";
    container(list, "workspace").ports[0].name = "workspace-rpc";
    serve.spec.template.spec.containers.push(container(list, "workspace"));
    list.items = list.items.filter(item => item !== workspace);
    for (const [owner, port] of [["serve", 8185], ["workspace", 8184]] as const) {
      const svc = list.items.find(item => item.kind === "Service" && item.metadata.name === `${owner}-rpc`);
      svc.spec.selector = structuredClone(serve.spec.template.metadata.labels);
      svc.spec.ports[0].targetPort = port;
    }
    for (const item of list.items.filter(item => item.kind === "NetworkPolicy")) item.spec = { podSelector: {}, policyTypes: ["Ingress"], ingress: [{}] };
    const result = await invoke(list);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.equal(result.report.communication.edges.length, 46);
  });
});

test("installation CLI rejects restartable init containers that claim a Product additional listener", async () => {
  const list = workedInstallation();
  deployment(list, "capability").spec.template.spec.initContainers = [{
    name: "object-sidecar", image: "example.invalid/sidecar:1", restartPolicy: "Always",
    env: [{ name: "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", value: ":8191" }]
  }];
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_listener_conflict"));
  assert.deepEqual(result.report.communication.edges, []);
});

test("installation CLI rejects unknown sidecars that claim a Product additional listener", async () => {
  const list = workedInstallation();
  deployment(list, "capability").spec.template.spec.containers.push({
    name: "object-sidecar", image: "example.invalid/sidecar:1",
    env: [{ name: "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", value: ":8191" }]
  });
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_listener_conflict"));
  assert.deepEqual(result.report.communication.edges, []);
});

test("installation CLI requires Capability object listener when the topology has no default", async () => {
  const list = workedInstallation();
  const process = container(list, "capability");
  process.env = process.env.filter(entry => entry.name !== "OPL_CAPABILITY_OBJECT_LISTEN_ADDR");
  const result = await invoke(list);
  assert.equal(result.status, 1, result.stdout);
  assert.ok(result.report.errors.some(error => error.code === "installation_listener_unproven"));
  assert.deepEqual(result.report.communication.edges, []);
});

test("installation CLI refuses malformed explicit additional listeners", async t => {
  for (const value of ["garbage", { valueFrom: { secretKeyRef: { name: "serve-access", key: "address" } } }]) await t.test(JSON.stringify(value), async () => {
    const list = workedInstallation();
    const access = { name: "OPL_SERVE_ACCESS_ADDR", ...(typeof value === "string" ? { value } : value) };
    container(list, "serve").env.push(access);
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "installation_listener_unproven"));
    assert.deepEqual(result.report.communication.edges, []);
  });
});

test("installation CLI proves Serve access entry listener separately from its owner RPC", async t => {
  for (const accessAddress of [":8185", "\u0085:8185\u0085"]) await t.test(`Serve access and RPC collide: ${JSON.stringify(accessAddress)}`, async () => {
    const list = workedInstallation();
    container(list, "serve").env.push({ name: "OPL_SERVE_ACCESS_ADDR", value: accessAddress });
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "installation_listener_conflict"), result.stdout);
    assert.deepEqual(result.report.communication.edges, []);
  });
  for (const accessAddress of [undefined, "\u0085", ":8189"]) await t.test(`Serve access with a shared Pod: ${JSON.stringify(accessAddress)}`, async () => {
    const list = workedInstallation();
    const serve = deployment(list, "serve"), workspace = deployment(list, "workspace");
    const serveProcess = container(list, "serve"), workspaceProcess = container(list, "workspace");
    serveProcess.name = "serve"; workspaceProcess.name = "workspace";
    serveProcess.ports[0].name = "serve-rpc";
    workspaceProcess.ports[0] = { name: "workspace-rpc", containerPort: 8188 };
    workspaceProcess.env.find(entry => entry.name === "OPL_WORKSPACE_ADDR").value = ":8188";
    if (accessAddress !== undefined) serveProcess.env.push({ name: "OPL_SERVE_ACCESS_ADDR", value: accessAddress });
    serve.spec.template.spec.containers.push(workspaceProcess);
    list.items = list.items.filter(item => item !== workspace);
    for (const [owner, port] of [["serve", 8185], ["workspace", 8188]] as const) {
      const svc = list.items.find(item => item.kind === "Service" && item.metadata.name === `${owner}-rpc`);
      svc.spec.selector = structuredClone(serve.spec.template.metadata.labels);
      svc.spec.ports[0].targetPort = port;
    }
    for (const item of list.items.filter(item => item.kind === "NetworkPolicy")) item.spec = { podSelector: {}, policyTypes: ["Ingress"], ingress: [{}] };
    const result = await invoke(list);
    const collides = accessAddress !== ":8189";
    assert.equal(result.status, collides ? 1 : 0, result.stdout + result.stderr);
    if (collides) {
      assert.ok(result.report.errors.some(error => error.code === "installation_listener_conflict"), result.stdout);
      assert.deepEqual(result.report.communication.edges, []);
    } else assert.equal(result.report.communication.edges.length, 46);
  });
});

test("installation CLI proves Service uniqueness across declared Pods and every native Pod producer", async t => {
  for (const kind of ["Pod", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job", "CronJob"]) {
    for (const labels of ["matching", "different", "missing", "invalid", "unknown-template"] as const) await t.test(`${kind} ${labels}`, async () => {
      const list = workedInstallation();
      const target = deployment(list, "workspace");
      const template = structuredClone(target.spec.template);
      if (labels === "different") template.metadata.labels = { unit: "unrelated" };
      if (labels === "missing") delete template.metadata.labels;
      if (labels === "invalid") template.metadata.labels = "unproven";
      const extra: any = { apiVersion: kind === "Pod" || kind === "ReplicationController" ? "v1" : ["Job", "CronJob"].includes(kind) ? "batch/v1" : "apps/v1", kind,
        metadata: { name: "additional-backend", namespace: "installation" } };
      if (kind === "Pod") { extra.metadata = { ...extra.metadata, ...template.metadata }; extra.spec = template.spec; }
      else if (kind === "CronJob") extra.spec = { jobTemplate: { spec: { template } } };
      else extra.spec = { template };
      if (labels === "unknown-template") {
        if (kind === "Pod") extra.metadata.labels = 123;
        else if (kind === "CronJob") delete extra.spec.jobTemplate;
        else delete extra.spec.template;
      }
      list.items.push(extra);
      const result = await invoke(list);
      const rejected = labels === "matching" || labels === "invalid" || labels === "unknown-template";
      assert.equal(result.status, rejected ? 1 : 0, result.stdout + result.stderr);
      if (rejected) {
        assert.ok(result.report.errors.some(error => ["rpc_service_backend_ambiguous", "rpc_service_backend_unproven", "installation_manifest_shape_invalid"].includes(error.code)), result.stdout);
        assert.deepEqual(result.report.communication.edges, []);
      } else assert.equal(result.report.communication.edges.length, 46);
    });
  }
  await t.test("Pod ownership metadata does not prove a declared second backend", async () => {
    const list = workedInstallation();
    const target = deployment(list, "workspace");
    list.items.push({ apiVersion: "v1", kind: "Pod", metadata: { name: "owned-pod", namespace: "installation", labels: structuredClone(target.spec.template.metadata.labels),
      ownerReferences: [{ apiVersion: "apps/v1", kind: "Deployment", name: target.metadata.name, uid: "unverified" }] }, spec: structuredClone(target.spec.template.spec) });
    const result = await invoke(list);
    assert.equal(result.status, 1, result.stdout);
    assert.ok(result.report.errors.some(error => error.code === "rpc_service_backend_ambiguous"));
  });
});


test("installation CLI refuses unknown Pod-producing APIs with a sanitized proof failure", async t => {
  for (const apiVersion of ["custom.example.invalid/v1", "apps/v99", "constructor", "__proto__", "toString"]) await t.test(apiVersion, async () => {
    const list = workedInstallation();
    list.items.push({ apiVersion, kind: "UnknownPodProducer", metadata: { name: "unproven-producer", namespace: "installation" },
      spec: { template: structuredClone(deployment(list, "workspace").spec.template) } });
    const result = await invoke(list);
    assert.equal(result.status, 1);
    assert.equal(result.stderr, "installation_manifest_invalid\n");
    assert.equal(result.report?.ok, false);
    assert.ok(result.report.errors.some(error => error.code === "rpc_service_backend_unproven"));
    assert.deepEqual(result.report.communication.edges, []);
  });
});

test("configured RPC edges follow each native caller's address semantics and retain address-or-token admission", async t => {
  // Independent inventory from the actual Go call sites, not the topology under test.
  const groups: [string, "raw" | "go-trim", string[]][] = [
    ["capability", "raw", ["SERVE"]],
    ["workspace", "go-trim", ["LEDGER", "CAPABILITY", "RUNTIME_CONTROL", "SERVE", "GATEWAY"]],
    ["serve", "raw", ["CAPABILITY", "RUNTIME_CONTROL", "FABRIC_COORDINATION", "GATEWAY"]],
    ["serve", "go-trim", ["LEDGER"]],
    ["resource-catalog", "go-trim", ["LEDGER"]],
    ["gateway-integration", "go-trim", ["FABRIC", "BUILD", "WORKSPACE", "CLOUD_IDENTITY"]],
    ["ledger", "go-trim", ["RESOURCE_CATALOG", "WORKSPACE", "GATEWAY", "CLOUD_IDENTITY"]],
    ["console-bff", "go-trim", ["CAPABILITY", "RUNTIME_CONTROL", "BUILD", "WORKSPACE", "SERVE", "RESOURCE_CATALOG", "GATEWAY"]],
    ["control-plane", "go-trim", ["CLOUD_IDENTITY"]],
  ];
  const setup = (owner: string) => {
    const list = workedInstallation();
    if (owner === "control-plane") {
      const control = structuredClone(deployment(list, "console-bff"));
      control.metadata.name = "unit-control-plane";
      control.spec.template.metadata.labels = { unit: "control-plane" };
      control.spec.template.spec.containers[0].command = ["/usr/local/bin/opl-control-plane"];
      control.spec.template.spec.containers[0].env = [];
      list.items.push(control);
    }
    return list;
  };
  for (const [owner, semantics, peers] of groups) for (const peer of peers) {
    const addressEnv = `OPL_${peer}_${peer === "CLOUD_IDENTITY" || owner === "console-bff" ? "URL" : "ADDR"}`;
    const tokenEnv = `OPL_${peer}_TOKEN`;
    const principal = owner === "gateway-integration" ? "tenant" : owner.replaceAll("-", "_");
    for (const [value, rawEnabled, trimEnabled] of [[" ", true, false], ["\u0085", true, false], ["\uFEFF", true, true], ["", false, false]] as const) {
      await t.test(`${owner} ${addressEnv} ${JSON.stringify(value)}`, async () => {
        const list = setup(owner), process = container(list, owner);
        process.env = process.env.filter(entry => ![addressEnv, tokenEnv].includes(entry.name));
        process.env.push({ name: addressEnv, value });
        const enabled = semantics === "raw" ? rawEnabled : trimEnabled;
        const result = await invoke(list);
        assert.equal(result.status, enabled ? 1 : 0, result.stdout + result.stderr);
        if (enabled) {
          assert.ok(result.report.errors.some(error => error.caller === principal && error.addressEnv === addressEnv));
          assert.deepEqual(result.report.communication.edges, []);
        } else assert.ok(!result.report.communication.edges.some(edge => edge.caller === principal && edge.addressEnv === addressEnv));
      });
    }
    for (const [value, enabled] of [[" ", false], ["\u0085", false], ["\uFEFF", true]] as const) {
      await t.test(`${owner} token-only ${tokenEnv} ${JSON.stringify(value)}`, async () => {
        const list = setup(owner), process = container(list, owner);
        process.env = process.env.filter(entry => ![addressEnv, tokenEnv].includes(entry.name));
        process.env.push({ name: tokenEnv, value });
        const result = await invoke(list);
        assert.equal(result.status, enabled ? 1 : 0, result.stdout + result.stderr);
        if (enabled) assert.ok(result.report.errors.some(error => error.code === "rpc_address_required" && error.caller === principal && error.addressEnv === addressEnv));
      });
    }
  }
  for (const [value, enabled] of [["\u0085", false], ["\uFEFF", true]] as const) await t.test(`Control Plane activationEnv ${JSON.stringify(value)}`, async () => {
    const list = setup("control-plane");
    container(list, "control-plane").env.push({ name: "OPL_CONTROL_PLANE_BFF_TOKEN", value });
    const result = await invoke(list);
    assert.equal(result.status, enabled ? 1 : 0, result.stdout + result.stderr);
  });
  for (const [addressEnv, enabled] of [["OPL_LEDGER_ADDR", true], ["OPL_FABRIC_COORDINATION_ADDR", false]] as const) await t.test(`Serve uses native address value for ${addressEnv}`, async () => {
    const list = workedInstallation();
    const entry = container(list, "serve").env.find(entry => entry.name === addressEnv);
    entry.value = `\u0085 ${entry.value} \u0085`;
    const result = await invoke(list);
    assert.equal(result.status, enabled ? 0 : 1, result.stdout + result.stderr);
  });
});


test("installation CLI supplies Product-resolved Service identity for configured Go-trimmed addresses", async () => {
  const list = workedInstallation();
  const address = container(list, "serve").env.find(e => e.name === "OPL_LEDGER_ADDR");
  address.value = `\u0085 ${address.value} \u3000`;
  const result = await invoke(list);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.deepEqual(result.report.communication.edges.find(e => e.caller === "serve" && e.target === "ledger").serviceRef,
    { name: "ledger-rpc", namespace: "installation" });
  assert.deepEqual(result.report.communication.edges.find(e => e.caller === "serve" && e.target === "workspace").serviceRef,
    { name: "workspace-rpc", namespace: "installation" });
});
