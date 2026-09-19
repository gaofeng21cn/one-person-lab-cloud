package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

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

func deleteWorkspaceRefundForTest(t *testing.T, fixture workspaceDeleteFixture, key string) *httptest.ResponseRecorder {
	t.Helper()
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, key)
	if response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	return response
}

func TestWorkspaceDeleteRefundDispatchesOnceWithFullAuthoritativeReadback(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-refund-full-readback")

	if len(sub2API.refunds) != 1 {
		t.Fatalf("platform refunds=%#v", sub2API.refunds)
	}
	refund := sub2API.refunds[0]
	fulfilled := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	expected, err := contracts.PlatformWorkspaceDeleteRefundMicros(52_580_000, fulfilled, time.Now().UTC())
	if err != nil || refund.RefundUSDMicros != expected || refund.UserID != 41 {
		t.Fatalf("refund=%#v expected=%d err=%v", refund, expected, err)
	}
	if refund.Code != walletAdjustmentRedeemCode(workspaceDeleteRefundWalletOperationID(workspaceDeleteOperationID("ws-alpha"))) {
		t.Fatalf("refund code=%q is not the deterministic operation code", refund.Code)
	}

	record, found, err := fixture.server.(*controlPlaneHTTPHandler).app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || record.Status != workspaceDeleteRefundStatusSucceeded || record.ReasonCode != "" ||
		record.PolicyVersion != contracts.WorkspaceRefundPolicyVersion || record.OriginalChargeUSDMicros != 52_580_000 ||
		record.RefundUSDMicros != expected || record.OriginalChargeCode != "opl:workspace-purchase-alpha" ||
		record.LaunchOperationID != "workspace-launch-alpha" || record.WalletAdjustmentOperationID == "" || record.RefundReceiptID == "" ||
		record.ReadbackID == "" || record.ReadbackProvider != "tencent-tke" {
		t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
	}

	// A replayed deletion request and a worker pass must never dispatch a second refund.
	deleteWorkspaceRefundForTest(t, fixture, "delete-refund-full-readback")
	if err := fixture.server.(*controlPlaneHTTPHandler).app.runWorkspaceDeletesOnce(context.Background(), fixture.server.(*controlPlaneHTTPHandler).service); err != nil {
		t.Fatal(err)
	}
	if len(sub2API.refunds) != 1 {
		t.Fatalf("replayed platform refunds=%#v", sub2API.refunds)
	}
	refundReceipts := 0
	for _, receipt := range ledger.receipts {
		if receipt.Type == "gateway.wallet_adjustment.v1" {
			refundReceipts++
		}
	}
	if refundReceipts != 1 {
		t.Fatalf("ledger refund receipts=%#v", ledger.receipts)
	}
}

func TestWorkspaceDeleteRefundRefusesEveryIncompleteReadback(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		mutate     func(*workspaceDeleteFabric)
		wantReason string
	}{
		{"cvm only shut down", func(f *workspaceDeleteFabric) {
			present := true
			f.computeReadCVMStatus, f.computeReadMachinePresent = "SHUTDOWN", &present
		}, contracts.WorkspaceDeleteGateResourcePresent},
		{"cbs still attached", func(f *workspaceDeleteFabric) {
			f.storageReadStatus, f.storageReadCBSStatus = "ready", "ATTACHED"
		}, contracts.WorkspaceDeleteGateResourcePresent},
		{"mount binding still present", func(f *workspaceDeleteFabric) {
			present := true
			f.storageBindingPresent = &present
		}, contracts.WorkspaceDeleteGateResourcePresent},
		{"single resource reported absent only", func(f *workspaceDeleteFabric) {
			f.storageReadCBSStatus = ""
		}, contracts.WorkspaceDeleteGateProviderUnconfirmed},
		{"provider readback unavailable", func(f *workspaceDeleteFabric) {
			f.storageReadErr = errWorkspaceDeleteUnconfirmed
		}, contracts.WorkspaceDeleteGateReadbackUnavailable},
		{"different cbs disk than the deleted workspace", func(f *workspaceDeleteFabric) {
			f.storageProviderResourceID = "disk-other"
		}, contracts.WorkspaceDeleteGateReadbackIdentity},
		{"provider identity mismatch", func(f *workspaceDeleteFabric) {
			f.mismatchStage = "storage-read"
		}, contracts.WorkspaceDeleteGateReadbackIdentity},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			testCase.mutate(fabric)
			fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			deleteWorkspaceRefundForTest(t, fixture, "delete-refund-refused-"+stableID(testCase.name))
			if len(sub2API.refunds) != 0 {
				t.Fatalf("refund dispatched for refused state: %#v", sub2API.refunds)
			}
			record, found, err := fixture.server.(*controlPlaneHTTPHandler).app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
			if err != nil || !found || record.Status != workspaceDeleteRefundStatusBlocked || record.RefundUSDMicros != 0 {
				t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
			}
		})
	}
}

