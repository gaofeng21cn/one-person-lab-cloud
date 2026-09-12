package fabric

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

// ErrWorkspaceApplicationRuntimeProviderUnsupported reports a provider that
// has not implemented the application runtime port yet.
var ErrWorkspaceApplicationRuntimeProviderUnsupported = errors.New("workspace_application_runtime_provider_unsupported")

// WorkspaceApplicationRuntimeInput drives one application runtime creation
// from an admitted revision. The revision is the declared description; the
// configuration digest is the identity of the non-secret configuration the
// operator supplied. Secret values never travel through this input.
type WorkspaceApplicationRuntimeInput struct {
	WorkspaceID           string                                 `json:"workspaceId"`
	ComputeID             string                                 `json:"computeId"`
	VolumeID              string                                 `json:"volumeId"`
	AttachmentID          string                                 `json:"attachmentId"`
	AttachmentOperationID string                                 `json:"attachmentOperationId"`
	RuntimeOperationID    string                                 `json:"runtimeOperationId"`
	IdempotencyKey        string                                 `json:"-"`
	OperationID           string                                 `json:"-"`
	Revision              contracts.WorkspaceApplicationRevision `json:"revision"`
	ConfigurationDigest   string                                 `json:"configurationDigest"`
}

// workspaceApplicationRuntimeProvider is the optional port a provider
// implements as its application runtime support lands. Local-Docker and
// Tencent implement it in their own slices; the engine rejects unsupported
// providers instead of guessing a container shape.
type workspaceApplicationRuntimeProvider interface {
	EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, error)
	ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error)
}

// workspaceApplicationRuntimeRecord is the durable resource payload of one
// application runtime operation: the runtime identity plus the authoritative
// component observation.
type workspaceApplicationRuntimeRecord struct {
	RuntimeID   string                                           `json:"runtimeId"`
	WorkspaceID string                                           `json:"workspaceId"`
	Observation contracts.WorkspaceApplicationRuntimeObservation `json:"observation"`
}

func workspaceApplicationRuntimeID(workspaceID string) string {
	return "rt_app_" + stableSuffix("workspace_application_runtime", workspaceID)
}

func (s *Service) validateWorkspaceApplicationRuntimeInput(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume, attachment StorageAttachment) error {
	if compute.ID == "" || volume.ID == "" || compute.AccountID == "" || compute.AccountID != volume.AccountID ||
		input.WorkspaceID == "" || input.WorkspaceID != compute.WorkspaceID || input.WorkspaceID != volume.WorkspaceID {
		return fmt.Errorf("workspace_application_runtime_resource_mismatch")
	}
	if attachment.ID == "" || input.AttachmentID != attachment.ID || input.AttachmentOperationID == "" ||
		input.AttachmentOperationID != attachment.OperationID || attachment.WorkspaceID != input.WorkspaceID ||
		attachment.ComputeID != input.ComputeID || attachment.VolumeID != input.VolumeID || attachment.Status != "attached" {
		return fmt.Errorf("workspace_application_runtime_attachment_mismatch")
	}
	if input.RuntimeOperationID == "" {
		return fmt.Errorf("workspace_application_runtime_identity_invalid")
	}
	if !isReadyResourceStatus(compute.Status) || volume.Status != "ready" {
		return fmt.Errorf("workspace_application_runtime_resource_status_invalid")
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(input.Revision); err != nil {
		return err
	}
	// Application images are publisher-owned, digest-pinned references admitted
	// by Control Plane; the OPL image catalog deliberately does not apply here.
	if strings.TrimSpace(input.ConfigurationDigest) == "" {
		return fmt.Errorf("workspace_application_runtime_configuration_digest_required")
	}
	return nil
}

// CreateWorkspaceApplicationRuntime creates the multi-component runtime of one
// admitted revision with the same durable claim, idempotency and readback
// convergence rules as the retained OPL App runtime creation. The observation
// is authoritative: it names every declared component where it actually runs.
func (s *Service) CreateWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("runtime_idempotency_key_required")
	}
	s.mu.Lock()
	compute := s.computes[input.ComputeID]
	volume := s.volumes[input.VolumeID]
	attachment := s.attachments[input.AttachmentID]
	s.mu.Unlock()
	if err := s.validateWorkspaceApplicationRuntimeInput(input, compute, volume, attachment); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	requestHash := hashInput(input)
	now := s.now()
	action := "create_workspace_application_runtime"
	operation := newOperation(action, "workspace_application_runtime", input.WorkspaceID, compute.AccountID, input.WorkspaceID, input.IdempotencyKey, requestHash, now)
	operation.ID = "fop_app_runtime_claim_" + stableSuffix(action, input.IdempotencyKey)
	operation.Status = "started"
	operation.CreatedAt = now
	record := workspaceApplicationRuntimeRecord{RuntimeID: workspaceApplicationRuntimeID(input.WorkspaceID), WorkspaceID: input.WorkspaceID}
	fillOperationResource(&operation, record)
	input.OperationID = input.IdempotencyKey
	stored, claimed, err := s.claimRuntimeOperation(ctx, operation)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if !claimed {
		if stored.RequestHash != requestHash {
			return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeIdempotencyConflict
		}
		return s.replayWorkspaceApplicationRuntime(ctx, stored, input)
	}
	observation, record, ensureErr := s.ensureWorkspaceApplicationRuntime(ctx, input, stored, compute, volume)
	if ensureErr != nil {
		_ = s.saveWorkspaceApplicationRuntimeOperation(ctx, stored, "failed", record, ensureErr)
		return observation, ensureErr
	}
	if err := s.saveWorkspaceApplicationRuntimeOperation(ctx, stored, "succeeded", record, nil); err != nil {
		return observation, err
	}
	return observation, nil
}

