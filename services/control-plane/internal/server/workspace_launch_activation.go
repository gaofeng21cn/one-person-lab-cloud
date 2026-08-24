package server

import (
	"context"
	"errors"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/domain"
)

func (a *controlPlaneWorkspaceLaunchStageAdapter) readWorkspaceLaunchActivation(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error) {
	workspace, found, err := a.app.tables.GetWorkspace(ctx, operation.stringFact("workspaceId"))
	if err != nil {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err
	}
	if !found {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}, nil
	}
	if !workspaceLaunchProjectionMatches(operation, workspace) {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, nil
	}
	activatedAt := firstNonEmpty(stringValue(workspace["activatedAt"]), stringValue(workspace["updatedAt"]), stringValue(workspace["createdAt"]))
	if activatedAt == "" {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, nil
	}
	return workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: map[string]any{
		"activationOperationId": operation.ID + ":activation", "workspaceActivatedAt": activatedAt,
	}}, nil
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) mutateWorkspaceLaunchActivation(ctx context.Context, operation workspaceLaunchReconcileOperation, idempotencyKey string) error {
	if idempotencyKey != operation.ID+":activation" {
		return errInvalidWorkspaceLaunchOperation
	}
	row, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		return err
	}
	_, err = a.app.tables.ActivateWorkspaceLaunchProjection(ctx, row)
	return err
}

