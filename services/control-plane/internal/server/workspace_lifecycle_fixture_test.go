package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

// The lifecycle fixtures stay because the application origin, deployment and
// application revision suites still seed a Workspace with its compute and
// storage children. The launch and delete execution that used to produce them
// is retired with the retained Control Plane execution.
type workspaceDeleteEventStore struct {
	controlPlaneTableStore
	events *workspaceDeleteEvents
}

func seedCurrentApplicationForLifecycle(t *testing.T, app *controlPlaneServer, workspaceID, applicationID string, selected bool) workspaceApplicationDeploymentIntent {
	t.Helper()
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: applicationID, Version: "1.0.0", Platform: "linux/amd64", Image: "registry.example/" + applicationID + "@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "application", EntryPort: "http", Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}}}
	return seedApplicationRevisionForLifecycle(t, app, workspaceID, revision, contracts.WorkspaceApplicationRuntimeConfiguration{}, nil, 0, selected)
}

func seedApplicationRevisionForLifecycle(t *testing.T, app *controlPlaneServer, workspaceID string, revision contracts.WorkspaceApplicationRevision, configuration contracts.WorkspaceApplicationRuntimeConfiguration, bindings []contracts.WorkspaceApplicationRuntimeSecretBinding, keyID int64, selected bool) workspaceApplicationDeploymentIntent {
	t.Helper()
	intent := applicationDeploymentFixtureForTest(t, app, workspaceID, "seed-"+revision.ApplicationID+"-"+revision.Version, revision)
	workspace, found, err := app.tables.GetWorkspace(context.Background(), workspaceID)
	if err != nil || !found {
		t.Fatalf("fixture Workspace missing: found=%v err=%v", found, err)
	}
	// These lifecycle fixtures share workspace-launch-alpha as their original
	// full Launch. Its accepted runtime remains the explicit legacy predecessor.
	if intent.CurrentBinding == provisioning.ApplicationBindingOPLApp {
		intent.LegacyPredecessor = &contracts.WorkspaceRuntimePowerInput{
			SchemaVersion: 1, AccountID: intent.AccountID, WorkspaceID: workspaceID,
			RuntimeID: stringValue(workspace["runtimeId"]), RuntimeOperationID: "workspace-launch-alpha:runtime", PaidThrough: stringValue(workspace["paidThrough"]),
		}
	}
	if contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) && configuration.CredentialVersion == "" {
		configuration.CredentialVersion = "accepted-fixture-credential-" + intent.OperationID
		if intent.LegacyPredecessor != nil {
			configuration.CredentialVersion = stringValue(nested(workspace, "access", "credentialVersion"))
			configuration.CredentialSourceRuntimeOperationID = intent.LegacyPredecessor.RuntimeOperationID
		}
	}
	if intent.LegacyPredecessor != nil && contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
		intent.DataLayout, intent.DataSourceRuntimeOperationID = "legacy_opl", intent.LegacyPredecessor.RuntimeOperationID
	}
	intent.Configuration, intent.SecretBindings, intent.WorkspaceAPIKeyID = configuration, bindings, keyID
	intent = seedAcceptedApplicationDeploymentForTest(t, app, intent)
	if !selected {
		return intent
	}
	row, found, err := app.tables.GetRuntimeOperation(context.Background(), intent.OperationID)
	if err != nil || !found {
		t.Fatal("missing intent", err)
	}
	intent.Phase, intent.ActivationAt, intent.ReceiptID = workspaceApplicationDeploymentActivePhase, time.Now().UTC().Format(time.RFC3339Nano), "application-receipt"
	// An activated generation always carries the runtime observation Fabric
	// reported for it, including the destination the installation gateway serves.
	activated := contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: workspaceID,
		RuntimeID: contracts.WorkspaceApplicationRuntimeID(intent.OperationID + ":runtime"),
		Status:    "ready", Entry: applicationGatewayEntry(revision),
		Components: contracts.WorkspaceApplicationRuntimeComponents(revision),
	}
	for index := range activated.Components {
		activated.Components[index].State = "ready"
	}
	intent.RuntimeObservation = &activated
	payload, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	row["result"], row["status"] = string(payload), "succeeded"
	if err := app.tables.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	workspace, found, err = app.tables.GetWorkspace(context.Background(), workspaceID)
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

func newResourceOnlyWorkspaceLifecycleFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger, *workspaceDeleteEvents) {
	t.Helper()
	events := &workspaceDeleteEvents{}
	// A resource-only purchase created no Runtime objects, so Fabric's labelled
	// readback reports them absent. The deletion now reads that back before it
	// records the runtime stage evidence.
	fabric := &workspaceDeleteFabric{events: events, residualObserveState: clients.WorkspaceOwnerObservationAbsent}
	store := workspaceDeleteEventStore{controlPlaneTableStore: newMemoryTableStore(), events: events}
	sub2API := &workspaceDeleteSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000_000, charges: map[string]int64{}},
		userID:            41, keyID: 19, keyExists: true, history: map[string]clients.Sub2APIBalanceHistoryEntry{},
	}
	ledger := &workspaceDeleteLedger{}
	service := controlplane.NewService(ledger, fabric, sub2API)
	fixture := newWorkspaceDeleteFixtureWithService(t, store, fabric, service)
	sub2API.events, ledger.events = events, events
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "workspace-launch-alpha", "acct-alpha", stringValue(fixture.workspace["ownerUserId"]), 41
	command.WorkspaceID, command.Name = "ws-alpha", "Alpha"
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	if operation.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly {
		t.Fatal("lifecycle fixture launch must be resource-only")
	}
	fixtureClock := time.Now().UTC()
	periodStart, paidThrough := workspaceDeleteFixtureBillingPeriod(fixtureClock)
	// This fixture's Workspace is refunded on deletion, so its refund window must
	// stay inside the platform's monthly baseline exactly like the other delete
	// fixture's.
	assertWorkspaceDeleteFixtureRefundWindow(t, periodStart, fixtureClock)
	readyFacts := map[string]any{
		"sub2apiRedeemCode":          "opl:workspace-purchase-alpha",
		"chargeAttempted":            true,
		"chargeConfirmation":         map[string]any{"code": "opl:workspace-purchase-alpha", "userId": int64(41), "chargeUsdMicros": int64(52_580_000), "status": "used"},
		"postChargeBalanceUsdMicros": int64(947_420_000), "postChargeBalanceKnown": true, "billingPeriodState": "frozen",
		"periodStart": periodStart.Format(time.RFC3339Nano), "paidThrough": paidThrough.Format(time.RFC3339Nano), "billingAnchorDay": periodStart.Day(),
		"computeAllocationId": "compute-alpha", "computeBindingRef": "workspace-launch-alpha:ensure_compute_allocation",
		"storageId": "storage-alpha", "storageBindingRef": "workspace-launch-alpha:storage",
		"attachmentId": "attachment-alpha", "attachmentBindingRef": "workspace-launch-alpha:attachment",
		"activationOperationId": "workspace-launch-alpha:activation", "workspaceActivatedAt": periodStart.Format(time.RFC3339Nano),
		"receiptId": "receipt-purchase-alpha", "receiptOperationId": "workspace-launch-alpha:purchase-receipt",
	}
	for key, value := range readyFacts {
		operation.raw[key], _ = json.Marshal(value)
	}
	operation.Stage, operation.Status = "succeeded", "succeeded"
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), launchRow); err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(workspace["runtimeId"]) != "" {
		t.Fatalf("resource-only activation runtimeId = %q, want empty", workspace["runtimeId"])
	}
	if ready, ok := nested(workspace, "runtime", "ready").(bool); !ok || ready {
		t.Fatal("resource-only activation must not mark the runtime ready")
	}
	if _, exists := workspace["workspaceApiKeyId"]; exists {
		t.Fatal("resource-only activation must not carry a workspace API key")
	}
	if workspace["applicationBinding"] != provisioning.ApplicationBindingEmpty {
		t.Fatalf("resource-only activation applicationBinding = %v, want empty", workspace["applicationBinding"])
	}
	workspace["purchaseReceiptId"] = "receipt-purchase-alpha"
	workspace["sub2apiUserId"], workspace["sub2apiRedeemCode"] = int64(41), "opl:workspace-purchase-alpha"
	purchaseInput, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		t.Fatal(err)
	}
	ledger.purchase = clients.Receipt{ReceiptInput: purchaseInput, ReceiptID: "receipt-purchase-alpha"}
	if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveStorage(context.Background(), map[string]any{
		"id": "storage-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "available",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveAttachment(context.Background(), map[string]any{
		"id": "attachment-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "volumeId": "storage-alpha", "status": "attached",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.workspace = workspace
	return fixture, sub2API, ledger, events
}

func newWorkspaceDeleteEventFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger, *workspaceDeleteEvents) {
	t.Helper()
	events := &workspaceDeleteEvents{}
	fabric := &workspaceDeleteFabric{events: events}
	store := workspaceDeleteEventStore{controlPlaneTableStore: newMemoryTableStore(), events: events}
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixtureWith(t, store, fabric)
	sub2API.events, ledger.events = events, events
	return fixture, sub2API, ledger, events
}

