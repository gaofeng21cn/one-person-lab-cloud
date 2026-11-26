package coordination

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// RenewResources extends the prepaid provider period of one accepted resource
// set.
//
// A renewal is not a new purchase. The compute and storage the Workspace
// already owns are renewed in place, with their original provider identities
// and their original durable mutation claim, so a lost response can never buy a
// second resource. The provider is mutated only after the Ledger confirms the
// wallet charge recorded for this exact renewal obligation, exactly like the
// purchase path.
func (s *Service) RenewResources(ctx context.Context, r *api.RenewResourcesCommand) (*api.Operation, error) {
	if err := peer(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if err := validateRenewResourcesCommand(r); err != nil {
		return nil, err
	}
	tenant := r.GetContext().GetScope().GetTenant().GetTenantId()
	if err := s.authorizeWorkspace(ctx, r.GetContext(), r.GetWorkspaceId(), tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	command := proto.Clone(r).(*api.RenewResourcesCommand)
	command.Context = nil
	// The confirmed charge and the Instance reference are later evidence about
	// this renewal, not its identity. They stay untrusted until their owners
	// can be queried, and must not prevent the same renewal from being resumed
	// once that evidence exists.
	command.ConfirmedChargeReceiptId = ""
	command.InstanceAuthorizationReference = ""
	body, err := protojson.Marshal(command)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "renewal command cannot be encoded")
	}
	normalized, err := (proto.MarshalOptions{Deterministic: true}).Marshal(command)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "renewal command cannot be encoded")
	}
	input := ownerstore.IdempotencyInput{ID: "idem_" + uuid.NewString(), TenantScope: tenant, ActorScope: r.GetContext().GetActorId(), OperationName: "RenewResources", IdempotencyKey: r.GetContext().GetIdempotencyKey(), RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	// Every writer locks the original workspace before reading its resource set:
	// a concurrent renewal cannot dispatch a second provider mutation.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.GetWorkspaceId()); err != nil {
		return nil, persistenceError(err)
	}
	if err = refuseDeletedProvision(ctx, tx, r.GetWorkspaceId(), tenant); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:idem:"+tenant+":"+input.ActorScope+":"+input.IdempotencyKey); err != nil {
		return nil, persistenceError(err)
	}
	// Waiting for a competing command must not preserve an authorization that
	// was revoked or expired while queued. Re-check at the actual write boundary.
	if err = s.authorizeWorkspace(ctx, r.GetContext(), r.GetWorkspaceId(), tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	record, found, err := s.Store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has a different renewal command")
	}
	if err != nil {
		return nil, persistenceError(err)
	}
	if found {
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.renew(ctx, r, record.OperationID)
	}
	setID, storedTenant, billingMode, observation, err := readRenewableResourceSet(ctx, tx, r.GetResourceSetId(), r.GetWorkspaceId())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "no resource set for this workspace")
	}
	if err != nil {
		return nil, err
	}
	if storedTenant != tenant {
		return nil, status.Error(codes.PermissionDenied, "resource belongs to another tenant")
	}
	if observation != "confirmed" {
		return nil, status.Error(codes.FailedPrecondition, "only a confirmed resource set has a provider period to renew")
	}
	// A renewal extends a prepaid provider period. A zero-charge workspace owns
	// no prepaid period, so there is nothing for the provider to extend.
	if billingMode != billingPrepaidMonthly {
		return nil, status.Error(codes.FailedPrecondition, "only a prepaid monthly resource set has a provider period to renew")
	}
	// The provider claim of one renewal is the resource set and the subscription
	// period it renews. A second renewal of the same period would move the same
	// provider deadline twice, so the claim is refused before any dispatch.
	claim := renewalProviderClaim(setID, r.GetSubscriptionPeriodId())
	var claimed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fabric.resource_actions WHERE provider_idempotency_key=$1)`, claim).Scan(&claimed); err != nil {
		return nil, persistenceError(err)
	}
	if claimed {
		return nil, status.Error(codes.AlreadyExists, "this subscription period is already renewed for this resource set")
	}
	operationID := "op_" + uuid.NewString()
	if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tenant, ActorID: r.GetContext().GetActorId(), Kind: "resource_renew", ResourceID: setID, Stage: "provider_renewal", RequestID: r.GetContext().GetRequestId(), AcceptedInput: body}); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='awaiting_confirmation',observation_result='unknown',error_code='DEPENDENCY_UNAVAILABLE' WHERE id=$1`, operationID); err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fabric.resource_actions (id,resource_set_id,command_id,action,provider_idempotency_key,approved_input,observation_result,error_code) VALUES ($1,$2,$3,'renew',$4,$5,'unknown','DEPENDENCY_UNAVAILABLE')`, "raction_"+uuid.NewString(), setID, operationID, claim, body); err != nil {
		return nil, persistenceError(err)
	}
	input.ResourceID, input.OperationID, input.ResponseBody = setID, operationID, []byte(`{}`)
	if err = s.Store.RecordIdempotency(ctx, tx, input); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.renew(ctx, r, operationID)
}

// readRenewableResourceSet reads the resource set this renewal names, together
// with the billing mode of its compute resource. Only the set's own rows are
// read: the renewal never infers a resource set from a caller claim.
func readRenewableResourceSet(ctx context.Context, tx *sql.Tx, setID, workspaceID string) (string, string, string, string, error) {
	var id, tenant, billingMode, observation string
	err := tx.QueryRowContext(ctx, `SELECT rs.id,rs.tenant_id,rs.observation_result,
		(SELECT r.billing_mode FROM fabric.resources r WHERE r.resource_set_id=rs.id AND r.kind='compute')
		FROM fabric.resource_sets rs WHERE rs.id=$1 AND rs.workspace_id=$2`, setID, workspaceID).
		Scan(&id, &tenant, &observation, &billingMode)
	if err != nil {
		return "", "", "", "", err
	}
	return id, tenant, billingMode, observation, nil
}

// renewalProviderClaim names the one provider mutation claim of a renewal. It
// is derived from the resource set and the renewed subscription period, so a
// replay of the same renewal reuses it and a second renewal of the same period
// collides with it.
func renewalProviderClaim(setID, periodID string) string {
	return "renew:" + setID + ":" + periodID
}

// renew dispatches the provider mutation of one accepted renewal and records
// its confirmed effect. A pending or unreadable provider stays unknown, never
// absent, and is retried against the same original resources and keys.
func (s *Service) renew(ctx context.Context, r *api.RenewResourcesCommand, operationID string) (*api.Operation, error) {
	current, err := s.operation(ctx, operationID)
	if err != nil {
		return nil, err
	}
	if current.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || s.Dispatcher == nil {
		return current, nil
	}
	if current.GetResourceId() != r.GetResourceSetId() {
		return nil, status.Error(codes.FailedPrecondition, "the renewal operation belongs to another resource set")
	}
	evidence, authorized := s.renewFundingEvidence(ctx, r)
	if !authorized {
		return current, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fabric:workspace:"+r.GetWorkspaceId()); err != nil {
		return nil, persistenceError(err)
	}
	if err = refuseDeletedProvision(ctx, tx, r.GetWorkspaceId(), r.GetContext().GetScope().GetTenant().GetTenantId()); err != nil {
		return nil, err
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
	intent := ResourceRenewalIntent{
		TenantID:       r.GetContext().GetScope().GetTenant().GetTenantId(),
		WorkspaceID:    r.GetWorkspaceId(),
		OperationID:    operationID,
		ResourceSetID:  current.GetResourceId(),
		PrepaidMonths:  r.GetPrepaidMonths(),
		IdempotencyKey: operationID + ":renew",
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='compute'`, intent.ResourceSetID).Scan(&intent.ComputeID); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM fabric.resources WHERE resource_set_id=$1 AND kind='storage'`, intent.ResourceSetID).Scan(&intent.StorageID); err != nil {
		return nil, persistenceError(err)
	}
	if err = s.authorizeWorkspace(ctx, r.GetContext(), r.GetWorkspaceId(), intent.TenantID, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES); err != nil {
		return nil, err
	}
	result, dispatchErr := s.Dispatcher.RenewResource(ctx, intent)
	if dispatchErr != nil {
		// A pending or unreadable provider stays unknown, never absent, and is
		// retried against the same original resources and keys.
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET status='awaiting_confirmation',stage='provider_renewal',error_code='DEPENDENCY_UNAVAILABLE',observation_result='unknown',updated_at=now() WHERE id=$1`, operationID); err != nil {
			return nil, persistenceError(err)
		}
		if err = tx.Commit(); err != nil {
			return nil, persistenceError(err)
		}
		return s.operation(ctx, operationID)
	}
	if err = confirmRenewedResources(ctx, tx, intent, result); err != nil {
		return nil, err
	}
	effect, err := json.Marshal(resourceEffectIdentity{
		SchemaVersion: 1, Provider: s.Dispatcher.Provider(), OperationID: intent.OperationID, ResourceSetID: intent.ResourceSetID,
		AccountID:  result.Compute.AccountID,
		Compute:    effectResource{ResourceID: result.Compute.ID, ProviderReference: result.Compute.ProviderResourceID, ProviderRequestID: result.Compute.ProviderRequestID},
		Storage:    effectResource{ResourceID: result.Storage.ID, ProviderReference: result.Storage.ProviderResourceID, ProviderRequestID: result.Storage.ProviderRequestID},
		ObservedAt: result.ObservedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, persistenceError(err)
	}
	patch, err := effectPatch(evidence, effect)
	if err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.resource_actions SET observation_result='confirmed',authorization_receipt_ref=$2,provider_request_ref=$3,evidence_ref=$4,error_code=NULL,approved_input=approved_input || $5::jsonb,updated_at=now() WHERE command_id=$1`, operationID, r.GetConfirmedChargeReceiptId(), result.Compute.ProviderRequestID, operationID, patch); err != nil {
		return nil, persistenceError(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, persistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fabric.operations SET result=$2,status='succeeded',stage='succeeded',observation_result='confirmed',error_code=NULL,updated_at=now(),completed_at=now() WHERE id=$1`, operationID, data); err != nil {
		return nil, persistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, persistenceError(err)
	}
	return s.operation(ctx, operationID)
}

