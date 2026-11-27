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

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// TestOperatorRoutesAuthorizePlatformOperatorBeforeOwnerRead drives a
// CloudIdentity session through every BFF operator route and proves the
// platform-operator decision is ALLOWED before the retained owner is called.
func TestOperatorRoutesAuthorizePlatformOperatorBeforeOwnerRead(t *testing.T) {
	const token = "operator-reader-test-token-000001"
	identity := &fakeIdentity{
		session:  &api.Session{ActorId: "operator-1", DisplayName: "Operator", CsrfToken: "csrf-1"},
		decision: allowOperatorDecision("operator-1"),
	}
	paths := map[string]string{
		"/internal/cloud/operator/announcements":                      "control-plane",
		"/internal/cloud/operator/health":                             "control-plane",
		"/internal/cloud/operator/runtime-observations":               "control-plane+fabric",
		"/internal/cloud/operator/reconciliation":                     "control-plane",
		"/internal/cloud/operator/workspace-launches/op-1/recovery":   "",
		"/internal/cloud/operator/wallet-adjustments/op-1":            "",
		"/internal/cloud/operator/application-deployments/op-1":       "",
		"/internal/cloud/operator/registry/repositories":              "",
		"/internal/cloud/operator/registry/tags/namespace/repository": "",
	}
	var ownerCalls []string
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The operator decision for this route is required before the owner's
		// data is read, so each owner call must already have one decision.
		if len(identity.requests) == len(ownerCalls) {
			t.Errorf("owner read before CloudIdentity operator decision: %d decisions, %d owner reads", len(identity.requests), len(ownerCalls))
		}
		if r.Header.Get(owneridentity.PeerHeader) != "console_bff" || r.Header.Get(owneridentity.TokenHeader) != token {
			t.Errorf("owner proof=%v", r.Header)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Actor-Id") != "" || r.Header.Get("X-Tenant-Id") != "" {
			t.Errorf("browser assertion reached owner: %v", r.Header)
		}
		source, declared := paths[r.URL.Path]
		if !declared {
			t.Errorf("unexpected owner path=%s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		ownerCalls = append(ownerCalls, r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"source": source, "status": "available", "available": true,
			"fetchedAt": "2026-10-20T00:00:00Z", "data": map[string]any{"kind": r.URL.Path},
		})
	}))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	handler := NewServer(readers, identity).Handler()

	for _, route := range []string{
		"/api/v2/operator/announcements?page=1&pageSize=20",
		"/api/v2/operator/health",
		"/api/v2/operator/runtime-observations",
		"/api/v2/operator/reconciliation",
		"/api/v2/operator/workspace-launches/op-1/recovery",
		"/api/v2/operator/wallet-adjustments/op-1",
		"/api/v2/operator/application-deployments/op-1",
		"/api/v2/operator/registry/repositories",
		"/api/v2/operator/registry/tags/namespace/repository",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, route))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", route, response.Code, response.Body.String())
		}
		if cookies := response.Result().Cookies(); len(cookies) != 0 {
			t.Fatalf("%s mutated a cookie: %v", route, cookies)
		}
	}
	if len(ownerCalls) != len(paths) {
		t.Fatalf("owner reads=%v", ownerCalls)
	}
	if len(identity.requests) != len(ownerCalls) {
		t.Fatalf("operator decisions=%d owner reads=%d", len(identity.requests), len(ownerCalls))
	}
	// The operator gate is the platform_admin-only action the console already
	// uses, asked at platform scope after the tenant is removed from the session.
	for _, request := range identity.requests {
		if request.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPUBLISHERNAMESPACE ||
			request.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_CAPABILITY ||
			request.GetScope().GetPlatform() == nil || request.GetActorId() != "operator-1" {
			t.Fatalf("operator decision request=%v", request)
		}
	}
}

// operatorAccountsProbe answers the owner that holds the customer account fact.
// The operator account page is read from that owner, not bridged from the
// retained Control Plane writer.
type operatorAccountsProbe struct {
	api.TenantProductServiceClient
	request *api.ListOperatorAccountsRpcRequest
	page    *api.OperatorAccountPage
	err     error
	calls   int
}

