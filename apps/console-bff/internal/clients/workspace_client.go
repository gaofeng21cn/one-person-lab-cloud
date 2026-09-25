package clients

import api "opl-cloud/packages/contracts/go/api"

// WorkspaceClient reuses the owner's existing authenticated connection.
func (c *Clients) WorkspaceClient() api.WorkspaceProductServiceClient {
	return c.workspace
}
