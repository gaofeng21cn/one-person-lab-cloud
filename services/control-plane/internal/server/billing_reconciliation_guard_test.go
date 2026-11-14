package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func billingReconciliationRowForTest(id, status, reason string) map[string]any {
	return map[string]any{
		"id": id, "status": status,
		"guard":  map[string]any{"status": status, "reason": reason, "blockNewWorkspaces": status == "mismatch"},
		"report": map[string]any{"id": id, "status": status},
	}
}

func TestRetainedWorkspaceLaunchFenceDoesNotDependOnReconciliationRead(t *testing.T) {
	fixture := newBillingReconciliationFixture(t)
	before, err := fixture.store.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.store.reconciliationErr = errors.New("reconciliation store unavailable")
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.member, http.MethodPost, "/api/workspace-launches", `{"name":"Alpha","packageId":"basic","autoRenew":false}`, "guard-read-failure")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), retainedWorkspaceLaunchNewPurchaseRetired) {
		t.Fatalf("retained new purchase status=%d body=%s", response.Code, response.Body.String())
	}
	if len(*fixture.calls) != 0 || len(fixture.sub2API.charges) != 0 || len(fixture.store.runtimeOps) != len(before) {
		t.Fatalf("retained new purchase reached downstream mutation: fabric=%#v charges=%#v operations=%#v", *fixture.calls, fixture.sub2API.charges, fixture.store.runtimeOps)
	}
}
