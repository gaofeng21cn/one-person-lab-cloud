package server

import (
	"context"
	"testing"
	"time"
)

func TestRetentionWorkerKeepsTerminalResourcesAndAppliesOwnedRetention(t *testing.T) {
	ctx := context.Background()
	store := NewTestEntStateStore(t, t.TempDir()+"/retention.sqlite").(*postgresEntStateStore)
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	mustStore(t, store.SaveCompute(ctx, map[string]any{"id": "compute-terminal", "accountId": "acct-unit", "status": "destroyed"}))
	mustStore(t, store.SaveAuditEvent(ctx, map[string]any{"id": "audit-old", "action": "test", "createdAt": old.Format(time.RFC3339)}))
	if _, err := store.execSQL(ctx, store.db, `INSERT INTO control_plane_production_e2e_records (id, created_at, updated_at, reason) VALUES (?, ?, ?, ?)`, "e2e-old", old, old, "retention-test"); err != nil {
		t.Fatal(err)
	}

	if err := app.runRetentionOnce(ctx); err != nil {
		t.Fatalf("run retention: %v", err)
	}
	if computes, err := store.ListComputes(ctx, ""); err != nil || len(computes) != 1 || stringValue(computes[0]["id"]) != "compute-terminal" {
		t.Fatalf("retention changed terminal resource truth: computes=%#v err=%v", computes, err)
	}
	var archivedCount int
	if err := store.queryRow(ctx, store.db, `SELECT count(*) FROM control_plane_archived_admin_audit_events`).Scan(&archivedCount); err != nil || archivedCount != 1 {
		t.Fatalf("archived audit count=%d err=%v", archivedCount, err)
	}
	var e2eCount int
	if err := store.queryRow(ctx, store.db, `SELECT count(*) FROM control_plane_production_e2e_records`).Scan(&e2eCount); err != nil || e2eCount != 0 {
		t.Fatalf("production E2E count=%d err=%v", e2eCount, err)
	}
}
