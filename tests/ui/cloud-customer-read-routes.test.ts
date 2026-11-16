import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { afterEach, test } from "node:test";

import { decodeSource } from "../../apps/console-ui/src/api/dtos.ts";
import {
  announcementReadRoute,
  billingReadRoute,
  gatewayUsageSummaryRoute,
  getAnnouncements,
  getBillingReceipts,
  walletReadRoute
} from "../../apps/console-ui/src/api/console-read-api.ts";
import { cloudIdentity } from "../../apps/console-ui/src/app/console-identity.ts";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

// #840: a CloudIdentity session must read the BFF owner surfaces, never the
// retained Control Plane routes that answer it with 401. These are the same
// decisions the readers use, so a future edit that swaps either side fails here
// instead of only on an installed product.
test("the cloud identity selects only the Cloud BFF owner routes", () => {
  const cloudRoutes = [
    walletReadRoute(true),
    gatewayUsageSummaryRoute(true),
    billingReadRoute(true),
    announcementReadRoute(true)
  ];

  assert.deepEqual(cloudRoutes, [
    "/api/v2/wallet",
    "/api/v2/usage/summary",
    "/api/v2/legacy-billing",
    "/api/v2/announcements"
  ]);
  for (const route of cloudRoutes) assert.match(route, /^\/api\/v2\//);
});

test("the retained identity keeps the Control Plane read routes it authenticated against", () => {
  const retainedRoutes = [
    walletReadRoute(false),
    gatewayUsageSummaryRoute(false),
    billingReadRoute(false),
    announcementReadRoute(false)
  ];

  assert.deepEqual(retainedRoutes, [
    "/api/gateway/wallet",
    "/api/gateway/usage-summary",
    "/api/billing",
    "/api/announcements"
  ]);
  for (const route of retainedRoutes) {
    assert.match(route, /^\/api\//);
    assert.doesNotMatch(route, /^\/api\/v2\//);
  }
});

test("no customer read chooses the same path for both identities", () => {
  for (const choose of [walletReadRoute, gatewayUsageSummaryRoute, billingReadRoute, announcementReadRoute]) {
    assert.notEqual(choose(true), choose(false), `${choose.name} must follow the baked identity`);
  }
});

test("the billing and announcement readers use the identity route decision", async () => {
  const requested: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const path = String(input);
    requested.push(path);
    if (path.startsWith("/api/billing/receipts")) {
      return new Response(JSON.stringify({
        source: "ledger",
        status: "empty",
        available: true,
        fetchedAt: "2026-10-18T00:00:00Z",
        data: { receipts: [], hasMore: false }
      }), { status: 200, headers: { "content-type": "application/json" } });
    }
    return new Response(JSON.stringify({
      source: "control-plane",
      status: "empty",
      available: true,
      fetchedAt: "2026-10-18T00:00:00Z",
      data: { items: [], page: 1, pageSize: 20, total: 0 }
    }), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;

  await getBillingReceipts();
  await getAnnouncements();
  assert.deepEqual(requested, ["/api/billing/receipts?limit=20", "/api/announcements?page=1&pageSize=20"]);
});

// The Control Plane withdrew its package-pricing catalog and preview in the
// R6 slice: the Resource Catalog prices new resources. The Console must not
// ask the retired routes for a price again.
test("the launch controller no longer reads the retired package pricing routes", () => {
  const controller = readFileSync(
    new URL("../../apps/console-ui/src/app/use-workspace-launch-controller.ts", import.meta.url),
    "utf8"
  );
  const readApi = readFileSync(
    new URL("../../apps/console-ui/src/api/console-read-api.ts", import.meta.url),
    "utf8"
  );

  for (const retired of ["/api/pricing/catalog", "/api/pricing/preview", "getPricingCatalog", "previewPricing", "packagePricingReadPlan"]) {
    assert.equal(controller.includes(retired), false, `the launch controller still reaches ${retired}`);
    assert.equal(readApi.includes(retired), false, `the read API still reaches ${retired}`);
  }
});

test("an unavailable owner answer stays an explicit unavailable source with no data", () => {
  const envelope = decodeSource<unknown>({
    source: "gateway",
    status: "unavailable",
    available: false,
    fetchedAt: "2026-10-18T00:00:00Z",
    reasonCode: "gateway_unavailable"
  });

  assert.equal(envelope.status, "unavailable");
  if (envelope.status !== "unavailable") throw new Error("expected an unavailable source envelope");
  assert.equal(envelope.available, false);
  assert.equal(envelope.reasonCode, "gateway_unavailable");
  assert.equal("data" in envelope, false);
});

test("the Node lane is the retained identity, so the cloud route test does not depend on the environment", () => {
  assert.equal(cloudIdentity, false);
});


test("Workspace offers preserve owner-fixed pairs and exact full USD micros", async () => {
  const { listAvailableComputePlans } = await import("../../apps/console-ui/src/api/workspaces-api.ts");
  const offer = { storagePlanId: "disk-owned-pair", storageName: "Included disk", storageCapacityGiB: 10, pricePolicyVersionId: "price-exact", monthlyUSDMicros: "9007199254740993" };
  globalThis.fetch = (async () => new Response(JSON.stringify({ items: [{ id: "compute-opaque", name: "Basic", workspaceOffers: [offer] }] }), { headers: { "content-type": "application/json" } })) as typeof fetch;
  assert.deepEqual((await listAvailableComputePlans())[0].workspaceOffers, [offer]);
  for (const invalid of [
    { ...offer, monthlyUSDMicros: 52580000 }, { ...offer, monthlyUSDMicros: "-1" },
    { ...offer, monthlyUSDMicros: "9223372036854775808" }, { ...offer, storageCapacityGiB: 0 },
    { ...offer, pricePolicyVersionId: "" }, { ...offer, storagePlanId: "" }
  ]) {
    globalThis.fetch = (async () => new Response(JSON.stringify({ items: [{ workspaceOffers: [invalid] }] }))) as typeof fetch;
    await assert.rejects(listAvailableComputePlans(), /invalid_workspace_offer/);
  }
  globalThis.fetch = (async () => new Response(JSON.stringify({ items: [{ workspaceOffers: [offer, offer] }] }))) as typeof fetch;
  await assert.rejects(listAvailableComputePlans(), /invalid_workspace_offer/);
});

test("Workspace projection keeps exact plan references without inventing financial facts", async () => {
  const { projectCustomerWorkspace } = await import("../../apps/console-ui/src/api/workspaces-api.ts");
  const workspace = projectCustomerWorkspace({ id: "workspace-1", name: "Workspace", computePlanId: "compute-opaque", storagePlanId: "disk-opaque", status: "active", deliveryModel: "agent_saas", resourceReadiness: "ready", applicationAvailability: "available", createdAt: "2026-11-12T00:00:00Z", updatedAt: "2026-11-12T00:00:00Z", version: "1" });
  assert.equal(workspace.computePlanId, "compute-opaque");
  assert.equal(workspace.storagePlanId, "disk-opaque");
  assert.equal(workspace.totalUsdMicros, undefined);
  assert.equal(workspace.autoRenew, undefined);
});
