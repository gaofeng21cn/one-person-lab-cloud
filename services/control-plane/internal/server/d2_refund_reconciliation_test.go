package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// Historical inputs are local fixtures. Recovery, persistence, the Sub2API
// readback client, the Ledger receipt and reconciliation all use the real paths.
func TestD2RefundReconciliationLegacyRecovery(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	owner, list := startGatewayAccountingLedger(t)
	ledger := &d2ReconciliationLedger{LedgerClient: owner, list: list, rejectAccountScan: true}
	chain := newD1FinanceChain(t, ledger)
	operationID := "wallet-adjustment-d2-legacy-recovery"
	now := time.Now().UTC()
	beforeRefund := chain.remote.snapshot()
	operation := walletAdjustmentOperation{
		RequestHash: "legacy-original-request", Phase: "authoritative_readback", AccountID: chain.accountID, Sub2APIUserID: gatewayAccountingSub2APIUserID,
		Kind: "business_refund", AmountUSDMicros: 3_000_000, AmountUSD: "3.00", Reason: "original purchase service correction", ActorUserID: "usr-local-operator",
		RelatedOperationID: chain.purchase.ID, AdjustmentAttempted: true, BeforeBalanceKnown: true, BeforeBalanceMicros: beforeRefund.ownerBalance,
		BeforeBalanceReadAt: now.Format(time.RFC3339Nano), ErrorCode: "authoritative_readback_unavailable", CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano), Status: "manual_review",
	}
	if err := chain.process.handler.app.persistWalletAdjustment(context.Background(), operationID, &operation); err != nil {
		t.Fatal(err)
	}
	legacyCode := legacyWalletAdjustmentRedeemCode(operationID)
	if operation.CanonicalRedeemCode != "" || operation.RedeemCodeVersion != "" || len(legacyCode) <= 32 {
		t.Fatalf("fixture is not the historical long-code operation: %+v", operation)
	}
	// Model the retained upstream transaction whose old response was lost. New
	// financial writes intentionally cannot create this legacy long code.
	chain.remote.mu.Lock()
	chain.remote.transactions = append(chain.remote.transactions, d1FinancialTransaction{
		Code: legacyCode, Type: "balance", Value: json.Number("3.00"), AppliedValue: json.Number("3.00"), Status: "used",
		UsedBy: gatewayAccountingSub2APIUserID, UsedAt: now, CreatedAt: now,
	})
	chain.remote.balances[gatewayAccountingSub2APIUserID] += operation.AmountUSDMicros
	chain.remote.refundRequests++
	chain.remote.refundWrites++
	chain.remote.mu.Unlock()
	beforeRecovery := chain.remote.snapshot()
	chain.restart(t, ledger)
	input := walletAdjustmentRecoveryRequest{AccountID: chain.accountID, EvidenceRef: "case-20260908-d2legacy"}
	path := "/api/operator/wallet-adjustments/" + operationID + "/recover"
	status, response := chain.request(t, path, input, "d2-legacy-recovery")
	if status != http.StatusOK || response.Status != "succeeded" || response.OperationID != operationID {
		t.Fatalf("legacy recovery status=%d response=%+v", status, response)
	}
	recovered := chain.operation(t, operationID)
	if recovered.CanonicalRedeemCode != "" || recovered.RedeemCodeVersion != "" || recovered.LegacySupersession != "legacy_history_confirmed" || recovered.ReceiptID == "" || recovered.RelatedOperationID != chain.purchase.ID {
		t.Fatalf("legacy recovery lost its confirmed original identity: %+v", recovered)
	}
	if after := chain.remote.snapshot(); after != beforeRecovery {
		t.Fatalf("legacy recovery changed money: before=%+v after=%+v", beforeRecovery, after)
	}
	chain.restart(t, ledger)
	body := d2RequestReconciliation(t, chain, ledger, "d2-confirmed-legacy-refund")
	assertReconciliationReport(t, body, "ok", 2, 2, 0)
	d2AssertExactRequests(t, ledger.queries, chain.accountID, []string{chain.purchase.ID, operationID})
	beforeReplay := chain.remote.snapshot()
	status, response = chain.request(t, path, input, "d2-legacy-recovery")
	if status != http.StatusOK || response.Status != "succeeded" || chain.remote.snapshot() != beforeReplay || ledger.receiptWrites != 2 {
		t.Fatalf("legacy recovery replay duplicated a financial effect: status=%d response=%+v state=%+v writes=%d", status, response, chain.remote.snapshot(), ledger.receiptWrites)
	}
}

func TestD2RefundReconciliationRejectsUnpaidOriginal(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	owner, list := startGatewayAccountingLedger(t)
	ledger := &d2ReconciliationLedger{LedgerClient: owner, list: list, rejectAccountScan: true}
	chain := newD1FinanceChain(t, ledger)
	status, refunded := chain.request(t, chain.refundPath(), chain.refund("3.00"), "d2-complete-refund-before-source-change")
	if status != http.StatusCreated || refunded.Status != "succeeded" {
		t.Fatalf("refund did not complete: status=%d response=%+v", status, refunded)
	}
	assertReconciliationReport(t, d2RequestReconciliation(t, chain, ledger, "d2-valid-refund-source"), "ok", 2, 2, 0)

	// Preserve the original order identity and terms but replace its source
	// record with a valid pending debit whose charge is not confirmed. A completed
	// refund receipt must not make an unconfirmed original order refundable.
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = chain.purchase.ID, chain.accountID, chain.purchase.stringFact("workspaceId")
	command.OwnerUserID, command.Sub2APIUserID = chain.purchase.stringFact("ownerUserId"), chain.purchase.int64Fact("sub2apiUserId")
	command.RequestHash = chain.purchase.stringFact("requestHash")
	command.PriceVersion, command.TotalChargeUSDMicros = chain.purchase.stringFact("priceVersion"), chain.purchase.int64Fact("totalChargeUsdMicros")
	pending, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	pending.Stage = contracts.StageDebit
	for field := range workspaceLaunchStageCanonicalFacts[contracts.StageKey] {
		pending.raw[field] = chain.purchase.raw[field]
	}
	pending.raw["chargeAttempted"] = mustJSON(true)
	row, err := workspaceLaunchReconcileOperationRow(pending)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeWorkspaceLaunchReconcileOperation(row); err != nil || pending.Status != "pending" || !pending.boolFact("chargeAttempted") || pending.raw["chargeConfirmation"] != nil {
		t.Fatalf("replacement must be a valid typed unconfirmed debit: status=%s err=%v", pending.Status, err)
	}
	mustStore(t, chain.process.store.SaveRuntimeOperation(context.Background(), row))
	body := d2RequestReconciliation(t, chain, ledger, "d2-refund-source-became-unpaid")
	assertReconciliationReport(t, body, "mismatch", 2, 0, 1)
	if numberField(mapField(mapField(body, "report"), "counts"), "pending", -1) != 1 {
		t.Fatalf("unconfirmed original disappeared from the audit set: %s", mustJSON(body))
	}
	d2AssertOperationException(t, mapField(body, "report"), refunded.OperationID, chain.accountID, chain.purchase.stringFact("workspaceId"), "billing_refund_source_invalid")
}
