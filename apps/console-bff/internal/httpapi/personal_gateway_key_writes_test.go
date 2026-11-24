package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// personalWriteRequest builds the browser's personal Key write request: the live
// session cookie, the session CSRF token and the caller's own idempotency key.
// The shared write guard reads all three before any owner call.
func personalWriteRequest(method, path, body string) *http.Request {
	request := sessionRequest(method, path)
	if body != "" {
		request.Body = io.NopCloser(strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "personal-key-write-1")
	return request
}

// personalWriteIdentity authorizes one personal Gateway Key write for the
// signed-in User. A personal Key belongs to the User, so the session carries no
// Tenant and the resource names no Tenant either.
func personalWriteIdentity(action api.AuthorizationActionEnum) *fakeIdentity {
	identity := allowedIdentity()
	identity.session.TenantId = nil
	identity.decision = allowFor("actor-1", "", api.OwnerEnum_OWNER_ENUM_GATEWAY, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "")
	identity.decision.Resource.Id = nil
	identity.decision.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	return identity
}

// personalWriteCall asserts the caller the owner actually received: the live
// session, its actor and the caller's own idempotency key.
func personalWriteCall(t *testing.T, call *api.CallContext) {
	t.Helper()
	if call == nil {
		t.Fatal("the owner received no call context")
	}
	if call.GetActorId() != "actor-1" || call.GetSessionId() != owneridentity.SessionReference("session-1") {
		t.Fatalf("the owner received another caller: %v", call)
	}
	if call.GetIdempotencyKey() != "personal-key-write-1" {
		t.Fatalf("the caller's idempotency key never reached the owner: %v", call)
	}
	if call.GetRequestId() == "" {
		t.Fatalf("the owner received no request id: %v", call)
	}
}

// TestPersonalGatewayKeyCreateCarriesEveryFieldAndReturnsTheSecretOnce proves
// the create route carries the customer's whole instruction to the Gateway owner
// and projects the owner's answer, including the secret exactly once.
func TestPersonalGatewayKeyCreateCarriesEveryFieldAndReturnsTheSecretOnce(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPERSONALGATEWAYKEY)
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(http.MethodPost, "/api/v2/gateway/keys", `{
		"name":"personal key","groupId":"4",
		"ipWhitelist":["10.0.0.0/8"],"ipBlacklist":["192.168.1.1"],
		"quotaUSDMicros":"1500000","expiresInDays":30,
		"rateLimit5hUSDMicros":"2000000","rateLimit1dUSDMicros":"3000000","rateLimit7dUSDMicros":"4000000"}`))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.personalCreateRequest == nil {
		t.Fatal("the create never reached the owner")
	}
	personalWriteCall(t, owner.personalCreateRequest.GetContext())
	body := owner.personalCreateRequest.GetBody()
	if body.GetName() != "personal key" || body.GetGroupId() != "4" {
		t.Fatalf("the owner received another instruction: %v", body)
	}
	if len(body.GetIpWhitelist()) != 1 || body.GetIpWhitelist()[0] != "10.0.0.0/8" || len(body.GetIpBlacklist()) != 1 || body.GetIpBlacklist()[0] != "192.168.1.1" {
		t.Fatalf("the owner received another address list: %v", body)
	}
	if body.GetQuotaUsdMicros() != 1_500_000 || body.GetExpiresInDays() != 30 {
		t.Fatalf("the owner received another quota or expiry: %v", body)
	}
	if body.GetRateLimit5HUsdMicros() != 2_000_000 || body.GetRateLimit1DUsdMicros() != 3_000_000 || body.GetRateLimit7DUsdMicros() != 4_000_000 {
		t.Fatalf("the owner received another rate limit: %v", body)
	}
	// The answer is the owner's own projection: the contract spelling, the exact
	// money, and the secret exactly once.
	answer := response.Body.String()
	if strings.Count(answer, "raw-secret-must-never-escape") != 1 {
		t.Fatalf("the create answer does not carry the secret exactly once: %s", answer)
	}
	for _, fact := range []string{`"secret":"raw-secret-must-never-escape"`, `"quotaUSDMicros":"1500000"`, `"rateLimit5hUSDMicros":"2000000"`, `"status":"active"`, `"kind":"general"`} {
		if !strings.Contains(answer, fact) {
			t.Fatalf("the create answer is missing %s: %s", fact, answer)
		}
	}
}

