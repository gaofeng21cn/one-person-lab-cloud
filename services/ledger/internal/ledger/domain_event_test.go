package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

func TestDomainEvidenceCannotUseGenericReceiptWriter(t *testing.T) {
	for _, kind := range []string{"package.uploaded.v1", "build.artifact_confirmed.v1", "build.failed.v1", "capability.version_registered.v1"} {
		_, err := NewMemoryStore().RecordReceipt(context.Background(), ReceiptInput{Type: kind, Status: "completed", Surface: "cloud", WorkspaceID: "invented-workspace", IdempotencyKey: "forged-event"})
		if !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("generic writer accepted %s: %v", kind, err)
		}
	}
}

func TestReadinessReceiptStatusRequiresConsistentObservedOutcome(t *testing.T) {
	base := &api.EventEnvelope{Owner: "serve", AggregateId: "dep-1"}
	tests := []struct {
		name    string
		payload *api.RuntimeReadinessObservedEvent
		status  string
		valid   bool
	}{
		{name: "confirmed", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "dep-1", Outcome: "confirmed", ApplicationAvailable: true, ReceiptId: proto.String("readiness-1")}, status: "completed", valid: true},
		{name: "rejected", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "dep-1", Outcome: "rejected"}, status: "failed", valid: true},
		{name: "unknown", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "dep-1", Outcome: "unknown"}, status: "review_required", valid: true},
		{name: "confirmed without receipt", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "dep-1", Outcome: "confirmed", ApplicationAvailable: true}, valid: false},
		{name: "unknown claims application", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "dep-1", Outcome: "unknown", ApplicationAvailable: true}, valid: false},
		{name: "negative model version", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "dep-1", Outcome: "unknown", AppliedModelConfigurationVersion: -1}, valid: false},
		{name: "wrong aggregate", payload: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-1", WorkspaceId: "ws-1", DeploymentId: "other", Outcome: "unknown"}, valid: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := proto.Clone(base).(*api.EventEnvelope)
			event.Payload = &api.EventEnvelope_RuntimeReadinessObserved{RuntimeReadinessObserved: tc.payload}
			got, err := readinessReceiptStatus(event, tc.payload)
			if tc.valid {
				if err != nil || got != tc.status {
					t.Fatalf("status=%q err=%v", got, err)
				}
			} else if !errors.Is(err, ErrInvalidReceiptInput) {
				t.Fatalf("err=%v, want invalid input", err)
			}
		})
	}
}

// catalogPolicyEventFixture is the exact platform-scope publication shape the
// Resource Catalog Outbox delivers: owner resource_catalog, scope platform, no
// tenant id, and a payload whose policy version is the aggregate.
func catalogPolicyEventFixture() *api.EventEnvelope {
	return &api.EventEnvelope{
		EventId: "evt-catalog-policy-1", EventType: "catalog.policy_changed.v1", SchemaVersion: 1,
		Owner: "resource_catalog", Scope: "platform", AggregateId: "policy-v1", AggregateVersion: 1,
		RequestId: "req-catalog-1", OccurredAt: timestamppb.New(time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)),
		Payload: &api.EventEnvelope_CatalogPolicyChanged{CatalogPolicyChanged: &api.CatalogPolicyChangedEvent{
			PolicyVersionId: "policy-v1", PolicyKind: "price", ValidFrom: timestamppb.New(time.Date(2026, 10, 4, 5, 0, 0, 0, time.UTC)),
		}},
	}
}

