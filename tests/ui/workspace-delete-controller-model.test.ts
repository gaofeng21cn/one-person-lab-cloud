import assert from "node:assert/strict";
import test from "node:test";

import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
import type { SourceEnvelope, WorkspaceDTO } from "../../apps/console-ui/src/api/dtos.ts";
import {
  isWorkspaceDeleteNotFound,
  resolveWorkspaceDeleteIntent,
  shouldRetainWorkspaceDeleteIntent,
  workspaceDeleteReadbackConfirmed,
  type WorkspaceDeleteErrorPayload,
  type WorkspaceDeleteIntent
} from "../../apps/console-ui/src/app/workspace-delete-controller-model.ts";

const workspace: WorkspaceDTO = {
  id: "workspace-alpha",
  ownerAccountId: "account-alpha",
  ownerUserId: "user-alpha",
  state: "active",
  createdAt: "2026-08-26T00:00:00.000Z",
  updatedAt: "2026-08-26T00:00:00.000Z",
  name: "Alpha Workspace"
};

function availableReadback(data: WorkspaceDTO | null): SourceEnvelope<WorkspaceDTO | null> {
  return {
    source: "control-plane",
    status: data ? "available" : "empty",
    available: true,
    fetchedAt: "2026-08-26T00:00:01.000Z",
    data
  };
}

function apiError(status?: number, payload?: WorkspaceDeleteErrorPayload): ApiError {
  const error: ApiError = new Error("workspace_delete_failed");
  if (status !== undefined) error.status = status;
  if (payload !== undefined) error.payload = payload;
  return error;
}

test("same Workspace reuses its delete idempotency intent", () => {
  const current: WorkspaceDeleteIntent = {
    workspaceId: "workspace-alpha",
    idempotencyKey: "workspace-delete:workspace-alpha:existing"
  };
  let keysCreated = 0;

  const result = resolveWorkspaceDeleteIntent(current, "workspace-alpha", () => {
    keysCreated += 1;
    return "workspace-delete:workspace-alpha:new";
  });

  assert.equal(result, current);
  assert.equal(keysCreated, 0);
});

test("a different Workspace receives a new delete idempotency intent", () => {
  const current: WorkspaceDeleteIntent = {
    workspaceId: "workspace-alpha",
    idempotencyKey: "workspace-delete:workspace-alpha:existing"
  };
  let keysCreated = 0;

  const result = resolveWorkspaceDeleteIntent(current, "workspace-beta", () => {
    keysCreated += 1;
    return "workspace-delete:workspace-beta:new";
  });

  assert.deepEqual(result, {
    workspaceId: "workspace-beta",
    idempotencyKey: "workspace-delete:workspace-beta:new"
  });
  assert.equal(keysCreated, 1);
});

test("delete succeeds only when the authoritative list confirms absence", () => {
  assert.equal(workspaceDeleteReadbackConfirmed(availableReadback(null)), true);
  assert.equal(workspaceDeleteReadbackConfirmed(availableReadback(workspace)), false);

  const unavailable: SourceEnvelope<WorkspaceDTO | null> = {
    source: "control-plane",
    status: "unavailable",
    available: false,
    fetchedAt: "2026-08-26T00:00:01.000Z",
    reasonCode: "control_plane_unavailable"
  };
  assert.equal(workspaceDeleteReadbackConfirmed(unavailable), false);
});

test("response loss and 404 retain a delete intent, while known client errors release it", () => {
  assert.equal(shouldRetainWorkspaceDeleteIntent(apiError()), true);
  const statuslessError: ApiError = Object.assign(new Error("response_lost"), { status: undefined });
  assert.equal(shouldRetainWorkspaceDeleteIntent(statuslessError), true);
  assert.equal(shouldRetainWorkspaceDeleteIntent(apiError(404, { error: "workspace_not_found" })), true);
  assert.equal(shouldRetainWorkspaceDeleteIntent(apiError(404, { error: "forbidden" })), true);
  assert.equal(shouldRetainWorkspaceDeleteIntent(apiError(503, { error: "upstream_unavailable" })), true);
  assert.equal(shouldRetainWorkspaceDeleteIntent(apiError(409, { error: "delete_conflict" })), false);
  assert.equal(shouldRetainWorkspaceDeleteIntent(apiError(422, { error: "invalid_workspace" })), false);
});

test("owner not-found errors are identified for authoritative absence readback", () => {
  assert.equal(isWorkspaceDeleteNotFound(apiError(404, { error: "workspace_not_found" })), true);
  assert.equal(isWorkspaceDeleteNotFound(apiError(404, { error: "forbidden" })), false);
  assert.equal(isWorkspaceDeleteNotFound(apiError()), false);
});
