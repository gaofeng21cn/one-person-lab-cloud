// Package provisioning owns the resource-only Workspace provisioning rules:
// the stage plan consumed from contracts, the application coupling predicates,
// the mutation/replay and review decisions, and the activation write guards.
// The rules are pure; persistence, HTTP and downstream clients stay outside
// this package and reach it through the ports declared here.
package provisioning

import (
	"errors"

	contracts "opl-cloud/packages/contracts/go"
)

var (
	// ErrInvalidOperation reports an operation identity that cannot be used.
	ErrInvalidOperation = errors.New("provisioning_operation_invalid")
	// ErrStageNotInPlan reports a stage that the operation's plan does not contain.
	ErrStageNotInPlan = errors.New("provisioning_stage_not_in_plan")
)

// Operation is the identity and position of one provisioning operation. It
// carries no IO and no persistence; the owning store keeps the durable row.
type Operation struct {
	ID          string
	AccountID   string
	WorkspaceID string
	Mode        contracts.WorkspaceProvisioningMode
	Stage       contracts.Stage
	Status      contracts.LaunchStatus
}

// NewOperation validates the identity and the provisioning mode. The initial
// stage is the first stage of the mode's plan.
func NewOperation(id, accountID, workspaceID string, mode contracts.WorkspaceProvisioningMode) (Operation, error) {
	if id == "" || accountID == "" || workspaceID == "" {
		return Operation{}, ErrInvalidOperation
	}
	if err := contracts.ValidateWorkspaceProvisioningMode(mode); err != nil {
		return Operation{}, err
	}
	return Operation{ID: id, AccountID: accountID, WorkspaceID: workspaceID, Mode: mode}, nil
}

// StagePlan returns the operation's stage plan from the contracts package,
// which is the single stage-plan owner. Retained full Launches keep the
// historical AllLaunchStages order.
func (o Operation) StagePlan() ([]contracts.Stage, error) {
	return contracts.WorkspaceProvisioningStages(o.Mode)
}

// NextStage returns the stage that follows current in plan. It reports
// terminal when current is the last plan stage; a current stage outside the
// plan is an error.
func NextStage(plan []contracts.Stage, current contracts.Stage) (next contracts.Stage, terminal bool, err error) {
	for index, stage := range plan {
		if stage != current {
			continue
		}
		if index+1 < len(plan) {
			return plan[index+1], false, nil
		}
		return contracts.StageSucceeded, true, nil
	}
	return "", true, ErrStageNotInPlan
}
