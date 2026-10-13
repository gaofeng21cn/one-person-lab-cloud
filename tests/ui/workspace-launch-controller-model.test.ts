import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
import { getPricingCatalog, previewPricing } from "../../apps/console-ui/src/api/console-read-api.ts";
import type {
  PlanId,
  PricingCatalogResponse,
  PricingPreviewResponse,
  WorkspaceLaunchRequest,
  WorkspaceLaunchResponse,
  WorkspacePricePreview
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  canReviewWorkspaceLaunch,
  canCreateAgentQuote,
  canCreateAgentWorkspace,
  operationPollDelayMs,
  canSubmitWorkspaceLaunch,
  classifyWorkspaceLaunchRecovery,
  resolveWorkspaceLaunchIntent,
  shouldPollWorkspaceLaunch,
  shouldRetainWorkspaceLaunchIntent,
  workspaceLaunchBalance,
  workspaceLaunchPreviewRequest,
  workspaceLaunchPricingSelection,
  workspaceLaunchProvisioningMode,
  workspaceLaunchReviewBlocked,
  workspaceLaunchReviewReadiness,
  workspaceLaunchSubmission,
  workspaceLaunchUnpricedPackages,
  workspacePricePreview,
  type WorkspaceLaunchIntent,
  type WorkspaceLaunchIntentInput,
  type WorkspaceLaunchReviewState
} from "../../apps/console-ui/src/app/workspace-launch-controller-model.ts";

const repositoryRoot = fileURLToPath(new URL("../..", import.meta.url)).replace(/\/$/, "");
const consoleUiRoot = join(repositoryRoot, "apps/console-ui/src");

function sourceText(relativePath: string): string {
  return readFileSync(join(repositoryRoot, relativePath), "utf8");
}

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

const basicRequest: WorkspaceLaunchRequest = {
  name: "Alpha",
  packageId: "basic",
  autoRenew: true,
  provisioningMode: "resource_only"
};

function launchResponse(
  status: string,
  operationId = `launch-${status}`
): WorkspaceLaunchResponse {
  return {
    operationId,
    status,
    phase: "compute",
    accountId: "acct-alpha",
    name: "Alpha",
    packageId: "basic",
    sizeGb: 10,
    autoRenew: true,
    priceVersion: "pilot-usd-2026-07-v1",
    currency: "USD",
    totalChargeUsdMicros: 52_580_000
  };
}

function apiError(payload: { status: "unknown"; retryable: true } | { error: string }): ApiError {
  const error: ApiError = new Error("workspace_launch_failed");
  error.payload = payload;
  return error;
}

test("identical Workspace launch input reuses the existing intent key", () => {
  let keysCreated = 0;
  const created = resolveWorkspaceLaunchIntent(null, basicRequest, () => {
    keysCreated += 1;
    return "workspace-launch:existing";
  });

  assert.equal(created.kind, "ready");
  if (created.kind !== "ready") return;
  const intent: WorkspaceLaunchIntent = created.intent;

  const result = resolveWorkspaceLaunchIntent(intent, { ...basicRequest }, () => {
    keysCreated += 1;
    return "workspace-launch:new";
  });

  assert.equal(result.kind, "ready");
  if (result.kind !== "ready") return;
  assert.equal(result.intent, intent);
  assert.equal(result.intent.idempotencyKey, "workspace-launch:existing");
  assert.equal(keysCreated, 1);
});

test("different Workspace launch input fields conflict without creating a key", () => {
  const intent: WorkspaceLaunchIntent = {
    input: basicRequest,
    idempotencyKey: "workspace-launch:existing"
  };
  let keysCreated = 0;

  const conflicts: WorkspaceLaunchRequest[] = [
    { ...basicRequest, name: "Beta" },
    { ...basicRequest, packageId: "pro" },
    { ...basicRequest, autoRenew: false }
  ];
  for (const input of conflicts) {
    const result = resolveWorkspaceLaunchIntent(intent, input, () => {
      keysCreated += 1;
      return "workspace-launch:new";
    });
    assert.equal(result.kind, "conflict");
  }
  assert.equal(keysCreated, 0);
});

