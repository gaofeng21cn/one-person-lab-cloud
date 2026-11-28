package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const (
	maxPurchaseEligibilityReasonRunes = 200
	maxPurchaseEligibilityBatchSize   = 100
)

func (s *Service) readActorPurchaseEligibility(ctx context.Context, actorID string) (bool, error) {
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `SELECT workspace_purchase_enabled FROM tenant.workspace_purchase_eligibility WHERE actor_id=$1`, actorID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return s.isPlatformAdmin(ctx, actorID)
	}
	if err != nil {
		return false, persistence(err)
	}
	return enabled, nil
}

// ReadWorkspacePurchaseEligibility answers Workspace's new-purchase admission
// check directly from the canonical CloudIdentity owner. The live session is
// the only identity selector; an inactive subject or missing policy row for a
// non-admin fails closed.
func (s *Service) ReadWorkspacePurchaseEligibility(ctx context.Context, r *api.ReadWorkspacePurchaseEligibilityRequest) (*api.WorkspacePurchaseEligibilityReadback, error) {
	if err := peer(ctx, owneridentity.Workspace.Service()); err != nil {
		return nil, err
	}
	call := r.GetContext()
	if call == nil || strings.TrimSpace(call.GetActorId()) == "" || strings.TrimSpace(call.GetSessionId()) == "" {
		return nil, status.Error(codes.Unauthenticated, "a live purchase session is required")
	}
	session, _, _, err := s.session(ctx, call.GetSessionId())
	if err != nil {
		return nil, err
	}
	if session.GetActorId() != call.GetActorId() {
		return nil, denied()
	}
	enabled, err := s.readActorPurchaseEligibility(ctx, session.GetActorId())
	if err != nil {
		return nil, err
	}
	return &api.WorkspacePurchaseEligibilityReadback{
		ActorId:                  session.GetActorId(),
		AccountId:                session.GetActorId(),
		WorkspacePurchaseEnabled: enabled,
	}, nil
}

