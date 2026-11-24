import { createHash, randomBytes } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import {
  localV2EnvFile, localV2InstallationFacts, localV2ReferencedVariables,
} from "./local-v2-installation-facts.ts";

// Installation inputs for the existing native topology. These are run-owned
// fixture facts; no production credential, provider or billing authority is used.
export function cleanHostInstallation({ template, compose, directory, sourceSha, cloudImage, workspaceImage, storageRoot, project, publisherSchemaDigest, tenantId = "qualification-bootstrap", insecureHttpHosts = "", buildRegistry = "" }) {
  if (!/^[0-9a-f]{40}$/.test(sourceSha)) throw new Error("clean_host_source_invalid");
  for (const image of [cloudImage, workspaceImage]) {
    if (!/^[^\s@]+@sha256:[0-9a-f]{64}$/.test(image)) throw new Error("clean_host_image_invalid");
  }
  const referenced = new Set(localV2ReferencedVariables(compose));
  const values = {};
  for (const match of template.matchAll(/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/gm)) {
    const name = match[1];
    if (name.endsWith("_PEER_TOKENS")) continue;
    if (/(?:_PASSWORD|_TOKEN|_KEY|_SEED)$/.test(name)) values[name] ??= randomBytes(32).toString("hex");
  }
  const email = "local-qualification@example.test";
  // The registry the User's OCI destination is bound to and the registry the
  // Build owner may write into are one installation fact. The Build owner
  // refuses any destination whose registry namespace is outside its approved
  // prefix, so the two must name the same host and the same namespace.
  const registryHost = buildRegistry || workspaceImage.split("/")[0];
  const registryNamespace = "qualification";
  if (buildRegistry && !/^127\.0\.0\.1:[1-9][0-9]{0,4}$/.test(buildRegistry)) {
    throw new Error("clean_host_build_registry_invalid");
  }
  // The plain-HTTP hosts the Build owner may pull from.
  //
  // The value belongs to whoever created the registry: only that caller knows
  // the host the registry really serves on, and a host that serves TLS does
  // not belong here at all. The tool never guesses the host and never writes
  // one into the published Compose file.
  const insecureHosts = String(insecureHttpHosts || "").trim();
  if (insecureHosts && !/^[A-Za-z0-9._:-]+(,[A-Za-z0-9._:-]+)*$/.test(insecureHosts)) {
    throw new Error("clean_host_insecure_http_hosts_invalid");
  }
  const profile = {
    schemaVersion: 1, profileId: "local-docker/local", capabilityVersion: "local-docker/v1", region: "local",
    accountBindings: [{ tenantId, accountId: "local-host" }],
    packages: [{ id: "local-basic", name: "Local qualification", available: true,
      compute: { id: "local-basic", server: "configured", cpu: 2, memoryGb: 4, instanceType: "local-basic" },
      storage: { id: "local-basic-10gib", sizeGb: 10, quotaPolicy: "linux-project" } }],
  };
  Object.assign(values, {
    OPL_CLOUD_IMAGE: cloudImage, OPL_WORKSPACE_IMAGE: workspaceImage,
    OPL_RELEASE_SHA: sourceSha,
    OPL_PKI_DIR: resolve(directory, "pki"),
    OPL_PUBLIC_URL: "https://localhost:8443", OPL_PROXY_PORT: "8443",
    OPL_DOCKER_SUBNET: "172.29.0.0/24", OPL_POSTGRES_HOST: "172.29.0.10",
    OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER: `${project}-control-plane`,
    OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH: resolve(directory, "plugin.schema.json"),
    OPL_PUBLISHER_SCHEMA_DIGEST: publisherSchemaDigest,
    // The published Agent Plugins schema the Capability owner validates every
    // uploaded manifest against. Its digest is the published artifact's own.
    OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST: "sha256:0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883",
    OPL_SUB2API_URL: "https://cloud-local-v2:8443/sub2api",
    OPL_CAPABILITY_OBJECT_URL: "https://localhost:8443/objects",
    OPL_WORKSPACE_DOMAIN: "ws-qualification.localhost",
    OPL_QUALIFICATION_USER_EMAIL: email, OPL_SUB2API_ADMIN_EMAIL: email,
    OPL_SUB2API_ADMIN_PASSWORD: values.OPL_QUALIFICATION_USER_PASSWORD,
    OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: storageRoot,
    OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT: resolve(directory, "fabric-secrets"),
    OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: JSON.stringify(profile),
    OPL_FABRIC_LOCAL_DOCKER_TRUSTED_WORKSPACE_IMAGES: buildRegistry
      ? `${workspaceImage},${registryHost}/${registryNamespace}/local-qualification`
      : workspaceImage,
    OPL_FABRIC_LOCAL_DOCKER_PROBE_IMAGE: workspaceImage,
    OPL_RESOURCE_CATALOG_PROFILE_JSON: JSON.stringify({ provider: "local-docker", region: "local", billingMode: "LOCAL_NO_CHARGE", providerCapabilityVersion: "local-docker/v1" }),
    OPL_DOCKER_SOCKET_PATH: "/var/run/docker.sock",
    OPL_CONSOLE_USERS_JSON: "[]", OPL_PLATFORM_ADMIN_SUBJECTS: "41", OPL_INVITATION_TTL: "168h",
    OPL_GATEWAY_WORKSPACE_KEY_GROUP: "Codex", OPL_WORKSPACE_APPLICATION_INGRESS_PEERS: "{}",
    OPL_WORKSPACE_REGISTRY_HOST: registryHost, OPL_WORKSPACE_REGISTRY_NAMESPACE: registryNamespace,
    OPL_BUILD_BUILDER: buildRegistry ? "qualification" : "default",
    OPL_BUILD_REGISTRY_PREFIX: `${registryHost}/${registryNamespace}`,
  });
  if (insecureHosts) values.OPL_BUILD_INSECURE_HTTP_HOSTS = insecureHosts;
  const facts = localV2InstallationFacts({ template, compose, values });
  for (const name of facts.unresolved) {
    if (referenced.has(name)) throw new Error(`clean_host_installation_fact_missing:${name}`);
  }
  return facts;
}

