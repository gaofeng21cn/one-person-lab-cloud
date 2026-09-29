package coordination

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// resume continues one committed resource intent. A provider mutation is
// attempted only after the owning funding evidence for that provider's approved
// billing mode is verified, and every attempt reuses the original resource IDs,
// provider keys and operation so a lost response cannot mint a second purchase.
func (s *Service) resume(ctx context.Context, r *api.EnsureResourcesCommand, operationID string) (*api.Operation, error) {
	current, err := s.operation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if current.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || s.Dispatcher == nil {
		return current, nil
	}
	plan := r.GetPlan()
	if plan.GetProvider() != s.Dispatcher.Provider() {
		return current, nil
	}
	fundingEvidence, authorized := s.dispatchFundingEvidence(ctx, r)
	if !authorized {
		return current, nil
	}
	// A transaction-scoped workspace lock serializes dispatch as well as intent
	// creation. The provider still owns its durable mutation claims; a lost target
	// commit replays those same resource IDs and original keys.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.WorkspaceId); err != nil {
		return nil, persistenceError(err)
	}
	var existingStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM fabric.operations WHERE id=$1 FOR UPDATE`, operationID).Scan(&existingStatus); err != nil {
		return nil, persistenceError(err)
	}
	if existingStatus == "succeeded" {
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	intent := ResourceIntent{TenantID: r.GetContext().GetScope().GetTenant().GetTenantId(), WorkspaceID: r.GetWorkspaceId(), OperationID: operationID, ResourceSetID: current.GetResourceId(), Plan: plan}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='compute'`, intent.ResourceSetID).Scan(&intent.ComputeID); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='storage'`, intent.ResourceSetID).Scan(&intent.StorageID); err != nil {
		return nil, persistenceError(err)
	}
	if err = s.authorizeWorkspace(ctx, r.GetContext(), r.GetWorkspaceId(), intent.TenantID, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	result, dispatchErr := s.Dispatcher.EnsureResources(ctx, intent)
	if dispatchErr != nil {
		// A pending or unreadable provider stays unknown, never absent, and is
		// retried against the same original intent.
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='awaiting_confirmation',stage='resource_preflight',error_code='DEPENDENCY_UNAVAILABLE',observation_result='unknown',updated_at=now() WHERE id=$1`, operationID); err != nil {
			return nil, persistenceError(err)
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	if err = validResourceResult(intent, result, s.Dispatcher.Provider()); err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	for _, resource := range []struct {
		id, provider string
		expiresAt    string
		value        any
	}{
		{intent.ComputeID, result.Compute.ProviderResourceID, result.Compute.Deadline, result.Compute},
		{intent.StorageID, result.Storage.ProviderResourceID, result.Storage.Deadline, result.Storage},
	} {
		observed, e := json.Marshal(resource.value)
		if e != nil {
			return nil, e
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.resources SET provider_resource_ref=$2,observed_specification=$3,observation_result='confirmed',observed_at=$4,provider_expires_at=$5,updated_at=now() WHERE id=$1`, resource.id, resource.provider, observed, result.ObservedAt, providerExpiry(resource.expiresAt)); err != nil {
			return nil, persistenceError(err)
		}
	}
	if err = confirmResourceNetwork(ctx, tx, intent, result, plan); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.resource_sets SET observation_result='confirmed',observed_at=$2,updated_at=now() WHERE id=$1`, intent.ResourceSetID, result.ObservedAt); err != nil {
		return nil, persistenceError(err)
	}
	effect, err := json.Marshal(resourceEffectIdentity{
		SchemaVersion: 1, Provider: s.Dispatcher.Provider(), OperationID: intent.OperationID, ResourceSetID: intent.ResourceSetID,
		AccountID:  result.Binding.GetAccountId(),
		Compute:    effectResource{ResourceID: result.Compute.ID, ProviderReference: result.Compute.ProviderResourceID, ProviderRequestID: result.Compute.ProviderRequestID},
		Storage:    effectResource{ResourceID: result.Storage.ID, ProviderReference: result.Storage.ProviderResourceID, ProviderRequestID: result.Storage.ProviderRequestID},
		Attachment: effectResource{ResourceID: result.Attachment.ID, ProviderReference: result.Attachment.ProviderAttachmentID, ProviderRequestID: result.Attachment.ProviderRequestID},
		Network:    effectResource{ProviderReference: result.Network.ProviderReference},
		ObservedAt: result.ObservedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	patch, err := effectPatch(fundingEvidence, effect)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.resource_actions SET observation_result='confirmed',authorization_receipt_ref=$2,provider_request_ref=$3,evidence_ref=$4,error_code=NULL,approved_input=approved_input || $5::jsonb,updated_at=now() WHERE command_id=$1`, operationID, r.GetConfirmedChargeReceiptId(), result.Attachment.ProviderRequestID, operationID, patch); err != nil {
		return nil, persistenceError(err)
	}
	// The provider's data-attachment preparation is preserved in the operation
	// result. Target fabric.attachments requires a real execution resource, which
	// does not exist until Serve creates its runtime; a compute network is not one.
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='succeeded',stage='actual_resource_evidence',observation_result='confirmed',error_code=NULL,result=$2,completed_at=now(),updated_at=now() WHERE id=$1`, operationID, data); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.operation(ctx, operationID)
}

// effectResource is one provider effect identity read back from the provider
// authority. It is persisted so a later attempt observes the original purchase
// instead of issuing a new one.
type effectResource struct {
	ResourceID        string `json:"resourceId,omitempty"`
	ProviderReference string `json:"providerReference"`
	ProviderRequestID string `json:"providerRequestId,omitempty"`
}

type resourceEffectIdentity struct {
	SchemaVersion int            `json:"schemaVersion"`
	Provider      string         `json:"provider"`
	OperationID   string         `json:"operationId"`
	ResourceSetID string         `json:"resourceSetId"`
	AccountID     string         `json:"accountId"`
	Compute       effectResource `json:"compute"`
	Storage       effectResource `json:"storage"`
	Attachment    effectResource `json:"attachment"`
	Network       effectResource `json:"network"`
	ObservedAt    string         `json:"observedAt"`
}

func effectPatch(fundingEvidence, effect json.RawMessage) (json.RawMessage, error) {
	patch := map[string]json.RawMessage{"resourceEffectIdentity": effect}
	if len(fundingEvidence) > 0 {
		patch["fundingEvidence"] = fundingEvidence
	}
	return json.Marshal(patch)
}

// confirmResourceNetwork materializes the provider-authoritative network
// placement of this resource set as its own resource fact. The identity is
// derived from the resource set, so a retry never creates a second row.
func confirmResourceNetwork(ctx context.Context, tx *sql.Tx, intent ResourceIntent, result *ResourceResult, plan *api.ResourcePlanSnapshot) error {
	requested, err := protojson.Marshal(plan)
	if err != nil {
		return err
	}
	observed, err := json.Marshal(result.Network)
	if err != nil {
		return err
	}
	// The plan is the accepted request, not a provider fact. It is stored once as
	// the requested specification of the derived network row.
	_, err = tx.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_resource_ref,provider_purchase_key,billing_mode,requested_specification,observed_specification,observation_result,observed_at) VALUES ($1,$2,'network',$3,$4,$5,$6,$7,'confirmed',$8) ON CONFLICT (id) DO UPDATE SET provider_resource_ref=EXCLUDED.provider_resource_ref,observed_specification=EXCLUDED.observed_specification,observation_result='confirmed',observed_at=EXCLUDED.observed_at,updated_at=now()`, "res_network_"+shortDigest(intent.ResourceSetID), intent.ResourceSetID, result.Network.ProviderReference, "network:"+intent.ResourceSetID, plan.GetBillingMode(), requested, observed, result.ObservedAt)
	if err != nil {
		return persistenceError(err)
	}
	return nil
}

// providerExpiry converts one provider prepaid deadline into the persisted
// expiry. A provider that does not report a deadline leaves it empty.
func providerExpiry(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return parsed.UTC()
}

// dispatchFundingEvidence verifies the original owner funding evidence required
// before any provider mutation. It returns the evidence bytes to persist beside
// the confirmed effect identity. A provider or billing mode whose evidence is
// not owner-verifiable never dispatches provider work.
func (s *Service) dispatchFundingEvidence(ctx context.Context, r *api.EnsureResourcesCommand) (json.RawMessage, bool) {
	plan := r.GetPlan()
	if plan == nil || strings.TrimSpace(r.GetConfirmedChargeReceiptId()) == "" {
		return nil, false
	}
	switch plan.GetProvider() {
	case providerLocalDocker:
		if plan.GetBillingMode() != billingLocalNoCharge || r.GetQuoteAcceptance().GetQuote().GetTotalUsdMicros() != 0 || s.Ledger == nil {
			return nil, false
		}
		evidence, ok := s.readLocalNoChargeEvidence(ctx, r)
		if !ok {
			return nil, false
		}
		proof, err := protojson.Marshal(evidence)
		if err != nil {
			return nil, false
		}
		return proof, true
	case providerTencentTKE:
		// Strict prepaid monthly purchase only. The confirmed charge reference is
		// not trusted on its own: Fabric re-reads the exact confirmed wallet charge
		// from the Ledger owner for this obligation before any provider mutation.
		if plan.GetBillingMode() != billingPrepaidMonthly || plan.GetPrepaidMonths() <= 0 || s.Ledger == nil {
			return nil, false
		}
		receipt, ok := s.readConfirmedWalletCharge(ctx, r)
		if !ok {
			return nil, false
		}
		return confirmedChargeEvidence(receipt)
	default:
		return nil, false
	}
}

// confirmedChargeEvidence wraps the owner-read confirmed wallet charge so the
// persisted funding evidence identifies both its source and the exact receipt.
func confirmedChargeEvidence(receipt *api.Receipt) (json.RawMessage, bool) {
	raw, err := protojson.Marshal(receipt)
	if err != nil {
		return nil, false
	}
	evidence, err := json.Marshal(struct {
		SchemaVersion int             `json:"schemaVersion"`
		Source        string          `json:"source"`
		Receipt       json.RawMessage `json:"receipt"`
	}{SchemaVersion: 1, Source: "ledger_confirmed_wallet_charge", Receipt: raw})
	if err != nil {
		return nil, false
	}
	return evidence, true
}

// readConfirmedWalletCharge re-reads the owner-authoritative confirmed charge for
// this exact obligation. A missing, mismatched, unconfirmed or non-wallet receipt
// is never funding evidence.
func (s *Service) readConfirmedWalletCharge(ctx context.Context, r *api.EnsureResourcesCommand) (*api.Receipt, bool) {
	call := proto.Clone(r.GetContext()).(*api.CallContext)
	call.AuthorizationContextId = ""
	receipt, err := s.Ledger.ReadReceiptByReference(ctx, &api.GetReceiptByReferenceRequest{Context: call, Owner: "workspace", OwnerEvidenceReference: r.GetObligationId()})
	if err != nil {
		return nil, false
	}
	if receipt.GetId() != r.GetConfirmedChargeReceiptId() || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOperationId() != r.GetObligationId() || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION ||
		receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED {
		return nil, false
	}
	return receipt, true
}

func (s *Service) readLocalNoChargeEvidence(ctx context.Context, r *api.EnsureResourcesCommand) (*api.LocalNoChargeReceiptEvidence, bool) {
	call := proto.Clone(r.GetContext()).(*api.CallContext)
	call.AuthorizationContextId = ""
	evidence, err := s.Ledger.ReadLocalNoChargeReceipt(ctx, &api.GetReceiptByReferenceRequest{Context: call, Owner: "workspace", OwnerEvidenceReference: r.GetObligationId()})
	if err != nil {
		return nil, false
	}
	receipt, commit := evidence.GetReceipt(), evidence.GetOwnerCommitEvidence()
	if receipt.GetId() != r.GetConfirmedChargeReceiptId() || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetOperationId() != r.GetObligationId() ||
		!proto.Equal(evidence.GetQuoteAcceptance(), r.GetQuoteAcceptance()) || commit.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || commit.GetOperationId() != r.GetObligationId() || commit.GetResourceId() != r.GetWorkspaceId() ||
		commit.GetActorId() != r.GetContext().GetActorId() || !proto.Equal(commit.GetScope(), r.GetContext().GetScope()) || commit.GetCommittedVersion() <= 0 || evidence.GetEvidenceDigest() != r.GetQuoteAcceptance().GetSnapshotDigest() {
		return nil, false
	}
	return evidence, true
}

// ReadResourceResult reads only a confirmed original operation's provider
// evidence.
func readResourceResult(ctx context.Context, tx *sql.Tx, setID string) (*ResourceResult, error) {
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT result FROM fabric.operations WHERE resource_id=$1 AND kind='resource_provision' AND status='succeeded' AND observation_result='confirmed'`, setID).Scan(&data); err != nil {
		return nil, err
	}
	var result ResourceResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
