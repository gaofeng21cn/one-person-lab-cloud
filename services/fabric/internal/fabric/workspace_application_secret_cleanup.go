package fabric

import (
	"context"
	"errors"
	contracts "opl-cloud/packages/contracts/go"
	"os"
)

type WorkspaceApplicationGatewaySecretCleanupInput = contracts.WorkspaceApplicationGatewaySecretCleanupInput
type workspaceApplicationSecretCleanupProvider interface {
	RemoveWorkspaceApplicationGatewaySecret(context.Context, WorkspaceApplicationGatewaySecretCleanupInput) error
}

func (s *Service) RemoveWorkspaceApplicationGatewaySecret(ctx context.Context, input WorkspaceApplicationGatewaySecretCleanupInput) error {
	if input.AccountID == "" || input.WorkspaceID == "" || input.SecretRef != gatewaySecretName(input.WorkspaceID) || input.IdempotencyKey == "" {
		return ErrWorkspaceApplicationRuntimeInputInvalid
	}
	return s.resourceLocks.WithPoolLock(ctx, workspaceRuntimeLockKey(input.WorkspaceID), func(ctx context.Context) error {
		provider, ok := s.runtimeProvider.(workspaceApplicationSecretCleanupProvider)
		if !ok {
			return ErrWorkspaceApplicationRuntimeProviderUnsupported
		}
		operations, err := s.resourceOperations.List(ctx)
		if err != nil {
			return err
		}
		// Historical generations shared one physical RuntimeID. Each retained
		// creation still needs its own validated input; another source's adoption
		// or List ordering cannot establish that this source left no component.
		seen := map[[2]string]bool{}
		for _, operation := range operations {
			source := [2]string{operation.ResourceID, operation.IdempotencyKey}
			if operation.WorkspaceID != input.WorkspaceID || operation.Action != "create_workspace_application_runtime" || seen[source] {
				continue
			}
			record, readErr := s.applicationCreationRecord(ctx, operation)
			if readErr != nil || operation.AccountID != input.AccountID || record.Input.AccountID != input.AccountID {
				return ErrRuntimeIdempotencyConflict
			}
			seen[source] = true
			runtimeProvider, ok := s.runtimeProvider.(workspaceApplicationLifecycleProvider)
			if !ok {
				return ErrWorkspaceApplicationRuntimeProviderUnsupported
			}
			live, err := runtimeProvider.ReadWorkspaceApplicationRuntimeLifecycle(ctx, record.Input)
			if err != nil {
				return err
			}
			if live.State != "absent" {
				return errors.New("workspace_application_secret_still_bound")
			}
		}
		owners, err := s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(ctx, input.WorkspaceID)
		if err != nil {
			return err
		}
		for _, owner := range owners {
			var runtime WorkspaceRuntime
			if owner.AccountID != input.AccountID || !decodeOperationResource(owner, &runtime) {
				return ErrRuntimeIdempotencyConflict
			}
			legacyProvider, ok := s.runtimeProvider.(workspaceApplicationLegacyProvider)
			if !ok {
				return ErrWorkspaceApplicationRuntimeProviderUnsupported
			}
			live, err := legacyProvider.WorkspaceApplicationLegacyLifecycle(ctx, WorkspaceApplicationRuntimeLifecycleInput{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, RuntimeID: runtime.ID, RuntimeOperationID: runtime.OperationID, LegacyRuntime: true, DesiredState: "absent"}, runtime, false)
			if err != nil {
				return err
			}
			if live.State != "absent" {
				return errors.New("workspace_application_secret_still_bound")
			}
		}
		operation := newOperation("remove_workspace_application_gateway_secret", "workspace_application_secret_cleanup", input.SecretRef, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), s.now())
		operation.ID = "fop_app_secret_cleanup_" + stableSuffix(input.IdempotencyKey)
		operation.Status = "started"
		operation.CreatedAt = s.now()
		operation.OperationID = input.IdempotencyKey
		operation.RedactedProviderPayload = map[string]any{"resource": input}
		stored, _, err := s.runtimeOperations.ClaimRuntime(ctx, operation)
		if err != nil {
			return err
		}
		if stored.RequestHash != operation.RequestHash {
			return ErrRuntimeIdempotencyConflict
		}
		previous := stored
		err = provider.RemoveWorkspaceApplicationGatewaySecret(s.providerMutationContext(ctx, stored), input)
		if err != nil {
			stored.Status = "failed"
			stored.ErrorCode = errorCode(err)
		} else {
			stored.Status = "succeeded"
			stored.FinishedAt = s.now()
		}
		if previous.Status == "succeeded" {
			return err
		}
		if previous.Status == "failed" {
			if stored.Status == "succeeded" {
				return s.runtimeOperations.ConvergeRuntimeReadback(ctx, previous, stored)
			}
			return err
		}
		return errors.Join(err, s.runtimeOperations.SaveRuntime(ctx, stored))
	})
}
func (p *LocalDockerProvider) RemoveWorkspaceApplicationGatewaySecret(ctx context.Context, input WorkspaceApplicationGatewaySecretCleanupInput) error {
	_, metadata, err := p.readGatewaySecretFiles(input.SecretRef)
	if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	if metadata.AccountID != input.AccountID || metadata.WorkspaceID != input.WorkspaceID {
		return ErrLaunchStageBindingConflict
	}
	root, err := p.openGatewaySecretRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	directory, err := root.Open(input.SecretRef + "/" + localDockerGatewayVersionsDir)
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil || closeErr != nil {
		return firstNonNil(err, closeErr)
	}
	for _, entry := range entries {
		_, version, err := readLocalDockerGatewayVersion(root, input.SecretRef, entry.Name())
		if err != nil {
			return err
		}
		if version.AccountID != input.AccountID || version.WorkspaceID != input.WorkspaceID || version.SecretRef != input.SecretRef {
			return ErrLaunchStageBindingConflict
		}
	}
	if err := p.RemoveGatewaySecret(ctx, input.WorkspaceID); err != nil {
		return err
	}
	if _, err := root.Lstat(input.SecretRef); !errors.Is(err, os.ErrNotExist) {
		return errors.New("workspace_application_gateway_secret_cleanup_pending")
	}
	return nil
}
func (p *TencentProvider) RemoveWorkspaceApplicationGatewaySecret(ctx context.Context, input WorkspaceApplicationGatewaySecretCleanupInput) error {
	secret, err := p.workspaceGatewaySecretIdentity(ctx, input.WorkspaceID)
	if errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		return nil
	}
	if err != nil {
		return err
	}
	if secret.AccountID != input.AccountID || secret.SecretRef != input.SecretRef {
		return ErrLaunchStageBindingConflict
	}
	return p.destroyGatewaySecret(ctx, secret, input.IdempotencyKey)
}