// SetWorkspacePurchaseEligibility is the sole canonical writer for Workspace
// new-purchase eligibility. Only a platform administrator through Console BFF
// may change it; the policy row, audit event and idempotency record commit
// atomically in one opl_tenant transaction.
func (s *Service) SetWorkspacePurchaseEligibility(ctx context.Context, r *api.SetWorkspacePurchaseEligibilityRpcRequest) (*api.WorkspacePurchaseEligibilityCommand, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	targetActorID := strings.TrimSpace(r.GetTargetActorId())
	parsedID, parseErr := strconv.ParseInt(targetActorID, 10, 64)
	if parseErr != nil || parsedID <= 0 || targetActorID != r.GetTargetActorId() || strconv.FormatInt(parsedID, 10) != targetActorID {
		return nil, status.Error(codes.InvalidArgument, "a valid Gateway subject id is required")
	}
	body := r.GetBody()
	if body == nil || strings.TrimSpace(body.GetConfirmationAccountId()) != targetActorID {
		return nil, status.Error(codes.InvalidArgument, "confirmationAccountId must match the target account")
	}
	reason := strings.TrimSpace(body.GetReason())
	if reason == "" || utf8.RuneCountInString(reason) > maxPurchaseEligibilityReasonRunes {
		return nil, status.Error(codes.InvalidArgument, "reason must be between 1 and 200 characters")
	}
	authReq := &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, Id: &targetActorID},
		RequestId:     c.GetRequestId(),
	}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		authReq.AuthorizationContextId = &id
	}
	decision, err := s.AuthorizeAction(ctx, authReq)
	if err != nil {
		return nil, err
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	if s.admins[targetActorID] {
		return nil, status.Error(codes.PermissionDenied, "platform administrator purchase eligibility is reserved")
	}
	if _, err := s.Gateway.directoryIdentity(ctx, targetActorID); err != nil {
		return nil, err
	}
	enabled := body.GetEnabled()
	canonicalReq := &api.SetWorkspacePurchaseEligibilityRpcRequest{
		TargetActorId: targetActorID,
		Body: &api.SetWorkspacePurchaseEligibilityRequest{
			ConfirmationAccountId: targetActorID,
			Enabled:               enabled,
			Reason:                reason,
		},
	}
	out := &api.WorkspacePurchaseEligibilityCommand{}
	err = s.command(ctx, c, "platform", "SetWorkspacePurchaseEligibility", canonicalReq, out, func(tx *sql.Tx) error {
		var before bool
		scanErr := tx.QueryRowContext(ctx, `SELECT workspace_purchase_enabled FROM tenant.workspace_purchase_eligibility WHERE actor_id=$1 FOR UPDATE`, targetActorID).Scan(&before)
		if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
			return persistence(scanErr)
		}
		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO tenant.workspace_purchase_eligibility(actor_id,workspace_purchase_enabled,reason,updated_by,updated_at)
			VALUES($1,$2,$3,$4,now())
			ON CONFLICT (actor_id) DO UPDATE
			SET workspace_purchase_enabled=EXCLUDED.workspace_purchase_enabled,
			    reason=EXCLUDED.reason,
			    updated_by=EXCLUDED.updated_by,
			    updated_at=now()`, targetActorID, enabled, reason, c.GetActorId()); execErr != nil {
			return persistence(execErr)
		}
		action := "account.workspace_purchase.revoke"
		if enabled {
			action = "account.workspace_purchase.grant"
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      c.GetActorId(),
			action:       action,
			resourceType: "account",
			resourceID:   targetActorID,
			requestID:    c.GetRequestId(),
			details: map[string]any{
				"before":                   before,
				"workspacePurchaseEnabled": enabled,
				"reason":                   reason,
			},
			outcome: "succeeded",
		}); auditErr != nil {
			return auditErr
		}
		proto.Merge(out, &api.WorkspacePurchaseEligibilityCommand{
			OperationId:              "workspace-purchase-eligibility-" + hash(c.GetIdempotencyKey()+":"+targetActorID)[:18],
			AccountId:                targetActorID,
			Status:                   "succeeded",
			WorkspacePurchaseEnabled: enabled,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListWorkspacePurchaseEligibility returns authoritative purchase eligibility
// for a bounded page of Gateway subjects to support operator account projection.
func (s *Service) ListWorkspacePurchaseEligibility(ctx context.Context, r *api.ListWorkspacePurchaseEligibilityRpcRequest) (*api.WorkspacePurchaseEligibilityList, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	authReq := &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTTENANTS,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT},
		RequestId:     c.GetRequestId(),
	}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		authReq.AuthorizationContextId = &id
	}
	decision, err := s.AuthorizeAction(ctx, authReq)
	if err != nil {
		return nil, err
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	rawIDs := r.GetActorIds()
	if len(rawIDs) > maxPurchaseEligibilityBatchSize {
		return nil, status.Error(codes.InvalidArgument, "too many actor ids requested")
	}
	if len(rawIDs) == 0 {
		return &api.WorkspacePurchaseEligibilityList{}, nil
	}
	seen := make(map[string]bool, len(rawIDs))
	unique := make([]string, 0, len(rawIDs))
	args := make([]any, 0, len(rawIDs))
	placeholders := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id := strings.TrimSpace(raw)
		if id == "" || id != raw {
			return nil, status.Error(codes.InvalidArgument, "actor ids must be non-empty trimmed identifiers")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT actor_id,workspace_purchase_enabled FROM tenant.workspace_purchase_eligibility WHERE actor_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	stored := make(map[string]bool, len(unique))
	for rows.Next() {
		var actorID string
		var enabled bool
		if scanErr := rows.Scan(&actorID, &enabled); scanErr != nil {
			return nil, persistence(scanErr)
		}
		stored[actorID] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}
	adminStates, err := s.platformAdminStates(ctx, unique)
	if err != nil {
		return nil, err
	}
	items := make([]*api.WorkspacePurchaseEligibilityReadback, 0, len(unique))
	for _, actorID := range unique {
		enabled, found := stored[actorID]
		if !found {
			enabled = adminStates[actorID]
		}
		items = append(items, &api.WorkspacePurchaseEligibilityReadback{
			ActorId:                  actorID,
			AccountId:                actorID,
			WorkspacePurchaseEnabled: enabled,
		})
	}
	return &api.WorkspacePurchaseEligibilityList{Items: items}, nil
}
