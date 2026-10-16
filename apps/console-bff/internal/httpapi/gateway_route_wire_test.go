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
	bindingRequest         *api.GetSelfUserExternalBindingRequest
	bindingResponse        *api.UserExternalBindingReadback
	bindingErr             error
	walletRequest          *api.GetWalletRpcRequest
	usageRequest           *api.GetUsageSummaryRpcRequest
	modelRequest           *api.ListModelsRpcRequest
	transactionsRequest    *api.ListWorkspaceTransactionsRpcRequest
	transactionsResponse   *api.WalletOperationPage
	transactionsErr        error
	trendRequest           *api.GetWorkspaceTransactionTrendRpcRequest
	personalKeysRequest    *api.ListPersonalGatewayKeysRpcRequest
	personalErr            error
	personalKeyRequest     *api.GetPersonalGatewayKeyRpcRequest
	personalGroupRequest   *api.ListPersonalGatewayGroupsRpcRequest
	personalUsageRequest   *api.ListPersonalGatewayKeyUsageRpcRequest
	personalSummaryRequest *api.GetPersonalGatewayKeyUsageSummaryRpcRequest
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

func TestPersonalGatewayRoutesPreserveNoTenantSession(t *testing.T) {
	for _, row := range []struct {
		path   string
		action api.AuthorizationActionEnum
	}{
		{"/api/v2/wallet", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET},
		{"/api/v2/usage/summary?period=month", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETUSAGESUMMARY},
	} {
		t.Run(row.path, func(t *testing.T) {
			owner := &gatewayOwnerServer{}
			client := dialGatewayOwner(t, owner)
			identity := allowedIdentity()
			identity.session.TenantId = nil
			identity.decision.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
			identity.decision.Action = row.action
			identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
			identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT}
			response := httptest.NewRecorder()
			request := sessionRequest(http.MethodGet, row.path)
			request.Header.Set("x-opl-actor", "other-user")
			request.Header.Set("x-opl-tenant", "other-tenant")
			NewServer(client, identity).Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("personal read=%d %s", response.Code, response.Body.String())
			}
			var call *api.CallContext
			if owner.walletRequest != nil {
				call = owner.walletRequest.Context
			} else if owner.usageRequest != nil {
				call = owner.usageRequest.Context
			}
			if call == nil || call.ActorId != "actor-1" || call.Scope.GetPlatform() == nil || call.GetSessionId() != owneridentity.SessionReference("session-1") {
				t.Fatalf("wrong caller=%v", call)
			}
			if len(identity.requests) != 1 || identity.requests[0].Resource.GetId() != "" {
				t.Fatal("personal read selected a Tenant")
			}
		})
	}
}

func (s *gatewayOwnerServer) ListPersonalGatewayKeys(_ context.Context, r *api.ListPersonalGatewayKeysRpcRequest) (*api.PersonalGatewayKeyPage, error) {
	s.personalKeysRequest = r
	if s.personalErr != nil {
		return nil, s.personalErr
	}
	return &api.PersonalGatewayKeyPage{Items: []*api.PersonalGatewayKey{{Id: "9007199254740993", Name: "personal", Status: api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_ACTIVE, Kind: api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_GENERAL, QuotaUsdMicros: 9007199254740993, RateLimit5HUsdMicros: 1, Usage1DUsdMicros: 2}}, Total: 1, Page: r.Page, PageSize: r.PageSize, Pages: 1}, nil
}

