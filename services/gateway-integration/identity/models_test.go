package identity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identity "opl-cloud/services/gateway-integration/identity"
)

func newDirectoryGateway(t *testing.T, handler http.HandlerFunc) *identity.Gateway {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	gateway, err := identity.NewGatewayWithDirectory(server.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	return gateway
}

func envelope(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func TestGatewayModelsReadsCatalogWithCallerKey(t *testing.T) {
	gateway := newDirectoryGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			envelope(w, map[string]any{"access_token": "admin-token", "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
		case "/api/v1/admin/users/42/api-keys":
			if r.Header.Get("Authorization") != "Bearer admin-token" {
				t.Errorf("admin auth=%q", r.Header.Get("Authorization"))
			}
			envelope(w, map[string]any{"items": []map[string]any{{"key": "sk-caller", "status": "active"}}})
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer sk-caller" {
				t.Errorf("catalog auth=%q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-sol"},{"id":"gpt-5.6-luna","name":"Luna"},{"id":"gpt-5.4","hidden":true}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	models, err := gateway.Models(context.Background(), "42")
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("models=%+v", models)
	}
	if models[0].ID != "gpt-5.6-sol" || models[0].Name != "gpt-5.6-sol" || !models[0].Available {
		t.Fatalf("entry0=%+v", models[0])
	}
	if models[1].Name != "Luna" {
		t.Fatalf("entry1=%+v", models[1])
	}
	if models[2].Available {
		t.Fatalf("hidden model was not marked unavailable: %+v", models[2])
	}
}

func TestGatewayModelsFailsClosedWithoutCallerKey(t *testing.T) {
	gateway := newDirectoryGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			envelope(w, map[string]any{"access_token": "admin-token", "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
		case "/api/v1/admin/users/42/api-keys":
			envelope(w, map[string]any{"items": []map[string]any{{"key": "sk-revoked", "status": "disabled"}}})
		default:
			http.NotFound(w, r)
		}
	})
	if _, err := gateway.Models(context.Background(), "42"); err == nil || !strings.Contains(err.Error(), "no active Gateway API key") {
		t.Fatalf("err=%v", err)
	}
}

func TestGatewayModelsRejectsMalformedCatalog(t *testing.T) {
	gateway := newDirectoryGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			envelope(w, map[string]any{"access_token": "admin-token", "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
		case "/api/v1/admin/users/42/api-keys":
			envelope(w, map[string]any{"items": []map[string]any{{"key": "sk-caller", "status": "active"}}})
		case "/v1/models":
			_, _ = w.Write([]byte(`not-json`))
		default:
			http.NotFound(w, r)
		}
	})
	if _, err := gateway.Models(context.Background(), "42"); err == nil || !strings.Contains(err.Error(), "invalid Gateway model catalog") {
		t.Fatalf("err=%v", err)
	}
}