func TestWorkspaceDeleteRefundNeverDispatchesForUnfinishedDeletion(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*workspaceDeleteFabric)
	}{
		{"pending deletion", func(f *workspaceDeleteFabric) {
			f.computeReads = []string{"destroying", "destroying"}
		}},
		{"deletion awaiting manual review", func(f *workspaceDeleteFabric) {
			f.mismatchStage = "attachment"
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			testCase.mutate(fabric)
			fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			handler := fixture.server.(*controlPlaneHTTPHandler)
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-refund-"+stableID(testCase.name))
			if response.Code == http.StatusOK {
				t.Fatalf("fixture completed the deletion: %s", response.Body.String())
			}
			operation := mustWorkspaceDeleteOperation(t, fixture)
			if operation.Phase == "complete" || operation.Status == "succeeded" {
				t.Fatalf("unfinished deletion=%#v", operation)
			}
			if err := handler.app.runWorkspaceDeleteRefund(context.Background(), handler.service, operation); err != nil {
				t.Fatal(err)
			}
			if len(sub2API.refunds) != 0 {
				t.Fatalf("refund dispatched for %s: %#v", testCase.name, sub2API.refunds)
			}
			record, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
			if err != nil || !found || record.Status != workspaceDeleteRefundStatusBlocked || record.ReasonCode != contracts.WorkspaceDeleteGateOperationIncomplete {
				t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
			}
		})
	}
}

func TestWorkspaceDeleteRefundNeverReadHidesLegacyOperationWithoutProviderIdentity(t *testing.T) {
	// A Delete operation that never recorded the provider identity cannot bind a
	// refund. Control Plane refuses without reading, and never refunds.
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageReadErr = errWorkspaceDeleteUnconfirmed
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-refund-legacy-identity")
	operation := mustWorkspaceDeleteOperation(t, fixture)
	operation.StorageProviderResourceID = ""
	fabric.storageReadErr = nil
	handler := fixture.server.(*controlPlaneHTTPHandler)
	callsBefore := len(fabric.recordedCalls())
	if err := handler.app.runWorkspaceDeleteRefund(context.Background(), handler.service, operation); err != nil {
		t.Fatal(err)
	}
	if len(fabric.recordedCalls()) != callsBefore {
		t.Fatal("identity-incomplete retained operation caused provider reads")
	}
	if len(sub2API.refunds) != 0 {
		t.Fatalf("legacy deletion refunded: %#v", sub2API.refunds)
	}
	record, found, err := fixture.server.(*controlPlaneHTTPHandler).app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || record.Status != workspaceDeleteRefundStatusBlocked || record.ReasonCode != contracts.WorkspaceDeleteGateOperationIncomplete {
		t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
	}
}

func TestWorkspaceDeleteRefundRecoversLostResponseWithoutSecondDispatch(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	sub2API.mu.Lock()
	sub2API.refundResponseLost = true
	sub2API.mu.Unlock()
	deleteWorkspaceRefundForTest(t, fixture, "delete-refund-lost-response")

	// The wallet refund was dispatched, but its response was lost. The refund must
	// stay unresolved instead of being reported as a completed refund.
	if len(sub2API.refunds) != 1 {
		t.Fatalf("dispatched refunds=%#v", sub2API.refunds)
	}
	record, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || record.Status == workspaceDeleteRefundStatusSucceeded {
		t.Fatalf("lost response recorded as %#v found=%v err=%v", record, found, err)
	}

	// Recovery queries the original refund operation instead of dispatching again.
	sub2API.mu.Lock()
	sub2API.refundResponseLost = false
	sub2API.mu.Unlock()
	if err := handler.app.runWorkspaceDeleteRefund(context.Background(), handler.service, mustWorkspaceDeleteOperation(t, fixture)); err != nil {
		t.Fatal(err)
	}
	if len(sub2API.refunds) != 1 {
		t.Fatalf("recovery dispatched a second refund: %#v", sub2API.refunds)
	}
	record, found, err = handler.app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || record.Status != workspaceDeleteRefundStatusSucceeded || record.RefundReceiptID == "" {
		t.Fatalf("recovered refund record=%#v found=%v err=%v", record, found, err)
	}
}

