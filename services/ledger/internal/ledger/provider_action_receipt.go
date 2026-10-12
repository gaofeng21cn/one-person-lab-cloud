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

const ProviderActionReceiptType = "workspace.provider_action.v1"
const providerActionReceiptService = "ledger.provider_action"

type ProviderActionRecord struct {
	Evidence                       *api.Receipt
	TenantID, ActorID, WorkspaceID string
}

func ValidateProviderActionReceiptInput(r *api.AppendReceiptRequest) error {
	c, receipt := r.GetOwnerCommitEvidence(), r.GetReceipt()
	call := r.GetContext()
	if c == nil || receipt == nil || call == nil || call.GetIdempotencyKey() == "" || len(call.GetIdempotencyKey()) > 512 ||
		call.GetScope().GetTenant().GetTenantId() == "" || call.GetActorId() == "" || r.GetOwnerEvidenceReference() == "" ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetArtifactDigest() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetOwnerEvidenceReference() || len(receipt.GetEvidenceSummary()) > 1024 ||
		c.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || c.GetOperationId() != r.GetOwnerEvidenceReference() ||
		c.GetActorId() != call.GetActorId() || !proto.Equal(c.GetScope(), call.GetScope()) ||
		c.GetCommittedVersion() < 1 || c.GetAcceptedAt() == nil || c.GetAcceptedAt().CheckValid() != nil || c.GetAuthorizationContextId() == "" {
		return ErrInvalidReceiptInput
	}
	return nil
}

func providerActionReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{providerActionReceiptService, "workspace", reference})
	return providerActionReceiptService + ":reference:" + hash
}

func (s *PostgresStore) RecordProviderActionReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*ProviderActionRecord, error) {
	if err := ValidateProviderActionReceiptInput(r); err != nil {
		return nil, err
	}
	if authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	receipt := proto.Clone(r.Receipt).(*api.Receipt)
	commit := proto.Clone(r.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	
	evidence := map[string]any{
		"receipt":             receipt,
		"ownerCommitEvidence": commit,
		"evidenceDigest":      r.EvidenceDigest,
	}
	raw, err := json.Marshal(evidence)
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
	workspaceID := commit.GetResourceId()
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
		if err := authorizeInsert(ctx); err != nil {
			return nil, err
		}
		now := s.now()
		receiptID := "receipt_provider_" + strings.TrimPrefix(referenceKey, providerActionReceiptService+":reference:")
		receipt.Id, receipt.CreatedAt = receiptID, timestamppb.New(now)
		evidence["receipt"] = receipt
		encoded, marshalErr := json.Marshal(evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
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
	var decoded receiptPayload
	if json.Unmarshal([]byte(storedPayload), &decoded) != nil {
		return nil, ErrReceiptNotFound
	}
	var storedEvidence map[string]any
	if raw, ok := decoded.InputRefs["providerAction"].(json.RawMessage); !ok || json.Unmarshal(raw, &storedEvidence) != nil {
		return nil, ErrReceiptNotFound
	}
	var storedReceipt api.Receipt
	if receiptRaw, err := json.Marshal(storedEvidence["receipt"]); err != nil || protojson.Unmarshal(receiptRaw, &storedReceipt) != nil {
		return nil, ErrReceiptNotFound
	}
	return &ProviderActionRecord{Evidence: &storedReceipt, TenantID: tenant, ActorID: actor, WorkspaceID: workspaceID}, nil
}

func (s *PostgresStore) ReadProviderActionReceipt(ctx context.Context, reference string) (*ProviderActionRecord, error) {
	referenceKey := providerActionReceiptReferenceKey(reference)
	row := s.db.QueryRowContext(ctx, `SELECT payload_json,organization_id FROM evidence_receipts WHERE receipt_type=$1 AND idempotency_key=$2`, ProviderActionReceiptType, referenceKey)
	var payloadJSON string
	var tenantID string
	if err := row.Scan(&payloadJSON, &tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReceiptNotFound
		}
		return nil, err
	}
	var decoded receiptPayload
	if json.Unmarshal([]byte(payloadJSON), &decoded) != nil {
		return nil, ErrReceiptNotFound
	}
	var storedEvidence map[string]any
	if raw, ok := decoded.InputRefs["providerAction"].(json.RawMessage); !ok || json.Unmarshal(raw, &storedEvidence) != nil {
		return nil, ErrReceiptNotFound
	}
	var storedReceipt api.Receipt
	if receiptRaw, err := json.Marshal(storedEvidence["receipt"]); err != nil || protojson.Unmarshal(receiptRaw, &storedReceipt) != nil {
		return nil, ErrReceiptNotFound
	}
	actorID := ""
	if id, ok := decoded.Actor["id"].(string); ok {
		actorID = id
	}
	return &ProviderActionRecord{Evidence: &storedReceipt, TenantID: tenantID, ActorID: actorID, WorkspaceID: decoded.WorkspaceID}, nil
}
