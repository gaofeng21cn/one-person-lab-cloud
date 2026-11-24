package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func newResourceOnlyWorkspaceLifecycleFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger, *workspaceDeleteEvents) {
	t.Helper()
	events := &workspaceDeleteEvents{}
	// A resource-only purchase created no Runtime objects, so Fabric's labelled
	// readback reports them absent. The deletion now reads that back before it
	// records the runtime stage evidence.
	fabric := &workspaceDeleteFabric{events: events, residualObserveState: clients.WorkspaceOwnerObservationAbsent}
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
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	if operation.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly {
		t.Fatal("lifecycle fixture launch must be resource-only")
	}
	fixtureClock := time.Now().UTC()
	periodStart, paidThrough := workspaceDeleteFixtureBillingPeriod(fixtureClock)
	readyFacts := map[string]any{
		"sub2apiRedeemCode":          "opl:workspace-purchase-alpha",
		"chargeAttempted":            true,
		"chargeConfirmation":         map[string]any{"code": "opl:workspace-purchase-alpha", "userId": int64(41), "chargeUsdMicros": int64(52_580_000), "status": "used"},
		"postChargeBalanceUsdMicros": int64(947_420_000), "postChargeBalanceKnown": true, "billingPeriodState": "frozen",
		"periodStart": periodStart.Format(time.RFC3339Nano), "paidThrough": paidThrough.Format(time.RFC3339Nano), "billingAnchorDay": periodStart.Day(),
		"computeAllocationId": "compute-alpha", "computeBindingRef": "workspace-launch-alpha:ensure_compute_allocation",
		"storageId": "storage-alpha", "storageBindingRef": "workspace-launch-alpha:storage",
		"attachmentId": "attachment-alpha", "attachmentBindingRef": "workspace-launch-alpha:attachment",
		"activationOperationId": "workspace-launch-alpha:activation", "workspaceActivatedAt": periodStart.Format(time.RFC3339Nano),
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
	if workspace["applicationBinding"] != provisioning.ApplicationBindingEmpty {
		t.Fatalf("resource-only activation applicationBinding = %v, want empty", workspace["applicationBinding"])
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

func TestWorkspaceRenewalRuntimeRecoveryEligibleSkipsResourceOnlyWorkspace(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	app := handler.app
	renewal, err := newWorkspaceRenewalOperation(fixture.workspace, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	row := workspaceRenewalOperationRow(renewal)
	mustStore(t, fixture.store.SaveRuntimeOperation(context.Background(), row))
	renewal, err = decodeWorkspaceRenewalOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.workspaceRenewalRuntimeRecoveryEligible(context.Background(), nil, renewal); err != nil {
		t.Fatalf("resource-only runtime recovery eligibility error = %v, want eligible", err)
	}
}

func TestWorkspaceRenewalRuntimePowerInputFullModeStillRequiresRuntimeIdentity(t *testing.T) {
	store := newMemoryTableStore()
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "ws-alpha"
	operation, err := newWorkspaceLaunchFixtureOperation(command)
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
	if _, err := app.workspaceRenewalRuntimePowerInput(context.Background(), renewal, "suspended"); err == nil {
		t.Fatal("full-mode runtime power without runtime identity must fail instead of skipping")
	}
}
