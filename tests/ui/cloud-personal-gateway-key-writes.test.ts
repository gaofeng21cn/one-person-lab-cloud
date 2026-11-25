import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { afterEach, test } from "node:test";

import {
  createGatewayKey,
  decodePersonalGatewayKeyOperation,
  decodePersonalGatewayKeySecret,
  deleteGatewayKey,
  personalGatewayKeyCreateRoute,
  personalGatewayKeyRevealRoute,
  personalGatewayKeyRevokeRoute,
  personalGatewayKeyUpdateRoute,
  personalGatewayCreateBody,
  personalGatewayUpdateBody,
  revealGatewayKey,
  updateGatewayKey
} from "../../apps/console-ui/src/api/console-read-api.ts";
import { cloudIdentity } from "../../apps/console-ui/src/app/console-identity.ts";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

// The four personal Key writes are the same decision as the personal reads: the
// baked identity selects the owner. A cloud build asks the Gateway owner through
// the BFF; a retained build keeps the Control Plane route it authenticated
// against. Neither identity falls back to the other's route.
test("the personal Gateway Key writes follow the baked identity", () => {
  assert.equal(personalGatewayKeyCreateRoute(true), "/api/v2/gateway/keys");
  assert.equal(personalGatewayKeyUpdateRoute(true, "71"), "/api/v2/gateway/keys/71");
  assert.equal(personalGatewayKeyRevokeRoute(true, "71"), "/api/v2/gateway/keys/71");
  assert.equal(personalGatewayKeyRevealRoute(true, "71"), "/api/v2/gateway/keys/71/reveal");

  assert.equal(personalGatewayKeyCreateRoute(false), "/api/gateway/keys");
  assert.equal(personalGatewayKeyUpdateRoute(false, "71"), "/api/gateway/keys/71");
  assert.equal(personalGatewayKeyRevokeRoute(false, "71"), "/api/gateway/keys/71");
  assert.equal(personalGatewayKeyRevealRoute(false, "71"), "/api/gateway/keys/71/reveal");

  for (const choose of [personalGatewayKeyCreateRoute, personalGatewayKeyUpdateRoute, personalGatewayKeyRevokeRoute, personalGatewayKeyRevealRoute]) {
    assert.notEqual(choose(true, "71"), choose(false, "71"), `${choose.name} must follow the baked identity`);
  }
  for (const route of [personalGatewayKeyCreateRoute(true), personalGatewayKeyUpdateRoute(true, "71"), personalGatewayKeyRevokeRoute(true, "71"), personalGatewayKeyRevealRoute(true, "71")]) {
    assert.match(route, /^\/api\/v2\//);
  }
});

test("the personal Gateway Key create command uses the owner contract's own spelling", () => {
  const body = personalGatewayCreateBody({
    name: "personal key",
    groupId: "4",
    ipWhitelist: ["10.0.0.0/8"],
    ipBlacklist: ["192.168.1.1"],
    quotaUsdMicros: 1_500_000,
    expiresInDays: 30,
    rateLimit5hUsdMicros: 2_000_000,
    rateLimit1dUsdMicros: 3_000_000,
    rateLimit7dUsdMicros: 4_000_000
  });

  assert.deepEqual(body, {
    name: "personal key",
    groupId: "4",
    quotaUSDMicros: "1500000",
    ipWhitelist: ["10.0.0.0/8"],
    ipBlacklist: ["192.168.1.1"],
    expiresInDays: 30,
    rateLimit5hUSDMicros: "2000000",
    rateLimit1dUSDMicros: "3000000",
    rateLimit7dUSDMicros: "4000000"
  });
  // The money is an exact decimal string, never a lossy JSON number.
  assert.equal(typeof body.quotaUSDMicros, "string");
});

test("the personal Gateway Key update command states a cleared expiry explicitly", () => {
  const cleared = personalGatewayUpdateBody({
    name: "renamed personal key",
    groupId: "4",
    ipWhitelist: ["10.0.0.0/8"],
    ipBlacklist: [],
    quotaUsdMicros: 2_000_000,
    expiresAt: "",
    rateLimit5hUsdMicros: 5_000_000,
    resetQuota: true,
    resetRateLimitUsage: true,
    enabled: false
  });

  assert.equal(cleared.clearExpiresAt, true);
  assert.equal("expiresAt" in cleared, false, "a cleared expiry never travels as an instant");
  assert.deepEqual(cleared.ipWhitelist, { values: ["10.0.0.0/8"] });
  assert.deepEqual(cleared.ipBlacklist, { values: [] });
  assert.equal(cleared.quotaUSDMicros, "2000000");
  assert.equal(cleared.resetQuota, true);
  assert.equal(cleared.resetRateLimitUsage, true);
  assert.equal(cleared.enabled, false);

  const set = personalGatewayUpdateBody({ expiresAt: "2026-12-01T00:00:00Z" });
  assert.equal(set.expiresAt, "2026-12-01T00:00:00.000Z");
  assert.equal("clearExpiresAt" in set, false);

  const unchanged = personalGatewayUpdateBody({ name: "renamed personal key" });
  assert.deepEqual(unchanged, { name: "renamed personal key" });
});

test("the personal Gateway Key secret projection carries the owner's answer once", () => {
  const key = {
    id: "71",
    name: "personal key",
    groupId: "4",
    status: "active",
    kind: "general",
    manageable: false,
    deletable: false,
    ipWhitelist: [],
    ipBlacklist: [],
    quotaUSDMicros: "1500000",
    quotaUsedUSDMicros: "0",
    rateLimit5hUSDMicros: "2000000",
    rateLimit1dUSDMicros: "3000000",
    rateLimit7dUSDMicros: "4000000",
    usage5hUSDMicros: "0",
    usage1dUSDMicros: "0",
    usage7dUSDMicros: "0",
    currentConcurrency: 0
  };
  const secret = decodePersonalGatewayKeySecret({ key, secret: "raw-secret" });

  assert.equal(secret.id, "71");
  assert.equal(secret.name, "personal key");
  assert.equal(secret.status, "active");
  assert.equal(secret.value, "raw-secret");
  assert.throws(() => decodePersonalGatewayKeySecret({ key, secret: "" }), /invalid_personal_gateway_secret/);
});

test("the personal Gateway Key revoke projection reports the owner's terminal operation", () => {
  const operation = decodePersonalGatewayKeyOperation({
    operationId: "71",
    kind: "revoke_key",
    status: "succeeded",
    stage: "succeeded",
    requestId: "request-1",
    createdAt: "2026-12-01T00:00:00Z",
    updatedAt: "2026-12-01T00:00:00Z"
  });

  assert.equal(operation.operationId, "71");
  assert.equal(operation.status, "deleted");
  assert.equal(operation.phase, "succeeded");
  assert.equal(operation.createdAt, "2026-12-01T00:00:00Z");
  // An operation the owner did not confirm as a revoke is never reported as a
  // finished delete.
  assert.throws(() => decodePersonalGatewayKeyOperation({ operationId: "71", kind: "revoke_key", status: "accepted" }), /invalid_personal_gateway_operation/);
});

test("the retained identity keeps the Control Plane write routes it authenticated against", async () => {
  const requested: Array<{ path: string; method: string; idempotency: string | null }> = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    requested.push({
      path: String(input),
      method: String(init?.method ?? "GET"),
      idempotency: headers.get("Idempotency-Key")
    });
    return new Response(JSON.stringify({
      source: "sub2api",
      status: "available",
      available: true,
      fetchedAt: "2026-12-01T00:00:00Z",
      data: { id: "71", name: "personal key", status: "active", value: "raw-secret" }
    }), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;

  await createGatewayKey({ name: "personal key", groupId: "4", quotaUsdMicros: 0 }, "csrf-1", "create-1");
  await updateGatewayKey("71", { quotaUsdMicros: 1 }, "csrf-1", "update-1");
  await deleteGatewayKey("71", "csrf-1", "delete-1");
  await revealGatewayKey("71", "csrf-1");

  assert.deepEqual(requested.map((row) => `${row.method} ${row.path}`), [
    "POST /api/gateway/keys",
    "PATCH /api/gateway/keys/71",
    "DELETE /api/gateway/keys/71",
    "POST /api/gateway/keys/71/reveal"
  ]);
  // The retained reveal never carried an idempotency key and still does not.
  assert.equal(requested[3].idempotency, null);
  assert.equal(requested[0].idempotency, "create-1");
});

// The refusal the cloud build used to answer is gone: the cloud path asks the
// Gateway owner through the BFF instead.
test("the cloud identity no longer refuses the personal Gateway Key writes", () => {
  const readApi = readFileSync(
    new URL("../../apps/console-ui/src/api/console-read-api.ts", import.meta.url),
    "utf8"
  );

  assert.equal(readApi.includes("gateway_key_capability_unavailable"), false,
    "the personal Gateway Key writes still refuse the cloud identity");
  // Each of the four writes asks the owner through the BFF under the cloud
  // identity, and keeps the retained Control Plane route otherwise.
  for (const route of [
    "personalGatewayKeyCreateRoute(true)",
    "personalGatewayKeyUpdateRoute(true, keyId)",
    "personalGatewayKeyRevokeRoute(true, keyId)",
    "personalGatewayKeyRevealRoute(true, keyId)"
  ]) {
    assert.ok(readApi.includes(route), `the cloud write path never reaches ${route}`);
  }
});

test("the Node lane is the retained identity, so the cloud route test does not depend on the environment", () => {
  assert.equal(cloudIdentity, false);
});

// The published Console reaches the Gateway owner through the BFF, so the
// personal Key management actions are the customer's in every baked identity.
// The owner decides which Key may be managed; the Console never withholds the
// action because of the identity it was built with.
test("the personal Key management actions are not withheld from the cloud identity", () => {
  const keysPanel = readFileSync(
    new URL("../../apps/console-ui/src/components/keys/KeysPanel.tsx", import.meta.url),
    "utf8"
  );

  assert.equal(keysPanel.includes("仅供查看"), false,
    "the cloud identity still answers with a read-only personal Key surface");
  assert.equal(keysPanel.includes("API 密钥仅供查看"), false,
    "the cloud identity still shows the read-only personal Key notice");
  assert.equal(keysPanel.includes("!cloudIdentity &&"), false,
    "the personal Key management actions are still gated on the baked identity");
  // The owner owns the fact that decides whether a Key may be managed, and the
  // Console asks it.
  assert.ok(keysPanel.includes("key.manageable"), "the Console ignores the owner's manageable fact");
  assert.ok(keysPanel.includes("key.deletable"), "the Console ignores the owner's deletable fact");
});
