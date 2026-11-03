package fabric

import (
	"context"
	"fmt"
	"time"
)

const ComputeFinalReleaseConfirmation = "RELEASE_ORIGINAL_RECYCLED_CVM"
const computeFinalReleaseAction = "release_recycled_compute_allocation"

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

// ReleaseRecycledComputeAllocation persists a dispatch fence before the single
// final-release call. Every subsequent request, including after process loss,
// only reads exact absence. The original Delete remains responsible for local
// resource finalization, its deletion receipt and settlement.
func (s *Service) ReleaseRecycledComputeAllocation(ctx context.Context, id string, input ComputeFinalReleaseInput) (ComputeFinalReleaseResult, error) {
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
			attempted := false
			for _, previous := range operations {
				if previous.Action != computeFinalReleaseAction || previous.ResourceID != id {
					continue
				}
				if previous.RequestHash != operation.RequestHash {
					return fmt.Errorf("compute_final_release_original_destroy_mismatch")
				}
				attempted = true
				operation = previous
			}
			out = ComputeFinalReleaseResult{OperationID: operation.OperationID, Status: "pending", DispatchAttempted: attempted}
			read := func() (bool, error) {
				observed, err := reader.ReadComputeDestroyStatus(ctx, cloneComputeAllocation(allocation))
				if err != nil {
					return false, err
				}
				if !sameComputeDestroyStableIdentity(allocation, observed) {
					return false, fmt.Errorf("compute_final_release_identity_mismatch")
				}
				return validTencentComputeAbsenceEvidence(observed) && observed.MachinePresent != nil && !*observed.MachinePresent && observed.TKEStatus == "NOT_FOUND" && observed.CVMStatus == "NOT_FOUND", nil
			}
			absent, err := read()
			if err != nil {
				return err
			}
			if !absent && !attempted {
				operation.CallerService = "operator"
				operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": input.DestroyOperationID}
				// No retry can pass this durable fence even if the process dies before
				// receiving the provider response or recording its outcome.
				if err = s.recordOperation(ctx, operation, "started", nil, nil); err != nil {
					return err
				}
				out.DispatchAttempted = true
				requestID, mutationErr := provider.ReleaseRecycledComputeAllocation(ctx, cloneComputeAllocation(allocation))
				operation.ProviderRequestID = requestID
				operation.Provider = allocation.Provider
				absent, err = read()
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
