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
    resourceId: "package-version-1", status: "succeeded", observationResult: "confirmed",
  };
  assert.deepEqual(assertCustomerCompleteUploadOperation(operation, { packageVersionId: "package-version-1" }), {
    owner: "capability", kind: "complete_upload", resourceId: "package-version-1",
    status: "succeeded", observationResult: "confirmed", operationId: "op-1",
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
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
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

test("the uploaded bytes are written through the requester the run supplies", async () => {
  // The object origin is served by the installation's own CA. A request that
  // trusted the host store would fail there, so the run's own requester writes
  // the part and the host-trusting default is never used.
  const packageDir = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));
  const archive = await buildCustomerPackageArchive({
    packageDir, probePath: "agent/provenance/probe.txt", probeContent: "nonce",
  });
  const written = [];
  const stop = new Error("stop_after_the_upload_completion");
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
      if (path === "/api/v2/uploads/upload_1/complete") return { payload: { owner: "capability", kind: "complete_upload", resourceId: "pv_1", status: "succeeded", observationResult: "confirmed", operationId: "op_upload" } };
      if (path === "/api/v2/package-versions/pv_1") return { payload: { id: "pv_1", packageId: "pkg_1", versionLabel: "1.0.0", sha256: uploaded?.sha256, sizeBytes: uploaded?.sizeBytes, status: "uploaded" } };
      throw stop;
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
      putPart: async (url, init) => {
        written.push({ url, method: init.method, digest: `sha256:${createHash("sha256").update(init.body).digest("hex")}` });
        return { status: 200, headers: { get: () => "" } };
      },
    }), (error) => error === stop || error?.message === "the read back package version does not match the uploaded bytes");
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.equal(written.length, 1);
  assert.equal(written[0].url, "https://objects.test/part-1");
  assert.equal(written[0].method, "PUT");
  // The bytes the owner was promised are the bytes that were written.
  assert.equal(written[0].digest, uploaded.sha256);
});
