package bff

import (
	"net/http"
	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
)

// NewWorkspaceHandler composes real Workspace clients with the process boundary.
func NewWorkspaceHandler(workspace api.WorkspaceProductServiceClient, identity IdentityReader) http.Handler {
	return httpapi.NewWorkspaceHandler(workspace, identity)
}
