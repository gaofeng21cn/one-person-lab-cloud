package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestDecideInboxComparesTheWholeImmutableIdentity(t *testing.T) {
	base := RecordedIdentity{
		EventType:         "fabric.resources_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "resource_set",
		AggregateID:       "rs-1",
		AggregateRevision: 2,
		PayloadSHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	if got := DecideInbox(RecordedIdentity{}, false, base); got != InboxCommit {
		t.Fatalf("first delivery = %q, want %q", got, InboxCommit)
	}
	if got := DecideInbox(base, true, base); got != InboxDuplicate {
		t.Fatalf("identical repeat = %q, want %q", got, InboxDuplicate)
	}

	// Changing any part of the immutable identity is a conflict, not a duplicate,
	// so a producer cannot reuse a source event id to rewrite a recorded fact.
	for name, mutate := range map[string]func(*RecordedIdentity){
		"event type":         func(r *RecordedIdentity) { r.EventType = "runtime.readiness_observed.v1" },
		"schema version":     func(r *RecordedIdentity) { r.SchemaVersion = 2 },
		"aggregate type":     func(r *RecordedIdentity) { r.AggregateType = "runtime_instance" },
		"aggregate id":       func(r *RecordedIdentity) { r.AggregateID = "rs-2" },
		"aggregate revision": func(r *RecordedIdentity) { r.AggregateRevision = 3 },
		"payload bytes": func(r *RecordedIdentity) {
			r.PayloadSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
	} {
		t.Run(name, func(t *testing.T) {
			incoming := base
			mutate(&incoming)
			if got := DecideInbox(base, true, incoming); got != InboxConflict {
				t.Fatalf("changed %s = %q, want %q", name, got, InboxConflict)
			}
		})
	}
}

func TestEventValidationRejectsIncompleteInputAndHashesPayload(t *testing.T) {
	valid := Event{
		ID:                "evt-1",
		EventType:         "runtime.readiness_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "runtime_instance",
		AggregateID:       "rt-1",
		AggregateRevision: 1,
		CorrelationID:     "req-1",
		Payload:           json.RawMessage(`{"runtimeInstanceId":"rt-1"}`),
		OccurredAt:        time.Now().UTC(),
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	hash := valid.PayloadSHA256()
	if len(hash) != 64 {
		t.Fatalf("payload hash %q must be a hex sha256", hash)
	}
	if hash != valid.PayloadSHA256() {
		t.Fatal("payload hash must be stable for identical bytes")
	}

	for name, mutate := range map[string]func(*Event){
		"missing id":                  func(e *Event) { e.ID = "" },
		"missing event type":          func(e *Event) { e.EventType = "" },
		"missing schema version":      func(e *Event) { e.SchemaVersion = 0 },
		"missing aggregate id":        func(e *Event) { e.AggregateID = "" },
		"negative revision":           func(e *Event) { e.AggregateRevision = -1 },
		"missing correlation":         func(e *Event) { e.CorrelationID = "" },
		"missing payload":             func(e *Event) { e.Payload = nil },
		"missing occurrence":          func(e *Event) { e.OccurredAt = time.Time{} },
		"invented event version":      func(e *Event) { e.EventType = "resource_catalog.quote_accepted.v1" },
		"invented aggregate type":     func(e *Event) { e.AggregateType = "anything_goes" },
		"aggregate id from elsewhere": func(e *Event) { e.AggregateID = "not-in-payload" },
		"payload without the id":      func(e *Event) { e.Payload = json.RawMessage(`{"resourceSetId":"rs-1"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.validate(); err == nil {
				t.Fatalf("event with %s must be rejected", name)
			}
		})
	}
}

func TestInboundEventValidationAndHashing(t *testing.T) {
	valid := InboundEvent{
		ID:                "inb-1",
		SourceOwner:       "fabric",
		SourceEventID:     "evt-1",
		EventType:         "fabric.resources_observed.v1",
		SchemaVersion:     1,
		AggregateType:     "resource_set",
		AggregateID:       "rs-1",
		AggregateRevision: 2,
		Payload:           json.RawMessage(`{"resourceSetId":"rs-1"}`),
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid inbound event rejected: %v", err)
	}
	if len(valid.PayloadSHA256()) != 64 {
		t.Fatal("inbound payload hash must be a hex sha256")
	}
	for name, mutate := range map[string]func(*InboundEvent){
		"missing source owner":    func(e *InboundEvent) { e.SourceOwner = "" },
		"missing source event":    func(e *InboundEvent) { e.SourceEventID = "" },
		"negative revision":       func(e *InboundEvent) { e.AggregateRevision = -1 },
		"missing payload":         func(e *InboundEvent) { e.Payload = nil },
		"invented event version":  func(e *InboundEvent) { e.EventType = "resource_catalog.quote_accepted.v1" },
		"aggregate type mismatch": func(e *InboundEvent) { e.AggregateType = "runtime_instance" },
		"aggregate id mismatch":   func(e *InboundEvent) { e.AggregateID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := candidate.validate(); err == nil {
				t.Fatalf("inbound event with %s must be rejected", name)
			}
		})
	}
}

func TestHashRequestBodyIsStableAndContentSensitive(t *testing.T) {
	first := HashRequestBody([]byte(`{"runtimeInstanceId":"rt-1"}`))
	second := HashRequestBody([]byte(`{"runtimeInstanceId":"rt-1"}`))
	other := HashRequestBody([]byte(`{"runtimeInstanceId":"rt-2"}`))

	if len(first) != 64 {
		t.Fatalf("request hash %q must be a hex sha256", first)
	}
	if first != second {
		t.Fatal("identical normalized bodies must hash identically")
	}
	if first == other {
		t.Fatal("different normalized bodies must not share a hash")
	}
}

func TestIdempotencyInputValidation(t *testing.T) {
	valid := IdempotencyInput{
		ID:             "idem-1",
		TenantScope:    "tenant-1",
		ActorScope:     "actor-1",
		OperationName:  "deployRuntime",
		IdempotencyKey: "key-1",
		RequestSHA256:  HashRequestBody([]byte(`{}`)),
		ResourceID:     "rt-1",
		ResponseStatus: 202,
	}
	if err := validateIdempotencyInput(valid); err != nil {
		t.Fatalf("valid idempotency input rejected: %v", err)
	}
	for name, mutate := range map[string]func(*IdempotencyInput){
		"missing tenant scope":   func(i *IdempotencyInput) { i.TenantScope = "" },
		"missing actor scope":    func(i *IdempotencyInput) { i.ActorScope = "" },
		"missing operation name": func(i *IdempotencyInput) { i.OperationName = "" },
		"missing key":            func(i *IdempotencyInput) { i.IdempotencyKey = "" },
		"bad request hash":       func(i *IdempotencyInput) { i.RequestSHA256 = "short" },
		"missing resource":       func(i *IdempotencyInput) { i.ResourceID = "" },
		"bad response status":    func(i *IdempotencyInput) { i.ResponseStatus = 99 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := validateIdempotencyInput(candidate); err == nil {
				t.Fatalf("idempotency input with %s must be rejected", name)
			}
		})
	}
}

func TestOperationTerminalStates(t *testing.T) {
	for status, want := range map[string]bool{
		OperationAccepted:        false,
		OperationRunning:         false,
		OperationAwaitingConfirm: false,
		OperationSucceeded:       true,
		OperationFailed:          true,
		OperationCancelled:       true,
		OperationNeedsAttention:  false,
	} {
		if got := (Operation{Status: status}).Terminal(); got != want {
			t.Fatalf("Operation{status:%q}.Terminal() = %v, want %v", status, got, want)
		}
	}
}

func TestOperationInputRequiresIdentityAndCommand(t *testing.T) {
	valid := OperationInput{
		ID:         "op-1",
		ActorID:    "actor-1",
		Kind:       "runtime_deploy",
		ResourceID: "rt-1",
		Stage:      "runtime",
		RequestID:  "req-1",
	}
	if err := validateOperationInput(valid); err != nil {
		t.Fatalf("valid operation input rejected: %v", err)
	}
	for _, field := range []string{"ID", "ActorID", "Kind", "ResourceID", "Stage", "RequestID"} {
		candidate := valid
		switch field {
		case "ID":
			candidate.ID = "  "
		case "ActorID":
			candidate.ActorID = ""
		case "Kind":
			candidate.Kind = ""
		case "ResourceID":
			candidate.ResourceID = ""
		case "Stage":
			candidate.Stage = ""
		case "RequestID":
			candidate.RequestID = ""
		}
		if err := validateOperationInput(candidate); err == nil {
			t.Fatalf("operation input with a missing %s must be rejected", field)
		}
	}
}

// Readiness is the decision behind GET /readyz: an unreachable or absent owner
// database must surface as not-ready rather than as healthy.
func TestReadinessFailsClosedWhenTheOwnerDatabaseIsUnavailable(t *testing.T) {
	ctx := context.Background()
	if err := (&Store{}).Ready(ctx); err == nil {
		t.Fatal("a store without a database must not report ready")
	}
	if err := New(nil).Ready(ctx); err == nil {
		t.Fatal("a store over a nil handle must not report ready")
	}

	db, err := sql.Open("postgres", "host=127.0.0.1 port=1 dbname=opl_runtime_control user=postgres sslmode=disable connect_timeout=1")
	if err != nil {
		t.Fatalf("open handle: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close handle: %v", err)
	}
	if err := New(db).Ready(ctx); err == nil {
		t.Fatal("a store over a closed handle must not report ready")
	}
}

func TestEmbeddedMigrationIsTheVerbatimOwnerBlock(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected 1 embedded migration, got %d", len(migrations))
	}
	query := migrations[0].Query
	for _, marker := range []string{
		"BEGIN;",
		"current_database() <> 'opl_runtime_control'",
		"CREATE SCHEMA runtime_control AUTHORIZATION opl_runtime_control_owner",
		"CREATE TABLE runtime_control.runtime_instances",
		"CREATE TABLE runtime_control.runtime_actions",
		"CREATE TABLE runtime_control.operations",
		"CREATE TABLE runtime_control.outbox_events",
		"CREATE TABLE runtime_control.outbox_deliveries",
		"CREATE TABLE runtime_control.inbox_events",
		"CREATE TABLE runtime_control.idempotency_records",
		"REVOKE UPDATE, DELETE ON runtime_control.outbox_events FROM opl_runtime_control_writer",
		"COMMIT;",
	} {
		if !strings.Contains(query, marker) {
			t.Fatalf("embedded migration is missing %q", marker)
		}
	}
}
