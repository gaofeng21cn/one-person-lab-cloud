package provisioning

import (
	"errors"

	contracts "opl-cloud/packages/contracts/go"
)

// Decision is the action a reconciler takes for one observation of the
// operation's current stage. It covers the resource-only provisioning flow;
// the retained full-Launch decisions stay owned by the existing server
// reconciler until their consumers migrate.
type Decision string

const (
	// DecisionConfirmAdvance records the stage as confirmed and advances the plan.
	DecisionConfirmAdvance Decision = "confirm_advance"
	// DecisionDispatchMutation issues the stage's idempotent mutation.
	DecisionDispatchMutation Decision = "dispatch_mutation"
	// DecisionWaitWithBudget keeps waiting inside the authoritative read budget.
	DecisionWaitWithBudget Decision = "wait_with_budget"
	// DecisionManualReview parks the operation for review. A debit parked here
	// keeps the original financial review semantics and never re-dispatches.
	DecisionManualReview Decision = "manual_review"
	// DecisionRetryEvidenceWrite retries only the evidence write. A receipt
	// failure never rewinds the operation or re-runs resource mutations.
	DecisionRetryEvidenceWrite Decision = "retry_evidence_write"
)

// StageRequiresApplicationFacts reports whether a stage cannot run without
// application facts (Gateway key, Gateway secret binding or OPL App Runtime).
// Resource-only provisioning consists entirely of stages that answer false.
func StageRequiresApplicationFacts(stage contracts.Stage) bool {
	switch stage {
	case contracts.StageKey, contracts.StageSecret, contracts.StageRuntime:
		return true
	default:
		return false
	}
}

// PlanRequiresApplicationFacts reports whether the mode's stage plan contains
// application-coupled stages. Control Plane launch stages and Fabric
// resource-stage validation must accept any plan that answers false without
// requiring an application image.
func PlanRequiresApplicationFacts(mode contracts.WorkspaceProvisioningMode) (bool, error) {
	if err := contracts.ValidateWorkspaceProvisioningMode(mode); err != nil {
		return false, err
	}
	return mode == contracts.WorkspaceProvisioningFull, nil
}

// CanMutateStage reports whether the stage's mutation may be dispatched. The
// retained Key stage additionally requires a valid delegated credential;
// resource-only plans contain no Key stage.
func CanMutateStage(stage contracts.Stage, keyCredentialValid bool) bool {
	if stage != contracts.StageKey {
		return true
	}
	return keyCredentialValid
}

// IdempotentReplayAllowed reports whether a reserved mutation may replay under
// its idempotency key after a lost response. The native wallet's debit
// idempotency is not atomic with the balance write, so a debit may only be
// resolved by reading the original outcome; the write itself is never replayed.
func IdempotentReplayAllowed(stage contracts.Stage) bool {
	return stage != contracts.StageDebit
}

// DecideObservation maps one authoritative stage observation to the next
// action for the resource-only flow. An unknown observation always parks for
// review; recovery from review happens only through explicit authorized
// mechanisms, never by re-dispatching an unproven mutation.
func DecideObservation(stage contracts.Stage, state contracts.StageState, mutationBudgetRemaining bool) Decision {
	if stage == contracts.StageReceipt {
		if state == contracts.StageStateReady {
			return DecisionConfirmAdvance
		}
		return DecisionRetryEvidenceWrite
	}
	switch state {
	case contracts.StageStateReady:
		return DecisionConfirmAdvance
	case contracts.StageStatePending, contracts.StageStateOwnershipPending,
		contracts.StageStateComputeDispatchPending, contracts.StageStateComputePoolQueued,
		contracts.StageStateRuntimeImageRevisionPending:
		return DecisionWaitWithBudget
	case contracts.StageStateAbsent:
		if mutationBudgetRemaining {
			return DecisionDispatchMutation
		}
		return DecisionManualReview
	default:
		return DecisionManualReview
	}
}

// activationRuntimeFactKeys are the workspace projection keys that describe a
// running application runtime. They belong to the Runtime stage, never to a
// resource-only activation.
var activationRuntimeFactKeys = []string{
	"runtimeId", "runtimeReady", "runtimeServiceName", "runtimeBindingRef",
}

// ValidateActivationWrites guards the workspace projection written at
// StageActivation. A resource-only activation establishes the resource
// entitlement with an empty current application binding; it must not fabricate
// runtime state such as RuntimeReady or a running application service.
func ValidateActivationWrites(mode contracts.WorkspaceProvisioningMode, facts map[string]any) error {
	if mode != contracts.WorkspaceProvisioningResourceOnly {
		return nil
	}
	for _, key := range activationRuntimeFactKeys {
		if _, ok := facts[key]; ok {
			return errors.New("provisioning_activation_runtime_fact_forbidden")
		}
	}
	return nil
}
