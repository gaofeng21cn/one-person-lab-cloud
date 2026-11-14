import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import {
  PORTABLE_ASSETS,
  distributionContract,
} from "../../tools/cloud-candidate-receipt.ts";

const COMPOSE = "deploy/portable/compose.cloud.yaml";
const QUALIFICATION_COMPOSE = "compose.local-v2.yaml";
const digest = `ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:${"a".repeat(64)}`;

// The published topology is the complete owner topology of one installation.
// `sub2api-authority` is intentionally absent: it is the qualification
// fixture, and a production installation talks to the operator's own HTTPS
// Sub2API through OPL_SUB2API_BASE_URL.
const expectedServices = [
  "build",
  "capability",
  "cloud-local-v2",
  "console-bff",
  "control-plane",
  "fabric",
  "ledger",
  "postgres",
  "resource-catalog",
  "runtime-control",
  "serve",
  "tenant",
  "workspace",
];

// The installation surface names are the published ones. They are not
// renamed by this change: the volumes, the network and the Postgres
// initialization config keep their names.
const expectedVolumes = [
  "opl-cloud-local-v2-build",
  "opl-cloud-local-v2-fabric",
  "opl-cloud-local-v2-objects",
  "opl-cloud-local-v2-postgres",
];
const expectedNetworks = ["opl-cloud-local-v2"];
const expectedConfigs = ["opl-local-v2-postgres-init"];

