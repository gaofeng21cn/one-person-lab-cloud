import assert from "node:assert/strict";
import test from "node:test";

import {
  isTerminalWorkspaceRuntimeImageReplacement,
  resolveWorkspaceRuntimeImageReplacementIntent,
  workspaceRuntimeImageReplacementIdempotencyKey,
  workspaceRuntimeImageReplacementReadbackMatches
} from "../../apps/console-ui/src/app/workspace-runtime-image-replacement-controller-model.ts";

test("Workspace image replacement creates a valid bounded mutation key", () => {
  const key = workspaceRuntimeImageReplacementIdempotencyKey(() => "01234567-89ab-cdef-0123-456789abcdef");
  assert.equal(key, "wri-01234567-89ab-cdef-0123-456789abcdef");
  assert.ok(key.length <= 48);
  assert.match(key, /^[a-z0-9]+(?:-[a-z0-9]+)*$/);
});

test("Workspace image replacement reuses an exact idempotent intent", () => {
  const first = resolveWorkspaceRuntimeImageReplacementIntent(null, "ws-alpha", "registry/workspace@sha256:abc", "promote", () => "key-1");
  const replay = resolveWorkspaceRuntimeImageReplacementIntent(first, "ws-alpha", "registry/workspace@sha256:abc", "promote", () => "key-2");
  const changed = resolveWorkspaceRuntimeImageReplacementIntent(first, "ws-alpha", "registry/workspace@sha256:def", "promote", () => "key-3");

  assert.equal(replay, first);
  assert.equal(changed.idempotencyKey, "key-3");
  assert.equal(changed.replacementImageDigest, "registry/workspace@sha256:def");
});

test("Workspace image replacement readback requires the same runtime and target image", () => {
  const readback = {
    workspaceId: "ws-alpha",
    runtimeId: "runtime-alpha",
    currentImageDigest: "registry/workspace@sha256:def",
    targetImageDigest: "registry/workspace@sha256:def",
    canReplace: false
  };
  assert.equal(workspaceRuntimeImageReplacementReadbackMatches(readback, "ws-alpha", "runtime-alpha", "registry/workspace@sha256:def"), true);
  assert.equal(workspaceRuntimeImageReplacementReadbackMatches({ ...readback, runtimeId: "runtime-other" }, "ws-alpha", "runtime-alpha", "registry/workspace@sha256:def"), false);
  assert.equal(isTerminalWorkspaceRuntimeImageReplacement("succeeded"), true);
  assert.equal(isTerminalWorkspaceRuntimeImageReplacement("started"), false);
});
