package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/internal/ownerstore"
)

func TestPostgresRiverWorkersMigrateEnqueueAndShutdown(t *testing.T) {
	t.Setenv("OPL_ARCHIVE_RETENTION_WORKER_ENABLED", "1")
	t.Setenv("OPL_ARCHIVE_RETENTION_INTERVAL_MS", "60000")
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "1")
	t.Setenv("OPL_WORKSPACE_LAUNCH_INTERVAL_MS", "60000")
	t.Setenv("OPL_MONTHLY_BILLING_WORKER_ENABLED", "0")
	t.Setenv("OPL_PROVIDER_RECONCILE_WORKER_ENABLED", "0")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")

	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_river_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, database)
		_, _ = admin.Exec(`DROP DATABASE ` + database)
		_ = admin.Close()
	})
	databaseURL := controlPlaneTestPostgresURL(t, database, "")
	store, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	pgStore, ok := store.(*postgresEntStateStore)
	if !ok || pgStore.pool == nil {
		t.Fatal("expected postgresEntStateStore with active pgxpool.Pool")
	}
	t.Cleanup(func() { _ = pgStore.Close() })

	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	service := &controlplane.Service{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.startRiverWorkers(ctx, service, pgStore.pool); err != nil {
		t.Fatalf("startRiverWorkers: %v", err)
	}
	t.Cleanup(func() {
		if err := app.stopRiverWorkers(context.Background()); err != nil {
			t.Errorf("stopRiverWorkers: %v", err)
		}
	})

	// Transactional enqueue in the same PostgreSQL transaction as control-plane state writes.
	tx, err := pgStore.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inserted, err := ownerstore.EnqueueInTx(ctx, app.riverClient, tx, WorkspaceLaunchSweepJobArgs{}, nil)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("EnqueueInTx(WorkspaceLaunchSweepJobArgs): %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if inserted.Job.ID <= 0 {
		t.Fatalf("expected positive River job ID, got %d", inserted.Job.ID)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var state string
		if err := pgStore.pool.QueryRow(ctx, `SELECT state::text FROM river_job WHERE id = $1`, inserted.Job.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("River job %d did not reach completed state before deadline", inserted.Job.ID)
}