// TestCatalogPolicyEventPersistsPlatformEvidenceAndDeduplicates proves the
// platform-scope publication from the real Resource Catalog producer is
// recordable, readable by exact identity, and immutable under replay.
func TestCatalogPolicyEventPersistsPlatformEvidenceAndDeduplicates(t *testing.T) {
	db := openLedgerTestPostgres(t)
	store := NewPostgresStore(db)
	if err := store.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	event := catalogPolicyEventFixture()
	first, err := store.RecordDomainEvent(ctx, event)
	if err != nil {
		t.Fatalf("record catalog policy event: %v", err)
	}
	if first.Type != "catalog.policy_changed.v1" || first.Status != "completed" || first.OrganizationID != "platform" || first.WorkspaceID != "" ||
		first.ArtifactID != evidenceDigest("policy-v1", "price") || first.Owner["name"] != "resource_catalog" || first.IdempotencyKey != "" {
		t.Fatalf("stored evidence = %+v", first)
	}
	var rows int
	var requestHash string
	if err := db.QueryRowContext(ctx, `SELECT count(*), max(request_hash) FROM evidence_receipts WHERE receipt_type='catalog.policy_changed.v1' AND idempotency_key='domain:resource_catalog:evt-catalog-policy-1'`).Scan(&rows, &requestHash); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || requestHash == "" {
		t.Fatalf("persisted rows=%d hash=%q", rows, requestHash)
	}
	// The identical bytes replay as the same receipt, never a second row.
	replay, err := store.RecordDomainEvent(ctx, proto.Clone(event).(*api.EventEnvelope))
	if err != nil || !replay.Replayed || replay.ReceiptID != first.ReceiptID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	// The same event id carrying different policy bytes is refused, not replaced.
	conflict := proto.Clone(event).(*api.EventEnvelope)
	conflict.GetCatalogPolicyChanged().PolicyKind = "retention"
	if _, err = store.RecordDomainEvent(ctx, conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting policy bytes error = %v, want ErrIdempotencyConflict", err)
	}
}

// TestCatalogPolicyEventRejectsScopeAndIdentityDrift proves the platform-scope
// publication cannot be smuggled through tenant shapes, foreign owners or
// mismatched aggregates.
func TestCatalogPolicyEventRejectsScopeAndIdentityDrift(t *testing.T) {
	db := openLedgerTestPostgres(t)
	store := NewPostgresStore(db)
	if err := store.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tests := []struct {
		name   string
		mutate func(*api.EventEnvelope)
	}{
		{name: "tenant scope", mutate: func(e *api.EventEnvelope) { e.Scope, e.TenantId = "tenant", "tenant-1" }},
		{name: "tenant id on platform scope", mutate: func(e *api.EventEnvelope) { e.TenantId = "tenant-1" }},
		{name: "foreign owner", mutate: func(e *api.EventEnvelope) { e.Owner = "workspace" }},
		{name: "wrong aggregate", mutate: func(e *api.EventEnvelope) { e.AggregateId = "other-policy" }},
		{name: "missing policy version", mutate: func(e *api.EventEnvelope) { e.GetCatalogPolicyChanged().PolicyVersionId = "" }},
		{name: "missing policy kind", mutate: func(e *api.EventEnvelope) { e.GetCatalogPolicyChanged().PolicyKind = "" }},
		{name: "missing valid from", mutate: func(e *api.EventEnvelope) { e.GetCatalogPolicyChanged().ValidFrom = nil }},
		{name: "nil payload", mutate: func(e *api.EventEnvelope) { e.Payload = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := catalogPolicyEventFixture()
			event.EventId = "evt-" + strings.ReplaceAll(tc.name, " ", "-")
			tc.mutate(event)
			if _, err := store.RecordDomainEvent(ctx, event); !errors.Is(err, ErrInvalidReceiptInput) {
				t.Fatalf("error = %v, want ErrInvalidReceiptInput", err)
			}
		})
	}
	// The tenancy rule is not loosened for the tenant-scoped events either: a
	// tenant event without its tenant stays refused.
	missing := &api.EventEnvelope{EventId: "evt-tenant-missing", EventType: "package.uploaded.v1", SchemaVersion: 1, Owner: "capability", Scope: "tenant", AggregateId: "pv-1", AggregateVersion: 1, RequestId: "req-1", OccurredAt: timestamppb.New(time.Now().UTC()), Payload: &api.EventEnvelope_PackageUploaded{PackageUploaded: &api.PackageUploadedEvent{PackageId: "p-1", PackageVersionId: "pv-1", Sha256: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1}}}
	if _, err := store.RecordDomainEvent(ctx, missing); !errors.Is(err, ErrInvalidReceiptInput) {
		t.Fatalf("tenant event without tenant = %v, want ErrInvalidReceiptInput", err)
	}
}

