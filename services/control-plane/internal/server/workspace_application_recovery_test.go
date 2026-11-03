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

func TestWorkspaceApplicationRecoveryMemory(t *testing.T) {
	testWorkspaceApplicationRecovery(t, newMemoryTableStore())
}

func TestWorkspaceApplicationRecoveryPostgres(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	testWorkspaceApplicationRecovery(t, store)
}

func testWorkspaceApplicationRecovery(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx := context.Background()
	service := newTestService(&fakeLedgerClient{}, &fakeFabricClient{})
	handler, id := applicationDeploymentWorkerFixture(t, store, service)
	app := handler.app
	mustStore(t, app.runWorkspaceApplicationDeployment(ctx, service, id))
	row, _, _ := store.GetRuntimeOperation(ctx, id)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentActivatingPhase {
		t.Fatalf("initial deployment phase=%s err=%v", intent.Phase, err)
	}
	mustStore(t, app.markWorkspaceApplicationManualReview(ctx, row, intent, "test_activation_unconfirmed"))
	failed, _, _ := store.GetRuntimeOperation(ctx, id)
	workspace, _, _ := store.GetWorkspace(ctx, intent.WorkspaceID)
	// The PostgreSQL projection omits zero-valued fields; SaveWorkspace changes
	// only fields present in the input. Restore the empty selection/version
	// explicitly so each rejection probe starts from the same reserved command.
	workspace["currentApplicationDeploymentId"] = intent.PreviousDeploymentID
	workspace["applicationBindingVersion"] = intent.ExpectedWorkspaceVersion
	for _, test := range []struct {
		field string
		value any
	}{
		{"reservedApplicationDeploymentId", "newer-operation"},
		{"currentApplicationDeploymentId", "different-current"},
		{"applicationBindingVersion", intent.ExpectedWorkspaceVersion + 1},
	} {
		changed := cloneMap(workspace)
		changed[test.field] = test.value
		mustStore(t, store.SaveWorkspace(ctx, changed))
		if _, err := store.ResumeWorkspaceApplicationDeployment(ctx, id); !errors.Is(err, errWorkspaceApplicationRecoveryConflict) {
			t.Fatalf("recovery ignored %s fence: %v", test.field, err)
		}
		retained, _, _ := store.GetRuntimeOperation(ctx, id)
		if retained["result"] != failed["result"] {
			t.Fatal("rejected recovery mutated failed command")
		}
		mustStore(t, store.SaveWorkspace(ctx, workspace))
	}
	suspended := cloneMap(workspace)
	suspended["state"] = "suspended"
	if _, err := workspaceApplicationRecoveryRow(suspended, failed, nil); !errors.Is(err, errWorkspaceApplicationRecoveryConflict) {
		t.Fatalf("recovery ignored suspended lifecycle: %v", err)
	}
	expired := cloneMap(workspace)
	expired["resourceBillingEnabled"], expired["paidThrough"] = true, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := workspaceApplicationRecoveryRow(expired, failed, nil); !errors.Is(err, errWorkspaceApplicationRecoveryConflict) {
		t.Fatalf("recovery ignored expired entitlement: %v", err)
	}
	resumed, err := store.ResumeWorkspaceApplicationDeployment(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := decodeWorkspaceApplicationDeploymentIntent(resumed)
	if err != nil || recovered.Phase != intent.Phase || recovered.FailurePhase != "" || recovered.RequestHash != intent.RequestHash || recovered.RuntimeObservation == nil {
		t.Fatalf("recovery changed frozen command or lost runtime: %+v %v", recovered, err)
	}
	replay, err := store.ResumeWorkspaceApplicationDeployment(ctx, id)
	if err != nil || replay["result"] != resumed["result"] {
		t.Fatalf("recovery replay changed operation: %v", err)
	}
	stale, _ := decodeWorkspaceApplicationDeploymentIntent(failed)
	if err := app.persistWorkspaceApplicationDeployment(ctx, failed, stale, "manual_review"); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
		t.Fatalf("stale failure overwrote recovery: %v", err)
	}
	runDeploymentWorkerToCompletion(t, app, service, id)
	selectedWorkspace, _, _ := store.GetWorkspace(ctx, intent.WorkspaceID)
	selected, found, err := app.currentWorkspaceApplicationDeployment(ctx, selectedWorkspace)
	if err != nil || !found || selected.OperationID != id || selected.Phase != workspaceApplicationDeploymentActivePhase {
		t.Fatalf("recovered deployment did not activate: %+v %v", selected, err)
	}
	// A failed receipt resumes against the newly committed binding/version.
	active, _, _ := store.GetRuntimeOperation(ctx, id)
	selected.Phase = workspaceApplicationDeploymentReceiptPhase
	mustStore(t, app.markWorkspaceApplicationManualReview(ctx, active, selected, "test_receipt_unavailable"))
	if _, err := store.ResumeWorkspaceApplicationDeployment(ctx, id); err != nil {
		t.Fatalf("post-activation recovery rejected current binding: %v", err)
	}
	runDeploymentWorkerToCompletion(t, app, service, id)
}

