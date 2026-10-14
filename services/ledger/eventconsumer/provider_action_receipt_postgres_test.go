package eventconsumer

import (
	"context"

	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/ledger/internal/ledger"
)

// providerActionDeletionRequest is the request the Workspace deletion path
// actually builds at services/workspace/internal/launch/deletion.go:744-753. It is
// transcribed field for field from deletionEvidence(op) and continuation(op,...)
// rather than invented here, so this test proves the real closure shape reaches
// the Ledger writer: a PROVIDER_ACTION receipt with the Workspace owner commit and
// the digest that commit froze, carrying no QuoteAcceptance and no WalletOperation.
func providerActionDeletionRequest(workspaceID, operationID string) *api.AppendReceiptRequest {
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-local"}}}
	digest := "sha256:" + strings.Repeat("c", 64)
	commit := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: operationID, ResourceId: workspaceID,
		AcceptedInputDigest: digest, CommittedVersion: 1, AcceptedAt: timestamppb.Now(), AuthorizationContextId: "accepted-deletion-authorization",
		ActorId: "actor-local", Scope: scope, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE,
		AuthorizationResource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		ContinuationResources: []*api.AuthorizationResource{{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)}}}
	return &api.AppendReceiptRequest{
		Context:                &api.CallContext{RequestId: "req-delete-original:record_deletion_receipt", IdempotencyKey: operationID + ":record_deletion_receipt", ActorId: "actor-local", Scope: scope, AcceptedOperationGrantId: proto.String("grant-local")},
		Receipt:                &api.Receipt{Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(operationID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: "Workspace deletion confirmed: the original application runtime and provider resources are absent."},
		EvidenceDigest:         digest,
		OwnerEvidenceReference: operationID,
		OwnerCommitEvidence:    commit,
	}
}

