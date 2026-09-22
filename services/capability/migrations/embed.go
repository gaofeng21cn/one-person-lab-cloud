// Package migrations embeds the capability owner's versioned DDL.
//
// Each file is the verbatim `BEGIN DATABASE opl_capability ... END DATABASE
// opl_capability` block from docs/spec/v2.26/contracts/schema.sql. The block
// guards its own database name and is applied on its own connection, so the
// schema cannot be installed into a different owner's database.
package migrations

import "embed"

// Files holds the ordered .sql migration sources.
//
//go:embed *.sql
var Files embed.FS
