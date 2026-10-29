package httpapi

import (
	"context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"net/http"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type ServeUpdateCommander interface {
	UpdateWorkspaceVersion(context.Context, string, *api.UpdateWorkspaceVersionRequest) (*api.Operation, error)
}

func registerServeUpdateRoute(mux *http.ServeMux, reader ServeDeliveryReader, identity IdentityReader) {
	s := &Server{identity: identity}
	s.publisherRoute(mux, "POST /api/v2/workspaces/{workspaceId}/update", owneridentity.Serve,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspaceId",
		func() proto.Message { return &api.UpdateWorkspaceVersionRequest{} },
		func(r *http.Request, _ *api.CallContext, body proto.Message) (proto.Message, error) {
			input := body.(*api.UpdateWorkspaceVersionRequest)
			if input.GetExpectedCurrentAgentDeploymentId() == "" {
				return nil, status.Error(codes.InvalidArgument, "expected current deployment is required")
			}
			if input.GetApplicationSelection() != nil {
				if input.CapabilityVersionId != nil {
					return nil, status.Error(codes.InvalidArgument, "application selection and capability version are mutually exclusive")
				}
				if err := contracts.ValidateWorkspaceApplicationSelection(input.ApplicationSelection); err != nil {
					return nil, status.Error(codes.InvalidArgument, err.Error())
				}
			} else if input.GetCapabilityVersionId() == "" {
				return nil, status.Error(codes.InvalidArgument, "application selection is required")
			}
			commander, ok := reader.(ServeUpdateCommander)
			if !ok || commander == nil {
				return nil, status.Error(codes.Unavailable, "Serve update owner is not configured")
			}
			return commander.UpdateWorkspaceVersion(r.Context(), r.PathValue("workspaceId"), input)
		})
}
