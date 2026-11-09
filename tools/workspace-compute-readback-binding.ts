export type ComputeReadbackBinding = {
  workspaceId: string;
  workspaceOperationId: string;
  tenantId: string;
  resourceSetId: string;
  fabricOperationId: string;
  computeId: string;
};

type WorkspaceBinding = Omit<ComputeReadbackBinding, "computeId">;

function fail(): never {
  throw new Error("workspace_compute_readback_binding_invalid");
}

function exact(value: unknown, keys: readonly string[]): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail();
  const row = value as Record<string, unknown>;
  if (Object.keys(row).length !== keys.length || keys.some(key => !Object.hasOwn(row, key))) fail();
  return row;
}

function id(value: unknown): string {
  // Owner IDs are opaque ASCII tokens, never SQL expressions or provider refs.
  // Checking every character also rejects a final newline (unlike a bare $).
  if (typeof value !== "string" || value.length < 1 || value.length > 256 ||
      !/^[A-Za-z0-9]/u.test(value) || /[^A-Za-z0-9_-]/u.test(value)) fail();
  return value;
}

function sqlId(value: unknown): string {
  return `'${id(value)}'`;
}

function oneRow(rows: unknown): unknown {
  if (!Array.isArray(rows) || rows.length !== 1 || typeof rows[0] !== "string") fail();
  try {
    return JSON.parse(rows[0]) as unknown;
  } catch {
    fail();
  }
}

function workspaceBinding(value: unknown): WorkspaceBinding {
  const row = exact(value, ["workspaceId", "workspaceOperationId", "tenantId", "resourceSetId", "fabricOperationId"]);
  return {
    workspaceId: id(row.workspaceId),
    workspaceOperationId: id(row.workspaceOperationId),
    tenantId: id(row.tenantId),
    resourceSetId: id(row.resourceSetId),
    fabricOperationId: id(row.fabricOperationId),
  };
}

// workspace/migrations/0001_opl_workspace.sql and internal/launch/deletion.go:
// accepted_input may freeze the set; unfinished creates can retain an empty set
// (internal/launch/closeout.go). The result retains the original Fabric intent
// and resource readback. Project identities with -> so JSON numbers/booleans
// cannot silently become valid string IDs through ->>.
export function workspaceComputeBindingSql(workspaceId: string, workspaceOperationId: string): string {
  const workspace = sqlId(workspaceId), operation = sqlId(workspaceOperationId);
  return `SELECT json_build_object(
    'operationId',o.id,'tenantId',o.tenant_id,'resourceId',o.resource_id,'kind',o.kind,
    'workspaceTenantId',w.tenant_id,'frozenSetId',o.accepted_input->'resourceSetId',
    'resourceReadback',json_build_object(
      'workspaceId',o.result->'resourceReadback'->'workspaceId',
      'resourceSetId',o.result->'resourceReadback'->'resourceSetId'),
    'deleteOperation',json_build_object(
      'operationId',o.result->'deleteOperation'->'operationId',
      'resourceId',o.result->'deleteOperation'->'resourceId')
  )::text FROM workspace.operations o
  LEFT JOIN workspace.workspaces w ON w.id=o.resource_id
  WHERE o.id=${operation} AND o.resource_id=${workspace} AND o.kind='delete_workspace'`;
}

export function decodeWorkspaceComputeBinding(
  rows: unknown, workspaceId: string, workspaceOperationId: string,
): WorkspaceBinding {
  id(workspaceId);
  id(workspaceOperationId);
  const row = exact(oneRow(rows), [
    "operationId", "tenantId", "resourceId", "kind", "workspaceTenantId", "frozenSetId",
    "resourceReadback", "deleteOperation",
  ]);
  const readback = exact(row.resourceReadback, ["workspaceId", "resourceSetId"]);
  const deleted = exact(row.deleteOperation, ["operationId", "resourceId"]);
  const tenantId = id(row.tenantId), resourceSetId = id(readback.resourceSetId);
  const fabricOperationId = id(deleted.operationId);
  if (row.operationId !== workspaceOperationId || row.resourceId !== workspaceId ||
      row.kind !== "delete_workspace" || row.workspaceTenantId !== tenantId ||
      readback.workspaceId !== workspaceId || deleted.resourceId !== resourceSetId) fail();
  // freezeUnfinishedDeletion can accept before Fabric returns a set. Its later
  // closeout records the set in result without rewriting accepted_input. Thus a
  // projected null/empty frozen set is allowed, but a present identity must agree.
  if (row.frozenSetId !== null && row.frozenSetId !== "" && row.frozenSetId !== resourceSetId) fail();
  return { workspaceId, workspaceOperationId, tenantId, resourceSetId, fabricOperationId };
}

