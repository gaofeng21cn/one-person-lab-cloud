package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// Retain the rotation fixture's Gateway/Secret effects while modelling managed
// application generations through the existing lifecycle fixture.
type workspaceKeyRotationRecoveryFabric struct {
	*workspaceKeyRotationFabric
	lifecycle    *applicationLifecycleFabric
	preflightErr error
}

func (f *workspaceKeyRotationRecoveryFabric) PreflightWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) error {
	if f.preflightErr != nil {
		return f.preflightErr
	}
	return f.fakeFabricClient.PreflightWorkspaceApplicationRuntime(ctx, input)
}

func (f *workspaceKeyRotationRecoveryFabric) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, key string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	observation, err := f.fakeFabricClient.EnsureWorkspaceApplicationRuntime(ctx, input, key)
	observation.Entry = applicationGatewayEntry(input.Revision)
	return observation, err
}

func (f *workspaceKeyRotationRecoveryFabric) ReadWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	return f.lifecycle.ReadWorkspaceApplicationRuntime(ctx, input)
}

func (f *workspaceKeyRotationRecoveryFabric) SetWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput, key string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	return f.lifecycle.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, key)
}

func (f *workspaceKeyRotationRecoveryFabric) ReadWorkspaceApplicationRuntimeLifecycle(ctx context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	return f.lifecycle.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
}

func (f *workspaceKeyRotationRecoveryFabric) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input clients.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	return f.lifecycle.ReadWorkspaceApplicationRuntimeCredentials(ctx, input)
}

func managedApplicationKeyRotationFixtureForTest(t *testing.T) (workspaceKeyRotationFixture, *workspaceKeyRotationRecoveryFabric, workspaceApplicationDeploymentIntent) {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "false")
	ctx := context.Background()
	fixture := newWorkspaceKeyRotationFixture(t, "")
	app := fixture.server.(*controlPlaneHTTPHandler).app
	workspace, found, err := fixture.store.GetWorkspace(ctx, "ws-alpha")
	if err != nil || !found {
		t.Fatalf("initial Workspace found=%v err=%v", found, err)
	}
	// Replace only the fixture placeholder before seeding its canonical purchase.
	delete(fixture.store.workspaces, "ws-alpha")
	seedCanonicalRuntimeAccessWorkspaceForTest(t, fixture.store.memoryTableStore, stringValue(workspace["ownerUserId"]))
	// Revision seed helpers require the concrete memory store, not the fault-injection wrapper.
	app.tables = fixture.store.memoryTableStore
	revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
	selected := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision,
		contracts.WorkspaceApplicationRuntimeConfiguration{},
		[]contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: "opl-gateway-ws-alpha", Version: "v1"}}, 9, true)
	app.tables = fixture.store
	fabric := &workspaceKeyRotationRecoveryFabric{
		workspaceKeyRotationFabric: fixture.fabric,
		lifecycle:                  &applicationLifecycleFabric{workspaceDeleteFabric: &workspaceDeleteFabric{}, states: map[string]string{}},
	}
	fixture.service = controlplane.NewService(fixture.ledger, fabric, fixture.client)
	fixture.restart(t)
	return fixture, fabric, selected
}

