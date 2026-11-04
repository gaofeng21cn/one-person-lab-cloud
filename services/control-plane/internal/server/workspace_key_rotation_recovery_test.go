package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

func TestWorkspaceKeyRotationRecoveryHTTPThroughManagedApplication(t *testing.T) {
	// Drive the real worker explicitly so child completion and parent recovery
	// are separate observable boundaries, including across server restarts.
	ctx := context.Background()
	fixture, fabric, selected := managedApplicationKeyRotationFixtureForTest(t)
	fabric.preflightErr = errors.New("injected_rotation_child_preflight_failure")
	const key = "recover-accepted-managed-key-rotation"
	fixture.accept(t, key)
	parentID := "workspace-key-rotate-" + stableID("ws-alpha", key)[:18]
	readOperation := func(id string) map[string]any {
		t.Helper()
		row, found, err := fixture.store.GetRuntimeOperation(ctx, id)
		if err != nil || !found {
			t.Fatalf("operation %s found=%v err=%v", id, found, err)
		}
		return row
	}
	accepted, err := decodeWorkspaceKeyRotation(readOperation(parentID))
	if err != nil {
		t.Fatal(err)
	}
	pending := fixture.rotate(t, key)
	if pending.Code != http.StatusConflict || !strings.Contains(pending.Body.String(), errWorkspaceKeyRotationInProgress.Error()) {
		t.Fatalf("accepted parent recovery status=%d body=%s", pending.Code, pending.Body.String())
	}
	parentRow := readOperation(parentID)
	parent, err := decodeWorkspaceKeyRotation(parentRow)
	if err != nil || parent.Phase != "runtime_bind" || parent.ApplicationDeploymentID == "" || parent.NewKeyID != 19 ||
		parent.RequestHash != accepted.RequestHash || !reflect.DeepEqual(parent.AuditEvent, accepted.AuditEvent) {
		t.Fatalf("original parent did not reserve its child: %+v err=%v", parent, err)
	}
	childID := parent.ApplicationDeploymentID
	child, err := decodeWorkspaceApplicationDeploymentIntent(readOperation(childID))
	if err != nil || child.OriginOperationID != parentID || child.PreviousDeploymentID != selected.OperationID || child.WorkspaceAPIKeyID != 19 ||
		len(child.SecretBindings) != 1 || child.SecretBindings[0].Version != parent.SecretVersion || child.SecretBindings[0].SecretRef != parent.SecretRef ||
		child.Configuration.CredentialVersion == selected.Configuration.CredentialVersion || child.Configuration.CredentialSourceRuntimeOperationID != "" {
		t.Fatalf("rotation child lost its accepted bindings: %+v err=%v", child, err)
	}
	if fixture.client.createWrites != 1 || fixture.client.deleteWrites != 0 || fixture.client.keys[9].Status != "disabled" || len(fixture.ledger.inputs) != 0 {
		t.Fatal("pending child duplicated a Key or retired the old Key before activation")
	}

	app := fixture.server.(*controlPlaneHTTPHandler).app
	mustStore(t, app.runWorkspaceApplicationDeploymentsOnce(ctx, fixture.service))
	failed, err := decodeWorkspaceApplicationDeploymentIntent(readOperation(childID))
	if err != nil || failed.Phase != workspaceApplicationDeploymentManualReviewPhase || failed.FailurePhase != workspaceApplicationDeploymentIntentPhase || failed.LastError != fabric.preflightErr.Error() {
		t.Fatalf("worker did not retain its failed child: %+v err=%v", failed, err)
	}
	fixture.restart(t)
	if replay := fixture.rotate(t, key); replay.Code != http.StatusConflict {
		t.Fatalf("pending parent replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	if readOperation(parentID)["result"] != parentRow["result"] || fixture.client.createWrites != 1 {
		t.Fatal("restart replay replaced the accepted parent or its Key")
	}
	fabric.preflightErr = nil
	retry := requestWithMutationKeyForTest(t, fixture.server, operatorSessionForTest(t, fixture.server), http.MethodPost,
		"/api/operator/application-deployments/"+childID+"/retry", `{}`, "retry-accepted-rotation-child")
	var resumed struct {
		Intent workspaceApplicationDeploymentIntent `json:"intent"`
	}
	if retry.Code != http.StatusAccepted || json.Unmarshal(retry.Body.Bytes(), &resumed) != nil ||
		resumed.Intent.OperationID != childID || resumed.Intent.RequestHash != child.RequestHash || resumed.Intent.Phase != child.Phase {
		t.Fatalf("operator retry did not resume the frozen child: status=%d body=%s", retry.Code, retry.Body.String())
	}
	app = fixture.server.(*controlPlaneHTTPHandler).app
	runDeploymentWorkerToCompletion(t, app, fixture.service, childID)
	childRow := readOperation(childID)
	completedChild, err := decodeWorkspaceApplicationDeploymentIntent(childRow)
	if err != nil || stringValue(childRow["status"]) != "succeeded" || completedChild.Phase != workspaceApplicationDeploymentActivePhase || completedChild.RequestHash != child.RequestHash {
		t.Fatalf("original child did not complete: %+v err=%v", completedChild, err)
	}
	workspace, found, err := fixture.store.GetWorkspace(ctx, "ws-alpha")
	if err != nil || !found || stringValue(workspace["currentApplicationDeploymentId"]) != childID ||
		int64(numberField(workspace, "applicationBindingVersion", 0)) != child.ExpectedWorkspaceVersion+1 || int64(numberField(workspace, "workspaceApiKeyId", 0)) != 19 {
		t.Fatalf("child activation did not commit the selected Key: Workspace=%+v err=%v", workspace, err)
	}
	// Activation is not parent completion: the disabled original Key and the
	// original parent must survive until the caller resumes the parent HTTP.
	oldKey, oldPresent := fixture.client.keys[9]
	if !oldPresent || oldKey.Status != "disabled" || oldKey.Name != "opl-workspace" || fixture.client.deleteWrites != 0 ||
		readOperation(parentID)["result"] != parentRow["result"] || stringValue(readOperation(parentID)["status"]) != "started" ||
		len(fixture.ledger.inputs) != 1 || fixture.ledger.inputs[0].Type != "workspace.application_deployed.v1" {
		t.Fatal("child completion prematurely retired the old Key or completed the parent")
	}
	if len(fabric.applicationRuntimeInputs) != 1 || fabric.applicationRuntimeInputs[0].RuntimeOperationID != childID+":runtime" ||
		!reflect.DeepEqual(fabric.applicationRuntimeInputs[0].SecretBindings, child.SecretBindings) || len(fabric.lifecycle.mutations) != 2 ||
		fabric.lifecycle.mutations[0].RuntimeOperationID != selected.OperationID+":runtime" || fabric.lifecycle.mutations[0].DesiredState != "suspended" ||
		fabric.lifecycle.mutations[1].RuntimeOperationID != selected.OperationID+":runtime" || fabric.lifecycle.mutations[1].DesiredState != "absent" || len(fixture.fabric.bindings) != 0 {
		t.Fatal("worker did not replace exactly the managed generation through Fabric")
	}

	// Exercise the parent's real Key CAS even though activation already selected
	// the replacement. A lost CAS response must retain the old Key for recovery.
	fixture.store.failCASAfterCommit = true
	interrupted := fixture.rotate(t, key)
	if interrupted.Code != http.StatusBadGateway || !fixture.store.casResponseLost || fixture.client.deleteWrites != 0 {
		t.Fatalf("parent CAS recovery boundary status=%d body=%s", interrupted.Code, interrupted.Body.String())
	}
	fixture.restart(t)
	complete := fixture.rotate(t, key)
	if complete.Code != http.StatusOK {
		t.Fatalf("parent recovery status=%d body=%s", complete.Code, complete.Body.String())
	}
	terminal, err := decodeWorkspaceKeyRotation(readOperation(parentID))
	if err != nil || terminal.Phase != "succeeded" || terminal.ApplicationDeploymentID != childID || terminal.RequestHash != accepted.RequestHash || stringValue(readOperation(parentID)["status"]) != "succeeded" {
		t.Fatalf("original parent did not complete: %+v err=%v", terminal, err)
	}
	rotationReceipt, rotationRecorded := fixture.ledger.receipts[parentID+":receipt"]
	deploymentReceipt, deploymentRecorded := fixture.ledger.receipts[childID+":deployment-receipt"]
	if !rotationRecorded || !deploymentRecorded || len(fixture.ledger.receipts) != 2 || len(fixture.ledger.inputs) != 2 ||
		rotationReceipt.Type != "workspace.gateway_key_rotated.v1" || rotationReceipt.ReceiptID != terminal.ReceiptID ||
		deploymentReceipt.Type != "workspace.application_deployed.v1" || deploymentReceipt.ReceiptID != completedChild.ReceiptID ||
		!strings.Contains(complete.Body.String(), terminal.ReceiptID) || !strings.Contains(complete.Body.String(), parentID) {
		t.Fatal("parent and child did not retain one receipt each")
	}
	if _, exists := fixture.client.keys[9]; exists || len(fixture.client.keys) != 1 || fixture.client.keys[19].Name != workspaceReservedKeyName("ws-alpha") || fixture.client.keys[19].Status != "active" ||
		fixture.client.createWrites != 1 || fixture.client.deleteWrites != 1 || !reflect.DeepEqual(fixture.client.updateStages, []string{"disable", "retire", "promote"}) {
		t.Fatal("parent did not retire and delete exactly the original Key")
	}
	operations, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parents, deployments := 0, 0
	for _, row := range operations {
		switch stringValue(row["action"]) {
		case "workspace.gateway_key.rotate":
			parents++
		case workspaceApplicationDeploymentAction:
			deployments++
		}
	}
	if parents != 1 || deployments != 2 { // The selected fixture generation plus its one rotation child.
		t.Fatalf("recovery created another operation: parents=%d deployments=%d", parents, deployments)
	}
	writes := []int{fixture.client.createWrites, fixture.client.updateWrites, fixture.client.deleteWrites,
		len(fixture.fabric.gatewaySecretInputs), len(fabric.applicationRuntimeInputs), len(fabric.lifecycle.mutations), len(fixture.ledger.inputs)}
	fixture.restart(t)
	replay := fixture.rotate(t, key)
	mustStore(t, fixture.server.(*controlPlaneHTTPHandler).app.runWorkspaceApplicationDeploymentsOnce(ctx, fixture.service))
	after, err := fixture.store.ListRuntimeOperations(ctx)
	replayedWrites := []int{fixture.client.createWrites, fixture.client.updateWrites, fixture.client.deleteWrites,
		len(fixture.fabric.gatewaySecretInputs), len(fabric.applicationRuntimeInputs), len(fabric.lifecycle.mutations), len(fixture.ledger.inputs)}
	if replay.Code != http.StatusOK || replay.Body.String() != complete.Body.String() || err != nil || !reflect.DeepEqual(operations, after) || !reflect.DeepEqual(writes, replayedWrites) || len(fixture.ledger.receipts) != 2 {
		t.Fatalf("restart/same-key replay changed operations or repeated effects: status=%d err=%v before=%v after=%v", replay.Code, err, writes, replayedWrites)
	}
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

func TestWorkspaceKeyRotationRecoveryReusesChildAfterLostClaimResponse(t *testing.T) {
	ctx := context.Background()
	for _, activateBeforeRecovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "before activation", true: "after selection advances"}[activateBeforeRecovery], func(t *testing.T) {
			fixture, fabric, selected := managedApplicationKeyRotationFixtureForTest(t)
			const key = "lost-child-claim-response"
			fixture.accept(t, key)
			parentID := "workspace-key-rotate-" + stableID("ws-alpha", key)[:18]
			childID := workspaceApplicationDeploymentOperationID("ws-alpha", "gateway-rotation:"+parentID)
			app := fixture.server.(*controlPlaneHTTPHandler).app
			store := &applicationChildClaimResponseLossStore{controlPlaneTableStore: fixture.store, failure: errors.New("application child claim response lost")}
			app.tables = store
			interrupted := fixture.rotate(t, key)
			if interrupted.Code != http.StatusBadGateway || store.claims != 1 {
				t.Fatalf("lost child claim status=%d claims=%d body=%s", interrupted.Code, store.claims, interrupted.Body.String())
			}
			parentRow, found, err := fixture.store.GetRuntimeOperation(ctx, parentID)
			if err != nil || !found {
				t.Fatalf("parent found=%v err=%v", found, err)
			}
			parent, err := decodeWorkspaceKeyRotation(parentRow)
			if err != nil || parent.Phase != "runtime_bind" || parent.ApplicationDeploymentID != "" {
				t.Fatalf("lost response attached an unacknowledged child: %+v err=%v", parent, err)
			}
			childRow, found, err := fixture.store.GetRuntimeOperation(ctx, childID)
			if err != nil || !found {
				t.Fatalf("committed child found=%v err=%v", found, err)
			}
			child, err := decodeWorkspaceApplicationDeploymentIntent(childRow)
			if err != nil || child.PreviousDeploymentID != selected.OperationID || child.OriginOperationID != parentID || child.ApplicationID != selected.ApplicationID || child.TargetRevision != selected.TargetRevision || child.DataBindingID != selected.DataBindingID || child.DataLayout != selected.DataLayout || child.DataSourceRuntimeOperationID != selected.DataSourceRuntimeOperationID {
				t.Fatalf("committed child lost the selected command: %+v err=%v", child, err)
			}
			if activateBeforeRecovery {
				runDeploymentWorkerToCompletion(t, app, fixture.service, childID)
				workspace, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
				if stringValue(workspace["currentApplicationDeploymentId"]) != childID || fixture.client.deleteWrites != 0 {
					t.Fatal("child did not activate independently of the unacknowledged parent")
				}
			}
			frozen, _, _ := fixture.store.GetRuntimeOperation(ctx, childID)
			operations, err := fixture.store.ListRuntimeOperations(ctx)
			if err != nil {
				t.Fatal(err)
			}
			writes := []int{fixture.client.createWrites, len(fixture.fabric.gatewaySecretInputs), len(fabric.applicationRuntimeInputs), len(fabric.lifecycle.mutations)}
			fixture.restart(t)
			app = fixture.server.(*controlPlaneHTTPHandler).app
			app.tables = store
			// Recover the child reservation before the parent acknowledges it. This
			// must read the original command even when selection/version advanced.
			recovered, err := app.createWorkspaceApplicationKeyRotationDeployment(ctx, "ws-alpha", parentID, parent)
			if err != nil {
				t.Fatal(err)
			}
			original, err := decodeWorkspaceApplicationDeploymentIntent(frozen)
			if err != nil || !reflect.DeepEqual(recovered, original) || recovered.RequestHash != child.RequestHash || store.claims != 1 {
				t.Fatalf("recovery replaced the frozen child: %+v err=%v claims=%d", recovered, err, store.claims)
			}
			after, err := fixture.store.ListRuntimeOperations(ctx)
			if err != nil || !reflect.DeepEqual(operations, after) {
				t.Fatalf("child recovery created or changed an operation: err=%v", err)
			}
			replayedWrites := []int{fixture.client.createWrites, len(fixture.fabric.gatewaySecretInputs), len(fabric.applicationRuntimeInputs), len(fabric.lifecycle.mutations)}
			if !reflect.DeepEqual(writes, replayedWrites) {
				t.Fatalf("recovering the frozen child repeated effects: before=%v after=%v", writes, replayedWrites)
			}
			response := fixture.rotate(t, key)
			if activateBeforeRecovery {
				if response.Code != http.StatusOK {
					t.Fatalf("activated child parent recovery=%d %s", response.Code, response.Body.String())
				}
			} else {
				if response.Code != http.StatusConflict {
					t.Fatalf("pending child parent recovery=%d %s", response.Code, response.Body.String())
				}
				runDeploymentWorkerToCompletion(t, app, fixture.service, childID)
				if response = fixture.rotate(t, key); response.Code != http.StatusOK {
					t.Fatalf("parent closeout=%d %s", response.Code, response.Body.String())
				}
			}
			if store.claims != 1 || fixture.client.createWrites != 1 || len(fixture.fabric.gatewaySecretInputs) != 1 || len(fabric.applicationRuntimeInputs) != 1 || len(fabric.lifecycle.mutations) != 2 {
				t.Fatalf("lost response duplicated Key/Secret/runtime effects: claims=%d before=%v", store.claims, writes)
			}
			parentRow, _, _ = fixture.store.GetRuntimeOperation(ctx, parentID)
			completed, err := decodeWorkspaceKeyRotation(parentRow)
			if err != nil || completed.ApplicationDeploymentID != childID || completed.Phase != "succeeded" {
				t.Fatalf("parent did not retain its original child: %+v err=%v", completed, err)
			}
		})
	}
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
