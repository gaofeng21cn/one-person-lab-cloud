package ownerservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/postgresmigrate"
)

// Database is this owner's own PostgreSQL connection plus the migration entry
// point for its own schema. It is the only place an owner opens its database, so
// every owner admits the same DSN, installs its own migrations, and reports the
// same readiness reason.
type Database struct {
	owner Owner
	db    *sql.DB
	store *ownerstore.Store
}

// OpenDatabase admits and opens the owner's database. An empty DSN is refused
// here: a domain owner that cannot read its own state is not ready, and silently
// continuing without persistence would report SERVING while owning no facts.
func OpenDatabase(ctx context.Context, owner Owner, databaseURL string) (*Database, error) {
	if !owner.Valid() {
		return nil, fmt.Errorf("%q is not a Cloud owner", owner)
	}
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, fmt.Errorf("%s: DATABASE_URL is required for this owner's persistence", owner)
	}
	if err := postgresmigrate.AdmitDatabaseURL(Getenv, databaseURL); err != nil {
		return nil, fmt.Errorf("%s: %w", owner, err)
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("%s: open database: %w", owner, err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: connect to database: %w", owner, err)
	}
	store, err := ownerstore.New(db, owner.String())
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Database{owner: owner, db: db, store: store}, nil
}

// Migrate installs this owner's own migrations. The DDL guards its own database
// name and is run through the shared owner migration journal, so a misconfigured
// DSN fails instead of mutating another owner's database.
func (d *Database) Migrate(ctx context.Context, source ownerstore.MigrationSource) error {
	if d == nil || d.store == nil {
		return errors.New("owner database is not open")
	}
	if source == nil {
		return fmt.Errorf("%s: migration source is required", d.owner)
	}
	return d.store.Install(ctx, source)
}

// Ready reports whether the owner's database answers. The check is a real query,
// not a cached connection state.
func (d *Database) Ready(ctx context.Context) error {
	if d == nil || d.db == nil {
		return errors.New("owner database is not open")
	}
	return d.db.PingContext(ctx)
}

// Store exposes the owner-local store for this owner's own business writes.
func (d *Database) Store() *ownerstore.Store { return d.store }

// DB exposes the owner connection for owner-local transactional call sites.
func (d *Database) DB() *sql.DB { return d.db }

// Close releases the owner connection.
func (d *Database) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// RequireDatabaseReadiness registers this owner's database as a readiness
// dependency, so MarkServing refuses to report SERVING while it is unreachable.
func (s *Server) RequireDatabaseReadiness(database *Database) error {
	if database == nil {
		return errors.New("owner database is required")
	}
	return s.AddReadinessCheck("database", database.Ready)
}
