import { isIP } from "node:net";
import { communicationPolicyProof } from "./installation-network-policy.ts";

// Product installation admission. Business communication requirements are read
// only from this checkout's portable topology, never inferred from env names.
export type SecretRef = { name: string; key: string };
export type CommunicationEdge = {
  caller: string; target: string; addressEnv: string; tokenEnv: string;
  callerDeployment: string; targetDeployment: string; port: number;
  tokenRef: SecretRef; peerRef: SecretRef; serviceRef: { name: string; namespace: string }; ingressPolicies: string[]; egressPolicies: string[];
};
export type InstallationError = { code: string; service?: string; caller?: string; target?: string; addressEnv?: string; tokenEnv?: string };
export type InstallationSecurity = { tokenMinimumLength: number };
export type InstallationReport = { ok: boolean; errors: InstallationError[]; communication: { edges: CommunicationEdge[]; security: InstallationSecurity | null } };
export type ProcessContract = {
  service: string; kind: string; binary: string; daemonArgs: string[]; transportPrincipal?: string;
  rpcEnabledByEnvSemantics?: "raw-nonempty" | "trimmed-nonempty";
  addressPortEnv?: string; additionalListeners?: { addressEnv: string; defaultAddress?: string; valueSemantics: "raw" | "trimmed" }[];
  addressEnv: string; defaultAddress: string; rpcAddressEnv?: string; defaultRPCAddress?: string; peerTokensEnv?: string; rpcEnabledByEnv?: string;
};
export type EdgeContract = { caller: string; target: string; addressEnv: string; tokenEnv: string; activation: "required" | "configured"; activationEnv?: string; configuredAddressSemantics?: "raw-nonempty" | "trimmed-nonempty" };
export type InstallationTopology = { image: { defaultCommand: string[] }; processes: ProcessContract[]; communication: { schemaVersion: number; edges: EdgeContract[]; security: InstallationSecurity } };
type Resource = Record<string, any>;
function object(value: unknown): value is Resource {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
export function installationSecurity(topology: unknown): InstallationSecurity | undefined {
  const value = object(topology) ? topology.communication?.security?.tokenMinimumLength : undefined;
  if (Number.isSafeInteger(value) && value > 0) return { tokenMinimumLength: value };
}
export function installationFailure(code: string, security: InstallationSecurity | null = null): InstallationReport {
  return { ok: false, errors: [{ code }], communication: { edges: [], security } };
}
function stringMap(value: unknown): boolean {
  return object(value) && Object.values(value).every(entry => typeof entry === "string");
}
function deepEqual(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (left === undefined || right === undefined || left === null || right === null || typeof left !== "object" || typeof right !== "object") return false;
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((entry, index) => deepEqual(entry, right[index]));
  }
  if (!object(left) || !object(right)) return false;
  const keys = new Set([...Object.keys(left), ...Object.keys(right)]);
  return [...keys].every(key => deepEqual(left[key], right[key]));
}
function validContainer(value: unknown): value is Resource {
  return object(value) && typeof value.name === "string" && typeof value.image === "string" &&
    (value.command === undefined || Array.isArray(value.command) && value.command.every((arg: unknown) => typeof arg === "string")) &&
    (value.args === undefined || Array.isArray(value.args) && value.args.every((arg: unknown) => typeof arg === "string")) &&
    (value.envFrom === undefined || Array.isArray(value.envFrom)) &&
    (value.env === undefined || Array.isArray(value.env) && value.env.every((entry: unknown) => object(entry) &&
      typeof entry.name === "string" && (entry.value === undefined || typeof entry.value === "string") &&
      (entry.valueFrom === undefined || object(entry.valueFrom)) && !(entry.value !== undefined && entry.valueFrom !== undefined))) &&
    (value.ports === undefined || Array.isArray(value.ports) && value.ports.every((entry: unknown) => object(entry) && validPort(entry.containerPort)));
}
function validList(value: unknown): value is Resource {
  if (!object(value) || value.apiVersion !== "v1" || value.kind !== "List" || !Array.isArray(value.items) || value.items.length === 0) return false;
  return value.items.every((resource: unknown) => {
    if (!object(resource) || typeof resource.kind !== "string" || typeof resource.apiVersion !== "string" ||
        !object(resource.metadata) || typeof resource.metadata.name !== "string" ||
        resource.metadata.namespace !== undefined && typeof resource.metadata.namespace !== "string" ||
        resource.metadata.labels !== undefined && !stringMap(resource.metadata.labels)) return false;
    if (resource.kind === "Service") return object(resource.spec) &&
      (resource.spec.selector === undefined || stringMap(resource.spec.selector)) &&
      (resource.spec.ports === undefined || Array.isArray(resource.spec.ports) && resource.spec.ports.every(object));
    if (resource.kind !== "Deployment") return true;
    return resource.apiVersion === "apps/v1" && object(resource.spec?.template?.spec) &&
      (resource.spec.template.metadata?.labels === undefined || stringMap(resource.spec.template.metadata.labels)) &&
      Array.isArray(resource.spec.template.spec.containers) && resource.spec.template.spec.containers.length > 0 &&
      resource.spec.template.spec.containers.every(validContainer) &&
      (resource.spec.template.spec.initContainers === undefined ||
        Array.isArray(resource.spec.template.spec.initContainers) && resource.spec.template.spec.initContainers.every(validContainer));
  });
}
type InstalledProcess = { contract: ProcessContract; deployment: Resource; container: Resource; env: Map<string, Resource> };
function effectiveEnv(deployment: Resource, container: Resource, names: Set<string>, resources: Resource[]): { env: Map<string, Resource>; error?: string } {
  const env = new Map<string, Resource>();
  const configMap = (name: string) => resources.find(resource => resource.kind === "ConfigMap" && resource.apiVersion === "v1" &&
    resource.metadata.name === name && namespace(resource) === namespace(deployment));
  for (const source of container.envFrom ?? []) {
    if (!object(source) || !object(source.configMapRef) || source.secretRef !== undefined ||
        Object.keys(source).some(key => !["prefix", "configMapRef"].includes(key)) ||
        source.prefix !== undefined && typeof source.prefix !== "string") return { env, error: "installation_env_from_unproven" };
    const map = configMap(source.configMapRef.name);
    if (!map) return { env, error: "installation_config_map_missing" };
    if (map.data !== undefined && (!object(map.data) || Object.values(map.data).some(value => typeof value !== "string")))
      return { env, error: "installation_config_map_invalid" };
    for (const [key, value] of Object.entries(map.data ?? {})) {
      const name = (source.prefix ?? "") + key;
      if (names.has(name)) env.set(name, { name, value });
    }
  }
  const seen = new Set<string>();
  for (const entry of container.env ?? []) {
    if (seen.has(entry.name)) return { env, error: "installation_env_ambiguous" };
    seen.add(entry.name);
    if (!names.has(entry.name)) continue;
    const ref = entry.valueFrom?.configMapKeyRef;
    if (ref) {
      const map = configMap(ref.name);
      if (!map || !object(map.data) || !Object.hasOwn(map.data, ref.key) || typeof map.data[ref.key] !== "string")
        return { env, error: "installation_config_map_key_unproven" };
      env.set(entry.name, { ...entry, valueFrom: undefined, value: map.data[ref.key] });
    } else env.set(entry.name, entry);
  }
  return { env };
}

