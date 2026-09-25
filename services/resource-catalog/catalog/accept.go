package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
)

// AcceptQuote binds one offer to exactly one Workspace operation.
//
// This is the Catalog side of the Workspace-to-Catalog edge: Workspace holds the
// business intent and the accepted snapshot, while this owner is the only writer
// of the quote's own status. The acceptance identity and snapshot digest are
// derived from the accepted, immutable row rather than from a new column, so the
// frozen schema is not changed and a replay returns the identical acceptance.
func (s *Service) AcceptQuote(ctx context.Context, r *api.AcceptQuoteRequest) (*api.QuoteAcceptance, error) {
	// Only the Workspace owner may accept a quote: it is the caller that holds the
	// customer's intent and the accepted snapshot.
	if peer, ok := ownerservice.PeerOwner(ctx); !ok || peer != ownerservice.OwnerWorkspace.Service() {
		return nil, status.Error(codes.Unauthenticated, "the verified Workspace owner is required to accept a quote")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if r.GetQuoteId() == "" || r.GetObligationId() == "" || r.GetWorkspaceId() == "" {
		return nil, status.Error(codes.InvalidArgument, "quoteId, workspaceId and obligationId are required")
	}
	if r.GetPlanChangeId() != "" {
		// A plan change is a resize acceptance. This slice prices the deploy variant
		// only, so accepting a plan-change quote would silently extend the same
		// subscription model to a variant that does not exist yet.
		return nil, status.Error(codes.Unimplemented, "this slice accepts only a deploy quote; a plan-change acceptance depends on a Workspace subscription and paid period that do not exist yet")
	}

	tenantID := r.GetContext().GetScope().GetTenant().GetTenantId()
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "a tenant-scoped acceptance is required")
	}

	accepted := &api.QuoteAcceptance{}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()

	var (
		storedState        string
		acceptedBy         sql.NullString
		acceptedAt         sql.NullTime
		expiresAt, created time.Time
	)
	// The row is locked so two concurrent acceptances cannot both observe an
	// unaccepted offer.
	err = tx.QueryRowContext(ctx, `SELECT status,accepted_by_operation_id,accepted_at,expires_at,created_at FROM resource_catalog.quotes WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, r.GetQuoteId(), tenantID).
		Scan(&storedState, &acceptedBy, &acceptedAt, &expiresAt, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "no such quote for this tenant")
	}
	if err != nil {
		return nil, dbError(err)
	}

	switch storedState {
	case "accepted":
		if acceptedBy.String != r.GetObligationId() {
			// One quote binds exactly one operation. A second obligation must not
			// inherit an acceptance it did not make, and must not create a second one.
			return nil, status.Error(codes.AlreadyExists, "this quote is already bound to another acceptance")
		}
	case "offered":
		if !time.Now().UTC().Before(expiresAt) {
			return nil, status.Error(codes.FailedPrecondition, "this quote has expired; a new quote is required")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE resource_catalog.quotes SET status='accepted',accepted_by_operation_id=$1,accepted_at=now() WHERE id=$2 AND status='offered'`, r.GetObligationId(), r.GetQuoteId()); err != nil {
			return nil, dbError(err)
		}
	default:
		return nil, status.Errorf(codes.FailedPrecondition, "a quote in state %q cannot be accepted", storedState)
	}

	quote, err := s.readQuoteTx(ctx, tx, r.GetQuoteId(), tenantID)
	if err != nil {
		return nil, err
	}
	quote.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED
	accepted.Quote = quote
	accepted.ObligationId = r.GetObligationId()
	accepted.AcceptanceId = acceptanceID(r.GetQuoteId())
	snapshot, err := acceptedSnapshotDigest(quote)
	if err != nil {
		return nil, err
	}
	accepted.SnapshotDigest = snapshot
	if err := tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return accepted, nil
}

// acceptanceID is derived from the quote so a replay of the same acceptance
// returns the same identity without storing one.
func acceptanceID(quoteID string) string { return "accept_" + quoteID }

// acceptedSnapshotDigest is the digest of the accepted offer as the contract's
// canonical public shape: the quote with its line items. It is recomputed from the
// accepted, immutable row, and the row's status is fixed to accepted so the digest
// does not depend on when the caller asks.
func acceptedSnapshotDigest(quote *api.Quote) (string, error) {
	stable := proto.Clone(quote).(*api.Quote)
	stable.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED
	// Deterministic protobuf marshalling gives a stable byte image for the same
	// message, which is what makes the digest a reproducible identity rather than a
	// formatting artifact.
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(stable)
	if err != nil {
		return "", status.Error(codes.Internal, "encode the accepted quote snapshot")
	}
	return digest(raw), nil
}

