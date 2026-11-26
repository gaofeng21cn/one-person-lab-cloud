package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerstore"
)

// renewalKind is this owner's operation kind for one accepted manual renewal.
// A renewal is its own accepted obligation: it continues the subscription a
// Workspace already owns, and it is authorized, charged and recorded under its
// own identity instead of under the launch that created the Workspace.
const renewalKind = "renew_workspace"

// renewalContinuationActions is the continuation surface of an accepted
// renew_workspace obligation: reading and accepting the renewal offer, charging
// it once, recording its receipt, observing the resources it renews and extending
// their prepaid period.
//
// It is exactly the group the identity owner grants for a renewal, so the two
// owners admit the same actions. It carries no provisioning write and no
// release: a renewal can neither create resources nor give up the ones it
// renews.
var renewalContinuationActions = []api.AuthorizationActionEnum{
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWACCEPTEDRESOURCES,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CHARGEACCEPTEDOBLIGATION,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READWALLETACTION,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT,
}

// RenewWorkspace accepts one manual renewal of a paid Workspace.
//
// A renewal continues the subscription the Workspace already owns: it accepts
// the renewal offer the Catalog priced, charges the customer once, extends the
// prepaid period of the resources the Workspace already runs, and records the
// confirmed period the Workspace is now paid for.
//
// A renewal whose paid term has already ended is refused before any charge. The
// provider extends its prepaid deadline from its own expiry date, so a renewal
// bought after the term ended would charge the customer for a month the
// provider does not cover.
func (s *Service) RenewWorkspace(ctx context.Context, r *api.RenewWorkspaceRpcRequest) (*api.Operation, error) {
	c, b := r.GetContext(), r.GetBody()
	tid := c.GetScope().GetTenant().GetTenantId()
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if tid == "" || b == nil || b.GetQuoteId() == "" || workspaceID == "" || c.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant, workspace, quote and idempotency key are required")
	}
	if err := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE, workspaceResource(workspaceID), tid); err != nil {
		return nil, err
	}
	normalized, err := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid Workspace renewal input")
	}
	idem := ownerstore.IdempotencyInput{ID: id("idem_"), TenantScope: tid, ActorScope: c.ActorId, OperationName: "renewWorkspace", IdempotencyKey: c.GetIdempotencyKey(), RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tid+"/"+c.ActorId+"/renewWorkspace/"+c.IdempotencyKey); err != nil {
		return nil, dbError(err)
	}
	replay, found, err := s.Store.LookupIdempotency(ctx, tx, idem)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if err != nil {
		return nil, dbError(err)
	}
	if found {
		tx.Rollback()
		op, err := s.Store.ReadOperation(ctx, replay.OperationID)
		if err != nil {
			return nil, dbError(err)
		}
		return renewalOperation(op)
	}
	readCall := proto.Clone(c).(*api.CallContext)
	readCall.AuthorizationContextId = ""
	quote, err := s.Catalog.ReadQuoteResourcePlan(ctx, &api.QuoteResourcePlanRequest{Context: readCall, QuoteId: b.GetQuoteId()})
	if err != nil {
		return nil, err
	}
	q := quote.GetQuote()
	if q.GetId() != b.GetQuoteId() || q.GetPurpose() != api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_RENEW || q.GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED ||
		q.GetExpiresAt() == nil || !q.GetExpiresAt().AsTime().After(s.lifecycleNow()) || quote.GetResourcePlan() == nil {
		return nil, status.Error(codes.FailedPrecondition, "an unexpired renewal quote with a frozen resource plan is required")
	}
	// The offer is priced for one Workspace. Renewing another one would buy a
	// month the price was never quoted for.
	if q.GetWorkspaceId() != workspaceID {
		return nil, status.Error(codes.FailedPrecondition, "the renewal quote is bound to another workspace")
	}
	// Only a prepaid monthly resource set owns a provider period to extend. A
	// zero-charge Workspace never expires and has nothing to renew.
	if quote.GetResourcePlan().GetBillingMode() != "PREPAID_MONTHLY" {
		return nil, status.Error(codes.FailedPrecondition, "only a prepaid monthly workspace can be renewed")
	}
	// The owner's own confirmed facts decide whether a renewal may be charged at
	// all, before the offer is consumed and before any money moves.
	if _, err = s.requireRenewableTerm(ctx, tid, workspaceID); err != nil {
		return nil, err
	}
	// Revalidate after the command lock and the quote read. A request queued
	// behind another transaction must not commit using a pre-lock permission.
	decisionRequest := &api.AuthorizationRequest{Scope: c.Scope, ActorId: c.ActorId, SessionId: c.SessionId, AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE, Resource: workspaceResource(workspaceID), RequestId: c.RequestId}
	decision, err := s.Identity.AuthorizeAction(ctx, decisionRequest)
	if err != nil {
		return nil, err
	}
	if err = owneridentity.ValidateDecision(decisionRequest, decision, time.Now()); err != nil || decision.GetAuthorizationContextId() == "" {
		return nil, status.Error(codes.PermissionDenied, "fresh Workspace authorization required")
	}
	oid := id("op_")
	material, _ := json.Marshal(struct {
		Request json.RawMessage `json:"request"`
		Quote   json.RawMessage `json:"quote"`
	}{wire(b), wire(quote)})
	accepted := acceptedOrder{Request: wire(b), Quote: wire(quote), AuthorizationContextID: decision.GetAuthorizationContextId(), InputDigest: "sha256:" + ownerstore.HashRequestBody(material)}
	raw, _ := json.Marshal(accepted)
	op, err := s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: oid, TenantID: tid, ActorID: c.ActorId, Kind: renewalKind, ResourceID: workspaceID, Stage: "quote_binding", RequestID: c.RequestId, AcceptedInput: raw})
	if err != nil {
		return nil, dbError(err)
	}
	// Two live renewals of one Workspace would charge the customer twice for one
	// month, and the second month would not be delivered. The renewal that is
	// already running is the one that owns this Workspace's subscription.
	if err = s.refuseConcurrentRenewal(ctx, tx, workspaceID, oid); err != nil {
		return nil, err
	}
	// PostgreSQL now() is transaction-start time, which precedes the fresh
	// post-lock decision. Record the actual acceptance instant for grant proof.
	if err = tx.QueryRowContext(ctx, `UPDATE workspace.operations SET created_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 RETURNING created_at,updated_at`, oid).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		return nil, dbError(err)
	}
	idem.ResourceID = workspaceID
	idem.OperationID = oid
	acceptedOperation, err := renewalOperation(op)
	if err != nil {
		return nil, err
	}
	idem.ResponseBody = wire(acceptedOperation)
	if err = s.Store.RecordIdempotency(ctx, tx, idem); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	// The durable order precedes every cross-owner side effect. A lost response
	// leaves the original renewal recoverable, never a second charge.
	_ = s.Resume(ctx, oid)
	op, err = s.Store.ReadOperation(ctx, oid)
	if err != nil {
		return nil, dbError(err)
	}
	return renewalOperation(op)
}

