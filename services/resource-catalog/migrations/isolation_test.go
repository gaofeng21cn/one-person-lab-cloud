package migrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/resource-catalog/migrations"
)

// TestOwnerDatabaseIsolation proves the resource_catalog owner's real migration
// entrypoint installs its own schema, refuses a differently named database, and
// leaves its writer able to write only this owner's schema.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "resource_catalog",
		Database:        "opl_resource_catalog",
		SchemaOwnerRole: "opl_resource_catalog_owner",
		WriterRole:      "opl_resource_catalog_writer",
		RuntimeRole:     "opl_resource_catalog_runtime",
		Source:          source,
		OperationKind:   "reconcile",
		Tables: []string{
			"compute_plans", "storage_plans", "price_policy_versions",
			"refund_policy_versions", "retention_policy_versions",
			"quotes", "quote_items", "operations", "idempotency_records",
			"outbox_events", "outbox_deliveries", "inbox_events",
		},
	})
}