func (p *operatorAccountsProbe) ListOperatorAccounts(_ context.Context, r *api.ListOperatorAccountsRpcRequest, _ ...grpc.CallOption) (*api.OperatorAccountPage, error) {
	p.calls++
	p.request = r
	if p.err != nil {
		return nil, p.err
	}
	return p.page, nil
}

// TestOperatorAccountsReadComesFromTheAccountOwner proves the operator account
// page is read from the owner that holds the account fact, after the platform
// operator decision, and that the Gateway observations the owner read natively
// are published as they were read while a fact this owner does not hold stays
// unavailable instead of being reported as a zero.
func TestOperatorAccountsReadComesFromTheAccountOwner(t *testing.T) {
	identity := &fakeIdentity{
		session:  &api.Session{ActorId: "operator-1", CsrfToken: "csrf-1"},
		decision: allowOperatorDecision("operator-1"),
	}
	gatewayEmail := "customer@example.test"
	gatewayStatus := "active"
	balance := int64(4200)
	keyCount := int32(2)
	today := int64(100)
	total := int64(900)
	probe := &operatorAccountsProbe{page: &api.OperatorAccountPage{
		Items: []*api.OperatorAccount{{
			AccountId: "acct-1", ActorId: "77", Email: "customer@example.test", Status: "active",
			WorkspacePurchaseEnabled: true,
			GatewayEmail:             &gatewayEmail, GatewayStatus: &gatewayStatus,
			BalanceUsdMicros: &balance, KeyCount: &keyCount,
			TodayActualCostUsdMicros: &today, TotalActualCostUsdMicros: &total,
			PlatformUsage: []*api.GatewayPlatformUsage{{
				Platform: "openai", TodayActualCostUsdMicros: 60, TotalActualCostUsdMicros: 500,
			}},
		}},
		Total: 1, Page: 2, PageSize: 3,
	}}
	server := &Server{identity: identity, tenant: probe}

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/accounts?page=2&pageSize=3"))
	if response.Code != http.StatusOK {
		t.Fatalf("accounts status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.calls != 1 || probe.request.GetPage() != 2 || probe.request.GetPageSize() != 3 {
		t.Fatalf("owner page request=%+v calls=%d", probe.request, probe.calls)
	}
	if probe.request.GetContext().GetActorId() != "operator-1" || probe.request.GetContext().GetScope().GetPlatform() == nil {
		t.Fatalf("owner page context=%+v", probe.request.GetContext())
	}
	if len(identity.requests) != 1 || identity.requests[0].GetAction() != operatorPrivilegeAction {
		t.Fatalf("operator decisions=%v", identity.requests)
	}

	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["source"] != "tenant+sub2api" {
		t.Fatalf("accounts source=%v", envelope["source"])
	}
	data := envelope["data"].(map[string]any)
	if data["total"].(float64) != 1 || data["page"].(float64) != 2 || data["pageSize"].(float64) != 3 {
		t.Fatalf("accounts page=%v", data)
	}
	item := data["items"].([]any)[0].(map[string]any)
	if item["accountId"] != "acct-1" || item["sub2apiUserId"] != "77" || item["email"] != "customer@example.test" ||
		item["workspacePurchaseEnabled"] != true {
		t.Fatalf("accounts item=%v", item)
	}
	gatewayIdentity := item["gatewayIdentity"].(map[string]any)
	if gatewayIdentity["source"] != "sub2api" || gatewayIdentity["available"] != true {
		t.Fatalf("gateway identity=%v", gatewayIdentity)
	}
	wallet := item["wallet"].(map[string]any)
	if wallet["data"].(map[string]any)["usdMicros"] != "4200" {
		t.Fatalf("wallet=%v", wallet)
	}
	if item["keyCount"].(map[string]any)["data"].(float64) != 2 {
		t.Fatalf("key count=%v", item["keyCount"])
	}
	usage := item["usage"].(map[string]any)
	if usage["data"].(map[string]any)["totalActualCostUsdMicros"].(float64) != 900 {
		t.Fatalf("usage=%v", usage)
	}
	// The Workspace count is a fact this owner does not hold, so the projection
	// reports it as unavailable instead of inventing a count.
	workspaceCount := item["workspaceCount"].(map[string]any)
	if workspaceCount["available"] != false || workspaceCount["source"] != "control-plane" ||
		workspaceCount["reasonCode"] != "control_plane_unavailable" {
		t.Fatalf("workspace count=%v", workspaceCount)
	}
}

// TestOperatorAccountsRefuseTheOwnerRefusal proves an owner refusal reaches the
// browser as the owner's own typed failure instead of an empty page.
func TestOperatorAccountsRefuseTheOwnerRefusal(t *testing.T) {
	identity := &fakeIdentity{
		session:  &api.Session{ActorId: "operator-1", CsrfToken: "csrf-1"},
		decision: allowOperatorDecision("operator-1"),
	}
	probe := &operatorAccountsProbe{err: status.Error(codes.Unavailable, "gateway directory unavailable")}
	server := &Server{identity: identity, tenant: probe}

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/accounts"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("owner refusal status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.calls != 1 {
		t.Fatalf("owner calls=%d", probe.calls)
	}
}

// TestOperatorRoutesRefuseTenantAdministratorWithoutOwnerRead proves a live
// CloudIdentity session with a tenant admin role is not an operator: the denial
// stays a 403 and no owner data is read or synthesized as unavailable.
func TestOperatorRoutesRefuseTenantAdministratorWithoutOwnerRead(t *testing.T) {
	const token = "operator-reader-test-token-000002"
	denied := allowOperatorDecision("tenant-admin-1")
	denied.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	identity := &fakeIdentity{
		session:  &api.Session{ActorId: "tenant-admin-1", TenantId: proto.String("tenant-1"), Role: rolePtr(api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN), CsrfToken: "csrf-1"},
		decision: denied,
	}
	var ownerCalls int
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ownerCalls++ }))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	handler := NewServer(readers, identity).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/overview"))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"FORBIDDEN"`) {
		t.Fatalf("tenant administrator status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Result().Header.Get("Set-Cookie") != "" {
		t.Fatalf("denied operator read set a cookie: %v", response.Result().Header.Values("Set-Cookie"))
	}
	if ownerCalls != 0 {
		t.Fatalf("owner read before operator refusal: %d", ownerCalls)
	}
	if len(identity.requests) != 1 || identity.requests[0].GetScope().GetTenant() != nil {
		t.Fatalf("operator scope must be platform, not the tenant: %v", identity.requests)
	}
}