func TestPersonalGatewayKeysUseTypedReadRoute(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := allowedIdentity()
	identity.session.TenantId = nil
	identity.decision = allowFor("actor-1", "", api.OwnerEnum_OWNER_ENUM_GATEWAY, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "")
	identity.decision.Resource.Id = nil
	identity.decision.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/gateway/keys"))
	if response.Code != http.StatusOK {
		t.Fatalf("personal key route status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.personalKeysRequest == nil || owner.personalKeysRequest.Page != 1 || owner.personalKeysRequest.PageSize != 20 || owner.personalKeysRequest.SortBy != "createdAt" || owner.personalKeysRequest.SortOrder != "desc" || owner.personalKeysRequest.Context.GetSessionId() != owneridentity.SessionReference("session-1") {
		t.Fatalf("request=%v", owner.personalKeysRequest)
	}
	for _, fact := range []string{`"quotaUSDMicros":"9007199254740993"`, `"rateLimit5hUSDMicros":"1"`, `"usage1dUSDMicros":"2"`, `"total":"1"`, `"status":"active"`, `"kind":"general"`, `"manageable":false`} {
		if !strings.Contains(response.Body.String(), fact) {
			t.Fatalf("missing %s: %s", fact, response.Body.String())
		}
	}

}

func (s *gatewayOwnerServer) GetPersonalGatewayKey(_ context.Context, r *api.GetPersonalGatewayKeyRpcRequest) (*api.PersonalGatewayKey, error) {
	s.personalKeyRequest = r
	if s.personalErr != nil {
		return nil, s.personalErr
	}
	return &api.PersonalGatewayKey{Id: r.KeyId, Name: "personal", Status: api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_DISABLED, Kind: api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_WORKSPACE}, nil
}
func (s *gatewayOwnerServer) ListPersonalGatewayGroups(_ context.Context, r *api.ListPersonalGatewayGroupsRpcRequest) (*api.PersonalGatewayGroupPage, error) {
	s.personalGroupRequest = r
	if s.personalErr != nil {
		return nil, s.personalErr
	}
	return &api.PersonalGatewayGroupPage{Items: []*api.PersonalGatewayGroup{{Id: "3", Name: "zero-rate", RateMultiplier: 0}}, Total: 1}, nil
}
func (s *gatewayOwnerServer) ListPersonalGatewayKeyUsage(_ context.Context, r *api.ListPersonalGatewayKeyUsageRpcRequest) (*api.PersonalGatewayKeyUsagePage, error) {
	s.personalUsageRequest = r
	if s.personalErr != nil {
		return nil, s.personalErr
	}
	return &api.PersonalGatewayKeyUsagePage{Items: []*api.PersonalGatewayKeyUsage{{ApiKeyId: r.KeyId, RequestId: "personal-request", InputTokens: 9007199254740993, ActualCostUsdMicros: 13, DurationMs: proto.Int64(0), FirstTokenMs: proto.Int64(0)}}, Total: 3, Page: r.Page, PageSize: r.PageSize, Pages: 3}, nil
}
func (s *gatewayOwnerServer) GetPersonalGatewayKeyUsageSummary(_ context.Context, r *api.GetPersonalGatewayKeyUsageSummaryRpcRequest) (*api.UsageSummary, error) {
	s.personalSummaryRequest = r
	if s.personalErr != nil {
		return nil, s.personalErr
	}
	return &api.UsageSummary{TotalRequests: 7, TotalActualCostUsdMicros: 13}, nil
}

func TestPersonalGatewayReadsTravelThroughSameSessionWire(t *testing.T) {
	for _, tenant := range []string{"", "tenant-1"} {
		t.Run("tenant="+tenant, func(t *testing.T) {
			owner := &gatewayOwnerServer{}
			client := dialGatewayOwner(t, owner)
			cases := []struct {
				path    string
				action  api.AuthorizationActionEnum
				fact    string
				request func() proto.Message
			}{
				{"/api/v2/gateway/keys?page=2&pageSize=1&search=Case+Sensitive&status=expired&groupId=0&sortBy=currentConcurrency&sortOrder=asc", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYS, `"quotaUSDMicros":"9007199254740993"`, func() proto.Message { return owner.personalKeysRequest }},
				{"/api/v2/gateway/keys/9223372036854775807", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEY, `"kind":"workspace"`, func() proto.Message { return owner.personalKeyRequest }},
				{"/api/v2/gateway/groups", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYGROUPS, `"rateMultiplier":0`, func() proto.Message { return owner.personalGroupRequest }},
				{"/api/v2/gateway/keys/9223372036854775807/usage?page=3&pageSize=1&period=week", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYUSAGE, `"inputTokens":"9007199254740993"`, func() proto.Message { return owner.personalUsageRequest }},
				{"/api/v2/gateway/keys/9223372036854775807/usage-summary?period=today", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEYUSAGESUMMARY, `"totalRequests":"7"`, func() proto.Message { return owner.personalSummaryRequest }},
			}
			for _, row := range cases {
				t.Run(row.path, func(t *testing.T) {
					identity := allowedIdentity()
					identity.decision = allowFor("actor-1", tenant, api.OwnerEnum_OWNER_ENUM_GATEWAY, row.action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, tenant)
					if tenant == "" {
						identity.session.TenantId = nil
						identity.decision.Resource.Id = nil
						identity.decision.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
					}
					request := sessionRequest(http.MethodGet, row.path)
					request.Header.Set("X-Actor-ID", "forged-actor")
					request.Header.Set("X-Tenant-ID", "forged-tenant")
					response := httptest.NewRecorder()
					handler := NewServer(client, identity).Handler()
					handler.ServeHTTP(response, request)
					if response.Code != 200 || !strings.Contains(response.Body.String(), row.fact) {
						t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
					}
					call := row.request().(interface{ GetContext() *api.CallContext }).GetContext()
					if call.GetSessionId() != owneridentity.SessionReference("session-1") || call.ActorId != "actor-1" || call.Scope.GetTenant().GetTenantId() != tenant {
						t.Fatalf("caller=%v", call)
					}
					if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
						t.Fatalf("headers=%v", response.Header())
					}
					if len(identity.requests) != 1 || identity.requests[0].Resource.GetId() != tenant || identity.requests[0].GetAction() != row.action {
						t.Fatalf("authorization=%v", identity.requests)
					}
					if row.action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYS {
						actual := owner.personalKeysRequest
						if actual.Page != 2 || actual.PageSize != 1 || actual.Search != "Case Sensitive" || actual.Status != "expired" || actual.GroupId == nil || *actual.GroupId != "0" || actual.SortBy != "currentConcurrency" || actual.SortOrder != "asc" {
							t.Fatalf("query=%v", actual)
						}
					}
					if row.action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEY && owner.personalKeyRequest.KeyId != "9223372036854775807" {
						t.Fatal("key ID changed")
					}
					if row.action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYUSAGE {
						for _, field := range []string{`"durationMs":"0"`, `"firstTokenMs":"0"`, `"actualCostUSDMicros":"13"`} {
							if !strings.Contains(response.Body.String(), field) {
								t.Fatalf("wire missing %s: %s", field, response.Body.String())
							}
						}
						actual := owner.personalUsageRequest
						if actual.KeyId != "9223372036854775807" || actual.Page != 3 || actual.PageSize != 1 || actual.Period != "week" {
							t.Fatalf("usage=%v", actual)
						}
					}
					if row.action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEYUSAGESUMMARY && (owner.personalSummaryRequest.KeyId != "9223372036854775807" || owner.personalSummaryRequest.Period != "today") {
						t.Fatalf("summary=%v", owner.personalSummaryRequest)
					}
					owner.personalErr = owneridentity.WithErrorCode(status.Error(codes.Unauthenticated, "delegated credential expired"), api.ErrorCodeEnum_ERROR_CODE_ENUM_REAUTHENTICATION_REQUIRED)
					response = httptest.NewRecorder()
					handler.ServeHTTP(response, sessionRequest(http.MethodGet, row.path))
					if response.Code != 401 || !strings.Contains(response.Body.String(), `"code":"REAUTHENTICATION_REQUIRED"`) || response.Header().Get("Set-Cookie") != "" {
						t.Fatalf("reauth=%d %s", response.Code, response.Body.String())
					}
					owner.personalErr = nil
					// A delegated-credential failure does not revoke this CloudIdentity session or the wallet route.
					identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET] = allowFor("actor-1", tenant, api.OwnerEnum_OWNER_ENUM_GATEWAY, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, tenant)
					identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET].Resource = identity.decision.Resource
					identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET].Scope = identity.decision.Scope
					response = httptest.NewRecorder()
					handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
					if response.Code != 200 {
						t.Fatalf("wallet after reauth=%d %s", response.Code, response.Body.String())
					}
				})
			}
		})
	}
}

