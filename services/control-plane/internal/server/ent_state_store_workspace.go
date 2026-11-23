package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

var (
	workspaceEntFields = []sqlRecordField{
		sqlTextField("account_id", "accountId"),
		sqlTextField("owner_account_id", "ownerAccountId"),
		sqlTextField("owner_user_id", "ownerUserId"),
		sqlTextField("user_id", "userId"),
		sqlTextField("name", "name"),
		sqlTextField("url", "url"),
		sqlTextField("state", "state"),
		sqlTextField("status", "status"),
		sqlTextField("purchase_receipt_id", "purchaseReceiptId"),
		sqlTextField("storage_id", "storageId"),
		sqlTextField("current_compute_allocation_id", "currentComputeAllocationId"),
		sqlTextField("current_attachment_id", "currentAttachmentId"),
		sqlTextField("runtime_id", "runtimeId"),
		sqlTextField("runtime_service_name", "runtime", "serviceName"),
		sqlTextField("runtime_service_name_root", "runtimeServiceName"),
		sqlTextField("service_name", "serviceName"),
		sqlNullableIntField("workspace_api_key_id", "workspaceApiKeyId"),
		sqlTextField("access_account", "access", "account"),
		sqlTextField("access_username", "access", "username"),
		sqlTextField("credential_status", "access", "credentialStatus"),
		sqlTextField("credential_version", "access", "credentialVersion"),
		sqlTextField("credential_secret_ref", "access", "secretRef"),
		sqlTextField("verification_slot_id", "verificationSlotId"),
		sqlTextField("application_binding", "applicationBinding"),
		sqlIntField("application_binding_version", "applicationBindingVersion"),
		sqlTextField("current_application_deployment_id", "currentApplicationDeploymentId"),
		sqlTextField("reserved_application_deployment_id", "reservedApplicationDeploymentId"),
		sqlBoolField("customer_product", "customerProduct"),
		{Column: "billing_state_json", Kind: "workspace_billing_json"},
	}
)

type workspaceBillingState struct {
	// Legacy paid states omit this field. An explicit false value marks a
	// customer-owned workspace whose compute/storage are not billed by Cloud.
	ResourceBillingEnabled *bool  `json:"resourceBillingEnabled,omitempty"`
	AutoRenew              bool   `json:"autoRenew"`
	AuthorizedBy           string `json:"authorizedBy"`
	AuthorizedAt           string `json:"authorizedAt"`
	PackageID              string `json:"packageId"`
	StorageGB              int64  `json:"storageGb"`
	PriceVersion           string `json:"priceVersion"`
	Currency               string `json:"currency"`
	BillingUnit            string `json:"billingUnit"`
	ComputeUSDMicros       int64  `json:"computeUsdMicros"`
	StorageUSDMicros       int64  `json:"storageUsdMicros"`
	TotalUSDMicros         int64  `json:"totalUsdMicros"`
	PeriodStart            string `json:"periodStart"`
	PaidThrough            string `json:"paidThrough"`
	NextRenewalAt          string `json:"nextRenewalAt"`
	BillingAnchorDay       int64  `json:"billingAnchorDay"`
	RenewalStatus          string `json:"renewalStatus"`
	ComputeAllocationID    string `json:"computeAllocationId"`
	StorageID              string `json:"storageId"`
	ManualReviewReason     string `json:"-"`
}

var workspaceBillingStateRequiredKeys = []string{
	"autoRenew", "authorizedBy", "authorizedAt", "packageId", "storageGb", "priceVersion", "currency", "billingUnit",
	"computeUsdMicros", "storageUsdMicros", "totalUsdMicros", "periodStart", "paidThrough",
	"nextRenewalAt", "billingAnchorDay", "renewalStatus", "computeAllocationId", "storageId",
}

var workspaceBillingStateExclusiveKeys = []string{
	"authorizedBy", "authorizedAt", "storageGb", "priceVersion", "currency", "billingUnit",
	"computeUsdMicros", "storageUsdMicros", "totalUsdMicros", "periodStart", "paidThrough", "nextRenewalAt", "billingAnchorDay",
}

const workspaceBillingLegacyMismatch = "legacy_billing_state_mismatch"
const workspaceBillingNotApplicable = "not_applicable"

func validateWorkspaceBillingState(row map[string]any) error {
	_, _, err := normalizeWorkspaceBillingStateForWorkspace(row, row)
	return err
}

func mergeWorkspaceForSave(existing, incoming map[string]any) (map[string]any, error) {
	row := cloneMap(incoming)
	if existing == nil {
		return row, nil
	}
	for _, key := range []string{"accountId", "ownerAccountId", "ownerUserId"} {
		if current := stringValue(existing[key]); current != "" && stringValue(row[key]) != current {
			return nil, errIdempotencyConflict
		}
	}
	existingBilling := workspaceAcceptedBillingState(existing)
	incomingBilling := workspaceAcceptedBillingState(row)
	if existingBilling != nil && incomingBilling != nil {
		for _, key := range []string{"computeAllocationId", "storageId", "packageId", "priceVersion"} {
			if existingBilling[key] != incomingBilling[key] {
				return nil, errIdempotencyConflict
			}
		}
	}
	existingAutoRenew, existingHasAutoRenew := existing["autoRenew"].(bool)
	incomingAutoRenew, incomingHasAutoRenew := row["autoRenew"].(bool)
	if existingHasAutoRenew && !existingAutoRenew && incomingHasAutoRenew && incomingAutoRenew {
		if existingBilling == nil {
			return nil, errIdempotencyConflict
		}
		for key, value := range existingBilling {
			row[key] = value
		}
	}
	preserveWorkspaceStorageLoss(existing, row)
	if !workspaceLifecycleInactive(existing) || workspaceLifecycleInactive(row) {
		return row, nil
	}
	for _, key := range []string{"state", "status", "currentComputeAllocationId", "currentAttachmentId"} {
		row[key] = existing[key]
	}
	if existingBilling != nil {
		for key, value := range existingBilling {
			row[key] = value
		}
	}
	return row, nil
}