func workspaceLaunchActivationRow(operation workspaceLaunchReconcileOperation) (map[string]any, error) {
	paidThrough, paidThroughErr := time.Parse(time.RFC3339, operation.stringFact("paidThrough"))
	if paidThroughErr != nil {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	resourceBillingEnabled := operation.raw["resourceBillingEnabled"] == nil || operation.boolFact("resourceBillingEnabled")
	autoRenew := operation.boolFact("autoRenew")
	if autoRenew && !resourceBillingEnabled {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	authorizedBy, authorizedAt := "", ""
	if autoRenew {
		authorizedBy, authorizedAt = operation.stringFact("ownerUserId"), operation.CreatedAt
	}
	computePrice, storagePrice, err := workspaceLaunchAcceptedPriceComponents(operation)
	if err != nil {
		return nil, err
	}
	row := workspaceProjectionRow(domain.WorkspaceProjection{
		ID: operation.stringFact("workspaceId"), AccountID: operation.stringFact("accountId"), OwnerID: operation.stringFact("ownerUserId"),
		Name: operation.stringFact("name"), PackageID: operation.stringFact("packageId"), Provider: "fabric", URL: operation.stringFact("url"), Status: "running",
		ComputeID: operation.stringFact("computeAllocationId"), VolumeID: operation.stringFact("storageId"), AttachmentID: operation.stringFact("attachmentId"),
		RuntimeID: operation.stringFact("runtimeId"), RuntimeServiceName: operation.stringFact("runtimeServiceName"), WorkspaceAPIKeyID: operation.int64Fact("workspaceApiKeyId"),
		RuntimeReady: true, RuntimeUsername: operation.stringFact("runtimeUsername"), CredentialStatus: operation.stringFact("credentialStatus"),
		CredentialVersion: operation.stringFact("credentialVersion"), CredentialSecretRef: operation.stringFact("credentialSecretRef"),
	})
	for key, value := range map[string]any{
		"resourceBillingEnabled": resourceBillingEnabled, "autoRenew": autoRenew, "authorizedBy": authorizedBy, "authorizedAt": authorizedAt, "priceVersion": operation.stringFact("priceVersion"), "currency": pricingCurrency,
		"billingUnit": pricingBillingUnit, "computeUsdMicros": computePrice, "storageUsdMicros": storagePrice, "totalUsdMicros": operation.int64Fact("totalChargeUsdMicros"),
		"periodStart": operation.stringFact("periodStart"), "paidThrough": operation.stringFact("paidThrough"), "nextRenewalAt": paidThrough.Add(-24 * time.Hour).Format(time.RFC3339Nano),
		"billingAnchorDay": operation.intFact("billingAnchorDay"), "renewalStatus": map[bool]string{true: "active", false: workspaceBillingNotApplicable}[resourceBillingEnabled], "computeAllocationId": operation.stringFact("computeAllocationId"),
		"storageId": operation.stringFact("storageId"), "storageGb": operation.intFact("sizeGb"), "activatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	} {
		row[key] = value
	}
	return row, nil
}

func workspaceLaunchProjectionMatches(operation workspaceLaunchReconcileOperation, workspace map[string]any) bool {
	return len(workspaceLaunchProjectionMismatchFields(operation, workspace)) == 0
}

func workspaceLaunchProjectionMismatchFields(operation workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	autoRenew := operation.boolFact("autoRenew")
	authorizedBy, authorizedAt := "", ""
	if autoRenew {
		authorizedBy, authorizedAt = operation.stringFact("ownerUserId"), operation.CreatedAt
	}
	mismatches := workspaceLaunchStableProjectionMismatchFields(operation, workspace)
	for _, check := range []struct {
		field string
		match bool
	}{
		{"workspace_api_key_id", int64(numberField(workspace, "workspaceApiKeyId", 0)) == operation.int64Fact("workspaceApiKeyId")},
		{"auto_renew", workspace["autoRenew"] == autoRenew},
		{"authorized_by", stringValue(workspace["authorizedBy"]) == authorizedBy},
		{"authorized_at", stringValue(workspace["authorizedAt"]) == authorizedAt},
	} {
		if !check.match {
			mismatches = append(mismatches, check.field)
		}
	}
	return mismatches
}

func workspaceLaunchAccessProjectionMismatchFields(operation workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	mismatches := workspaceLaunchStableProjectionMismatchFields(operation, workspace)
	if int64(numberField(workspace, "workspaceApiKeyId", 0)) != operation.int64Fact("workspaceApiKeyId") {
		mismatches = append(mismatches, "workspace_api_key_id")
	}
	return mismatches
}

func workspaceLaunchStableProjectionMatches(operation workspaceLaunchReconcileOperation, workspace map[string]any) bool {
	return len(workspaceLaunchStableProjectionMismatchFields(operation, workspace)) == 0
}

func workspaceLaunchStableProjectionMismatchFields(operation workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	checks := []struct {
		field string
		match bool
	}{
		{"account_id", firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) == operation.stringFact("accountId")},
		{"owner_user_id", stringValue(workspace["ownerUserId"]) == operation.stringFact("ownerUserId")},
		{"workspace_id", stringValue(workspace["id"]) == operation.stringFact("workspaceId")},
		{"name", stringValue(workspace["name"]) == operation.stringFact("name")},
		{"package_id", stringValue(workspace["packageId"]) == operation.stringFact("packageId")},
		{"url", stringValue(workspace["url"]) == operation.stringFact("url")},
		{"compute_allocation_id", firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])) == operation.stringFact("computeAllocationId")},
		{"storage_id", stringValue(workspace["storageId"]) == operation.stringFact("storageId")},
		{"attachment_id", firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])) == operation.stringFact("attachmentId")},
		{"runtime_id", stringValue(workspace["runtimeId"]) == operation.stringFact("runtimeId")},
		{"runtime_service_name", stringValue(nested(workspace, "runtime", "serviceName")) == operation.stringFact("runtimeServiceName")},
		{"runtime_username", stringValue(nested(workspace, "access", "username")) == operation.stringFact("runtimeUsername")},
		{"credential_status", stringValue(nested(workspace, "access", "credentialStatus")) == operation.stringFact("credentialStatus")},
		{"credential_version", stringValue(nested(workspace, "access", "credentialVersion")) == operation.stringFact("credentialVersion")},
		{"credential_secret_ref", stringValue(nested(workspace, "access", "secretRef")) == operation.stringFact("credentialSecretRef")},
		{"price_version", stringValue(workspace["priceVersion"]) == operation.stringFact("priceVersion")},
		{"total_usd_micros", int64(numberField(workspace, "totalUsdMicros", 0)) == operation.int64Fact("totalChargeUsdMicros")},
		{"period_start", stringValue(workspace["periodStart"]) == operation.stringFact("periodStart")},
		{"paid_through", stringValue(workspace["paidThrough"]) == operation.stringFact("paidThrough")},
		{"billing_anchor_day", int(numberField(workspace, "billingAnchorDay", 0)) == operation.intFact("billingAnchorDay")},
		{"storage_gb", int(numberField(workspace, "storageGb", 0)) == operation.intFact("sizeGb")},
		{"workspace_running", firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) == "running"},
	}
	mismatches := make([]string, 0)
	for _, check := range checks {
		if !check.match {
			mismatches = append(mismatches, check.field)
		}
	}
	return mismatches
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) readWorkspaceLaunchReceipt(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error) {
	input, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err
	}
	expected := []clients.ReceiptInput{input}
	if operation.raw["resourceBillingEnabled"] != nil && !operation.boolFact("resourceBillingEnabled") {
		expected = append(expected, workspaceLaunchLegacyCreatedReceiptInput(operation))
	} else {
		expected = append(expected, workspaceLaunchHistoricalChargedReceiptInput(input))
	}
	receipt, found, err := workspaceLaunchPurchaseReceiptFromLedger(ctx, a, expected)
	if err != nil {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err
	}
	if !found {
		return workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}, nil
	}
	return workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: map[string]any{
		"receiptId": receipt.ReceiptID, "receiptOperationId": operation.ID + ":purchase-receipt",
	}}, nil
}

