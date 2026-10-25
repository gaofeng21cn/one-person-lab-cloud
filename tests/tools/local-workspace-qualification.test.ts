import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, lstat, mkdir, mkdtemp, readFile, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  continueWorkspaceDelete,
  assertLocalV2ComposeEnvironment,
  assertWorkspaceQualificationTopology,
  deleteWorkspaceOnOwnerRoute,
  readWorkspaceRefundReceipt,
  readWorkspaceDeletionEvidence,
  cleanupLocalQualificationSecretRoot,
  cleanupLocalQualificationPostgresRoot,
  createHTTP,
  exactRepoDigestFromInspection,
  finishLocalWorkspaceQualification,
  immutableImageDigest,
  liveAuthorityAdjustmentReadback,
  login,
  loadJ0ReadyReceipt,
  openQualificationSessions,
  loadSub2APISecretFile,
  localBuildProxyArgs,
  localDockerInstallationInputs,
  localQualificationComposeFiles,
  qualificationEnvFileEntries,
  qualificationComposeEnvironment,
  parseLocalQualificationArgs,
  qualificationModeBinding,
  qualificationPathAllowed,
  RETAINED_LEGACY_ENDPOINT_PREFIXES,
  runLocalV2Qualification,
  verifyStores,
  localV2ApplicationEntryBoundary,
  localV2QualificationCA,
  localV2QualificationOrigin,
  LOCAL_V2_QUALIFICATION_SERVICES,
  LOCAL_V2_OWNER_STORES,
  redactedError,
  runLocalWorkspaceJ1HTTPQualification,
  sourceData,
  stableID,
  validateQualificationSourceIdentity,
  validateJ0ReadyReceipt,
  validateLocalJ1AccountingReadback,
  validateLocalQualificationReceipt,
  workspaceDeleteFailureEvidence,
  workspaceDeleteReceiptStagesMatch,
  workspaceGatewaySecretRef,
  workspaceReservedKeyName,
  retainedWorkspaceKey
} from "../../tools/local-workspace-qualification.ts";
import { runLocalWorkspaceQualification } from "../../tools/local-workspace-qualification.ts";

// The CloudIdentity login the browser performs. The in-memory fake answers it
// the way the identity owner does: a login context carrying a double-submit
// CSRF challenge, and a command that answers with the session it bound.
const ownerLoginContextHeaders = () => {
  const headers = new Headers();
  headers.append("set-cookie", "opl_login_context=challenge; Path=/api/v2/auth");
  headers.append("set-cookie", "opl_login_csrf=challenge-hash; Path=/api/v2/auth");
  return headers;
};
const ownerSessionHeaders = () => {
  const headers = new Headers();
  headers.append("set-cookie", "opl_console_session=owner-session; Path=/");
  return headers;
};
const ownerSessionPayload = {
  actorId: "41", displayName: "customer@example.test", tenantId: "tenant-j1",
  permissions: [], csrfToken: "owner-csrf-token", expiresAt: "2026-10-18T12:00:00Z",
};
const ownerWalletPayload = {
  source: "gateway", status: "available", balanceUSDMicros: "1000000000",
  currency: "USD", fetchedAt: "2026-10-18T00:00:00Z",
};


const sha = "a".repeat(40);
const cloudDigest = `sha256:${"b".repeat(64)}`;
const workspaceDigest = `sha256:${"c".repeat(64)}`;
const workspaceReference = `ghcr.io/example/workspace@${workspaceDigest}`;

test("a restart still establishes the declared customer's CloudIdentity session", async () => {
  const http = {
    origin: "https://qualification.invalid",
    async json(path) {
      if (path === "/api/auth/login") {
        return { response: { headers: new Headers({ "set-cookie": "opl_session=control-plane", "x-opl-csrf-token": "cp-csrf" }) },
          payload: { user: { accountId: "acct-customer" } } };
      }
      if (path === "/api/v2/auth/context") {
        return { response: { headers: ownerLoginContextHeaders() }, payload: { csrfToken: "owner-csrf-token" } };
      }
      if (path === "/api/v2/auth/login") {
        return { response: { headers: ownerSessionHeaders() }, payload: ownerSessionPayload };
      }
      if (path === "/api/v2/auth/session") return { response: { headers: new Headers() }, payload: ownerSessionPayload };
      if (path === "/api/auth/me") {
        return { response: { headers: new Headers() }, payload: {
          source: "sub2api", available: true, status: "available",
          data: { accountId: "acct-customer", email: "customer@example.test", role: "owner", status: "active", sub2apiUserId: "41" },
        } };
      }
      throw new Error(`unexpected request ${path}`);
    },
  };
  const auth = await openQualificationSessions(http, "customer@example.test", "password", "acct-customer", "restart-key");
  assert.match(auth.cookie, /opl_console_session=owner-session/,
    "the restarted v2 requests carry the CloudIdentity cookie, not only the Control Plane one");
  assert.match(auth.cookie, /opl_session=control-plane/);
  assert.equal(auth.ownerCsrf, "owner-csrf-token");
});

test("the owner route mints the delete operation and the harness follows its identity", async () => {
  const accepted = {
    operationId: "op_owner_delete", owner: "workspace", kind: "delete_workspace",
    resourceId: "ws-alpha", status: "accepted", stage: "intent"
  };
  const running = { ...accepted, status: "running", stage: "retire_runtime" };
  const succeeded = { ...accepted, status: "succeeded", stage: "completed" };
  const calls = [];
  const waits = [];
  const responses = [{ payload: accepted }, { payload: running }, { payload: succeeded }];
  const http = { json: async (...args) => { calls.push(args); return responses.shift(); } };

  const deletion = await deleteWorkspaceOnOwnerRoute(http, { cookie: "opl_session=s" }, {
    workspaceId: "ws-alpha", confirmationName: "Alpha", idempotencyKey: "delete-alpha",
    wait: async (milliseconds) => waits.push(milliseconds)
  });
  assert.deepEqual(deletion, {
    operationId: "op_owner_delete", status: "succeeded", stage: "completed", accepted,
    lostResponses: 0, lastLoss: ""
  });
  assert.deepEqual(waits, [1000]);
  assert.deepEqual(calls[0], ["/api/v2/workspaces/ws-alpha", {
    method: "DELETE",
    headers: { "idempotency-key": "delete-alpha" },
    body: { confirmationName: "Alpha", acknowledgeDataDestruction: true }
  }, { cookie: "opl_session=s" }, [202]]);
  assert.deepEqual(calls[1], ["/api/v2/operations/workspace/op_owner_delete", {}, { cookie: "opl_session=s" }]);

  // A command whose answer was lost is re-sent with the same idempotency key and
  // reaches the operation the first send minted, so one deletion happens once.
  const recovered = [];
  let sends = 0;
  const replayed = {
    json: async (path, init = {}, _auth, statuses) => {
      recovered.push([path, init.headers?.["idempotency-key"], statuses]);
      if (path.includes("/operations/")) return { payload: succeeded };
      sends += 1;
      if (sends === 1) throw new Error("HTTP DELETE returned 502: upstream closed");
      return { payload: accepted };
    }
  };
  const recoveredDeletion = await deleteWorkspaceOnOwnerRoute(replayed, null, {
    workspaceId: "ws-alpha", confirmationName: "Alpha", idempotencyKey: "delete-alpha",
    wait: async () => {}
  });
  assert.equal(recoveredDeletion.operationId, "op_owner_delete");
  assert.equal(recoveredDeletion.lostResponses, 1);
  assert.match(recoveredDeletion.lastLoss, /502/);
  assert.deepEqual(recovered.filter((call) => !String(call[0]).includes("/operations/")).map((call) => call[1]),
    ["delete-alpha", "delete-alpha"]);

  const foreign = { json: async () => ({ payload: { ...accepted, resourceId: "ws-other" } }) };
  await assert.rejects(() => deleteWorkspaceOnOwnerRoute(foreign, null, {
    workspaceId: "ws-alpha", confirmationName: "Alpha", idempotencyKey: "delete-alpha"
  }), /not accepted for this Workspace/);

  const failing = {
    json: async (path) => path.includes("/operations/")
      ? { payload: { ...accepted, status: "failed", stage: "refund" } }
      : { payload: accepted }
  };
  await assert.rejects(() => deleteWorkspaceOnOwnerRoute(failing, null, {
    workspaceId: "ws-alpha", confirmationName: "Alpha", idempotencyKey: "delete-alpha"
  }), /did not succeed/);
});

test("a Workspace refund binds Gateway's exact refund to the original charge", async () => {
  const refund = {
    id: "wallet-refund-alpha", workspaceId: "ws-alpha", kind: "refund", status: "confirmed",
    amountUSDMicros: "52506972", receiptId: "receipt-refund", externalReference: "opl:refund-alpha",
    originalChargeOperationId: "wallet-charge-alpha", createdAt: "2026-10-18T00:10:00Z",
  };
  const calls = [];
  const http = { json: async (...args) => { calls.push(args); return { payload: { items: [refund] } }; } };
  const binding = { workspaceId: "ws-alpha", chargeAmountUsdMicros: "52580000", chargeOperationId: "wallet-charge-alpha", refundOperationId: refund.id };
  assert.deepEqual(await readWorkspaceRefundReceipt(http, null, binding), {
    walletOperationId: refund.id, receiptId: refund.receiptId, amountUsdMicros: refund.amountUSDMicros,
    code: refund.externalReference, originalChargeOperationId: refund.originalChargeOperationId,
  });
  assert.deepEqual(calls.map((args) => args[0]), ["/api/v2/workspaces/ws-alpha/transactions?limit=100"]);
  for (const change of [{ originalChargeOperationId: "another-charge" }, { status: "unknown" }, { amountUSDMicros: "52580001" }]) {
    await assert.rejects(() => readWorkspaceRefundReceipt({ json: async () => ({ payload: { items: [{ ...refund, ...change }] } }) }, null, binding), /refund receipt readback is invalid/);
  }
});

test("deleted Workspace readback requires exact Ledger evidence and confirmed owner absence", async () => {
  const deletion = {
    workspaceId: "ws-alpha", operationId: "delete-alpha", resourceDeletionStatus: "confirmed", dataDeletionStatus: "confirmed",
    refundStatus: "confirmed", refundOperationId: "refund-alpha",
    deletionReceipt: { id: "receipt-delete", kind: "provider_action", owner: "workspace", operationId: "delete-alpha", outcome: "confirmed", createdAt: "2026-10-24T00:00:00Z" },
  };
  const read = (payload) => readWorkspaceDeletionEvidence({ json: async (path) => {
    assert.equal(path, "/api/v2/workspaces/ws-alpha/deletion");
    return { payload };
  } }, null, { workspaceId: "ws-alpha", operationId: "delete-alpha" });
  assert.equal(await read(deletion), deletion);
  for (const changed of [null, { ...deletion, dataDeletionStatus: "unknown" },
    { ...deletion, deletionReceipt: undefined },
    { ...deletion, deletionReceipt: { ...deletion.deletionReceipt, operationId: "foreign-delete" } },
    { ...deletion, deletionReceipt: { ...deletion.deletionReceipt, kind: "wallet_action" } },
    { ...deletion, refundStatus: "requested" },
  ]) await assert.rejects(() => read(changed), /deletion receipt readback/);
});

test("owner delete continues only from exact durable compute pending evidence", async () => {
  const path = "/api/workspaces/ws-alpha";
  const init = { method: "DELETE", headers: { "idempotency-key": "delete-alpha" }, body: {} };
  const auth = { cookie: "session=test", csrf: "csrf-test" };
  const operationId = "workspace-delete-alpha";
  const responses = [
    { response: { status: 202, headers: new Headers({ "retry-after": "1" }) }, payload: { status: "pending", phase: "storage_absent", ownerStage: "compute", computeStatus: "destroying", operationId, workspaceId: "ws-alpha", computeReadbacks: 1, maxComputeReadbacks: 8 } },
    { response: { status: 202, headers: new Headers({ "retry-after": "1" }) }, payload: { status: "pending", phase: "storage_absent", ownerStage: "compute", computeStatus: "destroying", operationId, workspaceId: "ws-alpha", computeReadbacks: 2, maxComputeReadbacks: 8 } },
    { response: { status: 200, headers: new Headers() }, payload: { status: "deleted", operationId, workspaceId: "ws-alpha" } }
  ];
  const calls = [];
  const waits = [];
  const pendingEvidence = [];
  const http = {
    json: async (...args) => {
      calls.push(args);
      return responses.shift();
    }
  };

  const deletion = await continueWorkspaceDelete(http, path, init, auth, {
    operationId, workspaceId: "ws-alpha", onPending: (pending) => pendingEvidence.push(pending)
  }, async (milliseconds) => waits.push(milliseconds));
  assert.equal(deletion.status, "deleted");
  assert.equal(calls.length, 3);
  assert.deepEqual(waits, [1000, 1000]);
  for (const call of calls) assert.deepEqual(call, [path, init, auth, [200, 202]]);
  assert.equal(pendingEvidence.length, 2);
  assert.deepEqual(pendingEvidence.at(-1), {
    phase: "storage_absent", ownerStage: "compute", ordinal: 2, max: 8,
    operationDigest: `sha256:${createHash("sha256").update(operationId).digest("hex")}`
  });
  assert.doesNotMatch(JSON.stringify(pendingEvidence), /workspace-delete-alpha|ws-alpha/);
});

test("owner delete response-loss continuation preserves the consumed read ordinal", async () => {
  const responses = [
    { response: { status: 202, headers: new Headers({ "retry-after": "1" }) }, payload: {
      status: "pending", phase: "storage_absent", ownerStage: "compute", computeStatus: "destroying",
      operationId: "workspace-delete-alpha", workspaceId: "ws-alpha", computeReadbacks: 2, maxComputeReadbacks: 8
    } },
    { response: { status: 200, headers: new Headers() }, payload: {
      status: "deleted", operationId: "workspace-delete-alpha", workspaceId: "ws-alpha"
    } }
  ];
  const waits = [];
  const result = await continueWorkspaceDelete({ json: async () => responses.shift() }, "/api/workspaces/ws-alpha", {
    method: "DELETE"
  }, null, {
    operationId: "workspace-delete-alpha", workspaceId: "ws-alpha", initialReadback: 1, initialMaxReadbacks: 8
  }, async (milliseconds) => waits.push(milliseconds));
  assert.equal(result.status, "deleted");
  assert.deepEqual(waits, [1000]);
  assert.deepEqual(workspaceDeleteFailureEvidence(new Error("The operation was aborted due to timeout")), {
    status: 0, reasonCode: "request_timeout"
  });
});

test("owner delete rejects pending identity, ordinal, and budget drift", async () => {
  const base = { status: "pending", phase: "storage_absent", ownerStage: "compute", computeStatus: "destroying", operationId: "workspace-delete-alpha", workspaceId: "ws-alpha", computeReadbacks: 1, maxComputeReadbacks: 8 };
  for (const [name, pending] of Object.entries({
    identity: { ...base, operationId: "workspace-delete-other" },
    ordinal: { ...base, computeReadbacks: 2 },
    budget: { ...base, maxComputeReadbacks: 9 },
    exhausted: { ...base, computeReadbacks: 8 }
  })) {
    const http = { json: async () => ({ response: { status: 202 }, payload: pending }) };
    await assert.rejects(() => continueWorkspaceDelete(http, "/api/workspaces/ws-alpha", { method: "DELETE" }, null, {
      operationId: "workspace-delete-alpha", workspaceId: "ws-alpha"
    }), /pending evidence/, name);
  }
});

