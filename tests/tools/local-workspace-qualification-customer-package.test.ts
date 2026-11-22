import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  CUSTOMER_EVIDENCE_LEVEL_ARTIFACT,
  CUSTOMER_EVIDENCE_LEVEL_CONTENT,
  assertCustomerCompleteUploadOperation,
  assertCustomerDeployedVersionIdentity,
  assertCustomerEntryContentProvenance,
  assertCustomerLocalNoChargeQuote,
  assertCustomerPackageLayout,
  assertCustomerPackageVersionReadback,
  buildCustomerPackageArchive,
  customerEntryRedirectTarget,
  customerEvidencePlan,
  customerPackageName,
  customerPackageProbePath,
  customerWorkspaceIdentity,
  parseLocalQualificationArgs,
  runCustomerPackageEvidenceChain,
} from "../../tools/local-workspace-qualification.ts";

const toolSource = new URL("../../tools/local-workspace-qualification.ts", import.meta.url);

function refuses(action, code) {
  assert.throws(action, (error) => {
    assert.equal(error?.code, code, `expected ${code}, got ${error?.code}: ${error?.message}`);
    return true;
  });
}

test("a run that did not declare a customer package keeps the shape it had", () => {
  const parsed = parseLocalQualificationArgs([
    "--source-sha", "a".repeat(40),
    "--receipt", "/tmp/qualification.json",
    "--installation-env", "/tmp/installation.env",
    "--application-contract", "/tmp/application.json",
    "--project", "opl-native-1",
  ]);
  assert.deepEqual(Object.keys(parsed).sort(), [
    "applicationContract", "installationEnv", "mode", "project", "receiptPath", "sourceSha", "workspaceName",
  ]);
  assert.equal("customer" in parsed, false);
});

test("the customer package, its probe and its evidence level are inputs of the run", () => {
  const parsed = parseLocalQualificationArgs([
    "--source-sha", "a".repeat(40),
    "--receipt", "/tmp/qualification.json",
    "--installation-env", "/tmp/installation.env",
    "--application-contract", "/tmp/application.json",
    "--project", "opl-native-1",
    "--customer-package-dir", "/tmp/package",
    "--customer-probe-path", "agent/provenance/probe.txt",
  ]);
  assert.equal(parsed.customer.declared, true);
  assert.equal(parsed.customer.mode, "directory");
  assert.equal(parsed.customer.requestedEvidenceLevel, CUSTOMER_EVIDENCE_LEVEL_CONTENT);
  assert.equal(parsed.customer.achievableEvidenceLevel, CUSTOMER_EVIDENCE_LEVEL_CONTENT);
  assert.equal(parsed.customer.degradationReason, "");
});

test("a package directory without a probe path is refused before anything is uploaded", () => {
  refuses(() => customerEvidencePlan({ packageDir: "/tmp/package" }), "local_v2_customer_probe_path_required");
});