// The delete fixture models a Workspace that was purchased a fixed age before
// the fixture clock, so its paid period and its refund window are a fixed
// distance from the deletion moment.
//
// A calendar-anchored period cannot do this: the purchase then sits anywhere
// between 0 and 31 days in the past, which lands on the edge of the platform's
// 720-hour monthly refund baseline twice a month. On those days the refund is
// correctly zero, the refund is not dispatched, and the suite fails on a
// calendar date instead of on the behaviour under test.

// seedResourceOnlyRenewalLaunch persists a succeeded resource-only Launch for
// the monthly billing fixture's Workspace, mirroring seedD4RenewalRuntime
// without the Gateway key, secret and runtime stages.
func seedResourceOnlyRenewalLaunch(t *testing.T, store controlPlaneTableStore, workspace map[string]any) {
	t.Helper()
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID = "workspace-launch-monthly"
	command.AccountID = stringValue(workspace["accountId"])
	command.OwnerUserID = stringValue(workspace["ownerUserId"])
	command.WorkspaceID = stringValue(workspace["id"])
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := operation.stagePlan()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range plan[:len(plan)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": workspace["computeAllocationId"], "storageId": workspace["storageId"], "attachmentId": workspace["currentAttachmentId"]} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		if stage == contracts.StageActivation {
			facts["activationOperationId"] = operation.ID + ":activation"
		}
		if stage == contracts.StageReceipt {
			facts["receiptOperationId"] = operation.ID + ":purchase-receipt"
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed resource-only renewal stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	operation.Version = len(plan)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
}

type workspaceDeleteEvents struct {
	mu    sync.Mutex
	items []string
}

type workspaceDeleteFixture struct {
	server             http.Handler
	store              controlPlaneTableStore
	session            *httptest.ResponseRecorder
	fabric             *workspaceDeleteFabric
	workspace          map[string]any
	billingPeriodStart time.Time
}

type workspaceDeleteFabric struct {
	fakeFabricClient
	mu                         sync.Mutex
	calls                      []string
	failStage                  string
	failures                   int
	mismatchStage              string
	unknownStage               string
	storageStatus              string
	computeReads               []string
	computeTerminal            <-chan struct{}
	destroyed                  bool
	clearObservationsOnDestroy bool
	runtimeResponseLost        bool
	observeState               string
	secretObserveState         string
	residualObserveState       string
	observeErr                 error
	observeRuntimeID           string
	observeKeyID               int64
	observeSecretRef           string
	observeFingerprint         string
	residualObservedAt         string
	residualReadbackID         *string
	storageResults             []workspaceDeleteStorageResult
	storageDestroyed           bool
	storageReadbackResults     []clients.StorageVolume
	storageReadObservedAt      *string
	storageReadbackID          *string
	computeReadbackResults     []clients.ComputeAllocation
	computeDestroyResults      []clients.ComputeAllocation
	storageReadStatus          string
	storageReadCBSStatus       string
	storageReadProvider        string
	storageProviderResourceID  string
	storageDestroyResourceID   string
	storageBindingPresent      *bool
	computeReadCVMStatus       string
	computeReadTKEStatus       string
	computeReadMachinePresent  *bool
	computeReadProvider        string
	computeReadMachineName     string
	computeReadInstanceID      string
	computeReadErr             error
	storageReadErr             error
	events                     *workspaceDeleteEvents
}

// workspaceDeleteStorageResult models one Fabric storage deletion outcome: its
// lifecycle status plus the stable retryable classification Fabric reports.
type workspaceDeleteStorageResult struct {
	Status       string
	DestroyState string
}

type workspaceDeleteSub2API struct {
	*testSub2APIClient
	mu                 sync.Mutex
	userID             int64
	keyID              int64
	keyExists          bool
	keyReads           int
	keyDeletes         int
	keyDeleteKeys      []string
	history            map[string]clients.Sub2APIBalanceHistoryEntry
	historyReads       [][]string
	refunds            []clients.Sub2APIRefundInput
	keyReadErr         error
	keyDeleteErr       error
	keyResponseLost    bool
	refundResponseLost bool
	keyName            string
	keyUserID          int64
	keyStatus          string
	events             *workspaceDeleteEvents
}

type workspaceDeleteLedger struct {
	fakeLedgerClient
	mu               sync.Mutex
	purchase         clients.Receipt
	reads            int
	receipts         []clients.ReceiptInput
	keys             []string
	recorded         map[string]clients.Receipt
	failures         int
	receiptCreatedAt string
	events           *workspaceDeleteEvents
}

// applicationGatewayEntry is the destination an installation gateway publishes
// for a revision that exposes a web entry.
func applicationGatewayEntry(revision contracts.WorkspaceApplicationRevision) *contracts.WorkspaceApplicationEntry {
	if port, published := contracts.WorkspaceApplicationEntryPort(revision); published && revision.ExposurePolicy != "cloud_private" {
		return &contracts.WorkspaceApplicationEntry{ServiceName: "app-entry-main", Port: port.Port}
	}
	return nil
}

func newWorkspaceDeleteCompletionFixture(t *testing.T) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger) {
	t.Helper()
	return newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), &workspaceDeleteFabric{})
}