test("owner delete failure keeps only redacted last pending evidence", async () => {
  const operationId = "workspace-delete-secret-alpha";
  const pending = {
    status: "pending", phase: "storage_absent", ownerStage: "compute", computeStatus: "destroying",
    operationId, workspaceId: "ws-secret-alpha", computeReadbacks: 1, maxComputeReadbacks: 8
  };
  const evidence = [];
  const failures = [];
  let calls = 0;
  const http = {
    json: async (_path, _init, _auth, statuses) => {
      if (statuses.includes(202)) {
        calls += 1;
        if (calls === 1) return { response: { status: 202, headers: new Headers({ "retry-after": "1" }) }, payload: pending };
        throw new Error(`HTTP DELETE /api/workspaces/${pending.workspaceId} returned 409: {"reasonCode":"fabric_compute_readback_unavailable","operationId":"${operationId}"}`);
      }
      throw new Error("unexpected status set");
    }
  };
  await assert.rejects(() => continueWorkspaceDelete(http, "/api/workspaces/ws-secret-alpha", { method: "DELETE" }, null, {
    operationId, workspaceId: pending.workspaceId, onPending: (value) => evidence.push(value), onFailure: (value) => failures.push(value)
  }, async () => {}), /continuation failed/);
  assert.equal(evidence.length, 1);
  assert.deepEqual(failures, [{ status: 409, reasonCode: "fabric_compute_readback_unavailable" }]);
  assert.deepEqual(evidence[0], {
    phase: "storage_absent", ownerStage: "compute", ordinal: 1, max: 8,
    operationDigest: `sha256:${createHash("sha256").update(operationId).digest("hex")}`
  });
  assert.doesNotMatch(JSON.stringify({ evidence, failures }), /workspace-delete-secret-alpha|ws-secret-alpha/);
});

test("qualification compose uses the runner-owned exact environment", () => {
  const exactSecretRoot = "/tmp/opl-qualification/fabric-secrets";
  const environment = qualificationComposeEnvironment({
    PATH: "/usr/bin",
    OPL_WORKSPACE_IMAGE: "registry.example/stale@sha256:" + "d".repeat(64),
    OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT: "/tmp/stale-secrets"
  }, [
    ["OPL_WORKSPACE_IMAGE", workspaceReference],
    ["OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT", exactSecretRoot]
  ]);

  assert.equal(environment.PATH, "/usr/bin");
  assert.equal(environment.OPL_WORKSPACE_IMAGE, workspaceReference);
  assert.equal(environment.OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT, exactSecretRoot);
});

test("local-v2 compose readback rejects a host override", () => {
  assert.doesNotThrow(() => assertLocalV2ComposeEnvironment("OPL_CLOUD_IMAGE=declared\n", {
    OPL_CLOUD_IMAGE: "declared"
  }));
  assert.throws(() => assertLocalV2ComposeEnvironment("OPL_CLOUD_IMAGE=ambient\n", {
    OPL_CLOUD_IMAGE: "declared"
  }), { code: "local_v2_compose_environment_mismatch" });
});

test("v2 store verification checks the owner schema and returns an explicit owner map", async () => {
  const calls = [];
  const compose = async (args) => {
    calls.push(args);
    if (args.some((arg) => String(arg).includes("pg_database"))) {
      return { stdout: LOCAL_V2_OWNER_STORES.map(([database, owner]) => `${database}:${owner}`).join("\n") };
    }
    return { stdout: "7:7" };
  };
  const result = await verifyStores(compose, { stores: LOCAL_V2_OWNER_STORES, v2: true });
  assert.equal(result.ownerSeparated, true);
  assert.equal(result.ownerStores.opl_workspace.schema, "workspace");
  assert.equal(result.ownerStores.opl_workspace.owner, "opl_workspace_owner");
  assert.ok(calls.some((args) => args.some((arg) => String(arg).includes("schemaname='workspace'"))));
});

test("qualification teardown clears the root-owned secret bind mount through Fabric", async () => {
  const calls = [];
  const compose = async (args, settings) => {
    calls.push({ args, settings });
    return { code: 0 };
  };
  await cleanupLocalQualificationSecretRoot(compose, "/tmp/opl-local-qualification-123/fabric-secrets");
  assert.deepEqual(calls, [{
    args: [
      "exec", "-T", "--user", "root", "fabric", "sh", "-c",
      'rm -rf -- "$1"/* "$1"/.[!.]* "$1"/..?*',
      "opl-qualification-secret-cleanup", "/tmp/opl-local-qualification-123/fabric-secrets"
    ],
    settings: { allowFailure: true }
  }]);
  await assert.rejects(
    () => cleanupLocalQualificationSecretRoot(async () => ({ code: 1 }), "/tmp/opl-local-qualification-123/fabric-secrets"),
    /secret cleanup failed/
  );
});

test("PostgreSQL teardown confines privileged cleanup to the exact data root and actual runner owner", async () => {
  const scratch = await mkdtemp(join(tmpdir(), "opl-local-qualification-"));
  const data = join(scratch, "postgres");
  const outside = await mkdtemp(join(tmpdir(), "opl-postgres-outside-"));
  await mkdir(data, { mode: 0o700 });
  const owner = await lstat(scratch);
  const calls = [];
  const execute = async (command, args, settings) => {
    calls.push({ command, args, settings });
    return { code: 0 };
  };
  try {
    await cleanupLocalQualificationPostgresRoot(scratch, data, execute);
    assert.equal(calls.length, 1);
    const { command, args, settings } = calls[0];
    assert.equal(command, "docker");
    assert.deepEqual(args.slice(0, 9), ["run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--mount", `type=bind,source=${await realpath(data)},target=/qualification-postgres`]);
    assert.match(args[11], /^postgres:.*@sha256:[0-9a-f]{64}$/);
    assert.deepEqual(args.slice(-2), [String(owner.uid), String(owner.gid)]);
    assert.deepEqual(settings, { allowFailure: true });
    await assert.rejects(() => cleanupLocalQualificationPostgresRoot(scratch, outside, execute), /exact run-owned data root/);
    await rm(data, { recursive: true });
    await symlink(outside, data);
    await assert.rejects(() => cleanupLocalQualificationPostgresRoot(scratch, data, execute), /exact run-owned data root/);
    assert.equal(calls.length, 1);
    await rm(data);
    await mkdir(data, { mode: 0o700 });
    await assert.rejects(() => cleanupLocalQualificationPostgresRoot(scratch, data, async () => ({ code: 1 })),
      new RegExp(`cleanup failed for .*postgres \\(owner ${owner.uid}:${owner.gid}\\)`));
  } finally {
    await rm(scratch, { recursive: true, force: true });
    await rm(outside, { recursive: true, force: true });
  }
});

test("source image tag inspection hands off one exact immutable RepoDigest", () => {
  const repository = "127.0.0.1:56287/opl-local-qualification-cloud";
  const exact = `${repository}@${cloudDigest}`;
  assert.equal(exactRepoDigestFromInspection(repository, {
    Id: cloudDigest,
    RepoDigests: [exact]
  }), exact);

  for (const [name, inspection] of Object.entries({
    missing: { Id: cloudDigest, RepoDigests: [] },
    "wrong repository": { Id: cloudDigest, RepoDigests: [`127.0.0.1:56287/other@${cloudDigest}`] },
    mutable: { Id: cloudDigest, RepoDigests: [`${repository}:source`] },
    duplicate: { Id: cloudDigest, RepoDigests: [exact, exact] },
    "invalid image identity": { Id: "sha256:not-a-digest", RepoDigests: [exact] }
  })) {
    assert.throws(() => exactRepoDigestFromInspection(repository, inspection), /source-built image/, name);
  }
});

test("qualification selects the current Candidate overlay set", async () => {
  const fixture = localQualificationComposeFiles("fixture");
  assert.deepEqual(fixture, [
    "compose.yaml",
    "deploy/portable/compose.deployment-platform-owned.yaml",
    "deploy/portable/compose.fabric-local-docker.yaml",
    "deploy/portable/compose.local-workspace.yaml",
    "deploy/portable/compose.local-qualification.yaml"
  ]);
  assert.deepEqual(localQualificationComposeFiles("live"), [
    ...fixture.slice(0, -1),
    "deploy/portable/compose.local-qualification-live.yaml"
  ]);
  assert.throws(() => localQualificationComposeFiles("staging"), /authority mode/);

  // The selected overlays are the ones that carry the deployment mode and the
  // Fabric provider the script fixes in its own environment, so a future split
  // that moves either selection fails here instead of at provider startup.
  const repository = new URL("../../", import.meta.url);
  const [deployment, fabric] = await Promise.all([
    readFile(new URL("deploy/portable/compose.deployment-platform-owned.yaml", repository), "utf8"),
    readFile(new URL("deploy/portable/compose.fabric-local-docker.yaml", repository), "utf8")
  ]);
  assert.match(deployment, /OPL_DEPLOYMENT_MODE: platform_owned\b/);
  assert.match(fabric, /OPL_FABRIC_PROVIDER: local-docker\b/);
  for (const file of fixture) {
    await readFile(new URL(file, repository), "utf8");
  }
});

test("qualification requires the installation-owned Local-Docker inputs", () => {
  const profile = JSON.stringify({
    schemaVersion: 1,
    packages: [{ id: "basic", name: "Basic Workspace", available: true, compute: { id: "local-basic", server: "2c4g", cpu: 2, memoryGb: 4, instanceType: "local-2c4g" }, storage: { sizeGb: 10, quotaPolicy: "linux-project" } }]
  });
  assert.throws(() => localDockerInstallationInputs({}), (error) => error.code === "local_docker_storage_root_missing");
  assert.throws(() => localDockerInstallationInputs({ OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: "relative/storage" }), (error) => error.code === "local_docker_storage_root_invalid");
  assert.throws(() => localDockerInstallationInputs({ OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: "/srv/opl-workspaces" }), (error) => error.code === "local_docker_provider_profile_missing");
  assert.throws(() => localDockerInstallationInputs({ OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: "/srv/opl-workspaces", OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: "{" }), (error) => error.code === "local_docker_provider_profile_invalid");
  assert.throws(() => localDockerInstallationInputs({ OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: "/srv/opl-workspaces", OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: '{"schemaVersion":1,"packages":[]}' }), (error) => error.code === "local_docker_provider_profile_invalid");
  assert.deepEqual(localDockerInstallationInputs({
    OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: "  /srv/opl-workspaces  ",
    OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: profile
  }), { storageRoot: "/srv/opl-workspaces", providerProfileJSON: profile });
});

test("qualification stops at the installation preflight before it invokes Docker", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-installation-preflight-test-"));
  const receiptPath = join(root, "receipt.json");
  const previous = {
    storageRoot: process.env.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT,
    profile: process.env.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON
  };
  let sourceReads = 0;
  try {
    delete process.env.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT;
    delete process.env.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON;
    await assert.rejects(() => runLocalWorkspaceQualification({
      sourceSha: sha,
      cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
      workspaceImage: workspaceReference,
      receiptPath,
      buildSourceImages: false,
      authorityMode: "fixture"
    }, {
      readSourceIdentity: async () => {
        sourceReads += 1;
        return { sha, tree: sha, clean: true };
      }
    }), (error) => error.code === "local_docker_storage_root_missing");
    const receipt = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(receipt.status, "NOT_READY");
    assert.equal(receipt.stage, "installation_preflight");
    assert.equal(receipt.errorCode, "local_docker_storage_root_missing");
    // Source identity is admitted first, and nothing beyond that runs: the
    // script never reaches Docker, image admission, or Compose.
    assert.equal(sourceReads, 1);
    assert.equal(receipt.images.cloud.digest, cloudDigest);
  } finally {
    for (const [name, value] of Object.entries({
      OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: previous.storageRoot,
      OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: previous.profile
    })) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
    await rm(root, { recursive: true, force: true });
  }
});

function liveAuthorityHistoryFetch({ duplicate = false } = {}) {
  const requestedPages = [];
  const request = async (input, init = {}) => {
    const url = new URL(String(input));
    if (url.pathname === "/api/v1/auth/login") {
      assert.equal(init.method, "POST");
      return new Response(JSON.stringify({ code: 0, data: { access_token: "test-access" } }), {
        status: 200, headers: { "content-type": "application/json" }
      });
    }
    assert.equal(url.pathname, "/api/v1/admin/users/41/balance-history");
    assert.equal(url.searchParams.get("page_size"), "100");
    assert.equal(url.searchParams.get("type"), "balance");
    const page = Number(url.searchParams.get("page"));
    assert.ok(Number.isInteger(page) && page >= 1 && page <= 3);
    requestedPages.push(page);
    const count = page === 3 ? 1 : 100;
    const items = Array.from({ length: count }, (_, index) => {
      const target = page === 2 && index === 42 || duplicate && page === 3;
      return {
        code: target ? "opl:target" : `opl:filler:${(page - 1) * 100 + index}`,
        type: "balance", value: "-52.580000", status: "used", used_by: 41,
        used_at: "2026-07-16T00:01:00Z", created_at: "2026-07-16T00:00:00Z"
      };
    });
    return new Response(JSON.stringify({ code: 0, data: { items, total: 201, page, page_size: 100, pages: 3 } }), {
      status: 200, headers: { "content-type": "application/json" }
    });
  };
  return { request, requestedPages };
}

test("local qualification accepts exact source and immutable image identities", () => {
  assert.deepEqual(parseLocalQualificationArgs([
    "--source-sha", sha,
    "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
    "--workspace-image", workspaceReference,
    "--receipt", "/tmp/qualification.json"
  ]), {
    mode: "legacy-fixture",
    sourceSha: sha,
    cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
    workspaceImage: workspaceReference,
    receiptPath: "/tmp/qualification.json",
    buildSourceImages: false,
    authorityMode: "fixture",
    j0ReadyReceipt: "",
    sub2apiSecretFile: ""
  });
  assert.equal(immutableImageDigest(`ghcr.io/example/cloud@${cloudDigest}`), cloudDigest);
  assert.equal(immutableImageDigest(workspaceDigest), workspaceDigest);
  assert.throws(() => parseLocalQualificationArgs(["--source-sha", "main"]), /source SHA/);
  assert.throws(() => parseLocalQualificationArgs([
    "--source-sha", sha, "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`, "--workspace-image", workspaceReference,
    "--receipt", "/tmp/qualification.json", "--authority-mode", "production"
  ]), /authority mode/);
  assert.throws(() => parseLocalQualificationArgs([
    "--source-sha", sha,
    "--cloud-image", "ghcr.io/example/cloud:latest",
    "--workspace-image", workspaceReference,
    "--receipt", "/tmp/qualification.json"
  ]), /immutable cloud image/);
});

function j0ReadyReceipt(sourceSha = sha, sourceTree = "d".repeat(40)) {
  return {
    schemaVersion: 1,
    kind: "opl.local-workspace.j0-ready.v1",
    status: "READY",
    source: { sha: sourceSha, tree: sourceTree, clean: true },
    authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
    provider: "local-docker",
    failed: 0,
    skipped: 0
  };
}

test("live arguments require one external J0 READY receipt", () => {
  const parsed = parseLocalQualificationArgs([
    "--source-sha", sha,
    "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
    "--workspace-image", workspaceReference,
    "--receipt", "/tmp/qualification.json",
    "--authority-mode", "live",
    "--j0-ready-receipt", "/tmp/j0-ready.json"
  ]);
  assert.equal(parsed.j0ReadyReceipt, "/tmp/j0-ready.json");
  assert.equal(parsed.mode, "legacy-live");
  assert.throws(() => parseLocalQualificationArgs([
    "--mode", "legacy-fixture", "--source-sha", sha,
    "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`, "--workspace-image", workspaceReference,
    "--receipt", "/tmp/qualification.json", "--authority-mode", "live", "--j0-ready-receipt", "/tmp/j0-ready.json"
  ]), { code: "local_qualification_mode_conflict" });
  assert.throws(() => parseLocalQualificationArgs([
    "--source-sha", sha,
    "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
    "--workspace-image", workspaceReference,
    "--receipt", "/tmp/qualification.json",
    "--authority-mode", "live"
  ]), /J0 READY receipt/);
});

