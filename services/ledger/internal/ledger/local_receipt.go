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

const LocalNoChargeReceiptType = "workspace.local_no_charge.v1"
const localReceiptService = "ledger.local_no_charge"

type LocalNoChargeRecord struct {
	Evidence                       *api.LocalNoChargeReceiptEvidence
	TenantID, ActorID, WorkspaceID string
}

// ValidateLocalNoChargeReceiptInput checks the evidence shape, not its authority.
// The authenticated coordination handler must read Catalog and Workspace before
// calling the dedicated writer. The generic HTTP receipt writer rejects this type.
func ValidateLocalNoChargeReceiptInput(r *api.AppendReceiptRequest) error {
	q, c, receipt := r.GetQuoteAcceptance(), r.GetOwnerCommitEvidence(), r.GetReceipt()
	call := r.GetContext()
	if q == nil || c == nil || receipt == nil || call == nil || call.GetIdempotencyKey() == "" || len(call.GetIdempotencyKey()) > 512 ||
		call.GetScope().GetTenant().GetTenantId() == "" || call.GetActorId() == "" || r.GetOwnerEvidenceReference() == "" ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetArtifactDigest() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetOwnerEvidenceReference() || len(receipt.GetEvidenceSummary()) > 1024 ||
		!artifactDigest.MatchString(r.GetEvidenceDigest()) || r.GetEvidenceDigest() != q.GetSnapshotDigest() ||
		q.GetAcceptanceId() == "" || q.GetObligationId() != r.GetOwnerEvidenceReference() || q.GetWorkspaceId() == "" ||
		q.GetQuote().GetId() == "" || q.GetQuote().GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED || q.GetQuote().GetTotalUsdMicros() != 0 ||
		q.GetResourcePlan().GetBillingMode() != "LOCAL_NO_CHARGE" || q.GetResourcePlan().GetProvider() != "local-docker" ||
		q.GetResourcePlan().GetComputePlanId() == "" || q.GetResourcePlan().GetComputePlanId() != q.GetQuote().GetComputePlanId() ||
		q.GetResourcePlan().GetStoragePlanId() == "" || q.GetResourcePlan().GetStoragePlanId() != q.GetQuote().GetStoragePlanId() ||
		c.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || c.GetOperationId() != r.GetOwnerEvidenceReference() || c.GetResourceId() != q.GetWorkspaceId() ||
		c.GetActorId() != call.GetActorId() || !proto.Equal(c.GetScope(), call.GetScope()) || !artifactDigest.MatchString(c.GetAcceptedInputDigest()) ||
		c.GetCommittedVersion() < 1 || c.GetAcceptedAt() == nil || c.GetAcceptedAt().CheckValid() != nil || c.GetAuthorizationContextId() == "" ||
		c.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE {
		return ErrInvalidReceiptInput
	}
	if q.GetQuote().GetWorkspaceId() != "" && q.GetQuote().GetWorkspaceId() != q.GetWorkspaceId() {
		return ErrInvalidReceiptInput
	}
	for _, line := range q.GetQuote().GetLineItems() {
		if line.GetAmountUsdMicros() != 0 {
			return ErrInvalidReceiptInput
		}
	}
	return nil
}

func localReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{localReceiptService, "workspace", reference})
	return localReceiptService + ":reference:" + hash
}

// RecordLocalNoChargeReceipt uses the existing immutable evidence store. Two
// scoped locks keep the caller key and original owner reference atomic: a changed
// payload cannot hide behind a new key, and a reused key cannot name a new order.
func (s *PostgresStore) RecordLocalNoChargeReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*LocalNoChargeRecord, error) {
	if err := ValidateLocalNoChargeReceiptInput(r); err != nil {
		return nil, err
	}
	if authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	evidence := &api.LocalNoChargeReceiptEvidence{Receipt: proto.Clone(r.Receipt).(*api.Receipt), QuoteAcceptance: proto.Clone(r.QuoteAcceptance).(*api.QuoteAcceptance), OwnerCommitEvidence: proto.Clone(r.OwnerCommitEvidence).(*api.OwnerCommitEvidence), EvidenceDigest: r.EvidenceDigest}
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
	referenceKey := localReceiptReferenceKey(r.OwnerEvidenceReference)
	callHash, _ := hashJSON([]string{tenant, actor, r.Context.IdempotencyKey})
	callKey := localReceiptService + ":request:" + callHash
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
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_ref FROM idempotency_keys WHERE id=$1 AND service=$2`, callKey, localReceiptService).Scan(&previousHash, &previousRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && (previousHash != requestHash || previousRef != referenceKey) {
		return nil, ErrIdempotencyConflict
	}
	var storedHash, storedPayload string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, referenceKey, LocalNoChargeReceiptType).Scan(&storedHash, &storedPayload)
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
		receiptID := "receipt_local_" + strings.TrimPrefix(referenceKey, localReceiptService+":reference:")
		evidence.Receipt.Id, evidence.Receipt.CreatedAt = receiptID, timestamppb.New(now)
		encoded, marshalErr := protojson.Marshal(evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
		input := ReceiptInput{Type: LocalNoChargeReceiptType, Status: "completed", Surface: "cloud", OrganizationID: tenant, WorkspaceID: r.QuoteAcceptance.WorkspaceId,
			RequestID: r.Context.RequestId, Actor: map[string]any{"id": actor}, Owner: map[string]any{"name": "workspace", "evidenceReference": r.OwnerEvidenceReference},
			InputRefs: map[string]any{"localNoCharge": json.RawMessage(encoded)}, Cost: map[string]any{"billingMode": "LOCAL_NO_CHARGE", "amountUsdMicros": "0"}}
		payload, marshalErr := json.Marshal(receiptPayload{ReceiptInput: input})
		if marshalErr != nil {
			return nil, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO evidence_receipts (id,receipt_type,status,organization_id,workspace_id,payload_json,idempotency_key,request_hash,created_at)
			VALUES ($1,$2,'completed',$3,$4,$5,$6,$7,$8)`, receiptID, LocalNoChargeReceiptType, tenant, input.WorkspaceID, string(payload), referenceKey, requestHash, now)
		if err != nil {
			return nil, err
		}
		storedPayload = string(payload)
	} else {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys (id,service,idempotency_key,request_hash,response_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`, callKey, localReceiptService, callHash, requestHash, referenceKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return decodeLocalNoCharge(storedPayload)
}

func (s *PostgresStore) ReadLocalNoChargeReceipt(ctx context.Context, reference string) (*LocalNoChargeRecord, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, localReceiptReferenceKey(reference), LocalNoChargeReceiptType).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeLocalNoCharge(payload)
}

func decodeLocalNoCharge(payload string) (*LocalNoChargeRecord, error) {
	var stored receiptPayload
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored.InputRefs["localNoCharge"])
	if err != nil {
		return nil, err
	}
	evidence := &api.LocalNoChargeReceiptEvidence{}
	if err = protojson.Unmarshal(raw, evidence); err != nil {
		return nil, err
	}
	return &LocalNoChargeRecord{Evidence: evidence, TenantID: stored.OrganizationID, ActorID: stringFromAny(stored.Actor["id"]), WorkspaceID: stored.WorkspaceID}, nil
}
