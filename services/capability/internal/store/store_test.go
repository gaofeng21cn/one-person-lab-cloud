package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecideInboxComparesTheWholeImmutableIdentity(t *testing.T) {
	base := RecordedIdentity{
		EventType:         "build.artifact_confirmed.v1",
		SchemaVersion:     1,
		AggregateType:     "build_job",
		AggregateID:       "job-1",
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
		"event type":         func(r *RecordedIdentity) { r.EventType = "package.uploaded.v1" },
		"schema version":     func(r *RecordedIdentity) { r.SchemaVersion = 2 },
		"aggregate type":     func(r *RecordedIdentity) { r.AggregateType = "package_version" },
		"aggregate id":       func(r *RecordedIdentity) { r.AggregateID = "job-2" },
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
		EventType:         "capability.version_registered.v1",
		SchemaVersion:     1,
		AggregateType:     "capability_version",
		AggregateID:       "ver-1",
		AggregateRevision: 1,
		CorrelationID:     "req-1",
		Payload:           json.RawMessage(`{"capabilityVersionId":"ver-1"}`),
		OccurredAt:        time.Now().UTC(),
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	zeroRevision := valid
	zeroRevision.AggregateRevision = 0
	if err := zeroRevision.validate(); err == nil {
		t.Fatal("aggregate revision zero must be rejected")
	}
	hash := valid.PayloadSHA256()
	if len(hash) != 64 {
		t.Fatalf("payload hash %q must be a hex sha256", hash)
	}
	if hash != valid.PayloadSHA256() {
		t.Fatal("payload hash must be stable for identical bytes")
	}

	// Capability owns two specified event versions. Each resolves to its own fixed
	// aggregate type and payload id field, so neither may be satisfied by the
	// other's values.
	uploaded := valid
	uploaded.EventType = "package.uploaded.v1"
	uploaded.AggregateType = "package_version"
	uploaded.AggregateID = "pv-1"
	uploaded.Payload = json.RawMessage(`{"packageVersionId":"pv-1"}`)
	if err := uploaded.validate(); err != nil {
		t.Fatalf("the second specified capability event version was rejected: %v", err)
	}

	// The aggregate type is fixed by the specification for this exact event
	// version: an empty or freely supplied value must be rejected, never defaulted.
	missingAggregateType := valid
	missingAggregateType.AggregateType = ""
	if err := missingAggregateType.validate(); err == nil {
		t.Fatal("event without an aggregate type must be rejected")
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
		"payload without the id":      func(e *Event) { e.Payload = json.RawMessage(`{"buildJobId":"job-1"}`) },
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

// Capability consumes the build artifact confirmation: the Build job pushed and
// read the artifact back by digest, and Capability is the only writer that
// registers the resulting version.
func TestInboundEventValidationAndHashing(t *testing.T) {
	valid := InboundEvent{
		ID:                "inb-1",
		SourceOwner:       "build",
		SourceEventID:     "evt-1",
		EventType:         "build.artifact_confirmed.v1",
		SchemaVersion:     1,
		AggregateType:     "build_job",
		AggregateID:       "job-1",
		AggregateRevision: 2,
		Payload:           json.RawMessage(`{"buildJobId":"job-1"}`),
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid inbound event rejected: %v", err)
	}
	zeroRevision := valid
	zeroRevision.AggregateRevision = 0
	if err := zeroRevision.validate(); err == nil {
		t.Fatal("aggregate revision zero must be rejected")
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
		"aggregate type mismatch": func(e *InboundEvent) { e.AggregateType = "capability_version" },
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
	first := HashRequestBody([]byte(`{"uploadId":"up-1"}`))
	second := HashRequestBody([]byte(`{"uploadId":"up-1"}`))
	other := HashRequestBody([]byte(`{"uploadId":"up-2"}`))

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
		OperationName:  "completeUpload",
		IdempotencyKey: "key-1",
		RequestSHA256:  HashRequestBody([]byte(`{}`)),
		ResourceID:     "pv-1",
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
		Kind:       "complete_upload",
		ResourceID: "pv-1",
		Stage:      "upload_verification",
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
		"current_database() <> 'opl_capability'",
		"CREATE SCHEMA capability AUTHORIZATION opl_capability_owner",
		"CREATE TABLE capability.operations",
		"CREATE TABLE capability.outbox_events",
		"CREATE TABLE capability.inbox_events",
		"CREATE TABLE capability.idempotency_records",
		"COMMIT;",
	} {
		if !strings.Contains(query, marker) {
			t.Fatalf("embedded migration is missing %q", marker)
		}
	}
}
