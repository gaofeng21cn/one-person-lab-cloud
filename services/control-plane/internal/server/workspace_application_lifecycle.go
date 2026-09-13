package server

import (
	"context"
	"errors"
	"sort"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The snapshot is persisted before the first external mutation. An empty
// snapshot proves that inventory was read; a nil snapshot has not been read.
type workspaceApplicationLifecycleOperation struct {
	Runtimes []workspaceApplicationLifecycleRuntime `json:"runtimes"`
}

type workspaceApplicationLifecycleRuntime struct {
	Input          contracts.WorkspaceApplicationRuntimeLifecycleInput  `json:"input"`
	IdempotencyKey string                                               `json:"idempotencyKey"`
	Result         contracts.WorkspaceApplicationRuntimeLifecycleResult `json:"result"`
}

func validWorkspaceApplicationLifecycle(operation *workspaceApplicationLifecycleOperation, accountID, workspaceID, operationID, desired string) bool {
	if operation == nil {
		return true // Retained lifecycle records predate application inventory.
	}
	seen := map[string]bool{}
	for _, runtime := range operation.Runtimes {
		input := runtime.Input
		deploymentID := strings.TrimSuffix(input.RuntimeOperationID, ":runtime")
		runtimeID := contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID)
		if input.HistoricalApplicationRuntime {
			runtimeID = contracts.WorkspaceApplicationHistoricalRuntimeID(workspaceID)
		}
		if input.AccountID != accountID || input.WorkspaceID != workspaceID ||
			deploymentID == "" || deploymentID == input.RuntimeOperationID || seen[input.RuntimeOperationID] || input.DesiredState != desired ||
			input.LegacyRuntime ||
			input.RuntimeID != runtimeID ||
			runtime.IdempotencyKey != operationID+":application:"+deploymentID+":"+desired {
			return false
		}
		if !workspaceApplicationLifecycleReadbackValid(input, runtime.Result) {
			return false
		}
		seen[input.RuntimeOperationID] = true
	}
	return true
}

func workspaceApplicationLifecycleReadbackValid(input contracts.WorkspaceApplicationRuntimeLifecycleInput, result contracts.WorkspaceApplicationRuntimeLifecycleResult) bool {
	if result.RuntimeID != input.RuntimeID || result.WorkspaceID != input.WorkspaceID {
		return false
	}
	switch result.State {
	case "pending", "running", "suspended", "absent":
		return true
	default:
		return false
	}
}

func workspaceApplicationLifecycleTargetsMatch(current, desired *workspaceApplicationLifecycleOperation) bool {
	if current == nil {
		return true
	}
	if desired == nil || len(current.Runtimes) != len(desired.Runtimes) {
		return false
	}
	for index := range current.Runtimes {
		if current.Runtimes[index].Input != desired.Runtimes[index].Input || current.Runtimes[index].IdempotencyKey != desired.Runtimes[index].IdempotencyKey {
			return false
		}
	}
	return true
}

// Purchase/resource proof deliberately has no application Runtime fields.
// The current deployment is proved independently by its selected operation.
func workspaceLaunchResourceProjectionMismatchFields(launch workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	checks := []struct {
		field string
		valid bool
	}{
		{"account_id", firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) == launch.stringFact("accountId")},
		{"owner_user_id", firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"])) == launch.stringFact("ownerUserId")},
		{"workspace_id", stringValue(workspace["id"]) == launch.stringFact("workspaceId")},
		{"name", stringValue(workspace["name"]) == launch.stringFact("name")},
		{"package_id", stringValue(workspace["packageId"]) == launch.stringFact("packageId")},
		{"compute_allocation_id", firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])) == launch.stringFact("computeAllocationId")},
		{"storage_id", stringValue(workspace["storageId"]) == launch.stringFact("storageId")},
		{"attachment_id", firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])) == launch.stringFact("attachmentId")},
		{"price_version", stringValue(workspace["priceVersion"]) == launch.stringFact("priceVersion")},
		{"total_usd_micros", int64(numberField(workspace, "totalUsdMicros", 0)) == launch.int64Fact("totalChargeUsdMicros")},
		{"billing_anchor_day", int(numberField(workspace, "billingAnchorDay", 0)) == launch.intFact("billingAnchorDay")},
		{"storage_gb", int(numberField(workspace, "storageGb", 0)) == launch.intFact("sizeGb")},
	}
	var mismatches []string
	for _, check := range checks {
		if !check.valid {
			mismatches = append(mismatches, check.field)
		}
	}
	return mismatches
}

