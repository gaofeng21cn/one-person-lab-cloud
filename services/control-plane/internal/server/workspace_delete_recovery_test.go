package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// restartWorkspaceDeleteServer models a Control Plane restart: a new server and
// session over the same durable store, Fabric double and service. Nothing about
// the deletion lives in process memory, so this is the restart the worker resumes
// from.
func restartWorkspaceDeleteServer(t *testing.T, store controlPlaneTableStore, fabric *workspaceDeleteFabric, service *controlplane.Service) workspaceDeleteFixture {
	t.Helper()
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceDeleteFixture{server: server, store: store, session: tenantOwnerSessionForTest(t, server), fabric: fabric}
}

// A restart must resume the original Delete operation from its classified wait and
// complete it without issuing a second provider mutation.
func TestWorkspaceDeleteRestartResumesClassifiedComputeWait(t *testing.T) {
	absent := false
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.computeReadbackResults = []clients.ComputeAllocation{
		{Status: "present", TKEStatus: "RUNNING", MachinePresent: &absent, DestroyState: clients.StorageDestroyStatePendingRetry},
		{Status: "external_deleted", CVMStatus: contracts.WorkspaceDeleteProviderStatusNotFound, TKEStatus: contracts.WorkspaceDeleteProviderStatusNotFound, MachinePresent: &absent},
	}
	fixture, _, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	first := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-restart-classified")
	if first.Code != http.StatusAccepted {
		t.Fatalf("initial status=%d body=%s", first.Code, first.Body.String())
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.Phase != "storage_absent" || operation.ComputeStatus != "destroying" {
		t.Fatalf("operation before restart=%#v", operation)
	}

	restarted := restartWorkspaceDeleteServer(t, fixture.store, fabric, fixture.server.(*controlPlaneHTTPHandler).service)
	expireWorkspaceDeleteComputeReadback(t, fixture.store)
	if err := restarted.server.(*controlPlaneHTTPHandler).app.runWorkspaceDeletesOnce(context.Background(), restarted.server.(*controlPlaneHTTPHandler).service); err != nil {
		t.Fatal(err)
	}
	if _, found, err := fixture.store.GetWorkspace(context.Background(), "ws-alpha"); found || err != nil {
		t.Fatalf("workspace still present found=%v err=%v", found, err)
	}
	computeMutations := 0
	for _, call := range fabric.recordedCalls() {
		if strings.HasPrefix(call, "compute:") {
			computeMutations++
		}
	}
	deletionReceipts := 0
	for _, receipt := range ledger.receipts {
		if receipt.Type == "workspace.deleted.v1" {
			deletionReceipts++
		}
	}
	if computeMutations != 1 || deletionReceipts != 1 {
		t.Fatalf("after restart compute=%d receipts=%d calls=%#v", computeMutations, deletionReceipts, fabric.recordedCalls())
	}
}

// A deletion that completed while its provider readback was unavailable leaves the
// refund blocked. A restart must resume that refund and dispatch exactly one wallet
// refund, because the refund is a separate operation with its own durable record.
func TestWorkspaceDeleteRestartDispatchesBlockedRefundExactlyOnce(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	// This readback belongs to the refund precondition only: the deletion stages use
	// the destroy surfaces, so the deletion still completes.
	fabric.storageReadErr = errWorkspaceDeleteUnconfirmed
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-restart-refund")
	if response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	handler := fixture.server.(*controlPlaneHTTPHandler)
	refund, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || refund.Status != workspaceDeleteRefundStatusBlocked || len(sub2API.refunds) != 0 {
		t.Fatalf("refund before restart=%#v found=%v err=%v refunds=%d", refund, found, err, len(sub2API.refunds))
	}

	// The provider recovers and the process restarts.
	fabric.mu.Lock()
	fabric.storageReadErr = nil
	fabric.mu.Unlock()
	restarted := restartWorkspaceDeleteServer(t, fixture.store, fabric, handler.service)
	if err := restarted.server.(*controlPlaneHTTPHandler).app.runWorkspaceDeletesOnce(context.Background(), restarted.server.(*controlPlaneHTTPHandler).service); err != nil {
		t.Fatal(err)
	}
	if len(sub2API.refunds) != 1 {
		t.Fatalf("refunds after restart=%#v", sub2API.refunds)
	}
	refund, found, err = restarted.server.(*controlPlaneHTTPHandler).app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || refund.Status != workspaceDeleteRefundStatusSucceeded || refund.RefundReceiptID == "" {
		t.Fatalf("refund after restart=%#v found=%v err=%v", refund, found, err)
	}

	// A further restart and worker pass must not dispatch a second refund.
	again := restartWorkspaceDeleteServer(t, fixture.store, fabric, handler.service)
	if err := again.server.(*controlPlaneHTTPHandler).app.runWorkspaceDeletesOnce(context.Background(), again.server.(*controlPlaneHTTPHandler).service); err != nil {
		t.Fatal(err)
	}
	if len(sub2API.refunds) != 1 {
		t.Fatalf("second restart dispatched again: %#v", sub2API.refunds)
	}
}

// The deletion receipt published by the deletion status is the durable evidence of
// the deletion. Its owner must be able to retrieve exactly that receipt, and it must
// carry the confirmed absences rather than a bare identifier.
func TestWorkspaceDeletionReceiptIsRetrievableByItsOwner(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-receipt-owner")

	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK || dto.ReceiptID == "" {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}

	response := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/billing/receipts/"+dto.ReceiptID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("deletion receipt status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Available bool           `json:"available"`
		Data      map[string]any `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || !envelope.Available {
		t.Fatalf("deletion receipt body=%s", response.Body.String())
	}
	if envelope.Data["receiptId"] != dto.ReceiptID || envelope.Data["type"] != "workspace.deleted.v1" ||
		envelope.Data["status"] != "completed" || envelope.Data["workspaceId"] != "ws-alpha" ||
		envelope.Data["operationId"] != workspaceDeleteOperationID("ws-alpha") || envelope.Data["resourceId"] != "ws-alpha" {
		t.Fatalf("deletion receipt data=%#v", envelope.Data)
	}
	statuses, ok := envelope.Data["resourceStatus"].(map[string]any)
	if !ok || len(statuses) != 6 {
		t.Fatalf("deletion receipt resource status=%#v", envelope.Data["resourceStatus"])
	}
	for _, field := range []string{"runtimeStatus", "gatewaySecretStatus", "attachmentStatus", "storageStatus", "computeStatus", "workspaceStatus"} {
		if statuses[field] != "absent" {
			t.Fatalf("deletion receipt %s=%#v", field, statuses[field])
		}
	}
	// The refund receipt is a separate statement and must not be conflated with the
	// deletion receipt.
	if dto.RefundReceiptID == "" || dto.RefundReceiptID == dto.ReceiptID {
		t.Fatalf("refund receipt dto=%#v", dto)
	}
}

// A deletion receipt belongs to its account: another owner cannot retrieve it.
func TestWorkspaceDeletionReceiptIsAccountScoped(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-receipt-scope")
	handler := fixture.server.(*controlPlaneHTTPHandler)
	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	_ = json.Unmarshal(status.Body.Bytes(), &dto)

	if _, exists, err := fixture.store.GetAccount(context.Background(), "acct-beta"); err != nil {
		t.Fatal(err)
	} else if !exists {
		seedTenantMember(t, fixture.store, "acct-beta", "org-beta", "usr-beta-receipt-scope", "beta-receipt-scope@example.com")
	}
	other := loginForTest(t, fixture.server, "beta-receipt-scope@example.com", "CorrectHorseBatteryStaple!")
	response := requestWithSession(t, fixture.server, other, http.MethodGet, "/api/billing/receipts/"+dto.ReceiptID, "")
	if response.Code == http.StatusOK {
		t.Fatalf("foreign account read the deletion receipt: %s", response.Body.String())
	}
	_ = handler
}
