import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { getPricingCatalog, previewPricing } from "../../apps/console-ui/src/api/console-read-api.ts";
import type {
  PlanId,
  PricingCatalogResponse,
  PricingPreviewResponse,
  WorkspacePricePreview
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  canCreateAgentQuote,
  canCreateAgentWorkspace,
  operationPollDelayMs,
  workspaceLaunchBalance,
  workspaceLaunchPreviewRequest,
  workspaceLaunchPricingSelection,
  workspaceLaunchUnpricedPackages,
  workspacePricePreview
} from "../../apps/console-ui/src/app/workspace-launch-controller-model.ts";

const repositoryRoot = fileURLToPath(new URL("../..", import.meta.url)).replace(/\/$/, "");
const consoleUiRoot = join(repositoryRoot, "apps/console-ui/src");


function consoleUiSources(directory = consoleUiRoot): Array<{ path: string; text: string }> {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = join(directory, entry.name);
    if (entry.isDirectory()) return consoleUiSources(full);
    if (!/\.(ts|tsx)$/.test(entry.name)) return [];
    return [{ path: full.slice(repositoryRoot.length + 1), text: readFileSync(full, "utf8") }];
  });
}

// callArguments returns the argument text of every call to the named function, so
// a test can assert what the call site actually passes rather than that a symbol
// appears somewhere in the file. A declaration is not a call.
function callArguments(source: string, callee: string): string[] {
  const argumentsList: string[] = [];
  for (let index = source.indexOf(callee); index !== -1; index = source.indexOf(callee, index + 1)) {
    if (/[A-Za-z0-9_$]/.test(source[index - 1] || "")) continue;
    if (/\bfunction\s+$/.test(source.slice(Math.max(0, index - 16), index))) continue;
    let depth = 0;
    let cursor = index + callee.length - 1;
    for (; cursor < source.length; cursor += 1) {
      if ("([{<".includes(source[cursor])) depth += 1;
      else if (")]}>".includes(source[cursor])) {
        depth -= 1;
        if (depth === 0) break;
      }
    }
    argumentsList.push(source.slice(index + callee.length, cursor).trim());
  }
  return argumentsList;
}

test("Agent launch stays closed until every owner readback is available", () => {
  const base = {
    sourceReady: true,
    hasName: true,
    hasCapabilityVersion: true,
    hasComputePlan: true,
    hasStoragePlan: true,
    modelSelectionsReady: true,
    walletReadbackReady: true,
    walletSufficient: true,
    quoteReady: false,
    quoteCurrent: false
  } as const;
  assert.equal(canCreateAgentQuote(base), true);
  assert.equal(canCreateAgentWorkspace(base, true), false);
  assert.equal(canCreateAgentWorkspace({ ...base, quoteReady: true, quoteCurrent: true }, false), false);
  assert.equal(canCreateAgentWorkspace({ ...base, quoteReady: true, quoteCurrent: true }, true), true);
});

test("Agent operation polling uses the owner's exact pollAfterSeconds", () => {
  assert.equal(operationPollDelayMs(1), 1000);
  assert.equal(operationPollDelayMs(300), 300000);
  assert.throws(() => operationPollDelayMs(undefined), /invalid_operation_poll_after_seconds/);
  assert.throws(() => operationPollDelayMs(0), /invalid_operation_poll_after_seconds/);
});

const pricedCatalog: PricingCatalogResponse = {
  priceVersion: "pilot-usd-2026-07-v1",
  billingUnit: "calendar_month",
  displayCurrency: "USD",
  walletCurrency: "USD",
  currency: "USD",
  packages: [
    { id: "basic", name: "Basic", available: true },
    { id: "pro", name: "Pro", available: true }
  ]
};

function ownerPreview(packageId: PlanId, compute: number, storage: number): PricingPreviewResponse {
  return {
    resourceType: "workspace",
    packageId,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    displayCurrency: "USD",
    billingUnit: "calendar_month",
    compute: { resourceType: "compute", packageId, priceVersion: "pilot-usd-2026-07-v1", currency: "USD", displayCurrency: "USD", billingUnit: "calendar_month", chargeUsdMicros: compute },
    storage: { resourceType: "storage", packageId, priceVersion: "pilot-usd-2026-07-v1", currency: "USD", displayCurrency: "USD", billingUnit: "calendar_month", chargeUsdMicros: storage },
    totalChargeUsdMicros: compute + storage
  };
}

