package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestD1RefundReservationPostgres(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	databaseURL := gatewayAccountingDatabase(t, "opl_d1_refund_")
	var stores [2]*postgresEntStateStore
	for i := range stores {
		state, err := newTestPostgresEntStateStore(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = state.(*postgresEntStateStore)
		t.Cleanup(func() { _ = state.(*postgresEntStateStore).client.Close() })
	}

	var nextRemoteUserID int64 = 100
	newFixture := func(t *testing.T, action string) d1RefundReservationFixture {
		t.Helper()
		nextRemoteUserID++
		return newD1RefundReservationFixture(t, stores, action, nextRemoteUserID)
	}

	t.Run("twenty operators cannot reserve more than the original five dollars", func(t *testing.T) {
		fixture := newFixture(t, "workspace.launch.v2")
		start := make(chan struct{})
		results := make(chan error, 20)
		var ready sync.WaitGroup
		ready.Add(20)
		for i := range 20 {
			go func(i int) {
				id := fmt.Sprintf("wallet-adjustment-concurrent-%02d", i)
				operation := fixture.refund(id, 1_000_000)
				ready.Done()
				<-start
				_, err := fixture.stores[i%2].SaveWalletAdjustment(context.Background(), id, operation)
				results <- err
			}(i)
		}
		ready.Wait()
		close(start)
		accepted, rejected := 0, 0
		for range 20 {
			switch err := <-results; {
			case err == nil:
				accepted++
			case errors.Is(err, errWalletAdjustmentConflict):
				rejected++
			default:
				t.Fatalf("concurrent refund reservation returned a storage failure: %v", err)
			}
		}
		if accepted != 5 || rejected != 15 {
			t.Fatalf("five-dollar original accepted=%d rejected=%d, want 5 and 15", accepted, rejected)
		}
		operations := fixture.refunds(t)
		var reserved int64
		for _, operation := range operations {
			if operation.Status != "pending" || operation.AdjustmentAttempted || operation.RelatedOperationID != fixture.originalID {
				t.Fatalf("reservation altered payment state or original purchase: %+v", operation)
			}
			reserved += operation.AmountUSDMicros
		}
		if len(operations) != 5 || reserved != 5_000_000 {
			t.Fatalf("durable reservations count=%d amount=%d, want 5 and 5000000", len(operations), reserved)
		}
	})

	for _, scenario := range []struct {
		name      string
		status    string
		attempted bool
		releases  bool
	}{
		{name: "pending refund keeps its three-dollar reservation", status: "pending"},
		{name: "unknown refund keeps its three-dollar reservation", status: "manual_review", attempted: true},
		{name: "failed after dispatch keeps its unresolved three dollars", status: "failed", attempted: true},
		{name: "failure before dispatch releases three dollars", status: "failed", releases: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newFixture(t, "workspace.launch.v2")
			firstID := "wallet-adjustment-existing-" + stableID(t.Name())[:12]
			first, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), firstID, fixture.refund(firstID, 3_000_000))
			if err != nil {
				t.Fatal(err)
			}
			first.Status, first.AdjustmentAttempted = scenario.status, scenario.attempted
			if scenario.attempted {
				first.Phase = "authoritative_readback"
			} else if scenario.status == "failed" {
				first.Phase = "complete"
			}
			if _, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), firstID, first); err != nil {
				t.Fatal(err)
			}
			nextID := "wallet-adjustment-next-" + stableID(t.Name())[:12]
			_, err = fixture.stores[1].SaveWalletAdjustment(context.Background(), nextID, fixture.refund(nextID, 3_000_000))
			if scenario.releases {
				if err != nil || len(fixture.refunds(t)) != 2 {
					t.Fatalf("undispatched rejected refund did not release capacity: %v", err)
				}
			} else if !errors.Is(err, errWalletAdjustmentConflict) || len(fixture.refunds(t)) != 1 {
				t.Fatalf("unresolved refund released capacity: error=%v", err)
			}
		})
	}

	t.Run("an old refund to another account still consumes the original budget", func(t *testing.T) {
		fixture := newFixture(t, "workspace.launch.v2")
		seedOperatorProjectionAccount(t, fixture.stores[0], "acct-d1-historical", "usr-d1-historical", "historical-d1@example.test", 42)
		legacyID := "wallet-adjustment-historical-cross-account"
		legacy := fixture.refund(legacyID, 3_000_000)
		legacy.AccountID, legacy.Sub2APIUserID = "acct-d1-historical", 42
		legacy.Status, legacy.Phase, legacy.AdjustmentAttempted = "succeeded", "complete", true
		// A pre-upgrade bad payment is a retained obligation. Import through the
		// actual generic persistence boundary; the new reservation API must never
		// permit creating such a payment itself.
		if err := fixture.stores[0].SaveRuntimeOperation(context.Background(), walletAdjustmentRow(legacyID, legacy)); err != nil {
			t.Fatal(err)
		}
		overID := "wallet-adjustment-after-historical-over"
		if _, err := fixture.stores[1].SaveWalletAdjustment(context.Background(), overID, fixture.refund(overID, 3_000_000)); !errors.Is(err, errWalletAdjustmentConflict) {
			t.Fatalf("historical cross-account payment was ignored: %v", err)
		}
		remainderID := "wallet-adjustment-after-historical-remainder"
		if _, err := fixture.stores[1].SaveWalletAdjustment(context.Background(), remainderID, fixture.refund(remainderID, 2_000_000)); err != nil {
			t.Fatalf("remaining two dollars cannot be refunded to the original account: %v", err)
		}
	})

	t.Run("only one process can claim a payment from the same persisted snapshot", func(t *testing.T) {
		fixture := newFixture(t, "workspace.launch.v2")
		id := "wallet-adjustment-claim-once"
		if _, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), id, fixture.refund(id, 1_000_000)); err != nil {
			t.Fatal(err)
		}
		first := fixture.read(t, 0, id)
		second := fixture.read(t, 1, id)
		if first.PersistedResult != second.PersistedResult || first.PersistedStatus != second.PersistedStatus {
			t.Fatal("two processes did not begin at the same durable snapshot")
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		for i, operation := range []walletAdjustmentOperation{first, second} {
			go func(i int, operation walletAdjustmentOperation) {
				operation.AdjustmentAttempted, operation.Phase = true, "authoritative_readback"
				<-start
				_, err := fixture.stores[i].SaveWalletAdjustment(context.Background(), id, operation)
				results <- err
			}(i, operation)
		}
		close(start)
		accepted, conflicts := 0, 0
		for range 2 {
			switch err := <-results; {
			case err == nil:
				accepted++
			case errors.Is(err, errWalletAdjustmentConflict):
				conflicts++
			default:
				t.Fatalf("claim returned a storage failure: %v", err)
			}
		}
		if accepted != 1 || conflicts != 1 {
			t.Fatalf("payment dispatch claims accepted=%d conflicts=%d", accepted, conflicts)
		}
		current := fixture.read(t, 1, id)
		if !current.AdjustmentAttempted || current.Phase != "authoritative_readback" || len(fixture.refunds(t)) != 1 {
			t.Fatalf("claimed payment lost its durable readback obligation: %+v", current)
		}
	})

	t.Run("a status-only change prevents a stale process from dispatching", func(t *testing.T) {
		fixture := newFixture(t, "workspace.launch.v2")
		id := "wallet-adjustment-status-changed"
		if _, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), id, fixture.refund(id, 1_000_000)); err != nil {
			t.Fatal(err)
		}
		stale := fixture.read(t, 0, id)
		// Simulate a retained writer that updates only the separately stored
		// status column. Identical JSON must not authorize a stale payment.
		if err := fixture.stores[1].client.RuntimeOperation.UpdateOneID(id).SetStatus("manual_review").Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		stale.AdjustmentAttempted, stale.Phase = true, "authoritative_readback"
		if _, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), id, stale); !errors.Is(err, errWalletAdjustmentConflict) {
			t.Fatalf("status-only change did not stop stale payment dispatch: %v", err)
		}
		current := fixture.read(t, 1, id)
		if current.Status != "manual_review" || current.AdjustmentAttempted {
			t.Fatalf("stale writer overrode manual review: %+v", current)
		}
	})

	t.Run("old unreserved refunds must recheck the budget before payment", func(t *testing.T) {
		fixture := newFixture(t, "workspace.launch.v2")
		for _, id := range []string{"wallet-adjustment-old-a", "wallet-adjustment-old-b"} {
			if err := fixture.stores[0].SaveRuntimeOperation(context.Background(), walletAdjustmentRow(id, fixture.refund(id, 3_000_000))); err != nil {
				t.Fatal(err)
			}
		}
		for i, id := range []string{"wallet-adjustment-old-a", "wallet-adjustment-old-b"} {
			operation := fixture.read(t, i, id)
			operation.AdjustmentAttempted, operation.Phase = true, "authoritative_readback"
			if _, err := fixture.stores[i].SaveWalletAdjustment(context.Background(), id, operation); !errors.Is(err, errWalletAdjustmentConflict) {
				t.Fatalf("pre-upgrade over-reservation permitted payment: %v", err)
			}
			if current := fixture.read(t, i, id); current.AdjustmentAttempted {
				t.Fatalf("rejected payment claim changed the durable obligation: %+v", current)
			}
		}
	})

	t.Run("the original transaction remains eligible when payment is claimed", func(t *testing.T) {
		fixture := newFixture(t, "workspace.launch.v2")
		id := "wallet-adjustment-original-changed"
		operation, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), id, fixture.refund(id, 1_000_000))
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.stores[1].client.RuntimeOperation.UpdateOneID(fixture.originalID).SetStatus("refunded").Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		operation.AdjustmentAttempted, operation.Phase = true, "authoritative_readback"
		if _, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), id, operation); !errors.Is(err, errWalletAdjustmentConflict) {
			t.Fatalf("previous reservation bypassed current original transaction eligibility: %v", err)
		}
		if current := fixture.read(t, 1, id); current.AdjustmentAttempted {
			t.Fatalf("original transaction rejection was persisted as a payment claim: %+v", current)
		}
	})

	t.Run("a completed paid renewal is an eligible original transaction", func(t *testing.T) {
		fixture := newFixture(t, "workspace.renewal")
		id := "wallet-adjustment-renewal-refund"
		operation, err := fixture.stores[0].SaveWalletAdjustment(context.Background(), id, fixture.refund(id, 5_000_000))
		if err != nil || operation.RelatedOperationID != fixture.originalID || operation.AmountUSDMicros != 5_000_000 {
			t.Fatalf("paid renewal refund reservation=%+v error=%v", operation, err)
		}
		overID := "wallet-adjustment-renewal-over"
		if _, err := fixture.stores[1].SaveWalletAdjustment(context.Background(), overID, fixture.refund(overID, 1)); !errors.Is(err, errWalletAdjustmentConflict) {
			t.Fatalf("renewal refund exceeded original paid amount: %v", err)
		}
	})
}

