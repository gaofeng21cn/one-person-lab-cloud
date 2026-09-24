// Package migrations embeds the workspace owner's versioned DDL.
//
// The file is the verbatim `BEGIN DATABASE opl_workspace ...` block from the
// accepted schema. The block guards its own database name and is applied on its own
// connection, so the schema cannot be installed into a different owner's database.
package migrations

import "embed"

// Files holds the ordered .sql migration sources.
//
//go:embed *.sql
var Files embed.FS
