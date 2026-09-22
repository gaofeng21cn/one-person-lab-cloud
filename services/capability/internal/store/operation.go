package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Operation status values. They are the v2.26 OperationStatus vocabulary; the
// database CHECK constraint enforces the same set, and no second vocabulary is
// defined here.
const (
	OperationAccepted        = "accepted"
	OperationRunning         = "running"
	OperationAwaitingConfirm = "awaiting_confirmation"
	OperationSucceeded       = "succeeded"
	OperationFailed          = "failed"
	OperationNeedsAttention  = "needs_attention"
	OperationCancelled       = "cancelled"
)

// Operation observation results distinguish an unknown external outcome from a
// failure. An unknown result is never treated as success.
const (
	ObservationConfirmed = "confirmed"
	ObservationRejected  = "rejected"
	ObservationUnknown   = "unknown"
)

var (
	// ErrOperationNotFound reports an unknown operation in this owner's database.
	ErrOperationNotFound = errors.New("capability operation not found")
	// ErrOperationTerminal reports an attempt to change a terminal operation.
	ErrOperationTerminal = errors.New("capability operation is already terminal")
	// ErrInvalidOperationInput reports a missing or invalid required field.
	ErrInvalidOperationInput = errors.New("invalid capability operation input")
)

// Operation is the owner-local operation record for the capability domain. The
// stage and kind values come from the typed v2.26 contract; this package stores
// them and never invents an alternative vocabulary.
type Operation struct {
	ID            string
	TenantID      string
	ActorID       string
	Kind          string
	ResourceID    string
	Status        string
	Stage         string
	ErrorCode     string
	Observation   string
	RequestID     string
	AcceptedInput json.RawMessage
	Result        json.RawMessage

	WorkerLeaseToken string
	WorkerLeaseUntil time.Time
	StartedAt        time.Time
	CompletedAt      time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// OperationInput creates an operation in the same transaction as its durable
// command. acceptedInput is stored de-identified; it must not carry raw
// passwords, session cookies, recovery tokens, signed upload URLs, or storage
// credentials.
type OperationInput struct {
	ID            string
	TenantID      string
	ActorID       string
	Kind          string
	ResourceID    string
	Stage         string
	RequestID     string
	AcceptedInput json.RawMessage
}

// Terminal reports whether the status ends the operation.
func (o Operation) Terminal() bool {
	switch o.Status {
	case OperationSucceeded, OperationFailed, OperationCancelled:
		return true
	default:
		return false
	}
}

func validateOperationInput(input OperationInput) error {
	switch {
	case strings.TrimSpace(input.ID) == "":
		return fmt.Errorf("%w: id is required", ErrInvalidOperationInput)
	case strings.TrimSpace(input.ActorID) == "":
		return fmt.Errorf("%w: actor is required", ErrInvalidOperationInput)
	case strings.TrimSpace(input.Kind) == "":
		return fmt.Errorf("%w: kind is required", ErrInvalidOperationInput)
	case strings.TrimSpace(input.ResourceID) == "":
		return fmt.Errorf("%w: resource is required", ErrInvalidOperationInput)
	case strings.TrimSpace(input.Stage) == "":
		return fmt.Errorf("%w: stage is required", ErrInvalidOperationInput)
	case strings.TrimSpace(input.RequestID) == "":
		return fmt.Errorf("%w: request id is required", ErrInvalidOperationInput)
	}
	return nil
}

// CreateOperation records an operation. It is expected to run in the caller's
// transaction, which also carries the durable command and any Outbox event.
func CreateOperation(ctx context.Context, tx *sql.Tx, input OperationInput) (Operation, error) {
	if err := validateOperationInput(input); err != nil {
		return Operation{}, err
	}
	accepted := input.AcceptedInput
	if len(accepted) == 0 {
		accepted = json.RawMessage(`{}`)
	}
	var tenantID any
	if strings.TrimSpace(input.TenantID) != "" {
		tenantID = input.TenantID
	}
	var operation Operation
	err := tx.QueryRowContext(ctx, `
		INSERT INTO `+Schema+`.operations
			(id, tenant_id, actor_id, kind, resource_id, status, stage, request_id, accepted_input)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, COALESCE(tenant_id, ''), actor_id, kind, resource_id, status, stage,
		          COALESCE(error_code, ''), COALESCE(observation_result, ''), request_id,
		          accepted_input, COALESCE(result, '{}'::jsonb), created_at, updated_at`,
		input.ID, tenantID, input.ActorID, input.Kind, input.ResourceID,
		OperationAccepted, input.Stage, input.RequestID, []byte(accepted)).
		Scan(&operation.ID, &operation.TenantID, &operation.ActorID, &operation.Kind,
			&operation.ResourceID, &operation.Status, &operation.Stage, &operation.ErrorCode,
			&operation.Observation, &operation.RequestID, &operation.AcceptedInput,
			&operation.Result, &operation.CreatedAt, &operation.UpdatedAt)
	if err != nil {
		return Operation{}, fmt.Errorf("create capability operation: %w", err)
	}
	return operation, nil
}

// ReadOperation loads one operation owned by this service. Operations are
// per-owner records; a foreign owner's operation is not visible here.
func (s *Store) ReadOperation(ctx context.Context, operationID string) (Operation, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return Operation{}, fmt.Errorf("%w: operation id is required", ErrInvalidOperationInput)
	}
	var (
		operation   Operation
		errorCode   sql.NullString
		observation sql.NullString
		leaseToken  sql.NullString
		leaseUntil  sql.NullTime
		startedAt   sql.NullTime
		completedAt sql.NullTime
		result      []byte
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(tenant_id, ''), actor_id, kind, resource_id, status, stage,
		       error_code, observation_result, request_id, accepted_input, result,
		       worker_lease_token, worker_lease_until, started_at, completed_at,
		       created_at, updated_at
		FROM `+Schema+`.operations WHERE id = $1`, operationID).
		Scan(&operation.ID, &operation.TenantID, &operation.ActorID, &operation.Kind,
			&operation.ResourceID, &operation.Status, &operation.Stage, &errorCode,
			&observation, &operation.RequestID, &operation.AcceptedInput, &result,
			&leaseToken, &leaseUntil, &startedAt, &completedAt,
			&operation.CreatedAt, &operation.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, ErrOperationNotFound
	}
	if err != nil {
		return Operation{}, fmt.Errorf("read capability operation: %w", err)
	}
	operation.ErrorCode = errorCode.String
	operation.Observation = observation.String
	operation.WorkerLeaseToken = leaseToken.String
	operation.WorkerLeaseUntil = leaseUntil.Time
	operation.StartedAt = startedAt.Time
	operation.CompletedAt = completedAt.Time
	operation.Result = json.RawMessage(result)
	if len(operation.Result) == 0 {
		operation.Result = json.RawMessage(`{}`)
	}
	return operation, nil
}

// ReconcileResult is the outcome of an owner readback-driven reconcile. An
// unknown external result stays non-terminal: it is never finalized on a guess.
type ReconcileResult struct {
	Operation    Operation
	Changed      bool
	StillUnknown bool
}

// ReconcileOperation re-reads the owner's own state for an operation and, when
// the operation is already terminal, returns it unchanged. The capability owner does
// not invent a success for an unknown provider result.
func (s *Store) ReconcileOperation(ctx context.Context, operationID string) (ReconcileResult, error) {
	operation, err := s.ReadOperation(ctx, operationID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if operation.Terminal() {
		return ReconcileResult{Operation: operation}, nil
	}
	return ReconcileResult{
		Operation:    operation,
		StillUnknown: operation.Observation == ObservationUnknown || operation.Observation == "",
	}, nil
}

// CompleteOperation transitions a non-terminal operation to a terminal status.
// completedAt must be set for a succeeded operation, matching the schema CHECK.
func (s *Store) CompleteOperation(ctx context.Context, operationID, status, stage, errorCode, observation string, result json.RawMessage, completedAt time.Time) (Operation, error) {
	switch status {
	case OperationSucceeded, OperationFailed, OperationCancelled:
	default:
		return Operation{}, fmt.Errorf("%w: %q is not a terminal status", ErrInvalidOperationInput, status)
	}
	if status == OperationSucceeded && completedAt.IsZero() {
		return Operation{}, fmt.Errorf("%w: a succeeded operation needs a completion time", ErrInvalidOperationInput)
	}
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	var nullObservation any
	if strings.TrimSpace(observation) != "" {
		nullObservation = observation
	}
	var nullError any
	if strings.TrimSpace(errorCode) != "" {
		nullError = errorCode
	}
	var nullCompleted any
	if !completedAt.IsZero() {
		nullCompleted = completedAt.UTC()
	}
	tag, err := s.db.ExecContext(ctx, `
		UPDATE `+Schema+`.operations
		SET status = $2, stage = $3, error_code = $4, observation_result = $5,
		    result = $6, completed_at = $7, updated_at = now()
		WHERE id = $1 AND status NOT IN ('succeeded','failed','cancelled')`,
		operationID, status, stage, nullError, nullObservation, []byte(result), nullCompleted)
	if err != nil {
		return Operation{}, fmt.Errorf("complete capability operation: %w", err)
	}
	rows, err := tag.RowsAffected()
	if err != nil {
		return Operation{}, fmt.Errorf("complete capability operation: %w", err)
	}
	if rows == 0 {
		// Either the operation does not exist or it is already terminal.
		if _, readErr := s.ReadOperation(ctx, operationID); errors.Is(readErr, ErrOperationNotFound) {
			return Operation{}, ErrOperationNotFound
		}
		return Operation{}, ErrOperationTerminal
	}
	return s.ReadOperation(ctx, operationID)
}
