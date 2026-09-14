package clients

import (
	"context"
	"errors"
	"net/url"

	contracts "opl-cloud/packages/contracts/go"
)

// WorkspaceApplicationRuntimeInput drives one application runtime creation on
// Fabric from an admitted revision. Secret values never travel here; the
// configuration digest only names the operator-supplied configuration.
type WorkspaceApplicationRuntimeInput = contracts.WorkspaceApplicationRuntimeInput

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
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.WorkspaceID, Action: "create_workspace_application_runtime",
	}, &result)
	return result, err
}

func (c *fabricHTTPClient) ReadWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	var result contracts.WorkspaceApplicationRuntimeObservation
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/readback", input, input.RuntimeOperationID, fabricMutationScope{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.WorkspaceID, Action: "read_workspace_application_runtime",
	}, &result)
	return result, err
}

type WorkspaceApplicationRuntimeLifecycleInput = contracts.WorkspaceApplicationRuntimeLifecycleInput
type WorkspaceApplicationRuntimeLifecycleResult = contracts.WorkspaceApplicationRuntimeLifecycleResult
type WorkspaceApplicationRuntimeCredentials = contracts.WorkspaceApplicationRuntimeCredentials

type FabricWorkspaceApplicationRuntimeLifecycleClient interface {
	SetWorkspaceApplicationRuntimeLifecycle(context.Context, WorkspaceApplicationRuntimeLifecycleInput, string) (WorkspaceApplicationRuntimeLifecycleResult, error)
	ReadWorkspaceApplicationRuntimeLifecycle(context.Context, WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeLifecycleResult, error)
	ReadWorkspaceApplicationRuntimeCredentials(context.Context, WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeCredentials, error)
}

func (c *fabricHTTPClient) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput, idempotencyKey string) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	var result WorkspaceApplicationRuntimeLifecycleResult
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/lifecycle", input, idempotencyKey, fabricMutationScope{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.RuntimeID, Action: "set_workspace_application_runtime_lifecycle",
	}, &result)
	return result, err
}

func (c *fabricHTTPClient) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeLifecycleResult, error) {
	var result WorkspaceApplicationRuntimeLifecycleResult
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/lifecycle-readback", input, input.RuntimeOperationID, fabricMutationScope{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.RuntimeID, Action: "read_workspace_application_runtime_lifecycle",
	}, &result)
	return result, err
}

func (c *fabricHTTPClient) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input WorkspaceApplicationRuntimeLifecycleInput) (WorkspaceApplicationRuntimeCredentials, error) {
	var result WorkspaceApplicationRuntimeCredentials
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/credentials", input, input.RuntimeOperationID, fabricMutationScope{
		AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.RuntimeID, Action: "read_workspace_application_runtime_credentials",
	}, &result)
	return result, err
}

type FabricWorkspaceApplicationRuntimePreflightClient interface {
	PreflightWorkspaceApplicationRuntime(context.Context, WorkspaceApplicationRuntimeInput) error
}

func (c *fabricHTTPClient) PreflightWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	var result struct {
		Admitted bool `json:"admitted"`
	}
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/preflight", input, input.RuntimeOperationID, fabricMutationScope{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_application_runtime", ResourceID: input.WorkspaceID, Action: "preflight_workspace_application_runtime"}, &result)
	if err == nil && !result.Admitted {
		return errors.New("workspace_application_runtime_not_admitted")
	}
	return err
}

type WorkspaceApplicationGatewaySecretCleanupInput = contracts.WorkspaceApplicationGatewaySecretCleanupInput
type FabricWorkspaceApplicationGatewaySecretCleanupClient interface {
	RemoveWorkspaceApplicationGatewaySecret(context.Context, WorkspaceApplicationGatewaySecretCleanupInput, string) error
}

func (c *fabricHTTPClient) RemoveWorkspaceApplicationGatewaySecret(ctx context.Context, input WorkspaceApplicationGatewaySecretCleanupInput, key string) error {
	var result struct {
		State string `json:"state"`
	}
	err := c.postMutation(ctx, "/fabric/workspace-application-runtimes/"+url.PathEscape(input.WorkspaceID)+"/gateway-secret-cleanup", input, key, fabricMutationScope{AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "gateway_secret", ResourceID: input.SecretRef, Action: "remove_workspace_application_gateway_secret"}, &result)
	if err == nil && result.State != "absent" {
		return errors.New("workspace_application_gateway_secret_cleanup_pending")
	}
	return err
}