// refuseConcurrentRenewal refuses a renewal while another renewal of the same
// Workspace is still open. Two live renewals would charge the customer twice
// for one month, and only one of them could advance the subscription.
func (s *Service) refuseConcurrentRenewal(ctx context.Context, tx *sql.Tx, workspaceID, operationID string) error {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT id FROM workspace.operations WHERE resource_id=$1 AND kind='renew_workspace'
		AND status IN ('accepted','running','awaiting_confirmation') AND id<>$2
		ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, operationID).Scan(&existing)
	if err == nil {
		return status.Error(codes.FailedPrecondition, "another renewal of this workspace is already running")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	}
	return nil
}

// requireRenewableTerm refuses a renewal this owner cannot deliver. It reads the
// owner's own confirmed facts: a Workspace that has no confirmed paid term, whose
// term has already ended, that is not active, or whose resources are gone is not
// renewable.
//
// A renewal whose paid term has ended is refused here, before any charge: the
// provider extends its prepaid deadline from its own expiry date, so a renewal
// bought after the term ended would charge the customer for a month the
// provider does not cover.
func (s *Service) requireRenewableTerm(ctx context.Context, tenant, workspaceID string) (workspaceStanding, error) {
	st, err := s.readWorkspaceStanding(ctx, workspaceID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return st, status.Error(codes.NotFound, "workspace not found")
		}
		return st, err
	}
	if st.TenantID != tenant {
		return st, status.Error(codes.PermissionDenied, "workspace belongs to another tenant")
	}
	if st.SubscriptionID == "" || !st.PeriodConfirmed {
		return st, status.Error(codes.FailedPrecondition, "only a workspace with a confirmed paid term can be renewed")
	}
	if st.expiredTerm(s.lifecycleNow()) {
		return st, status.Error(codes.FailedPrecondition, "the paid term of this workspace has ended; the renewal is refused before any charge")
	}
	if st.Status != "active" {
		return st, status.Error(codes.FailedPrecondition, "only an active workspace can be renewed")
	}
	if st.ResourceSetID == "" {
		return st, status.Error(codes.FailedPrecondition, "the workspace has no confirmed resource set to renew")
	}
	return st, nil
}

