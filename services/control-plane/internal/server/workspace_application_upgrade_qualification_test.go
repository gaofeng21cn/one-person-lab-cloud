package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// An in-place upgrade from public v0.1.8 keeps the installation's database.
// v0.1.8 could replay an accepted Workspace Key rotation and persist a pending
// `workspace.application.deploy` child for the selected application
// generation. The rotation constructor and its rotation recovery are retired,
// so the only way such a row exists today is an older release having written
// it. These fixtures reproduce the persisted fields used by the retained
// worker and drive it over that state.
const (
	upgradeRotationOldKeyID  = int64(19)
	upgradeRotationNewKeyID  = int64(27)
	upgradeRotationSecretRef = "opl-gateway-ws-alpha"
	upgradeRotationSecretVer = "v2"
	upgradeRotationKey       = "accepted-v018-workspace-key-rotation"
)

// upgradeProviderFabric models the provider state a retained installation
// carries into an in-place upgrade: the selected generation's runtime already
// exists, and every lifecycle mutation is recorded with the idempotency key
// the worker used for it.
type upgradeProviderFabric struct {
	fakeFabricClient
	states    map[string]string
	mutations []contracts.WorkspaceApplicationRuntimeLifecycleInput
	keys      []string
}

func (f *upgradeProviderFabric) SetWorkspaceApplicationRuntimeLifecycle(_ context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput, key string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	if f.states[input.RuntimeOperationID] == "" {
		return contracts.WorkspaceApplicationRuntimeLifecycleResult{}, errors.New("upgrade_runtime_identity_unknown")
	}
	f.mutations = append(f.mutations, input)
	f.keys = append(f.keys, key)
	f.states[input.RuntimeOperationID] = input.DesiredState
	return contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: input.DesiredState}, nil
}

func (f *upgradeProviderFabric) ReadWorkspaceApplicationRuntimeLifecycle(_ context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	return contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID, State: f.states[input.RuntimeOperationID]}, nil
}

func (f *upgradeProviderFabric) ReadWorkspaceApplicationRuntimeCredentials(_ context.Context, input contracts.WorkspaceApplicationRuntimeLifecycleInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	return contracts.WorkspaceApplicationRuntimeCredentials{RuntimeID: input.RuntimeID, WorkspaceID: input.WorkspaceID}, nil
}

// upgradeLedger records the deployment receipt so the qualification can prove
// it carries the original operation identity.
type upgradeLedger struct {
	fakeLedgerClient
	receipts []clients.ReceiptInput
}

func (l *upgradeLedger) RecordReceipt(_ context.Context, input clients.ReceiptInput, _ string) (clients.Receipt, error) {
	l.receipts = append(l.receipts, input)
	return clients.Receipt{ReceiptInput: input, ReceiptID: "receipt-from-ledger"}, nil
}

// upgradeQualificationFixture is the retained installation an in-place upgrade
// from public v0.1.8 carries forward: the Workspace, its selected application
// generation, the accepted Workspace Key rotation whose replay persisted the
// pending child, and that child exactly as v0.1.8 wrote it.
type upgradeQualificationFixture struct {
	app      *controlPlaneServer
	service  *controlplane.Service
	store    controlPlaneTableStore
	fabric   *upgradeProviderFabric
	ledger   *upgradeLedger
	selected workspaceApplicationDeploymentIntent
	child    workspaceApplicationDeploymentIntent
	parentID string
	before   map[string]any
}

