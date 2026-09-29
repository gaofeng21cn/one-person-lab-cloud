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

// WalletActionReceiptType is the evidence type of a confirmed Gateway wallet
// charge recorded by the Ledger for a paid Workspace order. It is distinct from
// the Local no-charge type so a paid obligation can never be funded by a
// zero-charge record or the reverse.
const WalletActionReceiptType = "workspace.wallet_action.v1"

const walletActionReceiptService = "ledger.wallet_action"

type WalletActionRecord struct {
	Evidence                       *api.WalletActionReceiptEvidence
	TenantID, ActorID, WorkspaceID string
}

// ValidateWalletActionReceiptInput checks the evidence shape, not its authority.
// The authenticated coordination handler must read the Gateway wallet operation
// before calling the dedicated writer. The generic HTTP receipt writer rejects
// this type; only the coordination path may append it.
func ValidateWalletActionReceiptInput(r *api.AppendReceiptRequest) error {
	q, c, wallet, receipt := r.GetQuoteAcceptance(), r.GetOwnerCommitEvidence(), r.GetWalletOperation(), r.GetReceipt()
	call := r.GetContext()
	if q == nil || c == nil || wallet == nil || receipt == nil || call == nil || call.GetIdempotencyKey() == "" || len(call.GetIdempotencyKey()) > 512 ||
		call.GetScope().GetTenant().GetTenantId() == "" || call.GetActorId() == "" || r.GetOwnerEvidenceReference() == "" ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetArtifactDigest() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetOwnerEvidenceReference() || len(receipt.GetEvidenceSummary()) > 1024 ||
		!artifactDigest.MatchString(r.GetEvidenceDigest()) || r.GetEvidenceDigest() != q.GetSnapshotDigest() ||
		q.GetAcceptanceId() == "" || q.GetObligationId() != r.GetOwnerEvidenceReference() || q.GetWorkspaceId() == "" ||
		q.GetQuote().GetId() == "" || q.GetQuote().GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED || q.GetQuote().GetTotalUsdMicros() <= 0 ||
		q.GetResourcePlan().GetBillingMode() == "LOCAL_NO_CHARGE" ||
		q.GetResourcePlan().GetComputePlanId() == "" || q.GetResourcePlan().GetComputePlanId() != q.GetQuote().GetComputePlanId() ||
		q.GetResourcePlan().GetStoragePlanId() == "" || q.GetResourcePlan().GetStoragePlanId() != q.GetQuote().GetStoragePlanId() {
		return ErrInvalidReceiptInput
	}
	if q.GetQuote().GetWorkspaceId() != "" && q.GetQuote().GetWorkspaceId() != q.GetWorkspaceId() {
		return ErrInvalidReceiptInput
	}
	// The wallet operation must identify this exact obligation at this exact amount.
	if wallet.GetId() == "" || wallet.GetReceiptId() == "" || wallet.GetWorkspaceId() != q.GetWorkspaceId() ||
		wallet.GetKind() != api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE || wallet.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED ||
		wallet.GetAmountUsdMicros() != q.GetQuote().GetTotalUsdMicros() || wallet.GetCreatedAt() == nil || wallet.CreatedAt.CheckValid() != nil {
		return ErrInvalidReceiptInput
	}
	// The Workspace owner commit must be the same accepted CREATEWORKSPACE obligation.
	if c.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || c.GetOperationId() != r.GetOwnerEvidenceReference() || c.GetResourceId() != q.GetWorkspaceId() ||
		c.GetActorId() != call.GetActorId() || !proto.Equal(c.GetScope(), call.GetScope()) || !artifactDigest.MatchString(c.GetAcceptedInputDigest()) ||
		c.GetCommittedVersion() < 1 || c.GetAcceptedAt() == nil || c.GetAcceptedAt().CheckValid() != nil || c.GetAuthorizationContextId() == "" ||
		c.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE {
		return ErrInvalidReceiptInput
	}
	return nil
}

func walletActionReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{walletActionReceiptService, "workspace", reference})
	return walletActionReceiptService + ":reference:" + hash
}

