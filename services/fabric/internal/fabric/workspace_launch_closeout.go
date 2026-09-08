package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

type WorkspaceLaunchCloseoutInput = contracts.WorkspaceLaunchCloseoutInput
type WorkspaceLaunchCloseoutResult = contracts.WorkspaceLaunchCloseoutResult

var ErrWorkspaceLaunchFrozen = errors.New("workspace_launch_frozen")

const workspaceLaunchCloseoutPayloadKey = "workspaceLaunchCloseout"

func workspaceLaunchLockKey(id string) string    { return "workspace-launch:" + id }
func workspaceLaunchCloseoutID(id string) string { return "fop_launch_closeout_" + stableSuffix(id) }

type workspaceLaunchCloseoutResourceReadback struct {
	State   string
	Compute *ComputeAllocation
	Storage *StorageVolume
}

type workspaceLaunchCloseoutProvider interface {
	ReadWorkspaceLaunchCloseoutResource(context.Context, WorkspaceLaunchProviderRequest) (workspaceLaunchCloseoutResourceReadback, error)
	DestroyWorkspaceLaunchGatewaySecret(context.Context, WorkspaceLaunchProviderRequest) error
}

type workspaceLaunchCloseoutSnapshot struct {
	result     WorkspaceLaunchCloseoutResult
	stages     map[string]FabricOperation
	requests   map[string]WorkspaceLaunchProviderRequest
	compute    *ComputeAllocation
	storage    *StorageVolume
	attachment *StorageAttachment
}

func (s *Service) workspaceLaunchFrozen(ctx context.Context, launchID string) (bool, error) {
	op, err := s.workspaceLaunchPreflights.Get(ctx, workspaceLaunchCloseoutID(launchID))
	if errors.Is(err, ErrOperationNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var binding WorkspaceLaunchCloseoutInput
	if !decodeWorkspaceLaunchCloseoutPayload(op.RedactedProviderPayload[workspaceLaunchCloseoutPayloadKey], &binding) ||
		binding.LaunchOperationID != launchID || op.RequestHash != hashInput(binding) || op.Action != "closeout_workspace_launch" {
		return false, ErrLaunchStageBindingConflict
	}
	return true, nil
}

func (s *Service) ReadWorkspaceLaunchCloseout(ctx context.Context, input WorkspaceLaunchCloseoutInput) (WorkspaceLaunchCloseoutResult, error) {
	var result WorkspaceLaunchCloseoutResult
	err := s.resourceLocks.WithPoolLock(ctx, workspaceLaunchLockKey(input.LaunchOperationID), func(ctx context.Context) error {
		snapshot, err := s.readWorkspaceLaunchCloseout(ctx, input)
		result = snapshot.result
		return err
	})
	return result, err
}

func (s *Service) FreezeWorkspaceLaunch(ctx context.Context, input WorkspaceLaunchCloseoutInput) (WorkspaceLaunchCloseoutResult, error) {
	return s.mutateWorkspaceLaunchCloseout(ctx, input, false)
}

func (s *Service) CloseoutWorkspaceLaunch(ctx context.Context, input WorkspaceLaunchCloseoutInput) (WorkspaceLaunchCloseoutResult, error) {
	return s.mutateWorkspaceLaunchCloseout(ctx, input, true)
}

func (s *Service) mutateWorkspaceLaunchCloseout(ctx context.Context, input WorkspaceLaunchCloseoutInput, cleanup bool) (WorkspaceLaunchCloseoutResult, error) {
	var result WorkspaceLaunchCloseoutResult
	err := s.resourceLocks.WithPoolLock(ctx, workspaceLaunchLockKey(input.LaunchOperationID), func(ctx context.Context) error {
		snapshot, err := s.readWorkspaceLaunchCloseout(ctx, input)
		result = snapshot.result
		if err != nil || result.State == "blocked" {
			return err
		}
		if !result.Frozen {
			now := s.now()
			op := newOperation("closeout_workspace_launch", "workspace_launch_closeout", input.LaunchOperationID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), now)
			op.ID, op.OperationID = workspaceLaunchCloseoutID(input.LaunchOperationID), input.IdempotencyKey
			op.Status, op.Provider, op.CreatedAt = "started", input.ProviderProfileRef, now
			op.RedactedProviderPayload = map[string]any{workspaceLaunchCloseoutPayloadKey: input}
			stored, _, claimErr := s.runtimeOperations.ClaimRuntime(ctx, op)
			if claimErr != nil {
				return claimErr
			}
			if stored.ID != op.ID || stored.RequestHash != op.RequestHash {
				return ErrLaunchStageBindingConflict
			}
			result.Frozen, snapshot.result.Frozen = true, true
		}
		if !cleanup {
			return nil
		}
		if result.State == "pending" {
			return nil
		}
		if err := s.cleanupWorkspaceLaunchResources(ctx, input, snapshot); err != nil {
			result.State, result.Reason = "pending", errorCode(err)
			return nil
		}
		snapshot, err = s.readWorkspaceLaunchCloseout(ctx, input)
		result = snapshot.result
		if err != nil || result.State != "absent" {
			return err
		}
		if err := s.finishWorkspaceLaunchCloseout(ctx, input, snapshot.stages, result); err != nil {
			result.State, result.Reason = "pending", errorCode(err)
			return nil
		}
		return nil
	})
	return result, err
}

