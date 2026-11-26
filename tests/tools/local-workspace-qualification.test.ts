import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, lstat, mkdir, mkdtemp, readFile, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  assertLocalV2ComposeEnvironment,
  assertWorkspaceQualificationTopology,
  buildSourceImages,
  deleteWorkspaceOnOwnerRoute,
  readWorkspaceDeletionEvidence,
  createHTTP,
  exactRepoDigestFromInspection,
  immutableImageDigest,
  login,
  loadJ0ReadyReceipt,
  localBuildProxyArgs,
  qualificationComposeEnvironment,
  parseInstallationEnvFile,
  parseLocalQualificationArgs,
  qualificationModeBinding,
  qualificationPathAllowed,
  RETAINED_LEGACY_ENDPOINT_PREFIXES,
  runLocalV2Qualification,
  verifyStores,
  localV2ApplicationEntryBoundary,
  localV2QualificationCA,
  localV2QualificationOrigin,
  readLocalV2InstallationDeclaration,
  LOCAL_V2_QUALIFICATION_SERVICES,
  LOCAL_V2_OWNER_STORES,
  redactedError,
  sourceData,
  validateQualificationSourceIdentity,
  validateJ0ReadyReceipt,
  validateLocalQualificationReceipt,
} from "../../tools/local-workspace-qualification.ts";
import { cleanHostInstallation } from "../../tools/local-v2-clean-host.ts";
import { localV2EnvFile } from "../../tools/local-v2-installation-facts.ts";

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

test("qualification reads the case-sensitive proxy settings from its generated installation", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "opl-qualification-installation-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const templatePath = join(process.cwd(), "deploy/portable/opl-cloud.env.example");
  const composePath = join(process.cwd(), "deploy/portable/compose.local-v2.yaml");
  const [template, compose] = await Promise.all([readFile(templatePath, "utf8"), readFile(composePath, "utf8")]);
  const facts = cleanHostInstallation({
    template, compose, directory, sourceSha: sha,
    cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
    workspaceImage: workspaceReference,
    storageRoot: join(directory, "storage"),
    project: "opl-native-1-1",
    publisherSchemaDigest: `sha256:${"d".repeat(64)}`
  });
  const envFile = join(directory, "installation.env");
  const declaration = localV2EnvFile(facts).replace(/^http_proxy=$/m, "http_proxy=http://proxy.example.test:8080");
  await writeFile(envFile, declaration);
  const { values } = await readLocalV2InstallationDeclaration({ envFile, templatePath, composePath });
  assert.equal(values.HTTP_PROXY, "");
  assert.equal(values.http_proxy, "http://proxy.example.test:8080");
  assert.equal(values.https_proxy, "");
  assert.equal(values.no_proxy, "");
  assert.throws(() => parseInstallationEnvFile(`${declaration}\nhttp_proxy=http://other.example.test`), {
    code: "local_v2_installation_env_duplicate"
  });
});

test("qualification compose environment carries every name the shipped installation declares", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "opl-qualification-compose-environment-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const templatePath = join(process.cwd(), "deploy/portable/opl-cloud.env.example");
  const composePath = join(process.cwd(), "deploy/portable/compose.local-v2.yaml");
  const [template, compose] = await Promise.all([readFile(templatePath, "utf8"), readFile(composePath, "utf8")]);
  const facts = cleanHostInstallation({
    template, compose, directory, sourceSha: sha,
    cloudImage: `ghcr.io/example/cloud@${cloudDigest}`,
    workspaceImage: workspaceReference,
    storageRoot: join(directory, "storage"),
    project: "opl-native-1-1",
    publisherSchemaDigest: `sha256:${"d".repeat(64)}`
  });
  const envFile = join(directory, "installation.env");
  await writeFile(envFile, localV2EnvFile(facts));
  // The installation exactly as the qualification reads it.
  const { values } = await readLocalV2InstallationDeclaration({ envFile, templatePath, composePath });
  // The owner topology declares the lowercase proxy settings among the
  // installation's shared facts, so the template declares them too.
  for (const name of ["http_proxy", "https_proxy", "no_proxy"]) {
    assert.equal(Object.prototype.hasOwnProperty.call(values, name), true, `${name} must be a declared installation fact`);
  }
  // The Compose environment the qualification starts the topology with carries
  // every declared name, in the spelling the declaration reader accepted.
  const environment = qualificationComposeEnvironment({ PATH: "/usr/bin" }, Object.entries(values));
  assert.equal(environment.http_proxy, "");
  assert.equal(environment.https_proxy, "");
  assert.equal(environment.no_proxy, "");
  assert.equal(environment.OPL_CLOUD_IMAGE, `ghcr.io/example/cloud@${cloudDigest}`);
  assert.equal(environment.PATH, "/usr/bin");
});

