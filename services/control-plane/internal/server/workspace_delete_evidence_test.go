package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// The deletion persists one confirmation per stage, in the frozen order, each bound
// to the resource identity and the time the observing owner observed the fact.
func TestWorkspaceDeleteRecordsStageEvidenceForEveryStage(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-evidence-chain")
	operation := mustWorkspaceDeleteOperation(t, fixture)

	if !contracts.WorkspaceDeleteStageEvidenceComplete(operation.StageEvidence) {
		t.Fatalf("evidence is not the complete sequence: %#v", operation.StageEvidence)
	}
	for _, evidence := range operation.StageEvidence {
		if !contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
			t.Fatalf("recorded evidence is invalid: %#v", evidence)
		}
	}
	// Each stage is identified by the entity whose confirmation it is, and the
	// observation time is a real timestamp rather than the record's write time.
	for index, evidence := range operation.StageEvidence {
		if _, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt); err != nil {
			t.Fatalf("evidence[%d] has no usable observation time: %#v", index, evidence)
		}
		if strings.TrimSpace(evidence.ResourceID) == "" {
			t.Fatalf("evidence[%d] has no resource identity: %#v", index, evidence)
		}
	}
	// A local transition must never claim a provider readback, and vice versa.
	byStage := map[string]contracts.WorkspaceDeleteStageEvidence{}
	for _, evidence := range operation.StageEvidence {
		byStage[evidence.Stage] = evidence
	}
	if byStage[contracts.WorkspaceDeleteStageAttachmentAbsent].EvidenceKind != contracts.WorkspaceDeleteEvidenceLocalTransition ||
		byStage[contracts.WorkspaceDeleteStageAttachmentAbsent].MutationAttempts != 1 {
		t.Fatalf("attachment evidence=%#v", byStage[contracts.WorkspaceDeleteStageAttachmentAbsent])
	}
	if byStage[contracts.WorkspaceDeleteStageStorageAbsent].EvidenceKind != contracts.WorkspaceDeleteEvidenceProviderReadback ||
		byStage[contracts.WorkspaceDeleteStageStorageAbsent].ReadbackID == "" ||
		byStage[contracts.WorkspaceDeleteStageWorkspaceAbsent].EvidenceKind != contracts.WorkspaceDeleteEvidenceLocalTransition {
		t.Fatalf("storage/workspace evidence=%#v", byStage)
	}
	// A query must never stand in for a destroy: the readbacks and the destroy are
	// counted separately.
	if byStage[contracts.WorkspaceDeleteStageStorageAbsent].ReadAttempts < 1 ||
		byStage[contracts.WorkspaceDeleteStageStorageAbsent].MutationAttempts != 1 {
		t.Fatalf("storage attempt counters=%#v", byStage[contracts.WorkspaceDeleteStageStorageAbsent])
	}
}

