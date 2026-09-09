package ledger

import (
	"context"
	"errors"
	"testing"
)

func TestD2ReconciliationOperationEvidenceMemory(t *testing.T) {
	assertD2ReconciliationOperationEvidence(t, NewMemoryStore())
}

func TestD2ReconciliationOperationEvidencePostgres(t *testing.T) {
	store, _ := installedLedgerTestPostgres(t)
	assertD2ReconciliationOperationEvidence(t, store)
}

func assertD2ReconciliationOperationEvidence(t *testing.T, store Store) {
	t.Helper()
	exception := func(operationID, kind, code string) map[string]any {
		return map[string]any{"resourceType": "workspace", "resourceId": "workspace-alpha", "accountId": "acct-alpha", "workspaceId": "workspace-alpha", "operationId": operationID, "kind": kind, "code": code}
	}
	for _, test := range []struct {
		name                      string
		checked, matched, pending int
		exceptions                []any
		wantError                 bool
	}{
		{name: "purchase and two renewal periods on same Workspace", checked: 3, exceptions: []any{exception("purchase", "purchase", "ledger_receipt_missing"), exception("renew-august", "renewal", "ledger_receipt_missing"), exception("renew-september", "renewal", "sub2api_charge_missing")}},
		{name: "multiple failures of one operation count once", checked: 1, exceptions: []any{exception("renew", "renewal", "ledger_receipt_missing"), exception("renew", "renewal", "sub2api_charge_mismatch")}},
		{name: "purchase and refund legs count independently", checked: 2, exceptions: []any{exception("purchase", "purchase", "sub2api_charge_mismatch"), exception("purchase", "refund", "sub2api_refund_mismatch")}},
		{name: "in progress purchase does not block new business", checked: 2, matched: 1, pending: 1, exceptions: []any{}},
		{name: "pending plus failed refund", checked: 2, pending: 1, exceptions: []any{exception("refund", "refund", "sub2api_refund_missing")}},
		{name: "refund source missing", checked: 1, exceptions: []any{exception("refund", "refund", "billing_refund_source_invalid")}},
		{name: "refund limit exceeded", checked: 1, exceptions: []any{exception("refund", "refund", "billing_refund_limit_exceeded")}},
		{name: "transaction reused", checked: 1, exceptions: []any{exception("purchase", "purchase", "billing_transaction_duplicate")}},
		{name: "negative pending rejected", checked: 1, matched: 1, pending: -1, exceptions: []any{}, wantError: true},
		{name: "pending cannot hide exception", checked: 1, pending: 1, exceptions: []any{exception("purchase", "purchase", "ledger_receipt_missing")}, wantError: true},
		{name: "same Workspace is not same operation", checked: 1, exceptions: []any{exception("renew-a", "renewal", "ledger_receipt_missing"), exception("renew-b", "renewal", "ledger_receipt_missing")}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := "ok"
			if len(test.exceptions) > 0 {
				status = "mismatch"
			}
			input := ReconciliationInput{IdempotencyKey: test.name, Report: map[string]any{
				"id": test.name, "status": status,
				"counts":     map[string]any{"billingOperations": test.checked, "matched": test.matched, "pending": test.pending, "exceptions": len(test.exceptions)},
				"exceptions": test.exceptions,
			}}
			result, err := store.RecordReconciliation(context.Background(), input)
			if test.wantError {
				if !errors.Is(err, ErrInvalidReconciliationInput) {
					t.Fatalf("invalid report accepted: %#v, %v", result, err)
				}
				return
			}
			if err != nil || result.BlockNewWorkspaces != (status == "mismatch") {
				t.Fatalf("report=%#v error=%v", result, err)
			}
			replayed, err := store.RecordReconciliation(context.Background(), input)
			if err != nil || !replayed.Replayed || replayed.BlockNewWorkspaces != result.BlockNewWorkspaces {
				t.Fatalf("replay=%#v error=%v", replayed, err)
			}
		})
	}
	for _, field := range []string{"accountId", "operationId", "kind", "workspaceId"} {
		record := exception("renewal-alpha", "renewal", "ledger_receipt_missing")
		delete(record, field)
		input := ReconciliationInput{IdempotencyKey: "partial-" + field, Report: map[string]any{
			"id": "partial-" + field, "status": "mismatch", "counts": map[string]any{"billingOperations": 1, "matched": 0, "exceptions": 1}, "exceptions": []any{record},
		}}
		if _, err := store.RecordReconciliation(context.Background(), input); !errors.Is(err, ErrInvalidReconciliationInput) {
			t.Fatalf("partial identity without %s error=%v", field, err)
		}
	}
}
