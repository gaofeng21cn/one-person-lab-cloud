package clients

import "context"

type FabricWorkspaceRuntimeGatewayNetworkRecoveryClient interface {
	RecoverWorkspaceRuntimeGatewayNetwork(context.Context, WorkspaceRuntimeGatewayNetworkRecoveryInput, string) (WorkspaceRuntimeGatewayNetworkRecoveryResult, error)
}

type WorkspaceRuntimeGatewayNetworkRecoveryInput struct {
	AccountID          string `json:"accountId"`
	WorkspaceID        string `json:"workspaceId"`
	ComputeID          string `json:"computeId"`
	RuntimeID          string `json:"runtimeId"`
	RuntimeOperationID string `json:"runtimeOperationId"`
	RuntimeServiceName string `json:"runtimeServiceName"`
	IdempotencyKey     string `json:"-"`
}

type WorkspaceRuntimeGatewayNetworkRecoveryResult struct {
	SchemaVersion      int              `json:"schemaVersion"`
	OperationID        string           `json:"operationId"`
	AccountID          string           `json:"accountId"`
	WorkspaceID        string           `json:"workspaceId"`
	ComputeID          string           `json:"computeId"`
	RuntimeID          string           `json:"runtimeId"`
	RuntimeServiceName string           `json:"runtimeServiceName"`
	GatewayContainerID string           `json:"gatewayContainerId"`
	NetworkID          string           `json:"networkId"`
	NetworkName        string           `json:"networkName"`
	Status             string           `json:"status"`
	Runtime            WorkspaceRuntime `json:"runtime"`
}
