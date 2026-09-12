package clients

import (
	"context"
	"net/url"

	contracts "opl-cloud/packages/contracts/go"
)

// WorkspaceApplicationRuntimeInput drives one application runtime creation on
// Fabric from an admitted revision. Secret values never travel here; the
// configuration digest only names the operator-supplied configuration.
type WorkspaceApplicationRuntimeInput struct {
	WorkspaceID           string                                 `json:"workspaceId"`
	ComputeID             string                                 `json:"computeId"`
	VolumeID              string                                 `json:"volumeId"`
	AttachmentID          string                                 `json:"attachmentId"`
	AttachmentOperationID string                                 `json:"attachmentOperationId"`
	RuntimeOperationID    string                                 `json:"runtimeOperationId"`
	Revision              contracts.WorkspaceApplicationRevision `json:"revision"`
	ConfigurationDigest   string                                 `json:"configurationDigest"`
}

// FabricWorkspaceApplicationRuntimeClient is the narrow port the Control
// Plane deployment driver consumes to materialise an application runtime on
// Fabric and to read back the authoritative component observation.
type FabricWorkspaceApplicationRuntimeClient interface {
	EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, idempotencyKey string) (contracts.WorkspaceApplicationRuntimeObservation, error)
	ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error)
}

func (c *fabricHTTPClient) EnsureWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, idempotencyKey string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	var result contracts.WorkspaceApplicationRuntimeObservation
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes", input, idempotencyKey, fabricMutationScope{
		WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.WorkspaceID, Action: "create_workspace_application_runtime",
	}, &result)
	return result, err
}

func (c *fabricHTTPClient) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	var result contracts.WorkspaceApplicationRuntimeObservation
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/readback", input, input.RuntimeOperationID, fabricMutationScope{
		WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.WorkspaceID, Action: "read_workspace_application_runtime",
	}, &result)
	return result, err
}
