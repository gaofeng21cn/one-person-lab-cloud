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

// These names mirror docs/spec/v2.26/contracts/schema.sql and the spec harness
// (`CREATE ROLE {database}_owner NOLOGIN; CREATE ROLE {database}_writer NOLOGIN;
// CREATE DATABASE {database};`). The fixture creates them idempotently so a
// repeated run never mutates another owner's database.
const (
	resourceCatalogTestDatabase = "opl_resource_catalog"
	resourceCatalogOwnerRole    = "opl_resource_catalog_owner"
	resourceCatalogWriterRole   = "opl_resource_catalog_writer"
)

// openResourceCatalogTestPostgres prepares the owner's own database and roles,
// applies nothing itself, and returns a connection to the fixture database. It
// is gated on OPL_POSTGRES_TESTS=1 so ordinary `go test` never needs a server.
func openResourceCatalogTestPostgres(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("OPL_POSTGRES_TESTS") != "1" {
		t.Skip("OPL_POSTGRES_TESTS=1 is required for the resource catalog PostgreSQL tests")
	}
	adminURL := fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable",
		url.QueryEscape(testEnvOr("PGUSER", "postgres")),
		testEnvOr("PGHOST", "127.0.0.1"),
		testEnvOr("PGPORT", "5432"),
		testEnvOr("PGDATABASE", "postgres"))
	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		t.Fatalf("open fixture postgres: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		t.Fatalf("connect fixture postgres: %v", err)
	}

	// The owner DDL revokes database-scope privileges, authorizes the schema to
	// the owner role and switches to it, so the fixture connection must be able
	// to administer those objects. This is a precondition, not a fallback.
	var superuser sql.NullBool
	if err := admin.QueryRowContext(ctx,
		`SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&superuser); err != nil {
		_ = admin.Close()
		t.Fatalf("read fixture role: %v", err)
	}
	if !superuser.Valid || !superuser.Bool {
		_ = admin.Close()
		t.Fatalf("the resource catalog fixture needs PostgreSQL superuser privileges to create %s/%s and %s",
			resourceCatalogOwnerRole, resourceCatalogWriterRole, resourceCatalogTestDatabase)
	}

	for _, role := range []string{resourceCatalogOwnerRole, resourceCatalogWriterRole} {
		statement := fmt.Sprintf(
			`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN EXECUTE 'CREATE ROLE %s NOLOGIN'; END IF; END $$;`,
			pq.QuoteLiteral(role), pq.QuoteIdentifier(role))
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			_ = admin.Close()
			t.Fatalf("create fixture role %s: %v", role, err)
		}
	}
	var databaseExists bool
	if err := admin.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, resourceCatalogTestDatabase).Scan(&databaseExists); err != nil {
		_ = admin.Close()
		t.Fatalf("inspect fixture database: %v", err)
	}
	if !databaseExists {
		if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(resourceCatalogTestDatabase)); err != nil {
			_ = admin.Close()
			t.Fatalf("create fixture database: %v", err)
		}
	}

	testURL, err := resourceCatalogTestDSN(adminURL, resourceCatalogTestDatabase)
	if err != nil {
		_ = admin.Close()
		t.Fatalf("derive fixture DSN: %v", err)
	}
	db, err := sql.Open("postgres", testURL)
	if err != nil {
		_ = admin.Close()
		t.Fatalf("open fixture database: %v", err)
	}
	db.SetMaxOpenConns(5)
	t.Cleanup(func() {
		_ = db.Close()
		_ = admin.Close()
	})
	return db
}

// resourceCatalogTestDSN retargets the fixture connection at the owner database
// without inventing a second connection vocabulary.
// The gated PostgreSQL lane provides PGHOST/PGPORT/PGUSER/PGDATABASE with TLS
// disabled, the same convention services/build uses.
func testEnvOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func resourceCatalogTestDSN(rawURL, database string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme != "" {
		parsed.Path = "/" + database
		return parsed.String(), nil
	}
	if err != nil {
		return "", err
	}
	return rawURL + " dbname=" + pq.QuoteLiteral(database), nil
}

// withResourceCatalogTx runs one owner transaction and reports the caller's
// error so a test can assert a rejected write instead of hiding it.
func withResourceCatalogTx(t *testing.T, db *sql.DB, run func(tx *sql.Tx) error) error {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin owner transaction: %v", err)
	}
	if err := run(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Fatalf("roll back owner transaction: %v", rollbackErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit owner transaction: %v", err)
	}
	return nil
}

// sameJSON compares two payloads by value: PostgreSQL jsonb normalizes whitespace
// and key order, so byte equality is not the persisted fact.
func sameJSON(t *testing.T, got, want json.RawMessage) bool {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode stored payload %s: %v", got, err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode expected payload %s: %v", want, err)
	}
	return reflect.DeepEqual(gotValue, wantValue)
}

func TestResourceCatalogMigrationsInstallOnce(t *testing.T) {
	db := openResourceCatalogTestPostgres(t)
	catalogStore := New(db)
	ctx := context.Background()
	if err := catalogStore.Install(ctx); err != nil {
		t.Fatalf("install resource catalog schema: %v", err)
	}
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var applied int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM opl_schema_migrations WHERE service = $1`, Schema).Scan(&applied); err != nil {
		t.Fatalf("read migration journal: %v", err)
	}
	if applied != len(migrations) {
		t.Fatalf("applied migration count = %d, want %d", applied, len(migrations))
	}
	if err := catalogStore.Install(ctx); err != nil {
		t.Fatalf("reinstall resource catalog schema: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM opl_schema_migrations WHERE service = $1`, Schema).Scan(&applied); err != nil {
		t.Fatalf("re-read migration journal: %v", err)
	}
	if applied != len(migrations) {
		t.Fatalf("reinstalling changed the journal: count = %d, want %d", applied, len(migrations))
	}
	// The journal is the only installation fact; the owner's own schema is
	// installed in the owner database, never in another owner's schema.
	var schemaName sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT to_regnamespace($1)::text`, Schema).Scan(&schemaName); err != nil {
		t.Fatalf("inspect owner schema: %v", err)
	}
	if !schemaName.Valid {
		t.Fatal("the resource catalog schema was not installed")
	}
}

// The runtime writer is scoped to this owner's database and cannot rewrite the
// append-only facts: an UPDATE or DELETE on them is refused by the grant, so a
// "second writer" cannot silently replace published or delivered facts.
func TestResourceCatalogWriterCannotMutateAppendOnlyFacts(t *testing.T) {
	db := openResourceCatalogTestPostgres(t)
	ctx := context.Background()
	if err := New(db).Install(ctx); err != nil {
		t.Fatalf("install resource catalog schema: %v", err)
	}
	for _, statement := range []string{
		`UPDATE ` + Schema + `.outbox_events SET payload = '{}'::jsonb`,
		`DELETE FROM ` + Schema + `.outbox_events`,
		`UPDATE ` + Schema + `.quote_items SET amount_usd_micros = 0`,
		`DELETE FROM ` + Schema + `.price_policy_versions`,
	} {
		t.Run(statement, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin writer transaction: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE "+pq.QuoteIdentifier(resourceCatalogWriterRole)); err != nil {
				t.Fatalf("assume the runtime writer role: %v", err)
			}
			_, err = tx.ExecContext(ctx, statement)
			var pqErr *pq.Error
			if !errors.As(err, &pqErr) || pqErr.Code != "42501" {
				t.Fatalf("writer statement error = %v, want insufficient_privilege (42501)", err)
			}
		})
	}
}

func TestResourceCatalogOperationRoundTrip(t *testing.T) {
	db := openResourceCatalogTestPostgres(t)
	catalogStore := New(db)
	ctx := context.Background()
	if err := catalogStore.Install(ctx); err != nil {
		t.Fatalf("install resource catalog schema: %v", err)
	}

	operationID := fmt.Sprintf("op-quote-%d", time.Now().UnixNano())
	var created Operation
	err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		result, err := CreateOperation(ctx, tx, OperationInput{
			ID:            operationID,
			TenantID:      "tenant-1",
			ActorID:       "actor-1",
			Kind:          "reconcile",
			ResourceID:    "quote-1",
			Stage:         "quote_binding",
			RequestID:     "req-1",
			AcceptedInput: json.RawMessage(`{"quoteId":"quote-1"}`),
		})
		created = result
		return err
	})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if created.Status != OperationAccepted || created.Terminal() {
		t.Fatalf("created operation = %+v", created)
	}

	read, err := catalogStore.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatalf("read operation: %v", err)
	}
	if read.ID != operationID || read.TenantID != "tenant-1" || read.ActorID != "actor-1" ||
		read.Kind != "reconcile" || read.Stage != "quote_binding" || read.ResourceID != "quote-1" ||
		read.RequestID != "req-1" {
		t.Fatalf("read operation = %+v", read)
	}
	if string(read.AcceptedInput) != `{"quoteId": "quote-1"}` {
		t.Fatalf("accepted input = %s", read.AcceptedInput)
	}

	// This owner writes no committed aggregate revision on the operation path yet,
	// so the readback refuses instead of answering with an empty digest or a zero
	// version, and a resource that is not the operation's own resource is bad input.
	if _, err := catalogStore.ReadCommittedEvidence(ctx, operationID, "quote-1"); !errors.Is(err, ErrNoCommittedEvidence) {
		t.Fatalf("committed evidence error = %v, want ErrNoCommittedEvidence", err)
	}
	if _, err := catalogStore.ReadCommittedEvidence(ctx, operationID, "quote-2"); !errors.Is(err, ErrInvalidOperationInput) {
		t.Fatalf("mismatched resource error = %v, want ErrInvalidOperationInput", err)
	}
	if _, err := catalogStore.ReadCommittedEvidence(ctx, "missing-operation", "quote-1"); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("unknown operation evidence error = %v, want ErrOperationNotFound", err)
	}

	reconciled, err := catalogStore.ReconcileOperation(ctx, operationID)
	if err != nil {
		t.Fatalf("reconcile operation: %v", err)
	}
	if reconciled.Changed || !reconciled.StillUnknown {
		t.Fatalf("a non-terminal operation with an unknown external result must stay non-terminal: %+v", reconciled)
	}

	completed, err := catalogStore.CompleteOperation(ctx, operationID, OperationSucceeded, "succeeded",
		"", ObservationConfirmed, json.RawMessage(`{"quoteId":"quote-1"}`), time.Now().UTC())
	if err != nil {
		t.Fatalf("complete operation: %v", err)
	}
	if completed.Status != OperationSucceeded || !completed.Terminal() || completed.CompletedAt.IsZero() {
		t.Fatalf("completed operation = %+v", completed)
	}
	if _, err := catalogStore.CompleteOperation(ctx, operationID, OperationFailed, "readback",
		"quote_expired", ObservationRejected, nil, time.Time{}); !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("second transition error = %v, want ErrOperationTerminal", err)
	}
	if _, err := catalogStore.CompleteOperation(ctx, "missing-operation", OperationFailed, "readback",
		"", ObservationRejected, nil, time.Time{}); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("unknown operation error = %v, want ErrOperationNotFound", err)
	}
	if _, err := catalogStore.ReadOperation(ctx, "missing-operation"); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("unknown read error = %v, want ErrOperationNotFound", err)
	}
}

func TestResourceCatalogOutboxDeliversPerConsumer(t *testing.T) {
	db := openResourceCatalogTestPostgres(t)
	catalogStore := New(db)
	ctx := context.Background()
	if err := catalogStore.Install(ctx); err != nil {
		t.Fatalf("install resource catalog schema: %v", err)
	}

	runID := time.Now().UnixNano()
	policyID := fmt.Sprintf("policy-%d", runID)
	// The recorded aggregate type and the payload field supplying the aggregate id
	// are fixed for this exact event version by the specification's
	// x-aggregate-identity, so the fixture states both rather than inventing them.
	event := Event{
		ID:                fmt.Sprintf("evt-%d", runID),
		EventType:         "catalog.policy_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "catalog_policy_version",
		AggregateID:       policyID,
		AggregateRevision: 1,
		TenantID:          "tenant-1",
		CorrelationID:     "req-1",
		Payload:           json.RawMessage(fmt.Sprintf(`{"policyVersionId":%q}`, policyID)),
		OccurredAt:        time.Now().UTC(),
	}
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return AppendEvent(ctx, tx, event, []string{"workspace", "ledger"})
	}); err != nil {
		t.Fatalf("append outbox event: %v", err)
	}

	var storedHash string
	if err := db.QueryRowContext(ctx,
		`SELECT payload_sha256 FROM `+Schema+`.outbox_events WHERE id = $1`, event.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read outbox row: %v", err)
	}
	if storedHash != event.PayloadSHA256() {
		t.Fatalf("stored payload hash = %q, want %q", storedHash, event.PayloadSHA256())
	}

	// The fixture database is shared across runs, so each assertion is scoped to
	// this run's event instead of the consumer's whole backlog.
	pendingFor := func(consumer string) (PendingDelivery, bool) {
		t.Helper()
		pending, err := catalogStore.PendingDeliveries(ctx, consumer, 500)
		if err != nil {
			t.Fatalf("list %s deliveries: %v", consumer, err)
		}
		for _, delivery := range pending {
			if delivery.EventID == event.ID {
				return delivery, true
			}
		}
		return PendingDelivery{}, false
	}

	for _, consumer := range []string{"workspace", "ledger"} {
		delivery, ok := pendingFor(consumer)
		if !ok {
			t.Fatalf("%s did not receive the outbox event", consumer)
		}
		if delivery.Event.EventType != event.EventType || !sameJSON(t, delivery.Event.Payload, event.Payload) {
			t.Fatalf("%s delivery = %+v", consumer, delivery)
		}
		if delivery.Event.AggregateType != event.AggregateType {
			t.Fatalf("%s delivery aggregate type = %q", consumer, delivery.Event.AggregateType)
		}
	}

	if err := catalogStore.AcknowledgeDelivery(ctx, "workspace", event.ID); err != nil {
		t.Fatalf("acknowledge workspace delivery: %v", err)
	}
	if _, ok := pendingFor("workspace"); ok {
		t.Fatal("an acknowledged consumer still has the delivery pending")
	}
	if _, ok := pendingFor("ledger"); !ok {
		t.Fatal("one consumer's acknowledgement must not deliver another consumer's event")
	}

	// The outbox row stays immutable, so acknowledging is delivery-scoped.
	var acknowledged bool
	if err := db.QueryRowContext(ctx,
		`SELECT acknowledged_at IS NOT NULL FROM `+Schema+`.outbox_deliveries
		WHERE event_id = $1 AND consumer_owner = 'workspace'`, event.ID).Scan(&acknowledged); err != nil {
		t.Fatalf("read delivery row: %v", err)
	}
	if !acknowledged {
		t.Fatal("the workspace delivery was not recorded as acknowledged")
	}

	// The event is valid apart from the consumer owner, so it carries its own
	// aggregate identity and a payload that agrees with it byte-for-byte.
	blankConsumer := event
	blankConsumer.ID = fmt.Sprintf("evt-blank-%d", runID)
	blankConsumer.AggregateID = fmt.Sprintf("policy-blank-%d", runID)
	blankConsumer.Payload = json.RawMessage(fmt.Sprintf(`{"policyVersionId":%q}`, blankConsumer.AggregateID))
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return AppendEvent(ctx, tx, blankConsumer, []string{""})
	}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("blank consumer error = %v, want ErrInvalidEvent", err)
	}
	// The specification fixes the aggregate type for this exact event version, so
	// a producer cannot satisfy the column with an invented string.
	inventedType := event
	inventedType.ID = fmt.Sprintf("evt-type-%d", runID)
	inventedType.AggregateType = "anything_goes"
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return AppendEvent(ctx, tx, inventedType, []string{"ledger"})
	}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invented aggregate type error = %v, want ErrInvalidEvent", err)
	}
	// Nor can it record an aggregate id that its payload does not carry.
	foreignID := event
	foreignID.ID = fmt.Sprintf("evt-foreign-id-%d", runID)
	foreignID.AggregateID = fmt.Sprintf("policy-other-%d", runID)
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return AppendEvent(ctx, tx, foreignID, []string{"ledger"})
	}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("aggregate id from another field error = %v, want ErrInvalidEvent", err)
	}
}

func TestResourceCatalogInboxDeduplicatesAndRejectsConflicts(t *testing.T) {
	db := openResourceCatalogTestPostgres(t)
	catalogStore := New(db)
	ctx := context.Background()
	if err := catalogStore.Install(ctx); err != nil {
		t.Fatalf("install resource catalog schema: %v", err)
	}
	_ = catalogStore

	suffix := time.Now().UnixNano()
	policyID := fmt.Sprintf("policy-%d", suffix)
	// The specification's consumer table lists no resource_catalog subscription, so
	// this generic store case uses a specification-defined event version. It proves
	// the Inbox mechanism, not a production event validation, and no invented event
	// type stands in for a real one.
	first := InboundEvent{
		ID:                fmt.Sprintf("inb-%d", suffix),
		SourceOwner:       "resource_catalog",
		SourceEventID:     fmt.Sprintf("evt-%d", suffix),
		EventType:         "catalog.policy_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "catalog_policy_version",
		AggregateID:       policyID,
		AggregateRevision: 1,
		Payload:           json.RawMessage(fmt.Sprintf(`{"policyVersionId":%q}`, policyID)),
	}

	var applied InboxResult
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		result, err := DeliverInbox(ctx, tx, first, time.Time{})
		applied = result
		return err
	}); err != nil {
		t.Fatalf("deliver inbound event: %v", err)
	}
	if applied.Decision != InboxCommit || !applied.Applied {
		t.Fatalf("first delivery = %+v, want a commit", applied)
	}

	var duplicate InboxResult
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		result, err := DeliverInbox(ctx, tx, first, time.Time{})
		duplicate = result
		return err
	}); err != nil {
		t.Fatalf("repeat inbound event: %v", err)
	}
	if duplicate.Decision != InboxDuplicate || duplicate.Applied {
		t.Fatalf("identical repeat = %+v, want a duplicate acknowledgement", duplicate)
	}

	conflicting := first
	conflicting.ID = fmt.Sprintf("inb-conflict-%d", suffix)
	conflicting.Payload = json.RawMessage(fmt.Sprintf(`{"policyVersionId":%q,"tampered":true}`, policyID))
	var conflict InboxResult
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		result, err := DeliverInbox(ctx, tx, conflicting, time.Time{})
		conflict = result
		return err
	}); err != nil {
		t.Fatalf("conflicting inbound event: %v", err)
	}
	if conflict.Decision != InboxConflict || conflict.Applied {
		t.Fatalf("same identity with different bytes = %+v, want a conflict", conflict)
	}

	// Changed metadata with the same source owner and event id is refused too: the
	// recorded identity is compared in full, so a producer cannot reuse a source
	// event id to restate a fact at another revision.
	changedMetadata := first
	changedMetadata.ID = fmt.Sprintf("inb-metadata-%d", suffix)
	changedMetadata.AggregateRevision = first.AggregateRevision + 1
	var metadataConflict InboxResult
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		result, err := DeliverInbox(ctx, tx, changedMetadata, time.Time{})
		metadataConflict = result
		return err
	}); err != nil {
		t.Fatalf("redelivery with changed metadata: %v", err)
	}
	if metadataConflict.Decision != InboxConflict || metadataConflict.Applied {
		t.Fatalf("changed metadata = %+v, want a conflict", metadataConflict)
	}

	var storedHash string
	var payload []byte
	if err := db.QueryRowContext(ctx,
		`SELECT payload_sha256, payload FROM `+Schema+`.inbox_events
		WHERE source_owner = $1 AND source_event_id = $2`,
		first.SourceOwner, first.SourceEventID).Scan(&storedHash, &payload); err != nil {
		t.Fatalf("read inbox row: %v", err)
	}
	if storedHash != first.PayloadSHA256() {
		t.Fatalf("a conflict overwrote the recorded fact: hash = %q, want %q", storedHash, first.PayloadSHA256())
	}
	if string(payload) != fmt.Sprintf(`{"policyVersionId": %q}`, policyID) {
		t.Fatalf("recorded payload = %s", payload)
	}

	processedAt := time.Now().UTC()
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return MarkInboxProcessed(ctx, tx, first.SourceOwner, first.SourceEventID, "quote-1", "", processedAt)
	}); err != nil {
		t.Fatalf("mark inbox processed: %v", err)
	}
	var storedProcessed time.Time
	var resultResourceID sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT processed_at, result_resource_id FROM `+Schema+`.inbox_events
		WHERE source_owner = $1 AND source_event_id = $2`,
		first.SourceOwner, first.SourceEventID).Scan(&storedProcessed, &resultResourceID); err != nil {
		t.Fatalf("read processed inbox row: %v", err)
	}
	if storedProcessed.IsZero() || resultResourceID.String != "quote-1" {
		t.Fatalf("processed inbox row = %v/%q", storedProcessed, resultResourceID.String)
	}
}

