package ownerstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"opl-cloud/services/internal/postgresmigrate"
)

type probeRiverJobArgs struct {
	Key string `json:"key"`
}

func (probeRiverJobArgs) Kind() string { return "opl.ownerstore.probe" }

type probeRiverWorker struct {
	river.WorkerDefaults[probeRiverJobArgs]
	failOnce atomic.Bool
	executed chan string
}

func (w *probeRiverWorker) Work(_ context.Context, job *river.Job[probeRiverJobArgs]) error {
	if w.failOnce.CompareAndSwap(true, false) {
		return errors.New("transient probe failure")
	}
	w.executed <- job.Args.Key
	return nil
}

func TestRiverTransactionalEnqueueRollbackCommitAndRetry(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OPL_POSTGRES_TESTS") == "1" {
			databaseURL = "connect_timeout=10"
		} else {
			t.Skip("PostgreSQL test gate is not configured: set POSTGRES_MIGRATION_TEST_DATABASE_URL or OPL_POSTGRES_TESTS=1 for an isolated fixture")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()

	schema := fmt.Sprintf("opl_river_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE")
	})

	scopedURL := databaseURL
	if parsed, err := url.Parse(databaseURL); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		scopedURL = parsed.String()
	} else {
		scopedURL = databaseURL + " search_path=" + schema
	}

	pool, db, err := postgresmigrate.OpenPool(ctx, scopedURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		pool.Close()
	})

	if err := MigrateRiverSchema(ctx, pool, schema); err != nil {
		t.Fatalf("MigrateRiverSchema first run: %v", err)
	}
	// Idempotent re-run must succeed cleanly.
	if err := MigrateRiverSchema(ctx, pool, schema); err != nil {
		t.Fatalf("MigrateRiverSchema second run: %v", err)
	}

	worker := &probeRiverWorker{executed: make(chan string, 4)}
	worker.failOnce.Store(true)
	workers := river.NewWorkers()
	river.AddWorker(workers, worker)

	client, err := NewRiverClient(pool, RiverClientConfig{
		Schema:  schema,
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = client.Stop(stopCtx)
	})

	// 1. Rolled-back transaction must NOT leave a durable job behind.
	rolledBackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueInTx(ctx, client, rolledBackTx, probeRiverJobArgs{Key: "rolled-back"}, nil); err != nil {
		_ = rolledBackTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := rolledBackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// 2. Committed transaction enqueues atomically; first attempt fails and River retries to completion.
	committedTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueInTx(ctx, client, committedTx, probeRiverJobArgs{Key: "committed-job"}, &river.InsertOpts{
		MaxAttempts: 3,
	}); err != nil {
		_ = committedTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := committedTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case key := <-worker.executed:
		if key != "committed-job" {
			t.Fatalf("executed key = %q, want committed-job", key)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for River transactional job execution and retry")
	}

	select {
	case unexpected := <-worker.executed:
		t.Fatalf("unexpected execution of rolled-back or duplicate job: %q", unexpected)
	case <-time.After(300 * time.Millisecond):
	}
}
