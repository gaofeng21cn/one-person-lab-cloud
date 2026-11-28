package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
)

type serviceEntitlementTenantProbe struct {
	api.TenantProductServiceClient
	grantReq   *api.IssueServiceGrantRpcRequest
	grantResp  *api.ServiceGrantResponse
	grantErr   error
	jwksResp   *api.ServiceGrantJWKSResponse
	jwksErr    error
	setReq     *api.SetServiceEntitlementRpcRequest
	setResp    *api.ServiceEntitlementCommand
	setErr     error
	listItems  []*api.ServiceEntitlementReadback
	eligItems  []*api.WorkspacePurchaseEligibilityReadback
}

func (p *serviceEntitlementTenantProbe) IssueServiceGrant(_ context.Context, r *api.IssueServiceGrantRpcRequest, _ ...grpc.CallOption) (*api.ServiceGrantResponse, error) {
	p.grantReq = r
	if p.grantErr != nil {
		return nil, p.grantErr
	}
	if p.grantResp != nil {
		return p.grantResp, nil
	}
	limits, _ := structpb.NewStruct(map[string]any{"max_sites": 10})
	return &api.ServiceGrantResponse{
		ActorId:    "1042",
		Email:      "user@example.test",
		Service:    r.GetBody().GetService(),
		Enabled:    true,
		Origin:     "https://netfleet.example.test",
		GrantToken: "hdr.payload.sig",
		ExpiresAt:  timestamppb.New(time.Unix(1795834920, 0).UTC()),
		Role:       "user",
		Limits:     limits,
	}, nil
}

func (p *serviceEntitlementTenantProbe) GetServiceGrantJWKS(_ context.Context, _ *api.GetServiceGrantJWKSRpcRequest, _ ...grpc.CallOption) (*api.ServiceGrantJWKSResponse, error) {
	if p.jwksErr != nil {
		return nil, p.jwksErr
	}
	if p.jwksResp != nil {
		return p.jwksResp, nil
	}
	return &api.ServiceGrantJWKSResponse{
		Keys: []*api.ServiceGrantJWK{{Kty: "OKP", Crv: "Ed25519", Alg: "EdDSA", Use: "sig", Kid: "k-1", X: "abc"}},
	}, nil
}

func (p *serviceEntitlementTenantProbe) SetServiceEntitlement(_ context.Context, r *api.SetServiceEntitlementRpcRequest, _ ...grpc.CallOption) (*api.ServiceEntitlementCommand, error) {
	p.setReq = r
	if p.setErr != nil {
		return nil, p.setErr
	}
	if p.setResp != nil {
		return p.setResp, nil
	}
	return &api.ServiceEntitlementCommand{
		OperationId:      "service-entitlement-1",
		AccountId:        r.GetTargetActorId(),
		ServiceId:        r.GetServiceId(),
		Status:           "succeeded",
		Enabled:          r.GetBody().GetEnabled(),
		EffectiveEnabled: r.GetBody().GetEnabled(),
		Role:             "user",
		Origin:           "https://netfleet.example.test",
	}, nil
}

func (p *serviceEntitlementTenantProbe) ListWorkspacePurchaseEligibility(_ context.Context, r *api.ListWorkspacePurchaseEligibilityRpcRequest, _ ...grpc.CallOption) (*api.WorkspacePurchaseEligibilityList, error) {
	if p.eligItems != nil {
		return &api.WorkspacePurchaseEligibilityList{Items: p.eligItems}, nil
	}
	items := make([]*api.WorkspacePurchaseEligibilityReadback, 0, len(r.GetActorIds()))
	for _, id := range r.GetActorIds() {
		items = append(items, &api.WorkspacePurchaseEligibilityReadback{ActorId: id, AccountId: id, WorkspacePurchaseEnabled: true})
	}
	return &api.WorkspacePurchaseEligibilityList{Items: items}, nil
}

func (p *serviceEntitlementTenantProbe) ListServiceEntitlements(_ context.Context, _ *api.ListServiceEntitlementsRpcRequest, _ ...grpc.CallOption) (*api.ServiceEntitlementList, error) {
	return &api.ServiceEntitlementList{Items: p.listItems}, nil
}

