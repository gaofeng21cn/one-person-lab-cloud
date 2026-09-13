package server

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

func TestWorkspaceApplicationDeploymentHTTPReplayRetainsAcceptedCredentials(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	ctx := context.Background()
	store := newMemoryTableStore()
	fabric := &applicationReplacementFabric{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	app := server.(*controlPlaneHTTPHandler).app
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	revision := defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("e", 64))
	installation := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha",
		AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: "owner-alpha", Sub2APIUserID: 41, WorkspaceKeyGroupID: 5,
		Revision: revision, Phase: "waiting_resources", WorkspaceAPIKeyID: 19, GatewaySecret: &clients.GatewaySecretWriteResult{SecretRef: "gateway-original", Version: "v1"}}
	installationRow, err := workspaceDefaultApplicationRow(installation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, installationRow))
	for range 6 {
		mustStore(t, app.runWorkspaceDefaultApplicationsOnce(ctx, service))
	}
	operator := operatorSessionForTest(t, server)
	configuration := contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"CLIENT_SETTING": "original"}}
	body := func(applicationID, revisionID string, configuration contracts.WorkspaceApplicationRuntimeConfiguration) string {
		t.Helper()
		payload, err := json.Marshal(struct {
			WorkspaceID   string                                             `json:"workspaceId"`
			ApplicationID string                                             `json:"applicationId"`
			Revision      string                                             `json:"targetRevision"`
			Configuration contracts.WorkspaceApplicationRuntimeConfiguration `json:"configuration"`
		}{"ws-alpha", applicationID, revisionID, configuration})
		if err != nil {
			t.Fatal(err)
		}
		return string(payload)
	}
	originalBody := body(revision.ApplicationID, revision.Version, configuration)
	created := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", originalBody, "opl-http-command")
	var response struct {
		Intent workspaceApplicationDeploymentIntent `json:"intent"`
	}
	if created.Code != http.StatusAccepted || json.Unmarshal(created.Body.Bytes(), &response) != nil || response.Intent.ClientConfigurationDigest == "" {
		t.Fatalf("HTTP deployment status=%d body=%s", created.Code, created.Body.String())
	}
	runDeploymentWorkerToCompletion(t, app, service, response.Intent.OperationID)
	acceptedRow, _, _ := store.GetRuntimeOperation(ctx, response.Intent.OperationID)
	accepted, err := decodeWorkspaceApplicationDeploymentIntent(acceptedRow)
	if err != nil || accepted.Phase != workspaceApplicationDeploymentActivePhase {
		t.Fatalf("HTTP deployment did not activate: %+v %v", accepted, err)
	}

	// A later credential deployment changes the current owner configuration,
	// Secret version and Key while the original HTTP command remains immutable.
	rotatedConfiguration := contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{}}
	for name, value := range accepted.Configuration.Environment {
		rotatedConfiguration.Environment[name] = value
	}
	rotatedConfiguration.Environment["OWNER_CURRENT_SETTING"] = "new"
	rotatedBindings := append([]contracts.WorkspaceApplicationRuntimeSecretBinding(nil), accepted.SecretBindings...)
	rotatedBindings[0].SecretRef, rotatedBindings[0].Version = "gateway-rotated", "v2"
	rotated, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "rotate-current", revision.ApplicationID, revision.Version, rotatedConfiguration, rotatedBindings, 20, "", "")
	if err != nil {
		t.Fatal(err)
	}
	runDeploymentWorkerToCompletion(t, app, service, rotated.OperationID)
	workspaceBefore, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	if stringValue(workspaceBefore["currentApplicationDeploymentId"]) != rotated.OperationID || int64(numberField(workspaceBefore, "workspaceApiKeyId", 0)) != 20 {
		t.Fatal("credential successor did not become current")
	}
	lifecycleCount := len(fabric.lifecycle)
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", originalBody, "opl-http-command")
	if replay.Code != http.StatusAccepted || json.Unmarshal(replay.Body.Bytes(), &response) != nil || !reflect.DeepEqual(response.Intent, accepted) {
		t.Fatalf("HTTP replay did not return frozen intent: status=%d body=%s", replay.Code, replay.Body.String())
	}
	for _, test := range []struct {
		name string
		body string
	}{
		{"changed configuration", body(revision.ApplicationID, revision.Version, contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"CLIENT_SETTING": "changed"}})},
		{"omitted configuration", body(revision.ApplicationID, revision.Version, contracts.WorkspaceApplicationRuntimeConfiguration{})},
		{"different application", body("another-app", revision.Version, configuration)},
		{"different revision", body(revision.ApplicationID, "another-revision", configuration)},
	} {
		t.Run(test.name, func(t *testing.T) {
			denied := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", test.body, "opl-http-command")
			if denied.Code != http.StatusConflict || !strings.Contains(denied.Body.String(), errWorkspaceApplicationIntentConflict.Error()) {
				t.Fatalf("changed client command accepted: %d %s", denied.Code, denied.Body.String())
			}
		})
	}
	retained, _, _ := store.GetRuntimeOperation(ctx, accepted.OperationID)
	workspaceAfter, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	if retained["result"] != acceptedRow["result"] || !reflect.DeepEqual(workspaceBefore, workspaceAfter) || len(fabric.lifecycle) != lifecycleCount {
		t.Fatal("HTTP replay changed the original command, current selection, or runtime")
	}
}
