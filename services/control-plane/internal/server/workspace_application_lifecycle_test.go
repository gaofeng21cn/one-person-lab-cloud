package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type applicationLifecycleFabric struct {
	*workspaceDeleteFabric
	states            map[string]string
	failRuntime       string
	mutations         []clients.WorkspaceApplicationRuntimeLifecycleInput
	readbackError     bool
	readbackTransform func(contracts.WorkspaceApplicationRuntimeLifecycleResult) contracts.WorkspaceApplicationRuntimeLifecycleResult
	secretMutations   []clients.WorkspaceApplicationGatewaySecretCleanupInput
	secretError       error
	runtimeInputs     []clients.WorkspaceApplicationRuntimeInput
}

func (f *applicationLifecycleFabric) ReadWorkspaceApplicationRuntimeLifecycle(_ context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	if input.RuntimeID == f.failRuntime {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("injected_application_read_failure")
	}
	state := f.states[input.RuntimeID]
	if state == "" {
		state = "running"
	}
	result := contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: state}
	if f.readbackTransform != nil {
		result = f.readbackTransform(result)
	}
	return result, nil
}

func (f *applicationLifecycleFabric) RemoveWorkspaceApplicationGatewaySecret(_ context.Context, input clients.WorkspaceApplicationGatewaySecretCleanupInput, key string) error {
	f.secretMutations = append(f.secretMutations, input)
	if f.events != nil {
		f.events.add("application:secret:" + input.SecretRef)
	}
	return f.secretError
}

func (f *applicationLifecycleFabric) SetWorkspaceApplicationRuntimeLifecycle(_ context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput, key string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	f.mutations = append(f.mutations, input)
	f.states[input.RuntimeID] = input.DesiredState
	if f.events != nil {
		f.events.add("application:" + input.DesiredState + ":" + input.RuntimeOperationID)
	}
	return contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: input.DesiredState}, nil
}

func (f *applicationLifecycleFabric) ReadWorkspaceApplicationRuntimeCredentials(_ context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	return contracts.WorkspaceApplicationRuntimeCredentials{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, WebUIUsername: "owner", WebUIPassword: "test-current-password"}, nil
}

func (f *applicationLifecycleFabric) ReadWorkspaceApplicationRuntime(_ context.Context, input clients.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	f.runtimeInputs = append(f.runtimeInputs, input)
	if f.readbackError {
		return contracts.WorkspaceApplicationRuntimeObservation{}, errors.New("injected_runtime_unavailable")
	}
	components := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)
	for index := range components {
		components[index].State = "ready"
	}
	entryURL := ""
	if input.Revision.EntryPort != "" {
		entryURL = "http://application.example/"
	}
	runtimeID := contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID)
	if input.SchemaVersion == 0 {
		runtimeID = contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID)
	}
	return contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: runtimeID, Status: "ready", EntryURL: entryURL, Components: components}, nil
}

func seedCurrentApplicationForLifecycle(t *testing.T, app *controlPlaneServer, workspaceID, applicationID string, selected bool) workspaceApplicationDeploymentIntent {
	t.Helper()
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: applicationID, Version: "1.0.0", Platform: "linux/amd64", Image: "registry.example/" + applicationID + "@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "application", EntryPort: "http", Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}}}
	return seedApplicationRevisionForLifecycle(t, app, workspaceID, revision, contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, selected)
}

