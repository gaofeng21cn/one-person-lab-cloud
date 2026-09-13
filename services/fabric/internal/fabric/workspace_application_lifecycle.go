package fabric

import (
	"context"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
)

type WorkspaceApplicationRuntimeLifecycleInput = contracts.WorkspaceApplicationRuntimeLifecycleInput
type WorkspaceApplicationRuntimeLifecycleResult = contracts.WorkspaceApplicationRuntimeLifecycleResult

type workspaceApplicationLifecycleProvider interface {
	SetWorkspaceApplicationRuntimeLifecycle(context.Context, WorkspaceApplicationRuntimeInput, string) (WorkspaceApplicationRuntimeLifecycleResult, error)
	ReadWorkspaceApplicationRuntimeLifecycle(context.Context, WorkspaceApplicationRuntimeInput) (WorkspaceApplicationRuntimeLifecycleResult, error)
	ReadWorkspaceApplicationRuntimeCredentials(context.Context, WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error)
}

type workspaceApplicationLifecycleRecord struct {
	Input  WorkspaceApplicationRuntimeLifecycleInput  `json:"input"`
	Result WorkspaceApplicationRuntimeLifecycleResult `json:"result"`
}

func (s *Service) workspaceApplicationCreationInput(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeInput, error) {
	if input.LegacyRuntime || input.AccountID == "" || input.WorkspaceID == "" || input.RuntimeOperationID == "" || input.RuntimeID != applicationLifecycleRuntimeID(input) {
		return WorkspaceApplicationRuntimeInput{}, ErrRuntimeIdempotencyConflict
	}
	creation, found, err := s.runtimeOperationQueries.OperationByResourceActionIdempotency(ctx, "workspace_application_runtime", input.RuntimeID, "create_workspace_application_runtime", input.RuntimeOperationID)
	if err != nil {
		return WorkspaceApplicationRuntimeInput{}, err
	}
	if !found {
		return WorkspaceApplicationRuntimeInput{}, ErrWorkspaceLaunchResourceAbsent
	}
	record, readErr := s.applicationCreationRecord(ctx, creation)
	if readErr != nil {
		return WorkspaceApplicationRuntimeInput{}, readErr
	}
	if creation.AccountID != input.AccountID || creation.WorkspaceID != input.WorkspaceID || record.RuntimeID != input.RuntimeID || record.Input.AccountID != input.AccountID || record.Input.WorkspaceID != input.WorkspaceID || record.Input.RuntimeOperationID != input.RuntimeOperationID || creation.RequestHash != applicationRuntimeRequestHash(record.Input) {
		return WorkspaceApplicationRuntimeInput{}, ErrRuntimeIdempotencyConflict
	}
	if input.HistoricalApplicationRuntime {
		if !validHistoricalApplicationInput(record.Input) {
			return WorkspaceApplicationRuntimeInput{}, ErrRuntimeIdempotencyConflict
		}
	} else if err := validateWorkspaceApplicationConfiguration(record.Input); err != nil {
		return WorkspaceApplicationRuntimeInput{}, err
	}
	return record.Input, nil
}

func applicationLifecycleResult(observation contracts.WorkspaceApplicationRuntimeObservation) WorkspaceApplicationRuntimeLifecycleResult {
	state := observation.Status
	if state == "ready" {
		state = "running"
	}
	if state == "failed" {
		state = "pending"
	}
	return WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: observation.RuntimeID, WorkspaceID: observation.WorkspaceID, State: state, Observation: observation}
}