// Every environment variable name each service carries. Names only, never
// values: a dropped variable fails this table, and the values are the
// installation's own facts.
const expectedEnvironment: Record<string, string[]> = {
  postgres: [
    "OPL_BUILD_DATABASE_PASSWORD",
    "OPL_CAPABILITY_DATABASE_PASSWORD",
    "OPL_CONTROL_PLANE_DATABASE_PASSWORD",
    "OPL_FABRIC_DATABASE_PASSWORD",
    "OPL_GATEWAY_DATABASE_PASSWORD",
    "OPL_LEDGER_DATABASE_PASSWORD",
    "OPL_RESOURCE_CATALOG_DATABASE_PASSWORD",
    "OPL_RUNTIME_CONTROL_DATABASE_PASSWORD",
    "OPL_SERVE_DATABASE_PASSWORD",
    "OPL_TENANT_DATABASE_PASSWORD",
    "OPL_WORKSPACE_DATABASE_PASSWORD",
    "POSTGRES_DB",
    "POSTGRES_PASSWORD",
    "POSTGRES_USER",
  ],
  tenant: [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_BUILD_ADDR",
    "OPL_BUILD_TOKEN",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_FABRIC_ADDR",
    "OPL_FABRIC_TOKEN",
    "OPL_GATEWAY_DATABASE_URL",
    "OPL_GATEWAY_DIRECTORY_EMAIL",
    "OPL_GATEWAY_DIRECTORY_PASSWORD",
    "OPL_GATEWAY_WORKSPACE_KEY_GROUP",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_INVITATION_TTL",
    "OPL_PLATFORM_ADMIN_SUBJECTS",
    "OPL_SERVE_ADDR",
    "OPL_SERVE_TOKEN",
    "OPL_SESSION_SIGNING_KEY",
    "OPL_SUB2API_URL",
    "OPL_TENANT_ADDR",
    "OPL_TENANT_PEER_TOKENS",
    "OPL_WORKSPACE_ADDR",
    "OPL_WORKSPACE_REGISTRY_HOST",
    "OPL_WORKSPACE_REGISTRY_NAMESPACE",
    "OPL_WORKSPACE_TOKEN",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  "resource-catalog": [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_TOKEN",
    "OPL_RESOURCE_CATALOG_ADDR",
    "OPL_RESOURCE_CATALOG_PEER_TOKENS",
    "OPL_RESOURCE_CATALOG_PROFILE_JSON",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  ledger: [
    "DATABASE_URL",
    "LEDGER_ADDR",
    "NODE_ENV",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_GATEWAY_ADDR",
    "OPL_GATEWAY_TOKEN",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_INTERNAL_SERVICE_TOKEN",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_CAPABILITY_KEY",
    "OPL_LEDGER_PEER_TOKENS",
    "OPL_RESOURCE_CATALOG_ADDR",
    "OPL_RESOURCE_CATALOG_TOKEN",
    "OPL_WORKSPACE_ADDR",
    "OPL_WORKSPACE_TOKEN",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  capability: [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_BUILD_ADDR",
    "OPL_BUILD_TOKEN",
    "OPL_CAPABILITY_ADDR",
    "OPL_CAPABILITY_OBJECT_LISTEN_ADDR",
    "OPL_CAPABILITY_OBJECT_ROOT",
    "OPL_CAPABILITY_OBJECT_SIGNING_KEY",
    "OPL_CAPABILITY_OBJECT_TOKEN",
    "OPL_CAPABILITY_OBJECT_URL",
    "OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST",
    "OPL_CAPABILITY_PACKAGE_SCHEMA_PATH",
    "OPL_CAPABILITY_PEER_TOKENS",
    "OPL_CAPABILITY_STORAGE_KIND",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_TOKEN",
    "OPL_PUBLISHER_SCHEMA_DIGEST",
    "OPL_PUBLISHER_SCHEMA_PATH",
    "OPL_RUNTIME_CONTROL_ADDR",
    "OPL_RUNTIME_CONTROL_TOKEN",
    "OPL_SERVE_ADDR",
    "OPL_SERVE_TOKEN",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  build: [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_BUILD_ADDR",
    "OPL_BUILD_BUILDER",
    "OPL_BUILD_DOCKER_CONFIG",
    "OPL_BUILD_PEER_TOKENS",
    "OPL_BUILD_REGISTRY_PREFIX",
    "OPL_BUILD_REGISTRY_TOKEN",
    "OPL_BUILD_WORKDIR",
    "OPL_CAPABILITY_ADDR",
    "OPL_CAPABILITY_OBJECT_TOKEN",
    "OPL_CAPABILITY_OBJECT_URL",
    "OPL_CAPABILITY_TOKEN",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_TOKEN",
    "OPL_TENANT_TOKEN",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  "runtime-control": [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_CAPABILITY_ADDR",
    "OPL_CAPABILITY_TOKEN",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_RUNTIME_CONTROL_ADDR",
    "OPL_RUNTIME_CONTROL_PEER_TOKENS",
    "OPL_RUNTIME_PUBLISHER_SCHEMA_DIGEST",
    "OPL_RUNTIME_PUBLISHER_SCHEMA_PATH",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  workspace: [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_CAPABILITY_ADDR",
    "OPL_CAPABILITY_TOKEN",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_CONTROL_PLANE_URL",
    "OPL_CONTROL_PLANE_WORKSPACE_TOKEN",
    "OPL_FABRIC_ADDR",
    "OPL_FABRIC_TOKEN",
    "OPL_GATEWAY_ADDR",
    "OPL_GATEWAY_TOKEN",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_TOKEN",
    "OPL_RESOURCE_CATALOG_ADDR",
    "OPL_RESOURCE_CATALOG_TOKEN",
    "OPL_RUNTIME_CONTROL_ADDR",
    "OPL_RUNTIME_CONTROL_TOKEN",
    "OPL_SERVE_ADDR",
    "OPL_SERVE_TOKEN",
    "OPL_WORKSPACE_ADDR",
    "OPL_WORKSPACE_PEER_TOKENS",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  serve: [
    "DATABASE_URL",
    "NODE_ENV",
    "OPL_AIONUI_ADMIN_PASSWORD_SEED",
    "OPL_CAPABILITY_ADDR",
    "OPL_CAPABILITY_TOKEN",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_FABRIC_APPLICATION_URL",
    "OPL_FABRIC_COORDINATION_ADDR",
    "OPL_FABRIC_COORDINATION_TOKEN",
    "OPL_FABRIC_SERVE_CAPABILITY_KEY",
    "OPL_FABRIC_SERVE_SERVICE_TOKEN",
    "OPL_GATEWAY_ADDR",
    "OPL_GATEWAY_TOKEN",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_K8S_NAMESPACE",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_TOKEN",
    "OPL_PUBLIC_URL",
    "OPL_RUNTIME_CONTROL_ADDR",
    "OPL_RUNTIME_CONTROL_TOKEN",
    "OPL_SERVE_ACCESS_ADDR",
    "OPL_SERVE_ADDR",
    "OPL_SERVE_PEER_TOKENS",
    "OPL_WORKSPACE_ADDR",
    "OPL_WORKSPACE_APPLICATION_DOMAIN",
    "OPL_WORKSPACE_APPLICATION_INGRESS_PEERS",
    "OPL_WORKSPACE_DOMAIN",
    "OPL_WORKSPACE_TOKEN",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  fabric: [
    "DATABASE_URL",
    "FABRIC_ADDR",
    "NODE_ENV",
    "OPL_AIONUI_ADMIN_PASSWORD_SEED",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_CLOUD_IMAGE",
    "OPL_FABRIC_ADDR",
    "OPL_FABRIC_CAPABILITY_KEY",
    "OPL_FABRIC_DATABASE_URL",
    "OPL_FABRIC_LOCAL_DOCKER_ALLOW_UNBOUNDED_SWAP",
    "OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER",
    "OPL_FABRIC_LOCAL_DOCKER_HOST",
    "OPL_FABRIC_LOCAL_DOCKER_PROBE_IMAGE",
    "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON",
    "OPL_FABRIC_LOCAL_DOCKER_PUBLISH_HOST",
    "OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT",
    "OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT",
    "OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES",
    "OPL_FABRIC_PEER_TOKENS",
    "OPL_FABRIC_PROVIDER",
    "OPL_FABRIC_RUNNER_SERVICE_TOKEN",
    "OPL_FABRIC_SERVE_CAPABILITY_KEY",
    "OPL_FABRIC_SERVE_SERVICE_TOKEN",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_INTERNAL_SERVICE_TOKEN",
    "OPL_LEDGER_ADDR",
    "OPL_LEDGER_TOKEN",
    "OPL_RESOURCE_CATALOG_ADDR",
    "OPL_RESOURCE_CATALOG_TOKEN",
    "OPL_WORKSPACE_DOMAIN",
    "OPL_WORKSPACE_IMAGE",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  "control-plane": [
    "CONTROL_PLANE_ADDR",
    "DATABASE_URL",
    "FABRIC_URL",
    "LEDGER_URL",
    "NODE_ENV",
    "OPL_AIONUI_ADMIN_PASSWORD_SEED",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_CLOUD_IMAGE",
    "OPL_CONSOLE_DIST_DIR",
    "OPL_CONTROL_PLANE_BFF_TOKEN",
    "OPL_CONTROL_PLANE_WORKSPACE_TOKEN",
    "OPL_DEPLOYMENT_MODE",
    "OPL_FABRIC_CAPABILITY_KEY",
    "OPL_FABRIC_PROVIDER",
    "OPL_FABRIC_SERVICE_TOKEN",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_INTERNAL_SERVICE_TOKEN",
    "OPL_LEDGER_CAPABILITY_KEY",
    "OPL_LEDGER_SERVICE_TOKEN",
    "OPL_PUBLIC_URL",
    "OPL_RELEASE_SHA",
    "OPL_SUB2API_BASE_URL",
    "OPL_SUB2API_REQUEST_TIMEOUT_MS",
    "OPL_SUB2API_USER_EMAIL",
    "OPL_SUB2API_USER_PASSWORD",
    "OPL_WORKSPACE_APPLICATION_DOMAIN",
    "OPL_WORKSPACE_DOMAIN",
    "OPL_WORKSPACE_IMAGE",
    "PGSSLMODE",
    "PORT",
    "SSL_CERT_FILE",
  ],
  "console-bff": [
    "NODE_ENV",
    "OPL_BFF_ADDR",
    "OPL_BUILD_TOKEN",
    "OPL_BUILD_URL",
    "OPL_CAPABILITY_TOKEN",
    "OPL_CAPABILITY_URL",
    "OPL_CLOUD_IDENTITY_TOKEN",
    "OPL_CLOUD_IDENTITY_URL",
    "OPL_CONTROL_PLANE_TOKEN",
    "OPL_CONTROL_PLANE_URL",
    "OPL_FABRIC_TOKEN",
    "OPL_FABRIC_URL",
    "OPL_GATEWAY_TOKEN",
    "OPL_GATEWAY_URL",
    "OPL_GRPC_CA_FILE",
    "OPL_GRPC_CERT_FILE",
    "OPL_GRPC_KEY_FILE",
    "OPL_LEDGER_TOKEN",
    "OPL_LEDGER_URL",
    "OPL_PUBLIC_URL",
    "OPL_RESOURCE_CATALOG_TOKEN",
    "OPL_RESOURCE_CATALOG_URL",
    "OPL_RUNTIME_CONTROL_TOKEN",
    "OPL_RUNTIME_CONTROL_URL",
    "OPL_SERVE_TOKEN",
    "OPL_SERVE_URL",
    "OPL_WORKSPACE_TOKEN",
    "OPL_WORKSPACE_URL",
    "PGSSLMODE",
    "SSL_CERT_FILE",
  ],
  "cloud-local-v2": [
    "NGINX_ENVSUBST_FILTER",
    "OPL_WORKSPACE_APPLICATION_DOMAIN",
    "OPL_WORKSPACE_DOMAIN",
  ],
};

test("the published compose declares every service itself and inherits nothing", async () => {
  const source = await readFile(COMPOSE, "utf8");
  assert.equal(
    /^\s*extends\s*:/m.test(source),
    false,
    "the published compose must not inherit a service from another file",
  );
  assert.equal(
    source.includes(QUALIFICATION_COMPOSE),
    false,
    `the published compose must not reference ${QUALIFICATION_COMPOSE}`,
  );
  // The overlay tags only existed to override the inherited definition. The
  // published file needs neither, so it needs no Compose newer than 2.24.0.
  assert.equal(
    /!override|!reset/.test(source),
    false,
    "the published compose must not use the Compose 2.24.0 overlay tags",
  );
});

test("the release bundle no longer ships the qualification topology", () => {
  const candidateAssets =
    distributionContract.bundleFormats.formats["portable-v1"].candidateAssets;
  assert.equal(
    Array.isArray(candidateAssets),
    true,
    "portable-v1.candidateAssets must stay an explicit list",
  );
  assert.equal(
    candidateAssets.includes(QUALIFICATION_COMPOSE),
    false,
    `portable-v1.candidateAssets must not carry ${QUALIFICATION_COMPOSE}`,
  );
  assert.equal(
    PORTABLE_ASSETS.includes(QUALIFICATION_COMPOSE),
    false,
    `the derived portable asset list must not carry ${QUALIFICATION_COMPOSE}`,
  );
  assert.equal(
    PORTABLE_ASSETS.includes("compose.cloud.yaml"),
    true,
    "the derived portable asset list must still carry the published compose",
  );
});

test("the installer does not read the qualification topology out of the bundle", async () => {
  const installer = await readFile("deploy/portable/install.mjs", "utf8");
  assert.equal(
    installer.includes(QUALIFICATION_COMPOSE),
    false,
    `install.mjs must not read ${QUALIFICATION_COMPOSE} out of the bundle`,
  );
});

test("the published compose resolves to the complete owner topology", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-cloud-self-contained-"));
  try {
    const declared = spawnSync(
      "docker",
      [
        "compose",
        "--env-file",
        "/dev/null",
        "-p",
        "opl-cloud",
        "-f",
        COMPOSE,
        "config",
        "--variables",
        "--format",
        "json",
      ],
      { encoding: "utf8" },
    );
    assert.equal(declared.status, 0, declared.stderr);
    const variables = JSON.parse(declared.stdout);
    const values: Record<string, string> = {};
    for (const item of Object.values(variables) as any[]) {
      if (!item.Required && item.DefaultValue) continue;
      values[item.Name] = "x".repeat(64);
    }
    values.OPL_CLOUD_IMAGE = digest;
    values.OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES = digest;
    values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON = JSON.stringify({
      schemaVersion: 1,
    });
    values.OPL_RESOURCE_CATALOG_PROFILE_JSON = JSON.stringify({
      provider: "local-docker",
    });
    values.OPL_WORKSPACE_DOMAIN = "ws.example.test";
    values.OPL_SUB2API_URL = "https://sub2api.example.test";
    values.OPL_SUB2API_BASE_URL = "https://sub2api.example.test";
    values.OPL_SUB2API_ADMIN_EMAIL = "admin@example.test";
    values.OPL_SUB2API_USER_EMAIL = "user@example.test";
    values.OPL_WORKSPACE_IMAGE = `ghcr.io/example/workspace@sha256:${"b".repeat(64)}`;
    values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH = join(
      root,
      "plugin.schema.json",
    );
    values.OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST = `sha256:${"c".repeat(64)}`;
    values.OPL_PUBLISHER_SCHEMA_DIGEST = `sha256:${"d".repeat(64)}`;
    values.OPL_WORKSPACE_REGISTRY_HOST = "registry.example.test";
    values.OPL_WORKSPACE_REGISTRY_NAMESPACE = "opl";
    values.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT = join(root, "storage");
    values.OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT = join(root, "secrets");
    values.OPL_PKI_DIR = root;
    await writeFile(values.OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH, "{}\n");
    await writeFile(
      join(root, ".env"),
      Object.entries(values)
        .map(([name, value]) => `${name}=${value}`)
        .join("\n"),
    );
    const result = spawnSync(
      "docker",
      [
        "compose",
        "--env-file",
        join(root, ".env"),
        "-p",
        "opl-cloud",
        "-f",
        COMPOSE,
        "config",
        "--format",
        "json",
      ],
      { encoding: "utf8" },
    );
    assert.equal(result.status, 0, result.stderr);
    const model = JSON.parse(result.stdout);
    assert.deepEqual(Object.keys(model.services).sort(), expectedServices);
    assert.equal(model.services["sub2api-authority"], undefined);
    assert.deepEqual(Object.keys(model.volumes).sort(), expectedVolumes);
    assert.deepEqual(Object.keys(model.networks).sort(), expectedNetworks);
    assert.deepEqual(Object.keys(model.configs).sort(), expectedConfigs);
    for (const name of expectedServices) {
      assert.deepEqual(
        Object.keys(model.services[name].environment).sort(),
        expectedEnvironment[name],
        `environment of ${name}`,
      );
    }
    // The published product declares no qualification fact. The qualification
    // authority is a fixture, and the installation supplies its own Sub2API.
    const qualificationFacts = Object.keys(model.services)
      .flatMap((name: string) =>
        Object.keys(model.services[name].environment).filter((key: string) =>
          key.startsWith("OPL_QUALIFICATION_"),
        ),
      );
    assert.deepEqual(qualificationFacts, []);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
