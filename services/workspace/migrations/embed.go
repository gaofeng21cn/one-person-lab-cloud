// Package migrations embeds the workspace owner's versioned DDL.
//
// Each file is the verbatim `BEGIN DATABASE opl_workspace ... END DATABASE
// opl_workspace` block from docs/spec/v2.26/contracts/schema.sql. The block
// guards its own database name and is applied on its own connection, so the
// schema cannot be installed into a different owner's database.
package migrations

import "embed"

// Files holds the ordered .sql migration sources.
//
//go:embed *.sql
var Files embed.FS
