package contracts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func workspaceDeleteTestIdentity() WorkspaceDeleteIdentity {
	deletedAt := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	return WorkspaceDeleteIdentity{
		DeleteOperationID: "workspace-delete-ws-alpha", LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha",
		RuntimeID: "runtime-alpha", ComputeID: "compute-alpha", StorageID: "storage-alpha", AttachmentID: "attachment-alpha",
		StorageProviderResourceID: "disk-alpha", ComputeMachineName: "machine-alpha", ComputeInstanceID: "ins-alpha",
		ResourceFulfilledAt: deletedAt.Add(-100 * time.Hour).Format(time.RFC3339Nano), WorkspaceDeletedAt: deletedAt.Format(time.RFC3339Nano),
	}
}

func workspaceDeleteTestReadback(observedAt time.Time) WorkspaceDeleteReadback {
	readback := WorkspaceDeleteReadback{
		SchemaVersion: WorkspaceDeleteReadbackSchemaVersion, Provider: "tencent-tke",
		DeleteOperationID: "workspace-delete-ws-alpha", LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha",
		ObservedAt: observedAt.Format(time.RFC3339Nano), ReadbackID: "readback-alpha", Result: WorkspaceDeleteResultCompleted,
	}
	identity := workspaceDeleteTestIdentity()
	for _, kind := range WorkspaceDeleteRequiredResourceKinds() {
		resourceID, _ := identity.ExpectedResourceID(kind)
		readback.Facts = append(readback.Facts, WorkspaceDeleteResourceFact{
			Kind: kind, ResourceID: resourceID, Present: false, Observed: true, ProviderStatus: WorkspaceDeleteProviderStatusNotFound,
			ObservedAt: readback.ObservedAt, ReadbackID: readback.ReadbackID,
		})
	}
	return readback
}

func workspaceDeleteTestInput(now time.Time) WorkspaceDeleteRefundGateInput {
	return WorkspaceDeleteRefundGateInput{
		Identity: workspaceDeleteTestIdentity(), Readback: workspaceDeleteTestReadback(now.Add(-time.Minute)),
		DeleteReceiptRecorded: true, Now: now, MaxReadbackAge: time.Hour,
	}
}

func TestPlatformRefundDispatchAllowedRequiresCompleteAuthoritativeReadback(t *testing.T) {
	now := time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC)
	allowed, reason := PlatformRefundDispatchAllowed(workspaceDeleteTestInput(now))
	if !allowed || reason != "" {
		t.Fatalf("allowed=%v reason=%q", allowed, reason)
	}
}

