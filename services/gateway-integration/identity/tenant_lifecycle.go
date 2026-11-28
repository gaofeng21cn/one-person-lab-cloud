package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// The Tenant lifecycle is the customer's account lifecycle. A personal customer
// owns exactly one Tenant, so suspending that Tenant is what disables that
// account: the owner's own fences already refuse a suspended Tenant's session,
// its authorization and its Tenant read, so this write is what makes them
// refuse.
//
// The contract for this command is already published: the canonical API
// declares POST /api/v2/admin/tenants/{tenantId}/suspend for the platform
// administrator, with the reason as its only input, an accepted Operation as
// its answer, and tenant.tenants plus tenant.operations as the facts it
// writes.

// suspendTenantReasonLimit is the canonical maximum length of the operator's
// reason. The published contract bounds it, so the owner refuses a longer one
// rather than storing a fact the contract cannot describe.
const suspendTenantReasonLimit = 1024

// SuspendTenant suspends one Tenant. The Tenant stops serving its members the
// moment this commits: its status changes, its live sessions are revoked and
// its permission version advances, so the owner's own fences refuse the
// suspended Tenant afterwards.
//
// The command is idempotent by key: a replay returns the original Operation
// instead of a second write, and the same key with a different command is a
// conflict rather than a silent second suspension.
func (s *Service) SuspendTenant(ctx context.Context, r *api.SuspendTenantRpcRequest) (*api.Operation, error) {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return nil, e
	}
	c := r.GetContext()
	if c == nil || c.GetScope().GetPlatform() == nil || strings.TrimSpace(c.GetActorId()) == "" || c.GetSessionId() == "" || strings.TrimSpace(c.GetRequestId()) == "" {
		return nil, denied()
	}
	decision, e := s.AuthorizeAction(ctx, &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SUSPENDTENANT,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(strings.TrimSpace(r.GetTenantId()))},
		RequestId:     c.GetRequestId(),
	})
	if e != nil {
		return nil, e
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	tenantID := strings.TrimSpace(r.GetTenantId())
	reason := strings.TrimSpace(r.GetBody().GetReason())
	if tenantID == "" || tenantID != strings.TrimSpace(r.GetTenantId()) {
		return nil, status.Error(codes.InvalidArgument, "a tenant id is required")
	}
	if reason == "" || utf8.RuneCountInString(reason) > suspendTenantReasonLimit {
		return nil, status.Error(codes.InvalidArgument, "a reason is required")
	}
	out := &api.Operation{}
	// The idempotency record covers the whole command, not only its reason: the
	// same key must never replay one Tenant's Operation for another Tenant.
	e = s.command(ctx, c, "platform", "SuspendTenant", &api.SuspendTenantRpcRequest{TenantId: tenantID, Body: r.GetBody()}, out, func(tx *sql.Tx) error {
		var current string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM tenant.tenants WHERE id=$1 FOR UPDATE`, tenantID).Scan(&current); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				// The Tenant does not exist, so the refusal is a platform-scoped
				// event: there is no Tenant row to hang the evidence on.
				if e := s.recordAudit(ctx, tx, auditEvent{actorID: c.GetActorId(), action: "tenant.suspend", resourceType: "tenant", resourceID: tenantID, requestID: c.GetRequestId(), details: map[string]any{"reason": reason, "reasonCode": "not_found"}, outcome: "rejected"}); e != nil {
					return e
				}
				return rejected(status.Error(codes.NotFound, "tenant not found"))
			}
			return persistence(e)
		}
		if current != "active" {
			// The Tenant is already suspended, deleting or deleted. Suspending it
			// again would be a second write for the same fact, so the owner
			// refuses and keeps the Tenant exactly as it is.
			if e := s.recordAudit(ctx, tx, auditEvent{tenantID: tenantID, actorID: c.GetActorId(), action: "tenant.suspend", resourceType: "tenant", resourceID: tenantID, requestID: c.GetRequestId(), details: map[string]any{"reason": reason, "reasonCode": "tenant_inactive", "currentStatus": current}, outcome: "rejected"}); e != nil {
				return e
			}
			return rejected(owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the tenant is not active"), api.ErrorCodeEnum_ERROR_CODE_ENUM_TENANT_INACTIVE))
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.tenants SET status='suspended', suspended_at=now(), permission_version=permission_version+1, updated_at=now() WHERE id=$1`, tenantID); e != nil {
			return persistence(e)
		}
		// The members' live sessions stop with the Tenant. They are revoked, not
		// deleted, so the session history stays auditable.
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now(), updated_at=now() WHERE tenant_id=$1 AND revoked_at IS NULL`, tenantID); e != nil {
			return persistence(e)
		}
		operationID := "op_" + randomID()[:20]
		accepted, _ := json.Marshal(map[string]any{"tenantId": tenantID, "reason": reason})
		// The durable operation commits atomically with the suspension, so the
		// accepted evidence can never disagree with the Tenant's own state.
		if _, e := tx.ExecContext(ctx, `INSERT INTO tenant.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,observation_result,request_id,accepted_input,result,completed_at) VALUES($1,$2,$3,'suspend_tenant',$4,'succeeded','suspend_tenant','confirmed',$5,$6,$6,now())`, operationID, tenantID, c.GetActorId(), tenantID, c.GetRequestId(), accepted); e != nil {
			return persistence(e)
		}
		if e := s.recordAudit(ctx, tx, auditEvent{tenantID: tenantID, actorID: c.GetActorId(), action: "tenant.suspend", resourceType: "tenant", resourceID: tenantID, requestID: c.GetRequestId(), details: map[string]any{"reason": reason}, outcome: "succeeded"}); e != nil {
			return e
		}
		proto.Merge(out, &api.Operation{OperationId: operationID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_SUSPEND_TENANT, ResourceId: tenantID, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED, RequestId: c.GetRequestId(), CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now()})
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
