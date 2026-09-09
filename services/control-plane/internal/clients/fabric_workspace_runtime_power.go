package clients

import (
	"context"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
)

type FabricWorkspaceRuntimePowerClient interface {
	ReadWorkspaceRuntimePower(context.Context, contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error)
	SetWorkspaceRuntimePower(context.Context, contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error)
}

func (c *fabricHTTPClient) ReadWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	var result contracts.WorkspaceRuntimePowerResult
	err := c.post(ctx, "/fabric/workspace-runtimes/power/read", input, "", &result)
	return result, err
}

func (c *fabricHTTPClient) SetWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	if input.IdempotencyKey == "" {
		return contracts.WorkspaceRuntimePowerResult{}, errors.New("workspace runtime power idempotency key is required")
	}
	var result contracts.WorkspaceRuntimePowerResult
	err := c.postMutation(ctx, "/fabric/workspace-runtimes/power", input, input.IdempotencyKey, fabricMutationScope{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_runtime_power", ResourceID: input.WorkspaceID, Action: "set_workspace_runtime_power",
	}, &result)
	return result, err
}
