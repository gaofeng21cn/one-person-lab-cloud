package clients

import "context"

// FabricWorkspaceRuntimeImageReplacementClient is the narrow Fabric
// capability used to replace the image of an already successful Workspace
// Runtime. It deliberately does not expose a generic Kubernetes patch API.
type FabricWorkspaceRuntimeImageReplacementClient interface {
	ReplaceWorkspaceRuntimeImage(context.Context, WorkspaceRuntimeImageReplacementInput, string) (WorkspaceRuntimeImageReplacementResult, error)
}

type WorkspaceRuntimeImageReplacementInput struct {
	LaunchOperationID      string `json:"launchOperationId"`
	AccountID              string `json:"accountId"`
	WorkspaceID            string `json:"workspaceId"`
	ComputeID              string `json:"computeId"`
	StorageID              string `json:"storageId"`
	AttachmentID           string `json:"attachmentId"`
	RuntimeID              string `json:"runtimeId"`
	RuntimeOperationID     string `json:"runtimeOperationId"`
	RuntimeServiceName     string `json:"runtimeServiceName"`
	PreviousImageDigest    string `json:"previousImageDigest"`
	ReplacementImageDigest string `json:"replacementImageDigest"`
	IdempotencyKey         string `json:"-"`
}

type WorkspaceRuntimeImageReplacementResult struct {
	SchemaVersion          int              `json:"schemaVersion"`
	OperationID            string           `json:"operationId"`
	WorkspaceID            string           `json:"workspaceId"`
	RuntimeID              string           `json:"runtimeId"`
	PreviousImageDigest    string           `json:"previousImageDigest"`
	ReplacementImageDigest string           `json:"replacementImageDigest"`
	Status                 string           `json:"status"`
	Runtime                WorkspaceRuntime `json:"runtime"`
}