func (s *Service) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	return s.workspaceApplicationLifecycle(ctx, input, true)
}
func (s *Service) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	return s.workspaceApplicationLifecycle(ctx, input, false)
}
func (s *Service) workspaceApplicationLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput, mutate bool) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result := WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: "pending"}
	if input.DesiredState != "running" && input.DesiredState != "suspended" && input.DesiredState != "absent" {
		return result, ErrWorkspaceApplicationRuntimeInputInvalid
	}
	if mutate && input.IdempotencyKey == "" {
		return result, ErrWorkspaceApplicationRuntimeInputInvalid
	}
	err := s.resourceLocks.WithPoolLock(ctx, workspaceRuntimeLockKey(input.WorkspaceID), func(ctx context.Context) error {
		if input.LegacyRuntime {
			var err error
			result, err = s.workspaceApplicationLegacyLifecycle(ctx, input, mutate)
			return err
		}
		creation, err := s.workspaceApplicationCreationInput(ctx, input)
		if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
			result, err = s.fenceUncreatedApplicationRuntime(ctx, input, mutate)
			return err
		}
		if err != nil {
			return err
		}
		provider, ok := s.runtimeProvider.(workspaceApplicationLifecycleProvider)
		if !ok {
			return ErrWorkspaceApplicationRuntimeProviderUnsupported
		}
		latest, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_application_lifecycle", input.RuntimeID)
		if err != nil {
			return err
		}
		if found {
			var previous workspaceApplicationLifecycleRecord
			if !decodeOperationResource(latest, &previous) {
				return ErrRuntimeIdempotencyConflict
			}
			if mutate && previous.Input.DesiredState == "absent" && input.DesiredState != "absent" {
				return errors.New("workspace_application_runtime_retired")
			}
		}
		result, err = provider.ReadWorkspaceApplicationRuntimeLifecycle(ctx, creation)
		if err != nil {
			return err
		}
		if !mutate {
			return validateApplicationLifecycleResult(creation, result)
		}
		operation := newOperation("set_workspace_application_runtime_lifecycle", "workspace_application_lifecycle", input.RuntimeID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), s.now())
		operation.ID = "fop_app_lifecycle_" + stableSuffix(input.IdempotencyKey)
		operation.OperationID = input.IdempotencyKey
		operation.Status = "started"
		operation.CreatedAt = s.now()
		operation.RedactedProviderPayload = map[string]any{"resource": workspaceApplicationLifecycleRecord{Input: input, Result: result}}
		stored, claimed, err := s.runtimeOperations.ClaimRuntime(ctx, operation)
		if err != nil {
			return err
		}
		if stored.RequestHash != operation.RequestHash {
			return ErrRuntimeIdempotencyConflict
		}
		if !claimed && found && latest.IdempotencyKey != input.IdempotencyKey {
			return errors.New("workspace_application_lifecycle_superseded")
		}
		if !claimed && stored.Status == "succeeded" {
			if result.State != input.DesiredState && !(input.DesiredState == "suspended" && result.State == "absent") {
				return errors.New("workspace_application_lifecycle_readback_drift")
			}
			var previous workspaceApplicationLifecycleRecord
			if !decodeOperationResource(stored, &previous) {
				return ErrRuntimeIdempotencyConflict
			}
			result.ImageRetirement = previous.Result.ImageRetirement
			return nil
		}
		// A retry resumes the same desired-state command after exact live readback.
		// Deletion is repeated to finish deterministic image retirement after a lost response.
		if result.State != input.DesiredState || input.DesiredState == "absent" {
			mutationCtx := s.providerMutationContext(ctx, stored)
			if input.DesiredState == "absent" {
				references, referenceErr := s.retainedApplicationImages(ctx, input.RuntimeID)
				if referenceErr != nil {
					return referenceErr
				}
				mutationCtx = context.WithValue(mutationCtx, applicationRetainedImagesContextKey{}, references)
			}
			result, err = provider.SetWorkspaceApplicationRuntimeLifecycle(mutationCtx, creation, input.DesiredState)
		}
		validationErr := validateApplicationLifecycleResult(creation, result)
		err = errors.Join(err, validationErr)
		previous := stored
		stored.RedactedProviderPayload = map[string]any{"resource": workspaceApplicationLifecycleRecord{Input: input, Result: result}}
		if err != nil {
			stored.Status = "failed"
			stored.ErrorCode = errorCode(err)
		} else if result.State == input.DesiredState {
			stored.Status = "succeeded"
			stored.FinishedAt = s.now()
		}
		if previous.Status == "failed" {
			if stored.Status == "succeeded" {
				return s.runtimeOperations.ConvergeRuntimeReadback(ctx, previous, stored)
			}
			return err
		}
		return errors.Join(err, s.runtimeOperations.SaveRuntime(ctx, stored))
	})
	return result, err
}
func validateApplicationLifecycleResult(input WorkspaceApplicationRuntimeInput, result WorkspaceApplicationRuntimeLifecycleResult) error {
	if result.RuntimeID != applicationRuntimeID(input) || result.WorkspaceID != input.WorkspaceID {
		return ErrRuntimeIdempotencyConflict
	}
	if err := validateWorkspaceApplicationRuntimeObservation(input, &result.Observation); err != nil {
		return err
	}
	if applicationLifecycleResult(result.Observation).State != result.State {
		return ErrRuntimeIdempotencyConflict
	}
	return nil
}
func (s *Service) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	creation, err := s.workspaceApplicationCreationInput(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, err
	}
	provider, ok := s.runtimeProvider.(workspaceApplicationLifecycleProvider)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	live, err := provider.ReadWorkspaceApplicationRuntimeLifecycle(ctx, creation)
	if err != nil || live.State != "running" {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, firstNonNil(err, ErrWorkspaceLaunchPending)
	}
	return provider.ReadWorkspaceApplicationRuntimeCredentials(ctx, creation)
}

