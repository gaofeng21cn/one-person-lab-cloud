package fabric

import (
	"context"
	"errors"
	"strings"
)

const workspaceRuntimeImageReplacementAction = "replace_workspace_runtime_image"

var (
	ErrWorkspaceRuntimeImageReplacementUnavailable  = errors.New("workspace_runtime_image_replacement_unavailable")
	ErrWorkspaceRuntimeImageReplacementInputInvalid = errors.New("workspace_runtime_image_replacement_input_invalid")
	ErrWorkspaceRuntimeImageReplacementConflict     = errors.New("workspace_runtime_image_replacement_conflict")
)

func validWorkspaceRuntimeImageReplacementInput(input WorkspaceRuntimeImageReplacementInput, validateImage func(string) bool) bool {
	for _, value := range []string{
		input.LaunchOperationID, input.AccountID, input.WorkspaceID, input.ComputeID, input.StorageID, input.AttachmentID,
		input.RuntimeID, input.RuntimeOperationID, input.RuntimeServiceName, input.PreviousImageDigest,
		input.ReplacementImageDigest, input.IdempotencyKey,
	} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	return validateImage != nil && validateImage(input.PreviousImageDigest) && validateImage(input.ReplacementImageDigest) &&
		input.PreviousImageDigest != input.ReplacementImageDigest
}