func TestPlatformRefundDispatchAllowedRefusesEveryWeakerSignal(t *testing.T) {
	now := time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*WorkspaceDeleteRefundGateInput)
		reason string
	}{
		{"workspace only suspended while the runtime still exists", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceRuntimeController {
					in.Readback.Facts[index].Present, in.Readback.Facts[index].ProviderStatus = true, "SUSPENDED"
				}
			}
		}, WorkspaceDeleteGateResourcePresent},
		{"data deleted workspace with resources still present", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceCBS {
					in.Readback.Facts[index].Present, in.Readback.Facts[index].ProviderStatus = true, "UNATTACHED"
				}
			}
		}, WorkspaceDeleteGateResourcePresent},
		{"cvm shut down but present", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceCVM {
					in.Readback.Facts[index].Present, in.Readback.Facts[index].ProviderStatus = true, "RUNNING"
				}
			}
		}, WorkspaceDeleteGateResourcePresent},
		{"workspace only suspended", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceRuntimeController {
					in.Readback.Facts[index].Present, in.Readback.Facts[index].ProviderStatus = true, "SUSPENDED"
				}
			}
		}, WorkspaceDeleteGateResourcePresent},
		{"runtime gone but cbs still attached", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceCBS {
					in.Readback.Facts[index].Present, in.Readback.Facts[index].ProviderStatus = true, "ATTACHED"
				}
			}
		}, WorkspaceDeleteGateResourcePresent},
		{"single resource reported absent only", func(in *WorkspaceDeleteRefundGateInput) {
			in.Readback.Facts = in.Readback.Facts[:1]
		}, WorkspaceDeleteGateReadbackUnavailable},
		{"provider readback unavailable", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceCBS {
					in.Readback.Facts[index].Observed, in.Readback.Facts[index].ProviderStatus = false, ""
				}
			}
		}, WorkspaceDeleteGateReadbackUnavailable},
		{"provider identity mismatch", func(in *WorkspaceDeleteRefundGateInput) {
			in.Readback.Facts[0].ResourceID = "runtime-other"
		}, WorkspaceDeleteGateReadbackIdentity},
		{"readback read before this deletion", func(in *WorkspaceDeleteRefundGateInput) {
			stale := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
			in.Readback.ObservedAt = stale
			for index := range in.Readback.Facts {
				in.Readback.Facts[index].ObservedAt = stale
			}
		}, WorkspaceDeleteGateReadbackStale},
		{"stale cached readback", func(in *WorkspaceDeleteRefundGateInput) {
			cached := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
			in.Readback.ObservedAt = cached
			for index := range in.Readback.Facts {
				in.Readback.Facts[index].ObservedAt = cached
			}
			in.MaxReadbackAge = time.Minute
		}, WorkspaceDeleteGateReadbackStale},
		{"delete receipt not recorded", func(in *WorkspaceDeleteRefundGateInput) {
			in.DeleteReceiptRecorded = false
		}, WorkspaceDeleteGateReceiptMissing},
		{"readback mutated provider resources", func(in *WorkspaceDeleteRefundGateInput) {
			in.Readback.MutationCount = 1
		}, WorkspaceDeleteGateReadbackUnavailable},
		{"deletion operation still pending", func(in *WorkspaceDeleteRefundGateInput) {
			in.Identity.WorkspaceDeletedAt = ""
		}, WorkspaceDeleteGateOperationIncomplete},
		{"provider status unknown", func(in *WorkspaceDeleteRefundGateInput) {
			for index := range in.Readback.Facts {
				if in.Readback.Facts[index].Kind == WorkspaceDeleteResourceMachine {
					in.Readback.Facts[index].ProviderStatus = "UNKNOWN"
				}
			}
		}, WorkspaceDeleteGateProviderUnconfirmed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := workspaceDeleteTestInput(now)
			testCase.mutate(&input)
			if allowed, reason := PlatformRefundDispatchAllowed(input); allowed || reason != testCase.reason {
				t.Fatalf("allowed=%v reason=%q want reason=%q", allowed, reason, testCase.reason)
			}
		})
	}
}

func TestPlatformWorkspaceDeleteRefundMicrosBillsWholeHours(t *testing.T) {
	fulfilled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		deleted  time.Time
		expected int64
	}{
		{"one second of use bills one hour", fulfilled.Add(time.Second), 52_580_000 * 719 / 720},
		{"exactly one hour", fulfilled.Add(time.Hour), 52_580_000 * 719 / 720},
		{"one hour and one second bills two hours", fulfilled.Add(time.Hour + time.Second), 52_580_000 * 718 / 720},
		{"half the month", fulfilled.Add(360 * time.Hour), 52_580_000 * 360 / 720},
		{"full month in use", fulfilled.Add(720 * time.Hour), 0},
		{"beyond the month", fulfilled.Add(900 * time.Hour), 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refund, err := PlatformWorkspaceDeleteRefundMicros(52_580_000, fulfilled, testCase.deleted)
			if err != nil || refund != testCase.expected {
				t.Fatalf("refund=%d err=%v want=%d", refund, err, testCase.expected)
			}
			if refund > 52_580_000 {
				t.Fatalf("refund %d exceeds original charge", refund)
			}
		})
	}
}

