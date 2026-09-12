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
