package httpapi

import (
	"errors"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

// NewWorkspaceHandler exposes the same authenticated routes as the BFF process.
func NewWorkspaceHandler(workspace api.WorkspaceProductServiceClient, identity IdentityReader) http.Handler {
	return (&Server{identity: identity, workspace: workspace}).Handler()
}

func (s *Server) registerWorkspaceRoutes(mux *http.ServeMux, workspace api.WorkspaceProductServiceClient) {
	const kind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE
	require := func() error {
		if workspace == nil {
			return status.Error(codes.Unavailable, "workspace owner is not configured")
		}
		return nil
	}
	s.publisherRoute(mux, "POST /api/v2/workspaces", owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, kind, "", func() proto.Message { return &api.CreateWorkspaceRequest{} },
		func(r *http.Request, call *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return workspace.CreateWorkspace(r.Context(), &api.CreateWorkspaceRpcRequest{Context: call, Body: body.(*api.CreateWorkspaceRequest)})
		})
	s.publisherRoute(mux, "GET /api/v2/workspaces", owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, kind, "", nil,
		func(r *http.Request, call *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			limit := int32(25)
			if r.URL.Query().Has("limit") {
				value := optionalLimit(r)
				if value == nil || *value < 1 || *value > 100 {
					return nil, status.Error(codes.InvalidArgument, "limit must be an integer from 1 to 100")
				}
				limit = *value
			}
			return workspace.ListWorkspaces(r.Context(), &api.ListWorkspacesRpcRequest{Context: call, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: &limit})
		})
	// The model configuration belongs to the Workspace owner: the owner persists the
	// accepted intent, its own applied version, and the operation that carries the
	// reload. The BFF only forwards the caller's own request to that owner, so a
	// Console never reimplements the configuration or reads it from another service.
	s.publisherRoute(mux, "GET /api/v2/workspaces/{workspaceId}/models", owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS, kind, "workspaceId", nil,
		func(r *http.Request, call *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return workspace.GetWorkspaceModels(r.Context(), &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: r.PathValue("workspaceId")})
		})
	s.publisherRoute(mux, "PUT /api/v2/workspaces/{workspaceId}/models", owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS, kind, "workspaceId", func() proto.Message { return &api.UpdateWorkspaceModelsRequest{} },
		func(r *http.Request, call *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return workspace.UpdateWorkspaceModels(r.Context(), &api.UpdateWorkspaceModelsRpcRequest{Context: call, Body: body.(*api.UpdateWorkspaceModelsRequest), WorkspaceId: r.PathValue("workspaceId")})
		})
	// The canonical Workspace schema is the console-bff's derived read model: its
	// applicationAvailability, accessUrl and modelConfigurationVersion are
	// compositions of the Serve-owned deployment, runtime instance and access
	// readbacks, not Workspace columns. The detail route therefore composes the
	// Workspace owner's own readback with the Serve facts under the caller's own
	// per-owner authorization.
	mux.HandleFunc("GET /api/v2/workspaces/{workspaceId}", func(w http.ResponseWriter, r *http.Request) {
		publisherRequestID(r)
		w.Header().Set("Cache-Control", "no-store")
		caller, err := RequireSession(r.Context(), s.identity, r)
		if err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
		resource := &api.AuthorizationResource{Kind: kind, Id: proto.String(r.PathValue("workspaceId"))}
		if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Workspace,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, resource, r.Header.Get(requestIDHeader)); err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		if err := require(); err != nil {
			writePublisherError(w, r, 503, "owner_unconfigured", "publisher owner unavailable")
			return
		}
		read, err := s.workspaceApplication(ctx, caller, r.PathValue("workspaceId"))
		if err != nil {
			// A denied Serve decision is an authorization boundary, not an upstream
			// failure, so it keeps its own status; everything else is mapped as the
			// publisher routes map an owner call.
			if errors.Is(err, ErrAuthorizationRequired) || errors.Is(err, ErrSessionRequired) {
				writePublisherIdentityError(w, r, err)
				return
			}
			writeOwnerCallFailure(w, r, err)
			return
		}
		raw, err := publicjson.Marshal(read)
		if err != nil {
			writePublisherError(w, r, 502, "invalid_owner_response", "publisher owner returned an invalid response")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	})
}
