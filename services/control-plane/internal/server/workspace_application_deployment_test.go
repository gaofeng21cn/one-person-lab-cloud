package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func knowledgeApplicationRevisionForTest() contracts.WorkspaceApplicationRevision {
	return contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
		Image:     "repo.example/apps/knowledge@sha256:" + strings.Repeat("a", 64),
		Ports:     []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		EntryPort: "http", ExposurePolicy: "application",
	}
}

func createKnowledgeDeploymentForTest(t *testing.T, app *controlPlaneServer, mutationKey string) workspaceApplicationDeploymentIntent {
	t.Helper()
	revision := knowledgeApplicationRevisionForTest()
	intent := applicationDeploymentFixtureForTest(t, app, "ws-alpha", mutationKey, revision)
	intent.Configuration = contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"CONFIG": strings.Repeat("c", 64)}}
	intent = seedAcceptedApplicationDeploymentForTest(t, app, intent)
	return intent
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
	operation, err := newWorkspaceLaunchFixtureOperation(command)
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
	periodStart, paidThrough := workspaceFixtureCurrentBillingPeriod(time.Now(), 12)
	for key, value := range map[string]any{
		"paidThrough": paidThrough.Format(time.RFC3339Nano), "periodStart": periodStart.Format(time.RFC3339Nano), "billingAnchorDay": 12,
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

	admitted := createKnowledgeDeploymentForTest(t, server.(*controlPlaneHTTPHandler).app, "deploy-pg-first")
	if admitted.ExpectedWorkspaceVersion != 0 || admitted.Phase != workspaceApplicationDeploymentIntentPhase {
		t.Fatalf("postgres intent=%+v", admitted)
	}
	restarted, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	read := requestWithSession(t, restarted, operatorSessionForTest(t, restarted), http.MethodGet, "/api/operator/application-deployments/"+admitted.OperationID, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), admitted.OperationID) {
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
	operation, err := newWorkspaceLaunchFixtureOperation(command)
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
	periodStart, paidThrough := workspaceFixtureCurrentBillingPeriod(time.Now(), 12)
	for key, value := range map[string]any{
		"paidThrough": paidThrough.Format(time.RFC3339Nano), "periodStart": periodStart.Format(time.RFC3339Nano), "billingAnchorDay": 12,
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
	handler := server.(*controlPlaneHTTPHandler)
	created := createKnowledgeDeploymentForTest(t, handler.app, "deploy-chain-first")
	runDeploymentWorkerToCompletion(t, handler.app, service, created.OperationID)

	row, found, err := store.GetRuntimeOperation(context.Background(), created.OperationID)
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
	handler := server.(*controlPlaneHTTPHandler)
	created := createKnowledgeDeploymentForTest(t, handler.app, "deploy-chain-conflict")
	// The binding moves after the intent reserved version zero: the activation
	// must fail closed into manual review instead of overwriting.
	workspace, _ := handler.app.getWorkspace("ws-alpha")
	workspace["applicationBindingVersion"] = int64(5)
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	runDeploymentWorkerToCompletion(t, handler.app, service, created.OperationID)

	row, found, readErr := store.GetRuntimeOperation(context.Background(), created.OperationID)
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
	handler := server.(*controlPlaneHTTPHandler)
	intent := createKnowledgeDeploymentForTest(t, handler.app, "deploy-worker")
	return handler, intent.OperationID
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
	backing := newMemoryTableStore()
	store := &activationResponseLossStore{controlPlaneTableStore: backing, loseResponse: true}
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, operationID := applicationDeploymentWorkerFixture(t, backing, service)
	handler.app.tables = store
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
