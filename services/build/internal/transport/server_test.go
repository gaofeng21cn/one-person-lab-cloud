package transport

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v226 "opl-cloud/packages/contracts/go/v226"
	"opl-cloud/services/build/internal/store"
)

func codeOf(t *testing.T, err error) codes.Code {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	got := status.Code(err)
	return got
}

// An operation id is unique only inside its owning context, so a request naming
// another owner is refused before this owner's store is touched. The store here
// has no database, so any store access would surface as an internal error rather
// than the addressing error under test.
func TestNilRequestsReturnInvalidArgument(t *testing.T) {
	server := NewServer(store.New(nil))
	ctx := context.Background()
	if _, err := server.Read(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Read(nil) = %v, want InvalidArgument", err)
	}
	if _, err := server.Reconcile(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Reconcile(nil) = %v, want InvalidArgument", err)
	}
	if _, err := server.ReadOwnerCommit(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ReadOwnerCommit(nil) = %v, want InvalidArgument", err)
	}
	if _, err := server.Deliver(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Deliver(nil) = %v, want InvalidArgument", err)
	}
}

func TestReadRefusesAnotherOwnerWithoutTouchingTheStore(t *testing.T) {
	server := NewServer(store.New(nil))
	ctx := context.Background()

	for name, owner := range map[string]v226.OperationOwnerEnum{
		"unspecified":   v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_UNSPECIFIED,
		"another owner": v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := server.Read(ctx, &v226.OwnerOperationRequest{OperationId: "op-1", Owner: owner})
			if got := codeOf(t, err); got != codes.InvalidArgument {
				t.Fatalf("Read with %s owner = %v, want InvalidArgument", name, got)
			}
		})
	}

	// The owner that this endpoint serves passes the addressing check and reaches
	// the store, which reports its own missing-database failure.
	_, err := server.Read(ctx, &v226.OwnerOperationRequest{
		OperationId: "op-1",
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_BUILD,
	})
	if got := status.Code(err); got == codes.InvalidArgument {
		t.Fatalf("the build owner must pass addressing, got %v", got)
	}
}

func TestReconcileRefusesAnotherOwner(t *testing.T) {
	server := NewServer(store.New(nil))
	_, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		OperationId: "op-1",
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC,
	})
	if got := codeOf(t, err); got != codes.InvalidArgument {
		t.Fatalf("Reconcile for another owner = %v, want InvalidArgument", got)
	}
}

func TestDeliverRefusesAnotherInbox(t *testing.T) {
	server := NewServer(store.New(nil))
	envelope := &v226.EventEnvelope{EventId: "e-1", EventType: "package.uploaded.v1", SchemaVersion: 1, Owner: "capability"}

	_, err := server.Deliver(context.Background(), &v226.DeliverEventRequest{
		Event:                 envelope,
		AuthenticatedProducer: "capability",
		ConsumerOwner:         v226.OwnerEnum_OWNER_ENUM_LEDGER,
	})
	if got := codeOf(t, err); got != codes.InvalidArgument {
		t.Fatalf("Deliver into another owner's inbox = %v, want InvalidArgument", got)
	}

	// A delivery into this owner's inbox still requires an authenticated producer
	// that matches the envelope owner.
	_, err = server.Deliver(context.Background(), &v226.DeliverEventRequest{
		Event:                 envelope,
		AuthenticatedProducer: "ledger",
		ConsumerOwner:         v226.OwnerEnum_OWNER_ENUM_BUILD,
	})
	if got := codeOf(t, err); got != codes.PermissionDenied {
		t.Fatalf("producer/envelope owner mismatch = %v, want PermissionDenied", got)
	}
}

// Committed evidence is never fabricated: with no committed aggregate this owner
// refuses rather than returning an empty digest or a zero version.
func TestReadOwnerCommitRefusesWithoutCommittedEvidence(t *testing.T) {
	server := NewServer(store.New(nil))
	ctx := context.Background()

	_, err := server.ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_WORKSPACE,
		OperationId: "op-1",
	})
	if got := codeOf(t, err); got != codes.InvalidArgument {
		t.Fatalf("ReadOwnerCommit for another owner = %v, want InvalidArgument", got)
	}

	_, err = server.ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_BUILD,
		OperationId: "op-1",
	})
	if got := codeOf(t, err); got != codes.Internal {
		// No database is configured here, so the store cannot answer at all; the
		// point is that it never answers with placeholder evidence.
		t.Fatalf("ReadOwnerCommit without a store = %v, want Internal", got)
	}
}
