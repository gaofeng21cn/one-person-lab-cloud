package coordination_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/internal/ownerservice"
)

// unreachableDB is a real *sql.DB whose connections never open. This suite
// proves which owner Fabric's Secret write guards admit, so a request that is
// accepted by the guard must stop at the next check — the absent provider
// capability — instead of being answered by a database.
type unreachableDB struct{}

func (unreachableDB) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("secret peer guard suite provisions no database")
}

func (unreachableDB) Driver() driver.Driver { return unreachableDriver{} }

type unreachableDriver struct{}

func (unreachableDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("secret peer guard suite provisions no database")
}

// startSecretPeerSurface serves the real Fabric coordination handlers behind the
// real owner identity interceptor, with per-peer tokens for every owner this test
// drives. A caller that is refused here is refused by the operation's own guard:
// authentication, the header and the token have already succeeded.
func startSecretPeerSurface(t *testing.T, peers ...owneridentity.Service) func(owneridentity.Service) api.FabricCoordinationClient {
	t.Helper()
	const token = "isolated-fabric-secret-peer-token-0001"
	config := ownerservice.Config{
		Owner: ownerservice.OwnerFabric,
		TLS:   owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{},
	}
	for _, value := range peers {
		config.Peers[value] = token
	}
	catalogConnection, err := grpc.NewClient("passthrough:///unused", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { catalogConnection.Close() })
	service, err := coordination.New(sql.OpenDB(unreachableDB{}), ownerservice.NewAuthorizer(ownerservice.OwnerFabric, &identity{}).Authorize, api.NewCatalogCoordinationClient(catalogConnection))
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	client := func(value owneridentity.Service) api.FabricCoordinationClient {
		opts, err := config.TLS.DialOptions(value, owneridentity.Fabric.Service(), token)
		if err != nil {
			t.Fatal(err)
		}
		connection, err := grpc.NewClient(listener.Addr().String(), opts...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connection.Close() })
		return api.NewFabricCoordinationClient(connection)
	}
	return client
}

// TestSecretWriteGuardsAdmitTheWorkspaceHandoverCaller proves the owner decision on
// Fabric's two Secret write entry points over the real authenticated boundary:
// Workspace — the owner that obtains the Gateway binding and hands it to Fabric
// before Serve is called — and Serve are both admitted past the guard, and each
// reaches the next Fabric check instead of being refused as an outsider.
func TestSecretWriteGuardsAdmitTheWorkspaceHandoverCaller(t *testing.T) {
	clientFor := startSecretPeerSurface(t, owneridentity.Workspace.Service(), owneridentity.Serve.Service())
	workspace := clientFor(owneridentity.Workspace.Service())
	serve := clientFor(owneridentity.Serve.Service())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const workspaceID, tenant = "workspace-models-handover", "tenant"
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	bind := func(c api.FabricCoordinationClient, key string) error {
		_, err := c.BindSecret(ctx, &api.SecretBindingCommand{Context: call(key), WorkspaceId: workspaceID, RuntimeInstanceId: "rt-models", KeyBindingId: "key-binding", SecretDeliveryReference: "opl-gateway-" + workspaceID, TargetSlot: "gateway", Fingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
		return err
	}
	rebind := func(c api.FabricCoordinationClient, key string) error {
		_, err := c.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call(key), WorkspaceId: workspaceID, RuntimeInstanceId: "rt-models", ExpectedCurrentSecretBindingId: "sbx_predecessor", KeyBindingId: "key-binding", SecretDeliveryReference: "opl-gateway-" + workspaceID, TargetSlot: "gateway", Fingerprint: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"})
		return err
	}

	for _, entry := range []struct {
		entryPoint string
		invoke     func(api.FabricCoordinationClient, string) error
	}{
		{entryPoint: "BindSecret", invoke: bind},
		{entryPoint: "RebindSecret", invoke: rebind},
	} {
		// The models handover caller is admitted by both entry points and stops at
		// the provider-capability check that follows the guard.
		if err := entry.invoke(workspace, entry.entryPoint+"-workspace"); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("Workspace %s err=%v want failed precondition from the check after the peer guard", entry.entryPoint, err)
		}
		// Serve keeps its admitted path: admitting Workspace must not retire it.
		if err := entry.invoke(serve, entry.entryPoint+"-serve"); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("Serve %s err=%v want failed precondition from the check after the peer guard", entry.entryPoint, err)
		}
	}
}

// TestSecretWriteGuardsRefuseAnOwnerNeitherPathAuthorises proves the guards name
// their admitted owners rather than admitting whoever authenticates. Build
// authenticates against the same allowlist with the same token, so a refusal
// here is the operation-level decision and not an authentication failure.
func TestSecretWriteGuardsRefuseAnOwnerNeitherPathAuthorises(t *testing.T) {
	clientFor := startSecretPeerSurface(t, owneridentity.Workspace.Service(), owneridentity.Serve.Service(), owneridentity.Build.Service())
	build := clientFor(owneridentity.Build.Service())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const workspaceID, tenant = "workspace-models-handover", "tenant"
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	if _, err := build.BindSecret(ctx, &api.SecretBindingCommand{Context: call("bind-build"), WorkspaceId: workspaceID, RuntimeInstanceId: "rt-models", KeyBindingId: "key-binding", SecretDeliveryReference: "opl-gateway-" + workspaceID, TargetSlot: "gateway"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Build BindSecret err=%v want permission denied", err)
	}
	if _, err := build.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call("rebind-build"), WorkspaceId: workspaceID, RuntimeInstanceId: "rt-models", ExpectedCurrentSecretBindingId: "sbx_predecessor", KeyBindingId: "key-binding", SecretDeliveryReference: "opl-gateway-" + workspaceID, TargetSlot: "gateway"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Build RebindSecret err=%v want permission denied", err)
	}
}