func TestWorkspaceDeleteStorageRequiresFreshOwnerReadbackBeforeAdvancing(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*workspaceDeleteFabric)
	}{
		{"unavailable", func(f *workspaceDeleteFabric) {
			f.storageReadbackResults = nil
			f.storageReadErr = errWorkspaceDeleteUnconfirmed
		}},
		{"missing observation time", func(f *workspaceDeleteFabric) { value := ""; f.storageReadObservedAt = &value }},
		{"stale observation", func(f *workspaceDeleteFabric) {
			value := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
			f.storageReadObservedAt = &value
		}},
		{"future observation", func(f *workspaceDeleteFabric) {
			value := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
			f.storageReadObservedAt = &value
		}},
		{"missing readback reference", func(f *workspaceDeleteFabric) { value := ""; f.storageReadbackID = &value }},
		{"different volume", func(f *workspaceDeleteFabric) { f.storageReadbackResults[0].ID = "storage-other" }},
		{"different workspace", func(f *workspaceDeleteFabric) { f.storageReadbackResults[0].WorkspaceID = "ws-other" }},
		{"different provider resource", func(f *workspaceDeleteFabric) { f.storageReadbackResults[0].ProviderResourceID = "disk-other" }},
		{"missing provider resource", func(f *workspaceDeleteFabric) { f.storageReadbackResults[0].ProviderResourceID = "" }},
		{"disk still attached", func(f *workspaceDeleteFabric) { f.storageReadbackResults[0].CBSStatus = "ATTACHED" }},
		{"unknown binding", func(f *workspaceDeleteFabric) { f.storageReadbackResults[0].BindingPresent = nil }},
		{"binding present", func(f *workspaceDeleteFabric) { present := true; f.storageReadbackResults[0].BindingPresent = &present }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := newWorkspaceDeleteRefundFabric()
			testCase.mutate(fabric)
			fixture, sub2API, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			// Bind the original disk before deletion; a readback cannot rebind it.
			storage, _, err := fixture.store.GetStorage(context.Background(), "storage-alpha")
			if err != nil {
				t.Fatal(err)
			}
			storage["providerResourceId"] = "disk-alpha"
			if err := fixture.store.SaveStorage(context.Background(), storage); err != nil {
				t.Fatal(err)
			}
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "delete-storage-readback")
			if response.Code != http.StatusBadGateway {
				t.Fatalf("unconfirmed readback status=%d body=%s", response.Code, response.Body.String())
			}
			operation := mustWorkspaceDeleteOperation(t, fixture)
			if operation.Phase != "attachment_absent" || operation.StorageStatus != "" || operation.DeletionReceiptID != "" {
				t.Fatalf("unconfirmed readback advanced deletion: %#v", operation)
			}
			if evidence, found := contracts.WorkspaceDeleteStageEvidenceLatest(operation.StageEvidence, contracts.WorkspaceDeleteStageStorageAbsent); found && evidence.Confirmed() {
				t.Fatalf("invalid immutable confirmation: %#v", evidence)
			}
			for _, call := range fabric.recordedCalls() {
				if strings.HasPrefix(call, "compute:") {
					t.Fatalf("compute destruction preceded valid storage evidence: %s", call)
				}
			}
			for _, receipt := range ledger.receipts {
				if receipt.Type == "workspace.deleted.v1" {
					t.Fatal("unconfirmed storage was recorded as deleted")
				}
			}
			if len(sub2API.refunds) != 0 {
				t.Fatal("unconfirmed storage dispatched a refund")
			}
		})
	}
}

// A recorded confirmation is append-only: a later write can never rewrite or drop
// the binding the receipt and the refund gate already rely on.
func TestWorkspaceDeleteEvidenceIsAppendOnly(t *testing.T) {
	operation := workspaceDeleteOperation{
		OperationID: "op", WorkspaceID: "ws", AccountID: "acct", LaunchOperationID: "launch",
		Phase: "runtime_secret_absent", Status: "running", RuntimeStatus: "absent", SecretStatus: "absent",
		StageEvidence: []contracts.WorkspaceDeleteStageEvidence{{
			Stage: contracts.WorkspaceDeleteStageRuntimeAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent,
			EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "runtime",
			ObservedAt: "2026-09-18T05:00:00.000000Z", ReadbackID: "readback-a",
		}},
	}
	appended := confirmStageEvidence(operation,
		contracts.WorkspaceDeleteStageAttachmentAbsent, contracts.WorkspaceDeleteEvidenceReleased, contracts.WorkspaceDeleteEvidenceLocalTransition,
		"attachment", "", "2026-09-18T05:01:00.000000Z", "", 1,
	)
	if len(appended.StageEvidence) != 2 || !workspaceDeleteEvidenceExtends(operation.StageEvidence, appended.StageEvidence) {
		t.Fatalf("append result=%#v", appended.StageEvidence)
	}
	// The recorded binding survives a rewrite of the same slice.
	if appended.StageEvidence[0] != operation.StageEvidence[0] {
		t.Fatal("append rewrote an existing confirmation")
	}
	// A rewrite that changes or drops an existing binding is refused.
	for _, testCase := range []struct {
		name    string
		current []contracts.WorkspaceDeleteStageEvidence
		desired []contracts.WorkspaceDeleteStageEvidence
	}{
		{"dropped evidence", appended.StageEvidence, operation.StageEvidence},
		{"rewritten evidence", appended.StageEvidence, func() []contracts.WorkspaceDeleteStageEvidence {
			rewritten := append([]contracts.WorkspaceDeleteStageEvidence(nil), appended.StageEvidence...)
			rewritten[0].ObservedAt = "2030-01-01T00:00:00.000000Z"
			return rewritten
		}()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if workspaceDeleteEvidenceExtends(testCase.current, testCase.desired) {
				t.Fatalf("unsafe rewrite accepted: %#v", testCase.desired)
			}
		})
	}
}

