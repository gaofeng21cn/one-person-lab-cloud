package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/application"
)

func defaultOPLApplicationRevision(image string) contracts.WorkspaceApplicationRevision {
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "opl-app", Platform: "linux/amd64", Image: image,
		Ports: []contracts.WorkspaceApplicationPort{{Name: "webui", Port: 3000, Protocol: "TCP"}}, EntryPort: "webui",
		HealthChecks:     []contracts.WorkspaceApplicationHealthCheck{{Port: 3000, Path: "/", InitialDelaySeconds: 10}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}, {Name: "projects", MountPath: "/projects"}},
		ScratchMounts:    []contracts.WorkspaceApplicationMount{{Name: "recovery", MountPath: "/recovery"}},
		Credentials: []contracts.WorkspaceApplicationCredential{
			{Name: "admin-password", Kind: contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, Target: "/run/secrets/opl_webui_password", Username: "opl"},
			{Name: "session-secret", Kind: contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret, Target: "/run/secrets/webui_session_secret"},
			{Name: "gateway", Kind: contracts.WorkspaceApplicationCredentialGatewayKey, Target: "/run/secrets/opl_gateway_api_key"},
		}, ExposurePolicy: "application"}
	payload, _ := json.Marshal(revision)
	revision.Version = fmt.Sprintf("%x", sha256.Sum256(payload))
	return revision
}

// workspaceDefaultApplicationRow serializes retained history; no installer runs.
func workspaceDefaultApplicationRow(request workspaceDefaultApplicationRequest) (map[string]any, error) {
	if err := validateWorkspaceDefaultApplicationRequest(request); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	status := "pending"
	if request.Phase == "deployed" {
		status = "succeeded"
	}
	if request.Phase == "failed" {
		status = "failed"
	}
	return map[string]any{"id": request.OperationID, "operationId": request.OperationID, "accountId": request.AccountID, "workspaceId": request.WorkspaceID, "resourceId": request.WorkspaceID, "resourceKind": "workspace", "action": workspaceDefaultApplicationAction, "status": status, "result": string(encoded), "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

// Seed an already accepted revision directly in its persisted fixture store.
// Production exposes history reads only; this helper is not an admission API.
func seedAdmittedApplicationRevisionForTest(t *testing.T, store controlPlaneTableStore, revision contracts.WorkspaceApplicationRevision) {
	t.Helper()
	ctx := context.Background()
	digest, err := application.RevisionDigest(revision)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	existing, found, err := store.AdmittedApplicationRevision(ctx, revision.ApplicationID, revision.Version)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		if stringValue(existing["digest"]) != digest || stringValue(existing["payload"]) != string(payload) {
			t.Fatal("conflicting accepted revision fixture")
		}
		return
	}
	id := "historical-application-revision-" + stableID(revision.ApplicationID, revision.Version)
	switch target := store.(type) {
	case workspaceDeleteEventStore:
		seedAdmittedApplicationRevisionForTest(t, target.controlPlaneTableStore, revision)
	case *memoryTableStore:
		target.mu.Lock()
		defer target.mu.Unlock()
		target.applicationRevisions[revision.ApplicationID+"@"+revision.Version] = map[string]any{
			"id": id, "applicationId": revision.ApplicationID, "version": revision.Version,
			"digest": digest, "payload": string(payload), "admittedByUserId": "operator-test",
		}
	case *postgresEntStateStore:
		_, err := target.client.ApplicationRevision.Create().SetID(id).
			SetApplicationID(revision.ApplicationID).SetVersion(revision.Version).
			SetDigest(digest).SetPayload(string(payload)).SetAdmittedByUserID("operator-test").Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported accepted revision fixture store %T", store)
	}
}

func seedHistoricalDefaultApplicationForTest(t *testing.T, app *controlPlaneServer, fabric *applicationReplacementFabric, request workspaceDefaultApplicationRequest) workspaceApplicationDeploymentIntent {
	t.Helper()
	ctx := context.Background()
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, row))
	// The accepted installation already owns its configuration and Secret binding.
	// Fixture setup must not reveal a Gateway Key or write another Fabric Secret.
	if request.GatewaySecret == nil || request.GatewaySecret.SecretRef == "" || request.GatewaySecret.Version == "" || request.WorkspaceAPIKeyID <= 0 {
		t.Fatal("historical installation fixture requires its accepted Gateway binding")
	}
	configuration := contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{
		"OPL_WEBUI_AUTH_MODE": "password", "OPL_WEBUI_USERNAME": "opl",
	}}
	bindings := []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: request.GatewaySecret.SecretRef, Version: request.GatewaySecret.Version}}
	current := seedApplicationRevisionForLifecycle(t, app, request.WorkspaceID, request.Revision, configuration, bindings, request.WorkspaceAPIKeyID, true)
	input, err := app.workspaceApplicationRuntimeInput(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	fabric.inputs = map[string]clients.WorkspaceApplicationRuntimeInput{input.RuntimeOperationID: input}
	fabric.states = map[string]string{input.RuntimeOperationID: "ready"}
	request.Phase, request.DeploymentID = "deployed", current.OperationID
	row, err = workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, row))
	return current
}

// applicationDeploymentFixtureForTest snapshots only the fixture's resource and
// selection fields. Callers supply the accepted configuration, data lineage and
// credentials explicitly; this does not resolve or admit an application change.

// seedAcceptedApplicationDeploymentForTest persists a frozen typed command using
// the existing reservation CAS. It computes serialization identities only; all
// command fields and historical lineage have already been chosen by the fixture.

