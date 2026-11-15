import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  buildCustomerAgentPackageArchive,
  crc32,
  evaluateAgentBranchSelection,
  evaluateArtifactIdentity,
  evaluateContentProvenance,
  evaluateCustomerAgentCriteria,
  parseCustomerAgentLifecycleArgs,
  pollSchedule,
  registryManifestUrl,
  runCustomerAgentLifecycle,
  validateCustomerAgentLifecycleReceipt,
} from "../../tools/local-customer-agent-lifecycle.ts";

const toolSource = new URL("../../tools/local-customer-agent-lifecycle.ts", import.meta.url);
const fixtureDirectory = fileURLToPath(new URL("./fixtures/customer-agent-package", import.meta.url));

const buildDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111";
const otherDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222";
const registryDigest = "sha256:3333333333333333333333333333333333333333333333333333333333333333";
const buildRepository = "127.0.0.1:5000/opl-qualification/agent";

/*
 * The installation the run reads. The tool reads the declaration from disk,
 * so every scenario writes its own.
 */
const installationEnv = (extra = {}) => [
  "OPL_PUBLIC_URL=https://localhost:8443",
  "OPL_PKI_DIR=/tmp/opl-customer-agent-pki",
  "OPL_QUALIFICATION_USER_EMAIL=customer@example.test",
  "OPL_QUALIFICATION_USER_PASSWORD=customer-secret",
  'OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON={"profileId":"local-docker","accountBindings":[{"tenantId":"tenant-1","accountId":"local-host"}]}',
  ...Object.entries(extra).map(([name, value]) => `${name}=${value}`),
  "",
].join("\n");

/*
 * The owner the run talks to.
 *
 * The fake answers the routes the chain uses and records what it was asked, so
 * a scenario can break one owner without touching the rest of the chain.
 */