test("a package archive reaches the artifact level and says why", () => {
  const plan = customerEvidencePlan({ packageArchive: "/tmp/package.zip" });
  assert.equal(plan.achievableEvidenceLevel, CUSTOMER_EVIDENCE_LEVEL_ARTIFACT);
  assert.equal(plan.degradationReason, "");
  const demanding = customerEvidencePlan({ packageArchive: "/tmp/package.zip", expectedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT });
  assert.equal(demanding.achievableEvidenceLevel, CUSTOMER_EVIDENCE_LEVEL_ARTIFACT);
  assert.match(demanding.degradationReason, /cannot carry this run's probe/);
});

test("an unknown evidence level is refused by name", () => {
  refuses(() => customerEvidencePlan({ packageDir: "/tmp/package", probePath: "agent/probe.txt", expectedEvidenceLevel: "best-effort" }),
    "local_v2_customer_evidence_level_invalid");
});

test("the probe path is a relative path inside the package", () => {
  assert.equal(customerPackageProbePath("agent/provenance/probe.txt"), "agent/provenance/probe.txt");
  refuses(() => customerPackageProbePath("/agent/probe.txt"), "local_v2_customer_probe_path_invalid");
  refuses(() => customerPackageProbePath("../probe.txt"), "local_v2_customer_probe_path_invalid");
  refuses(() => customerPackageProbePath("agent/../probe.txt"), "local_v2_customer_probe_path_invalid");
});

test("the package name comes from the package the caller named", () => {
  assert.equal(customerPackageName({ mode: "directory", packageDir: "/tmp/my-agent" }, ""), "my-agent");
  assert.equal(customerPackageName({ mode: "archive", packageArchive: "/tmp/my-agent.zip" }, ""), "my-agent");
  assert.equal(customerPackageName({ mode: "directory", packageDir: "/tmp/my-agent" }, "other-name"), "other-name");
  refuses(() => customerPackageName({ mode: "directory", packageDir: "/tmp/my-agent" }, "not a name"), "local_v2_customer_package_name_invalid");
});

test("the uploaded archive carries the nonce in the probe path the caller named", async () => {
  const archive = await buildCustomerPackageArchive({
    packageDir: new URL("./fixtures/customer-agent-package", import.meta.url).pathname,
    probePath: "agent/provenance/probe.txt",
    probeContent: "nonce-of-this-run",
  });
  assert.ok(archive.entries.includes("agent/provenance/probe.txt"));
  assert.ok(archive.entries.includes("agent/bin/runtime"));
  assert.ok(archive.entries.includes("manifest.json"));
  assert.match(archive.digest, /^sha256:[0-9a-f]{64}$/);
  // The nonce is in the bytes the platform builds, and the placeholder of the
  // fixture is not.
  const text = archive.bytes.toString("latin1");
  assert.ok(text.includes("nonce-of-this-run"));
  assert.equal(text.includes("placeholder-probe"), false);
});

test("the upload completion is the owner's own operation and names the uploaded version", () => {
  const operation = {
    operationId: "op-1", owner: "capability", kind: "complete_upload",
    resourceId: "package-version-1", status: "succeeded", stage: "succeeded", observationResult: "confirmed",
  };
  assert.deepEqual(assertCustomerCompleteUploadOperation(operation, { packageVersionId: "package-version-1" }), {
    owner: "capability", kind: "complete_upload", resourceId: "package-version-1",
    status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op-1",
  });
  // The owner is not the capability owner.
  refuses(() => assertCustomerCompleteUploadOperation({ ...operation, owner: "build" }, { packageVersionId: "package-version-1" }),
    "local_v2_customer_upload_operation_invalid");
  // The kind is not the completion of an upload.
  refuses(() => assertCustomerCompleteUploadOperation({ ...operation, kind: "create_build" }, { packageVersionId: "package-version-1" }),
    "local_v2_customer_upload_operation_invalid");
  // The operation names another version.
  refuses(() => assertCustomerCompleteUploadOperation(operation, { packageVersionId: "package-version-2" }),
    "local_v2_customer_upload_operation_invalid");
  // The operation has not succeeded.
  refuses(() => assertCustomerCompleteUploadOperation({ ...operation, status: "accepted" }, { packageVersionId: "package-version-1" }),
    "local_v2_customer_upload_operation_invalid");
  // The operation is not confirmed.
  refuses(() => assertCustomerCompleteUploadOperation({ ...operation, observationResult: "unconfirmed" }, { packageVersionId: "package-version-1" }),
    "local_v2_customer_upload_operation_unconfirmed");
  // A success status alone cannot prove the owner's terminal stage or observation.
  for (const stage of [undefined, "", "receipt", "upload_verification"]) {
    refuses(() => assertCustomerCompleteUploadOperation({ ...operation, stage }, { packageVersionId: "package-version-1" }),
      "local_v2_customer_upload_operation_invalid");
  }
  for (const observationResult of [undefined, "", "unknown", "rejected"]) {
    refuses(() => assertCustomerCompleteUploadOperation({ ...operation, observationResult }, { packageVersionId: "package-version-1" }),
      "local_v2_customer_upload_operation_unconfirmed");
  }
  // A completion without the owner's operation identity is not a usable readback.
  refuses(() => assertCustomerCompleteUploadOperation({ ...operation, operationId: "" }, { packageVersionId: "package-version-1" }),
    "local_v2_customer_upload_operation_invalid");
});

test("the uploaded version is read back and matches the uploaded bytes", () => {
  const expected = { id: "package-version-1", packageId: "package-1", versionLabel: "1.0.0", sha256: "sha256:abc", sizeBytes: "12" };
  const readback = { id: "package-version-1", packageId: "package-1", versionLabel: "1.0.0", sha256: "sha256:abc", sizeBytes: 12, status: "uploaded" };
  assert.equal(assertCustomerPackageVersionReadback(readback, expected).status, "uploaded");
  // The owner stored other bytes.
  refuses(() => assertCustomerPackageVersionReadback({ ...readback, sha256: "sha256:other" }, expected), "local_v2_customer_package_version_mismatch");
  // The owner stored another size.
  refuses(() => assertCustomerPackageVersionReadback({ ...readback, sizeBytes: 13 }, expected), "local_v2_customer_package_version_mismatch");
  // The version is not in the uploaded state.
  refuses(() => assertCustomerPackageVersionReadback({ ...readback, status: "quarantined" }, expected), "local_v2_customer_package_version_mismatch");
  // The version belongs to another package.
  refuses(() => assertCustomerPackageVersionReadback({ ...readback, packageId: "package-2" }, expected), "local_v2_customer_package_version_mismatch");
});

test("the deployed version is the one this Build registered and not the first ready one", () => {
  const build = { id: "build-job-1", resultCapabilityVersionId: "capability-version-1", packageVersionId: "package-version-1" };
  const capabilityVersion = { id: "capability-version-1", buildJobId: "build-job-1", packageVersionId: "package-version-1" };
  assert.deepEqual(assertCustomerDeployedVersionIdentity({
    build, capabilityVersion, workspace: { capabilityVersionId: "capability-version-1" },
  }), {
    buildJobId: "build-job-1", capabilityVersionId: "capability-version-1",
    workspaceCapabilityVersionId: "capability-version-1", packageVersionId: "package-version-1",
  });
  // The Workspace runs another version: the one that was ready first.
  refuses(() => assertCustomerDeployedVersionIdentity({
    build, capabilityVersion, workspace: { capabilityVersionId: "capability-version-0" },
  }), "local_v2_customer_deployed_version_unbound");
  // The capability version is another version that happened to be ready.
  refuses(() => assertCustomerDeployedVersionIdentity({
    build, capabilityVersion: { ...capabilityVersion, id: "capability-version-0" }, workspace: { capabilityVersionId: "capability-version-0" },
  }), "local_v2_customer_deployed_version_unbound");
  // The capability version was registered by another Build.
  refuses(() => assertCustomerDeployedVersionIdentity({
    build, capabilityVersion: { ...capabilityVersion, buildJobId: "build-job-0" }, workspace: { capabilityVersionId: "capability-version-1" },
  }), "local_v2_customer_deployed_version_unbound");
  // The Build registered no version at all.
  refuses(() => assertCustomerDeployedVersionIdentity({
    build: { ...build, resultCapabilityVersionId: "" }, capabilityVersion, workspace: { capabilityVersionId: "capability-version-1" },
  }), "local_v2_customer_deployed_version_unbound");
  // The Build registered a Capability Version from another uploaded package.
  refuses(() => assertCustomerDeployedVersionIdentity({
    build, capabilityVersion: { ...capabilityVersion, packageVersionId: "package-version-0" },
    workspace: { capabilityVersionId: "capability-version-1" },
  }), "local_v2_customer_deployed_version_unbound");
});

test("the entry answers with the nonce this run put into the package", () => {
  assert.deepEqual(assertCustomerEntryContentProvenance('{"probe":"nonce-of-this-run"}', "nonce-of-this-run"), {
    matched: true, responseBytes: Buffer.byteLength('{"probe":"nonce-of-this-run"}', "utf8"),
  });
  // The entry answers without the nonce: it is another upload's answer.
  refuses(() => assertCustomerEntryContentProvenance('{"probe":"placeholder-probe"}', "nonce-of-this-run"),
    "local_v2_customer_entry_content_unbound");
  // The entry is empty.
  refuses(() => assertCustomerEntryContentProvenance("", "nonce-of-this-run"), "local_v2_customer_entry_content_unbound");
});

test("the purchase is refused unless the quote is the local provider's free quote", () => {
  const quote = { id: "quote-1", totalUSDMicros: "0" };
  assert.deepEqual(assertCustomerLocalNoChargeQuote(quote, { provider: "local-docker", billingMode: "LOCAL_NO_CHARGE" }), {
    provider: "local-docker", billingMode: "LOCAL_NO_CHARGE", totalUSDMicros: "0",
  });
  // A paid quote reaches a purchase and must not be bought here.
  refuses(() => assertCustomerLocalNoChargeQuote({ ...quote, totalUSDMicros: "1000" }, { provider: "local-docker", billingMode: "LOCAL_NO_CHARGE" }),
    "local_v2_customer_quote_not_local_no_charge");
  // Another provider's quote.
  refuses(() => assertCustomerLocalNoChargeQuote(quote, { provider: "tencent", billingMode: "LOCAL_NO_CHARGE" }),
    "local_v2_customer_quote_not_local_no_charge");
  // Another billing mode.
  refuses(() => assertCustomerLocalNoChargeQuote(quote, { provider: "local-docker", billingMode: "PREPAID" }),
    "local_v2_customer_quote_not_local_no_charge");
});

test("the Workspace identity is the purchase operation's resource id", () => {
  assert.deepEqual(customerWorkspaceIdentity({ operationId: "operation-1", resourceId: "workspace_abc123" }), {
    operationId: "operation-1", workspaceId: "workspace_abc123",
  });
  // The operation carries an identity of its own but names no resource.
  refuses(() => customerWorkspaceIdentity({ operationId: "operation-1", id: "workspace_abc123", workspaceId: "workspace_abc123" }),
    "local_v2_customer_workspace_identity_missing");
  // The operation names no operation.
  refuses(() => customerWorkspaceIdentity({ resourceId: "workspace_abc123" }), "local_v2_customer_workspace_identity_missing");
});

test("the package carries its runtime and its probe under the source root the recipe declares", () => {
  const entries = ["manifest.json", "agent/bin/runtime", "agent/provenance/probe.txt"];
  assert.deepEqual(assertCustomerPackageLayout({
    entries, sourceRoot: "agent", targetPath: "/app", entrypoint: ["/app/bin/runtime"], probePath: "agent/provenance/probe.txt",
  }), { sourceRoot: "agent", probePath: "agent/provenance/probe.txt", runtimePath: "agent/bin/runtime" });
  // A source root of "." is refused by the Build Runner.
  refuses(() => assertCustomerPackageLayout({
    entries, sourceRoot: ".", targetPath: "/app", entrypoint: ["/app/bin/runtime"], probePath: "agent/provenance/probe.txt",
  }), "local_v2_customer_build_recipe_source_root_invalid");
  // A source root that climbs out of the package.
  refuses(() => assertCustomerPackageLayout({
    entries, sourceRoot: "../elsewhere", targetPath: "/app", entrypoint: ["/app/bin/runtime"], probePath: "agent/provenance/probe.txt",
  }), "local_v2_customer_build_recipe_source_root_invalid");
  // The probe sits outside the directory the package is copied from.
  refuses(() => assertCustomerPackageLayout({
    entries, sourceRoot: "agent", targetPath: "/app", entrypoint: ["/app/bin/runtime"], probePath: "provenance/probe.txt",
  }), "local_v2_customer_probe_outside_source_root");
  // The runtime sits outside the directory the package is copied from.
  refuses(() => assertCustomerPackageLayout({
    entries: ["manifest.json", "bin/runtime", "agent/provenance/probe.txt"],
    sourceRoot: "agent", targetPath: "/app", entrypoint: ["/app/bin/runtime"], probePath: "agent/provenance/probe.txt",
  }), "local_v2_customer_runtime_outside_source_root");
  // The recipe declares no source root at all.
  refuses(() => assertCustomerPackageLayout({
    entries, sourceRoot: "", targetPath: "/app", entrypoint: ["/app/bin/runtime"], probePath: "agent/provenance/probe.txt",
  }), "local_v2_customer_build_recipe_unavailable");
});

test("the tool carries no agent name, no package identity and no expected response content", async () => {
  const source = await readFile(toolSource, "utf8");
  // The prose of the tool is not a fact it asserts, so the guard reads the
  // code without the comments.
  const code = source.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/(^|[^:])\/\/.*$/gm, "$1");
  // The fixture is one input of the chain and not a fact of the tool: the tool
  // must not know the name of the package it uploads.
  const manifest = JSON.parse(await readFile(new URL("./fixtures/customer-agent-package/manifest.json", import.meta.url), "utf8"));
  assert.equal(code.includes(String(manifest.name)), false);
  assert.equal(code.includes("placeholder-probe"), false);
  assert.equal(code.includes("ibd-evidence-assistant"), false);
  // The tool states no response content it expects. The only content it knows
  // is the nonce it mints for the run.
  assert.equal(/"(?:service|body|payload|content)"\s*:\s*"/.test(code), false);
  assert.equal(/expect(?:ed|s)?[ _-]?(?:body|content|response)/i.test(code), false);
});

test("every size the chain sends to an owner is a decimal string", async () => {
  // A 64-bit field is carried as a decimal string. The owners' strict JSON
  // contract types its 64-bit fields as strings, and a JSON number is refused
  // by the boundary before the owner ever sees the request, so a chain that
  // sends one never reaches its Build.
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const archive = await buildCustomerPackageArchive({
    packageDir, probePath: "agent/provenance/probe.txt", probeContent: "nonce",
  });
  const sent = [];
  const stop = new Error("stop_after_the_upload_completion");
  let uploaded = null;
  const http = {
    json: async (path, init = {}) => {
      if (init.body !== undefined) sent.push({ path, body: init.body });
      if (path === "/api/v2/namespaces") return { payload: { items: [{ id: "ns_1", isDefault: true }] } };
      if (path === "/api/v2/packages") return { payload: { id: "pkg_1" } };
      if (path === "/api/v2/packages/pkg_1/uploads") {
        uploaded = init.body;
        return { payload: { id: "upload_1", packageVersionId: "pv_1", partSizeBytes: "16777216" } };
      }
      if (path === "/api/v2/uploads/upload_1/parts") return { payload: { url: "https://objects.test/part-1", method: "PUT", contentType: "application/octet-stream" } };
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      throw stop;
    },
  };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ status: 200, headers: { get: () => "" } });
  try {
    await assert.rejects(runCustomerPackageEvidenceChain({
      http,
      auth: {},
      wait: async () => {},
      plan: {
        mode: "directory", packageDir, probePath: "agent/provenance/probe.txt",
        achievableEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        requestedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        degradationReason: "",
      },
      packageName: "local-qualification-agent",
      versionLabel: "1.0.0",
      workspaceName: "local qualification",
      provider: "local-docker",
      billingMode: "LOCAL_NO_CHARGE",
      computePlanId: "compute_1",
      storagePlanId: "storage_1",
      sourceSha: "a".repeat(40),
    }), (error) => error === stop);
  } finally {
    globalThis.fetch = originalFetch;
  }
  const sizes = sent.flatMap(({ body }) => [
    body?.sizeBytes,
    ...(Array.isArray(body?.parts) ? body.parts.map((part) => part?.sizeBytes) : []),
  ]).filter((value) => value !== undefined);
  assert.ok(sizes.length >= 3, "the chain sent no size to an owner");
  for (const size of sizes) {
    assert.equal(typeof size, "string", `a 64-bit size must be a decimal string, got ${JSON.stringify(size)}`);
  }
});