// applicationDeploymentFixtureForTest builds the deployment intent a live
// application deployment starts from.
func applicationDeploymentFixtureForTest(t *testing.T, app *controlPlaneServer, workspaceID, mutationKey string, revision contracts.WorkspaceApplicationRevision) workspaceApplicationDeploymentIntent {
	t.Helper()
	seedAdmittedApplicationRevisionForTest(t, app.tables, revision)
	workspace, found, err := app.tables.GetWorkspace(context.Background(), workspaceID)
	if err != nil || !found {
		t.Fatalf("fixture workspace missing: found=%v err=%v", found, err)
	}
	return workspaceApplicationDeploymentIntent{
		SchemaVersion: workspaceApplicationDeploymentSchemaVersion, Version: 2,
		OperationID: workspaceApplicationDeploymentOperationID(workspaceID, mutationKey),
		AccountID:   stringValue(workspace["accountId"]), WorkspaceID: workspaceID,
		ComputeID: stringValue(workspace["currentComputeAllocationId"]), StorageID: stringValue(workspace["storageId"]), AttachmentID: stringValue(workspace["currentAttachmentId"]),
		ApplicationID: revision.ApplicationID, TargetRevision: revision.Version,
		ExpectedWorkspaceVersion: int64(numberField(workspace, "applicationBindingVersion", 0)), CurrentBinding: stringValue(workspace["applicationBinding"]),
		PreviousDeploymentID: stringValue(workspace["currentApplicationDeploymentId"]),
		DataBindingID:        "application-data-" + stableID(workspaceID, revision.ApplicationID),
		Phase:                workspaceApplicationDeploymentIntentPhase, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// seedAcceptedApplicationDeploymentForTest stores one accepted application
// deployment intent. The production claim path is retired with the Workspace
// Gateway key rotation, so the row is seeded directly.
func seedAcceptedApplicationDeploymentForTest(t *testing.T, app *controlPlaneServer, intent workspaceApplicationDeploymentIntent) workspaceApplicationDeploymentIntent {
	t.Helper()
	ctx := context.Background()
	revision, found, err := app.tables.AdmittedApplicationRevision(ctx, intent.ApplicationID, intent.TargetRevision)
	if err != nil || !found {
		t.Fatalf("accepted revision missing: found=%v err=%v", found, err)
	}
	intent.RevisionDigest = stringValue(revision["digest"])
	intent.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(intent.Configuration, intent.SecretBindings, intent.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	intent.SecretBindingVersions = nil
	for _, binding := range intent.SecretBindings {
		intent.SecretBindingVersions = append(intent.SecretBindingVersions, binding.Name+":"+binding.SecretRef+":"+binding.Version+":"+binding.Key)
	}
	intent.DataBindingIDs = []string{intent.DataBindingID}
	intent.RequestHash = workspaceApplicationDeploymentRequestHash(intent)
	payload, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{
		"id": intent.OperationID, "operationId": intent.OperationID, "accountId": intent.AccountID,
		"workspaceId": intent.WorkspaceID, "resourceId": intent.WorkspaceID, "resourceKind": "workspace",
		"action": workspaceApplicationDeploymentAction, "status": "pending", "result": string(payload), "createdAt": intent.CreatedAt,
	}
	seedWorkspaceApplicationDeploymentIntentForTest(t, app.tables, row)
	return intent
}

// seedWorkspaceApplicationDeploymentIntentForTest stores one deployment
// intent row and reserves it on its Workspace. The production claim path is
// retired with the Workspace Gateway key rotation, so the row and the
// reservation are written directly.
func seedWorkspaceApplicationDeploymentIntentForTest(t *testing.T, store controlPlaneTableStore, row map[string]any) {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}
	workspaceID := stringValue(row["workspaceId"])
	workspace, found, err := store.GetWorkspace(ctx, workspaceID)
	if err != nil || !found {
		t.Fatalf("fixture workspace missing: found=%v err=%v", found, err)
	}
	workspace["reservedApplicationDeploymentId"] = stringValue(row["id"])
	if err := store.SaveWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}
}

// seedResourceOnlyActivatedWorkspace stores a Workspace whose Launch reached a
// succeeded, resource-bound state. It seeds the accepted Launch history, not an
// application deployment: the deployment execution engine is retired, so the
// retained readers project this Workspace from its Launch operation alone.
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

// knowledgeApplicationRevisionForTest is the admitted revision the retained
// history readers project.
func knowledgeApplicationRevisionForTest() contracts.WorkspaceApplicationRevision {
	return contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
		Image:     "repo.example/apps/knowledge@sha256:" + strings.Repeat("a", 64),
		Ports:     []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		EntryPort: "http", ExposurePolicy: "application",
	}
}

// createKnowledgeDeploymentForTest stores one accepted knowledge-application
// deployment intent. The deployment execution engine is retired, so the intent
// is seeded directly and only the retained readers project it.
func createKnowledgeDeploymentForTest(t *testing.T, app *controlPlaneServer, mutationKey string) workspaceApplicationDeploymentIntent {
	t.Helper()
	revision := knowledgeApplicationRevisionForTest()
	intent := applicationDeploymentFixtureForTest(t, app, "ws-alpha", mutationKey, revision)
	intent.Configuration = contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"CONFIG": strings.Repeat("c", 64)}}
	intent = seedAcceptedApplicationDeploymentForTest(t, app, intent)
	return intent
}

// applicationDeploymentWorkerFixture builds the retained server and one accepted
// deployment intent whose history the operator route projects.
func applicationDeploymentWorkerFixture(t *testing.T, store controlPlaneTableStore, service *controlplane.Service) (*controlPlaneHTTPHandler, string) {
	t.Helper()
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	handler := server.(*controlPlaneHTTPHandler)
	intent := createKnowledgeDeploymentForTest(t, handler.app, "deploy-worker")
	return handler, intent.OperationID
}
