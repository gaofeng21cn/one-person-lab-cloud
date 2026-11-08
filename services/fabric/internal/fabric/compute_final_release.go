package fabric

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const ComputeFinalReleaseConfirmation = "RELEASE_ORIGINAL_RECYCLED_CVM"
const computeFinalReleaseAction = "release_recycled_compute_allocation"

var ErrComputeFinalReleaseNotEligible = errors.New("compute_final_release_not_eligible")

// ComputeFinalReleaseInput authorizes one irreversible provider step of an
// already accepted destroy. It never changes the original destroy journal.
type ComputeFinalReleaseInput struct {
	AccountID          string `json:"accountId"`
	WorkspaceID        string `json:"workspaceId"`
	DestroyOperationID string `json:"destroyOperationId"`
	Confirmation       string `json:"confirmation"`
}

type ComputeFinalReleaseResult struct {
	OperationID       string `json:"operationId"`
	Status            string `json:"status"`
	DispatchAttempted bool   `json:"dispatchAttempted"`
	AbsenceConfirmed  bool   `json:"absenceConfirmed"`
}

type recycledComputeReleaser interface {
	ReleaseRecycledComputeAllocation(context.Context, ComputeAllocation) (string, error)
}

// ReleaseRecycledComputeAllocationForDeletion derives the exact original
// destroy operation from Fabric's journal. Workspace deletion supplies only
// the resource owner identity; it cannot manufacture a destroy operation ID or
// bypass the resource-scoped final-release fence.
func (s *Service) ReleaseRecycledComputeAllocationForDeletion(ctx context.Context, id, accountID, workspaceID string) (ComputeFinalReleaseResult, error) {
	if err := s.hydrateMissingResourceState(ctx); err != nil {
		return ComputeFinalReleaseResult{}, err
	}
	destroy, found, err := s.latestComputeDestroyOperation(ctx, id)
	if err != nil {
		return ComputeFinalReleaseResult{}, err
	}
	if !found || destroy.AccountID != accountID || destroy.WorkspaceID != workspaceID {
		return ComputeFinalReleaseResult{}, fmt.Errorf("compute_final_release_original_destroy_mismatch")
	}
	return s.releaseRecycledComputeAllocation(ctx, id, ComputeFinalReleaseInput{
		AccountID: accountID, WorkspaceID: workspaceID,
		DestroyOperationID: destroy.OperationID,
		Confirmation:       ComputeFinalReleaseConfirmation,
	}, "workspace")
}

// ComputeFinalReleaseEligible is deliberately narrower than a generic
// destroy-status readback. It identifies only the recycled NativeCVM state
// whose TKE Machine is gone while the original prepaid CVM remains in the
// provider's recyclable SHUTDOWN/MANMADE state.
func ComputeFinalReleaseEligible(allocation ComputeAllocation) bool {
	return allocation.Provider == "tencent-tke" && allocation.ProviderData["machineType"] == "NativeCVM" &&
		allocation.ProviderData["cvmApplicable"] == "true" && allocation.MachinePresent != nil && !*allocation.MachinePresent &&
		allocation.TKEStatus == "NOT_FOUND" && allocation.CVMStatus == "SHUTDOWN" &&
		allocation.ProviderData["isolatedSource"] == "MANMADE" && allocation.ProviderData["machinePresent"] == "false" &&
		allocation.ProviderData["tkeStatus"] == "NOT_FOUND" && allocation.ProviderData["cvmStatus"] == "SHUTDOWN" &&
		allocation.ProviderRequestID != "" && allocation.ProviderData["describeClusterMachinesReq"] != "" && allocation.ProviderData["describeCvmRequestId"] != ""
}

func computeFinalReleaseAbsent(observed ComputeAllocation) bool {
	return validTencentComputeAbsenceEvidence(observed) && observed.MachinePresent != nil && !*observed.MachinePresent &&
		observed.TKEStatus == "NOT_FOUND" && observed.CVMStatus == "NOT_FOUND"
}