func TestWorkspaceDeleteRefundZeroRefundHoursIsNotDispatched(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	// The original Launch fulfilled more than one monthly baseline ago, so the
	// platform keeps the full monthly charge and dispatches no refund.
	setWorkspaceLaunchActivatedAt(t, fixture, time.Now().UTC().Add(-900*time.Hour))

	deleteWorkspaceRefundForTest(t, fixture, "delete-refund-not-due")
	if len(sub2API.refunds) != 0 {
		t.Fatalf("refund dispatched without refundable hours: %#v", sub2API.refunds)
	}
	record, found, err := handler.app.workspaceDeleteRefundRecord(context.Background(), "ws-alpha")
	if err != nil || !found || record.Status != workspaceDeleteRefundStatusNotDue || record.RefundUSDMicros != 0 ||
		record.UsedHours <= int64(contracts.WorkspaceRefundMonthlyHours) || record.RefundHours != 0 {
		t.Fatalf("refund record=%#v found=%v err=%v", record, found, err)
	}
}

func setWorkspaceLaunchActivatedAt(t *testing.T, fixture workspaceDeleteFixture, activatedAt time.Time) {
	t.Helper()
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), "workspace-launch-alpha")
	if err != nil || !found {
		t.Fatalf("launch operation found=%v err=%v", found, err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &result); err != nil {
		t.Fatal(err)
	}
	result["workspaceActivatedAt"] = activatedAt.UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	row["result"] = string(payload)
	if err := fixture.store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
}

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
func TestWorkspaceDeletionStatusProjectsStageAndPageState(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		mutate        func(*workspaceDeleteFabric)
		seed          func(*testing.T, workspaceDeleteFixture)
		wantState     string
		wantStatus    string
		wantStage     string
		wantReason    string
		wantNextRetry bool
	}{
		{ // Still converging: no failure was recorded, so the honest projection is
			// that the platform is still waiting for this stage's evidence.
			name: "storage deletion still converging",
			mutate: func(f *workspaceDeleteFabric) {
				f.storageResults = []workspaceDeleteStorageResult{{Status: "ready", DestroyState: "pending_retry"}}
			},
			wantState: contracts.WorkspaceDeletePageStateWaiting, wantStatus: "pending",
			wantStage: contracts.WorkspaceDeleteStageStorageAbsent},
		{ // A provider identity conflict stops the operation and needs a human.
			name: "storage identity conflict",
			mutate: func(f *workspaceDeleteFabric) {
				f.storageDestroyResourceID = "disk-other"
			},
			seed: func(t *testing.T, fixture workspaceDeleteFixture) {
				seedWorkspaceDeleteStorageProviderResourceID(t, fixture, "disk-alpha")
			},
			wantState: contracts.WorkspaceDeletePageStateBlocked, wantStatus: "manual_review", wantReason: "fabric_storage_identity_conflict",
			wantStage: contracts.WorkspaceDeleteStageStorageAbsent,
		},
		{ // A scheduled readback makes the same operation retrying.
			name: "compute readback scheduled",
			mutate: func(f *workspaceDeleteFabric) {
				f.computeReadbackResults = []clients.ComputeAllocation{{Status: "present", DestroyState: clients.StorageDestroyStatePendingRetry}}
			},
			wantState: contracts.WorkspaceDeletePageStateRetrying, wantStatus: "pending", wantNextRetry: true,
			wantStage: contracts.WorkspaceDeleteStageComputeAbsent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			if testCase.mutate != nil {
				testCase.mutate(fabric)
			}
			fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			if testCase.seed != nil {
				testCase.seed(t, fixture)
			}
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-stage-"+stableID(testCase.name))
			if response.Code == http.StatusOK {
				t.Fatalf("fixture completed the deletion: %s", response.Body.String())
			}
			status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
			var dto workspaceDeletionStatusDTO
			if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
				t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
			}
			if dto.Status != testCase.wantStatus || dto.PageState != testCase.wantState {
				t.Fatalf("projection dto=%#v want status=%s state=%s", dto, testCase.wantStatus, testCase.wantState)
			}
			if testCase.wantReason != "" && dto.ReasonCode != testCase.wantReason {
				t.Fatalf("reason dto=%#v want=%s", dto, testCase.wantReason)
			}
			// The published stage is the owning stage vocabulary applied to this
			// operation's own confirmed facts, and it must name the stage the
			// operation is actually working on.
			if dto.Stage != testCase.wantStage || dto.Stage == "" {
				t.Fatalf("stage=%q want=%q dto=%#v", dto.Stage, testCase.wantStage, dto)
			}
			if testCase.wantNextRetry && dto.NextRetryAt == "" {
				t.Fatalf("missing scheduled retry dto=%#v", dto)
			}
			// The refund is only evaluated after the deletion completes, so an
			// unfinished deletion must never carry a refund result.
			if dto.RefundStatus != "" || dto.RefundReceiptID != "" || dto.RefundUSDMicros != 0 {
				t.Fatalf("unfinished deletion exposed a refund: %#v", dto)
			}
		})
	}
}

