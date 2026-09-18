package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The customer overview trend must explain a failed Renewal that was charged and
// then fully refunded: that order keeps a confirmed debit in the owner's fund
// record but writes only a refund receipt, so a receipt-based trend reports
// "charged $0.00" while the balance already moved twice.

const settlementTrendOwnerEmail = "settlement-trend-owner@example.com"

type settlementTrendHarness struct {
	server      http.Handler
	service     *controlplane.Service
	app         *controlPlaneServer
	session     *httptest.ResponseRecorder
	sub2API     *customerFactsSub2API
	ledger      *customerFactsLedger
	fabricCalls *[]string
}

func newSettlementTrendHarness(t *testing.T, history map[int64][]clients.Sub2APIBalanceHistoryEntry) *settlementTrendHarness {
	t.Helper()
	ledger := &customerFactsLedger{}
	calls := &[]string{}
	sub2API := &customerFactsSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000, charges: map[string]int64{}},
		history:           history,
	}
	service := controlplane.NewService(ledger, &customerFactsFabric{fakeFabricClient: fakeFabricClient{calls: calls}}, sub2API)
	server := NewServer(service)
	app := server.(*controlPlaneHTTPHandler).app
	mustStore(t, app.tables.CreateProvisionedAccount(context.Background(),
		map[string]any{"id": "acct-alpha", "ownerUserId": "usr-alpha", "sub2apiUserId": int64(41), "status": "active", "workspacePurchaseEnabled": true},
		map[string]any{"id": "usr-alpha", "email": settlementTrendOwnerEmail, "accountId": "acct-alpha", "role": "owner", "status": "active"}))
	return &settlementTrendHarness{server: server, service: service, app: app, sub2API: sub2API, ledger: ledger, fabricCalls: calls}
}

func (h *settlementTrendHarness) get(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	if h.session == nil {
		h.session = loginForTest(t, h.server, settlementTrendOwnerEmail, "CorrectHorseBatteryStaple!")
	}
	return requestWithSession(t, h.server, h.session, http.MethodGet, "/api/billing/workspace-settlements", "")
}

func (h *settlementTrendHarness) envelope(t *testing.T) (int, map[string]any) {
	t.Helper()
	response := h.get(t)
	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return response.Code, envelope
}

func (h *settlementTrendHarness) payload(t *testing.T) map[string]any {
	t.Helper()
	status, envelope := h.envelope(t)
	if status != http.StatusOK {
		t.Fatalf("settlement trend status = %d: %#v", status, envelope)
	}
	return mapField(envelope, "data")
}

func (h *settlementTrendHarness) save(t *testing.T, row map[string]any) {
	t.Helper()
	mustStore(t, h.app.tables.SaveRuntimeOperation(context.Background(), row))
}

func (h *settlementTrendHarness) trend(t *testing.T, now time.Time) workspaceSettlementTrend {
	t.Helper()
	trend, err := h.app.projectWorkspaceSettlementTrend(context.Background(), h.service, "acct-alpha", now)
	if err != nil {
		t.Fatal(err)
	}
	return trend
}

func settlementTrendShanghai() *time.Location {
	return time.FixedZone(clients.Sub2APIUsageTimezone, 8*60*60)
}

// settlementTrendDayKey names the Shanghai calendar day a movement belongs to.
func settlementTrendDayKey(at time.Time) string {
	return at.In(settlementTrendShanghai()).Format("2006-01-02")
}

// settlementTrendToday returns a deterministic instant inside the current
// Shanghai day (00:00 plus hours), so a same-day or in-window assertion never
// depends on the wall clock crossing midnight while the test runs.
func settlementTrendToday(hours int) time.Time {
	location := settlementTrendShanghai()
	now := time.Now().In(location)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Add(time.Duration(hours) * time.Hour)
}

func settlementTrendEntry(code string, valueUSDMicros, userID int64, usedAt time.Time) clients.Sub2APIBalanceHistoryEntry {
	usedBy, at := userID, usedAt
	return clients.Sub2APIBalanceHistoryEntry{Code: code, Type: "balance", ValueUSDMicros: valueUSDMicros, Status: "used", UsedBy: &usedBy, UsedAt: &at, CreatedAt: at}
}

// settlementTrendRenewalRow retains one Renewal order fact. A cancelled
// fulfillment keeps its redeem and refund codes, so the owner can still explain
// the debit that never produced a success receipt.
func settlementTrendRenewalRow(t *testing.T, id, status string, refunded bool) map[string]any {
	t.Helper()
	operation := workspaceRenewalOperation{
		ID: id, Status: status, Phase: "complete", RequestHash: "renewal-hash-" + id,
		AccountID: "acct-alpha", WorkspaceID: "ws-deleted", PackageID: "basic", StorageGB: 10,
		ComputeID: "compute-" + id, StorageID: "storage-" + id, PriceVersion: "pilot-usd-2026-07-v1",
		ComputeUSDMicros: 50_000_000, StorageUSDMicros: 2_580_000, TotalUSDMicros: 52_580_000,
		PeriodStart: "2026-08-01T00:00:00Z", PaidThrough: "2026-09-01T00:00:00Z", RenewedThrough: "2026-10-01T00:00:00Z",
		RedeemCode: "opl:renewal-charge-" + id, ChargeAttempted: true, CreatedAt: "2026-09-01T00:00:00Z",
	}
	if refunded {
		operation.RefundAttempted = true
		operation.RefundCode = "opl:renewal-refund-" + id
		operation.RefundReceiptID = "receipt-renewal-refund-" + id
	}
	return workspaceRenewalOperationRow(operation)
}

