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

// WorkspacePurchaseEligibility is the retained Account owner's current answer
// for the live session's actor. It is read before a new Workspace order, never
// reconstructed from Tenant membership, wallet funds or an existing Workspace.
// The pointer preserves the distinction between an explicit false and an absent
// or unknown eligibility fact at the JSON boundary.
type WorkspacePurchaseEligibility struct {
	ActorID                  string `json:"actorId"`
	AccountID                string `json:"accountId"`
	WorkspacePurchaseEnabled *bool  `json:"workspacePurchaseEnabled"`
}

// Runtime power changes preserve the original paid Workspace's resource identity.
type WorkspaceRuntimePowerInput struct {
	SchemaVersion       int    `json:"schemaVersion"`
	AccountID           string `json:"accountId"`
	WorkspaceID         string `json:"workspaceId"`
	RuntimeID           string `json:"runtimeId"`
	RuntimeOperationID  string `json:"runtimeOperationId"`
	PaidThrough         string `json:"paidThrough"`
	DesiredState        string `json:"desiredState"`
	IdempotencyKey      string `json:"idempotencyKey"`
	SuspensionReason    string `json:"suspensionReason,omitempty"`
	MissingResourceType string `json:"missingResourceType,omitempty"`
	MissingResourceID   string `json:"missingResourceID,omitempty"`
}

const WorkspaceRuntimeSuspensionProviderResourceAbsent = "provider_resource_absent"

type WorkspaceRuntimePowerResult struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Binding       WorkspaceRuntimePowerInput `json:"binding"`
	State         string                     `json:"state"`
	Reason        string                     `json:"reason"`
}