export function cleanHostApplication(workspaceImage) {
  const [repository, digest] = workspaceImage.split("@");
  const platform = { os: "linux", architecture: "amd64" };
  return {
    schemaVersion: "opl-publisher-contract/v1", publisherNamespaceId: "local-qualification",
    image: { repository, digest, platform }, kind: "runtime", runtimeAbiVersion: "opl-runtime/v1",
    applicationAccess: { mode: "anonymous" },
    data: { schemaVersion: "local-qualification/v1", upgrade: { mode: "compatible", compatibleFromSchemaVersions: ["local-qualification/v1"], backupRequired: false },
      rollback: { safe: true, compatibleSchemaVersions: ["local-qualification/v1"] }, mountPolicies: [{ mountName: "workspace-data", concurrentWritersSupported: false }] },
    applicationRevisionTemplate: {
      schemaVersion: 1, applicationId: "local-qualification-app", version: "1.0.0", platform: "linux/amd64", image: workspaceImage,
      ports: [{ name: "http", port: 3000, protocol: "TCP" }], entryPort: "http",
      healthChecks: [{ port: 3000, path: "/" }],
      persistentMounts: [{ name: "workspace-data", mountPath: "/data", readOnly: false }], exposurePolicy: "anonymous",
    },
  };
}

export function cleanHostBuildCatalog({ runtimeImage, webuiImage, frontendImage, recipeRef, registry }) {
  const image = (ref) => {
    const [repository, digest, extra] = String(ref || "").split("@");
    if (extra !== undefined || !repository.startsWith(`${registry}/qualification-fixtures/`) ||
        !/^sha256:[0-9a-f]{64}$/.test(digest || "")) {
      throw new Error("clean_host_build_artifact_invalid");
    }
    return { repository, digest, platform: { os: "linux", architecture: "amd64" } };
  };
  const runtimeArtifact = image(runtimeImage);
  const runtime = cleanHostApplication(runtimeImage);
  const recipe = image(recipeRef);
  runtime.applicationRevisionTemplate.applicationId = "local-qualification-build-runtime";
  runtime.packageFormatVersions = ["agent-plugins/1.0.0"];
  runtime.buildRecipe = {
    version: "opl-build-recipe/v1",
    frontend: image(frontendImage),
    recipe: {
      repository: recipe.repository, digest: recipe.digest,
      mediaType: "application/vnd.opl.build-recipe.v1+tar", dockerfilePath: "Dockerfile",
    },
    runtimeContextName: "runtime",
    packageInput: {
      contextName: "agent_package", formatVersion: runtime.packageFormatVersions[0],
      sourceRoot: "agent", targetPath: "/agent", uid: 1000, gid: 1000,
    },
    webuiInput: { contextName: "webui", sourcePath: "/web", targetPath: "/web", uid: 1000, gid: 1000 },
    networkPolicy: "none",
    outputPlatform: runtimeArtifact.platform,
    outputImageCommand: { entrypoint: ["/agent/bin/runtime"], cmd: ["serve"] },
  };
  return {
    runtime,
    webui: {
      schemaVersion: "opl-publisher-contract/v1", kind: "webui",
      publisherNamespaceId: runtime.publisherNamespaceId, image: image(webuiImage),
      runtimeAbiVersions: [runtime.runtimeAbiVersion], uiProtocolVersion: "opl-webui/v1",
      integrationMode: "bundled_static", staticRoot: "/web", entryFile: "index.html",
      assetBasePath: "/assets/", apiBasePath: "/api/", authenticationProtocol: "opl-application-session/v1",
      supportsSse: true, supportsWebsocket: false,
    },
  };
}

