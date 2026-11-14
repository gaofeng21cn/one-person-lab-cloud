import assert from "node:assert/strict";
import test from "node:test";

import {
  buildPolicyRequest,
  buildReceipt,
  buildRuntimeRegistrationRequest,
  buildWebuiRegistrationRequest,
  contractRefusals,
  publishRuntimePolicy,
  verifyEffectivePolicy
} from "../../tools/publish-runtime-policy.ts";

const digest = `sha256:${"a".repeat(64)}`;
const builderDigest = `sha256:${"b".repeat(64)}`;
const recipeDigest = `sha256:${"c".repeat(64)}`;

function runtimeContract(overrides: Record<string, unknown> = {}) {
  return {
    schemaVersion: "opl-publisher-contract/v1",
    publisherNamespaceId: "pub_official",
    kind: "runtime",
    runtimeAbiVersion: "opl-runtime/v1",
    packageFormatVersions: ["opl_packages.v1"],
    image: { repository: "ghcr.io/gaofeng21cn/one-person-lab-app", digest, platform: { os: "linux", architecture: "arm64" } },
    buildRecipe: {
      version: "opl-build-recipe/v1",
      frontend: { repository: "ghcr.io/gaofeng21cn/one-person-lab-app", digest: builderDigest, platform: { os: "linux", architecture: "arm64" } },
      recipe: { repository: "ghcr.io/gaofeng21cn/opl-recipes", digest: recipeDigest, mediaType: "application/vnd.opl.build-recipe.v1+tar", dockerfilePath: "Dockerfile" },
      runtimeContextName: "runtime",
      packageInput: { contextName: "agent_package", formatVersion: "opl_packages.v1", sourceRoot: "package", targetPath: "/opt/opl/agent", uid: 1000, gid: 1000 },
      webuiInput: { contextName: "webui", sourcePath: "/web", targetPath: "/opt/opl/web", uid: 1000, gid: 1000 },
      networkPolicy: "none",
      outputPlatform: { os: "linux", architecture: "arm64" },
      outputImageCommand: { entrypoint: ["/opt/opl/runtime/entrypoint"], cmd: [] }
    },
    ...overrides
  };
}

function webuiContract(overrides: Record<string, unknown> = {}) {
  return {
    schemaVersion: "opl-publisher-contract/v1",
    publisherNamespaceId: "pub_official",
    kind: "webui",
    runtimeAbiVersions: ["opl-runtime/v1"],
    uiProtocolVersion: "opl-webui/v1",
    integrationMode: "bundled_static",
    staticRoot: "/web",
    entryFile: "index.html",
    assetBasePath: "/assets/",
    apiBasePath: "/api/",
    authenticationProtocol: "opl-application-session/v1",
    supportsSse: true,
    supportsWebsocket: false,
    image: { repository: "ghcr.io/gaofeng21cn/one-person-lab-webui", digest, platform: { os: "linux", architecture: "arm64" } },
    ...overrides
  };
}

function reasons(refusals: { reason: string }[]): string[] {
  return refusals.map((refusal) => refusal.reason);
}

test("a complete runtime contract carries no refusal", () => {
  assert.deepEqual(contractRefusals(runtimeContract(), "runtime"), []);
});

test("a mutable image tag is refused by name", () => {
  const refusals = contractRefusals(
    runtimeContract({ image: { repository: "ghcr.io/gaofeng21cn/one-person-lab-app", digest: "latest", platform: { os: "linux", architecture: "arm64" } } }),
    "runtime"
  );
  assert.ok(reasons(refusals).includes("publisher_image_digest_not_immutable"));
});

test("a missing build recipe is refused instead of being derived", () => {
  const contract = runtimeContract();
  delete (contract as Record<string, unknown>).buildRecipe;
  const refusals = contractRefusals(contract, "runtime");
  assert.ok(reasons(refusals).includes("build_recipe_missing"));
});

test("a recipe whose package format the runtime does not declare is refused", () => {
  const contract = runtimeContract();
  const recipe = contract.buildRecipe as Record<string, unknown>;
  recipe.packageInput = { ...(recipe.packageInput as Record<string, unknown>), formatVersion: "agent-plugins/1.0.0" };
  const refusals = contractRefusals(contract, "runtime");
  assert.ok(reasons(refusals).includes("build_recipe_package_format_not_declared_by_runtime"));
});

