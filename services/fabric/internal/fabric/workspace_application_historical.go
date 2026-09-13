package fabric

import (
	"context"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
)

type historicalWorkspaceApplicationRuntimeInput struct {
	AccountID             string                                 `json:"accountId"`
	WorkspaceID           string                                 `json:"workspaceId"`
	ComputeID             string                                 `json:"computeId"`
	VolumeID              string                                 `json:"volumeId"`
	AttachmentID          string                                 `json:"attachmentId"`
	AttachmentOperationID string                                 `json:"attachmentOperationId"`
	RuntimeOperationID    string                                 `json:"runtimeOperationId"`
	Revision              contracts.WorkspaceApplicationRevision `json:"revision"`
	ConfigurationDigest   string                                 `json:"configurationDigest"`
}

func historicalApplicationRequestHash(input WorkspaceApplicationRuntimeInput) string {
	return hashInput(historicalWorkspaceApplicationRuntimeInput{input.AccountID, input.WorkspaceID, input.ComputeID, input.VolumeID, input.AttachmentID, input.AttachmentOperationID, input.RuntimeOperationID, input.Revision, input.ConfigurationDigest})
}
func applicationRuntimeID(input WorkspaceApplicationRuntimeInput) string {
	if input.SchemaVersion == 0 {
		return contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID)
	}
	return workspaceApplicationRuntimeID(input.RuntimeOperationID)
}
func applicationRuntimeRequestHash(input WorkspaceApplicationRuntimeInput) string {
	if input.SchemaVersion == 0 {
		return historicalApplicationRequestHash(input)
	}
	return hashInput(input)
}
func validHistoricalApplicationInput(input WorkspaceApplicationRuntimeInput) bool {
	return input.SchemaVersion == 0 && input.AccountID != "" && input.WorkspaceID != "" && input.ComputeID != "" && input.VolumeID != "" && input.RuntimeOperationID != "" && input.ConfigurationDigest != "" && input.DataLayout == "" && input.DataSourceRuntimeOperationID == "" && input.DataBindingID == "" && len(input.Configuration.Environment) == 0 && input.Configuration.CredentialVersion == "" && input.Configuration.CredentialSourceRuntimeOperationID == "" && len(input.SecretBindings) == 0 && input.Revision.RuntimeProfile == "" && contracts.ValidateWorkspaceApplicationRevision(input.Revision) == nil
}
func (s *Service) historicalApplicationReadback(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	var observation contracts.WorkspaceApplicationRuntimeObservation
	if !validHistoricalApplicationInput(input) {
		return observation, ErrWorkspaceApplicationRuntimeInputInvalid
	}
	err := s.resourceLocks.WithPoolLock(ctx, workspaceRuntimeLockKey(input.WorkspaceID), func(ctx context.Context) error {
		operation, found, err := s.runtimeOperationQueries.OperationByResourceActionIdempotency(ctx, "workspace_application_runtime", applicationRuntimeID(input), "create_workspace_application_runtime", input.RuntimeOperationID)
		if err != nil {
			return err
		}
		var record workspaceApplicationRuntimeRecord
		if !found || operation.AccountID != input.AccountID || operation.WorkspaceID != input.WorkspaceID || operation.RequestHash != historicalApplicationRequestHash(input) || !decodeOperationResource(operation, &record) || record.RuntimeID != applicationRuntimeID(input) {
			return ErrRuntimeIdempotencyConflict
		}
		// Retained success does not establish current readiness. A current provider
		// read validates ownership before recording the exact historical input.
		observation, err = s.readWorkspaceApplicationRuntime(ctx, input)
		if err != nil {
			return err
		}
		record.Input = input
		record.Observation = observation
		adoption := newOperation("adopt_workspace_application_runtime", "workspace_application_runtime", record.RuntimeID, input.AccountID, input.WorkspaceID, input.RuntimeOperationID+":adopt", operation.RequestHash, s.now())
		adoption.ID = "fop_app_adopt_" + stableSuffix(input.RuntimeOperationID)
		adoption.Status = "succeeded"
		adoption.CreatedAt = s.now()
		adoption.FinishedAt = s.now()
		fillOperationResource(&adoption, record)
		stored, _, err := s.runtimeOperations.ClaimRuntime(ctx, adoption)
		if err != nil {
			return err
		}
		if stored.RequestHash != operation.RequestHash || stored.AccountID != input.AccountID || stored.WorkspaceID != input.WorkspaceID {
			return ErrRuntimeIdempotencyConflict
		}
		return nil
	})
	return observation, err
}
func localDockerApplicationComponentNameForInput(input WorkspaceApplicationRuntimeInput, component string) (string, error) {
	identity := input.RuntimeOperationID
	if input.SchemaVersion == 0 {
		identity = input.WorkspaceID
	}
	return localDockerApplicationComponentName(identity, component)
}
func applicationPersistentSubPath(input WorkspaceApplicationRuntimeInput, mount contracts.WorkspaceApplicationMount) string {
	if input.SchemaVersion == 0 || input.DataLayout == "legacy_application" {
		return "main/" + mount.Name
	}
	if input.DataLayout == "legacy_opl" {
		return mount.Name
	}
	return contracts.WorkspaceApplicationDataDirectory(input.DataBindingID) + "/" + mount.Name
}
func (s *Service) validateApplicationDataLayout(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	if _, err := s.applicationCredentialSource(ctx, input); err != nil {
		return err
	}
	if input.DataLayout == "" {
		return nil
	}
	if input.DataSourceRuntimeOperationID == "" {
		return errors.New("workspace_application_data_source_required")
	}
	if input.DataLayout == "legacy_application" {
		source, found, err := s.runtimeOperationQueries.OperationByResourceActionIdempotency(ctx, "workspace_application_runtime", contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID), "create_workspace_application_runtime", input.DataSourceRuntimeOperationID)
		if err != nil {
			return err
		}
		original, readErr := s.applicationCreationRecord(ctx, source)
		if !found || readErr != nil || source.AccountID != input.AccountID || !validHistoricalApplicationInput(original.Input) || source.RequestHash != historicalApplicationRequestHash(original.Input) || original.Input.WorkspaceID != input.WorkspaceID || original.Input.VolumeID != input.VolumeID || original.Input.Revision.ApplicationID != input.Revision.ApplicationID {
			return errors.New("workspace_application_legacy_data_owner_missing")
		}
		return nil
	}
	if input.DataLayout != "legacy_opl" || input.Revision.RuntimeProfile != "opl_app" {
		return errors.New("workspace_application_data_layout_invalid")
	}
	for _, mount := range input.Revision.PersistentMounts {
		if !(mount.Name == "data" && mount.MountPath == "/data" || mount.Name == "projects" && mount.MountPath == "/projects") {
			return errors.New("workspace_application_legacy_data_mount_invalid")
		}
	}
	owners, err := s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(ctx, input.WorkspaceID)
	if err != nil {
		return err
	}
	var original WorkspaceRuntime
	if len(owners) != 1 || owners[0].AccountID != input.AccountID || !decodeOperationResource(owners[0], &original) || original.WorkspaceID != input.WorkspaceID || original.ID == "" || original.OperationID != input.DataSourceRuntimeOperationID {
		return errors.New("workspace_application_legacy_data_owner_missing")
	}
	return nil
}

func (s *Service) applicationCreationRecord(ctx context.Context, creation FabricOperation) (workspaceApplicationRuntimeRecord, error) {
	var record workspaceApplicationRuntimeRecord
	if !decodeOperationResource(creation, &record) {
		return record, ErrRuntimeIdempotencyConflict
	}
	if record.Input.AccountID != "" {
		return record, nil
	}
	adoption, found, err := s.runtimeOperationQueries.OperationByResourceActionIdempotency(ctx, "workspace_application_runtime", creation.ResourceID, "adopt_workspace_application_runtime", creation.IdempotencyKey+":adopt")
	if err != nil {
		return record, err
	}
	if !found || adoption.Status != "succeeded" || adoption.AccountID != creation.AccountID || adoption.WorkspaceID != creation.WorkspaceID || adoption.RequestHash != creation.RequestHash || !decodeOperationResource(adoption, &record) || !validHistoricalApplicationInput(record.Input) || historicalApplicationRequestHash(record.Input) != creation.RequestHash {
		return record, ErrRuntimeIdempotencyConflict
	}
	return record, nil
}
