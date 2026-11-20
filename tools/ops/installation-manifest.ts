import { isIP } from "node:net";
import { isDeepStrictEqual } from "node:util";
import { communicationPolicyProof } from "./installation-network-policy.ts";

// Product installation admission. Business communication requirements are read
// only from this checkout's portable topology, never inferred from env names.
export type SecretRef = { name: string; key: string };
export type CommunicationEdge = {
  caller: string; target: string; addressEnv: string; tokenEnv: string;
  callerDeployment: string; targetDeployment: string; port: number;
  tokenRef: SecretRef; peerRef: SecretRef; serviceRef?: { name: string; namespace: string }; bindAddress?: { host: string; port: number }; ingressPolicies: string[]; egressPolicies: string[];
};
export type InstallationError = { code: string; reasonCode?: string; service?: string; caller?: string; target?: string; addressEnv?: string; tokenEnv?: string };
export type InstallationSecurity = { tokenMinimumLength: number };
export type InstallationReport = { ok: boolean; errors: InstallationError[]; communication: { edges: CommunicationEdge[]; security: InstallationSecurity | null } };
export type ProcessContract = {
  service: string; kind: string; binary: string; daemonArgs: string[]; transportPrincipal?: string;
  rpcEnabledByEnvSemantics?: "raw-nonempty" | "trimmed-nonempty";
  addressPortEnv?: string; additionalListeners?: { addressEnv: string; defaultAddress?: string; valueSemantics: "raw" | "trimmed" }[];
  addressEnv: string; defaultAddress: string; rpcAddressEnv?: string; defaultRPCAddress?: string; peerTokensEnv?: string; rpcEnabledByEnv?: string;
  databaseEnv?: string | null; compositionDatabaseEnv?: string | null; rpcDatabaseEnv?: string;
};
export type EdgeContract = { caller: string; target: string; addressEnv: string; tokenEnv: string; activation: "required" | "configured"; activationEnvs?: string[]; configuredAddressSemantics?: "raw-nonempty" | "trimmed-nonempty" };
// The contract's deployment unit: one Deployment whose container runs every
// declared owner process in one process space.
export type DeploymentUnitContract = {
  name: string; kind: string; memberSource: string; imageSource: string; entrypoint?: string;
};
export type InstallationTopology = {
  image: { defaultCommand: string[]; env?: string };
  processes: ProcessContract[];
  communication: { schemaVersion: number; edges: EdgeContract[]; security: InstallationSecurity };
  deploymentUnit?: { rule?: string; unit: DeploymentUnitContract };
};
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
function validContainerList(value: unknown, required = false): value is Resource[] {
  return Array.isArray(value) && (!required || value.length > 0) && value.every(validContainer) &&
    new Set(value.map(container => container.name)).size === value.length;
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
    const initContainers = resource.spec?.template?.spec?.initContainers;
    const containers = resource.spec?.template?.spec?.containers;
    return resource.apiVersion === "apps/v1" && object(resource.spec?.template?.spec) &&
      (resource.spec.template.metadata?.labels === undefined || stringMap(resource.spec.template.metadata.labels)) &&
      validContainerList(containers, true) &&
      (initContainers === undefined || validContainerList(initContainers)) &&
      new Set([...containers, ...(initContainers ?? [])].map(container => container.name)).size === containers.length + (initContainers?.length ?? 0);
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
// A reference that resolves to another live Deployment is a Service/Deployment
// ownership fault. A ReplicaSet or Pod whose controller reference resolves to no
// live Deployment has instead lost the Pod ownership chain: the target may be the
// correct Deployment, so reporting that hop against the Service misdirects the
// operator. The two causes stay separate symbols because their repairs differ.
// A Pod whose ReplicaSet itself cannot be resolved has no provable
// controller chain to the target Deployment.
function backendOwnershipFailure(resource: Resource, target: Resource, resources: Resource[]): string | undefined {
  if (resource === target) return;
  if (resource.kind === "ReplicaSet") {
    const owner = controller(resource, "Deployment", resources);
    if (owner === target) return;
    return owner === undefined ? "pod_owner_chain_unproven" : "service_backend_owner";
  }
  if (resource.kind === "Pod") {
    const replicaSet = controller(resource, "ReplicaSet", resources);
    if (replicaSet === undefined) return "endpoint_pod_replicaset_unresolved";
    const owner = controller(replicaSet, "Deployment", resources);
    if (owner === target) return;
    return owner === undefined ? "pod_owner_chain_unproven" : "service_backend_owner";
  }
  return "service_backend_owner";
}
function productArgv(container: Resource, defaultCommand: string[] | undefined): string[] | undefined {
  return container.command?.length ? [...container.command, ...(container.args ?? [])] :
    !container.args?.length ? defaultCommand : undefined;
}

function servicePublishesNotReady(service: Resource): boolean {
  return service.spec?.publishNotReadyAddresses === true;
}
function endpointServiceability(endpoint: unknown, service: Resource): "serviceable" | "unserviceable" | undefined {
  if (!object(endpoint) || !Array.isArray(endpoint.addresses) || endpoint.addresses.length === 0 ||
      endpoint.addresses.some((address: unknown) => typeof address !== "string" || isIP(address) === 0)) return;
  const conditions = endpoint.conditions;
  if (conditions !== undefined && (!object(conditions) || Object.keys(conditions).some(key => !["ready", "serving", "terminating"].includes(key)) ||
      ["ready", "serving", "terminating"].some(key => conditions[key] !== undefined && typeof conditions[key] !== "boolean"))) return;
  if (conditions?.serving === false || conditions?.terminating === true ||
      conditions?.ready === false && !servicePublishesNotReady(service)) return "unserviceable";
  return "serviceable";
}
// The endpoint proof returns the operator-facing cause when an endpoint object
// cannot prove the target backend, and undefined when it can. Every non-empty
// return rejects the edge exactly where the former boolean predicate returned
// false, so admission decisions do not change while the receipt names one cause
// instead of collapsing several distinct faults into endpoint_backend.
function endpointBackendFailure(resource: Resource, service: Resource, target: Resource, targetContainer: Resource, servicePort: Resource, port: number, resources: Resource[], defaultCommand: string[],
  processContract: (container: Resource) => ProcessContract | undefined): string | undefined {
  const portFailure = (ports: unknown): string | undefined => {
    if (!Array.isArray(ports)) return "endpoint_port_unpublished";
    const matching = ports.filter(entry => object(entry) && (entry.name ?? "") === (servicePort.name ?? "") && (entry.protocol ?? "TCP") === "TCP");
    return matching.length === 1 && matching[0].port === port ? undefined : "endpoint_port_mismatch";
  };
  const podFailure = (ref: unknown, addresses: unknown, addressType?: "IPv4" | "IPv6"): string | undefined => {
    if (!object(ref) || ref.kind !== "Pod" || ref.apiVersion !== undefined && ref.apiVersion !== "v1") return "endpoint_target_invalid";
    if (typeof ref.uid !== "string" || !ref.uid) return "endpoint_target_unidentified";
    if ((ref.namespace ?? namespace(service)) !== namespace(service)) return "endpoint_target_namespace_mismatch";
    if (!Array.isArray(addresses) || addresses.length === 0) return "endpoint_address_invalid";
    const pod = resources.find(candidate => candidate.kind === "Pod" && candidate.apiVersion === "v1" &&
      namespace(candidate) === namespace(service) && candidate.metadata.name === ref.name && candidate.metadata.uid === ref.uid);
    if (!pod) return "endpoint_pod_absent";
    if (!labelsMatch(service.spec.selector, pod.metadata.labels ?? {})) return "endpoint_pod_selector_mismatch";
    const ownership = backendOwnershipFailure(pod, target, resources);
    if (ownership) return ownership;
    if (!validContainerList(pod.spec?.containers, true) ||
        pod.spec.initContainers !== undefined && !validContainerList(pod.spec.initContainers)) return "endpoint_pod_shape_unproven";
    const liveContainers = pod.spec.containers;
    const liveInitContainers = pod.spec.initContainers ?? [];
    const targetName = targetContainer.name;
    const liveTargets = liveContainers.filter((container: Resource) => container.name === targetName);
    // Prepare may project a new Deployment over still-running old replicas.
    // Resolve the target process entrypoint from its own ReplicaSet revision;
    // Candidate image identity belongs to the executor's running-image readback.
    const replicaSpec = controller(pod, "ReplicaSet", resources)?.spec?.template?.spec;
    if (!object(replicaSpec)) return "endpoint_pod_replicaset_unresolved";
    const replicaContainers = replicaSpec.containers;
    if (!validContainerList(replicaContainers, true)) return "endpoint_pod_template_containers_invalid";
    if (replicaSpec.initContainers !== undefined && !validContainerList(replicaSpec.initContainers))
      return "endpoint_pod_template_init_containers_invalid";
    const replicaTargets = replicaContainers.filter((container: unknown) => validContainer(container) && container.name === targetName);
    if (liveTargets.length !== 1 || replicaTargets.length !== 1) return "endpoint_pod_container_mismatch";
    const expectedArgv = productArgv(replicaTargets[0], defaultCommand);
    const liveArgv = productArgv(liveTargets[0], defaultCommand);
    if (!expectedArgv || !liveArgv || expectedArgv.length !== liveArgv.length ||
        expectedArgv.some((arg, index) => arg !== liveArgv[index])) return "endpoint_pod_command_mismatch";
    const competingLiveContainers = [...liveContainers.filter((container: Resource) => container?.name !== targetName),
      ...liveInitContainers.filter((container: Resource) => container?.restartPolicy === "Always")];
    for (const container of competingLiveContainers) {
      if ((container.ports ?? []).some((entry: Resource) => (entry.protocol ?? "TCP") === "TCP" &&
          (entry.containerPort === port || typeof servicePort.targetPort === "string" && entry.name === servicePort.targetPort))) return "endpoint_pod_port_conflict";
      const contract = processContract(container);
      if (!contract) continue;
      // The target of this proof is the Deployment itself, so the competing
      // container's environment is resolved against the target's own namespace.
      const result = effectiveEnv(target, container,
        new Set([contract.addressEnv, contract.rpcAddressEnv, contract.rpcEnabledByEnv, contract.addressPortEnv,
          ...(contract.additionalListeners ?? []).map(listener => listener.addressEnv)].filter((name): name is string => Boolean(name))), resources);
      if (result.error) return "endpoint_pod_listener_unproven";
      const process = { contract, deployment: target, container, env: result.env };
      const enabled = rpcEnabled(process);
      const competingAddresses: (string | undefined)[] = [];
      if (contract.rpcAddressEnv || enabled) competingAddresses.push(listenerAddress(process, contract.addressEnv, contract.defaultAddress, contract.kind === "ownerservice", contract.addressPortEnv));
      if (contract.rpcAddressEnv && enabled) competingAddresses.push(rpcListenerAddress(process));
      for (const listener of contract.additionalListeners ?? []) {
        if (envEntry(process, listener.addressEnv) || listener.defaultAddress !== undefined)
          competingAddresses.push(listenerAddress(process, listener.addressEnv, listener.defaultAddress, listener.valueSemantics === "trimmed"));
      }
      for (const address of competingAddresses) {
        if (address === undefined) return "endpoint_pod_listener_unproven";
        if (Number(/:([0-9]+)$/.exec(address)?.[1]) === port) return "endpoint_pod_port_conflict";
      }
    }
    if (competingLiveContainers.some((container: Resource) => (container?.ports ?? []).some((entry: Resource) =>
      (entry?.protocol ?? "TCP") === "TCP" && entry?.containerPort === port))) return "endpoint_pod_port_conflict";
    const podIP = pod.status?.podIP;
    if (typeof podIP !== "string" || isIP(podIP) === 0) return "endpoint_pod_ip_absent";
    const podIPs = pod.status?.podIPs;
    if (podIPs !== undefined && (!Array.isArray(podIPs) || podIPs.some((entry: unknown) => !object(entry) || typeof entry.ip !== "string" || isIP(entry.ip) === 0) ||
        podIPs[0]?.ip !== podIP)) return "endpoint_pod_ip_invalid";
    const ips = new Set([podIP, ...(podIPs ?? []).map((entry: Resource) => entry.ip)]);
    const expectedFamily = addressType === "IPv4" ? 4 : addressType === "IPv6" ? 6 : undefined;
    for (const address of addresses) {
      if (typeof address !== "string" || isIP(address) === 0) return "endpoint_address_invalid";
      if (!ips.has(address)) return "endpoint_address_mismatch";
      if (expectedFamily !== undefined && isIP(address) !== expectedFamily) return "endpoint_address_family_mismatch";
    }
    return;
  };
  if (resource.kind === "EndpointSlice") {
    if (resource.apiVersion !== "discovery.k8s.io/v1" ||
        resource.addressType !== "IPv4" && resource.addressType !== "IPv6") return "endpoint_slice_shape_invalid";
    if (controller(resource, "Service", resources) !== service) return "endpoint_slice_owner_mismatch";
    // An EndpointSlice that lists no address delivers no traffic, so it cannot
    // hand this Service to another workload and it proves nothing about the
    // target Deployment. Only a listed address carries a proof obligation. An
    // absent, null or empty list is admitted because the Deploy that creates the
    // backends is the only repair for a Service that has none, and refusing it
    // until those backends exist would make the fault permanent. A null list is
    // the API server's encoding of an empty list: the EndpointSlice endpoints
    // field carries no omitempty, so a slice that publishes no address is
    // written with a null list. A Service whose Pods are scaled to zero, or a
    // Service whose selector was just repointed at a backend that does not exist
    // yet, publishes its address that way.
    if (resource.endpoints === undefined || resource.endpoints === null) return;
    if (!Array.isArray(resource.endpoints)) return "endpoint_slice_shape_invalid";
    if (resource.endpoints.length === 0) return;
    const states = resource.endpoints.map((endpoint: unknown) => endpointServiceability(endpoint, service));
    if (states.some(state => state === undefined)) return "endpoint_address_invalid";
    for (const endpoint of resource.endpoints) {
      if (!object(endpoint)) return "endpoint_address_invalid";
      const portFailureReason = portFailure(resource.ports);
      if (portFailureReason) return portFailureReason;
      const podFailureReason = podFailure(endpoint.targetRef, endpoint.addresses, resource.addressType);
      if (podFailureReason) return podFailureReason;
    }
    // Every listed address proved its Pod, address and port. An endpoint that is
    // not serviceable receives no traffic either, so "no serviceable endpoint"
    // is the same published-nothing shape as an empty list, not an ambiguity.
    return;
  }
  // The legacy Endpoints controller does not attach a Service ownerReference.
  // Its exact Service name plus every Pod UID and address must still agree.
  if (resource.apiVersion !== "v1") return "endpoint_shape_invalid";
  // A null list is the API server's encoding of an empty list, so it lists
  // nothing exactly like an absent or empty one.
  if (resource.subsets === undefined || resource.subsets === null) return;
  if (!Array.isArray(resource.subsets)) return "endpoint_shape_invalid";
  for (const subset of resource.subsets) {
    if (!object(subset) || subset.addresses !== undefined && subset.addresses !== null && !Array.isArray(subset.addresses) ||
        subset.notReadyAddresses !== undefined && subset.notReadyAddresses !== null && !Array.isArray(subset.notReadyAddresses)) return "endpoint_shape_invalid";
  }
  // An Endpoints object that lists no entry at all — absent or empty subsets,
  // or every subset carrying empty address arrays — routes nothing and is
  // admitted for the same reason as an empty EndpointSlice. Listing an entry is
  // itself the proof obligation, independent of whether the Service currently
  // publishes it: a notReadyAddresses entry the Service does not publish still
  // names a Pod, so it must still be resolved against the same Pod, port,
  // owner chain, template and address proofs. Gating on publication instead of
  // listing would admit an unpublished entry whose targetRef names a foreign
  // or absent Pod without proving it.
  const listed = resource.subsets.some((subset: Resource) =>
    (subset.addresses ?? []).length > 0 || (subset.notReadyAddresses ?? []).length > 0);
  if (!listed) return;
  for (const subset of resource.subsets) {
    const allEntries = [...(subset.addresses ?? []), ...(subset.notReadyAddresses ?? [])];
    for (const entry of allEntries) {
      if (!object(entry)) return "endpoint_shape_invalid";
      const portFailureReason = portFailure(subset.ports);
      if (portFailureReason) return portFailureReason;
      const podFailureReason = podFailure(entry.targetRef, [entry.ip]);
      if (podFailureReason) return podFailureReason;
    }
  }
  return;
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
function listenerConfigured(process: InstalledProcess, name: string, trim: boolean, portEnv?: string): boolean {
  const entry = envEntry(process, name);
  if (entry?.valueFrom !== undefined) return true;
  if (typeof entry?.value === "string" && (trim ? trimRuntimeSpace(entry.value) : entry.value) !== "") return true;
  if (!portEnv) return false;
  const port = envEntry(process, portEnv);
  return port?.valueFrom !== undefined || typeof port?.value === "string" && port.value !== "";
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
function podListenerError(processes: InstalledProcess[], deferredLegacyListeners: Set<InstalledProcess>): string | undefined {
  const bindings: ListenerBinding[] = [];
  for (const process of processes) {
    const { contract } = process;
    const addresses: { address: string | undefined; explicit: boolean; required: boolean; deferUnproven: boolean }[] = [];
    if (contract.rpcAddressEnv || rpcEnabled(process)) addresses.push({
      address: listenerAddress(process, contract.addressEnv, contract.defaultAddress, contract.kind === "ownerservice", contract.addressPortEnv),
      explicit: listenerConfigured(process, contract.addressEnv, contract.kind === "ownerservice", contract.addressPortEnv), required: false,
      deferUnproven: deferredLegacyListeners.has(process)
    });
    if (contract.rpcAddressEnv && rpcEnabled(process)) addresses.push({
      address: rpcListenerAddress(process),
      explicit: listenerConfigured(process, contract.rpcAddressEnv, true), required: false, deferUnproven: false
    });
    if (contract.rpcAddressEnv && !rpcEnabled(process) && listenerConfigured(process, contract.rpcAddressEnv, true) &&
        !listenerBinding(listenerAddress(process, contract.rpcAddressEnv, undefined, true))) return "installation_listener_unproven";
    const additionalListeners = (contract.additionalListeners ?? []).map(listener => ({
        address: listenerAddress(process, listener.addressEnv, listener.defaultAddress, listener.valueSemantics === "trimmed"),
        explicit: listenerConfigured(process, listener.addressEnv, listener.valueSemantics === "trimmed"), required: true, deferUnproven: false
      }));
    for (const address of addresses.concat(additionalListeners)) {
      const binding = listenerBinding(address.address);
      // Explicit Product listeners are part of the static admission contract;
      // unresolved or malformed values cannot be treated as absent.
      if (!binding) {
        if (address.required || address.explicit && !address.deferUnproven || processes.length > 1) return "installation_listener_unproven";
        continue;
      }
      if (bindings.some(other => other.port === binding.port && (other.wildcard || binding.wildcard || other.host === binding.host)))
        return "installation_listener_conflict";
      bindings.push(binding);
    }
  }
}

// ---------------------------------------------------------------------------
// The composed deployment unit.
//
// The contract's deploymentUnit declares one Kubernetes Deployment whose
// container runs every declared owner process in one process space. The
// admission proves that shape against the contract itself:
//
//   * identity: the Deployment is the contract's declared unit, it runs the
//     product image and it runs the contract's declared entrypoint;
//   * membership: the unit hosts exactly the contract's declared members and
//     exposes exactly their declared listeners;
//   * live ownership: the workload behind every Service the unit publishes is
//     the unit's own Deployment and the unit's own Pod, so a foreign workload
//     or a replaced Pod cannot serve the unit's traffic;
//   * routes: every edge the contract declares resolves, inside this unit, to
//     the target's declared listener and to the material both ends need;
//   * isolation: the shared container never hands one member's identity-scoped
//     material to another member.
//
// Effective listener values follow the process fields and their value semantics;
// defaults describe only an unset installation environment.
// ---------------------------------------------------------------------------

type UnitListener = ListenerBinding & { addressEnv: string; transport: "grpc" | "http" };
type UnitMember = { contract: ProcessContract; principal: string; listeners: UnitListener[]; rpcActive: boolean };
type UnitAddress = { kind: "service"; name: string; namespace: string; port: number } | ({ kind: "bind" } & ListenerBinding);

function unitMemberListeners(process: InstalledProcess): UnitListener[] | undefined {
  const { contract } = process;
  const listeners: UnitListener[] = [];
  const add = (addressEnv: string, address: string | undefined, transport: "grpc" | "http") => {
    const binding = listenerBinding(address);
    if (!binding) return false;
    listeners.push({ addressEnv, transport, ...binding });
    return true;
  };
  if (contract.kind !== "ownerservice" && !add(contract.addressEnv,
      listenerAddress(process, contract.addressEnv, contract.defaultAddress, false, contract.addressPortEnv), "http")) return;
  if (rpcEnabled(process) && (contract.rpcAddressEnv || contract.kind === "ownerservice") &&
      !add(contract.rpcAddressEnv ?? contract.addressEnv, rpcListenerAddress(process), "grpc")) return;
  for (const listener of contract.additionalListeners ?? []) {
    const address = listenerAddress(process, listener.addressEnv, listener.defaultAddress, listener.valueSemantics === "trimmed");
    if (address === undefined && !envEntry(process, listener.addressEnv) && listener.defaultAddress === undefined) continue;
    if (!add(listener.addressEnv, address, "http")) return;
  }
  return listeners;
}

// IPs retain their host identity. A matching port cannot prove that a remote
// address belongs to this Pod's network namespace.
function parseUnitAddress(raw: unknown, unitNamespace: string): UnitAddress | undefined {
  if (typeof raw !== "string") return;
  const binding = listenerBinding(raw);
  if (binding) return { kind: "bind", ...binding };
  const service = parseServiceAddress(raw, unitNamespace);
  return service ? { kind: "service", ...service } : undefined;
}
function loopbackHost(host: string): boolean {
  return host === "[::1]" || isIP(host) === 4 && host.startsWith("127.");
}
function unitLocalHost(binding: ListenerBinding, deployment: Resource, resources: Resource[]): boolean {
  if (binding.wildcard || loopbackHost(binding.host)) return true;
  // A specific non-loopback bind needs readback from every current Pod of the
  // unit; an arbitrary remote IP is never inferred from its port.
  const pods = resources.filter(resource => resource.kind === "Pod" && namespace(resource) === namespace(deployment) &&
    backendOwnershipFailure(resource, deployment, resources) === undefined);
  return pods.length > 0 && pods.every(pod => {
    const replica = controller(pod, "ReplicaSet", resources);
    const currentSpec = deployment.spec.template.spec;
    if (!isDeepStrictEqual(replica?.spec?.template?.spec, currentSpec) || pod.spec?.hostNetwork === true) return false;
    const current = currentSpec.containers[0];
    const live = Array.isArray(pod.spec?.containers) ? pod.spec.containers.filter((container: Resource) => container?.name === current.name) : [];
    if (live.length !== 1 || ["image", "command", "args", "env", "envFrom", "ports"].some(field =>
      !isDeepStrictEqual(live[0][field], current[field]))) return false;
    const ips = [pod.status?.podIP, ...(Array.isArray(pod.status?.podIPs) ? pod.status.podIPs.map((entry: Resource) => entry?.ip) : [])];
    return ips.some(ip => typeof ip === "string" && isIP(ip) !== 0 &&
      (isIP(ip) === 6 ? new URL(`http://[${ip}]`).hostname : ip) === binding.host);
  });
}
function unitListenerReachable(listener: UnitListener, address: UnitAddress, service?: Resource): boolean {
  const family = (host: string) => isIP(host.startsWith("[") ? host.slice(1, -1) : host);
  if (address.kind === "service") {
    if (loopbackHost(listener.host)) return false;
    const families = service?.spec?.ipFamilies;
    if (Array.isArray(families) && families.length > 0 && family(listener.host) !== 0)
      return families.every(value => value === (family(listener.host) === 6 ? "IPv6" : "IPv4"));
    // No dual-stack socket or Service-family evidence is supplied. In
    // particular [::] must not silently prove an IPv4 Service route.
    return family(listener.host) !== 6;
  }
  const destination = address.host === "" ? "127.0.0.1" : address.host;
  if (listener.wildcard) return listener.host === "" || family(listener.host) === family(destination);
  if (address.wildcard) return loopbackHost(listener.host) && family(listener.host) === family(destination);
  return listener.host === address.host;
}
// The container port a Service port reaches inside this unit.
function unitTargetPort(servicePort: Resource, unitPorts: Resource[]): number | undefined {
  let targetPort = servicePort.targetPort ?? servicePort.port;
  if (typeof targetPort === "string") {
    const named = unitPorts.filter(entry => entry?.name === targetPort && (entry.protocol ?? "TCP") === "TCP");
    if (named.length !== 1) return;
    targetPort = named[0].containerPort;
  }
  return validPort(targetPort) ? targetPort : undefined;
}
function sameSecret(left: SecretRef, right: SecretRef): boolean {
  return left.name === right.name && left.key === right.key;
}

function validateComposedInstallation(
  resources: Resource[], topology: InstallationTopology, unit: DeploymentUnitContract, security: InstallationSecurity): InstallationReport {
  const report: InstallationReport = { ok: true, errors: [], communication: { edges: [], security } };
  const fail = (code: string, reasonCode?: string, detail?: { service?: string; caller?: string; target?: string; addressEnv?: string; tokenEnv?: string }) =>
    report.errors.push({ code, ...(reasonCode ? { reasonCode } : {}), ...(detail ?? {}) });
  const invalid = (code: string) => installationFailure(code, security);

  const imageEnv = topology.image?.env;
  if (typeof imageEnv !== "string" || !imageEnv) return invalid("installation_topology_process_invalid");
  if (typeof unit.name !== "string" || !unit.name || typeof unit.entrypoint !== "string" || !unit.entrypoint ||
      unit.memberSource !== "processes" || unit.kind !== "oneContainerProcessSpace") return invalid("installation_topology_process_invalid");

  const deployments = resources.filter(resource => resource.kind === "Deployment");
  const unitDeployment = deployments.find(deployment => deployment.metadata.name === unit.name);
  if (!unitDeployment) return invalid("installation_process_missing");
  const podSpec = unitDeployment.spec?.template?.spec;
  const containers: Resource[] = podSpec?.containers ?? [];
  // The contract declares one container in one process space.
  if (containers.length !== 1) return invalid("installation_process_unproven");
  const unitContainer = containers[0];
  if (podSpec.hostNetwork === true) return invalid("installation_pod_network_unsupported");

  const names = new Set<string>([imageEnv]);
  for (const process of topology.processes) {
    if ((process.databaseEnv && (typeof process.compositionDatabaseEnv !== "string" ||
        !/^[A-Za-z_][A-Za-z0-9_]*$/.test(process.compositionDatabaseEnv))) ||
        (!process.databaseEnv && process.compositionDatabaseEnv != null)) return invalid("installation_topology_process_invalid");
    for (const name of [process.addressEnv, process.rpcAddressEnv, process.peerTokensEnv, process.rpcEnabledByEnv,
      process.addressPortEnv, process.compositionDatabaseEnv, process.rpcDatabaseEnv]) if (name) names.add(name);
    for (const listener of process.additionalListeners ?? []) if (listener.addressEnv) names.add(listener.addressEnv);
  }
  for (const edge of topology.communication.edges) {
    names.add(edge.addressEnv); names.add(edge.tokenEnv);
    for (const name of edge.activationEnvs ?? []) names.add(name);
  }
  const resolved = effectiveEnv(unitDeployment, unitContainer, names, resources);
  if (resolved.error) return invalid(resolved.error);
  const env = resolved.env;

  // The installation states which image this unit runs; the contract owns the
  // environment that carries it.
  const imageEntry = env.get(imageEnv);
  const productImage = typeof imageEntry?.value === "string" ? imageEntry.value : undefined;
  if (!productImage || imageEntry!.valueFrom !== undefined) return invalid("installation_process_unproven");
  if (unitContainer.image !== productImage) return invalid("installation_process_unproven");

  // The unit's command must be exactly the contract's declared entrypoint. A
  // command that is not a declared owner process and not the declared unit is
  // an unproven product process.
  const argv = [...(unitContainer.command ?? []), ...(unitContainer.args ?? [])];
  if (argv.length !== 1 || argv[0] !== unit.entrypoint) return invalid("installation_process_unproven");

  // A controller-owned replica is the same Deployment unit, including an old
  // revision during rollout. Container names alone establish no such identity.
  const isUnitComposition = (container: Resource) => {
    const command = productArgv(container, container.image === productImage ? topology.image.defaultCommand : undefined);
    return command?.length === 1 && command[0] === unit.entrypoint;
  };
  // The Instance can retain the drained split Deployment templates for recovery.
  // Omitted replica counters in a current Kubernetes status represent zero.
  const observedStopped = (resource: Resource) => resource.spec?.replicas === 0 &&
    typeof resource.metadata?.uid === "string" && resource.metadata.uid.length > 0 &&
    Number.isSafeInteger(resource.metadata.generation) && resource.metadata.generation > 0 &&
    resource.status?.observedGeneration === resource.metadata.generation &&
    ["replicas", "updatedReplicas", "readyReplicas", "availableReplicas", "terminatingReplicas"].every(field =>
      resource.status[field] === undefined || resource.status[field] === 0);
  const replicasByDeployment = new Map<Resource, Resource[]>();
  const podsByController = new Map<Resource, Resource[]>();
  for (const resource of resources) {
    if (resource.apiVersion === "apps/v1" && resource.kind === "ReplicaSet") {
      const parent = controller(resource, "Deployment", resources);
      if (parent) replicasByDeployment.set(parent, [...(replicasByDeployment.get(parent) ?? []), resource]);
    } else if (resource.apiVersion === "v1" && resource.kind === "Pod") {
      const parent = controller(resource, "ReplicaSet", resources) ?? controller(resource, "Deployment", resources);
      if (parent) podsByController.set(parent, [...(podsByController.get(parent) ?? []), resource]);
    }
  }
  const stoppedTemplates = new Set<Resource>();
  const drained = (resource: Resource) => observedStopped(resource) &&
    (podsByController.get(resource) ?? []).every(pod => ["Succeeded", "Failed"].includes(pod.status?.phase));
  for (const deployment of deployments) {
    const replicas = replicasByDeployment.get(deployment) ?? [];
    if (deployment !== unitDeployment && deployment.apiVersion === "apps/v1" && namespace(deployment) === namespace(unitDeployment) &&
        drained(deployment) && replicas.every(drained)) {
      stoppedTemplates.add(deployment);
      for (const replica of replicas) stoppedTemplates.add(replica);
    }
  }
  for (const replica of replicasByDeployment.get(unitDeployment) ?? []) {
    if (drained(replica)) stoppedTemplates.add(replica);
  }
  for (const resource of resources) {
    const spec = resource.kind === "Pod" ? resource.spec : resource.kind === "CronJob"
      ? resource.spec?.jobTemplate?.spec?.template?.spec : resource.spec?.template?.spec;
    if (!spec) continue;
    if (spec.containers !== undefined && !validContainerList(spec.containers, true)) return invalid("installation_manifest_shape_invalid");
    if (spec.initContainers !== undefined && !validContainerList(spec.initContainers)) return invalid("installation_manifest_shape_invalid");
    if (stoppedTemplates.has(resource)) continue;
    let primary: Resource | undefined = resource === unitDeployment ? unitContainer : undefined;
    const replica = resource.apiVersion === "apps/v1" && resource.kind === "ReplicaSet" ? resource :
      resource.apiVersion === "v1" && resource.kind === "Pod"
      ? controller(resource, "ReplicaSet", resources) : undefined;
    if (replica && controller(replica, "Deployment", resources) === unitDeployment &&
        validContainerList(replica.spec?.template?.spec?.containers, true)) {
      const declared = replica.spec.template.spec.containers.filter(isUnitComposition);
      if (declared.length === 1) {
        const matching = (spec.containers ?? []).filter((container: Resource) => container.name === declared[0].name &&
          container.image === declared[0].image && isUnitComposition(container));
        if (matching.length === 1) primary = matching[0];
      }
    }
    // Init containers never represent the unit's primary, even with the same
    // image, executable or name; restartPolicy=Always is no exception.
    const extra = [...(spec.initContainers ?? []), ...(spec.containers ?? []).filter((container: Resource) => container !== primary)];
    for (const container of extra) {
      const ownerArgv = productArgv(container, container.image === productImage ? topology.image.defaultCommand : undefined);
      if (ownerArgv && topology.processes.some(process => ownerArgv[0] === process.binary)) return invalid("installation_deployment_unit_mixed");
      if (container.image === productImage || ownerArgv?.[0] === unit.entrypoint) return invalid("installation_process_unproven");
    }
  }

  const members: UnitMember[] = [];
  for (const contract of topology.processes) {
    const principal = contract.transportPrincipal;
    if (!principal) return invalid("installation_topology_process_invalid");
    const process = { contract, deployment: unitDeployment, container: unitContainer, env };
    const listeners = unitMemberListeners(process);
    if (!listeners || listeners.some(listener => !unitLocalHost(listener, unitDeployment, resources)))
      return invalid("installation_listener_unproven");
    members.push({ contract, principal, listeners, rpcActive: rpcEnabled(process) });
  }
  const unitPorts: Resource[] = (unitContainer.ports ?? []).filter((entry: Resource) => (entry.protocol ?? "TCP") === "TCP");
  const declaredPorts = new Map<number, string>();
  for (const entry of unitPorts) {
    if (typeof entry.name !== "string" || !entry.name) { fail("installation_unit_membership", "port_unnamed"); continue; }
    if (declaredPorts.has(entry.containerPort)) { fail("installation_unit_membership", "port_duplicate"); continue; }
    declaredPorts.set(entry.containerPort, entry.name);
  }
  const expectedPorts = new Map<number, { principal: string; addressEnv: string }>();
  for (const member of members) {
    if (member.listeners.length === 0 && member.rpcActive) { fail("installation_unit_membership", "member_missing", { target: member.principal }); continue; }
    for (const listener of member.listeners) {
      if (expectedPorts.has(listener.port)) { fail("installation_listener_conflict", undefined, { target: member.principal }); continue; }
      expectedPorts.set(listener.port, { principal: member.principal, addressEnv: listener.addressEnv });
    }
  }
  // Every declared member must be hosted, and no undeclared member may be.
  for (const [port, owner] of expectedPorts) if (!declaredPorts.has(port)) fail("installation_unit_membership", "member_missing", { target: owner.principal });
  for (const port of declaredPorts.keys()) if (!expectedPorts.has(port)) fail("installation_unit_membership", "member_undeclared");

  // The Services that publish this unit's listeners.
  const unitLabels = unitDeployment.spec.template.metadata?.labels ?? {};
  const unitNamespace = namespace(unitDeployment);
  const routes = new Map<number, { service: Resource; servicePort: Resource }[]>();
  for (const service of resources.filter(resource => resource.kind === "Service" && namespace(resource) === unitNamespace)) {
    const selector = service.spec?.selector;
    if (service.apiVersion !== "v1" || !object(selector) || Object.keys(selector).length === 0 ||
        service.spec.type === "ExternalName" || !labelsMatch(selector, unitLabels)) continue;
    for (const servicePort of service.spec.ports ?? []) {
      if (!object(servicePort) || (servicePort.protocol ?? "TCP") !== "TCP") continue;
      const containerPort = unitTargetPort(servicePort, unitPorts);
      if (containerPort === undefined) continue;
      const published = routes.get(containerPort) ?? [];
      published.push({ service, servicePort });
      routes.set(containerPort, published);
    }
  }
  // A member's declared listener is the one the unit's container ports and
  // Services route to.
  for (const [port, owner] of expectedPorts) {
    const publishers = routes.get(port) ?? [];
    if (publishers.length === 0) { fail("installation_unit_membership", "listener_unpublished", { target: owner.principal }); continue; }
  }
  if (report.errors.length > 0) { report.ok = false; report.communication.edges = []; return report; }

  // The live workload behind every Service this unit publishes.
  //
  // The Instance's production admission reads the cluster's live Pod,
  // ReplicaSet, Endpoint and EndpointSlice objects and hands them to this
  // checker inside the manifest it admits. The per-owner path proves what
  // backs each of its Services; the composed form must carry the same proof
  // for the Services that publish the unit's listeners, or a unit whose
  // Service is actually fronted by a foreign workload, or whose published
  // endpoint names a replaced Pod, would be admitted.
  //
  // The proof reuses the per-owner helpers unchanged: the same backend
  // ownership rule, the same endpoint rule and the same reason vocabulary.
  // The unit is the one Deployment that owns the published workload, so the
  // ownership chain Pod -> ReplicaSet -> Deployment must reach it.
  const unitProcessContract = (container: Resource) => {
    const argv = productArgv(container, container.image === productImage ? topology.image.defaultCommand : undefined);
    return argv && topology.processes.find(process => argv[0] === process.binary &&
      Array.isArray(process.daemonArgs) && argv.length === process.daemonArgs.length + 1 &&
      process.daemonArgs.every((arg, index) => argv[index + 1] === arg));
  };
  const publishedServices = new Map<Resource, { servicePort: Resource; containerPort: number }[]>();
  for (const [containerPort, publishers] of routes) {
    for (const publisher of publishers) {
      const ports = publishedServices.get(publisher.service) ?? [];
      ports.push({ servicePort: publisher.servicePort, containerPort });
      publishedServices.set(publisher.service, ports);
    }
  }
  for (const [service, ports] of publishedServices) {
    const detail = { service: service.metadata.name };
    const selector = service.spec.selector;
    // The workloads that back this Service must all be the unit's own.
    const backends: Resource[] = [];
    let backendUnproven = false;
    for (const resource of resources) {
      if (namespace(resource) !== namespace(service) || stoppedTemplates.has(resource)) continue;
      const labels = backendLabels(resource);
      if (labels === undefined) backendUnproven = true;
      else if (labels !== null && labelsMatch(selector, labels)) backends.push(resource);
    }
    if (backendUnproven) { fail("rpc_service_backend_unproven", undefined, detail); continue; }
    const backendFailure = backends.map(backend => backendOwnershipFailure(backend, unitDeployment, resources)).find(reason => reason !== undefined);
    if (backendFailure) { fail("rpc_service_backend_ambiguous", backendFailure, detail); continue; }
    // The published endpoints must name the unit's own Pod. An EndpointSlice or
    // Endpoints object that lists no address delivers no traffic, so it proves
    // nothing and carries no obligation; one that lists an address must name a
    // Pod of this unit.
    const endpoints = resources.filter(resource => namespace(resource) === namespace(service) &&
      (resource.kind === "Endpoints" && resource.metadata.name === service.metadata.name ||
       resource.kind === "EndpointSlice" && resource.metadata.labels?.["kubernetes.io/service-name"] === service.metadata.name));
    for (const { servicePort, containerPort } of ports) {
      const endpointFailure = endpoints.map(resource => endpointBackendFailure(resource, service, unitDeployment, unitContainer, servicePort, containerPort, resources, topology.image.defaultCommand, unitProcessContract))
        .find(reason => reason !== undefined);
      if (endpointFailure) { fail("rpc_service_backend_ambiguous", endpointFailure, detail); break; }
    }
  }
  if (report.errors.length > 0) { report.ok = false; report.communication.edges = []; return report; }

  for (const edge of topology.communication.edges) {
    const caller = members.find(member => member.principal === edge.caller);
    const target = members.find(member => member.principal === edge.target);
    if (!caller || !caller.rpcActive) continue;
    const address = env.get(edge.addressEnv);
    const addressSemantics = edge.activation === "configured" ? edge.configuredAddressSemantics! : "trimmed-nonempty";
    if (edge.activation === "configured" && !configured(address, addressSemantics) &&
        !configured(env.get(edge.tokenEnv), "trimmed-nonempty") &&
        !edge.activationEnvs?.some(name => configured(env.get(name), "trimmed-nonempty"))) continue;
    const detail = { caller: edge.caller, target: edge.target, addressEnv: edge.addressEnv, tokenEnv: edge.tokenEnv };
    if (!address || !configured(address, addressSemantics)) { fail("rpc_address_required", undefined, detail); continue; }
    if (!target || !target.rpcActive) { fail("rpc_target_missing", undefined, detail); continue; }
    const addressValue = edge.activation === "configured" && addressSemantics === "trimmed-nonempty" && typeof address.value === "string"
      ? trimRuntimeSpace(address.value) : address.value;
    const parsed = parseUnitAddress(addressValue, unitNamespace);
    if (!parsed) { fail("rpc_address_invalid", undefined, detail); continue; }
    let containerPort: number | undefined;
    let addressService: Resource | undefined;
    if (parsed.kind === "service") {
      addressService = resources.find(resource => resource.kind === "Service" && resource.apiVersion === "v1" &&
        resource.metadata.name === parsed.name && namespace(resource) === parsed.namespace);
      if (!addressService) { fail("rpc_service_missing", undefined, detail); continue; }
      const selector = addressService.spec?.selector;
      if (!object(selector) || Object.keys(selector).length === 0 || addressService.spec.type === "ExternalName" ||
          !labelsMatch(selector, unitLabels)) { fail("rpc_service_target_mismatch", undefined, detail); continue; }
      const matching = (addressService.spec.ports ?? []).filter((entry: Resource) => entry?.port === parsed.port && (entry.protocol ?? "TCP") === "TCP");
      if (matching.length !== 1) { fail("rpc_service_port_invalid", undefined, detail); continue; }
      containerPort = unitTargetPort(matching[0], unitPorts);
      if (containerPort === undefined) { fail("rpc_target_port_invalid", undefined, detail); continue; }
    } else {
      if (!unitLocalHost(parsed, unitDeployment, resources)) { fail("rpc_address_invalid", undefined, detail); continue; }
      containerPort = parsed.port;
    }
    const listener = target.listeners.find(listener => listener.transport === "grpc" && listener.port === containerPort);
    if (!listener || !unitListenerReachable(listener, parsed, addressService)) { fail("rpc_target_listener_invalid", undefined, detail); continue; }
    // A named edge selects its own unique Service port. Aliases that publish
    // the same listener do not make that edge ambiguous. A direct local dial
    // proves its bind address rather than inventing a selected Service.
    const publishers = (routes.get(containerPort) ?? []).filter(publisher => !addressService || publisher.service === addressService);
    if (publishers.length === 0) { fail("rpc_service_missing", undefined, detail); continue; }
    const tokenRef = secretReference(env.get(edge.tokenEnv));
    if (!tokenRef) { fail("rpc_token_ref_invalid", undefined, detail); continue; }
    if (!target.contract.peerTokensEnv) { fail("rpc_peer_ref_invalid", undefined, detail); continue; }
    const peerRef = secretReference(env.get(target.contract.peerTokensEnv));
    if (!peerRef) { fail("rpc_peer_ref_invalid", undefined, detail); continue; }
    const endpoint = { deployment: unitDeployment, container: unitContainer };
    const policies = communicationPolicyProof(endpoint, endpoint, containerPort, resources);
    if (policies.error) { fail(policies.error, undefined, detail); continue; }
    report.communication.edges.push({
      caller: edge.caller, target: edge.target, addressEnv: edge.addressEnv, tokenEnv: edge.tokenEnv,
      callerDeployment: unitDeployment.metadata.name, targetDeployment: unitDeployment.metadata.name, port: containerPort,
      tokenRef, peerRef, ...(addressService ? { serviceRef: { name: addressService.metadata.name, namespace: namespace(addressService) } } :
        parsed.kind === "bind" ? { bindAddress: { host: parsed.host, port: parsed.port } } : {}),
      ingressPolicies: policies.ingressPolicies, egressPolicies: policies.egressPolicies
    });
  }

  // Admission proves the required references and detectable reference reuse.
  // Distinct references say nothing about token values; Instance material
  // validation owns caller/token conflicts after resolving Secret contents.
  const peerTokens = new Map<string, SecretRef>();
  for (const member of members) {
    if (!member.contract.peerTokensEnv) continue;
    const ref = secretReference(env.get(member.contract.peerTokensEnv));
    if (!ref) { fail("installation_unit_identity_unproven", "peer_tokens_missing", { target: member.principal }); continue; }
    for (const [principal, other] of peerTokens) {
      if (sameSecret(other, ref)) fail("installation_unit_identity_unproven", "peer_token_shared", { target: member.principal, caller: principal });
    }
    peerTokens.set(member.principal, ref);
  }
  for (const edge of topology.communication.edges) {
    const ref = secretReference(env.get(edge.tokenEnv));
    if (!ref) continue;
    for (const [principal, peerRef] of peerTokens) {
      if (sameSecret(peerRef, ref)) fail("installation_unit_identity_unproven", "credential_reused", { target: edge.target, caller: principal });
    }
  }
  // The topology owns the parent -> child database mapping. Only declared
  // projections are checked; the shared parent's legacy DATABASE_URL cannot
  // stand in for an owner's actual alias. References prove reuse, not values
  // or database roles; Instance material/readback owns those facts.
  const databases = new Map<string, { ref: SecretRef; principal: string }>();
  for (const member of members) {
    for (const name of [member.contract.compositionDatabaseEnv, member.rpcActive ? member.contract.rpcDatabaseEnv : undefined]) {
      if (!name || databases.has(name)) continue;
      const ref = secretReference(env.get(name));
      if (!ref) { fail("installation_unit_identity_unproven", "database_missing", { target: member.principal }); continue; }
      for (const other of databases.values()) if (sameSecret(ref, other.ref))
        fail("installation_unit_identity_unproven", "database_shared", { target: member.principal, caller: other.principal });
      databases.set(name, { ref, principal: member.principal });
    }
  }

  report.ok = report.errors.length === 0;
  if (!report.ok) report.communication.edges = [];
  return report;
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
  // The contract's deployment unit and the per-owner process form are two
  // shapes of the same installation. The shape the installation declares is
  // the shape the admission proves; the two are never mixed.
  const declaredUnit = topology.deploymentUnit?.unit;
  if (declaredUnit && typeof declaredUnit.name === "string" &&
      deployments.some(deployment => deployment.metadata.name === declaredUnit.name))
    return validateComposedInstallation(resources, topology, declaredUnit, security);
  const productImages = new Set<string>(deployments.flatMap(deployment =>
    deployment.spec.template.spec.containers.filter((container: Resource) =>
      topology.processes.some(process => container.command?.[0] === process.binary))
      .map((container: Resource) => container.image)));
  const processContract = (container: Resource) => {
    const argv = productArgv(container, productImages.has(container.image) ? topology.image.defaultCommand : undefined);
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
        for (const name of edge.activationEnvs ?? []) names.add(name);
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
  const deferredLegacyListeners = new Set<InstalledProcess>();
  for (const edge of topology.communication.edges) {
    const caller = installed.get(edge.caller);
    const target = installed.get(edge.target);
    if (!caller || !target || !target.contract.rpcAddressEnv) continue;
    const address = envEntry(caller, edge.addressEnv);
    const addressSemantics = edge.activation === "configured" ? edge.configuredAddressSemantics! : "trimmed-nonempty";
    if (edge.activation === "configured" && !configured(address, addressSemantics) &&
        !configured(envEntry(caller, edge.tokenEnv), "trimmed-nonempty") &&
        !edge.activationEnvs?.some(name => configured(envEntry(caller, name), "trimmed-nonempty"))) continue;
    deferredLegacyListeners.add(target);
  }
  for (const processes of podProcesses.values()) {
    const error = podListenerError(processes, deferredLegacyListeners);
    if (error) return failure(error);
  }
  for (const edge of topology.communication.edges) {
    const caller = installed.get(edge.caller);
    if (!caller) continue;
    const address = envEntry(caller, edge.addressEnv);
    const addressSemantics = edge.activation === "configured" ? edge.configuredAddressSemantics! : "trimmed-nonempty";
    // Native address activation varies per caller; tokens and the extra
    // activation variables use Go TrimSpace. Keep address-or-token admission.
    if (edge.activation === "configured" && !configured(address, addressSemantics) && !configured(envEntry(caller, edge.tokenEnv), "trimmed-nonempty") &&
        !edge.activationEnvs?.some(name => configured(envEntry(caller, name), "trimmed-nonempty"))) continue;
    const fail = (code: string, reasonCode?: string) => report.errors.push({ code, ...(reasonCode ? { reasonCode } : {}), service: caller.contract.service,
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
    const backendFailure = backends.map(backend => backendOwnershipFailure(backend, target.deployment, resources)).find(reason => reason !== undefined);
    if (backendFailure) { fail("rpc_service_backend_ambiguous", backendFailure); continue; }
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
    const otherBinding = otherListener === null || otherListener === undefined ? undefined : listenerBinding(otherListener);
    let listenerUnproven = otherListener === undefined || otherBinding === undefined && otherListener !== null;
    let conflict = otherBinding !== undefined && otherBinding.port === targetPort;
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
      if (!resolved) { listenerUnproven = true; break; }
      const addresses: (string | undefined)[] = [];
      const enabled = rpcEnabled(resolved);
      if (contract.rpcAddressEnv || enabled) addresses.push(listenerAddress(resolved, contract.addressEnv, contract.defaultAddress, contract.kind === "ownerservice", contract.addressPortEnv));
      if (contract.rpcAddressEnv && enabled) addresses.push(rpcListenerAddress(resolved));
      for (const listener of contract.additionalListeners ?? []) {
        if (envEntry(resolved, listener.addressEnv) || listener.defaultAddress !== undefined) addresses.push(listenerAddress(resolved, listener.addressEnv, listener.defaultAddress, listener.valueSemantics === "trimmed"));
      }
      const bindings = addresses.map(address => address === undefined ? undefined : listenerBinding(address));
      if (bindings.some(binding => binding === undefined)) {
        listenerUnproven = true; break;
      }
      if (bindings.some(binding => binding!.port === targetPort)) {
        conflict = true; break;
      }
    }
    const endpoints = resources.filter(resource => namespace(resource) === namespace(service) &&
      (resource.kind === "Endpoints" && resource.metadata.name === service.metadata.name ||
       resource.kind === "EndpointSlice" && resource.metadata.labels?.["kubernetes.io/service-name"] === service.metadata.name));
    if (listenerUnproven) {
      fail("rpc_service_backend_ambiguous", "listener_unproven"); continue;
    }
    if (conflict) {
      fail("rpc_service_backend_ambiguous", "listener_conflict"); continue;
    }
    const endpointFailure = endpoints.map(resource => endpointBackendFailure(resource, service, target.deployment, target.container, servicePort, targetPort, resources, topology.image.defaultCommand, processContract))
      .find(reason => reason !== undefined);
    if (endpointFailure) {
      fail("rpc_service_backend_ambiguous", endpointFailure); continue;
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
