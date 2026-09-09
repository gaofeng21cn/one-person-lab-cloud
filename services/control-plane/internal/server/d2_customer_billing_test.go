package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

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
	reconciler := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{}, &workspaceLaunchUnitAdapter{})
	operation, err := reconciler.Create(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	for range workspaceLaunchReconcileStages {
		if operation.Status != "pending" {
			break
		}
		operation, err = reconciler.Reconcile(context.Background(), operation.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	operation.raw["periodStart"] = mustJSON("2026-08-01T00:00:00Z")
	operation.raw["paidThrough"] = mustJSON("2026-09-01T00:00:00Z")
	if operation.Status != "succeeded" {
		t.Fatalf("original purchase did not complete: %s", operation.Status)
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
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

func TestD2CustomerPartialRefundKeepsOriginalOrderAfterWorkspaceDeletion(t *testing.T) {
	for _, renewal := range []bool{false, true} {
		name := "purchase"
		if renewal {
			name = "renewal"
		}
		t.Run(name, func(t *testing.T) {
			receipt := d2CustomerRefundReceipt()
			ledger := &customerFactsLedger{receipt: receipt, page: clients.ReceiptPage{Receipts: []clients.Receipt{receipt}, NextCursor: "opaque-next", HasMore: true}}
			server := NewServer(newTestService(ledger, &fakeFabricClient{}))
			session := tenantAdminSessionForTest(t, server)
			app := server.(*controlPlaneHTTPHandler).app
			mustStore(t, app.tables.SaveRuntimeOperation(context.Background(), d2CustomerRefundOriginal(t, renewal)))
			// No Workspace row exists: the paid order remains the account and period authority.
			for _, path := range []string{"/api/billing/receipts?cursor=opaque-current&limit=20", "/api/billing/receipts/refund-receipt"} {
				response := requestWithSession(t, server, session, http.MethodGet, path, "")
				if response.Code != http.StatusOK {
					t.Fatalf("%s = %d: %s", path, response.Code, response.Body.String())
				}
				var envelope map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				data := mapField(envelope, "data")
				if strings.Contains(path, "?") {
					if data["nextCursor"] != "opaque-next" || data["hasMore"] != true {
						t.Fatalf("pagination changed: %#v", data)
					}
					items, ok := data["receipts"].([]any)
					if !ok || len(items) != 1 {
						t.Fatalf("receipt list = %#v", data)
					}
					data = items[0].(map[string]any)
				}
				if data["workspaceId"] != "ws-deleted" || data["relatedOperationId"] != "original-order" || data["operationId"] != "refund-order" ||
					data["refundUsdMicros"] != float64(3_000_000) || data["periodStart"] != "2026-08-01T00:00:00Z" || data["paidThrough"] != "2026-09-01T00:00:00Z" || data["kind"] != "business_refund" {
					t.Fatalf("refund projection = %#v", data)
				}
				for _, secret := range []string{"operator-private", "private-upstream-history", "sub2api", "chargeConfirmation"} {
					if strings.Contains(response.Body.String(), secret) {
						t.Fatalf("customer response leaked %q: %s", secret, response.Body.String())
					}
				}
			}
			if ledger.query != (clients.ReceiptQuery{AccountID: "acct-alpha", TypePrefix: "billing.", IncludeType: "gateway.wallet_adjustment.v1", IncludeExecutionKind: "business_refund", Cursor: "opaque-current", Limit: 20}) {
				t.Fatalf("customer Ledger query = %#v", ledger.query)
			}
		})
	}
}

func TestD2CustomerRefundRejectsUnrelatedOrUnconfirmedMoney(t *testing.T) {
	for _, name := range []string{"other account", "missing order", "wrong workspace", "not a refund", "pending refund", "amount exceeds original", "wrong operation"} {
		t.Run(name, func(t *testing.T) {
			receipt := d2CustomerRefundReceipt()
			original := d2CustomerRefundOriginal(t, true)
			switch name {
			case "other account":
				original["accountId"] = "acct-beta"
			case "wrong workspace":
				receipt.WorkspaceID = "ws-other"
			case "not a refund":
				receipt.Execution["kind"] = "recharge"
			case "pending refund":
				receipt.Status = "pending"
			case "amount exceeds original":
				receipt.Execution["amountUsdMicros"] = int64(52_580_001)
			case "wrong operation":
				receipt.Execution["operationId"] = "another-refund"
			}
			ledger := &customerFactsLedger{receipt: receipt, page: clients.ReceiptPage{Receipts: []clients.Receipt{receipt}}}
			server := NewServer(newTestService(ledger, &fakeFabricClient{}))
			session := tenantAdminSessionForTest(t, server)
			if name != "missing order" {
				mustStore(t, server.(*controlPlaneHTTPHandler).app.tables.SaveRuntimeOperation(context.Background(), original))
			}
			for _, path := range []string{"/api/billing/receipts", "/api/billing/receipts/refund-receipt"} {
				response := requestWithSession(t, server, session, http.MethodGet, path, "")
				assertUnavailableWorkspaceEnvelope(t, response, http.StatusBadGateway, "ledger")
			}
		})
	}
}

func TestD2CustomerMonthlyReceiptRetainsOrderReference(t *testing.T) {
	for _, receiptType := range []string{"billing.workspace_purchased.v1", "billing.workspace_renewed.v1"} {
		receipt := workspaceBillingReceipt(receiptType)
		receipt.RequestID = "monthly-order"
		projected, ok := projectCustomerBillingReceipt(receipt)
		if !ok || projected["operationId"] != "monthly-order" || projected["workspaceId"] != receipt.WorkspaceID {
			t.Fatalf("monthly receipt = %#v, ok=%v", projected, ok)
		}
	}
}
