import * as operatorApi from "../../apps/console-ui/src/api/console-read-api.ts";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { afterEach, test } from "node:test";

import {
  getOperatorAccounts,
  getOperatorAccountsPage,
  getOperatorAnnouncements,
  getOperatorOverview,
  getOperatorWorkspace,
  getOperatorWorkspaceRuntimeImagePolicy,
  getOperatorWorkspaceRuntimeImageReplacementPreview,
  getOperatorWorkspaces,
  getOperatorReconciliation,
  getOperatorHealth,
  getOperatorRuntimeObservations,
  getWalletAdjustment,
  getWorkspaceLaunchRecovery,
  getOperatorWorkspaceRuntimeImageReplacement,
  getOperatorWorkspaceApplicationDeployment,
  listOperatorRegistryRepositories,
  listOperatorRegistryTags,
  operatorReadRoute
} from "../../apps/console-ui/src/api/console-read-api.ts";
import {
  authorizationFailureState,
  isAuthorizationFailure
} from "../../apps/console-ui/src/app/authorization-error.ts";
import { cloudIdentity } from "../../apps/console-ui/src/app/console-identity.ts";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

function stubSourceEnvelope() {
  const requests: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    requests.push(String(input));
    return new Response(JSON.stringify({
      source: "control-plane",
      status: "empty",
      available: true,
      fetchedAt: "2026-10-20T00:00:00Z",
      data: {}
    }), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;
  return requests;
}

// #986: the operator console authenticates with CloudIdentity, so every operator
// read it renders must reach the BFF route that authorizes that same session.
// These are the exact callers the admin pages use; a future edit that sends one
// back to /api/operator/* fails here instead of only on an installed product.
test("operator console reads resolve to the Cloud BFF under CloudIdentity", async () => {
  assert.equal(cloudIdentity, false, "the Node lane has no baked cloud identity");
  assert.equal(operatorReadRoute(true), "/api/v2/operator");
  assert.equal(operatorReadRoute(false), "/api/operator");

  const requests = stubSourceEnvelope();
  await getOperatorAccounts(true);
  await getOperatorOverview(undefined, true);
  await getOperatorAccountsPage(2, 20, undefined, true);
  await getOperatorWorkspaces(1, 20, undefined, true);
  await getOperatorWorkspace("ws/1", undefined, true);
  await getOperatorWorkspaceRuntimeImagePolicy(undefined, true);
  await getOperatorAnnouncements(1, 20, undefined, true);
  await getOperatorWorkspaceRuntimeImageReplacementPreview("ws/1", undefined, true);

  assert.deepEqual(requests, [
    "/api/v2/operator/accounts",
    "/api/v2/operator/overview",
    "/api/v2/operator/accounts?page=2&pageSize=20",
    "/api/v2/operator/workspaces?page=1&pageSize=20",
    "/api/v2/operator/workspaces/ws%2F1",
    "/api/v2/operator/workspace-runtime-image-policy",
    "/api/v2/operator/announcements?page=1&pageSize=20",
    "/api/v2/operator/workspaces/ws%2F1/runtime-image-replacements/preview"
  ]);
});

test("a retained build keeps the Control Plane operator reads", async () => {
  const requests = stubSourceEnvelope();
  await getOperatorOverview(undefined, false);
  await getOperatorAccounts(false);
  await getOperatorAnnouncements(1, 20, undefined, false);
  await getOperatorWorkspaceRuntimeImageReplacementPreview("ws/1", undefined, false);
  assert.deepEqual(requests, [
    "/api/operator/overview",
    "/api/operator/accounts",
    "/api/operator/announcements?page=1&pageSize=20",
    "/api/operator/workspaces/ws%2F1/runtime-image-replacements/preview"
  ]);
});

// #996: the last two operator reads must select the BFF route in a Cloud build.
// The file may still contain hardcoded POST/PUT operator commands (writes are
// out of scope), but no read may hardcode /api/operator/. This scans the exact
// two read functions so a future edit cannot quietly restore the retained path.
test("console-read-api.ts keeps no hardcoded operator read path", () => {
  const source = readFileSync(new URL("../../apps/console-ui/src/api/console-read-api.ts", import.meta.url), "utf8");
  // Every live operator GET selects the identity route at build time.
  for (const name of [
    "getOperatorAccounts",
    "getOperatorOverview",
    "getOperatorAccountsPage",
    "getOperatorWorkspaces",
    "getOperatorWorkspace",
    "getOperatorWorkspaceRuntimeImagePolicy",
    "getOperatorAnnouncements",
    "getOperatorWorkspaceRuntimeImageReplacementPreview",
    "getOperatorReconciliation",
    "getOperatorHealth",
    "getOperatorRuntimeObservations",
    "getWalletAdjustment",
    "getWorkspaceLaunchRecovery",
    "getOperatorWorkspaceRuntimeImageReplacement",
    "getOperatorWorkspaceApplicationDeployment",
    "listOperatorRegistryRepositories",
    "listOperatorRegistryTags"
  ]) {
    const start = source.indexOf(`export function ${name}`);
    assert.notEqual(start, -1, `${name} must exist`);
    const end = source.indexOf("\nexport ", start + 1);
    const body = source.slice(start, end === -1 ? undefined : end);
    assert.doesNotMatch(body, /`\/api\/operator\//, `${name} must not hardcode a retained operator read`);
    assert.match(body, /operatorReadRoute\(cloud\)/, `${name} must select the build-time operator route`);
  }
});

// A refused read is an authorization decision. It must reach the controllers as
// 401/403 with no synthesized source envelope, otherwise a missing operator
// privilege renders as a dead Control Plane instead of a forbidden action.
test("operator authorization failures are not flattened into an unavailable source", async () => {
  for (const [status, message] of [[401, "登录已失效，请重新登录"], [403, "没有权限访问运营数据"]] as const) {
    globalThis.fetch = (async () => new Response(JSON.stringify({
      code: status === 401 ? "UNAUTHENTICATED" : "FORBIDDEN",
      message: "operator privilege required"
    }), { status, headers: { "content-type": "application/json" } })) as typeof fetch;

    const error = await getOperatorOverview(undefined, true).then(() => null, (cause: unknown) => cause);
    assert.ok(error, `status ${status} must reject`);
    assert.equal(isAuthorizationFailure(error), true);
    assert.deepEqual(authorizationFailureState(error), { value: null, loading: false, error: message });
    assert.equal((error as { status?: number }).status, status);
    assert.equal((error as { payload?: unknown }).payload !== undefined, true, "the BFF error body is preserved");
  }
});

test("remaining operator GET callers use the same CloudIdentity BFF", async () => {
 const requests: string[] = [];
 globalThis.fetch = (async (input: RequestInfo | URL) => {
  const path = String(input); requests.push(path);
  const body = path.endsWith("/recovery") ? { operationId: "op-1", launchVersion: 1, status: "pending", stage: "accepted", allowedActions: [] } : { status: "available", available: true, source: "control-plane", fetchedAt: "2026-10-21T00:00:00Z", data: {} };
  return new Response(JSON.stringify(body), {status: 200});
 }) as typeof fetch;
 await getOperatorHealth(undefined, true);
 await getOperatorRuntimeObservations(undefined, true);
 await getOperatorReconciliation(1, 20, undefined, true);
 await getWorkspaceLaunchRecovery("op-1", undefined, true);
 await getWalletAdjustment("op/1", undefined, true);
 await getOperatorWorkspaceRuntimeImageReplacement("ws/1", "op/1", undefined, true);
 await getOperatorWorkspaceApplicationDeployment("op/1", undefined, true);
 await listOperatorRegistryRepositories("namespace", true);
 await listOperatorRegistryTags("namespace", "repo/1", true);
 assert.deepEqual(requests, [
  "/api/v2/operator/health", "/api/v2/operator/runtime-observations", "/api/v2/operator/reconciliation?page=1&pageSize=20",
  "/api/v2/operator/workspace-launches/op-1/recovery", "/api/v2/operator/wallet-adjustments/op%2F1",
  "/api/v2/operator/workspaces/ws%2F1/runtime-image-replacements/op%2F1", "/api/v2/operator/application-deployments/op%2F1",
  "/api/v2/operator/registry/repositories?namespace=namespace", "/api/v2/operator/registry/tags/namespace/repo%2F1"
 ]);
});

const operatorCommands: Array<[string, string, (cloud: boolean) => Promise<unknown>]> = [
  ["image release", "/workspace-image-release-activations", cloud => operatorApi.activateOperatorWorkspaceImageRelease("r", 1, "reason", "csrf", "key", cloud)],
  ["image replacement", "/workspaces/ws/runtime-image-replacements", cloud => operatorApi.createOperatorWorkspaceRuntimeImageReplacement("ws", "digest", "reason", "csrf", "key", cloud)],
  ["wallet adjustment", "/accounts/acct/wallet-adjustments", cloud => operatorApi.createWalletAdjustment("acct", {} as never, "csrf", "key", cloud)],
  ["wallet recovery", "/wallet-adjustments/op/recover", cloud => operatorApi.recoverWalletAdjustment("op", {} as never, "csrf", "key", cloud)],
  ["disable account", "/accounts/acct/disable", cloud => operatorApi.disableOperatorAccount("acct", "reason", "csrf", "key", cloud)],
  ["eligibility", "/accounts/acct/workspace-purchase-eligibility", cloud => operatorApi.setOperatorWorkspacePurchaseEligibility("acct", false, "reason", "csrf", "key", cloud)],
  ["billing review", "/billing-reviews/workspace/ws/resolve", cloud => operatorApi.resolveBillingReview("workspace", "ws", {} as never, "csrf", "key", cloud)],
  ["launch recovery", "/workspace-launches/op/recover", cloud => operatorApi.recoverWorkspaceLaunch("op", {} as never, "csrf", "key", cloud)],
  ["announcement create", "/announcements", cloud => operatorApi.createOperatorAnnouncement({} as never, "csrf", "key", cloud)],
  ["announcement update", "/announcements/a", cloud => operatorApi.updateOperatorAnnouncement("a", {} as never, "csrf", "key", cloud)],
  ["announcement publish", "/announcements/a/publish", cloud => operatorApi.publishOperatorAnnouncement("a", {} as never, "csrf", "key", cloud)],
  ["announcement withdraw", "/announcements/a/withdraw", cloud => operatorApi.withdrawOperatorAnnouncement("a", "csrf", "key", cloud)],
  ["application admission", "/application-revisions", cloud => operatorApi.admitOperatorApplicationRevision({}, "csrf", "key", cloud)],
  ["application deployment", "/application-deployments", cloud => operatorApi.createOperatorWorkspaceApplicationDeployment("ws", "app", "rev", {} as never, "csrf", "key", [], undefined, cloud)],
  ["application retry", "/application-deployments/op/retry", cloud => operatorApi.retryOperatorWorkspaceApplicationDeployment("op", "csrf", cloud)],
  ["registry resolution", "/registry/resolve", cloud => operatorApi.resolveOperatorRegistryImage("ns", "repo", "tag", "csrf", "key", cloud)]
];

for (const [name, path, invoke] of operatorCommands) {
  test(`${name} refuses Cloud before fetch and preserves its legacy command`, async () => {
    const calls: string[] = [];
    globalThis.fetch = (async (url: string | URL | Request) => {
      calls.push(String(url));
      return new Response(JSON.stringify({ operationId: "op", launchVersion: 1, status: "manual_review", stage: "closeout", allowedActions: [] }), { status: 200, headers: { "content-type": "application/json" } });
    }) as typeof fetch;
    await assert.rejects(async () => invoke(true), /operator_write_unavailable/);
    assert.deepEqual(calls, []);
    await invoke(false);
    assert.deepEqual(calls, [`/api/operator${path}`]);
  });
}

test("activation uses its Cloud BFF owner and refuses the retired legacy route", async () => {
  const calls: string[] = [];
  globalThis.fetch = (async (url: string | URL | Request) => {
    calls.push(String(url));
    return new Response("{}", { status: 201, headers: { "content-type": "application/json" } });
  }) as typeof fetch;
  await assert.rejects(async () => operatorApi.provisionOperatorAccount({} as never, "csrf", "key", false), /activation_moved/);
  assert.deepEqual(calls, []);
  await operatorApi.provisionOperatorAccount({} as never, "csrf", "key", true);
  assert.deepEqual(calls, ["/api/v2/operator/accounts"]);
});
