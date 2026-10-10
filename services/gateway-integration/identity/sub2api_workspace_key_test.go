package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opl-cloud/services/gateway-integration/identity"
)

// sub2apiIssuanceRequest is the exact service request shape the approved seam
// fixes. The fixture decodes it so a request-shape drift fails the test.
type sub2apiIssuanceRequest struct {
	UserID            int64    `json:"user_id"`
	WorkspaceID       string   `json:"workspace_id"`
	LaunchOperationID string   `json:"launch_operation_id"`
	ExactName         string   `json:"exact_name"`
	GroupName         string   `json:"group_name"`
	ModelIDs          []string `json:"model_ids"`
	IdempotencyKey    string   `json:"idempotency_key"`
}

func workspaceKeyIssuerFixture(t *testing.T, handler http.HandlerFunc) *identity.Sub2APIWorkspaceKeyIssuer {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	issuer, err := identity.NewSub2APIWorkspaceKeyIssuer(server.URL, strings.Repeat("s", 32), "workspace-approved")
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

// TestSub2APIWorkspaceKeyIssuerSendsTheApprovedRequestAndReadsTheResolvedScope
// proves the declared empty model selection travels as the declared default scope
// and the authority's resolved concrete list is the one returned to the owner.
func TestSub2APIWorkspaceKeyIssuerSendsTheApprovedRequestAndReadsTheResolvedScope(t *testing.T) {
	var received sub2apiIssuanceRequest
	var authorization string
	issuer := workspaceKeyIssuerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/service/workspace-keys" || r.Method != http.MethodPost {
			t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
		}
		authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": map[string]any{"key_id": 4242, "api_key": "sk-managed", "group_id": 9, "model_ids": []string{"gpt-5.1-codex"}, "replayed": false}})
	})
	result, err := issuer.IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
		Subject: "77", WorkspaceID: "workspace-key", LaunchOperationID: "op-1:create_managed_key", ExactName: "opl-workspace-abcdef123456",
		GroupName: "workspace-approved", IdempotencyKey: "op-1:create_managed_key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Raw != "sk-managed" || result.ExternalKeyID != "4242" || result.GroupID != 9 || result.Replayed || strings.Join(result.ModelIDs, ",") != "gpt-5.1-codex" {
		t.Fatalf("issued=%+v", result)
	}
	if authorization != "Bearer "+strings.Repeat("s", 32) {
		t.Fatalf("authorization=%q want the service token as a bearer credential", authorization)
	}
	if received.UserID != 77 || received.WorkspaceID != "workspace-key" || received.LaunchOperationID != "op-1:create_managed_key" ||
		received.ExactName != "opl-workspace-abcdef123456" || received.GroupName != "workspace-approved" ||
		len(received.ModelIDs) != 0 || received.IdempotencyKey != "op-1:create_managed_key" {
		t.Fatalf("request=%+v", received)
	}
}

// TestSub2APIWorkspaceKeyIssuerRefusesUnapprovedIdentities proves each listed
// refusal keeps its own meaning and an unknown failure is never mistaken for one.
func TestSub2APIWorkspaceKeyIssuerRefusesUnapprovedIdentities(t *testing.T) {
	for name, testCase := range map[string]struct {
		status   int
		reason   string
		wantCode codes.Code
	}{
		"binding conflict":   {http.StatusConflict, "workspace_key_issuance_binding_conflict", codes.AlreadyExists},
		"key unavailable":    {http.StatusConflict, "workspace_key_issuance_key_unavailable", codes.FailedPrecondition},
		"scope unresolved":   {http.StatusUnprocessableEntity, "workspace_key_model_scope_unresolved", codes.FailedPrecondition},
		"model not allowed":  {http.StatusUnprocessableEntity, "workspace_key_model_not_allowed", codes.FailedPrecondition},
		"group unresolved":   {http.StatusUnprocessableEntity, "workspace_key_group_unresolved", codes.FailedPrecondition},
		"token unconfigured": {http.StatusServiceUnavailable, "service_token_not_configured", codes.FailedPrecondition},
		"missing token":      {http.StatusUnauthorized, "", codes.Unauthenticated},
		"invalid token":      {http.StatusForbidden, "", codes.PermissionDenied},
		"unknown failure":    {http.StatusInternalServerError, "", codes.Unavailable},
	} {
		issuer := workspaceKeyIssuerFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(testCase.status)
			json.NewEncoder(w).Encode(map[string]any{"code": 1, "message": "refused", "reason": testCase.reason})
		})
		_, err := issuer.IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{Subject: "77", WorkspaceID: "workspace-key", IdempotencyKey: "op-1:create_managed_key"})
		if status.Code(err) != testCase.wantCode {
			t.Fatalf("%s err=%v want %s", name, err, testCase.wantCode)
		}
	}
}

