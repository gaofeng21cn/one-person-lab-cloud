package catalog

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/runtime-control/migrations"
)

// runtimeControlHarness is one isolated runtime_control database installed
// through the owner's own migration entrypoint.
type runtimeControlHarness struct {
	DB *sql.DB
}

func newRuntimeControlHarness(t *testing.T) *runtimeControlHarness {
	t.Helper()
	ctx := context.Background()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip), Owner: "runtime_control", Database: "opl_runtime_control", SchemaOwnerRole: "opl_runtime_control_owner", WriterRole: "opl_runtime_control_writer", RuntimeRole: "opl_runtime_control_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &runtimeControlHarness{DB: db}
}
