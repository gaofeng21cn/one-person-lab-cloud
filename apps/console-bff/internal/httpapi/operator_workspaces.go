package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

// Operator Workspace membership comes exclusively from Workspace. Fabric facts
// remain separate observations, never a second business lifecycle projection.
func (s *Server) readOperatorWorkspaces(w http.ResponseWriter, r *http.Request, ctx context.Context, caller Caller, id string, page, size int) {
	action := api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE}
	if id != "" {
		action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATORWORKSPACE
		resource.Id = &id
	}
	if page > math.MaxInt32 {
		writePublisherError(w, r, 400, "invalid_pagination", "page exceeds owner pagination range")
		return
	}
	clients.CallContext(ctx).AuthorizationContextId = ""
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Workspace, action, resource, clients.CallContext(ctx).GetRequestId()); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	if s.workspace == nil {
		writeOwnerCallFailure(w, r, status.Error(codes.Unavailable, "Workspace owner is not configured"))
		return
	}
	if id != "" {
		item, err := s.workspace.GetOperatorWorkspace(ctx, &api.GetOperatorWorkspaceRpcRequest{Context: clients.CallContext(ctx), WorkspaceId: id})
		if err != nil {
			writeOwnerCallFailure(w, r, err)
			return
		}
		if item.GetWorkspace().GetId() != id {
			writeOwnerCallFailure(w, r, status.Error(codes.DataLoss, "Workspace owner returned another identity"))
			return
		}
		data, err := s.operatorWorkspaceView(ctx, caller, item)
		if err != nil {
			if errors.Is(err, ErrAuthorizationRequired) || errors.Is(err, ErrSessionRequired) {
				writePublisherIdentityError(w, r, err)
				return
			}
			writeOwnerCallFailure(w, r, err)
			return
		}
		writeJSON(w, 200, operatorSource("workspace+fabric+serve", data, "", ""))
		return
	}
	result, err := s.workspace.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{Context: clients.CallContext(ctx), Page: int32(page), PageSize: int32(size)})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if result == nil || result.Page != int32(page) || result.PageSize != int32(size) || result.Total < 0 || result.Total > 9007199254740991 || len(result.Items) > size || int64(len(result.Items)) > result.Total {
		writeOwnerCallFailure(w, r, status.Error(codes.DataLoss, "invalid Workspace owner page"))
		return
	}
	items := make([]any, 0, len(result.Items))
	seen := map[string]bool{}
	for _, item := range result.Items {
		wid := item.GetWorkspace().GetId()
		if wid == "" || seen[wid] {
			writeOwnerCallFailure(w, r, status.Error(codes.DataLoss, "invalid Workspace page identity"))
			return
		}
		seen[wid] = true
		data, err := s.operatorWorkspaceView(ctx, caller, item)
		if err != nil {
			if errors.Is(err, ErrAuthorizationRequired) || errors.Is(err, ErrSessionRequired) {
				writePublisherIdentityError(w, r, err)
				return
			}
			writeOwnerCallFailure(w, r, err)
			return
		}
		items = append(items, data)
	}
	envelope := operatorSource("workspace+fabric+serve", map[string]any{"items": items, "total": result.Total, "page": page, "pageSize": size}, "", "")
	if len(items) == 0 {
		envelope["status"] = "empty"
	}
	writeJSON(w, 200, envelope)
}

func operatorSource(source string, data any, reason, updated string) map[string]any {
	out := map[string]any{"source": source, "status": "available", "available": true, "fetchedAt": time.Now().UTC().Format(time.RFC3339Nano)}
	if reason != "" {
		out["status"] = "unavailable"
		out["available"] = false
		out["reasonCode"] = reason
	} else {
		out["data"] = data
	}
	if updated != "" {
		out["sourceUpdatedAt"] = updated
	}
	return out
}
func operatorTime(t *timestamppb.Timestamp) string {
	if t == nil || t.CheckValid() != nil {
		return ""
	}
	return t.AsTime().UTC().Format(time.RFC3339Nano)
}
func operatorString(source, value, updated string) map[string]any {
	if value == "" {
		return operatorSource(source, nil, "fact_not_available", updated)
	}
	return operatorSource(source, value, "", updated)
}