func settlementTrendRefundRow(t *testing.T, id, relatedOperationID string, amountUSDMicros, userID int64, status string) map[string]any {
	t.Helper()
	operation := walletAdjustmentOperation{
		RequestHash: "refund-hash-" + id, Phase: "complete", AccountID: "acct-alpha", Sub2APIUserID: userID,
		Kind: "business_refund", AmountUSDMicros: amountUSDMicros, AmountUSD: "52.580000", ActorUserID: "usr-alpha",
		RelatedOperationID: relatedOperationID, CanonicalRedeemCode: walletAdjustmentRedeemCode(id), RedeemCodeVersion: "v2",
		CreatedAt: "2026-09-10T00:00:00Z", UpdatedAt: "2026-09-10T00:05:00Z", Status: status,
		AdjustmentAttempted: true, ReceiptID: "receipt-" + id,
	}
	return walletAdjustmentRow(id, operation)
}

// settlementTrendPurchaseRow builds a succeeded Workspace purchase order. The
// local reconcile fixture records only a placeholder debit confirmation, so the
// retained row is completed with the confirmation shape the production
// reconciler writes from the wallet response: its code, wallet user, amount and
// applied state.
func settlementTrendPurchaseRow(t *testing.T) (map[string]any, string, int64) {
	t.Helper()
	row := d2CustomerRefundOriginal(t, false)
	var facts map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &facts); err != nil {
		t.Fatal(err)
	}
	code := stringValue(facts["sub2apiRedeemCode"])
	amount := int64(numberField(facts, "totalChargeUsdMicros", 0))
	facts["sub2apiUserId"] = int64(41)
	facts["chargeConfirmation"] = map[string]any{"code": code, "userId": int64(41), "chargeUsdMicros": amount, "status": "used"}
	row["result"] = string(mustJSON(facts))
	return row, code, amount
}

func settlementTrendDayCounts(payload map[string]any) map[string]int64 {
	days := map[string]int64{}
	for _, item := range payload["days"].([]any) {
		day := item.(map[string]any)
		days[stringValue(day["date"])] = int64(numberField(day, "chargedUsdMicros", 0))
	}
	return days
}

func settlementTrendRefundDays(payload map[string]any) map[string]int64 {
	days := map[string]int64{}
	for _, item := range payload["days"].([]any) {
		day := item.(map[string]any)
		days[stringValue(day["date"])] = int64(numberField(day, "refundedUsdMicros", 0))
	}
	return days
}

func settlementTrendCounts(payload map[string]any) map[string]int64 {
	counts := map[string]int64{}
	for _, key := range []string{"chargedUsdMicros", "refundedUsdMicros", "netUsdMicros", "settledCount", "inFlightCount", "unconfirmedCount", "unattributedCount", "outOfWindowCount"} {
		counts[key] = int64(numberField(payload, key, 0))
	}
	return counts
}

func settlementTrendRetainedRow(t *testing.T, harness *settlementTrendHarness, id string) string {
	t.Helper()
	row, found, err := harness.app.tables.GetRuntimeOperation(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("retained operation %s missing: found=%v err=%v", id, found, err)
	}
	return string(mustJSON(row))
}

func TestWorkspaceSettlementTrendCountsPurchaseAndRenewalCharges(t *testing.T) {
	purchase, purchaseCode, purchaseCharge := settlementTrendPurchaseRow(t)
	renewal := settlementTrendRenewalRow(t, "renewal-active", "active", false)
	purchaseAt, renewalAt := time.Now().UTC().Add(-3*24*time.Hour), time.Now().UTC().Add(-24*time.Hour)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry(purchaseCode, -purchaseCharge, 41, purchaseAt),
		settlementTrendEntry("opl:renewal-charge-renewal-active", -52_580_000, 41, renewalAt),
	}})
	harness.save(t, purchase)
	harness.save(t, renewal)

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["chargedUsdMicros"] != purchaseCharge+52_580_000 || counts["refundedUsdMicros"] != 0 || counts["netUsdMicros"] != purchaseCharge+52_580_000 || counts["settledCount"] != 2 {
		t.Fatalf("charge payload = %#v", counts)
	}
	if payload["complete"] != true || payload["timezone"] != clients.Sub2APIUsageTimezone {
		t.Fatalf("charge completeness = %#v", payload)
	}
	days := settlementTrendDayCounts(payload)
	if days[settlementTrendDayKey(purchaseAt)] != purchaseCharge || days[settlementTrendDayKey(renewalAt)] != 52_580_000 {
		t.Fatalf("charge days = %#v", days)
	}
}