// A reserved CP deployment may be cancelled before Fabric receives its create.
// Persisting this exact generation's fence prevents a delayed create from
// reviving it after expiry/deletion. No provider resource is inferred here.
func (s *Service) fenceUncreatedApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput, mutate bool) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	result := WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: "absent", Observation: contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, Status: "absent", Components: []contracts.WorkspaceApplicationRuntimeComponentState{}}}
	if !mutate {
		return result, nil
	}
	if input.DesiredState == "running" {
		return result, ErrWorkspaceLaunchResourceAbsent
	}
	latest, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_application_lifecycle", input.RuntimeID)
	if err != nil {
		return result, err
	}
	if found {
		var previous workspaceApplicationLifecycleRecord
		if !decodeOperationResource(latest, &previous) || previous.Input.AccountID != input.AccountID || previous.Input.WorkspaceID != input.WorkspaceID {
			return result, ErrRuntimeIdempotencyConflict
		}
		if previous.Input.DesiredState == "absent" && input.DesiredState != "absent" {
			return result, errors.New("workspace_application_runtime_retired")
		}
	}
	operation := newOperation("set_workspace_application_runtime_lifecycle", "workspace_application_lifecycle", input.RuntimeID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), s.now())
	operation.ID = "fop_app_lifecycle_" + stableSuffix(input.IdempotencyKey)
	operation.OperationID = input.IdempotencyKey
	operation.Status = "succeeded"
	operation.CreatedAt = s.now()
	operation.FinishedAt = s.now()
	operation.RedactedProviderPayload = map[string]any{"resource": workspaceApplicationLifecycleRecord{Input: input, Result: result}}
	stored, _, err := s.runtimeOperations.ClaimRuntime(ctx, operation)
	if err != nil {
		return result, err
	}
	if stored.RequestHash != operation.RequestHash {
		return result, ErrRuntimeIdempotencyConflict
	}
	return result, nil
}

type applicationRetainedImagesContextKey struct{}

func (s *Service) retainedApplicationImages(ctx context.Context, retiringID string) (map[string]bool, error) {
	operations, err := s.resourceOperations.List(ctx)
	if err != nil {
		return nil, err
	}
	images := map[string]bool{}
	seen := map[string]bool{}
	for _, operation := range operations {
		if operation.Action != "create_workspace_application_runtime" || operation.ResourceID == retiringID || seen[operation.ResourceID] {
			continue
		}
		seen[operation.ResourceID] = true
		record, readErr := s.applicationCreationRecord(ctx, operation)
		if readErr != nil || record.Input.AccountID == "" {
			return nil, errors.New("workspace_application_image_reference_unresolved")
		}
		lifecycle, found, err := s.resourceOperations.LatestResourceOperation(ctx, "workspace_application_lifecycle", record.RuntimeID)
		if err != nil {
			return nil, err
		}
		if found {
			var previous workspaceApplicationLifecycleRecord
			if !decodeOperationResource(lifecycle, &previous) {
				return nil, ErrRuntimeIdempotencyConflict
			}
			if previous.Result.State == "absent" && previous.Input.DesiredState == "absent" {
				continue
			}
		}
		for _, component := range contracts.WorkspaceApplicationRuntimeComponents(record.Input.Revision) {
			images[component.Image] = true
		}
	}
	return images, nil
}

func applicationLifecycleRuntimeID(input WorkspaceApplicationRuntimeLifecycleInput) string {
	if input.HistoricalApplicationRuntime {
		return contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID)
	}
	return workspaceApplicationRuntimeID(input.RuntimeOperationID)
}