func TestWorkspaceApplicationKeyRotationChildRejectsUnacceptedParent(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"missing", "foreign account", "foreign workspace", "wrong phase", "different Key", "different Secret"} {
		t.Run(name, func(t *testing.T) {
			fixture, fabric, selected := managedApplicationKeyRotationFixtureForTest(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			const key = "child-parent-validation"
			fixture.accept(t, key)
			parentID := "workspace-key-rotate-" + stableID("ws-alpha", key)[:18]
			row, _, _ := fixture.store.GetRuntimeOperation(ctx, parentID)
			parent, err := decodeWorkspaceKeyRotation(row)
			if err != nil {
				t.Fatal(err)
			}
			parent.Phase, parent.NewKeyID, parent.SecretRef, parent.SecretVersion = "runtime_bind", 19, "opl-gateway-ws-alpha", "v2"
			row = workspaceKeyRotationRow(parentID, selected.AccountID, selected.WorkspaceID, "started", parent)
			requestedID, requested := parentID, parent
			switch name {
			case "missing":
				requestedID = "never-accepted-rotation"
			case "foreign account":
				row["accountId"] = "acct-foreign"
			case "foreign workspace":
				row["workspaceId"] = "ws-foreign"
			case "wrong phase":
				parent.Phase = "replacement_check"
				row["result"] = encodeWorkspaceKeyRotation(parent)
			case "different Key":
				requested.NewKeyID = 20
			case "different Secret":
				requested.SecretVersion = "unaccepted-secret-version"
			}
			mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
			before, err := fixture.store.ListRuntimeOperations(ctx)
			if err != nil {
				t.Fatal(err)
			}
			workspaceBefore, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
			if _, err := app.createWorkspaceApplicationKeyRotationDeployment(ctx, "ws-alpha", requestedID, requested); err == nil {
				t.Fatal("unaccepted parent created an application child")
			}
			after, err := fixture.store.ListRuntimeOperations(ctx)
			workspaceAfter, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(workspaceBefore, workspaceAfter) {
				t.Fatalf("rejected parent changed operations or Workspace: err=%v", err)
			}
			assertWorkspaceKeyRotationNoExternalWrites(t, fixture)
			if len(fabric.applicationRuntimeInputs) != 0 || len(fabric.lifecycle.mutations) != 0 {
				t.Fatal("rejected parent changed an application runtime")
			}
		})
	}
}

type applicationChildClaimResponseLossStore struct {
	controlPlaneTableStore
	failure error
	claims  int
}

func (s *applicationChildClaimResponseLossStore) ClaimWorkspaceApplicationDeploymentIntent(ctx context.Context, row map[string]any) error {
	s.claims++
	if err := s.controlPlaneTableStore.ClaimWorkspaceApplicationDeploymentIntent(ctx, row); err != nil {
		return err
	}
	return s.failure
}

func TestWorkspaceApplicationKeyRotationChildInheritsHistoricalV1Data(t *testing.T) {
	ctx := context.Background()
	for _, fullLaunch := range []bool{false, true} {
		t.Run(map[bool]string{false: "historical generic runtime", true: "historical full Runtime"}[fullLaunch], func(t *testing.T) {
			t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "false")
			var fixture workspaceDeleteFixture
			if fullLaunch {
				fixture, _, _ = newWorkspaceDeleteCompletionFixture(t)
			} else {
				fixture, _, _, _ = newResourceOnlyWorkspaceLifecycleFixture(t)
				workspace, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
				workspace["workspaceApiKeyId"] = int64(19)
				mustStore(t, fixture.store.SaveWorkspace(ctx, workspace))
			}
			app := fixture.server.(*controlPlaneHTTPHandler).app
			revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
			selected := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision,
				contracts.WorkspaceApplicationRuntimeConfiguration{},
				[]contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: "opl_gateway_api_key", SecretRef: "opl-gateway-ws-alpha", Version: "v1"}}, 19, true)
			selected.Version = 1
			selected.DataBindingID, selected.DataLayout, selected.DataSourceRuntimeOperationID = "", "", ""
			selected.DataBindingIDs = nil
			selected.RequestHash = workspaceApplicationDeploymentRequestHash(selected)
			row, _, _ := fixture.store.GetRuntimeOperation(ctx, selected.OperationID)
			row["result"] = string(mustJSON(selected))
			mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
			parentStore := fixture.store
			if events, ok := parentStore.(workspaceDeleteEventStore); ok {
				parentStore = events.controlPlaneTableStore
			}
			parentFixture := workspaceKeyRotationFixture{store: &workspaceKeyRotationStore{memoryTableStore: parentStore.(*memoryTableStore)}}
			const key = "accepted-v1-application-rotation"
			parentFixture.accept(t, key)
			parentID := "workspace-key-rotate-" + stableID("ws-alpha", key)[:18]
			parentRow, _, _ := fixture.store.GetRuntimeOperation(ctx, parentID)
			parent, err := decodeWorkspaceKeyRotation(parentRow)
			if err != nil {
				t.Fatal(err)
			}
			parent.Phase, parent.NewKeyID, parent.SecretRef, parent.SecretVersion = "runtime_bind", 20, "opl-gateway-ws-alpha", "v2"
			parentRow["result"] = encodeWorkspaceKeyRotation(parent)
			mustStore(t, fixture.store.SaveRuntimeOperation(ctx, parentRow))
			child, err := app.createWorkspaceApplicationKeyRotationDeployment(ctx, "ws-alpha", parentID, parent)
			layout, source := "legacy_application", selected.OperationID+":runtime"
			if fullLaunch {
				layout, source = "legacy_opl", "workspace-launch-alpha:runtime"
			}
			if err != nil || child.Version != 2 || child.PreviousDeploymentID != selected.OperationID || child.ApplicationID != selected.ApplicationID || child.TargetRevision != selected.TargetRevision || child.DataBindingID == "" || child.DataLayout != layout || child.DataSourceRuntimeOperationID != source {
				t.Fatalf("Key rotation lost historical data ownership: %+v err=%v", child, err)
			}
			input, err := app.workspaceApplicationRuntimeInput(ctx, child)
			if err != nil || input.SchemaVersion != 2 || input.DataLayout != layout || input.DataSourceRuntimeOperationID != source || input.Configuration.CredentialSourceRuntimeOperationID != "" || input.SecretBindings[0].Version != "v2" {
				t.Fatalf("historical data did not reach the rotation Runtime input: %+v err=%v", input, err)
			}
			retained, _, _ := fixture.store.GetRuntimeOperation(ctx, selected.OperationID)
			if !reflect.DeepEqual(row, retained) {
				t.Fatal("v1 conversion rewrote the original accepted deployment")
			}
		})
	}
}