// Recovery continues the same accepted deployment through the retained operator
// route; store-only tests cannot detect that this HTTP entry was withdrawn.
func TestWorkspaceApplicationRecoveryHTTP(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	ctx := context.Background()
	handler := fixture.server.(*controlPlaneHTTPHandler)
	app := handler.app
	operator := operatorSessionForTest(t, fixture.server)
	intent := createKnowledgeDeploymentForTest(t, app, "recover-http")
	row, _, _ := fixture.store.GetRuntimeOperation(ctx, intent.OperationID)
	mustStore(t, app.markWorkspaceApplicationManualReview(ctx, row, intent, "test_preflight_unavailable"))
	failed, _, _ := fixture.store.GetRuntimeOperation(ctx, intent.OperationID)
	workspace, _, _ := fixture.store.GetWorkspace(ctx, intent.WorkspaceID)
	projection := map[string]any{}
	mustStore(t, app.projectWorkspaceApplicationInstallation(ctx, workspace, projection))
	installation, ok := projection["applicationInstallation"].(workspaceApplicationInstallationProjection)
	if !ok || !installation.CanRetry || installation.Status != "manual_review" || installation.OperationID != intent.OperationID || installation.ApplicationID != intent.ApplicationID || installation.Revision != intent.TargetRevision {
		t.Fatalf("operator recovery projection=%+v", projection)
	}
	expired := cloneMap(workspace)
	expired["resourceBillingEnabled"], expired["paidThrough"] = true, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	expiredProjection := map[string]any{}
	mustStore(t, app.projectWorkspaceApplicationInstallation(ctx, expired, expiredProjection))
	if expiredProjection["applicationInstallation"].(workspaceApplicationInstallationProjection).CanRetry {
		t.Fatal("expired entitlement offered operator retry")
	}
	path := "/api/operator/application-deployments/" + intent.OperationID + "/retry"
	denied := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, "{}", "retry-owner")
	if denied.Code != http.StatusForbidden {
		t.Errorf("customer invoked operator retry: status=%d, want 403", denied.Code)
	}
	retained, _, _ := fixture.store.GetRuntimeOperation(ctx, intent.OperationID)
	if retained["result"] != failed["result"] || retained["status"] != failed["status"] {
		t.Fatal("denied retry changed the failed deployment")
	}
	retry := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, path, "{}", "retry-operator")
	var response struct {
		Intent workspaceApplicationDeploymentIntent `json:"intent"`
	}
	if retry.Code != http.StatusAccepted || json.Unmarshal(retry.Body.Bytes(), &response) != nil {
		t.Fatalf("operator retry status=%d body=%s, want 202", retry.Code, retry.Body.String())
	}
	if response.Intent.OperationID != intent.OperationID || response.Intent.RequestHash != intent.RequestHash || response.Intent.Phase != intent.Phase {
		t.Fatal("operator retry changed the accepted deployment identity or command")
	}
	runDeploymentWorkerToCompletion(t, app, handler.service, intent.OperationID)
	completed, _, _ := fixture.store.GetRuntimeOperation(ctx, intent.OperationID)
	active, err := decodeWorkspaceApplicationDeploymentIntent(completed)
	if err != nil || active.Phase != workspaceApplicationDeploymentActivePhase || active.RequestHash != intent.RequestHash || active.OperationID != intent.OperationID || stringValue(completed["status"]) != "succeeded" {
		t.Fatalf("recovered original deployment did not complete: phase=%s err=%v", active.Phase, err)
	}
	operations, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deployments := 0
	for _, operation := range operations {
		if stringValue(operation["action"]) == workspaceApplicationDeploymentAction {
			deployments++
		}
	}
	if deployments != 1 {
		t.Fatalf("recovery created another deployment: count=%d", deployments)
	}
}

func TestRetiredWorkspaceDefaultApplicationResumeHTTPPreservesHistory(t *testing.T) {
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
	for _, path := range []string{"/api/workspaces/ws-alpha/application-installation/resume", "/api/workspaces/not-owned/application-installation/resume"} {
		for range 2 {
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, "{}", "resume-retired")
			if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("retired resume=%d %s", response.Code, response.Body.String())
			}
		}
	}
	retained, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil || !reflect.DeepEqual(retained, operations) {
		t.Fatalf("retired resume changed operation history: %v", err)
	}
	after, _, err := fixture.store.GetWorkspace(ctx, request.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, workspace) || len(externalCalls) != 0 {
		t.Fatalf("retired resume changed workspace or called external owners: calls=%v err=%v", externalCalls, err)
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
