import assert from "node:assert/strict";
import test from "node:test";

import type {
  GatewayKeySecretDTO,
  RuntimeCredentialResponse
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  acceptWorkspaceSecretCompletion,
  shouldExpireWorkspaceSecret,
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
