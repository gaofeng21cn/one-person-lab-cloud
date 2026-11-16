package server

import (
	contracts "opl-cloud/packages/contracts/go"
)

// stagePlan returns the provisioning stages the operation's mode declares.
// The retained launch execution that used it is retired, so the method stays
// as a test helper: production code must not keep a declaration that only its
// own tests reach.

func (operation workspaceLaunchReconcileOperation) stagePlan() ([]contracts.Stage, error) {
	return contracts.WorkspaceProvisioningStages(operation.provisioningMode())
}
