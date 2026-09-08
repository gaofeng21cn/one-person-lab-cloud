package ledger

import (
	"encoding/json"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func closeoutReceiptForTest(t *testing.T) ReceiptInput {
	t.Helper()
	execution := contracts.WorkspaceLaunchCloseoutReceiptExecution{OperationID: "workspace-launch-closed", AuthorizationID: "operator-close-1", AuthorizedAt: "2026-09-09T01:00:00Z", Reason: "unfulfilled launch", Outcome: "refunded", ChargeConfirmation: &contracts.WorkspaceLaunchCloseoutCharge{Code: "original-debit-code", UserID: 41, ChargeUSDMicros: 52580000, Status: "used"}, RefundedUSDMicros: 52580000, RefundOperationID: "wallet-adjustment-closed", FrozenAt: "2026-09-09T01:00:01Z", KeyRevokedAt: "2026-09-09T01:00:02Z", ResourcesAbsentAt: "2026-09-09T01:00:03Z", CompletedAt: "2026-09-09T01:00:04Z"}
	cost := contracts.WorkspaceLaunchCloseoutReceiptCost{Currency: "USD", PriceVersion: "pilot-usd-2026-07-v1", ChargeUSDMicros: 52580000, PeriodStart: "2026-09-09T00:00:00Z", PaidThrough: "2026-10-09T00:00:00Z"}
	input := ReceiptInput{Type: string(contracts.ReceiptTypeWorkspaceClosed), Status: "completed", Surface: "control_plane", AccountID: "acct-closed", WorkspaceID: "workspace-closed", RequestID: execution.OperationID, IdempotencyKey: execution.OperationID + ":closeout:ledger", Actor: map[string]any{"userId": "usr-admin"}}
	encoded, _ := json.Marshal(execution)
	if err := json.Unmarshal(encoded, &input.Execution); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(cost)
	if err := json.Unmarshal(encoded, &input.Cost); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestWorkspaceLaunchCloseoutReceiptRejectsUnprovenClosure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ReceiptInput)
	}{
		{"refund below original debit", func(r *ReceiptInput) { r.Execution["refundedUsdMicros"] = 52579999 }},
		{"unknown funds", func(r *ReceiptInput) { r.Execution["outcome"] = "unknown" }},
		{"resource absence missing", func(r *ReceiptInput) { delete(r.Execution, "resourcesAbsentAt") }},
		{"refund before resources", func(r *ReceiptInput) { r.Execution["completedAt"] = "2026-09-09T01:00:02Z" }},
		{"different operation", func(r *ReceiptInput) { r.Execution["operationId"] = "workspace-launch-other" }},
		{"untyped provider state", func(r *ReceiptInput) { r.Execution["providerInstanceId"] = "cvm-unscoped" }},
		{"invalid paid period", func(r *ReceiptInput) { r.Cost["paidThrough"] = r.Cost["periodStart"] }},
		{"not charged with refund", func(r *ReceiptInput) { r.Execution["outcome"] = "failed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := closeoutReceiptForTest(t)
			if err := validateReceiptInput(input); err != nil {
				t.Fatalf("valid closure rejected: %v", err)
			}
			tc.change(&input)
			if err := validateReceiptInput(input); err == nil {
				t.Fatal("unproven closeout admitted")
			}
		})
	}
	input := closeoutReceiptForTest(t)
	input.Execution["outcome"] = "failed"
	delete(input.Execution, "chargeConfirmation")
	input.Execution["refundedUsdMicros"] = 0
	input.Execution["refundOperationId"] = ""
	input.Cost["chargeUsdMicros"] = 0
	delete(input.Cost, "periodStart")
	delete(input.Cost, "paidThrough")
	if err := validateReceiptInput(input); err != nil {
		t.Fatalf("uncharged closure needs no invented billing period: %v", err)
	}
}
