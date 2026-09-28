package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

const DeploymentReceiptType = "workspace.deployment.v1"
const deploymentReceiptService = "ledger.workspace_deployment"

type DeploymentReceiptRecord struct {
	Receipt                *api.Receipt
	TenantID, ActorID      string
	WorkspaceID            string
	OwnerEvidenceReference string
}

func deploymentReceiptReferenceKey(reference string) string {
	hash, _ := hashJSON([]string{deploymentReceiptService, "reference", reference})
	return deploymentReceiptService + ":reference:" + hash
}

func deploymentReceiptRequestKey(tenant, actor, idempotency string) string {
	hash, _ := hashJSON([]string{tenant, actor, idempotency})
	return deploymentReceiptService + ":request:" + hash
}

// ValidateDeploymentReceiptInput validates the typed Workspace deployment
// evidence boundary. The actual quote, owner commit, and Ledger authorization
// are re-read by the coordination handler before persistence.
func ValidateDeploymentReceiptInput(r *api.AppendReceiptRequest) error {
	if r == nil || r.Context == nil || r.Receipt == nil || r.QuoteAcceptance == nil || r.OwnerCommitEvidence == nil {
		return ErrInvalidReceiptInput
	}
	q, c, receipt, call := r.QuoteAcceptance, r.OwnerCommitEvidence, r.Receipt, r.Context
	reference := strings.TrimSpace(r.OwnerEvidenceReference)
	if !strings.HasSuffix(reference, ":deployment") {
		return ErrInvalidReceiptInput
	}
	operationID := strings.TrimSuffix(reference, ":deployment")
	if operationID == "" || call.GetRequestId() == "" || call.GetIdempotencyKey() == "" || call.GetActorId() == "" || call.GetScope().GetTenant().GetTenantId() == "" ||
		r.EvidenceDigest == "" || !artifactDigest.MatchString(r.EvidenceDigest) ||
		receipt.GetId() != "" || receipt.GetCreatedAt() != nil || receipt.GetSourceSha() != "" || receipt.GetWorkflowRunId() != "" ||
		receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_DEPLOYMENT || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOperationId() != operationID || !artifactDigest.MatchString(receipt.GetArtifactDigest()) || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED ||
		len(receipt.GetEvidenceSummary()) == 0 || len(receipt.GetEvidenceSummary()) > 1024 ||
		q.GetAcceptanceId() == "" || q.GetObligationId() != operationID || q.GetWorkspaceId() == "" || q.GetQuote().GetId() == "" || q.GetQuote().GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED ||
		c.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || c.GetOperationId() != operationID || c.GetResourceId() != q.GetWorkspaceId() || c.GetActorId() != call.GetActorId() || !proto.Equal(c.GetScope(), call.GetScope()) ||
		c.GetCommittedVersion() < 1 || c.GetAcceptedAt() == nil || c.GetAcceptedAt().CheckValid() != nil || c.GetAuthorizationContextId() == "" ||
		c.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE || q.GetWorkspaceId() != c.GetResourceId() {
		return ErrInvalidReceiptInput
	}
	return nil
}

