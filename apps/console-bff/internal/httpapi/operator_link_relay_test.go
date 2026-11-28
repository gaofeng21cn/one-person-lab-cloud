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
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

type linkRelayTenantProbe struct {
	api.TenantProductServiceClient
	listReq  *api.ListOperatorAccountsRpcRequest
	listResp *api.OperatorGatewayAccountPage
	setReq   *api.SetLinkRelayEntitlementRpcRequest
	setResp  *api.LinkRelayEntitlementCommand
	setErr   error
	seedReq  *api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest
	seedResp *api.SeedLinkRelayEntitlementsCommand
	seedErr  error
	readReq  *api.ReadCallerLinkRelayEntitlementRpcRequest
	readResp *api.LinkRelayEntitlementReadback
	readErr  error
}

func (p *linkRelayTenantProbe) ListOperatorAccounts(_ context.Context, r *api.ListOperatorAccountsRpcRequest, _ ...grpc.CallOption) (*api.OperatorGatewayAccountPage, error) {
	p.listReq = r
	return p.listResp, nil
}

func (p *linkRelayTenantProbe) SetLinkRelayEntitlement(_ context.Context, r *api.SetLinkRelayEntitlementRpcRequest, _ ...grpc.CallOption) (*api.LinkRelayEntitlementCommand, error) {
	p.setReq = r
	if p.setErr != nil {
		return nil, p.setErr
	}
	if p.setResp != nil {
		return p.setResp, nil
	}
	return &api.LinkRelayEntitlementCommand{
		OperationId:      "link-relay-op-1",
		AccountId:        r.GetTargetActorId(),
		Status:           "succeeded",
		LinkRelayEnabled: r.GetBody().GetEnabled(),
	}, nil
}

func (p *linkRelayTenantProbe) SeedActiveGatewayLinkRelayEntitlements(_ context.Context, r *api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest, _ ...grpc.CallOption) (*api.SeedLinkRelayEntitlementsCommand, error) {
	p.seedReq = r
	if p.seedErr != nil {
		return nil, p.seedErr
	}
	if p.seedResp != nil {
		return p.seedResp, nil
	}
	return &api.SeedLinkRelayEntitlementsCommand{
		OperationId:   "link-relay-seed-op-1",
		Status:        "succeeded",
		ScannedCount:  12,
		SeededCount:   10,
		ExistingCount: 2,
	}, nil
}

func (p *linkRelayTenantProbe) ReadCallerLinkRelayEntitlement(_ context.Context, r *api.ReadCallerLinkRelayEntitlementRpcRequest, _ ...grpc.CallOption) (*api.LinkRelayEntitlementReadback, error) {
	p.readReq = r
	if p.readErr != nil {
		return nil, p.readErr
	}
	if p.readResp != nil {
		return p.readResp, nil
	}
	return &api.LinkRelayEntitlementReadback{
		ActorId:          "1042",
		Email:            "relay@example.test",
		LinkRelayEnabled: true,
		IsBootstrapAdmin: false,
		IsPlatformAdmin:  false,
		Reason:           "operator_requested",
	}, nil
}

func TestOperatorLinkRelayEntitlementWriteAndAccountProjection(t *testing.T) {
	now := timestamppb.New(time.Date(2026, 11, 28, 3, 0, 0, 0, time.UTC))
	tenant := &linkRelayTenantProbe{
		listResp: &api.OperatorGatewayAccountPage{
			Total:    1,
			Page:     1,
			PageSize: 20,
			Items: []*api.OperatorGatewayAccount{
				{
					ActorId:                  "45",
					Email:                    "relay-user@example.test",
					Status:                   "active",
					WorkspacePurchaseEnabled: false,
					PlatformAdminEnabled:     false,
					IsBootstrapAdmin:         false,
					LinkRelayEnabled:         true,
					GatewayUpdatedAt:         now,
				},
			},
		},
	}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", TenantId: ptr("tenant-ignored"), CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			operatorPrivilegeAction:                                                      activationPlatformDecision(operatorPrivilegeAction, api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "op-priv-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS:    activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "", "op-list-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETLINKRELAYENTITLEMENT: activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETLINKRELAYENTITLEMENT, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "45", "op-relay-1"),
		},
	}
	handler := (&Server{identity: identity, tenant: tenant}).Handler()

	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/operator/accounts", nil)
	getReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET accounts status=%d body=%s", getRes.Code, getRes.Body.String())
	}
	var listPayload struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Data.Items) != 1 || listPayload.Data.Items[0]["linkRelayEnabled"] != true {
		t.Fatalf("expected linkRelayEnabled=true in accounts projection, got %#v", listPayload.Data.Items)
	}

	writeReq := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/45/link-relay-entitlement",
		strings.NewReader(`{"confirmationAccountId":"45","enabled":false,"reason":"revoke_relay"}`))
	writeReq.Header.Set("Content-Type", "application/json")
	writeReq.Header.Set("X-CSRF-Token", "csrf-admin")
	writeReq.Header.Set("Idempotency-Key", "relay-idem-1")
	writeReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	writeRes := httptest.NewRecorder()
	handler.ServeHTTP(writeRes, writeReq)
	if writeRes.Code != http.StatusOK {
		t.Fatalf("POST link-relay-entitlement status=%d body=%s", writeRes.Code, writeRes.Body.String())
	}
	if tenant.setReq == nil || tenant.setReq.GetTargetActorId() != "45" || tenant.setReq.GetBody().GetEnabled() {
		t.Fatalf("unexpected SetLinkRelayEntitlement request: %+v", tenant.setReq)
	}
}

