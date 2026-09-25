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
	input := map[string]string{"accountId": "a", "workspaceId": "w", "runtimeOperationId": "r", "id": "c"}
	body, _ := json.Marshal(input)
	sum := sha256.Sum256(body)
	for _, tc := range []struct {
		name, path, caller, signing string
		want                        int
	}{
		{"start", "/fabric/workspace-application-runtimes", "serve", key, 204},
		{"readback", "/fabric/workspace-application-runtimes/w/readback", "serve", key, 204},
		{"forged-control-plane", "/fabric/workspace-application-runtimes", "control-plane", key, 403},
		{"wrong-key", "/fabric/workspace-application-runtimes", "serve", testFabricCapabilityKey, 403},
		{"buy-compute", "/fabric/compute-allocations", "serve", key, 403},
		{"delete-storage", "/fabric/storage-volumes/v/destroy", "serve", key, 403},
		{"credentials", "/fabric/workspace-application-runtimes/w/credentials", "serve", key, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action := "create_workspace_application_runtime"
			if tc.name == "readback" {
				action = "read_workspace_application_runtime"
			}
			claims := fabricCapabilityClaims{Version: 1, Caller: tc.caller, AccountID: "a", WorkspaceID: "w", ResourceKind: "workspace_application_runtime", ResourceID: "w", Action: action, OperationID: "r", ExpiresAt: time.Now().Add(time.Minute).Unix(), BodySHA256: hex.EncodeToString(sum[:])}
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
	if calls != 2 {
		t.Fatalf("unexpected admitted calls=%d", calls)
	}
}
