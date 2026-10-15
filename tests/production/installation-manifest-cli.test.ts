import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { spawnSync } from "node:child_process";
import { runProductionManifestCli } from "../../tools/validate-production-manifest.ts";

// Independent Kubernetes examples: neither the edges nor env names are generated
// from the declaration being tested.
function installation() {
  const token = "synthetic-only-private-token-".repeat(3);
  const items: any[] = [];
  const processes: Record<string, { deployment: string; container: string }> = {};
  function owner(service: string, listen: string, port: number, env: any[] = []) {
    processes[service] = { deployment: service, container: service };
    items.push({ apiVersion: "apps/v1", kind: "Deployment", metadata: { name: service }, spec: { template: {
      metadata: { labels: { component: service } }, spec: { containers: [{ name: service,
        command: [`/usr/local/bin/opl-${service}`], ports: [{ name: "rpc", containerPort: port }],
        env: [{ name: listen, value: `:${port}` }, ...env] }] }
    } } });
    items.push({ apiVersion: "v1", kind: "Service", metadata: { name: service }, spec: {
      selector: { component: service }, ports: [{ port, targetPort: "rpc", protocol: "TCP" }]
    } });
    items.push({ apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", metadata: { name: `${service}-deny` },
      spec: { podSelector: { matchLabels: { component: service } }, policyTypes: ["Ingress"], ingress: [] } });
  }
  const ref = (name: string, key = name) => ({ name, valueFrom: { secretKeyRef: { name: "owner-auth", key } } });
  owner("serve", "OPL_SERVE_ADDR", 8185, [
    { name: "OPL_WORKSPACE_ADDR", value: "workspace:8184" }, ref("OPL_WORKSPACE_TOKEN")
  ]);
  owner("workspace", "OPL_WORKSPACE_ADDR", 8184, [ref("OPL_WORKSPACE_PEER_TOKENS")]);
  owner("build", "OPL_BUILD_ADDR", 8182, [
    { name: "OPL_CAPABILITY_ADDR", value: "capability:8181" }, ref("OPL_CAPABILITY_TOKEN")
  ]);
  owner("capability", "OPL_CAPABILITY_ADDR", 8181, [ref("OPL_CAPABILITY_PEER_TOKENS")]);
  for (const [caller, target, port] of [["serve", "workspace", 8184], ["build", "capability", 8181]] as const) {
    items.push({ apiVersion: "networking.k8s.io/v1", kind: "NetworkPolicy", metadata: { name: `${caller}-${target}` }, spec: {
      podSelector: { matchLabels: { component: target } }, policyTypes: ["Ingress"], ingress: [{
        from: [{ podSelector: { matchLabels: { component: caller } } }], ports: [{ port, protocol: "TCP" }]
      }]
    } });
  }
  return { manifest: { apiVersion: "v1", kind: "List", items },
    bindings: { schemaVersion: 1, namespace: "synthetic", processes },
    secrets: { apiVersion: "v1", kind: "List", items: [{ apiVersion: "v1", kind: "Secret", metadata: { name: "owner-auth" }, stringData: {
      OPL_WORKSPACE_TOKEN: token, OPL_CAPABILITY_TOKEN: token,
      OPL_WORKSPACE_PEER_TOKENS: JSON.stringify({ serve: token }),
      OPL_CAPABILITY_PEER_TOKENS: JSON.stringify({ build: token })
    } }] }
  };
}

async function cli(value: ReturnType<typeof installation>) {
  const root = await mkdtemp(join(tmpdir(), "opl-installation-"));
  try {
    for (const [name, data] of Object.entries(value)) await writeFile(join(root, `${name}.json`), JSON.stringify(data));
    let stdout = "", stderr = "";
    const code = await runProductionManifestCli({ argv: ["--installation-manifest", join(root, "manifest.json"),
      "--installation-bindings", join(root, "bindings.json"), "--installation-secrets", join(root, "secrets.json")],
      stdout: { write: (s: string) => { stdout += s; } }, stderr: { write: (s: string) => { stderr += s; } } });
    assert.equal(stdout.includes("synthetic-only-private-token"), false);
    assert.equal(stdout.includes("workspace:8184"), false);
    return { code, report: JSON.parse(stdout), stderr };
  } finally { await rm(root, { recursive: true, force: true }); }
}

const container = (v: ReturnType<typeof installation>, service: string) => v.manifest.items.find(x => x.kind === "Deployment" && x.metadata.name === service).spec.template.spec.containers[0];

test("installation CLI admits Serve access and independent Build catalog wiring", async () => {
  const { code, report } = await cli(installation());
  assert.equal(code, 0, JSON.stringify(report));
  assert.equal(report.ok, true);
  assert.equal(report.checkedRequirements, 2);
});

