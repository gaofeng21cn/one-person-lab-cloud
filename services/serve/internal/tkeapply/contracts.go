package tkeapply

import (
	"errors"

	contracts "opl-cloud/packages/contracts/go"
)

// The application runtime input and lifecycle result are the shared contract
// types the workload executor consumes; they are aliases so this package adds no
// second wire shape.
type WorkspaceApplicationRuntimeInput = contracts.WorkspaceApplicationRuntimeInput
type WorkspaceApplicationRuntimeLifecycleResult = contracts.WorkspaceApplicationRuntimeLifecycleResult

// ErrInputInvalid refuses an application runtime input the executor cannot apply
// without guessing. It is never reported as a success.
var ErrInputInvalid = errors.New("workspace_application_runtime_input_invalid")

// ErrLaunchStageBindingConflict refuses a readback whose observed facts differ
// from the exact input the executor was asked to apply.
var ErrLaunchStageBindingConflict = errors.New("workspace_application_runtime_binding_conflict")

// ErrResourceAbsent reports that an object the executor reads is not present.
var ErrResourceAbsent = errors.New("workspace_application_resource_absent")

// ErrPlacementUnconfirmed refuses a workload whose confirmed placement does not
// name the node, the prepaid package and the prepaid storage claim it must be
// scheduled onto. Applying it anyway would be the executor's invention rather than
// the resources owner's fact.
var ErrPlacementUnconfirmed = errors.New("workspace_application_placement_unconfirmed")
