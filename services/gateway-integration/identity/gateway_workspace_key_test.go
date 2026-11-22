package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opl-cloud/services/gateway-integration/identity"
)

func TestGatewayWorkspaceKeyIssuerUsesExistingUserAPIsAndOperationIdempotency(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotIdempotency string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth, gotIdempotency = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		if r.Method == http.MethodGet {
			if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"id": 9, "name": "Codex", "status": "active"}}}); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"id": 4242, "user_id": 77, "key": "sk-created-once", "name": "opl-workspace-abcdef123456", "group_id": 9, "status": "active",
		}}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	gateway, err := identity.NewGateway(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := identity.NewGatewayWorkspaceKeyIssuer(gateway, "Codex")
	if err != nil {
		t.Fatal(err)
	}
	result, err := issuer.IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
		Subject: "77", WorkspaceID: "workspace-key", ExactName: "opl-workspace-abcdef123456",
		GroupName: "Codex", IdempotencyKey: "launch-1:create_managed_key",
		Delegation: &identity.ManagedKeyDelegation{Subject: "77", Bearer: "gateway-user-bearer", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Raw != "sk-created-once" || result.ExternalKeyID != "4242" || result.GroupID != 9 || len(result.ModelIDs) != 0 {
		t.Fatalf("result=%+v", result)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/keys" || gotAuth != "Bearer gateway-user-bearer" || gotIdempotency != "launch-1:create_managed_key" {
		t.Fatalf("request=%s %s auth=%q idempotency=%q", gotMethod, gotPath, gotAuth, gotIdempotency)
	}
	if gotBody["name"] != "opl-workspace-abcdef123456" || gotBody["group_id"].(float64) != 9 {
		t.Fatalf("body=%v", gotBody)
	}
}

func TestGatewayWorkspaceKeyIssuerRequiresAnExistingVisibleGroup(t *testing.T) {
	var createCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/groups/available" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"id": 10, "name": "Other", "status": "active"}}})
			return
		}
		createCalls++
		http.Error(w, "unexpected key creation", http.StatusInternalServerError)
	}))
	defer server.Close()
	gateway, err := identity.NewGateway(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := identity.NewGatewayWorkspaceKeyIssuer(gateway, "Codex")
	if err != nil {
		t.Fatal(err)
	}
	_, err = issuer.IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
		Subject: "77", WorkspaceID: "workspace-key", ExactName: "opl-workspace-abcdef123456", IdempotencyKey: "launch-2:create_managed_key",
		Delegation: &identity.ManagedKeyDelegation{Subject: "77", Bearer: "gateway-user-bearer", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), "Codex") || createCalls != 0 {
		t.Fatalf("err=%v createCalls=%d", err, createCalls)
	}
}

// issuedKeyGateway serves the three external reads the Workspace key issuer
// uses: the visible Group list, the key creation and the model catalog the
// issued Key reads for itself. The catalog is recorded per request so a test
// can prove which credential read it.
type issuedKeyGateway struct {
	server        *httptest.Server
	catalogAuth   []string
	catalogCalls  int
	catalogStatus int
	catalogBody   any
}

func newIssuedKeyGateway(t *testing.T) *issuedKeyGateway {
	t.Helper()
	gateway := &issuedKeyGateway{catalogStatus: http.StatusOK, catalogBody: map[string]any{"data": []map[string]any{}}}
	gateway.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/groups/available":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"id": 9, "name": "Codex", "status": "active"}}})
		case r.URL.Path == "/api/v1/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"id": 4242, "user_id": 77, "key": "sk-created-once", "name": "opl-workspace-abcdef123456", "group_id": 9, "status": "active",
			}})
		case r.URL.Path == "/v1/models":
			gateway.catalogCalls++
			gateway.catalogAuth = append(gateway.catalogAuth, r.Header.Get("Authorization"))
			w.WriteHeader(gateway.catalogStatus)
			_ = json.NewEncoder(w).Encode(gateway.catalogBody)
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	t.Cleanup(gateway.server.Close)
	return gateway
}