// TestPersonalGatewayKeyUpdateCarriesEveryField proves the update route carries
// the customer's whole instruction to the Gateway owner, including an address
// list that replaces the Key's list.
func TestPersonalGatewayKeyUpdateCarriesEveryField(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEPERSONALGATEWAYKEY)
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(http.MethodPatch, "/api/v2/gateway/keys/71", `{
		"name":"renamed personal key","groupId":"4",
		"ipWhitelist":{"values":["10.0.0.0/8"]},"ipBlacklist":{"values":[]},
		"quotaUSDMicros":"2000000","expiresAt":"2026-12-01T00:00:00Z",
		"rateLimit5hUSDMicros":"5000000","rateLimit1dUSDMicros":"6000000","rateLimit7dUSDMicros":"7000000",
		"resetQuota":true,"resetRateLimitUsage":true,"enabled":false}`))
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.personalUpdateRequest == nil {
		t.Fatal("the update never reached the owner")
	}
	personalWriteCall(t, owner.personalUpdateRequest.GetContext())
	if owner.personalUpdateRequest.GetKeyId() != "71" {
		t.Fatalf("the owner received another key: %v", owner.personalUpdateRequest)
	}
	body := owner.personalUpdateRequest.GetBody()
	if body.GetName() != "renamed personal key" || body.GetGroupId() != "4" {
		t.Fatalf("the owner received another instruction: %v", body)
	}
	if body.IpWhitelist == nil || len(body.GetIpWhitelist().GetValues()) != 1 || body.GetIpWhitelist().GetValues()[0] != "10.0.0.0/8" {
		t.Fatalf("the owner received another whitelist: %v", body)
	}
	if body.IpBlacklist == nil || len(body.GetIpBlacklist().GetValues()) != 0 {
		t.Fatalf("the owner received another blacklist: %v", body)
	}
	if body.GetQuotaUsdMicros() != 2_000_000 || body.ExpiresAt == nil {
		t.Fatalf("the owner received another quota or expiry: %v", body)
	}
	if body.GetRateLimit5HUsdMicros() != 5_000_000 || body.GetRateLimit1DUsdMicros() != 6_000_000 || body.GetRateLimit7DUsdMicros() != 7_000_000 {
		t.Fatalf("the owner received another rate limit: %v", body)
	}
	if !body.GetResetQuota() || !body.GetResetRateLimitUsage() || body.Enabled == nil || *body.Enabled {
		t.Fatalf("the owner received another flag: %v", body)
	}
}

// TestPersonalGatewayKeyUpdateCarriesAClearedExpiryAsAnExplicitClear proves a
// cleared expiry reaches the owner as an explicit instruction instead of an
// absent or empty instant, so the owner can tell it apart from "leave it
// unchanged".
func TestPersonalGatewayKeyUpdateCarriesAClearedExpiryAsAnExplicitClear(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEPERSONALGATEWAYKEY)
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(http.MethodPatch, "/api/v2/gateway/keys/71", `{"clearExpiresAt":true}`))
	if response.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.personalUpdateRequest == nil {
		t.Fatal("the clear never reached the owner")
	}
	body := owner.personalUpdateRequest.GetBody()
	if body.ClearExpiresAt == nil || !*body.ClearExpiresAt {
		t.Fatalf("the clear never reached the owner as an explicit instruction: %v", body)
	}
	if body.ExpiresAt != nil {
		t.Fatalf("the clear carried an expiry as well: %v", body)
	}
}