// A partial evidence list may not produce a receipt: the receipt would attest less
// than the deletion contract requires.
func TestWorkspaceDeleteRefusesReceiptFromPartialEvidence(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-evidence-partial")
	operation := mustWorkspaceDeleteOperation(t, fixture)

	// The operation completes with the full sequence; the receipt itself attests
	// every stage before the receipt stage. Removing one of those is the partial case.
	handler := fixture.server.(*controlPlaneHTTPHandler)
	truncated := operation
	truncated.StageEvidence = operation.StageEvidence[:len(operation.StageEvidence)-2]
	truncated.DeletionReceiptID = ""
	truncated.Phase, truncated.Status = "workspace_absent", "running"
	if err := handler.app.persistWorkspaceDelete(context.Background(), operation, truncated, false, true); err == nil {
		t.Fatal("truncating recorded evidence was accepted by the owning write path")
	}
	// And the receipt step itself refuses a partial list, so no receipt can attest
	// less than the deletion contract requires.
	if _, err := handler.app.recordWorkspaceDeletionReceipt(context.Background(), handler.service, truncated); err == nil {
		t.Fatal("a partial evidence list produced a deletion receipt")
	}
	receipts := 0
	for _, receipt := range ledger.receipts {
		if receipt.Type == "workspace.deleted.v1" {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("partial evidence wrote another deletion receipt: %d", receipts)
	}
	if !contracts.WorkspaceDeleteReceiptEvidenceComplete(operation.StageEvidence[:len(operation.StageEvidence)-1]) {
		t.Fatal("the recorded sequence is not the complete receipt evidence")
	}
	if contracts.WorkspaceDeleteReceiptEvidenceComplete(truncated.StageEvidence) {
		t.Fatal("a partial list satisfied the receipt evidence contract")
	}
}

// A stage that is waiting on the provider records the owning read's own observation:
// its observation time and the readback reference it came from. It must never present
// a local clock as a provider observation.
func TestRuntimeWaitRecordsTheOwningObservationNotALocalClock(t *testing.T) {
	const pinnedObservedAt = "2026-09-18T05:00:00.000000Z"
	fabric := &workspaceDeleteFabric{
		observeState:         clients.WorkspaceOwnerObservationReady,
		secretObserveState:   clients.WorkspaceOwnerObservationReady,
		residualObserveState: clients.WorkspaceRuntimeDeleteObservationPresent,
		// The provider reported exactly this observation for this read.
		residualObservedAt: pinnedObservedAt,
	}
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "evidence-runtime-wait")
	if response.Code != http.StatusAccepted {
		t.Fatalf("runtime wait status=%d body=%s", response.Code, response.Body.String())
	}
	operation := mustWorkspaceDeleteOperation(t, fixture)
	evidence, present := contracts.WorkspaceDeleteStageEvidenceLatest(operation.StageEvidence, contracts.WorkspaceDeleteStageRuntimeAbsent)
	if !present {
		t.Fatalf("no runtime observation was recorded: phase=%s evidence=%#v", operation.Phase, operation.StageEvidence)
	}
	if !contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
		t.Fatalf("recorded runtime observation fails its own contract: %#v", evidence)
	}
	if evidence.Result != contracts.WorkspaceDeleteEvidenceWaiting || evidence.Confirmed() {
		t.Fatalf("runtime wait is not an unfinished observation: %#v", evidence)
	}
	if evidence.EvidenceKind != contracts.WorkspaceDeleteEvidenceProviderReadback || evidence.ReadbackID != "run-readback-ws-alpha" {
		t.Fatalf("runtime wait does not name the readback it came from: %#v", evidence)
	}
	// The recorded time is the provider's observation, not the moment we persisted it.
	if evidence.ObservedAt != pinnedObservedAt {
		t.Fatalf("recorded observedAt=%q, want the owning read's %q", evidence.ObservedAt, pinnedObservedAt)
	}
	// The wait stays retryable rather than becoming a terminal review state.
	if operation.LastErrorCode != "" || operation.Status != "running" {
		t.Fatalf("runtime wait was recorded as terminal: status=%q lastError=%q", operation.Status, operation.LastErrorCode)
	}
}

