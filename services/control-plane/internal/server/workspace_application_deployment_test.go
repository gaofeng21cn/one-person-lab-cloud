package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func admitKnowledgeRevisionForTest(t *testing.T, server http.Handler, operator *httptest.ResponseRecorder) {
	t.Helper()
	revision := `{"schemaVersion":1,"applicationId":"knowledge-app","version":"1.0.0","platform":"linux/amd64",` +
		`"image":"repo.example/apps/knowledge@sha256:` + strings.Repeat("a", 64) + `",` +
		`"ports":[{"name":"http","port":8080,"protocol":"TCP"}],` +
		`"resources":{"cpu":2,"memoryGb":4},"exposurePolicy":"application"}`
	admitted := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision, "admit-deploy-test")
	if admitted.Code != http.StatusOK {
		t.Fatalf("revision admission status=%d body=%s", admitted.Code, admitted.Body.String())
	}
}

func deployIntentBody(configurationDigest string) string {
	return `{"workspaceId":"ws-alpha","applicationId":"knowledge-app","targetRevision":"1.0.0","configurationDigest":"` + configurationDigest + `"}`
}

func TestApplicationDeploymentIntentHTTP(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	operator := operatorSessionForTest(t, fixture.server)
	admitKnowledgeRevisionForTest(t, fixture.server, operator)

	first := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-first")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first intent status=%d body=%s", first.Code, first.Body.String())
	}
	var admitted struct {
		Intent struct {
			OperationID              string `json:"operationId"`
			Phase                    string `json:"phase"`
			ExpectedWorkspaceVersion int64  `json:"expectedWorkspaceVersion"`
			CurrentBinding           string `json:"currentBinding"`
		} `json:"intent"`
	}
	if json.Unmarshal(first.Body.Bytes(), &admitted) != nil || admitted.Intent.OperationID == "" ||
		admitted.Intent.Phase != workspaceApplicationDeploymentIntentPhase ||
		admitted.Intent.ExpectedWorkspaceVersion != 0 || admitted.Intent.CurrentBinding != provisioning.ApplicationBindingEmpty {
		t.Fatalf("first intent body=%s", first.Body.String())
	}

	replay := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-first")
	var replayed struct {
		Intent struct {
			OperationID string `json:"operationId"`
		} `json:"intent"`
	}
	if replay.Code != http.StatusAccepted || json.Unmarshal(replay.Body.Bytes(), &replayed) != nil ||
		replayed.Intent.OperationID != admitted.Intent.OperationID {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	changed := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("d", 64)), "deploy-knowledge-first")
	if changed.Code != http.StatusConflict {
		t.Fatalf("changed intent under the same key status=%d body=%s", changed.Code, changed.Body.String())
	}

	notAdmitted := `{"workspaceId":"ws-alpha","applicationId":"unknown-app","targetRevision":"1.0.0","configurationDigest":"` + strings.Repeat("c", 64) + `"}`
	missing := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", notAdmitted, "deploy-unknown")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "workspace_application_revision_not_found") {
		t.Fatalf("unadmitted revision status=%d body=%s", missing.Code, missing.Body.String())
	}

	workspace := cloneMap(fixture.workspace)
	workspace["applicationBinding"] = "other-app@2.0.0"
	mustStore(t, fixture.store.SaveWorkspace(context.Background(), workspace))
	mismatch := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-mismatch")
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), "workspace_application_deployment_transition_invalid") {
		t.Fatalf("binding mismatch status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}

	suspended := cloneMap(fixture.workspace)
	suspended["state"], suspended["status"] = "suspended", "suspended"
	mustStore(t, fixture.store.SaveWorkspace(context.Background(), suspended))
	unready := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-unready")
	if unready.Code != http.StatusConflict || !strings.Contains(unready.Body.String(), "workspace_application_resources_unready") {
		t.Fatalf("unready workspace status=%d body=%s", unready.Code, unready.Body.String())
	}

	read := requestWithSession(t, fixture.server, operator, http.MethodGet, "/api/operator/application-deployments/"+admitted.Intent.OperationID, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"phase":"intent"`) {
		t.Fatalf("intent read status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestApplicationDeploymentIntentPostgres(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_application_deploy_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, database)
		_, _ = admin.Exec(`DROP DATABASE ` + database)
		_ = admin.Close()
	})
	store, err := newTestPostgresEntStateStore(controlPlaneTestPostgresURL(t, database, ""))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "ws-alpha"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := operation.stagePlan()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range plan[:len(plan)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "attachmentId": "attachment-alpha"} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	for key, value := range map[string]any{
		"paidThrough": "2026-10-12T00:00:00Z", "periodStart": "2026-09-12T00:00:00Z", "billingAnchorDay": 12,
	} {
		operation.raw[key], _ = json.Marshal(value)
	}
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), launchRow))
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))

	operatorSession := operatorSessionForTest(t, server)
	admitKnowledgeRevisionForTest(t, server, operatorSession)
	first := requestWithMutationKeyForTest(t, server, operatorSession, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-pg-first")
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"phase":"intent"`) {
		t.Fatalf("postgres intent status=%d body=%s", first.Code, first.Body.String())
	}
	var admitted struct {
		Intent struct {
			OperationID              string `json:"operationId"`
			ExpectedWorkspaceVersion int64  `json:"expectedWorkspaceVersion"`
		} `json:"intent"`
	}
	if json.Unmarshal(first.Body.Bytes(), &admitted) != nil || admitted.Intent.ExpectedWorkspaceVersion != 0 {
		t.Fatalf("postgres intent body=%s", first.Body.String())
	}
	restarted, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	read := requestWithSession(t, restarted, operatorSessionForTest(t, restarted), http.MethodGet, "/api/operator/application-deployments/"+admitted.Intent.OperationID, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), admitted.Intent.OperationID) {
		t.Fatalf("postgres restart readback status=%d body=%s", read.Code, read.Body.String())
	}
}