for (const scenario of ["address", "token", "peer", "network", "wrong-token", "listener", "wrong-service", "egress", "duplicate-env"] as const) {
  test(`installation CLI rejects missing or conflicting ${scenario} at the public seam`, async () => {
    const v = installation();
    const env = container(v, "serve").env;
    if (scenario === "address") container(v, "serve").env = env.filter((e: any) => e.name !== "OPL_WORKSPACE_ADDR");
    if (scenario === "token") container(v, "serve").env = env.filter((e: any) => e.name !== "OPL_WORKSPACE_TOKEN");
    if (scenario === "peer") v.secrets.items[0].stringData.OPL_WORKSPACE_PEER_TOKENS = "{}";
    if (scenario === "network") v.manifest.items = v.manifest.items.filter(i => i.metadata.name !== "serve-workspace");
    if (scenario === "wrong-token") v.secrets.items[0].stringData.OPL_WORKSPACE_PEER_TOKENS = JSON.stringify({ serve: "other-token-".repeat(8) });
    if (scenario === "listener") env.find((e: any) => e.name === "OPL_WORKSPACE_ADDR").value = ":8184";
    if (scenario === "wrong-service") v.manifest.items.find(i => i.kind === "Service" && i.metadata.name === "workspace").spec.selector.component = "build";
    if (scenario === "egress") v.manifest.items.push({ kind: "NetworkPolicy", metadata: { name: "serve-egress-deny" }, spec: {
      podSelector: { matchLabels: { component: "serve" } }, policyTypes: ["Egress"], egress: [] } });
    if (scenario === "duplicate-env") env.push({ name: "OPL_WORKSPACE_ADDR", value: "workspace:8184" });
    const { code, report } = await cli(v);
    assert.equal(code, 1);
    assert.equal(report.ok, false);
    assert.ok(report.failures.length > 0);
  });
}

test("installation CLI resolves ConfigMap envFrom and Kubernetes env precedence", async () => {
  const v = installation();
  const c = container(v, "serve");
  c.env = c.env.filter((e: any) => e.name !== "OPL_WORKSPACE_ADDR");
  c.envFrom = [{ configMapRef: { name: "wiring" } }];
  v.manifest.items.push({ kind: "ConfigMap", metadata: { name: "wiring" }, data: { OPL_WORKSPACE_ADDR: "workspace.synthetic.svc.cluster.local:8184" } });
  assert.equal((await cli(v)).code, 0);
  c.env.push({ name: "OPL_WORKSPACE_ADDR", value: ":8184" });
  assert.equal((await cli(v)).code, 1);
});

test("installation CLI rejects unknown binding processes rather than skipping requirements", async () => {
  const v = installation();
  v.bindings.processes["not-a-product-process"] = v.bindings.processes.serve;
  delete v.bindings.processes.serve;
  assert.equal((await cli(v)).code, 1);
});

for (const scenario of ["additive", "empty-rule", "named-port", "range", "namespace-denied", "wrong-port", "shared-service-selector", "hidden-caller"] as const) {
  test(`installation CLI applies Kubernetes ${scenario} semantics`, async () => {
    const v = installation();
    const policy = v.manifest.items.find(i => i.metadata.name === "serve-workspace");
    if (scenario === "additive") v.manifest.items.push({ kind: "NetworkPolicy", metadata: { name: "extra-deny" }, spec: {
      podSelector: { matchLabels: { component: "workspace" } }, policyTypes: ["Ingress"], ingress: [] } });
    if (scenario === "empty-rule") policy.spec.ingress = [{ from: [], ports: [] }];
    if (scenario === "named-port") policy.spec.ingress[0].ports = [{ port: "rpc" }];
    if (scenario === "range") policy.spec.ingress[0].ports = [{ port: 8180, endPort: 8190 }];
    if (scenario === "namespace-denied") policy.spec.ingress[0].from = [{ namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": "another" } } }];
    if (scenario === "wrong-port") policy.spec.ingress[0].ports = [{ port: 8183 }];
    if (scenario === "shared-service-selector") v.manifest.items.find(i => i.kind === "Deployment" && i.metadata.name === "build").spec.template.metadata.labels.component = "workspace";
    if (scenario === "hidden-caller") delete v.bindings.processes.serve;
    const expected = ["namespace-denied", "wrong-port", "shared-service-selector", "hidden-caller"].includes(scenario) ? 1 : 0;
    assert.equal((await cli(v)).code, expected);
  });
}

test("installation CLI requires both migrated relationships when both callers are installed", async () => {
  const v = installation();
  container(v, "build").env = container(v, "build").env.filter((e: any) => e.name !== "OPL_CAPABILITY_TOKEN");
  const { code, report } = await cli(v);
  assert.equal(code, 1);
  assert.ok(report.failures.some((f: any) => f.requirement === "build-capability"));
});


test("installation command process preserves exit/stdout/stderr and sanitizes malformed private input", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-installation-process-"));
  try {
    const v = installation();
    for (const [name, data] of Object.entries(v)) await writeFile(join(root, `${name}.json`), JSON.stringify(data));
    const args = ["tools/validate-production-manifest.ts", "--installation-manifest", join(root, "manifest.json"),
      "--installation-bindings", join(root, "bindings.json"), "--installation-secrets", join(root, "secrets.json")];
    const admitted = spawnSync(process.execPath, args, { encoding: "utf8", timeout: 10000 });
    assert.equal(admitted.status, 0, admitted.stderr);
    assert.equal(JSON.parse(admitted.stdout).ok, true);
    assert.equal(admitted.stderr, "");
    v.secrets.items[0].stringData.OPL_WORKSPACE_PEER_TOKENS = "{}";
    await writeFile(join(root, "secrets.json"), JSON.stringify(v.secrets));
    const denied = spawnSync(process.execPath, args, { encoding: "utf8", timeout: 10000 });
    assert.equal(denied.status, 1);
    assert.equal(JSON.parse(denied.stdout).ok, false);
    assert.equal(denied.stderr, "installation_manifest_invalid\n");
    await writeFile(join(root, "secrets.json"), '{"synthetic-private-data":"secret" INVALID}');
    const malformed = spawnSync(process.execPath, args, { encoding: "utf8", timeout: 10000 });
    assert.equal(malformed.status, 1);
    assert.equal(malformed.stderr, "installation_input_invalid\n");
    assert.equal(malformed.stdout, "");
  } finally { await rm(root, { recursive: true, force: true }); }
});
