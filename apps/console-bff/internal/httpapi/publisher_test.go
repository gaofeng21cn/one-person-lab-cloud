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
	handler := NewPublisherHandler(probe, nil, identity)
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
	request *api.ListCapabilityVersionsRpcRequest
}

func (p *capabilityListProbe) ListCapabilityVersions(_ context.Context, r *api.ListCapabilityVersionsRpcRequest, _ ...grpc.CallOption) (*api.CapabilityVersionPage, error) {
	p.request = r
	return &api.CapabilityVersionPage{}, nil
}

type publisherOwnerReader struct {
	*fakeReader
	capability api.CapabilityProductServiceClient
	build      api.BuildProductServiceClient
	runtime    api.RuntimeControlProductServiceClient
	gateway    api.GatewayProductServiceClient
}

func (r publisherOwnerReader) PublisherClients() (api.CapabilityProductServiceClient, api.BuildProductServiceClient) {
	return r.capability, r.build
}
func (r publisherOwnerReader) RuntimeControlClient() api.RuntimeControlProductServiceClient {
	return r.runtime
}
func (r publisherOwnerReader) GatewayClient() api.GatewayProductServiceClient { return r.gateway }

type runtimeVersionListProbe struct {
	api.RuntimeControlProductServiceClient
	request *api.ListRuntimeVersionsRpcRequest
}

func (p *runtimeVersionListProbe) ListRuntimeVersions(_ context.Context, r *api.ListRuntimeVersionsRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	p.request = r
	return &api.RuntimeVersionPage{Items: []*api.RuntimeVersion{{Id: "runtime-1", VersionLabel: "v1", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED}}}, nil
}

type buildCreateProbe struct {
	api.BuildProductServiceClient
	request *api.CreateBuildRpcRequest
}

func (p *buildCreateProbe) CreateBuild(_ context.Context, r *api.CreateBuildRpcRequest, _ ...grpc.CallOption) (*api.BuildJob, error) {
	p.request = r
	return &api.BuildJob{Id: "build-1", PackageVersionId: r.Body.PackageVersionId, RuntimeVersionId: r.Body.RuntimeVersionId, WebuiVersionId: r.Body.WebuiVersionId, Status: api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_QUEUED, Stage: "queued"}, nil
}

func TestRuntimeVersionCatalogForwardsCursorAndLimitToOwner(t *testing.T) {
	identity := allowedIdentity()
	runtimeDecision := allowFor(
		"actor-1",
		"tenant-1",
		api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG,
		"",
	)
	runtimeDecision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS] = runtimeDecision
	probe := &runtimeVersionListProbe{}
	reader := publisherOwnerReader{fakeReader: resolvedReader(), runtime: probe}
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/catalog/runtime-versions?cursor=r1&limit=7"))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "runtime-1") || probe.request == nil || probe.request.GetQueryCursor() != "r1" || probe.request.GetQueryLimit() != 7 {
		t.Fatalf("runtime owner request=%v body=%s", probe.request, response.Body.String())
	}
}

func TestCreateBuildForwardsRuntimeVersionID(t *testing.T) {
	identity := allowedIdentity()
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD] = allowFor(
		"actor-1",
		"tenant-1",
		api.OwnerEnum_OWNER_ENUM_BUILD,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION,
		"package-version-1",
	)
	probe := &buildCreateProbe{}
	reader := publisherOwnerReader{fakeReader: resolvedReader(), build: probe}
	request := sessionRequest(http.MethodPost, "/api/v2/builds")
	request.Body = io.NopCloser(strings.NewReader(`{"packageVersionId":"package-version-1","runtimeVersionId":"runtime-1","webuiVersionId":"webui-1"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "build-command-1")
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.request == nil || probe.request.Body.GetPackageVersionId() != "package-version-1" || probe.request.Body.GetRuntimeVersionId() != "runtime-1" || probe.request.Body.GetWebuiVersionId() != "webui-1" {
		t.Fatalf("build owner request=%v", probe.request)
	}
}

type gatewayWalletProbe struct {
	api.GatewayProductServiceClient
	called       bool
	modelsCalled bool
	modelRequest *api.ListModelsRpcRequest
}

func (p *gatewayWalletProbe) ListModels(_ context.Context, r *api.ListModelsRpcRequest, _ ...grpc.CallOption) (*api.ModelPage, error) {
	p.modelsCalled = true
	p.modelRequest = r
	return &api.ModelPage{}, nil
}

func (p *gatewayWalletProbe) GetWallet(_ context.Context, r *api.GetWalletRpcRequest, _ ...grpc.CallOption) (*api.Wallet, error) {
	p.called = r.GetContext() != nil
	return &api.Wallet{Source: api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY, Status: api.WalletStatusEnum_WALLET_STATUS_ENUM_AVAILABLE, BalanceUsdMicros: 123456, Currency: api.WalletCurrencyEnum_WALLET_CURRENCY_ENUM_USD}, nil
}

func TestCapabilityVersionListPassesReadyFilterToOwner(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCAPABILITYVERSIONS
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION}
	probe := &capabilityListProbe{}
	reader := publisherOwnerReader{fakeReader: resolvedReader(), capability: probe}
	response := httptest.NewRecorder()
	request := sessionRequest(http.MethodGet, "/api/v2/capability-versions?status=ready&cursor=c1&limit=10")
	NewServer(reader, identity).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.request == nil || probe.request.GetQueryStatus() != api.ListCapabilityVersionsRpcRequestStatusEnum_LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_READY || probe.request.GetQueryCursor() != "c1" || probe.request.GetQueryLimit() != 10 {
		t.Fatalf("owner request=%v", probe.request)
	}
}

func TestModelCatalogRouteUsesGatewayOwner(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMODELS
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	probe := &gatewayWalletProbe{}
	reader := publisherOwnerReader{fakeReader: resolvedReader(), gateway: probe}
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/catalog/models?cursor=m1&limit=5"))
	if response.Code != http.StatusOK || !probe.modelsCalled || probe.modelRequest.GetQueryCursor() != "m1" || probe.modelRequest.GetQueryLimit() != 5 {
		t.Fatalf("model catalog response status=%d called=%v request=%v body=%s", response.Code, probe.modelsCalled, probe.modelRequest, response.Body.String())
	}
}

func TestWalletRouteUsesGatewayOwnerAndFailsClosedWhenMissing(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_GATEWAY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String("tenant-1")}
	probe := &gatewayWalletProbe{}
	reader := publisherOwnerReader{fakeReader: resolvedReader(), gateway: probe}
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
	if response.Code != http.StatusOK || !probe.called || !strings.Contains(response.Body.String(), `"balanceUSDMicros":"123456"`) {
		t.Fatalf("wallet response status=%d called=%v body=%s", response.Code, probe.called, response.Body.String())
	}

	response = httptest.NewRecorder()
	NewServer(publisherOwnerReader{fakeReader: resolvedReader()}, identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/wallet"))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "DEPENDENCY_UNAVAILABLE") {
		t.Fatalf("missing gateway was not fail-closed: status=%d body=%s", response.Code, response.Body.String())
	}
}
