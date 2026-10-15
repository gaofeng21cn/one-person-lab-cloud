package fabric

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

func (s *Service) ProviderFactsBatch(ctx context.Context, input ProviderFactsBatchInput) (ProviderFactsBatch, error) {
	result := ProviderFactsBatch{Items: make([]ProviderFact, len(input.Items))}
	if len(input.Items) == 0 || len(input.Items) > 50 {
		return ProviderFactsBatch{}, fmt.Errorf("provider_facts_batch_invalid")
	}
	batchCtx, cancel := context.WithTimeout(ctx, providerFactsBatchTimeout)
	defer cancel()
	type job struct {
		index int
		item  ProviderFactInput
	}
	jobs := make(chan job, len(input.Items))
	for index, item := range input.Items {
		result.Items[index] = ProviderFact{AccountID: item.AccountID, WorkspaceID: item.WorkspaceID, ResourceType: item.ResourceType, ResourceID: item.ResourceID}
		jobs <- job{index: index, item: item}
	}
	close(jobs)
	workers := providerFactsBatchWorkerCount
	if len(input.Items) < workers {
		workers = len(input.Items)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				select {
				case <-batchCtx.Done():
					return
				case next, ok := <-jobs:
					if !ok {
						return
					}
					result.Items[next.index] = s.providerFact(batchCtx, next.item)
				}
			}
		}()
	}
	wait.Wait()
	if batchCtx.Err() != nil {
		for index := range result.Items {
			if !result.Items[index].Available && result.Items[index].ErrorCode == "" {
				result.Items[index].ErrorCode = "provider_facts_timeout"
				result.Items[index].Observation = &contracts.ResourceObservation{
					State: contracts.ResourceObservedUnknown, ReasonCode: "provider_facts_timeout", ObservedAt: s.now().Format(time.RFC3339Nano),
				}
			}
		}
	}
	return result, nil
}

func (s *Service) RuntimeHealthSummary(ctx context.Context) (RuntimeHealthSummary, error) {
	provider := s.optionalProviders.runtimeHealth
	if provider == nil {
		return RuntimeHealthSummary{}, ErrRuntimeHealthSummaryUnavailable
	}
	readCtx, cancel := context.WithTimeout(ctx, runtimeHealthSummaryTimeout)
	defer cancel()
	summary, err := provider.RuntimeHealthSummary(readCtx)
	if err != nil {
		return RuntimeHealthSummary{}, fmt.Errorf("%w: %v", ErrRuntimeHealthSummaryUnavailable, err)
	}
	if summary.Total < 0 || summary.Ready < 0 || summary.Unready < 0 || summary.Ready+summary.Unready != summary.Total {
		return RuntimeHealthSummary{}, fmt.Errorf("%w: invalid_counts", ErrRuntimeHealthSummaryUnavailable)
	}
	return summary, nil
}