func (g *issuedKeyGateway) issuer(t *testing.T) *identity.GatewayWorkspaceKeyIssuer {
	t.Helper()
	gateway, err := identity.NewGateway(g.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := identity.NewGatewayWorkspaceKeyIssuer(gateway, "Codex")
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

func TestGatewayWorkspaceKeyIssuerConfirmsDeclaredModelsAgainstTheIssuedKey(t *testing.T) {
	gateway := newIssuedKeyGateway(t)
	gateway.catalogBody = map[string]any{"data": []map[string]any{
		{"id": "gpt-5.6-sol"}, {"id": "gpt-5.5"}, {"id": "hidden-model", "hidden": true},
	}}
	result, err := gateway.issuer(t).IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
		Subject: "77", WorkspaceID: "workspace-key", ExactName: "opl-workspace-abcdef123456",
		GroupName: "Codex", ModelIDs: []string{"gpt-5.6-sol", "gpt-5.5"}, IdempotencyKey: "launch-1:create_managed_key",
		Delegation: &identity.ManagedKeyDelegation{Subject: "77", Bearer: "gateway-user-bearer", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.ModelIDs, ",") != "gpt-5.6-sol,gpt-5.5" {
		t.Fatalf("resolved scope=%v", result.ModelIDs)
	}
	// The scope is confirmed with the Key that was just issued, not with the
	// login bearer that created it.
	if gateway.catalogCalls != 1 || len(gateway.catalogAuth) != 1 || gateway.catalogAuth[0] != "Bearer sk-created-once" {
		t.Fatalf("catalog calls=%d auth=%v", gateway.catalogCalls, gateway.catalogAuth)
	}
}

func TestGatewayWorkspaceKeyIssuerRefusesAModelTheIssuedKeyCannotUse(t *testing.T) {
	gateway := newIssuedKeyGateway(t)
	gateway.catalogBody = map[string]any{"data": []map[string]any{{"id": "gpt-5.5"}}}
	result, err := gateway.issuer(t).IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
		Subject: "77", WorkspaceID: "workspace-key", ExactName: "opl-workspace-abcdef123456",
		GroupName: "Codex", ModelIDs: []string{"gpt-5.6-sol"}, IdempotencyKey: "launch-1:create_managed_key",
		Delegation: &identity.ManagedKeyDelegation{Subject: "77", Bearer: "gateway-user-bearer", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(status.Convert(err).Message(), "gpt-5.6-sol") {
		t.Fatalf("err=%v", err)
	}
	if result.Raw != "" || result.ExternalKeyID != "4242" {
		t.Fatalf("a refused issuance must retain only its external key identity: %+v", result)
	}
}

func TestGatewayWorkspaceKeyIssuerLeavesAnEmptyDeclarationToTheExternalGroup(t *testing.T) {
	gateway := newIssuedKeyGateway(t)
	result, err := gateway.issuer(t).IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
		Subject: "77", WorkspaceID: "workspace-key", ExactName: "opl-workspace-abcdef123456",
		GroupName: "Codex", IdempotencyKey: "launch-1:create_managed_key",
		Delegation: &identity.ManagedKeyDelegation{Subject: "77", Bearer: "gateway-user-bearer", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ModelIDs) != 0 {
		t.Fatalf("resolved scope=%v", result.ModelIDs)
	}
	// A declaration that names no model adds no external read to the default
	// App's issuance.
	if gateway.catalogCalls != 0 {
		t.Fatalf("catalog calls=%d", gateway.catalogCalls)
	}
}

func TestGatewayWorkspaceKeyIssuerLeavesAnUnreadableScopeUnresolved(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       any
	}{
		{"upstream failure", http.StatusInternalServerError, nil},
		{"issued key could not read catalog", http.StatusUnauthorized, nil},
		{"catalog data missing", http.StatusOK, map[string]any{"object": "list"}},
		{"catalog data null", http.StatusOK, map[string]any{"data": nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := newIssuedKeyGateway(t)
			gateway.catalogStatus, gateway.catalogBody = test.statusCode, test.body
			result, err := gateway.issuer(t).IssueWorkspaceKey(t.Context(), identity.ManagedKeyIssueRequest{
				Subject: "77", WorkspaceID: "workspace-key", ExactName: "opl-workspace-abcdef123456",
				GroupName: "Codex", ModelIDs: []string{"gpt-5.6-sol"}, IdempotencyKey: "launch-1:create_managed_key",
				Delegation: &identity.ManagedKeyDelegation{Subject: "77", Bearer: "gateway-user-bearer", ExpiresAt: time.Now().Add(time.Hour)},
			})
			if status.Code(err) != codes.Unavailable || result.Raw != "" {
				t.Fatalf("err=%v, returnedKey=%t", err, result.Raw != "")
			}
		})
	}
}
