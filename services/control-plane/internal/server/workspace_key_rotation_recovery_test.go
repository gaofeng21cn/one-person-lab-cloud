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

func TestWorkspaceKeyRotationRecoveryHTTPThroughManagedApplication(t *testing.T) {
	// Drive the real worker explicitly so child completion and parent recovery
	// are separate observable boundaries, including across server restarts.
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
		preflightErr:               errors.New("injected_rotation_child_preflight_failure"),
	}
	fixture.service = controlplane.NewService(fixture.ledger, fabric, fixture.client)
	fixture.restart(t)
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

	app = fixture.server.(*controlPlaneHTTPHandler).app
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
	workspace, found, err = fixture.store.GetWorkspace(ctx, "ws-alpha")
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
