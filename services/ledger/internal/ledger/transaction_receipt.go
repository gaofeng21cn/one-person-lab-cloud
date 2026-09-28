package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

func (s *PostgresStore) recordReceiptTx(ctx context.Context, tx *sql.Tx, input ReceiptInput) (Receipt, error) {
	hashInput := input
	hashInput.IdempotencyKey = ""
	requestHash, err := hashJSON(hashInput)
	if err != nil {
		return Receipt{}, err
	}
	if existing, existingHash, err := receiptByIdempotencyKeyTx(ctx, tx, input.IdempotencyKey); err == nil {
		if existingHash != requestHash {
			return Receipt{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	now := s.now()
	receipt := Receipt{ReceiptInput: hashInput, ReceiptID: postgresID("receipt", now), CreatedAt: now}
	payload, err := json.Marshal(receiptPayload{ReceiptInput: receipt.ReceiptInput, Retention: receipt.Retention})
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evidence_receipts
		(id, receipt_type, status, account_id, organization_id, workspace_id, project_id, task_id,
		 job_id, artifact_id, review_id, payload_json, supersedes_receipt_id, provider_request_id,
		 redacted_url, token_version, idempotency_key, request_hash, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		receipt.ReceiptID, receipt.Type, receipt.Status, receipt.AccountID, receipt.OrganizationID,
		receipt.WorkspaceID, receipt.ProjectID, receipt.TaskID, receipt.JobID, receipt.ArtifactID,
		receipt.ReviewID, string(payload), receipt.SupersedesReceiptID, "", "", "",
		input.IdempotencyKey, requestHash, receipt.CreatedAt)
	if err != nil {
		if existing, existingHash, replayErr := receiptByIdempotencyKeyTx(ctx, tx, input.IdempotencyKey); replayErr == nil {
			if existingHash != requestHash {
				return Receipt{}, ErrIdempotencyConflict
			}
			existing.Replayed = true
			return existing, nil
		}
		return Receipt{}, err
	}
	return receipt, nil
}

func receiptByIdempotencyKeyTx(ctx context.Context, tx *sql.Tx, key string) (Receipt, string, error) {
	var (
		id, receiptType, status, accountID, organizationID, workspaceID, projectID, taskID string
		jobID, artifactID, reviewID, payloadJSON, supersedesID, requestHash                string
		createdAt                                                                          time.Time
	)
	err := tx.QueryRowContext(ctx, `SELECT id, receipt_type, status, account_id, organization_id, workspace_id,
		project_id, task_id, job_id, artifact_id, review_id, payload_json, supersedes_receipt_id,
		request_hash, created_at FROM evidence_receipts WHERE idempotency_key=$1`, key).Scan(
		&id, &receiptType, &status, &accountID, &organizationID, &workspaceID, &projectID, &taskID,
		&jobID, &artifactID, &reviewID, &payloadJSON, &supersedesID, &requestHash, &createdAt)
	if err != nil {
		return Receipt{}, "", err
	}
	var stored receiptPayload
	if err = decodeStoredJSON(payloadJSON, &stored); err != nil {
		return Receipt{}, "", err
	}
	input := stored.ReceiptInput
	input.Type, input.Status = receiptType, status
	input.AccountID, input.OrganizationID, input.WorkspaceID = accountID, organizationID, workspaceID
	input.ProjectID, input.TaskID, input.JobID = projectID, taskID, jobID
	input.ArtifactID, input.ReviewID, input.SupersedesReceiptID = artifactID, reviewID, supersedesID
	return Receipt{ReceiptInput: input, ReceiptID: id, CreatedAt: createdAt, Retention: stored.Retention}, requestHash, nil
}

func (s *PostgresStore) appendReceiptRecordedEventTx(ctx context.Context, tx *sql.Tx, source *api.EventEnvelope, receipt Receipt) error {
	payloadBytes, err := protojson.Marshal(source)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payloadBytes)
	return s.appendReceiptRecordedFieldsTx(ctx, tx, receipt.ReceiptID, source.Owner, source.TenantId, source.RequestId, source.AggregateId, "sha256:"+hex.EncodeToString(digest[:]), receipt.CreatedAt)
}

func (s *PostgresStore) appendReceiptRecordedFieldsTx(ctx context.Context, tx *sql.Tx, receiptID, sourceOwner, tenantID, correlationID, ownerEvidenceReference, evidenceDigest string, occurredAt time.Time) error {
	if s.ownerEvents == nil || receiptID == "" || sourceOwner == "" || tenantID == "" || correlationID == "" || ownerEvidenceReference == "" || evidenceDigest == "" {
		return ErrInvalidReceiptInput
	}
	payload, err := protojson.Marshal(&api.ReceiptRecordedEvent{
		ReceiptId: receiptID, SourceOwner: sourceOwner,
		OwnerEvidenceReference: ownerEvidenceReference, EvidenceDigest: evidenceDigest,
	})
	if err != nil {
		return err
	}
	event := ownerstore.Event{
		ID: "evt_receipt_" + receiptID, EventType: "ledger.receipt_recorded.v1", SchemaVersion: 1,
		AggregateType: "receipt", AggregateID: receiptID, AggregateRevision: 1,
		TenantID: tenantID, CorrelationID: correlationID, Payload: payload, OccurredAt: occurredAt,
	}
	var recordedHash string
	var recordedPayload []byte
	err = tx.QueryRowContext(ctx, "SELECT payload_sha256, payload FROM ledger.outbox_events WHERE id=$1", event.ID).Scan(&recordedHash, &recordedPayload)
	if err == nil {
		if recordedHash != event.PayloadSHA256() {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.ownerEvents.AppendEvent(ctx, tx, event)
}
