package server

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

func applicationBindingFixture(t *testing.T) (*controlPlaneHTTPHandler, *applicationReplacementFabric, workspaceApplicationDeploymentIntent) {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
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
	current := seedHistoricalDefaultApplicationForTest(t, handler.app, service, fabric, installation)
	return handler, fabric, current
}

func applicationBindingRevision() contracts.WorkspaceApplicationRevision {
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

func applicationBindingInputs() (contracts.WorkspaceApplicationRuntimeConfiguration, []contracts.WorkspaceApplicationRuntimeSecretBinding) {
	return contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"APP_MODE": "test"}, Files: map[string]string{"app_config": "{\"mode\":\"test\"}\n", "database_config": "max_connections=10\n"}}, []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "api_token", SecretRef: "installation-api", Version: "api-v1", Key: "token"}, {Name: "database_password", SecretRef: "installation-database", Version: "database-v1", Key: "password"}}
}

func admitBindingRevisionForTest(t *testing.T, handler *controlPlaneHTTPHandler, revision contracts.WorkspaceApplicationRevision) {
	t.Helper()
	seedAdmittedApplicationRevisionForTest(t, handler.app.tables, revision)
	row, found, err := handler.app.tables.AdmittedApplicationRevision(context.Background(), revision.ApplicationID, revision.Version)
	if err != nil || !found {
		t.Fatalf("admitted revision missing: found=%v err=%v", found, err)
	}
	stored, valid := decodeApplicationRevisionPayload(stringValue(row["payload"]))
	if !valid || !reflect.DeepEqual(stored, revision) {
		t.Fatalf("revision store dropped fields: %+v", stored)
	}
}

func createBindingIntentForTest(t *testing.T, app *controlPlaneServer, revision contracts.WorkspaceApplicationRevision, configuration contracts.WorkspaceApplicationRuntimeConfiguration, bindings []contracts.WorkspaceApplicationRuntimeSecretBinding, mutationKey string) workspaceApplicationDeploymentIntent {
	t.Helper()
	intent, err := app.createWorkspaceApplicationDeploymentIntent(context.Background(), "ws-alpha", mutationKey, revision.ApplicationID, revision.Version, configuration, bindings, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func TestWorkspaceApplicationBindingReplacementAndPreflight(t *testing.T) {
	handler, fabric, original := applicationBindingFixture(t)
	ctx := context.Background()
	before, _, _ := handler.app.tables.GetWorkspace(ctx, "ws-alpha")
	purchase, _, _ := handler.app.tables.GetRuntimeOperation(ctx, "workspace-launch-alpha")
	revision := applicationBindingRevision()
	admitBindingRevisionForTest(t, handler, revision)
	configuration, bindings := applicationBindingInputs()
	replacement := createBindingIntentForTest(t, handler.app, revision, configuration, bindings, "binding-replacement")
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
	failed := createBindingIntentForTest(t, handler.app, revision, configuration, bindings, "binding-preflight-rejected")
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

func TestWorkspaceApplicationBindingCommandIdentity(t *testing.T) {
	handler, fabric, _ := applicationBindingFixture(t)
	revision := applicationBindingRevision()
	admitBindingRevisionForTest(t, handler, revision)
	configuration, bindings := applicationBindingInputs()
	original := createBindingIntentForTest(t, handler.app, revision, configuration, bindings, "binding-identity")
	reordered := []contracts.WorkspaceApplicationRuntimeSecretBinding{bindings[1], bindings[0]}
	replay := createBindingIntentForTest(t, handler.app, revision, configuration, reordered, "binding-identity")
	if !reflect.DeepEqual(replay, original) {
		t.Fatal("binding reordering changed the accepted command")
	}
	for _, change := range []string{"secretRef", "version", "key", "files", "environment"} {
		t.Run(change, func(t *testing.T) {
			configuration, bindings := applicationBindingInputs()
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
			_, err := handler.app.createWorkspaceApplicationDeploymentIntent(context.Background(), "ws-alpha", "binding-identity", revision.ApplicationID, revision.Version, configuration, bindings, 0, "", "")
			if !errors.Is(err, errWorkspaceApplicationIntentConflict) {
				t.Fatalf("changed %s error=%v", change, err)
			}
		})
	}
	if len(fabric.lifecycle) != 0 || len(fabric.inputs) != 1 {
		t.Fatal("Owner intent/replays changed runtime before worker execution")
	}
}

func TestWorkspaceApplicationBindingRejectsInvalidBindings(t *testing.T) {
	handler, fabric, original := applicationBindingFixture(t)
	revision := applicationBindingRevision()
	admitBindingRevisionForTest(t, handler, revision)
	for _, name := range []string{"missing", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			configuration, bindings := applicationBindingInputs()
			if name == "missing" {
				bindings = bindings[:1]
			} else {
				bindings = append(bindings, bindings[0])
			}
			key := "binding-invalid-" + name
			_, err := handler.app.createWorkspaceApplicationDeploymentIntent(context.Background(), "ws-alpha", key, revision.ApplicationID, revision.Version, configuration, bindings, 0, "", "")
			if !errors.Is(err, errWorkspaceApplicationConfigurationInvalid) {
				t.Fatalf("invalid %s binding error=%v", name, err)
			}
			_, found, err := handler.app.tables.GetRuntimeOperation(context.Background(), workspaceApplicationDeploymentOperationID("ws-alpha", key))
			if err != nil || found {
				t.Fatalf("invalid bindings persisted intent: found=%v err=%v", found, err)
			}
		})
	}
	if len(fabric.lifecycle) != 0 || len(fabric.inputs) != 1 || fabric.states[original.OperationID+":runtime"] != "ready" {
		t.Fatal("invalid bindings changed current runtime")
	}
}
