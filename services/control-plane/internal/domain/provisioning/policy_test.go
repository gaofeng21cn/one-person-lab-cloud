package provisioning

import (
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestStageRequiresApplicationFacts(t *testing.T) {
	testCases := []struct {
		stage    contracts.Stage
		expected bool
	}{
		{contracts.StageKey, true},
		{contracts.StageSecret, true},
		{contracts.StageRuntime, true},
		{contracts.StageDebit, false},
		{contracts.StageCompute, false},
		{contracts.StageStorage, false},
		{contracts.StageAttachment, false},
		{contracts.StageActivation, false},
		{contracts.StageReceipt, false},
	}
	for _, testCase := range testCases {
		if got := StageRequiresApplicationFacts(testCase.stage); got != testCase.expected {
			t.Fatalf("StageRequiresApplicationFacts(%q) = %v, want %v", testCase.stage, got, testCase.expected)
		}
	}
}

func TestPlanRequiresApplicationFacts(t *testing.T) {
	resourceOnly, err := PlanRequiresApplicationFacts(contracts.WorkspaceProvisioningResourceOnly)
	if err != nil {
		t.Fatalf("PlanRequiresApplicationFacts(resource_only) unexpected error: %v", err)
	}
	if resourceOnly {
		t.Fatal("resource-only plan must not require application facts")
	}
	full, err := PlanRequiresApplicationFacts(contracts.WorkspaceProvisioningFull)
	if err != nil {
		t.Fatalf("PlanRequiresApplicationFacts(full) unexpected error: %v", err)
	}
	if !full {
		t.Fatal("full plan must require application facts")
	}
	if _, err := PlanRequiresApplicationFacts("application_only"); err == nil {
		t.Fatal("PlanRequiresApplicationFacts(invalid) must reject the mode")
	}
}

func TestCanMutateStage(t *testing.T) {
	if CanMutateStage(contracts.StageKey, false) {
		t.Fatal("Key stage must not mutate without a valid delegated credential")
	}
	if !CanMutateStage(contracts.StageKey, true) {
		t.Fatal("Key stage must mutate with a valid delegated credential")
	}
	if !CanMutateStage(contracts.StageDebit, false) || !CanMutateStage(contracts.StageCompute, false) {
		t.Fatal("non-Key stages must mutate without a delegated credential")
	}
}

func TestIdempotentReplayAllowed(t *testing.T) {
	if IdempotentReplayAllowed(contracts.StageDebit) {
		t.Fatal("a debit write must never replay; recovery reads the original outcome")
	}
	for _, stage := range []contracts.Stage{contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageActivation, contracts.StageReceipt} {
		if !IdempotentReplayAllowed(stage) {
			t.Fatalf("stage %q must allow idempotent replay", stage)
		}
	}
}

func TestDecideObservationResourceOnly(t *testing.T) {
	testCases := []struct {
		name                    string
		stage                   contracts.Stage
		state                   contracts.StageState
		mutationBudgetRemaining bool
		expected                Decision
	}{
		{name: "debit ready confirms", stage: contracts.StageDebit, state: contracts.StageStateReady, expected: DecisionConfirmAdvance},
		{name: "debit unknown parks for financial review", stage: contracts.StageDebit, state: contracts.StageStateUnknown, expected: DecisionManualReview},
		{name: "debit absent dispatches once", stage: contracts.StageDebit, state: contracts.StageStateAbsent, mutationBudgetRemaining: true, expected: DecisionDispatchMutation},
		{name: "debit absent without budget reviews", stage: contracts.StageDebit, state: contracts.StageStateAbsent, expected: DecisionManualReview},
		{name: "compute pending waits", stage: contracts.StageCompute, state: contracts.StageStatePending, expected: DecisionWaitWithBudget},
		{name: "compute pool queued waits", stage: contracts.StageCompute, state: contracts.StageStateComputePoolQueued, expected: DecisionWaitWithBudget},
		{name: "compute ready confirms", stage: contracts.StageCompute, state: contracts.StageStateReady, expected: DecisionConfirmAdvance},
		{name: "compute absent dispatches", stage: contracts.StageCompute, state: contracts.StageStateAbsent, mutationBudgetRemaining: true, expected: DecisionDispatchMutation},
		{name: "compute unknown reviews", stage: contracts.StageCompute, state: contracts.StageStateUnknown, expected: DecisionManualReview},
		{name: "activation ready confirms", stage: contracts.StageActivation, state: contracts.StageStateReady, expected: DecisionConfirmAdvance},
		{name: "activation unknown reviews", stage: contracts.StageActivation, state: contracts.StageStateUnknown, expected: DecisionManualReview},
		{name: "receipt ready confirms", stage: contracts.StageReceipt, state: contracts.StageStateReady, expected: DecisionConfirmAdvance},
		{name: "receipt absent retries evidence only", stage: contracts.StageReceipt, state: contracts.StageStateAbsent, expected: DecisionRetryEvidenceWrite},
		{name: "receipt unknown retries evidence only", stage: contracts.StageReceipt, state: contracts.StageStateUnknown, expected: DecisionRetryEvidenceWrite},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			decision := DecideObservation(testCase.stage, testCase.state, testCase.mutationBudgetRemaining)
			if decision != testCase.expected {
				t.Fatalf("DecideObservation(%q, %q) = %q, want %q", testCase.stage, testCase.state, decision, testCase.expected)
			}
		})
	}
}

func TestValidateActivationWrites(t *testing.T) {
	cleanFacts := map[string]any{"activationOperationId": "op-1", "workspaceActivatedAt": "2026-09-11T00:00:00Z"}
	if err := ValidateActivationWrites(contracts.WorkspaceProvisioningResourceOnly, cleanFacts); err != nil {
		t.Fatalf("ValidateActivationWrites(resource_only, clean) unexpected error: %v", err)
	}
	for _, key := range activationRuntimeFactKeys {
		facts := map[string]any{key: "value"}
		if err := ValidateActivationWrites(contracts.WorkspaceProvisioningResourceOnly, facts); err == nil {
			t.Fatalf("ValidateActivationWrites(resource_only, %q) must reject runtime facts", key)
		}
	}
	runtimeFacts := map[string]any{"runtimeReady": true, "runtimeServiceName": "opl-app"}
	if err := ValidateActivationWrites(contracts.WorkspaceProvisioningFull, runtimeFacts); err != nil {
		t.Fatalf("ValidateActivationWrites(full, runtime) unexpected error: %v", err)
	}
}