// newWorkspaceDeleteRefundFabric models a Fabric owner that has already
// destroyed the Workspace resources and can answer the authoritative readback.
func newWorkspaceDeleteRefundFabric() *workspaceDeleteFabric {
	absent := false
	return &workspaceDeleteFabric{
		destroyed: true, clearObservationsOnDestroy: true,
		// The deletion stage observes absence before the independent refund read.
		// Refund tests may vary that later read without changing the earlier fact.
		storageReadbackResults: []clients.StorageVolume{{
			ID: "storage-alpha", WorkspaceID: "ws-alpha", Provider: "tencent-tke", ProviderResourceID: "disk-alpha",
			Status: "external_deleted", CBSStatus: contracts.WorkspaceDeleteProviderStatusNotFound, BindingPresent: &absent,
		}},
		storageDestroyResourceID:  "disk-alpha",
		storageProviderResourceID: "disk-alpha",
		storageReadProvider:       "tencent-tke",
		storageReadStatus:         "external_deleted",
		storageReadCBSStatus:      contracts.WorkspaceDeleteProviderStatusNotFound,
		storageBindingPresent:     &absent,
		computeReadProvider:       "tencent-tke",
		computeReadCVMStatus:      contracts.WorkspaceDeleteProviderStatusNotFound,
		computeReadTKEStatus:      contracts.WorkspaceDeleteProviderStatusNotFound,
		computeReadMachinePresent: &absent,
		computeReadMachineName:    "machine-alpha",
		computeReadInstanceID:     "ins-alpha",
	}
}

