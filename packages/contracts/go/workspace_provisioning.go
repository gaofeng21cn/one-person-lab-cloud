package contracts

import "errors"

// WorkspaceProvisioningMode distinguishes the retained Launch contract from
// the new resource-only provisioning contract. Existing persisted Launches
// continue to use WorkspaceProvisioningFull.
type WorkspaceProvisioningMode string

const (
	WorkspaceProvisioningFull         WorkspaceProvisioningMode = "full"
	WorkspaceProvisioningResourceOnly WorkspaceProvisioningMode = "resource_only"
)

func ValidateWorkspaceProvisioningMode(mode WorkspaceProvisioningMode) error {
	if mode != WorkspaceProvisioningFull && mode != WorkspaceProvisioningResourceOnly {
		return errors.New("workspace_provisioning_mode_invalid")
	}
	return nil
}

// WorkspaceProvisioningStages is the single stage-plan owner. Do not change
// AllLaunchStages: retained full Launch rows depend on that historical order.
func WorkspaceProvisioningStages(mode WorkspaceProvisioningMode) ([]Stage, error) {
	if err := ValidateWorkspaceProvisioningMode(mode); err != nil {
		return nil, err
	}
	if mode == WorkspaceProvisioningResourceOnly {
		return []Stage{StageDebit, StageCompute, StageStorage, StageAttachment, StageActivation, StageReceipt, StageSucceeded}, nil
	}
	return AllLaunchStages(), nil
}
