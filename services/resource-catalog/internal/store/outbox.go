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

// ErrInvalidEvent reports a missing or invalid required event field.
var ErrInvalidEvent = errors.New("invalid outbox event")

// Event is the owner-local Outbox record. One row is written in the same
// transaction as the owner mutation it announces.
//
// Wire mapping follows docs/spec/v2.26/contracts/events.json `x-delivery`.
// That authoritative map binds eventId, eventType, schemaVersion, tenantId,
// aggregateId, aggregateVersion, occurredAt, requestId and payload to their
// outbox columns. It does not bind `aggregate_type`, and the v2.26 wire
// EventEnvelope carries no such field, so the owning domain's event producer
// supplies it explicitly here rather than this package inventing a vocabulary.
type Event struct {
	ID                string
	EventType         string
	SchemaVersion     int32
	AggregateType     string
	AggregateID       string
	AggregateRevision int64
	TenantID          string
	CorrelationID     string
	CausationID       string
	Payload           json.RawMessage
	OccurredAt        time.Time
}

// PayloadSHA256 returns the hex sha256 of the canonical payload bytes stored in
// the row, matching the schema CHECK `payload_sha256 ~ '^[0-9a-f]{64}$'`.
func (e Event) PayloadSHA256() string {
	sum := sha256.Sum256(e.Payload)
	return hex.EncodeToString(sum[:])
}

func (e Event) validate() error {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return fmt.Errorf("%w: id is required", ErrInvalidEvent)
	case strings.TrimSpace(e.EventType) == "":
		return fmt.Errorf("%w: event type is required", ErrInvalidEvent)
	case e.SchemaVersion <= 0:
		return fmt.Errorf("%w: schema version must be positive", ErrInvalidEvent)
	case strings.TrimSpace(e.AggregateType) == "":
		return fmt.Errorf("%w: aggregate type is required", ErrInvalidEvent)
	case strings.TrimSpace(e.AggregateID) == "":
		return fmt.Errorf("%w: aggregate id is required", ErrInvalidEvent)
	case e.AggregateRevision < 0:
		return fmt.Errorf("%w: aggregate revision must not be negative", ErrInvalidEvent)
	case strings.TrimSpace(e.CorrelationID) == "":
		return fmt.Errorf("%w: correlation id is required", ErrInvalidEvent)
	case len(e.Payload) == 0:
		return fmt.Errorf("%w: payload is required", ErrInvalidEvent)
	case e.OccurredAt.IsZero():
		return fmt.Errorf("%w: occurrence time is required", ErrInvalidEvent)
	}
	return nil
}

// AppendEvent writes the Outbox row and one delivery row per consumer owner in
// the caller's transaction. Each consumer is acknowledged independently, so a
// single consumer's success never marks the whole event delivered.
func AppendEvent(ctx context.Context, tx *sql.Tx, event Event, consumers []string) error {
	if err := event.validate(); err != nil {
		return err
	}
	var tenantID any
	if strings.TrimSpace(event.TenantID) != "" {
		tenantID = event.TenantID
	}
	var causationID any
	if strings.TrimSpace(event.CausationID) != "" {
		causationID = event.CausationID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO `+Schema+`.outbox_events
			(id, event_type, schema_version, aggregate_type, aggregate_id, aggregate_revision,
			 tenant_id, correlation_id, causation_id, payload, payload_sha256, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		event.ID, event.EventType, event.SchemaVersion, event.AggregateType, event.AggregateID,
		event.AggregateRevision, tenantID, event.CorrelationID, causationID,
		[]byte(event.Payload), event.PayloadSHA256(), event.OccurredAt.UTC()); err != nil {
		return fmt.Errorf("append resource catalog outbox event: %w", err)
	}
	for _, consumer := range consumers {
		consumer = strings.TrimSpace(consumer)
		if consumer == "" {
			return fmt.Errorf("%w: consumer owner is required", ErrInvalidEvent)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO `+Schema+`.outbox_deliveries (id, event_id, consumer_owner)
			VALUES ($1, $2, $3)`, deliveryID(event.ID, consumer), event.ID, consumer); err != nil {
			return fmt.Errorf("append resource catalog outbox delivery: %w", err)
		}
	}
	return nil
}

func deliveryID(eventID, consumer string) string {
	return "dlv_" + eventID + "_" + consumer
}

// PendingDelivery is one undelivered Outbox record for a consumer owner.
type PendingDelivery struct {
	DeliveryID   string
	EventID      string
	AttemptCount int
	Event        Event
}

// PendingDeliveries lists unacknowledged deliveries for one consumer, oldest
// first. Deliveries are per-consumer, so this never reports another consumer's
// progress.
func (s *Store) PendingDeliveries(ctx context.Context, consumer string, limit int) ([]PendingDelivery, error) {
	consumer = strings.TrimSpace(consumer)
	if consumer == "" {
		return nil, fmt.Errorf("%w: consumer owner is required", ErrInvalidEvent)
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.event_id, d.attempt_count,
		       e.id, e.event_type, e.schema_version, e.aggregate_type, e.aggregate_id,
		       e.aggregate_revision, COALESCE(e.tenant_id, ''), e.correlation_id,
		       COALESCE(e.causation_id, ''), e.payload, e.occurred_at
		FROM `+Schema+`.outbox_deliveries d
		JOIN `+Schema+`.outbox_events e ON e.id = d.event_id
		WHERE d.consumer_owner = $1 AND d.acknowledged_at IS NULL AND d.next_attempt_at <= now()
		ORDER BY d.next_attempt_at, d.id
		LIMIT $2`, consumer, limit)
	if err != nil {
		return nil, fmt.Errorf("list resource catalog pending deliveries: %w", err)
	}
	defer rows.Close()
	var pending []PendingDelivery
	for rows.Next() {
		var (
			delivery PendingDelivery
			payload  []byte
		)
		if err := rows.Scan(&delivery.DeliveryID, &delivery.EventID, &delivery.AttemptCount,
			&delivery.Event.ID, &delivery.Event.EventType, &delivery.Event.SchemaVersion,
			&delivery.Event.AggregateType, &delivery.Event.AggregateID,
			&delivery.Event.AggregateRevision, &delivery.Event.TenantID,
			&delivery.Event.CorrelationID, &delivery.Event.CausationID,
			&payload, &delivery.Event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan resource catalog pending delivery: %w", err)
		}
		delivery.Event.Payload = json.RawMessage(payload)
		pending = append(pending, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list resource catalog pending deliveries: %w", err)
	}
	return pending, nil
}

// AcknowledgeDelivery marks one consumer's delivery delivered. The Outbox row
// itself stays immutable: the runtime writer has no UPDATE on outbox_events.
func (s *Store) AcknowledgeDelivery(ctx context.Context, consumer, eventID string) error {
	consumer = strings.TrimSpace(consumer)
	eventID = strings.TrimSpace(eventID)
	if consumer == "" || eventID == "" {
		return fmt.Errorf("%w: consumer owner and event id are required", ErrInvalidEvent)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE `+Schema+`.outbox_deliveries
		SET acknowledged_at = now(), updated_at = now(),
		    lease_token = NULL, lease_until = NULL
		WHERE event_id = $1 AND consumer_owner = $2 AND acknowledged_at IS NULL`,
		eventID, consumer); err != nil {
		return fmt.Errorf("acknowledge resource catalog outbox delivery: %w", err)
	}
	return nil
}
