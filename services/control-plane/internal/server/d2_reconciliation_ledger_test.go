package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

// d2ReconciliationLedger stays because the historical settlement suite still
// reads the Ledger owner through it. The launch chain that produced those
// receipts is retired with the retained Control Plane launch execution.

type d2ReconciliationLedger struct {
	clients.LedgerClient
	list              clients.LedgerReceiptListClient
	queries           []clients.ReceiptQuery
	receiptWrites     int
	rejectAccountScan bool
	faultRequestID    string
	fault             string
	failNextReceipt   bool
}

func (l *d2ReconciliationLedger) RecordReceipt(ctx context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	l.receiptWrites++
	if l.failNextReceipt {
		l.failNextReceipt = false
		return clients.Receipt{}, fmt.Errorf("injected Ledger write unavailable")
	}
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
