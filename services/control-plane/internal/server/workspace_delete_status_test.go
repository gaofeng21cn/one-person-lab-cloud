package server

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// The delete execution engine is retired, so the retained surfaces are read from
// the durable record: the historical decoders, the customer-visible deletion
// status and the operator projection. The fixtures below seed that record
// directly, because no provider round trip advances a Delete operation any more.

// seedWorkspaceDeleteOperation rewrites the persisted Delete operation with the
// facts a completed stage would have recorded. The request hash does not cover
// the phase, so a seeded stage stays the same authorized operation.
func seedWorkspaceDeleteOperation(t *testing.T, fixture workspaceDeleteFixture, mutate func(*workspaceDeleteOperation)) workspaceDeleteOperation {
	t.Helper()
	row, found, err := fixture.store.GetRuntimeOperation(context.Background(), workspaceDeleteOperationID("ws-alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		// The command owns the durable authorization of the deletion, so the
		// recorded operation starts as its authorized first phase.
		response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-seed-authorization")
		if response.Code != http.StatusAccepted {
			t.Fatalf("seed delete status=%d body=%s", response.Code, response.Body.String())
		}
		row, found, err = fixture.store.GetRuntimeOperation(context.Background(), workspaceDeleteOperationID("ws-alpha"))
		if err != nil || !found {
			t.Fatalf("delete operation found=%v err=%v", found, err)
		}
	}
	var operation workspaceDeleteOperation
	if json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil {
		t.Fatal("delete operation decode failed")
	}
	mutate(&operation)
	row["result"] = string(mustJSON(operation))
	row["status"] = operation.Status
	if err := fixture.store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return operation
}

// seedWorkspaceDeleteRefundRecord persists the refund record the retired refund
// engine would have written, so the retained decoder and the customer-visible
// status can be read against a real durable row.
func seedWorkspaceDeleteRefundRecord(t *testing.T, fixture workspaceDeleteFixture, operation workspaceDeleteRefundOperation) {
	t.Helper()
	operationID := workspaceDeleteRefundOperationID(workspaceDeleteOperationID("ws-alpha"))
	row := map[string]any{
		"id": operationID, "operationId": operationID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceKind": "workspace", "resourceId": operation.WorkspaceID, "action": workspaceDeleteRefundAction,
		"status": operation.Status, "result": string(mustJSON(operation)), "createdAt": operation.CreatedAt,
	}
	if err := fixture.store.SaveRuntimeOperation(context.Background(), row); err != nil {
		t.Fatal(err)
	}
}

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

// The deletion status is a pure store read: reading progress never contacts the
// provider, because no execution engine performs provider reads any more.
func TestWorkspaceDeletionStatusServesEveryStageWithoutProviderReads(t *testing.T) {
	fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
	seedWorkspaceDeleteOperation(t, fixture, func(operation *workspaceDeleteOperation) {
		// The Machine is gone but TKE still reports the node: the deletion is waiting
		// on compute, which is exactly the case a phase-driven projection mislabelled.
		operation.Phase, operation.Status = "storage_absent", "running"
		operation.RuntimeStatus, operation.SecretStatus = "absent", "absent"
		operation.AttachmentStatus, operation.StorageStatus = "absent", "absent"
		operation.ComputeStatus = "destroying"
		operation.ComputeReadbacks, operation.MaxComputeReadbacks = 1, workspaceDeleteComputeReadbackBudget
		operation.ComputeReadbackNotBefore = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	})
	before := len(fixture.fabric.recordedCalls())

	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}
	if len(fixture.fabric.recordedCalls()) != before {
		t.Fatalf("status read contacted Fabric: before=%d after=%d", before, len(fixture.fabric.recordedCalls()))
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
}

// A converging storage deletion is the same kind of read: the status endpoint
// reports the stage and the scheduled retry from store state alone.
func TestWorkspaceDeletionStatusReportsStorageWaitFromStoreState(t *testing.T) {
	fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
	seedWorkspaceDeleteOperation(t, fixture, func(operation *workspaceDeleteOperation) {
		operation.Phase, operation.Status = "attachment_absent", "running"
		operation.RuntimeStatus, operation.SecretStatus = "absent", "absent"
		operation.AttachmentStatus = "absent"
	})
	before := len(fixture.fabric.recordedCalls())
	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}
	if len(fixture.fabric.recordedCalls()) != before {
		t.Fatal("status read contacted Fabric")
	}
	if dto.Phase != "attachment_absent" || dto.Stage != contracts.WorkspaceDeleteStageStorageAbsent {
		t.Fatalf("dto=%#v", dto)
	}
	if dto.PageState != contracts.WorkspaceDeletePageStateWaiting {
		t.Fatalf("page state=%q (no failure was recorded yet)", dto.PageState)
	}
}

