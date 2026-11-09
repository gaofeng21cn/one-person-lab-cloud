import assert from "node:assert/strict";
import test from "node:test";
import {
  decodeFabricComputeBinding,
  decodeWorkspaceComputeBinding,
  fabricComputeBindingSql,
  workspaceComputeBindingSql,
  type ComputeReadbackBinding,
} from "../../tools/workspace-compute-readback-binding.ts";

const binding: Omit<ComputeReadbackBinding, "computeId"> = {
  workspaceId: "workspace_1",
  workspaceOperationId: "op_workspace_delete",
  tenantId: "tenant_1",
  resourceSetId: "set_1",
  fabricOperationId: "op_fabric_delete",
};
const computeId = "compute_1";
const error = "workspace_compute_readback_binding_invalid";
const rows = (row: unknown): string[] => [JSON.stringify(row)];

// These are the SELECT projections, not pre-decoded objects: every decoder test
// passes actual psql-style JSON text through JSON.parse inside the production API.
function workspaceRow() {
  return {
    operationId: binding.workspaceOperationId,
    tenantId: binding.tenantId,
    resourceId: binding.workspaceId,
    kind: "delete_workspace",
    workspaceTenantId: binding.tenantId,
    frozenSetId: binding.resourceSetId,
    resourceReadback: { workspaceId: binding.workspaceId, resourceSetId: binding.resourceSetId },
    deleteOperation: { operationId: binding.fabricOperationId, resourceId: binding.resourceSetId },
  };
}

function fabricRow() {
  return {
    resourceSetId: binding.resourceSetId,
    workspaceId: binding.workspaceId,
    tenantId: binding.tenantId,
    operation: {
      operationId: binding.fabricOperationId,
      resourceId: binding.resourceSetId,
      tenantId: binding.tenantId,
      kind: "resource_delete",
    },
    resources: [{ id: computeId, resourceSetId: binding.resourceSetId, kind: "compute" }],
    deletionActions: [{
      resourceSetId: binding.resourceSetId,
      resourceId: computeId,
      commandId: `${binding.fabricOperationId}:compute`,
      action: "delete",
      approvedInput: { deletionKind: "compute", resourceSetId: binding.resourceSetId, resourceId: computeId },
    }],
  };
}

function workspaceDecode(value: unknown) {
  return decodeWorkspaceComputeBinding(value, binding.workspaceId, binding.workspaceOperationId);
}

function invalid(run: () => unknown) {
  assert.throws(run, (cause: unknown) => {
    assert.ok(cause instanceof Error);
    assert.equal(cause.message, error);
    assert.equal(cause.cause, undefined);
    return true;
  });
}

function change<T>(input: T, path: string, value: unknown, remove = false): T {
  const result = structuredClone(input);
  const keys = path.split(".");
  let parent = result as Record<string, unknown>;
  for (const key of keys.slice(0, -1)) parent = parent[key] as Record<string, unknown>;
  const key = keys.at(-1)!;
  if (remove) delete parent[key];
  else parent[key] = value;
  return result;
}

test("binds original Workspace deletion through the actual JSON decoders to one compute delete action", () => {
  const workspace = workspaceDecode(rows(workspaceRow()));
  assert.deepEqual(workspace, binding);
  assert.deepEqual(decodeFabricComputeBinding(rows(fabricRow()), workspace), { ...binding, computeId });
  // No normalization, state inference, or extra fields survive the boundary.
  assert.deepEqual(Object.keys(workspace), Object.keys(binding));
});