func TestWorkspaceSettlementTrendExplainsFailedRenewalFullRefund(t *testing.T) {
	// The debit and its reversal land on the same Shanghai day, and the order
	// never wrote a purchase receipt.
	chargedAt, refundedAt := settlementTrendToday(1), settlementTrendToday(2)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-cancelled", -52_580_000, 41, chargedAt),
		settlementTrendEntry("opl:renewal-refund-renewal-cancelled", 52_580_000, 41, refundedAt),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-cancelled", "refunded", true))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["chargedUsdMicros"] != 52_580_000 || counts["refundedUsdMicros"] != 52_580_000 || counts["netUsdMicros"] != 0 {
		t.Fatalf("cancelled renewal payload = %#v", counts)
	}
	if counts["settledCount"] != 2 || counts["unconfirmedCount"] != 0 || counts["inFlightCount"] != 0 || payload["complete"] != true {
		t.Fatalf("cancelled renewal counts = %#v", payload)
	}
	day := settlementTrendDayKey(chargedAt)
	if settlementTrendDayKey(refundedAt) != day {
		t.Fatalf("fixture must stay inside one Shanghai day")
	}
	if settlementTrendDayCounts(payload)[day] != 52_580_000 || settlementTrendRefundDays(payload)[day] != 52_580_000 {
		t.Fatalf("same day pair must stay on one day: %#v", payload["days"])
	}
	if harness.ledger.queries != nil || harness.ledger.receiptWrites != 0 {
		t.Fatalf("trend must not read or write Ledger receipts: %#v", harness.ledger.queries)
	}
}

func TestWorkspaceSettlementTrendDatesMovementsByShanghaiDay(t *testing.T) {
	now := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	// 2026-09-17T16:30Z is 2026-09-18 00:30 in Asia/Shanghai; a UTC calendar
	// would file this charge under the previous day.
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-early", -52_580_000, 41, time.Date(2026, 9, 17, 16, 30, 0, 0, time.UTC)),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-early", "active", false))

	trend := harness.trend(t, now)
	if trend.location == time.UTC || trend.windowStart.Format("2006-01-02") != "2026-09-05" {
		t.Fatalf("window = %s in %s", trend.windowStart, trend.location)
	}
	if len(trend.days) != workspaceSettlementTrendDays {
		t.Fatalf("days = %d", len(trend.days))
	}
	byDate := map[string]int64{}
	for _, day := range trend.days {
		byDate[day.date] = day.chargedUSDMicros
	}
	if byDate["2026-09-18"] != 52_580_000 || byDate["2026-09-17"] != 0 {
		t.Fatalf("shanghai day bucketing = %#v", byDate)
	}
}

func TestWorkspaceSettlementTrendKeepsCrossWindowRefundsNegative(t *testing.T) {
	refundedAt := settlementTrendToday(1)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-old", -52_580_000, 41, time.Now().UTC().Add(-20*24*time.Hour)),
		settlementTrendEntry(walletAdjustmentRedeemCode("refund-cross-window"), 30_000_000, 41, refundedAt),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-old", "active", false))
	harness.save(t, settlementTrendRefundRow(t, "refund-cross-window", "renewal-old", 30_000_000, 41, "succeeded"))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["chargedUsdMicros"] != 0 || counts["refundedUsdMicros"] != 30_000_000 || counts["netUsdMicros"] != -30_000_000 {
		t.Fatalf("cross window payload = %#v", counts)
	}
	if counts["outOfWindowCount"] != 1 || counts["unconfirmedCount"] != 0 {
		t.Fatalf("cross window counts = %#v", counts)
	}
	// The out-of-window charge is never re-dated onto the refund day.
	day := settlementTrendDayKey(refundedAt)
	if settlementTrendDayCounts(payload)[day] != 0 || settlementTrendRefundDays(payload)[day] != 30_000_000 {
		t.Fatalf("cross window days = %#v", payload["days"])
	}
}

func TestWorkspaceSettlementTrendHandlesPartialAndMultipleLinkedRefunds(t *testing.T) {
	firstAt, secondAt := time.Now().UTC().Add(-3*24*time.Hour), time.Now().UTC().Add(-24*time.Hour)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-partial", -52_580_000, 41, time.Now().UTC().Add(-6*24*time.Hour)),
		settlementTrendEntry(walletAdjustmentRedeemCode("refund-partial-one"), 30_000_000, 41, firstAt),
		settlementTrendEntry(walletAdjustmentRedeemCode("refund-partial-two"), 22_580_000, 41, secondAt),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-partial", "active", false))
	harness.save(t, settlementTrendRefundRow(t, "refund-partial-one", "renewal-partial", 30_000_000, 41, "succeeded"))
	harness.save(t, settlementTrendRefundRow(t, "refund-partial-two", "renewal-partial", 22_580_000, 41, "succeeded"))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["settledCount"] != 3 || counts["chargedUsdMicros"] != 52_580_000 || counts["refundedUsdMicros"] != 52_580_000 || counts["netUsdMicros"] != 0 {
		t.Fatalf("partial refunds = %#v", counts)
	}
	if counts["unconfirmedCount"] != 0 || counts["unattributedCount"] != 0 {
		t.Fatalf("partial refund counts = %#v", counts)
	}
	if settlementTrendRefundDays(payload)[settlementTrendDayKey(firstAt)] != 30_000_000 || settlementTrendRefundDays(payload)[settlementTrendDayKey(secondAt)] != 22_580_000 {
		t.Fatalf("partial refund days = %#v", settlementTrendRefundDays(payload))
	}
}

