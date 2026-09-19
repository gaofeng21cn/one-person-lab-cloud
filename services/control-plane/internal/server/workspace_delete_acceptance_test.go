package server

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// The deletion receipt must be sensitive to the resource identity and the readback
// record behind each stage: two observations that differ only in what they read must
// never produce the same receipt digest.
func TestReceiptEvidenceDigestBindsResourceAndReadbackIdentity(t *testing.T) {
	first := contracts.WorkspaceDeleteStageEvidence{
		Stage: contracts.WorkspaceDeleteStageStorageAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent,
		EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "storage-a",
		ProviderResourceID: "disk-a", ObservedAt: "2026-09-18T05:00:00.000000Z", ReadbackID: "read-a",
	}
	second := first
	second.ResourceID, second.ProviderResourceID, second.ReadbackID = "storage-b", "disk-b", "read-b"
	if reflect.DeepEqual(contracts.WorkspaceDeleteStageEvidenceDigests([]contracts.WorkspaceDeleteStageEvidence{first}), contracts.WorkspaceDeleteStageEvidenceDigests([]contracts.WorkspaceDeleteStageEvidence{second})) {
		t.Fatal("receipt digest is unchanged after changing the resource and readback identity")
	}
	// The reference is opaque: it must not carry the identifiers themselves.
	digest := contracts.WorkspaceDeleteStageEvidenceDigests([]contracts.WorkspaceDeleteStageEvidence{first})[0]
	for _, private := range []string{"disk-a", "read-a", "storage-a"} {
		if digest.EvidenceRef == private {
			t.Fatalf("evidence reference leaked %q", private)
		}
	}
}

// A provider observation that does not name the readback it came from is not
// traceable, so it is refused outright.
func TestProviderStageEvidenceRequiresItsReadbackRecord(t *testing.T) {
	evidence := contracts.WorkspaceDeleteStageEvidence{
		Stage: contracts.WorkspaceDeleteStageStorageAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent,
		EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "storage-a",
		ProviderResourceID: "disk-a", ObservedAt: "2026-09-18T05:00:00.000000Z",
	}
	if contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
		t.Fatal("provider evidence with no readback record is accepted")
	}
	evidence.ReadbackID = "read-a"
	if !contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
		t.Fatal("provider evidence with its readback record is rejected")
	}
}

// A resource-only purchase owns no Runtime controller, so requiring a Runtime
// identity would refuse a refund for a Workspace that correctly has none.
func TestWorkspaceDeleteRefundWideEnoughForEveryPurchaseShape(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "accept-purchase-shape")
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if !workspaceDeleteRefundIdentityComplete(operation) {
		t.Fatal("a normal purchase was rejected as identity-incomplete")
	}
	operation.ProvisioningMode, operation.RuntimeID = string(contracts.WorkspaceProvisioningResourceOnly), ""
	if !workspaceDeleteRefundIdentityComplete(operation) {
		t.Fatal("a resource-only purchase was rejected solely because it correctly has no Runtime identity")
	}
	// A purchase that owns a Runtime must still bind its identity.
	operation.ProvisioningMode = ""
	if workspaceDeleteRefundIdentityComplete(operation) {
		t.Fatal("a Runtime-owning purchase without a Runtime identity was accepted")
	}
}

// A stage that has not finished is still persisted, so the wait is explainable from
// state rather than only from a log.
func TestStorageWaitIsPersistedWithItsObservation(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageResults = []workspaceDeleteStorageResult{{Status: "ready", DestroyState: contracts.WorkspaceDeleteOutcomePendingRetry}}
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "accept-storage-wait")
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected a classified wait, status=%d body=%s", response.Code, response.Body.String())
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	evidence, present := contracts.WorkspaceDeleteStageEvidenceLatest(operation.StageEvidence, contracts.WorkspaceDeleteStageStorageAbsent)
	if !present || evidence.Result != contracts.WorkspaceDeleteEvidenceWaiting || evidence.Confirmed() {
		t.Fatalf("waiting storage observation not persisted: phase=%s reason=%q evidence=%#v", operation.Phase, operation.LastErrorCode, evidence)
	}
	if evidence.ReasonCode != contracts.WorkspaceDeleteOutcomePendingRetry || evidence.ReadbackID == "" {
		t.Fatalf("waiting observation lacks its cause or readback: %#v", evidence)
	}
}

