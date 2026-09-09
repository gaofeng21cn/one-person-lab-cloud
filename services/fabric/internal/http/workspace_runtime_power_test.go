package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opl-cloud/services/fabric/internal/fabric"
)

type runtimePowerHTTPProvider struct {
	workspaceLaunchHTTPProvider
	reads, sets int
	state       string
}

func (p *runtimePowerHTTPProvider) ReadWorkspaceRuntimePower(_ context.Context, input fabric.WorkspaceRuntimePowerInput) (fabric.WorkspaceRuntimePowerResult, error) {
	p.reads++
	return fabric.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: p.state}, nil
}
func (p *runtimePowerHTTPProvider) SetWorkspaceRuntimePower(_ context.Context, input fabric.WorkspaceRuntimePowerInput) (fabric.WorkspaceRuntimePowerResult, error) {
	p.sets++
	p.state = input.DesiredState
	return fabric.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: p.state}, nil
}

func runtimePowerHTTPFixture(t *testing.T, input fabric.WorkspaceRuntimePowerInput) (*runtimePowerHTTPProvider, *fabric.MemoryOperationStore, http.Handler) {
	t.Helper()
	store := fabric.NewMemoryOperationStore()
	now := time.Now().Add(-time.Hour)
	runtime := fabric.WorkspaceRuntime{ID: input.RuntimeID, WorkspaceID: input.WorkspaceID, OperationID: input.RuntimeOperationID, Status: "running", Ready: true}
	if err := store.Append(context.Background(), fabric.FabricOperation{
		ID: "original-runtime", OperationID: input.RuntimeOperationID, CallerService: "control-plane", Action: "create_workspace_runtime",
		ResourceKind: "workspace_runtime", ResourceID: input.WorkspaceID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID,
		IdempotencyKey: input.RuntimeOperationID, RequestHash: "runtime-request", Status: "succeeded", CreatedAt: now, StartedAt: now, FinishedAt: now,
		RedactedProviderPayload: map[string]any{"resource": runtime},
	}); err != nil {
		t.Fatal(err)
	}
	provider := &runtimePowerHTTPProvider{state: "running"}
	server := NewServerWithAuth(fabric.NewServiceWithOperationStore(provider, store), ServerAuthConfig{ControlPlaneToken: "internal-secret", RunnerToken: "runner-secret", CapabilityKey: testFabricCapabilityKey})
	return provider, store, server
}

func TestWorkspaceRuntimePowerHTTPSeparatesReadFromMutationCapability(t *testing.T) {
	input := fabric.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-alpha", WorkspaceID: "ws-alpha", RuntimeID: "runtime-alpha", RuntimeOperationID: "launch-alpha:runtime", PaidThrough: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), DesiredState: "suspended", IdempotencyKey: "power-alpha"}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	provider, store, server := runtimePowerHTTPFixture(t, input)
	claims := fabricCapabilityClaimsForTest{Version: 1, Caller: "control-plane", AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_runtime_power", ResourceID: input.WorkspaceID, Action: "set_workspace_runtime_power", OperationID: input.IdempotencyKey, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	for _, mutate := range []bool{false, true, true, false} {
		path := "/fabric/workspace-runtimes/power/read"
		if mutate {
			path = "/fabric/workspace-runtimes/power"
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer internal-secret")
		if mutate {
			req.Header.Set("Idempotency-Key", input.IdempotencyKey)
			req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, claims, body))
		}
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		var result fabric.WorkspaceRuntimePowerResult
		if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &result) != nil || result.Binding != input {
			t.Fatalf("response=%d %s", res.Code, res.Body.String())
		}
		if !mutate && provider.sets == 0 && result.State != "running" {
			t.Fatalf("read mutated runtime: %#v", result)
		}
		if provider.sets > 1 {
			t.Fatalf("replayed request mutated %d times", provider.sets)
		}
		operations, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !mutate && provider.sets == 0 && len(operations) != 1 {
			t.Fatalf("read created operation: %#v", operations)
		}
	}
	if provider.sets != 1 || provider.state != "suspended" {
		t.Fatalf("sets=%d state=%s", provider.sets, provider.state)
	}
}