// TestSub2APIWorkspaceKeyIssuerFailsClosedOnAnInvalidSuccessBody proves a success
// envelope that violates the approved contract is unresolved rather than accepted:
// the owner never records a wildcard, empty or malformed model scope.
func TestSub2APIWorkspaceKeyIssuerFailsClosedOnAnInvalidSuccessBody(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"empty scope":    {"key_id": 1, "api_key": "sk", "group_id": 1, "model_ids": []string{}, "replayed": false},
		"wildcard":       {"key_id": 1, "api_key": "sk", "group_id": 1, "model_ids": []string{"*"}, "replayed": false},
		"invalid id":     {"key_id": 1, "api_key": "sk", "group_id": 1, "model_ids": []string{"two words"}, "replayed": false},
		"missing key id": {"key_id": 0, "api_key": "sk", "group_id": 1, "model_ids": []string{"gpt"}, "replayed": false},
		"missing raw":    {"key_id": 1, "api_key": "", "group_id": 1, "model_ids": []string{"gpt"}, "replayed": false},
		"missing group":  {"key_id": 1, "api_key": "sk", "group_id": 0, "model_ids": []string{"gpt"}, "replayed": false},
	} {
		issuer := workspaceKeyIssuerFixture(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
		})
		if _, err := issuer.IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{Subject: "77", WorkspaceID: "workspace-key", IdempotencyKey: "op-1:create_managed_key"}); status.Code(err) != codes.Unavailable {
			t.Fatalf("%s err=%v want unavailable", name, err)
		}
	}
	issuer := workspaceKeyIssuerFixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) })
	if _, err := issuer.IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{Subject: "77", WorkspaceID: "workspace-key", IdempotencyKey: "op-1:create_managed_key"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("unreadable body err=%v want unavailable", err)
	}
}

// TestSub2APIWorkspaceKeyIssuerRequiresAnExplicitApprovedConfiguration proves a
// deployment cannot half-configure the issuance authority or point it at a
// non-approved endpoint.
func TestSub2APIWorkspaceKeyIssuerRequiresAnExplicitApprovedConfiguration(t *testing.T) {
	token := strings.Repeat("s", 32)
	for name, config := range map[string]struct {
		url, token, group string
	}{
		"remote http":   {"http://sub2api.example.com", token, "group"},
		"relative url":  {"/api", token, "group"},
		"userinfo":      {"https://user:pass@sub2api.example.com", token, "group"},
		"short token":   {"https://sub2api.example.com", "short", "group"},
		"missing token": {"https://sub2api.example.com", "", "group"},
		"missing group": {"https://sub2api.example.com", token, ""},
	} {
		if _, err := identity.NewSub2APIWorkspaceKeyIssuer(config.url, config.token, config.group); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err := identity.NewSub2APIWorkspaceKeyIssuer("http://127.0.0.1:8080", token, "group"); err != nil {
		t.Fatalf("loopback HTTP was refused: %v", err)
	}
}
