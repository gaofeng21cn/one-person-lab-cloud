// The Console BFF's public Resource Catalog composition surface.
//
// The process reaches the catalog through the shared client set, so this file
// carries no second dial path. It exposes only the authenticated handler for an
// isolated cross-owner composition, which a caller outside apps/console-bff
// cannot reach directly because the handler lives in an internal package. It
// mirrors the existing publisher pass-through for the same reason.
package bff

import (
	"net/http"

	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
)

// NewCatalogHandler exposes the Resource Catalog routes over the BFF's
// authenticated boundary.
func NewCatalogHandler(catalog api.ResourceCatalogProductServiceClient, identity IdentityReader) http.Handler {
	return httpapi.NewCatalogHandler(catalog, identity)
}
