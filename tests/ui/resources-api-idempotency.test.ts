import assert from "node:assert/strict";
import { afterEach, test } from "node:test";

import * as workspaceApi from "../../apps/console-ui/src/api/workspaces-api.ts";

const originalFetch = globalThis.fetch;

afterEach(() => { globalThis.fetch = originalFetch; });

function response(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { "content-type": "application/json" } });
}

test("Workspace launch polling addresses the exact operation", async () => {
  let url = "";
  globalThis.fetch = async (input) => {
    url = String(input);
    return response({ operationId: "launch-alpha", status: "succeeded", phase: "completed" });
  };
  const result = await workspaceApi.getWorkspaceLaunch("launch-alpha");
  assert.equal(url, "/api/workspace-launches/launch-alpha");
  assert.equal(result.status, "succeeded");
  assert.equal(workspaceApi.isTerminalWorkspaceLaunch(result.status), true);
});

test("Workspace launch recovery lists the current account operations", async () => {
  let url = "";
  globalThis.fetch = async (input) => {
    url = String(input);
    return response([{ operationId: "launch-alpha", status: "preparing", phase: "compute" }]);
  };

  const launches = await workspaceApi.getWorkspaceLaunches();
  assert.equal(url, "/api/workspace-launches");
  assert.equal(launches[0]?.operationId, "launch-alpha");
});

test("Workspace credential reveal uses its explicit route and request key", async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), init });
    return response({ workspaceId: "workspace-alpha", access: { account: "owner", username: "owner", password: "secret", credentialStatus: "active", credentialVersion: "v2" } });
  };

  await workspaceApi.revealWorkspaceCredentials("workspace-alpha", "csrf-alpha");

  assert.deepEqual(requests.map(({ url }) => url), [
    "/api/workspaces/workspace-alpha/runtime-credentials/reveal"
  ]);
  const keys = requests.map(({ init }) => new Headers(init?.headers).get("Idempotency-Key"));
  assert.match(keys[0] ?? "", /^runtime-credential-reveal:/);
  assert.deepEqual(JSON.parse(String(requests[0]?.init?.body)), {});
  assert.equal(new Headers(requests[0]?.init?.headers).get("x-opl-csrf"), "csrf-alpha");
});

test("the Workspace renewal read is a GET on the retained route", async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), init });
    return response({ autoRenew: true, effectiveAfter: "2026-07-31T00:00:00Z", nextRenewalAt: "2026-07-31T00:00:00Z", paidThrough: "2026-08-01T00:00:00Z", renewalStatus: "scheduled", recovery: { state: "not_required", reason: "" } });
  };

  const renewal = await workspaceApi.getWorkspaceRenewal("workspace-alpha");

  assert.deepEqual(requests.map(({ url }) => url), ["/api/workspaces/workspace-alpha/renewal"]);
  assert.equal(requests[0]?.init?.method, undefined);
  assert.equal(renewal.autoRenew, true);
});
