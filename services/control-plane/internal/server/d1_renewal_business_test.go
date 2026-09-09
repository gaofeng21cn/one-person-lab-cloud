package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func d1RenewalDecode[T any](t *testing.T, value any) T {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded T
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func d1RenewalOperation(t *testing.T, fixture workspaceRenewalWorkerFixture) workspaceRenewalOperation {
	t.Helper()
	operation, err := decodeWorkspaceRenewalOperation(fixture.operation(t))
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func assertD1RenewalFulfilled(t *testing.T, fixture workspaceRenewalWorkerFixture) {
	t.Helper()
	operation := d1RenewalOperation(t, fixture)
	if operation.Status != "active" || operation.Phase != "complete" || !operation.EntitlementCommitted || operation.ReceiptID == "" {
		t.Fatalf("paid renewal did not fulfill its period: %#v", operation)
	}
	if len(fixture.sub2API.charges) != 1 || fixture.sub2API.charges[0].UserID != 41 || fixture.sub2API.charges[0].Code != operation.RedeemCode || fixture.sub2API.charges[0].ChargeUSDMicros != 52_580_000 {
		t.Fatalf("renewal must debit the original account and price once: %#v", fixture.sub2API.charges)
	}
	confirmation := d1RenewalDecode[clients.Sub2APICharge](t, operation.ChargeConfirmation)
	if confirmation.Code != operation.RedeemCode || confirmation.UserID != 41 || confirmation.ChargeUSDMicros != 52_580_000 || confirmation.Status != "used" {
		t.Fatalf("renewal transaction confirmation: %#v", confirmation)
	}
	if len(fixture.fabric.computeRenewKeys) != 1 || fixture.fabric.computeRenewKeys[0] != operation.ID+":compute" || len(fixture.fabric.storageRenewKeys) != 1 || fixture.fabric.storageRenewKeys[0] != operation.ID+":storage" {
		t.Fatalf("original provider resources must renew once: compute=%#v storage=%#v", fixture.fabric.computeRenewKeys, fixture.fabric.storageRenewKeys)
	}
	workspace, found := fixture.app.getWorkspace(operation.WorkspaceID)
	billing := d1RenewalDecode[workspaceBillingState](t, workspace)
	if !found || billing.PaidThrough != fixture.renewedThrough.Format(time.RFC3339Nano) || billing.PeriodStart != fixture.paidThrough.Format(time.RFC3339Nano) || billing.RenewalStatus != "active" || billing.ComputeAllocationID != operation.ComputeID || billing.StorageID != operation.StorageID {
		t.Fatalf("renewal must preserve resources and extend the original period: %#v", billing)
	}
	if len(fixture.ledger.receipts) != 1 {
		t.Fatalf("renewal receipt count=%d", len(fixture.ledger.receipts))
	}
	receipt := fixture.ledger.receipts[0]
	cost := d1RenewalDecode[workspaceBillingState](t, receipt.Cost)
	if receipt.Type != "billing.workspace_renewed.v1" || receipt.Status != "completed" || receipt.AccountID != operation.AccountID || receipt.WorkspaceID != operation.WorkspaceID || receipt.RequestID != operation.ID || cost.TotalUSDMicros != 52_580_000 || cost.PeriodStart != operation.PaidThrough || cost.PaidThrough != operation.RenewedThrough {
		t.Fatalf("receipt must bind the renewed Workspace, transaction and period: receipt=%#v cost=%#v", receipt, cost)
	}
	costFields := d1RenewalDecode[map[string]json.RawMessage](t, receipt.Cost)
	components := d1RenewalDecode[map[string]clients.Sub2APICharge](t, costFields["components"])
	if components["compute"].ChargeUSDMicros != 50_000_000 || components["storage"].ChargeUSDMicros != 2_580_000 {
		t.Fatalf("receipt lost the accepted compute/storage price components: %#v", components)
	}
	if _, present := costFields["postChargeBalanceUsdMicros"]; present && !operation.PostChargeBalanceKnown {
		t.Fatal("renewal receipt invents a wallet balance that was not observed")
	}
	if len(fixture.sub2API.refunds) != 0 {
		t.Fatalf("successful renewal unexpectedly refunded: %#v", fixture.sub2API.refunds)
	}
}

func TestD1RenewalRejectsConflictingTransactionBeforeFulfillment(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*clients.Sub2APICharge)
	}{
		{name: "another_transaction", mutate: func(charge *clients.Sub2APICharge) { charge.Code = "another-order" }},
		{name: "another_account", mutate: func(charge *clients.Sub2APICharge) { charge.UserID++ }},
		{name: "different_amount", mutate: func(charge *clients.Sub2APICharge) { charge.ChargeUSDMicros-- }},
		{name: "not_applied", mutate: func(charge *clients.Sub2APICharge) { charge.Status = "unused" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
			now := fixture.paidThrough.Add(-monthlyRenewalLead)
			expected, err := newWorkspaceRenewalOperation(fixture.workspace, now)
			if err != nil {
				t.Fatal(err)
			}
			charge := clients.Sub2APICharge{Code: expected.RedeemCode, UserID: 41, ChargeUSDMicros: expected.TotalUSDMicros, Status: "used"}
			test.mutate(&charge)
			fixture.sub2API.chargeResults = []clients.Sub2APICharge{charge}
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); err != nil {
				t.Fatal(err)
			}
			operation := d1RenewalOperation(t, fixture)
			if operation.Status != "manual_review" || operation.ErrorCode != "sub2api_charge_confirmation_invalid" || operation.EntitlementCommitted || len(fixture.sub2API.charges) != 1 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 || len(fixture.sub2API.refunds) != 0 {
				t.Fatalf("conflicting transaction advanced financial or provider effects: operation=%#v charges=%#v", operation, fixture.sub2API.charges)
			}
			restarted, err := newControlPlaneAppWithStore(fixture.app.tables)
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if len(fixture.sub2API.charges) != 1 || len(fixture.sub2API.refunds) != 0 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 {
				t.Fatal("restart retried a transaction with conflicting evidence")
			}
		})
	}
}