func newWorkspaceDeleteCompletionFixtureWith(t *testing.T, store controlPlaneTableStore, fabric *workspaceDeleteFabric) (workspaceDeleteFixture, *workspaceDeleteSub2API, *workspaceDeleteLedger) {
	t.Helper()
	sub2API := &workspaceDeleteSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000_000, charges: map[string]int64{}},
		userID:            41, keyID: 19, keyExists: true, history: map[string]clients.Sub2APIBalanceHistoryEntry{},
	}
	ledger := &workspaceDeleteLedger{}
	service := controlplane.NewService(ledger, fabric, sub2API)
	fixture := newWorkspaceDeleteFixtureWithService(t, store, fabric, service)
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "workspace-launch-alpha", "acct-alpha", stringValue(fixture.workspace["ownerUserId"]), 41
	command.WorkspaceID, command.Name = "ws-alpha", "Alpha"
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	fixtureClock := time.Now().UTC()
	periodStart, paidThrough := workspaceDeleteFixtureBillingPeriod(fixtureClock)
	fixture.billingPeriodStart = periodStart
	// The fixture's refund window must stay strictly inside the platform's
	// monthly baseline, with margin on both ends.
	assertWorkspaceDeleteFixtureRefundWindow(t, periodStart, fixtureClock)
	ledger.receiptCreatedAt = fixtureClock.Format(time.RFC3339Nano)
	readyFacts := map[string]any{
		"sub2apiRedeemCode": "opl:workspace-purchase-alpha",
		"workspaceApiKeyId": int64(19), "workspaceKeyStatus": workspaceKeyCodexGroupBound, "workspaceKeyFingerprint": "sha256:" + strings.Repeat("a", 64),
		"chargeAttempted": true, "chargeConfirmation": map[string]any{"code": "opl:workspace-purchase-alpha", "userId": int64(41), "chargeUsdMicros": int64(52_580_000), "status": "used"},
		"postChargeBalanceUsdMicros": int64(947_420_000), "postChargeBalanceKnown": true, "billingPeriodState": "frozen",
		"periodStart": periodStart.Format(time.RFC3339Nano), "paidThrough": paidThrough.Format(time.RFC3339Nano), "billingAnchorDay": periodStart.Day(),
		"computeAllocationId": "compute-alpha", "computeBindingRef": "workspace-launch-alpha:compute", "storageId": "storage-alpha", "storageBindingRef": "workspace-launch-alpha:storage",
		"attachmentId": "attachment-alpha", "attachmentBindingRef": "workspace-launch-alpha:attachment",
		"gatewaySecretRef": "opl-gateway-ws-alpha", "gatewaySecretVersion": "v1", "secretBindingRef": "workspace-launch-alpha:secret",
		"runtimeId": "runtime-alpha", "runtimeReady": true, "runtimeServiceName": "runtime-alpha", "runtimeBindingRef": "workspace-launch-alpha:runtime",
		"url": "https://workspace.example/alpha", "runtimeUsername": "opl", "credentialStatus": "configured", "credentialVersion": "v1", "credentialSecretRef": "runtime-secret-alpha",
		"activationOperationId": "workspace-launch-alpha:activation", "workspaceActivatedAt": periodStart.Format(time.RFC3339Nano),
		"receiptId": "receipt-purchase-alpha", "receiptOperationId": "workspace-launch-alpha:purchase-receipt",
	}
	for key, value := range readyFacts {
		operation.raw[key], _ = json.Marshal(value)
	}
	operation.Stage, operation.Status = "succeeded", "succeeded"
	launchRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), launchRow); err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	workspace["purchaseReceiptId"] = "receipt-purchase-alpha"
	workspace["sub2apiUserId"], workspace["sub2apiRedeemCode"] = int64(41), "opl:workspace-purchase-alpha"
	purchaseInput, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		t.Fatal(err)
	}
	ledger.purchase = clients.Receipt{ReceiptInput: purchaseInput, ReceiptID: "receipt-purchase-alpha"}
	usedBy := int64(41)
	usedAt := periodStart
	sub2API.history["opl:workspace-purchase-alpha"] = clients.Sub2APIBalanceHistoryEntry{
		Code: "opl:workspace-purchase-alpha", Type: "balance", ValueUSDMicros: -52_580_000, Status: "used", UsedBy: &usedBy, UsedAt: &usedAt,
	}
	if err := fixture.store.DeleteWorkspace(context.Background(), "ws-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveCompute(context.Background(), map[string]any{
		"id": "compute-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveStorage(context.Background(), map[string]any{
		"id": "storage-alpha", "accountId": "acct-alpha", "ownerUserId": fixture.workspace["ownerUserId"], "workspaceId": "ws-alpha", "status": "available",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.SaveAttachment(context.Background(), map[string]any{
		"id": "attachment-alpha", "accountId": "acct-alpha", "workspaceId": "ws-alpha", "computeAllocationId": "compute-alpha", "storageId": "storage-alpha", "volumeId": "storage-alpha", "status": "attached",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.workspace = workspace
	return fixture, sub2API, ledger
}

func (f *workspaceDeleteFabric) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// The runtime observation read is a provider read that must never drive the
// deletion forward, so the fake answers it with an empty, well-formed result.
func (f *workspaceDeleteFabric) RuntimeObservations(_ context.Context) (contracts.RuntimeObservations, error) {
	// The typed readback requires a non-nil item list, not a missing one.
	return contracts.RuntimeObservations{ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Items: []contracts.RuntimeObservation{}}, nil
}

// workspaceDeleteOperationRowForTest encodes a delete operation into the runtime
// operation row the retained reader decodes. The retired delete command was the
// only production writer of this row; the reader keeps the persisted format alive,
// so the fixture writes the same shape the command used to write.
func workspaceDeleteOperationRowForTest(operation workspaceDeleteOperation) map[string]any {
	encoded, _ := json.Marshal(operation)
	return map[string]any{
		"id": operation.OperationID, "operationId": operation.OperationID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceId": operation.WorkspaceID, "resourceKind": "workspace", "action": workspaceDeleteAction, "status": operation.Status, "result": string(encoded),
		"computeAllocationId": operation.ComputeID, "storageId": operation.StorageID, "attachmentId": operation.AttachmentID, "runtimeId": operation.RuntimeID,
		"createdAt": operation.CreatedAt,
	}
}

// seedWorkspaceDeleteOperationForTest persists a delete operation in the state the
// deletion execution left behind. The execution is retired, so the retained reader
// is exercised against a recorded operation instead of a live one.
func seedWorkspaceDeleteOperationForTest(t *testing.T, fixture workspaceDeleteFixture, mutate func(*workspaceDeleteOperation)) workspaceDeleteOperation {
	t.Helper()
	operation := workspaceDeleteOperation{
		SchemaVersion: 2, OperationID: workspaceDeleteOperationID("ws-alpha"), AccountID: "acct-alpha",
		OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceID: "ws-alpha",
		ResourceType: "workspace", ResourceID: "ws-alpha", LaunchOperationID: "workspace-launch-alpha", LaunchReceiptID: "receipt-purchase-alpha",
		RuntimeID: "runtime-alpha", RuntimeServiceName: "runtime-alpha", ComputeID: "compute-alpha", StorageID: "storage-alpha", AttachmentID: "attachment-alpha",
		WorkspaceAPIKeyID: 19, GatewaySecretRef: "opl-gateway-ws-alpha", GatewayFingerprint: "sha256:" + strings.Repeat("a", 64),
		Phase: "claimed", Status: "running", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if mutate != nil {
		mutate(&operation)
	}
	operation.RequestHash = workspaceDeleteRequestHash(operation)
	if !validWorkspaceDeleteIdentity(operation) {
		t.Fatalf("seeded delete operation is not a valid operation: %#v", operation)
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), workspaceDeleteOperationRowForTest(operation)); err != nil {
		t.Fatal(err)
	}
	return operation
}

func mustWorkspaceDeleteOperation(t *testing.T, fixture workspaceDeleteFixture) workspaceDeleteOperation {
	t.Helper()
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), workspaceDeleteOperationID("ws-alpha"))
	if err != nil || !found {
		t.Fatalf("delete operation found=%v err=%v", found, err)
	}
	operation, err := decodeWorkspaceDeleteOperation(row)
	if err != nil {
		t.Fatalf("decode delete operation: %v", err)
	}
	return operation
}