test("a missing Build policy preserves the approved upload readback and stops before requesting a Build", async () => {
  // The upload still uses the installation's own object requester. The policy
  // gap is reported only after the owner confirms those bytes.
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const archive = await buildCustomerPackageArchive({
    packageDir, probePath: "agent/provenance/probe.txt", probeContent: "nonce",
  });
  const written = [];
  const readback = {};
  let uploaded = null;
  const http = {
    json: async (path, init = {}) => {
      if (path === "/api/v2/namespaces") return { payload: { items: [{ id: "ns_1", isDefault: true }] } };
      if (path === "/api/v2/packages") return { payload: { id: "pkg_1" } };
      if (path === "/api/v2/packages/pkg_1/uploads") {
        uploaded = init.body;
        return { payload: { id: "upload_1", packageVersionId: "pv_1", partSizeBytes: "16777216" } };
      }
      if (path === "/api/v2/uploads/upload_1/parts") return { payload: { url: "https://objects.test/part-1", method: "PUT", contentType: "application/octet-stream" } };
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      if (path === "/api/v2/catalog/build-policy") {
        throw Object.assign(new Error("HTTP GET /api/v2/catalog/build-policy returned 404"), { status: 404 });
      }
      assert.fail(`unexpected request ${init.method || "GET"} ${path}`);
    },
  };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => { throw new Error("the host-trusting default must not write the part"); };
  try {
    await assert.rejects(runCustomerPackageEvidenceChain({
      http,
      auth: {},
      wait: async () => {},
      plan: {
        mode: "directory", packageDir, probePath: "agent/provenance/probe.txt",
        achievableEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        requestedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        degradationReason: "",
      },
      packageName: "local-qualification-agent",
      versionLabel: "1.0.0",
      workspaceName: "local qualification",
      provider: "local-docker",
      billingMode: "LOCAL_NO_CHARGE",
      computePlanId: "compute_1",
      storagePlanId: "storage_1",
      sourceSha: "a".repeat(40),
      readback,
      putPart: async (url, init) => {
        written.push({ url, method: init.method, digest: `sha256:${createHash("sha256").update(init.body).digest("hex")}` });
        return { status: 200, headers: { get: () => "" } };
      },
    }), { code: "local_v2_customer_build_policy_unavailable" });
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.equal(written.length, 1);
  assert.equal(written[0].url, "https://objects.test/part-1");
  assert.equal(written[0].method, "PUT");
  // The bytes the owner was promised are the bytes that were written.
  assert.equal(written[0].digest, uploaded.sha256);
  assert.equal(readback.upload.operationId, "op_upload");
  assert.equal(readback.packageVersion.id, "pv_1");
  assert.equal(readback.packageVersion.status, "uploaded");
  assert.equal(readback.build, undefined);
});

test("the approved Runtime Release is read out of the catalog page", async () => {
  // The owner publishes the approved Runtime Releases as one page and serves no
  // route for a single release, so a chain that asked for one release by its
  // id would read a 404 the owner never wrote.
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const archive = await buildCustomerPackageArchive({
    packageDir, probePath: "agent/provenance/probe.txt", probeContent: "nonce",
  });
  const asked = [];
  const stop = new Error("stop_after_the_build_request");
  let uploaded = null;
  const http = {
    json: async (path, init = {}) => {
      asked.push(path);
      if (path === "/api/v2/namespaces") return { payload: { items: [{ id: "ns_1", isDefault: true }] } };
      if (path === "/api/v2/packages") return { payload: { id: "pkg_1" } };
      if (path === "/api/v2/packages/pkg_1/uploads") {
        uploaded = init.body;
        return { payload: { id: "upload_1", packageVersionId: "pv_1", partSizeBytes: "16777216" } };
      }
      if (path === "/api/v2/uploads/upload_1/parts") return { payload: { url: "https://objects.test/part-1", method: "PUT", contentType: "application/octet-stream" } };
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      if (path === "/api/v2/catalog/build-policy") return { payload: { runtimeVersionId: "rt_1", defaultWebuiVersionId: "wu_1" } };
      if (path === "/api/v2/catalog/runtime-versions?cursor=") {
        return { payload: { items: [{ id: "rt_0" }], nextCursor: "rt_0" } };
      }
      if (path === "/api/v2/catalog/runtime-versions?cursor=rt_0") {
        return { payload: { items: [{ id: "rt_1", publisherContract: { buildRecipe: { packageInput: { sourceRoot: "agent", targetPath: "/opt/opl" }, outputImageCommand: { entrypoint: ["/opt/opl/bin/runtime"] } } } }] } };
      }
      throw stop;
    },
  };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ status: 200, headers: { get: () => "" } });
  try {
    await assert.rejects(runCustomerPackageEvidenceChain({
      http,
      auth: {},
      wait: async () => {},
      plan: {
        mode: "directory", packageDir, probePath: "agent/provenance/probe.txt",
        achievableEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        requestedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        degradationReason: "",
      },
      packageName: "local-qualification-agent",
      versionLabel: "1.0.0",
      workspaceName: "local qualification",
      provider: "local-docker",
      billingMode: "LOCAL_NO_CHARGE",
      computePlanId: "compute_1",
      storagePlanId: "storage_1",
      sourceSha: "a".repeat(40),
    }), (error) => error === stop);
  } finally {
    globalThis.fetch = originalFetch;
  }
  // The release is on the second page of the catalog, so the chain has to
  // walk the cursor to find it, and it never asks for a release by its id.
  assert.equal(asked.includes("/api/v2/catalog/runtime-versions?cursor="), true);
  assert.equal(asked.includes("/api/v2/catalog/runtime-versions?cursor=rt_0"), true);
  assert.equal(asked.some((path) => path.startsWith("/api/v2/catalog/runtime-versions/")), false);
  assert.equal(archive.entries.length, 3);
});

