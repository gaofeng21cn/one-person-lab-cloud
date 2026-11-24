package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

// Financial commands run against isolated PostgreSQL and the real Ledger HTTP
// owner. Sub2API uses the D1 HTTP financial fixture and Fabric is local-only.
// Faults affect readback only; reconciliation must never repair money itself.
func TestD2FinanceBusinessChain(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	owner, list := startGatewayAccountingLedger(t)

	t.Run("purchases and partial refunds remain independently auditable after workspace deletion", func(t *testing.T) {
		ledger := &d2ReconciliationLedger{LedgerClient: owner, list: list}
		chain := newD1FinanceChain(t, ledger)
		first := chain.purchase
		refundIDs := make([]string, 0, 2)
		for index, amount := range []string{"30.00", "22.58"} {
			status, output := chain.request(t, chain.refundPath(), chain.refund(amount), fmt.Sprintf("d2-partial-%d", index))
			if status != http.StatusCreated || output.Status != "succeeded" {
				t.Fatalf("partial refund status=%d output=%+v", status, output)
			}
			operation := chain.operation(t, output.OperationID)
			if operation.RelatedOperationID != first.ID || operation.Status != "succeeded" {
				t.Fatalf("refund lost original payment: %+v", operation)
			}
			refundIDs = append(refundIDs, output.OperationID)
		}
		// A second real purchase must have independent original transaction and
		// receipt identities. Reuse the fixture resources only after stage reset.
		chain.process.fabric.mu.Lock()
		chain.process.fabric.stages = map[string]clients.WorkspaceLaunchStageResult{}
		chain.process.fabric.mu.Unlock()
		customer := chain.process.login(t, chain.remote.sub2api.ownerEmail, gatewayAccountingOwnerPassword)
		launch := seedRetainedWorkspaceLaunchWithDefaultApplicationCredentialForTest(t, chain.process.handler, gatewayAccountingCredentialForTest(t, chain.process.handler, customer), chain.accountID, chain.purchase.stringFact("ownerUserId"), chain.purchase.int64Fact("sub2apiUserId"), "Second paid Workspace", "basic", "d2-second-purchase", false)
		second := runGatewayAccountingLaunch(t, chain.process, launch.ID, false)
		if second.Status != "succeeded" || second.ID == first.ID || second.stringFact("workspaceId") == first.stringFact("workspaceId") {
			t.Fatalf("second independent purchase failed: %s", workspaceLaunchReconcileResultSummary(second))
		}

		ledger.rejectAccountScan = true
		body := d2RequestReconciliation(t, chain, ledger, "d2-all-original-operations")
		assertReconciliationReport(t, body, "ok", 4, 4, 0)
		d2AssertExactRequests(t, ledger.queries, chain.accountID, []string{first.ID, second.ID, refundIDs[0], refundIDs[1]})

		// Removing the current Workspace projection cannot erase either its
		// purchase or the two partial refunds from the financial audit set.
		mustStore(t, chain.process.store.DeleteWorkspace(context.Background(), first.stringFact("workspaceId")))
		body = d2RequestReconciliation(t, chain, ledger, "d2-deleted-workspace-history")
		assertReconciliationReport(t, body, "ok", 4, 4, 0)

		refund := chain.operation(t, refundIDs[0])
		for _, test := range []struct {
			name, operationID, workspaceID, transactionCode, exceptionCode string
		}{
			{name: "one_missing_original_debit", operationID: second.ID, workspaceID: second.stringFact("workspaceId"), transactionCode: second.stringFact("sub2apiRedeemCode"), exceptionCode: "sub2api_charge_missing"},
			{name: "one_missing_partial_refund", operationID: refundIDs[0], workspaceID: first.stringFact("workspaceId"), transactionCode: refund.CanonicalRedeemCode, exceptionCode: "sub2api_refund_missing"},
		} {
			t.Run(test.name, func(t *testing.T) {
				chain.remote.mu.Lock()
				original := append([]d1FinancialTransaction(nil), chain.remote.transactions...)
				for index, entry := range chain.remote.transactions {
					if entry.Notes == "OPL Cloud balance adjustment: "+test.transactionCode {
						chain.remote.transactions = append(chain.remote.transactions[:index], chain.remote.transactions[index+1:]...)
						break
					}
				}
				chain.remote.mu.Unlock()
				t.Cleanup(func() {
					chain.remote.mu.Lock()
					chain.remote.transactions = original
					chain.remote.mu.Unlock()
				})
				body := d2RequestReconciliation(t, chain, ledger, "d2-"+test.name)
				assertReconciliationReport(t, body, "mismatch", 4, 3, 1)
				d2AssertOperationException(t, mapField(body, "report"), test.operationID, chain.accountID, test.workspaceID, test.exceptionCode)
			})
		}

		for _, test := range []struct {
			name, fault, code string
		}{
			{name: "missing_receipt", fault: "missing", code: "ledger_receipt_missing"},
			{name: "duplicate_receipt", fault: "duplicate", code: "ledger_receipt_mismatch"},
			{name: "wrong_receipt_account", fault: "account", code: "ledger_receipt_mismatch"},
			{name: "wrong_receipt_amount", fault: "amount", code: "ledger_receipt_mismatch"},
		} {
			t.Run(test.name, func(t *testing.T) {
				ledger.faultRequestID, ledger.fault = refundIDs[0], test.fault
				t.Cleanup(func() { ledger.faultRequestID, ledger.fault = "", "" })
				body := d2RequestReconciliation(t, chain, ledger, "d2-"+test.name)
				assertReconciliationReport(t, body, "mismatch", 4, 3, 1)
				d2AssertOperationException(t, mapField(body, "report"), refundIDs[0], chain.accountID, first.stringFact("workspaceId"), test.code)
			})
		}
	})

	t.Run("unconfirmed refund stays visible until original recovery completes evidence", func(t *testing.T) {
		ledger := &d2ReconciliationLedger{LedgerClient: owner, list: list, rejectAccountScan: true}
		chain := newD1FinanceChain(t, ledger)
		chain.remote.mu.Lock()
		chain.remote.dropRefundResponse = true
		chain.remote.mu.Unlock()
		status, pending := chain.request(t, chain.refundPath(), chain.refund("10.00"), "d2-refund-response-lost")
		if status != http.StatusAccepted || pending.Status != "manual_review" {
			t.Fatalf("refund did not retain its uncertain outcome: status=%d output=%+v", status, pending)
		}
		body := d2RequestReconciliation(t, chain, ledger, "d2-pending-refund-unavailable")
		d2AssertPendingOperation(t, body, pending.OperationID)
		chain.remote.mu.Lock()
		chain.remote.historyUnavailable = false
		chain.remote.mu.Unlock()
		body = d2RequestReconciliation(t, chain, ledger, "d2-pending-refund-no-receipt")
		d2AssertPendingOperation(t, body, pending.OperationID)
		before := chain.remote.snapshot()
		input := walletAdjustmentRecoveryRequest{AccountID: chain.accountID, EvidenceRef: "case-20260908-d2refund"}
		status, recovered := chain.request(t, "/api/operator/wallet-adjustments/"+pending.OperationID+"/recover", input, "d2-complete-refund-evidence")
		if status != http.StatusOK || recovered.Status != "succeeded" || recovered.OperationID != pending.OperationID {
			t.Fatalf("original refund recovery status=%d output=%+v", status, recovered)
		}
		if after := chain.remote.snapshot(); after != before {
			t.Fatalf("completing unknown refund evidence paid again: before=%+v after=%+v", before, after)
		}
		body = d2RequestReconciliation(t, chain, ledger, "d2-refund-evidence-completed")
		assertReconciliationReport(t, body, "ok", 2, 2, 0)
	})
}

