// Package store owns the workspace domain's PostgreSQL schema (`workspace`), its
// migrations, and the records this owner writes.
//
// The schema is owned by docs/spec/v2.26 (02_database_schema_complete.md and
// contracts/schema.sql). This package never reads or writes another owner's
// tables: there are no cross-database foreign keys, joins, or transactions.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"opl-cloud/services/internal/postgresmigrate"
	"opl-cloud/services/workspace/migrations"
)

// Schema is the PostgreSQL schema this owner writes.
const Schema = "workspace"

// Migration is one versioned, owner-local migration.
type Migration struct {
	Version string
	Query   string
}

// Store reads and writes the workspace owner's records.
type Store struct {
	db *sql.DB
}

// New returns a Store over an already-open connection to the owner's database.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB exposes the owner connection for transactional call sites in this package.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Ready reports whether the owner's database is reachable. A dependency that is
// not ready surfaces as not-ready; it is never reported as healthy.
func (s *Store) Ready(ctx context.Context) error {
	if s.db == nil {
		return errors.New("workspace store database is required")
	}
	return s.db.PingContext(ctx)
}

// Install applies the owner-local migrations. The DDL itself guards the target
// database name, so a misconfigured DATABASE_URL fails instead of mutating the
// wrong schema.
func (s *Store) Install(ctx context.Context) error {
	if s.db == nil {
		return errors.New("workspace store database is required")
	}
	migrations, err := EmbeddedMigrations()
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
	return postgresmigrate.Apply(ctx, s.db, Schema, applied)
}

// EmbeddedMigrations returns the owner's migrations in version order.
func EmbeddedMigrations() ([]Migration, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	ordered := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		ordered = append(ordered, Migration{
			Version: strings.TrimSuffix(entry.Name(), ".sql"),
			Query:   string(data),
		})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })
	return ordered, nil
}
