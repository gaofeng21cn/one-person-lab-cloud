package ledger

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

func TestDomainEvidenceCannotUseGenericReceiptWriter(t *testing.T) {
	for _, kind := range []string{"package.uploaded.v1", "build.artifact_confirmed.v1", "build.failed.v1", "capability.version_registered.v1", "serve.agent_readiness_observed.v1"} {
		_, err := NewMemoryStore().RecordReceipt(context.Background(), ReceiptInput{Type: kind, Status: "completed", Surface: "cloud", WorkspaceID: "invented-workspace", IdempotencyKey: "forged-event"})
		if !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("generic writer accepted %s: %v", kind, err)
		}
	}
}

func TestServeReadinessEventValidationBindsRuntimeIdentity(t *testing.T) {
	event := &api.EventEnvelope{
		EventType: "serve.agent_readiness_observed.v1", Owner: "serve", AggregateId: "runtime-1",
		Payload: &api.EventEnvelope_RuntimeReadinessObserved{RuntimeReadinessObserved: &api.RuntimeReadinessObservedEvent{
			RuntimeInstanceId: "runtime-1", WorkspaceId: "workspace-1", DeploymentId: "deployment-1",
			Outcome: "confirmed", ApplicationAvailable: true, ReceiptId: proto.String("fabric-readback-1"),
		}},
	}
	if deployment, err := validateRuntimeReadinessEvent(event); err != nil || deployment != "deployment-1" {
		t.Fatalf("deployment=%q err=%v", deployment, err)
	}
	event.AggregateId = "other-runtime"
	if _, err := validateRuntimeReadinessEvent(event); !errors.Is(err, ErrInvalidReceiptInput) {
		t.Fatalf("identity mismatch err=%v", err)
	}
}
