package identity

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func (s *Service) authorizeWorkspaceTransactionRead(ctx context.Context, call *api.CallContext, workspaceID string, action api.AuthorizationActionEnum) (string, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return "", err
	}
	if call == nil || call.GetSessionId() == "" || call.GetAcceptedOperationGrantId() != "" {
		return "", denied()
	}
	session, _, _, err := s.session(ctx, call.GetSessionId())
	if err != nil {
		return "", err
	}
	// This is a team read, not a platform-admin or personal-wallet read. Bind
	// the database predicate to the live session's Tenant, never browser input.
	tenantID := session.GetTenantId()
	if tenantID == "" || session.GetActorId() != call.GetActorId() || !scopeMatchesSession(session, call.GetScope()) {
		return "", denied()
	}
	if strings.TrimSpace(workspaceID) == "" || workspaceID != strings.TrimSpace(workspaceID) {
		return "", status.Error(codes.InvalidArgument, "workspace id required")
	}
	request := &api.AuthorizationRequest{
		Scope: call.GetScope(), ActorId: call.GetActorId(), SessionId: call.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY,
		Action:        action,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		RequestId:     call.GetRequestId(),
	}
	if call.GetAuthorizationContextId() != "" {
		request.AuthorizationContextId = proto.String(call.GetAuthorizationContextId())
	}
	if err = s.authorizeInternal(ctx, owneridentity.Gateway.Service(), request); err != nil {
		return "", err
	}
	return tenantID, nil
}

// ListWorkspaceTransactions reads the Gateway's recorded operations. It never
// reconciles unknown outcomes or requires an active Workspace/wallet binding:
// retiring either must not hide the team's historical charges and refunds.
func (s *Service) ListWorkspaceTransactions(ctx context.Context, r *api.ListWorkspaceTransactionsRpcRequest) (*api.WalletOperationPage, error) {
	workspaceID := r.GetWorkspaceId()
	tenantID, err := s.authorizeWorkspaceTransactionRead(ctx, r.GetContext(), workspaceID, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACETRANSACTIONS)
	if err != nil {
		return nil, err
	}
	after, afterID, ok := parseCursor(r.GetQueryCursor())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid cursor")
	}
	limit := 25
	if r.QueryLimit != nil {
		if r.GetQueryLimit() < 1 || r.GetQueryLimit() > 100 {
			return nil, status.Error(codes.InvalidArgument, "limit must be between 1 and 100")
		}
		limit = int(r.GetQueryLimit())
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	rows, err := s.GatewayStore.db.QueryContext(ctx, walletSelect+`
		WHERE tenant_id=$1 AND workspace_id=$2
		AND ($3::timestamptz IS NULL OR (created_at,id)<($3,$4))
		ORDER BY created_at DESC,id DESC LIMIT $5`, tenantID, workspaceID, sql.NullTime{Time: after, Valid: afterID != ""}, afterID, limit+1)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	defer rows.Close()
	page := &api.WalletOperationPage{}
	for rows.Next() {
		record, err := s.GatewayStore.scanWallet(ctx, rows)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
		}
		page.Items = append(page.Items, WalletOperationMessage(record))
	}
	if err = rows.Err(); err != nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = proto.String(cursorOf(last.CreatedAt.AsTime(), last.GetId()))
	}
	return page, nil
}

