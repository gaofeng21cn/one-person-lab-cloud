import assert from "node:assert/strict";
import test from "node:test";

import type { SourceEnvelope, WorkspaceDTO, WorkspaceRenewalResponse } from "../../apps/console-ui/src/api/dtos.ts";
import {
  resolveWorkspaceRenewalIntent,
  shouldRetainWorkspaceRenewalIntent,
  workspaceRenewalReadbackMatches,
  workspaceRenewalResponseMatches,
  type WorkspaceRenewalIntent
} from "../../apps/console-ui/src/app/workspace-renewal-controller-model.ts";

const response: WorkspaceRenewalResponse = {
  autoRenew: true,
  effectiveAfter: "2026-08-26T00:00:00Z",
  nextRenewalAt: "2026-09-26T00:00:00Z",
  paidThrough: "2026-09-26T00:00:00Z",
  renewalStatus: "active"
};

const workspace: WorkspaceDTO = {
  id: "workspace-alpha",
  ownerAccountId: "acct-alpha",
  ownerUserId: "user-alpha",
  state: "active",
  createdAt: "2026-07-26T00:00:00Z",
  updatedAt: "2026-08-26T00:00:00Z",
  autoRenew: true,
  paidThrough: response.paidThrough,
  nextRenewalAt: response.nextRenewalAt,
  renewalStatus: response.renewalStatus
};

const readback: SourceEnvelope<WorkspaceDTO | null> = {
  source: "control-plane",
  status: "available",
  available: true,
  fetchedAt: "2026-08-26T00:00:01Z",
  data: workspace
};

test("Workspace renewal intent reuses the key for the same Workspace and setting", () => {
  const current: WorkspaceRenewalIntent = {
    workspaceId: "workspace-alpha",
    autoRenew: true,
    idempotencyKey: "workspace-renewal:existing"
  };
  let created = 0;

  const result = resolveWorkspaceRenewalIntent(current, "workspace-alpha", true, () => {
    created += 1;
    return "workspace-renewal:new";
  });

  assert.equal(result, current);
  assert.equal(created, 0);
});

test("Workspace renewal intent changes when the Workspace or setting changes", () => {
  const current: WorkspaceRenewalIntent = {
    workspaceId: "workspace-alpha",
    autoRenew: true,
    idempotencyKey: "workspace-renewal:existing"
  };

  assert.deepEqual(
    resolveWorkspaceRenewalIntent(current, "workspace-alpha", false, () => "workspace-renewal:toggle"),
    { workspaceId: "workspace-alpha", autoRenew: false, idempotencyKey: "workspace-renewal:toggle" }
  );
  assert.deepEqual(
    resolveWorkspaceRenewalIntent(current, "workspace-beta", true, () => "workspace-renewal:workspace"),
    { workspaceId: "workspace-beta", autoRenew: true, idempotencyKey: "workspace-renewal:workspace" }
  );
});

test("Workspace renewal response requires the requested setting and complete dates", () => {
  assert.equal(workspaceRenewalResponseMatches(response, true), true);
  assert.equal(workspaceRenewalResponseMatches({ ...response, autoRenew: false }, true), false);
  assert.equal(workspaceRenewalResponseMatches({ ...response, renewalStatus: " " }, true), false);
  assert.equal(workspaceRenewalResponseMatches({ ...response, paidThrough: "not-a-date" }, true), false);
});

test("Workspace renewal requires an identity-matched authoritative readback", () => {
  assert.equal(workspaceRenewalReadbackMatches(readback, "workspace-alpha", response), true);
  assert.equal(workspaceRenewalReadbackMatches({ ...readback, data: null }, "workspace-alpha", response), false);
  assert.equal(workspaceRenewalReadbackMatches({ ...readback, data: { ...workspace, id: "workspace-beta" } }, "workspace-alpha", response), false);
  assert.equal(workspaceRenewalReadbackMatches({ ...readback, data: { ...workspace, renewalStatus: "scheduled" } }, "workspace-alpha", response), false);
  assert.equal(workspaceRenewalReadbackMatches({ ...readback, data: { ...workspace, paidThrough: "2026-10-26T00:00:00Z" } }, "workspace-alpha", response), false);
});

test("Workspace renewal keeps intent for unknown or server failures", () => {
  assert.equal(shouldRetainWorkspaceRenewalIntent(new Error("network")), true);
  assert.equal(shouldRetainWorkspaceRenewalIntent({ status: "unknown" }), true);
  assert.equal(shouldRetainWorkspaceRenewalIntent({ status: 503 }), true);
  assert.equal(shouldRetainWorkspaceRenewalIntent({ status: 409 }), false);
});
