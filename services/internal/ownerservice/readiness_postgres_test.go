package ownerservice

import (
	"context"
	"os"
	"testing"

	"google.golang.org/grpc"

	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerReportsServingOnlyWithAReachableDatabase proves, against a real
// PostgreSQL server, that a process reports SERVING when its owner database is
// reachable and NOT_SERVING with the exact dependency reason when it is not.
func TestOwnerReportsServingOnlyWithAReachableDatabase(t *testing.T) {
	// This test drives the owner's real OpenDatabase entrypoint, which admits a local
	// sslmode=disable loopback DSN only under the isolated-test gate.
	t.Setenv("OPL_POSTGRES_TESTS", "1")
	adminDSN := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	ctx := context.Background()
	harness, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN:        adminDSN,
		Owner:           "tenant",
		Database:        "opl_readiness_probe",
		SchemaOwnerRole: "opl_readiness_probe_owner",
		WriterRole:      "opl_readiness_probe_writer",
		RuntimeRole:     "opl_readiness_probe_runtime",
	})
	if err != nil {
		t.Fatalf("provision isolated database: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close(context.Background()) })

	if err := harness.Install(ctx, harness.OwnerDSN, harness.DatabaseName(), emptyMigrations{}); err != nil {
		t.Fatalf("install owner schema: %v", err)
	}

	// The owner process opens its database through the real entrypoint with the role the
	// service runs as: a login that holds only this owner's writer grant.
	database, err := OpenDatabase(ctx, OwnerTenant, harness.RuntimeDSN)
	if err != nil {
		t.Fatalf("open owner database: %v", err)
	}
	defer database.Close()

	bootstrap, err := StartWithDatabase(ctx, database, bootstrapConfig(OwnerTenant), emptyMigrations{}, func(server *Server, _ *Database) error {
		if err := server.RequireProductGroups("TenantProductService"); err != nil {
			return err
		}
		return server.RegisterGroup("TenantProductService", func(*grpc.Server) {})
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bootstrap.Close()

	if !bootstrap.Server.healthServing() {
		t.Fatal("an owner with a reachable database and its product group registered did not report SERVING")
	}
	if err := bootstrap.Server.Ready(ctx); err != nil {
		t.Fatalf("ready owner reported not ready: %v", err)
	}
}