func workspaceLaunchHistoricalChargedReceiptInput(current clients.ReceiptInput) clients.ReceiptInput {
	historical := current
	historical.Execution = map[string]any{
		"resourceType": current.Execution["resourceType"], "resourceId": current.Execution["resourceId"],
		"computeAllocationId": current.Execution["computeAllocationId"], "storageId": current.Execution["storageId"],
		"attachmentId": current.Execution["attachmentId"], "runtimeId": current.Execution["runtimeId"],
		"workspaceApiKeyId": current.Execution["workspaceApiKeyId"], "workspaceKeyFingerprint": current.Execution["workspaceKeyFingerprint"],
		"runtimeServiceName": current.Execution["runtimeServiceName"],
	}
	return historical
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) mutateWorkspaceLaunchReceipt(ctx context.Context, operation workspaceLaunchReconcileOperation, idempotencyKey string) error {
	if idempotencyKey != operation.ID+":purchase-receipt" {
		return errInvalidWorkspaceLaunchOperation
	}
	input, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		return err
	}
	_, err = a.service.RecordMonthlyReceipt(ctx, input, idempotencyKey)
	return err
}

func workspaceLaunchPurchaseReceiptInput(operation workspaceLaunchReconcileOperation) (clients.ReceiptInput, error) {
	execution := workspaceLaunchCanonicalReceiptExecution(operation)
	owner := map[string]any{"accountId": operation.stringFact("accountId"), "workspaceId": operation.stringFact("workspaceId"), "ownerUserId": operation.stringFact("ownerUserId")}
	computePrice, storagePrice, err := workspaceLaunchAcceptedPriceComponents(operation)
	if err != nil {
		return clients.ReceiptInput{}, err
	}
	if operation.raw["resourceBillingEnabled"] != nil && !operation.boolFact("resourceBillingEnabled") {
		return clients.ReceiptInput{Type: "workspace.created", Status: "completed", Surface: "control_plane", AccountID: operation.stringFact("accountId"), WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID, Execution: execution, Owner: owner}, nil
	}
	return clients.ReceiptInput{
		Type: "billing.workspace_purchased.v1", Status: "completed", Surface: "control_plane", AccountID: operation.stringFact("accountId"),
		WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID,
		Execution: execution,
		Cost: map[string]any{"priceVersion": operation.stringFact("priceVersion"), "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
			"totalUsdMicros": operation.int64Fact("totalChargeUsdMicros"), "sub2apiUserId": operation.int64Fact("sub2apiUserId"),
			"sub2apiRedeemCode": operation.stringFact("sub2apiRedeemCode"), "postChargeBalanceUsdMicros": operation.int64Fact("postChargeBalanceUsdMicros"),
			"periodStart": operation.stringFact("periodStart"), "paidThrough": operation.stringFact("paidThrough"), "resourceType": "workspace", "resourceId": operation.stringFact("workspaceId"),
			"components": map[string]any{"compute": map[string]any{"resourceType": "compute", "resourceId": operation.stringFact("computeAllocationId"), "chargeUsdMicros": computePrice},
				"storage": map[string]any{"resourceType": "storage", "resourceId": operation.stringFact("storageId"), "sizeGb": int64(operation.intFact("sizeGb")), "chargeUsdMicros": storagePrice}}},
		Owner: owner,
	}, nil
}