func TestOperatorLinkRelaySeedRoute(t *testing.T) {
	tenant := &linkRelayTenantProbe{}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SEEDACTIVEGATEWAYLINKRELAYENTITLEMENTS: activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SEEDACTIVEGATEWAYLINKRELAYENTITLEMENTS, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "", "op-seed-1"),
		},
	}
	handler := (&Server{identity: identity, tenant: tenant}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v2/operator/link-relay/seed-active-users",
		strings.NewReader(`{"reason":"initial_seed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "csrf-admin")
	req.Header.Set("Idempotency-Key", "seed-idem-1")
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("seed route status=%d body=%s", res.Code, res.Body.String())
	}
	var body struct {
		OperationID   string `json:"operationId"`
		Status        string `json:"status"`
		ScannedCount  int32  `json:"scannedCount"`
		SeededCount   int32  `json:"seededCount"`
		ExistingCount int32  `json:"existingCount"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "succeeded" || body.ScannedCount != 12 || body.SeededCount != 10 || body.ExistingCount != 2 {
		t.Fatalf("unexpected seed response: %+v", body)
	}

	// Non-admin caller must be refused.
	identity.decisions = nil
	resDenied := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/operator/link-relay/seed-active-users",
		strings.NewReader(`{"reason":"initial_seed"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-CSRF-Token", "csrf-admin")
	req2.Header.Set("Idempotency-Key", "seed-idem-2")
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	handler.ServeHTTP(resDenied, req2)
	if resDenied.Code != http.StatusForbidden {
		t.Fatalf("non-admin seed status=%d want 403", resDenied.Code)
	}
}

func TestCallerLinkRelayEntitlementSupportsBearerAndSessionWithOriginGuards(t *testing.T) {
	now := time.Date(2026, 11, 28, 3, 5, 0, 0, time.UTC)
	tenant := &linkRelayTenantProbe{
		readResp: &api.LinkRelayEntitlementReadback{
			ActorId:          "1042",
			Email:            "device@example.test",
			LinkRelayEnabled: true,
			IsBootstrapAdmin: false,
			IsPlatformAdmin:  false,
			Reason:           "operator_requested",
			UpdatedAt:        timestamppb.New(now),
		},
	}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "1042", CsrfToken: "csrf-1"},
	}
	handler := (&Server{identity: identity, tenant: tenant}).Handler()

	// 1. Direct non-browser Gateway Bearer token succeeds.
	bearerReq := httptest.NewRequest(http.MethodGet, "https://cloud.test/api/v2/link-relay/entitlement", nil)
	bearerReq.Header.Set("Authorization", "Bearer gw-token-1042")
	bearerRes := httptest.NewRecorder()
	handler.ServeHTTP(bearerRes, bearerReq)
	if bearerRes.Code != http.StatusOK {
		t.Fatalf("bearer readback status=%d body=%s", bearerRes.Code, bearerRes.Body.String())
	}
	if tenant.readReq == nil || tenant.readReq.GetBearerToken() != "gw-token-1042" {
		t.Fatalf("expected bearer_token forwarded, got %+v", tenant.readReq)
	}

	// 2. Rejects cross-site or foreign Origin or mixed Cookie+Bearer or empty Bearer.
	for _, tc := range []struct {
		name string
		prep func(*http.Request)
		code int
	}{
		{"foreign origin on bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer gw-token-1042")
			r.Header.Set("Origin", "https://evil.test")
		}, http.StatusForbidden},
		{"cross-site fetch metadata", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer gw-token-1042")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}, http.StatusForbidden},
		{"mixed cookie and bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer gw-token-1042")
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
		}, http.StatusUnauthorized},
		{"empty bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer ")
		}, http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodGet, "https://cloud.test/api/v2/link-relay/entitlement", nil)
		tc.prep(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("%s status=%d want %d body=%s", tc.name, rec.Code, tc.code, rec.Body.String())
		}
	}

	// 3. Browser session cookie succeeds when Authorization is absent.
	tenant.readReq = nil
	cookieReq := httptest.NewRequest(http.MethodGet, "https://cloud.test/api/v2/link-relay/entitlement", nil)
	cookieReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
	cookieRes := httptest.NewRecorder()
	handler.ServeHTTP(cookieRes, cookieReq)
	if cookieRes.Code != http.StatusOK {
		t.Fatalf("cookie readback status=%d body=%s", cookieRes.Code, cookieRes.Body.String())
	}
	if tenant.readReq == nil || tenant.readReq.GetContext().GetSessionId() == "" || tenant.readReq.GetBearerToken() != "" {
		t.Fatalf("expected session context without bearer token, got %+v", tenant.readReq)
	}

	// 4. Owner Unauthenticated propagates as 401.
	tenant.readErr = status.Error(codes.Unauthenticated, "Gateway token expired")
	unauthReq := httptest.NewRequest(http.MethodGet, "https://cloud.test/api/v2/link-relay/entitlement", nil)
	unauthReq.Header.Set("Authorization", "Bearer expired-token")
	unauthRes := httptest.NewRecorder()
	handler.ServeHTTP(unauthRes, unauthReq)
	if unauthRes.Code != http.StatusUnauthorized {
		t.Fatalf("expired bearer status=%d want 401", unauthRes.Code)
	}
}