// seedUpgradeQualificationFixture models the v0.1.8 child constructor and its
// accepted rotation parent, bound to its runtime and carrying the child it
// claimed.
func seedUpgradeQualificationFixture(t *testing.T, store controlPlaneTableStore) upgradeQualificationFixture {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	ctx := context.Background()
	fabric := &upgradeProviderFabric{states: map[string]string{}}
	ledger := &upgradeLedger{}
	service := newTestService(ledger, fabric)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	app := server.(*controlPlaneHTTPHandler).app
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	workspace, found, err := store.GetWorkspace(ctx, "ws-alpha")
	if err != nil || !found {
		t.Fatalf("retained workspace missing: found=%v err=%v", found, err)
	}
	workspace["workspaceApiKeyId"] = upgradeRotationOldKeyID
	mustStore(t, store.SaveWorkspace(ctx, workspace))

	revision := defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))
	selected := seedApplicationRevisionForLifecycle(t, app, "ws-alpha", revision,
		contracts.WorkspaceApplicationRuntimeConfiguration{CredentialVersion: "opl-credential-v1"},
		[]contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", Key: contracts.WorkspaceApplicationGatewayKeyField, SecretRef: "opl-gateway-ws-alpha", Version: "v1"}},
		upgradeRotationOldKeyID, true)
	fabric.states[selected.OperationID+":runtime"] = "running"

	parentID := "workspace-key-rotate-" + stableID("ws-alpha", upgradeRotationKey)[:18]
	childID := workspaceApplicationDeploymentOperationID("ws-alpha", "gateway-rotation:"+parentID)

	// The rotation is persisted exactly as v0.1.8 persisted it after it
	// claimed the child: still started, still bound to the new Gateway Secret.
	parent := workspaceKeyRotationOperation{
		RequestHash: "rotation-request-hash", Phase: "runtime_bind",
		OldKeyID: upgradeRotationOldKeyID, NewKeyID: upgradeRotationNewKeyID,
		ReplacementName: "opl-workspace-ws-alpha-replacement", RetiredName: "opl-workspace-ws-alpha",
		SecretRef: upgradeRotationSecretRef, SecretVersion: upgradeRotationSecretVer,
		ApplicationDeploymentID: childID,
		AuditEvent: map[string]any{
			"id":     "audit-" + stableID(parentID, "workspace.gateway_key.rotate")[:12],
			"action": "workspace.gateway_key.rotate", "resourceKind": "workspace_gateway_key",
			"resourceId": "ws-alpha", "actorAccountId": "acct-alpha", "targetAccountId": "acct-alpha",
			"actorUserId": "owner-alpha", "createdAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	encodedParent, err := json.Marshal(parent)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, map[string]any{
		"id": parentID, "operationId": parentID, "accountId": "acct-alpha",
		"workspaceId": "ws-alpha", "resourceId": "ws-alpha", "resourceKind": "workspace_gateway_key",
		"action": "workspace.gateway_key.rotate", "status": "started", "result": string(encodedParent),
		"provider":  "sub2api",
		"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
	}))

	// The child replaces only the parent's Gateway Secret binding and keeps the
	// selected generation's application, revision, resource and data identities.
	workspace, _, _ = store.GetWorkspace(ctx, "ws-alpha")
	configuration := selected.Configuration
	configuration.CredentialVersion = stableID("workspace-application-gateway-rotation", "ws-alpha", parentID)
	configuration.CredentialSourceRuntimeOperationID = ""
	bindings := append([]contracts.WorkspaceApplicationRuntimeSecretBinding(nil), selected.SecretBindings...)
	bound := false
	for index := range bindings {
		if bindings[index].Name == "gateway" && bindings[index].Key == contracts.WorkspaceApplicationGatewayKeyField {
			bindings[index].SecretRef, bindings[index].Version = upgradeRotationSecretRef, upgradeRotationSecretVer
			bound = true
		}
	}
	if !bound {
		t.Fatal("the selected generation carries no Gateway Secret binding to rotate")
	}
	digest, err := contracts.WorkspaceApplicationConfigurationDigest(configuration, bindings, selected.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	child := workspaceApplicationDeploymentIntent{
		SchemaVersion: workspaceApplicationDeploymentSchemaVersion, Version: 2,
		OperationID: childID, AccountID: selected.AccountID, WorkspaceID: selected.WorkspaceID,
		ComputeID: selected.ComputeID, StorageID: selected.StorageID, AttachmentID: selected.AttachmentID,
		ApplicationID: selected.ApplicationID, TargetRevision: selected.TargetRevision, RevisionDigest: selected.RevisionDigest,
		ConfigurationDigest: digest, DataBindingIDs: []string{selected.DataBindingID},
		ExpectedWorkspaceVersion: int64(numberField(workspace, "applicationBindingVersion", 0)),
		CurrentBinding:           stringValue(workspace["applicationBinding"]),
		Configuration:            configuration, SecretBindings: bindings,
		DataBindingID: selected.DataBindingID, DataLayout: selected.DataLayout, DataSourceRuntimeOperationID: selected.DataSourceRuntimeOperationID,
		PreviousDeploymentID: selected.OperationID, WorkspaceAPIKeyID: upgradeRotationNewKeyID, OriginOperationID: parentID,
		Phase: workspaceApplicationDeploymentIntentPhase, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, binding := range bindings {
		child.SecretBindingVersions = append(child.SecretBindingVersions, binding.Name+":"+binding.SecretRef+":"+binding.Version+":"+binding.Key)
	}
	child.RequestHash = workspaceApplicationDeploymentRequestHash(child)
	encodedChild, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	seedWorkspaceApplicationDeploymentIntentForTest(t, store, map[string]any{
		"id": child.OperationID, "operationId": child.OperationID, "accountId": child.AccountID,
		"workspaceId": child.WorkspaceID, "resourceId": child.WorkspaceID, "resourceKind": "workspace",
		"action": workspaceApplicationDeploymentAction, "status": "pending", "result": string(encodedChild),
		"createdAt": child.CreatedAt,
	})
	workspace, _, _ = store.GetWorkspace(ctx, child.WorkspaceID)
	return upgradeQualificationFixture{
		app: app, service: service, store: store, fabric: fabric, ledger: ledger,
		selected: selected, child: child, parentID: parentID, before: cloneMap(workspace),
	}
}

// upgradeQualificationRetainedRow reads the retained child back and proves it
// still carries the identities v0.1.8 wrote.
func upgradeQualificationRetainedRow(t *testing.T, store controlPlaneTableStore, child workspaceApplicationDeploymentIntent) workspaceApplicationDeploymentIntent {
	t.Helper()
	row, found, err := store.GetRuntimeOperation(context.Background(), child.OperationID)
	if err != nil || !found {
		t.Fatalf("retained child missing: found=%v err=%v", found, err)
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	if intent.OperationID != child.OperationID || intent.Version != 2 || intent.OriginOperationID != child.OriginOperationID ||
		intent.WorkspaceAPIKeyID != child.WorkspaceAPIKeyID || intent.PreviousDeploymentID != child.PreviousDeploymentID ||
		intent.ComputeID != child.ComputeID || intent.StorageID != child.StorageID || intent.AttachmentID != child.AttachmentID ||
		intent.ApplicationID != child.ApplicationID || intent.TargetRevision != child.TargetRevision ||
		intent.RevisionDigest != child.RevisionDigest || intent.ConfigurationDigest != child.ConfigurationDigest ||
		intent.ExpectedWorkspaceVersion != child.ExpectedWorkspaceVersion || intent.CurrentBinding != child.CurrentBinding ||
		intent.DataBindingID != child.DataBindingID || intent.RequestHash != child.RequestHash {
		t.Fatalf("retained child lost its original identities: %+v", intent)
	}
	return intent
}

// upgradeQualificationAssertFinished proves the record finished under its
// original operation and resource identities.
func upgradeQualificationAssertFinished(t *testing.T, fixture upgradeQualificationFixture) workspaceApplicationDeploymentIntent {
	t.Helper()
	ctx := context.Background()
	intent := upgradeQualificationRetainedRow(t, fixture.store, fixture.child)
	if intent.Phase != workspaceApplicationDeploymentActivePhase || intent.ReceiptID != "receipt-from-ledger" {
		t.Fatalf("retained child did not finish: %+v", intent)
	}
	row, _, _ := fixture.store.GetRuntimeOperation(ctx, fixture.child.OperationID)
	if stringValue(row["status"]) != "succeeded" {
		t.Fatalf("retained child status=%q", stringValue(row["status"]))
	}
	workspace, _, _ := fixture.store.GetWorkspace(ctx, fixture.child.WorkspaceID)
	if stringValue(workspace["applicationBinding"]) != fixture.child.ApplicationID+"@"+fixture.child.TargetRevision ||
		int64(numberField(workspace, "applicationBindingVersion", 0)) != fixture.child.ExpectedWorkspaceVersion+1 ||
		stringValue(workspace["currentApplicationDeploymentId"]) != fixture.child.OperationID ||
		int64(numberField(workspace, "workspaceApiKeyId", 0)) != upgradeRotationNewKeyID {
		t.Fatalf("retained workspace was not activated at the reserved version: %+v", workspace)
	}
	for _, field := range []string{"storageId", "currentComputeAllocationId", "currentAttachmentId", "paidThrough", "periodStart", "totalUsdMicros", "priceVersion"} {
		if !reflect.DeepEqual(workspace[field], fixture.before[field]) {
			t.Fatalf("the recovery changed the retained %s: before=%v after=%v", field, fixture.before[field], workspace[field])
		}
	}
	if len(fixture.ledger.receipts) != 1 {
		t.Fatalf("deployment receipts=%d", len(fixture.ledger.receipts))
	}
	receipt := fixture.ledger.receipts[0]
	if receipt.Type != "workspace.application_deployed.v1" || receipt.RequestID != fixture.child.OperationID ||
		receipt.WorkspaceID != fixture.child.WorkspaceID || receipt.AccountID != fixture.child.AccountID ||
		receipt.Execution["operationId"] != fixture.child.OperationID ||
		receipt.Execution["applicationId"] != fixture.child.ApplicationID ||
		receipt.Execution["targetRevision"] != fixture.child.TargetRevision ||
		receipt.Execution["configurationDigest"] != fixture.child.ConfigurationDigest {
		t.Fatalf("deployment receipt lost the original operation identity: %+v", receipt)
	}
	return intent
}

// upgradeQualificationAssertPredecessor proves the selected generation was
// retired under the child's own mutation identity. A suspension that was
// already recorded is continued rather than issued again, so the expected
// number of suspension mutations depends on where the recovery window starts.
func upgradeQualificationAssertPredecessor(t *testing.T, fixture upgradeQualificationFixture, suspensions int) {
	t.Helper()
	predecessor := fixture.selected.OperationID + ":runtime"
	suspended, retired := 0, 0
	for index, mutation := range fixture.fabric.mutations {
		if mutation.RuntimeOperationID != predecessor {
			t.Fatalf("lifecycle mutation touched %q", mutation.RuntimeOperationID)
		}
		expected := fixture.child.OperationID + ":predecessor:" + mutation.DesiredState
		if fixture.fabric.keys[index] != expected || mutation.IdempotencyKey != expected {
			t.Fatalf("lifecycle mutation %d lost the original mutation identity: key=%q input=%q", index, fixture.fabric.keys[index], mutation.IdempotencyKey)
		}
		switch mutation.DesiredState {
		case "suspended":
			suspended++
		case "absent":
			retired++
		}
	}
	if suspended != suspensions || retired != 1 {
		t.Fatalf("predecessor lifecycle suspended=%d retired=%d mutations=%+v", suspended, retired, fixture.fabric.mutations)
	}
	if fixture.fabric.states[predecessor] != "absent" {
		t.Fatalf("predecessor was not retired: %q", fixture.fabric.states[predecessor])
	}
}

// TestWorkspaceApplicationUpgradeQualificationFinishesRetainedRotationChild
// drives the retained worker over the pending child an in-place upgrade from
// public v0.1.8 carries forward, and proves the record finishes under the
// operation and resource identities the older release wrote.
func TestWorkspaceApplicationUpgradeQualificationFinishesRetainedRotationChild(t *testing.T) {
	store := newMemoryTableStore()
	fixture := seedUpgradeQualificationFixture(t, store)
	ctx := context.Background()

	// The retained child is pending, reserved on its Workspace, and still
	// names the rotation that claimed it.
	row, _, _ := store.GetRuntimeOperation(ctx, fixture.child.OperationID)
	if stringValue(row["status"]) != "pending" || stringValue(row["action"]) != workspaceApplicationDeploymentAction {
		t.Fatalf("retained child is not a pending deployment: %+v", row)
	}
	workspace, _, _ := store.GetWorkspace(ctx, fixture.child.WorkspaceID)
	if stringValue(workspace["reservedApplicationDeploymentId"]) != fixture.child.OperationID {
		t.Fatalf("retained child lost its reservation: %+v", workspace)
	}
	parentRow, found, _ := store.GetRuntimeOperation(ctx, fixture.parentID)
	if !found {
		t.Fatal("the retained rotation parent is missing")
	}
	parent, err := decodeWorkspaceKeyRotation(parentRow)
	if err != nil || parent.ApplicationDeploymentID != fixture.child.OperationID || parent.NewKeyID != upgradeRotationNewKeyID {
		t.Fatalf("the retained rotation no longer names its child: %+v err=%v", parent, err)
	}

	if got := upgradeQualificationRetainedRow(t, store, fixture.child); got.Phase != workspaceApplicationDeploymentIntentPhase {
		t.Fatalf("retained child phase = %q, want %q", got.Phase, workspaceApplicationDeploymentIntentPhase)
	}
	if err := fixture.app.admitWorkspaceGatewayBudgetMutation(ctx, fixture.child.WorkspaceID); !errors.Is(err, errWorkspaceKeyRotationInProgress) {
		t.Fatalf("retained rotation budget admission error = %v, want %v", err, errWorkspaceKeyRotationInProgress)
	}

	runDeploymentWorkerToCompletion(t, fixture.app, fixture.service, fixture.child.OperationID)
	upgradeQualificationAssertFinished(t, fixture)
	upgradeQualificationAssertPredecessor(t, fixture, 1)

	// The provider mutation ran once, for the child's own runtime, with the
	// original resource, data and credential identities.
	if len(fixture.fabric.applicationRuntimeInputs) != 1 {
		t.Fatalf("provider runtime mutations=%d", len(fixture.fabric.applicationRuntimeInputs))
	}
	input := fixture.fabric.applicationRuntimeInputs[0]
	if input.RuntimeOperationID != fixture.child.OperationID+":runtime" || input.ComputeID != fixture.child.ComputeID ||
		input.VolumeID != fixture.child.StorageID || input.AttachmentID != fixture.child.AttachmentID ||
		input.AttachmentOperationID != "workspace-launch-alpha:attachment" ||
		input.DataBindingID != fixture.child.DataBindingID || input.ConfigurationDigest != fixture.child.ConfigurationDigest ||
		input.Configuration.CredentialVersion != fixture.child.Configuration.CredentialVersion ||
		input.SecretBindings[0].SecretRef != upgradeRotationSecretRef || input.SecretBindings[0].Version != upgradeRotationSecretVer {
		t.Fatalf("provider mutation lost the original identities: %+v", input)
	}
}

// TestWorkspaceApplicationUpgradeQualificationResumesLostActivationResponse
// proves a lost activation response does not repeat the provider mutation or
// the activation, and that the receipt still carries the original operation
// identity.
func TestWorkspaceApplicationUpgradeQualificationResumesLostActivationResponse(t *testing.T) {
	backing := newMemoryTableStore()
	store := &activationResponseLossStore{controlPlaneTableStore: backing, loseResponse: true}
	fixture := seedUpgradeQualificationFixture(t, backing)
	fixture.store = store
	fixture.app.tables = store
	ctx := context.Background()

	// The retained record reaches the activation on its third phase, and the
	// activation commits before its response is lost.
	var driveErr error
	for range 3 {
		if driveErr = fixture.app.runWorkspaceApplicationDeploymentsOnce(ctx, fixture.service); driveErr != nil {
			break
		}
	}
	if driveErr == nil {
		t.Fatal("expected the lost activation response")
	}
	// The activation committed with the retired phase, so the next tick has to
	// resume from there instead of activating again.
	row, _, _ := store.GetRuntimeOperation(ctx, fixture.child.OperationID)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentRetiringPhase {
		t.Fatalf("lost activation response did not leave a resumable record: %+v err=%v", intent, err)
	}
	workspace, _, _ := store.GetWorkspace(ctx, fixture.child.WorkspaceID)
	if stringValue(workspace["applicationBinding"]) != fixture.child.ApplicationID+"@"+fixture.child.TargetRevision ||
		int64(numberField(workspace, "applicationBindingVersion", 0)) != fixture.child.ExpectedWorkspaceVersion+1 {
		t.Fatalf("the committed activation was not durable: %+v", workspace)
	}

	runDeploymentWorkerToCompletion(t, fixture.app, fixture.service, fixture.child.OperationID)
	upgradeQualificationAssertFinished(t, fixture)
	upgradeQualificationAssertPredecessor(t, fixture, 1)
	if len(fixture.fabric.applicationRuntimeInputs) != 1 {
		t.Fatalf("the lost activation response repeated the provider mutation: %d", len(fixture.fabric.applicationRuntimeInputs))
	}
}

// TestWorkspaceApplicationUpgradeQualificationContinuesSuspendedPredecessor
// proves a predecessor that is already suspended is continued, not re-issued
// under a second identity.
func TestWorkspaceApplicationUpgradeQualificationContinuesSuspendedPredecessor(t *testing.T) {
	for _, recorded := range []bool{true, false} {
		name := "recorded suspension"
		if !recorded {
			name = "unrecorded suspension"
		}
		t.Run(name, func(t *testing.T) {
			store := newMemoryTableStore()
			fixture := seedUpgradeQualificationFixture(t, store)
			ctx := context.Background()
			predecessor := fixture.selected.OperationID + ":runtime"

			// The predecessor is already suspended at the provider. Whether the
			// worker recorded that suspension is the only difference between
			// the two recovery states.
			fixture.fabric.states[predecessor] = "suspended"
			row, _, _ := store.GetRuntimeOperation(ctx, fixture.child.OperationID)
			intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
			if err != nil {
				t.Fatal(err)
			}
			if recorded {
				intent.Phase = workspaceApplicationDeploymentRuntimePhase
				intent.PredecessorObservation = &contracts.WorkspaceApplicationRuntimeLifecycleResult{
					RuntimeID: contracts.WorkspaceApplicationRuntimeID(predecessor), WorkspaceID: fixture.child.WorkspaceID, State: "suspended",
				}
			} else {
				intent.Phase = workspaceApplicationDeploymentPredecessorPhase
			}
			encoded, err := json.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			row["result"] = string(encoded)
			mustStore(t, store.SaveRuntimeOperation(ctx, row))

			runDeploymentWorkerToCompletion(t, fixture.app, fixture.service, fixture.child.OperationID)
			upgradeQualificationAssertFinished(t, fixture)
			// A recorded suspension is continued, never issued again. An
			// unrecorded one is re-issued under the original mutation identity.
			upgradeQualificationAssertPredecessor(t, fixture, map[bool]int{true: 0, false: 1}[recorded])
			for index, mutation := range fixture.fabric.mutations {
				expected := fixture.child.OperationID + ":predecessor:" + mutation.DesiredState
				if fixture.fabric.keys[index] != expected {
					t.Fatalf("lifecycle mutation %d used a new identity: %q", index, fixture.fabric.keys[index])
				}
			}
			if len(fixture.fabric.applicationRuntimeInputs) != 1 {
				t.Fatalf("the predecessor recovery repeated the provider mutation: %d", len(fixture.fabric.applicationRuntimeInputs))
			}
		})
	}
}

// TestWorkspaceApplicationUpgradeQualificationResumesAcrossRestart proves the
// restarted Control Plane resumes the retained child from the same database
// and finishes it under its original identities.
func TestWorkspaceApplicationUpgradeQualificationResumesAcrossRestart(t *testing.T) {
	admin := openControlPlaneTestPostgres(t)
	database := fmt.Sprintf("control_plane_application_upgrade_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, database)
		_, _ = admin.Exec(`DROP DATABASE ` + database)
		_ = admin.Close()
	})
	databaseURL := controlPlaneTestPostgresURL(t, database, "")
	store, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedUpgradeQualificationFixture(t, store)
	ctx := context.Background()

	// The upgrade interrupts the retained child after the predecessor is
	// suspended but before the binding is activated.
	if err := fixture.app.runWorkspaceApplicationDeploymentsOnce(ctx, fixture.service); err != nil {
		t.Fatal(err)
	}
	row, _, _ := store.GetRuntimeOperation(ctx, fixture.child.OperationID)
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil || intent.Phase != workspaceApplicationDeploymentRuntimePhase {
		t.Fatalf("retained child did not reach the interrupted phase: %+v err=%v", intent, err)
	}
	_ = store.(*postgresEntStateStore).Close()

	// The upgraded Control Plane opens the same retained database.
	store, err = newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.(*postgresEntStateStore).Close() })
	fixture.store = store
	restarted, err := NewPersistentServer(fixture.service, store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = restarted.(*controlPlaneHTTPHandler).app
	runDeploymentWorkerToCompletion(t, fixture.app, fixture.service, fixture.child.OperationID)

	upgradeQualificationAssertFinished(t, fixture)
	upgradeQualificationAssertPredecessor(t, fixture, 1)
	if len(fixture.fabric.applicationRuntimeInputs) != 1 {
		t.Fatalf("the restart repeated the provider mutation: %d", len(fixture.fabric.applicationRuntimeInputs))
	}
	// The retained rotation still names the child it claimed.
	parentRow, found, _ := store.GetRuntimeOperation(ctx, fixture.parentID)
	if !found {
		t.Fatal("the retained rotation parent did not survive the restart")
	}
	parent, err := decodeWorkspaceKeyRotation(parentRow)
	if err != nil || parent.ApplicationDeploymentID != fixture.child.OperationID {
		t.Fatalf("the retained rotation lost its child across the restart: %+v err=%v", parent, err)
	}
}
