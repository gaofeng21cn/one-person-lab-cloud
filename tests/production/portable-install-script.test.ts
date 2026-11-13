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
function run(bundle: string, installDir: string, args: string[] = []) {
  return spawnSync("bash", [join(bundle, "install.sh"), "--install-dir", installDir, ...args], { cwd: tmpdir(), encoding: "utf8", env: { ...process.env, OPL_CLOUD_IMAGE: "ghcr.io/evil/wrong:latest", COMPOSE_PROJECT_NAME: "evil" } });
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
    assert.equal(state.values.OPL_LOCAL_V2_PACKAGE_SCHEMA_PATH, join(installDir, "schemas", "agent-plugins-1.0.0-plugin.schema.json"));
    assert.equal(sha256Of(await readFile(state.values.OPL_LOCAL_V2_PACKAGE_SCHEMA_PATH)), "0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883");
  });
});

test("installation state retains operator facts while updates replace only immutable bundle facts", async () => {
  await withBundle(async (bundle, root) => {
    const installDir = join(root, "install");
    const first = run(bundle, installDir, ["--dry-run"]); assert.equal(first.status, 2);
    const path = join(installDir, "installation.json"); const state = JSON.parse(await readFile(path, "utf8"));
    state.values.OPL_WORKSPACE_DOMAIN = "ws.example.test";
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
