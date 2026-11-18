import { readFileSync } from "node:fs";
import type { InstallationTopology } from "../../../tools/ops/installation-manifest.ts";

// #1247 owns this pending contract field. Admission tests supply the explicit
// projection independently until that worker's contract is integrated.
export function composedTopology(): InstallationTopology {
  const topology = JSON.parse(readFileSync(new URL("../../../deploy/portable/opl-cloud-owner-topology.json", import.meta.url), "utf8"));
  const projections: Record<string, string | null> = {
    capability: "OPL_CAPABILITY_DATABASE_URL", build: "OPL_BUILD_DATABASE_URL",
    "runtime-control": "OPL_RUNTIME_CONTROL_DATABASE_URL", workspace: "OPL_WORKSPACE_DATABASE_URL",
    serve: "OPL_SERVE_DATABASE_URL", "resource-catalog": "OPL_RESOURCE_CATALOG_DATABASE_URL",
    "gateway-integration": "OPL_TENANT_DATABASE_URL", ledger: "DATABASE_URL",
    fabric: "DATABASE_URL", "control-plane": "DATABASE_URL", "console-bff": null
  };
  for (const process of topology.processes) process.compositionDatabaseEnv = projections[process.service];
  return topology;
}

// Synthetic worked installation. RPC requirements are written independently of
// the Product topology: a stale or missing declaration must change CLI evidence.
export function workedInstallation() {
  const units = [
    ["capability", "capability", 8181, "OPL_CAPABILITY_ADDR", "OPL_CAPABILITY_PEER_TOKENS"],
    ["build", "build", 8182, "OPL_BUILD_ADDR", "OPL_BUILD_PEER_TOKENS"],
    ["runtime-control", "runtime_control", 8183, "OPL_RUNTIME_CONTROL_ADDR", "OPL_RUNTIME_CONTROL_PEER_TOKENS"],
    ["workspace", "workspace", 8184, "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_PEER_TOKENS"],
    ["serve", "serve", 8185, "OPL_SERVE_ADDR", "OPL_SERVE_PEER_TOKENS"],
    ["resource-catalog", "resource_catalog", 8186, "OPL_RESOURCE_CATALOG_ADDR", "OPL_RESOURCE_CATALOG_PEER_TOKENS"],
    ["gateway-integration", "tenant", 8187, "OPL_TENANT_ADDR", "OPL_TENANT_PEER_TOKENS"],
    ["fabric", "fabric", 8188, "OPL_FABRIC_ADDR", "OPL_FABRIC_PEER_TOKENS"],
    ["ledger", "ledger", 8189, "OPL_LEDGER_ADDR", "OPL_LEDGER_PEER_TOKENS"],
    ["console-bff", "console_bff", 8190, "OPL_BFF_ADDR", ""]
  ] as const;
  const calls: Record<string, [string, string, string][]> = {
    capability: [["runtime_control", "OPL_RUNTIME_CONTROL_ADDR", "OPL_RUNTIME_CONTROL_TOKEN"], ["build", "OPL_BUILD_ADDR", "OPL_BUILD_TOKEN"], ["ledger", "OPL_LEDGER_ADDR", "OPL_LEDGER_TOKEN"], ["serve", "OPL_SERVE_ADDR", "OPL_SERVE_TOKEN"]],
    build: [["capability", "OPL_CAPABILITY_ADDR", "OPL_CAPABILITY_TOKEN"], ["ledger", "OPL_LEDGER_ADDR", "OPL_LEDGER_TOKEN"], ["tenant", "OPL_CLOUD_IDENTITY_URL", "OPL_TENANT_TOKEN"]],
    runtime_control: [["capability", "OPL_CAPABILITY_ADDR", "OPL_CAPABILITY_TOKEN"]],
    workspace: [["resource_catalog", "OPL_RESOURCE_CATALOG_ADDR", "OPL_RESOURCE_CATALOG_TOKEN"], ["fabric", "OPL_FABRIC_ADDR", "OPL_FABRIC_TOKEN"], ["ledger", "OPL_LEDGER_ADDR", "OPL_LEDGER_TOKEN"], ["capability", "OPL_CAPABILITY_ADDR", "OPL_CAPABILITY_TOKEN"], ["runtime_control", "OPL_RUNTIME_CONTROL_ADDR", "OPL_RUNTIME_CONTROL_TOKEN"], ["serve", "OPL_SERVE_ADDR", "OPL_SERVE_TOKEN"], ["tenant", "OPL_GATEWAY_ADDR", "OPL_GATEWAY_TOKEN"]],
    serve: [["capability", "OPL_CAPABILITY_ADDR", "OPL_CAPABILITY_TOKEN"], ["runtime_control", "OPL_RUNTIME_CONTROL_ADDR", "OPL_RUNTIME_CONTROL_TOKEN"], ["fabric", "OPL_FABRIC_COORDINATION_ADDR", "OPL_FABRIC_COORDINATION_TOKEN"], ["tenant", "OPL_GATEWAY_ADDR", "OPL_GATEWAY_TOKEN"], ["ledger", "OPL_LEDGER_ADDR", "OPL_LEDGER_TOKEN"], ["workspace", "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_TOKEN"]],
    resource_catalog: [["ledger", "OPL_LEDGER_ADDR", "OPL_LEDGER_TOKEN"]],
    tenant: [["fabric", "OPL_FABRIC_ADDR", "OPL_FABRIC_TOKEN"], ["build", "OPL_BUILD_ADDR", "OPL_BUILD_TOKEN"], ["workspace", "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_TOKEN"], ["serve", "OPL_SERVE_ADDR", "OPL_SERVE_TOKEN"]],
    fabric: [["resource_catalog", "OPL_RESOURCE_CATALOG_ADDR", "OPL_RESOURCE_CATALOG_TOKEN"], ["ledger", "OPL_LEDGER_ADDR", "OPL_LEDGER_TOKEN"]],
    ledger: [["resource_catalog", "OPL_RESOURCE_CATALOG_ADDR", "OPL_RESOURCE_CATALOG_TOKEN"], ["workspace", "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_TOKEN"], ["tenant", "OPL_GATEWAY_ADDR", "OPL_GATEWAY_TOKEN"]],
    console_bff: [["capability", "OPL_CAPABILITY_URL", "OPL_CAPABILITY_TOKEN"], ["runtime_control", "OPL_RUNTIME_CONTROL_URL", "OPL_RUNTIME_CONTROL_TOKEN"], ["build", "OPL_BUILD_URL", "OPL_BUILD_TOKEN"], ["workspace", "OPL_WORKSPACE_URL", "OPL_WORKSPACE_TOKEN"], ["serve", "OPL_SERVE_URL", "OPL_SERVE_TOKEN"], ["resource_catalog", "OPL_RESOURCE_CATALOG_URL", "OPL_RESOURCE_CATALOG_TOKEN"], ["tenant", "OPL_GATEWAY_URL", "OPL_GATEWAY_TOKEN"]]
  };
  // The shared authorization call uses CLOUD_IDENTITY_TOKEN; Build also opens a
  // dedicated Gateway connection with TENANT_TOKEN at that very same address.
  for (const [, principal] of units) {
    if (principal !== "tenant") calls[principal].push(["tenant", "OPL_CLOUD_IDENTITY_URL", "OPL_CLOUD_IDENTITY_TOKEN"]);
  }
  const metadata = (name: string) => ({ name, namespace: "installation" });
  const items: any[] = [];
  for (const [service, principal, port, listenEnv, peerEnv] of units) {
    const env: any[] = [{ name: listenEnv, value: `:${port}` }];
    if (principal === "capability") env.push({ name: "OPL_CAPABILITY_OBJECT_LISTEN_ADDR", value: ":8191" });
    if (peerEnv) env.push({ name: peerEnv, valueFrom: { secretKeyRef: { name: `${service}-admission`, key: "peers" } } });
    if (principal === "fabric") env.push({ name: "OPL_FABRIC_DATABASE_URL", valueFrom: { secretKeyRef: { name: "fabric-db", key: "dsn" } } });
    for (const [target, addressEnv, tokenEnv] of calls[principal]) {
      const targetUnit = units.find(unit => unit[1] === target)!;
      if (!env.some(entry => entry.name === addressEnv)) env.push({ name: addressEnv, value: `${targetUnit[0]}-rpc:${targetUnit[2] + 1000}` });
      env.push({ name: tokenEnv, valueFrom: { secretKeyRef: { name: `${service}-auth`, key: tokenEnv } } });
    }
    items.push({ apiVersion: "apps/v1", kind: "Deployment", metadata: metadata(`unit-${service}`), spec: {
      selector: { matchLabels: { unit: service } }, template: { metadata: { labels: { unit: service } }, spec: { containers: [{
        name: "owner", image: "example.invalid/cloud@sha256:" + "a".repeat(64), command: [`/usr/local/bin/opl-${service}`], env,
        ports: [{ name: "rpc", containerPort: port, protocol: "TCP" }]
      }] } }
    } });
    items.push({ apiVersion: "v1", kind: "Service", metadata: metadata(`${service}-rpc`), spec: {
      selector: { unit: service }, ports: [{ name: "rpc", port: port + 1000, targetPort: "rpc", protocol: "TCP" }]
    } });
    const callers = Object.entries(calls).filter(([, edges]) => edges.some(edge => edge[0] === principal));
    items.push({ apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", metadata: metadata(`${service}-rpc`), spec: {
      podSelector: { matchLabels: { unit: service } }, policyTypes: ["Ingress"], ingress: [{
        from: callers.map(([caller]) => ({ podSelector: { matchLabels: { unit: units.find(unit => unit[1] === caller)![0] } } })),
        ports: [{ port: "rpc", protocol: "TCP" }]
      }]
    } });
  }
  return { apiVersion: "v1", kind: "List", items };
}