test("the run follows the Build it asked for to the version that Build registered", async () => {
  // The Build poll is where the run decides whether the job it asked for ever
  // reached a terminal state. A membership test that is not a membership test
  // ends the run with a TypeError instead of the owner's answer, so the chain
  // is driven across that poll here.
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const asked = [];
  const stop = new Error("stop_after_the_capability_version_read");
  let uploaded = null;
  const http = {
    json: async (path, init = {}) => {
      asked.push(path);
      if (path === "/api/v2/namespaces") return { payload: { items: [{ id: "ns_1", isDefault: true }] } };
      if (path === "/api/v2/packages") return { payload: { id: "pkg_1" } };
      if (path === "/api/v2/packages/pkg_1/uploads") {
        uploaded = init.body;
        return { payload: { id: "upload_1", packageVersionId: "pv_1", partSizeBytes: "16777216" } };
      }
      if (path === "/api/v2/uploads/upload_1/parts") return { payload: { url: "https://objects.test/part-1", method: "PUT", contentType: "application/octet-stream" } };
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      if (path === "/api/v2/catalog/build-policy") return { payload: { runtimeVersionId: "rt_1", defaultWebuiVersionId: "wu_1" } };
      if (path === "/api/v2/catalog/runtime-versions?cursor=") {
        return { payload: { items: [{ id: "rt_1", publisherContract: { buildRecipe: { packageInput: { sourceRoot: "agent", targetPath: "/opt/opl" }, outputImageCommand: { entrypoint: ["/opt/opl/bin/runtime"] } } } }] } };
      }
      if (path === "/api/v2/builds") {
        return { payload: { id: "build_1", status: "queued", packageVersionId: "pv_1", runtimeVersionId: "rt_1", webuiVersionId: "wu_1" } };
      }
      if (path === "/api/v2/builds/build_1") {
        return { payload: { id: "build_1", status: "succeeded", stage: "delivered", packageVersionId: "pv_1", runtimeVersionId: "rt_1", webuiVersionId: "wu_1", resultCapabilityVersionId: "cv_1" } };
      }
      throw stop;
    },
  };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ status: 200, headers: { get: () => "" } });
  try {
    await assert.rejects(runCustomerPackageEvidenceChain({
      http,
      auth: {},
      wait: async () => {},
      plan: {
        mode: "directory", packageDir, probePath: "agent/provenance/probe.txt",
        achievableEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        requestedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
        degradationReason: "",
      },
      packageName: "local-qualification-agent",
      versionLabel: "1.0.0",
      workspaceName: "local qualification",
      provider: "local-docker",
      billingMode: "LOCAL_NO_CHARGE",
      computePlanId: "compute_1",
      storagePlanId: "storage_1",
      sourceSha: "a".repeat(40),
    }), (error) => error === stop);
  } finally {
    globalThis.fetch = originalFetch;
  }
  // One Build was asked for, that job was followed, and the Capability Version
  // that job registered was the next thing the run read.
  assert.equal(asked.includes("/api/v2/builds"), true);
  assert.equal(asked.includes("/api/v2/builds/build_1"), true);
  assert.equal(asked.includes("/api/v2/capability-versions/cv_1"), true);
});

