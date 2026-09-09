package controlplane

import (
	"context"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

func (s *Service) ReadWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	client, ok := s.fabric.(clients.FabricWorkspaceRuntimePowerClient)
	if !ok {
		return contracts.WorkspaceRuntimePowerResult{}, errors.New("fabric_workspace_runtime_power_unavailable")
	}
	return client.ReadWorkspaceRuntimePower(ctx, input)
}

func (s *Service) SetWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	client, ok := s.fabric.(clients.FabricWorkspaceRuntimePowerClient)
	if !ok {
		return contracts.WorkspaceRuntimePowerResult{}, errors.New("fabric_workspace_runtime_power_unavailable")
	}
	return client.SetWorkspaceRuntimePower(ctx, input)
}
