package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/serve/migrations"
)

// TestOwnerDatabaseIsolation proves the serve owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "serve",
		Database:        "opl_serve",
		SchemaOwnerRole: "opl_serve_owner",
		WriterRole:      "opl_serve_writer",
		RuntimeRole:     "opl_serve_runtime",
		Source:          source,
		OperationKind:   "runtime_deploy",
		Tables:          []string{"agent_deployments", "agent_runtime_instances", "agent_runtime_actions", "access_bindings", "access_switches", "operations", "idempotency_records", "outbox_events", "outbox_deliveries", "inbox_events"},
	})
}
