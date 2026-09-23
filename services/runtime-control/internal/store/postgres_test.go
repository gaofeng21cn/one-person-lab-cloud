package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/lib/pq"
)

// The role and database names are the v2.26 owner names; they are not
// configurable test fixtures.
const (
	runtimeControlOwnerRole  = "opl_runtime_control_owner"
	runtimeControlWriterRole = "opl_runtime_control_writer"
	runtimeControlDatabase   = "opl_runtime_control"
)

// runtimeControlTestStore mirrors the v2.26 specification harness
// (docs/spec/v2.26/checks/validate_cross_domain.py, Database.setup): the owner
// NOLOGIN role, the writer NOLOGIN role and the owner database are created
// idempotently, then the owner's DDL block is applied on a connection to that
// database. It skips unless OPL_POSTGRES_TESTS=1.
func runtimeControlTestStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("OPL_POSTGRES_TESTS") != "1" {
		t.Skip("OPL_POSTGRES_TESTS is not 1")
	}

	admin, err := sql.Open("postgres", adminDatabaseURL())
	if err != nil {
		t.Fatalf("open administrative connection: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		t.Fatalf("connect administrative connection: %v", err)
	}
	if err := ensureOwnerRolesAndDatabase(ctx, admin); err != nil {
		_ = admin.Close()
		t.Fatalf("prepare owner roles and database: %v", err)
	}
	if err := admin.Close(); err != nil {
		t.Fatalf("close administrative connection: %v", err)
	}

	db, err := sql.Open("postgres", ownerDatabaseURL())
	if err != nil {
		t.Fatalf("open %s database: %v", runtimeControlDatabase, err)
	}
	db.SetMaxOpenConns(5)
	t.Cleanup(func() { _ = db.Close() })

	runtimeControlStore := New(db)
	if err := runtimeControlStore.Install(ctx); err != nil {
		t.Fatalf("install runtime control schema: %v", err)
	}
	if err := runtimeControlStore.Ready(ctx); err != nil {
		t.Fatalf("ready check against %s: %v", runtimeControlDatabase, err)
	}
	return runtimeControlStore
}

// The gated PostgreSQL lane provides PGHOST/PGPORT/PGUSER/PGDATABASE with TLS
// disabled, the same convention services/build uses. The fixture connects to the
// maintenance database first because the owner DDL guards its own database name.
func adminDatabaseURL() string {
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
		url.QueryEscape(testEnvOr("PGUSER", "postgres")),
		testEnvOr("PGHOST", "127.0.0.1"),
		testEnvOr("PGPORT", "5432"),
		testEnvOr("PGDATABASE", "postgres"))
}

func testEnvOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func ownerDatabaseURL() string {
	raw := adminDatabaseURL()
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Del("dbname")
		parsed.RawQuery = query.Encode()
		parsed.Path = "/" + runtimeControlDatabase
		return parsed.String()
	}
	return raw + " dbname=" + runtimeControlDatabase
}

// ensureOwnerRolesAndDatabase creates the owner's NOLOGIN roles and database if
// they are absent. Re-running it must not fail: this is the same idempotent
// preparation the specification harness performs before applying every owner
// DDL block.
func ensureOwnerRolesAndDatabase(ctx context.Context, admin *sql.DB) error {
	for _, role := range []string{runtimeControlOwnerRole, runtimeControlWriterRole} {
		statement := `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ` +
			pq.QuoteLiteral(role) + `) THEN CREATE ROLE ` + pq.QuoteIdentifier(role) + ` NOLOGIN; END IF; END $$;`
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create role %s: %w", role, err)
		}
	}

	var exists bool
	if err := admin.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", runtimeControlDatabase).Scan(&exists); err != nil {
		return fmt.Errorf("inspect database %s: %w", runtimeControlDatabase, err)
	}
	if exists {
		return nil
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(runtimeControlDatabase)); err != nil {
		return fmt.Errorf("create database %s: %w", runtimeControlDatabase, err)
	}
	return nil
}

