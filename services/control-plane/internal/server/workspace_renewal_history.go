package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// This file holds the retained readers of the withdrawn renewal execution
// engine. The engine that charged, renewed, expired and refunded a Workspace
// is gone; the records it wrote stay readable here for the billing
// reconciliation, the settlement projection, the operator observations and the
// recovery state that still name them.

var errWorkspaceRenewalResourcesReclaimed = errors.New("workspace_renewal_resources_reclaimed")

type workspaceRenewalOperation struct {
	ExpiryRuntimePower          *contracts.WorkspaceRuntimePowerResult  `json:"expiryRuntimePower,omitempty"`
	ResumeRuntimePower          *contracts.WorkspaceRuntimePowerResult  `json:"resumeRuntimePower,omitempty"`
	ExpiryApplicationPower      *workspaceApplicationLifecycleOperation `json:"expiryApplicationPower,omitempty"`
	ResumeApplicationPower      *workspaceApplicationLifecycleOperation `json:"resumeApplicationPower,omitempty"`
	ID                          string                                  `json:"-"`
	Status                      string                                  `json:"-"`
	CreatedAt                   string                                  `json:"-"`
	PersistedResult             string                                  `json:"-"`
	RequestHash                 string                                  `json:"requestHash"`
	Phase                       string                                  `json:"phase"`
	AccountID                   string                                  `json:"accountId"`
	OwnerUserID                 string                                  `json:"ownerUserId"`
	WorkspaceID                 string                                  `json:"workspaceId"`
	PackageID                   string                                  `json:"packageId"`
	StorageGB                   int64                                   `json:"storageGb"`
	ComputeID                   string                                  `json:"computeAllocationId"`
	StorageID                   string                                  `json:"storageId"`
	PriceVersion                string                                  `json:"priceVersion"`
	ComputeUSDMicros            int64                                   `json:"computeUsdMicros"`
	StorageUSDMicros            int64                                   `json:"storageUsdMicros"`
	TotalUSDMicros              int64                                   `json:"totalUsdMicros"`
	PeriodStart                 string                                  `json:"periodStart"`
	PaidThrough                 string                                  `json:"paidThrough"`
	RenewedThrough              string                                  `json:"renewedThrough"`
	RedeemCode                  string                                  `json:"sub2apiRedeemCode"`
	RefundCode                  string                                  `json:"sub2apiRefundCode"`
	ComputePreflightConfirmed   bool                                    `json:"computePreflightConfirmed,omitempty"`
	StoragePreflightConfirmed   bool                                    `json:"storagePreflightConfirmed,omitempty"`
	ChargeAttempted             bool                                    `json:"chargeAttempted,omitempty"`
	ChargeConfirmation          map[string]any                          `json:"chargeConfirmation,omitempty"`
	PreChargeBalanceUSDMicros   int64                                   `json:"preChargeBalanceUsdMicros,omitempty"`
	PostChargeBalanceUSDMicros  int64                                   `json:"postChargeBalanceUsdMicros,omitempty"`
	PostChargeBalanceKnown      bool                                    `json:"postChargeBalanceKnown,omitempty"`
	RefundAttempted             bool                                    `json:"refundAttempted,omitempty"`
	RefundConfirmation          map[string]any                          `json:"refundConfirmation,omitempty"`
	RefundReason                string                                  `json:"refundReason,omitempty"`
	RefundReceiptID             string                                  `json:"refundReceiptId,omitempty"`
	ComputeRenewal              map[string]any                          `json:"computeRenewal,omitempty"`
	StorageRenewal              map[string]any                          `json:"storageRenewal,omitempty"`
	ComputeReadback             map[string]any                          `json:"computeReadback,omitempty"`
	StorageReadback             map[string]any                          `json:"storageReadback,omitempty"`
	EntitlementCommitted        bool                                    `json:"entitlementCommitted,omitempty"`
	ReceiptID                   string                                  `json:"receiptId,omitempty"`
	ErrorCode                   string                                  `json:"errorCode,omitempty"`
	PriorStatus                 string                                  `json:"priorStatus,omitempty"`
	PriorErrorCode              string                                  `json:"priorErrorCode,omitempty"`
	ExpiryStatus                string                                  `json:"expiryStatus,omitempty"`
	ExpiryPhase                 string                                  `json:"expiryPhase,omitempty"`
	ExpiryErrorCode             string                                  `json:"expiryErrorCode,omitempty"`
	ExpiryReceiptID             string                                  `json:"expiryReceiptId,omitempty"`
	ExpiryPeriodStart           string                                  `json:"expiryPeriodStart,omitempty"`
	ExpiryPaidThrough           string                                  `json:"expiryPaidThrough,omitempty"`
	LeaseToken                  string                                  `json:"leaseToken,omitempty"`
	LeaseExpiresAt              string                                  `json:"leaseExpiresAt,omitempty"`
	ReviewResolutionKey         string                                  `json:"reviewResolutionKey,omitempty"`
	ReviewResolutionFingerprint string                                  `json:"reviewResolutionFingerprint,omitempty"`
	ReviewResolutionDecision    string                                  `json:"reviewResolutionDecision,omitempty"`
	ReviewResolutionEvidenceRef string                                  `json:"reviewResolutionEvidenceRef,omitempty"`
	ReviewResolutionReviewer    string                                  `json:"reviewResolutionReviewer,omitempty"`
	ReviewResolutionPhase       string                                  `json:"reviewResolutionPhase,omitempty"`
	ReviewResolutionResolvedAt  string                                  `json:"reviewResolutionResolvedAt,omitempty"`
	ReviewResolutionResult      map[string]any                          `json:"reviewResolutionResult,omitempty"`
}

