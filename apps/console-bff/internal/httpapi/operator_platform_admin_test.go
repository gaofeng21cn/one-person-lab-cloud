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
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

type platformAdminTenantProbe struct {
	api.TenantProductServiceClient
	listReq  *api.ListOperatorAccountsRpcRequest
	listResp *api.OperatorGatewayAccountPage
	listErr  error
	setReq   *api.SetPlatformAdminRpcRequest
	setResp  *api.PlatformAdminCommand
	setErr   error
}

func (p *platformAdminTenantProbe) ListOperatorAccounts(_ context.Context, r *api.ListOperatorAccountsRpcRequest, _ ...grpc.CallOption) (*api.OperatorGatewayAccountPage, error) {
	p.listReq = r
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.listResp, nil
}

func (p *platformAdminTenantProbe) ListServiceEntitlements(_ context.Context, _ *api.ListServiceEntitlementsRpcRequest, _ ...grpc.CallOption) (*api.ServiceEntitlementList, error) {
	return &api.ServiceEntitlementList{}, nil
}

func (p *platformAdminTenantProbe) SetPlatformAdmin(_ context.Context, r *api.SetPlatformAdminRpcRequest, _ ...grpc.CallOption) (*api.PlatformAdminCommand, error) {
	p.setReq = r
	if p.setErr != nil {
		return nil, p.setErr
	}
	if p.setResp != nil {
		return p.setResp, nil
	}
	return &api.PlatformAdminCommand{
		OperationId:          "platform-admin-op-1",
		AccountId:            r.GetTargetActorId(),
		Status:               "succeeded",
		PlatformAdminEnabled: r.GetBody().GetEnabled(),
		IsBootstrapAdmin:     false,
	}, nil
}

func TestOperatorAccountsGatewayDirectReadAndSearch(t *testing.T) {
	now := timestamppb.New(time.Date(2026, 11, 28, 2, 0, 0, 0, time.UTC))
	tenant := &platformAdminTenantProbe{
		listResp: &api.OperatorGatewayAccountPage{
			Total:    1,
			Page:     2,
			PageSize: 5,
			Items: []*api.OperatorGatewayAccount{
				{
					ActorId:                  "45",
					Email:                    "gatewayonly@example.test",
					Status:                   "active",
					WorkspacePurchaseEnabled: false,
					PlatformAdminEnabled:     false,
					IsBootstrapAdmin:         false,
					WalletAvailable:          true,
					WalletUsdMicros:          1234000,
					UsageAvailable:           false,
					KeyCountAvailable:        true,
					KeyCount:                 3,
					GatewayUpdatedAt:         now,
				},
			},
		},
	}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			operatorPrivilegeAction: activationPlatformDecision(operatorPrivilegeAction, api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "op-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS: activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "", "list-op-1"),
		},
	}
	handler := (&Server{identity: identity, tenant: tenant}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/operator/accounts?page=2&pageSize=5&search=gatewayonly", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if tenant.listReq == nil || tenant.listReq.GetPage() != 2 || tenant.listReq.GetPageSize() != 5 || tenant.listReq.GetSearch() != "gatewayonly" {
		t.Fatalf("unexpected owner request: %+v", tenant.listReq)
	}
	var env struct {
		Source string `json:"source"`
		Status string `json:"status"`
		Data   struct {
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"pageSize"`
			Items    []struct {
				AccountID            string `json:"accountId"`
				ConsoleUserID        string `json:"consoleUserId"`
				Role                 string `json:"role"`
				Sub2APIUserID        string `json:"sub2apiUserId"`
				PlatformAdminEnabled bool   `json:"platformAdminEnabled"`
				IsBootstrapAdmin     bool   `json:"isBootstrapAdmin"`
				Wallet               struct {
					Available bool `json:"available"`
					Data      struct {
						UsdMicros string `json:"usdMicros"`
					} `json:"data"`
				} `json:"wallet"`
				Usage struct {
					Available bool `json:"available"`
				} `json:"usage"`
				KeyCount struct {
					Available bool `json:"available"`
					Data      int  `json:"data"`
				} `json:"keyCount"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Source != "tenant+sub2api" || len(env.Data.Items) != 1 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	got := env.Data.Items[0]
	if got.AccountID != "45" || got.Sub2APIUserID != "45" || got.ConsoleUserID != "" || got.Role != "user" || !got.Wallet.Available || got.Wallet.Data.UsdMicros != "1234000" || got.Usage.Available || !got.KeyCount.Available || got.KeyCount.Data != 3 {
		t.Fatalf("unexpected gateway-only item projection: %+v", got)
	}

	// Nil tenant owner fails closed even without legacy control-plane operator configured.
	unconfigured := (&Server{identity: identity}).Handler()
	unconfRec := httptest.NewRecorder()
	unconfigured.ServeHTTP(unconfRec, req)
	if unconfRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when tenant is nil, got %d body=%s", unconfRec.Code, unconfRec.Body.String())
	}
}

func TestOperatorPlatformAdminWriteRoute(t *testing.T) {
	tenant := &platformAdminTenantProbe{}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", TenantId: proto.String("tenant-1"), CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPLATFORMADMIN: activationPlatformDecision(
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPLATFORMADMIN,
				api.OwnerEnum_OWNER_ENUM_TENANT,
				api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
				"45",
				"admin-auth-1",
			),
		},
	}
	handler := (&Server{identity: identity, tenant: tenant}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/45/platform-admin",
		strings.NewReader(`{"confirmationAccountId":"45","enabled":true,"reason":"operator_requested"}`))
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "csrf-admin")
	req.Header.Set("Idempotency-Key", "admin-key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OperationID          string `json:"operationId"`
		AccountID            string `json:"accountId"`
		Status               string `json:"status"`
		PlatformAdminEnabled bool   `json:"platformAdminEnabled"`
		IsBootstrapAdmin     bool   `json:"isBootstrapAdmin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OperationID != "platform-admin-op-1" || resp.AccountID != "45" || !resp.PlatformAdminEnabled || resp.IsBootstrapAdmin {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if tenant.setReq == nil || tenant.setReq.GetTargetActorId() != "45" || tenant.setReq.GetContext().GetScope().GetPlatform() == nil {
		t.Fatalf("unexpected owner request: %+v", tenant.setReq)
	}

	// FailedPrecondition from owner (e.g. self-revocation / last-admin) maps to 422.
	tenant.setErr = status.Error(codes.FailedPrecondition, "cannot revoke the last platform administrator")
	preReq := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/45/platform-admin",
		strings.NewReader(`{"confirmationAccountId":"45","enabled":false,"reason":"operator_requested"}`))
	preReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	preReq.Header.Set("Content-Type", "application/json")
	preReq.Header.Set("X-CSRF-Token", "csrf-admin")
	preReq.Header.Set("Idempotency-Key", "admin-key-2")
	preRec := httptest.NewRecorder()
	handler.ServeHTTP(preRec, preReq)
	if preRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on FailedPrecondition, got %d body=%s", preRec.Code, preRec.Body.String())
	}
}
