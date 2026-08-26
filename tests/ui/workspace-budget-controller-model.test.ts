import assert from "node:assert/strict";
import test from "node:test";

import type { WorkspaceGatewayBudgetDTO, WorkspaceGatewayBudgetUpdateRequest } from "../../apps/console-ui/src/api/dtos.ts";
import {
  resolveWorkspaceBudgetIntent,
  sameWorkspaceBudgetInput,
  shouldRetainWorkspaceBudgetIntent,
  workspaceBudgetIdentityMatches,
  workspaceBudgetResultMatchesInput,
  type WorkspaceBudgetIntent
} from "../../apps/console-ui/src/app/workspace-budget-controller-model.ts";

const budget: WorkspaceGatewayBudgetDTO = {
  workspaceId: "workspace-alpha",
  keyId: "9223372036854775807",
  status: "active",
  quotaUsdMicros: "9000000",
  quotaUsedUsdMicros: "0",
  rateLimit5hUsdMicros: "500000",
  rateLimit1dUsdMicros: "1000000",
  rateLimit7dUsdMicros: "4000000",
  usage5hUsdMicros: "0",
  usage1dUsdMicros: "0",
  usage7dUsdMicros: "0",
  enabled: true,
  updatedAt: "2026-08-26T00:00:00.000Z"
};

const input: WorkspaceGatewayBudgetUpdateRequest = {
  quotaUsdMicros: 9000000,
  rateLimit5hUsdMicros: 500000,
  rateLimit1dUsdMicros: 1000000,
  rateLimit7dUsdMicros: 4000000,
  enabled: true
};

test("same Workspace budget input reuses the idempotency intent", () => {
  const current: WorkspaceBudgetIntent = {
    workspaceId: "workspace-alpha",
    keyId: budget.keyId,
    input,
    signature: JSON.stringify([9000000, 500000, 1000000, 4000000, true, null, null]),
    idempotencyKey: "workspace-gateway-budget:existing"
  };
  let created = 0;

  const result = resolveWorkspaceBudgetIntent(current, "workspace-alpha", budget.keyId, { ...input }, () => {
    created += 1;
    return "workspace-gateway-budget:new";
  });

  assert.equal(result, current);
  assert.equal(created, 0);
});

test("Workspace or key changes create a new intent", () => {
  const current: WorkspaceBudgetIntent = {
    workspaceId: "workspace-alpha",
    keyId: budget.keyId,
    input,
    signature: JSON.stringify([9000000, 500000, 1000000, 4000000, true, null, null]),
    idempotencyKey: "workspace-gateway-budget:existing"
  };
  const result = resolveWorkspaceBudgetIntent(current, "workspace-beta", "19", input, () => "workspace-gateway-budget:new");

  assert.deepEqual(result, {
    workspaceId: "workspace-beta",
    keyId: "19",
    input,
    signature: JSON.stringify([9000000, 500000, 1000000, 4000000, true, null, null]),
    idempotencyKey: "workspace-gateway-budget:new"
  });
});

test("budget result must match all stable requested policy fields", () => {
  assert.equal(workspaceBudgetIdentityMatches(budget, "workspace-alpha", budget.keyId), true);
  assert.equal(workspaceBudgetResultMatchesInput(budget, input), true);
  assert.equal(workspaceBudgetResultMatchesInput({ ...budget, enabled: false }, input), false);
  assert.equal(workspaceBudgetResultMatchesInput({ ...budget, quotaUsedUsdMicros: "4" }, { resetQuota: true }), true);
  assert.equal(workspaceBudgetResultMatchesInput({ ...budget, usage1dUsdMicros: "2" }, { resetRateLimitUsage: true }), true);
});

test("budget input comparison ignores object property order", () => {
  assert.equal(sameWorkspaceBudgetInput(input, {
    enabled: true,
    rateLimit7dUsdMicros: 4000000,
    rateLimit1dUsdMicros: 1000000,
    quotaUsdMicros: 9000000,
    rateLimit5hUsdMicros: 500000
  }), true);
  assert.equal(sameWorkspaceBudgetInput(input, { ...input, enabled: false }), false);
});

test("response loss and server failures retain budget intent", () => {
  assert.equal(shouldRetainWorkspaceBudgetIntent(new Error("response_lost")), true);
  assert.equal(shouldRetainWorkspaceBudgetIntent(Object.assign(new Error("upstream"), { status: 503 })), true);
  assert.equal(shouldRetainWorkspaceBudgetIntent(Object.assign(new Error("invalid"), { status: 422 })), false);
});
