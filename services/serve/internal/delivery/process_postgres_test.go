package delivery_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/serve/internal/delivery"
)

// noMigrations is an already-installed owner schema: this test starts the Serve
// process against the real opl_serve schema the fixture installed through Serve's
// own migration entrypoint, so the process does not re-apply DDL. The real
// migration entrypoint itself is proven by TestOwnerDatabaseIsolation.
type noMigrations struct{}

func (noMigrations) ReadDir(string) ([]ownerstore.DirEntry, error) { return nil, nil }

func (noMigrations) ReadFile(string) ([]byte, error) {
	return nil, errors.New("no embedded migrations")
}

// serveDatabase opens the owner database the Serve process runs as: the runtime
// login holding only the owner's writer grant.
func serveDatabase(t *testing.T, runtimeDSN string) *ownerservice.Database {
	t.Helper()
	t.Setenv("OPL_POSTGRES_TESTS", "1")
	database, err := ownerservice.OpenDatabase(context.Background(), ownerservice.OwnerServe, runtimeDSN)
	if err != nil {
		t.Fatalf("open serve owner database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// These two constants are the fixed local test identities, never production
// values. The tokens exceed the shared minimum length.
const (
	identityToken = "0123456789abcdef0123456789abcdef"
	serveToken    = "fedcba9876543210fedcba9876543210"
)

// stubCloudIdentityServer is a decision STUB, not the CloudIdentity product
// implementation. It issues an ALLOW bound to the exact request Serve sent, so
// this package can prove Serve's own request shape, decision validation, scope
// guard and read projection in isolation.
//
// It deliberately does NOT model the real authorization policy: the production
// authority is services/gateway-integration/identity, whose policy table decides
// whether an audience/action pair is admitted at all. That real implementation,
// including the denial paths, is exercised only by the opt-in livebuild test
// (identity_live_test.go). A test that uses this stub proves Serve's owner
// behaviour, never that CloudIdentity admits the read.
type stubCloudIdentityServer struct {
	api.UnimplementedCloudIdentityAuthorizationServer
}

func (*stubCloudIdentityServer) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

// startStubCloudIdentity serves the stub decision above over a real gRPC
// listener, so the Serve process's real identity interceptor and its real typed
// client are both exercised.
func startStubCloudIdentity(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	api.RegisterCloudIdentityAuthorizationServer(server, &stubCloudIdentityServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func serveConfig(cloudIdentityAddr string) ownerservice.Config {
	return ownerservice.Config{
		Owner:              ownerservice.OwnerServe,
		TLS:                owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers:              map[ownerservice.Service]string{owneridentity.ConsoleBFF: serveToken},
		CloudIdentityAddr:  cloudIdentityAddr,
		CloudIdentityToken: identityToken,
	}
}

// configure is the Serve process's product registration: the same
// delivery.Configure path main.go uses, closed over the process configuration.
func configure(config ownerservice.Config) func(*ownerservice.Server, *ownerservice.Database) error {
	return func(server *ownerservice.Server, database *ownerservice.Database) error {
		return delivery.Configure(server, database, config)
	}
}

// consoleBFFConn dials the Serve owner as the Console BFF, presenting the
// verified peer identity the owner's interceptor requires.
func consoleBFFConn(t *testing.T, address string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			ctx = metadata.AppendToOutgoingContext(ctx, owneridentity.PeerHeader, string(owneridentity.ConsoleBFF), owneridentity.TokenHeader, serveToken)
			return invoke(ctx, method, req, reply, cc, opts...)
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestServeProcessServesOwnerTruthOverTheWire proves the real Serve process
// wiring: it reports SERVING with a reachable database and a reachable
// CloudIdentity boundary, and the Console BFF's own typed call reaches Serve's
// owner-local truth instead of a scaffold.
//
// The CloudIdentity side is the decision stub above, so this proves the process
// wiring, the identity interceptor, the typed client and the read projection. It
// does NOT prove that the real authority admits a Serve read; that is the
// livebuild identity integration test's job.
func TestServeProcessServesOwnerTruthOverTheWire(t *testing.T) {
	db, tenant, runtimeDSN := fixture(t)
	ctx := context.Background()
	seedDeployment(t, db, "ws-wire", tenant, "dep-wire", "active", "ready", "https://ws-wire.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)

	config := serveConfig(startStubCloudIdentity(t))
	bootstrap, err := ownerservice.StartWithDatabase(ctx, serveDatabase(t, runtimeDSN), config, noMigrations{}, configure(config))
	if err != nil {
		t.Fatalf("start serve process: %v", err)
	}
	t.Cleanup(func() { _ = bootstrap.Close() })
	if err := bootstrap.Server.Ready(ctx); err != nil {
		t.Fatalf("serve reported not ready: %v", err)
	}
	groups := bootstrap.Server.ProductGroups()
	if len(groups) != 1 || groups[0] != "ServeProductService" {
		t.Fatalf("serve exposed %v, want exactly ServeProductService", groups)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = bootstrap.Server.ServeOn(listener) }()
	t.Cleanup(bootstrap.Server.Stop)

	client := api.NewServeProductServiceClient(consoleBFFConn(t, listener.Addr().String()))
	callCtx := call(tenant, false)
	access, err := client.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: callCtx, WorkspaceId: "ws-wire"})
	if err != nil {
		t.Fatalf("GetWorkspaceAccess over the wire: %v", err)
	}
	if access.GetUrl() != "https://ws-wire.example/app" || !access.GetApplicationCredentialsAvailable() {
		t.Fatalf("wire access readback = %+v", access)
	}
	page, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: callCtx, WorkspaceId: "ws-wire"})
	if err != nil {
		t.Fatalf("ListDeployments over the wire: %v", err)
	}
	if len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != "dep-wire" {
		t.Fatalf("wire deployment page = %+v", page)
	}
}

// TestServeProcessRefusesReadinessWithoutCloudIdentity proves Serve fails closed:
// a process whose CloudIdentity boundary is unconfigured reports NOT_SERVING and
// names the missing dependency instead of serving unauthenticated reads.
func TestServeProcessRefusesReadinessWithoutCloudIdentity(t *testing.T) {
	_, _, runtimeDSN := fixture(t)
	ctx := context.Background()
	config := serveConfig("")
	bootstrap, err := ownerservice.StartWithDatabase(ctx, serveDatabase(t, runtimeDSN), config, noMigrations{}, configure(config))
	if err != nil {
		t.Fatalf("start serve process: %v", err)
	}
	t.Cleanup(func() { _ = bootstrap.Close() })
	err = bootstrap.Server.Ready(ctx)
	if err == nil || !strings.Contains(err.Error(), "cloud_identity") {
		t.Fatalf("serve without CloudIdentity reported ready: %v", err)
	}
}
