-- name: CountAccounts :one
SELECT count(*) FROM control_plane_accounts;

-- name: CountAccountStatuses :many
SELECT status, count(*) AS total
FROM control_plane_accounts
GROUP BY status;

-- name: GetAccountByID :one
SELECT id, created_at, updated_at, owner_user_id, sub2api_user_id, name, status, workspace_purchase_enabled
FROM control_plane_accounts
WHERE id = $1;

-- name: ListAccountsBySub2APIUserID :many
SELECT id, created_at, updated_at, owner_user_id, sub2api_user_id, name, status, workspace_purchase_enabled
FROM control_plane_accounts
WHERE sub2api_user_id = $1
LIMIT 2;

-- name: GetUserByID :one
SELECT id, created_at, updated_at, account_id, email, role, status,
       disabled_at, disabled_by, disabled_reason, deleted_at, deleted_by, delete_reason
FROM control_plane_users
WHERE id = $1;

-- name: DeleteUserByID :exec
DELETE FROM control_plane_users
WHERE id = $1;

-- name: GetSessionByID :one
SELECT id, created_at, updated_at, user_id, csrf, expires_at
FROM control_plane_sessions
WHERE id = $1;

-- name: DeleteSessionByID :exec
DELETE FROM control_plane_sessions
WHERE id = $1;

-- name: GetRuntimeOperationByID :one
SELECT id, created_at, updated_at, operation_id, account_id, workspace_id, period_start,
       resource_id, resource_kind, action, provider, provider_request_id, status, result,
       compute_allocation_id, storage_id, attachment_id, runtime_service_name,
       cvm_instance_id, instance_id, node_name, machine_name
FROM control_plane_runtime_operations
WHERE id = $1;
