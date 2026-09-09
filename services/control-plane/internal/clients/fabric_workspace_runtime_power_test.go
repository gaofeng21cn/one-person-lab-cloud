package clients

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

func TestFabricWorkspaceRuntimePowerClientUsesTypedRoutesAndScopesOnlySet(t *testing.T) {
	const capabilityKey = "runtime-power-capability-key"
	input := contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-alpha", WorkspaceID: "ws-alpha", RuntimeID: "runtime-alpha", RuntimeOperationID: "launch-alpha:runtime", PaidThrough: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), DesiredState: "suspended", IdempotencyKey: "power-alpha"}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fabric-token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		var received contracts.WorkspaceRuntimePowerInput
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		if received != input {
			t.Fatalf("typed body changed: %#v", received)
		}
		if r.URL.Path == "/fabric/workspace-runtimes/power/read" {
			if r.Header.Get("Idempotency-Key") != "" || r.Header.Get(FabricCapabilityHeader) != "" {
				t.Fatal("read must not carry mutation headers")
			}
		} else {
			if r.Header.Get("Idempotency-Key") != input.IdempotencyKey {
				t.Fatalf("idempotency=%q", r.Header.Get("Idempotency-Key"))
			}
			parts := strings.Split(r.Header.Get(FabricCapabilityHeader), ".")
			if len(parts) != 2 {
				t.Fatalf("capability=%q", r.Header.Get(FabricCapabilityHeader))
			}
			mac := hmac.New(sha256.New, []byte(capabilityKey))
			_, _ = mac.Write([]byte(parts[0]))
			sig, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
				t.Fatalf("capability signature: %v", err)
			}
			payload, err := base64.RawURLEncoding.DecodeString(parts[0])
			if err != nil {
				t.Fatal(err)
			}
			var claims fabricCapabilityClaims
			if err := json.Unmarshal(payload, &claims); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(mustJSON(t, input))
			if claims.Version != 1 || claims.Caller != "control-plane" || claims.AccountID != input.AccountID || claims.WorkspaceID != input.WorkspaceID || claims.ResourceKind != "workspace_runtime_power" || claims.ResourceID != input.WorkspaceID || claims.Action != "set_workspace_runtime_power" || claims.OperationID != input.IdempotencyKey || claims.BodySHA256 != hex.EncodeToString(digest[:]) || claims.ExpiresAt <= time.Now().Unix() {
				t.Fatalf("claims=%#v", claims)
			}
		}
		_ = json.NewEncoder(w).Encode(contracts.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: input.DesiredState})
	}))
	defer server.Close()
	client := NewFabricHTTPClientWithCapability(server.URL, "fabric-token", capabilityKey, server.Client()).(FabricWorkspaceRuntimePowerClient)
	if result, err := client.ReadWorkspaceRuntimePower(context.Background(), input); err != nil || result.Binding != input {
		t.Fatalf("read=%#v err=%v", result, err)
	}
	if result, err := client.SetWorkspaceRuntimePower(context.Background(), input); err != nil || result.Binding != input || result.State != input.DesiredState {
		t.Fatalf("set=%#v err=%v", result, err)
	}
	if len(paths) != 2 || paths[0] != "/fabric/workspace-runtimes/power/read" || paths[1] != "/fabric/workspace-runtimes/power" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestFabricWorkspaceRuntimePowerClientRejectsSetWithoutIdempotencyKey(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewFabricHTTPClientWithCapability(server.URL, "token", "capability", server.Client()).(FabricWorkspaceRuntimePowerClient)
	input := contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct", WorkspaceID: "ws", RuntimeID: "rt", RuntimeOperationID: "op", PaidThrough: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), DesiredState: "running"}
	if _, err := client.SetWorkspaceRuntimePower(context.Background(), input); err == nil || !strings.Contains(err.Error(), "idempotency key is required") {
		t.Fatalf("err=%v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("missing idempotency key sent %d requests", got)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
