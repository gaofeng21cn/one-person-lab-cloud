package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
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
	result := contracts.WorkspaceApplicationRuntimeObservation{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, RuntimeID: contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID), Status: f.states[input.RuntimeOperationID], Entry: &contracts.WorkspaceApplicationEntry{URL: "https://" + input.Revision.ApplicationID + ".example.test/"}, Components: contracts.WorkspaceApplicationRuntimeComponents(input.Revision)}
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

func TestAcceptedDefaultApplicationReplacementPreservesPurchaseAndIsolatesData(t *testing.T) {
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
	current := seedHistoricalDefaultApplicationForTest(t, app, fabric, request)
	row, _, _ := store.GetRuntimeOperation(ctx, request.OperationID)
	installed, err := decodeWorkspaceDefaultApplication(row)
	if err != nil || installed.Phase != "deployed" {
		t.Fatalf("historical installation=%+v err=%v", installed, err)
	}
	workspace, _, _ := store.GetWorkspace(ctx, "ws-alpha")
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
	successor := applicationDeploymentFixtureForTest(t, app, "ws-alpha", "upgrade-default", upgraded)
	successor.Configuration, successor.SecretBindings, successor.WorkspaceAPIKeyID = current.Configuration, current.SecretBindings, current.WorkspaceAPIKeyID
	successor.DataBindingID, successor.DataLayout, successor.DataSourceRuntimeOperationID = current.DataBindingID, current.DataLayout, current.DataSourceRuntimeOperationID
	successor = seedAcceptedApplicationDeploymentForTest(t, app, successor)
	runDeploymentWorkerToCompletion(t, app, service, successor.OperationID)
	upgradedInput := fabric.inputs[successor.OperationID+":runtime"]
	if upgradedInput.DataBindingID != dataID || upgradedInput.Configuration.CredentialVersion != current.Configuration.CredentialVersion || upgradedInput.Configuration.CredentialSourceRuntimeOperationID != current.Configuration.CredentialSourceRuntimeOperationID {
		t.Fatalf("worker changed the accepted data or credential identity: %+v", upgradedInput)
	}
	if len(fabric.lifecycle) != 2 || fabric.lifecycle[0].DesiredState != "suspended" || fabric.lifecycle[1].DesiredState != "absent" || fabric.lifecycle[1].RuntimeOperationID != current.OperationID+":runtime" {
		t.Fatalf("wrong predecessor lifecycle: %+v", fabric.lifecycle)
	}
	// The accepted replacement specifies its own namespace and no OPL credentials.
	other := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "agent", Version: "1", Platform: "linux/amd64", Image: "repo.example/agent@sha256:" + strings.Repeat("d", 64), PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}}, ExposurePolicy: "cloud_private"}
	replacement := applicationDeploymentFixtureForTest(t, app, "ws-alpha", "replace-agent", other)
	replacement = seedAcceptedApplicationDeploymentForTest(t, app, replacement)
	runDeploymentWorkerToCompletion(t, app, service, replacement.OperationID)
	replacementInput := fabric.inputs[replacement.OperationID+":runtime"]
	if replacementInput.DataBindingID != replacement.DataBindingID || replacementInput.DataBindingID == dataID || len(replacementInput.SecretBindings) != 0 || replacementInput.Configuration.CredentialVersion != "" || replacementInput.Configuration.CredentialSourceRuntimeOperationID != "" {
		t.Fatalf("worker gave the replacement OPL data or credentials: %+v", replacementInput)
	}
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
	// An accepted historical return already names its original data and credentials.
	reinstalled := applicationDeploymentFixtureForTest(t, app, "ws-alpha", "return-opl", upgraded)
	reinstalled.Configuration, reinstalled.SecretBindings, reinstalled.WorkspaceAPIKeyID = current.Configuration, current.SecretBindings, current.WorkspaceAPIKeyID
	reinstalled.DataBindingID, reinstalled.DataLayout, reinstalled.DataSourceRuntimeOperationID = dataID, current.DataLayout, current.DataSourceRuntimeOperationID
	reinstalled = seedAcceptedApplicationDeploymentForTest(t, app, reinstalled)
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

func TestAcceptedApplicationDeploymentSuspensionFence(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &fakeFabricClient{}
	service := newTestService(&fakeLedgerClient{}, fabric)
	handler, id := applicationDeploymentWorkerFixture(t, store, service)
	ctx := context.Background()
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

// Frozen historical data/credential fields must still reach the real Runtime
// input reader after the general reinstall admission path has been removed.
func TestAcceptedHistoricalApplicationRuntimeInputRetainsDataSource(t *testing.T) {
	ctx := context.Background()
	for _, layout := range []string{"legacy_opl", "legacy_application"} {
		t.Run(layout, func(t *testing.T) {
			fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			purchase, _, _ := fixture.store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
			revision := knowledgeApplicationRevisionForTest()
			configuration := contracts.WorkspaceApplicationRuntimeConfiguration{}
			var bindings []contracts.WorkspaceApplicationRuntimeSecretBinding
			source := "workspace-launch-alpha:runtime"
			if layout == "legacy_application" {
				original := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision, configuration, nil, 0, true)
				row, _, _ := fixture.store.GetRuntimeOperation(ctx, original.OperationID)
				original.Version = 1
				original.Configuration, original.SecretBindings = contracts.WorkspaceApplicationRuntimeConfiguration{}, nil
				original.DataBindingID, original.DataLayout, original.DataSourceRuntimeOperationID = "", "", ""
				original.RequestHash = workspaceApplicationDeploymentRequestHash(original)
				payload, err := json.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				row["result"] = string(payload)
				mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
				source = original.OperationID + ":runtime"
			}
			if layout == "legacy_opl" {
				revision = defaultOPLApplicationRevision("repo.example/opl-app@sha256:" + strings.Repeat("b", 64))
				source = "workspace-launch-alpha:runtime"
				configuration = contracts.WorkspaceApplicationRuntimeConfiguration{CredentialVersion: "full-runtime-explicitly-rotated", CredentialSourceRuntimeOperationID: source}
				bindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: "opl-gateway-ws-alpha", Version: "v1", Key: "opl_gateway_api_key"}}
			}
			seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "another-app", true)
			intent := applicationDeploymentFixtureForTest(t, app, "ws-alpha", "accepted-historical-return", revision)
			intent.Configuration, intent.SecretBindings = configuration, bindings
			intent.DataLayout, intent.DataSourceRuntimeOperationID = layout, source
			intent = seedAcceptedApplicationDeploymentForTest(t, app, intent)
			restarted, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			row, found, err := fixture.store.GetRuntimeOperation(ctx, intent.OperationID)
			if err != nil || !found {
				t.Fatalf("accepted history found=%v err=%v", found, err)
			}
			retained, err := decodeWorkspaceApplicationDeploymentIntent(row)
			if err != nil {
				t.Fatal(err)
			}
			input, err := restarted.(*controlPlaneHTTPHandler).app.workspaceApplicationRuntimeInput(ctx, retained)
			if err != nil || input.DataLayout != layout || input.DataSourceRuntimeOperationID != source || input.DataBindingID != intent.DataBindingID || input.Configuration.CredentialVersion != configuration.CredentialVersion || input.Configuration.CredentialSourceRuntimeOperationID != configuration.CredentialSourceRuntimeOperationID {
				t.Fatalf("historical runtime input lost accepted lineage: %+v err=%v", input, err)
			}
			after, _, _ := fixture.store.GetRuntimeOperation(ctx, "workspace-launch-alpha")
			if after["result"] != purchase["result"] {
				t.Fatal("historical runtime read rewrote purchase")
			}
		})
	}
}