func workspaceRenewalOperationID(workspaceID string, paidThrough time.Time) string {
	return "workspace-renewal-" + stableID(workspaceID, paidThrough.UTC().Format(time.RFC3339Nano))[:18]
}

func newWorkspaceRenewalOperation(workspace map[string]any, now time.Time) (workspaceRenewalOperation, error) {
	paidThrough, err := time.Parse(time.RFC3339, stringValue(workspace["paidThrough"]))
	if err != nil {
		return workspaceRenewalOperation{}, errInvalidWorkspaceBillingState
	}
	anchor := int(numberField(workspace, "billingAnchorDay", float64(paidThrough.Day())))
	renewedThrough := nextBillingMonth(paidThrough, anchor)
	id := workspaceRenewalOperationID(stringValue(workspace["id"]), paidThrough)
	operation := workspaceRenewalOperation{
		ID: id, Status: "claimed", CreatedAt: now.UTC().Format(time.RFC3339Nano), Phase: "preflight_compute",
		AccountID: stringValue(workspace["accountId"]), OwnerUserID: stringValue(workspace["ownerUserId"]), WorkspaceID: stringValue(workspace["id"]),
		PackageID: stringValue(workspace["packageId"]), StorageGB: int64(numberField(workspace, "storageGb", 0)),
		ComputeID: stringValue(workspace["computeAllocationId"]), StorageID: stringValue(workspace["storageId"]), PriceVersion: stringValue(workspace["priceVersion"]),
		ComputeUSDMicros: int64(numberField(workspace, "computeUsdMicros", 0)), StorageUSDMicros: int64(numberField(workspace, "storageUsdMicros", 0)), TotalUSDMicros: int64(numberField(workspace, "totalUsdMicros", 0)),
		PeriodStart: stringValue(workspace["periodStart"]), PaidThrough: paidThrough.UTC().Format(time.RFC3339Nano), RenewedThrough: renewedThrough.UTC().Format(time.RFC3339Nano),
		RedeemCode: monthlyRedeemCode(monthlyEnvironment(), id), RefundCode: monthlyRefundCode(monthlyEnvironment(), id),
	}
	operation.RequestHash = stableID(
		"workspace-renewal-v1", operation.AccountID, operation.OwnerUserID, operation.WorkspaceID, operation.PackageID,
		operation.ComputeID, operation.StorageID, operation.PriceVersion, strconv.FormatInt(operation.StorageGB, 10),
		strconv.FormatInt(operation.ComputeUSDMicros, 10), strconv.FormatInt(operation.StorageUSDMicros, 10), strconv.FormatInt(operation.TotalUSDMicros, 10), operation.PeriodStart, operation.PaidThrough,
	)
	if operation.AccountID == "" || operation.OwnerUserID == "" || operation.WorkspaceID == "" || operation.PackageID == "" || operation.ComputeID == "" || operation.StorageID == "" ||
		operation.StorageGB <= 0 || operation.ComputeUSDMicros <= 0 || operation.StorageUSDMicros <= 0 || operation.TotalUSDMicros <= 0 {
		return workspaceRenewalOperation{}, errInvalidWorkspaceBillingState
	}
	return operation, nil
}

