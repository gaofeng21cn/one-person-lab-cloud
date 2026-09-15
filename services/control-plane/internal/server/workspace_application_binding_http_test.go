package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

func applicationBindingHTTPFixture(t *testing.T) (*controlPlaneHTTPHandler, *httptest.ResponseRecorder, *applicationReplacementFabric, workspaceApplicationDeploymentIntent) {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	ctx := context.Background()
	store := newMemoryTableStore()
	fabric := &applicationReplacementFabric{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.(*controlPlaneHTTPHandler)
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	installation := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: "owner-alpha", Sub2APIUserID: 41, WorkspaceKeyGroupID: 5, Revision: defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("b", 64)), Phase: "waiting_resources", WorkspaceAPIKeyID: 19, GatewaySecret: &clients.GatewaySecretWriteResult{SecretRef: "gateway-original", Version: "v1"}}
	row, err := workspaceDefaultApplicationRow(installation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, row))
	for range 6 {
		mustStore(t, handler.app.runWorkspaceDefaultApplicationsOnce(ctx, service))
	}
	workspace, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	current, found, err := handler.app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found || current.Phase != workspaceApplicationDeploymentActivePhase {
		t.Fatalf("default installation not active: %+v found=%v err=%v", current, found, err)
	}
	return handler, operatorSessionForTest(t, server), fabric, current
}

func applicationBindingHTTPRevision() contracts.WorkspaceApplicationRevision {
	uid, gid := int64(1000), int64(1000)
	return contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "generic-bound-app", Version: "1.0.0", Platform: "linux/amd64", Image: "repo.example/generic@sha256:" + strings.Repeat("c", 64),
		Execution: contracts.WorkspaceApplicationExecution{UserID: &uid, GroupID: &gid, Init: true}, Entrypoint: []string{"/app/server"},
		Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}}, EntryPort: "http", ExposurePolicy: "application",
		HealthChecks:     []contracts.WorkspaceApplicationHealthCheck{{Port: 8080, Path: "/health", InitialDelaySeconds: 2}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}}, ScratchMounts: []contracts.WorkspaceApplicationMount{{Name: "tmp", MountPath: "/tmp"}},
		SecretInputs: []contracts.WorkspaceApplicationSecretInput{{Name: "api_token", Env: "API_TOKEN"}}, ConfigInputs: []contracts.WorkspaceApplicationConfigInput{{Name: "app_config", Target: "/etc/app/config.json"}},
		Dependencies: []contracts.WorkspaceApplicationDependency{{Name: "database", Image: "repo.example/database@sha256:" + strings.Repeat("d", 64),
			Execution: contracts.WorkspaceApplicationExecution{UserID: &uid, GroupID: &gid},
			Ports:     []contracts.WorkspaceApplicationDependencyPort{{Name: "database", Port: 5432, Protocol: "TCP"}}, HealthChecks: []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "tcp", Port: 5432}},
			PersistentMounts: []contracts.WorkspaceApplicationDependencyMount{{Name: "database", MountPath: "/var/lib/database"}},
			Command:          contracts.WorkspaceApplicationDependencyCommand{Entrypoint: []string{"/app/database"}, Args: []string{"--config", "/etc/database/config.ini"}, Env: map[string]string{"DATABASE_MODE": "single"}},
			SecretInputs:     []contracts.WorkspaceApplicationSecretInput{{Name: "database_password", Target: "/run/secrets/database_password"}}, ConfigInputs: []contracts.WorkspaceApplicationConfigInput{{Name: "database_config", Target: "/etc/database/config.ini"}},
		}},
	}
}

func applicationBindingHTTPInputs() (contracts.WorkspaceApplicationRuntimeConfiguration, []contracts.WorkspaceApplicationRuntimeSecretBinding) {
	return contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"APP_MODE": "test"}, Files: map[string]string{"app_config": "{\"mode\":\"test\"}\n", "database_config": "max_connections=10\n"}}, []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "api_token", SecretRef: "installation-api", Version: "api-v1", Key: "token"}, {Name: "database_password", SecretRef: "installation-database", Version: "database-v1", Key: "password"}}
}