func (s *Service) readWorkspaceLaunchCloseout(ctx context.Context, input WorkspaceLaunchCloseoutInput) (workspaceLaunchCloseoutSnapshot, error) {
	x := workspaceLaunchCloseoutSnapshot{
		result: WorkspaceLaunchCloseoutResult{SchemaVersion: 1, Binding: input, State: "eligible", Reason: "resources_identified", Resources: []contracts.WorkspaceLaunchCloseoutResource{}},
		stages: map[string]FabricOperation{}, requests: map[string]WorkspaceLaunchProviderRequest{},
	}
	if input.SchemaVersion != 1 || !validWorkspaceLaunchHash(input.SpecDigest) {
		return x, ErrWorkspaceLaunchInputInvalid
	}
	for _, value := range []string{input.LaunchOperationID, input.AccountID, input.WorkspaceID, input.ProviderProfileRef, input.ProviderBindingRef, input.IdempotencyKey} {
		if value == "" || value != strings.TrimSpace(value) {
			return x, ErrWorkspaceLaunchInputInvalid
		}
	}
	admission, err := s.launchStages.workspaceLaunchPreflight(ctx, input.ProviderBindingRef)
	if err != nil {
		return x, err
	}
	if admission.Input.LaunchOperationID != input.LaunchOperationID || admission.Input.AccountID != input.AccountID || admission.Input.WorkspaceID != input.WorkspaceID ||
		admission.ProviderProfileRef != input.ProviderProfileRef || admission.ProviderBindingRef != input.ProviderBindingRef || admission.SpecDigest != input.SpecDigest ||
		input.ProviderProfileRef != s.providerDescriptor.Descriptor().Name {
		return x, ErrLaunchStageBindingConflict
	}
	frozen, err := s.workspaceLaunchFrozen(ctx, input.LaunchOperationID)
	if err != nil {
		return x, err
	}
	x.result.Frozen = frozen
	if frozen {
		op, getErr := s.workspaceLaunchPreflights.Get(ctx, workspaceLaunchCloseoutID(input.LaunchOperationID))
		if getErr != nil || op.RequestHash != hashInput(input) {
			return x, ErrLaunchStageBindingConflict
		}
	}
	operations, err := s.launchStages.stages.WorkspaceLaunchStages(ctx, input.WorkspaceID)
	if err != nil {
		return x, err
	}
	for _, op := range operations {
		if op.WorkspaceID != input.WorkspaceID || op.ResourceKind != "workspace_launch_stage" {
			continue
		}
		binding, ok := decodeLaunchStageBinding(op)
		record, recordOK := decodeWorkspaceLaunchStageRecord(op)
		if !ok || !recordOK || binding.LaunchOperationID != input.LaunchOperationID || binding.AccountID != input.AccountID ||
			record.ProviderProfileRef != input.ProviderProfileRef || record.ProviderBindingRef != input.ProviderBindingRef || record.SpecDigest != input.SpecDigest {
			return x, ErrLaunchStageBindingConflict
		}
		if _, exists := x.stages[binding.Stage]; exists {
			return x, ErrLaunchStageBindingConflict
		}
		x.stages[binding.Stage] = op
	}
	for stage, op := range x.stages {
		binding, _ := decodeLaunchStageBinding(op)
		record, _ := decodeWorkspaceLaunchStageRecord(op)
		request := WorkspaceLaunchProviderRequest{
			Input: WorkspaceLaunchStageInput{Binding: binding, ProviderProfileRef: input.ProviderProfileRef, ProviderBindingRef: input.ProviderBindingRef,
				SpecDigest: input.SpecDigest, PackageID: admission.Input.PackageID, SizeGB: admission.Input.SizeGB, WorkspaceImageDigest: admission.Input.WorkspaceImageDigest,
				Resources: record.Resources, RuntimeImageRevision: record.RuntimeImageRevision},
			Current: record, Prior: map[string]workspaceLaunchStageRecord{}, ProviderPlan: admission.CanonicalProviderPlan,
		}
		for _, prior := range workspaceLaunchRequiredPriorStages(stage) {
			parent, exists := x.stages[prior]
			parentRecord, valid := decodeWorkspaceLaunchStageRecord(parent)
			if !exists || !valid || parent.Status != "succeeded" {
				return x, ErrLaunchStageBindingConflict
			}
			request.Prior[prior] = parentRecord
		}
		x.requests[stage] = request
	}
	block := func(reason string) (workspaceLaunchCloseoutSnapshot, error) {
		x.result.State, x.result.Reason = "blocked", reason
		return x, nil
	}
	if runtime, exists := x.stages["runtime"]; exists && !frozen && runtime.Status == "succeeded" {
		return block("runtime_already_deliverable")
	}
	runtime := s.ObserveWorkspaceRuntimeDelete(ctx, input.WorkspaceID)
	if runtime.State != WorkspaceOwnerObservationAbsent && runtime.State != WorkspaceRuntimeDeleteObservationPresent {
		return block("runtime_owner_unknown")
	}
	if runtime.State == WorkspaceRuntimeDeleteObservationPresent {
		if _, hasSecret := x.stages["secret"]; !hasSecret {
			return block("runtime_identity_conflict")
		}
		if request, exists := x.requests["runtime"]; exists {
			_, readErr := s.launchStages.provider.ReadWorkspaceLaunchStage(s.launchStages.providerOperationContext(ctx, x.stages["runtime"], true), request)
			if readErr == nil && !frozen {
				return block("runtime_already_deliverable")
			}
			if readErr != nil && !errors.Is(readErr, ErrWorkspaceLaunchPending) && !errors.Is(readErr, ErrWorkspaceLaunchResourceAbsent) {
				return block("runtime_owner_unknown")
			}
		} else {
			request := x.requests["secret"]
			if _, readErr := s.launchStages.provider.ReadWorkspaceLaunchStage(s.launchStages.providerOperationContext(ctx, x.stages["secret"], true), request); readErr != nil {
				return block("secret_owner_unknown")
			}
		}
	}
	x.result.Resources = append(x.result.Resources, contracts.WorkspaceLaunchCloseoutResource{Stage: "runtime", OperationID: x.stages["runtime"].ID, ResourceID: input.WorkspaceID, State: runtime.State})
	reader, supported := s.launchStages.provider.(workspaceLaunchCloseoutProvider)
	allAbsent := runtime.State == WorkspaceOwnerObservationAbsent
	for _, stage := range []string{"secret", "storage", "ensure_compute_allocation"} {
		resource := contracts.WorkspaceLaunchCloseoutResource{Stage: stage, OperationID: x.stages[stage].ID, State: "absent"}
		if stage == "secret" {
			resource.ResourceID = gatewaySecretName(input.WorkspaceID)
		}
		if request, exists := x.requests[stage]; exists {
			if !supported {
				return block("provider_closeout_unavailable")
			}
			readback, readErr := reader.ReadWorkspaceLaunchCloseoutResource(s.launchStages.providerOperationContext(ctx, x.stages[stage], true), request)
			if readErr != nil {
				resource.State = "unknown"
			} else {
				resource.State = readback.State
			}
			if readback.Compute != nil {
				x.compute = readback.Compute
				resource.ResourceID = readback.Compute.ID
			}
			if readback.Storage != nil {
				x.storage = readback.Storage
				resource.ResourceID = readback.Storage.ID
			}
			if resource.State != "present" && resource.State != "absent" {
				x.result.State, x.result.Reason = "pending", stage+"_owner_unknown"
			}
		}
		allAbsent = allAbsent && resource.State == "absent"
		x.result.Resources = append(x.result.Resources, resource)
	}
	if stage, exists := x.stages["attachment"]; exists {
		binding, _ := decodeLaunchStageBinding(stage)
		record, _ := decodeWorkspaceLaunchStageRecord(stage)
		var state struct {
			Attachment *StorageAttachment `json:"attachment"`
		}
		if decodeWorkspaceLaunchCloseoutPayload(record.ProviderState, &state) && state.Attachment != nil {
			x.attachment = state.Attachment
		} else if x.compute != nil && x.storage != nil {
			x.attachment = &StorageAttachment{ID: workspaceLaunchAttachmentID(binding), OperationID: binding.FabricOperationID, WorkspaceID: input.WorkspaceID, ComputeID: x.compute.ID, VolumeID: x.storage.ID, Provider: input.ProviderProfileRef}
		}
	}
	if x.attachment != nil {
		state := "present"
		latest, exists, err := s.resourceOperations.LatestResourceOperation(ctx, "storage_attachment", x.attachment.ID)
		if err != nil {
			return x, err
		}
		if exists && latest.Action == "detach_storage_attachment" && latest.Status == "succeeded" {
			var detached StorageAttachment
			if !decodeOperationResource(latest, &detached) || detached.ID != x.attachment.ID || detached.WorkspaceID != input.WorkspaceID || detached.ComputeID != x.attachment.ComputeID || detached.VolumeID != x.attachment.VolumeID || detached.Status != "detached" {
				return x, ErrLaunchStageBindingConflict
			}
			state = "absent"
		}
		x.result.Resources = append(x.result.Resources, contracts.WorkspaceLaunchCloseoutResource{Stage: "attachment", OperationID: x.stages["attachment"].ID, ResourceID: x.attachment.ID, State: state})
		allAbsent = allAbsent && state == "absent"
	} else {
		if _, exists := x.stages["attachment"]; exists {
			return x, ErrLaunchStageBindingConflict
		}
		x.result.Resources = append(x.result.Resources, contracts.WorkspaceLaunchCloseoutResource{Stage: "attachment", State: "absent"})
	}

	if allAbsent {
		x.result.State, x.result.Reason = "absent", "resources_absent"
	}
	for _, stage := range x.stages {
		if stage.ComputePoolLeaseExpires != nil && stage.ComputePoolLeaseExpires.After(s.now()) {
			x.result.State, x.result.Reason = "pending", "launch_dispatch_in_flight"
		}
	}

	return x, nil
}

