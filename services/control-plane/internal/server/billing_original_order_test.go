package server

import (
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// d2CustomerRefundOriginal builds the historical original order that a refund
// references. The retained Control Plane launch execution that used to drive
// the purchase to completion is retired, so this fixture writes the same
// persisted facts the production receipt builder reads: the accepted price
// components, the canonical receipt execution identity and the billing
// period. The row is decoded before it is returned, so a fixture that stops
// matching the production contract fails here instead of in the assertion.
func d2CustomerRefundOriginal(t *testing.T, renewal bool) map[string]any {
	t.Helper()
	if renewal {
		return workspaceRenewalOperationRow(workspaceRenewalOperation{
			ID: "original-order", Status: "active", Phase: "complete", RequestHash: "local-renewal-hash",
			AccountID: "acct-alpha", WorkspaceID: "ws-deleted", PriceVersion: "pricing-v1", TotalUSDMicros: 52_580_000,
			PeriodStart: "2026-07-01T00:00:00Z", PaidThrough: "2026-08-01T00:00:00Z", RenewedThrough: "2026-09-01T00:00:00Z",
		})
	}
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "original-order", "acct-alpha", "ws-deleted"
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	for fact, value := range map[string]any{
		"computeAllocationId":     "compute-ws-deleted",
		"storageId":               "storage-ws-deleted",
		"attachmentId":            "attachment-ws-deleted",
		"runtimeId":               "runtime-ws-deleted",
		"runtimeServiceName":      "runtime-ws-deleted",
		"gatewaySecretRef":        "gateway-secret-ws-deleted",
		"workspaceKeyFingerprint": strings.Repeat("f", 64),
		"workspaceApiKeyId":       int64(9),
		"periodStart":             "2026-08-01T00:00:00Z",
		"paidThrough":             "2026-09-01T00:00:00Z",
	} {
		operation.raw[fact] = mustJSON(value)
	}
	plan, err := contracts.WorkspaceProvisioningStages(contracts.WorkspaceProvisioningFull)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range plan[:len(plan)-1] {
		attempt := operation.Attempts[stage]
		attempt.Attempted = 1
		attempt.Confirmed = 1
		attempt.Status = "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operationWithStage(operation, stage), 1)
		operation.Attempts[stage] = attempt
	}
	operation.Stage = contracts.StageSucceeded
	operation.Status = contracts.StatusSucceeded
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatalf("seeded original purchase is not a decodable historical order: %v category=%s fields=%v", err, workspaceLaunchDecodeFailureCategory(err), workspaceLaunchDecodeFailedFields(err))
	}
	if _, err := workspaceLaunchPurchaseReceiptInput(decoded); err != nil {
		t.Fatalf("seeded original purchase has no purchase receipt: %v", err)
	}
	return row
}

func d2CustomerRefundReceipt() clients.Receipt {
	return clients.Receipt{
		ReceiptInput: walletAdjustmentReceipt("refund-order", walletAdjustmentOperation{
			AccountID: "acct-alpha", ActorUserID: "operator-private", Kind: "business_refund", AmountUSDMicros: 3_000_000,
			RelatedOperationID: "original-order", BalanceHistoryRef: "private-upstream-history",
		}),
		ReceiptID: "refund-receipt", CreatedAt: "2026-08-02T00:00:00Z",
	}
}

// billingReconciliationRowForTest builds the reconciliation guard row the
// tenant isolation and operator projection suites read.
func billingReconciliationRowForTest(id, status, reason string) map[string]any {
	return map[string]any{
		"id": id, "status": status,
		"guard":  map[string]any{"status": status, "reason": reason, "blockNewWorkspaces": status == "mismatch"},
		"report": map[string]any{"id": id, "status": status},
	}
}
