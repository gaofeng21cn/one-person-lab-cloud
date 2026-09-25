package clients

import api "opl-cloud/packages/contracts/go/api"

// CatalogClient exposes the Cloud Resource Catalog surface the BFF reaches. The
// BFF owns none of these facts; the Resource Catalog owner reads and writes them.
func (c *Clients) CatalogClient() api.ResourceCatalogProductServiceClient {
	return c.catalog
}
