package clients

import (
	"context"
	"fmt"

	api "opl-cloud/packages/contracts/go/api"
)

// This file owns the BFF's typed Serve read calls that are not part of the
// composed delivery view. They are kept beside the shared Clients type rather
// than inside it so the Serve read surface has one owner and one place to
// review. Like every other BFF read, a value comes only from the owner that
// wrote it; an unconfigured owner is an error, never a substituted default.

// Deployment reads one Serve-owned deployment attempt for one Workspace.
func (c *Clients) Deployment(ctx context.Context, workspaceID, deploymentID string) (*api.Deployment, error) {
	if c.serve == nil {
		return nil, fmt.Errorf("serve: %w", ErrUpstreamUnconfigured)
	}
	return c.serve.GetDeployment(ctx, &api.GetDeploymentRpcRequest{Context: CallContext(ctx), WorkspaceId: workspaceID, DeploymentId: deploymentID})
}
