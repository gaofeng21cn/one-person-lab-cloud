// Package clients: the Resource Catalog binding.
//
// This is the BFF's own typed client to the Resource Catalog owner. It is a
// separate file so the shared client set stays owned by the process wiring: the
// catalog is reached by the same service identity convention as every other
// owner, and a missing address is reported instead of substituted.
package clients

import (
	"fmt"
	"strings"

	"google.golang.org/grpc"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// ResourceCatalogAddressEnv and ResourceCatalogTokenEnv configure the Resource
// Catalog owner the BFF reaches for approved plans and policy versions.
const (
	ResourceCatalogAddressEnv = "OPL_RESOURCE_CATALOG_URL"
	ResourceCatalogTokenEnv   = "OPL_RESOURCE_CATALOG_TOKEN"
)

// CatalogClient is the BFF's typed Resource Catalog connection. It owns its own
// connection because the catalog is not part of the read-only owner set the
// browser delivery view composes.
type CatalogClient struct {
	api.ResourceCatalogProductServiceClient
	conn *grpc.ClientConn
}

// DialResourceCatalog opens the BFF's typed connection to the Resource Catalog
// owner. An empty address is refused: the BFF reports an unconfigured owner
// rather than inventing catalog facts.
func DialResourceCatalog(address, token string, tls owneridentity.TLSConfig) (*CatalogClient, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("resource catalog: %w", ErrUpstreamUnconfigured)
	}
	options, err := tls.DialOptions(owneridentity.ConsoleBFF, owneridentity.ResourceCatalog.Service(), token)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		return nil, fmt.Errorf("dial resource_catalog at %s: %w", address, err)
	}
	return &CatalogClient{ResourceCatalogProductServiceClient: api.NewResourceCatalogProductServiceClient(conn), conn: conn}, nil
}

// Close releases the Resource Catalog connection.
func (c *CatalogClient) Close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}