func (s *Service) providerFact(ctx context.Context, input ProviderFactInput) (result ProviderFact) {
	result = ProviderFact{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceType: input.ResourceType, ResourceID: input.ResourceID}
	defer func() {
		if result.Observation == nil {
			result.Observation = &contracts.ResourceObservation{State: contracts.ResourceObservedUnknown, ReasonCode: result.ErrorCode}
		}
		result.Observation.ObservedAt = s.now().Format(time.RFC3339Nano)
	}()
	if input.AccountID == "" || input.WorkspaceID == "" || input.ResourceID == "" {
		result.ErrorCode = "provider_fact_identity_required"
		return result
	}
	s.mu.Lock()
	compute := s.computes[input.ResourceID]
	storage := s.volumes[input.ResourceID]
	attachment := s.attachments[input.ResourceID]
	attachmentCompute := s.computes[attachment.ComputeID]
	attachmentStorage := s.volumes[attachment.VolumeID]
	s.mu.Unlock()
	missing := input.ResourceType == "compute" && compute.ID == "" ||
		input.ResourceType == "storage" && storage.ID == "" ||
		input.ResourceType == "attachment" && (attachment.ID == "" || attachmentCompute.ID == "" || attachmentStorage.ID == "")
	if missing {
		if err := s.hydrateMissingResourceState(ctx); err != nil {
			result.ErrorCode = errorCode(err)
			return result
		}
		s.mu.Lock()
		compute = s.computes[input.ResourceID]
		storage = s.volumes[input.ResourceID]
		attachment = s.attachments[input.ResourceID]
		attachmentCompute = s.computes[attachment.ComputeID]
		attachmentStorage = s.volumes[attachment.VolumeID]
		s.mu.Unlock()
	}
	var facts ProviderResourceFacts
	var err error
	if input.ResourceType != "compute" && input.ResourceType != "storage" && input.ResourceType != "attachment" && input.ResourceType != "runtime" {
		result.ErrorCode = "provider_fact_resource_type_invalid"
		return result
	}
	provider := s.optionalProviders.providerFacts
	if provider == nil {
		result.ErrorCode = "provider_facts_unavailable"
		return result
	}
	switch input.ResourceType {
	case "compute":
		if compute.ID == "" || compute.AccountID != input.AccountID || compute.WorkspaceID != input.WorkspaceID {
			result.ErrorCode = "provider_fact_identity_mismatch"
			return result
		}
		compute = s.computeProviderFactInput(ctx, compute)
		facts, err = provider.ReadComputeProviderFacts(ctx, compute)
	case "storage":
		if storage.ID == "" || storage.AccountID != input.AccountID || storage.WorkspaceID != input.WorkspaceID {
			result.ErrorCode = "provider_fact_identity_mismatch"
			return result
		}
		facts, err = provider.ReadStorageProviderFacts(ctx, storage)
	case "attachment":
		if attachment.ID == "" || attachment.WorkspaceID != input.WorkspaceID || attachmentCompute.AccountID != input.AccountID || attachmentCompute.WorkspaceID != input.WorkspaceID || attachmentStorage.AccountID != input.AccountID || attachmentStorage.WorkspaceID != input.WorkspaceID {
			result.ErrorCode = "provider_fact_identity_mismatch"
			return result
		}
		facts, err = provider.ReadStorageAttachmentProviderFacts(ctx, attachment, attachmentCompute, attachmentStorage)
	case "runtime":
		var runtime WorkspaceRuntime
		var owner FabricOperation
		runtime, owner, err = s.runtimeRead.readStatusWithOwner(ctx, input.WorkspaceID)
		runtime.Access.Password = ""
		if err == nil && (runtime.ID != input.ResourceID || runtime.WorkspaceID != input.WorkspaceID) {
			result.ErrorCode = "provider_fact_identity_mismatch"
			return result
		}
		facts = provider.WorkspaceRuntimeProviderFacts(runtime)
		if err != nil || owner.AccountID != input.AccountID {
			facts.Observation = &contracts.ResourceObservation{State: contracts.ResourceObservedUnknown, ReasonCode: "provider_fact_identity_mismatch"}
			if err != nil {
				facts.Observation.ReasonCode = errorCode(err)
			}
		}
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			owners, ownerErr := s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(ctx, input.WorkspaceID)
			var created WorkspaceRuntime
			if ownerErr == nil && len(owners) == 1 && owners[0].AccountID == input.AccountID && decodeOperationResource(owners[0], &created) &&
				created.WorkspaceID == input.WorkspaceID && created.ID == input.ResourceID {
				facts.Observation = &contracts.ResourceObservation{Available: true, State: contracts.ResourceObservedAbsent, ProviderID: created.ServiceName}
			}
		}
		if err == nil {
			facts.ComputeRuntimeBinding = s.workspaceComputeRuntimeBinding(ctx, provider, input, runtime)
		}
	}
	result.Observation = facts.Observation
	if err != nil {
		result.ErrorCode = errorCode(err)
		return result
	}
	if result.Observation == nil {
		result.Observation = observationFromFacts(facts)
	}
	if result.Observation.Available {
		result.Observation.ComputeRuntimeBinding = facts.ComputeRuntimeBinding
	}
	facts.Observation = nil
	facts.LastReadAt = s.now().Format(time.RFC3339Nano)
	result.Available, result.Facts = true, facts
	return result
}

