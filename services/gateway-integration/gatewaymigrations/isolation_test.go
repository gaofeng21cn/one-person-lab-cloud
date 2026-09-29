package gatewaymigrations_test

import (
	"os"
	"testing"

	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestOwnerDatabaseIsolation proves the gateway data owner's real migration
// entrypoint installs its own schema into its own database, refuses a differently
// named database, and leaves its writer able to write only the gateway schema.
// It is the second data owner inside the Gateway Integration deployment unit and
// must not install into the tenant database.
func TestOwnerDatabaseIsolation(t *testing.T) {
	source, err := gatewaymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	ownerstoretest.RunIsolationSuite(t, os.Getenv, ownerstoretest.Owner{
		Name:            "gateway",
		Database:        "opl_gateway",
		SchemaOwnerRole: "opl_gateway_owner",
		WriterRole:      "opl_gateway_writer",
		RuntimeRole:     "opl_gateway_runtime",
		Source:          source,
		OperationKind:   "wallet_operation",
		Tables:          []string{"identity_mappings", "tenant_wallet_bindings", "key_bindings", "wallet_operations", "outbox_events", "outbox_deliveries", "inbox_events", "idempotency_records", "operations"},
	})
}