test("a webui contract is not accepted as a runtime contract", () => {
  assert.ok(reasons(contractRefusals(webuiContract(), "runtime")).includes("publisher_contract_kind_mismatch"));
});

test("the registration request carries the upstream contract bytes unchanged", () => {
  const contract = runtimeContract();
  const request = buildRuntimeRegistrationRequest(contract, "receipt-1", "opl-app", "v1");
  assert.equal(request.publisherNamespaceId, "pub_official");
  assert.equal(request.admissionReceiptId, "receipt-1");
  assert.deepEqual(request.publisherContract, contract);
});

test("the webui registration request carries the webui contract", () => {
  const contract = webuiContract();
  const request = buildWebuiRegistrationRequest(contract, "receipt-2", "opl-webui", "v1");
  assert.equal(request.admissionReceiptId, "receipt-2");
  assert.equal(request.publisherContract.kind, "webui");
});

test("the policy names both registered catalog entries", () => {
  assert.deepEqual(buildPolicyRequest("runtime-1", "webui-1"), {
    runtimeVersionId: "runtime-1",
    defaultWebuiVersionId: "webui-1"
  });
});

test("a policy pointing at another runtime is refused", () => {
  const refusals = verifyEffectivePolicy(
    { runtimeVersionId: "runtime-other", defaultWebuiVersionId: "webui-1", policyVersion: "policy-1", effectiveAt: new Date().toISOString() },
    { runtimeVersionId: "runtime-1", webuiVersionId: "webui-1" }
  );
  assert.ok(reasons(refusals).includes("effective_policy_runtime_mismatch"));
});

test("a policy that is not effective yet is refused", () => {
  const future = new Date(Date.now() + 60_000).toISOString();
  const refusals = verifyEffectivePolicy(
    { runtimeVersionId: "runtime-1", defaultWebuiVersionId: "webui-1", policyVersion: "policy-1", effectiveAt: future },
    { runtimeVersionId: "runtime-1", webuiVersionId: "webui-1" }
  );
  assert.ok(reasons(refusals).includes("effective_policy_not_yet_effective"));
});

type Call = { url: string; method: string; body: unknown; headers: Record<string, string> };