func seedApplicationRevisionForLifecycle(t *testing.T, app *controlPlaneServer, workspaceID string, revision contracts.WorkspaceApplicationRevision, configuration contracts.WorkspaceApplicationRuntimeConfiguration, bindings []contracts.WorkspaceApplicationRuntimeSecretBinding, keyID int64, selected bool) workspaceApplicationDeploymentIntent {
	t.Helper()
	if _, _, err := app.admitWorkspaceApplicationRevision(context.Background(), revision, "operator-test"); err != nil {
		t.Fatal(err)
	}
	intent, err := app.createWorkspaceApplicationDeploymentIntent(context.Background(), workspaceID, "seed-"+revision.ApplicationID+"-"+revision.Version, revision.ApplicationID, revision.Version, configuration, bindings, keyID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !selected {
		return intent
	}
	row, found, err := app.tables.GetRuntimeOperation(context.Background(), intent.OperationID)
	if err != nil || !found {
		t.Fatal("missing intent", err)
	}
	intent.Phase, intent.ActivationAt, intent.ReceiptID = workspaceApplicationDeploymentActivePhase, time.Now().UTC().Format(time.RFC3339Nano), "application-receipt"
	payload, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	row["result"], row["status"] = string(payload), "succeeded"
	if err := app.tables.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	workspace, found, err := app.tables.GetWorkspace(context.Background(), workspaceID)
	if err != nil || !found {
		t.Fatal("missing workspace", err)
	}
	workspace["applicationBinding"], workspace["applicationBindingVersion"] = revision.ApplicationID+"@"+revision.Version, intent.ExpectedWorkspaceVersion+1
	workspace["currentApplicationDeploymentId"], workspace["reservedApplicationDeploymentId"] = intent.OperationID, ""
	if err := app.tables.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	return intent
}

func TestApplicationLifecycleSuspendsEveryOwnedRuntimeAndRecoversSelectedOnly(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	selected := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected-app", true)
	candidate := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "candidate-app", false)
	workspace, _, _ := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	stop, err := app.workspaceApplicationLifecycleInventory(context.Background(), fixture.server.(*controlPlaneHTTPHandler).service, workspace, "suspended", "renewal-owner")
	if err != nil || len(stop.Runtimes) != 2 {
		t.Fatalf("stop inventory=%#v error=%v", stop, err)
	}
	recover, err := app.workspaceApplicationLifecycleInventory(context.Background(), fixture.server.(*controlPlaneHTTPHandler).service, workspace, "running", "renewal-owner")
	if err != nil || len(recover.Runtimes) != 1 || recover.Runtimes[0].Input.RuntimeOperationID != selected.OperationID+":runtime" {
		t.Fatalf("recovery=%#v error=%v", recover, err)
	}
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}, failRuntime: contracts.WorkspaceApplicationRuntimeID(candidate.OperationID + ":runtime")}
	service := newTestService(&fakeLedgerClient{}, fabric)
	persists := 0
	err = app.convergeWorkspaceApplicationLifecycle(context.Background(), service, stop, func() error { persists++; return nil })
	if err == nil || len(fabric.mutations) != 1 || fabric.mutations[0].RuntimeOperationID != selected.OperationID+":runtime" || persists != 1 {
		t.Fatalf("partial suspension err=%v mutations=%#v persists=%d", err, fabric.mutations, persists)
	}
	fabric.failRuntime = ""
	if err := app.convergeWorkspaceApplicationLifecycle(context.Background(), service, stop, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !workspaceApplicationLifecycleComplete(stop, "suspended") || len(fabric.mutations) != 2 {
		t.Fatalf("suspension did not converge: %#v", stop)
	}
	if err := app.convergeWorkspaceApplicationLifecycle(context.Background(), service, recover, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if fabric.states[contracts.WorkspaceApplicationRuntimeID(candidate.OperationID+":runtime")] != "suspended" {
		t.Fatal("recovery resumed an unselected candidate")
	}
}

func TestApplicationLifecycleDeleteClearsAllRuntimesBeforeResources(t *testing.T) {
	fixture, sub2api, ledger, events := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected-app", true)
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "candidate-app", false)
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}}
	service := controlplane.NewService(ledger, fabric, sub2api)
	server, err := NewPersistentServer(service, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	response := requestWithMutationKeyForTest(t, server, session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-with-applications")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(fabric.mutations) != 2 {
		t.Fatalf("cleanup inputs=%#v", fabric.mutations)
	}
	sequence := events.snapshot()
	cleanupCount := 0
	for _, event := range sequence {
		if strings.HasPrefix(event, "application:absent:") {
			cleanupCount++
		}
		if event == "fabric:attachment" && cleanupCount != 2 {
			t.Fatalf("resources detached before application absence: %#v", sequence)
		}
	}
	if sub2api.keyDeletes != 0 || len(sub2api.refunds) != 0 {
		t.Fatal("application cleanup changed money or Gateway keys")
	}
}

