package fabric

import (
	"context"
	"fmt"
	"time"
)

const (
	computePrepaidDetachAction = "detach_prepaid_compute_allocation"
	computePrepaidRetireAction = "retire_prepaid_compute_allocation"
)

type prepaidComputeRetirer interface {
	DetachPrepaidComputeAllocation(context.Context, ComputeAllocation) (string, error)
	RetirePrepaidComputeAllocation(context.Context, ComputeAllocation) (string, error)
}

// ComputePrepaidRetirementEligible names the two steps that precede the
// separately fenced recycled-CVM release. Actual provider mutation admission
// re-reads exact ownership and prepaid billing; these observations are no grant.
func ComputePrepaidRetirementEligible(a ComputeAllocation) bool {
	if a.Provider != "tencent-tke" || a.ProviderData["machineType"] != "NativeCVM" ||
		a.ProviderData["cvmApplicable"] != "true" || a.ChargeType != "PREPAID" || a.MachinePresent == nil {
		return false
	}
	if *a.MachinePresent {
		return a.TKEStatus == "RUNNING"
	}
	return a.TKEStatus == "NOT_FOUND" && (a.CVMStatus == "RUNNING" || a.CVMStatus == "STOPPED") &&
		a.ProviderData["isolatedSource"] == "NOTISOLATED"
}

// RetirePrepaidComputeAllocationForDeletion continues the accepted original
// destroy. It does not replay that destroy's unsupported terminate-mode send:
// detachment and prepaid retirement each have their own permanent atomic fence.
func (s *Service) RetirePrepaidComputeAllocationForDeletion(ctx context.Context, id, accountID, workspaceID string) (ComputeFinalReleaseResult, error) {
	out := ComputeFinalReleaseResult{Status: "pending"}
	if id == "" || accountID == "" || workspaceID == "" {
		return out, fmt.Errorf("compute_prepaid_retirement_owner_required")
	}
	if err := s.hydrateMissingResourceState(ctx); err != nil {
		return out, err
	}
	err := s.resourceLocks.WithPoolLock(ctx, "compute-destroy:"+id, func(ctx context.Context) error {
		destroy, found, err := s.latestComputeDestroyOperation(ctx, id)
		if err != nil {
			return err
		}
		if !found || destroy.AccountID != accountID || destroy.WorkspaceID != workspaceID || destroy.ResourceKind != "compute_allocation" || destroy.OperationID == "" ||
			(destroy.Status != "started" && destroy.Status != "failed" && destroy.Status != "succeeded") {
			return fmt.Errorf("compute_prepaid_retirement_original_destroy_mismatch")
		}
		var allocation ComputeAllocation
		if !decodeOperationResource(destroy, &allocation) || allocation.ID != id || allocation.AccountID != accountID || allocation.WorkspaceID != workspaceID {
			return fmt.Errorf("compute_prepaid_retirement_identity_mismatch")
		}
		if len(allocation.CostTags) == 0 {
			// Hydration can restore proven Launch tags while the frozen destroy
			// deliberately remains unchanged. Copy only tags whose full stable
			// identity matches; foreign cached ownership cannot authorize a send.
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
			return fmt.Errorf("compute_prepaid_retirement_identity_mismatch")
		}
		provider, ok := s.computeProvider.(prepaidComputeRetirer)
		if !ok || s.optionalProviders.computeDestroyStatus == nil {
			return fmt.Errorf("compute_prepaid_retirement_unavailable")
		}
		poolKey := firstNonEmpty(allocation.PoolID, allocation.NodePoolID)
		if allocation.InstanceType != "" {
			poolKey += ":" + allocation.InstanceType
		}
		return s.resourceLocks.WithPoolLock(ctx, poolKey, func(ctx context.Context) error {
			read := func() (ComputeAllocation, error) {
				observed, e := s.optionalProviders.computeDestroyStatus.ReadComputeDestroyStatus(ctx, cloneComputeAllocation(allocation))
				if e != nil {
					return ComputeAllocation{}, e
				}
				if !sameComputeDestroyReadbackStableIdentity(allocation, observed) {
					return ComputeAllocation{}, fmt.Errorf("compute_prepaid_retirement_readback_mismatch")
				}
				return observed, nil
			}
			observed, e := read()
			if e != nil {
				return e
			}
			for _, action := range []string{computePrepaidDetachAction, computePrepaidRetireAction} {
				if computeFinalReleaseAbsent(observed) {
					out.Status, out.AbsenceConfirmed = "succeeded", true
					return nil
				}
				eligible := ComputePrepaidRetirementEligible(observed)
				if !eligible {
					return nil
				}
				if action == computePrepaidDetachAction && !*observed.MachinePresent {
					continue
				}
				if action == computePrepaidRetireAction && *observed.MachinePresent {
					return nil
				}
				input := struct {
					Action, ResourceID, AccountID, WorkspaceID, DestroyOperationID, CreationOperationID string
					InstanceID, NodePoolID, MachineName, NodeName, Zone, InstanceType, PrivateIP        string
					Provider, ProviderResourceID, PackageID, PoolID, ChargeType, RenewFlag, Deadline    string
					Tags                                                                                map[string]string
				}{Action: action, ResourceID: id, AccountID: accountID, WorkspaceID: workspaceID,
					DestroyOperationID: destroy.OperationID, CreationOperationID: allocation.OperationID,
					InstanceID: allocation.InstanceID, NodePoolID: allocation.NodePoolID, MachineName: allocation.MachineName,
					NodeName: allocation.NodeName, Zone: allocation.Zone, InstanceType: allocation.InstanceType, PrivateIP: allocation.PrivateIP,
					Provider: allocation.Provider, ProviderResourceID: allocation.ProviderResourceID, PackageID: allocation.PackageID,
					PoolID: allocation.PoolID, ChargeType: allocation.ChargeType, RenewFlag: allocation.RenewFlag, Deadline: allocation.Deadline,
					Tags: allocation.CostTags}
				operation := newOperation(action, "compute_prepaid_retirement", id, accountID, workspaceID, destroy.OperationID+":"+action, hashInput(input), s.now())
				operation.ID = stableID("fop-compute-prepaid-dispatch", id, action)
				operation.CallerService = "workspace"
				operation.Provider = allocation.Provider
				operation.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": destroy.OperationID}
				operation.Status, operation.CreatedAt = "started", s.now()
				persisted, claimed, claimErr := s.runtimeOperations.ClaimRuntime(ctx, operation)
				if claimErr != nil {
					return claimErr
				}
				if !validComputeFinalReleaseFence(persisted, operation, destroy.OperationID) || claimed && persisted.ID != operation.ID ||
					persisted.Provider != allocation.Provider || persisted.CallerService != "workspace" || len(persisted.RedactedProviderPayload) != 1 ||
					claimed && persisted.Status != "started" {
					return fmt.Errorf("compute_prepaid_retirement_dispatch_claim_invalid")
				}
				out.OperationID, out.DispatchAttempted = persisted.OperationID, true
				if !claimed {
					return nil
				}
				// The claim is durable even if the pool lock connection is lost. This
				// action is never sent again, including an uncertain reply or process loss.
				operation = persisted
				var requestID string
				var mutationErr error
				if action == computePrepaidDetachAction {
					requestID, mutationErr = provider.DetachPrepaidComputeAllocation(ctx, cloneComputeAllocation(allocation))
				} else {
					requestID, mutationErr = provider.RetirePrepaidComputeAllocation(ctx, cloneComputeAllocation(allocation))
				}
				operation.ProviderRequestID = requestID
				observed, e = read()
				progressed := e == nil && observed.MachinePresent != nil && !*observed.MachinePresent && observed.TKEStatus == "NOT_FOUND"
				if action == computePrepaidRetireAction {
					progressed = progressed && (computeFinalReleaseAbsent(observed) || observed.CVMStatus == "SHUTDOWN" && observed.ProviderData["isolatedSource"] == "MANMADE")
				}
				status := "succeeded"
				if !progressed {
					status = "failed"
					if mutationErr == nil {
						mutationErr = fmt.Errorf("compute_prepaid_retirement_unconfirmed")
					}
				} else {
					mutationErr = nil
				}
				writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				writeErr := s.recordOperation(writeCtx, operation, status, nil, mutationErr)
				cancel()
				if writeErr != nil {
					return writeErr
				}
				if e != nil {
					return e
				}
				if !progressed {
					return mutationErr
				}
			}
			return nil
		})
	})
	return out, err
}