// GetWorkspaceTransactionTrend aggregates the owner's original financial facts,
// independently of list pagination. Confirmation time determines settled days;
// requested/unknown records are separately reported by their creation window.
func (s *Service) GetWorkspaceTransactionTrend(ctx context.Context, r *api.GetWorkspaceTransactionTrendRpcRequest) (*api.WorkspaceTransactionTrend, error) {
	tenantID, err := s.authorizeWorkspaceTransactionRead(ctx, r.GetContext(), r.GetWorkspaceId(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACETRANSACTIONTREND)
	if err != nil {
		return nil, err
	}
	if s.GatewayStore == nil {
		return nil, status.Error(codes.Unavailable, "Gateway persistence unavailable")
	}
	end := time.Now().UTC()
	start := end.Truncate(24*time.Hour).AddDate(0, 0, -13)
	result := &api.WorkspaceTransactionTrend{WorkspaceId: r.GetWorkspaceId(), PeriodStart: timestamppb.New(start), PeriodEnd: timestamppb.New(end)}
	days := make(map[string]*api.WorkspaceTransactionTrendDay, 14)
	for i := 0; i < 14; i++ {
		day := &api.WorkspaceTransactionTrendDay{Date: start.AddDate(0, 0, i).Format("2006-01-02")}
		days[day.Date] = day
		result.Days = append(result.Days, day)
	}
	rows, err := s.GatewayStore.db.QueryContext(ctx, `
		WITH refund_totals AS (
			SELECT r.original_wallet_operation_id, sum(r.amount_usd_micros)::bigint AS total_refund_usd_micros
			FROM gateway.wallet_operations r
			JOIN gateway.wallet_operations original ON original.id=r.original_wallet_operation_id
			WHERE r.kind='refund' AND r.status IN ('requested','unknown','confirmed')
				AND r.tenant_id=original.tenant_id AND r.workspace_id=original.workspace_id
			GROUP BY r.original_wallet_operation_id
		), normalized AS (
			SELECT w.*,
				CASE
					WHEN w.status='confirmed' AND w.kind='refund'
						AND original.kind='charge' AND original.status='confirmed'
						AND original.tenant_id=w.tenant_id AND original.workspace_id=w.workspace_id
						AND COALESCE(refund_totals.total_refund_usd_micros, 0) <= original.amount_usd_micros
					THEN 'confirmed'
					WHEN w.status='confirmed' AND w.kind='refund' THEN 'unknown'
					ELSE w.status
				END AS effective_status,
				CASE
					WHEN w.status='confirmed' AND w.kind='refund'
						AND original.kind='charge' AND original.status='confirmed'
						AND original.tenant_id=w.tenant_id AND original.workspace_id=w.workspace_id
						AND COALESCE(refund_totals.total_refund_usd_micros, 0) <= original.amount_usd_micros
					THEN w.confirmed_at
					ELSE w.created_at
					END AS effective_at
			FROM gateway.wallet_operations w
			LEFT JOIN gateway.wallet_operations original ON original.id=w.original_wallet_operation_id
			LEFT JOIN refund_totals ON refund_totals.original_wallet_operation_id=w.original_wallet_operation_id
			WHERE w.tenant_id=$1 AND w.workspace_id=$2 AND w.kind IN ('charge','refund')
			AND w.status IN ('confirmed','unknown','requested')
		)
		SELECT (effective_at AT TIME ZONE 'UTC')::date::text, effective_status, kind, sum(amount_usd_micros)::bigint
		FROM normalized
		WHERE effective_at>=$3 AND effective_at<$4
		GROUP BY 1,2,3`, tenantID, r.GetWorkspaceId(), start, end)
	if err != nil {
		return nil, transactionTrendError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var date, state, kind string
		var amount int64
		if err := rows.Scan(&date, &state, &kind, &amount); err != nil {
			return nil, transactionTrendError(err)
		}
		day := days[date]
		if day == nil || amount < 0 {
			return nil, status.Error(codes.Unavailable, "invalid Gateway transaction aggregate")
		}
		var total *int64
		switch {
		case state == "confirmed" && kind == "charge":
			total = &day.ConfirmedChargeUsdMicros
		case state == "confirmed" && kind == "refund":
			total = &day.ConfirmedRefundUsdMicros
		case state == "unknown" && kind == "charge":
			total = &result.UnknownChargeUsdMicros
		case state == "unknown" && kind == "refund":
			total = &result.UnknownRefundUsdMicros
		case state == "requested" && kind == "charge":
			total = &result.RequestedChargeUsdMicros
		case state == "requested" && kind == "refund":
			total = &result.RequestedRefundUsdMicros
		default:
			return nil, status.Error(codes.Unavailable, "invalid Gateway transaction aggregate")
		}
		if amount > math.MaxInt64-*total {
			return nil, status.Error(codes.FailedPrecondition, "transaction aggregate exceeds int64 range")
		}
		*total += amount
	}
	if err := rows.Err(); err != nil {
		return nil, transactionTrendError(err)
	}
	return result, nil
}

func transactionTrendError(err error) error {
	var databaseError *pq.Error
	if errors.As(err, &databaseError) && databaseError.Code == "22003" {
		return status.Error(codes.FailedPrecondition, "transaction aggregate exceeds int64 range")
	}
	return status.Error(codes.Unavailable, "Gateway transaction aggregate unavailable")
}