func TestWorkspaceSettlementTrendExcludesUnattributedAdjustments(t *testing.T) {
	usedAt := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry(walletAdjustmentRedeemCode("refund-manual"), 5_000_000, 41, usedAt),
	}})
	harness.save(t, settlementTrendRefundRow(t, "refund-manual", "", 5_000_000, 41, "succeeded"))

	counts := settlementTrendCounts(harness.payload(t))
	if counts["unattributedCount"] != 1 || counts["settledCount"] != 0 || counts["refundedUsdMicros"] != 0 {
		t.Fatalf("unattributed adjustment = %#v", counts)
	}
}

func TestWorkspaceSettlementTrendRejectsAmbiguousFundEvidence(t *testing.T) {
	now := settlementTrendToday(1)
	for _, testCase := range []struct {
		name    string
		history map[int64][]clients.Sub2APIBalanceHistoryEntry
	}{
		{name: "amount differs", history: map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
			settlementTrendEntry("opl:renewal-charge-renewal-ambiguous", -52_580_001, 41, now.Add(-time.Hour)),
		}}},
		{name: "another wallet user", history: map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
			settlementTrendEntry("opl:renewal-charge-renewal-ambiguous", -52_580_000, 42, now.Add(-time.Hour)),
		}}},
		{name: "signed the wrong way", history: map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
			settlementTrendEntry("opl:renewal-charge-renewal-ambiguous", 52_580_000, 41, now.Add(-time.Hour)),
		}}},
		{name: "no wallet record", history: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newSettlementTrendHarness(t, testCase.history)
			harness.save(t, settlementTrendRenewalRow(t, "renewal-ambiguous", "active", false))
			counts := settlementTrendCounts(harness.payload(t))
			if counts["unconfirmedCount"] != 1 || counts["settledCount"] != 0 || counts["chargedUsdMicros"] != 0 {
				t.Fatalf("ambiguous evidence = %#v", counts)
			}
		})
	}
}

func TestWorkspaceSettlementTrendKeepsDuplicateFundCodesUncounted(t *testing.T) {
	now := settlementTrendToday(1)
	shared := "opl:renewal-charge-renewal-duplicate-one"
	second := settlementTrendRenewalRow(t, "renewal-duplicate-two", "active", false)
	var facts map[string]any
	if err := json.Unmarshal([]byte(stringValue(second["result"])), &facts); err != nil {
		t.Fatal(err)
	}
	facts["sub2apiRedeemCode"] = shared
	second["result"] = string(mustJSON(facts))
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry(shared, -52_580_000, 41, now.Add(-time.Hour)),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-duplicate-one", "active", false))
	harness.save(t, second)

	counts := settlementTrendCounts(harness.payload(t))
	if counts["unconfirmedCount"] != 2 || counts["settledCount"] != 0 || counts["chargedUsdMicros"] != 0 {
		t.Fatalf("duplicate code = %#v", counts)
	}
}

func TestWorkspaceSettlementTrendIsIdempotentAcrossReads(t *testing.T) {
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-repeat", -52_580_000, 41, settlementTrendToday(1)),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-repeat", "active", false))

	first, second := harness.payload(t), harness.payload(t)
	for _, key := range []string{"chargedUsdMicros", "refundedUsdMicros", "netUsdMicros", "settledCount"} {
		if first[key] != second[key] {
			t.Fatalf("repeat read changed %s: %#v vs %#v", key, first[key], second[key])
		}
	}
	if settlementTrendCounts(first)["chargedUsdMicros"] != 52_580_000 || len(harness.sub2API.historyIDs) != 2 {
		t.Fatalf("repeat read payload = %#v reads=%#v", first, harness.sub2API.historyIDs)
	}
	if harness.ledger.receiptWrites != 0 {
		t.Fatalf("trend wrote receipts: %d", harness.ledger.receiptWrites)
	}
}

func TestWorkspaceSettlementTrendKeepsDeletedWorkspaceHistory(t *testing.T) {
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-deleted-workspace", -52_580_000, 41, settlementTrendToday(1)),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-deleted-workspace", "active", false))
	// The Workspace projection is gone; the paid order remains the money fact.
	if _, found, err := harness.app.tables.GetWorkspace(context.Background(), "ws-deleted"); err != nil || found {
		t.Fatalf("workspace should be absent: found=%v err=%v", found, err)
	}

	counts := settlementTrendCounts(harness.payload(t))
	if counts["chargedUsdMicros"] != 52_580_000 || counts["settledCount"] != 1 {
		t.Fatalf("deleted workspace history = %#v", counts)
	}
}

