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
	maxOperatorMutationReasonRunes = 200
)

func (s *Service) isBootstrapAdmin(actorID string) bool {
	return s.admins[actorID]
}

// isPlatformAdmin resolves platform administrator privilege as the union of
// immutable deployment bootstrap roots and active tenant.platform_admins rows.
func (s *Service) isPlatformAdmin(ctx context.Context, actorID string) (bool, error) {
	if s.isBootstrapAdmin(actorID) {
		return true, nil
	}
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `SELECT enabled FROM tenant.platform_admins WHERE actor_id=$1`, actorID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, persistence(err)
	}
	return enabled, nil
}

// platformAdminStates resolves effective platform administrator status for a
// bounded batch of actors using bootstrap roots unioned with tenant.platform_admins.
func (s *Service) platformAdminStates(ctx context.Context, actorIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(actorIDs))
	queryIDs := make([]string, 0, len(actorIDs))
	placeholders := make([]string, 0, len(actorIDs))
	args := make([]any, 0, len(actorIDs))
	for _, actorID := range actorIDs {
		if s.isBootstrapAdmin(actorID) {
			out[actorID] = true
			continue
		}
		queryIDs = append(queryIDs, actorID)
		args = append(args, actorID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	if len(queryIDs) == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT actor_id,enabled FROM tenant.platform_admins WHERE actor_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	for rows.Next() {
		var actorID string
		var enabled bool
		if scanErr := rows.Scan(&actorID, &enabled); scanErr != nil {
			return nil, persistence(scanErr)
		}
		out[actorID] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}
	return out, nil
}

// validatePositiveActorID enforces canonical positive numeric Gateway subject IDs.
func validatePositiveActorID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	parsedID, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsedID <= 0 || trimmed != raw || strconv.FormatInt(parsedID, 10) != trimmed {
		return "", status.Error(codes.InvalidArgument, "a valid Gateway subject id is required")
	}
	return trimmed, nil
}

// requirePlatformAdminCall validates BFF peer identity, platform CallContext, and
// CloudIdentity AuthorizeAction decision for a platform-scoped governance call.
func (s *Service) requirePlatformAdminCall(
	ctx context.Context,
	c *api.CallContext,
	audience api.OwnerEnum,
	action api.AuthorizationActionEnum,
	kind api.AuthorizationResourceKind,
	resourceID string,
) error {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return err
	}
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return denied()
	}
	resource := &api.AuthorizationResource{Kind: kind}
	if resourceID != "" {
		id := resourceID
		resource.Id = &id
	}
	authReq := &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: audience,
		Action:        action,
		Resource:      resource,
		RequestId:     c.GetRequestId(),
	}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		authReq.AuthorizationContextId = &id
	}
	decision, err := s.AuthorizeAction(ctx, authReq)
	if err != nil {
		return err
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	return nil
}

