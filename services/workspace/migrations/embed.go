// Package migrations embeds and exposes this owner's versioned DDL.
//
// The SQL files are the owner-local schema: each begins with a guard on its own
// database name and is applied on its own connection, so the schema cannot be
// installed into a different owner's database.
package migrations

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