test("J0 READY admission binds exact clean source, authority, provider, and external 0600 file", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-j0-ready-test-"));
  const path = join(root, "j0-ready.json");
  const link = join(root, "j0-ready-link.json");
  const parentLink = join(root, "repo-parent");
  const value = j0ReadyReceipt();
  await writeFile(path, JSON.stringify(value), { mode: 0o600 });
  try {
    assert.equal(validateJ0ReadyReceipt(value, sha, "d".repeat(40)), value);
    const loaded = await loadJ0ReadyReceipt(path, sha, "d".repeat(40));
    assert.match(loaded.digest, /^sha256:[0-9a-f]{64}$/);
    assert.deepEqual(loaded.source, value.source);
    for (const invalid of [
      { ...value, kind: "opl.local-workspace.other.v1" },
      { ...value, status: "NOT_READY" },
      { ...value, source: { ...value.source, sha: "b".repeat(40) } },
      { ...value, source: { ...value.source, tree: "c".repeat(40) } },
      { ...value, source: { ...value.source, clean: false } },
      { ...value, authority: { ...value.authority, dedicated: false } },
      { ...value, provider: "tencent" },
      { ...value, skipped: 1 },
      { ...value, unexpected: true },
      { ...value, authority: { ...value.authority, token: "forbidden" } }
    ]) {
      assert.throws(() => validateJ0ReadyReceipt(invalid, sha, "d".repeat(40)), /j0_ready_receipt_invalid/);
    }
    await chmod(path, 0o644);
    await assert.rejects(() => loadJ0ReadyReceipt(path, sha, "d".repeat(40)), /j0_ready_receipt_invalid/);
    await chmod(path, 0o600);
    await symlink(path, link);
    await assert.rejects(() => loadJ0ReadyReceipt(link, sha, "d".repeat(40)), /j0_ready_receipt_invalid/);
    await symlink(new URL("../..", import.meta.url).pathname, parentLink);
    await assert.rejects(() => loadJ0ReadyReceipt(join(parentLink, "package.json"), sha, "d".repeat(40)), /j0_ready_receipt_invalid/);
    await assert.rejects(() => loadJ0ReadyReceipt(path, sha, "d".repeat(40), {
      currentUid: typeof process.getuid === "function" ? process.getuid() + 1 : 1
    }), /j0_ready_receipt_invalid/);
    await assert.rejects(() => loadJ0ReadyReceipt(new URL("../../package.json", import.meta.url).pathname, sha, "d".repeat(40)), /j0_ready_receipt_invalid/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("top-level live entry rejects J0 source drift before starting the J1 stack or HTTP", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-j0-top-level-reject-"));
  const path = join(root, "j0-ready.json");
  const outputPath = join(root, "j1.json");
  const baseOptions = {
    sourceSha: sha,
    cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
    workspaceImage: workspaceReference,
    receiptPath: outputPath,
    buildSourceImages: false,
    authorityMode: "live",
    j0ReadyReceipt: path,
    sub2apiSecretFile: ""
  };
  let stackStarts = 0;
  const dependencies = {
    loadLiveAuthority: async () => ({ baseURL: "https://sandbox.example.test", adminEmail: "admin@example.test", adminPassword: "password", authorityClass: "sandbox", qualificationUserEmail: "user@example.test", qualificationUserPassword: "password" }),
    readSourceIdentity: async () => ({ sha, tree: "d".repeat(40), clean: true }),
    runLiveJ1: async () => { stackStarts += 1; throw new Error("J1 stack must not start"); }
  };
  try {
    for (const invalid of [
      j0ReadyReceipt("b".repeat(40)),
      j0ReadyReceipt(sha, "c".repeat(40))
    ]) {
      await writeFile(path, JSON.stringify(invalid), { mode: 0o600 });
      await assert.rejects(() => runLocalWorkspaceQualification(baseOptions, dependencies), /j0_ready_receipt_invalid/);
    }
    assert.equal(stackStarts, 0);
    const failureReceipt = JSON.parse(await readFile(outputPath, "utf8"));
    assert.equal(failureReceipt.stage, "j0_ready_preflight");
    assert.equal(failureReceipt.errorCode, "j0_ready_receipt_invalid");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

const sub2apiSecretValues = Object.freeze({
  OPL_SUB2API_BASE_URL: "https://sandbox.example.test",
  OPL_SUB2API_ADMIN_EMAIL: "admin@example.test",
  OPL_SUB2API_ADMIN_PASSWORD: "admin-password-not-real",
  OPL_QUALIFICATION_AUTHORITY_CLASS: "sandbox",
  OPL_QUALIFICATION_USER_EMAIL: "user@example.test",
  OPL_QUALIFICATION_USER_PASSWORD: "user-password-not-real"
});

function sub2apiSecretYAML(values = sub2apiSecretValues) {
  return Object.entries(values).map(([key, value]) => `${key}: ${JSON.stringify(value)}`).join("\n") + "\n";
}

test("live Sub2API secret file admission is exact and never creates a second env SSOT", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-sub2api-secret-test-"));
  const path = join(root, "sub2api.yaml");
  const link = join(root, "sub2api-link.yaml");
  try {
    await writeFile(path, sub2apiSecretYAML(), { mode: 0o600 });
    const admitted = await loadSub2APISecretFile(path, { environment: {} });
    assert.deepEqual(admitted, {
      baseURL: sub2apiSecretValues.OPL_SUB2API_BASE_URL,
      adminEmail: sub2apiSecretValues.OPL_SUB2API_ADMIN_EMAIL,
      adminPassword: sub2apiSecretValues.OPL_SUB2API_ADMIN_PASSWORD,
      authorityClass: sub2apiSecretValues.OPL_QUALIFICATION_AUTHORITY_CLASS,
      qualificationUserEmail: sub2apiSecretValues.OPL_QUALIFICATION_USER_EMAIL,
      qualificationUserPassword: sub2apiSecretValues.OPL_QUALIFICATION_USER_PASSWORD
    });
    assert.deepEqual(parseLocalQualificationArgs([
      "--source-sha", sha, "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
      "--workspace-image", workspaceReference, "--receipt", "/tmp/qualification.json",
      "--authority-mode", "live", "--j0-ready-receipt", "/tmp/j0-ready.json", "--sub2api-secret-file", path
    ]).sub2apiSecretFile, path);
    assert.throws(() => parseLocalQualificationArgs([
      "--source-sha", sha, "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
      "--workspace-image", workspaceReference, "--receipt", "/tmp/qualification.json",
      "--sub2api-secret-file", path
    ]), /live authority mode/);
    assert.throws(() => parseLocalQualificationArgs([
      "--source-sha", sha, "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
      "--workspace-image", workspaceReference, "--receipt", "/tmp/qualification.json",
      "--authority-mode", "live", "--j0-ready-receipt", "/tmp/j0-ready.json", "--sub2api-secret-file", "relative.yaml"
    ]), /absolute/);

    await assert.rejects(() => loadSub2APISecretFile(path, {
      environment: { OPL_SUB2API_BASE_URL: "https://outer.example.test" }
    }), /sub2api_secret_file_env_conflict/);
    await assert.rejects(() => loadSub2APISecretFile(path, {
      environment: {}, currentUid: typeof process.getuid === "function" ? process.getuid() + 1 : 1
    }), /sub2api_secret_file_invalid/);
    await assert.rejects(() => loadSub2APISecretFile(root, { environment: {} }), /sub2api_secret_file_invalid/);

    await symlink(path, link);
    await assert.rejects(() => loadSub2APISecretFile(link, { environment: {} }), /sub2api_secret_file_invalid/);
    await chmod(path, 0o640);
    await assert.rejects(() => loadSub2APISecretFile(path, { environment: {} }), /sub2api_secret_file_invalid/);
    await chmod(path, 0o600);

    for (const [name, values] of Object.entries({
      unknown: { ...sub2apiSecretValues, UNKNOWN_FIELD: "forbidden" },
      missing: Object.fromEntries(Object.entries(sub2apiSecretValues).slice(1)),
      wrongType: { ...sub2apiSecretValues, OPL_SUB2API_ADMIN_PASSWORD: 42 }
    })) {
      await writeFile(path, sub2apiSecretYAML(values), { mode: 0o600 });
      await assert.rejects(() => loadSub2APISecretFile(path, { environment: {} }), /sub2api_secret_file_schema_invalid/, name);
    }

    const envFileEntries = qualificationEnvFileEntries([
      ...Object.entries(sub2apiSecretValues), ["OPL_CLOUD_IMAGE", "cloud-image"]
    ], { authorityMode: "live", sub2apiSecretFile: path });
    assert.deepEqual(envFileEntries, [["OPL_CLOUD_IMAGE", "cloud-image"]]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("Sub2API secret file failures redact the path and values before Docker", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-sub2api-secret-redaction-"));
  const path = join(root, "private-source.yaml");
  const receiptPath = join(root, "receipt.json");
  const previous = Object.fromEntries(Object.keys(sub2apiSecretValues).map((name) => [name, process.env[name]]));
  for (const name of Object.keys(sub2apiSecretValues)) delete process.env[name];
  try {
    await writeFile(path, `${sub2apiSecretYAML()}UNKNOWN_FIELD: ${JSON.stringify("do-not-leak-value")}\n`, { mode: 0o600 });
    await assert.rejects(() => runLocalWorkspaceQualification({
      sourceSha: sha,
      cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
      workspaceImage: workspaceReference,
      receiptPath,
      buildSourceImages: false,
      authorityMode: "live",
      sub2apiSecretFile: path
    }), /sub2api_secret_file_schema_invalid/);
    const receipt = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(receipt.stage, "authority_preflight");
    assert.equal(receipt.errorCode, "sub2api_secret_file_schema_invalid");
    assert.equal(receipt.error, "sub2api_secret_file_schema_invalid");
    assert.doesNotMatch(JSON.stringify(receipt), /private-source|do-not-leak-value|admin-password-not-real/);
  } finally {
    for (const [name, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
    await rm(root, { recursive: true, force: true });
  }
});

test("live authority adjustment readback counts the exact code through the final financial page", async () => {
  const exact = liveAuthorityHistoryFetch();
  const evidence = await liveAuthorityAdjustmentReadback(
    "https://sandbox.example", "admin@example.test", "password", "41", "opl:target", "-52580000", exact.request
  );
  assert.deepEqual(exact.requestedPages, [1, 2, 3]);
  assert.deepEqual(evidence, { code: "opl:target", userId: "41", valueUsdMicros: "-52580000", status: "used", count: 1 });

  const duplicate = liveAuthorityHistoryFetch({ duplicate: true });
  await assert.rejects(() => liveAuthorityAdjustmentReadback(
    "https://sandbox.example", "admin@example.test", "password", "41", "opl:target", "-52580000", duplicate.request
  ), /cardinality/);
  assert.deepEqual(duplicate.requestedPages, [1, 2, 3]);
});

test("Local J1 accounting allows unrelated history and rejects duplicate operation evidence", () => {
  const operationId = "workspace-launch-current";
  const workspaceId = "ws-current";
  const receiptId = "receipt-current";
  const runtimeId = "runtime-current";
  const applicationOperationId = "workspace-application-deploy-current";
  const keyId = "700";
  const keyName = workspaceReservedKeyName(workspaceId);
  const entryUrl = "http://127.0.0.1:32776/";
  const debitCode = "opl:current-debit";
  const amountUsdMicros = "52580000";
  const currentKey = { id: keyId, name: keyName, kind: "workspace", status: "active" };
  const historicalKey = { id: "699", name: "opl-workspace-historical", kind: "workspace", status: "active" };
  const currentReceipt = {
    receiptId, operationId, workspaceId, type: "billing.workspace_purchased.v1", status: "completed",
    chargeReference: debitCode, totalUsdMicros: amountUsdMicros,
    fulfillment: { computeAllocationId: "compute-current", storageId: "storage-current", attachmentId: "attachment-current" }
  };
  const historicalReceipt = {
    ...currentReceipt,
    receiptId: "receipt-historical",
    operationId: "workspace-launch-historical",
    workspaceId: "ws-historical",
    chargeReference: "opl:historical-debit",
    fulfillment: { computeAllocationId: "compute-historical", storageId: "storage-historical", attachmentId: "attachment-historical" }
  };
  const input = {
    operationId, workspaceId, receiptId, runtimeId, keyId, keyName, sub2apiUserId: "41", debitCode, amountUsdMicros,
    beforeMicros: "1000000000", afterMicros: "947420001",
    baselineKeys: [historicalKey],
    keys: [historicalKey, currentKey], receipts: [historicalReceipt, currentReceipt],
    key: currentKey, keyUsage: { totalRequests: 0 }, usage: { totalRequests: 0 },
    history: { items: [{ valueUsdMicros: "-1000000", status: "used" }] },
    debit: { count: 1, code: debitCode, userId: "41", amountUsdMicros },
    evidence: {
      operation: { owner: "workspace", kind: "create_workspace", operationId, resourceId: workspaceId, status: "succeeded" },
      current: { status: "ready", entryUrl, operationId: applicationOperationId },
      workspace: { url: entryUrl },
      runtime: { runtimeId, currentApplication: { operationId: applicationOperationId } },
      receipt: currentReceipt
    }
  };

  assert.deepEqual(validateLocalJ1AccountingReadback(input), { walletExactDeltaObserved: false });
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, keys: [...input.keys, currentKey] }), /key cardinality/);
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, keys: [historicalKey, { ...currentKey, name: "opl-workspace-other" }] }), /key cardinality/);
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, receipts: [...input.receipts, currentReceipt] }), /receipt cardinality/);
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, debit: { ...input.debit, count: 2 } }), /debit cardinality/);
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, baselineKeys: [...input.baselineKeys, currentKey] }), /predates/);
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, baselineKeys: [...input.baselineKeys, { id: "698", name: keyName, kind: "workspace", status: "active" }] }), /predates/);
  assert.throws(() => validateLocalJ1AccountingReadback({ ...input, receipts: [...input.receipts, { ...currentReceipt, receiptId: "receipt-duplicate" }] }), /receipt cardinality/);
  assert.throws(() => validateLocalJ1AccountingReadback({
    ...input,
    evidence: { ...input.evidence, runtime: { runtimeId, currentApplication: { operationId: "workspace-application-deploy-other" } } }
  }), /accounting binding/);
});

