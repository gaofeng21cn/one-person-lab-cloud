package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
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
}

type billingReconciliationException struct {
	resourceType, resourceID, code            string
	accountID, operationID, workspaceID, kind string
}

func (app *controlPlaneServer) billingReconciliationReport(ctx context.Context, service *controlplane.Service, idempotencyKey string) (map[string]any, error) {
	settlements := []billingSettlement{}
	for _, action := range []string{"workspace.launch", "workspace.launch.v2", "workspace.renewal", "gateway.wallet_adjustment.v1"} {
		rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{Action: action})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			settlements = append(settlements, projectBillingSettlements(row)...)
		}
	}
	sort.Slice(settlements, func(i, j int) bool {
		if settlements[i].operationID != settlements[j].operationID {
			return settlements[i].operationID < settlements[j].operationID
		}
		return settlements[i].kind < settlements[j].kind
	})
	originals := map[string]billingSettlement{}
	codes := map[string]int{}
	refundTotals := map[string]int64{}
	refundOverflow := map[string]bool{}
	for _, settlement := range settlements {
		if settlement.kind != "refund" {
			originals[settlement.operationID] = settlement
		}
		if settlement.code != "" {
			codes[settlement.code]++
		}
	}
	for index := range settlements {
		settlement := settlements[index]
		if settlement.kind != "refund" {
			continue
		}
		original, found := originals[settlement.relatedOperationID]
		if !found || settlement.amount <= 0 {
			continue
		}
		if original.accountID == settlement.accountID && original.userID == settlement.userID {
			settlements[index].workspaceID = original.workspaceID
		}
		total := refundTotals[original.operationID]
		if settlement.amount > original.amount-total {
			refundOverflow[original.operationID] = true
		} else {
			refundTotals[original.operationID] += settlement.amount
		}
	}

	matched, pending := 0, 0
	exceptions := []billingReconciliationException{}
	for _, settlement := range settlements {
		before := len(exceptions)
		add := func(code string) {
			exceptions = append(exceptions, billingReconciliationException{
				resourceType: "workspace", resourceID: firstNonEmpty(settlement.workspaceID, settlement.operationID), code: code,
				accountID: settlement.accountID, operationID: settlement.operationID, workspaceID: settlement.workspaceID, kind: settlement.kind,
			})
		}
		if settlement.invalid || settlement.accountID == "" || settlement.operationID == "" || settlement.kind != "refund" && settlement.workspaceID == "" || settlement.amount <= 0 || settlement.code == "" {
			add("billing_operation_invalid")
			continue
		}
		if codes[settlement.code] > 1 {
			add("billing_transaction_duplicate")
		}
		if settlement.kind == "refund" {
			original, found := originals[settlement.relatedOperationID]
			if !found || original.invalid || original.accountID != settlement.accountID || original.userID != settlement.userID || settlement.operationID != settlement.relatedOperationID && !original.refundable {
				add("billing_refund_source_invalid")
			}
			if refundOverflow[settlement.relatedOperationID] {
				add("billing_refund_limit_exceeded")
			}
		}
		// Normal owner progress has not yet promised a settled receipt. Report it
		// separately; it is never counted as matched and cannot block other buyers.
		if settlement.pending {
			if len(exceptions) == before {
				pending++
			}
			continue
		}
		userID, err := app.sub2APIUserID(ctx, settlement.accountID)
		if err != nil {
			add("sub2api_balance_history_unavailable")
		} else if settlement.userID <= 0 || userID != settlement.userID {
			add("sub2api_charge_mismatch")
		} else {
			history, err := service.FinancialBalanceHistoryByCodes(ctx, settlement.userID, []string{settlement.code})
			if err != nil {
				add("sub2api_balance_history_unavailable")
			} else {
				kind := "debit"
				missing, mismatch := "sub2api_charge_missing", "sub2api_charge_mismatch"
				if settlement.kind == "refund" {
					kind, missing, mismatch = "business_refund", "sub2api_refund_missing", "sub2api_refund_mismatch"
				}
				_, state, err := inspectWalletAdjustmentHistory(history, settlement.code, walletAdjustmentOperation{Kind: kind, Sub2APIUserID: settlement.userID, AmountUSDMicros: settlement.amount})
				if err != nil {
					add(mismatch)
				} else if state != "confirmed" {
					add(missing)
				}
			}
		}
		receipts, err := reconciliationLedgerReceipts(ctx, service, clients.ReceiptQuery{AccountID: settlement.accountID, RequestID: settlement.operationID})
		if err != nil {
			add("ledger_receipts_unavailable")
		} else if code := settlementReceiptReconciliationCode(settlement, receipts); code != "" {
			add(code)
		}
		if len(exceptions) == before {
			matched++
		}
	}
	report := reconciliationReport("reconciliation-"+stableID(idempotencyKey)[:18], len(settlements), matched, exceptions)
	report["counts"].(map[string]any)["pending"] = pending
	return report, nil
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
		attempt := operation.Attempts["debit"]
		if !operation.boolFact("chargeAttempted") && attempt.Attempted == 0 && operation.raw["chargeConfirmation"] == nil && operation.Status != "succeeded" {
			return nil
		}
		base.accountID, base.workspaceID = operation.stringFact("accountId"), operation.stringFact("workspaceId")
		base.userID, base.amount, base.code = operation.int64Fact("sub2apiUserId"), operation.int64Fact("totalChargeUsdMicros"), operation.stringFact("sub2apiRedeemCode")
		base.receiptID = operation.stringFact("receiptId")
		base.pending = operation.Status == "pending"
		input, err := workspaceLaunchPurchaseReceiptInput(operation)
		base.invalid = err != nil || base.accountID != stringValue(row["accountId"]) || base.workspaceID != stringValue(row["workspaceId"])
		if operation.raw["chargeConfirmation"] != nil {
			var confirmation map[string]any
			base.invalid = base.invalid || json.Unmarshal(operation.raw["chargeConfirmation"], &confirmation) != nil || !monthlyChargeConfirmationMatches(confirmation, base.code, base.userID, base.amount)
		}
		base.expected = []clients.ReceiptInput{input, workspaceLaunchHistoricalChargedReceiptInput(input)}
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
		base.expected = []clients.ReceiptInput{workspaceRenewalReceiptInput(operation, base.userID)}
		if operation.RefundAttempted || operation.RefundConfirmation != nil || operation.Status == "refunded" {
			// A cancelled fulfillment has one refund receipt binding both the debit
			// and its reversal; it never claims a successfully renewed Workspace.
			base.receiptID = operation.RefundReceiptID
			base.expected = []clients.ReceiptInput{workspaceRefundReceiptInput(operation, base.userID)}
			base.pending = operation.Status != "manual_review" && (operation.Status != "refunded" || operation.Phase != "complete")
			refund := base
			refund.kind, refund.code, refund.relatedOperationID = "refund", operation.RefundCode, operation.ID
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
		base.expected = []clients.ReceiptInput{walletAdjustmentReceipt(base.operationID, operation)}
		return []billingSettlement{base}
	}
	return nil
}