func (s *Service) cleanupWorkspaceLaunchResources(ctx context.Context, input WorkspaceLaunchCloseoutInput, x workspaceLaunchCloseoutSnapshot) error {
	if err := s.validateWorkspaceLaunchCloseoutCache(x); err != nil {
		return err
	}
	if x.result.Resources[0].State != "absent" {
		if _, err := s.DestroyWorkspaceRuntime(ctx, input.WorkspaceID, input.IdempotencyKey+":runtime"); err != nil {
			return err
		}
		if observation := s.ObserveWorkspaceRuntimeDelete(ctx, input.WorkspaceID); observation.State != WorkspaceOwnerObservationAbsent {
			return ErrWorkspaceLaunchPending
		}
	}

	if workspaceLaunchCloseoutResourceState(x.result, "secret") != "absent" {
		provider := s.launchStages.provider.(workspaceLaunchCloseoutProvider)
		if err := provider.DestroyWorkspaceLaunchGatewaySecret(s.launchStages.providerOperationContext(ctx, x.stages["secret"], false), x.requests["secret"]); err != nil {
			return err
		}
	}
	// These are cache projections of exact, persisted Launch identities. Existing
	// destroy operations remain the durable owner of delete dispatch and recovery.
	s.mu.Lock()
	if x.compute != nil && s.computes[x.compute.ID].Status != "destroying" && s.computes[x.compute.ID].Status != "destroyed" && !isExternallyDeletedComputeStatus(s.computes[x.compute.ID].Status) {
		s.computes[x.compute.ID] = cloneComputeAllocation(*x.compute)
	}
	if x.storage != nil && s.volumes[x.storage.ID].Status != "destroying" && s.volumes[x.storage.ID].Status != "destroyed" && s.volumes[x.storage.ID].Status != "external_deleted" {
		s.volumes[x.storage.ID] = cloneStorageVolume(*x.storage)
	}
	if x.attachment != nil && s.attachments[x.attachment.ID].ID == "" {
		s.attachments[x.attachment.ID] = *x.attachment
	}
	s.mu.Unlock()
	if x.attachment != nil && workspaceLaunchCloseoutResourceState(x.result, "attachment") != "absent" {
		if _, err := s.DetachStorageAttachment(ctx, x.attachment.ID); err != nil {
			return err
		}
	}
	if workspaceLaunchCloseoutResourceState(x.result, "storage") != "absent" && x.storage != nil {
		ctx = context.WithValue(ctx, workspaceLaunchStorageCloseoutContextKey{}, x.requests["storage"])
		if _, err := s.DestroyStorageVolume(ctx, x.storage.ID); err != nil {
			return err
		}
		return ErrWorkspaceLaunchPending
	}
	if workspaceLaunchCloseoutResourceState(x.result, "ensure_compute_allocation") != "absent" && x.compute != nil {
		if owner, ok := s.launchStages.provider.(workspaceLaunchCloseoutComputeOwner); ok {
			if err := owner.ClaimWorkspaceLaunchCloseoutCompute(s.launchStages.providerOperationContext(ctx, x.stages["ensure_compute_allocation"], false), x.requests["ensure_compute_allocation"], *x.compute); err != nil {
				return err
			}
		}
		if _, err := s.DestroyComputeAllocation(ctx, x.compute.ID); err != nil {
			return err
		}
		return ErrWorkspaceLaunchPending
	}
	return nil
}

