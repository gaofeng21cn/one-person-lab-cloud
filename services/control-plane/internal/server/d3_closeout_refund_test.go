package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// This financial-boundary fixture runs a real purchase through its confirmed
// debit. The already-fenced/absent resource facts are explicit owner fixtures;
// the closeout orchestration tests exercise those physical owners separately.
func newD3CloseoutRefundChain(t *testing.T, ledger clients.LedgerClient) *d1FinanceChain {
	t.Helper()
	accountID := "acct-closeout-" + stableID(t.Name())[:12]
	chain := &d1FinanceChain{accountID: accountID, databaseURL: gatewayAccountingDatabase(t)}
	chain.remote = newD1FinancialHTTPFixture(t)
	t.Setenv("NODE_ENV", "")
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "0")
	t.Setenv(controlledBasicPilotEnabledEnv, "1")
	t.Setenv(controlledBasicPilotAccountsEnv, accountID)
	t.Setenv(controlledBasicPilotMaxInFlightEnv, "1")
	chain.process = chain.openControlPlane(t, ledger)
	account, user := provisionedAccountRowsFor(accountID, "usr-closeout-"+stableID(t.Name())[:12], chain.remote.sub2api.ownerEmail, gatewayAccountingSub2APIUserID)
	mustStore(t, chain.process.store.CreateProvisionedAccount(context.Background(), account, user))
	owner := chain.process.login(t, chain.remote.sub2api.ownerEmail, gatewayAccountingOwnerPassword)
	launch := owner.mustRequest(t, http.MethodPost, "/api/workspace-launches", json.RawMessage(`{"name":"Unfulfilled paid Workspace","packageId":"basic","autoRenew":false}`), "d3-closeout-purchase", http.StatusAccepted)
	operationID := stringValue(launch["operationId"])
	reconciler := chain.process.handler.app.workspaceLaunchReconciler(chain.process.handler.service, clients.SessionDelegatedCredential{}, 0)
	for range 4 {
		row, found, err := chain.process.store.GetRuntimeOperation(context.Background(), operationID)
		if err != nil || !found {
			t.Fatalf("original order missing: %v", err)
		}
		chain.purchase, err = decodeWorkspaceLaunchReconcileOperation(row)
		if err != nil {
			t.Fatal(err)
		}
		if chain.purchase.Stage == contracts.StageCompute {
			break
		}
		if _, err := reconciler.Reconcile(context.Background(), operationID); err != nil {
			t.Fatal(err)
		}
	}
	if chain.purchase.Stage != contracts.StageCompute || chain.purchase.raw["chargeConfirmation"] == nil {
		t.Fatalf("purchase did not stop after confirmed debit: %s", workspaceLaunchReconcileResultSummary(chain.purchase))
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	chain.purchase.Closeout = &workspaceLaunchCloseout{AuthorizationID: "closeout-" + stableID(t.Name())[:12], LaunchVersion: chain.purchase.Version, AuthorizedBy: "usr-closeout-operator", AuthorizedAt: now, Reason: "confirmed unfulfilled order", Phase: "refund", FrozenAt: now, KeyRevokedAt: now, ResourcesAbsentAt: now, DebitState: "confirmed"}
	var err error
	chain.purchase, err = reconciler.persist(context.Background(), chain.purchase)
	if err != nil {
		t.Fatal(err)
	}
	chain.session = reservedOperatorSessionForTest(t, chain.process.handler)
	return chain
}

func finishD3RefundCloseout(t *testing.T, chain *d1FinanceChain) workspaceLaunchReconcileOperation {
	t.Helper()
	reconciler := chain.process.handler.app.workspaceLaunchReconciler(chain.process.handler.service, clients.SessionDelegatedCredential{}, 0)
	var operation workspaceLaunchReconcileOperation
	for range 4 {
		var err error
		operation, err = reconciler.Reconcile(context.Background(), chain.purchase.ID)
		if err != nil {
			t.Fatal(err)
		}
		if operation.Status == contracts.StatusRefunded {
			return operation
		}
	}
	t.Fatalf("closeout did not settle: %s %+v", workspaceLaunchReconcileResultSummary(operation), operation.Closeout)
	return operation
}

