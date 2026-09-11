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

func newResourceOnlyWorkspaceLifecycleFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger, *workspaceDeleteEvents) {
	t.Helper()
	events := &workspaceDeleteEvents{}
	fabric := &workspaceDeleteFabric{events: events}
	store := workspaceDeleteEventStore{controlPlaneTableStore: newMemoryTableStore(), events: events}
	sub2API := &workspaceDeleteSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000_000, charges: map[string]int64{}},
		userID:            41, keyID: 19, keyExists: true, history: map[string]clients.Sub2APIBalanceHistoryEntry{},
	}
	ledger := &workspaceDeleteLedger{}
	service := controlplane.NewService(ledger, fabric, sub2API)
	fixture := newWorkspaceDeleteFixtureWithService(t, store, fabric, service)
	sub2API.events, ledger.events = events, events
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "workspace-launch-alpha", "acct-alpha", stringValue(fixture.workspace["ownerUserId"]), 41
	command.WorkspaceID, command.Name = "ws-alpha", "Alpha"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	if operation.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly {
		t.Fatal("lifecycle fixture launch must be resource-only")
	}
	readyFacts := map[string]any{
		"sub2apiRedeemCode":          "opl:workspace-purchase-alpha",
		"chargeAttempted":            true,
		"chargeConfirmation":         map[string]any{"code": "opl:workspace-purchase-alpha", "userId": int64(41), "chargeUsdMicros": int64(52_580_000), "status": "used"},
		"postChargeBalanceUsdMicros": int64(947_420_000), "postChargeBalanceKnown": true, "billingPeriodState": "frozen",
		"periodStart": "2026-08-15T00:00:00Z", "paidThrough": "2026-09-15T00:00:00Z", "billingAnchorDay": 15,
		"computeAllocationId": "compute-alpha", "computeBindingRef": "workspace-launch-alpha:ensure_compute_allocation",
		"storageId": "storage-alpha", "storageBindingRef": "workspace-launch-alpha:storage",
		"attachmentId": "attachment-alpha", "attachmentBindingRef": "workspace-launch-alpha:attachment",
		"activationOperationId": "workspace-launch-alpha:activation", "workspaceActivatedAt": "2026-08-15T00:01:00Z",
		"receiptId": "receipt-purchase-alpha", "receiptOperationId": "workspace-launch-alpha:purchase-receipt",
	}
	for key, value := range readyFacts {
		operation.raw[key], _ = json.Marshal(value)
	}
	operation.Stage, operation.Status = "succeeded", "succeeded"
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), launchRow); err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(workspace["runtimeId"]) != "" {
		t.Fatalf("resource-only activation runtimeId = %q, want empty", workspace["runtimeId"])
	}
	if ready, ok := nested(workspace, "runtime", "ready").(bool); !ok || ready {
		t.Fatal("resource-only activation must not mark the runtime ready")
	}
	if _, exists := workspace["workspaceApiKeyId"]; exists {
		t.Fatal("resource-only activation must not carry a workspace API key")
	}
	workspace["purchaseReceiptId"] = "receipt-purchase-alpha"
	workspace["sub2apiUserId"], workspace["sub2apiRedeemCode"] = int64(41), "opl:workspace-purchase-alpha"
	purchaseInput, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		t.Fatal(err)
	}
	ledger.purchase = clients.Receipt{ReceiptInput: purchaseInput, ReceiptID: "receipt-purchase-alpha"}
	if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveStorage(context.Background(), map[string]any{
		"id": "storage-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "available",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveAttachment(context.Background(), map[string]any{
		"id": "attachment-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "volumeId": "storage-alpha", "status": "attached",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.workspace = workspace
	return fixture, sub2API, ledger, events
}

func TestWorkspaceDeleteCompletesResourceOnlyWorkspaceWithoutApplicationFacts(t *testing.T) {
	fixture, sub2API, ledger, events := newResourceOnlyWorkspaceLifecycleFixture(t)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-resource-only")
	if response.Code != http.StatusOK {
		row, _, _ := fixture.store.GetRuntimeOperation(context.Background(), workspaceDeleteOperationID("ws-alpha"))
		t.Fatalf("delete status=%d body=%s operation=%#v", response.Code, response.Body.String(), row)
	}
	var terminal map[string]any
	if json.Unmarshal(response.Body.Bytes(), &terminal) != nil || terminal["status"] != "deleted" || terminal["accountId"] != "acct-alpha" ||
		terminal["workspaceId"] != "ws-alpha" || terminal["runtimeId"] != "" ||
		int64(numberField(terminal, "workspaceApiKeyId", 0)) != 0 ||
		terminal["runtimeStatus"] != "absent" || terminal["secretStatus"] != "absent" || terminal["keyStatus"] != nil {
		t.Fatalf("resource-only delete terminal response=%#v", terminal)
	}
	if sub2API.keyDeletes != 0 || len(sub2API.refunds) != 0 {
		t.Fatalf("Sub2API completion keyDeletes=%d refunds=%#v", sub2API.keyDeletes, sub2API.refunds)
	}
	if len(ledger.receipts) != 1 {
		t.Fatalf("Ledger writes receipts=%#v", ledger.receipts)
	}
	receipt := ledger.receipts[0]
	if receipt.Type != "workspace.deleted.v1" || receipt.Status != "completed" || receipt.AccountID != "acct-alpha" || receipt.WorkspaceID != "ws-alpha" ||
		stringValue(receipt.Execution["runtimeId"]) != "" || stringValue(receipt.Execution["computeAllocationId"]) != "compute-alpha" ||
		stringValue(receipt.Execution["storageId"]) != "storage-alpha" || stringValue(receipt.Execution["attachmentId"]) != "attachment-alpha" ||
		int64(numberField(receipt.Execution, "workspaceApiKeyId", 0)) != 0 {
		t.Fatalf("resource-only deletion receipt=%#v", receipt)
	}
	computes, computeErr := fixture.store.ListComputes(context.Background(), "acct-alpha")
	storages, storageErr := fixture.store.ListStorages(context.Background(), "acct-alpha")
	attachments, attachmentErr := fixture.store.ListAttachments(context.Background(), "acct-alpha")
	if computeErr != nil || storageErr != nil || attachmentErr != nil || len(computes) != 0 || len(storages) != 0 || len(attachments) != 0 {
		t.Fatalf("Delete left resource projections computes=%#v storages=%#v attachments=%#v", computes, storages, attachments)
	}
	wantEvents := []string{
		"ledger:purchase-get",
		"fabric:attachment", "fabric:storage", "fabric:compute", "fabric:compute-read",
		"control-plane:workspace-absent", "ledger:deletion-receipt",
	}
	if got := events.snapshot(); strings.Join(got, "\n") != strings.Join(wantEvents, "\n") {
		t.Fatalf("resource-only deletion events=%#v want=%#v", got, wantEvents)
	}
}

func TestWorkspaceRenewalRuntimePowerSkipsResourceOnlyWorkspace(t *testing.T) {
	store := newMemoryTableStore()
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "ws-alpha"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage, operation.Status = "succeeded", "succeeded"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	app := &controlPlaneServer{tables: store}
	renewal := workspaceRenewalOperation{WorkspaceID: "ws-alpha", AccountID: "acct-alpha"}
	if err := app.convergeWorkspaceRenewalRuntimePower(context.Background(), nil, &renewal, "suspended"); err != nil {
		t.Fatalf("resource-only runtime suspend error = %v, want skipped", err)
	}
	if err := app.convergeWorkspaceRenewalRuntimePower(context.Background(), nil, &renewal, "running"); err != nil {
		t.Fatalf("resource-only runtime resume error = %v, want skipped", err)
	}
	if renewal.ExpiryRuntimePower != nil || renewal.ResumeRuntimePower != nil {
		t.Fatal("runtime power facts must stay unset for a resource-only workspace")
	}
	if err := app.workspaceRenewalRuntimeRecoveryEligible(context.Background(), nil, renewal); err != nil {
		t.Fatalf("resource-only runtime recovery eligibility error = %v, want eligible", err)
	}
}

func TestWorkspaceRenewalRuntimePowerFullModeStillRequiresRuntimeIdentity(t *testing.T) {
	store := newMemoryTableStore()
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "ws-alpha"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage, operation.Status = "succeeded", "succeeded"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	app := &controlPlaneServer{tables: store}
	renewal := workspaceRenewalOperation{WorkspaceID: "ws-alpha", AccountID: "acct-alpha"}
	if err := app.convergeWorkspaceRenewalRuntimePower(context.Background(), nil, &renewal, "suspended"); err == nil {
		t.Fatal("full-mode runtime power without runtime identity must fail instead of skipping")
	}
}