test("an owner pricing readback is the only source of the selected plan and its price", () => {
  const priced = {
    basic: workspacePricePreview(ownerPreview("basic", 50_000_000, 2_580_000)),
    pro: workspacePricePreview(ownerPreview("pro", 214_280_000, 25_800_000))
  } as Partial<Record<PlanId, WorkspacePricePreview>>;

  const basic = workspaceLaunchPricingSelection({ catalog: pricedCatalog, planId: "basic", previews: priced });
  assert.equal(basic.selectedPlan?.id, "basic");
  assert.equal(basic.selectedPrice, 52_580_000);

  const pro = workspaceLaunchPricingSelection({ catalog: pricedCatalog, planId: "pro", previews: priced });
  assert.equal(pro.selectedPlan?.name, "Pro");
  assert.equal(pro.selectedPrice, 240_080_000);

  // An owner response without a total is not a price; it is not rounded into one.
  assert.equal(workspacePricePreview({ ...ownerPreview("basic", 1, 2), totalChargeUsdMicros: undefined }), null);
  assert.equal(workspacePricePreview({ ...ownerPreview("basic", 1, 2), compute: undefined }), null);

  // Without a catalog readback there is no plan and no price to show.
  const unavailable = workspaceLaunchPricingSelection({ catalog: null, planId: "basic", previews: priced });
  assert.equal(unavailable.selectedPlan, null);
  assert.equal(unavailable.selectedPrice, null);
});

test("a real catalog readback names the selected plan and its price", () => {
  const priced = {
    basic: workspacePricePreview(ownerPreview("basic", 50_000_000, 2_580_000))
  } as Partial<Record<PlanId, WorkspacePricePreview>>;
  const selection = workspaceLaunchPricingSelection({ catalog: pricedCatalog, planId: "basic", previews: priced });

  assert.equal(selection.selectedPlan?.id, "basic");
  assert.equal(selection.selectedPrice, 52_580_000);

  const unreachable = workspaceLaunchPricingSelection({ catalog: null, planId: "basic", previews: {} });
  assert.equal(unreachable.selectedPlan, null);
  assert.equal(unreachable.selectedPrice, null);
});

test("a zero total is the owner's own price, not a missing one", () => {
  // Resource billing off: the owner zeroes every component and returns 0.
  const unbilled = { ...ownerPreview("basic", 0, 0), totalChargeUsdMicros: 0 };
  const preview = workspacePricePreview(unbilled);
  assert.ok(preview);
  assert.equal(preview.totalChargeUsdMicros, 0);

  const selection = workspaceLaunchPricingSelection({ catalog: pricedCatalog, planId: "basic", previews: { basic: preview } });
  assert.equal(selection.selectedPrice, 0);
  assert.equal(
    workspaceLaunchBalance({ resourceBillingMode: "none", customerOwned: false, selectedPrice: 0, walletUsdMicros: null }),
    true,
    "an unbilled purchase is affordable without a balance read"
  );
});

test("balance compares the Gateway balance against the selected price, not the Agent wallet", () => {
  const price = { resourceBillingMode: "enabled", customerOwned: false, selectedPrice: 52_580_000 } as const;
  assert.equal(workspaceLaunchBalance({ ...price, walletUsdMicros: "500000000" }), true);
  assert.equal(workspaceLaunchBalance({ ...price, walletUsdMicros: "52580000" }), true);
  assert.equal(workspaceLaunchBalance({ ...price, walletUsdMicros: "52579999" }), false);
  assert.equal(workspaceLaunchBalance({ ...price, walletUsdMicros: null }), null, "an unread balance is not an affordable one");
  assert.equal(workspaceLaunchBalance({ ...price, walletUsdMicros: "not-a-number" }), null);
  assert.equal(workspaceLaunchBalance({ ...price, selectedPrice: null, walletUsdMicros: "500000000" }), null);
  assert.equal(workspaceLaunchBalance({ ...price, selectedPrice: 1.5, walletUsdMicros: "500000000" }), null);
  assert.equal(workspaceLaunchBalance({ resourceBillingMode: undefined, customerOwned: false, selectedPrice: 1, walletUsdMicros: "500000000" }), true);
  assert.equal(workspaceLaunchBalance({ resourceBillingMode: "none", customerOwned: false, selectedPrice: 52_580_000, walletUsdMicros: null }), true);
  assert.equal(workspaceLaunchBalance({ resourceBillingMode: "enabled", customerOwned: true, selectedPrice: 52_580_000, walletUsdMicros: null }), true);
});