export function deployment(list: ReturnType<typeof workedInstallation>, service: string) {
  return list.items.find(item => item.kind === "Deployment" && item.metadata.name === `unit-${service}`);
}
export function container(list: ReturnType<typeof workedInstallation>, service: string) {
  return deployment(list, service).spec.template.spec.containers[0];
}
export function policy(list: ReturnType<typeof workedInstallation>, service: string) {
  return list.items.find(item => item.kind === "NetworkPolicy" && item.metadata.name === `${service}-rpc`);
}

// The composed deployment unit: one Deployment whose container runs every
// owner process this Product declares in one process space. The shared
// environment carries effective listener binds as well as caller routes.
// Those facts retain their own address semantics in admission.
const composedImage = "example.invalid/cloud@sha256:" + "a".repeat(64);
const composedLabels = { "app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "backend" };
const composedNamespace = "installation";
// The live workload the cluster runs for the unit. The Instance reads these
// objects from the cluster and hands them to the Product inside the manifest
// it admits, so the fixture states them the way the cluster does.
const composedUnitUid = "opl-cloud-backend-uid";
const composedPodIP = "192.0.2.1";
const composedPodIPs = [{ ip: composedPodIP }, { ip: "2001:db8::1" }];

// Every listener the Product's owners declare inside the unit, and the
// Services that publish them.
const composedListeners = [
  { owner: "control-plane", port: 8787, containerPort: "control-plane-http", service: "opl-cloud-control-plane", servicePort: 8787 },
  { owner: "ledger", port: 8189, containerPort: "ledger-typed", service: "opl-cloud-ledger", servicePort: 8186 },
  { owner: "ledger", port: 8081, containerPort: "ledger-http", service: "opl-cloud-ledger", servicePort: 8081 },
  { owner: "fabric", port: 8188, containerPort: "fabric-rpc", service: "opl-cloud-fabric", servicePort: 8187 },
  { owner: "fabric", port: 8082, containerPort: "fabric-http", service: "opl-cloud-fabric", servicePort: 8082 },
  { owner: "capability", port: 8181, containerPort: "capability-grpc", service: "opl-cloud-capability", servicePort: 8181 },
  { owner: "capability", port: 8191, containerPort: "capability-objects", service: "opl-cloud-capability", servicePort: 8281 },
  { owner: "build", port: 8182, containerPort: "build-grpc", service: "opl-cloud-build", servicePort: 8182 },
  { owner: "runtime-control", port: 8183, containerPort: "runtime-control-grpc", service: "opl-cloud-runtime-control", servicePort: 8183 },
  { owner: "tenant", port: 8187, containerPort: "tenant-grpc", service: "opl-cloud-tenant", servicePort: 8187 },
  { owner: "console-bff", port: 8190, containerPort: "console-bff-http", service: "opl-cloud-console-bff", servicePort: 8190 },
  { owner: "workspace", port: 8184, containerPort: "workspace-grpc", service: "opl-cloud-workspace", servicePort: 8184 },
  { owner: "serve", port: 8185, containerPort: "serve-grpc", service: "opl-cloud-serve", servicePort: 8185 },
  { owner: "serve", port: 8192, containerPort: "serve-access", service: "opl-cloud-serve", servicePort: 8188 },
  { owner: "resource-catalog", port: 8186, containerPort: "resource-catalog-grpc", service: "opl-cloud-resource-catalog", servicePort: 8186 }
] as const;

// The shared environment: every owner's own listener address, the address
// every caller dials, and the Product image this unit runs.
const composedSharedEnv: Record<string, string> = {
  OPL_CLOUD_IMAGE: composedImage,
  OPL_CAPABILITY_ADDR: ":8181",
  OPL_CAPABILITY_OBJECT_LISTEN_ADDR: ":8191",
  OPL_BUILD_ADDR: ":8182",
  OPL_RUNTIME_CONTROL_ADDR: ":8183",
  OPL_WORKSPACE_ADDR: ":8184",
  OPL_SERVE_ADDR: ":8185",
  OPL_SERVE_ACCESS_ADDR: ":8192",
  OPL_RESOURCE_CATALOG_ADDR: ":8186",
  OPL_TENANT_ADDR: ":8187",
  OPL_LEDGER_ADDR: ":8189",
  LEDGER_ADDR: ":8081",
  OPL_FABRIC_ADDR: ":8188",
  FABRIC_ADDR: ":8082",
  CONTROL_PLANE_ADDR: ":8787",
  PORT: "8787",
  OPL_BFF_ADDR: ":8190",
  OPL_CLOUD_IDENTITY_URL: "opl-cloud-tenant:8187",
  OPL_GATEWAY_ADDR: "opl-cloud-tenant:8187",
  OPL_GATEWAY_URL: "opl-cloud-tenant:8187",
  OPL_CAPABILITY_URL: "opl-cloud-capability:8181",
  OPL_RUNTIME_CONTROL_URL: "opl-cloud-runtime-control:8183",
  OPL_BUILD_URL: "opl-cloud-build:8182",
  OPL_WORKSPACE_URL: "opl-cloud-workspace:8184",
  OPL_SERVE_URL: "opl-cloud-serve:8185",
  OPL_RESOURCE_CATALOG_URL: "opl-cloud-resource-catalog:8186",
  OPL_LEDGER_URL: "opl-cloud-ledger:8186",
  OPL_FABRIC_URL: "opl-cloud-fabric:8187",
  OPL_FABRIC_COORDINATION_ADDR: ":8188"
};
// The shared container carries Fabric's credential for the callers that reach
// it, but the Console BFF has no Fabric origin, so it holds no Fabric route.

// The identity-scoped material the shared container carries. One Secret holds
// every owner boundary's credential; the key names the boundary, so two
// owners never read the same material.
const composedCredentials: Record<string, string> = {
  OPL_RUNTIME_CONTROL_TOKEN: "OPL_RUNTIME_CONTROL_TOKEN",
  OPL_BUILD_TOKEN: "OPL_BUILD_TOKEN",
  OPL_LEDGER_TOKEN: "OPL_LEDGER_TOKEN",
  OPL_SERVE_TOKEN: "OPL_SERVE_TOKEN",
  OPL_CAPABILITY_TOKEN: "OPL_CAPABILITY_TOKEN",
  OPL_WORKSPACE_TOKEN: "OPL_WORKSPACE_TOKEN",
  OPL_RESOURCE_CATALOG_TOKEN: "OPL_RESOURCE_CATALOG_TOKEN",
  OPL_FABRIC_TOKEN: "OPL_FABRIC_TOKEN",
  OPL_FABRIC_COORDINATION_TOKEN: "OPL_FABRIC_TOKEN",
  OPL_TENANT_TOKEN: "OPL_TENANT_TOKEN",
  OPL_CLOUD_IDENTITY_TOKEN: "OPL_TENANT_TOKEN",
  OPL_GATEWAY_TOKEN: "OPL_TENANT_TOKEN",
  OPL_CAPABILITY_PEER_TOKENS: "OPL_CAPABILITY_PEER_TOKENS",
  OPL_BUILD_PEER_TOKENS: "OPL_BUILD_PEER_TOKENS",
  OPL_RUNTIME_CONTROL_PEER_TOKENS: "OPL_RUNTIME_CONTROL_PEER_TOKENS",
  OPL_WORKSPACE_PEER_TOKENS: "OPL_WORKSPACE_PEER_TOKENS",
  OPL_SERVE_PEER_TOKENS: "OPL_SERVE_PEER_TOKENS",
  OPL_RESOURCE_CATALOG_PEER_TOKENS: "OPL_RESOURCE_CATALOG_PEER_TOKENS",
  OPL_TENANT_PEER_TOKENS: "OPL_TENANT_PEER_TOKENS",
  OPL_FABRIC_PEER_TOKENS: "OPL_FABRIC_PEER_TOKENS",
  OPL_LEDGER_PEER_TOKENS: "OPL_LEDGER_PEER_TOKENS"
};

export function workedComposedInstallation() {
  const metadata = (name: string) => ({ name, namespace: composedNamespace });
  const items: any[] = [];
  items.push({ apiVersion: "v1", kind: "ConfigMap", metadata: metadata("opl-cloud-config"), data: { ...composedSharedEnv } });
  const unit = { apiVersion: "apps/v1", kind: "Deployment", metadata: { ...metadata("opl-cloud-backend"), uid: composedUnitUid }, spec: {
    selector: { matchLabels: composedLabels }, template: { metadata: { labels: composedLabels }, spec: { containers: [{
      name: "backend", image: composedImage, command: ["/usr/local/bin/opl-cloud-composition"],
      envFrom: [{ configMapRef: { name: "opl-cloud-config" } }],
      env: [
        { name: "DATABASE_URL", valueFrom: { secretKeyRef: { name: "opl-cloud-database", key: "DATABASE_URL" } } },
        { name: "OPL_FABRIC_DATABASE_URL", valueFrom: { secretKeyRef: { name: "opl-cloud-fabric", key: "OPL_FABRIC_DATABASE_URL" } } },
        ...["CAPABILITY", "BUILD", "RUNTIME_CONTROL", "WORKSPACE", "SERVE", "RESOURCE_CATALOG", "TENANT"].map(owner =>
          ({ name: `OPL_${owner}_DATABASE_URL`, valueFrom: { secretKeyRef: { name: "opl-cloud-owner-databases", key: owner } } })),
        ...Object.entries(composedCredentials).map(([name, key]) =>
          ({ name, valueFrom: { secretKeyRef: { name: "opl-cloud-internal-service", key } } }))
      ],
      ports: composedListeners.map(listener => ({ name: listener.containerPort, containerPort: listener.port, protocol: "TCP" }))
    }] } }
  } };
  items.push(unit);
  const services = [...new Set(composedListeners.map(listener => listener.service))];
  for (const service of services) {
    items.push({ apiVersion: "v1", kind: "Service", metadata: { ...metadata(service), uid: `${service}-service-uid` }, spec: {
      selector: composedLabels,
      ports: composedListeners.filter(listener => listener.service === service)
        .map(listener => ({ name: listener.containerPort, port: listener.servicePort, targetPort: listener.containerPort, protocol: "TCP" }))
    } });
  }
  for (const owner of [...new Set(composedListeners.map(listener => listener.owner))]) {
    items.push({ apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", metadata: metadata(`${owner}-ingress`), spec: {
      podSelector: { matchLabels: composedLabels }, policyTypes: ["Ingress"],
      ingress: composedListeners.filter(listener => listener.owner === owner).map(listener => ({
        from: [{ podSelector: { matchLabels: composedLabels } }],
        ports: [{ port: listener.port, protocol: "TCP" }]
      }))
    } });
  }
  // The live workload the cluster runs for the unit: the ReplicaSet and Pod
  // the unit's Deployment owns, and the endpoint objects the cluster publishes
  // for every Service the unit serves. The Instance's production admission
  // reads these objects from the cluster and hands them to the Product inside
  // the manifest it admits, so the composed fixture carries the same overlay
  // the per-owner fixture's live backend carries.
  const reference = (resource: any) => ({ apiVersion: resource.apiVersion, kind: resource.kind, name: resource.metadata.name, uid: resource.metadata.uid, controller: true });
  const replicaSet = { apiVersion: "apps/v1", kind: "ReplicaSet", metadata: { name: "opl-cloud-backend-revision", namespace: composedNamespace, uid: "opl-cloud-backend-replicaset-uid", ownerReferences: [reference(unit)] }, spec: { template: structuredClone(unit.spec.template) } };
  const pod = { apiVersion: "v1", kind: "Pod", metadata: { ...structuredClone(unit.spec.template.metadata), name: "opl-cloud-backend-pod", namespace: composedNamespace, uid: "opl-cloud-backend-pod-uid", ownerReferences: [reference(replicaSet)] }, spec: structuredClone(unit.spec.template.spec), status: { podIP: composedPodIP, podIPs: composedPodIPs.map(entry => ({ ...entry })) } };
  items.push(replicaSet, pod);
  const targetRef = { apiVersion: "v1", kind: "Pod", name: pod.metadata.name, namespace: composedNamespace, uid: pod.metadata.uid };
  for (const service of services) {
    const published = items.find(item => item.kind === "Service" && item.metadata.name === service)!;
    items.push({ apiVersion: "discovery.k8s.io/v1", kind: "EndpointSlice", metadata: { name: `${service}-slice`, namespace: composedNamespace,
      labels: { "kubernetes.io/service-name": service }, ownerReferences: [reference(published)] }, addressType: "IPv4",
      ports: composedListeners.filter(listener => listener.service === service).map(listener => ({ name: listener.containerPort, port: listener.port, protocol: "TCP" })),
      endpoints: [{ addresses: [composedPodIP], targetRef: { ...targetRef } }] });
  }
  // The legacy Endpoints object the cluster publishes beside the slice.
  const legacy = services[0];
  items.push({ apiVersion: "v1", kind: "Endpoints", metadata: metadata(legacy), subsets: [{
    addresses: [{ ip: composedPodIP, targetRef: { ...targetRef } }],
    ports: composedListeners.filter(listener => listener.service === legacy).map(listener => ({ name: listener.containerPort, port: listener.port }))
  }] });
  return { apiVersion: "v1", kind: "List", items };
}

