package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// workspaceLaunchReconcileCreate is test-only fixture input. The production
// fresh-create writer was retired with the POST fence, so retained recovery
// tests build their persisted obligations directly from this shape.
type workspaceLaunchReconcileCreate struct {
	OperationID            string
	RequestHash            string
	AccountID              string
	OwnerUserID            string
	Sub2APIUserID          int64
	WorkspaceKeyGroupID    int64
	WorkspaceID            string
	Name                   string
	PackageID              string
	StorageGB              int
	AutoRenew              bool
	PriceVersion           string
	TotalChargeUSDMicros   int64
	ProviderProfileRef     string
	PreflightBindingRef    string
	SpecDigest             string
	WorkspaceImageDigest   string
	PreChargeBalanceMicros int64
	ResourceBillingEnabled *bool
	Mode                   contracts.WorkspaceProvisioningMode
	CreatedAt              time.Time
}

// newWorkspaceLaunchFixtureOperation builds one valid retained Launch
// obligation for a test fixture. It is not a production creator: callers
// persist the returned row through the ordinary store.
func newWorkspaceLaunchFixtureOperation(command workspaceLaunchReconcileCreate) (workspaceLaunchReconcileOperation, error) {
	if command.CreatedAt.IsZero() {
		command.CreatedAt = time.Now().UTC()
	}
	mode := command.Mode
	if mode == "" {
		mode = contracts.WorkspaceProvisioningFull
	}
	if err := contracts.ValidateWorkspaceProvisioningMode(mode); err != nil {
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
	plan, err := contracts.WorkspaceProvisioningStages(mode)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
	if strings.TrimSpace(command.OperationID) == "" || strings.TrimSpace(command.RequestHash) == "" || strings.TrimSpace(command.AccountID) == "" ||
		strings.TrimSpace(command.OwnerUserID) == "" || strings.TrimSpace(command.WorkspaceID) == "" || strings.TrimSpace(command.Name) == "" ||
		command.Sub2APIUserID <= 0 || strings.TrimSpace(command.PackageID) == "" || command.StorageGB <= 0 ||
		strings.TrimSpace(command.PriceVersion) == "" || command.TotalChargeUSDMicros < 0 || command.ResourceBillingEnabled != nil && *command.ResourceBillingEnabled && command.TotalChargeUSDMicros <= 0 || strings.TrimSpace(command.ProviderProfileRef) == "" ||
		strings.TrimSpace(command.PreflightBindingRef) == "" || !workspaceProviderSpecDigestPattern.MatchString(command.SpecDigest) {
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
	if workspaceLaunchModeRequiresApplicationFacts(mode) && (command.WorkspaceKeyGroupID <= 0 || strings.TrimSpace(command.WorkspaceImageDigest) == "") {
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
	facts := map[string]any{
		"schemaVersion":             workspaceLaunchReconcileSchemaVersion,
		"version":                   1,
		"stage":                     string(plan[0]),
		"requestHash":               command.RequestHash,
		"accountId":                 command.AccountID,
		"ownerUserId":               command.OwnerUserID,
		"sub2apiUserId":             command.Sub2APIUserID,
		"workspaceId":               command.WorkspaceID,
		"name":                      command.Name,
		"packageId":                 command.PackageID,
		"sizeGb":                    command.StorageGB,
		"autoRenew":                 command.AutoRenew,
		"priceVersion":              command.PriceVersion,
		"totalChargeUsdMicros":      command.TotalChargeUSDMicros,
		"providerProfileRef":        command.ProviderProfileRef,
		"preflightBindingRef":       command.PreflightBindingRef,
		"specDigest":                command.SpecDigest,
		"sub2apiRedeemCode":         monthlyRedeemCode(monthlyEnvironment(), command.OperationID),
		"preChargeBalanceUsdMicros": command.PreChargeBalanceMicros,
		"resourceBillingEnabled":    command.ResourceBillingEnabled == nil || *command.ResourceBillingEnabled,
	}
	if mode == contracts.WorkspaceProvisioningResourceOnly {
		facts[workspaceLaunchProvisioningModeFact] = string(mode)
	} else {
		facts["workspaceKeyGroupId"] = command.WorkspaceKeyGroupID
		facts["workspaceImageDigest"] = command.WorkspaceImageDigest
	}
	attempts := make(map[contracts.Stage]workspaceLaunchStageAttempt, len(plan)-1)
	for _, stage := range plan[:len(plan)-1] {
		attempts[stage] = workspaceLaunchStageAttempt{Max: 1, MaxPendingReadbacks: workspaceLaunchLegacyV3AuthoritativeReadBudget}
	}
	facts["attempts"] = attempts
	facts["observations"] = map[contracts.Stage]workspaceLaunchStageObservation{}
	rawBytes, err := json.Marshal(facts)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	row := map[string]any{
		"id": command.OperationID, "operationId": command.OperationID, "accountId": command.AccountID,
		"workspaceId": command.WorkspaceID, "resourceId": command.WorkspaceID, "resourceKind": "workspace_launch",
		"action": workspaceLaunchAction, "status": string(contracts.StatusPending), "result": string(rawBytes),
		"createdAt": command.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	return decodeWorkspaceLaunchReconcileOperation(row)
}

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
		time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
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