func (s *Service) workspaceComputeRuntimeBinding(ctx context.Context, provider providerFactsReader, input ProviderFactInput, runtime WorkspaceRuntime) *contracts.WorkspaceComputeRuntimeBinding {
	if runtime.ID != input.ResourceID || runtime.WorkspaceID != input.WorkspaceID || runtime.ComputeID == "" || runtime.NodeName == "" {
		return nil
	}
	s.mu.Lock()
	compute := s.computes[runtime.ComputeID]
	s.mu.Unlock()
	instanceID := firstNonEmpty(compute.InstanceID, compute.CVMInstanceID)
	if compute.ID != runtime.ComputeID || compute.AccountID != input.AccountID || compute.WorkspaceID != input.WorkspaceID || compute.Provider != "tencent-tke" ||
		compute.PackageID == "" || compute.NodePoolID == "" || compute.MachineName == "" || instanceID == "" || compute.NodeName == "" {
		return nil
	}
	ownership, err := s.machineOwnership.MachineOwnership(ctx, compute.ID)
	if err != nil || ownership.Status != "active" || ownership.ID == "" || ownership.ClaimedAt.IsZero() ||
		ownership.ResourceID != compute.ID || ownership.AccountID != compute.AccountID || ownership.WorkspaceID != compute.WorkspaceID ||
		ownership.PackageID != compute.PackageID || ownership.NodePoolID != compute.NodePoolID || ownership.MachineID != compute.MachineName ||
		ownership.InstanceID != instanceID || ownership.NodeName != compute.NodeName {
		return nil
	}
	reader, ok := provider.(workspaceComputeRuntimeBindingReader)
	if !ok {
		return nil
	}
	matched, err := reader.ReadWorkspaceComputeRuntimeBinding(ctx, runtime, compute, ownership)
	if err != nil {
		return nil
	}
	status := contracts.WorkspaceComputeRuntimeBindingMismatched
	if matched {
		status = contracts.WorkspaceComputeRuntimeBindingMatched
	}
	return &contracts.WorkspaceComputeRuntimeBinding{Status: status}
}

// computeReadbackFailure contains only fixed, non-secret owner diagnostics.
type computeReadbackFailure string

func (failure computeReadbackFailure) Error() string { return string(failure) }

func (s *Service) computeProviderFactInput(ctx context.Context, compute ComputeAllocation) ComputeAllocation {
	projected, _ := s.computeOwnershipReadbackInput(ctx, compute)
	return projected
}

// The current ownership store is authoritative for its claim. Reconstruct only
// an entirely missing local tag projection; never replace existing tags.
func (s *Service) computeOwnershipReadbackInput(ctx context.Context, compute ComputeAllocation) (ComputeAllocation, error) {
	if compute.Provider != "tencent-tke" || len(compute.CostTags) != 0 {
		return compute, nil
	}
	ownership, err := s.machineOwnership.MachineOwnership(ctx, compute.ID)
	if errors.Is(err, ErrMachineOwnershipNotFound) {
		return compute, computeReadbackFailure("compute_readback_ownership_missing")
	}
	if err != nil {
		return compute, computeReadbackFailure("compute_readback_ownership_unavailable")
	}
	if ownership.Status != "active" {
		return compute, computeReadbackFailure("compute_readback_ownership_inactive")
	}
	if ownership.ID == "" || ownership.ClaimedAt.IsZero() {
		return compute, computeReadbackFailure("compute_readback_ownership_invalid")
	}
	for _, check := range []struct {
		matched bool
		code    computeReadbackFailure
	}{
		{ownership.ResourceID == compute.ID, "compute_readback_ownership_resource"},
		{ownership.AccountID == compute.AccountID, "compute_readback_ownership_account"},
		{ownership.WorkspaceID == compute.WorkspaceID, "compute_readback_ownership_workspace"},
		{ownership.PackageID == compute.PackageID, "compute_readback_ownership_package"},
		{ownership.NodePoolID == compute.NodePoolID, "compute_readback_ownership_node_pool"},
		{ownership.MachineID == compute.MachineName, "compute_readback_ownership_machine"},
		{ownership.InstanceID == firstNonEmpty(compute.InstanceID, compute.CVMInstanceID), "compute_readback_ownership_instance"},
		{ownership.NodeName == compute.NodeName, "compute_readback_ownership_node"},
	} {
		if !check.matched {
			return compute, check.code
		}
	}
	compute.CostTags = oplCostTags(compute.AccountID, compute.WorkspaceID, compute.ID, ownership.ID)
	return compute, nil
}
