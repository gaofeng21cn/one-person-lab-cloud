package fabric

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// An operator explicitly accepts the unresolved original dispatch and authorizes
// one additional retain-mode detachment of the same immutable resource. This is
// never an automatic retry, and neither original fence nor outcome is rewritten.
const ComputePrepaidDetachReconciliationConfirmation = "RECONCILE_ORIGINAL_PREPAID_DETACH"
const computePrepaidDetachReconciliationAction = "reconcile_prepaid_compute_detachment"

type ComputePrepaidDetachReconciliationInput struct {
	AccountID              string `json:"accountId"`
	WorkspaceID            string `json:"workspaceId"`
	DestroyOperationID     string `json:"destroyOperationId"`
	DetachOperationID      string `json:"detachOperationId"`
	DetachRequestHash      string `json:"detachRequestHash"`
	AuthorizationReference string `json:"authorizationReference"`
	Confirmation           string `json:"confirmation"`
}

type ComputePrepaidDetachReconciliationResult struct {
	OperationID       string `json:"operationId"`
	DispatchAttempted bool   `json:"dispatchAttempted"`
	MachineAbsent     bool   `json:"machineAbsent"`
}

var prepaidDetachReconciliationToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
var prepaidDetachReconciliationHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validUnconfirmedPrepaidDetachError(value string) bool {
	for _, prefix := range []string{"compute_prepaid_detach_dispatch_unconfirmed", "compute_prepaid_retirement_unconfirmed"} {
		if value == prefix {
			return true
		}
		if strings.HasPrefix(value, prefix+":") {
			switch strings.TrimPrefix(value, prefix+":") {
			case "AuthFailure", "InvalidParameterValue", "UnsupportedOperation", "RequestLimitExceeded", "InternalError", "ResourceNotFound":
				return true
			}
		}
	}
	return false
}