// The deletion status is a platform projection: a stage from the frozen
// vocabulary, a page state, a stable reason and the scheduled retry. Console
// reads these instead of interpreting the durable phase.

func seedWorkspaceDeleteStorageProviderResourceID(t *testing.T, fixture workspaceDeleteFixture, providerResourceID string) {
	t.Helper()
	row, found, err := fixture.store.GetStorage(context.Background(), "storage-alpha")
	if err != nil || !found {
		t.Fatalf("storage row found=%v err=%v", found, err)
	}
	row["providerResourceId"] = providerResourceID
	if err := fixture.store.SaveStorage(context.Background(), row); err != nil {
		t.Fatal(err)
	}
}

func newWorkspaceLaunchMonthlyPreflightFixture(t *testing.T, failureMode string) (http.Handler, *memoryTableStore, *workspaceLaunchMonthlyPreflightSub2API, *workspaceLaunchMonthlyPreflightFabric, *[]string) {
	t.Helper()
	// These cases prove the platform billing sequence, independent of the shell
	// environment used to run the package.
	t.Setenv("OPL_DEPLOYMENT_MODE", string(deploymentPlatformOwned))
	events := []string{}
	client := &workspaceLaunchMonthlyPreflightSub2API{
		testSub2APIClient: &testSub2APIClient{
			balance: 100_000_000, charges: map[string]int64{},
			identities: map[string]clients.Sub2APIIdentity{"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"}},
			passwords:  map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
		},
		events: &events,
	}
	fabric := &workspaceLaunchMonthlyPreflightFabric{
		gatewayAccountingFabric: newGatewayAccountingFabric(),
		events:                  &events,
		failureMode:             failureMode,
	}
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	ledger := &workspaceLaunchMonthlyPreflightLedger{receipts: map[string]clients.Receipt{}}
	fabric.receiptLedger = ledger
	server, err := NewPersistentServer(controlplane.NewService(ledger, fabric, client), store)
	if err != nil {
		t.Fatal(err)
	}
	return server, store, client, fabric, &events
}

