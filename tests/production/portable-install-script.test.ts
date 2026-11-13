import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { canonicalJson, PORTABLE_ASSETS } from "../../tools/cloud-candidate-receipt.ts";

const IMAGE_DIGEST = `sha256:${"a".repeat(64)}`;
const IMAGE_REFERENCE = `ghcr.io/gaofeng21cn/one-person-lab-cloud@${IMAGE_DIGEST}`;
const SOURCE_SHA = "1".repeat(40);
const SOURCE_TREE = "2".repeat(40);
const AMD64_DIGEST = `sha256:${"b".repeat(64)}`;
const ARM64_DIGEST = `sha256:${"c".repeat(64)}`;

function sourceOf(name: string) {
  if (name === "compose.yaml") return "compose.yaml";
  if (name === "opl-cloud-distribution-contract.json") return "packages/contracts/opl-cloud-distribution-contract.json";
  return `deploy/portable/${name}`;
}
function sha256Of(bytes: Buffer) { return createHash("sha256").update(bytes).digest("hex"); }
function candidateReceipt(assets: { name: string; sha256: string }[]) {
  return { assets, cloudImage: { indexDigest: IMAGE_DIGEST, indexRef: IMAGE_REFERENCE,
    platforms: [{ digest: AMD64_DIGEST, platform: "linux/amd64" }, { digest: ARM64_DIGEST, platform: "linux/arm64" }],
    repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud", revision: SOURCE_SHA },
    kind: "opl_cloud_candidate", product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: SOURCE_SHA, tree: SOURCE_TREE },
    provenance: { workflowRepository: "gaofeng21cn/one-person-lab-cloud", workflowRunAttempt: "1", workflowRunId: "1", workflowSha: SOURCE_SHA }, schemaVersion: 2 };
}
async function writeBundle(root: string) {
  const bundle = join(root, "bundle"); await mkdir(bundle, { recursive: true });
  const assets = [];
  for (const name of PORTABLE_ASSETS) { const bytes = await readFile(sourceOf(name)); await writeFile(join(bundle, name), bytes); if (name === "install.sh") await chmod(join(bundle, name), 0o755); assets.push({ name, sha256: sha256Of(bytes) }); }
  await writeFile(join(bundle, "opl-cloud-candidate.json"), `${canonicalJson(candidateReceipt(assets))}\n`);
  const names = [...PORTABLE_ASSETS, "opl-cloud-candidate.json"];
  const sumLines = [];
  for (const name of names) sumLines.push(`${sha256Of(await readFile(join(bundle, name)))}  ${name}`);
  await writeFile(join(bundle, "SHA256SUMS"), `${sumLines.join("\n")}\n`);
  return bundle;
}
async function withBundle(body: (bundle: string, root: string) => Promise<void>) {
  const root = await mkdtemp(join(tmpdir(), "opl-cloud-install-"));
  try { await body(await writeBundle(root), root); } finally { spawnSync("python3", ["-c", "import shutil,sys; shutil.rmtree(sys.argv[1], ignore_errors=True)", root]); }
}
function run(bundle: string, installDir: string, args: string[] = [], env: NodeJS.ProcessEnv = {}) {
  return spawnSync("bash", [join(bundle, "install.sh"), "--install-dir", installDir, ...args], { cwd: tmpdir(), encoding: "utf8", env: { ...process.env, OPL_CLOUD_IMAGE: "ghcr.io/evil/wrong:latest", COMPOSE_PROJECT_NAME: "evil", ...env } });
}

test("portable asset set includes the one-command installer and product topology", async () => {
  assert.ok(PORTABLE_ASSETS.includes("install.sh"));
  assert.ok(PORTABLE_ASSETS.includes("install.mjs"));
  assert.ok(PORTABLE_ASSETS.includes("compose.cloud.yaml"));
  assert.ok(PORTABLE_ASSETS.includes("agent-plugins-1.0.0-plugin.schema.json"));
  const result = spawnSync(process.execPath, ["tools/cloud-candidate-receipt.ts", "validate-bundle", "--bundle", "."], { encoding: "utf8" });
  assert.notEqual(result.status, 0, "the source tree is not a downloaded bundle");
});

