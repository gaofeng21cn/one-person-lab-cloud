package controlplane

import (
	"context"
	"errors"
	"strings"

	"opl-cloud/services/control-plane/internal/clients"
)

func (s *Service) RecoverWorkspaceRuntimeGatewayNetwork(ctx context.Context, input clients.WorkspaceRuntimeGatewayNetworkRecoveryInput, idempotencyKey string) (clients.WorkspaceRuntimeGatewayNetworkRecoveryResult, error) {
	for _, value := range []string{input.AccountID, input.WorkspaceID, input.ComputeID, input.RuntimeID, input.RuntimeOperationID, input.RuntimeServiceName, idempotencyKey} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return clients.WorkspaceRuntimeGatewayNetworkRecoveryResult{}, errors.New("workspace_runtime_gateway_network_recovery_input_required")
		}
	}
	client, ok := s.fabric.(clients.FabricWorkspaceRuntimeGatewayNetworkRecoveryClient)
	if !ok {
		return clients.WorkspaceRuntimeGatewayNetworkRecoveryResult{}, errors.New("workspace_runtime_gateway_network_recovery_unavailable")
	}
	return client.RecoverWorkspaceRuntimeGatewayNetwork(ctx, input, idempotencyKey)
}