// firstChainObservationFixtures are the three contract-listed first-chain
// observation events in their legal tenant-scope shape. They are typed
// fixtures: producer delivery loops belong to the Serve and Gateway owners.
func firstChainObservationFixtures() map[string]*api.EventEnvelope {
	now := timestamppb.New(time.Date(2026, 10, 4, 7, 0, 0, 0, time.UTC))
	return map[string]*api.EventEnvelope{
		"serve.access_observed.v1": {
			EventId: "evt-access-1", EventType: "serve.access_observed.v1", SchemaVersion: 1,
			Owner: "serve", Scope: "tenant", TenantId: "tenant-1", AggregateId: "switch-1", AggregateVersion: 1,
			RequestId: "req-access-1", OccurredAt: now,
			Payload: &api.EventEnvelope_RouteObserved{RouteObserved: &api.RouteObservedEvent{WorkspaceId: "ws-1", SwitchId: "switch-1", ActionKind: "activate", RouteGeneration: 2, ExecutionEpoch: 1, RouteRevision: "rev-1", Outcome: "confirmed", RouteReceiptId: proto.String("route-receipt-1")}},
		},
		"fabric.resources_observed.v1": {
			EventId: "evt-resources-1", EventType: "fabric.resources_observed.v1", SchemaVersion: 1,
			Owner: "serve", Scope: "tenant", TenantId: "tenant-1", AggregateId: "resource-set-1", AggregateVersion: 1,
			RequestId: "req-resources-1", OccurredAt: now,
			Payload: &api.EventEnvelope_ResourcesObserved{ResourcesObserved: &api.ResourcesObservedEvent{ResourceSetId: "resource-set-1", WorkspaceId: "ws-1", ResourceActionId: "action-1", Outcome: "confirmed", AbsenceConfirmed: false}},
		},
		"wallet.operation_observed.v1": {
			EventId: "evt-wallet-1", EventType: "wallet.operation_observed.v1", SchemaVersion: 1,
			Owner: "gateway", Scope: "tenant", TenantId: "tenant-1", AggregateId: "wallet-op-1", AggregateVersion: 1,
			RequestId: "req-wallet-1", OccurredAt: now,
			Payload: &api.EventEnvelope_WalletOperationObserved{WalletOperationObserved: &api.WalletOperationObservedEvent{WalletOperationId: "wallet-op-1", WorkspaceId: "ws-1", Kind: "charge", Status: "confirmed", AmountUsdMicros: 52_580_000, ReceiptId: proto.String("wallet-receipt-1")}},
		},
	}
}

