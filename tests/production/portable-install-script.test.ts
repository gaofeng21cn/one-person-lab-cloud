import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { access, chmod, copyFile, mkdir, mkdtemp, readFile, readdir, symlink, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
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
    // The install path now requires the Sub2API authority to be reachable,
    // so the fixture points at a local stub instead of an unresolvable name.
    const authority = await startSub2apiStub();
    Object.assign(state.values, {
      OPL_WORKSPACE_DOMAIN: "ws.example.test", OPL_CAPABILITY_OBJECT_URL: "https://objects.example.test",
      OPL_SUB2API_URL: authority.url, OPL_SUB2API_BASE_URL: authority.url,
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
if (args[0] === 'compose' && args[1] === 'version') console.log('Docker Compose version v2.24.4');
else if (args[0] === 'inspect') console.log(${JSON.stringify(IMAGE_REFERENCE)});
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
    await authority.close();
  });
});

// The installation facts a second run must carry before the installer will
// pull anything. Only OPL_SUB2API_BASE_URL changes between the cases below.
const INSTALLATION_FACTS: Record<string, string> = {
  OPL_WORKSPACE_DOMAIN: "ws.example.test",
  OPL_CAPABILITY_OBJECT_URL: "https://objects.example.test",
  OPL_SUB2API_URL: "https://sub2api.example.test",
  OPL_SUB2API_BASE_URL: "https://sub2api.example.test",
  OPL_SUB2API_ADMIN_EMAIL: "admin@example.test",
  OPL_SUB2API_USER_EMAIL: "user@example.test",
  OPL_WORKSPACE_IMAGE: `ghcr.io/example/workspace@sha256:${"d".repeat(64)}`,
  OPL_WORKSPACE_REGISTRY_HOST: "registry.example.test",
  OPL_WORKSPACE_REGISTRY_NAMESPACE: "opl",
  OPL_GATEWAY_WORKSPACE_KEY_GROUP: "default",
  OPL_PLATFORM_ADMIN_SUBJECTS: "admin@example.test",
  OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: JSON.stringify({ schemaVersion: 1 }),
  OPL_RESOURCE_CATALOG_PROFILE_JSON: JSON.stringify({ provider: "local-docker" }),
};
const COMPOSE_SERVICES = ["postgres", "tenant", "resource-catalog", "ledger", "capability", "build", "runtime-control", "workspace", "serve", "fabric", "control-plane", "console-bff", "cloud-local-v2"];

// A Docker stub that reports one Compose version and delegates every Compose
// parse to the real binary, so the merge tags are still parsed for real.
// With "oldParser" the stub behaves like the Docker Compose release that
// knows !reset but not !override: it has no "config --variables" command,
// and it cannot merge the published compose file's merge tags.
async function writeDockerMock(root: string, options: { composeVersion?: string; pluginMissing?: boolean; oldParser?: boolean } = {}) {
  const bin = join(root, "mock-bin");
  await mkdir(bin, { recursive: true });
  const log = join(root, "docker-calls.jsonl");
  const realDocker = spawnSync("which", ["docker"], { encoding: "utf8" }).stdout.trim();
  assert.ok(realDocker, "the real docker binary parses the bundle's Compose file");
  const versionReply = options.pluginMissing
    ? `console.error("docker: 'compose' is not a docker command."); process.exitCode = 1;`
    : `console.log(${JSON.stringify(options.composeVersion ?? "Docker Compose version v2.24.4")});`;
  const mock = `#!${process.execPath}
const { appendFileSync, readFileSync } = require('node:fs');
const { execFileSync } = require('node:child_process');
const args = process.argv.slice(2);
appendFileSync(${JSON.stringify(log)}, JSON.stringify(args) + '\\n');
const isConfig = args.includes('config');
const isVariables = args.includes('--variables');
const isParse = isConfig && args.includes('--format');
const oldParser = ${options.oldParser ? "true" : "false"};
if (args[0] === 'compose' && args[1] === 'version') { ${versionReply} }
else if (oldParser && isVariables) { console.error('unknown flag: --variables'); process.exitCode = 16; }
else if (oldParser && isParse && /!(override|reset)/.test(readFileSync('compose.cloud.yaml', 'utf8'))) {
  console.error('cannot override services.x.networks.opl-cloud-local-v2');
  process.exitCode = 15;
}
else if (args[0] === 'inspect') console.log(${JSON.stringify(IMAGE_REFERENCE)});
else if (isConfig) process.stdout.write(execFileSync(${JSON.stringify(realDocker)}, args, {env: process.env}));
else if (args.includes('pull') || args.includes('up')) {}
else if (args.includes('ps') && args.includes('-q')) console.log('fixture-container');
else if (args.includes('ps')) console.log(${JSON.stringify(COMPOSE_SERVICES)}.map(Service => JSON.stringify({Service, State: 'running'})).join('\\n'));
else if (args.includes('exec')) console.log(JSON.stringify({status:'ok'}));
else { console.error('unexpected docker command'); process.exitCode = 1; }
`;
  await writeFile(join(bin, "docker"), mock, { mode: 0o755 });
  await writeFile(log, "");
  return {
    bin,
    log,
    calls: async () =>
      (await readFile(log, "utf8")).split("\n").filter(Boolean).map(line => JSON.parse(line) as string[]),
  };
}

