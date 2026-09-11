package server

import (
	"encoding/json"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

// workspaceLaunchProvisioningModeFact persists the operation's provisioning
// mode. Retained rows predate the fact and keep the full Launch contract by
// default, so it is written only for resource-only operations.
const workspaceLaunchProvisioningModeFact = "provisioningMode"

// workspaceLaunchApplicationFactFields are the operation facts that only the
// application-coupled full Launch contract carries. A resource-only operation
// must not persist them.
var workspaceLaunchApplicationFactFields = []string{"workspaceImageDigest", "workspaceKeyGroupId"}

func workspaceLaunchProvisioningModeFromRaw(raw map[string]json.RawMessage) (contracts.WorkspaceProvisioningMode, error) {
	value, exists := raw[workspaceLaunchProvisioningModeFact]
	if !exists || len(value) == 0 {
		return contracts.WorkspaceProvisioningFull, nil
	}
	var mode contracts.WorkspaceProvisioningMode
	if err := json.Unmarshal(value, &mode); err != nil {
		return "", errInvalidWorkspaceLaunchOperation
	}
	if err := contracts.ValidateWorkspaceProvisioningMode(mode); err != nil {
		return "", errInvalidWorkspaceLaunchOperation
	}
	return mode, nil
}

func (operation workspaceLaunchReconcileOperation) provisioningMode() contracts.WorkspaceProvisioningMode {
	mode, err := workspaceLaunchProvisioningModeFromRaw(operation.raw)
	if err != nil {
		return ""
	}
	return mode
}

func (operation workspaceLaunchReconcileOperation) stagePlan() ([]contracts.Stage, error) {
	return contracts.WorkspaceProvisioningStages(operation.provisioningMode())
}

// provisioningModeWire returns the mode as transmitted on the wire: full stays
// the implicit empty default so retained full-Launch wire contracts and request
// hashes are unchanged.
func (operation workspaceLaunchReconcileOperation) provisioningModeWire() string {
	if operation.provisioningMode() == contracts.WorkspaceProvisioningResourceOnly {
		return string(contracts.WorkspaceProvisioningResourceOnly)
	}
	return ""
}

func workspaceLaunchStageInPlan(plan []contracts.Stage, stage contracts.Stage) bool {
	for _, candidate := range plan {
		if candidate == stage {
			return true
		}
	}
	return false
}

// workspaceLaunchModeRequiresApplicationFacts delegates the application
// coupling decision to the domain provisioning package.
func workspaceLaunchModeRequiresApplicationFacts(mode contracts.WorkspaceProvisioningMode) bool {
	requires, err := provisioning.PlanRequiresApplicationFacts(mode)
	if err != nil {
		return true
	}
	return requires
}