func testSuffix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func TestPostgresOperationRoundTripAndTerminalTransition(t *testing.T) {
	runtimeControlStore := runtimeControlTestStore(t)
	ctx := context.Background()
	suffix := testSuffix(t)
	operationID := "op-" + suffix

	tx, err := runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin operation transaction: %v", err)
	}
	created, err := CreateOperation(ctx, tx, OperationInput{
		ID:            operationID,
		TenantID:      "tenant-" + suffix,
		ActorID:       "actor-" + suffix,
		Kind:          "runtime_deploy",
		ResourceID:    "rt-" + suffix,
		Stage:         "runtime",
		RequestID:     "req-" + suffix,
		AcceptedInput: json.RawMessage(`{"runtimeInstanceId":"rt-` + suffix + `"}`),
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("create operation: %v", err)
	}
	if created.Status != OperationAccepted {
		t.Fatalf("created status = %q, want %q", created.Status, OperationAccepted)
	}
	if created.Terminal() {
		t.Fatal("a newly accepted operation is not terminal")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit operation transaction: %v", err)
	}

	read, err := runtimeControlStore.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatalf("read operation: %v", err)
	}
	if read.ID != operationID || read.Kind != "runtime_deploy" || read.Stage != "runtime" ||
		read.ResourceID != "rt-"+suffix || read.TenantID != "tenant-"+suffix || read.ActorID != "actor-"+suffix {
		t.Fatalf("operation round-trip mismatch: %#v", read)
	}
	if string(read.AcceptedInput) == "" || string(read.AcceptedInput) == "null" {
		t.Fatalf("accepted input did not round-trip: %q", read.AcceptedInput)
	}
	if read.ErrorCode != "" || read.Observation != "" {
		t.Fatalf("an accepted operation carries no outcome yet: %#v", read)
	}

	// An unknown external result stays non-terminal and is never finalized on a
	// guess.
	reconciled, err := runtimeControlStore.ReconcileOperation(ctx, operationID)
	if err != nil {
		t.Fatalf("reconcile operation: %v", err)
	}
	if reconciled.Changed || !reconciled.StillUnknown || reconciled.Operation.Status != OperationAccepted {
		t.Fatalf("unconfirmed operation must stay unknown and unchanged: %#v", reconciled)
	}

	if _, err := runtimeControlStore.CompleteOperation(ctx, operationID, OperationSucceeded, "runtime", "", "", json.RawMessage(`{}`), time.Time{}); !errors.Is(err, ErrInvalidOperationInput) {
		t.Fatalf("succeeded without a completion time must be rejected, got %v", err)
	}
	if _, err := runtimeControlStore.CompleteOperation(ctx, operationID, OperationRunning, "runtime", "", "", nil, time.Time{}); !errors.Is(err, ErrInvalidOperationInput) {
		t.Fatalf("a non-terminal status must not complete an operation, got %v", err)
	}

	failed, err := runtimeControlStore.CompleteOperation(ctx, operationID, OperationFailed, "readback",
		"external_outcome_unknown", ObservationUnknown, json.RawMessage(`{"reason":"timeout"}`), time.Time{})
	if err != nil {
		t.Fatalf("complete operation: %v", err)
	}
	if !failed.Terminal() || failed.Observation != ObservationUnknown || failed.ErrorCode != "external_outcome_unknown" {
		t.Fatalf("terminal transition mismatch: %#v", failed)
	}
	if _, err := runtimeControlStore.CompleteOperation(ctx, operationID, OperationSucceeded, "runtime", "", ObservationConfirmed, nil, time.Now().UTC()); !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("a terminal operation must not be rewritten, got %v", err)
	}
	if _, err := runtimeControlStore.ReadOperation(ctx, "missing-"+suffix); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("unknown operation must be not found, got %v", err)
	}
}

