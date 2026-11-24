-- name: InsertFabricOperation :exec
INSERT INTO fabric_operations (
    id, operation_id, caller_service, action, resource_kind, resource_id,
    account_id, workspace_id, provider, provider_request_id, idempotency_key,
    request_hash, redacted_provider_payload, status, error_code, retryable,
    compute_pool_key, compute_pool_lease_owner, compute_pool_lease_expires_at,
    started_at, finished_at, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16,
    $17, $18, $19,
    $20, $21, $22
);

-- name: GetFabricOperationByID :one
SELECT id, operation_id, caller_service, action, resource_kind, resource_id,
       account_id, workspace_id, provider, provider_request_id, idempotency_key,
       request_hash, redacted_provider_payload, status, error_code, retryable,
       compute_pool_key, compute_pool_lease_owner, compute_pool_lease_expires_at,
       started_at, finished_at, created_at
FROM fabric_operations
WHERE id = $1;

-- name: GetFabricOperationByClaim :one
SELECT id, operation_id, caller_service, action, resource_kind, resource_id,
       account_id, workspace_id, provider, provider_request_id, idempotency_key,
       request_hash, redacted_provider_payload, status, error_code, retryable,
       compute_pool_key, compute_pool_lease_owner, compute_pool_lease_expires_at,
       started_at, finished_at, created_at
FROM fabric_operations
WHERE action = $1 AND idempotency_key = $2 AND status <> 'rejected'
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetComputePoolHead :one
SELECT id, operation_id, caller_service, action, resource_kind, resource_id,
       account_id, workspace_id, provider, provider_request_id, idempotency_key,
       request_hash, redacted_provider_payload, status, error_code, retryable,
       compute_pool_key, compute_pool_lease_owner, compute_pool_lease_expires_at,
       started_at, finished_at, created_at
FROM fabric_operations
WHERE action IN ('create_compute_allocation', 'ensure_compute_allocation')
  AND status IN ('started', 'claim_pending')
  AND compute_pool_key = $1
ORDER BY created_at, id
LIMIT 1;

-- name: ReleaseComputePoolHeadLease :execrows
UPDATE fabric_operations
SET compute_pool_lease_owner = '', compute_pool_lease_expires_at = NULL
WHERE id = $1 AND status = 'started' AND compute_pool_key = $2 AND compute_pool_lease_owner = $3;

-- name: GetMachineOwnershipByResourceID :one
SELECT id, resource_id, account_id, workspace_id, package_id, node_pool_id,
       machine_id, instance_id, node_name, status, provider_request_id,
       claimed_at, released_at
FROM machine_ownerships
WHERE resource_id = $1;

-- name: ListMachineOwnerships :many
SELECT id, resource_id, account_id, workspace_id, package_id, node_pool_id,
       machine_id, instance_id, node_name, status, provider_request_id,
       claimed_at, released_at
FROM machine_ownerships
ORDER BY claimed_at ASC, id ASC;

-- name: UpdateMachineOwnership :execrows
UPDATE machine_ownerships
SET account_id = $2,
    workspace_id = $3,
    package_id = $4,
    node_pool_id = $5,
    machine_id = $6,
    instance_id = $7,
    node_name = $8,
    status = $9,
    provider_request_id = $10,
    claimed_at = $11,
    released_at = $12
WHERE resource_id = $1;
