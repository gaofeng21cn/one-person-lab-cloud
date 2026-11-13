import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const digest = `ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:${"a".repeat(64)}`;
const names = ["tenant", "resource-catalog", "ledger", "capability", "build", "runtime-control", "workspace", "serve", "fabric", "control-plane", "console-bff", "cloud-local-v2", "postgres"];

test("portable Cloud Compose is the complete owner topology without qualification fixtures", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-cloud-topology-"));
  try {
    const vars = JSON.parse(spawnSync("docker", ["compose", "--env-file", "/dev/null", "-p", "opl-cloud", "-f", "deploy/portable/compose.cloud.yaml", "config", "--variables", "--format", "json"], { encoding: "utf8" }).stdout);
    const values: Record<string, string> = {};
    for (const item of Object.values(vars) as any[]) {
      if (!item.Required && item.DefaultValue) continue;
      values[item.Name] = "x".repeat(64);
    }
    values.OPL_CLOUD_IMAGE = digest;
    values.OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES = digest;
    values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON = JSON.stringify({ schemaVersion: 1 });
    values.OPL_RESOURCE_CATALOG_PROFILE_JSON = JSON.stringify({ provider: "local-docker" });
    values.OPL_WORKSPACE_DOMAIN = "ws.example.test";
    values.OPL_QUALIFICATION_AUTHORITY_TOKEN = "x".repeat(64);
    values.OPL_QUALIFICATION_USER_EMAIL = "user@example.test";
    values.OPL_QUALIFICATION_USER_PASSWORD = "p".repeat(64);
    values.OPL_QUALIFICATION_USER_TOKEN = "t".repeat(64);
    values.OPL_SUB2API_URL = "https://sub2api.example.test";
    values.OPL_SUB2API_BASE_URL = "https://sub2api.example.test";
    values.OPL_SUB2API_ADMIN_EMAIL = "admin@example.test";
    values.OPL_SUB2API_USER_EMAIL = "user@example.test";
    values.OPL_WORKSPACE_IMAGE = `ghcr.io/example/workspace@sha256:${"b".repeat(64)}`;
    values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH = join(root, "plugin.schema.json");
    values.OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST = `sha256:${"c".repeat(64)}`;
    values.OPL_PUBLISHER_SCHEMA_DIGEST = `sha256:${"d".repeat(64)}`;
    values.OPL_WORKSPACE_REGISTRY_HOST = "registry.example.test";
    values.OPL_WORKSPACE_REGISTRY_NAMESPACE = "opl";
    values.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT = join(root, "storage");
    values.OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT = join(root, "secrets");
    values.OPL_PKI_DIR = root;
    await writeFile(values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH, "{}\n");
    await writeFile(join(root, ".env"), Object.entries(values).map(([k, v]) => `${k}=${v}`).join("\n"));
    const result = spawnSync("docker", ["compose", "--env-file", join(root, ".env"), "-p", "opl-cloud", "-f", "deploy/portable/compose.cloud.yaml", "config", "--format", "json"], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    const model = JSON.parse(result.stdout);
    assert.deepEqual(Object.keys(model.services).sort(), names.sort());
    assert.equal(model.services["sub2api-authority"], undefined);
    for (const name of names.filter(name => name !== "postgres" && name !== "cloud-local-v2")) assert.equal(model.services[name].image, digest, name);
  } finally { await rm(root, { recursive: true, force: true }); }
});