// A PATH that holds every executable the host offers except Docker itself.
async function pathWithoutDocker(root: string) {
  const bin = join(root, "no-docker-bin");
  await mkdir(bin, { recursive: true });
  const linked = new Set<string>();
  for (const dir of (process.env.PATH || "").split(":").filter(Boolean)) {
    let entries: string[] = [];
    try { entries = await readdir(dir); } catch { continue; }
    for (const entry of entries) {
      if (entry === "docker" || entry === "docker-compose" || linked.has(entry)) continue;
      linked.add(entry);
      try { await symlink(join(dir, entry), join(bin, entry)); } catch {}
    }
  }
  try { await symlink(process.execPath, join(bin, "node")); } catch {}
  return bin;
}
// The PATH a Docker stub test owns completely. A stub that only prepends its
// own directory leaves the real Docker reachable behind it, which is how a
// test passed on one machine and failed in CI.
async function controlledPath(root: string, mock?: { bin: string }) {
  const bin = await pathWithoutDocker(root);
  if (mock) {
    await copyFile(join(mock.bin, "docker"), join(bin, "docker"));
    await chmod(join(bin, "docker"), 0o755);
  }
  return bin;
}
// The bundle is checksum-bound, so replacing an asset means re-signing the
// bundle the way the publisher signs it.
async function replaceBundleAsset(bundle: string, name: string, bytes: Buffer) {
  await writeFile(join(bundle, name), bytes);
  const assets = [];
  for (const asset of PORTABLE_ASSETS) {
    assets.push({ name: asset, sha256: sha256Of(await readFile(join(bundle, asset))) });
  }
  await writeFile(join(bundle, "opl-cloud-candidate.json"), `${canonicalJson(candidateReceipt(assets))}\n`);
  const sumLines = [];
  for (const asset of [...PORTABLE_ASSETS, "opl-cloud-candidate.json"]) {
    sumLines.push(`${sha256Of(await readFile(join(bundle, asset)))}  ${asset}`);
  }
  await writeFile(join(bundle, "SHA256SUMS"), `${sumLines.join("\n")}\n`);
}
// The published compose file of a self-contained bundle: no extends, no
// !override, no !reset. It declares the same services, so the installation
// reaches the same places. The only difference is the syntax the installed
// Docker Compose has to parse.
function selfContainedCompose() {
  return [
    "# A self-contained compose file: no extends, no merge tags.",
    "services:",
    ...COMPOSE_SERVICES.map(name => `  ${name}:\n    image: alpine:3`),
    "",
  ].join("\n");
}
// The installer's PKI generation runs shell commands that a test's controlled
// PATH does not carry. These tests are about the Compose precondition, so the
// PKI is placed where the installer expects to find it.
async function writePkiFixture(installDir: string) {
  const pki = join(installDir, "pki");
  await mkdir(pki, { recursive: true });
  const names = [
    "ca.crt", "ca.key", "proxy.crt", "proxy.key",
    ...["tenant", "capability", "build", "runtime_control", "workspace", "serve", "resource_catalog", "ledger", "fabric", "console_bff", "control_plane"]
      .flatMap(name => [`${name}.crt`, `${name}.key`]),
  ];
  for (const name of names) await writeFile(join(pki, name), `fixture-${name}`);
}
// A Sub2API authority stub that accepts a TCP connection and speaks nothing.
async function startSub2apiStub() {
  const server = createServer((socket) => socket.end());
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  return {
    url: `http://127.0.0.1:${port}`,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}
// A port that nothing listens on, so the authority refuses the connection.
async function closedPort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  await new Promise<void>((resolve) => server.close(() => resolve()));
  return port;
}
async function fillInstallationFacts(bundle: string, root: string, env: NodeJS.ProcessEnv, sub2apiUrl: string) {
  const installDir = join(root, "install");
  const first = run(bundle, installDir, ["--dry-run"], env);
  assert.equal(first.status, 2, `${first.stdout}${first.stderr}`);
  const statePath = join(installDir, "installation.json");
  const state = JSON.parse(await readFile(statePath, "utf8"));
  Object.assign(state.values, INSTALLATION_FACTS, { OPL_SUB2API_BASE_URL: sub2apiUrl, OPL_SUB2API_URL: sub2apiUrl });
  state.values.OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES = state.values.OPL_WORKSPACE_IMAGE;
  await writeFile(statePath, JSON.stringify(state));
  return { installDir, statePath, state };
}

