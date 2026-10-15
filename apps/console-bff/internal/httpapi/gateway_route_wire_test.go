package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
	api.UnimplementedGatewayCoordinationServer
	bindingRequest       *api.GetSelfUserExternalBindingRequest
	bindingResponse      *api.UserExternalBindingReadback
	bindingErr           error
	walletRequest        *api.GetWalletRpcRequest
	usageRequest         *api.GetUsageSummaryRpcRequest
	modelRequest         *api.ListModelsRpcRequest
	transactionsRequest  *api.ListWorkspaceTransactionsRpcRequest
	transactionsResponse *api.WalletOperationPage
	transactionsErr      error
	trendRequest         *api.GetWorkspaceTransactionTrendRpcRequest
}

func (s *gatewayOwnerServer) GetWorkspaceTransactionTrend(_ context.Context, r *api.GetWorkspaceTransactionTrendRpcRequest) (*api.WorkspaceTransactionTrend, error) {
	s.trendRequest = r
	return &api.WorkspaceTransactionTrend{WorkspaceId: r.GetWorkspaceId(), PeriodStart: timestamppb.Now(), PeriodEnd: timestamppb.Now(), Days: []*api.WorkspaceTransactionTrendDay{{Date: "2026-10-16", ConfirmedChargeUsdMicros: 9007199254740993}}, UnknownRefundUsdMicros: 7}, nil
}

func (s *gatewayOwnerServer) ListWorkspaceTransactions(_ context.Context, r *api.ListWorkspaceTransactionsRpcRequest) (*api.WalletOperationPage, error) {
	s.transactionsRequest = r
	return s.transactionsResponse, s.transactionsErr
}

func (s *gatewayOwnerServer) GetSelfUserExternalBinding(_ context.Context, r *api.GetSelfUserExternalBindingRequest) (*api.UserExternalBindingReadback, error) {
	s.bindingRequest = r
	return s.bindingResponse, s.bindingErr
}

func (s *gatewayOwnerServer) GetWallet(_ context.Context, r *api.GetWalletRpcRequest) (*api.Wallet, error) {
	s.walletRequest = r
	return &api.Wallet{Source: api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY, Status: api.WalletStatusEnum_WALLET_STATUS_ENUM_AVAILABLE, BalanceUsdMicros: 12_500_000, Currency: api.WalletCurrencyEnum_WALLET_CURRENCY_ENUM_USD, FetchedAt: timestamppb.Now()}, nil
}