func TestApplicationAccessUsesSelectedRuntimeAndSuppressesLegacyCredentials(t *testing.T) {
	fixture, sub2api, ledger, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	selected := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected-app", true)
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}}
	service := controlplane.NewService(ledger, fabric, sub2api)
	server, err := NewPersistentServer(service, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	response := requestWithSession(t, server, session, http.MethodGet, "/api/workspaces/ws-alpha/runtime-status", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), selected.OperationID) || !strings.Contains(response.Body.String(), "http://application.example/") {
		t.Fatalf("current runtime status=%d %s", response.Code, response.Body.String())
	}
	credentials := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspaces/ws-alpha/runtime-credentials/reveal", `{}`, "reveal-current")
	if credentials.Code != http.StatusConflict || strings.Contains(credentials.Body.String(), "test-current-password") {
		t.Fatalf("undeclared credentials=%d %s", credentials.Code, credentials.Body.String())
	}
	budget := requestWithSession(t, server, session, http.MethodGet, "/api/workspaces/ws-alpha/gateway-budget", "")
	if budget.Code != http.StatusConflict {
		t.Fatalf("undeclared Gateway budget=%d %s", budget.Code, budget.Body.String())
	}
	fabric.readbackError = true
	response = requestWithSession(t, server, session, http.MethodGet, "/api/workspaces/ws-alpha/runtime-status", "")
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "application.example") {
		t.Fatalf("stale runtime readback=%d %s", response.Code, response.Body.String())
	}
}

func TestApplicationLifecycleFencesAbsentUnfinishedGeneration(t *testing.T) {
	for _, desired := range []string{"suspended", "absent"} {
		t.Run(desired, func(t *testing.T) {
			fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			candidate := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "candidate", false)
			workspace, _, _ := app.tables.GetWorkspace(context.Background(), "ws-alpha")
			fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{contracts.WorkspaceApplicationRuntimeID(candidate.OperationID + ":runtime"): "absent"}}
			service := newTestService(&fakeLedgerClient{}, fabric)
			inventory, err := app.workspaceApplicationLifecycleInventory(context.Background(), service, workspace, desired, "lifecycle-owner")
			if err != nil {
				t.Fatal(err)
			}
			if err := app.convergeWorkspaceApplicationLifecycle(context.Background(), service, inventory, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if len(fabric.mutations) != 1 || fabric.mutations[0].DesiredState != desired {
				t.Fatalf("absent generation never fenced: %+v", fabric.mutations)
			}
			if err := app.convergeWorkspaceApplicationLifecycle(context.Background(), service, inventory, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if len(fabric.mutations) != 1 {
				t.Fatal("confirmed fence was repeated")
			}
		})
	}
}

func TestApplicationLifecycleRejectsWrongOwnerReadbackBeforeMutation(t *testing.T) {
	for _, field := range []string{"runtime", "workspace", "state"} {
		t.Run(field, func(t *testing.T) {
			fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected", true)
			workspace, _, _ := app.tables.GetWorkspace(context.Background(), "ws-alpha")
			fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}}
			fabric.readbackTransform = func(result contracts.WorkspaceApplicationRuntimeLifecycleResult) contracts.WorkspaceApplicationRuntimeLifecycleResult {
				switch field {
				case "runtime":
					result.RuntimeID = "foreign-runtime"
				case "workspace":
					result.WorkspaceID = "foreign-workspace"
				case "state":
					result.State = "unexpected"
				}
				return result
			}
			service := newTestService(&fakeLedgerClient{}, fabric)
			inventory, err := app.workspaceApplicationLifecycleInventory(context.Background(), service, workspace, "suspended", "lifecycle-owner")
			if err != nil {
				t.Fatal(err)
			}
			persists := 0
			if err := app.convergeWorkspaceApplicationLifecycle(context.Background(), service, inventory, func() error { persists++; return nil }); err == nil {
				t.Fatal("invalid owner readback accepted")
			}
			if len(fabric.mutations) != 0 || persists != 0 {
				t.Fatal("invalid readback caused a mutation or completion claim")
			}
		})
	}
}

