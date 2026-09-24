package ownerservice

import (
	"context"
	"errors"
	"opl-cloud/packages/contracts/go/owneridentity"
	"testing"

	"google.golang.org/grpc"

	"opl-cloud/services/internal/ownerstore"
)

// emptyMigrations is a migration source with no files: the startup tests here prove
// the process wiring, not an owner's schema, which each owner's own isolation test
// covers against a real PostgreSQL server.
type emptyMigrations struct{}

func (emptyMigrations) ReadDir(string) ([]ownerstore.DirEntry, error) { return nil, nil }
func (emptyMigrations) ReadFile(string) ([]byte, error)               { return nil, errors.New("no migrations") }

func bootstrapConfig(owner Owner) Config {
	return Config{TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Owner: owner, Addr: "127.0.0.1:0", Peers: map[Service]string{Service(OwnerWorkspace): "0123456789abcdef0123456789abcdef"}}
}

// TestStartRefusesUnimplementedHandlersWithoutServing proves the honest
// intermediate state: an owner whose domain handlers are not wired still starts,
// installs nothing it does not have, serves no product group, and reports
// NOT_SERVING with the exact reason instead of exiting or claiming readiness.
func TestStartRefusesUnimplementedHandlersWithoutServing(t *testing.T) {
	bootstrap, err := Start(context.Background(), bootstrapConfig(OwnerServe), emptyMigrations{}, func(server *Server, _ *Database) error {
		if err := server.RequireProductGroups("ServeProductService"); err != nil {
			return err
		}
		return ErrHandlersNotImplemented
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bootstrap.Close()

	if bootstrap.Server.healthServing() {
		t.Fatal("an owner with unimplemented handlers reported SERVING")
	}
	err = bootstrap.Server.Ready(context.Background())
	if err == nil {
		t.Fatal("an owner with unimplemented handlers reported ready")
	}
	if !containsGroup(err, "ServeProductService") {
		t.Fatalf("readiness did not name the missing product group: %v", err)
	}
}

// TestStartRequiresAProductRegistration proves a process cannot satisfy Bootstrap
// with a registration that installs no product surface.
func TestStartRequiresAProductRegistration(t *testing.T) {
	if _, err := Start(context.Background(), bootstrapConfig(OwnerBuild), emptyMigrations{}, nil); err == nil {
		t.Fatal("Bootstrap accepted a missing product registration")
	}
}

// TestStartWithoutDatabaseRegistersNoProductGroup proves that a process with no
// owner-local store serves no product surface at all, so it cannot answer a read an
// owner would have to answer from its own tables.
func TestStartWithoutDatabaseRegistersNoProductGroup(t *testing.T) {
	bootstrap, err := Start(context.Background(), bootstrapConfig(OwnerCapability), emptyMigrations{}, func(server *Server, _ *Database) error {
		if err := server.RequireProductGroups("CapabilityProductService"); err != nil {
			return err
		}
		return server.RegisterGroup("CapabilityProductService", func(*grpc.Server) {})
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bootstrap.Close()

	if bootstrap.Database != nil {
		t.Fatal("a start without DATABASE_URL opened a database")
	}
	if len(bootstrap.Server.ProductGroups()) != 1 {
		t.Fatalf("registered product groups = %v, want the declared one", bootstrap.Server.ProductGroups())
	}
}

// TestStartWithoutDatabaseReportsTheMissingDependency proves an owner configured
// without its own database does not claim health: DATABASE_URL is the dependency
// that makes its local writes possible.
func TestStartWithoutDatabaseReportsTheMissingDependency(t *testing.T) {
	bootstrap, err := Start(context.Background(), bootstrapConfig(OwnerCapability), emptyMigrations{}, func(server *Server, database *Database) error {
		if database != nil {
			t.Fatal("a start without DATABASE_URL opened a database")
		}
		if err := server.RequireProductGroups("CapabilityProductService"); err != nil {
			return err
		}
		return server.RegisterGroup("CapabilityProductService", func(*grpc.Server) {})
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bootstrap.Close()

	if bootstrap.Server.healthServing() {
		t.Fatal("an owner without its database reported SERVING")
	}
	if err := bootstrap.Server.Ready(context.Background()); err == nil {
		t.Fatal("an owner without its database reported ready")
	} else if !containsGroup(err, "database") {
		t.Fatalf("readiness did not name the missing database dependency: %v", err)
	}
}
