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
