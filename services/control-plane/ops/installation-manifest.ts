import { readFile } from "node:fs/promises";

type ObjectValue = Record<string, any>;
type Requirement = { id: string; caller: string; target: string; callerPrincipal: string; targetPrincipal: string; addressEnv: string; tokenEnv: string; peerTokensEnv: string };
type Failure = { requirement: string; check: string };
const record = (v: any): v is ObjectValue => v !== null && typeof v === "object" && !Array.isArray(v);
const namespaceOf = (v: ObjectValue, namespace: string) => v.metadata?.namespace || namespace;

// Kubernetes LabelSelector semantics, shared by Service and policy resolution.
function selected(selector: any, labels: ObjectValue): boolean {
  if (!record(selector)) throw new Error("selector_invalid");
  if (selector.matchLabels !== undefined && !record(selector.matchLabels)) throw new Error("selector_invalid");
  if (!Object.entries(selector.matchLabels ?? {}).every(([key, value]) => labels[key] === value)) return false;
  if (selector.matchExpressions !== undefined && !Array.isArray(selector.matchExpressions)) throw new Error("selector_invalid");
  return (selector.matchExpressions ?? []).every((e: any) => {
    if (!record(e) || typeof e.key !== "string") throw new Error("selector_invalid");
    const exists = Object.hasOwn(labels, e.key), values = e.values;
    switch (e.operator) {
      case "In":
      case "NotIn":
        if (!Array.isArray(values) || values.length === 0 || values.some(x => typeof x !== "string")) throw new Error("selector_invalid");
        return e.operator === "In" ? exists && values.includes(labels[e.key]) : !exists || !values.includes(labels[e.key]);
      case "Exists":
      case "DoesNotExist":
        if (values !== undefined && (!Array.isArray(values) || values.length)) throw new Error("selector_invalid");
        return e.operator === "Exists" ? exists : !exists;
      default: throw new Error("selector_invalid");
    }
  });
}

function resources(input: any): ObjectValue[] {
  if (!record(input) || input.kind !== "List" || !Array.isArray(input.items) || input.items.some(x => !record(x))) throw new Error("list_invalid");
  return input.items;
}

export async function loadInstallationTopology() {
  return JSON.parse(await readFile(new URL("../../../deploy/portable/opl-cloud-owner-topology.json", import.meta.url), "utf8"));
}