test("the customer chain reads the launch boundary the owner declares", async () => {
  // The Workspace owner parks a Local no-charge order at its activation
  // boundary on purpose, and this tool's own Local phase already reads that
  // boundary as the launch. The customer chain has to read the same boundary,
  // or a purchase the owner confirmed never reaches the entry it published.
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const asked = [];
  let uploaded = null;
  let nonce = "";
  const http = {
    json: async (path, init = {}) => {
      asked.push(path);
      if (path === "/api/v2/namespaces") return { payload: { items: [{ id: "ns_1", isDefault: true }] } };
      if (path === "/api/v2/packages") return { payload: { id: "pkg_1" } };
      if (path === "/api/v2/packages/pkg_1/uploads") {
        uploaded = init.body;
        return { payload: { id: "upload_1", packageVersionId: "pv_1", partSizeBytes: "16777216" } };
      }
      if (path === "/api/v2/uploads/upload_1/parts") return { payload: { url: "https://objects.test/part-1", method: "PUT", contentType: "application/octet-stream" } };
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      if (path === "/api/v2/catalog/build-policy") return { payload: { runtimeVersionId: "rt_1", defaultWebuiVersionId: "wu_1" } };
      if (path === "/api/v2/catalog/runtime-versions?cursor=") {
        return { payload: { items: [{ id: "rt_1", publisherContract: { buildRecipe: { packageInput: { sourceRoot: "agent", targetPath: "/opt/opl" }, outputImageCommand: { entrypoint: ["/opt/opl/bin/runtime"] } } } }] } };
      }
      if (path === "/api/v2/builds") {
        return { payload: { id: "build_1", status: "queued", packageVersionId: "pv_1", runtimeVersionId: "rt_1", webuiVersionId: "wu_1" } };
      }
      if (path === "/api/v2/builds/build_1") {
        return { payload: { id: "build_1", status: "succeeded", stage: "delivered", packageVersionId: "pv_1", runtimeVersionId: "rt_1", webuiVersionId: "wu_1", resultCapabilityVersionId: "cv_1" } };
      }
      if (path === "/api/v2/capability-versions/cv_1") {
        return { payload: { id: "cv_1", status: "ready", buildJobId: "build_1", packageVersionId: "pv_1", artifactDigest: `sha256:${"a".repeat(64)}` } };
      }
      if (path === "/api/v2/quotes") {
        return { payload: { id: "quote_1", capabilityVersionId: "cv_1", provider: "local-docker", billingMode: "LOCAL_NO_CHARGE", totalUSDMicros: "0", status: "offered", purpose: "deploy" } };
      }
      if (path === "/api/v2/workspaces") return { payload: { operationId: "op_ws_1", resourceId: "ws_1", status: "accepted" } };
      if (path === "/api/v2/operations/workspace/op_ws_1") {
        return { payload: { operationId: "op_ws_1", resourceId: "ws_1", status: "awaiting_confirmation", stage: "activation", observationResult: "confirmed", errorCode: "DEPENDENCY_UNAVAILABLE" } };
      }
      if (path === "/api/v2/operations/workspace/op_del_1") {
        return { payload: { operationId: "op_del_1", owner: "workspace", kind: "delete_workspace", resourceId: "ws_1", status: "succeeded", stage: "succeeded" } };
      }
      if (path === "/api/v2/workspaces/ws_1" && init.method === "DELETE") {
        return { payload: { operationId: "op_del_1", owner: "workspace", kind: "delete_workspace", resourceId: "ws_1", status: "accepted" } };
      }
      if (path === "/api/v2/workspaces/ws_1") {
        return { payload: { id: "ws_1", status: "provisioning", capabilityVersionId: "cv_1", accessUrl: "http://127.0.0.1:32774/" } };
      }
      throw new Error(`unexpected request ${path}`);
    },
    request: async (path) => {
      asked.push(path);
      if (path === "/api/v2/workspaces/ws_1") return { payload: { id: "ws_1", status: "deleted", accessUrl: "" } };
      throw new Error(`unexpected request ${path}`);
    },
  };
  const readback = {};
  await runCustomerPackageEvidenceChain({
    http,
    auth: {},
    wait: async () => {},
    readback,
    plan: {
      mode: "directory", packageDir, probePath: "agent/provenance/probe.txt",
      achievableEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
      requestedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
      degradationReason: "",
    },
    packageName: "local-qualification-agent",
    versionLabel: "1.0.0",
    workspaceName: "local qualification",
    provider: "local-docker",
    billingMode: "LOCAL_NO_CHARGE",
    computePlanId: "compute_1",
    storagePlanId: "storage_1",
    sourceSha: "a".repeat(40),
    putPart: async (_url, init) => {
      const found = Buffer.from(init.body).toString("latin1")
        .match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
      if (found) nonce = found[0];
      return { status: 200, headers: { get: () => "" } };
    },
    fetchEntry: async () => ({ status: 200, text: nonce }),
  });
  // The owner confirmed the launch at its activation boundary, so the chain
  // read that boundary as the launch, read the Workspace it published, and
  // proved the entry answers with this run's nonce.
  assert.equal(readback.customerOperation.status, "awaiting_confirmation");
  assert.equal(readback.customerOperation.stage, "activation");
  assert.equal(readback.customerOperation.observationResult, "confirmed");
  assert.equal(readback.customerOperation.activationBoundary, true);
  assert.equal(readback.customerWorkspace.capabilityVersionId, "cv_1");
  assert.equal(readback.customerEntry.status, 200);
  assert.equal(readback.customerEntry.matched, true);
  assert.equal(asked.includes("/api/v2/workspaces/ws_1"), true);
});

