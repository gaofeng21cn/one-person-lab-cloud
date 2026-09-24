package ownerservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"opl-cloud/services/internal/ownerstore"
)

// ErrHandlersNotImplemented reports that this owner's domain service groups are
// declared by the contract but no handler is wired yet. It is a truthful
// intermediate state: the process serves its own migrations, database and
// Operation readback and reports NOT_SERVING naming the missing groups, instead of
// exiting or claiming readiness.
var ErrHandlersNotImplemented = errors.New("owner product handlers are not implemented")

// Bootstrap is one owner process's startup result. It is the only startup path a
// Cloud owner uses, so every owner opens its own database, installs its own
// migrations, registers the shared Operation readback group, and reports the same
// readiness reason when a dependency is missing.
type Bootstrap struct {
	Server   *Server
	Database *Database
	// Migrated reports whether the owner's own migrations ran in this process.
	Migrated bool
}

// Start opens the owner's dependencies, installs its migrations, registers the
// owner's own service groups, and marks the process SERVING only when readiness is
// provable.
//
// `register` is the owner's own product registration: it receives the server and
// the owner-local store, and returns an error instead of silently registering
// nothing. When DATABASE_URL is absent the process still starts — a developer may
// run an owner without persistence — but it is reported NOT_SERVING with the exact
// missing dependency rather than claiming health it does not have.
func Start(ctx context.Context, config Config, source ownerstore.MigrationSource, register func(*Server, *Database) error) (*Bootstrap, error) {
	return StartWithDatabase(ctx, nil, config, source, register)
}

// StartWithDatabase is Start over an already-opened owner database. Tests use it to
// exercise the ready path against a real isolated PostgreSQL server; production
// startup passes nil and lets Start open the database from DATABASE_URL.
func StartWithDatabase(ctx context.Context, database *Database, config Config, source ownerstore.MigrationSource, register func(*Server, *Database) error) (*Bootstrap, error) {
	if !config.Owner.Valid() {
		return nil, fmt.Errorf("%q is not a Cloud owner", config.Owner)
	}
	if register == nil {
		return nil, fmt.Errorf("%s: product registration is required", config.Owner)
	}
	server, err := NewServer(config)
	if err != nil {
		return nil, err
	}
	bootstrap := &Bootstrap{Server: server}

	if database != nil {
		if err := database.Migrate(ctx, source); err != nil {
			return nil, err
		}
		bootstrap.Database = database
		bootstrap.Migrated = true
		if err := server.RequireDatabaseReadiness(database); err != nil {
			return nil, err
		}
		registerOperations(server, config.Owner, bootstrap)
		if err := register(server, bootstrap.Database); err != nil {
			return nil, err
		}
		return bootstrap.finish(ctx)
	}

	databaseURL := strings.TrimSpace(config.DatabaseURL)
	if databaseURL == "" {
		if err := server.AddReadinessCheck("database", func(context.Context) error {
			return errors.New("DATABASE_URL is not configured for this owner")
		}); err != nil {
			return nil, err
		}
	} else {
		database, err := OpenDatabase(ctx, config.Owner, databaseURL)
		if err != nil {
			return nil, err
		}
		bootstrap.Database = database
		if err := database.Migrate(ctx, source); err != nil {
			_ = database.Close()
			return nil, err
		}
		bootstrap.Migrated = true
		if err := server.RequireDatabaseReadiness(database); err != nil {
			_ = database.Close()
			return nil, err
		}
	}

	// The Operation readback group is infrastructure every owner serves over its own
	// store. It does not by itself satisfy a declared product group.
	registerOperations(server, config.Owner, bootstrap)
	// Without persistence this process registers no product group and therefore
	// stays NOT_SERVING: it has no owner-local Operation record to read, and an
	// empty answer would misrepresent a missing writer as a missing record.

	if err := register(server, bootstrap.Database); err != nil {
		if !errors.Is(err, ErrHandlersNotImplemented) {
			_ = bootstrap.Close()
			return nil, err
		}
		if err := server.AddReadinessCheck("product_handlers", func(context.Context) error { return err }); err != nil {
			_ = bootstrap.Close()
			return nil, err
		}
	}
	return bootstrap.finish(ctx)
}

// registerOperations installs the shared Operation readback group when this process
// has the owner-local store that answers it.
func registerOperations(server *Server, owner Owner, bootstrap *Bootstrap) error {
	if bootstrap.Database == nil {
		return nil
	}
	operations, err := NewOperations(owner, bootstrap.Database.Store())
	if err != nil {
		return err
	}
	return operations.Register(server)
}

// finish reports SERVING only when readiness is provable. A process that is not
// ready keeps answering probes with the truthful NOT_SERVING reason instead of
// exiting while its dependency recovers.
func (b *Bootstrap) finish(ctx context.Context) (*Bootstrap, error) {
	if err := b.Server.MarkServing(ctx); err != nil {
		log.Printf("%s started without readiness: %v", b.Server.config.Owner, err)
	}
	return b, nil
}

// Close releases the owner's dependencies.
func (b *Bootstrap) Close() error {
	if b == nil || b.Database == nil {
		return nil
	}
	return b.Database.Close()
}