type d1RenewalHistoryGateway struct {
	*monthlySub2API
	historyErr error
	entry      *clients.Sub2APIBalanceHistoryEntry
	history    int
}

func (gateway *d1RenewalHistoryGateway) FinancialBalanceHistoryByCodes(_ context.Context, _ int64, codes []string) (map[string]clients.Sub2APIBalanceHistoryEntry, error) {
	gateway.history++
	if gateway.historyErr != nil {
		return nil, gateway.historyErr
	}
	entries := make(map[string]clients.Sub2APIBalanceHistoryEntry)
	if gateway.entry != nil {
		for _, code := range codes {
			entry := *gateway.entry
			entry.Code = code
			entries[code] = entry
		}
	}
	return entries, nil
}

func TestD1RenewalUnknownDebitWaitsForAuthoritativeHistoryAcrossRestart(t *testing.T) {
	fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
	fixture.sub2API.chargeErrors = []error{clients.ErrSub2APIChargeUnknown}
	gateway := &d1RenewalHistoryGateway{monthlySub2API: fixture.sub2API, historyErr: errors.New("history unavailable")}
	fixture.service = controlplane.NewService(fixture.ledger, fixture.fabric, gateway)
	now := fixture.paidThrough.Add(-monthlyRenewalLead)
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); !errors.Is(err, clients.ErrSub2APIChargeUnknown) {
		t.Fatalf("unknown debit error=%v", err)
	}
	for attempt := range 2 {
		restarted, err := newControlPlaneAppWithStore(fixture.app.tables)
		if err != nil {
			t.Fatal(err)
		}
		fixture.app = restarted
		if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Duration(attempt+1)*time.Second)); !errors.Is(err, gateway.historyErr) {
			t.Fatalf("history failure error=%v", err)
		}
		operation := d1RenewalOperation(t, fixture)
		if operation.Status != "debit_pending" || !operation.ChargeAttempted || operation.EntitlementCommitted || len(fixture.sub2API.charges) != 1 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 || len(fixture.sub2API.refunds) != 0 {
			t.Fatalf("unknown debit was repeated or fulfilled: %#v", operation)
		}
	}
	usedBy := int64(41)
	gateway.historyErr = nil
	gateway.entry = &clients.Sub2APIBalanceHistoryEntry{Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy}
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertD1RenewalFulfilled(t, fixture)
	if got := strings.Count(strings.Join(*fixture.events, ","), "sub2api.balance"); got != 1 {
		t.Fatalf("recovered debit performed another balance admission: reads=%d", got)
	}
}