func encodeWorkspaceRenewalOperation(operation workspaceRenewalOperation) string {
	payload, _ := json.Marshal(operation)
	return string(payload)
}

func decodeWorkspaceRenewalOperation(row map[string]any) (workspaceRenewalOperation, error) {
	var operation workspaceRenewalOperation
	result := stringValue(row["result"])
	if json.Unmarshal([]byte(result), &operation) != nil {
		return workspaceRenewalOperation{}, errors.New("invalid_workspace_renewal_operation")
	}
	operation.ID = firstNonEmpty(stringValue(row["operationId"]), stringValue(row["id"]))
	operation.Status, operation.CreatedAt, operation.PersistedResult = stringValue(row["status"]), stringValue(row["createdAt"]), result
	if operation.ID == "" || operation.Status == "" || operation.RequestHash == "" || operation.AccountID == "" || operation.WorkspaceID == "" || operation.PaidThrough == "" {
		return workspaceRenewalOperation{}, errors.New("invalid_workspace_renewal_operation")
	}
	if !validWorkspaceApplicationLifecycle(operation.ExpiryApplicationPower, operation.AccountID, operation.WorkspaceID, operation.ID, "suspended") ||
		!validWorkspaceApplicationLifecycle(operation.ResumeApplicationPower, operation.AccountID, operation.WorkspaceID, operation.ID, "running") {
		return workspaceRenewalOperation{}, errors.New("invalid_workspace_renewal_operation")
	}
	for field, want := range map[string]string{
		"accountId": operation.AccountID, "workspaceId": operation.WorkspaceID, "resourceId": operation.WorkspaceID,
		"resourceKind": "workspace_renewal", "action": "workspace.renewal",
	} {
		if got := stringValue(row[field]); got != "" && got != want {
			return workspaceRenewalOperation{}, errors.New("invalid_workspace_renewal_operation")
		}
	}
	return operation, nil
}

// workspaceRenewalOperationRow renders the persisted shape of one renewal
// operation. The execution engine that wrote it is withdrawn, so this is the
// canonical record the retained readers decode.
func workspaceRenewalOperationRow(operation workspaceRenewalOperation) map[string]any {
	return map[string]any{
		"id": operation.ID, "operationId": operation.ID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceId": operation.WorkspaceID, "resourceKind": "workspace_renewal", "action": "workspace.renewal", "status": operation.Status,
		"result": encodeWorkspaceRenewalOperation(operation), "computeAllocationId": operation.ComputeID, "storageId": operation.StorageID,
		"periodStart": operation.PaidThrough, "createdAt": operation.CreatedAt,
	}
}

func workspaceRenewalFinancialRecoveryRequired(operation workspaceRenewalOperation) bool {
	return operation.Status == "manual_review" || operation.ChargeAttempted || operation.ChargeConfirmation != nil ||
		operation.RefundAttempted || operation.RefundConfirmation != nil || operation.EntitlementCommitted
}

// terminalWorkspaceRenewal reports whether a renewal operation reached a
// financial state that no further execution has to change. The delete fence
// and the retained settlement projection read it to decide whether an
// unfinished renewal still holds the Workspace.
func terminalWorkspaceRenewal(operation workspaceRenewalOperation) bool {
	financialTerminal := false
	switch operation.Status {
	case "active", "cancelled", "manual_review":
		financialTerminal = true
	case "refunded":
		financialTerminal = operation.Phase == "complete"
	case "expired_unpaid":
		financialTerminal = operation.ExpiryStatus == "expired_unpaid" || operation.Phase == "complete"
	}
	return financialTerminal && (operation.ExpiryStatus == "" || operation.ExpiryPhase == "complete")
}