// Every observation of a stage is one more read, so the persisted count matches what
// the owning operation actually did. A query never stands in for a destroy.
func TestStorageRetryAttemptsAreCountedSeparately(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageResults = []workspaceDeleteStorageResult{{Status: "ready", DestroyState: contracts.WorkspaceDeleteOutcomePendingRetry}, {Status: "external_deleted"}}
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "accept-storage-count")
	handler := fixture.server.(*controlPlaneHTTPHandler)
	if err := handler.app.runWorkspaceDeletesOnce(context.Background(), handler.service); err != nil {
		t.Fatal(err)
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	evidence, present := contracts.WorkspaceDeleteStageEvidenceLatest(operation.StageEvidence, contracts.WorkspaceDeleteStageStorageAbsent)
	dispatches := 0
	for _, call := range fabric.recordedCalls() {
		if strings.HasPrefix(call, "storage:") {
			dispatches++
		}
	}
	if !present || !evidence.Confirmed() || dispatches == 0 || evidence.ReadAttempts < dispatches {
		t.Fatalf("%d storage owner dispatches but persisted reads=%d mutations=%d (%#v)", dispatches, evidence.ReadAttempts, evidence.MutationAttempts, evidence)
	}
	if evidence.MutationAttempts != dispatches {
		t.Fatalf("persisted mutations=%d want=%d", evidence.MutationAttempts, dispatches)
	}
}

// A live readback that omits the exact CBS, Machine or CVM identity is not an
// identity-bound readback, so it must never authorise a refund.
func TestRefundRefusesReadbackThatOmitsProviderIdentities(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageReadErr = errWorkspaceDeleteUnconfirmed
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "accept-missing-identities")
	fabric.storageReadErr = nil
	fabric.storageProviderResourceID = ""
	fabric.computeReadMachineName = ""
	fabric.computeReadInstanceID = ""
	operation := mustWorkspaceDeleteOperation(t, fixture)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	if err := handler.app.runWorkspaceDeleteRefund(context.Background(), handler.service, operation); err != nil {
		t.Fatal(err)
	}
	if len(sub2API.refunds) != 0 {
		t.Fatalf("wallet refund dispatched although the readback omitted the exact CBS/Machine/CVM identities: %d", len(sub2API.refunds))
	}
	record, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), operation.WorkspaceID)
	if err != nil || !found || record.Status == workspaceDeleteRefundStatusSucceeded {
		t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
	}
}

// A renewal that paid for the period in use is the refund basis, so the refund is
// computed from that charge and that period, not from the original purchase.
func TestRefundUsesTheChargeThatPaidForThePeriodInUse(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageReadErr = errWorkspaceDeleteUnconfirmed
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "accept-renewal-charge")
	operation := mustWorkspaceDeleteOperation(t, fixture)

	now := time.Now().UTC()
	// The Workspace was first fulfilled 31 days ago and renewed for the current
	// month one day ago, so the first purchase is fully spent and the renewal is not.
	operation.LaunchFulfilledAt = now.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)
	renewal := workspaceRenewalOperation{
		ID: "renewal-current", Status: "active", Phase: "complete",
		CreatedAt: now.Add(-24 * time.Hour).Format(time.RFC3339Nano), AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID,
		TotalUSDMicros: 52_580_000, PeriodStart: operation.LaunchFulfilledAt,
		PaidThrough:    now.Add(-24 * time.Hour).Format(time.RFC3339Nano),
		RenewedThrough: now.Add(29 * 24 * time.Hour).Format(time.RFC3339Nano),
		RedeemCode:     "renewal-charge", ChargeConfirmation: map[string]any{"code": "renewal-charge", "userId": operation.Sub2APIUserID, "chargeUsdMicros": int64(52_580_000), "status": "used"},
	}
	// The wallet owner refunds a charge it can read back from the gateway, so the
	// renewal's confirmed charge is part of the fixture.
	usedAt, usedBy := now.Add(-24*time.Hour), operation.Sub2APIUserID
	sub2API.history["renewal-charge"] = clients.Sub2APIBalanceHistoryEntry{
		Code: "renewal-charge", Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy, UsedAt: &usedAt,
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(renewal)); err != nil {
		t.Fatal(err)
	}
	fabric.storageReadErr = nil
	handler := fixture.server.(*controlPlaneHTTPHandler)
	readback, err := handler.app.observeWorkspaceDeleteRefundReadback(context.Background(), handler.service, operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.app.dispatchWorkspaceDeleteRefund(context.Background(), handler.service, operation, readback, nil); err != nil {
		t.Fatal(err)
	}
	if len(sub2API.refunds) == 0 {
		record, _, _ := handler.app.workspaceDeleteRefundRecord(context.Background(), operation.WorkspaceID)
		t.Fatalf("a valid in-force renewal was ignored: status=%s order=%s refund=%d", record.Status, record.RefundOrderOperationID, record.RefundUSDMicros)
	}
	record, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), operation.WorkspaceID)
	if err != nil || !found || record.Status != workspaceDeleteRefundStatusSucceeded || record.RefundOrderOperationID != "renewal-current" ||
		record.OriginalChargeCode != "renewal-charge" || record.OriginalChargeUSDMicros != 52_580_000 || record.RefundUSDMicros <= 0 {
		t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
	}
	// The period start is the renewed period, not the original fulfilment.
	periodStart, parseErr := time.Parse(time.RFC3339Nano, record.ResourceFulfilledAt)
	if parseErr != nil || periodStart.Before(now.Add(-25*time.Hour)) {
		t.Fatalf("refund used period start %s instead of the renewed period", record.ResourceFulfilledAt)
	}
	// The refund never exceeds what that order charged.
	if record.RefundUSDMicros > record.OriginalChargeUSDMicros {
		t.Fatalf("refund %d exceeds its order charge %d", record.RefundUSDMicros, record.OriginalChargeUSDMicros)
	}
}