test("installer refuses a Docker Compose that cannot parse the published compose file", async () => {
  await withBundle(async (bundle, root) => {
    // The Docker Compose release that knows !reset but not !override. It
    // cannot merge the published compose file, so the installer refuses it.
    const mock = await writeDockerMock(root, { composeVersion: "Docker Compose version v2.24.3", oldParser: true });
    const env = { PATH: await controlledPath(root, mock) };
    // The URL carries credentials in its userinfo, a path and a query. The
    // refusal may report the host and nothing else.
    const authorityUrl = "https://leaked-user:leaked-password-token@127.0.0.1:9/base-path?token=leaked-query-token";
    const { installDir, statePath, state } = await fillInstallationFacts(bundle, root, env, authorityUrl);
    const result = run(bundle, installDir, [], env);
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);
    const reported = `${result.stdout}${result.stderr}`;
    assert.match(result.stderr, /docker_compose_parse_failed/);
    assert.match(result.stderr, /2\.24\.3/, "the refusal names the version the host has");
    assert.match(result.stderr, /cannot override services\.x\.networks\.opl-cloud-local-v2/, "the refusal carries the parser's own complaint");
    assert.match(result.stderr, /!override/, "the refusal names the merge tag this Compose does not apply");
    assert.match(result.stderr, /https:\/\/docs\.docker\.com\/compose\/install\//, "the refusal says how to upgrade");
    assert.ok(!reported.includes("docker_compose_version_too_old"), "no version floor is reported");
    assert.ok(!reported.includes("2.24.4"), "no required version is reported");
    assert.ok(!reported.includes("leaked-password-token"), "the URL's password is never reported");
    assert.ok(!reported.includes("leaked-user"), "the URL's user is never reported");
    assert.ok(!reported.includes("leaked-query-token"), "the URL's query is never reported");
    assert.ok(!reported.includes("/base-path"), "the URL's path is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_ADMIN_PASSWORD), "the administrator password is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_USER_PASSWORD), "the user password is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_ADMIN_EMAIL), "the administrator email is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_USER_EMAIL), "the user email is never reported");
    assert.ok(!reported.includes("OPL Cloud is running"), "a refused installation is not reported as a successful install");
    assert.ok(!(await mock.calls()).some(args => args.includes("pull") || args.includes("up")), "no image is pulled before the compose file is parsed");
    // A refusal happens before the installation generates its PKI, records
    // the version it checked against or pulls anything.
    await assert.rejects(access(join(installDir, "pki")), "the refusal leaves no PKI behind");
    await assert.rejects(access(join(installDir, "data", "postgres")), "the refusal leaves no data directory behind");
    const refused = JSON.parse(await readFile(statePath, "utf8"));
    assert.equal(refused.composeVersion, undefined, "the refused installation records no checked-against version");
  });
});

