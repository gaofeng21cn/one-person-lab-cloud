package ownermigrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/fabric/ownermigrations"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerDatabaseIsolation proves the fabric owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := ownermigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "fabric",
		Database:        "opl_fabric",
		SchemaOwnerRole: "opl_fabric_owner",
		WriterRole:      "opl_fabric_writer",
		RuntimeRole:     "opl_fabric_runtime",
		Source:          source,
		OperationKind:   "resource_provision",
		Tables:          []string{"resource_sets", "resources", "attachments", "secret_bindings", "resource_actions", "operations", "idempotency_records", "outbox_events", "outbox_deliveries", "inbox_events"},
	})
}
