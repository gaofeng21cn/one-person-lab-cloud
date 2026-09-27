package ledger

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
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
