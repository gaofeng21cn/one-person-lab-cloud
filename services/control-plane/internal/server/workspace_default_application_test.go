package server

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/domain/application"
)

type applicationReplacementFabric struct {
	fakeFabricClient
	preflightErr error
	inputs       map[string]clients.WorkspaceApplicationRuntimeInput
	lifecycle    []contracts.WorkspaceApplicationRuntimeLifecycleInput
	states       map[string]string
}

func (f *applicationReplacementFabric) PreflightWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) error {
	if f.preflightErr != nil {
		return f.preflightErr
	}
	return f.fakeFabricClient.PreflightWorkspaceApplicationRuntime(ctx, input)
}

func (f *applicationReplacementFabric) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, key string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	if f.inputs == nil {
		f.inputs = map[string]clients.WorkspaceApplicationRuntimeInput{}
		f.states = map[string]string{}
	}
	result, err := f.fakeFabricClient.EnsureWorkspaceApplicationRuntime(ctx, input, key)
	if err == nil {
		f.inputs[input.RuntimeOperationID] = input
		f.states[input.RuntimeOperationID] = "ready"
	}
	return result, err
}
func (f *applicationReplacementFabric) ReadWorkspaceApplicationRuntime(_ context.Context, input clients.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	result := contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), Status: f.states[input.RuntimeOperationID], EntryURL: "https://" + input.Revision.ApplicationID + ".example.test/", Components: contracts.WorkspaceApplicationRuntimeComponents(input.Revision)}
	for i := range result.Components {
		result.Components[i].State = result.Status
	}
	return result, nil
}
func (f *applicationReplacementFabric) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput, _ string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	original, exists := f.inputs[input.RuntimeOperationID]
	if !exists || original.AccountID != input.AccountID || original.WorkspaceID != input.WorkspaceID || contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) != input.RuntimeID {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("test_runtime_identity_mismatch")
	}
	f.lifecycle = append(f.lifecycle, input)
	f.states[input.RuntimeOperationID] = input.DesiredState
	if input.DesiredState == "running" {
		f.states[input.RuntimeOperationID] = "ready"
	}
	return f.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
}
func (f *applicationReplacementFabric) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	observation, err := f.ReadWorkspaceApplicationRuntime(ctx, f.inputs[input.RuntimeOperationID])
	state := observation.Status
	if state == "ready" {
		state = "running"
	}
	return contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: state, Observation: observation}, err
}
func (f *applicationReplacementFabric) ReadWorkspaceApplicationRuntimeCredentials(context.Context, contracts.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	return contracts.WorkspaceApplicationRuntimeCredentials{}, errors.New("not_requested")
}

