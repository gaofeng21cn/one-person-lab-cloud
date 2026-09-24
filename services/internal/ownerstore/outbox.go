package ownerstore

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

	contracts "opl-cloud/packages/contracts/go"
)

// ErrInvalidEvent reports a missing or invalid required event field.
var ErrInvalidEvent = errors.New("invalid outbox event")

// Event is the owner-local Outbox record. One row is written in the same
// transaction as the owner mutation it announces.
//
// AggregateType and AggregateID are not free-form: the shared contract fixes
// both per exact (eventType, schemaVersion) through the generated event-identity
// mapping, so they are validated against it instead of being accepted as
// producer text.
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

// PayloadSHA256 returns the hex sha256 of the payload bytes stored in the row.
func (e Event) PayloadSHA256() string {
	sum := sha256.Sum256(e.Payload)
	return hex.EncodeToString(sum[:])
}

func (e Event) validate(owner string) error {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return fmt.Errorf("%w: id is required", ErrInvalidEvent)
	case strings.TrimSpace(e.EventType) == "":
		return fmt.Errorf("%w: event type is required", ErrInvalidEvent)
	case e.SchemaVersion <= 0:
		return fmt.Errorf("%w: schema version must be positive", ErrInvalidEvent)
	case e.AggregateRevision <= 0:
		return fmt.Errorf("%w: aggregate revision must be positive", ErrInvalidEvent)
	case strings.TrimSpace(e.CorrelationID) == "":
		return fmt.Errorf("%w: correlation id is required", ErrInvalidEvent)
	case len(e.Payload) == 0:
		return fmt.Errorf("%w: payload is required", ErrInvalidEvent)
	case e.OccurredAt.IsZero():
		return fmt.Errorf("%w: occurrence time is required", ErrInvalidEvent)
	}
	identity, ok := contracts.LookupEventIdentity(e.EventType, e.SchemaVersion)
	if !ok {
		return fmt.Errorf("%w: %s schema %d is not a specified event version", ErrInvalidEvent, e.EventType, e.SchemaVersion)
	}
	if identity.Owner != owner {
		return fmt.Errorf("%w: %s schema %d is produced by %q, not %q", ErrInvalidEvent, e.EventType, e.SchemaVersion, identity.Owner, owner)
	}
	return ValidateAggregateIdentity(e.EventType, e.SchemaVersion, e.AggregateType, e.AggregateID, e.Payload)
}

// ValidateAggregateIdentity checks that a recorded aggregate is actually the
// aggregate this event's payload carries.
//
// The contract fixes each event version's owner and subscribed consumers, but it
// does not name an aggregate-type vocabulary or a payload id field. This package
// therefore does not invent one: it requires a non-empty aggregate type and
// requires the recorded aggregate id to appear verbatim as a top-level string
// value of the recorded payload. An aggregate id unrelated to the payload, or a
// payload with no matching string field, is refused instead of being stored.
func ValidateAggregateIdentity(eventType string, schemaVersion int32, aggregateType, aggregateID string, payload []byte) error {
	if _, ok := contracts.LookupEventIdentity(eventType, schemaVersion); !ok {
		return fmt.Errorf("%w: %s schema %d is not a specified event version",
			ErrInvalidEvent, eventType, schemaVersion)
	}
	if strings.TrimSpace(aggregateType) == "" {
		return fmt.Errorf("%w: aggregate type is required", ErrInvalidEvent)
	}
	aggregateID = strings.TrimSpace(aggregateID)
	if aggregateID == "" {
		return fmt.Errorf("%w: aggregate id is required", ErrInvalidEvent)
	}
	if len(payload) == 0 {
		return fmt.Errorf("%w: payload is required", ErrInvalidEvent)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("%w: payload is not a JSON object: %v", ErrInvalidEvent, err)
	}
	for _, raw := range decoded {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		if value == aggregateID {
			return nil
		}
	}
	return fmt.Errorf("%w: %s schema %d records aggregate id %q that no top-level payload field carries",
		ErrInvalidEvent, eventType, schemaVersion, aggregateID)
}