const ownerHTTP = (scenario = {}) => {
  const state = {
    builds: scenario.builds ?? [{ id: "build-1", packageVersionId: "pv-1", status: "succeeded", artifactDigest: buildDigest }],
    capabilityVersions: scenario.capabilityVersions ?? [{
      id: "cap-1",
      packageId: "pkg-1",
      packageVersionId: "pv-1",
      buildJobId: "build-1",
      status: "ready",
      artifact: { repository: buildRepository, digest: buildDigest },
    }],
    workspace: scenario.workspace ?? {
      id: "ws-1",
      name: "customer-agent-workspace",
      status: "running",
      applicationAvailability: "available",
      accessUrl: "http://127.0.0.1:31000/",
      capabilityVersionId: "cap-1",
      runtimeVersionId: "",
    },
    partUrl: scenario.partUrl || "",
  };
  const cookies = {
    context: ["opl_login_context=challenge; Path=/api/v2/auth", "opl_login_csrf=challenge-hash; Path=/api/v2/auth"],
    session: ["opl_console_session=owner-session; Path=/"],
  };
  const answer = (path, init = {}) => {
    const route = path.split("?")[0];
    const method = init.method || "GET";
    if (route === "/api/v2/auth/context") {
      return { status: 200, payload: { csrfToken: "login-csrf" }, cookies: cookies.context };
    }
    if (route === "/api/v2/auth/login") {
      return { status: 200, payload: { actorId: "41", displayName: "customer", tenantId: "tenant-1", csrfToken: "owner-csrf" }, cookies: cookies.session };
    }
    if (route === "/api/v2/auth/external-bindings") {
      return { status: 200, payload: { actorId: "41", registryHost: "127.0.0.1:5000", registryNamespace: "opl-qualification", repository: "agent" } };
    }
    if (route === "/api/v2/namespaces") {
      return { status: 200, payload: { items: [{ id: "ns-1", name: "customer", isDefault: true, status: "active" }] } };
    }
    if (route === "/api/v2/packages") {
      return { status: 200, payload: { id: "pkg-1", namespaceId: "ns-1", name: "customer-agent-fixture" } };
    }
    if (route.endsWith("/uploads")) {
      return { status: 200, payload: { id: "upload-1", packageVersionId: "pv-1", partSizeBytes: 16 * 1024 * 1024 } };
    }
    if (route.endsWith("/parts")) {
      return {
        status: 200,
        payload: {
          url: state.partUrl,
          method: "PUT",
          contentType: "application/octet-stream",
          requiredChecksumHeaderName: "X-OPL-SHA256",
          requiredChecksumHeaderValue: String(init.body?.sha256 || ""),
        },
      };
    }
    if (route.endsWith("/complete")) {
      return { status: 200, payload: { operationId: "op-upload", owner: "capability", kind: "complete_upload", resourceId: "pv-1", status: "succeeded" } };
    }
    if (route === "/api/v2/builds") {
      return { status: 200, payload: { items: state.builds } };
    }
    if (route.startsWith("/api/v2/builds/")) {
      const found = state.builds.find((item) => item.id === route.split("/").pop()) || null;
      if (!found) return { status: 404, payload: { code: "not_found" } };
      return { status: 200, payload: { ...found, status: "succeeded", artifactDigest: found.artifactDigest || buildDigest } };
    }
    if (route === "/api/v2/capability-versions") {
      return { status: 200, payload: { items: state.capabilityVersions } };
    }
    if (route.startsWith("/api/v2/capability-versions/")) {
      const found = state.capabilityVersions.find((item) => item.id === route.split("/").pop()) || null;
      if (!found) return { status: 404, payload: { code: "not_found" } };
      return { status: 200, payload: found };
    }
    if (route === "/api/v2/catalog/compute-plans") {
      return { status: 200, payload: { items: [{ id: "compute-1", name: "local-v2-compute" }] } };
    }
    if (route === "/api/v2/catalog/storage-plans") {
      return { status: 200, payload: { items: [{ id: "storage-1", name: "local-v2-storage" }] } };
    }
    if (route === "/api/v2/quotes") {
      return { status: 200, payload: { quoteId: "quote-1", status: "accepted" } };
    }
    if (route === "/api/v2/workspaces" && method === "POST") {
      return { status: 202, payload: { id: "ws-1", status: "provisioning" } };
    }
    if (route === "/api/v2/workspaces/ws-1/deletion") {
      return {
        status: 200,
        payload: {
          workspaceId: "ws-1",
          operationId: "op-delete",
          resourceDeletionStatus: "confirmed",
          dataDeletionStatus: "confirmed",
          refundStatus: "not_applicable",
          deletionReceipt: { id: "receipt-1", kind: "provider_action", owner: "workspace", operationId: "op-delete", outcome: "confirmed", createdAt: "2026-01-01T00:00:00Z" },
        },
      };
    }
    if (route.startsWith("/api/v2/workspaces/") && method === "DELETE") {
      state.deleted = true;
      return { status: 202, payload: { operationId: "op-delete", owner: "workspace", kind: "delete_workspace", resourceId: "ws-1", status: "accepted" } };
    }
    if (route === "/api/v2/workspaces/ws-1") {
      if (state.deleted) {
        return { status: 200, payload: { id: "ws-1", status: "deleted", accessUrl: "", capabilityVersionId: "", runtimeVersionId: "" } };
      }
      return { status: 200, payload: state.workspace };
    }
    if (route.startsWith("/api/v2/operations/workspace/")) {
      return {
        status: 200,
        payload: { operationId: "op-delete", owner: "workspace", kind: "delete_workspace", resourceId: "ws-1", status: "succeeded", stage: "released" },
      };
    }
    return { status: 404, payload: { code: "unknown_route", path: route } };
  };
  const build = (path, init = {}) => {
    const result = answer(path, init);
    const headers = new Headers();
    for (const cookie of result.cookies || []) headers.append("set-cookie", cookie);
    const text = result.payload === null || result.payload === undefined ? "" : JSON.stringify(result.payload);
    return {
      response: {
        status: result.status,
        headers: { get: (name) => headers.get(name), getSetCookie: () => headers.getSetCookie() },
      },
      payload: result.payload === undefined ? null : result.payload,
      text,
    };
  };
  return {
    origin: "https://localhost:8443",
    state,
    request: async (path, init = {}) => build(path, init),
    json: async (path, init = {}, auth = null, statuses = [200]) => {
      const result = build(path, init);
      if (!statuses.includes(result.response.status) || result.payload === null) {
        throw Object.assign(new Error(`HTTP ${init.method || "GET"} ${path} returned ${result.response.status}`), { status: result.response.status });
      }
      return result;
    },
  };
};

const clock = (step = 1000) => {
  let value = 0;
  return { now: () => (value += step) };
};

/*
 * The object store the owner authorized.
 *
 * The store keeps the bytes it received, so the scenario can read the probe
 * the run minted out of the uploaded package itself: the archive is stored
 * uncompressed, so the nonce is in the bytes the store holds.
 */
const partServer = async (received, state) => {
  const server = createServer((request, response) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      const body = Buffer.concat(chunks);
      const nonce = body.toString("latin1").match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/);
      if (nonce) state.nonce = nonce[0];
      received.push({ method: request.method, bytes: body.length, checksum: request.headers["x-opl-sha256"] });
      response.writeHead(204);
      response.end();
    });
  });
  await new Promise((resolvePromise) => server.listen(0, "127.0.0.1", resolvePromise));
  const address = server.address();
  return { server, url: `http://127.0.0.1:${address.port}/parts?permit=permit` };
};

