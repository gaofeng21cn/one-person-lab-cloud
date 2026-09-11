package server

import (
	"context"
	"errors"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const workspaceResourceReconcileAction = "workspace.resource_reconcile"

var errWorkspaceResourceReconcileConflict = errors.New("workspace_resource_reconcile_conflict")

// Resource loss changes availability, never the retained purchase or money outcome.
// The existing Workspace row is the durable retry target for Runtime suspension.
type workspaceResourceReconcileMutation struct {
	WorkspaceID               string
	ExpectedWorkspaceVersion  string
	ExpectedOperationsVersion string
	Missing                   clients.ProviderFact
	CheckedAt                 time.Time
}

func workspaceResourceReconcileVersion(workspace map[string]any) string {
	return stableID(string(mustJSON(workspace)))
}

func workspaceResourceReconcileLaunch(workspace map[string]any, operations []map[string]any) (workspaceLaunchReconcileOperation, bool) {
	var launch workspaceLaunchReconcileOperation
	count := 0
	for _, row := range operations {
		if stringValue(row["workspaceId"]) != stringValue(workspace["id"]) || stringValue(row["action"]) != workspaceLaunchAction {
			continue
		}
		count++
		var err error
		launch, err = decodeWorkspaceLaunchReconcileOperation(row)
		if err != nil {
			return launch, false
		}
	}
	return launch, count == 1 && launch.Status == contracts.StatusSucceeded && launch.Stage == contracts.StageSucceeded &&
		launch.stringFact("workspaceId") == stringValue(workspace["id"]) &&
		launch.stringFact("accountId") == firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) &&
		launch.stringFact("computeAllocationId") != "" && launch.stringFact("computeAllocationId") == firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])) &&
		launch.stringFact("storageId") != "" && launch.stringFact("storageId") == stringValue(workspace["storageId"]) &&
		launch.stringFact("runtimeId") != "" && launch.stringFact("runtimeId") == stringValue(workspace["runtimeId"]) &&
		launch.stringFact("runtimeBindingRef") != "" && launch.stringFact("receiptId") != ""
}

func workspaceResourceReconcileBlocked(operations []map[string]any) bool {
	for _, row := range operations {
		if workspaceDeleteBlocksRenewal(row) || stringValue(row["action"]) == workspaceDeleteLegacyAction || workspaceRenewalBlocksDelete(row) || workspaceKeyRotationBlocksDelete(row) {
			return true
		}
		if stringValue(row["action"]) == workspaceRuntimeImageReplacementAction && stringValue(row["status"]) == "started" {
			return true
		}
	}
	return false
}

func workspaceResourceMissing(input clients.ProviderFactInput, fact clients.ProviderFact, checkedAt time.Time) bool {
	if !providerFactConfirmedAbsent(input, fact) {
		return false
	}
	readAt, ok := parseTimeString(fact.Facts.LastReadAt)
	return ok && !readAt.After(checkedAt) && checkedAt.Sub(readAt) <= providerFreshnessWindow()
}

func prepareWorkspaceResourceReconcile(current map[string]any, operations []map[string]any, mutation workspaceResourceReconcileMutation) (map[string]any, map[string]any, error) {
	if current == nil || mutation.WorkspaceID == "" || stringValue(current["id"]) != mutation.WorkspaceID ||
		workspaceResourceReconcileVersion(current) != mutation.ExpectedWorkspaceVersion || runtimeOperationsVersion(operations, mutation.WorkspaceID) != mutation.ExpectedOperationsVersion || workspaceResourceReconcileBlocked(operations) {
		return nil, nil, errWorkspaceResourceReconcileConflict
	}
	launch, valid := workspaceResourceReconcileLaunch(current, operations)
	if !valid {
		return nil, nil, errWorkspaceResourceReconcileConflict
	}
	input := clients.ProviderFactInput{AccountID: launch.stringFact("accountId"), WorkspaceID: mutation.WorkspaceID, ResourceType: mutation.Missing.ResourceType}
	switch input.ResourceType {
	case "compute":
		input.ResourceID = launch.stringFact("computeAllocationId")
	case "storage":
		input.ResourceID = launch.stringFact("storageId")
	default:
		return nil, nil, errWorkspaceResourceReconcileConflict
	}
	if !workspaceResourceMissing(input, mutation.Missing, mutation.CheckedAt) {
		return nil, nil, errProviderFactsInvalid
	}
	desired := cloneMap(current)
	desired["autoRenew"] = false
	if input.ResourceType == "storage" {
		desired["state"], desired["status"] = "data_deleted", "unrecoverable"
	} else {
		desired["state"], desired["status"] = "suspended", "suspended"
	}
	preserveWorkspaceStorageLoss(current, desired)
	if workspaceResourceReconcileVersion(desired) == workspaceResourceReconcileVersion(current) {
		return nil, nil, nil
	}
	if err := validateWorkspaceBillingState(desired); err != nil {
		return nil, nil, err
	}
	audit := map[string]any{
		"id":     "audit-" + stableID(workspaceResourceReconcileAction, mutation.WorkspaceID, mutation.ExpectedWorkspaceVersion, mutation.Missing.ResourceType, mutation.Missing.ResourceID, mutation.Missing.Facts.LastReadAt)[:18],
		"action": workspaceResourceReconcileAction, "actorRole": "system", "actorUserId": "provider-reconcile-worker",
		"targetAccountId": input.AccountID, "resourceKind": "workspace", "resourceId": mutation.WorkspaceID,
		"before": workspaceResourceReconcileState(current),
		"after":  map[string]any{"workspace": workspaceResourceReconcileState(desired), "providerFact": mutation.Missing, "reason": "provider_resource_absent"},
		"result": "succeeded", "createdAt": mutation.CheckedAt.UTC().Format(time.RFC3339Nano),
	}
	return desired, audit, nil
}

