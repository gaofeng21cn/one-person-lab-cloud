package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/capability/migrations"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerDatabaseIsolation proves the capability owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "capability",
		Database:        "opl_capability",
		SchemaOwnerRole: "opl_capability_owner",
		WriterRole:      "opl_capability_writer",
		RuntimeRole:     "opl_capability_runtime",
		Source:          source,
		OperationKind:   "complete_upload",
		Tables:          []string{"namespaces", "packages", "package_versions", "upload_sessions", "capability_versions", "reference_claims", "operations", "idempotency_records", "outbox_events", "outbox_deliveries", "inbox_events"},
	})
}
