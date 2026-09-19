import assert from "node:assert/strict";
import test from "node:test";

import type {
  SourceEnvelope,
  WorkspaceRuntimeDTO
} from "../../apps/console-ui/src/api/dtos.ts";
import {
  applyFabricRuntimeReadCompletion,
  createFabricRuntimeReadScope,
  createFabricRuntimeReadState,
  fabricRuntimeReadScopeIsCurrent,
  fabricRuntimeReadSourceMatchesScope,
  type FabricRuntimeReadScope
} from "../../apps/console-ui/src/app/fabric-runtime-read-controller-model.ts";

const fetchedAt = "2026-08-27T00:00:00Z";

function runtime(workspaceId: string, runtimeId = `runtime-${workspaceId}`): WorkspaceRuntimeDTO {
  return {
    workspaceId,
    runtimeId,
    status: "running",
    ready: true,
    url: `https://workspace.example.invalid/w/${workspaceId}/`,
    serviceName: `runtime-${workspaceId}`,
    access: {
      username: "opl",
      credentialStatus: "configured",
      credentialVersion: "v1"
    },
    checks: [
      { name: "deployment_ready", ok: true },
      { name: "ready_pod_uses_retained_pvc", ok: true }
    ]
  };
}

function source(
  data: WorkspaceRuntimeDTO,
  sourceName = "fabric"
): SourceEnvelope<WorkspaceRuntimeDTO> {
  return {
    source: sourceName,
    status: "available",
    available: true,
    fetchedAt,
    data
  };
}

function unavailable(sourceName = "fabric"): SourceEnvelope<WorkspaceRuntimeDTO> {
  return {
    source: sourceName,
    status: "unavailable",
    available: false,
    fetchedAt,
    reasonCode: `${sourceName}_unavailable`
  };
}

function scope(overrides: Partial<FabricRuntimeReadScope> = {}): FabricRuntimeReadScope {
  return createFabricRuntimeReadScope({
    sessionGeneration: 3,
    routeGeneration: 5,
    requestGeneration: 7,
    workspaceId: "workspace-alpha",
    ...overrides
  });
}

test("Fabric Runtime source accepts only Fabric and the requested Workspace identity", () => {
  const activeScope = scope();

  assert.equal(fabricRuntimeReadSourceMatchesScope(activeScope, source(runtime("workspace-alpha"))), true);
  assert.equal(fabricRuntimeReadSourceMatchesScope(activeScope, source(runtime("workspace-beta"))), false);
  assert.equal(fabricRuntimeReadSourceMatchesScope(activeScope, source(runtime("workspace-alpha"), "control-plane")), false);
  assert.equal(fabricRuntimeReadSourceMatchesScope(activeScope, unavailable()), true);
  assert.equal(fabricRuntimeReadSourceMatchesScope(activeScope, unavailable("control-plane")), false);
});

test("scope identity rejects route, Session, request, and Workspace changes", () => {
  const activeScope = scope();

  assert.equal(fabricRuntimeReadScopeIsCurrent(activeScope, activeScope), true);
  assert.equal(fabricRuntimeReadScopeIsCurrent(activeScope, scope({ sessionGeneration: 4 })), false);
  assert.equal(fabricRuntimeReadScopeIsCurrent(activeScope, scope({ routeGeneration: 6 })), false);
  assert.equal(fabricRuntimeReadScopeIsCurrent(activeScope, scope({ requestGeneration: 8 })), false);
  assert.equal(fabricRuntimeReadScopeIsCurrent(activeScope, scope({ workspaceId: "workspace-beta" })), false);
});

test("current available completion commits the typed Runtime projection", () => {
  const state = createFabricRuntimeReadState();
  const activeScope = scope();
  const runtimeSource = source(runtime(activeScope.workspaceId));
  const result = applyFabricRuntimeReadCompletion(state, {
    activeScope,
    responseScope: activeScope,
    source: runtimeSource
  });

  assert.ok(result);
  assert.deepEqual(result.runtime, {
    value: runtimeSource,
    loading: false,
    error: ""
  });
});

test("same-Workspace refresh rejects the older completion", () => {
  const state = createFabricRuntimeReadState();
  const older = scope({ requestGeneration: 7 });
  const newer = scope({ requestGeneration: 8 });
  const stale = applyFabricRuntimeReadCompletion(state, {
    activeScope: newer,
    responseScope: older,
    source: source(runtime(older.workspaceId, "runtime-stale"))
  });
  const fresh = applyFabricRuntimeReadCompletion(state, {
    activeScope: newer,
    responseScope: newer,
    source: source(runtime(newer.workspaceId, "runtime-fresh"))
  });

  assert.equal(stale, null);
  assert.ok(fresh?.runtime.value?.available);
  assert.equal(fresh.runtime.value.data.runtimeId, "runtime-fresh");
});

test("late completion cannot cross a route, Session, or Workspace boundary", () => {
  const state = createFabricRuntimeReadState();
  const responseScope = scope();

  for (const activeScope of [
    scope({ sessionGeneration: 4 }),
    scope({ routeGeneration: 6 }),
    scope({ workspaceId: "workspace-beta" })
  ]) {
    assert.equal(applyFabricRuntimeReadCompletion(state, {
      activeScope,
      responseScope,
      source: source(runtime(responseScope.workspaceId))
    }), null);
  }
});

test("current Fabric unavailable completion settles only the Runtime projection", () => {
  const state = createFabricRuntimeReadState();
  const activeScope = scope();
  const runtimeUnavailable = unavailable();
  const result = applyFabricRuntimeReadCompletion(state, {
    activeScope,
    responseScope: activeScope,
    source: runtimeUnavailable,
    error: "Runtime 暂不可用"
  });

  assert.ok(result);
  assert.deepEqual(result.runtime, {
    value: runtimeUnavailable,
    loading: false,
    error: "Runtime 暂不可用"
  });
});

test("reset state contains no retained Runtime projection", () => {
  assert.deepEqual(createFabricRuntimeReadState(), {
    runtime: { value: null, loading: false, error: "" }
  });
});
