package ledger

import (
	"context"
	"errors"
	"testing"
)

func TestDomainEvidenceCannotUseGenericReceiptWriter(t *testing.T) {
	for _, kind := range []string{"package.uploaded.v1", "build.artifact_confirmed.v1", "build.failed.v1", "capability.version_registered.v1"} {
		_, err := NewMemoryStore().RecordReceipt(context.Background(), ReceiptInput{Type: kind, Status: "completed", Surface: "cloud", WorkspaceID: "invented-workspace", IdempotencyKey: "forged-event"})
		if !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("generic writer accepted %s: %v", kind, err)
		}
	}
}