func workspaceRenewalProviderFactInput(operation workspaceRenewalOperation, resourceType string) clients.ProviderFactInput {
	resourceID := operation.ComputeID
	if resourceType == "storage" {
		resourceID = operation.StorageID
	}
	return clients.ProviderFactInput{
		AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID,
		ResourceType: resourceType, ResourceID: resourceID,
	}
}

func (app *controlPlaneServer) workspaceRenewalResources(ctx context.Context, service *controlplane.Service, operation workspaceRenewalOperation) (map[string]any, map[string]any, clients.ProviderFact, clients.ProviderFact, error) {
	compute, computeOK := app.getCompute(operation.ComputeID)
	storage, storageOK := app.getStorage(operation.StorageID)
	if !computeOK || !storageOK || stringValue(compute["accountId"]) != operation.AccountID || stringValue(storage["accountId"]) != operation.AccountID ||
		stringValue(compute["workspaceId"]) != operation.WorkspaceID || stringValue(storage["workspaceId"]) != operation.WorkspaceID ||
		stringValue(compute["providerResourceId"]) == "" || stringValue(storage["providerResourceId"]) == "" ||
		stringValue(compute["packageId"]) != operation.PackageID || int64(numberField(storage, "sizeGb", 0)) != operation.StorageGB ||
		stringValue(storage["computeAllocationId"]) != operation.ComputeID {
		return nil, nil, clients.ProviderFact{}, clients.ProviderFact{}, errors.New("workspace_renewal_resource_identity_mismatch")
	}
	paidThrough, err := time.Parse(time.RFC3339, operation.PaidThrough)
	if err != nil {
		return nil, nil, clients.ProviderFact{}, clients.ProviderFact{}, errors.New("workspace_renewal_provider_truth_invalid")
	}
	inputs := []clients.ProviderFactInput{
		workspaceRenewalProviderFactInput(operation, "compute"),
		workspaceRenewalProviderFactInput(operation, "storage"),
	}
	facts, err := readProviderFacts(ctx, service, inputs)
	if err != nil {
		return nil, nil, clients.ProviderFact{}, clients.ProviderFact{}, errors.New("workspace_renewal_provider_truth_invalid")
	}
	computeFact, storageFact := facts[providerFactKey(inputs[0])], facts[providerFactKey(inputs[1])]
	if providerFactConfirmedAbsent(inputs[0], computeFact) || providerFactConfirmedAbsent(inputs[1], storageFact) {
		return nil, nil, computeFact, storageFact, errWorkspaceRenewalResourcesReclaimed
	}
	if !providerFactCovers(inputs[0], computeFact, stringValue(compute["providerResourceId"]), paidThrough) ||
		!providerFactCovers(inputs[1], storageFact, stringValue(storage["providerResourceId"]), paidThrough) {
		return nil, nil, clients.ProviderFact{}, clients.ProviderFact{}, errors.New("workspace_renewal_provider_truth_invalid")
	}
	return compute, storage, computeFact, storageFact, nil
}

func (app *controlPlaneServer) workspaceRenewalCurrentEntitlement(ctx context.Context, workspace map[string]any) bool {
	periodStart, err := time.Parse(time.RFC3339, stringValue(workspace["periodStart"]))
	if err != nil {
		return false
	}
	operationID := workspaceRenewalOperationID(stringValue(workspace["id"]), periodStart)
	row, found, err := app.tables.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		return false
	}
	operation, err := decodeWorkspaceRenewalOperation(row)
	if err != nil || operation.ID != operationID || !operation.EntitlementCommitted || operation.Status != "active" && (operation.Status != "verifying" || operation.Phase != "receipt") ||
		operation.AccountID != stringValue(workspace["accountId"]) || operation.WorkspaceID != stringValue(workspace["id"]) || operation.OwnerUserID != stringValue(workspace["ownerUserId"]) ||
		operation.ComputeID != stringValue(workspace["computeAllocationId"]) || operation.ComputeID != stringValue(workspace["currentComputeAllocationId"]) || operation.StorageID != stringValue(workspace["storageId"]) ||
		operation.PaidThrough != stringValue(workspace["periodStart"]) || operation.RenewedThrough != stringValue(workspace["paidThrough"]) {
		return false
	}
	userID, err := app.sub2APIUserID(ctx, operation.AccountID)
	return err == nil && monthlyChargeConfirmationMatches(operation.ChargeConfirmation, operation.RedeemCode, userID, operation.TotalUSDMicros)
}