function envEntry(process: InstalledProcess, name: string) {
  return process.env.get(name);
}
function configured(entry: Resource | undefined, semantics: "raw-nonempty" | "trimmed-nonempty") {
  return entry !== undefined && (entry.valueFrom !== undefined || typeof entry.value === "string" &&
    (semantics === "raw-nonempty" ? entry.value : trimRuntimeSpace(entry.value)) !== "");
}

function secretReference(entry: Resource | undefined): SecretRef | undefined {
  const ref = entry?.valueFrom?.secretKeyRef;
  if (!object(ref) || entry!.value !== undefined || Object.keys(entry!.valueFrom).length !== 1 ||
      ref.optional !== undefined && ref.optional !== false ||
      typeof ref.name !== "string" || ref.name.length > 253 || !/^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$/.test(ref.name) ||
      typeof ref.key !== "string" || ref.key.length > 253 || !/^[-._a-zA-Z0-9]+$/.test(ref.key)) return;
  return { name: ref.name, key: ref.key };
}

function namespace(resource: Resource): string { return resource.metadata.namespace ?? "default"; }
function validPort(value: unknown): value is number { return Number.isInteger(value) && Number(value) > 0 && Number(value) <= 65535; }
function labelsMatch(selector: Resource, labels: Resource) {
  return Object.entries(selector).every(([key, value]) => typeof value === "string" && Object.hasOwn(labels, key) && labels[key] === value);
}
function controller(resource: Resource, kind: string, resources: Resource[]): Resource | undefined {
  const refs = resource.metadata.ownerReferences;
  if (!Array.isArray(refs)) return;
  const owners = refs.filter(ref => object(ref) && ref.controller === true);
  if (owners.length !== 1 || owners[0].kind !== kind || typeof owners[0].uid !== "string" || !owners[0].uid) return;
  const ref = owners[0];
  return resources.find(candidate => candidate.kind === kind && candidate.apiVersion === ref.apiVersion &&
    namespace(candidate) === namespace(resource) && candidate.metadata.name === ref.name && candidate.metadata.uid === ref.uid);
}
// A live Deployment's generated replicas are not independent Service producers.
// Names or selectors alone do not establish this relationship; every hop must
// resolve the Kubernetes controller reference to the exact object UID.
function ownedBackend(resource: Resource, target: Resource, resources: Resource[]): boolean {
  if (resource === target) return true;
  if (resource.kind === "ReplicaSet") return controller(resource, "Deployment", resources) === target;
  if (resource.kind === "Pod") {
    const replicaSet = controller(resource, "ReplicaSet", resources);
    return replicaSet !== undefined && controller(replicaSet, "Deployment", resources) === target;
  }
  return false;
}
function normalizedPodField(spec: Resource, field: string): unknown {
  const value = spec[field];
  if (["hostNetwork", "hostPID", "hostIPC", "shareProcessNamespace"].includes(field)) return value === true;
  if (field === "hostUsers") return value === undefined ? true : value;
  if (field === "serviceAccountName") return value ?? "default";
  if (field === "restartPolicy") return value ?? "Always";
  if (field === "dnsPolicy") return value ?? "ClusterFirst";
  if (field === "enableServiceLinks") return value === undefined ? true : value;
  if (field === "securityContext" && object(value) && Object.keys(value).length === 0) return undefined;
  if (field === "volumes" && Array.isArray(value)) {
    const filtered = value.filter(volume => !(object(volume) && /^kube-api-access-[a-z0-9-]+$/.test(volume.name ?? "") &&
      object(volume.projected) && Array.isArray(volume.projected.sources) && volume.projected.sources.some((source: unknown) => object(source) && source.serviceAccountToken !== undefined)));
    return filtered.length === 0 ? undefined : filtered;
  }
  return value;
}
function samePodSecurityShape(live: Resource, declared: Resource): boolean {
  const fields = [
    "hostNetwork", "hostPID", "hostIPC", "shareProcessNamespace", "hostUsers", "serviceAccountName", "runtimeClassName",
    "automountServiceAccountToken", "securityContext", "volumes", "imagePullSecrets", "hostAliases",
    "dnsConfig", "dnsPolicy", "enableServiceLinks", "restartPolicy"
  ];
  const booleanFields = ["hostNetwork", "hostPID", "hostIPC", "shareProcessNamespace", "hostUsers", "automountServiceAccountToken", "enableServiceLinks"];
  if (booleanFields.some(field => [live[field], declared[field]].some(value => value !== undefined && typeof value !== "boolean"))) return false;
  return fields.every(field => deepEqual(normalizedPodField(live, field), normalizedPodField(declared, field)));
}
const liveContainerFields = [
  "name", "image", "command", "args", "env", "envFrom", "ports", "securityContext", "volumeMounts",
  "volumeDevices", "lifecycle", "readinessProbe", "livenessProbe", "startupProbe", "resources", "workingDir",
  "stdin", "stdinOnce", "tty", "restartPolicy", "imagePullPolicy", "resizePolicy", "terminationMessagePath",
  "terminationMessagePolicy"
];
function normalizedContainerField(container: Resource, field: string): unknown {
  const value = container[field];
  if (field === "imagePullPolicy" && value === undefined) {
    const image = typeof container.image === "string" ? container.image : "";
    if (image.includes("@")) return "IfNotPresent";
    const tag = image.slice(image.lastIndexOf("/") + 1).split("@")[0].split(":")[1];
    return tag === undefined || tag === "latest" ? "Always" : "IfNotPresent";
  }
  if (field === "terminationMessagePath") return value ?? "/dev/termination-log";
  if (field === "terminationMessagePolicy") return value ?? "File";
  return value;
}
function sameContainerShape(live: Resource, declared: Resource): boolean {
  return liveContainerFields.every(field => deepEqual(normalizedContainerField(live, field), normalizedContainerField(declared, field)));
}
function sameContainerList(live: unknown, declared: unknown): boolean {
  if (!Array.isArray(live) || !Array.isArray(declared) || live.length !== declared.length) return false;
  const declaredByName = new Map(declared.map(container => [container.name, container]));
  return live.every(container => object(container) && declaredByName.has(container.name) && sameContainerShape(container, declaredByName.get(container.name)!));
}
function sameLivePodShape(pod: Resource, deployment: Resource): boolean {
  const declared = deployment.spec.template.spec;
  return samePodSecurityShape(pod.spec, declared) &&
    sameContainerList(pod.spec.containers, declared.containers) &&
    sameContainerList(pod.spec.initContainers ?? [], declared.initContainers ?? []);
}
function servicePublishesNotReady(service: Resource): boolean {
  return service.spec?.publishNotReadyAddresses === true;
}
function endpointIsServiceable(endpoint: Resource, service: Resource): boolean {
  const conditions = endpoint.conditions;
  if (conditions === undefined) return true;
  if (!object(conditions) || Object.keys(conditions).some(key => !["ready", "serving", "terminating"].includes(key)) ||
      ["ready", "serving", "terminating"].some(key => conditions[key] !== undefined && typeof conditions[key] !== "boolean")) return false;
  if (conditions.ready === false && !servicePublishesNotReady(service)) return false;
  if (conditions.serving === false) return false;
  if (conditions.terminating === true && conditions.serving !== true) return false;
  return true;
}
function endpointBackend(resource: Resource, service: Resource, target: InstalledProcess, servicePort: Resource, port: number, resources: Resource[], topology: InstallationTopology): boolean {
  const provesPort = (ports: unknown): boolean => {
    if (!Array.isArray(ports)) return false;
    const matching = ports.filter(entry => object(entry) && (entry.name ?? "") === (servicePort.name ?? "") && (entry.protocol ?? "TCP") === "TCP");
    return matching.length === 1 && matching[0].port === port;
  };
  const provesPod = (ref: unknown, addresses: unknown, addressType?: "IPv4" | "IPv6"): boolean => {
    if (!object(ref) || ref.kind !== "Pod" || ref.apiVersion !== undefined && ref.apiVersion !== "v1" ||
        typeof ref.uid !== "string" || !ref.uid || (ref.namespace ?? namespace(service)) !== namespace(service) ||
        !Array.isArray(addresses) || addresses.length === 0) return false;
    const pod = resources.find(candidate => candidate.kind === "Pod" && candidate.apiVersion === "v1" &&
      namespace(candidate) === namespace(service) && candidate.metadata.name === ref.name && candidate.metadata.uid === ref.uid);
    if (!pod || !ownedBackend(pod, target.deployment, resources) || !labelsMatch(service.spec.selector, pod.metadata.labels ?? {})) return false;
    const validLiveContainer = (container: unknown): container is Resource => {
      if (!object(container) || typeof container.name !== "string" || typeof container.image !== "string") return false;
      if (container.command !== undefined && (!Array.isArray(container.command) || container.command.some((arg: unknown) => typeof arg !== "string"))) return false;
      if (container.args !== undefined && (!Array.isArray(container.args) || container.args.some((arg: unknown) => typeof arg !== "string"))) return false;
      if (container.env !== undefined && (!Array.isArray(container.env) || container.env.some((entry: unknown) => !object(entry) ||
          typeof entry.name !== "string" || entry.value !== undefined && typeof entry.value !== "string" ||
          entry.valueFrom !== undefined && !object(entry.valueFrom) || entry.value !== undefined && entry.valueFrom !== undefined))) return false;
      return container.ports === undefined || Array.isArray(container.ports) &&
        container.ports.every((entry: unknown) => object(entry) && validPort(entry.containerPort));
    };
    if (!object(pod.spec) || !Array.isArray(pod.spec.containers) || pod.spec.containers.length === 0 ||
        pod.spec.containers.some((container: unknown) => !validLiveContainer(container))) return false;
    const initContainers = pod.spec.initContainers;
    if (initContainers !== undefined && (!Array.isArray(initContainers) ||
        initContainers.some((container: unknown) => !validLiveContainer(container)))) return false;
    const liveContainers = pod.spec.containers;
    const liveInitContainers = pod.spec.initContainers ?? [];
    const targetName = target.container.name;
    const liveTarget = liveContainers.find((container: Resource) => container.name === targetName);
    if (!liveTarget || !sameLivePodShape(pod, target.deployment) || !sameContainerShape(liveTarget, target.container)) return false;
    if (new Set(liveContainers.map((container: Resource) => container.name)).size !== liveContainers.length) return false;
    const listenerEnvNames = new Set(topology.processes.flatMap(process => [process.addressEnv, process.rpcAddressEnv, process.rpcEnabledByEnv,
      ...(process.additionalListeners ?? []).map(listener => listener.addressEnv)].filter((name): name is string => Boolean(name))));
    const competingLiveContainers = [...liveContainers.filter((container: Resource) => container.name !== targetName),
      ...liveInitContainers.filter((container: Resource) => container.restartPolicy === "Always")];
    if (competingLiveContainers.some((container: Resource) => {
      const resolved = effectiveEnv(target.deployment, container, listenerEnvNames, resources);
      return resolved.error !== undefined || [...resolved.env.keys()].some(name => listenerEnvNames.has(name));
    })) return false;
    if (competingLiveContainers.some((container: Resource) => (container.ports ?? []).some((entry: Resource) =>
      (entry.protocol ?? "TCP") === "TCP" && entry.containerPort === port))) return false;
    const podIP = pod.status?.podIP;
    if (typeof podIP !== "string" || isIP(podIP) === 0) return false;
    const podIPs = pod.status?.podIPs;
    if (podIPs !== undefined && (!Array.isArray(podIPs) || podIPs.some((entry: unknown) => !object(entry) || typeof entry.ip !== "string" || isIP(entry.ip) === 0) ||
        !podIPs.some((entry: Resource) => entry.ip === podIP))) return false;
    const ips = new Set([podIP, ...(podIPs ?? []).map((entry: Resource) => entry.ip)]);
    const expectedFamily = addressType === "IPv4" ? 4 : addressType === "IPv6" ? 6 : undefined;
    return addresses.every(address => typeof address === "string" && isIP(address) !== 0 &&
      (expectedFamily === undefined || isIP(address) === expectedFamily) && ips.has(address));
  };
  if (resource.kind === "EndpointSlice") {
    return resource.apiVersion === "discovery.k8s.io/v1" && controller(resource, "Service", resources) === service &&
      (resource.addressType === "IPv4" || resource.addressType === "IPv6") &&
      Array.isArray(resource.endpoints) && resource.endpoints.length > 0 && resource.endpoints.every((endpoint: unknown) =>
        object(endpoint) && endpointIsServiceable(endpoint, service) && provesPort(resource.ports) && provesPod(endpoint.targetRef, endpoint.addresses, resource.addressType));
  }
  // The legacy Endpoints controller does not attach a Service ownerReference.
  // Its exact Service name plus every Pod UID and address must still agree.
  if (resource.apiVersion !== "v1" || !Array.isArray(resource.subsets) || resource.subsets.length === 0) return false;
  return resource.subsets.every((subset: unknown) => {
    if (!object(subset)) return false;
    const addresses = subset.addresses === undefined ? [] : Array.isArray(subset.addresses) ? subset.addresses : undefined;
    const notReadyAddresses = subset.notReadyAddresses === undefined ? [] :
      Array.isArray(subset.notReadyAddresses) ? subset.notReadyAddresses : undefined;
    if (notReadyAddresses && notReadyAddresses.length > 0 && !servicePublishesNotReady(service)) return false;
    const entries = addresses && notReadyAddresses ? [...addresses, ...notReadyAddresses] : undefined;
    return entries !== undefined && entries.length > 0 && entries.every(entry =>
      object(entry) && provesPort(subset.ports) && provesPod(entry.targetRef, [entry.ip]));
  });
}
function parseServiceAddress(raw: unknown, callerNamespace: string): { name: string; namespace: string; port: number } | undefined {
  if (typeof raw !== "string") return;
  const address = /^(?:dns:\/\/\/)?([a-z0-9](?:[-a-z0-9.]*[a-z0-9])?):([0-9]+)$/.exec(raw);
  if (!address || !validPort(Number(address[2]))) return;
  const parts = address[1].split(".");
  // Numeric IPs, arbitrary search domains and resolver authorities do not prove
  // a Kubernetes Service identity. DNS service names are explicit installation facts.
  if (parts.some(part => !/^[a-z](?:[-a-z0-9]*[a-z0-9])?$/.test(part))) return;
  if (![1, 2, 3, 5].includes(parts.length) || parts.length >= 3 && parts[2] !== "svc" ||
      parts.length === 5 && (parts[3] !== "cluster" || parts[4] !== "local")) return;
  return { name: parts[0], namespace: parts[1] ?? callerNamespace, port: Number(address[2]) };
}

