package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

func TestD4DeleteContinuesWithoutCustomerCredentialAndOnlyCompletesAfterReceipt(t *testing.T) {
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	sub2API.keyStatus = "disabled"
	ledger.failures = 1
	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	if status.Code != http.StatusOK || strings.TrimSpace(status.Body.String()) != "null" || status.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unclaimed status=%d body=%s", status.Code, status.Body.String())
	}
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "d4-delete-once")
	if response.Code != http.StatusBadGateway || sub2API.keyDeletes != 0 || len(sub2API.refunds) != 0 {
		t.Fatalf("delete status=%d body=%s keyDeletes=%d", response.Code, response.Body.String(), sub2API.keyDeletes)
	}
	status = requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var pending workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &pending) != nil || status.Code != http.StatusOK || pending.Status != "pending" || pending.Phase != "workspace_absent" || pending.ReceiptID != "" {
		t.Fatalf("receipt pending status=%d body=%s", status.Code, status.Body.String())
	}
	for _, cookie := range fixture.session.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			handler.app.sessionCredentials.Delete(sessionLookupKey(cookie.Value))
		}
	}
	calls := len(fixture.fabric.recordedCalls())
	if err := handler.app.runWorkspaceDeletesOnce(context.Background(), handler.service); err != nil {
		t.Fatal(err)
	}
	if len(fixture.fabric.recordedCalls()) != calls || sub2API.keyDeletes != 0 || len(sub2API.refunds) != 0 || len(ledger.receipts) != 2 || ledger.keys[0] != ledger.keys[1] {
		t.Fatal("receipt recovery repeated physical deletion or changed owner key")
	}
	fixture.session = tenantOwnerSessionForTest(t, fixture.server)
	status = requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var done workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &done) != nil || status.Code != http.StatusOK || done.Status != "deleted" || done.ReceiptID == "" || status.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("done status=%d body=%s", status.Code, status.Body.String())
	}
	if _, found, err := fixture.store.GetWorkspace(context.Background(), "ws-alpha"); found || err != nil {
		t.Fatalf("Workspace remains found=%v err=%v", found, err)
	}
}

func TestD4DeleteConvergesIndependentlyRemainingRuntimeObjects(t *testing.T) {
	for _, tc := range []struct{ name, runtime, secret string }{
		{"standalone Secret", clients.WorkspaceOwnerObservationAbsent, clients.WorkspaceOwnerObservationPending},
		{"Runtime after Secret removal", clients.WorkspaceOwnerObservationPending, clients.WorkspaceOwnerObservationAbsent},
		{"stopped Runtime", clients.WorkspaceOwnerObservationPending, clients.WorkspaceOwnerObservationPending},
		{"only Service or NetworkPolicy", clients.WorkspaceOwnerObservationAbsent, clients.WorkspaceOwnerObservationAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fabric := &workspaceDeleteFabric{observeState: tc.runtime, secretObserveState: tc.secret, clearObservationsOnDestroy: true}
			fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			result := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "d4-partial-runtime")
			if result.Code != http.StatusOK || len(ledger.receipts) != 1 || sub2API.keyDeletes != 0 || len(sub2API.refunds) != 0 {
				t.Fatalf("status=%d body=%s calls=%v", result.Code, result.Body.String(), fabric.recordedCalls())
			}
			mutations := 0
			for _, call := range fabric.recordedCalls() {
				if strings.HasPrefix(call, "runtime:") {
					mutations++
				}
			}
			if mutations != 1 {
				t.Fatalf("Runtime owner mutations=%d", mutations)
			}
		})
	}
}

func TestD4DeletionStatusIsOwnerBoundBeforeAndAfterWorkspaceRemoval(t *testing.T) {
	fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	_, err := handler.app.createUser(context.Background(), handler.service, map[string]any{"email": "d4-other@example.test", "accountId": "acct-beta", "password": "CorrectHorseBatteryStaple!"})
	if err != nil {
		t.Fatal(err)
	}
	other := loginForTest(t, fixture.server, "d4-other@example.test", "CorrectHorseBatteryStaple!")
	for _, deleted := range []bool{false, true} {
		if deleted {
			result := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "d4-status-delete")
			if result.Code != http.StatusOK {
				t.Fatal(result.Body.String())
			}
		}
		result := requestWithSession(t, fixture.server, other, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
		if result.Code != http.StatusForbidden || strings.Contains(result.Body.String(), "receipt-delete") {
			t.Fatalf("cross-owner status=%d body=%s", result.Code, result.Body.String())
		}
	}
	missing := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-missing/deletion", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
}
