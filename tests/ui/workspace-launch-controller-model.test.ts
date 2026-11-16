import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
import type {
  WorkspaceLaunchRequest,
  WorkspaceLaunchResponse
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
  workspaceLaunchProvisioningMode,
  workspaceLaunchReviewBlocked,
  workspaceLaunchReviewReadiness,
  workspaceLaunchSubmission,
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

test("the submission carries the customer's own renewal choice", () => {
  // Whether resources are billed is the owner's deployment fact. The owner
  // refuses an autoRenew it cannot honour, so the client states the customer's
  // choice instead of deciding a billing mode it can no longer read.
  const input: WorkspaceLaunchIntentInput = { name: "Alpha", packageId: "basic", autoRenew: true };
  assert.deepEqual(workspaceLaunchSubmission(input), {
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

// The Control Plane withdrew its package-pricing catalog and preview in the
// R6 slice, so a review asks for a name and a package and nothing else.
test("a review needs a name and a package, and no price", () => {
  const ready: WorkspaceLaunchReviewState = {
    launchRecoveryState: "clear",
    launchName: "Alpha",
    selectedPlanId: "basic"
  };

  assert.equal(canReviewWorkspaceLaunch(workspaceLaunchReviewReadiness(ready)), true);
  assert.equal(workspaceLaunchReviewBlocked(ready), false);
  assert.equal(workspaceLaunchReviewBlocked({ ...ready, launchName: "   " }), true);
  assert.equal(workspaceLaunchReviewBlocked({ ...ready, selectedPlanId: "" }), true);
  assert.equal(workspaceLaunchReviewBlocked({ ...ready, launchRecoveryState: "checking" }), true);
  assert.equal(workspaceLaunchReviewBlocked({ ...ready, launchRecoveryState: "conflict" }), true);
});

test("the confirm button and the model reach the same verdict on one readiness state", () => {
  const ready = {
    launchRecoveryState: "clear",
    launchName: "Alpha",
    selectedPlanId: "basic"
  } as const;
  const cases: Array<{ label: string; state: WorkspaceLaunchReviewState }> = [
    { label: "ready", state: ready },
    { label: "no name", state: { ...ready, launchName: "   " } },
    { label: "no plan", state: { ...ready, selectedPlanId: "" } },
    { label: "recovery checking", state: { ...ready, launchRecoveryState: "checking" } },
    { label: "recovery conflict", state: { ...ready, launchRecoveryState: "conflict" } }
  ];

  for (const { label, state } of cases) {
    // Built independently of the production mapping so a divergence in either
    // one is a failure rather than a shared blind spot.
    const independent: Parameters<typeof canReviewWorkspaceLaunch>[0] = {
      recoveryState: state.launchRecoveryState,
      hasName: state.launchName.trim().length > 0,
      hasSelectedPlan: state.selectedPlanId !== ""
    };
    assert.equal(
      workspaceLaunchReviewBlocked(state),
      !canReviewWorkspaceLaunch(independent),
      `the page and the model disagree about "${label}"`
    );
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
    { name: "Alpha", packageId: "basic", autoRenew: true }
  );
  assert.equal(submitted.provisioningMode, "full");
  assert.equal(submitted.autoRenew, true, "the customer's own renewal choice is what the owner receives");
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

// The retired pricing routes must not come back through another call site.
test("no Console source reaches the retired package pricing routes", () => {
  const retired = ["/api/pricing/catalog", "/api/pricing/preview", "getPricingCatalog(", "previewPricing("];
  const readers = consoleUiSources().flatMap(({ path, text }) =>
    retired.filter((needle) => text.includes(needle)).map((needle) => `${path}: ${needle}`)
  );

  assert.deepEqual(readers, [], "the retired package pricing routes must not be read again");
});
