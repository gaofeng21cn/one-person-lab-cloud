package ownerservice

import (
	"context"
	"errors"
	"opl-cloud/packages/contracts/go/owneridentity"
	"strings"
	"testing"

	"google.golang.org/grpc"
)

func testConfig(owner Owner) Config {
	return Config{
		TLS:   owneridentity.TLSConfig{AllowInsecureLocal: true},
		Owner: owner,
		Addr:  "127.0.0.1:0",
		Peers: map[Service]string{Service(OwnerWorkspace): "0123456789abcdef0123456789abcdef"},
	}
}

func TestReadyRefusesServingWithoutAProductGroup(t *testing.T) {
	server, err := NewServer(testConfig(OwnerCapability))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Ready(context.Background()); err == nil {
		t.Fatal("an owner with no registered product group reported ready")
	}
}

func TestReadyRefusesServingUntilDeclaredGroupsAreRegistered(t *testing.T) {
	server, err := NewServer(testConfig(OwnerCapability))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.RequireProductGroups("CapabilityProductService"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterGroup("BuildProductService", noopRegister); err != nil {
		t.Fatal(err)
	}
	if err := server.Ready(context.Background()); err == nil || !containsGroup(err, "CapabilityProductService") {
		t.Fatalf("readiness did not name the missing product group: %v", err)
	}
	if err := server.RegisterGroup("CapabilityProductService", noopRegister); err != nil {
		t.Fatal(err)
	}
	if err := server.Ready(context.Background()); err != nil {
		t.Fatalf("all declared groups are registered but readiness failed: %v", err)
	}
}

func TestReadyRefusesServingWhenADependencyFails(t *testing.T) {
	server, err := NewServer(testConfig(OwnerServe))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.RequireProductGroups("ServeProductService"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterGroup("ServeProductService", noopRegister); err != nil {
		t.Fatal(err)
	}
	dependencyError := errors.New("database is unreachable")
	if err := server.AddReadinessCheck("database", func(context.Context) error { return dependencyError }); err != nil {
		t.Fatal(err)
	}
	if err := server.MarkServing(context.Background()); !errors.Is(err, dependencyError) && err == nil {
		t.Fatalf("MarkServing accepted an unreachable dependency: %v", err)
	}
	if server.healthServing() {
		t.Fatal("an owner with a failing dependency reported SERVING")
	}
}

func TestRegisterAfterServingIsRefused(t *testing.T) {
	server, err := NewServer(testConfig(OwnerBuild))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.RequireProductGroups("BuildProductService"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterGroup("BuildProductService", noopRegister); err != nil {
		t.Fatal(err)
	}
	if err := server.MarkServing(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !server.healthServing() {
		t.Fatal("a ready owner did not report SERVING")
	}
	if err := server.RegisterGroup("BuildProductService", noopRegister); err == nil {
		t.Fatal("a serving owner accepted a new product group registration")
	}
}

func containsGroup(err error, group string) bool {
	var notReady ErrNotReady
	if !errors.As(err, &notReady) {
		return false
	}
	return strings.Contains(notReady.Reason, group)
}

// noopRegister is a product-group registration that installs no gRPC service: the
// readiness rule under test is registration bookkeeping, not the service body.
var noopRegister = func(*grpc.Server) {}