func (s *Service) finishWorkspaceLaunchCloseout(ctx context.Context, input WorkspaceLaunchCloseoutInput, stages map[string]FabricOperation, result WorkspaceLaunchCloseoutResult) error {
	if compute, exists := stages["ensure_compute_allocation"]; exists && compute.ComputePoolKey != "" && compute.Status == "started" {

		record, valid := decodeWorkspaceLaunchStageRecord(compute)
		if !valid {
			return ErrLaunchStageBindingConflict
		}
		if record.ComputePoolQueued {
			if err := s.launchStages.stages.CancelWorkspaceLaunchQueuedCompute(ctx, compute, workspaceLaunchCloseoutID(input.LaunchOperationID), s.now()); err != nil {
				return err
			}
		} else {
			owner, err := newLeaseToken()
			if err != nil {
				return err
			}
			now := s.now()
			leased, claimed, err := s.computePool.TryClaimComputePoolHead(ctx, compute.ID, compute.ComputePoolKey, owner, now, now.Add(45*time.Second))
			if err != nil || !claimed {
				return firstNonNil(err, ErrWorkspaceLaunchPending)
			}
			defer s.computePool.ReleaseComputePoolHead(context.WithoutCancel(ctx), compute.ID, compute.ComputePoolKey, owner)
			leased.Status, leased.ErrorCode, leased.FinishedAt = "failed", "workspace_launch_closed", now
			if err := s.launchStages.stages.SaveStageOutcome(ctx, leased); err != nil {
				return err
			}
		}
	}
	op, err := s.workspaceLaunchPreflights.Get(ctx, workspaceLaunchCloseoutID(input.LaunchOperationID))
	if err != nil {
		return err
	}
	if op.Status == "succeeded" {
		return nil
	}
	op.RedactedProviderPayload = maps.Clone(op.RedactedProviderPayload)
	op.RedactedProviderPayload["workspaceLaunchCloseoutReadback"] = result
	op.Status, op.FinishedAt = "succeeded", s.now()
	return s.runtimeOperations.SaveRuntime(ctx, op)
}

