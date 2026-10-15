package identity_test

import (
	"context"
	"os"
	"testing"

	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// gatewayStoreSystem installs the real Gateway migrations on a distinct owner
// connection. The Tenant connection cannot read or write this database.
func gatewayStoreSystem(t *testing.T) *identity.GatewayStore {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "gateway", Database: "opl_gateway", SchemaOwnerRole: "opl_gateway_owner", WriterRole: "opl_gateway_writer", RuntimeRole: "opl_gateway_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := gatewaymigrations.Source()
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
	store, err := identity.NewGatewayStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