test("both decoders reject non-array, non-text, malformed JSON and multiple rows without leaking input", () => {
  const inputs: unknown[] = [
    undefined, null, true, 1, {}, "[]", [], [undefined], [null], [1], [{}],
    [""], [" "], ["private-token=never-expose"], ['{"secret":"never-expose",}'],
    ['{} {}'], ["null"], ["true"], ["0"], ['"private-token"'], ["[]"],
    [JSON.stringify(workspaceRow()), JSON.stringify(workspaceRow())],
    [JSON.stringify(fabricRow()), JSON.stringify(fabricRow())],
    [JSON.stringify(workspaceRow()), ""], ["", JSON.stringify(fabricRow())],
  ];
  for (const input of inputs) {
    invalid(() => workspaceDecode(input));
    invalid(() => decodeFabricComputeBinding(input, binding));
  }
  invalid(() => workspaceDecode([workspaceRow()]));
  invalid(() => decodeFabricComputeBinding([fabricRow()], binding));
});

test("Workspace refuses wrong operation, workspace, tenant, frozen set and persisted owner references", () => {
  const replacements: [string, unknown][] = [
    ["operationId", "op_other"], ["resourceId", "workspace_other"], ["kind", "create_workspace"],
    ["tenantId", "tenant_other"], ["workspaceTenantId", "tenant_other"], ["workspaceTenantId", null],
    ["frozenSetId", "set_other"], ["frozenSetId", 1], ["frozenSetId", true],
    ["frozenSetId", []], ["frozenSetId", {}], ["frozenSetId", "set_1\\n"],
    ["resourceReadback.workspaceId", "workspace_other"], ["resourceReadback.resourceSetId", "set_other"],
    ["deleteOperation.resourceId", "set_other"], ["deleteOperation.operationId", ""],
    ["resourceReadback", []], ["deleteOperation", []], ["resourceReadback", null], ["deleteOperation", null],
  ];
  for (const [path, value] of replacements) invalid(() => workspaceDecode(rows(change(workspaceRow(), path, value))));
  invalid(() => decodeWorkspaceComputeBinding(rows(workspaceRow()), "workspace_other", binding.workspaceOperationId));
  invalid(() => decodeWorkspaceComputeBinding(rows(workspaceRow()), binding.workspaceId, "op_other"));
  // A self-consistent result for a different set still contradicts the frozen order.
  const rebound = workspaceRow();
  rebound.resourceReadback.resourceSetId = "set_other";
  rebound.deleteOperation.resourceId = "set_other";
  invalid(() => workspaceDecode(rows(rebound)));
});

test("an owner-permitted unfrozen set still requires the entire original Fabric and compute binding", () => {
  // freezeUnfinishedDeletion may commit before a resourceSetId is known, and
  // confirmOriginalResourceCloseout stores later readback without changing that
  // accepted input. SQL projects missing/JSON null as null, or the empty string.
  for (const frozenSetId of [null, ""]) {
    const row = { ...workspaceRow(), frozenSetId };
    const workspace = workspaceDecode(rows(row));
    assert.deepEqual(workspace, binding);
    assert.deepEqual(decodeFabricComputeBinding(rows(fabricRow()), workspace), { ...binding, computeId });
    invalid(() => workspaceDecode(rows(change(row, "resourceReadback.resourceSetId", ""))));
    invalid(() => workspaceDecode(rows(change(row, "deleteOperation.resourceId", "set_other"))));
    invalid(() => decodeFabricComputeBinding(rows(change(fabricRow(), "operation.operationId", "op_other")), workspace));
    invalid(() => decodeFabricComputeBinding(rows(change(fabricRow(), "deletionActions.0.commandId", "op_other:compute")), workspace));
    invalid(() => workspaceDecode(rows(change(row, "frozenSetId", undefined, true))));
  }
});

test("Workspace requires every projected field and rejects unknown fields at each level", () => {
  const row = workspaceRow();
  const paths = [
    ...Object.keys(row),
    ...Object.keys(row.resourceReadback).map(key => `resourceReadback.${key}`),
    ...Object.keys(row.deleteOperation).map(key => `deleteOperation.${key}`),
  ];
  for (const path of paths) invalid(() => workspaceDecode(rows(change(row, path, undefined, true))));
  for (const path of ["unknown", "resourceReadback.tenantId", "deleteOperation.workspaceId"]) {
    invalid(() => workspaceDecode(rows(change(row, path, "unrecognized-binding"))));
  }
  for (const path of ["tenantId", "resourceReadback.resourceSetId", "deleteOperation.operationId"]) {
    for (const value of [1, true, [], {}, "", " id", "id\n", "id' OR '1'='1"]) {
      invalid(() => workspaceDecode(rows(change(row, path, value))));
    }
  }
});

