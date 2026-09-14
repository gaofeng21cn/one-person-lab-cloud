package controlplane

import (
	"context"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// EnsureWorkspaceApplicationRuntime drives the Fabric creation of one
// application runtime through the narrow application-runtime client. The
// capability is refused when the configured Fabric client does not implement
// it yet.
func (s *Service) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, idempotencyKey string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if idempotencyKey == "" {
		return contracts.WorkspaceApplicationRuntimeObservation{}, errors.New("workspace_application_runtime_idempotency_key_required")
	}
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationRuntimeClient)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeObservation{}, errors.New("workspace_application_runtime_client_unavailable")
	}
	return client.EnsureWorkspaceApplicationRuntime(ctx, input, idempotencyKey)
}

// ReadWorkspaceApplicationRuntime returns the authoritative component
// observation of one application runtime.
func (s *Service) ReadWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationRuntimeClient)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeObservation{}, errors.New("workspace_application_runtime_client_unavailable")
	}
	return client.ReadWorkspaceApplicationRuntime(ctx, input)
}

func (s *Service) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput, idempotencyKey string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationRuntimeLifecycleClient)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("workspace_application_runtime_lifecycle_client_unavailable")
	}
	if idempotencyKey == "" {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("workspace_application_runtime_idempotency_key_required")
	}
	return client.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, idempotencyKey)
}

func (s *Service) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationRuntimeLifecycleClient)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("workspace_application_runtime_lifecycle_client_unavailable")
	}
	return client.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
}

func (s *Service) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationRuntimeLifecycleClient)
	if !ok {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, errors.New("workspace_application_runtime_credentials_client_unavailable")
	}
	return client.ReadWorkspaceApplicationRuntimeCredentials(ctx, input)
}

func (s *Service) PreflightWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) error {
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationRuntimePreflightClient)
	if !ok {
		return errors.New("workspace_application_runtime_preflight_client_unavailable")
	}
	return client.PreflightWorkspaceApplicationRuntime(ctx, input)
}

func (s *Service) RemoveWorkspaceApplicationGatewaySecret(ctx context.Context, input clients.WorkspaceApplicationGatewaySecretCleanupInput, key string) error {
	client, ok := s.fabric.(clients.FabricWorkspaceApplicationGatewaySecretCleanupClient)
	if !ok {
		return errors.New("workspace_application_gateway_secret_cleanup_client_unavailable")
	}
	return client.RemoveWorkspaceApplicationGatewaySecret(ctx, input, key)
}