// Native producers are interpreted by API identity and their exact Pod-template
// path. Missing/malformed matching facts never establish a unique backend.
function backendLabels(resource: Resource): Resource | null | undefined {
  let metadata: unknown;
  if (resource.apiVersion === "v1" && resource.kind === "Pod") metadata = resource.metadata;
  else if (resource.apiVersion === "apps/v1" && ["Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"].includes(resource.kind) ||
           resource.apiVersion === "v1" && resource.kind === "ReplicationController" ||
           resource.apiVersion === "batch/v1" && resource.kind === "Job") metadata = resource.spec?.template?.metadata;
  else if (resource.apiVersion === "batch/v1" && resource.kind === "CronJob") metadata = resource.spec?.jobTemplate?.spec?.template?.metadata;
  else {
    // These APIs do not create application Pods. Unknown APIs are deliberately
    // not classified by labels, resource names, or the shape of a spec.
    const nonProducers: Record<string, string[]> = {
      v1: ["Namespace", "Node", "Service", "ConfigMap", "Secret", "ServiceAccount", "PersistentVolumeClaim", "PersistentVolume", "Endpoints", "PodTemplate", "ResourceQuota", "LimitRange", "Event"],
      "admissionregistration.k8s.io/v1": ["MutatingWebhookConfiguration", "ValidatingWebhookConfiguration"],
      "apiextensions.k8s.io/v1": ["CustomResourceDefinition"],
      "apps/v1": ["ControllerRevision"],
      "certificates.k8s.io/v1": ["CertificateSigningRequest"],
      "coordination.k8s.io/v1": ["Lease"],
      "events.k8s.io/v1": ["Event"],
      "networking.k8s.io/v1": ["NetworkPolicy", "Ingress", "IngressClass"],
      "metrics.k8s.io/v1beta1": ["PodMetrics", "NodeMetrics"],
      "rbac.authorization.k8s.io/v1": ["Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding"],
      "storage.k8s.io/v1": ["StorageClass", "CSIDriver", "CSINode", "CSIStorageCapacity", "VolumeAttachment"],
      "policy/v1": ["PodDisruptionBudget"],
      "autoscaling/v1": ["HorizontalPodAutoscaler"],
      "autoscaling/v2": ["HorizontalPodAutoscaler"],
      "discovery.k8s.io/v1": ["EndpointSlice"],
      "cloud.tencent.com/v1alpha1": ["TkeServiceConfig"],
    };
    return Object.hasOwn(nonProducers, resource.apiVersion) && nonProducers[resource.apiVersion].includes(resource.kind) ? null : undefined;
  }
  if (metadata === undefined) {
    // PodTemplate metadata is optional; a present template without metadata has
    // known empty labels, whereas an absent template has unknown facts.
    const template = resource.kind === "CronJob" ? resource.spec?.jobTemplate?.spec?.template : resource.spec?.template;
    return object(template) ? {} : undefined;
  }
  if (!object(metadata) || metadata.labels !== undefined && !stringMap(metadata.labels)) return;
  return metadata.labels ?? {};
}
// Go strings.TrimSpace uses Unicode White_Space, which includes NEL (U+0085)
// and excludes BOM (U+FEFF); JavaScript trim() has the opposite behavior.
function trimRuntimeSpace(value: string): string {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}
function rpcEnabled(process: InstalledProcess): boolean {
  const { contract } = process;
  if (!contract.rpcEnabledByEnv) return true;
  const entry = envEntry(process, contract.rpcEnabledByEnv);
  // An unresolved Secret must never hide a potentially active RPC caller.
  if (entry?.valueFrom !== undefined) return true;
  if (typeof entry?.value !== "string") return false;
  return contract.rpcEnabledByEnvSemantics === "raw-nonempty" ? entry.value !== "" : trimRuntimeSpace(entry.value) !== "";
}
function listenerAddress(process: InstalledProcess, name: string, defaultAddress: string | undefined, trim: boolean, portEnv?: string): string | undefined {
  const entry = envEntry(process, name);
  if (entry?.valueFrom !== undefined) return;
  const value = typeof entry?.value === "string" ? trim ? trimRuntimeSpace(entry.value) : entry.value : "";
  if (value !== "") return value;
  if (portEnv) {
    const port = envEntry(process, portEnv);
    if (port?.valueFrom !== undefined) return;
    if (typeof port?.value === "string" && port.value !== "") return `:${port.value}`;
  }
  return defaultAddress;
}
function rpcListenerAddress(process: InstalledProcess): string | undefined {
  const { contract } = process;
  return listenerAddress(process, contract.rpcAddressEnv ?? contract.addressEnv,
    contract.rpcAddressEnv ? contract.defaultRPCAddress : contract.defaultAddress, contract.rpcAddressEnv !== undefined || contract.kind === "ownerservice");
}
type ListenerBinding = { host: string; port: number; wildcard: boolean };
function listenerBinding(address: string | undefined): ListenerBinding | undefined {
  const match = typeof address === "string" && /^(\[[0-9a-fA-F:.]+\]|[0-9.]*):([0-9]+)$/.exec(address);
  if (!match || !validPort(Number(match[2]))) return;
  const rawHost = match[1].startsWith("[") ? match[1].slice(1, -1) : match[1];
  if (rawHost !== "" && !isIP(rawHost)) return;
  const host = rawHost.includes(":") ? new URL(`http://[${rawHost}]`).hostname : rawHost;
  return { host, port: Number(match[2]), wildcard: ["", "0.0.0.0", "[::]"].includes(host) };
}
function podListenerError(deployment: Resource, processes: InstalledProcess[], resources: Resource[], topology: InstallationTopology): string | undefined {
  const bindings: ListenerBinding[] = [];
  const add = (address: string | undefined, required: boolean) => {
    const binding = listenerBinding(address);
    if (!binding) {
      if (required || processes.length > 1) return "installation_listener_unproven";
      return;
    }
    if (bindings.some(other => other.port === binding.port && (other.wildcard || binding.wildcard || other.host === binding.host)))
      return "installation_listener_conflict";
    bindings.push(binding);
  };
  for (const process of processes) {
    const { contract } = process;
    const addresses: (string | undefined)[] = [];
    if (contract.rpcAddressEnv || rpcEnabled(process)) addresses.push(listenerAddress(process, contract.addressEnv, contract.defaultAddress,
      contract.kind === "ownerservice", contract.addressPortEnv));
    if (contract.rpcAddressEnv && rpcEnabled(process)) addresses.push(rpcListenerAddress(process));
    for (const result of addresses.map(address => add(address, false))) if (result) return result;
    for (const listener of contract.additionalListeners ?? []) {
      const result = add(listenerAddress(process, listener.addressEnv, listener.defaultAddress, listener.valueSemantics === "trimmed"), true);
      if (result) return result;
    }
  }
  const additional = topology.processes.flatMap(process => process.additionalListeners ?? []);
  const listenerContainers = [
    ...deployment.spec.template.spec.containers,
    ...(deployment.spec.template.spec.initContainers ?? []).filter((container: Resource) => container.restartPolicy === "Always")
  ];
  for (const container of listenerContainers) {
    if (processes.some(process => process.container === container)) continue;
    const names = new Set(additional.map(listener => listener.addressEnv));
    const resolved = effectiveEnv(deployment, container, names, resources);
    if (resolved.error) return "installation_listener_unproven";
    for (const listener of additional) {
      const entry = resolved.env.get(listener.addressEnv);
      if (entry === undefined) continue;
      const result = add(listenerAddress({ contract: { additionalListeners: [], addressEnv: "", defaultAddress: "" } as ProcessContract,
        deployment, container, env: resolved.env }, listener.addressEnv, undefined, listener.valueSemantics === "trimmed"), true);
      if (result) return result;
    }
  }
}