test("installer accepts a Docker Compose that can parse the published compose file", async () => {
  await withBundle(async (bundle, root) => {
    const mock = await writeDockerMock(root, { composeVersion: "Docker Compose version v5.1.1" });
    const env = { PATH: await controlledPath(root, mock) };
    const authority = await startSub2apiStub();
    try {
      const { installDir, statePath } = await fillInstallationFacts(bundle, root, env, authority.url);
      await writePkiFixture(installDir);
      const result = run(bundle, installDir, [], env);
      assert.equal(result.status, 0, `${result.stdout}${result.stderr}`);
      assert.match(result.stdout, /Docker Compose 5\.1\.1 parsed the published compose file/);
      assert.match(result.stdout, /OPL Cloud is running/);
      assert.ok((await mock.calls()).some(args => args.includes("pull")), "the installation pulls after the compose file is parsed");
      const state = JSON.parse(await readFile(statePath, "utf8"));
      assert.equal(state.composeVersion, "5.1.1", "the installation records the version it was checked against");
    } finally { await authority.close(); }
  });
});

test("the same Docker Compose is accepted when the published compose file uses no merge tags", async () => {
  await withBundle(async (bundle, root) => {
    // The same old Docker Compose, the same installer, and a published
    // compose file that carries no merge tag. Nothing but the file changed,
    // so the refusal follows the file and not a version number.
    const mock = await writeDockerMock(root, { composeVersion: "Docker Compose version v2.24.3", oldParser: true });
    const env = { PATH: await controlledPath(root, mock) };
    await replaceBundleAsset(bundle, "compose.cloud.yaml", Buffer.from(selfContainedCompose(), "utf8"));
    const authority = await startSub2apiStub();
    try {
      const { installDir } = await fillInstallationFacts(bundle, root, env, authority.url);
      await writePkiFixture(installDir);
      const result = run(bundle, installDir, [], env);
      assert.equal(result.status, 0, `${result.stdout}${result.stderr}`);
      assert.ok(!(result.stderr || "").includes("docker_compose_parse_failed"), "a compose file without merge tags is not refused");
      assert.match(result.stdout, /OPL Cloud is running/);
    } finally { await authority.close(); }
  });
});

