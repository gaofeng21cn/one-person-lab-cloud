import assert from "node:assert/strict";
import test from "node:test";

import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
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
  workspaceLaunchPricingSelection,
  workspaceLaunchReviewBlocked,
  workspaceLaunchReviewReadiness,
  workspaceLaunchSubmission,
  workspacePricePreview,
  type WorkspaceLaunchIntent,
  type WorkspaceLaunchReviewState
} from "../../apps/console-ui/src/app/workspace-launch-controller-model.ts";

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
  const original = { ...basicRequest };
  assert.deepEqual(workspaceLaunchSubmission(basicRequest, "none"), {
    ...basicRequest,
    autoRenew: false
  });
  assert.deepEqual(workspaceLaunchSubmission(basicRequest, "enabled"), basicRequest);
  assert.deepEqual(basicRequest, original);
});

test("a customer Workspace Launch always opens resources only", () => {
  const submitted = workspaceLaunchSubmission({ ...basicRequest, provisioningMode: "full" }, "enabled");
  assert.equal(submitted.provisioningMode, "resource_only");
  assert.deepEqual(workspaceLaunchSubmission(basicRequest, "enabled"), basicRequest);
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
