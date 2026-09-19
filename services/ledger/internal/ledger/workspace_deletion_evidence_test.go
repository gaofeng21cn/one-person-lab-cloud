package ledger

import (
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// deletionEvidenceDigests builds the digest list a receipt carries: every stage the
// receipt can attest, in the frozen order.
func deletionEvidenceDigests() []contracts.WorkspaceDeleteStageEvidenceDigest {
	order := contracts.WorkspaceDeleteStageOrder()
	digests := make([]contracts.WorkspaceDeleteStageEvidenceDigest, 0, len(order)-1)
	for _, stage := range order[:len(order)-1] {
		result, kind, _ := contracts.WorkspaceDeleteStageEvidenceExpected(stage)
		digests = append(digests, contracts.WorkspaceDeleteStageEvidenceDigest{
			Stage: stage, Result: result, EvidenceKind: kind, ObservedAt: "2026-09-18T05:00:00.000000Z",
			// The digest binds the confirmation to the resource and readback it came
			// from, so the reference is part of the recorded structure.
			EvidenceRef: "deletion-evidence:" + stage,
		})
	}
	return digests
}

func withDeletionEvidence(input ReceiptInput, evidence any, version any) ReceiptInput {
	input.Execution["stageEvidence"] = evidence
	input.Execution["stageEvidenceSchemaVersion"] = version
	return input
}

// Ledger records a deletion receipt that carries the complete stage evidence and
// refuses one whose evidence is partial, mislabelled or malformed.
func TestWorkspaceDeletionReceiptValidatesStageEvidence(t *testing.T) {
	if !validWorkspaceDeletionReceipt(validWorkspaceDeletionReceiptInput()) {
		t.Fatal("a retained receipt without a summary must stay readable")
	}
	complete := withDeletionEvidence(validWorkspaceDeletionReceiptInput(), deletionEvidenceDigests(), int64(1))
	if !validWorkspaceDeletionReceipt(complete) {
		t.Fatal("complete stage evidence was refused")
	}

	digests := deletionEvidenceDigests()
	for _, testCase := range []struct {
		name     string
		evidence any
		version  any
	}{
		{"missing version", digests, nil},
		{"unknown version", digests, int64(2)},
		{"partial evidence", digests[:3], int64(1)},
		{"empty evidence", []contracts.WorkspaceDeleteStageEvidenceDigest{}, int64(1)},
		{"nil evidence with a version", nil, int64(1)},
		{"out of order", func() []contracts.WorkspaceDeleteStageEvidenceDigest {
			reordered := append([]contracts.WorkspaceDeleteStageEvidenceDigest(nil), digests...)
			reordered[0], reordered[1] = reordered[1], reordered[0]
			return reordered
		}(), int64(1)},
		{"mislabelled evidence kind", func() []contracts.WorkspaceDeleteStageEvidenceDigest {
			mislabelled := append([]contracts.WorkspaceDeleteStageEvidenceDigest(nil), digests...)
			mislabelled[1].EvidenceKind = contracts.WorkspaceDeleteEvidenceProviderReadback
			return mislabelled
		}(), int64(1)},
		{"wrong result", func() []contracts.WorkspaceDeleteStageEvidenceDigest {
			wrong := append([]contracts.WorkspaceDeleteStageEvidenceDigest(nil), digests...)
			wrong[2].Result = contracts.WorkspaceDeleteEvidenceRemoved
			return wrong
		}(), int64(1)},
		{"unparseable observation time", func() []contracts.WorkspaceDeleteStageEvidenceDigest {
			broken := append([]contracts.WorkspaceDeleteStageEvidenceDigest(nil), digests...)
			broken[0].ObservedAt = "not-a-time"
			return broken
		}(), int64(1)},
		// A confirmation that does not name the evidence behind it cannot be
		// attributed to what was observed, so it is refused.
		{"missing evidence reference", func() []contracts.WorkspaceDeleteStageEvidenceDigest {
			unattributed := append([]contracts.WorkspaceDeleteStageEvidenceDigest(nil), digests...)
			unattributed[2].EvidenceRef = ""
			return unattributed
		}(), int64(1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if validWorkspaceDeletionReceipt(withDeletionEvidence(validWorkspaceDeletionReceiptInput(), testCase.evidence, testCase.version)) {
				t.Fatalf("invalid evidence accepted: %#v", testCase.evidence)
			}
		})
	}
}

// The summary arrives over HTTP, so the recorded value must round-trip through the
// real decoder; a shape the decoder cannot read is refused.
func TestWorkspaceDeletionReceiptDecodesStageEvidenceFromJSON(t *testing.T) {
	digests := deletionEvidenceDigests()
	decoded, ok := contracts.WorkspaceDeleteStageEvidenceDigestList(digests)
	if !ok || !contracts.ValidWorkspaceDeleteStageEvidenceDigests(decoded) {
		t.Fatalf("in-process digests did not decode: %#v", decoded)
	}
	// A JSON-decoded shape (what the HTTP boundary produces) must decode the same.
	encoded := make([]any, 0, len(digests))
	for _, digest := range digests {
		encoded = append(encoded, map[string]any{
			"stage": digest.Stage, "result": digest.Result, "evidenceKind": digest.EvidenceKind,
			"observedAt": digest.ObservedAt, "evidenceRef": digest.EvidenceRef,
		})
	}
	decoded, ok = contracts.WorkspaceDeleteStageEvidenceDigestList(encoded)
	if !ok || !contracts.ValidWorkspaceDeleteStageEvidenceDigests(decoded) {
		t.Fatalf("JSON-decoded digests did not decode: %#v", decoded)
	}
	// An unknown field is refused instead of silently dropped.
	encoded[0].(map[string]any)["resourceId"] = "storage-alpha"
	if _, ok := contracts.WorkspaceDeleteStageEvidenceDigestList(encoded); ok {
		t.Fatal("a summary carrying an unknown field was accepted")
	}
}