func TestApplicationLifecycleDeleteStopsBeforeResourcesWhenRuntimeUnconfirmed(t *testing.T) {
	fixture, sub2api, ledger, events := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	selected := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected", true)
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}, failRuntime: contracts.WorkspaceApplicationRuntimeID(selected.OperationID + ":runtime")}
	server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, sub2api), fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	response := requestWithMutationKeyForTest(t, server, session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-unconfirmed")
	if response.Code != http.StatusBadGateway {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	for _, event := range events.snapshot() {
		if event == "fabric:attachment" || event == "fabric:storage" || event == "fabric:compute" {
			t.Fatalf("resource mutated before runtime absence: %v", events.snapshot())
		}
	}
	fabric.failRuntime = ""
	response = requestWithMutationKeyForTest(t, server, session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-unconfirmed")
	if response.Code != http.StatusOK {
		t.Fatalf("resume=%d %s", response.Code, response.Body.String())
	}
}

func TestApplicationLifecycleDeleteCleansDefaultGatewaySecretAndRetainsKey(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending-default", true: "selected-default"}[installed], func(t *testing.T) {
			fixture, sub2api, ledger, events := newResourceOnlyWorkspaceLifecycleFixture(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
			secret := clients.GatewaySecretWriteResult{SecretRef: contracts.WorkspaceGatewaySecretRef("ws-alpha"), Version: "v1", Fingerprint: "sha256:" + strings.Repeat("b", 64)}
			request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceKeyGroupID: 7, Revision: revision, Phase: "waiting_resources", GatewaySecret: &secret, WorkspaceAPIKeyID: 19}
			row, err := workspaceDefaultApplicationRow(request)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, fixture.store.SaveRuntimeOperation(context.Background(), row))
			if installed {
				seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, contracts.WorkspaceApplicationRuntimeConfiguration{}, []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: secret.SecretRef, Version: "v2"}}, 19, true)
			}
			fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}, secretError: errors.New("injected_secret_cleanup_failure")}
			server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, sub2api), fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			session := tenantOwnerSessionForTest(t, server)
			response := requestWithMutationKeyForTest(t, server, session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-default-secret")
			if response.Code != http.StatusBadGateway || len(fabric.secretMutations) != 1 {
				t.Fatalf("delete=%d %s secrets=%+v", response.Code, response.Body.String(), fabric.secretMutations)
			}
			for _, event := range events.snapshot() {
				if event == "fabric:attachment" {
					t.Fatal("storage detached before application secret absence")
				}
			}
			if installed && (len(fabric.mutations) != 1 || fabric.mutations[0].DesiredState != "absent") {
				t.Fatal("secret cleanup ran before selected runtime absence")
			}
			fabric.secretError = nil
			response = requestWithMutationKeyForTest(t, server, session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-default-secret")
			if response.Code != http.StatusOK {
				t.Fatalf("resume=%d %s", response.Code, response.Body.String())
			}
			if sub2api.keyDeletes != 0 || !sub2api.keyExists {
				t.Fatal("Workspace deletion changed Gateway Key retention")
			}
			operation, found, err := server.(*controlPlaneHTTPHandler).app.workspaceDeleteOperation(context.Background(), "ws-alpha")
			if err != nil || !found || len(operation.ApplicationSecrets.Secrets) != 1 || operation.ApplicationSecrets.Secrets[0].State != "absent" || len(operation.ApplicationSecrets.RetainedGatewayKeyIDs) != 1 || operation.ApplicationSecrets.RetainedGatewayKeyIDs[0] != 19 {
				t.Fatalf("cleanup proof=%+v err=%v", operation.ApplicationSecrets, err)
			}
		})
	}
}

func TestApplicationAccessReadyWithoutWebEntryIsNotOpenable(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "worker", Version: "1", Platform: "linux/amd64", Image: "registry.example/worker@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "cloud_private"}
	seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, true)
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}}
	service := newTestService(&fakeLedgerClient{}, fabric)
	workspace, _, _ := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	current, observation, err := app.readWorkspaceCurrentApplication(context.Background(), service, workspace)
	if err != nil || current == nil || current.Status != "ready" {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	projection := map[string]any{"url": "http://retired.example/", "openable": true}
	projectWorkspaceCurrentApplication(workspace, projection, current)
	if projection["openable"] != false || projection["url"] != nil || workspaceCurrentApplicationRuntimeResponse(current, observation)["status"] != "running" {
		t.Fatalf("nonweb projection=%+v", projection)
	}
}