const runScenario = async (scenario = {}, options = {}) => {
  const directory = await mkdtemp(join(tmpdir(), "opl-customer-agent-"));
  const envFile = join(directory, "installation.env");
  await writeFile(envFile, installationEnv());
  const received = [];
  const uploaded = { nonce: "" };
  const parts = await partServer(received, uploaded);
  const http = ownerHTTP({ ...scenario, partUrl: scenario.partUrl || parts.url });
  const receipt = await runCustomerAgentLifecycle({
    mode: "local-v2",
    sourceSha: "0123456789abcdef0123456789abcdef01234567",
    installationEnv: envFile,
    packageDir: fixtureDirectory,
    probePath: "provenance/probe.txt",
    buildTimeoutMs: 1000,
    registrationTimeoutMs: 1000,
    ...options,
  }, {
    http,
    wait: async () => {},
    now: (scenario.now || clock(1000)).now,
    fetchEntry: scenario.fetchEntry || (async () => ({ status: 200, text: JSON.stringify({ probe: uploaded.nonce }) })),
    readManifest: async () => ({ url: "http://127.0.0.1:5000/v2/opl-qualification/agent/manifests/sha256", status: 200, digest: scenario.registryDigest ?? buildDigest }),
    residualCounts: async () => ({ containers: 0, volumes: 0, networks: 0 }),
  });
  await new Promise((resolvePromise) => parts.server.close(resolvePromise));
  return { receipt, http, received, uploaded };
};

test("the tool names no agent, no package and no expected response content", async () => {
  const source = await readFile(toolSource, "utf8");
  for (const forbidden of ["stage3-local-agent", "OPL Workspace READY", "customer-agent-fixture"]) {
    assert.equal(source.includes(forbidden), false, `the tool must not carry the literal ${forbidden}`);
  }
});

test("the uploaded archive carries the run's nonce in the probe file", async () => {
  const archive = await buildCustomerAgentPackageArchive({
    packageDir: fixtureDirectory,
    probePath: "provenance/probe.txt",
    probeContent: "nonce-of-this-run",
  });
  assert.deepEqual(archive.entries, ["bin/runtime", "manifest.json", "provenance/probe.txt"]);
  assert.match(archive.digest, /^sha256:[0-9a-f]{64}$/);
  assert.equal(archive.manifest.name, "customer-agent-fixture");
  const text = archive.bytes.toString("latin1");
  assert.equal(text.includes("nonce-of-this-run"), true, "the probe file carries the nonce");
  assert.equal(text.includes("placeholder-probe"), false, "the placeholder probe is replaced");
});

test("the archive is written with the checksum a standard reader verifies", () => {
  assert.equal(crc32(Buffer.from("")), 0);
  assert.equal(crc32(Buffer.from("123456789")), 0xcbf43926);
});

test("layer A is red when the purchase is not on a customer agent branch", () => {
  const noCapability = evaluateAgentBranchSelection({ capabilityVersionId: "", runtimeVersionId: "" });
  assert.equal(noCapability.pass, false);
  assert.equal(noCapability.code, "customer_agent_agent_branch_not_selected");
  const platformBranch = evaluateAgentBranchSelection({ capabilityVersionId: "cap-1", runtimeVersionId: "runtime-1" });
  assert.equal(platformBranch.pass, false);
  assert.equal(platformBranch.code, "customer_agent_platform_branch_selected");
  const agentBranch = evaluateAgentBranchSelection({ capabilityVersionId: "cap-1", runtimeVersionId: "" });
  assert.equal(agentBranch.pass, true);
});

test("layer A is red when the registered artifact is not the build output", () => {
  const otherArtifact = evaluateArtifactIdentity({
    buildArtifact: `${buildRepository}@${buildDigest}`,
    capabilityArtifact: `${buildRepository}@${otherDigest}`,
    registryManifestDigest: buildDigest,
  });
  assert.equal(otherArtifact.pass, false);
  assert.equal(otherArtifact.code, "customer_agent_artifact_identity_mismatch");
  const otherRegistry = evaluateArtifactIdentity({
    buildArtifact: `${buildRepository}@${buildDigest}`,
    capabilityArtifact: `${buildRepository}@${buildDigest}`,
    registryManifestDigest: registryDigest,
  });
  assert.equal(otherRegistry.pass, false);
  assert.equal(otherRegistry.code, "customer_agent_artifact_identity_mismatch");
  const matching = evaluateArtifactIdentity({
    buildArtifact: `${buildRepository}@${buildDigest}`,
    capabilityArtifact: `${buildRepository}@${buildDigest}`,
    registryManifestDigest: buildDigest,
  });
  assert.equal(matching.pass, true);
});