test("installer manifest rejects checksum and extra-file drift before execution", async () => {
  await withBundle(async (bundle, root) => {
    await writeFile(join(bundle, "compose.cloud.yaml"), `${await readFile(join(bundle, "compose.cloud.yaml"), "utf8")}\n# tampered\n`);
    const result = run(bundle, join(root, "install"), ["--dry-run"]);
    assert.notEqual(result.status, 0); assert.match(result.stderr, /cloud_installation_bundle_invalid/);
  });
});

test("first run persists immutable identity and explicit missing facts outside the bundle", async () => {
  await withBundle(async (bundle, root) => {
    const installDir = join(root, "install");
    const result = run(bundle, installDir, ["--dry-run"]);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);
    const state = JSON.parse(await readFile(join(installDir, "installation.json"), "utf8"));
    assert.equal(state.imageRef, IMAGE_REFERENCE);
    assert.equal(state.sourceSha, SOURCE_SHA);
    assert.notEqual(state.values.OPL_CLOUD_IMAGE, "ghcr.io/evil/wrong:latest");
    assert.match(state.values.OPL_WORKSPACE_DOMAIN, /^unset-/);
    assert.equal(state.values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH, join(installDir, "schemas", "agent-plugins-1.0.0-plugin.schema.json"));
    assert.equal(sha256Of(await readFile(state.values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH)), "0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883");
  });
});

test("installation state retains operator facts while updates replace only immutable bundle facts", async () => {
  await withBundle(async (bundle, root) => {
    const installDir = join(root, "install");
    const first = run(bundle, installDir, ["--dry-run"]); assert.equal(first.status, 2);
    const path = join(installDir, "installation.json"); const state = JSON.parse(await readFile(path, "utf8"));
    state.values.OPL_WORKSPACE_DOMAIN = "ws.example.test";
    state.values.OPL_CAPABILITY_OBJECT_URL = "https://objects.example.test";
    state.values.OPL_SUB2API_URL = "https://sub2api.example.test";
    state.values.OPL_SUB2API_BASE_URL = "https://sub2api.example.test";
    state.values.OPL_SUB2API_ADMIN_EMAIL = "admin@example.test";
    state.values.OPL_SUB2API_USER_EMAIL = "user@example.test";
    state.values.OPL_WORKSPACE_IMAGE = `ghcr.io/example/workspace@sha256:${"d".repeat(64)}`;
    state.values.OPL_WORKSPACE_REGISTRY_HOST = "registry.example.test";
    state.values.OPL_WORKSPACE_REGISTRY_NAMESPACE = "opl";
    state.values.OPL_GATEWAY_WORKSPACE_KEY_GROUP = "default";
    state.values.OPL_PLATFORM_ADMIN_SUBJECTS = "admin@example.test";
    state.values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON = JSON.stringify({ schemaVersion: 1 });
    state.values.OPL_RESOURCE_CATALOG_PROFILE_JSON = JSON.stringify({ provider: "local-docker" });
    state.values.OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES = state.values.OPL_WORKSPACE_IMAGE;
    await writeFile(path, `${JSON.stringify(state)}\n`);
    const second = run(bundle, installDir, ["--dry-run"]); assert.equal(second.status, 0, `${second.stdout}${second.stderr}`);
    const updated = JSON.parse(await readFile(path, "utf8"));
    assert.equal(updated.values.OPL_WORKSPACE_DOMAIN, "ws.example.test");
    assert.equal(updated.values.OPL_SUB2API_BASE_URL, "https://sub2api.example.test");
    assert.equal(updated.values.OPL_CLOUD_IMAGE, IMAGE_REFERENCE);
  });
});