test("Fabric refuses wrong workspace, tenant, set, operation, compute or original action identities", () => {
  const replacements: [string, unknown][] = [
    ["workspaceId", "workspace_other"], ["tenantId", "tenant_other"], ["resourceSetId", "set_other"],
    ["operation.operationId", "op_other"], ["operation.resourceId", "set_other"],
    ["operation.tenantId", "tenant_other"], ["operation.kind", "resource_provision"],
    ["resources.0.id", "compute_other"], ["resources.0.kind", "storage"],
    ["resources.0.resourceSetId", "set_other"],
    ["deletionActions.0.resourceSetId", "set_other"], ["deletionActions.0.resourceId", "compute_other"],
    ["deletionActions.0.commandId", "op_other:compute"], ["deletionActions.0.commandId", `${binding.fabricOperationId}:storage`],
    ["deletionActions.0.action", "allocate"], ["deletionActions.0.approvedInput.deletionKind", "storage"],
    ["deletionActions.0.approvedInput.resourceSetId", "set_other"],
    ["deletionActions.0.approvedInput.resourceId", "compute_other"],
    ["deletionActions.0.approvedInput", null], ["deletionActions.0.approvedInput", []],
  ];
  for (const [path, value] of replacements) {
    invalid(() => decodeFabricComputeBinding(rows(change(fabricRow(), path, value)), binding));
  }
  invalid(() => decodeFabricComputeBinding(rows(fabricRow()), { ...binding, fabricOperationId: "op_other" }));
});

test("Fabric rejects missing, duplicate or ambiguous compute resources and deletion actions", () => {
  const row = fabricRow();
  for (const path of ["resources", "deletionActions"] as const) {
    for (const value of [null, {}, "[]", [], [null], [[]], [{ ...row[path][0] }, { ...row[path][0] }]]) {
      invalid(() => decodeFabricComputeBinding(rows(change(row, path, value)), binding));
    }
  }
  const resources = [...row.resources, { ...row.resources[0], id: "compute_other" }];
  invalid(() => decodeFabricComputeBinding(rows({ ...row, resources }), binding));
  const deletionActions = [...row.deletionActions, { ...row.deletionActions[0], commandId: "op_other:compute" }];
  invalid(() => decodeFabricComputeBinding(rows({ ...row, deletionActions }), binding));
});

test("Fabric requires exact schemas for the row, operation, resource, action and approved input", () => {
  const row = fabricRow();
  const objects: [string, object][] = [
    ["", row], ["operation", row.operation], ["resources.0", row.resources[0]],
    ["deletionActions.0", row.deletionActions[0]], ["deletionActions.0.approvedInput", row.deletionActions[0].approvedInput],
  ];
  for (const [prefix, value] of objects) {
    for (const key of Object.keys(value)) {
      const path = prefix ? `${prefix}.${key}` : key;
      invalid(() => decodeFabricComputeBinding(rows(change(row, path, undefined, true)), binding));
      invalid(() => decodeFabricComputeBinding(rows(change(row, path, null)), binding));
    }
    const unknown = prefix ? `${prefix}.unknown` : "unknown";
    invalid(() => decodeFabricComputeBinding(rows(change(row, unknown, "unrecognized-binding")), binding));
  }
});

test("Fabric input binding itself is exact, required and independently validated", () => {
  for (const value of [null, [], {}, { ...binding, computeId }, { ...binding, unknown: true }]) {
    invalid(() => fabricComputeBindingSql(value as typeof binding));
    invalid(() => decodeFabricComputeBinding(rows(fabricRow()), value as typeof binding));
  }
  for (const key of Object.keys(binding)) {
    const value = change(binding, key, undefined, true);
    invalid(() => fabricComputeBindingSql(value));
    invalid(() => decodeFabricComputeBinding(rows(fabricRow()), value));
  }
});

