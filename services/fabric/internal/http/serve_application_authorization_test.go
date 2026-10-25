package http

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServeApplicationCapabilityCannotMutateResources(t *testing.T) {
	const token = "serve-application-transport-only-token"
	const key = "serve-application-signature-only-key-012345"
	config := ServerAuthConfig{ControlPlaneToken: "control-plane-token", RunnerToken: "runner-token", CapabilityKey: testFabricCapabilityKey, ServeToken: token, ServeCapabilityKey: key, Now: time.Now}
	calls := 0
	handler := authorizeFabricRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }), nil, config)
	// Each case carries the body Serve actually posts on that route, so one
	// capability cannot be replayed from one application route onto another.
	for _, tc := range []struct {
		name, path, caller, signing, action string
		body                               map[string]string
		want                               int
	}{
		{"start", "/fabric/workspace-application-runtimes", "serve", key, "create_workspace_application_runtime",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 204},
		{"readback", "/fabric/workspace-application-runtimes/w/readback", "serve", key, "read_workspace_application_runtime",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 204},
		{"forged-control-plane", "/fabric/workspace-application-runtimes", "control-plane", key, "create_workspace_application_runtime",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 403},
		{"wrong-key", "/fabric/workspace-application-runtimes", "serve", testFabricCapabilityKey, "create_workspace_application_runtime",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 403},
		{"buy-compute", "/fabric/compute-allocations", "serve", key, "create_compute_allocation",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 403},
		{"delete-storage", "/fabric/storage-volumes/v/destroy", "serve", key, "create_storage_volume",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 403},
		{"lifecycle", "/fabric/workspace-application-runtimes/w/lifecycle", "serve", key, "set_workspace_application_runtime_lifecycle",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "runtimeId": "w", "desired": "absent"}, 204},
		{"readback-lifecycle", "/fabric/workspace-application-runtimes/w/lifecycle-readback", "serve", key, "read_workspace_application_runtime_lifecycle",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "runtimeId": "w", "desired": "absent"}, 204},
		{"credentials", "/fabric/workspace-application-runtimes/w/credentials", "serve", key, "read_workspace_application_runtime_credentials",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "runtimeId": "w"}, 204},
		{"preflight", "/fabric/workspace-application-runtimes/w/preflight", "serve", key, "preflight_workspace_application_runtime",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}, 403},
		{"gateway-secret-cleanup", "/fabric/workspace-application-runtimes/w/gateway-secret-cleanup", "serve", key, "remove_workspace_application_gateway_secret",
			map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "secretRef": "s"}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			sum := sha256.Sum256(body)
			claims := fabricCapabilityClaims{Version: 1, Caller: tc.caller, AccountID: "a", WorkspaceID: "w", ResourceKind: "workspace_application_runtime", ResourceID: "w", Action: tc.action, OperationID: "r", ExpiresAt: time.Now().Add(time.Minute).Unix(), BodySHA256: hex.EncodeToString(sum[:])}
			payload, _ := json.Marshal(claims)
			encoded := base64.RawURLEncoding.EncodeToString(payload)
			mac := hmac.New(sha256.New, []byte(tc.signing))
			mac.Write([]byte(encoded))
			r := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Idempotency-Key", "r")
			r.Header.Set(fabricCapabilityHeader, encoded+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	if calls != 5 {
		t.Fatalf("unexpected admitted calls=%d", calls)
	}
}