func TestPostgresOutboxAppendDeliversPerConsumer(t *testing.T) {
	runtimeControlStore := runtimeControlTestStore(t)
	ctx := context.Background()
	suffix := testSuffix(t)
	eventID := "evt-" + suffix

	tx, err := runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin outbox transaction: %v", err)
	}
	event := Event{
		ID:                eventID,
		EventType:         "runtime.readiness_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "runtime_instance",
		AggregateID:       "rt-" + suffix,
		AggregateRevision: 1,
		TenantID:          "tenant-" + suffix,
		CorrelationID:     "req-" + suffix,
		Payload:           json.RawMessage(`{"runtimeInstanceId":"rt-` + suffix + `"}`),
		OccurredAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := AppendEvent(ctx, tx, event, []string{"workspace", "ledger"}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("append outbox event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit outbox transaction: %v", err)
	}

	// A rejected consumer list aborts the whole transaction: the owner mutation
	// and its outbox row are one unit, never a partial write.
	badID := "evt-bad-" + suffix
	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin rejected outbox transaction: %v", err)
	}
	bad := event
	bad.ID = badID
	// A distinct aggregate identity keeps the Outbox row itself valid, so the
	// rejection has to come from the empty consumer owner. Its payload carries
	// exactly the id its aggregate columns record, as the specification's fixed
	// aggregate identity requires.
	bad.AggregateID = "rt-bad-" + suffix
	bad.AggregateRevision = 2
	bad.Payload = json.RawMessage(`{"runtimeInstanceId":"rt-bad-` + suffix + `"}`)
	if err := AppendEvent(ctx, tx, bad, []string{"workspace", "  "}); !errors.Is(err, ErrInvalidEvent) {
		_ = tx.Rollback()
		t.Fatalf("an empty consumer owner must be rejected, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback rejected outbox transaction: %v", err)
	}
	var rolledBack int
	if err := runtimeControlStore.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM runtime_control.outbox_events WHERE id = $1", badID).Scan(&rolledBack); err != nil {
		t.Fatalf("read rolled back outbox event: %v", err)
	}
	if rolledBack != 0 {
		t.Fatalf("a rolled back transaction left %d outbox rows", rolledBack)
	}

	pending, err := runtimeControlStore.PendingDeliveries(ctx, "workspace", 10)
	if err != nil {
		t.Fatalf("list workspace deliveries: %v", err)
	}
	delivered := deliveredEvent(pending, eventID)
	if delivered == nil {
		t.Fatalf("workspace delivery for %s is missing: %#v", eventID, pending)
	}
	if delivered.Event.EventType != event.EventType || delivered.Event.AggregateType != event.AggregateType ||
		delivered.Event.AggregateID != event.AggregateID || delivered.Event.AggregateRevision != event.AggregateRevision ||
		delivered.Event.TenantID != event.TenantID || delivered.Event.CorrelationID != event.CorrelationID {
		t.Fatalf("outbox round-trip mismatch: %#v", delivered.Event)
	}
	if string(delivered.Event.Payload) == "" || string(delivered.Event.Payload) == "null" {
		t.Fatalf("outbox payload did not round-trip: %q", delivered.Event.Payload)
	}

	if err := runtimeControlStore.AcknowledgeDelivery(ctx, "workspace", eventID); err != nil {
		t.Fatalf("acknowledge workspace delivery: %v", err)
	}
	pending, err = runtimeControlStore.PendingDeliveries(ctx, "workspace", 10)
	if err != nil {
		t.Fatalf("list acknowledged workspace deliveries: %v", err)
	}
	if deliveredEvent(pending, eventID) != nil {
		t.Fatal("one consumer's acknowledgement must clear that consumer's delivery")
	}
	pending, err = runtimeControlStore.PendingDeliveries(ctx, "ledger", 10)
	if err != nil {
		t.Fatalf("list ledger deliveries: %v", err)
	}
	if deliveredEvent(pending, eventID) == nil {
		t.Fatal("one consumer's acknowledgement must not clear another consumer's delivery")
	}

	if err := runtimeControlStore.AcknowledgeDelivery(ctx, "ledger", eventID); err != nil {
		t.Fatalf("acknowledge ledger delivery: %v", err)
	}
	pending, err = runtimeControlStore.PendingDeliveries(ctx, "ledger", 10)
	if err != nil {
		t.Fatalf("list acknowledged ledger deliveries: %v", err)
	}
	if deliveredEvent(pending, eventID) != nil {
		t.Fatal("acknowledgement must be durable for the second consumer")
	}
	if _, err := runtimeControlStore.PendingDeliveries(ctx, "  ", 10); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("an empty consumer owner must be rejected, got %v", err)
	}
	if err := runtimeControlStore.AcknowledgeDelivery(ctx, "", eventID); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("an empty consumer owner must not acknowledge, got %v", err)
	}
}