func applicationBindingHTTPBody(t *testing.T, revision contracts.WorkspaceApplicationRevision, configuration contracts.WorkspaceApplicationRuntimeConfiguration, bindings []contracts.WorkspaceApplicationRuntimeSecretBinding) string {
	t.Helper()
	payload, err := json.Marshal(struct {
		WorkspaceID    string                                               `json:"workspaceId"`
		ApplicationID  string                                               `json:"applicationId"`
		TargetRevision string                                               `json:"targetRevision"`
		Configuration  contracts.WorkspaceApplicationRuntimeConfiguration   `json:"configuration"`
		SecretBindings []contracts.WorkspaceApplicationRuntimeSecretBinding `json:"secretBindings"`
	}{"ws-alpha", revision.ApplicationID, revision.Version, configuration, bindings})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func admitBindingHTTPRevision(t *testing.T, handler *controlPlaneHTTPHandler, operator *httptest.ResponseRecorder, revision contracts.WorkspaceApplicationRevision) {
	t.Helper()
	payload, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-revisions", string(payload), "admit-"+revision.ApplicationID+revision.Version)
	if response.Code != http.StatusOK {
		t.Fatalf("revision HTTP admission status=%d body=%s", response.Code, response.Body.String())
	}
	row, found, err := handler.app.tables.AdmittedApplicationRevision(context.Background(), revision.ApplicationID, revision.Version)
	if err != nil || !found {
		t.Fatalf("admitted revision missing: found=%v err=%v", found, err)
	}
	stored, valid := decodeApplicationRevisionPayload(stringValue(row["payload"]))
	if !valid || !reflect.DeepEqual(stored, revision) {
		t.Fatalf("HTTP admission dropped revision fields: %+v", stored)
	}
}

func acceptedBindingHTTPIntent(t *testing.T, response *httptest.ResponseRecorder) workspaceApplicationDeploymentIntent {
	t.Helper()
	var result struct {
		Intent workspaceApplicationDeploymentIntent `json:"intent"`
	}
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Intent.OperationID == "" {
		t.Fatalf("deployment HTTP status=%d body=%s", response.Code, response.Body.String())
	}
	return result.Intent
}

func TestWorkspaceApplicationBindingHTTPReplacementAndPreflight(t *testing.T) {
	handler, operator, fabric, original := applicationBindingHTTPFixture(t)
	ctx := context.Background()
	before, _, _ := handler.app.tables.GetWorkspace(ctx, "ws-alpha")
	purchase, _, _ := handler.app.tables.GetRuntimeOperation(ctx, "workspace-launch-alpha")
	revision := applicationBindingHTTPRevision()
	admitBindingHTTPRevision(t, handler, operator, revision)
	configuration, bindings := applicationBindingHTTPInputs()
	body := applicationBindingHTTPBody(t, revision, configuration, bindings)
	replacement := acceptedBindingHTTPIntent(t, requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", body, "binding-replacement"))
	if replacement.PreviousDeploymentID != original.OperationID || replacement.DataBindingID == original.DataBindingID || replacement.WorkspaceAPIKeyID != 0 || replacement.Configuration.CredentialVersion != "" {
		t.Fatalf("generic replacement inherited OPL data or credentials: %+v", replacement)
	}
	runDeploymentWorkerToCompletion(t, handler.app, handler.service, replacement.OperationID)
	row, _, _ := handler.app.tables.GetRuntimeOperation(ctx, replacement.OperationID)
	active, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || active.Phase != workspaceApplicationDeploymentActivePhase {
		t.Fatalf("replacement not active: %+v err=%v", active, err)
	}
	sent, exists := fabric.inputs[replacement.OperationID+":runtime"]
	if !exists || !reflect.DeepEqual(sent.Revision, revision) || !reflect.DeepEqual(sent.Configuration, configuration) || !reflect.DeepEqual(sent.SecretBindings, bindings) || sent.DataBindingID != replacement.DataBindingID || sent.ConfigurationDigest != replacement.ConfigurationDigest {
		t.Fatalf("Fabric did not receive exact accepted inputs: %+v", sent)
	}
	if len(fabric.lifecycle) != 2 || fabric.lifecycle[0].DesiredState != "suspended" || fabric.lifecycle[1].DesiredState != "absent" || fabric.lifecycle[0].RuntimeOperationID != original.OperationID+":runtime" || fabric.lifecycle[1].RuntimeOperationID != original.OperationID+":runtime" || fabric.states[original.OperationID+":runtime"] != "absent" {
		t.Fatalf("predecessor lifecycle=%+v", fabric.lifecycle)
	}
	after, _, _ := handler.app.tables.GetWorkspace(ctx, "ws-alpha")
	for _, field := range []string{"storageId", "computeAllocationId", "currentComputeAllocationId", "attachmentId", "currentAttachmentId", "paidThrough", "periodStart", "purchaseReceiptId"} {
		if !reflect.DeepEqual(before[field], after[field]) {
			t.Fatalf("resource/purchase field %s changed", field)
		}
	}
	retained, _, _ := handler.app.tables.GetRuntimeOperation(ctx, "workspace-launch-alpha")
	if !reflect.DeepEqual(retained, purchase) || stringValue(after["currentApplicationDeploymentId"]) != replacement.OperationID {
		t.Fatal("replacement changed resource purchase or failed selection")
	}

	fabric.preflightErr = errors.New("workspace_application_secret_binding_missing")
	failed := acceptedBindingHTTPIntent(t, requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", body, "binding-preflight-rejected"))
	lifecycleBefore, runtimeBefore := len(fabric.lifecycle), len(fabric.inputs)
	mustStore(t, handler.app.runWorkspaceApplicationDeployment(ctx, handler.service, failed.OperationID))
	row, _, _ = handler.app.tables.GetRuntimeOperation(ctx, failed.OperationID)
	failure, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || failure.Phase != workspaceApplicationDeploymentManualReviewPhase || !strings.Contains(failure.LastError, "workspace_application_secret_binding_missing") {
		t.Fatalf("preflight rejection not recorded: %+v err=%v", failure, err)
	}
	after, _, _ = handler.app.tables.GetWorkspace(ctx, "ws-alpha")
	if len(fabric.lifecycle) != lifecycleBefore || len(fabric.inputs) != runtimeBefore || fabric.states[replacement.OperationID+":runtime"] != "ready" || stringValue(after["currentApplicationDeploymentId"]) != replacement.OperationID {
		t.Fatal("Fabric preflight rejection interrupted selected application")
	}
}

func TestWorkspaceApplicationBindingHTTPCommandIdentity(t *testing.T) {
	handler, operator, fabric, _ := applicationBindingHTTPFixture(t)
	revision := applicationBindingHTTPRevision()
	admitBindingHTTPRevision(t, handler, operator, revision)
	configuration, bindings := applicationBindingHTTPInputs()
	original := acceptedBindingHTTPIntent(t, requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", applicationBindingHTTPBody(t, revision, configuration, bindings), "binding-identity"))
	reordered := []contracts.WorkspaceApplicationRuntimeSecretBinding{bindings[1], bindings[0]}
	replay := acceptedBindingHTTPIntent(t, requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", applicationBindingHTTPBody(t, revision, configuration, reordered), "binding-identity"))
	if !reflect.DeepEqual(replay, original) {
		t.Fatal("binding reordering changed the accepted command")
	}
	for _, change := range []string{"secretRef", "version", "key", "files", "environment"} {
		t.Run(change, func(t *testing.T) {
			configuration, bindings := applicationBindingHTTPInputs()
			switch change {
			case "secretRef":
				bindings[0].SecretRef = "another-installation-secret"
			case "version":
				bindings[0].Version = "api-v2"
			case "key":
				bindings[0].Key = "other-token"
			case "files":
				configuration.Files["database_config"] = "max_connections=20\n"
			case "environment":
				configuration.Environment["APP_MODE"] = "changed"
			}
			rejected := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", applicationBindingHTTPBody(t, revision, configuration, bindings), "binding-identity")
			if rejected.Code != http.StatusConflict || !strings.Contains(rejected.Body.String(), errWorkspaceApplicationIntentConflict.Error()) {
				t.Fatalf("changed %s status=%d body=%s", change, rejected.Code, rejected.Body.String())
			}
		})
	}
	if len(fabric.lifecycle) != 0 || len(fabric.inputs) != 1 {
		t.Fatal("HTTP intent/replays changed runtime before worker execution")
	}
}

func TestWorkspaceApplicationBindingHTTPRejectsInvalidBindings(t *testing.T) {
	handler, operator, fabric, original := applicationBindingHTTPFixture(t)
	revision := applicationBindingHTTPRevision()
	admitBindingHTTPRevision(t, handler, operator, revision)
	for _, test := range []struct{ name, code string }{
		{"missing", "workspace_application_secret_binding_missing"},
		{"duplicate", "invalid_application_secret_bindings"},
		{"value", "invalid_application_secret_bindings"},
		{"configuration_unknown", "invalid_application_configuration"},
		{"opl_owned", "workspace_application_owned_configuration_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration, bindings := applicationBindingHTTPInputs()
			target := revision
			switch test.name {
			case "missing":
				bindings = bindings[:1]
			case "duplicate":
				bindings = append(bindings, bindings[0])
			case "opl_owned":
				target.ApplicationID, target.Version = original.ApplicationID, original.TargetRevision
				configuration = contracts.WorkspaceApplicationRuntimeConfiguration{}
				bindings = original.SecretBindings
			}
			body := applicationBindingHTTPBody(t, target, configuration, bindings)
			if test.name == "configuration_unknown" {
				body = strings.Replace(body, `"configuration":{`, `"configuration":{"unknown":true,`, 1)
			}
			if test.name == "value" {
				body = strings.Replace(body, `"secretRef":"installation-api"`, `"secretRef":"installation-api","value":"forbidden-inline-value"`, 1)
			}
			key := "binding-invalid-" + test.name
			response := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-deployments", body, key)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("invalid binding status=%d body=%s", response.Code, response.Body.String())
			}
			_, found, err := handler.app.tables.GetRuntimeOperation(context.Background(), workspaceApplicationDeploymentOperationID("ws-alpha", key))
			if err != nil || found {
				t.Fatalf("invalid request persisted intent: found=%v err=%v", found, err)
			}
		})
	}
	if len(fabric.lifecycle) != 0 || len(fabric.inputs) != 1 || fabric.states[original.OperationID+":runtime"] != "ready" {
		t.Fatal("invalid HTTP bindings changed current runtime")
	}
}

func TestWorkspaceApplicationBindingHTTPRejectsUnknownDependencyFields(t *testing.T) {
	handler, operator, _, _ := applicationBindingHTTPFixture(t)
	revision := applicationBindingHTTPRevision()
	payload, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(payload), `"name":"database_password","target"`, `"name":"database_password","value":"forbidden-inline-value","target"`, 1)
	if body == string(payload) {
		t.Fatal("negative fixture did not inject unknown nested field")
	}
	response := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, "/api/operator/application-revisions", body, "unknown-dependency-field")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_application_revision") {
		t.Fatalf("unknown revision field status=%d body=%s", response.Code, response.Body.String())
	}
	_, found, err := handler.app.tables.AdmittedApplicationRevision(context.Background(), revision.ApplicationID, revision.Version)
	if err != nil || found {
		t.Fatalf("invalid revision admitted: found=%v err=%v", found, err)
	}
}