// This validates installation declarations and supplied Secret material. It does
// not contact Kubernetes, establish TLS trust, or prove a live authenticated RPC.
export function validateInstallationManifest(manifest: any, bindings: any, secretManifest: any, topology: any) {
  const failures: Failure[] = [];
  let checkedRequirements = 0;
  const fail = (requirement: string, check: string) => failures.push({ requirement, check });
  const result = () => ({ ok: failures.length === 0, checkedRequirements, failures });
  try {
    const items = resources(manifest), secrets = resources(secretManifest);
    if (!record(bindings) || bindings.schemaVersion !== 1 || typeof bindings.namespace !== "string" || !bindings.namespace || !record(bindings.processes)) throw new Error("bindings_invalid");
    const namespace = bindings.namespace;
    if (!Array.isArray(topology?.processes) || !Array.isArray(topology?.communicationRequirements) || !topology.communicationRequirements.length) throw new Error("requirements_invalid");
    const processByName = new Map<string, ObjectValue>(topology.processes.map((p: ObjectValue) => [p.service, p]));
    const requirementIds = new Set<string>();
    for (const r of topology.communicationRequirements) {
      if (!record(r) || !/^[a-z][a-z0-9-]*$/.test(r.id) || requirementIds.has(r.id) ||
          !processByName.has(r.caller) || !processByName.has(r.target) ||
          ![r.callerPrincipal, r.targetPrincipal, r.addressEnv, r.tokenEnv, r.peerTokensEnv].every(x => typeof x === "string" && x.length)) throw new Error("requirements_invalid");
      requirementIds.add(r.id);
    }
    // Reject ambiguous Kubernetes identities instead of choosing an arbitrary copy.
    const inventory = new Map<string, ObjectValue>();
    for (const item of [...items, ...secrets]) {
      if (!record(item.metadata) || typeof item.metadata.name !== "string") throw new Error("resource_invalid");
      const key = `${item.kind}/${namespaceOf(item, namespace)}/${item.metadata.name}`;
      if (inventory.has(key)) throw new Error("resource_duplicate");
      inventory.set(key, item);
    }
    const get = (kind: string, name: string, ns = namespace) => inventory.get(`${kind}/${ns}/${name}`);
    const mapped = new Map<string, { deployment: ObjectValue; container: ObjectValue; namespace: string }>();
    const used = new Set<string>();
    for (const [service, binding] of Object.entries(bindings.processes)) {
      if (!processByName.has(service) || !record(binding) || typeof binding.deployment !== "string" || typeof binding.container !== "string") throw new Error("binding_invalid");
      const ns = binding.namespace ?? namespace;
      if (typeof ns !== "string" || !ns) throw new Error("binding_invalid");
      const key = `${ns}/${binding.deployment}/${binding.container}`;
      if (used.has(key)) throw new Error("binding_duplicate");
      used.add(key);
      const deployment = get("Deployment", binding.deployment, ns);
      const containers = deployment?.spec?.template?.spec?.containers;
      if (!Array.isArray(containers)) throw new Error("deployment_missing");
      const matches = containers.filter(c => c.name === binding.container);
      if (matches.length !== 1) throw new Error("container_missing");
      const container = matches[0];
      // Bind Product requirements to the process that will actually execute.
      if (!Array.isArray(container.command) || container.command[0] !== processByName.get(service)!.binary) throw new Error("process_command_mismatch");
      mapped.set(service, { deployment: deployment!, container, namespace: ns });
    }
    // A mapped subset is permitted, but cannot hide a migrated caller present in
    // the installation merely by omitting its binding.
    for (const r of topology.communicationRequirements as Requirement[]) {
      const binary = processByName.get(r.caller)!.binary;
      const callers = items.filter(i => i.kind === "Deployment").flatMap(i => (i.spec?.template?.spec?.containers ?? []).map((c: any) => ({ i, c }))).filter(({ c }) => c.command?.[0] === binary);
      const bound = mapped.get(r.caller);
      if (callers.length !== (bound ? 1 : 0)) fail(r.id, "caller_binding");
    }
    const secretText = (ref: any, ns: string): string => {
      if (!record(ref) || typeof ref.name !== "string" || typeof ref.key !== "string" || ref.optional === true) throw new Error("secret_reference");
      const secret = get("Secret", ref.name, ns);
      if (!secret) throw new Error("secret_missing");
      if (Object.hasOwn(secret.stringData ?? {}, ref.key)) {
        if (typeof secret.stringData[ref.key] !== "string") throw new Error("secret_invalid");
        return secret.stringData[ref.key];
      }
      const encoded = secret.data?.[ref.key];
      if (typeof encoded !== "string" || Buffer.from(encoded, "base64").toString("base64") !== encoded) throw new Error("secret_invalid");
      return Buffer.from(encoded, "base64").toString("utf8");
    };
    const environment = (p: { container: ObjectValue; namespace: string }): Map<string, any> => {
      const env = new Map<string, any>();
      for (const source of p.container.envFrom ?? []) {
        if (!record(source) || (!source.configMapRef && !source.secretRef) || (source.configMapRef && source.secretRef)) throw new Error("env_source_invalid");
        const ref = source.configMapRef ?? source.secretRef;
        const object = get(source.configMapRef ? "ConfigMap" : "Secret", ref.name, p.namespace);
        if (!object || ref.optional === true || (source.prefix !== undefined && typeof source.prefix !== "string")) throw new Error("env_source_missing");
        for (const key of Object.keys({ ...object.data, ...object.stringData })) {
          env.set(`${source.prefix ?? ""}${key}`, source.configMapRef ? { value: object.data[key] } : { valueFrom: { secretKeyRef: { name: ref.name, key } } });
        }
      }
      const seen = new Set<string>();
      for (const e of p.container.env ?? []) {
        if (!record(e) || typeof e.name !== "string" || seen.has(e.name)) throw new Error("env_duplicate");
        seen.add(e.name);
        env.set(e.name, e);
      }
      return env;
    };
    const value = (env: Map<string, any>, key: string, ns: string): string => {
      const e = env.get(key);
      if (!e) throw new Error("env_missing");
      if (e.valueFrom && Object.hasOwn(e, "value")) throw new Error("env_invalid");
      if (typeof e.value === "string" && e.value.trim() && !e.value.includes("$(")) return e.value;
      const ref = e.valueFrom?.configMapKeyRef;
      if (ref && !ref.optional) {
        const data = get("ConfigMap", ref.name, ns)?.data?.[ref.key];
        if (typeof data === "string" && data.trim() && !data.includes("$(")) return data;
      }
      throw new Error("env_value_invalid");
    };
    const portNumber = (v: any) => Number.isInteger(v) && v > 0 && v <= 65535;
    for (const r of topology.communicationRequirements as Requirement[]) {
      const caller = mapped.get(r.caller);
      if (!caller) continue;
      checkedRequirements++;
      const target = mapped.get(r.target);
      if (!target) { fail(r.id, "target_binding"); continue; }
      try {
        const callerEnv = environment(caller), targetEnv = environment(target);
        const address = value(callerEnv, r.addressEnv, caller.namespace).trim();
        const match = /^([a-z0-9.-]+):(\d+)$/.exec(address);
        if (!match || !portNumber(Number(match[2]))) throw new Error("address_invalid");
        const parts = match[1].split(".");
        const clusterDomain = bindings.clusterDomain ?? "cluster.local";
        if (typeof clusterDomain !== "string") throw new Error("bindings_invalid");
        const serviceName = parts[0], serviceNamespace = parts.length === 1 ? caller.namespace : parts[1];
        if (!(parts.length === 1 || parts.length === 2 || match[1] === `${serviceName}.${serviceNamespace}.svc` || match[1] === `${serviceName}.${serviceNamespace}.svc.${clusterDomain}`)) throw new Error("address_invalid");
        const service = get("Service", serviceName, serviceNamespace);
        const labels = target.deployment.spec.template.metadata?.labels ?? {};
        if (!service || serviceNamespace !== target.namespace || !record(service.spec?.selector) || !Object.keys(service.spec.selector).length || !selected({ matchLabels: service.spec.selector }, labels)) throw new Error("service_target");
        if (items.some(i => i.kind === "Deployment" && i !== target.deployment && namespaceOf(i, namespace) === target.namespace &&
          selected({ matchLabels: service.spec.selector }, i.spec?.template?.metadata?.labels ?? {}))) throw new Error("service_target");
        const ports = service.spec?.ports?.filter((p: any) => (p.protocol ?? "TCP") === "TCP" && p.port === Number(match[2]));
        if (ports?.length !== 1) throw new Error("service_port");
        const port = ports[0].targetPort ?? ports[0].port;
        let endpointPort = port;
        if (typeof port === "string") {
          const named = (target.container.ports ?? []).filter((p: any) => p.name === port && (p.protocol ?? "TCP") === "TCP");
          if (named.length !== 1) throw new Error("service_port");
          endpointPort = named[0].containerPort;
        }
        if (!portNumber(endpointPort)) throw new Error("service_port");
        const targetProcess = processByName.get(r.target)!;
        const listener = value(targetEnv, targetProcess.rpcAddressEnv ?? targetProcess.addressEnv, target.namespace);
        if (!new RegExp(`^(?:0\\.0\\.0\\.0|\\[::\\])?:${endpointPort}$`).test(listener)) throw new Error("listener_port");
        const tokenEntry = callerEnv.get(r.tokenEnv), peersEntry = targetEnv.get(r.peerTokensEnv);
        if (Object.hasOwn(tokenEntry ?? {}, "value") || Object.hasOwn(peersEntry ?? {}, "value")) throw new Error("secret_reference");
        const token = secretText(tokenEntry?.valueFrom?.secretKeyRef, caller.namespace).trim();
        const peers = JSON.parse(secretText(peersEntry?.valueFrom?.secretKeyRef, target.namespace));
        const validPrincipals = new Set(["console_bff", "control_plane", "gateway", ...topology.processes.map((process: ObjectValue) => process.ownerName).filter((name: unknown): name is string => typeof name === "string")]);
        if (token.length < 32 || !record(peers) || Object.entries(peers).some(([principal, peerToken]) =>
          !validPrincipals.has(principal.trim()) || typeof peerToken !== "string" || peerToken.trim().length < 32) ||
          typeof peers[r.callerPrincipal] !== "string" || peers[r.callerPrincipal].trim() !== token) throw new Error("peer_admission");
        const policies = items.filter(i => i.kind === "NetworkPolicy");
        const allows = (direction: "Ingress" | "Egress", local: typeof caller, remote: typeof caller) => {
          const localLabels = local.deployment.spec.template.metadata?.labels ?? {};
          const applicable = policies.filter(p => namespaceOf(p, namespace) === local.namespace && selected(p.spec?.podSelector, localLabels) &&
            (p.spec.policyTypes ?? ["Ingress", ...(p.spec.egress?.length ? ["Egress"] : [])]).includes(direction));
          if (!applicable.length) return true;
          return applicable.some(p => (p.spec[direction === "Ingress" ? "ingress" : "egress"] ?? []).some((rule: any) => {
            const portMatches = rule.ports === undefined || (Array.isArray(rule.ports) && (rule.ports.length === 0 || rule.ports.some((p: any) => {
              if ((p.protocol ?? "TCP") !== "TCP") return false;
              if (p.port === undefined) return true;
              if (typeof p.port === "string") return (target.container.ports ?? []).some((np: any) => np.name === p.port && np.containerPort === endpointPort && (np.protocol ?? "TCP") === "TCP");
              return portNumber(p.port) && (p.endPort === undefined ? endpointPort === p.port : portNumber(p.endPort) && p.endPort >= p.port && endpointPort >= p.port && endpointPort <= p.endPort);
            })));
            if (!portMatches) return false;
            const peers = rule[direction === "Ingress" ? "from" : "to"];
            if (peers === undefined || (Array.isArray(peers) && peers.length === 0)) return true;
            if (!Array.isArray(peers)) throw new Error("network_policy_invalid");
            return peers.some((peer: any) => {
              // IP blocks cannot prove a workload-selector relation before Pod
              // IP assignment; this admission seam requires namespace/pod peers.
              if (!record(peer) || peer.ipBlock) return false;
              if (!peer.namespaceSelector && !peer.podSelector) return true;
              if (peer.namespaceSelector) {
                const ns = get("Namespace", remote.namespace, namespace);
                if (!selected(peer.namespaceSelector, { ...ns?.metadata?.labels, "kubernetes.io/metadata.name": remote.namespace })) return false;
              } else if (local.namespace !== remote.namespace) return false;
              return !peer.podSelector || selected(peer.podSelector, remote.deployment.spec.template.metadata?.labels ?? {});
            });
          }));
        };
        if (!allows("Ingress", target, caller) || !allows("Egress", caller, target)) throw new Error("network_admission");
      } catch (error) {
        const allowed = new Set(["secret_reference", "secret_missing", "secret_invalid", "env_source_invalid", "env_source_missing", "env_duplicate", "env_missing", "env_invalid", "env_value_invalid", "address_invalid", "bindings_invalid", "service_target", "service_port", "listener_port", "peer_admission", "selector_invalid", "network_policy_invalid", "network_admission"]);
        const message = error instanceof Error ? error.message : "";
        fail(r.id, allowed.has(message) ? message : "material_invalid");
      }
    }
    if (!checkedRequirements) fail("installation", "no_requirements_selected");
  } catch (error) {
    const allowed = new Set(["list_invalid", "bindings_invalid", "requirements_invalid", "resource_invalid", "resource_duplicate", "binding_invalid", "binding_duplicate", "deployment_missing", "container_missing", "process_command_mismatch"]);
    const message = error instanceof Error ? error.message : "";
    fail("installation", allowed.has(message) ? message : "installation_invalid");
  }
  return result();
}