// When the owning read returns no usable readback, the wait is recorded explicitly as
// unavailable instead of claiming a provider observation.
func TestRuntimeWaitWithoutAReadbackIsRecordedAsUnavailable(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*workspaceDeleteFabric)
	}{
		{"the reading owner fails", func(f *workspaceDeleteFabric) { f.failStage, f.failures = "runtime-residual-read", 1 }},
		{"the readback names no reference", func(f *workspaceDeleteFabric) {
			empty := ""
			f.residualReadbackID = &empty
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fabric := &workspaceDeleteFabric{
				observeState:         clients.WorkspaceOwnerObservationReady,
				secretObserveState:   clients.WorkspaceOwnerObservationReady,
				residualObserveState: clients.WorkspaceRuntimeDeleteObservationPresent,
			}
			testCase.mutate(fabric)
			fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
			response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodDelete, "/api/workspaces/ws-alpha", `{}`, "evidence-runtime-unavailable-"+stableID(testCase.name))
			if response.Code != http.StatusAccepted {
				t.Fatalf("unavailable readback status=%d body=%s", response.Code, response.Body.String())
			}
			operation := mustWorkspaceDeleteOperation(t, fixture)
			evidence, present := contracts.WorkspaceDeleteStageEvidenceLatest(operation.StageEvidence, contracts.WorkspaceDeleteStageRuntimeAbsent)
			if !present {
				t.Fatalf("no runtime observation was recorded: %#v", operation.StageEvidence)
			}
			if !contracts.ValidWorkspaceDeleteStageEvidence(evidence) {
				t.Fatalf("unavailable observation fails its own contract: %#v", evidence)
			}
			if evidence.EvidenceKind != contracts.WorkspaceDeleteEvidenceUnavailable || evidence.ReadbackID != "" {
				t.Fatalf("unavailable readback was presented as an observation: %#v", evidence)
			}
			if evidence.Result != contracts.WorkspaceDeleteEvidenceWaiting || evidence.Confirmed() {
				t.Fatalf("unavailable readback became a confirmation: %#v", evidence)
			}
			if evidence.ReasonCode != "fabric_runtime_readback_unavailable" {
				t.Fatalf("unavailable readback has no stable cause: %#v", evidence)
			}
			// The attempt time is the only time an unavailable entry may carry, and the
			// operation stays retryable.
			if _, err := time.Parse(time.RFC3339Nano, evidence.ObservedAt); err != nil {
				t.Fatalf("unavailable entry has no attempt time: %#v", evidence)
			}
			if operation.Status != "running" || operation.LastErrorCode != "" {
				t.Fatalf("unavailable readback became terminal: status=%q lastError=%q", operation.Status, operation.LastErrorCode)
			}
		})
	}
}

// An unavailable observation never satisfies the receipt, so a deletion that could not
// read its provider can never be receipted from that attempt.
func TestUnavailableObservationNeverSatisfiesTheReceipt(t *testing.T) {
	observedAt := "2026-09-18T05:00:00.000000Z"
	unavailable := contracts.WorkspaceDeleteStageEvidence{
		Stage: contracts.WorkspaceDeleteStageRuntimeAbsent, Result: contracts.WorkspaceDeleteEvidenceWaiting,
		EvidenceKind: contracts.WorkspaceDeleteEvidenceUnavailable, ResourceID: "runtime-alpha",
		ObservedAt: observedAt, ReadAttempts: 1, MutationAttempts: 1,
	}
	if !contracts.ValidWorkspaceDeleteStageEvidence(unavailable) {
		t.Fatal("an unavailable observation is valid evidence")
	}
	if unavailable.Confirmed() {
		t.Fatal("an unavailable observation must never confirm its stage")
	}
	// Claiming the stage's confirmation while naming no readback is refused too, so a
	// provider observation can never be recorded without the readback behind it.
	claimed := unavailable
	claimed.Result, claimed.EvidenceKind = contracts.WorkspaceDeleteEvidenceAbsent, contracts.WorkspaceDeleteEvidenceProviderReadback
	if contracts.ValidWorkspaceDeleteStageEvidence(claimed) {
		t.Fatal("a confirmation without a readback was accepted")
	}
	claimed.ReadbackID = "readback-alpha"
	if !contracts.ValidWorkspaceDeleteStageEvidence(claimed) {
		t.Fatal("a confirmation with its readback was refused")
	}
	// An unavailable entry can never carry a readback reference.
	withReference := unavailable
	withReference.ReadbackID = "readback-alpha"
	if contracts.ValidWorkspaceDeleteStageEvidence(withReference) {
		t.Fatal("an unavailable observation carrying a readback was accepted")
	}
}