test("installer reports a missing Docker CLI and a missing Compose plugin separately", async () => {
  await withBundle(async (bundle, root) => {
    const withoutDocker = await pathWithoutDocker(root);
    const missing = run(bundle, join(root, "install"), ["--dry-run"], { PATH: withoutDocker });
    assert.equal(missing.status, 2, `${missing.stdout}${missing.stderr}`);
    assert.match(missing.stderr, /docker_command_missing/);
    assert.ok(!missing.stderr.includes("docker_compose_plugin_missing"), "a missing CLI is not reported as a missing plugin");

    const mock = await writeDockerMock(root, { pluginMissing: true });
    const missingPlugin = run(bundle, join(root, "install"), ["--dry-run"], { PATH: await controlledPath(root, mock) });
    assert.equal(missingPlugin.status, 2, `${missingPlugin.stdout}${missingPlugin.stderr}`);
    assert.match(missingPlugin.stderr, /docker_compose_plugin_missing/);
    assert.match(missingPlugin.stderr, /https:\/\/docs\.docker\.com\/compose\/install\//);
    assert.ok(!missingPlugin.stderr.includes("docker_command_missing"), "a missing plugin is not reported as a missing CLI");
  });
});

test("installer reports an unreadable Compose version instead of guessing a floor", async () => {
  await withBundle(async (bundle, root) => {
    const mock = await writeDockerMock(root, { composeVersion: "Docker Compose version" });
    const result = run(bundle, join(root, "install"), ["--dry-run"], { PATH: await controlledPath(root, mock) });
    assert.equal(result.status, 2, `${result.stdout}${result.stderr}`);
    assert.match(result.stderr, /docker_compose_version_unreadable/);
    assert.ok(!result.stderr.includes("docker_compose_version_too_old"), "an unreadable version is not reported as an old one");
    assert.ok(!result.stderr.includes("docker_compose_parse_failed"), "an unreadable version is not reported as a parse failure");
  });
});

test("installer refuses an unreachable Sub2API authority without leaking a credential", async () => {
  await withBundle(async (bundle, root) => {
    const mock = await writeDockerMock(root);
    const port = await closedPort();
    // The URL carries credentials in its userinfo, a path and a query. The
    // installer may report the host and nothing else.
    const authorityUrl = `https://leaked-user:leaked-password-token@127.0.0.1:${port}/base-path?token=leaked-query-token`;
    const env = { PATH: `${mock.bin}:${process.env.PATH}` };
    const { state } = await fillInstallationFacts(bundle, root, env, authorityUrl);
    const result = run(bundle, join(root, "install"), [], env);
    assert.notEqual(result.status, 0, `${result.stdout}${result.stderr}`);
    const reported = `${result.stdout}${result.stderr}`;
    assert.match(result.stderr, /sub2api_authority_unreachable/);
    assert.match(result.stderr, /127\.0\.0\.1/, "the report names the host to change");
    assert.match(result.stderr, /OPL_SUB2API_BASE_URL/, "the report names the variable to change");
    assert.ok(!reported.includes("leaked-password-token"), "the URL's password is never reported");
    assert.ok(!reported.includes("leaked-user"), "the URL's user is never reported");
    assert.ok(!reported.includes("leaked-query-token"), "the URL's query is never reported");
    assert.ok(!reported.includes("/base-path"), "the URL's path is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_ADMIN_PASSWORD), "the administrator password is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_USER_PASSWORD), "the user password is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_ADMIN_EMAIL), "the administrator email is never reported");
    assert.ok(!reported.includes(state.values.OPL_SUB2API_USER_EMAIL), "the user email is never reported");
    assert.ok(!(await mock.calls()).some(args => args.includes("pull") || args.includes("up")), "no image is pulled before the authority is checked");
  });
});

test("installer accepts a reachable Sub2API authority and says exactly what it proved", async () => {
  await withBundle(async (bundle, root) => {
    const mock = await writeDockerMock(root);
    const authority = await startSub2apiStub();
    try {
      const env = { PATH: `${mock.bin}:${process.env.PATH}` };
      // The authority is reachable, but its URL still carries a credential, a
      // path and a query. The receipt may name the host and nothing else.
      const authorityUrl = `https://leaked-user:leaked-password-token@127.0.0.1:${new URL(authority.url).port}/base-path?token=leaked-query-token`;
      const { state } = await fillInstallationFacts(bundle, root, env, authorityUrl);
      const result = run(bundle, join(root, "install"), [], env);
      assert.equal(result.status, 0, `${result.stdout}${result.stderr}`);
      const reported = `${result.stdout}${result.stderr}`;
      assert.ok(!reported.includes("leaked-password-token"), "the URL's password is never reported");
      assert.ok(!reported.includes("leaked-user"), "the URL's user is never reported");
      assert.ok(!reported.includes("leaked-query-token"), "the URL's query is never reported");
      assert.ok(!reported.includes("/base-path"), "the URL's path is never reported");
      assert.ok(!reported.includes(state.values.OPL_SUB2API_ADMIN_PASSWORD), "the administrator password is never reported");
      assert.ok(!reported.includes(state.values.OPL_SUB2API_USER_PASSWORD), "the user password is never reported");
      assert.ok(!reported.includes(state.values.OPL_SUB2API_ADMIN_EMAIL), "the administrator email is never reported");
      assert.ok(!reported.includes(state.values.OPL_SUB2API_USER_EMAIL), "the user email is never reported");
      assert.match(result.stdout, /Sub2API authority 127\.0\.0\.1:\d+ is reachable/);
      assert.match(result.stdout, /network reachability only; no credential was sent or verified/);
    } finally { await authority.close(); }
  });
});
