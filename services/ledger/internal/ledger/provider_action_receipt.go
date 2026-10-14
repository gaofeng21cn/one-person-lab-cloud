package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

// ProviderActionReceiptType is the evidence type of a confirmed Workspace
// provider action recorded by the Ledger, such as a deletion whose runtime and
// provider resources are both absent. It is distinct from the two funded types
// because an absence fact is not a funding fact: it names no accepted quote and
// no wallet operation, so it can never be satisfied by a zero-charge record, a
// funded charge, or the reverse.
const ProviderActionReceiptType = "workspace.provider_action.v1"

const providerActionReceiptService = "ledger.provider_action"

type ProviderActionRecord struct {
	Evidence                       *api.ProviderActionReceiptEvidence
	TenantID, ActorID, WorkspaceID string
}

// ValidateProviderActionReceiptInput checks the evidence shape, not its
// authority. The authenticated coordination handler must read the Workspace owner
// commit before calling the dedicated writer. The generic HTTP receipt writer
// rejects this type; only the coordination path may append it.
//
// Unlike the two funded kinds this evidence carries neither a QuoteAcceptance
// nor a WalletOperation, so the Workspace identity is read from the owner commit's
// own resource id. A deletion receipt is exactly that shape: its absence evidence
// is a Workspace owner commit, not an order and not a charge.
func ValidateProviderActionReceiptInput(r *api.AppendReceiptRequest) error {
	c, receipt := r.GetOwnerCommitEvidence(), r.GetReceipt()
	call := r.GetContext()
	if c == nil || receipt == nil || call == nil || call.GetIdempotencyKey() == "" || len(call.GetIdempotencyKey()) > 512 ||
		call.GetScope().GetTenant().GetTenantId() == "" || call.GetActorId() == "" || r.GetOwnerEvidenceReference() == "" ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetArtifactDigest() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetOwnerEvidenceReference() || len(receipt.GetEvidenceSummary()) > 1024 ||
		!artifactDigest.MatchString(r.GetEvidenceDigest()) || r.GetEvidenceDigest() != c.GetAcceptedInputDigest() {
		return ErrInvalidReceiptInput
	}
	// The Workspace owner commit must be this exact deletion of this exact
	// Workspace, by this actor in this tenant. Its resource id is the only
	// Workspace identity this evidence carries, so it may not be empty and may
	// not point somewhere other than the resource the action was authorized
	// against.
	if c.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || c.GetOperationId() != r.GetOwnerEvidenceReference() || c.GetResourceId() == "" ||
		c.GetActorId() != call.GetActorId() || !proto.Equal(c.GetScope(), call.GetScope()) ||
		c.GetCommittedVersion() < 1 || c.GetAcceptedAt() == nil || c.GetAcceptedAt().CheckValid() != nil || c.GetAuthorizationContextId() == "" ||
		c.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE {
		return ErrInvalidReceiptInput
	}
	if got := c.GetAuthorizationResource(); got.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || got.GetId() != c.GetResourceId() {
		return ErrInvalidReceiptInput
	}
	return nil
}

func providerActionReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{providerActionReceiptService, "workspace", reference})
	return providerActionReceiptService + ":reference:" + hash
}

// RecordProviderActionReceipt uses the existing immutable evidence store. Two
// scoped locks keep the caller key and original owner reference atomic, exactly
// as the two funded paths do, so a lost response replays the identical receipt
// rather than recording the same confirmed absence twice.
func (s *PostgresStore) RecordProviderActionReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*ProviderActionRecord, error) {
	if err := ValidateProviderActionReceiptInput(r); err != nil {
		return nil, err
	}
	if authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	evidence := &api.ProviderActionReceiptEvidence{Receipt: proto.Clone(r.Receipt).(*api.Receipt), OwnerCommitEvidence: proto.Clone(r.OwnerCommitEvidence).(*api.OwnerCommitEvidence), EvidenceDigest: r.EvidenceDigest}
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
	workspaceID := r.OwnerCommitEvidence.GetResourceId()
	referenceKey := providerActionReceiptReferenceKey(r.OwnerEvidenceReference)
	callHash, _ := hashJSON([]string{tenant, actor, r.Context.IdempotencyKey})
	callKey := providerActionReceiptService + ":request:" + callHash
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
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_ref FROM idempotency_keys WHERE id=$1 AND service=$2`, callKey, providerActionReceiptService).Scan(&previousHash, &previousRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && (previousHash != requestHash || previousRef != referenceKey) {
		return nil, ErrIdempotencyConflict
	}
	var storedHash, storedPayload string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, referenceKey, ProviderActionReceiptType).Scan(&storedHash, &storedPayload)
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
		receiptID := "receipt_provider_" + strings.TrimPrefix(referenceKey, providerActionReceiptService+":reference:")
		evidence.Receipt.Id, evidence.Receipt.CreatedAt = receiptID, timestamppb.New(now)
		encoded, marshalErr := protojson.Marshal(evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
		// An absence receipt moves no money: it carries no billing mode and no
		// amount, so it can never be read as a settled or a zero charge.
		input := ReceiptInput{Type: ProviderActionReceiptType, Status: "completed", Surface: "cloud", OrganizationID: tenant, WorkspaceID: workspaceID,
			RequestID: r.Context.RequestId, Actor: map[string]any{"id": actor}, Owner: map[string]any{"name": "workspace", "evidenceReference": r.OwnerEvidenceReference},
			InputRefs: map[string]any{"providerAction": json.RawMessage(encoded)}}
		payload, marshalErr := json.Marshal(receiptPayload{ReceiptInput: input})
		if marshalErr != nil {
			return nil, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO evidence_receipts (id,receipt_type,status,organization_id,workspace_id,payload_json,idempotency_key,request_hash,created_at)
			VALUES ($1,$2,'completed',$3,$4,$5,$6,$7,$8)`, receiptID, ProviderActionReceiptType, tenant, input.WorkspaceID, string(payload), referenceKey, requestHash, now)
		if err != nil {
			return nil, err
		}
		storedPayload = string(payload)
	} else {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys (id,service,idempotency_key,request_hash,response_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`, callKey, providerActionReceiptService, callHash, requestHash, referenceKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return decodeProviderAction(storedPayload)
}

func (s *PostgresStore) ReadProviderActionReceipt(ctx context.Context, reference string) (*ProviderActionRecord, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, providerActionReceiptReferenceKey(reference), ProviderActionReceiptType).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeProviderAction(payload)
}

func decodeProviderAction(payload string) (*ProviderActionRecord, error) {
	var stored receiptPayload
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored.InputRefs["providerAction"])
	if err != nil {
		return nil, err
	}
	evidence := &api.ProviderActionReceiptEvidence{}
	if err = protojson.Unmarshal(raw, evidence); err != nil {
		return nil, err
	}
	// ActorID is the stored receipt's own provenance projection. It is
	// caller-owned and privacy-mutable, so it is reporting only: a privacy
	// redaction clears it and leaves it empty. It is never authorization
	// authority — that is the immutable OwnerCommitEvidence above.
	return &ProviderActionRecord{Evidence: evidence, TenantID: stored.OrganizationID, ActorID: stringFromAny(stored.Actor["id"]), WorkspaceID: stored.WorkspaceID}, nil
}
