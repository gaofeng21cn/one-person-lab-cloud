package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// A settlement is one original debit or refund, independent of the current
// Workspace projection and provider resource lifetime.
type billingSettlement struct {
	accountID, operationID, workspaceID, kind string
	code                                      string
	userID, amount                            int64
	relatedOperationID, receiptID             string
	expected                                  []clients.ReceiptInput
	pending, invalid, refundable              bool
	// dispatched records that the owner already sent this movement's fund
	// request, whatever its result turned out to be. A missing wallet record is
	// never proof that a dispatched request did not change the balance, because
	// the audit record can be written after the balance update.
	dispatched bool
}

func projectBillingSettlements(row map[string]any) []billingSettlement {
	base := billingSettlement{accountID: stringValue(row["accountId"]), operationID: firstNonEmpty(stringValue(row["operationId"]), stringValue(row["id"])), workspaceID: stringValue(row["workspaceId"])}
	switch stringValue(row["action"]) {
	case "workspace.launch", "workspace.launch.v2":
		base.kind = "purchase"
		_, sourceErr := refundableWalletOperationCharge(row)
		base.refundable = sourceErr == nil
		if history, retained := readWorkspaceLaunchBillingHistory(row); retained {
			return historicalWorkspacePurchaseSettlements(row, base, history)
		}
		operation, err := decodeWorkspaceLaunchReconcileOperation(row)
		if err != nil {
			base.invalid = true
			return []billingSettlement{base}
		}
		if operation.raw["resourceBillingEnabled"] != nil && !operation.boolFact("resourceBillingEnabled") {
			return nil
		}
		if workspaceLaunchCloseoutActive(operation) && operation.Closeout.DebitState == "absent" {
			return nil
		}
		attempt := operation.Attempts["debit"]
		if !operation.boolFact("chargeAttempted") && attempt.Attempted == 0 && operation.raw["chargeConfirmation"] == nil && operation.Status != "succeeded" {
			return nil
		}
		base.dispatched = operation.boolFact("chargeAttempted") || attempt.Attempted > 0 || attempt.Unknown > 0 || operation.raw["chargeConfirmation"] != nil
		base.accountID, base.workspaceID = operation.stringFact("accountId"), operation.stringFact("workspaceId")
		base.userID, base.amount, base.code = operation.int64Fact("sub2apiUserId"), operation.int64Fact("totalChargeUsdMicros"), operation.stringFact("sub2apiRedeemCode")
		base.receiptID = operation.stringFact("receiptId")
		base.pending = operation.Status == "pending"
		base.invalid = base.accountID != stringValue(row["accountId"]) || base.workspaceID != stringValue(row["workspaceId"])
		if operation.raw["chargeConfirmation"] != nil {
			var confirmation map[string]any
			base.invalid = base.invalid || json.Unmarshal(operation.raw["chargeConfirmation"], &confirmation) != nil || !monthlyChargeConfirmationMatches(confirmation, base.code, base.userID, base.amount)
		}
		if workspaceLaunchCloseoutActive(operation) {
			base.receiptID = operation.Closeout.ReceiptID
			base.pending = operation.Closeout.Phase != "complete"
			base.expected = []clients.ReceiptInput{workspaceLaunchCloseoutReceiptInput(operation)}
		} else {
			input, err := workspaceLaunchPurchaseReceiptInput(operation)
			base.invalid = base.invalid || err != nil
			base.expected = []clients.ReceiptInput{input, workspaceLaunchHistoricalChargedReceiptInput(input)}
		}
		return []billingSettlement{base}
	case "workspace.renewal":
		base.kind = "renewal"
		_, sourceErr := refundableWalletOperationCharge(row)
		base.refundable = sourceErr == nil
		operation, err := decodeWorkspaceRenewalOperation(row)
		if err != nil {
			base.invalid = true
			return []billingSettlement{base}
		}
		if !operation.ChargeAttempted && operation.ChargeConfirmation == nil && !operation.EntitlementCommitted && operation.Status != "active" && !operation.RefundAttempted && operation.RefundConfirmation == nil {
			return nil
		}
		base.accountID, base.workspaceID = operation.AccountID, operation.WorkspaceID
		base.userID = int64(numberField(operation.ChargeConfirmation, "userId", 0))
		base.amount, base.code, base.receiptID = operation.TotalUSDMicros, operation.RedeemCode, operation.ReceiptID
		switch operation.Status {
		case "claimed", "debit_pending", "debited", "provider_renewing", "verifying", "refund_pending", "insufficient":
			base.pending = true
		case "active", "manual_review", "refunded", "cancelled", "expired_unpaid":
		default:
			base.invalid = true
		}
		base.invalid = base.invalid || operation.ComputeUSDMicros <= 0 || operation.StorageUSDMicros <= 0 || operation.ComputeUSDMicros > operation.TotalUSDMicros || operation.StorageUSDMicros != operation.TotalUSDMicros-operation.ComputeUSDMicros || !validSettlementPeriod(operation.PaidThrough, operation.RenewedThrough) || operation.PriceVersion == "" || operation.ComputeID == "" || operation.StorageID == "" || operation.StorageGB <= 0
		if operation.ChargeConfirmation != nil && !monthlyChargeConfirmationMatches(operation.ChargeConfirmation, base.code, base.userID, base.amount) {
			base.invalid = true
		}
		base.dispatched = operation.ChargeAttempted || operation.ChargeConfirmation != nil
		base.expected = []clients.ReceiptInput{workspaceRenewalReceiptInput(operation, base.userID)}
		if operation.RefundAttempted || operation.RefundConfirmation != nil || operation.Status == "refunded" {
			// A cancelled fulfillment has one refund receipt binding both the debit
			// and its reversal; it never claims a successfully renewed Workspace.
			base.receiptID = operation.RefundReceiptID
			base.expected = []clients.ReceiptInput{workspaceRefundReceiptInput(operation, base.userID)}
			base.pending = operation.Status != "manual_review" && (operation.Status != "refunded" || operation.Phase != "complete")
			refund := base
			refund.kind, refund.code, refund.relatedOperationID = "refund", operation.RefundCode, operation.ID
			refund.dispatched = operation.RefundAttempted || operation.RefundConfirmation != nil
			return []billingSettlement{base, refund}
		}
		return []billingSettlement{base}
	case "gateway.wallet_adjustment.v1":
		base.kind = "refund"
		operation, err := decodeWalletAdjustment(row)
		if err != nil {
			base.invalid = true
			return []billingSettlement{base}
		}
		if operation.Kind != "business_refund" || operation.Status == "failed" && !operation.AdjustmentAttempted {
			return nil
		}
		base.accountID, base.userID, base.amount, base.code = operation.AccountID, operation.Sub2APIUserID, operation.AmountUSDMicros, operation.CanonicalRedeemCode
		if operation.RedeemCodeVersion == "" && operation.CanonicalRedeemCode == "" && (operation.LegacySupersession == "legacy_history_confirmed" || operation.Status == "manual_review") {
			base.code = legacyWalletAdjustmentRedeemCode(base.operationID)
		}
		base.relatedOperationID, base.receiptID = operation.RelatedOperationID, operation.ReceiptID
		base.pending = operation.Status == "pending"
		base.dispatched = operation.AdjustmentAttempted || operation.RecoveryAttempted || operation.UpstreamFailure != nil
		base.expected = []clients.ReceiptInput{walletAdjustmentReceipt(base.operationID, operation)}
		return []billingSettlement{base}
	}
	return nil
}