// RecordDeploymentReceipt stores the immutable typed deployment evidence and
// emits Ledger's receipt-recorded Outbox event in one transaction. The callback
// is invoked only for a new owner-reference row while both identity locks are held.
func (s *PostgresStore) RecordDeploymentReceipt(ctx context.Context, r *api.AppendReceiptRequest, authorizeInsert func(context.Context) error) (*DeploymentReceiptRecord, error) {
	if err := ValidateDeploymentReceiptInput(r); err != nil || authorizeInsert == nil {
		return nil, ErrInvalidReceiptInput
	}
	q, c, call := r.QuoteAcceptance, r.OwnerCommitEvidence, r.Context
	reference := r.OwnerEvidenceReference
	tenant, actor := call.GetScope().GetTenant().GetTenantId(), call.GetActorId()
	referenceKey := deploymentReceiptReferenceKey(reference)
	callKey := deploymentReceiptRequestKey(tenant, actor, call.GetIdempotencyKey())
	quoteJSON, err := protojson.Marshal(q)
	if err != nil {
		return nil, ErrInvalidReceiptInput
	}
	commitJSON, err := protojson.Marshal(c)
	if err != nil {
		return nil, ErrInvalidReceiptInput
	}
	input := ReceiptInput{
		Type: DeploymentReceiptType, Status: "completed", Surface: "cloud", OrganizationID: tenant,
		WorkspaceID: q.GetWorkspaceId(), RequestID: strings.TrimSuffix(reference, ":deployment"), ArtifactID: r.Receipt.GetArtifactDigest(),
		Actor:     map[string]any{"id": actor},
		Execution: map[string]any{"operationId": strings.TrimSuffix(reference, ":deployment"), "resourceType": "workspace", "resourceId": q.GetWorkspaceId(), "evidenceSummary": r.Receipt.GetEvidenceSummary()},
		InputRefs: map[string]any{
			"ownerEvidenceReference": reference, "evidenceDigest": r.EvidenceDigest,
			"quoteAcceptance": json.RawMessage(quoteJSON), "ownerCommitEvidence": json.RawMessage(commitJSON),
		},
		Owner: map[string]any{"name": "workspace", "evidenceReference": reference}, IdempotencyKey: referenceKey,
	}
	requestHash, err := hashJSON(input)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	locks := []string{referenceKey, callKey}
	if locks[1] < locks[0] {
		locks[0], locks[1] = locks[1], locks[0]
	}
	for _, key := range locks {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return nil, err
		}
	}
	var previousHash, previousRef string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_ref FROM idempotency_keys WHERE id=$1 AND service=$2`, callKey, deploymentReceiptService).Scan(&previousHash, &previousRef)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && (previousHash != requestHash || previousRef != referenceKey) {
		return nil, ErrIdempotencyConflict
	}
	var existingType string
	err = tx.QueryRowContext(ctx, `SELECT receipt_type FROM evidence_receipts WHERE idempotency_key=$1`, referenceKey).Scan(&existingType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && existingType != DeploymentReceiptType {
		return nil, ErrIdempotencyConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		if err = authorizeInsert(ctx); err != nil {
			return nil, err
		}
	}
	receipt, err := s.recordReceiptTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if !receipt.Replayed {
		if err = s.appendReceiptRecordedFieldsTx(ctx, tx, receipt.ReceiptID, "workspace", tenant, call.GetRequestId(), reference, r.EvidenceDigest, receipt.CreatedAt); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys (id,service,idempotency_key,request_hash,response_ref) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (id) DO NOTHING`, callKey, deploymentReceiptService, call.GetIdempotencyKey(), requestHash, referenceKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	apiReceipt, err := deploymentReceiptAPI(receipt)
	if err != nil {
		return nil, err
	}
	return &DeploymentReceiptRecord{Receipt: apiReceipt, TenantID: tenant, ActorID: actor, WorkspaceID: q.GetWorkspaceId(), OwnerEvidenceReference: reference}, nil
}

func (s *PostgresStore) ReadDeploymentReceipt(ctx context.Context, reference string) (*DeploymentReceiptRecord, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, ErrReceiptNotFound
	}
	key := deploymentReceiptReferenceKey(reference)
	receipt, _, err := s.receiptByIdempotencyKey(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	if receipt.Type != DeploymentReceiptType {
		return nil, ErrReceiptNotFound
	}
	actor, _ := receipt.Actor["id"].(string)
	apiReceipt, err := deploymentReceiptAPI(receipt)
	if err != nil {
		return nil, err
	}
	return &DeploymentReceiptRecord{Receipt: apiReceipt, TenantID: receipt.OrganizationID, ActorID: actor, WorkspaceID: receipt.WorkspaceID, OwnerEvidenceReference: reference}, nil
}

func deploymentReceiptAPI(receipt Receipt) (*api.Receipt, error) {
	summary, ok := receipt.Execution["evidenceSummary"].(string)
	if !ok || summary == "" {
		return nil, ErrInvalidReceiptInput
	}
	return &api.Receipt{Id: receipt.ReceiptID, Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_DEPLOYMENT, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, ArtifactDigest: proto.String(receipt.ArtifactID), OperationId: proto.String(receipt.RequestID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: summary, CreatedAt: timestamppb.New(receipt.CreatedAt)}, nil
}