func (app *controlPlaneServer) workspaceApplicationLifecycleInventory(ctx context.Context, service *controlplane.Service, workspace map[string]any, desired, operationID string) (*workspaceApplicationLifecycleOperation, error) {
	var intents []workspaceApplicationDeploymentIntent
	if desired == "running" {
		current, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
		if err != nil {
			return nil, err
		}
		if found {
			intents = append(intents, current)
		}
	} else {
		var err error
		intents, err = app.ownedWorkspaceApplicationDeployments(ctx, workspace)
		if err != nil {
			return nil, err
		}
	}
	var historicalID string
	for _, intent := range intents {
		if intent.Version == 1 {
			owner, err := app.workspaceApplicationHistoricalLifecycleOwner(ctx, workspace, intents)
			if err != nil {
				return nil, err
			}
			historicalID = owner
			break
		}
	}
	result := &workspaceApplicationLifecycleOperation{Runtimes: make([]workspaceApplicationLifecycleRuntime, 0, len(intents))}
	sort.Slice(intents, func(i, j int) bool { return intents[i].OperationID < intents[j].OperationID })
	for _, intent := range intents {
		if intent.Version == 1 && intent.OperationID != historicalID {
			continue
		}
		runtimeOperationID := intent.OperationID + ":runtime"
		runtimeID := contracts.WorkspaceApplicationRuntimeID(runtimeOperationID)
		if intent.Version == 1 {
			input, err := app.workspaceApplicationRuntimeInput(ctx, intent)
			if err != nil {
				return nil, err
			}
			observation, err := service.ReadWorkspaceApplicationRuntime(ctx, input)
			if err != nil {
				return nil, err
			}
			runtimeID = contracts.WorkspaceApplicationHistoricalRuntimeID(intent.WorkspaceID)
			if observation.WorkspaceID != intent.WorkspaceID || observation.RuntimeID != runtimeID || contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, observation) != nil {
				return nil, errors.New("workspace_application_lifecycle_readback_invalid")
			}
		}
		binding := contracts.WorkspaceApplicationRuntimeLifecycleInput{
			AccountID: intent.AccountID, WorkspaceID: intent.WorkspaceID,
			RuntimeID: runtimeID, RuntimeOperationID: runtimeOperationID,
			DesiredState: desired, HistoricalApplicationRuntime: intent.Version == 1,
		}
		result.Runtimes = append(result.Runtimes, workspaceApplicationLifecycleRuntime{Input: binding, IdempotencyKey: operationID + ":application:" + intent.OperationID + ":" + desired, Result: contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: binding.RuntimeID, WorkspaceID: binding.WorkspaceID, State: "pending"}})
	}
	return result, nil
}

func (app *controlPlaneServer) workspaceApplicationHistoricalLifecycleOwner(ctx context.Context, workspace map[string]any, intents []workspaceApplicationDeploymentIntent) (string, error) {
	selected, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found {
		return "", errors.New("workspace_application_historical_selection_unconfirmed")
	}
	byID := make(map[string]workspaceApplicationDeploymentIntent, len(intents))
	for _, intent := range intents {
		byID[intent.OperationID] = intent
	}
	seen := map[string]bool{}
	for {
		if seen[selected.OperationID] {
			return "", errors.New("workspace_application_historical_lineage_invalid")
		}
		seen[selected.OperationID] = true
		if selected.Version == 1 {
			return selected.OperationID, nil
		}
		previous, found := byID[selected.PreviousDeploymentID]
		if !found || previous.ActivationAt == "" || selected.CurrentBinding != previous.ApplicationID+"@"+previous.TargetRevision || selected.ExpectedWorkspaceVersion != previous.ExpectedWorkspaceVersion+1 {
			return "", errors.New("workspace_application_historical_lineage_invalid")
		}
		selected = previous
	}
}

func workspaceApplicationLifecycleComplete(operation *workspaceApplicationLifecycleOperation, desired string) bool {
	if operation == nil {
		return false
	}
	for _, runtime := range operation.Runtimes {
		if runtime.Result.State != desired && !(desired == "suspended" && runtime.Result.State == "absent") {
			return false
		}
	}
	return true
}

func (app *controlPlaneServer) convergeWorkspaceApplicationLifecycle(ctx context.Context, service *controlplane.Service, operation *workspaceApplicationLifecycleOperation, persist func() error) error {
	var failures []error
	for index := range operation.Runtimes {
		current := operation.Runtimes[index]
		result, err := service.ReadWorkspaceApplicationRuntimeLifecycle(ctx, current.Input)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !workspaceApplicationLifecycleReadbackValid(current.Input, result) {
			failures = append(failures, errors.New("workspace_application_lifecycle_readback_invalid"))
			continue
		}
		if result.State != current.Input.DesiredState && result.State != "absent" || current.Input.DesiredState == "absent" && current.Result.State != "absent" || current.Input.DesiredState == "suspended" && current.Result.State == "pending" {
			result, err = service.SetWorkspaceApplicationRuntimeLifecycle(ctx, current.Input, current.IdempotencyKey)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if !workspaceApplicationLifecycleReadbackValid(current.Input, result) {
				failures = append(failures, errors.New("workspace_application_lifecycle_readback_invalid"))
				continue
			}
		}
		operation.Runtimes[index].Result = result
		if err := persist(); err != nil {
			return err
		}
		if result.State == "absent" && current.Input.DesiredState == "running" {
			failures = append(failures, errWorkspaceRenewalResourcesReclaimed)
			continue
		}
		if result.State != current.Input.DesiredState && !(current.Input.DesiredState == "suspended" && result.State == "absent") {
			failures = append(failures, errors.New("workspace_application_lifecycle_pending"))
		}
	}
	return errors.Join(failures...)
}