func validSettlementPeriod(start, end string) bool {
	from, err := time.Parse(time.RFC3339, start)
	through, endErr := time.Parse(time.RFC3339, end)
	return err == nil && endErr == nil && through.After(from)
}

func settlementReceiptReconciliationCode(settlement billingSettlement, receipts []clients.Receipt) string {
	matches := []clients.Receipt{}
	for _, receipt := range receipts {
		switch receipt.Type {
		case "billing.workspace_purchased.v1", "billing.workspace_renewed.v1", "billing.workspace_refunded.v1", "gateway.wallet_adjustment.v1":
			matches = append(matches, receipt)
		}
	}
	if len(matches) == 0 {
		return "ledger_receipt_missing"
	}
	if len(matches) != 1 || matches[0].ReceiptID == "" || settlement.receiptID == "" || matches[0].ReceiptID != settlement.receiptID {
		return "ledger_receipt_mismatch"
	}
	for _, expected := range settlement.expected {
		actual := matches[0].ReceiptInput
		// A separately observed balance is not an amount applied by this order.
		actual.Cost, expected.Cost = cloneMap(actual.Cost), cloneMap(expected.Cost)
		delete(actual.Cost, "postChargeBalanceUsdMicros")
		delete(expected.Cost, "postChargeBalanceUsdMicros")
		if workspaceLaunchReceiptInputMatches(actual, expected) {
			return ""
		}
	}
	return "ledger_receipt_mismatch"
}