func (s *Service) ensureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, operation FabricOperation, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, workspaceApplicationRuntimeRecord, error) {
	record := workspaceApplicationRuntimeRecord{RuntimeID: workspaceApplicationRuntimeID(input.WorkspaceID), WorkspaceID: input.WorkspaceID}
	provider, ok := s.runtimeProvider.(workspaceApplicationRuntimeProvider)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeObservation{}, record, ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	observation, err := provider.EnsureWorkspaceApplicationRuntime(s.providerMutationContext(ctx, operation), input, compute, volume)
	if err == nil {
		err = contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, observation)
	}
	if err == nil && observation.RuntimeID == "" {
		observation.RuntimeID = record.RuntimeID
	}
	record.Observation = observation
	return observation, record, err
}

// replayWorkspaceApplicationRuntime resolves an already-claimed operation. A
// stale started or failed claim is resolved by authoritative readback, never
// by re-applying the provider mutation.
func (s *Service) replayWorkspaceApplicationRuntime(ctx context.Context, stored FabricOperation, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	switch stored.Status {
	case "started":
		if !runtimeOperationNeedsReadback(stored, s.now()) {
			return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationInProgress
		}
	case "succeeded":
		var record workspaceApplicationRuntimeRecord
		if decodeOperationResource(stored, &record) && record.Observation.RuntimeID != "" {
			return record.Observation, nil
		}
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationFailed
	case "failed":
	default:
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationFailed
	}
	return s.convergeWorkspaceApplicationRuntime(ctx, stored, input)
}

func (s *Service) convergeWorkspaceApplicationRuntime(ctx context.Context, stored FabricOperation, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	provider, ok := s.runtimeProvider.(workspaceApplicationRuntimeProvider)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	observation, err := provider.ReadWorkspaceApplicationRuntime(ctx, input)
	if err != nil || contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, observation) != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationFailed
	}
	if observation.RuntimeID == "" {
		observation.RuntimeID = workspaceApplicationRuntimeID(input.WorkspaceID)
	}
	record := workspaceApplicationRuntimeRecord{RuntimeID: observation.RuntimeID, WorkspaceID: input.WorkspaceID, Observation: observation}
	if _, err := s.convergeRuntimeOperationReadback(ctx, stored, record, nil); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	return observation, nil
}

func (s *Service) saveWorkspaceApplicationRuntimeOperation(ctx context.Context, operation FabricOperation, status string, record workspaceApplicationRuntimeRecord, operationErr error) error {
	operation.Status = status
	operation.FinishedAt = s.now()
	operation.ErrorCode = errorCode(operationErr)
	operation.Retryable = false
	fillOperationResource(&operation, record)
	return s.runtimeOperations.SaveRuntime(ctx, operation)
}

// WorkspaceApplicationRuntimeReadback returns the authoritative component
// observation of one application runtime, converging the durable operation
// with a live provider read when the provider supports it.
func (s *Service) WorkspaceApplicationRuntimeReadback(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	// After the claim, the operation's ResourceID is the deterministic runtime
	// identity assigned by fillOperationResource.
	operation, found, err := s.runtimeOperationQueries.OperationByResourceActionIdempotency(
		ctx, "workspace_application_runtime", workspaceApplicationRuntimeID(input.WorkspaceID),
		"create_workspace_application_runtime", input.IdempotencyKey,
	)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if !found {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("workspace_application_runtime_not_found")
	}
	if operation.Status == "started" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationInProgress
	}
	if operation.Status != "succeeded" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationFailed
	}
	var record workspaceApplicationRuntimeRecord
	if !decodeOperationResource(operation, &record) || record.Observation.RuntimeID == "" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationFailed
	}
	// The succeeded record keeps the creation-time authority; a live read is
	// returned to the caller without rewriting it.
	if provider, ok := s.runtimeProvider.(workspaceApplicationRuntimeProvider); ok {
		live, readErr := provider.ReadWorkspaceApplicationRuntime(ctx, input)
		if readErr == nil && contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, live) == nil {
			if live.RuntimeID == "" {
				live.RuntimeID = record.RuntimeID
			}
			return live, nil
		}
	}
	return record.Observation, nil
}
