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
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

// GatewayWalletAdjustmentReceiptType is the evidence type of a confirmed
// administrator balance change that the Gateway owner recorded and the Ledger
// acknowledged. It is distinct from the retained Control Plane's
// gateway.wallet_adjustment.v1 so the two writers never share one fact: the
// retained writer is account-scoped and names a Sub2API balance history,
// while this one is tenant-scoped and names the Gateway owner's own confirmed
// wallet operation.
const GatewayWalletAdjustmentReceiptType = "gateway.wallet_adjustment.confirmed.v1"

const gatewayWalletAdjustmentReceiptService = "ledger.gateway_wallet_adjustment"

// GatewayWalletAdjustmentRecord is one stored administrator balance change.
type GatewayWalletAdjustmentRecord struct {
	Evidence          *api.GatewayWalletAdjustmentReceiptEvidence
	TenantID, ActorID string
}

// ValidateGatewayWalletAdjustmentReceiptInput checks the evidence shape, not
// its authority. The authenticated coordination handler must read the Gateway
// owner's confirmed wallet operation before calling the dedicated writer, and
// the generic HTTP receipt writer refuses this type.
func ValidateGatewayWalletAdjustmentReceiptInput(r *api.AppendReceiptRequest) error {
	evidence, receipt, call := r.GetGatewayWalletAdjustmentEvidence(), r.GetReceipt(), r.GetContext()
	if evidence == nil || receipt == nil || call == nil ||
		call.GetIdempotencyKey() == "" || len(call.GetIdempotencyKey()) > 512 ||
		call.GetScope().GetTenant().GetTenantId() == "" || call.GetActorId() == "" || r.GetOwnerEvidenceReference() == "" ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetArtifactDigest() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_GATEWAY ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetOwnerEvidenceReference() || len(receipt.GetEvidenceSummary()) > 1024 {
		return ErrInvalidReceiptInput
	}
	// The submitted evidence must be the very envelope the Gateway owner built
	// for this receipt: a receipt and the wallet operation it names, with the
	// digest of that operation.
	wallet := evidence.GetWalletOperation()
	if wallet == nil || evidence.GetReceipt() == nil || !proto.Equal(evidence.GetReceipt(), receipt) {
		return ErrInvalidReceiptInput
	}
	if !artifactDigest.MatchString(evidence.GetEvidenceDigest()) || evidence.GetEvidenceDigest() != r.GetEvidenceDigest() {
		return ErrInvalidReceiptInput
	}
	digest, err := contracts.WalletOperationDigest(wallet)
	if err != nil || digest != evidence.GetEvidenceDigest() {
		return ErrInvalidReceiptInput
	}
	// The wallet operation is the authority: a tenant-level adjustment names no
	// Workspace, and it is the Gateway owner's own confirmed record that names
	// the operator, the reason, the amount and the receipt id.
	if wallet.GetId() != r.GetOwnerEvidenceReference() || wallet.GetWorkspaceId() != "" || wallet.GetReceiptId() == "" ||
		wallet.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || wallet.GetAmountUsdMicros() <= 0 ||
		wallet.GetActorId() == "" || wallet.GetActorId() != call.GetActorId() || wallet.GetReason() == "" ||
		wallet.GetCreatedAt() == nil || wallet.GetCreatedAt().CheckValid() != nil {
		return ErrInvalidReceiptInput
	}
	switch wallet.GetKind() {
	case api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_RECHARGE,
		api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_DEBIT:
		if wallet.GetOriginalChargeOperationId() != "" {
			return ErrInvalidReceiptInput
		}
	case api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_BUSINESS_REFUND:
		if wallet.GetOriginalChargeOperationId() == "" {
			return ErrInvalidReceiptInput
		}
	default:
		return ErrInvalidReceiptInput
	}
	return nil
}

func gatewayWalletAdjustmentReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{gatewayWalletAdjustmentReceiptService, "gateway", reference})
	return gatewayWalletAdjustmentReceiptService + ":reference:" + hash
}

