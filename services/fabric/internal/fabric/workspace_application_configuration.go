package fabric

import (
	"context"
	"errors"
	"fmt"
	contracts "opl-cloud/packages/contracts/go"
)

type applicationCredentialSourceContextKey struct{}
type applicationCredentialSource struct {
	RuntimeOperationID, Version, OperationKey string
}

func (s *Service) applicationCredentialSource(ctx context.Context, input WorkspaceApplicationRuntimeInput) (applicationCredentialSource, error) {
	result := applicationCredentialSource{RuntimeOperationID: input.Configuration.CredentialSourceRuntimeOperationID, Version: input.Configuration.CredentialVersion}
	if result.RuntimeOperationID == "" {
		return result, nil
	}
	owners, err := s.runtimeRead.operations.WorkspaceRuntimeIdentityCandidates(ctx, input.WorkspaceID)
	if err != nil {
		return result, err
	}
	var original WorkspaceRuntime
	if !contracts.WorkspaceApplicationCredentialKind(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey) || len(owners) != 1 || owners[0].AccountID != input.AccountID || !decodeOperationResource(owners[0], &original) || original.WorkspaceID != input.WorkspaceID || original.OperationID != result.RuntimeOperationID {
		return result, errors.New("workspace_application_legacy_credential_owner_missing")
	}
	result.OperationKey = original.OperationID
	version := original.Access.CredentialVersion
	operations, err := s.resourceOperations.List(ctx)
	if err != nil {
		return result, err
	}
	var latest FabricOperation
	for _, operation := range operations {
		if operation.Action != "update_workspace_runtime" || operation.ResourceKind != "workspace_runtime" || operation.ResourceID != input.WorkspaceID || operation.WorkspaceID != input.WorkspaceID || operation.AccountID != input.AccountID || operation.Status != "succeeded" {
			continue
		}
		var runtime WorkspaceRuntime
		if !decodeOperationResource(operation, &runtime) || runtime.ID != original.ID || runtime.WorkspaceID != original.WorkspaceID || runtime.OperationID != original.OperationID || runtime.Access.CredentialVersion == "" {
			return result, ErrLaunchStageBindingConflict
		}
		if latest.ID == "" || operation.CreatedAt.After(latest.CreatedAt) || operation.CreatedAt.Equal(latest.CreatedAt) && operation.ID > latest.ID {
			latest = operation
			version = runtime.Access.CredentialVersion
			result.OperationKey = operation.IdempotencyKey
		}
	}
	if result.Version == "" || result.Version != version || result.OperationKey == "" {
		return result, errors.New("workspace_application_legacy_credential_owner_missing")
	}
	return result, nil
}

func (s *Service) applicationCredentialContext(ctx context.Context, input WorkspaceApplicationRuntimeInput) (context.Context, error) {
	source, err := s.applicationCredentialSource(ctx, input)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, applicationCredentialSourceContextKey{}, source), nil
}

// workspaceApplicationRequiresDerivedCredentials reports whether the revision
// asks the installation to derive a credential for it. Deriving needs the
// installation seed and the credential version, so the provider resolves those
// only for an application that declared the requirement.
func workspaceApplicationRequiresDerivedCredentials(revision contracts.WorkspaceApplicationRevision) bool {
	return contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword) ||
		contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret)
}

func validateWorkspaceApplicationConfiguration(input WorkspaceApplicationRuntimeInput) error {
	return contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input)
}

// workspaceApplicationGatewayBinding resolves the Secret binding named by the
// revision's declared Gateway credential. The credential's name is the
// application's choice; the platform only requires that the binding points at the
// Workspace's own Gateway secret.
func workspaceApplicationGatewayBinding(input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeSecretBinding, error) {
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		return contracts.WorkspaceApplicationRuntimeSecretBinding{}, errors.New("workspace_application_credentials_unavailable")
	}
	for _, binding := range input.SecretBindings {
		if binding.Name == credential.Name && binding.Key == "opl_gateway_api_key" && binding.SecretRef == gatewaySecretName(input.WorkspaceID) {
			return binding, nil
		}
	}
	return contracts.WorkspaceApplicationRuntimeSecretBinding{}, fmt.Errorf("workspace_application_gateway_binding_required")
}