func admitApplicationForReplacement(t *testing.T, store controlPlaneTableStore, revision contracts.WorkspaceApplicationRevision) {
	t.Helper()
	digest, err := application.RevisionDigest(revision)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(revision)
	_, err = store.ApplyApplicationRevisionAdmission(context.Background(), applicationRevisionMutation{ApplicationID: revision.ApplicationID, Version: revision.Version, Digest: digest, Payload: string(payload), AdmittedByUserID: "operator-test"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultApplicationThenReplacementPreservesPurchaseAndIsolatesData(t *testing.T) {
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
	original, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	purchase, _, _ := store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
	revision := defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("b", 64))
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: "owner-alpha", Sub2APIUserID: 41, WorkspaceKeyGroupID: 5, Revision: revision, Phase: "waiting_resources", WorkspaceAPIKeyID: 19, GatewaySecret: &clients.GatewaySecretWriteResult{SecretRef: "secret-ws-alpha", Version: "v1", Fingerprint: "sha256:" + strings.Repeat("a", 64)}}
	requestRow, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, requestRow))
	for range 6 {
		if err := app.runWorkspaceDefaultApplicationsOnce(ctx, service); err != nil {
			t.Fatal(err)
		}
	}
	row, _, _ := store.GetRuntimeOperation(ctx, request.OperationID)
	installed, err := decodeWorkspaceDefaultApplication(row)
	if err != nil || installed.Phase != "deployed" {
		t.Fatalf("default installation=%+v err=%v", installed, err)
	}
	workspace, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	current, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found {
		t.Fatalf("selection found=%v err=%v", found, err)
	}
	if current.OperationID != installed.DeploymentID || current.WorkspaceAPIKeyID != 19 || current.Configuration.Environment["OPL_WEBUI_AUTH_MODE"] != "password" || current.Configuration.Environment["OPL_WEBUI_USERNAME"] != "opl" || len(current.SecretBindings) != 1 {
		t.Fatalf("default configuration not bound: %+v", current)
	}
	dataID := current.DataBindingID
	if current.Configuration.CredentialVersion == "" || current.Configuration.CredentialSourceRuntimeOperationID != "" {
		t.Fatal("fresh installation did not establish its own credential identity")
	}
	// Same application upgrade retains data and excludes simultaneous writers.
	upgraded := revision
	upgraded.Version = "upgrade"
	upgraded.Image = "repo.example/opl-app@sha256:" + strings.Repeat("c", 64)
	admitApplicationForReplacement(t, store, upgraded)
	successor, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "upgrade-default", "opl-app", "upgrade", current.Configuration, current.SecretBindings, 19, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if successor.PreviousDeploymentID != current.OperationID || successor.DataBindingID != dataID {
		t.Fatal("upgrade lost predecessor or stable data")
	}
	if successor.Configuration.CredentialVersion != current.Configuration.CredentialVersion || successor.Configuration.CredentialSourceRuntimeOperationID != current.Configuration.CredentialSourceRuntimeOperationID {
		t.Fatal("image upgrade changed credential identity")
	}
	runDeploymentWorkerToCompletion(t, app, service, successor.OperationID)
	if len(fabric.lifecycle) != 2 || fabric.lifecycle[0].DesiredState != "suspended" || fabric.lifecycle[1].DesiredState != "absent" || fabric.lifecycle[1].RuntimeOperationID != current.OperationID+":runtime" {
		t.Fatalf("wrong predecessor lifecycle: %+v", fabric.lifecycle)
	}
	// A different app receives a separate namespace and no OPL credentials.
	other := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "agent", Version: "1", Platform: "linux/amd64", Image: "repo.example/agent@sha256:" + strings.Repeat("d", 64), PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}}, ExposurePolicy: "cloud_private"}
	admitApplicationForReplacement(t, store, other)
	replacement, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "replace-agent", "agent", "1", contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if replacement.DataBindingID == dataID || len(replacement.SecretBindings) != 0 {
		t.Fatal("unrelated application inherited OPL data or credentials")
	}
	runDeploymentWorkerToCompletion(t, app, service, replacement.OperationID)
	workspace, _, _ = store.GetWorkspace(ctx, "ws-alpha")
	for _, field := range []string{"storageId", "currentComputeAllocationId", "currentAttachmentId", "paidThrough", "periodStart", "purchaseReceiptId"} {
		if workspace[field] != original[field] {
			t.Fatalf("purchase/resource field %s changed", field)
		}
	}
	retained, _, _ := store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
	if retained["result"] != purchase["result"] {
		t.Fatal("application replacement rewrote purchase")
	}
	if stringValue(workspace["currentApplicationDeploymentId"]) != replacement.OperationID {
		t.Fatal("replacement was not selected")
	}
	// Replaying the accepted request after activation must retain its original predecessor.
	replay, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "replace-agent", "agent", "1", contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, "", "")
	if err != nil || replay.OperationID != replacement.OperationID || replay.PreviousDeploymentID != replacement.PreviousDeploymentID {
		t.Fatalf("replay changed accepted command: %+v %v", replay, err)
	}
	// Returning to OPL must reuse its previous data binding, not the Agent's.
	reinstalled, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "return-opl", "opl-app", "upgrade", current.Configuration, current.SecretBindings, 19, "", "")
	if err != nil || reinstalled.DataBindingID != dataID {
		t.Fatalf("reinstall lost data: %+v %v", reinstalled, err)
	}
	// A failed preflight leaves the currently selected Agent running.
	fabric.preflightErr = errors.New("workspace_application_secret_binding_missing")
	beforeLifecycle := len(fabric.lifecycle)
	if err := app.runWorkspaceApplicationDeployment(ctx, service, reinstalled.OperationID); err != nil {
		t.Fatal(err)
	}
	if len(fabric.lifecycle) != beforeLifecycle || fabric.states[replacement.OperationID+":runtime"] != "ready" {
		t.Fatal("preflight failure stopped predecessor")
	}
	failedRow, _, _ := store.GetRuntimeOperation(ctx, reinstalled.OperationID)
	failed, err := decodeWorkspaceApplicationDeploymentIntent(failedRow)
	if err != nil || failed.Phase != workspaceApplicationDeploymentManualReviewPhase {
		t.Fatalf("preflight failure not recorded: %+v %v", failed, err)
	}

}

