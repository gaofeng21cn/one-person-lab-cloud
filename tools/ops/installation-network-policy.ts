// A three-valued proof over Kubernetes NetworkPolicy's additive allow rules.
// Unknown data cannot establish an allow. No CIDR-to-pod or namespace-label guess
// is made; the caller supplies only the rendered installation's known facts.
type Resource = Record<string, any>;
type Proof = true | false | undefined;
export type PolicyEndpoint = { deployment: Resource; container: Resource };
const object = (value: unknown): value is Resource => value !== null && typeof value === "object" && !Array.isArray(value);
const namespace = (resource: Resource): string => resource.metadata.namespace ?? "default";
const labels = (endpoint: PolicyEndpoint): Resource => endpoint.deployment.spec.template.metadata?.labels ?? {};
const validPort = (value: unknown): value is number => Number.isInteger(value) && Number(value) > 0 && Number(value) <= 65535;
function and(proofs: Proof[]): Proof {
  return proofs.includes(false) ? false : proofs.includes(undefined) ? undefined : true;
}
function or(proofs: Proof[]): Proof {
  return proofs.includes(true) ? true : proofs.includes(undefined) ? undefined : false;
}

export function validSelector(selector: unknown): selector is Resource {
  if (!object(selector) || Object.keys(selector).some(key => !["matchLabels", "matchExpressions"].includes(key))) return false;
  if (selector.matchLabels !== undefined && (!object(selector.matchLabels) || Object.values(selector.matchLabels).some(value => typeof value !== "string"))) return false;
  if (selector.matchExpressions !== undefined && (!Array.isArray(selector.matchExpressions) || selector.matchExpressions.some((expression: unknown) => {
    if (!object(expression) || typeof expression.key !== "string" || !expression.key ||
        Object.keys(expression).some(key => !["key", "operator", "values"].includes(key))) return true;
    const values = expression.values;
    if (values !== undefined && (!Array.isArray(values) || values.some(value => typeof value !== "string"))) return true;
    if (expression.operator === "In" || expression.operator === "NotIn") return !values?.length;
    if (expression.operator === "Exists" || expression.operator === "DoesNotExist") return Boolean(values?.length);
    return true;
  }))) return false;
  return true;
}
function selects(selector: unknown, knownLabels: Resource, complete = true): Proof {
  if (!validSelector(selector)) return undefined;
  const proofs: Proof[] = Object.entries(selector.matchLabels ?? {}).map(([key, value]) =>
    Object.hasOwn(knownLabels, key) ? knownLabels[key] === value : complete ? false : undefined);
  for (const expression of selector.matchExpressions ?? []) {
    const present = Object.hasOwn(knownLabels, expression.key);
    if (!present && !complete) { proofs.push(undefined); continue; }
    switch (expression.operator) {
      case "In": proofs.push(present && expression.values.includes(knownLabels[expression.key])); break;
      case "NotIn": proofs.push(!present || !expression.values.includes(knownLabels[expression.key])); break;
      case "Exists": proofs.push(present); break;
      case "DoesNotExist": proofs.push(!present); break;
    }
  }
  return and(proofs);
}
function namespaceMatches(selector: unknown, endpoint: PolicyEndpoint, resources: Resource[]): Proof {
  const name = namespace(endpoint.deployment);
  const declared = resources.find(resource => resource.kind === "Namespace" && resource.apiVersion === "v1" && resource.metadata.name === name);
  return selects(selector, { ...(declared?.metadata.labels ?? {}), "kubernetes.io/metadata.name": name }, declared !== undefined);
}
function peerMatches(peer: unknown, policy: Resource, endpoint: PolicyEndpoint, resources: Resource[]): Proof {
  if (!object(peer) || Object.keys(peer).some(key => !["podSelector", "namespaceSelector", "ipBlock"].includes(key))) return undefined;
  if (peer.ipBlock !== undefined) return undefined;
  const proofs: Proof[] = [];
  if (peer.namespaceSelector !== undefined) proofs.push(namespaceMatches(peer.namespaceSelector, endpoint, resources));
  else if (peer.podSelector !== undefined) proofs.push(namespace(policy) === namespace(endpoint.deployment));
  if (peer.podSelector !== undefined) proofs.push(selects(peer.podSelector, labels(endpoint)));
  return and(proofs);
}
function portMatches(entry: unknown, target: PolicyEndpoint, port: number): Proof {
  if (!object(entry) || Object.keys(entry).some(key => !["port", "endPort", "protocol"].includes(key))) return undefined;
  if (!["TCP", "UDP", "SCTP"].includes(entry.protocol ?? "TCP")) return undefined;
  if ((entry.protocol ?? "TCP") !== "TCP") return false;
  if (entry.port === undefined) return entry.endPort === undefined ? true : undefined;
  if (typeof entry.port === "string") {
    if (entry.endPort !== undefined) return undefined;
    const ports = (target.container.ports ?? []).filter((value: Resource) => value.name === entry.port && (value.protocol ?? "TCP") === "TCP");
    return ports.length === 1 ? ports[0].containerPort === port : undefined;
  }
  if (!validPort(entry.port) || entry.endPort !== undefined && (!validPort(entry.endPort) || entry.endPort < entry.port)) return undefined;
  return entry.port <= port && port <= (entry.endPort ?? entry.port);
}
function allows(policy: Resource, direction: "Ingress" | "Egress", remote: PolicyEndpoint, target: PolicyEndpoint, port: number, resources: Resource[]): Proof {
  const rules = policy.spec[direction === "Ingress" ? "ingress" : "egress"] ?? [];
  if (!Array.isArray(rules)) return undefined;
  return or(rules.map((rule: unknown) => {
    const peerKey = direction === "Ingress" ? "from" : "to";
    if (!object(rule) || Object.keys(rule).some(key => ![peerKey, "ports"].includes(key))) return undefined;
    const peers = rule[peerKey];
    if (peers !== undefined && !Array.isArray(peers) || rule.ports !== undefined && !Array.isArray(rule.ports)) return undefined;
    return and([
      !peers?.length ? true : or(peers.map((peer: unknown) => peerMatches(peer, policy, remote, resources))),
      !rule.ports?.length ? true : or(rule.ports.map((entry: unknown) => portMatches(entry, target, port)))
    ]);
  }));
}
function directionProof(direction: "Ingress" | "Egress", endpoint: PolicyEndpoint, remote: PolicyEndpoint, target: PolicyEndpoint, port: number, resources: Resource[]): { error?: string; policies: string[] } {
  const policies: string[] = [];
  const result = (error?: string) => ({ error, policies: policies.sort() });
  let isolation: Proof = false;
  const grants: Proof[] = [];
  for (const policy of resources.filter(resource => resource.kind === "NetworkPolicy" && namespace(resource) === namespace(endpoint.deployment))) {
    if (policy.apiVersion !== "networking.k8s.io/v1" || !object(policy.spec)) return result(`rpc_${direction.toLowerCase()}_unproven`);
    // Kubernetes defaults to Ingress; Egress is added only for nonempty egress.
    const types = policy.spec.policyTypes?.length ? policy.spec.policyTypes : ["Ingress", ...(policy.spec.egress?.length ? ["Egress"] : [])];
    if (!Array.isArray(types) || types.some(type => !["Ingress", "Egress"].includes(type))) return result(`rpc_${direction.toLowerCase()}_unproven`);
    if (!types.includes(direction)) continue;
    const selection = selects(policy.spec.podSelector, labels(endpoint));
    if (selection === true) policies.push(policy.metadata.name);
    isolation = or([isolation, selection]);
    grants.push(and([selection, allows(policy, direction, remote, target, port, resources)]));
  }
  const prefix = `rpc_${direction.toLowerCase()}`;
  if (direction === "Egress" && isolation === false) return result();
  if (or(grants) === true) return result();
  if (isolation === false) return result(`${prefix}_unisolated`);
  if (isolation === undefined || or(grants) === undefined) return result(`${prefix}_unproven`);
  return result(`${prefix}_denied`);
}

export function communicationPolicyProof(caller: PolicyEndpoint, target: PolicyEndpoint, port: number, resources: Resource[]): { error?: string; ingressPolicies: string[]; egressPolicies: string[] } {
  const ingress = directionProof("Ingress", target, caller, target, port, resources);
  const egress = directionProof("Egress", caller, target, target, port, resources);
  return { error: ingress.error ?? egress.error, ingressPolicies: ingress.policies, egressPolicies: egress.policies };
}
