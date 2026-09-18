import assert from "node:assert/strict";
import test from "node:test";

import type { ApiError } from "../../apps/console-ui/src/api/console-api.ts";
import type { SourceEnvelope, WorkspaceDTO } from "../../apps/console-ui/src/api/dtos.ts";
import {
  formatWorkspaceDeleteTime,
  isWorkspaceDeleteNotFound,
  presentWorkspaceDelete,
  presentWorkspaceDeleteReason,
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

test("deletion progress renders only the platform stage, state, reason and refund it published", () => {
  const stages = ["runtime_absent", "attachment_absent", "storage_absent", "compute_absent", "workspace_absent", "receipt_recorded"] as const;
  for (const stage of stages) {
    const presentation = presentWorkspaceDelete({
      workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "pending", stage, phase: "storage_absent"
    });
    assert.ok(presentation, `stage ${stage} must render`);
    assert.notEqual(presentation.stageLabel, "", `stage ${stage} must have a label`);
  }
});

test("a blocked deletion is shown as paused with its stable reason, never as deleted", () => {
  const presentation = presentWorkspaceDelete({
    workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "manual_review",
    stage: "storage_absent", phase: "storage_absent", pageState: "blocked", reasonCode: "fabric_storage_identity_conflict"
  });
  assert.ok(presentation);
  assert.equal(presentation.tone, "danger");
  assert.match(presentation.detail, /身份/);
  assert.doesNotMatch(presentation.title, /已删除/);
});

test("a retrying deletion reports the automatic retry and keeps the refund separate", () => {
  const presentation = presentWorkspaceDelete({
    workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "pending",
    stage: "compute_absent", phase: "storage_absent", pageState: "retrying",
    nextRetryAt: "2026-08-26T00:00:30.000Z", refundStatus: "blocked"
  });
  assert.ok(presentation);
  assert.match(presentation.detail, /自动重试/);
  assert.equal(presentation.refundLabel, "退款等待资源删除确认");
  assert.doesNotMatch(presentation.stageLabel, /退款/);
});

test("a completed deletion and its refund status are distinct statements", () => {
  const presentation = presentWorkspaceDelete({
    workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "deleted",
    stage: "receipt_recorded", phase: "complete", pageState: "completed", refundStatus: "succeeded"
  });
  assert.ok(presentation);
  assert.equal(presentation.tone, "good");
  assert.equal(presentation.refundLabel, "退款已完成");
});

test("an unknown reason code degrades to a neutral statement instead of leaking the code", () => {
  assert.equal(presentWorkspaceDeleteReason(""), "");
  assert.equal(presentWorkspaceDeleteReason(undefined), "");
  assert.equal(presentWorkspaceDeleteReason("some_future_code"), "删除遇到未确认的结果");
  const presentation = presentWorkspaceDelete({
    workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "manual_review",
    stage: "runtime_absent", phase: "claimed", pageState: "blocked", reasonCode: "some_future_code"
  });
  assert.ok(presentation);
  assert.doesNotMatch(presentation.detail, /some_future_code/);
});

test("a missing deletion operation renders nothing so the page stays in its read state", () => {
  assert.equal(presentWorkspaceDelete(null), null);
});

test("a response without a page state still renders a truthful platform state", () => {
  for (const [status, expected] of [["deleted", "工作空间已删除"], ["manual_review", "删除已暂停，需要核对"], ["pending", "正在删除工作空间"]] as const) {
    const presentation = presentWorkspaceDelete({ workspaceId: workspace.id, operationId: "workspace-delete-alpha", status, phase: "claimed" });
    assert.ok(presentation, `${status} must render`);
    assert.equal(presentation.title, expected);
  }
});

test("the last readback time is rendered from the persisted evidence", () => {
  const presentation = presentWorkspaceDelete({
    workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "pending",
    stage: "compute_absent", phase: "storage_absent", pageState: "retrying",
    lastReadbackAt: "2026-09-18T05:00:00.000Z"
  });
  assert.ok(presentation);
  assert.notEqual(presentation.lastReadbackLabel, "");
  assert.match(presentation.lastReadbackLabel, /^2026-09-18 05:00:00$/);

  // A response without a readback time renders nothing rather than inventing one.
  const withoutReadback = presentWorkspaceDelete({
    workspaceId: workspace.id, operationId: "workspace-delete-alpha", status: "pending",
    stage: "compute_absent", phase: "storage_absent", pageState: "retrying"
  });
  assert.ok(withoutReadback);
  assert.equal(withoutReadback.lastReadbackLabel, "");
  assert.equal(formatWorkspaceDeleteTime(undefined), "");
  assert.equal(formatWorkspaceDeleteTime(""), "");
  // An unparseable value is passed through so the reader sees the raw fact.
  assert.equal(formatWorkspaceDeleteTime("not-a-time"), "not-a-time");
});