// TestOperatorRoutesUnknownSessionNeverDeletesTheConsoleSession pins the
// historical harm: an unknown CloudIdentity session is refused as
// unauthenticated without any Set-Cookie, so a failed read cannot log the
// console out.
func TestOperatorRoutesUnknownSessionNeverDeletesTheConsoleSession(t *testing.T) {
	const token = "operator-reader-test-token-000003"
	identity := &fakeIdentity{err: status.Error(codes.Unauthenticated, "expired or unknown session")}
	var ownerCalls int
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ownerCalls++ }))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	handler := NewServer(readers, identity).Handler()

	request := sessionRequest(http.MethodGet, "/api/v2/operator/overview")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown session status=%d body=%s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("unknown session mutated a cookie: %v", cookies)
	}
	if ownerCalls != 0 {
		t.Fatalf("unknown session reached the owner: %d", ownerCalls)
	}

	// A missing session is unauthenticated too, and also leaves cookies alone.
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/operator/overview", nil))
	if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
		t.Fatalf("missing session status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
}

// TestOperatorRoutesRejectUndeclaredSelectors keeps browser input from selecting
// a tenant, actor or account before any operator decision or owner call.
func TestOperatorRoutesRejectUndeclaredSelectors(t *testing.T) {
	const token = "operator-reader-test-token-000004"
	identity := &fakeIdentity{
		session:  &api.Session{ActorId: "operator-1", CsrfToken: "csrf-1"},
		decision: allowOperatorDecision("operator-1"),
	}
	var ownerCalls int
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ownerCalls++ }))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	handler := NewServer(readers, identity).Handler()

	for _, route := range []string{
		"/api/v2/operator/overview?tenantId=tenant-other",
		"/api/v2/operator/accounts?accountId=acct-other",
		"/api/v2/operator/accounts?page=0",
		"/api/v2/operator/accounts?pageSize=51",
		"/api/v2/operator/accounts?page=1&page=2",
		"/api/v2/operator/accounts?page=1&pageSize=20&actorId=other",
		"/api/v2/operator/accounts?search=a&search=b",
		"/api/v2/operator/announcements?accountId=acct-other",
		"/api/v2/operator/announcements?page=1&page=2",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, route))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", route, response.Code, response.Body.String())
		}
	}
	if len(identity.requests) != 0 || ownerCalls != 0 {
		t.Fatalf("selector reached decision/owner: %d/%d", len(identity.requests), ownerCalls)
	}
}

