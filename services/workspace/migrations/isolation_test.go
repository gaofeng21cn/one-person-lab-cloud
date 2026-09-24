package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/workspace/migrations"
)

// TestOwnerDatabaseIsolation proves the workspace owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "workspace",
		Database:        "opl_workspace",
		SchemaOwnerRole: "opl_workspace_owner",
		WriterRole:      "opl_workspace_writer",
		RuntimeRole:     "opl_workspace_runtime",
		Source:          source,
		OperationKind:   "create_workspace",
		Tables:          []string{"workspaces", "subscriptions", "subscription_periods", "operations", "idempotency_records", "outbox_events", "outbox_deliveries", "inbox_events"},
	})
}