func TestD1RenewalConflictingHistoryNeverRechargesOrFulfills(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*clients.Sub2APIBalanceHistoryEntry)
	}{
		{name: "account", mutate: func(entry *clients.Sub2APIBalanceHistoryEntry) { other := int64(42); entry.UsedBy = &other }},
		{name: "amount", mutate: func(entry *clients.Sub2APIBalanceHistoryEntry) { entry.ValueUSDMicros++ }},
		{name: "status", mutate: func(entry *clients.Sub2APIBalanceHistoryEntry) { entry.Status = "unused" }},
		{name: "type", mutate: func(entry *clients.Sub2APIBalanceHistoryEntry) { entry.Type = "subscription" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
			fixture.sub2API.chargeErrors = []error{clients.ErrSub2APIChargeUnknown}
			usedBy := int64(41)
			entry := clients.Sub2APIBalanceHistoryEntry{Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy}
			test.mutate(&entry)
			gateway := &d1RenewalHistoryGateway{monthlySub2API: fixture.sub2API, entry: &entry}
			fixture.service = controlplane.NewService(fixture.ledger, fixture.fabric, gateway)
			now := fixture.paidThrough.Add(-monthlyRenewalLead)
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); !errors.Is(err, clients.ErrSub2APIChargeUnknown) {
				t.Fatalf("unknown debit error=%v", err)
			}
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			operation := d1RenewalOperation(t, fixture)
			if operation.Status != "manual_review" || operation.ErrorCode != "sub2api_charge_mismatch" || len(fixture.sub2API.charges) != 1 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 || len(fixture.sub2API.refunds) != 0 {
				t.Fatalf("conflicting history was used to fulfill or mutate money: %#v", operation)
			}
		})
	}
}