func TestWorkspaceSettlementTrendDistinguishesUnavailableMissingTimeAndRealZero(t *testing.T) {
	t.Run("fund source unavailable", func(t *testing.T) {
		harness := newSettlementTrendHarness(t, nil)
		harness.sub2API.historyErr = clients.ErrSub2APIChargeUnknown
		harness.save(t, settlementTrendRenewalRow(t, "renewal-unavailable", "active", false))
		response := harness.get(t)
		assertUnavailableWorkspaceEnvelope(t, response, http.StatusBadGateway, "control_plane")
	})

	t.Run("missing effective time is not zero", func(t *testing.T) {
		harness := newSettlementTrendHarness(t, nil)
		harness.save(t, settlementTrendRenewalRow(t, "renewal-no-time", "active", false))
		// The owner reports the amount without an applied time: the movement is
		// known but cannot be dated, so it stays out of the days.
		usedBy := int64(41)
		harness.sub2API.history = map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
			{Code: "opl:renewal-charge-renewal-no-time", Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy},
		}}
		payload := harness.payload(t)
		counts := settlementTrendCounts(payload)
		if counts["unconfirmedCount"] != 1 || counts["settledCount"] != 0 || counts["chargedUsdMicros"] != 0 || payload["complete"] != false {
			t.Fatalf("missing time payload = %#v", payload)
		}
	})

	t.Run("real zero stays complete", func(t *testing.T) {
		harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
			settlementTrendEntry("opl:renewal-charge-renewal-zero", -52_580_000, 41, settlementTrendToday(1)),
			settlementTrendEntry("opl:renewal-refund-renewal-zero", 52_580_000, 41, settlementTrendToday(2)),
		}})
		harness.save(t, settlementTrendRenewalRow(t, "renewal-zero", "refunded", true))
		status, envelope := harness.envelope(t)
		if status != http.StatusOK || envelope["status"] != "available" {
			t.Fatalf("real zero envelope = %#v", envelope)
		}
		payload := mapField(envelope, "data")
		counts := settlementTrendCounts(payload)
		if counts["settledCount"] != 2 || counts["netUsdMicros"] != 0 || payload["complete"] != true {
			t.Fatalf("real zero payload = %#v", payload)
		}
	})

	t.Run("nothing in scope is empty", func(t *testing.T) {
		harness := newSettlementTrendHarness(t, nil)
		status, envelope := harness.envelope(t)
		if status != http.StatusOK || envelope["status"] != "empty" {
			t.Fatalf("empty envelope = %#v", envelope)
		}
		if len(mapField(envelope, "data")["days"].([]any)) != workspaceSettlementTrendDays {
			t.Fatalf("empty trend must still return the window: %#v", envelope)
		}
	})
}

func TestWorkspaceSettlementTrendIsolatesAccountsAndWritesNothing(t *testing.T) {
	// A retained row stored under this account whose order names another account
	// must never be folded in as this account's money.
	foreign := settlementTrendRenewalRow(t, "renewal-foreign", "active", false)
	var facts map[string]any
	if err := json.Unmarshal([]byte(stringValue(foreign["result"])), &facts); err != nil {
		t.Fatal(err)
	}
	facts["accountId"] = "acct-beta"
	facts["sub2apiUserId"] = int64(42)
	foreign["result"] = string(mustJSON(facts))
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-own", -52_580_000, 41, settlementTrendToday(1)),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-own", "active", false))
	harness.save(t, foreign)

	before := settlementTrendRetainedRow(t, harness, "renewal-own")
	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["chargedUsdMicros"] != 52_580_000 || counts["unconfirmedCount"] != 1 {
		t.Fatalf("account isolation = %#v", counts)
	}
	if len(harness.sub2API.historyIDs) != 1 || harness.sub2API.historyIDs[0] != 41 {
		t.Fatalf("trend read another wallet: %#v", harness.sub2API.historyIDs)
	}
	if strings.Contains(string(mustJSON(payload)), "acct-beta") || strings.Contains(string(mustJSON(payload)), "renewal-foreign") {
		t.Fatalf("payload leaked another account: %#v", payload)
	}
	// A read-only trend never touches Ledger, Fabric, money or retained rows.
	if harness.ledger.query != (clients.ReceiptQuery{}) || len(harness.ledger.queries) != 0 || harness.ledger.receiptWrites != 0 {
		t.Fatalf("trend read Ledger: %#v writes=%d", harness.ledger.queries, harness.ledger.receiptWrites)
	}
	if len(*harness.fabricCalls) != 0 {
		t.Fatalf("trend called Fabric: %#v", *harness.fabricCalls)
	}
	if after := settlementTrendRetainedRow(t, harness, "renewal-own"); after != before {
		t.Fatalf("trend mutated the retained order:\n%s\n%s", before, after)
	}
	adjustments := 0
	for _, row := range readRuntimeOperationRows(t, harness) {
		if stringValue(row["action"]) == "gateway.wallet_adjustment.v1" {
			adjustments++
		}
	}
	if adjustments != 0 {
		t.Fatalf("trend created wallet adjustments: %d", adjustments)
	}
}

func TestWorkspaceSettlementTrendCountsNothingForAnUndispatchedRenewalOrder(t *testing.T) {
	// No debit request was dispatched for this order, so the owner has no fund
	// movement to report: it is processing, not unknown money.
	harness := newSettlementTrendHarness(t, nil)
	harness.save(t, settlementTrendRenewalRowWithDispatch(t, "renewal-not-dispatched", "claimed", false, false))

	status, envelope := harness.envelope(t)
	counts := settlementTrendCounts(mapField(envelope, "data"))
	if counts["inFlightCount"] != 0 || counts["unconfirmedCount"] != 0 || counts["settledCount"] != 0 || counts["chargedUsdMicros"] != 0 {
		t.Fatalf("an undispatched renewal order = %#v", counts)
	}
	if status != http.StatusOK || envelope["status"] != "empty" {
		t.Fatalf("an undispatched renewal order carries no movement: %#v", envelope)
	}
}

