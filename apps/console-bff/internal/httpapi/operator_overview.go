package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const operatorOverviewMaxSafeInteger = int64(9007199254740991)

// readOperatorOverview composes the retained Control Plane overview with the
// Workspace owner's authoritative workspace count. It is intentionally kept
// separate from operator.go so route registration and the existing platform
// operator gate can be changed independently.
func (s *Server) readOperatorOverview(w http.ResponseWriter, r *http.Request, ctx context.Context, caller Caller) {
	raw, err := s.operator.OperatorOverview(ctx, caller.SessionID)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if !validOperatorEnvelope(raw, "control-plane") {
		writePublisherError(w, r, http.StatusBadGateway, "invalid_owner_response", "invalid operator owner response")
		return
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		writePublisherError(w, r, http.StatusBadGateway, "invalid_owner_response", "invalid operator owner response")
		return
	}
	if string(envelope["status"]) == `"unavailable"` {
		writeRawOperatorOverview(w, raw)
		return
	}

	var data map[string]json.RawMessage
	if dataRaw := envelope["data"]; len(dataRaw) == 0 || json.Unmarshal(dataRaw, &data) != nil || data == nil {
		writePublisherError(w, r, http.StatusBadGateway, "invalid_owner_response", "invalid operator owner response")
		return
	}

	workspaceCtx, err := s.authorizeOperatorWorkspaceOverview(ctx, caller)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	workspaceEnvelope, err := s.readOperatorWorkspaceCount(workspaceCtx)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	data["workspaces"] = mustMarshalOperatorOverviewValue(workspaceEnvelope)
	data["resources"] = mustMarshalOperatorOverviewValue(operatorSource("fabric", nil, "owner_aggregate_not_available", ""))

	envelope["source"] = mustMarshalOperatorOverviewValue("control-plane+workspace")
	envelope["data"] = mustMarshalOperatorOverviewValue(data)
	writeRawOperatorOverview(w, mustMarshalOperatorOverviewValue(envelope))
}

func (s *Server) authorizeOperatorWorkspaceOverview(ctx context.Context, caller Caller) (context.Context, error) {
	call := proto.Clone(clients.CallContext(ctx)).(*api.CallContext)
	call.AuthorizationContextId = ""
	workspaceCtx := clients.WithCallContext(ctx, call)
	if err := RequireAuthorizedAction(
		workspaceCtx,
		s.identity,
		caller,
		owneridentity.Workspace,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE},
		call.GetRequestId(),
	); err != nil {
		return nil, err
	}
	return workspaceCtx, nil
}

func (s *Server) readOperatorWorkspaceCount(ctx context.Context) (map[string]any, error) {
	workspaceUnavailable := func(reason string) map[string]any {
		return operatorSource("workspace", nil, reason, "")
	}

	if s.workspace == nil {
		return workspaceUnavailable("owner_unconfigured"), nil
	}
	page, err := s.workspace.ListOperatorWorkspaces(ctx, &api.ListOperatorWorkspacesRpcRequest{
		Context:  clients.CallContext(ctx),
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		if code := status.Code(err); code == codes.Unavailable || code == codes.DeadlineExceeded {
			return workspaceUnavailable("owner_unavailable"), nil
		}
		return nil, err
	}
	if page == nil || page.Page != 1 || page.PageSize != 1 || page.Total < 0 || page.Total > operatorOverviewMaxSafeInteger || len(page.Items) > 1 || int64(len(page.Items)) > page.Total {
		return nil, status.Error(codes.DataLoss, "invalid Workspace owner page")
	}
	return operatorSource("workspace", map[string]any{"total": page.Total}, "", ""), nil
}

func mustMarshalOperatorOverviewValue(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic("operator overview JSON value is not marshalable: " + err.Error())
	}
	return raw
}

func writeRawOperatorOverview(w http.ResponseWriter, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