// Every block reason the deletion state machine can persist must be classified
// deliberately: an identity conflict blocks, everything else keeps retrying.
func TestWorkspaceDeleteBlockClassCoversTheStateMachine(t *testing.T) {
	for code := range workspaceDeleteIdentityBlockCodes() {
		if workspaceDeleteBlockClass(code) != workspaceDeleteBlockIdentity {
			t.Fatalf("identity code %q classified as %q", code, workspaceDeleteBlockClass(code))
		}
	}
	for _, code := range []string{"fabric_compute_readback_unavailable", "fabric_storage_unconfirmed", "workspace_delete_inventory_unavailable", ""} {
		if workspaceDeleteBlockClass(code) != workspaceDeleteBlockRetryableReadback {
			t.Fatalf("code %q must stay retryable", code)
		}
	}
	// A blocked page state is reachable only through an identity conflict.
	blocked := workspaceDeleteOperation{Status: "manual_review", Phase: "attachment_absent", LastErrorCode: "fabric_runtime_identity_conflict"}
	if workspaceDeletePageState(blocked) != contracts.WorkspaceDeletePageStateBlocked {
		t.Fatalf("blocked state=%q", workspaceDeletePageState(blocked))
	}
	blocked.LastErrorCode = "fabric_runtime_absence_unconfirmed"
	if workspaceDeletePageState(blocked) != contracts.WorkspaceDeletePageStateRetrying {
		t.Fatalf("retrying state=%q", workspaceDeletePageState(blocked))
	}
	complete := workspaceDeleteOperation{Status: "succeeded", Phase: "complete"}
	if workspaceDeletePageState(complete) != contracts.WorkspaceDeletePageStateCompleted {
		t.Fatalf("completed state=%q", workspaceDeletePageState(complete))
	}
}

func TestWorkspaceDeleteRefundStatusIsReportedSeparatelyFromDeletion(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-refund-status")
	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}
	if dto.Status != "deleted" || dto.ReceiptID == "" {
		t.Fatalf("deletion status dto=%#v", dto)
	}
	if dto.RefundStatus != workspaceDeleteRefundStatusSucceeded || dto.RefundReceiptID == "" || dto.RefundOperationID == "" ||
		dto.RefundUSDMicros != sub2API.refunds[0].RefundUSDMicros || dto.OriginalChargeUSDMicros != 52_580_000 ||
		dto.RefundPolicyVersion != contracts.WorkspaceRefundPolicyVersion {
		t.Fatalf("refund status dto=%#v refunds=%#v", dto, sub2API.refunds)
	}
}