// RecordGatewayWalletAdjustmentReceipt uses the existing immutable evidence
// store. Two scoped locks keep the caller key and the original wallet
// operation reference atomic, exactly as the funded charge path does, so a
// lost response replays the identical receipt rather than recording a second
// balance change.
func (s *PostgresStore) RecordGatewayWalletAdjustmentReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*GatewayWalletAdjustmentRecord, error) {
	if err := ValidateGatewayWalletAdjustmentReceiptInput(r); err != nil {
		return nil, err
	}
	if authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	evidence := &api.GatewayWalletAdjustmentReceiptEvidence{
		Receipt: proto.Clone(r.Receipt).(*api.Receipt), WalletOperation: proto.Clone(r.GatewayWalletAdjustmentEvidence.WalletOperation).(*api.WalletOperation), EvidenceDigest: r.GatewayWalletAdjustmentEvidence.EvidenceDigest,
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
	referenceKey := gatewayWalletAdjustmentReceiptReferenceKey(r.OwnerEvidenceReference)
	callHash, _ := hashJSON([]string{tenant, actor, r.Context.IdempotencyKey})
	callKey := gatewayWalletAdjustmentReceiptService + ":request:" + callHash
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
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_ref FROM idempotency_keys WHERE id=$1 AND service=$2`, callKey, gatewayWalletAdjustmentReceiptService).Scan(&previousHash, &previousRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && (previousHash != requestHash || previousRef != referenceKey) {
		return nil, ErrIdempotencyConflict
	}
	var storedHash, storedPayload string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, referenceKey, GatewayWalletAdjustmentReceiptType).Scan(&storedHash, &storedPayload)
	if err == nil {
		if storedHash != requestHash {
			return nil, ErrIdempotencyConflict
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		// Waiting on either identity lock may outlive the caller's admission.
		// Re-authorize this insertion while holding both locks.
		if err := authorizeInsert(ctx); err != nil {
			return nil, err
		}
		now := s.now()
		// The Ledger, as the receipt writer, honors the exact receipt id the
		// Gateway wallet operation named. That id is what the operator reads
		// back against the wallet operation, so the two owners agree on the
		// balance-change identity without the Ledger inventing a money identity
		// of its own.
		receiptID := r.GatewayWalletAdjustmentEvidence.WalletOperation.GetReceiptId()
		evidence.Receipt.Id, evidence.Receipt.CreatedAt = receiptID, timestamppb.New(now)
		encoded, marshalErr := protojson.Marshal(evidence)
		if marshalErr != nil {
			return nil, marshalErr
		}
		wallet := r.GatewayWalletAdjustmentEvidence.WalletOperation
		input := ReceiptInput{Type: GatewayWalletAdjustmentReceiptType, Status: "completed", Surface: "cloud", OrganizationID: tenant,
			RequestID: r.Context.RequestId, Actor: map[string]any{"id": actor}, Owner: map[string]any{"name": "gateway", "evidenceReference": r.OwnerEvidenceReference},
			Execution: map[string]any{"operationId": wallet.GetId(), "kind": walletAdjustmentKindName(wallet.GetKind()), "amountUsdMicros": wallet.GetAmountUsdMicros()},
			InputRefs: map[string]any{"gatewayWalletAdjustment": json.RawMessage(encoded)}}
		payload, marshalErr := json.Marshal(receiptPayload{ReceiptInput: input})
		if marshalErr != nil {
			return nil, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO evidence_receipts (id,receipt_type,status,organization_id,workspace_id,payload_json,idempotency_key,request_hash,created_at)
			VALUES ($1,$2,'completed',$3,$4,$5,$6,$7,$8)`, receiptID, GatewayWalletAdjustmentReceiptType, tenant, "", string(payload), referenceKey, requestHash, now)
		if err != nil {
			return nil, err
		}
		storedPayload = string(payload)
	} else {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys (id,service,idempotency_key,request_hash,response_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`, callKey, gatewayWalletAdjustmentReceiptService, callHash, requestHash, referenceKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return decodeGatewayWalletAdjustment(storedPayload)
}

// ReadGatewayWalletAdjustmentReceipt reads one stored administrator balance
// change by the Gateway owner's own wallet operation id.
func (s *PostgresStore) ReadGatewayWalletAdjustmentReceipt(ctx context.Context, reference string) (*GatewayWalletAdjustmentRecord, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE idempotency_key=$1 AND receipt_type=$2`, gatewayWalletAdjustmentReceiptReferenceKey(reference), GatewayWalletAdjustmentReceiptType).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeGatewayWalletAdjustment(payload)
}

func decodeGatewayWalletAdjustment(payload string) (*GatewayWalletAdjustmentRecord, error) {
	var stored receiptPayload
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(stored.InputRefs["gatewayWalletAdjustment"])
	if err != nil {
		return nil, err
	}
	evidence := &api.GatewayWalletAdjustmentReceiptEvidence{}
	if err = protojson.Unmarshal(raw, evidence); err != nil {
		return nil, err
	}
	// ActorID is the stored receipt's own provenance projection. It is
	// caller-owned and privacy-mutable, so it is reporting only: a privacy
	// redaction clears it and leaves it empty. It is never authorization
	// authority — that is the Gateway owner's own confirmed wallet operation
	// above, which the Ledger re-read before it stored anything.
	return &GatewayWalletAdjustmentRecord{Evidence: evidence, TenantID: stored.OrganizationID, ActorID: stringFromAny(stored.Actor["id"])}, nil
}

// walletAdjustmentKindName names the administrator's own adjustment kinds in
// the stored receipt's execution block. A Workspace obligation kind is never
// written here: an operator action can never be read as a purchase or as a
// deletion refund.
func walletAdjustmentKindName(kind api.WalletOperationKindEnum) string {
	switch kind {
	case api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_RECHARGE:
		return "recharge"
	case api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_DEBIT:
		return "debit"
	case api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_BUSINESS_REFUND:
		return "business_refund"
	default:
		return ""
	}
}