func validComputeFinalReleaseFence(previous, expected FabricOperation, destroyOperationID string) bool {
	bound, ok := previous.RedactedProviderPayload["originalDestroyOperationId"].(string)
	return ok && bound == destroyOperationID && previous.Action == expected.Action && previous.ResourceKind == expected.ResourceKind &&
		previous.ResourceID == expected.ResourceID && previous.AccountID == expected.AccountID && previous.WorkspaceID == expected.WorkspaceID &&
		previous.RequestHash == expected.RequestHash && previous.OperationID == expected.OperationID &&
		(previous.Status == "started" || previous.Status == "failed" || previous.Status == "succeeded")
}

// ReleaseRecycledComputeAllocation persists a dispatch fence before the single
// final-release call. Every subsequent request, including after process loss,
// only reads exact absence. The original Delete remains responsible for local
// resource finalization, its deletion receipt and settlement.
func (s *Service) ReleaseRecycledComputeAllocation(ctx context.Context, id string, input ComputeFinalReleaseInput) (ComputeFinalReleaseResult, error) {
	return s.releaseRecycledComputeAllocation(ctx, id, input, "operator")
}

func (s *Service) releaseRecycledComputeAllocation(ctx context.Context, id string, input ComputeFinalReleaseInput, callerService string) (ComputeFinalReleaseResult, error) {
	var out ComputeFinalReleaseResult
	if input.Confirmation != ComputeFinalReleaseConfirmation || input.AccountID == "" || input.WorkspaceID == "" || input.DestroyOperationID == "" {
		return out, fmt.Errorf("compute_final_release_authorization_required")
	}
	if err := s.hydrateMissingResourceState(ctx); err != nil {
		return out, err
	}
	err := s.resourceLocks.WithPoolLock(ctx, "compute-destroy:"+id, func(ctx context.Context) error {
		destroy, found, err := s.latestComputeDestroyOperation(ctx, id)
		if err != nil {
			return err
		}
		if !found || destroy.OperationID != input.DestroyOperationID || destroy.AccountID != input.AccountID || destroy.WorkspaceID != input.WorkspaceID {
			return fmt.Errorf("compute_final_release_original_destroy_mismatch")
		}
		var allocation ComputeAllocation
		if !decodeOperationResource(destroy, &allocation) || allocation.ID != id || allocation.AccountID != input.AccountID || allocation.WorkspaceID != input.WorkspaceID {
			return fmt.Errorf("compute_final_release_identity_mismatch")
		}
		if allocation.Provider == "tencent-tke" && len(allocation.CostTags) == 0 {
			// Hydration may have recovered original Launch tags for this exact
			// resource. Copy only those proven tags into the frozen input.
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
		if !validTencentComputeDestroyStableIdentity(allocation) || allocation.ProviderData["machineType"] != "NativeCVM" {
			return fmt.Errorf("compute_final_release_identity_mismatch")
		}
		provider, ok := s.computeProvider.(recycledComputeReleaser)
		if !ok {
			return fmt.Errorf("compute_final_release_unavailable")
		}
		reader := s.optionalProviders.computeDestroyStatus
		if reader == nil {
			return fmt.Errorf("compute_destroy_status_readback_unavailable")
		}
		// Use the existing pool lock shared by destroy/renew execution.
		poolKey := firstNonEmpty(allocation.PoolID, allocation.NodePoolID)
		if allocation.InstanceType != "" {
			poolKey += ":" + allocation.InstanceType
		}
		return s.resourceLocks.WithPoolLock(ctx, poolKey, func(ctx context.Context) error {
			operations, err := s.operationHistory.List(ctx)
			if err != nil {
				return err
			}
			operation := newOperation(computeFinalReleaseAction, "compute_final_release", id, input.AccountID, input.WorkspaceID, id, hashInput(input), s.now())
			operation.CallerService = callerService
			operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": input.DestroyOperationID}
			attempted := false
			for _, previous := range operations {
				if previous.Action != computeFinalReleaseAction || previous.ResourceID != id {
					continue
				}
				if !validComputeFinalReleaseFence(previous, operation, input.DestroyOperationID) {
					return fmt.Errorf("compute_final_release_original_destroy_mismatch")
				}
				attempted = true
				operation = previous
			}
			out = ComputeFinalReleaseResult{OperationID: operation.OperationID, Status: "pending", DispatchAttempted: attempted}
			read := func() (ComputeAllocation, error) {
				observed, err := reader.ReadComputeDestroyStatus(ctx, cloneComputeAllocation(allocation))
				if err != nil {
					return ComputeAllocation{}, err
				}
				if !sameComputeDestroyStableIdentity(allocation, observed) {
					return ComputeAllocation{}, fmt.Errorf("compute_final_release_identity_mismatch")
				}
				return observed, nil
			}
			observed, err := read()
			if err != nil {
				return err
			}
			absent := computeFinalReleaseAbsent(observed)
			if !absent && !attempted && !ComputeFinalReleaseEligible(observed) {
				return ErrComputeFinalReleaseNotEligible
			}
			if !absent && !attempted {
				// The primary key is the final fence if an advisory-lock connection
				// is lost. Only the atomic claim winner may dispatch.
				operation.ID = stableID("fop-compute-final-release-dispatch", id)
				operation.Status, operation.CreatedAt = "started", s.now()
				persisted, claimed, claimErr := s.runtimeOperations.ClaimRuntime(ctx, operation)
				if claimErr != nil {
					return claimErr
				}
				if !validComputeFinalReleaseFence(persisted, operation, input.DestroyOperationID) || claimed && persisted.Status != "started" {
					return fmt.Errorf("compute_final_release_dispatch_claim_invalid")
				}
				operation = persisted
				out.DispatchAttempted = true
				if !claimed {
					observed, err = read()
					if err != nil {
						return err
					}
					if computeFinalReleaseAbsent(observed) {
						out.Status, out.AbsenceConfirmed = "succeeded", true
					}
					return nil
				}
				requestID, mutationErr := provider.ReleaseRecycledComputeAllocation(ctx, cloneComputeAllocation(allocation))
				operation.ProviderRequestID = requestID
				operation.Provider = allocation.Provider
				observed, err = read()
				absent = err == nil && computeFinalReleaseAbsent(observed)
				if err != nil || !absent {
					if mutationErr == nil {
						mutationErr = fmt.Errorf("compute_final_release_unconfirmed")
					}
					writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancel()
					if writeErr := s.recordOperation(writeCtx, operation, "failed", nil, mutationErr); writeErr != nil {
						return writeErr
					}
					return mutationErr
				}
			}
			if absent {
				out.Status, out.AbsenceConfirmed = "succeeded", true
				if !out.DispatchAttempted {
					return nil
				}
				operation.ErrorCode = ""
				return s.recordOperation(ctx, operation, "succeeded", nil, nil)
			}
			return nil
		})
	})
	return out, err
}

func (p *TencentProvider) ReleaseRecycledComputeAllocation(ctx context.Context, allocation ComputeAllocation) (string, error) {
	if !validTencentComputeDestroyStableIdentity(allocation) || allocation.ProviderData["machineType"] != "NativeCVM" {
		return "", fmt.Errorf("compute_final_release_identity_mismatch")
	}
	response, err := p.provision(ctx, provisionerRequest{
		Action: computeFinalReleaseAction, AccountID: allocation.AccountID, PackageID: allocation.PackageID,
		Region: allocation.ProviderData["region"], Zone: allocation.Zone, Tags: allocation.CostTags,
		Pool:       provisionerPool{ID: allocation.PoolID, ClusterID: allocation.ProviderData["clusterId"], NodePoolID: allocation.NodePoolID, InstanceType: allocation.InstanceType},
		Allocation: provisionerAllocation{ID: allocation.ID, InstanceID: allocation.InstanceID, MachineName: allocation.MachineName, MachineType: "NativeCVM", NodeName: allocation.NodeName, PrivateIP: allocation.PrivateIP},
	})
	if err != nil {
		return response.ProviderRequestID, err
	}
	if !response.OK {
		return response.ProviderRequestID, provisionerError(response)
	}
	if response.InstanceID != allocation.InstanceID || response.MutationCount < 0 || response.MutationCount > 1 || response.ProviderRequestID == "" {
		return "", fmt.Errorf("compute_final_release_response_invalid")
	}
	return response.ProviderRequestID, nil
}
