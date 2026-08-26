package server

import (
	"errors"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func roundTripTypedWorkspaceLaunchForTest(t *testing.T, operation workspaceLaunchReconcileOperation) (workspaceLaunchReconcileOperation, error) {
	t.Helper()
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return decodeWorkspaceLaunchReconcileOperation(row)
}

func TestWorkspaceLaunchTypedSchemaV3RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		stage  contracts.Stage
		status contracts.LaunchStatus
		state  contracts.StageState
	}{
		{name: "absent key", stage: contracts.StageKey, status: contracts.StatusPending, state: contracts.StageStateAbsent},
		{name: "compute ownership pending", stage: contracts.StageCompute, status: contracts.StatusPending, state: contracts.StageStateOwnershipPending},
		{name: "runtime image revision pending", stage: contracts.StageRuntime, status: contracts.StatusPending, state: contracts.StageStateRuntimeImageRevisionPending},
		{name: "unknown runtime", stage: contracts.StageRuntime, status: contracts.StatusManualReview, state: contracts.StageStateUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
			if err != nil {
				t.Fatal(err)
			}
			operation.Stage, operation.Status = tc.stage, tc.status
			operation.Observations[tc.stage] = workspaceLaunchStageObservation{State: tc.state}

			decoded, err := roundTripTypedWorkspaceLaunchForTest(t, operation)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Stage != tc.stage || decoded.Status != tc.status || decoded.Observations[tc.stage].State != tc.state {
				t.Fatalf("typed round trip = %s/%s/%s, want %s/%s/%s",
					decoded.Stage, decoded.Status, decoded.Observations[tc.stage].State, tc.stage, tc.status, tc.state)
			}
		})
	}
}

func TestWorkspaceLaunchTypedDecoderRejectsUnknownEnums(t *testing.T) {
	tests := []struct {
		name     string
		stage    contracts.Stage
		status   contracts.LaunchStatus
		state    contracts.StageState
		category string
	}{
		{name: "stage", stage: contracts.Stage("invalid"), status: contracts.StatusPending, state: contracts.StageStateAbsent, category: "invalid_stage"},
		{name: "status", stage: contracts.StageKey, status: contracts.LaunchStatus("invalid"), state: contracts.StageStateAbsent, category: "status_stage_mismatch"},
		{name: "state", stage: contracts.StageKey, status: contracts.StatusPending, state: contracts.StageState("invalid"), category: "invalid_observations"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
			if err != nil {
				t.Fatal(err)
			}
			operation.Stage, operation.Status = tc.stage, tc.status
			operation.Observations[tc.stage] = workspaceLaunchStageObservation{State: tc.state}

			_, decodeErr := roundTripTypedWorkspaceLaunchForTest(t, operation)
			if !errors.Is(decodeErr, errInvalidWorkspaceLaunchOperation) || workspaceLaunchDecodeFailureCategory(decodeErr) != tc.category {
				t.Fatalf("decode error = %v, category = %q, want %q", decodeErr, workspaceLaunchDecodeFailureCategory(decodeErr), tc.category)
			}
		})
	}
}

func TestWorkspaceLaunchTypedDecoderRejectsCrossFieldDrift(t *testing.T) {
	tests := []struct {
		name     string
		stage    contracts.Stage
		status   contracts.LaunchStatus
		state    contracts.StageState
		category string
	}{
		{name: "pending terminal stage", stage: contracts.StageSucceeded, status: contracts.StatusPending, category: "status_stage_mismatch"},
		{name: "succeeded runtime", stage: contracts.StageRuntime, status: contracts.StatusSucceeded, category: "status_stage_mismatch"},
		{name: "failed key", stage: contracts.StageKey, status: contracts.StatusFailed, category: "status_stage_mismatch"},
		{name: "ownership pending key", stage: contracts.StageKey, status: contracts.StatusManualReview, state: contracts.StageStateOwnershipPending, category: "invalid_observations"},
		{name: "runtime revision pending compute", stage: contracts.StageCompute, status: contracts.StatusPending, state: contracts.StageStateRuntimeImageRevisionPending, category: "invalid_observations"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
			if err != nil {
				t.Fatal(err)
			}
			operation.Stage, operation.Status = tc.stage, tc.status
			if tc.state != "" {
				operation.Observations[tc.stage] = workspaceLaunchStageObservation{State: tc.state}
			}

			_, decodeErr := roundTripTypedWorkspaceLaunchForTest(t, operation)
			if !errors.Is(decodeErr, errInvalidWorkspaceLaunchOperation) || workspaceLaunchDecodeFailureCategory(decodeErr) != tc.category {
				t.Fatalf("decode error = %v, category = %q, want %q", decodeErr, workspaceLaunchDecodeFailureCategory(decodeErr), tc.category)
			}
		})
	}
}

func TestWorkspaceLaunchTypedStageAdvanceReachesTerminal(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage, operation.Status = contracts.StageReceipt, contracts.StatusPending

	operation.advance()

	if operation.Stage != contracts.StageSucceeded || operation.Status != contracts.StatusSucceeded {
		t.Fatalf("terminal advance = %s/%s", operation.Stage, operation.Status)
	}
}
