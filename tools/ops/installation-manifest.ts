import { communicationPolicyProof } from "./installation-network-policy.ts";

// Product installation admission. Business communication requirements are read
// only from this checkout's portable topology, never inferred from env names.
export type SecretRef = { name: string; key: string };
export type CommunicationEdge = {
  caller: string; target: string; addressEnv: string; tokenEnv: string;
  callerDeployment: string; targetDeployment: string; port: number;
  tokenRef: SecretRef; peerRef: SecretRef; ingressPolicies: string[]; egressPolicies: string[];
};
export type InstallationError = { code: string; service?: string; caller?: string; target?: string; addressEnv?: string; tokenEnv?: string };
export type InstallationSecurity = { tokenMinimumLength: number };
export type InstallationReport = { ok: boolean; errors: InstallationError[]; communication: { edges: CommunicationEdge[]; security: InstallationSecurity | null } };
export type ProcessContract = {
  service: string; binary: string; transportPrincipal?: string;
  addressEnv: string; defaultAddress: string; rpcAddressEnv?: string; defaultRPCAddress?: string; peerTokensEnv?: string; rpcEnabledByEnv?: string;
};
export type EdgeContract = { caller: string; target: string; addressEnv: string; tokenEnv: string; activation: "required" | "configured"; activationEnv?: string };
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
      resource.spec.template.spec.containers.every((container: unknown) => object(container) &&
        typeof container.name === "string" && typeof container.image === "string" &&
        (container.command === undefined || Array.isArray(container.command) && container.command.every((arg: unknown) => typeof arg === "string")) &&
        (container.args === undefined || Array.isArray(container.args) && container.args.every((arg: unknown) => typeof arg === "string")) &&
        (container.envFrom === undefined || Array.isArray(container.envFrom)) &&
        (container.env === undefined || Array.isArray(container.env) && container.env.every((entry: unknown) => object(entry) &&
          typeof entry.name === "string" && (entry.value === undefined || typeof entry.value === "string") &&
          (entry.valueFrom === undefined || object(entry.valueFrom)) && !(entry.value !== undefined && entry.valueFrom !== undefined))) &&
        (container.ports === undefined || Array.isArray(container.ports) && container.ports.every((entry: unknown) => object(entry) && validPort(entry.containerPort))));
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
function configured(entry: Resource | undefined) {
  return entry !== undefined && (entry.valueFrom !== undefined || (typeof entry.value === "string" && entry.value.trim() !== ""));
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
function podTemplate(resource: Resource): Resource | undefined {
  if (resource.kind === "Pod") return resource;
  if (resource.kind === "CronJob") return resource.spec?.jobTemplate?.spec?.template;
  if (["Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job"].includes(resource.kind)) return resource.spec?.template;
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
function endpointBackend(resource: Resource, service: Resource, target: Resource, servicePort: Resource, port: number, resources: Resource[]): boolean {
  const provesPort = (ports: unknown): boolean => {
    if (!Array.isArray(ports)) return false;
    const matching = ports.filter(entry => object(entry) && (entry.name ?? "") === (servicePort.name ?? "") && (entry.protocol ?? "TCP") === "TCP");
    return matching.length === 1 && matching[0].port === port;
  };
  const provesPod = (ref: unknown, addresses: unknown): boolean => {
    if (!object(ref) || ref.kind !== "Pod" || ref.apiVersion !== undefined && ref.apiVersion !== "v1" ||
        typeof ref.uid !== "string" || !ref.uid || (ref.namespace ?? namespace(service)) !== namespace(service) ||
        !Array.isArray(addresses) || addresses.length === 0) return false;
    const pod = resources.find(candidate => candidate.kind === "Pod" && candidate.apiVersion === "v1" &&
      namespace(candidate) === namespace(service) && candidate.metadata.name === ref.name && candidate.metadata.uid === ref.uid);
    if (!pod || !ownedBackend(pod, target, resources) || !labelsMatch(service.spec.selector, pod.metadata.labels ?? {})) return false;
    const ips = new Set([pod.status?.podIP, ...(Array.isArray(pod.status?.podIPs) ? pod.status.podIPs.map((entry: Resource) => entry.ip) : [])]);
    return addresses.every(address => typeof address === "string" && ips.has(address));
  };
  if (resource.kind === "EndpointSlice") {
    return resource.apiVersion === "discovery.k8s.io/v1" && controller(resource, "Service", resources) === service &&
      Array.isArray(resource.endpoints) && resource.endpoints.every((endpoint: unknown) =>
        object(endpoint) && provesPort(resource.ports) && provesPod(endpoint.targetRef, endpoint.addresses));
  }
  // The legacy Endpoints controller does not attach a Service ownerReference.
  // Its exact Service name plus every Pod UID and address must still agree.
  return resource.apiVersion === "v1" && (resource.subsets === undefined || Array.isArray(resource.subsets) &&
    resource.subsets.every((subset: unknown) => object(subset) && [subset.addresses, subset.notReadyAddresses].every(entries =>
      entries === undefined || Array.isArray(entries) && entries.every(entry => object(entry) && provesPort(subset.ports) && provesPod(entry.targetRef, [entry.ip])))));
}
function listenerAddress(process: InstalledProcess, name: string, defaultAddress: string | undefined): string | undefined {
  const entry = envEntry(process, name);
  return entry?.valueFrom ? undefined : typeof entry?.value === "string" && entry.value.trim() !== "" ? entry.value.trim() : defaultAddress;
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

export function validateInstallationManifest(manifest: unknown, topology: InstallationTopology): InstallationReport {
  const security = installationSecurity(topology);
  if (!security) return installationFailure("installation_topology_security_invalid");
  const failure = (code: string) => installationFailure(code, security);
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
  const processContract = (container: Resource) => topology.processes.find(process =>
    !container.args?.length && (container.command?.length === 1 && container.command[0] === process.binary ||
      !container.command?.length && productImages.has(container.image) && topology.image.defaultCommand.length === 1 &&
      process.binary === topology.image.defaultCommand[0]));
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
      if (!contract.transportPrincipal) continue;
      if (deployment.spec.template.spec.hostNetwork === true) return failure("installation_pod_network_unsupported");
      const names = new Set([contract.addressEnv, contract.rpcAddressEnv, contract.peerTokensEnv, contract.rpcEnabledByEnv].filter((name): name is string => Boolean(name)));
      for (const edge of topology.communication.edges.filter(edge => edge.caller === contract.transportPrincipal)) {
        names.add(edge.addressEnv); names.add(edge.tokenEnv);
        if (edge.activationEnv) names.add(edge.activationEnv);
      }
      const resolved = effectiveEnv(deployment, container, names, resources);
      if (resolved.error) return failure(resolved.error);
      const process = { contract, deployment, container, env: resolved.env };
      processes.set(container, process);
      if (contract.rpcEnabledByEnv && !configured(envEntry(process, contract.rpcEnabledByEnv))) continue;
      if (installed.has(contract.transportPrincipal!)) return failure("installation_process_ambiguous");
      installed.set(contract.transportPrincipal!, process);
    }
  }
  if (recognized.size === 0) return failure("installation_process_missing");
  if (productNamespaces.size !== 1) return failure("installation_namespace_unsupported");
  for (const deployment of deployments) {
    for (const container of deployment.spec.template.spec.containers) {
      if (productImages.has(container.image) && !recognized.has(container)) {
        const defaultProcess = topology.processes.find(process => process.binary === topology.image.defaultCommand[0]);
        if (container.command === undefined && container.args === undefined && defaultProcess && !defaultProcess.transportPrincipal) {
          productNamespaces.add(namespace(deployment));
          continue;
        }
        return failure("installation_process_unproven");
      }
    }
  }
  if (productNamespaces.size !== 1) return failure("installation_namespace_unsupported");
  for (const edge of topology.communication.edges) {
    const caller = installed.get(edge.caller);
    if (!caller) continue;
    const address = envEntry(caller, edge.addressEnv);
    if (edge.activation === "configured" && !configured(address) && !configured(envEntry(caller, edge.tokenEnv)) &&
        !(edge.activationEnv && configured(envEntry(caller, edge.activationEnv)))) continue;
    const fail = (code: string) => report.errors.push({ code, service: caller.contract.service,
      caller: edge.caller, target: edge.target, addressEnv: edge.addressEnv, tokenEnv: edge.tokenEnv });
    if (!address || !configured(address)) { fail("rpc_address_required"); continue; }
    const parsed = parseServiceAddress(address.value, namespace(caller.deployment));
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
    const backends = resources.filter(resource => {
      const template = podTemplate(resource);
      return template !== undefined && namespace(resource) === namespace(service) && labelsMatch(selector, template.metadata?.labels ?? {});
    });
    if (backends.some(backend => !ownedBackend(backend, target.deployment, resources))) {
      fail("rpc_service_backend_ambiguous"); continue;
    }
    const matching = Array.isArray(service.spec.ports) ? service.spec.ports.filter((entry: Resource) => entry?.port === parsed.port && (entry.protocol ?? "TCP") === "TCP") : [];
    if (matching.length !== 1) { fail("rpc_service_port_invalid"); continue; }
    const servicePort = matching[0];
    let targetPort = servicePort.targetPort ?? servicePort.port;
    if (typeof targetPort === "string") {
      const ports = (target.container.ports ?? []).filter((entry: Resource) => entry?.name === targetPort && (entry.protocol ?? "TCP") === "TCP");
      if (ports.length !== 1) { fail("rpc_target_port_invalid"); continue; }
      targetPort = ports[0].containerPort;
    }
    if (!validPort(targetPort)) { fail("rpc_target_port_invalid"); continue; }
    // This is ownerservice.LoadConfig's declared runtime behavior, not a repair
    // of an explicit invalid binding. Unresolved valueFrom never selects a default.
    const listener = listenerAddress(target, target.contract.rpcAddressEnv ?? target.contract.addressEnv,
      target.contract.rpcAddressEnv ? target.contract.defaultRPCAddress : target.contract.defaultAddress);
    const listenMatch = typeof listener === "string" && /^(?:|0\.0\.0\.0|\[::\]):([0-9]+)$/.exec(listener);
    if (!listenMatch || Number(listenMatch[1]) !== targetPort) { fail("rpc_target_listener_invalid"); continue; }
    const pod = target.deployment.spec.template.spec;
    const competitors = [...pod.containers, ...(pod.initContainers ?? []).filter((container: Resource) => container.restartPolicy === "Always")];
    const otherListener = target.contract.rpcAddressEnv ? listenerAddress(target, target.contract.addressEnv, target.contract.defaultAddress) : null;
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
          new Set([contract.addressEnv, contract.rpcAddressEnv, contract.rpcEnabledByEnv].filter((name): name is string => Boolean(name))), resources);
        return result.error ? undefined : { contract, deployment: target.deployment, container, env: result.env };
      })();
      if (!resolved) { conflict = true; break; }
      const addresses: (string | undefined)[] = [];
      const rpcEnabled = !contract.rpcEnabledByEnv || configured(envEntry(resolved, contract.rpcEnabledByEnv));
      if (contract.rpcAddressEnv || rpcEnabled) addresses.push(listenerAddress(resolved, contract.addressEnv, contract.defaultAddress));
      if (contract.rpcAddressEnv && rpcEnabled) addresses.push(listenerAddress(resolved, contract.rpcAddressEnv, contract.defaultRPCAddress));
      if (addresses.some(address => address === undefined || Number(/:([0-9]+)$/.exec(address)?.[1]) === targetPort)) {
        conflict = true; break;
      }
    }
    const endpoints = resources.filter(resource => namespace(resource) === namespace(service) &&
      (resource.kind === "Endpoints" && resource.metadata.name === service.metadata.name ||
       resource.kind === "EndpointSlice" && resource.metadata.labels?.["kubernetes.io/service-name"] === service.metadata.name));
    if (conflict || endpoints.some(resource => !endpointBackend(resource, service, target.deployment, servicePort, targetPort, resources))) {
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
      tokenRef, peerRef, ingressPolicies: policies.ingressPolicies, egressPolicies: policies.egressPolicies
    });
  }
  report.ok = report.errors.length === 0;
  if (!report.ok) report.communication.edges = [];
  return report;
}