export function validateInstallationManifest(manifest: unknown, topology: InstallationTopology): InstallationReport {
  const security = installationSecurity(topology);
  if (!security) return installationFailure("installation_topology_security_invalid");
  const failure = (code: string) => installationFailure(code, security);
  if (topology.processes.some(process => !Array.isArray(process.daemonArgs) || process.daemonArgs.some(arg => typeof arg !== "string") ||
      process.rpcEnabledByEnv && !["raw-nonempty", "trimmed-nonempty"].includes(process.rpcEnabledByEnvSemantics!)))
    return failure("installation_topology_process_invalid");
  if (topology.communication.edges.some(edge => edge.activation === "configured" &&
      !["raw-nonempty", "trimmed-nonempty"].includes(edge.configuredAddressSemantics!)))
    return failure("installation_topology_edge_invalid");
  if (!validList(manifest)) return failure("installation_manifest_shape_invalid");
  const report: InstallationReport = { ok: true, errors: [], communication: { edges: [], security } };
  const resources: Resource[] = manifest.items;
  const installed = new Map<string, InstalledProcess>();
  const resourceKeys = new Set<string>();
  for (const resource of resources) {
    const key = JSON.stringify([resource.kind, namespace(resource), resource.metadata.name]);
    if (resourceKeys.has(key)) return failure("installation_resource_duplicate");
    resourceKeys.add(key);
  }
  const deployments = resources.filter(resource => resource.kind === "Deployment");
  const productImages = new Set<string>(deployments.flatMap(deployment =>
    deployment.spec.template.spec.containers.filter((container: Resource) =>
      topology.processes.some(process => container.command?.[0] === process.binary))
      .map((container: Resource) => container.image)));
  const processContract = (container: Resource) => {
    const argv = container.command?.length ? [...container.command, ...(container.args ?? [])] :
      productImages.has(container.image) && !container.args?.length ? topology.image.defaultCommand : undefined;
    return argv && topology.processes.find(process => argv[0] === process.binary &&
      Array.isArray(process.daemonArgs) && argv.length === process.daemonArgs.length + 1 &&
      process.daemonArgs.every((arg, index) => argv[index + 1] === arg));
  };
  const podProcesses = new Map<Resource, InstalledProcess[]>();
  const recognized = new Set<Resource>();
  const processes = new Map<Resource, InstalledProcess>();
  const productNamespaces = new Set<string>();
  for (const deployment of deployments) {
    for (const container of deployment.spec.template.spec.containers) {
      const contract = processContract(container);
      if (!contract) continue;
      recognized.add(container);
      productImages.add(container.image);
      productNamespaces.add(namespace(deployment));
      if (deployment.spec.template.spec.hostNetwork === true) return failure("installation_pod_network_unsupported");
      const names = new Set([contract.addressEnv, contract.rpcAddressEnv, contract.peerTokensEnv, contract.rpcEnabledByEnv, contract.addressPortEnv,
        ...(contract.additionalListeners ?? []).map(listener => listener.addressEnv)].filter((name): name is string => Boolean(name)));
      for (const edge of topology.communication.edges.filter(edge => edge.caller === contract.transportPrincipal)) {
        names.add(edge.addressEnv); names.add(edge.tokenEnv);
        if (edge.activationEnv) names.add(edge.activationEnv);
      }
      const resolved = effectiveEnv(deployment, container, names, resources);
      if (resolved.error) return failure(resolved.error);
      const process = { contract, deployment, container, env: resolved.env };
      processes.set(container, process);
      const siblings = podProcesses.get(deployment) ?? [];
      siblings.push(process); podProcesses.set(deployment, siblings);
      if (!contract.transportPrincipal || !rpcEnabled(process)) continue;
      if (installed.has(contract.transportPrincipal!)) return failure("installation_process_ambiguous");
      installed.set(contract.transportPrincipal!, process);
    }
  }
  for (const deployment of deployments) {
    for (const container of deployment.spec.template.spec.containers) {
      if (productImages.has(container.image) && !recognized.has(container)) {
        return failure("installation_process_unproven");
      }
    }
  }
  if (recognized.size === 0) return failure("installation_process_missing");
  if (productNamespaces.size !== 1) return failure("installation_namespace_unsupported");
  for (const [deployment, processes] of podProcesses.entries()) {
    const error = podListenerError(deployment, processes, resources, topology);
    if (error) return failure(error);
  }
  for (const edge of topology.communication.edges) {
    const caller = installed.get(edge.caller);
    if (!caller) continue;
    const address = envEntry(caller, edge.addressEnv);
    const addressSemantics = edge.activation === "configured" ? edge.configuredAddressSemantics! : "trimmed-nonempty";
    // Native address activation varies per caller; tokens and the extra
    // activation variable use Go TrimSpace. Keep address-or-token admission.
    if (edge.activation === "configured" && !configured(address, addressSemantics) && !configured(envEntry(caller, edge.tokenEnv), "trimmed-nonempty") &&
        !(edge.activationEnv && configured(envEntry(caller, edge.activationEnv), "trimmed-nonempty"))) continue;
    const fail = (code: string) => report.errors.push({ code, service: caller.contract.service,
      caller: edge.caller, target: edge.target, addressEnv: edge.addressEnv, tokenEnv: edge.tokenEnv });
    if (!address || !configured(address, addressSemantics)) { fail("rpc_address_required"); continue; }
    const addressValue = edge.activation === "configured" && addressSemantics === "trimmed-nonempty" && typeof address.value === "string"
      ? trimRuntimeSpace(address.value) : address.value;
    const parsed = parseServiceAddress(addressValue, namespace(caller.deployment));
    if (!parsed) { fail("rpc_address_invalid"); continue; }
    const target = installed.get(edge.target);
    if (!target) { fail("rpc_target_missing"); continue; }
    const service = resources.find(resource => resource.kind === "Service" && resource.metadata.name === parsed.name && namespace(resource) === parsed.namespace);
    if (!service) { fail("rpc_service_missing"); continue; }
    const selector = service.spec?.selector;
    if (service.apiVersion !== "v1" || !object(selector) || Object.keys(selector).length === 0 ||
        service.spec.type === "ExternalName" || namespace(service) !== namespace(target.deployment) ||
        !labelsMatch(selector, target.deployment.spec.template.metadata?.labels ?? {})) {
      fail("rpc_service_target_mismatch"); continue;
    }
    const backends: Resource[] = [];
    let backendUnproven = false;
    for (const resource of resources) {
      if (namespace(resource) !== namespace(service)) continue;
      const labels = backendLabels(resource);
      if (labels === undefined) backendUnproven = true;
      else if (labels !== null && labelsMatch(selector, labels)) backends.push(resource);
    }
    if (backendUnproven) { fail("rpc_service_backend_unproven"); continue; }
    if (backends.some(backend => !ownedBackend(backend, target.deployment, resources))) {
      fail("rpc_service_backend_ambiguous"); continue;
    }
    const matching = Array.isArray(service.spec.ports) ? service.spec.ports.filter((entry: Resource) => entry?.port === parsed.port && (entry.protocol ?? "TCP") === "TCP") : [];
    if (matching.length !== 1) { fail("rpc_service_port_invalid"); continue; }
    const servicePort = matching[0];
    let targetPort = servicePort.targetPort ?? servicePort.port;
    if (typeof targetPort === "string") {
      const ports = target.deployment.spec.template.spec.containers.flatMap((container: Resource) =>
        (container.ports ?? []).filter((entry: Resource) => entry?.name === targetPort && (entry.protocol ?? "TCP") === "TCP")
          .map((entry: Resource) => ({ ...entry, container })));
      if (ports.length !== 1 || ports[0].container !== target.container) { fail("rpc_target_port_invalid"); continue; }
      targetPort = ports[0].containerPort;
    }
    if (!validPort(targetPort)) { fail("rpc_target_port_invalid"); continue; }
    // Listener defaulting follows native configuration independently of the
    // optional RPC activation rule; unresolved values cannot select a default.
    const listener = rpcListenerAddress(target);
    const listenMatch = typeof listener === "string" && /^(?:|0\.0\.0\.0|\[::\]):([0-9]+)$/.exec(listener);
    if (!listenMatch || Number(listenMatch[1]) !== targetPort) { fail("rpc_target_listener_invalid"); continue; }
    const pod = target.deployment.spec.template.spec;
    const competitors = [...pod.containers, ...(pod.initContainers ?? []).filter((container: Resource) => container.restartPolicy === "Always")];
    const otherListener = target.contract.rpcAddressEnv ? listenerAddress(target, target.contract.addressEnv, target.contract.defaultAddress, target.contract.kind === "ownerservice", target.contract.addressPortEnv) : null;
    let conflict = otherListener === undefined || otherListener !== null && Number(/:([0-9]+)$/.exec(otherListener)?.[1]) === targetPort;
    for (const container of competitors) {
      if (container === target.container) continue;
      if ((container.ports ?? []).some((port: Resource) => (port.protocol ?? "TCP") === "TCP" &&
          (port.containerPort === targetPort || typeof servicePort.targetPort === "string" && port.name === servicePort.targetPort))) {
        conflict = true; break;
      }
      const contract = processContract(container);
      if (!contract) continue;
      const resolved = processes.get(container) ?? (() => {
        const result = effectiveEnv(target.deployment, container,
          new Set([contract.addressEnv, contract.rpcAddressEnv, contract.rpcEnabledByEnv, contract.addressPortEnv, ...(contract.additionalListeners ?? []).map(listener => listener.addressEnv)].filter((name): name is string => Boolean(name))), resources);
        return result.error ? undefined : { contract, deployment: target.deployment, container, env: result.env };
      })();
      if (!resolved) { conflict = true; break; }
      const addresses: (string | undefined)[] = [];
      const enabled = rpcEnabled(resolved);
      if (contract.rpcAddressEnv || enabled) addresses.push(listenerAddress(resolved, contract.addressEnv, contract.defaultAddress, contract.kind === "ownerservice", contract.addressPortEnv));
      if (contract.rpcAddressEnv && enabled) addresses.push(rpcListenerAddress(resolved));
      for (const listener of contract.additionalListeners ?? []) {
        if (envEntry(resolved, listener.addressEnv) || listener.defaultAddress !== undefined) addresses.push(listenerAddress(resolved, listener.addressEnv, listener.defaultAddress, listener.valueSemantics === "trimmed"));
      }
      if (addresses.some(address => address === undefined || Number(/:([0-9]+)$/.exec(address)?.[1]) === targetPort)) {
        conflict = true; break;
      }
    }
    const endpoints = resources.filter(resource => namespace(resource) === namespace(service) &&
      (resource.kind === "Endpoints" && resource.metadata.name === service.metadata.name ||
       resource.kind === "EndpointSlice" && resource.metadata.labels?.["kubernetes.io/service-name"] === service.metadata.name));
    if (conflict || endpoints.some(resource => !endpointBackend(resource, service, target, servicePort, targetPort, resources, topology))) {
      fail("rpc_service_backend_ambiguous"); continue;
    }
    const tokenRef = secretReference(envEntry(caller, edge.tokenEnv));
    if (!tokenRef) { fail("rpc_token_ref_invalid"); continue; }
    const peerRef = secretReference(envEntry(target, target.contract.peerTokensEnv!));
    if (!peerRef) { fail("rpc_peer_ref_invalid"); continue; }
    const policies = communicationPolicyProof(caller, target, targetPort, resources);
    if (policies.error) { fail(policies.error); continue; }
    report.communication.edges.push({
      caller: edge.caller, target: edge.target, addressEnv: edge.addressEnv, tokenEnv: edge.tokenEnv,
      callerDeployment: caller.deployment.metadata.name, targetDeployment: target.deployment.metadata.name, port: targetPort,
      tokenRef, peerRef, serviceRef: { name: service.metadata.name, namespace: namespace(service) },
      ingressPolicies: policies.ingressPolicies, egressPolicies: policies.egressPolicies
    });
  }
  report.ok = report.errors.length === 0;
  if (!report.ok) report.communication.edges = [];
  return report;
}
