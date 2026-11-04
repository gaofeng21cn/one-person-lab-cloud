package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

func TestFabricApplicationBridgeLifecycleIdentity(t *testing.T) {
	bridge, requests := lifecycleJournalBridge(t)
	input := contracts.WorkspaceApplicationRuntimeInput{AccountID: "account", WorkspaceID: "workspace", RuntimeOperationID: "runtime-original"}
	type command struct{ key, desired string }
	keys := make(map[command]string)
	owners := make(map[string]command)
	for _, step := range []command{
		{"upgrade-first", "suspended"}, {"upgrade-first", "suspended"},
		{"upgrade-first", "running"}, {"upgrade-first", "running"},
		{"upgrade-second", "suspended"}, {"upgrade-second", "suspended"},
		{"delete", "absent"}, {"delete", "absent"},
	} {
		input.IdempotencyKey = step.key
		result, err := bridge.SetWorkspaceApplicationRuntimeLifecycle(t.Context(), input, step.desired)
		if err != nil {
			t.Fatalf("lifecycle %s %s: %v", step.key, step.desired, err)
		}
		request := <-requests
		if request.RuntimeOperationID != input.RuntimeOperationID || request.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) || request.AccountID != input.AccountID || request.WorkspaceID != input.WorkspaceID || request.DesiredState != step.desired {
			t.Fatalf("lifecycle changed its original resource identity: %+v", request)
		}
		if result.RuntimeID != request.RuntimeID || result.WorkspaceID != input.WorkspaceID || result.State != step.desired {
			t.Fatalf("lifecycle readback=%+v", result)
		}
		if prior, ok := keys[step]; ok && prior != request.IdempotencyKey {
			t.Fatalf("same command changed retry identity: %q != %q", prior, request.IdempotencyKey)
		}
		if prior, ok := owners[request.IdempotencyKey]; ok && prior != step {
			t.Fatalf("different commands share an identity: %+v and %+v", prior, step)
		}
		keys[step], owners[request.IdempotencyKey] = request.IdempotencyKey, step
	}
	// The same deletion command addresses each runtime with its own action identity.
	input.RuntimeOperationID = "runtime-successor"
	if _, err := bridge.SetWorkspaceApplicationRuntimeLifecycle(t.Context(), input, "absent"); err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request.IdempotencyKey == keys[command{"delete", "absent"}] || request.RuntimeOperationID != input.RuntimeOperationID || request.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) {
		t.Fatalf("successor lifecycle identity=%+v", request)
	}
}

func TestAgentExecutionLifecycleCarriesCommandIdentity(t *testing.T) {
	bridge, requests := lifecycleJournalBridge(t)
	command := executionCommand(t)
	command.Context = &api.CallContext{IdempotencyKey: "workspace-delete:retire_runtime"}
	adapter := &agentExecutionAdapter{Executor: bridge}
	if err := adapter.Lifecycle(t.Context(), command, executionTargetFixture(command), "absent"); err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.IdempotencyKey != stableID("app_lifecycle_", command.GetContext().GetIdempotencyKey(), command.GetRuntimeInstanceId(), "absent") || request.RuntimeOperationID != command.GetRuntimeInstanceId() {
		t.Fatalf("adapter lost the command or runtime identity: %+v", request)
	}
}

func TestAgentExecutionLifecycleRequiresCommandIdentity(t *testing.T) {
	for _, key := range []string{"", "  "} {
		command := executionCommand(t)
		if key != "" {
			command.Context = &api.CallContext{IdempotencyKey: key}
		}
		fake := &fakeExecutor{}
		if err := executionAdapter(fake, RouteOrigin{}).Lifecycle(t.Context(), command, executionTargetFixture(command), "absent"); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("missing command identity err=%v", err)
		}
		if fake.calls != 0 {
			t.Fatal("missing command identity reached the executor")
		}
	}
}

func TestFabricApplicationBridgeLifecycleRequiresCommandIdentity(t *testing.T) {
	bridge, requests := lifecycleJournalBridge(t)
	for _, key := range []string{"", "  "} {
		input := contracts.WorkspaceApplicationRuntimeInput{AccountID: "account", WorkspaceID: "workspace", RuntimeOperationID: "runtime-original", IdempotencyKey: key}
		if _, err := bridge.SetWorkspaceApplicationRuntimeLifecycle(t.Context(), input, "absent"); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("missing command identity err=%v", err)
		}
	}
	if len(requests) != 0 {
		t.Fatal("missing command identity reached Fabric")
	}
}

