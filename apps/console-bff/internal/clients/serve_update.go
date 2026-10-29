package clients

import (
	"context"
	"fmt"
	api "opl-cloud/packages/contracts/go/api"
)

// UpdateWorkspaceVersion forwards the caller's authorized, idempotent command
// unchanged to Serve, the only owner of the current application.
func (c *Clients) UpdateWorkspaceVersion(ctx context.Context, workspaceID string, body *api.UpdateWorkspaceVersionRequest) (*api.Operation, error) {
	if c.serve == nil {
		return nil, fmt.Errorf("serve: %w", ErrUpstreamUnconfigured)
	}
	return c.serve.UpdateWorkspaceVersion(ctx, &api.UpdateWorkspaceVersionRpcRequest{Context: CallContext(ctx), WorkspaceId: workspaceID, Body: body})
}