test("every public entry rejects SQL injection and noncanonical ID tokens without exposing them", () => {
  const invalidIds: unknown[] = [
    null, undefined, 1, true, {}, [], "", " id", "id ", "id\n", "id\r\n", "id\0", "id\t",
    "id' OR '1'='1", "id'; SELECT pg_sleep(1); --", "id\\", "id--\nSELECT 1", "$(id)",
    "id/*comment*/", "id:compute", "id.with.dot", "中文", "_id", "-id", "a".repeat(257),
  ];
  for (const input of invalidIds) {
    const token = input as string;
    invalid(() => workspaceComputeBindingSql(token, binding.workspaceOperationId));
    invalid(() => workspaceComputeBindingSql(binding.workspaceId, token));
    invalid(() => decodeWorkspaceComputeBinding(rows(workspaceRow()), token, binding.workspaceOperationId));
    invalid(() => decodeWorkspaceComputeBinding(rows(workspaceRow()), binding.workspaceId, token));
    for (const key of Object.keys(binding)) {
      const value = change(binding, key, input);
      invalid(() => fabricComputeBindingSql(value));
      invalid(() => decodeFabricComputeBinding(rows(fabricRow()), value));
    }
    invalid(() => decodeFabricComputeBinding(rows(change(fabricRow(), "resources.0.id", input)), binding));
  }
  // Valid IDs remain exact and include the maximum accepted token length.
  const token = `A${"b".repeat(253)}_9`;
  assert.equal(token.length, 256);
  assert.match(workspaceComputeBindingSql(token, "op_1-2"), new RegExp(`o.resource_id='${token}'`));
});

test("SQL is one narrow SELECT per owner and preserves JSON identity types", () => {
  const workspaceSql = workspaceComputeBindingSql(binding.workspaceId, binding.workspaceOperationId);
  const fabricSql = fabricComputeBindingSql(binding);
  for (const sql of [workspaceSql, fabricSql]) {
    assert.match(sql, /^SELECT json_build_object\(/u);
    assert.doesNotMatch(sql, /;|\b(?:INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|CALL|DO|BEGIN|COMMIT)\b/u);
    assert.doesNotMatch(sql, /provider_|fabric_operations|status|observation_result|error_code|updated_at|SELECT \*/u);
  }
  assert.match(workspaceSql, /FROM workspace.operations o/u);
  assert.match(workspaceSql, /LEFT JOIN workspace.workspaces w ON w.id=o.resource_id/u);
  assert.match(workspaceSql, /o.id='op_workspace_delete'.*o.resource_id='workspace_1'.*o.kind='delete_workspace'/u);
  assert.match(workspaceSql, /accepted_input->'resourceSetId'/u);
  assert.match(workspaceSql, /result->'resourceReadback'->'workspaceId'/u);
  assert.match(workspaceSql, /result->'deleteOperation'->'operationId'/u);
  assert.doesNotMatch(workspaceSql, /->>|fabric\./u);
  assert.match(fabricSql, /FROM fabric.resource_sets s/u);
  assert.match(fabricSql, /JOIN fabric.operations o ON o.id='op_fabric_delete'/u);
  assert.match(fabricSql, /s.id='set_1'.*s.workspace_id='workspace_1'.*s.tenant_id='tenant_1'/u);
  assert.match(fabricSql, /r.resource_set_id=s.id AND r.kind='compute'/u);
  assert.match(fabricSql, /'approvedInput',a.approved_input/u);
  // Include conflicting candidates rather than filtering them into apparent success.
  assert.match(fabricSql, /a.approved_input->>'deletionKind'='compute' OR a.command_id=o.id \|\| ':compute' OR/u);
  assert.match(fabricSql, /a.action='delete' AND a.resource_id IN/u);
  assert.doesNotMatch(fabricSql, /workspace\.|LIMIT|DISTINCT/u);
});