export function composedUnit(list: ReturnType<typeof workedComposedInstallation>) {
  return list.items.find(item => item.kind === "Deployment" && item.metadata.name === "opl-cloud-backend");
}
export function composedContainer(list: ReturnType<typeof workedComposedInstallation>) {
  return composedUnit(list).spec.template.spec.containers[0];
}
export function composedService(list: ReturnType<typeof workedComposedInstallation>, name: string) {
  return list.items.find(item => item.kind === "Service" && item.metadata.name === name);
}
export function composedPolicy(list: ReturnType<typeof workedComposedInstallation>, owner: string) {
  return list.items.find(item => item.kind === "NetworkPolicy" && item.metadata.name === `${owner}-ingress`);
}
export function composedSharedConfigMap(list: ReturnType<typeof workedComposedInstallation>) {
  return list.items.find(item => item.kind === "ConfigMap" && item.metadata.name === "opl-cloud-config");
}
export function composedReplicaSet(list: ReturnType<typeof workedComposedInstallation>) {
  return list.items.find(item => item.kind === "ReplicaSet" && item.metadata.name === "opl-cloud-backend-revision");
}
export function composedPod(list: ReturnType<typeof workedComposedInstallation>) {
  return list.items.find(item => item.kind === "Pod" && item.metadata.name === "opl-cloud-backend-pod");
}
export function composedEndpointSlice(list: ReturnType<typeof workedComposedInstallation>, service: string) {
  return list.items.find(item => item.kind === "EndpointSlice" && item.metadata.name === `${service}-slice`);
}
export function composedEndpoints(list: ReturnType<typeof workedComposedInstallation>, service: string) {
  return list.items.find(item => item.kind === "Endpoints" && item.metadata.name === service);
}