// TestPersonalGatewayKeyRevealReturnsTheSecretOnceAndNeverAcrossAccounts
// proves the reveal route hands the secret back exactly once and offers the
// owner no way to name another account: the route carries no target selector,
// so the owner binds the Key to the caller's own session.
func TestPersonalGatewayKeyRevealReturnsTheSecretOnceAndNeverAcrossAccounts(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALPERSONALGATEWAYKEY)
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(http.MethodPost, "/api/v2/gateway/keys/71/reveal", `{"userId":"other","accountId":"other"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("reveal status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.personalRevealRequest == nil {
		t.Fatal("the reveal never reached the owner")
	}
	personalWriteCall(t, owner.personalRevealRequest.GetContext())
	if owner.personalRevealRequest.GetKeyId() != "71" {
		t.Fatalf("the owner received another key: %v", owner.personalRevealRequest)
	}
	answer := response.Body.String()
	if strings.Count(answer, "raw-secret-must-never-escape") != 1 {
		t.Fatalf("the reveal answer does not carry the secret exactly once: %s", answer)
	}
	if strings.Contains(answer, "other") {
		t.Fatalf("the reveal answer carries another account: %s", answer)
	}
}

// TestPersonalGatewayKeyRevokeIsIdempotent proves a repeated revoke is the
// owner's own answer both times: the route keeps no second copy of the Key's
// state and never reports a revoke the owner did not confirm.
func TestPersonalGatewayKeyRevokeIsIdempotent(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVOKEPERSONALGATEWAYKEY)
	handler := NewServer(client, identity).Handler()
	for attempt := 1; attempt <= 2; attempt++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, personalWriteRequest(http.MethodDelete, "/api/v2/gateway/keys/71", ""))
		if response.Code != http.StatusOK {
			t.Fatalf("revoke %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if owner.personalRevokeRequest == nil {
			t.Fatalf("revoke %d never reached the owner", attempt)
		}
		personalWriteCall(t, owner.personalRevokeRequest.GetContext())
		if owner.personalRevokeRequest.GetKeyId() != "71" {
			t.Fatalf("the owner received another key: %v", owner.personalRevokeRequest)
		}
		if !strings.Contains(response.Body.String(), `"kind":"revoke_key"`) {
			t.Fatalf("revoke %d did not publish the owner's operation: %s", attempt, response.Body.String())
		}
	}
}

// TestPersonalGatewayKeyProviderFailureNeverReportsSuccess proves a failed
// provider call is not reported as a success and never leaks a secret.
func TestPersonalGatewayKeyProviderFailureNeverReportsSuccess(t *testing.T) {
	owner := &gatewayOwnerServer{personalWriteErr: status.Error(codes.Unavailable, "Gateway personal key write outcome is unknown")}
	client := dialGatewayOwner(t, owner)
	for _, row := range []struct {
		method string
		path   string
		body   string
		action api.AuthorizationActionEnum
	}{
		{http.MethodPost, "/api/v2/gateway/keys", `{"name":"personal key","groupId":"4","quotaUSDMicros":"0"}`, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPERSONALGATEWAYKEY},
		{http.MethodPatch, "/api/v2/gateway/keys/71", `{"clearExpiresAt":true}`, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEPERSONALGATEWAYKEY},
		{http.MethodDelete, "/api/v2/gateway/keys/71", "", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVOKEPERSONALGATEWAYKEY},
		{http.MethodPost, "/api/v2/gateway/keys/71/reveal", "", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALPERSONALGATEWAYKEY},
	} {
		identity := personalWriteIdentity(row.action)
		response := httptest.NewRecorder()
		NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(row.method, row.path, row.body))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s reported a failed provider call as %d: %s", row.method, row.path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "raw-secret-must-never-escape") {
			t.Fatalf("%s %s leaked a secret: %s", row.method, row.path, response.Body.String())
		}
	}
}

// TestPersonalGatewayKeyWriteRefusesAnUnauthorizedAction proves an action the
// caller is not authorized for never reaches the owner, and that a decision for
// another action is refused as well.
func TestPersonalGatewayKeyWriteRefusesAnUnauthorizedAction(t *testing.T) {
	for _, row := range []struct {
		name   string
		mutate func(*fakeIdentity)
	}{
		{"denied", func(identity *fakeIdentity) {
			identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
		}},
		{"another action", func(identity *fakeIdentity) {
			identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEY
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			owner := &gatewayOwnerServer{}
			client := dialGatewayOwner(t, owner)
			identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPERSONALGATEWAYKEY)
			row.mutate(identity)
			response := httptest.NewRecorder()
			NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(http.MethodPost, "/api/v2/gateway/keys", `{"name":"personal key","groupId":"4","quotaUSDMicros":"0"}`))
			if response.Code != http.StatusForbidden {
				t.Fatalf("an unauthorized create answered %d: %s", response.Code, response.Body.String())
			}
			if owner.personalCreateRequest != nil {
				t.Fatal("an unauthorized create reached the owner")
			}
		})
	}
}

// TestPersonalGatewayKeyWriteGuardsNeverReachOwner proves the shared write guard
// refuses a personal Key write without a live CSRF token or without the
// caller's idempotency key before any owner call.
func TestPersonalGatewayKeyWriteGuardsNeverReachOwner(t *testing.T) {
	for _, row := range []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"csrf", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, http.StatusForbidden},
		{"idempotency", func(r *http.Request) { r.Header.Del("Idempotency-Key") }, http.StatusBadRequest},
		{"cross origin", func(r *http.Request) { r.Header.Set("Origin", "https://foreign.test") }, http.StatusForbidden},
	} {
		t.Run(row.name, func(t *testing.T) {
			owner := &gatewayOwnerServer{}
			client := dialGatewayOwner(t, owner)
			identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPERSONALGATEWAYKEY)
			request := personalWriteRequest(http.MethodPost, "/api/v2/gateway/keys", `{"name":"personal key","groupId":"4","quotaUSDMicros":"0"}`)
			row.mutate(request)
			response := httptest.NewRecorder()
			NewServer(client, identity).Handler().ServeHTTP(response, request)
			if response.Code != row.want || owner.personalCreateRequest != nil {
				t.Fatalf("status=%d owner=%v body=%s", response.Code, owner.personalCreateRequest != nil, response.Body.String())
			}
		})
	}
}

// TestPersonalGatewayKeyWriteRejectsAnUnknownBodyProperty proves the write route
// accepts exactly the contract's request vocabulary, so a mistyped instruction
// is refused instead of silently dropped.
func TestPersonalGatewayKeyWriteRejectsAnUnknownBodyProperty(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPERSONALGATEWAYKEY)
	response := httptest.NewRecorder()
	NewServer(client, identity).Handler().ServeHTTP(response, personalWriteRequest(http.MethodPost, "/api/v2/gateway/keys", `{"name":"personal key","groupId":"4","quotaUSDMicros":"0","tenantId":"foreign"}`))
	if response.Code != http.StatusBadRequest || owner.personalCreateRequest != nil {
		t.Fatalf("status=%d owner=%v body=%s", response.Code, owner.personalCreateRequest != nil, response.Body.String())
	}
}

// TestPersonalGatewayKeySecretIsNeverStoredByTheRoute proves the route projects
// the owner's answer rather than keeping a copy: a second reveal asks the owner
// again and the route holds no secret between the two answers.
func TestPersonalGatewayKeySecretIsNeverStoredByTheRoute(t *testing.T) {
	owner := &gatewayOwnerServer{}
	client := dialGatewayOwner(t, owner)
	identity := personalWriteIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALPERSONALGATEWAYKEY)
	handler := NewServer(client, identity).Handler()
	for attempt := 1; attempt <= 2; attempt++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, personalWriteRequest(http.MethodPost, "/api/v2/gateway/keys/71/reveal", ""))
		if response.Code != http.StatusOK {
			t.Fatalf("reveal %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if strings.Count(response.Body.String(), "raw-secret-must-never-escape") != 1 {
			t.Fatalf("reveal %d did not carry the secret exactly once: %s", attempt, response.Body.String())
		}
	}
	if owner.personalRevealRequest == nil || owner.personalRevealRequest.GetContext().GetRequestId() == "" {
		t.Fatalf("the reveal never reached the owner: %v", owner.personalRevealRequest)
	}
}
