import assert from "node:assert/strict";
import { afterEach, test } from "node:test";

import {
  getOperatorAccounts,
  getOperatorAccountsPage,
  getOperatorOverview,
  getOperatorWorkspace,
  getOperatorWorkspaceRuntimeImagePolicy,
  getOperatorWorkspaces,
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

  assert.deepEqual(requests, [
    "/api/v2/operator/accounts",
    "/api/v2/operator/overview",
    "/api/v2/operator/accounts?page=2&pageSize=20",
    "/api/v2/operator/workspaces?page=1&pageSize=20",
    "/api/v2/operator/workspaces/ws%2F1",
    "/api/v2/operator/workspace-runtime-image-policy"
  ]);
});

test("a retained build keeps the Control Plane operator reads", async () => {
  const requests = stubSourceEnvelope();
  await getOperatorOverview(undefined, false);
  await getOperatorAccounts(false);
  assert.deepEqual(requests, ["/api/operator/overview", "/api/operator/accounts"]);
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