async function refusesAsync(action, code) {
  await assert.rejects(action, (error) => {
    assert.equal(error?.code, code, `expected ${code}, got ${error?.code}: ${error?.message}`);
    return true;
  });
}

// An application whose runtime declares its own password gate answers an
// unauthenticated request to its entry with a redirect to its login page. The
// chain reads the answer the redirect reaches, and it never follows a
// redirect that leaves the entry's own origin.
function customerChainOwner() {
  const asked = [];
  let uploaded = null;
  const http = {
    json: async (path, init = {}) => {
      asked.push(path);
      if (path === "/api/v2/namespaces") return { payload: { items: [{ id: "ns_1", isDefault: true }] } };
      if (path === "/api/v2/packages") return { payload: { id: "pkg_1" } };
      if (path === "/api/v2/packages/pkg_1/uploads") {
        uploaded = init.body;
        return { payload: { id: "upload_1", packageVersionId: "pv_1", partSizeBytes: "16777216" } };
      }
      if (path === "/api/v2/uploads/upload_1/parts") return { payload: { url: "https://objects.test/part-1", method: "PUT", contentType: "application/octet-stream" } };
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      if (path === "/api/v2/catalog/build-policy") return { payload: { runtimeVersionId: "rt_1", defaultWebuiVersionId: "wu_1" } };
      if (path === "/api/v2/catalog/runtime-versions?cursor=") {
        return { payload: { items: [{ id: "rt_1", publisherContract: { buildRecipe: { packageInput: { sourceRoot: "agent", targetPath: "/opt/opl" }, outputImageCommand: { entrypoint: ["/opt/opl/bin/runtime"] } } } }] } };
      }
      if (path === "/api/v2/builds") return { payload: { id: "build_1", status: "queued", packageVersionId: "pv_1", runtimeVersionId: "rt_1", webuiVersionId: "wu_1" } };
      if (path === "/api/v2/builds/build_1") {
        return { payload: { id: "build_1", status: "succeeded", stage: "delivered", packageVersionId: "pv_1", runtimeVersionId: "rt_1", webuiVersionId: "wu_1", resultCapabilityVersionId: "cv_1" } };
      }
      if (path === "/api/v2/capability-versions/cv_1") {
        return { payload: { id: "cv_1", status: "ready", buildJobId: "build_1", packageVersionId: "pv_1", artifactDigest: `sha256:${"a".repeat(64)}` } };
      }
      if (path === "/api/v2/quotes") {
        return { payload: { id: "quote_1", capabilityVersionId: "cv_1", provider: "local-docker", billingMode: "LOCAL_NO_CHARGE", totalUSDMicros: "0", status: "offered", purpose: "deploy" } };
      }
      if (path === "/api/v2/workspaces") return { payload: { operationId: "op_ws_1", resourceId: "ws_1", status: "accepted" } };
      if (path === "/api/v2/operations/workspace/op_ws_1") {
        return { payload: { operationId: "op_ws_1", resourceId: "ws_1", status: "awaiting_confirmation", stage: "activation", observationResult: "confirmed", errorCode: "DEPENDENCY_UNAVAILABLE" } };
      }
      if (path === "/api/v2/operations/workspace/op_del_1") {
        return { payload: { operationId: "op_del_1", owner: "workspace", kind: "delete_workspace", resourceId: "ws_1", status: "succeeded", stage: "succeeded" } };
      }
      if (path === "/api/v2/workspaces/ws_1" && init.method === "DELETE") {
        return { payload: { operationId: "op_del_1", owner: "workspace", kind: "delete_workspace", resourceId: "ws_1", status: "accepted" } };
      }
      if (path === "/api/v2/workspaces/ws_1") {
        return { payload: { id: "ws_1", status: "provisioning", capabilityVersionId: "cv_1", accessUrl: "http://127.0.0.1:32774/" } };
      }
      throw new Error(`unexpected request ${path}`);
    },
    request: async (path) => {
      asked.push(path);
      if (path === "/api/v2/workspaces/ws_1") return { payload: { id: "ws_1", status: "deleted", accessUrl: "" } };
      throw new Error(`unexpected request ${path}`);
    },
  };
  return { http, asked };
}