func TestPlatformWorkspaceDeleteRefundMicrosRejectsInvalidInput(t *testing.T) {
	fulfilled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name    string
		charge  int64
		deleted time.Time
	}{
		{"no original charge", 0, fulfilled.Add(time.Hour)},
		{"zero fulfillment", 52_580_000, time.Time{}},
		{"deleted before fulfilled", 52_580_000, fulfilled.Add(-time.Hour)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := PlatformWorkspaceDeleteRefundMicros(testCase.charge, fulfilled, testCase.deleted); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestWorkspaceDeleteRefundIdempotencyKeyBindsPolicyVersion(t *testing.T) {
	key := WorkspaceDeleteRefundIdempotencyKey("workspace-delete-ws-alpha", WorkspaceRefundPolicyVersion)
	if key != "workspace-delete-ws-alpha:refund:workspace-delete-refund-v1" {
		t.Fatalf("key=%q", key)
	}
}

func TestClassifyWorkspaceDeleteReadbackKeepsIdentityConflictsBlocked(t *testing.T) {
	now := time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC)
	identity := workspaceDeleteTestIdentity()
	readback := workspaceDeleteTestReadback(now.Add(-time.Minute))
	if result, reason := ClassifyWorkspaceDeleteReadback(identity, readback, now, time.Hour); result != WorkspaceDeleteResultCompleted || reason != "" {
		t.Fatalf("result=%q reason=%q", result, reason)
	}
	readback.WorkspaceID = "ws-other"
	if result, _ := ClassifyWorkspaceDeleteReadback(identity, readback, now, time.Hour); result != WorkspaceDeleteResultBlockedIdentity {
		t.Fatalf("identity conflict classified as %q", result)
	}
	readback = workspaceDeleteTestReadback(now.Add(-time.Minute))
	readback.Facts[0].Present = true
	if result, _ := ClassifyWorkspaceDeleteReadback(identity, readback, now, time.Hour); result != WorkspaceDeleteResultWaiting {
		t.Fatalf("present resource classified as %q", result)
	}
}

func TestWorkspaceDeleteStageOrderIsTheSingleDeletionSequence(t *testing.T) {
	// The stage vocabulary is ordered because the retirement sequence is a
	// cross-owner invariant, not a presentation detail.
	order := WorkspaceDeleteStageOrder()
	want := []string{
		WorkspaceDeleteStageRuntimeAbsent, WorkspaceDeleteStageAttachmentAbsent, WorkspaceDeleteStageStorageAbsent,
		WorkspaceDeleteStageComputeAbsent, WorkspaceDeleteStageWorkspaceAbsent, WorkspaceDeleteStageReceiptRecorded,
	}
	if len(order) != len(want) {
		t.Fatalf("stage order=%#v", order)
	}
	seen := map[string]bool{}
	for index, stage := range order {
		if stage != want[index] || seen[stage] {
			t.Fatalf("stage order=%#v want=%#v", order, want)
		}
		seen[stage] = true
	}
	for _, stage := range want {
		if !seen[stage] {
			t.Fatalf("stage %q missing from the order", stage)
		}
	}
}

func TestWorkspaceDeleteOutcomeRetryableOwnsTheRetryDecision(t *testing.T) {
	// Fabric writes one of these on an unfinished deletion; Control Plane reads
	// the same vocabulary to keep the operation open instead of terminal.
	for _, outcome := range []string{WorkspaceDeleteOutcomePendingRetry, WorkspaceDeleteOutcomeUnconfirmedSend} {
		if !WorkspaceDeleteOutcomeRetryable(outcome) {
			t.Fatalf("%q must keep the owning operation open", outcome)
		}
	}
	// A completed deletion, an unverifiable failure, and any unknown token must
	// never be treated as retryable.
	for _, outcome := range []string{"", "completed", "unknown", "present"} {
		if WorkspaceDeleteOutcomeRetryable(outcome) {
			t.Fatalf("%q must not be retryable", outcome)
		}
	}
}

func TestPlatformRefundDispatchAllowedRefusesARetryableProviderOutcome(t *testing.T) {
	// A still-retrying deletion readback must never satisfy the refund gate: the
	// gate requires a completed deletion, not an unfinished one.
	now := time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC)
	input := workspaceDeleteTestInput(now)
	input.Readback.Result = WorkspaceDeleteResultWaiting
	input.Readback.ReasonCode = WorkspaceDeleteGateResourcePresent
	if allowed, _ := PlatformRefundDispatchAllowed(input); allowed {
		t.Fatal("an unfinished deletion authorized a refund")
	}
	input = workspaceDeleteTestInput(now)
	for index := range input.Readback.Facts {
		if input.Readback.Facts[index].Kind == WorkspaceDeleteResourceCVM {
			input.Readback.Facts[index].Present, input.Readback.Facts[index].ProviderStatus = true, "SHUTDOWN"
		}
	}
	if allowed, reason := PlatformRefundDispatchAllowed(input); allowed || reason != WorkspaceDeleteGateResourcePresent {
		t.Fatalf("shut down cvm allowed=%v reason=%q", allowed, reason)
	}
}