func TestApplicationCredentialConfigurationFollowsSelectedLineage(t *testing.T) {
	ctx := context.Background()
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	service := fixture.server.(*controlPlaneHTTPHandler).service
	revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
	bindings := []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: "opl-gateway-ws-alpha", Version: "gateway-original"}}
	initial := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, contracts.WorkspaceApplicationRuntimeConfiguration{Environment: map[string]string{"OPL_WEBUI_USERNAME": "opl"}}, bindings, 19, true)
	revision.Version = "rotated"
	configuration := initial.Configuration
	configuration.CredentialVersion = "explicit-credential-version"
	bindings[0].Version = "gateway-rotated"
	rotated := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, configuration, bindings, 20, true)
	selected := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "agent", true)
	// An unrelated newer intent is not evidence that its credentials served.
	detached := rotated
	detached.OperationID = "detached-opl-candidate"
	detached.Configuration.CredentialVersion = "never-selected"
	detached.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(detached.Configuration, detached.SecretBindings, detached.DataBindingID)
	detached.Phase, detached.ActivationAt, detached.ReceiptID = workspaceApplicationDeploymentManualReviewPhase, "", ""
	detached.CreatedAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	detached.RequestHash = workspaceApplicationDeploymentRequestHash(detached)
	row, _, _ := app.tables.GetRuntimeOperation(ctx, rotated.OperationID)
	row["id"], row["operationId"] = detached.OperationID, detached.OperationID
	row["status"] = "manual_review"
	payload, _ := json.Marshal(detached)
	row["result"] = string(payload)
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, row))
	resolved, resolvedBindings, keyID, err := app.workspaceOPLApplicationConfiguration(ctx, service, "ws-alpha", revision.ApplicationID)
	if err != nil || !reflect.DeepEqual(resolved, rotated.Configuration) || !reflect.DeepEqual(resolvedBindings, rotated.SecretBindings) || keyID != 20 {
		t.Fatalf("return to OPL lost selected credentials: configuration=%+v bindings=%+v key=%d err=%v", resolved, resolvedBindings, keyID, err)
	}
	returned, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "return-after-rotation", revision.ApplicationID, revision.Version, resolved, resolvedBindings, keyID, "", "")
	if err != nil || returned.Configuration.CredentialVersion != rotated.Configuration.CredentialVersion || returned.DataBindingID != initial.DataBindingID {
		t.Fatalf("return to OPL changed credentials or data: %+v %v", returned, err)
	}
	// A broken chain cannot be repaired by timestamps or the default request.
	row, _, _ = app.tables.GetRuntimeOperation(ctx, selected.OperationID)
	selected.PreviousDeploymentID = "missing-selected-predecessor"
	selected.RequestHash = workspaceApplicationDeploymentRequestHash(selected)
	payload, _ = json.Marshal(selected)
	row["result"] = string(payload)
	mustStore(t, app.tables.SaveRuntimeOperation(ctx, row))
	if _, _, _, err := app.workspaceOPLApplicationConfiguration(ctx, service, "ws-alpha", revision.ApplicationID); err == nil {
		t.Fatal("broken selected lineage recovered unproven credentials")
	}
}

func TestApplicationCredentialMigrationRetainsLatestFullRuntimeVersion(t *testing.T) {
	ctx := context.Background()
	for _, switchFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct migration", true: "return after another application"}[switchFirst], func(t *testing.T) {
			fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			workspace, _, _ := app.tables.GetWorkspace(ctx, "ws-alpha")
			access := cloneMap(mapField(workspace, "access"))
			access["credentialVersion"] = "full-runtime-explicitly-rotated"
			workspace["access"] = access
			mustStore(t, app.tables.SaveWorkspace(ctx, workspace))
			purchase, _, _ := app.tables.GetRuntimeOperation(ctx, "workspace-launch-alpha")
			if switchFirst {
				seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "agent", true)
			}
			revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
			admitApplicationForReplacement(t, app.tables, revision)
			bindings := []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: "opl-gateway-ws-alpha", Version: "v1"}}
			intent, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "full-migration", revision.ApplicationID, revision.Version, contracts.WorkspaceApplicationRuntimeConfiguration{}, bindings, 19, "", "")
			if err != nil || intent.Configuration.CredentialVersion != "full-runtime-explicitly-rotated" || intent.Configuration.CredentialSourceRuntimeOperationID != "workspace-launch-alpha:runtime" {
				t.Fatalf("full migration changed the current credential: %+v %v", intent, err)
			}
			input, err := app.workspaceApplicationRuntimeInput(ctx, intent)
			if err != nil || !reflect.DeepEqual(input.Configuration, intent.Configuration) {
				t.Fatalf("Fabric did not receive the bound credential identity: %+v %v", input, err)
			}
			replay, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "full-migration", revision.ApplicationID, revision.Version, contracts.WorkspaceApplicationRuntimeConfiguration{}, bindings, 19, "", "")
			if err != nil || !reflect.DeepEqual(replay, intent) {
				t.Fatalf("migration replay changed accepted identity: %+v %v", replay, err)
			}
			retained, _, _ := app.tables.GetRuntimeOperation(ctx, "workspace-launch-alpha")
			if retained["result"] != purchase["result"] {
				t.Fatal("credential migration rewrote purchase evidence")
			}
		})
	}
}

