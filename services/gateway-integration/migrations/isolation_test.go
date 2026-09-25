package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerDatabaseIsolation proves the tenant owner's real migration entrypoint
// installs its own schema, refuses a differently named database, and leaves its
// writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "tenant",
		Database:        "opl_tenant",
		SchemaOwnerRole: "opl_tenant_owner",
		WriterRole:      "opl_tenant_writer",
		RuntimeRole:     "opl_tenant_runtime",
		Source:          source,
		OperationKind:   "create_tenant",
		Tables:          []string{"tenants", "tenant_members", "sessions", "authorization_contexts", "accepted_operation_grants", "operations", "idempotency_records", "audit_events"},
	})
}