func TestWorkspaceDeleteStageEvidenceIsBoundToTheFrozenContract(t *testing.T) {
	observedAt := "2026-09-18T05:00:00.000000Z"
	valid := func(stage string) WorkspaceDeleteStageEvidence {
		result, kind, _ := WorkspaceDeleteStageEvidenceExpected(stage)
		return WorkspaceDeleteStageEvidence{
			Stage: stage, Result: result, EvidenceKind: kind, ResourceID: "resource-alpha",
			ObservedAt: observedAt, ReadbackID: "readback-alpha",
		}
	}
	for _, stage := range WorkspaceDeleteStageOrder() {
		if !ValidWorkspaceDeleteStageEvidence(valid(stage)) {
			t.Fatalf("stage %q evidence rejected", stage)
		}
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*WorkspaceDeleteStageEvidence)
	}{
		// A local transition may never be presented as a provider readback, and a
		// provider fact may never be presented as a local transition.
		{"attachment claimed as provider readback", func(e *WorkspaceDeleteStageEvidence) { e.EvidenceKind = WorkspaceDeleteEvidenceProviderReadback }},
		{"storage claimed as local transition", func(e *WorkspaceDeleteStageEvidence) { e.EvidenceKind = WorkspaceDeleteEvidenceLocalTransition }},
		{"unknown stage", func(e *WorkspaceDeleteStageEvidence) { e.Stage = "future_stage" }},
		{"result mismatch", func(e *WorkspaceDeleteStageEvidence) { e.Result = WorkspaceDeleteEvidenceRemoved }},
		{"missing resource identity", func(e *WorkspaceDeleteStageEvidence) { e.ResourceID = "" }},
		{"unparseable observation time", func(e *WorkspaceDeleteStageEvidence) { e.ObservedAt = "not-a-time" }},
		{"missing observation time", func(e *WorkspaceDeleteStageEvidence) { e.ObservedAt = "" }},
		{"negative attempt count", func(e *WorkspaceDeleteStageEvidence) { e.ReadAttempts = -1 }},
		{"padded reason code", func(e *WorkspaceDeleteStageEvidence) { e.ReasonCode = " spaced " }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := valid(WorkspaceDeleteStageAttachmentAbsent)
			if testCase.name == "storage claimed as local transition" {
				candidate = valid(WorkspaceDeleteStageStorageAbsent)
			}
			testCase.mutate(&candidate)
			if ValidWorkspaceDeleteStageEvidence(candidate) {
				t.Fatalf("invalid evidence accepted: %#v", candidate)
			}
		})
	}
}

func TestWorkspaceDeleteStageEvidenceCompleteRequiresTheWholeSequence(t *testing.T) {
	observedAt := "2026-09-18T05:00:00.000000Z"
	full := func() []WorkspaceDeleteStageEvidence {
		evidence := make([]WorkspaceDeleteStageEvidence, 0, len(WorkspaceDeleteStageOrder()))
		for _, stage := range WorkspaceDeleteStageOrder() {
			result, kind, _ := WorkspaceDeleteStageEvidenceExpected(stage)
			evidence = append(evidence, WorkspaceDeleteStageEvidence{
				Stage: stage, Result: result, EvidenceKind: kind, ResourceID: "resource-alpha", ObservedAt: observedAt,
				// A provider or ledger observation names the readback it came from.
				ReadbackID: "readback-" + stage,
			})
		}
		return evidence
	}
	if !WorkspaceDeleteStageEvidenceComplete(full()) {
		t.Fatal("complete evidence rejected")
	}
	// A partially recorded deletion can never satisfy the receipt contract.
	if WorkspaceDeleteStageEvidenceComplete(full()[:3]) {
		t.Fatal("partial evidence accepted")
	}
	if WorkspaceDeleteStageEvidenceComplete(nil) {
		t.Fatal("empty evidence accepted")
	}
	// A duplicated stage is not the sequence.
	duplicated := append(full(), full()[0])
	if WorkspaceDeleteStageEvidenceComplete(duplicated) {
		t.Fatal("duplicated stage accepted")
	}
	// An out-of-order list is not the sequence.
	reordered := full()
	reordered[0], reordered[1] = reordered[1], reordered[0]
	if WorkspaceDeleteStageEvidenceComplete(reordered) {
		t.Fatal("reordered evidence accepted")
	}
}

func TestWorkspaceDeleteStageEvidenceDigestsLeakNoPrivateIdentity(t *testing.T) {
	evidence := []WorkspaceDeleteStageEvidence{{
		Stage: WorkspaceDeleteStageStorageAbsent, Result: WorkspaceDeleteEvidenceAbsent,
		EvidenceKind: WorkspaceDeleteEvidenceProviderReadback, ResourceID: "storage-alpha",
		ProviderResourceID: "disk-private-alpha", ObservedAt: "2026-09-18T05:00:00.000000Z",
		ReadbackID: "readback-private-alpha", ReadAttempts: 2,
	}}
	digests := WorkspaceDeleteStageEvidenceDigests(evidence)
	if len(digests) != 1 || digests[0].Stage != WorkspaceDeleteStageStorageAbsent ||
		digests[0].Result != WorkspaceDeleteEvidenceAbsent || digests[0].ObservedAt != evidence[0].ObservedAt {
		t.Fatalf("digests=%#v", digests)
	}
	for _, leaked := range []string{"disk-private-alpha", "readback-private-alpha", "storage-alpha"} {
		if strings.Contains(string(mustJSONContract(t, digests)), leaked) {
			t.Fatalf("digest leaked %q: %#v", leaked, digests)
		}
	}
}