// readQuoteTx reads one quote and its lines inside the acceptance transaction.
func (s *Service) readQuoteTx(ctx context.Context, tx *sql.Tx, quoteID, tenantID string) (*api.Quote, error) {
	var (
		quote                                             api.Quote
		purpose, storedState                              string
		workspaceID, capabilityVersionID, scheduledChange sql.NullString
		models, admission                                 []byte
		periodStart, periodEnd, expiresAt, createdAt      time.Time
		sourceSubscriptionVersion                         sql.NullInt64
		planChange                                        []byte
	)
	err := tx.QueryRowContext(ctx, `SELECT id,purpose,workspace_id,capability_version_id,compute_plan_id,storage_plan_id,model_selections,period_months,period_start,period_end,price_policy_version_id,refund_policy_version_id,retention_policy_version_id,refund_terms,retention_terms,expected_interruption,total_usd_micros,status,admission_snapshot,expires_at,created_at,source_subscription_version,plan_change_calculation,scheduled_plan_change_id FROM resource_catalog.quotes WHERE id=$1 AND tenant_id=$2`, quoteID, tenantID).
		Scan(&quote.Id, &purpose, &workspaceID, &capabilityVersionID, &quote.ComputePlanId, &quote.StoragePlanId, &models, &quote.PeriodMonths, &periodStart, &periodEnd, &quote.PricePolicyVersionId, &quote.RefundPolicyVersionId, &quote.RetentionPolicyVersionId, &quote.RefundTerms, &quote.RetentionTerms, &quote.ExpectedInterruption, &quote.TotalUsdMicros, &storedState, &admission, &expiresAt, &createdAt, &sourceSubscriptionVersion, &planChange, &scheduledChange)
	if err != nil {
		return nil, dbError(err)
	}
	quote.Purpose = api.QuotePurposeEnum(api.QuotePurposeEnum_value["QUOTE_PURPOSE_ENUM_"+upper(purpose)])
	quote.Status = api.QuoteStatusEnum(api.QuoteStatusEnum_value["QUOTE_STATUS_ENUM_"+upper(storedState)])
	quote.PeriodStart = timestamppb.New(periodStart.UTC())
	quote.PeriodEnd = timestamppb.New(periodEnd.UTC())
	quote.ExpiresAt = timestamppb.New(expiresAt.UTC())
	quote.CreatedAt = timestamppb.New(createdAt.UTC())
	if workspaceID.Valid {
		quote.WorkspaceId = proto.String(workspaceID.String)
	}
	if capabilityVersionID.Valid {
		quote.CapabilityVersionId = proto.String(capabilityVersionID.String)
	}
	if scheduledChange.Valid {
		quote.ScheduledPlanChangeId = proto.String(scheduledChange.String)
	}
	var selections []map[string]string
	if err := json.Unmarshal(models, &selections); err != nil {
		return nil, status.Error(codes.DataLoss, "the stored model selections are not readable")
	}
	for _, selection := range selections {
		quote.ModelSelections = append(quote.ModelSelections, &api.ModelSelection{Slot: selection["slot"], ModelId: selection["modelId"]})
	}
	var snapshot quoteSnapshot
	if err := json.Unmarshal(admission, &snapshot); err != nil {
		return nil, status.Error(codes.DataLoss, "the stored quote admission snapshot is not readable")
	}
	quote.RuntimeReadbackRequirement = runtimeReadbackRequirementFor(snapshot, quote.GetCapabilityVersionId())
	rows, err := tx.QueryContext(ctx, `SELECT kind,description,quantity,amount_usd_micros FROM resource_catalog.quote_items WHERE quote_id=$1 ORDER BY sort_order`, quoteID)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			line api.QuoteLine
			kind string
		)
		if err := rows.Scan(&kind, &line.Description, &line.Quantity, &line.AmountUsdMicros); err != nil {
			return nil, dbError(err)
		}
		line.Kind = api.QuoteLineKindEnum(api.QuoteLineKindEnum_value["QUOTE_LINE_KIND_ENUM_"+upper(kind)])
		quote.LineItems = append(quote.LineItems, &line)
	}
	return &quote, dbError(rows.Err())
}
