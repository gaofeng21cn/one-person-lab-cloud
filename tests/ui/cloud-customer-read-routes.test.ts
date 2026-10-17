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
  getPricingCatalog,
  packagePricingReadPlan,
  previewPricing,
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

// Cloud has no package-pricing catalog owner yet, so the legacy catalog and
// preview must fail closed. They must not fall back to the retained route whose
// Account session a CloudIdentity build does not hold.
test("the cloud identity refuses the retained package pricing catalog and preview before any request", async () => {
  const requested: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    requested.push(String(input));
    throw new Error("unexpected_retained_pricing_request");
  }) as typeof fetch;

  assert.deepEqual(packagePricingReadPlan(true), {
    state: "unavailable",
    owner: "resource_catalog",
    reasonCode: "pricing_catalog_owner_unavailable"
  });
  await assert.rejects(getPricingCatalog(true), /pricing_catalog_owner_unavailable/);
  await assert.rejects(
    previewPricing({ resourceType: "workspace", packageId: "basic" }, "csrf-1", true),
    /pricing_catalog_owner_unavailable/
  );
  assert.deepEqual(requested, []);
});

test("the retained identity still prices through the Control Plane package routes", async () => {
  const requested: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const path = String(input);
    requested.push(path);
    if (path === "/api/pricing/catalog") {
      return new Response(JSON.stringify({ priceVersion: "2026-10", packages: [] }), {
        status: 200,
        headers: { "content-type": "application/json" }
      });
    }
    return new Response(JSON.stringify({ chargeUsdMicros: 1 }), {
      status: 200,
      headers: { "content-type": "application/json" }
    });
  }) as typeof fetch;

  assert.deepEqual(packagePricingReadPlan(false), { state: "read", owner: "control_plane" });
  assert.equal((await getPricingCatalog(false)).priceVersion, "2026-10");
  assert.equal((await previewPricing({ resourceType: "workspace", packageId: "basic" }, "csrf-1", false)).chargeUsdMicros, 1);
  assert.deepEqual(requested, ["/api/pricing/catalog", "/api/pricing/preview"]);
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

test("the launch controller refuses the package pricing read before it reaches the API", () => {
  const controller = readFileSync(
    new URL("../../apps/console-ui/src/app/use-workspace-launch-controller.ts", import.meta.url),
    "utf8"
  );
  const decision = controller.indexOf("packagePricingReadPlan(cloudIdentity)");
  const read = controller.indexOf("getPricingCatalog(");

  assert.notEqual(decision, -1, "the controller must make the identity decision explicitly");
  assert.notEqual(read, -1, "the legacy branch still reads the package pricing catalog");
  assert.ok(decision < read, "the identity decision must run before the retained pricing read");
  assert.match(controller, /plan\.state === "unavailable"/);
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