func TestWorkspaceDeleteStageEvidenceDigestListRejectsUnknownShape(t *testing.T) {
	valid := []WorkspaceDeleteStageEvidenceDigest{{Stage: WorkspaceDeleteStageStorageAbsent, Result: WorkspaceDeleteEvidenceAbsent, EvidenceKind: WorkspaceDeleteEvidenceProviderReadback, ObservedAt: "2026-09-18T05:00:00.000000Z"}}
	decoded, ok := WorkspaceDeleteStageEvidenceDigestList(valid)
	if !ok || len(decoded) != 1 {
		t.Fatalf("in-process digests=%#v ok=%v", decoded, ok)
	}
	// A JSON-decoded shape must decode identically, and an unknown field is refused
	// rather than silently dropped.
	encoded := []any{map[string]any{"stage": WorkspaceDeleteStageStorageAbsent, "result": WorkspaceDeleteEvidenceAbsent, "evidenceKind": WorkspaceDeleteEvidenceProviderReadback, "observedAt": "2026-09-18T05:00:00.000000Z"}}
	if decoded, ok := WorkspaceDeleteStageEvidenceDigestList(encoded); !ok || len(decoded) != 1 {
		t.Fatalf("json digests=%#v ok=%v", decoded, ok)
	}
	encoded[0].(map[string]any)["providerResourceId"] = "disk-private-alpha"
	if _, ok := WorkspaceDeleteStageEvidenceDigestList(encoded); ok {
		t.Fatal("a digest carrying an unknown field was accepted")
	}
	if _, ok := WorkspaceDeleteStageEvidenceDigestList("not-a-list"); ok {
		t.Fatal("a non-list summary was accepted")
	}
	if _, ok := WorkspaceDeleteStageEvidenceDigestList(nil); ok {
		t.Fatal("an absent summary was accepted as a list")
	}
}

