package server

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type operatorRuntimeObservation struct {
	ObjectRef     string                          `json:"objectRef,omitempty"`
	WorkspaceID   string                          `json:"workspaceId,omitempty"`
	RuntimeID     string                          `json:"runtimeId,omitempty"`
	BusinessState string                          `json:"businessState,omitempty"`
	DesiredState  contracts.ResourceObservedState `json:"desiredState,omitempty"`
	ObservedState contracts.ResourceObservedState `json:"observedState,omitempty"`
	Ownership     contracts.RuntimeOwnership      `json:"ownership,omitempty"`
	Status        string                          `json:"status"`
	ReasonCode    string                          `json:"reasonCode,omitempty"`
}

type operatorRuntimeObservations struct {
	ObservedAt     string                       `json:"observedAt"`
	Ready          bool                         `json:"ready"`
	BusinessTotal  int                          `json:"businessTotal"`
	ObservedTotal  int                          `json:"observedTotal"`
	RunningCount   int                          `json:"runningCount"`
	SuspendedCount int                          `json:"suspendedCount"`
	PendingCount   int                          `json:"pendingCount"`
	AttentionCount int                          `json:"attentionCount"`
	UnmatchedCount int                          `json:"unmatchedCount"`
	Items          []operatorRuntimeObservation `json:"items"`
}