func readRuntimeOperationRows(t *testing.T, harness *settlementTrendHarness) []map[string]any {
	t.Helper()
	rows := []map[string]any{}
	for _, action := range []string{"workspace.launch", "workspace.launch.v2", "workspace.renewal", "gateway.wallet_adjustment.v1", workspaceDeleteAction, workspaceDeleteLegacyAction} {
		found, err := queryRuntimeOperations(context.Background(), harness.app.tables, runtimeOperationQuery{AccountID: "acct-alpha", Action: action})
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, found...)
	}
	return rows
}

// settlementTrendLegacyDeleteRow retains the refund a retired Workspace delete
// wrote on its own operation row instead of a business-refund adjustment.
func settlementTrendLegacyDeleteRow(t *testing.T, terminal bool) (map[string]any, string) {
	t.Helper()
	operationID := workspaceDeleteLegacyOperationID("ws-deleted")
	refundCode := monthlyRefundCode(monthlyEnvironment(), operationID)
	createdAt := settlementTrendToday(1).UTC().Format(time.RFC3339Nano)
	operation := workspaceDeleteLegacyOperation{
		OperationID: operationID, AccountID: "acct-alpha", OwnerUserID: "usr-alpha", Sub2APIUserID: 41, WorkspaceID: "ws-deleted",
		LaunchOperationID: "renewal-legacy-delete", RuntimeID: "runtime-alpha", ComputeID: "compute-alpha", StorageID: "storage-alpha",
		AttachmentID: "attachment-alpha", WorkspaceAPIKeyID: 19, GatewaySecretRef: "opl-gateway-ws-alpha",
		GatewayFingerprint: "sha256:" + strings.Repeat("a", 64),
		DebitCode:          "opl:renewal-charge-renewal-legacy-delete", PurchaseReceiptID: "receipt-purchase-alpha",
		PurchaseReceipt: clients.ReceiptInput{
			Type: "billing.workspace_purchased.v1", Status: "completed", AccountID: "acct-alpha", WorkspaceID: "ws-deleted", RequestID: "renewal-legacy-delete",
			Cost:      map[string]any{"sub2apiUserId": int64(41), "sub2apiRedeemCode": "opl:renewal-charge-renewal-legacy-delete", "totalUsdMicros": int64(52_580_000), "resourceId": "ws-deleted"},
			Execution: map[string]any{"runtimeId": "runtime-alpha", "computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "attachmentId": "attachment-alpha", "workspaceApiKeyId": int64(19)},
		},
		RefundCode: refundCode, TotalUSDMicros: 52_580_000, Phase: "complete", Status: "succeeded", CreatedAt: createdAt,
	}
	if terminal {
		operation.RuntimeStatus, operation.SecretStatus, operation.AttachmentStatus = "absent", "absent", "detached"
		operation.StorageStatus, operation.ComputeStatus, operation.KeyStatus = "destroyed", "destroyed", "absent"
		operation.RefundReceiptID = "receipt-refund-alpha"
		operation.RefundConfirmation = map[string]any{"code": refundCode, "userId": int64(41), "refundUsdMicros": int64(52_580_000), "status": "used"}
	} else {
		operation.Phase, operation.Status, operation.RefundAttempted = "refund", "pending", true
	}
	operation.RequestHash = workspaceDeleteLegacyRequestHash(operation)
	return map[string]any{
		"id": operation.OperationID, "operationId": operation.OperationID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceId": operation.WorkspaceID, "resourceKind": "workspace", "action": workspaceDeleteLegacyAction, "status": operation.Status,
		"result": string(mustJSON(operation)), "computeAllocationId": operation.ComputeID, "storageId": operation.StorageID,
		"attachmentId": operation.AttachmentID, "runtimeId": operation.RuntimeID, "createdAt": createdAt,
	}, refundCode
}

func TestWorkspaceSettlementTrendCountsTheRetiredDeleteRefund(t *testing.T) {
	// The retired Workspace delete refunded the original order on its own row.
	// Dropping it would report a charge with no matching refund while claiming a
	// complete summary.
	chargedAt, refundedAt := time.Now().UTC().Add(-5*24*time.Hour), settlementTrendToday(1)
	deleteRow, refundCode := settlementTrendLegacyDeleteRow(t, true)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-legacy-delete", -52_580_000, 41, chargedAt),
		settlementTrendEntry(refundCode, 52_580_000, 41, refundedAt),
	}})
	// The original purchase order is still retained; the delete row names it.
	harness.save(t, settlementTrendRenewalRow(t, "renewal-legacy-delete", "active", false))
	harness.save(t, deleteRow)

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["chargedUsdMicros"] != 52_580_000 || counts["refundedUsdMicros"] != 52_580_000 || counts["netUsdMicros"] != 0 {
		t.Fatalf("retired delete refund = %#v", counts)
	}
	if counts["settledCount"] != 2 || counts["unconfirmedCount"] != 0 || payload["complete"] != true {
		t.Fatalf("retired delete refund counts = %#v", payload)
	}
	if settlementTrendRefundDays(payload)[settlementTrendDayKey(refundedAt)] != 52_580_000 {
		t.Fatalf("retired delete refund day = %#v", settlementTrendRefundDays(payload))
	}
}

