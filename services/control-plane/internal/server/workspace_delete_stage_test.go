package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// The published stage must name the stage the operation is working on right now.
// The durable phase names the stage that just completed, so a projection driven by
// the phase alone mislabels a long wait such as an in-flight compute termination.
func TestWorkspaceDeleteStageInProgressNamesTheCurrentStage(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		operation workspaceDeleteOperation
		wantStage string
	}{
		{name: "runtime still being released", operation: workspaceDeleteOperation{
			Phase: "claimed"}, wantStage: contracts.WorkspaceDeleteStageRuntimeAbsent},
		{name: "runtime released, mount being detached", operation: workspaceDeleteOperation{
			Phase: "runtime_secret_absent", RuntimeStatus: "absent", SecretStatus: "absent"}, wantStage: contracts.WorkspaceDeleteStageAttachmentAbsent},
		{name: "mount released, disk being destroyed", operation: workspaceDeleteOperation{
			Phase: "attachment_absent", RuntimeStatus: "absent", SecretStatus: "absent", AttachmentStatus: "absent"}, wantStage: contracts.WorkspaceDeleteStageStorageAbsent},
		// The long wait: the disk is already gone and the node is still terminating.
		{name: "compute still terminating while the phase names the disk", operation: workspaceDeleteOperation{
			Phase: "storage_absent", RuntimeStatus: "absent", SecretStatus: "absent", AttachmentStatus: "absent", StorageStatus: "absent", ComputeStatus: "destroying"}, wantStage: contracts.WorkspaceDeleteStageComputeAbsent},
		{name: "compute released, workspace being removed", operation: workspaceDeleteOperation{
			Phase: "compute_absent", RuntimeStatus: "absent", SecretStatus: "absent", AttachmentStatus: "absent", StorageStatus: "absent", ComputeStatus: "absent"}, wantStage: contracts.WorkspaceDeleteStageWorkspaceAbsent},
		{name: "legacy key phase is the compute stage", operation: workspaceDeleteOperation{
			Phase: "key_absent", RuntimeStatus: "absent", SecretStatus: "absent", AttachmentStatus: "absent", StorageStatus: "absent", ComputeStatus: "absent"}, wantStage: contracts.WorkspaceDeleteStageWorkspaceAbsent},
		{name: "workspace removed, receipt being recorded", operation: workspaceDeleteOperation{
			Phase: "workspace_absent", RuntimeStatus: "absent", SecretStatus: "absent", AttachmentStatus: "absent", StorageStatus: "absent", ComputeStatus: "absent"}, wantStage: contracts.WorkspaceDeleteStageReceiptRecorded},
		{name: "deletion complete", operation: workspaceDeleteOperation{
			Phase: "complete", RuntimeStatus: "absent", SecretStatus: "absent", AttachmentStatus: "absent", StorageStatus: "absent", ComputeStatus: "absent", DeletionReceiptID: "receipt-delete-alpha"}, wantStage: contracts.WorkspaceDeleteStageReceiptRecorded},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if stage := workspaceDeleteStageInProgress(testCase.operation); stage != testCase.wantStage {
				t.Fatalf("stage=%q want=%q operation=%#v", stage, testCase.wantStage, testCase.operation)
			}
		})
	}
}

// The deletion status is a pure store read: reading progress never contacts the
// provider, because only the owning worker performs provider reads.
func TestWorkspaceDeletionStatusServesEveryStageWithoutProviderReads(t *testing.T) {
	absent := false
	fabric := newWorkspaceDeleteRefundFabric()
	// The Machine is gone but TKE still reports the node: the deletion is waiting on
	// compute, which is exactly the case a phase-driven projection mislabelled.
	fabric.computeReadbackResults = []clients.ComputeAllocation{
		{Status: "present", TKEStatus: "RUNNING", MachinePresent: &absent, DestroyState: clients.StorageDestroyStatePendingRetry},
	}
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-stage-evidence")
	if response.Code != http.StatusAccepted {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	duringDelete := len(fabric.recordedCalls())

	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}
	if len(fabric.recordedCalls()) != duringDelete {
		t.Fatalf("status read contacted Fabric: before=%d after=%d", duringDelete, len(fabric.recordedCalls()))
	}
	if dto.Stage != contracts.WorkspaceDeleteStageComputeAbsent || dto.PageState != contracts.WorkspaceDeletePageStateRetrying ||
		dto.Phase != "storage_absent" || dto.NextRetryAt == "" || dto.ReasonCode != "" {
		t.Fatalf("dto=%#v", dto)
	}
	// Console renders this label for compute_absent: the wait is described as a
	// compute wait, not as a disk wait.
	if dto.Stage == contracts.WorkspaceDeleteStageStorageAbsent {
		t.Fatal("compute wait mislabelled as a storage wait")
	}

	// Reasoning must not need a provider round trip either: the recorded facts are
	// persisted with the operation the worker already resumed.
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.ComputeStatus != "destroying" || operation.StorageStatus != "absent" || operation.AttachmentStatus != "absent" {
		t.Fatalf("durable operation lacks the facts the projection reads: %#v", operation)
	}
}