// resumeRenewal continues one committed renewal under its own worker lease.
//
// The order of the steps is the order of the money and of the delivery: the
// offer is accepted, the resources that would be renewed are observed, the
// customer is charged once, the provider extends the prepaid deadline, and only
// then is the new paid period recorded. A renewal that cannot deliver is
// refused before the charge, never after it.
func (s *Service) resumeRenewal(ctx context.Context, operationID string) error {
	ctx, cancel := context.WithTimeout(ctx, resumeTimeout)
	defer cancel()
	op, err := s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	if op.Terminal() {
		return nil
	}
	token := id("lease_")
	var leased string
	err = s.Store.DB().QueryRowContext(ctx, `UPDATE workspace.operations SET worker_lease_token=$2,worker_lease_until=now()+interval '60 seconds'
		WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled') AND (worker_lease_until IS NULL OR worker_lease_until<now()) RETURNING id`, operationID, token).Scan(&leased)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return dbError(err)
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer releaseCancel()
		_, _ = s.Store.DB().ExecContext(releaseCtx, `UPDATE workspace.operations SET worker_lease_token=NULL,worker_lease_until=NULL WHERE id=$1 AND worker_lease_token=$2`, operationID, token)
	}()
	op, err = s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	a, result, err := decodeOrder(op)
	if err != nil {
		return err
	}
	offer := &api.QuoteAcceptance{}
	if protojson.Unmarshal(a.Quote, offer) != nil || offer.GetQuote().GetId() == "" || offer.GetResourcePlan() == nil {
		return status.Error(codes.DataLoss, "stored Workspace renewal quote is invalid")
	}
	// The renewal is its own accepted obligation, so it asks the identity owner
	// for its own grant instead of reusing the launch's grant.
	if result.GrantID == "" {
		e, err := renewalEvidence(op)
		if err != nil {
			return err
		}
		request := &api.AcceptedOperationGrantRequest{AuthorizationContextId: a.AuthorizationContextID, OwnerCommitEvidence: e, AllowedActions: renewalContinuationActions}
		if err = s.beginStep(ctx, op, token, "grant", 0, "identity", "identity_verification", request); err != nil {
			return err
		}
		grant, err := s.Identity.IssueAcceptedOperationGrant(ctx, request)
		if err != nil {
			return s.failedRenewal(ctx, op, token, "grant", "identity_verification", result, err)
		}
		if grant.GetId() == "" || grant.GetAcceptedOperationId() != op.ID || grant.GetResourceId() != op.ResourceID || grant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || grant.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE || grant.GetActorId() != op.ActorID || !proto.Equal(grant.GetScope(), e.Scope) {
			return s.failedRenewal(ctx, op, token, "grant", "identity_verification", result, status.Error(codes.DataLoss, "accepted grant does not identify the Workspace renewal"))
		}
		result.GrantID = grant.Id
		if err = s.checkpoint(ctx, op, token, "grant", "quote_binding", "running", "confirmed", grant.Id, "", result); err != nil {
			return err
		}
	}
	accepted := &api.QuoteAcceptance{}
	if len(result.Acceptance) == 0 {
		// The owner's own facts are re-read at the write boundary: a term that
		// ended while the renewal waited is refused before the offer is consumed.
		if _, err = s.requireRenewableTerm(ctx, op.TenantID, op.ResourceID); err != nil {
			return s.failedRenewal(ctx, op, token, "renewable_term", "quote_binding", result, err)
		}
		request := &api.AcceptQuoteRequest{Context: continuation(op, result.GrantID, "accept_quote"), QuoteId: offer.Quote.Id, WorkspaceId: op.ResourceID, ObligationId: op.ID}
		if err = s.beginStep(ctx, op, token, "accept_quote", 1, "resource_catalog", "quote_binding", request); err != nil {
			return err
		}
		accepted, err = s.Catalog.AcceptQuote(ctx, request)
		if err != nil {
			return s.failedRenewal(ctx, op, token, "accept_quote", "quote_binding", result, err)
		}
		if err = validateAcceptance(op, offer, accepted); err != nil {
			return s.failedRenewal(ctx, op, token, "accept_quote", "quote_binding", result, err)
		}
		result.Acceptance = wire(accepted)
		if err = s.checkpoint(ctx, op, token, "accept_quote", "resource_preflight", "running", "confirmed", accepted.AcceptanceId, "", result); err != nil {
			return err
		}
	} else if protojson.Unmarshal(result.Acceptance, accepted) != nil {
		return status.Error(codes.DataLoss, "stored Workspace renewal acceptance is invalid")
	}
	if err = validateAcceptance(op, offer, accepted); err != nil {
		return err
	}
	st, err := s.requireRenewableTerm(ctx, op.TenantID, op.ResourceID)
	if err != nil {
		return s.failedRenewal(ctx, op, token, "renewable_term", "resource_preflight", result, err)
	}
	// The renewed period continues the confirmed paid-through: the customer keeps
	// the days it already paid for and receives a full month on top of them.
	anchor := st.PeriodEnd.Time.UTC()
	if !anchor.After(s.lifecycleNow()) {
		return s.failedRenewal(ctx, op, token, "renewable_term", "resource_preflight", result, status.Error(codes.FailedPrecondition, "the paid term of this workspace has ended; the renewal is refused before any charge"))
	}
	// A second renewal of the same paid month would charge the customer twice
	// for one month, so a period this subscription already confirmed is refused
	// before any charge.
	if err = s.refuseRepeatedPeriod(ctx, st.SubscriptionID, anchor); err != nil {
		return s.failedRenewal(ctx, op, token, "renewable_term", "resource_preflight", result, err)
	}
	request := &api.ResourceReadbackRequest{Context: continuation(op, result.GrantID, "read_resources"), ResourceSetId: st.ResourceSetID}
	if err = s.beginStep(ctx, op, token, "read_resources", 2, "fabric", "resource_preflight", request); err != nil {
		return err
	}
	readback, err := s.Fabric.ReadResources(ctx, request)
	if err != nil {
		return s.failedRenewal(ctx, op, token, "read_resources", "resource_preflight", result, err)
	}
	if readback.GetResourceSetId() != st.ResourceSetID || readback.GetWorkspaceId() != op.ResourceID {
		return s.failedRenewal(ctx, op, token, "read_resources", "resource_preflight", result, status.Error(codes.DataLoss, "Fabric readback does not identify the renewed resources"))
	}
	switch readback.GetOutcome() {
	case api.Observation_OBSERVATION_CONFIRMED:
		if readback.GetAbsenceConfirmed() {
			return s.failedRenewal(ctx, op, token, "read_resources", "resource_preflight", result, status.Error(codes.FailedPrecondition, "the resources of this workspace are gone; the renewal is refused before any charge"))
		}
	case api.Observation_OBSERVATION_REJECTED:
		return s.failedRenewal(ctx, op, token, "read_resources", "resource_preflight", result, status.Error(codes.FailedPrecondition, "the resources of this workspace are not confirmed; the renewal is refused before any charge"))
	default:
		// An unanswered provider read proves nothing about the resources. The
		// renewal waits for the answer instead of charging for a renewal it
		// cannot confirm.
		return s.checkpoint(ctx, op, token, "read_resources", "resource_preflight", "awaiting_confirmation", "unknown", st.ResourceSetID, "DEPENDENCY_UNAVAILABLE", result)
	}
	result.ResourceSetID = st.ResourceSetID
	// The charge is the customer's money, so it happens exactly once for this
	// obligation and only after the renewal is known to be deliverable.
	receiptID, fundingRefused, err := s.fundingEvidence(ctx, op, token, accepted, &result)
	if err != nil {
		return err
	}
	if fundingRefused {
		// The funding owner explicitly refused this renewal, so no resource may
		// be renewed for it. The recorded refusal is the operation's outcome.
		return nil
	}
	if receiptID == "" {
		return nil
	}
	renewalRequest := &api.RenewResourcesCommand{
		Context:                  continuation(op, result.GrantID, "renew_resources"),
		WorkspaceId:              op.ResourceID,
		ResourceSetId:            st.ResourceSetID,
		SubscriptionPeriodId:     derivedID("period-", op.ID),
		PrepaidMonths:            int32(accepted.GetQuote().GetPeriodMonths()),
		ConfirmedChargeReceiptId: receiptID,
		ObligationId:             op.ID,
	}
	if err = s.beginStep(ctx, op, token, "renew_resources", 4, "fabric", "provider_renewal", renewalRequest); err != nil {
		return err
	}
	renewal, err := s.Fabric.RenewResources(ctx, renewalRequest)
	if err != nil {
		return s.failedRenewal(ctx, op, token, "renew_resources", "provider_renewal", result, err)
	}
	if renewal.GetOperationId() == "" || renewal.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC || renewal.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_RENEW || renewal.GetResourceId() != st.ResourceSetID {
		return s.failedRenewal(ctx, op, token, "renew_resources", "provider_renewal", result, status.Error(codes.DataLoss, "Fabric returned a different renewal operation"))
	}
	result.FabricOperationID = renewal.GetOperationId()
	if renewal.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || renewal.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		// The provider has not confirmed the extension yet. The renewal waits and
		// is retried against the same resources and the same keys, so a pending
		// provider never buys a second month.
		return s.checkpoint(ctx, op, token, "renew_resources", "provider_renewal", "awaiting_confirmation", "unknown", renewal.GetOperationId(), "DEPENDENCY_UNAVAILABLE", result)
	}
	return s.activateRenewedPeriod(ctx, op, token, accepted, &result, anchor)
}