func TestWorkspaceSettlementTrendKeepsAnUnconfirmedRetiredDeleteRefundOpen(t *testing.T) {
	chargedAt := time.Now().UTC().Add(-5 * 24 * time.Hour)
	deleteRow, refundCode := settlementTrendLegacyDeleteRow(t, false)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-legacy-delete", -52_580_000, 41, chargedAt),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-legacy-delete", "active", false))
	harness.save(t, deleteRow)

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["chargedUsdMicros"] != 52_580_000 || counts["refundedUsdMicros"] != 0 {
		t.Fatalf("unconfirmed delete refund = %#v", counts)
	}
	if counts["unconfirmedCount"] != 1 || payload["complete"] != false {
		t.Fatalf("unconfirmed delete refund must stay open: %#v", payload)
	}
	if strings.Contains(string(mustJSON(payload)), refundCode) {
		t.Fatalf("payload leaked the upstream refund code: %#v", payload)
	}
}

func TestWorkspaceSettlementTrendRejectsRefundsBeyondTheOriginalCharge(t *testing.T) {
	// Two confirmed refunds against one $52.58 charge cannot both be explained;
	// the owner reconciliation calls this billing_refund_limit_exceeded.
	chargedAt, firstAt, secondAt := time.Now().UTC().Add(-6*24*time.Hour), settlementTrendToday(1), settlementTrendToday(2)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-overflow", -52_580_000, 41, chargedAt),
		settlementTrendEntry(walletAdjustmentRedeemCode("refund-overflow-one"), 52_580_000, 41, firstAt),
		settlementTrendEntry(walletAdjustmentRedeemCode("refund-overflow-two"), 30_000_000, 41, secondAt),
	}})
	harness.save(t, settlementTrendRenewalRow(t, "renewal-overflow", "active", false))
	harness.save(t, settlementTrendRefundRow(t, "refund-overflow-one", "renewal-overflow", 52_580_000, 41, "succeeded"))
	harness.save(t, settlementTrendRefundRow(t, "refund-overflow-two", "renewal-overflow", 30_000_000, 41, "succeeded"))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["refundedUsdMicros"] != 52_580_000 || counts["netUsdMicros"] != 0 {
		t.Fatalf("overflow refunds must not exceed the charge: %#v", counts)
	}
	if counts["unconfirmedCount"] != 1 || payload["complete"] != false {
		t.Fatalf("overflow refund must stay open: %#v", payload)
	}
}

// A Renewal worker persists ChargeAttempted=true, dispatches the debit and only
// then learns that the result is unknown (ErrSub2APIChargeUnknown keeps the row
// in debit_pending). The wallet may already have moved while its balance record
// is not readable yet, so this movement is unknown, never "nothing happened".

// settlementTrendRenewalRowWithDispatch keeps the owner's dispatch facts intact
// so a test can model a request that was sent but has no confirmed result.
func settlementTrendRenewalRowWithDispatch(t *testing.T, id, status string, chargeAttempted, refundAttempted bool) map[string]any {
	t.Helper()
	row := settlementTrendRenewalRow(t, id, status, false)
	var facts map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &facts); err != nil {
		t.Fatal(err)
	}
	facts["chargeAttempted"] = chargeAttempted
	if refundAttempted {
		facts["refundAttempted"] = true
		facts["sub2apiRefundCode"] = "opl:renewal-refund-" + id
		facts["refundReceiptId"] = "receipt-renewal-refund-" + id
	}
	row["result"] = string(mustJSON(facts))
	return row
}

// settlementTrendRefundAdjustmentRow models an operator business refund whose
// request was dispatched but whose result is not readable yet.
func settlementTrendRefundAdjustmentRow(t *testing.T, id string, amountUSDMicros int64, status string, adjustmentAttempted bool) map[string]any {
	t.Helper()
	operation := walletAdjustmentOperation{
		RequestHash: "refund-hash-" + id, Phase: "adjustment", AccountID: "acct-alpha", Sub2APIUserID: 41,
		Kind: "business_refund", AmountUSDMicros: amountUSDMicros, AmountUSD: "30.000000", ActorUserID: "usr-alpha",
		RelatedOperationID: "renewal-refund-unknown", CanonicalRedeemCode: walletAdjustmentRedeemCode(id), RedeemCodeVersion: "v2",
		CreatedAt: "2026-09-10T00:00:00Z", UpdatedAt: "2026-09-10T00:05:00Z", Status: status, AdjustmentAttempted: adjustmentAttempted,
	}
	row := walletAdjustmentRow(id, operation)
	row["status"] = status
	return row
}

func TestWorkspaceSettlementTrendKeepsAnUnknownRenewalDebitUnconfirmed(t *testing.T) {
	// Real failure path: ChargeAttempted=true was persisted before the request,
	// the request result is unknown, and the balance record is not readable yet.
	harness := newSettlementTrendHarness(t, nil)
	harness.save(t, settlementTrendRenewalRowWithDispatch(t, "renewal-debit-unknown", "debit_pending", true, false))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["unconfirmedCount"] != 1 || counts["inFlightCount"] != 0 {
		t.Fatalf("an unknown dispatched debit is not in flight: %#v", counts)
	}
	if payload["complete"] != false {
		t.Fatalf("an unknown dispatched debit must not claim a complete summary: %#v", payload)
	}
	if counts["chargedUsdMicros"] != 0 || counts["netUsdMicros"] != 0 {
		t.Fatalf("unknown money must not enter the totals: %#v", counts)
	}
}

