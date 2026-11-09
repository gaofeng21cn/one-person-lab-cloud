package bff

import (
	"net/http"

	"opl-cloud/apps/console-bff/internal/httpapi"
)

// AgentReadOwner is the production BFF's typed owner read surface. Optional
// typed CatalogClient/WorkspaceClient providers are discovered by NewServer.
type AgentReadOwner = httpapi.OwnerReader

// NewAgentReadHandler exposes the production composition for isolated carrier
// qualification using explicit owner transports and the real identity boundary.
func NewAgentReadHandler(reader AgentReadOwner, identity IdentityReader) http.Handler {
	return httpapi.NewServer(reader, identity).Handler()
}