// A converging CBS deletion keeps the retention of the same Delete operation:
// the deletion must stay retryable and later complete without repeating any
// earlier or later stage mutation.
func TestWorkspaceDeleteRetriesWhileStorageDeletionIsConverging(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageResults = []workspaceDeleteStorageResult{
		{Status: "ready", DestroyState: clients.StorageDestroyStatePendingRetry},
		{Status: "external_deleted"},
	}
	fixture, _, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	first := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-storage-converging")
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"status":"pending"`) {
		t.Fatalf("converging status=%d body=%s", first.Code, first.Body.String())
	}
	if _, found, err := fixture.store.GetWorkspace(context.Background(), "ws-alpha"); err != nil || !found {
		t.Fatalf("converging removed Workspace found=%v err=%v", found, err)
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.Phase != "attachment_absent" || operation.Status != "running" || operation.DeletionReceiptID != "" {
		t.Fatalf("converging operation=%#v", operation)
	}
	for _, call := range fabric.recordedCalls() {
		if strings.HasPrefix(call, "compute:") {
			t.Fatalf("converging storage advanced to compute: %#v", fabric.recordedCalls())
		}
	}

	second := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-storage-converging")
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"status":"deleted"`) {
		t.Fatalf("converged status=%d body=%s", second.Code, second.Body.String())
	}
	storageMutations, computeMutations := 0, 0
	for _, call := range fabric.recordedCalls() {
		if strings.HasPrefix(call, "storage:") {
			storageMutations++
		}
		if strings.HasPrefix(call, "compute:") {
			computeMutations++
		}
	}
	deletionReceipts := 0
	for _, receipt := range ledger.receipts {
		if receipt.Type == "workspace.deleted.v1" {
			deletionReceipts++
		}
	}
	if storageMutations != 2 || computeMutations != 1 || deletionReceipts != 1 {
		t.Fatalf("converged storage=%d compute=%d receipts=%d calls=%#v", storageMutations, computeMutations, deletionReceipts, fabric.recordedCalls())
	}
}

