package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
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
	}
	mustStore(t, store.SaveWorkspace(ctx, workspace))
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

func TestWorkspaceApplicationRecoveryHTTP(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	ctx := context.Background()
	app := fixture.server.(*controlPlaneHTTPHandler).app
	operator := operatorSessionForTest(t, fixture.server)
	admitKnowledgeRevisionForTest(t, fixture.server, operator)
	created := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody("recovery"), "recover-http")
	var response struct {
		Intent workspaceApplicationDeploymentIntent `json:"intent"`
	}
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &response) != nil {
		t.Fatalf("deployment create=%d %s", created.Code, created.Body.String())
	}
	row, _, _ := fixture.store.GetRuntimeOperation(ctx, response.Intent.OperationID)
	mustStore(t, app.markWorkspaceApplicationManualReview(ctx, row, response.Intent, "test_preflight_unavailable"))
	path := "/api/operator/application-deployments/" + response.Intent.OperationID + "/retry"
	denied := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, "{}", "retry-owner")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("customer invoked operator retry: %d", denied.Code)
	}
	retry := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, path, "{}", "retry-operator")
	if retry.Code != http.StatusAccepted || json.Unmarshal(retry.Body.Bytes(), &response) != nil || response.Intent.Phase != workspaceApplicationDeploymentIntentPhase {
		t.Fatalf("operator retry=%d %s", retry.Code, retry.Body.String())
	}
}

func TestWorkspaceDefaultApplicationResumeHTTP(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	ctx := context.Background()
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha",
		AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceKeyGroupID: 5,
		Revision: defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("e", 64)), Phase: "failed", LastError: "test_installation_failed",
		WorkspaceAPIKeyID: 19, GatewaySecret: &clients.GatewaySecretWriteResult{SecretRef: "secret-ws-alpha", Version: "v1"}}
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
	purchase, _, _ := fixture.store.GetRuntimeOperation(ctx, request.LaunchOperationID)
	path := "/api/workspaces/ws-alpha/application-installation/resume"
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, "{}", "resume-default")
	if response.Code != http.StatusAccepted {
		t.Fatalf("default resume=%d %s", response.Code, response.Body.String())
	}
	current, _, _ := fixture.store.GetRuntimeOperation(ctx, request.OperationID)
	resumed, err := decodeWorkspaceDefaultApplication(current)
	if err != nil || resumed.DeploymentID == "" || resumed.Revision.Version != request.Revision.Version || resumed.WorkspaceAPIKeyID != request.WorkspaceAPIKeyID {
		t.Fatalf("default resume did not retain original installation: %+v %v", resumed, err)
	}
	retained, _, _ := fixture.store.GetRuntimeOperation(ctx, request.LaunchOperationID)
	if retained["result"] != purchase["result"] {
		t.Fatal("installation resume rewrote purchase")
	}
	replay := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, "{}", "resume-default")
	if replay.Code != http.StatusAccepted {
		t.Fatalf("default resume replay=%d %s", replay.Code, replay.Body.String())
	}
	current, _, _ = fixture.store.GetRuntimeOperation(ctx, request.OperationID)
	again, err := decodeWorkspaceDefaultApplication(current)
	if err != nil || again.DeploymentID != resumed.DeploymentID {
		t.Fatal("resume replay created another deployment")
	}
	denied := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, "/api/workspaces/not-owned/application-installation/resume", "{}", "resume-other")
	if denied.Code != http.StatusNotFound {
		t.Fatalf("unowned resume=%d", denied.Code)
	}
}