// sameJSONValue compares two JSON documents by value: jsonb stores the same
// document as the caller sent, not the caller's whitespace.
func sameJSONValue(t *testing.T, actual, expected []byte) bool {
	t.Helper()
	var actualValue, expectedValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("decode stored JSON %q: %v", actual, err)
	}
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		t.Fatalf("decode expected JSON %q: %v", expected, err)
	}
	return reflect.DeepEqual(actualValue, expectedValue)
}

func deliveredEvent(pending []PendingDelivery, eventID string) *PendingDelivery {
	for index := range pending {
		if pending[index].EventID == eventID {
			return &pending[index]
		}
	}
	return nil
}

func TestPostgresInboxDuplicateAndConflict(t *testing.T) {
	runtimeControlStore := runtimeControlTestStore(t)
	ctx := context.Background()
	suffix := testSuffix(t)
	// Fabric is this owner's inbound producer, and the event's aggregate identity is
	// the one the specification fixes for its exact version.
	event := InboundEvent{
		ID:                "inb-" + suffix,
		SourceOwner:       "fabric",
		SourceEventID:     "evt-" + suffix,
		EventType:         "fabric.resources_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "resource_set",
		AggregateID:       "rs-" + suffix,
		AggregateRevision: 1,
		Payload:           json.RawMessage(`{"resourceSetId":"rs-` + suffix + `","outcome":"confirmed"}`),
	}

	tx, err := runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin inbox transaction: %v", err)
	}
	first, err := DeliverInbox(ctx, tx, event, time.Time{})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("deliver first inbox event: %v", err)
	}
	if first.Decision != InboxCommit || !first.Applied {
		_ = tx.Rollback()
		t.Fatalf("first delivery decision = %#v, want commit", first)
	}
	if err := MarkInboxProcessed(ctx, tx, event.SourceOwner, event.SourceEventID, "rt-"+suffix, "", time.Time{}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("mark inbox processed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit inbox transaction: %v", err)
	}

	var processed bool
	var resultResourceID string
	if err := runtimeControlStore.DB().QueryRowContext(ctx, `
		SELECT processed_at IS NOT NULL, COALESCE(result_resource_id, '')
		FROM runtime_control.inbox_events WHERE source_owner = $1 AND source_event_id = $2`,
		event.SourceOwner, event.SourceEventID).Scan(&processed, &resultResourceID); err != nil {
		t.Fatalf("read inbox event: %v", err)
	}
	if !processed || resultResourceID != "rt-"+suffix {
		t.Fatalf("inbox processing outcome did not persist: processed=%v result=%q", processed, resultResourceID)
	}

	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin duplicate transaction: %v", err)
	}
	duplicate, err := DeliverInbox(ctx, tx, event, time.Time{})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("deliver duplicate inbox event: %v", err)
	}
	if duplicate.Decision != InboxDuplicate || duplicate.Applied {
		_ = tx.Rollback()
		t.Fatalf("identical repeat decision = %#v, want duplicate without reapplication", duplicate)
	}
	_ = tx.Rollback()

	conflicting := event
	conflicting.ID = event.ID + "-other"
	conflicting.Payload = json.RawMessage(`{"resourceSetId":"rs-` + suffix + `","outcome":"rejected"}`)
	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin conflict transaction: %v", err)
	}
	conflict, err := DeliverInbox(ctx, tx, conflicting, time.Time{})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("deliver conflicting inbox event: %v", err)
	}
	if conflict.Decision != InboxConflict || conflict.Applied {
		_ = tx.Rollback()
		t.Fatalf("same identity with different bytes = %#v, want conflict", conflict)
	}
	_ = tx.Rollback()

	// Reusing the source event id with changed metadata is also a conflict: the
	// deduplication compares the whole immutable identity, so a producer cannot
	// rewrite a recorded fact by keeping the bytes and moving the revision.
	restated := event
	restated.ID = event.ID + "-restated"
	restated.AggregateRevision = event.AggregateRevision + 1
	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin restated transaction: %v", err)
	}
	changed, err := DeliverInbox(ctx, tx, restated, time.Time{})
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("deliver restated inbox event: %v", err)
	}
	if changed.Decision != InboxConflict || changed.Applied {
		_ = tx.Rollback()
		t.Fatalf("same bytes with a changed aggregate revision = %#v, want conflict", changed)
	}
	_ = tx.Rollback()

	var recorded int
	if err := runtimeControlStore.DB().QueryRowContext(ctx, `
		SELECT count(*) FROM runtime_control.inbox_events
		WHERE source_owner = $1 AND source_event_id = $2`,
		event.SourceOwner, event.SourceEventID).Scan(&recorded); err != nil {
		t.Fatalf("count inbox events: %v", err)
	}
	if recorded != 1 {
		t.Fatalf("inbox deduplication recorded %d rows, want 1", recorded)
	}
}