test("Workspace launch intent survives unknown outcome and is released for known API errors", () => {
  assert.equal(shouldRetainWorkspaceLaunchIntent(apiError({ status: "unknown", retryable: true })), true);
  assert.equal(shouldRetainWorkspaceLaunchIntent(apiError({ error: "workspace_purchase_not_enabled" })), false);
});

test("Workspace launch recovery distinguishes zero, one, and multiple non-terminal operations", () => {
  const succeeded = launchResponse("succeeded");
  const preparing = launchResponse("preparing", "launch-preparing");
  const manualReview = launchResponse("manual_review", "launch-review");

  assert.deepEqual(classifyWorkspaceLaunchRecovery([succeeded]), { kind: "none" });

  const resumable = classifyWorkspaceLaunchRecovery([succeeded, preparing]);
  assert.equal(resumable.kind, "resume");
  if (resumable.kind === "resume") assert.equal(resumable.operation, preparing);

  assert.deepEqual(classifyWorkspaceLaunchRecovery([preparing, manualReview]), { kind: "conflict" });
});

test("Workspace launch review and submit require an unambiguous clear recovery", () => {
  const reviewReady = {
    recoveryState: "clear",
    hasName: true,
    hasSelectedPlan: true,
    selectedPriceKnown: true,
    balanceSufficient: true
  } as const;
  const submitReady = {
    ...reviewReady,
    sessionAvailable: true,
    busy: false,
    step: "confirm",
    confirmed: true
  } as const;

  assert.equal(canReviewWorkspaceLaunch(reviewReady), true);
  assert.equal(canSubmitWorkspaceLaunch(submitReady), true);
  for (const recoveryState of ["idle", "checking", "conflict", "unavailable"] as const) {
    assert.equal(canReviewWorkspaceLaunch({ ...reviewReady, recoveryState }), false, `review allowed during ${recoveryState}`);
    assert.equal(canSubmitWorkspaceLaunch({ ...submitReady, recoveryState }), false, `submit allowed during ${recoveryState}`);
  }
});

test("Workspace launch polling stops for manual review and terminal operations", () => {
  assert.equal(shouldPollWorkspaceLaunch(launchResponse("preparing")), true);
  assert.equal(shouldPollWorkspaceLaunch(launchResponse("manual_review")), false);
  assert.equal(shouldPollWorkspaceLaunch(launchResponse("succeeded")), false);
  assert.equal(shouldPollWorkspaceLaunch(launchResponse("failed")), false);
  assert.equal(shouldPollWorkspaceLaunch(launchResponse("refunded")), false);
});

test("resource billing mode none disables Workspace auto-renew", () => {
  const input: WorkspaceLaunchIntentInput = { name: "Alpha", packageId: "basic", autoRenew: true };
  assert.deepEqual(workspaceLaunchSubmission(input, "none"), {
    ...input,
    provisioningMode: workspaceLaunchProvisioningMode,
    autoRenew: false
  });
  assert.deepEqual(workspaceLaunchSubmission(input, "enabled"), {
    ...input,
    provisioningMode: workspaceLaunchProvisioningMode
  });
  assert.deepEqual(input, { name: "Alpha", packageId: "basic", autoRenew: true });
});

test("a new customer Workspace Launch states the default App product, not the legacy resource-only contract", () => {
  // 00_master_index.md D01 and 04_frontend_interaction_spec.md:15: a Launch that
  // selects neither a Package nor a separate WebUI deploys the approved OPL App
  // Runtime Release. 09_legacy_migration.md:9 keeps resource_only as the retained
  // historical contract for pre-existing purchases, so this client must not send
  // it for a new Launch.
  const input: WorkspaceLaunchIntentInput = { name: "Alpha", packageId: "basic", autoRenew: true };
  assert.equal(workspaceLaunchProvisioningMode, "full");
  assert.deepEqual(workspaceLaunchSubmission(input, "enabled"), {
    name: "Alpha", packageId: "basic", autoRenew: true, provisioningMode: "full"
  });
});

