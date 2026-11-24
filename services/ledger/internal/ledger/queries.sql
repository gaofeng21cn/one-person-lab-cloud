-- name: InsertEvidenceReceipt :exec
INSERT INTO evidence_receipts (
    id, receipt_type, status, account_id, organization_id, workspace_id,
    project_id, task_id, job_id, artifact_id, review_id,
    payload_json, supersedes_receipt_id, idempotency_key, request_hash, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16
);

-- name: GetEvidenceReceiptByID :one
SELECT id, receipt_type, status, account_id, organization_id, workspace_id,
       project_id, task_id, job_id, artifact_id, review_id,
       payload_json, supersedes_receipt_id, created_at
FROM evidence_receipts
WHERE id = $1;

-- name: GetEvidenceReceiptByIdempotencyKey :one
SELECT id, receipt_type, status, account_id, organization_id, workspace_id,
       project_id, task_id, job_id, artifact_id, review_id,
       payload_json, supersedes_receipt_id, request_hash, created_at
FROM evidence_receipts
WHERE idempotency_key = $1;

-- name: LockEvidenceReceiptForMutation :one
SELECT payload_json, created_at
FROM evidence_receipts
WHERE id = $1 AND receipt_type <> $2
FOR UPDATE /* ledger_receipt_mutation */;

-- name: UpdateEvidenceReceiptPayload :exec
UPDATE evidence_receipts
SET payload_json = $1
WHERE id = $2;

-- name: GetIdempotencyKeyByID :one
SELECT request_hash, response_ref
FROM idempotency_keys
WHERE id = $1;

-- name: InsertIdempotencyKey :exec
INSERT INTO idempotency_keys (
    id, service, idempotency_key, request_hash, response_ref, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: InsertEvidenceIndexEntry :exec
INSERT INTO evidence_index_entries (
    id, operation_id, candidate_sha, candidate_tree, image_digest,
    receipt_id, receipt_type, status, actor, observed_at,
    identity_digest, redacted_link, idempotency_key, request_hash, created_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15
);

-- name: GetEvidenceIndexByIdempotencyKey :one
SELECT id, operation_id, candidate_sha, candidate_tree, image_digest,
       receipt_id, receipt_type, status, actor, observed_at,
       identity_digest, redacted_link, request_hash, created_at
FROM evidence_index_entries
WHERE idempotency_key = $1;

-- name: InsertReconciliationReport :exec
INSERT INTO reconciliation_reports (
    id, status, report_json, block_new_workspaces, reason,
    idempotency_key, request_hash, created_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8
);

-- name: GetReconciliationByIdempotencyKey :one
SELECT id, status, report_json, block_new_workspaces, reason, request_hash, created_at
FROM reconciliation_reports
WHERE idempotency_key = $1;