test("READY receipt binds the exact durable and accounting evidence", () => {
  const fixtureReceipt = {
    schemaVersion: 1,
    status: "READY",
    source: { sha, tree: "d".repeat(40) },
    images: {
      cloud: { input: `ghcr.io/example/cloud@${cloudDigest}`, repoDigest: `ghcr.io/example/cloud@${cloudDigest}`, digest: cloudDigest, runningDigest: cloudDigest },
      workspace: { input: workspaceReference, repoDigest: workspaceReference, digest: workspaceDigest, runningDigest: workspaceDigest }
    },
    command: "npm run qualify:local:workspace -- --source-sha [redacted]",
    processes: { console: "ready", controlPlane: "ready", fabric: "ready", ledger: "ready" },
    stores: { controlPlane: "durable", fabric: "durable", ledger: "durable", ownerSeparated: true },
    identities: {
      accountId: "acct-admin", sub2apiUserId: "41", operationId: "workspace-operation-alpha",
      deleteOperationId: "workspace-delete-alpha", deletionReceiptId: "receipt-delete", workspaceId: "ws-alpha",
      runtimeId: "rt-alpha", keyId: "71", debitCode: "opl:qualification-alpha", purchaseReceiptId: "receipt-alpha",
      runtimeVersionId: "rv-alpha", computePlanId: "cp-alpha", storagePlanId: "sp-alpha"
    },
    debit: { count: 1, accountId: "acct-admin", operationId: "workspace-operation-alpha", workspaceId: "ws-alpha", code: "opl:qualification-alpha", userId: "41", amountUsdMicros: "52580000" },
    wallet: { beforeUsdMicros: "100000000", afterUsdMicros: "47420000", afterDeleteUsdMicros: "99926972" },
    receipt: {
      count: 1, id: "receipt-alpha", accountId: "acct-admin", operationId: "workspace-operation-alpha", workspaceId: "ws-alpha",
      quoteId: "quote-alpha", chargeOperationId: "wallet-charge-alpha", computeAllocationId: "compute-alpha", storageId: "storage-alpha", attachmentId: "attachment-alpha",
      chargeReference: "opl:qualification-alpha", amountUsdMicros: "52580000"
    },
    application: {
      status: "ready", operationId: "workspace-application-deploy-alpha", applicationId: "opl-app", revision: "1".repeat(64),
      entryUrl: "http://127.0.0.1:32776/", runtimeId: "rt-alpha", keyId: "71", resumed: false
    },
    restart: { performed: true, operationStable: true, workspaceStable: true, runtimeStable: true, receiptStable: true },
    deletion: {
      ownerAuthorized: true, accountId: "acct-admin", operationId: "workspace-delete-alpha", deletionReceiptId: "receipt-delete",
      workspaceId: "ws-alpha", runtimeId: "", workspaceApiKeyId: "",
      workspaceAbsent: true, runtimeAbsent: true, workspaceKeyRetained: true, fabricSecretAbsent: true
    },
    deletionReceipt: { count: 1, id: "receipt-delete", kind: "provider_action", owner: "workspace", outcome: "confirmed",
      operationId: "workspace-delete-alpha", workspaceId: "ws-alpha", createdAt: "2026-10-18T00:10:00Z" },
    residuals: { containers: 0, volumes: 0, networks: 0 },
    authorityWriteCounts: { keyCreates: 1, keyDeletes: 0, debits: 1, refunds: 1 },
    mutationCounts: { workspaceQuotePosts: 1, workspaceOrderPosts: 1, workspaceDeleteRequests: 1, refundPosts: 0 },
    // The refund the Workspace owner executed for its own Delete: Gateway's
    // wallet operation and its typed financial receipt, not a Control Plane
    // closeout.
    refund: {
      count: 1, walletOperationId: "wallet-refund-alpha", receiptId: "receipt-refund",
      amountUsdMicros: "52506972", code: "opl:qualification-refund", userId: "41", originalChargeOperationId: "wallet-charge-alpha"
    },
    usage: { source: "sub2api", status: "available", totalRequests: 0 },
    qualification: { authorityMode: "fixture", p0Ready: false },
    deferred: ["tencent-tke", "production-sub2api", "production-secrets"]
  };
  assert.equal(validateLocalQualificationReceipt(fixtureReceipt), fixtureReceipt);

  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, status: "NOT_READY" }), /READY receipt/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, debit: { ...fixtureReceipt.debit, count: 2 } }), /debit/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, restart: { ...fixtureReceipt.restart, runtimeStable: false } }), /restart/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, residuals: { ...fixtureReceipt.residuals, volumes: 1 } }), /residual/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, deletionReceipt: { count: 1, id: "receipt-delete", type: "workspace.deleted.v1" }
  }), /deletion receipt/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, deletion: { ...fixtureReceipt.deletion, workspaceKeyRetained: false }
  }), /owner deletion/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, deletion: { ...fixtureReceipt.deletion, runtimeId: "rt-alpha" }
  }), /owner deletion/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, deletion: { ...fixtureReceipt.deletion, workspaceApiKeyId: "71" }
  }), /owner deletion/);

  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, qualification: { authorityMode: "fixture", p0Ready: true } }), /authority classification/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, wallet: { ...fixtureReceipt.wallet, afterUsdMicros: "47420001" } }), /fixture wallet/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, authorityWriteCounts: { ...fixtureReceipt.authorityWriteCounts, debits: 2 } }), /authority write counts/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, authorityWriteCounts: { ...fixtureReceipt.authorityWriteCounts, refunds: 0 } }), /authority write counts/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, refund: { ...fixtureReceipt.refund, count: 0 } }), /platform refund/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, refund: { ...fixtureReceipt.refund, originalChargeOperationId: "charge-other" } }), /platform refund/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, refund: { ...fixtureReceipt.refund, walletOperationId: "" } }), /platform refund/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, refund: { ...fixtureReceipt.refund, code: "refund-without-owner-prefix" } }), /platform refund/);
  assert.throws(() => validateLocalQualificationReceipt({ ...fixtureReceipt, refund: { ...fixtureReceipt.refund, amountUsdMicros: "52580001" } }), /platform refund/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, wallet: { ...fixtureReceipt.wallet, afterDeleteUsdMicros: "47420000" }
  }), /fixture wallet/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, receipt: { ...fixtureReceipt.receipt, runtimeId: "rt-alpha" }
  }), /receipt binding/);
  // A receipt the owner publishes without resource fulfillment identities is
  // still bound by its receipt, quote, order, Workspace and charge. A partial set
  // is a mismatch, not a gap to fill from another owner's readback.
  assert.equal(validateLocalQualificationReceipt({
    ...fixtureReceipt,
    receipt: { ...fixtureReceipt.receipt, computeAllocationId: "", storageId: "", attachmentId: "" }
  }).receipt.quoteId, "quote-alpha");
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, receipt: { ...fixtureReceipt.receipt, attachmentId: "" }
  }), /receipt binding/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, receipt: { ...fixtureReceipt.receipt, quoteId: "" }
  }), /receipt binding/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, application: { ...fixtureReceipt.application, status: "pending" }
  }), /application installation/);
  assert.throws(() => validateLocalQualificationReceipt({
    ...fixtureReceipt, application: { ...fixtureReceipt.application, runtimeId: "rt-other" }
  }), /application installation/);
});

