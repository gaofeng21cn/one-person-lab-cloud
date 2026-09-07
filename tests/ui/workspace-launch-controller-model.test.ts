import assert from "node:assert/strict";
import test from "node:test";

import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
import type { WorkspaceLaunchRequest, WorkspaceLaunchResponse } from "../../apps/console-ui/src/api/dtos.ts";
import {
  canReviewWorkspaceLaunch,
  canSubmitWorkspaceLaunch,
  classifyWorkspaceLaunchRecovery,
  resolveWorkspaceLaunchIntent,
  shouldPollWorkspaceLaunch,
  shouldRetainWorkspaceLaunchIntent,
  workspaceLaunchSubmission,
  type WorkspaceLaunchIntent
} from "../../apps/console-ui/src/app/workspace-launch-controller-model.ts";

const basicRequest: WorkspaceLaunchRequest = {
  name: "Alpha",
  packageId: "basic",
  autoRenew: true
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