// fabric/ownermigrations/0001_opl_fabric.sql and coordination/delete.go:
// recordDeletionAttempt binds command_id to operationID+":compute", and its
// approved_input contains exactly deletionKind/resourceSetId/resourceId.
// Select every compute candidate, including malformed delete actions associated
// through their resource or command, so predicates cannot hide a wrong binding.
export function fabricComputeBindingSql(binding: WorkspaceBinding): string {
  const b = workspaceBinding(binding);
  const set = sqlId(b.resourceSetId), operation = sqlId(b.fabricOperationId);
  return `SELECT json_build_object(
    'resourceSetId',s.id,'workspaceId',s.workspace_id,'tenantId',s.tenant_id,
    'operation',json_build_object(
      'operationId',o.id,'resourceId',o.resource_id,'tenantId',o.tenant_id,'kind',o.kind),
    'resources',coalesce((SELECT json_agg(json_build_object(
      'id',r.id,'resourceSetId',r.resource_set_id,'kind',r.kind) ORDER BY r.id)
      FROM fabric.resources r WHERE r.resource_set_id=s.id AND r.kind='compute'),'[]'::json),
    'deletionActions',coalesce((SELECT json_agg(json_build_object(
      'resourceSetId',a.resource_set_id,'resourceId',a.resource_id,'commandId',a.command_id,
      'action',a.action,'approvedInput',a.approved_input) ORDER BY a.command_id)
      FROM fabric.resource_actions a WHERE a.resource_set_id=s.id AND (
        a.approved_input->>'deletionKind'='compute' OR a.command_id=o.id || ':compute' OR
        (a.action='delete' AND a.resource_id IN (
          SELECT r.id FROM fabric.resources r WHERE r.resource_set_id=s.id AND r.kind='compute'))
      )),'[]'::json)
  )::text FROM fabric.resource_sets s
  JOIN fabric.operations o ON o.id=${operation}
  WHERE s.id=${set} AND s.workspace_id=${sqlId(b.workspaceId)} AND s.tenant_id=${sqlId(b.tenantId)}`;
}

export function decodeFabricComputeBinding(rows: unknown, binding: WorkspaceBinding): ComputeReadbackBinding {
  const b = workspaceBinding(binding);
  const row = exact(oneRow(rows), ["resourceSetId", "workspaceId", "tenantId", "operation", "resources", "deletionActions"]);
  const operation = exact(row.operation, ["operationId", "resourceId", "tenantId", "kind"]);
  if (row.resourceSetId !== b.resourceSetId || row.workspaceId !== b.workspaceId || row.tenantId !== b.tenantId ||
      operation.operationId !== b.fabricOperationId || operation.resourceId !== b.resourceSetId ||
      operation.tenantId !== b.tenantId || operation.kind !== "resource_delete") fail();
  if (!Array.isArray(row.resources) || row.resources.length !== 1 ||
      !Array.isArray(row.deletionActions) || row.deletionActions.length !== 1) fail();
  const compute = exact(row.resources[0], ["id", "resourceSetId", "kind"]);
  const computeId = id(compute.id);
  if (compute.resourceSetId !== b.resourceSetId || compute.kind !== "compute") fail();
  const action = exact(row.deletionActions[0], ["resourceSetId", "resourceId", "commandId", "action", "approvedInput"]);
  const approved = exact(action.approvedInput, ["deletionKind", "resourceSetId", "resourceId"]);
  if (action.resourceSetId !== b.resourceSetId || action.resourceId !== computeId || action.action !== "delete" ||
      action.commandId !== `${b.fabricOperationId}:compute` || approved.deletionKind !== "compute" ||
      approved.resourceSetId !== b.resourceSetId || approved.resourceId !== computeId) fail();
  return { ...b, computeId };
}
