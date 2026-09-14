package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func admitKnowledgeRevisionForTest(t *testing.T, server http.Handler, operator *httptest.ResponseRecorder) {
	t.Helper()
	revision := `{"schemaVersion":1,"applicationId":"knowledge-app","version":"1.0.0","platform":"linux/amd64",` +
		`"image":"repo.example/apps/knowledge@sha256:` + strings.Repeat("a", 64) + `",` +
		`"ports":[{"name":"http","port":8080,"protocol":"TCP"}],` +
		`"entryPort":"http",` +
		`"resources":{"cpu":2,"memoryGb":4},"exposurePolicy":"application"}`
	admitted := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-revisions", revision, "admit-deploy-test")
	if admitted.Code != http.StatusOK {
		t.Fatalf("revision admission status=%d body=%s", admitted.Code, admitted.Body.String())
	}
}

func deployIntentBody(configurationDigest string) string {
	return `{"workspaceId":"ws-alpha","applicationId":"knowledge-app","targetRevision":"1.0.0","configuration":{"environment":{"CONFIG":"` + configurationDigest + `"}}}`
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

	notAdmitted := `{"workspaceId":"ws-alpha","applicationId":"unknown-app","targetRevision":"1.0.0","configuration":{"environment":{"CONFIG":"` + strings.Repeat("c", 64) + `"}}}`
	missing := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", notAdmitted, "deploy-unknown")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "workspace_application_revision_not_found") {
		t.Fatalf("unadmitted revision status=%d body=%s", missing.Code, missing.Body.String())
	}

	workspace := cloneMap(fixture.workspace)
	workspace["applicationBinding"] = "other-app@2.0.0"
	mustStore(t, fixture.store.SaveWorkspace(context.Background(), workspace))
	mismatch := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-knowledge-mismatch")
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), "workspace_application_binding_unknown") {
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
func seedResourceOnlyActivatedWorkspace(t *testing.T, store controlPlaneTableStore, operationID, workspaceID string) {
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
		for key, value := range map[string]any{"computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "attachmentId": "attachment-alpha", "attachmentBindingRef": operation.ID + ":attachment", "receiptOperationId": operation.ID + ":purchase-receipt"} {
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

func runDeploymentWorkerToCompletion(t *testing.T, app *controlPlaneServer, service *controlplane.Service, operationID string) {
	t.Helper()
	for range 6 {
		if err := app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); err != nil {
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
	runDeploymentWorkerToCompletion(t, handler.app, service, createdBody.Intent.OperationID)

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
	if len(fabric.applicationRuntimeInputs) != 1 || fabric.applicationRuntimeInputs[0].Revision.ApplicationID != "knowledge-app" ||
		fabric.applicationRuntimeInputs[0].AccountID != "acct-alpha" || fabric.applicationRuntimeInputs[0].AttachmentOperationID != "workspace-launch-alpha:attachment" {
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
	runDeploymentWorkerToCompletion(t, handler.app, service, createdBody.Intent.OperationID)

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
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentManualReviewPhase {
		t.Fatalf("manual review phase=%s err=%v", intent.Phase, err)
	}
}

type pendingApplicationFabric struct {
	fakeFabricClient
	transientError error
	attempts       int
	keys           []string
}

func (f *pendingApplicationFabric) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, key string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	f.attempts++
	f.keys = append(f.keys, key)
	if f.transientError != nil {
		err := f.transientError
		f.transientError = nil
		return contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	observation, err := f.fakeFabricClient.EnsureWorkspaceApplicationRuntime(ctx, input, key)
	if f.attempts < 3 {
		observation.Status = "pending"
		for index := range observation.Components {
			observation.Components[index].State = "pending"
		}
	}
	return observation, err
}

func applicationDeploymentWorkerFixture(t *testing.T, store controlPlaneTableStore, service *controlplane.Service) (*controlPlaneHTTPHandler, string) {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	operator := operatorSessionForTest(t, server)
	admitKnowledgeRevisionForTest(t, server, operator)
	created := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", deployIntentBody(strings.Repeat("c", 64)), "deploy-worker")
	var response struct {
		Intent workspaceApplicationDeploymentIntent `json:"intent"`
	}
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &response) != nil || response.Intent.OperationID == "" {
		t.Fatalf("create deployment status=%d body=%s", created.Code, created.Body.String())
	}
	return server.(*controlPlaneHTTPHandler), response.Intent.OperationID
}

func TestWorkspaceApplicationDeploymentWorkerRetriesPendingWithOriginalIdentity(t *testing.T) {
	store := newMemoryTableStore()
	transient := errors.New("fabric temporarily unavailable")
	fabric := &pendingApplicationFabric{transientError: transient}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, operationID := applicationDeploymentWorkerFixture(t, store, service)
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); !errors.Is(err, transient) {
		t.Fatalf("first scan error=%v", err)
	}
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	row, _, _ := store.GetRuntimeOperation(context.Background(), operationID)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentRuntimePhase || intent.RuntimeObservation.Status != "pending" || intent.LastError != "" {
		t.Fatalf("pending observation=%#v err=%v", intent, err)
	}
	workspace, _, _ := store.GetWorkspace(context.Background(), "ws-alpha")
	if workspace["applicationBinding"] != provisioning.ApplicationBindingEmpty {
		t.Fatal("pending runtime activated the application")
	}
	runDeploymentWorkerToCompletion(t, handler.app, service, operationID)
	if fabric.attempts != 3 {
		t.Fatalf("ensure attempts=%d", fabric.attempts)
	}
	for _, key := range fabric.keys {
		if key != operationID+":runtime" {
			t.Fatalf("retry identity changed: %q", key)
		}
	}
	for _, input := range fabric.applicationRuntimeInputs {
		if input.AttachmentOperationID != "workspace-launch-alpha:attachment" {
			t.Fatalf("attachment provenance changed: %q", input.AttachmentOperationID)
		}
	}
}

func TestWorkspaceApplicationDeploymentWorkerRejectsFailedOrForeignRuntime(t *testing.T) {
	for _, observed := range []struct {
		name, workspaceID, state, componentState string
	}{
		{"failed", "ws-alpha", "failed", "failed"},
		{"foreign", "ws-other", "ready", "ready"},
		{"inconsistent readiness", "ws-alpha", "ready", "pending"},
	} {
		t.Run(observed.name, func(t *testing.T) {
			store := newMemoryTableStore()
			fabric := &fakeFabricClient{}
			service := newTestService(&fakeLedgerClient{}, fabric)
			handler, operationID := applicationDeploymentWorkerFixture(t, store, service)
			revisionRow, _, _ := store.AdmittedApplicationRevision(context.Background(), "knowledge-app", "1.0.0")
			revision, ok := decodeApplicationRevisionPayload(stringValue(revisionRow["payload"]))
			if !ok {
				t.Fatal("admitted revision could not be read")
			}
			components := contracts.WorkspaceApplicationRuntimeComponents(revision)
			for index := range components {
				components[index].State = observed.componentState
			}
			fabric.applicationRuntimeObservation = contracts.WorkspaceApplicationRuntimeObservation{
				SchemaVersion: 1, WorkspaceID: observed.workspaceID, RuntimeID: "rt-observed",
				Status: observed.state, Components: components,
			}
			runDeploymentWorkerToCompletion(t, handler.app, service, operationID)
			row, _, _ := store.GetRuntimeOperation(context.Background(), operationID)
			intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
			workspace, _, _ := store.GetWorkspace(context.Background(), "ws-alpha")
			if err != nil || stringValue(row["status"]) != "manual_review" || intent.Phase != workspaceApplicationDeploymentManualReviewPhase ||
				workspace["applicationBinding"] != provisioning.ApplicationBindingEmpty || intent.ReceiptID != "" {
				t.Fatalf("invalid runtime activated: phase=%s binding=%v receipt=%s err=%v", intent.Phase, workspace["applicationBinding"], intent.ReceiptID, err)
			}
		})
	}
}

func TestWorkspaceApplicationDeploymentWorkerStopsInvalidHTTPRequest(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &pendingApplicationFabric{transientError: &clients.FabricHTTPError{StatusCode: http.StatusBadRequest, Body: `{"error":"workspace_application_runtime_attachment_mismatch"}`}}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, operationID := applicationDeploymentWorkerFixture(t, store, service)
	runDeploymentWorkerToCompletion(t, handler.app, service, operationID)
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	row, _, _ := store.GetRuntimeOperation(context.Background(), operationID)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentManualReviewPhase || stringValue(row["status"]) != "manual_review" || fabric.attempts != 1 {
		t.Fatalf("invalid request retried: phase=%s attempts=%d err=%v", intent.Phase, fabric.attempts, err)
	}
}

type activationResponseLossStore struct {
	controlPlaneTableStore
	loseResponse bool
}

func (s *activationResponseLossStore) ApplyWorkspaceApplicationActivation(ctx context.Context, mutation workspaceApplicationActivationMutation) error {
	if err := s.controlPlaneTableStore.ApplyWorkspaceApplicationActivation(ctx, mutation); err != nil {
		return err
	}
	if s.loseResponse {
		s.loseResponse = false
		return errors.New("activation commit response lost")
	}
	return nil
}

func TestWorkspaceApplicationDeploymentActivationLostResponseResumesReceipt(t *testing.T) {
	store := &activationResponseLossStore{controlPlaneTableStore: newMemoryTableStore(), loseResponse: true}
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, operationID := applicationDeploymentWorkerFixture(t, store, service)
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); err == nil {
		t.Fatal("expected lost activation response")
	}
	row, _, _ := store.GetRuntimeOperation(context.Background(), operationID)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	workspace, _, _ := store.GetWorkspace(context.Background(), "ws-alpha")
	if err != nil || intent.Phase != workspaceApplicationDeploymentReceiptPhase || workspace["applicationBinding"] != "knowledge-app@1.0.0" {
		t.Fatalf("activation was not atomic: intent=%#v workspace=%#v err=%v", intent, workspace, err)
	}
	restarted, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	runDeploymentWorkerToCompletion(t, restarted.(*controlPlaneHTTPHandler).app, service, operationID)
	workspace, _, _ = store.GetWorkspace(context.Background(), "ws-alpha")
	if numberField(workspace, "applicationBindingVersion", 0) != 1 || len(fabric.applicationRuntimeInputs) != 1 {
		t.Fatalf("recovery repeated runtime or activation: workspace=%#v inputs=%d", workspace, len(fabric.applicationRuntimeInputs))
	}
}

func TestWorkspaceApplicationDeploymentActivationPostgresAtomicRecovery(t *testing.T) {
	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_application_activation_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, database)
		_, _ = admin.Exec(`DROP DATABASE ` + database)
		_ = admin.Close()
	})
	databaseURL := controlPlaneTestPostgresURL(t, database, "")
	store, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, operationID := applicationDeploymentWorkerFixture(t, store, service)
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("deployment phase write failed")
	failPhaseWrite := true
	store.(*postgresEntStateStore).client.RuntimeOperation.Use(func(next controlplaneent.Mutator) controlplaneent.Mutator {
		return controlplaneent.MutateFunc(func(ctx context.Context, mutation controlplaneent.Mutation) (controlplaneent.Value, error) {
			if operation, ok := mutation.(*controlplaneent.RuntimeOperationMutation); ok && failPhaseWrite {
				if result, present := operation.Result(); present && strings.Contains(result, `"phase":"receipt"`) {
					failPhaseWrite = false
					return nil, injected
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	if err := handler.app.runWorkspaceApplicationDeploymentsOnce(context.Background(), service); !errors.Is(err, injected) {
		t.Fatalf("activation write error=%v", err)
	}
	_ = store.(*postgresEntStateStore).client.Close()
	store, err = newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.(*postgresEntStateStore).client.Close() })
	workspace, _, _ := store.GetWorkspace(context.Background(), "ws-alpha")
	row, _, _ := store.GetRuntimeOperation(context.Background(), operationID)
	intent, decodeErr := decodeWorkspaceApplicationDeploymentIntent(row)
	if decodeErr != nil || intent.Phase != workspaceApplicationDeploymentActivatingPhase ||
		workspace["applicationBinding"] != provisioning.ApplicationBindingEmpty || numberField(workspace, "applicationBindingVersion", 0) != 0 {
		t.Fatalf("partial activation persisted after rollback: workspace=%#v phase=%s err=%v", workspace, intent.Phase, decodeErr)
	}
	restarted, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	runDeploymentWorkerToCompletion(t, restarted.(*controlPlaneHTTPHandler).app, service, operationID)
	workspace, _, _ = store.GetWorkspace(context.Background(), "ws-alpha")
	if workspace["applicationBinding"] != "knowledge-app@1.0.0" || numberField(workspace, "applicationBindingVersion", 0) != 1 || len(fabric.applicationRuntimeInputs) != 1 {
		t.Fatalf("recovery repeated execution: workspace=%#v fabric inputs=%d", workspace, len(fabric.applicationRuntimeInputs))
	}
}