test("the pricing read reaches the selection and the verdict over the real routes", async () => {
  const originalFetch = globalThis.fetch;
  const requested: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const path = String(input);
    requested.push(path);
    if (path === "/api/pricing/catalog") {
      return new Response(JSON.stringify(pricedCatalog), { status: 200, headers: { "content-type": "application/json" } });
    }
    const packageId = JSON.parse(String((input as Request).init?.body || "{}")).packageId as PlanId;
    return new Response(JSON.stringify(ownerPreview(packageId, packageId === "pro" ? 214_280_000 : 50_000_000, packageId === "pro" ? 25_800_000 : 2_580_000)), { status: 200, headers: { "content-type": "application/json" } });
  }) as typeof fetch;

  try {
    const catalog = await getPricingCatalog();
    const entries = await Promise.all(catalog.packages.filter((plan) => plan.available).map(async (plan) =>
      [plan.id, workspacePricePreview(await previewPricing({ resourceType: "workspace", packageId: plan.id }, "csrf-1"))] as const
    ));
    const selection = workspaceLaunchPricingSelection({
      catalog,
      planId: "basic",
      previews: Object.fromEntries(entries.filter((entry) => entry[1] !== null))
    });

    assert.deepEqual(requested, [
      "/api/pricing/catalog",
      "/api/pricing/preview",
      "/api/pricing/preview"
    ]);
    assert.equal(selection.selectedPlan?.id, "basic");
    assert.equal(selection.selectedPrice, 52_580_000);
    assert.equal(
      workspaceLaunchBalance({ resourceBillingMode: catalog.resourceBillingMode, customerOwned: false, selectedPrice: selection.selectedPrice, walletUsdMicros: "500000000" }),
      true
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a package the owner does not offer earns no price and is named as unpriced", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const path = String(input);
    if (path === "/api/pricing/catalog") {
      return new Response(JSON.stringify({
        ...pricedCatalog,
        // The Control Plane only offers a package that has a fabric compute pool.
        packages: [{ id: "basic", name: "Basic", available: true }, { id: "pro", name: "Pro", available: false }]
      }), { status: 200, headers: { "content-type": "application/json" } });
    }
    return new Response(JSON.stringify({ error: "package_unavailable" }), { status: 409, headers: { "content-type": "application/json" } });
  }) as typeof fetch;

  try {
    const catalog = await getPricingCatalog();
    const entries = await Promise.all(catalog.packages.filter((plan) => plan.available).map(async (plan) => {
      try {
        return [plan.id, workspacePricePreview(await previewPricing({ resourceType: "workspace", packageId: plan.id }, "csrf-1"))] as const;
      } catch {
        return [plan.id, null] as const;
      }
    }));
    const unpriced = entries.filter((entry) => entry[1] === null).map(([planId]) => planId);

    assert.deepEqual(unpriced, ["basic"]);
    assert.deepEqual(workspaceLaunchUnpricedPackages("unpriced_packages:basic"), ["basic"]);
    assert.deepEqual(workspaceLaunchUnpricedPackages("pricing_catalog_unavailable"), []);
    assert.equal(catalog.packages.find((plan) => plan.id === "pro")?.available, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("no provisioning writer appears anywhere under the Console UI", () => {
  // The Control Plane launch command is retired, so the Console states no
  // provisioning shape at all: a typed wire field is a declaration, not a
  // writer, and a provisioning shape typed as a literal is a writer.
  const writers = consoleUiSources().flatMap(({ path, text }) =>
    [...text.matchAll(/provisioningMode\s*[:=]\s*["'`]/g)].map((match) => `${path}: ${match[0]}`)
  );

  assert.deepEqual(writers, [], "the retired launch command leaves no provisioning writer behind");
});

test("the pricing read reaches the selection over the real routes", () => {
  // A component preview carries no total, so the wrong resource type would come
  // back unpriceable instead of failing loudly.
  const previewCallSites = consoleUiSources().flatMap(({ path, text }) =>
    callArguments(text, "previewPricing(").map((argumentsText) => `${path}: ${argumentsText}`)
  );
  assert.equal(previewCallSites.length, 1, `previewPricing call sites: ${previewCallSites.join(" | ")}`);
  assert.match(previewCallSites[0], /workspaceLaunchPreviewRequest\(/);

  assert.deepEqual(workspaceLaunchPreviewRequest("pro"), { resourceType: "workspace", packageId: "pro" });
});
