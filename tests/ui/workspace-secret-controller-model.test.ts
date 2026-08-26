import assert from "node:assert/strict";
import test from "node:test";

import type {
  GatewayKeySecretDTO,
  RuntimeCredentialResponse
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  acceptWorkspaceSecretCompletion,
  resolveRuntimeRotationIntent,
  shouldExpireWorkspaceSecret,
  type RuntimeRotationIntent,
  type WorkspaceSecretProjection
} from "../../apps/console-ui/src/app/workspace-secret-controller-model.ts";

const runtimeCredential: RuntimeCredentialResponse = {
  workspaceId: "workspace-alpha",
  access: {
    account: "alpha",
    username: "opl",
    password: "runtime-secret",
    credentialStatus: "active",
    credentialVersion: "2"
  },
  receiptId: "receipt-runtime-alpha"
};

const workspaceKey: GatewayKeySecretDTO = {
  id: "gateway-key-alpha",
  name: "Workspace Alpha",
  status: "active",
  value: "workspace-key-secret"
};

test("accepted Runtime credential completion hides the Workspace Key", () => {
  const result = acceptWorkspaceSecretCompletion(4, 4, {
    kind: "runtime-credential",
    response: runtimeCredential
  });
  const expected: WorkspaceSecretProjection = {
    apiKey: null,
    workspace: runtimeCredential.access
  };

  assert.deepEqual(result, expected);
});

test("accepted Workspace Key completion hides the Runtime credential", () => {
  const result = acceptWorkspaceSecretCompletion(7, 7, {
    kind: "workspace-key",
    response: workspaceKey
  });
  const expected: WorkspaceSecretProjection = {
    apiKey: workspaceKey,
    workspace: null
  };

  assert.deepEqual(result, expected);
});

test("invalidated request generation cannot restore Secret data", () => {
  const result = acceptWorkspaceSecretCompletion(8, 9, {
    kind: "runtime-credential",
    response: runtimeCredential
  });

  assert.equal(result, null);
});

test("Workspace Secret expires at the exact 60 second boundary", () => {
  const revealedAtMs = 1_000;

  assert.equal(shouldExpireWorkspaceSecret(revealedAtMs, revealedAtMs + 59_999), false);
  assert.equal(shouldExpireWorkspaceSecret(revealedAtMs, revealedAtMs + 60_000), true);
});

test("Runtime rotation reuses an intent for the same Workspace", () => {
  let keysCreated = 0;
  const current: RuntimeRotationIntent = {
    workspaceId: "workspace-alpha",
    idempotencyKey: "runtime-credential:existing"
  };

  const result = resolveRuntimeRotationIntent(current, "workspace-alpha", () => {
    keysCreated += 1;
    return "runtime-credential:new";
  });

  assert.equal(result, current);
  assert.equal(result.idempotencyKey, "runtime-credential:existing");
  assert.equal(keysCreated, 0);
});

test("Runtime rotation creates a new intent when the Workspace changes", () => {
  let keysCreated = 0;
  const current: RuntimeRotationIntent = {
    workspaceId: "workspace-alpha",
    idempotencyKey: "runtime-credential:existing"
  };

  const result = resolveRuntimeRotationIntent(current, "workspace-beta", () => {
    keysCreated += 1;
    return "runtime-credential:new";
  });

  assert.deepEqual(result, {
    workspaceId: "workspace-beta",
    idempotencyKey: "runtime-credential:new"
  });
  assert.equal(keysCreated, 1);
});
