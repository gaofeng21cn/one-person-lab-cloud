package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/fabric/internal/fabric"
)

type closeoutHTTPProvider struct{ workspaceLaunchHTTPProvider }

func (closeoutHTTPProvider) ObserveWorkspaceRuntimeDelete(_ context.Context, id string) (fabric.WorkspaceRuntimeDeleteObservation, error) {
	return fabric.WorkspaceRuntimeDeleteObservation{SchemaVersion: 1, WorkspaceID: id, State: "absent"}, nil
}
func TestWorkspaceLaunchCloseoutHTTPRequiresExactMutationCapabilityAndKeepsPreviewReadOnly(t *testing.T) {
	ctx := context.Background()
	store := fabric.NewMemoryOperationStore()
	service := fabric.NewServiceWithOperationStore(closeoutHTTPProvider{}, store)
	preflight, err := service.PreflightWorkspaceLaunch(ctx, fabric.WorkspaceLaunchPreflightInput{SchemaVersion: 1, LaunchOperationID: "launch-closeout", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: "ghcr.io/gaofeng21cn/one-person-lab-app@sha256:" + strings.Repeat("a", 64), RequestHash: strings.Repeat("b", 64)})
	if err != nil || !preflight.Available {
		t.Fatalf("preflight=%#v err=%v", preflight, err)
	}
	input := fabric.WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: "launch-closeout", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ProviderProfileRef: preflight.ProviderProfileRef, ProviderBindingRef: preflight.ProviderBindingRef, SpecDigest: preflight.SpecDigest, IdempotencyKey: "close-original"}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithAuth(service, ServerAuthConfig{ControlPlaneToken: "internal-secret", RunnerToken: "runner-secret", CapabilityKey: testFabricCapabilityKey})
	claims := fabricCapabilityClaimsForTest{Version: 1, Caller: "control-plane", AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ResourceKind: "workspace_launch_closeout", ResourceID: input.LaunchOperationID, Action: "closeout_workspace_launch", OperationID: input.IdempotencyKey, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	for _, tc := range []struct {
		name, path, token, kind string
		status                  int
	}{
		{"unauthenticated preview", "/fabric/workspace-launches/closeout/read", "", "", http.StatusUnauthorized},
		{"read only preview", "/fabric/workspace-launches/closeout/read", "internal-secret", "", http.StatusOK},
		{"freeze without capability", "/fabric/workspace-launches/closeout/freeze", "internal-secret", "", http.StatusForbidden},
		{"closeout wrong capability", "/fabric/workspace-launches/closeout", "internal-secret", "workspace_runtime", http.StatusForbidden},
		{"runner cannot closeout", "/fabric/workspace-launches/closeout", "runner-secret", "workspace_launch_closeout", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			request.Header.Set("Idempotency-Key", input.IdempotencyKey)
			if tc.kind != "" {
				changed := claims
				changed.ResourceKind = tc.kind
				request.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, changed, body))
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			ops, _ := store.List(ctx)
			if len(ops) != 1 {
				t.Fatalf("preview/rejected mutation changed operation count=%d", len(ops))
			}
		})
	}
	for _, path := range []string{"/fabric/workspace-launches/closeout/freeze", "/fabric/workspace-launches/closeout"} {
		request := testRequest(http.MethodPost, path, bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer internal-secret")
		request.Header.Set("Idempotency-Key", input.IdempotencyKey)
		request.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, claims, body))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		var result fabric.WorkspaceLaunchCloseoutResult
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.Frozen || result.State != "absent" || result.Binding != input || len(result.Resources) != 5 {
			t.Fatalf("closeout response=%d %s", response.Code, response.Body.String())
		}
		stages := map[string]bool{}
		for _, resource := range result.Resources {
			if resource.State != "absent" || stages[resource.Stage] {
				t.Fatalf("invalid absence evidence: %#v", result.Resources)
			}
			stages[resource.Stage] = true
		}
		for _, stage := range []string{"runtime", "secret", "storage", "ensure_compute_allocation", "attachment"} {
			if !stages[stage] {
				t.Fatalf("missing %s readback", stage)
			}
		}
	}
}
