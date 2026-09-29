// Package gatewaymigrations embeds and exposes the Gateway Integration module's
// second data owner: the `gateway` schema in its own `opl_gateway` database.
//
// CloudIdentity (`tenant`) and Gateway Integration (`gateway`) are two distinct
// data/writer owners that share one deployment unit. They must not share a
// database, so this owner keeps its own embed.FS: installing the tenant database
// never attempts to create the gateway schema and vice versa. Like the tenant
// owner's migrations, this SQL guards its own database name and is applied on its
// own connection.
package gatewaymigrations

import (
	"embed"
	"io/fs"

	"opl-cloud/services/internal/ownerstore"
)

//go:embed *.sql
var files embed.FS

// Source returns this owner's embedded migration files in version order.
func Source() (ownerstore.MigrationSource, error) {
	root, err := fs.Sub(files, ".")
	if err != nil {
		return nil, err
	}
	return ownerstore.SourceFromFS(root)
}