func TestD1RenewalPersistedConfirmationIsRevalidatedBeforeProviderRenewal(t *testing.T) {
	fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
	persistErr := errors.New("stopped after confirmed debit")
	store := &failingWorkspaceRenewalPersistStore{
		memoryTableStore: fixture.app.tables.(*memoryTableStore), err: persistErr,
		fail: func(operation workspaceRenewalOperation) bool { return operation.Status == "debited" },
	}
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = app
	now := fixture.paidThrough.Add(-monthlyRenewalLead)
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); !errors.Is(err, persistErr) {
		t.Fatalf("persistence failure=%v", err)
	}
	operation := d1RenewalOperation(t, fixture)
	// This is the persisted untrusted JSON boundary: retain the original intent,
	// but emulate a conflicting stored confirmation returned after restart.
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"code":%q,"userId":42,"chargeUsdMicros":52580000,"status":"used"}`, operation.RedeemCode)), &operation.ChargeConfirmation); err != nil {
		t.Fatal(err)
	}
	releaseWorkspaceRenewalLease(&operation)
	mustStore(t, store.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(operation)))
	restarted, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = restarted
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(workspaceRenewalLeaseDuration+time.Second)); err != nil {
		t.Fatal(err)
	}
	operation = d1RenewalOperation(t, fixture)
	if operation.Status != "manual_review" || operation.ErrorCode != "sub2api_charge_confirmation_invalid" || len(fixture.sub2API.charges) != 1 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("persisted conflicting confirmation was used to renew: %#v", operation)
	}
}

func TestD1RenewalRefundRequiresTheOriginalConfirmedDebit(t *testing.T) {
	for _, test := range []struct {
		name         string
		confirmation func(workspaceRenewalOperation) string
	}{
		{name: "missing", confirmation: func(workspaceRenewalOperation) string { return "null" }},
		{name: "another_account", confirmation: func(operation workspaceRenewalOperation) string {
			return fmt.Sprintf(`{"code":%q,"userId":42,"chargeUsdMicros":52580000,"status":"used"}`, operation.RedeemCode)
		}},
		{name: "another_order", confirmation: func(workspaceRenewalOperation) string {
			return `{"code":"another-order","userId":41,"chargeUsdMicros":52580000,"status":"used"}`
		}},
		{name: "different_amount", confirmation: func(operation workspaceRenewalOperation) string {
			return fmt.Sprintf(`{"code":%q,"userId":41,"chargeUsdMicros":1000000,"status":"used"}`, operation.RedeemCode)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
			fixture.fabric.computeRenewErr = errors.New("provider response lost")
			fixture.fabric.computeSync = clients.ComputeAllocation{ID: "compute-workspace-monthly", AccountID: "acct-monthly", WorkspaceID: "workspace-monthly", Status: "renewing"}
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, fixture.paidThrough.Add(-monthlyRenewalLead)); err != nil {
				t.Fatal(err)
			}
			operation := d1RenewalOperation(t, fixture)
			if operation.Status != "manual_review" || len(fixture.sub2API.charges) != 1 {
				t.Fatalf("expected a charged renewal awaiting provider evidence: %#v", operation)
			}
			// The refund owner must reject an absent or conflicting persisted
			// original transaction before it writes a refund attempt or money.
			if err := json.Unmarshal([]byte(test.confirmation(operation)), &operation.ChargeConfirmation); err != nil {
				t.Fatal(err)
			}
			if err := fixture.app.refundWorkspaceRenewal(context.Background(), fixture.service, &operation, "fabric_compute_confirmed_absent"); err != nil {
				t.Fatal(err)
			}
			if operation.Status != "manual_review" || operation.ErrorCode != "sub2api_charge_confirmation_invalid" || operation.RefundAttempted || len(fixture.sub2API.refunds) != 0 || len(fixture.ledger.receipts) != 0 {
				t.Fatalf("refund accepted an unconfirmed or conflicting original transaction: %#v", operation)
			}
		})
	}
}

func TestD1RenewalHTTPTransactionRecoversAcrossPostgresReopen(t *testing.T) {
	ctx := context.Background()
	fixture := newWorkspaceRenewalWorkerFixture(t, nil)
	store, database := newPostgresWorkspaceRenewalStoreWithDB(t)
	seedTenantMember(t, store, "acct-monthly", "org-monthly", "usr-monthly-owner", "monthly-owner@example.com")
	mustStore(t, store.SaveCompute(ctx, fixture.compute))
	mustStore(t, store.SaveStorage(ctx, fixture.storage))
	mustStore(t, store.SaveWorkspace(ctx, fixture.workspace))
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = app
	gateway := newAuthoritativeReplaySub2API(t, authoritativeReplayConfig{
		chargeValue: "-52.580000", initialBalance: json.RawMessage("100"), adjustedBalance: json.RawMessage("0"), loseFirstResponse: true,
	})
	fixture.service = controlplane.NewService(fixture.ledger, fixture.fabric, gateway.client)
	now := fixture.paidThrough.Add(-monthlyRenewalLead)
	if err := fixture.app.runMonthlyBillingOnce(ctx, fixture.service, now); !errors.Is(err, clients.ErrSub2APIChargeUnknown) {
		t.Fatalf("lost HTTP debit response error=%v", err)
	}
	pending := d1RenewalOperation(t, fixture)
	if !pending.ChargeAttempted || pending.Status != "debit_pending" || len(gateway.codes) != 1 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("uncertain debit must persist before fulfillment: %#v", pending)
	}
	var schema string
	if err := database.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if err := store.client.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newTestPostgresEntStateStore(controlPlaneTestPostgresURL(t, "postgres", schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.(*postgresEntStateStore).client.Close() })
	restarted, err := newControlPlaneAppWithStore(reopened)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = restarted
	if err := fixture.app.runMonthlyBillingOnce(ctx, fixture.service, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	completed := d1RenewalOperation(t, fixture)
	if completed.ID != pending.ID || completed.RedeemCode != pending.RedeemCode || completed.Status != "active" || !completed.EntitlementCommitted || completed.ReceiptID == "" || len(gateway.codes) != 1 || gateway.historyCalls != 1 || len(fixture.fabric.computeRenewKeys) != 1 || len(fixture.fabric.storageRenewKeys) != 1 || len(fixture.ledger.receipts) != 1 {
		t.Fatalf("original HTTP transaction did not fulfill exactly once after PostgreSQL reopen: operation=%#v codes=%#v history=%d receipts=%#v", completed, gateway.codes, gateway.historyCalls, fixture.ledger.receipts)
	}
	confirmation := d1RenewalDecode[clients.Sub2APICharge](t, completed.ChargeConfirmation)
	if confirmation.Code != pending.RedeemCode || confirmation.UserID != 41 || confirmation.ChargeUSDMicros != 52_580_000 || confirmation.Status != "used" {
		t.Fatalf("restored confirmation lost original transaction identity: %#v", confirmation)
	}
	receipt := fixture.ledger.receipts[0]
	cost := d1RenewalDecode[workspaceBillingState](t, receipt.Cost)
	if receipt.RequestID != pending.ID || receipt.AccountID != pending.AccountID || receipt.WorkspaceID != pending.WorkspaceID || cost.TotalUSDMicros != pending.TotalUSDMicros || cost.PeriodStart != pending.PaidThrough || cost.PaidThrough != pending.RenewedThrough {
		t.Fatalf("recovered receipt lost transaction or period binding: receipt=%#v cost=%#v", receipt, cost)
	}
	before := len(*fixture.events)
	if err := fixture.app.runMonthlyBillingOnce(ctx, fixture.service, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(gateway.codes) != 1 || gateway.historyCalls != 1 || len(*fixture.events) != before || len(fixture.ledger.receipts) != 1 {
		t.Fatal("completed PostgreSQL renewal repeated an external effect")
	}
}

type d1RenewalConcurrentWalletGateway struct {
	*monthlySub2API
	balance                   int64
	otherTransactionUSDMicros int64
	otherTransactions         int
}

func (gateway *d1RenewalConcurrentWalletGateway) Balance(_ context.Context, userID int64) (clients.Sub2APIBalance, error) {
	*gateway.events = append(*gateway.events, "sub2api.balance")
	return clients.Sub2APIBalance{UserID: userID, USDMicros: gateway.balance}, nil
}

func (gateway *d1RenewalConcurrentWalletGateway) Charge(ctx context.Context, input clients.Sub2APIChargeInput) (clients.Sub2APICharge, error) {
	charge, err := gateway.monthlySub2API.Charge(ctx, input)
	if err != nil {
		return charge, err
	}
	gateway.balance -= charge.ChargeUSDMicros
	// A different accepted wallet transaction settles before Control Plane
	// receives this debit's confirmation. Its balance change is independent.
	gateway.balance += gateway.otherTransactionUSDMicros
	gateway.otherTransactions++
	return charge, nil
}

func TestD1RenewalPersistedPaidPhaseWithoutTransactionCannotFulfill(t *testing.T) {
	for _, test := range []struct {
		status string
		phase  string
	}{
		{status: "debited", phase: "provider_compute"},
		{status: "provider_renewing", phase: "provider_compute"},
		{status: "verifying", phase: "entitlement"},
	} {
		t.Run(test.status, func(t *testing.T) {
			fixture := newWorkspaceRenewalWorkerFixture(t, nil)
			now := fixture.paidThrough.Add(-monthlyRenewalLead)
			operation, err := newWorkspaceRenewalOperation(fixture.workspace, now)
			if err != nil {
				t.Fatal(err)
			}
			operation.Status, operation.Phase, operation.ChargeAttempted = test.status, test.phase, true
			mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(operation)))
			restarted, err := newControlPlaneAppWithStore(fixture.app.tables)
			if err != nil {
				t.Fatal(err)
			}
			fixture.app = restarted
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); err != nil {
				t.Fatal(err)
			}
			operation = d1RenewalOperation(t, fixture)
			if operation.Status != "manual_review" || operation.ErrorCode != "sub2api_charge_confirmation_invalid" || operation.EntitlementCommitted || len(fixture.sub2API.charges) != 0 || len(fixture.sub2API.refunds) != 0 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 {
				t.Fatalf("persisted phase label replaced original transaction proof: %#v", operation)
			}
		})
	}
}

func TestD1RenewalReconciliationUsesTransactionAndReceiptWithoutWalletBalanceProof(t *testing.T) {
	fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, fixture.paidThrough.Add(-monthlyRenewalLead)); err != nil {
		t.Fatal(err)
	}
	operation := d1RenewalOperation(t, fixture)
	compute, _ := fixture.app.getCompute(operation.ComputeID)
	storage, _ := fixture.app.getStorage(operation.StorageID)
	usedBy := int64(41)
	history := []clients.Sub2APIBalanceHistoryEntry{{
		Code: operation.RedeemCode, Type: "balance", ValueUSDMicros: -operation.TotalUSDMicros, Status: "used", UsedBy: &usedBy,
		UsedAt: &fixture.paidThrough, CreatedAt: fixture.paidThrough.Add(-time.Minute),
	}}
	remote := &customerFactsSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000, charges: map[string]int64{}},
		history:           map[int64][]clients.Sub2APIBalanceHistoryEntry{41: history},
	}
	facts := []clients.ProviderFact{reconciliationProviderFact("compute", compute), reconciliationProviderFact("storage", storage)}
	for index := range facts {
		facts[index].Facts.ExpiresAt = operation.RenewedThrough
	}
	calls := &[]string{}
	fabric := &customerFactsFabric{fakeFabricClient: fakeFabricClient{calls: calls}, facts: providerFactsByKey(facts)}
	for _, test := range []struct {
		name           string
		observed       bool
		localBalance   int64
		receiptBalance json.RawMessage
	}{
		{name: "balance_not_observed"},
		{name: "different_wallet_observation_times", observed: true, localBalance: 47_420_000, receiptBalance: json.RawMessage("60000000")},
		{name: "legacy_observation_with_new_receipt", observed: true, localBalance: 47_420_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation.PostChargeBalanceKnown, operation.PostChargeBalanceUSDMicros = test.observed, test.localBalance
			mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(operation)))
			receipt := clients.Receipt{ReceiptInput: fixture.ledger.receipts[0], ReceiptID: operation.ReceiptID, CreatedAt: fixture.paidThrough.Format(time.RFC3339)}
			costFields := d1RenewalDecode[map[string]json.RawMessage](t, receipt.Cost)
			if test.receiptBalance != nil {
				costFields["postChargeBalanceUsdMicros"] = test.receiptBalance
			}
			receipt.Cost = nil
			if err := json.Unmarshal(mustJSON(costFields), &receipt.Cost); err != nil {
				t.Fatal(err)
			}
			ledger := &customerFactsLedger{page: clients.ReceiptPage{Receipts: []clients.Receipt{receipt}}}
			service := controlplane.NewService(ledger, fabric, remote)
			report, err := fixture.app.billingReconciliationReport(context.Background(), service, "d1-renewal-reconcile-"+test.name)
			if err != nil {
				t.Fatal(err)
			}
			fields := d1RenewalDecode[map[string]json.RawMessage](t, report)
			status := d1RenewalDecode[string](t, fields["status"])
			counts := d1RenewalDecode[map[string]int](t, fields["counts"])
			exceptions := d1RenewalDecode[[]json.RawMessage](t, fields["exceptions"])
			if status != "ok" || counts["billingOperations"] != 1 || counts["matched"] != 1 || counts["exceptions"] != 0 || len(exceptions) != 0 {
				t.Fatalf("wallet observation overruled matching transaction and receipt: %s", mustJSON(report))
			}
			if len(remote.charges) != 0 || ledger.receiptWrites != 0 {
				t.Fatal("reconciliation must only read financial evidence")
			}
		})
	}
}

type d1RenewalRefundSourceGateway struct {
	*monthlySub2API
	lookup *clients.Sub2APIHTTPClient
}

func (gateway *d1RenewalRefundSourceGateway) FinancialBalanceHistoryByCodes(ctx context.Context, userID int64, codes []string) (map[string]clients.Sub2APIBalanceHistoryEntry, error) {
	return gateway.lookup.FinancialBalanceHistoryByCodes(ctx, userID, codes)
}

func TestD1RenewalRefundVerifiesOriginalAppliedDebitThroughHTTP(t *testing.T) {
	for _, test := range []struct {
		name                string
		mutate              func(map[string]any)
		absent, unavailable bool
		wantStatus          string
	}{
		{name: "confirmed_original", wantStatus: "refunded"},
		{name: "original_absent", absent: true, wantStatus: "manual_review"},
		{name: "another_transaction", mutate: func(entry map[string]any) { entry["code"] = "another-order" }, wantStatus: "manual_review"},
		{name: "another_account", mutate: func(entry map[string]any) { entry["used_by"] = 42 }, wantStatus: "manual_review"},
		{name: "different_amount", mutate: func(entry map[string]any) {
			entry["value"], entry["balance_applied_value"] = json.RawMessage("-50"), json.RawMessage("-50")
		}, wantStatus: "manual_review"},
		{name: "clamped_original", mutate: func(entry map[string]any) { entry["balance_applied_value"] = json.RawMessage("-40") }, wantStatus: "manual_review"},
		{name: "historical_applied_missing", mutate: func(entry map[string]any) { delete(entry, "balance_applied_value") }, wantStatus: "refund_pending"},
		{name: "historical_applied_null", mutate: func(entry map[string]any) { entry["balance_applied_value"] = nil }, wantStatus: "refund_pending"},
		{name: "lookup_unavailable", unavailable: true, wantStatus: "refund_pending"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
			fixture.fabric.computeRenewErr = errors.New("provider response lost")
			fixture.fabric.computeSync = clients.ComputeAllocation{ID: "compute-workspace-monthly", AccountID: "acct-monthly", WorkspaceID: "workspace-monthly", Status: "renewing"}
			now := fixture.paidThrough.Add(-monthlyRenewalLead)
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); err != nil {
				t.Fatal(err)
			}
			operation := d1RenewalOperation(t, fixture)
			if operation.Status != "manual_review" || !monthlyChargeConfirmationMatches(operation.ChargeConfirmation, operation.RedeemCode, 41, operation.TotalUSDMicros) {
				t.Fatalf("expected a locally confirmed original charge: %#v", operation)
			}
			resolved := false
			lookups := []string{}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				success := func(data any) {
					if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}); err != nil {
						t.Error(err)
					}
				}
				if r.URL.Path == "/api/v1/auth/login" {
					success(map[string]any{"access_token": "access", "refresh_token": "refresh"})
					return
				}
				code := r.URL.Query().Get("code")
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/redeem-codes/by-code" || r.URL.Query().Get("user_id") != "41" || code != operation.RedeemCode && code != operation.RefundCode {
					t.Errorf("unexpected financial request: %s %s", r.Method, r.URL.String())
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				lookups = append(lookups, code)
				if test.unavailable && !resolved {
					http.Error(w, "lookup unavailable", http.StatusServiceUnavailable)
					return
				}
				var record any
				if code == operation.RedeemCode && (resolved || !test.absent) {
					entry := authoritativeHistoryEntry(code, "-52.580000")
					if test.mutate != nil && !resolved {
						test.mutate(entry)
					}
					record = entry
				}
				if code == operation.RefundCode && len(fixture.sub2API.refunds) > 0 {
					record = authoritativeHistoryEntry(code, "52.580000")
				}
				success(map[string]any{"lookup": "exact_code_v1", "redeem_code": record})
			}))
			t.Cleanup(upstream.Close)
			client, err := clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{BaseURL: upstream.URL, AdminEmail: "admin@example.test", AdminPassword: "test-password", Timeout: time.Second}, upstream.Client())
			if err != nil {
				t.Fatal(err)
			}
			fixture.service = controlplane.NewService(fixture.ledger, fixture.fabric, &d1RenewalRefundSourceGateway{monthlySub2API: fixture.sub2API, lookup: client})
			err = fixture.app.refundWorkspaceRenewal(context.Background(), fixture.service, &operation, "fabric_compute_confirmed_absent")
			if operation.Status != test.wantStatus || len(lookups) != 1 || lookups[0] != operation.RedeemCode {
				t.Fatalf("original applied debit was not verified: err=%v operation=%#v lookups=%q", err, operation, lookups)
			}
			if test.wantStatus == "refunded" {
				if err != nil || operation.Phase != "complete" || len(fixture.sub2API.refunds) != 1 || fixture.sub2API.refunds[0].Code != operation.RefundCode || fixture.sub2API.refunds[0].RefundUSDMicros != operation.TotalUSDMicros || len(fixture.ledger.receipts) != 1 {
					t.Fatalf("confirmed original did not refund once: err=%v operation=%#v refunds=%#v receipts=%#v", err, operation, fixture.sub2API.refunds, fixture.ledger.receipts)
				}
				return
			}
			if len(fixture.sub2API.refunds) != 0 || len(fixture.ledger.receipts) != 0 {
				t.Fatalf("unverified original produced money or receipt: refunds=%#v receipts=%#v", fixture.sub2API.refunds, fixture.ledger.receipts)
			}
			if test.wantStatus == "manual_review" {
				if err != nil || operation.ErrorCode != "sub2api_refund_source_mismatch" {
					t.Fatalf("source conflict was not retained: err=%v operation=%#v", err, operation)
				}
				return
			}
			if err == nil || operation.ErrorCode != "sub2api_refund_source_unavailable" {
				t.Fatalf("unknown source was mistaken for absent: err=%v operation=%#v", err, operation)
			}
			resolved = true
			restarted, err := newControlPlaneAppWithStore(fixture.app.tables)
			if err != nil {
				t.Fatal(err)
			}
			fixture.app = restarted
			if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(workspaceRenewalLeaseDuration+time.Second)); err != nil {
				t.Fatal(err)
			}
			completed := d1RenewalOperation(t, fixture)
			if completed.Status != "refunded" || completed.Phase != "complete" || completed.RefundCode != operation.RefundCode || len(fixture.sub2API.refunds) != 1 || len(fixture.sub2API.charges) != 1 || len(fixture.ledger.receipts) != 1 {
				t.Fatalf("verified source did not resume original refund once: operation=%#v refunds=%#v", completed, fixture.sub2API.refunds)
			}
		})
	}
}

func TestD1RenewalResumedFulfillmentRequiresAppliedDebit(t *testing.T) {
	fixture := newWorkspaceRenewalWorkerFixture(t, []int64{100_000_000})
	store := &recordingWorkspaceRenewalStore{memoryTableStore: fixture.app.tables.(*memoryTableStore)}
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = app
	now := fixture.paidThrough.Add(-monthlyRenewalLead)
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range store.snapshots {
		operation, err := decodeWorkspaceRenewalOperation(snapshot.operation)
		if err != nil {
			t.Fatal(err)
		}
		if operation.ChargeConfirmation == nil || operation.Status != "debit_pending" && operation.Status != "debited" && operation.Status != "provider_renewing" && operation.Status != "verifying" {
			continue
		}
		for _, applied := range []string{"null", "-40"} {
			t.Run(operation.Status+"/"+operation.Phase+"/applied_"+applied, func(t *testing.T) {
				replay := newWorkspaceRenewalWorkerFixture(t, nil)
				op := operation
				op.LeaseToken, op.LeaseExpiresAt = "", ""
				replayStore := replay.app.tables.(*memoryTableStore)
				replayStore.mu.Lock()
				replayStore.workspaces[op.WorkspaceID] = cloneMap(snapshot.workspace)
				replayStore.computes[op.ComputeID] = cloneMap(snapshot.compute)
				replayStore.storages[op.StorageID] = cloneMap(snapshot.storage)
				replayStore.runtimeOps = []map[string]any{workspaceRenewalOperationRow(op)}
				replayStore.mu.Unlock()
				lookupCount := 0
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					var data any
					if r.URL.Path == "/api/v1/auth/login" {
						data = map[string]any{"access_token": "access", "refresh_token": "refresh"}
					} else if r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/redeem-codes/by-code" && r.URL.Query().Get("user_id") == "41" && r.URL.Query().Get("code") == op.RedeemCode {
						lookupCount++
						entry := authoritativeHistoryEntry(op.RedeemCode, "-52.580000")
						entry["balance_applied_value"] = json.RawMessage(applied)
						data = map[string]any{"lookup": "exact_code_v1", "redeem_code": entry}
					} else {
						t.Errorf("unexpected money request during persisted recovery: %s %s", r.Method, r.URL.String())
						http.Error(w, "unexpected request", http.StatusBadRequest)
						return
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}); err != nil {
						t.Error(err)
					}
				}))
				t.Cleanup(upstream.Close)
				client, err := clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{BaseURL: upstream.URL, AdminEmail: "admin@example.test", AdminPassword: "test-password", Timeout: time.Second}, upstream.Client())
				if err != nil {
					t.Fatal(err)
				}
				replay.service = controlplane.NewService(replay.ledger, replay.fabric, &d1RenewalRefundSourceGateway{monthlySub2API: replay.sub2API, lookup: client})
				err = replay.app.runMonthlyBillingOnce(context.Background(), replay.service, now)
				current := d1RenewalOperation(t, replay)
				if applied == "null" {
					if !errors.Is(err, clients.ErrSub2APIChargeUnknown) || current.Status != op.Status || current.ErrorCode != "sub2api_charge_history_unavailable" {
						t.Fatalf("legacy unverified debit must remain unresolved: err=%v current=%#v", err, current)
					}
				} else if err != nil || current.Status != "manual_review" || current.ErrorCode != "sub2api_charge_mismatch" {
					t.Fatalf("clamped original must require review: err=%v current=%#v", err, current)
				}
				if lookupCount != 1 || len(replay.sub2API.charges) != 0 || len(replay.sub2API.refunds) != 0 || len(replay.fabric.computeRenewKeys) != 0 || len(replay.fabric.storageRenewKeys) != 0 || len(replay.ledger.receipts) != 0 {
					t.Fatalf("unverified recovered debit caused new effects: lookups=%d charges=%#v refunds=%#v compute=%#v storage=%#v receipts=%#v", lookupCount, replay.sub2API.charges, replay.sub2API.refunds, replay.fabric.computeRenewKeys, replay.fabric.storageRenewKeys, replay.ledger.receipts)
				}
				workspace, _ := replay.app.getWorkspace(op.WorkspaceID)
				if workspace["paidThrough"] != snapshot.workspace["paidThrough"] {
					t.Fatal("unverified recovered debit extended customer entitlement")
				}
			})
		}
	}
}