type workspaceLaunchMonthlyPreflightSub2API struct {
	*testSub2APIClient
	events *[]string
	keys   []clients.Sub2APIWorkspaceKey
}

type workspaceLaunchMonthlyPreflightFabric struct {
	*gatewayAccountingFabric
	packages              []clients.FabricWorkspacePackage
	events                *[]string
	failureMode           string
	runtimePending        bool
	runtimeReady          bool
	runtimeEnsureCalls    int
	runtimeReadCalls      int
	receiptLedger         *workspaceLaunchMonthlyPreflightLedger
	providerProfileRef    string
	monthlyPreflightZones []string
}

type workspaceLaunchMonthlyPreflightLedger struct {
	fakeLedgerClient
	mu       sync.Mutex
	receipts map[string]clients.Receipt
	writes   int
}

type workspaceLaunchActivationCountingStore struct {
	*memoryTableStore
	activationMutations int
}

func newWorkspaceDeleteFixtureWithService(t *testing.T, store controlPlaneTableStore, fabric *workspaceDeleteFabric, service *controlplane.Service) workspaceDeleteFixture {
	t.Helper()
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	handler := server.(*controlPlaneHTTPHandler)
	users, err := handler.app.tables.ListUsers(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	var owner map[string]any
	for _, user := range users {
		if stringValue(user["accountId"]) == "acct-alpha" {
			owner = user
			break
		}
	}
	if owner == nil {
		t.Fatal("tenant owner missing")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	workspace := map[string]any{
		"id": "ws-alpha", "accountId": "acct-alpha", "ownerAccountId": "acct-alpha", "ownerUserId": owner["id"],
		"state": "running", "status": "running", "currentComputeAllocationId": "compute-alpha", "storageId": "storage-alpha",
		"currentAttachmentId": "attachment-alpha", "runtimeId": "runtime-alpha", "createdAt": now, "updatedAt": now,
	}
	if err := handler.app.tables.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	return workspaceDeleteFixture{server: server, store: handler.app.tables, session: session, fabric: fabric, workspace: workspace}
}

// workspaceDeleteFixtureBillingPeriod returns the paid period of a Workspace
// purchased at a fixed age before the clock. The paid period is the calendar
// month the purchase started, computed exactly like the billing owner computes
// it, so the fixture's anchor day stays consistent with its own period.
func workspaceDeleteFixtureBillingPeriod(clock time.Time) (time.Time, time.Time) {
	periodStart := clock.UTC().Add(-workspaceDeleteFixturePurchaseAge)
	return periodStart, nextBillingMonth(periodStart, periodStart.Day())
}

// assertWorkspaceDeleteFixtureRefundWindow refuses a fixture whose refund
// window sits on an edge of the platform's monthly baseline. The window is
// measured with the platform's own billing formula, so a drifted fixture fails
// here with a named cause instead of letting a later assertion report a
// missing refund.

// assertWorkspaceDeleteFixtureRefundWindow refuses a fixture whose refund
// window sits on an edge of the platform's monthly baseline. The window is
// measured with the platform's own billing formula, so a drifted fixture fails
// here with a named cause instead of letting a later assertion report a
// missing refund.
func assertWorkspaceDeleteFixtureRefundWindow(t *testing.T, periodStart, deletedAt time.Time) {
	t.Helper()
	usedHours, refundHours := workspaceRefundBilledHours(periodStart, deletedAt)
	if usedHours <= 0 || refundHours <= workspaceDeleteFixtureRefundMarginHours ||
		refundHours >= int64(contracts.WorkspaceRefundMonthlyHours)-workspaceDeleteFixtureRefundMarginHours {
		t.Fatalf("refund fixture drifted to the edge of the monthly refund window: usedHours=%d refundHours=%d monthly=%d marginHours=%d periodStart=%s deletedAt=%s",
			usedHours, refundHours, contracts.WorkspaceRefundMonthlyHours, workspaceDeleteFixtureRefundMarginHours, periodStart, deletedAt)
	}
}

// The delete fixture models a Workspace that was purchased a fixed age before
// the fixture clock, so its paid period and its refund window are a fixed
// distance from the deletion moment.
//
// A calendar-anchored period cannot do this: the purchase then sits anywhere
// between 0 and 31 days in the past, which lands on the edge of the platform's
// 720-hour monthly refund baseline twice a month. On those days the refund is
// correctly zero, the refund is not dispatched, and the suite fails on a
// calendar date instead of on the behaviour under test.
const workspaceDeleteFixturePurchaseAge = 15 * 24 * time.Hour

// The refund window of a delete fixture must stay this many hours away from
// both ends of the monthly baseline. A fixture that drifts onto an edge makes
// the refund either expire or cover the whole order, and the failure that
// follows is a vague "no refund was dispatched" instead of a named cause.

// The refund window of a delete fixture must stay this many hours away from
// both ends of the monthly baseline. A fixture that drifts onto an edge makes
// the refund either expire or cover the whole order, and the failure that
// follows is a vague "no refund was dispatched" instead of a named cause.
const workspaceDeleteFixtureRefundMarginHours = 24

// workspaceDeleteFixtureBillingPeriod returns the paid period of a Workspace
// purchased at a fixed age before the clock. The paid period is the calendar
// month the purchase started, computed exactly like the billing owner computes
// it, so the fixture's anchor day stays consistent with its own period.

func seedD4RenewalRuntime(t *testing.T, store controlPlaneTableStore, workspace map[string]any, fabric *monthlyFabric) {
	t.Helper()
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-monthly", stringValue(workspace["accountId"]), stringValue(workspace["ownerUserId"])
	command.WorkspaceID = stringValue(workspace["id"])
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": workspace["computeAllocationId"], "storageId": workspace["storageId"], "attachmentId": workspace["currentAttachmentId"], "runtimeId": "runtime-workspace-monthly", "runtimeServiceName": "opl-runtime-monthly"} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		if stage == contracts.StageRuntime {
			facts["runtimeBindingRef"] = operation.ID + ":runtime"
		}
		if stage == contracts.StageActivation {
			facts["activationOperationId"] = operation.ID + ":activation"
		}
		if stage == contracts.StageReceipt {
			facts["receiptOperationId"] = operation.ID + ":purchase-receipt"
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed renewal Runtime stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	operation.Version = len(workspaceLaunchReconcileStages)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	workspace["runtimeId"] = "runtime-workspace-monthly"
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	fabric.runtimePowerIdentity = contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: command.AccountID, WorkspaceID: command.WorkspaceID, RuntimeID: "runtime-workspace-monthly", RuntimeOperationID: operation.stringFact("runtimeBindingRef")}
	fabric.runtimePowerState = "running"
}

func (fabric *monthlyFabric) ReadWorkspaceRuntimePower(_ context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	fabric.runtimePowerReads = append(fabric.runtimePowerReads, input)
	if err := fabric.validateRuntimePower(input); err != nil {
		return contracts.WorkspaceRuntimePowerResult{}, err
	}
	return contracts.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: fabric.runtimePowerState}, nil
}