func TestPersonalGatewayReadRejectsQueriesBeforeCallingOwner(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	for _, row := range []struct {
		base    string
		action  api.AuthorizationActionEnum
		queries []string
	}{
		{"/api/v2/gateway/keys", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYS, []string{"actorId=other", "tenantId=other", "userId=other", "page=1&page=2", "pageSize=20&pageSize=20", "search=a&search=b", "status=active&status=disabled", "sortBy=name&sortBy=id", "sortOrder=asc&sortOrder=desc", "groupId=0&groupId=1", "page=", "page=0", "page=1000001", "pageSize=101", "pageSize=1.5", "page=+1", "page=01", "status=", "status=revoked", "sortBy=", "sortBy=secret", "sortOrder=", "sortOrder=invalid", "groupId=", "groupId=01", "groupId=-1", "groupId=9223372036854775808", "search=" + strings.Repeat("界", 101), "bad=%zz"}},
		{"/api/v2/gateway/keys/key-a", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEY, []string{"userId=other", "period=today"}},
		{"/api/v2/gateway/groups", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYGROUPS, []string{"userId=other", "page=1"}},
		{"/api/v2/gateway/keys/key-a/usage", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYUSAGE, []string{"", "period=", "period=year", "period=today&period=month", "period=today&page=0", "period=week&pageSize=101", "period=month&api_key_id=other"}},
		{"/api/v2/gateway/keys/key-a/usage-summary", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEYUSAGESUMMARY, []string{"", "period=", "period=year", "period=today&period=week", "period=today&page=1", "period=month&userId=other"}},
	} {
		identity := allowedIdentity()
		identity.decision = allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_GATEWAY, row.action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "tenant-1")
		handler := NewServer(client, identity).Handler()
		for _, query := range row.queries {
			t.Run(row.base+"?"+query, func(t *testing.T) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, sessionRequest(http.MethodGet, row.base+"?"+query))
				if response.Code != 400 {
					t.Fatalf("query=%d %s", response.Code, response.Body.String())
				}
			})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, row.base, nil))
		if response.Code != 401 {
			t.Fatalf("anonymous=%d", response.Code)
		}
	}
	if owner.personalKeysRequest != nil || owner.personalKeyRequest != nil || owner.personalGroupRequest != nil || owner.personalUsageRequest != nil || owner.personalSummaryRequest != nil {
		t.Fatal("invalid query reached Gateway owner")
	}
}