func workspaceLifecycleInactive(row map[string]any) bool {
	switch firstNonEmpty(stringValue(row["state"]), stringValue(row["status"])) {
	case "suspended", "stopped", "data_deleted", "unrecoverable", "storage_missing", "destroyed":
		return true
	default:
		return false
	}
}

func normalizeWorkspaceBillingStateForWorkspace(row, workspace map[string]any) (workspaceBillingState, bool, error) {
	currentComputeID := stringValue(workspace["currentComputeAllocationId"])
	state, present, err := normalizeWorkspaceBillingState(row, currentComputeID, stringValue(workspace["storageId"]), stringValue(workspace["ownerUserId"]))
	if err != nil || !present || state.RenewalStatus == "manual_review" || currentComputeID != "" {
		return state, present, err
	}
	if state.AutoRenew {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	switch firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) {
	case "suspended", "data_deleted", "unrecoverable":
		return state, present, nil
	default:
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
}

func normalizeWorkspaceBillingState(row map[string]any, expectedComputeID, expectedStorageID, expectedOwnerID string) (workspaceBillingState, bool, error) {
	_, hasRenewalStatus := row["renewalStatus"]
	_, hasAutoRenew := row["autoRenew"]
	_, hasPriceVersion := row["priceVersion"]
	if !hasRenewalStatus && !hasAutoRenew && !hasPriceVersion {
		return workspaceBillingState{}, false, nil
	}
	if row["renewalStatus"] == "manual_review" {
		for _, key := range workspaceBillingStateExclusiveKeys {
			if _, ok := row[key]; ok {
				return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
			}
		}
		autoRenew, validAutoRenew := row["autoRenew"].(bool)
		reason, validReason := row["manualReviewReason"].(string)
		if !validAutoRenew || autoRenew || !validReason || reason != workspaceBillingLegacyMismatch {
			return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
		}
		return workspaceBillingState{RenewalStatus: "manual_review", ManualReviewReason: reason}, true, nil
	}
	if resourceBillingEnabled, ok := row["resourceBillingEnabled"]; ok {
		enabled, valid := resourceBillingEnabled.(bool)
		if !valid {
			return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
		}
		if !enabled {
			return normalizeWorkspaceNonBillingState(row, expectedComputeID, expectedStorageID)
		}
	}
	for _, key := range workspaceBillingStateRequiredKeys {
		if _, ok := row[key]; !ok {
			return workspaceBillingState{}, false, fmt.Errorf("%w: missing %s", errInvalidWorkspaceBillingState, key)
		}
	}
	autoRenew, validAutoRenew := row["autoRenew"].(bool)
	authorizedBy, validAuthorizedBy := row["authorizedBy"].(string)
	authorizedAt, validAuthorizedAt := row["authorizedAt"].(string)
	packageID, validPackageID := row["packageId"].(string)
	priceVersion, validPriceVersion := row["priceVersion"].(string)
	currency, validCurrency := row["currency"].(string)
	billingUnit, validBillingUnit := row["billingUnit"].(string)
	periodStartText, validPeriodStart := row["periodStart"].(string)
	paidThroughText, validPaidThrough := row["paidThrough"].(string)
	nextRenewalText, validNextRenewal := row["nextRenewalAt"].(string)
	renewalStatus, validRenewalStatus := row["renewalStatus"].(string)
	computeID, validComputeID := row["computeAllocationId"].(string)
	storageID, validStorageID := row["storageId"].(string)
	if !validAutoRenew || !validAuthorizedBy || !validAuthorizedAt || !validPackageID || !validPriceVersion || !validCurrency || !validBillingUnit ||
		!validPeriodStart || !validPaidThrough || !validNextRenewal || !validRenewalStatus || !validComputeID || !validStorageID {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	storageGB, validStorageGB := requiredPositiveInteger(row, "storageGb")
	computeUSDMicros, validComputePrice := requiredPositiveInteger(row, "computeUsdMicros")
	storageUSDMicros, validStoragePrice := requiredPositiveInteger(row, "storageUsdMicros")
	totalUSDMicros, validTotal := requiredPositiveInteger(row, "totalUsdMicros")
	billingAnchorDay, validAnchor := requiredPositiveInteger(row, "billingAnchorDay")
	if !validStorageGB || !validComputePrice || !validStoragePrice || !validTotal || !validAnchor || billingAnchorDay > 31 {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	checkedTotal, sumOK := checkedAddInt64(computeUSDMicros, storageUSDMicros)
	if !sumOK || totalUSDMicros != checkedTotal || strings.TrimSpace(packageID) == "" || strings.TrimSpace(priceVersion) == "" ||
		currency != pricingCurrency || billingUnit != pricingBillingUnit {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	periodStart, startErr := time.Parse(time.RFC3339, periodStartText)
	paidThrough, paidErr := time.Parse(time.RFC3339, paidThroughText)
	nextRenewal, nextErr := time.Parse(time.RFC3339, nextRenewalText)
	if startErr != nil || paidErr != nil || nextErr != nil || !paidThrough.After(periodStart) || !nextRenewal.Equal(paidThrough.Add(-24*time.Hour)) {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	if (renewalStatus != "active" && renewalStatus != "expired_unpaid") || strings.TrimSpace(computeID) == "" || strings.TrimSpace(storageID) == "" ||
		expectedComputeID != "" && computeID != expectedComputeID || expectedStorageID == "" || storageID != expectedStorageID {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	if renewalStatus == "expired_unpaid" && autoRenew {
		authorized, err := time.Parse(time.RFC3339Nano, authorizedAt)
		if err != nil || authorizedBy != expectedOwnerID || authorized.Before(paidThrough) {
			return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
		}
	}
	if autoRenew && (authorizedBy == "" || authorizedAt == "") || authorizedBy != "" && authorizedBy != expectedOwnerID || (authorizedBy == "") != (authorizedAt == "") {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	if authorizedAt != "" {
		parsed, err := time.Parse(time.RFC3339, authorizedAt)
		if err != nil {
			return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
		}
		authorizedAt = parsed.UTC().Format(time.RFC3339Nano)
	}
	if _, ok := row["manualReviewReason"]; ok {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	return workspaceBillingState{
		ResourceBillingEnabled: boolPtr(true),
		AutoRenew:              autoRenew, AuthorizedBy: authorizedBy, AuthorizedAt: authorizedAt,
		PackageID: packageID, StorageGB: storageGB, PriceVersion: priceVersion, Currency: currency, BillingUnit: billingUnit,
		ComputeUSDMicros: computeUSDMicros, StorageUSDMicros: storageUSDMicros, TotalUSDMicros: totalUSDMicros,
		PeriodStart: periodStart.UTC().Format(time.RFC3339Nano), PaidThrough: paidThrough.UTC().Format(time.RFC3339Nano), NextRenewalAt: nextRenewal.UTC().Format(time.RFC3339Nano),
		BillingAnchorDay: billingAnchorDay, RenewalStatus: renewalStatus, ComputeAllocationID: computeID, StorageID: storageID,
	}, true, nil
}

func normalizeWorkspaceNonBillingState(row map[string]any, expectedComputeID, expectedStorageID string) (workspaceBillingState, bool, error) {
	for _, key := range workspaceBillingStateRequiredKeys {
		if _, ok := row[key]; !ok {
			return workspaceBillingState{}, false, fmt.Errorf("%w: missing %s", errInvalidWorkspaceBillingState, key)
		}
	}
	autoRenew, autoRenewOK := row["autoRenew"].(bool)
	resourceBillingEnabled, resourceBillingOK := row["resourceBillingEnabled"].(bool)
	renewalStatus, renewalOK := row["renewalStatus"].(string)
	packageID, packageOK := row["packageId"].(string)
	priceVersion, priceVersionOK := row["priceVersion"].(string)
	currency, currencyOK := row["currency"].(string)
	billingUnit, billingUnitOK := row["billingUnit"].(string)
	periodStartText, periodStartOK := row["periodStart"].(string)
	paidThroughText, paidThroughOK := row["paidThrough"].(string)
	nextRenewalText, nextRenewalOK := row["nextRenewalAt"].(string)
	computeID, computeIDOK := row["computeAllocationId"].(string)
	storageID, storageIDOK := row["storageId"].(string)
	if !autoRenewOK || autoRenew || !resourceBillingOK || resourceBillingEnabled || !renewalOK || renewalStatus != workspaceBillingNotApplicable ||
		!packageOK || strings.TrimSpace(packageID) == "" || !priceVersionOK || strings.TrimSpace(priceVersion) == "" || !currencyOK || currency != pricingCurrency || !billingUnitOK || billingUnit != pricingBillingUnit ||
		!periodStartOK || !paidThroughOK || !nextRenewalOK || !computeIDOK || !storageIDOK || strings.TrimSpace(computeID) == "" || strings.TrimSpace(storageID) == "" ||
		(expectedComputeID != "" && computeID != expectedComputeID) || (expectedStorageID != "" && storageID != expectedStorageID) {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	storageGB, storageOK := requiredNonNegativeWorkspaceInteger(row, "storageGb")
	computePrice, computePriceOK := requiredNonNegativeWorkspaceInteger(row, "computeUsdMicros")
	storagePrice, storagePriceOK := requiredNonNegativeWorkspaceInteger(row, "storageUsdMicros")
	totalPrice, totalPriceOK := requiredNonNegativeWorkspaceInteger(row, "totalUsdMicros")
	billingAnchorDay, anchorOK := requiredNonNegativeWorkspaceInteger(row, "billingAnchorDay")
	if !storageOK || !computePriceOK || !storagePriceOK || !totalPriceOK || !anchorOK || computePrice != 0 || storagePrice != 0 || totalPrice != 0 || billingAnchorDay > 31 {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	periodStart, startErr := time.Parse(time.RFC3339, periodStartText)
	paidThrough, paidErr := time.Parse(time.RFC3339, paidThroughText)
	nextRenewal, nextErr := time.Parse(time.RFC3339, nextRenewalText)
	if startErr != nil || paidErr != nil || nextErr != nil || !paidThrough.After(periodStart) || !nextRenewal.Equal(paidThrough.Add(-24*time.Hour)) {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	if stringValue(row["authorizedBy"]) != "" || stringValue(row["authorizedAt"]) != "" {
		return workspaceBillingState{}, false, errInvalidWorkspaceBillingState
	}
	return workspaceBillingState{
		ResourceBillingEnabled: boolPtr(false), AutoRenew: false, PackageID: packageID, StorageGB: storageGB,
		PriceVersion: priceVersion, Currency: currency, BillingUnit: billingUnit, PeriodStart: periodStart.UTC().Format(time.RFC3339Nano),
		PaidThrough: paidThrough.UTC().Format(time.RFC3339Nano), NextRenewalAt: nextRenewal.UTC().Format(time.RFC3339Nano),
		BillingAnchorDay: billingAnchorDay, RenewalStatus: workspaceBillingNotApplicable, ComputeAllocationID: computeID, StorageID: storageID,
	}, true, nil
}

func requiredNonNegativeWorkspaceInteger(row map[string]any, key string) (int64, bool) {
	value, ok := positiveIntegerField(row, key)
	if ok {
		return value, true
	}
	if numberField(row, key, -1) == 0 {
		return 0, true
	}
	return 0, false
}

func (state workspaceBillingState) record() map[string]any {
	if state.ResourceBillingEnabled != nil && !*state.ResourceBillingEnabled {
		return map[string]any{
			"resourceBillingEnabled": false, "autoRenew": false, "authorizedBy": "", "authorizedAt": "",
			"packageId": state.PackageID, "storageGb": state.StorageGB, "priceVersion": state.PriceVersion,
			"currency": state.Currency, "billingUnit": state.BillingUnit, "computeUsdMicros": int64(0),
			"storageUsdMicros": int64(0), "totalUsdMicros": int64(0), "periodStart": state.PeriodStart,
			"paidThrough": state.PaidThrough, "nextRenewalAt": state.NextRenewalAt, "billingAnchorDay": state.BillingAnchorDay,
			"renewalStatus": workspaceBillingNotApplicable, "computeAllocationId": state.ComputeAllocationID, "storageId": state.StorageID,
		}
	}
	if state.RenewalStatus == "manual_review" {
		return map[string]any{"autoRenew": false, "renewalStatus": state.RenewalStatus, "manualReviewReason": state.ManualReviewReason}
	}
	row := map[string]any{
		"autoRenew": state.AutoRenew, "authorizedBy": state.AuthorizedBy, "authorizedAt": state.AuthorizedAt,
		"packageId": state.PackageID, "storageGb": state.StorageGB,
		"priceVersion": state.PriceVersion, "currency": state.Currency, "billingUnit": state.BillingUnit,
		"computeUsdMicros": state.ComputeUSDMicros, "storageUsdMicros": state.StorageUSDMicros, "totalUsdMicros": state.TotalUSDMicros,
		"periodStart": state.PeriodStart, "paidThrough": state.PaidThrough, "nextRenewalAt": state.NextRenewalAt,
		"billingAnchorDay": state.BillingAnchorDay, "renewalStatus": state.RenewalStatus,
		"computeAllocationId": state.ComputeAllocationID, "storageId": state.StorageID,
	}
	return row
}

func encodeWorkspaceBillingState(row map[string]any) (string, error) {
	state, present, err := normalizeWorkspaceBillingStateForWorkspace(row, row)
	if err != nil {
		return "", err
	}
	if !present {
		return "{}", nil
	}
	if state.RenewalStatus == "manual_review" {
		encoded, err := json.Marshal(state.record())
		return string(encoded), err
	}
	encoded, err := json.Marshal(state)
	return string(encoded), err
}

func decodeWorkspaceBillingState(encoded string, workspace map[string]any) (map[string]any, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, errInvalidWorkspaceBillingState
	}
	if strings.TrimSpace(encoded) == "{}" {
		return nil, nil
	}
	var shape map[string]json.RawMessage
	shapeDecoder := json.NewDecoder(strings.NewReader(encoded))
	if err := shapeDecoder.Decode(&shape); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(shapeDecoder); err != nil {
		return nil, err
	}
	if len(shape) == 3 && shape["autoRenew"] != nil && shape["renewalStatus"] != nil && shape["manualReviewReason"] != nil {
		var marker map[string]any
		if err := json.Unmarshal([]byte(encoded), &marker); err != nil {
			return nil, err
		}
		normalized, _, err := normalizeWorkspaceBillingState(marker, "", "", "")
		if err != nil {
			return nil, err
		}
		return normalized.record(), nil
	}
	allowed := map[string]bool{}
	if _, ok := shape["resourceBillingEnabled"]; ok {
		allowed["resourceBillingEnabled"] = true
	}
	for _, key := range workspaceBillingStateRequiredKeys {
		allowed[key] = true
		if _, ok := shape[key]; !ok {
			return nil, fmt.Errorf("%w: missing %s", errInvalidWorkspaceBillingState, key)
		}
	}
	for key := range shape {
		if !allowed[key] {
			return nil, fmt.Errorf("%w: unknown %s", errInvalidWorkspaceBillingState, key)
		}
	}
	var state workspaceBillingState
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	normalized, _, err := normalizeWorkspaceBillingStateForWorkspace(state.record(), workspace)
	if err != nil {
		return nil, err
	}
	return normalized.record(), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errInvalidWorkspaceBillingState
	}
	return nil
}

func (s *postgresEntStateStore) ListWorkspaces(ctx context.Context, accountID string) ([]map[string]any, error) {
	suffix := ""
	var args []any
	if accountID != "" {
		suffix = "WHERE (account_id = ? OR (account_id = '' AND owner_account_id = ?))"
		args = []any{accountID, accountID}
	}
	rows, err := s.loadSQLRecordSet(ctx, s.db, "control_plane_workspaces", workspaceEntFields, suffix, args...)
	if err != nil {
		return nil, err
	}
	return filteredRecords(rows, accountID)
}

func (s *postgresEntStateStore) GetWorkspace(ctx context.Context, id string) (map[string]any, bool, error) {
	return s.getSQLRecord(ctx, s.db, "control_plane_workspaces", id, workspaceEntFields, false)
}

func (s *postgresEntStateStore) PageWorkspaces(ctx context.Context, accountID string, page tablePageQuery) (tablePage, error) {
	whereSQL := ""
	var args []any
	if accountID != "" {
		whereSQL = " WHERE (account_id = ? OR (account_id = '' AND owner_account_id = ?))"
		args = []any{accountID, accountID}
	}
	var total int
	if err := s.queryRow(ctx, s.db, "SELECT count(*) FROM control_plane_workspaces"+whereSQL, args...).Scan(&total); err != nil {
		return tablePage{}, err
	}
	pageArgs := append(append([]any(nil), args...), page.Limit, page.Offset)
	rows, err := s.loadSQLRecordSet(ctx, s.db, "control_plane_workspaces", workspaceEntFields, strings.TrimPrefix(whereSQL+" ORDER BY id ASC LIMIT ? OFFSET ?", " "), pageArgs...)
	if err != nil {
		return tablePage{}, err
	}
	items, err := filteredRecords(rows, accountID)
	sort.Slice(items, func(i, j int) bool { return stringValue(items[i]["id"]) < stringValue(items[j]["id"]) })
	return tablePage{Items: items, Total: total}, err
}

func (s *postgresEntStateStore) CountWorkspaces(ctx context.Context) (int, error) {
	var total int
	err := s.queryRow(ctx, s.db, "SELECT count(*) FROM control_plane_workspaces").Scan(&total)
	return total, err
}

func (s *postgresEntStateStore) CountWorkspacesByAccount(ctx context.Context, accountIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(accountIDs))
	for _, accountID := range accountIDs {
		counts[accountID] = 0
	}
	if len(counts) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(counts))
	for accountID := range counts {
		ids = append(ids, accountID)
	}
	sort.Strings(ids)
	marks := make([]string, len(ids))
	args := make([]any, 0, len(ids)*2)
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	args = append(args, args...)
	inList := strings.Join(marks, ", ")
	query := fmt.Sprintf("SELECT account_id, owner_account_id, count(*) FROM control_plane_workspaces WHERE (account_id IN (%s) OR (account_id = '' AND owner_account_id IN (%s))) GROUP BY account_id, owner_account_id", inList, inList)
	rows, err := s.queryRows(ctx, s.db, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var accountID, ownerAccountID string
		var count int
		if err := rows.Scan(&accountID, &ownerAccountID, &count); err != nil {
			return nil, err
		}
		resolved := firstNonEmpty(accountID, ownerAccountID)
		if _, ok := counts[resolved]; ok {
			counts[resolved] += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (s *postgresEntStateStore) SaveWorkspace(ctx context.Context, row map[string]any) error {
	if err := validateWorkspaceBillingState(row); err != nil {
		return err
	}
	row = cloneMap(row)
	if _, ok := row["customerProduct"]; !ok {
		row["customerProduct"] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.saveWorkspaceRecord(ctx, tx, row); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) ApplyWorkspaceRenewalIntent(ctx context.Context, update workspaceRenewalIntentCAS) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, wsFound, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", update.WorkspaceID, workspaceEntFields, true)
	if err != nil {
		return err
	}
	if !wsFound {
		return errWorkspaceRenewalCASConflict
	}
	currentAutoRenew, validAutoRenew := current["autoRenew"].(bool)
	if stringValue(current["accountId"]) != update.AccountID || stringValue(current["ownerUserId"]) != update.OwnerUserID ||
		stringValue(current["paidThrough"]) != update.ExpectedPaidThrough || !validAutoRenew || currentAutoRenew != update.ExpectedAutoRenew {
		return errWorkspaceRenewalCASConflict
	}
	operations, err := s.listSQLRecords(ctx, tx, "control_plane_runtime_operations", runtimeOpEntFields, "WHERE workspace_id = ?"+s.forUpdateClause(), update.WorkspaceID)
	if err != nil {
		return err
	}
	for _, row := range operations {
		if stringValue(row["id"]) == stringValue(update.CommandOperation["id"]) {
			return errWorkspaceRenewalCASConflict
		}
	}
	if runtimeOperationsVersion(operations, update.WorkspaceID) != update.ExpectedOperationsVersion {
		return errWorkspaceRenewalCASConflict
	}
	desired := cloneMap(current)
	desired["autoRenew"], desired["authorizedBy"], desired["authorizedAt"] = update.WorkspacePatch.AutoRenew, update.WorkspacePatch.AuthorizedBy, update.WorkspacePatch.AuthorizedAt
	if err := validateWorkspaceBillingState(desired); err != nil {
		return err
	}
	if err := validateWorkspaceRenewalIntentAudit(update, current); err != nil {
		return err
	}
	auditID := stringValue(update.AuditEvent["id"])
	auditEntity, auditExists, err := s.getSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, auditEntFields, true)
	if err != nil {
		return err
	}
	if auditExists && !workspaceRenewalIntentAuditIdentityMatches(auditEntity, update.AuditEvent) {
		return errIdempotencyConflict
	}
	if err := s.updateSQLRecord(ctx, tx, "control_plane_workspaces", update.WorkspaceID, desired, workspaceEntFields, true); err != nil {
		return err
	}
	command := controlPlaneRecord(update.CommandOperation)
	if err := s.insertSQLRecord(ctx, tx, "control_plane_runtime_operations", stringValue(command["id"]), command, runtimeOpEntFields); err != nil {
		if isSQLConstraintError(err) {
			return errWorkspaceRenewalCASConflict
		}
		return err
	}
	if !auditExists {
		audit := controlPlaneRecord(update.AuditEvent)
		if err := s.insertSQLRecord(ctx, tx, "control_plane_admin_audit_events", auditID, audit, auditEntFields); err != nil {
			if isSQLConstraintError(err) {
				return errIdempotencyConflict
			}
			return err
		}
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) PersistWorkspaceLaunchReconcile(ctx context.Context, update workspaceLaunchReconcileCAS) error {
	desired, err := decodeWorkspaceLaunchReconcileOperation(update.DesiredOperation)
	if err != nil {
		return errWorkspaceLaunchCASConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var projectedWorkspaceRow map[string]any
	if update.WorkspaceReceiptProjection != nil {
		projection := update.WorkspaceReceiptProjection
		if projection.AccountID != desired.stringFact("accountId") || projection.OwnerUserID != desired.stringFact("ownerUserId") ||
			projection.WorkspaceID != desired.stringFact("workspaceId") || projection.ReceiptID != desired.stringFact("receiptId") ||
			desired.Stage != contracts.StageSucceeded || desired.Status != contracts.StatusSucceeded {
			return errWorkspaceLaunchCASConflict
		}
		var wsFound bool
		projectedWorkspaceRow, wsFound, err = s.getSQLRecord(ctx, tx, "control_plane_workspaces", projection.WorkspaceID, workspaceEntFields, true)
		if err != nil {
			return err
		}
		if !wsFound {
			return errWorkspaceLaunchCASConflict
		}
		if firstNonEmpty(stringValue(projectedWorkspaceRow["accountId"]), stringValue(projectedWorkspaceRow["ownerAccountId"])) != projection.AccountID ||
			stringValue(projectedWorkspaceRow["ownerUserId"]) != projection.OwnerUserID || stringValue(projectedWorkspaceRow["id"]) != projection.WorkspaceID {
			return errWorkspaceLaunchCASConflict
		}
	}
	current, opFound, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", update.OperationID, runtimeOpEntFields, true)
	if err != nil {
		return err
	}
	if !opFound {
		return errWorkspaceLaunchCASConflict
	}
	if stringValue(current["result"]) != update.ExpectedOperationResult || !workspaceLaunchReconcileIdentityMatches(current, update.DesiredOperation) {
		return errWorkspaceLaunchCASConflict
	}
	if update.WorkspaceReceiptProjection != nil {
		projection := update.WorkspaceReceiptProjection
		existingReceiptID := stringValue(projectedWorkspaceRow["purchaseReceiptId"])
		if existingReceiptID != "" && existingReceiptID != projection.ReceiptID {
			return errIdempotencyConflict
		}
		if existingReceiptID == "" {
			if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_workspaces SET purchase_receipt_id = ?, updated_at = ? WHERE id = ?", projection.ReceiptID, time.Now().UTC(), projection.WorkspaceID); err != nil {
				return err
			}
		}
	}
	if err := s.updateSQLRecord(ctx, tx, "control_plane_runtime_operations", update.OperationID, update.DesiredOperation, runtimeOpEntFields, true); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) ClaimWorkspaceRenewal(ctx context.Context, claim workspaceRenewalClaimCAS) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	current, wsFound, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", claim.WorkspaceID, workspaceEntFields, true)
	if err != nil {
		return err
	}
	if !wsFound {
		return errWorkspaceRenewalCASConflict
	}
	autoRenew, validAutoRenew := current["autoRenew"].(bool)
	if stringValue(current["accountId"]) != claim.AccountID || stringValue(current["paidThrough"]) != claim.ExpectedPaidThrough || !validAutoRenew || autoRenew != claim.ExpectedAutoRenew {
		return errWorkspaceRenewalCASConflict
	}
	operations, err := s.listSQLRecords(ctx, tx, "control_plane_runtime_operations", runtimeOpEntFields, "WHERE workspace_id = ?"+s.forUpdateClause(), claim.WorkspaceID)
	if err != nil {
		return err
	}
	desiredID := stringValue(claim.DesiredOperation["id"])
	var existing map[string]any
	for _, row := range operations {
		if workspaceDeleteBlocksRenewal(row) {
			return errWorkspaceRenewalCASConflict
		}
		if stringValue(row["id"]) == desiredID {
			existing = row
		}
	}
	if runtimeOperationsVersion(operations, claim.WorkspaceID) != claim.ExpectedOperationsVersion {
		return errWorkspaceRenewalCASConflict
	}
	if claim.ExpectedOperationResult == "" {
		if existing != nil {
			return errWorkspaceRenewalCASConflict
		}
		if err := s.insertSQLRecord(ctx, tx, "control_plane_runtime_operations", desiredID, controlPlaneRecord(claim.DesiredOperation), runtimeOpEntFields); err != nil {
			if isSQLConstraintError(err) {
				return errWorkspaceRenewalCASConflict
			}
			return err
		}
	} else {
		if existing == nil || stringValue(existing["result"]) != claim.ExpectedOperationResult {
			return errWorkspaceRenewalCASConflict
		}
		if !workspaceRenewalClaimIdentityMatches(existing, claim.DesiredOperation) {
			return errIdempotencyConflict
		}
		if err := s.updateSQLRecord(ctx, tx, "control_plane_runtime_operations", desiredID, claim.DesiredOperation, runtimeOpEntFields, true); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) PersistWorkspaceRenewal(ctx context.Context, update workspaceRenewalPersistCAS) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var mergedWorkspace map[string]any
	if update.WorkspacePatch != nil {
		if update.WorkspaceID == "" || update.ExpectedWorkspacePaidThrough == "" || update.WorkspaceID != stringValue(update.DesiredOperation["workspaceId"]) {
			return errWorkspaceRenewalCASConflict
		}
		currentWorkspace, wsFound, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", update.WorkspaceID, workspaceEntFields, true)
		if err != nil {
			return err
		}
		if !wsFound {
			return sql.ErrNoRows
		}
		if stringValue(currentWorkspace["paidThrough"]) != update.ExpectedWorkspacePaidThrough ||
			!workspaceRenewalExpectedFieldsMatch(currentWorkspace, update.ExpectedWorkspaceFields) {
			return errWorkspaceRenewalCASConflict
		}
		mergedWorkspace, err = mergeWorkspaceRenewalPatch(currentWorkspace, update.WorkspacePatch)
		if err != nil {
			return err
		}
	} else if update.WorkspaceID != "" || update.ExpectedWorkspacePaidThrough != "" || len(update.ExpectedWorkspaceFields) != 0 {
		return errInvalidWorkspaceRenewalPatch
	}
	current, opFound, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", update.OperationID, runtimeOpEntFields, true)
	if err != nil {
		return err
	}
	if !opFound {
		return errWorkspaceRenewalCASConflict
	}
	if stringValue(current["result"]) != update.ExpectedOperationResult || stringValue(current["workspaceId"]) != stringValue(update.DesiredOperation["workspaceId"]) ||
		stringValue(current["action"]) != stringValue(update.DesiredOperation["action"]) {
		return errWorkspaceRenewalCASConflict
	}
	if mergedWorkspace != nil {
		if update.WorkspaceID != stringValue(current["workspaceId"]) {
			return errWorkspaceRenewalCASConflict
		}
		if err := s.updateSQLRecord(ctx, tx, "control_plane_workspaces", update.WorkspaceID, mergedWorkspace, workspaceEntFields, true); err != nil {
			return err
		}
	}
	if err := s.updateSQLRecord(ctx, tx, "control_plane_runtime_operations", update.OperationID, update.DesiredOperation, runtimeOpEntFields, true); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) ActivateWorkspaceLaunchProjection(ctx context.Context, row map[string]any) (map[string]any, error) {
	if err := validateWorkspaceBillingState(row); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	owner, ownerFound, err := s.getSQLRecord(ctx, tx, "control_plane_users", stringValue(row["ownerUserId"]), userEntFields, true)
	if err != nil {
		return nil, err
	}
	if !ownerFound {
		owner = nil
	}
	existing, existingFound, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", stringValue(row["id"]), workspaceEntFields, true)
	if err != nil {
		return nil, err
	}
	if !existingFound {
		existing = nil
	}
	prepared, err := prepareWorkspaceLaunchProjection(row, owner, existing)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := s.updateSQLRecord(ctx, tx, "control_plane_workspaces", stringValue(prepared["id"]), prepared, workspaceEntFields, true); err != nil {
			return nil, err
		}
	} else if err := s.insertSQLRecord(ctx, tx, "control_plane_workspaces", stringValue(prepared["id"]), prepared, workspaceEntFields); err != nil {
		if isSQLConstraintError(err) {
			return nil, errIdempotencyConflict
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (s *postgresEntStateStore) saveWorkspaceRecord(ctx context.Context, tx *sql.Tx, row map[string]any) error {
	id := stringValue(row["id"])
	if id == "" {
		return errors.New("missing_record_id")
	}
	existing, found, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", id, workspaceEntFields, true)
	if err != nil {
		return err
	}
	if found {
		row, err = mergeWorkspaceForSave(existing, row)
		if err != nil {
			return err
		}
		if err := validateWorkspaceBillingState(row); err != nil {
			return err
		}
		return s.updateSQLRecord(ctx, tx, "control_plane_workspaces", id, row, workspaceEntFields, true)
	}
	if err := s.insertSQLRecord(ctx, tx, "control_plane_workspaces", id, row, workspaceEntFields); isSQLConstraintError(err) {
		return errIdempotencyConflict
	} else {
		return err
	}
}

func (s *postgresEntStateStore) DeleteWorkspace(ctx context.Context, id string) error {
	return s.deleteSQLByID(ctx, s.db, "control_plane_workspaces", id)
}

func (s *postgresEntStateStore) ApplyWorkspaceDelete(ctx context.Context, mutation workspaceDeleteStoreMutation) error {
	desired, ok := validWorkspaceDeleteStoreMutation(mutation)
	if !ok {
		return errWorkspaceDeleteCASConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	workspaceRow, workspaceFound, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", desired.WorkspaceID, workspaceEntFields, true)
	if err != nil {
		return err
	}
	if mutation.RequireWorkspaceAbsent {
		if workspaceFound {
			return errWorkspaceDeleteCASConflict
		}
	} else {
		if !workspaceFound {
			return errWorkspaceDeleteCASConflict
		}
		if !workspaceDeleteWorkspaceProjectionMatches(desired, workspaceRow, mutation.DeleteWorkspace) {
			return errWorkspaceDeleteCASConflict
		}
	}
	if mutation.DeleteWorkspace {
		if err := s.validateWorkspaceDeleteResourceProjections(ctx, tx, desired); err != nil {
			return err
		}
	}
	if mutation.Create {
		operations, err := s.listSQLRecords(ctx, tx, "control_plane_runtime_operations", runtimeOpEntFields, "WHERE workspace_id = ?"+s.forUpdateClause(), desired.WorkspaceID)
		if err != nil {
			return err
		}
		for _, row := range operations {
			if workspaceRenewalBlocksDelete(row) || workspaceKeyRotationBlocksDelete(row) || workspaceDefaultApplicationPreparationBlocks(row) {
				return errWorkspaceDeleteCASConflict
			}
		}
	}

	current, opFound, err := s.getSQLRecord(ctx, tx, "control_plane_runtime_operations", desired.OperationID, runtimeOpEntFields, true)
	if err != nil {
		return err
	}
	if mutation.Create {
		if opFound {
			return errWorkspaceDeleteCASConflict
		}
		if err := s.insertSQLRecord(ctx, tx, "control_plane_runtime_operations", desired.OperationID, mutation.DesiredOperation, runtimeOpEntFields); err != nil {
			if isSQLConstraintError(err) {
				return errWorkspaceDeleteCASConflict
			}
			return err
		}
		if _, err := s.execSQL(ctx, tx, "UPDATE control_plane_workspaces SET state = 'deleting', status = 'deleting', updated_at = ? WHERE id = ?", time.Now().UTC(), desired.WorkspaceID); err != nil {
			return err
		}
	} else {
		if !opFound {
			return errWorkspaceDeleteCASConflict
		}
		currentOperation, decodeErr := decodeWorkspaceDeleteOperation(current)
		if decodeErr != nil || stringValue(current["result"]) != mutation.ExpectedResult || !workspaceDeleteOperationIdentityMatches(current, desired) ||
			!validWorkspaceDeleteTransition(currentOperation, desired, mutation) {
			return errWorkspaceDeleteCASConflict
		}
		if err := s.updateSQLRecord(ctx, tx, "control_plane_runtime_operations", desired.OperationID, mutation.DesiredOperation, runtimeOpEntFields, true); err != nil {
			return err
		}
	}
	if mutation.DeleteWorkspace {
		for _, target := range []struct {
			table string
			id    string
		}{
			{table: "control_plane_storage_attachments", id: desired.AttachmentID},
			{table: "control_plane_storage_volumes", id: desired.StorageID},
			{table: "control_plane_compute_allocations", id: desired.ComputeID},
			{table: "control_plane_workspaces", id: desired.WorkspaceID},
		} {
			if err := s.deleteSQLByID(ctx, tx, target.table, target.id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *postgresEntStateStore) validateWorkspaceDeleteResourceProjections(ctx context.Context, tx *sql.Tx, operation workspaceDeleteOperation) error {
	for _, resource := range []struct {
		kind   string
		table  string
		id     string
		fields []sqlRecordField
	}{
		{kind: "compute", table: "control_plane_compute_allocations", id: operation.ComputeID, fields: computeEntFields},
		{kind: "storage", table: "control_plane_storage_volumes", id: operation.StorageID, fields: storageEntFields},
		{kind: "attachment", table: "control_plane_storage_attachments", id: operation.AttachmentID, fields: attachmentEntFields},
	} {
		row, found, err := s.getSQLRecord(ctx, tx, resource.table, resource.id, resource.fields, true)
		if err != nil {
			return err
		}
		if found && !workspaceDeleteResourceProjectionMatches(operation, resource.kind, row) {
			return errWorkspaceDeleteCASConflict
		}
	}
	return nil
}

// ApplyWorkspaceResourceReconcile commits provider absence and its business
// consequence together, without touching retained financial operations.
func (s *postgresEntStateStore) ApplyWorkspaceResourceReconcile(ctx context.Context, mutation workspaceResourceReconcileMutation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	wsRow, found, err := s.getSQLRecord(ctx, tx, "control_plane_workspaces", mutation.WorkspaceID, workspaceEntFields, true)
	if err != nil {
		return err
	}
	if !found {
		return errWorkspaceResourceReconcileConflict
	}
	operations, err := s.listSQLRecords(ctx, tx, "control_plane_runtime_operations", runtimeOpEntFields, "WHERE workspace_id = ?"+s.forUpdateClause(), mutation.WorkspaceID)
	if err != nil {
		return err
	}
	desired, audit, err := prepareWorkspaceResourceReconcile(wsRow, operations, mutation)
	if err != nil {
		return err
	}
	if desired != nil {
		if err := s.updateSQLRecord(ctx, tx, "control_plane_workspaces", mutation.WorkspaceID, desired, workspaceEntFields, true); err != nil {
			return err
		}
		if err := s.insertSQLRecord(ctx, tx, "control_plane_admin_audit_events", stringValue(audit["id"]), controlPlaneRecord(audit), auditEntFields); err != nil {
			return err
		}
	}
	return tx.Commit()
}
