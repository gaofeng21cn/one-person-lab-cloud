package server

import (
	"context"
	"sort"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The customer overview trend counts Workspace wallet movements, not Ledger
// receipts: one failed Renewal whose fulfillment was cancelled has a confirmed
// debit and a full refund but only one refund receipt. The Control Plane
// settlement model already explains that order as two movements, so the trend
// reuses it and only adds the owner's fund evidence and its effective day.
const workspaceSettlementTrendDays = 14

// Workspace settlement movements are dated by the wallet record that actually
// applied the money, bucketed the same way Gateway Usage already buckets its
// periods.
const workspaceSettlementTrendTimezone = clients.Sub2APIUsageTimezone

type workspaceSettlementTrendDay struct {
	date              string
	chargedUSDMicros  int64
	refundedUSDMicros int64
	chargeCount       int
	refundCount       int
}

type workspaceSettlementTrend struct {
	location          *time.Location
	asOf              time.Time
	windowStart       time.Time
	windowEnd         time.Time
	days              []workspaceSettlementTrendDay
	chargedUSDMicros  int64
	refundedUSDMicros int64
	settledCount      int
	inFlightCount     int
	unconfirmedCount  int
	unattributedCount int
	outOfWindowCount  int
}

// A Workspace order that has not produced a wallet movement yet is in flight,
// not a defect: the owner promises no settled fund fact for it. Anything the
// owner cannot explain from this account's records stays unconfirmed so the
// page never presents an incomplete sum as complete.
type workspaceSettlementState string

const (
	workspaceSettlementSettled      workspaceSettlementState = "settled"
	workspaceSettlementOutOfWindow  workspaceSettlementState = "out_of_window"
	workspaceSettlementInFlight     workspaceSettlementState = "in_flight"
	workspaceSettlementUnconfirmed  workspaceSettlementState = "unconfirmed"
	workspaceSettlementUnattributed workspaceSettlementState = "unattributed"
)

type workspaceSettlementMeasurement struct {
	state       workspaceSettlementState
	kind        string
	amount      int64
	effectiveAt time.Time
}

func newWorkspaceSettlementTrend(now time.Time) workspaceSettlementTrend {
	location := time.FixedZone(workspaceSettlementTrendTimezone, 8*60*60)
	today := now.In(location)
	windowStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -(workspaceSettlementTrendDays - 1))
	days := make([]workspaceSettlementTrendDay, 0, workspaceSettlementTrendDays)
	for offset := 0; offset < workspaceSettlementTrendDays; offset++ {
		days = append(days, workspaceSettlementTrendDay{date: windowStart.AddDate(0, 0, offset).Format("2006-01-02")})
	}
	return workspaceSettlementTrend{
		location:    location,
		asOf:        now.UTC(),
		windowStart: windowStart,
		windowEnd:   windowStart.AddDate(0, 0, workspaceSettlementTrendDays),
		days:        days,
	}
}

func (t workspaceSettlementTrend) netUSDMicros() int64 {
	return t.chargedUSDMicros - t.refundedUSDMicros
}

// A window with no movement, no unresolved fund fact and nothing in flight is a
// real empty result; a window that merely sums to zero is not.
func (t workspaceSettlementTrend) hasMovements() bool {
	return t.settledCount > 0
}

func (t *workspaceSettlementTrend) record(measurement workspaceSettlementMeasurement) {
	switch measurement.state {
	case workspaceSettlementSettled:
		day := measurement.effectiveAt.In(t.location).Format("2006-01-02")
		for index := range t.days {
			if t.days[index].date != day {
				continue
			}
			t.settledCount++
			if measurement.kind == "refund" {
				t.days[index].refundedUSDMicros += measurement.amount
				t.days[index].refundCount++
				t.refundedUSDMicros += measurement.amount
				return
			}
			t.days[index].chargedUSDMicros += measurement.amount
			t.days[index].chargeCount++
			t.chargedUSDMicros += measurement.amount
			return
		}
		// A confirmed movement dated outside the returned window still belongs to
		// the order, it simply does not fall in these days.
		t.outOfWindowCount++
	case workspaceSettlementInFlight:
		t.inFlightCount++
	case workspaceSettlementUnconfirmed:
		t.unconfirmedCount++
	case workspaceSettlementUnattributed:
		t.unattributedCount++
	}
}