func TestWorkspaceSettlementTrendKeepsAConfirmedDebitWithoutEffectiveTimeUnconfirmed(t *testing.T) {
	usedBy, at := int64(41), settlementTrendToday(1)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		{Code: "opl:renewal-charge-renewal-no-effective-time", Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy, CreatedAt: at},
	}})
	harness.save(t, settlementTrendRenewalRowWithDispatch(t, "renewal-no-effective-time", "debited", true, false))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["unconfirmedCount"] != 1 || counts["settledCount"] != 0 || payload["complete"] != false {
		t.Fatalf("a confirmed debit without an effective time = %#v", counts)
	}
}

func TestWorkspaceSettlementTrendKeepsAUnknownDispatchedRefundUnconfirmed(t *testing.T) {
	chargedAt := time.Now().UTC().Add(-3 * 24 * time.Hour)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-refund-unknown", -52_580_000, 41, chargedAt),
	}})
	harness.save(t, settlementTrendRenewalRowWithDispatch(t, "renewal-refund-unknown", "refund_pending", true, true))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["refundedUsdMicros"] != 0 || counts["chargedUsdMicros"] != 52_580_000 {
		t.Fatalf("an unknown dispatched refund must not enter the totals: %#v", counts)
	}
	if counts["unconfirmedCount"] != 1 || payload["complete"] != false {
		t.Fatalf("an unknown dispatched refund must stay unconfirmed: %#v", counts)
	}
}

func TestWorkspaceSettlementTrendKeepsAUnknownDispatchedAdjustmentUnconfirmed(t *testing.T) {
	chargedAt := time.Now().UTC().Add(-3 * 24 * time.Hour)
	original := settlementTrendRenewalRow(t, "renewal-refund-unknown", "active", false)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-refund-unknown", -52_580_000, 41, chargedAt),
	}})
	harness.save(t, original)
	harness.save(t, settlementTrendRefundAdjustmentRow(t, "refund-adjustment-unknown", 30_000_000, "pending", true))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["unconfirmedCount"] != 1 || counts["inFlightCount"] != 0 || counts["refundedUsdMicros"] != 0 || payload["complete"] != false {
		t.Fatalf("an unknown dispatched adjustment = %#v", counts)
	}
}

func TestWorkspaceSettlementTrendKeepsAnUndispatchedOrderInFlight(t *testing.T) {
	// No fund request was dispatched, so the order is processing rather than
	// unknown. The owner facts, not the status name, decide this.
	original := settlementTrendRenewalRow(t, "renewal-refund-unknown", "active", false)
	harness := newSettlementTrendHarness(t, map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-refund-unknown", -52_580_000, 41, time.Now().UTC().Add(-3*24*time.Hour)),
	}})
	harness.save(t, original)
	harness.save(t, settlementTrendRefundAdjustmentRow(t, "refund-not-dispatched", 30_000_000, "pending", false))

	payload := harness.payload(t)
	counts := settlementTrendCounts(payload)
	if counts["inFlightCount"] != 1 || counts["unconfirmedCount"] != 0 {
		t.Fatalf("an undispatched pending refund is in flight: %#v", counts)
	}
	if payload["complete"] != true || counts["refundedUsdMicros"] != 0 {
		t.Fatalf("an undispatched pending refund has no unknown money: %#v", payload)
	}
}

func TestWorkspaceSettlementTrendRecoversAfterTheFundEvidenceArrives(t *testing.T) {
	chargedAt := settlementTrendToday(1)
	harness := newSettlementTrendHarness(t, nil)
	harness.save(t, settlementTrendRenewalRowWithDispatch(t, "renewal-evidence-late", "debit_pending", true, false))

	before := settlementTrendCounts(harness.payload(t))
	if before["unconfirmedCount"] != 1 || before["chargedUsdMicros"] != 0 || harness.payload(t)["complete"] != false {
		t.Fatalf("unknown debit before evidence = %#v", before)
	}
	// The wallet record becomes readable: the same row now resolves exactly.
	harness.sub2API.history = map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {
		settlementTrendEntry("opl:renewal-charge-renewal-evidence-late", -52_580_000, 41, chargedAt),
	}}
	after := settlementTrendCounts(harness.payload(t))
	if after["chargedUsdMicros"] != 52_580_000 || after["settledCount"] != 1 || after["unconfirmedCount"] != 0 {
		t.Fatalf("unknown debit after evidence = %#v", after)
	}
	if harness.payload(t)["complete"] != true {
		t.Fatalf("resolved debit must be complete: %#v", harness.payload(t))
	}
	if settlementTrendDayCounts(harness.payload(t))[settlementTrendDayKey(chargedAt)] != 52_580_000 {
		t.Fatalf("resolved debit day = %#v", settlementTrendDayCounts(harness.payload(t)))
	}
}
