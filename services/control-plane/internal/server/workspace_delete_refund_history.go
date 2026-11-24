package server

import (
	"context"
	"encoding/json"

	contracts "opl-cloud/packages/contracts/go"
)

// This file owns the retained original-charge refund record: its decoder, its
// deterministic identity and the read that projects it onto the deletion status.
//
// The refund execution engine is retired. Nothing here reserves, dispatches or
// confirms a wallet refund any more, and no reservation row is deleted. A
// deletion whose refund was already reserved against an original charge stays
// readable through this decoder, so an unresolved platform refund remains
// visible on the customer-visible deletion status instead of disappearing with
// its executor.
const (
	workspaceDeleteRefundAction = "workspace.delete.refund.v1"

	workspaceDeleteRefundStatusBlocked      = "blocked"
	workspaceDeleteRefundStatusPending      = "pending"
	workspaceDeleteRefundStatusManualReview = "manual_review"
	workspaceDeleteRefundStatusSucceeded    = "succeeded"
	workspaceDeleteRefundStatusNotDue       = "not_due"
)

type workspaceDeleteRefundOperation struct {
	SchemaVersion               int    `json:"schemaVersion"`
	AccountID                   string `json:"accountId"`
	WorkspaceID                 string `json:"workspaceId"`
	DeleteOperationID           string `json:"deleteOperationId"`
	LaunchOperationID           string `json:"launchOperationId"`
	PolicyVersion               string `json:"policyVersion"`
	Status                      string `json:"status"`
	ReasonCode                  string `json:"reasonCode,omitempty"`
	OriginalChargeCode          string `json:"originalChargeCode,omitempty"`
	OriginalChargeUSDMicros     int64  `json:"originalChargeUsdMicros,omitempty"`
	RefundUSDMicros             int64  `json:"refundUsdMicros,omitempty"`
	UsedHours                   int64  `json:"usedHours,omitempty"`
	RefundHours                 int64  `json:"refundHours,omitempty"`
	WalletAdjustmentOperationID string `json:"walletAdjustmentOperationId,omitempty"`
	// RefundOrderOperationID identifies the confirmed order this refund is reserved
	// against: the renewal when one paid for the period in use, otherwise the
	// original purchase.
	RefundOrderOperationID string `json:"refundOrderOperationId,omitempty"`
	RefundReceiptID        string `json:"refundReceiptId,omitempty"`
	DeletedAt              string `json:"deletedAt,omitempty"`
	ResourceFulfilledAt    string `json:"resourceFulfilledAt,omitempty"`
	ReadbackID             string `json:"readbackId,omitempty"`
	ReadbackProvider       string `json:"readbackProvider,omitempty"`
	ReadbackObservedAt     string `json:"readbackObservedAt,omitempty"`
	CreatedAt              string `json:"createdAt"`
	UpdatedAt              string `json:"updatedAt"`
}

func workspaceDeleteRefundOperationID(deleteOperationID string) string {
	return "workspace-delete-refund-" + stableID(deleteOperationID)[:24]
}

func decodeWorkspaceDeleteRefund(row map[string]any) (workspaceDeleteRefundOperation, error) {
	var operation workspaceDeleteRefundOperation
	if stringValue(row["action"]) != workspaceDeleteRefundAction || json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil ||
		operation.AccountID == "" || operation.WorkspaceID == "" || stringValue(row["accountId"]) != operation.AccountID ||
		operation.DeleteOperationID == "" || operation.PolicyVersion != contracts.WorkspaceRefundPolicyVersion ||
		operation.RefundUSDMicros < 0 || operation.OriginalChargeUSDMicros < 0 ||
		(operation.Status != workspaceDeleteRefundStatusBlocked && operation.Status != workspaceDeleteRefundStatusPending &&
			operation.Status != workspaceDeleteRefundStatusManualReview && operation.Status != workspaceDeleteRefundStatusSucceeded &&
			operation.Status != workspaceDeleteRefundStatusNotDue) {
		return workspaceDeleteRefundOperation{}, errWorkspaceDeleteStateRead
	}
	return operation, nil
}

func (app *controlPlaneServer) workspaceDeleteRefundRecord(ctx context.Context, workspaceID string) (workspaceDeleteRefundOperation, bool, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDeleteRefundOperationID(workspaceDeleteOperationID(workspaceID)))
	if err != nil || !found {
		return workspaceDeleteRefundOperation{}, found, err
	}
	operation, err := decodeWorkspaceDeleteRefund(row)
	return operation, err == nil, err
}