// measureWorkspaceSettlement resolves one order movement against the wallet
// records that actually applied the money. Attribution uses the recorded order
// identities only: the account, the operation, the stored adjustment code and
// the wallet user, never an amount or timestamp coincidence.
func measureWorkspaceSettlement(
	settlement billingSettlement,
	originals map[string]billingSettlement,
	history map[string]clients.Sub2APIBalanceHistoryEntry,
	userID int64,
	codeCounts map[string]int,
) workspaceSettlementMeasurement {
	kind := "debit"
	if settlement.kind == "refund" {
		kind = "business_refund"
	}
	if settlement.kind == "refund" {
		if settlement.relatedOperationID == "" {
			// An operator adjustment with no recorded Workspace order is out of
			// scope for a Workspace net-charge trend.
			return workspaceSettlementMeasurement{state: workspaceSettlementUnattributed}
		}
		original, found := originals[settlement.relatedOperationID]
		if !found || original.kind == "refund" {
			return workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed}
		}
	}
	if settlement.invalid || settlement.operationID == "" || settlement.amount <= 0 || settlement.code == "" || codeCounts[settlement.code] != 1 {
		return workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed}
	}
	if settlement.userID > 0 && settlement.userID != userID {
		return workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed}
	}
	entry, state, err := inspectWalletAdjustmentHistory(history, settlement.code, walletAdjustmentOperation{
		Kind: kind, Sub2APIUserID: userID, AmountUSDMicros: settlement.amount,
	})
	if err != nil {
		return workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed}
	}
	if state != "confirmed" {
		if settlement.pending {
			return workspaceSettlementMeasurement{state: workspaceSettlementInFlight}
		}
		return workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed}
	}
	if entry.UsedAt == nil || entry.UsedAt.IsZero() {
		return workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed}
	}
	return workspaceSettlementMeasurement{state: workspaceSettlementSettled, kind: settlement.kind, amount: settlement.amount, effectiveAt: entry.UsedAt.UTC()}
}

// refundOverflowOperations mirrors the owner reconciliation's refund limit: a
// refund that would push an order's confirmed refunds past its confirmed charge
// is not explainable, so that movement stays unconfirmed instead of inflating
// the trend. Explainable refunds on the same order still count, and the
// accumulation order matches billingReconciliationReport.
func refundOverflowOperations(settlements []billingSettlement) map[string]bool {
	ordered := append([]billingSettlement(nil), settlements...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].operationID != ordered[j].operationID {
			return ordered[i].operationID < ordered[j].operationID
		}
		return ordered[i].kind < ordered[j].kind
	})
	originals := map[string]billingSettlement{}
	refundTotals := map[string]int64{}
	overflow := map[string]bool{}
	for _, settlement := range ordered {
		if settlement.kind != "refund" {
			originals[settlement.operationID] = settlement
		}
	}
	for _, settlement := range ordered {
		if settlement.kind != "refund" || settlement.amount <= 0 {
			continue
		}
		original, found := originals[settlement.relatedOperationID]
		if !found {
			continue
		}
		if settlement.amount > original.amount-refundTotals[original.operationID] {
			overflow[settlement.operationID] = true
			continue
		}
		refundTotals[original.operationID] += settlement.amount
	}
	return overflow
}

// projectWorkspaceSettlementTrend reads only owner facts. It never settles,
// refunds, retries or writes anything, and it stays scoped to one account.
func (app *controlPlaneServer) projectWorkspaceSettlementTrend(
	ctx context.Context,
	service *controlplane.Service,
	accountID string,
	now time.Time,
) (workspaceSettlementTrend, error) {
	trend := newWorkspaceSettlementTrend(now)
	if accountID == "" {
		return trend, errMonthlyAccountUnmapped
	}
	userID, err := app.sub2APIUserID(ctx, accountID)
	if err != nil {
		return trend, err
	}
	settlements := make([]billingSettlement, 0)
	// The retired Workspace delete wrote its refund on the delete operation
	// instead of a business-refund adjustment, so its retained movement is read
	// from the owner row that holds it.
	for _, action := range []string{"workspace.launch", "workspace.launch.v2", "workspace.renewal", "gateway.wallet_adjustment.v1", workspaceDeleteLegacyAction} {
		rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{AccountID: accountID, Action: action})
		if err != nil {
			return trend, err
		}
		for _, row := range rows {
			projected := projectBillingSettlements(row)
			if action == workspaceDeleteLegacyAction {
				projected = projectWorkspaceDeleteLegacySettlements(row)
			}
			for _, settlement := range projected {
				// A retained row whose order names another account never
				// contributes here. It is reported as unresolved instead of
				// silently passing as this account's silence.
				if settlement.accountID != accountID {
					trend.record(workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed})
					continue
				}
				settlements = append(settlements, settlement)
			}
		}
	}
	codeCounts := make(map[string]int, len(settlements))
	originals := make(map[string]billingSettlement, len(settlements))
	codes := make([]string, 0, len(settlements))
	for _, settlement := range settlements {
		if settlement.code != "" {
			codeCounts[settlement.code]++
			if codeCounts[settlement.code] == 1 {
				codes = append(codes, settlement.code)
			}
		}
		if settlement.kind != "refund" {
			originals[settlement.operationID] = settlement
		}
	}
	history := map[string]clients.Sub2APIBalanceHistoryEntry{}
	if len(codes) > 0 {
		sort.Strings(codes)
		history, err = service.FinancialBalanceHistoryByCodes(ctx, userID, codes)
		if err != nil {
			return trend, err
		}
	}
	overflow := refundOverflowOperations(settlements)
	for _, settlement := range settlements {
		// A refund that would exceed its order's confirmed charge is not
		// explainable, so its own movement stays unconfirmed.
		if settlement.kind == "refund" && overflow[settlement.operationID] {
			trend.record(workspaceSettlementMeasurement{state: workspaceSettlementUnconfirmed})
			continue
		}
		trend.record(measureWorkspaceSettlement(settlement, originals, history, userID, codeCounts))
	}
	return trend, nil
}