test("pre-rename installation state migrates once without replacing custom facts, secrets or PKI", async () => {
  await withBundle(async (bundle, root) => {
    const installDir = join(root, "install");
    assert.equal(run(bundle, installDir, ["--dry-run"]).status, 2);
    const statePath = join(installDir, "installation.json");
    const state = JSON.parse(await readFile(statePath, "utf8"));
    const pki = join(root, "retained-custom-pki");
    const schema = join(root, "retained-schema", "plugin.schema.json");
    await mkdir(pki, { recursive: true });
    await mkdir(join(root, "retained-schema"), { recursive: true });
    const schemaBytes = await readFile(
      join(bundle, "agent-plugins-1.0.0-plugin.schema.json"),
    );
    await writeFile(schema, schemaBytes);
    const pkiFiles = [
      "ca.crt",
      "ca.key",
      "proxy.crt",
      "proxy.key",
      ...[
        "tenant",
        "capability",
        "build",
        "runtime_control",
        "workspace",
        "serve",
        "resource_catalog",
        "ledger",
        "fabric",
        "console_bff",
        "control_plane",
      ].flatMap((name) => [`${name}.crt`, `${name}.key`]),
    ];
    for (const name of pkiFiles)
      await writeFile(join(pki, name), `retained-${name}`);
    // The schemaVersion:1 shape and these names are the state written by the
    // installer shipped before the installation-fact rename (PR #1174).
    const oldFacts: Record<string, [string, string]> = {
      OPL_LOCAL_V2_SUBNET: ["OPL_DOCKER_SUBNET", "172.29.100.0/24"],
      OPL_LOCAL_V2_POSTGRES_HOST: ["OPL_POSTGRES_HOST", "172.29.100.10"],
      OPL_LOCAL_V2_PROXY_PORT: ["OPL_PROXY_PORT", "9443"],
      OPL_LOCAL_V2_GATEWAY_CONTAINER: [
        "OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER",
        "retained-gateway",
      ],
      OPL_LOCAL_V2_PUBLIC_URL: ["OPL_PUBLIC_URL", "https://localhost:9443"],
      OPL_LOCAL_V2_SUB2API_URL: [
        "OPL_SUB2API_URL",
        "https://sub2api.example.test",
      ],
      OPL_LOCAL_V2_CAPABILITY_OBJECT_URL: [
        "OPL_CAPABILITY_OBJECT_URL",
        "https://localhost:9443/objects",
      ],
      OPL_LOCAL_V2_CAPABILITY_OBJECT_INTERNAL_URL: [
        "OPL_CAPABILITY_OBJECT_INTERNAL_URL",
        "https://cloud-local-v2:9443/objects",
      ],
      OPL_LOCAL_V2_PKI_DIR: ["OPL_PKI_DIR", pki],
      OPL_LOCAL_V2_PACKAGE_SCHEMA_PATH: [
        "OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH",
        schema,
      ],
      OPL_LOCAL_V2_PACKAGE_SCHEMA_DIGEST: [
        "OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST",
        "sha256:0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883",
      ],
      OPL_LOCAL_V2_PUBLISHER_SCHEMA_DIGEST: [
        "OPL_PUBLISHER_SCHEMA_DIGEST",
        "sha256:176d492a0c0c491ca4e180d9e5d3e04a08ab9c621c4254806658ef529c77b5ef",
      ],
    };
    for (const [oldName, [name, value]] of Object.entries(oldFacts)) {
      delete state.values[name];
      state.values[oldName] = value;
    }
    Object.assign(state.values, {
      OPL_WORKSPACE_DOMAIN: "ws.example.test",
      OPL_SUB2API_BASE_URL: "https://sub2api.example.test",
      OPL_SUB2API_ADMIN_EMAIL: "admin@example.test",
      OPL_SUB2API_USER_EMAIL: "user@example.test",
      OPL_WORKSPACE_IMAGE: `ghcr.io/example/workspace@sha256:${"d".repeat(64)}`,
      OPL_WORKSPACE_REGISTRY_HOST: "registry.example.test",
      OPL_WORKSPACE_REGISTRY_NAMESPACE: "opl",
      OPL_GATEWAY_WORKSPACE_KEY_GROUP: "default",
      OPL_PLATFORM_ADMIN_SUBJECTS: "admin@example.test",
      OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: JSON.stringify({
        schemaVersion: 1,
      }),
      OPL_RESOURCE_CATALOG_PROFILE_JSON: JSON.stringify({
        provider: "local-docker",
      }),
    });
    state.values.OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES =
      state.values.OPL_WORKSPACE_IMAGE;
    const retainedSecrets = Object.fromEntries(
      Object.entries(state.values).filter(([name]) =>
        /(?:PASSWORD|TOKEN|KEY|SEED)$/.test(name),
      ),
    );
    await writeFile(statePath, JSON.stringify(state));
    const second = run(bundle, installDir, ["--dry-run"]);
    assert.equal(second.status, 0, `${second.stdout}${second.stderr}`);
    const updated = JSON.parse(await readFile(statePath, "utf8"));
    for (const [oldName, [name, value]] of Object.entries(oldFacts)) {
      assert.equal(updated.values[name], value, name);
      assert.ok(!(oldName in updated.values), oldName);
    }
    for (const [name, value] of Object.entries(retainedSecrets))
      assert.equal(updated.values[name], value, name);
    for (const name of pkiFiles)
      assert.equal(await readFile(join(pki, name), "utf8"), `retained-${name}`);
    assert.deepEqual(await readFile(schema), schemaBytes);
    const third = run(bundle, installDir, ["--dry-run"]);
    assert.equal(third.status, 0, `${third.stdout}${third.stderr}`);
    assert.deepEqual(
      JSON.parse(await readFile(statePath, "utf8")).values,
      updated.values,
    );
    // Optional facts must actually reach Compose. An invalid custom port
    // must fail config instead of being silently replaced by the default.
    updated.values.OPL_PROXY_PORT = "not-a-port";
    await writeFile(statePath, JSON.stringify(updated));
    const invalid = run(bundle, installDir, ["--dry-run"]);
    assert.notEqual(invalid.status, 0);
    assert.match(invalid.stderr, /invalid.*port|invalid hostPort/);
  });
});

