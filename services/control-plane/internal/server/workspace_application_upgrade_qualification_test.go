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
	"opl-cloud/services/control-plane/internal/controlplane"
)

// An in-place upgrade from public v0.1.8 keeps the installation's database.
// v0.1.8 could replay an accepted Workspace Key rotation and persist a pending
// `workspace.application.deploy` child for the selected application
// generation. The rotation constructor and its rotation recovery are retired,
// so the only way such a row exists today is an older release having written
// it.
//
// The shared application deployment Worker that used to advance that child is
// retired with the rest of the engine. What has to survive the retirement is
// the read side: the retained decoders still read the carried-forward row with
// the identities the older release wrote, and the retained consumers still see
// the rotation that owns it. These fixtures seed exactly that pre-upgrade state
// and prove the retained surface still sees it.
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

// upgradeLedger records the deployment receipt the retired worker wrote.
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

// seedUpgradeQualificationFixture reproduces the v0.1.8 child constructor
// field for field. The rotation parent is persisted in the state v0.1.8 left
// it: accepted, bound to its runtime, and still carrying the child it
// claimed.
func seedUpgradeQualificationFixture(t *testing.T, store controlPlaneTableStore) upgradeQualificationFixture {
	t.Helper()
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
	mustStore(t, store.SaveRuntimeOperation(context.Background(), map[string]any{
		"id": parentID, "operationId": parentID, "accountId": "acct-alpha",
		"workspaceId": "ws-alpha", "resourceId": "ws-alpha", "resourceKind": "workspace_gateway_key",
		"action": "workspace.gateway_key.rotate", "status": "started", "result": string(encodedParent),
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

// TestRetainedV018RotationChildStaysDecodableAndFenced proves the retained read
// side still sees a rotation an in-place upgrade from public v0.1.8 carried
// forward: the pending deployment child decodes with the identities the older
// release wrote, the Workspace still reserves it, the rotation parent still
// names it, and a retained consumer still refuses the Workspace while that
// rotation has not succeeded.
//
// The execution engine that used to advance the child is retired with this
// slice. The read side is the part the retirement must not break, because it
// is what keeps an unresolved carried-forward rotation visible.
func TestRetainedV018RotationChildStaysDecodableAndFenced(t *testing.T) {
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
	intent := upgradeQualificationRetainedRow(t, fixture.store, fixture.child)
	if intent.Phase != workspaceApplicationDeploymentIntentPhase {
		t.Fatalf("retained child is not an intent-phase row: %+v", intent)
	}
	parentRow, found, _ := store.GetRuntimeOperation(ctx, fixture.parentID)
	if !found {
		t.Fatal("the retained rotation parent is missing")
	}
	parent, err := decodeWorkspaceKeyRotation(parentRow)
	if err != nil || parent.ApplicationDeploymentID != fixture.child.OperationID || parent.NewKeyID != upgradeRotationNewKeyID {
		t.Fatalf("the retained rotation no longer names its child: %+v err=%v", parent, err)
	}
	// The Gateway budget admission is a retained consumer of the rotation rows.
	// It has to keep refusing while the carried-forward rotation has not
	// succeeded, otherwise the unresolved rotation stops being visible.
	if err := fixture.app.admitWorkspaceGatewayBudgetMutation(ctx, fixture.child.WorkspaceID); !errors.Is(err, errWorkspaceKeyRotationInProgress) {
		t.Fatalf("gateway budget admission error=%v, want %v", err, errWorkspaceKeyRotationInProgress)
	}
}
