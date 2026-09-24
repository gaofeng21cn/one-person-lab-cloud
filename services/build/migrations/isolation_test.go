package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerDatabaseIsolation proves the build owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "build",
		Database:        "opl_build",
		SchemaOwnerRole: "opl_build_owner",
		WriterRole:      "opl_build_writer",
		RuntimeRole:     "opl_build_runtime",
		Source:          source,
		OperationKind:   "build",
		Tables:          []string{"build_jobs", "build_artifacts", "build_logs", "operations", "idempotency_records", "outbox_events", "outbox_deliveries", "inbox_events"},
	})
}