test("layer B is red when the entry answers with another run's bytes", () => {
  const foreign = evaluateContentProvenance({ nonce: "nonce-of-this-run", entryText: `{"probe":"nonce-of-another-run"}` });
  assert.equal(foreign.pass, false);
  assert.equal(foreign.code, "customer_agent_content_provenance_not_observed");
  const own = evaluateContentProvenance({ nonce: "nonce-of-this-run", entryText: `{"probe":"nonce-of-this-run"}` });
  assert.equal(own.pass, true);
});

test("the criteria summary needs the structure and the content", () => {
  const branch = evaluateAgentBranchSelection({ capabilityVersionId: "cap-1", runtimeVersionId: "" });
  const artifact = evaluateArtifactIdentity({
    buildArtifact: `${buildRepository}@${buildDigest}`,
    capabilityArtifact: `${buildRepository}@${buildDigest}`,
    registryManifestDigest: buildDigest,
  });
  const content = evaluateContentProvenance({ nonce: "n", entryText: "n" });
  assert.equal(evaluateCustomerAgentCriteria({ branch, artifact, content }).pass, true);
  assert.equal(evaluateCustomerAgentCriteria({ branch, artifact, content: { pass: false } }).structurePass, true);
});

test("the polling backoff grows and is capped", () => {
  const delays = pollSchedule(6, 1000, 4000);
  assert.deepEqual(delays, [1000, 2000, 4000, 4000, 4000, 4000]);
  assert.deepEqual(pollSchedule(0, 1000, 4000), []);
});

test("the receipt validator refuses a run that reports a pass it did not earn", () => {
  const base = {
    schemaVersion: 1,
    kind: "opl.customer-agent-lifecycle-receipt.v1",
    status: "passed",
    evidenceLevel: "content-provenance",
    digests: {
      packageArchive: buildDigest,
      buildArtifact: `${buildRepository}@${buildDigest}`,
      registryManifestDigest: buildDigest,
      capabilityArtifact: `${buildRepository}@${buildDigest}`,
    },
    criteria: {
      agentBranch: { pass: true },
      artifactIdentity: { pass: true },
      contentProvenance: { pass: true },
    },
    degradation: null,
    blocked: null,
  };
  assert.equal(validateCustomerAgentLifecycleReceipt(base).valid, true);
  const degraded = { ...base, status: "degraded", evidenceLevel: "artifact-identity" };
  assert.equal(validateCustomerAgentLifecycleReceipt(degraded).valid, false, "a degraded run must carry its reason");
  const withReason = { ...degraded, degradation: { code: "customer_agent_content_provenance_not_observed", detail: "the package did not echo the probe file" } };
  assert.equal(validateCustomerAgentLifecycleReceipt(withReason).valid, true);
  const passedOnStructure = { ...base, criteria: { ...base.criteria, contentProvenance: { pass: false } } };
  assert.equal(validateCustomerAgentLifecycleReceipt(passedOnStructure).valid, false);
  const blocked = { ...base, status: "blocked", evidenceLevel: "" };
  assert.equal(validateCustomerAgentLifecycleReceipt(blocked).valid, false, "a blocked run must name the step it stopped at");
});

test("the arguments name the package under test and nothing else", () => {
  const options = parseCustomerAgentLifecycleArgs([
    "--source-sha", "0123456789abcdef0123456789abcdef01234567",
    "--installation-env", "/tmp/installation.env",
    "--package-dir", "/tmp/package",
    "--probe-path", "provenance/probe.txt",
    "--expected-evidence-level", "content-provenance",
  ]);
  assert.equal(options.packageDir, "/tmp/package");
  assert.equal(options.probePath, "provenance/probe.txt");
  assert.equal(options.expectedEvidenceLevel, "content-provenance");
  assert.throws(() => parseCustomerAgentLifecycleArgs(["--unknown", "value"]), /unknown customer agent lifecycle argument/);
  assert.throws(() => parseCustomerAgentLifecycleArgs(["--source-sha", "short"]), /40-character product commit/);
});

test("the registry manifest URL names the repository the customer bound", () => {
  assert.equal(
    registryManifestUrl(buildRepository, buildDigest),
    `http://127.0.0.1:5000/v2/opl-qualification/agent/manifests/${buildDigest}`,
  );
  assert.throws(() => registryManifestUrl(buildRepository, "not-a-digest"), /immutable reference/);
});