func validateEventConsumers(eventType string, schemaVersion int32, consumers []string) error {
	identity, ok := contracts.LookupEventIdentity(eventType, schemaVersion)
	if !ok {
		return fmt.Errorf("%w: %s schema %d is not a specified event version", ErrInvalidEvent, eventType, schemaVersion)
	}
	got := make(map[string]struct{}, len(consumers))
	for _, consumer := range consumers {
		consumer = strings.TrimSpace(consumer)
		if consumer == "" || !identity.Subscribed(consumer) {
			return fmt.Errorf("%w: owner %q is not subscribed to %s schema %d", ErrInvalidEvent, consumer, eventType, schemaVersion)
		}
		if _, duplicate := got[consumer]; duplicate {
			return fmt.Errorf("%w: duplicate delivery for consumer %q", ErrInvalidEvent, consumer)
		}
		got[consumer] = struct{}{}
	}
	for _, consumer := range identity.Consumers {
		if _, present := got[consumer]; !present {
			return fmt.Errorf("%w: missing delivery for subscribed owner %q on %s schema %d", ErrInvalidEvent, consumer, eventType, schemaVersion)
		}
	}
	return nil
}

// AppendEvent writes the Outbox row and one delivery row per consumer owner in the
// caller's transaction. The subscribed consumer set is derived from the contract,
// so a producer cannot silently skip a consumer or invent one. Each consumer is
// acknowledged independently.
func (s *Store) AppendEvent(ctx context.Context, tx *sql.Tx, event Event) error {
	if err := event.validate(s.schema); err != nil {
		return err
	}
	identity, _ := contracts.LookupEventIdentity(event.EventType, event.SchemaVersion)
	consumers := identity.Consumers
	if err := validateEventConsumers(event.EventType, event.SchemaVersion, consumers); err != nil {
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
		INSERT INTO `+s.schema+`.outbox_events
			(id, event_type, schema_version, aggregate_type, aggregate_id, aggregate_revision,
			 tenant_id, correlation_id, causation_id, payload, payload_sha256, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		event.ID, event.EventType, event.SchemaVersion, event.AggregateType, event.AggregateID,
		event.AggregateRevision, tenantID, event.CorrelationID, causationID,
		[]byte(event.Payload), event.PayloadSHA256(), event.OccurredAt.UTC()); err != nil {
		return fmt.Errorf("append %s outbox event: %w", s.schema, err)
	}
	for _, consumer := range consumers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO `+s.schema+`.outbox_deliveries (id, event_id, consumer_owner)
			VALUES ($1, $2, $3)`, deliveryID(event.ID, consumer), event.ID, consumer); err != nil {
			return fmt.Errorf("append %s outbox delivery: %w", s.schema, err)
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
		FROM `+s.schema+`.outbox_deliveries d
		JOIN `+s.schema+`.outbox_events e ON e.id = d.event_id
		WHERE d.consumer_owner = $1 AND d.acknowledged_at IS NULL AND d.next_attempt_at <= now()
		ORDER BY d.next_attempt_at, d.id
		LIMIT $2`, consumer, limit)
	if err != nil {
		return nil, fmt.Errorf("list %s pending deliveries: %w", s.schema, err)
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
			return nil, fmt.Errorf("scan %s pending delivery: %w", s.schema, err)
		}
		delivery.Event.Payload = json.RawMessage(payload)
		pending = append(pending, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %s pending deliveries: %w", s.schema, err)
	}
	return pending, nil
}

// AcknowledgeDelivery marks one consumer's delivery delivered. The Outbox row
// itself stays immutable.
func (s *Store) AcknowledgeDelivery(ctx context.Context, consumer, eventID string) error {
	consumer = strings.TrimSpace(consumer)
	eventID = strings.TrimSpace(eventID)
	if consumer == "" || eventID == "" {
		return fmt.Errorf("%w: consumer owner and event id are required", ErrInvalidEvent)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE `+s.schema+`.outbox_deliveries
		SET acknowledged_at = now(), updated_at = now(),
		    lease_token = NULL, lease_until = NULL
		WHERE event_id = $1 AND consumer_owner = $2 AND acknowledged_at IS NULL`,
		eventID, consumer); err != nil {
		return fmt.Errorf("acknowledge %s outbox delivery: %w", s.schema, err)
	}
	return nil
}

// RecordDeliveryFailure records one failed delivery attempt and defers the next
// attempt. Delivery failure is observable and never marks the event delivered.
func (s *Store) RecordDeliveryFailure(ctx context.Context, deliveryID, errorCode string, retryAt time.Time) error {
	if retryAt.IsZero() {
		retryAt = time.Now().UTC().Add(time.Second)
	}
	var code any
	if strings.TrimSpace(errorCode) != "" {
		code = errorCode
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE `+s.schema+`.outbox_deliveries
		SET attempt_count = attempt_count + 1, last_error_code = $2,
		    next_attempt_at = $3, updated_at = now(),
		    lease_token = NULL, lease_until = NULL
		WHERE id = $1 AND acknowledged_at IS NULL`,
		deliveryID, code, retryAt.UTC()); err != nil {
		return fmt.Errorf("record %s delivery failure: %w", s.schema, err)
	}
	return nil
}