// A retained operation that predates the evidence contract completes with the
// original receipt shape. It never receives back-filled confirmations it did not
// observe.
func TestWorkspaceDeleteRetainedOperationKeepsOriginalReceiptShape(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, ledger := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-evidence-legacy")
	operation := mustWorkspaceDeleteOperation(t, fixture)
	handler := fixture.server.(*controlPlaneHTTPHandler)

	// Model a retained operation as it exists in the store: it predates the evidence
	// contract, so it has no recorded confirmation at all.
	legacy := operation
	legacy.StageEvidence = nil
	legacy.DeletionReceiptID = ""
	legacy.Phase, legacy.Status, legacy.DeletedAt = "workspace_absent", "running", ""
	if err := fixture.store.SaveRuntimeOperation(context.Background(), workspaceDeleteOperationRow(legacy)); err != nil {
		t.Fatal(err)
	}
	next, err := handler.app.recordWorkspaceDeletionReceipt(context.Background(), handler.service, legacy)
	if err != nil {
		t.Fatalf("legacy operation could not record its receipt: %v", err)
	}
	// The receipt keeps the original shape: no fabricated summary, and no evidence
	// is invented for the stages the operation never confirmed.
	var recorded clients.ReceiptInput
	for _, receipt := range ledger.receipts {
		if receipt.Type == "workspace.deleted.v1" {
			recorded = receipt
		}
	}
	if _, present := recorded.Execution["stageEvidence"]; present {
		t.Fatalf("legacy receipt carried a fabricated summary: %#v", recorded.Execution["stageEvidence"])
	}
	if _, present := recorded.Execution["stageEvidenceSchemaVersion"]; present {
		t.Fatalf("legacy receipt carried a fabricated summary version: %#v", recorded.Execution)
	}
	recordedEvidence := confirmStageEvidence(operation,
		contracts.WorkspaceDeleteStageReceiptRecorded, contracts.WorkspaceDeleteEvidenceRecorded, contracts.WorkspaceDeleteEvidenceLedgerReceipt,
		operation.WorkspaceID, "receipt", "2026-09-18T05:00:00.000000Z", "receipt", 0,
	)
	if len(recordedEvidence.StageEvidence) != len(operation.StageEvidence) {
		t.Fatalf("a mid-sequence confirmation was recorded without its predecessors: %#v", recordedEvidence.StageEvidence)
	}
	if next.DeletionReceiptID == "" {
		t.Fatal("legacy receipt step did not record the receipt identity")
	}
}

// The deletion receipt carries the persisted confirmations, and its owner can read
// them back. A receipt whose summary is malformed is refused rather than published.
func TestWorkspaceDeletionReceiptCarriesPersistedEvidence(t *testing.T) {
	fabric := newWorkspaceDeleteRefundFabric()
	fixture, _, _ := newWorkspaceDeleteCompletionFixtureWith(t, newMemoryTableStore(), fabric)
	deleteWorkspaceRefundForTest(t, fixture, "delete-evidence-receipt")
	operation := mustWorkspaceDeleteOperation(t, fixture)

	status := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/workspaces/ws-alpha/deletion", "")
	var dto workspaceDeletionStatusDTO
	if json.Unmarshal(status.Body.Bytes(), &dto) != nil || dto.ReceiptID == "" {
		t.Fatalf("deletion status=%d body=%s", status.Code, status.Body.String())
	}
	response := requestWithSession(t, fixture.server, fixture.session, http.MethodGet, "/api/billing/receipts/"+dto.ReceiptID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("deletion receipt status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Available bool `json:"available"`
		Data      struct {
			StageEvidence []contracts.WorkspaceDeleteStageEvidenceDigest `json:"stageEvidence"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || !envelope.Available {
		t.Fatalf("deletion receipt body=%s", response.Body.String())
	}
	// The receipt attests every stage but its own, and each digest matches the
	// confirmation the operation recorded.
	if !contracts.ValidWorkspaceDeleteStageEvidenceDigests(envelope.Data.StageEvidence) {
		t.Fatalf("receipt evidence=%#v", envelope.Data.StageEvidence)
	}
	for index, digest := range envelope.Data.StageEvidence {
		recorded := operation.StageEvidence[index]
		if digest.Stage != recorded.Stage || digest.Result != recorded.Result || digest.EvidenceKind != recorded.EvidenceKind ||
			digest.ObservedAt != recorded.ObservedAt {
			t.Fatalf("receipt digest %d does not match the recorded confirmation: %#v vs %#v", index, digest, recorded)
		}
	}
	// Private provider and readback identities stay out of the owner-facing digest.
	body := response.Body.String()
	for _, private := range []string{"disk-alpha", "ins-alpha", "run-readback-ws-alpha"} {
		if strings.Contains(body, private) {
			t.Fatalf("deletion receipt leaked %q: %s", private, body)
		}
	}
}