func TestApplicationReservationAndSuspensionFence(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, id := applicationDeploymentWorkerFixture(t, store, service)
	ctx := context.Background()
	_, err := handler.app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "concurrent", "knowledge-app", "1.0.0", contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, "", "")
	if !errors.Is(err, errWorkspaceApplicationIntentConflict) {
		t.Fatalf("concurrent command err=%v", err)
	}
	if err := handler.app.runWorkspaceApplicationDeployment(ctx, service, id); err != nil {
		t.Fatal(err)
	}
	workspace, _, _ := store.GetWorkspace(ctx, "ws-alpha")
	paidThrough, _ := time.Parse(time.RFC3339Nano, stringValue(workspace["paidThrough"]))
	if workspaceApplicationEntitlementOpen(workspace, paidThrough.Add(time.Second)) {
		t.Fatal("expired entitlement was admitted")
	}
	workspace["state"], workspace["status"] = "suspended", "suspended"
	mustStore(t, store.SaveWorkspace(ctx, workspace))
	if err := handler.app.runWorkspaceApplicationDeployment(ctx, service, id); err != nil {
		t.Fatal(err)
	}
	workspace, _, _ = store.GetWorkspace(ctx, "ws-alpha")
	if stringValue(workspace["currentApplicationDeploymentId"]) != "" {
		t.Fatal("expired entitlement activated")
	}
	row, _, _ := store.GetRuntimeOperation(ctx, id)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentManualReviewPhase {
		t.Fatalf("expired operation=%+v %v", intent, err)
	}
}

func TestHistoricalApplicationReinstallRestoresOriginalDataSource(t *testing.T) {
	ctx := context.Background()
	t.Run("full OPL to another app and back", func(t *testing.T) {
		fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
		app := fixture.server.(*controlPlaneHTTPHandler).app
		purchase, _, _ := fixture.store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
		agent := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "another-app", true)
		if agent.LegacyPredecessor == nil {
			t.Fatal("fixture lost historical predecessor")
		}
		revision := defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("b", 64))
		admitApplicationForReplacement(t, fixture.store, revision)
		bindings := []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: "opl-gateway-ws-alpha", Version: "v1", Key: "opl_gateway_api_key"}}
		reinstalled, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "return-historical-opl", revision.ApplicationID, revision.Version, contracts.WorkspaceApplicationRuntimeConfiguration{}, bindings, 19, "", "")
		if err != nil || reinstalled.DataLayout != "legacy_opl" || reinstalled.DataSourceRuntimeOperationID != "workspace-launch-alpha:runtime" || reinstalled.PreviousDeploymentID != agent.OperationID {
			t.Fatalf("historical data lost: %+v %v", reinstalled, err)
		}
		retained, _, _ := fixture.store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
		if retained["result"] != purchase["result"] {
			t.Fatal("data resolution rewrote purchase")
		}
	})
	t.Run("generic v1 to another app and back", func(t *testing.T) {
		fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
		app := fixture.server.(*controlPlaneHTTPHandler).app
		original := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "original-app", true)
		row, _, _ := fixture.store.GetRuntimeOperation(ctx, original.OperationID)
		original.Version = 1
		original.Configuration, original.SecretBindings = contracts.WorkspaceApplicationRuntimeConfiguration{}, nil
		original.DataBindingID, original.DataLayout, original.DataSourceRuntimeOperationID = "", "", ""
		original.RequestHash = workspaceApplicationDeploymentRequestHash(original)
		payload, _ := json.Marshal(original)
		row["result"] = string(payload)
		mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
		agent := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "another-app", true)
		reinstalled, err := app.createWorkspaceApplicationDeploymentIntent(ctx, "ws-alpha", "return-historical-generic", original.ApplicationID, original.TargetRevision, contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, "", "")
		if err != nil || reinstalled.DataLayout != "legacy_application" || reinstalled.DataSourceRuntimeOperationID != original.OperationID+":runtime" || reinstalled.PreviousDeploymentID != agent.OperationID {
			t.Fatalf("historical generic data lost: %+v %v", reinstalled, err)
		}
	})
}