func (p *TencentProvider) DetachPrepaidComputeAllocation(ctx context.Context, a ComputeAllocation) (string, error) {
	return p.prepaidComputeRetirement(ctx, a, computePrepaidDetachAction)
}
func (p *TencentProvider) RetirePrepaidComputeAllocation(ctx context.Context, a ComputeAllocation) (string, error) {
	return p.prepaidComputeRetirement(ctx, a, computePrepaidRetireAction)
}
func (p *TencentProvider) prepaidComputeRetirement(ctx context.Context, a ComputeAllocation, action string) (string, error) {
	if !validTencentComputeDestroyStableIdentity(a) || a.ChargeType != "PREPAID" || a.RenewFlag != "NOTIFY_AND_MANUAL_RENEW" || a.ProviderData["machineType"] != "NativeCVM" {
		return "", fmt.Errorf("compute_prepaid_retirement_identity_mismatch")
	}
	response, err := p.provision(ctx, provisionerRequest{Action: action, AccountID: a.AccountID, PackageID: a.PackageID, Region: a.ProviderData["region"], Zone: a.Zone, Tags: a.CostTags,
		Pool:       provisionerPool{ID: a.PoolID, ClusterID: a.ProviderData["clusterId"], NodePoolID: a.NodePoolID, InstanceType: a.InstanceType},
		Allocation: provisionerAllocation{ID: a.ID, InstanceID: a.InstanceID, MachineName: a.MachineName, MachineType: "NativeCVM", NodeName: a.NodeName, PrivateIP: a.PrivateIP}})
	if err != nil {
		return response.ProviderRequestID, err
	}
	if !response.OK {
		return response.ProviderRequestID, provisionerError(response)
	}
	if response.InstanceID != a.InstanceID || response.MutationCount < 0 || response.MutationCount > 1 || response.ProviderRequestID == "" {
		return "", fmt.Errorf("compute_prepaid_retirement_response_invalid")
	}
	return response.ProviderRequestID, nil
}
