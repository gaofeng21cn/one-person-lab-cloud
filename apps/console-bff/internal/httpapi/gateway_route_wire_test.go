package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// gatewayOwnerServer is the generated GatewayProductService contract server used
// as the wire endpoint for the caller test below. The owner's real
// implementation is exercised by the live test in
// services/gateway-integration/identity (real PostgreSQL, real gRPC, real
// Sub2API HTTP fixture); this test proves the BFF's own client transport, route
// boundary and serialization over a real gRPC connection rather than an
// in-process probe.
type gatewayOwnerServer struct {
	api.UnimplementedGatewayProductServiceServer
	walletRequest *api.GetWalletRpcRequest
	modelRequest  *api.ListModelsRpcRequest
}

func (s *gatewayOwnerServer) GetWallet(_ context.Context, r *api.GetWalletRpcRequest) (*api.Wallet, error) {
	s.walletRequest = r
	return &api.Wallet{Source: api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY, Status: api.WalletStatusEnum_WALLET_STATUS_ENUM_AVAILABLE, BalanceUsdMicros: 12_500_000, Currency: api.WalletCurrencyEnum_WALLET_CURRENCY_ENUM_USD, FetchedAt: timestamppb.Now()}, nil
}

func (s *gatewayOwnerServer) ListModels(_ context.Context, r *api.ListModelsRpcRequest) (*api.ModelPage, error) {
	s.modelRequest = r
	return &api.ModelPage{Items: []*api.Model{{Id: "gpt-5.6-sol", Name: "gpt-5.6-sol", Available: true, FetchedAt: timestamppb.Now()}}}, nil
}

// dialGatewayOwner starts a real gRPC server hosting the GatewayProductService
// contract and returns a production BFF client set dialed against it.
func dialGatewayOwner(t *testing.T, server api.GatewayProductServiceServer) *clients.Clients {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	api.RegisterGatewayProductServiceServer(grpcServer, server)
	api.RegisterOwnerOperationsServer(grpcServer, &api.UnimplementedOwnerOperationsServer{})
	api.RegisterTenantProductServiceServer(grpcServer, &api.UnimplementedTenantProductServiceServer{})
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	client, err := clients.Dial(clients.Config{
		Addresses: map[owneridentity.Owner]string{owneridentity.Gateway: listener.Addr().String()},
		Tokens:    map[owneridentity.Owner]string{owneridentity.Gateway: "gateway-test-token-000000000000000001"},
		TLS:       owneridentity.TLSConfig{AllowInsecureLocal: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

// TestWalletAndModelRoutesTravelThroughTheBFFClientWire proves the real BFF
// handler reaches the Gateway owner through the production typed client over a
// real gRPC connection, and that the wallet/model responses serialize with the
// contract spelling.
func TestWalletAndModelRoutesTravelThroughTheBFFClientWire(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := allowedIdentity()

	walletIdentity := allowedIdentity()
	walletIdentity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET
	walletIdentity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	walletIdentity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-1")}

	response := httptest.NewRecorder()
	NewServer(client, walletIdentity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
	if response.Code != http.StatusOK || owner.walletRequest == nil {
		t.Fatalf("wallet status=%d reachedOwner=%v body=%s", response.Code, owner.walletRequest != nil, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"balanceUSDMicros":"12500000"`) {
		t.Fatalf("wallet wire=%s", response.Body.String())
	}
	// The BFF forwards the caller's own session to the owner and asserts nothing.
	if owner.walletRequest.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") {
		t.Fatalf("owner wallet context=%v", owner.walletRequest.GetContext())
	}

	modelIdentity := allowedIdentity()
	modelIdentity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMODELS
	modelIdentity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	modelIdentity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	response = httptest.NewRecorder()
	NewServer(client, modelIdentity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/catalog/models?cursor=m1&limit=5"))
	if response.Code != http.StatusOK || owner.modelRequest == nil {
		t.Fatalf("models status=%d reachedOwner=%v body=%s", response.Code, owner.modelRequest != nil, response.Body.String())
	}
	if owner.modelRequest.GetQueryCursor() != "m1" || owner.modelRequest.GetQueryLimit() != 5 {
		t.Fatalf("owner model request=%v", owner.modelRequest)
	}
	for _, forbidden := range []string{"PricePerMillionTokens", "priceSource"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("model wire leaked %s: %s", forbidden, response.Body.String())
		}
	}
	_ = identity
}

// TestWalletRouteFailsClosedWhenTheGatewayOwnerIsAbsent proves an unconfigured
// gateway owner is refused rather than answered with fabricated wallet facts.
func TestWalletRouteFailsClosedWhenTheGatewayOwnerIsAbsent(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-1")}
	client, err := clients.Dial(clients.Config{TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatalf("missing gateway not fail-closed: status=%d body=%s", response.Code, response.Body.String())
	}
}

var _ = insecure.NewCredentials
