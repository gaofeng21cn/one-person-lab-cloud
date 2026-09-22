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
	"time"
)

// ErrInvalidInboxEvent reports a missing or invalid required field on an
// inbound event.
var ErrInvalidInboxEvent = errors.New("invalid inbox event")

// InboxDecision is the idempotency decision for one inbound event.
type InboxDecision string

const (
	// InboxCommit applies the event for the first time.
	InboxCommit InboxDecision = "commit"
	// InboxDuplicate acknowledges an already-applied event without reapplying.
	InboxDuplicate InboxDecision = "duplicate"
	// InboxConflict rejects the same producer/event id carrying different bytes.
	InboxConflict InboxDecision = "conflict"
)

// DecideInbox is the pure deduplication rule for inbound events: a unique
// (source_owner, source_event_id) is applied once, a repeat with identical bytes
// is acknowledged without reapplication, and the same identity with different
// bytes is rejected rather than silently overwriting the first fact.
func DecideInbox(existingHash string, found bool, incomingHash string) InboxDecision {
	switch {
	case !found:
		return InboxCommit
	case existingHash == incomingHash:
		return InboxDuplicate
	default:
		return InboxConflict
	}
}

// InboundEvent is one event a consumer owner has accepted for processing.
//
// AggregateType follows the same rule as the Outbox: the v2.26 wire
// EventEnvelope does not carry it, so the consuming owner supplies the
// aggregate identity it is dispatching for rather than this package guessing.
type InboundEvent struct {
	ID                string
	SourceOwner       string
	SourceEventID     string
	EventType         string
	SchemaVersion     int32
	AggregateType     string
	AggregateID       string
	AggregateRevision int64
	Payload           json.RawMessage
}

// PayloadSHA256 returns the hex sha256 of the payload bytes.
func (e InboundEvent) PayloadSHA256() string {
	sum := sha256.Sum256(e.Payload)
	return hex.EncodeToString(sum[:])
}

func (e InboundEvent) validate() error {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return fmt.Errorf("%w: id is required", ErrInvalidInboxEvent)
	case strings.TrimSpace(e.SourceOwner) == "":
		return fmt.Errorf("%w: source owner is required", ErrInvalidInboxEvent)
	case strings.TrimSpace(e.SourceEventID) == "":
		return fmt.Errorf("%w: source event id is required", ErrInvalidInboxEvent)
	case strings.TrimSpace(e.EventType) == "":
		return fmt.Errorf("%w: event type is required", ErrInvalidInboxEvent)
	case e.SchemaVersion <= 0:
		return fmt.Errorf("%w: schema version must be positive", ErrInvalidInboxEvent)
	case strings.TrimSpace(e.AggregateType) == "":
		return fmt.Errorf("%w: aggregate type is required", ErrInvalidInboxEvent)
	case strings.TrimSpace(e.AggregateID) == "":
		return fmt.Errorf("%w: aggregate id is required", ErrInvalidInboxEvent)
	case e.AggregateRevision < 0:
		return fmt.Errorf("%w: aggregate revision must not be negative", ErrInvalidInboxEvent)
	case len(e.Payload) == 0:
		return fmt.Errorf("%w: payload is required", ErrInvalidInboxEvent)
	}
	return nil
}

// InboxResult reports what the consumer did with the event.
type InboxResult struct {
	Decision InboxDecision
	Applied  bool
	Reason   string
}

// DeliverInbox records an inbound event in the consumer's own database. The
// deduplication row, the owner's business change, processed_at, and any
// follow-up Outbox row belong to the same transaction; this method therefore
// runs in the caller's transaction.
func DeliverInbox(ctx context.Context, tx *sql.Tx, event InboundEvent, receivedAt time.Time) (InboxResult, error) {
	if err := event.validate(); err != nil {
		return InboxResult{}, err
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	incoming := event.PayloadSHA256()

	var existingHash string
	err := tx.QueryRowContext(ctx, `
		SELECT payload_sha256 FROM `+Schema+`.inbox_events
		WHERE source_owner = $1 AND source_event_id = $2`,
		event.SourceOwner, event.SourceEventID).Scan(&existingHash)
	found := true
	if errors.Is(err, sql.ErrNoRows) {
		found = false
	} else if err != nil {
		return InboxResult{}, fmt.Errorf("read capability inbox event: %w", err)
	}

	switch DecideInbox(existingHash, found, incoming) {
	case InboxDuplicate:
		return InboxResult{Decision: InboxDuplicate, Reason: "already applied"}, nil
	case InboxConflict:
		return InboxResult{
			Decision: InboxConflict,
			Reason:   "source owner and event id already recorded with different payload",
		}, nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO `+Schema+`.inbox_events
			(id, source_owner, source_event_id, event_type, schema_version, aggregate_type,
			 aggregate_id, aggregate_revision, payload_sha256, payload, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		event.ID, event.SourceOwner, event.SourceEventID, event.EventType, event.SchemaVersion,
		event.AggregateType, event.AggregateID, event.AggregateRevision, incoming,
		[]byte(event.Payload), receivedAt.UTC()); err != nil {
		return InboxResult{}, fmt.Errorf("record capability inbox event: %w", err)
	}
	return InboxResult{Decision: InboxCommit, Applied: true}, nil
}

// MarkInboxProcessed records the consumer's processing outcome in the same
// owner database. A non-terminal or failed outcome keeps processed_at unset so
// the event remains visible as pending.
func MarkInboxProcessed(ctx context.Context, tx *sql.Tx, sourceOwner, sourceEventID, resultResourceID, errorCode string, processedAt time.Time) error {
	if processedAt.IsZero() {
		processedAt = time.Now().UTC()
	}
	var resultID any
	if strings.TrimSpace(resultResourceID) != "" {
		resultID = resultResourceID
	}
	var code any
	if strings.TrimSpace(errorCode) != "" {
		code = errorCode
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE `+Schema+`.inbox_events
		SET processed_at = $3, result_resource_id = $4, error_code = $5
		WHERE source_owner = $1 AND source_event_id = $2 AND processed_at IS NULL`,
		sourceOwner, sourceEventID, processedAt.UTC(), resultID, code); err != nil {
		return fmt.Errorf("mark capability inbox event processed: %w", err)
	}
	return nil
}