func TestApplicationAccessCredentialRotationWaitsForSelectedGeneration(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "false")
	fixture, sub2api, ledger, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
	selected := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, contracts.WorkspaceApplicationRuntimeConfiguration{}, []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: "opl-gateway-ws-alpha", Version: "v1"}}, 19, true)
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}}
	server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, sub2api), fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	response := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspaces/ws-alpha/runtime-credentials/rotate", `{}`, "password-once")
	if response.Code != http.StatusAccepted || strings.Contains(response.Body.String(), "test-current-password") || strings.Contains(response.Body.String(), `"access"`) {
		t.Fatalf("pending password=%d %s", response.Code, response.Body.String())
	}
	workspace, _, _ := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	if stringValue(workspace["currentApplicationDeploymentId"]) != selected.OperationID || stringValue(workspace["reservedApplicationDeploymentId"]) == "" {
		t.Fatal("pending rotation changed selected generation")
	}
	row, _, _ := app.tables.GetRuntimeOperation(context.Background(), stringValue(workspace["reservedApplicationDeploymentId"]))
	rotated, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || rotated.Configuration.CredentialVersion == "" || rotated.Configuration.CredentialVersion == selected.Configuration.CredentialVersion || rotated.Configuration.CredentialSourceRuntimeOperationID != "" {
		t.Fatalf("explicit rotation did not advance credential identity: %+v %v", rotated, err)
	}
	replayed := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspaces/ws-alpha/runtime-credentials/rotate", `{}`, "password-once")
	if replayed.Code != http.StatusAccepted {
		t.Fatalf("password rotation replay=%d %s", replayed.Code, replayed.Body.String())
	}
	replayedRow, _, _ := app.tables.GetRuntimeOperation(context.Background(), rotated.OperationID)
	if replayedRow["result"] != row["result"] {
		t.Fatal("password rotation replay changed its accepted version")
	}
	projection := map[string]any{}
	if err := app.projectWorkspaceApplicationInstallation(context.Background(), workspace, projection); err != nil {
		t.Fatal(err)
	}
	installation, ok := projection["applicationInstallation"].(workspaceApplicationInstallationProjection)
	if !ok || installation.Status != "pending" {
		t.Fatalf("rotation installation=%+v", projection)
	}
}

func TestApplicationAccessGatewayRotationAdvancesCredentialIdentity(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "false")
	ctx := context.Background()
	fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	service := fixture.server.(*controlPlaneHTTPHandler).service
	revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
	selected := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, contracts.WorkspaceApplicationRuntimeConfiguration{}, []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: "opl-gateway-ws-alpha", Version: "v1"}}, 19, true)
	if selected.Configuration.CredentialSourceRuntimeOperationID == "" {
		t.Fatal("fixture did not inherit the full Runtime credential")
	}
	operation := workspaceKeyRotationOperation{RequestHash: "key-rotation-command", Phase: "runtime_binding", OldKeyID: 19, NewKeyID: 20, SecretRef: "opl-gateway-ws-alpha", SecretVersion: "v2", ReplacementName: "opl-workspace-ws-alpha-next", RetiredName: "opl-workspace-ws-alpha-retired"}
	handled, err := app.bindWorkspaceCurrentApplicationGateway(ctx, service, "ws-alpha", "key-rotation-operation", &operation)
	if !handled || !errors.Is(err, errWorkspaceKeyRotationInProgress) || operation.ApplicationDeploymentID == "" {
		t.Fatalf("rotation did not reserve a successor: %+v %v", operation, err)
	}
	row, _, _ := app.tables.GetRuntimeOperation(ctx, operation.ApplicationDeploymentID)
	rotated, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || rotated.Configuration.CredentialVersion == "" || rotated.Configuration.CredentialVersion == selected.Configuration.CredentialVersion || rotated.Configuration.CredentialSourceRuntimeOperationID != "" || rotated.WorkspaceAPIKeyID != 20 || rotated.SecretBindings[0].Version != "v2" {
		t.Fatalf("Key rotation did not advance credential and Secret binding together: %+v %v", rotated, err)
	}
	_, err = app.bindWorkspaceCurrentApplicationGateway(ctx, service, "ws-alpha", "key-rotation-operation", &operation)
	if !errors.Is(err, errWorkspaceKeyRotationInProgress) {
		t.Fatalf("Key rotation replay=%v", err)
	}
	replayedRow, _, _ := app.tables.GetRuntimeOperation(ctx, operation.ApplicationDeploymentID)
	if replayedRow["result"] != row["result"] {
		t.Fatal("Key rotation replay changed its accepted version")
	}
}