test("qualification compose environment refuses an unusable name and a broken value", () => {
  assert.throws(() => qualificationComposeEnvironment({}, [["OPL CLOUD IMAGE", "declared"]]), {
    message: /qualification compose environment entry is invalid/
  });
  assert.throws(() => qualificationComposeEnvironment({}, [["OPL_CLOUD_IMAGE", "first\nsecond"]]), {
    message: /qualification compose environment entry is invalid/
  });
});

test("source image builds pass the exact qualification source revision to Docker", async () => {
  const directory = await mkdtemp(join(tmpdir(), "opl-source-image-build-test-"));
  const binDirectory = join(directory, "bin");
  const logPath = join(directory, "docker-args.log");
  await mkdir(binDirectory);
  await writeFile(join(binDirectory, "docker"), `#!/bin/sh
printf '%s\\n' "$*" >> "$OPL_TEST_DOCKER_LOG"
case "$1:$2" in
  image:inspect)
    ref="$3"
    case "$ref" in
      *cloud*) digest="d" ;;
      *) digest="e" ;;
    esac
    case "$ref" in
      *@*) repository="\${ref%@*}" ;;
      *) repository="\${ref%:source}" ;;
    esac
    printf '[{"Id":"sha256:%s%s","RepoDigests":["%s@sha256:%s%s"]}]\\n' "$digest" "$(printf '0%.0s' $(seq 1 63))" "$repository" "$digest" "$(printf '0%.0s' $(seq 1 63))"
    ;;
esac
exit 0
`);
  await chmod(join(binDirectory, "docker"), 0o700);
  const previousPath = process.env.PATH;
  const previousLog = process.env.OPL_TEST_DOCKER_LOG;
  process.env.PATH = `${binDirectory}:${previousPath || ""}`;
  process.env.OPL_TEST_DOCKER_LOG = logPath;
  try {
    await buildSourceImages(sha, "opl-source-test", 32768);
    const commands = (await readFile(logPath, "utf8")).trim().split("\n");
    const cloudBuild = commands.find((command) => command.includes("build") && command.includes("opl-source-test-cloud:source"));
    assert.ok(cloudBuild);
    assert.match(cloudBuild, new RegExp(`--build-arg OPL_SOURCE_SHA=${sha}`));
  } finally {
    if (previousPath === undefined) delete process.env.PATH;
    else process.env.PATH = previousPath;
    if (previousLog === undefined) delete process.env.OPL_TEST_DOCKER_LOG;
    else process.env.OPL_TEST_DOCKER_LOG = previousLog;
    await rm(directory, { recursive: true, force: true });
  }
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

test("the declared native topology is the default local qualification", () => {
  // The default is local-v2: a run names the declared native installation.
  assert.deepEqual(parseLocalQualificationArgs([
    "--source-sha", sha,
    "--installation-env", "/tmp/installation.env",
    "--application-contract", "/tmp/application.json",
    "--project", "opl-native-1",
    "--receipt", "/tmp/qualification.json"
  ]), {
    mode: "local-v2",
    sourceSha: sha,
    receiptPath: "/tmp/qualification.json",
    installationEnv: "/tmp/installation.env",
    applicationContract: "/tmp/application.json",
    project: "opl-native-1",
    workspaceName: ""
  });
  assert.equal(immutableImageDigest(`ghcr.io/example/cloud@${cloudDigest}`), cloudDigest);
  assert.equal(immutableImageDigest(workspaceDigest), workspaceDigest);
  assert.throws(() => parseLocalQualificationArgs(["--source-sha", "main"]), /source SHA/);
  // The declared installation owns the images, so a second spelling of one is
  // refused instead of silently overriding the declaration.
  assert.throws(() => parseLocalQualificationArgs([
    "--source-sha", sha, "--installation-env", "/tmp/installation.env",
    "--application-contract", "/tmp/application.json", "--project", "opl-native-1",
    "--cloud-image", `ghcr.io/example/cloud@${cloudDigest}`,
    "--receipt", "/tmp/qualification.json"
  ]), { code: "local_v2_images_are_declared" });
  // A run that does not name the declared installation is refused before it
  // starts: there is no other qualification to fall back to.
  assert.throws(() => parseLocalQualificationArgs([
    "--source-sha", sha, "--receipt", "/tmp/qualification.json"
  ]), { code: "local_v2_installation_env_required" });
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

test("the retired version-one qualification is refused by name", () => {
  const installation = [
    "--source-sha", sha,
    "--installation-env", "/tmp/installation.env",
    "--application-contract", "/tmp/application.json",
    "--project", "opl-native-1",
    "--receipt", "/tmp/qualification.json"
  ];
  // A retired mode is refused by name, so the caller is told which run it
  // asked for and that the run is gone.
  for (const mode of ["legacy-fixture", "legacy-live"]) {
    assert.throws(() => parseLocalQualificationArgs(["--mode", mode, ...installation]), { code: "local_qualification_mode_retired" });
  }
  // An unknown mode is a different fact: it is refused as unknown.
  assert.throws(() => parseLocalQualificationArgs(["--mode", "not-a-mode", ...installation]), { code: "local_qualification_mode_invalid" });
  // The options that belonged to the retired runs are refused by name too.
  for (const option of ["--authority-mode", "--j0-ready-receipt", "--sub2api-secret-file"]) {
    assert.throws(() => parseLocalQualificationArgs([...installation, option, "/tmp/retired"]), { code: "local_qualification_option_retired" });
  }
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
    const readback = await http.json("/api/operator/accounts?page=1&pageSize=50", {}, admin);
    const accountPage = sourceData(readback.payload, "control-plane+sub2api");
    assert.equal(accountPage.items[0].accountId, "acct-mapped");
    assert.throws(() => sourceData({ source: "control-plane", available: true, status: "available", data: accountPage }, "control-plane+sub2api"), /unavailable/);
    assert.throws(() => sourceData({ source: "control-plane+sub2api", available: false, status: "unavailable", data: accountPage }, "control-plane+sub2api"), /unavailable/);
    const qualification = await login(http, "customer@example.test", "customer-password");
    assert.deepEqual(qualification, { cookie: "opl_session=admin-session", csrf: "csrf-admin" });
    assert.equal(workspacePosts, 0);
    assert.deepEqual(requests.map(({ method, path }) => `${method}:${path}`), [
      "POST:/api/auth/login", "GET:/api/operator/accounts", "POST:/api/auth/login"
    ]);
  } finally {
    await new Promise((resolvePromise) => server.close(() => resolvePromise()));
  }
});




// Exercise the owner readbacks, including the existing purchase evidence binding.
function workspaceEvidenceHTTP(page, options = {}) {
  const workspaceId = "ws-evidence";
  const operationId = "operation-evidence";
  const receiptId = "receipt-evidence";
  const auth = { cookie: "evidence-session", csrf: "evidence-csrf" };
  const calls = [];
  const charge = {
    id: "wallet-evidence", workspaceId, kind: "charge", status: "confirmed",
    receiptId, amountUSDMicros: "52580000", externalReference: "charge-evidence",
  };
  const responses = new Map([
    [`/api/v2/operations/workspace/${operationId}`, {
      operationId, owner: "workspace", kind: "create_workspace", status: "succeeded", resourceId: workspaceId,
    }],
    [`/api/v2/workspaces/${workspaceId}`, {
      id: workspaceId, applicationAvailability: "available", accessUrl: "https://previous.example.test/",
      capabilityVersionId: "capability-evidence", ...options.workspace,
    }],
    [`/api/v2/workspaces/${workspaceId}/deployments?limit=1`, page],
    [`/api/v2/workspaces/${workspaceId}/access`, Object.hasOwn(options, "access") ? options.access : {
      workspaceId, deploymentId: "deployment-current", url: "https://app.example.test/",
    }],
    [`/api/v2/workspaces/${workspaceId}/transactions?limit=100`, { items: [charge] }],
    [`/api/v2/workspaces/${workspaceId}/financial-receipts/${receiptId}`, {
      workspaceId,
      receipt: { id: receiptId, operationId, owner: "workspace", kind: "wallet_action", outcome: "confirmed" },
      walletOperation: charge,
      quote: { id: "quote-evidence", status: "accepted", totalUSDMicros: charge.amountUSDMicros },
      evidenceDigest: `sha256:${"e".repeat(64)}`,
    }],
  ]);
  return {
    calls, auth,
    async json(path, init = {}, suppliedAuth) {
      calls.push(path);
      assert.equal(suppliedAuth, auth, "owner reads retain the caller's authentication");
      if (path.endsWith("/access")) assert.deepEqual(init, {
        method: "POST", headers: { "idempotency-key": `workspace-access:${page.currentDeployment.id}` },
      });
      assert.ok(responses.has(path), `unexpected owner read ${path}`);
      return { payload: responses.get(path) };
    },
  };
}












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
  assert.equal(localV2ApplicationEntryBoundary("https://localhost/w/ws_1/", "https://localhost:443").ok, true);
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

function localV2Fixture({ operationResponses = [], replayOrder, workspaceReadback, quoteOverride = {}, routeReply } = {}) {
  const requests = [];
  let workspacePosts = 0;
  let operationReads = 0;
  const workspaceImage = `local/opl-workspace-local-v2@sha256:${"d".repeat(64)}`;
  const values = {
    OPL_PUBLIC_URL: "https://localhost:18443",
    OPL_PKI_DIR: "/tmp/opl-local-v2-pki",
    OPL_PROXY_PORT: "18443",
    OPL_DOCKER_SUBNET: "172.31.0.0/24",
    OPL_POSTGRES_HOST: "172.31.0.10",
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
    const overridden = routeReply?.(method, clean, ok);
    if (overridden) return overridden;
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
    if (clean === "/api/v2/quotes") return ok({ id: "quote_test", status: "accepted", purpose: "deploy", runtimeVersionId: "runtime_test", computePlanId: "compute_test", storagePlanId: "storage_test", periodMonths: 1, totalUSDMicros: "0", ...quoteOverride }, 201);
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
    origin: values.OPL_PUBLIC_URL,
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

async function runLocalV2FixtureQualification(fixture, dependencies = {}, optionOverrides = {}, verifyReceipts = null) {
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
    const receiptPath = join(root, "receipt.json");
    const result = await runLocalV2Qualification({
      sourceSha: "a".repeat(40), receiptPath, installationEnv: join(root, "installation.env"),
      applicationContract: contractPath, project: "opl-local-v2", workspaceName: "Local v2 qualification",
      ...optionOverrides
    }, {
      readSourceIdentity: async () => ({ sha: "a".repeat(40), tree: "b".repeat(40), clean: true }),
      inspectImage: async (reference) => ({ Id: `sha256:${"f".repeat(64)}`, RepoDigests: [reference],
        Config: { Labels: { "org.opencontainers.image.revision": "a".repeat(40) } } }),
      inspectOwner: async () => ({ Image: `sha256:${"f".repeat(64)}` }),
      readDeclaration: async () => ({ envFile: join(root, "installation.env"), values: fixture.values, facts: { entries: [], unresolved: [] } }),
      createClient: () => fixture.client, compose: fixture.compose, ownerQuery: async () => "3",
      wait: async () => {}, pollIntervalMs: 0, pollAttempts: 100, ...dependencies
    });
    await verifyReceipts?.(receiptPath);
    return result;
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

test("uploaded Workspace runtime failure preserves only its owner diagnostics before cleanup", async () => {
  const order = { owner: "workspace", kind: "create_workspace", operationId: "op_uploaded", resourceId: "workspace_uploaded" };
  const response = { ...order, status: "awaiting_confirmation", stage: "runtime", observationResult: "unknown", errorCode: "DEPENDENCY_UNAVAILABLE" };
  const deletion = { owner: "workspace", kind: "delete_workspace", operationId: "op_delete_uploaded", resourceId: order.resourceId, status: "succeeded", stage: "succeeded" };
  const events = [];
  const fixture = localV2Fixture({
    quoteOverride: { capabilityVersionId: "capv_uploaded", provider: "local-docker", billingMode: "LOCAL_NO_CHARGE" },
    routeReply(method, path, ok) {
      const replies = {
        "/api/v2/namespaces": { items: [{ id: "ns_uploaded", isDefault: true }] },
        "/api/v2/packages": { id: "pkg_uploaded" },
        "/api/v2/packages/pkg_uploaded/uploads": { id: "upload_fixture", packageVersionId: "pv_uploaded", partSizeBytes: "16777216" },
        "/api/v2/uploads/upload_fixture/parts": { url: "https://objects.test/fixture" },
        "/api/v2/uploads/upload_fixture/complete": { owner: "capability", kind: "complete_upload", resourceId: "pv_uploaded", status: "succeeded", stage: "succeeded", observationResult: "confirmed", operationId: "op_upload" },
        "/api/v2/catalog/build-policy": { runtimeVersionId: "runtime_test", defaultWebuiVersionId: "webui_fixture" },
        "/api/v2/catalog/runtime-versions": { items: [{ id: "runtime_test", name: "local-qualification-app", publisherContract: { buildRecipe: {
          packageInput: { sourceRoot: "agent", targetPath: "/agent" }, outputImageCommand: { entrypoint: ["/agent/bin/runtime"] }
        } } }] },
        "/api/v2/builds": { id: "build_uploaded", status: "queued", packageVersionId: "pv_uploaded", runtimeVersionId: "runtime_test", webuiVersionId: "webui_fixture" },
        "/api/v2/builds/build_uploaded": { id: "build_uploaded", status: "succeeded", stage: "succeeded", packageVersionId: "pv_uploaded", runtimeVersionId: "runtime_test", webuiVersionId: "webui_fixture", resultCapabilityVersionId: "capv_uploaded" },
        "/api/v2/capability-versions/capv_uploaded": { id: "capv_uploaded", status: "ready", artifactDigest: `sha256:${"a".repeat(64)}` },
        "/api/v2/operations/workspace/op_uploaded": response,
        "/api/v2/operations/workspace/op_delete_uploaded": deletion
      };
      if (path === "/api/v2/workspaces" && method === "POST" && events.length === 0) {
        events.push("purchase");
        return ok(order, 202);
      }
      if (path === "/api/v2/workspaces/workspace_uploaded" && method === "DELETE") {
        events.push("delete");
        return ok(deletion, 202);
      }
      if (path === "/api/v2/workspaces/workspace_uploaded") return ok({ id: order.resourceId, status: "deleted", accessUrl: "" });
      if (replies[path]) return ok(replies[path]);
    }
  });
  const originalJson = fixture.client.json;
  let upload;
  fixture.client.json = async (path, init, ...args) => {
    if (path === "/api/v2/packages/pkg_uploaded/uploads") upload = init.body;
    if (path === "/api/v2/package-versions/pv_uploaded") {
      return { payload: { id: "pv_uploaded", packageId: "pkg_uploaded", versionLabel: "1.0.0",
        sha256: upload.sha256, sizeBytes: upload.sizeBytes, status: "uploaded" } };
    }
    return originalJson(path, init, ...args);
  };
  const runtimeFailure = {
    workspaceSteps: [{ step: "read_runtime", observation: "unknown", errorCode: "DEPENDENCY_UNAVAILABLE" }],
    serveDeployments: [{
      deploymentStatus: "deploying", deploymentErrorCode: null, runtimeStatus: "starting",
      runtimeErrorCode: null, startObservation: "unknown", startErrorCode: "local_docker_runtime_unconfirmed"
    }]
  };
  const result = await runLocalV2FixtureQualification(fixture, {
    pollAttempts: 2,
    putCustomerPart: async () => ({ status: 200, headers: new Headers() }),
    ownerQuery: async (database, sql) => {
      if (database === "opl_workspace") {
        assert.match(sql, /o\.id = 'op_uploaded' and o\.resource_id = 'workspace_uploaded' and o\.tenant_id = 'tenant_test'/);
        assert.match(sql, /o\.kind = 'create_workspace'/);
        events.push("workspace evidence");
        return JSON.stringify([{ ...runtimeFailure.workspaceSteps[0], inputSnapshot: "private request" }]);
      }
      if (database === "opl_serve") {
        assert.match(sql, /d\.workspace_id = 'workspace_uploaded' and d\.capability_version_id = 'capv_uploaded'/);
        assert.match(sql, /o\.tenant_id = 'tenant_test' and o\.kind = 'runtime_deploy'/);
        assert.match(sql, /a\.expected_deployment_id = d\.id and a\.action = 'start'/);
        events.push("serve evidence");
        return JSON.stringify([{ ...runtimeFailure.serveDeployments[0], runtimeErrorCode: "https://private.test/credential", raw: "private result" }]);
      }
      return "3";
    }
  }, {
    customer: { declared: true, mode: "directory", packageDir: join(process.cwd(), "tests/tools/fixtures/customer-agent-package"),
      probePath: "agent/provenance/probe.txt", requestedEvidenceLevel: "content-provenance", achievableEvidenceLevel: "content-provenance", degradationReason: "" }
  }, async (receiptPath) => {
    const primary = JSON.parse(await readFile(receiptPath, "utf8"));
    assert.equal(primary.boundary.code, "local_v2_customer_workspace_operation_failed", JSON.stringify(primary.boundary));
    const nativeEvidence = JSON.parse(await readFile(`${receiptPath}.diagnostic.json`, "utf8"));
    assert.equal(nativeEvidence.status, "NOT_READY");
    assert.deepEqual(nativeEvidence.evidence.readbacks.customerPackage.runtimeFailure, runtimeFailure);
    assert.deepEqual(nativeEvidence, primary);
    assert.throws(() => validateLocalQualificationReceipt(primary), /READY/);
  });
  assert.deepEqual(events, ["purchase", "workspace evidence", "serve evidence", "delete"]);
  assert.equal(result.boundary.code, "local_v2_customer_workspace_operation_failed");
});

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
      readSourceIdentity: async () => ({ sha: "a".repeat(40), tree: "b".repeat(40), clean: true }),
      inspectImage: async (reference) => ({ Id: `sha256:${"f".repeat(64)}`, RepoDigests: [reference],
        Config: { Labels: { "org.opencontainers.image.revision": "a".repeat(40) } } }),
      inspectOwner: async () => ({ Image: `sha256:${"f".repeat(64)}` }),
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
    assert.deepEqual(result.receiptCompatibility, { nativeSchemaVersion: 2, legacyReceiptCompatible: false });
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
  assert.equal(localV2QualificationOrigin({ OPL_PUBLIC_URL: "https://localhost:18443" }), "https://localhost:18443");
  assert.throws(() => localV2QualificationOrigin({ OPL_PUBLIC_URL: "http://localhost:18443" }), { code: "local_v2_public_url_invalid" });
  assert.throws(() => localV2QualificationOrigin({ OPL_PUBLIC_URL: "https://localhost:18443/api" }), { code: "local_v2_public_url_invalid" });
  assert.throws(() => localV2QualificationOrigin({}), { code: "local_v2_public_url_invalid" });
  assert.equal(localV2QualificationCA({ OPL_PKI_DIR: "/tmp/pki" }), "/tmp/pki/ca.crt");
  assert.throws(() => localV2QualificationCA({ OPL_PKI_DIR: "relative/pki" }), { code: "local_v2_pki_dir_invalid" });
  assert.throws(() => localV2QualificationCA({}), { code: "local_v2_pki_dir_invalid" });
});


test("local-v2 entry rejects same-port foreign hosts, protocols and credentials", () => {
  for (const entry of ["https://foreign.invalid:18443/w/one", "http://localhost:18443/w/one", "https://user:pass@localhost:18443/w/one"]) {
    assert.equal(localV2ApplicationEntryBoundary(entry, "https://localhost:18443").ok, false, entry);
  }
});

test("local-v2 quote is bound before an order is submitted", async () => {
  for (const quoteOverride of [{ runtimeVersionId: "other" }, { computePlanId: "other" }, { storagePlanId: "other" }, { periodMonths: 2 }, { purpose: "renew" }]) {
    const fixture = localV2Fixture({ quoteOverride });
    const result = await runLocalV2FixtureQualification(fixture);
    assert.equal(result.errorCode, "local_v2_quote_invalid");
    assert.equal(fixture.requests.includes("POST /api/v2/workspaces"), false);
  }
});

test("local-v2 operation deadline includes reads and never replays after expiry", async () => {
  let now = 0;
  const fixture = localV2Fixture({
    operationResponses: Array(100).fill({ status: "awaiting_confirmation", stage: "resource_preflight", observationResult: "unknown" })
  });
  const json = fixture.client.json;
  fixture.client.json = async (...args) => {
    if (args[0] === "/api/v2/operations/workspace/op_test") now += 2500;
    return json(...args);
  };
  const result = await runLocalV2FixtureQualification(fixture, { now: () => now, pollIntervalMs: 1, pollAttempts: 100 });
  assert.equal(result.boundary.code, "local_v2_operation_poll_budget_exhausted");
  assert.equal(result.evidence.readbacks.operation.polls, 1);
  assert.equal(fixture.requests.filter((x) => x === "POST /api/v2/workspaces").length, 1);
});

test("local-v2 no-charge deletion confirms only after a reachable entry and owner absence", async () => {
  let deleted = false;
  let probes = 0;
  const operation = { owner: "workspace", kind: "create_workspace", operationId: "op_test", resourceId: "workspace_test", status: "awaiting_confirmation", stage: "activation", observationResult: "confirmed" };
  const deletion = { owner: "workspace", kind: "delete_workspace", operationId: "op_delete", resourceId: "workspace_test", status: "succeeded", stage: "done" };
  const ready = { id: "workspace_test", status: "provisioning", resourceReadiness: "ready", applicationAvailability: "available", accessUrl: "https://localhost:18443/w/one", runtimeVersionId: "runtime_test" };
  const fixture = localV2Fixture({ operationResponses: [operation], workspaceReadback: ready,
    routeReply(method, path, ok) {
      if (method === "DELETE") { assert.equal(probes, 1); deleted = true; return ok(deletion, 202); }
      if (path === "/api/v2/operations/workspace/op_delete") return ok(deletion);
      if (path === "/api/v2/workspaces/workspace_test" && deleted) return ok({ ...ready, status: "deleted", accessUrl: "" });
      if (path === "/api/v2/workspaces/workspace_test/deletion") return ok({
        workspaceId: "workspace_test", operationId: "op_delete", resourceDeletionStatus: "confirmed", dataDeletionStatus: "confirmed", refundStatus: "not_applicable",
        deletionReceipt: { id: "receipt_delete", kind: "provider_action", owner: "workspace", operationId: "op_delete", outcome: "confirmed", createdAt: "2026-10-25T00:00:00Z" }
      });
    }
  });
  const result = await runLocalV2FixtureQualification(fixture, {
    fetchEntry: async () => { probes += 1; return { response: { status: 200 }, text: "ready" }; },
    ownerQuery: async (database, sql) => database === "opl_fabric" && sql.includes("deleted_at") || database === "opl_serve" ? "0" : "4"
  });
  assert.equal(result.evidence.stages.find((x) => x.stage === "workspace_delete")?.status, "confirmed");
  assert.equal(result.evidence.readbacks.deletion.refundStatus, "not_applicable");
  assert.equal(result.status, "NOT_READY");
});

test("local-v2 unreachable same-origin entry never authorizes deletion", async () => {
  const fixture = localV2Fixture({
    operationResponses: [{ owner: "workspace", kind: "create_workspace", operationId: "op_test", resourceId: "workspace_test", status: "awaiting_confirmation", stage: "activation", observationResult: "confirmed" }],
    workspaceReadback: { id: "workspace_test", status: "provisioning", resourceReadiness: "ready", applicationAvailability: "available", accessUrl: "https://localhost:18443/w/one", runtimeVersionId: "runtime_test" }
  });
  const result = await runLocalV2FixtureQualification(fixture, { fetchEntry: async () => ({ response: { status: 503 }, text: "unavailable" }) });
  assert.equal(result.boundary.code, "local_v2_application_entry_unreachable");
  assert.equal(fixture.requests.some((x) => x.startsWith("DELETE ")), false);
});


test("owner HTTP transport sends the DELETE body and preserves caller cancellation", async (t) => {
  let requests = 0;
  const server = createServer(async (req, res) => {
    requests += 1;
    let body = "";
    for await (const chunk of req) body += chunk;
    assert.equal(req.headers["content-length"], String(Buffer.byteLength(body)));
    assert.deepEqual(JSON.parse(body), { confirmationName: "One", acknowledgeDataDestruction: true });
    res.writeHead(202, { "content-type": "application/json" });
    res.end(JSON.stringify({ operationId: "op_delete" }));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  const http = createHTTP(`http://127.0.0.1:${server.address().port}`);
  const init = { method: "DELETE", body: { confirmationName: "One", acknowledgeDataDestruction: true } };
  const result = await http.json("/api/v2/workspaces/one", init, null, [202]);
  assert.equal(result.payload.operationId, "op_delete");
  await assert.rejects(http.request("/api/v2/workspaces/one", { ...init, signal: AbortSignal.abort() }));
  assert.equal(requests, 1);
});