// refuseRepeatedPeriod refuses a renewal of a paid period this subscription has
// already confirmed. Two renewals of one month would charge the customer twice
// for one month, so the second one is refused before the charge.
func (s *Service) refuseRepeatedPeriod(ctx context.Context, subscriptionID string, periodStart time.Time) error {
	if subscriptionID == "" || periodStart.IsZero() {
		return status.Error(codes.FailedPrecondition, "the renewed paid period cannot be identified")
	}
	var existing string
	err := s.Store.DB().QueryRowContext(ctx, `SELECT id FROM workspace.subscription_periods WHERE subscription_id=$1 AND period_start=$2 LIMIT 1`, subscriptionID, periodStart).Scan(&existing)
	if err == nil {
		return status.Error(codes.AlreadyExists, "this workspace was already renewed for this paid period")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	}
	return nil
}

// activateRenewedPeriod records the renewed paid period: the confirmed period
// of this renewal, the subscription that now carries it, and the terminal
// confirmed state of the renewal operation.
//
// The renewed period is anchored on the confirmed paid-through, not on the
// moment of the renewal: the customer keeps the days it already paid for and
// receives a full month on top of them. The Catalog prices the offer as a
// month of the Workspace's own plan pair and leaves the placement of that
// month to the owner of the subscription.
//
// Every write is one owner transaction, so a reader never observes a renewed
// period without the confirmed charge that paid for it, and a replay of the same
// renewal never inserts a second entitlement.
func (s *Service) activateRenewedPeriod(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, result *orderResult, anchor time.Time) error {
	funding, err := result.paidFundingEvidence()
	if err != nil {
		return err
	}
	if err = validatePaidFundingEvidence(op, accepted, funding); err != nil {
		return err
	}
	quote, charge, receipt := accepted.GetQuote(), funding.GetWalletOperation(), funding.GetReceipt()
	if quote.GetPeriodMonths() <= 0 || quote.GetPricePolicyVersionId() == "" || quote.GetTotalUsdMicros() <= 0 {
		return status.Error(codes.DataLoss, "the accepted renewal quote cannot define a confirmed paid period")
	}
	snapshot, err := protojson.Marshal(accepted)
	if err != nil {
		return status.Error(codes.Internal, "the accepted renewal snapshot cannot be encoded")
	}
	periodID := derivedID("period-", op.ID)
	billingKey := "billing:" + op.ID
	raw, err := json.Marshal(result)
	if err != nil {
		return status.Error(codes.Internal, "Workspace result cannot be encoded")
	}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	// The subscription this renewal continues is locked, so two renewals cannot
	// advance the same paid period at once.
	var subscriptionID string
	var currentStart, currentEnd time.Time
	err = tx.QueryRowContext(ctx, `SELECT id,current_period_start,current_period_end FROM workspace.subscriptions WHERE workspace_id=$1 FOR UPDATE`, op.ResourceID).Scan(&subscriptionID, &currentStart, &currentEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.FailedPrecondition, "the workspace this renewal continues has no subscription")
	}
	if err != nil {
		return dbError(err)
	}
	// The anchor is the confirmed paid-through. It is re-read under the lock, so
	// a renewal that another renewal already advanced is refused instead of
	// buying a second month.
	if !currentEnd.UTC().Equal(anchor) {
		return status.Error(codes.FailedPrecondition, "another renewal already advanced this subscription")
	}
	periodStart, periodEnd := anchor, anchor.AddDate(0, int(quote.GetPeriodMonths()), 0)
	// The confirmed period is append-only history. A replay with the same
	// original identities inserts nothing, and the readback below still has to
	// equal this renewal's exact charge receipt.
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscription_periods
		(id, subscription_id, quote_id, accepted_quote_snapshot, period_start, period_end,
		 billing_key, charge_wallet_operation_id, charge_receipt_id, provenance)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'quoted')
		ON CONFLICT DO NOTHING`,
		periodID, subscriptionID, quote.GetId(), snapshot, periodStart, periodEnd,
		billingKey, charge.GetId(), receipt.GetId()); err != nil {
		return dbError(err)
	}
	if err = validateStoredRenewedPeriod(ctx, tx, periodID, subscriptionID, billingKey, accepted, charge, receipt.GetId(), periodStart, periodEnd); err != nil {
		return err
	}
	// The subscription now carries the renewed period and the price it was
	// bought at. The Workspace itself keeps running: a renewal changes what the
	// customer is paid for, not whether the Workspace is served.
	updated, err := tx.ExecContext(ctx, `UPDATE workspace.subscriptions SET
		current_period_start=$2, current_period_end=$3, last_charge_wallet_operation_id=$4,
		accepted_quote_id=$5, accepted_quote_snapshot=$6, current_price_policy_version_id=$7,
		current_monthly_usd_micros=$8, billing_anchor_day=$9, period_months=$10,
		version=version+1, updated_at=now()
		WHERE id=$1 AND workspace_id=$11`, subscriptionID, periodStart, periodEnd, charge.GetId(),
		quote.GetId(), snapshot, quote.GetPricePolicyVersionId(), quote.GetTotalUsdMicros(),
		int(periodStart.Day()), quote.GetPeriodMonths(), op.ResourceID)
	if err != nil {
		return dbError(err)
	}
	if changed, err := updated.RowsAffected(); err != nil {
		return dbError(err)
	} else if changed != 1 {
		return status.Error(codes.Aborted, "the subscription this renewal advances is not the one it continues")
	}
	completed, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET result=$3,status='succeeded',stage='succeeded',observation_result='confirmed',error_code=NULL,updated_at=now(),completed_at=now()
		WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, token, raw)
	if err = fenced(completed, err); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

// validateStoredRenewedPeriod reads the confirmed period of one renewal and
// refuses a row that does not name the exact original charge receipt, the exact
// accepted offer and the exact renewed window. The period row is append-only,
// so a mismatch is a defect to surface, not a value to repair.
func validateStoredRenewedPeriod(ctx context.Context, tx *sql.Tx, periodID, subscriptionID, billingKey string, accepted *api.QuoteAcceptance, charge *api.WalletOperation, receiptID string, periodStart, periodEnd time.Time) error {
	var storedID, storedSubscription, quoteID, storedBillingKey, chargeOpID, storedReceipt, provenance string
	var snapshot []byte
	var storedStart, storedEnd time.Time
	err := tx.QueryRowContext(ctx, `SELECT id, subscription_id, quote_id, accepted_quote_snapshot, period_start, period_end,
		billing_key, charge_wallet_operation_id, charge_receipt_id, provenance
		FROM workspace.subscription_periods WHERE id=$1`, periodID).
		Scan(&storedID, &storedSubscription, &quoteID, &snapshot, &storedStart, &storedEnd, &storedBillingKey, &chargeOpID, &storedReceipt, &provenance)
	if err != nil {
		return dbError(err)
	}
	readback := &api.QuoteAcceptance{}
	if protojson.Unmarshal(snapshot, readback) != nil || !proto.Equal(readback, accepted) {
		return status.Error(codes.DataLoss, "the stored renewed period is not this renewal's accepted quote")
	}
	if storedID != periodID || storedSubscription != subscriptionID || quoteID != accepted.GetQuote().GetId() || storedBillingKey != billingKey ||
		chargeOpID != charge.GetId() || storedReceipt != receiptID || provenance != "quoted" ||
		!storedStart.UTC().Equal(periodStart.UTC()) || !storedEnd.UTC().Equal(periodEnd.UTC()) {
		return status.Error(codes.DataLoss, "the stored renewed period differs from this renewal's confirmed charge receipt")
	}
	return nil
}

// renewalEvidence is the commit evidence of one accepted renewal. A renewal is
// its own accepted obligation: it continues one Workspace's subscription and it
// names the renewal action, so the grant it asks for carries exactly the
// renewal surface.
func renewalEvidence(op ownerstore.Operation) (*api.OwnerCommitEvidence, error) {
	a, _, err := decodeOrder(op)
	if err != nil {
		return nil, err
	}
	return &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: op.ID, ResourceId: op.ResourceID, AcceptedInputDigest: a.InputDigest, CommittedVersion: 1, AcceptedAt: timestamppb.New(op.CreatedAt), AuthorizationContextId: a.AuthorizationContextID, ActorId: op.ActorID, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: op.TenantID}}}, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE, AuthorizationResource: workspaceResource(op.ResourceID), ContinuationResources: []*api.AuthorizationResource{workspaceResource(op.ResourceID)}}, nil
}

// renewalOperation projects one owner-local renew_workspace row as the typed
// Operation the caller polls.
func renewalOperation(o ownerstore.Operation) (*api.Operation, error) {
	stage, ok := api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(o.Stage)]
	if !ok || stage == 0 {
		return nil, status.Error(codes.Internal, "stored Workspace renewal stage is invalid")
	}
	state, ok := api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(o.Status)]
	if !ok || state == 0 {
		return nil, status.Error(codes.Internal, "stored Workspace renewal status is invalid")
	}
	out := &api.Operation{OperationId: o.ID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RENEW_WORKSPACE, ResourceId: o.ResourceID, Status: api.OperationStatusEnum(state), Stage: api.OperationStageEnum(stage), RequestId: o.RequestID, CreatedAt: timestamppb.New(o.CreatedAt), UpdatedAt: timestamppb.New(o.UpdatedAt)}
	if o.Observation != "" {
		observation, ok := api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(o.Observation)]
		if !ok || observation == 0 {
			return nil, status.Error(codes.Internal, "stored Workspace renewal observation is invalid")
		}
		v := api.OperationObservationResultEnum(observation)
		out.ObservationResult = &v
	}
	if o.ErrorCode != "" {
		number, ok := api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(o.ErrorCode)]
		if !ok || number == 0 {
			return nil, status.Error(codes.Internal, "stored Workspace renewal error is invalid")
		}
		code := api.ErrorCodeEnum(number)
		out.ErrorCode = &code
	}
	if !o.Terminal() {
		out.PollAfterSeconds = proto.Int32(5)
	}
	return out, nil
}

// failedRenewal records why one renewal stopped. A refusal by the owner's own
// facts, or by an owner that answered, is a rejected renewal. A transport loss
// is not: it leaves the renewal pending, because a lost answer never proves that
// the customer was not charged.
func (s *Service) failedRenewal(ctx context.Context, op ownerstore.Operation, token, step, stage string, result orderResult, cause error) error {
	state, observation, code := renewalFailure(step, cause)
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.checkpoint(writeCtx, op, token, step, stage, state, observation, "", code, result); err != nil {
		return err
	}
	return cause
}

// renewalFailure is the outcome vocabulary of one renewal step. Each refusal
// has its own code, so a rejected renewal is never read as a lost dependency and
// never as an ended paid term.
func renewalFailure(step string, cause error) (state, observation, code string) {
	switch status.Code(cause) {
	case codes.FailedPrecondition:
		switch step {
		case "read_resources":
			return "failed", "rejected", "WORKSPACE_NOT_READY"
		case "renew_resources":
			return "failed", "rejected", "OWNER_CAPABILITY_UNAVAILABLE"
		case "accept_quote":
			return "failed", "rejected", "QUOTE_MISMATCH"
		}
		return "failed", "rejected", "RENEWAL_PERIOD_ELAPSED"
	case codes.AlreadyExists:
		return "failed", "rejected", "VERSION_CONFLICT"
	case codes.InvalidArgument, codes.NotFound:
		return "failed", "rejected", "QUOTE_MISMATCH"
	}
	return callFailure(step, cause)
}
