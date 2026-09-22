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

	"github.com/lib/pq"

	"opl-cloud/services/gateway-integration/migrations"
)

// The two owner blocks guard their own database names and require their own
// roles, so this test provisions the databases and roles the specification's
// isolated harness provisions. It runs only in the gated database lane.
const (
	tenantOwnerRole   = "opl_tenant_owner"
	tenantWriterRole  = "opl_tenant_writer"
	gatewayOwnerRole  = "opl_gateway_owner"
	gatewayWriterRole = "opl_gateway_writer"
)

func provisionOwnerRolesAndDatabases(t *testing.T) string {
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
	// Both owners' NOLOGIN writer roles and both databases are provisioned here:
	// this deployment unit owns both, and the cross-owner guard below proves the
	// two blocks are not interchangeable. CREATE DATABASE cannot run inside a
	// transaction block, so existence is checked before each statement instead.
	ctx := context.Background()
	for _, role := range []string{tenantOwnerRole, tenantWriterRole, gatewayOwnerRole, gatewayWriterRole} {
		exists, err := catalogObjectExists(ctx, admin, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, role)
		if err != nil {
			t.Fatalf("inspect role %s: %v", role, err)
		}
		if exists {
			continue
		}
		if _, err := admin.ExecContext(ctx, `CREATE ROLE `+pq.QuoteIdentifier(role)+` NOLOGIN`); err != nil {
			t.Fatalf("provision role %s: %v", role, err)
		}
	}
	for _, database := range []string{"opl_tenant", "opl_gateway"} {
		exists, err := catalogObjectExists(ctx, admin, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, database)
		if err != nil {
			t.Fatalf("inspect database %s: %v", database, err)
		}
		if exists {
			continue
		}
		if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(database)); err != nil {
			t.Fatalf("provision database %s: %v", database, err)
		}
	}
	return maintenance
}

func openOwnerDatabase(t *testing.T, database string) *sql.DB {
	t.Helper()
	maintenance := provisionOwnerRolesAndDatabases(t)
	db, err := sql.Open("postgres", replaceDatabase(t, maintenance, database))
	if err != nil {
		t.Fatalf("open owner database %s: %v", database, err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("reach owner database %s: %v", database, err)
	}
	return db
}

func catalogObjectExists(ctx context.Context, db *sql.DB, query, name string) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, query, name).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
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