// The chain writes its probe into the package and uploads it, so the nonce
// the entry has to answer with is only known after the upload.
async function runChainWithEntry(http, entryResponse) {
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const readback = {};
  let nonce = "";
  await runCustomerPackageEvidenceChain({
    http,
    auth: {},
    wait: async () => {},
    readback,
    plan: {
      mode: "directory", packageDir, probePath: "agent/provenance/probe.txt",
      achievableEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
      requestedEvidenceLevel: CUSTOMER_EVIDENCE_LEVEL_CONTENT,
      degradationReason: "",
    },
    packageName: "local-qualification-agent",
    versionLabel: "1.0.0",
    workspaceName: "local qualification",
    provider: "local-docker",
    billingMode: "LOCAL_NO_CHARGE",
    computePlanId: "compute_1",
    storagePlanId: "storage_1",
    sourceSha: "a".repeat(40),
    putPart: async (_url, init) => {
      const found = Buffer.from(init.body).toString("latin1")
        .match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
      if (found) nonce = found[0];
      return { status: 200, headers: { get: () => "" } };
    },
    fetchEntry: async (url) => entryResponse(url, () => nonce),
  });
  return readback;
}

test("the customer chain follows the entry's own redirect and accepts the answer it reaches", async () => {
  const { http } = customerChainOwner();
  const visited = [];
  const readback = await runChainWithEntry(http, async (url, nonce) => {
    visited.push(url);
    if (url.endsWith("/login")) return { status: 200, text: nonce() };
    return { status: 302, location: "/login", text: "" };
  });
  // The entry answered with a redirect and the application answered at the
  // target. The receipt keeps the first answer, the redirect it declared and
  // the answer the run accepted.
  assert.equal(readback.customerEntry.status, 200);
  assert.equal(readback.customerEntry.firstStatus, 302);
  assert.equal(readback.customerEntry.redirects.length, 1);
  assert.equal(readback.customerEntry.redirects[0].to, "http://127.0.0.1:32774/login");
  assert.equal(visited[0], "http://127.0.0.1:32774/");
  assert.equal(visited[1], "http://127.0.0.1:32774/login");
});

