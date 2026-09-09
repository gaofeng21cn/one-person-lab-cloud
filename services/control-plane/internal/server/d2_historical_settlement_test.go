package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The fixed receipt payload below follows the schema-2 writer retained at
// 34f9b732^:workspace_launch.go:4056-4105. It deliberately does not call the
// current settlement projection or receipt builder to create expected evidence.
func TestD2HistoricalWorkspaceSettlementsThroughLedgerHTTP(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	ledgerDatabaseURL := gatewayAccountingDatabase(t, "opl_gateway_ledger_")
	owner, list := startGatewayAccountingLedger(t, ledgerDatabaseURL)
	ledgerDatabase, err := sql.Open("postgres", ledgerDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledgerDatabase.Close() })
	for _, test := range []struct {
		name     string
		refunded bool
		conflict string
		wantCode string
	}{
		{name: "completed_purchase_without_current_workspace"},
		{name: "automatic_refund_uses_one_receipt_for_both_transfers", refunded: true},
		{name: "purchase_receipt_amount_conflict", conflict: "receipt_amount", wantCode: "ledger_receipt_mismatch"},
		{name: "purchase_account_confirmation_conflict", conflict: "confirmation_account", wantCode: "sub2api_charge_mismatch"},
		{name: "purchase_persisted_amount_conflict", conflict: "persisted_amount", wantCode: "billing_operation_invalid"},
		{name: "refund_receipt_amount_conflict", refunded: true, conflict: "refund_amount", wantCode: "ledger_receipt_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := newTestPostgresEntStateStore(gatewayAccountingDatabase(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.(*postgresEntStateStore).client.Close() })
			account, user := provisionedAccountRowsFor("acct-d2-history", "usr-d2-history", "history@example.test", 41)
			mustStore(t, store.CreateProvisionedAccount(ctx, account, user))
			operationID := "launch-history-" + stableID(t.Name())[:12]
			history, receiptInput := d2HistoricalSettlementEvidence(test.refunded, operationID)
			switch test.conflict {
			case "receipt_amount":
				receiptInput.Cost["totalUsdMicros"] = int64(1)
			case "confirmation_account":
				history.ChargeConfirmation.UserID = 42
			case "persisted_amount":
				history.TotalChargeUSDMicros = 1
			case "refund_amount":
				receiptInput.Cost["refundUsdMicros"] = int64(1)
			}
			// These bytes predate current receipt-admission rules. Seed retained
			// persistence, then exercise the actual Ledger HTTP reader unchanged.
			payload, err := json.Marshal(receiptInput)
			if err != nil {
				t.Fatal(err)
			}
			receiptID := "receipt-" + operationID
			_, err = ledgerDatabase.ExecContext(ctx, `INSERT INTO evidence_receipts
				(id, receipt_type, status, account_id, workspace_id, payload_json, idempotency_key, request_hash, created_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, receiptID, receiptInput.Type, receiptInput.Status, receiptInput.AccountID, receiptInput.WorkspaceID,
				string(payload), operationID+":historical-receipt", stableID(string(payload)), "2026-07-01T00:00:00Z")
			if err != nil {
				t.Fatal(err)
			}
			history.ReceiptID = receiptID
			status := "succeeded"
			if test.refunded {
				status, history.RefundReceiptID = "refunded", receiptID
			}
			encoded, err := json.Marshal(history)
			if err != nil {
				t.Fatal(err)
			}
			// The original schema-2 writer retained the financial owner inside
			// chargeConfirmation; it did not write a top-level Sub2API user ID.
			var persisted map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &persisted); err != nil {
				t.Fatal(err)
			}
			delete(persisted, "sub2apiUserId")
			encoded, err = json.Marshal(persisted)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, store.SaveRuntimeOperation(ctx, map[string]any{
				"id": operationID, "operationId": operationID, "accountId": "acct-d2-history", "workspaceId": "ws-d2-history-deleted",
				"resourceId": "ws-d2-history-deleted", "resourceKind": "workspace_launch", "action": workspaceLaunchAction,
				"status": status, "result": string(encoded), "createdAt": "2026-07-01T00:00:00Z",
			}))
			if _, found, err := store.GetWorkspace(ctx, history.WorkspaceID); err != nil || found {
				t.Fatalf("historical test unexpectedly has a current Workspace: found=%t err=%v", found, err)
			}
			usedAt := time.Date(2026, 7, 1, 0, 1, 0, 0, time.UTC)
			usedBy := int64(41)
			transactions := []clients.Sub2APIBalanceHistoryEntry{{Code: "opl:history-purchase", Type: "balance", Status: "used", ValueUSDMicros: -52_580_000, UsedBy: &usedBy, UsedAt: &usedAt, CreatedAt: usedAt}}
			if test.refunded {
				transactions = append(transactions, clients.Sub2APIBalanceHistoryEntry{Code: "opl:history-refund", Type: "balance", Status: "used", ValueUSDMicros: 52_580_000, UsedBy: &usedBy, UsedAt: &usedAt, CreatedAt: usedAt})
			}
			remote := &customerFactsSub2API{testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}}, history: map[int64][]clients.Sub2APIBalanceHistoryEntry{41: transactions}}
			ledger := &d2ReconciliationLedger{LedgerClient: owner, list: list, rejectAccountScan: true}
			calls := &[]string{}
			fabric := &fakeFabricClient{calls: calls}
			server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, remote), store)
			if err != nil {
				t.Fatal(err)
			}
			response := requestWithMutationKeyForTest(t, server, operatorSessionForTest(t, server), http.MethodPost, "/api/billing/reconciliation", `{"confirm":true}`, "d2-historical-"+test.name)
			if response.Code != http.StatusCreated {
				t.Fatalf("historical reconciliation status=%d body=%s", response.Code, response.Body.String())
			}
			body := decodeReconciliationResponse(t, response)
			count := 1
			if test.refunded {
				count = 2
			}
			if test.wantCode == "" {
				assertReconciliationReport(t, body, "ok", count, count, 0)
				d2AssertExactRequests(t, ledger.queries, "acct-d2-history", []string{operationID})
			} else {
				report := mapField(body, "report")
				if stringValue(report["status"]) != "mismatch" || numberField(mapField(report, "counts"), "matched", -1) >= float64(count) {
					t.Fatalf("historical conflict was accepted: %s", mustJSON(body))
				}
				d2AssertOperationException(t, report, operationID, "acct-d2-history", "ws-d2-history-deleted", test.wantCode)
			}
			if len(remote.charges) != 0 || ledger.receiptWrites != 0 || len(*calls) != 0 {
				t.Fatalf("historical readback wrote financial evidence or accessed current resources: charges=%v receipts=%d fabric=%v", remote.charges, ledger.receiptWrites, *calls)
			}
		})
	}
}

func d2HistoricalSettlementEvidence(refunded bool, operationID string) (workspaceLaunchBillingHistory, clients.ReceiptInput) {
	history := workspaceLaunchBillingHistory{
		walletRefundChargeFacts: walletRefundChargeFacts{
			AccountID: "acct-d2-history", RedeemCode: "opl:history-purchase", TotalChargeUSDMicros: 52_580_000, Phase: "complete",
			ChargeConfirmation: &walletRefundCharge{Code: "opl:history-purchase", UserID: 41, AmountUSDMicros: 52_580_000, Status: "used"},
		},
		SchemaVersion: 2, OwnerUserID: "usr-d2-history", WorkspaceID: "ws-d2-history-deleted", PackageID: "basic", StorageGB: 10,
		PriceVersion: "pilot-usd-2026-07-v1", PeriodStart: "2026-07-01T00:00:00Z", PaidThrough: "2026-08-01T00:00:00Z",
		ComputeID: "compute-history", StorageID: "storage-history", AttachmentID: "attachment-history", WorkspaceAPIKeyID: 91,
		WorkspaceKeyFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RuntimeID: "runtime-history", RuntimeServiceName: "runtime-service-history", ChargeAttempted: true,
	}
	receipt := clients.ReceiptInput{
		Type: "billing.workspace_purchased.v1", Status: "completed", Surface: "control_plane", AccountID: "acct-d2-history", WorkspaceID: "ws-d2-history-deleted", RequestID: operationID,
		Execution: map[string]any{
			"resourceType": "workspace", "resourceId": "ws-d2-history-deleted", "computeAllocationId": "compute-history", "storageId": "storage-history",
			"attachmentId": "attachment-history", "workspaceApiKeyId": int64(91), "workspaceKeyFingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "runtimeId": "runtime-history", "runtimeServiceName": "runtime-service-history",
		},
		Cost: map[string]any{
			"priceVersion": "pilot-usd-2026-07-v1", "currency": "USD", "billingUnit": "calendar_month", "totalUsdMicros": int64(52_580_000),
			"sub2apiUserId": int64(41), "sub2apiRedeemCode": "opl:history-purchase", "postChargeBalanceUsdMicros": int64(47_420_000),
			"periodStart": "2026-07-01T00:00:00Z", "paidThrough": "2026-08-01T00:00:00Z", "resourceType": "workspace", "resourceId": "ws-d2-history-deleted",
			"components": map[string]any{
				"compute": map[string]any{"resourceType": "compute", "resourceId": "compute-history", "chargeUsdMicros": int64(50_000_000)},
				"storage": map[string]any{"resourceType": "storage", "resourceId": "storage-history", "sizeGb": int64(10), "chargeUsdMicros": int64(2_580_000)},
			},
		},
		Owner: map[string]any{"accountId": "acct-d2-history", "workspaceId": "ws-d2-history-deleted", "ownerUserId": "usr-d2-history"},
	}
	if refunded {
		history.Phase, history.RefundCode, history.RefundAttempted, history.RefundReason = "refunded", "opl:history-refund", true, "fabric_compute_confirmed_absent"
		history.RefundConfirmation = map[string]any{"code": "opl:history-refund", "userId": int64(41), "refundUsdMicros": int64(52_580_000), "status": "used"}
		receipt.Type = "billing.workspace_refunded.v1"
		receipt.Execution = map[string]any{
			"resourceType": "workspace", "resourceId": "ws-d2-history-deleted", "reason": "fabric_compute_confirmed_absent", "computeAllocationId": "compute-history", "storageId": "storage-history",
			"refundConfirmation": map[string]any{"code": "opl:history-refund", "userId": int64(41), "refundUsdMicros": int64(52_580_000), "status": "used"},
		}
		delete(receipt.Cost, "postChargeBalanceUsdMicros")
		receipt.Cost["sub2apiRefundCode"], receipt.Cost["refundUsdMicros"] = "opl:history-refund", int64(52_580_000)
	}
	return history, receipt
}
