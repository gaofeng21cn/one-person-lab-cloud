import assert from "node:assert/strict";
import test from "node:test";
import { readNativeAppUpgradeContext, updateNativeDefaultApp } from "../../apps/console-ui/src/api/workspace-app-upgrade-api.ts";

const reply = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
const intent = { workspaceId: "ws_native/1042", expectedCurrentDeploymentId: "dep-original", runtimeVersionId: "rv-next", idempotencyKey: "upgrade-original" };
const operation = { owner: "serve", kind: "update_workspace", operationId: "op-upgrade", status: "running", pollAfterSeconds: 1 };

test("native upgrade reads a unique Serve selection and offers only approved RuntimeVersions", async () => {
  const previous = globalThis.fetch;
  const seen: string[] = [];
  globalThis.fetch = (async (input) => {
    const path = String(input); seen.push(path);
    if (path.startsWith("/api/v2/catalog/")) return reply({ items: [{ id: "rv-next", status: "approved" }, { id: "revoked", status: "revoked" }] });
    if (path.includes("cursor=next")) return reply({ items: [{ id: "dep-original", workspaceId: intent.workspaceId, status: "active" }] });
    return reply({ items: [{ id: "dep-old", workspaceId: intent.workspaceId, status: "superseded" }], nextCursor: "next" });
  }) as typeof fetch;
  try {
    const result = await readNativeAppUpgradeContext(intent.workspaceId);
    assert.equal(result.currentDeploymentId, "dep-original");
    assert.deepEqual(result.releases.map((release) => release.id), ["rv-next"]);
    assert.ok(seen.some((path) => path.includes("ws_native%2F1042/deployments")));
    assert.ok(seen.every((path) => path.startsWith("/api/v2/")));
  } finally { globalThis.fetch = previous; }
});

test("native upgrade replays the exact typed command and key after a lost response", async () => {
  const previous = globalThis.fetch;
  const requests: RequestInit[] = [];
  globalThis.fetch = (async (input, init) => {
    assert.equal(String(input), "/api/v2/workspaces/ws_native%2F1042/update");
    requests.push(init!);
    if (requests.length === 1) throw new TypeError("response lost");
    return reply(operation, 202);
  }) as typeof fetch;
  try {
    await assert.rejects(updateNativeDefaultApp(intent, "csrf"), /response lost/);
    assert.equal((await updateNativeDefaultApp(intent, "csrf")).operationId, operation.operationId);
    for (const request of requests) {
      assert.equal(request.method, "POST");
      assert.equal((request.headers as Record<string, string>)["Idempotency-Key"], intent.idempotencyKey);
      assert.equal((request.headers as Record<string, string>)["X-CSRF-Token"], "csrf");
      assert.deepEqual(JSON.parse(String(request.body)), { applicationSelection: { kind: "opl_app", runtimeVersionId: "rv-next" }, expectedCurrentDeploymentId: "dep-original" });
    }
  } finally { globalThis.fetch = previous; }
});

test("native upgrade refuses ambiguous/current foreign deployments and malformed owner operations", async () => {
  const previous = globalThis.fetch;
  try {
    for (const items of [[], [{ id: "dep-a", workspaceId: "foreign", status: "active" }], [{ id: "dep-a", workspaceId: intent.workspaceId, status: "active" }, { id: "dep-b", workspaceId: intent.workspaceId, status: "active" }]]) {
      globalThis.fetch = (async (input) => reply({ items: String(input).includes("catalog") ? [] : items })) as typeof fetch;
      await assert.rejects(readNativeAppUpgradeContext(intent.workspaceId));
    }
    for (const value of [{ ...operation, owner: "workspace" }, { ...operation, pollAfterSeconds: 0 }, { ...operation, status: "unknown" }]) {
      globalThis.fetch = (async () => reply(value, 202)) as typeof fetch;
      await assert.rejects(updateNativeDefaultApp(intent, "csrf"));
    }
    globalThis.fetch = (async () => reply({ error: "current_deployment_conflict" }, 409)) as typeof fetch;
    await assert.rejects(updateNativeDefaultApp(intent, "csrf"), /current_deployment_conflict/);
  } finally { globalThis.fetch = previous; }
});
