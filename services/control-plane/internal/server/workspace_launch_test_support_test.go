package server

import (
	"testing"
	"time"
)

// Lifecycle fixtures represent an active prepaid workspace at the caller's
// clock, using the same calendar-month boundary as the billing owner.
func workspaceFixtureCurrentBillingPeriod(now time.Time, anchorDay int) (time.Time, time.Time) {
	now = now.UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	start := nextBillingMonth(month.AddDate(0, -1, 0), anchorDay)
	if now.Before(start) {
		start = nextBillingMonth(month.AddDate(0, -2, 0), anchorDay)
	}
	return start, nextBillingMonth(start, anchorDay)
}

func TestWorkspaceFixtureBillingPeriodRemainsActiveWithoutWeakeningExpiry(t *testing.T) {
	for _, clock := range []time.Time{
		time.Date(2026, 9, 14, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2031, 2, 28, 12, 0, 0, 0, time.UTC),
	} {
		for _, anchor := range []int{12, 15, 31} {
			start, end := workspaceFixtureCurrentBillingPeriod(clock, anchor)
			if start.After(clock) || !clock.Before(end) || !nextBillingMonth(start, anchor).Equal(end) {
				t.Fatalf("clock=%s anchor=%d period=%s..%s", clock, anchor, start, end)
			}
			workspace := map[string]any{"state": "running", "resourceBillingEnabled": true, "paidThrough": end.Format(time.RFC3339Nano)}
			if !workspaceApplicationEntitlementOpen(workspace, clock) || workspaceApplicationEntitlementOpen(workspace, end) {
				t.Fatal("active fixture must still expire at its paid boundary")
			}
		}
	}
}

func workspaceGatewayTestRow(row map[string]any) map[string]any {
	billing := canonicalWorkspaceRenewalRow(false)
	billing["id"], billing["accountId"], billing["ownerAccountId"], billing["ownerUserId"] = "ws-alpha", "acct-alpha", "acct-alpha", "usr-alpha"
	billing["currentComputeAllocationId"], billing["computeAllocationId"], billing["storageId"] = "compute-alpha", "compute-alpha", "storage-alpha"
	billing["periodStart"], billing["paidThrough"], billing["nextRenewalAt"], billing["billingAnchorDay"] = "2098-12-01T00:00:00Z", "2099-01-01T00:00:00Z", "2098-12-31T00:00:00Z", int64(1)
	for key, value := range row {
		billing[key] = value
	}
	return billing
}

type workspaceBillingChildIdentity struct {
	AccountID, OwnerUserID, WorkspaceID, PackageID, ComputeID, StorageID string
	StorageGB                                                            int64
}

func workspaceBillingStateFromChildren(compute, storage map[string]any, identity workspaceBillingChildIdentity) (map[string]any, string) {
	quote, err := workspacePricingPreview(defaultPricingCatalog(), map[string]any{"packageId": identity.PackageID, "sizeGb": identity.StorageGB})
	if err != nil {
		return nil, "workspace_launch_billing_price_mismatch"
	}
	computePrice, computeOK := requiredPositiveInteger(mapField(quote, "compute"), "chargeUsdMicros")
	storagePrice, storageOK := requiredPositiveInteger(mapField(quote, "storage"), "chargeUsdMicros")
	total, totalOK := checkedAddInt64(computePrice, storagePrice)
	periodStart, startErr := time.Parse(time.RFC3339, stringValue(compute["periodStart"]))
	paidThrough, paidErr := time.Parse(time.RFC3339, stringValue(compute["paidThrough"]))
	if !computeOK || !storageOK || !totalOK || startErr != nil || paidErr != nil {
		return nil, "workspace_launch_billing_state_invalid"
	}
	return map[string]any{
		"ownerUserId": identity.OwnerUserID, "currentComputeAllocationId": identity.ComputeID,
		"autoRenew": false, "authorizedBy": "", "authorizedAt": "", "packageId": identity.PackageID, "storageGb": identity.StorageGB,
		"priceVersion": pricingCatalogVersion, "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
		"computeUsdMicros": computePrice, "storageUsdMicros": storagePrice, "totalUsdMicros": total,
		"periodStart": periodStart.UTC().Format(time.RFC3339Nano), "paidThrough": paidThrough.UTC().Format(time.RFC3339Nano),
		"nextRenewalAt": paidThrough.UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano), "billingAnchorDay": int64(periodStart.Day()),
		"renewalStatus": "active", "computeAllocationId": identity.ComputeID, "storageId": identity.StorageID,
	}, ""
}

func stripWorkspaceLaunchResourceBilling(row map[string]any) {
	for _, key := range []string{
		"billingOperationId", "billingOperationStartedAt", "billingStatus", "sub2apiRedeemCode", "sub2apiRefundCode",
		"priceVersion", "currency", "billingUnit", "pricingVersion", "priceSnapshot", "monthlyPriceCnyCents", "chargeUsdMicros", "postChargeBalanceUsdMicros",
		"postChargeBalanceKnown", "periodStart", "paidThrough", "billingAnchorDay", "lastReceiptId", "lastBillingError",
	} {
		delete(row, key)
	}
}