func TestFabricApplicationBridgeLifecycleReadUsesRuntimeIdentity(t *testing.T) {
	input := contracts.WorkspaceApplicationRuntimeInput{AccountID: "account", WorkspaceID: "workspace", RuntimeOperationID: "runtime-original", IdempotencyKey: "lifecycle-command"}
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.Header.Get("X-OPL-Fabric-Capability"), ".")
		var claims map[string]any
		if len(parts) != 2 {
			t.Error("missing read capability")
			http.Error(w, "invalid capability", http.StatusBadRequest)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil || json.Unmarshal(raw, &claims) != nil || claims["operationId"] != input.RuntimeOperationID || r.URL.Path != "/fabric/workspace-application-runtimes/workspace/lifecycle-readback" {
			t.Error("lifecycle read changed its runtime identity")
			http.Error(w, "invalid read", http.StatusBadRequest)
			return
		}
		requests <- r.Header.Get("Idempotency-Key")
		if err := json.NewEncoder(w).Encode(contracts.WorkspaceApplicationRuntimeLifecycleResult{}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	bridge := &fabricApplicationBridge{BaseURL: server.URL, Token: "token", CapabilityKey: strings.Repeat("k", 32), Client: server.Client()}
	if _, err := bridge.ReadWorkspaceApplicationRuntimeLifecycle(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if key := <-requests; key != input.RuntimeOperationID {
		t.Fatalf("read key=%q, want original runtime %q", key, input.RuntimeOperationID)
	}
}

func TestFabricApplicationBridgeLifecycleRejectsChangedInput(t *testing.T) {
	bridge, requests := lifecycleJournalBridge(t)
	input := contracts.WorkspaceApplicationRuntimeInput{AccountID: "account", WorkspaceID: "workspace", RuntimeOperationID: "runtime-original", IdempotencyKey: "delete"}
	if _, err := bridge.SetWorkspaceApplicationRuntimeLifecycle(t.Context(), input, "absent"); err != nil {
		t.Fatal(err)
	}
	<-requests
	input.WorkspaceID = "another-workspace"
	if _, err := bridge.SetWorkspaceApplicationRuntimeLifecycle(t.Context(), input, "absent"); err == nil || !strings.Contains(err.Error(), "runtime_idempotency_conflict") {
		t.Fatalf("changed input bypassed the original journal: %v", err)
	}
}

// Exercise the real bridge HTTP request against Fabric's journal rule: a
// lifecycle action/key may only replay an identical typed input hash, and only
// the latest command may replay. This is
// an HTTP fake, not a replacement for Fabric consumer integration coverage.
func lifecycleJournalBridge(t *testing.T) (*fabricApplicationBridge, <-chan contracts.WorkspaceApplicationRuntimeLifecycleInput) {
	t.Helper()
	const token, key = "lifecycle-test-token", "lifecycle-test-signature-key-000001"
	requests := make(chan contracts.WorkspaceApplicationRuntimeLifecycleInput, 8)
	journal := make(map[string][sha256.Size]byte)
	latest := make(map[string]string)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		var input contracts.WorkspaceApplicationRuntimeLifecycleInput
		parts := strings.Split(r.Header.Get("X-OPL-Fabric-Capability"), ".")
		if json.Unmarshal(body, &input) != nil || len(parts) != 2 {
			t.Error("invalid lifecycle request or capability")
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		claimsJSON, decodeErr := base64.RawURLEncoding.DecodeString(parts[0])
		var claims map[string]any
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(parts[0]))
		sum := sha256.Sum256(body)
		if decodeErr != nil || json.Unmarshal(claimsJSON, &claims) != nil || parts[1] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
			t.Error("invalid signed lifecycle capability")
			http.Error(w, "invalid capability", http.StatusUnauthorized)
			return
		}
		input.IdempotencyKey = r.Header.Get("Idempotency-Key")
		if r.Method != http.MethodPost || r.URL.Path != "/fabric/workspace-application-runtimes/"+input.WorkspaceID+"/lifecycle" || r.Header.Get("Authorization") != "Bearer "+token || input.IdempotencyKey == "" {
			t.Error("invalid lifecycle endpoint, credentials or command identity")
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		for field, expected := range map[string]string{
			"caller": "serve", "action": "set_workspace_application_runtime_lifecycle",
			"resourceKind": "workspace_application_runtime", "resourceId": input.RuntimeID,
			"accountId": input.AccountID, "workspaceId": input.WorkspaceID,
			"operationId": input.IdempotencyKey, "bodySha256": hex.EncodeToString(sum[:]),
		} {
			if claims[field] != expected {
				t.Errorf("capability %s=%v, want %s", field, claims[field], expected)
				http.Error(w, "capability scope mismatch", http.StatusForbidden)
				return
			}
		}
		// Fabric hashes the decoded contract; IdempotencyKey is excluded by json:"-".
		raw, err := json.Marshal(input)
		if err != nil {
			t.Error(err)
			http.Error(w, "invalid input", http.StatusBadRequest)
			return
		}
		digest := sha256.Sum256(raw)
		mu.Lock()
		prior, exists := journal[input.IdempotencyKey]
		if exists && prior != digest {
			mu.Unlock()
			http.Error(w, "runtime_idempotency_conflict", http.StatusConflict)
			return
		}
		if exists && latest[input.RuntimeID] != input.IdempotencyKey {
			mu.Unlock()
			http.Error(w, "workspace_application_lifecycle_superseded", http.StatusConflict)
			return
		}
		journal[input.IdempotencyKey] = digest
		latest[input.RuntimeID] = input.IdempotencyKey
		mu.Unlock()
		requests <- input
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: input.DesiredState}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return &fabricApplicationBridge{BaseURL: server.URL, Token: token, CapabilityKey: key, Client: server.Client()}, requests
}
