package httpapi

import (
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
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
	s.publisherRoute(mux, "GET /api/v2/workspaces/{workspaceId}", owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, kind, "workspaceId", nil,
		func(r *http.Request, call *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return workspace.GetWorkspace(r.Context(), &api.GetWorkspaceRpcRequest{Context: call, WorkspaceId: r.PathValue("workspaceId")})
		})
}