test("an intent that changes provisioning mode is a conflict, not a silent reuse", () => {
  const intent: WorkspaceLaunchIntent = {
    input: { ...basicRequest },
    idempotencyKey: "launch-key"
  };
  const resolved = resolveWorkspaceLaunchIntent(intent, { ...basicRequest, provisioningMode: "full" }, () => "new-key");
  assert.equal(resolved.kind, "conflict");
});

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

test("a real catalog readback makes the Workspace launch confirm step reachable", () => {
  const priced = {
    basic: workspacePricePreview(ownerPreview("basic", 50_000_000, 2_580_000))
  } as Partial<Record<PlanId, WorkspacePricePreview>>;
  const selection = workspaceLaunchPricingSelection({ catalog: pricedCatalog, planId: "basic", previews: priced });
  const state: WorkspaceLaunchReviewState = {
    launchRecoveryState: "clear",
    launchName: "Alpha",
    selectedPlan: selection.selectedPlan,
    selectedPrice: selection.selectedPrice,
    balanceSufficient: true
  };

  assert.equal(canReviewWorkspaceLaunch(workspaceLaunchReviewReadiness(state)), true);
  assert.equal(workspaceLaunchReviewBlocked(state), false);

  const unreachable = workspaceLaunchPricingSelection({ catalog: null, planId: "basic", previews: {} });
  assert.equal(
    canReviewWorkspaceLaunch(workspaceLaunchReviewReadiness({
      launchRecoveryState: "clear",
      launchName: "Alpha",
      selectedPlan: unreachable.selectedPlan,
      selectedPrice: unreachable.selectedPrice,
      balanceSufficient: true
    })),
    false,
    "an empty catalog must not let the customer confirm a purchase"
  );
});

test("the confirm button and the model reach the same verdict on one readiness state", () => {
  const plan = pricedCatalog.packages[0];
  const ready = {
    launchRecoveryState: "clear",
    launchName: "Alpha",
    selectedPlan: plan,
    selectedPrice: 52_580_000,
    balanceSufficient: true
  } as const;
  const cases: Array<{ label: string; state: WorkspaceLaunchReviewState }> = [
    { label: "ready", state: ready },
    { label: "no name", state: { ...ready, launchName: "   " } },
    { label: "no plan", state: { ...ready, selectedPlan: null } },
    { label: "unknown price", state: { ...ready, selectedPrice: null } },
    { label: "balance unknown", state: { ...ready, balanceSufficient: null } },
    { label: "balance short", state: { ...ready, balanceSufficient: false } },
    { label: "recovery checking", state: { ...ready, launchRecoveryState: "checking" } },
    { label: "recovery conflict", state: { ...ready, launchRecoveryState: "conflict" } }
  ];

  for (const { label, state } of cases) {
    // Built independently of the production mapping so a divergence in either
    // one is a failure rather than a shared blind spot.
    const independent: Parameters<typeof canReviewWorkspaceLaunch>[0] = {
      recoveryState: state.launchRecoveryState,
      hasName: state.launchName.trim().length > 0,
      hasSelectedPlan: state.selectedPlan !== null,
      selectedPriceKnown: state.selectedPrice !== null,
      balanceSufficient: state.balanceSufficient === true
    };
    assert.equal(
      workspaceLaunchReviewBlocked(state),
      !canReviewWorkspaceLaunch(independent),
      `the page and the model disagree about "${label}"`
    );
  }
});