// ReplaceWorkspaceRuntimeImage performs a narrowly scoped, idempotent image
// replacement. Resource identities are checked before the provider mutation;
// the original Launch operation and its billing facts are never rewritten.
func (s *Service) ReplaceWorkspaceRuntimeImage(ctx context.Context, input WorkspaceRuntimeImageReplacementInput) (WorkspaceRuntimeImageReplacementResult, error) {
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return WorkspaceRuntimeImageReplacementResult{}, ErrWorkspaceRuntimeImageReplacementInputInvalid
	}
	if !validWorkspaceRuntimeImageReplacementInput(input, s.workspaceImagePolicy.ValidateWorkspaceImageReference) {
		return WorkspaceRuntimeImageReplacementResult{}, ErrWorkspaceRuntimeImageReplacementInputInvalid
	}
	provider := s.optionalProviders.runtimeImageReplacement
	if provider == nil {
		return WorkspaceRuntimeImageReplacementResult{}, ErrWorkspaceRuntimeImageReplacementUnavailable
	}
	if !s.workspaceRuntimeImageReplacementResourcesMatch(input) {
		return WorkspaceRuntimeImageReplacementResult{}, ErrWorkspaceRuntimeImageReplacementConflict
	}
	returnResult := WorkspaceRuntimeImageReplacementResult{
		SchemaVersion:          1,
		OperationID:            input.IdempotencyKey,
		WorkspaceID:            input.WorkspaceID,
		RuntimeID:              input.RuntimeID,
		PreviousImageDigest:    input.PreviousImageDigest,
		ReplacementImageDigest: input.ReplacementImageDigest,
		Status:                 "started",
	}
	requestHash := hashInput(input)
	now := s.now()
	operation := newOperation(workspaceRuntimeImageReplacementAction, "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, requestHash, now)
	operation.ID = "fop_runtime_image_replacement_claim_" + stableSuffix(workspaceRuntimeImageReplacementAction, input.IdempotencyKey)[:18]
	operation.OperationID = input.IdempotencyKey
	operation.Status = "started"
	operation.CreatedAt = now
	operation.RedactedProviderPayload = map[string]any{"replacement": input}

	err := s.resourceLocks.WithPoolLock(ctx, "workspace-runtime-image-replacement:"+input.WorkspaceID, func(lockCtx context.Context) error {
		stored, claimed, err := s.claimRuntimeOperation(lockCtx, operation)
		if err != nil {
			return err
		}
		if !claimed {
			if stored.RequestHash != requestHash {
				return ErrRuntimeIdempotencyConflict
			}
			if stored.Status == "succeeded" {
				return decodeWorkspaceRuntimeImageReplacementResult(stored, &returnResult)
			}
			readback, readErr := s.runtimeRead.providerStatus(lockCtx, input.WorkspaceID)
			readback.Access.Password = ""
			if readErr == nil && replacementRuntimeReadbackMatches(readback, input) && readback.Ready {
				if _, convergeErr := s.convergeRuntimeOperationReadback(lockCtx, stored, readback, map[string]any{"replacement": input}); convergeErr != nil {
					return convergeErr
				}
				returnResult.Runtime = readback
				returnResult.Status = "succeeded"
				return nil
			}
			if stored.Status == "started" {
				return ErrRuntimeOperationInProgress
			}
			return ErrRuntimeOperationFailed
		}
		imageReader, imageReaderOK := s.providerDescriptor.(workspaceImageReferenceReader)
		protectedImage := ""
		if imageReaderOK {
			protectedImage = strings.TrimSpace(imageReader.WorkspaceImageReference())
		}
		if protectedImage == "" || input.ReplacementImageDigest != protectedImage {
			_ = s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, WorkspaceRuntime{WorkspaceID: input.WorkspaceID}, "failed", ErrWorkspaceRuntimeImageReplacementConflict)
			return ErrWorkspaceRuntimeImageReplacementConflict
		}

		current, err := s.runtimeRead.providerStatus(lockCtx, input.WorkspaceID)
		current.Access.Password = ""
		if err != nil {
			_ = s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, current, "started", err)
			return err
		}
		if !replacementRuntimeSourceMatches(current, input) {
			_ = s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, current, "failed", ErrWorkspaceRuntimeImageReplacementConflict)
			return ErrWorkspaceRuntimeImageReplacementConflict
		}

		result, err := provider.ReplaceWorkspaceRuntimeImage(s.providerMutationContextForRuntimeImageReplacement(lockCtx, stored, input), input)
		result.Access.Password = ""
		if err != nil {
			// A provider timeout may occur after the patch reached Kubernetes. Keep
			// the parent claim started so the next worker pass resolves by readback.
			_ = s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, result, "started", err)
			return err
		}
		if !replacementRuntimeReadbackMatches(result, input) {
			_ = s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, result, "started", ErrWorkspaceRuntimeImageReplacementConflict)
			return ErrWorkspaceRuntimeImageReplacementConflict
		}
		if !result.Ready {
			_ = s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, result, "started", ErrRuntimeOperationInProgress)
			return ErrRuntimeOperationInProgress
		}
		if err := s.saveWorkspaceRuntimeImageReplacementOperation(lockCtx, stored, input, result, "succeeded", nil); err != nil {
			return err
		}
		returnResult.Runtime = result
		returnResult.Status = "succeeded"
		return nil
	})
	return returnResult, err
}

// workspaceRuntimeImageReplacementResourcesMatch keeps the provider capability
// behind Fabric's persisted owner chain. The provider still verifies the live
// Deployment labels, but IDs supplied by the Control Plane must first resolve
// to the same ready Compute/CBS/Attachment records in this Fabric instance.
func (s *Service) workspaceRuntimeImageReplacementResourcesMatch(input WorkspaceRuntimeImageReplacementInput) bool {
	s.mu.Lock()
	compute, computeOK := s.computes[input.ComputeID]
	volume, volumeOK := s.volumes[input.StorageID]
	attachment, attachmentOK := s.attachments[input.AttachmentID]
	s.mu.Unlock()
	if !computeOK || !volumeOK || !attachmentOK || compute.ID != input.ComputeID || volume.ID != input.StorageID || attachment.ID != input.AttachmentID {
		return false
	}
	if compute.AccountID != input.AccountID || volume.AccountID != input.AccountID ||
		compute.WorkspaceID != input.WorkspaceID || volume.WorkspaceID != input.WorkspaceID || attachment.WorkspaceID != input.WorkspaceID {
		return false
	}
	if compute.Status == "" || !isReadyResourceStatus(compute.Status) || volume.Status != "ready" || attachment.Status != "attached" {
		return false
	}
	providerName := strings.TrimSpace(s.providerDescriptor.Descriptor().Name)
	if providerName == "" {
		return false
	}
	for _, resourceProvider := range []string{compute.Provider, volume.Provider, attachment.Provider} {
		if strings.TrimSpace(resourceProvider) != "" && strings.TrimSpace(resourceProvider) != providerName {
			return false
		}
	}
	return attachment.ComputeID == input.ComputeID && attachment.VolumeID == input.StorageID &&
		attachment.OperationID != ""
}