func TestPostgresIdempotencyReplayAndConflict(t *testing.T) {
	runtimeControlStore := runtimeControlTestStore(t)
	ctx := context.Background()
	suffix := testSuffix(t)
	body := json.RawMessage(`{"runtimeInstanceId":"rt-` + suffix + `"}`)
	input := IdempotencyInput{
		ID:             "idem-" + suffix,
		TenantScope:    "tenant-" + suffix,
		ActorScope:     "actor-" + suffix,
		OperationName:  "deployRuntime",
		IdempotencyKey: "key-" + suffix,
		RequestSHA256:  HashRequestBody(body),
		ResourceID:     "rt-" + suffix,
		OperationID:    "op-" + suffix,
		ResponseStatus: 202,
		ResponseBody:   json.RawMessage(`{"operationId":"op-` + suffix + `"}`),
	}

	tx, err := runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin idempotency transaction: %v", err)
	}
	if err := RecordIdempotency(ctx, tx, input); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record idempotency: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit idempotency transaction: %v", err)
	}

	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin replay transaction: %v", err)
	}
	replay, found, err := LookupIdempotency(ctx, tx, input)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("lookup replayed command: %v", err)
	}
	_ = tx.Rollback()
	if !found || !replay.Replayed {
		t.Fatalf("a repeated identical command must replay the stored result: %#v", replay)
	}
	if replay.ID != input.ID || replay.ResourceID != input.ResourceID || replay.OperationID != input.OperationID ||
		replay.ResponseStatus != input.ResponseStatus {
		t.Fatalf("replayed result mismatch: %#v", replay)
	}
	if !sameJSONValue(t, replay.ResponseBody, input.ResponseBody) {
		t.Fatalf("replayed response body = %q, want %s", replay.ResponseBody, input.ResponseBody)
	}

	conflicting := input
	conflicting.RequestSHA256 = HashRequestBody([]byte(`{"runtimeInstanceId":"rt-other-` + suffix + `"}`))
	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin conflict transaction: %v", err)
	}
	if _, found, err := LookupIdempotency(ctx, tx, conflicting); !errors.Is(err, ErrIdempotencyConflict) || !found {
		_ = tx.Rollback()
		t.Fatalf("a reused key with a different body must conflict, got found=%v err=%v", found, err)
	}
	_ = tx.Rollback()

	unknown := input
	unknown.IdempotencyKey = "key-unknown-" + suffix
	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin unknown key transaction: %v", err)
	}
	if _, found, err := LookupIdempotency(ctx, tx, unknown); err != nil || found {
		_ = tx.Rollback()
		t.Fatalf("an unseen key is not a replay, got found=%v err=%v", found, err)
	}
	_ = tx.Rollback()

	invalid := input
	invalid.ID = "idem-invalid-" + suffix
	invalid.ResponseStatus = 99
	tx, err = runtimeControlStore.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin invalid transaction: %v", err)
	}
	if err := RecordIdempotency(ctx, tx, invalid); !errors.Is(err, ErrInvalidIdempotencyInput) {
		_ = tx.Rollback()
		t.Fatalf("an invalid record must be rejected, got %v", err)
	}
	_ = tx.Rollback()
}
