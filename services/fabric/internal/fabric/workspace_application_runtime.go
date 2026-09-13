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

// ErrWorkspaceApplicationRuntimeInputInvalid classifies rejected application
// inputs before an operation is claimed or a provider mutation is dispatched.
var ErrWorkspaceApplicationRuntimeInputInvalid = errors.New("workspace_application_runtime_input_invalid")

// WorkspaceApplicationRuntimeInput drives one application runtime creation
// from an admitted revision. The revision is the declared description; the
// configuration digest is the identity of the non-secret configuration the
// operator supplied. Secret values never travel through this input.
type WorkspaceApplicationRuntimeInput = contracts.WorkspaceApplicationRuntimeInput

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
	Input       WorkspaceApplicationRuntimeInput                 `json:"input"`
}

func workspaceApplicationRuntimeID(runtimeOperationID string) string {
	return contracts.WorkspaceApplicationRuntimeID(runtimeOperationID)
}

func (s *Service) validateWorkspaceApplicationRuntimeInput(input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume, attachment StorageAttachment) error {
	if compute.ID == "" || volume.ID == "" || compute.AccountID == "" || compute.AccountID != volume.AccountID ||
		input.AccountID == "" || input.AccountID != compute.AccountID ||
		input.WorkspaceID == "" || input.WorkspaceID != compute.WorkspaceID || input.WorkspaceID != volume.WorkspaceID {
		return fmt.Errorf("workspace_application_runtime_resource_mismatch")
	}
	if attachment.ID == "" || input.AttachmentID != attachment.ID || input.AttachmentOperationID == "" ||
		input.AttachmentOperationID != attachment.OperationID || attachment.WorkspaceID != input.WorkspaceID ||
		attachment.ComputeID != input.ComputeID || attachment.VolumeID != input.VolumeID || attachment.Status != "attached" {
		return fmt.Errorf("workspace_application_runtime_attachment_mismatch")
	}
	if input.RuntimeOperationID == "" || input.IdempotencyKey != input.RuntimeOperationID {
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
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		return err
	}
	return nil
}

// CreateWorkspaceApplicationRuntime creates the multi-component runtime of one
// admitted revision with the same durable claim, idempotency and readback
// convergence rules as the retained OPL App runtime creation. The observation
// is authoritative: it names every declared component where it actually runs.
func (s *Service) CreateWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if input.SchemaVersion == 0 {
		return s.historicalApplicationReadback(ctx, input, false)
	}
	var result contracts.WorkspaceApplicationRuntimeObservation
	err := s.resourceLocks.WithPoolLock(ctx, workspaceRuntimeLockKey(input.WorkspaceID), func(ctx context.Context) error {
		var err error
		result, err = s.createWorkspaceApplicationRuntime(ctx, input)
		return err
	})
	return result, err
}

func (s *Service) createWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, errors.Join(ErrWorkspaceApplicationRuntimeInputInvalid, errors.New("runtime_idempotency_key_required"))
	}
	s.mu.Lock()
	compute := s.computes[input.ComputeID]
	volume := s.volumes[input.VolumeID]
	attachment := s.attachments[input.AttachmentID]
	s.mu.Unlock()
	if err := s.validateWorkspaceApplicationRuntimeInput(input, compute, volume, attachment); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, errors.Join(ErrWorkspaceApplicationRuntimeInputInvalid, err)
	}
	latest, found, lifecycleErr := s.resourceOperations.LatestResourceOperation(ctx, "workspace_application_lifecycle", applicationRuntimeID(input))
	if lifecycleErr != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, lifecycleErr
	}
	if found {
		var previous workspaceApplicationLifecycleRecord
		if !decodeOperationResource(latest, &previous) {
			return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeIdempotencyConflict
		}
		if previous.Input.AccountID != input.AccountID || previous.Input.WorkspaceID != input.WorkspaceID {
			return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeIdempotencyConflict
		}
		if previous.Input.DesiredState != "running" {
			return contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: applicationRuntimeID(input), Status: "absent", Components: contracts.WorkspaceApplicationRuntimeComponents(input.Revision)}, errors.New("workspace_application_runtime_fenced")
		}
	}
	if err := s.validateApplicationDataLayout(ctx, input); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	requestHash := hashInput(input)
	now := s.now()
	action := "create_workspace_application_runtime"
	operation := newOperation(action, "workspace_application_runtime", input.WorkspaceID, compute.AccountID, input.WorkspaceID, input.IdempotencyKey, requestHash, now)
	operation.ID = "fop_app_runtime_claim_" + stableSuffix(action, input.IdempotencyKey)
	operation.Status = "started"
	operation.CreatedAt = now
	record := workspaceApplicationRuntimeRecord{RuntimeID: applicationRuntimeID(input), WorkspaceID: input.WorkspaceID, Input: input}
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
	if errors.Is(ensureErr, ErrWorkspaceLaunchPending) {
		// Components are still coming up; the claim stays started so the next
		// replay resolves by readback.
		if err := s.saveWorkspaceApplicationRuntimeOperation(ctx, stored, "started", record, nil); err != nil {
			return observation, err
		}
		return observation, ensureErr
	}
	if ensureErr != nil {
		saveErr := s.saveWorkspaceApplicationRuntimeOperation(ctx, stored, "failed", record, ensureErr)
		if observation.Status == "failed" && errors.Is(ensureErr, ErrRuntimeOperationFailed) {
			return observation, saveErr
		}
		return observation, errors.Join(ensureErr, saveErr)
	}
	if err := s.saveWorkspaceApplicationRuntimeOperation(ctx, stored, "succeeded", record, nil); err != nil {
		return observation, err
	}
	return observation, nil
}

