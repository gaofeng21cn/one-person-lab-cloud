package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

var packageEntitlementIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func validatePackageEntitlementID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw || !packageEntitlementIDPattern.MatchString(trimmed) {
		return "", status.Error(codes.InvalidArgument, "a valid packageId is required")
	}
	return trimmed, nil
}

// packageEntitlementStates loads all restricted package entitlement rows for a batch of actors.
func (s *Service) packageEntitlementStates(ctx context.Context, actorIDs []string) (map[string][]*api.OperatorPackageEntitlement, error) {
	if len(actorIDs) == 0 {
		return map[string][]*api.OperatorPackageEntitlement{}, nil
	}
	args := make([]any, 0, len(actorIDs))
	placeholders := make([]string, 0, len(actorIDs))
	for _, id := range actorIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := s.DB.QueryContext(
		ctx,
		`SELECT actor_id,package_id,enabled,reason,expires_at,updated_at
		 FROM tenant.package_entitlements
		 WHERE actor_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY actor_id ASC, package_id ASC`,
		args...,
	)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	out := make(map[string][]*api.OperatorPackageEntitlement, len(actorIDs))
	for rows.Next() {
		var actorID, packageID, reason string
		var enabled bool
		var expiresAt sql.NullTime
		var updatedAt time.Time
		if scanErr := rows.Scan(&actorID, &packageID, &enabled, &reason, &expiresAt, &updatedAt); scanErr != nil {
			return nil, persistence(scanErr)
		}
		item := &api.OperatorPackageEntitlement{
			PackageId: packageID,
			Enabled:   enabled,
			Reason:    reason,
			UpdatedAt: timestamppb.New(updatedAt.UTC()),
		}
		if expiresAt.Valid {
			item.ExpiresAt = timestamppb.New(expiresAt.Time.UTC())
		}
		out[actorID] = append(out[actorID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}
	return out, nil
}

// SetPackageEntitlement grants, updates, or revokes restricted Software Center package
// admission for a Gateway subject atomically in opl_tenant with audit and idempotency.
func (s *Service) SetPackageEntitlement(ctx context.Context, r *api.SetPackageEntitlementRpcRequest) (*api.PackageEntitlementCommand, error) {
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
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPACKAGEENTITLEMENT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM,
		targetActorID,
	); err != nil {
		return nil, err
	}
	body := r.GetBody()
	if body == nil || strings.TrimSpace(body.GetConfirmationAccountId()) != targetActorID {
		return nil, status.Error(codes.InvalidArgument, "confirmationAccountId must match the target account")
	}
	packageID, err := validatePackageEntitlementID(body.GetPackageId())
	if err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(body.GetReason())
	if reason == "" || utf8.RuneCountInString(reason) > maxOperatorMutationReasonRunes {
		return nil, status.Error(codes.InvalidArgument, "reason must be between 1 and 200 characters")
	}
	if body.GetClearExpiresAt() && body.GetExpiresAt() != nil {
		return nil, status.Error(codes.InvalidArgument, "clearExpiresAt and expiresAt cannot both be set")
	}
	var expiresNull sql.NullTime
	if !body.GetClearExpiresAt() && body.GetExpiresAt() != nil {
		if err := body.GetExpiresAt().CheckValid(); err != nil {
			return nil, status.Error(codes.InvalidArgument, "expiresAt must be a valid timestamp")
		}
		expiresNull = sql.NullTime{Time: body.GetExpiresAt().AsTime().UTC(), Valid: true}
	}
	if _, err := s.Gateway.directoryIdentity(ctx, targetActorID); err != nil {
		return nil, err
	}

	enabled := body.GetEnabled()
	canonicalBody := &api.SetPackageEntitlementRequest{
		ConfirmationAccountId: targetActorID,
		PackageId:             packageID,
		Enabled:               enabled,
		Reason:                reason,
		ClearExpiresAt:        body.GetClearExpiresAt(),
	}
	if expiresNull.Valid {
		canonicalBody.ExpiresAt = timestamppb.New(expiresNull.Time)
	}
	canonicalReq := &api.SetPackageEntitlementRpcRequest{
		TargetActorId: targetActorID,
		Body:          canonicalBody,
	}

	out := &api.PackageEntitlementCommand{}
	err = s.command(ctx, c, "platform", "SetPackageEntitlement", canonicalReq, out, func(tx *sql.Tx) error {
		if _, lockErr := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "tenant:package:"+targetActorID+":"+packageID); lockErr != nil {
			return persistence(lockErr)
		}
		var before bool
		scanErr := tx.QueryRowContext(
			ctx,
			`SELECT enabled FROM tenant.package_entitlements WHERE actor_id=$1 AND package_id=$2 FOR UPDATE`,
			targetActorID,
			packageID,
		).Scan(&before)
		if scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
			return persistence(scanErr)
		}
		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO tenant.package_entitlements(actor_id,package_id,enabled,reason,expires_at,updated_by,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,now())
			ON CONFLICT (actor_id, package_id) DO UPDATE
			SET enabled=EXCLUDED.enabled,
			    reason=EXCLUDED.reason,
			    expires_at=EXCLUDED.expires_at,
			    updated_by=EXCLUDED.updated_by,
			    updated_at=now()`, targetActorID, packageID, enabled, reason, expiresNull, c.GetActorId()); execErr != nil {
			return persistence(execErr)
		}
		action := "account.package.revoke"
		if enabled {
			action = "account.package.grant"
		}
		details := map[string]any{
			"packageId": packageID,
			"before":    before,
			"enabled":   enabled,
			"reason":    reason,
		}
		if expiresNull.Valid {
			details["expiresAt"] = expiresNull.Time.Format(time.RFC3339Nano)
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      c.GetActorId(),
			action:       action,
			resourceType: "account_package",
			resourceID:   targetActorID + ":" + packageID,
			requestID:    c.GetRequestId(),
			details:      details,
			outcome:      "succeeded",
		}); auditErr != nil {
			return auditErr
		}
		cmd := &api.PackageEntitlementCommand{
			OperationId: "pkg-entitlement-" + hash(c.GetIdempotencyKey()+":"+targetActorID+":"+packageID)[:18],
			AccountId:   targetActorID,
			PackageId:   packageID,
			Status:      "succeeded",
			Enabled:     enabled,
		}
		if expiresNull.Valid {
			cmd.ExpiresAt = timestamppb.New(expiresNull.Time)
		}
		proto.Merge(out, cmd)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListCallerPackageEntitlements returns currently active, unexpired restricted package
// entitlements for the caller's live Gateway subject (Session or Gateway Bearer).
func (s *Service) ListCallerPackageEntitlements(ctx context.Context, r *api.ListCallerPackageEntitlementsRpcRequest) (*api.CallerPackageEntitlementsReadback, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	actorID, email, active, err := s.resolveCallerGatewaySubject(ctx, r.GetContext(), r.GetBearerToken())
	if err != nil {
		return nil, err
	}
	if !active {
		return &api.CallerPackageEntitlementsReadback{
			ActorId: actorID,
			Email:   strings.ToLower(strings.TrimSpace(email)),
		}, nil
	}
	rows, err := s.DB.QueryContext(
		ctx,
		`SELECT package_id,enabled,reason,expires_at,updated_at
		 FROM tenant.package_entitlements
		 WHERE actor_id=$1 AND enabled=true AND (expires_at IS NULL OR expires_at > now())
		 ORDER BY package_id ASC`,
		actorID,
	)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	items := make([]*api.OperatorPackageEntitlement, 0)
	for rows.Next() {
		var pkgID, reason string
		var enabled bool
		var expiresAt sql.NullTime
		var updatedAt time.Time
		if scanErr := rows.Scan(&pkgID, &enabled, &reason, &expiresAt, &updatedAt); scanErr != nil {
			return nil, persistence(scanErr)
		}
		entry := &api.OperatorPackageEntitlement{
			PackageId: pkgID,
			Enabled:   enabled,
			Reason:    reason,
			UpdatedAt: timestamppb.New(updatedAt.UTC()),
		}
		if expiresAt.Valid {
			entry.ExpiresAt = timestamppb.New(expiresAt.Time.UTC())
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}
	return &api.CallerPackageEntitlementsReadback{
		ActorId: actorID,
		Email:   strings.ToLower(strings.TrimSpace(email)),
		Items:   items,
	}, nil
}