// projectWorkspaceDeleteLegacySettlements explains the one refund a retired
// Workspace delete wrote on its own operation row instead of creating a
// business-refund adjustment. The retained row keeps the refund code, wallet
// user, amount and confirmation, and names the original purchase order, so the
// movement is owned evidence rather than a new rule. A delete that attempted a
// refund but never reached a confirmed terminal state keeps the movement
// unconfirmed instead of dropping it.
func projectWorkspaceDeleteLegacySettlements(row map[string]any) []billingSettlement {
	operation, err := decodeWorkspaceDeleteLegacyOperation(row)
	if err != nil {
		return nil
	}
	refund := billingSettlement{
		accountID: operation.AccountID, operationID: operation.OperationID, workspaceID: operation.WorkspaceID,
		kind: "refund", code: operation.RefundCode, userID: operation.Sub2APIUserID, amount: operation.TotalUSDMicros,
		relatedOperationID: operation.LaunchOperationID, receiptID: operation.RefundReceiptID,
		dispatched: operation.RefundAttempted || operation.RefundConfirmation != nil,
	}
	if validWorkspaceDeleteLegacyTerminal(operation) {
		return []billingSettlement{refund}
	}
	if !operation.RefundAttempted && operation.RefundConfirmation == nil {
		// No refund dispatch happened, so this delete moved no money.
		return nil
	}
	// The dispatch happened and no confirmed refund exists. Reading its wallet
	// record still proves the movement when the owner wrote one; otherwise the
	// trend reports it as unconfirmed rather than assuming zero.
	refund.receiptID = ""
	return []billingSettlement{refund}
}

func validSettlementPeriod(start, end string) bool {
	from, err := time.Parse(time.RFC3339, start)
	through, endErr := time.Parse(time.RFC3339, end)
	return err == nil && endErr == nil && through.After(from)
}

func sub2APIReconciliationCode(row map[string]any, userID int64, history map[string]clients.Sub2APIBalanceHistoryEntry) string {
	code := stringValue(row["sub2apiRedeemCode"])
	entry, found := history[code]
	if !found {
		return "sub2api_charge_missing"
	}
	charge, validCharge := requiredNonNegativeInteger(row, "chargeUsdMicros")
	if !validCharge || entry.Code != code || entry.Type != "balance" || entry.Status != "used" || entry.UsedBy == nil || *entry.UsedBy != userID || entry.ValueUSDMicros != -charge {
		return "sub2api_charge_mismatch"
	}
	return ""
}