// A later expiry or stale projection cannot make a destroyed disk recoverable.
// Financial facts may continue to reconcile, but lost storage never reopens.
func preserveWorkspaceStorageLoss(current, desired map[string]any) {
	switch firstNonEmpty(stringValue(current["state"]), stringValue(current["status"])) {
	case "data_deleted", "unrecoverable", "storage_missing", "destroyed":
		for _, key := range []string{"state", "status", "currentComputeAllocationId", "currentAttachmentId"} {
			desired[key] = current[key]
		}
		desired["autoRenew"] = false
	}
}

func workspaceResourceReconcileState(workspace map[string]any) map[string]any {
	return map[string]any{"state": workspace["state"], "status": workspace["status"], "autoRenew": workspace["autoRenew"]}
}

func (app *controlPlaneServer) reconcileWorkspaceResources(ctx context.Context, service *controlplane.Service, workspaceID string, now time.Time) error {
	// Match the existing image-replacement lock order. Store CAS also rejects
	// another replica's Workspace or operation change before the state write.
	unlockDelete, err := app.lockResourceContext(ctx, "workspace-delete", workspaceID)
	if err != nil {
		return err
	}
	defer unlockDelete()
	unlockRenewal, err := app.lockResourceContext(ctx, "workspace-renewal", workspaceID)
	if err != nil {
		return err
	}
	defer unlockRenewal()
	workspace, found, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil || !found {
		return err
	}
	operations, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	launch, valid := workspaceResourceReconcileLaunch(workspace, operations)
	if !valid || workspaceResourceReconcileBlocked(operations) {
		return nil
	}
	inputs := []clients.ProviderFactInput{
		{AccountID: launch.stringFact("accountId"), WorkspaceID: workspaceID, ResourceType: "storage", ResourceID: launch.stringFact("storageId")},
		{AccountID: launch.stringFact("accountId"), WorkspaceID: workspaceID, ResourceType: "compute", ResourceID: launch.stringFact("computeAllocationId")},
	}
	facts, readErr := readProviderFacts(ctx, service, inputs)
	checkedAt := time.Now().UTC()
	if now.After(checkedAt) {
		checkedAt = now
	}
	if readErr != nil {
		return readErr
	}
	for _, input := range inputs {
		fact := facts[providerFactKey(input)]
		if !workspaceResourceMissing(input, fact, checkedAt) {
			continue
		}
		mutation := workspaceResourceReconcileMutation{WorkspaceID: workspaceID, ExpectedWorkspaceVersion: workspaceResourceReconcileVersion(workspace), ExpectedOperationsVersion: runtimeOperationsVersion(operations, workspaceID), Missing: fact, CheckedAt: checkedAt}
		if err := app.tables.ApplyWorkspaceResourceReconcile(ctx, mutation); err != nil {
			return err
		}
		// Storage absence is decisive even when compute ownership is unknown.
		// Compute absence alone preserves the disk and all original identities.
		return app.suspendWorkspaceForMissingResource(ctx, service, launch, workspace, fact)
	}
	return nil
}

func (app *controlPlaneServer) suspendWorkspaceForMissingResource(ctx context.Context, service *controlplane.Service, launch workspaceLaunchReconcileOperation, workspace map[string]any, missing clients.ProviderFact) error {
	input := contracts.WorkspaceRuntimePowerInput{
		SchemaVersion: 1, AccountID: launch.stringFact("accountId"), WorkspaceID: launch.stringFact("workspaceId"),
		RuntimeID: launch.stringFact("runtimeId"), RuntimeOperationID: launch.stringFact("runtimeBindingRef"),
		PaidThrough: stringValue(workspace["paidThrough"]), DesiredState: "suspended",
		SuspensionReason:    contracts.WorkspaceRuntimeSuspensionProviderResourceAbsent,
		MissingResourceType: missing.ResourceType, MissingResourceID: missing.ResourceID,
		IdempotencyKey: workspaceResourceReconcileAction + ":" + stableID(launch.ID, missing.ResourceType, missing.ResourceID, stringValue(workspace["paidThrough"])),
	}
	result, err := service.ReadWorkspaceRuntimePower(ctx, input)
	if err != nil {
		return err
	}
	if result.SchemaVersion != 1 || result.Binding != input {
		return errors.New("workspace_runtime_power_readback_invalid")
	}
	if result.State == "suspended" || result.State == "absent" {
		return nil
	}
	result, err = service.SetWorkspaceRuntimePower(ctx, input)
	if err != nil {
		return err
	}
	if result.SchemaVersion != 1 || result.Binding != input {
		return errors.New("workspace_runtime_power_readback_invalid")
	}
	if result.State != "suspended" && result.State != "absent" {
		return errors.New("workspace_runtime_suspension_pending")
	}
	return nil
}