// TestRemainingOperatorReadsRefuseTenantAdministratorWithoutOwnerRead pins the
// same authorization order for the remaining operator surfaces: the
// platform-operator decision is denied before the retained owner is read, and
// the refusal is an explicit 403, never a source-unavailable envelope.
func TestRemainingOperatorReadsRefuseTenantAdministratorWithoutOwnerRead(t *testing.T) {
	const token = "operator-reader-test-token-000005"
	denied := allowOperatorDecision("tenant-admin-1")
	denied.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	identity := &fakeIdentity{
		session:  &api.Session{ActorId: "tenant-admin-1", TenantId: proto.String("tenant-1"), Role: rolePtr(api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN), CsrfToken: "csrf-1"},
		decision: denied,
	}
	var ownerCalls int
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ownerCalls++ }))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	handler := NewServer(readers, identity).Handler()

	for _, route := range []string{
		"/api/v2/operator/announcements",
		"/api/v2/operator/health",
		"/api/v2/operator/runtime-observations",
		"/api/v2/operator/reconciliation",
		"/api/v2/operator/workspace-launches/op-1/recovery",
		"/api/v2/operator/wallet-adjustments/op-1",
		"/api/v2/operator/application-deployments/op-1",
		"/api/v2/operator/registry/repositories",
		"/api/v2/operator/registry/tags/namespace/repository",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, route))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"FORBIDDEN"`) {
			t.Fatalf("%s status=%d body=%s", route, response.Code, response.Body.String())
		}
		if cookies := response.Result().Cookies(); len(cookies) != 0 {
			t.Fatalf("%s denied read set a cookie: %v", route, cookies)
		}
	}
	if ownerCalls != 0 {
		t.Fatalf("owner read before operator refusal: %d", ownerCalls)
	}
	if len(identity.requests) != 9 {
		t.Fatalf("operator decisions=%d", len(identity.requests))
	}
	for _, request := range identity.requests {
		if request.GetScope().GetTenant() != nil {
			t.Fatalf("operator scope must be platform, not the tenant: %v", request)
		}
	}
}

// TestRemainingOperatorReadsUnknownSessionNeverTouchCookies pins the 401
// boundary for operator announcements: a missing or unknown CloudIdentity
// session is refused without clearing opl_console_session.
func TestRemainingOperatorReadsUnknownSessionNeverTouchCookies(t *testing.T) {
	const token = "operator-reader-test-token-000006"
	identity := &fakeIdentity{err: status.Error(codes.Unauthenticated, "expired or unknown session")}
	var ownerCalls int
	owner := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ownerCalls++ }))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	handler := NewServer(readers, identity).Handler()

	for _, route := range []string{
		"/api/v2/operator/announcements",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, route))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d body=%s", route, response.Code, response.Body.String())
		}
		if cookies := response.Result().Cookies(); len(cookies) != 0 {
			t.Fatalf("%s unknown session mutated a cookie: %v", route, cookies)
		}
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
			t.Fatalf("%s missing session status=%d cookies=%v", route, response.Code, response.Result().Cookies())
		}
	}
	if ownerCalls != 0 {
		t.Fatalf("unknown session reached the owner: %d", ownerCalls)
	}
}

func allowOperatorDecision(actor string) *api.AuthorizationDecision {
	return &api.AuthorizationDecision{
		ActorId:           actor,
		SessionId:         proto.String(owneridentity.SessionReference("session-1")),
		Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		PermissionVersion: 1,
		IssuedAt:          timestamppb.New(time.Now().Add(-time.Minute)),
		ExpiresAt:         timestamppb.New(time.Now().Add(time.Minute)),
		Result:            api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		Issuer:            api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action:            api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPUBLISHERNAMESPACE,
		AudienceOwner:     api.OwnerEnum_OWNER_ENUM_CAPABILITY,
		Resource:          &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
	}
}

func rolePtr(role api.TenantRoleEnum) *api.TenantRoleEnum { return &role }