test("conflicting old and new installation facts fail before changing persisted state", async () => {
  await withBundle(async (bundle, root) => {
    const installDir = join(root, "install");
    assert.equal(run(bundle, installDir, ["--dry-run"]).status, 2);
    const statePath = join(installDir, "installation.json");
    const state = JSON.parse(await readFile(statePath, "utf8"));
    state.values.OPL_LOCAL_V2_PUBLIC_URL = "https://old.example.test";
    state.values.OPL_PUBLIC_URL = "https://new.example.test";
    const original = `${JSON.stringify(state)}\n`;
    await writeFile(statePath, original);
    const result = run(bundle, installDir, ["--dry-run"]);
    assert.notEqual(result.status, 0);
    assert.match(
      result.stderr,
      /installation_fact_conflict:OPL_LOCAL_V2_PUBLIC_URL:OPL_PUBLIC_URL/,
    );
    assert.equal(await readFile(statePath, "utf8"), original);
  });
});

test("full installer accepts Compose NDJSON and refuses missing or non-running services", async () => {
  await withBundle(async (bundle, root) => {
    const installDir = join(root, "install");
    assert.equal(run(bundle, installDir, ["--dry-run"]).status, 2);
    const statePath = join(installDir, "installation.json");
    const state = JSON.parse(await readFile(statePath, "utf8"));
    Object.assign(state.values, {
      OPL_WORKSPACE_DOMAIN: "ws.example.test", OPL_CAPABILITY_OBJECT_URL: "https://objects.example.test",
      OPL_SUB2API_URL: "https://sub2api.example.test", OPL_SUB2API_BASE_URL: "https://sub2api.example.test",
      OPL_SUB2API_ADMIN_EMAIL: "admin@example.test", OPL_SUB2API_USER_EMAIL: "user@example.test",
      OPL_WORKSPACE_IMAGE: `ghcr.io/example/workspace@sha256:${"d".repeat(64)}`,
      OPL_WORKSPACE_REGISTRY_HOST: "registry.example.test", OPL_WORKSPACE_REGISTRY_NAMESPACE: "opl",
      OPL_GATEWAY_WORKSPACE_KEY_GROUP: "default", OPL_PLATFORM_ADMIN_SUBJECTS: "admin@example.test",
      OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: JSON.stringify({ schemaVersion: 1 }),
      OPL_RESOURCE_CATALOG_PROFILE_JSON: JSON.stringify({ provider: "local-docker" })
    });
    state.values.OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES = state.values.OPL_WORKSPACE_IMAGE;
    await writeFile(statePath, JSON.stringify(state));
    const realDocker = spawnSync("which", ["docker"], { encoding: "utf8" }).stdout.trim();
    assert.ok(realDocker);
    const bin = join(root, "mock-bin"); await mkdir(bin);
    const log = join(root, "docker-calls.jsonl");
    const scenario = join(root, "scenario");
    const mock = `#!${process.execPath}
const { appendFileSync, readFileSync } = require('node:fs');
const { execFileSync } = require('node:child_process');
const args = process.argv.slice(2);
appendFileSync(${JSON.stringify(log)}, JSON.stringify(args) + '\\n');
if (args[0] === 'inspect') console.log(${JSON.stringify(IMAGE_REFERENCE)});
else if (args.includes('config')) process.stdout.write(execFileSync(${JSON.stringify(realDocker)}, args, {env: process.env}));
else if (args.includes('pull') || args.includes('up')) {}
else if (args.includes('ps') && args.includes('-q')) console.log('fixture-container');
else if (args.includes('ps')) {
  const mode = readFileSync(${JSON.stringify(scenario)}, 'utf8');
  const services = ['postgres','tenant','resource-catalog','ledger','capability','build','runtime-control','workspace','serve','fabric','control-plane','console-bff','cloud-local-v2'];
  let rows = services.map(Service => ({Service, State: mode === 'stopped' && Service === 'tenant' ? 'exited' : 'running'}));
  if (mode === 'missing') rows = rows.filter(row => row.Service !== 'control-plane');
  console.log(mode === 'array' ? JSON.stringify(rows) : rows.map(row => JSON.stringify(row)).join('\\n'));
} else if (args.includes('exec')) console.log(JSON.stringify({status:'ok'}));
else { console.error('unexpected docker command'); process.exitCode = 1; }
`;
    await writeFile(join(bin, "docker"), mock, { mode: 0o755 });
    for (const mode of ["ndjson", "array", "missing", "stopped"]) {
      await writeFile(scenario, mode); await writeFile(log, "");
      const result = run(bundle, installDir, [], { PATH: `${bin}:${process.env.PATH}` });
      const calls: string[][] = (await readFile(log, "utf8")).trim().split("\n").map(line => JSON.parse(line));
      assert.ok(calls.some(args => args.includes("pull")));
      assert.ok(calls.some(args => args.includes("up")));
      if (mode === "missing" || mode === "stopped") {
        assert.notEqual(result.status, 0, mode);
        assert.match(result.stderr, /cloud_services_not_running/);
        assert.ok(!calls.some(args => args.includes("inspect") || args.includes("exec")), "incomplete startup must not be accepted by later owner probes");
      } else {
        assert.equal(result.status, 0, `${mode}:${result.stdout}${result.stderr}`);
        assert.equal(calls.filter(args => args.includes("exec")).length, 11);
        assert.equal(calls.filter(args => args[0] === "inspect").length, 11);
        assert.match(result.stdout, /OPL Cloud is running/);
      }
    }
  });
});
