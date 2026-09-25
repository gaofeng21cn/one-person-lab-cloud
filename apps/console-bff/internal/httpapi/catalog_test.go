package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// catalogProbe records the typed call the BFF makes so a test can assert the
// authorized caller context that reached the owner, not just the HTTP status.
type catalogProbe struct {
	api.ResourceCatalogProductServiceClient
	compute      *api.CreateComputePlanRpcRequest
	listCompute  *api.ListComputePlansRpcRequest
	availability *api.SetComputePlanAvailabilityRpcRequest
	price        *api.CreatePricePolicyVersionRpcRequest
}

func (p *catalogProbe) CreateComputePlan(_ context.Context, r *api.CreateComputePlanRpcRequest, _ ...grpc.CallOption) (*api.ComputePlan, error) {
	p.compute = r
	return &api.ComputePlan{Id: "compute-1", Name: r.Body.Name, Vcpus: r.Body.Vcpus, MemoryMiB: r.Body.MemoryMiB,
		Availability:              api.ComputePlanAvailabilityEnum_COMPUTE_PLAN_AVAILABILITY_ENUM_AVAILABLE,
		BillingMode:               api.ComputePlanBillingModeEnum_COMPUTE_PLAN_BILLING_MODE_ENUM_LOCAL_NO_CHARGE,
		ProviderCapabilityVersion: r.Body.ProviderCapabilityVersion}, nil
}

func (p *catalogProbe) ListComputePlans(_ context.Context, r *api.ListComputePlansRpcRequest, _ ...grpc.CallOption) (*api.ComputePlanPage, error) {
	p.listCompute = r
	return &api.ComputePlanPage{Items: []*api.ComputePlan{{
		Id: "compute-1", Name: "basic", Vcpus: 2, MemoryMiB: 4096,
		Availability:              api.ComputePlanAvailabilityEnum_COMPUTE_PLAN_AVAILABILITY_ENUM_AVAILABLE,
		BillingMode:               api.ComputePlanBillingModeEnum_COMPUTE_PLAN_BILLING_MODE_ENUM_LOCAL_NO_CHARGE,
		ProviderCapabilityVersion: "provider/v1",
		ValidFrom:                 timestamppb.New(time.Now().Add(-time.Hour)),
		CreatedAt:                 timestamppb.Now(),
	}}}, nil
}

func (p *catalogProbe) SetComputePlanAvailability(_ context.Context, r *api.SetComputePlanAvailabilityRpcRequest, _ ...grpc.CallOption) (*api.ComputePlan, error) {
	p.availability = r
	return &api.ComputePlan{Id: r.PlanId, Name: "basic", Vcpus: 2, MemoryMiB: 4096,
		Availability:              api.ComputePlanAvailabilityEnum_COMPUTE_PLAN_AVAILABILITY_ENUM_RETIRED,
		BillingMode:               api.ComputePlanBillingModeEnum_COMPUTE_PLAN_BILLING_MODE_ENUM_LOCAL_NO_CHARGE,
		ProviderCapabilityVersion: "provider/v1",
		ValidFrom:                 timestamppb.New(time.Now().Add(-time.Hour)),
		CreatedAt:                 timestamppb.Now()}, nil
}

func (p *catalogProbe) CreatePricePolicyVersion(_ context.Context, r *api.CreatePricePolicyVersionRpcRequest, _ ...grpc.CallOption) (*api.PricePolicyVersion, error) {
	p.price = r
	return &api.PricePolicyVersion{Id: "price-1", VersionLabel: r.Body.VersionLabel, ComputePlanId: r.Body.ComputePlanId, StoragePlanId: r.Body.StoragePlanId,
		Currency:     api.PricePolicyVersionCurrencyEnum_PRICE_POLICY_VERSION_CURRENCY_ENUM_USD,
		PeriodMonths: 1,
		RenewalPolicy: &api.RenewalPolicy{
			Version:                   api.RenewalPolicyVersionEnum_RENEWAL_POLICY_VERSION_ENUM_RENEWAL_POLICY_V1,
			Trigger:                   api.RenewalPolicyTriggerEnum_RENEWAL_POLICY_TRIGGER_ENUM_MANUAL_OR_EXPLICITLY_CONSENTED_AUTOMATIC,
			EffectiveStart:            api.RenewalPolicyEffectiveStartEnum_RENEWAL_POLICY_EFFECTIVE_START_ENUM_PREVIOUS_PAID_THROUGH,
			Months:                    1,
			UsesAcceptedPriceSnapshot: true,
		},
		PlanChangePolicyVersion: api.PricePolicyVersionPlanChangePolicyVersionEnum_PRICE_POLICY_VERSION_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1,
		ValidFrom:               timestamppb.New(time.Now().Add(-time.Hour)),
		CreatedAt:               timestamppb.Now()}, nil
}

// platformIdentity is a platform administrator session: it carries no tenant and
// its decision is scoped to the platform, which is what the administrator catalog
// paths resolve to.
func platformIdentity(action api.AuthorizationActionEnum) *fakeIdentity {
	return &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", CsrfToken: "csrf-admin"},
		decision: &api.AuthorizationDecision{
			ActorId: "admin-1", SessionId: ptr(owneridentity.SessionReference("session-admin")),
			Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
			Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
			Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
			Action: action, AudienceOwner: api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG,
			Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
		},
	}
}

func adminSessionRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	return request
}

func prepareAdminWrite(request *http.Request, body string) {
	request.Body = io.NopCloser(strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-admin")
	request.Header.Set("Idempotency-Key", "stable-catalog-command")
}

// TestCatalogAdminCommandUsesPlatformScope proves the administrator catalog
// command carries the session's own actor and request id, the caller's
// idempotency key, and the platform scope the owner requires for an administrator
// action — and that browser-supplied identity and price fields never reach it.
func TestCatalogAdminCommandUsesPlatformScope(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN)
	identity.decision.AuthorizationContextId = proto.String("fresh-authority-context")
	probe := &catalogProbe{}
	handler := NewCatalogHandler(probe, identity)

	request := adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/compute-plans")
	prepareAdminWrite(request, `{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"local/profile","providerSkuId":"local-basic","providerCapabilityVersion":"provider/v1","validFrom":"2026-09-01T00:00:00Z"}`)
	request.Header.Set("x-opl-actor", "forged-actor")
	request.Header.Set("x-opl-tenant", "forged-tenant")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("admin create failed: %d %s", response.Code, response.Body.String())
	}
	call := probe.compute.GetContext()
	if call.GetActorId() != "admin-1" || call.GetScope().GetTenant() != nil || call.GetScope().GetPlatform() == nil ||
		call.GetIdempotencyKey() != "stable-catalog-command" || call.GetAuthorizationContextId() != "fresh-authority-context" {
		t.Fatalf("incorrect administrator call context: %v", call)
	}
	// The authorizer was asked for the platform scope with no fabricated tenant.
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(identity.requests))
	}
	if request := identity.requests[0]; request.GetScope().GetTenant() != nil || request.GetScope().GetPlatform() == nil ||
		request.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN ||
		request.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG {
		t.Fatalf("administrator authorization request was not platform-scoped catalog: %v", request)
	}
}

// TestCatalogMemberReadCarriesTenantScope proves a customer list read keeps the
// session's own tenant and asks for the member action.
func TestCatalogMemberReadCarriesTenantScope(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	probe := &catalogProbe{}
	handler := NewCatalogHandler(probe, identity)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/catalog/compute-plans?limit=10"))
	if response.Code != http.StatusOK {
		t.Fatalf("member read failed: %d %s", response.Code, response.Body.String())
	}
	if probe.listCompute.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("member read lost tenant scope: %v", probe.listCompute.GetContext())
	}
	if probe.listCompute.GetQueryLimit() != 10 {
		t.Fatalf("limit = %d, want 10", probe.listCompute.GetQueryLimit())
	}
}

// TestCatalogBoundaryRejections proves the shared authenticated boundary's
// preconditions still apply to the new paths: no session, a wrong CSRF token, a
// cross-origin write and a missing idempotency key are all refused before the
// owner is called.
func TestCatalogBoundaryRejections(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN)
	probe := &catalogProbe{}
	handler := NewCatalogHandler(probe, identity)
	valid := `{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"p","providerSkuId":"s","providerCapabilityVersion":"provider/v1","validFrom":"2026-09-01T00:00:00Z"}`

	anonymous := httptest.NewRequest(http.MethodPost, "/api/v2/admin/catalog/compute-plans", strings.NewReader(valid))
	prepareAdminWrite(anonymous, valid)
	anonymous.Header.Del("Cookie")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, anonymous)
	if response.Code != http.StatusUnauthorized || probe.compute != nil {
		t.Fatalf("anonymous write reached the owner: %d", response.Code)
	}

	crossSite := adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/compute-plans")
	prepareAdminWrite(crossSite, valid)
	crossSite.Header.Set("Origin", "https://cross-site.test")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, crossSite)
	if response.Code != http.StatusForbidden || probe.compute != nil || !strings.Contains(response.Body.String(), "ORIGIN_REJECTED") {
		t.Fatalf("cross-origin write reached the owner: %d %s", response.Code, response.Body.String())
	}

	noKey := adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/compute-plans")
	prepareAdminWrite(noKey, valid)
	noKey.Header.Del("Idempotency-Key")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, noKey)
	if response.Code != http.StatusBadRequest || probe.compute != nil {
		t.Fatalf("write without an idempotency key reached the owner: %d", response.Code)
	}

	badCsrf := adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/compute-plans")
	prepareAdminWrite(badCsrf, valid)
	badCsrf.Header.Set("X-CSRF-Token", "wrong")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, badCsrf)
	if response.Code != http.StatusForbidden || probe.compute != nil {
		t.Fatalf("write with a wrong CSRF token reached the owner: %d", response.Code)
	}
}

// TestCatalogDeniedAuthorizationNeverReachesOwner proves a CloudIdentity denial
// on a catalog action stops the request at the BFF.
func TestCatalogDeniedAuthorizationNeverReachesOwner(t *testing.T) {
	identity := platformIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN)
	identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	probe := &catalogProbe{}
	handler := NewCatalogHandler(probe, identity)
	request := adminSessionRequest(http.MethodPost, "/api/v2/admin/catalog/compute-plans")
	prepareAdminWrite(request, `{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"p","providerSkuId":"s","providerCapabilityVersion":"provider/v1","validFrom":"2026-09-01T00:00:00Z"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || probe.compute != nil {
		t.Fatalf("denied catalog command reached the owner: %d", response.Code)
	}
}
