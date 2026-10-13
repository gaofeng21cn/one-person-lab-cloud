package launch

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// ReadWorkspaceAccess computes entitlement only from this owner's confirmed
// subscription. Readiness, an accepted quote or a pending renewal is not a paid
// term. Local no-charge activation has no confirmed term yet and is unverified.
func (s *Service) ReadWorkspaceAccess(ctx context.Context, r *api.ReadWorkspaceAccessRequest) (*api.WorkspaceAccessDecision, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || peer != owneridentity.Serve.Service() {
		return nil, status.Error(codes.PermissionDenied, "Serve peer required")
	}
	if strings.TrimSpace(r.GetWorkspaceId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "Workspace identity required")
	}
	if s.Store == nil {
		return nil, status.Error(codes.Unavailable, "Workspace store unavailable")
	}
	var state string
	var start, end sql.NullTime
	var confirmed bool
	err := s.Store.DB().QueryRowContext(ctx, `SELECT w.status,s.current_period_start,s.current_period_end,
		EXISTS(SELECT 1 FROM workspace.subscription_periods p WHERE p.subscription_id=s.id
		AND p.period_start=s.current_period_start AND p.period_end=s.current_period_end
		AND p.charge_wallet_operation_id=s.last_charge_wallet_operation_id
		AND COALESCE(p.charge_receipt_id,'')<>'')
		FROM workspace.workspaces w LEFT JOIN workspace.subscriptions s ON s.workspace_id=w.id
		WHERE w.id=$1`, r.WorkspaceId).Scan(&state, &start, &end, &confirmed)
	decision := &api.WorkspaceAccessDecision{WorkspaceId: r.WorkspaceId, Verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED}
	if errors.Is(err, sql.ErrNoRows) {
		return decision, nil
	}
	if err != nil {
		return nil, dbError(err)
	}
	if state != "active" {
		return decision, nil
	}
	if !start.Valid || !end.Valid || !end.Time.After(start.Time) || !confirmed {
		decision.Verdict = api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_UNVERIFIED
		return decision, nil
	}
	now := time.Now().UTC()
	if s.accessNow != nil {
		now = s.accessNow().UTC()
	}
	if !now.Before(start.Time) && now.Before(end.Time) {
		decision.Verdict = api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED
	}
	return decision, nil
}