// workspaceRenewalReceiptCost renders the money facts one renewal receipt
// carries. The retained settlement projection compares the persisted receipt
// against this shape, so it stays independently of the withdrawn engine.
func workspaceRenewalReceiptCost(operation workspaceRenewalOperation, charged bool, userID int64) map[string]any {
	periodStart, paidThrough := operation.PeriodStart, operation.PaidThrough
	if charged {
		periodStart, paidThrough = operation.PaidThrough, operation.RenewedThrough
	}
	cost := map[string]any{
		"priceVersion": operation.PriceVersion, "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
		"totalUsdMicros": operation.TotalUSDMicros, "periodStart": periodStart, "paidThrough": paidThrough,
		"resourceType": "workspace", "resourceId": operation.WorkspaceID,
		"components": map[string]any{
			"compute": map[string]any{"resourceType": "compute", "resourceId": operation.ComputeID, "chargeUsdMicros": operation.ComputeUSDMicros},
			"storage": map[string]any{"resourceType": "storage", "resourceId": operation.StorageID, "sizeGb": operation.StorageGB, "chargeUsdMicros": operation.StorageUSDMicros},
		},
	}
	if charged {
		cost["sub2apiUserId"], cost["sub2apiRedeemCode"] = userID, operation.RedeemCode
		if operation.PostChargeBalanceKnown {
			cost["postChargeBalanceUsdMicros"] = operation.PostChargeBalanceUSDMicros
		}
	}
	return cost
}

func workspaceRenewalReceiptInput(operation workspaceRenewalOperation, userID int64) clients.ReceiptInput {
	input := clients.ReceiptInput{
		Type: "billing.workspace_renewed.v1", Status: "completed", Surface: "control_plane", AccountID: operation.AccountID,
		WorkspaceID: operation.WorkspaceID, RequestID: operation.ID,
		Execution: map[string]any{
			"resourceType": "workspace", "resourceId": operation.WorkspaceID, "computeAllocationId": operation.ComputeID, "storageId": operation.StorageID,
			"computeReadback": operation.ComputeReadback, "storageReadback": operation.StorageReadback,
		},
		Cost:  workspaceRenewalReceiptCost(operation, true, userID),
		Owner: map[string]any{"accountId": operation.AccountID, "workspaceId": operation.WorkspaceID, "ownerUserId": operation.OwnerUserID},
	}
	if operation.ReviewResolutionKey != "" {
		input.InputRefs = map[string]any{"evidenceRef": operation.ReviewResolutionEvidenceRef}
		input.ReviewerChecks = map[string]any{
			"decision": operation.ReviewResolutionDecision, "reviewer": operation.ReviewResolutionReviewer,
			"evidenceRef": operation.ReviewResolutionEvidenceRef, "resolvedAt": operation.ReviewResolutionResolvedAt,
		}
	}
	return input
}

func workspaceRefundReceiptInput(operation workspaceRenewalOperation, userID int64) clients.ReceiptInput {
	cost := workspaceRenewalReceiptCost(operation, false, 0)
	cost["periodStart"], cost["paidThrough"] = operation.PaidThrough, operation.RenewedThrough
	cost["sub2apiUserId"], cost["sub2apiRedeemCode"] = userID, operation.RedeemCode
	cost["sub2apiRefundCode"], cost["refundUsdMicros"] = operation.RefundCode, operation.TotalUSDMicros
	return clients.ReceiptInput{
		Type: "billing.workspace_refunded.v1", Status: "completed", Surface: "control_plane", AccountID: operation.AccountID,
		WorkspaceID: operation.WorkspaceID, RequestID: operation.ID,
		Execution: map[string]any{
			"resourceType": "workspace", "resourceId": operation.WorkspaceID, "reason": operation.RefundReason,
			"computeAllocationId": operation.ComputeID, "storageId": operation.StorageID, "refundConfirmation": operation.RefundConfirmation,
		},
		Cost:  cost,
		Owner: map[string]any{"accountId": operation.AccountID, "workspaceId": operation.WorkspaceID, "ownerUserId": operation.OwnerUserID},
	}
}

