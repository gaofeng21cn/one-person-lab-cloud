package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestD2ReceiptRequestLookupMemory(t *testing.T) {
	store := NewMemoryStore()
	for _, receipt := range d2ReceiptLookupHistory() {
		store.receipts[receipt.ReceiptID] = receipt
	}
	assertD2ReceiptRequestLookup(t, store)
}

func TestD2ReceiptRequestLookupPostgres(t *testing.T) {
	store, db := installedLedgerTestPostgres(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	copy, err := tx.Prepare(pq.CopyIn("evidence_receipts", "id", "receipt_type", "status", "account_id", "workspace_id", "payload_json", "idempotency_key", "request_hash", "created_at"))
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range d2ReceiptLookupHistory() {
		payload, err := json.Marshal(receiptPayload{ReceiptInput: receipt.ReceiptInput})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := copy.Exec(receipt.ReceiptID, receipt.Type, receipt.Status, receipt.AccountID, receipt.WorkspaceID, string(payload), receipt.ReceiptID, receipt.ReceiptID, receipt.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := copy.Exec(); err != nil {
		t.Fatal(err)
	}
	if err := copy.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-upgrade schema with existing evidence, then let the real
	// migration runner install the request index without rewriting any receipts.
	if _, err := db.Exec("DROP INDEX evidence_receipts_account_request_created"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM opl_schema_migrations WHERE service = 'ledger' AND version = '202609080001_receipt_request_lookup'"); err != nil {
		t.Fatal(err)
	}
	if err := store.Install(context.Background()); err != nil {
		t.Fatalf("upgrade existing receipt history: %v", err)
	}
	assertD2ReceiptRequestLookup(t, store)
	if _, err := db.Exec("ANALYZE evidence_receipts"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`EXPLAIN (FORMAT JSON) SELECT id FROM evidence_receipts WHERE account_id = $1 AND (payload_json::jsonb ->> 'requestId') = $2 ORDER BY created_at DESC, id DESC LIMIT 101`, "acct-alpha", "workspace-launch-alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("query plan missing")
	}
	var plan string
	if err := rows.Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Logf("exact request lookup plan: %s", plan)
	if !strings.Contains(plan, "evidence_receipts_account_request_created") {
		t.Fatal("exact request lookup scanned unrelated history instead of using the request index")
	}
}

// Persisted evidence may contain duplicate or conflicting historical receipts.
// Exact lookup must expose all of them, including receipts older than 10k other
// account operations, instead of choosing whichever receipt is encountered first.
func d2ReceiptLookupHistory() []Receipt {
	when := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	target := validWorkspaceLaunchReceiptInput("billing.workspace_purchased.v1")
	history := []Receipt{
		{ReceiptInput: target, ReceiptID: "target-a", CreatedAt: when},
		{ReceiptInput: target, ReceiptID: "target-b", CreatedAt: when.Add(time.Second)},
	}
	wrongAccount := validWorkspaceLaunchReceiptInput("billing.workspace_purchased.v1")
	wrongAccount.AccountID = "acct-other"
	history = append(history, Receipt{ReceiptInput: wrongAccount, ReceiptID: "foreign-request", CreatedAt: when.Add(2 * time.Second)})
	wrongWorkspace := validWorkspaceLaunchReceiptInput("billing.workspace_purchased.v1")
	wrongWorkspace.WorkspaceID = "workspace-other"
	history = append(history, Receipt{ReceiptInput: wrongWorkspace, ReceiptID: "foreign-workspace", CreatedAt: when.Add(3 * time.Second)})
	for i := range 10001 {
		input := ReceiptInput{AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", RequestID: fmt.Sprintf("execution-%d", i), Type: "execution.receipt.v1", Status: "completed", Surface: "workspace"}
		history = append(history, Receipt{ReceiptInput: input, ReceiptID: fmt.Sprintf("history-%05d", i), CreatedAt: when.Add(time.Duration(i+10) * time.Second)})
	}
	return history
}

func assertD2ReceiptRequestLookup(t *testing.T, store Store) {
	t.Helper()
	query := ReceiptQuery{AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", RequestID: "workspace-launch-alpha", Type: "billing.workspace_purchased.v1", Limit: 100}
	page, err := store.ListReceipts(context.Background(), query)
	if err != nil || page.Lookup == nil || page.Lookup.RequestID != query.RequestID || len(page.Receipts) != 2 || page.HasMore || page.NextCursor != "" || page.Receipts[0].ReceiptID != "target-b" || page.Receipts[1].ReceiptID != "target-a" {
		t.Fatalf("old original operation and duplicate evidence: page=%#v error=%v", page, err)
	}
	query.Limit = 1
	first, err := store.ListReceipts(context.Background(), query)
	if err != nil || len(first.Receipts) != 1 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first exact page=%#v error=%v", first, err)
	}
	query.Cursor = first.NextCursor
	second, err := store.ListReceipts(context.Background(), query)
	if err != nil || len(second.Receipts) != 1 || second.HasMore || second.Receipts[0].ReceiptID == first.Receipts[0].ReceiptID {
		t.Fatalf("duplicate evidence lost by pagination: page=%#v error=%v", second, err)
	}
	query.Cursor, query.AccountID = "", "acct-no-receipts"
	empty, err := store.ListReceipts(context.Background(), query)
	if err != nil || empty.Lookup == nil || empty.Lookup.AccountID != query.AccountID || len(empty.Receipts) != 0 || empty.HasMore {
		t.Fatalf("wrong account lookup leaked evidence: page=%#v error=%v", empty, err)
	}
	query.AccountID, query.RequestID = "acct-alpha", "workspace-launch-alph"
	empty, err = store.ListReceipts(context.Background(), query)
	if err != nil || len(empty.Receipts) != 0 {
		t.Fatalf("request must match literally, not by prefix: page=%#v error=%v", empty, err)
	}
}

func TestD2InclusiveReceiptListMemory(t *testing.T) {
	assertD2InclusiveReceiptList(t, NewMemoryStore())
}

func TestD2InclusiveReceiptListPostgres(t *testing.T) {
	store, _ := installedLedgerTestPostgres(t)
	assertD2InclusiveReceiptList(t, store)
}

func assertD2InclusiveReceiptList(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	purchase, err := store.RecordReceipt(ctx, validWorkspaceLaunchReceiptInput("billing.workspace_purchased.v1"))
	if err != nil {
		t.Fatal(err)
	}
	refund, err := store.RecordReceipt(ctx, validWalletAdjustmentReceiptInput())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"recharge", "debit", "business_refund"} {
		input := validWalletAdjustmentReceiptInput()
		input.RequestID, input.IdempotencyKey = kind+"-other", kind+"-other:receipt"
		input.Execution["operationId"], input.Execution["kind"] = input.RequestID, kind
		if kind == "business_refund" {
			input.AccountID, input.Owner["accountId"] = "acct-other", "acct-other"
		} else {
			delete(input.InputRefs, "relatedOperationId")
		}
		if _, err := store.RecordReceipt(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	query := ReceiptQuery{AccountID: "acct-alpha", TypePrefix: "billing.", IncludeType: "gateway.wallet_adjustment.v1", IncludeExecutionKind: "business_refund", Limit: 1}
	wanted := map[string]bool{purchase.ReceiptID: true, refund.ReceiptID: true}
	for i := 0; i < 2; i++ {
		page, err := store.ListReceipts(ctx, query)
		if err != nil || page.Lookup == nil || page.Lookup.IncludeExecutionKind != "business_refund" || len(page.Receipts) != 1 || !wanted[page.Receipts[0].ReceiptID] || page.HasMore != (i == 0) {
			t.Fatalf("customer bill page %d=%#v error=%v", i, page, err)
		}
		delete(wanted, page.Receipts[0].ReceiptID)
		query.Cursor = page.NextCursor
	}
	if len(wanted) != 0 {
		t.Fatalf("customer bill missing purchase/refund: %#v", wanted)
	}
}