func decodeWorkspaceLaunchCloseoutPayload(value any, target any) bool {
	body, err := json.Marshal(value)
	return err == nil && json.Unmarshal(body, target) == nil
}

func workspaceLaunchCloseoutResourceState(result WorkspaceLaunchCloseoutResult, stage string) string {
	for _, resource := range result.Resources {
		if resource.Stage == stage {
			return resource.State
		}
	}
	return "absent"
}

func (s *Service) validateWorkspaceLaunchCloseoutCache(x workspaceLaunchCloseoutSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x.compute != nil {
		current, expected := s.computes[x.compute.ID], *x.compute
		if current.ID != "" && (current.AccountID != expected.AccountID || current.WorkspaceID != expected.WorkspaceID || current.Provider != expected.Provider || current.PackageID != expected.PackageID || current.ProviderResourceID != "" && current.ProviderResourceID != expected.ProviderResourceID) {
			return ErrLaunchStageBindingConflict
		}
	}
	if x.storage != nil {
		current, expected := s.volumes[x.storage.ID], *x.storage
		if current.ID != "" && (current.AccountID != expected.AccountID || current.WorkspaceID != expected.WorkspaceID || current.Provider != expected.Provider || current.SizeGB != expected.SizeGB || current.ProviderResourceID != "" && current.ProviderResourceID != expected.ProviderResourceID) {
			return ErrLaunchStageBindingConflict
		}
	}
	if x.attachment != nil {
		current, expected := s.attachments[x.attachment.ID], *x.attachment
		if current.ID != "" && (current.WorkspaceID != expected.WorkspaceID || current.ComputeID != expected.ComputeID || current.VolumeID != expected.VolumeID) {
			return ErrLaunchStageBindingConflict
		}
	}
	return nil
}
