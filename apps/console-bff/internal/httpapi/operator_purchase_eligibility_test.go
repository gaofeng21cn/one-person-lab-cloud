package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
)

type eligibilityTenantProbe struct {
	api.TenantProductServiceClient
	setReq  *api.SetWorkspacePurchaseEligibilityRpcRequest
	setResp *api.WorkspacePurchaseEligibilityCommand
	setErr  error
	listReq *api.ListWorkspacePurchaseEligibilityRpcRequest
	listMap map[string]bool
	listErr error
}

func (p *eligibilityTenantProbe) SetWorkspacePurchaseEligibility(_ context.Context, r *api.SetWorkspacePurchaseEligibilityRpcRequest, _ ...grpc.CallOption) (*api.WorkspacePurchaseEligibilityCommand, error) {
	p.setReq = r
	if p.setErr != nil {
		return nil, p.setErr
	}
	if p.setResp != nil {
		return p.setResp, nil
	}
	return &api.WorkspacePurchaseEligibilityCommand{
		OperationId:              "workspace-purchase-eligibility-1",
		AccountId:                r.GetTargetActorId(),
		Status:                   "succeeded",
		WorkspacePurchaseEnabled: r.GetBody().GetEnabled(),
	}, nil
}

func (p *eligibilityTenantProbe) ListWorkspacePurchaseEligibility(_ context.Context, r *api.ListWorkspacePurchaseEligibilityRpcRequest, _ ...grpc.CallOption) (*api.WorkspacePurchaseEligibilityList, error) {
	p.listReq = r
	if p.listErr != nil {
		return nil, p.listErr
	}
	items := make([]*api.WorkspacePurchaseEligibilityReadback, 0, len(r.GetActorIds()))
	for _, id := range r.GetActorIds() {
		items = append(items, &api.WorkspacePurchaseEligibilityReadback{
			ActorId:                  id,
			AccountId:                id,
			WorkspacePurchaseEnabled: p.listMap[id],
		})
	}
	return &api.WorkspacePurchaseEligibilityList{Items: items}, nil
}

func TestOperatorPurchaseEligibilityWriteRoute(t *testing.T) {
	tenant := &eligibilityTenantProbe{}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", TenantId: proto.String("tenant-1"), CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY: activationPlatformDecision(
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY,
				api.OwnerEnum_OWNER_ENUM_TENANT,
				api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
				"42",
				"elig-auth-1",
			),
		},
	}
	handler := (&Server{identity: identity, tenant: tenant}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/42/workspace-purchase-eligibility",
		strings.NewReader(`{"confirmationAccountId":"42","enabled":true,"reason":"operator_requested"}`))
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "csrf-admin")
	req.Header.Set("Idempotency-Key", "elig-key-1")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		OperationID              string `json:"operationId"`
		AccountID                string `json:"accountId"`
		Status                   string `json:"status"`
		WorkspacePurchaseEnabled bool   `json:"workspacePurchaseEnabled"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OperationID != "workspace-purchase-eligibility-1" || resp.AccountID != "42" || resp.Status != "succeeded" || !resp.WorkspacePurchaseEnabled {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if tenant.setReq == nil || tenant.setReq.GetTargetActorId() != "42" || tenant.setReq.GetContext().GetScope().GetPlatform() == nil || tenant.setReq.GetContext().GetIdempotencyKey() != "elig-key-1" {
		t.Fatalf("unexpected owner request: %+v", tenant.setReq)
	}

	// Rejects mismatched confirmationAccountId before calling owner.
	tenant.setReq = nil
	badReq := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/42/workspace-purchase-eligibility",
		strings.NewReader(`{"confirmationAccountId":"99","enabled":true,"reason":"operator_requested"}`))
	badReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("X-CSRF-Token", "csrf-admin")
	badReq.Header.Set("Idempotency-Key", "elig-key-2")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest || tenant.setReq != nil {
		t.Fatalf("expected 400 without owner call, got status=%d req=%+v", badRec.Code, tenant.setReq)
	}

	// Rejects unauthorized caller before calling owner.
	denied := activationPlatformDecision(
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY,
		api.OwnerEnum_OWNER_ENUM_TENANT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		"42",
		"elig-auth-denied",
	)
	denied.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY] = denied
	denyReq := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/42/workspace-purchase-eligibility",
		strings.NewReader(`{"confirmationAccountId":"42","enabled":false,"reason":"operator_requested"}`))
	denyReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	denyReq.Header.Set("Content-Type", "application/json")
	denyReq.Header.Set("X-CSRF-Token", "csrf-admin")
	denyReq.Header.Set("Idempotency-Key", "elig-key-3")
	denyRec := httptest.NewRecorder()
	handler.ServeHTTP(denyRec, denyReq)
	if denyRec.Code != http.StatusForbidden || tenant.setReq != nil {
		t.Fatalf("expected 403 without owner call, got status=%d req=%+v", denyRec.Code, tenant.setReq)
	}
}

func TestOperatorAccountsProjectsAuthoritativePurchaseEligibility(t *testing.T) {
	const token = "operator-elig-proj-test-token-0001"
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/cloud/operator/accounts" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"source":    "control-plane+sub2api",
			"status":    "available",
			"available": true,
			"fetchedAt": "2026-11-27T00:00:00Z",
			"data": map[string]any{
				"items": []map[string]any{
					{"accountId": "acct-42", "sub2apiUserId": "42", "workspacePurchaseEnabled": false},
					{"accountId": "acct-43", "sub2apiUserId": "43", "workspacePurchaseEnabled": true},
				},
				"total": 2, "page": 1, "pageSize": 20,
			},
		})
	}))
	defer cpServer.Close()

	readers, err := clients.Dial(clients.Config{ControlPlaneURL: cpServer.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()

	tenant := &eligibilityTenantProbe{listMap: map[string]bool{"42": true, "43": false}}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			operatorPrivilegeAction: activationPlatformDecision(operatorPrivilegeAction, api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "op-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTTENANTS: activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTTENANTS, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", "list-1"),
		},
	}
	srv := NewServer(readers, identity)
	srv.tenant = tenant
	accountsReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v2/operator/accounts?page=1&pageSize=20", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
		return r
	}
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, accountsReq())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var env struct {
		Data struct {
			Items []struct {
				AccountID                string `json:"accountId"`
				WorkspacePurchaseEnabled bool   `json:"workspacePurchaseEnabled"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.Items) != 2 || !env.Data.Items[0].WorkspacePurchaseEnabled || env.Data.Items[1].WorkspacePurchaseEnabled {
		t.Fatalf("expected authoritative CloudIdentity eligibility projection, got %+v", env.Data.Items)
	}

	// Fails closed when CloudIdentity eligibility readback fails.
	tenant.listErr = status.Error(codes.Unavailable, "tenant db unavailable")
	failRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(failRec, accountsReq())
	if failRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when eligibility projection fails, got %d body=%s", failRec.Code, failRec.Body.String())
	}
}