function fakeConsole(options: { webuiStatus?: number; policyRuntimeId?: string } = {}) {
  const calls: Call[] = [];
  const impl = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const headers = (init?.headers ?? {}) as Record<string, string>;
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    calls.push({ url, method, body, headers });
    const cookie = "opl_console_session=session-1";
    if (url.endsWith("/api/v2/auth/session")) {
      return new Response(JSON.stringify({ csrfToken: "csrf-1", actorId: "actor-1" }), { status: 200, headers: { "set-cookie": cookie } });
    }
    if (url.endsWith("/api/v2/auth/login")) {
      return new Response(JSON.stringify({ csrfToken: "csrf-2", actorId: "actor-1" }), { status: 200, headers: { "set-cookie": cookie } });
    }
    if (url.endsWith("/api/v2/admin/catalog/runtime-versions")) {
      return new Response(JSON.stringify({ id: "runtime-1" }), { status: 200, headers: { "content-type": "application/json" } });
    }
    if (url.endsWith("/api/v2/admin/catalog/webui-versions")) {
      if (options.webuiStatus) return new Response("webui rejected", { status: options.webuiStatus });
      return new Response(JSON.stringify({ id: "webui-1" }), { status: 200, headers: { "content-type": "application/json" } });
    }
    if (url.endsWith("/api/v2/admin/catalog/build-policy")) {
      return new Response(JSON.stringify({ policyVersion: "policy-1" }), { status: 200, headers: { "content-type": "application/json" } });
    }
    if (url.endsWith("/api/v2/catalog/build-policy")) {
      return new Response(
        JSON.stringify({
          runtimeVersionId: options.policyRuntimeId ?? "runtime-1",
          defaultWebuiVersionId: "webui-1",
          policyVersion: "policy-1",
          effectiveAt: new Date().toISOString()
        }),
        { status: 200, headers: { "content-type": "application/json" } }
      );
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;
  return { calls, impl };
}

function publishOptions(fetchImpl: typeof fetch) {
  return {
    base: "https://localhost:8443",
    runtimeContract: runtimeContract(),
    webuiContract: webuiContract(),
    runtimeAdmissionReceiptId: "receipt-runtime",
    webuiAdmissionReceiptId: "receipt-webui",
    runtimeName: "opl-app",
    runtimeVersionLabel: "v1",
    webuiName: "opl-webui",
    webuiVersionLabel: "v1",
    email: "admin@example.test",
    password: "secret",
    fetchImpl
  };
}

test("publishing registers both catalog entries and then the policy", async () => {
  const { calls, impl } = fakeConsole();
  const result = await publishRuntimePolicy(publishOptions(impl));
  assert.equal(result.published, true);
  assert.equal(result.runtimeVersionId, "runtime-1");
  assert.equal(result.webuiVersionId, "webui-1");

  const writePaths = calls
    .filter((call) => call.method !== "GET" && new URL(call.url).pathname.startsWith("/api/v2/admin/catalog/"))
    .map((call) => new URL(call.url).pathname);
  assert.deepEqual(writePaths, [
    "/api/v2/admin/catalog/runtime-versions",
    "/api/v2/admin/catalog/webui-versions",
    "/api/v2/admin/catalog/build-policy"
  ]);

  const runtimeCall = calls.find((call) => call.url.endsWith("/runtime-versions"));
  assert.equal(runtimeCall?.method, "POST");
  assert.ok(runtimeCall?.headers["idempotency-key"]);
  assert.ok(runtimeCall?.headers["x-csrf-token"]);

  const policyCall = calls.find((call) => call.url.endsWith("/build-policy") && call.method === "PUT");
  assert.deepEqual(policyCall?.body, { runtimeVersionId: "runtime-1", defaultWebuiVersionId: "webui-1" });
});

test("the policy is not written when the webui registration is refused", async () => {
  const { calls, impl } = fakeConsole({ webuiStatus: 400 });
  await assert.rejects(() => publishRuntimePolicy(publishOptions(impl)));
  assert.equal(calls.some((call) => call.url.endsWith("/build-policy") && call.method === "PUT"), false);
});

test("a policy that points at another runtime is not published", async () => {
  const { impl } = fakeConsole({ policyRuntimeId: "runtime-other" });
  const result = await publishRuntimePolicy(publishOptions(impl));
  assert.equal(result.published, false);
  assert.ok(reasons(result.refusals).includes("effective_policy_runtime_mismatch"));
});

test("a missing admission receipt is refused before any call is made", async () => {
  const { calls, impl } = fakeConsole();
  const result = await publishRuntimePolicy({ ...publishOptions(impl), runtimeAdmissionReceiptId: "" });
  assert.equal(result.published, false);
  assert.ok(reasons(result.refusals).includes("runtime_admission_receipt_missing"));
  assert.equal(calls.length, 0);
});

test("a webui that does not declare the runtime ABI is refused before any call", async () => {
  const { calls, impl } = fakeConsole();
  const webui = webuiContract({ runtimeAbiVersions: ["opl-runtime/v9"] });
  const result = await publishRuntimePolicy({ ...publishOptions(impl), webuiContract: webui });
  assert.equal(result.published, false);
  assert.ok(reasons(result.refusals).includes("webui_does_not_declare_runtime_abi"));
  assert.equal(calls.length, 0);
});

test("the receipt records the contract digests and the published ids", () => {
  const receipt = buildReceipt(
    { published: true, runtimeVersionId: "runtime-1", webuiVersionId: "webui-1", policyVersion: "policy-1", refusals: [] },
    "https://localhost:8443",
    runtimeContract(),
    webuiContract()
  );
  assert.equal(receipt.schema, "opl.cloud.catalog-policy-publication.v1");
  assert.equal(receipt.runtimeVersionId, "runtime-1");
  assert.match(receipt.runtimeContractDigest, /^sha256:[0-9a-f]{64}$/);
  assert.match(receipt.webuiContractDigest, /^sha256:[0-9a-f]{64}$/);
});
