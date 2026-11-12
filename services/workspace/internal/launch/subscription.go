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
	"opl-cloud/services/internal/ownerservice"
)

// GetSubscription reads the Workspace owner's accepted subscription. Catalog's
// current offers cannot establish the price or policies of an existing order.
func (s *Service) GetSubscription(ctx context.Context, r *api.GetSubscriptionRpcRequest) (*api.Subscription, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if workspaceID == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace id is required")
	}
	if s.Store == nil {
		return nil, status.Error(codes.Unavailable, "Workspace store unavailable")
	}
	// Authorize against the persisted tenant before reading financial facts,
	// including the absence of a subscription. CloudIdentity owns the role rule.
	var tenant string
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT tenant_id FROM workspace.workspaces WHERE id=$1`, workspaceID).Scan(&tenant); err != nil {
		return nil, dbError(err)
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION, workspaceResource(workspaceID), tenant); err != nil {
		return nil, err
	}
	out := &api.Subscription{WorkspaceId: workspaceID}
	var start, end, created time.Time
	var state, provenance, renewalMode string
	var quoteID, legacyID, changeID, consentID, priceID sql.NullString
	var monthly sql.NullInt64
	var quoted, legacy []byte
	err := s.Store.DB().QueryRowContext(ctx, `SELECT s.id,s.current_period_start,s.current_period_end,s.period_months,
        s.created_at,s.provenance,s.renewal_mode,s.renewal_settings_version,s.version,
        s.accepted_quote_id,s.legacy_purchase_id,s.active_change_operation_id,s.renewal_consent_id,
        s.current_price_policy_version_id,s.current_monthly_usd_micros,
        s.accepted_quote_snapshot,s.legacy_obligation_snapshot,w.status
        FROM workspace.subscriptions s JOIN workspace.workspaces w ON w.id=s.workspace_id
        WHERE s.workspace_id=$1 AND w.tenant_id=$2`, workspaceID, tenant).Scan(
		&out.Id, &start, &end, &out.PeriodMonths, &created, &provenance, &renewalMode, &out.RenewalSettingsVersion, &out.Version,
		&quoteID, &legacyID, &changeID, &consentID, &priceID, &monthly, &quoted, &legacy, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "the Workspace has no subscription")
	}
	if err != nil {
		return nil, dbError(err)
	}
	out.CurrentPeriodStart, out.CurrentPeriodEnd, out.CreatedAt = timestamppb.New(start), timestamppb.New(end), timestamppb.New(created)
	invalid := func() (*api.Subscription, error) {
		return nil, status.Error(codes.DataLoss, "stored Workspace subscription is invalid")
	}
	if strings.TrimSpace(out.Id) == "" || out.CurrentPeriodStart.CheckValid() != nil || out.CurrentPeriodEnd.CheckValid() != nil ||
		out.CreatedAt.CheckValid() != nil || !end.After(start) || out.PeriodMonths < 1 || out.Version < 0 || out.RenewalSettingsVersion < 0 {
		return invalid()
	}
	for _, value := range []sql.NullString{quoteID, legacyID, changeID, consentID, priceID} {
		if value.Valid && strings.TrimSpace(value.String) == "" {
			return invalid()
		}
	}
	if monthly.Valid && monthly.Int64 < 0 {
		return invalid()
	}
	// Price and policy form one accepted basis. A legacy row missing either
	// cannot quote a complete basis; no zero or current Catalog price is invented.
	if priceID.Valid && monthly.Valid {
		out.CurrentPricePolicyVersionId = proto.String(priceID.String)
		out.CurrentMonthlyUsdMicros = proto.Int64(monthly.Int64)
	}
	if changeID.Valid {
		out.RenewalOperationId = proto.String(changeID.String)
	}
	if consentID.Valid {
		out.RenewalConsentId = proto.String(consentID.String)
	}
	switch renewalMode {
	case "manual":
		out.RenewalMode = api.SubscriptionRenewalModeEnum_SUBSCRIPTION_RENEWAL_MODE_ENUM_MANUAL
	case "automatic":
		if !consentID.Valid {
			return invalid()
		}
		out.RenewalMode = api.SubscriptionRenewalModeEnum_SUBSCRIPTION_RENEWAL_MODE_ENUM_AUTOMATIC
	default:
		return invalid()
	}
	switch provenance {
	case "quoted":
		if !quoteID.Valid || legacyID.Valid || len(legacy) != 0 || !priceID.Valid || !monthly.Valid {
			return invalid()
		}
		accepted := &api.QuoteAcceptance{}
		if protojson.Unmarshal(quoted, accepted) != nil || accepted.GetQuote().GetId() != quoteID.String ||
			(accepted.GetWorkspaceId() != "" && accepted.GetWorkspaceId() != workspaceID) {
			return invalid()
		}
		q := accepted.GetQuote()
		if strings.TrimSpace(q.GetRefundPolicyVersionId()) == "" || strings.TrimSpace(q.GetRetentionPolicyVersionId()) == "" ||
			strings.TrimSpace(q.GetRefundTerms()) == "" || strings.TrimSpace(q.GetRetentionTerms()) == "" {
			return invalid()
		}
		out.Provenance = api.SubscriptionProvenanceEnum_SUBSCRIPTION_PROVENANCE_ENUM_QUOTED
		out.AcceptedQuoteId = proto.String(quoteID.String)
		out.RefundPolicyVersionId, out.RetentionPolicyVersionId = proto.String(q.RefundPolicyVersionId), proto.String(q.RetentionPolicyVersionId)
		out.RefundTerms, out.RetentionTerms = proto.String(q.RefundTerms), proto.String(q.RetentionTerms)
	case "legacy_import":
		if !legacyID.Valid || quoteID.Valid || len(quoted) != 0 {
			return invalid()
		}
		// Legacy evidence is not a fabricated QuoteAcceptance. Decode only the
		// original snapshot's named policy fields; other obligation evidence
		// remains private and absent policy fields remain absent.
		var policy *subscriptionPolicySnapshot
		if json.Unmarshal(legacy, &policy) != nil || policy == nil {
			return invalid()
		}
		for _, value := range []*string{policy.RefundPolicyVersionID, policy.RetentionPolicyVersionID, policy.RefundTerms, policy.RetentionTerms} {
			if value != nil && strings.TrimSpace(*value) == "" {
				return invalid()
			}
		}
		out.Provenance = api.SubscriptionProvenanceEnum_SUBSCRIPTION_PROVENANCE_ENUM_LEGACY_IMPORT
		out.LegacyPurchaseId = proto.String(legacyID.String)
		out.RefundPolicyVersionId, out.RetentionPolicyVersionId = policy.RefundPolicyVersionID, policy.RetentionPolicyVersionID
		out.RefundTerms, out.RetentionTerms = policy.RefundTerms, policy.RetentionTerms
	default:
		return invalid()
	}
	switch {
	case state == "deleted":
		out.Status = api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_TERMINATED
	case !s.lifecycleNow().Before(end):
		out.Status = api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_EXPIRED
	default:
		out.Status = api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_ACTIVE
	}
	return out, nil
}

// These optional fields are the exact legacy policy projection declared by the
// existing Subscription contract, not a second persisted policy model.
type subscriptionPolicySnapshot struct {
	RefundPolicyVersionID    *string `json:"refundPolicyVersionId"`
	RetentionPolicyVersionID *string `json:"retentionPolicyVersionId"`
	RefundTerms              *string `json:"refundTerms"`
	RetentionTerms           *string `json:"retentionTerms"`
}
