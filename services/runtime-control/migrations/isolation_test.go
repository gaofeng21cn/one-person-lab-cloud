package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/runtime-control/migrations"
)

// TestOwnerDatabaseIsolation proves the runtime_control owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "runtime_control",
		Database:        "opl_runtime_control",
		SchemaOwnerRole: "opl_runtime_control_owner",
		WriterRole:      "opl_runtime_control_writer",
		RuntimeRole:     "opl_runtime_control_runtime",
		Source:          source,
		OperationKind:   "reconcile",
		Tables:          []string{"runtime_releases", "catalog_policies", "operations", "idempotency_records", "outbox_events", "outbox_deliveries", "inbox_events"},
	})
}
