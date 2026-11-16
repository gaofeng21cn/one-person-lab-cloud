package server

import (
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// The resume authorization rotation helpers stay as test helpers: the
// retained launch execution that used them is retired, so production code
// must not keep a declaration only its own tests reach.
func (operation *workspaceLaunchReconcileOperation) rotateResumeAuthorization(authorization workspaceLaunchResumeAuthorization) {
	if operation.ResumeAuthorization != nil {
		operation.ConsumedResumeAuthorizations = append(operation.ConsumedResumeAuthorizations, workspaceLaunchConsumedResumeAuthorization{
			Authorization: *operation.ResumeAuthorization,
			ConsumedAt:    operation.ResumeAuthorizationConsumedAt,
		})
	}
	operation.ResumeAuthorization = &authorization
	operation.ResumeAuthorizationConsumedAt = ""
}

func (operation *workspaceLaunchReconcileOperation) consumeResumeAuthorization(now time.Time) {
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorizationConsumedAt == "" {
		operation.ResumeAuthorizationConsumedAt = now.UTC().Format(time.RFC3339Nano)
	}
}

func (operation *workspaceLaunchReconcileOperation) advance() {
	plan, err := operation.stagePlan()
	if err != nil {
		return
	}
	index := -1
	for i, stage := range plan {
		if stage == operation.Stage {
			index = i
			break
		}
	}
	if index < 0 || index == len(plan)-1 {
		operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
		return
	}
	operation.Stage = plan[index+1]
	if operation.Stage == contracts.StageSucceeded {
		operation.Status = contracts.StatusSucceeded
	} else {
		operation.Status = contracts.StatusPending
	}
}