type d2ReconciliationLedger struct {
	clients.LedgerClient
	list              clients.LedgerReceiptListClient
	queries           []clients.ReceiptQuery
	receiptWrites     int
	rejectAccountScan bool
	faultRequestID    string
	fault             string
}

func (l *d2ReconciliationLedger) RecordReceipt(ctx context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	l.receiptWrites++
	return l.LedgerClient.RecordReceipt(ctx, input, key)
}

func (l *d2ReconciliationLedger) ListReceipts(ctx context.Context, query clients.ReceiptQuery) (clients.ReceiptPage, error) {
	l.queries = append(l.queries, query)
	if l.rejectAccountScan && query.RequestID == "" {
		// An account may have more than 10,000 unrelated usage/task receipts.
		// Exact original-operation queries never read this unrelated history.
		offset, _ := strconv.Atoi(query.Cursor)
		rows := make([]clients.Receipt, 0, query.Limit)
		for index := offset; index < min(offset+query.Limit, 10_001); index++ {
			rows = append(rows, clients.Receipt{ReceiptInput: clients.ReceiptInput{Type: "task.completed.v1", Status: "completed", AccountID: query.AccountID, RequestID: fmt.Sprintf("unrelated-%d", index)}, ReceiptID: fmt.Sprintf("unrelated-receipt-%d", index)})
		}
		return clients.ReceiptPage{Receipts: rows, HasMore: offset+len(rows) < 10_001, NextCursor: strconv.Itoa(offset + len(rows))}, nil
	}
	page, err := l.list.ListReceipts(ctx, query)
	if err != nil || l.faultRequestID == "" {
		return page, err
	}
	rows := make([]clients.Receipt, 0, len(page.Receipts)+1)
	for _, receipt := range page.Receipts {
		if receipt.RequestID != l.faultRequestID || !strings.HasPrefix(receipt.Type, "gateway.wallet_adjustment.") {
			rows = append(rows, receipt)
			continue
		}
		switch l.fault {
		case "missing":
			continue
		case "duplicate":
			duplicate := receipt
			duplicate.ReceiptID += "-duplicate"
			rows = append(rows, duplicate)
		case "account":
			receipt.AccountID = "acct-unrelated-receipt-owner"
		case "amount":
			receipt.Execution = cloneMap(receipt.Execution)
			receipt.Execution["amountUsdMicros"] = 1
		}
		rows = append(rows, receipt)
	}
	page.Receipts = rows
	return page, nil
}