// TestProviderActionDeletionReceiptPostgresWirePersistence is the behaviour the
// deletion closure depends on. Against a real Ledger process over real gRPC and
// isolated PostgreSQL it proves the confirmed absence is admitted into its own
// writer, persisted, and — the step deletion.go:761 depends on — resolvable again
// by the original owner reference so the refund may be issued against evidence
// that really exists.
func TestProviderActionDeletionReceiptPostgresWirePersistence(t *testing.T) {
	db, s, owners, client, _ := coordinatedService(t)
	ctx := context.Background()
	request := providerActionDeletionRequest("workspace-local", "op_delete_original")
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)

	appended, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatalf("append confirmed deletion evidence: %v", err)
	}
	if appended.GetId() == "" || appended.GetCreatedAt() == nil || appended.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION ||
		appended.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || appended.GetOperationId() != "op_delete_original" {
		t.Fatalf("receipt=%v", appended)
	}
	// A lost response replays the identical receipt rather than recording the same
	// confirmed absence twice.
	replay := proto.Clone(request).(*api.AppendReceiptRequest)
	replay.Context.RequestId = "req-delete-original:record_deletion_receipt_retry"
	replay.Context.IdempotencyKey = request.Context.IdempotencyKey + ":retry"
	second, err := client.AppendReceipt(ctx, replay)
	if err != nil || !proto.Equal(appended, second) {
		t.Fatalf("replay differs: %v %v", second, err)
	}
	read := &api.GetReceiptByReferenceRequest{Context: replay.Context, Owner: "workspace", OwnerEvidenceReference: request.OwnerEvidenceReference}
	public, err := client.ReadReceiptByReference(ctx, read)
	if err != nil || !proto.Equal(public, appended) {
		t.Fatalf("public read-by-reference=%v %v, want the stored deletion receipt", public, err)
	}
	// The evidence envelope is the persisted shape of the stored row. It is read
	// through the store rather than over the wire: no owner asks for the typed
	// absence, because a caller that already holds the appended Receipt reads it
	// back in Receipt space, which is what the two assertions above proved.
	evidence, err := ledger.NewPostgresStore(db).ReadProviderActionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(evidence.Evidence.GetReceipt(), appended) || !proto.Equal(evidence.Evidence.GetOwnerCommitEvidence(), request.OwnerCommitEvidence) ||
		evidence.Evidence.GetEvidenceDigest() != request.EvidenceDigest {
		t.Fatalf("stored provider-action evidence differs")
	}
	// A restart reads the same persisted evidence from the same row.
	restarted, err := ledger.NewPostgresStore(db).ReadProviderActionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil || !proto.Equal(restarted.Evidence, evidence.Evidence) {
		t.Fatalf("restart read=%v", err)
	}
	stored := evidence
	if stored.WorkspaceID != "workspace-local" || stored.TenantID != "tenant-local" || stored.ActorID != "actor-local" {
		t.Fatalf("stored identity workspace=%q tenant=%q actor=%q", stored.WorkspaceID, stored.TenantID, stored.ActorID)
	}
	// The Workspace identity comes from the owner commit, and the persisted payload
	// carries no funding reference at all, so a confirmed absence can never be read
	// as a settled or a zero charge.
	var workspaceID, payload string
	if err = db.QueryRowContext(ctx, `SELECT workspace_id,payload_json FROM evidence_receipts WHERE receipt_type=$1`, ledger.ProviderActionReceiptType).Scan(&workspaceID, &payload); err != nil {
		t.Fatal(err)
	}
	if workspaceID != "workspace-local" {
		t.Fatalf("workspace_id=%q, want the owner commit's resource id", workspaceID)
	}
	for _, forbidden := range []string{"quoteAcceptance", "walletOperation", "amountUsdMicros", "billingMode"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("absence receipt carries a funding reference %q: %s", forbidden, payload)
		}
	}
	// The same reference under different bytes is refused, never replaced.
	conflict := proto.Clone(request).(*api.AppendReceiptRequest)
	conflict.Receipt.EvidenceSummary = "a different deletion summary"
	if _, err = client.AppendReceipt(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting evidence=%v, want AlreadyExists", err)
	}
	// The generic HTTP receipt writer must never mint this typed evidence.
	if _, err = s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: ledger.ProviderActionReceiptType, Status: "completed", Surface: "cloud", WorkspaceID: "workspace-local", IdempotencyKey: "forged-provider-action"}); err != ledger.ErrInvalidReceiptInput {
		t.Fatalf("generic writer accepted the provider-action type: %v", err)
	}
	var rows int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.ProviderActionReceiptType).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("receipt rows=%d %v, want exactly one confirmed absence", rows, err)
	}
}