func reconciliationReport(id string, checked, matched int, exceptions []billingReconciliationException) map[string]any {
	status := "ok"
	if len(exceptions) > 0 {
		status = "mismatch"
	}
	items := make([]any, 0, len(exceptions))
	for _, exception := range exceptions {
		item := map[string]any{"resourceType": exception.resourceType, "resourceId": exception.resourceID, "code": exception.code}
		if exception.operationID != "" {
			item["operationId"], item["accountId"], item["workspaceId"], item["kind"] = exception.operationID, exception.accountID, exception.workspaceID, exception.kind
		}
		items = append(items, item)
	}
	return map[string]any{"id": id, "status": status, "counts": map[string]any{"billingOperations": checked, "matched": matched, "exceptions": len(exceptions)}, "exceptions": items}
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

func (app *controlPlaneServer) reconciliationBlocksNewWorkspaces(ctx context.Context) (map[string]any, bool, error) {
	row, ok, err := app.tables.BillingReconciliation(ctx)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		projection := map[string]any{"reports": 0, "guard": map[string]any{"status": "not_required", "blockNewWorkspaces": false, "reason": "billing_reconciliation_not_required"}}
		return projection, false, nil
	}
	blocked, err := billingReconciliationBlockState(row)
	if err != nil {
		return nil, true, err
	}
	row["reports"] = 1
	return row, blocked, nil
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

func validateBillingReconciliationMutation(mutation billingReconciliationMutation) error {
	row, audit := mutation.Row, mutation.AuditEvent
	if _, err := billingReconciliationBlockState(row); err != nil {
		return err
	}
	report := mapField(row, "report")
	if stringValue(report["id"]) != stringValue(row["id"]) {
		return errors.New("billing_reconciliation_report_invalid")
	}
	if !billingReconciliationAuditIdentityMatches(audit, row) {
		return errors.New("billing_reconciliation_audit_invalid")
	}
	if _, err := time.Parse(time.RFC3339, stringValue(audit["createdAt"])); err != nil {
		return errors.New("billing_reconciliation_audit_invalid")
	}
	return nil
}

func billingReconciliationIdentityMatches(current, desired map[string]any) bool {
	currentBlocked, currentErr := billingReconciliationBlockState(current)
	desiredBlocked, desiredErr := billingReconciliationBlockState(desired)
	return currentErr == nil && desiredErr == nil && stringValue(current["id"]) == stringValue(desired["id"]) &&
		stringValue(current["status"]) == stringValue(desired["status"]) && currentBlocked == desiredBlocked &&
		stringValue(mapField(current, "guard")["reason"]) == stringValue(mapField(desired, "guard")["reason"])
}

func billingReconciliationContentMatches(current, desired map[string]any) bool {
	if !billingReconciliationIdentityMatches(current, desired) {
		return false
	}
	currentReport, currentOK := current["report"]
	desiredReport, desiredOK := desired["report"]
	if !currentOK || !desiredOK {
		return false
	}
	currentJSON, currentErr := json.Marshal(currentReport)
	desiredJSON, desiredErr := json.Marshal(desiredReport)
	return currentErr == nil && desiredErr == nil && string(currentJSON) == string(desiredJSON)
}

func billingReconciliationAuditID(resultID string) string {
	return "audit-" + stableID("billing.reconciliation", "billing_reconciliation", resultID)[:12]
}

func billingReconciliationAuditIdentityMatches(row, desired map[string]any) bool {
	resultID := stringValue(desired["id"])
	after, ok := row["after"].(map[string]any)
	return stringValue(row["id"]) == billingReconciliationAuditID(resultID) && stringValue(row["actorUserId"]) != "" &&
		stringValue(row["action"]) == "billing.reconciliation" && stringValue(row["resourceKind"]) == "billing_reconciliation" &&
		stringValue(row["resourceId"]) == resultID && stringValue(row["result"]) == "succeeded" && ok &&
		billingReconciliationContentMatches(after, desired)
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