func mustJSONContract(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// A stage that is still waiting or that failed is recorded so a stalled deletion
// can be explained, but it never counts as a confirmation: only the result the
// frozen contract requires for that stage may satisfy a receipt.
func TestWorkspaceDeleteStageEvidenceSeparatesWaitingFromConfirmed(t *testing.T) {
	observedAt := "2026-09-18T05:00:00.000000Z"
	waiting := WorkspaceDeleteStageEvidence{
		Stage: WorkspaceDeleteStageStorageAbsent, Result: WorkspaceDeleteEvidenceWaiting,
		EvidenceKind: WorkspaceDeleteEvidenceProviderReadback, ResourceID: "storage-alpha",
		ObservedAt: observedAt, ReadbackID: "readback-alpha", ReadAttempts: 1, MutationAttempts: 1,
	}
	if !ValidWorkspaceDeleteStageEvidence(waiting) {
		t.Fatal("a waiting observation is valid evidence")
	}
	if waiting.Confirmed() {
		t.Fatal("a waiting observation must not be a confirmation")
	}
	failed := waiting
	failed.Result = WorkspaceDeleteEvidenceFailed
	if !ValidWorkspaceDeleteStageEvidence(failed) || failed.Confirmed() {
		t.Fatalf("failed observation=%#v", failed)
	}
	// The confirmation of the same stage is a different result.
	confirmed := waiting
	confirmed.Result, confirmed.ReadAttempts = WorkspaceDeleteEvidenceAbsent, 2
	if !confirmed.Confirmed() {
		t.Fatal("the expected result is a confirmation")
	}
	// A result that belongs to no stage outcome is refused outright.
	if ValidWorkspaceDeleteStageEvidence(WorkspaceDeleteStageEvidence{
		Stage: WorkspaceDeleteStageStorageAbsent, Result: "unknown", EvidenceKind: WorkspaceDeleteEvidenceProviderReadback,
		ResourceID: "storage-alpha", ObservedAt: observedAt, ReadbackID: "readback-alpha",
	}) {
		t.Fatal("an unknown result was accepted")
	}
}

// A provider or ledger observation must name the readback it came from; without it
// the fact cannot be traced back to what was actually read.
func TestWorkspaceDeleteProviderEvidenceRequiresItsReadback(t *testing.T) {
	observedAt := "2026-09-18T05:00:00.000000Z"
	for _, stage := range []string{WorkspaceDeleteStageRuntimeAbsent, WorkspaceDeleteStageStorageAbsent, WorkspaceDeleteStageComputeAbsent, WorkspaceDeleteStageReceiptRecorded} {
		result, kind, _ := WorkspaceDeleteStageEvidenceExpected(stage)
		evidence := WorkspaceDeleteStageEvidence{
			Stage: stage, Result: result, EvidenceKind: kind, ResourceID: "resource-alpha", ObservedAt: observedAt,
		}
		if ValidWorkspaceDeleteStageEvidence(evidence) {
			t.Fatalf("stage %q accepted without a readback reference", stage)
		}
		evidence.ReadbackID = "readback-alpha"
		if !ValidWorkspaceDeleteStageEvidence(evidence) {
			t.Fatalf("stage %q rejected with its readback reference", stage)
		}
	}
	// A committed local transition has no provider readback to name.
	local := WorkspaceDeleteStageEvidence{
		Stage: WorkspaceDeleteStageAttachmentAbsent, Result: WorkspaceDeleteEvidenceReleased,
		EvidenceKind: WorkspaceDeleteEvidenceLocalTransition, ResourceID: "attachment-alpha", ObservedAt: observedAt,
	}
	if !ValidWorkspaceDeleteStageEvidence(local) {
		t.Fatal("a committed local transition was rejected")
	}
}

// An unavailable observation states that a read returned nothing. It may never carry
// the stage's confirmation, never name a readback it did not obtain, and never satisfy
// a receipt — so an unfinished stage stays explainable without fabricating a provider
// fact.
func TestWorkspaceDeleteUnavailableObservationIsExplicitAndNeverConfirms(t *testing.T) {
	observedAt := "2026-09-18T05:00:00.000000Z"
	for _, stage := range WorkspaceDeleteStageOrder() {
		expectedResult, expectedKind, _ := WorkspaceDeleteStageEvidenceExpected(stage)
		unavailable := WorkspaceDeleteStageEvidence{
			Stage: stage, Result: WorkspaceDeleteEvidenceWaiting, EvidenceKind: WorkspaceDeleteEvidenceUnavailable,
			ResourceID: "resource-alpha", ObservedAt: observedAt, ReadAttempts: 1, MutationAttempts: 1,
		}
		if !ValidWorkspaceDeleteStageEvidence(unavailable) {
			t.Fatalf("stage %q refused an explicit unavailable observation", stage)
		}
		if unavailable.Confirmed() {
			t.Fatalf("stage %q treated an unavailable observation as a confirmation", stage)
		}
		// Claiming the confirmation while naming no readback is refused.
		claimed := unavailable
		claimed.Result, claimed.EvidenceKind = expectedResult, expectedKind
		if expectedKind != WorkspaceDeleteEvidenceLocalTransition && ValidWorkspaceDeleteStageEvidence(claimed) {
			t.Fatalf("stage %q accepted a confirmation without its readback", stage)
		}
		// An unavailable observation never names a readback.
		withReference := unavailable
		withReference.ReadbackID = "readback-alpha"
		if ValidWorkspaceDeleteStageEvidence(withReference) {
			t.Fatalf("stage %q accepted an unavailable observation naming a readback", stage)
		}
	}
	// A receipt digest list requires the stage's own confirmation, so an unavailable
	// observation can never be published as receipt evidence.
	digests := []WorkspaceDeleteStageEvidenceDigest{{
		Stage: WorkspaceDeleteStageRuntimeAbsent, Result: WorkspaceDeleteEvidenceWaiting,
		EvidenceKind: WorkspaceDeleteEvidenceUnavailable, ObservedAt: observedAt, EvidenceRef: "deletion-evidence:unavailable",
	}}
	if ValidWorkspaceDeleteStageEvidenceDigests(append(digests, WorkspaceDeleteStageEvidenceDigest{
		Stage: WorkspaceDeleteStageAttachmentAbsent, Result: WorkspaceDeleteEvidenceReleased,
		EvidenceKind: WorkspaceDeleteEvidenceLocalTransition, ObservedAt: observedAt, EvidenceRef: "deletion-evidence:attachment",
	})) {
		t.Fatal("a receipt accepted an unavailable observation")
	}
}