// The admin Runtime detail receives the same persisted deletion facts as the
// customer deletion page: the stage in progress, the page state, the stable reason,
// the most recent readback and the scheduled retry. Reading them is a store read,
// so it never drives the deletion forward.
func TestOperatorRuntimeObservationPublishesDeletionProgress(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		seed       func(*workspaceDeleteOperation)
		wantStatus string
		wantReason string
		wantStage  string
		wantState  string
		wantRetry  bool
	}{
		{
			name: "compute still terminating",
			seed: func(operation *workspaceDeleteOperation) {
				operation.Phase, operation.Status = "storage_absent", "running"
				operation.RuntimeStatus, operation.SecretStatus = "absent", "absent"
				operation.AttachmentStatus, operation.StorageStatus = "absent", "absent"
				operation.ComputeStatus = "destroying"
				operation.ComputeReadbacks, operation.MaxComputeReadbacks = 1, workspaceDeleteComputeReadbackBudget
				operation.ComputeReadbackNotBefore = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
			},
			wantStatus: "pending", wantReason: "workspace_delete_in_progress",
			wantStage: contracts.WorkspaceDeleteStageComputeAbsent, wantState: contracts.WorkspaceDeletePageStateRetrying, wantRetry: true,
		},
		{
			name: "storage identity conflict",
			seed: func(operation *workspaceDeleteOperation) {
				operation.Phase, operation.Status = "attachment_absent", "manual_review"
				operation.RuntimeStatus, operation.SecretStatus = "absent", "absent"
				operation.AttachmentStatus = "absent"
				operation.LastErrorCode = "fabric_storage_identity_conflict"
			},
			wantStatus: "attention", wantReason: "workspace_delete_identity_conflict",
			wantStage: contracts.WorkspaceDeleteStageStorageAbsent, wantState: contracts.WorkspaceDeletePageStateBlocked,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
			seedWorkspaceDeleteOperation(t, fixture, testCase.seed)
			handler := fixture.server.(*controlPlaneHTTPHandler)
			before := len(fixture.fabric.recordedCalls())
			observations, err := handler.app.operatorRuntimeObservations(context.Background(), handler.service, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if len(fixture.fabric.recordedCalls()) != before {
				t.Fatalf("operator observation drove the deletion forward: before=%d after=%d", before, len(fixture.fabric.recordedCalls()))
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
			if item.DeleteStage != testCase.wantStage || item.DeletePageState != testCase.wantState {
				t.Fatalf("operator deletion progress=%#v", item)
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

// A platform refund that was reserved against an original charge and never
// settled stays visible on the customer-visible deletion status. The refund
// execution engine is retired, so the obligation must not disappear with it: the
// retained decoder still reports the reservation, its amount and the wallet
// operation it owns.
func TestWorkspaceDeletionStatusReportsUnresolvedRefundObligation(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		status     string
		reasonCode string
	}{
		{name: "pending dispatch", status: workspaceDeleteRefundStatusPending, reasonCode: "sub2api_refund_unconfirmed"},
		{name: "blocked refund", status: workspaceDeleteRefundStatusBlocked, reasonCode: "wallet_refund_operation_unavailable"},
		{name: "manual review", status: workspaceDeleteRefundStatusManualReview, reasonCode: "sub2api_refund_mismatch"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
			seedWorkspaceDeleteOperation(t, fixture, func(operation *workspaceDeleteOperation) {
				operation.Phase, operation.Status = "workspace_absent", "running"
				operation.RuntimeStatus, operation.SecretStatus = "absent", "absent"
				operation.AttachmentStatus, operation.StorageStatus = "absent", "absent"
				operation.ComputeStatus = "absent"
				operation.ComputeReadbacks, operation.MaxComputeReadbacks = 1, workspaceDeleteComputeReadbackBudget
			})
			seedWorkspaceDeleteRefundRecord(t, fixture, workspaceDeleteRefundOperation{
				SchemaVersion: 1, AccountID: "acct-alpha", WorkspaceID: "ws-alpha",
				DeleteOperationID: workspaceDeleteOperationID("ws-alpha"), LaunchOperationID: "workspace-launch-alpha",
				PolicyVersion: contracts.WorkspaceRefundPolicyVersion, Status: testCase.status, ReasonCode: testCase.reasonCode,
				OriginalChargeUSDMicros: 52_580_000, RefundUSDMicros: 26_290_000,
				WalletAdjustmentOperationID: "wallet-adjustment-delete-alpha", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			})

			status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
			var dto workspaceDeletionStatusDTO
			if json.Unmarshal(status.Body.Bytes(), &dto) != nil || status.Code != http.StatusOK {
				t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
			}
			if dto.RefundStatus != testCase.status || dto.RefundReasonCode != testCase.reasonCode {
				t.Fatalf("refund obligation dto=%#v", dto)
			}
			if dto.RefundOperationID != "wallet-adjustment-delete-alpha" || dto.RefundUSDMicros != 26_290_000 ||
				dto.OriginalChargeUSDMicros != 52_580_000 || dto.RefundPolicyVersion != contracts.WorkspaceRefundPolicyVersion {
				t.Fatalf("refund reservation dto=%#v", dto)
			}
			// An unresolved refund never claims a receipt: the wallet owner has not
			// confirmed the money movement.
			if dto.RefundReceiptID != "" {
				t.Fatalf("unresolved refund exposed a receipt: %#v", dto)
			}
		})
	}
}

// The deletion status belongs to the Workspace owner. Another account may not
// read it, and a Workspace that never existed is not a deletion.
func TestWorkspaceDeletionStatusIsOwnerBound(t *testing.T) {
	fixture, _, _ := newWorkspaceDeleteCompletionFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	if _, err := handler.app.createUser(context.Background(), handler.service, map[string]any{
		"email": "delete-status-other@example.test", "accountId": "acct-beta", "password": "CorrectHorseBatteryStaple!",
	}); err != nil {
		t.Fatal(err)
	}
	other := loginForTest(t, fixture.server, "delete-status-other@example.test", "CorrectHorseBatteryStaple!")

	result := requestWithSession(t, fixture.server, other, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	if result.Code != http.StatusForbidden {
		t.Fatalf("cross-owner status=%d body=%s", result.Code, result.Body.String())
	}
	missing := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-missing/deletion", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
}

// The deletion receipt must be sensitive to the resource identity and the readback
// record behind each stage: two observations that differ only in what they read must
// never produce the same receipt digest.
func TestReceiptEvidenceDigestBindsResourceAndReadbackIdentity(t *testing.T) {
	first := contracts.WorkspaceDeleteStageEvidence{
		Stage: contracts.WorkspaceDeleteStageStorageAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent,
		EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "storage-a",
		ProviderResourceID: "disk-a", ObservedAt: "2026-09-18T05:00:00.000000Z", ReadbackID: "read-a",
	}
	second := first
	second.ResourceID, second.ProviderResourceID, second.ReadbackID = "storage-b", "disk-b", "read-b"
	if reflect.DeepEqual(contracts.WorkspaceDeleteStageEvidenceDigests([]contracts.WorkspaceDeleteStageEvidence{first}), contracts.WorkspaceDeleteStageEvidenceDigests([]contracts.WorkspaceDeleteStageEvidence{second})) {
		t.Fatal("receipt digest is unchanged after changing the resource and readback identity")
	}
	// The reference is opaque: it must not carry the identifiers themselves.
	digest := contracts.WorkspaceDeleteStageEvidenceDigests([]contracts.WorkspaceDeleteStageEvidence{first})[0]
	for _, private := range []string{"disk-a", "read-a", "storage-a"} {
		if digest.EvidenceRef == private {
			t.Fatalf("evidence reference leaked %q", private)
		}
	}
}

// A provider observation that does not name the readback it came from is not
// traceable, so it is refused outright.
func TestProviderStageEvidenceRequiresItsReadbackRecord(t *testing.T) {
	evidence := contracts.WorkspaceDeleteStageEvidence{
		Stage: contracts.WorkspaceDeleteStageStorageAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent,
		EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "storage-a",
		ProviderResourceID: "disk-a", ObservedAt: "2026-09-18T05:00:00.000000Z",
	}
	if contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
		t.Fatal("provider evidence with no readback record is accepted")
	}
	evidence.ReadbackID = "read-a"
	if !contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
		t.Fatal("provider evidence with its readback record is rejected")
	}
}
