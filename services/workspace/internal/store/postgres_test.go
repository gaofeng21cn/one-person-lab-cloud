package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// The workspace owner's DDL guards its own database name and requires its own
// roles, so this test provisions that owner database the same way the
// specification's isolated harness does. It runs only in the gated database
// lane.
func openOwnerDatabase(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("OPL_POSTGRES_TESTS") != "1" {
		t.Skip("set OPL_POSTGRES_TESTS=1 with a reachable PostgreSQL server to run owner database tests")
	}
	maintenance := maintenanceDSN(t)
	admin, err := sql.Open("postgres", maintenance)
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	defer admin.Close()
	if err := admin.PingContext(context.Background()); err != nil {
		t.Fatalf("reach maintenance database: %v", err)
	}
	for _, role := range []string{"opl_workspace_owner", "opl_workspace_writer"} {
		statement := fmt.Sprintf(
			`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='%s') THEN CREATE ROLE %s NOLOGIN; END IF; END $$;`,
			role, role)
		if _, err := admin.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("provision owner role %s: %v", role, err)
		}
	}
	// CREATE DATABASE cannot run inside a transaction block, so it is issued as
	// its own statement once the catalog says the database is absent.
	var databaseExists bool
	if err := admin.QueryRowContext(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname='opl_workspace')`).Scan(&databaseExists); err != nil {
		t.Fatalf("inspect owner database: %v", err)
	}
	if !databaseExists {
		if _, err := admin.ExecContext(context.Background(), `CREATE DATABASE opl_workspace`); err != nil {
			t.Fatalf("provision owner database: %v", err)
		}
	}

	ownerDSN := replaceDatabase(t, maintenance, "opl_workspace")
	db, err := sql.Open("postgres", ownerDSN)
	if err != nil {
		t.Fatalf("open owner database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("reach owner database: %v", err)
	}
	return db
}

func maintenanceDSN(t *testing.T) string {
	t.Helper()
	host := envOrLocal("PGHOST", "127.0.0.1")
	port := envOrLocal("PGPORT", "5432")
	user := envOrLocal("PGUSER", "postgres")
	name := envOrLocal("PGDATABASE", "postgres")
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", url.QueryEscape(user), host, port, name)
}

func replaceDatabase(t *testing.T, dsn, database string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	parsed.Path = "/" + database
	return parsed.String()
}

func envOrLocal(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func TestInstallAppliesTheOwnerSchemaAndRejectsTheWrongDatabase(t *testing.T) {
	db := openOwnerDatabase(t)
	owner := New(db)
	if err := owner.Install(context.Background()); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}
	// Re-installing is idempotent through the migration journal.
	if err := owner.Install(context.Background()); err != nil {
		t.Fatalf("reinstall owner schema: %v", err)
	}
	if err := owner.Ready(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}

	for _, table := range []string{"workspaces", "operations", "outbox_events", "outbox_deliveries", "inbox_events", "idempotency_records"} {
		var exists bool
		if err := db.QueryRowContext(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, "workspace."+table).Scan(&exists); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table workspace.%s was not created", table)
		}
	}

	// The DDL guards its own database name: applying the workspace block against
	// a different database must fail rather than create the schema elsewhere.
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	other, err := sql.Open("postgres", replaceDatabase(t, maintenanceDSN(t), envOrLocal("PGDATABASE", "postgres")))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	defer other.Close()
	if _, err := other.ExecContext(context.Background(), migrations[0].Query); err == nil {
		t.Fatal("applying the workspace block against another database must fail")
	}
}

func TestOperationOutboxInboxAndIdempotencyRoundTrip(t *testing.T) {
	db := openOwnerDatabase(t)
	owner := New(db)
	if err := owner.Install(context.Background()); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}
	ctx := context.Background()
	suffix := time.Now().UTC().Format("20060102150405.000000000")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	operation, err := CreateOperation(ctx, tx, OperationInput{
		ID:            "op-" + suffix,
		ActorID:       "actor-1",
		Kind:          "create_workspace",
		ResourceID:    "ws-" + suffix,
		Stage:         "admission",
		RequestID:     "req-" + suffix,
		AcceptedInput: json.RawMessage(`{"computePlanId":"cp-1"}`),
	})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if operation.Status != OperationAccepted {
		t.Fatalf("new operation status = %q", operation.Status)
	}

	// The Outbox row is written in the same transaction as the command.
	event := Event{
		ID:                "evt-" + suffix,
		EventType:         "workspace.state_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "workspace",
		AggregateID:       operation.ResourceID,
		AggregateRevision: 1,
		CorrelationID:     operation.RequestID,
		Payload:           json.RawMessage(`{"workspaceId":"` + operation.ResourceID + `"}`),
		OccurredAt:        time.Now().UTC(),
	}
	if err := AppendEvent(ctx, tx, event, []string{"runtime_control", "ledger"}); err != nil {
		t.Fatalf("append outbox event: %v", err)
	}

	idempotency := IdempotencyInput{
		ID:             "idem-" + suffix,
		TenantScope:    "tenant-1",
		ActorScope:     "actor-1",
		OperationName:  "createWorkspace",
		IdempotencyKey: "key-" + suffix,
		RequestSHA256:  HashRequestBody([]byte(`{"computePlanId":"cp-1"}`)),
		ResourceID:     operation.ResourceID,
		OperationID:    operation.ID,
		ResponseStatus: 201,
		ResponseBody:   json.RawMessage(`{"workspaceId":"` + operation.ResourceID + `"}`),
	}
	if err := RecordIdempotency(ctx, tx, idempotency); err != nil {
		t.Fatalf("record idempotency: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	read, err := owner.ReadOperation(ctx, operation.ID)
	if err != nil {
		t.Fatalf("read operation: %v", err)
	}
	if read.ResourceID != operation.ResourceID || read.RequestID != operation.RequestID {
		t.Fatalf("read operation mismatch: %+v", read)
	}
	if _, err := owner.ReadOperation(ctx, "missing-"+suffix); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("unknown operation error = %v, want ErrOperationNotFound", err)
	}

	// A terminal transition requires a completion time and is not repeatable.
	completed, err := owner.CompleteOperation(ctx, operation.ID, OperationSucceeded, "readback", "", ObservationConfirmed,
		json.RawMessage(`{"workspaceId":"`+operation.ResourceID+`"}`), time.Now().UTC())
	if err != nil {
		t.Fatalf("complete operation: %v", err)
	}
	if !completed.Terminal() || completed.Observation != ObservationConfirmed {
		t.Fatalf("completed operation = %+v", completed)
	}
	if _, err := owner.CompleteOperation(ctx, operation.ID, OperationFailed, "readback", "", "", nil, time.Time{}); !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("re-completing a terminal operation error = %v, want ErrOperationTerminal", err)
	}
	if _, err := owner.CompleteOperation(ctx, "missing-"+suffix, OperationSucceeded, "readback", "", "", nil, time.Now().UTC()); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("completing an unknown operation error = %v, want ErrOperationNotFound", err)
	}

	// Each consumer is acknowledged independently. Deliveries are listed per
	// consumer across this owner's whole database, so each assertion selects
	// this run's event rather than assuming the queue is otherwise empty.
	pending, err := owner.PendingDeliveries(ctx, "runtime_control", allPendingDeliveries)
	if err != nil {
		t.Fatalf("list pending deliveries: %v", err)
	}
	runtimeDelivery, found := pendingForEvent(pending, event.ID)
	if !found {
		t.Fatalf("runtime_control delivery is missing from %+v", pending)
	}
	if runtimeDelivery.Event.AggregateType != "workspace" {
		t.Fatalf("pending delivery lost the producer-supplied aggregate type: %+v", runtimeDelivery.Event)
	}
	if err := owner.AcknowledgeDelivery(ctx, "runtime_control", event.ID); err != nil {
		t.Fatalf("acknowledge delivery: %v", err)
	}
	afterRuntime, err := owner.PendingDeliveries(ctx, "runtime_control", allPendingDeliveries)
	if err != nil {
		t.Fatalf("list pending deliveries: %v", err)
	}
	if _, found := pendingForEvent(afterRuntime, event.ID); found {
		t.Fatalf("runtime_control delivery must be acknowledged, got %+v", afterRuntime)
	}
	// Acknowledging one consumer must not mark the other delivered.
	pendingLedger, err := owner.PendingDeliveries(ctx, "ledger", allPendingDeliveries)
	if err != nil {
		t.Fatalf("list pending deliveries: %v", err)
	}
	if _, found := pendingForEvent(pendingLedger, event.ID); !found {
		t.Fatalf("ledger delivery must remain pending, got %+v", pendingLedger)
	}

	// Idempotency replays the same identity and rejects a changed body.
	replayTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin replay transaction: %v", err)
	}
	defer replayTx.Rollback()
	record, found, err := LookupIdempotency(ctx, replayTx, idempotency)
	if err != nil || !found || !record.Replayed {
		t.Fatalf("replay lookup = %+v found=%v err=%v", record, found, err)
	}
	if record.ResourceID != operation.ResourceID || record.OperationID != operation.ID {
		t.Fatalf("replay must return the original identity, got %+v", record)
	}
	conflicting := idempotency
	conflicting.RequestSHA256 = HashRequestBody([]byte(`{"computePlanId":"cp-2"}`))
	if _, found, err := LookupIdempotency(ctx, replayTx, conflicting); !found || !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting idempotency lookup found=%v err=%v, want ErrIdempotencyConflict", found, err)
	}
}

func TestInboxDeduplicatesAndRejectsConflictingBytes(t *testing.T) {
	db := openOwnerDatabase(t)
	owner := New(db)
	if err := owner.Install(context.Background()); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}
	ctx := context.Background()
	suffix := time.Now().UTC().Format("20060102150405.000000000")

	event := InboundEvent{
		ID:                "inb-" + suffix,
		SourceOwner:       "runtime_control",
		SourceEventID:     "evt-" + suffix,
		EventType:         "runtime.readiness_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "runtime_instance",
		AggregateID:       "rt-" + suffix,
		AggregateRevision: 1,
		Payload:           json.RawMessage(`{"runtimeInstanceId":"rt-1"}`),
	}

	commitTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	result, err := DeliverInbox(ctx, commitTx, event, time.Now().UTC())
	if err != nil {
		t.Fatalf("deliver inbox event: %v", err)
	}
	if result.Decision != InboxCommit || !result.Applied {
		t.Fatalf("first delivery = %+v, want commit", result)
	}
	if err := MarkInboxProcessed(ctx, commitTx, event.SourceOwner, event.SourceEventID, "rt-1", "", time.Now().UTC()); err != nil {
		t.Fatalf("mark processed: %v", err)
	}
	if err := commitTx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	duplicateTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer duplicateTx.Rollback()
	duplicate, err := DeliverInbox(ctx, duplicateTx, event, time.Now().UTC())
	if err != nil {
		t.Fatalf("deliver duplicate: %v", err)
	}
	if duplicate.Decision != InboxDuplicate || duplicate.Applied {
		t.Fatalf("duplicate delivery = %+v, want duplicate without reapply", duplicate)
	}

	conflicting, err := DeliverInbox(ctx, duplicateTx, InboundEvent{
		ID:                event.ID,
		SourceOwner:       event.SourceOwner,
		SourceEventID:     event.SourceEventID,
		EventType:         event.EventType,
		SchemaVersion:     event.SchemaVersion,
		AggregateType:     event.AggregateType,
		AggregateID:       event.AggregateID,
		AggregateRevision: event.AggregateRevision,
		Payload:           json.RawMessage(`{"runtimeInstanceId":"rt-2"}`),
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("deliver conflicting event: %v", err)
	}
	if conflicting.Decision != InboxConflict || conflicting.Applied {
		t.Fatalf("conflicting delivery = %+v, want conflict without overwrite", conflicting)
	}

	var storedHash string
	if err := db.QueryRowContext(ctx,
		`SELECT payload_sha256 FROM workspace.inbox_events WHERE source_owner = $1 AND source_event_id = $2`,
		event.SourceOwner, event.SourceEventID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored inbox hash: %v", err)
	}
	if storedHash != event.PayloadSHA256() {
		t.Fatal("the first recorded fact must not be overwritten by a conflicting delivery")
	}
}

// allPendingDeliveries asks for every undelivered row this owner holds for one
// consumer; the owner database outlives a single test run.
const allPendingDeliveries = 1 << 20

func pendingForEvent(pending []PendingDelivery, eventID string) (PendingDelivery, bool) {
	for _, delivery := range pending {
		if delivery.Event.ID == eventID {
			return delivery, true
		}
	}
	return PendingDelivery{}, false
}