func workspaceLaunchAcceptedPriceComponents(operation workspaceLaunchReconcileOperation) (int64, int64, error) {
	catalog, found := pricingCatalogByVersion(operation.stringFact("priceVersion"))
	if !found {
		return 0, 0, errors.New("workspace_launch_price_version_unknown")
	}
	resourceBillingEnabled := operation.raw["resourceBillingEnabled"] == nil || operation.boolFact("resourceBillingEnabled")
	if !resourceBillingEnabled {
		if operation.int64Fact("totalChargeUsdMicros") != 0 {
			return 0, 0, errors.New("workspace_launch_pricing_snapshot_invalid")
		}
		return 0, 0, nil
	}
	quote, err := workspacePricingPreview(catalog, map[string]any{"packageId": operation.stringFact("packageId"), "sizeGb": operation.intFact("sizeGb")})
	if err != nil {
		return 0, 0, err
	}
	computePrice, computeOK := requiredPositiveInteger(mapField(quote, "compute"), "chargeUsdMicros")
	storagePrice, storageOK := requiredPositiveInteger(mapField(quote, "storage"), "chargeUsdMicros")
	totalPrice, totalOK := requiredPositiveInteger(quote, "totalChargeUsdMicros")
	if !computeOK || !storageOK || !totalOK || totalPrice != operation.int64Fact("totalChargeUsdMicros") {
		return 0, 0, errors.New("workspace_launch_pricing_snapshot_invalid")
	}
	return computePrice, storagePrice, nil
}

func workspaceLaunchCanonicalReceiptExecution(operation workspaceLaunchReconcileOperation) map[string]any {
	return map[string]any{
		"operationId": operation.ID, "resourceType": "workspace", "resourceId": operation.stringFact("workspaceId"),
		"computeAllocationId": operation.stringFact("computeAllocationId"), "storageId": operation.stringFact("storageId"), "attachmentId": operation.stringFact("attachmentId"),
		"runtimeId": operation.stringFact("runtimeId"), "workspaceApiKeyId": operation.int64Fact("workspaceApiKeyId"),
		"workspaceKeyFingerprint": operation.stringFact("workspaceKeyFingerprint"), "runtimeServiceName": operation.stringFact("runtimeServiceName"),
		"gatewaySecretRef": operation.stringFact("gatewaySecretRef"),
	}
}

func workspaceLaunchLegacyCreatedReceiptInput(operation workspaceLaunchReconcileOperation) clients.ReceiptInput {
	requestID := operation.ID + ":purchase-receipt"
	return clients.ReceiptInput{
		Type: "workspace.created", Status: "completed", Surface: "workspace", AccountID: operation.stringFact("accountId"), WorkspaceID: operation.stringFact("workspaceId"),
		RequestID: requestID, Execution: map[string]any{"operationId": requestID, "runtimeId": operation.stringFact("runtimeId")},
		OutputRefs: map[string]any{"url": operation.stringFact("url")}, Owner: map[string]any{"accountId": operation.stringFact("accountId"), "userId": operation.stringFact("ownerUserId")},
	}
}