// seedResourceOnlyActivatedWorkspace prepares a memory store whose workspace
// was activated through a succeeded resource-only launch: live resources, an
// empty application binding and binding version zero.
func seedResourceOnlyActivatedWorkspace(t *testing.T, store *memoryTableStore, operationID, workspaceID string) {
	t.Helper()
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = operationID, "acct-alpha", workspaceID
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := operation.stagePlan()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range plan[:len(plan)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "attachmentId": "attachment-alpha"} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	for key, value := range map[string]any{
		"paidThrough": "2026-10-12T00:00:00Z", "periodStart": "2026-09-12T00:00:00Z", "billingAnchorDay": 12,
	} {
		operation.raw[key], _ = json.Marshal(value)
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
}

func runDeploymentToCompletion(t *testing.T, app *controlPlaneServer, service *controlplane.Service, operationID string) {
	t.Helper()
	for range 6 {
		if err := app.runWorkspaceApplicationDeployment(context.Background(), service, operationID); err != nil {
			t.Fatalf("deployment drive: %v", err)
		}
		row, found, err := app.tables.GetRuntimeOperation(context.Background(), operationID)
		if err != nil || !found {
			t.Fatalf("intent row found=%v err=%v", found, err)
		}
		if stringValue(row["status"]) == "succeeded" || stringValue(row["status"]) == "manual_review" {
			return
		}
	}
	t.Fatal("deployment did not reach a terminal state")
}

func TestWorkspaceApplicationDeploymentFullChain(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	operator := operatorSessionForTest(t, server)
	admitKnowledgeRevisionForTest(t, server, operator)

	created := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-chain-first")
	if created.Code != http.StatusAccepted {
		t.Fatalf("intent status=%d body=%s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Intent struct {
			OperationID string `json:"operationId"`
		} `json:"intent"`
	}
	if json.Unmarshal(created.Body.Bytes(), &createdBody) != nil || createdBody.Intent.OperationID == "" {
		t.Fatalf("intent body=%s", created.Body.String())
	}
	handler := server.(*controlPlaneHTTPHandler)
	runDeploymentToCompletion(t, handler.app, service, createdBody.Intent.OperationID)

	row, found, err := store.GetRuntimeOperation(context.Background(), createdBody.Intent.OperationID)
	if err != nil || !found || stringValue(row["status"]) != "succeeded" {
		t.Fatalf("intent row status=%v found=%v err=%v", stringValue(row["status"]), found, err)
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentActivePhase || intent.ReceiptID != "receipt-from-ledger" {
		t.Fatalf("active intent=%#v err=%v", intent, err)
	}
	workspace, _ := handler.app.getWorkspace("ws-alpha")
	if workspace["applicationBinding"] != "knowledge-app@1.0.0" || int64(numberField(workspace, "applicationBindingVersion", 0)) != 1 {
		t.Fatalf("activated workspace=%#v", workspace)
	}
	if len(fabric.applicationRuntimeInputs) != 1 || fabric.applicationRuntimeInputs[0].Revision.ApplicationID != "knowledge-app" {
		t.Fatalf("fabric ensure inputs=%#v", fabric.applicationRuntimeInputs)
	}
}

func TestWorkspaceApplicationDeploymentActivationConflictGoesManualReview(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	operator := operatorSessionForTest(t, server)
	admitKnowledgeRevisionForTest(t, server, operator)
	handler := server.(*controlPlaneHTTPHandler)
	created := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-chain-conflict")
	if created.Code != http.StatusAccepted {
		t.Fatalf("intent status=%d body=%s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Intent struct {
			OperationID string `json:"operationId"`
		} `json:"intent"`
	}
	if json.Unmarshal(created.Body.Bytes(), &createdBody) != nil {
		t.Fatal(created.Body.String())
	}
	// The binding moves after the intent reserved version zero: the activation
	// must fail closed into manual review instead of overwriting.
	workspace, _ := handler.app.getWorkspace("ws-alpha")
	workspace["applicationBindingVersion"] = int64(5)
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	runDeploymentToCompletion(t, handler.app, service, createdBody.Intent.OperationID)

	row, found, readErr := store.GetRuntimeOperation(context.Background(), createdBody.Intent.OperationID)
	if readErr != nil || !found {
		t.Fatalf("intent found=%v err=%v", found, readErr)
	}
	if stringValue(row["status"]) != "manual_review" {
		t.Fatalf("status=%q, want manual_review", stringValue(row["status"]))
	}
	result := stringValue(row["result"])
	if !strings.Contains(result, "workspace_application_activation_conflict") {
		t.Fatalf("intent result=%s", result)
	}
}