// A storage result that names another volume is an identity conflict, never a
// retryable wait.
func TestWorkspaceDeleteRejectsStorageIdentityConflict(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageResults = []workspaceDeleteStorageResult{{Status: "ready", DestroyState: clients.StorageDestroyStatePendingRetry}}
	fabric.storageDestroyResourceID = "disk-other"
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	// The Workspace was launched with its own disk. A destroy result naming another
	// disk must never be accepted, even when it claims to be retryable.
	seedWorkspaceDeleteStorageProviderResourceID(t, fixture, "disk-alpha")
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-storage-identity")
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"status":"manual_review"`) {
		t.Fatalf("identity conflict status=%d body=%s", response.Code, response.Body.String())
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.Status != "manual_review" || operation.LastErrorCode != "fabric_storage_identity_conflict" {
		t.Fatalf("identity conflict operation=%#v", operation)
	}
}

// A provider-classified unfinished compute deletion keeps the same Delete
// operation retryable, and its later absence completes without repeating any
// earlier stage mutation.
func TestWorkspaceDeleteRetriesWhileComputeDeletionIsStillConverging(t *testing.T) {
	absent := false
	present := true
	for _, testCase := range []struct {
		name    string
		class   string
		status  string
		cvm     string
		machine *bool
	}{
		{"machine absent while its cvm terminates", clients.StorageDestroyStateUnconfirmedSend, "present", "SHUTDOWN", &absent},
		{"machine still present", clients.StorageDestroyStatePendingRetry, "present", "", &present},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			fabric.computeReadbackResults = []clients.ComputeAllocation{
				{Status: testCase.status, CVMStatus: testCase.cvm, TKEStatus: "NOT_FOUND", MachinePresent: testCase.machine, DestroyState: testCase.class},
				{Status: "external_deleted", CVMStatus: contracts.WorkspaceDeleteProviderStatusNotFound, TKEStatus: contracts.WorkspaceDeleteProviderStatusNotFound, MachinePresent: &absent},
			}
			fixture, _, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			first := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-compute-converging-"+stableID(testCase.name))
			if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"status":"pending"`) {
				t.Fatalf("converging status=%d body=%s", first.Code, first.Body.String())
			}
			if _, found, err := fixture.store.GetWorkspace(context.Background(), "ws-alpha"); err != nil || !found {
				t.Fatalf("converging removed Workspace found=%v err=%v", found, err)
			}
			operation := mustWorkspaceDeleteOperation(t, fixture)
			if operation.Phase != "storage_absent" || operation.Status != "running" || operation.DeletionReceiptID != "" || operation.LastErrorCode != "" {
				t.Fatalf("converging operation=%#v", operation)
			}
			computeMutations := 0
			for _, call := range fabric.recordedCalls() {
				if strings.HasPrefix(call, "compute:") {
					computeMutations++
				}
			}
			if computeMutations != 1 {
				t.Fatalf("converging compute mutations=%d calls=%#v", computeMutations, fabric.recordedCalls())
			}

			expireWorkspaceDeleteComputeReadback(t, fixture.store)
			second := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-compute-converging-"+stableID(testCase.name))
			if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"status":"deleted"`) {
				t.Fatalf("converged status=%d body=%s", second.Code, second.Body.String())
			}
			computeMutations = 0
			deletionReceipts := 0
			for _, call := range fabric.recordedCalls() {
				if strings.HasPrefix(call, "compute:") {
					computeMutations++
				}
			}
			for _, receipt := range ledger.receipts {
				if receipt.Type == "workspace.deleted.v1" {
					deletionReceipts++
				}
			}
			if computeMutations != 1 || deletionReceipts != 1 {
				t.Fatalf("converged compute=%d receipts=%d calls=%#v", computeMutations, deletionReceipts, fabric.recordedCalls())
			}
		})
	}
}

// A destroy result Fabric already classified as unfinished keeps the same
// operation pending from the first attempt, without arming a second mutation.
func TestWorkspaceDeleteKeepsClassifiedComputeDestroyPending(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.computeDestroyResults = []clients.ComputeAllocation{{Status: "present", DestroyState: clients.StorageDestroyStatePendingRetry}}
	fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-compute-classified-destroy")
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"status":"pending"`) {
		t.Fatalf("classified destroy status=%d body=%s", response.Code, response.Body.String())
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.Phase != "storage_absent" || operation.Status != "running" || operation.ComputeStatus != "destroying" ||
		operation.MaxComputeReadbacks != workspaceDeleteComputeReadbackBudget || operation.ComputeReadbacks != 1 || operation.LastErrorCode != "" {
		t.Fatalf("classified destroy operation=%#v", operation)
	}
	if len(sub2API.refunds) != 0 {
		t.Fatalf("classified destroy refunded: %#v", sub2API.refunds)
	}
	computeMutations := 0
	for _, call := range fabric.recordedCalls() {
		if strings.HasPrefix(call, "compute:") {
			computeMutations++
		}
	}
	if computeMutations != 1 {
		t.Fatalf("classified destroy mutations=%d calls=%#v", computeMutations, fabric.recordedCalls())
	}
}

// An unverifiable compute readback and a compute identity conflict stay
// reviewable; neither may be reported as a completed deletion.
func TestWorkspaceDeleteKeepsUnverifiableComputeReadbackReviewable(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		mutate     func(*workspaceDeleteFabric)
		wantReason string
	}{
		{"provider readback unavailable", func(f *workspaceDeleteFabric) {
			f.computeReadErr = errWorkspaceDeleteUnconfirmed
		}, "fabric_compute_readback_unavailable"},
		{"compute identity conflict", func(f *workspaceDeleteFabric) {
			f.mismatchStage = "compute-read"
		}, "fabric_compute_identity_conflict"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			testCase.mutate(fabric)
			fixture, sub2API, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-compute-review-"+stableID(testCase.name))
			if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"status":"manual_review"`) {
				t.Fatalf("reviewable status=%d body=%s", response.Code, response.Body.String())
			}
			if len(sub2API.refunds) != 0 {
				t.Fatalf("reviewable compute refunded: %#v", sub2API.refunds)
			}
			operation := mustWorkspaceDeleteOperation(t, fixture)
			if operation.Status != "manual_review" || operation.LastErrorCode != testCase.wantReason {
				t.Fatalf("reviewable operation=%#v", operation)
			}
			if _, found, err := fixture.store.GetWorkspace(context.Background(), "ws-alpha"); err != nil || !found {
				t.Fatalf("reviewable removed Workspace found=%v err=%v", found, err)
			}
		})
	}
}