// A refund recorded before the paying order was named still continues: it belongs to
// the original purchase, and the next attempt queries that same operation instead of
// creating a second refund.
func TestRefundContinuesARecordedReservationForTheSameOrder(t *testing.T) {
	operation := workspaceDeleteOperation{OperationID: "workspace-delete-ws", LaunchOperationID: "workspace-launch-ws", WorkspaceID: "ws"}
	named := workspaceDeleteRefundOperation{RefundOrderOperationID: "workspace-launch-ws"}
	if order := refundOrderOf(named, operation); order != "workspace-launch-ws" {
		t.Fatalf("named order=%q", order)
	}
	legacy := workspaceDeleteRefundOperation{}
	if order := refundOrderOf(legacy, operation); order != "workspace-launch-ws" {
		t.Fatalf("legacy order=%q, want the original purchase", order)
	}
}

// A refund may only be reserved against an order the wallet owner can resolve. A
// reservation naming an unknown order is refused there, so the platform never pays
// against a charge it cannot read back.
func TestRefundReservationRequiresAResolvableOrder(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	// Hold the refund at the readback gate so the deletion completes without paying,
	// which isolates the reservation rule under test.
	fabric.storageReadErr = errWorkspaceDeleteUnconfirmed
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "accept-unresolvable-order")
	if len(sub2API.refunds) != 0 {
		t.Fatalf("fixture paid a refund before the assertion: %#v", sub2API.refunds)
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	fabric.mu.Lock()
	fabric.storageReadErr = nil
	fabric.mu.Unlock()
	base, err := handler.app.workspaceDeleteRefundBase(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	// The resolved basis is always an order this Workspace actually paid through.
	if base.OriginalOperationID != operation.LaunchOperationID || base.Charge.UserID != operation.Sub2APIUserID || base.Charge.AmountUSDMicros <= 0 {
		t.Fatalf("basis=%#v", base)
	}
	// Reserving the same operation id against a different order is refused by the
	// wallet owner, which is the single admission point for refund reservations.
	walletOperationID := workspaceDeleteRefundWalletOperationID(operation.OperationID)
	mismatched := base
	mismatched.OriginalOperationID = "an-unrelated-order"
	if err := handler.app.ensureWorkspaceDeleteRefundWalletOperation(context.Background(), walletOperationID,
		stableID("workspace-delete-refund-v2", operation.OperationID, mismatched.OriginalOperationID), operation, mismatched, 1_000_000); err == nil {
		t.Fatal("a reservation against an unrelated order was accepted")
	}
	if len(sub2API.refunds) != 0 {
		t.Fatalf("a refund was dispatched for an unresolvable reservation: %#v", sub2API.refunds)
	}
}

// A resource-only purchase — the Workspace exists with no application deployed —
// is refunded on deletion too. Its refund binds the resources it does own and
// requires no Runtime identity, which is exactly what the identity rule now allows.
func TestResourceOnlyPurchaseIsRefundedOnDeletion(t *testing.T) {
	fixture, sub2API, ledger, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	// The Control Plane storage projection carries the provider disk identity, which
	// the Delete operation binds at claim time.
	storage, found, err := fixture.store.GetStorage(context.Background(), "storage-alpha")
	if err != nil || !found {
		t.Fatalf("storage projection found=%v err=%v", found, err)
	}
	storage["providerResourceId"] = "disk-alpha"
	if err := fixture.store.SaveStorage(context.Background(), storage); err != nil {
		t.Fatal(err)
	}
	// The provider readback reports that exact identity for the destroyed resources.
	// A resource-only purchase owns no Runtime controller, so its readback reports no
	// Runtime object: a live Runtime for this Workspace would be an identity conflict,
	// and the deletion chain already required that labelled-object readback absent.
	absent := false
	fixture.fabric.observeState = clients.WorkspaceOwnerObservationAbsent
	fixture.fabric.secretObserveState = clients.WorkspaceOwnerObservationAbsent
	fixture.fabric.storageDestroyResourceID = "disk-alpha"
	fixture.fabric.storageProviderResourceID = "disk-alpha"
	fixture.fabric.storageReadProvider = "tencent-tke"
	fixture.fabric.storageReadStatus = "external_deleted"
	fixture.fabric.storageReadCBSStatus = contracts.WorkspaceDeleteProviderStatusNotFound
	fixture.fabric.storageBindingPresent = &absent
	fixture.fabric.computeReadProvider = "tencent-tke"
	fixture.fabric.computeReadCVMStatus = contracts.WorkspaceDeleteProviderStatusNotFound
	fixture.fabric.computeReadTKEStatus = contracts.WorkspaceDeleteProviderStatusNotFound
	fixture.fabric.computeReadMachinePresent = &absent
	fixture.fabric.computeReadMachineName = "machine-alpha"
	fixture.fabric.computeReadInstanceID = "ins-alpha"
	// The wallet owner refunds a charge it can read back from the gateway, so the
	// original purchase must be verifiable there. This is the platform's existing
	// money rule, not a relaxation of it.
	purchaseUsedAt, purchaseUsedBy := time.Now().UTC().Add(-72*time.Hour), int64(41)
	sub2API.history["opl:workspace-purchase-alpha"] = clients.Sub2APIBalanceHistoryEntry{
		Code: "opl:workspace-purchase-alpha", Type: "balance", ValueUSDMicros: -52_580_000,
		Status: "used", UsedBy: &purchaseUsedBy, UsedAt: &purchaseUsedAt,
	}

	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "accept-resource-only-refund")
	if response.Code != http.StatusOK {
		t.Fatalf("resource-only deletion status=%d body=%s", response.Code, response.Body.String())
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.ProvisioningMode != string(contracts.WorkspaceProvisioningResourceOnly) || operation.RuntimeID != "" {
		t.Fatalf("operation=%#v", operation)
	}
	handler := fixture.server.(*controlPlaneHTTPHandler)
	record, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || record.Status != workspaceDeleteRefundStatusSucceeded || record.RefundUSDMicros <= 0 {
		t.Fatalf("resource-only refund record=%#v found=%v err=%v refunds=%#v", record, found, err, sub2API.refunds)
	}
	if len(sub2API.refunds) != 1 || sub2API.refunds[0].RefundUSDMicros != record.RefundUSDMicros {
		t.Fatalf("resource-only wallet refunds=%#v record=%d", sub2API.refunds, record.RefundUSDMicros)
	}
	// The refund is reserved against the purchase this Workspace actually paid
	// through, and a deletion receipt exists for the same operation.
	if record.RefundOrderOperationID != operation.LaunchOperationID || record.OriginalChargeUSDMicros != 52_580_000 {
		t.Fatalf("resource-only refund basis=%#v", record)
	}
	deletionReceipts := 0
	for _, receipt := range ledger.receipts {
		if receipt.Type == "workspace.deleted.v1" {
			deletionReceipts++
		}
	}
	if deletionReceipts != 1 {
		t.Fatalf("resource-only deletion receipts=%d", deletionReceipts)
	}
}