// TestProviderActionDeletionReceiptRefusesForeignOrInventedEvidence proves the new
// authority is not a softer one than the two funded kinds. The Workspace owner
// commit is the only evidence, so a receipt whose commit the Workspace owner did
// not accept is refused before anything is stored, and a caller cannot supply the
// receipt identity, the Workspace identity or the outcome itself.
func TestProviderActionDeletionReceiptRefusesForeignOrInventedEvidence(t *testing.T) {
	db, _, owners, client, _ := coordinatedService(t)
	ctx := context.Background()
	base := providerActionDeletionRequest("workspace-local", "op_delete_original")
	owners.commit = proto.Clone(base.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	for _, tc := range []struct {
		name   string
		mutate func(*api.AppendReceiptRequest)
		code   codes.Code
	}{
		{"create obligation instead of a deletion", func(r *api.AppendReceiptRequest) {
			r.OwnerCommitEvidence.AcceptedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE
		}, codes.InvalidArgument},
		{"resource id that is not the authorized resource", func(r *api.AppendReceiptRequest) {
			r.OwnerCommitEvidence.ResourceId = "workspace-foreign"
		}, codes.InvalidArgument},
		{"absence committed against a workspace the actor may not authorize", func(r *api.AppendReceiptRequest) {
			r.OwnerCommitEvidence.ResourceId = "workspace-foreign"
			r.OwnerCommitEvidence.AuthorizationResource.Id = proto.String("workspace-foreign")
			r.OwnerCommitEvidence.ContinuationResources[0].Id = proto.String("workspace-foreign")
		}, codes.PermissionDenied},
		{"invention of the commit itself", func(r *api.AppendReceiptRequest) {
			r.OwnerCommitEvidence.CommittedVersion++
		}, codes.FailedPrecondition},
		{"caller receipt id", func(r *api.AppendReceiptRequest) { r.Receipt.Id = "invented" }, codes.InvalidArgument},
		{"caller created_at", func(r *api.AppendReceiptRequest) { r.Receipt.CreatedAt = timestamppb.Now() }, codes.InvalidArgument},
		{"unconfirmed absence", func(r *api.AppendReceiptRequest) {
			r.Receipt.Outcome = api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_UNKNOWN
		}, codes.InvalidArgument},
		{"digest that is not the commit's accepted input", func(r *api.AppendReceiptRequest) {
			r.EvidenceDigest = "sha256:" + strings.Repeat("d", 64)
		}, codes.InvalidArgument},
		{"foreign actor", func(r *api.AppendReceiptRequest) { r.Context.ActorId = "actor-foreign" }, codes.InvalidArgument},
		{"missing owner commit", func(r *api.AppendReceiptRequest) { r.OwnerCommitEvidence = nil }, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := proto.Clone(base).(*api.AppendReceiptRequest)
			r.Context.IdempotencyKey = "idempotency-" + strings.ReplaceAll(tc.name, " ", "-")
			tc.mutate(r)
			if _, err := client.AppendReceipt(ctx, r); status.Code(err) != tc.code {
				t.Fatalf("code=%v err=%v, want %v", status.Code(err), err, tc.code)
			}
		})
	}
	var stored int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.ProviderActionReceiptType).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("a refused absence was stored: rows=%d %v", stored, err)
	}
}

// TestReadReceiptByReferenceStillRefusesAnUnknownReference proves the third
// readback did not turn the public resolver into a success-by-default: a
// reference no writer ever recorded is still NotFound after all three arms.
func TestReadReceiptByReferenceStillRefusesAnUnknownReference(t *testing.T) {
	_, _, _, client, _ := coordinatedService(t)
	ctx := context.Background()
	_, err := client.ReadReceiptByReference(ctx, &api.GetReceiptByReferenceRequest{
		Context: &api.CallContext{RequestId: "req-read-unknown", IdempotencyKey: "idempotency-read-unknown", ActorId: "actor-local",
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-local"}}}, AcceptedOperationGrantId: proto.String("grant-local")},
		Owner:                  "workspace",
		OwnerEvidenceReference: "op_delete_never_recorded",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code=%v err=%v, want NotFound", status.Code(err), err)
	}
}

// TestProviderActionReceiptCarriesNoFundingIdentityInItsPayload states the reason
// the evidence message has its own shape instead of reusing either funded one:
// decoding the persisted row must not be able to produce a quote acceptance or a
// wallet operation, so no reader can treat an absence as a charge.
func TestProviderActionReceiptCarriesNoFundingIdentityInItsPayload(t *testing.T) {
	request := providerActionDeletionRequest("workspace-local", "op_delete_original")
	if request.GetQuoteAcceptance() != nil || request.GetWalletOperation() != nil {
		t.Fatal("the deletion request must carry neither a quote acceptance nor a wallet operation")
	}
	if err := ledger.ValidateProviderActionReceiptInput(request); err != nil {
		t.Fatalf("the real deletion evidence shape was refused: %v", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil || strings.Contains(string(encoded), "quoteAcceptance") || strings.Contains(string(encoded), "walletOperation") {
		t.Fatalf("serialized request carries a funding field: %s %v", encoded, err)
	}
	// A funded shape is not provider-action evidence: the same writer refuses it.
	funded := coordinationFixtureRequest()
	if err := ledger.ValidateProviderActionReceiptInput(funded); err == nil {
		t.Fatal("a zero-charge order was accepted as provider-action absence evidence")
	}
}