test("the whole chain passes when the owner, the registry and the entry agree", async () => {
  const { receipt, uploaded } = await runScenario();
  assert.equal(receipt.status, "passed", JSON.stringify(receipt.blocked || receipt.degradation || ""));
  assert.notEqual(uploaded.nonce, "", "the run minted a nonce and wrote it into the uploaded package");
  assert.equal(receipt.evidenceLevel, "content-provenance");
  assert.equal(receipt.criteria.agentBranch.pass, true);
  assert.equal(receipt.criteria.artifactIdentity.pass, true);
  assert.equal(receipt.criteria.contentProvenance.pass, true);
  assert.equal(receipt.digests.buildArtifact, `${buildRepository}@${buildDigest}`);
  assert.equal(receipt.digests.capabilityArtifact, `${buildRepository}@${buildDigest}`);
  assert.equal(receipt.digests.registryManifestDigest, buildDigest);
  assert.equal(validateCustomerAgentLifecycleReceipt(receipt).valid, true);
});

test("the chain is blocked and says so when the platform starts no build", async () => {
  const { receipt } = await runScenario({ builds: [] });
  assert.equal(receipt.status, "blocked");
  assert.equal(receipt.blocked.step, "automatic_build");
  assert.equal(receipt.blocked.code, "customer_agent_automatic_build_not_triggered");
});

test("the chain is blocked when the platform registers no capability version", async () => {
  const { receipt } = await runScenario({ capabilityVersions: [] });
  assert.equal(receipt.status, "blocked");
  assert.equal(receipt.blocked.step, "capability_registration");
  assert.equal(receipt.blocked.code, "customer_agent_capability_version_not_registered");
});

test("the chain is red when the registered artifact is not the build output", async () => {
  const { receipt } = await runScenario({
    capabilityVersions: [{
      id: "cap-1",
      packageId: "pkg-1",
      packageVersionId: "pv-1",
      buildJobId: "build-1",
      status: "ready",
      artifact: { repository: buildRepository, digest: otherDigest },
    }],
  });
  assert.equal(receipt.status, "failed");
  assert.equal(receipt.blocked.code, "customer_agent_artifact_identity_mismatch");
  assert.equal(receipt.criteria.artifactIdentity.pass, false);
});

test("the chain is red when the purchase is on the platform application branch", async () => {
  const { receipt } = await runScenario({
    workspace: {
      id: "ws-1",
      status: "running",
      applicationAvailability: "available",
      accessUrl: "http://127.0.0.1:31000/",
      capabilityVersionId: "cap-1",
      runtimeVersionId: "runtime-1",
    },
  });
  assert.equal(receipt.status, "failed");
  assert.equal(receipt.blocked.code, "customer_agent_platform_branch_selected");
  assert.equal(receipt.criteria.agentBranch.pass, false);
});

test("a package that does not echo the probe degrades the run and never reports a pass", async () => {
  const { receipt } = await runScenario({
    fetchEntry: async () => ({ status: 200, text: `{"probe":"a-different-answer"}` }),
  });
  assert.equal(receipt.status, "degraded");
  assert.notEqual(receipt.status, "passed");
  assert.equal(receipt.evidenceLevel, "artifact-identity");
  assert.equal(receipt.criteria.contentProvenance.pass, false);
  assert.equal(receipt.degradation.code, "customer_agent_content_provenance_not_observed");
  assert.match(receipt.degradation.detail, /probe file/);
  assert.equal(validateCustomerAgentLifecycleReceipt(receipt).valid, true);
});

test("a caller that requires the content provenance gets a red run, not a degraded one", async () => {
  const { receipt } = await runScenario({
    fetchEntry: async () => ({ status: 200, text: `{"probe":"a-different-answer"}` }),
  }, { expectedEvidenceLevel: "content-provenance" });
  assert.equal(receipt.status, "failed");
  assert.equal(receipt.blocked.code, "customer_agent_content_provenance_not_observed");
  assert.equal(receipt.evidenceLevel, "artifact-identity");
});

test("the part upload reaches the object store the owner authorized", async () => {
  const { receipt, received } = await runScenario();
  assert.equal(receipt.status, "passed", JSON.stringify(receipt.blocked || ""));
  assert.equal(received.length, 1);
  assert.equal(received[0].method, "PUT");
  assert.ok(received[0].bytes > 0);
  assert.match(String(received[0].checksum), /^sha256:[0-9a-f]{64}$/);
});