func (s *gatewayOwnerServer) GetUsageSummary(_ context.Context, r *api.GetUsageSummaryRpcRequest) (*api.UsageSummary, error) {
	s.usageRequest = r
	return &api.UsageSummary{TotalRequests: 17, TotalInputTokens: 170, TotalOutputTokens: 34, TotalTokens: 204, TotalActualCostUsdMicros: 125001}, nil
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
	if coordination, ok := server.(api.GatewayCoordinationServer); ok {
		api.RegisterGatewayCoordinationServer(grpcServer, coordination)
	}
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

	usageIdentity := allowedIdentity()
	usageIdentity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETUSAGESUMMARY
	usageIdentity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	usageIdentity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-1")}
	response = httptest.NewRecorder()
	NewServer(client, usageIdentity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/usage/summary?period=month"))
	if response.Code != http.StatusOK || owner.usageRequest == nil {
		t.Fatalf("usage status=%d reachedOwner=%v body=%s", response.Code, owner.usageRequest != nil, response.Body.String())
	}
	if owner.usageRequest.GetPeriod() != "month" || owner.usageRequest.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") {
		t.Fatalf("owner usage request=%v", owner.usageRequest)
	}
	if !strings.Contains(response.Body.String(), `"totalRequests":"17"`) || !strings.Contains(response.Body.String(), `"totalActualCostUSDMicros":"125001"`) {
		t.Fatalf("usage wire=%s", response.Body.String())
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

func TestWorkspaceTransactionsTravelThroughTheBFFClientWire(t *testing.T) {
	created := timestamppb.New(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	owner := &gatewayOwnerServer{transactionsResponse: &api.WalletOperationPage{
		Items:      []*api.WalletOperation{{Id: "refund-a", WorkspaceId: proto.String("deleted-workspace"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN, AmountUsdMicros: 9007199254740993, OriginalChargeOperationId: proto.String("charge-a"), CreatedAt: created, UpdatedAt: created}},
		NextCursor: proto.String("next-cursor"),
	}}
	client := dialGatewayOwner(t, owner)
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACETRANSACTIONS
	identity.decision.AuthorizationContextId = proto.String("transactions-authority")
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("deleted-workspace")}
	handler := NewServer(client, identity).Handler()
	request := sessionRequest(http.MethodGet, "/api/v2/workspaces/deleted-workspace/transactions?cursor=original-cursor&limit=2&tenantId=forged-tenant&actorId=forged-actor")
	request.Header.Set("X-Tenant-ID", "forged-tenant")
	request.Header.Set("X-Actor-ID", "forged-actor")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || owner.transactionsRequest == nil {
		t.Fatalf("status=%d request=%v body=%s", response.Code, owner.transactionsRequest, response.Body.String())
	}
	actual := owner.transactionsRequest
	if actual.GetWorkspaceId() != "deleted-workspace" || actual.GetQueryCursor() != "original-cursor" || actual.GetQueryLimit() != 2 || actual.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") || actual.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" || actual.GetContext().GetActorId() == "forged-actor" || actual.GetContext().GetAuthorizationContextId() == "" {
		t.Fatalf("owner request=%v", actual)
	}
	for _, fact := range []string{`"amountUSDMicros":"9007199254740993"`, `"status":"unknown"`, `"kind":"refund"`, `"originalChargeOperationId":"charge-a"`, `"createdAt":"2026-10-01T12:00:00Z"`, `"nextCursor":"next-cursor"`} {
		if !strings.Contains(response.Body.String(), fact) {
			t.Fatalf("missing %s: %s", fact, response.Body.String())
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("financial response may be cached: %v", response.Header())
	}
	for _, query := range []string{"limit=bad", "limit=%ZZ", "limit=", "limit=0", "limit=101", "limit=2147483648", "limit=2&limit=3", "cursor=a&cursor=b"} {
		t.Run(query, func(t *testing.T) {
			owner.transactionsRequest = nil
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/deleted-workspace/transactions?"+query))
			if response.Code != http.StatusBadRequest || owner.transactionsRequest != nil {
				t.Fatalf("malformed pagination reached owner: status=%d request=%v body=%s", response.Code, owner.transactionsRequest, response.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		code     codes.Code
		httpCode int
	}{{codes.PermissionDenied, http.StatusForbidden}, {codes.Unavailable, http.StatusServiceUnavailable}} {
		owner.transactionsErr = status.Error(tc.code, "owner refusal")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/deleted-workspace/transactions"))
		if response.Code != tc.httpCode || owner.transactionsRequest == nil || owner.transactionsRequest.QueryLimit != nil {
			t.Fatalf("owner failure/default not preserved: status=%d request=%v body=%s", response.Code, owner.transactionsRequest, response.Body.String())
		}
	}
}

func TestWorkspaceTransactionTrendTravelsThroughTheBFFClientWire(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACETRANSACTIONTREND
	identity.decision.AuthorizationContextId = proto.String("trend-authority")
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("deleted-workspace")}
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/deleted-workspace/transactions/trend"))
	if response.Code != http.StatusOK || owner.trendRequest.GetWorkspaceId() != "deleted-workspace" || owner.trendRequest.GetContext().GetAuthorizationContextId() != "trend-authority" || owner.trendRequest.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" || !strings.Contains(response.Body.String(), `"confirmedChargeUSDMicros":"9007199254740993"`) || !strings.Contains(response.Body.String(), `"unknownRefundUSDMicros":"7"`) {
		t.Fatalf("trend status=%d request=%v body=%s", response.Code, owner.trendRequest, response.Body.String())
	}
}

// The browser can resolve only its own binding. Actor-like query parameters are
// never forwarded, and an upstream actor mismatch cannot cross the BFF boundary.
func TestSelfExternalBindingsTravelThroughBFFWire(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := allowedIdentity()
	handler := NewServer(client, identity).Handler()
	for _, tc := range []struct {
		name   string
		output *api.UserExternalBindingReadback
		err    error
		code   int
	}{
		{"self", &api.UserExternalBindingReadback{ActorId: "actor-1", Sub2ApiUserId: "101", RegistryHost: "uswccr.ccs.tencentyun.com", RegistryNamespace: "oplcloud", Repository: "testcloud4", Status: api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE, Verification: api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED}, nil, 200},
		{"different_actor", &api.UserExternalBindingReadback{ActorId: "actor-2"}, nil, 502},
		{"absent", nil, status.Error(codes.NotFound, "binding absent"), 404},
		{"revoked", nil, status.Error(codes.PermissionDenied, "binding revoked"), 403},
		{"unverified", nil, status.Error(codes.FailedPrecondition, "binding unverified"), 422},
		{"unavailable", nil, status.Error(codes.Unavailable, "owner unavailable"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner.bindingResponse, owner.bindingErr, owner.bindingRequest = tc.output, tc.err, nil
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/auth/external-bindings?actorId=actor-2&repository=forged"))
			if response.Code != tc.code {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.code, response.Body.String())
			}
			call := owner.bindingRequest.GetContext()
			if call.GetActorId() != "actor-1" || call.GetSessionId() != owneridentity.SessionReference("session-1") || call.GetScope().GetTenant().GetTenantId() != "tenant-1" {
				t.Fatalf("binding context=%v", call)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private binding response is cacheable")
			}
			if tc.name == "self" && (!strings.Contains(response.Body.String(), `"repository":"testcloud4"`) || !strings.Contains(response.Body.String(), `"sub2apiUserId":"101"`)) {
				t.Fatalf("self binding=%s", response.Body.String())
			}
		})
	}
	owner.bindingRequest = nil
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/auth/external-bindings", nil))
	if response.Code != 401 || owner.bindingRequest != nil {
		t.Fatalf("anonymous status=%d reached owner=%v", response.Code, owner.bindingRequest != nil)
	}
}
