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
	data, err := s.readRetainedOperatorOverview(ctx, caller.SessionID)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
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

	writeRawOperatorOverview(w, mustMarshalOperatorOverviewValue(operatorSource("control-plane+workspace", data, "", "")))
}

// A retained projection outage belongs to its own fields. It cannot suppress
// an independently authorized Workspace owner read or substitute a zero count.
func (s *Server) readRetainedOperatorOverview(ctx context.Context, sessionID string) (map[string]json.RawMessage, error) {
	unavailable := func(reason string) map[string]json.RawMessage {
		data := make(map[string]json.RawMessage)
		for field, source := range map[string]string{
			"accounts": "control-plane", "wallet": "sub2api", "keys": "sub2api",
			"usage": "sub2api", "reconciliation": "control-plane", "health": "control-plane",
		} {
			data[field] = mustMarshalOperatorOverviewValue(operatorSource(source, nil, reason, ""))
		}
		return data
	}
	if s.operator == nil {
		return unavailable("control_plane_owner_unconfigured"), nil
	}
	raw, err := s.operator.OperatorOverview(ctx, sessionID)
	if err != nil {
		if code := status.Code(err); code == codes.Unavailable || code == codes.DeadlineExceeded {
			return unavailable("control_plane_owner_unavailable"), nil
		}
		return nil, err
	}
	if !validOperatorEnvelope(raw, "control-plane") {
		return nil, status.Error(codes.DataLoss, "invalid operator owner response")
	}
	var envelope struct {
		Status     string                     `json:"status"`
		ReasonCode string                     `json:"reasonCode"`
		Data       map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, status.Error(codes.DataLoss, "invalid operator owner response")
	}
	if envelope.Status == "unavailable" {
		return unavailable(envelope.ReasonCode), nil
	}
	if envelope.Data == nil {
		return nil, status.Error(codes.DataLoss, "invalid operator owner response")
	}
	return envelope.Data, nil
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
