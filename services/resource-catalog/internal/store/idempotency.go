package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidIdempotencyInput reports a missing required idempotency field.
var ErrInvalidIdempotencyInput = errors.New("invalid idempotency input")

// ErrIdempotencyConflict reports the same idempotency key sent with a different
// normalized request body.
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request body")

// IdempotencyInput records one idempotent command result. The scope is
// tenant_scope + actor_scope + the stable API operation name + key; it is not
// the per-request Operation entity id.
type IdempotencyInput struct {
	ID             string
	TenantScope    string
	ActorScope     string
	OperationName  string
	IdempotencyKey string
	RequestSHA256  string
	ResourceID     string
	OperationID    string
	ResponseStatus int
	ResponseBody   json.RawMessage
}

// IdempotencyRecord is a stored idempotent command result.
type IdempotencyRecord struct {
	ID             string
	ResourceID     string
	OperationID    string
	RequestSHA256  string
	ResponseStatus int
	ResponseBody   json.RawMessage
	Replayed       bool
}

// HashRequestBody returns the hex sha256 of a normalized request body. The
// caller normalizes first; identical bytes must hash identically so a replayed
// command returns the same identity.
func HashRequestBody(normalized []byte) string {
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

func validateIdempotencyInput(input IdempotencyInput) error {
	switch {
	case strings.TrimSpace(input.ID) == "":
		return fmt.Errorf("%w: id is required", ErrInvalidIdempotencyInput)
	case strings.TrimSpace(input.TenantScope) == "":
		return fmt.Errorf("%w: tenant scope is required", ErrInvalidIdempotencyInput)
	case strings.TrimSpace(input.ActorScope) == "":
		return fmt.Errorf("%w: actor scope is required", ErrInvalidIdempotencyInput)
	case strings.TrimSpace(input.OperationName) == "":
		return fmt.Errorf("%w: operation name is required", ErrInvalidIdempotencyInput)
	case strings.TrimSpace(input.IdempotencyKey) == "":
		return fmt.Errorf("%w: idempotency key is required", ErrInvalidIdempotencyInput)
	case len(input.RequestSHA256) != 64:
		return fmt.Errorf("%w: request hash must be a hex sha256", ErrInvalidIdempotencyInput)
	case strings.TrimSpace(input.ResourceID) == "":
		return fmt.Errorf("%w: resource id is required", ErrInvalidIdempotencyInput)
	case input.ResponseStatus < 100 || input.ResponseStatus > 599:
		return fmt.Errorf("%w: response status must be an HTTP status", ErrInvalidIdempotencyInput)
	}
	return nil
}

// LookupIdempotency returns a previously stored result. A matching hash is a
// replay of the same command and returns the original identity; a different hash
// is a conflict, never a second write. Records are retained for the whole
// obligation lifetime and are not cleared on a short TTL.
func LookupIdempotency(ctx context.Context, tx *sql.Tx, input IdempotencyInput) (IdempotencyRecord, bool, error) {
	var (
		record IdempotencyRecord
		body   []byte
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, resource_id, COALESCE(operation_id, ''), request_sha256,
		       response_status, response_body
		FROM `+Schema+`.idempotency_records
		WHERE tenant_scope = $1 AND actor_scope = $2 AND operation_name = $3 AND idempotency_key = $4`,
		input.TenantScope, input.ActorScope, input.OperationName, input.IdempotencyKey).
		Scan(&record.ID, &record.ResourceID, &record.OperationID, &record.RequestSHA256,
			&record.ResponseStatus, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return IdempotencyRecord{}, false, fmt.Errorf("read resource catalog idempotency record: %w", err)
	}
	if record.RequestSHA256 != input.RequestSHA256 {
		return IdempotencyRecord{}, true, ErrIdempotencyConflict
	}
	record.RequestSHA256 = input.RequestSHA256
	record.ResponseBody = json.RawMessage(body)
	record.Replayed = true
	return record, true, nil
}

// RecordIdempotency stores the safe response identity for a command. It runs in
// the caller's transaction so the record and the owner's business write commit
// together. Only de-identified response bodies are permitted here.
func RecordIdempotency(ctx context.Context, tx *sql.Tx, input IdempotencyInput) error {
	if err := validateIdempotencyInput(input); err != nil {
		return err
	}
	body := input.ResponseBody
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	var operationID any
	if strings.TrimSpace(input.OperationID) != "" {
		operationID = input.OperationID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO `+Schema+`.idempotency_records
			(id, tenant_scope, actor_scope, operation_name, idempotency_key,
			 request_sha256, resource_id, operation_id, response_status, response_body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		input.ID, input.TenantScope, input.ActorScope, input.OperationName,
		input.IdempotencyKey, input.RequestSHA256, input.ResourceID, operationID,
		input.ResponseStatus, []byte(body)); err != nil {
		return fmt.Errorf("record resource catalog idempotency: %w", err)
	}
	return nil
}
