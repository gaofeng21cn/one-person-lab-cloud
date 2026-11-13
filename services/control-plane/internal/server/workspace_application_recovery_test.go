package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestRetiredWorkspaceApplicationCommandsHTTPPreserveHistory(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	ctx := context.Background()
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceKeyGroupID: 5, Revision: defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("e", 64)), Phase: "failed", LastError: "test_installation_failed", WorkspaceAPIKeyID: 19, GatewaySecret: &clients.GatewaySecretWriteResult{SecretRef: "secret-ws-alpha", Version: "v1"}}
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
	operations, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _, _ := fixture.store.GetWorkspace(ctx, request.WorkspaceID)
	var externalCalls []string
	transport := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		externalCalls = append(externalCalls, r.Method+" "+r.URL.String())
		return nil, errors.New("unexpected retired installation request")
	})}
	sub2API, err := clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{BaseURL: "https://sub2api.example.test", AdminEmail: "operator@example.test", AdminPassword: "test-password", Timeout: time.Second}, transport)
	if err != nil {
		t.Fatal(err)
	}
	handler := fixture.server.(*controlPlaneHTTPHandler)
	*handler.service = *controlplane.NewService(clients.NewLedgerHTTPClient("https://ledger.example.test", "test-token", transport), clients.NewFabricHTTPClientWithCapability("https://fabric.example.test", "test-token", "test-capability", transport), sub2API)
	for _, path := range []string{"/api/workspaces/ws-alpha/application-installation/resume", "/api/workspaces/not-owned/application-installation/resume", "/api/workspaces/ws-alpha/runtime-credentials/rotate", "/api/workspaces/not-owned/runtime-credentials/rotate"} {
		for range 2 {
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, "{}", "resume-retired")
			if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("retired application command=%d %s", response.Code, response.Body.String())
			}
		}
	}
	retained, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil || !reflect.DeepEqual(retained, operations) {
		t.Fatalf("retired application command changed operation history: %v", err)
	}
	after, _, err := fixture.store.GetWorkspace(ctx, request.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, workspace) || len(externalCalls) != 0 {
		t.Fatalf("retired application command changed workspace or called external owners: calls=%v err=%v", externalCalls, err)
	}
	projection := map[string]any{}
	mustStore(t, handler.app.projectWorkspaceApplicationInstallation(ctx, workspace, projection))
	installation, ok := projection["applicationInstallation"].(workspaceApplicationInstallationProjection)
	if !ok || installation.OperationID != request.OperationID || installation.ApplicationID != request.Revision.ApplicationID || installation.Revision != request.Revision.Version || installation.Status != "manual_review" || installation.CanRetry {
		t.Fatalf("historical installation projection=%+v", projection)
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["applicationInstallation"]["canResume"]; exists {
		t.Fatalf("retired customer action leaked into history: %s", payload)
	}
}