func TestWorkspaceApplicationKeyRotationChildRejectsOwnedDataConflict(t *testing.T) {
	ctx := context.Background()
	for _, field := range []string{"binding", "layout", "source"} {
		t.Run(field, func(t *testing.T) {
			fixture, fabric, selected := managedApplicationKeyRotationFixtureForTest(t)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			// A retained generation for the same application must not disagree
			// with the selected generation about the durable data namespace.
			conflict := selected
			conflict.OperationID = "conflicting-owned-deployment"
			switch field {
			case "binding":
				conflict.DataBindingID = "different-owned-data"
				conflict.DataBindingIDs = []string{conflict.DataBindingID}
			case "layout":
				conflict.DataLayout = "legacy_application"
			case "source":
				conflict.DataSourceRuntimeOperationID = "different-owned-runtime:runtime"
			}
			var err error
			conflict.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(conflict.Configuration, conflict.SecretBindings, conflict.DataBindingID)
			if err != nil {
				t.Fatal(err)
			}
			conflict.RequestHash = workspaceApplicationDeploymentRequestHash(conflict)
			row, _, _ := fixture.store.GetRuntimeOperation(ctx, selected.OperationID)
			row["id"], row["operationId"], row["result"] = conflict.OperationID, conflict.OperationID, string(mustJSON(conflict))
			mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
			const key = "owned-application-data-conflict"
			fixture.accept(t, key)
			parentID := "workspace-key-rotate-" + stableID("ws-alpha", key)[:18]
			parentRow, _, _ := fixture.store.GetRuntimeOperation(ctx, parentID)
			parent, err := decodeWorkspaceKeyRotation(parentRow)
			if err != nil {
				t.Fatal(err)
			}
			parent.Phase, parent.NewKeyID, parent.SecretRef, parent.SecretVersion = "runtime_bind", 19, "opl-gateway-ws-alpha", "v2"
			parentRow["result"] = encodeWorkspaceKeyRotation(parent)
			mustStore(t, fixture.store.SaveRuntimeOperation(ctx, parentRow))
			before, err := fixture.store.ListRuntimeOperations(ctx)
			if err != nil {
				t.Fatal(err)
			}
			workspaceBefore, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
			_, err = app.createWorkspaceApplicationKeyRotationDeployment(ctx, "ws-alpha", parentID, parent)
			if err == nil || err.Error() != "workspace_application_data_binding_conflict" {
				t.Fatalf("owned data conflict error=%v", err)
			}
			after, err := fixture.store.ListRuntimeOperations(ctx)
			workspaceAfter, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(workspaceBefore, workspaceAfter) {
				t.Fatalf("conflicting data admitted a child or changed selection: err=%v", err)
			}
			assertWorkspaceKeyRotationNoExternalWrites(t, fixture)
			if len(fabric.applicationRuntimeInputs) != 0 || len(fabric.lifecycle.mutations) != 0 {
				t.Fatal("owned data conflict changed an application runtime")
			}
		})
	}
}