func TestApplicationLifecycleHistoricalRuntimeUsesSelectedLineageOnce(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	ctx := context.Background()
	historical := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "historical", true)
	historical.Version = 1
	historical.RequestHash = workspaceApplicationDeploymentRequestHash(historical)
	row, _, _ := app.tables.GetRuntimeOperation(ctx, historical.OperationID)
	payload, _ := json.Marshal(historical)
	row["result"] = string(payload)
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, row))
	retired := historical
	retired.OperationID, retired.ApplicationID = "retired-historical-operation", "retired-application"
	retired.RequestHash = workspaceApplicationDeploymentRequestHash(retired)
	retiredRow := cloneMap(row)
	retiredRow["id"], retiredRow["operationId"] = retired.OperationID, retired.OperationID
	payload, _ = json.Marshal(retired)
	retiredRow["result"] = string(payload)
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, retiredRow))
	selected := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "selected", true)
	workspace, _, _ := app.tables.GetWorkspace(ctx, "ws-alpha")
	fabric := &applicationLifecycleFabric{workspaceDeleteFabric: fixture.fabric, states: map[string]string{}}
	service := newTestService(&fakeLedgerClient{}, fabric)
	inventory, err := app.workspaceApplicationLifecycleInventory(ctx, service, workspace, "suspended", "lifecycle-owner")
	if err != nil || len(inventory.Runtimes) != 2 || len(fabric.runtimeInputs) != 1 || fabric.runtimeInputs[0].RuntimeOperationID != historical.OperationID+":runtime" || fabric.runtimeInputs[0].SchemaVersion != 0 {
		t.Fatalf("historical inventory=%+v reads=%+v err=%v", inventory, fabric.runtimeInputs, err)
	}
	for _, target := range inventory.Runtimes {
		if target.Input.HistoricalApplicationRuntime && target.Input.RuntimeID != contracts.WorkspaceApplicationHistoricalRuntimeID("ws-alpha") {
			t.Fatal("historical physical identity was rewritten")
		}
	}
	// A valid command hash alone cannot identify the old runtime without the
	// selected predecessor chain. Refuse to choose another row by recency.
	selected.PreviousDeploymentID = ""
	selected.RequestHash = workspaceApplicationDeploymentRequestHash(selected)
	selectedRow, _, _ := app.tables.GetRuntimeOperation(ctx, selected.OperationID)
	payload, _ = json.Marshal(selected)
	selectedRow["result"] = string(payload)
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, selectedRow))
	if _, err := app.workspaceApplicationLifecycleInventory(ctx, service, workspace, "absent", "lifecycle-owner"); err == nil {
		t.Fatal("unproven historical lineage accepted")
	}
}

func TestApplicationDefaultPreparationAndDeleteClaimsAreMutuallyExclusive(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	app, ctx := handler.app, context.Background()
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceKeyGroupID: 7, Revision: defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64)), Phase: "credentials_required"}
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, row))
	request.Phase = "preparing_credentials"
	if err := app.persistWorkspaceDefaultApplication(ctx, row, request); err != nil {
		t.Fatal(err)
	}
	workspace, _, _ := app.tables.GetWorkspace(ctx, "ws-alpha")
	deletion, err := app.newWorkspaceDeleteOperation(ctx, handler.service, workspace, 41, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.tables.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(deletion)}); !errors.Is(err, errWorkspaceDeleteCASConflict) {
		t.Fatalf("delete crossed preparing claim: %v", err)
	}
	request.Phase = "credentials_required"
	if err := app.persistWorkspaceDefaultApplication(ctx, row, request); err != nil {
		t.Fatal(err)
	}
	if err := app.tables.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(deletion)}); err != nil {
		t.Fatal(err)
	}
	request.Phase = "preparing_credentials"
	if err := app.persistWorkspaceDefaultApplication(ctx, row, request); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
		t.Fatalf("prepare crossed delete claim: %v", err)
	}
	workspace, _, _ = app.tables.GetWorkspace(ctx, "ws-alpha")
	if workspace["state"] != "deleting" {
		t.Fatal("delete did not close application admission")
	}
}
