import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { copyFile, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import YAML from "yaml";

// The portable Candidate must declare and forward every runtime fact the owners
// it ships actually require, or an installation built from the published assets
// starts a provider that refuses real work. The required env names are read from
// the owning Go source, so this contract tracks those owners instead of
// restating their constants.

async function protectedResourceFacts() {
  const source = await readFile("services/internal/protectedresource/guard.go", "utf8");
  const fromEnv = source.slice(source.indexOf("func FromEnv()"), source.indexOf("func FromMap("));
  const names = [...fromEnv.matchAll(/"([A-Z][A-Z0-9_]*)"/g)].map((match) => match[1]);
  assert.ok(names.length > 0, "protectedresource.FromEnv must name the facts it reads");
  for (const name of names) {
    assert.ok(
      name.startsWith("OPL_SYSTEM_COMPUTE_") || name === "OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON",
      `${name} is outside the expected protected-resource fact set`
    );
  }
  return names;
}

// The provisioner owns startup and NodePool bootstrap requirements separately
// from the provider constructor and readiness requirements below.
async function tencentProvisionerBootstrapFacts() {
  const source = await readFile("services/fabric/cmd/opl-tencent-provisioner/main.go", "utf8");
  const names = (body) => [...body.matchAll(/"([A-Z][A-Z0-9_]*)"/g)].map((match) => match[1]);
  const requiredTencentEnv = source.match(/var requiredTencentEnv = \[\]string\{([\s\S]*?)\n\}/);
  const nodePoolInputs = source.match(/func buildCreateNativeNodePoolRequest[\s\S]*?missingSpecificEnv\(env, \[\]string\{([\s\S]*?)\n\s*\}\)/);
  assert.ok(requiredTencentEnv, "the Tencent provisioner must declare startup environment inputs");
  assert.ok(nodePoolInputs, "the Tencent provisioner must declare node-pool environment inputs");
  const thresholds = [...source.matchAll(/const workspaceNodeImageGC(?:High|Low)ThresholdEnv = "([A-Z][A-Z0-9_]*)"/g)].map((match) => match[1]);
  return [...new Set([...names(requiredTencentEnv[1]), ...names(nodePoolInputs[1]), ...thresholds])];
}

async function tencentProviderRequiredFacts() {
  const provider = await readFile("services/fabric/internal/fabric/tencent_provider.go", "utf8");
  const constructor = provider.slice(provider.indexOf("func tencentInstallationConfigError"), provider.indexOf("func (p *TencentProvider) validateInstallationConfig"));
  const validated = [...constructor.matchAll(/missing = append\(missing, "([A-Z][A-Z0-9_]*)/g)].map((match) => match[1]);

  const runtime = await readFile("services/fabric/internal/fabric/tencent_provider_runtime.go", "utf8");
  const readiness = runtime.slice(runtime.indexOf("func (p *TencentProvider) Readiness"));
  const required = readiness.match(/required := \[\]string\{([^}]*)\}/);
  assert.ok(required, "TencentProvider.Readiness must declare its required facts");
  const reported = [...required[1].matchAll(/"([A-Z][A-Z0-9_]*)"/g)].map((match) => match[1]);

  const facts = [...new Set([...validated, ...reported])];
  assert.ok(facts.length >= 9, `Tencent provider must require its installation facts, saw ${facts.join(",")}`);
  return facts;
}

async function prepareManagedTKEComposeFixture(root) {
  const environment = await readFile("deploy/portable/opl-cloud.env.example", "utf8");
  const releaseEnvironment = environment
    .replace(/^OPL_RELEASE_SHA=.*$/m, "OPL_RELEASE_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
    .replace(/^OPL_RELEASE_TREE=.*$/m, "OPL_RELEASE_TREE=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
  await Promise.all([
    copyFile("compose.yaml", join(root, "compose.yaml")),
    copyFile("deploy/portable/compose.fabric-tencent-tke.yaml", join(root, "compose.fabric-tencent-tke.yaml")),
    copyFile("deploy/portable/compose.deployment-managed-tke.yaml", join(root, "compose.deployment-managed-tke.yaml")),
    writeFile(join(root, "opl-cloud.env"), releaseEnvironment.replace(/^OPL_WORKSPACE_IMAGE_RELEASES_JSON=.*$/m, "OPL_WORKSPACE_IMAGE_RELEASES_JSON={\"releases\":[]}"))
  ]);
}

function composeConfig(root, envFile, ...args) {
  return spawnSync("docker", [
    "compose", "--env-file", envFile,
    "-f", "compose.yaml",
    "-f", "compose.deployment-managed-tke.yaml",
    "-f", "compose.fabric-tencent-tke.yaml",
    "config", ...args
  ], {
    cwd: root,
    encoding: "utf8",
    timeout: 30_000,
    env: { PATH: process.env.PATH, HOME: process.env.HOME }
  });
}

test("portable managed-TKE Fabric overlay forwards every protected-resource fact its guard reads", async () => {
  // The guard rejects every mutating kubectl action without these facts, so they
  // are mandatory for a tencent-tke installation rather than optional extras.
  const required = await protectedResourceFacts();
  const overlay = YAML.parse(await readFile("deploy/portable/compose.fabric-tencent-tke.yaml", "utf8"));
  const environment = overlay.services.fabric.environment;
  for (const name of required) {
    assert.ok(name in environment, `compose.fabric-tencent-tke.yaml must forward ${name} to Fabric`);
    assert.ok(String(environment[name]).includes(`\${${name}`), `${name} must be sourced from the installer environment`);
  }
});

test("portable managed-TKE Fabric overlay forwards every Tencent provisioner bootstrap input", async () => {
  const overlay = YAML.parse(await readFile("deploy/portable/compose.fabric-tencent-tke.yaml", "utf8"));
  const environment = overlay.services.fabric.environment;
  for (const name of await tencentProvisionerBootstrapFacts()) {
    assert.ok(name in environment, `compose.fabric-tencent-tke.yaml must forward ${name} to Fabric`);
    const forwarded = String(environment[name]);
    const sourcedFromInstaller = forwarded.includes(`\${${name}`);
    const regionDerivedFromFabric = name === "TENCENTCLOUD_REGION" && forwarded.includes("${OPL_TENCENT_REGION:");
    assert.ok(sourcedFromInstaller || regionDerivedFromFabric, `${name} must be sourced from the installer environment`);
    assert.match(forwarded, /^\$\{[A-Z][A-Z0-9_]*:\?[^}]+\}$/, `${name} must fail closed when unset`);
  }
});

test("portable managed-TKE Fabric overlay forwards every fact the Tencent provider validates or reports", async () => {
  const overlay = YAML.parse(await readFile("deploy/portable/compose.fabric-tencent-tke.yaml", "utf8"));
  const environment = overlay.services.fabric.environment;
  for (const name of await tencentProviderRequiredFacts()) {
    assert.ok(name in environment, `compose.fabric-tencent-tke.yaml must forward ${name} to Fabric`);
    assert.ok(String(environment[name]).includes(`\${${name}`), `${name} must be sourced from the installer environment`);
  }
  // The provider is the only reader of the cluster credentials, so the overlay
  // supplies the kubeconfig at the path it is told to read.
  const volumes = overlay.services.fabric.volumes ?? [];
  const kubeconfig = volumes.find((mount) => String(mount.target).includes("${TENCENT_DEPLOY_KUBECONFIG_REF"));
  assert.ok(kubeconfig, "Fabric must mount the TKE deploy kubeconfig at TENCENT_DEPLOY_KUBECONFIG_REF");
  assert.equal(kubeconfig.read_only, true);
  assert.ok(String(kubeconfig.source).includes("${TENCENT_DEPLOY_KUBECONFIG_HOST_PATH"), "the kubeconfig bind source must be an installer fact");
});

test("portable environment template declares the protected-resource and application-origin facts", async () => {
  const template = await readFile("deploy/portable/opl-cloud.env.example", "utf8");
  const provisionerFacts = await tencentProvisionerBootstrapFacts();
  for (const name of [...await protectedResourceFacts(), ...await tencentProviderRequiredFacts(), ...provisionerFacts.filter((name) => name !== "TENCENTCLOUD_REGION")]) {
    assert.match(template, new RegExp(`^${name}=`, "m"), `opl-cloud.env.example must declare ${name}`);
  }
  assert.match(template, /^OPL_TENCENT_REGION=/m);
  assert.match(template, /^TENCENT_DEPLOY_KUBECONFIG_HOST_PATH=/m);
  assert.match(template, /^OPL_WORKSPACE_APPLICATION_DOMAIN=/m);
});

test("base Compose forwards the application-origin domain the Control Plane and Serve read", async () => {
  const compose = YAML.parse(await readFile("compose.yaml", "utf8"));
  const controlPlane = compose.services["control-plane"].environment;
  assert.ok("OPL_WORKSPACE_APPLICATION_DOMAIN" in controlPlane, "Control Plane must receive the application-origin domain");
  const controlPlaneReader = await readFile("services/control-plane/internal/server/workspace_application_origin.go", "utf8");
  const serveReader = await readFile("services/serve/internal/delivery/routeorigin.go", "utf8");
  assert.match(controlPlaneReader, /"OPL_WORKSPACE_APPLICATION_DOMAIN"/);
  assert.match(serveReader, /"OPL_WORKSPACE_APPLICATION_DOMAIN"/);
});

test("managed-TKE deployment explicitly enables the Workspace progression workers", async () => {
  const base = YAML.parse(await readFile("compose.yaml", "utf8"));
  const overlay = YAML.parse(await readFile("deploy/portable/compose.deployment-managed-tke.yaml", "utf8"));
  const overlayEnvironment = overlay.services["control-plane"].environment;
  const environment = {
    ...base.services["control-plane"].environment,
    ...overlayEnvironment
  };
  for (const name of [
    "OPL_WORKSPACE_LAUNCH_WORKER_ENABLED",
    "OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED",
    "OPL_MONTHLY_BILLING_WORKER_ENABLED"
  ]) {
    assert.equal(overlayEnvironment[name], "1", `${name} must be declared by the managed-TKE deployment overlay`);
    assert.equal(environment[name], "1", `${name} must be enabled by the managed-TKE deployment overlay`);
  }
});

test("managed-TKE portable bundle resolves the runtime inputs and fails closed when they are missing", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-cloud-managed-tke-"));
  try {
    await prepareManagedTKEComposeFixture(root);
    const resolved = composeConfig(root, "opl-cloud.env", "--format", "json");
    assert.equal(resolved.error, undefined);
    assert.equal(resolved.status, 0, [resolved.stdout, resolved.stderr].filter(Boolean).join("\n"));
    const services = JSON.parse(resolved.stdout).services;
    const fabricEnvironment = services.fabric.environment;
    const controlPlaneEnvironment = services["control-plane"].environment;
    const inputs = {
      TENCENTCLOUD_SECRET_ID: "<replace-with-tencent-secret-id>",
      TENCENTCLOUD_SECRET_KEY: "<replace-with-tencent-secret-key>",
      OPL_TENCENT_REGION: "<replace-with-tencent-region>",
      TENCENT_DEPLOY_CLUSTER_ID: "<replace-with-tke-cluster-id>",
      TENCENT_CVM_SUBNET_ID: "<replace-with-tencent-cvm-subnet-id>",
      TENCENT_CVM_SECURITY_GROUP_IDS: "<replace-with-tencent-cvm-security-group-ids>",
      OPL_WORKSPACE_NODE_IMAGE_GC_HIGH_THRESHOLD_PERCENT: "85",
      OPL_WORKSPACE_NODE_IMAGE_GC_LOW_THRESHOLD_PERCENT: "70"
    };
    for (const [name, value] of Object.entries(inputs)) {
      assert.equal(fabricEnvironment[name], value, `${name} must reach Fabric unchanged`);
    }
    assert.equal(fabricEnvironment.TENCENTCLOUD_REGION, inputs.OPL_TENCENT_REGION);
    for (const [serviceName, service] of Object.entries(services)) {
      if (serviceName === "fabric") continue;
      assert.ok(!("TENCENTCLOUD_SECRET_ID" in service.environment), `${serviceName} must not receive provider credentials`);
      assert.ok(!("TENCENTCLOUD_SECRET_KEY" in service.environment), `${serviceName} must not receive provider credentials`);
    }
    assert.equal(controlPlaneEnvironment.OPL_WORKSPACE_LAUNCH_WORKER_ENABLED, "1");
    assert.equal(controlPlaneEnvironment.OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED, "1");
    assert.equal(controlPlaneEnvironment.OPL_MONTHLY_BILLING_WORKER_ENABLED, "1");
    assert.equal(controlPlaneEnvironment.OPL_RELEASE_SHA, "a".repeat(40));
    assert.equal(controlPlaneEnvironment.OPL_RELEASE_TREE, "b".repeat(40));

    const environment = await readFile(join(root, "opl-cloud.env"), "utf8");
    for (const name of Object.keys(inputs)) {
      for (const replacement of ["", `${name}=\n`]) {
        const missingPath = join(root, "opl-cloud.missing.env");
        await writeFile(missingPath, environment.replace(new RegExp(`^${name}=.*\\n`, "m"), replacement), { mode: 0o600 });
        const missing = composeConfig(root, "opl-cloud.missing.env", "--quiet");
        assert.equal(missing.error, undefined);
        assert.notEqual(missing.status, 0, `${name} must fail Compose resolution when missing or empty`);
        assert.match(missing.stderr, new RegExp(name));
      }
    }
    for (const name of ["OPL_RELEASE_SHA", "OPL_RELEASE_TREE"]) {
      for (const replacement of ["", `${name}=\n`]) {
        const missingPath = join(root, "opl-cloud.missing-release.env");
        await writeFile(missingPath, environment.replace(new RegExp(`^${name}=.*\\n`, "m"), replacement), { mode: 0o600 });
        const missing = composeConfig(root, "opl-cloud.missing-release.env", "--quiet");
        assert.equal(missing.error, undefined);
        assert.notEqual(missing.status, 0, `${name} must fail Compose resolution when missing or empty`);
        assert.match(missing.stderr, new RegExp(name));
      }
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