func (s *Service) ReconcilePrepaidComputeDetachment(ctx context.Context, id string, input ComputePrepaidDetachReconciliationInput) (ComputePrepaidDetachReconciliationResult, error) {
	var out ComputePrepaidDetachReconciliationResult
	if input.Confirmation != ComputePrepaidDetachReconciliationConfirmation || !prepaidDetachReconciliationHash.MatchString(input.DetachRequestHash) {
		return out, fmt.Errorf("compute_prepaid_detach_reconciliation_authorization_required")
	}
	for _, value := range []string{id, input.AccountID, input.WorkspaceID, input.DestroyOperationID, input.DetachOperationID, input.AuthorizationReference} {
		if !prepaidDetachReconciliationToken.MatchString(value) {
			return out, fmt.Errorf("compute_prepaid_detach_reconciliation_authorization_required")
		}
	}
	if err := s.hydrateMissingResourceState(ctx); err != nil {
		return out, err
	}
	err := s.resourceLocks.WithPoolLock(ctx, "compute-destroy:"+id, func(ctx context.Context) error {
		destroy, found, err := s.latestComputeDestroyOperation(ctx, id)
		if err != nil {
			return err
		}
		if !found || destroy.OperationID != input.DestroyOperationID || destroy.AccountID != input.AccountID || destroy.WorkspaceID != input.WorkspaceID || destroy.ResourceKind != "compute_allocation" || (destroy.Status != "started" && destroy.Status != "failed" && destroy.Status != "succeeded") {
			return fmt.Errorf("compute_prepaid_detach_reconciliation_original_destroy_mismatch")
		}
		var allocation ComputeAllocation
		if !decodeOperationResource(destroy, &allocation) || allocation.ID != id || allocation.AccountID != input.AccountID || allocation.WorkspaceID != input.WorkspaceID {
			return fmt.Errorf("compute_prepaid_detach_reconciliation_identity_mismatch")
		}
		if len(allocation.CostTags) == 0 {
			current, found := s.GetComputeAllocation(ctx, id)
			withTags := cloneComputeAllocation(allocation)
			withTags.CostTags = current.CostTags
			if found && len(current.CostTags) > 0 && sameWorkspaceLaunchComputeTagIdentity(withTags, current) {
				allocation = withTags
			} else {
				allocation, err = s.computeDestroyStatusOwnershipInput(ctx, allocation)
				if err != nil {
					return err
				}
			}
		}
		if !validTencentComputeDestroyStableIdentity(allocation) || allocation.ChargeType != "PREPAID" || allocation.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" || allocation.ProviderData["machineType"] != "NativeCVM" {
			return fmt.Errorf("compute_prepaid_detach_reconciliation_identity_mismatch")
		}
		provider, ok := s.computeProvider.(prepaidComputeRetirer)
		if !ok || s.optionalProviders.computeDestroyStatus == nil {
			return fmt.Errorf("compute_prepaid_detach_reconciliation_unavailable")
		}
		poolKey := firstNonEmpty(allocation.PoolID, allocation.NodePoolID)
		if allocation.InstanceType != "" {
			poolKey += ":" + allocation.InstanceType
		}
		return s.resourceLocks.WithPoolLock(ctx, poolKey, func(ctx context.Context) error {
			history, err := s.operationHistory.List(ctx)
			if err != nil {
				return err
			}
			// A completed uncertain call is required. An original sender still running,
			// or an unbound/foreign record, cannot be superseded by an operator command.
			var previous FabricOperation
			claimedOriginal := false
			for _, row := range history {
				if row.ResourceID != id || row.Action != computePrepaidDetachAction {
					continue
				}
				if row.OperationID != input.DetachOperationID || row.RequestHash != input.DetachRequestHash || row.AccountID != input.AccountID || row.WorkspaceID != input.WorkspaceID || row.ResourceKind != "compute_prepaid_retirement" || row.CallerService != "workspace" || row.Provider != allocation.Provider || len(row.RedactedProviderPayload) != 1 || row.RedactedProviderPayload["originalDestroyOperationId"] != destroy.OperationID {
					return fmt.Errorf("compute_prepaid_detach_reconciliation_original_fence_mismatch")
				}
				if row.ID == stableID("fop-compute-prepaid-dispatch", id, computePrepaidDetachAction) && row.Status == "started" {
					claimedOriginal = true
				}
				previous = row
			}
			if !claimedOriginal || previous.Status != "failed" || previous.FinishedAt.IsZero() || !validUnconfirmedPrepaidDetachError(previous.ErrorCode) {
				return fmt.Errorf("compute_prepaid_detach_reconciliation_uncertain_outcome_required")
			}
			operation := newOperation(computePrepaidDetachReconciliationAction, "compute_prepaid_reconciliation", id, input.AccountID, input.WorkspaceID, destroy.OperationID+":"+computePrepaidDetachReconciliationAction, hashInput(input), s.now())
			operation.ID = stableID("fop-compute-prepaid-detach-reconciliation", id)
			operation.CallerService, operation.Provider = "operator", allocation.Provider
			operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": destroy.OperationID, "originalDetachOperationId": input.DetachOperationID, "originalDetachRequestHash": input.DetachRequestHash, "authorizationReference": input.AuthorizationReference}
			read := func() (ComputeAllocation, error) {
				observed, err := s.optionalProviders.computeDestroyStatus.ReadComputeDestroyStatus(ctx, cloneComputeAllocation(allocation))
				if err != nil {
					return ComputeAllocation{}, err
				}
				if !sameComputeDestroyReadbackStableIdentity(allocation, observed) {
					return ComputeAllocation{}, fmt.Errorf("compute_prepaid_detach_reconciliation_readback_mismatch")
				}
				return observed, nil
			}
			absent := func(observed ComputeAllocation) bool {
				return observed.MachinePresent != nil && !*observed.MachinePresent && observed.TKEStatus == "NOT_FOUND"
			}
			observed, err := read()
			if err != nil {
				return err
			}
			// Read and validate an existing recovery claim even after provider progress,
			// so changing the authorization cannot silently retarget an accepted command.
			attempted := false
			for _, row := range history {
				if row.ResourceID != id || row.Action != computePrepaidDetachReconciliationAction {
					continue
				}
				if !validPrepaidDetachReconciliationFence(row, operation) {
					return fmt.Errorf("compute_prepaid_detach_reconciliation_conflict")
				}
				attempted = true
				out.OperationID = row.OperationID
			}
			out.DispatchAttempted, out.MachineAbsent = attempted, absent(observed)
			if attempted || out.MachineAbsent {
				return nil
			}
			if !ComputePrepaidRetirementEligible(observed) || observed.MachinePresent == nil || !*observed.MachinePresent {
				return fmt.Errorf("compute_prepaid_detach_reconciliation_not_eligible")
			}
			operation.Status, operation.CreatedAt = "started", s.now()
			persisted, claimed, err := s.runtimeOperations.ClaimRuntime(ctx, operation)
			if err != nil {
				return err
			}
			if !validPrepaidDetachReconciliationFence(persisted, operation) || claimed && (persisted.ID != operation.ID || persisted.Status != "started") {
				return fmt.Errorf("compute_prepaid_detach_reconciliation_claim_invalid")
			}
			out.OperationID, out.DispatchAttempted = persisted.OperationID, true
			if !claimed {
				return nil
			}
			requestID, mutationErr := provider.DetachPrepaidComputeAllocation(ctx, cloneComputeAllocation(allocation))
			operation = persisted
			operation.ProviderRequestID = requestID
			observed, readErr := read()
			out.MachineAbsent = readErr == nil && absent(observed)
			outcome := "failed"
			if out.MachineAbsent {
				outcome, mutationErr = "succeeded", nil
			} else if mutationErr == nil {
				mutationErr = fmt.Errorf("compute_prepaid_detach_reconciliation_unconfirmed")
			}
			writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			writeErr := s.recordOperation(writeCtx, operation, outcome, nil, mutationErr)
			cancel()
			if writeErr != nil {
				return writeErr
			}
			if readErr != nil {
				return readErr
			}
			return mutationErr
		})
	})
	return out, err
}

func validPrepaidDetachReconciliationFence(previous, expected FabricOperation) bool {
	return previous.Action == expected.Action && previous.ResourceKind == expected.ResourceKind && previous.ResourceID == expected.ResourceID && previous.AccountID == expected.AccountID && previous.WorkspaceID == expected.WorkspaceID && previous.OperationID == expected.OperationID && previous.IdempotencyKey == expected.IdempotencyKey && previous.RequestHash == expected.RequestHash && previous.CallerService == expected.CallerService && previous.Provider == expected.Provider && hashInput(previous.RedactedProviderPayload) == hashInput(expected.RedactedProviderPayload) && (previous.Status == "started" || previous.Status == "failed" || previous.Status == "succeeded")
}