func TestResourceCatalogIdempotencyReplayAndConflict(t *testing.T) {
	db := openResourceCatalogTestPostgres(t)
	catalogStore := New(db)
	ctx := context.Background()
	if err := catalogStore.Install(ctx); err != nil {
		t.Fatalf("install resource catalog schema: %v", err)
	}
	_ = catalogStore

	runID := time.Now().UnixNano()
	input := IdempotencyInput{
		ID:             fmt.Sprintf("idem-%d", runID),
		TenantScope:    "tenant-1",
		ActorScope:     "actor-1",
		OperationName:  "acceptQuote",
		IdempotencyKey: fmt.Sprintf("key-%d", runID),
		RequestSHA256:  HashRequestBody([]byte(`{"quoteId":"quote-1"}`)),
		ResourceID:     "quote-1",
		OperationID:    "op-quote-1",
		ResponseStatus: 201,
		ResponseBody:   json.RawMessage(`{"quoteId":"quote-1"}`),
	}

	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		if _, found, err := LookupIdempotency(ctx, tx, input); err != nil || found {
			return fmt.Errorf("first lookup found=%v err=%w", found, err)
		}
		return RecordIdempotency(ctx, tx, input)
	}); err != nil {
		t.Fatalf("record idempotency: %v", err)
	}

	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		record, found, err := LookupIdempotency(ctx, tx, input)
		if err != nil {
			return err
		}
		if !found || !record.Replayed {
			return fmt.Errorf("replay found=%v record=%+v", found, record)
		}
		if record.ResourceID != input.ResourceID || record.OperationID != input.OperationID ||
			record.ResponseStatus != input.ResponseStatus {
			return fmt.Errorf("replay returned another identity: %+v", record)
		}
		return nil
	}); err != nil {
		t.Fatalf("replay idempotency: %v", err)
	}

	differentBody := input
	differentBody.RequestSHA256 = HashRequestBody([]byte(`{"quoteId":"quote-2"}`))
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		_, found, err := LookupIdempotency(ctx, tx, differentBody)
		if !found || !errors.Is(err, ErrIdempotencyConflict) {
			return fmt.Errorf("conflict found=%v err=%v", found, err)
		}
		return err
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different body error = %v, want ErrIdempotencyConflict", err)
	}

	second := input
	second.ID = fmt.Sprintf("idem-second-%d", time.Now().UnixNano())
	second.ActorScope = "actor-2"
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return RecordIdempotency(ctx, tx, second)
	}); err != nil {
		t.Fatalf("record a second actor's key: %v", err)
	}
	reused := input
	reused.ID = fmt.Sprintf("idem-reused-%d", time.Now().UnixNano())
	reused.ResponseBody = json.RawMessage(`{"quoteId":"quote-1","replaced":true}`)
	if err := withResourceCatalogTx(t, db, func(tx *sql.Tx) error {
		return RecordIdempotency(ctx, tx, reused)
	}); err == nil {
		t.Fatal("re-recording the same scope and key must be rejected by the unique key")
	}
}