// A converging storage deletion is the same kind of read: the status endpoint
// reports the stage and the scheduled retry from store state alone.
func TestWorkspaceDeletionStatusReportsStorageWaitFromStoreState(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fabric.storageResults = []workspaceDeleteStorageResult{{Status: "ready", DestroyState: clients.StorageDestroyStatePendingRetry}}
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-stage-storage")
	if response.Code != http.StatusAccepted {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	before := len(fabric.recordedCalls())
	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}
	if len(fabric.recordedCalls()) != before {
		t.Fatal("status read contacted Fabric")
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	if operation.Phase != "attachment_absent" || dto.Stage != contracts.WorkspaceDeleteStageStorageAbsent {
		t.Fatalf("dto=%#v operation=%#v", dto, operation)
	}
	if dto.PageState != contracts.WorkspaceDeletePageStateWaiting {
		t.Fatalf("page state=%q (no failure was recorded yet)", dto.PageState)
	}
	_ = context.Background()
}

// The admin Runtime detail receives the same persisted deletion facts as the
// customer deletion page: the stage in progress, the page state, the stable reason,
// the most recent readback and the scheduled retry. Reading them is a store read,
// so it never drives the deletion forward.
func TestOperatorRuntimeObservationPublishesDeletionProgress(t *testing.T) {
	absent := false
	present := true
	for _, testCase := range []struct {
		name       string
		mutate     func(*workspaceDeleteFabric)
		wantStatus string
		wantReason string
		wantStage  string
		wantState  string
		wantRetry  bool
	}{
		{
			name: "compute still terminating",
			mutate: func(f *workspaceDeleteFabric) {
				f.computeReadbackResults = []clients.ComputeAllocation{{Status: "present", TKEStatus: "RUNNING", MachinePresent: &absent, DestroyState: clients.StorageDestroyStatePendingRetry}}
			},
			wantStatus: "pending", wantReason: "workspace_delete_in_progress",
			wantStage: contracts.WorkspaceDeleteStageComputeAbsent, wantState: contracts.WorkspaceDeletePageStateRetrying, wantRetry: true,
		},
		{
			name: "storage identity conflict",
			mutate: func(f *workspaceDeleteFabric) {
				_ = present
				f.storageDestroyResourceID = "disk-other"
			},
			wantStatus: "attention", wantReason: "workspace_delete_identity_conflict",
			wantStage: contracts.WorkspaceDeleteStageStorageAbsent, wantState: contracts.WorkspaceDeletePageStateBlocked,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			testCase.mutate(fabric)
			fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			if testCase.name == "storage identity conflict" {
				seedWorkspaceDeleteStorageProviderResourceID(t, fixture, "disk-alpha")
			}
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "operator-delete-"+stableID(testCase.name))
			if response.Code == http.StatusOK {
				t.Fatalf("fixture completed the deletion: %s", response.Body.String())
			}
			handler := fixture.server.(*controlPlaneHTTPHandler)
			before := len(fabric.recordedCalls())
			observations, err := handler.app.operatorRuntimeObservations(context.Background(), handler.service, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if len(fabric.recordedCalls()) != before {
				t.Fatalf("operator observation drove the deletion forward: before=%d after=%d", before, len(fabric.recordedCalls()))
			}
			var item *operatorRuntimeObservation
			for index := range observations.Items {
				if observations.Items[index].WorkspaceID == "ws-alpha" {
					item = &observations.Items[index]
				}
			}
			if item == nil {
				t.Fatalf("workspace missing from observations: %#v", observations.Items)
			}
			if item.Status != testCase.wantStatus || item.ReasonCode != testCase.wantReason {
				t.Fatalf("operator status=%q reason=%q want=%q/%q", item.Status, item.ReasonCode, testCase.wantStatus, testCase.wantReason)
			}
			if item.DeleteStage != testCase.wantStage || item.DeletePageState != testCase.wantState || item.DeleteLastReadbackAt == "" {
				t.Fatalf("operator deletion progress=%#v", item)
			}
			if _, err := time.Parse(time.RFC3339Nano, item.DeleteLastReadbackAt); err != nil {
				t.Fatalf("last readback time is not a real time: %#v", item.DeleteLastReadbackAt)
			}
			if testCase.wantRetry && item.DeleteNextRetryAt == "" {
				t.Fatalf("missing scheduled retry: %#v", item)
			}
			// The generic reason the plan replaces must be gone: a stalled deletion is
			// now named by its stage and its stable cause.
			if item.ReasonCode == "workspace_delete_incomplete" {
				t.Fatalf("deletion collapsed into the generic reason: %#v", item)
			}
		})
	}
}
