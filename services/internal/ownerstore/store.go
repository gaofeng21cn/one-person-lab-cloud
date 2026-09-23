// Package ownerstore provides the PostgreSQL mechanics every Cloud domain owner
// repeats: an owner-local migration install, owner Operations, a transactional
// Outbox with per-consumer delivery, an idempotent Inbox, and command
// idempotency replay.
//
// It is deliberately policy-free: it knows a schema name and the shared column
// shapes, never a domain rule, price, provider fact, or state machine. Each
// owner supplies its own schema, its own migration files, and its own business
// writes; no owner can reach another owner's tables through this package
// because every statement is qualified with the one schema the caller owns.
package ownerstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"opl-cloud/services/internal/postgresmigrate"
)

// Migration is one versioned, owner-local migration.
type Migration struct {
	Version string
	Query   string
}

// MigrationSource lets a service hand its embedded migration files to Install
// without this package depending on that service's embed.FS.
type MigrationSource interface {
	ReadDir(name string) ([]DirEntry, error)
	ReadFile(name string) ([]byte, error)
}

// DirEntry is the subset of fs.DirEntry this package needs.
type DirEntry interface {
	Name() string
	IsDir() bool
}

var ownerSchemas = map[string]struct{}{
	"tenant":           {},
	"capability":       {},
	"build":            {},
	"workspace":        {},
	"runtime_control":  {},
	"serve":            {},
	"fabric":           {},
	"gateway":          {},
	"resource_catalog": {},
	"ledger":           {},
}

// Store reads and writes one owner's records in that owner's schema.
type Store struct {
	db     *sql.DB
	schema string
}

// New returns a Store over an already-open connection to the owner's database.
// The schema must be one of the fixed Cloud owner schemas; a caller cannot pass a
// free-form identifier into the generated SQL.
func New(db *sql.DB, schema string) (*Store, error) {
	schema = strings.TrimSpace(schema)
	if _, ok := ownerSchemas[schema]; !ok {
		return nil, fmt.Errorf("ownerstore: %q is not a Cloud owner schema", schema)
	}
	return &Store{db: db, schema: schema}, nil
}

// Schema is the owner schema this store writes.
func (s *Store) Schema() string { return s.schema }

// DB exposes the owner connection for transactional call sites in the owning
// service's own store package.
func (s *Store) DB() *sql.DB { return s.db }

// Ready reports whether the owner's database is reachable. A dependency that is
// not ready surfaces as not-ready; it is never reported as healthy.
func (s *Store) Ready(ctx context.Context) error {
	if s.db == nil {
		return errors.New("ownerstore: database is required")
	}
	return s.db.PingContext(ctx)
}

// Install applies the owner-local migrations in version order. The migration DDL
// itself guards the target database name, so a misconfigured DATABASE_URL fails
// instead of mutating the wrong schema.
func (s *Store) Install(ctx context.Context, source MigrationSource) error {
	migrations, err := ReadMigrations(source)
	if err != nil {
		return err
	}
	applied := make([]postgresmigrate.Migration, 0, len(migrations))
	for _, migration := range migrations {
		migration := migration
		applied = append(applied, postgresmigrate.Migration{
			Version: migration.Version,
			Run: func(ctx context.Context) error {
				_, err := s.db.ExecContext(ctx, migration.Query)
				return err
			},
		})
	}
	return postgresmigrate.Apply(ctx, s.db, s.schema, applied)
}

// ReadMigrations reads every *.sql file from a service's embedded migration set
// in version order.
func ReadMigrations(source MigrationSource) ([]Migration, error) {
	entries, err := source.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("ownerstore: read embedded migrations: %w", err)
	}
	ordered := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := source.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("ownerstore: read migration %s: %w", entry.Name(), err)
		}
		ordered = append(ordered, Migration{
			Version: strings.TrimSuffix(entry.Name(), ".sql"),
			Query:   string(data),
		})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })
	return ordered, nil
}
