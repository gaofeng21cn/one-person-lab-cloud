package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
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