func replacementRuntimeSourceMatches(runtime WorkspaceRuntime, input WorkspaceRuntimeImageReplacementInput) bool {
	return runtime.WorkspaceID == input.WorkspaceID && runtime.ID == input.RuntimeID &&
		runtime.OperationID == input.RuntimeOperationID && runtime.ServiceName == input.RuntimeServiceName &&
		runtime.ImageID == input.PreviousImageDigest
}

func replacementRuntimeReadbackMatches(runtime WorkspaceRuntime, input WorkspaceRuntimeImageReplacementInput) bool {
	return runtime.WorkspaceID == input.WorkspaceID && runtime.ID == input.RuntimeID &&
		runtime.OperationID == input.RuntimeOperationID && runtime.ServiceName == input.RuntimeServiceName &&
		runtime.ImageID == input.ReplacementImageDigest
}

func (s *Service) providerMutationContextForRuntimeImageReplacement(ctx context.Context, operation FabricOperation, input WorkspaceRuntimeImageReplacementInput) context.Context {
	binding := WorkspaceLaunchStageBinding{
		SchemaVersion: 1, LaunchOperationID: input.LaunchOperationID, AccountID: input.AccountID,
		WorkspaceID: input.WorkspaceID, Stage: "runtime", Action: "ensure_runtime",
		FabricOperationID: operation.OperationID, IdempotencyKey: operation.IdempotencyKey,
		RequestHash: operation.RequestHash, ExpectedResourceBinding: input.RuntimeServiceName,
	}
	return context.WithValue(ctx, providerMutationJournalContextKey{}, &providerMutationJournal{
		operations: s.providerMutations, machineOwnership: s.machineOwnership, parent: binding,
		parentOperation: operation, provider: s.providerDescriptor.Descriptor().Name, now: s.now,
	})
}

func (s *Service) saveWorkspaceRuntimeImageReplacementOperation(ctx context.Context, operation FabricOperation, input WorkspaceRuntimeImageReplacementInput, runtime WorkspaceRuntime, status string, operationErr error) error {
	operation.Status = status
	operation.ErrorCode = errorCode(operationErr)
	operation.Retryable = status == "started"
	if status != "started" {
		operation.FinishedAt = s.now()
	}
	fillOperationResource(&operation, runtime)
	if operation.RedactedProviderPayload == nil {
		operation.RedactedProviderPayload = map[string]any{}
	}
	operation.RedactedProviderPayload["replacement"] = input
	return s.runtimeOperations.SaveRuntime(ctx, operation)
}

func decodeWorkspaceRuntimeImageReplacementResult(operation FabricOperation, result *WorkspaceRuntimeImageReplacementResult) error {
	if result == nil || operation.Status != "succeeded" || !decodeOperationResource(operation, &result.Runtime) {
		return ErrRuntimeOperationFailed
	}
	result.SchemaVersion = 1
	result.OperationID = operation.OperationID
	result.WorkspaceID = operation.WorkspaceID
	result.RuntimeID = result.Runtime.ID
	result.Status = "succeeded"
	if raw, ok := operation.RedactedProviderPayload["replacement"]; ok {
		var input WorkspaceRuntimeImageReplacementInput
		if decodeStrictJSON(mustJSON(raw), &input) {
			result.PreviousImageDigest = input.PreviousImageDigest
			result.ReplacementImageDigest = input.ReplacementImageDigest
		}
	}
	return nil
}