func d2RequestReconciliation(t *testing.T, chain *d1FinanceChain, ledger *d2ReconciliationLedger, key string) map[string]any {
	t.Helper()
	before, beforeWrites := chain.remote.snapshot(), ledger.receiptWrites
	beforeTransactions := len(chain.remote.transactions)
	response := requestWithMutationKeyForTest(t, chain.process.handler, chain.session, http.MethodPost, "/api/billing/reconciliation", `{"confirm":true}`, key)
	if response.Code != http.StatusCreated {
		t.Fatalf("reconciliation status=%d body=%s", response.Code, response.Body.String())
	}
	if after := chain.remote.snapshot(); after != before || ledger.receiptWrites != beforeWrites || len(chain.remote.transactions) != beforeTransactions {
		t.Fatalf("reconciliation changed funds or appended financial receipts: before=%+v after=%+v receipt writes before=%d after=%d", before, after, beforeWrites, ledger.receiptWrites)
	}
	return decodeReconciliationResponse(t, response)
}

func d2AssertExactRequests(t *testing.T, queries []clients.ReceiptQuery, accountID string, operationIDs []string) {
	t.Helper()
	seen := make(map[string]bool)
	for _, query := range queries {
		if query.AccountID != accountID || query.RequestID == "" {
			t.Fatalf("financial audit enumerated unrelated account history: %+v", query)
		}
		seen[query.RequestID] = true
	}
	for _, operationID := range operationIDs {
		if !seen[operationID] {
			t.Fatalf("original operation %s did not receive an exact Ledger readback: %+v", operationID, queries)
		}
	}
}

func d2AssertOperationException(t *testing.T, report map[string]any, operationID, accountID, workspaceID, code string) {
	t.Helper()
	for _, item := range report["exceptions"].([]any) {
		exception := item.(map[string]any)
		if stringValue(exception["operationId"]) == operationID && stringValue(exception["accountId"]) == accountID && stringValue(exception["workspaceId"]) == workspaceID && stringValue(exception["code"]) == code {
			return
		}
	}
	t.Fatalf("exception does not identify the exact customer, Workspace and original transaction: expected %s/%s/%s/%s report=%s", accountID, workspaceID, operationID, code, mustJSON(report))
}

func d2AssertPendingOperation(t *testing.T, body map[string]any, operationID string) {
	t.Helper()
	report := mapField(body, "report")
	counts := mapField(report, "counts")
	if stringValue(report["status"]) != "mismatch" || numberField(counts, "billingOperations", -1) != 2 || numberField(counts, "matched", -1) >= 2 {
		t.Fatalf("unconfirmed refund disappeared or was reported fully reconciled: %s", mustJSON(body))
	}
	for _, item := range report["exceptions"].([]any) {
		if exception := item.(map[string]any); stringValue(exception["operationId"]) == operationID && stringValue(exception["code"]) != "" {
			return
		}
	}
	t.Fatalf("unconfirmed original refund has no actionable exception: %s", mustJSON(body))
}