func TestWorkspaceRuntimePowerHTTPRejectsMismatchedAuthorityBeforeMutation(t *testing.T) {
	input := fabric.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: "acct-alpha", WorkspaceID: "ws-alpha", RuntimeID: "runtime-alpha", RuntimeOperationID: "launch-alpha:runtime", PaidThrough: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), DesiredState: "suspended", IdempotencyKey: "power-alpha"}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	base := fabricCapabilityClaimsForTest{Version: 1, Caller: "control-plane", AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_runtime_power", ResourceID: input.WorkspaceID, Action: "set_workspace_runtime_power", OperationID: input.IdempotencyKey, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	for _, tc := range []struct {
		name                   string
		claims                 func(*fabricCapabilityClaimsForTest)
		input                  func(*fabric.WorkspaceRuntimePowerInput)
		token                  string
		key                    string
		unsigned, tamperedBody bool
		want                   int
	}{
		{name: "missing capability", unsigned: true, want: http.StatusForbidden},
		{name: "runner token", token: "runner-secret", want: http.StatusForbidden},
		{name: "wrong account", claims: func(c *fabricCapabilityClaimsForTest) { c.AccountID = "acct-other" }, want: http.StatusForbidden},
		{name: "wrong workspace", claims: func(c *fabricCapabilityClaimsForTest) { c.WorkspaceID = "ws-other" }, want: http.StatusForbidden},
		{name: "wrong resource kind", claims: func(c *fabricCapabilityClaimsForTest) { c.ResourceKind = "workspace_runtime" }, want: http.StatusForbidden},
		{name: "wrong resource id", claims: func(c *fabricCapabilityClaimsForTest) { c.ResourceID = "ws-other" }, want: http.StatusForbidden},
		{name: "wrong action", claims: func(c *fabricCapabilityClaimsForTest) { c.Action = "destroy_workspace_runtime" }, want: http.StatusForbidden},
		{name: "wrong operation", claims: func(c *fabricCapabilityClaimsForTest) { c.OperationID = "other-operation" }, want: http.StatusForbidden},
		{name: "wrong request key", key: "other-key", want: http.StatusForbidden},
		{name: "wrong signed body", input: func(i *fabric.WorkspaceRuntimePowerInput) { i.RuntimeID = "other-runtime" }, tamperedBody: true, want: http.StatusForbidden},
		{name: "wrong body key", input: func(i *fabric.WorkspaceRuntimePowerInput) { i.IdempotencyKey = "other-key" }, want: http.StatusBadRequest},
		{name: "wrong bound runtime", input: func(i *fabric.WorkspaceRuntimePowerInput) { i.RuntimeID = "other-runtime" }, want: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, store, server := runtimePowerHTTPFixture(t, input)
			claims, requestInput := base, input
			if tc.claims != nil {
				tc.claims(&claims)
			}
			if tc.input != nil {
				tc.input(&requestInput)
			}
			requestBody, err := json.Marshal(requestInput)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/fabric/workspace-runtimes/power", bytes.NewReader(requestBody))
			token := tc.token
			if token == "" {
				token = "internal-secret"
			}
			req.Header.Set("Authorization", "Bearer "+token)
			key := tc.key
			if key == "" {
				key = input.IdempotencyKey
			}
			req.Header.Set("Idempotency-Key", key)
			if !tc.unsigned {
				signedBody := requestBody
				if tc.tamperedBody {
					signedBody = body
				}
				req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, claims, signedBody))
			}
			res := httptest.NewRecorder()
			server.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			operations, err := store.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if provider.reads != 0 || provider.sets != 0 || len(operations) != 1 {
				t.Fatalf("rejected request reached provider: reads=%d sets=%d operations=%d", provider.reads, provider.sets, len(operations))
			}
		})
	}
}
