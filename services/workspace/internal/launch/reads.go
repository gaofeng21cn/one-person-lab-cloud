package launch

import (
	"context"
	"encoding/json"
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

const workspaceColumns = `w.id,w.tenant_id,w.name,w.status,w.compute_plan_id,w.storage_plan_id,w.created_at,w.updated_at,w.version,
	COALESCE(o.accepted_input,'{}'::jsonb),COALESCE(o.result,'{}'::jsonb)`
const workspaceJoin = ` FROM workspace.workspaces w LEFT JOIN workspace.operations o ON o.id=w.active_operation_id`

type rowScanner interface{ Scan(...any) error }

func scanWorkspace(row rowScanner) (*api.Workspace, string, error) {
	w := &api.Workspace{ResourceReadiness: api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_PENDING, ApplicationAvailability: api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_PENDING, DeliveryModel: api.WorkspaceDeliveryModelEnum_WORKSPACE_DELIVERY_MODEL_ENUM_AGENT_SAAS}
	var tenant, state string
	var created, updated time.Time
	var input, result []byte
	if err := row.Scan(&w.Id, &tenant, &w.Name, &state, &w.ComputePlanId, &w.StoragePlanId, &created, &updated, &w.Version, &input, &result); err != nil {
		return nil, "", dbError(err)
	}
	v, ok := api.WorkspaceStatusEnum_value["WORKSPACE_STATUS_ENUM_"+strings.ToUpper(state)]
	if !ok || v == 0 {
		return nil, "", status.Error(codes.DataLoss, "stored Workspace status is invalid")
	}
	w.Status = api.WorkspaceStatusEnum(v)
	w.CreatedAt, w.UpdatedAt = timestamppb.New(created), timestamppb.New(updated)
	var accepted acceptedOrder
	var progress orderResult
	if json.Unmarshal(input, &accepted) != nil || json.Unmarshal(result, &progress) != nil {
		return nil, "", status.Error(codes.DataLoss, "stored Workspace order is invalid")
	}
	if len(accepted.Quote) > 0 {
		quote := &api.QuoteAcceptance{}
		if protojson.Unmarshal(accepted.Quote, quote) != nil || quote.GetQuote().GetCapabilityVersionId() == "" {
			return nil, "", status.Error(codes.DataLoss, "stored Workspace capability choice is invalid")
		}
		w.CapabilityVersionId = proto.String(quote.Quote.GetCapabilityVersionId())
	}
	if progress.ResourceSetID != "" {
		w.ResourceReadiness = api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNKNOWN
	}
	if len(progress.ResourceReadback) > 0 {
		readback := &api.ResourceReadback{}
		if protojson.Unmarshal(progress.ResourceReadback, readback) != nil || readback.ResourceSetId != progress.ResourceSetID || readback.WorkspaceId != w.Id {
			return nil, "", status.Error(codes.DataLoss, "stored Workspace resource readback is invalid")
		}
		w.ResourceReadiness = resourceReadiness(readback)
	}
	if state == "failed" || state == "deleted" || state == "suspended" {
		w.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNAVAILABLE
	} else if state != "provisioning" {
		w.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN
	}
	// Accepted quotes confer neither a paid period nor Serve availability. The
	// current slice deliberately leaves currentPeriodEnd and accessUrl absent.
	return w, tenant, nil
}

func resourceReadiness(r *api.ResourceReadback) api.WorkspaceResourceReadinessEnum {
	if r.GetAbsenceConfirmed() {
		return api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNAVAILABLE
	}
	switch r.GetOutcome() {
	case api.Observation_OBSERVATION_CONFIRMED:
		return api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_READY
	case api.Observation_OBSERVATION_REJECTED:
		return api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNAVAILABLE
	default:
		return api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNKNOWN
	}
}

func (s *Service) GetWorkspace(ctx context.Context, r *api.GetWorkspaceRpcRequest) (*api.Workspace, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.GetWorkspaceId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace id is required")
	}
	w, tenant, err := scanWorkspace(s.Store.DB().QueryRowContext(ctx, `SELECT `+workspaceColumns+workspaceJoin+` WHERE w.id=$1`, r.WorkspaceId))
	if err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, workspaceResource(w.Id), tenant); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Service) ListWorkspaces(ctx context.Context, r *api.ListWorkspacesRpcRequest) (*api.WorkspacePage, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	tenant := r.Context.GetScope().GetTenant().GetTenantId()
	if tenant == "" {
		return nil, status.Error(codes.PermissionDenied, "Workspace list requires tenant scope")
	}
	limit := int32(25)
	if r.QueryLimit != nil {
		if *r.QueryLimit < 1 || *r.QueryLimit > 100 {
			return nil, status.Error(codes.InvalidArgument, "limit must be between 1 and 100")
		}
		limit = *r.QueryLimit
	}
	if err := s.authorize(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, workspaceResource(""), tenant); err != nil {
		return nil, err
	}
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT `+workspaceColumns+workspaceJoin+` WHERE w.tenant_id=$1 AND ($2='' OR w.id<$2) ORDER BY w.id DESC LIMIT $3`, tenant, r.GetQueryCursor(), limit+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	page := &api.WorkspacePage{}
	for rows.Next() {
		w, _, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		if len(page.Items) == int(limit) {
			page.NextCursor = proto.String(page.Items[len(page.Items)-1].Id)
			break
		}
		page.Items = append(page.Items, w)
	}
	if err = rows.Err(); err != nil {
		return nil, dbError(err)
	}
	return page, nil
}

func (s *Service) GetOperation(ctx context.Context, r *api.GetOperationRpcRequest) (*api.Operation, error) {
	if r.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE {
		return nil, status.Error(codes.InvalidArgument, "Workspace operation owner is required")
	}
	reader, err := ownerservice.NewOperations(ownerservice.OwnerWorkspace, s.Store, s.Auth)
	if err != nil {
		return nil, status.Error(codes.Internal, "Workspace operation readback is unavailable")
	}
	return reader.Read(ctx, &api.OwnerOperationRequest{Context: r.GetContext(), OperationId: r.GetOperationId()})
}
