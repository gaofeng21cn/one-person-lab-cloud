package clients

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

func TestFabricApplicationRuntimeHTTPPreservesIdentityCapabilityAndPending(t *testing.T) {
	const capabilityKey = "application-runtime-test-capability"
	input := WorkspaceApplicationRuntimeInput{
		AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ComputeID: "compute-alpha", VolumeID: "volume-alpha",
		AttachmentID: "attachment-alpha", AttachmentOperationID: "launch-alpha:attachment", RuntimeOperationID: "deploy-alpha:runtime",
		ConfigurationDigest: strings.Repeat("c", 64),
		Revision: contracts.WorkspaceApplicationRevision{
			SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
			Image: "registry.example/app@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "application", EntryPort: "http",
			Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		},
	}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		action, path := "create_workspace_application_runtime", "/fabric/workspace-application-runtimes"
		if calls > 1 {
			action, path = "read_workspace_application_runtime", "/fabric/workspace-application-runtimes/ws-alpha/readback"
		}
		if r.Method != http.MethodPost || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer internal-secret" || r.Header.Get("Idempotency-Key") != input.RuntimeOperationID {
			t.Errorf("unexpected application runtime request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var decoded WorkspaceApplicationRuntimeInput
		if err := json.Unmarshal(body, &decoded); err != nil || !reflect.DeepEqual(decoded, input) {
			t.Errorf("runtime wire input=%#v err=%v", decoded, err)
		}
		parts := strings.Split(r.Header.Get(FabricCapabilityHeader), ".")
		if len(parts) != 2 {
			t.Error("missing signed capability")
			return
		}
		mac := hmac.New(sha256.New, []byte(capabilityKey))
		_, _ = mac.Write([]byte(parts[0]))
		signature, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
			t.Error("invalid capability signature")
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			t.Error(err)
			return
		}
		var claims fabricCapabilityClaims
		digest := sha256.Sum256(body)
		if err := json.Unmarshal(payload, &claims); err != nil || claims.AccountID != input.AccountID || claims.WorkspaceID != input.WorkspaceID ||
			claims.Caller != "control-plane" || claims.ResourceKind != "workspace_application_runtime" || claims.ResourceID != input.WorkspaceID ||
			claims.Action != action || claims.OperationID != input.RuntimeOperationID || claims.BodySHA256 != hex.EncodeToString(digest[:]) ||
			claims.ExpiresAt <= time.Now().Unix() || claims.ExpiresAt > time.Now().Add(2*time.Minute).Unix() {
			t.Errorf("invalid operation capability: %#v err=%v", claims, err)
		}
		if calls == 4 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":"provider_read_unavailable"}`)
			return
		}
		state := "pending"
		if calls == 3 {
			state = "ready"
		}
		components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
		components[0].State = state
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(contracts.WorkspaceApplicationRuntimeObservation{
			SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: "rt-app-alpha", Status: state,
			Entry: &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-alpha-main", Port: 8080}, Components: components,
		})
	}))
	defer upstream.Close()
	client := NewFabricHTTPClientWithCapability(upstream.URL, "internal-secret", capabilityKey, upstream.Client()).(FabricWorkspaceApplicationRuntimeClient)
	first, err := client.EnsureWorkspaceApplicationRuntime(context.Background(), input, input.RuntimeOperationID)
	if err != nil || first.Status != "pending" || first.RuntimeID != "rt-app-alpha" {
		t.Fatalf("create pending=%#v err=%v", first, err)
	}
	pending, err := client.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || pending.Status != "pending" {
		t.Fatalf("read pending=%#v err=%v", pending, err)
	}
	ready, err := client.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || ready.Status != "ready" || ready.Entry == nil || ready.Entry.ServiceName != "app-runtime-alpha-main" {
		t.Fatalf("read ready=%#v err=%v", ready, err)
	}
	failed, err := client.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err == nil || failed.Status == "ready" || calls != 4 {
		t.Fatalf("failed read reused old ready=%#v err=%v calls=%d", failed, err, calls)
	}
}
