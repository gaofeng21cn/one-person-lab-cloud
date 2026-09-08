package server

import (
	"encoding/json"

	"opl-cloud/services/control-plane/internal/clients"
)

// This is the retained schema-2 financial record written before 34f9b732.
// It permits historical readback only, never execution by the current Launch reducer.
type workspaceLaunchBillingHistory struct {
	walletRefundChargeFacts
	SchemaVersion           int            `json:"schemaVersion"`
	OwnerUserID             string         `json:"ownerUserId"`
	WorkspaceID             string         `json:"workspaceId"`
	PackageID               string         `json:"packageId"`
	StorageGB               int64          `json:"sizeGb"`
	PriceVersion            string         `json:"priceVersion"`
	PeriodStart             string         `json:"periodStart"`
	PaidThrough             string         `json:"paidThrough"`
	ComputeID               string         `json:"computeAllocationId"`
	StorageID               string         `json:"storageId"`
	AttachmentID            string         `json:"attachmentId"`
	WorkspaceAPIKeyID       int64          `json:"workspaceApiKeyId"`
	WorkspaceKeyFingerprint string         `json:"workspaceKeyFingerprint"`
	RuntimeID               string         `json:"runtimeId"`
	RuntimeServiceName      string         `json:"runtimeServiceName"`
	ChargeAttempted         bool           `json:"chargeAttempted"`
	ReceiptID               string         `json:"receiptId"`
	RefundCode              string         `json:"sub2apiRefundCode"`
	RefundAttempted         bool           `json:"refundAttempted"`
	RefundConfirmation      map[string]any `json:"refundConfirmation"`
	RefundReason            string         `json:"refundReason"`
	RefundReceiptID         string         `json:"refundReceiptId"`
}

func historicalWorkspacePurchaseSettlements(row map[string]any, base billingSettlement, history workspaceLaunchBillingHistory) []billingSettlement {
	base.accountID, base.workspaceID = history.AccountID, history.WorkspaceID
	base.amount, base.code, base.receiptID = history.TotalChargeUSDMicros, history.RedeemCode, history.ReceiptID
	status := stringValue(row["status"])
	// Retired executors cannot make progress; incomplete historical evidence
	// requires review instead of being hidden as normal pending work.
	if !history.ChargeAttempted && history.ChargeConfirmation == nil && status != "succeeded" && status != "refunded" && !history.RefundAttempted {
		return nil
	}
	if history.ChargeConfirmation != nil {
		base.userID = history.ChargeConfirmation.UserID
	}
	base.invalid = history.AccountID == "" || history.AccountID != stringValue(row["accountId"]) || history.WorkspaceID == "" || history.WorkspaceID != stringValue(row["workspaceId"]) || history.OwnerUserID == "" || history.SchemaVersion != 2
	if !base.pending {
		charge := history.ChargeConfirmation
		base.invalid = base.invalid || charge == nil || charge.Code != base.code || charge.AmountUSDMicros != base.amount || charge.UserID <= 0 || charge.Status != "used" || !validSettlementPeriod(history.PeriodStart, history.PaidThrough)
	}
	catalog, found := pricingCatalogByVersion(history.PriceVersion)
	quote, err := workspacePricingPreview(catalog, map[string]any{"packageId": history.PackageID, "sizeGb": history.StorageGB})
	compute, computeOK := requiredPositiveInteger(mapField(quote, "compute"), "chargeUsdMicros")
	storage, storageOK := requiredPositiveInteger(mapField(quote, "storage"), "chargeUsdMicros")
	total, totalOK := requiredPositiveInteger(quote, "totalChargeUsdMicros")
	base.invalid = base.invalid || !found || err != nil || !computeOK || !storageOK || !totalOK || total != base.amount
	base.refundable = !base.invalid && status == "succeeded"
	input := clients.ReceiptInput{
		Type: "billing.workspace_purchased.v1", Status: "completed", Surface: "control_plane", AccountID: base.accountID, WorkspaceID: base.workspaceID, RequestID: base.operationID,
		Execution: map[string]any{"resourceType": "workspace", "resourceId": base.workspaceID, "computeAllocationId": history.ComputeID, "storageId": history.StorageID, "attachmentId": history.AttachmentID, "workspaceApiKeyId": history.WorkspaceAPIKeyID, "workspaceKeyFingerprint": history.WorkspaceKeyFingerprint, "runtimeId": history.RuntimeID, "runtimeServiceName": history.RuntimeServiceName},
		Cost: map[string]any{"priceVersion": history.PriceVersion, "currency": pricingCurrency, "billingUnit": pricingBillingUnit, "totalUsdMicros": base.amount, "sub2apiUserId": base.userID, "sub2apiRedeemCode": base.code, "periodStart": history.PeriodStart, "paidThrough": history.PaidThrough, "resourceType": "workspace", "resourceId": base.workspaceID,
			"components": map[string]any{"compute": map[string]any{"resourceType": "compute", "resourceId": history.ComputeID, "chargeUsdMicros": compute}, "storage": map[string]any{"resourceType": "storage", "resourceId": history.StorageID, "sizeGb": history.StorageGB, "chargeUsdMicros": storage}}},
		Owner: map[string]any{"accountId": base.accountID, "workspaceId": base.workspaceID, "ownerUserId": history.OwnerUserID},
	}
	if history.RefundAttempted || history.RefundConfirmation != nil || status == "refunded" {
		input.Type = "billing.workspace_refunded.v1"
		input.Execution = map[string]any{"resourceType": "workspace", "resourceId": base.workspaceID, "reason": history.RefundReason, "computeAllocationId": history.ComputeID, "storageId": history.StorageID, "refundConfirmation": history.RefundConfirmation}
		input.Cost["sub2apiRefundCode"], input.Cost["refundUsdMicros"] = history.RefundCode, base.amount
		base.receiptID, base.expected = history.RefundReceiptID, []clients.ReceiptInput{input}
		refund := base
		refund.kind, refund.code, refund.relatedOperationID = "refund", history.RefundCode, base.operationID
		return []billingSettlement{base, refund}
	}
	base.expected = []clients.ReceiptInput{input}
	return []billingSettlement{base}
}

func readWorkspaceLaunchBillingHistory(row map[string]any) (workspaceLaunchBillingHistory, bool) {
	var history workspaceLaunchBillingHistory
	err := json.Unmarshal([]byte(stringValue(row["result"])), &history)
	return history, err == nil && stringValue(row["action"]) == workspaceLaunchAction && history.SchemaVersion == 2
}
