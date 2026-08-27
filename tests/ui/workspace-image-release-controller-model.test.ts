import assert from "node:assert/strict";
import test from "node:test";

import {
  resolveWorkspaceImageReleaseActivationIntent,
  workspaceImageReleaseActivationIdempotencyKey,
  workspaceImageReleaseActivationMatches
} from "../../apps/console-ui/src/app/workspace-image-release-controller-model.ts";

test("Workspace image release activation creates a valid bounded mutation key", () => {
  const key = workspaceImageReleaseActivationIdempotencyKey(() => "01234567-89ab-cdef-0123-456789abcdef");
  assert.equal(key, "wira-01234567-89ab-cdef-0123-456789abcdef");
  assert.ok(key.length <= 48);
  assert.match(key, /^[a-z0-9]+(?:-[a-z0-9]+)*$/);
});

test("Workspace image release activation reuses only the exact CAS intent", () => {
  const first = resolveWorkspaceImageReleaseActivationIntent(null, "26.8.4", 4, "rollback", () => "key-1");
  const replay = resolveWorkspaceImageReleaseActivationIntent(first, "26.8.4", 4, "rollback", () => "key-2");
  const changed = resolveWorkspaceImageReleaseActivationIntent(first, "26.8.26", 4, "promote", () => "key-3");

  assert.equal(replay, first);
  assert.equal(changed.idempotencyKey, "key-3");
  assert.equal(changed.releaseVersion, "26.8.26");
});

test("Workspace image release activation accepts only the selected version and CAS revision", () => {
  const intent = resolveWorkspaceImageReleaseActivationIntent(null, "26.8.4", 4, "rollback", () => "key-1");
  assert.equal(workspaceImageReleaseActivationMatches({ revision: 5, active: { version: "26.8.4" } }, intent), true);
  assert.equal(workspaceImageReleaseActivationMatches({ revision: 6, active: { version: "26.8.4" } }, intent), false);
  assert.equal(workspaceImageReleaseActivationMatches({ revision: 5, active: { version: "26.8.26" } }, intent), false);
});
