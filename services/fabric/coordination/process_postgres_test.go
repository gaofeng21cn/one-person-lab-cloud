package coordination_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

type identityServer struct {
	api.UnimplementedCloudIdentityAuthorizationServer
	authority identity
}

func (i *identityServer) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	return i.authority.AuthorizeAction(ctx, r)
}

// This proves the same optional startup main invokes, real owner migrations,
// peer metadata and shared Operation readback. Catalog/Identity decisions are
// fixtures here; the parent cross-owner test proves their actual policy.
func TestProcessServesAcceptedResourcesAndOwnerOperation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "fabric", Database: "opl_fabric", SchemaOwnerRole: "opl_fabric_owner", WriterRole: "opl_fabric_writer", RuntimeRole: "opl_fabric_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	t.Setenv("OPL_POSTGRES_TESTS", "1")
	r := command("process")
	catalogOwner := &catalog{acceptances: map[string]*api.QuoteAcceptance{r.QuoteAcceptance.Quote.Id: r.QuoteAcceptance}}
	dependency := grpc.NewServer()
	api.RegisterCatalogCoordinationServer(dependency, catalogOwner)
	api.RegisterCloudIdentityAuthorizationServer(dependency, &identityServer{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go dependency.Serve(listener)
	t.Cleanup(dependency.Stop)
	token := "isolated-fabric-process-peer-token-00000001"
	peers, _ := json.Marshal(map[string]string{"workspace": token})
	env := map[string]string{"OPL_FABRIC_DATABASE_URL": h.OwnerDSN, "OPL_FABRIC_PEER_TOKENS": string(peers), "OPL_GRPC_INSECURE_LOCAL": "1", "OPL_RESOURCE_CATALOG_ADDR": listener.Addr().String(), "OPL_RESOURCE_CATALOG_TOKEN": token, "OPL_CLOUD_IDENTITY_URL": listener.Addr().String(), "OPL_CLOUD_IDENTITY_TOKEN": token}
	bootstrap, err := coordination.Start(ctx, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bootstrap.Close() })
	if err = bootstrap.Server.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	wire, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go bootstrap.Server.ServeOn(wire)
	t.Cleanup(bootstrap.Server.Stop)
	opts, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.Workspace.Service(), owneridentity.Fabric.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(wire.Addr().String(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	accepted, err := api.NewFabricCoordinationClient(conn).EnsureResources(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	read, err := api.NewOwnerOperationsClient(conn).Read(ctx, &api.OwnerOperationRequest{Context: r.Context, OperationId: accepted.OperationId})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(accepted, read) {
		t.Fatalf("owner operation differs from acceptance: %v / %v", accepted, read)
	}
}