// SetPlatformAdmin grants or revokes delegated platform administrator access in
// tenant.platform_admins. Deployment bootstrap roots are immutable, self-revocation
// is rejected, and revoking the last effective administrator is blocked atomically.
func (s *Service) SetPlatformAdmin(ctx context.Context, r *api.SetPlatformAdminRpcRequest) (*api.PlatformAdminCommand, error) {
	c := r.GetContext()
	targetActorID, err := validatePositiveActorID(r.GetTargetActorId())
	if err != nil {
		if peerErr := peer(ctx, owneridentity.ConsoleBFF); peerErr != nil {
			return nil, peerErr
		}
		return nil, err
	}
	if err := s.requirePlatformAdminCall(
		ctx,
		c,
		api.OwnerEnum_OWNER_ENUM_TENANT,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPLATFORMADMIN,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		targetActorID,
	); err != nil {
		return nil, err
	}
	body := r.GetBody()
	if body == nil || strings.TrimSpace(body.GetConfirmationAccountId()) != targetActorID {
		return nil, status.Error(codes.InvalidArgument, "confirmationAccountId must match the target account")
	}
	reason := strings.TrimSpace(body.GetReason())
	if reason == "" || utf8.RuneCountInString(reason) > maxOperatorMutationReasonRunes {
		return nil, status.Error(codes.InvalidArgument, "reason must be between 1 and 200 characters")
	}
	if s.isBootstrapAdmin(targetActorID) {
		return nil, status.Error(codes.PermissionDenied, "bootstrap platform administrator cannot be modified")
	}
	enabled := body.GetEnabled()
	if !enabled && targetActorID == c.GetActorId() {
		return nil, status.Error(codes.FailedPrecondition, "cannot revoke your own platform administrator access")
	}
	if _, err := s.Gateway.directoryIdentity(ctx, targetActorID); err != nil {
		return nil, err
	}
	canonicalReq := &api.SetPlatformAdminRpcRequest{
		TargetActorId: targetActorID,
		Body: &api.SetPlatformAdminRequest{
			ConfirmationAccountId: targetActorID,
			Enabled:               enabled,
			Reason:                reason,
		},
	}
	out := &api.PlatformAdminCommand{}
	err = s.command(ctx, c, "platform", "SetPlatformAdmin", canonicalReq, out, func(tx *sql.Tx) error {
		if _, lockErr := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tenant:platform_admins',0))`); lockErr != nil {
			return persistence(lockErr)
		}
		if !s.isBootstrapAdmin(c.GetActorId()) {
			var callerEnabled bool
			callerErr := tx.QueryRowContext(ctx, `SELECT enabled FROM tenant.platform_admins WHERE actor_id=$1`, c.GetActorId()).Scan(&callerEnabled)
			if errors.Is(callerErr, sql.ErrNoRows) || (callerErr == nil && !callerEnabled) {
				if auditErr := s.recordAudit(ctx, tx, auditEvent{
					actorID:      c.GetActorId(),
					action:       "account.platform_admin.revoke",
					resourceType: "account",
					resourceID:   targetActorID,
					requestID:    c.GetRequestId(),
					details: map[string]any{
						"enabled": enabled,
						"reason":  reason,
						"refusal": "caller_no_longer_platform_admin",
					},
					outcome: "rejected",
				}); auditErr != nil {
					return auditErr
				}
				return rejected(status.Error(codes.PermissionDenied, "platform administrator authorization revoked concurrently"))
			}
			if callerErr != nil {
				return persistence(callerErr)
			}
		}
		var before bool
		scanErr := tx.QueryRowContext(ctx, `SELECT enabled FROM tenant.platform_admins WHERE actor_id=$1 FOR UPDATE`, targetActorID).Scan(&before)
		if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
			return persistence(scanErr)
		}
		if !enabled {
			var otherDelegated int
			if countErr := tx.QueryRowContext(ctx, `SELECT count(*) FROM tenant.platform_admins WHERE enabled=true AND actor_id<>$1`, targetActorID).Scan(&otherDelegated); countErr != nil {
				return persistence(countErr)
			}
			if len(s.admins)+otherDelegated == 0 {
				if auditErr := s.recordAudit(ctx, tx, auditEvent{
					actorID:      c.GetActorId(),
					action:       "account.platform_admin.revoke",
					resourceType: "account",
					resourceID:   targetActorID,
					requestID:    c.GetRequestId(),
					details: map[string]any{
						"before":  before,
						"enabled": false,
						"reason":  reason,
						"refusal": "last_platform_admin",
					},
					outcome: "rejected",
				}); auditErr != nil {
					return auditErr
				}
				return rejected(status.Error(codes.FailedPrecondition, "cannot revoke the last platform administrator"))
			}
		}
		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO tenant.platform_admins(actor_id,enabled,reason,updated_by,updated_at)
			VALUES($1,$2,$3,$4,now())
			ON CONFLICT (actor_id) DO UPDATE
			SET enabled=EXCLUDED.enabled,
			    reason=EXCLUDED.reason,
			    updated_by=EXCLUDED.updated_by,
			    updated_at=now()`, targetActorID, enabled, reason, c.GetActorId()); execErr != nil {
			return persistence(execErr)
		}
		action := "account.platform_admin.revoke"
		if enabled {
			action = "account.platform_admin.grant"
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      c.GetActorId(),
			action:       action,
			resourceType: "account",
			resourceID:   targetActorID,
			requestID:    c.GetRequestId(),
			details: map[string]any{
				"before":               before,
				"platformAdminEnabled": enabled,
				"reason":               reason,
			},
			outcome: "succeeded",
		}); auditErr != nil {
			return auditErr
		}
		proto.Merge(out, &api.PlatformAdminCommand{
			OperationId:          "platform-admin-" + hash(c.GetIdempotencyKey()+":"+targetActorID)[:18],
			AccountId:            targetActorID,
			Status:               "succeeded",
			PlatformAdminEnabled: enabled,
			IsBootstrapAdmin:     false,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
