// Package migrations embeds this deployment unit's versioned DDL.
//
// Each file is the verbatim `BEGIN DATABASE ... END DATABASE` block from
// docs/spec/v2.26/contracts/schema.sql. The unit carries two data owners
// (CloudIdentity/`tenant` and Gateway Integration/`gateway`), so each block is
// applied on a connection to its own database: the block guards its own
// database name and cannot be installed into the other owner's database.
package migrations

import "embed"

// Files holds the ordered .sql migration sources.
//
//go:embed *.sql
var Files embed.FS

// The two owner-local blocks. They are separate files because the deployment
// unit owns two databases and two database roles, not one shared schema.
const (
	// TenantFile installs the CloudIdentity owner schema on `opl_tenant`.
	TenantFile = "0001_opl_tenant.sql"
	// GatewayFile installs the Gateway Integration owner schema on `opl_gateway`.
	GatewayFile = "0002_opl_gateway.sql"
)
