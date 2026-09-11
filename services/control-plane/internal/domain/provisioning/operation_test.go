package provisioning

import (
	"errors"
	"reflect"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestNewOperationRejectsInvalidIdentityAndMode(t *testing.T) {
	testCases := []struct {
		name                       string
		id, accountID, workspaceID string
		mode                       contracts.WorkspaceProvisioningMode
		expected                   error
	}{
		{name: "valid resource-only identity", id: "op-1", accountID: "acc-1", workspaceID: "ws-1", mode: contracts.WorkspaceProvisioningResourceOnly},
		{name: "valid full identity", id: "op-2", accountID: "acc-1", workspaceID: "ws-1", mode: contracts.WorkspaceProvisioningFull},
		{name: "missing operation id", accountID: "acc-1", workspaceID: "ws-1", mode: contracts.WorkspaceProvisioningResourceOnly, expected: ErrInvalidOperation},
		{name: "missing account id", id: "op-1", workspaceID: "ws-1", mode: contracts.WorkspaceProvisioningResourceOnly, expected: ErrInvalidOperation},
		{name: "missing workspace id", id: "op-1", accountID: "acc-1", mode: contracts.WorkspaceProvisioningResourceOnly, expected: ErrInvalidOperation},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			operation, err := NewOperation(testCase.id, testCase.accountID, testCase.workspaceID, testCase.mode)
			if testCase.expected != nil {
				if !errors.Is(err, testCase.expected) {
					t.Fatalf("NewOperation() error = %v, want %v", err, testCase.expected)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewOperation() unexpected error: %v", err)
			}
			if operation.Stage != "" {
				t.Fatalf("NewOperation() initial stage = %q, want empty", operation.Stage)
			}
		})
	}
}

func TestStagePlanComesFromTheContractOwner(t *testing.T) {
	for _, mode := range []contracts.WorkspaceProvisioningMode{
		contracts.WorkspaceProvisioningResourceOnly, contracts.WorkspaceProvisioningFull,
	} {
		operation, err := NewOperation("op-1", "acc-1", "ws-1", mode)
		if err != nil {
			t.Fatalf("NewOperation() unexpected error: %v", err)
		}
		plan, err := operation.StagePlan()
		if err != nil {
			t.Fatalf("StagePlan() unexpected error: %v", err)
		}
		expected, err := contracts.WorkspaceProvisioningStages(mode)
		if err != nil {
			t.Fatalf("WorkspaceProvisioningStages() unexpected error: %v", err)
		}
		if !reflect.DeepEqual(plan, expected) {
			t.Fatalf("StagePlan() = %v, want %v", plan, expected)
		}
	}
}

func TestResourceOnlyPlanExcludesApplicationStages(t *testing.T) {
	plan, err := contracts.WorkspaceProvisioningStages(contracts.WorkspaceProvisioningResourceOnly)
	if err != nil {
		t.Fatalf("WorkspaceProvisioningStages() unexpected error: %v", err)
	}
	if len(plan) == 0 {
		t.Fatal("resource-only plan is empty")
	}
	for _, stage := range plan {
		if StageRequiresApplicationFacts(stage) {
			t.Fatalf("resource-only plan stage %q requires application facts", stage)
		}
	}
}

func TestNextStageWalksTheResourceOnlyPlan(t *testing.T) {
	plan, err := contracts.WorkspaceProvisioningStages(contracts.WorkspaceProvisioningResourceOnly)
	if err != nil {
		t.Fatalf("WorkspaceProvisioningStages() unexpected error: %v", err)
	}
	current := plan[0]
	for {
		next, terminal, err := NextStage(plan, current)
		if err != nil {
			t.Fatalf("NextStage(%q) unexpected error: %v", current, err)
		}
		if terminal {
			if next != contracts.StageSucceeded {
				t.Fatalf("terminal next stage = %q, want %q", next, contracts.StageSucceeded)
			}
			break
		}
		current = next
	}
	if _, _, err := NextStage(plan, contracts.StageKey); !errors.Is(err, ErrStageNotInPlan) {
		t.Fatalf("NextStage(StageKey) error = %v, want %v", err, ErrStageNotInPlan)
	}
}
