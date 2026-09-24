import assert from "node:assert/strict";
import test from "node:test";

import { getAgentDelivery } from "../../apps/console-ui/src/api/delivery-api.ts";

test("Agent delivery adapter calls only the same-origin BFF delivery path", async () => {
  const calls: string[] = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    calls.push(String(input));
    return new Response(JSON.stringify({
      workspaceId: "ws-1",
      workspace: { owner: "workspace", state: "WORKSPACE_STATUS_ENUM_ACTIVE", details: { computePlanId: "compute-plan-a" } },
      capabilityVersion: { owner: "capability", state: "CAPABILITY_VERSION_STATUS_ENUM_READY", details: { artifactDigest: `sha256:${"a".repeat(64)}` } },
      build: { owner: "build", state: "BUILD_JOB_STATUS_ENUM_SUCCEEDED", details: { buildId: "build-1" } },
      serve: { owner: "serve", state: "DEPLOYMENT_STATUS_ENUM_ACTIVE", details: { deploymentId: "dep-1", accessUrl: "https://agent.example.test" } }
    }), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;
  try {
    const view = await getAgentDelivery("ws-1");
    assert.equal(calls.length, 1);
    assert.equal(calls[0], "/api/v2/delivery/ws-1");
    assert.equal(view.serve.owner, "serve");
    assert.equal(view.serve.details.accessUrl, "https://agent.example.test");
    assert.equal(view.capabilityVersion.owner, "capability");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Agent delivery adapter encodes the workspace id and surfaces an upstream failure", async () => {
  const calls: string[] = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    calls.push(String(input));
    return new Response(JSON.stringify({ ok: false, error: "owner_read_failed", safeMessage: "serve unavailable" }), { status: 502, headers: { "content-type": "application/json" } });
  }) as typeof fetch;
  try {
    await assert.rejects(() => getAgentDelivery("ws/1"));
    assert.equal(calls[0], "/api/v2/delivery/ws%2F1");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