func (fabric *monthlyFabric) SetWorkspaceRuntimePower(_ context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	if err := fabric.validateRuntimePower(input); err != nil {
		return contracts.WorkspaceRuntimePowerResult{}, err
	}
	fabric.runtimePowerCalls = append(fabric.runtimePowerCalls, input)
	if fabric.runtimePowerErr != nil {
		return contracts.WorkspaceRuntimePowerResult{}, fabric.runtimePowerErr
	}
	fabric.runtimePowerState = input.DesiredState
	return contracts.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: fabric.runtimePowerState}, nil
}

func (fabric *monthlyFabric) validateRuntimePower(input contracts.WorkspaceRuntimePowerInput) error {
	binding := fabric.runtimePowerIdentity
	if binding.RuntimeID == "" || input.SchemaVersion != 1 || input.AccountID != binding.AccountID || input.WorkspaceID != binding.WorkspaceID || input.RuntimeID != binding.RuntimeID || input.RuntimeOperationID != binding.RuntimeOperationID || input.IdempotencyKey == "" || input.PaidThrough == "" || input.DesiredState != "running" && input.DesiredState != "suspended" {
		return errors.New("runtime_power_identity_mismatch")
	}
	if _, err := time.Parse(time.RFC3339, input.PaidThrough); err != nil {
		return err
	}
	return nil
}
