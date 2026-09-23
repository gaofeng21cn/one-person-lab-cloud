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
		EventType:         "catalog.policy_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "catalog_policy_version",
		AggregateID:       "policy-1",
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
		"event type":         func(r *RecordedIdentity) { r.EventType = "workspace.state_changed.v1" },
		"schema version":     func(r *RecordedIdentity) { r.SchemaVersion = 2 },
		"aggregate type":     func(r *RecordedIdentity) { r.AggregateType = "capability_version" },
		"aggregate id":       func(r *RecordedIdentity) { r.AggregateID = "policy-2" },
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
		EventType:         "catalog.policy_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "catalog_policy_version",
		AggregateID:       "policy-1",
		AggregateRevision: 1,
		CorrelationID:     "req-1",
		Payload:           json.RawMessage(`{"policyVersionId":"policy-1"}`),
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

	// The aggregate type is an explicit producer-supplied field: an empty value
	// must be rejected, never defaulted.
	missingAggregateType := valid
	missingAggregateType.AggregateType = ""
	if err := missingAggregateType.validate(); err == nil {
		t.Fatal("event without an aggregate type must be rejected")
	}

	for name, mutate := range map[string]func(*Event){
		"missing id":             func(e *Event) { e.ID = "" },
		"missing event type":     func(e *Event) { e.EventType = "" },
		"missing schema version": func(e *Event) { e.SchemaVersion = 0 },
		"missing aggregate id":   func(e *Event) { e.AggregateID = "" },
		"negative revision":      func(e *Event) { e.AggregateRevision = -1 },
		"missing correlation":    func(e *Event) { e.CorrelationID = "" },
		"missing payload":        func(e *Event) { e.Payload = nil },
		"missing occurrence":     func(e *Event) { e.OccurredAt = time.Time{} },
		// The aggregate type and the payload field supplying the aggregate id are
		// fixed per exact (eventType, schemaVersion), so neither is free-form.
		"unspecified event version":   func(e *Event) { e.EventType = "catalog.policy_changed.v2" },
		"invented aggregate type":     func(e *Event) { e.AggregateType = "anything_goes" },
		"aggregate id from elsewhere": func(e *Event) { e.AggregateID = "not-in-payload" },
		"payload without the id":      func(e *Event) { e.Payload = json.RawMessage(`{"computePlanId":"plan-1"}`) },
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
	// The v2.26 consumer table lists no resource_catalog subscription yet, so this
	// fixture uses a specification-defined event version to exercise the store's
	// deduplication rule. It proves the generic Inbox mechanism, not a production
	// subscription, and no invented event type stands in for a real one.
	valid := InboundEvent{
		ID:                "inb-1",
		SourceOwner:       "resource_catalog",
		SourceEventID:     "evt-1",
		EventType:         "catalog.policy_changed.v1",
		SchemaVersion:     1,
		AggregateType:     "catalog_policy_version",
		AggregateID:       "policy-1",
		AggregateRevision: 2,
		Payload:           json.RawMessage(`{"policyVersionId":"policy-1"}`),
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid inbound event rejected: %v", err)
	}
	if len(valid.PayloadSHA256()) != 64 {
		t.Fatal("inbound payload hash must be a hex sha256")
	}
	for name, mutate := range map[string]func(*InboundEvent){
		"missing id":                func(e *InboundEvent) { e.ID = "" },
		"missing source owner":      func(e *InboundEvent) { e.SourceOwner = "" },
		"missing source event":      func(e *InboundEvent) { e.SourceEventID = "" },
		"missing event type":        func(e *InboundEvent) { e.EventType = "" },
		"missing schema version":    func(e *InboundEvent) { e.SchemaVersion = 0 },
		"negative revision":         func(e *InboundEvent) { e.AggregateRevision = -1 },
		"missing payload":           func(e *InboundEvent) { e.Payload = nil },
		"unspecified event version": func(e *InboundEvent) { e.EventType = "catalog.policy_changed.v2" },
		"aggregate type mismatch":   func(e *InboundEvent) { e.AggregateType = "capability_version" },
		"aggregate id mismatch":     func(e *InboundEvent) { e.AggregateID = "policy-2" },
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
	first := HashRequestBody([]byte(`{"computePlanId":"plan-1"}`))
	second := HashRequestBody([]byte(`{"computePlanId":"plan-1"}`))
	other := HashRequestBody([]byte(`{"computePlanId":"plan-2"}`))

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
		OperationName:  "acceptQuote",
		IdempotencyKey: "key-1",
		RequestSHA256:  HashRequestBody([]byte(`{}`)),
		ResourceID:     "quote-1",
		ResponseStatus: 201,
	}
	if err := validateIdempotencyInput(valid); err != nil {
		t.Fatalf("valid idempotency input rejected: %v", err)
	}
	for name, mutate := range map[string]func(*IdempotencyInput){
		"missing id":             func(i *IdempotencyInput) { i.ID = "" },
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
		Kind:       "reconcile",
		ResourceID: "quote-1",
		Stage:      "quote_binding",
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

// The owner's readiness is a real dependency check: an absent or closed
// database is never reported as ready.
func TestReadyFailsClosedWithoutAReachableDatabase(t *testing.T) {
	if err := New(nil).Ready(context.Background()); err == nil {
		t.Fatal("a store without a database must not be ready")
	}
	db, err := sql.Open("postgres", "connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := New(db).Ready(context.Background()); err == nil {
		t.Fatal("a closed database must not be ready")
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
	if migrations[0].Version != "0001_opl_resource_catalog" {
		t.Fatalf("embedded migration version = %q", migrations[0].Version)
	}
	query := migrations[0].Query
	for _, marker := range []string{
		"BEGIN;",
		"current_database() <> 'opl_resource_catalog'",
		"CREATE SCHEMA resource_catalog AUTHORIZATION opl_resource_catalog_owner",
		"CREATE TABLE resource_catalog.operations",
		"CREATE TABLE resource_catalog.outbox_events",
		"CREATE TABLE resource_catalog.outbox_deliveries",
		"CREATE TABLE resource_catalog.inbox_events",
		"CREATE TABLE resource_catalog.idempotency_records",
		"REVOKE UPDATE, DELETE ON resource_catalog.outbox_events FROM opl_resource_catalog_writer",
		"COMMIT;",
	} {
		if !strings.Contains(query, marker) {
			t.Fatalf("embedded migration is missing %q", marker)
		}
	}
}
