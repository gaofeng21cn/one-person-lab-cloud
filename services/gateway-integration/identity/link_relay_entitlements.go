package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

const linkRelaySeedDirectoryPageSize = 50

// readActorLinkRelayEntitlement resolves fail-closed Link Relay admission for one actor:
// an explicit row in tenant.link_relay_entitlements always wins, while missing rows
// fall back to platform administrator status only.
func (s *Service) readActorLinkRelayEntitlement(ctx context.Context, actorID string) (bool, string, *time.Time, error) {
	var enabled bool
	var reason string
	var updatedAt time.Time
	err := s.DB.QueryRowContext(
		ctx,
		`SELECT relay_enabled,reason,updated_at FROM tenant.link_relay_entitlements WHERE actor_id=$1`,
		actorID,
	).Scan(&enabled, &reason, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		admin, adminErr := s.isPlatformAdmin(ctx, actorID)
		if adminErr != nil {
			return false, "", nil, adminErr
		}
		if admin {
			return true, "platform_admin_default", nil, nil
		}
		return false, "", nil, nil
	}
	if err != nil {
		return false, "", nil, persistence(err)
	}
	utc := updatedAt.UTC()
	return enabled, reason, &utc, nil
}

// linkRelayEntitlementStates reads explicit Link Relay overrides for a batch of actors.
func (s *Service) linkRelayEntitlementStates(ctx context.Context, actorIDs []string) (map[string]bool, error) {
	if len(actorIDs) == 0 {
		return map[string]bool{}, nil
	}
	args := make([]any, 0, len(actorIDs))
	placeholders := make([]string, 0, len(actorIDs))
	for _, id := range actorIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := s.DB.QueryContext(
		ctx,
		`SELECT actor_id,relay_enabled FROM tenant.link_relay_entitlements WHERE actor_id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	out := make(map[string]bool, len(actorIDs))
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

// SetLinkRelayEntitlement grants or revokes explicit OPL Link Relay admission for one
// Gateway subject, recording audit trail and idempotency atomically in opl_tenant.
func (s *Service) SetLinkRelayEntitlement(ctx context.Context, r *api.SetLinkRelayEntitlementRpcRequest) (*api.LinkRelayEntitlementCommand, error) {
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
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETLINKRELAYENTITLEMENT,
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
	enabled := body.GetEnabled()
	if !enabled && s.isBootstrapAdmin(targetActorID) {
		return nil, status.Error(codes.PermissionDenied, "bootstrap platform administrator link relay access is reserved")
	}
	if _, err := s.Gateway.directoryIdentity(ctx, targetActorID); err != nil {
		return nil, err
	}
	canonicalReq := &api.SetLinkRelayEntitlementRpcRequest{
		TargetActorId: targetActorID,
		Body: &api.SetLinkRelayEntitlementRequest{
			ConfirmationAccountId: targetActorID,
			Enabled:               enabled,
			Reason:                reason,
		},
	}
	out := &api.LinkRelayEntitlementCommand{}
	err = s.command(ctx, c, "platform", "SetLinkRelayEntitlement", canonicalReq, out, func(tx *sql.Tx) error {
		if _, lockErr := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "tenant:link_relay:"+targetActorID); lockErr != nil {
			return persistence(lockErr)
		}
		var before bool
		scanErr := tx.QueryRowContext(ctx, `SELECT relay_enabled FROM tenant.link_relay_entitlements WHERE actor_id=$1 FOR UPDATE`, targetActorID).Scan(&before)
		if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
			return persistence(scanErr)
		}
		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO tenant.link_relay_entitlements(actor_id,relay_enabled,reason,updated_by,updated_at)
			VALUES($1,$2,$3,$4,now())
			ON CONFLICT (actor_id) DO UPDATE
			SET relay_enabled=EXCLUDED.relay_enabled,
			    reason=EXCLUDED.reason,
			    updated_by=EXCLUDED.updated_by,
			    updated_at=now()`, targetActorID, enabled, reason, c.GetActorId()); execErr != nil {
			return persistence(execErr)
		}
		action := "account.link_relay.revoke"
		if enabled {
			action = "account.link_relay.grant"
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      c.GetActorId(),
			action:       action,
			resourceType: "account",
			resourceID:   targetActorID,
			requestID:    c.GetRequestId(),
			details: map[string]any{
				"before":           before,
				"linkRelayEnabled": enabled,
				"reason":           reason,
			},
			outcome: "succeeded",
		}); auditErr != nil {
			return auditErr
		}
		proto.Merge(out, &api.LinkRelayEntitlementCommand{
			OperationId:      "link-relay-" + hash(c.GetIdempotencyKey()+":"+targetActorID)[:18],
			AccountId:        targetActorID,
			Status:           "succeeded",
			LinkRelayEnabled: enabled,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SeedActiveGatewayLinkRelayEntitlements paginates the live OPL Gateway user directory
// and inserts active users into tenant.link_relay_entitlements with ON CONFLICT DO NOTHING,
// preserving existing explicit revocations and recording exact scanned/seeded/existing counts.
func (s *Service) SeedActiveGatewayLinkRelayEntitlements(ctx context.Context, r *api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest) (*api.SeedLinkRelayEntitlementsCommand, error) {
	c := r.GetContext()
	if err := s.requirePlatformAdminCall(
		ctx,
		c,
		api.OwnerEnum_OWNER_ENUM_TENANT,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SEEDACTIVEGATEWAYLINKRELAYENTITLEMENTS,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		"",
	); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(r.GetBody().GetReason())
	if reason == "" || utf8.RuneCountInString(reason) > maxOperatorMutationReasonRunes {
		return nil, status.Error(codes.InvalidArgument, "reason must be between 1 and 200 characters")
	}

	seenActive := make(map[string]struct{})
	activeIDs := make([]string, 0)
	for page := 1; ; page++ {
		usersPage, err := s.Gateway.DirectoryUsers(ctx, page, linkRelaySeedDirectoryPageSize, "")
		if err != nil {
			return nil, err
		}
		for _, user := range usersPage.Items {
			if user.Status != "active" {
				continue
			}
			actorID := strconv.FormatInt(user.ID, 10)
			if _, dup := seenActive[actorID]; dup {
				continue
			}
			seenActive[actorID] = struct{}{}
			activeIDs = append(activeIDs, actorID)
		}
		if page >= usersPage.Pages || len(usersPage.Items) == 0 {
			break
		}
	}

	canonicalReq := &api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest{
		Body: &api.SeedActiveGatewayLinkRelayEntitlementsRequest{Reason: reason},
	}
	out := &api.SeedLinkRelayEntitlementsCommand{}
	err := s.command(ctx, c, "platform", "SeedActiveGatewayLinkRelayEntitlements", canonicalReq, out, func(tx *sql.Tx) error {
		if _, lockErr := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tenant:link_relay_entitlements:seed',0))`); lockErr != nil {
			return persistence(lockErr)
		}
		var inserted int64
		const chunkSize = 200
		for start := 0; start < len(activeIDs); start += chunkSize {
			end := min(start+chunkSize, len(activeIDs))
			chunk := activeIDs[start:end]
			args := make([]any, 0, len(chunk)+2)
			args = append(args, reason, c.GetActorId())
			tuples := make([]string, 0, len(chunk))
			for _, actorID := range chunk {
				args = append(args, actorID)
				tuples = append(tuples, fmt.Sprintf("($%d,true,$1,$2,now())", len(args)))
			}
			res, execErr := tx.ExecContext(
				ctx,
				`INSERT INTO tenant.link_relay_entitlements(actor_id,relay_enabled,reason,updated_by,updated_at) VALUES `+
					strings.Join(tuples, ",")+
					` ON CONFLICT (actor_id) DO NOTHING`,
				args...,
			)
			if execErr != nil {
				return persistence(execErr)
			}
			affected, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return persistence(rowsErr)
			}
			inserted += affected
		}
		scannedCount := int32(len(activeIDs))
		seededCount := int32(inserted)
		existingCount := scannedCount - seededCount
		if existingCount < 0 {
			existingCount = 0
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      c.GetActorId(),
			action:       "account.link_relay.seed",
			resourceType: "account",
			resourceID:   "*",
			requestID:    c.GetRequestId(),
			details: map[string]any{
				"scannedCount":  scannedCount,
				"seededCount":   seededCount,
				"existingCount": existingCount,
				"reason":        reason,
			},
			outcome: "succeeded",
		}); auditErr != nil {
			return auditErr
		}
		proto.Merge(out, &api.SeedLinkRelayEntitlementsCommand{
			OperationId:   "link-relay-seed-" + hash(c.GetIdempotencyKey())[:18],
			Status:        "succeeded",
			ScannedCount:  scannedCount,
			SeededCount:   seededCount,
			ExistingCount: existingCount,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) resolveCallerGatewaySubject(ctx context.Context, c *api.CallContext, bearerToken string) (string, string, bool, error) {
	if token := strings.TrimSpace(bearerToken); token != "" {
		user, err := s.Gateway.VerifyBearer(ctx, token)
		if err != nil {
			return "", "", false, err
		}
		return strconv.FormatInt(user.ID, 10), user.Email, true, nil
	}
	if c == nil || c.GetSessionId() == "" {
		return "", "", false, status.Error(codes.Unauthenticated, "active Cloud session or Gateway bearer required")
	}
	session, _, _, err := s.session(ctx, c.GetSessionId())
	if err != nil {
		return "", "", false, err
	}
	if c.GetActorId() != "" && c.GetActorId() != session.GetActorId() {
		return "", "", false, denied()
	}
	subjectID := session.GetActorId()
	if binding, bindErr := s.userBinding(ctx, session.GetActorId()); bindErr == nil && strings.TrimSpace(binding.Sub2APIUserID) != "" {
		subjectID = binding.Sub2APIUserID
	}
	user, err := s.Gateway.directoryIdentity(ctx, subjectID)
	if err != nil {
		return "", "", false, err
	}
	return subjectID, user.Email, user.Status == "active", nil
}

// ReadCallerLinkRelayEntitlement answers OPL Link Relay admission for the authenticated caller
// using either a live Gateway bearer token or an active Console session reference.
func (s *Service) ReadCallerLinkRelayEntitlement(ctx context.Context, r *api.ReadCallerLinkRelayEntitlementRpcRequest) (*api.LinkRelayEntitlementReadback, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	actorID, email, active, err := s.resolveCallerGatewaySubject(ctx, r.GetContext(), r.GetBearerToken())
	if err != nil {
		return nil, err
	}
	return s.buildLinkRelayReadback(ctx, actorID, email, active)
}

// ReadLinkRelayEntitlement answers internal service peer inquiries for one Gateway actor ID.
func (s *Service) ReadLinkRelayEntitlement(ctx context.Context, r *api.ReadLinkRelayEntitlementRequest) (*api.LinkRelayEntitlementReadback, error) {
	if _, ok := ownerservice.PeerOwner(ctx); !ok {
		return nil, denied()
	}
	actorID, err := validatePositiveActorID(r.GetActorId())
	if err != nil {
		return nil, err
	}
	user, err := s.Gateway.directoryIdentity(ctx, actorID)
	if err != nil {
		return nil, err
	}
	return s.buildLinkRelayReadback(ctx, actorID, user.Email, user.Status == "active")
}

func (s *Service) buildLinkRelayReadback(ctx context.Context, actorID, email string, gatewayActive bool) (*api.LinkRelayEntitlementReadback, error) {
	isBootstrap := s.isBootstrapAdmin(actorID)
	isAdmin, err := s.isPlatformAdmin(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if !gatewayActive {
		return &api.LinkRelayEntitlementReadback{
			ActorId:          actorID,
			Email:            strings.ToLower(strings.TrimSpace(email)),
			LinkRelayEnabled: false,
			IsBootstrapAdmin: isBootstrap,
			IsPlatformAdmin:  isAdmin,
			Reason:           "gateway_account_inactive",
		}, nil
	}
	enabled, reason, updatedAt, err := s.readActorLinkRelayEntitlement(ctx, actorID)
	if err != nil {
		return nil, err
	}
	out := &api.LinkRelayEntitlementReadback{
		ActorId:          actorID,
		Email:            strings.ToLower(strings.TrimSpace(email)),
		LinkRelayEnabled: enabled,
		IsBootstrapAdmin: isBootstrap,
		IsPlatformAdmin:  isAdmin,
		Reason:           reason,
	}
	if updatedAt != nil {
		out.UpdatedAt = timestamppb.New(*updatedAt)
	}
	return out, nil
}