func (s *Server) operatorWorkspaceView(ctx context.Context, caller Caller, item *api.OperatorWorkspace) (map[string]any, error) {
	workspace := item.GetWorkspace()
	if workspace.GetId() == "" || operatorTime(workspace.GetCreatedAt()) == "" || operatorTime(workspace.GetUpdatedAt()) == "" || item.GetTenantId() == "" {
		return nil, status.Error(codes.DataLoss, "invalid Workspace owner identity")
	}
	workspace, err := s.composeOperatorWorkspaceApplication(ctx, caller, workspace)
	if err != nil {
		return nil, err
	}
	raw, err := publicjson.Marshal(workspace)
	if err != nil {
		return nil, status.Error(codes.DataLoss, "invalid Workspace owner DTO")
	}
	var business map[string]any
	if json.Unmarshal(raw, &business) != nil {
		return nil, status.Error(codes.DataLoss, "invalid Workspace owner DTO")
	}
	// Match the customer adapter's field vocabulary; never derive a business state
	// from infrastructure observation or invent a retained Account mapping.
	business["state"] = business["status"]
	delete(business, "status")
	if v, ok := business["currentPeriodEnd"]; ok {
		business["paidThrough"] = v
		delete(business, "currentPeriodEnd")
	}
	if v, ok := business["accessUrl"]; ok {
		business["url"] = v
		delete(business, "accessUrl")
	}
	updated := operatorTime(workspace.UpdatedAt)
	out := map[string]any{
		"workspace":         operatorSource("workspace", business, "", updated),
		"tenant":            operatorSource("workspace", map[string]string{"id": item.TenantId}, "", updated),
		"createdBy":         operatorSource("workspace", map[string]string{"id": item.CreatedBy}, "", updated),
		"ownerAccount":      operatorSource("tenant", nil, "retained_account_mapping_not_provided", ""),
		"ownerUser":         operatorSource("tenant", nil, "user_profile_not_provided", ""),
		"receipt":           operatorSource("ledger", nil, "receipt_not_read", ""),
		"workspaceKeyUsage": operatorSource("gateway", nil, "workspace_key_usage_not_read", ""),
		"resources":         []any{},
	}
	if item.ResourceSetId == "" {
		out["resourcesReadback"] = operatorSource("workspace", nil, "resource_set_not_assigned", updated)
		return out, nil
	}
	if s.fabric == nil {
		out["resourcesReadback"] = operatorSource("fabric", nil, "owner_unconfigured", "")
		return out, nil
	}
	// Each owner gets a fresh exact decision. A Workspace list decision cannot be
	// reused as an observation grant or as permission for another Workspace.
	call := proto.Clone(clients.CallContext(ctx)).(*api.CallContext)
	call.AuthorizationContextId = ""
	fabricCtx := clients.WithCallContext(ctx, call)
	wid := workspace.Id
	if err = RequireAuthorizedAction(fabricCtx, s.identity, caller, owneridentity.Fabric, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READOPERATORRESOURCES, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: &wid}, call.RequestId); err != nil {
		return nil, err
	}
	read, err := s.fabric.ReadResources(fabricCtx, &api.ResourceReadbackRequest{Context: call, ResourceSetId: item.ResourceSetId, WorkspaceId: wid})
	if err != nil {
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded:
			out["resourcesReadback"] = operatorSource("fabric", nil, "fabric_read_unavailable", "")
			return out, nil
		default:
			return nil, err
		}
	}
	if read.GetResourceSetId() != item.ResourceSetId || read.GetWorkspaceId() != wid || read.GetResourceVersion() == "" || read.GetOutcome() == api.Observation_OBSERVATION_UNSPECIFIED {
		return nil, status.Error(codes.DataLoss, "Fabric readback identity or observation is invalid")
	}
	observed := operatorTime(read.ObservedAt)
	if read.ObservedAt != nil && observed == "" {
		return nil, status.Error(codes.DataLoss, "Fabric observation time is invalid")
	}
	outcome := strings.ToLower(strings.TrimPrefix(read.Outcome.String(), "OBSERVATION_"))
	out["resourcesReadback"] = operatorSource("fabric", map[string]any{"resourceSetId": read.ResourceSetId, "workspaceId": wid, "outcome": outcome, "absenceConfirmed": read.AbsenceConfirmed, "resourceVersion": read.ResourceVersion, "observedAt": observed}, "", observed)
	resources := make([]any, 0, len(read.Resources))
	seen := map[string]bool{}
	for _, fact := range read.Resources {
		kind, id, state := fact.GetKind(), fact.GetId(), fact.GetState()
		// Fabric deliberately reports the runtime, Secret and attachment deletion
		// facts before those resources have ever been dispatched. Their identity is
		// therefore empty, but the kind/state pair is still an authoritative fact.
		if id == "" && kind != "workspace_runtime" && kind != "gateway_secret_binding" && kind != "storage_attachment" {
			return nil, status.Error(codes.DataLoss, "invalid Fabric resource fact")
		}
		key := kind + "\x00" + id
		if kind == "" || state == "" || seen[key] {
			return nil, status.Error(codes.DataLoss, "invalid Fabric resource fact")
		}
		seen[key] = true
		resources = append(resources, map[string]any{
			"ownerAccount": out["ownerAccount"], "ownerUser": out["ownerUser"],
			"workspace":    operatorSource("workspace", map[string]string{"id": wid, "name": workspace.Name}, "", updated),
			"resourceType": operatorSource("fabric", fact.Kind, "", observed), "status": operatorSource("fabric", fact.State, "", observed),
			"providerErrorCode": operatorString("fabric", read.ErrorCode, observed), "providerId": operatorString("fabric", fact.OpaqueProviderReference, observed),
			"packageOrSpec": operatorSource("fabric", nil, "fact_not_provided", observed), "zone": operatorSource("fabric", nil, "fact_not_provided", observed),
			"createdAt": operatorSource("fabric", nil, "fact_not_provided", observed), "expiresAt": operatorSource("fabric", nil, "fact_not_provided", observed),
			"lastReadAt": operatorString("fabric", observed, observed), "operationRef": operatorString("workspace", item.OperationId, updated),
			"receiptRef": operatorString("fabric", fact.ReceiptId, observed),
		})
	}
	out["resources"] = resources
	return out, nil
}
