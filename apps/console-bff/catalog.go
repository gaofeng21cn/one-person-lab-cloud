// The Console BFF's Resource Catalog composition surface.
//
// This file is separate from bff.go so the catalog binding is added without
// touching the shared process wiring: it exposes the same authenticated handler
// and the same service-identity dialer the process uses, for an isolated
// cross-owner composition.
package bff

import (
	"net/http"

	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// NewCatalogHandler exposes the Resource Catalog routes over the BFF's
// authenticated boundary.
func NewCatalogHandler(catalog api.ResourceCatalogProductServiceClient, identity IdentityReader) http.Handler {
	return httpapi.NewCatalogHandler(catalog, identity)
}

// DialResourceCatalog opens the BFF's typed client to the Resource Catalog owner
// under the BFF service identity.
func DialResourceCatalog(address, token string, tls owneridentity.TLSConfig) (*clients.CatalogClient, error) {
	return clients.DialResourceCatalog(address, token, tls)
}