test("owner delete proves Gateway Secret removal and Gateway Key retention by their own owners", () => {
  const workspaceId = "ws-alpha";
  const keyName = workspaceReservedKeyName(workspaceId);
  // The provider secret ref is derived from the Workspace identity, exactly like
  // Contracts' WorkspaceGatewaySecretRef.
  assert.equal(workspaceGatewaySecretRef(workspaceId), `opl-gateway-${createHash("sha256").update(workspaceId).digest("hex").slice(0, 16)}`);
  // The authority's own key record is what the retention check reads: it has no
  // Control Plane `kind` projection, and the exact record must be unchanged.
  const authorityKey = { id: 71, userId: 41, name: keyName, groupId: 7, status: "active" };
  const authorityKeyRecord = { ...authorityKey, status: "disabled" };
  assert.equal(retainedWorkspaceKey([authorityKey], [authorityKeyRecord], workspaceId, "71"), true);
  assert.equal(retainedWorkspaceKey([authorityKey], [{ ...authorityKey, status: "active" }], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([authorityKey], [], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([authorityKey], [{ ...authorityKey, id: 72 }], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([authorityKey], [{ ...authorityKey, userId: 42 }], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([authorityKey], [{ ...authorityKey, name: `${keyName}-other` }], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([{ ...authorityKey, name: `${keyName}-other` }], [authorityKeyRecord], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([authorityKey], [authorityKeyRecord, { id: 72, userId: 41, name: `${keyName}-second`, groupId: 7, status: "active" }], workspaceId, "71"), false);
  assert.equal(retainedWorkspaceKey([authorityKey, { id: 72, userId: 41, name: `${keyName}-second`, groupId: 7, status: "active" }], [authorityKeyRecord], workspaceId, "71"), false);
  const stages = [
    { stage: "runtime_absent", result: "absent", evidenceKind: "provider_readback", observedAt: "t1", evidenceRef: "ref-1" },
    { stage: "attachment_absent", result: "released", evidenceKind: "local_transition", observedAt: "t2", evidenceRef: "ref-2" },
    { stage: "storage_absent", result: "absent", evidenceKind: "provider_readback", observedAt: "t3", evidenceRef: "ref-3" },
    { stage: "compute_absent", result: "absent", evidenceKind: "provider_readback", observedAt: "t4", evidenceRef: "ref-4" },
    { stage: "workspace_absent", result: "removed", evidenceKind: "local_transition", observedAt: "t5", evidenceRef: "ref-5" }
  ];
  assert.equal(workspaceDeleteReceiptStagesMatch(stages), true);
  assert.equal(workspaceDeleteReceiptStagesMatch(stages.slice(0, 4)), false);
  assert.equal(workspaceDeleteReceiptStagesMatch([...stages.slice(0, 4), { ...stages[4], stage: "receipt_recorded" }]), false);
  assert.equal(workspaceDeleteReceiptStagesMatch([{ ...stages[0], evidenceRef: "" }, ...stages.slice(1)]), false);
  assert.equal(workspaceDeleteReceiptStagesMatch([{ ...stages[0], result: "waiting" }, ...stages.slice(1)]), false);
});

test("local build proxy rejects credentials or URL parameters and errors redact the entire URL authority and query", () => {
  const previous = process.env.OPL_LOCAL_BUILD_PROXY;
  try {
    process.env.OPL_LOCAL_BUILD_PROXY = "socks5://host.docker.internal:10808";
    assert.deepEqual(localBuildProxyArgs(), ["--build-arg", "HTTPS_PROXY=socks5://host.docker.internal:10808"]);
    process.env.OPL_LOCAL_BUILD_PROXY = "socks5h://host.docker.internal:10808";
    assert.throws(() => localBuildProxyArgs(), /does not support socks5h:\/\/; use socks5:\/\//);
    process.env.OPL_LOCAL_BUILD_PROXY = "http://builder:secret@127.0.0.1:3128";
    assert.throws(() => localBuildProxyArgs(), /must not contain credentials/);
    assert.doesNotMatch(redactedError(new Error("pull https://builder:secret@example.test/v2 failed")), /builder|secret/);
    assert.match(redactedError(new Error("pull https://builder:secret@example.test/v2 failed")), /https:\/\/\[redacted\]@example\.test/);
    process.env.OPL_LOCAL_BUILD_PROXY = "https://proxy.example/path?token=s3cr3t";
    assert.throws(() => localBuildProxyArgs(), /must not contain query or fragment/);
    const queryRedacted = redactedError(new Error("build https://proxy.example/path?token=s3cr3t#credential failed"));
    assert.doesNotMatch(queryRedacted, /s3cr3t|credential/);
    assert.match(queryRedacted, /\?\[redacted\]/);
  } finally {
    if (previous === undefined) delete process.env.OPL_LOCAL_BUILD_PROXY;
    else process.env.OPL_LOCAL_BUILD_PROXY = previous;
  }
});

test("qualification source identity requires one clean unchanged HEAD and tree", () => {
  const source = { sha, tree: "d".repeat(40), clean: true };
  assert.deepEqual(validateQualificationSourceIdentity(source, source, sha), source);
  assert.throws(() => validateQualificationSourceIdentity({ ...source, clean: false }, source, sha), /clean/);
  assert.throws(() => validateQualificationSourceIdentity(source, { ...source, tree: "e".repeat(40) }, sha), /changed/);
  assert.throws(() => validateQualificationSourceIdentity(source, source, "f".repeat(40)), /requested source/);
});

test("sourceData and createHTTP/login preserve account mapping scope without Workspace mutation", async () => {
  const requests = [];
  let mappingPosts = 0;
  let workspacePosts = 0;
  const server = createServer(async (request, response) => {
    const url = new URL(request.url || "/", "http://qualification.test");
    requests.push({ method: request.method, path: url.pathname, cookie: request.headers.cookie || "", csrf: request.headers["x-opl-csrf"] || "" });
    response.setHeader("content-type", "application/json");
    if (request.method === "POST" && url.pathname === "/api/auth/login") {
      response.statusCode = 200;
      response.setHeader("set-cookie", ["opl_session=admin-session; Path=/; HttpOnly"]);
      response.setHeader("x-opl-csrf-token", "csrf-admin");
      response.end(JSON.stringify({ user: { accountId: "acct-admin", role: "admin" } }));
      return;
    }
    if (request.method === "POST" && url.pathname === "/api/operator/accounts") {
      assert.equal(request.headers.cookie, "opl_session=admin-session");
      assert.equal(request.headers["x-opl-csrf"], "csrf-admin");
      mappingPosts += 1;
      response.statusCode = 201;
      response.end(JSON.stringify({ status: "succeeded", accountId: "acct-mapped", operationId: "account-provision-mapped" }));
      return;
    }
    if (request.method === "GET" && url.pathname === "/api/operator/accounts") {
      assert.equal(request.headers.cookie, "opl_session=admin-session");
      assert.equal(request.headers["x-opl-csrf"], "csrf-admin");
      response.statusCode = 200;
      response.end(JSON.stringify({ source: "control-plane+sub2api", available: true, status: "available", data: {
        items: [{ accountId: "acct-mapped", email: "customer@example.test", status: "active" }], total: 1, page: 1, pageSize: 50
      } }));
      return;
    }
    if (request.method === "POST" && url.pathname === "/api/workspace-launches") workspacePosts += 1;
    response.statusCode = 404;
    response.end(JSON.stringify({ error: "not_found" }));
  });
  await new Promise((resolvePromise, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolvePromise);
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  const http = createHTTP(`http://127.0.0.1:${address.port}`);
  try {
    const admin = await login(http, "admin@example.test", "admin-password");
    assert.deepEqual(admin, { cookie: "opl_session=admin-session", csrf: "csrf-admin" });
    const mapping = await http.json("/api/operator/accounts", {
      method: "POST", headers: { "idempotency-key": "account-provision-mapped" },
      body: { email: "customer@example.test", password: "customer-password" }
    }, admin, [201]);
    assert.equal(mapping.payload.accountId, "acct-mapped");
    const readback = await http.json("/api/operator/accounts?page=1&pageSize=50", {}, admin);
    const accountPage = sourceData(readback.payload, "control-plane+sub2api");
    assert.equal(accountPage.items[0].accountId, "acct-mapped");
    assert.throws(() => sourceData({ source: "control-plane", available: true, status: "available", data: accountPage }, "control-plane+sub2api"), /unavailable/);
    assert.throws(() => sourceData({ source: "control-plane+sub2api", available: false, status: "unavailable", data: accountPage }, "control-plane+sub2api"), /unavailable/);
    const qualification = await login(http, "customer@example.test", "customer-password");
    assert.deepEqual(qualification, { cookie: "opl_session=admin-session", csrf: "csrf-admin" });
    assert.equal(mappingPosts, 1);
    assert.equal(workspacePosts, 0);
    assert.deepEqual(requests.map(({ method, path }) => `${method}:${path}`), [
      "POST:/api/auth/login", "POST:/api/operator/accounts", "GET:/api/operator/accounts", "POST:/api/auth/login"
    ]);
  } finally {
    await new Promise((resolvePromise) => server.close(() => resolvePromise()));
  }
});

for (const scenario of [
  { name: "covers every live stage and validates exact local cleanup", status: 200, marker: true },
  { name: "classifies HTTP 502 without a marker", status: 502, marker: false, errorCode: "workspace_open_http_failed", error: "Workspace Runtime open returned HTTP 502" },
  { name: "rejects HTTP 502 even with the fixture marker", status: 502, marker: true, errorCode: "workspace_open_http_failed", error: "Workspace Runtime open returned HTTP 502" },
  { name: "rejects HTTP 200 without the fixture marker", status: 200, marker: false, errorCode: "workspace_open_fixture_marker_missing", error: "Workspace Runtime open is missing the qualification fixture marker" },
  { name: "rejects HTTP 204 without the fixture marker", status: 204, marker: false, errorCode: "workspace_open_fixture_marker_missing", error: "Workspace Runtime open is missing the qualification fixture marker" }
]) {
test(`canonical J1 HTTP preview ${scenario.name}`, async () => {
  const accountEmail = "customer@example.test";
  const accountPassword = "customer-password";
  const accountProvisionKey = "account-provision-j1";
  const accountId = `acct-${stableID("account", accountEmail).slice(0, 18)}`;
  const accountOperationId = `account-provision-${stableID(accountProvisionKey, accountEmail).slice(0, 18)}`;
  const launchKey = "local-qualification:j1";
  // The owner mints the operation identity and the Workspace identity of a new
  // purchase, so the fixture never derives them.
  const operationId = "workspace-operation-j1";
  const workspaceId = "ws-owner-j1";
  const runtimeVersionId = "rv-j1";
  const computePlanId = "cp-j1";
  const storagePlanId = "sp-j1";
  const receiptId = "receipt-j1";
  const runtimeId = "runtime-j1";
  const applicationOperationId = "workspace-application-deploy-j1";
  const keyId = "700";
  const keyName = workspaceReservedKeyName(workspaceId);
  const debitCode = "opl:j1-debit";
  const amountUsdMicros = "52580000";
  let entryURL = "";
  const resourceFulfillment = { computeAllocationId: "compute-j1", storageId: "storage-j1", attachmentId: "attachment-j1" };
  const historicalKey = { id: "699", name: "opl-workspace-historical", kind: "workspace", status: "active" };
  const historicalReceipt = {
    receiptId: "receipt-historical", operationId: "workspace-launch-historical", workspaceId: "ws-historical",
    type: "billing.workspace_purchased.v1", status: "completed", chargeReference: "opl:historical-debit",
    totalUsdMicros: "52580000", fulfillment: { computeAllocationId: "compute-historical", storageId: "storage-historical", attachmentId: "attachment-historical" }
  };
  const requests = [];
  const ownerCommands = [];
  const counts = { mappingPosts: 0, quotePosts: 0, workspacePosts: 0, keyCreates: 0, debits: 0, refunds: 0, deletes: 0, restarts: 0 };
  let ownerReads = 0;
  let server;
  const envelope = (source, data, status = "available") => ({ source, available: true, status, data });
  const send = (response, status, payload, headers = {}) => {
    response.writeHead(status, { "content-type": "application/json", ...headers });
    response.end(JSON.stringify(payload));
  };
  const readBody = (request) => new Promise((resolvePromise) => {
    let text = "";
    request.on("data", (chunk) => { text += chunk; });
    request.on("end", () => resolvePromise(text ? JSON.parse(text) : null));
  });
  const ownerOperation = (status = "succeeded") => ({
    operationId, owner: "workspace", kind: "create_workspace", resourceId: workspaceId, status,
    stage: status === "succeeded" ? "succeeded" : "creating", requestId: "request-j1"
  });
  const currentApplication = () => ({
    operationId: applicationOperationId, applicationId: "opl-app", revision: "revision-j1", status: "ready", entryUrl: entryURL
  });
  server = createServer(async (request, response) => {
    const url = new URL(request.url || "/", "http://qualification.test");
    const method = request.method || "GET";
    requests.push({ method, path: url.pathname });
    if (method === "GET" && url.pathname === "/api/healthz") return send(response, 200, { status: "ok" });
    if (method === "GET" && url.pathname === "/") { response.writeHead(200, { "content-type": "text/html" }); response.end("<div id=\"root\"><script src=\"/assets/app.js\"></script></div>"); return; }
    if (method === "GET" && url.pathname === "/assets/app.js") { response.writeHead(200, { "content-type": "application/javascript" }); response.end("console.log('stub')"); return; }
    if (method === "POST" && url.pathname === "/api/auth/login") {
      const body = await new Promise((resolvePromise) => { let text = ""; request.on("data", (chunk) => { text += chunk; }); request.on("end", () => resolvePromise(JSON.parse(text))); });
      const admin = body.email === "admin@example.test";
      const customer = body.email === accountEmail;
      if (!admin && !customer) return send(response, 401, { error: "invalid_credentials" });
      return send(response, 200, { user: { accountId: admin ? "acct-admin" : accountId, role: admin ? "admin" : "owner" } }, { "set-cookie": `${admin ? "admin" : "customer"}=session; Path=/`, "x-opl-csrf-token": admin ? "csrf-admin" : "csrf-customer" });
    }
    if (method === "POST" && url.pathname === "/api/operator/accounts") {
      counts.mappingPosts += 1;
      return send(response, 201, { status: "succeeded", accountId, operationId: accountOperationId });
    }
    if (method === "GET" && url.pathname === "/api/operator/accounts") {
      return send(response, 200, envelope("control-plane+sub2api", { items: [{ accountId, email: accountEmail, status: "active", sub2apiUserId: "41" }], total: 1, page: 1, pageSize: 50 }));
    }
    if (method === "GET" && url.pathname === "/api/auth/me") return send(response, 200, envelope("sub2api", { accountId, email: accountEmail, role: "owner", status: "active", sub2apiUserId: "41" }));
    // The CloudIdentity login the browser performs: a login context that carries
    // a double-submit CSRF challenge, and a command that presents it beside the
    // challenge cookie.
    if (method === "GET" && url.pathname === "/api/v2/auth/context") {
      return send(response, 200, { csrfToken: "owner-csrf-token" }, {
        "set-cookie": [
          "opl_login_context=challenge; Path=/api/v2/auth; HttpOnly",
          "opl_login_csrf=challenge-hash; Path=/api/v2/auth; HttpOnly",
        ],
      });
    }
    if (method === "POST" && url.pathname === "/api/v2/auth/login") {
      const body = await readBody(request);
      if (body?.username !== accountEmail && body?.username !== "admin@example.test") {
        return send(response, 401, { error: "invalid_credentials" });
      }
      return send(response, 200, {
        actorId: "41", displayName: accountEmail, tenantId: "tenant-j1",
        permissions: [], csrfToken: "owner-csrf-token", expiresAt: "2026-10-18T12:00:00Z",
      }, { "set-cookie": "opl_console_session=owner-session; Path=/; HttpOnly" });
    }
    if (method === "GET" && url.pathname === "/api/v2/auth/session") {
      return send(response, 200, {
        actorId: "41", displayName: accountEmail, tenantId: "tenant-j1",
        permissions: [], csrfToken: "owner-csrf-token", expiresAt: "2026-10-18T12:00:00Z",
      });
    }
    if (method === "GET" && url.pathname === "/api/v2/tenant") {
      return send(response, 200, { id: "tenant-j1", name: "J1 Tenant", status: "active", billingSub2apiUserId: "41" });
    }
    // The wallet the Gateway owner holds, read under the owner session.
    if (method === "GET" && url.pathname === "/api/v2/wallet") {
      return send(response, 200, {
        source: "gateway", status: "available", balanceUSDMicros: counts.debits ? "947420000" : "1000000000",
        currency: "USD", fetchedAt: "2026-10-18T00:00:00Z",
      });
    }
    if (method === "GET" && url.pathname === "/api/gateway/wallet") return send(response, 200, envelope("sub2api", { userId: "41", currency: "USD", usdMicros: counts.debits ? "947420000" : "1000000000", status: "active" }));
    if (method === "GET" && url.pathname === "/api/gateway/usage-summary") return send(response, 200, envelope("sub2api", { totalRequests: 0 }));
    if (method === "GET" && url.pathname === "/api/gateway/keys") {
      const items = counts.keyCreates ? [historicalKey, { id: keyId, name: keyName, kind: "workspace", status: "active" }] : [historicalKey];
      return send(response, 200, envelope("sub2api", { items, total: items.length, page: 1, pageSize: 100, pages: 1 }));
    }
    if (method === "GET" && url.pathname === "/api/v2/catalog/runtime-versions") {
      return send(response, 200, { items: [{ id: "rv-deprecated", status: "deprecated" }, { id: runtimeVersionId, status: "approved" }] });
    }
    if (method === "GET" && url.pathname === "/api/v2/catalog/compute-plans") {
      return send(response, 200, { items: [{ id: "cp-retired", availability: "retired" }, { id: computePlanId, availability: "available" }] });
    }
    if (method === "GET" && url.pathname === "/api/v2/catalog/storage-plans") {
      return send(response, 200, { items: [{ id: storagePlanId, availability: "available" }] });
    }
    if (method === "POST" && url.pathname === "/api/v2/quotes") {
      counts.quotePosts += 1;
      ownerCommands.push({ path: url.pathname, idempotencyKey: request.headers["idempotency-key"], body: await readBody(request) });
      return send(response, 201, {
        id: "quote-j1", purpose: "deploy", runtimeVersionId, computePlanId, storagePlanId,
        modelSelections: [], periodMonths: 1, totalUSDMicros: amountUsdMicros, status: "offered"
      });
    }
    if (method === "GET" && url.pathname === "/api/v2/quotes/quote-j1") {
      return send(response, 200, { id: "quote-j1", purpose: "deploy", runtimeVersionId, computePlanId, storagePlanId,
        modelSelections: [], periodMonths: 1, totalUSDMicros: amountUsdMicros, status: counts.workspacePosts ? "accepted" : "offered" });
    }
    if (method === "POST" && url.pathname === "/api/v2/workspaces") {
      counts.workspacePosts += 1; counts.keyCreates += 1; counts.debits += 1;
      ownerCommands.push({ path: url.pathname, idempotencyKey: request.headers["idempotency-key"], body: await readBody(request) });
      return send(response, 202, ownerOperation("accepted"));
    }
    if (method === "GET" && url.pathname === `/api/v2/operations/workspace/${operationId}`) {
      ownerReads += 1;
      return send(response, 200, ownerOperation(ownerReads === 1 ? "accepted" : "succeeded"));
    }
    if (method === "GET" && url.pathname === `/api/v2/workspaces/${workspaceId}`) {
      return send(response, 200, { id: workspaceId, name: "J1", status: "running",
        capabilityVersionId: "capability-j1", applicationAvailability: "available", accessUrl: entryURL });
    }
    if (method === "GET" && url.pathname === `/api/v2/workspaces/${workspaceId}/deployments`) {
      return send(response, 200, { items: [{ id: applicationOperationId, workspaceId, status: "active",
        runtimeInstanceId: runtimeId, capabilityVersionId: "capability-j1" }] });
    }
    if (method === "GET" && url.pathname === `/api/v2/workspaces/${workspaceId}/transactions`) {
      return send(response, 200, { items: counts.debits ? [{
        id: "wallet-charge-j1", workspaceId, kind: "charge", status: "confirmed",
        amountUSDMicros: amountUsdMicros, receiptId, externalReference: debitCode,
        createdAt: "2026-10-18T00:00:05Z", updatedAt: "2026-10-18T00:00:05Z",
      }] : [] });
    }
    if (method === "GET" && url.pathname === `/api/v2/workspaces/${workspaceId}/financial-receipts/${receiptId}`) {
      return send(response, 200, {
        workspaceId,
        receipt: { id: receiptId, kind: "wallet_action", owner: "workspace", outcome: "confirmed", operationId: operationId,
          evidenceSummary: "confirmed charge", createdAt: "2026-10-18T00:00:05Z" },
        walletOperation: { id: "wallet-charge-j1", workspaceId, kind: "charge", status: "confirmed",
          amountUSDMicros: amountUsdMicros, receiptId, externalReference: debitCode,
          createdAt: "2026-10-18T00:00:05Z", updatedAt: "2026-10-18T00:00:05Z" },
        quote: { id: "quote-j1", status: "accepted", totalUSDMicros: amountUsdMicros, lineItems: [] },
        evidenceDigest: `sha256:${"e".repeat(64)}`,
      });
    }
    if (method === "GET" && url.pathname === `/api/billing/receipts/${receiptId}`) return send(response, 200, envelope("ledger", { receiptId, accountId, operationId, workspaceId, type: "billing.workspace_purchased.v1", status: "completed", chargeReference: debitCode, totalUsdMicros: amountUsdMicros, fulfillment: resourceFulfillment }));
    if (method === "GET" && url.pathname === "/api/billing/receipts") {
      const currentReceipt = { receiptId, accountId, operationId, workspaceId, type: "billing.workspace_purchased.v1", status: "completed", chargeReference: debitCode, totalUsdMicros: amountUsdMicros, fulfillment: resourceFulfillment };
      return send(response, 200, envelope("ledger", { receipts: counts.debits ? [historicalReceipt, currentReceipt] : [historicalReceipt], hasMore: false, nextCursor: "" }));
    }
    if (method === "GET" && url.pathname === `/api/gateway/keys/${keyId}`) return send(response, 200, envelope("sub2api", { id: keyId, kind: "workspace", status: "active" }));
    if (method === "GET" && url.pathname === `/api/gateway/keys/${keyId}/usage-summary`) return send(response, 200, envelope("sub2api", { totalRequests: 0 }));
    if (method === "GET" && url.pathname === "/api/gateway/balance-history") return send(response, 200, envelope("sub2api", { items: [{ valueUsdMicros: `-${amountUsdMicros}`, status: "used" }], total: 1, page: 1, pageSize: 20, pages: 1 }));
    return send(response, 404, { error: "not_found" });
  });
  const privateBody = "body-private-value https://private-entry.example.test/path?token=body-secret";
  const privateHeader = "header-private-value";
  const privateStatusText = "status-text-private-value";
  let entryRequests = 0;
  const entryServer = createServer((_request, response) => {
    entryRequests += 1;
    assert.equal(stages.at(-1), "workspace_open");
    response.writeHead(scenario.status, privateStatusText, { "content-type": "text/html", "x-private-value": privateHeader });
    response.end(`<html>${scenario.marker ? "OPL Workspace READY" : "unqualified application"} ${privateBody}</html>`);
  });
  await new Promise((resolvePromise, reject) => { entryServer.once("error", reject); entryServer.listen(0, "127.0.0.1", resolvePromise); });
  entryURL = `http://127.0.0.1:${entryServer.address().port}/`;
  await new Promise((resolvePromise, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolvePromise); });
  const address = server.address();
  const http = createHTTP(`http://127.0.0.1:${address.port}`);
  const stages = [];
  const cleanupCalls = [];
  const j0Root = await mkdtemp(join(tmpdir(), "opl-j0-top-level-"));
  const j0Path = join(j0Root, "ready.json");
  const outputPath = join(j0Root, "j1.json");
  await writeFile(j0Path, JSON.stringify(j0ReadyReceipt(sha, "d".repeat(40))), { mode: 0o600 });
  const installation = {
    OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT: process.env.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT,
    OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: process.env.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON
  };
  try {
    const options = parseLocalQualificationArgs([
      "--source-sha", sha, "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
      "--workspace-image", workspaceReference, "--receipt", outputPath,
      "--authority-mode", "live", "--j0-ready-receipt", j0Path
    ]);
    // The store stack binds the installation-owned Local-Docker facts, so a run
    // that starts it has to supply them before Compose is invoked.
    process.env.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT = "/srv/opl-workspaces";
    process.env.OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON = JSON.stringify({
      schemaVersion: 1,
      packages: [{ id: "basic", name: "Basic Workspace", available: true, compute: { id: "local-basic", server: "2c4g", cpu: 2, memoryGb: 4, instanceType: "local-2c4g" }, storage: { sizeGb: 10, quotaPolicy: "linux-project" } }]
    });
    let runtimeReadbacks = 0;
    let debitReadbacks = 0;
    const qualify = () => runLocalWorkspaceQualification(options, {
      loadLiveAuthority: async () => ({ baseURL: "https://sandbox.example.test", adminEmail: "admin@example.test", adminPassword: "admin-password", authorityClass: "sandbox", qualificationUserEmail: "admin@example.test", qualificationUserPassword: "admin-password" }),
      readSourceIdentity: async () => ({ sha, tree: "d".repeat(40), clean: true }),
      runLiveJ1: async ({ j0Ready }) => runLocalWorkspaceJ1HTTPQualification({
        http, adminEmail: "admin@example.test", adminPassword: "admin-password", qualificationEmail: accountEmail, qualificationPassword: accountPassword,
        accountProvisionKey, launchKey, workspaceName: "J1", wait: async () => {},
        onStage: (stage) => stages.push(stage),
        readRuntime: async ({ runtimeId: observedRuntimeId }) => {
          runtimeReadbacks += 1;
          if (observedRuntimeId !== runtimeId) throw new Error("Runtime readback identity is invalid");
          return { runningDigest: workspaceDigest };
        },
        readDebit: async ({ code, sub2apiUserId, amountUsdMicros: value }) => {
          debitReadbacks += 1;
          return { code, userId: sub2apiUserId, amountUsdMicros: value, count: 1 };
        },
        cleanup: async (scope) => { cleanupCalls.push(scope); return { containers: 0, volumes: 0, networks: 0 }; },
        receiptBase: {
          schemaVersion: 1, status: "READY", source: { sha, tree: "d".repeat(40) },
          images: { cloud: { input: `ghcr.io/example/cloud@${cloudDigest}`, repoDigest: `ghcr.io/example/cloud@${cloudDigest}`, digest: cloudDigest, runningDigest: cloudDigest }, workspace: { input: workspaceReference, repoDigest: workspaceReference, digest: workspaceDigest, runningDigest: workspaceDigest } },
          command: "npm run qualify:local:workspace", processes: { console: "ready", controlPlane: "ready", fabric: "ready", ledger: "ready" }, stores: { controlPlane: "durable", fabric: "durable", ledger: "durable", ownerSeparated: true },
          j0Ready: { ...j0Ready, digest: j0Ready.digest },
          qualification: { authorityMode: "live", p0Ready: true }, deferred: []
        }
      })
    });
    if (scenario.errorCode) {
      let failure;
      await assert.rejects(qualify, (error) => {
        failure = error;
        assert.equal(error.code, scenario.errorCode);
        assert.equal(error.message, scenario.error);
        assert.equal(redactedError(error), scenario.error);
        return true;
      });
      assert.deepEqual(stages, ["bootstrap_ready", "admin_login", "account_provision", "qualification_login", "wallet_usage_baseline", "catalog_resolution", "workspace_order", "application_installation", "terminal_readback", "workspace_open"]);
      assert.equal(runtimeReadbacks, 1);
      assert.equal(debitReadbacks, 0);
      assert.equal(cleanupCalls.length, 1);
      assert.equal(cleanupCalls[0].failed, true);
      assert.equal(cleanupCalls[0].launchSubmitted, true);
      assert.equal(cleanupCalls[0].accountId, accountId);
      assert.equal(cleanupCalls[0].workspaceId, workspaceId);
      // Settle the actual lifecycle failure through the same NOT_READY writer
      // used by the Docker runner, including preservation through scratch failure.
      await assert.rejects(() => finishLocalWorkspaceQualification({
        ...qualificationSettlement(j0Root, outputPath, failure),
        options,
        stage: stages.at(-1)
      }, {
        removeScratch: async () => { throw Object.assign(new Error("scratch-private-value"), { code: "EACCES" }); }
      }), (error) => error === failure);
      const writtenReceipt = JSON.parse(await readFile(outputPath, "utf8"));
      assert.equal(writtenReceipt.status, "NOT_READY");
      assert.equal(writtenReceipt.stage, "workspace_open");
      assert.equal(writtenReceipt.errorCode, scenario.errorCode);
      assert.equal(writtenReceipt.error, scenario.error);
      const diagnostics = JSON.stringify({ code: failure.code, error: redactedError(failure), stage: writtenReceipt.stage, errorCode: writtenReceipt.errorCode, receiptError: writtenReceipt.error });
      for (const forbidden of [privateBody, "body-private-value", "private-entry.example.test", "body-secret", privateHeader, privateStatusText, entryURL, "127.0.0.1", accountPassword, "scratch-private-value"]) {
        assert.equal(diagnostics.includes(forbidden), false, forbidden);
      }
    } else {
      const result = await qualify();
      assert.equal(result.status, "READY");
      const writtenReceipt = JSON.parse(await readFile(outputPath, "utf8"));
      assert.equal(writtenReceipt.j0Ready.digest, result.j0Ready.digest);
      assert.deepEqual(writtenReceipt.j0Ready.source, { sha, tree: "d".repeat(40), clean: true });
      // The recorded identities are the ones the owner minted for this purchase.
      assert.equal(writtenReceipt.identities.operationId, operationId);
      assert.equal(writtenReceipt.identities.workspaceId, workspaceId);
      assert.equal(writtenReceipt.identities.runtimeVersionId, runtimeVersionId);
      assert.equal(writtenReceipt.identities.computePlanId, computePlanId);
      assert.equal(writtenReceipt.identities.storagePlanId, storagePlanId);
      assert.equal(writtenReceipt.receipt.operationId, operationId);
      assert.equal(writtenReceipt.debit.operationId, operationId);
      assert.deepEqual(stages, ["bootstrap_ready", "admin_login", "account_provision", "qualification_login", "wallet_usage_baseline", "catalog_resolution", "workspace_order", "application_installation", "terminal_readback", "workspace_open", "accounting_readback", "receipt_validation", "qualification_cleanup"]);
      assert.deepEqual(cleanupCalls, [{ accountId, workspaceId }]);
      assert.equal(runtimeReadbacks, 1);
      assert.equal(debitReadbacks, 1);
    }
    assert.equal(entryRequests, 1);
    assert.deepEqual(counts, { mappingPosts: 1, quotePosts: 1, workspacePosts: 1, keyCreates: 1, debits: 1, refunds: 0, deletes: 0, restarts: 0 });
    // The buyer declares the application and the plan pair it buys, and the
    // owner prices and admits exactly that declaration.
    assert.deepEqual(ownerCommands.map((command) => command.path), ["/api/v2/quotes", "/api/v2/workspaces"]);
    assert.deepEqual(ownerCommands[0].body, {
      purpose: "deploy",
      applicationSelection: { kind: "opl_app", runtimeVersionId },
      computePlanId, storagePlanId, modelSelections: [], periodMonths: 1
    });
    assert.deepEqual(ownerCommands[1].body, { name: "J1", quoteId: "quote-j1", renewalMode: "manual" });
    // The buyer's own idempotency identity is stable across the two commands,
    // so a repeated delivery replays the same quote and the same order.
    assert.equal(ownerCommands[0].idempotencyKey, `${launchKey}:quote`);
    assert.equal(ownerCommands[1].idempotencyKey, launchKey);
    assert.equal(requests.filter((request) => request.method === "POST" && request.path === "/api/v2/workspaces").length, 1);
    assert.equal(requests.filter((request) => request.method === "POST" && request.path === "/api/v2/quotes").length, 1);
    assert.equal(requests.filter((request) => request.method === "GET" && request.path === `/api/v2/operations/workspace/${operationId}`).length >= 1, true);
    assert.equal(requests.some((request) => request.path === "/api/workspace-launches"), false);
    assert.equal(requests.some((request) => request.method === "DELETE"), false);
    assert.equal(requests.some((request) => request.path.includes("refund")), false);
  } finally {
    for (const [name, value] of Object.entries(installation)) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
    await new Promise((resolvePromise) => server.close(() => resolvePromise()));
    await new Promise((resolvePromise) => entryServer.close(() => resolvePromise()));
    await rm(j0Root, { recursive: true, force: true });
  }
});
}

test("canonical J1 HTTP preview cleans a failure before the owner Workspace order", async () => {
  const cleanupCalls = [];
  const http = {
    json: async (path) => {
      if (path === "/api/healthz") return { payload: { status: "ok" } };
      throw new Error("unexpected request after conflict");
    },
    request: async (path) => path === "/" ? { response: { status: 200 }, text: "<div id=\"root\"><script src=\"/app.js\"></script></div>" } :
      { response: { status: 200 }, text: "asset" }
  };
  http.json = async (path) => {
    if (path === "/api/healthz") return { payload: { status: "ok" } };
    if (path === "/api/auth/login") return { response: { headers: new Headers({ "set-cookie": "admin=session", "x-opl-csrf-token": "csrf" }) }, payload: { user: { accountId: "acct-admin" } } };
    if (path === "/api/operator/accounts") return { response: { status: 201 }, payload: { status: "succeeded", accountId: "acct-wrong", operationId: "account-provision-wrong" } };
    throw new Error("unexpected request after conflict");
  };
  await assert.rejects(() => runLocalWorkspaceJ1HTTPQualification({
    http, adminEmail: "admin@example.test", adminPassword: "password", qualificationEmail: "customer@example.test", qualificationPassword: "password",
    accountProvisionKey: "mapping", launchKey: "launch", workspaceName: "J1",
    readRuntime: async () => ({}), readDebit: async () => ({}), cleanup: async (scope) => { cleanupCalls.push(scope); return { containers: 0, volumes: 0, networks: 0 }; }, receiptBase: {}
  }), /provision response/);
  assert.deepEqual(cleanupCalls, [{ failed: true, launchSubmitted: false, recoveryAuthority: null }]);
  assert.throws(() => sourceData({ source: "control-plane+sub2api", available: true, status: "conflict", data: {} }, "control-plane+sub2api"), /unavailable/);
});

test("canonical J1 HTTP preview preserves the recovery scope after an owner Workspace order submission", async () => {
  const accountId = `acct-${stableID("account", "customer@example.test").slice(0, 18)}`;
  const provisionOperationId = `account-provision-${stableID("mapping", "customer@example.test").slice(0, 18)}`;
  const cleanupCalls = [];
  // The owner mints the operation identity and the Workspace identity, so the
  // fixture states them instead of deriving them.
  const operationId = "workspace-operation-owner";
  const workspaceId = "ws-owner-minted";
  const ownerCommands = [];
  const http = {
    request: async (path) => path === "/"
      ? { response: { status: 200 }, text: '<div id="root"><script src="/app.js"></script></div>' }
      : { response: { status: 200 }, text: "asset" },
    json: async (path, init = {}) => {
      if (path === "/api/healthz") return { payload: { status: "ok" } };
      if (path === "/api/auth/login") return { response: { headers: new Headers({ "set-cookie": "session=test", "x-opl-csrf-token": "csrf" }) }, payload: { user: { accountId: init.body.email === "customer@example.test" ? accountId : "acct-admin" } } };
      if (path === "/api/v2/auth/context") {
        return { response: { headers: ownerLoginContextHeaders() }, payload: { csrfToken: "owner-csrf-token" } };
      }
      if (path === "/api/v2/auth/login") {
        return { response: { headers: ownerSessionHeaders() }, payload: ownerSessionPayload };
      }
      if (path === "/api/v2/auth/session") {
        return { payload: ownerSessionPayload };
      }
      if (path === "/api/v2/tenant") return { payload: { id: "tenant-j1", name: "J1 Tenant", status: "active", billingSub2apiUserId: "41" } };
      if (path === "/api/v2/wallet") {
        return { payload: ownerWalletPayload };
      }
      if (path === "/api/operator/accounts" && init.method === "POST") return { payload: { status: "succeeded", accountId, operationId: provisionOperationId } };
      if (path.startsWith("/api/operator/accounts?")) return { payload: { source: "control-plane+sub2api", available: true, status: "available", data: { items: [{ accountId, email: "customer@example.test", status: "active", sub2apiUserId: "41" }], total: 1, page: 1, pageSize: 50 } } };
      if (path === "/api/auth/me") return { payload: { source: "sub2api", available: true, status: "available", data: { accountId, email: "customer@example.test", role: "owner", status: "active", sub2apiUserId: "41" } } };
      if (path === "/api/gateway/wallet") return { payload: { source: "sub2api", available: true, status: "available", data: { usdMicros: "100000000" } } };
      if (path === "/api/gateway/usage-summary?period=month") return { payload: { source: "sub2api", available: true, status: "available", data: { totalRequests: 0 } } };
      if (path === "/api/gateway/keys?page=1&pageSize=100") return { payload: { source: "sub2api", available: true, status: "available", data: { items: [], total: 0, page: 1, pageSize: 100, pages: 1 } } };
      if (path === "/api/billing/receipts?limit=100") return { payload: { source: "ledger", available: true, status: "available", data: { receipts: [], hasMore: false } } };
      if (path.startsWith("/api/v2/catalog/runtime-versions")) return { payload: { items: [{ id: "rv-owner", status: "approved" }] } };
      if (path.startsWith("/api/v2/catalog/compute-plans")) return { payload: { items: [{ id: "cp-owner", availability: "available" }] } };
      if (path.startsWith("/api/v2/catalog/storage-plans")) return { payload: { items: [{ id: "sp-owner", availability: "available" }] } };
      if (path === "/api/v2/quotes") {
        ownerCommands.push({ path, idempotencyKey: init.headers["idempotency-key"] });
        return { payload: { id: "quote-owner", purpose: "deploy", runtimeVersionId: "rv-owner", computePlanId: "cp-owner", storagePlanId: "sp-owner", modelSelections: [], periodMonths: 1, totalUSDMicros: "52580000", status: "offered" } };
      }
      if (path === "/api/v2/quotes/quote-owner") return { payload: { id: "quote-owner", purpose: "deploy", runtimeVersionId: "rv-owner", computePlanId: "cp-owner", storagePlanId: "sp-owner", modelSelections: [], periodMonths: 1, totalUSDMicros: "52580000", status: "accepted" } };
      if (path === "/api/v2/workspaces" && init.method === "POST") {
        ownerCommands.push({ path, idempotencyKey: init.headers["idempotency-key"] });
        return { payload: { operationId, owner: "workspace", kind: "create_workspace", resourceId: workspaceId, status: "accepted", stage: "creating" } };
      }
      if (path === `/api/v2/workspaces/${workspaceId}`) return { payload: null };
      if (path === `/api/v2/operations/workspace/${operationId}`) {
        return { payload: { operationId, owner: "workspace", kind: "create_workspace", resourceId: workspaceId, status: "succeeded", stage: "succeeded" } };
      }
      if (path.startsWith("/api/workspaces?")) return { payload: { source: "control-plane", available: true, status: "available", data: { items: [], total: 0, page: 1, pageSize: 20 } } };
      throw new Error(`unexpected request ${path}`);
    }
  };
  await assert.rejects(() => runLocalWorkspaceJ1HTTPQualification({
    http, adminEmail: "admin@example.test", adminPassword: "password", qualificationEmail: "customer@example.test", qualificationPassword: "password",
    accountProvisionKey: "mapping", launchKey: "launch", workspaceName: "J1",
    wait: async () => {}, readRuntime: async () => ({}), readDebit: async () => ({}),
    cleanup: async (scope) => { cleanupCalls.push(scope); return null; }, receiptBase: {}
  }), /Workspace owner readback is invalid/);
  assert.equal(cleanupCalls.length, 1);
  assert.equal(cleanupCalls[0].failed, true);
  assert.equal(cleanupCalls[0].launchSubmitted, true);
  // The recovery scope names the identities the owner minted for this purchase.
  assert.equal(cleanupCalls[0].operationId, operationId);
  assert.equal(cleanupCalls[0].workspaceId, workspaceId);
  // The owner operation does not publish the retained per-resource write
  // counters, so the preserved stack remains the recovery authority.
  assert.equal(cleanupCalls[0].recoveryAuthority, null);
  // The purchase went to the owner and was admitted exactly once.
  assert.deepEqual(ownerCommands.map((command) => command.path), ["/api/v2/quotes", "/api/v2/workspaces"]);
  assert.deepEqual(ownerCommands.map((command) => command.idempotencyKey), ["launch:quote", "launch"]);
});

test("canonical J1 HTTP preview still forbids cleanup when the post-Create owner readback is unavailable", async () => {
  let submitted = false;
  const ownerCommands = [];
  const cleanupCalls = [];
  const accountId = `acct-${stableID("account", "customer@example.test").slice(0, 18)}`;
  const http = {
    request: async (path) => path === "/"
      ? { response: { status: 200 }, text: '<div id="root"><script src="/app.js"></script></div>' }
      : { response: { status: 200 }, text: "asset" },
    json: async (path, init = {}) => {
      if (path === "/api/healthz") return { payload: { status: "ok" } };
      if (path === "/api/auth/login") return { response: { headers: new Headers({ "set-cookie": "session=test", "x-opl-csrf-token": "csrf" }) }, payload: { user: { accountId: init.body.email === "customer@example.test" ? accountId : "acct-admin" } } };
      if (path === "/api/v2/auth/context") {
        return { response: { headers: ownerLoginContextHeaders() }, payload: { csrfToken: "owner-csrf-token" } };
      }
      if (path === "/api/v2/auth/login") {
        return { response: { headers: ownerSessionHeaders() }, payload: ownerSessionPayload };
      }
      if (path === "/api/v2/auth/session") {
        return { payload: ownerSessionPayload };
      }
      if (path === "/api/v2/tenant") return { payload: { id: "tenant-j1", name: "J1 Tenant", status: "active", billingSub2apiUserId: "41" } };
      if (path === "/api/v2/wallet") {
        return { payload: ownerWalletPayload };
      }
      if (path === "/api/operator/accounts" && init.method === "POST") return { payload: { status: "succeeded", accountId, operationId: `account-provision-${stableID("mapping", "customer@example.test").slice(0, 18)}` } };
      if (path.startsWith("/api/operator/accounts?")) return { payload: { source: "control-plane+sub2api", available: true, status: "available", data: { items: [{ accountId, email: "customer@example.test", status: "active", sub2apiUserId: "41" }], total: 1, page: 1, pageSize: 50 } } };
      if (path === "/api/auth/me") return { payload: { source: "sub2api", available: true, status: "available", data: { accountId, email: "customer@example.test", role: "owner", status: "active", sub2apiUserId: "41" } } };
      if (path === "/api/gateway/wallet") return { payload: { source: "sub2api", available: true, status: "available", data: { usdMicros: "100000000" } } };
      if (path === "/api/gateway/usage-summary?period=month") return { payload: { source: "sub2api", available: true, status: "available", data: { totalRequests: 0 } } };
      if (path === "/api/gateway/keys?page=1&pageSize=100") return { payload: { source: "sub2api", available: true, status: "available", data: { items: [], total: 0, page: 1, pageSize: 100, pages: 1 } } };
      if (path === "/api/billing/receipts?limit=100") return { payload: { source: "ledger", available: true, status: "available", data: { receipts: [], hasMore: false } } };
      if (path.startsWith("/api/v2/catalog/runtime-versions")) return { payload: { items: [{ id: "rv-owner", status: "approved" }] } };
      if (path.startsWith("/api/v2/catalog/compute-plans")) return { payload: { items: [{ id: "cp-owner", availability: "available" }] } };
      if (path.startsWith("/api/v2/catalog/storage-plans")) return { payload: { items: [{ id: "sp-owner", availability: "available" }] } };
      if (path === "/api/v2/quotes") {
        ownerCommands.push({ path, idempotencyKey: init.headers["idempotency-key"] });
        return { payload: { id: "quote-owner", purpose: "deploy", runtimeVersionId: "rv-owner", computePlanId: "cp-owner", storagePlanId: "sp-owner", modelSelections: [], periodMonths: 1, totalUSDMicros: "52580000", status: "offered" } };
      }
      if (path === "/api/v2/workspaces" && init.method === "POST") {
        submitted = true;
        ownerCommands.push({ path, idempotencyKey: init.headers["idempotency-key"] });
        throw new Error("response unavailable");
      }
      if (path.startsWith("/api/v2/operations/workspace/") && submitted) throw new Error("owner readback unavailable");
      throw new Error(`unexpected request ${path}`);
    }
  };
  await assert.rejects(() => runLocalWorkspaceJ1HTTPQualification({
    http, adminEmail: "admin@example.test", adminPassword: "password", qualificationEmail: "customer@example.test", qualificationPassword: "password",
    accountProvisionKey: "mapping", launchKey: "launch", workspaceName: "J1",
    wait: async () => {}, readRuntime: async () => ({}), readDebit: async () => ({}),
    cleanup: async (scope) => { cleanupCalls.push(scope); return null; }, receiptBase: {}
  }), /response unavailable/);
  assert.deepEqual(cleanupCalls, [{ accountId, idempotencyKey: "launch", quoteId: "quote-owner", workspaceName: "J1",
    declaration: { applicationSelection: { kind: "opl_app", runtimeVersionId: "rv-owner" }, computePlanId: "cp-owner", storagePlanId: "sp-owner", periodMonths: 1, modelSelections: [] },
    failed: true, launchSubmitted: true, recoveryAuthority: null }]);
  // The order answer was lost, so the harness re-issues the same order under
  // the same idempotency identity instead of buying a second Workspace.
  assert.deepEqual(ownerCommands.filter((command) => command.path === "/api/v2/workspaces").map((command) => command.idempotencyKey), ["launch", "launch", "launch"]);
  assert.deepEqual(ownerCommands.filter((command) => command.path === "/api/v2/quotes").map((command) => command.idempotencyKey), ["launch:quote", "launch:quote", "launch:quote"]);
});

test("live authority configuration fails closed before Docker and writes a redacted receipt", async () => {
  const receiptPath = join(tmpdir(), `local-live-preflight-${process.pid}.json`);
  const previous = {
    baseURL: process.env.OPL_SUB2API_BASE_URL,
    email: process.env.OPL_SUB2API_ADMIN_EMAIL,
    password: process.env.OPL_SUB2API_ADMIN_PASSWORD,
    authorityClass: process.env.OPL_QUALIFICATION_AUTHORITY_CLASS
  };
  delete process.env.OPL_SUB2API_BASE_URL;
  delete process.env.OPL_SUB2API_ADMIN_EMAIL;
  delete process.env.OPL_SUB2API_ADMIN_PASSWORD;
  delete process.env.OPL_QUALIFICATION_AUTHORITY_CLASS;
  try {
    await assert.rejects(() => runLocalWorkspaceQualification({
      sourceSha: sha,
      cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
      workspaceImage: workspaceReference,
      receiptPath,
      buildSourceImages: false,
      authorityMode: "live"
    }), /protected non-production Sub2API credentials/);
    const receipt = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(receipt.status, "NOT_READY");
    assert.equal(receipt.stage, "authority_preflight");
    assert.equal(receipt.errorCode, "live_authority_configuration_missing");
  } finally {
    for (const [name, value] of Object.entries({
      OPL_SUB2API_BASE_URL: previous.baseURL,
      OPL_SUB2API_ADMIN_EMAIL: previous.email,
      OPL_SUB2API_ADMIN_PASSWORD: previous.password,
      OPL_QUALIFICATION_AUTHORITY_CLASS: previous.authorityClass
    })) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
    await rm(receiptPath, { force: true });
  }
});

const qualificationSettlement = (root, receiptPath, failure) => ({
  options: {
    sourceSha: sha,
    cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
    workspaceImage: workspaceReference,
    receiptPath,
    buildSourceImages: false,
    authorityMode: "fixture"
  },
  startedAt: "2026-10-11T00:00:00.000Z",
  sourceTree: "d".repeat(40),
  cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
  workspaceImage: workspaceReference,
  stage: "qualification_cleanup",
  failure,
  residuals: { containers: 0, volumes: 0, networks: 0 },
  recovery: null,
  ownerDeletePending: null,
  finalReceipt: undefined,
  tempRoot: join(root, "scratch"),
  preserveRecoveryAuthority: false
});

test("scratch cleanup failure preserves an earlier qualification stage failure", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-qualification-settle-"));
  const receiptPath = join(root, "receipt.json");
  const earlier = new Error("Ledger deletion Receipt binding is invalid");
  const denial = Object.assign(new Error(`EACCES: permission denied, scandir '${join(root, "scratch", "postgres")}'`), { code: "EACCES" });
  try {
    await assert.rejects(
      () => finishLocalWorkspaceQualification({
        ...qualificationSettlement(root, receiptPath, earlier),
        ownerDeleteFailure: { status: 409, reasonCode: "fabric_compute_readback_unavailable" }
      }, {
        removeScratch: async () => { throw denial; }
      }),
      (error) => error === earlier
    );
    const receipt = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(receipt.status, "NOT_READY");
    assert.equal(receipt.stage, "qualification_cleanup");
    assert.equal(receipt.errorCode, "local_workspace_qualification_failed");
    assert.equal(receipt.error, redactedError(earlier));
    assert.deepEqual(receipt.ownerDeleteFailure, { status: 409, reasonCode: "fabric_compute_readback_unavailable" });
    assert.doesNotMatch(receipt.error, /EACCES|permission denied/);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("unconfirmed scratch cleanup fails an otherwise clean attempt with a receipt", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-qualification-settle-"));
  const receiptPath = join(root, "receipt.json");
  const denial = Object.assign(new Error(`EACCES: permission denied, scandir '${join(root, "scratch", "postgres")}'`), { code: "EACCES" });
  try {
    await assert.rejects(
      () => finishLocalWorkspaceQualification(qualificationSettlement(root, receiptPath, undefined), {
        removeScratch: async () => { throw denial; }
      }),
      (error) => error === denial
    );
    const receipt = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(receipt.status, "NOT_READY");
    assert.equal(receipt.stage, "qualification_cleanup");
    assert.equal(receipt.errorCode, "local_workspace_qualification_failed");
    assert.equal(receipt.error, redactedError(denial));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("confirmed scratch cleanup leaves the READY result untouched", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-qualification-settle-"));
  const receiptPath = join(root, "receipt.json");
  const ready = { schemaVersion: 1, status: "READY", source: { sha, tree: "d".repeat(40) } };
  const removed = [];
  try {
    const settled = await finishLocalWorkspaceQualification(
      { ...qualificationSettlement(root, receiptPath, undefined), finalReceipt: ready },
      { removeScratch: async (target) => { removed.push(target); } }
    );
    assert.equal(settled, ready);
    assert.deepEqual(removed, [join(root, "scratch")]);
    assert.deepEqual(JSON.parse(await readFile(receiptPath, "utf8")), ready);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("scratch teardown retains recovery authority without attempting removal", async () => {
  const root = await mkdtemp(join(tmpdir(), "opl-qualification-settle-"));
  const earlier = new Error("launch recovery readback pending");
  try {
    await assert.rejects(() => finishLocalWorkspaceQualification({
      ...qualificationSettlement(root, join(root, "receipt.json"), earlier), preserveRecoveryAuthority: true
    }, { removeScratch: async () => assert.fail("recovery scratch must be retained") }), error => error === earlier);
    assert.equal(JSON.parse(await readFile(join(root, "receipt.json"), "utf8")).status, "NOT_READY");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});


test("v2 qualification refuses the old three-service topology before customer mutation", () => {
  assert.throws(() => assertWorkspaceQualificationTopology({ services: { "control-plane": {}, fabric: {}, ledger: {} } }),
    { code: "local_v2_qualification_topology_missing" });
  const owners = { workspace: {}, "console-bff": {}, tenant: {}, serve: {} };
  assert.doesNotThrow(() => assertWorkspaceQualificationTopology({ services: owners }));
  assert.throws(() => assertWorkspaceQualificationTopology({ services: { ...owners, tenant: undefined } }), /tenant/);
});

test("local-v2 mode binds only v2 owner endpoints", () => {
  const binding = qualificationModeBinding("local-v2");
  assert.deepEqual(binding.composeFiles, ["deploy/portable/compose.local-v2.yaml"]);
  assert.equal(binding.installationEnvRequired, true);
  const endpoints = Object.values(binding.endpoints);
  assert.ok(endpoints.length >= 15);
  for (const endpoint of endpoints) assert.match(endpoint, /^\/api\/v2\//);
  for (const prefix of ["/api/auth/", "/api/operator/", "/api/gateway/", "/api/v1/"]) {
    assert.equal(endpoints.some((endpoint) => endpoint.startsWith(prefix)), false, `${prefix} must not be a v2 endpoint`);
    assert.equal(qualificationPathAllowed("local-v2", `${prefix}anything`), false);
  }
  assert.equal(qualificationPathAllowed("local-v2", "/api/v2/quotes"), true);
  assert.equal(qualificationPathAllowed("local-v2", "/api/v2/workspaces/ws_1/transactions"), true);
  assert.equal(qualificationPathAllowed("local-v2", "/api/v2/operations/workspace/op_1"), true);
});

test("legacy mode keeps the retained Control Plane endpoints", () => {
  const binding = qualificationModeBinding("legacy-fixture");
  assert.deepEqual(binding.composeFiles, localQualificationComposeFiles("fixture"));
  assert.equal(binding.installationEnvRequired, false);
  assert.equal(binding.endpoints.login, "/api/auth/login");
  assert.equal(binding.endpoints.accounts, "/api/operator/accounts");
  assert.equal(binding.endpoints.wallet, "/api/gateway/wallet");
  assert.equal(qualificationPathAllowed("legacy-fixture", "/api/auth/login"), true);
  assert.throws(() => qualificationModeBinding("production"), { code: "local_qualification_mode_invalid" });
});

test("the local-v2 client refuses a retained endpoint before it opens a socket", async () => {
  const http = createHTTP("https://127.0.0.1:1", { mode: "local-v2" });
  await assert.rejects(() => http.request("/api/auth/login", { method: "POST" }), { code: "local_v2_retained_endpoint_refused" });
  await assert.rejects(() => http.json("/api/operator/accounts"), { code: "local_v2_retained_endpoint_refused" });
  await assert.rejects(() => http.json("/api/gateway/wallet"), { code: "local_v2_retained_endpoint_refused" });
  // A legacy client is unchanged: the guard belongs to the mode, not the transport.
  const legacy = createHTTP("https://127.0.0.1:1");
  await assert.rejects(() => legacy.request("/api/auth/login", { method: "POST" }), (error) => error.code !== "local_v2_retained_endpoint_refused");
});

test("the local-v2 application entry must be served by the declared origin", () => {
  assert.deepEqual(localV2ApplicationEntryBoundary("", "https://localhost:8443"), {
    ok: false, code: "local_v2_application_entry_missing", detail: "the Workspace owner has not published an application entry"
  });
  const unreachable = localV2ApplicationEntryBoundary("https://ws.localhost/w/ws_1/", "https://localhost:8443");
  assert.equal(unreachable.ok, false);
  assert.equal(unreachable.code, "local_v2_application_entry_port_unreachable");
  assert.equal(localV2ApplicationEntryBoundary("https://ws.localhost/w/ws_1/", "https://localhost:443").ok, true);
  assert.equal(localV2ApplicationEntryBoundary("not a url", "https://localhost:443").code, "local_v2_application_entry_invalid");
});

test("local-v2 arguments require the declared installation and refuse a second image spelling", () => {
  const sha = "a".repeat(40);
  assert.throws(() => parseLocalQualificationArgs(["--mode", "local-v2", "--source-sha", sha, "--receipt", "/tmp/r.json"]),
    { code: "local_v2_installation_env_required" });
  assert.throws(() => parseLocalQualificationArgs([
    "--mode", "local-v2", "--source-sha", sha, "--receipt", "/tmp/r.json",
    "--installation-env", "/tmp/installation.env", "--application-contract", "/tmp/contract.json", "--project", "opl-local-v2",
    "--cloud-image", `ghcr.io/example/cloud@sha256:${"b".repeat(64)}`
  ]), { code: "local_v2_images_are_declared" });
  const parsed = parseLocalQualificationArgs([
    "--mode", "local-v2", "--source-sha", sha, "--receipt", "/tmp/r.json",
    "--installation-env", "/tmp/installation.env", "--application-contract", "/tmp/contract.json", "--project", "opl-local-v2"
  ]);
  assert.equal(parsed.mode, "local-v2");
  assert.equal(parsed.installationEnv, "/tmp/installation.env");
  assert.equal(parsed.applicationContract, "/tmp/contract.json");
  assert.equal(parsed.project, "opl-local-v2");
});

function localV2Fixture({ operationResponses = [], replayOrder, workspaceReadback } = {}) {
  const requests = [];
  let workspacePosts = 0;
  let operationReads = 0;
  const workspaceImage = `local/opl-workspace-local-v2@sha256:${"d".repeat(64)}`;
  const values = {
    OPL_LOCAL_V2_PUBLIC_URL: "https://localhost:18443",
    OPL_LOCAL_V2_PKI_DIR: "/tmp/opl-local-v2-pki",
    OPL_LOCAL_V2_PROXY_PORT: "18443",
    OPL_LOCAL_V2_SUBNET: "172.31.0.0/24",
    OPL_LOCAL_V2_POSTGRES_HOST: "172.31.0.10",
    OPL_WORKSPACE_DOMAIN: "ws-qualification.localhost",
    OPL_CLOUD_IMAGE: `opl-cloud-local-v2@sha256:${"e".repeat(64)}`,
    OPL_WORKSPACE_IMAGE: workspaceImage,
    OPL_RESOURCE_CATALOG_PROFILE_JSON: JSON.stringify({ provider: "local-docker", region: "local", billingMode: "LOCAL_NO_CHARGE", providerCapabilityVersion: "local-docker/v1" }),
    OPL_QUALIFICATION_USER_EMAIL: "local-qualification@example.test",
    OPL_QUALIFICATION_USER_PASSWORD: `Local-${"f".repeat(40)}-Aa1!`,
    OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON: JSON.stringify({
      schemaVersion: 1, profileId: "local-docker/local", capabilityVersion: "local-docker/v1", region: "local",
      accountBindings: [{ tenantId: "tenant_test", accountId: "local-host" }],
      packages: [{ id: "local-v2-2vcpu-4gib", name: "Local v2", available: true,
        compute: { id: "local-v2-2vcpu-4gib", server: "configured", cpu: 2, memoryGb: 4, instanceType: "local-v2-2vcpu-4gib" },
        storage: { id: "local-v2-20gib", sizeGb: 20, quotaPolicy: "linux-project" } }]
    })
  };
  const payload = (method, path) => {
    const clean = path.split("?")[0];
    requests.push(`${method} ${clean}`);
    const ok = (body, status = 200) => ({ response: { status, headers: { get: () => null, getSetCookie: () => [] } }, payload: body, text: JSON.stringify(body) });
    if (clean === "/api/v2/auth/context") {
      return { response: { status: 200, headers: { getSetCookie: () => ["opl_login_context=ctx; Path=/", "opl_login_csrf=csrf; Path=/"] } }, payload: { csrfToken: "context-csrf" }, text: "" };
    }
    if (clean === "/api/v2/auth/login") {
      return { response: { status: 200, headers: { getSetCookie: () => ["opl_console_session=session; Path=/"] } }, payload: { actorId: "41", csrfToken: "owner-csrf", displayName: "Local", tenantId: "", expiresAt: "" }, text: "" };
    }
    if (clean === "/api/v2/tenant") return ok({ id: "tenant_test", billingSub2apiUserId: "41", status: "active" });
    if (clean === "/api/v2/admin/catalog/publisher-namespaces") return ok({ items: [{ id: "publisher_test", name: "local-v2-qualification-publisher" }] });
    if (clean === "/api/v2/admin/catalog/runtime-versions") return ok({ items: [{ id: "runtime_test", name: "local-qualification-app" }] });
    if (clean === "/api/v2/admin/catalog/compute-plans") return ok({ items: [{ id: "compute_test", name: "local-v2-compute" }] });
    if (clean === "/api/v2/admin/catalog/storage-plans") return ok({ items: [{ id: "storage_test", name: "local-v2-storage" }] });
    if (clean === "/api/v2/admin/catalog/retention-policies") return ok({ items: [{ id: "retention_test", versionLabel: "local-v2-retention" }] });
    if (clean === "/api/v2/admin/catalog/refund-policies") return ok({ items: [{ id: "refund_test", versionLabel: "local-v2-refund" }] });
    if (clean === "/api/v2/admin/catalog/price-policies") return ok({ items: [{ id: "price_test", versionLabel: "local-v2-price" }] });
    if (clean.startsWith("/api/v2/catalog/runtime-versions")) return ok({ items: [{ id: "runtime_test", name: "local-qualification-app" }] });
    if (clean.startsWith("/api/v2/catalog/compute-plans")) return ok({ items: [{ id: "compute_test", name: "local-v2-compute" }] });
    if (clean.startsWith("/api/v2/catalog/storage-plans")) return ok({ items: [{ id: "storage_test", name: "local-v2-storage" }] });
    if (clean === "/api/v2/quotes") return ok({ id: "quote_test", status: "accepted", purpose: "deploy", runtimeVersionId: "runtime_test", totalUSDMicros: "0" }, 201);
    if (clean === "/api/v2/workspaces") {
      workspacePosts += 1;
      return ok(workspacePosts > 1 && replayOrder
        ? replayOrder
        : { owner: "workspace", kind: "create_workspace", operationId: "op_test", resourceId: "workspace_test" }, 202);
    }
    if (clean === "/api/v2/operations/workspace/op_test") {
      const configured = operationResponses[operationReads++];
      return ok(configured ?? operationResponses.at(-1) ?? { status: "needs_attention", stage: "resource_preflight", errorCode: "local_docker_storage_quota_unavailable", error: { message: "storage root must be ext4 or XFS" } });
    }
    if (clean === "/api/v2/workspaces/workspace_test") {
      return ok(workspaceReadback || { id: "workspace_test", status: "failed", applicationAvailability: "unknown", resourceReadiness: "unknown", accessUrl: "", runtimeVersionId: "runtime_test" });
    }
    if (clean.endsWith("/transactions")) return ok({ items: [] });
    return ok({}, 404);
  };
  const client = {
    origin: values.OPL_LOCAL_V2_PUBLIC_URL,
    request: async (path, init = {}) => payload(String(init.method || "GET").toUpperCase(), path),
    json: async (path, init = {}, _auth = null, statuses = [200]) => {
      const result = payload(String(init.method || "GET").toUpperCase(), path);
      if (!statuses.includes(result.response.status)) throw new Error(`unexpected ${result.response.status} for ${path}`);
      return result;
    }
  };
  const compose = async (args) => {
    if (args[0] === "up") return { code: 0, stdout: "", stderr: "" };
    if (args.includes("config") && args.includes("--environment")) {
      return { code: 0, stdout: Object.entries(values).map(([name, value]) => `${name}=${value}`).join("\n") + "\n", stderr: "" };
    }
    if (args.includes("config")) {
      return { code: 0, stdout: JSON.stringify({ services: Object.fromEntries(LOCAL_V2_QUALIFICATION_SERVICES.map((name) => [name, {}])) }), stderr: "" };
    }
    if (args.some((arg) => String(arg).includes("pg_database"))) {
      return { code: 0, stdout: LOCAL_V2_OWNER_STORES.map(([database, owner]) => `${database}:${owner}`).join("\n"), stderr: "" };
    }
    if (args.some((arg) => String(arg).includes("pg_tables"))) return { code: 0, stdout: "7:7", stderr: "" };
    return { code: 0, stdout: "", stderr: "" };
  };
  return { values, workspaceImage, requests, client, compose };
}

async function runLocalV2FixtureQualification(fixture, dependencies = {}) {
  const root = await mkdtemp(join(tmpdir(), "opl-local-v2-regression-"));
  try {
    const contractPath = join(root, "contract.json");
    await writeFile(contractPath, JSON.stringify({
      schemaVersion: "opl-publisher-contract/v1", publisherNamespaceId: "publisher_test",
      image: { repository: "local/opl-workspace-local-v2", digest: fixture.workspaceImage.split("@")[1], platform: { os: "linux", architecture: "arm64" } },
      kind: "runtime", runtimeAbiVersion: "opl-runtime/v1", packageFormatVersions: ["agent-plugins/1.0.0"],
      applicationAccess: { mode: "anonymous" },
      applicationRevisionTemplate: {
        schemaVersion: 1, applicationId: "local-qualification-app", version: "1.0.0", platform: "linux/arm64",
        image: fixture.workspaceImage, exposurePolicy: "anonymous", ports: [{ name: "webui", port: 3000, protocol: "TCP" }],
        entryPort: "webui", healthChecks: [{ port: 3000, path: "/" }]
      }
    }));
    return await runLocalV2Qualification({
      sourceSha: "a".repeat(40), receiptPath: join(root, "receipt.json"), installationEnv: join(root, "installation.env"),
      applicationContract: contractPath, project: "opl-local-v2", workspaceName: "Local v2 qualification"
    }, {
      readDeclaration: async () => ({ envFile: join(root, "installation.env"), values: fixture.values, facts: { entries: [], unresolved: [] } }),
      createClient: () => fixture.client, compose: fixture.compose, ownerQuery: async () => "3",
      wait: async () => {}, pollIntervalMs: 0, pollAttempts: 100, ...dependencies
    });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

test("local-v2 replay keeps polling through running and stops activation at an empty entry", async () => {
  const parked = { status: "awaiting_confirmation", stage: "resource_preflight", observationResult: "unknown" };
  const fixture = localV2Fixture({
    operationResponses: [
      ...Array(41).fill(parked),
      { status: "running", stage: "runtime", observationResult: "unknown" },
      { status: "awaiting_confirmation", stage: "activation", observationResult: "confirmed" }
    ],
    workspaceReadback: { id: "workspace_test", status: "provisioning", resourceReadiness: "ready", applicationAvailability: "available", accessUrl: "", runtimeVersionId: "runtime_test" }
  });
  const result = await runLocalV2FixtureQualification(fixture);
  // The entry is the earliest failure, and DELETE is authorized only after it is
  // confirmed, so the run stops there and leaves the Workspace intact.
  assert.equal(result.stage, "application_entry");
  assert.equal(result.boundary.code, "local_v2_application_entry_missing");
  assert.equal(result.evidence.readbacks.operation.status, "awaiting_confirmation");
  assert.equal(result.evidence.readbacks.operation.stage, "activation");
  // The activation latch reads the declared park the poll it appears, instead of
  // waiting for the state to stop moving: 41 parked reads, one replay, the running
  // read and the activation read.
  assert.equal(result.evidence.readbacks.operation.polls, 43);
  assert.equal(result.evidence.readbacks.operation.recoveryReplays, 1);
  assert.equal(fixture.requests.filter((request) => request === "POST /api/v2/workspaces").length, 2);
  assert.equal(fixture.requests.some((request) => request.startsWith("DELETE /api/v2/workspaces/")), false);
});

test("local-v2 replay rejects an operation or resource identity mismatch", async () => {
  const fixture = localV2Fixture({
    operationResponses: Array(41).fill({ status: "awaiting_confirmation", stage: "resource_preflight", observationResult: "unknown" }),
    replayOrder: { owner: "workspace", kind: "create_workspace", operationId: "op_other", resourceId: "workspace_other" }
  });
  const result = await runLocalV2FixtureQualification(fixture);
  assert.equal(result.errorCode, "local_v2_replay_identity_mismatch");
  assert.equal(fixture.requests.filter((request) => request === "POST /api/v2/workspaces").length, 2);
  assert.equal(fixture.requests.some((request) => request.startsWith("DELETE /api/v2/workspaces/")), false);
});

test("local-v2 activation requires the Workspace and Serve readbacks before deletion", async () => {
  const fixture = localV2Fixture({
    operationResponses: Array(41).fill({ status: "awaiting_confirmation", stage: "activation", observationResult: "confirmed" }),
    workspaceReadback: { id: "workspace_test", status: "provisioning", resourceReadiness: "unknown", applicationAvailability: "available", accessUrl: "https://workspace.localhost:18443", runtimeVersionId: "runtime_test" }
  });
  const result = await runLocalV2FixtureQualification(fixture);
  assert.equal(result.stage, "application_entry");
  assert.equal(result.boundary.code, "local_v2_application_readiness_unconfirmed");
  assert.equal(fixture.requests.some((request) => request.startsWith("DELETE /api/v2/workspaces/")), false);
});

test("local-v2 polling and replay budgets reject invalid values before dependencies run", async () => {
  const invalid = [NaN, Infinity, -1, 1.5, 1_000_001];
  for (const [name, code] of [
    ["pollIntervalMs", "local_v2_poll_interval_invalid"],
    ["pollAttempts", "local_v2_poll_attempts_invalid"],
    ["recoveryReplays", "local_v2_recovery_replays_invalid"]
  ]) {
    for (const value of invalid) {
      let dependencyCalled = false;
      await assert.rejects(runLocalV2Qualification({}, {
        [name]: value,
        readDeclaration: async () => { dependencyCalled = true; throw new Error("unexpected side effect"); }
      }), { code });
      assert.equal(dependencyCalled, false, `${name}=${value} reached a dependency`);
    }
  }
  await assert.rejects(runLocalV2Qualification({}, {
    pollIntervalMs: 60_000, pollAttempts: 1_000,
    readDeclaration: async () => { throw new Error("poll budget should be rejected first"); }
  }), { code: "local_v2_poll_budget_invalid" });
  assert.doesNotThrow(() => localV2Fixture());
});

test("local-v2 qualification collects owner readbacks and reports the host boundary instead of READY", async () => {
  const fixture = localV2Fixture();
  const root = await mkdtemp(join(tmpdir(), "opl-local-v2-qualification-"));
  try {
    const contractPath = join(root, "contract.json");
    await writeFile(contractPath, JSON.stringify({
      schemaVersion: "opl-publisher-contract/v1",
      publisherNamespaceId: "publisher_test",
      image: {
        repository: "local/opl-workspace-local-v2",
        digest: fixture.workspaceImage.split("@")[1],
        platform: { os: "linux", architecture: "arm64" }
      },
      kind: "runtime",
      runtimeAbiVersion: "opl-runtime/v1",
      packageFormatVersions: ["agent-plugins/1.0.0"],
      applicationAccess: { mode: "anonymous" },
      applicationRevisionTemplate: {
        schemaVersion: 1, applicationId: "local-qualification-app", version: "1.0.0",
        platform: "linux/arm64", image: fixture.workspaceImage, exposurePolicy: "anonymous",
        ports: [{ name: "webui", port: 3000, protocol: "TCP" }], entryPort: "webui",
        healthChecks: [{ port: 3000, path: "/" }]
      }
    }));
    const receiptPath = join(root, "receipt.json");
    const result = await runLocalV2Qualification({
      sourceSha: "a".repeat(40), receiptPath, installationEnv: join(root, "installation.env"),
      applicationContract: contractPath, project: "opl-local-v2", workspaceName: "Local v2 qualification"
    }, {
      readDeclaration: async () => ({ envFile: join(root, "installation.env"), values: fixture.values, facts: { entries: [], unresolved: [] } }),
      createClient: () => fixture.client,
      compose: fixture.compose,
      ownerQuery: async () => "3",
      wait: async () => {},
      pollIntervalMs: 0,
      pollAttempts: 2
    });
    assert.equal(result.status, "NOT_READY");
    assert.equal(result.mode, "local-v2");
    assert.equal(result.boundary.code, "local_docker_storage_quota_unavailable", JSON.stringify(result.boundary));
    assert.equal(result.evidence.readbacks.tenant.id, "tenant_test");
    assert.equal(result.evidence.readbacks.cloudIdentity.actorId, "41");
    assert.equal(result.evidence.readbacks.catalog.runtimeVersionId, "runtime_test");
    assert.equal(result.evidence.readbacks.catalog.computePlanId, "compute_test");
    assert.equal(result.evidence.readbacks.catalog.storagePlanId, "storage_test");
    assert.equal(result.evidence.readbacks.quote.totalUSDMicros, "0");
    assert.equal(result.evidence.readbacks.order.workspaceId, "workspace_test");
    assert.equal(result.evidence.readbacks.workspace.status, "failed");
    assert.equal(result.evidence.readbacks.ledger.receipts, 3);
    assert.equal(result.evidence.readbacks.fabric.resourceSets, 3);
    assert.equal(result.evidence.readbacks.stores.ownerSeparated, true);
    assert.equal(result.evidence.readbacks.stores.ownerStores.opl_workspace.schema, "workspace");
    assert.deepEqual(result.retainedWritersUsed, []);
    assert.equal(result.receiptCompatibility.compatible, false);
    assert.ok(result.receiptCompatibility.missing.length > 0);
    // A boundary is evidence, never a READY receipt.
    assert.throws(() => validateLocalQualificationReceipt(result), /READY/);
    const written = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(written.status, "NOT_READY");
    assert.equal(written.boundary.code, "local_docker_storage_quota_unavailable");
    // Not one retained Control Plane route was requested.
    for (const request of fixture.requests) {
      const path = request.split(" ")[1];
      assert.equal(RETAINED_LEGACY_ENDPOINT_PREFIXES.some((prefix) => path.startsWith(prefix)), false, `${request} is a retained route`);
    }
    assert.ok(fixture.requests.includes("POST /api/v2/quotes"));
    assert.ok(fixture.requests.includes("POST /api/v2/workspaces"));
    assert.ok(fixture.requests.includes("GET /api/v2/operations/workspace/op_test"));
    assert.ok(fixture.requests.includes("GET /api/v2/workspaces/workspace_test"));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the local-v2 origin and CA come only from the declared installation", () => {
  assert.equal(localV2QualificationOrigin({ OPL_LOCAL_V2_PUBLIC_URL: "https://localhost:18443" }), "https://localhost:18443");
  assert.throws(() => localV2QualificationOrigin({ OPL_LOCAL_V2_PUBLIC_URL: "http://localhost:18443" }), { code: "local_v2_public_url_invalid" });
  assert.throws(() => localV2QualificationOrigin({ OPL_LOCAL_V2_PUBLIC_URL: "https://localhost:18443/api" }), { code: "local_v2_public_url_invalid" });
  assert.throws(() => localV2QualificationOrigin({}), { code: "local_v2_public_url_invalid" });
  assert.equal(localV2QualificationCA({ OPL_LOCAL_V2_PKI_DIR: "/tmp/pki" }), "/tmp/pki/ca.crt");
  assert.throws(() => localV2QualificationCA({ OPL_LOCAL_V2_PKI_DIR: "relative/pki" }), { code: "local_v2_pki_dir_invalid" });
  assert.throws(() => localV2QualificationCA({}), { code: "local_v2_pki_dir_invalid" });
});