type d1RefundReservationFixture struct {
	stores       [2]*postgresEntStateStore
	originalID   string
	accountID    string
	remoteUserID int64
}

func newD1RefundReservationFixture(t *testing.T, stores [2]*postgresEntStateStore, action string, remoteUserID int64) d1RefundReservationFixture {
	t.Helper()
	suffix := stableID(t.Name())[:12]
	fixture := d1RefundReservationFixture{stores: stores, originalID: "workspace-original-d1-" + suffix, accountID: "acct-d1-reservation-" + suffix, remoteUserID: remoteUserID}
	seedOperatorProjectionAccount(t, fixture.stores[0], fixture.accountID, "usr-d1-reservation-"+suffix, "reservation-d1-"+suffix+"@example.test", remoteUserID)
	facts := walletRefundChargeFacts{
		AccountID: fixture.accountID, Sub2APIUserID: remoteUserID, RedeemCode: monthlyRedeemCode("local", fixture.originalID),
		ChargeConfirmation: &walletRefundCharge{Code: monthlyRedeemCode("local", fixture.originalID), UserID: remoteUserID, AmountUSDMicros: 5_000_000, Status: "used"},
	}
	status, resourceKind := "succeeded", "workspace_launch"
	if action == "workspace.renewal" {
		facts.TotalUSDMicros, facts.Phase = 5_000_000, "complete"
		status, resourceKind = "active", "workspace"
	} else {
		facts.TotalChargeUSDMicros = 5_000_000
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.stores[0].client.RuntimeOperation.Create().
		SetID(fixture.originalID).SetOperationID(fixture.originalID).SetAccountID(fixture.accountID).
		SetWorkspaceID("ws-d1-" + suffix).SetResourceID("ws-d1-" + suffix).SetResourceKind(resourceKind).
		SetAction(action).SetStatus(status).SetResult(string(encoded)).SetCreatedAt(time.Now().UTC()).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f d1RefundReservationFixture) refund(id string, amount int64) walletAdjustmentOperation {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return walletAdjustmentOperation{
		RequestHash: stableID(id, f.accountID, f.originalID, formatWalletUSD(amount)), Phase: "before_balance", Status: "pending",
		AccountID: f.accountID, Sub2APIUserID: f.remoteUserID, Kind: "business_refund", AmountUSDMicros: amount,
		AmountUSD: formatWalletUSD(amount), Reason: "original service settlement correction", RelatedOperationID: f.originalID,
		ActorUserID: "usr-d1-operator", CanonicalRedeemCode: walletAdjustmentRedeemCode(id), RedeemCodeVersion: "v2",
		CreatedAt: now, UpdatedAt: now,
	}
}

func (f d1RefundReservationFixture) read(t *testing.T, store int, id string) walletAdjustmentOperation {
	t.Helper()
	row, found, err := f.stores[store].GetRuntimeOperation(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("refund readback found=%t error=%v", found, err)
	}
	operation, err := decodeWalletAdjustment(row)
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func (f d1RefundReservationFixture) refunds(t *testing.T) []walletAdjustmentOperation {
	t.Helper()
	rows, err := queryRuntimeOperations(context.Background(), f.stores[1], runtimeOperationQuery{AccountID: f.accountID, Action: "gateway.wallet_adjustment.v1"})
	if err != nil {
		t.Fatal(err)
	}
	operations := make([]walletAdjustmentOperation, 0, len(rows))
	for _, row := range rows {
		operation, err := decodeWalletAdjustment(row)
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, operation)
	}
	return operations
}