test("the customer chain refuses a redirect that leaves the entry's own origin", async () => {
  const { http } = customerChainOwner();
  const visited = [];
  await refusesAsync(() => runChainWithEntry(http, async (url) => {
    visited.push(url);
    return { status: 302, location: "https://elsewhere.test/login", text: "" };
  }), "local_v2_customer_entry_unreachable");
  // The run asked the entry and stopped: it never carried the session to
  // another host.
  assert.deepEqual(visited, ["http://127.0.0.1:32774/"]);
});

test("the customer chain refuses an entry whose final answer is not 200", async () => {
  const { http } = customerChainOwner();
  await refusesAsync(() => runChainWithEntry(http, async (url) => {
    if (url.endsWith("/login")) return { status: 404, text: "not found" };
    return { status: 302, location: "/login", text: "" };
  }), "local_v2_customer_entry_unreachable");
});

test("the customer chain stops at a redirect that never answers", async () => {
  const { http } = customerChainOwner();
  const visited = [];
  await refusesAsync(() => runChainWithEntry(http, async (url) => {
    visited.push(url);
    return { status: 302, location: "/login", text: "" };
  }), "local_v2_customer_entry_unreachable");
  // The redirect chain is bounded, so a loop cannot keep the run going.
  assert.equal(visited.length <= 7, true);
});

test("a redirect target is only followed inside the entry's own origin", () => {
  assert.equal(customerEntryRedirectTarget("/login", "http://127.0.0.1:32774/"), "http://127.0.0.1:32774/login");
  assert.equal(customerEntryRedirectTarget("", "http://127.0.0.1:32774/"), "");
  assert.equal(customerEntryRedirectTarget("https://elsewhere.test/login", "http://127.0.0.1:32774/"), "");
  assert.equal(customerEntryRedirectTarget("http://127.0.0.1:9999/login", "http://127.0.0.1:32774/"), "");
});
