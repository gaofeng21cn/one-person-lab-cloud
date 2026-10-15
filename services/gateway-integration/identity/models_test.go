package identity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestGatewayUsageStatsReadsDirectoryScopedTotals(t *testing.T) {
	gateway := newDirectoryGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			envelope(w, map[string]any{"access_token": "admin-token", "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
		case "/api/v1/admin/usage/stats":
			if r.Header.Get("Authorization") != "Bearer admin-token" || r.URL.Query().Get("user_id") != "42" || r.URL.Query().Get("timezone") != "Asia/Shanghai" {
				t.Fatalf("usage request auth=%q query=%v", r.Header.Get("Authorization"), r.URL.Query())
			}
			if r.URL.Query().Get("start_date") == "" || r.URL.Query().Get("end_date") == "" {
				t.Fatalf("usage date range=%v", r.URL.Query())
			}
			envelope(w, map[string]any{"total_requests": 17, "total_input_tokens": 170, "total_output_tokens": 34, "total_tokens": 204, "total_actual_cost": "0.1250019"})
		default:
			http.NotFound(w, r)
		}
	})

	stats, err := gateway.UsageStats(context.Background(), "42", "month")
	if err != nil {
		t.Fatalf("usage stats: %v", err)
	}
	if stats.TotalRequests != 17 || stats.TotalInputTokens != 170 || stats.TotalOutputTokens != 34 || stats.TotalTokens != 204 || stats.TotalActualCostUSDMicros != 125001 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestGatewayUsageStatsInvalidatesRejectedCachedToken(t *testing.T) {
	loginCalls := 0
	usageCalls := 0
	gateway := newDirectoryGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginCalls++
			envelope(w, map[string]any{"access_token": fmt.Sprintf("admin-token-%d", loginCalls), "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
		case "/api/v1/admin/usage/stats":
			usageCalls++
			if usageCalls == 2 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			wantToken := fmt.Sprintf("Bearer admin-token-%d", loginCalls)
			if r.Header.Get("Authorization") != wantToken {
				t.Fatalf("usage auth=%q want %q", r.Header.Get("Authorization"), wantToken)
			}
			envelope(w, map[string]any{"total_requests": 1, "total_input_tokens": 2, "total_output_tokens": 3, "total_tokens": 5, "total_actual_cost": "0.1"})
		default:
			http.NotFound(w, r)
		}
	})

	if _, err := gateway.UsageStats(context.Background(), "42", "month"); err != nil {
		t.Fatalf("initial usage stats: %v", err)
	}
	if _, err := gateway.UsageStats(context.Background(), "42", "month"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("rejected cached token error=%v", err)
	}
	if _, err := gateway.UsageStats(context.Background(), "42", "month"); err != nil {
		t.Fatalf("usage stats after reauthentication: %v", err)
	}
	if loginCalls != 2 {
		t.Fatalf("login calls=%d want 2", loginCalls)
	}
}

func TestGatewayUsageStatsFailsClosedForInvalidUpstreamFacts(t *testing.T) {
	for _, payload := range []map[string]any{
		{"total_requests": -1, "total_input_tokens": 170, "total_output_tokens": 34, "total_tokens": 204, "total_actual_cost": "0.1"},
		{"total_requests": 17, "total_input_tokens": 170, "total_output_tokens": 34, "total_tokens": 204},
		{"total_requests": 17, "total_input_tokens": 170, "total_output_tokens": 34, "total_tokens": 204, "total_actual_cost": "not-a-number"},
	} {
		payload := payload
		t.Run(strings.ReplaceAll(fmt.Sprint(payload["total_requests"]), " ", "_"), func(t *testing.T) {
			gateway := newDirectoryGateway(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/login":
					envelope(w, map[string]any{"access_token": "admin-token", "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
				case "/api/v1/admin/usage/stats":
					envelope(w, payload)
				default:
					http.NotFound(w, r)
				}
			})
			if _, err := gateway.UsageStats(context.Background(), "42", "month"); err == nil {
				t.Fatal("malformed Gateway usage stats were accepted")
			}
		})
	}
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