// TestFirstChainObservationEventsPersistWithExactIdentity proves the three
// observation events are recordable with contract-exact identity: the aggregate
// is the observation's own subject, the tenant and workspace are stored, the
// derived digest is stable, and identical bytes replay rather than duplicate.
func TestFirstChainObservationEventsPersistWithExactIdentity(t *testing.T) {
	db := openLedgerTestPostgres(t)
	store := NewPostgresStore(db)
	if err := store.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, event := range firstChainObservationFixtures() {
		first, err := store.RecordDomainEvent(ctx, proto.Clone(event).(*api.EventEnvelope))
		if err != nil {
			t.Fatalf("%s record: %v", name, err)
		}
		if first.Type != name || first.OrganizationID != "tenant-1" || first.WorkspaceID != "ws-1" || first.ArtifactID == "" || first.RequestID != event.RequestId || first.Owner["name"] != event.Owner {
			t.Fatalf("%s stored evidence = %+v", name, first)
		}
		replay, err := store.RecordDomainEvent(ctx, proto.Clone(event).(*api.EventEnvelope))
		if err != nil || !replay.Replayed || replay.ReceiptID != first.ReceiptID {
			t.Fatalf("%s replay = %+v err=%v", name, replay, err)
		}
		var rows int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1 AND idempotency_key='domain:'||$2||':'||$3`, name, event.Owner, event.EventId).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("%s rows=%d err=%v", name, rows, err)
		}
	}
}

// TestFirstChainObservationEventsRejectIdentityDrift proves the contract-exact
// guards for the three observation decoders, including that the wallet
// observation is validated as owner=gateway metadata and never routed into the
// funded-charge writer.
func TestFirstChainObservationEventsRejectIdentityDrift(t *testing.T) {
	base := firstChainObservationFixtures()
	access := func() *api.EventEnvelope { return proto.Clone(base["serve.access_observed.v1"]).(*api.EventEnvelope) }
	resources := func() *api.EventEnvelope {
		return proto.Clone(base["fabric.resources_observed.v1"]).(*api.EventEnvelope)
	}
	wallet := func() *api.EventEnvelope {
		return proto.Clone(base["wallet.operation_observed.v1"]).(*api.EventEnvelope)
	}
	tests := []struct {
		name   string
		event  *api.EventEnvelope
		mutate func(*api.EventEnvelope)
	}{
		{"access foreign owner", access(), func(e *api.EventEnvelope) { e.Owner = "gateway" }},
		// Platform scope is refused by RecordDomainEvent's scope switch before the
		// payload decoder runs; that guard is exercised in the wire rejection matrix.
		{"access aggregate drift", access(), func(e *api.EventEnvelope) { e.AggregateId = "other-switch" }},
		{"access missing workspace", access(), func(e *api.EventEnvelope) { e.GetRouteObserved().WorkspaceId = "" }},
		{"access unknown action", access(), func(e *api.EventEnvelope) { e.GetRouteObserved().ActionKind = "swap" }},
		{"access negative epoch", access(), func(e *api.EventEnvelope) { e.GetRouteObserved().ExecutionEpoch = -1 }},
		{"access missing revision", access(), func(e *api.EventEnvelope) { e.GetRouteObserved().RouteRevision = "" }},
		{"access bad outcome", access(), func(e *api.EventEnvelope) { e.GetRouteObserved().Outcome = "maybe" }},
		{"access empty optional receipt", access(), func(e *api.EventEnvelope) { e.GetRouteObserved().RouteReceiptId = proto.String("") }},
		{"access nil payload", access(), func(e *api.EventEnvelope) { e.Payload = nil }},
		{"resources aggregate drift", resources(), func(e *api.EventEnvelope) { e.AggregateId = "other-set" }},
		{"resources missing set", resources(), func(e *api.EventEnvelope) { e.GetResourcesObserved().ResourceSetId = "" }},
		{"resources missing action", resources(), func(e *api.EventEnvelope) { e.GetResourcesObserved().ResourceActionId = "" }},
		{"resources absence without confirm", resources(), func(e *api.EventEnvelope) {
			e.GetResourcesObserved().Outcome = "unknown"
			e.GetResourcesObserved().AbsenceConfirmed = true
		}},
		{"resources bad outcome", resources(), func(e *api.EventEnvelope) { e.GetResourcesObserved().Outcome = "done" }},
		{"wallet foreign owner", wallet(), func(e *api.EventEnvelope) { e.Owner = "serve" }},
		{"wallet aggregate drift", wallet(), func(e *api.EventEnvelope) { e.AggregateId = "other-op" }},
		{"wallet missing operation", wallet(), func(e *api.EventEnvelope) { e.GetWalletOperationObserved().WalletOperationId = "" }},
		{"wallet bad kind", wallet(), func(e *api.EventEnvelope) { e.GetWalletOperationObserved().Kind = "withdrawal" }},
		{"wallet bad status", wallet(), func(e *api.EventEnvelope) { e.GetWalletOperationObserved().Status = "settled" }},
		{"wallet negative amount", wallet(), func(e *api.EventEnvelope) { e.GetWalletOperationObserved().AmountUsdMicros = -1 }},
		{"wallet bad purpose", wallet(), func(e *api.EventEnvelope) { e.GetWalletOperationObserved().Purpose = proto.String("donation") }},
		{"wallet half coverage", wallet(), func(e *api.EventEnvelope) {
			e.GetWalletOperationObserved().CoverageStart = timestamppb.New(time.Now().UTC())
		}},
		{"wallet inverted coverage", wallet(), func(e *api.EventEnvelope) {
			e.GetWalletOperationObserved().CoverageStart = timestamppb.New(time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC))
			e.GetWalletOperationObserved().CoverageEnd = timestamppb.New(time.Date(2026, 10, 4, 7, 0, 0, 0, time.UTC))
		}},
	}
	// The decoder guards are pure functions that run before any store access, so
	// the rejection matrix calls them directly and needs no database.
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.event.EventId = fmt.Sprintf("evt-drift-%d", i)
			tc.mutate(tc.event)
			var err error
			switch tc.event.EventType {
			case "serve.access_observed.v1":
				_, _, err = accessObservedEvidence(tc.event, tc.event.GetRouteObserved())
			case "fabric.resources_observed.v1":
				_, _, err = resourcesObservedEvidence(tc.event, tc.event.GetResourcesObserved())
			case "wallet.operation_observed.v1":
				_, _, err = walletObservedEvidence(tc.event, tc.event.GetWalletOperationObserved())
			default:
				t.Fatalf("unexpected fixture type %s", tc.event.EventType)
			}
			if !errors.Is(err, ErrInvalidReceiptInput) {
				t.Fatalf("%s error = %v, want ErrInvalidReceiptInput", tc.name, err)
			}
		})
	}
	// The wallet observation is metadata, not a funded charge: it never becomes a
	// wallet action receipt and stays refused by the generic writer.
	for _, kind := range []string{"serve.access_observed.v1", "fabric.resources_observed.v1", "wallet.operation_observed.v1"} {
		if _, err := NewMemoryStore().RecordReceipt(context.Background(), ReceiptInput{Type: kind, Status: "completed", Surface: "cloud", WorkspaceID: "ws-1", IdempotencyKey: "forged-" + kind}); !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("generic writer accepted %s: %v", kind, err)
		}
	}
}

// TestNewlyTypedEventsCannotUseGenericReceiptWriter extends the single-writer
// rule to the two event types whose Ledger consumer now accepts real producers.
func TestNewlyTypedEventsCannotUseGenericReceiptWriter(t *testing.T) {
	for _, kind := range []string{"serve.agent_readiness_observed.v1", "catalog.policy_changed.v1", "serve.access_observed.v1", "fabric.resources_observed.v1", "wallet.operation_observed.v1"} {
		_, err := NewMemoryStore().RecordReceipt(context.Background(), ReceiptInput{Type: kind, Status: "completed", Surface: "cloud", OrganizationID: "platform", IdempotencyKey: "forged-" + kind})
		if !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("generic writer accepted %s: %v", kind, err)
		}
	}
}

// TestDomainEventEvidenceCarriesTheSameSecretGate proves the receipt secret gate
// rejects a credential-shaped key in the exact InputRefs shape RecordDomainEvent
// builds for a domain event, so no future decoder path can smuggle one through.
func TestDomainEventEvidenceCarriesTheSameSecretGate(t *testing.T) {
	// Every domain-event receipt type shares one gate: the exact InputRefs shape
	// RecordDomainEvent builds is refused whenever a credential-shaped key appears
	// anywhere inside it, so no decoder path can smuggle one into a stored row.
	for _, kind := range []string{"catalog.policy_changed.v1", "serve.agent_readiness_observed.v1", "serve.access_observed.v1", "fabric.resources_observed.v1", "wallet.operation_observed.v1"} {
		for key, value := range map[string]any{
			"nested":     map[string]any{"apiKey": "must-not-persist"},
			"credential": []any{map[string]any{"token": "must-not-persist"}},
			"kubeconfig": "must-not-persist",
		} {
			input := ReceiptInput{
				Type: kind, Status: "completed", Surface: "cloud", OrganizationID: "tenant-1",
				RequestID: "req-gate", Owner: map[string]any{"name": "serve"},
				InputRefs: map[string]any{"domainEvent": map[string]any{key: value}}, IdempotencyKey: "domain:serve:evt-gate",
			}
			if !containsForbiddenReceiptKey(input) {
				t.Fatalf("secret gate accepted a credential-shaped key under %s for %s", key, kind)
			}
		}
	}
	// A policy event whose only content is the contract's own fields passes.
	clean := ReceiptInput{
		Type: "catalog.policy_changed.v1", Status: "completed", Surface: "cloud", OrganizationID: "platform",
		RequestID: "req-gate", Owner: map[string]any{"name": "resource_catalog"},
		InputRefs: map[string]any{"domainEvent": map[string]any{"policyVersionId": "policy-v1", "policyKind": "price"}}, IdempotencyKey: "domain:resource_catalog:evt-gate",
	}
	if containsForbiddenReceiptKey(clean) {
		t.Fatal("secret gate refused the legal policy event shape")
	}
}