func reconciliationLedgerReceipts(ctx context.Context, service *controlplane.Service, query clients.ReceiptQuery) ([]clients.Receipt, error) {
	if query.AccountID == "" || query.RequestID == "" && query.WorkspaceID == "" {
		return nil, errors.New("ledger_receipt_scope_required")
	}
	receipts := []clients.Receipt{}
	query.Limit = 100
	seen := map[string]bool{}
	for {
		page, err := service.BillingReceipts(ctx, query)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, page.Receipts...)
		if !page.HasMore {
			return receipts, nil
		}
		if page.NextCursor == "" || seen[page.NextCursor] {
			return nil, errors.New("ledger_receipt_pagination_invalid")
		}
		seen[page.NextCursor] = true
		query.Cursor = page.NextCursor
	}
}

func (app *controlPlaneServer) reconciliationProjectionLocked() map[string]any {
	row, ok, err := app.tables.BillingReconciliation(context.Background())
	if err != nil {
		return map[string]any{"reports": 0, "guard": map[string]any{"status": "unavailable", "blockNewWorkspaces": true, "reason": "billing_reconciliation_unavailable"}}
	}
	if !ok {
		return map[string]any{"reports": 0, "guard": map[string]any{"status": "not_required", "blockNewWorkspaces": false, "reason": "billing_reconciliation_not_required"}}
	}
	if _, err := billingReconciliationBlockState(row); err != nil {
		return map[string]any{"reports": 0, "guard": map[string]any{"status": "unavailable", "blockNewWorkspaces": true, "reason": "billing_reconciliation_invalid"}}
	}
	row["reports"] = 1
	return row
}

func billingReconciliationBlockState(row map[string]any) (bool, error) {
	status := stringValue(row["status"])
	guard := mapField(row, "guard")
	guardStatus, reason := stringValue(guard["status"]), stringValue(guard["reason"])
	blocked, hasBlocked := guard["blockNewWorkspaces"].(bool)
	if stringValue(row["id"]) == "" || (status != "ok" && status != "mismatch") || guardStatus != status || reason == "" || !hasBlocked || blocked != (status == "mismatch") {
		return true, errors.New("billing_reconciliation_guard_invalid")
	}
	return blocked, nil
}

func (app *controlPlaneServer) resourceLedgerEvidenceLocked(accountIDs ...string) []any {
	rows := []any{}
	for _, workspace := range app.listWorkspaces("") {
		if len(accountIDs) > 0 && !app.resourceBelongsToAccount(workspace, accountIDs[0]) {
			continue
		}
		workspaceID := stringValue(workspace["id"])
		computeID := stringValue(workspace["currentComputeAllocationId"])
		storageID := stringValue(workspace["storageId"])
		attachmentID := stringValue(workspace["currentAttachmentId"])
		compute, _ := app.getCompute(computeID)
		storage, _ := app.getStorage(storageID)
		attachment, _ := app.getAttachment(attachmentID)
		operation := app.operationEvidenceForResourceLocked(workspaceID, computeID, storageID, attachmentID)
		ownerAccountID := firstNonEmpty(stringValue(workspace["ownerAccountId"]), stringValue(compute["ownerAccountId"]), stringValue(storage["ownerAccountId"]), stringValue(attachment["ownerAccountId"]))
		rows = append(rows, map[string]any{
			"id": firstNonEmpty(workspaceID, computeID, storageID, attachmentID), "accountId": ownerAccountID,
			"ownerAccountId": ownerAccountID, "ownerUserId": firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(compute["ownerUserId"]), stringValue(storage["ownerUserId"])),
			"workspaceId": workspaceID, "workspaceIds": uniqueStrings([]string{workspaceID}),
			"computeAllocationId": computeID, "storageId": storageID, "attachmentId": attachmentID,
			"providerRequestId": firstNonEmpty(stringValue(compute["providerRequestId"]), stringValue(storage["providerRequestId"]), stringValue(attachment["providerRequestId"])),
			"operationId":       firstNonEmpty(stringValue(operation["operationId"]), stringValue(compute["operationId"]), stringValue(storage["operationId"]), stringValue(attachment["operationId"])),
			"receiptIds":        uniqueStrings([]string{stringValue(compute["lastReceiptId"]), stringValue(storage["lastReceiptId"]), stringValue(workspace["purchaseReceiptId"])}),
		})
	}
	return rows
}

func (app *controlPlaneServer) operationEvidenceForResourceLocked(ids ...string) map[string]any {
	operations := app.runtimeOperationRows(runtimeOperationQuery{})
	for index := len(operations) - 1; index >= 0; index-- {
		operation := operations[index]
		if mapContainsAnyID(operation, ids...) {
			return map[string]any{"operationId": operation["operationId"], "resourceId": operation["resourceId"]}
		}
	}
	return map[string]any{}
}