func TestInstallAppliesTheOwnerSchemaOnItsOwnDatabase(t *testing.T) {
	db := openOwnerDatabase(t, "opl_gateway")
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

	for _, table := range []string{"identity_mappings", "tenant_wallet_bindings", "key_bindings",
		"wallet_operations", "operations", "outbox_events", "outbox_deliveries", "inbox_events",
		"idempotency_records"} {
		var exists bool
		if err := db.QueryRowContext(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, "gateway."+table).Scan(&exists); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table gateway.%s was not created", table)
		}
	}
	// This database carries only the gateway owner's schema: CloudIdentity's
	// tables are a different database, not a schema in this one.
	var tenantSchemaExists bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT to_regclass('tenant.operations') IS NOT NULL`).Scan(&tenantSchemaExists); err != nil {
		t.Fatalf("inspect cross-owner schema: %v", err)
	}
	if tenantSchemaExists {
		t.Fatal("the tenant owner's schema must not exist in opl_gateway")
	}
}

func TestEachOwnerBlockRejectsTheOtherDatabase(t *testing.T) {
	db := openOwnerDatabase(t, "opl_gateway")
	own, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	other, err := migrations.Files.ReadFile(migrations.TenantFile)
	if err != nil {
		t.Fatalf("read the other owner's block: %v", err)
	}

	// The gateway block guards its own database name: applied against a different
	// database it must fail instead of creating the schema elsewhere.
	maintenance, err := sql.Open("postgres", replaceDatabase(t, maintenanceDSN(t), envOrLocal("PGDATABASE", "postgres")))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	defer maintenance.Close()
	if _, err := maintenance.ExecContext(context.Background(), own[0].Query); err == nil {
		t.Fatal("applying the gateway block against another database must fail")
	}

	// The tenant block is a different database and a different role, so it must
	// never be applied from this store either.
	if _, err := db.ExecContext(context.Background(), string(other)); err == nil {
		t.Fatal("applying the tenant block against opl_gateway must fail")
	}
}

func TestOperationOutboxInboxAndIdempotencyRoundTrip(t *testing.T) {
	db := openOwnerDatabase(t, "opl_gateway")
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
		TenantID:      "tenant-" + suffix,
		ActorID:       "actor-1",
		Kind:          "revoke_key",
		ResourceID:    "key-" + suffix,
		Stage:         "key_revocation",
		RequestID:     "req-" + suffix,
		AcceptedInput: json.RawMessage(`{"keyBindingId":"key-1"}`),
	})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if operation.Status != OperationAccepted {
		t.Fatalf("new operation status = %q", operation.Status)
	}
	if operation.TenantID != "tenant-"+suffix {
		t.Fatalf("new operation tenant = %q", operation.TenantID)
	}

	// The Outbox row and one delivery row per consumer owner are written in the
	// same transaction as the command.
	event := Event{
		ID:                "evt-" + suffix,
		EventType:         "wallet.operation_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "wallet_operation",
		AggregateID:       "wallet-op-" + suffix,
		AggregateRevision: 1,
		TenantID:          operation.TenantID,
		CorrelationID:     operation.RequestID,
		Payload:           json.RawMessage(`{"walletOperationId":"wallet-op-1"}`),
		OccurredAt:        time.Now().UTC(),
	}
	if err := AppendEvent(ctx, tx, event, []string{"tenant", "workspace", "ledger"}); err != nil {
		t.Fatalf("append outbox event: %v", err)
	}

	idempotency := IdempotencyInput{
		ID:             "idem-" + suffix,
		TenantScope:    "tenant-1",
		ActorScope:     "actor-1",
		OperationName:  "revokeGatewayKey",
		IdempotencyKey: "key-" + suffix,
		RequestSHA256:  HashRequestBody([]byte(`{"keyBindingId":"key-1"}`)),
		ResourceID:     operation.ResourceID,
		OperationID:    operation.ID,
		ResponseStatus: 202,
		ResponseBody:   json.RawMessage(`{"operationId":"op-1"}`),
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

	// A non-terminal operation with an unknown external result stays unknown.
	reconciled, err := owner.ReconcileOperation(ctx, operation.ID)
	if err != nil {
		t.Fatalf("reconcile operation: %v", err)
	}
	if reconciled.Operation.Terminal() || !reconciled.StillUnknown {
		t.Fatalf("reconciled operation = %+v, want a non-terminal unknown result", reconciled)
	}

	// A terminal transition requires a completion time and is not repeatable.
	completed, err := owner.CompleteOperation(ctx, operation.ID, OperationSucceeded, "readback", "", ObservationConfirmed,
		json.RawMessage(`{"keyBindingId":"key-1"}`), time.Now().UTC())
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

	// Each consumer is acknowledged independently. The gated lane reuses one owner
	// database across runs, so every delivery assertion is scoped to this run's
	// event rather than to the whole consumer.
	pending := pendingForEvent(t, owner, "tenant", event.ID)
	if len(pending) != 1 {
		t.Fatalf("pending tenant deliveries = %+v", pending)
	}
	if pending[0].Event.AggregateType != "wallet_operation" {
		t.Fatalf("pending delivery lost the producer-supplied aggregate type: %+v", pending[0].Event)
	}
	if err := owner.AcknowledgeDelivery(ctx, "tenant", event.ID); err != nil {
		t.Fatalf("acknowledge delivery: %v", err)
	}
	if after := pendingForEvent(t, owner, "tenant", event.ID); len(after) != 0 {
		t.Fatalf("tenant should be acknowledged, got %+v", after)
	}
	// Acknowledging one consumer must not mark another one delivered.
	for _, consumer := range []string{"workspace", "ledger"} {
		if remaining := pendingForEvent(t, owner, consumer, event.ID); len(remaining) != 1 {
			t.Fatalf("%s delivery must remain pending, got %+v", consumer, remaining)
		}
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
	conflicting.RequestSHA256 = HashRequestBody([]byte(`{"keyBindingId":"key-2"}`))
	if _, found, err := LookupIdempotency(ctx, replayTx, conflicting); !found || !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting idempotency lookup found=%v err=%v, want ErrIdempotencyConflict", found, err)
	}
}

// pendingForEvent returns one run's undelivered Outbox deliveries for a single
// consumer; rows left by earlier runs in the same gated database are ignored.
func pendingForEvent(t *testing.T, owner *Store, consumer, eventID string) []PendingDelivery {
	t.Helper()
	pending, err := owner.PendingDeliveries(context.Background(), consumer, 500)
	if err != nil {
		t.Fatalf("list %s pending deliveries: %v", consumer, err)
	}
	var matching []PendingDelivery
	for _, delivery := range pending {
		if delivery.EventID == eventID {
			matching = append(matching, delivery)
		}
	}
	return matching
}

func TestInboxDeduplicatesAndRejectsConflictingBytes(t *testing.T) {
	db := openOwnerDatabase(t, "opl_gateway")
	owner := New(db)
	if err := owner.Install(context.Background()); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}
	ctx := context.Background()
	suffix := time.Now().UTC().Format("20060102150405.000000000")

	event := InboundEvent{
		ID:                "inb-" + suffix,
		SourceOwner:       "tenant",
		SourceEventID:     "evt-" + suffix,
		EventType:         "tenant.access_revoked.v1",
		SchemaVersion:     1,
		AggregateType:     "tenant",
		AggregateID:       "tenant-" + suffix,
		AggregateRevision: 1,
		Payload:           json.RawMessage(`{"tenantId":"tenant-1"}`),
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
	if err := MarkInboxProcessed(ctx, commitTx, event.SourceOwner, event.SourceEventID, "tenant-1", "", time.Now().UTC()); err != nil {
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
		Payload:           json.RawMessage(`{"tenantId":"tenant-2"}`),
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("deliver conflicting event: %v", err)
	}
	if conflicting.Decision != InboxConflict || conflicting.Applied {
		t.Fatalf("conflicting delivery = %+v, want conflict without overwrite", conflicting)
	}

	var storedHash string
	if err := db.QueryRowContext(ctx,
		`SELECT payload_sha256 FROM gateway.inbox_events WHERE source_owner = $1 AND source_event_id = $2`,
		event.SourceOwner, event.SourceEventID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored inbox hash: %v", err)
	}
	if storedHash != event.PayloadSHA256() {
		t.Fatal("the first recorded fact must not be overwritten by a conflicting delivery")
	}
}

// The runtime writer may write its own domain but must not rewrite immutable
// owner facts, and it must not reach the other owner's schema at all.
func TestWriterRoleKeepsImmutableOwnerRowsAndOtherOwnerOutOfReach(t *testing.T) {
	db := openOwnerDatabase(t, "opl_gateway")
	owner := New(db)
	if err := owner.Install(context.Background()); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}
	ctx := context.Background()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open writer connection: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET ROLE `+gatewayWriterRole); err != nil {
		t.Fatalf("set writer role: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `RESET ROLE`) }()

	denied := []struct {
		name string
		sql  string
	}{
		{"update outbox event", `UPDATE gateway.outbox_events SET event_type = 'x' WHERE false`},
		{"delete outbox event", `DELETE FROM gateway.outbox_events WHERE false`},
	}
	for _, statement := range denied {
		if _, err := conn.ExecContext(ctx, statement.sql); err == nil {
			t.Fatalf("%s must be denied to %s", statement.name, gatewayWriterRole)
		}
	}
	// Control: the same role keeps the DML the specification grants it.
	if _, err := conn.ExecContext(ctx, `UPDATE gateway.operations SET updated_at = now() WHERE false`); err != nil {
		t.Fatalf("the writer role must keep its granted DML on gateway.operations: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT 1 FROM tenant.operations`); err == nil {
		t.Fatal("the gateway database must not expose the tenant owner's schema")
	}
}