async function main() {
  const options = Object.fromEntries(process.argv.slice(2).map((value, index, args) => index % 2 === 0 ? [value.replace(/^--/, ""), args[index + 1]] : null).filter(Boolean));
  const productRoot = resolve(options["product-root"] || ".");
  const directory = resolve(options.directory);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await mkdir(resolve(directory, "fabric-secrets"), { recursive: true, mode: 0o700 });
  const template = await readFile(resolve(productRoot, "deploy/portable/opl-cloud.env.example"), "utf8");
  const compose = await readFile(resolve(productRoot, "deploy/portable/compose.local-v2.yaml"), "utf8");
  if (options["cleanup-from"]) {
    const receipt = JSON.parse(await readFile(resolve(options["cleanup-from"]), "utf8"));
    const readbacks = receipt?.evidence?.readbacks;
    if (receipt?.mode !== "local-v2" || receipt?.source?.sha !== options["source-sha"] ||
        !/^opl-native-[1-9][0-9]*-[1-9][0-9]*$/.test(String(options.project || ""))) {
      throw new Error("clean_host_cleanup_receipt_invalid");
    }
    if (receipt.status === "NOT_READY" && receipt.stage === "installation_preflight" && !readbacks?.installation &&
        !readbacks?.order) return;
    if (readbacks?.installation?.cloudImage !== options["cloud-image"] ||
        readbacks?.installation?.qualificationEmail !== "local-qualification@example.test") {
      throw new Error("clean_host_cleanup_receipt_invalid");
    }
    const workspaceId = String(readbacks?.order?.workspaceId || "");
    if (!workspaceId) return;
    const profileLine = (await readFile(resolve(directory, "installation.env"), "utf8")).split("\n")
      .find((line) => line.startsWith("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON="));
    const profile = JSON.parse(profileLine.slice(profileLine.indexOf("=") + 1));
    if (!/^workspace_[a-z0-9]+$/.test(workspaceId) || readbacks?.accountBinding?.accountId !== "local-host" ||
        !profile.accountBindings.some((binding) => binding.tenantId === readbacks?.tenant?.id && binding.accountId === "local-host")) {
      throw new Error("clean_host_cleanup_owner_mismatch");
    }
    const { cleanupLocalQualificationResources } = await import(pathToFileURL(resolve(productRoot, "tools/local-workspace-qualification.ts")).href);
    const residuals = await cleanupLocalQualificationResources("local-host", workspaceId);
    await writeFile(resolve(directory, "fixture-cleanup.json"), `${JSON.stringify({ scope: "run_owned_local_fixture", residuals })}\n`, { mode: 0o600 });
    return;
  }
  if (options["bind-from"]) {
    const receipt = JSON.parse(await readFile(resolve(options["bind-from"]), "utf8"));
    const installation = receipt?.evidence?.readbacks?.installation;
    const tenantId = receipt?.evidence?.readbacks?.tenant?.id;
    if (receipt?.mode !== "local-v2" || receipt?.status !== "NOT_READY" ||
        receipt?.errorCode !== "local_v2_account_binding_required" ||
        receipt?.source?.sha !== options["source-sha"] ||
        installation?.cloudImage !== options["cloud-image"] ||
        !/^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(String(tenantId || ""))) {
      throw new Error("clean_host_tenant_binding_receipt_invalid");
    }
    const previous = await readFile(resolve(directory, "installation.env"), "utf8");
    const values = Object.fromEntries(previous.split("\n").filter((line) => /^[A-Za-z_][A-Za-z0-9_]*=/.test(line))
      .map((line) => [line.slice(0, line.indexOf("=")), line.slice(line.indexOf("=") + 1)]));
    if (values.OPL_CLOUD_IMAGE !== options["cloud-image"] || values.OPL_WORKSPACE_IMAGE !== installation.workspaceImage) {
      throw new Error("clean_host_tenant_binding_installation_mismatch");
    }
    const profile = JSON.parse(values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON);
    profile.accountBindings = [{ tenantId, accountId: "local-host" }];
    values.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON = JSON.stringify(profile);
    const facts = localV2InstallationFacts({ template, compose, values });
    await writeFile(resolve(directory, "installation.env"), localV2EnvFile(facts), { mode: 0o600 });
    return;
  }
  const schema = await readFile(resolve(productRoot, "docs/spec/target/contracts/publisher-contract.schema.json"));
  const facts = cleanHostInstallation({ template, compose, directory, sourceSha: options["source-sha"],
    cloudImage: options["cloud-image"], workspaceImage: options["workspace-image"], storageRoot: options["storage-root"], project: options.project,
    publisherSchemaDigest: `sha256:${createHash("sha256").update(schema).digest("hex")}`,
    insecureHttpHosts: options["insecure-http-hosts"] || "", buildRegistry: options["build-registry"] || "" });
  await writeFile(resolve(directory, "installation.env"), localV2EnvFile(facts), { mode: 0o600 });
  await writeFile(resolve(directory, "application.json"), `${JSON.stringify(cleanHostApplication(options["workspace-image"]))}\n`, { mode: 0o600 });
  if (options["build-registry"]) {
    const catalog = cleanHostBuildCatalog({
      registry: options["build-registry"], runtimeImage: options["runtime-image"], webuiImage: options["webui-image"],
      frontendImage: options["frontend-image"], recipeRef: options["recipe-ref"],
    });
    await writeFile(resolve(directory, "build-catalog.json"), `${JSON.stringify(catalog)}\n`, { mode: 0o600 });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) await main();
