package fabric

import (
	"context"
	"testing"
	"time"
)

func TestPostgresFabricRiverDispatcherTransactionalEnqueueAndDedup(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	poolDone := make(chan string, 4)
	opDone := make(chan string, 4)

	dispatcher, err := NewRiverDispatcher(ctx, store, RiverReconcileHandlers{
		ReconcilePoolHead: func(_ context.Context, poolKey, operationID string) error {
			poolDone <- poolKey + ":" + operationID
			return nil
		},
		ReconcileOperation: func(_ context.Context, operationID string) error {
			opDone <- operationID
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewRiverDispatcher: %v", err)
	}
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("dispatcher.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = dispatcher.Stop(stopCtx)
	})

	tx, err := store.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := dispatcher.EnqueuePoolReconcileTx(ctx, tx, "pool-ap-guangzhou-1", "fop-river-1")
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	duplicate, err := dispatcher.EnqueuePoolReconcileTx(ctx, tx, "pool-ap-guangzhou-1", "fop-river-1")
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if !duplicate.UniqueSkippedAsDuplicate || duplicate.Job.ID != first.Job.ID {
		_ = tx.Rollback(ctx)
		t.Fatalf("expected duplicate enqueue to coalesce onto job %d, got skipped=%v id=%d", first.Job.ID, duplicate.UniqueSkippedAsDuplicate, duplicate.Job.ID)
	}
	if _, err := dispatcher.EnqueueOperationReconcileTx(ctx, tx, "fop-river-2"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-poolDone:
		if got != "pool-ap-guangzhou-1:fop-river-1" {
			t.Fatalf("pool reconcile got %q", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for Fabric River pool reconcile job")
	}
	select {
	case got := <-opDone:
		if got != "fop-river-2" {
			t.Fatalf("operation reconcile got %q", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for Fabric River operation reconcile job")
	}
}
