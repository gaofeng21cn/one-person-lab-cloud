package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type publisherProbe struct {
	api.CapabilityProductServiceClient
	received *api.CreatePackageRpcRequest
}

func (p *publisherProbe) CreatePackage(_ context.Context, r *api.CreatePackageRpcRequest, _ ...grpc.CallOption) (*api.Package, error) {
	p.received = r
	return &api.Package{Id: "pkg-1", NamespaceId: r.Body.NamespaceId, Name: r.Body.Name, Visibility: api.PackageVisibilityEnum_PACKAGE_VISIBILITY_ENUM_PRIVATE, Status: api.PackageStatusEnum_PACKAGE_STATUS_ENUM_ACTIVE}, nil
}

func TestPublisherCommandsPreserveOnlyAuthenticatedContext(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPACKAGE
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE}
	identity.decision.AuthorizationContextId = proto.String("fresh-authority-context")
	probe := &publisherProbe{}
	handler := NewPublisherHandler(probe, nil, nil, identity)
	request := sessionRequest("POST", "/api/v2/packages")
	request.Body = http.NoBody
	prepare := func(body string) {
		request.Body = io.NopCloser(strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", "csrf-1")
		request.Header.Set("Idempotency-Key", "stable-command")
		request.Header.Set("x-opl-actor", "forged-actor")
		request.Header.Set("x-opl-tenant", "forged-tenant")
	}
	prepare(`{"namespaceId":"ns-1","name":"agent","description":""}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("write failed: %d %s", response.Code, response.Body.String())
	}
	call := probe.received.Context
	if call.ActorId != "actor-1" || call.GetScope().GetTenant().TenantId != "tenant-1" || call.GetSessionId() != owneridentity.SessionReference("session-1") || call.IdempotencyKey != "stable-command" || call.AuthorizationContextId != "fresh-authority-context" {
		t.Fatalf("incorrect propagated caller: %v", call)
	}
	probe.received = nil
	prepare(`{"namespaceId":"ns-1","name":"agent","actorId":"forged"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 400 || probe.received != nil {
		t.Fatal("browser-supplied identity reached owner")
	}
	prepare(`{"namespaceId":"ns-1","name":"agent"}`)
	request.Header.Set("Origin", "https://cross-site.test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || probe.received != nil || !strings.Contains(response.Body.String(), "ORIGIN_REJECTED") {
		t.Fatal("cross-origin write reached owner")
	}
}

type capabilityListProbe struct {
	api.CapabilityProductServiceClient
	received *api.ListCapabilityVersionsRpcRequest
}

func (p *capabilityListProbe) ListCapabilityVersions(_ context.Context, r *api.ListCapabilityVersionsRpcRequest, _ ...grpc.CallOption) (*api.CapabilityVersionPage, error) {
	p.received = r
	return &api.CapabilityVersionPage{}, nil
}
func TestCapabilityVersionListPreservesFilters(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCAPABILITYVERSIONS
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE}
	probe := &capabilityListProbe{}
	handler := NewPublisherHandler(probe, nil, nil, identity)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest("GET", "/api/v2/capability-versions?packageId=package-a&status=ready&cursor=version-a&limit=7"))
	if response.Code != 200 {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	request := probe.received
	if request.GetQueryPackageId() != "package-a" || request.GetQueryCursor() != "version-a" || request.GetQueryLimit() != 7 || request.GetQueryStatus() != api.ListCapabilityVersionsRpcRequestStatusEnum_LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_READY {
		t.Fatalf("owner query=%v", request)
	}
	probe.received = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest("GET", "/api/v2/capability-versions?status=other"))
	if response.Code != 400 || probe.received != nil {
		t.Fatalf("invalid status=%d, query=%v", response.Code, probe.received)
	}
}

type gatewayReader struct {
	*fakeReader
	capability api.CapabilityProductServiceClient
	gateway    api.GatewayProductServiceClient
}

func (r gatewayReader) PublisherClients() (api.CapabilityProductServiceClient, api.BuildProductServiceClient) {
	return r.capability, nil
}
func (r gatewayReader) GatewayClient() api.GatewayProductServiceClient { return r.gateway }

type gatewayProbe struct {
	api.GatewayProductServiceClient
	walletCalled bool
	modelsCalled bool
	modelRequest *api.ListModelsRpcRequest
}

func (p *gatewayProbe) ListModels(_ context.Context, r *api.ListModelsRpcRequest, _ ...grpc.CallOption) (*api.ModelPage, error) {
	p.modelsCalled = true
	p.modelRequest = r
	return &api.ModelPage{}, nil
}

func (p *gatewayProbe) GetWallet(_ context.Context, r *api.GetWalletRpcRequest, _ ...grpc.CallOption) (*api.Wallet, error) {
	p.walletCalled = r.GetContext() != nil
	return &api.Wallet{Source: api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY, Status: api.WalletStatusEnum_WALLET_STATUS_ENUM_AVAILABLE, BalanceUsdMicros: 123456, Currency: api.WalletCurrencyEnum_WALLET_CURRENCY_ENUM_USD}, nil
}

func TestModelCatalogRouteUsesGatewayOwner(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMODELS
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	probe := &gatewayProbe{}
	reader := gatewayReader{fakeReader: resolvedReader(), gateway: probe}
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/catalog/models?cursor=m1&limit=5"))
	if response.Code != http.StatusOK || !probe.modelsCalled || probe.modelRequest.GetQueryCursor() != "m1" || probe.modelRequest.GetQueryLimit() != 5 {
		t.Fatalf("model catalog status=%d called=%v request=%v body=%s", response.Code, probe.modelsCalled, probe.modelRequest, response.Body.String())
	}
}

func TestWalletRouteUsesGatewayOwnerAndFailsClosedWhenMissing(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String("tenant-1")}
	probe := &gatewayProbe{}
	reader := gatewayReader{fakeReader: resolvedReader(), gateway: probe}
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
	if response.Code != http.StatusOK || !probe.walletCalled || !strings.Contains(response.Body.String(), `"balanceUSDMicros":"123456"`) {
		t.Fatalf("wallet status=%d called=%v body=%s", response.Code, probe.walletCalled, response.Body.String())
	}

	response = httptest.NewRecorder()
	NewServer(gatewayReader{fakeReader: resolvedReader()}, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatalf("missing gateway not fail-closed: status=%d body=%s", response.Code, response.Body.String())
	}
}