type workspaceRenewalRecovery struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

func (app *controlPlaneServer) workspaceRenewalRecoveryState(ctx context.Context, service *controlplane.Service, workspace map[string]any, operations []map[string]any, now time.Time) workspaceRenewalRecovery {
	result := func(state, reason string) workspaceRenewalRecovery {
		return workspaceRenewalRecovery{State: state, Reason: reason}
	}
	if stringValue(workspace["state"]) == "data_deleted" {
		return result("reclaimed", errWorkspaceRenewalResourcesReclaimed.Error())
	}
	if workspace["resourceBillingEnabled"] == false || stringValue(workspace["renewalStatus"]) == "not_applicable" {
		return result("not_required", "workspace_billing_not_applicable")
	}
	paidThrough, err := time.Parse(time.RFC3339, stringValue(workspace["paidThrough"]))
	if err != nil {
		return result("unavailable", "workspace_renewal_identity_mismatch")
	}
	if now.Before(paidThrough) {
		return result("not_required", "workspace_paid_period_active")
	}
	operation, err := newWorkspaceRenewalOperation(workspace, now)
	if err != nil {
		return result("unavailable", "workspace_renewal_identity_mismatch")
	}
	for _, row := range operations {
		if workspaceDeleteBlocksRenewal(row) {
			return result("unavailable", "workspace_delete_in_progress")
		}
		if stringValue(row["action"]) != "workspace.renewal" {
			continue
		}
		current, decodeErr := decodeWorkspaceRenewalOperation(row)
		if decodeErr != nil {
			return result("unavailable", "workspace_renewal_identity_mismatch")
		}
		if current.PaidThrough != operation.PaidThrough {
			continue
		}
		if current.ErrorCode == errWorkspaceRenewalResourcesReclaimed.Error() {
			return result("reclaimed", current.ErrorCode)
		}
		if current.Status == "manual_review" {
			return result("unavailable", "workspace_renewal_manual_review")
		}
		if current.RefundAttempted || current.RefundConfirmation != nil {
			return result("unavailable", "workspace_renewal_manual_review")
		}
		if workspaceRenewalFinancialRecoveryRequired(current) {
			return result("pending", "workspace_renewal_pending")
		}
	}
	renewedThrough, _ := time.Parse(time.RFC3339, operation.RenewedThrough)
	if !now.Before(renewedThrough) {
		return result("unavailable", "workspace_renewal_period_elapsed")
	}
	if stringValue(workspace["currentComputeAllocationId"]) != operation.ComputeID || stringValue(workspace["currentAttachmentId"]) == "" {
		return result("unavailable", "workspace_renewal_identity_mismatch")
	}
	if _, _, _, _, err := app.workspaceRenewalResources(ctx, service, operation); err != nil {
		if errors.Is(err, errWorkspaceRenewalResourcesReclaimed) {
			return result("reclaimed", err.Error())
		}
		return result("unavailable", "workspace_renewal_provider_truth_unavailable")
	}
	if err := app.workspaceRenewalRuntimeRecoveryEligible(ctx, service, operation); err != nil {
		if errors.Is(err, errWorkspaceRenewalResourcesReclaimed) {
			return result("reclaimed", err.Error())
		}
		return result("unavailable", "workspace_renewal_provider_truth_unavailable")
	}
	userID, err := app.sub2APIUserID(ctx, operation.AccountID)
	if err != nil {
		return result("unavailable", "workspace_renewal_account_unavailable")
	}
	balance, err := service.Sub2APIBalance(ctx, userID)
	if err != nil || balance.UserID != userID || balance.Status != "active" || balance.USDMicros < 0 {
		return result("unavailable", "workspace_renewal_account_unavailable")
	}
	if balance.USDMicros < operation.TotalUSDMicros {
		return result("unavailable", "workspace_renewal_insufficient_balance")
	}
	authorizedAt, _ := time.Parse(time.RFC3339Nano, stringValue(workspace["authorizedAt"]))
	if workspace["autoRenew"] == true && !authorizedAt.Before(paidThrough) {
		return result("pending", "workspace_renewal_pending")
	}
	return result("recoverable", "workspace_renewal_authorization_required")
}