func (s *Service) ensureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, operation FabricOperation, compute ComputeAllocation, volume StorageVolume) (contracts.WorkspaceApplicationRuntimeObservation, workspaceApplicationRuntimeRecord, error) {
	record := workspaceApplicationRuntimeRecord{RuntimeID: applicationRuntimeID(input), WorkspaceID: input.WorkspaceID, Input: input}
	provider, ok := s.runtimeProvider.(workspaceApplicationRuntimeProvider)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeObservation{}, record, ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	credentialCtx, err := s.applicationCredentialContext(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, record, err
	}
	observation, err := provider.EnsureWorkspaceApplicationRuntime(s.providerMutationContext(credentialCtx, operation), input, compute, volume)
	if err == nil || errors.Is(err, ErrWorkspaceLaunchPending) {
		err = validateWorkspaceApplicationRuntimeObservation(input, &observation)
		if err == nil {
			switch observation.Status {
			case "pending":
				err = ErrWorkspaceLaunchPending
			case "failed", "absent":
				err = ErrRuntimeOperationFailed
			}
		}
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
		var record workspaceApplicationRuntimeRecord
		providerReturned := decodeOperationResource(stored, &record) && record.Observation.Status == "pending"
		if !providerReturned && !runtimeOperationNeedsReadback(stored, s.now()) {
			return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationInProgress
		}
	case "succeeded":
		return s.readWorkspaceApplicationRuntime(ctx, input)
	case "failed":
	default:
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeOperationFailed
	}
	return s.convergeWorkspaceApplicationRuntime(ctx, stored, input)
}

func (s *Service) convergeWorkspaceApplicationRuntime(ctx context.Context, stored FabricOperation, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	observation, err := s.readWorkspaceApplicationRuntime(ctx, input)
	if err != nil {
		return observation, err
	}
	if observation.Status != "ready" {
		if observation.Status == "pending" {
			return observation, ErrWorkspaceLaunchPending
		}
		if observation.Status == "failed" {
			return observation, nil
		}
		return observation, ErrRuntimeOperationFailed
	}
	record := workspaceApplicationRuntimeRecord{RuntimeID: observation.RuntimeID, WorkspaceID: input.WorkspaceID, Observation: observation, Input: input}
	if _, err := s.convergeRuntimeOperationReadback(ctx, stored, record, nil); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	return observation, nil
}

func (s *Service) saveWorkspaceApplicationRuntimeOperation(ctx context.Context, operation FabricOperation, status string, record workspaceApplicationRuntimeRecord, operationErr error) error {
	operation.Status = status
	if status != "started" {
		operation.FinishedAt = s.now()
	}
	operation.ErrorCode = errorCode(operationErr)
	operation.Retryable = false
	fillOperationResource(&operation, record)
	return s.runtimeOperations.SaveRuntime(ctx, operation)
}

// WorkspaceApplicationRuntimeReadback returns the authoritative component
// observation of one application runtime, converging the durable operation
// with a live provider read when the provider supports it.
func (s *Service) WorkspaceApplicationRuntimeReadback(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if input.SchemaVersion == 0 {
		return s.historicalApplicationReadback(ctx, input, true)
	}
	// After the claim, the operation's ResourceID is the deterministic runtime
	// identity assigned by fillOperationResource.
	operation, found, err := s.runtimeOperationQueries.OperationByResourceActionIdempotency(
		ctx, "workspace_application_runtime", applicationRuntimeID(input),
		"create_workspace_application_runtime", input.IdempotencyKey,
	)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if !found {
		return contracts.WorkspaceApplicationRuntimeObservation{}, fmt.Errorf("workspace_application_runtime_not_found")
	}
	if operation.RequestHash != hashInput(input) {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrRuntimeIdempotencyConflict
	}
	return s.replayWorkspaceApplicationRuntime(ctx, operation, input)
}

// Live reads never substitute a historical successful observation for an
// unavailable, invalid or currently unready provider result.
func (s *Service) readWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	provider, ok := s.runtimeProvider.(workspaceApplicationRuntimeProvider)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeObservation{}, ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	observation, err := provider.ReadWorkspaceApplicationRuntime(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	if err := validateWorkspaceApplicationRuntimeObservation(input, &observation); err != nil {
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	return observation, nil
}

func validateWorkspaceApplicationRuntimeObservation(input WorkspaceApplicationRuntimeInput, observation *contracts.WorkspaceApplicationRuntimeObservation) error {
	if err := contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, *observation); err != nil {
		return err
	}
	runtimeID := applicationRuntimeID(input)
	if observation.WorkspaceID != input.WorkspaceID || (observation.RuntimeID != "" && observation.RuntimeID != runtimeID) {
		return errors.New("workspace_application_runtime_observation_identity_mismatch")
	}
	observation.RuntimeID = runtimeID
	return nil
}
