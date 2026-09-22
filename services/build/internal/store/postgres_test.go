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

// The build owner's DDL guards its own database name and requires its own roles,
// so this test provisions that owner database the same way the specification's
// isolated harness does. It runs only in the gated database lane.
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
	for _, statement := range []string{
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='opl_build_owner') THEN CREATE ROLE opl_build_owner NOLOGIN; END IF; END $$;`,
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='opl_build_writer') THEN CREATE ROLE opl_build_writer NOLOGIN; END IF; END $$;`,
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_database WHERE datname='opl_build') THEN CREATE DATABASE opl_build; END IF; END $$;`,
	} {
		if _, err := admin.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("provision owner database/roles: %v", err)
		}
	}
	ownerDSN := replaceDatabase(t, maintenance, "opl_build")
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

	for _, table := range []string{"operations", "outbox_events", "outbox_deliveries", "inbox_events", "idempotency_records"} {
		var exists bool
		if err := db.QueryRowContext(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, "build."+table).Scan(&exists); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table build.%s was not created", table)
		}
	}

	// The DDL guards its own database name: applying the build block against a
	// different database must fail rather than create the schema elsewhere.
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
		t.Fatal("applying the build block against another database must fail")
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
		Kind:          "build",
		ResourceID:    "job-" + suffix,
		Stage:         "queued",
		RequestID:     "req-" + suffix,
		AcceptedInput: json.RawMessage(`{"packageVersionId":"pv-1"}`),
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
		EventType:         "build.artifact_confirmed.v1",
		SchemaVersion:     1,
		AggregateType:     "build_job",
		AggregateID:       operation.ResourceID,
		AggregateRevision: 1,
		CorrelationID:     operation.RequestID,
		Payload:           json.RawMessage(`{"buildJobId":"` + operation.ResourceID + `"}`),
		OccurredAt:        time.Now().UTC(),
	}
	if err := AppendEvent(ctx, tx, event, []string{"capability", "ledger"}); err != nil {
		t.Fatalf("append outbox event: %v", err)
	}

	idempotency := IdempotencyInput{
		ID:             "idem-" + suffix,
		TenantScope:    "tenant-1",
		ActorScope:     "actor-1",
		OperationName:  "createBuild",
		IdempotencyKey: "key-" + suffix,
		RequestSHA256:  HashRequestBody([]byte(`{"packageVersionId":"pv-1"}`)),
		ResourceID:     operation.ResourceID,
		OperationID:    operation.ID,
		ResponseStatus: 201,
		ResponseBody:   json.RawMessage(`{"buildJobId":"` + operation.ResourceID + `"}`),
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
		json.RawMessage(`{"capabilityVersionId":"ver-1"}`), time.Now().UTC())
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

	// Each consumer is acknowledged independently.
	pending, err := owner.PendingDeliveries(ctx, "capability", 10)
	if err != nil {
		t.Fatalf("list pending deliveries: %v", err)
	}
	if len(pending) != 1 || pending[0].Event.ID != event.ID {
		t.Fatalf("pending capability deliveries = %+v", pending)
	}
	if pending[0].Event.AggregateType != "build_job" {
		t.Fatalf("pending delivery lost the producer-supplied aggregate type: %+v", pending[0].Event)
	}
	if err := owner.AcknowledgeDelivery(ctx, "capability", event.ID); err != nil {
		t.Fatalf("acknowledge delivery: %v", err)
	}
	afterCapability, err := owner.PendingDeliveries(ctx, "capability", 10)
	if err != nil {
		t.Fatalf("list pending deliveries: %v", err)
	}
	if len(afterCapability) != 0 {
		t.Fatalf("capability should be acknowledged, got %+v", afterCapability)
	}
	// Acknowledging one consumer must not mark the other delivered.
	pendingLedger, err := owner.PendingDeliveries(ctx, "ledger", 10)
	if err != nil {
		t.Fatalf("list pending deliveries: %v", err)
	}
	if len(pendingLedger) != 1 {
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
	conflicting.RequestSHA256 = HashRequestBody([]byte(`{"packageVersionId":"pv-2"}`))
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
		SourceOwner:       "capability",
		SourceEventID:     "evt-" + suffix,
		EventType:         "capability.version_registered.v1",
		SchemaVersion:     1,
		AggregateType:     "capability_version",
		AggregateID:       "ver-" + suffix,
		AggregateRevision: 1,
		Payload:           json.RawMessage(`{"capabilityVersionId":"ver-1"}`),
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
	if err := MarkInboxProcessed(ctx, commitTx, event.SourceOwner, event.SourceEventID, "ver-1", "", time.Now().UTC()); err != nil {
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
		Payload:           json.RawMessage(`{"capabilityVersionId":"ver-2"}`),
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("deliver conflicting event: %v", err)
	}
	if conflicting.Decision != InboxConflict || conflicting.Applied {
		t.Fatalf("conflicting delivery = %+v, want conflict without overwrite", conflicting)
	}

	var storedHash string
	if err := db.QueryRowContext(ctx,
		`SELECT payload_sha256 FROM build.inbox_events WHERE source_owner = $1 AND source_event_id = $2`,
		event.SourceOwner, event.SourceEventID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored inbox hash: %v", err)
	}
	if storedHash != event.PayloadSHA256() {
		t.Fatal("the first recorded fact must not be overwritten by a conflicting delivery")
	}
}
