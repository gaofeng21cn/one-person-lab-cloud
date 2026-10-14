package eventconsumer

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/ledger/internal/ledger"
)

// redactingCall returns a read call for the same Workspace under a different
// actor, and one under a different tenant. The Workspace and the stored reference
// are identical in both cases, so only the caller identity varies.
func redactingCall(reference, actorID, tenantID string) *api.GetReceiptByReferenceRequest {
	return &api.GetReceiptByReferenceRequest{
		Context: &api.CallContext{RequestId: "req-" + actorID + "-" + tenantID, IdempotencyKey: "idem-" + actorID + "-" + tenantID, ActorId: actorID,
			Scope:                    &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}},
			AcceptedOperationGrantId: proto.String("grant-local")},
		Owner: "workspace", OwnerEvidenceReference: reference,
	}
}

const (
	commitActor   = "actor-local"
	foreignActor  = "actor-foreign"
	commitTenant  = "tenant-local"
	foreignTenant = "tenant-foreign"
)

// TestProviderActionReadAuthorizationSurvivesPrivacyRedaction is the regression for
// the authorization defect this slice introduced.
//
// A stored receipt's own provenance block is caller-owned and privacy-mutable: a
// privacy redaction clears receipt.Actor. When that block was the read authority,
// redaction silently removed the actor fence — Authorizer treats an empty scope
// actor as "no fence" rather than "refuse" — so a foreign actor that was denied
// before redaction was admitted after it, while the immutable OwnerCommitEvidence
// inside the evidence blob still carried the real committing actor.
//
// Read authority is therefore bound to that commit. This proves all three legs
// over real Ledger gRPC and real PostgreSQL, before and after the mutation:
//
//  1. a different actor stays denied on both sides of the mutation;
//  2. the authorized actor keeps its read;
//  3. the Tenant fence stays intact, because tenant identity is not redacted and
//     was never sourced from the mutable block.
//
// It also pins the privacy promise itself: after redaction the stored provenance
// actor really is gone, so the fix holds the fence by moving authority, not by
// keeping unredacted data in the place redaction is supposed to clear.
func TestProviderActionReadAuthorizationSurvivesPrivacyRedaction(t *testing.T) {
	db, s, owners, client, _ := coordinatedService(t)
	ctx := context.Background()
	request := providerActionDeletionRequest("workspace-local", "op_delete_original")
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)

	appended, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatalf("append confirmed deletion evidence: %v", err)
	}
	read := func(actorID, tenantID string) error {
		_, readErr := client.ReadReceiptByReference(ctx, redactingCall(request.OwnerEvidenceReference, actorID, tenantID))
		return readErr
	}

	// Before redaction: the committing actor reads, a foreign actor does not, and a
	// foreign tenant does not.
	if err = read(commitActor, commitTenant); err != nil {
		t.Fatalf("committing actor refused before redaction: %v", err)
	}
	if err = read(foreignActor, commitTenant); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign actor before redaction: code=%v err=%v, want PermissionDenied", status.Code(err), err)
	}
	if err = read(commitActor, foreignTenant); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign tenant before redaction: code=%v err=%v, want PermissionDenied", status.Code(err), err)
	}

	// Mutate: the generic privacy endpoint accepts any receipt id with no type
	// restriction, and clears the stored provenance block.
	if _, err = s.store.PrivacyDeleteReceipt(ctx, ledger.ReceiptPrivacyDeleteInput{ReceiptID: appended.GetId(), IdempotencyKey: "privacy-original-deletion", Reason: "data subject request"}); err != nil {
		t.Fatalf("privacy delete: %v", err)
	}

	// The redaction really happened: the mutable provenance projection is now empty.
	redacted, err := ledger.NewPostgresStore(db).ReadProviderActionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil {
		t.Fatal(err)
	}
	if redacted.ActorID != "" {
		t.Fatalf("stored provenance actor=%q, want empty after redaction", redacted.ActorID)
	}
	// The immutable commit that carries the authority survived it.
	if got := redacted.Evidence.GetOwnerCommitEvidence().GetActorId(); got != commitActor {
		t.Fatalf("commit evidence actor=%q, want the immutable %q", got, commitActor)
	}
	if !proto.Equal(redacted.Evidence.GetReceipt(), appended) {
		t.Fatalf("redaction changed the stored receipt: %v", redacted.Evidence.GetReceipt())
	}

	// After redaction all three legs still hold. This is the assertion that failed
	// before the fix: the foreign actor was admitted here.
	if err = read(foreignActor, commitTenant); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign actor after redaction: code=%v err=%v, want PermissionDenied", status.Code(err), err)
	}
	if err = read(commitActor, foreignTenant); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign tenant after redaction: code=%v err=%v, want PermissionDenied", status.Code(err), err)
	}
	if err = read(commitActor, commitTenant); err != nil {
		t.Fatalf("committing actor refused after redaction: %v", err)
	}
	readback, err := client.ReadReceiptByReference(ctx, redactingCall(request.OwnerEvidenceReference, commitActor, commitTenant))
	if err != nil || !proto.Equal(readback, appended) {
		t.Fatalf("authorized readback=%v err=%v, want the stored deletion receipt", readback, err)
	}
}

// TestRedactedReceiptWithoutACommittingActorIsRefusedNotWidened proves the
// fail-closed half of the boundary. An evidence blob that names no committing actor
// must not degrade into "no actor fence", because Authorizer treats an empty scope
// actor as an unrestricted scope rather than as a refusal.
//
// The writer refuses to record such evidence, so this reaches the stored row the only
// way it can exist: a row whose commit evidence lost its actor after the fact. The
// read must then be refused, not widened.
func TestRedactedReceiptWithoutACommittingActorIsRefusedNotWidened(t *testing.T) {
	db, _, owners, client, _ := coordinatedService(t)
	ctx := context.Background()
	request := providerActionDeletionRequest("workspace-local", "op_delete_original")
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	appended, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatalf("append confirmed deletion evidence: %v", err)
	}

	// Strip the committing actor from the persisted commit evidence, simulating an
	// evidence blob that carries no authority at all.
	var payload string
	if err = db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE id=$1`, appended.GetId()).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err = json.Unmarshal([]byte(payload), &stored); err != nil {
		t.Fatal(err)
	}
	refs, ok := stored["inputRefs"].(map[string]any)
	if !ok {
		t.Fatalf("stored payload has no inputRefs: %s", payload)
	}
	evidence, ok := refs["providerAction"].(map[string]any)
	if !ok {
		t.Fatalf("stored payload has no providerAction evidence: %s", payload)
	}
	commit, ok := evidence["ownerCommitEvidence"].(map[string]any)
	if !ok {
		t.Fatalf("stored evidence has no ownerCommitEvidence: %s", payload)
	}
	delete(commit, "actorId")
	stripped, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE evidence_receipts SET payload_json=$1 WHERE id=$2`, string(stripped), appended.GetId()); err != nil {
		t.Fatal(err)
	}

	// Any caller, including the original committing actor, is refused outright. The
	// read is not silently admitted without its fence.
	for _, tc := range []struct{ actorID, tenantID string }{
		{commitActor, commitTenant}, {foreignActor, commitTenant},
	} {
		_, err = client.ReadReceiptByReference(ctx, redactingCall(request.OwnerEvidenceReference, tc.actorID, tc.tenantID))
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("actor=%q read with no committing actor: code=%v err=%v, want FailedPrecondition", tc.actorID, status.Code(err), err)
		}
	}
}
