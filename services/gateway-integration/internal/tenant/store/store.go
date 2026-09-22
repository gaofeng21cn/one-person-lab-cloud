// Package store owns the CloudIdentity domain's PostgreSQL schema (`tenant`),
// its migrations, and the records this owner writes.
//
// CloudIdentity is one of the two data owners this deployment unit serves. The
// schema is owned by docs/spec/v2.26 (02_database_schema_complete.md and
// contracts/schema.sql). This package never reads or writes another owner's
// tables: there are no cross-database foreign keys, joins, or transactions, and
// it never shares a pool with the gateway store.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/postgresmigrate"
)

// Schema is the PostgreSQL schema this owner writes.
const Schema = "tenant"

// Migration is one versioned, owner-local migration.
type Migration struct {
	Version string
	Query   string
}

// Store reads and writes the CloudIdentity owner's records.
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
		return errors.New("tenant store database is required")
	}
	return s.db.PingContext(ctx)
}

// Install applies the owner-local migration. The DDL itself guards the target
// database name, so a connection to the wrong database fails instead of mutating
// another owner's schema.
func (s *Store) Install(ctx context.Context) error {
	if s.db == nil {
		return errors.New("tenant store database is required")
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

// EmbeddedMigrations returns this owner's migration: the embedded block that
// guards the `opl_tenant` database. The other owner's block is deliberately not
// applied here.
func EmbeddedMigrations() ([]Migration, error) {
	data, err := migrations.Files.ReadFile(migrations.TenantFile)
	if err != nil {
		return nil, fmt.Errorf("read embedded migration %s: %w", migrations.TenantFile, err)
	}
	return []Migration{{
		Version: strings.TrimSuffix(migrations.TenantFile, ".sql"),
		Query:   string(data),
	}}, nil
}