// RecordWalletActionReceipt uses the existing immutable evidence store. Two scoped
// locks keep the caller key and original owner reference atomic, exactly as the
// Local no-charge path does, so a lost response replays the identical receipt
// rather than recording a second funded charge.
func (s *PostgresStore) RecordWalletActionReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*WalletActionRecord, error) {
	if err := ValidateWalletActionReceiptInput(r); err != nil {
		return nil, err
	}
	if authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	evidence := &api.WalletActionReceiptEvidence{
		Receipt: proto.Clone(r.Receipt).(*api.Receipt), QuoteAcceptance: proto.Clone(r.QuoteAcceptance).(*api.QuoteAcceptance),
		WalletOperation: proto.Clone(r.WalletOperation).(*api.WalletOperation), OwnerCommitEvidence: proto.Clone(r.OwnerCommitEvidence).(*api.OwnerCommitEvidence), EvidenceDigest: r.EvidenceDigest,
	}
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
	referenceKey := walletActionReceiptReferenceKey(r.OwnerEvidenceReference)
	callHash, _ := hashJSON([]string{tenant, actor, r.Context.IdempotencyKey})
	callKey := walletActionReceiptService + ":request:" + callHash
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
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_ref FROM idempotency_keys WHERE id=$1 AND service=$2`, callKey, walletActionReceiptService).Scan(&previousHash, &previousRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && (previousHash != requestHash || previousRef != referenceKey) {
		return nil, ErrIdempotencyConflict
	}
	var storedHash, storedPayload string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, referenceKey, WalletActionReceiptType).Scan(&storedHash, &storedPayload)
	if err == nil {
		if storedHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if err := authorizeInsert(ctx); err != nil {
			return nil, err
		}
		now := s.now()
		// The Ledger, as the receipt writer, honors the exact receipt id the Gateway
		// wallet operation named. That id is what Fabric later reads back by
		// reference, so the two owners agree on the funded-charge identity without
		// the Ledger inventing or hashing a money identity of its own.
		receiptID := r.WalletOperation.GetReceiptId()
		evidence.Receipt.Id, evidence.Receipt.CreatedAt = receiptID, timestamppb.New(now)
		encoded, marshalErr := protojson.Marshal(evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
		input := ReceiptInput{Type: WalletActionReceiptType, Status: "completed", Surface: "cloud", OrganizationID: tenant, WorkspaceID: r.QuoteAcceptance.WorkspaceId,
			RequestID: r.Context.RequestId, Actor: map[string]any{"id": actor}, Owner: map[string]any{"name": "workspace", "evidenceReference": r.OwnerEvidenceReference},
			InputRefs: map[string]any{"walletAction": json.RawMessage(encoded)}, Cost: map[string]any{"amountUsdMicros": r.WalletOperation.AmountUsdMicros, "currency": "USD", "walletOperationId": r.WalletOperation.Id}}
		payload, marshalErr := json.Marshal(receiptPayload{ReceiptInput: input})
		if marshalErr != nil {
			return nil, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO evidence_receipts (id,receipt_type,status,organization_id,workspace_id,payload_json,idempotency_key,request_hash,created_at)
			VALUES ($1,$2,'completed',$3,$4,$5,$6,$7,$8)`, receiptID, WalletActionReceiptType, tenant, input.WorkspaceID, string(payload), referenceKey, requestHash, now)
		if err != nil {
			return nil, err
		}
		storedPayload = string(payload)
	} else {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys (id,service,idempotency_key,request_hash,response_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`, callKey, walletActionReceiptService, callHash, requestHash, referenceKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return decodeWalletAction(storedPayload)
}

func (s *PostgresStore) ReadWalletActionReceipt(ctx context.Context, reference string) (*WalletActionRecord, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, walletActionReceiptReferenceKey(reference), WalletActionReceiptType).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeWalletAction(payload)
}

func decodeWalletAction(payload string) (*WalletActionRecord, error) {
	var stored receiptPayload
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored.InputRefs["walletAction"])
	if err != nil {
		return nil, err
	}
	evidence := &api.WalletActionReceiptEvidence{}
	if err = protojson.Unmarshal(raw, evidence); err != nil {
		return nil, err
	}
	return &WalletActionRecord{Evidence: evidence, TenantID: stored.OrganizationID, ActorID: stringFromAny(stored.Actor["id"]), WorkspaceID: stored.WorkspaceID}, nil
}