test("a zero total is the owner's own price, not a missing one", () => {
  // Resource billing off: the owner zeroes every component and returns 0.
  const unbilled = { ...ownerPreview("basic", 0, 0), totalChargeUsdMicros: 0 };
  const preview = workspacePricePreview(unbilled);
  assert.ok(preview);
  assert.equal(preview.totalChargeUsdMicros, 0);

  const selection = workspaceLaunchPricingSelection({ catalog: pricedCatalog, planId: "basic", previews: { basic: preview } });
  assert.equal(selection.selectedPrice, 0);
  assert.equal(canReviewWorkspaceLaunch(workspaceLaunchReviewReadiness({
    launchRecoveryState: "clear",
    launchName: "Alpha",
    selectedPlan: selection.selectedPlan,
    selectedPrice: selection.selectedPrice,
    balanceSufficient: workspaceLaunchBalance({ resourceBillingMode: "none", customerOwned: false, selectedPrice: 0, walletUsdMicros: null })
  })), true, "an unbilled purchase is affordable without a balance read");
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
    assert.equal(workspaceLaunchReviewBlocked({
      launchRecoveryState: "clear",
      launchName: "Alpha",
      selectedPlan: selection.selectedPlan,
      selectedPrice: selection.selectedPrice,
      balanceSufficient: workspaceLaunchBalance({ resourceBillingMode: catalog.resourceBillingMode, customerOwned: false, selectedPrice: selection.selectedPrice, walletUsdMicros: "500000000" })
    }), false);
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

test("the launch controller states the provisioning shape only through the single writer", () => {
  const controllerSource = readFileSync(
    new URL("../../apps/console-ui/src/app/use-workspace-launch-controller.ts", import.meta.url),
    "utf8"
  );

  assert.equal(controllerSource.includes("workspaceLaunchSubmission("), true, "the submit call site must use the single writer");
  assert.equal(
    /provisioningMode\s*:/.test(controllerSource),
    false,
    "the controller must not state a provisioning shape of its own"
  );

  const submitted: WorkspaceLaunchRequest = workspaceLaunchSubmission(
    { name: "Alpha", packageId: "basic", autoRenew: true },
    "enabled"
  );
  assert.equal(submitted.provisioningMode, "full");
});

test("no second provisioning writer appears anywhere under the Console UI", () => {
  const modelPath = "apps/console-ui/src/app/workspace-launch-controller-model.ts";
  // A typed wire field is a declaration, not a writer. A provisioning shape typed
  // as a literal is a writer, wherever it sits.
  const writers = consoleUiSources().flatMap(({ path, text }) =>
    path === modelPath ? [] : [...text.matchAll(/provisioningMode\s*[:=]\s*["'`]/g)].map((match) => `${path}: ${match[0]}`)
  );

  assert.deepEqual(writers, [], "only workspace-launch-controller-model.ts may state a provisioning shape");
});

test("the launch call sites pass the owner's product shape, not a hardcoded one", () => {
  const controller = sourceText("apps/console-ui/src/app/use-workspace-launch-controller.ts");

  // The billing mode decides auto-renew, so the call site has to carry the
  // owner's value instead of a literal that silently drops it.
  const submitArguments = callArguments(controller, "workspaceLaunchSubmission(");
  assert.equal(submitArguments.length, 1, "exactly one submission call site");
  assert.match(submitArguments[0], /resourceBillingMode/, "the submission call site must read the owner's billing mode");
  assert.equal(
    /"none"/.test(submitArguments[0]),
    false,
    "the submission call site must not hardcode the unbilled mode"
  );

  // A component preview carries no total, so the wrong resource type would come
  // back unpriceable instead of failing loudly.
  const previewCallSites = consoleUiSources().flatMap(({ path, text }) =>
    callArguments(text, "previewPricing(").map((argumentsText) => `${path}: ${argumentsText}`)
  );
  assert.equal(previewCallSites.length, 1, `previewPricing call sites: ${previewCallSites.join(" | ")}`);
  assert.match(previewCallSites[0], /workspaceLaunchPreviewRequest\(/);

  assert.deepEqual(workspaceLaunchPreviewRequest("pro"), { resourceType: "workspace", packageId: "pro" });
});
