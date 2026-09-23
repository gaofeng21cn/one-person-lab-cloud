package contracts

import (
	"encoding/json"
	"errors"
	"testing"
)

// The specification fixes the aggregate identity of every versioned event. These
// cases pin the contract that Outbox and Inbox rows must satisfy rather than
// accepting a free-form aggregate type or a guessed aggregate id.
func TestEventIdentitiesCoverEverySpecifiedEventVersion(t *testing.T) {
	if len(EventIdentities) != 18 {
		t.Fatalf("expected 18 specified event identities, got %d", len(EventIdentities))
	}
	seen := map[string]bool{}
	for _, identity := range EventIdentities {
		key := identity.EventType
		if seen[key] {
			t.Fatalf("duplicate event type %q", key)
		}
		seen[key] = true
		switch {
		case identity.EventType == "":
			t.Fatal("event type is required")
		case identity.SchemaVersion <= 0:
			t.Fatalf("%s: schema version must be positive", identity.EventType)
		case identity.Owner == "":
			t.Fatalf("%s: owner is required", identity.EventType)
		case identity.AggregateType == "":
			t.Fatalf("%s: aggregate type is required", identity.EventType)
		case identity.AggregateIDField == "":
			t.Fatalf("%s: aggregate id payload field is required", identity.EventType)
		}
	}
}

func TestLookupEventIdentityIsExactOnTypeAndVersion(t *testing.T) {
	identity, ok := LookupEventIdentity("build.artifact_confirmed.v1", 1)
	if !ok {
		t.Fatal("build.artifact_confirmed.v1@1 must be specified")
	}
	if identity.AggregateType != "build_job" || identity.AggregateIDField != "buildJobId" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if identity.Owner != "build" {
		t.Fatalf("owner = %q, want build", identity.Owner)
	}

	// An unknown schema version or event type is not silently accepted.
	if _, ok := LookupEventIdentity("build.artifact_confirmed.v1", 2); ok {
		t.Fatal("an unspecified schema version must not resolve")
	}
	if _, ok := LookupEventIdentity("build.artifact_confirmed.v2", 1); ok {
		t.Fatal("an unspecified event type must not resolve")
	}
	if _, ok := LookupEventIdentity("", 1); ok {
		t.Fatal("an empty event type must not resolve")
	}
}

func TestAggregateIDFromPayloadRejectsAnythingButTheExactField(t *testing.T) {
	identity, ok := LookupEventIdentity("fabric.route_observed.v1", 1)
	if !ok {
		t.Fatal("fabric.route_observed.v1@1 must be specified")
	}
	if identity.AggregateType != "route_binding" || identity.AggregateIDField != "routeBindingId" {
		t.Fatalf("unexpected identity: %+v", identity)
	}

	id, err := identity.AggregateIDFromPayload([]byte(`{"routeBindingId":"rb-1","outcome":"confirmed"}`))
	if err != nil || id != "rb-1" {
		t.Fatalf("AggregateIDFromPayload = %q, %v", id, err)
	}

	// The workspace id in the same payload must never stand in for the route object.
	for name, payload := range map[string]string{
		"missing field": `{"workspaceId":"ws-1"}`,
		"empty value":   `{"routeBindingId":""}`,
		"non-string":    `{"routeBindingId":7}`,
		"null":          `{"routeBindingId":null}`,
		"empty payload": ``,
		"invalid json":  `{"routeBindingId":`,
		"not an object": `["routeBindingId"]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := identity.AggregateIDFromPayload([]byte(payload)); !errors.Is(err, ErrEventAggregateIdentityMismatch) {
				t.Fatalf("payload %s must be rejected, got %v", payload, err)
			}
		})
	}
}

// Every specified event must be able to resolve its own aggregate id from a
// minimal payload, so a producer cannot satisfy the column with a free string.
func TestEverySpecifiedEventResolvesItsAggregateID(t *testing.T) {
	for _, identity := range EventIdentities {
		payload, err := json.Marshal(map[string]string{identity.AggregateIDField: "id-1"})
		if err != nil {
			t.Fatalf("%s: build payload: %v", identity.EventType, err)
		}
		id, err := identity.AggregateIDFromPayload(payload)
		if err != nil || id != "id-1" {
			t.Fatalf("%s: AggregateIDFromPayload = %q, %v", identity.EventType, id, err)
		}
	}
}

func TestEventIdentityCarriesTheSpecifiedProducerAndSubscribers(t *testing.T) {
	identity, ok := LookupEventIdentity("subscription.renewal_settings_changed.v1", 1)
	if !ok {
		t.Fatal("subscription.renewal_settings_changed.v1@1 must be specified")
	}
	if identity.Owner != "workspace" {
		t.Fatalf("producer owner = %q, want workspace", identity.Owner)
	}
	for _, owner := range []string{"gateway", "ledger", "tenant"} {
		if !identity.Subscribed(owner) {
			t.Errorf("specified subscriber %q was not generated", owner)
		}
	}
	for _, owner := range []string{"build", "capability", "workspace", ""} {
		if identity.Subscribed(owner) {
			t.Errorf("unspecified subscriber %q was accepted", owner)
		}
	}
}