func (p *serviceEntitlementTenantProbe) ListOperatorAccounts(_ context.Context, r *api.ListOperatorAccountsRpcRequest, _ ...grpc.CallOption) (*api.OperatorGatewayAccountPage, error) {
	return &api.OperatorGatewayAccountPage{
		Total:    1,
		Page:     r.GetPage(),
		PageSize: r.GetPageSize(),
		Items: []*api.OperatorGatewayAccount{
			{
				ActorId: "42",
				Email:   "u42@example.test",
				Status:  "active",
			},
		},
	}, nil
}

func TestClientServiceGrantAndJWKSRoutes(t *testing.T) {
	probe := &serviceEntitlementTenantProbe{}
	handler := (&Server{identity: &fakeIdentity{}, tenant: probe}).Handler()

	// 1. Password bootstrap succeeds without browser CSRF or session cookies.
	req := httptest.NewRequest(http.MethodPost, "/api/v2/client/service-grant", strings.NewReader(`{"email":"User@Example.test","password":"secret","service":"netfleet_cloud"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cc=%q body=%s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	var grantResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &grantResp); err != nil {
		t.Fatal(err)
	}
	if grantResp["actor_id"] != "1042" || grantResp["origin"] != "https://netfleet.example.test" || grantResp["grant_token"] != "hdr.payload.sig" {
		t.Fatalf("unexpected service-grant response: %+v", grantResp)
	}

	// 2. Bearer token auth is accepted on /api/v2/client/service-grant (and still blocked on other BFF routes).
	bearerReq := httptest.NewRequest(http.MethodPost, "/api/v2/client/service-grant", strings.NewReader(`{"service":"netfleet_cloud","scope":"console:login"}`))
	bearerReq.Header.Set("Content-Type", "application/json")
	bearerReq.Header.Set("Authorization", "Bearer gw-token-123")
	bearerRec := httptest.NewRecorder()
	handler.ServeHTTP(bearerRec, bearerReq)
	if bearerRec.Code != http.StatusOK || probe.grantReq.GetBearerToken() != "gw-token-123" || probe.grantReq.GetBody().GetScope() != "console:login" {
		t.Fatalf("bearer bootstrap failed: status=%d req=%+v body=%s", bearerRec.Code, probe.grantReq, bearerRec.Body.String())
	}

	otherBearerReq := httptest.NewRequest(http.MethodGet, "/api/v2/tenant", nil)
	otherBearerReq.Header.Set("Authorization", "Bearer gw-token-123")
	otherBearerRec := httptest.NewRecorder()
	handler.ServeHTTP(otherBearerRec, otherBearerReq)
	if otherBearerRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bearer on non-exempt route, got %d", otherBearerRec.Code)
	}

	// 3. 2FA challenge returns {requires_2fa: true} with no origin/token.
	probe.grantResp = &api.ServiceGrantResponse{Requires_2Fa: true}
	req2fa := httptest.NewRequest(http.MethodPost, "/api/v2/client/service-grant", strings.NewReader(`{"email":"u@example.test","password":"secret","service":"netfleet_cloud"}`))
	req2fa.Header.Set("Content-Type", "application/json")
	rec2fa := httptest.NewRecorder()
	handler.ServeHTTP(rec2fa, req2fa)
	if rec2fa.Code != http.StatusOK || strings.Contains(rec2fa.Body.String(), "grant_token") || !strings.Contains(rec2fa.Body.String(), `"requires_2fa":true`) {
		t.Fatalf("2FA response invalid: code=%d body=%s", rec2fa.Code, rec2fa.Body.String())
	}
	probe.grantResp = nil

	// 4. Denied entitlement maps to 403 service_not_entitled and leaks no origin/grant_token.
	probe.grantErr = status.Error(codes.PermissionDenied, "service_not_entitled")
	denyReq := httptest.NewRequest(http.MethodPost, "/api/v2/client/service-grant", strings.NewReader(`{"email":"blocked@example.test","password":"secret","service":"netfleet_cloud"}`))
	denyReq.Header.Set("Content-Type", "application/json")
	denyRec := httptest.NewRecorder()
	handler.ServeHTTP(denyRec, denyReq)
	if denyRec.Code != http.StatusForbidden || !strings.Contains(denyRec.Body.String(), `"service_not_entitled"`) || strings.Contains(denyRec.Body.String(), `"origin"`) || strings.Contains(denyRec.Body.String(), `"grant_token"`) {
		t.Fatalf("expected 403 service_not_entitled without leaks, got status=%d body=%s", denyRec.Code, denyRec.Body.String())
	}
	probe.grantErr = nil

	// 5. Public JWKS endpoint returns public Ed25519 keys.
	jwksRec := httptest.NewRecorder()
	handler.ServeHTTP(jwksRec, httptest.NewRequest(http.MethodGet, "/.well-known/opl-grant-jwks.json", nil))
	if jwksRec.Code != http.StatusOK || !strings.Contains(jwksRec.Body.String(), `"Ed25519"`) {
		t.Fatalf("JWKS failed: status=%d body=%s", jwksRec.Code, jwksRec.Body.String())
	}

	// 6. Rate limiter blocks excessive requests per email.
	for i := 0; i < serviceGrantMaxPerEmail+1; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/v2/client/service-grant", strings.NewReader(`{"email":"flood@example.test","password":"secret","service":"netfleet_cloud"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if i == serviceGrantMaxPerEmail && w.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 rate_limited at iteration %d, got %d (%s)", i, w.Code, w.Body.String())
		}
	}
}

func TestOperatorServiceEntitlementRouteAndProjection(t *testing.T) {
	const token = "operator-se-proj-test-token-0001"
	limitsPB, _ := structpb.NewStruct(map[string]any{"max_sites": 15})
	probe := &serviceEntitlementTenantProbe{
		listItems: []*api.ServiceEntitlementReadback{
			{
				ActorId:             "42",
				ServiceId:           "netfleet_cloud",
				AdmissionMode:       "active_gateway_default_with_overrides",
				HasExplicitOverride: true,
				Enabled:             true,
				EffectiveEnabled:    true,
				Role:                "user",
				Origin:              "https://netfleet.example.test",
				Limits:              limitsPB,
			},
		},
	}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", TenantId: proto.String("tenant-1"), CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			operatorPrivilegeAction: activationPlatformDecision(operatorPrivilegeAction, api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "op-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS: activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "", "list-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETSERVICEENTITLEMENT: activationPlatformDecision(
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETSERVICEENTITLEMENT,
				api.OwnerEnum_OWNER_ENUM_TENANT,
				api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
				"42",
				"se-auth-1",
			),
		},
	}

	readers, err := clients.Dial(clients.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()

	srv := NewServer(readers, identity)
	srv.tenant = probe

	postReq := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/42/service-entitlements/netfleet_cloud",
		strings.NewReader(`{"confirmationAccountId":"42","enabled":false,"reason":"operator_requested"}`))
	postReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("X-CSRF-Token", "csrf-admin")
	postReq.Header.Set("Idempotency-Key", "se-key-1")
	postRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK || probe.setReq.GetServiceId() != "netfleet_cloud" || probe.setReq.GetBody().GetEnabled() {
		t.Fatalf("operator SetServiceEntitlement failed: code=%d req=%+v body=%s", postRec.Code, probe.setReq, postRec.Body.String())
	}

	accountsReq := httptest.NewRequest(http.MethodGet, "/api/v2/operator/accounts?page=1&pageSize=20", nil)
	accountsReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	accountsRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(accountsRec, accountsReq)
	if accountsRec.Code != http.StatusOK || !strings.Contains(accountsRec.Body.String(), `"serviceEntitlements"`) || !strings.Contains(accountsRec.Body.String(), `"netfleet_cloud"`) {
		t.Fatalf("operator accounts missing serviceEntitlements projection: code=%d body=%s", accountsRec.Code, accountsRec.Body.String())
	}
}
