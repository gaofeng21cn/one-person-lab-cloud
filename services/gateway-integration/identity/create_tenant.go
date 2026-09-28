package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// CreateTenant is the Cloud Tenant admission entry. It is a platform-admin
// operation: the actor's platform role is re-verified here, the Tenant owner
// writes the Tenant, its owner membership, its stable application-OCI repository
// binding and its durable operation in one transaction, and no caller-supplied
// destination is accepted. The owner email is an admission input used only to
// derive the initial repository-name candidate.
func (s *Service) CreateTenant(ctx context.Context, r *api.CreateTenantRpcRequest) (*api.Operation, error) {
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
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATETENANT,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT},
		RequestId:     c.GetRequestId(),
	})
	if e != nil {
		return nil, e
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return nil, status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	if s.registryHost == "" || s.registryNamespace == "" {
		return nil, status.Error(codes.FailedPrecondition, "installation registry host and namespace are required before admitting a Tenant")
	}
	b := r.GetBody()
	name := strings.TrimSpace(b.GetName())
	ownerSubject := strings.TrimSpace(b.GetOwnerGatewaySubjectId())
	if name == "" || ownerSubject == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant name and owner Gateway subject are required")
	}
	out := &api.Operation{}
	e = s.command(ctx, c, "platform", "CreateTenant", &api.CreateTenantRequest{Name: name, BillingSub2ApiUserId: b.GetBillingSub2ApiUserId(), OwnerGatewaySubjectId: ownerSubject, OwnerEmail: b.GetOwnerEmail()}, out, func(tx *sql.Tx) error {
		tenantID := "tenant_" + randomID()[:20]
		operationID := "op_" + randomID()[:20]
		if _, e := tx.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES($1,$2)`, tenantID, name); e != nil {
			return persistence(e)
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES($1,$2,$3,'owner')`, "member_"+randomID()[:20], tenantID, ownerSubject); e != nil {
			return persistence(e)
		}
		binding, e := s.reserveRepositoryBinding(ctx, tx, tenantID, b.GetOwnerEmail())
		if e != nil {
			return e
		}
		accepted, _ := json.Marshal(map[string]any{"name": name, "ownerEmailProvided": strings.TrimSpace(b.GetOwnerEmail()) != ""})
		// The durable operation commits atomically with the Tenant and binding.
		// Status/stage reuse the ownerstore vocabulary and the tenant schema's
		// own CHECK constraints unchanged.
		if _, e := tx.ExecContext(ctx, `INSERT INTO tenant.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,observation_result,request_id,accepted_input,result,completed_at) VALUES($1,$2,$3,'create_tenant',$4,'succeeded','create_tenant','confirmed',$5,$6,$6,now())`, operationID, tenantID, c.GetActorId(), tenantID, c.GetRequestId(), accepted); e != nil {
			return persistence(e)
		}
		if e := s.recordAudit(ctx, tx, auditEvent{tenantID: tenantID, actorID: c.GetActorId(), action: "tenant.create", resourceType: "tenant", resourceID: tenantID, requestID: c.GetRequestId(), details: map[string]any{"repository": binding.GetRepository(), "registryHost": binding.GetRegistryHost(), "registryNamespace": binding.GetRegistryNamespace(), "ownerEmailProvided": strings.TrimSpace(b.GetOwnerEmail()) != ""}, outcome: "succeeded"}); e != nil {
			return e
		}
		proto.Merge(out, &api.Operation{OperationId: operationID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_TENANT, ResourceId: tenantID, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED, RequestId: c.GetRequestId(), CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now()})
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