func (app *controlPlaneServer) operatorRuntimeObservations(ctx context.Context, service *controlplane.Service, now time.Time) (operatorRuntimeObservations, error) {
	workspaces, err := app.tables.ListWorkspaces(ctx, "")
	if err != nil {
		return operatorRuntimeObservations{}, err
	}
	operations, err := app.tables.ListRuntimeOperations(ctx)
	if err != nil {
		return operatorRuntimeObservations{}, err
	}
	observations, err := service.RuntimeObservations(ctx)
	if err != nil {
		return operatorRuntimeObservations{}, err
	}
	rows := make(map[string]map[string]any, len(workspaces))
	byWorkspace := make(map[string][]map[string]any)
	for _, workspace := range workspaces {
		id := stringValue(workspace["id"])
		if id == "" || rows[id] != nil {
			return operatorRuntimeObservations{}, errors.New("operator_workspace_identity_conflict")
		}
		rows[id] = workspace
	}
	for _, operation := range operations {
		id := stringValue(operation["workspaceId"])
		if rows[id] != nil {
			byWorkspace[id] = append(byWorkspace[id], operation)
		}
	}
	counts := make(map[string]int)
	for _, observation := range observations.Items {
		if rows[observation.WorkspaceID] != nil {
			counts[observation.WorkspaceID]++
		}
	}
	result := operatorRuntimeObservations{ObservedAt: observations.ObservedAt, BusinessTotal: len(workspaces), ObservedTotal: len(observations.Items), Items: make([]operatorRuntimeObservation, 0, max(len(workspaces), len(observations.Items)))}
	appendItem := func(item operatorRuntimeObservation) {
		result.Items = append(result.Items, item)
		switch item.Status {
		case "running":
			result.RunningCount++
		case "suspended":
			result.SuspendedCount++
		case "pending":
			result.PendingCount++
		case "attention":
			result.AttentionCount++
		}
	}
	for _, observation := range observations.Items {
		item := operatorRuntimeObservation{ObjectRef: observation.ObjectRef, WorkspaceID: observation.WorkspaceID, RuntimeID: observation.RuntimeID, DesiredState: observation.DesiredState, ObservedState: observation.ObservedState, Ownership: observation.Ownership, Status: "attention"}
		workspace := rows[observation.WorkspaceID]
		if workspace == nil {
			item.ReasonCode = "runtime_unmatched_workspace"
			result.UnmatchedCount++
			appendItem(item)
			continue
		}
		item.BusinessState = firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"]))
		accountID := firstNonEmpty(stringValue(workspace["ownerAccountId"]), stringValue(workspace["accountId"]))
		switch {
		case observation.Ownership == contracts.RuntimeOwnershipConflict:
			item.ReasonCode = "runtime_ownership_conflict"
		case observation.Ownership != contracts.RuntimeOwnershipVerified:
			item.ReasonCode = "runtime_ownership_unregistered"
		case counts[observation.WorkspaceID] > 1:
			item.ReasonCode = "runtime_multiple_objects"
		case accountID == "" || observation.AccountID != accountID || stringValue(workspace["runtimeId"]) != "" && observation.RuntimeID != stringValue(workspace["runtimeId"]):
			item.ReasonCode = "runtime_binding_mismatch"
		default:
			item.Status, item.ReasonCode = operatorRuntimeState(workspace, byWorkspace[observation.WorkspaceID], &observation, now)
		}
		appendItem(item)
	}
	for _, workspace := range workspaces {
		id := stringValue(workspace["id"])
		if counts[id] != 0 {
			continue
		}
		item := operatorRuntimeObservation{ObservedState: contracts.ResourceObservedAbsent, WorkspaceID: id, RuntimeID: stringValue(workspace["runtimeId"]), BusinessState: firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"]))}
		item.Status, item.ReasonCode = operatorRuntimeState(workspace, byWorkspace[id], nil, now)
		appendItem(item)
	}
	sort.Slice(result.Items, func(i, j int) bool {
		left, right := result.Items[i], result.Items[j]
		if left.WorkspaceID != right.WorkspaceID {
			return left.WorkspaceID < right.WorkspaceID
		}
		return left.ObjectRef < right.ObjectRef
	})
	result.Ready = result.AttentionCount == 0
	return result, nil
}

func operatorRuntimeState(workspace map[string]any, operations []map[string]any, observation *contracts.RuntimeObservation, now time.Time) (string, string) {
	for _, row := range operations {
		if workspaceDeleteBlocksRenewal(row) {
			operation, err := decodeWorkspaceDeleteOperation(row)
			if err != nil {
				return "attention", "workspace_delete_state_invalid"
			}
			if operation.Status == "failed" || operation.Status == "manual_review" || operation.Phase == "complete" {
				return "attention", "workspace_delete_incomplete"
			}
			return "pending", "workspace_delete_in_progress"
		}
	}
	if observation == nil && stringValue(workspace["runtimeId"]) == "" {
		for _, row := range operations {
			if stringValue(row["action"]) != workspaceLaunchAction {
				continue
			}
			launch, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil || launch.stringFact("workspaceId") != stringValue(workspace["id"]) || launch.stringFact("accountId") != firstNonEmpty(stringValue(workspace["ownerAccountId"]), stringValue(workspace["accountId"])) {
				return "attention", "workspace_launch_state_invalid"
			}
			if slices.Index(workspaceLaunchReconcileStages, launch.Stage) <= slices.Index(workspaceLaunchReconcileStages, contracts.StageRuntime) && launch.Status != contracts.StatusSucceeded {
				if launch.Status == contracts.StatusFailed || launch.Status == contracts.StatusManualReview || launch.Status == contracts.StatusRefunded {
					return "attention", "workspace_runtime_not_created"
				}
				return "pending", "workspace_runtime_not_created"
			}
		}
	}
	suspended := workspaceLifecycleInactive(workspace)
	expired := false
	if !providerAcceptanceWorkspaceBillingExempt(workspace) {
		_, periodExpired, reason := workspaceBillingAccessFacts(workspace, now)
		expired = periodExpired
		if reason == "workspace_billing_state_invalid" {
			return "attention", reason
		}
		if reason == "workspace_billing_manual_review" && !expired && !suspended {
			return "attention", reason
		}
		suspended = suspended || expired
	}
	if suspended {
		if observation == nil {
			return "suspended", "runtime_absent_while_suspended"
		}
		if observation.DesiredState == contracts.ResourceObservedSuspended && observation.ObservedState == contracts.ResourceObservedSuspended {
			return "suspended", ""
		}
		for _, row := range operations {
			if stringValue(row["action"]) != "workspace.renewal" {
				continue
			}
			renewal, err := decodeWorkspaceRenewalOperation(row)
			if err != nil {
				return "attention", "workspace_renewal_state_invalid"
			}
			if renewal.AccountID != firstNonEmpty(stringValue(workspace["ownerAccountId"]), stringValue(workspace["accountId"])) {
				return "attention", "workspace_renewal_state_invalid"
			}
			if renewal.ExpiryPaidThrough == stringValue(workspace["paidThrough"]) && (renewal.ExpiryPhase == "suspend" || renewal.ExpiryPhase == "runtime_suspend") && renewal.ExpiryErrorCode == "" {
				return "pending", "runtime_suspend_incomplete"
			}
		}
		if expired {
			return "attention", "workspace_billing_period_expired"
		}
		return "attention", "runtime_suspend_incomplete"
	}
	if observation == nil {
		return "attention", "runtime_missing"
	}
	if observation.DesiredState == contracts.ResourceObservedSuspended {
		return "attention", "runtime_unexpected_suspension"
	}
	if observation.DesiredState == contracts.ResourceObservedRunning && observation.ObservedState == contracts.ResourceObservedRunning {
		return "running", ""
	}
	if observation.ObservedState == contracts.ResourceObservedPending && (observation.ReasonCode == "runtime_generation_pending" || operatorRuntimeTransitionPending(workspace, operations)) {
		return "pending", "runtime_not_ready"
	}
	return "attention", "runtime_not_ready"
}

func operatorRuntimeTransitionPending(workspace map[string]any, operations []map[string]any) bool {
	accountID := firstNonEmpty(stringValue(workspace["ownerAccountId"]), stringValue(workspace["accountId"]))
	workspaceID := stringValue(workspace["id"])
	for _, row := range operations {
		switch stringValue(row["action"]) {
		case workspaceLaunchAction:
			launch, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err == nil && launch.Status == contracts.StatusPending && launch.Stage == contracts.StageRuntime && launch.stringFact("accountId") == accountID && launch.stringFact("workspaceId") == workspaceID {
				return true
			}
		case workspaceRuntimeImageReplacementAction:
			_, status, err := decodeWorkspaceRuntimeImageReplacementOperation(row, accountID, workspaceID)
			if err == nil && status == "started" {
				return true
			}
		case "workspace.renewal":
			renewal, err := decodeWorkspaceRenewalOperation(row)
			if err == nil && renewal.AccountID == accountID && renewal.WorkspaceID == workspaceID && renewal.PaidThrough == stringValue(workspace["paidThrough"]) && renewal.Status == "verifying" && renewal.Phase != "receipt" && renewal.ExpiryStatus == "past_due" && renewal.ExpiryPaidThrough == renewal.PaidThrough {
				return true
			}
		}
	}
	return false
}
