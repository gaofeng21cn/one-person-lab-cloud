package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newPostgresBillingReconciliationStore(t *testing.T, suffix string) (*postgresEntStateStore, string) {
	t.Helper()
	admin := openControlPlaneTestPostgres(t)
	schema := fmt.Sprintf("control_plane_billing_guard_%s_%d", suffix, time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = admin.Close()
	})
	databaseURL := controlPlaneTestPostgresURL(t, "postgres", schema)
	stateStore, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	store := stateStore.(*postgresEntStateStore)
	t.Cleanup(func() { _ = store.client.Close() })
	return store, databaseURL
}

func seedPostgresBillingReconciliationAccount(t *testing.T, store controlPlaneTableStore, suffix string, userID int64) string {
	t.Helper()
	accountID, ownerID := "acct-billing-guard-"+suffix, "usr-billing-guard-"+suffix
	account, owner := provisionedAccountRowsFor(accountID, ownerID, suffix+"@example.com", userID)
	if err := store.CreateProvisionedAccount(context.Background(), account, owner); err != nil {
		t.Fatal(err)
	}
	return accountID
}

func TestPostgresBillingReconciliationReplayAndCleanSurviveRestart(t *testing.T) {
	ctx := context.Background()
	store, databaseURL := newPostgresBillingReconciliationStore(t, "restart")
	seedPostgresBillingReconciliationAccount(t, store, "restart", 901)
	clean := billingReconciliationRowForTest("reconcile-pg-clean", "ok", "operator_reconciliation")
	mismatch := billingReconciliationRowForTest("reconcile-pg-mismatch", "mismatch", "operator_reconciliation")
	applyBillingReconciliationForTest(t, store, clean, "")
	applyBillingReconciliationForTest(t, store, mismatch, "reconcile-pg-clean")
	reportConflict := billingReconciliationRowForTest("reconcile-pg-clean", "ok", "operator_reconciliation")
	reportConflict["report"].(map[string]any)["changed"] = true
	if err := store.ApplyBillingReconciliation(ctx, billingReconciliationMutationForTest(reportConflict, "reconcile-pg-mismatch")); !errors.Is(err, errIdempotencyConflict) {
		t.Fatalf("same ID report conflict error=%v", err)
	}
	if err := store.ApplyBillingReconciliation(ctx, billingReconciliationMutationForTest(clean, "reconcile-pg-mismatch")); err != nil {
		t.Fatalf("old clean replay error=%v", err)
	}
	if err := store.client.Close(); err != nil {
		t.Fatal(err)
	}

	restartedState, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	restarted := restartedState.(*postgresEntStateStore)
	current, found, err := restarted.BillingReconciliation(ctx)
	if err != nil || !found || stringValue(current["id"]) != "reconcile-pg-mismatch" {
		t.Fatalf("blocked restart readback=%#v found=%v err=%v", current, found, err)
	}
	audits, err := restarted.ListAuditEvents(ctx, "")
	if err != nil || len(audits) != 2 {
		t.Fatalf("blocked restart audits=%#v err=%v", audits, err)
	}
	resume := billingReconciliationRowForTest("reconcile-pg-resume", "ok", "operator_reconciliation")
	applyBillingReconciliationForTest(t, restarted, resume, "reconcile-pg-mismatch")
	if err := restarted.client.Close(); err != nil {
		t.Fatal(err)
	}

	finalState, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	finalStore := finalState.(*postgresEntStateStore)
	t.Cleanup(func() { _ = finalStore.client.Close() })
	current, found, err = finalStore.BillingReconciliation(ctx)
	blocked, guardErr := billingReconciliationBlockState(current)
	if err != nil || guardErr != nil || !found || blocked || stringValue(current["id"]) != "reconcile-pg-resume" {
		t.Fatalf("clean restart readback=%#v blocked=%v found=%v err=%v guardErr=%v", current, blocked, found, err, guardErr)
	}
	audits, err = finalStore.ListAuditEvents(ctx, "")
	if err != nil || len(audits) != 3 {
		t.Fatalf("clean restart audits=%#v err=%v", audits, err)
	}
}

func TestPostgresBillingReconciliationAuditConflictRollsBackGuard(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresBillingReconciliationStore(t, "audit")
	seedPostgresBillingReconciliationAccount(t, store, "audit", 902)
	clean := billingReconciliationRowForTest("reconcile-pg-audit-clean", "ok", "operator_reconciliation")
	applyBillingReconciliationForTest(t, store, clean, "")
	mismatch := billingReconciliationRowForTest("reconcile-pg-audit-mismatch", "mismatch", "operator_reconciliation")
	mutation := billingReconciliationMutationForTest(mismatch, "reconcile-pg-audit-clean")
	if err := store.SaveAuditEvent(ctx, map[string]any{
		"id": mutation.AuditEvent["id"], "actorUserId": "usr-other", "action": "other.action", "resourceKind": "other", "resourceId": "other", "result": "succeeded",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBillingReconciliation(ctx, mutation); !errors.Is(err, errIdempotencyConflict) {
		t.Fatalf("conflicting audit error=%v", err)
	}
	current, found, err := store.BillingReconciliation(ctx)
	if err != nil || !found || stringValue(current["id"]) != "reconcile-pg-audit-clean" {
		t.Fatalf("guard changed despite audit conflict: current=%#v found=%v err=%v", current, found, err)
	}
}