// confirmRenewedResources stores the confirmed renewal: the same resources with
// their extended prepaid deadline. A deadline that does not move forward is not
// a renewal and is refused instead of stored as one.
func confirmRenewedResources(ctx context.Context, tx *sql.Tx, intent ResourceRenewalIntent, result ResourceRenewalResult) error {
	observedCompute, err := json.Marshal(result.Compute)
	if err != nil {
		return persistenceError(err)
	}
	observedStorage, err := json.Marshal(result.Storage)
	if err != nil {
		return persistenceError(err)
	}
	for _, resource := range []struct {
		id       string
		observed []byte
		deadline string
	}{
		{intent.ComputeID, observedCompute, result.Compute.Deadline},
		{intent.StorageID, observedStorage, result.Storage.Deadline},
	} {
		var previous sql.NullTime
		if err = tx.QueryRowContext(ctx, `SELECT provider_expires_at FROM fabric.resources WHERE id=$1 FOR UPDATE`, resource.id).Scan(&previous); err != nil {
			return persistenceError(err)
		}
		deadline, ok := providerDeadline(resource.deadline)
		if !ok {
			return status.Error(codes.FailedPrecondition, "the provider did not confirm a renewed prepaid deadline")
		}
		if previous.Valid {
			if !deadline.After(previous.Time.UTC()) {
				return status.Error(codes.FailedPrecondition, "the provider did not extend the prepaid deadline")
			}
			// The confirmed deadline has to cover the prepaid months the Workspace
			// bought. A provider that extended a shorter period would let the
			// Cloud paid period outrun the compute the customer paid for.
			minimum := previous.Time.UTC().Truncate(time.Second).AddDate(0, int(intent.PrepaidMonths), 0)
			if deadline.Truncate(time.Second).Before(minimum) {
				return status.Error(codes.FailedPrecondition, "the provider did not extend the prepaid deadline by the purchased months")
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fabric.resources SET observed_specification=$2,observation_result='confirmed',observed_at=$3,provider_expires_at=$4,updated_at=now() WHERE id=$1`, resource.id, resource.observed, result.ObservedAt, deadline); err != nil {
			return persistenceError(err)
		}
	}
	return nil
}

// renewFundingEvidence verifies the renewal's own funding evidence before any
// provider mutation. The reference string alone proves nothing: the confirmed
// wallet charge is re-read from the Ledger owner for this exact obligation.
func (s *Service) renewFundingEvidence(ctx context.Context, r *api.RenewResourcesCommand) (json.RawMessage, bool) {
	if s.Ledger == nil {
		return nil, false
	}
	receipt, ok := s.readConfirmedCharge(ctx, r.GetContext(), r.GetObligationId(), r.GetConfirmedChargeReceiptId())
	if !ok {
		return nil, false
	}
	return confirmedChargeEvidence(receipt)
}

// providerDeadline parses one provider prepaid deadline. A provider that does
// not report a parseable deadline confirms nothing.
func providerDeadline(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

// validateRenewResourcesCommand admits the renewal command this owner serves.
// A renewal names the Workspace, the resource set it renews, the renewed
// subscription period, the prepaid months it buys, and the obligation whose
// confirmed charge proves it.
func validateRenewResourcesCommand(r *api.RenewResourcesCommand) error {
	if r == nil || strings.TrimSpace(r.GetWorkspaceId()) == "" || strings.TrimSpace(r.GetResourceSetId()) == "" ||
		strings.TrimSpace(r.GetSubscriptionPeriodId()) == "" || strings.TrimSpace(r.GetObligationId()) == "" ||
		strings.TrimSpace(r.GetConfirmedChargeReceiptId()) == "" || strings.TrimSpace(r.GetContext().GetIdempotencyKey()) == "" {
		return status.Error(codes.InvalidArgument, "workspace, resource set, subscription period, obligation, charge receipt and idempotency key are required")
	}
	if r.GetPrepaidMonths() <= 0 {
		return status.Error(codes.InvalidArgument, "prepaidMonths must be positive")
	}
	return nil
}