func (app *controlPlaneServer) workspaceRenewalRuntimePowerInput(ctx context.Context, operation workspaceRenewalOperation, desired string) (contracts.WorkspaceRuntimePowerInput, error) {
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: operation.WorkspaceID, Action: workspaceLaunchAction})
	if err != nil {
		return contracts.WorkspaceRuntimePowerInput{}, err
	}
	if len(rows) != 1 {
		return contracts.WorkspaceRuntimePowerInput{}, errors.New("workspace_runtime_identity_unavailable")
	}
	launch, err := decodeWorkspaceLaunchReconcileOperation(rows[0])
	if err != nil || launch.Status != contracts.StatusSucceeded || launch.Stage != contracts.StageSucceeded || launch.stringFact("accountId") != operation.AccountID || launch.stringFact("workspaceId") != operation.WorkspaceID || launch.stringFact("computeAllocationId") != operation.ComputeID || launch.stringFact("storageId") != operation.StorageID || launch.stringFact("runtimeId") == "" || launch.stringFact("receiptId") == "" {
		return contracts.WorkspaceRuntimePowerInput{}, errors.New("workspace_runtime_identity_mismatch")
	}
	workspace, ok := app.getWorkspace(operation.WorkspaceID)
	if !ok || stringValue(workspace["runtimeId"]) != launch.stringFact("runtimeId") {
		return contracts.WorkspaceRuntimePowerInput{}, errors.New("workspace_runtime_identity_mismatch")
	}
	paidThrough := operation.ExpiryPaidThrough
	if desired == "running" {
		paidThrough = operation.RenewedThrough
	}
	runtimeBindingRef := launch.stringFact("runtimeBindingRef")
	if runtimeBindingRef == "" {
		return contracts.WorkspaceRuntimePowerInput{}, errors.New("workspace_runtime_identity_mismatch")
	}
	return contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID, RuntimeID: launch.stringFact("runtimeId"), RuntimeOperationID: runtimeBindingRef, PaidThrough: paidThrough, DesiredState: desired, IdempotencyKey: operation.ID + ":runtime:" + desired}, nil
}

func (app *controlPlaneServer) workspaceRenewalRuntimeRecoveryEligible(ctx context.Context, service *controlplane.Service, operation workspaceRenewalOperation) error {
	workspace, found, err := app.tables.GetWorkspace(ctx, operation.WorkspaceID)
	if err != nil || !found {
		return errors.New("workspace_runtime_identity_unavailable")
	}
	applications, err := app.workspaceApplicationLifecycleInventory(ctx, service, workspace, "running", operation.ID)
	if err != nil {
		return err
	}
	if len(applications.Runtimes) > 0 {
		for _, target := range applications.Runtimes {
			result, err := service.ReadWorkspaceApplicationRuntimeLifecycle(ctx, target.Input)
			if err != nil {
				return err
			}
			if result.RuntimeID != target.Input.RuntimeID || result.WorkspaceID != target.Input.WorkspaceID {
				return errors.New("workspace_application_lifecycle_readback_invalid")
			}
			if result.State == "absent" {
				return errWorkspaceRenewalResourcesReclaimed
			}
			if result.State != "running" && result.State != "suspended" {
				return errors.New("workspace_application_lifecycle_pending")
			}
		}
		return nil
	}
	if mode, err := app.workspaceLaunchProvisioningMode(ctx, operation.WorkspaceID); err != nil {
		return err
	} else if mode == contracts.WorkspaceProvisioningResourceOnly {
		// A resource-only Workspace owns no runtime whose recovery could be
		// verified; resource truth is checked by workspaceRenewalResources.
		return nil
	}
	input, err := app.workspaceRenewalRuntimePowerInput(ctx, operation, "running")
	if err != nil {
		return err
	}
	result, err := service.ReadWorkspaceRuntimePower(ctx, input)
	if err != nil {
		return err
	}
	if result.SchemaVersion != 1 || result.Binding != input {
		return errors.New("workspace_runtime_power_readback_invalid")
	}
	if result.State == "absent" {
		return errWorkspaceRenewalResourcesReclaimed
	}
	if result.State != "running" && result.State != "suspended" {
		return errors.New("workspace_runtime_power_pending")
	}
	return nil
}
