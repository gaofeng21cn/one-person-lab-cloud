package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

// DeletionReceiptType is the evidence type of a confirmed Workspace deletion
// recorded through the typed coordination surface. The Workspace owner records it
// after Serve confirmed the original runtime retirement and Fabric confirmed the
// original provider resources absent; the refund then names that receipt as its
// entitlement.
//
// It is deliberately distinct from "workspace.deleted.v1", which the retained
// Control Plane writer still emits for the historical deletion obligation, from
// the funding types, and from every other evidence kind: a Cloud deletion cannot
// stand in for a legacy deletion, a charge, or the reverse, and a refund cannot
// be issued against evidence that was never recorded.
const DeletionReceiptType = "workspace.deletion_confirmed.v1"

const deletionReceiptService = "ledger.workspace_deletion"

type DeletionRecord struct {
	Evidence                       *api.Receipt
	TenantID, ActorID, WorkspaceID string
	EvidenceDigest                 string
}

// ValidateDeletionReceiptInput checks the evidence shape, not its authority. The
// authenticated coordination handler re-reads the Workspace owner commit before
// calling the dedicated writer, and the generic HTTP writer rejects this type.
func ValidateDeletionReceiptInput(r *api.AppendReceiptRequest) error {
	c, receipt := r.GetOwnerCommitEvidence(), r.GetReceipt()
	call := r.GetContext()
	if c == nil || receipt == nil || call == nil || call.GetIdempotencyKey() == "" || len(call.GetIdempotencyKey()) > 512 ||
		call.GetScope().GetTenant().GetTenantId() == "" || call.GetActorId() == "" || r.GetOwnerEvidenceReference() == "" ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetArtifactDigest() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetOwnerEvidenceReference() || len(receipt.GetEvidenceSummary()) > 1024 ||
		!artifactDigest.MatchString(r.GetEvidenceDigest()) {
		return ErrInvalidReceiptInput
	}
	// The deletion's own owner commit is the accepted DELETEWORKSPACE obligation,
	// so the recorded evidence names the deletion that produced it rather than an
	// unrelated launch or another Workspace.
	if c.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || c.GetOperationId() != r.GetOwnerEvidenceReference() ||
		c.GetActorId() != call.GetActorId() || !proto.Equal(c.GetScope(), call.GetScope()) ||
		!artifactDigest.MatchString(c.GetAcceptedInputDigest()) || c.GetCommittedVersion() < 1 ||
		c.GetAcceptedAt() == nil || c.GetAcceptedAt().CheckValid() != nil || c.GetAuthorizationContextId() == "" ||
		c.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE ||
		c.GetResourceId() == "" || c.GetResourceId() != c.GetAuthorizationResource().GetId() {
		return ErrInvalidReceiptInput
	}
	return nil
}

func deletionReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{deletionReceiptService, "workspace", reference})
	return deletionReceiptService + ":reference:" + hash
}

// RecordDeletionReceipt uses the existing immutable evidence store. Two scoped
// locks keep the caller key and original owner reference atomic, exactly as the
// funding paths do, so a lost response replays the identical deletion receipt
// rather than recording a second one.
func (s *PostgresStore) RecordDeletionReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*DeletionRecord, error) {
	if err := ValidateDeletionReceiptInput(r); err != nil {
		return nil, err
	}
	if authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	evidence := proto.Clone(r.Receipt).(*api.Receipt)
	evidence.Id, evidence.CreatedAt = "", nil
	raw, err := protojson.Marshal(evidence)
	if err != nil {
		return nil, ErrInvalidReceiptInput
	}
	var normalized any
	if json.Unmarshal(raw, &normalized) != nil || containsForbiddenReceiptKey(normalized) {
		return nil, ErrInvalidReceiptInput
	}
	requestHash, err := hashJSON(normalized)
	if err != nil {
		return nil, err
	}
	tenant, actor := r.Context.GetScope().GetTenant().GetTenantId(), r.Context.GetActorId()
	referenceKey := deletionReceiptReferenceKey(r.OwnerEvidenceReference)
	callHash, _ := hashJSON([]string{tenant, actor, r.Context.IdempotencyKey})
	callKey := deletionReceiptService + ":request:" + callHash
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	locks := []string{referenceKey, callKey}
	sort.Strings(locks)
	for _, key := range locks {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return nil, err
		}
	}
	var previousHash, previousRef string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_ref FROM idempotency_keys WHERE id=$1 AND service=$2`, callKey, deletionReceiptService).Scan(&previousHash, &previousRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && (previousHash != requestHash || previousRef != referenceKey) {
		return nil, ErrIdempotencyConflict
	}
	var storedHash, storedPayload string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, referenceKey, DeletionReceiptType).Scan(&storedHash, &storedPayload)
	if err == nil {
		if storedHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		// Waiting on either identity lock may outlive the caller's admission.
		// Re-authorize this insertion while holding both locks; replay still uses
		// the handler's normal read admission and never changes stored evidence.
		if err := authorizeInsert(ctx); err != nil {
			return nil, err
		}
		now := s.now()
		// The deletion is its own owner fact, so the Ledger names it by the exact
		// operation the Workspace owner accepted and read back.
		receiptID := evidenceID("receipt_deletion_", referenceKey)
		evidence.Id, evidence.CreatedAt = receiptID, timestamppb.New(now)
		encoded, marshalErr := protojson.Marshal(evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
		input := ReceiptInput{Type: DeletionReceiptType, Status: "completed", Surface: "cloud", OrganizationID: tenant, WorkspaceID: r.OwnerCommitEvidence.ResourceId,
			RequestID: r.Context.RequestId, Actor: map[string]any{"id": actor}, Owner: map[string]any{"name": "workspace", "evidenceReference": r.OwnerEvidenceReference},
			InputRefs: map[string]any{"deletion": json.RawMessage(encoded)}, OutputRefs: map[string]any{"workspaceStatus": "absent"}}
		payload, marshalErr := json.Marshal(receiptPayload{ReceiptInput: input})
		if marshalErr != nil {
			return nil, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO evidence_receipts (id,receipt_type,status,organization_id,workspace_id,payload_json,idempotency_key,request_hash,created_at)
			VALUES ($1,$2,'completed',$3,$4,$5,$6,$7,$8)`, receiptID, DeletionReceiptType, tenant, input.WorkspaceID, string(payload), referenceKey, requestHash, now)
		if err != nil {
			return nil, err
		}
		storedPayload = string(payload)
	} else {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys (id,service,idempotency_key,request_hash,response_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`, callKey, deletionReceiptService, callHash, requestHash, referenceKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return decodeDeletionReceipt(storedPayload)
}

func (s *PostgresStore) ReadDeletionReceipt(ctx context.Context, reference string) (*DeletionRecord, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, deletionReceiptReferenceKey(reference), DeletionReceiptType).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeDeletionReceipt(payload)
}

func decodeDeletionReceipt(payload string) (*DeletionRecord, error) {
	var stored receiptPayload
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored.InputRefs["deletion"])
	if err != nil {
		return nil, err
	}
	evidence := &api.Receipt{}
	if err = protojson.Unmarshal(raw, evidence); err != nil {
		return nil, err
	}
	return &DeletionRecord{Evidence: evidence, TenantID: stored.OrganizationID, ActorID: stringFromAny(stored.Actor["id"]), WorkspaceID: stored.WorkspaceID}, nil
}