func TestD3CloseoutRefundBusinessChain(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	ledger, list := startGatewayAccountingLedger(t)
	t.Run("partial manual refund and closing remainder reconcile without a successful purchase receipt", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		status, manual := chain.request(t, chain.refundPath(), chain.refund("10.00"), "manual-partial")
		if status != http.StatusCreated || manual.Status != "succeeded" {
			t.Fatalf("manual partial refund failed: status=%d body=%+v", status, manual)
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || closed.Closeout.RefundOperationID == "" || closed.stringFact("receiptId") != "" {
			t.Fatalf("wrong closeout money or false purchase evidence: %+v", closed.Closeout)
		}
		state := chain.remote.snapshot()
		if state.refundWrites != 2 || state.refunded != gatewayAccountingChargeMicros {
			t.Fatalf("closing remainder duplicated a partial payment: %+v", state)
		}
		closing := chain.operation(t, closed.Closeout.RefundOperationID)
		if closing.AmountUSDMicros != gatewayAccountingChargeMicros-10_000_000 {
			t.Fatalf("wrong closing remainder: %+v", closing)
		}
		report, err := chain.process.handler.app.billingReconciliationReport(context.Background(), chain.process.handler.service, "d3-closeout-reconciled")
		if err != nil {
			t.Fatal(err)
		}
		d2AssertReportCounts(t, report, "ok", 3, 3, 0)
		page, err := list.ListReceipts(context.Background(), clients.ReceiptQuery{AccountID: chain.accountID, TypePrefix: "billing.", IncludeType: "gateway.wallet_adjustment.v1", IncludeExecutionKind: "business_refund", Limit: 100})
		if err != nil || len(page.Receipts) != 3 {
			t.Fatalf("customer receipt count=%d err=%v", len(page.Receipts), err)
		}
		for _, receipt := range page.Receipts {
			projected, ok := chain.process.handler.app.projectCustomerBillingReceipt(context.Background(), receipt)
			if !ok {
				t.Fatalf("closeout broke customer billing: type=%s", receipt.Type)
			}
			if receipt.Type == "billing.workspace_closed.v1" && numberField(projected, "chargeUsdMicros", 0) != float64(gatewayAccountingChargeMicros) {
				t.Fatalf("original debit missing from customer statement: %+v", projected)
			}
		}
		chain.restart(t, ledger)
		finishD3RefundCloseout(t, chain)
		if chain.remote.snapshot().refundWrites != 2 {
			t.Fatal("restart refunded the order again")
		}
	})
	t.Run("already fully refunded original needs no zero amount payment", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		status, manual := chain.request(t, chain.refundPath(), chain.refund(formatWalletUSD(gatewayAccountingChargeMicros)), "manual-full")
		if status != http.StatusCreated || manual.Status != "succeeded" {
			t.Fatalf("manual full refund failed: status=%d %+v", status, manual)
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundOperationID != "" || closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 1 {
			t.Fatalf("already-refunded original sent another payment: %+v", closed.Closeout)
		}
	})
	t.Run("unknown manual refund resolves read only before remainder is fixed", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		chain.remote.dropRefundResponse = true
		status, manual := chain.request(t, chain.refundPath(), chain.refund("10.00"), "manual-lost-response")
		if status != http.StatusAccepted || manual.Status != "manual_review" {
			t.Fatalf("response loss not retained: status=%d %+v", status, manual)
		}
		chain.remote.mu.Lock()
		chain.remote.historyUnavailable = false
		chain.remote.mu.Unlock()
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 2 {
			t.Fatalf("recovery resent unknown manual payment: %+v", chain.remote.snapshot())
		}
	})
	t.Run("unattempted manual reservation prevents early closing refund", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		id := "wallet-adjustment-unattempted-manual"
		manual := walletAdjustmentOperation{RequestHash: "d3-pending-manual", Phase: "before_balance", AccountID: chain.accountID, Sub2APIUserID: gatewayAccountingSub2APIUserID, Kind: "business_refund", AmountUSDMicros: 10_000_000, AmountUSD: "10.00", Reason: "manual original correction", RelatedOperationID: chain.purchase.ID, ActorUserID: "usr-operator", CanonicalRedeemCode: walletAdjustmentRedeemCode(id), RedeemCodeVersion: "v2", CreatedAt: now, UpdatedAt: now, Status: "pending"}
		manual, err := chain.process.store.SaveWalletAdjustment(context.Background(), id, manual)
		if err != nil {
			t.Fatal(err)
		}
		refundID, amount, complete, err := chain.process.handler.app.refundWorkspaceLaunchCloseout(context.Background(), chain.process.handler.service, chain.purchase)
		if err != nil || complete || refundID != "" || amount != 0 || chain.remote.snapshot().refundWrites != 0 {
			t.Fatalf("reservation was treated as returned money: id=%s amount=%d complete=%v err=%v", refundID, amount, complete, err)
		}
		manual.Status, manual.Phase = "failed", "complete"
		if _, err := chain.process.store.SaveWalletAdjustment(context.Background(), id, manual); err != nil {
			t.Fatal(err)
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 1 {
			t.Fatalf("released reservation lost refundable remainder: %+v", closed.Closeout)
		}
	})
	t.Run("two processes reserve and dispatch the same closing payment once", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		second := chain.openControlPlane(t, ledger)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, process := range []*gatewayAccountingControlPlane{chain.process, second} {
			go func() {
				<-start
				_, _, _, err := process.handler.app.refundWorkspaceLaunchCloseout(context.Background(), process.handler.service, chain.purchase)
				results <- err
			}()
		}
		close(start)
		for range 2 {
			if err := <-results; err != nil && !errors.Is(err, errWalletAdjustmentConflict) && !errors.Is(err, errWalletAdjustmentState) {
				t.Fatal(err)
			}
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 1 {
			t.Fatalf("two processes refunded the same original twice: %+v", chain.remote.snapshot())
		}
	})
	t.Run("manual full refund closes an automatic payment rejected before dispatch", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		reservations, err := chain.process.store.ReserveWorkspaceLaunchCloseoutRefund(context.Background(), chain.purchase)
		if err != nil || len(reservations) != 1 {
			t.Fatalf("closing reservation missing: count=%d err=%v", len(reservations), err)
		}
		closing := reservations[0]
		closing.Operation.Status, closing.Operation.Phase = "failed", "complete"
		if _, err := chain.process.store.SaveWalletAdjustment(context.Background(), closing.ID, closing.Operation); err != nil {
			t.Fatal(err)
		}
		status, response := chain.request(t, chain.refundPath(), chain.refund(formatWalletUSD(gatewayAccountingChargeMicros)), "manual-after-rejected-closing")
		if status != http.StatusCreated || response.Status != "succeeded" {
			t.Fatalf("released budget could not be returned: status=%d response=%+v", status, response)
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundOperationID != "" || closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 1 {
			t.Fatalf("failed unattempted payment stranded a fully returned order: %+v", closed.Closeout)
		}
	})
	t.Run("mismatched original debit prevents a closing payment", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		chain.remote.mu.Lock()
		for i := range chain.remote.transactions {
			if chain.remote.transactions[i].Code == chain.purchase.stringFact("sub2apiRedeemCode") {
				chain.remote.transactions[i].Value = json.Number("-1.00")
			}
		}
		chain.remote.mu.Unlock()
		before := chain.remote.snapshot()
		id, amount, complete, err := chain.process.handler.app.refundWorkspaceLaunchCloseout(context.Background(), chain.process.handler.service, chain.purchase)
		if err == nil || id != "" || amount != 0 || complete || chain.remote.snapshot() != before {
			t.Fatalf("mismatched original debit allowed money movement: id=%s amount=%d complete=%v err=%v", id, amount, complete, err)
		}
		if _, found, err := chain.process.store.GetRuntimeOperation(context.Background(), workspaceLaunchRefundOperationID(chain.purchase.ID)); err != nil || found {
			t.Fatalf("unproven original debit acquired refund capacity: found=%v err=%v", found, err)
		}
	})
	t.Run("a retained manual refund cannot close the order without its Ledger evidence", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		status, response := chain.request(t, chain.refundPath(), chain.refund("10.00"), "manual-before-missing-receipt")
		if status != http.StatusCreated || response.Status != "succeeded" {
			t.Fatalf("manual refund failed: status=%d response=%+v", status, response)
		}
		manual := chain.operation(t, response.OperationID)
		confirmedReceiptID := manual.ReceiptID
		manual.ReceiptID = "receipt-closeout-unavailable"
		manual, err := chain.process.store.SaveWalletAdjustment(context.Background(), response.OperationID, manual)
		if err != nil {
			t.Fatal(err)
		}
		before := chain.remote.snapshot()
		_, amount, complete, err := chain.process.handler.app.refundWorkspaceLaunchCloseout(context.Background(), chain.process.handler.service, chain.purchase)
		if err == nil || complete || amount != 0 || chain.remote.snapshot() != before {
			t.Fatalf("local success replaced missing owner evidence: amount=%d complete=%v err=%v", amount, complete, err)
		}
		manual.ReceiptID = confirmedReceiptID
		if _, err := chain.process.store.SaveWalletAdjustment(context.Background(), response.OperationID, manual); err != nil {
			t.Fatal(err)
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 2 {
			t.Fatalf("restored evidence lost or duplicated the remaining refund: %+v", chain.remote.snapshot())
		}
	})
	t.Run("a closing payment reserves the original budget against a concurrent manual refund", func(t *testing.T) {
		chain := newD3CloseoutRefundChain(t, ledger)
		second := chain.openControlPlane(t, ledger)
		secondSession := reservedOperatorSessionForTest(t, second.handler)
		arrived, release := make(chan struct{}), make(chan struct{})
		chain.remote.mu.Lock()
		chain.remote.refundArrived, chain.remote.refundRelease = arrived, release
		chain.remote.mu.Unlock()
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		result := make(chan error, 1)
		go func() {
			_, _, _, err := chain.process.handler.app.refundWorkspaceLaunchCloseout(context.Background(), chain.process.handler.service, chain.purchase)
			result <- err
		}()
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("closing payment did not reach the financial boundary")
		}
		status, response, err := d1WalletHTTPRequest(second, secondSession, chain.refundPath(), chain.refund("3.00"), "concurrent-manual")
		if err != nil || status != http.StatusConflict {
			t.Fatalf("manual refund spent reserved closing budget: status=%d response=%+v err=%v", status, response, err)
		}
		close(release)
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("closing payment did not finish")
		}
		closed := finishD3RefundCloseout(t, chain)
		if closed.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || chain.remote.snapshot().refundWrites != 1 {
			t.Fatalf("manual and closing payments over-refunded the original: %+v", chain.remote.snapshot())
		}
	})
}
