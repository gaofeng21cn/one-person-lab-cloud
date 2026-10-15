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
    tenant: [["fabric", "OPL_FABRIC_ADDR", "OPL_FABRIC_TOKEN"], ["build", "OPL_BUILD_ADDR", "OPL_BUILD_TOKEN"], ["workspace", "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_TOKEN"]],
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
